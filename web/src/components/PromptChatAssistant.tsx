import React, { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import {
  Check,
  Loader2,
  MessageCircle,
  RotateCcw,
  Send,
  Square,
  X,
} from "lucide-react";
import { streamChatCompletion } from "../api/client";
import { ensureModelsCatalog, useModelsCatalog } from "../lib/models";

interface PromptChatMessage {
  role: "user" | "assistant";
  content: string;
}

interface PromptChatRequest {
  studio: string;
  targetLabel: string;
  targetKey: string;
  value: string;
  onApply: (value: string) => void;
  context?: string;
}

const PROMPT_CHAT_EVENT = "ai-web-free:open-prompt-chat";

export function PromptChatAssistant({
  studio,
  targetLabel,
  targetKey,
  value,
  onApply,
  context,
}: PromptChatRequest): React.JSX.Element {
  const handleOpen = () => {
    window.dispatchEvent(
      new CustomEvent<PromptChatRequest>(PROMPT_CHAT_EVENT, {
        detail: { studio, targetLabel, targetKey, value, onApply, context },
      })
    );
  };

  return (
    <button
      type="button"
      onClick={handleOpen}
      title={`和 AI 对话润色${targetLabel}`}
      aria-label={`和 AI 对话润色${targetLabel}`}
      className="inline-flex shrink-0 items-center gap-1 rounded-md border z-border px-2 py-1 text-[11px] font-medium z-text-muted hover:bg-muted hover:z-text"
    >
      <MessageCircle size={13} />
      <span>AI 对话</span>
    </button>
  );
}

export function PromptChatAssistantHost(): React.JSX.Element | null {
  const { chatModels } = useModelsCatalog({ autoLoad: false });
  const [active, setActive] = useState<PromptChatRequest | null>(null);
  const [messages, setMessages] = useState<PromptChatMessage[]>([]);
  const [draft, setDraft] = useState("");
  const [model, setModel] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  const abortRef = useRef<AbortController | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const targetKeyRef = useRef("");

  useEffect(() => {
    const onOpen = (event: Event) => {
      const request = (event as CustomEvent<PromptChatRequest>).detail;
      if (!request) return;
      if (targetKeyRef.current !== request.targetKey) {
        abortRef.current?.abort();
        abortRef.current = null;
        setMessages([]);
        setDraft("");
        setError("");
        setSending(false);
        targetKeyRef.current = request.targetKey;
      }
      setActive(request);
    };
    window.addEventListener(PROMPT_CHAT_EVENT, onOpen);
    return () => window.removeEventListener(PROMPT_CHAT_EVENT, onOpen);
  }, []);

  useEffect(() => {
    if (chatModels.length > 0 && !chatModels.some((item) => item.id === model)) {
      setModel(chatModels[0].id);
    }
  }, [chatModels, model]);

  useEffect(() => {
    if (active) void ensureModelsCatalog();
  }, [active]);

  useEffect(() => {
    if (!active) return;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setActive(null);
    };
    window.addEventListener("keydown", onKeyDown);
    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener("keydown", onKeyDown);
    };
  }, [active]);

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight });
  }, [messages]);

  useEffect(() => () => abortRef.current?.abort(), []);

  const clearChat = () => {
    abortRef.current?.abort();
    abortRef.current = null;
    setSending(false);
    setMessages([]);
    setDraft("");
    setError("");
  };

  const sendMessage = async (event: React.FormEvent) => {
    event.preventDefault();
    const text = draft.trim();
    if (!text || sending || !active) return;

    const assistantIndex = messages.length + 1;
    const request = active;
    const nextMessages: PromptChatMessage[] = [
      ...messages,
      { role: "user", content: text },
      { role: "assistant", content: "" },
    ];
    const controller = new AbortController();
    abortRef.current = controller;
    setMessages(nextMessages);
    setDraft("");
    setError("");
    setSending(true);

    try {
      await streamChatCompletion({
        model: model || chatModels[0]?.id || "gpt-5-mini",
        openuiMode: false,
        signal: controller.signal,
        messages: [
          {
            role: "system",
            content: [
              `你是${request.studio}中的提示词与文案润色助手。`,
              "根据用户的修改要求优化当前文本，保留原意与关键约束；用户未指定语言时沿用原文语言。",
              "优先给出可直接使用的完整成稿，不要擅自开始图片、视频或语音生成。需要澄清时先简短提问。",
              `当前输入内容：\n${request.value.trim() || "（空）"}`,
              request.context ? `工坊参数：\n${request.context}` : "",
            ].filter(Boolean).join("\n\n"),
          },
          ...messages,
          { role: "user", content: text },
        ],
        onDelta: (_delta, fullText) => {
          setMessages((current) =>
            current.map((message, index) =>
              index === assistantIndex ? { ...message, content: fullText } : message
            )
          );
        },
      });
    } catch (err) {
      if (!controller.signal.aborted) {
        setError(err instanceof Error ? err.message : "聊天请求失败");
      }
    } finally {
      if (abortRef.current === controller) {
        abortRef.current = null;
        setSending(false);
      }
    }
  };

  if (!active || typeof document === "undefined") return null;

  const latestAnswer = [...messages]
    .reverse()
    .find((message) => message.role === "assistant" && message.content.trim());

  return createPortal(
    <div className="fixed inset-0 z-[100] flex justify-end" role="presentation">
      <button
        type="button"
        aria-label="关闭提示词对话"
        onClick={() => setActive(null)}
        className="absolute inset-0 bg-black/25"
      />
      <aside
        role="dialog"
        aria-modal="true"
        aria-label={`${active.studio}提示词助手`}
        className="relative flex h-full w-full max-w-[440px] flex-col border-l z-border z-surface shadow-2xl"
        style={{ backgroundColor: "var(--bg-surface)" }}
      >
        <header className="z-surface flex shrink-0 items-center justify-between gap-3 border-b z-border px-4 py-3">
          <div className="flex min-w-0 flex-1 items-center gap-2.5">
            <span className="grid size-8 shrink-0 place-items-center rounded-md bg-primary/10 text-primary">
              <MessageCircle size={16} />
            </span>
            <div className="min-w-0">
              <h2 className="truncate text-sm font-semibold z-text">提示词助手</h2>
              <p className="truncate text-[11px] z-text-muted">{active.studio} · {active.targetLabel}</p>
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            <select
              aria-label="对话模型"
              value={model || chatModels[0]?.id || "gpt-5-mini"}
              onChange={(event) => setModel(event.target.value)}
              className="w-28 max-w-32 rounded-md border z-border z-surface px-2 py-1.5 text-[11px] z-text"
            >
              {chatModels.length > 0 ? chatModels.map((item) => (
                <option key={item.id} value={item.id}>{item.id}</option>
              )) : <option value="gpt-5-mini">gpt-5-mini</option>}
            </select>
            <button
              type="button"
              onClick={clearChat}
              disabled={sending}
              title="清空对话"
              aria-label="清空对话"
              className="grid size-8 place-items-center rounded-md z-text-muted hover:bg-muted disabled:opacity-40"
            >
              <RotateCcw size={14} />
            </button>
            <button
              type="button"
              onClick={() => setActive(null)}
              title="关闭"
              aria-label="关闭"
              className="grid size-8 place-items-center rounded-md z-text-muted hover:bg-muted disabled:opacity-40"
            >
              <X size={16} />
            </button>
          </div>
        </header>

        <div className="z-surface shrink-0 border-b z-border px-4 py-3" style={{ backgroundColor: "var(--bg-muted)" }}>
          <div className="mb-1 text-[10px] font-medium uppercase text-z-text-muted">当前内容</div>
          <p className="max-h-20 overflow-y-auto whitespace-pre-wrap break-words text-xs leading-relaxed z-text-muted">
            {active.value.trim() || "尚未填写内容，可直接描述你想创作的内容。"}
          </p>
        </div>

        <div ref={scrollRef} className="min-h-0 flex-1 space-y-3 overflow-y-auto p-4">
          {messages.length === 0 ? (
            <p className="max-w-[85%] rounded-lg border z-border z-surface px-3 py-2.5 text-xs leading-relaxed z-text-muted">
              可以要求我调整风格、补充细节或翻译成另一种语言。润色结果会由你决定是否回填。
            </p>
          ) : messages.map((message, index) => (
            <div key={`${index}-${message.role}`} className={`flex ${message.role === "user" ? "justify-end" : "justify-start"}`}>
              <div className={`max-w-[92%] whitespace-pre-wrap break-words rounded-lg px-3 py-2.5 text-xs leading-relaxed ${message.role === "user" ? "bg-primary text-primary-foreground" : "border z-border z-surface z-text"}`}>
                {message.content || (sending && message.role === "assistant" ? <Loader2 size={14} className="animate-spin" /> : "")}
              </div>
            </div>
          ))}
          {latestAnswer && !sending && (
            <div className="flex justify-start">
              <button
                type="button"
                onClick={() => {
                  active.onApply(latestAnswer.content);
                  setActive((current) => current ? { ...current, value: latestAnswer.content } : current);
                }}
                className="inline-flex items-center gap-1.5 rounded-md border z-border px-2.5 py-1.5 text-[11px] font-medium z-text hover:bg-muted"
              >
                <Check size={13} />
                替换当前内容
              </button>
            </div>
          )}
          {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
        </div>

        <form onSubmit={sendMessage} className="flex shrink-0 items-end gap-2 border-t z-border p-3">
          <textarea
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                void sendMessage(event as unknown as React.FormEvent);
              }
            }}
            rows={3}
            aria-label="输入润色要求"
            placeholder="输入修改要求..."
            className="max-h-36 min-h-16 flex-1 resize-y rounded-md border z-border p-input px-3 py-2.5 text-xs"
          />
          {sending ? (
            <button
              type="button"
              onClick={() => abortRef.current?.abort()}
              title="停止生成"
              aria-label="停止生成"
              className="grid size-10 shrink-0 place-items-center rounded-md bg-destructive text-destructive-foreground"
            >
              <Square size={14} />
            </button>
          ) : (
            <button
              type="submit"
              disabled={!draft.trim()}
              title="发送"
              aria-label="发送"
              className="grid size-10 shrink-0 place-items-center rounded-md bg-primary text-primary-foreground disabled:opacity-40"
            >
              <Send size={15} />
            </button>
          )}
        </form>
      </aside>
    </div>,
    document.body
  );
}
