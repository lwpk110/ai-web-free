import { useCallback, useEffect, useState } from "react";
import {
  fetchAdminModelsCatalog,
  fetchProviderDescriptors,
  type ModelsCatalogItem,
} from "../api/client";

export type ModelCategory = "chat" | "image" | "audio";

export interface CatalogModel {
  id: string;
  category: ModelCategory;
  source: "upstream" | "builtin" | "custom";
  provider?: string;
  subtitle: string;
  tag: string;
}

export interface ProviderModelCatalog {
  providerId: string;
  providerName: string;
  status?: string;
  models: string[];
  chatModels: string[];
  imageModels: string[];
  audioModels: string[];
  updatedAt?: string;
}

export interface ModelsCatalogState {
  models: string[];
  catalog: CatalogModel[];
  chatModels: CatalogModel[];
  imageModels: CatalogModel[];
  audioModels: CatalogModel[];
  providers: Record<string, ProviderModelCatalog>;
  customModels: string[];
  source: "live" | "cache" | "fallback" | "static";
  cachedAt: string;
  warning: string | null;
  loading: boolean;
  error: string | null;
}

const BUILTIN_MODEL_META: Record<
  string,
  { category: ModelCategory; subtitle: string; tag: string; provider?: string }
> = {
  // --- 图像模型 ---
  "gpt-image-2.5": {
    category: "image",
    subtitle: "标准平衡文生图 / 参考编辑",
    tag: "标准",
    provider: "chatgpt",
  },
  "gpt-image-2.5-sunburst": {
    category: "image",
    subtitle: "高细节建筑、光影与摄影精绘",
    tag: "精绘",
    provider: "chatgpt",
  },
  "gpt-image-2.5-flare": {
    category: "image",
    subtitle: "极速低延迟出图，适合快速草稿",
    tag: "极速",
    provider: "chatgpt",
  },
  "gpt-image-2.6": {
    category: "image",
    subtitle: "最新预览图像档位",
    tag: "预览",
    provider: "chatgpt",
  },
  "gpt-image-2": {
    category: "image",
    subtitle: "GPT-Image v2 兼容映射",
    tag: "兼容",
    provider: "chatgpt",
  },
  "gpt-image-1": {
    category: "image",
    subtitle: "OpenAI 标准文生图兼容别名",
    tag: "兼容",
    provider: "chatgpt",
  },
  "dall-e-3": {
    category: "image",
    subtitle: "OpenAI 标准兼容自动映射",
    tag: "兼容",
    provider: "chatgpt",
  },
  "grok-2-image": {
    category: "image",
    subtitle: "xAI Aurora 原生文生图",
    tag: "生图",
    provider: "grok",
  },

  // --- 语音模型 ---
  "gpt-4o-mini-tts": {
    category: "audio",
    subtitle: "多音色高质量语音合成",
    tag: "语音",
    provider: "chatgpt",
  },
  "tts-1": {
    category: "audio",
    subtitle: "OpenAI TTS-1 语音合成别名",
    tag: "语音",
    provider: "chatgpt",
  },
  "tts-1-hd": {
    category: "audio",
    subtitle: "高精度 TTS-1-HD 合成别名",
    tag: "语音",
    provider: "chatgpt",
  },
  "gpt-4o-transcribe": {
    category: "audio",
    subtitle: "流式多语种语音识别",
    tag: "语音",
    provider: "chatgpt",
  },
  "whisper-1": {
    category: "audio",
    subtitle: "Whisper STT 语音转文字 / 翻译",
    tag: "语音",
    provider: "chatgpt",
  },

  // --- 对话模型 (ChatGPT) ---
  "gpt-5-6": {
    category: "chat",
    subtitle: "默认旗舰对话与 Tool Calling",
    tag: "旗舰",
    provider: "chatgpt",
  },
  "gpt-5-3": {
    category: "chat",
    subtitle: "默认平衡对话模型",
    tag: "对话",
    provider: "chatgpt",
  },
  "gpt-5-3-instant": {
    category: "chat",
    subtitle: "低延迟即时对话",
    tag: "极速",
    provider: "chatgpt",
  },
  "gpt-5-6-mini": {
    category: "chat",
    subtitle: "轻量快速响应模型",
    tag: "轻量",
    provider: "chatgpt",
  },
  "gpt-5-mini": {
    category: "chat",
    subtitle: "经济轻量对话模型",
    tag: "轻量",
    provider: "chatgpt",
  },
  "gpt-6": {
    category: "chat",
    subtitle: "长程推理对话模型",
    tag: "推理",
    provider: "chatgpt",
  },
  "gpt-4o": {
    category: "chat",
    subtitle: "全能多模态对话兼容映射",
    tag: "对话",
    provider: "chatgpt",
  },
  "gpt-4o-mini": {
    category: "chat",
    subtitle: "高速兼容轻量映射",
    tag: "轻量",
    provider: "chatgpt",
  },
  research: {
    category: "chat",
    subtitle: "深度研究与综合分析",
    tag: "推理",
    provider: "chatgpt",
  },
  o1: {
    category: "chat",
    subtitle: "深度思考与复杂逻辑推理",
    tag: "推理",
    provider: "chatgpt",
  },
  "o3-mini": {
    category: "chat",
    subtitle: "极速高性价比推理模型",
    tag: "推理",
    provider: "chatgpt",
  },
  auto: {
    category: "chat",
    subtitle: "ChatGPT 上游自适应路由",
    tag: "自适应",
    provider: "chatgpt",
  },

  // --- 对话模型 (Grok) ---
  "grok-3": {
    category: "chat",
    subtitle: "xAI 旗舰长文本与高智能推理模型",
    tag: "旗舰",
    provider: "grok",
  },
  "grok-3-mini": {
    category: "chat",
    subtitle: "xAI 轻量快速思考对话模型",
    tag: "轻量",
    provider: "grok",
  },
  "grok-2": {
    category: "chat",
    subtitle: "xAI 均衡全能对话模型",
    tag: "对话",
    provider: "grok",
  },
  "grok-beta": {
    category: "chat",
    subtitle: "xAI 早期测试版模型",
    tag: "测试",
    provider: "grok",
  },

  // --- 常见三方扩展模型别名 ---
  "deepseek-chat": {
    category: "chat",
    subtitle: "DeepSeek-V3 高通用性对话",
    tag: "对话",
  },
  "deepseek-reasoner": {
    category: "chat",
    subtitle: "DeepSeek-R1 深度长链推理",
    tag: "推理",
  },
  "claude-3-7-sonnet": {
    category: "chat",
    subtitle: "Anthropic 混合推理旗舰",
    tag: "旗舰",
  },
  "claude-3-5-sonnet": {
    category: "chat",
    subtitle: "Anthropic 高性能编码与综合模型",
    tag: "旗舰",
  },
  "gemini-2.5-flash": {
    category: "chat",
    subtitle: "Google 极速低延迟多模态对话",
    tag: "极速",
  },
  "gemini-2.5-pro": {
    category: "chat",
    subtitle: "Google 旗舰深度长文本与推理",
    tag: "旗舰",
  },
};

