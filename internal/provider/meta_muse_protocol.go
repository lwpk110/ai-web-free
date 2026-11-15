package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// MuseUpstreamMeta 记录 Muse 上游协议层返回的确定性关联元数据，用于持久化与抢救定位
type MuseUpstreamMeta struct {
	VidID             string `json:"vid_id,omitempty"`
	AccountEmail      string `json:"account_email,omitempty"`
	UpstreamSessionID string `json:"upstream_session_id,omitempty"`
	UpstreamMsgID     string `json:"upstream_msg_id,omitempty"`
	UpstreamSeq       int64  `json:"upstream_seq,omitempty"`
	UpstreamAssetPath string `json:"upstream_asset_path,omitempty"`
	UpstreamAssetURL  string `json:"upstream_asset_url,omitempty"`
	ExpectedBytes     int64  `json:"expected_bytes,omitempty"`
	UpstreamMeta      string `json:"upstream_meta,omitempty"`
}

type museProtoVideoAsset struct {
	Source      string `json:"source"` // "presentation" | "resource"
	MsgID       string `json:"msg_id"`
	Seq         int64  `json:"seq"`
	SessionID   string `json:"session_id"`
	Path        string `json:"path"`
	Label       string `json:"label"`
	OriginalURL string `json:"original_url"`
	ByteLen     int64  `json:"byte_len"`
}

type museProtoMatchResult struct {
	Found     bool                 `json:"found"`
	Tier      string               `json:"tier"`
	BlobURL   string               `json:"blob_url"`
	Asset     *museProtoVideoAsset `json:"asset,omitempty"`
	MsgCount  int                  `json:"msg_count"`
	PresCount int                  `json:"pres_count"`
	BlobCount int                  `json:"blob_count"`
}

