import { AlertCircle, CheckCircle2, KeyRound, Settings2, Sliders, X } from "lucide-react";
import React, { useEffect, useState } from "react";
import type { DramaCapabilities, VoiceDescriptor } from "../../api/client";
import type { CharacterNodeData, ScriptNodeData } from "../../lib/dramaPresets";

interface DramaSettingsModalProps {
  open: boolean;
  onClose: () => void;
  capabilities: DramaCapabilities | null;
  scriptData: ScriptNodeData | undefined;
  voices: VoiceDescriptor[];
  onSave: (patch: Partial<ScriptNodeData>) => void;
  onOpenCloudKeyModal: () => void;
}

const IMAGE_PROVIDER_META = [
  { id: "chatgpt", label: "ChatGPT 生图", desc: "支持多参考图视觉锁脸与高一致性绘本" },
  { id: "siliconflow", label: "硅基流动 FLUX 生图", desc: "免费/低成本极速文生图（需 SILICONFLOW_API_KEY）" },
  { id: "dashscope", label: "阿里云百炼 (Wanx-v1)", desc: "通义万相文生图（需 DASHSCOPE_API_KEY）" },
  { id: "jimeng", label: "即梦生图", desc: "字节系国风/写实生图（需即梦账号）" },
  { id: "grok", label: "Grok Aurora 生图", desc: "xAI Grok 免费号池生图（需 Grok 账号）" },
];

const VIDEO_PROVIDER_META = [
  { id: "meta_muse", label: "Meta Muse (muse-video / muse-talk)", desc: "免费逆向音画协同 & 数字人口型同步" },
  { id: "siliconflow", label: "硅基流动 (CogVideoX-5b)", desc: "开源商业级图生视频（需 SILICONFLOW_API_KEY）" },
  { id: "dashscope", label: "阿里云百炼 (Wan-Video-v1)", desc: "通义万相图生视频（需 DASHSCOPE_API_KEY）" },
  { id: "jimeng", label: "即梦视频 (Jimeng-Video-2.0)", desc: "电影级运镜视频（需即梦账号）" },
];

const TTS_PROVIDER_META: Array<{
  id: string;
  engine: CharacterEngine;
  label: string;
  desc: string;
  selectable: boolean;
}> = [
  { id: "edge_tts", engine: "edge-tts", label: "微软 Edge-TTS", desc: "零配置中文神经网络音色，画布默认兜底", selectable: true },
  { id: "local_tts", engine: "local_tts", label: "本地 / OpenAI 兼容 TTS", desc: "读取 LOCAL_TTS_BASE_URL / API Key / MODEL", selectable: true },
  { id: "dashscope", engine: "dashscope", label: "阿里百炼 CosyVoice", desc: "支持音色设计与克隆（需 DASHSCOPE_API_KEY）", selectable: true },
  { id: "siliconflow", engine: "siliconflow", label: "硅基流动 CosyVoice2", desc: "云端多音色配音（需 SILICONFLOW_API_KEY）", selectable: true },
  { id: "chatgpt", engine: "chatgpt", label: "ChatGPT 官方语音", desc: "当前保留给 /v1/audio/speech，短剧后台直连暂未启用", selectable: false },
];

type CharacterEngine = CharacterNodeData["voiceEngine"];

function providerForEngine(engine: CharacterEngine) {
  return TTS_PROVIDER_META.find((p) => p.engine === engine)?.id || "edge_tts";
}

function engineForProvider(id: string): CharacterEngine {
  const found = TTS_PROVIDER_META.find((p) => p.id === id);
  return found?.engine || "edge-tts";
}

function fallbackVoice(engine: CharacterEngine, caps: DramaCapabilities | null) {
  if (engine === "edge-tts") return caps?.recommended?.free_first?.tts_voice || "zh-CN-YunxiNeural";
  return "";
}