// 供应商静态内置基线（模型极少变更，作为初次冷启动或离线底座）
const STATIC_PROVIDER_BASELINES: Record<
  string,
  { name: string; models: string[] }
> = {
  chatgpt: {
    name: "ChatGPT (OpenAI 网页逆向)",
    models: [
      "gpt-5-6",
      "gpt-5-3",
      "gpt-5-3-instant",
      "gpt-5-mini",
      "gpt-4o",
      "gpt-4o-mini",
      "o3-mini",
      "o1",
      "gpt-6",
      "research",
      "auto",
      "gpt-image-2.5-sunburst",
      "gpt-image-2.5-flare",
      "gpt-image-2.5",
      "gpt-image-2.6",
      "gpt-image-1",
      "dall-e-3",
      "gpt-4o-mini-tts",
      "tts-1",
      "tts-1-hd",
      "whisper-1",
    ],
  },
  grok: {
    name: "Grok (xAI Web 逆向)",
    models: ["grok-3", "grok-3-mini", "grok-2", "grok-2-image", "grok-beta"],
  },
  local_tts: {
    name: "Local TTS (本地语音引擎)",
    models: ["tts-1", "tts-1-hd"],
  },
};

export const DEFAULT_CHAT_MODELS: string[] = [
  "gpt-5-6",
  "gpt-5-3",
  "gpt-5-mini",
  "gpt-4o",
  "gpt-4o-mini",
  "o3-mini",
  "o1",
  "grok-3",
  "grok-3-mini",
  "gpt-6",
  "research",
  "auto",
];

export const FALLBACK_CHAT_MODELS: string[] = DEFAULT_CHAT_MODELS;

export const BUILTIN_IDS = new Set<string>([]);

