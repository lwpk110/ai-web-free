import React, { useState, useEffect } from "react";
import {
  submitVideo,
  getVideoTask,
  fetchVideoTasks,
  fetchVideoModels,
  VideoTask,
  VideoModelInfo,
} from "../api/client";
import {
  Film,
  Play,
  Download,
  Sparkles,
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
} from "lucide-react";

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
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

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
      setTasks(res.tasks || []);
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

  const presetPrompts = [
    {
      label: "🍁 电影级推镜 (秋叶古道)",
      model: "THUDM/CogVideoX-5b",
      prompt: "金色的落叶在秋风中缓缓飘落，阳光穿透薄雾照耀在古老小径上，4k高画质，电影级运镜缓慢向前推近",
    },
    {
      label: "🎭 角色对白微表情 (Meta Muse)",
      model: "muse-video",
      prompt: "近景特写，古代侠客微微抬首，眼神坚毅且深邃，周围微风拂动发丝与竹林沙沙作响，电影质感光影",
    },
    {
      label: "🌌 赛博穿梭 (极速运镜)",
      model: "THUDM/CogVideoX-2b",
      prompt: "雨夜中的赛博朋克未来都会，高耸入云的摩天楼与巨幅霓虹广告牌，无人机镜头在悬浮汽车之间极速穿梭",
    },
    {
      label: "🌊 惊涛骇浪 (自然史诗)",
      model: "jimeng-video-2.0",
      prompt: "狂风呼啸的狂暴大洋，巨浪拍击在黑色礁石上溅起漫天白色浪花，极具冲击力的慢动作镜头",
    },
  ];

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

      {/* 运镜灵感快捷填入 */}
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs z-text-muted font-medium flex items-center gap-1">
          <Sparkles className="w-3.5 h-3.5 text-amber-500" />
          运镜灵感模板：
        </span>
        {presetPrompts.map((p, idx) => (
          <button
            key={idx}
            onClick={() => {
              setPrompt(p.prompt);
              if (p.model) setModel(p.model);
            }}
            className="text-xs px-2.5 py-1 rounded-full p-card border z-border hover:bg-muted/70 transition-colors z-text"
          >
            {p.label}
          </button>
        ))}
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

            {/* Prompt */}
            <div className="space-y-1.5">
              <label className="text-xs font-medium z-text">视频画面描述 (Prompt)</label>
              <textarea
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                placeholder="描述你希望生成的动态场景、镜头运镜与视觉氛围..."
                rows={4}
                className="w-full text-xs rounded-lg border z-border p-input p-2.5 focus:ring-1 focus:ring-primary resize-none leading-relaxed z-text"
              />
            </div>

            {/* 参考图 URL (图生视频) */}
            <div className="space-y-1.5">
              <div className="flex items-center justify-between text-xs">
                <label className="font-medium z-text flex items-center gap-1">
                  <ImageIcon className="w-3.5 h-3.5 text-muted-foreground" />
                  起始参考图 URL (可选，图生视频)
                </label>
                <span className="text-[11px] z-text-muted">留空则为纯文生视频</span>
              </div>
              <input
                type="text"
                value={imageUrl}
                onChange={(e) => setImageUrl(e.target.value)}
                placeholder="https://.../keyframe.png"
                className="w-full text-xs rounded-lg border z-border p-input p-2 focus:ring-1 focus:ring-primary z-text"
              />
            </div>

            {/* 音频 URL (MuseTalk 数字人口型驱动) */}
            {model.includes("talk") || model.includes("muse") ? (
              <div className="space-y-1.5">
                <div className="flex items-center justify-between text-xs">
                  <label className="font-medium z-text flex items-center gap-1">
                    <AudioWaveform className="w-3.5 h-3.5 text-purple-500" />
                    配音音频 URL (MuseTalk 口型驱动)
                  </label>
                  <span className="text-[11px] text-purple-500">对齐角色唇形</span>
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

            {/* 时长 */}
            <div className="space-y-1.5">
              <label className="text-xs font-medium z-text">生成时长</label>
              <div className="grid grid-cols-2 gap-2 text-xs">
                <button
                  type="button"
                  onClick={() => setDuration(5)}
                  className={`py-1.5 rounded-lg border transition-all ${
                    duration === 5
                      ? "border-primary bg-primary/10 text-primary font-semibold"
                      : "z-border hover:bg-muted/50 z-text"
                  }`}
                >
                  5 秒片段
                </button>
                <button
                  type="button"
                  onClick={() => setDuration(10)}
                  className={`py-1.5 rounded-lg border transition-all ${
                    duration === 10
                      ? "border-primary bg-primary/10 text-primary font-semibold"
                      : "z-border hover:bg-muted/50 z-text"
                  }`}
                >
                  10 秒完整镜头
                </button>
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
                  <div className="flex justify-end">
                    <a
                      href={activeTask.video_url}
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
            <h2 className="text-sm font-semibold flex items-center justify-between z-text">
              <span>生成作品历史 ({tasks.length})</span>
              <span className="text-xs z-text-muted font-normal">纯 Go 零 CGO 持久化存储</span>
            </h2>

            {tasks.length === 0 ? (
              <div className="py-16 text-center text-xs z-text-muted space-y-2">
                <Film className="w-8 h-8 mx-auto opacity-40" />
                <p>暂无生成的视频记录，在左侧输入 Prompt 立即体验</p>
              </div>
            ) : (
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 max-h-[620px] overflow-y-auto pr-1">
                {tasks.map((t) => (
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
                            <span className="text-[11px] text-destructive/80 font-mono line-clamp-2" title={t.error_msg}>
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
                      <div className="flex items-center justify-between text-[11px] z-text-muted pt-1 border-t z-border">
                        <span className="px-1.5 py-0.5 rounded bg-muted text-[10px]">
                          {t.model}
                        </span>
                        {t.video_url && (
                          <a
                            href={t.video_url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-primary hover:underline flex items-center gap-1"
                          >
                            <ExternalLink className="w-3 h-3" />
                            外链播放
                          </a>
                        )}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
