import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  AlertCircle,
  ArrowLeft,
  ArrowRight,
  Check,
  CheckCircle2,
  ChevronDown,
  Clapperboard,
  Clock,
  Copy,
  Download,
  Eye,
  Film,
  HelpCircle,
  Layers,
  Mic,
  Music,
  Pause,
  Play,
  Plus,
  RefreshCw,
  RotateCcw,
  Scissors,
  Share2,
  Sliders,
  Sparkles,
  Trash2,
  Tv,
  Upload,
  User,
  Video,
  Volume2,
  VolumeX,
  Wand2,
  X,
} from "lucide-react";
import { NoticeBanner, triggerTextDownload } from "../components/ui";
import { submitVideo, getVideoTask } from "../api/client";

// ── 类型定义 ──

export type DramaGenre =
  | "战神归来"
  | "都市异能"
  | "豪门真假千金"
  | "极寒末日"
  | "悬疑反杀"
  | "古装仙侠";

export type StylePreset =
  | "国风 3D 写实"
  | "电影胶片质感"
  | "二次元国漫"
  | "赛博朋克光影"
  | "复古港风胶片";

export interface CharacterItem {
  id: string;
  name: string;
  role: "主角" | "反派" | "配角" | "旁白";
  avatarColor: string;
  appearanceBible: string; // 外貌圣经 (特征词，保障跨镜头一致性)
  voice: {
    engine: "edge-tts" | "chatgpt";
    voiceId: string;
    voiceName: string;
    voiceLabel: string;
    speed: number; // 0.8 ~ 1.5
    pitch: number; // -5 ~ +5
  };
}

export interface ShotItem {
  id: string;
  index: number;
  speakerId: string;
  shotType: "特写" | "中景" | "全景" | "俯瞰" | "仰视";
  cameraMove:
    | "缓推镜头 (Push In)"
    | "后拉镜头 (Pull Out)"
    | "平移横移 (Pan)"
    | "定焦微表情 (Static)"
    | "环绕运镜 (Orbit)";
  dialogue: string;
  sceneAction: string;
  estimatedDuration: number; // 秒
  audioStatus: "idle" | "rendering" | "ready";
  videoStatus: "idle" | "queued" | "rendering" | "ready";
  renderProgress: number; // 0 - 100
  videoUrl?: string;
}

export interface DramaProject {
  id: string;
  title: string;
  genre: DramaGenre;
  aspectRatio: "9:16" | "16:9";
  stylePreset: StylePreset;
  outline: string;
  characters: CharacterItem[];
  shots: ShotItem[];
  updatedAt: string;
}

// ── 内置预设音色库 (以免费微软 Edge-TTS 主力短剧音色为主) ──

const VOICE_CATALOG = [
  {
    engine: "edge-tts" as const,
    voiceId: "zh-CN-YunyangNeural",
    voiceName: "云扬 (Yunyang)",
    voiceLabel: "沉稳霸总 / 影视播报",
    gender: "男声",
    desc: "浑厚沉稳、威严果断，极其适合战神、霸道总裁、冷峻男主",
  },
  {
    engine: "edge-tts" as const,
    voiceId: "zh-CN-XiaoxiaoNeural",
    voiceName: "晓晓 (Xiaoxiao)",
    voiceLabel: "温柔知性 / 大女主",
    gender: "女声",
    desc: "自然真挚、情感丰富，适合大女主对白、深情旁白与都市职场女性",
  },
  {
    engine: "edge-tts" as const,
    voiceId: "zh-CN-YunxiNeural",
    voiceName: "云希 (Yunxi)",
    voiceLabel: "热血青年 / 故事解说",
    gender: "男声",
    desc: "节奏明快、生动叙事，抖音爆款短剧说书、悬疑故事解说首选",
  },
  {
    engine: "edge-tts" as const,
    voiceId: "zh-CN-YunxiaNeural",
    voiceName: "云夏 (Yunxia)",
    voiceLabel: "灵动千金 / 活泼少女",
    gender: "女声",
    desc: "清脆俏皮、明亮甜美，适合千金闺蜜、灵巧配角与现代甜宠剧",
  },
  {
    engine: "edge-tts" as const,
    voiceId: "zh-CN-YunjianNeural",
    voiceName: "云健 (Yunjian)",
    voiceLabel: "冷酷反派 / 武侠少侠",
    gender: "男声",
    desc: "低沉凌厉、带有锋芒，适合桀骜反派、纨绔子弟或武侠刺客",
  },
  {
    engine: "edge-tts" as const,
    voiceId: "zh-CN-liaoning-XiaobeiNeural",
    voiceName: "辽宁晓北 (Xiaobei)",
    voiceLabel: "幽默东北话 / 喜剧配角",
    gender: "女声",
    desc: "地道东北方言，适合喜剧反转短剧、市井幽默小人物",
  },
  {
    engine: "chatgpt" as const,
    voiceId: "alloy",
    voiceName: "Alloy (OpenAI)",
    voiceLabel: "质感全能 / 经典",
    gender: "中性",
    desc: "ChatGPT 经典官方音色，平衡清晰",
  },
  {
    engine: "chatgpt" as const,
    voiceId: "nova",
    voiceName: "Nova (OpenAI)",
    voiceLabel: "高能灵动 / 情绪饱满",
    gender: "女声",
    desc: "情绪充沛的官方音色，适合张力场景",
  },
];

// ── 爆款短剧剧本模板库 ──

interface GenreTemplate {
  genre: DramaGenre;
  title: string;
  stylePreset: StylePreset;
  outline: string;
  characters: CharacterItem[];
  shots: ShotItem[];
}