// museProtoHookJS 在浏览器 JS 运行时层拦截 Noise-XX 解密后的 JSON 事件与视频 Blob 分配：
// 记录 chat_events、delta.message_done、delta.presentation 及 video/* 的 URL.createObjectURL(size)。
const museProtoHookJS = `(() => {
	if (window.__museProtoState) return;
	const state = window.__museProtoState = {
		messages: [],
		presentations: [],
		blobs: [],
		seenMsgKeys: new Set(),
		seenPresKeys: new Set(),
	};

	const parseMeta = (raw) => {
		if (!raw) return null;
		if (typeof raw === 'object') return raw;
		try { return JSON.parse(raw); } catch (_) { return null; }
	};

	const recordResources = (resList) => {
		if (!Array.isArray(resList)) return [];
		const out = [];
		for (const r of resList) {
			if (!r) continue;
			const meta = parseMeta(r.metadata);
			const kind = r.kind || meta?.kind || '';
			const mime = r.mime_type || meta?.mime_type || '';
			if (kind === 'video' || mime.startsWith('video/')) {
				out.push({
					kind: kind || 'video',
					label: r.label || '',
					path: meta?.path || r.uri || '',
					originalUrl: meta?.variants?.original || '',
					byteLen: Number(meta?.size_bytes || 0),
				});
			}
		}
		return out;
	};

	const addMessage = (m) => {
		if (!m) return;
		const msgId = m.message_id || m.id || '';
		const seq = Number(m.seq ?? m.chat_event_seq ?? 0);
		const key = msgId + ':' + seq;
		if (state.seenMsgKeys.has(key)) return;
		state.seenMsgKeys.add(key);

		const p = m.payload || m;
		let text = m.display_text || p.display_text || p.content || '';
		if (!text && m.transcript && Array.isArray(m.transcript.messages)) {
			const parts = [];
			for (const tm of m.transcript.messages) {
				for (const c of (tm.content || [])) {
					if (c && c.text) parts.push(c.text);
				}
			}
			text = parts.join('\n');
		}
		const resources = recordResources(m.resources || p.resources);
		state.messages.push({
			msgId,
			replyToMsgId: m.reply_to_message_id || p.reply_to_message_id || '',
			rootExecId: p.metadata?.root_message_execution_id || m.metadata?.root_message_execution_id || '',
			seq,
			sessionId: p.session_id || m.chat_context?.chat_id || '',
			role: m.role || p.role || '',
			text: String(text || '').slice(0, 1500),
			resources,
		});
		if (Array.isArray(m.presentations || p.presentations)) {
			for (const pr of (m.presentations || p.presentations)) addPresentation(pr);
		}
	};

	const addPresentation = (pr) => {
		if (!pr) return;
		const p = pr.payload || pr;
		const data = p.data || {};
		const videos = Array.isArray(data.videos) ? data.videos : [];
		if (videos.length === 0 && p.kind !== 'video') return;
		const msgId = p.message_id || p.id || '';
		const seq = Number(p.seq ?? p.chat_event_seq ?? 0);
		const key = msgId + ':' + seq + ':' + (p.id || '');
		if (state.seenPresKeys.has(key)) return;
		state.seenPresKeys.add(key);

		state.presentations.push({
			msgId,
			seq,
			sessionId: p.session_id || p.chat_context?.chat_id || '',
			fallbackText: p.fallback_text || data.fallback_text || '',
			videos: videos.map(v => ({
				path: v.path || '',
				label: v.label || '',
				originalUrl: v.variants?.original || '',
				byteLen: Number(v.byte_len || 0),
			})),
		});
	};

	const origCreateObj = URL.createObjectURL.bind(URL);
	URL.createObjectURL = function(obj) {
		const res = origCreateObj(obj);
		try {
			if (obj && typeof obj.size === 'number' && String(obj.type || '').startsWith('video/')) {
				state.blobs.push({ blobUrl: res, size: obj.size, type: obj.type || '', blobObj: obj });
				if (state.blobs.length > 30) {
					state.blobs.shift();
				}
			}
		} catch (_) {}
		return res;
	};

	window.__museReadVideoDataURL = function(targetUrl, expectedSize) {
		const readBlob = (b) => {
			if (!b || b.size < 100000) return Promise.resolve('too_small:' + (b ? b.size : 0));
			return new Promise((resolve) => {
				const reader = new FileReader();
				reader.onloadend = () => resolve(reader.result || 'ERR:empty_reader');
				reader.onerror = () => resolve('ERR:reader_error');
				reader.readAsDataURL(b);
			});
		};
		for (let i = state.blobs.length - 1; i >= 0; i--) {
			const item = state.blobs[i];
			if (item && item.blobObj && (item.blobUrl === targetUrl || (expectedSize > 0 && item.size === expectedSize))) {
				return readBlob(item.blobObj);
			}
		}
		if (!targetUrl) return Promise.resolve('ERR:empty_target_url');
		return fetch(targetUrl).then(r => {
			if (!r.ok) return 'ERR:http_' + r.status;
			return r.blob().then(readBlob);
		}).catch(e => 'ERR:fetch_' + (e && e.message ? e.message : String(e)));
	};

	const origJSONParse = JSON.parse;
	JSON.parse = function(text, reviver) {
		const parsed = origJSONParse.call(this, text, reviver);
		try {
			if (parsed && typeof parsed === 'object') {
				if (parsed.ok === true && parsed.result && Array.isArray(parsed.result.chat_events)) {
					for (const ev of parsed.result.chat_events) {
						if (ev.event_name === 'presentation' || ev.payload?.kind === 'video' || ev.payload?.data?.videos) {
							addPresentation(ev);
						} else {
							addMessage(ev);
						}
					}
				} else if (parsed.event === 'delta.message_done' && parsed.payload) {
					addMessage(parsed.payload);
				} else if (parsed.event === 'delta.presentation' && parsed.payload) {
					addPresentation(parsed.payload);
				}
			}
		} catch (_) {}
		return parsed;
	};
})();`

// installMuseProtoHook 在页面文档加载前注入协议级监听 Hook
func installMuseProtoHook(ctx context.Context) {
	_, _ = chromedp.Call(ctx, page.Enable, page.EnableParams{})
	_, _ = chromedp.Call(ctx, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{
		Source: museProtoHookJS,
	})
}

// ensureMuseProtoHook 幂等兜底确保当前页面已挂载协议监听状态
func ensureMuseProtoHook(ctx context.Context) {
	_, _ = chromedp.Run(ctx, chromedp.Evaluate[chromedp.Void](museProtoHookJS))
}

