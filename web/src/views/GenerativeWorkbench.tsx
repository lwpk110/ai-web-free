import React, { useState } from "react";
import { Renderer } from "@openuidev/react-lang";
import {
  aiWebFreeLibrary,
  aiWebFreeToolProvider,
  getOpenUISystemPrompt,
} from "../lib/openui-library";
import { streamChatCompletion } from "../api/client";

interface ChatMessage {
  id: string;
  role: "user" | "assistant";
  content: string;
  openuiMode: boolean;
  isStreaming?: boolean;
  showSource?: boolean;
}

const INITIAL_OPENUI_PROGRAM = `root = Card([header, callout, poolCard, chart, diagTable])
header = CardHeader("✨ 欢迎使用 Thesys OpenUI Generative Workbench", "基于 @openuidev/react-lang 与 @openuidev/react-ui 构建 · 流式生成交互式界面")
callout = Callout("info", "OpenUI Lang 引擎已就绪", "在下方输入自然语言指令，模型（gpt-5-6 / gpt-6）将直接流式输出 OpenUI Lang 声明式代码并由 <Renderer /> 实时渲染为图表、表单、号池大盘或生图组件。")
poolCard = PoolOverviewCard("chatgpt", "ChatGPT 免费号池实时健康监控")
chart = BarChart(["gpt-5-6", "gpt-6", "gpt-image-2.5-sunburst", "gpt-image-2.5-flare"], [Series("模型并发权重", [100, 85, 70, 95])])
diagTable = AccountDiagnosticTable("chatgpt")`;

const STARTER_PROMPTS = [
  {
    label: "📊 生成号池与模型分析仪表盘",
    prompt:
      "请使用 OpenUI Lang 生成一个包含 CardHeader、PoolOverviewCard、对比各模型性能的 BarChart 以及 AccountDiagnosticTable 的完整网关监控仪表盘。",
  },
  {
    label: "🎨 呼出 Image 2.5 生图工作卡",
    prompt:
      "请用 OpenUI Lang 生成一个包含 CardHeader、Callout 说明以及 ImageStudioCard（提示词：赛博朋克风格的未来东京霓虹雨夜，高细节电影感，模型：gpt-image-2.5-sunburst）的交互卡片。",
  },
  {
    label: "📝 生成动态配置表单与 Tabs",
    prompt:
      "请使用 OpenUI Lang 的 Tabs、Table、Callout 组件生成一份多供应商（ChatGPT Web、Claude、Gemini、DeepSeek）能力对比矩阵与网关巡检建议。",
  },
];

