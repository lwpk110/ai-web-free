export interface PendingChatRemix {
  id: string;
  imageUrl: string;
  prompt: string;
  model?: string;
  size?: string;
  createdAt?: string;
}

const STORAGE_KEY = "ai_web_free_pending_chat_remix";
export const CHAT_REMIX_EVENT = "ai-web-free:chat-remix";

export function dispatchChatRemix(item: PendingChatRemix): void {
  try {
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(item));
  } catch {
    // ignore storage quota errors
  }
  window.dispatchEvent(new CustomEvent<PendingChatRemix>(CHAT_REMIX_EVENT, { detail: item }));
  window.location.hash = "#/chat";
}

export function consumePendingChatRemix(): PendingChatRemix | null {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    sessionStorage.removeItem(STORAGE_KEY);
    const parsed = JSON.parse(raw) as PendingChatRemix;
    if (parsed && typeof parsed.imageUrl === "string" && parsed.imageUrl.trim()) {
      return parsed;
    }
  } catch {
    // ignore parse errors
  }
  return null;
}

export function clearPendingChatRemixStorage(): void {
  try {
    sessionStorage.removeItem(STORAGE_KEY);
  } catch {
    // ignore
  }
}
