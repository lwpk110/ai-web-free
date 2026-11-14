import { useCallback, useEffect, useState } from "react";
import {
  fetchAdminModelsCatalog,
  type ModelsCatalogItem,
} from "../api/client";

export type ModelCategory = "chat" | "image" | "audio";

export interface CatalogModel {
  id: string;
  category: ModelCategory;
  source: "upstream" | "builtin";
  subtitle: string;
  tag: string;
}

const BUILTIN_MODEL_META: Record<
  string,
  { category: ModelCategory; subtitle: string; tag: string }
> = {
  "gpt-image-2.5": {
    category: "image",
    subtitle: "标准平衡文生图 / 参考编辑",
    tag: "标准",
  },
  "gpt-image-2.5-sunburst": {
    category: "image",
    subtitle: "高细节建筑、光影与摄影精绘",
    tag: "精绘",
  },
  "gpt-image-2.5-flare": {
    category: "image",
    subtitle: "极速低延迟出图，适合快速草稿",
    tag: "极速",
  },
  "gpt-image-2.6": {
    category: "image",
    subtitle: "最新预览图像档位",
    tag: "预览",
  },
  "gpt-image-2": {
    category: "image",
    subtitle: "GPT-Image v2 兼容映射",
    tag: "兼容",
  },
  "gpt-image-1": {
    category: "image",
    subtitle: "OpenAI 标准文生图兼容别名",
    tag: "兼容",
  },
  "dall-e-3": {
    category: "image",
    subtitle: "OpenAI 标准兼容自动映射",
    tag: "兼容",
  },
  "gpt-4o-mini-tts": {
    category: "audio",
    subtitle: "多音色高质量语音合成",
    tag: "语音",
  },
  "tts-1": {
    category: "audio",
    subtitle: "OpenAI TTS-1 语音合成别名",
    tag: "语音",
  },
  "tts-1-hd": {
    category: "audio",
    subtitle: "高精度 TTS-1-HD 合成别名",
    tag: "语音",
  },
  "gpt-4o-transcribe": {
    category: "audio",
    subtitle: "流式多语种语音识别",
    tag: "语音",
  },
  "whisper-1": {
    category: "audio",
    subtitle: "Whisper STT 语音转文字 / 翻译",
    tag: "语音",
  },
  "gpt-5-6": {
    category: "chat",
    subtitle: "默认旗舰对话与 Tool Calling",
    tag: "旗舰",
  },
  "gpt-5-3": {
    category: "chat",
    subtitle: "默认平衡对话模型",
    tag: "对话",
  },
  "gpt-5-3-instant": {
    category: "chat",
    subtitle: "低延迟即时对话",
    tag: "极速",
  },
  "gpt-5-6-mini": {
    category: "chat",
    subtitle: "轻量快速响应模型",
    tag: "轻量",
  },
  "gpt-5-mini": {
    category: "chat",
    subtitle: "经济轻量对话模型",
    tag: "轻量",
  },
  "gpt-6": {
    category: "chat",
    subtitle: "长程推理对话模型",
    tag: "推理",
  },
  "gpt-4o": {
    category: "chat",
    subtitle: "全能多模态对话兼容映射",
    tag: "对话",
  },
  "gpt-4o-mini": {
    category: "chat",
    subtitle: "高速兼容轻量映射",
    tag: "轻量",
  },
  research: {
    category: "chat",
    subtitle: "深度研究与综合分析",
    tag: "推理",
  },
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

export function classifyModel(
  id: string,
  explicitSource?: "upstream" | "builtin"
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

  const isBuiltin =
    explicitSource === "builtin"
      ? true
      : explicitSource === "upstream"
        ? false
        : BUILTIN_IDS.has(lower);
  let subtitle = known?.subtitle;
  let tag = known?.tag;
  if (!subtitle) {
    if (
      lower.includes("thinking") ||
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
      subtitle = "ChatGPT 上游实时对话模型";
      tag = tag || (lower.includes("mini") || lower.includes("nano") ? "轻量" : "对话");
    }
  }

  return {
    id,
    category,
    source: isBuiltin ? "builtin" : "upstream",
    subtitle,
    tag: tag || "对话",
  };
}

export interface ModelsCatalogState {
  models: string[];
  catalog: CatalogModel[];
  chatModels: CatalogModel[];
  imageModels: CatalogModel[];
  audioModels: CatalogModel[];
  source: "live" | "cache" | "fallback";
  cachedAt: string;
  warning: string | null;
  loading: boolean;
  error: string | null;
}

const STORAGE_KEY = "ai_web_free_models_catalog_v1";
const CACHE_TTL_MS = 15 * 60 * 1000;
const MODELS_UPDATED_EVENT = "models-catalog-updated";

function buildCatalogState(
  items: ModelsCatalogItem[],
  source: "live" | "cache" | "fallback",
  cachedAt = "",
  warning: string | null = null,
  loading = false,
  error: string | null = null
): ModelsCatalogState {
  const seen = new Set<string>();
  const catalog: CatalogModel[] = [];
  for (const item of items) {
    const id = item.id?.trim();
    if (!id || seen.has(id)) continue;
    seen.add(id);
    const explicit =
      item.source === "upstream" || item.source === "builtin"
        ? item.source
        : undefined;
    catalog.push(classifyModel(id, explicit));
  }
  return {
    models: catalog.map((m) => m.id),
    catalog,
    chatModels: catalog.filter((m) => m.category === "chat"),
    imageModels: catalog.filter((m) => m.category === "image"),
    audioModels: catalog.filter((m) => m.category === "audio"),
    source,
    cachedAt,
    warning,
    loading,
    error,
  };
}

interface PersistedCatalog {
  items: ModelsCatalogItem[];
  source: "live" | "cache";
  cachedAt: string;
  savedAt: number;
  warning?: string | null;
}

function loadPersistedCatalog(): ModelsCatalogState | null {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as PersistedCatalog;
    if (
      !parsed ||
      !Array.isArray(parsed.items) ||
      parsed.items.length === 0 ||
      typeof parsed.savedAt !== "number" ||
      Date.now() - parsed.savedAt > CACHE_TTL_MS
    ) {
      return null;
    }
    return buildCatalogState(
      parsed.items,
      "cache",
      parsed.cachedAt || new Date(parsed.savedAt).toISOString(),
      parsed.warning || null
    );
  } catch {
    return null;
  }
}