export const GenerativeWorkbench: React.FC = () => {
  const [messages, setMessages] = useState<ChatMessage[]>([
    {
      id: "welcome-1",
      role: "assistant",
      content: INITIAL_OPENUI_PROGRAM,
      openuiMode: true,
      isStreaming: false,
      showSource: false,
    },
  ]);
  const [input, setInput] = useState("");
  const [model, setModel] = useState("gpt-5-6");
  const [openuiMode, setOpenuiMode] = useState(true);
  const [apiKey, setApiKey] = useState("");
  const [isSending, setIsSending] = useState(false);
  const [showSystemPromptModal, setShowSystemPromptModal] = useState(false);

  const sendPrompt = async (promptText: string) => {
    const trimmed = promptText.trim();
    if (!trimmed || isSending) return;

    const userMsg: ChatMessage = {
      id: `user-${Date.now()}`,
      role: "user",
      content: trimmed,
      openuiMode,
    };
    const assistantId = `assistant-${Date.now() + 1}`;
    const assistantPlaceholder: ChatMessage = {
      id: assistantId,
      role: "assistant",
      content: openuiMode
        ? `root = Card([header])\nheader = CardHeader("正在流式生成 OpenUI 界面...", "模型: ${model}")`
        : "思考中...",
      openuiMode,
      isStreaming: true,
      showSource: false,
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

      let startedRealStream = false;
      await streamChatCompletion({
        messages: apiMessages,
        model,
        openuiMode,
        apiKey: apiKey.trim() || undefined,
        onDelta: (_delta, fullText) => {
          // Strip optional ```openui-lang or ``` code fences if the model wrapped output
          const cleaned = fullText
            .replace(/^```(?:openui-lang|openui)?\s*\n?/i, "")
            .replace(/\n?```\s*$/i, "");
          startedRealStream = true;
          setMessages((prev) =>
            prev.map((m) =>
              m.id === assistantId
                ? { ...m, content: cleaned, isStreaming: true }
                : m
            )
          );
        },
      });

      if (!startedRealStream) {
        setMessages((prev) =>
          prev.map((m) =>
            m.id === assistantId ? { ...m, isStreaming: false } : m
          )
        );
      } else {
        setMessages((prev) =>
          prev.map((m) =>
            m.id === assistantId ? { ...m, isStreaming: false } : m
          )
        );
      }
    } catch (err: any) {
      const errMsg = err?.message || "请求失败";
      setMessages((prev) =>
        prev.map((m) =>
          m.id === assistantId
            ? {
                ...m,
                isStreaming: false,
                content: openuiMode
                  ? `root = Card([header, errCallout, fallbackPool])\nheader = CardHeader("调用提示", "${model}")\nerrCallout = Callout("warning", "流式接口返回提示", ${JSON.stringify(
                      errMsg
                    )})\nfallbackPool = PoolOverviewCard("chatgpt", "本地号池实时状态（离线可用）")`
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

  return (
    <div className="workbench-layout">
      {/* 顶部工具栏 */}
      <div className="workbench-toolbar">
        <div className="toolbar-left">
          <span className="badge-openui">Thesys OpenUI v1.1</span>
          <label className="toolbar-field">
            <span>模型:</span>
            <select
              className="console-select"
              value={model}
              onChange={(e) => setModel(e.target.value)}
            >
              <option value="gpt-5-6">gpt-5-6 (Web 主力)</option>
              <option value="gpt-6">gpt-6 (旗舰)</option>
              <option value="gpt-5-6-mini">gpt-5-6-mini (极速)</option>
              <option value="auto">auto (自动路由)</option>
            </select>
          </label>
          <label className="toolbar-toggle">
            <input
              type="checkbox"
              checked={openuiMode}
              onChange={(e) => setOpenuiMode(e.target.checked)}
            />
            <span>
              {openuiMode
                ? "🟢 OpenUI Lang 生成式 UI 模式"
                : "⚪ 普通 Markdown 文本模式"}
            </span>
          </label>
        </div>

        <div className="toolbar-right">
          <input
            type="password"
            className="console-input input-sm"
            placeholder="可选 API Key (Authorization)"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
          />
          <button
            className="btn-secondary btn-sm"
            onClick={() => setShowSystemPromptModal(true)}
          >
            📜 查看 library.prompt()
          </button>
        </div>
      </div>

      {/* 快捷生成指令模板 */}
      <div className="starter-chips">
        {STARTER_PROMPTS.map((item, idx) => (
          <button
            key={idx}
            className="starter-chip"
            onClick={() => sendPrompt(item.prompt)}
            disabled={isSending}
          >
            {item.label}
          </button>
        ))}
      </div>

      {/* 消息与 OpenUI <Renderer /> 流式画布 */}
      <div className="workbench-stream">
        {messages.map((msg) => (
          <div key={msg.id} className={`message-row ${msg.role}`}>
            {msg.role === "user" ? (
              <div className="user-bubble">{msg.content}</div>
            ) : (
              <div className="assistant-card-shell">
                <div className="assistant-card-topbar">
                  <span className="assistant-tag">
                    {msg.openuiMode
                      ? "⚡ OpenUI <Renderer /> Live Output"
                      : "💬 Markdown Output"}
                  </span>
                  {msg.openuiMode && (
                    <button
                      className="btn-secondary btn-xs"
                      onClick={() => toggleSource(msg.id)}
                    >
                      {msg.showSource
                        ? "隐藏 openui-lang 源码"
                        : "</> 检视 / 实时编辑 openui-lang"}
                    </button>
                  )}
                </div>

                {msg.showSource && msg.openuiMode && (
                  <div className="openui-source-editor">
                    <div className="source-editor-label">
                      实时修改下方 OpenUI Lang 声明式语句，右侧/下方{" "}
                      <code>&lt;Renderer /&gt;</code> 会立即热更新组件树：
                    </div>
                    <textarea
                      value={msg.content}
                      onChange={(e) => updateSourceLive(msg.id, e.target.value)}
                      rows={7}
                    />
                  </div>
                )}

                <div className="openui-renderer-surface">
                  {msg.openuiMode ? (
                    <Renderer
                      library={aiWebFreeLibrary}
                      response={msg.content}
                      isStreaming={Boolean(msg.isStreaming)}
                      toolProvider={aiWebFreeToolProvider}
                      onAction={(evt) => {
                        if (evt?.userMessage) {
                          sendPrompt(evt.userMessage);
                        }
                      }}
                    />
                  ) : (
                    <pre className="plain-markdown-box">{msg.content}</pre>
                  )}
                </div>
              </div>
            )}
          </div>
        ))}
      </div>

      {/* 底部输入框 */}
      <form
        className="workbench-composer"
        onSubmit={(e) => {
          e.preventDefault();
          sendPrompt(input);
        }}
      >
        <input
          className="composer-input"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder={
            openuiMode
              ? "输入需求，让大模型用 OpenUI Lang 流式构建交互式图表、表单、号池大盘或生图卡片..."
              : "输入普通对话内容..."
          }
          disabled={isSending}
        />
        <button
          type="submit"
          className="btn-primary"
          disabled={isSending || !input.trim()}
        >
          {isSending ? "生成中..." : "发送 (Stream UI)"}
        </button>
      </form>

      {/* 查看由 aiWebFreeLibrary.prompt() 自动生成的 System Prompt 弹窗 */}
      {showSystemPromptModal && (
        <div
          className="modal-backdrop"
          onClick={() => setShowSystemPromptModal(false)}
        >
          <div
            className="modal-panel"
            onClick={(e) => e.stopPropagation()}
            style={{ maxWidth: 820 }}
          >
            <div className="modal-header">
              <h3>
                由 <code>aiWebFreeLibrary.prompt()</code> 生成的 OpenUI Lang
                System Prompt
              </h3>
              <button
                className="btn-secondary btn-xs"
                onClick={() => setShowSystemPromptModal(false)}
              >
                关闭
              </button>
            </div>
            <pre className="system-prompt-preview">
              {getOpenUISystemPrompt()}
            </pre>
          </div>
        </div>
      )}
    </div>
  );
};