export const FALLBACK_CATALOG_IDS: string[] = [
  ...DEFAULT_CHAT_MODELS,
  "gpt-image-2.5-sunburst",
  "gpt-image-2.5-flare",
  "gpt-image-2.5",
  "gpt-image-2.6",
  "gpt-image-1",
  "dall-e-3",
  "grok-2-image",
  "gpt-4o-mini-tts",
  "tts-1",
  "tts-1-hd",
  "gpt-4o-transcribe",
  "whisper-1",
];

export function guessProviderForModel(id: string): string {
  const lower = id.toLowerCase().trim();
  if (lower.startsWith("grok")) return "grok";
  if (lower.startsWith("wan-video") || lower.includes("video")) return "video";
  return "chatgpt";
}

export function classifyModel(
  id: string,
  explicitSource?: "upstream" | "builtin" | "custom",
  provider?: string
): CatalogModel {
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

  const resolvedSource =
    explicitSource || (BUILTIN_IDS.has(lower) ? "builtin" : "upstream");
  const resolvedProvider = provider || known?.provider || guessProviderForModel(id);

  let subtitle = known?.subtitle;
  let tag = known?.tag;
  if (!subtitle) {
    if (resolvedSource === "custom") {
      subtitle = "用户自定义模型";
      tag = tag || "自定义";
    } else if (
      lower.includes("thinking") ||
      lower.includes("reasoner") ||
      lower.includes("o1") ||
      lower.includes("o3") ||
      lower.includes("o4")
    ) {
      subtitle = "深度思考推理模型";
      tag = tag || "推理";
    } else if (lower.includes("research")) {
      subtitle = "深度研究与检索分析";
      tag = tag || "推理";
    } else if (category === "image") {
      subtitle = "图像生成与编辑模型";
      tag = tag || "图像";
    } else if (category === "audio") {
      subtitle = "语音合成 / 转录模型";
      tag = tag || "语音";
    } else {
      subtitle = `${resolvedProvider.toUpperCase()} 上游对话模型`;
      tag = tag || (lower.includes("mini") || lower.includes("nano") ? "轻量" : "对话");
    }
  }

  return {
    id,
    category,
    source: resolvedSource,
    provider: resolvedProvider,
    subtitle,
    tag: tag || "对话",
  };
}

const GLOBAL_CACHE_KEY = "ai_web_free_global_model_cache_v2";
const CUSTOM_MODELS_STORAGE_KEY = "ai_web_free_custom_models";
const CACHE_TTL_MS = 24 * 60 * 60 * 1000; // 模型数据极少改动，采用 24 小时静态长缓存
const MODELS_UPDATED_EVENT = "models-catalog-updated";

function cloneBaselineProviders(): Record<string, ProviderModelCatalog> {
  const result: Record<string, ProviderModelCatalog> = {};
  const now = new Date().toISOString();
  for (const [pid, def] of Object.entries(STATIC_PROVIDER_BASELINES)) {
    const classified = def.models.map((id) => classifyModel(id, "builtin", pid));
    result[pid] = {
      providerId: pid,
      providerName: def.name,
      status: "enabled",
      models: [...def.models],
      chatModels: classified.filter((m) => m.category === "chat").map((m) => m.id),
      imageModels: classified.filter((m) => m.category === "image").map((m) => m.id),
      audioModels: classified.filter((m) => m.category === "audio").map((m) => m.id),
      updatedAt: now,
    };
  }
  return result;
}

function loadPersistedCustomModels(): string[] {
  try {
    const raw = window.localStorage.getItem(CUSTOM_MODELS_STORAGE_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed)
      ? parsed.filter((id) => typeof id === "string" && id.trim())
      : [];
  } catch {
    return [];
  }
}

function savePersistedCustomModels(models: string[]) {
  try {
    window.localStorage.setItem(CUSTOM_MODELS_STORAGE_KEY, JSON.stringify(models));
  } catch {}
}

function reassembleCatalogState(
  providers: Record<string, ProviderModelCatalog>,
  customModels: string[],
  source: "live" | "cache" | "fallback" | "static",
  cachedAt = "",
  warning: string | null = null,
  loading = false,
  error: string | null = null
): ModelsCatalogState {
  const seen = new Set<string>();
  const catalog: CatalogModel[] = [];

  // 1. 各供应商已缓存模型
  for (const pid of Object.keys(providers)) {
    const p = providers[pid];
    for (const id of p.models) {
      const clean = id.trim();
      if (!clean || seen.has(clean)) continue;
      seen.add(clean);
      catalog.push(classifyModel(clean, "upstream", pid));
    }
  }

  // 2. 自定义模型
  for (const id of customModels) {
    const clean = id.trim();
    if (!clean || seen.has(clean)) continue;
    seen.add(clean);
    catalog.push(classifyModel(clean, "custom", "custom"));
  }

  return {
    models: catalog.map((m) => m.id),
    catalog,
    chatModels: catalog.filter((m) => m.category === "chat"),
    imageModels: catalog.filter((m) => m.category === "image"),
    audioModels: catalog.filter((m) => m.category === "audio"),
    providers,
    customModels,
    source,
    cachedAt: cachedAt || new Date().toISOString(),
    warning,
    loading,
    error,
  };
}

