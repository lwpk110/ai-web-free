import React, { useEffect, useMemo, useRef, useState } from "react";
import { Renderer } from "@openuidev/react-lang";
import { ThemeProvider } from "@openuidev/react-ui";
import {
  Activity,
  Check,
  ChevronDown,
  Clapperboard,
  Code2,
  Copy,
  Download,
  Edit3,
  ExternalLink,
  FileCode2,
  FileText,
  Globe,
  History,
  ImagePlus,
  KeyRound,
  Languages,
  Layers,
  MessageSquare,
  PanelLeftClose,
  PanelLeftOpen,
  Pin,
  PinOff,
  Plus,
  RefreshCw,
  RotateCcw,
  Search,
  SendHorizontal,
  ShieldAlert,
  Sparkles,
  Square,
  StopCircle,
  Trash2,
  Upload,
  Wrench,
  X,
} from "lucide-react";
import {
  aiWebFreeLibrary,
  aiWebFreeToolProvider,
  getOpenUISystemPrompt,
} from "../lib/openui-library";
import {
  crawlMedia,
  editImageWithReferences,
  exportServerConversationMarkdown,
  type CrawlerMediaKind,
  fetchDiagnostics,
  fetchServerChatPresets,
  generateImage,
  importServerConversations,
  searchServerChat,
  streamChatCompletion,
  synthesizeSpeech,
  type DiagnosticsReport,
  type ServerChatPreset,
  type ServerSearchHit,
} from "../api/client";
import {
  CHAT_REMIX_EVENT,
  consumePendingChatRemix,
  type PendingChatRemix,
} from "../lib/chatRemixBridge";
import { dispatchDramaCanvas } from "../lib/dramaCanvasBridge";
import { FALLBACK_CATALOG_IDS, useModelsCatalog } from "../lib/models";
import {
  DEFAULT_IMAGE_MODELS,
  DEFAULT_TTS_MODELS,
  extractPromptFallback,
  extractSpeechFallback,
  isImageModelId,
  isTtsModelId,
  parseImageCommand,
  resolveSemanticModelRoute,
  type SemanticRouteLLMResult,
} from "../lib/model-router";
import {
  base64ToBlob,
  createUnifiedConversation,
  deleteUnifiedConversation,
  loadUnifiedConversationMessages,
  loadUnifiedConversations,
  saveUnifiedConversationMessages,
  saveUnifiedImage,
  updateUnifiedConversation,
  type CopilotSkill,
  type StoredChatMessage,
  type StoredConversation,
  type StoredImageRecord,
} from "../lib/storage";
import {
  containsOpenUIDSL,
  MarkdownContent,
  splitHybridSegments,
} from "../components/MarkdownContent";

const INITIAL_COPILOT_MESSAGE = `欢迎使用 **AI-Web-Free 智能对话与平台诊断 Copilot**。我可以与你进行多轮技术对话，也能实时读取全平台号池、代理节点、卡密队列与审计链路数据进行深度根因诊断和一键自愈：

- 直接提问或使用 \`/diagnose\` 分析 **号池熔断、账号封禁、403 WAF 风控、401 过期、429 限流** 等根因
- 使用 \`/fix\` 呼出平台诊断自愈台或直接点击下方按钮执行 **一键智能自愈**（支持无可用账号下的 **离线诊断兜底**）
- 使用 \`/openui\` 或 \`/image\` 流式渲染交互式 OpenUI 看板与生图工作卡
- 使用 \`/voice\` 或 \`/crawl\` 触发 **全工坊网络爬虫工具**：先检索现有本地缓存库，未命中则从网页搜集并缓存 **音色克隆样本 / 参考图像 / 视频素材** 写入系统库跨工坊复用（纯云端推理，无需本地硬件）

\`\`\`openui
root = Card([header, diagCard, poolCard])
header = CardHeader("AI-Web-Free Copilot 实时诊断台", "混合渲染：Markdown 专业分析 + 嵌入式 OpenUI 交互式运维组件")
diagCard = PlatformDiagnosisCard("平台全链路健康与异常根因扫描", "all")
poolCard = PoolOverviewCard("chatgpt", "ChatGPT Web 免费号池实时监控")
\`\`\``;

const CHAT_MODEL_STORAGE_KEY = "ai_web_free_chat_model";

const FALLBACK_CHAT_MODELS = FALLBACK_CATALOG_IDS.filter(
  (id) =>
    !id.includes("image") &&
    !id.includes("dall-e") &&
    !id.includes("tts") &&
    !id.includes("transcribe") &&
    !id.includes("whisper")
);

function formatSingleLinePrompt(
  text: string,
  maxLen: number,
  stripBrackets = false
): string {
  let normalized = text.replace(/\s+/g, " ").trim();
  if (stripBrackets) {
    normalized = normalized.replace(/[[\]]/g, "");
  }
  if (!normalized) {
    return "";
  }
  if (normalized.length <= maxLen) {
    return normalized;
  }
  return `${normalized.slice(0, maxLen).trimEnd()}...`;
}

function getStoredChatModel(): string {
  try {
    return window.sessionStorage.getItem(CHAT_MODEL_STORAGE_KEY) || "gpt-5-6";
  } catch {
    return "gpt-5-6";
  }
}

interface SkillDefinition {
  command: string;
  skill: CopilotSkill | "clear";
  label: string;
  description: string;
  defaultPrompt: string;
  icon: React.ComponentType<{ size?: number; className?: string }>;
}

const SLASH_SKILLS: SkillDefinition[] = [
  {
    command: "/diagnose",
    skill: "diagnose",
    label: "诊断平台",
    description: "采集号池/代理/卡密/审计实时遥测，输出 5 类根因分析与诊断卡",
    defaultPrompt:
      "请结合实时平台遥测数据，全面诊断当前号池熔断、账号封禁、403 WAF 拦截、401 过期、429 限流及代理异常的根因，并附带诊断卡片。",
    icon: ShieldAlert,
  },
  {
    command: "/fix",
    skill: "fix",
    label: "修复方案",
    description: "分析可恢复故障并挂载一站式自愈与确认清理工作台",
    defaultPrompt:
      "请分析当前平台故障并给出分层修复方案（优先安全自愈：切换健康代理、续期过期账号、停用封禁或补充匿名 UUID），并生成一键修复面板。",
    icon: Wrench,
  },
  {
    command: "/openui",
    skill: "openui",
    label: "OpenUI 卡片",
    description: "以纯 Thesys OpenUI Lang 流式构建交互式仪表盘与图表",
    defaultPrompt:
      "请使用 OpenUI Lang 生成一份包含 CardHeader、PoolOverviewCard、AccountDiagnosticTable 与维护建议 Callout 的号池交互大盘。",
    icon: Sparkles,
  },
  {
    command: "/image",
    skill: "image",
    label: "AI 图像生成",
    description: "调用图像生成模型（支持 /image [模型] 提示词，缺省 auto 模式自动随机选择）",
    defaultPrompt:
      "/image 一个赛博朋克风格的微缩发光水晶立方体，棚拍微距，8k 高清",
    icon: ImagePlus,
  },
  {
    command: "/voice",
    skill: "media_crawl",
    label: "音色爬虫克隆",
    description: "先查本地系统音色库，未命中则搜索或爬取指定网页音频缓存到本地克隆入库",
    defaultPrompt: "/voice 霸总低沉男声 短剧配音",
    icon: Globe,
  },
  {
    command: "/crawl",
    skill: "media_crawl",
    label: "全工坊素材爬虫",
    description: "从网络/指定 URL 搜索并缓存音色、参考图或视频素材写入系统库供各工坊复用",
    defaultPrompt: "/crawl 古风仙侠女主立绘 参考图",
    icon: Globe,
  },
  {
    command: "/clear",
    skill: "clear",
    label: "清空本轮会话",
    description: "清除当前会话历史消息并恢复初始状态",
    defaultPrompt: "",
    icon: RotateCcw,
  },
];

const STARTER_PROMPTS = [
  {
    icon: Activity,
    label: "诊断全平台为什么异常/熔断",
    prompt:
      "/diagnose 请检查当前所有账号与代理状态，说明导致不可用、封禁或 403/401 的真实根因与修复建议。",
  },
  {
    icon: Wrench,
    label: "生成自愈与代理恢复面板",
    prompt:
      "/fix 帮我诊断当前损坏代理和过期账号，并展示一键自愈与账号诊断表格。",
  },
  {
    icon: Globe,
    label: "搜集短剧音色/素材入库",
    prompt: "/voice 霸总低沉男声",
  },
  {
    icon: ImagePlus,
    label: "快捷生图 (/image)",
    prompt: SLASH_SKILLS[3].defaultPrompt,
  },
  {
    icon: Layers,
    label: "多供应商 Failover 矩阵",
    prompt:
      "/openui 请使用 OpenUI Lang 的 Tabs、Table 与 Callout 组件生成一份多供应商（ChatGPT Web、Claude、Gemini）能力对比矩阵与无损 Failover 策略说明。",
  },
];

const OPS_INTENT_RE =
  /(诊断|熔断|封禁|停用|风控|拦截|403|401|429|号池|代理|报错|异常|不可用|失败|为什么挂|挂了|续期|卡密|测活|修复|自愈|健康|banned|expired|failover|waf|cloudflare|sentinel|turnstile|proxy|pool|health)/i;
const IMAGE_INTENT_RE = /(生图卡|image\s*studio|画一张|生成图片|文生图|生图工作台)/i;
const OPENUI_INTENT_RE = /(openui|交互卡片|交互式|仪表盘|看板|可视化|饼图|柱状图|折线图)/i;
const MEDIA_CRAWL_INTENT_RE =
  /((爬虫|爬取|搜集|抓取|搜一个|找一个|找合适|采集).*(音色|声音|配音|克隆|声线|参考图|立绘|剧照|视频|素材))|((音色|参考图|视频素材).*(缓存|克隆|入库|系统库|复用))/i;

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

async function classifySemanticRouteWithLLM(params: {
  promptText: string;
  history: StoredChatMessage[];
  chatModel: string;
  chatModels: string[];
  imageModels: string[];
  audioModels: string[];
  apiKey?: string;
  signal?: AbortSignal;
}): Promise<SemanticRouteLLMResult | null> {
  const {
    promptText,
    history,
    chatModel,
    chatModels,
    imageModels,
    audioModels,
    apiKey,
    signal,
  } = params;

  const routeAbort = new AbortController();
  const timeoutId = window.setTimeout(() => routeAbort.abort(), 12000);
  const onParentAbort = () => routeAbort.abort();
  signal?.addEventListener("abort", onParentAbort, { once: true });

  const recentContext = history.slice(-6).map((m) => ({
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
            `Available image models: ${imageModels.slice(0, 8).join(", ")}`,
            `Available audio models: ${audioModels.slice(0, 6).join(", ")}`,
            `Available chat models: ${chatModels.slice(0, 10).join(", ")}`,
            'Reply with ONLY a single-line valid JSON object (no markdown fences): {"modality":"chat|image|audio|research","targetModel":"","needsContextExtraction":false,"imagePrompt":"","reason":"short Chinese reason"}',
            'If modality is "image", fill "imagePrompt" with a complete, self-contained image generation prompt combining the conversation history and the user\'s latest request/reference.',
          ].join("\n"),
        },
        ...recentContext,
        {
          role: "user",
          content: promptText,
        },
      ],
      model: chatModel,
      openuiMode: false,
      apiKey,
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
    signal?.removeEventListener("abort", onParentAbort);
  }
}

