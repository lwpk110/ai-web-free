import {
  ArrowDown,
  ArrowUp,
  Copy,
  Film,
  GitBranch,
  Image as ImageIcon,
  Layers,
  Mic,
  Play,
  RefreshCw,
  ScrollText,
  Sparkles,
  Trash2,
  Upload,
  Video,
  Wand2,
} from "lucide-react";
import React, { useState } from "react";
import { cacheDramaMedia, type DramaCapabilities, type VoiceDescriptor } from "../../api/client";
import { cloneVoiceForCharacter, designVoiceForCharacter } from "../../lib/dramaClientActions";
import {
  BUILTIN_VOICE_OPTIONS,
  CAMERA_MOTIONS,
  getActiveShotBranch,
  STYLE_PRESETS,
  type AudioImportNodeData,
  type CharacterNodeData,
  type ComposerNodeData,
  type DramaFlowNode,
  type SceneNodeData,
  type ScriptNodeData,
  type ShotNodeData,
} from "../../lib/dramaPresets";
import { useDramaCanvasStore } from "../../stores/dramaCanvasStore";

interface DramaNodeInspectorProps {
  selectedNode: DramaFlowNode | undefined;
  capabilities: DramaCapabilities | null;
  voices: VoiceDescriptor[];
  onRunBreakdown: () => void;
  onGenerateCharacter: (nodeId: string) => void;
  onGenerateScene: (nodeId: string) => void;
  onGenerateShotAudio: (nodeId: string) => void;
  onGenerateShotKeyframe: (nodeId: string) => void;
  onGenerateShotVideo: (nodeId: string) => void;
  onTranscribeAudio: (nodeId: string) => void;
  onOpenPlayer: () => void;
  onExportVideo: () => void;
  onExportZip: () => void;
}

function fileToDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onloadend = () => resolve(String(r.result || ""));
    r.onerror = () => reject(r.error);
    r.readAsDataURL(file);
  });
}

