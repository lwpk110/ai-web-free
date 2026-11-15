import { FileAudio, Image as ImageIcon, Loader2, Sparkles, X } from "lucide-react";
import React, { useEffect, useState } from "react";
import {
  fetchAudioHistory,
  fetchPromptPlaza,
  fetchServerGallery,
  type AudioHistoryItem,
  type PromptPlazaItem,
  type ServerGalleryWork,
} from "../../api/client";
import { useDramaCanvasStore } from "../../stores/dramaCanvasStore";

interface DramaAssetDrawerModalProps {
  openTab: "gallery" | "audio" | "prompts" | null;
  onClose: () => void;
}

export const DramaAssetDrawerModal: React.FC<DramaAssetDrawerModalProps> = ({ openTab, onClose }) => {
  const { addNode, updateScriptNode } = useDramaCanvasStore();
  const [loading, setLoading] = useState(false);
  const [gallery, setGallery] = useState<ServerGalleryWork[]>([]);
  const [audios, setAudios] = useState<AudioHistoryItem[]>([]);
  const [prompts, setPrompts] = useState<PromptPlazaItem[]>([]);

  useEffect(() => {
    if (!openTab) return;
    setLoading(true);
    if (openTab === "gallery") {
      fetchServerGallery({ page: 1, pageSize: 24 })
        .then((res) => setGallery(res.items || []))
        .catch(() => {})
        .finally(() => setLoading(false));
    } else if (openTab === "audio") {
      fetchAudioHistory()
        .then((res) => setAudios(res.records || []))
        .catch(() => {})
        .finally(() => setLoading(false));
    } else if (openTab === "prompts") {
      fetchPromptPlaza()
        .then((res) => setPrompts((res.items || []).slice(0, 30)))
        .catch(() => {})
        .finally(() => setLoading(false));
    }
  }, [openTab]);

  if (!openTab) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 backdrop-blur-sm">
      <div className="flex max-h-[85vh] w-full max-w-3xl flex-col overflow-hidden rounded-2xl border border-[var(--border-default)] bg-[var(--bg-surface)] text-[var(--text-primary)] shadow-2xl">
        <div className="flex items-center justify-between border-b border-[var(--border-default)] px-5 py-3">
          <div className="flex items-center gap-2 text-sm font-semibold">
            {openTab === "gallery" && (
              <>
                <ImageIcon className="h-4 w-4 text-indigo-500" />
                从「图片工坊画廊」引入角色 / 场景 / 分镜素材
              </>
            )}
            {openTab === "audio" && (
              <>
                <FileAudio className="h-4 w-4 text-teal-500" />
                从「配音工坊历史」引入配音或参考音色
              </>
            )}
            {openTab === "prompts" && (
              <>
                <Sparkles className="h-4 w-4 text-amber-500" />
                从「提示词广场」导入视觉画风或剧情灵感
              </>
            )}
          </div>
          <button type="button" onClick={onClose} className="rounded-lg p-1 hover:bg-[var(--bg-surface-hover)]">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto p-5">
          {loading ? (
            <div className="flex items-center justify-center py-12 text-xs text-[var(--text-muted)]">
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              正在加载全平台资产...
            </div>
          ) : openTab === "gallery" ? (
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
              {gallery.length === 0 && (
                <div className="col-span-full py-10 text-center text-xs text-[var(--text-muted)]">
                  图片画廊暂无持久化作品，可先在「图片工坊」生图或直接点击画布生成。
                </div>
              )}
              {gallery.map((item) => (
                <div
                  key={item.id}
                  className="flex flex-col overflow-hidden rounded-xl border border-[var(--border-default)] bg-[var(--bg-elevated)]"
                >
                  <img src={item.url} alt={item.prompt} className="h-32 w-full object-cover" />
                  <div className="p-2.5 flex-1 flex flex-col justify-between gap-2">
                    <p className="line-clamp-2 text-[11px] text-[var(--text-secondary)]">{item.prompt}</p>
                    <div className="grid grid-cols-3 gap-1 text-[10px]">
                      <button
                        type="button"
                        onClick={() => {
                          addNode("character", {
                            name: item.prompt.slice(0, 12) || "画廊角色",
                            appearanceBible: item.prompt,
                            portraitUrl: item.url,
                          });
                          onClose();
                        }}
                        className="rounded bg-violet-500/15 py-1 font-medium text-violet-600 dark:text-violet-300"
                      >
                        作角色
                      </button>
                      <button
                        type="button"
                        onClick={() => {
                          addNode("scene", {
                            name: item.prompt.slice(0, 12) || "画廊场景",
                            environmentPrompt: item.prompt,
                            imageUrl: item.url,
                          });
                          onClose();
                        }}
                        className="rounded bg-sky-500/15 py-1 font-medium text-sky-600 dark:text-sky-300"
                      >
                        作场景
                      </button>
                      <button
                        type="button"
                        onClick={() => {
                          addNode("shot", {
                            dialogue: item.prompt.slice(0, 40),
                            sceneAction: item.prompt,
                            imageUrl: item.url,
                          });
                          onClose();
                        }}
                        className="rounded bg-amber-500/15 py-1 font-medium text-amber-600 dark:text-amber-300"
                      >
                        作分镜
                      </button>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          ) : openTab === "audio" ? (
            <div className="space-y-2.5">
              {audios.length === 0 && (
                <div className="py-10 text-center text-xs text-[var(--text-muted)]">
                  配音工坊暂无历史音频记录。
                </div>
              )}
              {audios.map((aud) => (
                <div
                  key={aud.id}
                  className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-[var(--border-default)] bg-[var(--bg-elevated)] p-3"
                >
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-xs font-medium">{aud.input_text || aud.id}</div>
                    <div className="text-[10px] text-[var(--text-muted)]">
                      {aud.provider} · {aud.voice_name || aud.voice_id}
                    </div>
                    <audio src={aud.audio_url} controls className="mt-1.5 h-8 w-full max-w-md" />
                  </div>
                  <button
                    type="button"
                    onClick={() => {
                      addNode("audioImport", {
                        title: `配音_${aud.voice_name || aud.voice_id}`,
                        sourceAudioUrl: aud.audio_url,
                        transcript: aud.input_text,
                      });
                      onClose();
                    }}
                    className="rounded-lg bg-teal-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-teal-500"
                  >
                    置入音频节点
                  </button>
                </div>
              ))}
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2">
              {prompts.map((pt) => {
                const promptText = pt.prompt_zh || pt.prompt_en;
                return (
                  <div
                    key={pt.id}
                    className="flex flex-col justify-between rounded-xl border border-[var(--border-default)] bg-[var(--bg-elevated)] p-3"
                  >
                    <div>
                      <div className="text-xs font-semibold">{pt.title_zh}</div>
                      <p className="mt-1 line-clamp-3 text-[11px] text-[var(--text-secondary)]">{promptText}</p>
                    </div>
                    <div className="mt-2.5 flex gap-2">
                      <button
                        type="button"
                        onClick={() => {
                          updateScriptNode({ stylePreset: pt.prompt_en || promptText });
                          onClose();
                        }}
                        className="flex-1 rounded-lg border border-[var(--border-default)] py-1 text-[11px] hover:bg-[var(--bg-surface-hover)]"
                      >
                        设为全片画风锁
                      </button>
                      <button
                        type="button"
                        onClick={() => {
                          updateScriptNode({ outline: promptText, title: pt.title_zh });
                          onClose();
                        }}
                        className="flex-1 rounded-lg bg-indigo-600 py-1 text-[11px] text-white hover:bg-indigo-500"
                      >
                        作为剧本梗概
                      </button>
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