export const DramaSettingsModal: React.FC<DramaSettingsModalProps> = ({
  open,
  onClose,
  capabilities,
  scriptData,
  voices,
  onSave,
  onOpenCloudKeyModal,
}) => {
  const [allowedImg, setAllowedImg] = useState<string[]>([]);
  const [allowedVid, setAllowedVid] = useState<string[]>([]);
  const [allowedTTS, setAllowedTTS] = useState<string[]>([]);
  const [scriptModel, setScriptModel] = useState("auto");
  const [imgOverride, setImgOverride] = useState("");
  const [vidOverride, setVidOverride] = useState("");
  const [defaultEngine, setDefaultEngine] = useState<CharacterEngine>("edge-tts");
  const [defaultVoiceId, setDefaultVoiceId] = useState("");
  const [defaultVoiceName, setDefaultVoiceName] = useState("");
  const [concurrency, setConcurrency] = useState(0);
  const [autoFallbackKB, setAutoFallbackKB] = useState(true);

  useEffect(() => {
    if (!open) return;
    const activeImg = IMAGE_PROVIDER_META.filter((m) => capabilities?.image?.providers?.[m.id]?.available).map((m) => m.id);
    const activeVid = VIDEO_PROVIDER_META.filter((m) => capabilities?.video?.providers?.[m.id]?.available).map((m) => m.id);
    const activeTTS = TTS_PROVIDER_META
      .filter((m) => m.selectable && capabilities?.tts?.providers?.[m.id]?.available)
      .map((m) => m.id);
    const engine = scriptData?.defaultTTSEngine || "edge-tts";
    setAllowedImg(scriptData?.allowedImageProviders?.length ? scriptData.allowedImageProviders : activeImg);
    setAllowedVid(scriptData?.allowedVideoProviders?.length ? scriptData.allowedVideoProviders : activeVid);
    setAllowedTTS(scriptData?.allowedTTSProviders?.length ? scriptData.allowedTTSProviders : ["edge_tts", ...activeTTS.filter((id) => id !== "edge_tts")]);
    setScriptModel(scriptData?.scriptModelOverride || "auto");
    setImgOverride(scriptData?.imageModelOverride || "auto");
    setVidOverride(scriptData?.videoModelOverride || "auto");
    setDefaultEngine(engine);
    setDefaultVoiceId(scriptData?.defaultTTSVoiceId || fallbackVoice(engine, capabilities));
    setDefaultVoiceName(scriptData?.defaultTTSVoiceName || "");
    setConcurrency(scriptData?.imageConcurrency || 0);
    setAutoFallbackKB(scriptData?.autoFallbackKenBurns ?? true);
  }, [open, capabilities, scriptData]);

  if (!open) return null;

  const toggleItem = (list: string[], setList: (v: string[]) => void, id: string) => {
    setList(list.includes(id) ? list.filter((x) => x !== id) : [...list, id]);
  };

  const voiceOptions = voices.filter((v) => providerForEngine(defaultEngine) === v.provider && v.available !== false);
  const llmProviders = Object.entries(capabilities?.llm.providers || {}).sort((a, b) => a[0].localeCompare(b[0]));
  const llmModels = Array.from(new Set(["auto", ...(capabilities?.llm.models || [])]));
  const imageProviders = Object.entries(capabilities?.image.providers || {}).sort((a, b) => a[0].localeCompare(b[0]));
  const imageModels = Array.from(new Set(["auto", ...imageProviders.flatMap((entry) => entry[1].models || [])]));
  const videoProviders = Object.entries(capabilities?.video.providers || {}).sort((a, b) => a[0].localeCompare(b[0]));
  const videoModels = Array.from(new Set(["auto", ...videoProviders.flatMap((entry) => entry[1].models || [])]));
  const selectedVoice = voiceOptions.find((v) => v.id === defaultVoiceId);

  const changeEngine = (engine: CharacterEngine) => {
    setDefaultEngine(engine);
    const provider = providerForEngine(engine);
    const nextVoice = voiceOptions.find((v) => v.provider === provider) || voices.find((v) => v.provider === provider && v.available !== false);
    const chosen = nextVoice?.id || fallbackVoice(engine, capabilities);
    setDefaultVoiceId(chosen);
    setDefaultVoiceName(nextVoice?.name || (engine === "edge-tts" ? "自动中文解说音色" : chosen));
  };

  const handleApply = () => {
    const engine = allowedTTS.length && !allowedTTS.includes(providerForEngine(defaultEngine))
      ? engineForProvider(allowedTTS.find((id) => TTS_PROVIDER_META.find((p) => p.id === id)?.selectable) || "edge_tts")
      : defaultEngine;
    onSave({
      scriptModelOverride: scriptModel === "auto" ? undefined : scriptModel,
      allowedImageProviders: allowedImg,
      allowedVideoProviders: allowedVid,
      allowedTTSProviders: allowedTTS,
      defaultTTSEngine: engine,
      defaultTTSVoiceId: defaultVoiceId.trim() || fallbackVoice(engine, capabilities),
      defaultTTSVoiceName: (selectedVoice?.name || defaultVoiceName).trim() || undefined,
      imageModelOverride: imgOverride === "auto" ? undefined : imgOverride.trim() || undefined,
      videoModelOverride: vidOverride === "auto" ? undefined : vidOverride.trim() || undefined,
      imageConcurrency: concurrency > 0 ? concurrency : undefined,
      autoFallbackKenBurns: autoFallbackKB,
    });
    onClose();
  };

  const renderProviderStatus = (st?: { available: boolean; active?: number; zero_config?: boolean; retired?: boolean }, provider = "") => {
    if (st?.retired) return <span className="text-[10px] text-[var(--text-muted)]">已下线</span>;
    if (st?.available) {
      return (
        <span className="inline-flex items-center gap-0.5 text-[10px] text-emerald-600 dark:text-emerald-400">
          <CheckCircle2 className="h-3 w-3" /> {st.active ?? 1} 可用
        </span>
      );
    }
    const reason = provider === "local_tts" ? "未配置" : provider === "chatgpt" ? "无活跃账号" : "未配置 Key";
    return (
      <span className="inline-flex items-center gap-0.5 text-[10px] text-[var(--text-muted)]">
        <AlertCircle className="h-3 w-3" /> {reason}
      </span>
    );
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/55 p-4 backdrop-blur-sm">
      <div className="flex max-h-[88vh] w-full max-w-3xl flex-col overflow-hidden rounded-2xl border border-[var(--border-default)] bg-[var(--bg-surface)] shadow-2xl">
        <div className="flex items-center justify-between border-b border-[var(--border-default)] px-5 py-3.5">
          <div className="flex items-center gap-2">
            <Settings2 className="h-4 w-4 text-indigo-500" />
            <h3 className="text-sm font-semibold">短剧能力与通道设置</h3>
          </div>
          <button type="button" onClick={onClose} className="rounded-lg p-1 text-[var(--text-muted)] hover:bg-[var(--bg-surface-hover)]">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex-1 space-y-5 overflow-y-auto p-5 text-xs">
          <section className="space-y-2.5">
            <div className="flex items-center justify-between gap-3">
              <div>
                <div className="font-semibold text-[var(--text-primary)]">剧本拆解模型</div>
                <div className="text-[11px] text-[var(--text-muted)]">
                  {capabilities?.llm.available
                    ? `当前 ${capabilities.llm.active} 个 ChatGPT 账号可执行角色、场景与分镜拆解`
                    : capabilities?.llm.reason || "暂无可用拆本模型账号"}
                </div>
              </div>
              <button
                type="button"
                onClick={() => {
                  onClose();
                  onOpenCloudKeyModal();
                }}
                className="inline-flex shrink-0 items-center gap-1 rounded-lg border border-amber-500/30 bg-amber-500/10 px-2.5 py-1 text-[11px] text-amber-700 hover:bg-amber-500/20 dark:text-amber-400"
              >
                <KeyRound className="h-3 w-3" />
                配置 API Key
              </button>
            </div>
            <label className="block space-y-1 rounded-xl border border-[var(--border-default)] bg-[var(--bg-elevated)] p-3">
              <span className="font-medium">拆本 LLM 模型（auto = 从可用模型随机）</span>
              <select
                value={scriptModel}
                onChange={(e) => setScriptModel(e.target.value)}
                className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-surface)] px-2.5 py-1.5"
              >
                <option value="auto">auto · 从可用文本模型随机</option>
                {llmProviders.length > 0 ? (
                  llmProviders.map((entry) => (
                    <optgroup key={entry[0]} label={entry[1].name || entry[0]}>
                      {(entry[1].models || []).map((model) => (
                        <option key={model} value={model} disabled={!entry[1].available}>{model}</option>
                      ))}
                    </optgroup>
                  ))
                ) : (
                  llmModels.filter((model) => model !== "auto").map((model) => (
                    <option key={model} value={model}>{model}</option>
                  ))
                )}
              </select>
            </label>
          </section>

          <section className="space-y-2.5">
            <div className="font-semibold text-[var(--text-primary)]">允许使用的生图通道</div>
            <div className="text-[11px] text-[var(--text-muted)]">引擎只会从「已勾选且有活跃凭据」的通道中选择模型</div>
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              {IMAGE_PROVIDER_META.map((p) => {
                const st = capabilities?.image?.providers?.[p.id];
                const checked = allowedImg.includes(p.id);
                return (
                  <label key={p.id} className={`flex cursor-pointer items-start gap-2.5 rounded-xl border p-3 transition-colors ${checked ? "border-indigo-500/50 bg-indigo-500/5" : "border-[var(--border-default)] bg-[var(--bg-elevated)] opacity-80"}`}>
                    <input type="checkbox" checked={checked} onChange={() => toggleItem(allowedImg, setAllowedImg, p.id)} className="mt-0.5" />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center justify-between gap-1">
                        <span className="font-medium text-[var(--text-primary)]">{p.label}</span>
                        {renderProviderStatus(st)}
                      </div>
                      <div className="mt-0.5 text-[10px] text-[var(--text-muted)]">{p.desc}</div>
                    </div>
                  </label>
                );
              })}
            </div>
          </section>

          <section className="space-y-2.5">
            <div className="font-semibold text-[var(--text-primary)]">允许使用的视频通道</div>
            <div className="text-[11px] text-[var(--text-muted)]">当允许的通道均无凭据时，将按自动降级策略处理</div>
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              {VIDEO_PROVIDER_META.map((p) => {
                const st = capabilities?.video?.providers?.[p.id];
                const checked = allowedVid.includes(p.id);
                return (
                  <label key={p.id} className={`flex cursor-pointer items-start gap-2.5 rounded-xl border p-3 transition-colors ${checked ? "border-indigo-500/50 bg-indigo-500/5" : "border-[var(--border-default)] bg-[var(--bg-elevated)] opacity-80"}`}>
                    <input type="checkbox" checked={checked} onChange={() => toggleItem(allowedVid, setAllowedVid, p.id)} className="mt-0.5" />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center justify-between gap-1">
                        <span className="font-medium text-[var(--text-primary)]">{p.label}</span>
                        {renderProviderStatus(st)}
                      </div>
                      <div className="mt-0.5 text-[10px] text-[var(--text-muted)]">{p.desc}</div>
                    </div>
                  </label>
                );
              })}
            </div>
          </section>

          <section className="space-y-2.5">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <div className="font-semibold text-[var(--text-primary)]">允许使用的配音通道</div>
                <div className="text-[11px] text-[var(--text-muted)]">角色未指定专属音色时，将使用下方默认引擎与音色</div>
              </div>
              <div className="flex flex-wrap items-center gap-1.5 text-[10px]">
                <span className={capabilities?.tts.voice_design_available ? "rounded-full bg-emerald-500/10 px-2 py-0.5 text-emerald-600" : "rounded-full bg-slate-500/10 px-2 py-0.5 text-[var(--text-muted)]"}>
                  音色设计 {capabilities?.tts.voice_design_available ? "可用" : "不可用"}
                </span>
                <span className={capabilities?.tts.voice_clone_available ? "rounded-full bg-emerald-500/10 px-2 py-0.5 text-emerald-600" : "rounded-full bg-slate-500/10 px-2 py-0.5 text-[var(--text-muted)]"}>
                  声音克隆 {capabilities?.tts.voice_clone_available ? "可用" : "不可用"}
                </span>
              </div>
            </div>
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              {TTS_PROVIDER_META.map((p) => {
                const st = capabilities?.tts?.providers?.[p.id];
                const checked = allowedTTS.includes(p.id);
                return (
                  <label
                    key={p.id}
                    className={`flex ${p.selectable ? "cursor-pointer" : "cursor-not-allowed"} items-start gap-2.5 rounded-xl border p-3 transition-colors ${checked ? "border-indigo-500/50 bg-indigo-500/5" : "border-[var(--border-default)] bg-[var(--bg-elevated)] opacity-80"}`}
                  >
                    <input
                      type="checkbox"
                      checked={checked && p.selectable}
                      disabled={!p.selectable}
                      onChange={() => p.selectable && toggleItem(allowedTTS, setAllowedTTS, p.id)}
                      className="mt-0.5"
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center justify-between gap-1">
                        <span className="font-medium text-[var(--text-primary)]">{p.label}</span>
                        {renderProviderStatus(st, p.id)}
                      </div>
                      <div className="mt-0.5 text-[10px] text-[var(--text-muted)]">{p.desc}</div>
                    </div>
                  </label>
                );
              })}
            </div>
            <div className="grid grid-cols-1 gap-3 rounded-xl border border-[var(--border-default)] bg-[var(--bg-elevated)] p-3 sm:grid-cols-2">
              <label className="space-y-1">
                <span className="font-medium">默认配音引擎</span>
                <select
                  value={defaultEngine}
                  onChange={(e) => changeEngine(e.target.value as CharacterEngine)}
                  className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-surface)] px-2.5 py-1.5"
                >
                  {TTS_PROVIDER_META.filter((p) => p.selectable).map((p) => (
                    <option key={p.id} value={p.engine}>{p.label}</option>
                  ))}
                </select>
              </label>
              <label className="space-y-1">
                <span className="font-medium">默认音色</span>
                <select
                  value={defaultVoiceId}
                  onChange={(e) => {
                    const chosen = voiceOptions.find((v) => v.id === e.target.value);
                    setDefaultVoiceId(e.target.value);
                    setDefaultVoiceName(chosen?.name || e.target.value);
                  }}
                  className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-surface)] px-2.5 py-1.5"
                >
                  {defaultVoiceId && !voiceOptions.some((v) => v.id === defaultVoiceId) && (
                    <option value={defaultVoiceId}>{defaultVoiceName || defaultVoiceId}</option>
                  )}
                  {voiceOptions.map((v) => (
                    <option key={v.id} value={v.id}>{v.name} · {v.id}</option>
                  ))}
                  {voiceOptions.length === 0 && !defaultVoiceId && <option value="">当前引擎暂无音色目录</option>}
                </select>
              </label>
            </div>
          </section>

          <section className="grid grid-cols-1 gap-4 rounded-xl border border-[var(--border-default)] bg-[var(--bg-elevated)] p-3.5 sm:grid-cols-2">
            <label className="space-y-1">
              <span className="flex items-center gap-1 font-medium">
                <Sliders className="h-3.5 w-3.5 text-indigo-500" />
                生图并发数（0 = 按活跃账号自动）
              </span>
              <input
                type="number"
                min={0}
                max={8}
                value={concurrency}
                onChange={(e) => setConcurrency(Math.max(0, Math.min(8, Number(e.target.value) || 0)))}
                className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-surface)] px-2.5 py-1.5"
              />
            </label>
            <label className="flex cursor-pointer items-center gap-2 self-end rounded-lg border border-[var(--border-default)] bg-[var(--bg-surface)] px-3 py-2">
              <input type="checkbox" checked={autoFallbackKB} onChange={(e) => setAutoFallbackKB(e.target.checked)} />
              <div>
                <div className="font-medium">视频无凭据时自动降级 Ken Burns</div>
                <div className="text-[10px] text-[var(--text-muted)]">关闭后，无可用视频通道时将直接报错提示</div>
              </div>
            </label>
            <label className="space-y-1">
              <span className="font-medium text-[var(--text-muted)]">生图模型（auto = 从可用模型随机）</span>
              <select value={imgOverride} onChange={(e) => setImgOverride(e.target.value)} className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-surface)] px-2.5 py-1.5">
                <option value="auto">auto · 从可用生图模型随机</option>
                {imageProviders.length > 0 ? (
                  imageProviders.map((entry) => (
                    <optgroup key={entry[0]} label={entry[0] + (entry[1].available ? ' · 可用' : ' · 未配置')}>
                      {(entry[1].models || []).map((model) => (
                        <option key={model} value={model} disabled={!entry[1].available}>{model}</option>
                      ))}
                    </optgroup>
                  ))
                ) : (
                  imageModels.filter((model) => model !== "auto").map((model) => (
                    <option key={model} value={model}>{model}</option>
                  ))
                )}
              </select>
            </label>
            <label className="space-y-1">
              <span className="font-medium text-[var(--text-muted)]">视频模型（auto = 从可用模型随机）</span>
              <select value={vidOverride} onChange={(e) => setVidOverride(e.target.value)} className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-surface)] px-2.5 py-1.5">
                <option value="auto">auto · 从可用视频模型随机</option>
                {videoProviders.map((entry) => (
                  <optgroup key={entry[0]} label={entry[0] + (entry[1].available ? ' · 可用' : ' · 未配置')}>
                    {(entry[1].models || []).map((model) => (
                      <option key={model} value={model} disabled={!entry[1].available}>{model}</option>
                    ))}
                  </optgroup>
                ))}
                {videoProviders.length === 0 && videoModels.filter((model) => model !== "auto").map((model) => (
                  <option key={model} value={model}>{model}</option>
                ))}
              </select>
            </label>
          </section>
        </div>

        <div className="flex items-center justify-end gap-2 border-t border-[var(--border-default)] px-5 py-3">
          <button type="button" onClick={onClose} className="rounded-lg border border-[var(--border-default)] px-3.5 py-1.5 text-xs hover:bg-[var(--bg-surface-hover)]">
            取消
          </button>
          <button type="button" onClick={handleApply} className="rounded-lg bg-indigo-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-indigo-500">
            保存能力设置
          </button>
        </div>
      </div>
    </div>
  );
};
