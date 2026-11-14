import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  Check,
  Copy,
  Database,
  Dices,
  Download,
  Flame,
  Heart,
  Image as ImageIcon,
  Layers,
  Loader2,
  Maximize2,
  RefreshCw,
  Repeat2,
  Search,
  SlidersHorizontal,
  Sparkles,
  Star,
  ThumbsUp,
  Trash2,
  UploadCloud,
  Wand2,
  X,
} from "lucide-react";
import {
  editImageWithReferences,
  expandImagePrompt,
  fetchAccounts,
  fetchPromptPlaza,
  generateImage,
  triggerPromptPlazaSync,
  type PromptPlazaItem,
  type PromptSyncStatus,
  type PromptVariable,
} from "../api/client";
import { NoticeBanner } from "../components/ui";
import { useModelsCatalog } from "../lib/models";
import {
  base64ToBlob,
  clearStoredImages,
  dataUrlToBlob,
  deleteStoredImage,
  hydrateStoredImage,
  loadFavoritePromptIds,
  loadStoredImages,
  saveStoredImage,
  toggleFavoritePromptId,
  type HydratedImageItem,
  type StoredImageRecord,
} from "../lib/storage";

const MODEL_CARDS = [
  {
    id: "gpt-image-2.5-sunburst",
    desc: "高细节建筑、光影与摄影精绘",
    tag: "精绘",
  },
  {
    id: "gpt-image-2.5-flare",
    desc: "极速低延迟出图，适合快速草稿",
    tag: "极速",
  },
  {
    id: "gpt-image-2.5",
    desc: "标准平衡文生图与图生图编辑",
    tag: "标准",
  },
  {
    id: "gpt-image-2.6",
    desc: "最新预览图像档位",
    tag: "预览",
  },
  {
    id: "dall-e-3",
    desc: "OpenAI 标准兼容自动映射",
    tag: "兼容",
  },
];

const ASPECT_OPTIONS = [
  {
    value: "1024x1024",
    label: "1:1",
    sub: "1024×1024",
    rect: { x: 4, y: 4, w: 12, h: 12 },
  },
  {
    value: "1792x1024",
    label: "16:9",
    sub: "1792×1024",
    rect: { x: 2, y: 5, w: 16, h: 10 },
  },
  {
    value: "1024x1792",
    label: "9:16",
    sub: "1024×1792",
    rect: { x: 5, y: 2, w: 10, h: 16 },
  },
];

const FALLBACK_PRESETS = [
  {
    label: "云端玻璃温室",
    prompt:
      "A futuristic glass greenhouse floating in the clouds at golden hour, ultra-detailed architectural photography, soft volumetric light",
  },
  {
    label: "钛金属合成器",
    prompt:
      "Minimalist matte titanium desktop audio synthesizer with warm amber OLED readout, studio product photography, softbox lighting",
  },
  {
    label: "京都水彩茶屋",
    prompt:
      "A peaceful Kyoto wooden tea house beside a misty bamboo stream at dawn, delicate watercolor and ink illustration",
  },
];

const PLAZA_CATEGORIES = [
  "全部",
  "⭐ 我的收藏",
  "摄影写实",
  "3D 与微缩",
  "海报与文字排版",
  "插画与二次元",
  "产品与 UI",
  "参考图编辑",
] as const;

const SOURCE_OPTIONS = [
  { value: "all", label: "全部来源" },
  { value: "civitai", label: "Civitai 热榜" },
  { value: "lexica", label: "Lexica 灵感" },
  { value: "huggingface", label: "HF 数据集" },
  { value: "reddit", label: "Reddit 热帖" },
  { value: "ai_search", label: "AI 趋势" },
  { value: "seed", label: "精选模板" },
];

const SOURCE_LABELS: Record<string, string> = {
  seed: "精选模板",
  civitai: "Civitai",
  lexica: "Lexica",
  reddit: "Reddit",
  huggingface: "HF 数据集",
  ai_search: "AI 趋势",
};

interface CategoryVisualTheme {
  gradient: string;
  accent: string;
  badge: string;
}

const CATEGORY_THEMES: Record<string, CategoryVisualTheme> = {
  摄影写实: {
    gradient:
      "linear-gradient(135deg, rgba(15,23,42,0.92) 0%, rgba(51,65,85,0.88) 55%, rgba(180,83,9,0.72) 100%)",
    accent: "rgba(251,191,36,0.28)",
    badge: "35mm · OPTICS",
  },
  "3D 与微缩": {
    gradient:
      "linear-gradient(135deg, rgba(30,27,75,0.94) 0%, rgba(49,46,129,0.86) 55%, rgba(8,145,178,0.76) 100%)",
    accent: "rgba(34,211,238,0.28)",
    badge: "OCTANE · 3D",
  },
  海报与文字排版: {
    gradient:
      "linear-gradient(135deg, rgba(127,29,29,0.92) 0%, rgba(153,27,27,0.86) 52%, rgba(217,119,6,0.74) 100%)",
    accent: "rgba(253,224,71,0.26)",
    badge: "SWISS · TYPE",
  },
  插画与二次元: {
    gradient:
      "linear-gradient(135deg, rgba(6,78,59,0.92) 0%, rgba(15,118,110,0.86) 55%, rgba(59,130,246,0.72) 100%)",
    accent: "rgba(110,231,183,0.26)",
    badge: "WATERCOLOR · CEL",
  },
  "产品与 UI": {
    gradient:
      "linear-gradient(135deg, rgba(24,24,27,0.94) 0%, rgba(39,39,42,0.88) 58%, rgba(37,99,235,0.72) 100%)",
    accent: "rgba(96,165,250,0.28)",
    badge: "INDUSTRIAL · UI",
  },
  参考图编辑: {
    gradient:
      "linear-gradient(135deg, rgba(76,29,149,0.92) 0%, rgba(109,40,217,0.86) 55%, rgba(219,39,119,0.72) 100%)",
    accent: "rgba(244,114,182,0.28)",
    badge: "IMG2IMG · EDIT",
  },
};

interface ActiveTemplateState {
  title: string;
  template: string;
  variables: PromptVariable[];
  values: Record<string, string>;
}

function renderTemplatePrompt(
  template: string,
  variables: PromptVariable[],
  values: Record<string, string>
): string {
  let out = template;
  for (const v of variables) {
    const val = (values[v.key] ?? v.default_value ?? "").trim();
    const token = `[${v.key}]`;
    if (val) {
      out = out.split(token).join(val);
    }
  }
  return out;
}

function formatSizeBadge(size: string): string {
  const found = ASPECT_OPTIONS.find((a) => a.value === size);
  return found ? found.label : "1:1";
}

