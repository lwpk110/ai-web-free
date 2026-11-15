import {
  batchMigrateServerGallery,
  clearServerChatMessages,
  clearServerGallery,
  createServerConversation,
  deleteServerConversation,
  deleteServerConversationMessage,
  deleteServerGalleryWork,
  exportServerConversationMarkdown,
  fetchServerChatMessages,
  fetchServerChatPresets,
  fetchServerConversationDetail,
  fetchServerConversations,
  fetchServerGallery,
  fetchServerPromptFavorites,
  saveServerChatMessages,
  saveServerConversationMessages,
  saveServerGalleryWork,
  searchServerChat,
  syncServerPromptFavorites,
  toggleServerGalleryFavorite,
  toggleServerPromptFavorite,
  updateServerGalleryMetadata,
  updateServerConversation,
  type ServerChatMessage,
  type ServerConversation,
  type ServerChatPreset,
  type ServerGalleryWork,
} from "../api/client";

export interface StoredImageRecord {
  id: string;
  /** Prefer binary Blob stored in IndexedDB so large PNGs survive page reloads cleanly. */
  blob?: Blob;
  /** External HTTP URL if upstream returned a signed URL instead of b64_json. */
  remoteUrl?: string;
  prompt: string;
  revisedPrompt?: string;
  model: string;
  size: string;
  quality?: string;
  mode?: "generations" | "edits";
  category?: string;
  source?: string;
  projectId?: string;
  projectTitle?: string;
  roleName?: string;
  shotIndex?: number;
  tags?: string[];
  latencyMs?: number;
  isFavorite?: boolean;
  storageType?: string;
  createdAt: string;
  timestamp: number;
}

export interface HydratedImageItem extends StoredImageRecord {
  /** Runtime display URL (object URL from Blob, or remoteUrl). */
  url: string;
  isObjectUrl: boolean;
}

export type CopilotSkill =
  | "auto"
  | "diagnose"
  | "fix"
  | "openui"
  | "image"
  | "audio"
  | "media_crawl"
  | "chat";

export interface StoredChatMessage {
  id: string;
  conversationId?: string;
  role: "user" | "assistant" | "system";
  content: string;
  model?: string;
  openuiMode: boolean;
  skill?: CopilotSkill;
  tokensPrompt?: number;
  tokensComp?: number;
  latencyMs?: number;
  citations?: string;
  isOfflineFallback?: boolean;
  isStreaming?: boolean;
  showSource?: boolean;
  timestamp?: number;
}

export interface StoredConversation {
  id: string;
  title: string;
  model: string;
  systemPrompt?: string;
  presetId?: string;
  isPinned: boolean;
  isArchived: boolean;
  tags?: string;
  messageCount: number;
  createdAt: string;
  updatedAt: string;
  timestamp: number;
}

const DB_NAME = "ai_web_free_studio_db";
const DB_VERSION = 3;
const STORE_IMAGES = "images";
const STORE_CHATS = "chats";
const STORE_CONVERSATIONS = "conversations";

let dbPromise: Promise<IDBDatabase | null> | null = null;

function openStudioDB(): Promise<IDBDatabase | null> {
  if (typeof window === "undefined" || !("indexedDB" in window)) {
    return Promise.resolve(null);
  }
  if (dbPromise) return dbPromise;

  dbPromise = new Promise((resolve) => {
    // 设置 1.5 秒超时兜底，防止跨标签页锁死阻塞 UI 初始化
    const timer = window.setTimeout(() => {
      dbPromise = null;
      resolve(null);
    }, 1500);
    try {
      const req = window.indexedDB.open(DB_NAME, DB_VERSION);
      req.onupgradeneeded = () => {
        const db = req.result;
        if (!db.objectStoreNames.contains(STORE_IMAGES)) {
          const imgStore = db.createObjectStore(STORE_IMAGES, {
            keyPath: "id",
          });
          imgStore.createIndex("by_timestamp", "timestamp", { unique: false });
        }
        if (!db.objectStoreNames.contains(STORE_CHATS)) {
          db.createObjectStore(STORE_CHATS, { keyPath: "key" });
        }
        if (!db.objectStoreNames.contains(STORE_CONVERSATIONS)) {
          const convStore = db.createObjectStore(STORE_CONVERSATIONS, {
            keyPath: "id",
          });
          convStore.createIndex("by_updated_at", "timestamp", { unique: false });
          convStore.createIndex("by_pinned", "isPinned", { unique: false });
        }
      };
      req.onsuccess = () => {
        window.clearTimeout(timer);
        resolve(req.result);
      };
      req.onerror = () => {
        window.clearTimeout(timer);
        dbPromise = null;
        resolve(null);
      };
      req.onblocked = () => {
        window.clearTimeout(timer);
        dbPromise = null;
        resolve(null);
      };
    } catch {
      window.clearTimeout(timer);
      dbPromise = null;
      resolve(null);
    }
  });
  return dbPromise;
}

