import { Handle, Position, type NodeProps, type NodeTypes } from "@xyflow/react";
import {
  CheckCircle2,
  Clapperboard,
  Film,
  FileAudio,
  GitBranch,
  Image as ImageIcon,
  Layers,
  Loader2,
  MapPin,
  Mic,
  Play,
  RefreshCw,
  ScrollText,
  Sparkles,
  User,
  Video,
  AlertCircle,
} from "lucide-react";
import React from "react";
import {
  getActiveShotBranch,
  type AudioImportNodeData,
  type CharacterNodeData,
  type ComposerNodeData,
  type SceneNodeData,
  type ScriptNodeData,
  type ShotNodeData,
} from "../../lib/dramaPresets";

export interface DramaNodeActionCallbacks {
  isPipelineRunning?: boolean;
  onRunBreakdown: () => void;
  onRunPipeline: () => void;
  onGenerateCharacter: (nodeId: string) => void;
  onGenerateScene: (nodeId: string) => void;
  onGenerateShotAudio: (nodeId: string) => void;
  onGenerateShotKeyframe: (nodeId: string) => void;
  onGenerateShotVideo: (nodeId: string) => void;
  onAddShotBranch: (nodeId: string) => void;
  onSwitchShotBranch: (nodeId: string, versionId: string) => void;
  onTranscribeAudio: (nodeId: string) => void;
  onOpenPlayer: () => void;
  onExportVideo: () => void;
  onExportZip: () => void;
}

const NodeActionsContext = React.createContext<DramaNodeActionCallbacks | null>(null);

export const DramaNodeActionsProvider = NodeActionsContext.Provider;

function useNodeActions() {
  const ctx = React.useContext(NodeActionsContext);
  if (!ctx) {
    throw new Error("DramaNodeActionsProvider missing");
  }
  return ctx;
}

function StatusBadge({ status, label }: { status: string; label?: string }) {
  if (status === "running") {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-amber-500/15 px-2 py-0.5 text-[10px] font-medium text-amber-600 dark:text-amber-400">
        <Loader2 className="h-3 w-3 animate-spin" />
        {label || "生成中"}
      </span>
    );
  }
  if (status === "ready") {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-emerald-500/15 px-2 py-0.5 text-[10px] font-medium text-emerald-600 dark:text-emerald-400">
        <CheckCircle2 className="h-3 w-3" />
        {label || "就绪"}
      </span>
    );
  }
  if (status === "ken_burns") {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-sky-500/15 px-2 py-0.5 text-[10px] font-medium text-sky-600 dark:text-sky-400">
        <Film className="h-3 w-3" />
        {label || "Ken Burns"}
      </span>
    );
  }
  if (status === "stale") {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-orange-500/15 px-2 py-0.5 text-[10px] font-medium text-orange-600 dark:text-orange-400">
        <RefreshCw className="h-3 w-3" />
        {label || "待更新"}
      </span>
    );
  }
  if (status === "error") {
    return (
      <span className="inline-flex items-center gap-1 rounded-full bg-rose-500/15 px-2 py-0.5 text-[10px] font-medium text-rose-600 dark:text-rose-400">
        <AlertCircle className="h-3 w-3" />
        {label || "失败"}
      </span>
    );
  }
  return (
    <span className="inline-flex items-center rounded-full bg-slate-500/10 px-2 py-0.5 text-[10px] text-[var(--text-muted)]">
      {label || "待生成"}
    </span>
  );
}

const BaseCard: React.FC<{
  selected?: boolean;
  accentColor: string;
  widthClass?: string;
  children: React.ReactNode;
}> = ({ selected, accentColor, widthClass = "w-[300px]", children }) => (
  <div
    className={`${widthClass} rounded-xl border bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-md transition-all ${
      selected
        ? "ring-2 ring-indigo-500 border-indigo-500 shadow-indigo-500/15"
        : "border-[var(--border-default)] hover:border-[var(--border-strong)]"
    }`}
  >
    <div className="h-1.5 w-full rounded-t-xl" style={{ backgroundColor: accentColor }} />
    <div className="p-3">{children}</div>
  </div>
);

