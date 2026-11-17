import type { Edge, Node } from "@xyflow/react";
import type {
  DramaLayoutData,
  DramaNodeStateSchema,
  DramaProjectSchema,
  DramaRoutingPolicy,
} from "../api/client";

export type DramaNodeType = "script" | "character" | "scene" | "shot" | "audioImport" | "composer";

export type NodeTaskStatus = "idle" | "running" | "ready" | "stale" | "error";

export interface ScriptNodeData extends Record<string, unknown> {
  kind: "script";
  title: string;
  genre: string;
  aspectRatio: "9:16" | "16:9" | "1:1";
  stylePreset: string;
  outline: string;
  routingPolicy: DramaRoutingPolicy;
  scriptModelOverride?: string;
  imageModelOverride?: string;
  videoModelOverride?: string;
  allowedImageProviders?: string[];
  allowedVideoProviders?: string[];
  allowedTTSProviders?: string[];
  defaultTTSEngine?: CharacterNodeData["voiceEngine"];
  defaultTTSVoiceId?: string;
  defaultTTSVoiceName?: string;
  imageConcurrency?: number;
  autoFallbackKenBurns?: boolean;
  bgmUrl?: string;
  status: NodeTaskStatus;
  error?: string;
}

export interface CharacterNodeData extends Record<string, unknown> {
  kind: "character";
  charId: string;
  name: string;
  role: string;
  avatarColor: string;
  appearanceBible: string;
  portraitUrl?: string;
  portraitPrompt?: string;
  voiceEngine: "edge-tts" | "local_tts" | "jianying" | "dashscope" | "siliconflow" | "chatgpt";
  voiceId: string;
  voiceName: string;
  speed: number;
  pitch: number;
  status: NodeTaskStatus;
  locked?: boolean;
  error?: string;
}

export interface SceneNodeData extends Record<string, unknown> {
  kind: "scene";
  sceneId: string;
  name: string;
  environmentPrompt: string;
  lightingStyle: string;
  imageUrl?: string;
  status: NodeTaskStatus;
  locked?: boolean;
  error?: string;
}

export interface ShotVersionBranch {
  id: string;
  label: string;
  modelUsed?: string;
  imageUrl?: string;
  audioUrl?: string;
  videoUrl?: string;
  videoTaskId?: string;
  renderMode: "video" | "ken_burns";
  audioStatus: NodeTaskStatus;
  keyframeStatus: NodeTaskStatus;
  videoStatus: "idle" | "running" | "ready" | "ken_burns" | "error";
  keyframeLocked?: boolean;
  audioLocked?: boolean;
  keyframeDegraded?: boolean;
  progress: number;
  statusNote?: string;
  error?: string;
}

export interface ShotNodeData extends Record<string, unknown> {
  kind: "shot";
  shotId: string;
  index: number;
  speakerId: string;
  sceneId?: string;
  continuous?: boolean;
  shotType: "全景" | "中景" | "特写" | "过肩" | string;
  cameraMove: string;
  dialogue: string;
  sceneAction: string;
  continuityNotes?: string;
  polishedVideoPrompt?: string;
  transition: "cut" | "crossfade" | "dip_black";
  estimatedDuration: number;
  actualAudioDuration?: number;
  activeVersionId: string;
  versions: ShotVersionBranch[];
}

export interface AudioImportNodeData extends Record<string, unknown> {
  kind: "audioImport";
  title: string;
  sourceAudioUrl?: string;
  transcript?: string;
  language?: string;
  clonedVoiceId?: string;
  status: NodeTaskStatus;
  error?: string;
}

export interface ComposerNodeData extends Record<string, unknown> {
  kind: "composer";
  title: string;
  bgmUrl?: string;
  bgmVolume: number;
  showSubtitles: boolean;
  exportedVideoUrl?: string;
  exportedExt?: string;
  exportedFilename?: string;
  exportProgress: number;
  exportStageText?: string;
  status: NodeTaskStatus;
  error?: string;
}

export type DramaFlowNode =
  | Node<ScriptNodeData, "script">
  | Node<CharacterNodeData, "character">
  | Node<SceneNodeData, "scene">
  | Node<ShotNodeData, "shot">
  | Node<AudioImportNodeData, "audioImport">
  | Node<ComposerNodeData, "composer">;

export interface DramaProjectSnapshot {
  id: string;
  title: string;
  updatedAt: string;
  nodes: DramaFlowNode[];
  edges: Edge[];
  checkpointAfterCast: boolean;
  checkpointAfterKeyframes: boolean;
}

export const STYLE_PRESETS = [
  "电影感写实 (8K ARRI ALEXA, 胶片质感, 自然皮肤纹理, 戏剧光影)",
  "国风 3D 写实 (UE5 虚幻引擎次世代材质, 东方仙侠光影, 极致发丝细节)",
  "二次元国漫 (精细赛璐璐立绘, 高饱和光晕, 新海诚天空光影)",
  "赛博霓虹悬疑 (暗调蓝紫对比, 潮湿地面反射霓虹灯箱, 浅景深)",
  "民国复古电影 (柔焦暖黄影调, 旗袍西装考究质感, 胶片颗粒)",
];