export function dataUrlToBlob(dataUrl: string): Blob | null {
  try {
    const commaIdx = dataUrl.indexOf(",");
    if (commaIdx === -1) return null;
    const meta = dataUrl.slice(0, commaIdx);
    const b64 = dataUrl.slice(commaIdx + 1);
    const mimeMatch = /data:([^;]+);base64/i.exec(meta);
    const mime = mimeMatch ? mimeMatch[1] : "image/png";
    const binary = window.atob(b64);
    const len = binary.length;
    const bytes = new Uint8Array(len);
    for (let i = 0; i < len; i++) {
      bytes[i] = binary.charCodeAt(i);
    }
    return new Blob([bytes], { type: mime });
  } catch {
    return null;
  }
}

export function base64ToBlob(b64: string, mime = "image/png"): Blob | null {
  try {
    const clean = b64.includes(",") ? b64.split(",").pop() || "" : b64;
    const binary = window.atob(clean);
    const len = binary.length;
    const bytes = new Uint8Array(len);
    for (let i = 0; i < len; i++) {
      bytes[i] = binary.charCodeAt(i);
    }
    return new Blob([bytes], { type: mime });
  } catch {
    return null;
  }
}

export function hydrateStoredImage(rec: StoredImageRecord): HydratedImageItem | null {
  if (rec.blob instanceof Blob && rec.blob.size > 0) {
    return {
      ...rec,
      url: URL.createObjectURL(rec.blob),
      isObjectUrl: true,
    };
  }
  if (rec.remoteUrl) {
    return {
      ...rec,
      url: rec.remoteUrl,
      isObjectUrl: false,
    };
  }
  return null;
}

export async function loadStoredImages(): Promise<HydratedImageItem[]> {
  const db = await openStudioDB();
  if (!db) return [];

  return new Promise((resolve) => {
    try {
      const tx = db.transaction(STORE_IMAGES, "readonly");
      const store = tx.objectStore(STORE_IMAGES);
      const req = store.getAll();
      req.onsuccess = () => {
        const rows: StoredImageRecord[] = Array.isArray(req.result)
          ? req.result
          : [];
        rows.sort((a, b) => (b.timestamp || 0) - (a.timestamp || 0));
        const items = rows
          .map(hydrateStoredImage)
          .filter((item): item is HydratedImageItem => item !== null);
        resolve(items);
      };
      req.onerror = () => resolve([]);
    } catch {
      resolve([]);
    }
  });
}

export async function saveStoredImage(
  rec: StoredImageRecord
): Promise<boolean> {
  const db = await openStudioDB();
  if (!db) return false;

  return new Promise((resolve) => {
    try {
      const tx = db.transaction(STORE_IMAGES, "readwrite");
      tx.objectStore(STORE_IMAGES).put(rec);
      tx.oncomplete = () => resolve(true);
      tx.onerror = () => resolve(false);
    } catch {
      resolve(false);
    }
  });
}

