import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  ActionBarPrimitive,
  AssistantRuntimeProvider,
  AttachmentPrimitive,
  AuiIf,
  BranchPickerPrimitive,
  ComposerPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
  useAui,
  useAuiState,
  useLocalRuntime,
  useRemoteThreadListRuntime,
} from "@assistant-ui/react";
import {
  Activity,
  Bot,
  Check,
  ChevronDown,
  Clapperboard,
  Code2,
  Copy,
  Edit3,
  FileText,
  Globe,
  ImagePlus,
  KeyRound,
  Layers,
  PanelLeftOpen,
  Paperclip,
  Plus,
  RefreshCw,
  RotateCcw,
  Search,
  SendHorizontal,
  ShieldAlert,
  Sparkles,
  Square,
  Trash2,
  Wand2,
  Wrench,
  X,
} from "lucide-react";
import {
  deleteServerChatPreset,
  fetchServerChatPresets,
  importServerConversations,
  saveServerChatPreset,
  searchServerChat,
  type ServerChatPreset,
  type ServerSearchHit,
} from "../../api/client";
import {
  CHAT_REMIX_EVENT,
  consumePendingChatRemix,
  type PendingChatRemix,
} from "../../lib/chatRemixBridge";
import { createChatAttachmentAdapter } from "../../lib/chat/attachments";
import {
  createChatModelAdapter,
  type ChatMessageCustomMeta,
  type ChatRuntimeConfig,
} from "../../lib/chat/chatModelAdapter";
import { createBackendThreadListAdapter } from "../../lib/chat/threadAdapters";
import { dispatchDramaCanvas } from "../../lib/dramaCanvasBridge";
import { FALLBACK_CATALOG_IDS, useModelsCatalog } from "../../lib/models";
import { DEFAULT_IMAGE_MODELS, DEFAULT_TTS_MODELS } from "../../lib/model-router";
import { OpenUITextPart } from "./OpenUITextPart";
import { ThreadSidebar } from "./ThreadSidebar";

const CHAT_MODEL_STORAGE_KEY = "ai_web_free_chat_model";

const FALLBACK_CHAT_MODELS = FALLBACK_CATALOG_IDS.filter(
  (id) =>
    !id.includes("image") &&
    !id.includes("dall-e") &&
    !id.includes("tts") &&
    !id.includes("transcribe") &&
    !id.includes("whisper")
);

function getStoredChatModel(): string {
  try {
    return window.sessionStorage.getItem(CHAT_MODEL_STORAGE_KEY) || "gpt-5-6";
  } catch {
    return "gpt-5-6";
  }
}

