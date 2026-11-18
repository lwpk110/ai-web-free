import React, { useState, useEffect, useRef } from "react";
import {
  fetchVoices,
  fetchVoicePreview,
  pullLocalVoices,
  cloneCustomVoice,
  deleteCustomVoice,
  synthesizeSpeechWithMeta,
  voiceDesign,
  voiceClone,
  fetchAudioHistory,
  VoiceDescriptor,
  AudioHistoryItem,
  TTSProviderSummary,
  SynthesizeSpeechMeta,
} from "../api/client";
import { CloudKeyConfigModal, type CloudKeyProviderId } from "../components/CloudKeyConfigModal";
import {
  Mic,
  Volume2,
  Download,
  Play,
  Pause,
  Wand2,
  AudioWaveform,
  Sparkles,
  RefreshCw,
  Search,
  Check,
  Radio,
  Sliders,
  Layers,
  History,
  Copy,
  Clapperboard,
  ExternalLink,
  X,
  HardDrive,
  AlertTriangle,
  KeyRound,
  Globe,
  Trash2,
  DownloadCloud,
} from "lucide-react";
import { PromptChatAssistant } from "../components/PromptChatAssistant";
import { dispatchDramaCanvas } from "../lib/dramaCanvasBridge";

export function TTSStudio(): React.JSX.Element {
  const [voices, setVoices] = useState<VoiceDescriptor[]>([]);
  const [providersSummary, setProvidersSummary] = useState<Record<string, TTSProviderSummary>>({});
  const [cachedLocalTotal, setCachedLocalTotal] = useState<number>(0);
  const [loadingVoices, setLoadingVoices] = useState(false);
  const [voiceLoadError, setVoiceLoadError] = useState<string | null>(null);
  const [selectedVoice, setSelectedVoice] = useState<string>("zh-CN-XiaoxiaoNeural");
  const [inputText, setInputText] = useState<string>(
    "注意看，眼前这个女人叫小美，就在刚刚，她发现了整个控制台里隐藏的秘密...",
  );
  const [speed, setSpeed] = useState<number>(1.0);
  const [allowFallback, setAllowFallback] = useState<boolean>(false);
  const [activeCategory, setActiveCategory] = useState<string>("全部");
  const [availabilityFilter, setAvailabilityFilter] = useState<"all" | "available" | "cached">("all");
  const [searchTerm, setSearchTerm] = useState<string>("");

  const [synthesizing, setSynthesizing] = useState<boolean>(false);
  const [audioUrl, setAudioUrl] = useState<string | null>(null);
  const audioUrlRef = useRef<string | null>(null);
  const [synthMeta, setSynthMeta] = useState<Omit<SynthesizeSpeechMeta, "blob"> | null>(null);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const generationControllerRef = useRef<AbortController | null>(null);

  // 本地目录与样音拉取状态
  const [pullingLocal, setPullingLocal] = useState<boolean>(false);
  const [pullStatusMsg, setPullStatusMsg] = useState<string | null>(null);

  // 历史配音列表
  const [historyList, setHistoryList] = useState<AudioHistoryItem[]>([]);
  const [copiedId, setCopiedId] = useState<string | null>(null);

  // 音色试听状态
  const [auditioningVoice, setAuditioningVoice] = useState<string | null>(null);
  const auditionAudioRef = useRef<HTMLAudioElement | null>(null);
  const auditionControllerRef = useRef<AbortController | null>(null);
  const auditionUrlRef = useRef<string | null>(null);
  const [visibleVoiceLimit, setVisibleVoiceLimit] = useState(30);

  // 声音设计弹窗状态
  const [showDesignModal, setShowDesignModal] = useState<boolean>(false);
  const [designPrompt, setDesignPrompt] = useState<string>(
    "沉稳磁性的中年男性纪录片旁白，语速平缓，声线浑厚富有深度",
  );
  const [designing, setDesigning] = useState<boolean>(false);
  const [designResult, setDesignResult] = useState<{ voice_id: string; preview_url: string } | null>(null);

  // 声音克隆弹窗状态
  const [showCloneModal, setShowCloneModal] = useState<boolean>(false);
  const [cloneAudioUrl, setCloneAudioUrl] = useState<string>("");
  const [cloning, setCloning] = useState<boolean>(false);
  const [cloneVoiceId, setCloneVoiceId] = useState<string | null>(null);

  // 云端 API Key 配置弹窗状态
  const [showKeyModal, setShowKeyModal] = useState<boolean>(false);
  const [keyProvider, setKeyProvider] = useState<CloudKeyProviderId>("siliconflow");
  const [cloningCustomId, setCloningCustomId] = useState<string | null>(null);

  // 主音频播放控制
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const [isPlaying, setIsPlaying] = useState<boolean>(false);
  const [sideTab, setSideTab] = useState<"library" | "history">("library");

  useEffect(() => {
    loadVoices();
    loadHistory();
  }, []);

  useEffect(() => () => {
    generationControllerRef.current?.abort();
    auditionControllerRef.current?.abort();
    if (audioUrlRef.current) URL.revokeObjectURL(audioUrlRef.current);
    if (auditionUrlRef.current) URL.revokeObjectURL(auditionUrlRef.current);
  }, []);

  const loadVoices = async () => {
    setLoadingVoices(true);
    setVoiceLoadError(null);
    try {
      const res = await fetchVoices();
      const list = res.voices || [];
      setVoices(list);
      setProvidersSummary(res.providers || {});
      setCachedLocalTotal(res.cached_local_total ?? list.filter((v) => v.cached_local).length);
      if (list.length > 0 && !list.some((v) => v.id === selectedVoice)) {
        const preferred = list.find((v) => v.available !== false) || list[0];
        setSelectedVoice(preferred.id);
      }
    } catch (err: any) {
      setVoiceLoadError(err.message || "音色列表加载失败，请检查网关连接后重试。");
    } finally {
      setLoadingVoices(false);
    }
  };

  const loadHistory = async () => {
    try {
      const res = await fetchAudioHistory();
      setHistoryList(res.records || []);
    } catch {
      // ignore
    }
  };

  const handlePullLocal = async () => {
    setPullingLocal(true);
    setErrorMsg(null);
    setPullStatusMsg(null);
    try {
      const res = await pullLocalVoices({
        include_samples: true,
        refresh_live: true,
        limit: 24,
      });
      const warnSuffix =
        res.errors && res.errors.length > 0 ? `（部分警告: ${res.errors[0]}）` : "";
      setPullStatusMsg(
        `已完成本地拉取：目录共 ${res.catalog_total} 个音色（Edge 实时同步 ${res.edge_live_synced}），本次新缓存 ${res.pulled_samples} 个样音、累计已缓存 ${res.cached_total} 个${warnSuffix}`,
      );
      await loadVoices();
    } catch (err: any) {
      setErrorMsg(`拉取本地音色失败: ${err.message || "网络或服务异常"}`);
    } finally {
      setPullingLocal(false);
    }
  };

  const speakWithBrowserFallback = (text: string) => {
    if (typeof window === "undefined" || !("speechSynthesis" in window)) {
      setErrorMsg("当前浏览器不支持 Web Speech API 本地系统朗读");
      return;
    }
    window.speechSynthesis.cancel();
    const utter = new SpeechSynthesisUtterance(text);
    utter.lang = "zh-CN";
    utter.rate = Math.min(Math.max(speed, 0.5), 2.0);
    window.speechSynthesis.speak(utter);
  };

  const handleSynthesize = async () => {
    if (!inputText.trim()) return;
    generationControllerRef.current?.abort();
    const controller = new AbortController();
    generationControllerRef.current = controller;
    setSynthesizing(true);
    setErrorMsg(null);
    setSynthMeta(null);
    try {
      const result = await synthesizeSpeechWithMeta(
        inputText,
        selectedVoice,
        speed,
        "tts-1",
        controller.signal,
        { allowFallback },
      );
      if (controller.signal.aborted) return;
      const url = URL.createObjectURL(result.blob);
      if (audioUrlRef.current) URL.revokeObjectURL(audioUrlRef.current);
      audioUrlRef.current = url;
      setAudioUrl(url);
      setSynthMeta({
        provider: result.provider,
        voice: result.voice,
        fallback: result.fallback,
        fallbackReason: result.fallbackReason,
      });
      setIsPlaying(true);
      setTimeout(() => {
        if (audioRef.current) {
          audioRef.current.play().catch(() => {});
        }
      }, 100);
      loadHistory();
    } catch (err: any) {
      if (err.name !== "AbortError") setErrorMsg(err.message || "语音合成失败");
    } finally {
      if (generationControllerRef.current === controller) {
        generationControllerRef.current = null;
        setSynthesizing(false);
      }
    }
  };

  const cancelSynthesis = () => generationControllerRef.current?.abort();

  const stopAudition = () => {
    auditionControllerRef.current?.abort();
    if (auditionAudioRef.current) {
      auditionAudioRef.current.pause();
      auditionAudioRef.current.removeAttribute("src");
      auditionAudioRef.current.load();
    }
    if (auditionUrlRef.current) URL.revokeObjectURL(auditionUrlRef.current);
    auditionUrlRef.current = null;
    auditionControllerRef.current = null;
    setAuditioningVoice(null);
  };

  // 一键试听指定音色（优先命中本地 voice_samples 缓存；未缓存时实时合成并落盘缓存）
  const handleAudition = async (v: VoiceDescriptor, e?: React.MouseEvent) => {
    if (e) e.stopPropagation();
    if (auditioningVoice === v.id) {
      stopAudition();
      return;
    }

    if (v.available === false && !v.cached_local) {
      setErrorMsg(v.status_reason || `音色 ${v.name} 所属引擎当前未就绪，无法试听。`);
      return;
    }

    auditionControllerRef.current?.abort();
    if (auditionUrlRef.current) URL.revokeObjectURL(auditionUrlRef.current);
    auditionAudioRef.current?.pause();
    const controller = new AbortController();
    auditionControllerRef.current = controller;
    setAuditioningVoice(v.id);
    setErrorMsg(null);
    try {
      const preview = await fetchVoicePreview(v.id, v.preview_url, controller.signal);
      if (controller.signal.aborted) return;
      if (preview.cacheStatus === "stored" && !v.cached_local) {
        setVoices((prev) =>
          prev.map((item) => (item.id === v.id ? { ...item, cached_local: true } : item)),
        );
        setCachedLocalTotal((count) => count + 1);
      }
      const sampleUrl = URL.createObjectURL(preview.blob);
      auditionUrlRef.current = sampleUrl;
      if (auditionAudioRef.current) {
        auditionAudioRef.current.src = sampleUrl;
        await auditionAudioRef.current.play();
      }
    } catch (err: any) {
      if (err.name !== "AbortError") setErrorMsg(`试听失败: ${err.message || "音色服务异常"}`);
      if (auditionControllerRef.current === controller) setAuditioningVoice(null);
    }
  };

  const handleVoiceDesignSubmit = async () => {
    if (!designPrompt.trim()) return;
    setDesigning(true);
    setErrorMsg(null);
    try {
      const res = await voiceDesign(designPrompt);
      setDesignResult(res);
      const newVoice: VoiceDescriptor = {
        id: res.voice_id,
        name: `定制音色: ${designPrompt.slice(0, 8)}...`,
        provider: "dashscope",
        category: "百炼精品",
        gender: "male",
        sample_text: designPrompt,
        tags: ["自然语言捏声音", "定制"],
        preview_url: res.preview_url,
        available: true,
      };
      setVoices((prev) => [newVoice, ...prev]);
      setSelectedVoice(res.voice_id);
    } catch (err: any) {
      setErrorMsg(err.message || "声音设计失败");
    } finally {
      setDesigning(false);
    }
  };

  const handleVoiceCloneSubmit = async () => {
    if (!cloneAudioUrl.trim()) return;
    setCloning(true);
    setErrorMsg(null);
    try {
      const res = await voiceClone(cloneAudioUrl);
      setCloneVoiceId(res.voice_id);
      const newVoice: VoiceDescriptor = {
        id: res.voice_id,
        name: `克隆音色: ${res.voice_id.slice(-6)}`,
        provider: "dashscope",
        category: "百炼精品",
        gender: "female",
        sample_text: "这是使用声音复刻合成的自定义人声音频。",
        tags: ["音频声音复刻", "克隆"],
        available: true,
      };
      setVoices((prev) => [newVoice, ...prev]);
      setSelectedVoice(res.voice_id);
    } catch (err: any) {
      setErrorMsg(err.message || "声音克隆失败");
    } finally {
      setCloning(false);
    }
  };

  const openCloudKeyModal = (prov: CloudKeyProviderId) => {
    setKeyProvider(prov);
    setShowKeyModal(true);
  };

  const handleCloneCachedCustomVoice = async (v: VoiceDescriptor, e: React.MouseEvent) => {
    e.stopPropagation();
    setCloningCustomId(v.id);
    setErrorMsg(null);
    try {
      const res = await cloneCustomVoice(v.id, { clone_engine: "auto", transcript: v.sample_text });
      if (res.status === "cloned") {
        setPullStatusMsg(`✅ 已完成云端音色克隆：${v.name}，现在可直接用于配音与短剧角色。`);
      } else {
        setPullStatusMsg(res.custom_voice?.error || "当前未检测到 SILICONFLOW_API_KEY 或 DASHSCOPE_API_KEY，请先配置云端 Key 后再试。");
      }
      await loadVoices();
    } catch (err: any) {
      setErrorMsg(err?.message || "云端复刻失败");
    } finally {
      setCloningCustomId(null);
    }
  };

  const handleDeleteCachedCustomVoice = async (v: VoiceDescriptor, e: React.MouseEvent) => {
    e.stopPropagation();
    try {
      await deleteCustomVoice(v.id);
      if (selectedVoice === v.id) setSelectedVoice("zh-CN-XiaoxiaoNeural");
      await loadVoices();
    } catch (err: any) {
      setErrorMsg(err?.message || "删除自定义音色失败");
    }
  };

  const localSummary = providersSummary.local_tts;
  const localReady = localSummary?.available === true;
  const localConfigured = localSummary?.configured === true || localReady;
  const sfReady = providersSummary.siliconflow?.available === true;
  const dsReady = providersSummary.dashscope?.available === true;

  const displayableVoices = localConfigured
    ? voices
    : voices.filter((v) => v.provider !== "local_tts");

  const rawCategories = Array.from(new Set(displayableVoices.map((v) => v.category).filter(Boolean)));
  const categories = ["全部", "短剧常用", "系统克隆库", ...rawCategories.filter((c) => c !== "系统克隆库")];

  const filteredVoices = displayableVoices.filter((v) => {
    if (activeCategory === "短剧常用") {
      if (!v.tags?.includes("短剧常用") && v.provider !== "custom") return false;
    } else if (activeCategory !== "全部" && v.category !== activeCategory) {
      return false;
    }
    if (availabilityFilter === "available" && v.available === false) {
      return false;
    }
    if (availabilityFilter === "cached" && !v.cached_local) {
      return false;
    }
    if (searchTerm) {
      const q = searchTerm.toLowerCase();
      return (
        v.name.toLowerCase().includes(q) ||
        v.id.toLowerCase().includes(q) ||
        v.provider.toLowerCase().includes(q) ||
        (v.tags && v.tags.some((t) => t.toLowerCase().includes(q)))
      );
    }
    return true;
  });
  const visibleVoices = filteredVoices.slice(0, visibleVoiceLimit);

  const presetLines = [
    {
      label: "🎬 影视解说开场 (晓晓)",
      text: "注意看，眼前这个女人叫小美，就在刚刚，她发现了整个控制台里隐藏的秘密...",
      voice: localReady ? "zf_xiaoxiao" : "zh-CN-XiaoxiaoNeural",
    },
    {
      label: "🎙️ 沉浸叙事男主 (云希)",
      text: "在这个被阴云笼罩的城市边缘，所有线索都在悄无声息地指向那个未知的终点...",
      voice: localReady ? "zm_yunxi" : "zh-CN-YunxiNeural",
    },
    {
      label: "💼 权威霸总宣判 (云扬)",
      text: "全场肃静，今天这笔投资合同，签还是不签，你只有这一次选择的机会。",
      voice: localReady ? "zm_yunyang" : "zh-CN-YunyangNeural",
    },
    {
      label: "⚔️ 热血武侠少年 (云健)",
      text: "我辈修仙之人，何惧一战！今日便以此剑斩破虚妄！",
      voice: localReady ? "zm_yunjian" : "zh-CN-YunjianNeural",
    },
    {
      label: "🍢 东北老铁唠嗑 (晓北)",
      text: "哎呀妈呀，老铁你可算来了，今天必须整两句，老带劲了！",
      voice: "zh-CN-liaoning-XiaobeiNeural",
    },
  ];

  const selectedVoiceInfo = voices.find((v) => v.id === selectedVoice);
  const providerBadgeOf = (provider: string): { label: string; dot: string; pill: string } => {
    switch (provider) {
      case "local_tts":
        return { label: "自定义 TTS", dot: "bg-teal-500", pill: "z-pill-info" };
      case "edge_tts":
        return { label: "Edge", dot: "bg-emerald-500", pill: "z-pill-success" };
      case "siliconflow":
        return { label: "硅基流动", dot: "bg-violet-500", pill: "z-pill-info" };
      case "dashscope":
        return { label: "阿里百炼", dot: "bg-sky-500", pill: "z-pill-info" };
      case "chatgpt":
        return { label: "OpenAI", dot: "bg-amber-500", pill: "z-pill-warn" };
      case "custom":
        return { label: "克隆库", dot: "bg-indigo-500", pill: "z-pill-info" };
      default:
        return { label: provider, dot: "bg-muted-foreground", pill: "z-pill-info" };
    }
  };
  const selectedBadge = selectedVoiceInfo ? providerBadgeOf(selectedVoiceInfo.provider) : null;
  const statusItems: Array<{ label: string; ok: boolean; prov?: CloudKeyProviderId }> = [
    { label: "Edge-TTS", ok: true },
    { label: "硅基流动", ok: !!sfReady, prov: "siliconflow" },
    { label: "阿里百炼", ok: !!dsReady, prov: "dashscope" },
  ];

  return (
    <div className="flex-1 grid grid-cols-1 md:grid-cols-[380px_1fr] lg:grid-cols-[400px_1fr] min-h-0 h-full overflow-hidden">
      <audio
        ref={auditionAudioRef}
        onEnded={() => setAuditioningVoice(null)}
        onError={() => setAuditioningVoice(null)}
        className="hidden"
      />

      {/* 左栏：语音创作参数台（对齐 ImageStudio / VideoStudio 统一骨架） */}
      <aside className="z-surface border-b md:border-b-0 md:border-r z-border flex flex-col min-h-0 max-h-[56vh] md:max-h-none md:h-full overflow-hidden">
        {/* 左栏头部 */}
        <div className="p-3 border-b z-border flex items-center justify-between gap-2 shrink-0 bg-muted/10">
          <div className="flex items-center gap-2 min-w-0">
            <div className="p-1.5 bg-primary/10 rounded-lg text-primary shrink-0">
              <Mic className="w-4 h-4" />
            </div>
            <div className="min-w-0">
              <h2 className="text-xs font-semibold z-text flex items-center gap-1.5 truncate">
                <span>语音创作参数台</span>
                <span className="text-[10px] font-normal px-1.5 py-0.5 rounded bg-primary/10 text-primary shrink-0">
                  TTS Studio
                </span>
              </h2>
              <p className="text-[10px] z-text-muted truncate">免密 Edge · 硅基流动 · 阿里百炼</p>
            </div>
          </div>
          <div className="flex items-center gap-1.5 shrink-0">
            <a
              href="#/chat"
              className="flex items-center gap-1 px-2 py-1 text-[11px] font-medium rounded-md border border-primary/30 bg-primary/10 text-primary hover:bg-primary/20 transition-colors"
              title="前往 Chat 使用 /voice 爬虫工具"
            >
              <Globe className="w-3 h-3" />
              找音色
            </a>
            <button
              type="button"
              onClick={() => openCloudKeyModal(sfReady ? "dashscope" : "siliconflow")}
              className="flex items-center gap-1 px-2 py-1 text-[11px] font-medium rounded-md bg-amber-500/10 text-amber-700 dark:text-amber-400 border border-amber-500/25 hover:bg-amber-500/20 transition-colors cursor-pointer"
              title="配置云端 API Key"
            >
              <KeyRound className="w-3 h-3" />
              Key
            </button>
          </div>
        </div>

        {/* 左栏参数滚动区 */}
        <div className="flex-1 overflow-y-auto p-4 space-y-4 min-h-0 text-xs">
          {/* 当前选中音色卡片 */}
          <div className="p-card rounded-xl p-3 space-y-2.5">
            <div className="flex items-center justify-between gap-2">
              <span className="text-[11px] font-medium z-text-sec flex items-center gap-1.5">
                <Radio className="w-3.5 h-3.5 text-primary" />
                当前配音音色
              </span>
              <div className="flex items-center gap-1.5">
                {selectedBadge && (
                  <span className={`inline-flex items-center gap-1 px-1.5 py-0.5 rounded-full border text-[10px] font-medium ${selectedBadge.pill}`}>
                    <span className={`w-1.5 h-1.5 rounded-full ${selectedBadge.dot}`} />
                    {selectedBadge.label}
                  </span>
                )}
                {selectedVoiceInfo?.cached_local && (
                  <span className="inline-flex items-center gap-0.5 px-1.5 py-0.5 rounded-full border z-pill-success text-[10px] font-medium">
                    <Check className="w-2.5 h-2.5" />
                    已缓存
                  </span>
                )}
              </div>
            </div>

            <div className="flex items-center justify-between gap-2.5 rounded-lg border z-border bg-muted/30 px-2.5 py-2">
              <div className="min-w-0 flex-1">
                <p className="text-xs font-semibold z-text truncate">
                  {selectedVoiceInfo?.name || selectedVoice}
                </p>
                <p className="text-[10px] font-mono z-text-muted truncate mt-0.5">
                  {selectedVoiceInfo
                    ? `${selectedVoiceInfo.id}${selectedVoiceInfo.locale ? ` · ${selectedVoiceInfo.locale}` : ""}`
                    : "未在当前目录中，可在右侧音色库点选"}
                </p>
              </div>
              <button
                type="button"
                onClick={() => {
                  if (auditioningVoice === selectedVoice) stopAudition();
                  else if (selectedVoiceInfo) void handleAudition(selectedVoiceInfo);
                }}
                disabled={!selectedVoiceInfo}
                className={`inline-flex items-center gap-1 h-7 px-2.5 rounded-lg border text-[11px] font-medium transition-colors shrink-0 cursor-pointer disabled:opacity-45 disabled:cursor-not-allowed ${
                  auditioningVoice === selectedVoice
                    ? "z-primary-btn border-transparent"
                    : "z-border z-surface z-ghost-btn z-text"
                }`}
              >
                {auditioningVoice === selectedVoice ? (
                  <>
                    <Pause className="w-3 h-3" />
                    停止
                  </>
                ) : (
                  <>
                    <Play className="w-3 h-3" />
                    试听
                  </>
                )}
              </button>
            </div>
          </div>

          {/* 快捷台词模板 + 配音文案 */}
          <div className="space-y-2">
            <div className="flex items-center justify-between gap-2">
              <label htmlFor="tts-script-input" className="font-medium z-text flex items-center gap-1.5">
                <Sparkles className="w-3.5 h-3.5 text-primary" />
                <span>配音台词文案</span>
              </label>
              <div className="flex items-center gap-1.5 text-[11px] z-text-muted">
                <span className="font-mono-num tabular-nums">{inputText.length} 字</span>
                {inputText && (
                  <button
                    type="button"
                    onClick={() => setInputText("")}
                    title="清空文案"
                    aria-label="清空文案"
                    className="p-1 rounded-md hover:bg-muted z-text-muted hover:z-text transition-colors cursor-pointer"
                  >
                    <X className="w-3.5 h-3.5" />
                  </button>
                )}
                <PromptChatAssistant
                  studio="配音工坊"
                  targetLabel="配音文案"
                  targetKey="tts:script"
                  value={inputText}
                  onApply={setInputText}
                  context={`当前音色：${selectedVoice}；语速：${speed}x`}
                />
              </div>
            </div>

            <div className="flex items-center gap-1.5 overflow-x-auto scrollbar-none pb-0.5">
              {presetLines.map((p, idx) => (
                <button
                  key={idx}
                  type="button"
                  onClick={() => {
                    setInputText(p.text);
                    setSelectedVoice(p.voice);
                  }}
                  className="shrink-0 text-[11px] px-2.5 py-1 rounded-full border z-border z-surface z-text-muted hover:z-text hover:bg-muted/60 transition-colors cursor-pointer"
                >
                  {p.label}
                </button>
              ))}
            </div>

            <textarea
              id="tts-script-input"
              value={inputText}
              onChange={(e) => setInputText(e.target.value)}
              placeholder="输入需要朗读的台词或旁白，系统将按标点智能切片并发合成、无损拼接..."
              rows={7}
              aria-label="配音文案"
              className="w-full text-xs rounded-xl border z-border p-input p-3 focus:outline-none focus:ring-1 focus:ring-primary resize-none leading-relaxed z-text"
            />
          </div>

          {/* 语速与调度参数 */}
          <div className="p-card rounded-xl p-3 space-y-3">
            <div className="flex items-center justify-between">
              <label htmlFor="tts-speed" className="font-medium z-text flex items-center gap-1.5">
                <Sliders className="w-3.5 h-3.5 text-primary" />
                <span>语速与节奏</span>
              </label>
              <span className="font-mono-num text-[11px] font-semibold px-2 py-0.5 rounded-md border p-badge-indigo tabular-nums">
                {speed.toFixed(1)}x
              </span>
            </div>

            <div className="p-surface p-1 rounded-lg grid grid-cols-5 gap-1">
              {[0.8, 1.0, 1.2, 1.5, 1.8].map((s) => {
                const active = Math.abs(speed - s) < 0.01;
                return (
                  <button
                    key={s}
                    type="button"
                    onClick={() => setSpeed(s)}
                    className={`py-1 rounded-md text-[11px] font-mono-num tabular-nums transition-all cursor-pointer ${
                      active ? "p-seg-active font-semibold" : "z-text-muted z-ghost-btn"
                    }`}
                  >
                    {s.toFixed(1)}x
                  </button>
                );
              })}
            </div>

            <input
              id="tts-speed"
              type="range"
              min="0.5"
              max="2.0"
              step="0.1"
              value={speed}
              onChange={(e) => setSpeed(parseFloat(e.target.value))}
              className="w-full accent-indigo-500 cursor-pointer"
            />

            <label className="flex items-start gap-2 pt-2 border-t z-border text-[11px] z-text-muted cursor-pointer select-none leading-relaxed">
              <input
                type="checkbox"
                checked={allowFallback}
                onChange={(e) => setAllowFallback(e.target.checked)}
                className="mt-0.5 rounded accent-indigo-500 cursor-pointer"
              />
              <span>主引擎失败时允许跨引擎音色映射兜底（默认关闭，防止音色被静默替换）</span>
            </label>
          </div>
        </div>

        {/* 左栏底部固定生成按钮（与 ImageStudio / VideoStudio 一致） */}
        <div className="p-3 border-t z-border z-surface shrink-0 flex items-center gap-2">
          <button
            type="button"
            onClick={handleSynthesize}
            disabled={synthesizing || !inputText.trim()}
            className="flex-1 py-2.5 rounded-xl z-primary-btn text-xs font-semibold inline-flex items-center justify-center gap-1.5 cursor-pointer"
          >
            {synthesizing ? (
              <>
                <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                <span>正在合成配音...</span>
              </>
            ) : (
              <>
                <Volume2 className="w-3.5 h-3.5" />
                <span>开始生成配音</span>
              </>
            )}
          </button>
          {synthesizing && (
            <button
              type="button"
              onClick={cancelSynthesis}
              className="py-2.5 px-3 rounded-xl border z-border z-surface z-ghost-btn text-xs font-medium cursor-pointer"
            >
              取消
            </button>
          )}
        </div>
      </aside>

      {/* 右栏：点阵画布工作区（音色分类库 / 配音作品库 + 成片播放器） */}
      <div className="studio-grid-bg overflow-y-auto p-5 flex flex-col gap-4 min-h-0 text-xs">
        {/* 顶栏：双 Tab 切换 + 引擎状态徽章 + 工具按钮 */}
        <div className="flex flex-wrap items-center justify-between gap-2.5">
          <div className="flex flex-wrap items-center gap-2">
            <div role="tablist" className="p-surface p-1 rounded-xl inline-flex items-center gap-1">
              <button
                type="button"
                role="tab"
                aria-selected={sideTab === "library"}
                onClick={() => setSideTab("library")}
                className={`inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg cursor-pointer transition-all ${
                  sideTab === "library" ? "p-seg-active font-semibold" : "z-text-muted z-ghost-btn"
                }`}
              >
                <Layers size={13} />
                <span>音色分类库</span>
                <span className="font-mono-num opacity-80">({filteredVoices.length})</span>
              </button>
              <button
                type="button"
                role="tab"
                aria-selected={sideTab === "history"}
                onClick={() => setSideTab("history")}
                className={`inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg cursor-pointer transition-all ${
                  sideTab === "history" ? "p-seg-active font-semibold" : "z-text-muted z-ghost-btn"
                }`}
              >
                <History size={13} />
                <span>配音作品库</span>
                <span className="font-mono-num opacity-80">({historyList.length})</span>
              </button>
            </div>

            <div className="flex flex-wrap items-center gap-1.5">
              {statusItems.map((s) =>
                s.prov && !s.ok ? (
                  <button
                    key={s.label}
                    type="button"
                    onClick={() => openCloudKeyModal(s.prov!)}
                    title={`点击配置 ${s.label} API Key`}
                    className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full border z-pill-warn font-medium cursor-pointer hover:opacity-90 transition-opacity"
                  >
                    <span className="status-dot-amber" />
                    <span>{s.label} 未配置</span>
                  </button>
                ) : (
                  <span
                    key={s.label}
                    className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full border font-medium ${
                      s.ok ? "z-pill-success" : "z-pill-warn"
                    }`}
                  >
                    <span className={s.ok ? "status-dot-green" : "status-dot-amber"} />
                    <span>{s.label}</span>
                  </span>
                ),
              )}
              <span className="inline-flex items-center gap-1 px-2.5 py-1 rounded-full border z-pill-info font-mono-num">
                <HardDrive className="w-3 h-3" />
                <span>已缓存 {cachedLocalTotal}</span>
              </span>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-1.5">
            <button
              type="button"
              onClick={() => setShowDesignModal(true)}
              className="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer"
              title="自然语言文字设计音色"
            >
              <Wand2 className="w-3.5 h-3.5 text-purple-500" />
              <span>声音设计</span>
            </button>
            <button
              type="button"
              onClick={() => setShowCloneModal(true)}
              className="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer"
              title="上传参考音频复刻声音"
            >
              <AudioWaveform className="w-3.5 h-3.5 text-sky-500" />
              <span>声音克隆</span>
            </button>
            <button
              type="button"
              onClick={handlePullLocal}
              disabled={pullingLocal}
              className="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer disabled:opacity-60"
            >
              <DownloadCloud className={`w-3.5 h-3.5 ${pullingLocal ? "animate-bounce" : ""}`} />
              <span>{pullingLocal ? "拉取中..." : "缓存样音"}</span>
            </button>
            <button
              type="button"
              onClick={() => {
                void loadVoices();
                void loadHistory();
              }}
              disabled={loadingVoices}
              aria-label="刷新"
              className="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer disabled:opacity-60"
            >
              <RefreshCw className={`w-3.5 h-3.5 ${loadingVoices ? "animate-spin" : ""}`} />
              <span>刷新</span>
            </button>
          </div>
        </div>

        {/* 状态通知 / 降级 / 错误横幅 */}
        {pullStatusMsg && (
          <div role="status" className="p-3 rounded-xl border z-pill-success flex items-center justify-between gap-3">
            <span>{pullStatusMsg}</span>
            <button type="button" onClick={() => setPullStatusMsg(null)} className="shrink-0 underline cursor-pointer">
              关闭
            </button>
          </div>
        )}
        {synthMeta?.fallback && (
          <div role="status" className="p-3 rounded-xl border z-pill-warn flex items-start gap-2">
            <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5" />
            <span>
              目标音色（<code className="font-mono">{selectedVoice}</code>）未直接可用（{synthMeta.fallbackReason || "已启用降级"}），本次已显式路由至{" "}
              <strong>{synthMeta.provider}</strong> / <code className="font-mono">{synthMeta.voice}</code>。
            </span>
          </div>
        )}
        {errorMsg && (
          <div role="alert" className="p-3 rounded-xl border z-pill-danger flex flex-wrap items-center justify-between gap-2">
            <span>{errorMsg}</span>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => speakWithBrowserFallback(inputText)}
                className="px-2.5 py-1 rounded-lg border border-current/30 hover:bg-black/5 dark:hover:bg-white/5 cursor-pointer"
              >
                浏览器系统朗读试听
              </button>
              <button type="button" onClick={() => setErrorMsg(null)} className="underline cursor-pointer">
                关闭
              </button>
            </div>
          </div>
        )}

        {/* 最新合成成果播放器卡片 */}
        {audioUrl && (
          <div className="p-card rounded-xl border border-primary/30 bg-primary/5 p-3.5 space-y-2.5 max-w-3xl">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="flex flex-wrap items-center gap-2">
                <span className="inline-flex items-center gap-1.5 text-xs font-semibold text-primary">
                  <AudioWaveform className={`w-4 h-4 ${isPlaying ? "animate-pulse" : ""}`} />
                  {isPlaying ? "正在播放成品配音" : "配音合成完成"}
                </span>
                {synthMeta?.provider && (
                  <span className="px-2 py-0.5 rounded-full text-[10px] font-mono border z-pill-info">
                    {synthMeta.provider} · {synthMeta.voice} · {speed.toFixed(1)}x
                  </span>
                )}
              </div>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={async () => {
                    let stableAudioUrl = audioUrl;
                    if (stableAudioUrl.startsWith("blob:")) {
                      try {
                        const blob = await fetch(stableAudioUrl).then((r) => r.blob());
                        stableAudioUrl = await new Promise<string>((resolve) => {
                          const reader = new FileReader();
                          reader.onloadend = () => resolve(typeof reader.result === "string" ? reader.result : audioUrl);
                          reader.readAsDataURL(blob);
                        });
                      } catch {
                        /* retain */
                      }
                    }
                    dispatchDramaCanvas({
                      type: "audio",
                      title: inputText.slice(0, 18) || "配音工坊音频",
                      text: inputText,
                      audioUrl: stableAudioUrl,
                      voiceId: selectedVoice,
                      sourceView: "tts",
                    });
                  }}
                  className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg border z-border z-surface z-ghost-btn text-[11px] font-medium cursor-pointer"
                >
                  <Clapperboard className="w-3.5 h-3.5 text-primary" />
                  发送到短剧画布
                </button>
                <a
                  href={audioUrl}
                  download={`tts_${selectedVoice}_${Date.now()}.mp3`}
                  className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg z-primary-btn text-[11px] font-medium"
                >
                  <Download className="w-3.5 h-3.5" />
                  下载 MP3
                </a>
              </div>
            </div>
            <audio
              ref={audioRef}
              src={audioUrl}
              controls
              className="w-full h-10"
              onPlay={() => setIsPlaying(true)}
              onPause={() => setIsPlaying(false)}
              onEnded={() => setIsPlaying(false)}
            />
          </div>
        )}

        {sideTab === "library" ? (
          <>
            {/* 搜索与分类过滤工具栏 */}
            <div className="p-card rounded-xl p-3 space-y-2.5">
              <div className="flex flex-wrap items-center gap-2.5">
                <div className="relative flex-1 min-w-[200px]">
                  <Search className="w-3.5 h-3.5 absolute left-3 top-1/2 -translate-y-1/2 z-text-muted" />
                  <input
                    type="search"
                    placeholder="搜索音色名称、ID、引擎或风格标签..."
                    aria-label="搜索音色"
                    value={searchTerm}
                    onChange={(e) => setSearchTerm(e.target.value)}
                    className="w-full h-8 text-xs pl-8 pr-8 rounded-lg border z-border p-input focus:outline-none focus:ring-1 focus:ring-primary z-text"
                  />
                  {searchTerm && (
                    <button
                      type="button"
                      onClick={() => setSearchTerm("")}
                      aria-label="清空搜索"
                      className="absolute right-2.5 top-1/2 -translate-y-1/2 z-text-muted hover:z-text cursor-pointer"
                    >
                      <X className="w-3.5 h-3.5" />
                    </button>
                  )}
                </div>

                <div className="p-surface p-0.5 rounded-lg inline-flex items-center gap-0.5 text-[11px] shrink-0">
                  {([
                    ["all", "全部"],
                    ["available", "仅可用"],
                    ["cached", "已缓存"],
                  ] as const).map(([val, label]) => (
                    <button
                      key={val}
                      type="button"
                      onClick={() => setAvailabilityFilter(val)}
                      className={`px-2.5 py-1 rounded-md transition-all cursor-pointer ${
                        availabilityFilter === val
                          ? "p-seg-active font-medium"
                          : "z-text-muted z-ghost-btn"
                      }`}
                    >
                      {label}
                    </button>
                  ))}
                </div>
              </div>

              <div className="flex flex-wrap items-center gap-1.5">
                {categories.map((c) => (
                  <button
                    key={c}
                    type="button"
                    onClick={() => setActiveCategory(c)}
                    className={`px-2.5 py-1 text-[11px] rounded-full border whitespace-nowrap transition-all cursor-pointer ${
                      activeCategory === c
                        ? "p-chip-active font-medium"
                        : "z-border z-text-muted z-ghost-btn"
                    }`}
                  >
                    {c}
                  </button>
                ))}
              </div>
            </div>

            {voiceLoadError && (
              <div role="alert" className="p-3 rounded-xl border z-pill-danger flex items-center justify-between gap-3">
                <span>音色列表加载失败：{voiceLoadError}</span>
                <button
                  type="button"
                  onClick={loadVoices}
                  disabled={loadingVoices}
                  className="shrink-0 underline disabled:opacity-50 cursor-pointer"
                >
                  重试
                </button>
              </div>
            )}

            {loadingVoices && voices.length === 0 ? (
              <div className="p-card rounded-xl py-16 text-center z-text-muted space-y-2">
                <RefreshCw className="w-5 h-5 mx-auto animate-spin text-primary" />
                <p>正在加载音色目录...</p>
              </div>
            ) : !voiceLoadError && filteredVoices.length === 0 ? (
              <div className="p-card rounded-xl py-16 text-center z-text-muted space-y-2">
                <Layers className="w-6 h-6 mx-auto opacity-40" />
                <p>当前筛选条件下没有匹配的音色</p>
              </div>
            ) : (
              <>
                <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4 gap-2">
                  {visibleVoices.map((v) => {
                    const isSelected = selectedVoice === v.id;
                    const isAuditioning = auditioningVoice === v.id;
                    const isAvailable = v.available !== false;
                    const badge = providerBadgeOf(v.provider);
                    return (
                      <div
                        key={v.id}
                        role="radio"
                        aria-checked={isSelected}
                        tabIndex={0}
                        onClick={() => setSelectedVoice(v.id)}
                        onKeyDown={(e) => {
                          if (e.key === "Enter" || e.key === " ") {
                            e.preventDefault();
                            setSelectedVoice(v.id);
                          }
                        }}
                        title={v.status_reason || v.id}
                        className={`group p-card p-card-interactive rounded-xl p-2.5 flex flex-col justify-between gap-2 cursor-pointer transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/50 ${
                          isSelected
                            ? "p-card-selected"
                            : !isAvailable
                            ? "opacity-75 border-dashed"
                            : ""
                        }`}
                        style={{ contentVisibility: "auto", containIntrinsicSize: "96px" }}
                      >
                        <div className="space-y-1 min-w-0">
                          <div className="flex items-center justify-between gap-1.5">
                            <div className="flex items-center gap-1.5 min-w-0">
                              <span
                                className={
                                  isSelected
                                    ? "status-dot-indigo"
                                    : "w-2 h-2 rounded-full border z-border shrink-0 opacity-60"
                                }
                              />
                              <span className="text-xs font-semibold z-text truncate">{v.name}</span>
                            </div>
                            <div className="flex items-center gap-1 shrink-0">
                              {isSelected && (
                                <span className="px-1.5 py-0.25 rounded-full border p-badge-indigo text-[10px] font-medium">
                                  已选中
                                </span>
                              )}
                              {v.cached_local && (
                                <span
                                  className="inline-flex items-center px-1 py-0.5 rounded-full border z-pill-success text-[10px]"
                                  title="本地已缓存试听样音"
                                >
                                  <Check className="w-2.5 h-2.5" />
                                </span>
                              )}
                              <span className={`inline-flex items-center gap-1 px-1.5 py-0.25 rounded-full border text-[10px] ${badge.pill}`}>
                                <span className={`w-1.5 h-1.5 rounded-full ${badge.dot}`} />
                                {badge.label}
                              </span>
                            </div>
                          </div>

                          {!isAvailable && v.status_reason ? (
                            <p className="text-[11px] text-amber-600 dark:text-amber-400 truncate">
                              未就绪：{v.status_reason}
                            </p>
                          ) : v.sample_text ? (
                            <p className="text-[11px] z-text-muted truncate" title={v.sample_text}>
                              {v.sample_text}
                            </p>
                          ) : v.tags?.length ? (
                            <p className="text-[11px] z-text-muted truncate">
                              {v.tags.slice(0, 4).join(" · ")}
                            </p>
                          ) : (
                            <p className="text-[11px] z-text-muted truncate">{v.category || "通用音色"}</p>
                          )}
                        </div>

                        <div className="pt-1.5 border-t z-border flex items-center justify-between gap-1.5">
                          <span className="text-[10px] font-mono z-text-muted truncate min-w-0" title={v.id}>
                            {v.id}
                          </span>
                          <div className="flex items-center gap-1 shrink-0">
                            {v.provider === "custom" && !isAvailable && (
                              <button
                                type="button"
                                onClick={(e) => void handleCloneCachedCustomVoice(v, e)}
                                disabled={cloningCustomId === v.id}
                                className="px-1.5 py-0.5 rounded-md text-[10px] border z-border hover:bg-muted disabled:opacity-50 cursor-pointer"
                                title="上传本地缓存参考音至云端 CosyVoice 完成复刻"
                              >
                                {cloningCustomId === v.id ? "复刻中..." : "云端复刻"}
                              </button>
                            )}
                            {v.provider === "custom" && (
                              <button
                                type="button"
                                onClick={(e) => void handleDeleteCachedCustomVoice(v, e)}
                                className="p-1 rounded-md text-destructive hover:bg-destructive/10 cursor-pointer"
                                title="从系统克隆库移除"
                                aria-label="移除克隆音色"
                              >
                                <Trash2 className="w-3 h-3" />
                              </button>
                            )}
                            <button
                              type="button"
                              onClick={(e) => handleAudition(v, e)}
                              disabled={!isAvailable && !v.cached_local}
                              aria-label={isAuditioning ? "停止试听" : `试听 ${v.name}`}
                              className={`inline-flex items-center gap-1 h-6 px-2 rounded-md text-[11px] font-medium transition-colors cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed ${
                                isAuditioning
                                  ? "p-seg-active"
                                  : "border z-border z-surface z-ghost-btn text-primary"
                              }`}
                              title={v.cached_local ? "播放本地缓存试听样音" : isAvailable ? "试听并缓存音色样本" : v.status_reason || "当前引擎未就绪"}
                            >
                              {isAuditioning ? <Pause className="w-3 h-3" /> : <Play className="w-3 h-3" />}
                              <span>{isAuditioning ? "停止" : "试听"}</span>
                            </button>
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>

                {visibleVoices.length < filteredVoices.length && (
                  <button
                    type="button"
                    onClick={() => setVisibleVoiceLimit((limit) => limit + 30)}
                    className="w-full py-2.5 text-xs font-medium z-text-muted hover:z-text border z-border z-surface z-ghost-btn rounded-xl cursor-pointer transition-colors"
                  >
                    加载更多音色（剩余 {filteredVoices.length - visibleVoices.length} 个）
                  </button>
                )}
              </>
            )}
          </>
        ) : (
          <div className="space-y-3">
            {historyList.length === 0 ? (
              <div className="p-card rounded-xl py-16 text-center z-text-muted space-y-2">
                <History className="w-7 h-7 mx-auto opacity-40" />
                <p>暂无配音作品记录，在左侧生成配音后将自动入库</p>
              </div>
            ) : (
              <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 gap-2.5">
                {historyList.slice(0, 30).map((h) => (
                  <div
                    key={h.id}
                    className="p-card p-card-interactive rounded-xl p-2.5 flex flex-col justify-between gap-2"
                  >
                    <div className="space-y-1.5 min-w-0">
                      <div className="flex items-center justify-between gap-2">
                        <span className="font-semibold z-text text-xs truncate">
                          {h.voice_name || h.voice_id}
                        </span>
                        <span className="px-2 py-0.5 rounded-full border z-pill-info text-[10px] font-mono-num shrink-0">
                          {h.provider} · {h.speed}x
                        </span>
                      </div>
                      <p className="z-text-muted line-clamp-2 text-[11px] leading-relaxed" title={h.input_text}>
                        {h.input_text}
                      </p>
                    </div>
                    <div className="pt-2 border-t z-border flex items-center justify-between gap-2 text-[11px]">
                      <span className="font-mono-num text-[10px] z-text-muted truncate">
                        {h.created_at ? new Date(h.created_at).toLocaleString() : h.voice_id}
                      </span>
                      <div className="flex items-center gap-1.5 shrink-0">
                        <button
                          type="button"
                          onClick={() => {
                            setInputText(h.input_text);
                            setSelectedVoice(h.voice_id);
                            setCopiedId(h.id);
                            setTimeout(() => setCopiedId(null), 1500);
                          }}
                          className="inline-flex items-center gap-1 px-2 py-1 rounded-lg border z-border z-ghost-btn z-text-muted hover:z-text cursor-pointer"
                          title="复用文本与音色"
                        >
                          {copiedId === h.id ? (
                            <>
                              <Check size={12} className="text-emerald-500" />
                              <span>已填入</span>
                            </>
                          ) : (
                            <>
                              <Copy size={12} />
                              <span>复用</span>
                            </>
                          )}
                        </button>
                        {h.audio_url && (
                          <a
                            href={h.audio_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="inline-flex items-center gap-1 px-2 py-1 rounded-lg border z-border z-ghost-btn text-primary"
                            title="播放 / 下载音频"
                          >
                            <ExternalLink size={12} />
                            <span>播放</span>
                          </a>
                        )}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>

      {/* 声音设计弹窗 */}
      {showDesignModal && (
        <div className="fixed inset-0 z-50 bg-black/60 backdrop-blur-xs flex items-center justify-center p-4">
          <div className="p-card rounded-2xl border z-border max-w-lg w-full p-5 space-y-4 shadow-xl">
            <div className="flex items-center justify-between pb-2.5 border-b z-border">
              <h3 className="text-sm font-semibold flex items-center gap-2 z-text">
                <Wand2 className="w-4 h-4 text-purple-500" />
                自然语言文字捏声音 (Voice Design)
              </h3>
              <button
                type="button"
                onClick={() => setShowDesignModal(false)}
                className="p-1 rounded-lg hover:bg-muted z-text-muted hover:z-text cursor-pointer"
                aria-label="关闭"
              >
                <X className="w-4 h-4" />
              </button>
            </div>

            <p className="text-xs z-text-muted leading-relaxed">
              基于 CosyVoice 音色大模型，通过自然语言描述凭空生成全新的独有人声音色。
            </p>

            <div className="space-y-1.5">
              <div className="flex items-center justify-between gap-2">
                <label className="text-xs font-medium z-text">音色特征 Prompt 描述</label>
                <PromptChatAssistant
                  studio="声音设计"
                  targetLabel="音色描述"
                  targetKey="tts:voice-design"
                  value={designPrompt}
                  onApply={setDesignPrompt}
                  context="请润色对音色的自然语言描述，突出性别、年龄感、音域、音色质感、语速和情绪。"
                />
              </div>
              <textarea
                value={designPrompt}
                onChange={(e) => setDesignPrompt(e.target.value)}
                rows={4}
                className="w-full text-xs rounded-xl border z-border p-input p-3 z-text leading-relaxed"
              />
            </div>

            {designResult && (
              <div className="p-3 rounded-xl border z-pill-success text-xs space-y-1">
                <p className="font-medium">已生成并自动选中定制音色：{designResult.voice_id}</p>
              </div>
            )}

            <button
              type="button"
              onClick={handleVoiceDesignSubmit}
              disabled={designing || !designPrompt.trim()}
              className="w-full py-2.5 rounded-xl z-primary-btn text-xs font-semibold flex items-center justify-center gap-1.5 cursor-pointer"
            >
              {designing ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <Wand2 className="w-3.5 h-3.5" />}
              {designing ? "正在设计生成..." : "开始生成并绑定到当前音色"}
            </button>
          </div>
        </div>
      )}

      {/* 声音克隆弹窗 */}
      {showCloneModal && (
        <div className="fixed inset-0 z-50 bg-black/60 backdrop-blur-xs flex items-center justify-center p-4">
          <div className="p-card rounded-2xl border z-border max-w-lg w-full p-5 space-y-4 shadow-xl">
            <div className="flex items-center justify-between pb-2.5 border-b z-border">
              <h3 className="text-sm font-semibold flex items-center gap-2 z-text">
                <AudioWaveform className="w-4 h-4 text-sky-500" />
                音频声音复刻 (Voice Clone)
              </h3>
              <button
                type="button"
                onClick={() => setShowCloneModal(false)}
                className="p-1 rounded-lg hover:bg-muted z-text-muted hover:z-text cursor-pointer"
                aria-label="关闭"
              >
                <X className="w-4 h-4" />
              </button>
            </div>

            <p className="text-xs z-text-muted leading-relaxed">
              提供 10~20 秒干净人声音频直链，即可一键克隆该声音并纳入当前音色库。
            </p>

            <div className="space-y-1.5">
              <label className="text-xs font-medium z-text">参考音频直链 URL (mp3/wav)</label>
              <input
                type="text"
                value={cloneAudioUrl}
                onChange={(e) => setCloneAudioUrl(e.target.value)}
                placeholder="https://.../my_voice_sample.mp3"
                className="w-full text-xs rounded-xl border z-border p-input p-2.5 z-text"
              />
            </div>

            {cloneVoiceId && (
              <div className="p-3 rounded-xl border z-pill-success text-xs">
                已完成声音克隆并自动选中：{cloneVoiceId}
              </div>
            )}

            <button
              type="button"
              onClick={handleVoiceCloneSubmit}
              disabled={cloning || !cloneAudioUrl.trim()}
              className="w-full py-2.5 rounded-xl z-primary-btn text-xs font-semibold flex items-center justify-center gap-1.5 cursor-pointer"
            >
              {cloning ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <AudioWaveform className="w-3.5 h-3.5" />}
              {cloning ? "正在复刻声音..." : "开始音频复刻"}
            </button>
          </div>
        </div>
      )}

      <CloudKeyConfigModal
        open={showKeyModal}
        onClose={() => setShowKeyModal(false)}
        initialProvider={keyProvider}
        allowedProviders={["siliconflow", "dashscope", "local_tts"]}
        title="配置云端 TTS API Key（无需本地算力）"
        onSaved={(_prov, msg) => {
          setPullStatusMsg(msg);
          void loadVoices();
        }}
      />
    </div>
  );
}