export async function deleteStoredImage(id: string): Promise<boolean> {
  const db = await openStudioDB();
  if (!db) return false;

  return new Promise((resolve) => {
    try {
      const tx = db.transaction(STORE_IMAGES, "readwrite");
      tx.objectStore(STORE_IMAGES).delete(id);
      tx.oncomplete = () => resolve(true);
      tx.onerror = () => resolve(false);
    } catch {
      resolve(false);
    }
  });
}

export async function clearStoredImages(): Promise<boolean> {
  const db = await openStudioDB();
  if (!db) return false;

  return new Promise((resolve) => {
    try {
      const tx = db.transaction(STORE_IMAGES, "readwrite");
      tx.objectStore(STORE_IMAGES).clear();
      tx.oncomplete = () => resolve(true);
      tx.onerror = () => resolve(false);
    } catch {
      resolve(false);
    }
  });
}

export async function loadStoredChat(
  key = "default"
): Promise<StoredChatMessage[] | null> {
  const db = await openStudioDB();
  if (!db) return null;

  return new Promise((resolve) => {
    try {
      const tx = db.transaction(STORE_CHATS, "readonly");
      const req = tx.objectStore(STORE_CHATS).get(key);
      req.onsuccess = () => {
        const res = req.result as
          | { key: string; messages: StoredChatMessage[] }
          | undefined;
        if (res && Array.isArray(res.messages) && res.messages.length > 0) {
          resolve(
            res.messages.map((m) => ({
              ...m,
              isStreaming: false,
            }))
          );
        } else {
          resolve(null);
        }
      };
      req.onerror = () => resolve(null);
    } catch {
      resolve(null);
    }
  });
}

export async function saveStoredChat(
  messages: StoredChatMessage[],
  key = "default"
): Promise<boolean> {
  const db = await openStudioDB();
  if (!db) return false;

  return new Promise((resolve) => {
    try {
      const clean = messages.map((m) => ({
        ...m,
        isStreaming: false,
      }));
      const tx = db.transaction(STORE_CHATS, "readwrite");
      tx.objectStore(STORE_CHATS).put({
        key,
        messages: clean.slice(-60),
        updatedAt: Date.now(),
      });
      tx.oncomplete = () => resolve(true);
      tx.onerror = () => resolve(false);
    } catch {
      resolve(false);
    }
  });
}

export async function clearStoredChat(key = "default"): Promise<boolean> {
  const db = await openStudioDB();
  if (!db) return false;

  return new Promise((resolve) => {
    try {
      const tx = db.transaction(STORE_CHATS, "readwrite");
      tx.objectStore(STORE_CHATS).delete(key);
      tx.oncomplete = () => resolve(true);
      tx.onerror = () => resolve(false);
    } catch {
      resolve(false);
    }
  });
}

const FAVORITE_PROMPTS_KEY = "ai_web_free_fav_prompts_v1";

export function loadFavoritePromptIds(): string[] {
  try {
    const raw = window.localStorage.getItem(FAVORITE_PROMPTS_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed)
      ? parsed.filter((x): x is string => typeof x === "string")
      : [];
  } catch {
    return [];
  }
}

export function toggleFavoritePromptId(id: string): string[] {
  const current = loadFavoritePromptIds();
  const exists = current.includes(id);
  const next = exists ? current.filter((x) => x !== id) : [id, ...current];
  try {
    window.localStorage.setItem(FAVORITE_PROMPTS_KEY, JSON.stringify(next));
  } catch {
    // ignore storage quota errors
  }
  return next;
}

export function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve) => {
    try {
      const reader = new FileReader();
      reader.onloadend = () => {
        const res = reader.result;
        resolve(typeof res === "string" ? res : "");
      };
      reader.onerror = () => resolve("");
      reader.readAsDataURL(blob);
    } catch {
      resolve("");
    }
  });
}

/** 检查当前浏览器本地 IndexedDB 中存储的历史作品数量 */
export async function checkLocalLegacyImagesCount(): Promise<number> {
  const db = await openStudioDB();
  if (!db) return 0;
  return new Promise((resolve) => {
    try {
      const tx = db.transaction(STORE_IMAGES, "readonly");
      const store = tx.objectStore(STORE_IMAGES);
      const req = store.count();
      req.onsuccess = () => resolve(req.result || 0);
      req.onerror = () => resolve(0);
    } catch {
      resolve(0);
    }
  });
}

