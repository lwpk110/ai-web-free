import { CheckCircle2, KeyRound, Settings2, Sliders, X, AlertCircle } from "lucide-react";
import React, { useEffect, useState } from "react";
import type { DramaCapabilities } from "../../api/client";
import type { ScriptNodeData } from "../../lib/dramaPresets";

interface DramaSettingsModalProps {
  open: boolean;
  onClose: () => void;
  capabilities: DramaCapabilities | null;
  scriptData: ScriptNodeData | undefined;
  onSave: (patch: Partial<ScriptNodeData>) => void;
  onOpenCloudKeyModal: () => void;
}

const IMAGE_PROVIDER_META = [
  { id: "chatgpt", label: "ChatGPT (gpt-image-1)", desc: "支持多参考图视觉锁脸与高一致性绘本" },
  { id: "siliconflow", label: "硅基流动 (FLUX.1-schnell)", desc: "免费/低成本极速文生图（需 SILICONFLOW_API_KEY）" },
  { id: "dashscope", label: "阿里云百炼 (Wanx-v1)", desc: "通义万相文生图（需 DASHSCOPE_API_KEY）" },
  { id: "jimeng", label: "即梦 (Jimeng 2.1)", desc: "字节系国风/写实生图（需即梦账号）" },
];

const VIDEO_PROVIDER_META = [
  { id: "meta_muse", label: "Meta Muse (muse-video / muse-talk)", desc: "免费逆向音画协同 & 数字人口型同步" },
  { id: "siliconflow", label: "硅基流动 (CogVideoX-5b)", desc: "开源商业级图生视频（需 SILICONFLOW_API_KEY）" },
  { id: "dashscope", label: "阿里云百炼 (Wan-Video-v1)", desc: "通义万相图生视频（需 DASHSCOPE_API_KEY）" },
  { id: "jimeng", label: "即梦视频 (Jimeng-Video-2.0)", desc: "电影级运镜视频（需即梦账号）" },
];

