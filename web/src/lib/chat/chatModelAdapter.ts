import type {
  ChatModelAdapter,
  ChatModelRunOptions,
  ChatModelRunResult,
  ThreadMessage,
} from "@assistant-ui/react";
import {
  crawlMedia,
  editImageWithReferences,
  fetchDiagnostics,
  generateImage,
  streamChatCompletion,
  synthesizeSpeech,
  type CrawlerMediaKind,
  type DiagnosticsReport,
  type OpenAIChatContentPart,
} from "../../api/client";
import { containsOpenUIDSL } from "../../components/MarkdownContent";
import { getProgressiveOpenUISystemPrompt } from "../openui-library";
import { sanitizeOpenUIHtmlText } from "../openui-html-sanitizer";
import {
  DEFAULT_IMAGE_MODELS,
  DEFAULT_TTS_MODELS,
  extractPromptFallback,
  extractSpeechFallback,
  isImageModelId,
  isTtsModelId,
  resolveSemanticModelRoute,
  type SemanticRouteLLMResult,
} from "../model-router";
import { base64ToBlob, saveUnifiedImage, type CopilotSkill } from "../storage";
import { sanitizeMessagesForLLM } from "./sanitizer";

export interface ChatRuntimeConfig {
  model: string;
  openuiMode: boolean;
  autoSwitchModel: boolean;
  systemPrompt?: string;
  apiKey?: string;
  availableChatModels: string[];
  availableImageModels: string[];
  availableAudioModels: string[];
}

export interface ChatMessageCustomMeta {
  model?: string;
  openuiMode?: boolean;
  skill?: CopilotSkill;
  latencyMs?: number;
  isOfflineFallback?: boolean;
}

const OPS_INTENT_RE =
  /(诊断|熔断|封禁|停用|风控|拦截|403|401|429|号池|代理|报错|异常|不可用|失败|为什么挂|挂了|续期|卡密|测活|修复|自愈|健康|banned|expired|failover|waf|cloudflare|sentinel|turnstile|proxy|pool|health)/i;
const IMAGE_INTENT_RE = /(生图卡|image\s*studio|画一张|生成图片|文生图|生图工作台)/i;
const OPENUI_INTENT_RE = /(openui|交互卡片|交互式|仪表盘|看板|可视化|饼图|柱状图|折线图)/i;
const MEDIA_CRAWL_INTENT_RE =
  /((爬虫|爬取|搜集|抓取|搜一个|找一个|找合适|采集).*(音色|声音|配音|克隆|声线|参考图|立绘|剧照|视频|素材))|((音色|参考图|视频素材).*(缓存|克隆|入库|系统库|复用))/i;

function formatSingleLinePrompt(text: string, maxLen: number, stripBrackets = false): string {
  let normalized = text.replace(/\s+/g, " ").trim();
  if (stripBrackets) normalized = normalized.replace(/[[\]]/g, "");
  if (!normalized) return "";
  if (normalized.length <= maxLen) return normalized;
  return `${normalized.slice(0, maxLen).trimEnd()}...`;
}

export function generateSmartTitle(prompt: string): string {
  let clean = prompt.trim();
  clean = clean.replace(/^\/(image|diagnose|fix|openui|voice|crawl|clear)\s*/i, "");
  clean = clean.replace(
    /^(gpt-image-[^\s]+|dall-e-[^\s]+|flux-[^\s]+|wanx-[^\s]+|jimeng-[^\s]+|auto)\s*/i,
    ""
  );
  clean = clean.replace(
    /^(以当前模型最大的能力|请帮我|帮我|请生成|请绘制|生成一个|生成一张|制作一个|设计一个|画一个|画一张)\s*/i,
    ""
  );
  clean = clean.trim();
  if (!clean) return "新建对话";
  const runes = Array.from(clean);
  if (runes.length > 20) {
    return runes.slice(0, 20).join("") + "...";
  }
  return clean;
}

function extractMessagePlainText(msg: ThreadMessage): string {
  const parts: string[] = [];
  for (const part of msg.content) {
    if (part.type === "text" && part.text) {
      parts.push(part.text);
    }
  }
  if (msg.role === "user" && msg.attachments?.length) {
    for (const att of msg.attachments) {
      if (att.status.type !== "complete") continue;
      for (const p of att.content) {
        if (p.type === "text" && p.text) {
          parts.push(p.text);
        }
      }
    }
  }
  return parts.join("\n").trim();
}

function extractMessageImageUrls(msg: ThreadMessage): string[] {
  const urls: string[] = [];
  for (const part of msg.content) {
    if (part.type === "image" && part.image) {
      urls.push(part.image);
    }
  }
  if (msg.role === "user" && msg.attachments?.length) {
    for (const att of msg.attachments) {
      if (att.status.type !== "complete") continue;
      for (const p of att.content) {
        if (p.type === "image" && p.image) {
          urls.push(p.image);
        }
      }
    }
  }
  return urls;
}

