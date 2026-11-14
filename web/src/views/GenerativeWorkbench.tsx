import React, { useEffect, useRef, useState } from "react";
import { Renderer } from "@openuidev/react-lang";
import { ThemeProvider } from "@openuidev/react-ui";
import {
  Activity,
  Check,
  Code2,
  Copy,
  FileCode2,
  ImagePlus,
  KeyRound,
  Layers,
  MessageSquare,
  RotateCcw,
  SendHorizontal,
  ShieldAlert,
  Sparkles,
  Square,
  Wrench,
  X,
} from "lucide-react";
import {
  aiWebFreeLibrary,
  aiWebFreeToolProvider,
  getOpenUISystemPrompt,
} from "../lib/openui-library";
import {
  fetchDiagnostics,
  fetchModelsCatalog,
  streamChatCompletion,
  type DiagnosticsReport,
} from "../api/client";
import { classifyModel } from "../lib/models";
import {
  clearStoredChat,
  loadStoredChat,
  saveStoredChat,
  type CopilotSkill,
  type StoredChatMessage,
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

\`\`\`openui
root = Card([header, diagCard, poolCard])
header = CardHeader("AI-Web-Free Copilot 实时诊断台", "混合渲染：Markdown 专业分析 + 嵌入式 OpenUI 交互式运维组件")
diagCard = PlatformDiagnosisCard("平台全链路健康与异常根因扫描", "all")
poolCard = PoolOverviewCard("chatgpt", "ChatGPT Web 免费号池实时监控")
\`\`\``;

const DEFAULT_CHAT_MODELS = [
  "gpt-5-6",
  "gpt-5-3",
  "gpt-5-mini",
  "gpt-4o",
  "gpt-4o-mini",
];

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
    label: "AI 生图卡",
    description: "在对话流中直接唤起 GPT-Image-2.5 交互式生图卡片",
    defaultPrompt:
      "请用 OpenUI Lang 生成一个包含 CardHeader 与 ImageStudioCard（提示词：Minimalist architectural concrete pavilion in morning mist, natural sunlight，模型：gpt-image-2.5-sunburst）的生图卡片。",
    icon: ImagePlus,
  },
  {
    command: "/clear",
    skill: "clear",
    label: "清空会话",
    description: "清除 IndexedDB 本地持久化多轮历史并恢复默认初始工作台",
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
    icon: ImagePlus,
    label: "唤起 GPT-Image 生图卡",
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

function parseInputWithSlash(
  rawText: string,
  pinnedSkill: CopilotSkill
): {
  cleanPrompt: string;
  explicitSkill: CopilotSkill | "clear" | null;
} {
  const trimmed = rawText.trim();
  const lower = trimmed.toLowerCase();
  for (const item of SLASH_SKILLS) {
    if (lower === item.command || lower.startsWith(`${item.command} `)) {
      const rest = trimmed.slice(item.command.length).trim();
      return {
        cleanPrompt: rest || item.defaultPrompt,
        explicitSkill: item.skill,
      };
    }
  }
  if (!trimmed && pinnedSkill !== "auto") {
    const matched = SLASH_SKILLS.find((s) => s.skill === pinnedSkill);
    if (matched) {
      return {
        cleanPrompt: matched.defaultPrompt,
        explicitSkill: matched.skill,
      };
    }
  }
  return {
    cleanPrompt: trimmed,
    explicitSkill: pinnedSkill !== "auto" ? pinnedSkill : null,
  };
}

function detectEffectiveSkill(
  explicitSkill: CopilotSkill | null,
  prompt: string
): Exclude<CopilotSkill, "auto"> {
  if (explicitSkill && explicitSkill !== "auto") {
    return explicitSkill;
  }
  if (OPS_INTENT_RE.test(prompt)) return "diagnose";
  if (IMAGE_INTENT_RE.test(prompt)) return "image";
  if (OPENUI_INTENT_RE.test(prompt)) return "openui";
  return "chat";
}

function buildCompactTelemetryPayload(diag: DiagnosticsReport) {
  return {
    generated_at: diag.generated_at,
    health_score: diag.health_score,
    overall_severity: diag.overall_severity,
    headline: diag.headline,
    pool_stats: diag.pool_stats,
    proxy_summary: diag.proxy_summary,
    kami_worker: diag.kami_worker,
    audit_summary: diag.audit_summary,
    root_causes: diag.root_causes,
    flagged_accounts: (diag.flagged_accounts || []).slice(0, 12),
    tripped_proxies: (diag.tripped_proxies || []).slice(0, 10),
  };
}

function buildSystemPromptForSkill(
  skill: Exclude<CopilotSkill, "auto">,
  diag: DiagnosticsReport | null
): string {
  if (skill === "openui" || skill === "image") {
    return getOpenUISystemPrompt();
  }
  if ((skill === "diagnose" || skill === "fix") && diag) {
    return [
      "你是 AI-Web-Free 平台的 SRE / 运维诊断 Copilot，必须严格基于用户消息末尾附带的实时系统遥测 JSON 回答问题，严禁编造未出现的账号、代理或状态数据。",
      "请使用清晰、结构化的中文 Markdown 输出分析：点明健康评分、具体失败原因（永久封禁 status=banned、403 WAF/Cloudflare 代理风控、401 令牌过期/卡密续期退避、429 全局限流/生图限流、代理节点连续失败熔断）、受影响账号/代理及对应处置步骤。",
      "如需在回复中挂载交互式修复界面，请在 Markdown 总结末尾嵌入一个 ```openui 代码块，例如：",
      "```openui",
      "root = Card([header, diagCard, tableCard])",
      'header = CardHeader("全平台根因诊断与修复台", "支持点击安全自愈或二次确认清理封禁账号")',
      'diagCard = PlatformDiagnosisCard("平台全链路诊断与一键自愈", "all")',
      'tableCard = AccountDiagnosticTable("chatgpt")',
      "```",
    ].join("\n\n");
  }
  if (skill === "diagnose" || skill === "fix") {
    return "你是 AI-Web-Free 平台架构与运维 Copilot。当前管理端遥测接口暂未返回数据（可能启用了 ADMIN_TOKEN 鉴权未解锁）。请向用户解释 5 类常见平台故障定位方法（403 WAF vs account_deactivated 永久封禁 vs 401 续期退避 vs 429 限流 vs 代理 3 次失败熔断），并提示其可在控制台解锁后重新执行 `/diagnose`。";
  }
  return "你是 AI-Web-Free 智能助手与网关架构顾问。请以准确、简洁的中文 Markdown 回复用户问题；不要编造本地实时遥测数据。若用户想检查网关号池状态或生成交互看板，可提示其使用 `/diagnose`、`/fix` 或 `/openui`。";
}

function buildUserPayloadForSkill(
  promptText: string,
  skill: Exclude<CopilotSkill, "auto">,
  diag: DiagnosticsReport | null
): string {
  if (skill === "diagnose" || skill === "fix") {
    if (diag) {
      const jsonTelemetry = JSON.stringify(buildCompactTelemetryPayload(diag));
      return [
        promptText,
        "",
        "---",
        "[平台实时诊断上下文 · 请严格基于下方真实遥测 JSON 用中文 Markdown 输出根因分析与处置建议；严禁编造未出现的账号或代理；如需交互卡片可在末尾附加 ```openui 块]",
        "```json",
        jsonTelemetry,
        "```",
      ].join("\n");
    }
    return [
      promptText,
      "",
      "---",
      "[平台提示：当前未能读取 `/admin/api/v1/diagnostics` 实时遥测（可能开启了 ADMIN_TOKEN 尚未解锁）。请勿编造实时账号/代理数据，请用中文 Markdown 说明 5 类故障排查路径并提醒用户先在控制台右上角解锁管理令牌。]",
    ].join("\n");
  }
  return promptText;
}

function buildOfflineFallbackContent(
  diag: DiagnosticsReport,
  upstreamErr: string
): string {
  return [
    `> ⚠️ **在线模型对话当前不可用**（${upstreamErr}）。已自动切换为 **本地离线诊断引擎**，直接分析实时号池、代理与审计状态：`,
    diag.markdown_report,
    "```openui",
    "root = Card([header, diagCard, diagTable])",
    'header = CardHeader("离线应急诊断与自愈控制台", "无需外部大模型，点击下方按钮可立即尝试自愈与恢复号池")',
    'diagCard = PlatformDiagnosisCard("离线全平台根因诊断与修复台", "all")',
    'diagTable = AccountDiagnosticTable("chatgpt")',
    "```",
  ].join("\n\n");
}

function getSkillMetaLabel(msg: StoredChatMessage): string {
  if (msg.isOfflineFallback) return "本地离线诊断兜底";
  if (msg.skill === "diagnose") return "平台诊断 · 混合渲染";
  if (msg.skill === "fix") return "自愈方案 · 混合渲染";
  if (msg.openuiMode && (msg.skill === "openui" || msg.skill === "image")) {
    return "Thesys OpenUI 交互卡片";
  }
  if (msg.openuiMode) {
    return "Thesys OpenUI 交互卡片";
  }
  return "Copilot Markdown 对话";
}

function createDefaultMessages(): StoredChatMessage[] {
  return [
    {
      id: "welcome-1",
      role: "assistant",
      content: INITIAL_COPILOT_MESSAGE,
      openuiMode: true,
      skill: "diagnose",
      isStreaming: false,
      showSource: false,
    },
  ];
}

function migrateStoredMessages(saved: StoredChatMessage[]): StoredChatMessage[] {
  const defaultWelcome = createDefaultMessages()[0];
  return saved.map((m) => {
    if (m.id === "welcome-1" && !m.content.includes("PlatformDiagnosisCard")) {
      return defaultWelcome;
    }
    if (m.role === "assistant") {
      const hasDsl = containsOpenUIDSL(m.content);
      return {
        ...m,
        openuiMode: hasDsl,
        isStreaming: false,
        showSource: false,
      };
    }
    return { ...m, isStreaming: false };
  });
}

export const GenerativeWorkbench: React.FC<{
  themeMode: "light" | "dark";
}> = ({ themeMode }) => {
  const [messages, setMessages] = useState<StoredChatMessage[]>(
    createDefaultMessages
  );
  const [hydrated, setHydrated] = useState(false);
  const [idbSynced, setIdbSynced] = useState(false);

  const [input, setInput] = useState("");
  const [model, setModel] = useState("gpt-5-6");
  const [chatModels, setChatModels] = useState<string[]>(DEFAULT_CHAT_MODELS);
  const [pinnedSkill, setPinnedSkill] = useState<CopilotSkill>("auto");
  const [apiKey, setApiKey] = useState("");
  const [isSending, setIsSending] = useState(false);
  const [showSystemPromptModal, setShowSystemPromptModal] = useState(false);
  const [showQuickMenu, setShowQuickMenu] = useState(false);
  const [copiedPrompt, setCopiedPrompt] = useState(false);

  const streamEndRef = useRef<HTMLDivElement | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    let alive = true;
    loadStoredChat("default").then((saved) => {
      if (!alive) return;
      if (saved && saved.length > 0) {
        setMessages(migrateStoredMessages(saved));
      }
      setIdbSynced(true);
      setHydrated(true);
    });
    fetchModelsCatalog()
      .then((list) => {
        if (!alive || list.length === 0) return;
        const dynamicChat = list
          .map((m) => classifyModel(m.id))
          .filter((m) => m.category === "chat")
          .map((m) => m.id);
        const merged = Array.from(
          new Set([...dynamicChat, ...DEFAULT_CHAT_MODELS])
        );
        if (merged.length > 0) {
          setChatModels(merged);
          setModel((prev) => (merged.includes(prev) ? prev : merged[0]));
        }
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    if (!hydrated || isSending) return;
    saveStoredChat(messages, "default").then((ok) => {
      if (ok) setIdbSynced(true);
    });
  }, [messages, hydrated, isSending]);

  useEffect(() => {
    streamEndRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [messages.length]);

  const handleReset = async () => {
    if (isSending) return;
    await clearStoredChat("default");
    setPinnedSkill("auto");
    setMessages(createDefaultMessages());
  };

  const handleStopStreaming = () => {
    abortRef.current?.abort();
  };

  const sendPrompt = async (rawPrompt: string, forcedSkill?: CopilotSkill) => {
    const parsed = parseInputWithSlash(rawPrompt, forcedSkill ?? pinnedSkill);
    if (parsed.explicitSkill === "clear") {
      setShowQuickMenu(false);
      setInput("");
      await handleReset();
      return;
    }

    const promptText = parsed.cleanPrompt;
    if (!promptText || isSending) return;

    const effectiveSkill = detectEffectiveSkill(
      parsed.explicitSkill === "auto" ? null : parsed.explicitSkill,
      promptText
    );
    const sendOpenUIHeader =
      effectiveSkill === "openui" || effectiveSkill === "image";

    setShowQuickMenu(false);
    const userMsg: StoredChatMessage = {
      id: `user-${Date.now()}`,
      role: "user",
      content:
        rawPrompt.trim() ||
        (parsed.explicitSkill ? `/${parsed.explicitSkill}` : promptText),
      openuiMode: sendOpenUIHeader,
      skill: effectiveSkill,
      timestamp: Date.now(),
    };
    const assistantId = `assistant-${Date.now() + 1}`;
    const assistantPlaceholder: StoredChatMessage = {
      id: assistantId,
      role: "assistant",
      content: sendOpenUIHeader
        ? `root = Card([header])\nheader = CardHeader("正在流式生成交互界面...", "目标模型: ${model}")`
        : effectiveSkill === "diagnose" || effectiveSkill === "fix"
          ? "正在采集平台号池、代理池与审计遥测并分析根因..."
          : "正在思考...",
      openuiMode: sendOpenUIHeader,
      skill: effectiveSkill,
      isStreaming: true,
      showSource: false,
      timestamp: Date.now(),
    };

    const historyWindow = messages
      .filter(
        (m) =>
          m.id !== "welcome-1" &&
          !m.isStreaming &&
          m.content.trim().length > 0
      )
      .slice(-12)
      .map((m) => ({
        role: m.role,
        content: m.content,
      }));

    setMessages((prev) => [...prev, userMsg, assistantPlaceholder]);
    setInput("");
    setIsSending(true);

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

      const systemContent = buildSystemPromptForSkill(effectiveSkill, liveDiag);
      const userPayload = buildUserPayloadForSkill(
        promptText,
        effectiveSkill,
        liveDiag
      );
      const apiMessages: Array<{ role: string; content: string }> = [
        { role: "system", content: systemContent },
        ...historyWindow,
        { role: "user", content: userPayload },
      ];

      const finalText = await streamChatCompletion({
        messages: apiMessages,
        model,
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

      if (!finalText.trim()) {
        throw new Error("上游未返回有效响应内容");
      }

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

      setMessages((prev) =>
        prev.map((m) =>
          m.id === assistantId
            ? {
                ...m,
                content: finalRenderedText,
                openuiMode: containsOpenUIDSL(finalRenderedText),
                isStreaming: false,
              }
            : m
        )
      );
    } catch (err: unknown) {
      if (err instanceof DOMException && err.name === "AbortError") {
        setMessages((prev) =>
          prev.map((m) =>
            m.id === assistantId
              ? {
                  ...m,
                  openuiMode: containsOpenUIDSL(m.content),
                  isStreaming: false,
                }
              : m
          )
        );
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

      setMessages((prev) =>
        prev.map((m) => {
          if (m.id !== assistantId) return m;
          if (liveDiag) {
            return {
              ...m,
              openuiMode: true,
              skill: "diagnose",
              isOfflineFallback: true,
              isStreaming: false,
              content: buildOfflineFallbackContent(liveDiag, errMsg),
            };
          }
          return {
            ...m,
            isStreaming: false,
            openuiMode: sendOpenUIHeader,
            content: sendOpenUIHeader
              ? `root = Card([header, errCallout, fallbackDiag])\nheader = CardHeader("网关调用异常", "${model}")\nerrCallout = Callout("warning", "流式接口响应提示", ${JSON.stringify(
                  errMsg
                )})\nfallbackDiag = PlatformDiagnosisCard("本地平台诊断自愈台", "all")`
              : `> ⚠️ **请求异常**: ${errMsg}\n\n如因号池可用账号耗尽或代理熔断导致无法推理，请先在顶部确认管理端已解锁后执行 \`/diagnose\` 查看离线诊断报告。`,
          };
        })
      );
    } finally {
      abortRef.current = null;
      setIsSending(false);
    }
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

  const handleCopySystemPrompt = async () => {
    try {
      await navigator.clipboard.writeText(getOpenUISystemPrompt());
      setCopiedPrompt(true);
      window.setTimeout(() => setCopiedPrompt(false), 1600);
    } catch {
      // ignore
    }
  };

  const handleSkillClick = (item: SkillDefinition) => {
    setShowQuickMenu(false);
    if (item.skill === "clear") {
      void handleReset();
      return;
    }
    const targetSkill: CopilotSkill = item.skill;
    if (input.trim()) {
      void sendPrompt(`${item.command} ${input.trim()}`);
      return;
    }
    setPinnedSkill((prev) => (prev === targetSkill ? "auto" : targetSkill));
  };

  return (
    <div className="flex-1 flex flex-col min-h-0 h-full overflow-hidden">
      {/* Unified Copilot Control Strip */}
      <div className="px-4 py-2.5 z-surface border-b z-border flex flex-wrap items-center justify-between gap-2 shrink-0 text-xs">
        <div className="flex flex-wrap items-center gap-2">
          <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full border z-border p-surface font-medium">
            <MessageSquare size={12} />
            <span>
              {pinnedSkill === "auto"
                ? "Copilot · 自动意图 + 混合渲染"
                : `已锁定技能：/${pinnedSkill}`}
            </span>
          </span>

          <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full border z-pill-success">
            <span className="status-dot-green" />
            <span>
              {idbSynced ? "12 轮记忆 · IDB 同步" : "本地会话初始化中"}
            </span>
          </span>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <select
            value={model}
            onChange={(e) => setModel(e.target.value)}
            className="px-2.5 py-1.5 rounded-xl p-input font-mono-num text-xs"
            aria-label="选择对话模型"
          >
            {chatModels.map((m) => (
              <option key={m} value={m}>
                模型：{m}
              </option>
            ))}
          </select>

          <div className="hidden sm:flex items-center gap-1.5 px-2.5 py-1.5 rounded-xl p-input">
            <KeyRound size={12} className="z-text-muted shrink-0" />
            <input
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder="可选 API Key"
              className="w-24 bg-transparent focus:outline-none font-mono-num"
            />
          </div>

          <button
            type="button"
            onClick={() => setShowSystemPromptModal(true)}
            className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn cursor-pointer"
          >
            <FileCode2 size={12} />
            <span>OpenUI 规范</span>
          </button>

          <button
            type="button"
            onClick={handleReset}
            disabled={isSending}
            className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn cursor-pointer"
          >
            <RotateCcw size={12} />
            <span>清空重置</span>
          </button>
        </div>
      </div>

      {/* Hybrid Markdown + Scoped OpenUI Message Stream */}
      <div className="flex-1 overflow-y-auto px-4 py-5 min-h-0">
        <div className="max-w-3xl mx-auto space-y-4 text-xs">
          {/* Starter Prompt Chips */}
          <div className="flex flex-wrap gap-2">
            {STARTER_PROMPTS.map((item) => {
              const Icon = item.icon;
              return (
                <button
                  key={item.label}
                  type="button"
                  disabled={isSending}
                  onClick={() => sendPrompt(item.prompt)}
                  className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full border z-border z-surface z-ghost-btn cursor-pointer"
                >
                  <Icon size={12} className="z-text-muted" />
                  <span>{item.label}</span>
                </button>
              );
            })}
          </div>

          {messages.map((msg) => {
            const segments =
              msg.role === "assistant"
                ? splitHybridSegments(msg.content, msg.openuiMode)
                : [];
            const hasOpenUI = segments.some((s) => s.type === "openui");

            return (
              <div
                key={msg.id}
                className={
                  msg.role === "user" ? "flex justify-end" : "w-full"
                }
              >
                {msg.role === "user" ? (
                  <div className="px-4 py-2.5 rounded-2xl z-primary-btn max-w-xl leading-relaxed whitespace-pre-wrap">
                    {msg.content}
                  </div>
                ) : (
                  <div className="p-card rounded-2xl p-4 space-y-3">
                    <div className="flex flex-wrap items-center justify-between gap-2 pb-2.5 border-b z-border">
                      <div className="flex flex-wrap items-center gap-2">
                        <span
                          className={
                            msg.isStreaming
                              ? "status-dot-amber animate-pulse"
                              : "status-dot-green"
                          }
                        />
                        <span className="font-medium z-text-sec">
                          {msg.isStreaming
                            ? `${getSkillMetaLabel(msg)} · 流式生成中`
                            : getSkillMetaLabel(msg)}
                        </span>
                        {msg.isOfflineFallback && (
                          <span className="inline-flex items-center px-2 py-0.5 rounded-full border z-pill-warn text-[11px] font-medium">
                            0 可用账号 · 离线诊断引擎
                          </span>
                        )}
                      </div>

                      {hasOpenUI && (
                        <button
                          type="button"
                          onClick={() => toggleSource(msg.id)}
                          className="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg border z-border z-surface z-ghost-btn cursor-pointer"
                        >
                          <Code2 size={12} />
                          <span>
                            {msg.showSource ? "收起源码" : "查看 / 编辑源码"}
                          </span>
                        </button>
                      )}
                    </div>

                    {msg.showSource && hasOpenUI && (
                      <div className="space-y-1.5">
                        <div className="z-text-muted">
                          消息原文（支持直接微调其中的 Markdown 或 OpenUI DSL 片段）
                        </div>
                        <textarea
                          rows={6}
                          value={msg.content}
                          onChange={(e) =>
                            updateSourceLive(msg.id, e.target.value)
                          }
                          className="w-full p-3 rounded-xl p-code-box font-mono-num resize-y focus:outline-none"
                        />
                      </div>
                    )}

                    <div className="min-w-0 space-y-3 overflow-x-auto">
                      {segments.map((seg, idx) =>
                        seg.type === "markdown" ? (
                          <MarkdownContent
                            key={`${msg.id}-md-${idx}`}
                            content={seg.text}
                          />
                        ) : (
                          <ThemeProvider
                            key={`${msg.id}-oui-${idx}`}
                            mode={themeMode}
                          >
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
                                    void sendPrompt(actionMsg);
                                  }
                                }}
                              />
                            </div>
                          </ThemeProvider>
                        )
                      )}
                    </div>
                  </div>
                )}
              </div>
            );
          })}
          <div ref={streamEndRef} />
        </div>
      </div>

      {/* Floating Glass Composer with / Skill Pills */}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void sendPrompt(input);
        }}
        className="px-4 py-3 z-canvas border-t z-border shrink-0"
      >
        <div className="max-w-3xl mx-auto relative space-y-2">
          {showQuickMenu && (
            <div className="absolute bottom-full left-0 mb-2 w-full sm:w-96 p-surface rounded-2xl p-2 shadow-xl space-y-1 z-20 text-xs">
              <div className="px-2 py-1 z-text-muted font-medium">
                / 技能命令与快速诊断模板
              </div>
              {SLASH_SKILLS.map((item) => {
                const Icon = item.icon;
                return (
                  <button
                    key={item.command}
                    type="button"
                    onClick={() => {
                      if (item.skill === "clear") {
                        setShowQuickMenu(false);
                        setInput("");
                        void handleReset();
                      } else {
                        setShowQuickMenu(false);
                        void sendPrompt(item.command);
                      }
                    }}
                    className="w-full flex items-center justify-between gap-2 px-2.5 py-2 rounded-xl z-row-hover text-left cursor-pointer"
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      <Icon size={13} className="shrink-0 z-text-muted" />
                      <div className="min-w-0">
                        <div className="font-mono-num font-medium">
                          {item.command}{" "}
                          <span className="font-sans z-text-sec">
                            · {item.label}
                          </span>
                        </div>
                        <div className="text-[11px] z-text-muted truncate">
                          {item.description}
                        </div>
                      </div>
                    </div>
                    <span className="font-mono-num z-text-muted shrink-0">
                      执行 →
                    </span>
                  </button>
                );
              })}
            </div>
          )}

          {/* ChatGPT-style / Skill Pill Strip */}
          <div className="flex flex-wrap items-center gap-1.5 text-xs">
            {SLASH_SKILLS.map((item) => {
              const Icon = item.icon;
              const active = pinnedSkill === item.skill;
              return (
                <button
                  key={item.command}
                  type="button"
                  disabled={isSending}
                  onClick={() => handleSkillClick(item)}
                  title={item.description}
                  className={`inline-flex items-center gap-1 px-2.5 py-1 rounded-full border cursor-pointer transition-colors ${
                    active
                      ? "z-primary-btn border-transparent font-medium"
                      : "z-border z-surface z-ghost-btn"
                  }`}
                >
                  <Icon size={11} />
                  <span className="font-mono-num">{item.command}</span>
                  <span>{item.label}</span>
                </button>
              );
            })}
            {pinnedSkill !== "auto" && (
              <button
                type="button"
                onClick={() => setPinnedSkill("auto")}
                className="inline-flex items-center gap-1 px-2 py-1 rounded-full z-text-muted hover:underline cursor-pointer"
              >
                <X size={11} />
                <span>恢复自动意图</span>
              </button>
            )}
          </div>

          <div className="p-surface rounded-2xl p-3 space-y-2">
            <textarea
              rows={2}
              value={input}
              onChange={(e) => {
                const v = e.target.value;
                setInput(v);
                if (v === "/") setShowQuickMenu(true);
                else if (showQuickMenu && !v.startsWith("/")) {
                  setShowQuickMenu(false);
                }
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  void sendPrompt(input);
                } else if (e.key === "Escape") {
                  setShowQuickMenu(false);
                }
              }}
              placeholder={
                pinnedSkill === "diagnose"
                  ? "输入想排查的问题（如：为什么账号全过期/403 熔断），或直接发送执行全面诊断..."
                  : pinnedSkill === "fix"
                    ? "描述希望恢复的异常，或直接发送拉起自愈修复台..."
                    : pinnedSkill === "openui"
                      ? "输入需要流式生成的 OpenUI 交互式图表/表格..."
                      : "输入任何问题直接对话；输入 / 呼出 /diagnose、/fix、/openui、/image、/clear 技能..."
              }
              disabled={isSending}
              className="w-full bg-transparent text-xs focus:outline-none resize-none"
            />
            <div className="flex items-center justify-between gap-2 pt-1.5 border-t z-border text-xs">
              <div className="flex items-center gap-2 z-text-muted">
                <button
                  type="button"
                  onClick={() => setShowQuickMenu((s) => !s)}
                  className="font-mono-num px-2 py-0.5 rounded-md border z-border z-muted z-ghost-btn cursor-pointer"
                >
                  / 技能列表
                </button>
                <span className="hidden sm:inline">
                  Enter 发送 · Shift+Enter 换行 · 支持自动诊断与混合 OpenUI 渲染
                </span>
              </div>
              {isSending ? (
                <button
                  type="button"
                  onClick={handleStopStreaming}
                  className="inline-flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl border z-border z-surface font-medium cursor-pointer"
                >
                  <Square size={12} />
                  <span>停止生成</span>
                </button>
              ) : (
                <button
                  type="submit"
                  disabled={!input.trim() && pinnedSkill === "auto"}
                  onClick={(e) => {
                    if (!input.trim() && pinnedSkill !== "auto") {
                      e.preventDefault();
                      const match = SLASH_SKILLS.find(
                        (s) => s.skill === pinnedSkill
                      );
                      if (match?.defaultPrompt) {
                        void sendPrompt(match.command);
                      }
                    }
                  }}
                  className="inline-flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl z-primary-btn font-medium cursor-pointer"
                >
                  <SendHorizontal size={13} />
                  <span>
                    {!input.trim() && pinnedSkill !== "auto"
                      ? `运行 /${pinnedSkill}`
                      : "发送请求"}
                  </span>
                </button>
              )}
            </div>
          </div>
        </div>
      </form>

      {/* System Prompt Viewer Modal */}
      {showSystemPromptModal && (
        <div
          className="fixed inset-0 z-50 bg-black/55 backdrop-blur-xs flex items-center justify-center p-4"
          onClick={() => setShowSystemPromptModal(false)}
        >
          <div
            className="p-surface z-text rounded-2xl max-w-2xl w-full p-5 shadow-2xl space-y-3 text-xs"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-start justify-between gap-2 pb-2.5 border-b z-border">
              <div>
                <h3 className="text-sm font-semibold">
                  OpenUI Lang System Prompt 规范
                </h3>
                <p className="z-text-muted mt-0.5">
                  携带请求头{" "}
                  <code className="px-1 rounded z-muted font-mono-num">
                    X-OpenUI-Lang: true
                  </code>{" "}
                  调用{" "}
                  <code className="px-1 rounded z-muted font-mono-num">
                    /v1/chat/completions
                  </code>{" "}
                  时将自动前置注入；常规聊天与诊断不触发外部 API 注入。
                </p>
              </div>
              <div className="flex items-center gap-1.5">
                <button
                  type="button"
                  onClick={handleCopySystemPrompt}
                  className="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg border z-border z-ghost-btn cursor-pointer"
                >
                  {copiedPrompt ? <Check size={12} /> : <Copy size={12} />}
                  <span>{copiedPrompt ? "已复制" : "复制"}</span>
                </button>
                <button
                  type="button"
                  onClick={() => setShowSystemPromptModal(false)}
                  className="p-1 rounded-lg border z-border z-ghost-btn cursor-pointer"
                >
                  <X size={14} />
                </button>
              </div>
            </div>
            <pre className="p-3 rounded-xl p-code-box font-mono-num max-h-96 overflow-auto text-xs leading-relaxed">
              {getOpenUISystemPrompt()}
            </pre>
          </div>
        </div>
      )}
    </div>
  );
};
