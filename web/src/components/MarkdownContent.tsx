import React from "react";

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
    return segments;
  }

  // 2. 无 ```openui 围栏时，自动拆分混排或纯 DSL，否则一律交由 Markdown 渲染
  return splitUnfencedOpenUI(content);
}

function renderInlineMarkdown(
  text: string,
  keyPrefix: string,
  depth = 0
): React.ReactNode[] {
  if (depth > 4) {
    return [text];
  }
  const nodes: React.ReactNode[] = [];
  const tokenRe =
    /(`[^`]+`|<br\s*\/?>|\*\*[^*\n]+?\*\*|__[^_\n]+?__|~~[^~\n]+?~~|\*[^*\n]+?\*|\[[^\]\n]+\]\([^)\s]+\)|https?:\/\/[^\s<>)"]+)/gi;
  let last = 0;
  let idx = 0;
  let m: RegExpExecArray | null;

  while ((m = tokenRe.exec(text)) !== null) {
    if (m.index > last) {
      nodes.push(text.slice(last, m.index));
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
          {renderInlineMarkdown(tok.slice(2, -2), `${key}-b`, depth + 1)}
        </strong>
      );
    } else if (tok.startsWith("~~") && tok.endsWith("~~")) {
      nodes.push(
        <del key={key} className="opacity-75">
          {renderInlineMarkdown(tok.slice(2, -2), `${key}-del`, depth + 1)}
        </del>
      );
    } else if (tok.startsWith("*") && tok.endsWith("*")) {
      nodes.push(
        <em key={key}>
          {renderInlineMarkdown(tok.slice(1, -1), `${key}-em`, depth + 1)}
        </em>
      );
    } else if (tok.startsWith("[")) {
      const linkMatch = /^\[([^\]\n]+)\]\(([^)\s]+)\)$/.exec(tok);
      if (linkMatch) {
        const [, label, href] = linkMatch;
        const safe = /^(https?:\/\/|#\/)/i.test(href);
        if (safe) {
          nodes.push(
            <a
              key={key}
              href={href}
              target={href.startsWith("#/") ? undefined : "_blank"}
              rel={href.startsWith("#/") ? undefined : "noreferrer"}
              className="md-link"
            >
              {renderInlineMarkdown(label, `${key}-a`, depth + 1)}
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

  if (last < text.length) {
    nodes.push(text.slice(last));
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
        /^(-{3,}|\*{3,}|_{3,})$/.test(curTrim)
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
    blocks.push(
      <p key={`p-${blocks.length}`}>
        {renderInlineMarkdown(paraLines.join(" "), `p-${blocks.length}`)}
      </p>
    );
  }

  return <div className="copilot-md text-xs">{blocks}</div>;
};
