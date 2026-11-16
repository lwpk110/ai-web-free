import React, { useEffect, useState } from "react";
import { KeyRound, Loader2, X } from "lucide-react";
import { importAccounts } from "../api/client";

export type CloudKeyProviderId =
  | "siliconflow"
  | "dashscope"
  | "jimeng"
  | "meta_muse"
  | "local_tts";

interface ProviderMeta {
  id: CloudKeyProviderId;
  label: string;
  envHint: string;
  credentialType: string;
  desc: string;
  inputLabel: string;
  placeholder: string;
}

const PROVIDER_META: Record<CloudKeyProviderId, ProviderMeta> = {
  siliconflow: {
    id: "siliconflow",
    label: "硅基流动",
    envHint: "SILICONFLOW_API_KEY",
    credentialType: "api_key",
    desc: "配置 SILICONFLOW_API_KEY，解锁云端 CosyVoice2-0.5B 配音与本地采集音色云端复刻、FLUX/Kolors 文生图、CogVideoX/Wan2.1 视频生成（零本地硬件占用）。",
    inputLabel: "API Key（支持多行轮询）",
    placeholder: "sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  },
  dashscope: {
    id: "dashscope",
    label: "阿里云百炼",
    envHint: "DASHSCOPE_API_KEY",
    credentialType: "api_key",
    desc: "配置 DASHSCOPE_API_KEY（支持 sk-... 或 sk-...#workspaceId），解锁云端 CosyVoice v1/v2/v3 配音/捏声音/复刻，以及通义万相 Wanx 生图与万相视频。",
    inputLabel: "API Key（支持多行轮询）",
    placeholder: "sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  },
  jimeng: {
    id: "jimeng",
    label: "字节即梦",
    envHint: "JIMENG_SESSION_ID",
    credentialType: "session_id",
    desc: "配置字节即梦（Dreamina / 即梦 Web）SessionID Cookie，解锁即梦 2.1 高美学云端生图与即梦 2.0 电影级云端视频。",
    inputLabel: "SessionID Cookie（每行一个）",
    placeholder: "粘贴浏览器 Cookie 中的 sessionid 值",
  },
  meta_muse: {
    id: "meta_muse",
    label: "Meta Muse",
    envHint: "META_COOKIE",
    credentialType: "cookie",
    desc: "配置 Meta AI 会话 Cookie，解锁云端 Muse 高清文生视频与图生视频驱动（无需本地 GPU）。",
    inputLabel: "Meta AI Cookie",
    placeholder: "datr=...; c_user=...; xs=...",
  },
  local_tts: {
    id: "local_tts",
    label: "OpenAI 兼容云端 TTS",
    envHint: "CUSTOM_TTS_BASE_URL",
    credentialType: "endpoint",
    desc: "配置任意外部/中继 OpenAI 兼容 /v1/audio/speech 云端端点（格式：BaseURL|APIKey|Model，例如 https://api.openai.com/v1|sk-...|tts-1）。",
    inputLabel: "端点|密钥|模型（竖线分隔）",
    placeholder: "https://api.openai.com/v1|sk-...|tts-1",
  },
};

interface CloudKeyConfigModalProps {
  open: boolean;
  onClose: () => void;
  initialProvider?: CloudKeyProviderId;
  allowedProviders?: CloudKeyProviderId[];
  title?: string;
  onSaved?: (provider: CloudKeyProviderId, message: string) => void;
}

export function CloudKeyConfigModal({
  open,
  onClose,
  initialProvider = "siliconflow",
  allowedProviders = ["siliconflow", "dashscope", "jimeng", "meta_muse", "local_tts"],
  title = "配置云端模型 API Key（纯云端免本地部署）",
  onSaved,
}: CloudKeyConfigModalProps): React.JSX.Element | null {
  const safeInitial = allowedProviders.includes(initialProvider)
    ? initialProvider
    : allowedProviders[0] || "siliconflow";

  const [provider, setProvider] = useState<CloudKeyProviderId>(safeInitial);
  const [rawInput, setRawInput] = useState("");
  const [saving, setSaving] = useState(false);
  const [feedback, setFeedback] = useState<{ ok: boolean; text: string } | null>(null);

  useEffect(() => {
    if (open) {
      setProvider(safeInitial);
      setRawInput("");
      setFeedback(null);
    }
  }, [open, safeInitial]);

  if (!open) return null;

  const meta = PROVIDER_META[provider];

  const handleSave = async () => {
    const trimmed = rawInput.trim();
    if (!trimmed) return;
    setSaving(true);
    setFeedback(null);
    try {
      const res = await importAccounts({
        provider: meta.id,
        credentialType: meta.credentialType,
        rawText: trimmed,
        persist: false,
      });
      const msg =
        res.result?.messages?.[0] ||
        `已成功注入 ${meta.label} 云端凭据并激活号池`;
      const fullMsg = `${msg}（提示：如需服务重启后自动生效，请同步写入 .env 的 ${meta.envHint}）`;
      setFeedback({ ok: true, text: fullMsg });
      onSaved?.(meta.id, fullMsg);
    } catch (e) {
      setFeedback({
        ok: false,
        text: e instanceof Error ? e.message : "保存失败，请检查凭据格式",
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 bg-black/50 flex items-center justify-center p-4">
      <div className="p-card rounded-2xl border z-border max-w-lg w-full max-h-[90vh] overflow-y-auto p-5 space-y-4 shadow-xl">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <KeyRound className="w-4 h-4 text-primary" />
            <h3 className="text-sm font-semibold z-text">{title}</h3>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1 rounded hover:bg-muted z-text-muted"
            aria-label="关闭弹窗"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="flex flex-wrap gap-1.5">
          {allowedProviders.map((id) => {
            const m = PROVIDER_META[id];
            return (
              <button
                key={id}
                type="button"
                onClick={() => {
                  setProvider(id);
                  setRawInput("");
                  setFeedback(null);
                }}
                className={`px-2.5 py-1.5 text-xs rounded-md font-medium border transition-colors ${
                  provider === id
                    ? "bg-primary/10 text-primary border-primary/30"
                    : "z-border z-text-muted hover:z-text"
                }`}
              >
                {m.label}
              </button>
            );
          })}
        </div>

        <p className="text-xs z-text-muted leading-relaxed">{meta.desc}</p>

        <div className="space-y-1.5">
          <label className="text-xs font-medium z-text block">{meta.inputLabel}</label>
          <textarea
            value={rawInput}
            onChange={(e) => setRawInput(e.target.value)}
            rows={3}
            placeholder={meta.placeholder}
            className="w-full px-3 py-2 text-xs font-mono rounded-lg border z-border bg-background z-text focus:outline-none focus:ring-1 focus:ring-primary resize-none"
          />
        </div>

        {feedback && (
          <div
            className={`px-3 py-2 text-xs rounded-lg border ${
              feedback.ok
                ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20"
                : "bg-destructive/10 text-destructive border-destructive/20"
            }`}
          >
            {feedback.text}
          </div>
        )}

        <div className="flex items-center justify-end gap-2 pt-1">
          <button
            type="button"
            onClick={onClose}
            className="px-3 py-1.5 text-xs rounded-md border z-border hover:bg-muted z-text-muted"
          >
            关闭
          </button>
          <button
            type="button"
            onClick={() => void handleSave()}
            disabled={saving || !rawInput.trim()}
            className="px-4 py-1.5 text-xs font-medium rounded-md z-primary-btn disabled:opacity-50 inline-flex items-center gap-1.5"
          >
            {saving && <Loader2 className="w-3.5 h-3.5 animate-spin" />}
            验证并注入云端号池
          </button>
        </div>
      </div>
    </div>
  );
}