export const CAMERA_MOTIONS = [
  "缓推镜头 (Push In)",
  "后拉镜头 (Pull Out)",
  "平移横移 (Pan)",
  "定焦微表情 (Static)",
  "低机位仰拍 (Low Angle)",
  "环绕推进 (Orbit)",
];

export const BUILTIN_VOICE_OPTIONS = [
  { id: "zh-CN-YunyangNeural", name: "云扬 (Yunyang · 沉稳霸总/旁白)", engine: "edge-tts" as const },
  { id: "zh-CN-XiaoxiaoNeural", name: "晓晓 (Xiaoxiao · 大女主/重生逆袭)", engine: "edge-tts" as const },
  { id: "zh-CN-YunxiNeural", name: "云希 (Yunxi · 热血战神/少年男主)", engine: "edge-tts" as const },
  { id: "zh-CN-YunjianNeural", name: "云健 (Yunjian · 冷酷反派/武侠硬汉)", engine: "edge-tts" as const },
  { id: "zh-CN-XiaoyiNeural", name: "晓伊 (Xiaoyi · 悬疑惊悚/女配腹黑)", engine: "edge-tts" as const },
  { id: "zh-CN-YunxiaNeural", name: "云夏 (Yunxia · 甜宠千金/萌娃正太)", engine: "edge-tts" as const },
  { id: "zh-CN-liaoning-XiaobeiNeural", name: "晓北 (东北方言 · 搞笑闺蜜/喜剧)", engine: "edge-tts" as const },
  { id: "zh-CN-shaanxi-XiaoniNeural", name: "晓妮 (陕西方言 · 豪爽长辈)", engine: "edge-tts" as const },
  { id: "zh-HK-HiuGaaiNeural", name: "晓佳 (粤语港风 · 豪门御姐)", engine: "edge-tts" as const },
  { id: "zh-HK-WanLungNeural", name: "云龙 (粤语港风 · 商场大佬)", engine: "edge-tts" as const },
  { id: "zh-TW-HsiaoChenNeural", name: "晓臻 (台偶言情 · 温柔女主)", engine: "edge-tts" as const },
  { id: "zh-TW-YunJheNeural", name: "云哲 (台偶言情 · 深情男二)", engine: "edge-tts" as const },
  { id: "en-US-AndrewMultilingualNeural", name: "Andrew (出海英语霸总 · 多语种)", engine: "edge-tts" as const },
  { id: "en-US-AvaMultilingualNeural", name: "Ava (出海英语大女主 · 多语种)", engine: "edge-tts" as const },
  { id: "alloy", name: "Alloy (ChatGPT 多语种均衡音色)", engine: "chatgpt" as const },
  { id: "nova", name: "Nova (ChatGPT 清亮女声)", engine: "chatgpt" as const },
];

export interface DramaTemplatePreset {
  genre: string;
  title: string;
  aspectRatio: "9:16" | "16:9" | "1:1";
  stylePreset: string;
  outline: string;
  characters: Array<{
    id: string;
    name: string;
    role: string;
    avatarColor: string;
    appearanceBible: string;
    voiceEngine: CharacterNodeData["voiceEngine"];
    voiceId: string;
    voiceName: string;
  }>;
  scenes: Array<{
    id: string;
    name: string;
    environmentPrompt: string;
    lightingStyle: string;
  }>;
  shots: Array<{
    id: string;
    index: number;
    speakerId: string;
    sceneId: string;
    shotType: string;
    cameraMove: string;
    dialogue: string;
    sceneAction: string;
    continuityNotes?: string;
    estimatedDuration: number;
  }>;
}

