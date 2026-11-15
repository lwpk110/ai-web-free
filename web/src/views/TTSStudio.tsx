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
  const [visibleVoiceLimit, setVisibleVoiceLimit] = useState(24);

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

  // 一键试听指定音色（优先命中本地 voice_samples 缓存；未缓存时实时合成并落盘缓存）
  const handleAudition = async (v: VoiceDescriptor, e: React.MouseEvent) => {
    e.stopPropagation();
    if (auditioningVoice === v.id) {
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

  return (
    <div className="p-6 max-w-7xl mx-auto space-y-6">
      <audio
        ref={auditionAudioRef}
        onEnded={() => setAuditioningVoice(null)}
        onError={() => setAuditioningVoice(null)}
        className="hidden"
      />

      {/* 顶栏标题区 */}
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 pb-4 border-b z-border">
        <div>
          <div className="flex flex-wrap items-center gap-2">
            <div className="p-2 bg-primary/10 rounded-lg text-primary">
              <Mic className="w-5 h-5" />
            </div>
            <h1 className="text-2xl font-bold tracking-tight z-text">语音创作工坊 (TTS Studio)</h1>
            <span className="px-2.5 py-0.5 text-xs font-medium rounded-full bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border border-emerald-500/20">
              微软 Edge-TTS 云端免密
            </span>
            <button
              type="button"
              onClick={() => openCloudKeyModal("siliconflow")}
              className={`px-2.5 py-0.5 text-xs font-medium rounded-full border transition-colors ${
                sfReady
                  ? "bg-purple-500/10 text-purple-600 dark:text-purple-400 border-purple-500/20"
                  : "bg-muted text-muted-foreground z-border hover:border-purple-500/40"
              }`}
              title={sfReady ? "硅基流动 CosyVoice2 已就绪" : "点击配置 SILICONFLOW_API_KEY"}
            >
              {sfReady ? "硅基流动 CosyVoice2 已激活" : "硅基流动 · 需 API Key"}
            </button>
            <button
              type="button"
              onClick={() => openCloudKeyModal("dashscope")}
              className={`px-2.5 py-0.5 text-xs font-medium rounded-full border transition-colors ${
                dsReady
                  ? "bg-blue-500/10 text-blue-600 dark:text-blue-400 border-blue-500/20"
                  : "bg-muted text-muted-foreground z-border hover:border-blue-500/40"
              }`}
              title={dsReady ? "阿里云百炼 CosyVoice 已就绪" : "点击配置 DASHSCOPE_API_KEY"}
            >
              {dsReady ? "阿里百炼 CosyVoice 已激活" : "阿里百炼 · 需 API Key"}
            </button>
            {localConfigured && (
              <span
                className={`px-2.5 py-0.5 text-xs font-medium rounded-full border ${
                  localReady
                    ? "bg-teal-500/10 text-teal-600 dark:text-teal-400 border-teal-500/20"
                    : "bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/20"
                }`}
                title={localSummary?.last_error || localSummary?.endpoint || "自定义 OpenAI 兼容 TTS"}
              >
                {localReady ? "自定义 TTS 已连通" : "自定义 TTS 待检测"}
              </span>
            )}
            <span className="px-2.5 py-0.5 text-xs font-medium rounded-full bg-cyan-500/10 text-cyan-600 dark:text-cyan-400 border border-cyan-500/20">
              已缓存样音 {cachedLocalTotal}
            </span>
          </div>
          <p className="text-sm z-text-muted mt-1">
            全云端语音合成（免 Key 微软 Edge-TTS · 硅基流动 CosyVoice2 · 阿里云百炼 CosyVoice · ChatGPT），支持一键将云端音色目录与试听样音缓存至本地存储。
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <a
            href="#/chat"
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md border z-border hover:bg-muted/50 transition-colors z-text"
            title="前往 Chat 使用 /voice 爬虫工具搜索并克隆新音色写入系统库"
          >
            <Globe className="w-3.5 h-3.5 text-primary" />
            Chat 爬虫找音色
          </a>
          <button
            type="button"
            onClick={() => openCloudKeyModal(sfReady ? "dashscope" : "siliconflow")}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md bg-amber-500/10 text-amber-700 dark:text-amber-400 border border-amber-500/25 hover:bg-amber-500/20 transition-colors"
            title="导入硅基流动 / 阿里云百炼 / 自定义 OpenAI 兼容云端 TTS API Key"
          >
            <KeyRound className="w-3.5 h-3.5" />
            配置云端 API Key
          </button>
          <button
            type="button"
            onClick={handlePullLocal}
            disabled={pullingLocal}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md bg-emerald-500/10 text-emerald-700 dark:text-emerald-400 border border-emerald-500/25 hover:bg-emerald-500/20 transition-colors disabled:opacity-50"
            title="同步最新音色目录并拉取 24 个精选音色试听 MP3 到本地存储"
          >
            {pullingLocal ? (
              <RefreshCw className="w-3.5 h-3.5 animate-spin" />
            ) : (
              <HardDrive className="w-3.5 h-3.5" />
            )}
            {pullingLocal ? "正在拉取..." : "拉取音色到本地"}
          </button>
          <button
            onClick={() => setShowDesignModal(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md bg-purple-500/10 text-purple-600 dark:text-purple-400 border border-purple-500/20 hover:bg-purple-500/20 transition-colors"
          >
            <Wand2 className="w-3.5 h-3.5" />
            文字捏音色 (设计)
          </button>
          <button
            onClick={() => setShowCloneModal(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md bg-blue-500/10 text-blue-600 dark:text-blue-400 border border-blue-500/20 hover:bg-blue-500/20 transition-colors"
          >
            <AudioWaveform className="w-3.5 h-3.5" />
            音频克隆 (复刻)
          </button>
          <button
            onClick={() => {
              loadVoices();
              loadHistory();
            }}
            disabled={loadingVoices}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md border z-border hover:bg-muted/50 transition-colors z-text-muted disabled:opacity-50"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${loadingVoices ? "animate-spin" : ""}`} />
            刷新
          </button>
        </div>
      </div>

      {pullStatusMsg && (
        <div className="p-3 text-xs rounded-lg bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 border border-emerald-500/25 flex items-center justify-between gap-3">
          <span>{pullStatusMsg}</span>
          <button onClick={() => setPullStatusMsg(null)} className="shrink-0 hover:underline">
            关闭
          </button>
        </div>
      )}

      {synthMeta?.fallback && (
        <div className="p-3 text-xs rounded-lg bg-amber-500/10 text-amber-700 dark:text-amber-300 border border-amber-500/25 flex items-center gap-2">
          <AlertTriangle className="w-4 h-4 shrink-0" />
          <span>
            注意：目标音色（<code className="font-mono">{selectedVoice}</code>）未直接可用（{synthMeta.fallbackReason || "已启用降级"}），本次已显式路由至{" "}
            <strong>{synthMeta.provider}</strong> / <code className="font-mono">{synthMeta.voice}</code>。
          </span>
        </div>
      )}

      {errorMsg && (
        <div className="p-3 text-sm rounded-lg bg-destructive/10 text-destructive border border-destructive/20 flex flex-wrap items-center justify-between gap-2">
          <span>{errorMsg}</span>
          <div className="flex items-center gap-3">
            <button
              type="button"
              onClick={() => speakWithBrowserFallback(inputText)}
              className="text-xs px-2 py-0.5 rounded border border-destructive/30 hover:bg-destructive/10"
            >
              浏览器系统朗读试听
            </button>
            <button onClick={() => setErrorMsg(null)} className="text-xs hover:underline">
              关闭
            </button>
          </div>
        </div>
      )}

      {/* 常用预设快捷填入 */}
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs z-text-muted font-medium flex items-center gap-1">
          <Sparkles className="w-3.5 h-3.5 text-amber-500" />
          爆款文案预设：
        </span>
        {presetLines.map((p, idx) => (
          <button
            key={idx}
            onClick={() => {
              setInputText(p.text);
              setSelectedVoice(p.voice);
            }}
            className="text-xs px-2.5 py-1 rounded-full p-card border z-border hover:bg-muted/70 transition-colors z-text"
          >
            {p.label}
          </button>
        ))}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-12 gap-6">
        {/* 左侧：输入与播放控制区 */}
        <div className="lg:col-span-5 space-y-4">
          <div className="p-card rounded-xl border z-border p-4 shadow-sm space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-2 text-xs z-text-muted">
              <span className="font-semibold z-text flex items-center gap-1.5">
                <Sliders className="w-4 h-4 text-primary" />
                配音文本输入
              </span>
              <div className="flex items-center gap-2">
                <span>{inputText.length} 字符</span>
                {inputText && (
                  <button type="button" onClick={() => setInputText("")} title="清空文案" className="p-1 rounded hover:bg-muted z-text-muted">
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

            <textarea
              value={inputText}
              onChange={(e) => setInputText(e.target.value)}
              placeholder="请输入需要朗读的文案内容，系统将自动基于标点符号并发切片无损拼接..."
              rows={7}
              className="w-full text-xs rounded-lg border z-border p-input p-3 focus:outline-none focus:ring-1 focus:ring-primary resize-none font-sans leading-relaxed z-text"
            />
            <div className="space-y-2 pt-2 border-t z-border">
              <div className="flex items-center justify-between text-xs">
                <span className="z-text-muted">语速调节 ({speed}x)</span>
                <span className="font-mono text-xs text-primary">{speed}x</span>
              </div>
              <input
                type="range"
                min="0.5"
                max="2.0"
                step="0.1"
                value={speed}
                onChange={(e) => setSpeed(parseFloat(e.target.value))}
                className="w-full accent-primary h-1.5 bg-muted rounded-lg cursor-pointer"
              />
              <label className="flex items-center gap-2 pt-1 text-[11px] z-text-muted cursor-pointer select-none">
                <input
                  type="checkbox"
                  checked={allowFallback}
                  onChange={(e) => setAllowFallback(e.target.checked)}
                  className="rounded border z-border accent-primary"
                />
                <span>主引擎失败时允许跨引擎音色映射兜底（默认关闭，确保音色不静默替换）</span>
              </label>
            </div>

            <div className="flex gap-2">
            <button
              onClick={handleSynthesize}
              disabled={synthesizing || !inputText.trim()}
              className="flex-1 py-2.5 px-4 rounded-lg bg-primary text-primary-foreground font-medium text-sm hover:bg-primary/90 transition-all flex items-center justify-center gap-2 shadow disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {synthesizing ? (
                <><RefreshCw className="w-4 h-4 animate-spin" />正在合成...</>
              ) : (
                <><Volume2 className="w-4 h-4" />生成配音</>
              )}
            </button>
            {synthesizing && (
              <button type="button" onClick={cancelSynthesis} className="px-3 rounded-lg border z-border text-xs z-text-muted hover:bg-muted/50">
                取消
              </button>
            )}
            </div>
          </div>

          {/* 音频试听播放卡片 */}
          {audioUrl && (
            <div className="p-card rounded-xl border border-primary/30 p-4 shadow-sm space-y-3 bg-primary/5">
              <div className="flex items-center justify-between gap-2">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-xs font-medium text-primary flex items-center gap-1.5">
                    <AudioWaveform className="w-4 h-4" />
                    当前合成音频就绪
                  </span>
                  {synthMeta?.provider && (
                    <span className="px-1.5 py-0.5 rounded text-[10px] font-mono bg-primary/10 text-primary">
                      {synthMeta.provider} · {synthMeta.voice}
                    </span>
                  )}
                </div>
                <div className="flex items-center gap-3">
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
                    className="flex items-center gap-1 text-xs text-amber-600 dark:text-amber-400 hover:underline font-medium"
                  >
                    <Clapperboard className="w-3.5 h-3.5" />
                    发送到短剧画布
                  </button>
                  <a
                    href={audioUrl}
                    download={`tts_${selectedVoice}_${Date.now()}.mp3`}
                    className="flex items-center gap-1 text-xs text-primary hover:underline font-medium"
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
                className="w-full h-10 accent-primary"
                onPlay={() => setIsPlaying(true)}
                onPause={() => setIsPlaying(false)}
                onEnded={() => setIsPlaying(false)}
              />
            </div>
          )}

          {/* 配音历史作品墙 */}
          <div className="p-card rounded-xl border z-border p-4 shadow-sm space-y-3">
            <div className="flex items-center justify-between text-xs z-text-muted">
              <span className="font-semibold z-text flex items-center gap-1.5">
                <History className="w-4 h-4 text-primary" />
                配音生成历史 ({historyList.length})
              </span>
              <span className="text-[11px]">纯 Go 数据库持久化</span>
            </div>

            {historyList.length === 0 ? (
              <p className="text-xs z-text-muted py-4 text-center">暂无配音历史，合成后将自动入库</p>
            ) : (
              <div className="space-y-2 max-h-48 overflow-y-auto pr-1">
                {historyList.slice(0, 10).map((h) => (
                  <div
                    key={h.id}
                    className="p-2.5 rounded-lg border z-border p-card flex items-center justify-between gap-2 text-xs"
                  >
                    <div className="min-w-0 flex-1 space-y-0.5">
                      <div className="flex items-center gap-1.5">
                        <span className="font-medium text-primary text-[11px] truncate">
                          {h.voice_name || h.voice_id}
                        </span>
                        <span className="text-[10px] z-text-muted">
                          {h.provider} · {h.speed}x
                        </span>
                      </div>
                      <p className="z-text line-clamp-1 text-[11px] opacity-80" title={h.input_text}>
                        {h.input_text}
                      </p>
                    </div>

                    <div className="flex items-center gap-1 shrink-0">
                      <button
                        onClick={() => {
                          setInputText(h.input_text);
                          setSelectedVoice(h.voice_id);
                          setCopiedId(h.id);
                          setTimeout(() => setCopiedId(null), 1500);
                        }}
                        className="p-1 rounded hover:bg-muted text-muted-foreground"
                        title="复用文本与音色"
                      >
                        {copiedId === h.id ? <Check size={14} className="text-emerald-500" /> : <Copy size={14} />}
                      </button>
                      {h.audio_url && (
                        <a
                          href={h.audio_url}
                          target="_blank"
                          rel="noopener noreferrer"
                          className="p-1 rounded hover:bg-muted text-primary"
                          title="播放 / 外链"
                        >
                          <ExternalLink size={14} />
                        </a>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>

        {/* 右侧：音色库选择网格 */}
        <div className="lg:col-span-7 space-y-4">
          <div className="p-card rounded-xl border z-border p-4 shadow-sm space-y-4">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
              <div className="flex flex-wrap items-center gap-2">
                <Layers className="w-4 h-4 text-primary" />
                <h2 className="text-sm font-semibold z-text">音色分类库 ({filteredVoices.length})</h2>
                <div className="flex items-center rounded-md border z-border p-0.5 text-[11px]">
                  {(
                    [
                      ["all", "全部"],
                      ["available", "可用"],
                      ["cached", "已缓存"],
                    ] as const
                  ).map(([val, label]) => (
                    <button
                      key={val}
                      type="button"
                      onClick={() => setAvailabilityFilter(val)}
                      className={`px-2 py-0.5 rounded transition-colors ${
                        availabilityFilter === val
                          ? "bg-primary text-primary-foreground font-medium"
                          : "z-text-muted hover:z-text"
                      }`}
                    >
                      {label}
                    </button>
                  ))}
                </div>
              </div>

              {/* 搜索框 */}
              <div className="relative w-full sm:w-56">
                <Search className="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 z-text-muted" />
                <input
                  type="text"
                  placeholder="搜索音色名称或标签..."
                  value={searchTerm}
                  onChange={(e) => setSearchTerm(e.target.value)}
                  className="w-full text-xs pl-8 pr-3 py-1.5 rounded-md border z-border p-input focus:outline-none focus:ring-1 focus:ring-primary z-text"
                />
              </div>
            </div>

            {/* 分类切换 Tab */}
            <div className="flex items-center gap-1.5 overflow-x-auto pb-1 scrollbar-none border-b z-border text-xs">
              {categories.map((c) => (
                <button
                  key={c}
                  onClick={() => setActiveCategory(c)}
                  className={`px-3 py-1.5 rounded-md transition-colors whitespace-nowrap ${
                    activeCategory === c
                      ? "bg-primary text-primary-foreground font-medium"
                      : "z-text-muted hover:bg-muted/50 z-text"
                  }`}
                >
                  {c}
                </button>
              ))}
            </div>

            {voiceLoadError && (
              <div role="alert" className="flex items-center justify-between gap-3 rounded-md border border-destructive/20 bg-destructive/5 px-3 py-2 text-xs text-destructive">
                <span>音色列表加载失败：{voiceLoadError}</span>
                <button type="button" onClick={loadVoices} disabled={loadingVoices} className="shrink-0 underline disabled:opacity-50">
                  重试
                </button>
              </div>
            )}
            {loadingVoices && voices.length === 0 && (
              <p className="py-6 text-center text-xs z-text-muted">正在加载音色...</p>
            )}
            {!loadingVoices && !voiceLoadError && filteredVoices.length === 0 && (
              <p className="py-6 text-center text-xs z-text-muted">没有匹配的音色</p>
            )}

            {/* 音色卡片列表 */}
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 max-h-[500px] overflow-y-auto pr-1">
              {visibleVoices.map((v) => {
                const isSelected = selectedVoice === v.id;
                const isAuditioning = auditioningVoice === v.id;
                const isAvailable = v.available !== false;
                const providerBadge =
                  v.provider === "local_tts"
                    ? { label: "自定义 TTS", cls: "bg-teal-500/10 text-teal-600 dark:text-teal-400" }
                    : v.provider === "edge_tts"
                    ? { label: "Edge 官方", cls: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400" }
                    : v.provider === "siliconflow"
                    ? { label: "硅基流动", cls: "bg-purple-500/10 text-purple-600 dark:text-purple-400" }
                    : v.provider === "dashscope"
                    ? { label: "阿里百炼", cls: "bg-blue-500/10 text-blue-600 dark:text-blue-400" }
                    : v.provider === "chatgpt"
                    ? { label: "OpenAI", cls: "bg-amber-500/10 text-amber-600 dark:text-amber-400" }
                    : v.provider === "custom"
                    ? { label: "系统克隆库", cls: "bg-indigo-500/10 text-indigo-600 dark:text-indigo-400" }
                    : { label: v.provider, cls: "bg-muted text-muted-foreground" };

                return (
                  <div
                    key={v.id}
                    onClick={() => setSelectedVoice(v.id)}
                    title={v.status_reason || v.id}
                    className={`p-3 rounded-lg border text-left cursor-pointer transition-all relative flex flex-col justify-between gap-2 p-card ${
                      isSelected
                        ? "border-primary bg-primary/5 ring-1 ring-primary shadow-sm"
                        : isAvailable
                        ? "z-border hover:border-primary/40 hover:bg-muted/30"
                        : "z-border border-dashed opacity-70 hover:opacity-90"
                    }`}
                    style={{ contentVisibility: "auto", containIntrinsicSize: "150px" }}
                  >
                    <div>
                      <div className="flex items-start justify-between gap-2">
                        <span className="font-semibold text-xs z-text flex items-center gap-1.5">
                          <Radio
                            className={`w-3.5 h-3.5 shrink-0 ${
                              isSelected ? "text-primary fill-primary" : "z-text-muted"
                            }`}
                          />
                          {v.name}
                        </span>
                        <div className="flex items-center gap-1 shrink-0">
                          {v.cached_local && (
                            <span className="px-1.5 py-0.5 rounded text-[10px] bg-cyan-500/10 text-cyan-600 dark:text-cyan-400">
                              已缓存
                            </span>
                          )}
                          <span className={`px-1.5 py-0.5 rounded text-[10px] font-normal ${providerBadge.cls}`}>
                            {providerBadge.label}
                          </span>
                        </div>
                      </div>

                      {!isAvailable && v.status_reason && (
                        <p className="text-[10px] text-amber-600 dark:text-amber-400 mt-1 line-clamp-1">
                          未就绪：{v.status_reason}
                        </p>
                      )}

                      {v.sample_text && (
                        <p className="text-[11px] z-text-muted mt-1 line-clamp-2 leading-relaxed">
                          “{v.sample_text}”
                        </p>
                      )}
                    </div>

                    <div className="flex items-center justify-between pt-1 border-t z-border mt-1">
                      <div className="flex items-center gap-1 flex-wrap">
                        {v.tags &&
                          v.tags.slice(0, 2).map((t, tidx) => (
                            <span key={tidx} className="px-1 py-0.2 rounded bg-muted text-[10px] z-text-muted">
                              {t}
                            </span>
                          ))}
                      </div>

                      {/* 一键试听与自定义云端复刻 */}
                      <div className="flex items-center gap-1 shrink-0">
                        {v.provider === "custom" && !isAvailable && (
                          <button
                            type="button"
                            onClick={(e) => void handleCloneCachedCustomVoice(v, e)}
                            disabled={cloningCustomId === v.id}
                            className="px-1.5 py-0.5 rounded text-[10px] bg-purple-500/10 text-purple-600 dark:text-purple-400 hover:bg-purple-500/20 disabled:opacity-50"
                            title="上传本地缓存参考音至云端 CosyVoice 完成复刻"
                          >
                            {cloningCustomId === v.id ? "复刻中..." : "云端复刻"}
                          </button>
                        )}
                        {v.provider === "custom" && (
                          <button
                            type="button"
                            onClick={(e) => void handleDeleteCachedCustomVoice(v, e)}
                            className="p-1 rounded text-rose-500 hover:bg-rose-500/10"
                            title="从系统克隆库移除"
                          >
                            <Trash2 className="w-3 h-3" />
                          </button>
                        )}
                        <button
                          type="button"
                          onClick={(e) => handleAudition(v, e)}
                          disabled={!isAvailable && !v.cached_local}
                        className={`flex items-center gap-1 px-2 py-0.5 rounded text-[11px] transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
                          isAuditioning
                            ? "bg-amber-500 text-white font-medium"
                            : "bg-primary/10 text-primary hover:bg-primary/20"
                        }`}
                        title={
                          v.cached_local
                            ? "直接播放本地缓存试听样音"
                            : isAvailable
                            ? "试听并缓存音色预设样本"
                            : v.status_reason || "当前引擎未就绪"
                        }
                      >
                        {isAuditioning ? (
                          <>
                            <Pause className="w-3 h-3 animate-spin" />
                            试听中
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
                  </div>
                );
              })}
            </div>
            {visibleVoices.length < filteredVoices.length && (
              <button type="button" onClick={() => setVisibleVoiceLimit((limit) => limit + 24)} className="w-full py-2 text-xs z-text-muted hover:z-text border z-border rounded-md">
                加载更多音色（{filteredVoices.length - visibleVoices.length}）
              </button>
            )}
          </div>
        </div>
      </div>

      {/* 声音设计弹窗 */}
      {showDesignModal && (
        <div className="fixed inset-0 z-50 bg-black/60 backdrop-blur-xs flex items-center justify-center p-4">
          <div className="p-card rounded-xl border z-border max-w-lg w-full p-5 space-y-4 shadow-xl">
            <div className="flex items-center justify-between pb-2 border-b z-border">
              <h3 className="text-sm font-semibold flex items-center gap-2 z-text">
                <Wand2 className="w-4 h-4 text-purple-500" />
                自然语言文字捏声音 (Voice Design)
              </h3>
              <button onClick={() => setShowDesignModal(false)} className="text-xs z-text-muted hover:z-text">
                关闭
              </button>
            </div>

            <p className="text-xs z-text-muted leading-relaxed">
              基于 CosyVoice 音色大模型，纯通过文字描述凭空创造全新的独有人声音色。
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
                className="w-full text-xs rounded-lg border z-border p-input p-2.5 z-text"
              />
            </div>

            <button
              onClick={handleVoiceDesignSubmit}
              disabled={designing || !designPrompt.trim()}
              className="w-full py-2 rounded-lg bg-purple-600 text-white text-xs font-medium hover:bg-purple-700 transition-colors flex items-center justify-center gap-1.5 disabled:opacity-50"
            >
              {designing ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <Wand2 className="w-3.5 h-3.5" />}
              开始生成并绑定到当前音色
            </button>
          </div>
        </div>
      )}

      {/* 声音克隆弹窗 */}
      {showCloneModal && (
        <div className="fixed inset-0 z-50 bg-black/60 backdrop-blur-xs flex items-center justify-center p-4">
          <div className="p-card rounded-xl border z-border max-w-lg w-full p-5 space-y-4 shadow-xl">
            <div className="flex items-center justify-between pb-2 border-b z-border">
              <h3 className="text-sm font-semibold flex items-center gap-2 z-text">
                <AudioWaveform className="w-4 h-4 text-blue-500" />
                音频声音复刻 (Voice Clone)
              </h3>
              <button onClick={() => setShowCloneModal(false)} className="text-xs z-text-muted hover:z-text">
                关闭
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
                className="w-full text-xs rounded-lg border z-border p-input p-2 z-text"
              />
            </div>

            <button
              onClick={handleVoiceCloneSubmit}
              disabled={cloning || !cloneAudioUrl.trim()}
              className="w-full py-2 rounded-lg bg-blue-600 text-white text-xs font-medium hover:bg-blue-700 transition-colors flex items-center justify-center gap-1.5 disabled:opacity-50"
            >
              {cloning ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <AudioWaveform className="w-3.5 h-3.5" />}
              开始音频复刻
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