/** 读取当前本地 IndexedDB 中原始的全部作品数据 */
export async function loadRawStoredImages(): Promise<StoredImageRecord[]> {
  const db = await openStudioDB();
  if (!db) return [];
  return new Promise((resolve) => {
    try {
      const tx = db.transaction(STORE_IMAGES, "readonly");
      const store = tx.objectStore(STORE_IMAGES);
      const req = store.getAll();
      req.onsuccess = () => {
        resolve(Array.isArray(req.result) ? req.result : []);
      };
      req.onerror = () => resolve([]);
    } catch {
      resolve([]);
    }
  });
}

/** 一键将当前浏览器本地 IndexedDB 中的历史作品批量上报同步至服务端数据库 */
export async function syncLocalImagesToServer(
  onProgress?: (current: number, total: number) => void
): Promise<{ migrated: number; skipped: number; total: number }> {
  const localItems = await loadRawStoredImages();
  if (localItems.length === 0) {
    return { migrated: 0, skipped: 0, total: 0 };
  }

  const batchPayload: Array<{
    id?: string;
    prompt: string;
    revised_prompt?: string;
    model: string;
    size: string;
    quality?: string;
    mode?: "generations" | "edits";
    category?: string;
    source?: string;
    project_id?: string;
    project_title?: string;
    role_name?: string;
    shot_index?: number;
    tags?: string[];
    b64_json?: string;
    url?: string;
    latency_ms?: number;
    is_favorite?: boolean;
    timestamp?: number;
  }> = [];

  let processed = 0;
  for (const item of localItems) {
    let b64: string | undefined;
    if (item.blob instanceof Blob) {
      b64 = await blobToBase64(item.blob);
    }
    batchPayload.push({
      id: item.id,
      prompt: item.prompt,
      revised_prompt: item.revisedPrompt,
      model: item.model,
      size: item.size,
      quality: item.quality,
      mode: item.mode,
      category: item.category,
      source: item.source,
      project_id: item.projectId,
      project_title: item.projectTitle,
      role_name: item.roleName,
      shot_index: item.shotIndex,
      tags: item.tags,
      b64_json: b64,
      url: item.remoteUrl,
      latency_ms: item.latencyMs,
      is_favorite: item.isFavorite,
      timestamp: item.timestamp,
    });
    processed++;
    if (onProgress) {
      onProgress(processed, localItems.length);
    }
  }

  // 分批提交，每批 10 张，避免请求体过大
  let totalMigrated = 0;
  let totalSkipped = 0;
  const CHUNK_SIZE = 10;
  for (let i = 0; i < batchPayload.length; i += CHUNK_SIZE) {
    const chunk = batchPayload.slice(i, i + CHUNK_SIZE);
    try {
      const res = await batchMigrateServerGallery(chunk);
      totalMigrated += res.migrated;
      totalSkipped += res.skipped;
    } catch {
      // 容错处理
    }
  }

  return {
    migrated: totalMigrated,
    skipped: totalSkipped,
    total: localItems.length,
  };
}

