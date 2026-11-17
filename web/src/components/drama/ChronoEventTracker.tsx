import React, { useMemo, useState } from "react";
import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  Clock,
  ExternalLink,
  Filter,
  Flame,
  HelpCircle,
  Play,
  RefreshCw,
  Search,
  ShieldAlert,
  Terminal,
  XCircle,
  Zap,
} from "lucide-react";

export interface ChronoLogEntry {
  timestamp: string;
  level: "info" | "success" | "warn" | "error";
  nodeKey?: string;
  message: string;
}

export interface ChronoNodeState {
  status: "pending" | "running" | "ready" | "failed" | "degraded" | "stale";
  statusNote?: string;
  error?: string;
  attempts?: number;
  updatedAt?: string;
}

export interface ChronoEventTrackerProps {
  logs: ChronoLogEntry[];
  nodeStates: Record<string, ChronoNodeState>;
  onFocusNode: (nodeKey: string) => void;
  onRetryNode?: (nodeKey: string) => void;
  onClose?: () => void;
  selectedLogRunId?: string;
  runsHistory?: Array<{ id: string; status: string; createdAt: string }>;
  onSelectRun?: (runId: string) => void;
}

interface ParsedEvent {
  id: string;
  timeStr: string;
  level: "info" | "success" | "warn" | "error";
  nodeKey: string;
  nodeName: string;
  stageName: string;
  message: string;
  isBottleneck: boolean;
  bottleneckReason?: string;
  blockedCount?: number;
}

// 格式化节点语义名称与所属阶段
function parseNodeMeta(nodeKey: string): { nodeName: string; stageName: string } {
  if (!nodeKey) return { nodeName: "全局调度引擎", stageName: "System" };

  if (nodeKey.startsWith("script.")) {
    return {
      nodeName: nodeKey === "script.analyze" ? "剧本深度拆解" : "剧本高光节拍",
      stageName: "阶段 1 · 剧本拆解",
    };
  }
  if (nodeKey.startsWith("bible.characters")) {
    return { nodeName: "角色外貌与音色圣经", stageName: "阶段 2 · 核心资产" };
  }
  if (nodeKey.startsWith("bible.scenes")) {
    return { nodeName: "场景环境与影调设定", stageName: "阶段 2 · 核心资产" };
  }
  if (nodeKey.startsWith("image.keyframe:")) {
    const shot = nodeKey.replace("image.keyframe:", "");
    return { nodeName: `分镜 #${shot} 首帧定格生图`, stageName: "阶段 3 · 视觉分镜" };
  }
  if (nodeKey.startsWith("tts.speech:")) {
    const shot = nodeKey.replace("tts.speech:", "");
    return { nodeName: `分镜 #${shot} 台词多角色配音`, stageName: "阶段 3 · 语音配音" };
  }
  if (nodeKey.startsWith("video.clip:")) {
    const shot = nodeKey.replace("video.clip:", "");
    return { nodeName: `分镜 #${shot} 动态视频渲染`, stageName: "阶段 3 · 视频生成" };
  }
  if (nodeKey.startsWith("timeline.")) {
    return { nodeName: "全片时间线与剪映总装", stageName: "阶段 4 · 成片合成" };
  }
  if (nodeKey.startsWith("render.")) {
    return { nodeName: "FFmpeg 4K 终片导出", stageName: "阶段 4 · 成片合成" };
  }
  return { nodeName: nodeKey, stageName: "任务节点" };
}

export interface ChronoBottleneck {
  nodeKey: string;
  nodeName: string;
  error: string;
  blockedDownstream: string[];
  suggestedFix: string;
  kind: "failed" | "stale_running" | "history_error";
}

const STALE_RUNNING_MS = 3 * 60 * 1000;