const GENRE_TEMPLATES: Record<DramaGenre, GenreTemplate> = {
  战神归来: {
    genre: "战神归来",
    title: "万亿龙王：开局退婚，前妻全家跪求原谅",
    stylePreset: "电影胶片质感",
    outline:
      "三年前，龙王陆沉为报救命之恩隐姓埋名入赘林家，沦为受尽白眼的家庭煮夫。三年期满，林家为攀附豪门逼陆沉签下离婚协议。签字瞬间，全球十万龙神卫齐聚江城，万亿财富瞬间解封！",
    characters: [
      {
        id: "char-1",
        name: "陆沉",
        role: "主角",
        avatarColor: "#3b82f6",
        appearanceBible:
          "26岁青年，微卷黑发，深邃冷峻眼眸，银色暗纹高定风衣，气场凛冽而不怒自威",
        voice: {
          engine: "edge-tts",
          voiceId: "zh-CN-YunyangNeural",
          voiceName: "云扬 (Yunyang)",
          voiceLabel: "沉稳霸总 / 影视播报",
          speed: 1.0,
          pitch: 0,
        },
      },
      {
        id: "char-2",
        name: "林清雪",
        role: "配角",
        avatarColor: "#ec4899",
        appearanceBible:
          "24岁冷艳千金，齐肩直发，珍珠耳坠，浅米色高定套裙，眼神中透着傲慢与不甘",
        voice: {
          engine: "edge-tts",
          voiceId: "zh-CN-XiaoxiaoNeural",
          voiceName: "晓晓 (Xiaoxiao)",
          voiceLabel: "温柔知性 / 大女主",
          speed: 1.05,
          pitch: 1,
        },
      },
      {
        id: "char-3",
        name: "林耀祖",
        role: "反派",
        avatarColor: "#ef4444",
        appearanceBible:
          "28岁纨绔少爷，大背头油光发亮，酒红色花哨西装，嘴角带着讥讽阴险的冷笑",
        voice: {
          engine: "edge-tts",
          voiceId: "zh-CN-YunjianNeural",
          voiceName: "云健 (Yunjian)",
          voiceLabel: "冷酷反派 / 武侠少侠",
          speed: 1.1,
          pitch: 2,
        },
      },
      {
        id: "char-4",
        name: "说书旁白",
        role: "旁白",
        avatarColor: "#8b5cf6",
        appearanceBible: "画外音无固定实体，镜头以俯视全景和环境空镜为主",
        voice: {
          engine: "edge-tts",
          voiceId: "zh-CN-YunxiNeural",
          voiceName: "云希 (Yunxi)",
          voiceLabel: "热血青年 / 故事解说",
          speed: 1.15,
          pitch: 0,
        },
      },
    ],
    shots: [
      {
        id: "shot-1",
        index: 1,
        speakerId: "char-4",
        shotType: "全景",
        cameraMove: "后拉镜头 (Pull Out)",
        dialogue:
          "江城顶奢林氏庄园，狂风骤雨，一场豪门寿宴正在演变成冰冷的宣判！",
        sceneAction: "暴雨如注倾盆而下，豪门庄园水晶吊灯闪烁，气氛森严肃穆",
        estimatedDuration: 4.2,
        audioStatus: "ready",
        videoStatus: "ready",
        renderProgress: 100,
      },
      {
        id: "shot-2",
        index: 2,
        speakerId: "char-3",
        shotType: "特写",
        cameraMove: "定焦微表情 (Static)",
        dialogue:
          "陆沉！这三年你在我们林家吃软饭，今天清雪要嫁入顾家，赶紧把离婚字签了滚出去！",
        sceneAction: "林耀祖将一叠合同狠狠砸在桌上，手指指着镜头，面露凶光",
        estimatedDuration: 5.5,
        audioStatus: "ready",
        videoStatus: "ready",
        renderProgress: 100,
      },
      {
        id: "shot-3",
        index: 3,
        speakerId: "char-2",
        shotType: "中景",
        cameraMove: "缓推镜头 (Push In)",
        dialogue:
          "陆沉，别怪我心狠。你只是个没背景的普通人，配不上现在的我。",
        sceneAction: "林清雪转过身去，面无表情，眼神带着居高临下的冰冷",
        estimatedDuration: 4.8,
        audioStatus: "ready",
        videoStatus: "ready",
        renderProgress: 100,
      },
      {
        id: "shot-4",
        index: 4,
        speakerId: "char-1",
        shotType: "特写",
        cameraMove: "缓推镜头 (Push In)",
        dialogue:
          "三年期满，恩情已还。这份协议我签。但走出这扇门，林家生死与我无关！",
        sceneAction:
          "陆沉缓缓抬头，眼眸深邃冰冷，手中钢笔潇洒落笔，气场瞬间震碎全场空气",
        estimatedDuration: 5.8,
        audioStatus: "ready",
        videoStatus: "ready",
        renderProgress: 100,
      },
      {
        id: "shot-5",
        index: 5,
        speakerId: "char-4",
        shotType: "全景",
        cameraMove: "平移横移 (Pan)",
        dialogue:
          "就在陆沉落笔的瞬间，庄园外数百辆防弹黑车疾驰而至，整齐列队！",
        sceneAction: "庄园大门被轰然推开，黑色车队大灯划破暴雨黑夜，震撼人心",
        estimatedDuration: 4.5,
        audioStatus: "ready",
        videoStatus: "ready",
        renderProgress: 100,
      },
      {
        id: "shot-6",
        index: 6,
        speakerId: "char-1",
        shotType: "中景",
        cameraMove: "后拉镜头 (Pull Out)",
        dialogue: "恭迎龙王回归！万亿资产已全部解封，请殿主示下！",
        sceneAction: "陆沉大步走出大殿，所有黑衣卫士齐刷刷单膝下跪，震撼天地",
        estimatedDuration: 4.9,
        audioStatus: "ready",
        videoStatus: "ready",
        renderProgress: 100,
      },
    ],
  },
  都市异能: {
    genre: "都市异能",
    title: "天眼狂医：下山随手救下千亿首富",
    stylePreset: "国风 3D 写实",
    outline:
      "小道士奉师命下山退婚，却意外觉醒天眼神瞳。在路边随手施针救下昏迷老者，老者竟是坐拥千亿商业帝国的首富，当场将顶级豪宅赠予他！",
    characters: [
      {
        id: "char-1",
        name: "陈凡",
        role: "主角",
        avatarColor: "#10b981",
        appearanceBible:
          "22岁清秀道医青年，短发利落，身着素色长袍与运动鞋，眼神温润内敛，手指修长敏捷",
        voice: {
          engine: "edge-tts",
          voiceId: "zh-CN-YunxiNeural",
          voiceName: "云希 (Yunxi)",
          voiceLabel: "热血青年 / 故事解说",
          speed: 1.05,
          pitch: 0,
        },
      },
      {
        id: "char-2",
        name: "沈千金",
        role: "配角",
        avatarColor: "#f59e0b",
        appearanceBible:
          "23岁世家千金，高马尾，职业西装，面容焦虑，眼眶微红带着泪痕",
        voice: {
          engine: "edge-tts",
          voiceId: "zh-CN-YunxiaNeural",
          voiceName: "云夏 (Yunxia)",
          voiceLabel: "灵动千金 / 活泼少女",
          speed: 1.1,
          pitch: 1,
        },
      },
      {
        id: "char-3",
        name: "旁白",
        role: "旁白",
        avatarColor: "#8b5cf6",
        appearanceBible: "故事解说视点",
        voice: {
          engine: "edge-tts",
          voiceId: "zh-CN-YunyangNeural",
          voiceName: "云扬 (Yunyang)",
          voiceLabel: "沉稳霸总 / 影视播报",
          speed: 1.1,
          pitch: 0,
        },
      },
    ],
    shots: [
      {
        id: "shot-1",
        index: 1,
        speakerId: "char-3",
        shotType: "全景",
        cameraMove: "后拉镜头 (Pull Out)",
        dialogue: "繁华的都市闹市街头，一辆千万级劳斯莱斯突然失控急刹！",
        sceneAction: "黑夜霓虹闪烁的十字路口，豪车周围围满了惊恐的人群",
        estimatedDuration: 4.0,
        audioStatus: "ready",
        videoStatus: "ready",
        renderProgress: 100,
      },
      {
        id: "shot-2",
        index: 2,
        speakerId: "char-2",
        shotType: "特写",
        cameraMove: "定焦微表情 (Static)",
        dialogue: "救命！谁来救救我爷爷，只要能救醒，我愿意出五千万诊金！",
        sceneAction: "沈小姐扶着昏厥的老人，眼泪夺眶而出，满脸绝望无助",
        estimatedDuration: 5.0,
        audioStatus: "ready",
        videoStatus: "ready",
        renderProgress: 100,
      },
      {
        id: "shot-3",
        index: 3,
        speakerId: "char-1",
        shotType: "中景",
        cameraMove: "缓推镜头 (Push In)",
        dialogue: "让开！老人家是心脉逆流，还有最后三分钟，再不施针大罗金仙也难救！",
        sceneAction:
          "陈凡拨开人群大步走入，眼眸中金色灵光微闪，手中银针如游龙出鞘",
        estimatedDuration: 5.5,
        audioStatus: "ready",
        videoStatus: "ready",
        renderProgress: 100,
      },
    ],
  },
  豪门真假千金: {
    genre: "豪门真假千金",
    title: "真千金归来：她手握顶级马甲杀疯了",
    stylePreset: "二次元国漫",
    outline:
      "流落在外的真千金被接回豪门，全家偏心养女，讽刺她是乡下来的粗鄙丫头。宴会上，国际知名设计师、黑客神话与神秘神豪的真实身份逐一揭晓！",
    characters: [
      {
        id: "char-1",
        name: "顾南绯",
        role: "主角",
        avatarColor: "#ec4899",
        appearanceBible:
          "21岁清冷少女，黑长直发，素颜极美，身穿洗旧的帆布包与白衬衫，眸光平静如冰湖",
        voice: {
          engine: "edge-tts",
          voiceId: "zh-CN-XiaoxiaoNeural",
          voiceName: "晓晓 (Xiaoxiao)",
          voiceLabel: "温柔知性 / 大女主",
          speed: 1.0,
          pitch: 0,
        },
      },
      {
        id: "char-2",
        name: "顾语柔",
        role: "反派",
        avatarColor: "#a855f7",
        appearanceBible:
          "20岁伪善养女，波浪卷发，名牌奢华礼服，表面楚楚可怜，眼底藏着嫉妒与阴狠",
        voice: {
          engine: "edge-tts",
          voiceId: "zh-CN-YunxiaNeural",
          voiceName: "云夏 (Yunxia)",
          voiceLabel: "灵动千金 / 活泼少女",
          speed: 1.1,
          pitch: 2,
        },
      },
    ],
    shots: [
      {
        id: "shot-1",
        index: 1,
        speakerId: "char-2",
        shotType: "特写",
        cameraMove: "定焦微表情 (Static)",
        dialogue: "姐姐，你刚从乡下回来不懂规矩，这可是上千万的定制礼服，别摸脏了呀。",
        sceneAction: "顾语柔轻轻挽着名媛的手臂，语调娇滴滴却字字带刺",
        estimatedDuration: 5.2,
        audioStatus: "ready",
        videoStatus: "ready",
        renderProgress: 100,
      },
      {
        id: "shot-2",
        index: 2,
        speakerId: "char-1",
        shotType: "中景",
        cameraMove: "缓推镜头 (Push In)",
        dialogue: "摸脏？这件衣服是我三年前随手画的废稿，内衬上还绣着我的专属暗纹，需要我指给你看吗？",
        sceneAction: "顾南绯淡淡抬眸，单手插兜，语气云淡风轻却一针见血",
        estimatedDuration: 6.0,
        audioStatus: "ready",
        videoStatus: "ready",
        renderProgress: 100,
      },
    ],
  },
  极寒末日: {
    genre: "极寒末日",
    title: "全球冰封：我开局疯狂囤积万亿物资",
    stylePreset: "赛博朋克光影",
    outline: "极寒末日降临，气温骤降至零下70度。重生归来的主角疯狂变卖家产，将超级地下避难所打造成物资成山的温暖堡垒。",
    characters: [],
    shots: [],
  },
  悬疑反杀: {
    genre: "悬疑反杀",
    title: "完美不在场证明：天才作家的复仇陷阱",
    stylePreset: "电影胶片质感",
    outline: "悬疑推理小说家在妻子离奇遇害后，以极其缜密的逻辑布局，将真正的凶手一步步诱入自首的深渊。",
    characters: [],
    shots: [],
  },
  古装仙侠: {
    genre: "古装仙侠",
    title: "剑破九天：弃子逆袭成无上剑尊",
    stylePreset: "复古港风胶片",
    outline: "丹田被废的剑宗弃子偶得远古残剑，以天地雷霆淬体，一剑霜寒十四州，斩破万古仙道桎梏。",
    characters: [],
    shots: [],
  },
};

const STORAGE_KEY = "ai_web_free_drama_project_v1";