interface PersistedGlobalPayload {
  version: 2;
  savedAt: number;
  source: "live" | "cache";
  cachedAt: string;
  warning?: string | null;
  providers: Record<string, ProviderModelCatalog>;
  customModels: string[];
}

function loadPersistedGlobalCache(): ModelsCatalogState | null {
  try {
    const raw = window.localStorage.getItem(GLOBAL_CACHE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as PersistedGlobalPayload;
    if (
      !parsed ||
      parsed.version !== 2 ||
      typeof parsed.savedAt !== "number" ||
      Date.now() - parsed.savedAt > CACHE_TTL_MS ||
      !parsed.providers
    ) {
      return null;
    }
    const customModels = Array.isArray(parsed.customModels)
      ? parsed.customModels
      : loadPersistedCustomModels();
    return reassembleCatalogState(
      parsed.providers,
      customModels,
      "cache",
      parsed.cachedAt || new Date(parsed.savedAt).toISOString(),
      parsed.warning || null
    );
  } catch {
    return null;
  }
}

function savePersistedGlobalCache(state: ModelsCatalogState) {
  try {
    const payload: PersistedGlobalPayload = {
      version: 2,
      savedAt: Date.now(),
      source: state.source === "live" ? "live" : "cache",
      cachedAt: state.cachedAt,
      warning: state.warning,
      providers: state.providers,
      customModels: state.customModels,
    };
    window.localStorage.setItem(GLOBAL_CACHE_KEY, JSON.stringify(payload));
  } catch {}
}

function makeStaticBaselineState(): ModelsCatalogState {
  const providers = cloneBaselineProviders();
  const customModels =
    typeof window !== "undefined" ? loadPersistedCustomModels() : [];
  return reassembleCatalogState(
    providers,
    customModels,
    "static",
    new Date().toISOString(),
    null
  );
}

// 静态单例缓存实例
let currentState: ModelsCatalogState =
  (typeof window !== "undefined" ? loadPersistedGlobalCache() : null) ||
  makeStaticBaselineState();

let hasFetchedThisSession = false;
let inFlightPromise: Promise<ModelsCatalogState> | null = null;

function notifyCatalogUpdated() {
  if (typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent(MODELS_UPDATED_EVENT));
  }
}

export function getModelsCatalogState(): ModelsCatalogState {
  return currentState;
}

export function getGlobalChatModels(providerId?: string): CatalogModel[] {
  if (!providerId) return currentState.chatModels;
  return currentState.chatModels.filter(
    (m) => !m.provider || m.provider === providerId
  );
}

export function getGlobalImageModels(providerId?: string): CatalogModel[] {
  if (!providerId) return currentState.imageModels;
  return currentState.imageModels.filter(
    (m) => !m.provider || m.provider === providerId
  );
}

export function getGlobalAudioModels(providerId?: string): CatalogModel[] {
  if (!providerId) return currentState.audioModels;
  return currentState.audioModels.filter(
    (m) => !m.provider || m.provider === providerId
  );
}

export function getGlobalProviders(): Record<string, ProviderModelCatalog> {
  return currentState.providers;
}

export function addGlobalCustomModel(modelId: string): void {
  const trimmed = modelId.trim();
  if (!trimmed) return;
  const current = currentState.customModels || [];
  if (current.includes(trimmed)) return;
  const nextCustom = [trimmed, ...current];
  savePersistedCustomModels(nextCustom);
  currentState = reassembleCatalogState(
    currentState.providers,
    nextCustom,
    currentState.source,
    currentState.cachedAt,
    currentState.warning
  );
  savePersistedGlobalCache(currentState);
  notifyCatalogUpdated();
}