/** 加载统一画廊作品（优先拉取服务端持久化数据库，离线或无网时 fallback 至本地 IndexedDB） */
export async function loadUnifiedImages(options?: {
  model?: string;
  favorite?: boolean;
  q?: string;
}): Promise<HydratedImageItem[]> {
  try {
    const serverRes = await fetchServerGallery({
      page: 1,
      pageSize: 150,
      model: options?.model,
      favorite: options?.favorite,
      q: options?.q,
    });
    if (Array.isArray(serverRes.items) && serverRes.items.length > 0) {
      return serverRes.items.map((item: ServerGalleryWork): HydratedImageItem => ({
        id: item.id,
        url: item.url,
        isObjectUrl: false,
        prompt: item.prompt,
        revisedPrompt: item.revised_prompt,
        model: item.model,
        size: item.size,
        quality: item.quality,
        mode: item.mode,
        category: item.category,
        source: item.source,
        projectId: item.project_id,
        projectTitle: item.project_title,
        roleName: item.role_name,
        shotIndex: item.shot_index,
        tags: item.tags || [],
        storageType: item.storage_type,
        isFavorite: item.is_favorite,
        latencyMs: item.latency_ms,
        createdAt: item.created_at ? new Date(item.created_at).toLocaleTimeString("zh-CN", { hour12: false }) : "",
        timestamp: item.created_at ? Date.parse(item.created_at) : Date.now(),
      }));
    }
  } catch {
    // 服务端异常或无网时优雅降级为本地 IndexedDB
  }

  return loadStoredImages();
}

/** 保存画廊作品（双写：先保存到服务端持久化数据库，同时保留本地缓存） */
export async function saveUnifiedImage(
  rec: StoredImageRecord,
  b64Json?: string
): Promise<HydratedImageItem> {
  // 1. 本地 IndexedDB 缓存
  await saveStoredImage(rec);

  // 2. 尝试同步服务端数据库
  try {
    let payloadB64 = b64Json;
    if (!payloadB64 && rec.blob instanceof Blob) {
      payloadB64 = await blobToBase64(rec.blob);
    }
    const serverWork = await saveServerGalleryWork({
      id: rec.id,
      prompt: rec.prompt,
      revised_prompt: rec.revisedPrompt,
      model: rec.model,
      size: rec.size,
      quality: rec.quality,
      mode: rec.mode,
      category: rec.category,
      source: rec.source,
      project_id: rec.projectId,
      project_title: rec.projectTitle,
      role_name: rec.roleName,
      shot_index: rec.shotIndex,
      tags: rec.tags,
      b64_json: payloadB64,
      url: rec.remoteUrl,
      latency_ms: rec.latencyMs,
      is_favorite: rec.isFavorite,
      timestamp: rec.timestamp,
    });

    return {
      id: serverWork.id,
      url: serverWork.url,
      isObjectUrl: false,
      prompt: serverWork.prompt,
      revisedPrompt: serverWork.revised_prompt,
      model: serverWork.model,
      size: serverWork.size,
      quality: serverWork.quality,
      mode: serverWork.mode,
      category: serverWork.category,
      source: serverWork.source,
      projectId: serverWork.project_id,
      projectTitle: serverWork.project_title,
      roleName: serverWork.role_name,
      shotIndex: serverWork.shot_index,
      tags: serverWork.tags || [],
      storageType: serverWork.storage_type,
      isFavorite: serverWork.is_favorite,
      latencyMs: serverWork.latency_ms,
      createdAt: serverWork.created_at ? new Date(serverWork.created_at).toLocaleTimeString("zh-CN", { hour12: false }) : rec.createdAt,
      timestamp: rec.timestamp,
    };
  } catch {
    return (
      hydrateStoredImage(rec) || {
        ...rec,
        url: rec.remoteUrl || "",
        isObjectUrl: false,
      }
    );
  }
}

/** 删除作品（服务端物理删除 + 本地缓存清理） */
export async function deleteUnifiedImage(id: string): Promise<boolean> {
  await deleteStoredImage(id);
  try {
    await deleteServerGalleryWork(id);
    return true;
  } catch {
    return false;
  }
}

/** Update gallery classification locally first, then sync it to the server when available. */
export async function updateUnifiedImageMetadata(
  id: string,
  metadata: { category?: string; tags?: string[] }
): Promise<{ category?: string; tags: string[] }> {
  const localItems = await loadRawStoredImages();
  const localItem = localItems.find((item) => item.id === id);
  const localPatch = {
    category: metadata.category ?? localItem?.category,
    tags: metadata.tags ?? localItem?.tags ?? [],
  };
  if (localItem) {
    await saveStoredImage({ ...localItem, ...localPatch });
  }

  try {
    const saved = await updateServerGalleryMetadata(id, metadata);
    const result = { category: saved.category, tags: saved.tags || [] };
    if (localItem) await saveStoredImage({ ...localItem, ...result });
    return result;
  } catch (error) {
    if (localItem) return localPatch;
    throw error;
  }
}