async function extractImagePromptFromContext(params: {
  userRequest: string;
  history: StoredChatMessage[];
  chatModel: string;
  apiKey?: string;
  signal?: AbortSignal;
}): Promise<string> {
  const { userRequest, history, chatModel, apiKey, signal } = params;
  const recent = history.slice(-8).map((m) => ({
    role: m.role,
    content: m.content.slice(0, 2000),
  }));

  try {
    const extracted = await streamChatCompletion({
      messages: [
        {
          role: "system",
          content:
            "You are an image prompt extractor. Read the conversation history and the user's latest image-generation request, and output ONLY the final, self-contained image generation prompt (preserving all visual subject, clothing, hair, lighting, composition, style, and aspect-ratio details from the latest prompt in context). Output ONLY the prompt text itself—no explanations, no markdown code fences, no quotes, and no refusals.",
        },
        ...recent,
        {
          role: "user",
          content: `User request: ${userRequest}\n\nOutput ONLY the final self-contained image generation prompt:`,
        },
      ],
      model: chatModel,
      openuiMode: false,
      apiKey,
      signal,
      onDelta: () => {
        // 静默提取提示词
      },
    });

    const cleaned = extracted
      .trim()
      .replace(/^```[a-zA-Z]*\s*\n?/, "")
      .replace(/\n?```$/, "")
      .replace(/^["'“”]+|["'“”]+$/g, "")
      .trim();

    if (cleaned && cleaned.length >= 8) {
      return cleaned;
    }
  } catch {
    // 提取失败时回退到本地历史摘要
  }

  return extractPromptFallback(history, userRequest);
}

async function extractSpeechTextFromContext(params: {
  userRequest: string;
  history: StoredChatMessage[];
  chatModel: string;
  apiKey?: string;
  signal?: AbortSignal;
}): Promise<string> {
  const { userRequest, history, chatModel, apiKey, signal } = params;
  const recent = history.slice(-6).map((m) => ({
    role: m.role,
    content: m.content.slice(0, 2000),
  }));

  try {
    const extracted = await streamChatCompletion({
      messages: [
        {
          role: "system",
          content:
            "You are a text-to-speech script extractor. Read the conversation history and the user's latest request, and output ONLY the exact plain-text passage to be read aloud (without markdown syntax, URLs, code blocks, or commentary). Output ONLY the text to speak.",
        },
        ...recent,
        {
          role: "user",
          content: `User request: ${userRequest}\n\nOutput ONLY the plain text to read aloud:`,
        },
      ],
      model: chatModel,
      openuiMode: false,
      apiKey,
      signal,
      onDelta: () => {
        // 静默提取朗读文本
      },
    });

    const cleaned = extracted.trim();
    if (cleaned && cleaned.length >= 2) {
      return cleaned.slice(0, 2000);
    }
  } catch {
    // 回退到本地正文提取
  }

  return extractSpeechFallback(history, userRequest);
}

function blobToDataUrl(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onloadend = () => {
      if (typeof reader.result === "string") {
        resolve(reader.result);
      } else {
        reject(new Error("音频编码失败"));
      }
    };
    reader.onerror = () => reject(new Error("音频读取失败"));
    reader.readAsDataURL(blob);
  });
}

function buildOfflineFallbackContent(
  report: DiagnosticsReport,
  rawError?: string
): string {
  const rootCauseLines = report.root_causes
    .map(
      (rc, idx) =>
        `${idx + 1}. **[${rc.severity.toUpperCase()}] ${rc.title}** (${rc.category})\n   - 根因：${rc.summary}\n   - 建议自愈：${(rc.suggested_fixes || []).join("; ") || "无"}`
    )
    .join("\n\n");

  const breakerLine =
    report.tripped_proxies && report.tripped_proxies.length
      ? `\n\n> ⚡ **代理熔断中节点**: ${report.tripped_proxies.map((b) => `\`${b.url}\` (连续失败 ${b.consecutive_errors} 次)`).join(", ")}`
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

function buildSystemPromptForSkill(
  skill: CopilotSkill,
  liveDiag: DiagnosticsReport | null,
  customSystemPrompt?: string
): string {
  const openuiPrompt = getOpenUISystemPrompt();
  const base = customSystemPrompt || openuiPrompt;
  if (!liveDiag || (skill !== "diagnose" && skill !== "fix")) {
    return base;
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

  return `${base}

---
【当前平台实时遥测快照 (只读真值)】
\`\`\`json
${telemetryJson}
\`\`\`

请优先结合上述平台真实遥测给出 Markdown 诊断分析，并在回复末尾附带 OpenUI Lang 代码块渲染 PlatformDiagnosisCard 与 AccountDiagnosticTable 方便运维人员一键自愈。`;
}

function buildUserPayloadForSkill(
  rawText: string,
  skill: CopilotSkill,
  liveDiag: DiagnosticsReport | null
): string {
  if (!liveDiag || (skill !== "diagnose" && skill !== "fix")) {
    return rawText;
  }
  const readyAccounts = liveDiag.pool_stats ? liveDiag.pool_stats.active : 0;
  const totalAccounts = liveDiag.pool_stats ? liveDiag.pool_stats.total : 0;
  const trippedProxies = liveDiag.tripped_proxies ? liveDiag.tripped_proxies.length : 0;
  const rootCauses = liveDiag.root_causes ? liveDiag.root_causes.length : 0;

  const statusSummary = `[实时遥测: 健康分=${liveDiag.health_score}, 可用账号=${readyAccounts}/${totalAccounts}, 熔断代理数=${trippedProxies}, 异常根因数=${rootCauses}]`;
  return `${rawText}\n\n${statusSummary}`;
}

export function generateSmartTitle(prompt: string): string {
  let clean = prompt.trim();
  clean = clean.replace(/^\/(image|diagnose|fix|openui|voice|crawl|clear)\s*/i, "");
  clean = clean.replace(/^(gpt-image-[^\s]+|dall-e-[^\s]+|flux-[^\s]+|wanx-[^\s]+|jimeng-[^\s]+|auto)\s*/i, "");
  clean = clean.replace(/^(以当前模型最大的能力|请帮我|帮我|请生成|请绘制|生成一个|生成一张|制作一个|设计一个|画一个|画一张)\s*/i, "");
  clean = clean.trim();
  if (!clean) return "新建对话";
  const runes = Array.from(clean);
  if (runes.length > 20) {
    return runes.slice(0, 20).join("") + "...";
  }
  return clean;
}

export interface GenerativeWorkbenchProps {
  themeMode?: "light" | "dark";
}