export const DRAMA_PRESETS: Record<string, DramaTemplatePreset> = {
  都市逆袭: {
    genre: "都市赘婿/战神归来",
    title: "龙王赘婿：三年期满，千亿车队跪迎归位",
    aspectRatio: "9:16",
    stylePreset: STYLE_PRESETS[0],
    outline:
      "隐忍三年的上门女婿陆沉在寿宴上遭妻弟当众羞辱签字离婚。走出庄园瞬间，街头数百辆防弹黑车整齐列阵，全城顶流跪迎龙王归位！",
    characters: [
      {
        id: "char-1",
        name: "陆沉",
        role: "主角",
        avatarColor: "#2563eb",
        appearanceBible: "26岁青年，微卷黑发，深邃冷峻眼眸，黑色极简高定风衣，左腕戴机械暗钢腕表，气场沉稳锐利",
        voiceEngine: "edge-tts",
        voiceId: "zh-CN-YunyangNeural",
        voiceName: "云扬 (Yunyang · 沉稳霸总/旁白)",
      },
      {
        id: "char-2",
        name: "林清雪",
        role: "女主",
        avatarColor: "#ec4899",
        appearanceBible: "24岁冷艳千金，齐肩黑直发，珍珠耳坠，浅米色修身高定套裙，神色冷漠",
        voiceEngine: "edge-tts",
        voiceId: "zh-CN-XiaoxiaoNeural",
        voiceName: "晓晓 (Xiaoxiao · 大女主/知性)",
      },
      {
        id: "char-3",
        name: "林耀祖",
        role: "反派",
        avatarColor: "#ef4444",
        appearanceBible: "28岁纨绔少爷，大背头，酒红色缎面西装配金表，神态张狂轻蔑",
        voiceEngine: "edge-tts",
        voiceId: "zh-CN-YunjianNeural",
        voiceName: "云健 (Yunjian · 冷酷反派/武侠)",
      },
      {
        id: "char-4",
        name: "解说旁白",
        role: "旁白",
        avatarColor: "#8b5cf6",
        appearanceBible: "画外音视点，无需真人特写，以全景环境构图为主",
        voiceEngine: "edge-tts",
        voiceId: "zh-CN-YunxiNeural",
        voiceName: "云希 (Yunxi · 热血青年/解说)",
      },
    ],
    scenes: [
      {
        id: "scene-1",
        name: "林氏豪门宴会厅",
        environmentPrompt: "奢华水晶吊灯大厅，大理石反光地面，长桌红酒杯与协议文件，窗外倾盆暴雨雷电",
        lightingStyle: "室内暖金吊灯与窗外冷蓝闪电强烈冷暖反差",
      },
      {
        id: "scene-2",
        name: "暴雨庄园正门外广场",
        environmentPrompt: "深夜大雨磅礴的铁艺庄园大门广场，数十辆黑色加长防弹轿车车灯穿透雨幕形成光柱",
        lightingStyle: "高对比暴雨夜逆光，汽车大灯丁达尔效应光束",
      },
    ],
    shots: [
      {
        id: "shot-1",
        index: 1,
        speakerId: "char-4",
        sceneId: "scene-1",
        shotType: "全景",
        cameraMove: "后拉镜头 (Pull Out)",
        dialogue: "江城顶奢林氏庄园，窗外暴雨如注，一场寿宴正演变成冰冷的羞辱！",
        sceneAction: "奢华宴会厅全景，水晶吊灯闪烁，宾客环顾窃窃私语",
        continuityNotes: "开场建立宴会厅整体布局与暴雨夜冷暖对比基调",
        estimatedDuration: 4.5,
      },
      {
        id: "shot-2",
        index: 2,
        speakerId: "char-3",
        sceneId: "scene-1",
        shotType: "特写",
        cameraMove: "定焦微表情 (Static)",
        dialogue: "陆沉！在林家吃了三年软饭，今天清雪高嫁顾少，赶紧签了离婚协议滚蛋！",
        sceneAction: "林耀祖将一沓离婚文件狠狠摔在红木桌面，手指傲慢指着前方",
        continuityNotes: "延续宴会厅暖金灯光，桌上文件特写",
        estimatedDuration: 5.2,
      },
      {
        id: "shot-3",
        index: 3,
        speakerId: "char-1",
        sceneId: "scene-1",
        shotType: "特写",
        cameraMove: "缓推镜头 (Push In)",
        dialogue: "三年恩情已还，这字我签。但走出这扇门，林家生死从此与我无关。",
        sceneAction: "陆沉神色不动声色，接过钢笔干脆落笔签名，黑风衣衣领凌厉",
        continuityNotes: "陆沉签完同一份桌上的离婚协议，气场骤冷",
        estimatedDuration: 5.2,
      },
      {
        id: "shot-4",
        index: 4,
        speakerId: "char-1",
        sceneId: "scene-2",
        shotType: "全景",
        cameraMove: "低机位仰拍 (Low Angle)",
        dialogue: "暴雨止步之处，便是新王登基之时。",
        sceneAction: "陆沉大步踏出庄园大门，雨幕中数十辆黑车大灯齐亮，黑衣卫士撑伞列队单膝迎候",
        continuityNotes: "陆沉保持相同黑色高定风衣从屋内走出至暴雨广场，光影切换为车灯逆光",
        estimatedDuration: 4.8,
      },
    ],
  },
  悬疑反转: {
    genre: "都市悬疑",
    title: "第七次敲门：监控里消失的自己",
    aspectRatio: "9:16",
    stylePreset: STYLE_PRESETS[3],
    outline:
      "凌晨三点半，独自加班的设计师林默收到一条来电提醒，抬头看向门禁监控画面——画面里正站在防盗门外疯狂敲门的人，竟穿着和自己一模一样的风衣。",
    characters: [
      {
        id: "char-1",
        name: "林默",
        role: "主角",
        avatarColor: "#0ea5e9",
        appearanceBible: "27岁男设计师，黑框细边眼镜，深灰色连帽风衣，眼底带着疲惫血丝与震惊",
        voiceEngine: "edge-tts",
        voiceId: "zh-CN-YunxiNeural",
        voiceName: "云希 (Yunxi · 热血青年/解说)",
      },
      {
        id: "char-2",
        name: "AI系统音",
        role: "旁白",
        avatarColor: "#8b5cf6",
        appearanceBible: "冷色调智能门禁屏幕上跳动的像素波形与警报提示",
        voiceEngine: "edge-tts",
        voiceId: "zh-CN-XiaoyiNeural",
        voiceName: "晓伊 (Xiaoyi · 悬疑叙述)",
      },
    ],
    scenes: [
      {
        id: "scene-1",
        name: "深夜高楼工作室走廊",
        environmentPrompt: "狭窄冷调写字楼走廊，安全出口绿光微弱，门禁电子屏闪烁雪花噪点",
        lightingStyle: "青绿色暗调低照度悬疑布光",
      },
    ],
    shots: [
      {
        id: "shot-1",
        index: 1,
        speakerId: "char-2",
        sceneId: "scene-1",
        shotType: "全景",
        cameraMove: "缓推镜头 (Push In)",
        dialogue: "凌晨三点三十分，整栋写字楼早已断电锁门，却响起了第七次敲门声。",
        sceneAction: "空荡幽暗的写字楼室内，台灯冷白光线照亮办公桌，门锁发出咚咚闷响",
        continuityNotes: "建立深夜密闭空间压迫感",
        estimatedDuration: 4.8,
      },
      {
        id: "shot-2",
        index: 2,
        speakerId: "char-1",
        sceneId: "scene-1",
        shotType: "特写",
        cameraMove: "定焦微表情 (Static)",
        dialogue: "不可能……门外监控里穿着灰风衣站着的那个人，为什么长着我的脸？！",
        sceneAction: "林默死死盯着壁挂门禁屏幕，瞳孔骤缩，镜片反射屏幕幽蓝微光",
        continuityNotes: "保持林默黑框眼镜与深灰连帽风衣形象不变",
        estimatedDuration: 5.0,
      },
      {
        id: "shot-3",
        index: 3,
        speakerId: "char-2",
        sceneId: "scene-1",
        shotType: "中景",
        cameraMove: "后拉镜头 (Pull Out)",
        dialogue: "门禁系统忽然弹出提示：欢迎回家，林先生，您已连续七天未离开过房间。",
        sceneAction: "电子门咔哒自动解锁打开，门外却空无一人，只有地上放着一部仍在录像的手机",
        continuityNotes: "室内青蓝悬疑调性与门外走廊灯光融合",
        estimatedDuration: 5.4,
      },
    ],
  },
};