/** 清空作品画廊 */
export async function clearUnifiedImages(): Promise<boolean> {
  await clearStoredImages();
  try {
    await clearServerGallery();
    return true;
  } catch {
    return false;
  }
}

/** 切换作品收藏状态 */
export async function toggleUnifiedFavorite(id: string, currentFav?: boolean): Promise<boolean> {
  try {
    const res = await toggleServerGalleryFavorite(id, !currentFav);
    return res.is_favorite;
  } catch {
    return !currentFav;
  }
}

/** 统一加载 Copilot 对话记录（服务端优先，本地兜底） */
export async function loadUnifiedChat(key = "default"): Promise<StoredChatMessage[] | null> {
  try {
    const serverMessages = await fetchServerChatMessages(key);
    if (Array.isArray(serverMessages) && serverMessages.length > 0) {
      const mapped: StoredChatMessage[] = serverMessages.map((m: ServerChatMessage) => ({
        id: m.id,
        role: m.role as "user" | "assistant",
        content: m.content,
        openuiMode: !!m.openui_mode,
        skill: (m.skill as CopilotSkill) || "auto",
        timestamp: m.created_at ? Date.parse(m.created_at) : undefined,
      }));
      void saveStoredChat(mapped, key);
      return mapped;
    }
  } catch {
    // ignore
  }
  return loadStoredChat(key);
}

/** 统一保存 Copilot 对话记录（双写） */
export async function saveUnifiedChat(
  messages: StoredChatMessage[],
  key = "default"
): Promise<boolean> {
  await saveStoredChat(messages, key);
  try {
    await saveServerChatMessages(
      messages.map((m) => ({
        id: m.id,
        role: m.role,
        content: m.content,
        openuiMode: m.openuiMode,
        skill: m.skill,
        timestamp: m.timestamp,
      })),
      key
    );
    return true;
  } catch {
    return false;
  }
}

/** 统一清空 Copilot 对话记录 */
export async function clearUnifiedChat(key = "default"): Promise<boolean> {
  await clearStoredChat(key);
  try {
    await clearServerChatMessages(key);
    return true;
  } catch {
    return false;
  }
}

/** 统一加载 Prompt Plaza 收藏（服务端优先并合并本地） */
export async function loadUnifiedFavoritePromptIds(): Promise<string[]> {
  const localFavs = loadFavoritePromptIds();
  try {
    const serverFavs = await fetchServerPromptFavorites();
    const merged = Array.from(new Set([...serverFavs, ...localFavs]));
    try {
      window.localStorage.setItem(FAVORITE_PROMPTS_KEY, JSON.stringify(merged));
    } catch {}
    if (localFavs.length > 0 && serverFavs.length === 0) {
      void syncServerPromptFavorites(localFavs);
    }
    return merged;
  } catch {
    return localFavs;
  }
}

/** 统一切换 Prompt Plaza 收藏（服务端与本地同步） */
export async function toggleUnifiedFavoritePromptId(id: string): Promise<string[]> {
  const localNext = toggleFavoritePromptId(id);
  try {
    const serverNext = await toggleServerPromptFavorite(id);
    return Array.from(new Set([...serverNext, ...localNext]));
  } catch {
    return localNext;
  }
}

// ─── 多会话工作台 Local-First 与 Hybrid Sync ──────────────────────────────

export async function loadStoredConversations(): Promise<StoredConversation[]> {
  const db = await openStudioDB();
  if (!db || !db.objectStoreNames.contains(STORE_CONVERSATIONS)) return [];

  return new Promise((resolve) => {
    try {
      const tx = db.transaction(STORE_CONVERSATIONS, "readonly");
      const store = tx.objectStore(STORE_CONVERSATIONS);
      const req = store.getAll();
      req.onsuccess = () => {
        const rows: StoredConversation[] = Array.isArray(req.result) ? req.result : [];
        rows.sort((a, b) => {
          if (a.isPinned !== b.isPinned) return a.isPinned ? -1 : 1;
          return (b.timestamp || 0) - (a.timestamp || 0);
        });
        resolve(rows);
      };
      req.onerror = () => resolve([]);
    } catch {
      resolve([]);
    }
  });
}

