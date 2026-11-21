import {
  CheckCircle2,
  Film,
  Image as ImageIcon,
  Layers,
  Mic,
  Play,
  Plus,
  RefreshCw,
  Sparkles,
  Trash2,
  Video,
} from "lucide-react";
import React, { useState } from "react";
import { toThumbImageUrl } from "../../lib/mediaUrl";
import {
  getActiveShotBranch,
  STYLE_PRESETS,
  type CharacterNodeData,
  type ComposerNodeData,
  type DramaFlowNode,
  type SceneNodeData,
  type ScriptNodeData,
  type ShotNodeData,
} from "../../lib/dramaPresets";

interface DramaWizardViewProps {
  nodes: DramaFlowNode[];
  updateNodeData: <T extends Record<string, unknown>>(
    nodeId: string,
    updater: Partial<T> | ((prev: T) => T),
  ) => void;
  updateScriptNode: (patch: Partial<ScriptNodeData>) => void;
  addNode: (kind: "character" | "scene" | "shot") => void;
  removeNode: (nodeId: string) => void;
  onSelectShot: (nodeId: string) => void;
  onRunBreakdown: () => void;
  onRunPipeline: () => void;
  onGenerateCharacter: (nodeId: string) => void;
  onGenerateScene: (nodeId: string) => void;
  onGenerateShotAudio: (nodeId: string) => void;
  onGenerateShotKeyframe: (nodeId: string) => void;
  onGenerateShotVideo: (nodeId: string) => void;
  onOpenPlayer: () => void;
  onExportVideo: () => void;
  onExportZip: () => void;
}

const STEPS = [
  { id: 1, label: "1. 剧本大纲" },
  { id: 2, label: "2. 角色场景锚点" },
  { id: 3, label: "3. 分镜剧本列表" },
  { id: 4, label: "4. 批量生成监控" },
  { id: 5, label: "5. 时间轴与导出" },
];

