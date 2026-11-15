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
  Clapperboard,
  FileAudio,
  FolderPlus,
  Image as ImageIcon,
  LayoutGrid,
  ListChecks,
  Loader2,
  MapPin,
  Network,
  PauseCircle,
  Play,
  RefreshCw,
  ScrollText,
  Sparkles,
  Square,
  Trash2,
  UserPlus,
  Workflow,
} from "lucide-react";
import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { fetchVoices, type DramaRoutingPolicy, type VoiceDescriptor } from "../api/client";
import { DramaAssetDrawerModal } from "../components/drama/DramaAssetDrawerModal";
import { DramaNodeInspector } from "../components/drama/DramaNodeInspector";
import { DRAMA_NODE_TYPES, DramaNodeActionsProvider } from "../components/drama/DramaNodes";
import { DramaTimelinePlayerModal } from "../components/drama/DramaTimelinePlayerModal";
import { DramaWizardView } from "../components/drama/DramaWizardView";
import { consumePendingDramaImports, DRAMA_IMPORT_EVENT, type DramaCanvasImportPayload } from "../lib/dramaCanvasBridge";
import {
  generateCharacterPortrait,
  generateSceneAnchor,
  generateShotAudioNode,
  generateShotKeyframeNode,
  generateShotVideoNode,
  resumeInflightDramaTasks,
  runAutoDramaPipeline,
  runScriptBreakdownToCanvas,
  transcribeAudioImportToScript,
} from "../lib/dramaOrchestrator";
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

