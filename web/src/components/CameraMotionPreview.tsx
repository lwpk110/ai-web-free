import React, { useState, useEffect } from "react";
import { Play, Pause, Plus, Sparkles, Film, Loader2, CheckCircle2 } from "lucide-react";
import type { VideoCameraPreset } from "../data/videoCameraPresets";

interface CameraMotionPreviewProps {
  preset: VideoCameraPreset;
  museVideoUrl?: string;
  museGenerating?: boolean;
  museProgress?: number;
  museError?: string | null;
  onGenerateMusePreview?: (preset: VideoCameraPreset) => void;
  onAppendClause?: (preset: VideoCameraPreset) => void;
  onApplyExample?: (preset: VideoCameraPreset) => void;
}

const CAMERA_KEYFRAMES_CSS = `
@keyframes cam-bg-dolly-in {
  0%, 10% { transform: scale(1) translate3d(0, 0, 0); }
  85%, 100% { transform: scale(1.42) translate3d(0, -2%, 0); }
}
@keyframes cam-bg-rush-in {
  0%, 15% { transform: scale(1); filter: blur(0px); }
  60% { transform: scale(1.55); filter: blur(1.5px); }
  85%, 100% { transform: scale(1.9); filter: blur(0px); }
}
@keyframes cam-bg-pull-back {
  0%, 10% { transform: scale(1.65) translate3d(0, 2%, 0); }
  85%, 100% { transform: scale(0.96) translate3d(0, 0, 0); }
}
@keyframes cam-bg-vertigo {
  0%, 10% { transform: scale(0.86); }
  85%, 100% { transform: scale(1.75); }
}
@keyframes cam-subj-vertigo {
  0%, 10% { transform: scale(1.04); }
  85%, 100% { transform: scale(0.96); }
}
@keyframes cam-bg-track {
  0% { transform: translateX(6%) scale(1.12); }
  100% { transform: translateX(-6%) scale(1.12); }
}
@keyframes cam-fg-track {
  0% { transform: translateX(35%); }
  100% { transform: translateX(-45%); }
}
@keyframes cam-bg-whip {
  0%, 22% { transform: translateX(12%) scale(1.15); filter: blur(0px); }
  45% { transform: translateX(-2%) scale(1.18); filter: blur(5px); }
  68%, 100% { transform: translateX(-14%) scale(1.15); filter: blur(0px); }
}
@keyframes cam-bg-tilt-up {
  0%, 10% { transform: translateY(-14%) scale(1.22); }
  85%, 100% { transform: translateY(14%) scale(1.22); }
}
@keyframes cam-orbit-subj {
  0% { transform: perspective(360px) rotateY(-38deg) translateX(-14px) scale(1.05); }
  50% { transform: perspective(360px) rotateY(0deg) translateX(0px) scale(1.14); }
  100% { transform: perspective(360px) rotateY(38deg) translateX(14px) scale(1.05); }
}
@keyframes cam-orbit-bg {
  0% { transform: translateX(10%) scale(1.18); }
  100% { transform: translateX(-10%) scale(1.18); }
}
@keyframes cam-crane-up {
  0%, 10% { transform: perspective(380px) rotateX(0deg) translateY(12%) scale(1.32); }
  85%, 100% { transform: perspective(380px) rotateX(14deg) translateY(-10%) scale(0.95); }
}
@keyframes cam-fpv-dive {
  0% { transform: scale(0.95) rotate(-6deg) translateY(-6%); }
  45% { transform: scale(1.32) rotate(8deg) translateY(2%); }
  80%, 100% { transform: scale(1.7) rotate(-3deg) translateY(6%); }
}
@keyframes cam-aerial-cruise {
  0% { transform: scale(1.06) translate3d(-4%, 4%, 0); }
  100% { transform: scale(1.2) translate3d(4%, -4%, 0); }
}
@keyframes cam-handheld {
  0% { transform: translate(0px, 0px) rotate(0deg) scale(1.08); }
  20% { transform: translate(-2.5px, 1.5px) rotate(-0.8deg) scale(1.09); }
  40% { transform: translate(2px, -2px) rotate(0.7deg) scale(1.08); }
  60% { transform: translate(-1.5px, -1px) rotate(-0.5deg) scale(1.1); }
  80% { transform: translate(2.5px, 1.5px) rotate(0.9deg) scale(1.08); }
  100% { transform: translate(0px, 0px) rotate(0deg) scale(1.08); }
}
@keyframes cam-rack-fg {
  0%, 25% { filter: blur(0px); transform: scale(1.05); opacity: 1; }
  65%, 100% { filter: blur(4px); transform: scale(1.14); opacity: 0.72; }
}
@keyframes cam-rack-subj {
  0%, 25% { filter: blur(3.5px); transform: scale(0.96); opacity: 0.75; }
  65%, 100% { filter: blur(0px); transform: scale(1.08); opacity: 1; }
}
@keyframes cam-anamorphic-flare {
  0%, 10% { transform: translateX(-60%) scaleX(0.8); opacity: 0.35; }
  50% { transform: translateX(0%) scaleX(1.25); opacity: 0.95; }
  90%, 100% { transform: translateX(60%) scaleX(0.8); opacity: 0.35; }
}
@keyframes cam-macro-slomo {
  0%, 10% { transform: scale(1.38) translateY(3%); }
  85%, 100% { transform: scale(1.62) translateY(-3%); }
}
@keyframes cam-progress-loop {
  0% { width: 0%; }
  100% { width: 100%; }
}
`;