export const DramaSettingsModal: React.FC<DramaSettingsModalProps> = ({
  open,
  onClose,
  capabilities,
  scriptData,
  onSave,
  onOpenCloudKeyModal,
}) => {
  const [allowedImg, setAllowedImg] = useState<string[]>([]);
  const [allowedVid, setAllowedVid] = useState<string[]>([]);
  const [imgOverride, setImgOverride] = useState("");
  const [vidOverride, setVidOverride] = useState("");
  const [concurrency, setConcurrency] = useState(0);
  const [autoFallbackKB, setAutoFallbackKB] = useState(true);

  useEffect(() => {
    if (!open) return;
    // 默认仅自动预选当前真正有活跃凭据的通道，若全部未配置则留空表示自动探测
    const activeImg = IMAGE_PROVIDER_META.filter((m) => capabilities?.image?.providers?.[m.id]?.available).map((m) => m.id);
    const activeVid = VIDEO_PROVIDER_META.filter((m) => capabilities?.video?.providers?.[m.id]?.available).map((m) => m.id);
    setAllowedImg(scriptData?.allowedImageProviders?.length ? scriptData.allowedImageProviders : activeImg);
    setAllowedVid(scriptData?.allowedVideoProviders?.length ? scriptData.allowedVideoProviders : activeVid);
    setImgOverride(scriptData?.imageModelOverride || "");
    setVidOverride(scriptData?.videoModelOverride || "");
    setConcurrency(scriptData?.imageConcurrency || 0);
    setAutoFallbackKB(scriptData?.autoFallbackKenBurns ?? true);
  }, [open, capabilities, scriptData]);

  if (!open) return null;

  const toggleItem = (list: string[], setList: (v: string[]) => void, id: string) => {
    setList(list.includes(id) ? list.filter((x) => x !== id) : [...list, id]);
  };

  const handleApply = () => {
    onSave({
      allowedImageProviders: allowedImg,
      allowedVideoProviders: allowedVid,
      imageModelOverride: imgOverride.trim() || undefined,
      videoModelOverride: vidOverride.trim() || undefined,
      imageConcurrency: concurrency > 0 ? concurrency : undefined,
      autoFallbackKenBurns: autoFallbackKB,
    });
    onClose();
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/55 p-4 backdrop-blur-sm">
      <div className="flex max-h-[88vh] w-full max-w-2xl flex-col overflow-hidden rounded-2xl border border-[var(--border-default)] bg-[var(--bg-surface)] shadow-2xl">
        <div className="flex items-center justify-between border-b border-[var(--border-default)] px-5 py-3.5">
          <div className="flex items-center gap-2">
            <Settings2 className="h-4 w-4 text-indigo-500" />
            <h3 className="text-sm font-semibold">短剧能力与通道设置</h3>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg p-1 text-[var(--text-muted)] hover:bg-[var(--bg-surface-hover)]"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex-1 space-y-5 overflow-y-auto p-5 text-xs">
          {/* 生图通道白名单 */}
          <section className="space-y-2.5">
            <div className="flex items-center justify-between">
              <div>
                <div className="font-semibold text-[var(--text-primary)]">允许使用的生图通道</div>
                <div className="text-[11px] text-[var(--text-muted)]">
                  引擎只会从「已勾选且有活跃凭据」的通道中选择模型，不会调用未配置 Key 的通道
                </div>
              </div>
              <button
                type="button"
                onClick={() => {
                  onClose();
                  onOpenCloudKeyModal();
                }}
                className="inline-flex items-center gap-1 rounded-lg border border-amber-500/30 bg-amber-500/10 px-2.5 py-1 text-[11px] text-amber-700 hover:bg-amber-500/20 dark:text-amber-400"
              >
                <KeyRound className="h-3 w-3" />
                配置 API Key
              </button>
            </div>
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              {IMAGE_PROVIDER_META.map((p) => {
                const st = capabilities?.image?.providers?.[p.id];
                const avail = Boolean(st?.available);
                const checked = allowedImg.includes(p.id);
                return (
                  <label
                    key={p.id}
                    className={`flex cursor-pointer items-start gap-2.5 rounded-xl border p-3 transition-colors ${
                      checked
                        ? "border-indigo-500/50 bg-indigo-500/5"
                        : "border-[var(--border-default)] bg-[var(--bg-elevated)] opacity-80"
                    }`}
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() => toggleItem(allowedImg, setAllowedImg, p.id)}
                      className="mt-0.5"
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center justify-between gap-1">
                        <span className="font-medium text-[var(--text-primary)]">{p.label}</span>
                        {avail ? (
                          <span className="inline-flex items-center gap-0.5 text-[10px] text-emerald-600 dark:text-emerald-400">
                            <CheckCircle2 className="h-3 w-3" /> {st?.active ?? 1} 可用
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-0.5 text-[10px] text-[var(--text-muted)]">
                            <AlertCircle className="h-3 w-3" /> 未配置
                          </span>
                        )}
                      </div>
                      <div className="mt-0.5 text-[10px] text-[var(--text-muted)]">{p.desc}</div>
                    </div>
                  </label>
                );
              })}
            </div>
          </section>

          {/* 视频通道白名单 */}
          <section className="space-y-2.5">
            <div>
              <div className="font-semibold text-[var(--text-primary)]">允许使用的视频通道</div>
              <div className="text-[11px] text-[var(--text-muted)]">
                如果当前只有 Meta Muse 可用，建议仅勾选 Meta Muse；当允许的通道均无凭据时将按下方规则处理
              </div>
            </div>
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              {VIDEO_PROVIDER_META.map((p) => {
                const st = capabilities?.video?.providers?.[p.id];
                const avail = Boolean(st?.available);
                const checked = allowedVid.includes(p.id);
                return (
                  <label
                    key={p.id}
                    className={`flex cursor-pointer items-start gap-2.5 rounded-xl border p-3 transition-colors ${
                      checked
                        ? "border-indigo-500/50 bg-indigo-500/5"
                        : "border-[var(--border-default)] bg-[var(--bg-elevated)] opacity-80"
                    }`}
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() => toggleItem(allowedVid, setAllowedVid, p.id)}
                      className="mt-0.5"
                    />
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center justify-between gap-1">
                        <span className="font-medium text-[var(--text-primary)]">{p.label}</span>
                        {avail ? (
                          <span className="inline-flex items-center gap-0.5 text-[10px] text-emerald-600 dark:text-emerald-400">
                            <CheckCircle2 className="h-3 w-3" /> {st?.active ?? 1} 可用
                          </span>
                        ) : (
                          <span className="inline-flex items-center gap-0.5 text-[10px] text-[var(--text-muted)]">
                            <AlertCircle className="h-3 w-3" /> 未配置
                          </span>
                        )}
                      </div>
                      <div className="mt-0.5 text-[10px] text-[var(--text-muted)]">{p.desc}</div>
                    </div>
                  </label>
                );
              })}
            </div>
          </section>

          {/* 并发与模型覆盖 */}
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
              <input
                type="checkbox"
                checked={autoFallbackKB}
                onChange={(e) => setAutoFallbackKB(e.target.checked)}
              />
              <div>
                <div className="font-medium">视频无凭据时自动降级 Ken Burns</div>
                <div className="text-[10px] text-[var(--text-muted)]">关闭后，无可用视频通道时将直接报错提示</div>
              </div>
            </label>

            <label className="space-y-1">
              <span className="font-medium text-[var(--text-muted)]">指定生图模型（留空自动）</span>
              <input
                type="text"
                placeholder="例如 gpt-image-1 / flux-schnell"
                value={imgOverride}
                onChange={(e) => setImgOverride(e.target.value)}
                className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-surface)] px-2.5 py-1.5"
              />
            </label>

            <label className="space-y-1">
              <span className="font-medium text-[var(--text-muted)]">指定视频模型（留空自动）</span>
              <input
                type="text"
                placeholder="例如 muse-video / muse-talk"
                value={vidOverride}
                onChange={(e) => setVidOverride(e.target.value)}
                className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-surface)] px-2.5 py-1.5"
              />
            </label>
          </section>
        </div>

        <div className="flex items-center justify-end gap-2 border-t border-[var(--border-default)] px-5 py-3">
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg border border-[var(--border-default)] px-3.5 py-1.5 text-xs hover:bg-[var(--bg-surface-hover)]"
          >
            取消
          </button>
          <button
            type="button"
            onClick={handleApply}
            className="rounded-lg bg-indigo-600 px-4 py-1.5 text-xs font-medium text-white hover:bg-indigo-500"
          >
            保存能力设置
          </button>
        </div>
      </div>
    </div>
  );
};