export const ScriptCanvasNode: React.FC<NodeProps> = ({ data, selected }) => {
  const d = data as ScriptNodeData;
  const actions = useNodeActions();
  return (
    <BaseCard selected={selected} accentColor="#4f46e5" widthClass="w-[320px]">
      <Handle type="target" position={Position.Left} className="!h-3 !w-3 !bg-indigo-500" />
      <Handle type="source" position={Position.Right} className="!h-3 !w-3 !bg-indigo-500" />
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <div className="rounded-lg bg-indigo-500/10 p-1.5 text-indigo-500">
            <ScrollText className="h-4 w-4" />
          </div>
          <div className="min-w-0">
            <div className="truncate text-xs font-semibold">{d.title || "剧本主控节点"}</div>
            <div className="text-[10px] text-[var(--text-muted)]">
              {d.genre} · {d.aspectRatio}
            </div>
          </div>
        </div>
        <StatusBadge status={d.status} />
      </div>

      <p className="mt-2 line-clamp-3 rounded-lg bg-[var(--bg-muted)] p-2 text-[11px] leading-relaxed text-[var(--text-secondary)]">
        {d.outline || "点击右侧属性面板输入故事大纲..."}
      </p>

      {d.error && <div className="mt-1.5 truncate text-[10px] text-rose-500">{d.error}</div>}

      <div className="nodrag mt-2.5 flex items-center gap-1.5">
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            actions.onRunBreakdown();
          }}
          disabled={actions.isPipelineRunning || d.status === "running"}
          className="flex-1 inline-flex items-center justify-center gap-1 rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] px-2 py-1.5 text-[11px] font-medium hover:bg-[var(--bg-surface-hover)] disabled:cursor-not-allowed disabled:opacity-50"
        >
          <Sparkles className="h-3 w-3 text-indigo-500" />
          AI 拆解分镜
        </button>
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            actions.onRunPipeline();
          }}
          disabled={actions.isPipelineRunning || d.status === "running"}
          className="flex-1 inline-flex items-center justify-center gap-1 rounded-lg bg-indigo-600 px-2 py-1.5 text-[11px] font-medium text-white hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-60"
        >
          {actions.isPipelineRunning ? (
            <>
              <Loader2 className="h-3 w-3 animate-spin" />
              运行中...
            </>
          ) : (
            <>
              <Play className="h-3 w-3" />
              一键成片
            </>
          )}
        </button>
      </div>
    </BaseCard>
  );
};

export const CharacterCanvasNode: React.FC<NodeProps> = ({ id, data, selected }) => {
  const d = data as CharacterNodeData;
  const actions = useNodeActions();
  return (
    <BaseCard selected={selected} accentColor={d.avatarColor || "#8b5cf6"} widthClass="w-[285px]">
      <Handle type="target" position={Position.Left} className="!h-2.5 !w-2.5 !bg-violet-500" />
      <Handle type="source" position={Position.Right} className="!h-2.5 !w-2.5 !bg-violet-500" />
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <div
            className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full text-xs font-bold text-white"
            style={{ backgroundColor: d.avatarColor || "#8b5cf6" }}
          >
            {d.name?.slice(0, 1) || <User className="h-3.5 w-3.5" />}
          </div>
          <div className="min-w-0">
            <div className="truncate text-xs font-semibold">{d.name}</div>
            <div className="text-[10px] text-[var(--text-muted)]">{d.role} · 角色视觉/音色锚点</div>
          </div>
        </div>
        <StatusBadge status={d.status} />
      </div>

      <div className="mt-2 flex gap-2">
        <div className="h-16 w-16 shrink-0 overflow-hidden rounded-lg border border-[var(--border-default)] bg-[var(--bg-muted)] flex items-center justify-center">
          {d.portraitUrl ? (
            <img src={d.portraitUrl} alt={d.name} className="h-full w-full object-cover" />
          ) : (
            <User className="h-5 w-5 text-[var(--text-muted)]" />
          )}
        </div>
        <div className="min-w-0 flex-1 space-y-1">
          <p className="line-clamp-2 text-[10px] leading-snug text-[var(--text-secondary)]">
            {d.appearanceBible}
          </p>
          <div className="flex items-center gap-1 text-[10px] text-indigo-500 truncate">
            <Mic className="h-3 w-3 shrink-0" />
            <span className="truncate">{d.voiceName || d.voiceId}</span>
          </div>
        </div>
      </div>

      {d.error && <div className="mt-1 truncate text-[10px] text-rose-500">{d.error}</div>}

      <div className="nodrag mt-2 flex justify-end">
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            actions.onGenerateCharacter(id);
          }}
          disabled={d.status === "running"}
          className="inline-flex items-center gap-1 rounded-md border border-[var(--border-default)] bg-[var(--bg-elevated)] px-2 py-1 text-[10px] font-medium hover:bg-[var(--bg-surface-hover)] disabled:opacity-50"
        >
          <RefreshCw className={`h-3 w-3 ${d.status === "running" ? "animate-spin" : ""}`} />
          {d.portraitUrl ? "重绘定妆照" : "生成定妆照"}
        </button>
      </div>
    </BaseCard>
  );
};