function getMotionAnimations(presetId: string): {
  bgAnim: string;
  subjAnim?: string;
  fgAnim?: string;
  isAnamorphic?: boolean;
  showFgObject?: boolean;
  motionTag: string;
} {
  switch (presetId) {
    case "slow-dolly-in":
      return { bgAnim: "cam-bg-dolly-in 2.8s ease-in-out infinite", motionTag: "PUSH IN · 50mm" };
    case "rapid-rush-in":
      return { bgAnim: "cam-bg-rush-in 2.2s cubic-bezier(0.7, 0, 0.2, 1) infinite", motionTag: "RUSH IN · FAST" };
    case "pull-back-reveal":
      return { bgAnim: "cam-bg-pull-back 2.8s ease-out infinite", motionTag: "PULL BACK · REVEAL" };
    case "dolly-zoom":
      return {
        bgAnim: "cam-bg-vertigo 2.8s ease-in-out infinite",
        subjAnim: "cam-subj-vertigo 2.8s ease-in-out infinite",
        motionTag: "VERTIGO · DOLLY ZOOM",
      };
    case "sideways-tracking":
      return {
        bgAnim: "cam-bg-track 2.8s linear infinite alternate",
        fgAnim: "cam-fg-track 2.8s linear infinite",
        showFgObject: true,
        motionTag: "PARALLAX · TRACKING",
      };
    case "whip-pan":
      return { bgAnim: "cam-bg-whip 2.2s cubic-bezier(0.8, 0, 0.2, 1) infinite", motionTag: "WHIP PAN · TRANSITION" };
    case "tilt-up-reveal":
      return { bgAnim: "cam-bg-tilt-up 2.8s ease-in-out infinite", motionTag: "TILT UP · LOW ANGLE" };
    case "orbit-360":
      return {
        bgAnim: "cam-orbit-bg 3s ease-in-out infinite alternate",
        subjAnim: "cam-orbit-subj 3s ease-in-out infinite alternate",
        motionTag: "360° ORBIT · ARC",
      };
    case "crane-up":
      return { bgAnim: "cam-crane-up 2.8s ease-in-out infinite", motionTag: "CRANE UP · EPIC" };
    case "fpv-flythrough":
      return { bgAnim: "cam-fpv-dive 2.4s ease-in-out infinite", motionTag: "FPV DIVE · BANKING" };
    case "aerial-establishing":
      return { bgAnim: "cam-aerial-cruise 3s linear infinite alternate", motionTag: "AERIAL · DRONE" };
    case "handheld-doc":
      return { bgAnim: "cam-handheld 2.2s ease-in-out infinite", motionTag: "HANDHELD · 35mm" };
    case "rack-focus":
      return {
        bgAnim: "cam-bg-dolly-in 3s ease-in-out infinite",
        subjAnim: "cam-rack-subj 3s ease-in-out infinite",
        fgAnim: "cam-rack-fg 3s ease-in-out infinite",
        showFgObject: true,
        motionTag: "RACK FOCUS · f/1.4",
      };
    case "anamorphic-cinema":
      return {
        bgAnim: "cam-aerial-cruise 3s ease-in-out infinite alternate",
        isAnamorphic: true,
        motionTag: "ANAMORPHIC · 2.39:1",
      };
    case "macro-slomo":
      return { bgAnim: "cam-macro-slomo 3s ease-in-out infinite", motionTag: "MACRO · 120FPS" };
    default:
      return { bgAnim: "cam-bg-dolly-in 2.8s ease-in-out infinite", motionTag: "CINEMATIC" };
  }
}

