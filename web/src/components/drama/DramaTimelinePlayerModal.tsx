import { Download, Film, Layers, Loader2, Pause, Play, RotateCcw, Volume2, VolumeX, X } from "lucide-react";
import React, { useEffect, useMemo, useRef, useState } from "react";
import type { ComposerShotItem } from "../../lib/dramaVideoComposer";

interface DramaTimelinePlayerModalProps {
  open: boolean;
  onClose: () => void;
  title: string;
  aspectRatio: string;
  shots: ComposerShotItem[];
  bgmUrl?: string;
  bgmVolume?: number;
  showSubtitles?: boolean;
  exportedVideoUrl?: string;
  exportedFilename?: string;
  exportProgress: number;
  exportStageText?: string;
  onExportVideo: () => Promise<void>;
  onExportZip: () => Promise<void>;
}

export const DramaTimelinePlayerModal: React.FC<DramaTimelinePlayerModalProps> = ({
  open,
  onClose,
  title,
  aspectRatio,
  shots,
  bgmUrl,
  bgmVolume = 0.22,
  showSubtitles = true,
  exportedVideoUrl,
  exportedFilename,
  exportProgress,
  exportStageText,
  onExportVideo,
  onExportZip,
}) => {
  const [isPlaying, setIsPlaying] = useState(false);
  const [activeIdx, setActiveIdx] = useState(0);
  const [muted, setMuted] = useState(false);
  const [localProgressPct, setLocalProgressPct] = useState(0);

  const shotAudioRef = useRef<HTMLAudioElement | null>(null);
  const bgmAudioRef = useRef<HTMLAudioElement | null>(null);
  const videoElRef = useRef<HTMLVideoElement | null>(null);
  const timerRef = useRef<number | null>(null);

  const totalDuration = useMemo(
    () => shots.reduce((sum, s) => sum + Math.max(1.5, s.durationSec || 4), 0),
    [shots],
  );

  const curShot = shots[activeIdx] || shots[0];

  const stopAll = () => {
    setIsPlaying(false);
    if (timerRef.current !== null) {
      window.clearInterval(timerRef.current);
      timerRef.current = null;
    }
    shotAudioRef.current?.pause();
    bgmAudioRef.current?.pause();
    videoElRef.current?.pause();
  };

  useEffect(() => {
    if (!open) {
      stopAll();
      setActiveIdx(0);
      setLocalProgressPct(0);
    }
  }, [open]);

  useEffect(() => {
    if (!open || !isPlaying || !curShot) return;

    const durMs = Math.max(1500, (curShot.durationSec || 4) * 1000);
    const startAt = performance.now();

    if (shotAudioRef.current) {
      shotAudioRef.current.pause();
      if (curShot.audioUrl) {
        shotAudioRef.current.src = curShot.audioUrl;
        shotAudioRef.current.muted = muted;
        shotAudioRef.current.currentTime = 0;
        shotAudioRef.current.play().catch(() => {});
      }
    }

    if (bgmAudioRef.current && bgmUrl) {
      bgmAudioRef.current.volume = muted ? 0 : bgmVolume;
      if (bgmAudioRef.current.paused) {
        bgmAudioRef.current.play().catch(() => {});
      }
    }

    if (videoElRef.current && curShot.renderMode === "video" && curShot.videoUrl) {
      videoElRef.current.currentTime = 0;
      videoElRef.current.play().catch(() => {});
    }

    if (timerRef.current !== null) window.clearInterval(timerRef.current);
    timerRef.current = window.setInterval(() => {
      const elapsed = performance.now() - startAt;
      const pct = Math.min(100, Math.round((elapsed / durMs) * 100));
      setLocalProgressPct(pct);
      if (elapsed >= durMs) {
        window.clearInterval(timerRef.current!);
        timerRef.current = null;
        if (activeIdx + 1 < shots.length) {
          setActiveIdx((i) => i + 1);
          setLocalProgressPct(0);
        } else {
          stopAll();
        }
      }
    }, 60);

    return () => {
      if (timerRef.current !== null) {
        window.clearInterval(timerRef.current);
        timerRef.current = null;
      }
    };
  }, [open, isPlaying, activeIdx, curShot, bgmUrl, bgmVolume, muted, shots.length]);

  useEffect(() => {
    if (bgmAudioRef.current) bgmAudioRef.current.volume = muted ? 0 : bgmVolume;
    if (shotAudioRef.current) shotAudioRef.current.muted = muted;
  }, [muted, bgmVolume]);

  if (!open) return null;

  const isPortrait = aspectRatio === "9:16";
  const isSquare = aspectRatio === "1:1";
  const frameClass = isPortrait
    ? "w-[270px] h-[480px]"
    : isSquare
      ? "w-[400px] h-[400px]"
      : "w-[560px] h-[315px]";

  const exporting = exportProgress > 0 && exportProgress < 100;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4 backdrop-blur-sm">
      <div className="flex max-h-[92vh] w-full max-w-5xl flex-col overflow-hidden rounded-2xl border border-[var(--border-default)] bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-2xl">
        <div className="flex items-center justify-between border-b border-[var(--border-default)] px-5 py-3.5">
          <div>
            <h3 className="text-sm font-semibold">{title} · 多轨同步监视与成片导出</h3>
            <p className="text-xs text-[var(--text-muted)]">
              共 {shots.length} 镜 · 总时长 {totalDuration.toFixed(1)}s · 画幅 {aspectRatio}
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg p-1.5 text-[var(--text-muted)] hover:bg-[var(--bg-surface-hover)] hover:text-[var(--text-primary)]"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="grid flex-1 grid-cols-1 gap-5 overflow-y-auto p-5 lg:grid-cols-[1fr_320px]">
          <div className="flex flex-col items-center justify-center rounded-xl border border-[var(--border-default)] bg-zinc-950 p-4">
            <div className={`relative overflow-hidden rounded-xl bg-black shadow-xl ${frameClass}`}>
              {curShot?.renderMode === "video" && curShot.videoUrl ? (
                <video
                  ref={videoElRef}
                  src={curShot.videoUrl}
                  poster={curShot.imageUrl}
                  muted
                  playsInline
                  className="h-full w-full object-cover"
                />
              ) : curShot?.imageUrl ? (
                <img
                  src={curShot.imageUrl}
                  alt={curShot.title}
                  className="h-full w-full object-cover transition-transform duration-[4000ms] ease-out"
                  style={{
                    transform: isPlaying
                      ? activeIdx % 2 === 0
                        ? "scale(1.1) translateY(-1.5%)"
                        : "scale(1.02) translateX(-1.5%)"
                      : "scale(1.0)",
                  }}
                />
              ) : (
                <div className="flex h-full w-full items-center justify-center px-6 text-center text-xs text-zinc-400">
                  第 {curShot?.index || 1} 镜暂未生成视觉画面
                </div>
              )}

              {showSubtitles && curShot?.dialogue && (
                <div className="pointer-events-none absolute inset-x-4 bottom-5 flex justify-center">
                  <div className="rounded-lg bg-black/70 px-3 py-1.5 text-center text-xs font-medium text-white shadow">
                    {curShot.dialogue}
                  </div>
                </div>
              )}

              <div className="absolute top-2.5 left-2.5 rounded-full bg-black/60 px-2 py-0.5 text-[10px] text-white">
                #{curShot?.index || 1} · {curShot?.renderMode === "video" ? "真视频轨" : "Ken Burns 动效"}
              </div>
            </div>

            <div className="mt-4 flex w-full max-w-md items-center gap-3">
              <button
                type="button"
                onClick={() => (isPlaying ? stopAll() : setIsPlaying(true))}
                className="inline-flex items-center gap-1.5 rounded-lg bg-indigo-600 px-3.5 py-1.5 text-xs font-medium text-white hover:bg-indigo-500"
              >
                {isPlaying ? <Pause className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />}
                {isPlaying ? "暂停" : "从当前镜连播"}
              </button>
              <button
                type="button"
                onClick={() => {
                  stopAll();
                  setActiveIdx(0);
                  setLocalProgressPct(0);
                }}
                className="rounded-lg border border-zinc-700 px-2.5 py-1.5 text-xs text-zinc-200 hover:bg-zinc-800"
              >
                <RotateCcw className="h-3.5 w-3.5" />
              </button>
              <button
                type="button"
                onClick={() => setMuted((m) => !m)}
                className="rounded-lg border border-zinc-700 px-2.5 py-1.5 text-xs text-zinc-200 hover:bg-zinc-800"
              >
                {muted ? <VolumeX className="h-3.5 w-3.5" /> : <Volume2 className="h-3.5 w-3.5" />}
              </button>
              <div className="flex-1 h-1.5 overflow-hidden rounded-full bg-zinc-800">
                <div className="h-full bg-indigo-500 transition-all" style={{ width: `${localProgressPct}%` }} />
              </div>
            </div>

            <audio ref={shotAudioRef} className="hidden" />
            {bgmUrl && <audio ref={bgmAudioRef} src={bgmUrl} loop className="hidden" />}
          </div>

          <div className="flex flex-col gap-4">
            <div className="rounded-xl border border-[var(--border-default)] bg-[var(--bg-elevated)] p-3.5">
              <div className="text-xs font-semibold mb-2">时间轴镜头序列</div>
              <div className="max-h-[220px] space-y-1.5 overflow-y-auto pr-1">
                {shots.map((s, idx) => (
                  <button
                    key={s.id}
                    type="button"
                    onClick={() => {
                      stopAll();
                      setActiveIdx(idx);
                      setLocalProgressPct(0);
                    }}
                    className={`flex w-full items-center gap-2 rounded-lg border px-2.5 py-1.5 text-left text-xs transition ${
                      idx === activeIdx
                        ? "border-indigo-500 bg-indigo-500/10"
                        : "border-[var(--border-default)] hover:bg-[var(--bg-surface-hover)]"
                    }`}
                  >
                    <span className="font-mono text-[10px] font-bold text-indigo-500">#{s.index}</span>
                    <span className="flex-1 truncate">{s.dialogue || s.title}</span>
                    <span className="font-mono text-[10px] text-[var(--text-muted)]">{s.durationSec.toFixed(1)}s</span>
                  </button>
                ))}
              </div>
            </div>

            <div className="rounded-xl border border-[var(--border-default)] bg-[var(--bg-elevated)] p-3.5 space-y-2.5">
              <div className="text-xs font-semibold">零外部依赖成片合成与工程交付</div>
              <p className="text-[11px] leading-relaxed text-[var(--text-muted)]">
                直接通过浏览器 Canvas + WebAudio 将全部镜头画面、Ken Burns 运镜、对白配音、BGM 与字幕流式合成为单个连续视频文件，或打包真实工程 ZIP（含剪映草稿）。
              </p>

              {exporting && (
                <div className="space-y-1 rounded-lg bg-[var(--bg-muted)] p-2">
                  <div className="flex items-center justify-between text-[11px]">
                    <span className="inline-flex items-center gap-1 font-medium">
                      <Loader2 className="h-3 w-3 animate-spin text-emerald-500" />
                      {exportProgress}%
                    </span>
                    <span className="truncate text-[var(--text-muted)] max-w-[180px]">{exportStageText}</span>
                  </div>
                  <div className="h-1.5 w-full overflow-hidden rounded-full bg-black/10">
                    <div className="h-full bg-emerald-500 transition-all" style={{ width: `${exportProgress}%` }} />
                  </div>
                </div>
              )}

              <button
                type="button"
                onClick={onExportVideo}
                disabled={exporting || shots.length === 0}
                className="w-full inline-flex items-center justify-center gap-1.5 rounded-lg bg-emerald-600 px-3 py-2 text-xs font-medium text-white hover:bg-emerald-500 disabled:opacity-50"
              >
                <Film className="h-3.5 w-3.5" />
                合成并导出单个连续成片
              </button>

              {exportedVideoUrl && (
                <a
                  href={exportedVideoUrl}
                  download={exportedFilename || "short-drama.webm"}
                  className="w-full inline-flex items-center justify-center gap-1.5 rounded-lg border border-emerald-500/40 bg-emerald-500/10 px-3 py-1.5 text-xs font-medium text-emerald-600 dark:text-emerald-400"
                >
                  <Download className="h-3.5 w-3.5" />
                  下载最近合成文件 ({exportedFilename})
                </a>
              )}

              <button
                type="button"
                onClick={onExportZip}
                disabled={exporting || shots.length === 0}
                className="w-full inline-flex items-center justify-center gap-1.5 rounded-lg border border-[var(--border-default)] bg-[var(--bg-surface)] px-3 py-2 text-xs font-medium hover:bg-[var(--bg-surface-hover)] disabled:opacity-50"
              >
                <Layers className="h-3.5 w-3.5 text-sky-500" />
                打包完整工程 ZIP（含视频/配音/SRT/剪映草稿）
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