export const SceneCanvasNode: React.FC<NodeProps> = ({ id, data, selected }) => {
  const d = data as SceneNodeData;
  const actions = useNodeActions();
  return (
    <BaseCard selected={selected} accentColor="#0ea5e9" widthClass="w-[285px]">
      <Handle type="target" position={Position.Left} className="!h-2.5 !w-2.5 !bg-sky-500" />
      <Handle type="source" position={Position.Right} className="!h-2.5 !w-2.5 !bg-sky-500" />
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-1.5 min-w-0">
          <MapPin className="h-3.5 w-3.5 text-sky-500 shrink-0" />
          <span className="truncate text-xs font-semibold">{d.name}</span>
        </div>
        <StatusBadge status={d.status} />
      </div>

      <div className="mt-2 h-20 w-full overflow-hidden rounded-lg border border-[var(--border-default)] bg-[var(--bg-muted)] flex items-center justify-center">
        {d.imageUrl ? (
          <img src={d.imageUrl} alt={d.name} className="h-full w-full object-cover" />
        ) : (
          <div className="text-[10px] text-[var(--text-muted)] px-2 text-center line-clamp-2">
            {d.environmentPrompt}
          </div>
        )}
      </div>

      {d.error && <div className="mt-1 truncate text-[10px] text-rose-500">{d.error}</div>}

      <div className="nodrag mt-2 flex items-center justify-between gap-2">
        <span className="truncate text-[10px] text-[var(--text-muted)]">{d.lightingStyle}</span>
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            actions.onGenerateScene(id);
          }}
          disabled={d.status === "running"}
          className="shrink-0 inline-flex items-center gap-1 rounded-md border border-[var(--border-default)] bg-[var(--bg-elevated)] px-2 py-1 text-[10px] font-medium hover:bg-[var(--bg-surface-hover)] disabled:opacity-50"
        >
          <ImageIcon className="h-3 w-3 text-sky-500" />
          {d.imageUrl ? "重绘空镜" : "生成空镜"}
        </button>
      </div>
    </BaseCard>
  );
};

