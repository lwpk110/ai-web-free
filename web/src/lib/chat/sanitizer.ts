import { splitHybridSegments } from "../../components/MarkdownContent";
import type { OpenAIChatContentPart } from "../../api/client";
import { sanitizeOpenUIHtmlText } from "../openui-html-sanitizer";

export interface SanitizedLLMMessage {
  role: string;
  content: string | OpenAIChatContentPart[];
}

/**
 * 折叠历史轮次中冗长的 OpenUI Lang DSL 代码块，同时清洗残余 HTML/伪 XML 标签，
 * 既避免上下文膨胀，又阻断多轮对话中的错误标签 Few-Shot 传染。
 */
export function foldHistoricalOpenUIContent(raw: string): string {
  const segments = splitHybridSegments(raw);
  if (segments.length === 0) return sanitizeOpenUIHtmlText(raw, "markdown");

  return segments
    .map((seg) => {
      if (seg.type === "markdown") {
        return sanitizeOpenUIHtmlText(seg.text, "markdown");
      }
      const lines = seg.code
        .split("\n")
        .map((l) => l.trim())
        .filter(Boolean);
      const rootLine = lines.find((l) => l.startsWith("root"));
      const compMatches = Array.from(
        new Set(
          lines
            .map((l) => /=\s*([A-Z][A-Za-z0-9_]*)\s*\(/.exec(l)?.[1])
            .filter((name): name is string => Boolean(name))
        )
      );
      const summary = compMatches.length
        ? compMatches.join(", ")
        : rootLine || "OpenUI Card";
      return `[已渲染交互式 OpenUI 组件: ${summary}]`;
    })
    .filter(Boolean)
    .join("\n\n");
}

export function sanitizeMessagesForLLM(
  messages: SanitizedLLMMessage[],
  keepLastCount = 2
): SanitizedLLMMessage[] {
  const cutoff = Math.max(0, messages.length - keepLastCount);
  return messages.map((msg, idx) => {
    if (msg.role !== "assistant" || typeof msg.content !== "string") {
      return msg;
    }
    // 即使在保留的最近助手轮次中，也清洗伪标签，防止 Few-Shot 模仿传染
    if (idx >= cutoff) {
      return {
        ...msg,
        content: sanitizeOpenUIHtmlText(msg.content, "markdown"),
      };
    }
    return {
      ...msg,
      content: foldHistoricalOpenUIContent(msg.content),
    };
  });
}