export function CameraMotionPreview({
  preset,
  museVideoUrl,
  museGenerating = false,
  museProgress = 0,
  museError = null,
  onGenerateMusePreview,
  onAppendClause,
  onApplyExample,
}: CameraMotionPreviewProps): React.JSX.Element {
  const [playing, setPlaying] = useState<boolean>(true);
  const [viewMode, setViewMode] = useState<"muse" | "schematic">(
    museVideoUrl ? "muse" : "schematic",
  );

  useEffect(() => {
    setViewMode(museVideoUrl ? "muse" : "schematic");
  }, [preset.id, museVideoUrl]);

  const anim = getMotionAnimations(preset.id);
  const playState = playing ? "running" : "paused";
  const showRealMuseVideo = viewMode === "muse" && Boolean(museVideoUrl);

  return (
    <div className="rounded-xl border z-border bg-slate-950/90 text-slate-100 overflow-hidden shadow-inner flex flex-col sm:flex-row">
      <style>{CAMERA_KEYFRAMES_CSS}</style>

      {/* 左侧：16:9 实时运镜动态视口（支持 Muse AI 真人实拍 & CSS 运镜示意切换） */}
      <div className="relative w-full sm:w-64 md:w-72 aspect-video shrink-0 overflow-hidden bg-gradient-to-b from-slate-900 via-indigo-950 to-slate-950 select-none">
        {showRealMuseVideo ? (
          <video
            key={museVideoUrl}
            src={museVideoUrl}
            autoPlay
            loop
            muted
            playsInline
            controls
            className="w-full h-full object-cover bg-black"
          />
        ) : (
          <>
            {/* 运动背景与山脉/城市层 */}
            <div
              key={`bg-${preset.id}`}
              className="absolute inset-0 flex items-center justify-center"
              style={{
                animation: anim.bgAnim,
                animationPlayState: playState,
              }}
            >
              {/* 远景星空/光斑 */}
              <div className="absolute inset-0 bg-[radial-gradient(circle_at_30%_25%,rgba(99,102,241,0.35),transparent_55%),radial-gradient(circle_at_75%_60%,rgba(245,158,11,0.22),transparent_50%)]" />
              {/* 透视网格地面 */}
              <div
                className="absolute inset-x-[-30%] bottom-[-20%] h-[58%] opacity-35"
                style={{
                  backgroundImage:
                    "linear-gradient(to right, rgba(148,163,184,0.35) 1px, transparent 1px), linear-gradient(to bottom, rgba(148,163,184,0.35) 1px, transparent 1px)",
                  backgroundSize: "22px 16px",
                  transform: "perspective(180px) rotateX(62deg)",
                }}
              />
              {/* 远景山峰剪影 */}
              <svg
                viewBox="0 0 320 120"
                className="absolute bottom-[24%] w-full h-20 text-indigo-900/70 fill-current"
                preserveAspectRatio="none"
              >
                <path d="M0,120 L0,75 L48,35 L95,70 L155,20 L215,68 L268,38 L320,78 L320,120 Z" />
              </svg>

              {/* 主体锚点（人物/几何体） */}
              <div
                key={`subj-${preset.id}`}
                className="relative z-10 flex flex-col items-center"
                style={
                  anim.subjAnim
                    ? { animation: anim.subjAnim, animationPlayState: playState }
                    : undefined
                }
              >
                <div className="w-7 h-7 rounded-full bg-gradient-to-tr from-amber-400 to-rose-400 shadow-[0_0_20px_rgba(251,191,36,0.6)] border border-white/50" />
                <div className="w-10 h-6 mt-1 rounded-t-xl bg-gradient-to-b from-slate-200/90 to-slate-400/70 shadow-md" />
              </div>
            </div>

            {/* 前景虚化/视差层（用于 Rack Focus / Tracking） */}
            {anim.showFgObject && (
              <div
                key={`fg-${preset.id}`}
                className="absolute bottom-2 left-6 z-20 pointer-events-none"
                style={{
                  animation: anim.fgAnim,
                  animationPlayState: playState,
                }}
              >
                <div className="w-6 h-14 rounded-lg bg-gradient-to-t from-cyan-500/80 to-indigo-400/70 border border-white/30 shadow-lg" />
              </div>
            )}

            {/* 变形宽银幕横向拉丝眩光 + 黑边 */}
            {anim.isAnamorphic && (
              <>
                <div className="absolute inset-x-0 top-0 h-2.5 bg-black z-30" />
                <div className="absolute inset-x-0 bottom-0 h-2.5 bg-black z-30" />
                <div
                  className="absolute top-1/2 inset-x-0 h-[2px] z-20 bg-gradient-to-r from-transparent via-cyan-400 to-transparent shadow-[0_0_12px_#22d3ee]"
                  style={{
                    animation: "cam-anamorphic-flare 2.8s ease-in-out infinite",
                    animationPlayState: playState,
                  }}
                />
              </>
            )}

            {/* 摄影机取景器 HUD */}
            <div className="absolute inset-2 z-30 pointer-events-none flex flex-col justify-between">
              <div className="flex items-center justify-between text-[9px] font-mono tracking-wider">
                <span className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-black/55 text-rose-400">
                  <span className="w-1.5 h-1.5 rounded-full bg-rose-500 animate-pulse" />
                  REC 3s
                </span>
                <span className="px-1.5 py-0.5 rounded bg-black/55 text-cyan-300">
                  {anim.motionTag}
                </span>
              </div>

              {/* 中心十字准星 */}
              <div className="self-center w-4 h-4 relative opacity-50">
                <span className="absolute inset-x-0 top-1/2 h-px bg-white/80" />
                <span className="absolute inset-y-0 left-1/2 w-px bg-white/80" />
              </div>

              {/* 3 秒循环进度条 */}
              <div className="h-0.5 w-full bg-white/15 rounded-full overflow-hidden">
                <div
                  key={`prog-${preset.id}`}
                  className="h-full bg-amber-400"
                  style={{
                    animation: "cam-progress-loop 2.8s linear infinite",
                    animationPlayState: playState,
                  }}
                />
              </div>
            </div>

            {/* 播放/暂停悬浮按钮 */}
            <button
              type="button"
              onClick={() => setPlaying((p) => !p)}
              title={playing ? "暂停预览" : "播放预览"}
              className="absolute bottom-3 right-2.5 z-40 p-1 rounded-full bg-black/60 hover:bg-black/80 text-white/90 transition-colors cursor-pointer"
            >
              {playing ? <Pause className="w-3 h-3" /> : <Play className="w-3 h-3" />}
            </button>
          </>
        )}

        {/* Muse AI 生成中状态遮罩 */}
        {museGenerating && (
          <div className="absolute inset-0 z-40 bg-black/60 backdrop-blur-[1px] flex flex-col items-center justify-center gap-1.5 text-white text-[11px]">
            <Loader2 className="w-5 h-5 animate-spin text-purple-400" />
            <span className="font-medium">Muse AI 真人实拍生成中...</span>
            <span className="text-[10px] font-mono text-purple-300">{museProgress}%</span>
          </div>
        )}
      </div>

      {/* 右侧：运镜解析与快捷操作 */}
      <div className="flex-1 p-3 flex flex-col justify-between gap-2 bg-slate-900/60">
        <div className="space-y-1">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs font-semibold text-white">{preset.nameZh}</span>
              <span className="text-[11px] font-mono px-1.5 py-0.5 rounded bg-indigo-500/20 text-indigo-300 border border-indigo-500/30">
                {preset.termEn}
              </span>
              <span className="text-[10px] px-1.5 py-0.5 rounded bg-white/10 text-slate-300">
                {preset.category}
              </span>
            </div>
            {museVideoUrl && (
              <div className="inline-flex rounded-md border border-white/15 overflow-hidden text-[10px]">
                <button
                  type="button"
                  onClick={() => setViewMode("muse")}
                  className={`px-2 py-0.5 cursor-pointer transition-colors ${
                    viewMode === "muse"
                      ? "bg-purple-600 text-white font-medium"
                      : "bg-black/30 text-slate-300 hover:text-white"
                  }`}
                >
                  🎬 Muse 实拍
                </button>
                <button
                  type="button"
                  onClick={() => setViewMode("schematic")}
                  className={`px-2 py-0.5 cursor-pointer transition-colors ${
                    viewMode === "schematic"
                      ? "bg-indigo-600 text-white font-medium"
                      : "bg-black/30 text-slate-300 hover:text-white"
                  }`}
                >
                  📐 运镜示意
                </button>
              </div>
            )}
          </div>
          <p className="text-[11px] text-slate-300 leading-relaxed">{preset.effectDesc}</p>
          <p className="text-[10px] font-mono text-amber-300/90 truncate" title={preset.insertClauseEn}>
            提示词：{preset.insertClauseEn}
          </p>
          {museError && (
            <p className="text-[10px] text-rose-400 truncate" title={museError}>
              Muse 实拍生成失败：{museError}
            </p>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-2 pt-1">
          {museVideoUrl ? (
            <span className="inline-flex items-center gap-1 px-2.5 py-1 rounded-md bg-emerald-500/15 text-emerald-300 border border-emerald-500/30 font-medium text-[11px]">
              <CheckCircle2 className="w-3 h-3" />
              实拍示例已就绪
            </span>
          ) : (
            onGenerateMusePreview && (
              <button
                type="button"
                disabled={museGenerating}
                onClick={() => onGenerateMusePreview(preset)}
                className="inline-flex items-center gap-1 px-2.5 py-1 rounded-md bg-purple-600 hover:bg-purple-500 text-white font-medium text-[11px] transition-colors cursor-pointer disabled:opacity-50"
                title="调用 Muse AI 生成 5 秒真人实拍电影级运镜示例（生成一次后永久复用）"
              >
                {museGenerating ? (
                  <Loader2 className="w-3 h-3 animate-spin" />
                ) : (
                  <Film className="w-3 h-3" />
                )}
                {museGenerating ? `生成中 ${museProgress}%` : "Muse AI 真人实拍示例 (5s)"}
              </button>
            )
          )}
          {onAppendClause && (
            <button
              type="button"
              onClick={() => onAppendClause(preset)}
              className="inline-flex items-center gap-1 px-2.5 py-1 rounded-md border border-white/20 hover:bg-white/10 text-[11px] text-white transition-colors cursor-pointer"
            >
              <Plus className="w-3 h-3" />
              追加运镜到当前 Prompt
            </button>
          )}
          {onApplyExample && (
            <button
              type="button"
              onClick={() => onApplyExample(preset)}
              className="inline-flex items-center gap-1 px-2.5 py-1 rounded-md bg-amber-500 hover:bg-amber-400 text-slate-950 font-medium text-[11px] transition-colors cursor-pointer"
            >
              <Sparkles className="w-3 h-3" />
              套用双语导演示例
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