export const ShortDramaStudio: React.FC = () => {
  const {
    projectId,
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
    initFromPersistence,
    refreshCapabilities,
    loadProject,
    createNewProject,
    removeProject,
    setSelectedNodeId,
    setViewMode,
    setCheckpointAfterCast,
    setCheckpointAfterKeyframes,
    onNodesChange,
    onEdgesChange,
    onConnect,
    autoLayout,
    updateNodeData,
    updateScriptNode,
    addNode,
    addShotVersionBranch,
    switchShotVersionBranch,
    importCrossStudioAsset,
  } = useDramaCanvasStore();

  const [voices, setVoices] = useState<VoiceDescriptor[]>([]);
  const [assetDrawer, setAssetDrawer] = useState<"gallery" | "audio" | "prompts" | null>(null);
  const [playerOpen, setPlayerOpen] = useState(false);
  const [themeMode, setThemeMode] = useState<"light" | "dark">(() =>
    document.documentElement.dataset.theme === "dark" ? "dark" : "light",
  );
  const abortRef = useRef<AbortController | null>(null);
  const lastExportBlobRef = useRef<{ blob: Blob; ext: string } | null>(null);

  // 初始化双写持久化、探测能力并恢复未完成视频轮询 + 导入跨工坊素材
  useEffect(() => {
    void initFromPersistence().then(() => {
      void resumeInflightDramaTasks();
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
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    void runScriptBreakdownToCanvas(ctrl.signal).catch(() => {});
  }, []);

  const handleRunPipeline = useCallback((resumeFrom: "start" | "after_cast" | "after_keyframes" = "start") => {
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    void runAutoDramaPipeline({ resumeFrom, signal: ctrl.signal }).catch(() => {});
  }, []);

  const handleExportSingleVideo = useCallback(async () => {
    if (!composerNode || !scriptNode) return;
    try {
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
  }, [composerNode, composerShots, scriptNode, updateNodeData]);

  const handleExportZip = useCallback(async () => {
    if (!composerNode || !scriptNode) return;
    try {
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

  const nodeActionCallbacks = useMemo(
    () => ({
      onRunBreakdown: handleRunBreakdown,
      onRunPipeline: () => handleRunPipeline("start"),
      onGenerateCharacter: (id: string) => void generateCharacterPortrait(id).catch(() => {}),
      onGenerateScene: (id: string) => void generateSceneAnchor(id).catch(() => {}),
      onGenerateShotAudio: (id: string) => void generateShotAudioNode(id).catch(() => {}),
      onGenerateShotKeyframe: (id: string) => void generateShotKeyframeNode(id).catch(() => {}),
      onGenerateShotVideo: (id: string) => void generateShotVideoNode(id).catch(() => {}),
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
      switchShotVersionBranch,
    ],
  );

  const isRunning = pipelineState === "running";
  const isPaused = pipelineState === "paused_cast" || pipelineState === "paused_keyframes";

  return (
    <DramaNodeActionsProvider value={nodeActionCallbacks}>
      <div className="flex h-[calc(100vh-3.5rem)] w-full flex-col overflow-hidden bg-[var(--bg-canvas)] text-[var(--text-primary)]">
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
              {isSaving ? "正在同步..." : "已云端+本地保存"}
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

            {isRunning ? (
              <button
                type="button"
                onClick={() => abortRef.current?.abort()}
                className="inline-flex items-center gap-1.5 rounded-lg bg-rose-600 px-3.5 py-1.5 text-xs font-medium text-white hover:bg-rose-500"
              >
                <Square className="h-3.5 w-3.5" />
                停止流水线
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
                onClick={() => handleRunPipeline("start")}
                className="inline-flex items-center gap-1.5 rounded-lg bg-indigo-600 px-3.5 py-1.5 text-xs font-medium text-white hover:bg-indigo-500"
              >
                <Sparkles className="h-3.5 w-3.5" />
                一键生成连贯短片
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
              免费配音 Edge-TTS / 剪映
            </span>
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

          {pipelineStageText && (
            <div className="flex items-center gap-1.5 text-indigo-600 dark:text-indigo-400 font-medium">
              {isRunning && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {isPaused && <PauseCircle className="h-3.5 w-3.5 text-amber-500" />}
              <span>{pipelineStageText}</span>
            </div>
          )}
        </div>

        {/* 主体区：无限画布三栏 vs 5步分镜列表视图 */}
        {viewMode === "wizard" ? (
          <DramaWizardView
            onRunBreakdown={handleRunBreakdown}
            onRunPipeline={() => handleRunPipeline("start")}
            onGenerateCharacter={(id) => void generateCharacterPortrait(id).catch(() => {})}
            onGenerateScene={(id) => void generateSceneAnchor(id).catch(() => {})}
            onGenerateShotAudio={(id) => void generateShotAudioNode(id).catch(() => {})}
            onGenerateShotKeyframe={(id) => void generateShotKeyframeNode(id).catch(() => {})}
            onGenerateShotVideo={(id) => void generateShotVideoNode(id).catch(() => {})}
            onOpenPlayer={() => setPlayerOpen(true)}
            onExportVideo={() => void handleExportSingleVideo()}
            onExportZip={() => void handleExportZip()}
          />
        ) : (
          <div className="flex flex-1 overflow-hidden">
            {/* 左侧节点箱与全平台素材引入面板 */}
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

            {/* 中央 React Flow 无限画布 */}
            <div className="relative flex-1">
              <ReactFlowProvider>
                <ReactFlow
                  nodes={nodes}
                  edges={edges}
                  nodeTypes={DRAMA_NODE_TYPES}
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

            {/* 右侧节点属性精调面板 */}
            <aside className="w-80 shrink-0 overflow-y-auto border-l border-[var(--border-default)] bg-[var(--bg-surface)]">
              <DramaNodeInspector
                selectedNode={selectedNode}
                capabilities={capabilities}
                voices={voices}
                onRunBreakdown={handleRunBreakdown}
                onGenerateCharacter={(id) => void generateCharacterPortrait(id).catch(() => {})}
                onGenerateScene={(id) => void generateSceneAnchor(id).catch(() => {})}
                onGenerateShotAudio={(id) => void generateShotAudioNode(id).catch(() => {})}
                onGenerateShotKeyframe={(id) => void generateShotKeyframeNode(id).catch(() => {})}
                onGenerateShotVideo={(id) => void generateShotVideoNode(id).catch(() => {})}
                onTranscribeAudio={(id) => void transcribeAudioImportToScript(id).catch(() => {})}
                onOpenPlayer={() => setPlayerOpen(true)}
                onExportVideo={() => void handleExportSingleVideo()}
                onExportZip={() => void handleExportZip()}
              />
            </aside>
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
      </div>
    </DramaNodeActionsProvider>
  );
};
