export interface RouterHistoryMessage {
  role: string;
  content: string;
}

export const DEFAULT_CHAT_MODELS = [
  "gpt-5-6",
  "gpt-5-3",
  "gpt-5-mini",
  "gpt-4o",
  "gpt-4o-mini",
  "o3-mini",
  "o1",
  "gpt-6",
  "research",
  "auto",
];

export const DEFAULT_IMAGE_MODELS = [
  "gpt-image-2.5-sunburst",
  "gpt-image-2.5-flare",
  "gpt-image-2.6",
  "gpt-image-2.5",
  "gpt-image-2",
  "gpt-image-1",
];

export const DEFAULT_TTS_MODELS = [
  "gpt-4o-mini-tts",
  "tts-1",
  "tts-1-hd",
];

export interface ParsedImageCommand {
  isImageCommand: boolean;
  prompt: string;
  model: string;
  isAuto: boolean;
}

export type RouteModality = "chat" | "image" | "audio";

export interface ModelRouteDecision {
  modality: RouteModality;
  targetModel: string;
  switched: boolean;
  isAutoImage: boolean;
  reason?: string;
  needsContextExtraction: boolean;
  cleanedPrompt: string;
  extractedPrompt?: string;
}

const IMAGE_STUDIO_WIDGET_RE = /(生图卡|image\s*studio|生图工作台)/i;
const PROMPT_WRITING_ONLY_RE =
  /(?:帮我|请)?(?:写|生成|整理|优化|润色|推荐|提供|给)(?:一个|一段|一份|几个)?.*?(?:提示词|prompt)\b/i;
const IMAGE_HOWTO_RE =
  /(?:怎么画|如何画|如何生成|有哪些生图模型|生图模型对比|画图教程)/i;
const NATURAL_IMAGE_GEN_RE =
  /(?:出图|生图|画图|文生图|(?:生成|重生成|再生成|绘制|重画|画|制作|创作)(?:一[张幅副个组]|[几两三]张)?[^\n，。,.!?！？]{0,18}?(?:图片|图像|插画|海报|壁纸|头像|写真|概念图|设定图|立绘|图)|^(?:画|绘)(?:一[张幅副个]|个|幅)|(?:generate|create|draw|render|make)\s+(?:an?\s+)?(?:image|picture|photo|illustration|artwork))/i;

const AUDIO_HOWTO_RE =
  /(?:怎么转语音|如何配音|有哪些语音模型|语音模型对比|tts\s*教程)/i;
const NATURAL_AUDIO_GEN_RE =
  /(?:朗读|念出来|读出来|转成语音|转语音|生成语音|合成语音|语音播报|配音|read\s+aloud|text\s+to\s+speech|speak\s+(?:this|it|that)|generate\s+audio)/i;

const RESEARCH_INTENT_RE =
  /(?:深度研究|深度调研|全面调研|深入调研|研究报告|deep\s*research)/i;

const CONTEXT_REF_RE =
  /(?:最新|上面|上述|以上|刚才|刚刚|前面|之前|这个|那个|这段|上一版|该提示词|照着|按此|按照|根据|继续|再生成|重画|重新生成|latest|above|previous|that\s+prompt|this\s+prompt|based\s+on)/i;