function toOpenAIMessageContent(msg: ThreadMessage): string | OpenAIChatContentPart[] {
  const contentParts: OpenAIChatContentPart[] = [];
  const textChunks: string[] = [];

  for (const part of msg.content) {
    if (part.type === "text" && part.text.trim()) {
      textChunks.push(part.text);
    } else if (part.type === "image" && part.image) {
      contentParts.push({ type: "image_url", image_url: { url: part.image } });
    } else if (part.type === "file" && part.data && part.sourceType === "id") {
      contentParts.push({ type: "file", file: { file_id: part.data } });
    }
  }

  if (msg.role === "user" && msg.attachments?.length) {
    for (const att of msg.attachments) {
      if (att.status.type !== "complete") continue;
      for (const p of att.content) {
        if (p.type === "text" && p.text.trim()) {
          textChunks.push(p.text);
        } else if (p.type === "image" && p.image) {
          contentParts.push({ type: "image_url", image_url: { url: p.image } });
        } else if (p.type === "file" && p.data && p.sourceType === "id") {
          contentParts.push({ type: "file", file: { file_id: p.data } });
        }
      }
    }
  }

  const combinedText = textChunks.join("\n\n").trim();
  if (contentParts.length === 0) {
    return combinedText;
  }
  if (combinedText) {
    contentParts.unshift({ type: "text", text: combinedText });
  }
  return contentParts;
}