// resolveMuseProtoVideo 按 4 级确定性规则从 Muse 上游协议状态中关联视频产物与内存 Blob：
// Tier 0: 已知持久化上游元数据（asset_path / msg_id / seq）精确命中
// Tier 1: delta.presentation 视频路径/标签/直链直接包含 VID-xxxx
// Tier 2: 按同 sessionId 的 [hitSeq, nextUserSeq) 轮次窗口关联 presentation / resources
// Tier 3: 通过 byte_len 精确匹配 URL.createObjectURL 生成的 video/* blob URL
func resolveMuseProtoVideo(ctx context.Context, meta MuseUpstreamMeta, promptPrefix string) (*museProtoMatchResult, error) {
	metaJSON, _ := json.Marshal(meta)
	prefixJSON, _ := json.Marshal(strings.TrimSpace(promptPrefix))

	js := fmt.Sprintf(`(() => {
		const st = window.__museProtoState || { messages: [], presentations: [], blobs: [] };
		const target = %s;
		const promptPrefix = %s;
		const vid = String(target.vid_id || '').trim().toUpperCase();

		const findBlobBySize = (sz) => {
			if (!sz) return '';
			for (let i = st.blobs.length - 1; i >= 0; i--) {
				if (st.blobs[i].size === sz) return st.blobs[i].blobUrl;
			}
			return '';
		};

		const buildRes = (found, tier, source, prOrMsg, v) => ({
			found,
			tier,
			blob_url: findBlobBySize(v.byteLen || target.expected_bytes || 0),
			asset: {
				source,
				msg_id: prOrMsg.msgId || '',
				seq: prOrMsg.seq || 0,
				session_id: prOrMsg.sessionId || '',
				path: v.path || '',
				label: v.label || '',
				original_url: v.originalUrl || '',
				byte_len: v.byteLen || 0,
			},
			msg_count: st.messages.length,
			pres_count: st.presentations.length,
			blob_count: st.blobs.length,
		});

		// Tier 0: 已落库的精确上游路径 / 消息ID 命中（抢救优先）
		if (target.upstream_asset_path || target.upstream_msg_id) {
			for (let i = st.presentations.length - 1; i >= 0; i--) {
				const pr = st.presentations[i];
				for (const v of (pr.videos || [])) {
					if ((target.upstream_asset_path && v.path === target.upstream_asset_path) ||
						(target.upstream_msg_id && pr.msgId === target.upstream_msg_id)) {
						return JSON.stringify(buildRes(true, 'tier0_saved_meta', 'presentation', pr, v));
					}
				}
			}
		}

		// Tier 1: presentation 视频路径或 fallback_text 直接含 VID-xxxx
		if (vid) {
			for (let i = st.presentations.length - 1; i >= 0; i--) {
				const pr = st.presentations[i];
				for (const v of (pr.videos || [])) {
					const hay = (v.path + ' ' + v.label + ' ' + v.originalUrl + ' ' + pr.fallbackText).toUpperCase();
					if (hay.includes(vid)) {
						return JSON.stringify(buildRes(true, 'tier1_presentation_vid', 'presentation', pr, v));
					}
				}
			}
		}

		// Tier 2: 定位包含 VID（或旧任务 promptPrefix）的消息，在同 sessionId 的 [hitSeq, nextUserSeq) 轮次窗口内匹配成片
		const msgs = [...st.messages].sort((a, b) => a.seq - b.seq);
		let hitIdx = -1;
		if (vid) {
			hitIdx = msgs.findIndex(m => (m.text || '').toUpperCase().includes(vid));
		}
		if (hitIdx < 0 && promptPrefix && promptPrefix.length >= 6) {
			for (let i = msgs.length - 1; i >= 0; i--) {
				if ((msgs[i].text || '').includes(promptPrefix)) {
					hitIdx = i;
					break;
				}
			}
		}
		if (hitIdx >= 0) {
			const hit = msgs[hitIdx];
			let nextUserSeq = Number.MAX_SAFE_INTEGER;
			for (let i = hitIdx + 1; i < msgs.length; i++) {
				if (msgs[i].sessionId === hit.sessionId && msgs[i].role === 'user' && msgs[i].seq > hit.seq) {
					nextUserSeq = msgs[i].seq;
					break;
				}
			}
			const presCand = st.presentations
				.filter(pr => pr.sessionId === hit.sessionId && pr.seq >= hit.seq && pr.seq < nextUserSeq && pr.videos.length > 0)
				.sort((a, b) => b.seq - a.seq);
			if (presCand.length > 0) {
				return JSON.stringify(buildRes(true, 'tier2_turn_seq_presentation', 'presentation', presCand[0], presCand[0].videos[0]));
			}
			for (let i = msgs.length - 1; i >= hitIdx; i--) {
				const m = msgs[i];
				if (m.sessionId === hit.sessionId && m.seq >= hit.seq && m.seq < nextUserSeq && m.resources && m.resources.length > 0) {
					return JSON.stringify(buildRes(true, 'tier2_turn_seq_resource', 'resource', m, m.resources[0]));
				}
			}
		}

		// Tier 3 (兜底): 若仅有 expected_bytes 且内存已有精确匹配的 video blob
		if (target.expected_bytes > 0) {
			const b = findBlobBySize(target.expected_bytes);
			if (b) {
				return JSON.stringify({
					found: true,
					tier: 'tier3_expected_bytes_blob',
					blob_url: b,
					asset: {
						source: 'saved_meta',
						msg_id: target.upstream_msg_id || '',
						seq: target.upstream_seq || 0,
						session_id: target.upstream_session_id || '',
						path: target.upstream_asset_path || '',
						original_url: target.upstream_asset_url || '',
						byte_len: target.expected_bytes,
					},
					msg_count: st.messages.length,
					pres_count: st.presentations.length,
					blob_count: st.blobs.length,
				});
			}
		}

		return JSON.stringify({
			found: false,
			tier: 'none',
			msg_count: st.messages.length,
			pres_count: st.presentations.length,
			blob_count: st.blobs.length,
		});
	})()`, string(metaJSON), string(prefixJSON))

	raw, err := chromedp.Run(ctx, chromedp.Evaluate[string](js))
	if err != nil {
		return nil, err
	}
	var out museProtoMatchResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// scrollToMuseProtoHint 将包含 VID / 文件名 / 提示词前缀的历史消息滚动至视口，触发懒加载视频水合 Blob
func scrollToMuseProtoHint(ctx context.Context, meta MuseUpstreamMeta, promptPrefix string) {
	hints := make([]string, 0, 4)
	if v := strings.TrimSpace(meta.VidID); v != "" {
		hints = append(hints, v)
	}
	if meta.UpstreamAssetPath != "" {
		parts := strings.Split(meta.UpstreamAssetPath, "/")
		if last := strings.TrimSpace(parts[len(parts)-1]); last != "" {
			hints = append(hints, last)
		}
	}
	if p := strings.TrimSpace(promptPrefix); p != "" {
		hints = append(hints, p)
	}
	hintsJSON, _ := json.Marshal(hints)
	js := fmt.Sprintf(`(() => {
		const hints = %s;
		for (const h of hints) {
			if (!h) continue;
			const nodes = Array.from(document.querySelectorAll('p, span, div')).filter(el => {
				if (el.closest('aside') || el.tagName === 'TEXTAREA' || el.closest('textarea')) return false;
				const t = (el.innerText || '').trim();
				return t.includes(h) && t.length < 2000 && !Array.from(el.children).some(c => (c.innerText || '').includes(h));
			});
			if (nodes.length > 0) {
				try { nodes[nodes.length - 1].scrollIntoView({ block: 'center' }); } catch (_) {}
				return true;
			}
		}
		return false;
	})()`, string(hintsJSON))
	_, _ = chromedp.Run(ctx, chromedp.Evaluate[bool](js))
}

// extractMuseVideoBase64 优先直接读取缓存的 Blob 对象（免疫 revokeObjectURL 与 CORS），否则回退 fetch(targetURL)
func extractMuseVideoBase64(ctx context.Context, targetURL string, expectedBytes int64) (string, error) {
	js := fmt.Sprintf(`(() => {
		const target = %q;
		const exp = %d;
		if (typeof window.__museReadVideoDataURL === 'function') {
			return window.__museReadVideoDataURL(target, exp);
		}
		if (!target) return Promise.resolve('ERR:empty_target_url');
		return fetch(target).then(r => r.blob()).then(b => {
			if (b.size < 100000) return 'too_small:' + b.size;
			return new Promise((resolve) => {
				const reader = new FileReader();
				reader.onloadend = () => resolve(reader.result || 'ERR:empty_reader');
				reader.onerror = () => resolve('ERR:reader_error');
				reader.readAsDataURL(b);
			});
		}).catch(e => 'ERR:fetch_' + (e && e.message ? e.message : String(e)));
	})()`, targetURL, expectedBytes)
	return chromedp.Run(ctx, chromedp.Evaluate[string](js, chromedp.EvalAwaitPromise))
}

func toMuseUpstreamMeta(res *museProtoMatchResult, vidID, accountEmail string) MuseUpstreamMeta {
	out := MuseUpstreamMeta{
		VidID:        vidID,
		AccountEmail: accountEmail,
	}
	if res == nil || res.Asset == nil {
		return out
	}
	out.UpstreamSessionID = res.Asset.SessionID
	out.UpstreamMsgID = res.Asset.MsgID
	out.UpstreamSeq = res.Asset.Seq
	out.UpstreamAssetPath = res.Asset.Path
	out.UpstreamAssetURL = res.Asset.OriginalURL
	out.ExpectedBytes = res.Asset.ByteLen
	extra, _ := json.Marshal(map[string]any{
		"match_tier": res.Tier,
		"source":     res.Asset.Source,
		"label":      res.Asset.Label,
	})
	out.UpstreamMeta = string(extra)
	return out
}
