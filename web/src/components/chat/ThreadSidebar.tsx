import React, { useMemo, useState } from "react";
import {
  ThreadListItemPrimitive,
  ThreadListPrimitive,
  useAui,
  useAuiState,
} from "@assistant-ui/react";
import {
  Check,
  Download,
  Edit3,
  MessageSquare,
  PanelLeftClose,
  Pin,
  PinOff,
  Plus,
  Search,
  Trash2,
  Upload,
  X,
} from "lucide-react";
import { exportServerConversationMarkdown } from "../../api/client";

interface ThreadSidebarProps {
  open: boolean;
  onCloseMobile: () => void;
  onOpenSearch: () => void;
  onImportJson: (file: File) => void;
}

const ThreadListItemRow: React.FC = () => {
  const aui = useAui();
  const item = useAuiState((s) => s.threadListItem);
  const [editing, setEditing] = useState(false);
  const [draftTitle, setDraftTitle] = useState(item.title || "新建对话");

  const isPinned = Boolean(
    (item as { custom?: Record<string, unknown> }).custom?.isPinned
  );

  const commitRename = async () => {
    const next = draftTitle.trim();
    setEditing(false);
    if (next && next !== item.title) {
      await aui.threadListItem().rename(next);
    }
  };

  const handleTogglePin = async (e: React.MouseEvent) => {
    e.stopPropagation();
    const remoteId = item.remoteId;
    if (!remoteId) return;
    const prevCustom =
      ((item as { custom?: Record<string, unknown> }).custom as Record<string, unknown>) ||
      {};
    await aui.threadListItem().updateCustom?.({
      ...prevCustom,
      isPinned: !isPinned,
    });
  };

  const handleExportMarkdown = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (!item.remoteId) return;
    try {
      const md = await exportServerConversationMarkdown(item.remoteId);
      const blob = new Blob([md], { type: "text/markdown;charset=utf-8" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${(item.title || item.remoteId).replace(/[^\w\u4e00-\u9fa5-]+/g, "_")}.md`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch {
      // ignore
    }
  };

  return (
    <ThreadListItemPrimitive.Root className="group flex items-center gap-1 rounded-lg border border-transparent px-2 py-1.5 text-xs transition-colors hover:bg-[var(--bg-surface-hover)] data-[active]:bg-[var(--bg-surface)] data-[active]:border-[var(--hairline)] data-[active]:shadow-2xs data-[active]:font-medium">
      {editing ? (
        <div className="flex flex-1 items-center gap-1">
          <input
            value={draftTitle}
            onChange={(e) => setDraftTitle(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void commitRename();
              if (e.key === "Escape") setEditing(false);
            }}
            autoFocus
            className="flex-1 min-w-0 rounded-md border z-border bg-[var(--bg-surface)] px-2 py-1 text-xs z-text outline-none"
          />
          <button
            type="button"
            onClick={() => void commitRename()}
            className="p-1 rounded text-emerald-600 hover:bg-emerald-500/10"
            title="保存"
          >
            <Check size={12} />
          </button>
          <button
            type="button"
            onClick={() => setEditing(false)}
            className="p-1 rounded z-text-muted hover:bg-[var(--bg-muted)]"
            title="取消"
          >
            <X size={12} />
          </button>
        </div>
      ) : (
        <>
          <ThreadListItemPrimitive.Trigger className="flex flex-1 min-w-0 items-center gap-2 text-left py-0.5 cursor-pointer">
            {isPinned ? (
              <Pin size={12} className="shrink-0 z-text" />
            ) : (
              <MessageSquare size={12} className="shrink-0 z-text-muted" />
            )}
            <span className="truncate flex-1 z-text">
              <ThreadListItemPrimitive.Title fallback="新建对话" />
            </span>
          </ThreadListItemPrimitive.Trigger>

          <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity shrink-0">
            <button
              type="button"
              onClick={handleTogglePin}
              className="p-1 rounded hover:bg-[var(--bg-muted)] z-text-muted hover:z-text cursor-pointer"
              title={isPinned ? "取消置顶" : "置顶会话"}
            >
              {isPinned ? <PinOff size={11} /> : <Pin size={11} />}
            </button>
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                setDraftTitle(item.title || "新建对话");
                setEditing(true);
              }}
              className="p-1 rounded hover:bg-[var(--bg-muted)] z-text-muted hover:z-text cursor-pointer"
              title="重命名"
            >
              <Edit3 size={11} />
            </button>
            <button
              type="button"
              onClick={handleExportMarkdown}
              className="p-1 rounded hover:bg-[var(--bg-muted)] z-text-muted hover:z-text cursor-pointer"
              title="导出 Markdown"
            >
              <Download size={11} />
            </button>
            <ThreadListItemPrimitive.Delete
              className="p-1 rounded hover:bg-rose-500/10 z-text-muted hover:text-rose-500 cursor-pointer"
              title="删除会话"
            >
              <Trash2 size={11} />
            </ThreadListItemPrimitive.Delete>
          </div>
        </>
      )}
    </ThreadListItemPrimitive.Root>
  );
};

function resolveTimeGroup(
  updatedAtRaw?: unknown,
  isPinned?: boolean
): "置顶" | "今天" | "昨天" | "过去 7 天" | "更早" {
  if (isPinned) return "置顶";
  const ts = typeof updatedAtRaw === "string" ? Date.parse(updatedAtRaw) : NaN;
  if (!Number.isFinite(ts)) return "今天";
  const now = new Date();
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const oneDay = 86400 * 1000;
  if (ts >= startOfToday) return "今天";
  if (ts >= startOfToday - oneDay) return "昨天";
  if (ts >= startOfToday - 7 * oneDay) return "过去 7 天";
  return "更早";
}

const GroupedThreadList: React.FC = () => {
  const threadIds = useAuiState((s) => s.threads.threadIds);
  const threadItems = useAuiState((s) => s.threads.threadItems);

  const grouped = useMemo(() => {
    const order: Array<"置顶" | "今天" | "昨天" | "过去 7 天" | "更早"> = [
      "置顶",
      "今天",
      "昨天",
      "过去 7 天",
      "更早",
    ];
    const buckets = new Map<string, Array<{ id: string; index: number }>>();
    for (const g of order) buckets.set(g, []);

    const itemById = new Map<string, (typeof threadItems)[number]>();
    for (const item of threadItems) {
      itemById.set(item.id, item);
      if (item.remoteId) itemById.set(item.remoteId, item);
    }

    threadIds.forEach((threadId, index) => {
      const item = itemById.get(threadId);
      const custom = (item as { custom?: Record<string, unknown> } | undefined)?.custom;
      const group = resolveTimeGroup(
        custom?.updatedAt,
        Boolean(custom?.isPinned)
      );
      buckets.get(group)?.push({ id: threadId, index });
    });

    return order
      .map((label) => ({ label, items: buckets.get(label) || [] }))
      .filter((g) => g.items.length > 0);
  }, [threadIds, threadItems]);

  if (grouped.length === 0) {
    return (
      <div className="px-2 py-6 text-center text-[11px] z-text-muted">
        暂无历史会话，点击上方开启新对话
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {grouped.map((group) => (
        <div key={group.label} className="space-y-0.5">
          <div className="px-2 pb-1 text-[10px] font-semibold uppercase tracking-wider z-text-muted">
            {group.label}
          </div>
          {group.items.map((entry) => (
            <ThreadListPrimitive.ItemByIndex
              key={entry.id}
              index={entry.index}
              components={{ ThreadListItem: ThreadListItemRow }}
            />
          ))}
        </div>
      ))}
    </div>
  );
};

export const ThreadSidebar: React.FC<ThreadSidebarProps> = ({
  open,
  onCloseMobile,
  onOpenSearch,
  onImportJson,
}) => {
  const importInputRef = React.useRef<HTMLInputElement | null>(null);

  if (!open) return null;

  return (
    <aside className="w-60 shrink-0 p-sidebar flex flex-col h-full">
      <div className="px-2.5 pt-2.5 pb-2 border-b z-border space-y-2">
        <div className="flex items-center justify-between gap-1.5">
          <ThreadListPrimitive.Root className="flex-1">
            <ThreadListPrimitive.New className="w-full h-8 flex items-center justify-center gap-1.5 rounded-lg z-primary-btn px-2.5 text-xs font-medium transition cursor-pointer">
              <Plus size={13} />
              <span>新对话</span>
            </ThreadListPrimitive.New>
          </ThreadListPrimitive.Root>

          <button
            type="button"
            onClick={onOpenSearch}
            className="h-8 w-8 inline-flex items-center justify-center rounded-lg border z-border z-surface z-ghost-btn z-text-sec cursor-pointer"
            title="全文检索会话 (Ctrl+Shift+F)"
          >
            <Search size={13} />
          </button>
          <button
            type="button"
            onClick={() => importInputRef.current?.click()}
            className="h-8 w-8 inline-flex items-center justify-center rounded-lg border z-border z-surface z-ghost-btn z-text-sec cursor-pointer"
            title="导入 JSON 会话包"
          >
            <Upload size={13} />
          </button>
          <button
            type="button"
            onClick={onCloseMobile}
            className="md:hidden h-8 w-8 inline-flex items-center justify-center rounded-lg z-text-muted hover:bg-[var(--bg-muted)]"
            title="收起侧栏"
          >
            <PanelLeftClose size={14} />
          </button>
          <input
            ref={importInputRef}
            type="file"
            accept=".json,application/json"
            className="hidden"
            onChange={(e) => {
              const file = e.target.files?.[0];
              if (file) onImportJson(file);
              e.target.value = "";
            }}
          />
        </div>
      </div>

      <div className="flex-1 overflow-y-auto px-2 py-2.5">
        <ThreadListPrimitive.Root>
          <GroupedThreadList />
        </ThreadListPrimitive.Root>
      </div>
    </aside>
  );
};
