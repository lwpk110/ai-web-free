import React, { useEffect, useState, useMemo } from "react";
import {
  Flame,
  Clock,
  RefreshCw,
  ExternalLink,
  Search,
  Sparkles,
  Tag,
  ThumbsUp,
  MessageSquare,
  Eye,
  Copy,
  Check,
  Radio,
  ArrowUpRight,
  LayoutGrid,
  List,
  Compass,
  X,
  BookOpenText,
  Inbox,
  ArrowDown,
  ShieldAlert,
  MessageCircle,
  Send,
  Globe2,
  UserRound,
  Bot,
  Trash2,
} from "lucide-react";
import {
  fetchDeals,
  postDealsChat,
  refreshDeals,
  fetchDealsStatus,
  type DealItem,
  type DealsStatus,
} from "../api/client";
import { MarkdownContent } from "../components/MarkdownContent";
import {
  getDealTagGroup,
  groupDealTags,
  matchesDealTags,
  normalizeDealTag,
  type DealTagGroupId,
  type SelectedDealTags,
} from "../lib/deals-tags";

const isExampleDeal = (deal: DealItem) => deal.id.startsWith("seed-");
const formatHeatScore = (score: number) => score > 0 ? score.toFixed(1) : "—";
const formatMetric = (value: number) => value > 0 ? value.toLocaleString() : "—";

type DealsChatMessage = { id: string; role: "user" | "assistant"; content: string };

