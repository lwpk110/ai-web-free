import React, { useEffect, useState } from "react";
import {
  AlertTriangle,
  Check,
  CheckCircle2,
  Copy,
  Download,
  ExternalLink,
  Info,
  Maximize2,
  RotateCw,
  ShieldCheck,
  Sparkles,
  X,
  XCircle,
  ZoomIn,
  ZoomOut,
} from "lucide-react";
import { stripUnknownPseudoXmlTags } from "../lib/openui-html-sanitizer";

function MarkdownImageCard({ src, alt }: { src: string; alt: string }) {
  const [loaded, setLoaded] = useState(false);
  const [failed, setFailed] = useState(false);
  const [retryKey, setRetryKey] = useState(0);
  const [lightboxOpen, setLightboxOpen] = useState(false);
  const [zoomActual, setZoomActual] = useState(false);
  const [copied, setCopied] = useState(false);

  // 计算带重试参数的图片地址，避免缓存死锁
  const effectiveSrc = retryKey > 0
    ? (src.includes("?") ? `${src}&_r=${retryKey}` : `${src}?_r=${retryKey}`)
    : src;

  const handleRetry = (e: React.MouseEvent) => {
    e.stopPropagation();
    setFailed(false);
    setLoaded(false);
    setRetryKey((k) => k + 1);
  };

  const handleCopyLink = async (e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(src);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // 忽略剪贴板异常
    }
  };

  const handleDownload = (e: React.MouseEvent) => {
    e.stopPropagation();
    const a = document.createElement("a");
    a.href = src;
    a.download = `image-${Date.now()}.png`;
    a.target = "_blank";
    a.rel = "noreferrer";
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  };

  useEffect(() => {
    if (!lightboxOpen) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setLightboxOpen(false);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [lightboxOpen]);

  return (
    <>
      <span className="block my-2.5 w-full max-w-xs sm:max-w-sm md:max-w-md overflow-hidden rounded-2xl border z-border shadow-sm group bg-[var(--bg-surface)] relative select-none">
        {/* 骨架屏占位：绝对定位覆盖在图片上方，绝不用 display:none 隐藏 img，防止触发浏览器 lazy-load 死锁 */}
        {!loaded && !failed && (
          <span className="flex flex-col items-center justify-center gap-2 w-full aspect-square bg-gradient-to-br from-indigo-500/10 via-[var(--bg-muted)] to-purple-500/10 animate-pulse text-xs z-text-muted">
            <span className="w-5 h-5 rounded-full border-2 border-indigo-500/40 border-t-indigo-600 animate-spin" />
            <span>正在加载缩略图...</span>
          </span>
        )}

        {failed ? (
          <span className="flex flex-col items-center justify-center gap-2 w-full py-8 px-4 bg-[var(--bg-muted)] text-xs z-text-muted text-center">
            <span className="font-medium text-[var(--text-default)]">缩略图加载受阻</span>
            <div className="flex items-center gap-2 mt-1">
              <button
                type="button"
                onClick={handleRetry}
                className="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg border z-border bg-[var(--bg-surface)] hover:bg-[var(--bg-muted)] text-xs cursor-pointer transition shadow-xs"
              >
                <RotateCw size={12} />
                <span>重新加载</span>
              </button>
              <a
                href={src}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg text-indigo-500 hover:text-indigo-600 hover:underline text-xs"
              >
                <ExternalLink size={12} />
                <span>在新标签页打开</span>
              </a>
            </div>
          </span>
        ) : (
          <div
            className="relative cursor-pointer overflow-hidden bg-black/5 dark:bg-white/5"
            onClick={() => setLightboxOpen(true)}
            title="点击打开图像工坊同款全屏灯箱预览"
          >
            {/* img 始终保持在 DOM 布局流中，绝不使用 display:none，确保浏览器即刻发起网络请求并触发 onLoad */}
            <img
              key={effectiveSrc}
              src={effectiveSrc}
              alt={alt || "AI 生成图片"}
              referrerPolicy="no-referrer"
              crossOrigin="anonymous"
              onLoad={() => {
                setLoaded(true);
                setFailed(false);
              }}
              onError={() => {
                setFailed(true);
                setLoaded(false);
              }}
              className={`w-full h-auto max-h-80 object-contain transition-all duration-300 group-hover:scale-[1.01] ${
                loaded ? "opacity-100" : "opacity-0 absolute inset-0 pointer-events-none"
              }`}
            />

            {/* 图像工坊同款：悬浮操作快捷胶囊栏 */}
            {loaded && !failed && (
              <div className="absolute top-2 right-2 flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity bg-black/65 backdrop-blur-xs rounded-xl p-1 text-white shadow-md">
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    setLightboxOpen(true);
                  }}
                  className="p-1 rounded-lg hover:bg-white/20 transition cursor-pointer"
                  title="全屏大图预览"
                >
                  <Maximize2 size={13} />
                </button>
                <button
                  type="button"
                  onClick={handleDownload}
                  className="p-1 rounded-lg hover:bg-white/20 transition cursor-pointer"
                  title="下载图片"
                >
                  <Download size={13} />
                </button>
                <a
                  href={src}
                  target="_blank"
                  rel="noreferrer"
                  onClick={(e) => e.stopPropagation()}
                  className="p-1 rounded-lg hover:bg-white/20 transition inline-flex items-center text-white"
                  title="在新标签页打开原图"
                >
                  <ExternalLink size={13} />
                </a>
              </div>
            )}
          </div>
        )}

        {/* 底部提示词信息栏 */}
        {alt && (
          <span className="block px-3 py-1.5 text-[11px] z-text-muted bg-[var(--bg-surface)] border-t z-border truncate">
            {alt}
          </span>
        )}
      </span>

      {/* 图像工坊同款全屏画廊灯箱 (Lightbox) */}
      {lightboxOpen && (
        <div
          role="dialog"
          aria-modal="true"
          className="fixed inset-0 z-[9999] flex flex-col items-center justify-center bg-black/85 backdrop-blur-sm p-4 sm:p-6"
          onClick={() => setLightboxOpen(false)}
        >
          {/* 顶栏控制栏 */}
          <div
            className="w-full max-w-5xl flex items-center justify-between gap-3 text-white mb-3"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center gap-2 min-w-0">
              <span className="px-2.5 py-1 rounded-lg bg-white/10 text-xs font-medium font-mono truncate max-w-[260px] sm:max-w-md">
                {alt || "AI 生成图片原图"}
              </span>
            </div>

            <div className="flex items-center gap-1.5 shrink-0">
              <button
                type="button"
                onClick={() => setZoomActual((z) => !z)}
                className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-lg bg-white/10 hover:bg-white/20 text-xs transition cursor-pointer"
                title={zoomActual ? "适应屏幕" : "1:1 原图"}
              >
                {zoomActual ? <ZoomOut size={13} /> : <ZoomIn size={13} />}
                <span className="hidden sm:inline">{zoomActual ? "适应屏幕" : "1:1 原图"}</span>
              </button>
              <button
                type="button"
                onClick={handleCopyLink}
                className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-lg bg-white/10 hover:bg-white/20 text-xs transition cursor-pointer"
                title="复制图片链接"
              >
                {copied ? <Check size={13} className="text-emerald-400" /> : <Copy size={13} />}
                <span className="hidden sm:inline">{copied ? "已复制" : "复制链接"}</span>
              </button>
              <button
                type="button"
                onClick={handleDownload}
                className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-lg bg-white/10 hover:bg-white/20 text-xs transition cursor-pointer"
                title="下载原图"
              >
                <Download size={13} />
                <span className="hidden sm:inline">下载</span>
              </button>
              <a
                href={src}
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-lg bg-white/10 hover:bg-white/20 text-xs transition text-white"
                title="新标签页打开"
              >
                <ExternalLink size={13} />
                <span className="hidden sm:inline">新标签打开</span>
              </a>
              <button
                type="button"
                onClick={() => setLightboxOpen(false)}
                className="p-1.5 rounded-lg bg-white/10 hover:bg-white/25 text-white transition ml-1 cursor-pointer"
                title="关闭 (Esc)"
              >
                <X size={16} />
              </button>
            </div>
          </div>

          {/* 图像主视口 */}
          <div
            className="flex-1 w-full max-w-5xl flex items-center justify-center overflow-auto rounded-2xl p-2 relative"
            onClick={(e) => {
              // 点击图片背景关闭，点击图片本身切换缩放
              if (e.target === e.currentTarget) {
                setLightboxOpen(false);
              }
            }}
          >
            <img
              src={effectiveSrc}
              alt={alt || "高清原图"}
              referrerPolicy="no-referrer"
              onClick={(e) => {
                e.stopPropagation();
                setZoomActual((z) => !z);
              }}
              className={`rounded-xl shadow-2xl transition-all select-none ${
                zoomActual
                  ? "max-w-none w-auto h-auto cursor-zoom-out m-auto"
                  : "max-w-full max-h-[82vh] w-auto h-auto object-contain cursor-zoom-in"
              }`}
            />
          </div>
        </div>
      )}
    </>
  );
}

export type HybridSegment =
  | { type: "markdown"; text: string }
  | { type: "openui"; code: string };

