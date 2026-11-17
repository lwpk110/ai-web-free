import {
  Background,
  Controls,
  MiniMap,
  ReactFlow,
  ReactFlowProvider,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Clapperboard,
  Clock,
  Download,
  Film,
  FileAudio,
  FolderPlus,
  Globe,
  Image as ImageIcon,
  KeyRound,
  LayoutGrid,
  ListChecks,
  Loader2,
  Lock,
  MapPin,
  Maximize2,
  Mic,
  Minimize2,
  Network,
  PauseCircle,
  Play,
  RefreshCw,
  ScrollText,
  Settings2,
  ShieldAlert,
  Sparkles,
  Terminal,
  Trash2,
  UserPlus,
  Workflow,
} from "lucide-react";
import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { fetchVoices, type DramaRoutingPolicy, type VoiceDescriptor } from "../api/client";
import { DramaAssetDrawerModal } from "../components/drama/DramaAssetDrawerModal";
import { DramaNodeInspector } from "../components/drama/DramaNodeInspector";
import { DramaSettingsModal } from "../components/drama/DramaSettingsModal";
import { DRAMA_NODE_TYPES, DramaNodeActionsProvider } from "../components/drama/DramaNodes";
import { FlowingPulseEdge } from "../components/drama/FlowingPulseEdge";
import { CinematicTimeline, type TimelineShotItem } from "../components/drama/CinematicTimeline";
import { ChronoEventTracker } from "../components/drama/ChronoEventTracker";
import { CloudKeyConfigModal } from "../components/CloudKeyConfigModal";
import { DramaTimelinePlayerModal } from "../components/drama/DramaTimelinePlayerModal";
import { DramaWizardView } from "../components/drama/DramaWizardView";
import { consumePendingDramaImports, DRAMA_IMPORT_EVENT, type DramaCanvasImportPayload } from "../lib/dramaCanvasBridge";
import {
  transcribeAudioImportToScript,
} from "../lib/dramaClientActions";
import {
  DRAMA_PRESETS,
  getActiveShotBranch,
  type CharacterNodeData,
  type ComposerNodeData,
  type DramaFlowNode,
  type SceneNodeData,
  type ScriptNodeData,
  type ShotNodeData,
} from "../lib/dramaPresets";
import {
  exportDramaBundleZip,
  exportDramaToSingleVideo,
  type ComposerShotItem,
} from "../lib/dramaVideoComposer";
import { useDramaCanvasStore } from "../stores/dramaCanvasStore";

const DRAMA_EDGE_TYPES = {
  flowingPulse: FlowingPulseEdge,
};

function downloadBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 10_000);
}

function assertShotsReadyForExport(nodes: DramaFlowNode[]) {
  const shots = (nodes.filter((n) => n.type === "shot") as Array<DramaFlowNode & { data: ShotNodeData }>).sort(
    (a, b) => a.data.index - b.data.index,
  );
  const missingVisuals: number[] = [];
  const missingAudio: number[] = [];

  for (const shot of shots) {
    const branch = getActiveShotBranch(shot.data);
    const hasVideo = branch.videoStatus === "ready" && branch.renderMode === "video" && Boolean(branch.videoUrl);
    const hasKeyframe = branch.keyframeStatus === "ready" && Boolean(branch.imageUrl);
    if (!hasVideo && !hasKeyframe) missingVisuals.push(shot.data.index);
    if (shot.data.dialogue.trim() && (branch.audioStatus !== "ready" || !branch.audioUrl)) {
      missingAudio.push(shot.data.index);
    }
  }

  const issues = [
    missingVisuals.length > 0 ? `缺少有效画面：第 ${missingVisuals.join("、")} 镜` : "",
    missingAudio.length > 0 ? `缺少有效对白音轨：第 ${missingAudio.join("、")} 镜` : "",
  ].filter(Boolean);
  if (issues.length > 0) {
    throw new Error(`无法导出完整成片。${issues.join("；")}。请重试对应素材后再导出。`);
  }
}

