import React, { useState, useEffect, useRef } from "react";
import {
  fetchVoices,
  synthesizeSpeech,
  voiceDesign,
  voiceClone,
  fetchAudioHistory,
  VoiceDescriptor,
  AudioHistoryItem,
} from "../api/client";
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
  ExternalLink,
} from "lucide-react";

export function TTSStudio(): React.JSX.Element {
  const [voices, setVoices] = useState<VoiceDescriptor[]>([]);
  const [selectedVoice, setSelectedVoice] = useState<string>("zh-CN-XiaoxiaoNeural");
  const [inputText, setInputText] = useState<string>(
    "注意看，眼前这个女人叫小美，就在刚刚，她发现了整个控制台里隐藏的秘密...",
  );
  const [speed, setSpeed] = useState<number>(1.0);
  const [activeCategory, setActiveCategory] = useState<string>("全部");
  const [searchTerm, setSearchTerm] = useState<string>("");

  const [synthesizing, setSynthesizing] = useState<boolean>(false);
  const [audioUrl, setAudioUrl] = useState<string | null>(null);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  // 历史配音列表
  const [historyList, setHistoryList] = useState<AudioHistoryItem[]>([]);
  const [copiedId, setCopiedId] = useState<string | null>(null);

  // 音色试听状态
  const [auditioningVoice, setAuditioningVoice] = useState<string | null>(null);
  const auditionAudioRef = useRef<HTMLAudioElement | null>(null);

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

  // 主音频播放控制
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const [isPlaying, setIsPlaying] = useState<boolean>(false);

  useEffect(() => {
    loadVoices();
    loadHistory();
  }, []);

  const loadVoices = async () => {
    try {
      const res = await fetchVoices();
      const list = res.voices || [];
      setVoices(list);
      if (list.length > 0 && !list.some((v) => v.id === selectedVoice)) {
        setSelectedVoice(list[0].id);
      }
    } catch (err: any) {
      console.error("Load voices failed", err);
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

  const handleSynthesize = async () => {
    if (!inputText.trim()) return;
    setSynthesizing(true);
    setErrorMsg(null);
    try {
      const blob = await synthesizeSpeech(inputText, selectedVoice, speed);
      const url = URL.createObjectURL(blob);
      if (audioUrl) {
        URL.revokeObjectURL(audioUrl);
      }
      setAudioUrl(url);
      setIsPlaying(true);
      setTimeout(() => {
        if (audioRef.current) {
          audioRef.current.play().catch(() => {});
        }
      }, 100);
      loadHistory();
    } catch (err: any) {
      setErrorMsg(err.message || "语音合成失败");
    } finally {
      setSynthesizing(false);
    }
  };

  // 一键试听指定音色的 sampleText
  const handleAudition = async (v: VoiceDescriptor, e: React.MouseEvent) => {
    e.stopPropagation();
    if (auditioningVoice === v.id) {
      if (auditionAudioRef.current) {
        auditionAudioRef.current.pause();
      }
      setAuditioningVoice(null);
      return;
    }

    setAuditioningVoice(v.id);
    try {
      const sampleText = v.sample_text || `你好，我是${v.name}，很高兴为你提供智能语音合成。`;
      const blob = await synthesizeSpeech(sampleText, v.id, 1.0);
      const sampleUrl = URL.createObjectURL(blob);
      if (auditionAudioRef.current) {
        auditionAudioRef.current.src = sampleUrl;
        auditionAudioRef.current.play().catch(() => {});
      }
    } catch (err: any) {
      setErrorMsg(`试听失败: ${err.message || "音色服务异常"}`);
      setAuditioningVoice(null);
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
        category: "自然情感",
        gender: "male",
        sample_text: designPrompt,
        tags: ["自然语言捏声音", "定制"],
        preview_url: res.preview_url,
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
        category: "自然情感",
        gender: "female",
        sample_text: "这是使用声音复刻合成的自定义人声音频。",
        tags: ["音频声音复刻", "克隆"],
      };
      setVoices((prev) => [newVoice, ...prev]);
      setSelectedVoice(res.voice_id);
    } catch (err: any) {
      setErrorMsg(err.message || "声音克隆失败");
    } finally {
      setCloning(false);
    }
  };

  const categories = ["全部", "影视解说", "特色搞怪", "地域方言", "自然情感", "外语英语"];

  const filteredVoices = voices.filter((v) => {
    if (activeCategory !== "全部" && v.category !== activeCategory) {
      return false;
    }
    if (searchTerm) {
      const q = searchTerm.toLowerCase();
      return (
        v.name.toLowerCase().includes(q) ||
        v.id.toLowerCase().includes(q) ||
        (v.tags && v.tags.some((t) => t.toLowerCase().includes(q)))
      );
    }
    return true;
  });

  const presetLines = [
    {
      label: "🎬 影视解说开场 (晓晓)",
      text: "注意看，眼前这个女人叫小美，就在刚刚，她发现了整个控制台里隐藏的秘密...",
      voice: "zh-CN-XiaoxiaoNeural",
    },
    {
      label: "🎙️ 沉浸叙事男主 (云希)",
      text: "在这个被阴云笼罩的城市边缘，所有线索都在悄无声息地指向那个未知的终点...",
      voice: "zh-CN-YunxiNeural",
    },
    {
      label: "💼 权威霸总宣判 (云扬)",
      text: "全场肃静，今天这笔投资合同，签还是不签，你只有这一次选择的机会。",
      voice: "zh-CN-YunyangNeural",
    },
    {
      label: "🐒 齐天大圣 (剪映猴哥)",
      text: "俺老孙来也！妖怪，休得无礼，快快还我师傅！",
      voice: "zh_male_houge",
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
          <div className="flex items-center gap-2">
            <div className="p-2 bg-primary/10 rounded-lg text-primary">
              <Mic className="w-5 h-5" />
            </div>
            <h1 className="text-2xl font-bold tracking-tight z-text">语音创作工坊 (TTS Studio)</h1>
            <span className="px-2.5 py-0.5 text-xs font-medium rounded-full bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border border-emerald-500/20">
              微软 Edge-TTS (免密无限制)
            </span>
            <span className="px-2.5 py-0.5 text-xs font-medium rounded-full bg-blue-500/10 text-blue-600 dark:text-blue-400 border border-blue-500/20">
              剪映 200+ 热门爆款音色
            </span>
          </div>
          <p className="text-sm z-text-muted mt-1">
            免登录聚合微软 Edge 原生引擎（晓晓、云希、云扬）、字节剪映抖音爆款库与 CosyVoice 语音大模型，支持长文本智能分句流式拼接。
          </p>
        </div>

        <div className="flex items-center gap-2">
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
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md border z-border hover:bg-muted/50 transition-colors z-text-muted"
          >
            <RefreshCw className="w-3.5 h-3.5" />
            刷新
          </button>
        </div>
      </div>

      {errorMsg && (
        <div className="p-3 text-sm rounded-lg bg-destructive/10 text-destructive border border-destructive/20 flex items-center justify-between">
          <span>{errorMsg}</span>
          <button onClick={() => setErrorMsg(null)} className="text-xs hover:underline">
            关闭
          </button>
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
            <div className="flex items-center justify-between text-xs z-text-muted">
              <span className="font-semibold z-text flex items-center gap-1.5">
                <Sliders className="w-4 h-4 text-primary" />
                配音文本输入
              </span>
              <span>{inputText.length} 字符（支持标点智能切片流式拼接）</span>
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
            </div>

            <button
              onClick={handleSynthesize}
              disabled={synthesizing || !inputText.trim()}
              className="w-full py-2.5 px-4 rounded-lg bg-primary text-primary-foreground font-medium text-sm hover:bg-primary/90 transition-all flex items-center justify-center gap-2 shadow disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {synthesizing ? (
                <>
                  <RefreshCw className="w-4 h-4 animate-spin" />
                  正在调用 Edge-TTS / 剪映并发切片合成...
                </>
              ) : (
                <>
                  <Volume2 className="w-4 h-4" />
                  立即生成配音音频
                </>
              )}
            </button>
          </div>

          {/* 音频试听播放卡片 */}
          {audioUrl && (
            <div className="p-card rounded-xl border border-primary/30 p-4 shadow-sm space-y-3 bg-primary/5">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium text-primary flex items-center gap-1.5">
                  <AudioWaveform className="w-4 h-4" />
                  当前合成音频就绪
                </span>
                <a
                  href={audioUrl}
                  download={`tts_${selectedVoice}_${Date.now()}.mp3`}
                  className="flex items-center gap-1 text-xs text-primary hover:underline font-medium"
                >
                  <Download className="w-3.5 h-3.5" />
                  下载 MP3
                </a>
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
              <div className="flex items-center gap-2">
                <Layers className="w-4 h-4 text-primary" />
                <h2 className="text-sm font-semibold z-text">音色分类库 ({filteredVoices.length})</h2>
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

            {/* 音色卡片列表 */}
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 max-h-[500px] overflow-y-auto pr-1">
              {filteredVoices.map((v) => {
                const isSelected = selectedVoice === v.id;
                const isAuditioning = auditioningVoice === v.id;
                return (
                  <div
                    key={v.id}
                    onClick={() => setSelectedVoice(v.id)}
                    className={`p-3 rounded-lg border text-left cursor-pointer transition-all relative flex flex-col justify-between gap-2 p-card ${
                      isSelected
                        ? "border-primary bg-primary/5 ring-1 ring-primary shadow-sm"
                        : "z-border hover:border-primary/40 hover:bg-muted/30"
                    }`}
                  >
                    <div>
                      <div className="flex items-start justify-between gap-2">
                        <span className="font-semibold text-xs z-text flex items-center gap-1.5">
                          <Radio
                            className={`w-3.5 h-3.5 ${
                              isSelected ? "text-primary fill-primary" : "z-text-muted"
                            }`}
                          />
                          {v.name}
                        </span>
                        <div className="flex items-center gap-1">
                          <span
                            className={`px-1.5 py-0.5 rounded text-[10px] font-normal ${
                              v.provider === "edge_tts"
                                ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
                                : v.provider === "jianying"
                                ? "bg-blue-500/10 text-blue-600 dark:text-blue-400"
                                : "bg-purple-500/10 text-purple-600 dark:text-purple-400"
                            }`}
                          >
                            {v.provider === "edge_tts" ? "Edge免密" : v.provider === "jianying" ? "剪映逆向" : v.provider}
                          </span>
                        </div>
                      </div>

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

                      {/* 一键试听按钮 */}
                      <button
                        type="button"
                        onClick={(e) => handleAudition(v, e)}
                        className={`flex items-center gap-1 px-2 py-0.5 rounded text-[11px] transition-colors ${
                          isAuditioning
                            ? "bg-amber-500 text-white font-medium"
                            : "bg-primary/10 text-primary hover:bg-primary/20"
                        }`}
                        title="试听音色预设样本"
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
                );
              })}
            </div>
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
              <label className="text-xs font-medium z-text">音色特征 Prompt 描述</label>
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
    </div>
  );
}