export const ShotCanvasNode: React.FC<NodeProps> = ({ id, data, selected }) => {
  const d = data as ShotNodeData;
  const actions = useNodeActions();
  const active = getActiveShotBranch(d);

  return (
    <BaseCard selected={selected} accentColor="#f59e0b" widthClass="w-[335px]">
      <Handle type="target" position={Position.Left} className="!h-2.5 !w-2.5 !bg-amber-500" />
      <Handle type="source" position={Position.Right} className="!h-2.5 !w-2.5 !bg-emerald-500" />
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-1.5 min-w-0">
          <span className="inline-flex h-5 items-center rounded bg-amber-500/15 px-1.5 text-[10px] font-bold text-amber-600 dark:text-amber-400">
            #{d.index}
          </span>
          <span className="truncate text-xs font-semibold">{d.shotType}</span>
          <span className="truncate text-[10px] text-[var(--text-muted)]">· {d.cameraMove.split(" ")[0]}</span>
        </div>
        <span className="text-[10px] font-mono text-[var(--text-muted)]">
          {(d.actualAudioDuration || d.estimatedDuration).toFixed(1)}s
        </span>
      </div>

      {/* A/B 多版本平行分支标签 */}
      <div className="nodrag mt-2 flex items-center gap-1 overflow-x-auto pb-0.5">
        {d.versions.map((ver) => (
          <button
            key={ver.id}
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              actions.onSwitchShotBranch(id, ver.id);
            }}
            className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-medium transition ${
              ver.id === active.id
                ? "bg-amber-500 text-white"
                : "bg-[var(--bg-muted)] text-[var(--text-secondary)] hover:bg-[var(--bg-surface-hover)]"
            }`}
          >
            <GitBranch className="h-2.5 w-2.5" />
            {ver.label}
          </button>
        ))}
        <button
          type="button"
          title="分叉生成对比版本"
          onClick={(e) => {
            e.stopPropagation();
            actions.onAddShotBranch(id);
          }}
          className="rounded border border-dashed border-[var(--border-strong)] px-1.5 py-0.5 text-[10px] text-[var(--text-muted)] hover:text-[var(--text-primary)]"
        >
          + 分支
        </button>
      </div>

      {/* 预览画框 */}
      <div className="mt-2 relative h-28 w-full overflow-hidden rounded-lg border border-[var(--border-default)] bg-zinc-950/90 flex items-center justify-center">
        {active.renderMode === "video" && active.videoUrl ? (
          <video
            src={active.videoUrl}
            poster={active.imageUrl}
            className="h-full w-full object-cover"
            muted
            loop
            playsInline
            onMouseEnter={(ev) => ev.currentTarget.play().catch(() => {})}
            onMouseLeave={(ev) => ev.currentTarget.pause()}
          />
        ) : active.imageUrl ? (
          <img src={active.imageUrl} alt={`Shot ${d.index}`} className="h-full w-full object-cover" />
        ) : (
          <div className="px-3 text-center text-[10px] text-zinc-400 line-clamp-3">{d.sceneAction}</div>
        )}
        <div className="absolute top-1.5 right-1.5 flex items-center gap-1">
          <StatusBadge status={active.keyframeStatus} label="首帧" />
          <StatusBadge status={active.audioStatus} label="音轨" />
          <StatusBadge status={active.videoStatus} label={active.videoStatus === "ken_burns" ? "运镜" : "视频"} />
        </div>
      </div>

      <p className="mt-2 line-clamp-2 text-[11px] text-[var(--text-secondary)] leading-snug">
        “{d.dialogue || d.sceneAction}”
      </p>

      {(active.error || active.statusNote) && (
        <div className={`mt-1 truncate text-[10px] ${active.error ? "text-rose-500" : "text-sky-500"}`}>
          {active.error || active.statusNote}
        </div>
      )}

      {/* 单节点局部重生成快捷按钮 */}
      <div className="nodrag mt-2 grid grid-cols-3 gap-1">
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            actions.onGenerateShotAudio(id);
          }}
          disabled={active.audioStatus === "running"}
          className="inline-flex items-center justify-center gap-1 rounded-md border border-[var(--border-default)] bg-[var(--bg-elevated)] py-1 text-[10px] hover:bg-[var(--bg-surface-hover)] disabled:opacity-50"
        >
          <Mic className="h-3 w-3 text-emerald-500" />
          配音
        </button>
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            actions.onGenerateShotKeyframe(id);
          }}
          disabled={active.keyframeStatus === "running"}
          className="inline-flex items-center justify-center gap-1 rounded-md border border-[var(--border-default)] bg-[var(--bg-elevated)] py-1 text-[10px] hover:bg-[var(--bg-surface-hover)] disabled:opacity-50"
        >
          <ImageIcon className="h-3 w-3 text-indigo-500" />
          首帧
        </button>
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            actions.onGenerateShotVideo(id);
          }}
          disabled={active.videoStatus === "running"}
          className="inline-flex items-center justify-center gap-1 rounded-md border border-[var(--border-default)] bg-[var(--bg-elevated)] py-1 text-[10px] hover:bg-[var(--bg-surface-hover)] disabled:opacity-50"
        >
          <Video className="h-3 w-3 text-amber-500" />
          出片
        </button>
      </div>
    </BaseCard>
  );
};

export const AudioImportCanvasNode: React.FC<NodeProps> = ({ id, data, selected }) => {
  const d = data as AudioImportNodeData;
  const actions = useNodeActions();
  return (
    <BaseCard selected={selected} accentColor="#14b8a6" widthClass="w-[285px]">
      <Handle type="source" position={Position.Right} className="!h-2.5 !w-2.5 !bg-teal-500" />
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-1.5 min-w-0">
          <FileAudio className="h-4 w-4 text-teal-500 shrink-0" />
          <span className="truncate text-xs font-semibold">{d.title}</span>
        </div>
        <StatusBadge status={d.status} />
      </div>

      {d.sourceAudioUrl && (
        <audio src={d.sourceAudioUrl} controls className="nodrag mt-2 h-8 w-full" />
      )}

      {d.transcript && (
        <p className="mt-1.5 line-clamp-2 rounded bg-[var(--bg-muted)] p-1.5 text-[10px] text-[var(--text-secondary)]">
          {d.transcript}
        </p>
      )}
      {d.error && <div className="mt-1 truncate text-[10px] text-rose-500">{d.error}</div>}

      <div className="nodrag mt-2 flex justify-end">
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            actions.onTranscribeAudio(id);
          }}
          disabled={!d.sourceAudioUrl || d.status === "running"}
          className="inline-flex items-center gap-1 rounded-md border border-[var(--border-default)] bg-[var(--bg-elevated)] px-2 py-1 text-[10px] font-medium hover:bg-[var(--bg-surface-hover)] disabled:opacity-50"
        >
          <Sparkles className="h-3 w-3 text-teal-500" />
          Whisper 转写写入剧本
        </button>
      </div>
    </BaseCard>
  );
};

export const ComposerCanvasNode: React.FC<NodeProps> = ({ data, selected }) => {
  const d = data as ComposerNodeData;
  const actions = useNodeActions();
  return (
    <BaseCard selected={selected} accentColor="#10b981" widthClass="w-[310px]">
      <Handle type="target" position={Position.Left} className="!h-3 !w-3 !bg-emerald-500" />
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <div className="rounded-lg bg-emerald-500/15 p-1.5 text-emerald-500">
            <Clapperboard className="h-4 w-4" />
          </div>
          <div className="min-w-0">
            <div className="truncate text-xs font-semibold">{d.title || "时间线成片合成台"}</div>
            <div className="text-[10px] text-[var(--text-muted)]">字幕烧录 · BGM混音 · ZIP导出</div>
          </div>
        </div>
        <StatusBadge status={d.status} />
      </div>

      {d.exportProgress > 0 && d.exportProgress < 100 && (
        <div className="mt-2">
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-[var(--bg-muted)]">
            <div className="h-full bg-emerald-500 transition-all" style={{ width: `${d.exportProgress}%` }} />
          </div>
          <div className="mt-1 truncate text-[10px] text-[var(--text-muted)]">{d.exportStageText}</div>
        </div>
      )}

      {d.error && <div className="mt-1.5 truncate text-[10px] text-rose-500">{d.error}</div>}

      <div className="nodrag mt-3 space-y-1.5">
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            actions.onOpenPlayer();
          }}
          className="w-full inline-flex items-center justify-center gap-1.5 rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-emerald-500"
        >
          <Play className="h-3.5 w-3.5" />
          多轨连播监视器
        </button>
        <div className="grid grid-cols-2 gap-1.5">
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              actions.onExportVideo();
            }}
            className="inline-flex items-center justify-center gap-1 rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] py-1.5 text-[11px] font-medium hover:bg-[var(--bg-surface-hover)]"
          >
            <Film className="h-3 w-3 text-emerald-500" />
            合成连续成片
          </button>
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              actions.onExportZip();
            }}
            className="inline-flex items-center justify-center gap-1 rounded-lg border border-[var(--border-default)] bg-[var(--bg-elevated)] py-1.5 text-[11px] font-medium hover:bg-[var(--bg-surface-hover)]"
          >
            <Layers className="h-3 w-3 text-sky-500" />
            打包完整 ZIP
          </button>
        </div>
      </div>
    </BaseCard>
  );
};

export const DRAMA_NODE_TYPES: NodeTypes = {
  script: ScriptCanvasNode,
  character: CharacterCanvasNode,
  scene: SceneCanvasNode,
  shot: ShotCanvasNode,
  audioImport: AudioImportCanvasNode,
  composer: ComposerCanvasNode,
};
