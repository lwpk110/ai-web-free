import { useEffect, useMemo, useRef } from "react";
import {
  useAui,
  type ExportedMessageRepository,
  type ExportedMessageRepositoryItem,
  type RemoteThreadListAdapter,
  type ThreadHistoryAdapter,
  type ThreadMessage,
} from "@assistant-ui/react";

type RemoteThreadMetadata = Awaited<
  ReturnType<RemoteThreadListAdapter["list"]>
>["threads"][number];
import { createAssistantStream } from "assistant-stream";
import {
  appendServerConversationMessage,
  createServerConversation,
  deleteServerConversation,
  deleteServerConversationMessage,
  fetchServerConversationDetail,
  fetchServerConversations,
  updateServerConversation,
  type ServerChatMessage,
  type ServerChatMessagePayload,
  type ServerConversation,
} from "../../api/client";
import { generateSmartTitle, type ChatMessageCustomMeta } from "./chatModelAdapter";

class KeyedMutationQueue {
  private readonly tails = new Map<string, Promise<void>>();

  run<T>(key: string, fn: () => Promise<T>): Promise<T> {
    const prev = this.tails.get(key);
    const next = prev ? prev.then(fn) : fn();
    const tail = next.then(
      () => undefined,
      () => undefined
    );
    this.tails.set(key, tail);
    tail.then(() => {
      if (this.tails.get(key) === tail) {
        this.tails.delete(key);
      }
    });
    return next;
  }
}

const threadWriteQueue = new KeyedMutationQueue();

function safeJsonParse<T>(raw?: string): T | undefined {
  if (!raw || !raw.trim()) return undefined;
  try {
    return JSON.parse(raw) as T;
  } catch {
    return undefined;
  }
}

function extractPlainTextFromThreadMessage(msg: ThreadMessage): string {
  const chunks: string[] = [];
  for (const part of msg.content) {
    if (part.type === "text" && part.text) {
      chunks.push(part.text);
    }
  }
  return chunks.join("\n").trim();
}

function toRemoteThreadMetadata(conv: ServerConversation): RemoteThreadMetadata {
  const customFromJson = safeJsonParse<Record<string, unknown>>(conv.custom_json) || {};
  return {
    remoteId: conv.id,
    externalId: conv.id,
    status: conv.is_archived ? "archived" : "regular",
    title: conv.title || "新建对话",
    lastMessageAt: conv.updated_at ? new Date(conv.updated_at) : undefined,
    custom: {
      ...customFromJson,
      model: conv.model || customFromJson.model,
      presetId: conv.preset_id || customFromJson.presetId,
      isPinned: Boolean(conv.is_pinned),
      messageCount: conv.message_count ?? 0,
      updatedAt: conv.updated_at,
    },
  };
}

function serializeHistoryItem(item: ExportedMessageRepositoryItem): ServerChatMessagePayload {
  const { message, parentId } = item;
  const plainText = extractPlainTextFromThreadMessage(message);
  const customMeta = ((message.metadata as { custom?: ChatMessageCustomMeta } | undefined)?.custom ||
    {}) as ChatMessageCustomMeta;

  return {
    id: message.id,
    parentId: parentId || undefined,
    role: message.role,
    content: plainText,
    partsJson: JSON.stringify(message.content),
    attachmentsJson:
      message.role === "user" && message.attachments?.length
        ? JSON.stringify(message.attachments)
        : undefined,
    metadataJson: message.metadata ? JSON.stringify(message.metadata) : undefined,
    statusJson:
      message.role === "assistant" && message.status
        ? JSON.stringify(message.status)
        : undefined,
    model: customMeta.model,
    openuiMode: Boolean(customMeta.openuiMode),
    skill: customMeta.skill,
    latencyMs: customMeta.latencyMs,
    timestamp: message.createdAt ? message.createdAt.getTime() : Date.now(),
  };
}