function savePersistedCatalog(
  items: ModelsCatalogItem[],
  source: "live" | "cache",
  cachedAt: string,
  warning?: string | null
) {
  try {
    const payload: PersistedCatalog = {
      items,
      source,
      cachedAt,
      savedAt: Date.now(),
      warning: warning || null,
    };
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(payload));
  } catch {
    // ignore storage quota errors
  }
}

function makeFallbackState(): ModelsCatalogState {
  return buildCatalogState(
    FALLBACK_CATALOG_IDS.map((id) => ({ id, source: "builtin" })),
    "fallback"
  );
}

let currentState: ModelsCatalogState =
  (typeof window !== "undefined" ? loadPersistedCatalog() : null) ||
  makeFallbackState();
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
    (currentState.source === "live" || currentState.source === "cache")
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
      const res = await fetchAdminModelsCatalog({
        refresh: forceRefresh,
        apiKey: apiKey || undefined,
      });
      hasFetchedThisSession = true;

      if (res.data.length > 0) {
        const nextSource: "live" | "cache" | "fallback" =
          res.source === "cache"
            ? "cache"
            : res.source === "fallback"
              ? "fallback"
              : "live";
        const cachedAt = res.cached_at || new Date().toISOString();
        currentState = buildCatalogState(
          res.data,
          nextSource,
          cachedAt,
          res.warning || null,
          false,
          null
        );
        if (nextSource === "live" || nextSource === "cache") {
          savePersistedCatalog(res.data, nextSource, cachedAt, res.warning);
        }
      } else {
        currentState = {
          ...currentState,
          loading: false,
          warning: res.warning || "接口返回空模型列表，已保留当前参考目录",
        };
      }
    } catch (err: any) {
      currentState = {
        ...currentState,
        loading: false,
        error: err?.message || "获取模型目录失败",
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
  const seen = new Set<string>();
  const items: ModelsCatalogItem[] = [];
  for (const raw of upstreamSlugs) {
    const id = raw.trim();
    if (!id || seen.has(id)) continue;
    seen.add(id);
    items.push({ id, source: "upstream" });
  }
  if (items.length === 0) return;

  for (const id of BUILTIN_IDS) {
    if (!seen.has(id)) {
      seen.add(id);
      items.push({ id, source: "builtin" });
    }
  }
  const now = new Date().toISOString();
  hasFetchedThisSession = true;
  currentState = buildCatalogState(items, "live", now, null, false, null);
  savePersistedCatalog(items, "live", now, null);
  notifyCatalogUpdated();
}

export function useModelsCatalog(options?: { autoLoad?: boolean }) {
  const [state, setState] = useState<ModelsCatalogState>(() => currentState);
  const autoLoad = options?.autoLoad !== false;

  useEffect(() => {
    const sync = () => setState(currentState);
    const onPoolUpdated = () => {
      if (currentState.source === "fallback") {
        void ensureModelsCatalog({ forceRefresh: true });
      }
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

  return {
    ...state,
    refresh,
  };
}