export function removeGlobalCustomModel(modelId: string): void {
  const trimmed = modelId.trim();
  if (!trimmed) return;
  const current = currentState.customModels || [];
  const nextCustom = current.filter((m) => m !== trimmed);
  savePersistedCustomModels(nextCustom);
  currentState = reassembleCatalogState(
    currentState.providers,
    nextCustom,
    currentState.source,
    currentState.cachedAt,
    currentState.warning
  );
  savePersistedGlobalCache(currentState);
  notifyCatalogUpdated();
}

/**
 * 确保全局静态模型缓存可用。
 * 模型极少变更，未要求 forceRefresh 时优先复用本地持久化缓存与单例。
 * 若需刷新，则并行拉取各供应商声明模型与上游探测模型，按供应商组织并持久化。
 */
export async function ensureModelsCatalog(options?: {
  forceRefresh?: boolean;
  apiKey?: string;
}): Promise<ModelsCatalogState> {
  const forceRefresh = Boolean(options?.forceRefresh);
  const apiKey = options?.apiKey?.trim() || "";

  if (
    !forceRefresh &&
    !apiKey &&
    hasFetchedThisSession &&
    (currentState.source === "live" ||
      currentState.source === "cache" ||
      currentState.source === "static")
  ) {
    return currentState;
  }

  if (inFlightPromise && !forceRefresh && !apiKey) {
    return inFlightPromise;
  }

  const run = (async () => {
    currentState = { ...currentState, loading: true, error: null };
    notifyCatalogUpdated();

    try {
      // 并行拉取供应商列表与模型目录
      const [providersRes, modelsRes] = await Promise.allSettled([
        fetchProviderDescriptors(),
        fetchAdminModelsCatalog({
          refresh: forceRefresh,
          apiKey: apiKey || undefined,
        }),
      ]);

      const providersMap: Record<string, ProviderModelCatalog> = {
        ...cloneBaselineProviders(),
        ...currentState.providers,
      };

      // 1. 处理各供应商返回的特征模型与生图模型
      if (providersRes.status === "fulfilled" && Array.isArray(providersRes.value)) {
        for (const p of providersRes.value) {
          if (!p || !p.id) continue;
          const pid = p.id.toLowerCase();
          const pModels = Array.from(
            new Set([...(p.featured_models || []), ...(p.image_models || [])])
          );
          const existing = providersMap[pid] || {
            providerId: pid,
            providerName: p.name || pid,
            models: [],
            chatModels: [],
            imageModels: [],
            audioModels: [],
            updatedAt: new Date().toISOString(),
          };
          const merged = Array.from(new Set([...pModels, ...existing.models]));
          const classified = merged.map((id) => classifyModel(id, "upstream", pid));
          providersMap[pid] = {
            providerId: pid,
            providerName: p.name || existing.providerName,
            status: p.status,
            models: merged,
            chatModels: classified.filter((m) => m.category === "chat").map((m) => m.id),
            imageModels: classified.filter((m) => m.category === "image").map((m) => m.id),
            audioModels: classified.filter((m) => m.category === "audio").map((m) => m.id),
            updatedAt: new Date().toISOString(),
          };
        }
      }

      // 2. 处理上游实时探测的活跃模型目录
      let upstreamWarning: string | null = null;
      let nextSource: "live" | "cache" | "static" = "live";
      let cachedAt = new Date().toISOString();

      if (modelsRes.status === "fulfilled") {
        const res = modelsRes.value;
        upstreamWarning = res.warning || null;
        if (res.source === "cache") nextSource = "cache";
        if (res.cached_at) cachedAt = res.cached_at;

        if (Array.isArray(res.data) && res.data.length > 0) {
          for (const item of res.data) {
            const id = item.id?.trim();
            if (!id) continue;
            const pid = guessProviderForModel(id);
            const targetP = providersMap[pid] || {
              providerId: pid,
              providerName: pid.toUpperCase(),
              models: [],
              chatModels: [],
              imageModels: [],
              audioModels: [],
              updatedAt: cachedAt,
            };
            if (!targetP.models.includes(id)) {
              targetP.models = [id, ...targetP.models];
              const classified = classifyModel(id, "upstream", pid);
              if (classified.category === "chat" && !targetP.chatModels.includes(id)) {
                targetP.chatModels = [id, ...targetP.chatModels];
              } else if (classified.category === "image" && !targetP.imageModels.includes(id)) {
                targetP.imageModels = [id, ...targetP.imageModels];
              } else if (classified.category === "audio" && !targetP.audioModels.includes(id)) {
                targetP.audioModels = [id, ...targetP.audioModels];
              }
            }
          }
        }
      }

      hasFetchedThisSession = true;
      currentState = reassembleCatalogState(
        providersMap,
        currentState.customModels,
        nextSource,
        cachedAt,
        upstreamWarning
      );
      savePersistedGlobalCache(currentState);
    } catch (err: any) {
      currentState = {
        ...currentState,
        loading: false,
        error: err?.message || "拉取供应商与模型目录失败",
      };
    } finally {
      inFlightPromise = null;
      notifyCatalogUpdated();
    }

    return currentState;
  })();

  if (!apiKey) {
    inFlightPromise = run;
  }
  return run;
}