export function analyzeChronoBottlenecks(
  logs: ChronoLogEntry[],
  nodeStates: Record<string, ChronoNodeState>,
): ChronoBottleneck[] {
  const byKey = new Map<string, ChronoBottleneck>();
  const blockedFor = (key: string) => {
    const blocked: string[] = [];
    if (key.startsWith("image.keyframe:")) {
      const shot = key.replace("image.keyframe:", "");
      blocked.push(`video.clip:${shot}`, "timeline.assemble");
    } else if (key.startsWith("bible.")) {
      blocked.push("全部下游分镜生图与视频");
    } else if (key.startsWith("video.clip:")) {
      blocked.push("timeline.assemble", "render.mp4");
    }
    return blocked;
  };
  const fixFor = (err: string) => {
    if (err.includes("no active siliconflow api key") || err.includes("key")) {
      return "通道未配置有效 API Key，请在「能力设置」切换为可用通道";
    }
    if (err.includes("timeout") || err.includes("504") || err.includes("500")) {
      return "模型上游临时超时，建议稍后重试或减小生图并发数";
    }
    return "点击单节点重试或调整 Prompt 提示词";
  };

  Object.entries(nodeStates).forEach(([key, st]) => {
    if (st.status === "failed") {
      const err = st.error || st.statusNote || "未知执行错误";
      byKey.set(key, {
        nodeKey: key,
        nodeName: parseNodeMeta(key).nodeName,
        error: err,
        blockedDownstream: blockedFor(key),
        suggestedFix: fixFor(err),
        kind: "failed",
      });
      return;
    }
    if (st.status !== "running" || !st.updatedAt) return;
    const updated = new Date(st.updatedAt).getTime();
    if (Number.isFinite(updated) && Date.now() - updated >= STALE_RUNNING_MS) {
      const idleMin = Math.floor((Date.now() - updated) / 60000);
      const err = `节点运行中已 ${idleMin} 分钟无状态心跳`;
      byKey.set(key, {
        nodeKey: key,
        nodeName: parseNodeMeta(key).nodeName,
        error: err,
        blockedDownstream: blockedFor(key),
        suggestedFix: "后台任务可能仍在等待上游返回；可先刷新工程，超过 5 分钟仍无变化再重跑此节点",
        kind: "stale_running",
      });
    }
  });

  logs.forEach((log) => {
    const key = log.nodeKey || "";
    if (!key || byKey.has(key)) return;
    const isError = log.level === "error" || log.message.startsWith("失败 ");
    if (!isError) return;
    byKey.set(key, {
      nodeKey: key,
      nodeName: parseNodeMeta(key).nodeName,
      error: `历史执行错误：${log.message}`,
      blockedDownstream: blockedFor(key),
      suggestedFix: "该错误来自当前选中的执行记录；若资产已补跑成功，可切换最新 Run 查看实时状态",
      kind: "history_error",
    });
  });

  return Array.from(byKey.values());
}

