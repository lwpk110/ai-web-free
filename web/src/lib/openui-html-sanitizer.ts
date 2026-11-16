import React from "react";

const CODE_SEGMENT_RE = /(```[\s\S]*?```|`[^`\n]+`)/g;

const KNOWN_STRIPPABLE_TAG_RE =
  /<\/?(?:span|div|font|center|p|b|strong|i|em|u|s|del|strike|small|sup|sub|mark|details|summary|section|article|header|footer|ul|ol|li|kbd|icon|svg|path|badge|pill|chip|tag|status|metric|stat|row|col|column|box|card|stack|spacer|divider|CodeBlock|openui|openui-lang)\b[^>]*\/?>/gi;

// 通用未知伪标签兜底（如 <StatusPill color="ok" />、<MetricCard>...</MetricCard>），
// 避开泛型语法（如 Array<string>）、自动链接（<https://...>）与数学比较符号（如 x < 5）
const UNKNOWN_PSEUDO_TAG_RE =
  /(?<![A-Za-z0-9_])<\/?(?!https?:\/\/)(?:[A-Z][A-Za-z0-9_:-]*(?:\s+[^<>]*)?\/?|[a-z][a-z0-9_-]*(?:\s+[a-zA-Z_:][a-zA-Z0-9_.:-]*\s*=\s*(?:"[^"]*"|'[^']*'|[^\s<>]+))*[^<>]*\/)>/g;

const UNKNOWN_OPEN_OR_CLOSE_WITH_ATTRS_RE =
  /(?<![A-Za-z0-9_])<(?!https?:\/\/)(?:\/[A-Za-z][A-Za-z0-9_:-]*|[A-Za-z][A-Za-z0-9_:-]*\s+[a-zA-Z_:][a-zA-Z0-9_.:-]*\s*=[^<>]*)>/g;

const NON_TEXT_PROP_KEYS = new Set([
  "codeString",
  "language",
  "id",
  "value",
  "url",
  "src",
  "bgImageSrc",
  "image",
  "icon",
  "name",
  "variant",
  "size",
  "type",
  "orientation",
  "direction",
  "provider",
  "statusFilter",
  "focus",
  "mediaKind",
  "targetUrl",
  "action",
  "children",
]);

function convertOutsideCode(
  text: string,
  mode: "markdown" | "plain"
): string {
  const isPlain = mode === "plain";
  let out = text.replace(/&nbsp;/gi, " ");

  out = out.replace(/<br\s*\/?>/gi, isPlain ? " " : "  \n");
  out = out.replace(/<hr\s*\/?>/gi, isPlain ? " — " : "\n\n---\n\n");
  out = out.replace(/<\/p>\s*<p\b[^>]*>/gi, isPlain ? " " : "\n\n");
  out = out.replace(/<\/?p\b[^>]*>/gi, isPlain ? " " : "\n");

  // 移除自闭合伪 UI 标签（如 <icon name="shield-check" color="success" size="xl"/>）
  out = out.replace(/<(?:icon|spacer|divider|svg|path)\b[^>]*\/?>/gi, "");

  // 迭代展开常见内联与结构化 HTML / 伪 Widget 标签（支持最多 3 层嵌套）
  for (let pass = 0; pass < 3; pass++) {
    const prev = out;
    out = out.replace(/<(strong|b)\b[^>]*>([\s\S]*?)<\/\1>/gi, isPlain ? "$2" : "**$2**");
    out = out.replace(/<(em|i)\b[^>]*>([\s\S]*?)<\/\1>/gi, isPlain ? "$2" : "*$2*");
    out = out.replace(/<(del|s|strike)\b[^>]*>([\s\S]*?)<\/\1>/gi, isPlain ? "$2" : "~~$2~~");
    out = out.replace(/<(code|kbd)\b[^>]*>([\s\S]*?)<\/\1>/gi, isPlain ? "$2" : "`$2`");
    out = out.replace(
      /<a\b[^>]*href=["']([^"']+)["'][^>]*>([\s\S]*?)<\/a>/gi,
      isPlain ? "$2" : "[$2]($1)"
    );
    out = out.replace(/<h([1-6])\b[^>]*>([\s\S]*?)<\/h\1>/gi, (_m, lvl, body) =>
      isPlain ? `${body.trim()} ` : `\n${"#".repeat(Number(lvl))} ${body.trim()}\n`
    );
    out = out.replace(/<li\b[^>]*>([\s\S]*?)<\/li>/gi, (_m, body) =>
      isPlain ? `• ${body.trim()} ` : `\n- ${body.trim()}`
    );
    out = out.replace(/<blockquote\b[^>]*>([\s\S]*?)<\/blockquote>/gi, (_m, body) =>
      isPlain ? body.trim() : `\n> ${body.trim()}\n`
    );
    out = out.replace(
      /<(span|div|small|sup|sub|mark|u|font|center|details|summary|section|article|header|footer|ul|ol|badge|pill|chip|tag|status|metric|stat|row|col|column|box|card|stack)\b[^>]*>([\s\S]*?)<\/\1>/gi,
      "$2"
    );
    if (out === prev) break;
  }

  // 清理已知 HTML / 伪 Widget 标签以及任意未知的大驼峰/带属性伪 XML 标签
  out = out.replace(KNOWN_STRIPPABLE_TAG_RE, "");
  out = out.replace(UNKNOWN_PSEUDO_TAG_RE, "");
  out = out.replace(UNKNOWN_OPEN_OR_CLOSE_WITH_ATTRS_RE, "");

  if (isPlain) {
    out = out.replace(/\s+/g, " ").trim();
  } else {
    out = out.replace(/\n{3,}/g, "\n\n");
  }
  return out;
}

/**
 * 将大模型偶发输出的 HTML 或任意未知伪 XML/JSX 标签（如 `<br>`、`<icon .../>`、`<span>`、`<StatusPill>`）
 * 安全标准化为 Markdown（`mode="markdown"`）或纯净文本（`mode="plain"`），
 * 同时完整保留 ```...``` 代码块、`...` 行内代码、泛型写法（`Array<T>`）与数学比较符。
 */
export function sanitizeOpenUIHtmlText(
  raw: string,
  modeOrSingleLine: "markdown" | "plain" | boolean = "markdown"
): string {
  if (!raw || (!raw.includes("<") && !raw.includes("&nbsp;"))) {
    return raw;
  }
  const mode: "markdown" | "plain" =
    modeOrSingleLine === true
      ? "plain"
      : modeOrSingleLine === false
        ? "markdown"
        : modeOrSingleLine;

  const parts = raw.split(CODE_SEGMENT_RE);
  return parts
    .map((part, idx) => (idx % 2 === 1 ? part : convertOutsideCode(part, mode)))
    .join("");
}

/**
 * 专门用于普通 Markdown 文本行的轻量通用兜底：
 * 在保留已知 Widget 标签（如 `<badge>`、`<icon>`、`<b>`）与合法链接的前提下，
 * 剥离任意未知的大驼峰/带属性伪 XML 标签，避免前端露出尖括号源码。
 */
export function stripUnknownPseudoXmlTags(raw: string): string {
  if (!raw || !raw.includes("<")) return raw;
  const parts = raw.split(CODE_SEGMENT_RE);
  return parts
    .map((part, idx) => {
      if (idx % 2 === 1) return part;
      return part
        .replace(UNKNOWN_PSEUDO_TAG_RE, "")
        .replace(UNKNOWN_OPEN_OR_CLOSE_WITH_ATTRS_RE, "");
    })
    .join("");
}

function sanitizeValueRecursive(
  val: unknown,
  mode: "markdown" | "plain",
  depth = 0
): unknown {
  if (depth > 4 || val == null) return val;
  if (typeof val === "string") {
    return sanitizeOpenUIHtmlText(val, mode);
  }
  if (Array.isArray(val)) {
    let changed = false;
    const next = val.map((item) => {
      const cleaned = sanitizeValueRecursive(item, mode, depth + 1);
      if (cleaned !== item) changed = true;
      return cleaned;
    });
    return changed ? next : val;
  }
  if (
    typeof val === "object" &&
    !React.isValidElement(val) &&
    Object.getPrototypeOf(val) === Object.prototype
  ) {
    const obj = val as Record<string, unknown>;
    if ("type" in obj && ("props" in obj || "componentName" in obj)) {
      return val;
    }
    let changed = false;
    const next: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(obj)) {
      if (NON_TEXT_PROP_KEYS.has(k)) {
        next[k] = v;
      } else {
        const cleaned = sanitizeValueRecursive(v, mode, depth + 1);
        if (cleaned !== v) changed = true;
        next[k] = cleaned;
      }
    }
    return changed ? next : val;
  }
  return val;
}

export function deepSanitizeOpenUIProps<T extends Record<string, any>>(
  props: T,
  mode: "markdown" | "plain"
): T {
  if (!props || typeof props !== "object") return props;
  return sanitizeValueRecursive(props, mode, 0) as T;
}
