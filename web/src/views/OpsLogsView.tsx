import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  Check,
  Copy,
  Download,
  RefreshCw,
  Search,
  Terminal,
  Trash2,
} from "lucide-react";
import {
  clearOpsLogs,
  fetchOpsLogs,
  type OpsLogEntry,
  type OpsLogSummary,
} from "../api/client";
import {
  clearFrontendOpsLogs,
  getFrontendOpsLogs,
  subscribeFrontendOpsLogs,
} from "../lib/ops-logger";
import {
  formatClockTime,
  NoticeBanner,
  triggerTextDownload,
} from "../components/ui";

function levelBadgeClass(level: string): string {
  switch (level) {
    case "error":
      return "z-pill-danger";
    case "warn":
      return "z-pill-warn";
    default:
      return "z-pill-success";
  }
}

function sourceBadgeLabel(source: string): string {
  return source === "frontend" ? "前端" : "后台";
}

export const OpsLogsView: React.FC = () => {
  const [source, setSource] = useState<"all" | "backend" | "frontend">("all");
  const [level, setLevel] = useState<"all" | "error" | "warn_error" | "info">(
    "all"
  );
  const [search, setSearch] = useState("");
  const [limit, setLimit] = useState(200);
  const [liveTail, setLiveTail] = useState(true);

  const [summary, setSummary] = useState<OpsLogSummary | null>(null);
  const [backendEntries, setBackendEntries] = useState<OpsLogEntry[]>([]);
  const [localEntries, setLocalEntries] = useState<OpsLogEntry[]>(() =>
    getFrontendOpsLogs()
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetchOpsLogs({
        source,
        level,
        q: search.trim() || undefined,
        limit,
      });
      setSummary(res.summary);
      setBackendEntries(res.entries || []);
    } catch (err: any) {
      setError(err?.message || "加载后台运维日志失败（仍可查看本地前端日志）");
    } finally {
      setLoading(false);
    }
  }, [source, level, search, limit]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    const unsub = subscribeFrontendOpsLogs(() => {
      setLocalEntries(getFrontendOpsLogs());
    });
    return unsub;
  }, []);

  useEffect(() => {
    if (!liveTail) return;
    const timer = window.setInterval(() => {
      load();
    }, 3000);
    return () => window.clearInterval(timer);
  }, [liveTail, load]);

  const mergedEntries = useMemo(() => {
    // 将尚未上报或本地保留的前端日志与后台日志去重合并，按时间倒序展示
    const seen = new Set<string>();
    const list: OpsLogEntry[] = [];

    const pushIfNew = (item: OpsLogEntry) => {
      const key = `${item.source}|${item.level}|${item.module || ""}|${item.message}|${item.timestamp.slice(0, 19)}`;
      if (seen.has(key)) return;
      seen.add(key);
      list.push(item);
    };

    backendEntries.forEach(pushIfNew);

    const q = search.trim().toLowerCase();
    localEntries.forEach((item) => {
      if (source !== "all" && item.source !== source) return;
      if (level === "error" && item.level !== "error") return;
      if (
        level === "warn_error" &&
        item.level !== "error" &&
        item.level !== "warn"
      ) {
        return;
      }
      if (level === "info" && item.level !== "info") return;
      if (q) {
        const hay =
          `${item.module || ""} ${item.message} ${item.detail || ""} ${item.page || ""}`.toLowerCase();
        if (!hay.includes(q)) return;
      }
      pushIfNew(item);
    });

    list.sort((a, b) => (a.timestamp < b.timestamp ? 1 : -1));
    return list.slice(0, limit);
  }, [backendEntries, localEntries, source, level, search, limit]);

  const selectedEntry =
    mergedEntries.find((e) => e.id === selectedId) || mergedEntries[0] || null;

  const handleClear = async () => {
    clearFrontendOpsLogs();
    setLocalEntries([]);
    try {
      await clearOpsLogs();
      await load();
    } catch (err: any) {
      setError(err?.message || "清空后台日志失败");
    }
  };

  const handleExport = () => {
    const content = mergedEntries.map((e) => JSON.stringify(e)).join("\n");
    triggerTextDownload(
      `ai_web_free_ops_logs_${new Date().toISOString().slice(0, 10)}.jsonl`,
      content,
      "application/x-ndjson;charset=utf-8"
    );
  };

  const handleCopyDetail = async () => {
    if (!selectedEntry) return;
    const text = [
      `[${selectedEntry.timestamp}] [${selectedEntry.source.toUpperCase()}] [${selectedEntry.level.toUpperCase()}] [${selectedEntry.module || "-"}]`,
      selectedEntry.page ? `Page: ${selectedEntry.page}` : "",
      `Message: ${selectedEntry.message}`,
      selectedEntry.detail ? `Detail:\n${selectedEntry.detail}` : "",
    ]
      .filter(Boolean)
      .join("\n");
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      // ignore clipboard error
    }
  };

  const totalErrors =
    (summary?.error_count ?? 0) +
    localEntries.filter((e) => e.level === "error").length;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">
            前后端运维日志
          </h1>
          <p className="text-xs z-text-muted mt-1">
            实时聚合 Go 后台标准日志 / Gin HTTP 异常与前端控制台报错、网络失败、Promise 异常堆栈，便于直接定位排错。
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2 text-xs">
          <button
            type="button"
            onClick={() => setLiveTail((v) => !v)}
            className={`inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl border font-medium cursor-pointer ${
              liveTail ? "z-pill-success" : "z-border z-surface z-text-muted"
            }`}
          >
            <span
              className={liveTail ? "status-dot-green" : "status-dot-amber"}
            />
            <span>{liveTail ? "实时轮询 (3s)" : "已暂停轮询"}</span>
          </button>

          <button
            type="button"
            onClick={load}
            disabled={loading}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer"
          >
            <RefreshCw size={12} className={loading ? "animate-spin" : ""} />
            <span>刷新</span>
          </button>

          <button
            type="button"
            onClick={handleClear}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer"
          >
            <Trash2 size={12} />
            <span>清空</span>
          </button>

          <button
            type="button"
            onClick={handleExport}
            disabled={mergedEntries.length === 0}
            className="inline-flex items-center gap-1.5 px-3.5 py-1.5 rounded-xl z-primary-btn font-medium cursor-pointer"
          >
            <Download size={12} />
            <span>导出 JSONL</span>
          </button>
        </div>
      </div>

      {error && (
        <NoticeBanner
          type="error"
          text={error}
          onClose={() => setError(null)}
        />
      )}

      {/* Summary & Filters */}
      <div className="flex flex-wrap items-center justify-between gap-2.5 text-xs">
        <div className="flex flex-wrap items-center gap-2">
          <div className="p-surface p-1 rounded-xl inline-flex gap-0.5">
            {[
              {
                k: "all" as const,
                label: `全部 (${mergedEntries.length})`,
              },
              {
                k: "backend" as const,
                label: `后台 (${summary?.total_backend ?? 0})`,
              },
              {
                k: "frontend" as const,
                label: `前端 (${Math.max(summary?.total_frontend ?? 0, localEntries.length)})`,
              },
            ].map((item) => (
              <button
                key={item.k}
                type="button"
                onClick={() => setSource(item.k)}
                className={`px-2.5 py-1 rounded-lg cursor-pointer transition-colors ${
                  source === item.k
                    ? "z-primary-btn font-medium"
                    : "z-text-muted z-ghost-btn"
                }`}
              >
                {item.label}
              </button>
            ))}
          </div>

          <div className="p-surface p-1 rounded-xl inline-flex gap-0.5">
            {[
              { k: "all" as const, label: "全部级别" },
              { k: "error" as const, label: `仅错误 (${totalErrors})` },
              { k: "warn_error" as const, label: "警告+错误" },
              { k: "info" as const, label: "信息" },
            ].map((item) => (
              <button
                key={item.k}
                type="button"
                onClick={() => setLevel(item.k)}
                className={`px-2.5 py-1 rounded-lg cursor-pointer transition-colors ${
                  level === item.k
                    ? "z-surface border z-border font-medium"
                    : "z-text-muted z-ghost-btn"
                }`}
              >
                {item.label}
              </button>
            ))}
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <div className="relative">
            <Search
              size={13}
              className="absolute left-2.5 top-1/2 -translate-y-1/2 z-text-muted"
            />
            <input
              type="text"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="搜索模块 / 错误信息 / 路由..."
              className="pl-8 pr-3 py-1.5 rounded-xl border z-border z-surface text-xs w-56 focus:outline-none"
            />
          </div>

          <div className="p-surface p-1 rounded-xl inline-flex gap-0.5">
            {[100, 200, 500].map((n) => (
              <button
                key={n}
                type="button"
                onClick={() => setLimit(n)}
                className={`px-2 py-1 rounded-lg font-mono-num cursor-pointer ${
                  limit === n
                    ? "z-surface border z-border font-medium"
                    : "z-text-muted z-ghost-btn"
                }`}
              >
                {n}条
              </button>
            ))}
          </div>
        </div>
      </div>

      {/* Split Master-Detail */}
      <div className="grid grid-cols-1 lg:grid-cols-[1fr_400px] gap-4 items-start">
        <div className="p-card rounded-2xl overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-left border-collapse text-xs">
              <thead>
                <tr
                  className="border-b z-border z-text-muted"
                  style={{ background: "var(--bg-input)" }}
                >
                  <th className="py-3 px-4 font-medium">时间</th>
                  <th className="py-3 px-3 font-medium">来源 / 级别</th>
                  <th className="py-3 px-3 font-medium">模块</th>
                  <th className="py-3 px-4 font-medium">日志内容</th>
                </tr>
              </thead>
              <tbody className="divide-y z-border">
                {mergedEntries.map((entry) => {
                  const isSelected = selectedEntry?.id === entry.id;
                  return (
                    <tr
                      key={entry.id}
                      onClick={() => setSelectedId(entry.id)}
                      className="z-row-hover cursor-pointer"
                      style={
                        isSelected
                          ? { background: "var(--bg-muted)" }
                          : undefined
                      }
                    >
                      <td className="py-2.5 px-4 font-mono-num z-text-muted whitespace-nowrap">
                        {formatClockTime(entry.timestamp)}
                      </td>
                      <td className="py-2.5 px-3 whitespace-nowrap">
                        <div className="inline-flex items-center gap-1.5">
                          <span className="px-1.5 py-0.5 rounded-md border z-border z-muted font-mono-num">
                            {sourceBadgeLabel(entry.source)}
                          </span>
                          <span
                            className={`px-2 py-0.5 rounded-full border font-mono-num uppercase ${levelBadgeClass(
                              entry.level
                            )}`}
                          >
                            {entry.level}
                          </span>
                        </div>
                      </td>
                      <td className="py-2.5 px-3 font-mono-num z-text-sec whitespace-nowrap max-w-[140px] truncate">
                        {entry.module || "--"}
                      </td>
                      <td className="py-2.5 px-4 font-mono-num max-w-[420px]">
                        <div className="truncate">{entry.message}</div>
                        {entry.detail && (
                          <div className="text-[11px] z-text-muted truncate mt-0.5">
                            {entry.detail}
                          </div>
                        )}
                      </td>
                    </tr>
                  );
                })}
                {mergedEntries.length === 0 && (
                  <tr>
                    <td colSpan={4} className="py-12 text-center z-text-muted">
                      当前筛选条件下暂无前后端运维日志
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>

        {/* Sticky Detail Inspector */}
        <div className="lg:sticky lg:top-4">
          {!selectedEntry ? (
            <div className="p-card rounded-2xl p-6 text-xs z-text-muted text-center space-y-2">
              <Terminal size={20} className="mx-auto opacity-60" />
              <div>点击左侧任意日志条目，即可查看完整错误信息与堆栈详情。</div>
            </div>
          ) : (
            <div className="p-card rounded-2xl p-4 space-y-3 text-xs">
              <div className="flex items-start justify-between gap-2 pb-3 border-b z-border">
                <div className="min-w-0">
                  <div className="font-mono-num text-[11px] uppercase tracking-wider z-text-muted">
                    {sourceBadgeLabel(selectedEntry.source)} ·{" "}
                    {selectedEntry.module || "general"}
                  </div>
                  <div className="font-mono-num z-text-muted mt-0.5">
                    {selectedEntry.timestamp}
                  </div>
                </div>
                <span
                  className={`px-2 py-0.5 rounded-full border font-mono-num uppercase shrink-0 ${levelBadgeClass(
                    selectedEntry.level
                  )}`}
                >
                  {selectedEntry.level}
                </span>
              </div>

              {selectedEntry.page && (
                <div className="flex items-center justify-between gap-2 p-2 rounded-xl z-surface border z-border">
                  <span className="z-text-muted">触发页面</span>
                  <span className="font-mono-num">{selectedEntry.page}</span>
                </div>
              )}

              <div className="space-y-1">
                <div className="z-text-muted font-medium">错误 / 日志消息</div>
                <pre className="p-3 rounded-xl border z-border z-surface font-mono-num text-[11px] whitespace-pre-wrap break-all max-h-48 overflow-y-auto">
                  {selectedEntry.message}
                </pre>
              </div>

              {selectedEntry.detail && (
                <div className="space-y-1">
                  <div className="z-text-muted font-medium">
                    详细堆栈 / 响应体
                  </div>
                  <pre className="p-3 rounded-xl border z-border z-surface font-mono-num text-[11px] whitespace-pre-wrap break-all max-h-72 overflow-y-auto">
                    {selectedEntry.detail}
                  </pre>
                </div>
              )}

              <button
                type="button"
                onClick={handleCopyDetail}
                className="w-full inline-flex items-center justify-center gap-1.5 px-3 py-2 rounded-xl border z-border z-surface z-ghost-btn font-medium cursor-pointer"
              >
                {copied ? <Check size={13} /> : <Copy size={13} />}
                <span>{copied ? "已复制排错详情" : "复制排错详情"}</span>
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
