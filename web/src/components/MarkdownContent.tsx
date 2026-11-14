import React from "react";

export type HybridSegment =
  | { type: "markdown"; text: string }
  | { type: "openui"; code: string };

const OPENUI_FENCE_RE = /```(?:openui-lang|openui)\s*\n([\s\S]*?)(?:\n?```|$)/gi;

export function splitHybridSegments(
  content: string,
  legacyOpenUIMode = false
): HybridSegment[] {
  const trimmed = content.trim();
  if (!trimmed) {
    return [];
  }

  // 先检测内联 ```openui / ```openui-lang 代码块
  OPENUI_FENCE_RE.lastIndex = 0;
  const segments: HybridSegment[] = [];
  let lastIdx = 0;
  let match: RegExpExecArray | null;
  while ((match = OPENUI_FENCE_RE.exec(content)) !== null) {
    const before = content.slice(lastIdx, match.index).trim();
    if (before) {
      segments.push({ type: "markdown", text: before });
    }
    const code = (match[1] || "").trim();
    if (code) {
      segments.push({ type: "openui", code });
    }
    lastIdx = match.index + match[0].length;
  }

  if (segments.length > 0) {
    const tail = content.slice(lastIdx).trim();
    if (tail) {
      segments.push({ type: "markdown", text: tail });
    }
    return segments;
  }

  // 若整条消息本身就是 OpenUI Lang 程序（如 root = Card(...) 或纯 openuiMode 消息）
  const looksLikeFullOpenUI =
    /^root\s*=\s*[A-Za-z0-9_]+\(/m.test(trimmed) &&
    !/^#{1,4}\s/m.test(trimmed);

  if (legacyOpenUIMode || looksLikeFullOpenUI) {
    const cleaned = trimmed
      .replace(/^```(?:openui-lang|openui)?\s*\n?/i, "")
      .replace(/\n?```\s*$/i, "");
    return [{ type: "openui", code: cleaned }];
  }

  return [{ type: "markdown", text: content }];
}

function renderInlineMarkdown(text: string, keyPrefix: string): React.ReactNode[] {
  const nodes: React.ReactNode[] = [];
  const tokenRe =
    /(`[^`]+`|\*\*[^*\n]+\*\*|\*[^*\n]+\*|\[[^\]\n]+\]\([^)\s]+\))/g;
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
    } else if (tok.startsWith("**") && tok.endsWith("**")) {
      nodes.push(<strong key={key}>{tok.slice(2, -2)}</strong>);
    } else if (tok.startsWith("*") && tok.endsWith("*")) {
      nodes.push(<em key={key}>{tok.slice(1, -1)}</em>);
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
              className="underline underline-offset-2"
            >
              {label}
            </a>
          );
        } else {
          nodes.push(tok);
        }
      } else {
        nodes.push(tok);
      }
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
  return /^\s*\|?(\s*:?-{3,}:?\s*\|)+\s*:?-{3,}:?\s*\|?\s*$/.test(line);
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

    // Fenced code block
    if (trimmed.startsWith("```")) {
      const lang = trimmed.slice(3).trim();
      const codeLines: string[] = [];
      i++;
      while (i < lines.length && !lines[i].trim().startsWith("```")) {
        codeLines.push(lines[i]);
        i++;
      }
      if (i < lines.length) i++;
      blocks.push(
        <pre key={`pre-${blocks.length}`} className="md-code-block" data-lang={lang}>
          <code>{codeLines.join("\n")}</code>
        </pre>
      );
      continue;
    }

    // Horizontal rule
    if (/^(-{3,}|\*{3,}|_{3,})$/.test(trimmed)) {
      blocks.push(<hr key={`hr-${blocks.length}`} className="my-2 border-t z-border" />);
      i++;
      continue;
    }

    // Headings
    const headingMatch = /^(#{1,4})\s+(.+)$/.exec(trimmed);
    if (headingMatch) {
      const level = headingMatch[1].length;
      const body = renderInlineMarkdown(headingMatch[2], `h-${blocks.length}`);
      if (level === 1) blocks.push(<h1 key={`h-${blocks.length}`}>{body}</h1>);
      else if (level === 2) blocks.push(<h2 key={`h-${blocks.length}`}>{body}</h2>);
      else if (level === 3) blocks.push(<h3 key={`h-${blocks.length}`}>{body}</h3>);
      else blocks.push(<h4 key={`h-${blocks.length}`}>{body}</h4>);
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
      while (i < lines.length && lines[i].includes("|") && lines[i].trim() !== "") {
        rows.push(splitTableRow(lines[i]));
        i++;
      }
      blocks.push(
        <div key={`tbl-${blocks.length}`} className="overflow-x-auto">
          <table>
            <thead>
              <tr>
                {headers.map((h, hi) => (
                  <th key={hi}>{renderInlineMarkdown(h, `th-${blocks.length}-${hi}`)}</th>
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
      blocks.push(
        <blockquote key={`bq-${blocks.length}`}>
          {quoteLines.map((ql, qi) => (
            <p key={qi}>{renderInlineMarkdown(ql, `bq-${blocks.length}-${qi}`)}</p>
          ))}
        </blockquote>
      );
      continue;
    }

    // Unordered list
    if (/^[-*+]\s+/.test(trimmed)) {
      const items: string[] = [];
      while (i < lines.length && /^[-*+]\s+/.test(lines[i].trim())) {
        items.push(lines[i].trim().replace(/^[-*+]\s+/, ""));
        i++;
      }
      blocks.push(
        <ul key={`ul-${blocks.length}`}>
          {items.map((it, ii) => (
            <li key={ii}>{renderInlineMarkdown(it, `ul-${blocks.length}-${ii}`)}</li>
          ))}
        </ul>
      );
      continue;
    }

    // Ordered list
    if (/^\d+\.\s+/.test(trimmed)) {
      const items: string[] = [];
      while (i < lines.length && /^\d+\.\s+/.test(lines[i].trim())) {
        items.push(lines[i].trim().replace(/^\d+\.\s+/, ""));
        i++;
      }
      blocks.push(
        <ol key={`ol-${blocks.length}`}>
          {items.map((it, ii) => (
            <li key={ii}>{renderInlineMarkdown(it, `ol-${blocks.length}-${ii}`)}</li>
          ))}
        </ol>
      );
      continue;
    }

    // Paragraph (consume consecutive plain lines)
    const paraLines: string[] = [];
    while (i < lines.length) {
      const cur = lines[i];
      const curTrim = cur.trim();
      if (
        !curTrim ||
        curTrim.startsWith("```") ||
        curTrim.startsWith(">") ||
        /^#{1,4}\s+/.test(curTrim) ||
        /^[-*+]\s+/.test(curTrim) ||
        /^\d+\.\s+/.test(curTrim) ||
        /^(-{3,}|\*{3,}|_{3,})$/.test(curTrim)
      ) {
        break;
      }
      if (cur.includes("|") && i + 1 < lines.length && isTableSeparator(lines[i + 1])) {
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
