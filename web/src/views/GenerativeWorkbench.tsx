import React, { useEffect, useRef, useState } from "react";
import { Renderer } from "@openuidev/react-lang";
import { ThemeProvider } from "@openuidev/react-ui";
import {
  BarChart3,
  Check,
  Code2,
  Copy,
  FileCode2,
  ImagePlus,
  KeyRound,
  Layers,
  Loader2,
  RotateCcw,
  SendHorizontal,
  Sparkles,
  X,
} from "lucide-react";
import {
  aiWebFreeLibrary,
  aiWebFreeToolProvider,
  getOpenUISystemPrompt,
} from "../lib/openui-library";
import { fetchModelsCatalog, streamChatCompletion } from "../api/client";
import { classifyModel } from "../lib/models";
import {
  clearStoredChat,
  loadStoredChat,
  saveStoredChat,
  type StoredChatMessage,
} from "../lib/storage";

const INITIAL_OPENUI_PROGRAM = `root = Card([header, poolCard, diagTable])
header = CardHeader("AI-Web-Free 实时诊断卡片", "基于声明式 OpenUI 组件流式生成 · 仅在携带 X-OpenUI-Lang: true 时激活，不污染标准 OpenAI API")
poolCard = PoolOverviewCard("chatgpt", "ChatGPT Web 免费号池实时监控")
diagTable = AccountDiagnosticTable("chatgpt")`;

const DEFAULT_CHAT_MODELS = [
  "gpt-5-6",
  "gpt-5-3",
  "gpt-5-mini",
  "gpt-4o",
  "gpt-4o-mini",
];

const STARTER_PROMPTS = [
  {
    icon: BarChart3,
    label: "号池健康诊断报告",
    prompt:
      "请使用 OpenUI Lang 生成一份包含 CardHeader、PoolOverviewCard、AccountDiagnosticTable 与维护建议 Callout 的当前号池诊断仪表盘。",
  },
  {
    icon: ImagePlus,
    label: "唤起 GPT-Image 生图卡",
    prompt:
      "请用 OpenUI Lang 生成一个包含 CardHeader 与 ImageStudioCard（提示词：Minimalist architectural concrete pavilion in morning mist, natural sunlight，模型：gpt-image-2.5-sunburst）的交互式生图卡片。",
  },
  {
    icon: Layers,
    label: "多供应商 Failover 矩阵",
    prompt:
      "请使用 OpenUI Lang 的 Tabs、Table 与 Callout 组件生成一份多供应商（ChatGPT Web、Claude、Gemini）能力对比矩阵与高可用 Failover 策略说明。",
  },
];