function deserializeServerMessage(rec: ServerChatMessage): ExportedMessageRepositoryItem {
  const parsedParts = safeJsonParse<ThreadMessage["content"]>(rec.parts_json);
  const content: ThreadMessage["content"] =
    Array.isArray(parsedParts) && parsedParts.length > 0
      ? parsedParts
      : [{ type: "text", text: rec.content || "" }];

  const parsedMeta = safeJsonParse<Record<string, unknown>>(rec.metadata_json) || {};
  const existingCustom = (parsedMeta.custom as Record<string, unknown> | undefined) || {};
  const mergedMetadata = {
    ...parsedMeta,
    custom: {
      ...existingCustom,
      ...(rec.model ? { model: rec.model } : {}),
      ...(rec.openui_mode !== undefined ? { openuiMode: rec.openui_mode } : {}),
      ...(rec.skill ? { skill: rec.skill } : {}),
      ...(rec.latency_ms ? { latencyMs: rec.latency_ms } : {}),
    },
  };

  const createdAt = rec.created_at ? new Date(rec.created_at) : new Date();

  if (rec.role === "user") {
    const parsedAttachments = safeJsonParse<
      Extract<ThreadMessage, { role: "user" }>["attachments"]
    >(rec.attachments_json);
    const userMsg: ThreadMessage = {
      id: rec.id,
      role: "user",
      createdAt,
      content: content as Extract<ThreadMessage, { role: "user" }>["content"],
      attachments: Array.isArray(parsedAttachments) ? parsedAttachments : [],
      metadata: mergedMetadata as Extract<ThreadMessage, { role: "user" }>["metadata"],
    };
    return {
      parentId: rec.parent_id ? rec.parent_id : null,
      message: userMsg,
    };
  }

  if (rec.role === "system") {
    const sysMsg: ThreadMessage = {
      id: rec.id,
      role: "system",
      createdAt,
      content: content as Extract<ThreadMessage, { role: "system" }>["content"],
      metadata: mergedMetadata as Extract<ThreadMessage, { role: "system" }>["metadata"],
    };
    return {
      parentId: rec.parent_id ? rec.parent_id : null,
      message: sysMsg,
    };
  }

  const parsedStatus = safeJsonParse<Extract<ThreadMessage, { role: "assistant" }>["status"]>(
    rec.status_json
  );
  const assistantMsg: ThreadMessage = {
    id: rec.id,
    role: "assistant",
    createdAt,
    content: content as Extract<ThreadMessage, { role: "assistant" }>["content"],
    status: parsedStatus || { type: "complete", reason: "stop" },
    metadata: mergedMetadata as Extract<ThreadMessage, { role: "assistant" }>["metadata"],
  };
  return {
    parentId: rec.parent_id ? rec.parent_id : null,
    message: assistantMsg,
  };
}

export function createBackendHistoryAdapter(
  getAui: () => ReturnType<typeof useAui>
): ThreadHistoryAdapter {
  const upsert = async (item: ExportedMessageRepositoryItem, moveHead: boolean) => {
    const aui = getAui();
    const { remoteId } = await aui.threadListItem().initialize();
    const payload = serializeHistoryItem(item);
    await threadWriteQueue.run(remoteId, () =>
      appendServerConversationMessage(remoteId, payload, moveHead)
    );
  };

  return {
    async load(): Promise<ExportedMessageRepository> {
      const aui = getAui();
      const remoteId = aui.threadListItem().getState().remoteId;
      if (!remoteId) {
        return { messages: [] };
      }
      try {
        const detail = await fetchServerConversationDetail(remoteId);
        const items = (detail.messages || []).map(deserializeServerMessage);
        const headId =
          detail.conversation?.head_message_id ||
          (items.length > 0 ? items[items.length - 1].message.id : null);
        return {
          headId,
          messages: items,
        };
      } catch (err) {
        console.warn("[ChatHistoryAdapter] Failed to load server conversation:", err);
        return { messages: [] };
      }
    },

    async append(item: ExportedMessageRepositoryItem): Promise<void> {
      await upsert(item, true);
    },

    async update(item: ExportedMessageRepositoryItem): Promise<void> {
      await upsert(item, false);
    },

    async delete(items: ExportedMessageRepositoryItem[]): Promise<void> {
      const aui = getAui();
      const remoteId = aui.threadListItem().getState().remoteId;
      if (!remoteId) return;
      await threadWriteQueue.run(remoteId, async () => {
        for (const item of items) {
          try {
            await deleteServerConversationMessage(remoteId, item.message.id);
          } catch {
            // ignore missing entry
          }
        }
      });
    },
  };
}

