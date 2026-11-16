import {
  batchMigrateServerGallery,
  clearServerGallery,
  deleteServerGalleryWork,
  fetchServerGallery,
  fetchServerPromptFavorites,
  saveServerGalleryWork,
  syncServerPromptFavorites,
  toggleServerGalleryFavorite,
  toggleServerPromptFavorite,
  updateServerGalleryMetadata,
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