function formatSyncTimestamp(iso?: string): string {
  if (!iso) return "尚未同步";
  const ts = Date.parse(iso);
  if (Number.isNaN(ts)) return iso;
  return new Date(ts).toLocaleTimeString("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

interface PlazaPromptCardProps {
  item: PromptPlazaItem;
  isFavorite: boolean;
  onToggleFavorite: (id: string) => void;
  onApply: (item: PromptPlazaItem, lang: "en" | "zh") => void;
  onQuickGenerate: (item: PromptPlazaItem, lang: "en" | "zh") => void;
}

const PlazaPromptCard: React.FC<PlazaPromptCardProps> = ({
  item,
  isFavorite,
  onToggleFavorite,
  onApply,
  onQuickGenerate,
}) => {
  const [lang, setLang] = useState<"en" | "zh">("en");
  const [imgFailed, setImgFailed] = useState(false);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    setImgFailed(false);
  }, [item.preview_url]);

  const theme =
    CATEGORY_THEMES[item.category] || CATEGORY_THEMES["摄影写实"];
  const activePrompt =
    lang === "zh" ? item.prompt_zh || item.prompt_en : item.prompt_en;
  const previewText = useMemo(
    () =>
      item.variables && item.variables.length > 0
        ? renderTemplatePrompt(activePrompt, item.variables, {})
        : activePrompt,
    [activePrompt, item.variables]
  );

  const showRealImage = Boolean(item.preview_url) && !imgFailed;
  const sourceLabel = SOURCE_LABELS[item.source] || item.source;
  const isEdits = item.recommended_mode === "edits";

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(previewText);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1400);
    } catch {
      // ignore clipboard errors
    }
  };

  return (
    <div className="p-card p-card-interactive rounded-2xl overflow-hidden flex flex-col group">
      {/* Visual Preview Header */}
      <div className="relative h-40 overflow-hidden z-muted">
        {showRealImage ? (
          <img
            src={item.preview_url}
            alt={item.title_zh}
            loading="lazy"
            onError={() => setImgFailed(true)}
            className="w-full h-full object-cover transition-transform duration-300 group-hover:scale-[1.03]"
          />
        ) : (
          <div
            className="w-full h-full relative flex flex-col justify-between p-3 text-white select-none"
            style={{ background: theme.gradient }}
          >
            <svg
              className="absolute inset-0 w-full h-full opacity-25 pointer-events-none"
              viewBox="0 0 320 160"
              preserveAspectRatio="none"
              aria-hidden="true"
            >
              <circle cx="268" cy="34" r="56" fill={theme.accent} />
              <circle
                cx="48"
                cy="142"
                r="44"
                stroke="rgba(255,255,255,0.28)"
                strokeWidth="1.2"
                fill="none"
              />
              <path
                d="M0 120 Q90 85 170 118 T320 96"
                stroke="rgba(255,255,255,0.2)"
                strokeWidth="1.2"
                fill="none"
              />
            </svg>
            <div />
            <div className="relative z-10 flex items-end justify-between gap-2 pb-9">
              <div className="min-w-0">
                <div className="text-[10px] font-mono-num tracking-widest uppercase text-white/75">
                  {theme.badge}
                </div>
                <div className="text-sm font-semibold truncate text-white drop-shadow-xs mt-0.5">
                  {item.title_zh}
                </div>
              </div>
            </div>
          </div>
        )}

        {/* Top Badges */}
        <div className="absolute inset-x-0 top-0 p-2.5 flex items-center justify-between gap-2 bg-gradient-to-b from-black/55 to-transparent">
          <div className="flex items-center gap-1.5 min-w-0">
            <span className="px-2 py-0.5 rounded-md bg-black/60 backdrop-blur-xs text-white font-medium truncate">
              {sourceLabel}
            </span>
            <span className="px-1.5 py-0.5 rounded-md bg-black/50 backdrop-blur-xs text-white/90 font-mono-num">
              {formatSizeBadge(item.recommended_size)}
            </span>
          </div>
          <button
            type="button"
            onClick={() => onToggleFavorite(item.id)}
            className="p-1.5 rounded-lg bg-black/55 hover:bg-black/75 text-white cursor-pointer transition-colors"
            title={isFavorite ? "取消收藏" : "加入收藏"}
          >
            <Star
              size={13}
              className={isFavorite ? "text-amber-400 fill-amber-400" : "text-white"}
            />
          </button>
        </div>

        {/* Bottom Metadata Strip */}
        <div className="absolute inset-x-0 bottom-0 px-2.5 py-2 flex items-center justify-between gap-2 bg-gradient-to-t from-black/70 via-black/35 to-transparent text-white">
          <span
            className={`px-2 py-0.5 rounded-md text-[11px] font-medium whitespace-nowrap shrink-0 ${
              isEdits ? "bg-violet-500/80" : "bg-emerald-500/75"
            }`}
          >
            {isEdits ? "垫图编辑" : "文生图"}
          </span>
          <div className="flex items-center gap-1.5 shrink-0 font-mono-num text-[11px] text-white/95">
            {item.stats?.hearts > 0 && (
              <span className="inline-flex items-center gap-1">
                <Heart size={11} className="fill-rose-400 text-rose-400" />
                {item.stats.hearts}
              </span>
            )}
            {item.stats?.likes > 0 && (
              <span className="inline-flex items-center gap-1">
                <ThumbsUp size={11} />
                {item.stats.likes}
              </span>
            )}
            <span className="px-1.5 py-0.5 rounded bg-white/15 font-semibold">
              {item.stats?.score?.toFixed(1) ?? "90.0"}
            </span>
          </div>
        </div>
      </div>

      {/* Card Body */}
      <div className="p-3.5 flex-1 flex flex-col justify-between gap-2.5">
        <div className="space-y-2">
          <div className="flex items-start justify-between gap-2">
            <div className="min-w-0">
              <h3 className="text-[13px] font-semibold leading-snug truncate">
                {item.title_zh}
              </h3>
              <div className="flex items-center gap-1.5 mt-1 flex-wrap">
                <span className="px-1.5 py-0.5 rounded border z-border z-canvas z-text-muted text-[10px]">
                  {item.category}
                </span>
                <span className="font-mono-num text-[10px] z-text-muted truncate max-w-[140px]">
                  {item.recommended_model}
                </span>
              </div>
            </div>

            <div className="flex items-center gap-1 shrink-0">
              <button
                type="button"
                onClick={() => setLang((prev) => (prev === "en" ? "zh" : "en"))}
                className="px-1.5 py-1 rounded-lg border z-border z-canvas z-ghost-btn font-mono-num text-[10px] cursor-pointer"
                title="切换中/英文 Prompt 预览与填入"
              >
                {lang === "en" ? "EN" : "中"}
              </button>
              <button
                type="button"
                onClick={handleCopy}
                className="p-1 rounded-lg border z-border z-canvas z-ghost-btn cursor-pointer"
                title="复制完整提示词"
              >
                {copied ? <Check size={12} /> : <Copy size={12} />}
              </button>
            </div>
          </div>

          <p className="z-text-sec leading-relaxed line-clamp-3 text-[11.5px]">
            {previewText}
          </p>

          {item.variables && item.variables.length > 0 && (
            <div className="flex items-center gap-1 flex-wrap">
              <span className="text-[10px] z-text-muted">可调槽位:</span>
              {item.variables.map((v) => (
                <span
                  key={v.key}
                  className="px-1.5 py-0.5 rounded-md z-muted z-text-sec font-mono-num text-[10px]"
                >
                  [{v.key}]
                </span>
              ))}
            </div>
          )}
        </div>

        {/* Actions */}
        <div className="flex items-center gap-1.5 pt-1 border-t z-border">
          <button
            type="button"
            onClick={() => onApply(item, lang)}
            className="flex-1 inline-flex items-center justify-center gap-1 py-1.5 rounded-xl border z-border z-canvas z-ghost-btn font-medium cursor-pointer"
            title="同步推荐模式、模型、画幅与变量槽位到左侧参数台"
          >
            <SlidersHorizontal size={12} />
            <span>做同款</span>
          </button>
          <button
            type="button"
            onClick={() => onQuickGenerate(item, lang)}
            className="flex-1 inline-flex items-center justify-center gap-1 py-1.5 rounded-xl z-primary-btn font-medium cursor-pointer"
            title={
              isEdits
                ? "同步编辑参数并前往上传参考图"
                : "同步全套参数并直接生成图像"
            }
          >
            <Wand2 size={12} />
            <span>{isEdits ? "去垫图" : "立即生成"}</span>
          </button>
        </div>
      </div>
    </div>
  );
};