export const ShortDramaStudio: React.FC = () => {
  const {
    projectId,
    backendProject,
    activeRunId,
    runLogs,
    runSummary,
    runsHistory,
    selectedLogRunId,
    logPanelOpen,
    nodes,
    edges,
    selectedNodeId,
    viewMode,
    checkpointAfterCast,
    checkpointAfterKeyframes,
    pipelineState,
    pipelineStageText,
    capabilities,
    capabilitiesLoading,
    projects,
    isSaving,
    syncStatus,
    initFromPersistence,
    refreshCapabilities,
    loadProject,
    createNewProject,
    removeProject,
    setSelectedNodeId,
    setViewMode,
    setLogPanelOpen,
    selectRunLogView,
    setCheckpointAfterCast,
    setCheckpointAfterKeyframes,
    onNodesChange,
    onEdgesChange,
    onConnect,
    autoLayout,
    updateNodeData,
    updateScriptNode,
    addNode,
    removeNode,
    addShotVersionBranch,
    switchShotVersionBranch,
    importCrossStudioAsset,
  } = useDramaCanvasStore();

  const [voices, setVoices] = useState<VoiceDescriptor[]>([]);
  const [cloudKeyModalOpen, setCloudKeyModalOpen] = useState(false);
  const [settingsModalOpen, setSettingsModalOpen] = useState(false);
  const [assetDrawer, setAssetDrawer] = useState<"gallery" | "audio" | "prompts" | null>(null);
  const [playerOpen, setPlayerOpen] = useState(false);
  const [themeMode, setThemeMode] = useState<"light" | "dark">(() =>
    document.documentElement.dataset.theme === "dark" ? "dark" : "light",
  );
  const abortRef = useRef<AbortController | null>(null);
  const lastExportBlobRef = useRef<{ blob: Blob; ext: string } | null>(null);
  const [isCanvasMaximized, setIsCanvasMaximized] = useState(false);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && isCanvasMaximized) {
        setIsCanvasMaximized(false);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [isCanvasMaximized]);

  // 初始化双写持久化、探测能力 + 导入跨工坊素材（活跃后台 Run 由 store 自动重连 SSE）
  useEffect(() => {
    void initFromPersistence().then(() => {
      const pending = consumePendingDramaImports();
      pending.forEach((p) => void importCrossStudioAsset(p));
    });
    fetchVoices()
      .then((res) => setVoices(res.voices || []))
      .catch(() => {});

    const onImportEvent = (ev: Event) => {
      const detail = (ev as CustomEvent<DramaCanvasImportPayload>).detail;
      if (detail) {
        consumePendingDramaImports();
        void importCrossStudioAsset(detail);
      }
    };
    window.addEventListener(DRAMA_IMPORT_EVENT, onImportEvent);

    const observer = new MutationObserver(() => {
      setThemeMode(document.documentElement.dataset.theme === "dark" ? "dark" : "light");
    });
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });

    return () => {
      window.removeEventListener(DRAMA_IMPORT_EVENT, onImportEvent);
      observer.disconnect();
      abortRef.current?.abort();
    };
  }, [initFromPersistence, importCrossStudioAsset]);

  const scriptNode = nodes.find((n) => n.type === "script") as (DramaFlowNode & { data: ScriptNodeData }) | undefined;
  const composerNode = nodes.find((n) => n.type === "composer") as
    | (DramaFlowNode & { data: ComposerNodeData })
    | undefined;
  const selectedNode = nodes.find((n) => n.id === selectedNodeId);

  const composerShots: ComposerShotItem[] = useMemo(() => {
    return (nodes.filter((n) => n.type === "shot") as Array<DramaFlowNode & { data: ShotNodeData }>)
      .sort((a, b) => a.data.index - b.data.index)
      .map((n) => {
        const br = getActiveShotBranch(n.data);
        return {
          id: n.id,
          index: n.data.index,
          title: `第 ${n.data.index} 镜`,
          dialogue: n.data.dialogue,
          visualPrompt: n.data.sceneAction,
          imageUrl: br.imageUrl,
          videoUrl: br.videoUrl,
          audioUrl: br.audioUrl,
          durationSec: Math.max(1.5, n.data.actualAudioDuration || n.data.estimatedDuration || 4.5),
          renderMode: br.renderMode,
          transition: n.data.transition || "crossfade",
          cameraMotion: n.data.cameraMove,
        };
      });
  }, [nodes]);

  const handleRunBreakdown = useCallback(() => {
    void useDramaCanvasStore
      .getState()
      .runBackendPipeline({
        targets: ["script.*", "bible.*", "storyboard.*"],
        force: ["script.*", "bible.*", "storyboard.*"],
      })
      .catch((err) => {
        useDramaCanvasStore
          .getState()
          .setPipelineState("error", err instanceof Error ? err.message : String(err));
      });
  }, []);

  const handleRunPipeline = useCallback((
    resumeFrom: "start" | "after_cast" | "after_keyframes" = "start",
    breakScript = resumeFrom === "start",
  ) => {
    void useDramaCanvasStore
      .getState()
      .runBackendPipeline(
        breakScript
          ? { targets: ["*"] }
          : undefined,
      )
      .catch((err) => {
        useDramaCanvasStore
          .getState()
          .setPipelineState("error", err instanceof Error ? err.message : String(err));
      });
  }, []);

  const handleExportSingleVideo = useCallback(async () => {
    if (!composerNode || !scriptNode) return;
    try {
      assertShotsReadyForExport(nodes);
      updateNodeData<ComposerNodeData>(composerNode.id, {
        status: "running",
        exportProgress: 5,
        exportStageText: "正在初始化纯前端合流渲染器...",
        error: undefined,
      });
      const result = await exportDramaToSingleVideo({
        title: scriptNode.data.title,
        aspectRatio: scriptNode.data.aspectRatio,
        shots: composerShots,
        bgmUrl: composerNode.data.bgmUrl,
        bgmVolume: composerNode.data.bgmVolume,
        showSubtitles: composerNode.data.showSubtitles,
        onProgress: (pct, stage) => {
          updateNodeData<ComposerNodeData>(composerNode.id, {
            exportProgress: pct,
            exportStageText: stage,
          });
        },
      });
      lastExportBlobRef.current = { blob: result.blob, ext: result.ext };
      const url = URL.createObjectURL(result.blob);
      updateNodeData<ComposerNodeData>(composerNode.id, {
        status: "ready",
        exportProgress: 100,
        exportStageText: `渲染完成 (${result.durationSec.toFixed(1)}s)`,
        exportedVideoUrl: url,
        exportedExt: result.ext,
        exportedFilename: result.filename,
      });
      downloadBlob(result.blob, result.filename);
    } catch (err) {
      updateNodeData<ComposerNodeData>(composerNode.id, {
        status: "error",
        exportProgress: 0,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }, [composerNode, composerShots, nodes, scriptNode, updateNodeData]);

  const handleExportZip = useCallback(async () => {
    if (!composerNode || !scriptNode) return;
    try {
      assertShotsReadyForExport(nodes);
      updateNodeData<ComposerNodeData>(composerNode.id, {
        status: "running",
        exportProgress: 8,
        exportStageText: "正在汇集真实画面、语音与剪映草稿...",
        error: undefined,
      });
      const chars = (nodes.filter((n) => n.type === "character") as Array<DramaFlowNode & { data: CharacterNodeData }>).map(
        (c) => ({ name: c.data.name, portraitUrl: c.data.portraitUrl }),
      );
      const scns = (nodes.filter((n) => n.type === "scene") as Array<DramaFlowNode & { data: SceneNodeData }>).map(
        (s) => ({ name: s.data.name, imageUrl: s.data.imageUrl }),
      );
      const out = await exportDramaBundleZip({
        title: scriptNode.data.title,
        aspectRatio: scriptNode.data.aspectRatio,
        shots: composerShots,
        characters: chars,
        scenes: scns,
        projectJson: JSON.stringify({ projectId, nodes, edges }, null, 2),
        renderedVideo: lastExportBlobRef.current || undefined,
        onProgress: (pct, stage) => {
          updateNodeData<ComposerNodeData>(composerNode.id, {
            exportProgress: pct,
            exportStageText: stage,
          });
        },
      });
      updateNodeData<ComposerNodeData>(composerNode.id, {
        status: "ready",
        exportProgress: 100,
        exportStageText: `ZIP 工程归档就绪 (${out.filename})`,
      });
      downloadBlob(out.blob, out.filename);
    } catch (err) {
      updateNodeData<ComposerNodeData>(composerNode.id, {
        status: "error",
        exportProgress: 0,
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }, [composerNode, composerShots, edges, nodes, projectId, scriptNode, updateNodeData]);

  const canvasStats = useMemo(() => {
    const map = backendProject?.nodes || {};
    const vals = Object.values(map);
    let ready = 0;
    let running = 0;
    let stale = 0;
    let failed = 0;
    let locked = 0;
    let pending = 0;
    for (const v of vals) {
      if (v.locked) locked++;
      if (v.status === "ready" || v.status === "degraded") ready++;
      else if (v.status === "running") running++;
      else if (v.status === "stale") stale++;
      else if (v.status === "failed") failed++;
      else pending++;
    }
    return { total: vals.length, ready, running, stale, failed, locked, pending };
  }, [backendProject]);

  const [bottomDrawerMode, setBottomDrawerMode] = useState<"timeline" | "chrono" | "logs">("timeline");
  const [bottomDrawerCollapsed, setBottomDrawerCollapsed] = useState(false);
  const reactFlowInstanceRef = useRef<any>(null);

  const focusNodeByKey = useCallback((nodeKey?: string) => {
    if (!nodeKey) return;
    let target = nodes.find((n) => {
      if (n.type === "character") return n.data.charId === nodeKey || `image.portrait:${n.data.charId}` === nodeKey;
      if (n.type === "scene") return n.data.sceneId === nodeKey || `image.scene:${n.data.sceneId}` === nodeKey;
      if (n.type === "shot") {
        return (
          n.data.shotId === nodeKey ||
          `image.keyframe:${n.data.shotId}` === nodeKey ||
          `video.clip:${n.data.shotId}` === nodeKey ||
          `tts.line:${n.data.shotId}` === nodeKey ||
          `tts.speech:${n.data.shotId}` === nodeKey ||
          n.id === nodeKey
        );
      }
      if (n.type === "composer") return nodeKey.startsWith("timeline.") || nodeKey.startsWith("render.");
      if (n.type === "script") return nodeKey.startsWith("script.");
      return false;
    });

    if (!target) {
      const [, rawId] = nodeKey.split(":");
      if (rawId) {
        target = nodes.find((n) => {
          if (n.type === "character") return n.data.charId === rawId;
          if (n.type === "scene") return n.data.sceneId === rawId;
          if (n.type === "shot") return n.data.shotId === rawId;
          return false;
        });
      }
    }

    if (target) {
      setSelectedNodeId(target.id);
      if (reactFlowInstanceRef.current && target.position) {
        reactFlowInstanceRef.current.setCenter(target.position.x + 160, target.position.y + 100, {
          duration: 600,
          zoom: 1.05,
        });
      }
    }
  }, [nodes, setSelectedNodeId]);

  const isRunning = pipelineState === "running" || Boolean(activeRunId) || canvasStats.running > 0;
  const isPaused = pipelineState === "paused_cast" || pipelineState === "paused_keyframes";

  const timelineShots = useMemo<TimelineShotItem[]>(() => {
    const shotNodes = (nodes.filter((n) => n.type === "shot") as Array<DramaFlowNode & { data: ShotNodeData }>).sort(
      (a, b) => a.data.index - b.data.index,
    );
    return shotNodes.map((s) => {
      const active = getActiveShotBranch(s.data);
      return {
        id: s.id,
        index: s.data.index,
        title: `${s.data.shotType} · ${s.data.cameraMove.split(" ")[0]}`,
        durationSec: s.data.actualAudioDuration || s.data.estimatedDuration || 4,
        status: (s.data.status as any) || "pending",
        imageUrl: active.imageUrl,
        voiceoverText: s.data.voiceover ? String(s.data.voiceover) : undefined,
        speakerName: s.data.characterName ? String(s.data.characterName) : undefined,
      };
    });
  }, [nodes]);

  const styledEdges = useMemo(() => {
    return edges.map((e) => {
      let color = "#6366F1";
      if (e.source.includes("char")) color = "#A855F7";
      else if (e.source.includes("scene")) color = "#0EA5E9";
      else if (e.source.includes("shot")) color = "#F59E0B";

      return {
        ...e,
        type: "flowingPulse",
        data: {
          color,
          activePulse: isRunning,
          glow: true,
        },
      };
    });
  }, [edges, isRunning]);

  const chronoNodeStates = useMemo(() => {
    const map: Record<string, any> = {};
    const backendNodes = backendProject?.nodes || {};
    Object.entries(backendNodes).forEach(([k, v]) => {
      map[k] = {
        status: v.status,
        statusNote: v.status_note,
        error: v.error,
        attempts: v.attempts,
        updatedAt: v.updated_at,
      };
    });
    nodes.forEach((n) => {
      if (n.data.status === "error" && n.data.error) {
        map[n.id] = {
          status: "failed",
          error: n.data.error,
        };
      }
    });
    return map;
  }, [backendProject, nodes]);

  const bottleneckCount = useMemo(() => {
    let count = 0;
    const backendNodes = backendProject?.nodes || {};
    Object.values(backendNodes).forEach((v) => {
      if (v.status === "failed") count++;
    });
    nodes.forEach((n) => {
      if (n.data.status === "error") count++;
    });
    return count;
  }, [backendProject, nodes]);

  const nodeActionCallbacks = useMemo(
    () => ({
      isPipelineRunning: isRunning,
      onRunBreakdown: handleRunBreakdown,
      onRunPipeline: () => handleRunPipeline("start"),
      onGenerateCharacter: (id: string) => {
        const node = useDramaCanvasStore.getState().nodes.find((n) => n.id === id && n.type === "character");
        if (node?.type === "character") {
          void useDramaCanvasStore.getState().retrySingleNode(`image.portrait:${node.data.charId}`).catch(() => {});
        }
      },
      onGenerateScene: (id: string) => {
        const node = useDramaCanvasStore.getState().nodes.find((n) => n.id === id && n.type === "scene");
        if (node?.type === "scene") {
          void useDramaCanvasStore.getState().retrySingleNode(`image.scene:${node.data.sceneId}`).catch(() => {});
        }
      },
      onGenerateShotAudio: (id: string) => {
        const node = useDramaCanvasStore.getState().nodes.find((n) => n.id === id && n.type === "shot");
        if (node?.type === "shot") {
          void useDramaCanvasStore.getState().retrySingleNode(`tts.line:${node.data.shotId}`).catch(() => {});
        }
      },
      onGenerateShotKeyframe: (id: string) => {
        const node = useDramaCanvasStore.getState().nodes.find((n) => n.id === id && n.type === "shot");
        if (node?.type === "shot") {
          void useDramaCanvasStore.getState().retrySingleNode(`image.keyframe:${node.data.shotId}`).catch(() => {});
        }
      },
      onGenerateShotVideo: (id: string) => {
        const node = useDramaCanvasStore.getState().nodes.find((n) => n.id === id && n.type === "shot");
        if (node?.type === "shot") {
          void useDramaCanvasStore.getState().retrySingleNode(`video.clip:${node.data.shotId}`).catch(() => {});
        }
      },
      onAddShotBranch: (id: string) => {
        addShotVersionBranch(id);
      },
      onSwitchShotBranch: (id: string, verId: string) => {
        switchShotVersionBranch(id, verId);
      },
      onTranscribeAudio: (id: string) => void transcribeAudioImportToScript(id).catch(() => {}),
      onOpenPlayer: () => setPlayerOpen(true),
      onExportVideo: () => void handleExportSingleVideo(),
      onExportZip: () => void handleExportZip(),
    }),
    [
      addShotVersionBranch,
      handleExportSingleVideo,
      handleExportZip,
      handleRunBreakdown,
      handleRunPipeline,
      isRunning,
      switchShotVersionBranch,
    ],
  );

  return (
    <DramaNodeActionsProvider value={nodeActionCallbacks}>
      <div
        className={`flex flex-col overflow-hidden bg-[var(--bg-canvas)] text-[var(--text-primary)] transition-all ${
          isCanvasMaximized ? "fixed inset-0 z-50 h-screen w-screen" : "h-full w-full flex-1 min-h-0"
        }`}
      >
        {/* 顶栏：工程管理 / 模式切换 / 检查点策略 / 一键启动 */}
        <div className="flex flex-wrap items-center justify-between gap-2.5 border-b border-[var(--border-default)] bg-[var(--bg-surface)] px-4 py-2.5">
          <div className="flex flex-wrap items-center gap-2">
            <div className="flex items-center gap-1.5 rounded-lg bg-indigo-600/10 px-2.5 py-1 text-xs font-semibold text-indigo-600 dark:text-indigo-400">
              <Workflow className="h-4 w-4" />
              短剧无限画布
            </div>

            <select
              value={projectId}
              onChange={(e) => void loadProject(e.target.value)}
              className="max-w-[180px] rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2.5 py-1 text-xs"
            >
              {!projects.some((p) => p.id === projectId) && (
                <option value={projectId}>{scriptNode?.data.title || "当前工程"}</option>
              )}
              {projects.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.title}
                </option>
              ))}
            </select>

            <button
              type="button"
              title="新建模板工程"
              onClick={() => void createNewProject("悬疑反转")}
              className="inline-flex items-center gap-1 rounded-lg border border-[var(--border-default)] px-2 py-1 text-xs hover:bg-[var(--bg-surface-hover)]"
            >
              <FolderPlus className="h-3.5 w-3.5" />
              新建
            </button>
            <button
              type="button"
              title="删除当前工程"
              onClick={() => void removeProject(projectId)}
              className="inline-flex items-center rounded-lg border border-[var(--border-default)] p-1.5 text-rose-500 hover:bg-rose-500/10"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </button>

            <span className="text-[11px] text-[var(--text-muted)]">
              {isSaving ? "正在同步..." : syncStatus === "synced" ? "云端+本地已保存" : syncStatus === "local" ? "仅本地保存" : "等待首次保存"}
            </span>
          </div>

          {/* 视图切换 + 检查点开关 */}
          <div className="flex flex-wrap items-center gap-2">
            <div className="inline-flex rounded-lg border border-[var(--border-default)] bg-[var(--bg-muted)] p-0.5 text-xs">
              <button
                type="button"
                onClick={() => setViewMode("canvas")}
                className={`inline-flex items-center gap-1 rounded-md px-2.5 py-1 font-medium transition ${
                  viewMode === "canvas" ? "bg-[var(--bg-surface)] shadow-sm text-indigo-600 dark:text-indigo-400" : ""
                }`}
              >
                <Network className="h-3.5 w-3.5" />
                无限画布
              </button>
              <button
                type="button"
                onClick={() => setViewMode("wizard")}
                className={`inline-flex items-center gap-1 rounded-md px-2.5 py-1 font-medium transition ${
                  viewMode === "wizard" ? "bg-[var(--bg-surface)] shadow-sm text-indigo-600 dark:text-indigo-400" : ""
                }`}
              >
                <ListChecks className="h-3.5 w-3.5" />
                5步分镜表
              </button>
            </div>

            {scriptNode && (
              <select
                value={scriptNode.data.routingPolicy}
                onChange={(e) => updateScriptNode({ routingPolicy: e.target.value as DramaRoutingPolicy })}
                className="rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2 py-1 text-xs"
              >
                <option value="free_first">路由：免费优先</option>
                <option value="quality_first">路由：质量优先</option>
                <option value="fast">路由：极速出片</option>
              </select>
            )}

            <label className="inline-flex items-center gap-1 rounded-lg border border-[var(--border-default)] px-2 py-1 text-[11px]">
              <input
                type="checkbox"
                checked={checkpointAfterCast}
                onChange={(e) => setCheckpointAfterCast(e.target.checked)}
              />
              定妆后暂停
            </label>
            <label className="inline-flex items-center gap-1 rounded-lg border border-[var(--border-default)] px-2 py-1 text-[11px]">
              <input
                type="checkbox"
                checked={checkpointAfterKeyframes}
                onChange={(e) => setCheckpointAfterKeyframes(e.target.checked)}
              />
              首帧后暂停
            </label>

            {viewMode === "canvas" && (
              <button
                type="button"
                onClick={autoLayout}
                className="inline-flex items-center gap-1 rounded-lg border border-[var(--border-default)] px-2.5 py-1 text-xs hover:bg-[var(--bg-surface-hover)]"
              >
                <LayoutGrid className="h-3.5 w-3.5" />
                整理布局
              </button>
            )}

            <button
              type="button"
              onClick={() => setSettingsModalOpen(true)}
              className="inline-flex items-center gap-1 rounded-lg border border-[var(--border-default)] px-2.5 py-1 text-xs hover:bg-[var(--bg-surface-hover)]"
              title="配置允许的生图/视频通道、并发数与自动降级策略"
            >
              <Settings2 className="h-3.5 w-3.5" />
              能力设置
            </button>

            {bottleneckCount > 0 && (
              <button
                type="button"
                onClick={() => {
                  setBottomDrawerMode("chrono");
                  setBottomDrawerCollapsed(false);
                }}
                className="inline-flex items-center gap-1.5 rounded-lg border border-rose-500/40 bg-rose-500/15 px-2.5 py-1 text-xs font-semibold text-rose-600 dark:text-rose-400 hover:bg-rose-500/25 transition animate-pulse"
                title="发现执行卡点，点击展开阻塞透视诊断"
              >
                <ShieldAlert className="h-3.5 w-3.5" />
                <span>{bottleneckCount} 处卡点阻断</span>
              </button>
            )}

            <button
              type="button"
              onClick={() => {
                if (bottomDrawerMode === "timeline" && !bottomDrawerCollapsed) {
                  setBottomDrawerCollapsed(true);
                } else {
                  setBottomDrawerMode("timeline");
                  setBottomDrawerCollapsed(false);
                }
              }}
              className={`inline-flex items-center gap-1 rounded-lg border px-2.5 py-1 text-xs transition-colors ${
                bottomDrawerMode === "timeline" && !bottomDrawerCollapsed
                  ? "border-amber-500/50 bg-amber-500/10 text-amber-600 dark:text-amber-400 font-medium"
                  : "border-[var(--border-default)] hover:bg-[var(--bg-surface-hover)]"
              }`}
              title="切换/折叠影视剪辑多轨时间线"
            >
              <Film className="h-3.5 w-3.5 text-amber-500" />
              时间线
            </button>

            <button
              type="button"
              onClick={() => {
                if (bottomDrawerMode === "chrono" && !bottomDrawerCollapsed) {
                  setBottomDrawerCollapsed(true);
                } else {
                  setBottomDrawerMode("chrono");
                  setBottomDrawerCollapsed(false);
                }
              }}
              className={`inline-flex items-center gap-1 rounded-lg border px-2.5 py-1 text-xs transition-colors ${
                bottomDrawerMode === "chrono" && !bottomDrawerCollapsed
                  ? "border-indigo-500/50 bg-indigo-500/10 text-indigo-600 dark:text-indigo-400 font-medium"
                  : "border-[var(--border-default)] hover:bg-[var(--bg-surface-hover)]"
              }`}
              title="查看事件时光轴与卡点根因分析"
            >
              <Clock className="h-3.5 w-3.5 text-indigo-500" />
              时光轴
              {runLogs.length > 0 && (
                <span className="rounded-full bg-[var(--bg-elevated)] px-1.5 text-[10px] font-medium font-mono">
                  {runLogs.length}
                </span>
              )}
            </button>

            <button
              type="button"
              onClick={() => setIsCanvasMaximized(!isCanvasMaximized)}
              className={`inline-flex items-center gap-1 rounded-lg border px-2.5 py-1 text-xs transition-colors ${
                isCanvasMaximized
                  ? "border-indigo-500 bg-indigo-500/15 text-indigo-600 dark:text-indigo-400 font-semibold"
                  : "border-[var(--border-default)] hover:bg-[var(--bg-surface-hover)] text-[var(--text-default)]"
              }`}
              title={isCanvasMaximized ? "退出最大化 (Esc)" : "最大化画布 (Zen Mode)：收缩所有导航面板，只给用户画布操作"}
            >
              {isCanvasMaximized ? <Minimize2 className="h-3.5 w-3.5" /> : <Maximize2 className="h-3.5 w-3.5" />}
              <span>{isCanvasMaximized ? "还原" : "最大化画布"}</span>
            </button>

            {isRunning ? (
              <button
                type="button"
                disabled
                className="inline-flex cursor-not-allowed items-center gap-1.5 rounded-lg bg-indigo-600/80 px-3.5 py-1.5 text-xs font-medium text-white opacity-90"
                title="后台 DAG 流水线正在执行中，关闭页面也不会中断"
              >
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
                {runSummary && runSummary.total > 0
                  ? `后台生成中 (${runSummary.ready + runSummary.degraded}/${runSummary.total})`
                  : "后台生成中..."}
              </button>
            ) : isPaused ? (
              <button
                type="button"
                onClick={() =>
                  handleRunPipeline(pipelineState === "paused_cast" ? "after_cast" : "after_keyframes")
                }
                className="inline-flex items-center gap-1.5 rounded-lg bg-amber-600 px-3.5 py-1.5 text-xs font-medium text-white hover:bg-amber-500"
              >
                <Play className="h-3.5 w-3.5" />
                检查点已确认 · 继续运行
              </button>
            ) : (
              <button
                type="button"
                onClick={() => handleRunPipeline("start", !nodes.some((node) => node.type === "shot"))}
                className="inline-flex items-center gap-1.5 rounded-lg bg-indigo-600 px-3.5 py-1.5 text-xs font-medium text-white hover:bg-indigo-500"
              >
                <Sparkles className="h-3.5 w-3.5" />
                {canvasStats.total > 0 && canvasStats.ready === canvasStats.total && canvasStats.failed === 0 && canvasStats.stale === 0
                  ? "全片已就绪 · 增量校验"
                  : canvasStats.failed > 0
                    ? `补跑失败节点 (${canvasStats.failed})`
                    : canvasStats.stale > 0
                      ? `更新失效节点 (${canvasStats.stale})`
                      : "一键生成连贯短片"}
              </button>
            )}
          </div>
        </div>

        {/* 能力矩阵 Preflight 检查条与实时进度状态 */}
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-[var(--border-default)] bg-[var(--bg-elevated)] px-4 py-1.5 text-[11px]">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium text-[var(--text-muted)]">平台算力实况：</span>
            <span
              title={capabilities?.llm.reason || "ChatGPT 对话拆本可用"}
              className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 ${
                capabilities?.llm.available ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400" : "bg-amber-500/10 text-amber-600"
              }`}
            >
              {capabilities?.llm.available ? <CheckCircle2 className="h-3 w-3" /> : <AlertTriangle className="h-3 w-3" />}
              拆本 LLM ({capabilities?.llm.active ?? 0})
            </span>
            <span
              title={capabilities?.image.reason || "生图与参考图锁定能力"}
              className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 ${
                capabilities?.image.available ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400" : "bg-rose-500/10 text-rose-500"
              }`}
            >
              {capabilities?.image.available ? <CheckCircle2 className="h-3 w-3" /> : <AlertTriangle className="h-3 w-3" />}
              生图 {capabilities?.image.edit_available ? "(支持多图参考锁脸)" : "(提示词锁定模式)"}
            </span>
            <span className="inline-flex items-center gap-1 rounded-full bg-emerald-500/10 px-2 py-0.5 text-emerald-600 dark:text-emerald-400">
              <CheckCircle2 className="h-3 w-3" />
              纯云端配音 Edge-TTS / CosyVoice
            </span>
            <button
              type="button"
              onClick={() => setCloudKeyModalOpen(true)}
              className="inline-flex items-center gap-1 rounded-full border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-amber-700 dark:text-amber-400 hover:bg-amber-500/20"
              title="无本地硬件要求，点击配置硅基流动/阿里百炼/即梦/Meta Muse 云端 API Key"
            >
              <KeyRound className="h-3 w-3" />
              配置云端 API Key
            </button>
            <span
              title={capabilities?.video.reason || "真实视频大模型可用"}
              className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 ${
                capabilities?.video.available ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400" : "bg-sky-500/10 text-sky-600 dark:text-sky-400"
              }`}
            >
              {capabilities?.video.available ? (
                <>
                  <CheckCircle2 className="h-3 w-3" />
                  视频生成 {capabilities.video.lipsync_available ? "(含数字人口型)" : "(文/图生视频)"}
                </>
              ) : (
                <>
                  <Clapperboard className="h-3 w-3" />
                  无视频模型凭据 · 自动 Ken Burns 运镜合成
                </>
              )}
            </span>
            <button
              type="button"
              onClick={() => void refreshCapabilities()}
              disabled={capabilitiesLoading}
              className="inline-flex items-center gap-1 text-[var(--text-muted)] hover:text-[var(--text-primary)]"
            >
              <RefreshCw className={`h-3 w-3 ${capabilitiesLoading ? "animate-spin" : ""}`} />
              刷新
            </button>
          </div>

          <div className="flex flex-wrap items-center gap-2.5">
            {canvasStats.total > 0 && (() => {
              const pct = Math.round((canvasStats.ready / canvasStats.total) * 100);
              const allReady = canvasStats.ready === canvasStats.total && canvasStats.failed === 0 && canvasStats.running === 0 && canvasStats.stale === 0;
              if (isRunning || canvasStats.running > 0) {
                return (
                  <span className="inline-flex items-center gap-1.5 rounded-full border border-indigo-500/30 bg-indigo-500/10 px-2.5 py-0.5 text-[11px] font-semibold text-indigo-600 dark:text-indigo-400">
                    <Loader2 className="h-3 w-3 animate-spin" />
                    后台制作中 · {canvasStats.ready}/{canvasStats.total} ({pct}%)
                  </span>
                );
              }
              if (allReady) {
                return (
                  <button
                    type="button"
                    onClick={() => setPlayerOpen(true)}
                    className="inline-flex items-center gap-1.5 rounded-full border border-emerald-500/40 bg-emerald-500/15 px-3 py-0.5 text-[11px] font-semibold text-emerald-700 hover:bg-emerald-500/25 dark:text-emerald-300"
                    title="所有节点均已就绪，点击打开时间线播放器预览或导出成片"
                  >
                    <CheckCircle2 className="h-3.5 w-3.5" />
                    已成片 · 全部就绪 ({canvasStats.ready}/{canvasStats.total}) · 点击播放预览
                  </button>
                );
              }
              if (canvasStats.failed > 0) {
                return (
                  <span className="inline-flex items-center gap-1.5 rounded-full border border-rose-500/30 bg-rose-500/10 px-2.5 py-0.5 text-[11px] font-semibold text-rose-600 dark:text-rose-400">
                    <AlertTriangle className="h-3 w-3" />
                    部分失败 · 就绪 {canvasStats.ready}/{canvasStats.total} · 失败 {canvasStats.failed}
                  </span>
                );
              }
              if (canvasStats.stale > 0) {
                return (
                  <span className="inline-flex items-center gap-1.5 rounded-full border border-orange-500/30 bg-orange-500/10 px-2.5 py-0.5 text-[11px] font-semibold text-orange-600 dark:text-orange-400">
                    <RefreshCw className="h-3 w-3" />
                    待更新 · {canvasStats.stale} 项需重算 ({canvasStats.ready}/{canvasStats.total})
                  </span>
                );
              }
              return (
                <span className="inline-flex items-center gap-1.5 rounded-full bg-slate-500/10 px-2.5 py-0.5 text-[11px] font-medium text-[var(--text-muted)]">
                  进度 {canvasStats.ready}/{canvasStats.total} ({pct}%)
                </span>
              );
            })()}
            {canvasStats.locked > 0 && (
              <span className="inline-flex items-center gap-0.5 rounded bg-slate-500/10 px-1.5 py-0.5 text-[10px] text-[var(--text-muted)]">
                <Lock className="h-2.5 w-2.5" /> 锁定 {canvasStats.locked}
              </span>
            )}
            {pipelineStageText && (
              <div className="flex items-center gap-1.5 font-medium text-indigo-600 dark:text-indigo-400">
                {isRunning && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
                {isPaused && <PauseCircle className="h-3.5 w-3.5 text-amber-500" />}
                <span className="max-w-xs truncate" title={pipelineStageText}>{pipelineStageText}</span>
              </div>
            )}
          </div>
        </div>

        {/* 主体区：无限画布三栏 vs 5步分镜列表视图 */}
        {viewMode === "wizard" ? (
          <DramaWizardView
            nodes={nodes}
            updateNodeData={updateNodeData}
            updateScriptNode={updateScriptNode}
            addNode={(kind) => addNode(kind)}
            removeNode={removeNode}
            onSelectShot={(nodeId) => {
              setSelectedNodeId(nodeId);
              setViewMode("canvas");
            }}
            onRunBreakdown={handleRunBreakdown}
            onRunPipeline={() => handleRunPipeline("start", false)}
            onGenerateCharacter={nodeActionCallbacks.onGenerateCharacter}
            onGenerateScene={nodeActionCallbacks.onGenerateScene}
            onGenerateShotAudio={nodeActionCallbacks.onGenerateShotAudio}
            onGenerateShotKeyframe={nodeActionCallbacks.onGenerateShotKeyframe}
            onGenerateShotVideo={nodeActionCallbacks.onGenerateShotVideo}
            onOpenPlayer={() => setPlayerOpen(true)}
            onExportVideo={() => void handleExportSingleVideo()}
            onExportZip={() => void handleExportZip()}
          />
        ) : (
          <div className="flex flex-1 overflow-hidden">
            {/* 左侧节点箱与全平台素材引入面板 (最大化时收缩) */}
            {!isCanvasMaximized && (
              <aside className="hidden w-60 shrink-0 flex-col gap-3 overflow-y-auto border-r border-[var(--border-default)] bg-[var(--bg-surface)] p-3 text-xs lg:flex">
              <div className="space-y-1.5">
                <div className="px-1 text-[11px] font-semibold uppercase tracking-wide text-[var(--text-muted)]">
                  画布节点添加
                </div>
                <button
                  type="button"
                  onClick={() => addNode("character")}
                  className="flex w-full items-center gap-2 rounded-lg border border-[var(--border-default)] px-2.5 py-2 text-left hover:bg-[var(--bg-surface-hover)]"
                >
                  <UserPlus className="h-3.5 w-3.5 text-violet-500" /> + 角色视觉/声线锚点
                </button>
                <button
                  type="button"
                  onClick={() => addNode("scene")}
                  className="flex w-full items-center gap-2 rounded-lg border border-[var(--border-default)] px-2.5 py-2 text-left hover:bg-[var(--bg-surface-hover)]"
                >
                  <MapPin className="h-3.5 w-3.5 text-sky-500" /> + 场景环境锚点
                </button>
                <button
                  type="button"
                  onClick={() => addNode("shot")}
                  className="flex w-full items-center gap-2 rounded-lg border border-[var(--border-default)] px-2.5 py-2 text-left hover:bg-[var(--bg-surface-hover)]"
                >
                  <Clapperboard className="h-3.5 w-3.5 text-amber-500" /> + 分镜镜头卡片
                </button>
                <button
                  type="button"
                  onClick={() => addNode("audioImport")}
                  className="flex w-full items-center gap-2 rounded-lg border border-[var(--border-default)] px-2.5 py-2 text-left hover:bg-[var(--bg-surface-hover)]"
                >
                  <FileAudio className="h-3.5 w-3.5 text-teal-500" /> + 音频 / Whisper 转写
                </button>
              </div>

              <div className="space-y-1.5 border-t border-[var(--border-default)] pt-3">
                <div className="px-1 text-[11px] font-semibold uppercase tracking-wide text-[var(--text-muted)]">
                  全平台能力联动导入
                </div>
                <button
                  type="button"
                  onClick={() => setAssetDrawer("gallery")}
                  className="flex w-full items-center gap-2 rounded-lg bg-[var(--bg-muted)] px-2.5 py-2 text-left hover:bg-[var(--bg-surface-hover)]"
                >
                  <ImageIcon className="h-3.5 w-3.5 text-indigo-500" /> 从图片工坊画廊导入
                </button>
                <button
                  type="button"
                  onClick={() => setAssetDrawer("audio")}
                  className="flex w-full items-center gap-2 rounded-lg bg-[var(--bg-muted)] px-2.5 py-2 text-left hover:bg-[var(--bg-surface-hover)]"
                >
                  <FileAudio className="h-3.5 w-3.5 text-teal-500" /> 从配音历史库导入
                </button>
                <button
                  type="button"
                  onClick={() => setAssetDrawer("prompts")}
                  className="flex w-full items-center gap-2 rounded-lg bg-[var(--bg-muted)] px-2.5 py-2 text-left hover:bg-[var(--bg-surface-hover)]"
                >
                  <Sparkles className="h-3.5 w-3.5 text-amber-500" /> 从提示词广场套用风格
                </button>
                <a
                  href="#/chat"
                  className="flex w-full items-center gap-2 rounded-lg border border-dashed border-[var(--border-default)] px-2.5 py-2 text-left text-primary hover:bg-[var(--bg-surface-hover)]"
                >
                  <Globe className="h-3.5 w-3.5" /> Chat 爬虫搜音色/图/视频入库
                </a>
              </div>

              <div className="space-y-1.5 border-t border-[var(--border-default)] pt-3">
                <div className="px-1 text-[11px] font-semibold uppercase tracking-wide text-[var(--text-muted)]">
                  预设模板快速开局
                </div>
                {Object.keys(DRAMA_PRESETS).map((key) => (
                  <button
                    key={key}
                    type="button"
                    onClick={() => void createNewProject(key)}
                    className="flex w-full items-center gap-2 rounded-lg border border-[var(--border-default)] px-2.5 py-1.5 text-left hover:bg-[var(--bg-surface-hover)]"
                  >
                    <ScrollText className="h-3.5 w-3.5 text-indigo-500" /> {key}模板
                  </button>
                ))}
              </div>
            </aside>
            )}

            {/* 中央 React Flow 无限画布 */}
            <div className="relative flex-1">
              {/* 最大化纯净画布微型悬浮退出胶囊 */}
              {isCanvasMaximized && (
                <div className="absolute top-3 right-3 z-40 flex items-center gap-2 rounded-xl bg-[var(--bg-surface)]/90 border border-[var(--border-default)] p-1.5 shadow-2xl backdrop-blur-md">
                  <div className="flex items-center gap-1.5 px-2 text-xs font-medium text-[var(--text-muted)]">
                    <span className="h-2 w-2 rounded-full bg-emerald-500 animate-pulse"></span>
                    <span>纯净画布模式 (Zen Mode)</span>
                    <span className="text-[10px] px-1.5 py-0.2 rounded bg-[var(--bg-elevated)] border border-[var(--border-default)] font-mono">
                      ESC 退出
                    </span>
                  </div>
                  <button
                    type="button"
                    onClick={() => setIsCanvasMaximized(false)}
                    className="inline-flex items-center gap-1 rounded-lg bg-indigo-600 hover:bg-indigo-500 text-white px-2.5 py-1 text-xs font-semibold shadow-md transition"
                    title="还原窗口并展开全部面板"
                  >
                    <Minimize2 className="h-3 w-3" />
                    <span>退出最大化</span>
                  </button>
                </div>
              )}
              <ReactFlowProvider>
                <ReactFlow
                  nodes={nodes}
                  edges={styledEdges}
                  nodeTypes={DRAMA_NODE_TYPES}
                  edgeTypes={DRAMA_EDGE_TYPES}
                  onInit={(instance) => {
                    reactFlowInstanceRef.current = instance;
                  }}
                  onNodesChange={onNodesChange}
                  onEdgesChange={onEdgesChange}
                  onConnect={onConnect}
                  onNodeClick={(_, n) => setSelectedNodeId(n.id)}
                  colorMode={themeMode}
                  fitView
                  minZoom={0.25}
                  maxZoom={1.6}
                >
                  <Background gap={20} size={1} />
                  <Controls />
                  <MiniMap pannable zoomable />
                </ReactFlow>
              </ReactFlowProvider>
            </div>

            {/* 右侧节点属性精调面板 (在最大化全屏模式下自动收缩) */}
            {!isCanvasMaximized && (
              <aside className="w-80 shrink-0 overflow-y-auto border-l border-[var(--border-default)] bg-[var(--bg-surface)]">
                <DramaNodeInspector
                  selectedNode={selectedNode}
                  capabilities={capabilities}
                  voices={voices}
                  onRunBreakdown={handleRunBreakdown}
                  onGenerateCharacter={nodeActionCallbacks.onGenerateCharacter}
                  onGenerateScene={nodeActionCallbacks.onGenerateScene}
                  onGenerateShotAudio={nodeActionCallbacks.onGenerateShotAudio}
                  onGenerateShotKeyframe={nodeActionCallbacks.onGenerateShotKeyframe}
                  onGenerateShotVideo={nodeActionCallbacks.onGenerateShotVideo}
                  onTranscribeAudio={(id) => void transcribeAudioImportToScript(id).catch(() => {})}
                  onOpenPlayer={() => setPlayerOpen(true)}
                  onExportVideo={() => void handleExportSingleVideo()}
                  onExportZip={() => void handleExportZip()}
                />
              </aside>
            )}
          </div>
        )}

        {/* 底部专业影视双模底栏：影视多轨时间轴 + 事件时光轴卡点分析 (最大化时收缩) */}
        {!isCanvasMaximized && !bottomDrawerCollapsed && (
          <div className="flex shrink-0 flex-col border-t border-[var(--border-default)] bg-[var(--bg-surface)] text-xs shadow-2xl">
            {/* 底栏切换条 */}
            <div className="flex items-center justify-between border-b border-[var(--border-default)] bg-[var(--bg-elevated)] px-4 py-1">
              <div className="flex items-center gap-1">
                <button
                  type="button"
                  onClick={() => setBottomDrawerMode("timeline")}
                  className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs transition ${
                    bottomDrawerMode === "timeline"
                      ? "bg-amber-500/15 font-semibold text-amber-600 dark:text-amber-400"
                      : "text-[var(--text-muted)] hover:text-[var(--text-default)]"
                  }`}
                >
                  <Film className="h-3.5 w-3.5" />
                  <span>影视多轨时间轴</span>
                  <span className="rounded bg-[var(--bg-surface)] px-1 py-0.2 text-[10px] font-mono text-[var(--text-muted)]">
                    {timelineShots.length} 镜头
                  </span>
                </button>

                <button
                  type="button"
                  onClick={() => setBottomDrawerMode("chrono")}
                  className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs transition ${
                    bottomDrawerMode === "chrono"
                      ? "bg-indigo-500/15 font-semibold text-indigo-600 dark:text-indigo-400"
                      : "text-[var(--text-muted)] hover:text-[var(--text-default)]"
                  }`}
                >
                  <Clock className="h-3.5 w-3.5" />
                  <span>事件时光轴 & 卡点分析</span>
                  {bottleneckCount > 0 ? (
                    <span className="rounded-full bg-rose-500/20 px-1.5 py-0.2 text-[10px] font-bold text-rose-600 animate-pulse">
                      {bottleneckCount} 卡点
                    </span>
                  ) : runLogs.length > 0 ? (
                    <span className="rounded bg-[var(--bg-surface)] px-1 py-0.2 text-[10px] font-mono text-[var(--text-muted)]">
                      {runLogs.length}
                    </span>
                  ) : null}
                </button>

                <button
                  type="button"
                  onClick={() => setBottomDrawerMode("logs")}
                  className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs transition ${
                    bottomDrawerMode === "logs"
                      ? "bg-slate-500/15 font-semibold text-slate-700 dark:text-slate-300"
                      : "text-[var(--text-muted)] hover:text-[var(--text-default)]"
                  }`}
                >
                  <Terminal className="h-3.5 w-3.5" />
                  <span>终端日志</span>
                </button>
              </div>

              <button
                type="button"
                onClick={() => setBottomDrawerCollapsed(true)}
                className="inline-flex items-center gap-1 rounded px-2 py-0.5 text-[11px] text-[var(--text-muted)] hover:bg-[var(--bg-surface-hover)]"
              >
                最小化收起 <ChevronDown className="h-3 w-3" />
              </button>
            </div>

            {/* 模式 1：影视剪辑多轨时间线 */}
            {bottomDrawerMode === "timeline" && (
              <CinematicTimeline
                shots={timelineShots}
                selectedShotId={selectedNodeId || undefined}
                onSelectShot={(shotId) => focusNodeByKey(shotId)}
                onOpenPreviewModal={() => setPlayerOpen(true)}
                onExportDraft={() => void handleExportZip()}
                isCollapsed={false}
                onToggleCollapse={() => setBottomDrawerCollapsed(true)}
              />
            )}

            {/* 模式 2：事件时光轴 & 阻塞卡点透视 */}
            {bottomDrawerMode === "chrono" && (
              <div className="h-72 shrink-0 overflow-hidden">
                <ChronoEventTracker
                  logs={runLogs.map((l) => ({
                    timestamp: l.timestamp,
                    level: (l.level as any) || "info",
                    nodeKey: l.node_key,
                    message: l.message,
                  }))}
                  nodeStates={chronoNodeStates}
                  onFocusNode={(key) => focusNodeByKey(key)}
                  onRetryNode={(key) => {
                    void useDramaCanvasStore.getState().retrySingleNode(key).catch(() => {});
                  }}
                  onClose={() => setBottomDrawerCollapsed(true)}
                  selectedLogRunId={selectedLogRunId || undefined}
                  runsHistory={runsHistory.map((r) => ({
                    id: r.id,
                    status: r.status,
                    createdAt: r.created_at || "",
                  }))}
                  onSelectRun={(runId) => void selectRunLogView(runId)}
                />
              </div>
            )}

            {/* 模式 3：原始控制台日志 */}
            {bottomDrawerMode === "logs" && (
              <div className="h-52 shrink-0 overflow-y-auto px-4 py-2 font-mono text-[11px] leading-relaxed">
                {runLogs.length === 0 ? (
                  <div className="flex h-full items-center justify-center text-[var(--text-muted)]">
                    暂无执行日志，点击「一键生成连贯短片」或任意节点操作即可实时追踪执行轨迹
                  </div>
                ) : (
                  <div className="space-y-1">
                    {runLogs.map((entry, idx) => {
                      const levelBadge =
                        entry.level === "error"
                          ? "bg-rose-500/15 text-rose-600 dark:text-rose-400"
                          : entry.level === "warn"
                            ? "bg-amber-500/15 text-amber-600 dark:text-amber-400"
                            : entry.level === "success"
                              ? "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400"
                              : "bg-slate-500/10 text-[var(--text-muted)]";
                      const timeStr = entry.timestamp ? new Date(entry.timestamp).toLocaleTimeString() : "";
                      return (
                        <div key={`${entry.timestamp}-${idx}`} className="flex items-start gap-2">
                          <span className="shrink-0 text-[10px] text-[var(--text-muted)]">{timeStr}</span>
                          <span className={`shrink-0 rounded px-1.5 py-0.2 text-[10px] font-semibold uppercase ${levelBadge}`}>
                            {entry.level}
                          </span>
                          {entry.node_key && (
                            <button
                              type="button"
                              onClick={() => focusNodeByKey(entry.node_key)}
                              className="shrink-0 rounded bg-indigo-500/10 px-1.5 py-0.2 text-[10px] text-indigo-600 hover:underline dark:text-indigo-400"
                              title="点击定位画布节点"
                            >
                              {entry.node_key}
                            </button>
                          )}
                          <span className="break-all text-[var(--text-primary)]">{entry.message}</span>
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            )}
          </div>
        )}

        {/* 当底栏折叠时的微型展开条 */}
        {!isCanvasMaximized && bottomDrawerCollapsed && (
          <div className="h-6 shrink-0 bg-[var(--bg-elevated)] border-t border-[var(--border-default)] px-4 flex items-center justify-between text-[11px] text-[var(--text-muted)] select-none">
            <div className="flex items-center gap-3">
              <span className="flex items-center gap-1 text-amber-500 font-medium">
                <Film className="h-3 w-3" /> 多轨时间线 ({timelineShots.length})
              </span>
              <span>·</span>
              <span className="flex items-center gap-1 text-indigo-500 font-medium">
                <Clock className="h-3 w-3" /> 时光轴追踪
              </span>
              {bottleneckCount > 0 && (
                <span className="text-rose-500 font-semibold flex items-center gap-1">
                  ● 发现 {bottleneckCount} 个卡点阻断
                </span>
              )}
            </div>
            <button
              type="button"
              onClick={() => setBottomDrawerCollapsed(false)}
              className="flex items-center gap-1 text-[var(--text-default)] hover:text-indigo-500"
            >
              展开底栏 <ChevronUp className="h-3 w-3" />
            </button>
          </div>
        )}

        <DramaAssetDrawerModal openTab={assetDrawer} onClose={() => setAssetDrawer(null)} />

        <DramaTimelinePlayerModal
          open={playerOpen}
          onClose={() => setPlayerOpen(false)}
          title={scriptNode?.data.title || "短剧成片"}
          aspectRatio={scriptNode?.data.aspectRatio || "9:16"}
          shots={composerShots}
          bgmUrl={composerNode?.data.bgmUrl}
          bgmVolume={composerNode?.data.bgmVolume}
          showSubtitles={composerNode?.data.showSubtitles}
          exportedVideoUrl={composerNode?.data.exportedVideoUrl}
          exportedFilename={composerNode?.data.exportedFilename}
          exportProgress={composerNode?.data.exportProgress || 0}
          exportStageText={composerNode?.data.exportStageText}
          onExportVideo={handleExportSingleVideo}
          onExportZip={handleExportZip}
        />

        <CloudKeyConfigModal
          open={cloudKeyModalOpen}
          onClose={() => setCloudKeyModalOpen(false)}
          initialProvider="siliconflow"
          allowedProviders={["siliconflow", "dashscope", "jimeng", "meta_muse"]}
          title="配置短剧云端创作 API Key（纯云端推理）"
          onSaved={() => {
            void refreshCapabilities();
          }}
        />

        <DramaSettingsModal
          open={settingsModalOpen}
          onClose={() => setSettingsModalOpen(false)}
          capabilities={capabilities}
          scriptData={scriptNode?.data}
          onSave={(patch) => {
            updateScriptNode(patch);
            void useDramaCanvasStore.getState().persistNow();
          }}
          onOpenCloudKeyModal={() => setCloudKeyModalOpen(true)}
        />
      </div>
    </DramaNodeActionsProvider>
  );
};