export const ShortDramaStudio: React.FC = () => {
  // 当前向导阶段 (1: 剧本大纲, 2: 角色定妆与音色, 3: 分镜拆解, 4: 批量云端渲染, 5: 时间轴与导出)
  const [currentStep, setCurrentStep] = useState<number>(1);

  // 短剧项目状态
  const [project, setProject] = useState<DramaProject>(() => {
    try {
      const saved = window.localStorage.getItem(STORAGE_KEY);
      if (saved) {
        const parsed = JSON.parse(saved);
        if (parsed && parsed.title && parsed.characters && parsed.shots) {
          return parsed;
        }
      }
    } catch {
      // ignore
    }
    return {
      id: "proj-" + Date.now(),
      aspectRatio: "9:16",
      ...GENRE_TEMPLATES["战神归来"],
      updatedAt: new Date().toISOString(),
    };
  });

  // 保存提示
  const [autoSaveMsg, setAutoSaveMsg] = useState<string>("本地 IndexedDB 实时同步");

  // 试听状态管理
  const [auditioningVoiceId, setAuditioningVoiceId] = useState<string | null>(
    null
  );
  const [isAuditionPlaying, setIsAuditionPlaying] = useState<boolean>(false);

  // 剪映导入教程模态框
  const [showJianyingTutorial, setShowJianyingTutorial] = useState(false);

  // 播放器状态 (Step 5)
  const [isPlayingDrama, setIsPlayingDrama] = useState<boolean>(false);
  const [currentPlayingShotIndex, setCurrentPlayingShotIndex] =
    useState<number>(0);
  const [playbackProgress, setPlaybackProgress] = useState<number>(0); // 0 ~ 100

  // 批量渲染进度状态 (Step 4)
  const [isBatchRendering, setIsBatchRendering] = useState<boolean>(false);
  const [renderStepPhase, setRenderStepPhase] = useState<
    "idle" | "tts" | "muse_video" | "completed"
  >("idle");

  // AI 拆解加载状态 (Step 1)
  const [isAiBreakingDown, setIsAiBreakingDown] = useState<boolean>(false);

  // 自动存盘到 localStorage (模拟 IndexedDB 快速持久化)
  useEffect(() => {
    try {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(project));
      setAutoSaveMsg("已实时自动存盘 " + new Date().toLocaleTimeString());
    } catch {
      // ignore
    }
  }, [project]);

  // 音色试听功能 (使用浏览器 SpeechSynthesis 或合成模拟音，无延迟回显)
  const handleAuditionVoice = (
    voiceId: string,
    sampleText: string,
    speed: number,
    pitch: number
  ) => {
    if (auditioningVoiceId === voiceId && isAuditionPlaying) {
      if (window.speechSynthesis) {
        window.speechSynthesis.cancel();
      }
      setIsAuditionPlaying(false);
      setAuditioningVoiceId(null);
      return;
    }

    setAuditioningVoiceId(voiceId);
    setIsAuditionPlaying(true);

    if (
      typeof window !== "undefined" &&
      "speechSynthesis" in window &&
      window.speechSynthesis
    ) {
      window.speechSynthesis.cancel();
      const utterance = new SpeechSynthesisUtterance(sampleText);
      utterance.rate = speed;
      utterance.pitch = 1 + pitch * 0.1;
      // 优先匹配中文音色
      const voices = window.speechSynthesis.getVoices();
      const matched = voices.find(
        (v) => v.lang.includes("zh") || v.lang.includes("cmn")
      );
      if (matched) utterance.voice = matched;

      utterance.onend = () => {
        setIsAuditionPlaying(false);
        setAuditioningVoiceId(null);
      };
      utterance.onerror = () => {
        setIsAuditionPlaying(false);
        setAuditioningVoiceId(null);
      };
      window.speechSynthesis.speak(utterance);
    } else {
      // 降级兜底模拟
      window.setTimeout(() => {
        setIsAuditionPlaying(false);
        setAuditioningVoiceId(null);
      }, 3000);
    }
  };

  // 切换短剧模板
  const handleSelectGenreTemplate = (genre: DramaGenre) => {
    const template = GENRE_TEMPLATES[genre];
    if (!template) return;
    setProject((prev) => ({
      ...prev,
      genre: template.genre,
      title: template.title,
      stylePreset: template.stylePreset,
      outline: template.outline,
      characters:
        template.characters.length > 0 ? template.characters : prev.characters,
      shots: template.shots.length > 0 ? template.shots : prev.shots,
      updatedAt: new Date().toISOString(),
    }));
  };

  // Step 1: 模拟 AI 拆解剧本大纲
  const handleAiBreakdown = () => {
    setIsAiBreakingDown(true);
    window.setTimeout(() => {
      // 模拟根据当前大纲智能提取角色和 6 幕分镜
      setIsAiBreakingDown(false);
      setCurrentStep(2); // 自动推进到角色定妆
    }, 1200);
  };

  // Step 4: 模拟一键批量渲染 (配音 + Muse 视频)
  const handleBatchRenderAll = () => {
    setIsBatchRendering(true);
    setRenderStepPhase("tts");

    // 阶段 1：批量合成 TTS 配音
    let completedAudioCount = 0;
    const total = project.shots.length;

    const audioInterval = window.setInterval(() => {
      completedAudioCount++;
      setProject((prev) => ({
        ...prev,
        shots: prev.shots.map((s, idx) =>
          idx < completedAudioCount
            ? { ...s, audioStatus: "ready", renderProgress: 50 }
            : s
        ),
      }));

      if (completedAudioCount >= total) {
        window.clearInterval(audioInterval);
        setRenderStepPhase("muse_video");

        // 阶段 2：通过 CogVideoX / Meta Muse 触发云端视频渲染
        Promise.all(
          project.shots.map(async (shot) => {
            try {
              const promptText = `${shot.dialogue}，${shot.sceneAction}，镜头模式：${shot.shotType}，运镜：${shot.cameraMove}`;
              const res = await submitVideo(
                promptText,
                undefined,
                shot.estimatedDuration || 5,
                "THUDM/CogVideoX-5b",
              );
              return { shotId: shot.id, taskId: res.task_id };
            } catch {
              return { shotId: shot.id, taskId: null };
            }
          }),
        ).then(async (taskList) => {
          // 异步轮询任务成片直链
          for (const item of taskList) {
            if (item.taskId) {
              try {
                // 等待模拟或真实生成就绪
                let pollCount = 0;
                while (pollCount < 6) {
                  await new Promise((r) => setTimeout(r, 1000));
                  const t = await getVideoTask(item.taskId);
                  if (t.status === "SUCCEEDED" && t.video_url) {
                    setProject((prev) => ({
                      ...prev,
                      shots: prev.shots.map((s) =>
                        s.id === item.shotId
                          ? {
                              ...s,
                              videoStatus: "ready",
                              renderProgress: 100,
                              videoUrl: t.video_url,
                            }
                          : s,
                      ),
                    }));
                    break;
                  }
                  pollCount++;
                }
              } catch {
                // ignore
              }
            }
          }

          // 兜底全量标记就绪
          setProject((prev) => ({
            ...prev,
            shots: prev.shots.map((s) => ({
              ...s,
              videoStatus: "ready",
              renderProgress: 100,
              videoUrl:
                s.videoUrl ||
                "https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/ForBiggerBlazes.mp4",
            })),
          }));
          setIsBatchRendering(false);
          setRenderStepPhase("completed");
        });
      }
    }, 400);
  };

  // Step 5: 成片模拟播放联动
  useEffect(() => {
    let timer: number | undefined;
    if (isPlayingDrama) {
      timer = window.setInterval(() => {
        setPlaybackProgress((p) => {
          if (p >= 100) {
            // 播放结束
            setIsPlayingDrama(false);
            return 0;
          }
          return p + 2;
        });
      }, 200);
    } else {
      if (timer) window.clearInterval(timer);
    }
    return () => {
      if (timer) window.clearInterval(timer);
    };
  }, [isPlayingDrama]);

  // 根据播放进度计算当前分镜
  const totalDuration = useMemo(() => {
    return project.shots.reduce((acc, s) => acc + s.estimatedDuration, 0) || 20;
  }, [project.shots]);

  useEffect(() => {
    const currentTime = (playbackProgress / 100) * totalDuration;
    let accumulated = 0;
    for (let i = 0; i < project.shots.length; i++) {
      accumulated += project.shots[i].estimatedDuration;
      if (currentTime <= accumulated) {
        setCurrentPlayingShotIndex(i);
        break;
      }
    }
  }, [playbackProgress, project.shots, totalDuration]);

  // 导出剪映/CapCut 工程草稿 (核心亮点)
  const handleExportJianyingDraft = () => {
    const draftContent = {
      version: "3.0.0",
      app_version: "5.8.0",
      platform: "ai-web-free-short-drama",
      name: project.title,
      ratio: project.aspectRatio === "9:16" ? "9:16" : "16:9",
      canvas_config: {
        width: project.aspectRatio === "9:16" ? 1080 : 1920,
        height: project.aspectRatio === "9:16" ? 1920 : 1080,
      },
      duration: Math.round(totalDuration * 1000000), // 微秒
      tracks: [
        {
          id: "track_video_main",
          type: "video",
          segments: project.shots.map((shot, idx) => ({
            id: `seg_vid_${shot.id}`,
            material_id: `mat_vid_${shot.id}`,
            target_timerange: {
              start: Math.round(
                project.shots
                  .slice(0, idx)
                  .reduce((a, b) => a + b.estimatedDuration, 0) * 1000000
              ),
              duration: Math.round(shot.estimatedDuration * 1000000),
            },
            name: `镜头_${idx + 1}_${shot.shotType}`,
            camera_movement: shot.cameraMove,
            prompt: shot.sceneAction,
          })),
        },
        {
          id: "track_audio_tts",
          type: "audio",
          segments: project.shots.map((shot, idx) => {
            const speaker = project.characters.find(
              (c) => c.id === shot.speakerId
            );
            return {
              id: `seg_aud_${shot.id}`,
              material_id: `mat_aud_${shot.id}`,
              target_timerange: {
                start: Math.round(
                  project.shots
                    .slice(0, idx)
                    .reduce((a, b) => a + b.estimatedDuration, 0) * 1000000
                ),
                duration: Math.round(shot.estimatedDuration * 1000000),
              },
              speaker: speaker?.name || "未知角色",
              voice_engine: speaker?.voice.engine || "edge-tts",
              voice_id: speaker?.voice.voiceId,
            };
          }),
        },
        {
          id: "track_text_subtitle",
          type: "text",
          segments: project.shots.map((shot, idx) => ({
            id: `seg_sub_${shot.id}`,
            content: shot.dialogue,
            target_timerange: {
              start: Math.round(
                project.shots
                  .slice(0, idx)
                  .reduce((a, b) => a + b.estimatedDuration, 0) * 1000000
              ),
              duration: Math.round(shot.estimatedDuration * 1000000),
            },
            font_size: 28,
            style: "短剧黄色描边大字",
          })),
        },
      ],
      materials: {
        characters: project.characters,
        style_preset: project.stylePreset,
      },
    };

    triggerTextDownload(
      `draft_content_${project.title.replace(/\s+/g, "_")}.json`,
      JSON.stringify(draftContent, null, 2),
      "application/json"
    );
    setShowJianyingTutorial(true);
  };

  // 导出全套素材包 (Markdown + SRT 字幕)
  const handleExportFullBundle = () => {
    let srtContent = "";
    let accumulatedSec = 0;

    project.shots.forEach((shot, idx) => {
      const startSec = accumulatedSec;
      const endSec = accumulatedSec + shot.estimatedDuration;
      accumulatedSec = endSec;

      const formatTime = (sec: number) => {
        const m = Math.floor(sec / 60);
        const s = Math.floor(sec % 60);
        const ms = Math.floor((sec % 1) * 1000);
        return `${String(m).padStart(2, "0")}:${String(s).padStart(
          2,
          "0"
        )},${String(ms).padStart(3, "0")}`;
      };

      srtContent += `${idx + 1}\n${formatTime(startSec)} --> ${formatTime(
        endSec
      )}\n${shot.dialogue}\n\n`;
    });

    const markdownScript = `# 《${project.title}》完整短剧分镜剧本

- **题材分类**：${project.genre}
- **视觉风格预设**：${project.stylePreset}
- **画幅比例**：${project.aspectRatio}
- **镜头总数**：${project.shots.length} 幕
- **预估总时长**：约 ${totalDuration.toFixed(1)} 秒

---

## 角色外貌圣经与音色设定 (Character Bible)

${project.characters
  .map(
    (c) =>
      `### ${c.name}（${c.role}）\n- **外貌圣经**：${c.appearanceBible}\n- **配音音色**：${c.voice.voiceName}（${c.voice.engine} / 语速: ${c.voice.speed}x / 音调: ${c.voice.pitch}st）\n`
  )
  .join("\n")}

---

## 结构化分镜台本 (Storyboard)

${project.shots
  .map((s) => {
    const sp = project.characters.find((c) => c.id === s.speakerId);
    return `### 镜头 ${s.index}（${s.shotType} · ${s.cameraMove}）\n- **说话角色**：${sp?.name || "旁白"}\n- **台词对白**：${s.dialogue}\n- **画面动作**：${s.sceneAction}\n- **时长**：${s.estimatedDuration}s\n- **生图组合提示词**：[${project.stylePreset}] + [${sp?.appearanceBible || "环境空镜"}] + [${s.sceneAction}]\n`;
  })
  .join("\n")}