export function GenerativeWorkbench({ themeMode = "dark" }: GenerativeWorkbenchProps): React.ReactElement {
  const { catalog, imageModels, audioModels } = useModelsCatalog();
  const [model, setModel] = useState<string>(getStoredChatModel);
  const [autoSwitchEnabled, setAutoSwitchEnabled] = useState<boolean>(() => {
    try {
      const saved = localStorage.getItem("ai_web_free_auto_switch_model");
      return saved === null ? true : saved === "true";
    } catch {
      return true;
    }
  });
  const [apiKey, setApiKey] = useState<string>("");
  const [input, setInput] = useState<string>("");
  const [messages, setMessages] = useState<StoredChatMessage[]>([]);
  const [isSending, setIsSending] = useState<boolean>(false);
  const [copiedPrompt, setCopiedPrompt] = useState<boolean>(false);
  const [copiedMsgId, setCopiedMsgId] = useState<string | null>(null);

  // ─── 多会话状态 ────────────────────────────────────────────────────────
  const [conversations, setConversations] = useState<StoredConversation[]>([]);
  const [activeConvId, setActiveConvId] = useState<string>("default");
  const [isDrawerOpen, setIsDrawerOpen] = useState<boolean>(true);
  const [sidebarFilter, setSidebarFilter] = useState<string>("");
  const [editingConvId, setEditingConvId] = useState<string | null>(null);
  const [editingTitle, setEditingTitle] = useState<string>("");

  // ─── 预设角色 ──────────────────────────────────────────────────────────
  const [presets, setPresets] = useState<ServerChatPreset[]>([]);
  const [selectedPresetId, setSelectedPresetId] = useState<string>("preset-copilot");

  // ─── 全局全文搜索弹窗 ──────────────────────────────────────────────────
  const [isSearchModalOpen, setIsSearchModalOpen] = useState<boolean>(false);
  const [searchModalQuery, setSearchModalQuery] = useState<string>("");
  const [searchHits, setSearchHits] = useState<ServerSearchHit[]>([]);
  const [isSearching, setIsSearching] = useState<boolean>(false);

  // ─── 重新生成菜单 ──────────────────────────────────────────────────────
  const [retryDropdownMsgId, setRetryDropdownMsgId] = useState<string | null>(null);

  // ─── Chat Remix 参考图状态 ───────────────────────────────────────────────
  const [pendingRemixImage, setPendingRemixImage] = useState<PendingChatRemix | null>(null);
  const [remixMode, setRemixMode] = useState<"edit" | "chat">("edit");

  useEffect(() => {
    const applyRemix = (item: PendingChatRemix) => {
      setPendingRemixImage(item);
      setRemixMode("edit");
      setInput((prev) =>
        prev.trim()
          ? prev
          : `基于这张图进行修改：保持主体构图不变，`,
      );
      setTimeout(() => textareaRef.current?.focus(), 60);
    };

    const initial = consumePendingChatRemix();
    if (initial) {
      applyRemix(initial);
    }

    const onRemixEvent = (e: Event) => {
      const detail = (e as CustomEvent<PendingChatRemix>).detail;
      if (detail?.imageUrl) {
        consumePendingChatRemix();
        applyRemix(detail);
      }
    };
    window.addEventListener(CHAT_REMIX_EVENT, onRemixEvent);
    return () => window.removeEventListener(CHAT_REMIX_EVENT, onRemixEvent);
  }, []);

  const abortRef = useRef<AbortController | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);
  const searchInputRef = useRef<HTMLInputElement | null>(null);
  const initialLoadDoneRef = useRef<boolean>(false);

  const availableModels = useMemo(() => {
    const list = catalog
      .filter(
        (m) =>
          !m.id.includes("image") &&
          !m.id.includes("dall-e") &&
          !m.id.includes("tts") &&
          !m.id.includes("transcribe") &&
          !m.id.includes("whisper")
      )
      .map((m) => m.id);
    return list.length > 0 ? list : FALLBACK_CHAT_MODELS;
  }, [catalog]);

  const availableImageModels = useMemo(() => {
    const list = imageModels.map((m) => m.id);
    return list.length > 0 ? list : DEFAULT_IMAGE_MODELS;
  }, [imageModels]);

  const availableAudioModels = useMemo(() => {
    const list = audioModels
      .map((m) => m.id)
      .filter((id) => isTtsModelId(id, DEFAULT_TTS_MODELS));
    return list.length > 0 ? list : DEFAULT_TTS_MODELS;
  }, [audioModels]);

  const toggleAutoSwitch = () => {
    setAutoSwitchEnabled((prev) => {
      const next = !prev;
      try {
        localStorage.setItem("ai_web_free_auto_switch_model", String(next));
      } catch {
        // ignore
      }
      return next;
    });
  };

  // 1. 初始化加载预设与会话列表
  useEffect(() => {
    void (async () => {
      try {
        const pList = await fetchServerChatPresets();
        if (pList && pList.length > 0) {
          setPresets(pList);
        }
      } catch {
        // ignore
      }

      try {
        const convs = await loadUnifiedConversations();
        if (convs && convs.length > 0) {
          setConversations(convs);
          const targetId = convs[0].id;
          setActiveConvId(targetId);
          const msgs = await loadUnifiedConversationMessages(targetId);
          if (msgs.length > 0) {
            setMessages(msgs);
          } else {
            setMessages([
              {
                id: "init-welcome",
                conversationId: targetId,
                role: "assistant",
                content: INITIAL_COPILOT_MESSAGE,
                openuiMode: true,
                skill: "auto",
                timestamp: Date.now(),
              },
            ]);
          }
        } else {
          const created = await createUnifiedConversation({
            title: "平台运维与诊断",
            model: "gpt-5-6",
            presetId: "preset-copilot",
          });
          setConversations([created]);
          setActiveConvId(created.id);
          const welcome: StoredChatMessage = {
            id: "init-welcome",
            conversationId: created.id,
            role: "assistant",
            content: INITIAL_COPILOT_MESSAGE,
            openuiMode: true,
            skill: "auto",
            timestamp: Date.now(),
          };
          setMessages([welcome]);
          void saveUnifiedConversationMessages(created.id, [welcome]);
        }
      } catch (err) {
        console.warn("Failed to load unified conversations:", err);
        const fallbackConv: StoredConversation = {
          id: "conv-default",
          title: "平台运维与诊断",
          model: "gpt-5-6",
          presetId: "preset-copilot",
          isPinned: false,
          isArchived: false,
          messageCount: 0,
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
          timestamp: Date.now(),
        };
        setConversations([fallbackConv]);
        setActiveConvId(fallbackConv.id);
        setMessages([
          {
            id: "init-welcome",
            conversationId: fallbackConv.id,
            role: "assistant",
            content: INITIAL_COPILOT_MESSAGE,
            openuiMode: true,
            skill: "auto",
            timestamp: Date.now(),
          },
        ]);
      } finally {
        initialLoadDoneRef.current = true;
      }
    })();
  }, []);

  // 2. 消息变更时自动写回当前会话（跳过流式/路由/生图进行中的临时占位消息）
  useEffect(() => {
    if (!initialLoadDoneRef.current || !activeConvId) return;
    if (messages.length === 0) return;
    if (messages.some((m) => m.isStreaming)) return;
    const t = window.setTimeout(() => {
      void saveUnifiedConversationMessages(activeConvId, messages);
      setConversations((prev) =>
        prev.map((c) =>
          c.id === activeConvId
            ? { ...c, messageCount: messages.length, updatedAt: new Date().toISOString() }
            : c
        )
      );
    }, 400);
    return () => window.clearTimeout(t);
  }, [messages, activeConvId]);

  // 3. 自动滚动到底部
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [messages]);

  // 4. 监听 Ctrl+K / Cmd+K 快捷呼出全文检索
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setIsSearchModalOpen((prev) => !prev);
      }
      if (e.key === "Escape" && isSearchModalOpen) {
        setIsSearchModalOpen(false);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [isSearchModalOpen]);

  useEffect(() => {
    if (isSearchModalOpen) {
      window.setTimeout(() => searchInputRef.current?.focus(), 50);
    }
  }, [isSearchModalOpen]);

  const activeConv = useMemo(() => {
    return conversations.find((c) => c.id === activeConvId);
  }, [conversations, activeConvId]);

  // 时间维度分组会话列表
  const groupedConversations = useMemo(() => {
    const filter = sidebarFilter.trim().toLowerCase();
    const filtered = filter
      ? conversations.filter(
          (c) =>
            c.title.toLowerCase().includes(filter) ||
            (c.tags && c.tags.toLowerCase().includes(filter))
        )
      : conversations;

    const pinned: StoredConversation[] = [];
    const today: StoredConversation[] = [];
    const yesterday: StoredConversation[] = [];
    const last7Days: StoredConversation[] = [];
    const older: StoredConversation[] = [];

    const now = new Date();
    const todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
    const yesterdayStart = todayStart - 86400000;
    const sevenDaysStart = todayStart - 6 * 86400000;

    for (const c of filtered) {
      if (c.isPinned) {
        pinned.push(c);
        continue;
      }
      const t = c.timestamp || (c.updatedAt ? Date.parse(c.updatedAt) : 0);
      if (t >= todayStart) {
        today.push(c);
      } else if (t >= yesterdayStart) {
        yesterday.push(c);
      } else if (t >= sevenDaysStart) {
        last7Days.push(c);
      } else {
        older.push(c);
      }
    }

    return { pinned, today, yesterday, last7Days, older };
  }, [conversations, sidebarFilter]);

  // 切换会话
  const handleSelectConversation = async (convId: string) => {
    if (convId === activeConvId) return;
    if (isSending && abortRef.current) {
      abortRef.current.abort();
    }
    if (activeConvId && messages.length > 0) {
      void saveUnifiedConversationMessages(activeConvId, messages);
    }
    setActiveConvId(convId);
    const msgs = await loadUnifiedConversationMessages(convId);
    if (msgs.length > 0) {
      setMessages(msgs);
    } else {
      setMessages([
        {
          id: "msg-" + Date.now(),
          conversationId: convId,
          role: "assistant",
          content: "已切换到新会话，有什么我可以协助你的？",
          openuiMode: false,
          skill: "auto",
          timestamp: Date.now(),
        },
      ]);
    }
  };

  // 创建新会话
  const handleCreateNewConversation = async () => {
    if (isSending && abortRef.current) {
      abortRef.current.abort();
    }
    if (activeConvId && messages.length > 0) {
      void saveUnifiedConversationMessages(activeConvId, messages);
    }
    const currentPreset = presets.find((p) => p.id === selectedPresetId);
    const newTitle = currentPreset ? `${currentPreset.name} 对话` : "新对话";
    const newConv = await createUnifiedConversation({
      title: newTitle,
      model: currentPreset?.default_model || model,
      presetId: selectedPresetId,
      systemPrompt: currentPreset?.system_prompt,
    });
    setConversations((prev) => [newConv, ...prev]);
    setActiveConvId(newConv.id);
    const initMsg: StoredChatMessage = {
      id: "msg-" + Date.now(),
      conversationId: newConv.id,
      role: "assistant",
      content: currentPreset
        ? `您好！我是 **${currentPreset.name}**。\n\n${currentPreset.description}\n\n请问今天有什么技术任务需要我处理？`
        : INITIAL_COPILOT_MESSAGE,
      openuiMode: !currentPreset,
      skill: "auto",
      timestamp: Date.now(),
    };
    setMessages([initMsg]);
    void saveUnifiedConversationMessages(newConv.id, [initMsg]);
  };

  // 置顶切换
  const handleTogglePin = async (e: React.MouseEvent, conv: StoredConversation) => {
    e.stopPropagation();
    const nextPin = !conv.isPinned;
    await updateUnifiedConversation(conv.id, { isPinned: nextPin });
    setConversations((prev) =>
      prev.map((c) => (c.id === conv.id ? { ...c, isPinned: nextPin } : c))
    );
  };

  // 原地重命名
  const handleStartRename = (e: React.MouseEvent, conv: StoredConversation) => {
    e.stopPropagation();
    setEditingConvId(conv.id);
    setEditingTitle(conv.title);
  };

  const handleSaveRename = async () => {
    if (!editingConvId) return;
    const finalTitle = editingTitle.trim() || "未命名对话";
    await updateUnifiedConversation(editingConvId, { title: finalTitle });
    setConversations((prev) =>
      prev.map((c) => (c.id === editingConvId ? { ...c, title: finalTitle } : c))
    );
    setEditingConvId(null);
    setEditingTitle("");
  };

  // 删除会话
  const handleDeleteConversation = async (e: React.MouseEvent, convId: string) => {
    e.stopPropagation();
    if (!window.confirm("确定要删除此会话及其所有历史消息吗？")) return;
    await deleteUnifiedConversation(convId);
    const remaining = conversations.filter((c) => c.id !== convId);
    setConversations(remaining);
    if (activeConvId === convId) {
      if (remaining.length > 0) {
        void handleSelectConversation(remaining[0].id);
      } else {
        void handleCreateNewConversation();
      }
    }
  };

  // 导出会话为 Markdown
  const handleExportMarkdown = async (convId = activeConvId) => {
    try {
      const text = await exportServerConversationMarkdown(convId);
      const blob = new Blob([text], { type: "text/markdown;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${activeConv?.title || "chat-export"}-${convId.slice(0, 8)}.md`;
      a.click();
      URL.revokeObjectURL(url);
    } catch {
      alert("导出 Markdown 失败，请检查网络连接");
    }
  };

  // 导出全部会话 JSON
  const handleExportJSON = () => {
    const dataStr =
      "data:text/json;charset=utf-8," +
      encodeURIComponent(
        JSON.stringify(
          {
            exported_at: new Date().toISOString(),
            conversations,
            current_messages: messages,
          },
          null,
          2
        )
      );
    const a = document.createElement("a");
    a.href = dataStr;
    a.download = `ai-web-free-chat-${new Date().toISOString().slice(0, 10)}.json`;
    a.click();
  };

  // 导入会话 JSON
  const handleImportJSON = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    try {
      const text = await file.text();
      const payload = JSON.parse(text);
      if (payload && Array.isArray(payload.conversations)) {
        await importServerConversations(payload);
        const refreshed = await loadUnifiedConversations();
        setConversations(refreshed);
        alert(`成功导入 ${payload.conversations.length} 个历史会话`);
      } else {
        alert("JSON 格式不匹配，未包含 conversations 数组");
      }
    } catch (err) {
      alert("导入失败: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      e.target.value = "";
    }
  };

  // 全文搜索
  const handleSearchSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const q = searchModalQuery.trim();
    if (!q) return;
    setIsSearching(true);
    try {
      const res = await searchServerChat(q);
      setSearchHits(res.hits || []);
    } catch {
      setSearchHits([]);
    } finally {
      setIsSearching(false);
    }
  };

  const handleJumpToSearchHit = async (hit: ServerSearchHit) => {
    setIsSearchModalOpen(false);
    await handleSelectConversation(hit.conversation_id);
  };

  // 角色预设切换
  const handleSelectPreset = (presetId: string) => {
    setSelectedPresetId(presetId);
    const preset = presets.find((p) => p.id === presetId);
    if (!preset || !activeConvId) return;

    if (preset.default_model) {
      setModel(preset.default_model);
    }
    void updateUnifiedConversation(activeConvId, {
      presetId,
      systemPrompt: preset.system_prompt,
      model: preset.default_model || model,
    });
  };

  // 消息高频操作：编辑提问
  const handleEditPrompt = (msg: StoredChatMessage) => {
    setInput(msg.content);
    const msgIdx = messages.findIndex((m) => m.id === msg.id);
    if (msgIdx !== -1) {
      setMessages(messages.slice(0, msgIdx));
    }
    window.requestAnimationFrame(() => {
      textareaRef.current?.focus();
    });
  };

  // 消息高频操作：复制
  const handleCopyMessage = async (msgId: string, content: string) => {
    try {
      await navigator.clipboard.writeText(content);
      setCopiedMsgId(msgId);
      window.setTimeout(() => setCopiedMsgId(null), 1600);
    } catch {
      // ignore
    }
  };

  // 消息高频操作：停止生成
  const handleStopGenerating = () => {
    if (abortRef.current) {
      abortRef.current.abort();
      abortRef.current = null;
    }
    setIsSending(false);
  };

  // 核心发送函数
  const handleSendMessage = async (
    overridePrompt?: string,
    targetModel = model,
    baseMessages?: StoredChatMessage[],
    hasExplicitModelOverride = false
  ) => {
    let promptText = (overridePrompt !== undefined ? overridePrompt : input).trim();
    if (!promptText || isSending) return;
    const historyMessages = baseMessages ?? messages;

    if (!overridePrompt) {
      setInput("");
    }

    if (promptText === "/clear") {
      setMessages([]);
      if (activeConvId) {
        void saveUnifiedConversationMessages(activeConvId, []);
      }
      return;
    }

    // ─── 确保绑定有效会话与首轮提问智能提取标题 ────────────────────────────────
    let currentConvId = activeConvId;
    let currentConv = conversations.find((c) => c.id === currentConvId);

    if (!currentConv) {
      const smartTitle = generateSmartTitle(promptText);
      const created = await createUnifiedConversation({
        title: smartTitle,
        model: targetModel,
        presetId: selectedPresetId,
      });
      currentConvId = created.id;
      setActiveConvId(created.id);
      setConversations((prev) => [created, ...prev.filter((c) => c.id !== created.id)]);
      currentConv = created;
    } else if (
      (currentConv.title === "新建对话" ||
        currentConv.title === "新对话" ||
        currentConv.title === "平台运维与诊断" ||
        currentConv.title.endsWith("对话")) &&
      historyMessages.filter((m) => m.role === "user").length <= 1
    ) {
      const smartTitle = generateSmartTitle(promptText);
      void updateUnifiedConversation(currentConv.id, { title: smartTitle });
      setConversations((prev) =>
        prev.map((c) => (c.id === currentConv!.id ? { ...c, title: smartTitle } : c))
      );
    }

    // ─── Chat Remix 参考图直连处理 ──────────────────────────────────────────
    const activeRemix = pendingRemixImage;
    if (activeRemix) {
      setPendingRemixImage(null);
      if (remixMode === "edit") {
        const chosenImageModel =
          activeRemix.model && availableImageModels.includes(activeRemix.model)
            ? activeRemix.model
            : availableImageModels[0] || "gpt-image-2.5-sunburst";
        const userMessageId = "user-" + Date.now();
        const assistantId = "assistant-" + (Date.now() + 1);
        const userDisplayContent = `![Remix 参考图](${activeRemix.imageUrl})\n\n${promptText}`;

        const nextUserMsg: StoredChatMessage = {
          id: userMessageId,
          conversationId: currentConvId,
          role: "user",
          content: userDisplayContent,
          openuiMode: false,
          skill: "image",
          timestamp: Date.now(),
        };
        const nextAssistantMsg: StoredChatMessage = {
          id: assistantId,
          conversationId: currentConvId,
          role: "assistant",
          content: `🎨 正在基于参考图调用 \`${chosenImageModel}\` 执行 Chat Remix 重绘...`,
          model: chosenImageModel,
          openuiMode: false,
          skill: "image",
          isStreaming: true,
          timestamp: Date.now(),
        };

        const updated = [...historyMessages, nextUserMsg, nextAssistantMsg];
        setMessages(updated);
        setIsSending(true);
        const startTime = Date.now();

        try {
          const imgResp = await fetch(activeRemix.imageUrl);
          const imgBlob = await imgResp.blob();
          const ext = imgBlob.type.includes("jpeg") ? "jpg" : "png";
          const refFile = new File([imgBlob], `remix-ref.${ext}`, {
            type: imgBlob.type || "image/png",
          });

          const res = await editImageWithReferences({
            prompt: promptText,
            model: chosenImageModel,
            size: activeRemix.size || "1024x1024",
            files: [refFile],
            apiKey: apiKey.trim() || undefined,
          });

          const elapsedMs = Date.now() - startTime;
          const item = res.data?.[0];
          let imgUrl = item?.url || (item?.b64_json ? `data:image/png;base64,${item.b64_json}` : "");
          if (!imgUrl) {
            throw new Error("服务端 Chat Remix 未返回有效图像地址");
          }

          let finalPersistentUrl = imgUrl;
          try {
            const localBlob = item?.b64_json ? base64ToBlob(item.b64_json) || undefined : undefined;
            const persistedAsset = await saveUnifiedImage(
              {
                id: `img-remix-${Date.now()}`,
                blob: localBlob,
                remoteUrl: item?.url,
                prompt: promptText,
                model: chosenImageModel,
                size: activeRemix.size || "1024x1024",
                quality: "hd",
                mode: "edits",
                latencyMs: elapsedMs,
                createdAt: new Date().toLocaleTimeString("zh-CN", { hour12: false }),
                timestamp: Date.now(),
              },
              item?.b64_json,
            );
            if (persistedAsset?.url) {
              finalPersistentUrl = persistedAsset.url;
            }
          } catch {
            // ignore gallery persistence failure
          }

          const safeImageUrl = finalPersistentUrl.trim();
          const successMarkdown = [
            `![Chat Remix 结果](${safeImageUrl})`,
            "",
            `✨ **Chat Remix 重绘完成** · \`${chosenImageModel}\` · ${elapsedMs}ms`,
            `- [在新标签页查看高清原图](${safeImageUrl}) · [前往生图工坊/创作画廊管理](#/image)`,
          ].join("\n");

          const completedAssistantMsg: StoredChatMessage = {
            ...nextAssistantMsg,
            content: successMarkdown,
            isStreaming: false,
            latencyMs: elapsedMs,
          };
          const finalMessages = [...historyMessages, nextUserMsg, completedAssistantMsg];
          setMessages(finalMessages);
          void saveUnifiedConversationMessages(currentConvId, finalMessages);
        } catch (err: any) {
          const failedAssistantMsg: StoredChatMessage = {
            ...nextAssistantMsg,
            content: `❌ **Chat Remix 重绘失败**\n\n> ${err?.message || String(err)}`,
            isStreaming: false,
          };
          setMessages([...historyMessages, nextUserMsg, failedAssistantMsg]);
        } finally {
          setIsSending(false);
        }
        return;
      }
      // remixMode === "chat": 将参考图上下文附带到文本对话中
      promptText = `![参考图](${activeRemix.imageUrl})\n\n[原始提示词] ${activeRemix.prompt || "无"}\n\n${promptText}`;
    }

    // ─── 全工坊爬虫优先路由：/voice、/crawl 或自然语言搜索素材克隆/入库 ─────
    const isExplicitCrawlCmd =
      /^\/(voice|crawl-voice|crawl)\b/i.test(promptText.trim()) ||
      MEDIA_CRAWL_INTENT_RE.test(promptText);
    if (isExplicitCrawlCmd) {
      const parsedCrawl = parseMediaCrawlIntent(promptText);
      const userMessageId = "user-" + Date.now();
      const assistantId = "assistant-" + (Date.now() + 1);
      const nextUserMsg: StoredChatMessage = {
        id: userMessageId,
        conversationId: currentConvId,
        role: "user",
        content: promptText,
        openuiMode: false,
        skill: "media_crawl",
        timestamp: Date.now(),
      };
      const nextAssistantMsg: StoredChatMessage = {
        id: assistantId,
        conversationId: currentConvId,
        role: "assistant",
        content: "🌐 正在检索系统现有缓存库并爬取外部匹配资源...",
        model: "crawler",
        openuiMode: false,
        skill: "media_crawl",
        isStreaming: true,
        timestamp: Date.now(),
      };
      setMessages([...historyMessages, nextUserMsg, nextAssistantMsg]);
      setIsSending(true);

      const escapeAttr = (v: string) =>
        v.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\n/g, " ");

      const startTime = Date.now();
      try {
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

        const openuiBlock = [
          "```openui",
          "root = Card([header, crawler])",
          `header = CardHeader("跨工坊素材爬虫与系统库入库台", "检索词: ${escapeAttr(parsedCrawl.query || parsedCrawl.targetUrl || "全库检索")} · 现有命中 语音${sysVoiceCount}/图像${sysImageCount}/视频${sysVideoCount} · 新抓取 ${candCount} 项")`,
          `crawler = MediaCrawlerCard("${escapeAttr(parsedCrawl.query)}", "${escapeAttr(parsedCrawl.targetUrl)}", "${parsedCrawl.mediaKind}", "${escapeAttr(parsedCrawl.role)}")`,
          "```",
        ].join("\n");

        const summaryMarkdown = [
          `🌐 **已完成系统缓存检索与网页媒体爬虫扫描** (${elapsedMs}ms)`,
          `- **系统现有库命中**：音色 ${sysVoiceCount} 个 · 图像 ${sysImageCount} 张 · 视频 ${sysVideoCount} 条`,
          `- **网络爬虫新发现**：${candCount} 条候选（可在下方卡片内直接试听/预览，点击按钮即可 **缓存本地克隆或写入系统库**，并在「配音/生图/视频/短剧工坊」中直接复用）`,
          "",
          openuiBlock,
        ].join("\n");

        const completedMsg: StoredChatMessage = {
          ...nextAssistantMsg,
          content: summaryMarkdown,
          openuiMode: true,
          isStreaming: false,
          latencyMs: elapsedMs,
        };
        const finalMessages = [...historyMessages, nextUserMsg, completedMsg];
        setMessages(finalMessages);
        if (currentConvId) {
          void saveUnifiedConversationMessages(currentConvId, finalMessages);
          void updateUnifiedConversation(currentConvId, { messageCount: finalMessages.length });
        }
      } catch (err: any) {
        const errText = err?.message || "媒体爬虫检索异常";
        const failMsg: StoredChatMessage = {
          ...nextAssistantMsg,
          content: `❌ **素材爬虫调用失败**：${errText}`,
          isStreaming: false,
          latencyMs: Date.now() - startTime,
        };
        const finalMessages = [...historyMessages, nextUserMsg, failMsg];
        setMessages(finalMessages);
      } finally {
        setIsSending(false);
      }
      return;
    }

    // ─── 统一自然语言语义路由（生图 / 语音 / 深度研究 / 显式点名模型） ──────────
    const routeController = new AbortController();
    abortRef.current = routeController;
    const routingMsgId = "routing-" + Date.now();
    const needsSemanticCheck =
      autoSwitchEnabled &&
      !promptText.trim().startsWith("/") &&
      !hasExplicitModelOverride &&
      !isImageModelId(targetModel, availableImageModels) &&
      !isTtsModelId(targetModel, availableAudioModels);

    if (needsSemanticCheck) {
      const pendingUserMsg: StoredChatMessage = {
        id: "user-" + Date.now(),
        conversationId: currentConvId,
        role: "user",
        content: promptText,
        openuiMode: false,
        timestamp: Date.now(),
      };
      const pendingRouteMsg: StoredChatMessage = {
        id: routingMsgId,
        conversationId: currentConvId,
        role: "assistant",
        content: "🧭 正在理解您的自然语言意图...",
        model: targetModel,
        openuiMode: false,
        isStreaming: true,
        timestamp: Date.now(),
      };
      setMessages([...historyMessages, pendingUserMsg, pendingRouteMsg]);
      setIsSending(true);
    }

    const routerChatModel =
      isImageModelId(targetModel, availableImageModels) ||
      isTtsModelId(targetModel, availableAudioModels)
        ? availableModels[0] || "gpt-5-6"
        : targetModel;

    const route = await resolveSemanticModelRoute({
      promptText,
      selectedModel: targetModel,
      hasExplicitModelOverride,
      autoSwitchEnabled,
      history: historyMessages,
      availableChatModels: availableModels,
      availableImageModels,
      availableAudioModels,
      classifyWithLLM: ({
        promptText: q,
        history: h,
        chatModels,
        imageModels,
        audioModels,
      }) =>
        classifySemanticRouteWithLLM({
          promptText: q,
          history: h,
          chatModel: routerChatModel,
          chatModels,
          imageModels,
          audioModels,
          apiKey: apiKey.trim() || undefined,
          signal: routeController.signal,
        }),
    });

    if (routeController.signal.aborted) {
      setIsSending(false);
      abortRef.current = null;
      return;
    }

    const effectiveChatModel = route.targetModel;

    if (route.modality === "image") {
      const chosenImageModel = route.targetModel;
      const userMessageId = "user-" + Date.now();
      const assistantId = "assistant-" + (Date.now() + 1);

      const nextUserMsg: StoredChatMessage = {
        id: userMessageId,
        conversationId: currentConvId,
        role: "user",
        content: promptText,
        openuiMode: false,
        skill: "image",
        timestamp: Date.now(),
      };

      const initialContent = route.needsContextExtraction
        ? `🧠 正在结合上下文提炼画面描述，并切换至 \`${chosenImageModel}\`...`
        : `🎨 正在调用 \`${chosenImageModel}\`${
            route.isAutoImage
              ? " *(auto 模式)*"
              : route.switched
                ? ` *(${route.reason || "自动切换"})*`
                : ""
          } 绘制图像...`;

      const nextAssistantMsg: StoredChatMessage = {
        id: assistantId,
        conversationId: currentConvId,
        role: "assistant",
        content: initialContent,
        model: chosenImageModel,
        openuiMode: false,
        skill: "image",
        isStreaming: true,
        timestamp: Date.now(),
      };

      const updated = [...historyMessages, nextUserMsg, nextAssistantMsg];
      setMessages(updated);
      setIsSending(true);

      const startTime = Date.now();
      const controller = new AbortController();
      abortRef.current = controller;

      try {
        let finalImagePrompt = route.extractedPrompt || route.cleanedPrompt;

        if (!route.extractedPrompt && route.needsContextExtraction) {
          const extractionChatModel =
            isImageModelId(model, availableImageModels) ||
            isTtsModelId(model, availableAudioModels)
              ? availableModels[0] || "gpt-5-6"
              : model;
          finalImagePrompt = await extractImagePromptFromContext({
            userRequest: promptText,
            history: historyMessages,
            chatModel: extractionChatModel,
            apiKey: apiKey.trim() || undefined,
            signal: controller.signal,
          });

          const extractedPreview = formatSingleLinePrompt(finalImagePrompt, 48);
          setMessages((prev) =>
            prev.map((m) =>
              m.id === assistantId
                ? {
                    ...m,
                    content: `🎨 正在使用 \`${chosenImageModel}\` 绘制图像${
                      extractedPreview ? ` · ${extractedPreview}` : "..."
                    }`,
                  }
                : m
            )
          );
        }

        const res = await generateImage({
          prompt: finalImagePrompt,
          model: chosenImageModel,
          size: "1024x1024",
          quality: "hd",
          apiKey: apiKey.trim() || undefined,
        });

        const elapsedMs = Date.now() - startTime;
        const item = res.data?.[0];
        let imgUrl = "";
        if (item?.url) {
          imgUrl = item.url;
        } else if (item?.b64_json) {
          imgUrl = `data:image/png;base64,${item.b64_json}`;
        }

        if (!imgUrl) {
          throw new Error("服务端生图未返回有效图像地址");
        }

        const assetId = `img-chat-${Date.now()}`;
        let localBlob: Blob | undefined;
        if (item?.b64_json) {
          localBlob = base64ToBlob(item.b64_json) || undefined;
        }

        const imageRecord: StoredImageRecord = {
          id: assetId,
          blob: localBlob,
          remoteUrl: item?.url,
          prompt: finalImagePrompt,
          revisedPrompt: item?.revised_prompt,
          model: chosenImageModel,
          size: "1024x1024",
          quality: "hd",
          mode: "generations",
          latencyMs: elapsedMs,
          createdAt: new Date().toLocaleTimeString("zh-CN", { hour12: false }),
          timestamp: Date.now(),
        };

        let finalPersistentUrl = imgUrl;
        try {
          const persistedAsset = await saveUnifiedImage(imageRecord, item?.b64_json);
          if (persistedAsset?.url) {
            finalPersistentUrl = persistedAsset.url;
          }
        } catch (saveAssetErr) {
          console.warn("Auto save to gallery asset failed:", saveAssetErr);
        }

        const modelTagSuffix = route.isAutoImage
          ? " *(auto 模式自动挑选)*"
          : route.switched
            ? ` *(${route.reason || "自动切换"})*`
            : "";

        const safeImageUrl = finalPersistentUrl.trim();
        const imageAlt =
          formatSingleLinePrompt(finalImagePrompt, 60, true) || "AI 生成图片";

        const successMarkdown = [
          `![${imageAlt}](${safeImageUrl})`,
          "",
          `✨ **图像生成完成** · \`${chosenImageModel}\`${modelTagSuffix} · ${elapsedMs}ms`,
          `- [在新标签页查看高清原图](${safeImageUrl}) · [前往生图工坊/创作画廊管理](#/image)`,
        ].join("\n");

        const completedAssistantMsg: StoredChatMessage = {
          ...nextAssistantMsg,
          content: successMarkdown,
          model: chosenImageModel,
          isStreaming: false,
          latencyMs: elapsedMs,
        };

        const finalMessages = [...historyMessages, nextUserMsg, completedAssistantMsg];
        setMessages(finalMessages);
        if (currentConvId) {
          void saveUnifiedConversationMessages(currentConvId, finalMessages);
          void updateUnifiedConversation(currentConvId, {
            messageCount: finalMessages.length,
          });
        }
      } catch (err: any) {
        const elapsedMs = Date.now() - startTime;
        const errMsg = err?.message || "图像生成请求异常";
        const failMarkdown = [
          `❌ **图像生成失败** (${chosenImageModel})`,
          `> 错误信息：${errMsg}`,
          "",
          "提示：您可以点击消息下方的 **重试** 再次尝试，或点击 **换模型** 切换其他图像模型重新生成。",
        ].join("\n");

        const failedAssistantMsg: StoredChatMessage = {
          ...nextAssistantMsg,
          content: failMarkdown,
          model: chosenImageModel,
          isStreaming: false,
          latencyMs: elapsedMs,
        };

        const finalMessages = [...historyMessages, nextUserMsg, failedAssistantMsg];
        setMessages(finalMessages);
        if (currentConvId) {
          void saveUnifiedConversationMessages(currentConvId, finalMessages);
        }
      } finally {
        abortRef.current = null;
        setIsSending(false);
      }
      return;
    }

    if (route.modality === "audio") {
      const chosenAudioModel = route.targetModel;
      const userMessageId = "user-" + Date.now();
      const assistantId = "assistant-" + (Date.now() + 1);

      const nextUserMsg: StoredChatMessage = {
        id: userMessageId,
        conversationId: currentConvId,
        role: "user",
        content: promptText,
        openuiMode: false,
        skill: "audio",
        timestamp: Date.now(),
      };

      const initialContent = route.needsContextExtraction
        ? `🎙️ **检测到${route.reason || "语音合成意图"}，正在结合上下文提取文本并切换至语音模型** \`${chosenAudioModel}\`...`
        : `🎙️ **正在调用语音模型** \`${chosenAudioModel}\`${
            route.switched ? ` *(${route.reason || "自动切换"})*` : ""
          } 合成音频...`;

      const nextAssistantMsg: StoredChatMessage = {
        id: assistantId,
        conversationId: currentConvId,
        role: "assistant",
        content: initialContent,
        model: chosenAudioModel,
        openuiMode: false,
        skill: "audio",
        isStreaming: true,
        timestamp: Date.now(),
      };

      const updated = [...historyMessages, nextUserMsg, nextAssistantMsg];
      setMessages(updated);
      setIsSending(true);

      const startTime = Date.now();
      const controller = new AbortController();
      abortRef.current = controller;

      try {
        let speechText = route.cleanedPrompt;
        if (route.needsContextExtraction) {
          const extractionChatModel =
            isImageModelId(model, availableImageModels) ||
            isTtsModelId(model, availableAudioModels)
              ? availableModels[0] || "gpt-5-6"
              : model;
          speechText = await extractSpeechTextFromContext({
            userRequest: promptText,
            history: historyMessages,
            chatModel: extractionChatModel,
            apiKey: apiKey.trim() || undefined,
            signal: controller.signal,
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
          `- **所用模型**：\`${chosenAudioModel}\`${route.switched ? ` *(${route.reason || "自动切换"})*` : ""}`,
          `- **朗读正文**：${speechText.length > 180 ? `${speechText.slice(0, 180)}...` : speechText}`,
        ].join("\n");

        const completedAssistantMsg: StoredChatMessage = {
          ...nextAssistantMsg,
          content: successMarkdown,
          model: chosenAudioModel,
          isStreaming: false,
          latencyMs: elapsedMs,
        };

        const finalMessages = [...historyMessages, nextUserMsg, completedAssistantMsg];
        setMessages(finalMessages);
        if (currentConvId) {
          void saveUnifiedConversationMessages(currentConvId, finalMessages);
          void updateUnifiedConversation(currentConvId, {
            messageCount: finalMessages.length,
          });
        }
      } catch (err: any) {
        const elapsedMs = Date.now() - startTime;
        const errMsg = err?.message || "语音合成请求异常";
        const failMarkdown = [
          `❌ **语音合成失败** (${chosenAudioModel})`,
          `> 错误信息：${errMsg}`,
          "",
          "提示：您可以点击消息下方的 **重试** 或 **换模型** 切换其他语音/对话模型。",
        ].join("\n");

        const failedAssistantMsg: StoredChatMessage = {
          ...nextAssistantMsg,
          content: failMarkdown,
          model: chosenAudioModel,
          isStreaming: false,
          latencyMs: elapsedMs,
        };

        const finalMessages = [...historyMessages, nextUserMsg, failedAssistantMsg];
        setMessages(finalMessages);
        if (currentConvId) {
          void saveUnifiedConversationMessages(currentConvId, finalMessages);
        }
      } finally {
        abortRef.current = null;
        setIsSending(false);
      }
      return;
    }

    const matchedSkillDef = SLASH_SKILLS.find(
      (s) => promptText === s.command || promptText.startsWith(s.command + " ")
    );

    let effectiveSkill: CopilotSkill = "auto";
    if (matchedSkillDef && matchedSkillDef.skill !== "clear") {
      effectiveSkill = matchedSkillDef.skill;
    } else if (OPS_INTENT_RE.test(promptText)) {
      effectiveSkill = "diagnose";
    } else if (IMAGE_INTENT_RE.test(promptText)) {
      effectiveSkill = "image";
    } else if (OPENUI_INTENT_RE.test(promptText)) {
      effectiveSkill = "openui";
    }

    const sendOpenUIHeader =
      effectiveSkill === "openui" ||
      effectiveSkill === "image" ||
      effectiveSkill === "diagnose" ||
      effectiveSkill === "fix";

    const userMessageId = "user-" + Date.now();
    const assistantId = "assistant-" + (Date.now() + 1);

    const nextUserMsg: StoredChatMessage = {
      id: userMessageId,
      conversationId: currentConvId,
      role: "user",
      content: promptText,
      openuiMode: false,
      skill: effectiveSkill,
      timestamp: Date.now(),
    };

    const nextAssistantMsg: StoredChatMessage = {
      id: assistantId,
      conversationId: currentConvId,
      role: "assistant",
      content: "",
      model: effectiveChatModel,
      openuiMode: sendOpenUIHeader,
      skill: effectiveSkill,
      isStreaming: true,
      timestamp: Date.now(),
    };

    const updatedMessages = [...historyMessages, nextUserMsg, nextAssistantMsg];
    setMessages(updatedMessages);
    setIsSending(true);

    const startTime = Date.now();
    const controller = new AbortController();
    abortRef.current = controller;

    let liveDiag: DiagnosticsReport | null = null;
    try {
      if (effectiveSkill === "diagnose" || effectiveSkill === "fix") {
        try {
          liveDiag = await fetchDiagnostics();
        } catch {
          liveDiag = null;
        }
      }

      const activePreset = presets.find((p) => p.id === activeConv?.presetId);
      const systemContent = buildSystemPromptForSkill(
        effectiveSkill,
        liveDiag,
        activePreset?.system_prompt || activeConv?.systemPrompt
      );

      const userPayload = buildUserPayloadForSkill(promptText, effectiveSkill, liveDiag);

      const historyWindow = historyMessages.slice(-10).map((m) => ({
        role: m.role,
        content: m.content,
      }));

      const apiMessages: Array<{ role: string; content: string }> = [
        { role: "system", content: systemContent },
        ...historyWindow,
        { role: "user", content: userPayload },
      ];

      const finalText = await streamChatCompletion({
        messages: apiMessages,
        model: effectiveChatModel,
        openuiMode: sendOpenUIHeader,
        apiKey: apiKey.trim() || undefined,
        signal: controller.signal,
        onDelta: (_delta, fullText) => {
          setMessages((prev) =>
            prev.map((m) =>
              m.id === assistantId
                ? {
                    ...m,
                    content: fullText,
                    openuiMode: containsOpenUIDSL(fullText),
                    isStreaming: true,
                  }
                : m
            )
          );
        },
      });

      const latencyMs = Date.now() - startTime;

      let finalRenderedText = finalText;
      if (
        (effectiveSkill === "diagnose" || effectiveSkill === "fix") &&
        !finalText.includes("PlatformDiagnosisCard")
      ) {
        const diagCardBlock = [
          "```openui",
          "root = Card([diagHeader, diagCard, diagTable])",
          'diagHeader = CardHeader("平台实时诊断与一键修复台", "支持安全自愈（切换健康代理、卡密续期、补充匿名 UUID）与二次确认清理封禁账号")',
          'diagCard = PlatformDiagnosisCard("平台全链路根因诊断与修复台", "all")',
          'diagTable = AccountDiagnosticTable("chatgpt")',
          "```",
        ].join("\n");
        finalRenderedText = `${finalText.trimEnd()}\n\n${diagCardBlock}`;
      }

      if (!finalRenderedText.trim()) {
        finalRenderedText =
          "> ⚠️ 模型本次未返回有效内容，请点击下方 **重试** 或 **换模型** 再试。";
      }

      const completedAssistantMsg: StoredChatMessage = {
        ...nextAssistantMsg,
        content: finalRenderedText,
        model: effectiveChatModel,
        latencyMs,
        openuiMode: containsOpenUIDSL(finalRenderedText),
        isStreaming: false,
      };
      const finalMessages = [...historyMessages, nextUserMsg, completedAssistantMsg];

      // 关键：在 React setState 之外立即同步写回服务端与本地持久化，
      // 防止用户在等待期间切换页面导致组件卸载、setMessages updater 未执行而丢失回复。
      if (currentConvId) {
        void saveUnifiedConversationMessages(currentConvId, finalMessages);
        void updateUnifiedConversation(currentConvId, {
          messageCount: finalMessages.length,
          model: effectiveChatModel,
        });
      }
      setMessages(finalMessages);
    } catch (err: unknown) {
      if (err instanceof DOMException && err.name === "AbortError") {
        setMessages((prev) => {
          const nextList = prev.map((m) =>
            m.id === assistantId
              ? {
                  ...m,
                  openuiMode: containsOpenUIDSL(m.content),
                  isStreaming: false,
                }
              : m
          );
          if (currentConvId) {
            void saveUnifiedConversationMessages(currentConvId, nextList);
          }
          return nextList;
        });
        return;
      }

      const errMsg = err instanceof Error ? err.message : "请求失败";
      if (!liveDiag) {
        try {
          liveDiag = await fetchDiagnostics();
        } catch {
          liveDiag = null;
        }
      }

      setMessages((prev) => {
        const nextList: StoredChatMessage[] = prev.map((m) => {
          if (m.id !== assistantId) return m;
          if (liveDiag) {
            return {
              ...m,
              openuiMode: true,
              skill: "diagnose" as CopilotSkill,
              isOfflineFallback: true,
              isStreaming: false,
              content: buildOfflineFallbackContent(liveDiag, errMsg),
            };
          }
          return {
            ...m,
            isStreaming: false,
            openuiMode: sendOpenUIHeader,
            content: `> ⚠️ **请求异常**: ${errMsg}\n\n如因号池可用账号耗尽或代理熔断导致无法推理，请先在顶部确认管理端已解锁后执行 \`/diagnose\` 查看离线诊断报告。`,
          };
        });
        if (currentConvId) {
          void saveUnifiedConversationMessages(currentConvId, nextList);
        }
        return nextList;
      });
    } finally {
      abortRef.current = null;
      setIsSending(false);
    }
  };

  // 消息高频操作：重新生成
  const handleRegenerate = (assistantMsgId: string, overrideModel?: string) => {
    const idx = messages.findIndex((m) => m.id === assistantMsgId);
    if (idx === -1) return;

    let targetPrompt = "";
    let userMsgIndex = -1;
    for (let i = idx - 1; i >= 0; i--) {
      if (messages[i].role === "user") {
        targetPrompt = messages[i].content;
        userMsgIndex = i;
        break;
      }
    }
    if (!targetPrompt || userMsgIndex === -1) return;

    if (
      overrideModel &&
      (targetPrompt === "/image" || targetPrompt.startsWith("/image "))
    ) {
      const parsed = parseImageCommand(targetPrompt, availableImageModels);
      targetPrompt = isImageModelId(overrideModel, availableImageModels)
        ? `/image ${overrideModel} ${parsed.prompt}`
        : parsed.prompt;
    }

    const historyBeforeUser = messages.slice(0, userMsgIndex);
    setMessages(historyBeforeUser);
    setRetryDropdownMsgId(null);
    void handleSendMessage(
      targetPrompt,
      overrideModel || model,
      historyBeforeUser,
      Boolean(overrideModel)
    );
  };

  const toggleSource = (id: string) => {
    setMessages((prev) =>
      prev.map((m) => (m.id === id ? { ...m, showSource: !m.showSource } : m))
    );
  };

  const updateSourceLive = (id: string, newCode: string) => {
    setMessages((prev) =>
      prev.map((m) =>
        m.id === id
          ? { ...m, content: newCode, openuiMode: containsOpenUIDSL(newCode) }
          : m
      )
    );
  };

  const filteredSlashSkills = (() => {
    if (input.startsWith("/") && !/\s/.test(input)) {
      const prefix = input.toLowerCase();
      return SLASH_SKILLS.filter((item) =>
        item.command.toLowerCase().startsWith(prefix)
      );
    }
    return [];
  })();

  return (
    <div className="flex h-full min-h-[calc(100vh-4rem)] w-full overflow-hidden bg-[var(--bg-canvas)] text-[var(--text-primary)]">
      {/* ─── 左侧会话抽屉 (Sidebar Drawer) ─────────────────────────────── */}
      <aside
        className={`relative flex flex-col p-sidebar transition-all duration-200 shrink-0 ${
          isDrawerOpen
            ? "w-72 min-w-[18rem]"
            : "w-0 min-w-0 -translate-x-full overflow-hidden border-none"
        }`}
      >
        {/* 抽屉顶部：新建对话与搜索 */}
        <div className="p-3 border-b z-border flex flex-col gap-2">
          <div className="flex items-center justify-between">
            <span className="text-xs font-semibold uppercase tracking-wider z-text-muted">
              会话记录
            </span>
            <button
              onClick={() => setIsDrawerOpen(false)}
              className="p-1.5 rounded-lg z-text-muted hover:z-text hover:bg-[var(--bg-muted)] transition"
              title="折叠抽屉"
            >
              <PanelLeftClose size={15} />
            </button>
          </div>

          <button
            onClick={handleCreateNewConversation}
            className="flex items-center justify-center gap-2 w-full py-2 px-3 rounded-xl p-card hover:bg-[var(--bg-surface-hover)] text-xs font-medium z-text shadow-sm transition"
          >
            <Plus size={15} className="text-indigo-500" />
            <span>新建对话</span>
          </button>

          <div className="relative">
            <Search size={13} className="absolute left-2.5 top-2.5 z-text-muted" />
            <input
              type="text"
              value={sidebarFilter}
              onChange={(e) => setSidebarFilter(e.target.value)}
              placeholder="过滤会话标题..."
              className="w-full p-input rounded-lg pl-8 pr-3 py-1.5 text-xs z-text focus:outline-none"
            />
          </div>
        </div>

        {/* 抽屉主体：分组列表 */}
        <div className="flex-1 overflow-y-auto p-2 space-y-4">
          {conversations.length === 0 ? (
            <div className="flex flex-col items-center justify-center p-6 text-center z-text-muted space-y-2">
              <MessageSquare size={24} className="opacity-40 text-indigo-400" />
              <p className="text-xs">暂无历史会话记录</p>
              <button
                onClick={handleCreateNewConversation}
                className="text-xs text-indigo-500 hover:text-indigo-400 font-medium cursor-pointer"
              >
                + 立即开启新对话
              </button>
            </div>
          ) : groupedConversations.pinned.length === 0 &&
            groupedConversations.today.length === 0 &&
            groupedConversations.yesterday.length === 0 &&
            groupedConversations.last7Days.length === 0 &&
            groupedConversations.older.length === 0 ? (
            <div className="flex flex-col items-center justify-center p-6 text-center z-text-muted space-y-1">
              <Search size={20} className="opacity-40 mb-1" />
              <p className="text-xs">未找到包含 "{sidebarFilter}" 的会话</p>
            </div>
          ) : (
            <>
              {/* 📌 置顶会话 */}
              {groupedConversations.pinned.length > 0 && (
                <div>
                  <div className="px-2 py-1 text-[11px] font-semibold text-amber-500 flex items-center gap-1">
                    <Pin size={11} />
                    <span>置顶会话</span>
                  </div>
                  <div className="space-y-0.5">
                    {groupedConversations.pinned.map((conv) => (
                      <ConversationItem
                        key={conv.id}
                        conv={conv}
                        isActive={conv.id === activeConvId}
                        isEditing={editingConvId === conv.id}
                        editingTitle={editingTitle}
                        onSelect={() => handleSelectConversation(conv.id)}
                        onTogglePin={(e) => handleTogglePin(e, conv)}
                        onStartRename={(e) => handleStartRename(e, conv)}
                        onSaveRename={handleSaveRename}
                        onChangeEditingTitle={setEditingTitle}
                        onDelete={(e) => handleDeleteConversation(e, conv.id)}
                        onExport={() => handleExportMarkdown(conv.id)}
                      />
                    ))}
                  </div>
                </div>
              )}

              {/* 今天 */}
              {groupedConversations.today.length > 0 && (
                <div>
                  <div className="px-2 py-1 text-[11px] font-semibold z-text-muted">
                    今天
                  </div>
                  <div className="space-y-0.5">
                    {groupedConversations.today.map((conv) => (
                      <ConversationItem
                        key={conv.id}
                        conv={conv}
                        isActive={conv.id === activeConvId}
                        isEditing={editingConvId === conv.id}
                        editingTitle={editingTitle}
                        onSelect={() => handleSelectConversation(conv.id)}
                        onTogglePin={(e) => handleTogglePin(e, conv)}
                        onStartRename={(e) => handleStartRename(e, conv)}
                        onSaveRename={handleSaveRename}
                        onChangeEditingTitle={setEditingTitle}
                        onDelete={(e) => handleDeleteConversation(e, conv.id)}
                        onExport={() => handleExportMarkdown(conv.id)}
                      />
                    ))}
                  </div>
                </div>
              )}

              {/* 昨天 */}
              {groupedConversations.yesterday.length > 0 && (
                <div>
                  <div className="px-2 py-1 text-[11px] font-semibold z-text-muted">
                    昨天
                  </div>
                  <div className="space-y-0.5">
                    {groupedConversations.yesterday.map((conv) => (
                      <ConversationItem
                        key={conv.id}
                        conv={conv}
                        isActive={conv.id === activeConvId}
                        isEditing={editingConvId === conv.id}
                        editingTitle={editingTitle}
                        onSelect={() => handleSelectConversation(conv.id)}
                        onTogglePin={(e) => handleTogglePin(e, conv)}
                        onStartRename={(e) => handleStartRename(e, conv)}
                        onSaveRename={handleSaveRename}
                        onChangeEditingTitle={setEditingTitle}
                        onDelete={(e) => handleDeleteConversation(e, conv.id)}
                        onExport={() => handleExportMarkdown(conv.id)}
                      />
                    ))}
                  </div>
                </div>
              )}

              {/* 过去 7 天 */}
              {groupedConversations.last7Days.length > 0 && (
                <div>
                  <div className="px-2 py-1 text-[11px] font-semibold z-text-muted">
                    前 7 天
                  </div>
                  <div className="space-y-0.5">
                    {groupedConversations.last7Days.map((conv) => (
                      <ConversationItem
                        key={conv.id}
                        conv={conv}
                        isActive={conv.id === activeConvId}
                        isEditing={editingConvId === conv.id}
                        editingTitle={editingTitle}
                        onSelect={() => handleSelectConversation(conv.id)}
                        onTogglePin={(e) => handleTogglePin(e, conv)}
                        onStartRename={(e) => handleStartRename(e, conv)}
                        onSaveRename={handleSaveRename}
                        onChangeEditingTitle={setEditingTitle}
                        onDelete={(e) => handleDeleteConversation(e, conv.id)}
                        onExport={() => handleExportMarkdown(conv.id)}
                      />
                    ))}
                  </div>
                </div>
              )}

              {/* 更早 */}
              {groupedConversations.older.length > 0 && (
                <div>
                  <div className="px-2 py-1 text-[11px] font-semibold z-text-muted">
                    更早历史
                  </div>
                  <div className="space-y-0.5">
                    {groupedConversations.older.map((conv) => (
                      <ConversationItem
                        key={conv.id}
                        conv={conv}
                        isActive={conv.id === activeConvId}
                        isEditing={editingConvId === conv.id}
                        editingTitle={editingTitle}
                        onSelect={() => handleSelectConversation(conv.id)}
                        onTogglePin={(e) => handleTogglePin(e, conv)}
                        onStartRename={(e) => handleStartRename(e, conv)}
                        onSaveRename={handleSaveRename}
                        onChangeEditingTitle={setEditingTitle}
                        onDelete={(e) => handleDeleteConversation(e, conv.id)}
                        onExport={() => handleExportMarkdown(conv.id)}
                      />
                    ))}
                  </div>
                </div>
              )}
            </>
          )}
        </div>

        {/* 抽屉底部工具栏 */}
        <div className="p-2 border-t z-border z-surface flex items-center justify-between text-xs z-text-muted">
          <button
            onClick={() => setIsSearchModalOpen(true)}
            className="flex items-center gap-1.5 hover:z-text py-1 px-2 rounded-lg hover:bg-[var(--bg-muted)] transition"
            title="快捷键 Ctrl+K / Cmd+K"
          >
            <Search size={13} />
            <span>搜索 (⌘K)</span>
          </button>
          <div className="flex items-center gap-1">
            <button
              onClick={handleExportJSON}
              className="p-1.5 hover:z-text hover:bg-[var(--bg-muted)] rounded-lg transition"
              title="导出全部会话 JSON"
            >
              <Download size={14} />
            </button>
            <label
              className="p-1.5 hover:z-text hover:bg-[var(--bg-muted)] rounded-lg cursor-pointer transition"
              title="导入会话 JSON"
            >
              <Upload size={14} />
              <input
                type="file"
                accept=".json"
                onChange={handleImportJSON}
                className="hidden"
              />
            </label>
          </div>
        </div>
      </aside>

      {/* ─── 核心对话交互区 ────────────────────────────────────────────── */}
      <main className="flex-1 flex flex-col h-full min-w-0 bg-[var(--bg-canvas)]">
        {/* 顶部控制栏 */}
        <header className="h-14 p-topbar px-4 flex items-center justify-between gap-3 shrink-0">
          <div className="flex items-center gap-3 min-w-0">
            {!isDrawerOpen && (
              <button
                onClick={() => setIsDrawerOpen(true)}
                className="p-1.5 rounded-xl border z-border z-surface z-ghost-btn text-xs z-text-muted hover:z-text transition cursor-pointer"
                title="展开会话抽屉"
              >
                <PanelLeftOpen size={16} />
              </button>
            )}

            <div className="flex items-center gap-2 min-w-0">
              <span
                className="font-semibold text-sm truncate z-text max-w-[200px] md:max-w-[320px]"
                title={activeConv?.title || "新对话"}
              >
                {activeConv?.title || "新对话"}
              </span>
              <button
                onClick={(e) => activeConv && handleStartRename(e, activeConv)}
                className="z-text-muted hover:z-text p-1 rounded-lg hover:bg-[var(--bg-muted)] transition cursor-pointer"
                title="重命名会话"
              >
                <Edit3 size={13} />
              </button>
            </div>
          </div>

          {/* 顶部右侧控制：预设角色、模型切换、导出与搜索 */}
          <div className="flex items-center gap-2">
            {/* 预设角色选择器 */}
            <div className="hidden sm:flex items-center gap-1.5 p-input rounded-xl px-2.5 py-1 text-xs">
              <Sparkles size={13} className="text-indigo-500" />
              <select
                value={selectedPresetId}
                onChange={(e) => handleSelectPreset(e.target.value)}
                className="bg-transparent z-text focus:outline-none cursor-pointer"
              >
                {presets.map((p) => (
                  <option key={p.id} value={p.id} className="bg-[var(--bg-surface)] text-[var(--text-primary)]">
                    {p.name}
                  </option>
                ))}
              </select>
            </div>

            {/* 模型选择器（支持对话 / 图像 / 语音全模型随时切换） */}
            <div className="flex items-center gap-1 p-input rounded-xl px-2.5 py-1 text-xs">
              <select
                value={model}
                onChange={(e) => {
                  setModel(e.target.value);
                  try {
                    window.sessionStorage.setItem(CHAT_MODEL_STORAGE_KEY, e.target.value);
                  } catch {}
                  if (activeConvId) {
                    void updateUnifiedConversation(activeConvId, { model: e.target.value });
                  }
                }}
                className="bg-transparent z-text focus:outline-none cursor-pointer font-mono-num"
              >
                <optgroup label="对话模型" className="bg-[var(--bg-surface)] text-[var(--text-primary)]">
                  {availableModels.map((m) => (
                    <option key={m} value={m} className="bg-[var(--bg-surface)] text-[var(--text-primary)]">
                      {m}
                    </option>
                  ))}
                </optgroup>
                <optgroup label="图像模型" className="bg-[var(--bg-surface)] text-[var(--text-primary)]">
                  {availableImageModels.map((m) => (
                    <option key={m} value={m} className="bg-[var(--bg-surface)] text-[var(--text-primary)]">
                      {m}
                    </option>
                  ))}
                </optgroup>
                <optgroup label="语音合成模型" className="bg-[var(--bg-surface)] text-[var(--text-primary)]">
                  {availableAudioModels.map((m) => (
                    <option key={m} value={m} className="bg-[var(--bg-surface)] text-[var(--text-primary)]">
                      {m}
                    </option>
                  ))}
                </optgroup>
              </select>
            </div>

            {/* 上下文自动切模开关 */}
            <button
              onClick={toggleAutoSwitch}
              className={`hidden sm:flex items-center gap-1 px-2.5 py-1 rounded-xl border text-xs transition cursor-pointer ${
                autoSwitchEnabled
                  ? "border-indigo-500/40 bg-indigo-500/10 text-indigo-500 font-medium"
                  : "z-border z-surface z-text-muted hover:z-text"
              }`}
              title={
                autoSwitchEnabled
                  ? "已开启：根据上下文意图自动切换图像 / 语音 / 研究 / 指定模型（点击关闭）"
                  : "已关闭：严格使用顶部选定模型（点击开启自动切模）"
              }
            >
              <Sparkles size={12} />
              <span>自动切模</span>
            </button>

            {/* 导出菜单 */}
            <button
              onClick={() => handleExportMarkdown()}
              className="p-1.5 rounded-xl border z-border z-surface z-ghost-btn z-text-muted hover:z-text transition cursor-pointer"
              title="导出当前会话为 Markdown"
            >
              <Download size={15} />
            </button>

            {/* 全局搜索触发器 */}
            <button
              onClick={() => setIsSearchModalOpen(true)}
              className="hidden md:flex items-center gap-1.5 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn text-xs z-text-muted hover:z-text transition cursor-pointer"
              title="Ctrl+K / Cmd+K"
            >
              <Search size={13} />
              <span>搜索</span>
              <kbd className="text-[10px] bg-[var(--bg-muted)] px-1.5 py-0.5 rounded z-text-muted font-mono-num">
                ⌘K
              </kbd>
            </button>
          </div>
        </header>

        {/* 消息对话流 */}
        <div ref={scrollRef} className="flex-1 overflow-y-auto px-4 py-6 space-y-6">
          {messages.map((msg) => (
            <div
              key={msg.id}
              className={`flex flex-col group ${
                msg.role === "user" ? "items-end" : "items-start"
              }`}
            >
              <div className="flex items-center gap-2 mb-1 px-1 text-xs z-text-muted">
                <span className="font-medium">{msg.role === "user" ? "你" : msg.model || "Copilot"}</span>
                {msg.timestamp && (
                  <span>
                    {new Date(msg.timestamp).toLocaleTimeString("zh-CN", {
                      hour: "2-digit",
                      minute: "2-digit",
                    })}
                  </span>
                )}
                {msg.latencyMs ? (
                  <span className="z-text-muted font-mono-num text-[11px]">
                    · {msg.latencyMs}ms
                  </span>
                ) : null}
              </div>

              {/* 气泡内容 */}
              {msg.role === "user" ? (
                <div className="relative max-w-xl rounded-2xl px-4 py-2.5 z-primary-btn leading-relaxed whitespace-pre-wrap shadow-sm rounded-tr-sm">
                  {/* 用户提问编辑回滚按钮 */}
                  <button
                    onClick={() => handleEditPrompt(msg)}
                    className="absolute -left-8 top-2 opacity-0 group-hover:opacity-100 p-1 hover:bg-[var(--bg-muted)] rounded-lg z-text-muted hover:z-text transition cursor-pointer"
                    title="编辑提问并重试"
                  >
                    <Edit3 size={14} />
                  </button>
                  <div className="text-sm font-normal">{msg.content}</div>
                </div>
              ) : (
                <div className="relative w-full max-w-[94%] md:max-w-[88%] rounded-2xl p-4 p-card space-y-3 rounded-tl-sm">
                  {/* 联网检索引用来源卡片 */}
                  {msg.citations && (
                    <div className="text-xs p-surface rounded-xl p-3 border z-border z-text-sec">
                      <div className="font-semibold z-text flex items-center gap-1.5 mb-1.5">
                        <ExternalLink size={13} className="text-indigo-500" />
                        <span>网页参考来源</span>
                      </div>
                      <div className="space-y-1">
                        {(() => {
                          try {
                            const parsed = JSON.parse(msg.citations);
                            if (Array.isArray(parsed)) {
                              return parsed.map((cite: any, i: number) => (
                                <a
                                  key={i}
                                  href={cite.url}
                                  target="_blank"
                                  rel="noreferrer"
                                  className="block text-indigo-500 hover:underline truncate"
                                >
                                  [{i + 1}] {cite.title || cite.url}
                                </a>
                              ));
                            }
                          } catch {}
                          return <span>{msg.citations}</span>;
                        })()}
                      </div>
                    </div>
                  )}

                  {/* OpenUI DSL 混合渲染 */}
                  {msg.skill === "image" && msg.isStreaming ? (
                    <div className="space-y-3">
                      <div className="flex items-center gap-2 text-xs z-text-sec">
                        <Sparkles size={14} className="text-indigo-500 animate-spin" />
                        <span className="font-medium z-text">
                          {msg.content.replace(/[`*🧠🎨]/g, "").trim() || "正在绘制图像..."}
                        </span>
                      </div>
                      <div className="w-60 sm:w-72 aspect-square rounded-2xl border z-border bg-gradient-to-br from-indigo-500/10 via-[var(--bg-muted)] to-purple-500/10 animate-pulse flex flex-col items-center justify-center gap-2.5 p-4 text-center">
                        <div className="w-10 h-10 rounded-xl bg-indigo-500/15 text-indigo-500 flex items-center justify-center">
                          <ImagePlus size={20} className="animate-bounce" />
                        </div>
                        <div className="text-xs font-medium z-text">正在构图渲染缩略图</div>
                        <div className="text-[11px] z-text-muted font-mono-num">
                          {msg.model || "image-model"}
                        </div>
                      </div>
                    </div>
                  ) : msg.openuiMode ? (
                    <div className="space-y-3">
                      {splitHybridSegments(msg.content).map((seg, idx) => {
                        if (seg.type === "markdown") {
                          return (
                            <MarkdownContent
                              key={`${msg.id}-md-${idx}`}
                              content={seg.text}
                            />
                          );
                        }
                        return (
                          <div key={`${msg.id}-oui-${idx}`} className="my-2">
                            {msg.showSource ? (
                              <textarea
                                value={seg.code}
                                onChange={(e) =>
                                  updateSourceLive(msg.id, e.target.value)
                                }
                                className="w-full h-32 p-code-box font-mono-num text-xs p-3 rounded-xl focus:outline-none resize-y"
                              />
                            ) : (
                              <ThemeProvider mode={themeMode}>
                                <div className="openui-scope">
                                  <Renderer
                                    library={aiWebFreeLibrary}
                                    response={seg.code}
                                    isStreaming={Boolean(msg.isStreaming)}
                                    toolProvider={aiWebFreeToolProvider}
                                    onAction={(evt) => {
                                      const actionMsg = (
                                        evt as { humanFriendlyMessage?: string }
                                      )?.humanFriendlyMessage;
                                      if (actionMsg) {
                                        void handleSendMessage(actionMsg);
                                      }
                                    }}
                                  />
                                </div>
                              </ThemeProvider>
                            )}
                          </div>
                        );
                      })}
                    </div>
                  ) : (
                    <MarkdownContent content={msg.content} />
                  )}

                  {/* 助手消息底部操作栏 */}
                  <div className="flex items-center justify-between pt-2.5 border-t z-border text-xs z-text-muted">
                    <div className="flex items-center gap-2">
                      {/* 复制 */}
                      <button
                        onClick={() => handleCopyMessage(msg.id, msg.content)}
                        className="flex items-center gap-1 hover:z-text p-1 rounded-lg hover:bg-[var(--bg-muted)] transition cursor-pointer"
                        title="复制全文"
                      >
                        {copiedMsgId === msg.id ? (
                          <>
                            <Check size={13} className="text-emerald-500" />
                            <span className="text-emerald-500 font-medium">已复制</span>
                          </>
                        ) : (
                          <>
                            <Copy size={13} />
                            <span>复制</span>
                          </>
                        )}
                      </button>

                      {/* 重新生成 */}
                      {!msg.isStreaming && (
                        <button
                          onClick={() => handleRegenerate(msg.id)}
                          className="flex items-center gap-1 hover:z-text p-1 rounded-lg hover:bg-[var(--bg-muted)] transition cursor-pointer"
                          title="重新生成回答"
                        >
                          <RefreshCw size={13} />
                          <span>重试</span>
                        </button>
                      )}

                      {/* 换模型重试 */}
                      {!msg.isStreaming && (
                        <div className="relative">
                          <button
                            onClick={() =>
                              setRetryDropdownMsgId(
                                retryDropdownMsgId === msg.id ? null : msg.id
                              )
                            }
                            className="flex items-center gap-1 hover:z-text p-1 rounded-lg hover:bg-[var(--bg-muted)] transition cursor-pointer"
                            title="换其他模型重新生成"
                          >
                            <span>换模型</span>
                            <ChevronDown size={11} />
                          </button>

                          {retryDropdownMsgId === msg.id && (
                            <div className="absolute left-0 bottom-6 z-20 w-52 p-card rounded-xl shadow-xl py-1 text-xs border z-border max-h-64 overflow-y-auto">
                              {(msg.skill === "image"
                                ? [
                                    { label: "图像模型", list: availableImageModels },
                                    { label: "对话模型", list: availableModels },
                                    { label: "语音模型", list: availableAudioModels },
                                  ]
                                : msg.skill === "audio"
                                  ? [
                                      { label: "语音模型", list: availableAudioModels },
                                      { label: "对话模型", list: availableModels },
                                      { label: "图像模型", list: availableImageModels },
                                    ]
                                  : [
                                      { label: "对话模型", list: availableModels },
                                      { label: "图像模型", list: availableImageModels },
                                      { label: "语音模型", list: availableAudioModels },
                                    ]
                              ).map((group) => (
                                <div key={group.label}>
                                  <div className="px-3 py-1 text-[10px] font-semibold uppercase tracking-wider z-text-muted border-t first:border-t-0 z-border">
                                    {group.label}
                                  </div>
                                  {group.list.map((m) => (
                                    <button
                                      key={m}
                                      onClick={() => handleRegenerate(msg.id, m)}
                                      className={`w-full text-left px-3 py-1.5 hover:bg-[var(--bg-surface-hover)] font-mono-num truncate cursor-pointer ${
                                        msg.model === m
                                          ? "text-indigo-500 font-semibold"
                                          : "z-text"
                                      }`}
                                    >
                                      {m}
                                    </button>
                                  ))}
                                </div>
                              ))}
                            </div>
                          )}
                        </div>
                      )}

                      {/* 查看 OpenUI 源码切换 */}
                      {msg.openuiMode && (
                        <button
                          onClick={() => toggleSource(msg.id)}
                          className="flex items-center gap-1 hover:z-text p-1 rounded-lg hover:bg-[var(--bg-muted)] transition cursor-pointer"
                          title="切换 DSL 源码"
                        >
                          <Code2 size={13} />
                          <span>{msg.showSource ? "显示组件" : "源码"}</span>
                        </button>
                      )}

                      {/* 发送到短剧画布作为剧本灵感或分镜台词 */}
                      {!msg.isStreaming && msg.content.trim() && (
                        <button
                          type="button"
                          onClick={() =>
                            dispatchDramaCanvas({
                              type: "script",
                              title: msg.content.trim().split("\n")[0].slice(0, 22) || "对话工坊剧本",
                              text: msg.content,
                              sourceView: "chat",
                            })
                          }
                          className="flex items-center gap-1 text-amber-600 dark:text-amber-400 hover:bg-amber-500/10 p-1 px-1.5 rounded-lg transition cursor-pointer"
                          title="将此段对话直接推送到短剧无限画布剧本节点"
                        >
                          <Clapperboard size={13} />
                          <span>发往短剧画布</span>
                        </button>
                      )}
                    </div>

                    {msg.model && (
                      <span className="font-mono-num text-[11px] z-text-muted">
                        {msg.model}
                      </span>
                    )}
                  </div>
                </div>
              )}
            </div>
          ))}

          {/* 发送中指示器 */}
          {isSending && (
            <div className="flex items-center gap-2 z-text-muted text-xs py-2">
              <div className="h-2 w-2 rounded-full bg-indigo-500 animate-ping" />
              <span>思考与流式生成中...</span>
            </div>
          )}
        </div>

        {/* ─── 底部输入区域 ──────────────────────────────────────────── */}
        <footer className="border-t z-border p-4 z-surface">
          {/* 斜杠指令菜单浮层 */}
          {filteredSlashSkills.length > 0 && (
            <div className="mb-2 max-w-xl p-card rounded-xl shadow-xl overflow-hidden text-xs border z-border">
              {filteredSlashSkills.map((sk) => {
                const IconComponent = sk.icon;
                return (
                  <button
                    key={sk.command}
                    onClick={() => {
                      setInput(sk.command + " ");
                      textareaRef.current?.focus();
                    }}
                    className="w-full px-3 py-2 text-left hover:bg-[var(--bg-surface-hover)] flex items-center justify-between z-text border-b z-border last:border-none cursor-pointer"
                  >
                    <div className="flex items-center gap-2">
                      <IconComponent size={14} className="text-indigo-500" />
                      <span className="font-mono-num font-semibold">{sk.command}</span>
                      <span className="z-text-sec">{sk.label}</span>
                    </div>
                    <span className="z-text-muted text-[11px] truncate max-w-[200px]">
                      {sk.description}
                    </span>
                  </button>
                );
              })}
            </div>
          )}

          {/* 快捷推荐提示卡 */}
          {messages.length <= 1 && (
            <div className="mb-3 grid grid-cols-2 sm:grid-cols-4 gap-2">
              {STARTER_PROMPTS.map((sp, idx) => {
                const IconComp = sp.icon;
                return (
                  <button
                    key={idx}
                    onClick={() => {
                      setInput(sp.prompt);
                      void handleSendMessage(sp.prompt);
                    }}
                    className="p-2.5 p-card p-card-interactive rounded-xl text-left text-xs transition group cursor-pointer"
                  >
                    <IconComp size={14} className="text-indigo-500 mb-1 group-hover:scale-110 transition" />
                    <div className="font-medium z-text truncate">{sp.label}</div>
                  </button>
                );
              })}
            </div>
          )}

          {/* Chat Remix 参考图悬浮条 */}
          {pendingRemixImage && (
            <div className="mb-2 p-2.5 rounded-xl border border-indigo-500/30 bg-indigo-500/10 flex items-center gap-3 text-xs">
              <img
                src={pendingRemixImage.imageUrl}
                alt="Remix 参考图"
                className="w-12 h-12 rounded-lg object-cover border z-border shrink-0 bg-black/10"
              />
              <div className="flex-1 min-w-0 space-y-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-semibold text-indigo-600 dark:text-indigo-400">
                    Chat Remix 参考图已就绪
                  </span>
                  <div className="inline-flex rounded-md border z-border overflow-hidden text-[11px]">
                    <button
                      type="button"
                      onClick={() => setRemixMode("edit")}
                      className={`px-2 py-0.5 transition cursor-pointer ${
                        remixMode === "edit"
                          ? "bg-indigo-600 text-white font-medium"
                          : "z-surface z-text-muted hover:z-text"
                      }`}
                    >
                      🎨 图生图重绘
                    </button>
                    <button
                      type="button"
                      onClick={() => setRemixMode("chat")}
                      className={`px-2 py-0.5 transition cursor-pointer ${
                        remixMode === "chat"
                          ? "bg-indigo-600 text-white font-medium"
                          : "z-surface z-text-muted hover:z-text"
                      }`}
                    >
                      💬 对话讨论
                    </button>
                  </div>
                </div>
                <p className="z-text-muted truncate" title={pendingRemixImage.prompt}>
                  原 Prompt：{pendingRemixImage.prompt || "（无）"}
                </p>
              </div>
              <button
                type="button"
                onClick={() => setPendingRemixImage(null)}
                className="p-1 rounded-lg hover:bg-black/10 z-text-muted hover:z-text cursor-pointer"
                title="移除 Remix 参考图"
              >
                <X size={14} />
              </button>
            </div>
          )}

          {/* 核心输入框 */}
          <div className="relative flex items-end gap-2 p-card rounded-xl p-2.5 border z-border focus-within:border-[var(--brand-primary)] transition">
            <textarea
              ref={textareaRef}
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  void handleSendMessage();
                }
              }}
              placeholder="输入消息，或使用 /voice (搜集克隆音色)、/crawl (爬取图/视频入库)、/image、/diagnose、/openui (Enter 发送)..."
              rows={2}
              className="flex-1 bg-transparent text-sm z-text placeholder:z-text-muted focus:outline-none resize-none max-h-36 overflow-y-auto"
            />

            {/* 发送或停止按钮 */}
            {isSending ? (
              <button
                onClick={handleStopGenerating}
                className="flex items-center gap-1.5 px-3 py-2 bg-rose-600 hover:bg-rose-500 text-white rounded-lg text-xs font-semibold shadow-md transition animate-pulse cursor-pointer"
                title="中断生成"
              >
                <StopCircle size={15} />
                <span>停止</span>
              </button>
            ) : (
              <button
                onClick={() => handleSendMessage()}
                disabled={!input.trim()}
                className="p-2.5 z-primary-btn rounded-lg transition shadow-sm cursor-pointer"
                title="发送 (Enter)"
              >
                <SendHorizontal size={16} />
              </button>
            )}
          </div>
        </footer>
      </main>

      {/* ─── 全局全文检索弹窗 (Ctrl+K Modal) ────────────────────────────── */}
      {isSearchModalOpen && (
        <div className="fixed inset-0 z-50 bg-black/40 backdrop-blur-sm flex items-start justify-center pt-20 px-4">
          <div className="w-full max-w-2xl p-card rounded-2xl shadow-2xl overflow-hidden flex flex-col border z-border">
            <form onSubmit={handleSearchSubmit} className="p-3 border-b z-border flex items-center gap-2">
              <Search size={18} className="z-text-muted" />
              <input
                ref={searchInputRef}
                type="text"
                value={searchModalQuery}
                onChange={(e) => setSearchModalQuery(e.target.value)}
                placeholder="跨会话全文检索（关键词、报错、代码片段）..."
                className="flex-1 p-input rounded-lg px-3 py-1.5 text-sm z-text focus:outline-none"
              />
              <button
                type="submit"
                disabled={isSearching || !searchModalQuery.trim()}
                className="px-3.5 py-1.5 z-primary-btn text-xs rounded-lg font-medium cursor-pointer"
              >
                {isSearching ? "检索中..." : "搜索"}
              </button>
              <button
                type="button"
                onClick={() => setIsSearchModalOpen(false)}
                className="p-1 z-text-muted hover:z-text rounded-lg hover:bg-[var(--bg-muted)] transition cursor-pointer"
              >
                <X size={18} />
              </button>
            </form>

            <div className="max-h-96 overflow-y-auto p-3 space-y-2">
              {searchHits.length === 0 ? (
                <div className="text-center py-8 text-xs z-text-muted">
                  {searchModalQuery
                    ? "未找到匹配的历史会话消息"
                    : "输入关键词开始跨所有历史会话进行毫秒级全文检索"}
                </div>
              ) : (
                searchHits.map((hit, idx) => (
                  <div
                    key={idx}
                    onClick={() => handleJumpToSearchHit(hit)}
                    className="p-3 p-card hover:bg-[var(--bg-surface-hover)] border z-border rounded-xl cursor-pointer transition flex flex-col gap-1 group"
                  >
                    <div className="flex items-center justify-between text-xs">
                      <span className="font-semibold z-text group-hover:text-indigo-500 transition">
                        {hit.conversation_title}
                      </span>
                      <span className="text-[11px] z-text-muted">
                        {new Date(hit.created_at).toLocaleString("zh-CN", {
                          month: "numeric",
                          day: "numeric",
                          hour: "2-digit",
                          minute: "2-digit",
                        })}
                      </span>
                    </div>
                    <div className="text-xs z-text-sec leading-relaxed font-mono-num line-clamp-2">
                      <span className="z-text-muted uppercase mr-1">
                        [{hit.role}]
                      </span>
                      {hit.snippet}
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ─── 会话列表单项子组件 ───────────────────────────────────────────────────

interface ConversationItemProps {
  conv: StoredConversation;
  isActive: boolean;
  isEditing: boolean;
  editingTitle: string;
  onSelect: () => void;
  onTogglePin: (e: React.MouseEvent) => void;
  onStartRename: (e: React.MouseEvent) => void;
  onSaveRename: () => void;
  onChangeEditingTitle: (val: string) => void;
  onDelete: (e: React.MouseEvent) => void;
  onExport: () => void;
}

function ConversationItem({
  conv,
  isActive,
  isEditing,
  editingTitle,
  onSelect,
  onTogglePin,
  onStartRename,
  onSaveRename,
  onChangeEditingTitle,
  onDelete,
  onExport,
}: ConversationItemProps): React.ReactElement {
  if (isEditing) {
    return (
      <div className="p-1 p-card rounded-lg flex items-center gap-1 border z-border">
        <input
          type="text"
          value={editingTitle}
          autoFocus
          onChange={(e) => onChangeEditingTitle(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") onSaveRename();
            if (e.key === "Escape") onSaveRename();
          }}
          onBlur={onSaveRename}
          className="flex-1 p-input px-2 py-1 text-xs z-text rounded focus:outline-none"
        />
        <button
          onClick={onSaveRename}
          className="p-1 text-emerald-500 hover:text-emerald-400 cursor-pointer"
          title="保存"
        >
          <Check size={14} />
        </button>
      </div>
    );
  }

  return (
    <div
      onClick={onSelect}
      className={`group relative flex items-center justify-between px-2.5 py-2 rounded-xl cursor-pointer text-xs transition ${
        isActive
          ? "p-card z-text font-medium shadow-sm"
          : "z-text-sec hover:bg-[var(--bg-surface-hover)] hover:z-text"
      }`}
    >
      <div className="flex items-center gap-2 min-w-0 flex-1">
        <MessageSquare
          size={14}
          className={`shrink-0 ${isActive ? "text-indigo-500" : "z-text-muted"}`}
        />
        <span className="truncate">{conv.title}</span>
      </div>

      {/* 悬浮操作图标 */}
      <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition shrink-0 ml-1">
        <button
          onClick={onTogglePin}
          className="p-1 hover:text-amber-500 z-text-muted rounded hover:bg-[var(--bg-muted)] transition cursor-pointer"
          title={conv.isPinned ? "取消置顶" : "置顶会话"}
        >
          {conv.isPinned ? <PinOff size={13} /> : <Pin size={13} />}
        </button>
        <button
          onClick={onStartRename}
          className="p-1 hover:z-text z-text-muted rounded hover:bg-[var(--bg-muted)] transition cursor-pointer"
          title="重命名"
        >
          <Edit3 size={13} />
        </button>
        <button
          onClick={onDelete}
          className="p-1 hover:text-rose-500 z-text-muted rounded hover:bg-[var(--bg-muted)] transition cursor-pointer"
          title="删除会话"
        >
          <Trash2 size={13} />
        </button>
      </div>
    </div>
  );
}