export const DramaWizardView: React.FC<DramaWizardViewProps> = ({
  nodes,
  updateNodeData,
  updateScriptNode,
  addNode,
  removeNode,
  onSelectShot,
  onRunBreakdown,
  onRunPipeline,
  onGenerateCharacter,
  onGenerateScene,
  onGenerateShotAudio,
  onGenerateShotKeyframe,
  onGenerateShotVideo,
  onOpenPlayer,
  onExportVideo,
  onExportZip,
}) => {
  const [step, setStep] = useState(1);

  const scriptNode = nodes.find((n) => n.type === "script") as (DramaFlowNode & { data: ScriptNodeData }) | undefined;
  const composerNode = nodes.find((n) => n.type === "composer") as
    | (DramaFlowNode & { data: ComposerNodeData })
    | undefined;
  const characters = nodes.filter((n) => n.type === "character") as Array<DramaFlowNode & { data: CharacterNodeData }>;
  const scenes = nodes.filter((n) => n.type === "scene") as Array<DramaFlowNode & { data: SceneNodeData }>;
  const shots = (nodes.filter((n) => n.type === "shot") as Array<DramaFlowNode & { data: ShotNodeData }>).sort(
    (a, b) => a.data.index - b.data.index,
  );

  return (
    <div className="flex h-full flex-col overflow-hidden bg-[var(--bg-canvas)] text-[var(--text-primary)]">
      {/* 5步切换导航 */}
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--border-default)] bg-[var(--bg-surface)] px-5 py-2.5">
        <div className="flex flex-wrap items-center gap-1.5">
          {STEPS.map((s) => (
            <button
              key={s.id}
              type="button"
              onClick={() => setStep(s.id)}
              className={`rounded-lg px-3 py-1.5 text-xs font-medium transition ${
                step === s.id
                  ? "bg-indigo-600 text-white shadow-sm"
                  : "bg-[var(--bg-muted)] text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
              }`}
            >
              {s.label}
            </button>
          ))}
        </div>
        <div className="text-[11px] text-[var(--text-muted)]">
          当前数据与「无限画布模式」实时双向同步
        </div>
      </div>

      <div className="flex-1 overflow-y-auto p-6">
        <div className="mx-auto max-w-5xl space-y-5">
          {step === 1 && scriptNode && (
            <div className="rounded-2xl border border-[var(--border-default)] bg-[var(--bg-surface)] p-6 space-y-4">
              <h3 className="text-base font-semibold">故事梗概与全片视觉基调</h3>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                <label className="space-y-1 text-xs">
                  <span className="text-[var(--text-muted)]">短剧标题</span>
                  <input
                    type="text"
                    value={scriptNode.data.title}
                    onChange={(e) => updateScriptNode({ title: e.target.value })}
                    className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-3 py-2"
                  />
                </label>
                <label className="space-y-1 text-xs">
                  <span className="text-[var(--text-muted)]">题材</span>
                  <input
                    type="text"
                    value={scriptNode.data.genre}
                    onChange={(e) => updateScriptNode({ genre: e.target.value })}
                    className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-3 py-2"
                  />
                </label>
                <label className="space-y-1 text-xs">
                  <span className="text-[var(--text-muted)]">全片画风锁定</span>
                  <select
                    value={STYLE_PRESETS.includes(scriptNode.data.stylePreset) ? scriptNode.data.stylePreset : "custom"}
                    onChange={(e) => e.target.value !== "custom" && updateScriptNode({ stylePreset: e.target.value })}
                    className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-3 py-2"
                  >
                    {STYLE_PRESETS.map((st) => (
                      <option key={st} value={st}>
                        {st.split(" (")[0]}
                      </option>
                    ))}
                  </select>
                </label>
              </div>

              <label className="block space-y-1 text-xs">
                <span className="text-[var(--text-muted)]">剧情大纲</span>
                <textarea
                  rows={5}
                  value={scriptNode.data.outline}
                  onChange={(e) => updateScriptNode({ outline: e.target.value })}
                  className="w-full rounded-xl border border-[var(--border-default)] bg-[var(--bg-input)] p-3 leading-relaxed"
                />
              </label>

              <div className="flex flex-wrap justify-between gap-2">
                <button
                  type="button"
                  onClick={onRunBreakdown}
                  className="inline-flex items-center gap-1.5 rounded-lg bg-indigo-600 px-4 py-2 text-xs font-medium text-white hover:bg-indigo-500"
                >
                  <Sparkles className="h-4 w-4" />
                  AI 自动拆解角色与分镜
                </button>
                <button
                  type="button"
                  onClick={() => setStep(2)}
                  className="rounded-lg border border-[var(--border-default)] px-4 py-2 text-xs font-medium hover:bg-[var(--bg-surface-hover)]"
                >
                  下一步：角色场景锚点 →
                </button>
              </div>
            </div>
          )}

          {step === 2 && (
            <div className="space-y-5">
              <div className="flex items-center justify-between">
                <h3 className="text-base font-semibold">角色视觉锚点与专属声线 ({characters.length})</h3>
                <div className="flex gap-2">
                  <button
                    type="button"
                    onClick={() => addNode("character")}
                    className="inline-flex items-center gap-1 rounded-lg border border-[var(--border-default)] bg-[var(--bg-surface)] px-3 py-1.5 text-xs hover:bg-[var(--bg-surface-hover)]"
                  >
                    <Plus className="h-3.5 w-3.5" /> 新增角色
                  </button>
                  <button
                    type="button"
                    onClick={() => addNode("scene")}
                    className="inline-flex items-center gap-1 rounded-lg border border-[var(--border-default)] bg-[var(--bg-surface)] px-3 py-1.5 text-xs hover:bg-[var(--bg-surface-hover)]"
                  >
                    <Plus className="h-3.5 w-3.5" /> 新增场景
                  </button>
                </div>
              </div>

              <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                {characters.map((ch) => (
                  <div
                    key={ch.id}
                    className="flex gap-3 rounded-xl border border-[var(--border-default)] bg-[var(--bg-surface)] p-4"
                  >
                    <div className="h-24 w-24 shrink-0 overflow-hidden rounded-lg border border-[var(--border-default)] bg-[var(--bg-muted)] flex items-center justify-center">
                      {ch.data.portraitUrl ? (
                        <img src={toThumbImageUrl(ch.data.portraitUrl, 480)} alt={ch.data.name} loading="lazy" decoding="async" className="h-full w-full object-cover" />
                      ) : (
                        <span className="text-xs text-[var(--text-muted)]">无定妆图</span>
                      )}
                    </div>
                    <div className="min-w-0 flex-1 space-y-2 text-xs">
                      <div className="flex items-center justify-between">
                        <span className="font-semibold">
                          {ch.data.name} <span className="text-[var(--text-muted)]">({ch.data.role})</span>
                        </span>
                        <button
                          type="button"
                          onClick={() => removeNode(ch.id)}
                          className="text-rose-500 hover:underline"
                        >
                          删除
                        </button>
                      </div>
                      <textarea
                        rows={2}
                        value={ch.data.appearanceBible}
                        onChange={(e) =>
                          updateNodeData<CharacterNodeData>(ch.id, { appearanceBible: e.target.value })
                        }
                        className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] p-1.5 text-[11px]"
                      />
                      <div className="flex items-center justify-between">
                        <span className="truncate text-[11px] text-indigo-500">{ch.data.voiceName}</span>
                        <button
                          type="button"
                          onClick={() => onGenerateCharacter(ch.id)}
                          disabled={ch.data.status === "running"}
                          className="inline-flex items-center gap-1 rounded-md bg-violet-600 px-2.5 py-1 text-[11px] text-white hover:bg-violet-500 disabled:opacity-50"
                        >
                          <RefreshCw className={`h-3 w-3 ${ch.data.status === "running" ? "animate-spin" : ""}`} />
                          生成定妆
                        </button>
                      </div>
                    </div>
                  </div>
                ))}
              </div>

              {scenes.length > 0 && (
                <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                  {scenes.map((sc) => (
                    <div
                      key={sc.id}
                      className="flex gap-3 rounded-xl border border-[var(--border-default)] bg-[var(--bg-surface)] p-4 text-xs"
                    >
                      <div className="h-24 w-28 shrink-0 overflow-hidden rounded-lg border border-[var(--border-default)] bg-[var(--bg-muted)] flex items-center justify-center">
                        {sc.data.imageUrl ? (
                          <img src={toThumbImageUrl(sc.data.imageUrl, 480)} alt={sc.data.name} loading="lazy" decoding="async" className="h-full w-full object-cover" />
                        ) : (
                          <span className="text-[11px] text-[var(--text-muted)]">空镜图</span>
                        )}
                      </div>
                      <div className="flex-1 space-y-1.5">
                        <div className="font-semibold">{sc.data.name}</div>
                        <p className="line-clamp-2 text-[11px] text-[var(--text-secondary)]">
                          {sc.data.environmentPrompt}
                        </p>
                        <button
                          type="button"
                          onClick={() => onGenerateScene(sc.id)}
                          disabled={sc.data.status === "running"}
                          className="inline-flex items-center gap-1 rounded-md bg-sky-600 px-2.5 py-1 text-[11px] text-white hover:bg-sky-500"
                        >
                          <ImageIcon className="h-3 w-3" /> 生成空镜
                        </button>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}

          {step === 3 && (
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <h3 className="text-base font-semibold">分镜列表详情 ({shots.length} 镜)</h3>
                <button
                  type="button"
                  onClick={() => addNode("shot")}
                  className="inline-flex items-center gap-1 rounded-lg bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-500"
                >
                  <Plus className="h-3.5 w-3.5" /> 新增分镜镜头
                </button>
              </div>
              {shots.map((sh) => {
                const branch = getActiveShotBranch(sh.data);
                return (
                  <div
                    key={sh.id}
                    className="grid grid-cols-1 gap-4 rounded-xl border border-[var(--border-default)] bg-[var(--bg-surface)] p-4 md:grid-cols-[160px_1fr]"
                  >
                    <div className="h-36 overflow-hidden rounded-lg border border-[var(--border-default)] bg-zinc-950 flex items-center justify-center">
                      {branch.imageUrl ? (
                        <img src={toThumbImageUrl(branch.imageUrl, 480)} alt={`Shot ${sh.data.index}`} loading="lazy" decoding="async" className="h-full w-full object-cover" />
                      ) : (
                        <span className="text-[11px] text-zinc-400">#{sh.data.index} 待生图</span>
                      )}
                    </div>
                    <div className="space-y-2 text-xs">
                      <div className="flex items-center justify-between">
                        <span className="font-semibold">
                          第 #{sh.data.index} 镜 · {sh.data.shotType} · {sh.data.cameraMove}
                        </span>
                        <div className="flex items-center gap-2">
                          <button
                            type="button"
                            onClick={() => {
                              onSelectShot(sh.id);
                            }}
                            className="text-indigo-500 hover:underline"
                          >
                            在画布中定位
                          </button>
                          <button
                            type="button"
                            onClick={() => removeNode(sh.id)}
                            className="text-rose-500 hover:underline"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </button>
                        </div>
                      </div>
                      <input
                        type="text"
                        value={sh.data.dialogue}
                        onChange={(e) => updateNodeData<ShotNodeData>(sh.id, { dialogue: e.target.value })}
                        className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2.5 py-1.5"
                      />
                      <textarea
                        rows={2}
                        value={sh.data.sceneAction}
                        onChange={(e) => updateNodeData<ShotNodeData>(sh.id, { sceneAction: e.target.value })}
                        className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] p-2"
                      />
                      <div className="flex flex-wrap gap-2">
                        <button
                          type="button"
                          onClick={() => onGenerateShotAudio(sh.id)}
                          className="inline-flex items-center gap-1 rounded border border-[var(--border-default)] px-2.5 py-1 hover:bg-[var(--bg-surface-hover)]"
                        >
                          <Mic className="h-3 w-3 text-emerald-500" /> 配音
                        </button>
                        <button
                          type="button"
                          onClick={() => onGenerateShotKeyframe(sh.id)}
                          className="inline-flex items-center gap-1 rounded border border-[var(--border-default)] px-2.5 py-1 hover:bg-[var(--bg-surface-hover)]"
                        >
                          <ImageIcon className="h-3 w-3 text-indigo-500" /> 首帧
                        </button>
                        <button
                          type="button"
                          onClick={() => onGenerateShotVideo(sh.id)}
                          className="inline-flex items-center gap-1 rounded border border-[var(--border-default)] px-2.5 py-1 hover:bg-[var(--bg-surface-hover)]"
                        >
                          <Video className="h-3 w-3 text-amber-500" /> 出片
                        </button>
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          )}

          {step === 4 && (
            <div className="rounded-2xl border border-[var(--border-default)] bg-[var(--bg-surface)] p-6 space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="text-base font-semibold">全片 DAG 并行/顺序生成进度监控</h3>
                  <p className="text-xs text-[var(--text-muted)]">
                    系统自动锁定角色首帧并按镜头序号传承光影构图，无视频算力时自动诚实切换 Ken Burns 高清运镜。
                  </p>
                </div>
                <button
                  type="button"
                  onClick={onRunPipeline}
                  className="inline-flex items-center gap-1.5 rounded-lg bg-indigo-600 px-4 py-2 text-xs font-medium text-white hover:bg-indigo-500"
                >
                  <Play className="h-3.5 w-3.5" />
                  一键补全全部未完成节点
                </button>
              </div>
              <div className="divide-y divide-[var(--border-default)] rounded-xl border border-[var(--border-default)]">
                {shots.map((sh) => {
                  const br = getActiveShotBranch(sh.data);
                  return (
                    <div key={sh.id} className="flex items-center justify-between px-4 py-2.5 text-xs">
                      <div className="flex items-center gap-2 min-w-0">
                        <span className="font-mono font-bold text-amber-500">#{sh.data.index}</span>
                        <span className="truncate">{sh.data.dialogue || sh.data.sceneAction}</span>
                      </div>
                      <div className="flex items-center gap-3 shrink-0">
                        <span>首帧: {br.keyframeStatus}</span>
                        <span>音轨: {br.audioStatus}</span>
                        <span>成片: {br.videoStatus}</span>
                        {(br.videoStatus === "ready" || br.videoStatus === "ken_burns") && (
                          <CheckCircle2 className="h-4 w-4 text-emerald-500" />
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {step === 5 && (
            <div className="rounded-2xl border border-[var(--border-default)] bg-[var(--bg-surface)] p-6 space-y-4">
              <h3 className="text-base font-semibold">最终时间轴合成与工程导出</h3>
              <p className="text-xs text-[var(--text-muted)]">
                {composerNode?.data.exportStageText || "支持多轨连播监视器、浏览器纯前端逐帧合成导出单个连续视频文件，以及下载完整 ZIP 工程包（含 SRT 字幕与剪映草稿）。"}
              </p>
              <div className="flex flex-wrap gap-3">
                <button
                  type="button"
                  onClick={onOpenPlayer}
                  className="inline-flex items-center gap-1.5 rounded-lg bg-emerald-600 px-4 py-2 text-xs font-medium text-white hover:bg-emerald-500"
                >
                  <Play className="h-4 w-4" /> 打开多轨同步预览器
                </button>
                <button
                  type="button"
                  onClick={onExportVideo}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-[var(--border-default)] px-4 py-2 text-xs font-medium hover:bg-[var(--bg-surface-hover)]"
                >
                  <Film className="h-4 w-4 text-emerald-500" /> 合成连续视频成片
                </button>
                <button
                  type="button"
                  onClick={onExportZip}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-[var(--border-default)] px-4 py-2 text-xs font-medium hover:bg-[var(--bg-surface-hover)]"
                >
                  <Layers className="h-4 w-4 text-sky-500" /> 打包完整工程 ZIP
                </button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