export function createShotVersionBranch(id = "ver-a", label = "主版本"): ShotVersionBranch {
  return {
    id,
    label,
    renderMode: "ken_burns",
    audioStatus: "idle",
    keyframeStatus: "idle",
    videoStatus: "idle",
    progress: 0,
  };
}

export function buildDramaGraphFromTemplate(
  preset: DramaTemplatePreset,
  options?: { projectId?: string; routingPolicy?: DramaRoutingPolicy },
): { nodes: DramaFlowNode[]; edges: Edge[] } {
  const nodes: DramaFlowNode[] = [];
  const edges: Edge[] = [];
  const routingPolicy = options?.routingPolicy || "free_first";

  const scriptNodeId = "script-main";
  nodes.push({
    id: scriptNodeId,
    type: "script",
    position: { x: 40, y: 120 },
    data: {
      kind: "script",
      title: preset.title,
      genre: preset.genre,
      aspectRatio: preset.aspectRatio,
      stylePreset: preset.stylePreset,
      outline: preset.outline,
      routingPolicy,
      status: "ready",
    },
  });

  // 角色锚点与场景锚点统一在第二列纵向排布
  const charNodeMap = new Map<string, string>();
  let anchorY = 40;
  preset.characters.forEach((ch) => {
    const nodeId = `char-node-${ch.id}`;
    charNodeMap.set(ch.id, nodeId);
    nodes.push({
      id: nodeId,
      type: "character",
      position: { x: 430, y: anchorY },
      data: {
        kind: "character",
        charId: ch.id,
        name: ch.name,
        role: ch.role,
        avatarColor: ch.avatarColor,
        appearanceBible: ch.appearanceBible,
        voiceEngine: ch.voiceEngine,
        voiceId: ch.voiceId,
        voiceName: ch.voiceName,
        speed: 1.0,
        pitch: 0,
        status: "idle",
      },
    });
    edges.push({
      id: `edge-${scriptNodeId}-${nodeId}`,
      source: scriptNodeId,
      target: nodeId,
      animated: false,
      style: { stroke: "#6366f1", strokeWidth: 1.5 },
    });
    anchorY += 230;
  });

  const sceneNodeMap = new Map<string, string>();
  preset.scenes.forEach((sc) => {
    const nodeId = `scene-node-${sc.id}`;
    sceneNodeMap.set(sc.id, nodeId);
    nodes.push({
      id: nodeId,
      type: "scene",
      position: { x: 430, y: anchorY },
      data: {
        kind: "scene",
        sceneId: sc.id,
        name: sc.name,
        environmentPrompt: sc.environmentPrompt,
        lightingStyle: sc.lightingStyle,
        status: "idle",
      },
    });
    edges.push({
      id: `edge-${scriptNodeId}-${nodeId}`,
      source: scriptNodeId,
      target: nodeId,
      animated: false,
      style: { stroke: "#0ea5e9", strokeWidth: 1.5 },
    });
    anchorY += 220;
  });

  const composerNodeId = "composer-main";
  let shotY = 40;
  let prevShotNodeId: string | null = null;

  preset.shots.forEach((shot) => {
    const nodeId = `shot-node-${shot.id}`;
    nodes.push({
      id: nodeId,
      type: "shot",
      position: { x: 840, y: shotY },
      data: {
        kind: "shot",
        shotId: shot.id,
        index: shot.index,
        speakerId: shot.speakerId,
        sceneId: shot.sceneId,
        shotType: shot.shotType,
        cameraMove: shot.cameraMove,
        dialogue: shot.dialogue,
        sceneAction: shot.sceneAction,
        continuityNotes: shot.continuityNotes,
        transition: shot.index === 1 ? "cut" : "crossfade",
        estimatedDuration: shot.estimatedDuration,
        activeVersionId: "ver-a",
        versions: [createShotVersionBranch("ver-a", "主版本")],
      },
    });

    const speakerNodeId = charNodeMap.get(shot.speakerId);
    if (speakerNodeId) {
      edges.push({
        id: `edge-${speakerNodeId}-${nodeId}`,
        source: speakerNodeId,
        target: nodeId,
        style: { stroke: "#8b5cf6", strokeWidth: 1.5 },
      });
    }
    const sceneNodeId = sceneNodeMap.get(shot.sceneId);
    if (sceneNodeId) {
      edges.push({
        id: `edge-${sceneNodeId}-${nodeId}`,
        source: sceneNodeId,
        target: nodeId,
        style: { stroke: "#06b6d4", strokeWidth: 1.5, strokeDasharray: "4 4" },
      });
    }
    if (prevShotNodeId) {
      edges.push({
        id: `edge-seq-${prevShotNodeId}-${nodeId}`,
        source: prevShotNodeId,
        target: nodeId,
        animated: true,
        style: { stroke: "#f59e0b", strokeWidth: 1.8 },
      });
    }
    edges.push({
      id: `edge-${nodeId}-${composerNodeId}`,
      source: nodeId,
      target: composerNodeId,
      style: { stroke: "#10b981", strokeWidth: 1.6 },
    });

    prevShotNodeId = nodeId;
    shotY += 290;
  });

  nodes.push({
    id: composerNodeId,
    type: "composer",
    position: { x: 1290, y: Math.max(120, Math.round(shotY / 2 - 140)) },
    data: {
      kind: "composer",
      title: `${preset.title} · 成片合流输出`,
      bgmVolume: 0.22,
      showSubtitles: true,
      exportProgress: 0,
      status: "idle",
    },
  });

  return { nodes, edges };
}

