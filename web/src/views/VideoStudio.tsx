import React, { useState, useEffect } from "react";
import {
  submitVideo,
  getVideoTask,
  fetchVideoTasks,
  fetchVideoModels,
  deleteVideoTask,
  clearFailedVideoTasks,
  expandVideoPrompt,
  fetchServerGallery,
  fetchAudioHistory,
  VideoTask,
  VideoModelInfo,
  VideoPromptExpandResponse,
  ServerGalleryWork,
  AudioHistoryItem,
} from "../api/client";
import {
  VIDEO_CAMERA_CATEGORIES,
  VIDEO_CAMERA_PRESETS,
  CameraCategory,
  VideoCameraPreset,
} from "../data/videoCameraPresets";
import { CameraMotionPreview } from "../components/CameraMotionPreview";
import { dispatchDramaCanvas } from "../lib/dramaCanvasBridge";
import {
  Film,
  Play,
  Download,
  Sparkles,
  Wand2,
  Loader2,
  Undo2,
  Languages,
  Camera,
  Plus,
  ChevronDown,
  ChevronUp,
  FolderOpen,
  Repeat2,
  Volume2,
  RefreshCw,
  Clock,
  Video,
  Image as ImageIcon,
  CheckCircle2,
  AlertCircle,
  ExternalLink,
  Layers,
  Sliders,
  AudioWaveform,
  Trash2,
  Upload,
  X,
} from "lucide-react";
import { PromptChatAssistant } from "../components/PromptChatAssistant";