export interface BackendThreadListAdapterOptions {
  getDefaultModel: () => string;
  onThreadsChanged?: () => void;
}

export function createBackendThreadListAdapter(
  options: BackendThreadListAdapterOptions
): RemoteThreadListAdapter & Required<Pick<RemoteThreadListAdapter, "unarchive">> {
  return {
    unstable_useAdapters() {
      const aui = useAui();
      const auiRef = useRef(aui);
      useEffect(() => {
        auiRef.current = aui;
      });
      return useMemo(
        () => ({
          history: createBackendHistoryAdapter(() => auiRef.current),
        }),
        []
      );
    },

    async list() {
      const [regularRes, archivedRes] = await Promise.all([
        fetchServerConversations(1, 100, "", false).catch(() => ({ conversations: [] })),
        fetchServerConversations(1, 50, "", true).catch(() => ({ conversations: [] })),
      ]);
      const all = [...(regularRes.conversations || []), ...(archivedRes.conversations || [])];
      return {
        threads: all.map(toRemoteThreadMetadata),
      };
    },

    async initialize(threadId, initOpts) {
      const model =
        (initOpts?.custom?.model as string | undefined) || options.getDefaultModel() || "auto";
      const created = await createServerConversation({
        id: threadId,
        title: "新建对话",
        model,
        preset_id: initOpts?.custom?.presetId as string | undefined,
        custom_json: initOpts?.custom ? JSON.stringify(initOpts.custom) : undefined,
      });
      options.onThreadsChanged?.();
      return {
        remoteId: created.id,
        externalId: created.id,
      };
    },

    async rename(remoteId, newTitle) {
      await updateServerConversation(remoteId, { title: newTitle });
      options.onThreadsChanged?.();
    },

    async updateCustom(remoteId, custom) {
      await updateServerConversation(remoteId, {
        custom_json: custom ? JSON.stringify(custom) : "",
        is_pinned: typeof custom?.isPinned === "boolean" ? custom.isPinned : undefined,
        model: typeof custom?.model === "string" ? custom.model : undefined,
        preset_id: typeof custom?.presetId === "string" ? custom.presetId : undefined,
      });
      options.onThreadsChanged?.();
    },

    async archive(remoteId) {
      await updateServerConversation(remoteId, { is_archived: true });
      options.onThreadsChanged?.();
    },

    async unarchive(remoteId) {
      await updateServerConversation(remoteId, { is_archived: false });
      options.onThreadsChanged?.();
    },

    async delete(remoteId) {
      await threadWriteQueue.run(remoteId, () => deleteServerConversation(remoteId));
      options.onThreadsChanged?.();
    },

    async fetch(threadId) {
      const detail = await fetchServerConversationDetail(threadId);
      return toRemoteThreadMetadata(detail.conversation);
    },

    async generateTitle(remoteId, messages) {
      const firstUser = messages.find((m) => m.role === "user");
      const firstText = firstUser ? extractPlainTextFromThreadMessage(firstUser) : "";
      const title = generateSmartTitle(firstText || "新建对话");
      await updateServerConversation(remoteId, { title }).catch(() => {});
      options.onThreadsChanged?.();
      return createAssistantStream((controller) => {
        controller.appendText(title);
      });
    },
  };
}