export function getActiveShotBranch(data: ShotNodeData): ShotVersionBranch {
  return data.versions.find((v) => v.id === data.activeVersionId) || data.versions[0] || createShotVersionBranch();
}

export function layoutDramaNodesCleanly(nodes: DramaFlowNode[]): DramaFlowNode[] {
  let scriptY = 100;
  let col1Y = 40;
  let col2Y = 40;
  let maxY = 300;

  return nodes.map((node) => {
    if (node.type === "script" || node.type === "audioImport") {
      const next = { ...node, position: { x: 40, y: scriptY } };
      scriptY += 340;
      maxY = Math.max(maxY, scriptY);
      return next;
    }
    if (node.type === "character" || node.type === "scene") {
      const next = { ...node, position: { x: 430, y: col1Y } };
      col1Y += 235;
      maxY = Math.max(maxY, col1Y);
      return next;
    }
    if (node.type === "shot") {
      const next = { ...node, position: { x: 840, y: col2Y } };
      col2Y += 295;
      maxY = Math.max(maxY, col2Y);
      return next;
    }
    return {
      ...node,
      position: { x: 1290, y: Math.max(120, Math.round(Math.max(col1Y, col2Y) / 2 - 150)) },
    };
  });
}

function mapBackendNodeStatus(st?: DramaNodeStateSchema): NodeTaskStatus {
  if (!st) return "idle";
  switch (st.status) {
    case "running":
      return "running";
    case "ready":
    case "degraded":
      return "ready";
    case "stale":
      return "stale";
    case "failed":
      return "error";
    default:
      return "idle";
  }
}

export function flowGraphToLayout(
  nodes: DramaFlowNode[],
  flags: { checkpointAfterCast: boolean; checkpointAfterKeyframes: boolean },
): DramaLayoutData {
  const positions: Record<string, { x: number; y: number }> = {};
  nodes.forEach((n) => {
    positions[n.id] = { x: n.position.x, y: n.position.y };
  });
  return {
    positions,
    checkpoint_after_cast: flags.checkpointAfterCast,
    checkpoint_after_keyframes: flags.checkpointAfterKeyframes,
  };
}