export const DramaNodeInspector: React.FC<DramaNodeInspectorProps> = ({
  selectedNode,
  capabilities,
  voices,
  onRunBreakdown,
  onGenerateCharacter,
  onGenerateScene,
  onGenerateShotAudio,
  onGenerateShotKeyframe,
  onGenerateShotVideo,
  onTranscribeAudio,
  onOpenPlayer,
  onExportVideo,
  onExportZip,
}) => {
  const {
    nodes,
    updateNodeData,
    removeNode,
    duplicateShotNode,
    moveShotOrder,
    addShotVersionBranch,
    switchShotVersionBranch,
  } = useDramaCanvasStore();

  const [voiceDesignOpen, setVoiceDesignOpen] = useState(false);
  const [voiceDesignPrompt, setVoiceDesignPrompt] = useState("成熟磁性低沉男性配音，富有戏剧张力");
  const [voiceBusy, setVoiceBusy] = useState(false);
  const [voiceMsg, setVoiceMsg] = useState<string | null>(null);
  const [cloneTargetCharId, setCloneTargetCharId] = useState("");

  if (!selectedNode) {
    return (
      <div className="flex h-full flex-col items-center justify-center p-6 text-center text-xs text-[var(--text-muted)]">
        <ScrollText className="mb-2 h-8 w-8 opacity-40" />
        点击画布中的任意节点，即可在此微调提示词、音色、机位运镜、A/B 分支与局部重生成。
      </div>
    );
  }

  const characters = nodes.filter((n) => n.type === "character") as Array<DramaFlowNode & { data: CharacterNodeData }>;
  const scenes = nodes.filter((n) => n.type === "scene") as Array<DramaFlowNode & { data: SceneNodeData }>;
  const shots = (nodes.filter((n) => n.type === "shot") as Array<DramaFlowNode & { data: ShotNodeData }>).sort(
    (a, b) => a.data.index - b.data.index,
  );

  if (selectedNode.type === "script") {
    const d = selectedNode.data;
    return (
      <div className="space-y-3 p-4 text-xs">
        <div className="flex items-center justify-between">
          <span className="font-semibold text-sm">剧本总纲配置</span>
          <span className="rounded bg-indigo-500/10 px-2 py-0.5 text-[10px] text-indigo-500">源节点</span>
        </div>

        <label className="block space-y-1">
          <span className="text-[11px] text-[var(--text-muted)]">短剧剧名</span>
          <input
            type="text"
            value={d.title}
            onChange={(e) => updateNodeData<ScriptNodeData>(selectedNode.id, { title: e.target.value })}
            className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2.5 py-1.5"
          />
        </label>

        <div className="grid grid-cols-2 gap-2">
          <label className="block space-y-1">
            <span className="text-[11px] text-[var(--text-muted)]">题材</span>
            <input
              type="text"
              value={d.genre}
              onChange={(e) => updateNodeData<ScriptNodeData>(selectedNode.id, { genre: e.target.value })}
              className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2.5 py-1.5"
            />
          </label>
          <label className="block space-y-1">
            <span className="text-[11px] text-[var(--text-muted)]">画幅比例</span>
            <select
              value={d.aspectRatio}
              onChange={(e) =>
                updateNodeData<ScriptNodeData>(selectedNode.id, {
                  aspectRatio: e.target.value as ScriptNodeData["aspectRatio"],
                })
              }
              className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2 py-1.5"
            >
              <option value="9:16">9:16 竖屏短剧</option>
              <option value="16:9">16:9 横屏影视</option>
              <option value="1:1">1:1 方形画幅</option>
            </select>
          </label>
        </div>

        <label className="block space-y-1">
          <span className="text-[11px] text-[var(--text-muted)]">全片视觉画风锁</span>
          <select
            value={STYLE_PRESETS.includes(d.stylePreset) ? d.stylePreset : "custom"}
            onChange={(e) => {
              if (e.target.value !== "custom") {
                updateNodeData<ScriptNodeData>(selectedNode.id, { stylePreset: e.target.value });
              }
            }}
            className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2 py-1.5"
          >
            {STYLE_PRESETS.map((s) => (
              <option key={s} value={s}>
                {s.split(" (")[0]}
              </option>
            ))}
            {!STYLE_PRESETS.includes(d.stylePreset) && <option value="custom">自定义风格</option>}
          </select>
        </label>

        <div className="grid grid-cols-2 gap-2">
          <label className="block space-y-1">
            <span className="text-[11px] text-[var(--text-muted)]">生图模型覆盖</span>
            <select
              value={d.imageModelOverride || ""}
              onChange={(e) => updateNodeData<ScriptNodeData>(selectedNode.id, { imageModelOverride: e.target.value || undefined })}
              className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2 py-1.5"
            >
              <option value="">自动路由 ({capabilities?.recommended?.[d.routingPolicy]?.image_model || "auto"})</option>
              <option value="flux-schnell">FLUX.1-schnell</option>
              <option value="wanx-v1">通义万相 Wanx-v1</option>
              <option value="jimeng-2.1">字节即梦 2.1</option>
              <option value="gpt-image-1">ChatGPT gpt-image-1</option>
            </select>
          </label>
          <label className="block space-y-1">
            <span className="text-[11px] text-[var(--text-muted)]">视频模型覆盖</span>
            <select
              value={d.videoModelOverride || ""}
              onChange={(e) => updateNodeData<ScriptNodeData>(selectedNode.id, { videoModelOverride: e.target.value || undefined })}
              className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2 py-1.5"
            >
              <option value="">自动路由</option>
              <option value="THUDM/CogVideoX-5b">CogVideoX-5B</option>
              <option value="wan-video-v1">通义万相 Wan2.1</option>
              <option value="muse-video">Meta Muse Video</option>
              <option value="muse-talk">Meta MuseTalk (对口型)</option>
              <option value="jimeng-video-2.0">字节即梦 2.0</option>
            </select>
          </label>
        </div>

        <label className="block space-y-1">
          <span className="text-[11px] text-[var(--text-muted)]">故事大纲 / 剧本梗概</span>
          <textarea
            rows={5}
            value={d.outline}
            onChange={(e) => updateNodeData<ScriptNodeData>(selectedNode.id, { outline: e.target.value })}
            className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] p-2.5 leading-relaxed"
          />
        </label>

        <button
          type="button"
          onClick={onRunBreakdown}
          disabled={d.status === "running"}
          className="w-full inline-flex items-center justify-center gap-1.5 rounded-lg bg-indigo-600 py-2 font-medium text-white hover:bg-indigo-500 disabled:opacity-50"
        >
          <Sparkles className="h-3.5 w-3.5" />
          AI 导演自动拆解角色/场景与分镜
        </button>
      </div>
    );
  }

  if (selectedNode.type === "character") {
    const d = selectedNode.data;
    const mergedVoiceList = [
      ...voices
        .filter((v) => v.provider === "custom")
        .map((v) => ({
          id: v.id,
          name: `⭐ ${v.name} (系统克隆库${v.available === false ? " · 需云端Key复刻" : ""})`,
          engine: "edge-tts" as CharacterNodeData["voiceEngine"],
        })),
      ...BUILTIN_VOICE_OPTIONS,
      ...voices
        .filter((v) => v.provider !== "custom" && !BUILTIN_VOICE_OPTIONS.some((b) => b.id === v.id))
        .map((v) => ({
          id: v.id,
          name: `${v.name} (${v.provider === "siliconflow" ? "硅基CosyVoice2" : v.provider === "dashscope" ? "百炼CosyVoice" : v.provider})`,
          engine: (v.provider as CharacterNodeData["voiceEngine"]) || "edge-tts",
        })),
    ];

    return (
      <div className="space-y-3 p-4 text-xs">
        <div className="flex items-center justify-between">
          <span className="font-semibold text-sm">角色锚点属性</span>
          <button
            type="button"
            onClick={() => removeNode(selectedNode.id)}
            className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-rose-500 hover:bg-rose-500/10"
          >
            <Trash2 className="h-3.5 w-3.5" />
            删除
          </button>
        </div>

        <div className="grid grid-cols-2 gap-2">
          <label className="block space-y-1">
            <span className="text-[11px] text-[var(--text-muted)]">角色名</span>
            <input
              type="text"
              value={d.name}
              onChange={(e) => updateNodeData<CharacterNodeData>(selectedNode.id, { name: e.target.value })}
              className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2.5 py-1.5"
            />
          </label>
          <label className="block space-y-1">
            <span className="text-[11px] text-[var(--text-muted)]">剧组定位</span>
            <input
              type="text"
              value={d.role}
              onChange={(e) => updateNodeData<CharacterNodeData>(selectedNode.id, { role: e.target.value })}
              className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2.5 py-1.5"
            />
          </label>
        </div>

        <label className="block space-y-1">
          <span className="text-[11px] text-[var(--text-muted)]">外貌/穿搭视觉圣经（跨镜一致性基准）</span>
          <textarea
            rows={4}
            value={d.appearanceBible}
            onChange={(e) => updateNodeData<CharacterNodeData>(selectedNode.id, { appearanceBible: e.target.value })}
            className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] p-2 leading-relaxed"
          />
        </label>

        {d.portraitUrl && (
          <div className="overflow-hidden rounded-lg border border-[var(--border-default)] bg-black/20">
            <img src={d.portraitUrl} alt={d.name} className="max-h-52 w-full object-cover" />
          </div>
        )}

        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => onGenerateCharacter(selectedNode.id)}
            disabled={d.status === "running"}
            className="flex-1 inline-flex items-center justify-center gap-1 rounded-lg bg-violet-600 py-2 font-medium text-white hover:bg-violet-500 disabled:opacity-50"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${d.status === "running" ? "animate-spin" : ""}`} />
            {d.portraitUrl ? "重绘定妆锚点图" : "生成定妆锚点图"}
          </button>
          <label className="cursor-pointer inline-flex items-center gap-1 rounded-lg border border-[var(--border-default)] px-2.5 py-2 hover:bg-[var(--bg-surface-hover)]">
            <Upload className="h-3.5 w-3.5" />
            上传
            <input
              type="file"
              accept="image/*"
              className="hidden"
              onChange={async (e) => {
                const f = e.target.files?.[0];
                if (!f) return;
                const dataUrl = await fileToDataUrl(f);
                const cached = await cacheDramaMedia({ data_url: dataUrl, category: "images" }).catch(() => ({
                  local_url: dataUrl,
                }));
                updateNodeData<CharacterNodeData>(selectedNode.id, { portraitUrl: cached.local_url, status: "ready" });
              }}
            />
          </label>
        </div>

        <label className="block space-y-1">
          <span className="text-[11px] text-[var(--text-muted)]">专属配音音色</span>
          <select
            value={d.voiceId}
            onChange={(e) => {
              const chosen = mergedVoiceList.find((v) => v.id === e.target.value);
              updateNodeData<CharacterNodeData>(selectedNode.id, {
                voiceId: e.target.value,
                voiceName: chosen?.name || e.target.value,
                voiceEngine: chosen?.engine || "edge-tts",
              });
            }}
            className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2 py-1.5"
          >
            {mergedVoiceList.map((v) => (
              <option key={v.id} value={v.id}>
                {v.name}
              </option>
            ))}
          </select>
        </label>

        <div className="grid grid-cols-2 gap-2">
          <label className="block space-y-1">
            <span className="text-[11px] text-[var(--text-muted)]">语速 ({d.speed}x)</span>
            <input
              type="range"
              min="0.7"
              max="1.5"
              step="0.05"
              value={d.speed}
              onChange={(e) => updateNodeData<CharacterNodeData>(selectedNode.id, { speed: Number(e.target.value) })}
              className="w-full"
            />
          </label>
          <div className="flex items-end">
            <button
              type="button"
              onClick={() => setVoiceDesignOpen((v) => !v)}
              className="w-full inline-flex items-center justify-center gap-1 rounded-lg border border-[var(--border-default)] py-1.5 hover:bg-[var(--bg-surface-hover)]"
            >
              <Wand2 className="h-3.5 w-3.5 text-amber-500" />
              CosyVoice 定制音色
            </button>
          </div>
        </div>

        {voiceDesignOpen && (
          <div className="space-y-2 rounded-lg border border-[var(--border-default)] bg-[var(--bg-muted)] p-2.5">
            <div className="text-[11px] font-medium">描述理想音色（需百炼 Key）</div>
            <textarea
              rows={2}
              value={voiceDesignPrompt}
              onChange={(e) => setVoiceDesignPrompt(e.target.value)}
              className="w-full rounded border border-[var(--border-default)] bg-[var(--bg-surface)] p-1.5"
            />
            {voiceMsg && <div className="text-[10px] text-amber-600">{voiceMsg}</div>}
            <button
              type="button"
              disabled={voiceBusy || !capabilities?.tts?.voice_design_available}
              onClick={async () => {
                setVoiceBusy(true);
                setVoiceMsg(null);
                try {
                  const vid = await designVoiceForCharacter(selectedNode.id, voiceDesignPrompt);
                  setVoiceMsg(`定制音色已绑定: ${vid}`);
                } catch (err) {
                  setVoiceMsg(err instanceof Error ? err.message : String(err));
                } finally {
                  setVoiceBusy(false);
                }
              }}
              className="w-full rounded bg-amber-600 py-1.5 text-white hover:bg-amber-500 disabled:opacity-50"
            >
              {capabilities?.tts?.voice_design_available ? "生成并绑定音色" : "请先配置百炼 Key"}
            </button>
          </div>
        )}
      </div>
    );
  }

  if (selectedNode.type === "scene") {
    const d = selectedNode.data;
    return (
      <div className="space-y-3 p-4 text-xs">
        <div className="flex items-center justify-between">
          <span className="font-semibold text-sm">场景环境锚点</span>
          <button
            type="button"
            onClick={() => removeNode(selectedNode.id)}
            className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-rose-500 hover:bg-rose-500/10"
          >
            <Trash2 className="h-3.5 w-3.5" />
            删除
          </button>
        </div>

        <label className="block space-y-1">
          <span className="text-[11px] text-[var(--text-muted)]">场景名称</span>
          <input
            type="text"
            value={d.name}
            onChange={(e) => updateNodeData<SceneNodeData>(selectedNode.id, { name: e.target.value })}
            className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2.5 py-1.5"
          />
        </label>

        <label className="block space-y-1">
          <span className="text-[11px] text-[var(--text-muted)]">空间布置与氛围描述</span>
          <textarea
            rows={4}
            value={d.environmentPrompt}
            onChange={(e) => updateNodeData<SceneNodeData>(selectedNode.id, { environmentPrompt: e.target.value })}
            className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] p-2 leading-relaxed"
          />
        </label>

        <label className="block space-y-1">
          <span className="text-[11px] text-[var(--text-muted)]">光影色调设定</span>
          <input
            type="text"
            value={d.lightingStyle}
            onChange={(e) => updateNodeData<SceneNodeData>(selectedNode.id, { lightingStyle: e.target.value })}
            className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2.5 py-1.5"
          />
        </label>

        {d.imageUrl && (
          <img src={d.imageUrl} alt={d.name} className="max-h-48 w-full rounded-lg border border-[var(--border-default)] object-cover" />
        )}

        <button
          type="button"
          onClick={() => onGenerateScene(selectedNode.id)}
          disabled={d.status === "running"}
          className="w-full inline-flex items-center justify-center gap-1.5 rounded-lg bg-sky-600 py-2 font-medium text-white hover:bg-sky-500 disabled:opacity-50"
        >
          <ImageIcon className="h-3.5 w-3.5" />
          {d.imageUrl ? "重绘空镜锚点图" : "生成空镜锚点图"}
        </button>
      </div>
    );
  }

  if (selectedNode.type === "shot") {
    const d = selectedNode.data;
    const branch = getActiveShotBranch(d);
    return (
      <div className="space-y-3 p-4 text-xs">
        <div className="flex items-center justify-between">
          <span className="font-semibold text-sm">第 #{d.index} 镜分镜属性</span>
          <div className="flex items-center gap-1">
            <button
              type="button"
              title="上移镜头"
              onClick={() => moveShotOrder(selectedNode.id, "up")}
              className="rounded p-1 hover:bg-[var(--bg-surface-hover)]"
            >
              <ArrowUp className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              title="下移镜头"
              onClick={() => moveShotOrder(selectedNode.id, "down")}
              className="rounded p-1 hover:bg-[var(--bg-surface-hover)]"
            >
              <ArrowDown className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              title="复制克隆镜头"
              onClick={() => duplicateShotNode(selectedNode.id)}
              className="rounded p-1 hover:bg-[var(--bg-surface-hover)]"
            >
              <Copy className="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              title="删除镜头"
              onClick={() => removeNode(selectedNode.id)}
              className="rounded p-1 text-rose-500 hover:bg-rose-500/10"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </button>
          </div>
        </div>

        {/* A/B 分支 */}
        <div className="rounded-lg border border-[var(--border-default)] bg-[var(--bg-muted)] p-2">
          <div className="mb-1.5 flex items-center justify-between">
            <span className="text-[11px] font-medium">A/B 平行实验版本</span>
            <button
              type="button"
              onClick={() => addShotVersionBranch(selectedNode.id)}
              className="inline-flex items-center gap-1 rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-medium text-amber-600 dark:text-amber-400"
            >
              <GitBranch className="h-3 w-3" />
              新建对比分支
            </button>
          </div>
          <div className="flex flex-wrap gap-1">
            {d.versions.map((ver) => (
              <button
                key={ver.id}
                type="button"
                onClick={() => switchShotVersionBranch(selectedNode.id, ver.id)}
                className={`rounded px-2 py-0.5 text-[11px] ${
                  ver.id === branch.id
                    ? "bg-amber-500 text-white font-medium"
                    : "bg-[var(--bg-surface)] text-[var(--text-secondary)]"
                }`}
              >
                {ver.label}
              </button>
            ))}
          </div>
        </div>

        <div className="grid grid-cols-2 gap-2">
          <label className="block space-y-1">
            <span className="text-[11px] text-[var(--text-muted)]">主讲/出场角色</span>
            <select
              value={d.speakerId}
              onChange={(e) => updateNodeData<ShotNodeData>(selectedNode.id, { speakerId: e.target.value })}
              className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2 py-1.5"
            >
              {characters.map((ch) => (
                <option key={ch.id} value={ch.data.charId}>
                  {ch.data.name}
                </option>
              ))}
            </select>
          </label>
          <label className="block space-y-1">
            <span className="text-[11px] text-[var(--text-muted)]">所在场景</span>
            <select
              value={d.sceneId || ""}
              onChange={(e) => updateNodeData<ShotNodeData>(selectedNode.id, { sceneId: e.target.value || undefined })}
              className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2 py-1.5"
            >
              <option value="">不限定场景</option>
              {scenes.map((sc) => (
                <option key={sc.id} value={sc.data.sceneId}>
                  {sc.data.name}
                </option>
              ))}
            </select>
          </label>
        </div>

        <div className="grid grid-cols-2 gap-2">
          <label className="block space-y-1">
            <span className="text-[11px] text-[var(--text-muted)]">景别</span>
            <select
              value={d.shotType}
              onChange={(e) => updateNodeData<ShotNodeData>(selectedNode.id, { shotType: e.target.value })}
              className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2 py-1.5"
            >
              <option value="全景">全景</option>
              <option value="中景">中景</option>
              <option value="特写">特写</option>
              <option value="过肩">过肩</option>
            </select>
          </label>
          <label className="block space-y-1">
            <span className="text-[11px] text-[var(--text-muted)]">转场衔接</span>
            <select
              value={d.transition}
              onChange={(e) =>
                updateNodeData<ShotNodeData>(selectedNode.id, {
                  transition: e.target.value as ShotNodeData["transition"],
                })
              }
              className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2 py-1.5"
            >
              <option value="crossfade">叠化淡入 (Crossfade)</option>
              <option value="cut">硬切 (Cut)</option>
              <option value="dip_black">黑场过渡 (Dip Black)</option>
            </select>
          </label>
        </div>

        <label className="block space-y-1">
          <span className="text-[11px] text-[var(--text-muted)]">镜头运动</span>
          <select
            value={d.cameraMove}
            onChange={(e) => updateNodeData<ShotNodeData>(selectedNode.id, { cameraMove: e.target.value })}
            className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2 py-1.5"
          >
            {CAMERA_MOTIONS.map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </select>
        </label>

        <label className="block space-y-1">
          <span className="text-[11px] text-[var(--text-muted)]">台词 / 旁白（决定真实时长与字幕）</span>
          <textarea
            rows={2}
            value={d.dialogue}
            onChange={(e) => updateNodeData<ShotNodeData>(selectedNode.id, { dialogue: e.target.value })}
            className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] p-2"
          />
        </label>

        <label className="block space-y-1">
          <span className="text-[11px] text-[var(--text-muted)]">画面动作与构图提示词</span>
          <textarea
            rows={3}
            value={d.sceneAction}
            onChange={(e) => updateNodeData<ShotNodeData>(selectedNode.id, { sceneAction: e.target.value })}
            className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] p-2"
          />
        </label>

        <label className="block space-y-1">
          <span className="text-[11px] text-[var(--text-muted)]">跨镜视觉承接备注</span>
          <input
            type="text"
            value={d.continuityNotes || ""}
            onChange={(e) => updateNodeData<ShotNodeData>(selectedNode.id, { continuityNotes: e.target.value })}
            placeholder="例如：延续上一镜角色黑风衣与桌上的合同"
            className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] px-2.5 py-1.5"
          />
        </label>

        {branch.audioUrl && (
          <audio src={branch.audioUrl} controls className="h-8 w-full" />
        )}

        <div className="grid grid-cols-3 gap-1.5 pt-1">
          <button
            type="button"
            onClick={() => onGenerateShotAudio(selectedNode.id)}
            disabled={branch.audioStatus === "running"}
            className="inline-flex items-center justify-center gap-1 rounded-lg border border-[var(--border-default)] py-1.5 font-medium hover:bg-[var(--bg-surface-hover)] disabled:opacity-50"
          >
            <Mic className="h-3.5 w-3.5 text-emerald-500" />
            配音
          </button>
          <button
            type="button"
            onClick={() => onGenerateShotKeyframe(selectedNode.id)}
            disabled={branch.keyframeStatus === "running"}
            className="inline-flex items-center justify-center gap-1 rounded-lg border border-[var(--border-default)] py-1.5 font-medium hover:bg-[var(--bg-surface-hover)] disabled:opacity-50"
          >
            <ImageIcon className="h-3.5 w-3.5 text-indigo-500" />
            首帧
          </button>
          <button
            type="button"
            onClick={() => onGenerateShotVideo(selectedNode.id)}
            disabled={branch.videoStatus === "running"}
            className="inline-flex items-center justify-center gap-1 rounded-lg bg-amber-600 py-1.5 font-medium text-white hover:bg-amber-500 disabled:opacity-50"
          >
            <Video className="h-3.5 w-3.5" />
            出片
          </button>
        </div>
      </div>
    );
  }

  if (selectedNode.type === "audioImport") {
    const d = selectedNode.data;
    return (
      <div className="space-y-3 p-4 text-xs">
        <div className="flex items-center justify-between">
          <span className="font-semibold text-sm">参考音频 / Whisper 转写</span>
          <button
            type="button"
            onClick={() => removeNode(selectedNode.id)}
            className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-rose-500 hover:bg-rose-500/10"
          >
            <Trash2 className="h-3.5 w-3.5" />
            删除
          </button>
        </div>

        <label className="cursor-pointer flex items-center justify-center gap-1.5 rounded-lg border border-dashed border-[var(--border-strong)] py-3 hover:bg-[var(--bg-surface-hover)]">
          <Upload className="h-4 w-4 text-teal-500" />
          上传本地 MP3 / WAV / M4A
          <input
            type="file"
            accept="audio/*"
            className="hidden"
            onChange={async (e) => {
              const f = e.target.files?.[0];
              if (!f) return;
              const dataUrl = await fileToDataUrl(f);
              const cached = await cacheDramaMedia({ data_url: dataUrl, category: "audio" }).catch(() => ({
                local_url: dataUrl,
              }));
              updateNodeData<AudioImportNodeData>(selectedNode.id, {
                title: f.name,
                sourceAudioUrl: cached.local_url,
                status: "ready",
              });
            }}
          />
        </label>

        {d.sourceAudioUrl && <audio src={d.sourceAudioUrl} controls className="h-8 w-full" />}

        <button
          type="button"
          onClick={() => onTranscribeAudio(selectedNode.id)}
          disabled={!d.sourceAudioUrl || d.status === "running"}
          className="w-full inline-flex items-center justify-center gap-1.5 rounded-lg bg-teal-600 py-2 font-medium text-white hover:bg-teal-500 disabled:opacity-50"
        >
          <Sparkles className="h-3.5 w-3.5" />
          Whisper 转写并写入剧本大纲
        </button>

        {d.transcript && (
          <textarea
            rows={4}
            value={d.transcript}
            onChange={(e) => updateNodeData<AudioImportNodeData>(selectedNode.id, { transcript: e.target.value })}
            className="w-full rounded-lg border border-[var(--border-default)] bg-[var(--bg-input)] p-2"
          />
        )}

        {characters.length > 0 && d.sourceAudioUrl && (
          <div className="space-y-1.5 rounded-lg border border-[var(--border-default)] bg-[var(--bg-muted)] p-2.5">
            <div className="text-[11px] font-medium">声音复刻绑定到角色（需百炼 Key）</div>
            <select
              value={cloneTargetCharId || characters[0].id}
              onChange={(e) => setCloneTargetCharId(e.target.value)}
              className="w-full rounded border border-[var(--border-default)] bg-[var(--bg-surface)] px-2 py-1"
            >
              {characters.map((ch) => (
                <option key={ch.id} value={ch.id}>
                  {ch.data.name}
                </option>
              ))}
            </select>
            <button
              type="button"
              disabled={!capabilities?.tts?.voice_clone_available}
              onClick={async () => {
                const targetId = cloneTargetCharId || characters[0].id;
                try {
                  await cloneVoiceForCharacter(targetId, d.sourceAudioUrl!, "drama");
                } catch {
                  /* error stored or handled */
                }
              }}
              className="w-full rounded bg-indigo-600 py-1.5 text-white disabled:opacity-50"
            >
              复刻音色并绑定角色
            </button>
          </div>
        )}
      </div>
    );
  }

  // Composer node inspector
  const comp = selectedNode.data as ComposerNodeData;
  return (
    <div className="space-y-3 p-4 text-xs">
      <div className="font-semibold text-sm">成片合流输出台</div>

      <div className="rounded-lg border border-[var(--border-default)] bg-[var(--bg-muted)] p-2.5 space-y-1">
        <div className="flex justify-between">
          <span className="text-[var(--text-muted)]">时间轴镜头数</span>
          <span className="font-semibold">{shots.length} 镜</span>
        </div>
        <div className="flex justify-between">
          <span className="text-[var(--text-muted)]">预估成片总长</span>
          <span className="font-mono font-semibold">
            {shots.reduce((sum, s) => sum + (s.data.actualAudioDuration || s.data.estimatedDuration), 0).toFixed(1)}s
          </span>
        </div>
      </div>

      <label className="flex items-center justify-between rounded-lg border border-[var(--border-default)] px-2.5 py-2">
        <span>硬烧录中文字幕</span>
        <input
          type="checkbox"
          checked={comp.showSubtitles}
          onChange={(e) => updateNodeData<ComposerNodeData>(selectedNode.id, { showSubtitles: e.target.checked })}
        />
      </label>

      <div className="space-y-1.5 rounded-lg border border-[var(--border-default)] p-2.5">
        <div className="flex items-center justify-between">
          <span className="text-[11px] text-[var(--text-muted)]">背景配乐 BGM ({Math.round(comp.bgmVolume * 100)}%)</span>
          <label className="cursor-pointer text-[11px] text-indigo-500 hover:underline">
            上传音乐
            <input
              type="file"
              accept="audio/*"
              className="hidden"
              onChange={async (e) => {
                const f = e.target.files?.[0];
                if (!f) return;
                const dataUrl = await fileToDataUrl(f);
                const cached = await cacheDramaMedia({ data_url: dataUrl, category: "audio" }).catch(() => ({
                  local_url: dataUrl,
                }));
                updateNodeData<ComposerNodeData>(selectedNode.id, { bgmUrl: cached.local_url });
              }}
            />
          </label>
        </div>
        <input
          type="range"
          min="0"
          max="0.8"
          step="0.02"
          value={comp.bgmVolume}
          onChange={(e) => updateNodeData<ComposerNodeData>(selectedNode.id, { bgmVolume: Number(e.target.value) })}
          className="w-full"
        />
        {comp.bgmUrl && <audio src={comp.bgmUrl} controls className="h-8 w-full" />}
      </div>

      <button
        type="button"
        onClick={onOpenPlayer}
        className="w-full inline-flex items-center justify-center gap-1.5 rounded-lg bg-emerald-600 py-2 font-medium text-white hover:bg-emerald-500"
      >
        <Play className="h-3.5 w-3.5" />
        打开多轨同步预览器
      </button>
      <button
        type="button"
        onClick={onExportVideo}
        className="w-full inline-flex items-center justify-center gap-1.5 rounded-lg border border-[var(--border-default)] py-2 font-medium hover:bg-[var(--bg-surface-hover)]"
      >
        <Film className="h-3.5 w-3.5 text-emerald-500" />
        导出单文件完整成片
      </button>
      <button
        type="button"
        onClick={onExportZip}
        className="w-full inline-flex items-center justify-center gap-1.5 rounded-lg border border-[var(--border-default)] py-2 font-medium hover:bg-[var(--bg-surface-hover)]"
      >
        <Layers className="h-3.5 w-3.5 text-sky-500" />
        打包完整工程 ZIP（含剪映草稿）
      </button>
    </div>
  );
};