export const ChronoEventTracker: React.FC<ChronoEventTrackerProps> = ({
  logs,
  nodeStates,
  onFocusNode,
  onRetryNode,
  onClose,
  selectedLogRunId,
  runsHistory = [],
  onSelectRun,
}) => {
  const [filterMode, setFilterMode] = useState<"all" | "bottlenecks" | "errors" | "nodes">("all");
  const [searchTerm, setSearchTerm] = useState("");

  const bottlenecks = useMemo(
    () => analyzeChronoBottlenecks(logs, nodeStates),
    [logs, nodeStates],
  );

  // 2. 结构化时光事件列表
  const parsedEvents = useMemo<ParsedEvent[]>(() => {
    return logs.map((log, index) => {
      const { nodeName, stageName } = parseNodeMeta(log.nodeKey || "");
      const isError = log.level === "error" || log.message.includes("失败") || log.message.includes("error");
      const isBottleneck = isError && !!log.nodeKey;

      let timeFormatted = "";
      try {
        const d = new Date(log.timestamp);
        timeFormatted = d.toTimeString().split(" ")[0] || log.timestamp;
      } catch {
        timeFormatted = log.timestamp;
      }

      return {
        id: `ev-${index}-${log.timestamp}`,
        timeStr: timeFormatted,
        level: log.level,
        nodeKey: log.nodeKey || "",
        nodeName,
        stageName,
        message: log.message,
        isBottleneck,
        bottleneckReason: isBottleneck ? log.message : undefined,
      };
    });
  }, [logs]);

  // 3. 过滤过滤后的事件
  const filteredEvents = useMemo(() => {
    return parsedEvents.filter((ev) => {
      if (searchTerm) {
        const match =
          ev.nodeName.toLowerCase().includes(searchTerm.toLowerCase()) ||
          ev.message.toLowerCase().includes(searchTerm.toLowerCase()) ||
          ev.nodeKey.toLowerCase().includes(searchTerm.toLowerCase());
        if (!match) return false;
      }
      if (filterMode === "bottlenecks") {
        return ev.isBottleneck;
      }
      if (filterMode === "errors") {
        return ev.level === "error" || ev.level === "warn";
      }
      if (filterMode === "nodes") {
        return !!ev.nodeKey;
      }
      return true;
    });
  }, [parsedEvents, filterMode, searchTerm]);

  return (
    <div className="flex h-full flex-col bg-[var(--bg-surface)] text-[var(--text-default)] select-none">
      {/* 顶部状态与工具栏 */}
      <div className="flex shrink-0 items-center justify-between border-b border-[var(--border-default)] bg-[var(--bg-elevated)] px-4 py-2">
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-1.5 font-semibold text-xs">
            <Clock className="h-4 w-4 text-indigo-500" />
            <span>事件时光轴 & 阻塞卡点透视</span>
          </div>

          {/* 卡点胶囊预警 */}
          {bottlenecks.length > 0 ? (
            <span className="inline-flex items-center gap-1 rounded-full bg-rose-500/10 px-2.5 py-0.5 text-[11px] font-semibold text-rose-600 border border-rose-500/30 animate-pulse">
              <ShieldAlert className="h-3.5 w-3.5" />
              检测到 {bottlenecks.length} 处阻塞卡点！
            </span>
          ) : (
            <span className="inline-flex items-center gap-1 rounded-full bg-emerald-500/10 px-2.5 py-0.5 text-[11px] font-medium text-emerald-600 border border-emerald-500/30">
              <CheckCircle2 className="h-3.5 w-3.5" />
              时序正常 · 无卡点阻塞
            </span>
          )}

          {/* Run 批次切换 */}
          {runsHistory.length > 0 && onSelectRun && (
            <select
              value={selectedLogRunId || runsHistory[0]?.id || ""}
              onChange={(e) => onSelectRun(e.target.value)}
              className="rounded border border-[var(--border-default)] bg-[var(--bg-surface)] px-2 py-0.5 text-[11px] outline-none"
            >
              {runsHistory.map((r, i) => (
                <option key={r.id} value={r.id}>
                  执行批次 #{runsHistory.length - i} ({r.status}) -{" "}
                  {r.createdAt ? new Date(r.createdAt).toLocaleTimeString() : ""}
                </option>
              ))}
            </select>
          )}
        </div>

        {/* 过滤控制器 */}
        <div className="flex items-center gap-2 text-xs">
          {/* 搜索框 */}
          <div className="relative flex items-center">
            <Search className="absolute left-2 h-3.5 w-3.5 text-[var(--text-muted)]" />
            <input
              type="text"
              placeholder="搜索节点或原因..."
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              className="h-7 w-36 rounded-md border border-[var(--border-default)] bg-[var(--bg-surface)] pl-7 pr-2 text-[11px] outline-none focus:border-indigo-500 transition-all focus:w-48"
            />
          </div>

          {/* 筛选按钮组 */}
          <div className="flex items-center rounded-lg bg-[var(--bg-surface)] p-0.5 border border-[var(--border-default)] text-[11px]">
            <button
              onClick={() => setFilterMode("all")}
              className={`px-2 py-1 rounded-md transition-colors ${
                filterMode === "all" ? "bg-[var(--bg-elevated)] font-medium shadow-xs" : "text-[var(--text-muted)]"
              }`}
            >
              全部 ({parsedEvents.length})
            </button>
            <button
              onClick={() => setFilterMode("bottlenecks")}
              className={`px-2 py-1 rounded-md transition-colors flex items-center gap-1 ${
                filterMode === "bottlenecks"
                  ? "bg-rose-500/20 text-rose-600 font-medium"
                  : "text-[var(--text-muted)] hover:text-rose-600"
              }`}
            >
              <Flame className="h-3 w-3" />
              卡点 ({bottlenecks.length})
            </button>
            <button
              onClick={() => setFilterMode("errors")}
              className={`px-2 py-1 rounded-md transition-colors ${
                filterMode === "errors" ? "bg-amber-500/20 text-amber-600 font-medium" : "text-[var(--text-muted)]"
              }`}
            >
              异常/重试
            </button>
            <button
              onClick={() => setFilterMode("nodes")}
              className={`px-2 py-1 rounded-md transition-colors ${
                filterMode === "nodes" ? "bg-[var(--bg-elevated)] font-medium" : "text-[var(--text-muted)]"
              }`}
            >
              仅节点
            </button>
          </div>

          {onClose && (
            <button
              onClick={onClose}
              className="rounded p-1 hover:bg-[var(--bg-surface-hover)] text-[var(--text-muted)] hover:text-[var(--text-default)]"
              title="收起时光轴"
            >
              ✕
            </button>
          )}
        </div>
      </div>

      {/* 核心内容区：左侧卡点深度诊断看板 + 右侧事件时光流 */}
      <div className="flex flex-1 overflow-hidden">
        {/* 卡点诊断专区 (若有卡点则常驻左侧 320px) */}
        {bottlenecks.length > 0 && (
          <div className="w-80 shrink-0 border-r border-[var(--border-default)] bg-rose-950/5 p-3 overflow-y-auto space-y-3">
            <div className="flex items-center justify-between text-xs font-bold text-rose-700 dark:text-rose-400">
              <span className="flex items-center gap-1.5">
                <AlertTriangle className="h-4 w-4" />
                当前卡点根因与解决指引
              </span>
              <span className="text-[10px] font-mono px-1.5 py-0.2 rounded bg-rose-500/20">
                {bottlenecks.length} 个阻塞
              </span>
            </div>

            <div className="space-y-2.5">
              {bottlenecks.map((b) => (
                <div
                  key={b.nodeKey}
                  className="rounded-xl border border-rose-500/30 bg-[var(--bg-surface)] p-2.5 shadow-sm space-y-2 text-xs"
                >
                  <div className="flex items-start justify-between gap-1">
                    <div>
                      <div className="font-semibold text-rose-600 dark:text-rose-400 flex items-center gap-1">
                        <span>●</span> {b.nodeName}
                      </div>
                      <div className="text-[10px] font-mono text-[var(--text-muted)]">{b.nodeKey}</div>
                    </div>
                    <button
                      onClick={() => onFocusNode(b.nodeKey)}
                      className="rounded bg-rose-500/10 hover:bg-rose-500/20 px-2 py-0.5 text-[10px] font-medium text-rose-600 flex items-center gap-1 transition"
                      title="在画布中定位此节点"
                    >
                      <ArrowRight className="h-3 w-3" /> 定位
                    </button>
                  </div>

                  {/* 报错细节 */}
                  <div className="rounded bg-rose-500/5 p-2 font-mono text-[10px] text-rose-700 dark:text-rose-300 border border-rose-500/20 leading-relaxed">
                    {b.error}
                  </div>

                  {/* 解决建议 */}
                  <div className="text-[11px] text-[var(--text-muted)] bg-[var(--bg-elevated)] p-2 rounded">
                    <span className="font-medium text-amber-600 dark:text-amber-400">💡 建议操作：</span>
                    {b.suggestedFix}
                  </div>

                  {/* 操作条 */}
                  <div className="flex items-center justify-between pt-1 border-t border-[var(--border-default)]">
                    <span className="text-[10px] text-[var(--text-muted)]">
                      级联影响: {b.blockedDownstream.join(", ")}
                    </span>
                    {onRetryNode && (
                      <button
                        onClick={() => onRetryNode(b.nodeKey)}
                        className="rounded bg-indigo-600 hover:bg-indigo-500 text-white px-2 py-0.5 text-[10px] font-medium flex items-center gap-1 shadow-xs"
                      >
                        <RefreshCw className="h-2.5 w-2.5" /> 立即重跑此卡点
                      </button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* 右侧：事件时光流水线 (Timeline Stream) */}
        <div className="flex-1 overflow-y-auto p-4 space-y-3">
          {filteredEvents.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center text-[var(--text-muted)] text-xs space-y-2">
              <Terminal className="h-8 w-8 opacity-40" />
              <div>暂无符合筛选条件的事件记录</div>
            </div>
          ) : (
            <div className="relative pl-6 space-y-3 before:absolute before:left-2.5 before:top-2 before:bottom-2 before:w-0.5 before:bg-[var(--border-default)]">
              {filteredEvents.map((ev) => {
                const isError = ev.level === "error";
                const isSuccess = ev.level === "success";
                const isWarn = ev.level === "warn";

                let dotBg = "bg-slate-400";
                if (isError) dotBg = "bg-rose-500 shadow-md shadow-rose-500/50 animate-pulse";
                else if (isSuccess) dotBg = "bg-emerald-500";
                else if (isWarn) dotBg = "bg-amber-500";

                return (
                  <div key={ev.id} className="relative group text-xs">
                    {/* 时光轴圆点 */}
                    <div
                      className={`absolute -left-6 top-1.5 h-3 w-3 rounded-full border-2 border-[var(--bg-surface)] ${dotBg}`}
                    />

                    {/* 事件卡片 */}
                    <div
                      className={`rounded-lg border p-2.5 transition-all ${
                        isError
                          ? "border-rose-500/40 bg-rose-500/5"
                          : isSuccess
                          ? "border-emerald-500/30 bg-emerald-500/5"
                          : isWarn
                          ? "border-amber-500/30 bg-amber-500/5"
                          : "border-[var(--border-default)] bg-[var(--bg-elevated)] hover:border-indigo-500/50"
                      }`}
                    >
                      <div className="flex items-center justify-between gap-2 mb-1">
                        <div className="flex items-center gap-2 flex-wrap">
                          <span className="font-mono text-[10px] text-[var(--text-muted)]">{ev.timeStr}</span>
                          <span className="rounded bg-[var(--bg-surface)] px-1.5 py-0.5 text-[10px] font-medium border border-[var(--border-default)]">
                            {ev.stageName}
                          </span>
                          <span className="font-semibold text-slate-800 dark:text-slate-200">{ev.nodeName}</span>
                          {ev.nodeKey && (
                            <span className="font-mono text-[10px] text-[var(--text-muted)]">({ev.nodeKey})</span>
                          )}
                        </div>

                        {ev.nodeKey && (
                          <div className="opacity-0 group-hover:opacity-100 transition-opacity flex items-center gap-1.5">
                            <button
                              onClick={() => onFocusNode(ev.nodeKey)}
                              className="rounded bg-indigo-500/10 hover:bg-indigo-500/20 text-indigo-600 dark:text-indigo-400 px-2 py-0.5 text-[10px] font-medium flex items-center gap-1"
                            >
                              画布定位 <ArrowRight className="h-3 w-3" />
                            </button>
                            {isError && onRetryNode && (
                              <button
                                onClick={() => onRetryNode(ev.nodeKey)}
                                className="rounded bg-rose-600 hover:bg-rose-500 text-white px-2 py-0.5 text-[10px] font-medium flex items-center gap-1"
                              >
                                重跑 <RefreshCw className="h-2.5 w-2.5" />
                              </button>
                            )}
                          </div>
                        )}
                      </div>

                      {/* 消息体 */}
                      <div className="font-mono text-[11px] leading-relaxed text-[var(--text-default)]">
                        {ev.message}
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
export default ChronoEventTracker;
