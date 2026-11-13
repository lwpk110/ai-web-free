export type ModelCategory = "chat" | "image" | "audio";

export interface CatalogModel {
  id: string;
  category: ModelCategory;
  source: "upstream" | "builtin";
  subtitle: string;
}

const BUILTIN_MODEL_META: Record<
  string,
  { category: ModelCategory; subtitle: string }
> = {
  "gpt-image-2.5": { category: "image", subtitle: "标准平衡文生图 / 参考编辑" },
  "gpt-image-2.5-sunburst": {
    category: "image",
    subtitle: "高细节精绘 · 光影增强",
  },
  "gpt-image-2.5-flare": {
    category: "image",
    subtitle: "极速低延迟出图",
  },
  "gpt-image-2.6": { category: "image", subtitle: "最新预览图像档位" },
  "gpt-image-2": { category: "image", subtitle: "GPT-Image v2 兼容映射" },
  "gpt-image-1": { category: "image", subtitle: "OpenAI 标准文生图兼容别名" },
  "dall-e-3": { category: "image", subtitle: "DALL·E 3 兼容自动映射" },
  "gpt-4o-mini-tts": { category: "audio", subtitle: "多音色高质量语音合成" },
  "tts-1": { category: "audio", subtitle: "OpenAI TTS-1 语音合成别名" },
  "tts-1-hd": { category: "audio", subtitle: "高精度 TTS-1-HD 合成别名" },
  "gpt-4o-transcribe": { category: "audio", subtitle: "流式多语种语音识别" },
  "whisper-1": { category: "audio", subtitle: "Whisper STT 语音转文字 / 翻译" },
  "gpt-5-6": { category: "chat", subtitle: "默认旗舰对话与 Tool Calling" },
  "gpt-5-3": { category: "chat", subtitle: "默认平衡对话模型" },
  "gpt-5-3-instant": { category: "chat", subtitle: "低延迟即时对话" },
  "gpt-5-6-mini": { category: "chat", subtitle: "轻量快速响应模型" },
  "gpt-5-mini": { category: "chat", subtitle: "经济轻量对话模型" },
  "gpt-6": { category: "chat", subtitle: "长程推理对话模型" },
  "gpt-4o": { category: "chat", subtitle: "全能多模态对话兼容映射" },
  "gpt-4o-mini": { category: "chat", subtitle: "高速兼容轻量映射" },
  research: { category: "chat", subtitle: "深度研究与综合分析" },
};

export const BUILTIN_IDS = new Set([
  "gpt-image-2.5",
  "gpt-image-2.5-sunburst",
  "gpt-image-2.5-flare",
  "gpt-image-2.6",
  "gpt-image-2",
  "gpt-image-1",
  "dall-e-3",
  "gpt-4o-mini-tts",
  "tts-1",
  "tts-1-hd",
  "gpt-4o-transcribe",
  "whisper-1",
]);

export const FALLBACK_CATALOG_IDS: string[] = [
  "gpt-5-6",
  "gpt-5-3",
  "gpt-5-mini",
  "gpt-4o",
  "gpt-image-2.5-sunburst",
  "gpt-image-2.5-flare",
  "gpt-image-2.5",
  "gpt-image-2.6",
  "gpt-image-1",
  "dall-e-3",
  "gpt-4o-mini-tts",
  "tts-1",
  "tts-1-hd",
  "gpt-4o-transcribe",
  "whisper-1",
];

export function classifyModel(id: string): CatalogModel {
  const lower = id.trim().toLowerCase();
  const known = BUILTIN_MODEL_META[lower] || BUILTIN_MODEL_META[id];
  let category: ModelCategory = known?.category || "chat";
  if (!known) {
    if (
      lower.includes("image") ||
      lower.includes("dall-e") ||
      lower.includes("dalle")
    ) {
      category = "image";
    } else if (
      lower.includes("tts") ||
      lower.includes("whisper") ||
      lower.includes("transcribe") ||
      lower.includes("audio")
    ) {
      category = "audio";
    }
  }

  const isBuiltin = BUILTIN_IDS.has(lower);
  let subtitle = known?.subtitle;
  if (!subtitle) {
    if (lower.includes("thinking") || lower.includes("o1") || lower.includes("o3") || lower.includes("o4")) {
      subtitle = "深度思考推理模型";
    } else if (lower.includes("research")) {
      subtitle = "深度研究与检索分析";
    } else if (category === "image") {
      subtitle = "图像生成与编辑模型";
    } else if (category === "audio") {
      subtitle = "语音合成 / 转录模型";
    } else {
      subtitle = "ChatGPT 上游实时对话模型";
    }
  }

  return {
    id,
    category,
    source: isBuiltin ? "builtin" : "upstream",
    subtitle,
  };
}