export function DealsView() {
  const [deals, setDeals] = useState<DealItem[]>([]);
  const [resultsTotal, setResultsTotal] = useState(0);
  const [status, setStatus] = useState<DealsStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const [copyError, setCopyError] = useState(false);

  // 视图控制
  const [viewMode, setViewMode] = useState<"grid" | "compact">("grid");

  // 筛选与排序状态
  const [sortBy, setSortBy] = useState<"hot" | "newest" | "likes" | "replies">("hot");
  const [hotOnly, setHotOnly] = useState(false);
  const [selectedSource, setSelectedSource] = useState<string>("all");
  const [selectedCategory, setSelectedCategory] = useState<string>("all");
  const [searchKeyword, setSearchKeyword] = useState("");
  const [submittedKeyword, setSubmittedKeyword] = useState("");
  const [selectedTags, setSelectedTags] = useState<SelectedDealTags>({});
  const [showExamples, setShowExamples] = useState(true);
  const [chatOpen, setChatOpen] = useState(false);
  const [chatMessages, setChatMessages] = useState<DealsChatMessage[]>([]);
  const [chatInput, setChatInput] = useState("");
  const [webSearchEnabled, setWebSearchEnabled] = useState(true);
  const [chatBusy, setChatBusy] = useState(false);
  const [chatError, setChatError] = useState<string | null>(null);

  const toggleTag = (tag: string) => {
    const key = normalizeDealTag(tag);
    const group = getDealTagGroup(key);
    setSelectedTags((current) => {
      const next = { ...current };
      if (next[group] === key) delete next[group];
      else next[group] = key;
      return next;
    });
  };

  const clearTagGroup = (group: DealTagGroupId) => {
    setSelectedTags((current) => {
      const next = { ...current };
      delete next[group];
      return next;
    });
  };

  const pageSize = 20;

  const loadData = async (silent = false, append = false) => {
    if (!silent && !append) setLoading(true);
    if (append) setLoadingMore(true);
    setError(null);
    try {
      const [dealsRes, statusRes] = await Promise.all([
        fetchDeals({
          sort: sortBy,
          hot_only: hotOnly,
          source: selectedSource,
          category: selectedCategory,
          keyword: submittedKeyword,
          limit: pageSize,
          offset: append ? deals.length : 0,
        }),
        fetchDealsStatus().catch(() => null),
      ]);
      const items = dealsRes.items || [];
      setResultsTotal(dealsRes.total);
      if (append) {
        setDeals((current) => {
          const seen = new Set(current.map((deal) => deal.id || deal.url));
          return [...current, ...items.filter((deal) => !seen.has(deal.id || deal.url))];
        });
      } else {
        setDeals(items);
      }
      if (statusRes) setStatus(statusRes);
    } catch (err: any) {
      setError(err?.message || "获取羊毛资讯失败");
    } finally {
      if (!silent) setLoading(false);
      if (append) setLoadingMore(false);
    }
  };

  useEffect(() => {
    loadData();
  }, [sortBy, hotOnly, selectedSource, selectedCategory, submittedKeyword]);

  const handleSearchSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const keyword = searchKeyword.trim();
    if (keyword === submittedKeyword) {
      void loadData();
    } else {
      setSubmittedKeyword(keyword);
    }
  };

  const handleClearSearch = () => {
    setSearchKeyword("");
    setSubmittedKeyword("");
  };

  const handleRefresh = async () => {
    if (refreshing) return;
    setRefreshing(true);
    try {
      const res = await refreshDeals();
      if (res.status) setStatus(res.status);
      await loadData(true);
    } catch (err: any) {
      setError(`刷新抓取失败：${err?.message || String(err)}`);
    } finally {
      setRefreshing(false);
    }
  };

  const handleCopyLink = async (deal: DealItem) => {
    try {
      await navigator.clipboard.writeText(deal.url);
      setCopiedId(deal.id);
      setCopyError(false);
      setTimeout(() => setCopiedId(null), 2000);
    } catch {
      setCopiedId(null);
      setCopyError(true);
      setTimeout(() => setCopyError(false), 2500);
    }
  };

  const handleDealsChatSubmit = async (event?: { preventDefault: () => void }) => {
    event?.preventDefault();
    const content = chatInput.trim();
    if (!content || chatBusy) return;
    const userMessage: DealsChatMessage = { id: crypto.randomUUID(), role: "user", content };
    const nextMessages = [...chatMessages, userMessage];
    setChatMessages(nextMessages);
    setChatInput("");
    setChatBusy(true);
    setChatError(null);
    try {
      const response = await postDealsChat({
        messages: nextMessages.slice(-12).map(({ role, content: messageContent }) => ({ role, content: messageContent })),
        web_search: webSearchEnabled,
      });
      setChatMessages((current) => [...current, { id: crypto.randomUUID(), role: "assistant", content: response.answer }]);
    } catch (err: any) {
      setChatError(err?.message || "对话请求失败，请稍后重试");
    } finally {
      setChatBusy(false);
    }
  };

  const formatRelativeTime = (timeStr: string) => {
    if (!timeStr) return "刚刚";
    const date = new Date(timeStr);
    const now = new Date();
    const diffSec = Math.floor((now.getTime() - date.getTime()) / 1000);
    if (diffSec < 60) return "刚刚";
    if (diffSec < 3600) return `${Math.floor(diffSec / 60)} 分钟前`;
    if (diffSec < 86400) return `${Math.floor(diffSec / 3600)} 小时前`;
    return `${Math.floor(diffSec / 86400)} 天前`;
  };

  const getSourceBadge = (source: string) => {
    switch (source) {
      case "linuxdo":
        return { label: "LINUX DO", bg: "bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/20" };
      case "nodeseek":
        return { label: "NodeSeek", bg: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20" };
      case "v2ex":
        return { label: "V2EX", bg: "bg-purple-500/10 text-purple-600 dark:text-purple-400 border-purple-500/20" };
      case "official":
        return { label: "官方福利", bg: "bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/20" };
      default:
        return { label: source.toUpperCase(), bg: "bg-zinc-500/10 text-zinc-600 dark:text-zinc-400 border-zinc-500/20" };
    }
  };

  const getCategoryBadge = (cat: string) => {
    switch (cat) {
      case "ai":
        return { label: "AI 大模型", color: "text-sky-500 bg-sky-500/10 border-sky-500/20" };
      case "api":
        return { label: "API 接口", color: "text-emerald-500 bg-emerald-500/10 border-emerald-500/20" };
      case "media":
        return { label: "绘图 / 视频", color: "text-pink-500 bg-pink-500/10 border-pink-500/20" };
      case "vps":
        return { label: "云主机 / VPS", color: "text-indigo-500 bg-indigo-500/10 border-indigo-500/20" };
      case "card":
        return { label: "账号卡密", color: "text-amber-500 bg-amber-500/10 border-amber-500/20" };
      default:
        return { label: "综合福利", color: "text-zinc-500 bg-zinc-500/10 border-zinc-500/20" };
    }
  };

  const categories = [
    { id: "all", label: "🌟 全部线报" },
    { id: "ai", label: "🤖 AI 大模型" },
    { id: "api", label: "🔌 API 免费接口" },
    { id: "media", label: "🎨 绘图与视频" },
    { id: "vps", label: "☁️ 云主机与算力" },
    { id: "card", label: "🔑 账号与卡密技巧" },
  ];

  const exampleCount = deals.filter(isExampleDeal).length;
  const browsableDeals = useMemo(
    () => showExamples ? deals : deals.filter((deal) => !isExampleDeal(deal)),
    [deals, showExamples],
  );
  const tagGroups = useMemo(() => groupDealTags(browsableDeals), [browsableDeals]);
  const selectedTagEntries = Object.entries(selectedTags) as [DealTagGroupId, string][];
  const visibleDeals = useMemo(
    () => browsableDeals.filter((deal) => matchesDealTags(deal, selectedTags)),
    [browsableDeals, selectedTags],
  );
  const spotlightDeal = visibleDeals.find((deal) => !isExampleDeal(deal)) || null;

  return (
    <div className="deals-view mx-auto w-full min-w-0 max-w-[1440px] space-y-5 pb-12 text-zinc-800 dark:text-zinc-100">
      <header className="flex min-w-0 flex-col justify-between gap-3 sm:flex-row sm:items-end">
        <div className="min-w-0">
          <div className="mb-1.5 flex items-center gap-2 text-[11px] font-semibold tracking-[0.12em] text-orange-600 dark:text-orange-400">
            <span className="h-1.5 w-1.5 rounded-full bg-orange-500" />
            DEVELOPER FINDS
          </div>
          <h1 className="text-xl font-bold tracking-tight text-zinc-900 dark:text-zinc-50 sm:text-2xl">免费额度与开发者福利</h1>
          <p className="mt-1 text-xs leading-5 text-zinc-500 dark:text-zinc-400">AI 与 API、云资源和创作工具，持续汇总社区线索与官方活动。</p>
        </div>
        <div className="inline-flex shrink-0 items-center gap-2 self-start rounded-full border border-zinc-200 bg-white px-3 py-1.5 text-[11px] text-zinc-500 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-400 sm:self-auto">
          <BookOpenText size={13} className="text-orange-500" />
          库内记录 <span className="font-mono-num font-semibold text-zinc-800 dark:text-zinc-100">{status?.total_count ?? resultsTotal}</span> 条
        </div>
      </header>
      {(copyError || error) && (
        <div className="flex min-w-0 items-start justify-between gap-3 rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-xs text-red-700 dark:border-red-900/60 dark:bg-red-950/20 dark:text-red-300" role="status">
          <span className="min-w-0 break-words">{copyError ? "复制失败：请检查浏览器剪贴板权限。" : error}</span>
          {error && <button type="button" onClick={() => void loadData()} className="shrink-0 font-semibold underline underline-offset-2">重试</button>}
        </div>
      )}
      {exampleCount > 0 && (
        <div className="flex flex-col justify-between gap-3 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs text-amber-800 dark:border-amber-900 dark:bg-amber-950/25 dark:text-amber-300 sm:flex-row sm:items-center">
          <div className="min-w-0 leading-5">
            <p className="font-semibold">本批记录含 {exampleCount} 条预置示例</p>
            <p className="text-amber-700 dark:text-amber-400">保留展示供浏览，标注为示例的内容及链接未经核验。</p>
          </div>
          <button type="button" aria-pressed={showExamples} onClick={() => setShowExamples((current) => !current)} className="shrink-0 self-start rounded-lg border border-amber-300 px-3 py-2 font-medium transition hover:bg-amber-100 dark:border-amber-800 dark:hover:bg-amber-900/40 sm:self-auto">
            {showExamples ? "隐藏示例" : "查看预置示例"}
          </button>
        </div>
      )}
      <section className="overflow-hidden rounded-2xl border border-sky-200 bg-gradient-to-br from-white via-sky-50/60 to-indigo-50/50 shadow-sm dark:border-sky-900/50 dark:from-zinc-900 dark:via-sky-950/20 dark:to-indigo-950/20">
        <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3.5 sm:px-5">
          <div className="flex min-w-0 items-center gap-3">
            <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-sky-500/10 text-sky-600 dark:text-sky-400"><MessageCircle size={17} /></span>
            <div className="min-w-0">
              <h2 className="text-sm font-semibold text-zinc-900 dark:text-zinc-100">线报 AI 助手</h2>
              <p className="mt-0.5 text-[11px] text-zinc-500 dark:text-zinc-400">追问福利规则、比较平台额度，或搜索全网最新活动</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button type="button" aria-pressed={webSearchEnabled} onClick={() => setWebSearchEnabled((enabled) => !enabled)} title="开启后通过 ChatGPT 号池搜索公开网页并附来源" className={`inline-flex min-h-9 items-center gap-1.5 rounded-lg border px-3 text-xs font-medium transition ${webSearchEnabled ? "border-sky-200 bg-sky-50 text-sky-700 dark:border-sky-800 dark:bg-sky-950/50 dark:text-sky-300" : "border-zinc-200 bg-white text-zinc-500 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-400"}`}>
              <Globe2 size={13} /> 全网搜索 {webSearchEnabled ? "开" : "关"}
            </button>
            <button type="button" aria-expanded={chatOpen} onClick={() => setChatOpen((open) => !open)} className="inline-flex min-h-9 items-center gap-1.5 rounded-lg bg-zinc-900 px-3 text-xs font-medium text-white transition hover:bg-zinc-700 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-white">
              <MessageCircle size={13} /> {chatOpen ? "收起对话" : "开始对话"}
            </button>
          </div>
        </div>
        {chatOpen && (
          <div className="border-t border-sky-100 px-4 py-4 dark:border-sky-900/40 sm:px-5">
            {chatMessages.length > 0 && <div aria-live="polite" className="mb-4 max-h-[420px] space-y-3 overflow-y-auto pr-1">
              {chatMessages.map((message) => <div key={message.id} className={`flex gap-2.5 ${message.role === "user" ? "justify-end" : "justify-start"}`}>
                {message.role === "assistant" && <span className="mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-sky-500/10 text-sky-600 dark:text-sky-400"><Bot size={14} /></span>}
                <div className={`max-w-[90%] rounded-2xl px-3.5 py-2.5 text-xs leading-6 sm:max-w-[80%] ${message.role === "user" ? "bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900" : "border border-zinc-200 bg-white text-zinc-700 dark:border-zinc-800 dark:bg-zinc-900 dark:text-zinc-300"}`}>
                  {message.role === "assistant" ? <MarkdownContent content={message.content} /> : <p className="whitespace-pre-wrap">{message.content}</p>}
                </div>
                {message.role === "user" && <span className="mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-zinc-200 text-zinc-600 dark:bg-zinc-800 dark:text-zinc-300"><UserRound size={14} /></span>}
              </div>)}
              {chatBusy && <div className="flex items-center gap-2 text-xs text-zinc-500"><RefreshCw size={13} className="animate-spin text-sky-500" /> 正在整理线报并搜索...</div>}
            </div>}
            {chatError && <div role="alert" className="mb-3 flex items-center justify-between gap-3 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-700 dark:border-rose-900/50 dark:bg-rose-950/30 dark:text-rose-300"><span>{chatError}</span><button type="button" aria-label="清除错误" onClick={() => setChatError(null)}><X size={13} /></button></div>}
            {chatMessages.length > 0 && <button type="button" disabled={chatBusy} onClick={() => setChatMessages([])} className="mb-2 inline-flex items-center gap-1 rounded-md px-2 py-1 text-[11px] text-zinc-400 transition hover:bg-zinc-100 hover:text-zinc-600 disabled:opacity-50 dark:hover:bg-zinc-800 dark:hover:text-zinc-300"><Trash2 size={11} /> 清空本次对话</button>}
            <form onSubmit={handleDealsChatSubmit} className="flex items-end gap-2">
              <textarea value={chatInput} onChange={(event) => setChatInput(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); void handleDealsChatSubmit(event); } }} rows={2} maxLength={4000} aria-label="输入线报问题" placeholder="例如：这几个免费 API 哪个额度更适合做语音转写？" className="min-h-12 min-w-0 flex-1 resize-y rounded-xl border border-zinc-200 bg-white px-3.5 py-2.5 text-xs leading-5 text-zinc-800 outline-none transition placeholder:text-zinc-400 focus:border-sky-400 focus:ring-2 focus:ring-sky-500/15 dark:border-zinc-700 dark:bg-zinc-950/50 dark:text-zinc-200" />
              <button type="submit" disabled={chatBusy || !chatInput.trim()} className="inline-flex min-h-11 shrink-0 items-center gap-1.5 rounded-xl bg-sky-600 px-3.5 text-xs font-semibold text-white shadow-sm transition hover:bg-sky-500 disabled:cursor-not-allowed disabled:opacity-50"><Send size={13} /> 发送</button>
            </form>
            <p className="mt-2 text-[10px] text-zinc-400">Enter 发送 · Shift+Enter 换行{webSearchEnabled ? " · 已开启联网，回答可能需要稍等" : " · 当前仅使用模型知识回答"}</p>
          </div>
        )}
      </section>
      {/* 顶部 Hero 焦点卡片 */}
      {spotlightDeal && (
        <div className="relative isolate overflow-hidden rounded-2xl border border-orange-200 bg-gradient-to-br from-white via-orange-50/80 to-amber-50/60 p-5 shadow-sm dark:border-orange-500/20 dark:from-zinc-900 dark:via-zinc-900 dark:to-orange-950/30 sm:p-6">
          <div className="relative flex min-w-0 flex-col justify-between gap-5 xl:flex-row xl:items-center xl:gap-8">
            <div className="min-w-0 flex-1 space-y-3">
              <div className="flex flex-wrap items-center gap-2">
                <span className="inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-bold bg-orange-500 text-white shadow-sm animate-pulse">
                  <Flame size={13} />
                  {hotOnly ? "社区热榜焦点" : "本页焦点"} · {sortBy === "newest" ? "最新发布" : sortBy === "likes" ? "点赞优先" : sortBy === "replies" ? "讨论优先" : "热度优先"}
                </span>
                <span className="text-xs font-mono text-orange-600 dark:text-orange-400 font-semibold" title="互动数经过对数式压缩并按发布时间衰减，归一化到 0–100">
                  热度指数: {formatHeatScore(spotlightDeal.score)}{spotlightDeal.score > 0 ? " / 100" : ""}
                </span>
                <span className="text-xs text-zinc-400">
                  {formatRelativeTime(spotlightDeal.published_at)}
                </span>
              </div>

              <h2 className="text-xl sm:text-2xl font-extrabold tracking-tight text-zinc-900 dark:text-zinc-100 leading-snug">
                <a
                  href={spotlightDeal.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="hover:text-orange-600 dark:hover:text-orange-400 transition"
                >
                  {spotlightDeal.title}
                </a>
              </h2>

              <p className="text-xs sm:text-sm text-zinc-600 dark:text-zinc-300 leading-relaxed max-w-3xl line-clamp-2">
                {spotlightDeal.summary}
              </p>

              <div className="flex flex-wrap items-center gap-4 text-xs text-zinc-500 dark:text-zinc-400 pt-1">
                <span className="flex items-center gap-1 font-medium text-orange-600 dark:text-orange-400">
                  <ThumbsUp size={13} /> {formatMetric(spotlightDeal.likes)} 点赞
                </span>
                <span className="flex items-center gap-1">
                  <MessageSquare size={13} /> {formatMetric(spotlightDeal.replies)} 讨论
                </span>
                <span className="flex items-center gap-1">
                  <Eye size={13} /> {formatMetric(spotlightDeal.views)} 浏览
                </span>
                <span>来源: {getSourceBadge(spotlightDeal.source).label}</span>
              </div>
            </div>

            <div className="flex shrink-0 flex-wrap items-center gap-2 sm:gap-3">
              <button
                onClick={() => handleCopyLink(spotlightDeal)}
                className="inline-flex min-h-10 items-center gap-1.5 rounded-xl border border-zinc-200 bg-white/80 px-3.5 py-2.5 text-xs font-medium text-zinc-700 shadow-sm transition hover:bg-white dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-200 dark:hover:bg-zinc-700"
              >
                {copiedId === spotlightDeal.id ? <Check size={14} className="text-emerald-500" /> : <Copy size={14} />}
                {copiedId === spotlightDeal.id ? "已复制" : "复制链接"}
              </button>
              <a
                href={spotlightDeal.url}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex min-h-10 items-center gap-1.5 rounded-xl bg-orange-600 px-4 py-2.5 text-xs font-semibold text-white shadow-sm shadow-orange-500/15 transition hover:bg-orange-500"
              >
                查看原帖
                <ArrowUpRight size={14} />
              </a>
            </div>
          </div>
        </div>
      )}

      {/* 控制总览栏：状态与即时刷新 */}
      <div className="flex min-w-0 flex-col justify-between gap-3 rounded-xl border border-emerald-200/80 bg-emerald-50/70 p-3.5 text-xs shadow-sm dark:border-emerald-900/60 dark:bg-emerald-950/20 sm:flex-row sm:items-center sm:p-4">
        <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2 text-zinc-500 dark:text-zinc-400">
          <div className="flex items-center gap-1.5">
            <Radio size={14} className={status?.is_running ? "text-emerald-500" : "text-zinc-400"} />
            <span className="font-medium text-zinc-800 dark:text-zinc-200">{status ? status.is_running ? "定时同步已开启" : "定时同步未运行" : "同步状态未知"}</span>
          </div>
          <span className="hidden sm:inline">·</span>
          <span>同步间隔 {status?.interval || "待确认"}</span>
          <span className="hidden sm:inline">·</span>
          <span>已加载 {deals.length - exampleCount} 条抓取记录</span>
        </div>

        <div className="flex min-w-0 flex-wrap items-center gap-2">
          {status?.last_update_at && (
            <span className="text-zinc-400 hidden md:inline">
              最近同步: {formatRelativeTime(status.last_update_at)}
            </span>
          )}
          <button
            onClick={handleRefresh}
            disabled={refreshing}
            className="inline-flex min-h-10 shrink-0 items-center justify-center gap-1.5 rounded-lg bg-zinc-900 px-3.5 py-2 text-xs font-medium text-white shadow-sm transition hover:bg-zinc-700 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-zinc-100 dark:text-zinc-900 dark:hover:bg-white"
          >
            <RefreshCw size={12} className={refreshing ? "animate-spin" : ""} />
            {refreshing ? "正在同步..." : "抓取最新线报"}
          </button>
        </div>
      </div>

      {/* 分类快捷标签栏 */}
      <div className="flex min-w-0 snap-x gap-2 overflow-x-auto pb-2 text-xs [scrollbar-width:thin]">
        {categories.map((cat) => (
          <button
            key={cat.id}
            type="button"
            aria-pressed={selectedCategory === cat.id}
            onClick={() => setSelectedCategory(cat.id)}
            className={`inline-flex min-h-10 shrink-0 snap-start items-center gap-1.5 rounded-xl border px-3.5 py-2 font-medium transition ${
              selectedCategory === cat.id
                ? "border-orange-500 bg-orange-50 text-orange-700 shadow-sm dark:border-orange-400/70 dark:bg-orange-950/40 dark:text-orange-200"
                : "border-zinc-200/80 bg-white text-zinc-600 shadow-sm hover:border-zinc-300 hover:bg-zinc-50 dark:border-zinc-800 dark:bg-zinc-900/70 dark:text-zinc-300 dark:hover:bg-zinc-800"
            }`}
          >
            {cat.label}
          </button>
        ))}
      </div>

      {/* 筛选与检索控制条 */}
      <div className="flex min-w-0 flex-col gap-3 rounded-2xl border border-zinc-200 bg-white p-3 shadow-sm dark:border-zinc-800 dark:bg-zinc-900/75 sm:p-3.5">
        <form onSubmit={handleSearchSubmit} className="relative flex min-w-0 w-full flex-1">
          <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-zinc-400" />
          <input
            type="text"
            value={searchKeyword}
            onChange={(e) => setSearchKeyword(e.target.value)}
            aria-label="搜索线报"
            placeholder="搜索标题、福利或平台，如 Grok、免费 API..."
            className="min-h-10 w-full min-w-0 rounded-xl border border-zinc-200 bg-zinc-50/70 py-2 pl-9 pr-20 text-xs text-zinc-800 outline-none transition placeholder:text-zinc-400 focus:border-orange-400 focus:bg-white focus:ring-2 focus:ring-orange-500/15 dark:border-zinc-700 dark:bg-zinc-950/40 dark:text-zinc-200 dark:focus:bg-zinc-950"
          />
          <div className="absolute right-1.5 top-1/2 flex -translate-y-1/2 items-center gap-1">
            {searchKeyword && (
              <button type="button" onClick={handleClearSearch} className="inline-flex min-h-8 items-center gap-1 rounded-lg px-2 text-[11px] text-zinc-500 transition hover:bg-zinc-100 hover:text-zinc-800 dark:hover:bg-zinc-800 dark:hover:text-zinc-200">
                <X size={12} /> 清除
              </button>
            )}
            <button type="submit" className="inline-flex min-h-8 items-center gap-1 rounded-lg bg-zinc-900 px-2.5 text-[11px] font-medium text-white transition hover:bg-zinc-700 dark:bg-zinc-100 dark:text-zinc-900">
              <Search size={12} />
              <span>搜索</span>
            </button>
          </div>
        </form>

        <div className="flex min-w-0 flex-wrap items-center justify-between gap-2 text-xs">
          <div role="group" aria-label="线报范围" className="inline-flex rounded-lg border border-zinc-200 bg-white p-0.5 dark:border-zinc-700 dark:bg-zinc-800">
            <button type="button" aria-pressed={!hotOnly} title="显示所有线报，也包含未核验的预置示例" onClick={() => setHotOnly(false)} className={`inline-flex min-h-8 items-center gap-1 rounded-md px-2.5 font-medium transition ${!hotOnly ? "bg-zinc-100 text-zinc-800 shadow-xs dark:bg-zinc-700 dark:text-zinc-100" : "text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"}`}>
              <LayoutGrid size={12} /> 全部线报
            </button>
            <button type="button" aria-pressed={hotOnly} title="只显示互动数据真实且达到热度门槛的社区线报" onClick={() => setHotOnly(true)} className={`inline-flex min-h-8 items-center gap-1 rounded-md px-2.5 font-medium transition ${hotOnly ? "bg-orange-500 text-white shadow-xs" : "text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"}`}>
              <Flame size={12} /> 社区热榜 <span className={hotOnly ? "text-white/80" : "text-zinc-400"}>{status?.hot_count ?? 0}</span>
            </button>
          </div>
          <p className="w-full text-[10px] text-zinc-400">全部线报包含预置示例；社区热榜仅统计真实互动数据达到门槛的采集记录。右侧“热度 / 最新 / 高赞 / 热评”只改变排序方式。</p>

          {/* 来源筛选 */}
          <select
            value={selectedSource}
            onChange={(e) => setSelectedSource(e.target.value)}
            className="px-2.5 py-1.5 rounded-lg border border-zinc-200 dark:border-zinc-700 bg-white dark:bg-zinc-800 text-zinc-700 dark:text-zinc-300 focus:outline-none"
          >
            <option value="all">🌐 全部渠道</option>
            <option value="linuxdo">LINUX DO 社区</option>
            <option value="nodeseek">NodeSeek 论坛</option>
            <option value="v2ex">V2EX 程序员站</option>
            <option value="official">各大厂商官方活动</option>
          </select>

          {/* 排序模式 */}
          <div className="inline-flex rounded-lg border border-zinc-200 dark:border-zinc-700 bg-white dark:bg-zinc-800 p-0.5">
            <button
              onClick={() => setSortBy("hot")}
              title="按时间衰减热度排序"
              className={`px-2.5 py-1 rounded-md font-medium flex items-center gap-1 transition ${
                sortBy === "hot"
                  ? "bg-orange-500 text-white shadow-xs"
                  : "text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"
              }`}
            >
              <Flame size={12} />
              热度
            </button>
            <button
              onClick={() => setSortBy("newest")}
              title="按发布时间排序"
              className={`px-2.5 py-1 rounded-md font-medium flex items-center gap-1 transition ${
                sortBy === "newest"
                  ? "bg-zinc-900 text-white dark:bg-zinc-700 shadow-xs"
                  : "text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"
              }`}
            >
              <Clock size={12} />
              最新
            </button>
            <button
              onClick={() => setSortBy("likes")}
              title="按点赞量降序"
              className={`px-2.5 py-1 rounded-md font-medium flex items-center gap-1 transition ${
                sortBy === "likes"
                  ? "bg-zinc-900 text-white dark:bg-zinc-700 shadow-xs"
                  : "text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"
              }`}
            >
              <ThumbsUp size={12} />
              高赞
            </button>
            <button
              onClick={() => setSortBy("replies")}
              title="按评论互动量降序"
              className={`px-2.5 py-1 rounded-md font-medium flex items-center gap-1 transition ${
                sortBy === "replies"
                  ? "bg-zinc-900 text-white dark:bg-zinc-700 shadow-xs"
                  : "text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200"
              }`}
            >
              <MessageSquare size={12} />
              热评
            </button>
          </div>

          {/* 视图切换 */}
          <div className="inline-flex rounded-lg border border-zinc-200 dark:border-zinc-700 bg-white dark:bg-zinc-800 p-0.5">
            <button
              onClick={() => setViewMode("grid")}
              className={`p-1.5 rounded-md transition ${viewMode === "grid" ? "bg-zinc-100 dark:bg-zinc-700 text-zinc-900 dark:text-zinc-100" : "text-zinc-400"}`}
              title="网格瀑布卡片"
            >
              <LayoutGrid size={13} />
            </button>
            <button
              onClick={() => setViewMode("compact")}
              className={`p-1.5 rounded-md transition ${viewMode === "compact" ? "bg-zinc-100 dark:bg-zinc-700 text-zinc-900 dark:text-zinc-100" : "text-zinc-400"}`}
              title="紧凑列表清单"
            >
              <List size={13} />
            </button>
          </div>
        </div>
      </div>

      {(browsableDeals.length > 0 || selectedTagEntries.length > 0) && (
        <section aria-label="按分类标签筛选线报" className="min-w-0 overflow-hidden rounded-2xl border border-zinc-200 bg-white shadow-sm dark:border-zinc-800 dark:bg-zinc-900/60">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-zinc-100 px-4 py-3 dark:border-zinc-800">
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <h2 className="flex items-center gap-1.5 text-xs font-semibold"><Tag size={14} className="text-orange-500" /> 标签分类</h2>
              <p className="text-[11px] text-zinc-500 dark:text-zinc-400">同组单选，跨组组合 · 数量统计已加载内容</p>
            </div>
            {selectedTagEntries.length > 0 && <button type="button" onClick={() => setSelectedTags({})} className="min-h-8 rounded-lg px-2 text-[11px] text-zinc-500 transition hover:bg-zinc-100 dark:text-zinc-400 dark:hover:bg-zinc-800">清空标签</button>}
          </div>
          <div className="divide-y divide-zinc-100 px-4 dark:divide-zinc-800/80">
            {tagGroups.filter((group) => group.tags.length > 0 || selectedTags[group.id]).map((group) => (
              <div key={group.id} role="group" aria-label={group.label} className="flex min-w-0 flex-col gap-2 py-3 sm:flex-row sm:gap-4">
                <span className="pt-1.5 text-[11px] font-medium text-zinc-500 dark:text-zinc-400 sm:w-20 sm:shrink-0">{group.label}</span>
                <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5 text-[11px]">
                  <button type="button" aria-label={`${group.label}：不限`} aria-pressed={!selectedTags[group.id]} onClick={() => clearTagGroup(group.id)} className={`inline-flex min-h-8 items-center rounded-lg border px-2.5 transition ${!selectedTags[group.id] ? "border-zinc-300 bg-zinc-100 font-medium text-zinc-800 dark:border-zinc-600 dark:bg-zinc-800 dark:text-zinc-100" : "border-transparent text-zinc-500 hover:bg-zinc-50 dark:text-zinc-400 dark:hover:bg-zinc-800"}`}>不限</button>
                  {group.tags.map((tag) => (
                    <button key={tag.key} type="button" aria-pressed={selectedTags[group.id] === tag.key} onClick={() => toggleTag(tag.key)} className={`inline-flex min-h-8 max-w-full items-center gap-1.5 rounded-lg border px-2.5 text-left transition ${selectedTags[group.id] === tag.key ? "border-orange-200 bg-orange-50 font-medium text-orange-700 dark:border-orange-900 dark:bg-orange-950/50 dark:text-orange-300" : "border-zinc-200/80 text-zinc-600 hover:border-orange-200 hover:bg-orange-50/50 dark:border-zinc-800 dark:text-zinc-400 dark:hover:border-orange-900 dark:hover:bg-orange-950/20"}`}>
                      <span className="min-w-0 break-words [overflow-wrap:anywhere]">#{tag.label}</span><span className="font-mono-num text-zinc-400">{tag.count}</span>
                    </button>
                  ))}
                </div>
              </div>
            ))}
          </div>
          {selectedTagEntries.length > 0 && (
            <div className="flex flex-wrap items-center gap-2 border-t border-zinc-100 bg-zinc-50/70 px-4 py-3 text-[11px] dark:border-zinc-800 dark:bg-zinc-950/30">
              <span className="text-zinc-500 dark:text-zinc-400">已选条件</span>
              {selectedTagEntries.map(([groupId, key]) => {
                const group = tagGroups.find((group) => group.id === groupId)!;
                const label = group.tags.find((tag) => tag.key === key)?.label || key;
                return <button key={groupId} type="button" onClick={() => clearTagGroup(groupId)} aria-label={`移除${group.label}筛选：${label}`} className="inline-flex min-h-7 items-center gap-1 rounded-lg border border-orange-200 bg-orange-50 px-2 text-orange-700 dark:border-orange-900 dark:bg-orange-950/40 dark:text-orange-300">{group.label}：{label}<X size={11} /></button>;
              })}
              <span className="text-zinc-500 dark:text-zinc-400" aria-live="polite">匹配 {visibleDeals.length} 条{deals.length < resultsTotal ? "，可加载更多继续筛选" : ""}</span>
            </div>
          )}
        </section>
      )}

      {/* 资讯内容主体 */}
      {loading ? (
        <div className="py-24 text-center space-y-3">
          <RefreshCw size={26} className="animate-spin text-orange-500 mx-auto" />
          <p className="text-xs text-zinc-500 font-medium">正在读取社区线报...</p>
        </div>
      ) : visibleDeals.length === 0 ? (
        <div className="py-20 text-center space-y-3 bg-white dark:bg-zinc-900/40 rounded-2xl border border-dashed border-zinc-300 dark:border-zinc-800">
          <Inbox size={30} className="mx-auto text-zinc-300 dark:text-zinc-600" />
          <p className="text-sm font-medium text-zinc-600 dark:text-zinc-400">{selectedTagEntries.length > 0 ? "当前标签组合暂无匹配线报" : "当前已加载内容中暂无匹配的线报"}</p>
          <p className="px-4 text-xs text-zinc-400">{hotOnly ? "热榜只收录真实互动数据达到门槛的社区线报；预置示例可在“全部线报”中查看。" : deals.length < resultsTotal ? "可在下方加载更多记录，或调整筛选条件。" : "可以调整分类、来源或搜索词。"}</p>
          {selectedTagEntries.length > 0 && <button type="button" onClick={() => setSelectedTags({})} className="mt-2 rounded-lg border border-zinc-200 bg-white px-3 py-2 text-xs font-medium text-zinc-600 transition hover:bg-zinc-50 dark:border-zinc-700 dark:bg-zinc-900 dark:text-zinc-300 dark:hover:bg-zinc-800">清除标签筛选</button>}
        </div>
      ) : viewMode === "compact" ? (
        /* 紧凑列表清单模式 */
        <div className="min-w-0 divide-y divide-zinc-100 overflow-hidden rounded-2xl border border-zinc-200 bg-white shadow-sm dark:divide-zinc-800 dark:border-zinc-800 dark:bg-zinc-900">
          {visibleDeals.map((deal, idx) => {
            const srcBadge = getSourceBadge(deal.source);
            return (
              <div
                key={deal.id || deal.url}
                className="flex min-w-0 items-center justify-between gap-2 px-3 py-3.5 text-xs transition hover:bg-zinc-50/80 dark:hover:bg-zinc-800/40 sm:gap-4 sm:px-4"
              >
                <div className="flex min-w-0 flex-1 items-center gap-2 sm:gap-3">
                  <span className="w-6 text-center font-mono font-bold text-zinc-400">
                    {hotOnly && sortBy === "hot" && deal.is_hot && idx < 3 ? ["🥇", "🥈", "🥉"][idx] : `#${idx + 1}`}
                  </span>
                  <span className={`px-2 py-0.5 rounded text-[10px] font-semibold border shrink-0 ${srcBadge.bg}`}>
                    {srcBadge.label}
                  </span>
                  <a
                    href={deal.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="font-medium text-zinc-800 dark:text-zinc-200 hover:text-orange-600 dark:hover:text-orange-400 truncate transition"
                  >
                    {deal.title}
                  </a>
                </div>

                <div className="flex shrink-0 items-center gap-2 text-zinc-400 sm:gap-4">
                  {isExampleDeal(deal) && <span className="text-[11px] text-amber-600 dark:text-amber-400">预置示例 · 未核验</span>}
                  {!isExampleDeal(deal) && <>
                  <span className="hidden sm:inline font-mono text-[11px] text-orange-600 dark:text-orange-400 font-semibold" title="按互动量与时间衰减计算的 0–100 热度指数">
                    🔥 {formatHeatScore(deal.score)}{deal.score > 0 ? "/100" : ""}
                  </span>
                  <span className="flex items-center gap-1">
                    <ThumbsUp size={11} /> {formatMetric(deal.likes)}
                  </span>
                  <span className="flex items-center gap-1">
                    <MessageSquare size={11} /> {formatMetric(deal.replies)}
                  </span>
                  <span className="hidden md:inline text-[11px]">
                    {formatRelativeTime(deal.published_at)}
                  </span>
                  </>}
                  <a
                    href={deal.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="p-1 rounded hover:bg-zinc-200 dark:hover:bg-zinc-700 text-zinc-600 dark:text-zinc-300"
                  >
                    <ArrowUpRight size={13} />
                  </a>
                </div>
              </div>
            );
          })}
        </div>
      ) : (
        /* 网格瀑布流模式 */
        <div className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 sm:gap-4">
          {visibleDeals.map((deal, idx) => {
            const srcBadge = getSourceBadge(deal.source);
            const catBadge = getCategoryBadge(deal.category);
            const isExample = isExampleDeal(deal);
            const isTop3 = hotOnly && idx < 3 && sortBy === "hot" && deal.is_hot && !isExample;

            return (
              <div
                key={deal.id || deal.url}
                className={`group relative flex min-w-0 flex-col justify-between rounded-2xl border bg-white p-4 shadow-sm transition duration-200 hover:-translate-y-0.5 hover:border-zinc-300 hover:shadow-md dark:bg-zinc-900/70 dark:hover:border-zinc-700 sm:p-5 ${
                  isTop3
                    ? "border-orange-500/30 dark:border-orange-500/20"
                    : "border-zinc-200/80 dark:border-zinc-800"
                }`}
              >
                <div>
                  {/* 标头行 */}
                  <div className="mb-3 flex min-w-0 flex-wrap items-center justify-between gap-2">
                    <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                      <span className={`px-2 py-0.5 rounded-md text-[10px] font-semibold border ${srcBadge.bg}`}>
                        {srcBadge.label}
                      </span>
                      <span className={`px-1.5 py-0.5 rounded text-[10px] font-medium border ${catBadge.color}`}>
                        {catBadge.label}
                      </span>
                      {isExample && <span className="rounded border border-amber-200 bg-amber-50 px-1.5 py-0.5 text-[10px] text-amber-700 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300">预置示例 · 未核验</span>}
                    </div>

                    <div className="ml-auto flex shrink-0 items-center gap-1.5 text-xs">
                      {isTop3 ? (
                        <span className="font-bold text-xs">
                          {idx === 0 ? "🥇 TOP 1" : idx === 1 ? "🥈 TOP 2" : "🥉 TOP 3"}
                        </span>
                      ) : (
                        <span className="text-zinc-400 font-mono text-[11px]">
                          #{idx + 1}
                        </span>
                      )}
                      <span title="按互动量与时间衰减计算的 0–100 热度指数" className={`text-[11px] font-mono font-semibold ${deal.score > 0 ? "text-orange-500" : "text-zinc-400"}`}>
                        🔥 {formatHeatScore(deal.score)}{deal.score > 0 ? "/100" : ""}
                      </span>
                    </div>
                  </div>

                  {/* 标题 */}
                  <a
                    href={deal.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="block text-sm font-bold leading-6 text-zinc-900 line-clamp-2 transition group-hover:text-orange-600 dark:text-zinc-100 dark:group-hover:text-orange-400"
                  >
                    {deal.title}
                  </a>

                  {/* 核心亮点摘要 */}
                  <p className="mt-2.5 line-clamp-3 text-xs leading-relaxed text-zinc-600 dark:text-zinc-400">
                    {deal.summary}
                  </p>

                  {/* 标签 */}
                  {deal.tags && deal.tags.length > 0 && (
                    <div className="mt-3 flex flex-wrap gap-1.5">
                      {deal.tags.map((tag) => (
                        <button
                          key={`${deal.id}-${tag}`}
                          type="button"
                          aria-pressed={selectedTags[getDealTagGroup(tag)] === normalizeDealTag(tag)}
                          onClick={() => toggleTag(tag)}
                          className={`max-w-full break-words rounded-md border px-1.5 py-0.5 text-left text-[10px] transition [overflow-wrap:anywhere] ${selectedTags[getDealTagGroup(tag)] === normalizeDealTag(tag) ? "border-orange-200 bg-orange-50 text-orange-700 dark:border-orange-900 dark:bg-orange-950/50 dark:text-orange-300" : "border-transparent bg-zinc-100 text-zinc-600 hover:border-zinc-300 hover:bg-zinc-50 dark:bg-zinc-800/80 dark:text-zinc-400 dark:hover:border-zinc-700"}`}
                        >
                          #{tag}
                        </button>
                      ))}
                    </div>
                  )}
                </div>

                {/* 底部指标与直达操作 */}
                <div className="mt-4 pt-3 border-t border-zinc-100 dark:border-zinc-800/80 flex items-center justify-between text-xs text-zinc-400">
                  <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
                    <span className="flex items-center gap-1" title="点赞数">
                      <ThumbsUp size={11} />
                      {formatMetric(deal.likes)}
                    </span>
                    <span className="flex items-center gap-1" title="评论回复">
                      <MessageSquare size={11} />
                      {formatMetric(deal.replies)}
                    </span>
                    <span className="flex items-center gap-1" title="浏览量">
                      <Eye size={11} />
                      {formatMetric(deal.views)}
                    </span>
                    {isExample && <span className="text-[10px] text-amber-600 dark:text-amber-400">内容及链接待核验</span>}
                    <span className="text-[11px]">
                      {isExample ? "时间未核验" : formatRelativeTime(deal.published_at)}
                    </span>
                  </div>

                  <div className="flex shrink-0 items-center gap-1.5">
                    <button
                      onClick={() => handleCopyLink(deal)}
                      title="复制文章直达链接"
                      className="p-1 rounded-md text-zinc-400 hover:text-zinc-600 dark:hover:text-zinc-200 hover:bg-zinc-100 dark:hover:bg-zinc-800 transition"
                    >
                      {copiedId === deal.id ? <Check size={13} className="text-emerald-500" /> : <Copy size={13} />}
                    </button>
                    <a
                      href={deal.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex items-center gap-1 px-2.5 py-1 rounded-md text-[11px] font-medium bg-zinc-100 hover:bg-zinc-200 dark:bg-zinc-800 dark:hover:bg-zinc-700 text-zinc-800 dark:text-zinc-200 transition"
                    >
                      直达
                      <ArrowUpRight size={12} />
                    </a>
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {deals.length > 0 && (
        <div className="flex min-w-0 flex-col justify-between gap-3 rounded-xl border border-zinc-200 bg-white/70 px-3.5 py-3 text-xs dark:border-zinc-800 dark:bg-zinc-900/45 sm:flex-row sm:items-center">
          <span className="min-w-0 text-zinc-500 dark:text-zinc-400" aria-live="polite">
            已加载 <strong className="font-mono-num font-semibold text-zinc-800 dark:text-zinc-100">{deals.length}</strong> / {resultsTotal} 条
            {exampleCount > 0 && <>（含 {exampleCount} 条预置示例）</>}
            <> · 当前匹配 <strong className="font-mono-num font-semibold text-orange-600 dark:text-orange-400">{visibleDeals.length}</strong> 条</>
          </span>
          {deals.length < resultsTotal && (
            <button type="button" disabled={loadingMore || loading} onClick={() => void loadData(false, true)} className="inline-flex min-h-9 shrink-0 items-center justify-center gap-1.5 self-start rounded-lg border border-zinc-200 bg-white px-3.5 text-xs font-medium text-zinc-700 shadow-sm transition hover:border-zinc-300 hover:bg-zinc-50 disabled:cursor-wait disabled:opacity-60 dark:border-zinc-700 dark:bg-zinc-800 dark:text-zinc-200 dark:hover:bg-zinc-700 sm:self-auto">
              <ArrowDown size={13} className={loadingMore ? "animate-bounce" : ""} />
              {loadingMore ? "正在加载..." : `加载更多 · ${Math.min(pageSize, resultsTotal - deals.length)} 条`}
            </button>
          )}
          {deals.length >= resultsTotal && resultsTotal > 0 && <span className="shrink-0 text-[11px] text-zinc-400">已加载全部线报</span>}
        </div>
      )}

      {/* 底部板块：社区直通车与安全避坑贴士 */}
      <div className="grid min-w-0 grid-cols-1 gap-3 pt-4 md:grid-cols-2">
        {/* 社区直通车 */}
        <div className="p-5 rounded-2xl border border-zinc-200 dark:border-zinc-800 bg-white dark:bg-zinc-900/60 shadow-sm space-y-3">
          <div className="flex items-center gap-2 text-xs font-bold text-zinc-800 dark:text-zinc-200">
            <Compass size={14} className="text-orange-500" />
            全网极客与白嫖论坛直通车
          </div>
          <div className="grid grid-cols-1 gap-2 text-xs sm:grid-cols-2 lg:grid-cols-3">
            <a
              href="https://linux.do/c/36"
              target="_blank"
              rel="noopener noreferrer"
              className="p-2.5 rounded-xl border border-zinc-200/60 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-800/40 hover:border-blue-500/40 hover:text-blue-500 transition flex items-center justify-between"
            >
              <span>LINUX DO 羊毛</span>
              <ExternalLink size={11} className="opacity-60" />
            </a>
            <a
              href="https://www.nodeseek.com"
              target="_blank"
              rel="noopener noreferrer"
              className="p-2.5 rounded-xl border border-zinc-200/60 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-800/40 hover:border-emerald-500/40 hover:text-emerald-500 transition flex items-center justify-between"
            >
              <span>NodeSeek 科技</span>
              <ExternalLink size={11} className="opacity-60" />
            </a>
            <a
              href="https://openrouter.ai/models?max_price=0"
              target="_blank"
              rel="noopener noreferrer"
              className="p-2.5 rounded-xl border border-zinc-200/60 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-800/40 hover:border-purple-500/40 hover:text-purple-500 transition flex items-center justify-between"
            >
              <span>OpenRouter 免费</span>
              <ExternalLink size={11} className="opacity-60" />
            </a>
            <a
              href="https://www.v2ex.com/go/promotions"
              target="_blank"
              rel="noopener noreferrer"
              className="p-2.5 rounded-xl border border-zinc-200/60 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-800/40 hover:border-violet-500/40 hover:text-violet-500 transition flex items-center justify-between"
            >
              <span>V2EX 优惠节点</span>
              <ExternalLink size={11} className="opacity-60" />
            </a>
            <a
              href="https://cloud.siliconflow.cn"
              target="_blank"
              rel="noopener noreferrer"
              className="p-2.5 rounded-xl border border-zinc-200/60 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-800/40 hover:border-amber-500/40 hover:text-amber-500 transition flex items-center justify-between"
            >
              <span>硅基流动 平台</span>
              <ExternalLink size={11} className="opacity-60" />
            </a>
            <a
              href="https://huggingface.co/spaces"
              target="_blank"
              rel="noopener noreferrer"
              className="p-2.5 rounded-xl border border-zinc-200/60 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-800/40 hover:border-yellow-500/40 hover:text-yellow-500 transition flex items-center justify-between"
            >
              <span>HuggingFace 算力</span>
              <ExternalLink size={11} className="opacity-60" />
            </a>
          </div>
        </div>

        {/* 避坑与安全提示 */}
        <div className="p-5 rounded-2xl border border-amber-500/20 bg-amber-500/5 dark:bg-amber-950/10 shadow-sm space-y-2 text-xs">
          <div className="flex items-center gap-2 font-bold text-amber-600 dark:text-amber-400">
            <ShieldAlert size={14} />
            白嫖羊毛防踩坑小贴士
          </div>
          <ul className="space-y-1.5 text-zinc-600 dark:text-zinc-400 text-[11px] leading-relaxed list-disc list-inside">
            <li><strong>勿绑常用实体银行卡</strong>：申请免费试用服务时，推荐使用虚拟卡或空额度预付卡，防止到期自动扣费；</li>
            <li><strong>使用独立临时邮箱</strong>：注册各类免额度平台时，配合本项目的临时邮箱中继服务，保护个人核心隐私；</li>
            <li><strong>警惕第三方代充与黑产卡密</strong>：优先自建注册机或使用官方开放免费端点，远离黑灰产低质号源。</li>
          </ul>
        </div>
      </div>
    </div>
  );
}