export async function saveStoredConversation(conv: StoredConversation): Promise<void> {
  const db = await openStudioDB();
  if (!db || !db.objectStoreNames.contains(STORE_CONVERSATIONS)) return;

  return new Promise((resolve) => {
    try {
      const tx = db.transaction(STORE_CONVERSATIONS, "readwrite");
      const store = tx.objectStore(STORE_CONVERSATIONS);
      store.put(conv);
      tx.oncomplete = () => resolve();
      tx.onerror = () => resolve();
    } catch {
      resolve();
    }
  });
}

export async function deleteStoredConversation(id: string): Promise<void> {
  const db = await openStudioDB();
  if (!db || !db.objectStoreNames.contains(STORE_CONVERSATIONS)) return;

  return new Promise((resolve) => {
    try {
      const tx = db.transaction(STORE_CONVERSATIONS, "readwrite");
      const store = tx.objectStore(STORE_CONVERSATIONS);
      store.delete(id);
      tx.oncomplete = () => resolve();
      tx.onerror = () => resolve();
    } catch {
      resolve();
    }
  });
}

function serverConvToStored(c: ServerConversation): StoredConversation {
  return {
    id: c.id,
    title: c.title,
    model: c.model,
    systemPrompt: c.system_prompt,
    presetId: c.preset_id,
    isPinned: !!c.is_pinned,
    isArchived: !!c.is_archived,
    tags: c.tags,
    messageCount: c.message_count,
    createdAt: c.created_at,
    updatedAt: c.updated_at,
    timestamp: c.updated_at ? Date.parse(c.updated_at) : Date.now(),
  };
}

/** 统一获取会话列表（服务端为主真源，立即返回不被本地缓存阻塞） */
export async function loadUnifiedConversations(): Promise<StoredConversation[]> {
  try {
    const res = await fetchServerConversations(1, 100);
    if (res && Array.isArray(res.conversations)) {
      const serverMapped = res.conversations.map(serverConvToStored);
      serverMapped.sort((a, b) => {
        if (a.isPinned !== b.isPinned) return a.isPinned ? -1 : 1;
        return (b.timestamp || 0) - (a.timestamp || 0);
      });

      // 异步在后台与本地 IndexedDB 对齐，绝不阻塞 UI 主渲染流程
      void (async () => {
        try {
          for (const conv of serverMapped) {
            void saveStoredConversation(conv);
          }
          const localList = await loadStoredConversations();
          const serverIdSet = new Set(serverMapped.map((c) => c.id));
          const missingOnServer = localList.filter((lc) => !serverIdSet.has(lc.id));
          for (const orphan of missingOnServer) {
            await createServerConversation({
              id: orphan.id,
              title: orphan.title,
              model: orphan.model,
              system_prompt: orphan.systemPrompt,
              preset_id: orphan.presetId,
              is_pinned: orphan.isPinned,
            }).catch(() => {});
            const orphanMsgs = await loadStoredChat(orphan.id);
            if (orphanMsgs && orphanMsgs.length > 0) {
              await saveServerConversationMessages(
                orphan.id,
                orphanMsgs.map((m) => ({
                  id: m.id,
                  role: m.role,
                  content: m.content,
                  model: m.model,
                  openuiMode: m.openuiMode,
                  skill: m.skill,
                  tokensPrompt: m.tokensPrompt,
                  tokensComp: m.tokensComp,
                  latencyMs: m.latencyMs,
                  citations: m.citations,
                }))
              ).catch(() => {});
            }
          }
        } catch {}
      })();

      return serverMapped;
    }
  } catch {
    // 服务端不可用时回退到本地缓存
  }
  return loadStoredConversations();
}