const EXPLICIT_MODEL_MENTION_RE =
  /(?:用|使用|切换(?:到|为)?|换(?:成|为)?|调用|use|switch\s+to)\s*[`"']?([a-z0-9][a-z0-9._-]{1,48})[`"']?/i;

export function isImageModelId(
  modelId: string,
  candidateImageModels: string[] = DEFAULT_IMAGE_MODELS
): boolean {
  if (!modelId) return false;
  if (
    candidateImageModels.some(
      (m) => m.toLowerCase() === modelId.toLowerCase()
    )
  ) {
    return true;
  }
  return /^(gpt-image-|dall-e-|flux-|wanx-|jimeng-)/i.test(modelId);
}

export function isTtsModelId(
  modelId: string,
  candidateTtsModels: string[] = DEFAULT_TTS_MODELS
): boolean {
  if (!modelId) return false;
  if (
    candidateTtsModels.some((m) => m.toLowerCase() === modelId.toLowerCase())
  ) {
    return true;
  }
  return /(?:^tts-|-tts$)/i.test(modelId);
}

export function parseImageCommand(
  rawText: string,
  candidateImageModels: string[]
): ParsedImageCommand {
  const trimmed = rawText.trim();
  if (
    trimmed !== "/image" &&
    !trimmed.startsWith("/image ") &&
    !trimmed.startsWith("/image\t")
  ) {
    return { isImageCommand: false, prompt: "", model: "", isAuto: false };
  }

  const pool =
    candidateImageModels && candidateImageModels.length > 0
      ? candidateImageModels
      : DEFAULT_IMAGE_MODELS;

  const pickRandomModel = () =>
    pool[Math.floor(Math.random() * pool.length)] || DEFAULT_IMAGE_MODELS[0];

  const rest = trimmed.slice(6).trim();
  if (!rest) {
    return {
      isImageCommand: true,
      prompt: "超写实电影级商业摄影，精致的光影构图，8k 高清画质",
      model: pickRandomModel(),
      isAuto: true,
    };
  }

  const parts = rest.split(/\s+/);
  const firstWord = parts[0];
  const remainder = rest.slice(firstWord.length).trim();

  if (firstWord.toLowerCase() === "auto") {
    return {
      isImageCommand: true,
      prompt: remainder || "超写实电影级商业摄影，精致的光影构图，8k 高清画质",
      model: pickRandomModel(),
      isAuto: true,
    };
  }

  const matched = pool.find(
    (m) => m.toLowerCase() === firstWord.toLowerCase()
  );
  if (matched && remainder) {
    return {
      isImageCommand: true,
      prompt: remainder,
      model: matched,
      isAuto: false,
    };
  }

  return {
    isImageCommand: true,
    prompt: rest,
    model: pickRandomModel(),
    isAuto: true,
  };
}

export function detectNaturalImageIntent(promptText: string): boolean {
  const trimmed = promptText.trim();
  if (!trimmed || trimmed.startsWith("/")) return false;
  if (IMAGE_STUDIO_WIDGET_RE.test(trimmed)) return false;
  if (IMAGE_HOWTO_RE.test(trimmed)) return false;
  if (
    PROMPT_WRITING_ONLY_RE.test(trimmed) &&
    !/(出图|生图|画图|生成图片|生成图像|画一张)/i.test(trimmed)
  ) {
    return false;
  }
  return NATURAL_IMAGE_GEN_RE.test(trimmed);
}

export function detectNaturalAudioIntent(promptText: string): boolean {
  const trimmed = promptText.trim();
  if (!trimmed || trimmed.startsWith("/")) return false;
  if (AUDIO_HOWTO_RE.test(trimmed)) return false;
  return NATURAL_AUDIO_GEN_RE.test(trimmed);
}

export function detectResearchIntent(promptText: string): boolean {
  const trimmed = promptText.trim();
  if (!trimmed || trimmed.startsWith("/")) return false;
  return RESEARCH_INTENT_RE.test(trimmed);
}

export function needsContextExtraction(
  promptText: string,
  history: RouterHistoryMessage[]
): boolean {
  if (history.length === 0) return false;
  const trimmed = promptText.trim();
  return CONTEXT_REF_RE.test(trimmed) || trimmed.length <= 24;
}

export function stripLeadingImageCommandWords(promptText: string): string {
  return (
    promptText
      .trim()
      .replace(
        /^(?:请|帮我|麻烦|给我)?(?:再|重新)?(?:生成|绘制|画|制作|创作|出图|生图)(?:一[张幅副个组]|个|幅)?(?:图片|图像|图)?[：:，,\s]*/i,
        ""
      )
      .trim() || promptText.trim()
  );
}

export function stripLeadingAudioCommandWords(promptText: string): string {
  return (
    promptText
      .trim()
      .replace(
        /^(?:请|帮我|麻烦|给我)?(?:朗读|念出来|读出来|转成语音|转语音|生成语音|合成语音|语音播报|配音)[：:，,\s]*/i,
        ""
      )
      .trim() || promptText.trim()
  );
}

export function extractPromptFallback(
  history: RouterHistoryMessage[],
  userRequest: string
): string {
  for (let i = history.length - 1; i >= 0; i--) {
    const msg = history[i];
    if (!msg.content || !msg.content.trim()) continue;
    const codeMatch = /```(?:text|prompt|markdown)?\s*\n([\s\S]*?)```/i.exec(
      msg.content
    );
    if (codeMatch && codeMatch[1]?.trim()) {
      return codeMatch[1].trim();
    }
    if (msg.role === "assistant" && msg.content.trim().length >= 30) {
      return msg.content.trim().slice(0, 1200);
    }
  }
  return stripLeadingImageCommandWords(userRequest);
}

export function extractSpeechFallback(
  history: RouterHistoryMessage[],
  userRequest: string
): string {
  for (let i = history.length - 1; i >= 0; i--) {
    const msg = history[i];
    if (!msg.content || !msg.content.trim()) continue;
    if (msg.role === "assistant") {
      // 去掉代码块与标记语法，保留自然语言正文
      const clean = msg.content
        .replace(/```[\s\S]*?```/g, "")
        .replace(/<[^>]+>/g, " ")
        .replace(/[*_`>#~]/g, "")
        .replace(/\s+/g, " ")
        .trim();
      if (clean.length >= 4) {
        return clean.slice(0, 2000);
      }
    }
  }
  return stripLeadingAudioCommandWords(userRequest);
}

function findMentionedModel(
  promptText: string,
  allKnownModels: string[]
): string | undefined {
  const m = EXPLICIT_MODEL_MENTION_RE.exec(promptText);
  if (!m || !m[1]) return undefined;
  const candidate = m[1].toLowerCase();
  return allKnownModels.find((id) => id.toLowerCase() === candidate);
}

export interface ResolveModelRouteParams {
  promptText: string;
  selectedModel: string;
  hasExplicitModelOverride: boolean;
  autoSwitchEnabled: boolean;
  history: RouterHistoryMessage[];
  availableChatModels: string[];
  availableImageModels: string[];
  availableAudioModels: string[];
}

export function resolveContextualModelRoute(
  params: ResolveModelRouteParams
): ModelRouteDecision {
  const {
    promptText,
    selectedModel,
    hasExplicitModelOverride,
    autoSwitchEnabled,
    history,
    availableChatModels,
    availableImageModels,
    availableAudioModels,
  } = params;

  const imgPool =
    availableImageModels.length > 0 ? availableImageModels : DEFAULT_IMAGE_MODELS;
  const ttsPool =
    availableAudioModels.length > 0 ? availableAudioModels : DEFAULT_TTS_MODELS;

  // 1. 显式 /image 斜杠指令优先级最高
  const imgCmd = parseImageCommand(promptText, imgPool);
  if (imgCmd.isImageCommand) {
    return {
      modality: "image",
      targetModel: imgCmd.model,
      switched: imgCmd.model.toLowerCase() !== selectedModel.toLowerCase(),
      isAutoImage: imgCmd.isAuto,
      reason: "/image 指令",
      needsContextExtraction: false,
      cleanedPrompt: imgCmd.prompt,
    };
  }

  // 2. 消息下方「换模型」显式覆盖或当前顶部已选中生图/语音模型
  if (hasExplicitModelOverride || isImageModelId(selectedModel, imgPool) || isTtsModelId(selectedModel, ttsPool)) {
    if (isImageModelId(selectedModel, imgPool)) {
      return {
        modality: "image",
        targetModel: selectedModel,
        switched: false,
        isAutoImage: false,
        reason: hasExplicitModelOverride ? "手动切换图像模型" : "当前已选图像模型",
        needsContextExtraction: needsContextExtraction(promptText, history),
        cleanedPrompt: stripLeadingImageCommandWords(promptText),
      };
    }
    if (isTtsModelId(selectedModel, ttsPool)) {
      return {
        modality: "audio",
        targetModel: selectedModel,
        switched: false,
        isAutoImage: false,
        reason: hasExplicitModelOverride ? "手动切换语音模型" : "当前已选语音模型",
        needsContextExtraction: needsContextExtraction(promptText, history),
        cleanedPrompt: stripLeadingAudioCommandWords(promptText),
      };
    }
    if (hasExplicitModelOverride) {
      return {
        modality: "chat",
        targetModel: selectedModel,
        switched: false,
        isAutoImage: false,
        needsContextExtraction: false,
        cleanedPrompt: promptText,
      };
    }
  }

  // 若用户关闭了自动切模，直接沿用当前对话模型
  if (!autoSwitchEnabled) {
    return {
      modality: "chat",
      targetModel: selectedModel,
      switched: false,
      isAutoImage: false,
      needsContextExtraction: false,
      cleanedPrompt: promptText,
    };
  }

  // 3. 文本中显式点名了某个可用模型（如 "用 gpt-image-2.6 出图" / "用 research 调研" / "用 tts-1 读出来"）
  const allKnownModels = [
    ...imgPool,
    ...ttsPool,
    ...availableChatModels,
  ];
  const mentionedModel = findMentionedModel(promptText, allKnownModels);
  if (mentionedModel) {
    if (isImageModelId(mentionedModel, imgPool)) {
      return {
        modality: "image",
        targetModel: mentionedModel,
        switched: mentionedModel.toLowerCase() !== selectedModel.toLowerCase(),
        isAutoImage: false,
        reason: "对话指定图像模型",
        needsContextExtraction: needsContextExtraction(promptText, history),
        cleanedPrompt: stripLeadingImageCommandWords(promptText),
      };
    }
    if (isTtsModelId(mentionedModel, ttsPool)) {
      return {
        modality: "audio",
        targetModel: mentionedModel,
        switched: mentionedModel.toLowerCase() !== selectedModel.toLowerCase(),
        isAutoImage: false,
        reason: "对话指定语音模型",
        needsContextExtraction: needsContextExtraction(promptText, history),
        cleanedPrompt: stripLeadingAudioCommandWords(promptText),
      };
    }
    return {
      modality: "chat",
      targetModel: mentionedModel,
      switched: mentionedModel.toLowerCase() !== selectedModel.toLowerCase(),
      isAutoImage: false,
      reason: "对话指定模型",
      needsContextExtraction: false,
      cleanedPrompt: promptText,
    };
  }

  // 4. 自然语言生图意图 -> 自动切换图像模型
  if (detectNaturalImageIntent(promptText)) {
    const chosen = imgPool[0] || DEFAULT_IMAGE_MODELS[0];
    return {
      modality: "image",
      targetModel: chosen,
      switched: chosen.toLowerCase() !== selectedModel.toLowerCase(),
      isAutoImage: false,
      reason: "上下文生图意图",
      needsContextExtraction: needsContextExtraction(promptText, history),
      cleanedPrompt: stripLeadingImageCommandWords(promptText),
    };
  }

  // 5. 自然语言语音合成意图 -> 自动切换语音模型
  if (detectNaturalAudioIntent(promptText)) {
    const chosen = ttsPool[0] || DEFAULT_TTS_MODELS[0];
    return {
      modality: "audio",
      targetModel: chosen,
      switched: chosen.toLowerCase() !== selectedModel.toLowerCase(),
      isAutoImage: false,
      reason: "语音合成意图",
      needsContextExtraction: needsContextExtraction(promptText, history),
      cleanedPrompt: stripLeadingAudioCommandWords(promptText),
    };
  }

  // 6. 深度研究意图 -> 自动切换 research 模型（若可用）
  const researchModel = availableChatModels.find(
    (m) => m.toLowerCase() === "research"
  );
  if (
    researchModel &&
    selectedModel.toLowerCase() !== "research" &&
    detectResearchIntent(promptText)
  ) {
    return {
      modality: "chat",
      targetModel: researchModel,
      switched: true,
      isAutoImage: false,
      reason: "深度研究意图",
      needsContextExtraction: false,
      cleanedPrompt: promptText,
    };
  }

  // 7. 默认保留当前对话模型
  return {
    modality: "chat",
    targetModel: selectedModel,
    switched: false,
    isAutoImage: false,
    needsContextExtraction: false,
    cleanedPrompt: promptText,
  };
}

export interface SemanticRouteLLMResult {
  modality: "chat" | "image" | "audio" | "research";
  targetModel?: string;
  needsContextExtraction?: boolean;
  imagePrompt?: string;
  reason?: string;
}

export type SemanticRouteClassifier = (args: {
  promptText: string;
  history: RouterHistoryMessage[];
  chatModels: string[];
  imageModels: string[];
  audioModels: string[];
}) => Promise<SemanticRouteLLMResult | null>;

/**
 * 优先走确定性快捷规则（斜杠指令、手动指定模型、关闭自动切模），
 * 其余自然语言输入交由 LLM 语义路由器理解意图，失败时安全回退至规则兜底。
 */
export async function resolveSemanticModelRoute(
  params: ResolveModelRouteParams & {
    classifyWithLLM?: SemanticRouteClassifier;
  }
): Promise<ModelRouteDecision> {
  const {
    promptText,
    selectedModel,
    hasExplicitModelOverride,
    autoSwitchEnabled,
    history,
    availableChatModels,
    availableImageModels,
    availableAudioModels,
    classifyWithLLM,
  } = params;

  const imgPool =
    availableImageModels.length > 0 ? availableImageModels : DEFAULT_IMAGE_MODELS;
  const ttsPool =
    availableAudioModels.length > 0 ? availableAudioModels : DEFAULT_TTS_MODELS;

  const trimmed = promptText.trim();

  // 1. 显式斜杠指令或关闭自动切模、或手动覆盖/当前已选专用模型 -> 走确定性路由
  if (
    trimmed.startsWith("/") ||
    !autoSwitchEnabled ||
    hasExplicitModelOverride ||
    isImageModelId(selectedModel, imgPool) ||
    isTtsModelId(selectedModel, ttsPool)
  ) {
    return resolveContextualModelRoute(params);
  }

  // 2. 自然语言语义路由（LLM 分类 + 上下文消歧）
  if (classifyWithLLM) {
    try {
      const llmDecision = await classifyWithLLM({
        promptText,
        history,
        chatModels: availableChatModels,
        imageModels: imgPool,
        audioModels: ttsPool,
      });

      if (llmDecision && llmDecision.modality) {
        const pickedTarget = (llmDecision.targetModel || "").trim();

        if (llmDecision.modality === "image") {
          const matchedImg =
            imgPool.find(
              (m) => m.toLowerCase() === pickedTarget.toLowerCase()
            ) ||
            imgPool[0] ||
            DEFAULT_IMAGE_MODELS[0];
          const extracted = (llmDecision.imagePrompt || "").trim();
          return {
            modality: "image",
            targetModel: matchedImg,
            switched: matchedImg.toLowerCase() !== selectedModel.toLowerCase(),
            isAutoImage: false,
            reason: llmDecision.reason || "自然语言生图意图",
            needsContextExtraction:
              !extracted &&
              (Boolean(llmDecision.needsContextExtraction) ||
                needsContextExtraction(promptText, history)),
            cleanedPrompt: stripLeadingImageCommandWords(promptText),
            extractedPrompt: extracted.length >= 8 ? extracted : undefined,
          };
        }

        if (llmDecision.modality === "audio") {
          const matchedTts =
            ttsPool.find(
              (m) => m.toLowerCase() === pickedTarget.toLowerCase()
            ) ||
            ttsPool[0] ||
            DEFAULT_TTS_MODELS[0];
          return {
            modality: "audio",
            targetModel: matchedTts,
            switched: matchedTts.toLowerCase() !== selectedModel.toLowerCase(),
            isAutoImage: false,
            reason: llmDecision.reason || "自然语言语音意图",
            needsContextExtraction:
              Boolean(llmDecision.needsContextExtraction) ||
              needsContextExtraction(promptText, history),
            cleanedPrompt: stripLeadingAudioCommandWords(promptText),
          };
        }

        if (llmDecision.modality === "research") {
          const researchModel = availableChatModels.find(
            (m) => m.toLowerCase() === "research"
          );
          if (researchModel && selectedModel.toLowerCase() !== "research") {
            return {
              modality: "chat",
              targetModel: researchModel,
              switched: true,
              isAutoImage: false,
              reason: llmDecision.reason || "深度研究意图",
              needsContextExtraction: false,
              cleanedPrompt: promptText,
            };
          }
        }

        // modality === "chat"
        const matchedChat = availableChatModels.find(
          (m) => m.toLowerCase() === pickedTarget.toLowerCase()
        );
        const finalChat = matchedChat || selectedModel;
        return {
          modality: "chat",
          targetModel: finalChat,
          switched: finalChat.toLowerCase() !== selectedModel.toLowerCase(),
          isAutoImage: false,
          reason: llmDecision.reason || undefined,
          needsContextExtraction: false,
          cleanedPrompt: promptText,
        };
      }
    } catch {
      // 语义路由失败或超时，静默回退至规则兜底
    }
  }

  return resolveContextualModelRoute(params);
}