function createDefaultMessages(): StoredChatMessage[] {
  return [
    {
      id: "welcome-1",
      role: "assistant",
      content: INITIAL_OPENUI_PROGRAM,
      openuiMode: true,
      isStreaming: false,
      showSource: false,
    },
  ];
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
  const [openuiMode, setOpenuiMode] = useState(true);
  const [apiKey, setApiKey] = useState("");
  const [isSending, setIsSending] = useState(false);
  const [showSystemPromptModal, setShowSystemPromptModal] = useState(false);
  const [showQuickMenu, setShowQuickMenu] = useState(false);
  const [copiedPrompt, setCopiedPrompt] = useState(false);

  const streamEndRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    let alive = true;
    loadStoredChat("default").then((saved) => {
      if (!alive) return;
      if (saved && saved.length > 0) {
        setMessages(saved);
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

  const sendPrompt = async (promptText: string) => {
    const trimmed = promptText.trim();
    if (!trimmed || isSending) return;

    setShowQuickMenu(false);
    const userMsg: StoredChatMessage = {
      id: `user-${Date.now()}`,
      role: "user",
      content: trimmed,
      openuiMode,
      timestamp: Date.now(),
    };
    const assistantId = `assistant-${Date.now() + 1}`;
    const assistantPlaceholder: StoredChatMessage = {
      id: assistantId,
      role: "assistant",
      content: openuiMode
        ? `root = Card([header])\nheader = CardHeader("正在生成交互式界面...", "目标模型: ${model}")`
        : "正在思考...",
      openuiMode,
      isStreaming: true,
      showSource: false,
      timestamp: Date.now(),
    };

    setMessages((prev) => [...prev, userMsg, assistantPlaceholder]);
    setInput("");
    setIsSending(true);

    try {
      const apiMessages: Array<{ role: string; content: string }> = [];
      if (openuiMode) {
        apiMessages.push({
          role: "system",
          content: getOpenUISystemPrompt(),
        });
      }
      apiMessages.push({
        role: "user",
        content: trimmed,
      });

      await streamChatCompletion({
        messages: apiMessages,
        model,
        openuiMode,
        apiKey: apiKey.trim() || undefined,
        onDelta: (_delta, fullText) => {
          const cleaned = fullText
            .replace(/^```(?:openui-lang|openui)?\s*\n?/i, "")
            .replace(/\n?```\s*$/i, "");
          setMessages((prev) =>
            prev.map((m) =>
              m.id === assistantId
                ? { ...m, content: cleaned, isStreaming: true }
                : m
            )
          );
        },
      });

      setMessages((prev) =>
        prev.map((m) =>
          m.id === assistantId ? { ...m, isStreaming: false } : m
        )
      );
    } catch (err: any) {
      const errMsg = err?.message || "请求失败";
      setMessages((prev) =>
        prev.map((m) =>
          m.id === assistantId
            ? {
                ...m,
                isStreaming: false,
                content: openuiMode
                  ? `root = Card([header, errCallout, fallbackPool])\nheader = CardHeader("网关调用异常", "${model}")\nerrCallout = Callout("warning", "流式接口响应提示", ${JSON.stringify(
                      errMsg
                    )})\nfallbackPool = PoolOverviewCard("chatgpt", "本地号池实时监控")`
                  : `请求异常: ${errMsg}`,
              }
            : m
        )
      );
    } finally {
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
      prev.map((m) => (m.id === id ? { ...m, content: newCode } : m))
    );
  };

  const handleReset = async () => {
    if (isSending) return;
    await clearStoredChat("default");
    setMessages(createDefaultMessages());
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

  return (
    <div className="flex-1 flex flex-col min-h-0 h-full overflow-hidden">
      {/* Control Strip */}
      <div className="px-4 py-2.5 z-surface border-b z-border flex flex-wrap items-center justify-between gap-2 shrink-0 text-xs">
        <div className="flex flex-wrap items-center gap-2">
          <div className="p-surface p-0.5 rounded-xl inline-flex gap-0.5">
            <button
              type="button"
              onClick={() => setOpenuiMode(true)}
              className={`inline-flex items-center gap-1 px-2.5 py-1 rounded-lg cursor-pointer transition-colors ${
                openuiMode
                  ? "z-primary-btn font-medium"
                  : "z-text-muted z-ghost-btn"
              }`}
            >
              <Sparkles size={12} />
              <span>OpenUI 交互卡片</span>
            </button>
            <button
              type="button"
              onClick={() => setOpenuiMode(false)}
              className={`px-2.5 py-1 rounded-lg cursor-pointer transition-colors ${
                !openuiMode
                  ? "z-primary-btn font-medium"
                  : "z-text-muted z-ghost-btn"
              }`}
            >
              标准 Markdown
            </button>
          </div>

          <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full border z-pill-success">
            <span className="status-dot-green" />
            <span>
              {idbSynced ? "IndexedDB 会话记忆已同步" : "本地会话初始化中"}
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
            <span>System Prompt</span>
          </button>

          <button
            type="button"
            onClick={handleReset}
            disabled={isSending}
            className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn cursor-pointer"
          >
            <RotateCcw size={12} />
            <span>重置对话</span>
          </button>
        </div>
      </div>

      {/* Strictly Scoped OpenUI Message Stream */}
      <ThemeProvider mode={themeMode}>
        <div className="openui-scope flex-1 overflow-y-auto px-4 py-5 min-h-0">
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

            {messages.map((msg) => (
              <div
                key={msg.id}
                className={
                  msg.role === "user" ? "flex justify-end" : "w-full"
                }
              >
                {msg.role === "user" ? (
                  <div className="px-4 py-2.5 rounded-2xl z-primary-btn max-w-xl leading-relaxed">
                    {msg.content}
                  </div>
                ) : (
                  <div className="p-card rounded-2xl p-4 space-y-3">
                    <div className="flex flex-wrap items-center justify-between gap-2 pb-2.5 border-b z-border">
                      <div className="flex items-center gap-2">
                        <span
                          className={
                            msg.isStreaming
                              ? "status-dot-amber animate-pulse"
                              : "status-dot-green"
                          }
                        />
                        <span className="font-medium z-text-sec">
                          {msg.openuiMode
                            ? msg.isStreaming
                              ? "Thesys OpenUI · 流式渲染中"
                              : "Thesys OpenUI <Renderer /> 局部作用域"
                            : "Markdown 文本响应"}
                        </span>
                      </div>

                      {msg.openuiMode && (
                        <button
                          type="button"
                          onClick={() => toggleSource(msg.id)}
                          className="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg border z-border z-surface z-ghost-btn cursor-pointer"
                        >
                          <Code2 size={12} />
                          <span>
                            {msg.showSource ? "收起声明式源码" : "查看 / 编辑源码"}
                          </span>
                        </button>
                      )}
                    </div>

                    {msg.showSource && msg.openuiMode && (
                      <div className="space-y-1.5">
                        <div className="z-text-muted">
                          OpenUI Lang 声明式源码（实时编辑可立即刷新下方组件树）
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

                    <div className="min-w-0 overflow-x-auto">
                      {msg.openuiMode ? (
                        <Renderer
                          library={aiWebFreeLibrary}
                          response={msg.content}
                          isStreaming={Boolean(msg.isStreaming)}
                          toolProvider={aiWebFreeToolProvider}
                          onAction={(evt) => {
                            const actionMsg = (
                              evt as { humanFriendlyMessage?: string }
                            )?.humanFriendlyMessage;
                            if (actionMsg) {
                              sendPrompt(actionMsg);
                            }
                          }}
                        />
                      ) : (
                        <pre className="whitespace-pre-wrap font-sans text-xs leading-relaxed z-text-sec">
                          {msg.content}
                        </pre>
                      )}
                    </div>
                  </div>
                )}
              </div>
            ))}
            <div ref={streamEndRef} />
          </div>
        </div>
      </ThemeProvider>

      {/* Floating Glass Composer */}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          sendPrompt(input);
        }}
        className="px-4 py-3 z-canvas border-t z-border shrink-0"
      >
        <div className="max-w-3xl mx-auto relative">
          {showQuickMenu && (
            <div className="absolute bottom-full left-0 mb-2 w-full sm:w-80 p-surface rounded-2xl p-2 shadow-xl space-y-1 z-20 text-xs">
              <div className="px-2 py-1 z-text-muted font-medium">
                快捷 OpenUI / 诊断指令
              </div>
              {STARTER_PROMPTS.map((item) => (
                <button
                  key={item.label}
                  type="button"
                  onClick={() => sendPrompt(item.prompt)}
                  className="w-full flex items-center justify-between gap-2 px-2.5 py-2 rounded-xl z-row-hover text-left cursor-pointer"
                >
                  <span>{item.label}</span>
                  <span className="font-mono-num z-text-muted">发送 →</span>
                </button>
              ))}
            </div>
          )}

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
                  sendPrompt(input);
                } else if (e.key === "Escape") {
                  setShowQuickMenu(false);
                }
              }}
              placeholder={
                openuiMode
                  ? "输入对话指令或 OpenUI 卡片生成需求..."
                  : "输入对话内容..."
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
                  / 快捷指令
                </button>
                <span className="hidden sm:inline">
                  Enter 发送 · Shift+Enter 换行
                </span>
              </div>
              <button
                type="submit"
                disabled={isSending || !input.trim()}
                className="inline-flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl z-primary-btn font-medium cursor-pointer"
              >
                {isSending ? (
                  <Loader2 size={13} className="animate-spin" />
                ) : (
                  <SendHorizontal size={13} />
                )}
                <span>{isSending ? "生成中" : "发送请求"}</span>
              </button>
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
                  时将自动前置注入。
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
