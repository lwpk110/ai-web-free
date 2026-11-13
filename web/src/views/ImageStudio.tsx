import React, { useEffect, useRef, useState } from "react";
import {
  Check,
  Copy,
  Database,
  Download,
  Image as ImageIcon,
  Layers,
  Loader2,
  Maximize2,
  Repeat2,
  Sparkles,
  Trash2,
  UploadCloud,
  Wand2,
  X,
} from "lucide-react";
import {
  editImageWithReferences,
  fetchAccounts,
  generateImage,
} from "../api/client";
import { NoticeBanner } from "../components/ui";
import {
  base64ToBlob,
  clearStoredImages,
  dataUrlToBlob,
  deleteStoredImage,
  hydrateStoredImage,
  loadStoredImages,
  saveStoredImage,
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

const PROMPT_PRESETS = [
  {
    label: "极简玻璃温室",
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

export const ImageStudio: React.FC = () => {
  const [mode, setMode] = useState<"generations" | "edits">("generations");
  const [prompt, setPrompt] = useState(PROMPT_PRESETS[0].prompt);
  const [model, setModel] = useState("gpt-image-2.5-sunburst");
  const [size, setSize] = useState("1024x1024");
  const [quality, setQuality] = useState("hd");
  const [files, setFiles] = useState<File[]>([]);
  const [filePreviews, setFilePreviews] = useState<string[]>([]);
  const [apiKey, setApiKey] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
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

  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const objectUrlsRef = useRef<Set<string>>(new Set());

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

    return () => {
      alive = false;
      objectUrlsRef.current.forEach((u) => URL.revokeObjectURL(u));
      objectUrlsRef.current.clear();
    };
  }, []);

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
    latencyMs: number
  ): Promise<HydratedImageItem | null> => {
    let blob: Blob | undefined;
    let remoteUrl: string | undefined;

    if (item.b64_json) {
      blob = base64ToBlob(item.b64_json) || undefined;
    } else if (item.url?.startsWith("data:")) {
      blob = dataUrlToBlob(item.url) || undefined;
    } else if (item.url) {
      remoteUrl = item.url;
      // Best-effort local blob caching if CORS allows
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
      prompt,
      revisedPrompt: item.revised_prompt,
      model,
      size,
      quality,
      mode,
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

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!prompt.trim() || loading) return;
    if (mode === "edits" && files.length === 0) {
      setError("请至少上传 1 张参考图像用于图生图编辑");
      return;
    }

    setLoading(true);
    setError(null);
    const started = performance.now();

    try {
      const res =
        mode === "edits"
          ? await editImageWithReferences({
              prompt,
              model,
              size,
              files,
              apiKey: apiKey.trim() || undefined,
            })
          : await generateImage({
              prompt,
              model,
              size,
              quality,
              apiKey: apiKey.trim() || undefined,
            });

      const latencyMs = Math.round(performance.now() - started);
      const saved = (
        await Promise.all(
          (res.data || []).map((item, idx) =>
            persistSingleResult(item, idx, latencyMs)
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
    if (MODEL_CARDS.some((m) => m.id === item.model)) {
      setModel(item.model);
    }
    if (ASPECT_OPTIONS.some((a) => a.value === item.size)) {
      setSize(item.size);
    }
    setLightboxItem(null);
  };

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
            <label className="font-medium z-text-sec block">生图模型档位</label>
            <div className="space-y-1.5">
              {MODEL_CARDS.map((m) => {
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

          {/* Prompt */}
          <div className="space-y-1.5">
            <label className="font-medium z-text-sec block">Prompt 提示词</label>
            <div className="flex flex-wrap gap-1.5">
              {PROMPT_PRESETS.map((p) => (
                <button
                  key={p.label}
                  type="button"
                  onClick={() => setPrompt(p.prompt)}
                  className="px-2.5 py-1 rounded-full border z-border z-canvas z-ghost-btn whitespace-nowrap cursor-pointer"
                >
                  {p.label}
                </button>
              ))}
            </div>
            <textarea
              rows={4}
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder="描述画面主体、光影与构图..."
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

      {/* Right Dot-Grid Studio Gallery */}
      <div className="studio-grid-bg overflow-y-auto p-5 flex flex-col gap-4 min-h-0 text-xs">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="text-sm font-semibold">持久画廊与产物库</h2>
            <span
              className={`inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full border font-medium ${
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
            <span className="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full border z-pill-info font-mono-num">
              <Database size={11} />
              <span>
                {hydratedIdb
                  ? `IndexedDB 本地已保存 ${results.length} 项`
                  : "读取本地画廊中..."}
              </span>
            </span>
          </div>

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

        {error && (
          <NoticeBanner
            type="error"
            text={error}
            onClose={() => setError(null)}
          />
        )}

        {results.length === 0 ? (
          <div className="flex-1 p-card rounded-2xl flex flex-col items-center justify-center p-8 text-center gap-3">
            <div className="w-12 h-12 rounded-2xl z-muted border z-border flex items-center justify-center z-text-muted">
              <ImageIcon size={22} />
            </div>
            <div className="text-sm font-semibold">创作画廊准备就绪</div>
            <div className="z-text-muted max-w-md leading-relaxed">
              在左侧选择模型档位、画幅比例或上传参考图点击生成；生成的产物将以二进制 Blob 持久存入浏览器{" "}
              <code className="font-mono-num">IndexedDB</code>，刷新或关闭标签页也不会丢失。
            </div>
          </div>
        ) : (
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
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