export function seedModelsCatalogFromSlugs(upstreamSlugs: string[]): void {
  if (!Array.isArray(upstreamSlugs) || upstreamSlugs.length === 0) return;
  const current = currentState;
  const providers = { ...current.providers };
  const cg = providers["chatgpt"] || {
    providerId: "chatgpt",
    providerName: "ChatGPT (OpenAI 网页逆向)",
    models: [],
    chatModels: [],
    imageModels: [],
    audioModels: [],
    updatedAt: new Date().toISOString(),
  };
  const seen = new Set(cg.models);
  const nextModels = [...cg.models];
  for (const slug of upstreamSlugs) {
    const s = slug.trim();
    if (s && !seen.has(s)) {
      seen.add(s);
      nextModels.push(s);
    }
  }
  const classified = nextModels.map((id) => classifyModel(id, "upstream", "chatgpt"));
  providers["chatgpt"] = {
    ...cg,
    models: nextModels,
    chatModels: classified.filter((m) => m.category === "chat").map((m) => m.id),
    imageModels: classified.filter((m) => m.category === "image").map((m) => m.id),
    audioModels: classified.filter((m) => m.category === "audio").map((m) => m.id),
    updatedAt: new Date().toISOString(),
  };

  currentState = reassembleCatalogState(
    providers,
    current.customModels,
    "live",
    new Date().toISOString(),
    null
  );
  savePersistedGlobalCache(currentState);
  notifyCatalogUpdated();
}

/**
 * 全局模型目录 Hook。任何页面均可通过此 Hook 取得全局统一的静态/动态模型缓存，
 * 并根据自身业务需要（对话、生图、语音、特定供应商）重新组织。
 */
export function useModelsCatalog(options?: { autoLoad?: boolean }) {
  const [state, setState] = useState<ModelsCatalogState>(() => currentState);
  const autoLoad = options?.autoLoad !== false;

  useEffect(() => {
    const sync = () => setState(currentState);
    const onPoolUpdated = () => {
      void ensureModelsCatalog({ forceRefresh: true });
    };
    window.addEventListener(MODELS_UPDATED_EVENT, sync);
    window.addEventListener("pool-updated", onPoolUpdated);
    if (autoLoad) {
      void ensureModelsCatalog();
    }
    return () => {
      window.removeEventListener(MODELS_UPDATED_EVENT, sync);
      window.removeEventListener("pool-updated", onPoolUpdated);
    };
  }, [autoLoad]);

  const refresh = useCallback(
    (opts?: { forceRefresh?: boolean; apiKey?: string }) =>
      ensureModelsCatalog({ forceRefresh: true, ...opts }),
    []
  );

  const getModelsByProvider = useCallback(
    (providerId: string): CatalogModel[] => {
      return state.catalog.filter((m) => m.provider === providerId);
    },
    [state.catalog]
  );

  const getChatModels = useCallback(
    (providerId?: string): CatalogModel[] => {
      if (!providerId) return state.chatModels;
      return state.chatModels.filter(
        (m) => !m.provider || m.provider === providerId
      );
    },
    [state.chatModels]
  );

  const getImageModels = useCallback(
    (providerId?: string): CatalogModel[] => {
      if (!providerId) return state.imageModels;
      return state.imageModels.filter(
        (m) => !m.provider || m.provider === providerId
      );
    },
    [state.imageModels]
  );

  const getAudioModels = useCallback(
    (providerId?: string): CatalogModel[] => {
      if (!providerId) return state.audioModels;
      return state.audioModels.filter(
        (m) => !m.provider || m.provider === providerId
      );
    },
    [state.audioModels]
  );

  return {
    ...state,
    refresh,
    getModelsByProvider,
    getChatModels,
    getImageModels,
    getAudioModels,
    addCustomModel: addGlobalCustomModel,
    removeCustomModel: removeGlobalCustomModel,
  };
}

export const useGlobalModelCatalog = useModelsCatalog;
