export type DramaImportAssetType =
  | "script"
  | "character"
  | "scene"
  | "image"
  | "audio"
  | "video"
  | "bgm";

export interface DramaCanvasImportPayload {
  id?: string;
  type: DramaImportAssetType;
  title: string;
  sourceView: "chat" | "image" | "tts" | "video" | "drama";
  prompt?: string;
  text?: string;
  imageUrl?: string;
  audioUrl?: string;
  videoUrl?: string;
  voiceId?: string;
  durationSec?: number;
  metadata?: Record<string, unknown>;
  createdAt: number;
}

const STORAGE_KEY = "aiwf_drama_canvas_pending_imports_v1";
export const DRAMA_IMPORT_EVENT = "aiwf:drama-canvas-import";

export function dispatchDramaCanvas(
  payload: Omit<DramaCanvasImportPayload, "createdAt">,
  options?: { navigate?: boolean },
): void {
  const fullPayload: DramaCanvasImportPayload = {
    ...payload,
    id: payload.id || `imp-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
    createdAt: Date.now(),
  };

  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    const list: DramaCanvasImportPayload[] = raw ? JSON.parse(raw) : [];
    list.push(fullPayload);
    // 最多保留最近 20 条跨工坊待导入资产，防止 localStorage 膨胀
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(list.slice(-20)));
  } catch {
    // 忽略无痕模式或存储受限错误，仍通过内存事件递送
  }

  window.dispatchEvent(new CustomEvent<DramaCanvasImportPayload>(DRAMA_IMPORT_EVENT, { detail: fullPayload }));

  if (options?.navigate !== false) {
    window.location.hash = "#/drama";
  }
}

export function consumePendingDramaImports(): DramaCanvasImportPayload[] {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    if (!raw) return [];
    window.localStorage.removeItem(STORAGE_KEY);
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}
