import React, { useState, useEffect } from "react";
import { Play, Pause, Film, Mic, Music, Subtitles, Maximize2, Sparkles, ChevronDown, ChevronUp } from "lucide-react";

export interface TimelineShotItem {
  id: string; // e.g. "shot-1"
  index: number; // 1, 2, 3
  title: string;
  durationSec: number;
  status: "pending" | "running" | "ready" | "failed" | "degraded";
  imageUrl?: string;
  voiceoverText?: string;
  speakerName?: string;
}

export interface CinematicTimelineProps {
  shots: TimelineShotItem[];
  selectedShotId?: string;
  onSelectShot: (shotId: string) => void;
  onOpenPreviewModal: () => void;
  onExportDraft?: () => void;
  isCollapsed?: boolean;
  onToggleCollapse?: () => void;
}

export const CinematicTimeline: React.FC<CinematicTimelineProps> = ({
  shots,
  selectedShotId,
  onSelectShot,
  onOpenPreviewModal,
  onExportDraft,
  isCollapsed = false,
  onToggleCollapse,
}) => {
  const [isPlaying, setIsPlaying] = useState(false);
  const [playheadSec, setPlayheadSec] = useState(0);

  const totalDuration = shots.reduce((acc, s) => acc + (s.durationSec || 4), 0);

  // 格式化时间码 00:00:04.20
  const formatTimecode = (sec: number) => {
    const m = Math.floor(sec / 60);
    const s = Math.floor(sec % 60);
    const ms = Math.floor((sec % 1) * 100);
    return `${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}.${ms.toString().padStart(2, "0")}`;
  };

  // 模拟播放头平滑移动
  useEffect(() => {
    if (!isPlaying) return;
    const interval = setInterval(() => {
      setPlayheadSec((prev) => {
        const next = prev + 0.1;
        if (next >= totalDuration) {
          setIsPlaying(false);
          return 0;
        }
        return next;
      });
    }, 100);
    return () => clearInterval(interval);
  }, [isPlaying, totalDuration]);

  if (isCollapsed) {
    return (
      <div className="h-7 shrink-0 bg-slate-900 border-t border-slate-800 px-4 flex items-center justify-between text-xs text-slate-400 select-none">
        <div className="flex items-center gap-3">
          <Film className="h-3.5 w-3.5 text-amber-500" />
          <span className="font-semibold text-slate-300">影视多轨时间轴 (已折叠)</span>
          <span className="font-mono text-[11px] text-amber-400">{shots.length} 镜头 · 约 {totalDuration.toFixed(1)}s</span>
        </div>
        <button
          onClick={onToggleCollapse}
          className="flex items-center gap-1 text-slate-400 hover:text-slate-200 text-[11px]"
        >
          <span>展开多轨</span>
          <ChevronUp className="h-3.5 w-3.5" />
        </button>
      </div>
    );
  }

  return (
    <div className="h-32 shrink-0 bg-slate-950 border-t border-slate-800 flex flex-col z-30 select-none shadow-2xl">
      {/* 顶栏控制 */}
      <div className="h-7 px-4 bg-slate-900 border-b border-slate-800/80 flex items-center justify-between text-xs text-slate-400">
        <div className="flex items-center gap-3">
          <button
            onClick={() => setIsPlaying(!isPlaying)}
            className="flex items-center gap-1.5 text-slate-200 hover:text-white font-semibold text-[11px]"
          >
            {isPlaying ? <Pause className="h-3 w-3 text-amber-400" /> : <Play className="h-3 w-3 text-amber-400" />}
            <span>{isPlaying ? "暂停" : "连贯试听"}</span>
          </button>
          <div className="h-3 w-px bg-slate-800" />
          <span className="font-mono text-[11px] text-amber-400">
            {formatTimecode(playheadSec)} / {formatTimecode(totalDuration)}
          </span>
          <span className="text-[10px] text-slate-500 hidden sm:inline">
            (点击镜头卡片，画布视口自动飞向该分镜)
          </span>
        </div>

        <div className="flex items-center gap-3 text-[11px]">
          <button
            onClick={onOpenPreviewModal}
            className="flex items-center gap-1 text-emerald-400 hover:text-emerald-300 font-medium"
          >
            <Maximize2 className="h-3 w-3" />
            <span>全屏成片预览</span>
          </button>
          {onExportDraft && (
            <button
              onClick={onExportDraft}
              className="flex items-center gap-1 text-indigo-400 hover:text-indigo-300"
            >
              <Sparkles className="h-3 w-3" />
              <span>剪映草稿</span>
            </button>
          )}
          {onToggleCollapse && (
            <button
              onClick={onToggleCollapse}
              className="text-slate-400 hover:text-slate-200 flex items-center gap-0.5"
              title="收起时间线"
            >
              <ChevronDown className="h-3.5 w-3.5" />
            </button>
          )}
        </div>
      </div>

      {/* 多轨内容容器 */}
      <div className="flex-1 overflow-x-auto p-2 relative flex flex-col justify-center space-y-1.5 bg-slate-950/60">
        {/* 轨 1: 视频片段轨 (VIDEO) */}
        <div className="flex items-center gap-1 h-8">
          <span className="w-12 text-[10px] text-slate-500 font-mono text-right pr-2 shrink-0 flex items-center justify-end gap-1">
            <Film className="h-3 w-3 text-amber-500" />
            <span>VID</span>
          </span>
          <div className="flex items-center gap-1.5 flex-1 min-w-max">
            {shots.map((shot) => {
              const isSelected = selectedShotId === shot.id;
              let borderCol = "border-slate-700 bg-slate-900";
              let textCol = "text-slate-300";

              if (shot.status === "ready") {
                borderCol = isSelected ? "border-amber-400 bg-amber-500/20" : "border-amber-500/60 bg-amber-950/30";
                textCol = "text-amber-200";
              } else if (shot.status === "running") {
                borderCol = "border-indigo-500 bg-indigo-950/40 animate-pulse";
                textCol = "text-indigo-200";
              } else if (shot.status === "failed") {
                borderCol = "border-rose-500 bg-rose-950/40";
                textCol = "text-rose-300";
              }

              // 宽度按时长比例计算
              const widthPx = Math.max(Math.round((shot.durationSec || 4) * 36), 110);

              return (
                <div
                  key={shot.id}
                  onClick={() => onSelectShot(shot.id)}
                  style={{ width: `${widthPx}px` }}
                  className={`h-7 rounded-md border px-2 flex items-center justify-between text-xs cursor-pointer transition shadow-sm hover:scale-[1.02] shrink-0 ${borderCol}`}
                  title={`点击在画布定位分镜 #${shot.index}`}
                >
                  <div className="truncate font-medium flex items-center gap-1">
                    <span className="text-[10px] font-mono text-amber-400 font-bold">#{shot.index}</span>
                    <span className={`text-[11px] truncate ${textCol}`}>{shot.title || "分镜片段"}</span>
                  </div>
                  <span className="text-[9px] font-mono text-slate-400 shrink-0 ml-1">
                    {(shot.durationSec || 4).toFixed(1)}s
                  </span>
                </div>
              );
            })}
          </div>
        </div>

        {/* 轨 2: 语音台词与配音轨 (AUDIO) */}
        <div className="flex items-center gap-1 h-6">
          <span className="w-12 text-[10px] text-slate-500 font-mono text-right pr-2 shrink-0 flex items-center justify-end gap-1">
            <Mic className="h-3 w-3 text-purple-400" />
            <span>AUD</span>
          </span>
          <div className="flex items-center gap-1.5 flex-1 min-w-max">
            {shots.map((shot) => {
              const widthPx = Math.max(Math.round((shot.durationSec || 4) * 36), 110);
              return (
                <div
                  key={`aud-${shot.id}`}
                  onClick={() => onSelectShot(shot.id)}
                  style={{ width: `${widthPx}px` }}
                  className="h-5 rounded bg-purple-950/40 border border-purple-800/40 px-2 flex items-center text-[10px] text-purple-300 truncate cursor-pointer hover:border-purple-500 transition shrink-0"
                >
                  <span className="truncate">
                    {shot.speakerName ? `${shot.speakerName}: ` : ""}
                    {shot.voiceoverText || "（环境无台词）"}
                  </span>
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
};
export default CinematicTimeline;
