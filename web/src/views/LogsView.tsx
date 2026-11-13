import React, { useEffect, useState } from "react";
import { Download, FileText, RefreshCw } from "lucide-react";
import {
  fetchLogs,
  type AuditEntry,
  type AuditSummary,
} from "../api/client";
import {
  formatClockTime,
  NoticeBanner,
  TraceInspectorPanel,
  triggerTextDownload,
} from "../components/ui";

const ENDPOINT_OPTIONS = [
  { v: "", label: "全部端点" },
  { v: "/v1/chat/completions", label: "/chat" },
  { v: "/v1/responses", label: "/responses" },
  { v: "/v1/images/generations", label: "/images/gen" },
  { v: "/v1/images/edits", label: "/images/edits" },
  { v: "/v1/audio/speech", label: "/audio/speech" },
  { v: "/v1/audio/transcriptions", label: "/audio/stt" },
];

export const LogsView: React.FC = () => {
  const [filter, setFilter] = useState("all");
  const [pathFilter, setPathFilter] = useState("");
  const [limit, setLimit] = useState(100);
  const [summary, setSummary] = useState<AuditSummary | null>(null);
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const load = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await fetchLogs({
        filter,
        path: pathFilter,
        limit,
      });
      const list = res.entries || [];
      setSummary(res.summary);
      setEntries(list);
      setSelectedId((prev) => {
        if (prev && list.some((e) => e.id === prev)) return prev;
        return list.length > 0 ? list[0].id : null;
      });
    } catch (err: any) {
      setError(err?.message || "加载审计日志失败");
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, [filter, pathFilter, limit]);

  const selectedEntry =
    entries.find((e) => e.id === selectedId) || entries[0] || null;

  const handleDownloadJsonl = () => {
    const content = entries.map((e) => JSON.stringify(e)).join("\n");
    triggerTextDownload(
      `aurora_audit_${new Date().toISOString().slice(0, 10)}.jsonl`,
      content,
      "application/x-ndjson;charset=utf-8"
    );
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">
            请求追踪与 Failover 审计
          </h1>
          <p className="text-xs z-text-muted mt-1">
            基于零 CGO 内存环形缓冲（容量 {summary?.capacity ?? 1000} 条）与可选 JSONL 轮转落盘，点击任意 Trace 在右侧检视完整切号链路。
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2 text-xs">
          <span className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl border z-border z-surface font-mono-num">
            <FileText size={12} />
            <span>
              {summary?.file_logging
                ? `JSONL: ${summary.log_file || "已开启"}`
                : `仅内存 (${summary?.buffered_count ?? entries.length}/${summary?.capacity ?? 1000})`}
            </span>
          </span>
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
            onClick={handleDownloadJsonl}
            disabled={entries.length === 0}
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

      {/* Filter & Endpoint Strip */}
      <div className="flex flex-wrap items-center justify-between gap-2.5 text-xs">
        <div className="p-surface p-1 rounded-xl inline-flex flex-wrap gap-0.5">
          {[
            {
              k: "all",
              label: `全部 (${summary?.buffered_count ?? entries.length})`,
            },
            {
              k: "failover",
              label: `含 Failover (${summary?.failover_recovered ?? 0})`,
            },
            {
              k: "error",
              label: `异常状态 (${summary?.failed_requests ?? 0})`,
            },
            {
              k: "event",
              label: `运维事件 (${summary?.total_events ?? 0})`,
            },
          ].map((item) => (
            <button
              key={item.k}
              type="button"
              onClick={() => setFilter(item.k)}
              className={`px-2.5 py-1 rounded-lg cursor-pointer transition-colors ${
                filter === item.k
                  ? "z-primary-btn font-medium"
                  : "z-text-muted z-ghost-btn"
              }`}
            >
              {item.label}
            </button>
          ))}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <div className="p-surface p-1 rounded-xl inline-flex flex-wrap gap-0.5">
            {ENDPOINT_OPTIONS.map((ep) => (
              <button
                key={ep.v}
                type="button"
                onClick={() => setPathFilter(ep.v)}
                className={`px-2 py-1 rounded-lg font-mono-num cursor-pointer transition-colors ${
                  pathFilter === ep.v
                    ? "z-surface border z-border font-medium"
                    : "z-text-muted z-ghost-btn"
                }`}
              >
                {ep.label}
              </button>
            ))}
          </div>

          <div className="p-surface p-1 rounded-xl inline-flex gap-0.5">
            {[50, 100, 300].map((n) => (
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

      {/* Helicone / Langfuse Split Trace Inspector */}
      <div className="grid grid-cols-1 lg:grid-cols-[1fr_370px] gap-4 items-start">
        <div className="p-card rounded-2xl overflow-hidden">
          <div className="overflow-x-auto">
            <table className="w-full text-left border-collapse text-xs">
              <thead>
                <tr
                  className="border-b z-border z-text-muted"
                  style={{ background: "var(--bg-input)" }}
                >
                  <th className="py-3 px-4 font-medium">时间</th>
                  <th className="py-3 px-3 font-medium">端点与模型</th>
                  <th className="py-3 px-3 font-medium">命中账号 / 代理</th>
                  <th className="py-3 px-3 font-medium">状态 / 切号</th>
                  <th className="py-3 px-4 font-medium text-right">耗时</th>
                </tr>
              </thead>
              <tbody className="divide-y z-border">
                {entries.map((entry) => {
                  const isSelected = selectedEntry?.id === entry.id;
                  const isEvent = entry.kind === "event";
                  const okStatus = !entry.status || entry.status < 400;
                  const dotCls = isEvent
                    ? "status-dot-green"
                    : entry.failover_trace
                      ? "status-dot-amber"
                      : okStatus
                        ? "status-dot-green"
                        : "status-dot-red";

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
                      <td className="py-3 px-4 font-mono-num z-text-muted whitespace-nowrap">
                        {formatClockTime(entry.timestamp)}
                      </td>
                      <td className="py-3 px-3">
                        <div className="flex items-center gap-2">
                          <span className={dotCls} />
                          <span className="font-mono-num font-medium truncate max-w-[190px]">
                            {entry.path || "--"}
                          </span>
                        </div>
                        <div className="z-text-muted font-mono-num mt-0.5 pl-4 truncate max-w-[200px]">
                          {entry.model || entry.kind}
                        </div>
                      </td>
                      <td className="py-3 px-3 font-mono-num">
                        <div className="truncate max-w-[190px]">
                          {entry.account_email || entry.account_id || "--"}
                        </div>
                        {entry.proxy && (
                          <div className="z-text-muted truncate max-w-[190px] mt-0.5">
                            {entry.proxy}
                          </div>
                        )}
                      </td>
                      <td className="py-3 px-3 font-mono-num whitespace-nowrap">
                        <div className="inline-flex items-center gap-1.5">
                          {isEvent ? (
                            <span className="px-2 py-0.5 rounded-full border z-border z-muted">
                              Event
                            </span>
                          ) : (
                            <span
                              className={`px-2 py-0.5 rounded-full border ${
                                okStatus ? "z-pill-success" : "z-pill-danger"
                              }`}
                            >
                              {entry.status || "--"}
                            </span>
                          )}
                          {entry.failover_trace && (
                            <span className="px-1.5 py-0.5 rounded-md border z-pill-warn">
                              救回 ×{entry.attempt_count || 2}
                            </span>
                          )}
                        </div>
                      </td>
                      <td className="py-3 px-4 font-mono-num text-right whitespace-nowrap z-text-sec">
                        {entry.duration_ms !== undefined
                          ? `${entry.duration_ms}ms`
                          : "--"}
                      </td>
                    </tr>
                  );
                })}
                {entries.length === 0 && (
                  <tr>
                    <td colSpan={5} className="py-10 text-center z-text-muted">
                      当前筛选条件下暂无日志记录
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>

        {/* Sticky Right Trace Inspector */}
        <div className="lg:sticky lg:top-4">
          <TraceInspectorPanel entry={selectedEntry} />
        </div>
      </div>
    </div>
  );
};