function parseMediaCrawlIntent(raw: string): {
  query: string;
  targetUrl: string;
  mediaKind: CrawlerMediaKind;
  role: string;
} {
  let clean = raw
    .trim()
    .replace(/^\/(voice|crawl-voice|crawl)\s*/i, "")
    .trim();
  const urlMatch = clean.match(/https?:\/\/[^\s"'<>]+/i);
  const targetUrl = urlMatch ? urlMatch[0] : "";
  if (targetUrl) {
    clean = clean.replace(targetUrl, "").trim();
  }

  let mediaKind: CrawlerMediaKind = "auto";
  if (/^\/voice\b/i.test(raw) || /(音色|声音|配音|克隆|声线|tts|voice)/i.test(raw)) {
    mediaKind = "voice";
  } else if (/(参考图|立绘|剧照|人物图|场景图|背景图|图片|image|portrait)/i.test(raw)) {
    mediaKind = "image";
  } else if (/(视频|片段|镜头|空镜|video|clip|b-roll)/i.test(raw)) {
    mediaKind = "video";
  }

  let role = "";
  for (const r of ["霸总男主", "大女主", "甜宠千金", "冷酷反派", "悬疑旁白", "热血战神"]) {
    if (clean.includes(r) || clean.includes(r.slice(0, 2))) {
      role = r;
      break;
    }
  }
  const cleanQuery = clean
    .replace(/(请帮我|帮我|爬虫|爬取|搜集|抓取|搜索|查找|找合适的|缓存到本地|克隆|写入系统库|加入系统库|复用)/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  return {
    query: cleanQuery || clean,
    targetUrl,
    mediaKind,
    role,
  };
}

function buildOfflineFallbackContent(report: DiagnosticsReport, rawError?: string): string {
  const rootCauseLines = (report.root_causes || [])
    .map(
      (rc, idx) =>
        `${idx + 1}. **[${rc.severity.toUpperCase()}] ${rc.title}** (${rc.category})\n   - 根因：${rc.summary}\n   - 建议自愈：${(rc.suggested_fixes || []).join("; ") || "无"}`
    )
    .join("\n\n");

  const breakerLine =
    report.tripped_proxies && report.tripped_proxies.length
      ? `\n\n> ⚡ **代理熔断中节点**: ${report.tripped_proxies
          .map((b) => `\`${b.url}\` (连续失败 ${b.consecutive_errors} 次)`)
          .join(", ")}`
      : "";

  const errPrefix = rawError
    ? `> ⚠️ **实时推理调用中断**: \`${rawError}\`\n> 已自动切入 **平台就地离线诊断引擎**，结合本地遥测给出根因研判与自愈建议。\n\n`
    : "";

  const readyAccounts = report.pool_stats ? report.pool_stats.active : 0;
  const totalAccounts = report.pool_stats ? report.pool_stats.total : 0;
  const imageReady = report.pool_stats
    ? Math.max(0, report.pool_stats.active - (report.pool_stats.image_limited || 0))
    : 0;

  return `${errPrefix}### 🔍 平台全链路遥测根因研判 (${report.generated_at})

**平台健康度评分**: **${report.health_score}/100**（号池可用: ${readyAccounts}/${totalAccounts}，生图可用: ${imageReady}/${totalAccounts}）${breakerLine}

#### 核心根因与修复建议
${rootCauseLines || "当前未检出高危根因，可能是瞬时网络抖动或上游 Cloudflare 临时质询。"}

\`\`\`openui
root = Card([header, diagCard, diagTable])
header = CardHeader("平台离线诊断与一键修复面板", "无需可用账号，直接基于本地遥测执行安全自愈")
diagCard = PlatformDiagnosisCard("平台全链路根因研判", "all")
diagTable = AccountDiagnosticTable("chatgpt")
\`\`\``;
}

function buildSystemPromptForSkill(params: {
  userText: string;
  skill: CopilotSkill;
  liveDiag: DiagnosticsReport | null;
  customSystemPrompt?: string;
  hasAttachments: boolean;
  openuiExplicit: boolean;
}): string {
  const {
    userText,
    skill,
    liveDiag,
    customSystemPrompt,
    hasAttachments,
    openuiExplicit,
  } = params;

  const progressiveOpenUI = getProgressiveOpenUISystemPrompt({
    userText,
    skill,
    hasAttachments,
    openuiExplicit,
  });
  const base = customSystemPrompt
    ? `${customSystemPrompt}\n\n${progressiveOpenUI}`
    : progressiveOpenUI;

  const attachmentRule = hasAttachments
    ? "\n\n【多模态附件指令】用户上传了数据文件或图片。若包含表格/CSV/JSON统计数据，请主动使用 OpenUI 组件（BarChart、LineChart、PieChart、Table、CardHeader）输出可视化分析看板。"
    : "";

  if (!liveDiag || (skill !== "diagnose" && skill !== "fix")) {
    return `${base}${attachmentRule}`;
  }

  const telemetryJson = JSON.stringify(
    {
      health_score: liveDiag.health_score,
      pool_stats: liveDiag.pool_stats,
      root_causes: liveDiag.root_causes,
      tripped_proxies: liveDiag.tripped_proxies,
    },
    null,
    2
  );

  return `${base}${attachmentRule}

---
【当前平台实时遥测快照 (只读真值)】
\`\`\`json
${telemetryJson}
\`\`\`

请优先结合上述平台真实遥测给出 Markdown 诊断分析，并在回复末尾附带 \`\`\`openui 代码块渲染 PlatformDiagnosisCard 与 AccountDiagnosticTable 方便运维人员一键自愈。`;
}

async function classifySemanticRouteWithLLM(params: {
  promptText: string;
  history: Array<{ role: string; content: string }>;
  chatModel: string;
  chatModels: string[];
  imageModels: string[];
  audioModels: string[];
  apiKey?: string;
  signal?: AbortSignal;
}): Promise<SemanticRouteLLMResult | null> {
  const routeAbort = new AbortController();
  const timeoutId = window.setTimeout(() => routeAbort.abort(), 12000);
  const onParentAbort = () => routeAbort.abort();
  params.signal?.addEventListener("abort", onParentAbort, { once: true });

  const recentContext = params.history.slice(-6).map((m) => ({
    role: m.role,
    content: m.content.slice(0, 500),
  }));

  try {
    const raw = await streamChatCompletion({
      messages: [
        {
          role: "system",
          content: [
            "You are an intent & modality router. Decide which modality handles the user's latest request given the conversation history.",
            'Modalities: "image" (generate/draw/redraw/modify an image, portrait, concept art, poster, etc. Note: asking only to write/refine a text prompt without generating an image is "chat"), "audio" (read aloud, TTS, voiceover), "research" (deep research/report), or "chat" (normal Q&A, coding, ops diagnostics, prompt writing).',
            `Available image models: ${params.imageModels.slice(0, 8).join(", ")}`,
            `Available audio models: ${params.audioModels.slice(0, 6).join(", ")}`,
            `Available chat models: ${params.chatModels.slice(0, 10).join(", ")}`,
            'Reply with ONLY a single-line valid JSON object (no markdown fences): {"modality":"chat|image|audio|research","targetModel":"","needsContextExtraction":false,"imagePrompt":"","reason":"short Chinese reason"}',
            'If modality is "image", fill "imagePrompt" with a complete, self-contained image generation prompt combining the conversation history and the user\'s latest request/reference.',
          ].join("\n"),
        },
        ...recentContext,
        { role: "user", content: params.promptText },
      ],
      model: params.chatModel,
      openuiMode: false,
      apiKey: params.apiKey,
      signal: routeAbort.signal,
      onDelta: () => {},
    });

    const trimmed = raw
      .trim()
      .replace(/^```(?:json)?\s*\n?/i, "")
      .replace(/\n?```$/, "")
      .trim();
    const jsonMatch = /\{[\s\S]*\}/.exec(trimmed);
    if (!jsonMatch) return null;
    const parsed = JSON.parse(jsonMatch[0]) as SemanticRouteLLMResult;
    if (
      parsed &&
      (parsed.modality === "chat" ||
        parsed.modality === "image" ||
        parsed.modality === "audio" ||
        parsed.modality === "research")
    ) {
      return parsed;
    }
    return null;
  } catch {
    return null;
  } finally {
    window.clearTimeout(timeoutId);
    params.signal?.removeEventListener("abort", onParentAbort);
  }
}

async function extractImagePromptFromContext(params: {
  userRequest: string;
  history: Array<{ role: string; content: string }>;
  chatModel: string;
  apiKey?: string;
  signal?: AbortSignal;
}): Promise<string> {
  const recent = params.history.slice(-8).map((m) => ({
    role: m.role,
    content: m.content.slice(0, 2000),
  }));
  try {
    const extracted = await streamChatCompletion({
      messages: [
        {
          role: "system",
          content:
            "You are an image prompt extractor. Read the conversation history and the user's latest image-generation request, and output ONLY the final, self-contained image generation prompt. Output ONLY the prompt text itself.",
        },
        ...recent,
        {
          role: "user",
          content: `User request: ${params.userRequest}\n\nOutput ONLY the final self-contained image generation prompt:`,
        },
      ],
      model: params.chatModel,
      openuiMode: false,
      apiKey: params.apiKey,
      signal: params.signal,
      onDelta: () => {},
    });
    const cleaned = extracted
      .trim()
      .replace(/^```[a-zA-Z]*\s*\n?/, "")
      .replace(/\n?```$/, "")
      .replace(/^["'“”]+|["'“”]+$/g, "")
      .trim();
    if (cleaned && cleaned.length >= 8) return cleaned;
  } catch {
    // fallback
  }
  return extractPromptFallback(params.history, params.userRequest);
}

async function extractSpeechTextFromContext(params: {
  userRequest: string;
  history: Array<{ role: string; content: string }>;
  chatModel: string;
  apiKey?: string;
  signal?: AbortSignal;
}): Promise<string> {
  const recent = params.history.slice(-6).map((m) => ({
    role: m.role,
    content: m.content.slice(0, 2000),
  }));
  try {
    const extracted = await streamChatCompletion({
      messages: [
        {
          role: "system",
          content:
            "You are a text-to-speech script extractor. Read the conversation history and the user's latest request, and output ONLY the exact plain-text passage to be read aloud (without markdown syntax, URLs, or code blocks).",
        },
        ...recent,
        {
          role: "user",
          content: `User request: ${params.userRequest}\n\nOutput ONLY the plain text to read aloud:`,
        },
      ],
      model: params.chatModel,
      openuiMode: false,
      apiKey: params.apiKey,
      signal: params.signal,
      onDelta: () => {},
    });
    const cleaned = extracted.trim();
    if (cleaned && cleaned.length >= 2) return cleaned.slice(0, 2000);
  } catch {
    // fallback
  }
  return extractSpeechFallback(params.history, params.userRequest);
}

function blobToDataUrl(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onloadend = () => {
      if (typeof reader.result === "string") resolve(reader.result);
      else reject(new Error("音频编码失败"));
    };
    reader.onerror = () => reject(new Error("音频读取失败"));
    reader.readAsDataURL(blob);
  });
}

async function urlToBlob(url: string): Promise<Blob> {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`参考图加载失败: HTTP ${res.status}`);
  return res.blob();
}