export function flowGraphToSchemaProject(
  projectId: string,
  nodes: DramaFlowNode[],
  prevProject?: DramaProjectSchema | null,
): DramaProjectSchema {
  const scriptNode = nodes.find((n) => n.type === "script") as (DramaFlowNode & { data: ScriptNodeData }) | undefined;
  const composerNode = nodes.find((n) => n.type === "composer") as (DramaFlowNode & { data: ComposerNodeData }) | undefined;
  const s = scriptNode?.data;

  const assets: DramaProjectSchema["assets"] = { ...(prevProject?.assets || {}) };
  const nodeStates: DramaProjectSchema["nodes"] = { ...(prevProject?.nodes || {}) };

  const characters: DramaProjectSchema["characters"] = [];
  const scenes: DramaProjectSchema["scenes"] = [];
  const shots: DramaProjectSchema["shots"] = [];

  for (const n of nodes) {
    if (n.type === "character") {
      const d = n.data;
      let portraitId = prevProject?.characters.find((c) => c.id === d.charId)?.portrait || "";
      if (d.portraitUrl && (!portraitId || assets[portraitId]?.url !== d.portraitUrl)) {
        portraitId = `asset_portrait_${d.charId}`;
        assets[portraitId] = { id: portraitId, kind: "image", url: d.portraitUrl, prompt: d.portraitPrompt };
      }
      if (!d.portraitUrl) portraitId = "";
      const key = `image.portrait:${d.charId}`;
      const existing = nodeStates[key];
      nodeStates[key] = {
        status: existing?.status || (d.portraitUrl ? "ready" : "pending"),
        input_hash: existing?.input_hash || "",
        output: portraitId || existing?.output,
        locked: Boolean(d.locked),
        attempts: existing?.attempts || 0,
        error: d.error,
      };
      characters.push({
        id: d.charId,
        name: d.name,
        role: d.role,
        avatar_color: d.avatarColor,
        appearance_bible: d.appearanceBible,
        portrait_prompt: d.portraitPrompt,
        voice: {
          engine: d.voiceEngine,
          id: d.voiceId,
          name: d.voiceName,
          speed: d.speed,
          pitch: d.pitch,
        },
        portrait: portraitId || undefined,
      });
    } else if (n.type === "scene") {
      const d = n.data;
      let anchorId = prevProject?.scenes.find((sc) => sc.id === d.sceneId)?.anchor || "";
      if (d.imageUrl && (!anchorId || assets[anchorId]?.url !== d.imageUrl)) {
        anchorId = `asset_scene_${d.sceneId}`;
        assets[anchorId] = { id: anchorId, kind: "image", url: d.imageUrl };
      }
      if (!d.imageUrl) anchorId = "";
      const key = `image.scene:${d.sceneId}`;
      const existing = nodeStates[key];
      nodeStates[key] = {
        status: existing?.status || (d.imageUrl ? "ready" : "pending"),
        input_hash: existing?.input_hash || "",
        output: anchorId || existing?.output,
        locked: Boolean(d.locked),
        attempts: existing?.attempts || 0,
        error: d.error,
      };
      scenes.push({
        id: d.sceneId,
        name: d.name,
        environment: d.environmentPrompt,
        lighting: d.lightingStyle,
        anchor: anchorId || undefined,
      });
    } else if (n.type === "shot") {
      const d = n.data;
      const br = getActiveShotBranch(d);
      const prevShot = prevProject?.shots.find((sh) => sh.id === d.shotId);
      let kfId = prevShot?.keyframe || "";
      if (br.imageUrl && (!kfId || assets[kfId]?.url !== br.imageUrl)) {
        kfId = `asset_kf_${d.shotId}`;
        assets[kfId] = { id: kfId, kind: "image", url: br.imageUrl, model: br.modelUsed };
      }
      if (!br.imageUrl) kfId = "";

      let audId = prevShot?.audio || "";
      if (br.audioUrl && (!audId || assets[audId]?.url !== br.audioUrl)) {
        audId = `asset_aud_${d.shotId}`;
        assets[audId] = { id: audId, kind: "audio", url: br.audioUrl };
      }
      if (!br.audioUrl) audId = "";

      const kfKey = `image.keyframe:${d.shotId}`;
      const prevKf = nodeStates[kfKey];
      nodeStates[kfKey] = {
        status: prevKf?.status || (br.imageUrl ? "ready" : "pending"),
        input_hash: prevKf?.input_hash || "",
        output: kfId || prevKf?.output,
        locked: Boolean(br.keyframeLocked),
        attempts: prevKf?.attempts || 0,
      };

      const ttsKey = `tts.line:${d.shotId}`;
      const prevTts = nodeStates[ttsKey];
      nodeStates[ttsKey] = {
        status: prevTts?.status || (br.audioUrl ? "ready" : "pending"),
        input_hash: prevTts?.input_hash || "",
        output: audId || prevTts?.output,
        locked: Boolean(br.audioLocked),
        attempts: prevTts?.attempts || 0,
      };

      shots.push({
        id: d.shotId,
        index: d.index,
        scene_id: d.sceneId,
        speaker_id: d.speakerId,
        continuous: d.continuous,
        shot_type: d.shotType,
        camera_move: d.cameraMove,
        action: d.sceneAction,
        dialogue: d.dialogue,
        continuity_notes: d.continuityNotes,
        polished_video_prompt: d.polishedVideoPrompt,
        transition: d.transition,
        duration_sec: d.estimatedDuration,
        actual_audio_duration: d.actualAudioDuration,
        keyframe: kfId || undefined,
        audio: audId || undefined,
      });
    }
  }

  return {
    id: projectId,
    schema_version: 1,
    revision: prevProject?.revision || 1,
    script: {
      title: s?.title || "未命名短剧",
      outline: s?.outline || "",
      genre: s?.genre || "都市悬疑",
      aspect_ratio: s?.aspectRatio || "9:16",
      style: s?.stylePreset || "电影感写实",
      routing_policy: s?.routingPolicy || "free_first",
      script_model_override: s?.scriptModelOverride,
      image_model_override: s?.imageModelOverride,
      video_model_override: s?.videoModelOverride,
      allowed_image_providers: s?.allowedImageProviders,
      allowed_video_providers: s?.allowedVideoProviders,
      allowed_tts_providers: s?.allowedTTSProviders,
      default_tts_engine: s?.defaultTTSEngine,
      default_tts_voice_id: s?.defaultTTSVoiceId,
      default_tts_voice_name: s?.defaultTTSVoiceName,
      image_concurrency: s?.imageConcurrency,
      auto_fallback_ken_burns: s?.autoFallbackKenBurns,
      bgm_url: s?.bgmUrl || composerNode?.data.bgmUrl,
      bgm_volume: composerNode?.data.bgmVolume,
      show_subtitles: composerNode?.data.showSubtitles,
    },
    analysis: prevProject?.analysis,
    characters,
    scenes,
    shots: shots.sort((a, b) => a.index - b.index),
    assets,
    nodes: nodeStates,
    timeline: prevProject?.timeline,
  };
}