export const ImageStudio: React.FC = () => {
  const {
    imageModels,
    source: modelsSource,
    loading: modelsLoading,
    refresh: refreshModels,
  } = useModelsCatalog();

  const modelCards = useMemo(() => {
    if (imageModels.length > 0) {
      return imageModels.map((m) => ({
        id: m.id,
        desc: m.subtitle,
        tag: m.tag,
      }));
    }
    return MODEL_CARDS;
  }, [imageModels]);

  const [mode, setMode] = useState<"generations" | "edits">("generations");
  const [prompt, setPrompt] = useState(FALLBACK_PRESETS[0].prompt);
  const [model, setModel] = useState(
    () => modelCards[0]?.id || "gpt-image-2.5-sunburst"
  );
  const [size, setSize] = useState("1024x1024");
  const [quality, setQuality] = useState("hd");
  const [files, setFiles] = useState<File[]>([]);
  const [filePreviews, setFilePreviews] = useState<string[]>([]);
  const [apiKey, setApiKey] = useState("");
  useEffect(() => {
    if (modelCards.length === 0) return;
    setModel((prev) =>
      modelCards.some((m) => m.id === prev)
        ? prev
        : modelCards.find((m) => m.id === "gpt-image-2.5-sunburst")?.id ||
          modelCards[0].id
    );
  }, [modelCards]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<{
    type: "info" | "success";
    text: string;
  } | null>(null);
  const [results, setResults] = useState<HydratedImageItem[]>([]);
  const [hydratedIdb, setHydratedIdb] = useState(false);
  const [imageCapableCount, setImageCapableCount] = useState<number | null>(
    null
  );
  const [lightboxItem, setLightboxItem] = useState<HydratedImageItem | null>(
    null
  );
  const [copiedCardId, setCopiedCardId] = useState<string | null>(null);
  const [dragOver, setDragOver] = useState(false);

  // Prompt Plaza & AI Expansion State
  const [activeTab, setActiveTab] = useState<"plaza" | "gallery">("plaza");
  const [plazaItems, setPlazaItems] = useState<PromptPlazaItem[]>([]);
  const [plazaCategories, setPlazaCategories] = useState<
    Record<string, number>
  >({});
  const [plazaSources, setPlazaSources] = useState<Record<string, number>>({});
  const [syncStatus, setSyncStatus] = useState<PromptSyncStatus | null>(null);
  const [plazaLoading, setPlazaLoading] = useState(false);
  const [plazaSyncing, setPlazaSyncing] = useState(false);
  const [plazaCategory, setPlazaCategory] = useState<string>("全部");
  const [plazaSource, setPlazaSource] = useState<string>("all");
  const [plazaSort, setPlazaSort] = useState<"popular" | "latest">("popular");
  const [plazaQuery, setPlazaQuery] = useState<string>("");
  const [favoriteIds, setFavoriteIds] = useState<string[]>(() =>
    loadFavoritePromptIds()
  );
  const [expandingPrompt, setExpandingPrompt] = useState(false);
  const [activeTemplate, setActiveTemplate] =
    useState<ActiveTemplateState | null>(null);

  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const objectUrlsRef = useRef<Set<string>>(new Set());
  const syncPollTimerRef = useRef<number | null>(null);
  const warmupTimerRef = useRef<number | null>(null);
  const didInitFilterRef = useRef(false);
  const plazaFilterRef = useRef({
    source: plazaSource,
    sort: plazaSort,
    q: plazaQuery,
  });
  plazaFilterRef.current = {
    source: plazaSource,
    sort: plazaSort,
    q: plazaQuery,
  };

  const applyPlazaResponse = (
    res: {
      items?: PromptPlazaItem[];
      categories?: Record<string, number>;
      sources?: Record<string, number>;
      sync_status?: PromptSyncStatus | null;
    },
  ) => {
    setPlazaItems(res.items || []);
    setPlazaCategories(res.categories || {});
    setPlazaSources(res.sources || res.sync_status?.source_counts || {});
    setSyncStatus(res.sync_status || null);
  };

  const startSyncPolling = (showNotice: boolean) => {
    setPlazaSyncing(true);
    if (syncPollTimerRef.current !== null) {
      return;
    }
    let polls = 0;
    syncPollTimerRef.current = window.setInterval(async () => {
      polls += 1;
      try {
        const latest = await fetchPromptPlaza(plazaFilterRef.current);
        applyPlazaResponse(latest);
        if (!latest.sync_status?.running || polls >= 16) {
          if (syncPollTimerRef.current !== null) {
            window.clearInterval(syncPollTimerRef.current);
            syncPollTimerRef.current = null;
          }
          setPlazaSyncing(false);
          if (showNotice && !latest.sync_status?.running) {
            setNotice({
              type: latest.sync_status?.last_error ? "info" : "success",
              text: latest.sync_status?.last_error
                ? `同步完成（部分外部源受限，已保留精选与可用条目）：${latest.sync_status.last_error}`
                : `已同步最新社区热榜与 AI 趋势，当前共收录 ${latest.sync_status?.total_count ?? latest.items.length} 条精选提示词`,
            });
          }
        }
      } catch {
        if (polls >= 16 && syncPollTimerRef.current !== null) {
          window.clearInterval(syncPollTimerRef.current);
          syncPollTimerRef.current = null;
          setPlazaSyncing(false);
        }
      }
    }, 1500);
  };

  const loadPlazaData = async (opts?: {
    source?: string;
    sort?: string;
    q?: string;
  }) => {
    setPlazaLoading(true);
    try {
      const res = await fetchPromptPlaza({
        source: opts?.source ?? plazaSource,
        sort: opts?.sort ?? plazaSort,
        q: opts?.q ?? plazaQuery,
      });
      applyPlazaResponse(res);
      if (res.sync_status?.running) {
        startSyncPolling(false);
      } else if (
        !res.sync_status?.last_synced_at &&
        warmupTimerRef.current === null
      ) {
        warmupTimerRef.current = window.setTimeout(() => {
          warmupTimerRef.current = null;
          void loadPlazaData(plazaFilterRef.current);
        }, 2500);
      }
    } catch (err: any) {
      // Keep UI usable even if admin token is locked
      setError(err?.message || "加载提示词广场失败");
    } finally {
      setPlazaLoading(false);
    }
  };

  useEffect(() => {
    let alive = true;
    loadStoredImages()
      .then((items) => {
        if (!alive) {
          items.forEach((it) => {
            if (it.isObjectUrl) URL.revokeObjectURL(it.url);
          });
          return;
        }
        items.forEach((it) => {
          if (it.isObjectUrl) objectUrlsRef.current.add(it.url);
        });
        setResults(items);
        setHydratedIdb(true);
      })
      .catch(() => setHydratedIdb(true));

    fetchAccounts("chatgpt")
      .then((res) => {
        if (!alive) return;
        const eligible = (res.accounts || []).filter(
          (a) => a.status === "active" && !a.image_limited
        ).length;
        setImageCapableCount(eligible);
      })
      .catch(() => {});

    loadPlazaData();

    return () => {
      alive = false;
      objectUrlsRef.current.forEach((u) => URL.revokeObjectURL(u));
      objectUrlsRef.current.clear();
      if (syncPollTimerRef.current !== null) {
        window.clearInterval(syncPollTimerRef.current);
        syncPollTimerRef.current = null;
      }
      if (warmupTimerRef.current !== null) {
        window.clearTimeout(warmupTimerRef.current);
        warmupTimerRef.current = null;
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!didInitFilterRef.current) {
      didInitFilterRef.current = true;
      return;
    }
    const timer = window.setTimeout(() => {
      loadPlazaData({ source: plazaSource, sort: plazaSort, q: plazaQuery });
    }, 200);
    return () => window.clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plazaSource, plazaSort, plazaQuery]);

  useEffect(() => {
    const urls = files.map((f) => URL.createObjectURL(f));
    setFilePreviews(urls);
    return () => {
      urls.forEach((u) => URL.revokeObjectURL(u));
    };
  }, [files]);

  const appendFiles = (incoming: FileList | File[]) => {
    const valid = Array.from(incoming).filter((f) =>
      f.type.startsWith("image/")
    );
    if (valid.length === 0) return;
    setFiles((prev) => [...prev, ...valid].slice(0, 4));
  };

  const removeFileAt = (idx: number) => {
    setFiles((prev) => prev.filter((_, i) => i !== idx));
  };

  const persistSingleResult = async (
    item: { url?: string; b64_json?: string; revised_prompt?: string },
    idx: number,
    latencyMs: number,
    usedPrompt: string,
    usedModel: string,
    usedSize: string,
    usedMode: "generations" | "edits"
  ): Promise<HydratedImageItem | null> => {
    let blob: Blob | undefined;
    let remoteUrl: string | undefined;

    if (item.b64_json) {
      blob = base64ToBlob(item.b64_json) || undefined;
    } else if (item.url?.startsWith("data:")) {
      blob = dataUrlToBlob(item.url) || undefined;
    } else if (item.url) {
      remoteUrl = item.url;
      try {
        const resp = await fetch(item.url);
        if (resp.ok) {
          const fetched = await resp.blob();
          if (fetched.size > 0 && fetched.type.startsWith("image/")) {
            blob = fetched;
          }
        }
      } catch {
        // External signed URL without CORS still stored safely via remoteUrl
      }
    }

    if (!blob && !remoteUrl) return null;

    const record: StoredImageRecord = {
      id: `img-${Date.now()}-${idx}`,
      blob,
      remoteUrl,
      prompt: usedPrompt,
      revisedPrompt: item.revised_prompt,
      model: usedModel,
      size: usedSize,
      quality,
      mode: usedMode,
      latencyMs,
      createdAt: new Date().toLocaleTimeString("zh-CN", { hour12: false }),
      timestamp: Date.now() - idx,
    };

    await saveStoredImage(record);
    const hydrated = hydrateStoredImage(record);
    if (hydrated?.isObjectUrl) {
      objectUrlsRef.current.add(hydrated.url);
    }
    return hydrated;
  };

  const executeImageRequest = async (override?: {
    prompt: string;
    model: string;
    size: string;
    mode: "generations" | "edits";
  }) => {
    const targetPrompt = (override?.prompt ?? prompt).trim();
    const targetModel = override?.model ?? model;
    const targetSize = override?.size ?? size;
    const targetMode = override?.mode ?? mode;

    if (!targetPrompt || loading) return;
    if (targetMode === "edits" && files.length === 0) {
      setError("请至少上传 1 张参考图像用于图生图编辑");
      return;
    }

    setActiveTab("gallery");
    setLoading(true);
    setError(null);
    const started = performance.now();

    try {
      const res =
        targetMode === "edits"
          ? await editImageWithReferences({
              prompt: targetPrompt,
              model: targetModel,
              size: targetSize,
              files,
              apiKey: apiKey.trim() || undefined,
            })
          : await generateImage({
              prompt: targetPrompt,
              model: targetModel,
              size: targetSize,
              quality,
              apiKey: apiKey.trim() || undefined,
            });

      const latencyMs = Math.round(performance.now() - started);
      const saved = (
        await Promise.all(
          (res.data || []).map((item, idx) =>
            persistSingleResult(
              item,
              idx,
              latencyMs,
              targetPrompt,
              targetModel,
              targetSize,
              targetMode
            )
          )
        )
      ).filter((x): x is HydratedImageItem => x !== null);

      if (saved.length === 0) {
        setError("未从响应中解析到图像数据");
      } else {
        setResults((prev) => [...saved, ...prev]);
      }
    } catch (err: any) {
      setError(err?.message || "图像生成失败");
    } finally {
      setLoading(false);
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    await executeImageRequest();
  };

  const applyPlazaItem = (
    item: PromptPlazaItem,
    lang: "en" | "zh" = "en"
  ): {
    prompt: string;
    model: string;
    size: string;
    mode: "generations" | "edits";
  } => {
    const nextMode: "generations" | "edits" =
      item.recommended_mode === "edits" ? "edits" : "generations";
    const nextModel = modelCards.some((m) => m.id === item.recommended_model)
      ? item.recommended_model
      : model;
    const nextSize = ASPECT_OPTIONS.some((a) => a.value === item.recommended_size)
      ? item.recommended_size
      : size;

    const template =
      lang === "zh" ? item.prompt_zh || item.prompt_en : item.prompt_en;
    const vars = item.variables || [];
    const initValues: Record<string, string> = {};
    for (const v of vars) {
      initValues[v.key] = v.default_value;
    }
    const rendered =
      vars.length > 0
        ? renderTemplatePrompt(template, vars, initValues)
        : template;

    setMode(nextMode);
    setModel(nextModel);
    setSize(nextSize);
    setPrompt(rendered);
    setError(null);

    if (vars.length > 0) {
      setActiveTemplate({
        title: item.title_zh,
        template,
        variables: vars,
        values: initValues,
      });
    } else {
      setActiveTemplate(null);
    }

    return {
      prompt: rendered,
      model: nextModel,
      size: nextSize,
      mode: nextMode,
    };
  };

  const handleApplyCard = (item: PromptPlazaItem, lang: "en" | "zh") => {
    const applied = applyPlazaItem(item, lang);
    setNotice({
      type: "success",
      text: `已联动套用「${item.title_zh}」(${
        applied.mode === "edits" ? "参考图编辑" : "文生图"
      } · ${applied.model} · ${formatSizeBadge(applied.size)})`,
    });
  };

  const handleQuickGenerate = async (
    item: PromptPlazaItem,
    lang: "en" | "zh"
  ) => {
    const applied = applyPlazaItem(item, lang);
    if (applied.mode === "edits" && files.length === 0) {
      setNotice({
        type: "info",
        text: `已切换至「参考图编辑」并套用「${item.title_zh}」，请先在左侧上传至少 1 张参考图后点击生成。`,
      });
      return;
    }
    await executeImageRequest(applied);
  };

  const handleExpandPrompt = async () => {
    const idea = prompt.trim();
    if (!idea || expandingPrompt) return;
    setExpandingPrompt(true);
    setError(null);
    try {
      const expanded = await expandImagePrompt({ idea, mode, size });
      const vars = expanded.variables || [];
      const initValues: Record<string, string> = {};
      for (const v of vars) {
        initValues[v.key] = v.default_value;
      }
      const rendered =
        vars.length > 0
          ? renderTemplatePrompt(expanded.prompt_en, vars, initValues)
          : expanded.prompt_en;

      setPrompt(rendered);
      if (modelCards.some((m) => m.id === expanded.recommended_model)) {
        setModel(expanded.recommended_model);
      }
      if (vars.length > 0) {
        setActiveTemplate({
          title: expanded.title_zh || "AI 魔法扩写",
          template: expanded.prompt_en,
          variables: vars,
          values: initValues,
        });
      } else {
        setActiveTemplate(null);
      }
      setNotice({
        type: "success",
        text:
          expanded.source === "ai_pool"
            ? `已通过本机号池 AI 扩写为「${expanded.title_zh}」，可在上方槽位微调细节`
            : `已智能结构化扩写为「${expanded.title_zh}」，可在上方槽位微调细节`,
      });
    } catch (err: any) {
      setError(err?.message || "AI 扩写失败");
    } finally {
      setExpandingPrompt(false);
    }
  };

  const handleRandomInspiration = () => {
    const pool = filteredPlazaItems.length > 0 ? filteredPlazaItems : plazaItems;
    if (pool.length === 0) return;
    const candidates = pool.filter((it) => it.prompt_en !== prompt);
    const list = candidates.length > 0 ? candidates : pool;
    const picked = list[Math.floor(Math.random() * list.length)];
    const applied = applyPlazaItem(picked, "en");
    setNotice({
      type: "info",
      text: `🎲 随机抽取灵感：「${picked.title_zh}」(${applied.model} · ${formatSizeBadge(applied.size)})`,
    });
  };

  const handleUpdateVariable = (key: string, nextVal: string) => {
    if (!activeTemplate) return;
    const nextValues = { ...activeTemplate.values, [key]: nextVal };
    setActiveTemplate({ ...activeTemplate, values: nextValues });
    setPrompt(
      renderTemplatePrompt(
        activeTemplate.template,
        activeTemplate.variables,
        nextValues
      )
    );
  };

  const handleSyncPlaza = async () => {
    if (plazaSyncing) return;
    setPlazaSyncing(true);
    setError(null);
    try {
      const res = await triggerPromptPlazaSync();
      setSyncStatus(res.sync_status);
      startSyncPolling(true);
    } catch (err: any) {
      setError(err?.message || "触发同步失败");
      setPlazaSyncing(false);
    }
  };

  const handleToggleFavorite = (id: string) => {
    setFavoriteIds(toggleFavoritePromptId(id));
  };

  const handleDeleteOne = async (item: HydratedImageItem) => {
    await deleteStoredImage(item.id);
    if (item.isObjectUrl) {
      URL.revokeObjectURL(item.url);
      objectUrlsRef.current.delete(item.url);
    }
    setResults((prev) => prev.filter((x) => x.id !== item.id));
    if (lightboxItem?.id === item.id) {
      setLightboxItem(null);
    }
  };

  const handleClearGallery = async () => {
    await clearStoredImages();
    results.forEach((r) => {
      if (r.isObjectUrl) {
        URL.revokeObjectURL(r.url);
        objectUrlsRef.current.delete(r.url);
      }
    });
    setResults([]);
    setLightboxItem(null);
  };

  const handleCopyPromptOrUrl = async (item: HydratedImageItem) => {
    try {
      const target =
        item.remoteUrl || item.revisedPrompt || item.prompt || item.url;
      await navigator.clipboard.writeText(target);
      setCopiedCardId(item.id);
      window.setTimeout(() => setCopiedCardId(null), 1500);
    } catch {
      // ignore
    }
  };

  const handleRemix = (item: HydratedImageItem) => {
    setPrompt(item.revisedPrompt || item.prompt);
    if (modelCards.some((m) => m.id === item.model)) {
      setModel(item.model);
    }
    if (ASPECT_OPTIONS.some((a) => a.value === item.size)) {
      setSize(item.size);
    }
    setActiveTemplate(null);
    setLightboxItem(null);
  };

  const filteredPlazaItems = useMemo(() => {
    if (plazaCategory === "全部") return plazaItems;
    if (plazaCategory === "⭐ 我的收藏") {
      const favSet = new Set(favoriteIds);
      return plazaItems.filter((it) => favSet.has(it.id));
    }
    return plazaItems.filter((it) => it.category === plazaCategory);
  }, [plazaItems, plazaCategory, favoriteIds]);

  const topQuickCapsules = useMemo(() => plazaItems.slice(0, 3), [plazaItems]);

  return (
    <div className="flex-1 grid grid-cols-1 md:grid-cols-[336px_1fr] min-h-0 h-full overflow-hidden">
      {/* Left Independent Parameter Studio Panel */}
      <form
        onSubmit={handleSubmit}
        className="z-surface border-b md:border-b-0 md:border-r z-border flex flex-col min-h-0 max-h-[56vh] md:max-h-none md:h-full overflow-hidden"
      >
        <div className="flex-1 overflow-y-auto p-4 space-y-4 min-h-0 text-xs">
          <div>
            <h2 className="text-sm font-semibold">图像创作参数台</h2>
            <p className="z-text-muted mt-1 leading-relaxed">
              遇到生图 429 自动隔离该号生图能力并无感切号重绘，产物自动持久存入浏览器 IndexedDB。
            </p>
          </div>

          {/* Mode Toggle */}
          <div className="p-surface p-1 rounded-xl grid grid-cols-2 gap-1">
            <button
              type="button"
              onClick={() => {
                setMode("generations");
                setError(null);
              }}
              className={`inline-flex items-center justify-center gap-1.5 py-1.5 rounded-lg cursor-pointer transition-colors ${
                mode === "generations"
                  ? "z-primary-btn font-medium"
                  : "z-text-muted z-ghost-btn"
              }`}
            >
              <Sparkles size={12} />
              <span>文生图</span>
            </button>
            <button
              type="button"
              onClick={() => {
                setMode("edits");
                setError(null);
              }}
              className={`inline-flex items-center justify-center gap-1.5 py-1.5 rounded-lg cursor-pointer transition-colors ${
                mode === "edits"
                  ? "z-primary-btn font-medium"
                  : "z-text-muted z-ghost-btn"
              }`}
            >
              <Layers size={12} />
              <span>参考图编辑</span>
            </button>
          </div>

          {/* Tactile Model Cards */}
          <div className="space-y-1.5">
            <div className="flex items-center justify-between gap-2">
              <label className="font-medium z-text-sec block">
                生图模型档位 ({modelCards.length})
              </label>
              <button
                type="button"
                disabled={modelsLoading}
                onClick={() =>
                  void refreshModels({
                    forceRefresh: true,
                    apiKey: apiKey.trim() || undefined,
                  })
                }
                title={
                  modelsSource === "fallback"
                    ? "从网关重新拉取模型目录（当前内置参考）"
                    : "重新拉取模型目录（当前已缓存复用）"
                }
                className="inline-flex items-center gap-1 px-2 py-0.5 rounded-lg border z-border z-canvas z-ghost-btn z-text-muted cursor-pointer"
              >
                <RefreshCw
                  size={11}
                  className={modelsLoading ? "animate-spin" : ""}
                />
                <span>{modelsLoading ? "拉取中" : "刷新"}</span>
              </button>
            </div>
            <div className="space-y-1.5">
              {modelCards.map((m) => {
                const active = model === m.id;
                return (
                  <button
                    key={m.id}
                    type="button"
                    onClick={() => setModel(m.id)}
                    className={`w-full p-2.5 rounded-xl flex items-center justify-between gap-2 text-left cursor-pointer border transition-colors ${
                      active
                        ? "p-nav-active"
                        : "z-border z-canvas z-ghost-btn z-text-sec"
                    }`}
                  >
                    <div className="min-w-0">
                      <div className="font-mono-num font-medium truncate">
                        {m.id}
                      </div>
                      <div className="z-text-muted truncate mt-0.5">
                        {m.desc}
                      </div>
                    </div>
                    <span
                      className={`px-2 py-0.5 rounded-md border shrink-0 ${
                        active ? "z-pill-success" : "z-border z-muted z-text-muted"
                      }`}
                    >
                      {m.tag}
                    </span>
                  </button>
                );
              })}
            </div>
          </div>

          {/* SVG Aspect Ratio Wireframes */}
          <div className="space-y-1.5">
            <label className="font-medium z-text-sec block">画幅比例</label>
            <div className="grid grid-cols-3 gap-2">
              {ASPECT_OPTIONS.map((opt) => {
                const active = size === opt.value;
                return (
                  <button
                    key={opt.value}
                    type="button"
                    onClick={() => setSize(opt.value)}
                    className={`p-2 rounded-xl border flex flex-col items-center gap-1 cursor-pointer transition-colors ${
                      active
                        ? "z-primary-btn border-transparent font-medium"
                        : "z-border z-canvas z-text-sec z-ghost-btn"
                    }`}
                  >
                    <svg
                      width="20"
                      height="20"
                      viewBox="0 0 20 20"
                      fill="none"
                      aria-hidden="true"
                    >
                      <rect
                        x={opt.rect.x}
                        y={opt.rect.y}
                        width={opt.rect.w}
                        height={opt.rect.h}
                        rx="2"
                        stroke="currentColor"
                        strokeWidth="1.5"
                      />
                    </svg>
                    <span>{opt.label}</span>
                  </button>
                );
              })}
            </div>
          </div>

          {/* Quality Pills */}
          <div className="space-y-1.5">
            <label className="font-medium z-text-sec block">画质精度</label>
            <div className="p-surface p-1 rounded-xl grid grid-cols-2 gap-1">
              <button
                type="button"
                onClick={() => setQuality("hd")}
                className={`py-1.5 rounded-lg cursor-pointer transition-colors ${
                  quality === "hd"
                    ? "z-primary-btn font-medium"
                    : "z-text-muted z-ghost-btn"
                }`}
              >
                HD 高精细度
              </button>
              <button
                type="button"
                onClick={() => setQuality("standard")}
                className={`py-1.5 rounded-lg cursor-pointer transition-colors ${
                  quality === "standard"
                    ? "z-primary-btn font-medium"
                    : "z-text-muted z-ghost-btn"
                }`}
              >
                Standard 标准
              </button>
            </div>
          </div>

          {/* Reference Image Uploader for /edits */}
          {mode === "edits" && (
            <div className="space-y-2">
              <label className="font-medium z-text-sec block">
                参考图上传（最多 4 张）
              </label>
              <input
                ref={fileInputRef}
                type="file"
                accept="image/*"
                multiple
                hidden
                onChange={(e) => {
                  if (e.target.files) appendFiles(e.target.files);
                  e.target.value = "";
                }}
              />
              <div
                onClick={() => fileInputRef.current?.click()}
                onDragOver={(e) => {
                  e.preventDefault();
                  setDragOver(true);
                }}
                onDragLeave={() => setDragOver(false)}
                onDrop={(e) => {
                  e.preventDefault();
                  setDragOver(false);
                  if (e.dataTransfer.files) appendFiles(e.dataTransfer.files);
                }}
                className={`border border-dashed rounded-xl p-3.5 text-center z-canvas cursor-pointer space-y-1 ${
                  dragOver ? "z-border-strong z-muted" : "z-border"
                }`}
              >
                <div className="flex justify-center z-text-muted">
                  <UploadCloud size={18} />
                </div>
                <div className="font-medium">点击或拖拽参考图至此处</div>
                <div className="z-text-muted">支持 PNG / JPG / WebP</div>
              </div>

              {filePreviews.length > 0 && (
                <div className="grid grid-cols-4 gap-1.5">
                  {filePreviews.map((src, i) => (
                    <div
                      key={src}
                      className="relative aspect-square rounded-lg border z-border overflow-hidden"
                    >
                      <img
                        src={src}
                        alt={`ref-${i}`}
                        className="w-full h-full object-cover"
                      />
                      <button
                        type="button"
                        onClick={() => removeFileAt(i)}
                        className="absolute top-1 right-1 p-0.5 rounded-full bg-black/75 text-white cursor-pointer"
                        aria-label="移除参考图"
                      >
                        <X size={10} />
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}

          {/* Prompt + AI Expand + Random + Interactive Variables */}
          <div className="space-y-2">
            <div className="flex items-center justify-between gap-2">
              <label className="font-medium z-text-sec">Prompt 提示词</label>
              <div className="flex items-center gap-1">
                <button
                  type="button"
                  onClick={handleExpandPrompt}
                  disabled={!prompt.trim() || expandingPrompt}
                  className="inline-flex items-center gap-1 px-2 py-0.5 rounded-lg border z-border z-canvas z-ghost-btn text-[11px] font-medium cursor-pointer disabled:opacity-45"
                  title="调用本机号池将简短想法扩写为大师级生图提示词"
                >
                  {expandingPrompt ? (
                    <Loader2 size={11} className="animate-spin" />
                  ) : (
                    <Sparkles size={11} />
                  )}
                  <span>{expandingPrompt ? "扩写中..." : "AI 扩写"}</span>
                </button>
                <button
                  type="button"
                  onClick={handleRandomInspiration}
                  disabled={plazaItems.length === 0}
                  className="inline-flex items-center gap-1 px-2 py-0.5 rounded-lg border z-border z-canvas z-ghost-btn text-[11px] font-medium cursor-pointer disabled:opacity-45"
                  title="从提示词广场随机抽取一条高分灵感"
                >
                  <Dices size={11} />
                  <span>随机灵感</span>
                </button>
              </div>
            </div>

            <div className="flex flex-wrap gap-1.5">
              {topQuickCapsules.length > 0
                ? topQuickCapsules.map((p) => (
                    <button
                      key={p.id}
                      type="button"
                      onClick={() => handleApplyCard(p, "en")}
                      className="px-2.5 py-1 rounded-full border z-border z-canvas z-ghost-btn whitespace-nowrap cursor-pointer text-[11px]"
                      title={`做同款：${p.title_zh}`}
                    >
                      {p.title_zh}
                    </button>
                  ))
                : FALLBACK_PRESETS.map((p) => (
                    <button
                      key={p.label}
                      type="button"
                      onClick={() => {
                        setActiveTemplate(null);
                        setPrompt(p.prompt);
                      }}
                      className="px-2.5 py-1 rounded-full border z-border z-canvas z-ghost-btn whitespace-nowrap cursor-pointer text-[11px]"
                    >
                      {p.label}
                    </button>
                  ))}
            </div>

            {/* Interactive Variable Slot Editor */}
            {activeTemplate && activeTemplate.variables.length > 0 && (
              <div className="p-card rounded-xl p-2.5 space-y-2">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-medium z-text-sec text-[11px] truncate">
                    变量槽位填空 · {activeTemplate.title}
                  </span>
                  <button
                    type="button"
                    onClick={() => setActiveTemplate(null)}
                    className="p-0.5 rounded z-text-muted z-ghost-btn cursor-pointer"
                    title="收起变量槽位面板"
                  >
                    <X size={12} />
                  </button>
                </div>
                <div className="space-y-2">
                  {activeTemplate.variables.map((v) => {
                    const currentVal =
                      activeTemplate.values[v.key] ?? v.default_value;
                    return (
                      <div key={v.key} className="space-y-1">
                        <div className="flex items-center justify-between gap-1 text-[11px]">
                          <span className="z-text-muted">{v.label}</span>
                          <code className="font-mono-num z-text-sec">
                            [{v.key}]
                          </code>
                        </div>
                        <input
                          type="text"
                          value={currentVal}
                          onChange={(e) =>
                            handleUpdateVariable(v.key, e.target.value)
                          }
                          className="w-full px-2.5 py-1 rounded-lg p-input text-[11px]"
                        />
                        {v.options && v.options.length > 0 && (
                          <div className="flex flex-wrap gap-1">
                            {v.options.map((opt) => {
                              const selected = currentVal === opt;
                              return (
                                <button
                                  key={opt}
                                  type="button"
                                  onClick={() =>
                                    handleUpdateVariable(v.key, opt)
                                  }
                                  className={`px-2 py-0.5 rounded-md border text-[10px] cursor-pointer truncate max-w-full ${
                                    selected
                                      ? "z-primary-btn border-transparent"
                                      : "z-border z-canvas z-text-muted z-ghost-btn"
                                  }`}
                                  title={opt}
                                >
                                  {opt}
                                </button>
                              );
                            })}
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              </div>
            )}

            <textarea
              rows={4}
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder="输入简短创意点击「AI 扩写」，或从右侧广场做同款..."
              className="w-full p-2.5 rounded-xl p-input resize-y"
            />
          </div>

          <details className="z-text-muted">
            <summary className="cursor-pointer select-none py-1">
              高级鉴权配置（可选网关 API Key）
            </summary>
            <input
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              placeholder="Bearer key（留空直连）"
              className="mt-1.5 w-full px-3 py-1.5 rounded-xl p-input font-mono-num"
            />
          </details>
        </div>

        {/* Sticky Primary Submit Button */}
        <div className="p-3.5 border-t z-border z-surface shrink-0">
          <button
            type="submit"
            disabled={loading || !prompt.trim()}
            className="w-full py-2.5 rounded-xl z-primary-btn text-xs font-semibold inline-flex items-center justify-center gap-1.5 cursor-pointer"
          >
            {loading ? (
              <Loader2 size={14} className="animate-spin" />
            ) : (
              <Wand2 size={14} />
            )}
            <span>
              {loading
                ? "生图队列处理中..."
                : mode === "edits"
                  ? "生成编辑图像"
                  : "开始生成图像"}
            </span>
          </button>
        </div>
      </form>

      {/* Right Dot-Grid Studio Workspace (Dual Tab: Prompt Plaza <-> Local Gallery) */}
      <div className="studio-grid-bg overflow-y-auto p-5 flex flex-col gap-4 min-h-0 text-xs">
        {/* Top Dual Tab & Pool Status Bar */}
        <div className="flex flex-wrap items-center justify-between gap-2.5">
          <div className="flex flex-wrap items-center gap-2">
            <div className="p-surface p-1 rounded-xl inline-flex items-center gap-1">
              <button
                type="button"
                onClick={() => setActiveTab("plaza")}
                className={`inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg cursor-pointer transition-colors ${
                  activeTab === "plaza"
                    ? "z-primary-btn font-semibold"
                    : "z-text-muted z-ghost-btn"
                }`}
              >
                <Flame size={13} />
                <span>提示词广场</span>
                <span className="font-mono-num opacity-80">
                  ({plazaItems.length})
                </span>
              </button>
              <button
                type="button"
                onClick={() => setActiveTab("gallery")}
                className={`inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg cursor-pointer transition-colors ${
                  activeTab === "gallery"
                    ? "z-primary-btn font-semibold"
                    : "z-text-muted z-ghost-btn"
                }`}
              >
                <ImageIcon size={13} />
                <span>我的作品库</span>
                <span className="font-mono-num opacity-80">
                  ({results.length})
                </span>
              </button>
            </div>

            <span
              className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full border font-medium ${
                (imageCapableCount ?? 0) > 0 ? "z-pill-success" : "z-pill-warn"
              }`}
            >
              <span
                className={
                  (imageCapableCount ?? 0) > 0
                    ? "status-dot-green"
                    : "status-dot-amber"
                }
              />
              <span>
                {imageCapableCount !== null
                  ? `${imageCapableCount} 个生图专线账号就绪`
                  : "正在检测生图池容量..."}
              </span>
            </span>
          </div>

          {activeTab === "plaza" ? (
            <div className="flex items-center gap-2">
              {syncStatus && (syncStatus.active_sources?.length ?? 0) > 0 && (
                <span
                  className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full border z-pill-info font-mono-num"
                  title={`已激活拉取源：${syncStatus.active_sources?.join("、")}`}
                >
                  <span>已接入 {syncStatus.active_sources?.length} 个外部源</span>
                </span>
              )}
              <span className="z-text-muted font-mono-num hidden sm:inline">
                最近同步：{formatSyncTimestamp(syncStatus?.last_synced_at)}
              </span>
              <button
                type="button"
                onClick={handleSyncPlaza}
                disabled={plazaSyncing}
                className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer disabled:opacity-60"
              >
                <RefreshCw
                  size={12}
                  className={plazaSyncing ? "animate-spin" : ""}
                />
                <span>{plazaSyncing ? "多源采集中..." : "立即同步热榜"}</span>
              </button>
            </div>
          ) : (
            <div className="flex items-center gap-2">
              <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full border z-pill-info font-mono-num">
                <Database size={11} />
                <span>
                  {hydratedIdb
                    ? `IndexedDB 本地已保存 ${results.length} 项`
                    : "读取本地画廊中..."}
                </span>
              </span>
              {results.length > 0 && (
                <button
                  type="button"
                  onClick={handleClearGallery}
                  className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl border z-border z-surface z-ghost-btn cursor-pointer"
                >
                  <Trash2 size={12} />
                  <span>清空画廊</span>
                </button>
              )}
            </div>
          )}
        </div>

        {error && (
          <NoticeBanner
            type="error"
            text={error}
            onClose={() => setError(null)}
          />
        )}
        {notice && (
          <NoticeBanner
            type={notice.type}
            text={notice.text}
            onClose={() => setNotice(null)}
          />
        )}

        {/* TAB 1: PROMPT PLAZA */}
        {activeTab === "plaza" && (
          <div className="space-y-4">
            {/* Category Chips & Search / Source / Sort Toolbar */}
            <div className="p-card rounded-2xl p-3 space-y-2.5">
              <div className="flex flex-wrap items-center gap-1.5">
                {PLAZA_CATEGORIES.map((cat) => {
                  const active = plazaCategory === cat;
                  const count =
                    cat === "全部"
                      ? plazaCategories["all"] ?? plazaItems.length
                      : cat === "⭐ 我的收藏"
                        ? favoriteIds.length
                        : plazaCategories[cat] ?? 0;
                  return (
                    <button
                      key={cat}
                      type="button"
                      onClick={() => setPlazaCategory(cat)}
                      className={`inline-flex items-center gap-1 px-2.5 py-1 rounded-full border cursor-pointer transition-colors ${
                        active
                          ? "z-primary-btn border-transparent font-medium"
                          : "z-border z-canvas z-text-sec z-ghost-btn"
                      }`}
                    >
                      <span>{cat}</span>
                      <span className="font-mono-num opacity-75">{count}</span>
                    </button>
                  );
                })}
              </div>

              <div className="flex flex-wrap items-center gap-1.5 pt-2 border-t z-border">
                <span className="z-text-muted text-[11px] mr-0.5">拉取源:</span>
                {SOURCE_OPTIONS.map((opt) => {
                  const active = plazaSource === opt.value;
                  const count =
                    opt.value === "all"
                      ? plazaSources["all"] ??
                        syncStatus?.total_count ??
                        plazaItems.length
                      : plazaSources[opt.value] ?? 0;
                  return (
                    <button
                      key={opt.value}
                      type="button"
                      onClick={() => setPlazaSource(opt.value)}
                      className={`inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full border text-[11px] cursor-pointer transition-colors ${
                        active
                          ? "z-primary-btn border-transparent font-medium"
                          : "z-border z-canvas z-text-sec z-ghost-btn"
                      } ${!active && opt.value !== "all" && count === 0 ? "opacity-60" : ""}`}
                    >
                      <span>{opt.label}</span>
                      <span className="font-mono-num opacity-75">{count}</span>
                    </button>
                  );
                })}
              </div>

              <div className="flex flex-wrap items-center gap-2 pt-2 border-t z-border">
                <div className="relative flex-1 min-w-[180px]">
                  <Search
                    size={13}
                    className="absolute left-2.5 top-1/2 -translate-y-1/2 z-text-muted pointer-events-none"
                  />
                  <input
                    type="text"
                    value={plazaQuery}
                    onChange={(e) => setPlazaQuery(e.target.value)}
                    placeholder="搜索主题、材质、风格关键词..."
                    className="w-full pl-8 pr-3 py-1.5 rounded-xl p-input"
                  />
                </div>

                <div className="p-surface p-0.5 rounded-xl inline-flex items-center gap-0.5">
                  <button
                    type="button"
                    onClick={() => setPlazaSort("popular")}
                    className={`px-2.5 py-1 rounded-lg cursor-pointer ${
                      plazaSort === "popular"
                        ? "z-primary-btn font-medium"
                        : "z-text-muted z-ghost-btn"
                    }`}
                  >
                    热门度
                  </button>
                  <button
                    type="button"
                    onClick={() => setPlazaSort("latest")}
                    className={`px-2.5 py-1 rounded-lg cursor-pointer ${
                      plazaSort === "latest"
                        ? "z-primary-btn font-medium"
                        : "z-text-muted z-ghost-btn"
                    }`}
                  >
                    最新
                  </button>
                </div>
              </div>
            </div>

            {plazaLoading && plazaItems.length === 0 ? (
              <div className="p-card rounded-2xl p-10 flex flex-col items-center justify-center gap-2 z-text-muted">
                <Loader2 size={20} className="animate-spin" />
                <span>正在加载热门提示词广场...</span>
              </div>
            ) : filteredPlazaItems.length === 0 ? (
              <div className="p-card rounded-2xl p-10 flex flex-col items-center justify-center text-center gap-2">
                <Star size={20} className="z-text-muted" />
                <div className="text-sm font-semibold">暂无匹配的提示词</div>
                <div className="z-text-muted max-w-md">
                  {plazaCategory === "⭐ 我的收藏"
                    ? "还没有收藏过提示词卡片，点击卡片右上角星标即可收藏到这里。"
                    : "尝试更换搜索关键词或切换分类筛选，也可以点击右上角「立即同步热榜」采集更多社区灵感。"}
                </div>
              </div>
            ) : (
              <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-4">
                {filteredPlazaItems.map((item) => (
                  <PlazaPromptCard
                    key={item.id}
                    item={item}
                    isFavorite={favoriteIds.includes(item.id)}
                    onToggleFavorite={handleToggleFavorite}
                    onApply={handleApplyCard}
                    onQuickGenerate={handleQuickGenerate}
                  />
                ))}
              </div>
            )}
          </div>
        )}

        {/* TAB 2: LOCAL INDEXEDDB GALLERY */}
        {activeTab === "gallery" && (
          <>
            {results.length === 0 && !loading ? (
              <div className="flex-1 p-card rounded-2xl flex flex-col items-center justify-center p-8 text-center gap-3">
                <div className="w-12 h-12 rounded-2xl z-muted border z-border flex items-center justify-center z-text-muted">
                  <ImageIcon size={22} />
                </div>
                <div className="text-sm font-semibold">创作画廊准备就绪</div>
                <div className="z-text-muted max-w-md leading-relaxed">
                  可先切到「提示词广场」一键做同款或立即生成；生成的产物将以二进制 Blob 持久存入浏览器{" "}
                  <code className="font-mono-num">IndexedDB</code>，刷新或关闭标签页也不会丢失。
                </div>
                <button
                  type="button"
                  onClick={() => setActiveTab("plaza")}
                  className="inline-flex items-center gap-1.5 px-3.5 py-2 rounded-xl z-primary-btn font-medium cursor-pointer mt-1"
                >
                  <Flame size={13} />
                  <span>逛逛提示词广场</span>
                </button>
              </div>
            ) : (
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
                {loading && (
                  <div className="p-card rounded-2xl overflow-hidden flex flex-col">
                    <div className="aspect-square z-muted flex flex-col items-center justify-center gap-2 z-text-muted">
                      <Loader2 size={24} className="animate-spin" />
                      <span className="font-medium">正在绘制中...</span>
                    </div>
                    <div className="p-3 space-y-1.5">
                      <div className="font-mono-num z-text-muted">
                        {model} · {size.replace("x", "×")}
                      </div>
                      <p className="line-clamp-2 z-text-sec">{prompt}</p>
                    </div>
                  </div>
                )}
                {results.map((item) => (
                  <div
                    key={item.id}
                    className="p-card rounded-2xl overflow-hidden flex flex-col group"
                  >
                    <div
                      className="relative aspect-square z-muted cursor-pointer overflow-hidden"
                      onClick={() => setLightboxItem(item)}
                    >
                      <img
                        src={item.url}
                        alt={item.prompt}
                        className="w-full h-full object-cover transition-transform duration-300 group-hover:scale-[1.02]"
                      />
                      <div className="absolute inset-x-0 top-0 p-2 flex items-center justify-between gap-2 bg-gradient-to-b from-black/50 to-transparent text-white">
                        <span className="px-2 py-0.5 rounded-md bg-black/55 backdrop-blur-xs font-mono-num truncate max-w-[170px]">
                          {item.model}
                        </span>
                        <button
                          type="button"
                          onClick={(e) => {
                            e.stopPropagation();
                            setLightboxItem(item);
                          }}
                          className="p-1.5 rounded-lg bg-black/55 hover:bg-black/75 text-white cursor-pointer"
                          title="放大检视与 Remix"
                        >
                          <Maximize2 size={13} />
                        </button>
                      </div>
                    </div>

                    <div className="p-3 space-y-2">
                      <div className="flex items-center justify-between font-mono-num z-text-muted">
                        <span>{item.size.replace("x", "×")}</span>
                        <span>
                          {item.latencyMs
                            ? `${(item.latencyMs / 1000).toFixed(2)}s · `
                            : ""}
                          {item.createdAt}
                        </span>
                      </div>

                      <p className="line-clamp-2 z-text-sec leading-relaxed">
                        {item.revisedPrompt || item.prompt}
                      </p>

                      <div className="flex items-center gap-1.5 pt-1">
                        <a
                          href={item.url}
                          download={`${item.id}.png`}
                          target="_blank"
                          rel="noreferrer"
                          className="flex-1 inline-flex items-center justify-center gap-1 py-1.5 rounded-xl border z-border z-ghost-btn font-medium"
                        >
                          <Download size={12} />
                          <span>下载原图</span>
                        </a>
                        <button
                          type="button"
                          onClick={() => handleRemix(item)}
                          className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl border z-border z-ghost-btn cursor-pointer"
                          title="复用该条 Prompt 与模型参数"
                        >
                          <Repeat2 size={12} />
                          <span>Remix</span>
                        </button>
                        <button
                          type="button"
                          onClick={() => handleCopyPromptOrUrl(item)}
                          className="inline-flex items-center gap-1 px-2 py-1.5 rounded-xl border z-border z-ghost-btn cursor-pointer"
                          title="复制链接或 Prompt"
                        >
                          {copiedCardId === item.id ? (
                            <Check size={12} />
                          ) : (
                            <Copy size={12} />
                          )}
                        </button>
                        <button
                          type="button"
                          onClick={() => handleDeleteOne(item)}
                          className="p-1.5 rounded-xl border z-border z-ghost-btn cursor-pointer"
                          style={{ color: "var(--pill-danger-text)" }}
                          title="从 IndexedDB 画廊删除"
                        >
                          <Trash2 size={12} />
                        </button>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </>
        )}
      </div>

      {/* Split Metadata Lightbox Modal */}
      {lightboxItem && (
        <div
          className="fixed inset-0 z-50 bg-black/75 backdrop-blur-sm flex items-center justify-center p-4"
          onClick={() => setLightboxItem(null)}
        >
          <div
            className="p-surface z-text rounded-2xl max-w-4xl w-full overflow-hidden shadow-2xl grid grid-cols-1 md:grid-cols-[1.25fr_1fr]"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="studio-grid-bg p-4 flex items-center justify-center min-h-[280px] max-h-[82vh]">
              <img
                src={lightboxItem.url}
                alt={lightboxItem.prompt}
                className="max-h-[74vh] w-auto object-contain rounded-xl border z-border"
              />
            </div>

            <div className="p-5 flex flex-col justify-between gap-4 border-t md:border-t-0 md:border-l z-border text-xs">
              <div className="space-y-4">
                <div className="flex items-start justify-between gap-2 pb-3 border-b z-border">
                  <div>
                    <div className="font-mono-num uppercase tracking-wider z-text-muted">
                      产物详情 · {lightboxItem.id}
                    </div>
                    <h3 className="text-sm font-semibold font-mono-num mt-0.5">
                      {lightboxItem.model}
                    </h3>
                  </div>
                  <button
                    type="button"
                    onClick={() => setLightboxItem(null)}
                    className="p-1 rounded-lg border z-border z-ghost-btn cursor-pointer"
                    aria-label="关闭大图检视"
                  >
                    <X size={14} />
                  </button>
                </div>

                <div className="space-y-1.5">
                  <div className="z-text-muted font-medium">Prompt 提示词</div>
                  <div
                    className="p-3 rounded-xl z-text-sec leading-relaxed max-h-40 overflow-y-auto"
                    style={{
                      background: "var(--bg-input)",
                      border: "1px solid var(--hairline)",
                    }}
                  >
                    {lightboxItem.revisedPrompt || lightboxItem.prompt}
                  </div>
                </div>

                <div className="grid grid-cols-2 gap-2.5">
                  <div
                    className="p-2.5 rounded-xl"
                    style={{
                      background: "var(--bg-input)",
                      border: "1px solid var(--hairline)",
                    }}
                  >
                    <div className="z-text-muted">画幅分辨率</div>
                    <div className="font-mono-num font-semibold mt-0.5">
                      {lightboxItem.size.replace("x", "×")}
                    </div>
                  </div>
                  <div
                    className="p-2.5 rounded-xl"
                    style={{
                      background: "var(--bg-input)",
                      border: "1px solid var(--hairline)",
                    }}
                  >
                    <div className="z-text-muted">耗时 / 生成时间</div>
                    <div className="font-mono-num font-semibold mt-0.5">
                      {lightboxItem.latencyMs
                        ? `${(lightboxItem.latencyMs / 1000).toFixed(2)}s · `
                        : ""}
                      {lightboxItem.createdAt}
                    </div>
                  </div>
                </div>
              </div>

              <div className="flex items-center gap-2 pt-3 border-t z-border">
                <button
                  type="button"
                  onClick={() => handleRemix(lightboxItem)}
                  className="flex-1 inline-flex items-center justify-center gap-1.5 py-2.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer"
                >
                  <Repeat2 size={13} />
                  <span>复用参数 Remix</span>
                </button>
                <a
                  href={lightboxItem.url}
                  download={`${lightboxItem.id}.png`}
                  target="_blank"
                  rel="noreferrer"
                  className="flex-1 inline-flex items-center justify-center gap-1.5 py-2.5 rounded-xl z-primary-btn font-medium"
                >
                  <Download size={13} />
                  <span>下载 PNG 原图</span>
                </a>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