const OPENUI_FENCE_RE =
  /```(?:openui-lang|openui)[ \t]*\r?\n([\s\S]*?)(?:\r?\n```(?=[ \t]*(?:\r?\n|$))|$)/gi;

const OPENUI_ROOT_LINE_RE =
  /^\s*root\s*=\s*[A-Z][A-Za-z0-9_]*\s*(?:\(|$)/m;

const OPENUI_ASSIGN_START_RE =
  /^\s*[a-zA-Z_][A-Za-z0-9_]*\s*=\s*[A-Z][A-Za-z0-9_]*\s*(?:\(|$)/;

/**
 * 判断片段是否真实包含 OpenUI Lang DSL 语句（如 `root = Card(...)`）。
 * 严禁把普通 Markdown 当作 OpenUI 传入 `<Renderer />`，否则会导致页面空白。
 */
export function containsOpenUIDSL(text: string): boolean {
  const trimmed = text.trim();
  if (!trimmed) return false;
  return OPENUI_ROOT_LINE_RE.test(trimmed);
}

function isLikelyStreamingOpenUIFencePrefix(code: string): boolean {
  const trimmed = code.trim();
  if (!trimmed) return true;
  if (trimmed.length <= 16 && !trimmed.includes("\n")) {
    return /^r(?:o(?:o(?:t(?:\s*(?:=(?:\s*(?:[A-Z][A-Za-z0-9_]*)?)?)?)?)?)?)?$/.test(
      trimmed
    );
  }
  return false;
}

/**
 * 将不含 ```openui 围栏、但实际混排了 `root = Card(...)` 的文本拆分为 Markdown 与 OpenUI 片段。
 * 普通 fenced code block（如 ```python / ```json）内部会被原样保留为 Markdown，不会误拆。
 */
function splitUnfencedOpenUI(text: string): HybridSegment[] {
  const normalized = text.replace(/\r\n/g, "\n");
  if (!containsOpenUIDSL(normalized)) {
    return text.trim() ? [{ type: "markdown", text }] : [];
  }

  const lines = normalized.split("\n");
  const out: HybridSegment[] = [];
  let mdBuf: string[] = [];
  let activeFence: string | null = null;
  let i = 0;

  const flushMd = () => {
    const joined = mdBuf.join("\n").trim();
    if (joined) {
      out.push({ type: "markdown", text: joined });
    }
    mdBuf = [];
  };

  while (i < lines.length) {
    const line = lines[i];
    const trimmed = line.trim();

    // 跳过非 openui 的普通代码块内部行
    const fenceMatch = /^(`{3,}|~{3,})(.*)$/.exec(trimmed);
    if (fenceMatch) {
      if (!activeFence) {
        activeFence = fenceMatch[1];
      } else if (trimmed.startsWith(activeFence)) {
        activeFence = null;
      }
      mdBuf.push(line);
      i++;
      continue;
    }

    if (!activeFence && OPENUI_ASSIGN_START_RE.test(line)) {
      // 尝试采集一段连续的 OpenUI DSL 赋值块
      const dslLines: string[] = [];
      let j = i;
      let parenBalance = 0;
      let bracketBalance = 0;
      let inString = false;
      let escaped = false;

      const scanLineState = (s: string) => {
        for (let k = 0; k < s.length; k++) {
          const ch = s[k];
          if (escaped) {
            escaped = false;
            continue;
          }
          if (inString) {
            if (ch === "\\") {
              escaped = true;
            } else if (ch === '"') {
              inString = false;
            }
            continue;
          }
          if (ch === '"') {
            inString = true;
          } else if (ch === "(") {
            parenBalance++;
          } else if (ch === ")") {
            parenBalance = Math.max(0, parenBalance - 1);
          } else if (ch === "[") {
            bracketBalance++;
          } else if (ch === "]") {
            bracketBalance = Math.max(0, bracketBalance - 1);
          }
        }
      };

      while (j < lines.length) {
        const cur = lines[j];
        const curTrim = cur.trim();
        const inContinuation = parenBalance > 0 || bracketBalance > 0 || inString;

        if (inContinuation) {
          dslLines.push(cur);
          scanLineState(cur);
          j++;
          continue;
        }

        if (OPENUI_ASSIGN_START_RE.test(cur)) {
          dslLines.push(cur);
          scanLineState(cur);
          j++;
          continue;
        }

        // 赋值语句之间的空行或注释，仅当后续仍有赋值语句时继续吞并
        if (curTrim === "" || curTrim.startsWith("//") || curTrim.startsWith("# ")) {
          let lookahead = j + 1;
          while (lookahead < lines.length && lines[lookahead].trim() === "") {
            lookahead++;
          }
          if (lookahead < lines.length && OPENUI_ASSIGN_START_RE.test(lines[lookahead])) {
            dslLines.push(cur);
            j++;
            continue;
          }
        }
        break;
      }

      const dslCandidate = dslLines.join("\n").trim();
      if (containsOpenUIDSL(dslCandidate)) {
        flushMd();
        out.push({ type: "openui", code: dslCandidate });
        i = j;
        continue;
      }
    }

    mdBuf.push(line);
    i++;
  }

  flushMd();
  return out.length > 0 ? out : [{ type: "markdown", text }];
}

function cleanAdjacentOpenUIWrappers(segments: HybridSegment[]): HybridSegment[] {
  const result = segments.map((s) => ({ ...s }));
  const openWrapperRe = /(?:^|\n)\s*<(?:CodeBlock(?:\s+[^>]*)?|openui(?:-lang)?)\s*>\s*$/i;
  const closeWrapperRe = /^\s*<\/(?:CodeBlock|openui(?:-lang)?)\s*>\s*(?:\n|$)/i;

  for (let idx = 0; idx < result.length; idx++) {
    if (result[idx].type !== "openui") continue;

    const prev = result[idx - 1];
    const next = result[idx + 1];
    if (prev && prev.type === "markdown" && openWrapperRe.test(prev.text)) {
      prev.text = prev.text.replace(openWrapperRe, "").trimEnd();
      if (next && next.type === "markdown" && closeWrapperRe.test(next.text)) {
        next.text = next.text.replace(closeWrapperRe, "").trimStart();
      }
    } else if (next && next.type === "markdown" && closeWrapperRe.test(next.text)) {
      next.text = next.text.replace(closeWrapperRe, "").trimStart();
    }
  }

  return result.filter((s) => (s.type === "markdown" ? s.text.trim().length > 0 : true));
}

export function splitHybridSegments(
  content: string,
  _legacyOpenUIMode = false
): HybridSegment[] {
  const trimmed = content.trim();
  if (!trimmed) {
    return [];
  }

  // 1. 先检测显式 ```openui / ```openui-lang 代码块
  OPENUI_FENCE_RE.lastIndex = 0;
  const segments: HybridSegment[] = [];
  let lastIdx = 0;
  let match: RegExpExecArray | null;

  while ((match = OPENUI_FENCE_RE.exec(content)) !== null) {
    const before = content.slice(lastIdx, match.index);
    if (before.trim()) {
      segments.push(...splitUnfencedOpenUI(before));
    }
    const code = (match[1] || "").trim();
    if (containsOpenUIDSL(code) || isLikelyStreamingOpenUIFencePrefix(code)) {
      if (code) {
        segments.push({ type: "openui", code });
      }
    } else if (code) {
      // 模型误将普通 Markdown 放进了 ```openui 围栏：回退为 Markdown 渲染，杜绝空白
      segments.push({ type: "markdown", text: code });
    }
    lastIdx = match.index + match[0].length;
  }

  if (segments.length > 0) {
    const tail = content.slice(lastIdx);
    if (tail.trim()) {
      segments.push(...splitUnfencedOpenUI(tail));
    }
    return cleanAdjacentOpenUIWrappers(segments);
  }

  // 2. 无 ```openui 围栏时，自动拆分混排或纯 DSL，否则一律交由 Markdown 渲染
  return cleanAdjacentOpenUIWrappers(splitUnfencedOpenUI(content));
}

type AttrValue = string | number | boolean;

interface WidgetElementNode {
  kind: "element";
  tag: string;
  attrs: Record<string, AttrValue>;
  children: WidgetAstNode[];
}

interface WidgetTextNode {
  kind: "text";
  text: string;
}

type WidgetAstNode = WidgetElementNode | WidgetTextNode;

type WidgetToken =
  | { type: "text"; text: string }
  | { type: "open"; tag: string; attrs: Record<string, AttrValue> }
  | { type: "self"; tag: string; attrs: Record<string, AttrValue> }
  | { type: "close"; tag: string };

const BLOCK_WIDGET_TAGS = new Set([
  "row",
  "col",
  "column",
  "box",
  "card",
  "grid",
  "grid-item",
  "carousel",
  "carousel-item",
  "stack",
  "callout",
  "alert",
  "details",
  "summary",
  "div",
  "section",
  "progress",
  "bar",
  "divider",
  "hr",
  "spacer",
  "ul",
  "ol",
  "table",
  "thead",
  "tbody",
  "tr",
  "h1",
  "h2",
  "h3",
  "h4",
  "h5",
  "h6",
  "blockquote",
]);

const INLINE_WIDGET_TAGS = new Set([
  "text",
  "title",
  "heading",
  "subtitle",
  "metric",
  "stat",
  "label",
  "caption",
  "badge",
  "pill",
  "chip",
  "tag",
  "status",
  "span",
  "p",
  "b",
  "strong",
  "i",
  "em",
  "u",
  "s",
  "del",
  "code",
  "kbd",
  "mark",
  "sup",
  "sub",
  "small",
  "br",
  "li",
  "th",
  "td",
  "a",
  "asyncimage",
  "image",
  "img",
  "audio",
  "icon",
]);

const KNOWN_WIDGET_TAGS = new Set([
  ...BLOCK_WIDGET_TAGS,
  ...INLINE_WIDGET_TAGS,
]);

const VOID_WIDGET_TAGS = new Set([
  "br",
  "hr",
  "divider",
  "spacer",
  "asyncimage",
  "image",
  "img",
  "audio",
  "icon",
]);

const FLEX_CONTAINER_TAGS = new Set([
  "row",
  "col",
  "column",
  "box",
  "card",
  "grid",
  "grid-item",
  "carousel",
  "carousel-item",
  "stack",
]);

const KNOWN_BOOLEAN_ATTRS = new Set([
  "border",
  "bordered",
  "clip",
  "tabularnums",
  "wrap",
  "nowrap",
  "bold",
  "italic",
  "truncate",
  "open",
  "inline",
  "compact",
  "flush",
  "fill",
  "grow",
  "shrink",
  "disabled",
  "checked",
  "shadow",
  "hoverable",
  "secondary",
]);

const MAYBE_WIDGET_RE = new RegExp(
  `<\\/?(${Array.from(KNOWN_WIDGET_TAGS).join("|")})(?:\\s|>|\\/>)`,
  "i"
);

const MAYBE_BLOCK_WIDGET_RE = new RegExp(
  `<(${Array.from(BLOCK_WIDGET_TAGS).join("|")})(?:\\s|>|\\/>)`,
  "i"
);

function parseWidgetAttrs(
  raw: string
): { attrs: Record<string, AttrValue>; valid: boolean } {
  const attrs: Record<string, AttrValue> = {};
  const trimmed = raw.trim();
  if (!trimmed) {
    return { attrs, valid: true };
  }

  let i = 0;
  let hasEqualAttr = false;
  let allKnownBooleans = true;
  const len = raw.length;

  while (i < len) {
    while (i < len && /\s/.test(raw[i])) i++;
    if (i >= len) break;

    const nameStart = i;
    while (i < len && /[a-zA-Z0-9_.:-]/.test(raw[i])) i++;
    const name = raw.slice(nameStart, i);
    if (!name || !/^[a-zA-Z_:][a-zA-Z0-9_.:-]*$/.test(name)) {
      return { attrs, valid: false };
    }

    while (i < len && /\s/.test(raw[i])) i++;
    if (i < len && raw[i] === "=") {
      hasEqualAttr = true;
      i++; // skip '='
      while (i < len && /\s/.test(raw[i])) i++;
      if (i >= len) return { attrs, valid: false };

      const ch = raw[i];
      if (ch === '"' || ch === "'") {
        const quote = ch;
        i++;
        let val = "";
        while (i < len && raw[i] !== quote) {
          if (raw[i] === "\\" && i + 1 < len) {
            val += raw[i + 1];
            i += 2;
            continue;
          }
          val += raw[i];
          i++;
        }
        if (i >= len) return { attrs, valid: false };
        i++; // closing quote
        attrs[name] = val;
      } else if (ch === "{") {
        i++;
        let depth = 1;
        const valStart = i;
        let inQuote: string | null = null;
        while (i < len && depth > 0) {
          const c = raw[i];
          if (inQuote) {
            if (c === "\\" && i + 1 < len) {
              i += 2;
              continue;
            }
            if (c === inQuote) inQuote = null;
          } else {
            if (c === '"' || c === "'") inQuote = c;
            else if (c === "{") depth++;
            else if (c === "}") depth--;
          }
          i++;
        }
        if (depth !== 0) return { attrs, valid: false };
        const expr = raw.slice(valStart, i - 1).trim();
        if (/^-?\d+(\.\d+)?$/.test(expr)) {
          attrs[name] = Number(expr);
        } else if (expr === "true") {
          attrs[name] = true;
        } else if (expr === "false") {
          attrs[name] = false;
        } else if (
          (expr.startsWith('"') && expr.endsWith('"')) ||
          (expr.startsWith("'") && expr.endsWith("'"))
        ) {
          attrs[name] = expr.slice(1, -1);
        } else {
          attrs[name] = expr;
        }
      } else {
        const valStart = i;
        while (i < len && !/\s/.test(raw[i])) i++;
        attrs[name] = raw.slice(valStart, i);
      }
    } else {
      attrs[name] = true;
      if (!KNOWN_BOOLEAN_ATTRS.has(name.toLowerCase())) {
        allKnownBooleans = false;
      }
    }
  }

  // 防止自然语言句子 `<text 里面 包含 普通词语>` 被误判为 JSX 标签
  if (!hasEqualAttr && !allKnownBooleans && Object.keys(attrs).length > 1) {
    return { attrs, valid: false };
  }
  return { attrs, valid: true };
}

function tryParseTagAt(
  input: string,
  pos: number
):
  | {
      kind: "open" | "self" | "close";
      tag: string;
      attrs: Record<string, AttrValue>;
      end: number;
    }
  | null {
  if (input[pos] !== "<") return null;

  // 闭合标签 </tag>
  if (input[pos + 1] === "/") {
    const closeMatch = /^<\/([a-zA-Z][a-zA-Z0-9-]*)\s*>/.exec(input.slice(pos));
    if (!closeMatch) return null;
    const tag = closeMatch[1].toLowerCase();
    if (!KNOWN_WIDGET_TAGS.has(tag)) return null;
    return {
      kind: "close",
      tag,
      attrs: {},
      end: pos + closeMatch[0].length,
    };
  }

  const openNameMatch = /^<([a-zA-Z][a-zA-Z0-9-]*)/.exec(input.slice(pos));
  if (!openNameMatch) return null;
  const tag = openNameMatch[1].toLowerCase();
  if (!KNOWN_WIDGET_TAGS.has(tag)) return null;

  let cur = pos + openNameMatch[0].length;
  const afterName = input[cur];
  if (
    afterName !== undefined &&
    afterName !== ">" &&
    afterName !== "/" &&
    !/\s/.test(afterName)
  ) {
    return null;
  }

  let quote: string | null = null;
  let braceDepth = 0;
  let foundClose = -1;

  while (cur < input.length) {
    const ch = input[cur];
    if (quote) {
      if (ch === "\\" && cur + 1 < input.length) {
        cur += 2;
        continue;
      }
      if (ch === quote) quote = null;
    } else if (braceDepth > 0) {
      if (ch === '"' || ch === "'") quote = ch;
      else if (ch === "{") braceDepth++;
      else if (ch === "}") braceDepth--;
    } else {
      if (ch === '"' || ch === "'") quote = ch;
      else if (ch === "{") braceDepth++;
      else if (ch === "<") {
        // 遇到新的 < 且此前没有 >，说明并非完整合法标签
        return null;
      } else if (ch === ">") {
        foundClose = cur;
        break;
      }
    }
    cur++;
  }

  if (foundClose === -1) return null;

  let attrEnd = foundClose;
  let selfClosing = false;
  if (attrEnd > pos + openNameMatch[0].length && input[attrEnd - 1] === "/") {
    selfClosing = true;
    attrEnd--;
  }

  const rawAttrs = input.slice(pos + openNameMatch[0].length, attrEnd);
  const parsedAttrs = parseWidgetAttrs(rawAttrs);
  if (!parsedAttrs.valid) return null;

  return {
    kind: selfClosing ? "self" : "open",
    tag,
    attrs: parsedAttrs.attrs,
    end: foundClose + 1,
  };
}

function tokenizeWidgetMarkup(input: string): WidgetToken[] {
  const tokens: WidgetToken[] = [];
  let textBuf = "";
  let i = 0;

  const flushText = () => {
    if (textBuf) {
      tokens.push({ type: "text", text: textBuf });
      textBuf = "";
    }
  };

  while (i < input.length) {
    const ch = input[i];

    // 保护反引号内的行内代码，不将其中的 `<badge>` 解析为标签
    if (ch === "`") {
      let tickLen = 0;
      while (i + tickLen < input.length && input[i + tickLen] === "`") {
        tickLen++;
      }
      const closer = "`".repeat(tickLen);
      const searchEnd = input.indexOf(closer, i + tickLen);
      const nextNl = input.indexOf("\n", i + tickLen);
      if (searchEnd !== -1 && (nextNl === -1 || searchEnd < nextNl)) {
        textBuf += input.slice(i, searchEnd + tickLen);
        i = searchEnd + tickLen;
        continue;
      }
      textBuf += ch;
      i++;
      continue;
    }

    if (ch === "<") {
      const parsed = tryParseTagAt(input, i);
      if (parsed) {
        flushText();
        if (parsed.kind === "close") {
          tokens.push({ type: "close", tag: parsed.tag });
        } else if (parsed.kind === "self") {
          tokens.push({ type: "self", tag: parsed.tag, attrs: parsed.attrs });
        } else {
          tokens.push({ type: "open", tag: parsed.tag, attrs: parsed.attrs });
        }
        i = parsed.end;
        continue;
      }
    }

    textBuf += ch;
    i++;
  }

  flushText();
  return tokens;
}

function hasValidWidgetTagInText(input: string): boolean {
  if (!MAYBE_WIDGET_RE.test(input)) return false;
  const tokens = tokenizeWidgetMarkup(input);
  return tokens.some((t) => t.type !== "text");
}

function startsWithKnownWidgetTag(trimmedLine: string): boolean {
  if (!trimmedLine.startsWith("<")) return false;
  return tryParseTagAt(trimmedLine, 0) !== null;
}

function isOrphanClosingWidgetLine(trimmedLine: string): boolean {
  if (!trimmedLine.startsWith("</")) return false;
  const tokens = tokenizeWidgetMarkup(trimmedLine);
  let hasClose = false;
  for (const tok of tokens) {
    if (tok.type === "close") {
      hasClose = true;
    } else if (tok.type === "text" && !tok.text.trim()) {
      continue;
    } else {
      return false;
    }
  }
  return hasClose;
}

function buildWidgetTree(tokens: WidgetToken[]): WidgetAstNode[] {
  const root: WidgetElementNode = {
    kind: "element",
    tag: "__root__",
    attrs: {},
    children: [],
  };
  const stack: WidgetElementNode[] = [root];

  for (const tok of tokens) {
    const top = stack[stack.length - 1];
    if (tok.type === "text") {
      top.children.push({ kind: "text", text: tok.text });
    } else if (tok.type === "self") {
      top.children.push({
        kind: "element",
        tag: tok.tag,
        attrs: tok.attrs,
        children: [],
      });
    } else if (tok.type === "open") {
      const el: WidgetElementNode = {
        kind: "element",
        tag: tok.tag,
        attrs: tok.attrs,
        children: [],
      };
      top.children.push(el);
      if (!VOID_WIDGET_TAGS.has(tok.tag)) {
        stack.push(el);
      }
    } else if (tok.type === "close") {
      for (let k = stack.length - 1; k >= 1; k--) {
        if (stack[k].tag === tok.tag) {
          stack.length = k;
          break;
        }
      }
    }
  }

  return root.children;
}

function resolveAttrString(
  attrs: Record<string, AttrValue>,
  ...keys: string[]
): string | undefined {
  for (const k of keys) {
    const found =
      attrs[k] ?? attrs[k.toLowerCase()];
    if (typeof found === "string") return found;
    if (typeof found === "number") return String(found);
  }
  return undefined;
}

function hasBooleanAttr(
  attrs: Record<string, AttrValue>,
  ...keys: string[]
): boolean {
  for (const k of keys) {
    const found = attrs[k] ?? attrs[k.toLowerCase()];
    if (found === true || found === "true") return true;
  }
  return false;
}

function resolveSpacing(val: AttrValue | undefined): string | undefined {
  if (val === undefined || typeof val === "boolean") return undefined;
  if (typeof val === "number") {
    return val === 0 ? "0px" : `${val * 0.25}rem`;
  }
  const s = val.trim();
  if (!s) return undefined;
  if (/^-?\d+(\.\d+)?$/.test(s)) {
    const n = Number(s);
    return n === 0 ? "0px" : `${n * 0.25}rem`;
  }
  return s;
}

function resolveRadius(val: AttrValue | undefined): string | undefined {
  if (val === undefined || val === false) return undefined;
  if (val === true) return "0.5rem";
  const s = String(val).trim().toLowerCase();
  if (s === "none") return "0px";
  if (s === "xs") return "0.25rem";
  if (s === "sm") return "0.375rem";
  if (s === "md") return "0.5rem";
  if (s === "lg") return "0.75rem";
  if (s === "xl") return "1rem";
  if (s === "2xl") return "1.25rem";
  if (s === "full") return "9999px";
  return resolveSpacing(val);
}

function isSafeCssColor(val: string): boolean {
  return /^(#[0-9a-fA-F]{3,8}|rgba?\([^)]+\)|hsla?\([^)]+\)|var\(--[a-zA-Z0-9-]+\))$/.test(
    val.trim()
  );
}

function resolveTextColor(color: string | undefined): string | undefined {
  if (!color) return undefined;
  const c = color.trim().toLowerCase();
  switch (c) {
    case "primary":
      return "var(--text-primary)";
    case "secondary":
      return "var(--text-secondary)";
    case "muted":
    case "tertiary":
      return "var(--text-muted)";
    case "success":
    case "green":
    case "ok":
      return "var(--pill-success-text)";
    case "warning":
    case "warn":
    case "amber":
    case "yellow":
      return "var(--pill-warn-text)";
    case "danger":
    case "error":
    case "red":
    case "critical":
      return "var(--pill-danger-text)";
    case "info":
    case "blue":
    case "accent":
      return "var(--pill-info-text)";
    case "white":
      return "#ffffff";
    default:
      return isSafeCssColor(color) ? color.trim() : undefined;
  }
}

function resolveBgColor(bg: string | undefined): string | undefined {
  if (!bg) return undefined;
  const c = bg.trim().toLowerCase();
  switch (c) {
    case "surface":
    case "surface-primary":
      return "var(--bg-surface)";
    case "surface-secondary":
    case "elevated":
      return "var(--bg-elevated)";
    case "surface-tertiary":
    case "muted":
    case "subtle":
      return "var(--bg-muted)";
    case "canvas":
      return "var(--bg-canvas)";
    case "success":
    case "green":
      return "var(--pill-success-bg)";
    case "warning":
    case "warn":
    case "amber":
      return "var(--pill-warn-bg)";
    case "danger":
    case "error":
    case "red":
      return "var(--pill-danger-bg)";
    case "info":
    case "blue":
      return "var(--pill-info-bg)";
    default:
      return isSafeCssColor(bg) ? bg.trim() : undefined;
  }
}

function resolveAlign(val: string | undefined): string | undefined {
  if (!val) return undefined;
  const v = val.trim().toLowerCase();
  if (v === "start" || v === "flex-start") return "flex-start";
  if (v === "end" || v === "flex-end") return "flex-end";
  if (v === "center" || v === "stretch" || v === "baseline") return v;
  return undefined;
}

function resolveJustify(val: string | undefined): string | undefined {
  if (!val) return undefined;
  const v = val.trim().toLowerCase();
  if (v === "between" || v === "space-between") return "space-between";
  if (v === "around" || v === "space-around") return "space-around";
  if (v === "evenly" || v === "space-evenly") return "space-evenly";
  if (v === "start" || v === "flex-start") return "flex-start";
  if (v === "end" || v === "flex-end") return "flex-end";
  if (v === "center") return "center";
  return undefined;
}

function resolveFontSize(
  size: string | undefined,
  fallback?: string
): string | undefined {
  if (!size) return fallback;
  switch (size.trim().toLowerCase()) {
    case "xs":
      return "0.7rem";
    case "sm":
      return "0.75rem";
    case "md":
    case "base":
      return "0.825rem";
    case "lg":
      return "0.95rem";
    case "xl":
      return "1.125rem";
    case "2xl":
      return "1.35rem";
    case "3xl":
      return "1.65rem";
    default:
      return /^\d+(\.\d+)?(px|rem|em)$/.test(size.trim())
        ? size.trim()
        : fallback;
  }
}

function resolveToneClass(attrs: Record<string, AttrValue>): string {
  const tone = (
    resolveAttrString(attrs, "color", "tone", "variant", "type", "status") || ""
  )
    .trim()
    .toLowerCase();
  if (["success", "green", "ok", "healthy"].includes(tone)) {
    return "z-pill-success";
  }
  if (["warning", "warn", "amber", "yellow", "orange", "degraded"].includes(tone)) {
    return "z-pill-warn";
  }
  if (["danger", "error", "red", "critical"].includes(tone)) {
    return "z-pill-danger";
  }
  if (["info", "blue", "primary", "accent"].includes(tone)) {
    return "z-pill-info";
  }
  return "md-widget-tone-neutral";
}

function renderWidgetAst(
  nodes: WidgetAstNode[],
  keyPrefix: string,
  parentTag?: string
): React.ReactNode[] {
  const inFlexParent = parentTag ? FLEX_CONTAINER_TAGS.has(parentTag) : false;

  return nodes
    .map((node, idx): React.ReactNode => {
      const key = `${keyPrefix}-${idx}`;
      if (node.kind === "text") {
        if (inFlexParent && !node.text.trim()) {
          return null;
        }
        const textToRender = inFlexParent ? node.text.trim() : node.text;
        return (
          <React.Fragment key={key}>
            {renderInlineMarkdown(textToRender, `${key}-tx`, 1, false)}
          </React.Fragment>
        );
      }

      const { tag, attrs, children } = node;
      const renderedChildren = renderWidgetAst(children, key, tag);

      if (tag === "br") return <br key={key} />;
      if (tag === "icon") {
        const iconName = (resolveAttrString(attrs, "name") || "").toLowerCase();
        const color = resolveTextColor(resolveAttrString(attrs, "color"));
        const iconStyle = color ? { color } : undefined;
        if (iconName.includes("shield")) {
          return <ShieldCheck key={key} size={14} className="inline-block align-text-bottom mr-1" style={iconStyle} />;
        }
        if (iconName.includes("check") || iconName.includes("success")) {
          return <CheckCircle2 key={key} size={14} className="inline-block align-text-bottom mr-1" style={iconStyle} />;
        }
        if (iconName.includes("warn") || iconName.includes("alert")) {
          return <AlertTriangle key={key} size={14} className="inline-block align-text-bottom mr-1" style={iconStyle} />;
        }
        if (iconName.includes("error") || iconName.includes("close") || iconName.includes("x")) {
          return <XCircle key={key} size={14} className="inline-block align-text-bottom mr-1" style={iconStyle} />;
        }
        if (iconName.includes("info")) {
          return <Info key={key} size={14} className="inline-block align-text-bottom mr-1" style={iconStyle} />;
        }
        return <Sparkles key={key} size={13} className="inline-block align-text-bottom mr-1" style={iconStyle} />;
      }
      if (tag === "hr" || tag === "divider") {
        return <hr key={key} className="md-hr" />;
      }
      if (tag === "spacer") {
        return <div key={key} style={{ flex: 1 }} />;
      }

      if (
        tag === "badge" ||
        tag === "pill" ||
        tag === "chip" ||
        tag === "tag" ||
        tag === "status"
      ) {
        const size = resolveAttrString(attrs, "size")?.toLowerCase();
        const toneCls = resolveToneClass(attrs);
        return (
          <span
            key={key}
            className={`md-widget-badge border ${toneCls} ${
              size === "lg" ? "is-lg" : size === "xs" ? "is-xs" : ""
            }`}
          >
            {renderedChildren}
          </span>
        );
      }

      if (
        tag === "title" ||
        tag === "heading" ||
        tag === "metric" ||
        tag === "stat"
      ) {
        const color = resolveTextColor(resolveAttrString(attrs, "color"));
        const fontSize = resolveFontSize(
          resolveAttrString(attrs, "size"),
          "0.95rem"
        );
        const tabular =
          hasBooleanAttr(attrs, "tabularNums") ||
          tag === "metric" ||
          tag === "stat";
        return (
          <span
            key={key}
            className={`md-widget-title ${tabular ? "font-mono-num" : ""}`}
            style={{
              color: color || "var(--text-primary)",
              fontSize,
            }}
          >
            {renderedChildren}
          </span>
        );
      }

      if (
        tag === "text" ||
        tag === "subtitle" ||
        tag === "caption" ||
        tag === "label"
      ) {
        const color = resolveTextColor(
          resolveAttrString(attrs, "color") ||
            (tag === "subtitle" || tag === "caption" ? "secondary" : undefined)
        );
        const fontSize = resolveFontSize(
          resolveAttrString(attrs, "size"),
          tag === "caption" ? "0.7rem" : "0.775rem"
        );
        const weightRaw = resolveAttrString(attrs, "weight")?.toLowerCase();
        const fontWeight =
          hasBooleanAttr(attrs, "bold") || weightRaw === "bold"
            ? 700
            : weightRaw === "semibold" || weightRaw === "medium"
              ? 600
              : undefined;
        const tabular = hasBooleanAttr(attrs, "tabularNums");
        return (
          <span
            key={key}
            className={`md-widget-text ${tabular ? "font-mono-num" : ""}`}
            style={{
              color: color || "var(--text-primary)",
              fontSize,
              fontWeight,
            }}
          >
            {renderedChildren}
          </span>
        );
      }

      if (tag === "progress" || tag === "bar") {
        const rawVal = Number(attrs.value ?? 0);
        const rawMax = Number(attrs.max ?? 100);
        const pct =
          rawMax > 0
            ? Math.max(0, Math.min(100, (rawVal / rawMax) * 100))
            : 0;
        const barBg =
          resolveTextColor(resolveAttrString(attrs, "color")) ||
          resolveBgColor(resolveAttrString(attrs, "background")) ||
          "#16A34A";
        return (
          <div key={key} className="md-widget-progress-track">
            <div
              className="md-widget-progress-fill"
              style={{ width: `${pct}%`, background: barBg }}
            />
          </div>
        );
      }

      if (tag === "callout" || tag === "alert") {
        const toneCls = resolveToneClass(attrs);
        const title = resolveAttrString(attrs, "title", "heading");
        return (
          <div key={key} className={`md-widget-callout border ${toneCls}`}>
            {title && <div className="font-semibold mb-1">{title}</div>}
            <div>{renderedChildren}</div>
          </div>
        );
      }

      if (tag === "row") {
        const gap = resolveSpacing(attrs.gap) ?? "0.625rem";
        const alignItems = resolveAlign(resolveAttrString(attrs, "align")) || "center";
        const justifyContent = resolveJustify(resolveAttrString(attrs, "justify"));
        return (
          <div
            key={key}
            className="md-widget-row"
            style={{
              gap,
              alignItems,
              justifyContent,
            }}
          >
            {renderedChildren}
          </div>
        );
      }

      if (
        tag === "col" ||
        tag === "column" ||
        tag === "stack" ||
        tag === "box" ||
        tag === "card" ||
        tag === "div" ||
        tag === "section"
      ) {
        const isCard = tag === "card";
        const direction =
          resolveAttrString(attrs, "direction")?.toLowerCase() === "row"
            ? "row"
            : "column";
        const hasBorder = isCard || hasBooleanAttr(attrs, "border", "bordered");
        const padding =
          resolveSpacing(attrs.padding ?? attrs.p) ??
          (isCard || hasBorder ? "0.75rem" : undefined);
        const gap =
          resolveSpacing(attrs.gap) ??
          (direction === "column" && (children.length > 1 || isCard)
            ? "0.35rem"
            : undefined);
        const radius =
          resolveRadius(attrs.radius ?? attrs.rounded) ??
          (hasBorder || isCard ? "0.75rem" : undefined);
        const background =
          resolveBgColor(resolveAttrString(attrs, "background", "bg")) ??
          (isCard || hasBorder ? "var(--bg-surface)" : undefined);
        const height = resolveSpacing(attrs.height ?? attrs.h);
        const width = resolveSpacing(attrs.width ?? attrs.w);
        const clip = hasBooleanAttr(attrs, "clip");
        const alignItems = resolveAlign(resolveAttrString(attrs, "align"));
        const justifyContent = resolveJustify(resolveAttrString(attrs, "justify"));

        const rawFlex = attrs.flex;
        const isEmptyBarSegment =
          children.length === 0 && background !== undefined && !hasBorder;
        let flexStyle: string | undefined;
        let minWidthStyle: string | undefined;
        if (rawFlex !== undefined && rawFlex !== false) {
          const flexNum = rawFlex === true ? 1 : Number(rawFlex);
          if (!Number.isNaN(flexNum)) {
            if (isEmptyBarSegment) {
              flexStyle = `${flexNum} 0 0%`;
              minWidthStyle = "0px";
            } else {
              flexStyle = `${flexNum} 1 ${hasBorder || padding ? "150px" : "0px"}`;
              minWidthStyle = hasBorder || padding ? "150px" : "0px";
            }
          } else if (typeof rawFlex === "string") {
            flexStyle = rawFlex;
          }
        }

        return (
          <div
            key={key}
            className="md-widget-box"
            style={{
              display: "flex",
              flexDirection: direction,
              flexWrap: direction === "row" && !height ? "wrap" : undefined,
              flex: flexStyle,
              minWidth: minWidthStyle,
              border: hasBorder ? "1px solid var(--hairline)" : undefined,
              borderRadius: radius,
              padding,
              gap,
              background,
              height,
              width,
              overflow: clip ? "hidden" : undefined,
              alignItems:
                alignItems ?? (isEmptyBarSegment ? "stretch" : undefined),
              justifyContent,
              alignSelf: isEmptyBarSegment ? "stretch" : undefined,
              boxShadow:
                isCard || hasBorder ? "var(--shadow-card)" : undefined,
            }}
          >
            {renderedChildren}
          </div>
        );
      }

      if (tag === "grid") {
        const colsRaw = Number(attrs.columns ?? attrs.cols ?? 0);
        const gap = resolveSpacing(attrs.gap) ?? "0.625rem";
        return (
          <div
            key={key}
            className="md-widget-grid"
            style={{
              display: "grid",
              gap,
              gridTemplateColumns:
                colsRaw > 0
                  ? `repeat(${colsRaw}, minmax(0, 1fr))`
                  : "repeat(auto-fit, minmax(180px, 1fr))",
            }}
          >
            {renderedChildren}
          </div>
        );
      }

      if (tag === "grid-item" || tag === "carousel-item") {
        return (
          <div
            key={key}
            className={`md-widget-box md-widget-grid-item${
              tag === "carousel-item" ? " md-widget-carousel-item" : ""
            }`}
          >
            {renderedChildren}
          </div>
        );
      }

      if (tag === "carousel") {
        return (
          <div key={key} className="md-widget-carousel">
            {renderedChildren}
          </div>
        );
      }

      if (tag === "audio") {
        const src = resolveAttrString(attrs, "src", "url") || "";
        if (!/^(https?:\/\/|blob:|data:audio\/|\/)/i.test(src)) {
          return null;
        }
        return (
          <audio
            key={key}
            controls
            src={src}
            className="w-full max-w-md my-1 rounded-lg"
          />
        );
      }

      if (tag === "asyncimage" || tag === "image" || tag === "img") {
        const src = resolveAttrString(attrs, "src", "url");
        const query = resolveAttrString(attrs, "query", "prompt");
        const alt = resolveAttrString(attrs, "alt", "title") || query || "视觉参考图";
        const aspectRatio = resolveAttrString(attrs, "aspectRatio", "aspect_ratio");
        const width = resolveSpacing(attrs.width ?? attrs.w);

        if (src && /^(https?:\/\/|data:image\/|\/|#)/i.test(src)) {
          return (
            <img
              key={key}
              src={src}
              alt={alt}
              className="md-widget-image"
              style={{
                width: width || "100%",
                aspectRatio: aspectRatio ? aspectRatio.replace(":", " / ") : undefined,
              }}
            />
          );
        }

        return (
          <div
            key={key}
            className="md-widget-async-image"
            style={{ width: width || "100%" }}
          >
            <div className="md-widget-async-image-header">
              <span className="md-widget-async-image-label">
                <svg
                  width="12"
                  height="12"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  aria-hidden="true"
                >
                  <rect width="18" height="18" x="3" y="3" rx="2" ry="2" />
                  <circle cx="9" cy="9" r="2" />
                  <path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21" />
                </svg>
                <span>视觉参考</span>
              </span>
              {aspectRatio && (
                <span className="md-widget-async-image-ratio">{aspectRatio}</span>
              )}
            </div>
            {query && <div className="md-widget-async-image-query">{query}</div>}
          </div>
        );
      }

      if (tag === "details") {
        return (
          <details key={key} className="md-widget-details" open={hasBooleanAttr(attrs, "open")}>
            {renderedChildren}
          </details>
        );
      }
      if (tag === "summary") {
        return <summary key={key} className="cursor-pointer font-medium">{renderedChildren}</summary>;
      }
      if (tag === "b" || tag === "strong") return <strong key={key}>{renderedChildren}</strong>;
      if (tag === "i" || tag === "em") return <em key={key}>{renderedChildren}</em>;
      if (tag === "u") return <u key={key}>{renderedChildren}</u>;
      if (tag === "s" || tag === "del") return <del key={key}>{renderedChildren}</del>;
      if (tag === "code") return <code key={key} className="md-inline-code">{renderedChildren}</code>;
      if (tag === "kbd") return <kbd key={key} className="md-inline-code">{renderedChildren}</kbd>;
      if (tag === "mark") return <mark key={key} className="px-1 rounded">{renderedChildren}</mark>;
      if (tag === "sup") return <sup key={key}>{renderedChildren}</sup>;
      if (tag === "sub") return <sub key={key}>{renderedChildren}</sub>;
      if (tag === "small") return <small key={key}>{renderedChildren}</small>;
      if (tag === "p") return <p key={key}>{renderedChildren}</p>;
      if (tag === "h1") return <h1 key={key}>{renderedChildren}</h1>;
      if (tag === "h2") return <h2 key={key}>{renderedChildren}</h2>;
      if (tag === "h3") return <h3 key={key}>{renderedChildren}</h3>;
      if (tag === "h4") return <h4 key={key}>{renderedChildren}</h4>;
      if (tag === "h5") return <h5 key={key}>{renderedChildren}</h5>;
      if (tag === "h6") return <h6 key={key}>{renderedChildren}</h6>;
      if (tag === "blockquote") return <blockquote key={key}>{renderedChildren}</blockquote>;
      if (tag === "ul") return <ul key={key}>{renderedChildren}</ul>;
      if (tag === "ol") return <ol key={key}>{renderedChildren}</ol>;
      if (tag === "li") return <li key={key}>{renderedChildren}</li>;
      if (tag === "table") {
        return (
          <div key={key} className="md-table-wrap">
            <table>{renderedChildren}</table>
          </div>
        );
      }
      if (tag === "thead") return <thead key={key}>{renderedChildren}</thead>;
      if (tag === "tbody") return <tbody key={key}>{renderedChildren}</tbody>;
      if (tag === "tr") return <tr key={key}>{renderedChildren}</tr>;
      if (tag === "th") return <th key={key}>{renderedChildren}</th>;
      if (tag === "td") return <td key={key}>{renderedChildren}</td>;
      if (tag === "a") {
        const href = resolveAttrString(attrs, "href") || "";
        const safe = /^(https?:\/\/|#\/)/i.test(href);
        if (!safe) return <span key={key}>{renderedChildren}</span>;
        return (
          <a
            key={key}
            href={href}
            target={href.startsWith("#/") ? undefined : "_blank"}
            rel={href.startsWith("#/") ? undefined : "noreferrer"}
            className="md-link"
          >
            {renderedChildren}
          </a>
        );
      }

      return <span key={key}>{renderedChildren}</span>;
    })
    .filter(Boolean);
}

function parseAndRenderWidgetMarkup(
  markup: string,
  keyPrefix: string
): React.ReactNode[] {
  const tokens = tokenizeWidgetMarkup(markup);
  const tree = buildWidgetTree(tokens);
  const hasTopLevelGridItems = tree.some(
    (n) => n.kind === "element" && n.tag === "grid-item"
  );
  if (!hasTopLevelGridItems) {
    return renderWidgetAst(tree, keyPrefix);
  }

  // 将连续的顶层 <grid-item> 自动归组到响应式网格容器中
  const grouped: React.ReactNode[] = [];
  let gridBuf: WidgetAstNode[] = [];
  const flushGrid = (idx: number) => {
    if (gridBuf.length === 0) return;
    grouped.push(
      <div
        key={`${keyPrefix}-autogrid-${idx}`}
        className="md-widget-grid"
        style={{
          display: "grid",
          gap: "0.75rem",
          gridTemplateColumns: "repeat(auto-fit, minmax(200px, 1fr))",
        }}
      >
        {renderWidgetAst(gridBuf, `${keyPrefix}-ag-${idx}`)}
      </div>
    );
    gridBuf = [];
  };

  tree.forEach((node, idx) => {
    if (node.kind === "element" && node.tag === "grid-item") {
      gridBuf.push(node);
    } else if (
      gridBuf.length > 0 &&
      node.kind === "text" &&
      !node.text.trim()
    ) {
      // 忽略相邻 <grid-item> 之间的纯空白文本节点
    } else {
      flushGrid(idx);
      grouped.push(...renderWidgetAst([node], `${keyPrefix}-n-${idx}`));
    }
  });
  flushGrid(tree.length);
  return grouped;
}

function renderInlineMarkdown(
  text: string,
  keyPrefix: string,
  depth = 0,
  allowWidgets = true
): React.ReactNode[] {
  if (depth > 4) {
    return [text];
  }
  if (allowWidgets && hasValidWidgetTagInText(text)) {
    return parseAndRenderWidgetMarkup(text, `${keyPrefix}-w`);
  }
  const safeText = stripUnknownPseudoXmlTags(text);
  const nodes: React.ReactNode[] = [];
  const tokenRe =
    /(`[^`]+`|<br\s*\/?>|\*\*[^*\n]+?\*\*|__[^_\n]+?__|~~[^~\n]+?~~|\*[^*\n]+?\*|!\[[^\]\n]*\]\([^)\s]+\)|\[[^\]\n]+\]\([^)\s]+\)|https?:\/\/[^\s<>)"]+)/gi;
  let last = 0;
  let idx = 0;
  let m: RegExpExecArray | null;

  while ((m = tokenRe.exec(safeText)) !== null) {
    if (m.index > last) {
      nodes.push(safeText.slice(last, m.index));
    }
    const tok = m[0];
    const key = `${keyPrefix}-${idx++}`;

    if (tok.startsWith("`") && tok.endsWith("`")) {
      nodes.push(
        <code key={key} className="md-inline-code">
          {tok.slice(1, -1)}
        </code>
      );
    } else if (/^<br\s*\/?>$/i.test(tok)) {
      nodes.push(<br key={key} />);
    } else if (
      (tok.startsWith("**") && tok.endsWith("**")) ||
      (tok.startsWith("__") && tok.endsWith("__"))
    ) {
      nodes.push(
        <strong key={key}>
          {renderInlineMarkdown(
            tok.slice(2, -2),
            `${key}-b`,
            depth + 1,
            allowWidgets
          )}
        </strong>
      );
    } else if (tok.startsWith("~~") && tok.endsWith("~~")) {
      nodes.push(
        <del key={key} className="opacity-75">
          {renderInlineMarkdown(
            tok.slice(2, -2),
            `${key}-del`,
            depth + 1,
            allowWidgets
          )}
        </del>
      );
    } else if (tok.startsWith("*") && tok.endsWith("*")) {
      nodes.push(
        <em key={key}>
          {renderInlineMarkdown(
            tok.slice(1, -1),
            `${key}-em`,
            depth + 1,
            allowWidgets
          )}
        </em>
      );
    } else if (tok.startsWith("![")) {
      const imgMatch = /^!\[([^\]\n]*)\]\(([^)\s]+)\)$/.exec(tok);
      if (imgMatch) {
        const [, alt, src] = imgMatch;
        const safe = /^(https?:\/\/|data:image\/|\/storage\/)/i.test(src);
        if (safe) {
          nodes.push(<MarkdownImageCard key={key} src={src} alt={alt} />);
        } else {
          nodes.push(tok);
        }
      } else {
        nodes.push(tok);
      }
    } else if (tok.startsWith("[")) {
      const linkMatch = /^\[([^\]\n]+)\]\(([^)\s]+)\)$/.exec(tok);
      if (linkMatch) {
        const [, label, href] = linkMatch;
        const safe = /^(https?:\/\/|#\/|\/storage\/)/i.test(href);
        if (safe) {
          nodes.push(
            <a
              key={key}
              href={href}
              target={href.startsWith("#/") ? undefined : "_blank"}
              rel={href.startsWith("#/") ? undefined : "noreferrer"}
              className="md-link"
            >
              {renderInlineMarkdown(label, `${key}-a`, depth + 1, allowWidgets)}
            </a>
          );
        } else {
          nodes.push(tok);
        }
      } else {
        nodes.push(tok);
      }
    } else if (/^https?:\/\//i.test(tok)) {
      // 保留末尾标点不纳入 URL
      const urlClean = tok.replace(/[.,;:!?)]+$/, "");
      const trail = tok.slice(urlClean.length);
      nodes.push(
        <a
          key={key}
          href={urlClean}
          target="_blank"
          rel="noreferrer"
          className="md-link"
        >
          {urlClean}
        </a>
      );
      if (trail) nodes.push(trail);
    }

    last = m.index + tok.length;
  }

  if (last < safeText.length) {
    nodes.push(safeText.slice(last));
  }
  return nodes;
}

function splitTableRow(line: string): string[] {
  const trimmed = line.trim().replace(/^\|/, "").replace(/\|$/, "");
  return trimmed.split("|").map((cell) => cell.trim());
}

function isTableSeparator(line: string): boolean {
  return /^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)*\|?\s*$/.test(line);
}

const UL_ITEM_RE = /^(\s*)(?:[-*+•])\s+(.*)$/;
const OL_ITEM_RE = /^(\s*)\d+[.)]\s+(.*)$/;

function matchListLine(line: string): {
  kind: "ul" | "ol";
  indent: number;
  text: string;
} | null {
  const ul = UL_ITEM_RE.exec(line);
  if (ul) {
    return { kind: "ul", indent: ul[1].length, text: ul[2] };
  }
  const ol = OL_ITEM_RE.exec(line);
  if (ol) {
    return { kind: "ol", indent: ol[1].length, text: ol[2] };
  }
  return null;
}

function parseListBlock(
  lines: string[],
  startIndex: number,
  keyPrefix: string
): { node: React.ReactNode; nextIndex: number } {
  const first = matchListLine(lines[startIndex]);
  if (!first) {
    return { node: null, nextIndex: startIndex + 1 };
  }
  const baseIndent = first.indent;
  const listKind = first.kind;
  const items: React.ReactNode[] = [];
  let i = startIndex;

  while (i < lines.length) {
    const line = lines[i];
    if (!line.trim()) {
      // 允许列表项之间隔空行，仅当下一非空行仍是同缩进同类型列表项
      let look = i + 1;
      while (look < lines.length && !lines[look].trim()) look++;
      const nextItem = look < lines.length ? matchListLine(lines[look]) : null;
      if (nextItem && nextItem.indent === baseIndent && nextItem.kind === listKind) {
        i = look;
        continue;
      }
      break;
    }

    const parsed = matchListLine(line);
    if (!parsed || parsed.indent < baseIndent) {
      break;
    }
    if (parsed.indent === baseIndent && parsed.kind !== listKind) {
      break;
    }

    const itemIdx = items.length;
    const rawText = parsed.text.trim();
    const taskMatch = /^\[( |x|X)\]\s+(.*)$/.exec(rawText);
    const bodyNode = taskMatch ? (
      <span className="md-task-item">
        <span
          className={`md-task-box ${taskMatch[1].toLowerCase() === "x" ? "is-checked" : ""}`}
          aria-hidden="true"
        >
          {taskMatch[1].toLowerCase() === "x" ? "✓" : ""}
        </span>
        <span>
          {renderInlineMarkdown(taskMatch[2], `${keyPrefix}-li-${itemIdx}`)}
        </span>
      </span>
    ) : (
      renderInlineMarkdown(rawText, `${keyPrefix}-li-${itemIdx}`)
    );

    i++;
    let childList: React.ReactNode = null;
    if (i < lines.length) {
      const sub = matchListLine(lines[i]);
      if (sub && sub.indent > baseIndent) {
        const res = parseListBlock(lines, i, `${keyPrefix}-sub-${itemIdx}`);
        childList = res.node;
        i = res.nextIndex;
      }
    }

    items.push(
      <li key={itemIdx}>
        {bodyNode}
        {childList}
      </li>
    );
  }

  const ListTag = listKind === "ol" ? "ol" : "ul";
  return {
    node: <ListTag key={keyPrefix}>{items}</ListTag>,
    nextIndex: i,
  };
}

function classifyBlockquoteTone(quoteLines: string[]): string {
  const first = (quoteLines[0] || "").trim();
  if (/^(?:⚠️|警告|注意|告警|warning)/i.test(first)) return "is-warn";
  if (/^(?:❌|🚨|严重|error|critical|失败)/i.test(first)) return "is-danger";
  if (/^(?:✅|✔|成功|healthy|已恢复)/i.test(first)) return "is-ok";
  if (/^(?:💡|ℹ️|提示|建议|tip|note)/i.test(first)) return "is-info";
  return "";
}

const WIDGET_BLOCK_PRIORITY_TAGS = new Set([
  ...BLOCK_WIDGET_TAGS,
  "badge",
  "pill",
  "chip",
  "tag",
  "status",
  "title",
  "metric",
  "stat",
  "progress",
  "bar",
]);

function isMarkdownBlockBoundary(lines: string[], idx: number): boolean {
  if (idx >= lines.length) return true;
  const line = lines[idx];
  const trimmed = line.trim();
  if (!trimmed) return false;
  if (/^(`{3,}|~{3,})/.test(trimmed)) return true;
  if (/^#{1,6}\s+/.test(trimmed)) return true;
  if (/^(-{3,}|\*{3,}|_{3,})$/.test(trimmed)) return true;
  if (trimmed.startsWith(">")) return true;
  if (matchListLine(line) !== null) return true;
  if (
    line.includes("|") &&
    idx + 1 < lines.length &&
    isTableSeparator(lines[idx + 1])
  ) {
    return true;
  }
  return false;
}

function shouldStartWidgetBlock(trimmedLine: string): boolean {
  if (!trimmedLine.startsWith("<")) return false;
  const tag = tryParseTagAt(trimmedLine, 0);
  if (!tag || tag.kind === "close") return false;
  return WIDGET_BLOCK_PRIORITY_TAGS.has(tag.tag);
}

function containsBlockLevelWidgetTag(text: string): boolean {
  if (!MAYBE_BLOCK_WIDGET_RE.test(text)) return false;
  return tokenizeWidgetMarkup(text).some(
    (t) => t.type !== "text" && BLOCK_WIDGET_TAGS.has(t.tag)
  );
}

function collectWidgetBlockLines(
  lines: string[],
  startIndex: number
): { chunk: string; nextIndex: number } {
  let depth = 0;
  let j = startIndex;

  while (j < lines.length) {
    const cur = lines[j];
    const curTrim = cur.trim();

    if (j > startIndex) {
      if (!curTrim) {
        if (depth <= 0) {
          let nextNonEmpty = j + 1;
          while (nextNonEmpty < lines.length && !lines[nextNonEmpty].trim()) {
            nextNonEmpty++;
          }
          if (
            nextNonEmpty < lines.length &&
            /^<(?:grid-item|carousel-item)(?:\s|>)/i.test(lines[nextNonEmpty].trim())
          ) {
            j = nextNonEmpty;
            continue;
          }
          break;
        }
        let look = j + 1;
        while (look < lines.length && !lines[look].trim()) look++;
        if (look >= lines.length || isMarkdownBlockBoundary(lines, look)) {
          break;
        }
        // 仅当后续直至下一个 Markdown 块边界前都不存在任何闭合/widget 标签时才提前结束
        let hasClosingOrWidgetAhead = false;
        for (let scan = look; scan < lines.length; scan++) {
          const scanTrim = lines[scan].trim();
          if (!scanTrim) continue;
          if (isMarkdownBlockBoundary(lines, scan)) break;
          if (hasValidWidgetTagInText(scanTrim) || isOrphanClosingWidgetLine(scanTrim)) {
            hasClosingOrWidgetAhead = true;
            break;
          }
        }
        if (!hasClosingOrWidgetAhead) {
          break;
        }
        j++;
        continue;
      }

      if (isMarkdownBlockBoundary(lines, j)) {
        break;
      }

      if (depth <= 0 && !/^<(?:grid-item|carousel-item)(?:\s|>)/i.test(curTrim)) {
        break;
      }
    }

    const tokens = tokenizeWidgetMarkup(cur);
    for (const tok of tokens) {
      if (tok.type === "open" && !VOID_WIDGET_TAGS.has(tok.tag)) {
        depth++;
      } else if (tok.type === "close") {
        depth = Math.max(0, depth - 1);
      }
    }
    j++;
    if (depth <= 0) {
      let nextNonEmpty = j;
      while (nextNonEmpty < lines.length && !lines[nextNonEmpty].trim()) {
        nextNonEmpty++;
      }
      if (
        nextNonEmpty < lines.length &&
        /^<(?:grid-item|carousel-item)(?:\s|>)/i.test(lines[nextNonEmpty].trim())
      ) {
        continue;
      }
      break;
    }
  }

  return {
    chunk: lines.slice(startIndex, j).join("\n"),
    nextIndex: j,
  };
}

export const MarkdownContent: React.FC<{ content: string }> = ({ content }) => {
  const lines = content.replace(/\r\n/g, "\n").split("\n");
  const blocks: React.ReactNode[] = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];
    const trimmed = line.trim();

    if (!trimmed) {
      i++;
      continue;
    }

    // 忽略被 Markdown 块边界切断后遗留的孤立闭合标签（如表格后的 </box> 或 </CodeBlock>）
    if (isOrphanClosingWidgetLine(trimmed) || /^<\/CodeBlock\s*>$/i.test(trimmed)) {
      i++;
      continue;
    }

    // XML/JSX 风格 <CodeBlock language="...">...</CodeBlock> 或 <pre>...</pre> 代码块兜底
    const xmlCodeOpen = /^<(CodeBlock|pre)(?:\s+([^>]*))?>$/i.exec(trimmed);
    if (xmlCodeOpen) {
      const closingTag = xmlCodeOpen[1].toLowerCase();
      const attrRaw = xmlCodeOpen[2] || "";
      const langMatch = /(?:language|lang)\s*=\s*["']([^"']+)["']/i.exec(attrRaw);
      const lang = langMatch ? langMatch[1] : "";
      const codeLines: string[] = [];
      i++;
      while (i < lines.length) {
        if (lines[i].trim().toLowerCase() === `</${closingTag}>`) {
          i++;
          break;
        }
        codeLines.push(lines[i]);
        i++;
      }
      blocks.push(
        <div key={`pre-${blocks.length}`} className="md-code-wrap">
          {lang && <div className="md-code-lang">{lang}</div>}
          <pre className="md-code-block" data-lang={lang}>
            <code>{codeLines.join("\n")}</code>
          </pre>
        </div>
      );
      continue;
    }

    // Fenced code block (``` or ~~~)
    const fenceOpen = /^(`{3,}|~{3,})(.*)$/.exec(trimmed);
    if (fenceOpen) {
      const fenceMark = fenceOpen[1];
      const lang = fenceOpen[2].trim();
      const codeLines: string[] = [];
      i++;
      while (i < lines.length) {
        if (lines[i].trim().startsWith(fenceMark)) {
          i++;
          break;
        }
        codeLines.push(lines[i]);
        i++;
      }
      blocks.push(
        <div key={`pre-${blocks.length}`} className="md-code-wrap">
          {lang && <div className="md-code-lang">{lang}</div>}
          <pre className="md-code-block" data-lang={lang}>
            <code>{codeLines.join("\n")}</code>
          </pre>
        </div>
      );
      continue;
    }

    // Horizontal rule
    if (/^(-{3,}|\*{3,}|_{3,})$/.test(trimmed)) {
      blocks.push(
        <hr key={`hr-${blocks.length}`} className="md-hr" />
      );
      i++;
      continue;
    }

    // Widget / JSX markup block (<row>, <box>, <card>, <badge>, <title>, <callout>, etc.)
    if (shouldStartWidgetBlock(trimmed)) {
      const { chunk, nextIndex } = collectWidgetBlockLines(lines, i);
      blocks.push(
        <div key={`wb-${blocks.length}`} className="md-widget-block">
          {parseAndRenderWidgetMarkup(chunk, `wb-${blocks.length}`)}
        </div>
      );
      i = nextIndex;
      continue;
    }

    // Headings h1-h6
    const headingMatch = /^(#{1,6})\s+(.+)$/.exec(trimmed);
    if (headingMatch) {
      const level = headingMatch[1].length;
      const cleanHeading = headingMatch[2].replace(/\s+#+\s*$/, "");
      const body = renderInlineMarkdown(cleanHeading, `h-${blocks.length}`);
      if (level === 1) blocks.push(<h1 key={`h-${blocks.length}`}>{body}</h1>);
      else if (level === 2)
        blocks.push(<h2 key={`h-${blocks.length}`}>{body}</h2>);
      else if (level === 3)
        blocks.push(<h3 key={`h-${blocks.length}`}>{body}</h3>);
      else if (level === 4)
        blocks.push(<h4 key={`h-${blocks.length}`}>{body}</h4>);
      else if (level === 5)
        blocks.push(<h5 key={`h-${blocks.length}`}>{body}</h5>);
      else blocks.push(<h6 key={`h-${blocks.length}`}>{body}</h6>);
      i++;
      continue;
    }

    // Markdown table
    if (
      line.includes("|") &&
      i + 1 < lines.length &&
      isTableSeparator(lines[i + 1])
    ) {
      const headers = splitTableRow(line);
      i += 2;
      const rows: string[][] = [];
      while (
        i < lines.length &&
        lines[i].includes("|") &&
        lines[i].trim() !== ""
      ) {
        rows.push(splitTableRow(lines[i]));
        i++;
      }
      blocks.push(
        <div key={`tbl-${blocks.length}`} className="md-table-wrap">
          <table>
            <thead>
              <tr>
                {headers.map((h, hi) => (
                  <th key={hi}>
                    {renderInlineMarkdown(h, `th-${blocks.length}-${hi}`)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row, ri) => (
                <tr key={ri}>
                  {headers.map((_, ci) => (
                    <td key={ci}>
                      {renderInlineMarkdown(
                        row[ci] ?? "",
                        `td-${blocks.length}-${ri}-${ci}`
                      )}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
      continue;
    }

    // Blockquote
    if (trimmed.startsWith(">")) {
      const quoteLines: string[] = [];
      while (i < lines.length && lines[i].trim().startsWith(">")) {
        quoteLines.push(lines[i].trim().replace(/^>\s?/, ""));
        i++;
      }
      const tone = classifyBlockquoteTone(quoteLines);
      blocks.push(
        <blockquote
          key={`bq-${blocks.length}`}
          className={tone ? `md-quote-${tone}` : undefined}
        >
          {quoteLines
            .filter((ql) => ql.trim() !== "")
            .map((ql, qi) => (
              <p key={qi}>
                {renderInlineMarkdown(ql, `bq-${blocks.length}-${qi}`)}
              </p>
            ))}
        </blockquote>
      );
      continue;
    }

    // Unordered & Ordered lists (supporting nested sub-lists and task items)
    if (matchListLine(line)) {
      const res = parseListBlock(lines, i, `list-${blocks.length}`);
      if (res.node) {
        blocks.push(res.node);
      }
      i = res.nextIndex;
      continue;
    }

    // Paragraph (consume consecutive plain lines)
    const paraLines: string[] = [];
    while (i < lines.length) {
      const cur = lines[i];
      const curTrim = cur.trim();
      if (
        !curTrim ||
        /^(`{3,}|~{3,})/.test(curTrim) ||
        curTrim.startsWith(">") ||
        /^#{1,6}\s+/.test(curTrim) ||
        matchListLine(cur) !== null ||
        /^(-{3,}|\*{3,}|_{3,})$/.test(curTrim) ||
        /^<\/?(?:CodeBlock|pre)(?:\s|>)/i.test(curTrim) ||
        shouldStartWidgetBlock(curTrim) ||
        isOrphanClosingWidgetLine(curTrim)
      ) {
        break;
      }
      if (
        cur.includes("|") &&
        i + 1 < lines.length &&
        isTableSeparator(lines[i + 1])
      ) {
        break;
      }
      paraLines.push(curTrim);
      i++;
    }
    const paraText = paraLines.join(" ");
    if (containsBlockLevelWidgetTag(paraText)) {
      blocks.push(
        <div key={`p-${blocks.length}`} className="md-widget-block">
          {renderInlineMarkdown(paraText, `p-${blocks.length}`)}
        </div>
      );
    } else {
      blocks.push(
        <p key={`p-${blocks.length}`}>
          {renderInlineMarkdown(paraText, `p-${blocks.length}`)}
        </p>
      );
    }
  }

  return <div className="copilot-md text-xs">{blocks}</div>;
};
