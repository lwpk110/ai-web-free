import {
  CompositeAttachmentAdapter,
  SimpleImageAttachmentAdapter,
  type AttachmentAdapter,
  type CompleteAttachment,
  type PendingAttachment,
} from "@assistant-ui/react";
import { uploadOpenAIFile } from "../../api/client";

const TEXT_FILE_EXTENSIONS = new Set([
  "csv",
  "tsv",
  "json",
  "jsonl",
  "txt",
  "md",
  "markdown",
  "log",
  "yaml",
  "yml",
  "xml",
  "sql",
  "py",
  "ts",
  "tsx",
  "js",
  "jsx",
  "go",
  "html",
  "css",
  "sh",
  "env",
  "toml",
  "ini",
]);

const MAX_INLINE_TEXT_CHARS = 32000;

function getFileExt(name: string): string {
  const idx = name.lastIndexOf(".");
  if (idx === -1) return "";
  return name.slice(idx + 1).toLowerCase();
}

function isReadableTextFile(file: File): boolean {
  if (
    file.type.startsWith("text/") ||
    file.type === "application/json" ||
    file.type === "application/xml" ||
    file.type === "text/csv" ||
    file.type === "application/csv"
  ) {
    return true;
  }
  return TEXT_FILE_EXTENSIONS.has(getFileExt(file.name));
}

function summarizeCsvPreview(name: string, raw: string): string {
  const lines = raw.replace(/\r\n/g, "\n").split("\n").filter((l) => l.trim().length > 0);
  const totalRows = Math.max(0, lines.length - 1);
  const header = lines[0] || "";
  const clipped = raw.length > MAX_INLINE_TEXT_CHARS ? `${raw.slice(0, MAX_INLINE_TEXT_CHARS)}\n... (已截断，仅保留前 ${MAX_INLINE_TEXT_CHARS} 字符)` : raw;
  return [
    `<attachment name="${name}" kind="tabular" total_rows="${totalRows}" header="${header.slice(0, 240)}">`,
    clipped,
    `</attachment>`,
  ].join("\n");
}

function formatTextAttachmentPrompt(file: File, rawText: string): string {
  const ext = getFileExt(file.name);
  if (ext === "csv" || ext === "tsv") {
    return summarizeCsvPreview(file.name, rawText);
  }
  const clipped =
    rawText.length > MAX_INLINE_TEXT_CHARS
      ? `${rawText.slice(0, MAX_INLINE_TEXT_CHARS)}\n... (文件共 ${rawText.length} 字符，已截断)`
      : rawText;
  return [
    `<attachment name="${file.name}" mime="${file.type || "text/plain"}" bytes="${file.size}">`,
    clipped,
    `</attachment>`,
  ].join("\n");
}

/**
 * 混合多模态附件适配器：
 * 1. 图片交由 SimpleImageAttachmentAdapter（data URL -> image_url）；
 * 2. CSV/TSV/JSON/TXT/日志/代码文件在浏览器端直接解析为结构化文本上下文，
 *    即使网关当前仅有匿名 noauth 账号也可零阻碍分析并渲染 OpenUI 看板；
 * 3. PDF/Office 等二进制文档在有登录态账号时自动上传至 POST /v1/files 获取 file_id，
 *    若无登录态账号则给出明确可理解的回退提示。
 */
export class HybridDocumentAttachmentAdapter implements AttachmentAdapter {
  public readonly accept =
    ".csv,.tsv,.json,.jsonl,.txt,.md,.log,.yaml,.yml,.xml,.sql,.py,.ts,.tsx,.js,.go,.pdf,.xlsx,.xls,.docx,text/*,application/json,application/pdf";

  private readonly getApiKey: () => string | undefined;

  constructor(getApiKey: () => string | undefined) {
    this.getApiKey = getApiKey;
  }

  async add({ file }: { file: File }): Promise<PendingAttachment> {
    return {
      id: `att-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
      type: "document",
      name: file.name,
      contentType: file.type || "application/octet-stream",
      file,
      status: { type: "requires-action", reason: "composer-send" },
    };
  }

  async send(
    attachment: PendingAttachment,
    options?: { signal?: AbortSignal }
  ): Promise<CompleteAttachment> {
    const file = attachment.file;
    if (isReadableTextFile(file)) {
      const rawText = await file.text();
      return {
        id: attachment.id,
        type: "document",
        name: attachment.name,
        contentType: attachment.contentType,
        status: { type: "complete" },
        content: [
          {
            type: "text",
            text: formatTextAttachmentPrompt(file, rawText),
          },
        ],
      };
    }

    // 二进制文件（如 PDF / Excel / Word）尝试调用 /v1/files 上传到 ChatGPT 会话文件库
    try {
      const uploaded = await uploadOpenAIFile(file, this.getApiKey(), options?.signal);
      return {
        id: attachment.id,
        type: "file",
        name: attachment.name,
        contentType: attachment.contentType,
        status: { type: "complete" },
        content: [
          {
            type: "file",
            filename: uploaded.filename || file.name,
            mimeType: file.type || "application/octet-stream",
            data: uploaded.id,
            sourceType: "id",
          },
        ],
      };
    } catch (err) {
      const reason = err instanceof Error ? err.message : String(err);
      return {
        id: attachment.id,
        type: "document",
        name: attachment.name,
        contentType: attachment.contentType,
        status: { type: "complete" },
        content: [
          {
            type: "text",
            text: `<attachment name="${file.name}" status="upload_unavailable" bytes="${file.size}">[提示：二进制文件上传受限（${reason}）。若当前号池仅含匿名 noauth 账号，请将表格导出为 CSV/JSON 或直接粘贴内容进行分析。]</attachment>`,
          },
        ],
      };
    }
  }

  async remove(): Promise<void> {
    // 浏览器内存附件无需远程清理
  }
}

export function createChatAttachmentAdapter(
  getApiKey: () => string | undefined
): CompositeAttachmentAdapter {
  return new CompositeAttachmentAdapter([
    new SimpleImageAttachmentAdapter(),
    new HybridDocumentAttachmentAdapter(getApiKey),
  ]);
}