function formatDuration(ms?: number): string {
  if (!ms || ms <= 0) return "";
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

interface SlashSkillDef {
  command: string;
  label: string;
  description: string;
  defaultPrompt: string;
  icon: React.ComponentType<{ size?: number; className?: string }>;
}

const SLASH_SKILLS: SlashSkillDef[] = [
  {
    command: "/diagnose",
    label: "诊断平台",
    description: "采集号池/代理/卡密/审计实时遥测，输出 5 类根因分析与诊断卡",
    defaultPrompt:
      "/diagnose 请结合实时平台遥测数据，全面诊断当前号池熔断、账号封禁、403 WAF 拦截、401 过期、429 限流及代理异常的根因，并附带诊断卡片。",
    icon: ShieldAlert,
  },
  {
    command: "/fix",
    label: "修复方案",
    description: "分析可恢复故障并挂载一站式自愈与确认清理工作台",
    defaultPrompt:
      "/fix 请分析当前平台故障并给出分层修复方案（优先安全自愈：切换健康代理、续期过期账号、停用封禁或补充匿名 UUID），并生成一键修复面板。",
    icon: Wrench,
  },
  {
    command: "/openui",
    label: "OpenUI 卡片",
    description: "以纯 Thesys OpenUI Lang 流式构建交互式仪表盘与图表",
    defaultPrompt:
      "/openui 请使用 OpenUI Lang 生成一份包含 CardHeader、PoolOverviewCard、AccountDiagnosticTable 与维护建议 Callout 的号池交互大盘。",
    icon: Sparkles,
  },
  {
    command: "/image",
    label: "AI 图像生成",
    description: "调用图像生成模型（支持 /image [模型] 提示词，缺省 auto 自动选择）",
    defaultPrompt: "/image 一个赛博朋克风格的微缩发光水晶立方体，棚拍微距，8k 高清",
    icon: ImagePlus,
  },
  {
    command: "/voice",
    label: "音色爬虫克隆",
    description: "先查本地系统音色库，未命中则搜索或爬取指定网页音频缓存到本地克隆入库",
    defaultPrompt: "/voice 霸总低沉男声 短剧配音",
    icon: Globe,
  },
  {
    command: "/crawl",
    label: "全工坊素材爬虫",
    description: "从网络/指定 URL 搜索并缓存音色、参考图或视频素材写入系统库供各工坊复用",
    defaultPrompt: "/crawl 古风仙侠女主立绘 参考图",
    icon: Globe,
  },
  {
    command: "/clear",
    label: "新建对话",
    description: "开启一个全新分支会话线程",
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
    prompt: "/fix 帮我诊断当前损坏代理和过期账号，并展示一键自愈与账号诊断表格。",
  },
  {
    icon: Globe,
    label: "搜集短剧音色/素材入库",
    prompt: "/voice 霸总低沉男声",
  },
  {
    icon: ImagePlus,
    label: "快捷生图 (/image)",
    prompt: "/image 一个赛博朋克风格的微缩发光水晶立方体，棚拍微距，8k 高清",
  },
  {
    icon: Layers,
    label: "多供应商 Failover 矩阵",
    prompt:
      "/openui 请使用 OpenUI Lang 的 Tabs、Table 与 Callout 组件生成一份多供应商（ChatGPT Web、Claude、Gemini）能力对比矩阵与无损 Failover 策略说明。",
  },
];

const QUICK_DOCK_CHIPS = [
  { label: "/diagnose 诊断", prompt: SLASH_SKILLS[0].defaultPrompt },
  { label: "/fix 自愈", prompt: SLASH_SKILLS[1].defaultPrompt },
  { label: "/image 生图", prompt: "/image " },
  { label: "/voice 音色", prompt: "/voice " },
];

const UserAttachmentChip: React.FC = () => {
  const att = useAuiState((s) => s.attachment);
  const previewSrc =
    att.type === "image" && att.content
      ? (att.content.find((p) => p.type === "image") as { image?: string } | undefined)
          ?.image
      : undefined;

  return (
    <AttachmentPrimitive.Root className="inline-flex items-center gap-1.5 rounded-lg border z-border bg-[var(--bg-canvas)] px-2 py-1 text-xs">
      {previewSrc ? (
        <img
          src={previewSrc}
          alt={att.name}
          className="h-5 w-5 rounded object-cover shrink-0"
        />
      ) : (
        <FileText size={12} className="z-text-sec shrink-0" />
      )}
      <span className="max-w-[160px] truncate z-text font-medium">
        <AttachmentPrimitive.Name />
      </span>
      <AttachmentPrimitive.Remove className="ml-0.5 rounded p-0.5 z-text-muted hover:text-rose-500 hover:bg-rose-500/10 cursor-pointer">
        <X size={11} />
      </AttachmentPrimitive.Remove>
    </AttachmentPrimitive.Root>
  );
};

const MessageAttachmentPreview: React.FC = () => {
  const att = useAuiState((s) => s.attachment);
  const imagePart = att.content?.find(
    (p): p is { type: "image"; image: string } => p.type === "image"
  );
  if (imagePart?.image) {
    return (
      <AttachmentPrimitive.Root className="overflow-hidden rounded-xl border z-border bg-[var(--bg-surface)] shadow-2xs max-w-[240px]">
        <img
          src={imagePart.image}
          alt={att.name}
          className="max-h-44 w-auto object-contain"
        />
      </AttachmentPrimitive.Root>
    );
  }
  return (
    <AttachmentPrimitive.Root className="inline-flex items-center gap-1.5 rounded-lg border z-border bg-[var(--bg-surface)] px-2.5 py-1 text-[11px] z-text-sec">
      <FileText size={12} className="z-text-muted shrink-0" />
      <AttachmentPrimitive.Name />
    </AttachmentPrimitive.Root>
  );
};

const UserMessageRow: React.FC = () => {
  return (
    <MessagePrimitive.Root className="group flex w-full max-w-5xl flex-col items-end py-2 mx-auto">
      <div className="flex flex-col items-end gap-1.5 max-w-[78ch]">
        <MessagePrimitive.Attachments
          components={{
            Attachment: MessageAttachmentPreview,
          }}
        />
        <div className="rounded-2xl rounded-tr-sm border z-border bg-[var(--bg-muted)] z-text px-4 py-2.5 text-[13.5px] leading-relaxed whitespace-pre-wrap break-words shadow-2xs">
          <MessagePrimitive.Parts
            components={{
              Text: ({ text }) => <span>{text}</span>,
            }}
          />
        </div>
      </div>

      <div className="mt-1 flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
        <ActionBarPrimitive.Root className="flex items-center gap-1 text-[11px] z-text-muted">
          <ActionBarPrimitive.Copy className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded hover:bg-[var(--bg-muted)] hover:z-text cursor-pointer">
            <Copy size={11} />
            <span>复制</span>
          </ActionBarPrimitive.Copy>
          <ActionBarPrimitive.Edit className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded hover:bg-[var(--bg-muted)] hover:z-text cursor-pointer">
            <Edit3 size={11} />
            <span>编辑并分叉</span>
          </ActionBarPrimitive.Edit>
        </ActionBarPrimitive.Root>
      </div>
    </MessagePrimitive.Root>
  );
};

const EditUserComposer: React.FC = () => {
  return (
    <MessagePrimitive.Root className="flex w-full max-w-5xl flex-col items-end py-2 mx-auto">
      <ComposerPrimitive.Root className="w-full max-w-[78ch] rounded-2xl border z-border-strong p-surface p-3 space-y-2.5">
        <ComposerPrimitive.Input
          autoFocus
          className="w-full resize-none bg-transparent text-sm z-text outline-none min-h-[60px]"
        />
        <div className="flex items-center justify-end gap-2">
          <ComposerPrimitive.Cancel className="rounded-lg border z-border px-3 py-1 text-xs z-text-sec hover:bg-[var(--bg-muted)] cursor-pointer">
            取消
          </ComposerPrimitive.Cancel>
          <ComposerPrimitive.Send className="rounded-lg z-primary-btn px-3 py-1 text-xs font-medium cursor-pointer">
            保存并分叉重新生成
          </ComposerPrimitive.Send>
        </div>
      </ComposerPrimitive.Root>
    </MessagePrimitive.Root>
  );
};

interface AssistantMessageRowProps {
  themeMode: "light" | "dark";
  availableModels: string[];
}

const AssistantMessageRow: React.FC<AssistantMessageRowProps> = ({
  themeMode,
  availableModels,
}) => {
  const aui = useAui();
  const msg = useAuiState((s) => s.message);
  const isLast = useAuiState((s) => s.message.isLast);
  const [showSource, setShowSource] = useState(false);
  const [showModelPicker, setShowModelPicker] = useState(false);
  const [copied, setCopied] = useState(false);

  const isRunning = msg.status?.type === "running";
  const customMeta = ((msg.metadata as { custom?: ChatMessageCustomMeta } | undefined)
    ?.custom || {}) as ChatMessageCustomMeta;

  const plainText = msg.content
    .filter((p): p is { type: "text"; text: string } => p.type === "text")
    .map((p) => p.text)
    .join("\n");

  const hasOpenUI =
    Boolean(customMeta.openuiMode) ||
    plainText.includes("```openui") ||
    /^\s*root\s*=\s*[A-Z]/m.test(plainText);

  const formattedTime = formatDuration(customMeta.latencyMs);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(plainText);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // ignore
    }
  };

  return (
    <MessagePrimitive.Root className="group flex w-full max-w-5xl flex-col py-2.5 mx-auto">
      {/* 去套娃布局：助手消息采用无框流式正文 + 独立 OpenUI 画板卡片 */}
      <div className="w-full space-y-2.5">
        <div className="flex items-center justify-between gap-2 text-[11px] z-text-muted">
          <div className="flex items-center gap-2">
            <span className="inline-flex h-5 w-5 items-center justify-center rounded-md border z-border bg-[var(--bg-surface)] z-text shadow-2xs">
              <Bot size={12} className={isRunning ? "animate-pulse" : ""} />
            </span>
            <span className="font-mono font-medium z-text-sec">
              {customMeta.model || "Copilot"}
            </span>
            {customMeta.isOfflineFallback && (
              <span className="rounded-md border z-pill-warn px-1.5 py-0.5 text-[10px] font-medium">
                离线诊断兜底
              </span>
            )}
            {formattedTime && (
              <span className="font-mono-num opacity-75">· {formattedTime}</span>
            )}
          </div>
          {isRunning && (
            <span className="inline-flex items-center gap-1.5 text-[11px] z-text-sec">
              <span className="h-1.5 w-1.5 rounded-full bg-emerald-500 animate-ping" />
              <span>生成中</span>
            </span>
          )}
        </div>

        <div className="pl-7 space-y-2.5">
          <MessagePrimitive.Parts
            components={{
              Text: (partProps) => (
                <OpenUITextPart
                  {...partProps}
                  themeMode={themeMode}
                  showSource={showSource}
                />
              ),
              Empty: () =>
                isRunning ? (
                  <div className="flex items-center gap-2 text-xs z-text-muted py-1">
                    <span className="h-2 w-2 rounded-full bg-emerald-500 animate-pulse" />
                    <span>正在连接网关推理引擎...</span>
                  </div>
                ) : null,
              tools: {
                Fallback: ({ toolName, status }) => (
                  <div className="my-1 inline-flex items-center gap-2 rounded-lg border z-border bg-[var(--bg-surface)] px-2.5 py-1 text-xs z-text-sec">
                    <span
                      className={`h-1.5 w-1.5 rounded-full ${
                        status.type === "running"
                          ? "animate-ping bg-sky-500"
                          : "bg-emerald-500"
                      }`}
                    />
                    <span>
                      {status.type === "running"
                        ? `正在检索数据 (${toolName})...`
                        : `数据已就绪 (${toolName})`}
                    </span>
                  </div>
                ),
              },
            }}
          />

          {msg.status?.type === "incomplete" && msg.status.reason === "error" && (
            <div className="max-w-[78ch] rounded-xl border z-pill-danger px-3 py-2 text-xs">
              请求执行中断：
              {typeof msg.status.error === "string"
                ? msg.status.error
                : "上游响应异常，可点击下方重试或切换模型"}
            </div>
          )}

          {/* 末条消息常驻操作栏，历史消息悬停静默显现 */}
          <div
            className={`flex flex-wrap items-center justify-between gap-2 pt-1 text-[11px] z-text-muted transition-opacity ${
              isLast || showModelPicker
                ? "opacity-100"
                : "opacity-0 group-hover:opacity-100"
            }`}
          >
            <div className="flex flex-wrap items-center gap-1">
              <BranchPickerPrimitive.Root
                hideWhenSingleBranch
                className="inline-flex items-center gap-1 rounded-md border z-border bg-[var(--bg-surface)] px-1.5 py-0.5 mr-1"
              >
                <BranchPickerPrimitive.Previous className="px-1 hover:z-text disabled:opacity-30 cursor-pointer">
                  ‹
                </BranchPickerPrimitive.Previous>
                <span className="font-mono-num text-[11px]">
                  <BranchPickerPrimitive.Number /> / <BranchPickerPrimitive.Count />
                </span>
                <BranchPickerPrimitive.Next className="px-1 hover:z-text disabled:opacity-30 cursor-pointer">
                  ›
                </BranchPickerPrimitive.Next>
              </BranchPickerPrimitive.Root>

              <button
                type="button"
                onClick={handleCopy}
                className="inline-flex items-center gap-1 px-2 py-1 rounded-md hover:bg-[var(--bg-muted)] hover:z-text transition cursor-pointer"
                title="复制全文"
              >
                {copied ? (
                  <>
                    <Check size={12} className="text-emerald-500" />
                    <span className="text-emerald-600 dark:text-emerald-400 font-medium">
                      已复制
                    </span>
                  </>
                ) : (
                  <>
                    <Copy size={12} />
                    <span>复制</span>
                  </>
                )}
              </button>

              {!isRunning && (
                <ActionBarPrimitive.Reload className="inline-flex items-center gap-1 px-2 py-1 rounded-md hover:bg-[var(--bg-muted)] hover:z-text transition cursor-pointer">
                  <RefreshCw size={12} />
                  <span>重试</span>
                </ActionBarPrimitive.Reload>
              )}

              {!isRunning && (
                <div className="relative">
                  <button
                    type="button"
                    onClick={() => setShowModelPicker((v) => !v)}
                    className="inline-flex items-center gap-1 px-2 py-1 rounded-md hover:bg-[var(--bg-muted)] hover:z-text transition cursor-pointer"
                    title="切换其他模型生成新分支"
                  >
                    <span>换模型</span>
                    <ChevronDown size={11} />
                  </button>
                  {showModelPicker && (
                    <div className="absolute left-0 bottom-full mb-1 z-30 w-52 max-h-60 overflow-y-auto rounded-xl border z-border bg-[var(--bg-surface)] shadow-xl p-1">
                      {availableModels.map((m) => (
                        <button
                          key={m}
                          type="button"
                          onClick={() => {
                            setShowModelPicker(false);
                            aui.message().reload({
                              runConfig: { custom: { overrideModel: m } },
                            });
                          }}
                          className="w-full text-left px-2.5 py-1.5 rounded-lg text-xs hover:bg-[var(--bg-muted)] z-text truncate cursor-pointer"
                        >
                          {m}
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              )}

              {!isRunning && plainText.trim().length > 0 && (
                <button
                  type="button"
                  onClick={() =>
                    dispatchDramaCanvas({
                      type: "script",
                      title:
                        plainText.trim().split("\n")[0].slice(0, 22) ||
                        "对话工坊剧本",
                      text: plainText,
                      sourceView: "chat",
                    })
                  }
                  className="inline-flex items-center gap-1 px-2 py-1 rounded-md hover:bg-[var(--bg-muted)] hover:z-text transition cursor-pointer"
                  title="将此段对话推送到短剧无限画布剧本节点"
                >
                  <Clapperboard size={12} />
                  <span>推至短剧</span>
                </button>
              )}
            </div>

            {hasOpenUI && (
              <button
                type="button"
                onClick={() => setShowSource((v) => !v)}
                className="inline-flex items-center gap-1 px-2 py-0.5 rounded-md border z-border bg-[var(--bg-surface)] hover:bg-[var(--bg-muted)] hover:z-text transition cursor-pointer"
              >
                <Code2 size={12} />
                <span>{showSource ? "渲染视图" : "DSL 源码"}</span>
              </button>
            )}
          </div>
        </div>
      </div>
    </MessagePrimitive.Root>
  );
};

interface ChatWorkspaceInnerProps {
  themeMode: "light" | "dark";
  config: ChatRuntimeConfig;
  onUpdateConfig: (updater: (prev: ChatRuntimeConfig) => ChatRuntimeConfig) => void;
}

const ChatWorkspaceInner: React.FC<ChatWorkspaceInnerProps> = ({
  themeMode,
  config,
  onUpdateConfig,
}) => {
  const aui = useAui();
  const composerText = useAuiState((s) => s.composer.text);
  const isRunning = useAuiState((s) => s.thread.isRunning);

  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [presets, setPresets] = useState<ServerChatPreset[]>([]);
  const [activePresetId, setActivePresetId] = useState<string>("");
  const [showPresetModal, setShowPresetModal] = useState(false);
  const [newPresetName, setNewPresetName] = useState("");
  const [newPresetPrompt, setNewPresetPrompt] = useState("");
  const [showApiKeyBar, setShowApiKeyBar] = useState(false);

  const [showSearchModal, setShowSearchModal] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [searchHits, setSearchHits] = useState<ServerSearchHit[]>([]);
  const [searching, setSearching] = useState(false);

  const [pendingRemix, setPendingRemix] = useState<PendingChatRemix | null>(null);
  const [remixMode, setRemixMode] = useState<"edit" | "chat">("edit");

  useEffect(() => {
    fetchServerChatPresets()
      .then((list) => setPresets(list))
      .catch(() => {});
  }, []);

  useEffect(() => {
    const initial = consumePendingChatRemix();
    if (initial) {
      setPendingRemix(initial);
      setRemixMode("edit");
    }
    const onRemix = (e: Event) => {
      const detail = (e as CustomEvent<PendingChatRemix>).detail;
      if (detail) {
        setPendingRemix(detail);
        setRemixMode("edit");
      }
    };
    window.addEventListener(CHAT_REMIX_EVENT, onRemix);
    return () => window.removeEventListener(CHAT_REMIX_EVENT, onRemix);
  }, []);

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "f" && e.shiftKey) {
        e.preventDefault();
        setShowSearchModal(true);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  useEffect(() => {
    aui.composer().setRunConfig({
      custom: pendingRemix
        ? { remixImageUrl: pendingRemix.imageUrl, remixMode }
        : undefined,
    });
  }, [aui, pendingRemix, remixMode]);

  useEffect(() => {
    if (!showSearchModal) return;
    const q = searchQuery.trim();
    if (!q) {
      setSearchHits([]);
      return;
    }
    let cancelled = false;
    setSearching(true);
    const timer = window.setTimeout(() => {
      searchServerChat(q, 20)
        .then((res) => {
          if (!cancelled) setSearchHits(res.hits || []);
        })
        .catch(() => {
          if (!cancelled) setSearchHits([]);
        })
        .finally(() => {
          if (!cancelled) setSearching(false);
        });
    }, 220);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [searchQuery, showSearchModal]);

  const handleSelectPreset = (presetId: string) => {
    setActivePresetId(presetId);
    const found = presets.find((p) => p.id === presetId);
    onUpdateConfig((prev) => ({
      ...prev,
      systemPrompt: found ? found.system_prompt : undefined,
      model:
        found?.default_model && found.default_model !== "auto"
          ? found.default_model
          : prev.model,
    }));
  };

  const handleSaveCustomPreset = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newPresetName.trim() || !newPresetPrompt.trim()) return;
    try {
      const saved = await saveServerChatPreset({
        name: newPresetName.trim(),
        system_prompt: newPresetPrompt.trim(),
        default_model: config.model,
      });
      setPresets((prev) => [...prev, saved]);
      setActivePresetId(saved.id);
      onUpdateConfig((prev) => ({ ...prev, systemPrompt: saved.system_prompt }));
      setNewPresetName("");
      setNewPresetPrompt("");
      setShowPresetModal(false);
    } catch {
      // ignore
    }
  };

  const handleImportJson = async (file: File) => {
    try {
      const text = await file.text();
      const parsed = JSON.parse(text);
      const conversations = Array.isArray(parsed)
        ? parsed
        : parsed.conversations || [parsed];
      await importServerConversations({ conversations });
      await aui.threads().reload();
    } catch {
      // ignore invalid file
    }
  };

  const handleQuickPrompt = (prompt: string) => {
    if (!prompt.trim()) return;
    if (prompt.trim() === "/clear") {
      aui.threads().switchToNewThread();
      return;
    }
    if (prompt.endsWith(" ")) {
      aui.composer().setText(prompt);
      return;
    }
    aui.thread().append({
      role: "user",
      content: [{ type: "text", text: prompt }],
    });
  };

  const filteredSlashSkills = useMemo(() => {
    const trimmed = composerText.trim();
    if (!trimmed.startsWith("/") || trimmed.includes(" ")) return [];
    const q = trimmed.toLowerCase();
    return SLASH_SKILLS.filter((s) => s.command.startsWith(q));
  }, [composerText]);

  const allRetryModels = useMemo(() => {
    const set = new Set([
      ...config.availableChatModels,
      ...config.availableImageModels,
      ...config.availableAudioModels,
    ]);
    return Array.from(set);
  }, [config]);

  return (
    <div className="flex h-full w-full overflow-hidden bg-[var(--bg-canvas)]">
      <ThreadSidebar
        open={sidebarOpen}
        onCloseMobile={() => setSidebarOpen(false)}
        onOpenSearch={() => setShowSearchModal(true)}
        onImportJson={(file) => void handleImportJson(file)}
      />

      <main className="flex flex-1 flex-col min-w-0 h-full bg-[var(--bg-canvas)] relative">
        {/* 统一极简顶栏 */}
        <header className="h-11 shrink-0 flex items-center justify-between gap-2 p-topbar px-3">
          <div className="flex items-center gap-1.5 min-w-0">
            <button
              type="button"
              onClick={() => setSidebarOpen((v) => !v)}
              className="h-7.5 w-7.5 inline-flex items-center justify-center rounded-lg border z-border z-surface z-ghost-btn z-text-sec cursor-pointer"
              title={sidebarOpen ? "收起会话列表" : "展开会话列表"}
            >
              <PanelLeftOpen size={13} />
            </button>

            <select
              value={config.model}
              onChange={(e) => {
                const next = e.target.value;
                try {
                  window.sessionStorage.setItem(CHAT_MODEL_STORAGE_KEY, next);
                } catch {}
                onUpdateConfig((prev) => ({ ...prev, model: next }));
              }}
              className="h-7.5 rounded-lg border z-border bg-[var(--bg-surface)] px-2.5 text-xs font-medium z-text outline-none max-w-[175px] cursor-pointer"
              title="选择主推理模型"
            >
              {config.availableChatModels.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
              {config.availableImageModels.map((m) => (
                <option key={m} value={m}>
                  🎨 {m}
                </option>
              ))}
              {config.availableAudioModels.map((m) => (
                <option key={m} value={m}>
                  🔊 {m}
                </option>
              ))}
            </select>

            <select
              value={activePresetId}
              onChange={(e) => handleSelectPreset(e.target.value)}
              className="h-7.5 rounded-lg border z-border bg-[var(--bg-surface)] px-2.5 text-xs z-text-sec outline-none max-w-[165px] cursor-pointer"
              title="选择角色预设"
            >
              <option value="">默认 OpenUI Copilot</option>
              {presets.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>

            <button
              type="button"
              onClick={() => setShowPresetModal(true)}
              className="h-7.5 inline-flex items-center gap-1 rounded-lg border z-border z-surface z-ghost-btn px-2 text-[11px] z-text-sec cursor-pointer"
              title="新建/管理角色预设"
            >
              <Plus size={11} />
              <span className="hidden sm:inline">预设</span>
            </button>
          </div>

          <div className="flex items-center gap-1.5">
            <button
              type="button"
              onClick={() =>
                onUpdateConfig((prev) => ({
                  ...prev,
                  autoSwitchModel: !prev.autoSwitchModel,
                }))
              }
              className={`h-7.5 inline-flex items-center gap-1.5 rounded-lg border px-2.5 text-[11px] font-medium transition cursor-pointer ${
                config.autoSwitchModel
                  ? "z-pill-info"
                  : "z-border z-surface z-text-muted hover:bg-[var(--bg-muted)]"
              }`}
              title="自然语言自动识别生图/语音/对话意图并切换模型"
            >
              <Wand2 size={12} />
              <span className="hidden sm:inline">智能路由</span>
            </button>

            <button
              type="button"
              onClick={() =>
                onUpdateConfig((prev) => ({ ...prev, openuiMode: !prev.openuiMode }))
              }
              className={`h-7.5 inline-flex items-center gap-1.5 rounded-lg border px-2.5 text-[11px] font-medium transition cursor-pointer ${
                config.openuiMode
                  ? "z-pill-success"
                  : "z-border z-surface z-text-muted hover:bg-[var(--bg-muted)]"
              }`}
              title="开启后优先注入 X-OpenUI-Lang: true 生成交互看板"
            >
              <Sparkles size={12} />
              <span className="hidden sm:inline">OpenUI 看板</span>
            </button>

            <button
              type="button"
              onClick={() => setShowApiKeyBar((v) => !v)}
              className={`h-7.5 w-7.5 inline-flex items-center justify-center rounded-lg border transition cursor-pointer ${
                config.apiKey
                  ? "z-pill-info"
                  : "z-border z-surface z-ghost-btn z-text-muted"
              }`}
              title="可选 Bearer API Key"
            >
              <KeyRound size={13} />
            </button>
          </div>
        </header>

        {showApiKeyBar && (
          <div className="flex items-center gap-2 border-b z-border bg-[var(--bg-surface)] px-4 py-1.5 text-xs">
            <span className="z-text-sec shrink-0">Gateway Bearer Key (可选):</span>
            <input
              type="password"
              value={config.apiKey || ""}
              onChange={(e) =>
                onUpdateConfig((prev) => ({ ...prev, apiKey: e.target.value }))
              }
              placeholder="若服务端配置了 AUTHORIZATION，可在此填入密钥"
              className="flex-1 rounded-md border z-border bg-[var(--bg-input)] px-2.5 py-1 text-xs z-text outline-none"
            />
          </div>
        )}

        {/* 主消息流视口 */}
        <ThreadPrimitive.Root className="flex flex-1 flex-col min-h-0 relative">
          <ThreadPrimitive.Viewport className="flex-1 overflow-y-auto px-4 sm:px-8 pt-5 pb-6 space-y-1">
            <ThreadPrimitive.Empty>
              <div className="mx-auto max-w-4xl py-8 space-y-5">
                <div className="rounded-2xl p-card p-6 space-y-4">
                  <div className="flex items-center gap-3">
                    <span className="inline-flex h-9 w-9 items-center justify-center rounded-xl border z-border bg-[var(--bg-canvas)] z-text shadow-2xs">
                      <Sparkles size={17} />
                    </span>
                    <div>
                      <h2 className="text-sm font-semibold z-text">
                        AI-Web-Free Copilot · 分支工作台与 OpenUI
                      </h2>
                      <p className="text-xs z-text-muted mt-0.5">
                        支持分支时间旅行、历史编辑分叉、CSV/JSON/截图上传生成交互看板与实时号池自愈
                      </p>
                    </div>
                  </div>

                  <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2.5">
                    {STARTER_PROMPTS.map((sp) => {
                      const Icon = sp.icon;
                      return (
                        <button
                          key={sp.label}
                          type="button"
                          onClick={() => handleQuickPrompt(sp.prompt)}
                          className="flex items-center gap-2.5 rounded-xl p-card p-card-interactive px-3.5 py-3 text-left text-xs font-medium z-text cursor-pointer"
                        >
                          <Icon size={14} className="z-text-sec shrink-0" />
                          <span className="truncate">{sp.label}</span>
                        </button>
                      );
                    })}
                  </div>
                </div>
              </div>
            </ThreadPrimitive.Empty>

            <ThreadPrimitive.Messages
              components={{
                UserMessage: UserMessageRow,
                EditComposer: EditUserComposer,
                AssistantMessage: () => (
                  <AssistantMessageRow
                    themeMode={themeMode}
                    availableModels={allRetryModels}
                  />
                ),
              }}
            />

            <ThreadPrimitive.ScrollToBottom className="sticky bottom-3 mx-auto flex items-center justify-center rounded-full border z-border p-surface px-3 py-1 text-xs z-text-sec hover:z-text cursor-pointer">
              ↓ 回到最新
            </ThreadPrimitive.ScrollToBottom>
          </ThreadPrimitive.Viewport>

          {/* 悬浮输入坞 (Floating Composer Dock) */}
          <div className="shrink-0 bg-gradient-to-t from-[var(--bg-canvas)] via-[var(--bg-canvas)]/95 to-transparent px-4 sm:px-8 pt-2 pb-3.5">
            <div className="mx-auto max-w-4xl space-y-2 relative">
              {pendingRemix && (
                <div className="flex items-center justify-between gap-2 rounded-xl border z-pill-info px-3 py-2 text-xs">
                  <div className="flex items-center gap-2.5 min-w-0">
                    <img
                      src={pendingRemix.imageUrl}
                      alt="Remix 参考图"
                      className="h-9 w-9 rounded-lg object-cover border z-border shrink-0"
                    />
                    <div className="min-w-0">
                      <div className="font-medium z-text truncate">
                        已挂载参考图 ({pendingRemix.model || "image"})
                      </div>
                      <div className="text-[11px] z-text-muted truncate">
                        {pendingRemix.prompt || "输入修改指令即可直接重绘或对话"}
                      </div>
                    </div>
                  </div>
                  <div className="flex items-center gap-1.5 shrink-0">
                    <button
                      type="button"
                      onClick={() =>
                        setRemixMode((m) => (m === "edit" ? "chat" : "edit"))
                      }
                      className="rounded-lg border z-border bg-[var(--bg-surface)] px-2 py-1 text-[11px] font-medium z-text cursor-pointer"
                    >
                      {remixMode === "edit" ? "模式: 修图重绘" : "模式: 带图对话"}
                    </button>
                    <button
                      type="button"
                      onClick={() => setPendingRemix(null)}
                      className="p-1 rounded hover:bg-[var(--bg-muted)] z-text-muted cursor-pointer"
                    >
                      <X size={13} />
                    </button>
                  </div>
                </div>
              )}

              {filteredSlashSkills.length > 0 && (
                <div className="absolute bottom-full left-0 right-0 mb-2 z-20 rounded-xl border z-border p-surface p-1.5 space-y-0.5">
                  {filteredSlashSkills.map((sk) => {
                    const Icon = sk.icon;
                    return (
                      <button
                        key={sk.command}
                        type="button"
                        onClick={() => {
                          if (sk.command === "/clear") {
                            aui.composer().setText("");
                            aui.threads().switchToNewThread();
                          } else {
                            aui.composer().setText(sk.defaultPrompt);
                          }
                        }}
                        className="w-full flex items-center gap-2.5 rounded-lg px-2.5 py-2 text-left hover:bg-[var(--bg-surface-hover)] transition cursor-pointer"
                      >
                        <Icon size={14} className="z-text-sec shrink-0" />
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center gap-2 text-xs font-medium z-text">
                            <span className="font-mono">{sk.command}</span>
                            <span>{sk.label}</span>
                          </div>
                          <div className="text-[11px] z-text-muted truncate">
                            {sk.description}
                          </div>
                        </div>
                      </button>
                    );
                  })}
                </div>
              )}

              <ComposerPrimitive.AttachmentDropzone className="rounded-2xl">
                <ComposerPrimitive.Root
                  onSubmit={() => {
                    if (pendingRemix) {
                      window.setTimeout(() => setPendingRemix(null), 150);
                    }
                  }}
                  className="flex flex-col gap-2 rounded-2xl p-surface p-3 focus-within:border-[var(--hairline-strong)] transition"
                >
                  <div className="flex flex-wrap gap-1.5 empty:hidden">
                    <ComposerPrimitive.Attachments
                      components={{
                        Attachment: UserAttachmentChip,
                      }}
                    />
                  </div>

                  <ComposerPrimitive.Input
                    rows={1}
                    maxRows={6}
                    placeholder="输入消息，输入 / 呼出技能，或拖拽 CSV / JSON / 截图生成 OpenUI 看板..."
                    className="w-full resize-none bg-transparent px-1 py-0.5 text-[13.5px] z-text outline-none placeholder:z-text-muted"
                  />

                  <div className="flex items-center justify-between gap-2 pt-1 border-t z-border">
                    <div className="flex flex-wrap items-center gap-1">
                      <ComposerPrimitive.AddAttachment
                        className="h-7 inline-flex items-center gap-1 rounded-lg border z-border bg-[var(--bg-canvas)] px-2 text-[11px] font-medium z-text-sec hover:bg-[var(--bg-muted)] hover:z-text transition cursor-pointer"
                        title="上传 CSV / JSON / 代码 / 图片 / PDF"
                      >
                        <Paperclip size={12} />
                        <span>附件</span>
                      </ComposerPrimitive.AddAttachment>
                      {QUICK_DOCK_CHIPS.map((chip) => (
                        <button
                          key={chip.label}
                          type="button"
                          onClick={() => handleQuickPrompt(chip.prompt)}
                          className="hidden sm:inline-flex h-7 items-center rounded-lg px-2 text-[11px] z-text-muted hover:bg-[var(--bg-muted)] hover:z-text transition cursor-pointer"
                        >
                          {chip.label}
                        </button>
                      ))}
                    </div>

                    <div className="flex items-center gap-1.5">
                      <AuiIf condition={(s) => !s.thread.isRunning}>
                        <ComposerPrimitive.Send
                          disabled={isRunning}
                          className="h-7.5 inline-flex items-center gap-1.5 rounded-lg z-primary-btn px-3 text-xs font-medium transition cursor-pointer"
                        >
                          <SendHorizontal size={13} />
                          <span>发送</span>
                        </ComposerPrimitive.Send>
                      </AuiIf>
                      <AuiIf condition={(s) => s.thread.isRunning}>
                        <ComposerPrimitive.Cancel className="h-7.5 inline-flex items-center gap-1.5 rounded-lg bg-rose-600 hover:bg-rose-700 text-white px-3 text-xs font-medium transition cursor-pointer">
                          <Square size={12} />
                          <span>停止</span>
                        </ComposerPrimitive.Cancel>
                      </AuiIf>
                    </div>
                  </div>
                </ComposerPrimitive.Root>
              </ComposerPrimitive.AttachmentDropzone>
            </div>
          </div>
        </ThreadPrimitive.Root>
      </main>

      {/* 全文检索模态框 */}
      {showSearchModal && (
        <div
          className="fixed inset-0 z-50 flex items-start justify-center bg-black/50 backdrop-blur-xs pt-20 p-4"
          onClick={() => setShowSearchModal(false)}
        >
          <div
            className="w-full max-w-lg rounded-2xl p-surface shadow-2xl overflow-hidden"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center gap-2 border-b z-border px-3.5 py-2.5">
              <Search size={15} className="z-text-muted" />
              <input
                autoFocus
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="搜索所有历史会话内容..."
                className="flex-1 bg-transparent text-sm z-text outline-none"
              />
              <button
                type="button"
                onClick={() => setShowSearchModal(false)}
                className="p-1 rounded hover:bg-[var(--bg-muted)] z-text-muted"
              >
                <X size={14} />
              </button>
            </div>
            <div className="max-h-80 overflow-y-auto p-2 space-y-1">
              {searching ? (
                <div className="py-6 text-center text-xs z-text-muted">检索中...</div>
              ) : searchHits.length === 0 ? (
                <div className="py-6 text-center text-xs z-text-muted">
                  {searchQuery.trim() ? "未找到匹配的历史消息" : "输入关键词以检索跨会话消息"}
                </div>
              ) : (
                searchHits.map((hit) => (
                  <button
                    key={`${hit.conversation_id}-${hit.message_id}`}
                    type="button"
                    onClick={() => {
                      aui.threads().switchToThread(hit.conversation_id);
                      setShowSearchModal(false);
                    }}
                    className="w-full text-left rounded-xl px-3 py-2 hover:bg-[var(--bg-surface-hover)] transition space-y-0.5 cursor-pointer"
                  >
                    <div className="flex items-center justify-between text-xs font-medium z-text">
                      <span className="truncate">{hit.conversation_title}</span>
                      <span className="text-[10px] z-text-muted shrink-0 ml-2">
                        {hit.role}
                      </span>
                    </div>
                    <p className="text-[11px] z-text-muted line-clamp-2">{hit.snippet}</p>
                  </button>
                ))
              )}
            </div>
          </div>
        </div>
      )}

      {/* 角色预设管理模态框 */}
      {showPresetModal && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-xs p-4"
          onClick={() => setShowPresetModal(false)}
        >
          <div
            className="w-full max-w-md rounded-2xl p-surface shadow-2xl p-4 space-y-4"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-semibold z-text">新建自定义角色预设</h3>
              <button
                type="button"
                onClick={() => setShowPresetModal(false)}
                className="p-1 rounded hover:bg-[var(--bg-muted)] z-text-muted"
              >
                <X size={14} />
              </button>
            </div>
            <form onSubmit={handleSaveCustomPreset} className="space-y-3">
              <input
                value={newPresetName}
                onChange={(e) => setNewPresetName(e.target.value)}
                placeholder="预设名称（如：SQL 性能优化专家）"
                className="w-full rounded-xl border z-border bg-[var(--bg-input)] px-3 py-2 text-xs z-text outline-none"
              />
              <textarea
                value={newPresetPrompt}
                onChange={(e) => setNewPresetPrompt(e.target.value)}
                rows={4}
                placeholder="系统提示词 (System Prompt)..."
                className="w-full rounded-xl border z-border bg-[var(--bg-input)] px-3 py-2 text-xs z-text outline-none resize-y"
              />
              <div className="flex justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setShowPresetModal(false)}
                  className="rounded-xl border z-border px-3 py-1.5 text-xs z-text-sec hover:bg-[var(--bg-muted)]"
                >
                  取消
                </button>
                <button
                  type="submit"
                  className="rounded-xl z-primary-btn px-3.5 py-1.5 text-xs font-medium cursor-pointer"
                >
                  保存并启用
                </button>
              </div>
            </form>

            {presets.some((p) => !p.is_builtin) && (
              <div className="border-t z-border pt-3 space-y-1.5">
                <div className="text-[11px] font-medium z-text-muted">已保存的自定义预设</div>
                {presets
                  .filter((p) => !p.is_builtin)
                  .map((p) => (
                    <div
                      key={p.id}
                      className="flex items-center justify-between text-xs rounded-lg px-2 py-1 bg-[var(--bg-muted)]/60"
                    >
                      <span className="truncate">{p.name}</span>
                      <button
                        type="button"
                        onClick={() => {
                          void deleteServerChatPreset(p.id).then(() =>
                            setPresets((prev) => prev.filter((item) => item.id !== p.id))
                          );
                        }}
                        className="text-rose-500 hover:bg-rose-500/10 p-1 rounded cursor-pointer"
                      >
                        <Trash2 size={12} />
                      </button>
                    </div>
                  ))}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

export interface ChatWorkspaceProps {
  themeMode?: "light" | "dark";
}

export const ChatWorkspace: React.FC<ChatWorkspaceProps> = ({ themeMode = "dark" }) => {
  const { chatModels, imageModels, audioModels } = useModelsCatalog();

  const availableChatModels =
    chatModels.length > 0 ? chatModels.map((m) => m.id) : FALLBACK_CHAT_MODELS;
  const availableImageModels =
    imageModels.length > 0 ? imageModels.map((m) => m.id) : DEFAULT_IMAGE_MODELS;
  const availableAudioModels =
    audioModels.length > 0 ? audioModels.map((m) => m.id) : DEFAULT_TTS_MODELS;

  const [config, setConfig] = useState<ChatRuntimeConfig>(() => ({
    model: getStoredChatModel(),
    openuiMode: true,
    autoSwitchModel: true,
    availableChatModels: FALLBACK_CHAT_MODELS,
    availableImageModels: DEFAULT_IMAGE_MODELS,
    availableAudioModels: DEFAULT_TTS_MODELS,
  }));

  useEffect(() => {
    setConfig((prev) => ({
      ...prev,
      availableChatModels,
      availableImageModels,
      availableAudioModels,
    }));
  }, [
    availableChatModels.join(","),
    availableImageModels.join(","),
    availableAudioModels.join(","),
  ]);

  const configRef = useRef(config);
  configRef.current = config;

  const chatModelAdapter = useMemo(
    () => createChatModelAdapter(() => configRef.current),
    []
  );
  const attachmentAdapter = useMemo(
    () => createChatAttachmentAdapter(() => configRef.current.apiKey),
    []
  );
  const threadListAdapter = useMemo(
    () =>
      createBackendThreadListAdapter({
        getDefaultModel: () => configRef.current.model,
      }),
    []
  );

  const runtime = useRemoteThreadListRuntime({
    runtimeHook: () =>
      useLocalRuntime(chatModelAdapter, {
        adapters: {
          attachments: attachmentAdapter,
        },
      }),
    adapter: threadListAdapter,
  });

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <ChatWorkspaceInner
        themeMode={themeMode}
        config={config}
        onUpdateConfig={setConfig}
      />
    </AssistantRuntimeProvider>
  );
};