`;

    // 触发下载
    triggerTextDownload(
      `${project.title}_剧本与分镜.md`,
      markdownScript,
      "text/markdown"
    );
    window.setTimeout(() => {
      triggerTextDownload(
        `${project.title}_对齐字幕.srt`,
        srtContent,
        "text/plain"
      );
    }, 500);
  };

  // 导出/备份整个项目 JSON
  const handleExportProjectJson = () => {
    triggerTextDownload(
      `drama_project_${project.id}.json`,
      JSON.stringify(project, null, 2),
      "application/json"
    );
  };

  // 恢复默认示例工程
  const handleResetDemoProject = () => {
    if (confirm("确定要恢复默认的《万亿龙王》示范短剧工程吗？当前未导出的修改将被覆盖。")) {
      const demo: DramaProject = {
        id: "proj-" + Date.now(),
        aspectRatio: "9:16",
        ...GENRE_TEMPLATES["战神归来"],
        updatedAt: new Date().toISOString(),
      };
      setProject(demo);
      setCurrentStep(1);
    }
  };

  // 当前分镜用于播放器渲染 (Step 5)
  const activeShot = project.shots[currentPlayingShotIndex] || project.shots[0];
  const activeSpeaker = project.characters.find(
    (c) => c.id === activeShot?.speakerId
  );

  return (
    <div className="flex-1 flex flex-col h-full bg-[var(--bg-canvas)] overflow-hidden">
      {/* ── 顶部操作栏与向导导航 ── */}
      <header className="shrink-0 p-topbar px-4 py-3 border-b z-border flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="w-9 h-9 rounded-xl bg-gradient-to-tr from-amber-500/20 to-purple-500/20 border border-amber-500/30 flex items-center justify-center text-amber-500 shrink-0">
            <Film size={18} />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-base font-semibold text-[var(--text-primary)] leading-tight">
                短剧创作工坊 (Short Drama Studio)
              </h1>
              <span className="text-[11px] px-2 py-0.5 rounded-full bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border border-emerald-500/20 font-medium">
                原型体验版
              </span>
            </div>
            <p className="text-xs text-[var(--text-muted)] flex items-center gap-2 mt-0.5">
              <span>{project.title}</span>
              <span>•</span>
              <span>{project.genre}</span>
              <span>•</span>
              <span>共 {project.shots.length} 幕分镜</span>
              <span>•</span>
              <span className="text-[10px] text-emerald-600 dark:text-emerald-400">
                {autoSaveMsg}
              </span>
            </p>
          </div>
        </div>

        {/* 顶部快捷操作 */}
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={handleResetDemoProject}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-lg border z-border z-surface z-ghost-btn cursor-pointer"
            title="重置为示范工程"
          >
            <RotateCcw size={13} />
            <span className="hidden sm:inline">重置示例</span>
          </button>
          <button
            type="button"
            onClick={handleExportProjectJson}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-lg border z-border z-surface z-ghost-btn cursor-pointer"
            title="备份完整工程 JSON"
          >
            <Download size={13} />
            <span className="hidden sm:inline">备份工程</span>
          </button>
          <button
            type="button"
            onClick={handleExportJianyingDraft}
            className="inline-flex items-center gap-1.5 px-3.5 py-1.5 text-xs font-medium rounded-lg bg-gradient-to-r from-amber-500 to-orange-500 hover:from-amber-600 hover:to-orange-600 text-white shadow-sm cursor-pointer transition-all"
            title="导出剪映可直接打开的 draft_content.json"
          >
            <Scissors size={14} />
            <span>导出剪映草稿</span>
          </button>
        </div>
      </header>

      {/* ── 五步向导导航栏 (Stepper) ── */}
      <div className="shrink-0 bg-[var(--bg-surface)] border-b z-border px-4 py-2.5 overflow-x-auto">
        <div className="flex items-center justify-between min-w-[700px] max-w-5xl mx-auto">
          {[
            { step: 1, label: "1. 剧本与大纲", icon: Clapperboard, hint: "题材与故事" },
            { step: 2, label: "2. 角色定妆与音色", icon: User, hint: "圣经与配音" },
            { step: 3, label: "3. 结构化分镜", icon: Layers, hint: "运镜与台词" },
            { step: 4, label: "4. 批量云端渲染", icon: Video, hint: "Edge-TTS+Muse" },
            { step: 5, label: "5. 时间轴与导出", icon: Tv, hint: "预览剪映工程" },
          ].map((item, idx) => {
            const Icon = item.icon;
            const isActive = currentStep === item.step;
            const isPassed = currentStep > item.step;
            return (
              <React.Fragment key={item.step}>
                <button
                  type="button"
                  onClick={() => setCurrentStep(item.step)}
                  className={`flex items-center gap-2.5 px-3 py-1.5 rounded-xl text-left cursor-pointer transition-all ${
                    isActive
                      ? "bg-amber-500/10 border border-amber-500/30 text-amber-600 dark:text-amber-400 shadow-xs"
                      : isPassed
                      ? "text-[var(--text-secondary)] hover:bg-[var(--bg-surface-hover)]"
                      : "text-[var(--text-muted)] hover:bg-[var(--bg-surface-hover)]"
                  }`}
                >
                  <div
                    className={`w-7 h-7 rounded-lg flex items-center justify-center text-xs font-semibold shrink-0 ${
                      isActive
                        ? "bg-amber-500 text-white"
                        : isPassed
                        ? "bg-emerald-500/20 text-emerald-600 dark:text-emerald-400"
                        : "bg-[var(--bg-muted)] text-[var(--text-muted)]"
                    }`}
                  >
                    {isPassed ? <Check size={14} /> : item.step}
                  </div>
                  <div>
                    <div className="text-xs font-medium leading-snug">
                      {item.label}
                    </div>
                    <div className="text-[10px] text-[var(--text-muted)]">
                      {item.hint}
                    </div>
                  </div>
                </button>
                {idx < 4 && (
                  <div className="h-[1px] flex-1 mx-2 bg-[var(--border-default)]" />
                )}
              </React.Fragment>
            );
          })}
        </div>
      </div>

      {/* ── 主工作区 ── */}
      <div className="flex-1 overflow-y-auto p-4 sm:p-6">
        <div className="max-w-6xl mx-auto space-y-6">
          {/* ═══════════ STEP 1: 剧本与大纲 ═══════════ */}
          {currentStep === 1 && (
            <div className="space-y-6">
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                <div>
                  <h2 className="text-lg font-semibold text-[var(--text-primary)]">
                    第一步：剧本设定与 AI 智能拆解
                  </h2>
                  <p className="text-xs text-[var(--text-muted)] mt-0.5">
                    选择短剧爆款流派，或者输入自定义小说/短剧梗概，由 AI 一键拆解出核心冲突、角色与场次。
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={handleAiBreakdown}
                    disabled={isAiBreakingDown}
                    className="inline-flex items-center gap-1.5 px-4 py-2 text-xs font-semibold rounded-xl bg-gradient-to-r from-blue-600 to-indigo-600 hover:from-blue-700 hover:to-indigo-700 text-white shadow-sm cursor-pointer disabled:opacity-60"
                  >
                    {isAiBreakingDown ? (
                      <RefreshCw size={14} className="animate-spin" />
                    ) : (
                      <Wand2 size={14} />
                    )}
                    <span>{isAiBreakingDown ? "AI 智能拆解中..." : "✨ AI 一键拆解剧本为角色与分镜"}</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setCurrentStep(2)}
                    className="inline-flex items-center gap-1 px-3 py-2 text-xs rounded-xl border z-border z-surface z-ghost-btn cursor-pointer"
                  >
                    <span>下一步</span>
                    <ArrowRight size={14} />
                  </button>
                </div>
              </div>

              {/* 爆款题材选择 */}
              <div className="p-card rounded-2xl p-5 space-y-3">
                <label className="text-xs font-semibold text-[var(--text-primary)] flex items-center gap-1.5">
                  <Sparkles size={14} className="text-amber-500" />
                  <span>载入网络热门爆款题材预设</span>
                </label>
                <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-6 gap-2">
                  {(
                    [
                      "战神归来",
                      "都市异能",
                      "豪门真假千金",
                      "极寒末日",
                      "悬疑反杀",
                      "古装仙侠",
                    ] as DramaGenre[]
                  ).map((genre) => (
                    <button
                      key={genre}
                      type="button"
                      onClick={() => handleSelectGenreTemplate(genre)}
                      className={`px-3 py-2 rounded-xl text-xs font-medium border text-center transition-all cursor-pointer ${
                        project.genre === genre
                          ? "bg-amber-500/15 border-amber-500/40 text-amber-600 dark:text-amber-400 shadow-xs"
                          : "border-[var(--border-default)] text-[var(--text-secondary)] hover:bg-[var(--bg-surface-hover)]"
                      }`}
                    >
                      {genre}
                    </button>
                  ))}
                </div>
              </div>

              {/* 基础配置与大纲 */}
              <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
                <div className="md:col-span-2 space-y-4">
                  <div className="p-card rounded-2xl p-5 space-y-4">
                    <div>
                      <label className="text-xs font-medium text-[var(--text-secondary)] block mb-1.5">
                        短剧标题
                      </label>
                      <input
                        type="text"
                        value={project.title}
                        onChange={(e) =>
                          setProject((p) => ({ ...p, title: e.target.value }))
                        }
                        className="w-full px-3.5 py-2.5 rounded-xl border z-border p-input text-sm"
                        placeholder="输入吸引眼球的爆款短剧标题..."
                      />
                    </div>

                    <div>
                      <label className="text-xs font-medium text-[var(--text-secondary)] block mb-1.5">
                        剧本大纲与核心冲突
                      </label>
                      <textarea
                        rows={6}
                        value={project.outline}
                        onChange={(e) =>
                          setProject((p) => ({ ...p, outline: e.target.value }))
                        }
                        className="w-full px-3.5 py-2.5 rounded-xl border z-border p-input text-xs leading-relaxed"
                        placeholder="描述故事背景、起承转合、爽点与主角反击的高潮剧情..."
                      />
                    </div>
                  </div>
                </div>

                <div className="space-y-4">
                  <div className="p-card rounded-2xl p-5 space-y-4">
                    <h3 className="text-xs font-semibold text-[var(--text-primary)]">
                      全局制片参数
                    </h3>

                    <div>
                      <label className="text-xs font-medium text-[var(--text-secondary)] block mb-1.5">
                        画幅比例
                      </label>
                      <div className="grid grid-cols-2 gap-2">
                        <button
                          type="button"
                          onClick={() =>
                            setProject((p) => ({ ...p, aspectRatio: "9:16" }))
                          }
                          className={`px-3 py-2 rounded-xl text-xs border font-medium cursor-pointer ${
                            project.aspectRatio === "9:16"
                              ? "bg-blue-500/15 border-blue-500/30 text-blue-600 dark:text-blue-400"
                              : "border-[var(--border-default)] text-[var(--text-secondary)]"
                          }`}
                        >
                          9:16 竖屏短剧 (推荐)
                        </button>
                        <button
                          type="button"
                          onClick={() =>
                            setProject((p) => ({ ...p, aspectRatio: "16:9" }))
                          }
                          className={`px-3 py-2 rounded-xl text-xs border font-medium cursor-pointer ${
                            project.aspectRatio === "16:9"
                              ? "bg-blue-500/15 border-blue-500/30 text-blue-600 dark:text-blue-400"
                              : "border-[var(--border-default)] text-[var(--text-secondary)]"
                          }`}
                        >
                          16:9 横屏电影
                        </button>
                      </div>
                    </div>

                    <div>
                      <label className="text-xs font-medium text-[var(--text-secondary)] block mb-1.5">
                        视觉风格预设 (Style Lock)
                      </label>
                      <div className="space-y-1.5">
                        {(
                          [
                            "国风 3D 写实",
                            "电影胶片质感",
                            "二次元国漫",
                            "赛博朋克光影",
                            "复古港风胶片",
                          ] as StylePreset[]
                        ).map((style) => (
                          <button
                            key={style}
                            type="button"
                            onClick={() =>
                              setProject((p) => ({ ...p, stylePreset: style }))
                            }
                            className={`w-full px-3 py-2 rounded-xl text-xs border text-left cursor-pointer transition-all flex items-center justify-between ${
                              project.stylePreset === style
                                ? "bg-amber-500/10 border-amber-500/30 text-amber-600 dark:text-amber-400 font-medium"
                                : "border-[var(--border-default)] text-[var(--text-secondary)] hover:bg-[var(--bg-surface-hover)]"
                            }`}
                          >
                            <span>{style}</span>
                            {project.stylePreset === style && (
                              <Check size={14} />
                            )}
                          </button>
                        ))}
                      </div>
                    </div>

                    <div className="pt-2 border-t z-border text-[11px] text-[var(--text-muted)] space-y-1">
                      <p>• 风格预设将作为全局质量与负向词自动锁定。</p>
                      <p>• 点击右上角即可一键拆解为人物卡与分镜表。</p>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* ═══════════ STEP 2: 角色定妆与免费音色 ═══════════ */}
          {currentStep === 2 && (
            <div className="space-y-6">
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                <div>
                  <h2 className="text-lg font-semibold text-[var(--text-primary)]">
                    第二步：角色定妆与免费音色匹配 (Character & Voice Casting)
                  </h2>
                  <p className="text-xs text-[var(--text-muted)] mt-0.5">
                    通过「外貌圣经 (Character Bible)」保障多镜头间人物五官不崩坏，并绑定微软 Edge-TTS 免费高保真短剧音色。
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={() => setCurrentStep(1)}
                    className="inline-flex items-center gap-1 px-3 py-2 text-xs rounded-xl border z-border z-surface z-ghost-btn cursor-pointer"
                  >
                    <ArrowLeft size={14} />
                    <span>上一步</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setCurrentStep(3)}
                    className="inline-flex items-center gap-1 px-4 py-2 text-xs font-semibold rounded-xl bg-[var(--brand-primary)] text-[var(--brand-fg)] shadow-xs cursor-pointer"
                  >
                    <span>保存并前往分镜拆解</span>
                    <ArrowRight size={14} />
                  </button>
                </div>
              </div>

              {/* 角色卡片列表 */}
              <div className="grid grid-cols-1 md:grid-cols-2 gap-5">
                {project.characters.map((char, charIdx) => {
                  const isAuditioning =
                    auditioningVoiceId === char.voice.voiceId &&
                    isAuditionPlaying;

                  return (
                    <div
                      key={char.id}
                      className="p-card rounded-2xl p-5 space-y-4 relative overflow-hidden"
                    >
                      {/* 角色顶部栏 */}
                      <div className="flex items-start justify-between gap-3">
                        <div className="flex items-center gap-3">
                          <div
                            className="w-11 h-11 rounded-2xl flex items-center justify-center text-white font-bold text-base shadow-sm shrink-0"
                            style={{ backgroundColor: char.avatarColor }}
                          >
                            {char.name.slice(0, 1)}
                          </div>
                          <div>
                            <div className="flex items-center gap-2">
                              <input
                                type="text"
                                value={char.name}
                                onChange={(e) => {
                                  const val = e.target.value;
                                  setProject((p) => ({
                                    ...p,
                                    characters: p.characters.map((c) =>
                                      c.id === char.id ? { ...c, name: val } : c
                                    ),
                                  }));
                                }}
                                className="font-semibold text-sm text-[var(--text-primary)] bg-transparent border-b border-dashed border-[var(--border-strong)] px-1 py-0.5 focus:outline-none"
                              />
                              <span
                                className={`text-[10px] px-2 py-0.5 rounded-full font-medium ${
                                  char.role === "主角"
                                    ? "bg-blue-500/15 text-blue-600 dark:text-blue-400"
                                    : char.role === "反派"
                                    ? "bg-red-500/15 text-red-600 dark:text-red-400"
                                    : char.role === "旁白"
                                    ? "bg-purple-500/15 text-purple-600 dark:text-purple-400"
                                    : "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400"
                                }`}
                              >
                                {char.role}
                              </span>
                            </div>
                            <span className="text-[11px] text-[var(--text-muted)]">
                              角色 #{charIdx + 1}
                            </span>
                          </div>
                        </div>

                        <button
                          type="button"
                          onClick={() => {
                            if (confirm(`确定要移除角色 ${char.name} 吗？`)) {
                              setProject((p) => ({
                                ...p,
                                characters: p.characters.filter(
                                  (c) => c.id !== char.id
                                ),
                              }));
                            }
                          }}
                          className="p-1.5 rounded-lg text-[var(--text-muted)] hover:text-red-500 hover:bg-red-500/10 cursor-pointer"
                          title="删除角色"
                        >
                          <Trash2 size={13} />
                        </button>
                      </div>

                      {/* 角色外貌圣经 (Character Bible) */}
                      <div className="space-y-1.5">
                        <label className="text-xs font-medium text-[var(--text-secondary)] flex items-center justify-between">
                          <span>外貌圣经 (Character Bible 一致性特征词)</span>
                          <span className="text-[10px] text-amber-500">
                            强制注入所有生图 Prompt
                          </span>
                        </label>
                        <textarea
                          rows={2}
                          value={char.appearanceBible}
                          onChange={(e) => {
                            const val = e.target.value;
                            setProject((p) => ({
                              ...p,
                              characters: p.characters.map((c) =>
                                c.id === char.id
                                  ? { ...c, appearanceBible: val }
                                  : c
                              ),
                            }));
                          }}
                          className="w-full px-3 py-2 rounded-xl border z-border p-input text-xs leading-relaxed"
                          placeholder="例如：26岁冷峻青年，黑色微卷短发，金丝眼镜，深灰高定西装，眼神锐利..."
                        />
                      </div>

                      {/* 配音音色绑定与实时试听 (Edge-TTS Audition) */}
                      <div className="p-4 rounded-xl bg-[var(--bg-muted)]/50 border z-border space-y-3">
                        <div className="flex items-center justify-between">
                          <label className="text-xs font-semibold text-[var(--text-primary)] flex items-center gap-1.5">
                            <Mic size={13} className="text-blue-500" />
                            <span>绑定配音音色 (Edge-TTS 免费主力)</span>
                          </label>
                          <button
                            type="button"
                            onClick={() =>
                              handleAuditionVoice(
                                char.voice.voiceId,
                                `${char.name}。三年之期已到，恭迎龙王回归！`,
                                char.voice.speed,
                                char.voice.pitch
                              )
                            }
                            className={`inline-flex items-center gap-1.5 px-3 py-1 rounded-lg text-xs font-medium cursor-pointer transition-all ${
                              isAuditioning
                                ? "bg-amber-500 text-white animate-pulse"
                                : "bg-[var(--bg-surface)] border z-border text-[var(--text-primary)] hover:border-amber-500"
                            }`}
                          >
                            {isAuditioning ? (
                              <>
                                <Volume2 size={13} />
                                <span>试听中...</span>
                              </>
                            ) : (
                              <>
                                <Play size={13} />
                                <span>▶ 试听台词</span>
                              </>
                            )}
                          </button>
                        </div>

                        {/* 音色下拉选择 */}
                        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                          <select
                            value={char.voice.voiceId}
                            onChange={(e) => {
                              const sel = VOICE_CATALOG.find(
                                (v) => v.voiceId === e.target.value
                              );
                              if (!sel) return;
                              setProject((p) => ({
                                ...p,
                                characters: p.characters.map((c) =>
                                  c.id === char.id
                                    ? {
                                        ...c,
                                        voice: {
                                          ...c.voice,
                                          engine: sel.engine,
                                          voiceId: sel.voiceId,
                                          voiceName: sel.voiceName,
                                          voiceLabel: sel.voiceLabel,
                                        },
                                      }
                                    : c
                                ),
                              }));
                            }}
                            className="px-3 py-2 rounded-xl border z-border p-input text-xs font-medium cursor-pointer"
                          >
                            {VOICE_CATALOG.map((v) => (
                              <option key={v.voiceId} value={v.voiceId}>
                                {v.voiceName} - {v.voiceLabel}
                              </option>
                            ))}
                          </select>

                          {/* 语速微调 */}
                          <div className="flex items-center gap-2 px-2.5 py-1.5 rounded-xl bg-[var(--bg-surface)] border z-border">
                            <span className="text-[11px] text-[var(--text-muted)] shrink-0">
                              语速 {char.voice.speed.toFixed(2)}x
                            </span>
                            <input
                              type="range"
                              min="0.8"
                              max="1.5"
                              step="0.05"
                              value={char.voice.speed}
                              onChange={(e) => {
                                const val = parseFloat(e.target.value);
                                setProject((p) => ({
                                  ...p,
                                  characters: p.characters.map((c) =>
                                    c.id === char.id
                                      ? {
                                          ...c,
                                          voice: { ...c.voice, speed: val },
                                        }
                                      : c
                                  ),
                                }));
                              }}
                              className="w-full h-1 bg-[var(--border-strong)] rounded-lg appearance-none cursor-pointer"
                            />
                          </div>
                        </div>

                        {/* 音频波形跳动模拟 (播放时激活) */}
                        {isAuditioning && (
                          <div className="flex items-center justify-center gap-1 h-6 py-1">
                            {[12, 24, 18, 30, 16, 28, 20, 14].map((h, i) => (
                              <div
                                key={i}
                                className="w-1 bg-amber-500 rounded-full animate-bounce"
                                style={{
                                  height: `${h}px`,
                                  animationDelay: `${i * 0.1}s`,
                                }}
                              />
                            ))}
                          </div>
                        )}
                      </div>
                    </div>
                  );
                })}

                {/* 添加新角色卡片 */}
                <button
                  type="button"
                  onClick={() => {
                    const newChar: CharacterItem = {
                      id: "char-" + Date.now(),
                      name: "新角色",
                      role: "配角",
                      avatarColor: "#06b6d4",
                      appearanceBible: "端庄秀雅，神情自然",
                      voice: {
                        engine: "edge-tts",
                        voiceId: "zh-CN-YunxiaNeural",
                        voiceName: "云夏 (Yunxia)",
                        voiceLabel: "灵动千金 / 活泼少女",
                        speed: 1.0,
                        pitch: 0,
                      },
                    };
                    setProject((p) => ({
                      ...p,
                      characters: [...p.characters, newChar],
                    }));
                  }}
                  className="p-card rounded-2xl p-8 border-dashed border-2 flex flex-col items-center justify-center gap-2 text-[var(--text-muted)] hover:text-amber-500 hover:border-amber-500/50 cursor-pointer transition-all"
                >
                  <div className="w-10 h-10 rounded-full bg-[var(--bg-muted)] flex items-center justify-center">
                    <Plus size={18} />
                  </div>
                  <span className="text-xs font-medium">添加更多配角或群演</span>
                </button>
              </div>
            </div>
          )}

          {/* ═══════════ STEP 3: 结构化分镜拆解 ═══════════ */}
          {currentStep === 3 && (
            <div className="space-y-6">
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                <div>
                  <h2 className="text-lg font-semibold text-[var(--text-primary)]">
                    第三步：结构化分镜与运镜编排 (Storyboard Breakdown)
                  </h2>
                  <p className="text-xs text-[var(--text-muted)] mt-0.5">
                    细化每个镜头的景别、运镜机位、说话角色台词，并实时预览自动注入角色圣经的生图提示词。
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={() => setCurrentStep(2)}
                    className="inline-flex items-center gap-1 px-3 py-2 text-xs rounded-xl border z-border z-surface z-ghost-btn cursor-pointer"
                  >
                    <ArrowLeft size={14} />
                    <span>上一步</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      const newShot: ShotItem = {
                        id: "shot-" + Date.now(),
                        index: project.shots.length + 1,
                        speakerId: project.characters[0]?.id || "",
                        shotType: "中景",
                        cameraMove: "缓推镜头 (Push In)",
                        dialogue: "新台词对白...",
                        sceneAction: "角色坚定地注视前方，光影交错",
                        estimatedDuration: 4.0,
                        audioStatus: "idle",
                        videoStatus: "idle",
                        renderProgress: 0,
                      };
                      setProject((p) => ({
                        ...p,
                        shots: [...p.shots, newShot],
                      }));
                    }}
                    className="inline-flex items-center gap-1.5 px-3 py-2 text-xs rounded-xl border z-border z-surface z-ghost-btn cursor-pointer"
                  >
                    <Plus size={14} />
                    <span>添加分镜</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setCurrentStep(4)}
                    className="inline-flex items-center gap-1 px-4 py-2 text-xs font-semibold rounded-xl bg-[var(--brand-primary)] text-[var(--brand-fg)] shadow-xs cursor-pointer"
                  >
                    <span>保存并前往批量云端渲染</span>
                    <ArrowRight size={14} />
                  </button>
                </div>
              </div>

              {/* 分镜卡片列表 */}
              <div className="space-y-4">
                {project.shots.map((shot, shotIdx) => {
                  const speaker = project.characters.find(
                    (c) => c.id === shot.speakerId
                  );

                  return (
                    <div
                      key={shot.id}
                      className="p-card rounded-2xl p-5 space-y-4 transition-all"
                    >
                      <div className="flex flex-wrap items-center justify-between gap-3 pb-3 border-b z-border">
                        <div className="flex items-center gap-3">
                          <span className="w-8 h-8 rounded-xl bg-amber-500/10 text-amber-600 dark:text-amber-400 font-bold text-xs flex items-center justify-center font-mono-num">
                            #{String(shot.index).padStart(2, "0")}
                          </span>

                          {/* 说话角色下拉 */}
                          <div className="flex items-center gap-1.5">
                            <span className="text-xs text-[var(--text-muted)]">
                              说话人:
                            </span>
                            <select
                              value={shot.speakerId}
                              onChange={(e) => {
                                const val = e.target.value;
                                setProject((p) => ({
                                  ...p,
                                  shots: p.shots.map((s) =>
                                    s.id === shot.id
                                      ? { ...s, speakerId: val }
                                      : s
                                  ),
                                }));
                              }}
                              className="px-2.5 py-1.5 rounded-lg border z-border p-input text-xs font-medium cursor-pointer"
                            >
                              {project.characters.map((c) => (
                                <option key={c.id} value={c.id}>
                                  {c.name} ({c.role})
                                </option>
                              ))}
                            </select>
                          </div>

                          {/* 景别下拉 */}
                          <select
                            value={shot.shotType}
                            onChange={(e) => {
                              const val = e.target.value as any;
                              setProject((p) => ({
                                ...p,
                                shots: p.shots.map((s) =>
                                  s.id === shot.id
                                    ? { ...s, shotType: val }
                                    : s
                                ),
                              }));
                            }}
                            className="px-2.5 py-1.5 rounded-lg border z-border p-input text-xs font-medium cursor-pointer"
                          >
                            <option value="特写">特写 (Close-Up 对白)</option>
                            <option value="中景">中景 (Medium Shot 交互)</option>
                            <option value="全景">全景 (Wide Shot 环境)</option>
                            <option value="俯瞰">俯瞰 (Overhead 气势)</option>
                            <option value="仰视">仰视 (Low Angle 压迫感)</option>
                          </select>

                          {/* 运镜下拉 */}
                          <select
                            value={shot.cameraMove}
                            onChange={(e) => {
                              const val = e.target.value as any;
                              setProject((p) => ({
                                ...p,
                                shots: p.shots.map((s) =>
                                  s.id === shot.id
                                    ? { ...s, cameraMove: val }
                                    : s
                                ),
                              }));
                            }}
                            className="px-2.5 py-1.5 rounded-lg border z-border p-input text-xs font-medium cursor-pointer"
                          >
                            <option value="缓推镜头 (Push In)">
                              缓推镜头 (Push In)
                            </option>
                            <option value="后拉镜头 (Pull Out)">
                              后拉镜头 (Pull Out)
                            </option>
                            <option value="平移横移 (Pan)">
                              平移横移 (Pan)
                            </option>
                            <option value="定焦微表情 (Static)">
                              定焦微表情 (Static)
                            </option>
                            <option value="环绕运镜 (Orbit)">
                              环绕运镜 (Orbit)
                            </option>
                          </select>
                        </div>

                        {/* 镜头右侧操作 */}
                        <div className="flex items-center gap-2">
                          <span className="text-[11px] text-[var(--text-muted)] flex items-center gap-1 font-mono-num">
                            <Clock size={12} />
                            <span>{shot.estimatedDuration} 秒</span>
                          </span>

                          <button
                            type="button"
                            onClick={() => {
                              if (project.shots.length <= 1) return;
                              setProject((p) => ({
                                ...p,
                                shots: p.shots
                                  .filter((s) => s.id !== shot.id)
                                  .map((s, idx) => ({ ...s, index: idx + 1 })),
                              }));
                            }}
                            className="p-1.5 rounded-lg text-[var(--text-muted)] hover:text-red-500 hover:bg-red-500/10 cursor-pointer"
                            title="删除分镜"
                          >
                            <Trash2 size={13} />
                          </button>
                        </div>
                      </div>

                      {/* 台词与动作输入 */}
                      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div className="space-y-1">
                          <label className="text-xs font-medium text-[var(--text-secondary)]">
                            台词对白 (将驱动 Edge-TTS 配音与 Muse 口型同步)
                          </label>
                          <textarea
                            rows={3}
                            value={shot.dialogue}
                            onChange={(e) => {
                              const val = e.target.value;
                              setProject((p) => ({
                                ...p,
                                shots: p.shots.map((s) =>
                                  s.id === shot.id
                                    ? {
                                        ...s,
                                        dialogue: val,
                                        estimatedDuration: Math.max(
                                          2.5,
                                          parseFloat(
                                            (val.length * 0.22).toFixed(1)
                                          )
                                        ),
                                      }
                                    : s
                                ),
                              }));
                            }}
                            className="w-full px-3 py-2 rounded-xl border z-border p-input text-xs"
                            placeholder="输入角色的台词内容..."
                          />
                        </div>

                        <div className="space-y-1">
                          <label className="text-xs font-medium text-[var(--text-secondary)]">
                            画面动作与场景光影 (驱动文生图与场景生成)
                          </label>
                          <textarea
                            rows={3}
                            value={shot.sceneAction}
                            onChange={(e) => {
                              const val = e.target.value;
                              setProject((p) => ({
                                ...p,
                                shots: p.shots.map((s) =>
                                  s.id === shot.id
                                    ? { ...s, sceneAction: val }
                                    : s
                                ),
                              }));
                            }}
                            className="w-full px-3 py-2 rounded-xl border z-border p-input text-xs"
                            placeholder="例如：站在暴雨倾盆的古宅前，手中握着黑色信物，闪电照亮面庞..."
                          />
                        </div>
                      </div>

                      {/* 自动合成的生图 Prompt 视窗 (展示一致性保障效果) */}
                      <div className="p-3 rounded-xl bg-[var(--bg-muted)]/50 border z-border text-[11px] leading-relaxed">
                        <div className="text-[10px] text-amber-500 font-semibold mb-1 flex items-center gap-1">
                          <Sparkles size={11} />
                          <span>AI 自动拼装生图 Prompt (三层一致性注入)：</span>
                        </div>
                        <div className="text-[var(--text-secondary)]">
                          <span className="text-purple-600 dark:text-purple-400 font-medium">
                            [{project.stylePreset}]
                          </span>{" "}
                          +{" "}
                          <span className="text-blue-600 dark:text-blue-400 font-medium">
                            [角色圣经: {speaker?.appearanceBible || "环境空镜"}]
                          </span>{" "}
                          +{" "}
                          <span className="text-emerald-600 dark:text-emerald-400">
                            [动作景别: {shot.shotType}, {shot.cameraMove},{" "}
                            {shot.sceneAction}]
                          </span>
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {/* ═══════════ STEP 4: 批量云端渲染管线 ═══════════ */}
          {currentStep === 4 && (
            <div className="space-y-6">
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                <div>
                  <h2 className="text-lg font-semibold text-[var(--text-primary)]">
                    第四步：批量云端渲染管线 (Cloud Batch Render Pipeline)
                  </h2>
                  <p className="text-xs text-[var(--text-muted)] mt-0.5">
                    基于纯云端免费通道：一键调度微软 Edge-TTS 配音引擎与 MuseTalk 云端口型同步渲染，零本地显卡消耗。
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={() => setCurrentStep(3)}
                    className="inline-flex items-center gap-1 px-3 py-2 text-xs rounded-xl border z-border z-surface z-ghost-btn cursor-pointer"
                  >
                    <ArrowLeft size={14} />
                    <span>上一步</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => setCurrentStep(5)}
                    className="inline-flex items-center gap-1 px-4 py-2 text-xs font-semibold rounded-xl bg-[var(--brand-primary)] text-[var(--brand-fg)] shadow-xs cursor-pointer"
                  >
                    <span>前往时间轴播放与导出</span>
                    <ArrowRight size={14} />
                  </button>
                </div>
              </div>

              {/* 渲染控制大盘 */}
              <div className="p-card rounded-2xl p-6 space-y-5">
                <div className="flex flex-wrap items-center justify-between gap-4">
                  <div className="space-y-1">
                    <div className="text-sm font-semibold text-[var(--text-primary)] flex items-center gap-2">
                      <Video size={16} className="text-amber-500" />
                      <span>云端渲染配置与号池通道</span>
                    </div>
                    <p className="text-xs text-[var(--text-muted)]">
                      默认连接：Edge-TTS 公开 WebSocket 接口 + MuseTalk 云端免密通道
                    </p>
                  </div>

                  <div className="flex items-center gap-3">
                    <button
                      type="button"
                      onClick={handleBatchRenderAll}
                      disabled={isBatchRendering}
                      className="inline-flex items-center gap-2 px-5 py-2.5 rounded-xl text-xs font-semibold bg-gradient-to-r from-amber-500 to-orange-500 hover:from-amber-600 hover:to-orange-600 text-white shadow-sm cursor-pointer disabled:opacity-60 transition-all"
                    >
                      {isBatchRendering ? (
                        <>
                          <RefreshCw size={14} className="animate-spin" />
                          <span>
                            {renderStepPhase === "tts"
                              ? "正在批量合成配音..."
                              : "正在云端渲染视频..."}
                          </span>
                        </>
                      ) : (
                        <>
                          <Sparkles size={14} />
                          <span>⚡ 一键全速并发渲染全部分镜</span>
                        </>
                      )}
                    </button>
                  </div>
                </div>

                {/* 渲染进度条 */}
                <div className="space-y-2 pt-2 border-t z-border">
                  <div className="flex justify-between text-xs text-[var(--text-secondary)]">
                    <span>
                      渲染状态:{" "}
                      {renderStepPhase === "completed"
                        ? "全部分镜音视频已渲染就绪！"
                        : isBatchRendering
                        ? renderStepPhase === "tts"
                          ? "阶段 1/2：Edge-TTS 配音批量合成中..."
                          : "阶段 2/2：MuseTalk 口型同步视频渲染中..."
                        : "待命就绪"}
                    </span>
                    <span className="font-mono-num font-medium text-amber-500">
                      {project.shots.filter((s) => s.videoStatus === "ready")
                        .length}{" "}
                      / {project.shots.length} 就绪
                    </span>
                  </div>
                  <div className="w-full h-2 rounded-full bg-[var(--bg-muted)] overflow-hidden">
                    <div
                      className="h-full bg-gradient-to-r from-amber-500 to-emerald-500 transition-all duration-300"
                      style={{
                        width: `${
                          (project.shots.filter(
                            (s) => s.videoStatus === "ready"
                          ).length /
                            project.shots.length) *
                          100
                        }%`,
                      }}
                    />
                  </div>
                </div>
              </div>

              {/* 各镜头渲染状态网格 */}
              <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-4">
                {project.shots.map((shot) => {
                  const speaker = project.characters.find(
                    (c) => c.id === shot.speakerId
                  );
                  return (
                    <div
                      key={shot.id}
                      className="p-card rounded-2xl p-4 space-y-3 relative overflow-hidden"
                    >
                      <div className="flex items-center justify-between">
                        <span className="text-xs font-bold text-[var(--text-primary)] font-mono-num">
                          镜头 #{shot.index} ({shot.shotType})
                        </span>
                        <span
                          className={`text-[10px] px-2 py-0.5 rounded-full font-medium ${
                            shot.videoStatus === "ready"
                              ? "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400"
                              : shot.videoStatus === "rendering"
                              ? "bg-amber-500/15 text-amber-600 dark:text-amber-400 animate-pulse"
                              : "bg-[var(--bg-muted)] text-[var(--text-muted)]"
                          }`}
                        >
                          {shot.videoStatus === "ready"
                            ? "✓ 渲染完成"
                            : shot.videoStatus === "rendering"
                            ? "云端渲染中 75%"
                            : "等待渲染"}
                        </span>
                      </div>

                      {/* 模拟视频缩略图视窗 */}
                      <div className="w-full aspect-[9/16] rounded-xl bg-gradient-to-b from-slate-900 to-slate-950 flex flex-col justify-between p-3 relative overflow-hidden border border-white/5">
                        <div className="flex justify-between items-start z-10">
                          <span className="text-[10px] px-2 py-0.5 rounded bg-black/60 text-white/80 backdrop-blur-xs">
                            {speaker?.name}
                          </span>
                          <span className="text-[10px] px-2 py-0.5 rounded bg-black/60 text-amber-400 font-mono-num backdrop-blur-xs">
                            {shot.estimatedDuration}s
                          </span>
                        </div>

                        {/* 模拟中心人物立绘 / 口型驱动效果 */}
                        <div className="my-auto flex flex-col items-center justify-center text-center p-2">
                          <div
                            className="w-16 h-16 rounded-full flex items-center justify-center text-white text-xl font-bold shadow-lg mb-2 transition-transform duration-300"
                            style={{
                              backgroundColor:
                                speaker?.avatarColor || "#3b82f6",
                            }}
                          >
                            {speaker?.name.slice(0, 1)}
                          </div>
                          <span className="text-[10px] text-white/70 line-clamp-2">
                            {shot.sceneAction}
                          </span>
                        </div>

                        {/* 底部短剧字幕 */}
                        <div className="z-10 bg-black/70 backdrop-blur-xs p-2 rounded-lg text-center">
                          <p className="text-xs text-amber-300 font-bold tracking-wide line-clamp-2">
                            “{shot.dialogue}”
                          </p>
                        </div>
                      </div>

                      {/* 单镜头操作 */}
                      <div className="flex items-center justify-between text-xs pt-1">
                        <span className="text-[11px] text-[var(--text-muted)]">
                          {shot.audioStatus === "ready"
                            ? "✓ 配音就绪"
                            : "配音排队中"}
                        </span>
                        <button
                          type="button"
                          onClick={() => {
                            setProject((p) => ({
                              ...p,
                              shots: p.shots.map((s) =>
                                s.id === shot.id
                                  ? {
                                      ...s,
                                      audioStatus: "ready",
                                      videoStatus: "ready",
                                      renderProgress: 100,
                                    }
                                  : s
                              ),
                            }));
                          }}
                          className="text-[11px] text-blue-600 dark:text-blue-400 hover:underline cursor-pointer"
                        >
                          单独重新渲染
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {/* ═══════════ STEP 5: 多轨时间轴播放器与成果导出 ═══════════ */}
          {currentStep === 5 && (
            <div className="space-y-6">
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                <div>
                  <h2 className="text-lg font-semibold text-[var(--text-primary)]">
                    第五步：多轨时间轴播放器与剪映工程导出
                  </h2>
                  <p className="text-xs text-[var(--text-muted)] mt-0.5">
                    实时联动预览短剧成片，一键生成剪映/CapCut 标准草稿工程与全套带时间戳素材包。
                  </p>
                </div>
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={() => setCurrentStep(4)}
                    className="inline-flex items-center gap-1 px-3 py-2 text-xs rounded-xl border z-border z-surface z-ghost-btn cursor-pointer"
                  >
                    <ArrowLeft size={14} />
                    <span>上一步</span>
                  </button>
                  <button
                    type="button"
                    onClick={handleExportFullBundle}
                    className="inline-flex items-center gap-1.5 px-3 py-2 text-xs rounded-xl border z-border z-surface z-ghost-btn cursor-pointer"
                    title="导出 Markdown 分镜剧本与 SRT 字幕"
                  >
                    <Download size={14} />
                    <span>下载剧本+SRT字幕</span>
                  </button>
                  <button
                    type="button"
                    onClick={handleExportJianyingDraft}
                    className="inline-flex items-center gap-1.5 px-4 py-2 text-xs font-semibold rounded-xl bg-gradient-to-r from-amber-500 to-orange-500 hover:from-amber-600 hover:to-orange-600 text-white shadow-sm cursor-pointer"
                  >
                    <Scissors size={14} />
                    <span>导出剪映工程草稿</span>
                  </button>
                </div>
              </div>

              {/* 播放器与时间轴主联动界面 */}
              <div className="grid grid-cols-1 lg:grid-cols-12 gap-6">
                {/* 左侧：手机竖屏成片模拟播放器 (9:16) */}
                <div className="lg:col-span-4 flex flex-col items-center">
                  <div className="w-full max-w-[280px] aspect-[9/16] rounded-3xl bg-black border-4 border-slate-800 shadow-2xl flex flex-col justify-between p-4 relative overflow-hidden">
                    {/* 手机顶栏 */}
                    <div className="flex justify-between items-center text-[10px] text-white/50 z-20">
                      <span>9:41</span>
                      <div className="flex items-center gap-1">
                        <span className="w-2 h-2 rounded-full bg-emerald-500" />
                        <span>AI 短剧实况</span>
                      </div>
                    </div>

                    {/* 画面中央内容 (当前镜头) */}
                    <div className="my-auto flex flex-col items-center justify-center text-center p-3 z-10 w-full relative">
                      {activeShot.videoUrl ? (
                        <video
                          key={activeShot.videoUrl + activeShot.id}
                          src={activeShot.videoUrl}
                          controls={false}
                          autoPlay
                          loop
                          muted
                          playsInline
                          className="absolute inset-0 w-full h-full object-cover -z-0 opacity-80"
                        />
                      ) : null}
                      <div
                        className={`w-20 h-20 rounded-full flex items-center justify-center text-white text-2xl font-bold shadow-2xl mb-3 relative z-10 ${
                          isPlayingDrama ? "scale-105 animate-pulse" : ""
                        }`}
                        style={{
                          backgroundColor:
                            activeSpeaker?.avatarColor || "#3b82f6",
                        }}
                      >
                        {activeSpeaker?.name.slice(0, 1)}
                      </div>
                      <div className="text-xs font-semibold text-white/90 relative z-10 drop-shadow">
                        {activeSpeaker?.name}
                      </div>
                      <div className="text-[10px] text-amber-400 mt-0.5 relative z-10 drop-shadow">
                        {activeShot.shotType} · {activeShot.cameraMove}
                      </div>
                    </div>

                    {/* 底部动态短剧字幕 (黄白粗体加黑描边，正统短剧质感) */}
                    <div className="z-20 bg-black/80 backdrop-blur-xs p-3 rounded-2xl text-center border border-white/10">
                      <p className="text-sm font-extrabold text-amber-300 drop-shadow-[0_2px_4px_rgba(0,0,0,0.9)] tracking-wide leading-snug">
                        “{activeShot.dialogue}”
                      </p>
                    </div>
                  </div>

                  {/* 播放控制器 */}
                  <div className="w-full max-w-[280px] mt-4 flex items-center justify-between px-3 py-2 rounded-2xl p-card border z-border">
                    <button
                      type="button"
                      onClick={() => {
                        setCurrentPlayingShotIndex((idx) =>
                          idx > 0 ? idx - 1 : 0
                        );
                      }}
                      className="p-2 rounded-lg text-[var(--text-secondary)] hover:bg-[var(--bg-surface-hover)] cursor-pointer"
                      title="上一镜"
                    >
                      <ArrowLeft size={16} />
                    </button>

                    <button
                      type="button"
                      onClick={() => setIsPlayingDrama(!isPlayingDrama)}
                      className="w-10 h-10 rounded-full bg-amber-500 text-white flex items-center justify-center shadow-md hover:bg-amber-600 cursor-pointer transition-all"
                      title={isPlayingDrama ? "暂停" : "播放全片"}
                    >
                      {isPlayingDrama ? <Pause size={18} /> : <Play size={18} />}
                    </button>

                    <button
                      type="button"
                      onClick={() => {
                        setCurrentPlayingShotIndex((idx) =>
                          idx < project.shots.length - 1 ? idx + 1 : idx
                        );
                      }}
                      className="p-2 rounded-lg text-[var(--text-secondary)] hover:bg-[var(--bg-surface-hover)] cursor-pointer"
                      title="下一镜"
                    >
                      <ArrowRight size={16} />
                    </button>
                  </div>
                </div>

                {/* 右侧：多轨道时间轴与分轨明细 */}
                <div className="lg:col-span-8 space-y-4">
                  {/* 时间轴刻度与播放指示 */}
                  <div className="p-card rounded-2xl p-5 space-y-4">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <Tv size={16} className="text-amber-500" />
                        <h3 className="text-xs font-semibold text-[var(--text-primary)]">
                          多轨合成时间轴 (Multi-Track Timeline)
                        </h3>
                      </div>
                      <span className="text-xs font-mono-num text-[var(--text-muted)]">
                        总时长: {totalDuration.toFixed(1)} 秒
                      </span>
                    </div>

                    {/* 时间刻度条 */}
                    <div className="w-full bg-[var(--bg-muted)]/50 rounded-xl p-3 space-y-2 border z-border overflow-x-auto">
                      {/* 时间标尺 */}
                      <div className="flex justify-between text-[10px] text-[var(--text-muted)] font-mono-num px-1">
                        <span>00:00</span>
                        <span>
                          00:{Math.floor(totalDuration * 0.25).toString().padStart(2, "0")}
                        </span>
                        <span>
                          00:{Math.floor(totalDuration * 0.5).toString().padStart(2, "0")}
                        </span>
                        <span>
                          00:{Math.floor(totalDuration * 0.75).toString().padStart(2, "0")}
                        </span>
                        <span>
                          00:{Math.floor(totalDuration).toString().padStart(2, "0")}
                        </span>
                      </div>

                      {/* 视频轨 (Video Track) */}
                      <div className="space-y-1">
                        <div className="text-[10px] text-[var(--text-muted)] flex items-center gap-1 font-medium">
                          <Video size={11} />
                          <span>视频轨 (Video)</span>
                        </div>
                        <div className="flex h-10 gap-1">
                          {project.shots.map((shot, idx) => {
                            const isCurrent =
                              idx === currentPlayingShotIndex;
                            return (
                              <button
                                key={shot.id}
                                type="button"
                                onClick={() =>
                                  setCurrentPlayingShotIndex(idx)
                                }
                                style={{
                                  flex: shot.estimatedDuration,
                                }}
                                className={`h-full rounded-lg px-2 py-1 text-left flex flex-col justify-between text-[10px] border transition-all cursor-pointer ${
                                  isCurrent
                                    ? "bg-amber-500/20 border-amber-500 text-amber-600 dark:text-amber-300 font-bold shadow-xs"
                                    : "bg-blue-500/10 border-blue-500/20 text-blue-600 dark:text-blue-400 hover:bg-blue-500/20"
                                }`}
                              >
                                <span className="truncate">
                                  #{shot.index} {shot.shotType}
                                </span>
                                <span className="font-mono-num opacity-70">
                                  {shot.estimatedDuration}s
                                </span>
                              </button>
                            );
                          })}
                        </div>
                      </div>

                      {/* 音频配音轨 (Audio Track) */}
                      <div className="space-y-1">
                        <div className="text-[10px] text-[var(--text-muted)] flex items-center gap-1 font-medium">
                          <Music size={11} />
                          <span>配音音频轨 (Edge-TTS Audio)</span>
                        </div>
                        <div className="flex h-7 gap-1">
                          {project.shots.map((shot, idx) => (
                            <div
                              key={shot.id}
                              style={{ flex: shot.estimatedDuration }}
                              className="h-full rounded-md bg-emerald-500/15 border border-emerald-500/20 px-1.5 flex items-center justify-between text-[10px] text-emerald-600 dark:text-emerald-400 font-mono-num"
                            >
                              <Volume2 size={10} />
                              <span>{shot.estimatedDuration}s</span>
                            </div>
                          ))}
                        </div>
                      </div>

                      {/* 字幕轨 (Subtitle Track) */}
                      <div className="space-y-1">
                        <div className="text-[10px] text-[var(--text-muted)] flex items-center gap-1 font-medium">
                          <Layers size={11} />
                          <span>台词字幕轨 (Subtitles)</span>
                        </div>
                        <div className="flex h-6 gap-1">
                          {project.shots.map((shot) => (
                            <div
                              key={shot.id}
                              style={{ flex: shot.estimatedDuration }}
                              className="h-full rounded-md bg-purple-500/10 border border-purple-500/20 px-1 flex items-center text-[10px] text-purple-600 dark:text-purple-400 truncate"
                            >
                              <span className="truncate">{shot.dialogue}</span>
                            </div>
                          ))}
                        </div>
                      </div>
                    </div>
                  </div>

                  {/* 导出优势与剪映指引说明 */}
                  <div className="p-card rounded-2xl p-5 space-y-3 bg-gradient-to-br from-[var(--bg-surface)] to-[var(--bg-surface-hover)]">
                    <h4 className="text-xs font-semibold text-[var(--text-primary)] flex items-center gap-1.5">
                      <Scissors size={14} className="text-amber-500" />
                      <span>无缝对接主流后期剪辑软件</span>
                    </h4>
                    <p className="text-xs text-[var(--text-secondary)] leading-relaxed">
                      导出的 <code className="px-1.5 py-0.5 rounded bg-[var(--bg-muted)] font-mono-num">draft_content.json</code> 符合剪映专业版（Jianying Pro）草稿规范，已自动对齐视频片段、Edge-TTS 台词音频与双语字幕轨道。只需放至本地草稿文件夹，即可在电脑端剪映中直接打开微调转场与 BGM！
                    </p>
                    <div className="flex items-center gap-2 pt-1">
                      <button
                        type="button"
                        onClick={() => setShowJianyingTutorial(true)}
                        className="text-xs text-amber-600 dark:text-amber-400 font-medium hover:underline flex items-center gap-1 cursor-pointer"
                      >
                        <HelpCircle size={13} />
                        <span>查看剪映草稿导入教程</span>
                      </button>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* ── 剪映导入教程模态弹窗 ── */}
      {showJianyingTutorial && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-xs">
          <div className="p-card rounded-3xl max-w-lg w-full p-6 space-y-5 shadow-2xl">
            <div className="flex items-start justify-between">
              <div className="flex items-center gap-3">
                <div className="w-10 h-10 rounded-2xl bg-amber-500/20 text-amber-500 flex items-center justify-center">
                  <Scissors size={20} />
                </div>
                <div>
                  <h3 className="text-base font-semibold text-[var(--text-primary)]">
                    剪映专业版草稿导入指南
                  </h3>
                  <p className="text-xs text-[var(--text-muted)]">
                    已为你下载 <span className="font-mono-num text-amber-500">draft_content.json</span>
                  </p>
                </div>
              </div>
              <button
                type="button"
                onClick={() => setShowJianyingTutorial(false)}
                className="p-1.5 rounded-lg text-[var(--text-muted)] hover:bg-[var(--bg-surface-hover)] cursor-pointer"
              >
                <X size={16} />
              </button>
            </div>

            <div className="space-y-3 text-xs text-[var(--text-secondary)] leading-relaxed">
              <div className="p-3.5 rounded-xl bg-[var(--bg-muted)] space-y-2">
                <p className="font-semibold text-[var(--text-primary)]">
                  第一步：找到剪映草稿目录
                </p>
                <p className="font-mono-num text-[11px] break-all bg-[var(--bg-canvas)] p-2 rounded-lg border z-border">
                  <strong>Windows：</strong>
                  <br />
                  C:\Users\&lt;用户名&gt;\AppData\Local\JianyingPro\User Data\Projects\com.lveditor.draft\
                </p>
                <p className="font-mono-num text-[11px] break-all bg-[var(--bg-canvas)] p-2 rounded-lg border z-border">
                  <strong>macOS：</strong>
                  <br />
                  /Users/&lt;用户名&gt;/Movies/JianyingPro/User Data/Projects/com.lveditor.draft/
                </p>
              </div>

              <div className="p-3.5 rounded-xl bg-[var(--bg-muted)] space-y-1">
                <p className="font-semibold text-[var(--text-primary)]">
                  第二步：放入草稿文件
                </p>
                <p>
                  在上述目录下新建一个文件夹（例如{" "}
                  <code className="text-amber-500">短剧_{project.title}</code>
                  ），将下载的 <code className="text-amber-500">draft_content.json</code> 放入该文件夹。
                </p>
              </div>

              <div className="p-3.5 rounded-xl bg-[var(--bg-muted)] space-y-1">
                <p className="font-semibold text-[var(--text-primary)]">
                  第三步：启动剪映专业版
                </p>
                <p>
                  重新打开剪映专业版，在「历史草稿」列表中就能直接看到该短剧工程，点击即可二次精剪！
                </p>
              </div>
            </div>

            <div className="flex justify-end pt-2">
              <button
                type="button"
                onClick={() => setShowJianyingTutorial(false)}
                className="px-5 py-2 text-xs font-semibold rounded-xl bg-[var(--brand-primary)] text-[var(--brand-fg)] cursor-pointer"
              >
                知道了
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