export function createChatModelAdapter(
  getConfig: () => ChatRuntimeConfig
): ChatModelAdapter {
  return {
    async *run(options: ChatModelRunOptions): AsyncGenerator<ChatModelRunResult, void> {
      const cfg = getConfig();
      const { messages, runConfig, abortSignal } = options;
      const customRun = (runConfig?.custom || {}) as {
        overrideModel?: string;
        skillHint?: CopilotSkill;
        remixImageUrl?: string;
        remixMode?: "edit" | "chat";
      };

      const targetModel = customRun.overrideModel || cfg.model || "gpt-5-6";
      const lastUserMsg = [...messages].reverse().find((m) => m.role === "user");
      if (!lastUserMsg) {
        yield {
          content: [{ type: "text", text: "请输入您的问题或 `/` 指令。" }],
          status: { type: "complete", reason: "stop" },
        };
        return;
      }

      const rawUserText = extractMessagePlainText(lastUserMsg);
      const userImageUrls = extractMessageImageUrls(lastUserMsg);
      const historyPlain = messages
        .slice(0, -1)
        .map((m) => ({ role: m.role, content: extractMessagePlainText(m) }));

      const availableImageModels =
        cfg.availableImageModels.length > 0 ? cfg.availableImageModels : DEFAULT_IMAGE_MODELS;
      const availableAudioModels =
        cfg.availableAudioModels.length > 0 ? cfg.availableAudioModels : DEFAULT_TTS_MODELS;
      const availableChatModels =
        cfg.availableChatModels.length > 0 ? cfg.availableChatModels : ["gpt-5-6", "gpt-4o"];

      const startTime = Date.now();

      // 1. 若明确是生图 Remix 重绘模式（来自 #/image 或带参考图重绘指令）
      if (
        customRun.remixImageUrl &&
        customRun.remixMode === "edit" &&
        !rawUserText.trim().startsWith("/")
      ) {
        const chosenImageModel = isImageModelId(targetModel, availableImageModels)
          ? targetModel
          : availableImageModels[0] || "gpt-image-2.5-sunburst";
        yield {
          content: [
            {
              type: "text",
              text: `🎨 **正在基于参考图进行 Chat Remix 重绘** (\`${chosenImageModel}\`)...`,
            },
          ],
          status: { type: "running" },
        };
        const refBlob = await urlToBlob(customRun.remixImageUrl);
        const refFile = new File([refBlob], "remix-ref.png", { type: refBlob.type || "image/png" });
        const imgRes = await editImageWithReferences({
          prompt: rawUserText,
          files: [refFile],
          model: chosenImageModel,
          size: "1024x1024",
          apiKey: cfg.apiKey,
        });
        const item = imgRes?.data?.[0];
        const imgUrl =
          item?.url || (item?.b64_json ? `data:image/png;base64,${item.b64_json}` : "");
        if (!imgUrl) throw new Error("服务端 Chat Remix 未返回有效图像地址");

        let finalPersistentUrl = imgUrl;
        try {
          const localBlob = item?.b64_json ? base64ToBlob(item.b64_json) || undefined : undefined;
          const persisted = await saveUnifiedImage(
            {
              id: `img-remix-${Date.now()}`,
              blob: localBlob,
              remoteUrl: item?.url,
              prompt: rawUserText,
              model: chosenImageModel,
              size: "1024x1024",
              quality: "hd",
              mode: "edits",
              latencyMs: Date.now() - startTime,
              createdAt: new Date().toLocaleTimeString("zh-CN", { hour12: false }),
              timestamp: Date.now(),
            },
            item?.b64_json
          );
          if (persisted?.url) finalPersistentUrl = persisted.url;
        } catch {
          // ignore gallery persistence failure
        }

        const elapsedMs = Date.now() - startTime;
        const successMarkdown = [
          `![Chat Remix 结果](${finalPersistentUrl.trim()})`,
          "",
          `✨ **Chat Remix 重绘完成** · \`${chosenImageModel}\` · ${elapsedMs}ms`,
          `- [在新标签页查看高清原图](${finalPersistentUrl.trim()}) · [前往生图工坊/创作画廊管理](#/image)`,
        ].join("\n");

        yield {
          content: [{ type: "text", text: successMarkdown }],
          status: { type: "complete", reason: "stop" },
          metadata: {
            custom: {
              model: chosenImageModel,
              openuiMode: false,
              skill: "image",
              latencyMs: elapsedMs,
            },
          },
        };
        return;
      }

      // 2. 全工坊媒体爬虫路由：/voice、/crawl 或自然语言素材检索
      const isCrawlCmd =
        /^\/(voice|crawl-voice|crawl)\b/i.test(rawUserText.trim()) ||
        MEDIA_CRAWL_INTENT_RE.test(rawUserText);
      if (isCrawlCmd) {
        const parsedCrawl = parseMediaCrawlIntent(rawUserText);
        yield {
          content: [
            { type: "text", text: "🌐 正在检索系统现有缓存库并爬取外部匹配资源..." },
          ],
          status: { type: "running" },
        };
        const res = await crawlMedia({
          query: parsedCrawl.query,
          url: parsedCrawl.targetUrl,
          media_kind: parsedCrawl.mediaKind,
          limit: 10,
        });
        const elapsedMs = Date.now() - startTime;
        const sysVoiceCount = res.existing_voices?.length || 0;
        const sysImageCount = res.existing_images?.length || 0;
        const sysVideoCount = res.existing_videos?.length || 0;
        const candCount = res.candidates?.length || 0;
        const escapeAttr = (v: string) =>
          v.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\n/g, " ");

        const openuiBlock = [
          "```openui",
          "root = Card([header, crawler])",
          `header = CardHeader("跨工坊素材爬虫与系统库入库台", "检索词: ${escapeAttr(
            parsedCrawl.query || parsedCrawl.targetUrl || "全库检索"
          )} · 现有命中 语音${sysVoiceCount}/图像${sysImageCount}/视频${sysVideoCount} · 新抓取 ${candCount} 项")`,
          `crawler = MediaCrawlerCard("${escapeAttr(parsedCrawl.query)}", "${escapeAttr(
            parsedCrawl.targetUrl
          )}", "${parsedCrawl.mediaKind}", "${escapeAttr(parsedCrawl.role)}")`,
          "```",
        ].join("\n");

        const summaryMarkdown = [
          `🌐 **已完成系统缓存检索与网页媒体爬虫扫描** (${elapsedMs}ms)`,
          `- **系统现有库命中**：音色 ${sysVoiceCount} 个 · 图像 ${sysImageCount} 张 · 视频 ${sysVideoCount} 条`,
          `- **网络爬虫新发现**：${candCount} 条候选（可在下方卡片内直接试听/预览，点击按钮即可 **缓存本地克隆或写入系统库**，并在「配音/生图/视频/短剧工坊」中直接复用）`,
          "",
          openuiBlock,
        ].join("\n");

        yield {
          content: [{ type: "text", text: summaryMarkdown }],
          status: { type: "complete", reason: "stop" },
          metadata: {
            custom: {
              model: "crawler",
              openuiMode: true,
              skill: "media_crawl",
              latencyMs: elapsedMs,
            },
          },
        };
        return;
      }

      // 3. 语义多模态路由（图像生成 / 语音合成 / 对话）
      const hasExplicitModelOverride = Boolean(customRun.overrideModel);
      const needsSemanticCheck =
        cfg.autoSwitchModel &&
        !rawUserText.trim().startsWith("/") &&
        !hasExplicitModelOverride &&
        userImageUrls.length === 0 &&
        !isImageModelId(targetModel, availableImageModels) &&
        !isTtsModelId(targetModel, availableAudioModels);

      if (needsSemanticCheck) {
        yield {
          content: [{ type: "text", text: "🧭 正在理解您的意图与模态..." }],
          status: { type: "running" },
        };
      }

      const routerChatModel =
        isImageModelId(targetModel, availableImageModels) ||
        isTtsModelId(targetModel, availableAudioModels)
          ? availableChatModels[0] || "gpt-5-6"
          : targetModel;

      const route = await resolveSemanticModelRoute({
        promptText: rawUserText,
        selectedModel: targetModel,
        hasExplicitModelOverride,
        autoSwitchEnabled: cfg.autoSwitchModel,
        history: historyPlain,
        availableChatModels,
        availableImageModels,
        availableAudioModels,
        classifyWithLLM: needsSemanticCheck
          ? (args) =>
              classifySemanticRouteWithLLM({
                promptText: args.promptText,
                history: args.history,
                chatModel: routerChatModel,
                chatModels: args.chatModels,
                imageModels: args.imageModels,
                audioModels: args.audioModels,
                apiKey: cfg.apiKey,
                signal: abortSignal,
              })
          : undefined,
      });

      // 3a. 生图模态路由
      if (route.modality === "image") {
        const chosenImageModel = route.targetModel;
        yield {
          content: [
            {
              type: "text",
              text: `🎨 **正在调用图像模型** \`${chosenImageModel}\`${
                route.switched ? ` *(${route.reason || "自动切换"})*` : ""
              } 绘制图像...`,
            },
          ],
          status: { type: "running" },
        };

        let finalImagePrompt = route.extractedPrompt || route.cleanedPrompt;
        if (!route.extractedPrompt && route.needsContextExtraction) {
          finalImagePrompt = await extractImagePromptFromContext({
            userRequest: rawUserText,
            history: historyPlain,
            chatModel: routerChatModel,
            apiKey: cfg.apiKey,
            signal: abortSignal,
          });
        }

        const imgRes = await generateImage({
          prompt: finalImagePrompt,
          model: chosenImageModel,
          size: "1024x1024",
          quality: "hd",
          apiKey: cfg.apiKey,
        });
        const item = imgRes?.data?.[0];
        const imgUrl =
          item?.url || (item?.b64_json ? `data:image/png;base64,${item.b64_json}` : "");
        if (!imgUrl) throw new Error("图像模型未返回有效的图片数据");

        const elapsedMs = Date.now() - startTime;
        let finalPersistentUrl = imgUrl;
        try {
          const localBlob = item?.b64_json ? base64ToBlob(item.b64_json) || undefined : undefined;
          const persisted = await saveUnifiedImage(
            {
              id: `img-${Date.now()}`,
              blob: localBlob,
              remoteUrl: item?.url,
              prompt: finalImagePrompt,
              model: chosenImageModel,
              size: "1024x1024",
              quality: "hd",
              mode: "generations",
              latencyMs: elapsedMs,
              createdAt: new Date().toLocaleTimeString("zh-CN", { hour12: false }),
              timestamp: Date.now(),
            },
            item?.b64_json
          );
          if (persisted?.url) finalPersistentUrl = persisted.url;
        } catch {
          // ignore
        }

        const safeUrl = finalPersistentUrl.trim();
        const imageAlt = formatSingleLinePrompt(finalImagePrompt, 60, true) || "AI 生成图片";
        const successMarkdown = [
          `![${imageAlt}](${safeUrl})`,
          "",
          `✨ **图像生成完成** · \`${chosenImageModel}\` · ${elapsedMs}ms`,
          `- [在新标签页查看高清原图](${safeUrl}) · [前往生图工坊/创作画廊管理](#/image)`,
        ].join("\n");

        yield {
          content: [{ type: "text", text: successMarkdown }],
          status: { type: "complete", reason: "stop" },
          metadata: {
            custom: {
              model: chosenImageModel,
              openuiMode: false,
              skill: "image",
              latencyMs: elapsedMs,
            },
          },
        };
        return;
      }

      // 3b. 语音合成路由
      if (route.modality === "audio") {
        const chosenAudioModel = route.targetModel;
        yield {
          content: [
            {
              type: "text",
              text: `🎙️ **正在调用语音模型** \`${chosenAudioModel}\` 合成音频...`,
            },
          ],
          status: { type: "running" },
        };
        let speechText = route.cleanedPrompt;
        if (route.needsContextExtraction) {
          speechText = await extractSpeechTextFromContext({
            userRequest: rawUserText,
            history: historyPlain,
            chatModel: routerChatModel,
            apiKey: cfg.apiKey,
            signal: abortSignal,
          });
        }
        const audioBlob = await synthesizeSpeech(
          speechText,
          "zh-CN-XiaoxiaoNeural",
          1.0,
          chosenAudioModel
        );
        const audioDataUrl = await blobToDataUrl(audioBlob);
        const elapsedMs = Date.now() - startTime;

        const successMarkdown = [
          `<audio src="${audioDataUrl}" />`,
          "",
          `🔊 **语音合成成功** (${elapsedMs}ms)`,
          `- **所用模型**：\`${chosenAudioModel}\``,
          `- **朗读正文**：${speechText.length > 180 ? `${speechText.slice(0, 180)}...` : speechText}`,
        ].join("\n");

        yield {
          content: [{ type: "text", text: successMarkdown }],
          status: { type: "complete", reason: "stop" },
          metadata: {
            custom: {
              model: chosenAudioModel,
              openuiMode: false,
              skill: "audio",
              latencyMs: elapsedMs,
            },
          },
        };
        return;
      }

      // 4. 文本 / 平台诊断 / OpenUI 混合流式对话
      const activeChatModel = route.targetModel;
      let effectiveSkill: CopilotSkill = customRun.skillHint || "auto";
      const trimmed = rawUserText.trim();
      if (/^\/diagnose\b/i.test(trimmed)) effectiveSkill = "diagnose";
      else if (/^\/fix\b/i.test(trimmed)) effectiveSkill = "fix";
      else if (/^\/openui\b/i.test(trimmed)) effectiveSkill = "openui";
      else if (OPS_INTENT_RE.test(trimmed)) effectiveSkill = "diagnose";
      else if (OPENUI_INTENT_RE.test(trimmed)) effectiveSkill = "openui";
      else if (IMAGE_INTENT_RE.test(trimmed)) effectiveSkill = "openui";

      const hasAttachments = Boolean(lastUserMsg.attachments && lastUserMsg.attachments.length > 0);
      const sendOpenUIHeader =
        cfg.openuiMode ||
        effectiveSkill === "openui" ||
        effectiveSkill === "diagnose" ||
        effectiveSkill === "fix" ||
        hasAttachments;

      let liveDiag: DiagnosticsReport | null = null;
      if (effectiveSkill === "diagnose" || effectiveSkill === "fix") {
        try {
          liveDiag = await fetchDiagnostics();
        } catch {
          liveDiag = null;
        }
      }

      const systemPrompt = buildSystemPromptForSkill({
        userText: rawUserText,
        skill: effectiveSkill,
        liveDiag,
        customSystemPrompt: cfg.systemPrompt,
        hasAttachments,
        openuiExplicit: sendOpenUIHeader,
      });

      const rawHistoryMessages = messages.map((m) => ({
        role: m.role,
        content: toOpenAIMessageContent(m),
      }));

      // 注入实时遥测摘要与尾部近因格式锚定到最后一条用户消息（仅发往上游，不污染本地 UI）
      const lastIdx = rawHistoryMessages.length - 1;
      const lastEntry = rawHistoryMessages[lastIdx];
      const recencyAnchor = sendOpenUIHeader
        ? "\n\n[格式约束：仅允许在 ```openui 代码块内使用 `root = Card([...])` 语法；严禁输出 <br>、<span>、<icon>、<CodeBlock> 等任何 HTML/XML 标签]"
        : "";
      const diagSummary =
        liveDiag && (effectiveSkill === "diagnose" || effectiveSkill === "fix")
          ? `\n\n[实时遥测: 健康分=${liveDiag.health_score}, 可用账号=${
              liveDiag.pool_stats?.active ?? 0
            }/${liveDiag.pool_stats?.total ?? 0}, 熔断代理数=${
              liveDiag.tripped_proxies?.length ?? 0
            }, 异常根因数=${liveDiag.root_causes?.length ?? 0}]`
          : "";
      const suffix = `${diagSummary}${recencyAnchor}`;
      if (suffix && lastEntry) {
        if (typeof lastEntry.content === "string") {
          lastEntry.content = `${lastEntry.content}${suffix}`;
        } else if (Array.isArray(lastEntry.content)) {
          lastEntry.content = [...lastEntry.content, { type: "text", text: suffix }];
        }
      }

      const sanitizedHistory = sanitizeMessagesForLLM(rawHistoryMessages, 2);
      const requestMessages = [
        { role: "system", content: systemPrompt },
        ...sanitizedHistory,
      ];

      // 注：仅当后端外部 API 需要兜底且客户端未注入 System Prompt 时才会用 X-OpenUI-Lang 注入 56KB 全量提示词；
      // 前端工作台已精准注入渐进式披露后的 System Prompt，故不重复触发后端全量覆盖。
      const useHeaderFallback = false;

      // 使用推拉队列把 SSE onDelta 转成标准 AsyncGenerator 流式帧
      let latestText = "";
      let streamDone = false;
      let streamError: unknown = null;
      let notifyNext: (() => void) | null = null;

      const wakeUp = () => {
        if (notifyNext) {
          const fn = notifyNext;
          notifyNext = null;
          fn();
        }
      };

      const runPromise = streamChatCompletion({
        messages: requestMessages,
        model: activeChatModel,
        openuiMode: useHeaderFallback,
        apiKey: cfg.apiKey,
        signal: abortSignal,
        onDelta: (_delta, fullText) => {
          latestText = fullText;
          wakeUp();
        },
      })
        .then((finalText) => {
          latestText = sanitizeOpenUIHtmlText(finalText || latestText, "markdown");
          streamDone = true;
          wakeUp();
        })
        .catch((err) => {
          streamError = err;
          streamDone = true;
          wakeUp();
        });

      let lastYielded = "";
      while (!streamDone) {
        if (latestText !== lastYielded) {
          lastYielded = latestText;
          yield {
            content: [{ type: "text", text: latestText }],
            status: { type: "running" },
            metadata: {
              custom: {
                model: activeChatModel,
                openuiMode: sendOpenUIHeader || containsOpenUIDSL(latestText),
                skill: effectiveSkill,
              },
            },
          };
        }
        if (!streamDone) {
          await new Promise<void>((resolve) => {
            notifyNext = resolve;
          });
        }
      }

      await runPromise;

      if (streamError) {
        if (abortSignal.aborted) {
          yield {
            content: [{ type: "text", text: latestText || "" }],
            status: { type: "incomplete", reason: "cancelled" },
          };
          return;
        }
        // 号池耗尽或运维提问时，自动切入本地离线诊断兜底
        const errMsg = streamError instanceof Error ? streamError.message : String(streamError);
        if (effectiveSkill === "diagnose" || effectiveSkill === "fix" || OPS_INTENT_RE.test(trimmed)) {
          try {
            const report = liveDiag || (await fetchDiagnostics());
            const offlineMarkdown = buildOfflineFallbackContent(report, errMsg);
            const elapsedMs = Date.now() - startTime;
            yield {
              content: [{ type: "text", text: offlineMarkdown }],
              status: { type: "complete", reason: "stop" },
              metadata: {
                custom: {
                  model: "offline-diagnostics",
                  openuiMode: true,
                  skill: effectiveSkill,
                  latencyMs: elapsedMs,
                  isOfflineFallback: true,
                },
              },
            };
            return;
          } catch {
            // fallback failed, rethrow original
          }
        }
        throw streamError;
      }

      const elapsedMs = Date.now() - startTime;
      const detectedOpenUI = sendOpenUIHeader || containsOpenUIDSL(latestText);

      yield {
        content: [{ type: "text", text: latestText }],
        status: { type: "complete", reason: "stop" },
        metadata: {
          custom: {
            model: activeChatModel,
            openuiMode: detectedOpenUI,
            skill: effectiveSkill,
            latencyMs: elapsedMs,
          },
        },
      };
    },
  };
}