export function applyBackendProjectToFlow(
  proj: DramaProjectSchema,
  existingNodes: DramaFlowNode[],
  layout?: DramaLayoutData,
): { nodes: DramaFlowNode[]; edges: Edge[] } {
  const rebuilt = buildDramaGraphFromTemplate(
    {
      title: proj.script.title,
      genre: proj.script.genre,
      aspectRatio: (proj.script.aspect_ratio as "9:16" | "16:9" | "1:1") || "9:16",
      stylePreset: proj.script.style,
      outline: proj.script.outline,
      characters: proj.characters.map((c) => ({
        id: c.id,
        name: c.name,
        role: c.role,
        avatarColor: c.avatar_color || "#2563eb",
        appearanceBible: c.appearance_bible,
        voiceEngine: (c.voice.engine as CharacterNodeData["voiceEngine"]) || "edge-tts",
        voiceId: c.voice.id,
        voiceName: c.voice.name || c.voice.id,
      })),
      scenes: proj.scenes.map((sc) => ({
        id: sc.id,
        name: sc.name,
        environmentPrompt: sc.environment,
        lightingStyle: sc.lighting,
      })),
      shots: proj.shots.map((sh) => ({
        id: sh.id,
        index: sh.index,
        speakerId: sh.speaker_id || "",
        sceneId: sh.scene_id || "",
        shotType: sh.shot_type,
        cameraMove: sh.camera_move,
        dialogue: sh.dialogue,
        sceneAction: sh.action,
        continuityNotes: sh.continuity_notes,
        estimatedDuration: sh.duration_sec,
      })),
    },
    { routingPolicy: (proj.script.routing_policy as DramaRoutingPolicy) || "free_first" },
  );

  const existingById = new Map(existingNodes.map((n) => [n.id, n]));
  const nodes = rebuilt.nodes.map((node) => {
    const pos = layout?.positions?.[node.id] || existingById.get(node.id)?.position || node.position;
    if (node.type === "script") {
      return {
        ...node,
        position: pos,
        data: {
          ...node.data,
          scriptModelOverride: proj.script.script_model_override,
          imageModelOverride: proj.script.image_model_override,
          videoModelOverride: proj.script.video_model_override,
          allowedImageProviders: proj.script.allowed_image_providers,
          allowedVideoProviders: proj.script.allowed_video_providers,
          allowedTTSProviders: proj.script.allowed_tts_providers,
          defaultTTSEngine: (proj.script.default_tts_engine as ScriptNodeData["defaultTTSEngine"]) || "edge-tts",
          defaultTTSVoiceId: proj.script.default_tts_voice_id,
          defaultTTSVoiceName: proj.script.default_tts_voice_name,
          imageConcurrency: proj.script.image_concurrency,
          autoFallbackKenBurns: proj.script.auto_fallback_ken_burns,
          bgmUrl: proj.script.bgm_url,
        },
      };
    }
    if (node.type === "character") {
      const char = proj.characters.find((c) => c.id === node.data.charId);
      const st = proj.nodes[`image.portrait:${node.data.charId}`];
      const url = (char?.portrait && proj.assets[char.portrait]?.url) || (st?.output && proj.assets[st.output]?.url);
      return {
        ...node,
        position: pos,
        data: {
          ...node.data,
          portraitPrompt: char?.portrait_prompt,
          speed: char?.voice.speed ?? 1.0,
          pitch: char?.voice.pitch ?? 0,
          portraitUrl: url,
          status: mapBackendNodeStatus(st),
          locked: Boolean(st?.locked),
          error: st?.error,
        },
      };
    }
    if (node.type === "scene") {
      const sc = proj.scenes.find((s) => s.id === node.data.sceneId);
      const st = proj.nodes[`image.scene:${node.data.sceneId}`];
      const url = (sc?.anchor && proj.assets[sc.anchor]?.url) || (st?.output && proj.assets[st.output]?.url);
      return {
        ...node,
        position: pos,
        data: {
          ...node.data,
          imageUrl: url,
          status: mapBackendNodeStatus(st),
          locked: Boolean(st?.locked),
          error: st?.error,
        },
      };
    }
    if (node.type === "shot") {
      const sh = proj.shots.find((s) => s.id === node.data.shotId);
      const kfSt = proj.nodes[`image.keyframe:${node.data.shotId}`];
      const ttsSt = proj.nodes[`tts.line:${node.data.shotId}`];
      const vidSt = proj.nodes[`video.clip:${node.data.shotId}`];
      const existingShot = existingById.get(node.id);
      const existingBranch = existingShot?.type === "shot" ? getActiveShotBranch(existingShot.data) : undefined;

      const kfUrl = (sh?.keyframe && proj.assets[sh.keyframe]?.url) || (kfSt?.output && proj.assets[kfSt.output]?.url) || existingBranch?.imageUrl;
      const audUrl = (sh?.audio && proj.assets[sh.audio]?.url) || (ttsSt?.output && proj.assets[ttsSt.output]?.url) || existingBranch?.audioUrl;
      const vidUrl = (sh?.video && proj.assets[sh.video]?.url) || (vidSt?.output && proj.assets[vidSt.output]?.url) || existingBranch?.videoUrl;

      let mappedVideoStatus: ShotVersionBranch["videoStatus"] = existingBranch?.videoStatus || "idle";
      let mappedRenderMode: ShotVersionBranch["renderMode"] = existingBranch?.renderMode || "ken_burns";
      if (vidSt) {
        if (vidSt.status === "ready") {
          mappedVideoStatus = "ready";
          mappedRenderMode = "video";
        } else if (vidSt.status === "degraded") {
          mappedVideoStatus = "ken_burns";
          mappedRenderMode = "ken_burns";
        } else if (vidSt.status === "running") {
          mappedVideoStatus = "running";
        } else if (vidSt.status === "failed") {
          mappedVideoStatus = "error";
        } else if (vidSt.status === "pending" || vidSt.status === "stale") {
          mappedVideoStatus = "idle";
        }
      }

      const baseBranch = existingBranch || getActiveShotBranch(node.data);
      const updatedBranch: ShotVersionBranch = {
        ...baseBranch,
        imageUrl: kfUrl,
        audioUrl: audUrl,
        videoUrl: vidUrl,
        videoTaskId: vidSt?.task_id || baseBranch.videoTaskId,
        modelUsed: vidSt?.model_used || kfSt?.model_used || baseBranch.modelUsed,
        keyframeStatus: mapBackendNodeStatus(kfSt),
        audioStatus: mapBackendNodeStatus(ttsSt),
        videoStatus: mappedVideoStatus,
        renderMode: mappedRenderMode,
        progress: mappedVideoStatus === "ready" || mappedVideoStatus === "ken_burns" ? 100 : baseBranch.progress,
        keyframeLocked: Boolean(kfSt?.locked),
        audioLocked: Boolean(ttsSt?.locked),
        keyframeDegraded: kfSt?.status === "degraded",
        statusNote: vidSt?.status_note || kfSt?.status_note || baseBranch.statusNote,
        error: vidSt?.error || kfSt?.error || ttsSt?.error,
      };
      return {
        ...node,
        position: pos,
        data: {
          ...node.data,
          continuous: sh?.continuous,
          polishedVideoPrompt: sh?.polished_video_prompt,
          actualAudioDuration: sh?.actual_audio_duration,
          estimatedDuration: sh?.duration_sec || node.data.estimatedDuration,
          versions: [updatedBranch, ...node.data.versions.filter((v) => v.id !== updatedBranch.id)],
        },
      };
    }
    if (node.type === "composer") {
      const prevComp = existingById.get(node.id);
      const tlSt = proj.nodes["timeline.assemble"];
      return {
        ...node,
        position: pos,
        data: {
          ...node.data,
          ...(prevComp?.type === "composer" ? prevComp.data : {}),
          status: tlSt ? mapBackendNodeStatus(tlSt) : (prevComp?.type === "composer" ? prevComp.data.status : "idle"),
          bgmUrl: proj.script.bgm_url ?? (prevComp?.type === "composer" ? prevComp.data.bgmUrl : undefined),
          bgmVolume: proj.script.bgm_volume ?? (prevComp?.type === "composer" ? prevComp.data.bgmVolume : 0.25),
          showSubtitles: proj.script.show_subtitles ?? (prevComp?.type === "composer" ? prevComp.data.showSubtitles : true),
        },
      };
    }
    return { ...node, position: pos };
  });

  return { nodes, edges: rebuilt.edges };
}