export function VideoStudio(): React.JSX.Element {
  const [prompt, setPrompt] = useState<string>(
    "金色的落叶在秋风中缓缓飘落，阳光穿透薄雾照耀在古老小径上，4k高画质，电影级运镜缓慢向前推近",
  );
  const [imageUrl, setImageUrl] = useState<string>("");
  const [audioUrl, setAudioUrl] = useState<string>("");
  const [model, setModel] = useState<string>("THUDM/CogVideoX-5b");
  const [duration, setDuration] = useState<number>(5);
  const [modelsList, setModelsList] = useState<VideoModelInfo[]>([]);

  const [submitting, setSubmitting] = useState<boolean>(false);
  const [activeTask, setActiveTask] = useState<VideoTask | null>(null);
  const [tasks, setTasks] = useState<VideoTask[]>([]);
  const [historyFilter, setHistoryFilter] = useState<"all" | "succeeded" | "pending" | "failed">("all");
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [clearingFailed, setClearingFailed] = useState<boolean>(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const fileInputRef = React.useRef<HTMLInputElement | null>(null);

  // AI 扩写 / 润色与专业运镜风格状态
  const [enhancingAction, setEnhancingAction] = useState<"expand" | "polish" | null>(null);
  const [previousPrompt, setPreviousPrompt] = useState<string | null>(null);
  const [enhanceResult, setEnhanceResult] = useState<VideoPromptExpandResponse | null>(null);
  const [enhanceLang, setEnhanceLang] = useState<"zh" | "en">("zh");
  const [selectedCameraId, setSelectedCameraId] = useState<string | null>(null);
  const [hoveredCameraId, setHoveredCameraId] = useState<string | null>(null);
  const [cameraCategory, setCameraCategory] = useState<CameraCategory>("全部");
  const [showCameraPanel, setShowCameraPanel] = useState<boolean>(true);

  // 本项目素材库选择器（图片 / 语音 / 视频）
  const [assetPickerOpen, setAssetPickerOpen] = useState<boolean>(false);
  const [assetPickerTab, setAssetPickerTab] = useState<"images" | "audio" | "videos">("images");
  const [assetLoading, setAssetLoading] = useState<boolean>(false);
  const [galleryAssets, setGalleryAssets] = useState<ServerGalleryWork[]>([]);
  const [audioAssets, setAudioAssets] = useState<AudioHistoryItem[]>([]);

  const openAssetPicker = async (tab: "images" | "audio" | "videos") => {
    setAssetPickerTab(tab);
    setAssetPickerOpen(true);
    setAssetLoading(true);
    try {
      const [galleryRes, audioRes] = await Promise.all([
        fetchServerGallery({ pageSize: 40 }).catch(() => ({ items: [] })),
        fetchAudioHistory().catch(() => ({ records: [] })),
      ]);
      setGalleryAssets(galleryRes.items || []);
      setAudioAssets(audioRes.records || []);
    } finally {
      setAssetLoading(false);
    }
  };

  const handleReuseVideoTask = (t: VideoTask) => {
    if (t.prompt) setPrompt(t.prompt);
    if (t.model) setModel(t.model);
    if (t.duration) setDuration(t.duration);
    setImageUrl(t.image_url || "");
    setAudioUrl(t.audio_url || "");
    setAssetPickerOpen(false);
  };

  const selectedCameraPreset: VideoCameraPreset | undefined = VIDEO_CAMERA_PRESETS.find(
    (c) => c.id === selectedCameraId,
  );

  const filteredCameraPresets =
    cameraCategory === "全部"
      ? VIDEO_CAMERA_PRESETS
      : VIDEO_CAMERA_PRESETS.filter((c) => c.category === cameraCategory);

  const previewCameraPreset: VideoCameraPreset =
    VIDEO_CAMERA_PRESETS.find((c) => c.id === (hoveredCameraId || selectedCameraId)) ||
    filteredCameraPresets[0] ||
    VIDEO_CAMERA_PRESETS[0];

  const [musePreviewUrls, setMusePreviewUrls] = useState<Record<string, string>>(() => {
    try {
      const saved = localStorage.getItem("ai_web_free_muse_camera_previews");
      return saved ? (JSON.parse(saved) as Record<string, string>) : {};
    } catch {
      return {};
    }
  });
  const [museGeneratingMap, setMuseGeneratingMap] = useState<Record<string, boolean>>({});
  const [museProgressMap, setMuseProgressMap] = useState<Record<string, number>>({});
  const [museErrorMap, setMuseErrorMap] = useState<Record<string, string | null>>({});
  const taskCameraMapRef = React.useRef<Record<string, string>>({});

  const matchPresetForTask = (t: VideoTask): string | null => {
    if (taskCameraMapRef.current[t.task_id]) {
      return taskCameraMapRef.current[t.task_id];
    }
    const p = (t.prompt || "").trim();
    if (!p) return null;
    for (const preset of VIDEO_CAMERA_PRESETS) {
      if (
        p === preset.musePrompt ||
        p === preset.examplePromptEn ||
        p === preset.examplePromptZh ||
        p.includes(preset.termEn) ||
        p.includes(preset.insertClauseEn) ||
        p.includes(preset.insertClauseZh)
      ) {
        return preset.id;
      }
    }
    return null;
  };

  const syncPreviewUrlsFromTasks = (taskList: VideoTask[]) => {
    const updates: Record<string, string> = {};
    // 遍历（最新任务优先保留）
    for (const t of taskList) {
      if (t.status === "SUCCEEDED" && t.video_url) {
        const presetId = matchPresetForTask(t);
        if (presetId && !updates[presetId]) {
          updates[presetId] = t.video_url;
        }
      }
    }
    if (Object.keys(updates).length === 0) return;
    setMusePreviewUrls((prev) => {
      let changed = false;
      const next = { ...prev };
      for (const [k, v] of Object.entries(updates)) {
        if (!next[k] || taskCameraMapRef.current && Object.values(taskCameraMapRef.current).includes(k)) {
          if (next[k] !== v) {
            next[k] = v;
            changed = true;
          }
        }
      }
      if (!changed) return prev;
      try {
        localStorage.setItem("ai_web_free_muse_camera_previews", JSON.stringify(next));
      } catch {
        // ignore storage quota
      }
      return next;
    });
  };

  const handleGenerateMusePreview = async (preset: VideoCameraPreset) => {
    if (musePreviewUrls[preset.id] || museGeneratingMap[preset.id]) return;
    setSelectedCameraId(preset.id);
    setMuseGeneratingMap((prev) => ({ ...prev, [preset.id]: true }));
    setMuseProgressMap((prev) => ({ ...prev, [preset.id]: 10 }));
    setMuseErrorMap((prev) => ({ ...prev, [preset.id]: null }));

    const liveActionPrompt = preset.musePrompt;

    try {
      const res = await submitVideo(liveActionPrompt, undefined, 5, "muse-video");
      const taskId = res.task_id;
      const pollTimer = window.setInterval(async () => {
        try {
          const latest = await getVideoTask(taskId);
          setMuseProgressMap((prev) => ({
            ...prev,
            [preset.id]: latest.progress ?? 50,
          }));
          if (latest.status === "SUCCEEDED" && latest.video_url) {
            window.clearInterval(pollTimer);
            setMusePreviewUrls((prev) => {
              const next = { ...prev, [preset.id]: latest.video_url! };
              try {
                localStorage.setItem("ai_web_free_muse_camera_previews", JSON.stringify(next));
              } catch {
                // ignore storage quota
              }
              return next;
            });
            setMuseGeneratingMap((prev) => ({ ...prev, [preset.id]: false }));
            void loadTasks();
          } else if (latest.status === "FAILED") {
            window.clearInterval(pollTimer);
            setMuseErrorMap((prev) => ({
              ...prev,
              [preset.id]: latest.error_msg || "Muse AI 生成失败",
            }));
            setMuseGeneratingMap((prev) => ({ ...prev, [preset.id]: false }));
            void loadTasks();
          }
        } catch (pollErr: any) {
          window.clearInterval(pollTimer);
          setMuseErrorMap((prev) => ({
            ...prev,
            [preset.id]: pollErr?.message || "轮询 Muse 任务失败",
          }));
          setMuseGeneratingMap((prev) => ({ ...prev, [preset.id]: false }));
        }
      }, 2500);
    } catch (err: any) {
      setMuseErrorMap((prev) => ({
        ...prev,
        [preset.id]: err?.message || "提交 Muse 实拍任务失败",
      }));
      setMuseGeneratingMap((prev) => ({ ...prev, [preset.id]: false }));
    }
  };

  const handleEnhancePrompt = async (action: "expand" | "polish") => {
    const raw = prompt.trim();
    if (!raw || enhancingAction) return;
    setEnhancingAction(action);
    setErrorMsg(null);
    setPreviousPrompt(prompt);
    try {
      const hasChinese = /[\u4e00-\u9fff]/.test(raw);
      const res = await expandVideoPrompt({
        prompt: raw,
        action,
        camera_style: selectedCameraPreset
          ? `${selectedCameraPreset.nameZh} (${selectedCameraPreset.termEn})`
          : undefined,
        model,
        duration,
        has_image: Boolean(imageUrl.trim()),
      });
      const defaultLang: "zh" | "en" = hasChinese ? "zh" : "en";
      setEnhanceResult(res);
      setEnhanceLang(defaultLang);
      setPrompt(defaultLang === "zh" ? res.prompt_zh : res.prompt_en);
    } catch (err: any) {
      setErrorMsg(err?.message || (action === "polish" ? "AI 润色失败" : "AI 扩写失败"));
      setPreviousPrompt(null);
    } finally {
      setEnhancingAction(null);
    }
  };

  const handleSwitchEnhanceLang = (lang: "zh" | "en") => {
    if (!enhanceResult) return;
    setEnhanceLang(lang);
    setPrompt(lang === "zh" ? enhanceResult.prompt_zh : enhanceResult.prompt_en);
  };

  const handleUndoEnhance = () => {
    if (previousPrompt !== null) {
      setPrompt(previousPrompt);
      setPreviousPrompt(null);
      setEnhanceResult(null);
    }
  };

  const handleAppendCameraClause = (preset: VideoCameraPreset) => {
    const hasChinese = /[\u4e00-\u9fff]/.test(prompt) || !prompt.trim();
    const clause = hasChinese ? preset.insertClauseZh : preset.insertClauseEn;
    setSelectedCameraId(preset.id);
    setPrompt((prev) => {
      const trimmed = prev.trim();
      if (!trimmed) return clause;
      if (trimmed.includes(preset.termEn) || trimmed.includes(preset.nameZh)) return prev;
      return `${trimmed}${trimmed.endsWith("。") || trimmed.endsWith(".") ? " " : "，"}${clause}`;
    });
  };

  const handleApplyCameraExample = (preset: VideoCameraPreset) => {
    setSelectedCameraId(preset.id);
    setPreviousPrompt(prompt);
    setEnhanceResult({
      title_zh: preset.nameZh,
      camera_move: `${preset.termEn} / ${preset.nameZh}`,
      prompt_zh: preset.examplePromptZh,
      prompt_en: preset.examplePromptEn,
      action: "expand",
      source: "camera_preset",
    });
    setEnhanceLang("zh");
    setPrompt(preset.examplePromptZh);
    if (preset.recommendedModel) {
      setModel(preset.recommendedModel);
    }
  };

  const readImageFileToDataURL = (file: File) => {
    const reader = new FileReader();
    reader.onload = () => {
      if (typeof reader.result === "string") {
        setImageUrl(reader.result);
      }
    };
    reader.readAsDataURL(file);
  };

  const handlePasteImage = (e: React.ClipboardEvent) => {
    const items = e.clipboardData?.items;
    if (!items) return;
    for (let i = 0; i < items.length; i++) {
      const item = items[i];
      if (item.type.startsWith("image/")) {
        const file = item.getAsFile();
        if (file) {
          e.preventDefault();
          readImageFileToDataURL(file);
          return;
        }
      }
    }
  };

  useEffect(() => {
    loadModels();
    loadTasks();
  }, []);

  const loadModels = async () => {
    try {
      const models = await fetchVideoModels();
      if (models && models.length > 0) {
        setModelsList(models);
      }
    } catch {
      // ignore
    }
  };

  const loadTasks = async () => {
    try {
      const res = await fetchVideoTasks();
      const list = res.tasks || [];
      setTasks(list);
      syncPreviewUrlsFromTasks(list);
    } catch {
      // ignore
    }
  };

  // 轮询活跃任务状态
  useEffect(() => {
    if (!activeTask || activeTask.status === "SUCCEEDED" || activeTask.status === "FAILED") {
      return;
    }

    const timer = setInterval(async () => {
      try {
        const latest = await getVideoTask(activeTask.task_id);
        setActiveTask(latest);
        if (latest.status === "SUCCEEDED" || latest.status === "FAILED") {
          if (latest.status === "SUCCEEDED" && latest.video_url) {
            syncPreviewUrlsFromTasks([latest]);
          }
          loadTasks();
        }
      } catch (err) {
        console.error("Poll task failed", err);
      }
    }, 2500);

    return () => clearInterval(timer);
  }, [activeTask]);

  const handleSubmit = async () => {
    if (!prompt.trim() && !imageUrl.trim()) return;
    setSubmitting(true);
    setErrorMsg(null);

    try {
      const res = await submitVideo(
        prompt,
        imageUrl || undefined,
        duration,
        model,
        audioUrl || undefined,
      );
      if (selectedCameraId) {
        taskCameraMapRef.current[res.task_id] = selectedCameraId;
      }
      const initialTask: VideoTask = {
        task_id: res.task_id,
        provider: res.provider || "siliconflow",
        model,
        prompt,
        image_url: imageUrl || undefined,
        audio_url: audioUrl || undefined,
        duration,
        status: "PENDING",
        progress: 10,
        created_at: new Date().toISOString(),
      };
      setActiveTask(initialTask);
      setTasks((prev) => [initialTask, ...prev]);
    } catch (err: any) {
      setErrorMsg(err.message || "提交视频生成失败");
    } finally {
      setSubmitting(false);
    }
  };

  const currentModelMeta = modelsList.find((m) => m.id === model);

  return (
    <div className="p-6 max-w-7xl mx-auto space-y-6">
      {/* 顶栏 */}
      <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 pb-4 border-b z-border">
        <div>
          <div className="flex items-center gap-2">
            <div className="p-2 bg-primary/10 rounded-lg text-primary">
              <Film className="w-5 h-5" />
            </div>
            <h1 className="text-2xl font-bold tracking-tight z-text">视频创作工坊 (Video Studio)</h1>
            <span className="px-2.5 py-0.5 text-xs font-medium rounded-full bg-blue-500/10 text-blue-600 dark:text-blue-400 border border-blue-500/20">
              CogVideoX & Meta Muse
            </span>
            <span className="px-2.5 py-0.5 text-xs font-medium rounded-full bg-purple-500/10 text-purple-600 dark:text-purple-400 border border-purple-500/20">
              字节即梦 2.0
            </span>
          </div>
          <p className="text-sm z-text-muted mt-1">
            支持标准 OpenAI 异步视频接口（POST /v1/videos/generations），内置免算力号池、真实异步任务队列与本地持久化。
          </p>
        </div>

        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => void openAssetPicker("images")}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md border border-primary/30 bg-primary/10 text-primary hover:bg-primary/20 transition-colors"
          >
            <FolderOpen className="w-3.5 h-3.5" />
            项目素材库 (图/音/视频)
          </button>
          <button
            onClick={() => {
              loadTasks();
              loadModels();
            }}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md border z-border hover:bg-muted/50 transition-colors z-text-muted"
          >
            <RefreshCw className="w-3.5 h-3.5" />
            刷新列表与模型
          </button>
        </div>
      </div>

      {/* 模式说明条 */}
      <div className="p-3 text-xs rounded-lg bg-blue-500/10 text-blue-600 dark:text-blue-400 border border-blue-500/20 flex items-start gap-2">
        <Sparkles className="w-4 h-4 shrink-0 mt-0.5 text-blue-500" />
        <div>
          <span className="font-semibold">视频生成模式说明：</span>
          系统内置真实文生视频大模型通道与智能语义成片匹配引擎。当配置了真实 GPU 扩散秘钥（如 <code className="px-1.5 py-0.5 rounded bg-muted text-foreground font-mono">SILICONFLOW_API_KEY</code>）时将直接执行云端深度物理扩散生成；未配置 Key 时将根据您的 Prompt 关键词进行智能语义特征分析与切题高清成片交付，杜绝画不对题与黑屏。
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

      {/* 专业运镜风格提示词建议面板 */}
      <div className="p-card rounded-xl border z-border p-3.5 shadow-sm space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            <Camera className="w-4 h-4 text-amber-500" />
            <span className="text-xs font-semibold z-text">专业运镜风格提示词建议</span>
            <span className="text-[11px] z-text-muted hidden sm:inline">
              选中运镜后点击「AI 扩写 / 润色」可自动融入镜头调度，也可直接追加或套用双语示例
            </span>
          </div>
          <button
            type="button"
            onClick={() => setShowCameraPanel((v) => !v)}
            className="inline-flex items-center gap-1 text-[11px] z-text-muted hover:z-text px-2 py-1 rounded-md hover:bg-muted/60 transition-colors"
          >
            {showCameraPanel ? (
              <>
                收起建议 <ChevronUp className="w-3.5 h-3.5" />
              </>
            ) : (
              <>
                展开 ({VIDEO_CAMERA_PRESETS.length} 种运镜) <ChevronDown className="w-3.5 h-3.5" />
              </>
            )}
          </button>
        </div>

        {showCameraPanel && (
          <>
            <div className="flex flex-wrap items-center gap-1.5">
              {VIDEO_CAMERA_CATEGORIES.map((cat) => {
                const active = cameraCategory === cat;
                return (
                  <button
                    key={cat}
                    type="button"
                    onClick={() => setCameraCategory(cat)}
                    className={`text-[11px] px-2.5 py-1 rounded-full border transition-colors ${
                      active
                        ? "bg-primary text-primary-foreground border-transparent font-medium"
                        : "z-border hover:bg-muted/60 z-text-muted"
                    }`}
                  >
                    {cat}
                  </button>
                );
              })}
              {selectedCameraPreset && (
                <button
                  type="button"
                  onClick={() => setSelectedCameraId(null)}
                  className="ml-auto inline-flex items-center gap-1 text-[11px] px-2 py-0.5 rounded-full bg-amber-500/10 text-amber-600 dark:text-amber-400 border border-amber-500/20 hover:bg-amber-500/20 transition-colors"
                >
                  已选：{selectedCameraPreset.nameZh}
                  <X className="w-3 h-3" />
                </button>
              )}
            </div>

            <CameraMotionPreview
              preset={previewCameraPreset}
              museVideoUrl={musePreviewUrls[previewCameraPreset.id]}
              museGenerating={Boolean(museGeneratingMap[previewCameraPreset.id])}
              museProgress={museProgressMap[previewCameraPreset.id] || 0}
              museError={museErrorMap[previewCameraPreset.id] || null}
              onGenerateMusePreview={handleGenerateMusePreview}
              onAppendClause={handleAppendCameraClause}
              onApplyExample={handleApplyCameraExample}
            />

            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5 gap-2 max-h-[210px] overflow-y-auto pr-1">
              {filteredCameraPresets.map((preset) => {
                const isSelected = selectedCameraId === preset.id;
                return (
                  <div
                    key={preset.id}
                    onMouseEnter={() => setHoveredCameraId(preset.id)}
                    onMouseLeave={() => setHoveredCameraId(null)}
                    onClick={() =>
                      setSelectedCameraId((prev) => (prev === preset.id ? null : preset.id))
                    }
                    className={`group cursor-pointer rounded-lg border p-2.5 transition-all flex flex-col justify-between gap-1.5 ${
                      isSelected
                        ? "border-primary bg-primary/5 ring-1 ring-primary/30"
                        : "z-border hover:bg-muted/40"
                    }`}
                  >
                    <div className="space-y-0.5">
                      <div className="flex items-center justify-between gap-1">
                        <span className="text-xs font-semibold z-text truncate">
                          {musePreviewUrls[preset.id] ? "🎬 " : ""}
                          {preset.nameZh}
                        </span>
                        <span className="text-[10px] px-1.5 py-0.5 rounded bg-muted z-text-muted shrink-0">
                          {preset.category}
                        </span>
                      </div>
                      <div className="text-[10px] font-mono text-primary/80 truncate">
                        {preset.termEn}
                      </div>
                      <p className="text-[11px] z-text-muted line-clamp-2 leading-snug">
                        {preset.effectDesc}
                      </p>
                    </div>
                    <div className="flex items-center gap-1.5 pt-1 border-t z-border">
                      <button
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation();
                          handleAppendCameraClause(preset);
                        }}
                        className="flex-1 inline-flex items-center justify-center gap-0.5 text-[10px] py-1 px-1.5 rounded border z-border hover:bg-muted z-text transition-colors"
                        title="将此运镜词追加到当前提示词末尾"
                      >
                        <Plus className="w-2.5 h-2.5" />
                        追加运镜
                      </button>
                      <button
                        type="button"
                        onClick={(e) => {
                          e.stopPropagation();
                          handleApplyCameraExample(preset);
                        }}
                        className="flex-1 inline-flex items-center justify-center gap-0.5 text-[10px] py-1 px-1.5 rounded bg-primary/10 text-primary hover:bg-primary/20 font-medium transition-colors"
                        title="填入完整导演级双语示例并匹配推荐模型"
                      >
                        套用示例
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
          </>
        )}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-12 gap-6">
        {/* 左侧：参数与提交表单 */}
        <div className="lg:col-span-5 space-y-4">
          <div className="p-card rounded-xl border z-border p-4 shadow-sm space-y-4">
            <h2 className="text-sm font-semibold flex items-center justify-between z-text">
              <span className="flex items-center gap-2">
                <Video className="w-4 h-4 text-primary" />
                生成参数设置
              </span>
              {currentModelMeta?.tag && (
                <span className="text-[11px] font-normal px-2 py-0.5 rounded bg-primary/10 text-primary">
                  {currentModelMeta.tag}
                </span>
              )}
            </h2>

            {/* 模型选择 */}
            <div className="space-y-1.5">
              <label className="text-xs font-medium z-text">选择视频大模型</label>
              <select
                value={model}
                onChange={(e) => setModel(e.target.value)}
                className="w-full text-xs rounded-lg border z-border p-input p-2.5 focus:ring-1 focus:ring-primary z-text"
              >
                <optgroup label="智谱 / 硅基流动 CogVideoX (官方SLA · 免费额度)">
                  <option value="THUDM/CogVideoX-5b">THUDM/CogVideoX-5b (50亿参数 · 商业级文生视频)</option>
                  <option value="THUDM/CogVideoX-2b">THUDM/CogVideoX-2b (轻量极速版 · 5秒快速成片)</option>
                </optgroup>
                <optgroup label="Meta AI Muse (原生音视频协同 · 逆向号池)">
                  <option value="muse-video">Meta AI Muse (音画一体 · 自带环境音与配乐)</option>
                  <option value="muse-talk">Meta MuseTalk (角色数字人对白 · 精确口型驱动)</option>
                </optgroup>
                <optgroup label="字节即梦 AI (Seedance · 电影级)">
                  <option value="jimeng-video-2.0">字节即梦 2.0 (每日积分 · 复杂运动运镜)</option>
                </optgroup>
              </select>
              {currentModelMeta?.description && (
                <p className="text-[11px] z-text-muted mt-1 leading-normal">
                  {currentModelMeta.description}
                </p>
              )}
            </div>

            {/* Prompt + AI 扩写 / 润色 */}
            <div className="space-y-1.5">
              <div className="flex flex-wrap items-center justify-between gap-2 text-xs">
                <label className="font-medium z-text flex items-center gap-1.5">
                  <span>视频画面描述 (Prompt)</span>
                  {selectedCameraPreset && (
                    <span className="text-[10px] px-1.5 py-0.5 rounded bg-amber-500/10 text-amber-600 dark:text-amber-400 border border-amber-500/20 font-normal">
                      运镜: {selectedCameraPreset.nameZh}
                    </span>
                  )}
                </label>
                <div className="flex items-center gap-1.5">
                  <button
                    type="button"
                    onClick={() => handleEnhancePrompt("expand")}
                    disabled={!prompt.trim() || enhancingAction !== null}
                    title="调用本机号池将简短想法扩写为完整电影级视频提示词"
                    className="inline-flex items-center gap-1 px-2 py-0.5 rounded-md border z-border hover:bg-muted/70 text-[11px] font-medium z-text transition-colors disabled:opacity-45"
                  >
                    {enhancingAction === "expand" ? (
                      <Loader2 className="w-3 h-3 animate-spin text-primary" />
                    ) : (
                      <Sparkles className="w-3 h-3 text-amber-500" />
                    )}
                    <span>{enhancingAction === "expand" ? "扩写中..." : "AI 扩写"}</span>
                  </button>
                  <button
                    type="button"
                    onClick={() => handleEnhancePrompt("polish")}
                    disabled={!prompt.trim() || enhancingAction !== null}
                    title="保留核心画面，升级专业运镜术语与光影时间轴质感"
                    className="inline-flex items-center gap-1 px-2 py-0.5 rounded-md border z-border hover:bg-muted/70 text-[11px] font-medium z-text transition-colors disabled:opacity-45"
                  >
                    {enhancingAction === "polish" ? (
                      <Loader2 className="w-3 h-3 animate-spin text-primary" />
                    ) : (
                      <Wand2 className="w-3 h-3 text-purple-500" />
                    )}
                    <span>{enhancingAction === "polish" ? "润色中..." : "AI 润色"}</span>
                  </button>
                  <PromptChatAssistant
                    studio="视频工坊"
                    targetLabel="视频画面描述"
                    targetKey="video:prompt"
                    value={prompt}
                    onApply={setPrompt}
                    context={`视频模型：${model}；时长：${duration} 秒${selectedCameraPreset ? `；运镜：${selectedCameraPreset.nameZh} (${selectedCameraPreset.termEn})` : ""}`}
                  />
                </div>
              </div>
              <textarea
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                onPaste={handlePasteImage}
                placeholder="输入简短想法点击「AI 扩写」，或输入草稿点击「AI 润色」升级运镜与光影（支持 Ctrl+V 粘贴图片进行图生视频）..."
                rows={4}
                className="w-full text-xs rounded-lg border z-border p-input p-2.5 focus:ring-1 focus:ring-primary resize-none leading-relaxed z-text"
              />
              {enhanceResult && (
                <div className="flex flex-wrap items-center justify-between gap-2 px-2.5 py-1.5 rounded-lg bg-muted/40 border z-border text-[11px]">
                  <div className="flex items-center gap-1.5 min-w-0">
                    <span className="px-1.5 py-0.5 rounded bg-primary/10 text-primary font-medium shrink-0">
                      {enhanceResult.source === "ai_pool"
                        ? "号池 AI"
                        : enhanceResult.source === "camera_preset"
                          ? "运镜示例"
                          : "智能结构化"}
                    </span>
                    <span className="z-text-muted truncate" title={enhanceResult.camera_move}>
                      {enhanceResult.camera_move}
                    </span>
                  </div>
                  <div className="flex items-center gap-1.5 shrink-0">
                    <button
                      type="button"
                      onClick={() =>
                        handleSwitchEnhanceLang(enhanceLang === "zh" ? "en" : "zh")
                      }
                      className="inline-flex items-center gap-1 px-2 py-0.5 rounded border z-border hover:bg-muted z-text transition-colors"
                      title="在中英文双语版本间一键切换"
                    >
                      <Languages className="w-3 h-3 text-blue-500" />
                      {enhanceLang === "zh" ? "切至英文版" : "切至中文版"}
                    </button>
                    {previousPrompt !== null && (
                      <button
                        type="button"
                        onClick={handleUndoEnhance}
                        className="inline-flex items-center gap-1 px-2 py-0.5 rounded border z-border hover:bg-muted z-text-muted hover:z-text transition-colors"
                        title="恢复扩写/润色前的提示词"
                      >
                        <Undo2 className="w-3 h-3" />
                        撤销
                      </button>
                    )}
                  </div>
                </div>
              )}
            </div>

            {/* 参考图 URL / Ctrl+V 粘贴上传 (图生视频) */}
            <div className="space-y-2" onPaste={handlePasteImage}>
              <div className="flex items-center justify-between text-xs">
                <label className="font-medium z-text flex items-center gap-1">
                  <ImageIcon className="w-3.5 h-3.5 text-muted-foreground" />
                  起始参考图 (Ctrl+V 粘贴 / URL，图生视频)
                </label>
                <div className="flex items-center gap-2">
                  <button
                    type="button"
                    onClick={() => void openAssetPicker("images")}
                    className="inline-flex items-center gap-1 text-[11px] text-indigo-600 dark:text-indigo-400 hover:underline"
                  >
                    <FolderOpen className="w-3 h-3" />
                    从画廊选择
                  </button>
                  <button
                    type="button"
                    onClick={() => fileInputRef.current?.click()}
                    className="inline-flex items-center gap-1 text-[11px] text-primary hover:underline"
                  >
                    <Upload className="w-3 h-3" />
                    选择本地图片
                  </button>
                </div>
                <input
                  ref={fileInputRef}
                  type="file"
                  accept="image/*"
                  className="hidden"
                  onChange={(e) => {
                    const file = e.target.files?.[0];
                    if (file) {
                      readImageFileToDataURL(file);
                    }
                    e.target.value = "";
                  }}
                />
              </div>
              <input
                type="text"
                value={imageUrl.startsWith("data:") ? "" : imageUrl}
                onChange={(e) => setImageUrl(e.target.value)}
                onPaste={handlePasteImage}
                placeholder={
                  imageUrl.startsWith("data:")
                    ? "已从剪贴板/本地加载参考图像（可继续粘贴替换或点击下方清除）"
                    : "在此 Ctrl+V 粘贴截图/图片，或输入 https://.../keyframe.png"
                }
                className="w-full text-xs rounded-lg border z-border p-input p-2 focus:ring-1 focus:ring-primary z-text"
              />
              {imageUrl.trim() && (
                <div className="relative rounded-lg border z-border bg-muted/30 p-2 flex items-center gap-3">
                  <img
                    src={imageUrl}
                    alt="参考图预览"
                    className="w-16 h-16 object-cover rounded border z-border shrink-0 bg-black/10"
                  />
                  <div className="flex-1 min-w-0 text-[11px] z-text-muted">
                    <p className="font-medium z-text truncate">
                      {imageUrl.startsWith("data:") ? "已就绪：剪贴板 / 本地参考图像" : imageUrl}
                    </p>
                    <p>提交时将自动上传给 Muse 作为图生视频首帧参考</p>
                  </div>
                  <button
                    type="button"
                    onClick={() => setImageUrl("")}
                    title="移除参考图"
                    className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-destructive transition-colors"
                  >
                    <X className="w-4 h-4" />
                  </button>
                </div>
              )}
            </div>

            {/* 音频 URL (MuseTalk 数字人口型驱动) */}
            {model.includes("talk") || model.includes("muse") ? (
              <div className="space-y-1.5">
                <div className="flex items-center justify-between text-xs">
                  <label className="font-medium z-text flex items-center gap-1">
                    <AudioWaveform className="w-3.5 h-3.5 text-purple-500" />
                    配音音频 URL (MuseTalk 口型驱动)
                  </label>
                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      onClick={() => void openAssetPicker("audio")}
                      className="inline-flex items-center gap-1 text-[11px] text-indigo-600 dark:text-indigo-400 hover:underline"
                    >
                      <FolderOpen className="w-3 h-3" />
                      从语音库选择
                    </button>
                    <span className="text-[11px] text-purple-500">对齐角色唇形</span>
                  </div>
                </div>
                <input
                  type="text"
                  value={audioUrl}
                  onChange={(e) => setAudioUrl(e.target.value)}
                  placeholder="https://.../speech_line.mp3"
                  className="w-full text-xs rounded-lg border z-border p-input p-2 focus:ring-1 focus:ring-primary z-text"
                />
              </div>
            ) : null}

            {/* 动态生成时长 */}
            <div className="space-y-2">
              <div className="flex items-center justify-between text-xs">
                <label className="font-medium z-text">生成时长（动态秒数）</label>
                <span className="font-mono font-semibold text-primary">{duration} 秒</span>
              </div>
              <div className="grid grid-cols-5 gap-1.5 text-xs">
                {[3, 5, 8, 10, 15].map((sec) => (
                  <button
                    key={sec}
                    type="button"
                    onClick={() => setDuration(sec)}
                    className={`py-1.5 rounded-lg border transition-all ${
                      duration === sec
                        ? "border-primary bg-primary/10 text-primary font-semibold"
                        : "z-border hover:bg-muted/50 z-text"
                    }`}
                  >
                    {sec}s
                  </button>
                ))}
              </div>
              <div className="flex items-center gap-3 pt-1">
                <input
                  type="range"
                  min={1}
                  max={30}
                  step={1}
                  value={Math.min(30, Math.max(1, duration))}
                  onChange={(e) => setDuration(Number(e.target.value) || 5)}
                  className="flex-1 accent-primary cursor-pointer"
                />
                <div className="flex items-center gap-1 shrink-0">
                  <input
                    type="number"
                    min={1}
                    max={60}
                    value={duration}
                    onChange={(e) => {
                      const val = parseInt(e.target.value, 10);
                      if (!Number.isNaN(val)) {
                        setDuration(Math.min(60, Math.max(1, val)));
                      }
                    }}
                    className="w-16 text-xs text-center rounded-md border z-border p-input py-1 px-1.5 z-text"
                  />
                  <span className="text-xs z-text-muted">秒</span>
                </div>
              </div>
            </div>

            <button
              onClick={handleSubmit}
              disabled={submitting || (!prompt.trim() && !imageUrl.trim())}
              className="w-full py-2.5 px-4 rounded-lg bg-primary text-primary-foreground font-medium text-sm hover:bg-primary/90 transition-all flex items-center justify-center gap-2 shadow disabled:opacity-50"
            >
              {submitting ? (
                <>
                  <RefreshCw className="w-4 h-4 animate-spin" />
                  正在提交渲染任务...
                </>
              ) : (
                <>
                  <Film className="w-4 h-4" />
                  开始生成 AI 视频
                </>
              )}
            </button>
          </div>

          {/* 活跃任务进度卡片 */}
          {activeTask && (
            <div className="p-card rounded-xl border border-primary/30 p-4 shadow-sm space-y-3 bg-primary/5">
              <div className="flex items-center justify-between text-xs">
                <span className="font-semibold text-primary flex items-center gap-1.5">
                  {activeTask.status === "SUCCEEDED" ? (
                    <CheckCircle2 className="w-4 h-4 text-emerald-500" />
                  ) : activeTask.status === "FAILED" ? (
                    <AlertCircle className="w-4 h-4 text-destructive" />
                  ) : (
                    <Clock className="w-4 h-4 animate-spin text-purple-500" />
                  )}
                  任务状态: {activeTask.status}
                </span>
                <span className="font-mono text-xs z-text-muted">{activeTask.task_id}</span>
              </div>

              {activeTask.status !== "SUCCEEDED" && activeTask.status !== "FAILED" && (
                <div className="space-y-1.5">
                  <div className="flex justify-between text-[11px] z-text-muted">
                    <span>AI 深度渲染合成中...</span>
                    <span>{activeTask.progress}%</span>
                  </div>
                  <div className="w-full bg-muted rounded-full h-2 overflow-hidden">
                    <div
                      className="bg-primary h-2 rounded-full transition-all duration-500"
                      style={{ width: `${activeTask.progress}%` }}
                    />
                  </div>
                </div>
              )}

              {activeTask.status === "FAILED" && (
                <div className="p-3 rounded-lg bg-destructive/10 border border-destructive/20 text-xs text-destructive space-y-1">
                  <div className="font-semibold flex items-center gap-1">
                    <AlertCircle className="w-3.5 h-3.5" />
                    <span>上游生成失败原因：</span>
                  </div>
                  <p className="font-mono break-all leading-relaxed">{activeTask.error_msg || "未知上游异常"}</p>
                </div>
              )}

              {activeTask.video_url && (
                <div className="space-y-2 pt-2 border-t z-border">
                  <video
                    src={activeTask.video_url}
                    controls
                    autoPlay
                    loop
                    className="w-full rounded-lg aspect-video bg-black shadow"
                  />
                  <div className="flex items-center justify-end gap-3">
                    <button
                      type="button"
                      onClick={() =>
                        dispatchDramaCanvas({
                          id: activeTask.task_id,
                          type: "video",
                          title: activeTask.prompt.slice(0, 20) || "视频工坊镜头",
                          prompt: activeTask.prompt,
                          videoUrl: activeTask.local_url || activeTask.video_url,
                          imageUrl: activeTask.image_url,
                          audioUrl: activeTask.audio_url,
                          durationSec: activeTask.duration,
                          sourceView: "video",
                        })
                      }
                      className="text-xs text-amber-600 dark:text-amber-400 hover:underline flex items-center gap-1"
                    >
                      <Film className="w-3.5 h-3.5" />
                      发送到短剧画布
                    </button>
                    <a
                      href={activeTask.local_url || activeTask.video_url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-xs text-primary hover:underline flex items-center gap-1"
                    >
                      <Download className="w-3.5 h-3.5" />
                      下载高清 MP4
                    </a>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>

        {/* 右侧：生成历史作品列表 */}
        <div className="lg:col-span-7 space-y-4">
          <div className="p-card rounded-xl border z-border p-4 shadow-sm space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <h2 className="text-sm font-semibold flex items-center gap-2 z-text">
                <span>生成作品历史 ({tasks.length})</span>
                <span className="text-xs z-text-muted font-normal">纯 Go 零 CGO 持久化存储</span>
              </h2>
              {tasks.some((t) => t.status === "FAILED") && (
                <button
                  type="button"
                  disabled={clearingFailed}
                  onClick={async () => {
                    setClearingFailed(true);
                    try {
                      await clearFailedVideoTasks();
                      if (activeTask?.status === "FAILED") {
                        setActiveTask(null);
                      }
                      await loadTasks();
                    } catch (err) {
                      setErrorMsg(err instanceof Error ? err.message : String(err));
                    } finally {
                      setClearingFailed(false);
                    }
                  }}
                  className="inline-flex items-center gap-1 px-2.5 py-1 rounded-md text-xs font-medium text-destructive bg-destructive/10 hover:bg-destructive/20 transition-colors disabled:opacity-50"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                  {clearingFailed
                    ? "清理中..."
                    : `清理失败 (${tasks.filter((t) => t.status === "FAILED").length})`}
                </button>
              )}
            </div>

            {/* 状态分类标签 */}
            <div className="flex flex-wrap items-center gap-1.5 border-b z-border pb-3">
              {[
                { key: "all" as const, label: "全部", count: tasks.length },
                {
                  key: "succeeded" as const,
                  label: "已完成",
                  count: tasks.filter((t) => t.status === "SUCCEEDED").length,
                },
                {
                  key: "pending" as const,
                  label: "排队中",
                  count: tasks.filter((t) => t.status === "PENDING" || t.status === "RUNNING").length,
                },
                {
                  key: "failed" as const,
                  label: "已失败",
                  count: tasks.filter((t) => t.status === "FAILED").length,
                },
              ].map((tab) => {
                const active = historyFilter === tab.key;
                return (
                  <button
                    key={tab.key}
                    type="button"
                    onClick={() => setHistoryFilter(tab.key)}
                    className={`px-2.5 py-1 rounded-md text-xs font-medium transition-colors ${
                      active
                        ? "bg-primary text-primary-foreground shadow-sm"
                        : "bg-muted/60 z-text-muted hover:bg-muted"
                    }`}
                  >
                    {tab.label} ({tab.count})
                  </button>
                );
              })}
            </div>

            {(() => {
              const filteredTasks = tasks.filter((t) => {
                if (historyFilter === "succeeded") return t.status === "SUCCEEDED";
                if (historyFilter === "pending") return t.status === "PENDING" || t.status === "RUNNING";
                if (historyFilter === "failed") return t.status === "FAILED";
                return true;
              });

              if (filteredTasks.length === 0) {
                return (
                  <div className="py-16 text-center text-xs z-text-muted space-y-2">
                    <Film className="w-8 h-8 mx-auto opacity-40" />
                    <p>
                      {tasks.length === 0
                        ? "暂无生成的视频记录，在左侧输入 Prompt 立即体验"
                        : "当前分类下暂无视频记录"}
                    </p>
                  </div>
                );
              }

              return (
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 max-h-[620px] overflow-y-auto pr-1">
                  {filteredTasks.map((t) => (
                    <div
                      key={t.task_id}
                      className="rounded-lg border z-border p-card overflow-hidden shadow-sm flex flex-col justify-between"
                    >
                      {t.video_url ? (
                        <video
                          src={t.video_url}
                          controls
                          className="w-full aspect-video bg-black object-cover"
                        />
                      ) : (
                        <div className="w-full aspect-video bg-muted/40 p-3 flex flex-col items-center justify-center text-center text-xs z-text-muted space-y-1">
                          {t.status === "FAILED" ? (
                            <>
                              <AlertCircle className="w-5 h-5 text-destructive mb-1" />
                              <span className="font-semibold text-destructive">生成失败</span>
                              <span
                                className="text-[11px] text-destructive/80 font-mono line-clamp-2"
                                title={t.error_msg}
                              >
                                {t.error_msg || "上游接口响应异常"}
                              </span>
                            </>
                          ) : (
                            <>
                              <Clock className="w-5 h-5 animate-spin text-purple-500 mb-1" />
                              <span>渲染排队中...</span>
                            </>
                          )}
                        </div>
                      )}

                      <div className="p-3 space-y-2">
                        <p className="text-xs z-text line-clamp-2 font-medium" title={t.prompt}>
                          {t.prompt || "（基于参考图生成）"}
                        </p>
                        <div className="flex items-center justify-between gap-2 text-[11px] z-text-muted pt-1 border-t z-border">
                          <span className="px-1.5 py-0.5 rounded bg-muted text-[10px]">
                            {t.model}
                          </span>
                          <div className="flex items-center gap-2">
                            <button
                              type="button"
                              onClick={() => handleReuseVideoTask(t)}
                              title="复用此任务的 Prompt、模型与素材参数"
                              className="inline-flex items-center gap-0.5 text-indigo-600 dark:text-indigo-400 hover:underline"
                            >
                              <Repeat2 className="w-3 h-3" />
                              复用
                            </button>
                            {t.video_url && (
                              <a
                                href={t.video_url}
                                target="_blank"
                                rel="noopener noreferrer"
                                className="text-primary hover:underline flex items-center gap-1"
                              >
                                <ExternalLink className="w-3 h-3" />
                                外链
                              </a>
                            )}
                            <button
                              type="button"
                              disabled={deletingId === t.task_id}
                              onClick={async () => {
                                setDeletingId(t.task_id);
                                try {
                                  await deleteVideoTask(t.task_id);
                                  setTasks((prev) => prev.filter((item) => item.task_id !== t.task_id));
                                  if (activeTask?.task_id === t.task_id) {
                                    setActiveTask(null);
                                  }
                                } catch (err) {
                                  setErrorMsg(err instanceof Error ? err.message : String(err));
                                } finally {
                                  setDeletingId(null);
                                }
                              }}
                              title="删除此记录"
                              className="inline-flex items-center gap-0.5 text-muted-foreground hover:text-destructive transition-colors disabled:opacity-50"
                            >
                              <Trash2 className="w-3 h-3" />
                              删除
                            </button>
                          </div>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              );
            })()}
          </div>
        </div>
      </div>

      {/* 本项目素材库选择弹窗（图片 / 语音 / 视频） */}
      {assetPickerOpen && (
        <div
          className="fixed inset-0 z-50 bg-black/60 backdrop-blur-xs flex items-center justify-center p-4"
          onClick={() => setAssetPickerOpen(false)}
        >
          <div
            className="p-card rounded-2xl border z-border shadow-2xl max-w-4xl w-full max-h-[84vh] flex flex-col overflow-hidden"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="p-4 border-b z-border flex items-center justify-between gap-3">
              <div className="flex items-center gap-2">
                <FolderOpen className="w-4 h-4 text-primary" />
                <h3 className="text-sm font-semibold z-text">选择本项目素材组件</h3>
              </div>
              <button
                type="button"
                onClick={() => setAssetPickerOpen(false)}
                className="p-1 rounded-lg hover:bg-muted z-text-muted hover:z-text"
                aria-label="关闭素材库"
              >
                <X className="w-4 h-4" />
              </button>
            </div>

            <div className="px-4 pt-3 pb-2 border-b z-border flex items-center gap-2">
              {[
                { key: "images" as const, label: `🖼️ 项目图片 (${galleryAssets.length})` },
                { key: "audio" as const, label: `🔊 项目语音 (${audioAssets.length})` },
                { key: "videos" as const, label: `🎬 项目视频 (${tasks.length})` },
              ].map((tab) => (
                <button
                  key={tab.key}
                  type="button"
                  onClick={() => setAssetPickerTab(tab.key)}
                  className={`px-3 py-1.5 rounded-lg text-xs font-medium transition-colors ${
                    assetPickerTab === tab.key
                      ? "bg-primary text-primary-foreground"
                      : "bg-muted/60 z-text-muted hover:bg-muted"
                  }`}
                >
                  {tab.label}
                </button>
              ))}
            </div>

            <div className="p-4 overflow-y-auto flex-1">
              {assetLoading ? (
                <div className="py-16 flex flex-col items-center justify-center gap-2 text-xs z-text-muted">
                  <Loader2 className="w-6 h-6 animate-spin text-primary" />
                  <span>正在加载项目素材库...</span>
                </div>
              ) : assetPickerTab === "images" ? (
                galleryAssets.length === 0 ? (
                  <div className="py-14 text-center text-xs z-text-muted">
                    项目画廊暂无保存的图片，请先在「图像工坊」生成或保存作品。
                  </div>
                ) : (
                  <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-3">
                    {galleryAssets.map((img) => (
                      <div
                        key={img.id}
                        className="rounded-xl border z-border overflow-hidden flex flex-col justify-between bg-muted/20"
                      >
                        <img
                          src={img.url}
                          alt={img.prompt}
                          className="w-full aspect-square object-cover bg-black/10"
                        />
                        <div className="p-2 space-y-1.5">
                          <p className="text-[11px] z-text line-clamp-2" title={img.prompt}>
                            {img.revised_prompt || img.prompt}
                          </p>
                          <div className="flex gap-1">
                            <button
                              type="button"
                              onClick={() => {
                                setImageUrl(img.url);
                                setAssetPickerOpen(false);
                              }}
                              className="flex-1 py-1 px-1.5 rounded bg-primary/10 text-primary hover:bg-primary/20 text-[10px] font-medium"
                            >
                              设为参考图
                            </button>
                            <button
                              type="button"
                              onClick={() => {
                                setImageUrl(img.url);
                                if (img.revised_prompt || img.prompt) {
                                  setPrompt(img.revised_prompt || img.prompt);
                                }
                                setAssetPickerOpen(false);
                              }}
                              className="py-1 px-1.5 rounded border z-border hover:bg-muted text-[10px] z-text"
                              title="同时填入图片与 Prompt"
                            >
                              +词
                            </button>
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                )
              ) : assetPickerTab === "audio" ? (
                audioAssets.length === 0 ? (
                  <div className="py-14 text-center text-xs z-text-muted">
                    暂无项目语音记录，请先在「配音工坊」生成语音。
                  </div>
                ) : (
                  <div className="space-y-2.5">
                    {audioAssets.map((rec) => (
                      <div
                        key={rec.id}
                        className="p-3 rounded-xl border z-border bg-muted/20 flex flex-col sm:flex-row sm:items-center justify-between gap-3"
                      >
                        <div className="flex-1 min-w-0 space-y-1.5">
                          <div className="flex items-center gap-2 text-[11px]">
                            <Volume2 className="w-3.5 h-3.5 text-purple-500 shrink-0" />
                            <span className="font-semibold z-text">{rec.voice_name || rec.voice_id}</span>
                            <span className="px-1.5 py-0.5 rounded bg-muted z-text-muted font-mono text-[10px]">
                              {rec.provider} · {rec.format || "mp3"}
                            </span>
                          </div>
                          <p className="text-xs z-text-muted line-clamp-2">{rec.input_text}</p>
                          {rec.audio_url && (
                            <audio src={rec.audio_url} controls className="h-8 w-full max-w-sm" />
                          )}
                        </div>
                        <button
                          type="button"
                          onClick={() => {
                            setAudioUrl(rec.audio_url);
                            if (!model.includes("muse") && !model.includes("talk")) {
                              setModel("muse-talk");
                            }
                            setAssetPickerOpen(false);
                          }}
                          className="px-3 py-1.5 rounded-lg bg-primary text-primary-foreground text-xs font-medium shrink-0 hover:bg-primary/90"
                        >
                          选为配音音频
                        </button>
                      </div>
                    ))}
                  </div>
                )
              ) : tasks.length === 0 ? (
                <div className="py-14 text-center text-xs z-text-muted">
                  暂无项目视频任务记录。
                </div>
              ) : (
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  {tasks.map((vt) => (
                    <div
                      key={vt.task_id}
                      className="p-3 rounded-xl border z-border bg-muted/20 flex flex-col justify-between gap-2"
                    >
                      {vt.video_url && (
                        <video
                          src={vt.video_url}
                          controls
                          className="w-full aspect-video rounded-lg bg-black object-cover"
                        />
                      )}
                      <div className="space-y-1">
                        <div className="flex items-center justify-between text-[11px] z-text-muted">
                          <span className="font-mono">{vt.model}</span>
                          <span>{vt.duration}s · {vt.status}</span>
                        </div>
                        <p className="text-xs z-text line-clamp-2">{vt.prompt || "（图生视频任务）"}</p>
                      </div>
                      <button
                        type="button"
                        onClick={() => handleReuseVideoTask(vt)}
                        className="w-full py-1.5 rounded-lg bg-primary/10 text-primary hover:bg-primary/20 text-xs font-medium flex items-center justify-center gap-1"
                      >
                        <Repeat2 className="w-3.5 h-3.5" />
                        复用配方参数（Prompt / 参考图 / 音频）
                      </button>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