/** 统一创建会话 */
export async function createUnifiedConversation(payload: {
  title?: string;
  model?: string;
  systemPrompt?: string;
  presetId?: string;
  isPinned?: boolean;
}): Promise<StoredConversation> {
  const localId = "conv-" + Math.random().toString(36).slice(2, 10);
  const now = new Date().toISOString();
  const localConv: StoredConversation = {
    id: localId,
    title: payload.title || "新建对话",
    model: payload.model || "auto",
    systemPrompt: payload.systemPrompt,
    presetId: payload.presetId,
    isPinned: !!payload.isPinned,
    isArchived: false,
    messageCount: 0,
    createdAt: now,
    updatedAt: now,
    timestamp: Date.now(),
  };

  await saveStoredConversation(localConv);

  try {
    const serverConv = await createServerConversation({
      id: localId,
      title: localConv.title,
      model: localConv.model,
      system_prompt: localConv.systemPrompt,
      preset_id: localConv.presetId,
      is_pinned: localConv.isPinned,
    });
    const synced = serverConvToStored(serverConv);
    await saveStoredConversation(synced);
    return synced;
  } catch {
    return localConv;
  }
}

/** 统一修改会话 */
export async function updateUnifiedConversation(
  id: string,
  updates: Partial<StoredConversation>
): Promise<void> {
  const localList = await loadStoredConversations();
  const target = localList.find((c) => c.id === id);
  if (target) {
    const updated: StoredConversation = {
      ...target,
      ...updates,
      updatedAt: new Date().toISOString(),
      timestamp: Date.now(),
    };
    await saveStoredConversation(updated);
  }

  try {
    await updateServerConversation(id, {
      title: updates.title,
      model: updates.model,
      system_prompt: updates.systemPrompt,
      preset_id: updates.presetId,
      is_pinned: updates.isPinned,
      is_archived: updates.isArchived,
      tags: updates.tags,
    });
  } catch {
    // ignore
  }
}

/** 统一删除会话 */
export async function deleteUnifiedConversation(id: string): Promise<void> {
  await deleteStoredConversation(id);
  await clearStoredChat(id);
  try {
    await deleteServerConversation(id);
  } catch {
    // ignore
  }
}

/** 统一加载指定会话的消息列表 */
export async function loadUnifiedConversationMessages(
  convId: string
): Promise<StoredChatMessage[]> {
  try {
    const res = await fetchServerConversationDetail(convId);
    if (res && Array.isArray(res.messages)) {
      const mapped: StoredChatMessage[] = res.messages.map((m: ServerChatMessage) => ({
        id: m.id,
        conversationId: convId,
        role: m.role as "user" | "assistant" | "system",
        content: m.content,
        model: m.model,
        openuiMode: !!m.openui_mode,
        skill: (m.skill as CopilotSkill) || "auto",
        tokensPrompt: m.tokens_prompt,
        tokensComp: m.tokens_comp,
        latencyMs: m.latency_ms,
        citations: m.citations,
        timestamp: m.created_at ? Date.parse(m.created_at) : Date.now(),
      }));
      void saveStoredChat(mapped, convId);
      return mapped;
    }
  } catch {
    // ignore
  }
  const local = await loadStoredChat(convId);
  return local || [];
}

/** 统一保存指定会话的消息列表（双写） */
export async function saveUnifiedConversationMessages(
  convId: string,
  messages: StoredChatMessage[]
): Promise<void> {
  await saveStoredChat(messages, convId);
  try {
    await saveServerConversationMessages(
      convId,
      messages.map((m) => ({
        id: m.id,
        role: m.role,
        content: m.content,
        model: m.model,
        openuiMode: m.openuiMode,
        skill: m.skill,
        tokensPrompt: m.tokensPrompt,
        tokensComp: m.tokensComp,
        latencyMs: m.latencyMs,
        citations: m.citations,
        timestamp: m.timestamp,
      }))
    );
  } catch {
    // ignore
  }
}
