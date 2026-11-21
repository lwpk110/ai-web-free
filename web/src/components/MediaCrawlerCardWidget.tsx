import React, { useEffect, useRef, useState } from "react";
import {
  AudioLines,
  CheckCircle2,
  Download,
  ExternalLink,
  Film,
  Globe,
  Image as ImageIcon,
  KeyRound,
  Loader2,
  Pause,
  Play,
  Search,
  Sparkles,
} from "lucide-react";
import {
  CrawledMediaCandidate,
  CrawlerMediaKind,
  UnifiedMediaCrawlResponse,
  UnifiedMediaImportResponse,
  VoiceDescriptor,
  crawlMedia,
  fetchVoicePreview,
  importCrawledMedia,
} from "../api/client";
import { toThumbImageUrl } from "../lib/mediaUrl";
import { CloudKeyConfigModal } from "./CloudKeyConfigModal";

interface MediaCrawlerCardWidgetProps {
  initialQuery?: string;
  initialUrl?: string;
  initialKind?: CrawlerMediaKind;
  initialRole?: string;
}

const SHORT_DRAMA_ROLES = [
  "",
  "霸总男主",
  "大女主",
  "甜宠千金",
  "冷酷反派",
  "悬疑旁白",
  "热血战神",
  "character",
  "scene",
];

