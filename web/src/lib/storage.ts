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
  latencyMs?: number;
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
  | "chat";

export interface StoredChatMessage {
  id: string;
  role: "user" | "assistant";
  content: string;
  openuiMode: boolean;
  skill?: CopilotSkill;
  isOfflineFallback?: boolean;
  isStreaming?: boolean;
  showSource?: boolean;
  timestamp?: number;
}

const DB_NAME = "ai_web_free_studio_db";
const DB_VERSION = 1;
const STORE_IMAGES = "images";
const STORE_CHATS = "chats";

let dbPromise: Promise<IDBDatabase | null> | null = null;

function openStudioDB(): Promise<IDBDatabase | null> {
  if (typeof window === "undefined" || !("indexedDB" in window)) {
    return Promise.resolve(null);
  }
  if (dbPromise) return dbPromise;

  dbPromise = new Promise((resolve) => {
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
      };
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => resolve(null);
      req.onblocked = () => resolve(null);
    } catch {
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