export function MediaCrawlerCardWidget({
  initialQuery = "",
  initialUrl = "",
  initialKind = "auto",
  initialRole = "",
}: MediaCrawlerCardWidgetProps): React.JSX.Element {
  const [query, setQuery] = useState(initialQuery);
  const [targetUrl, setTargetUrl] = useState(initialUrl);
  const [kind, setKind] = useState<CrawlerMediaKind>(initialKind);
  const [loading, setLoading] = useState(false);
  const [report, setReport] = useState<UnifiedMediaCrawlResponse | null>(null);
  const [error, setError] = useState<string | null>(null);

  // 每个候选卡片的局部编辑状态与导入结果
  const [edits, setEdits] = useState<
    Record<string, { title: string; role: string; transcript: string; cloneEngine: "auto" | "siliconflow" | "dashscope" }>
  >({});
  const [importingUrl, setImportingUrl] = useState<string | null>(null);
  const [importedMap, setImportedMap] = useState<Record<string, UnifiedMediaImportResponse>>({});
  const [keyModalOpen, setKeyModalOpen] = useState(false);
  const [auditioningId, setAuditioningId] = useState<string | null>(null);
  const auditionAudioRef = useRef<HTMLAudioElement | null>(null);
  const auditionCtrlRef = useRef<AbortController | null>(null);
  const auditionUrlRef = useRef<string | null>(null);

  const stopAudition = () => {
    auditionCtrlRef.current?.abort();
    auditionCtrlRef.current = null;
    if (auditionAudioRef.current) {
      auditionAudioRef.current.pause();
      auditionAudioRef.current.removeAttribute("src");
    }
    if (auditionUrlRef.current) {
      URL.revokeObjectURL(auditionUrlRef.current);
      auditionUrlRef.current = null;
    }
    setAuditioningId(null);
  };

  useEffect(() => () => stopAudition(), []);

  const handleAuditionVoice = async (v: VoiceDescriptor) => {
    if (auditioningId === v.id) {
      stopAudition();
      return;
    }
    stopAudition();
    const ctrl = new AbortController();
    auditionCtrlRef.current = ctrl;
    setAuditioningId(v.id);
    setError(null);
    try {
      const res = await fetchVoicePreview(v.id, v.preview_url, ctrl.signal);
      if (ctrl.signal.aborted) return;
      const objUrl = URL.createObjectURL(res.blob);
      auditionUrlRef.current = objUrl;
      if (auditionAudioRef.current) {
        auditionAudioRef.current.src = objUrl;
        await auditionAudioRef.current.play();
      }
    } catch (e) {
      if (!(e instanceof DOMException && e.name === "AbortError")) {
        setError(e instanceof Error ? `试听失败: ${e.message}` : "试听失败");
      }
      if (auditionCtrlRef.current === ctrl) {
        setAuditioningId(null);
      }
    }
  };

  const runCrawl = async (qStr = query, uStr = targetUrl, kVal = kind) => {
    if (!qStr.trim() && !uStr.trim()) {
      setError("请输入搜索关键词（如：霸总低沉男声、古风大女主立绘）或粘贴目标网页 URL");
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const res = await crawlMedia({
        query: qStr.trim(),
        url: uStr.trim(),
        media_kind: kVal,
        limit: 12,
      });
      setReport(res);
      const nextEdits: typeof edits = {};
      for (const item of res.candidates || []) {
        nextEdits[item.media_url] = {
          title: item.title || "网页采集素材",
          role: item.suggested_role || initialRole || "",
          transcript: item.transcript || item.title || "",
          cloneEngine: "auto",
        };
      }
      setEdits(nextEdits);
    } catch (e) {
      setError(e instanceof Error ? e.message : "检索爬虫请求失败");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (initialQuery.trim() || initialUrl.trim()) {
      void runCrawl(initialQuery, initialUrl, initialKind);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleImportCandidate = async (item: CrawledMediaCandidate) => {
    const cfg = edits[item.media_url] || {
      title: item.title,
      role: item.suggested_role || initialRole || "",
      transcript: item.transcript || "",
      cloneEngine: "auto" as const,
    };
    setImportingUrl(item.media_url);
    setError(null);
    try {
      const res = await importCrawledMedia({
        media_kind: item.media_kind,
        url: item.media_url,
        source_page: item.source_page,
        title: cfg.title.trim() || item.title,
        role: cfg.role.trim() || undefined,
        transcript: cfg.transcript.trim() || undefined,
        gender: item.gender,
        locale: item.locale,
        tags: item.tags,
        clone_engine: item.media_kind === "voice" ? cfg.cloneEngine : undefined,
      });
      setImportedMap((prev) => ({ ...prev, [item.media_url]: res }));
    } catch (e) {
      setError(e instanceof Error ? e.message : "缓存入库失败");
    } finally {
      setImportingUrl(null);
    }
  };

  const existingVoices = report?.existing_voices || [];
  const existingImages = report?.existing_images || [];
  const existingVideos = report?.existing_videos || [];
  const candidates = report?.candidates || [];

  return (
    <div className="p-card rounded-2xl border z-border p-4 space-y-3.5 text-xs shadow-sm">
      <audio
        ref={auditionAudioRef}
        onEnded={() => setAuditioningId(null)}
        onError={() => setAuditioningId(null)}
        className="hidden"
      />
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <Globe className="w-4 h-4 text-primary" />
          <span className="font-semibold text-sm z-text">
            全工坊网络爬虫与系统素材库缓存工具
          </span>
          <span className="px-2 py-0.5 rounded-full text-[10px] font-medium bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border border-emerald-500/20">
            先查系统缓存 · 不足再网爬入库复用
          </span>
        </div>
        <button
          type="button"
          onClick={() => setKeyModalOpen(true)}
          className="inline-flex items-center gap-1 px-2.5 py-1 rounded-md border z-border hover:bg-muted/60 text-[11px] z-text-muted hover:z-text"
        >
          <KeyRound className="w-3 h-3 text-primary" />
          <span>配置云端克隆/生图/视频 Key</span>
        </button>
      </div>

      {/* 搜索与网页抓取表单 */}
      <div className="grid grid-cols-1 md:grid-cols-12 gap-2">
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="关键词（如：低沉霸总男声 / 古风女主立绘 / 都市夜景视频）"
          className="md:col-span-5 px-2.5 py-1.5 rounded-lg border z-border bg-background z-text focus:outline-none focus:ring-1 focus:ring-primary"
        />
        <input
          type="url"
          value={targetUrl}
          onChange={(e) => setTargetUrl(e.target.value)}
          placeholder="可选：直接指定网页或 .mp3/.png/.mp4 源 URL 抓取"
          className="md:col-span-4 px-2.5 py-1.5 rounded-lg border z-border bg-background z-text font-mono focus:outline-none focus:ring-1 focus:ring-primary"
        />
        <select
          value={kind}
          onChange={(e) => setKind(e.target.value as CrawlerMediaKind)}
          className="md:col-span-1 px-2 py-1.5 rounded-lg border z-border bg-background z-text"
        >
          <option value="auto">全部</option>
          <option value="voice">音色</option>
          <option value="image">图片</option>
          <option value="video">视频</option>
        </select>
        <button
          type="button"
          onClick={() => void runCrawl()}
          disabled={loading}
          className="md:col-span-2 px-3 py-1.5 rounded-lg z-primary-btn font-medium inline-flex items-center justify-center gap-1.5 disabled:opacity-50"
        >
          {loading ? (
            <Loader2 className="w-3.5 h-3.5 animate-spin" />
          ) : (
            <Search className="w-3.5 h-3.5" />
          )}
          <span>{loading ? "检索抓取中" : "搜索/抓取"}</span>
        </button>
      </div>

      {error && (
        <div className="px-3 py-2 rounded-lg bg-destructive/10 text-destructive border border-destructive/20">
          {error}
        </div>
      )}

      {/* 1. 现有系统缓存库匹配结果 */}
      {(existingVoices.length > 0 || existingImages.length > 0 || existingVideos.length > 0) && (
        <div className="rounded-xl border z-border bg-muted/30 p-3 space-y-2">
          <div className="font-medium z-text flex items-center gap-1.5">
            <Sparkles className="w-3.5 h-3.5 text-amber-500" />
            <span>
              当前系统库已命中（可直接使用，如不满意可继续选用下方爬虫结果）：
              语音 {existingVoices.length} · 图像 {existingImages.length} · 视频 {existingVideos.length}
            </span>
          </div>
          {existingVoices.length > 0 && (
            <div className="flex flex-wrap gap-1.5">
              {existingVoices.slice(0, 6).map((v) => {
                const isAud = auditioningId === v.id;
                const canAud = v.available !== false || v.cached_local;
                return (
                  <div
                    key={v.id}
                    className="inline-flex items-center gap-1.5 px-2 py-1 rounded-lg border z-border z-surface text-[11px]"
                    title={v.sample_text || v.id}
                  >
                    <AudioLines className="w-3 h-3 text-purple-500 shrink-0" />
                    <a href="#/tts" className="hover:underline z-text font-medium truncate max-w-[160px]">
                      {v.name}
                    </a>
                    {v.cached_local && (
                      <span className="text-[10px] text-emerald-600 dark:text-emerald-400 shrink-0">已缓存</span>
                    )}
                    {canAud && (
                      <button
                        type="button"
                        onClick={() => void handleAuditionVoice(v)}
                        className={`inline-flex items-center gap-0.5 px-1.5 py-0.5 rounded text-[10px] cursor-pointer transition-colors ${
                          isAud ? "p-seg-active font-medium" : "bg-muted hover:bg-indigo-500/10 text-primary"
                        }`}
                        title={isAud ? "停止试听" : "直接试听系统音色"}
                      >
                        {isAud ? <Pause className="w-2.5 h-2.5" /> : <Play className="w-2.5 h-2.5" />}
                        <span>{isAud ? "停" : "试听"}</span>
                      </button>
                    )}
                  </div>
                );
              })}
            </div>
          )}
          {existingImages.length > 0 && (
            <div className="flex flex-wrap gap-1.5">
              {existingImages.slice(0, 4).map((img) => (
                <a
                  key={img.id}
                  href="#/images"
                  className="inline-flex items-center gap-1.5 px-2 py-1 rounded-md border z-border bg-background hover:border-primary/40 text-[11px]"
                >
                  <ImageIcon className="w-3 h-3 text-sky-500" />
                  <span className="max-w-[160px] truncate">{img.prompt}</span>
                </a>
              ))}
            </div>
          )}
        </div>
      )}

      {/* 2. 网页爬虫采集候选与一键缓存克隆/入库 */}
      {candidates.length > 0 ? (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-2.5">
          {candidates.map((item) => {
            const cfg = edits[item.media_url] || {
              title: item.title,
              role: item.suggested_role || "",
              transcript: item.transcript || "",
              cloneEngine: "auto" as const,
            };
            const imported = importedMap[item.media_url];
            const isImporting = importingUrl === item.media_url;

            return (
              <div
                key={item.media_url}
                className="rounded-xl border z-border bg-background/80 p-3 space-y-2 flex flex-col justify-between"
              >
                <div className="space-y-1.5">
                  <div className="flex items-center justify-between gap-2">
                    <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded bg-primary/10 text-primary text-[10px] font-medium">
                      {item.media_kind === "voice" && <AudioLines className="w-3 h-3" />}
                      {item.media_kind === "image" && <ImageIcon className="w-3 h-3" />}
                      {item.media_kind === "video" && <Film className="w-3 h-3" />}
                      {item.media_kind === "voice"
                        ? "音色样本"
                        : item.media_kind === "image"
                          ? "参考图像"
                          : "视频片段"}
                    </span>
                    <span className="text-[10px] z-text-muted font-mono truncate max-w-[160px]">
                      {item.source_domain}
                    </span>
                  </div>

                  {/* 内联预览 */}
                  {item.media_kind === "voice" && (
                    <audio controls preload="none" src={item.media_url} className="w-full h-8" />
                  )}
                  {item.media_kind === "image" && (
                    <img
                      src={toThumbImageUrl(item.media_url, 480)}
                      alt={item.title}
                      className="w-full h-32 object-cover rounded-lg bg-muted/40"
                      loading="lazy"
                      decoding="async"
                    />
                  )}
                  {item.media_kind === "video" && (
                    <video
                      controls
                      preload="metadata"
                      src={item.media_url}
                      className="w-full h-32 rounded-lg bg-black object-cover"
                    />
                  )}

                  {/* 元数据编辑（命名与剧组标签） */}
                  <div className="grid grid-cols-2 gap-1.5">
                    <input
                      type="text"
                      value={cfg.title}
                      onChange={(e) =>
                        setEdits((prev) => ({
                          ...prev,
                          [item.media_url]: { ...cfg, title: e.target.value },
                        }))
                      }
                      placeholder="系统库展示名称"
                      className="px-2 py-1 rounded border z-border bg-background z-text text-[11px]"
                    />
                    <select
                      value={cfg.role}
                      onChange={(e) =>
                        setEdits((prev) => ({
                          ...prev,
                          [item.media_url]: { ...cfg, role: e.target.value },
                        }))
                      }
                      className="px-2 py-1 rounded border z-border bg-background z-text text-[11px]"
                    >
                      {SHORT_DRAMA_ROLES.map((r) => (
                        <option key={r || "none"} value={r}>
                          {r ? `短剧角色: ${r}` : "常规/通用素材"}
                        </option>
                      ))}
                    </select>
                  </div>

                  {item.media_kind === "voice" && (
                    <div className="grid grid-cols-3 gap-1.5">
                      <input
                        type="text"
                        value={cfg.transcript}
                        onChange={(e) =>
                          setEdits((prev) => ({
                            ...prev,
                            [item.media_url]: { ...cfg, transcript: e.target.value },
                          }))
                        }
                        placeholder="参考台词文本（帮助 CosyVoice2 精准复刻）"
                        className="col-span-2 px-2 py-1 rounded border z-border bg-background z-text text-[11px]"
                      />
                      <select
                        value={cfg.cloneEngine}
                        onChange={(e) =>
                          setEdits((prev) => ({
                            ...prev,
                            [item.media_url]: {
                              ...cfg,
                              cloneEngine: e.target.value as "auto" | "siliconflow" | "dashscope",
                            },
                          }))
                        }
                        className="px-1.5 py-1 rounded border z-border bg-background z-text text-[11px]"
                      >
                        <option value="auto">自动云端克隆</option>
                        <option value="siliconflow">硅基流动克隆</option>
                        <option value="dashscope">阿里百炼克隆</option>
                      </select>
                    </div>
                  )}
                </div>

                <div className="pt-1 flex flex-wrap items-center justify-between gap-2">
                  {imported ? (
                    <div className="flex flex-wrap items-center justify-between w-full gap-1.5 text-[11px]">
                      <span className="inline-flex items-center gap-1 text-emerald-600 dark:text-emerald-400 font-medium">
                        <CheckCircle2 className="w-3.5 h-3.5" />
                        {imported.media_kind === "voice"
                          ? imported.status === "cloned"
                            ? `已入库并云端克隆 (${imported.id})`
                            : `样音已缓存入库（需 API Key 完成云端克隆）`
                          : `已缓存写入系统库 (${imported.id})`}
                      </span>
                      <div className="flex items-center gap-1.5">
                        {imported.media_kind === "voice" && imported.status !== "cloned" && (
                          <button
                            type="button"
                            onClick={() => setKeyModalOpen(true)}
                            className="px-2 py-0.5 rounded border border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400"
                          >
                            填 Key 复刻
                          </button>
                        )}
                        <a
                          href={imported.target_view || "#/short-drama"}
                          className="inline-flex items-center gap-0.5 px-2 py-0.5 rounded bg-primary/10 text-primary hover:bg-primary/20"
                        >
                          <span>去工坊复用</span>
                          <ExternalLink className="w-3 h-3" />
                        </a>
                      </div>
                    </div>
                  ) : (
                    <>
                      <a
                        href={item.source_page || item.media_url}
                        target="_blank"
                        rel="noreferrer"
                        className="text-[10px] z-text-muted hover:z-text inline-flex items-center gap-0.5"
                      >
                        来源链接 <ExternalLink className="w-2.5 h-2.5" />
                      </a>
                      <button
                        type="button"
                        onClick={() => void handleImportCandidate(item)}
                        disabled={isImporting}
                        className="px-2.5 py-1 rounded-lg z-primary-btn text-[11px] font-medium inline-flex items-center gap-1 disabled:opacity-50"
                      >
                        {isImporting ? (
                          <Loader2 className="w-3 h-3 animate-spin" />
                        ) : (
                          <Download className="w-3 h-3" />
                        )}
                        <span>
                          {item.media_kind === "voice"
                            ? "缓存本地克隆并入库"
                            : item.media_kind === "image"
                              ? "缓存至系统图库"
                              : "缓存至系统视频库"}
                        </span>
                      </button>
                    </>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      ) : (
        report &&
        !loading && (
          <div className="py-4 text-center z-text-muted border border-dashed z-border rounded-xl">
            暂未发现公开直链候选，建议在上方 URL 框粘贴包含试听音频/图片/视频的具体网页地址直接提取。
          </div>
        )
      )}

      {report?.warnings && report.warnings.length > 0 && (
        <div className="text-[10px] z-text-muted space-y-0.5">
          {report.warnings.map((w, i) => (
            <div key={i}>• {w}</div>
          ))}
        </div>
      )}

      <CloudKeyConfigModal
        open={keyModalOpen}
        onClose={() => setKeyModalOpen(false)}
        initialProvider="siliconflow"
        allowedProviders={["siliconflow", "dashscope", "jimeng", "meta_muse", "local_tts"]}
      />
    </div>
  );
}
