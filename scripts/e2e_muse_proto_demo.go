//go:build ignore

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

type protoVideoAsset struct {
	Source      string `json:"source"` // "presentation" | "resource"
	MsgID       string `json:"msg_id"`
	Seq         int64  `json:"seq"`
	SessionID   string `json:"session_id"`
	Path        string `json:"path"`
	Label       string `json:"label"`
	OriginalURL string `json:"original_url"`
	ByteLen     int64  `json:"byte_len"`
}

type protoMatchResult struct {
	Found       bool             `json:"found"`
	Tier        string           `json:"tier"`
	BlobURL     string           `json:"blob_url"`
	Asset       *protoVideoAsset `json:"asset,omitempty"`
	MsgCount    int              `json:"msg_count"`
	PresCount   int              `json:"pres_count"`
	BlobCount   int              `json:"blob_count"`
	DebugWindow string           `json:"debug_window,omitempty"`
}

func getCookieFromLocalGateway() (string, string) {
	client := &http.Client{Timeout: 5 * time.Second}
	accResp, err := client.Get("http://127.0.0.1:18080/admin/api/v1/providers/meta_muse/accounts")
	if err != nil {
		return "", ""
	}
	defer accResp.Body.Close()
	var accList struct {
		Accounts []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Proxy  string `json:"proxy"`
		} `json:"accounts"`
	}
	if json.NewDecoder(accResp.Body).Decode(&accList) != nil || len(accList.Accounts) == 0 {
		return "", ""
	}
	targetID := accList.Accounts[0].ID
	proxy := accList.Accounts[0].Proxy
	for _, a := range accList.Accounts {
		if strings.EqualFold(a.Status, "active") {
			targetID = a.ID
			proxy = a.Proxy
			break
		}
	}

	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:18080/admin/api/v1/providers/meta_muse/accounts/%s/action", targetID),
		strings.NewReader(`{"action":"get_cookie"}`))
	if err != nil {
		return "", proxy
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", proxy
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Result struct {
			Cookie string `json:"cookie"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &parsed) != nil {
		return "", proxy
	}
	return parsed.Result.Cookie, proxy
}

func resolveDemoChromePath() string {
	if p := os.Getenv("CHROME_PATH"); p != "" {
		return p
	}
	if runtime.GOOS == "windows" {
		for _, p := range []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		} {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return "/usr/bin/google-chrome"
}

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
				state.blobs.push({ blobUrl: res, size: obj.size, type: obj.type || '' });
			}
		} catch (_) {}
		return res;
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

func resolveVideoByVID(ctx context.Context, vid string) (*protoMatchResult, error) {
	js := fmt.Sprintf(`(() => {
		const st = window.__museProtoState || { messages: [], presentations: [], blobs: [] };
		const vid = %q.toUpperCase();
		const findBlobBySize = (sz) => {
			if (!sz) return '';
			for (let i = st.blobs.length - 1; i >= 0; i--) {
				if (st.blobs[i].size === sz) return st.blobs[i].blobUrl;
			}
			return '';
		};

		// Tier 1: Direct VID match in presentation video path/label/originalUrl
		for (let i = st.presentations.length - 1; i >= 0; i--) {
			const pr = st.presentations[i];
			for (const v of (pr.videos || [])) {
				const hay = (v.path + ' ' + v.label + ' ' + v.originalUrl + ' ' + pr.fallbackText).toUpperCase();
				if (hay.includes(vid)) {
					return JSON.stringify({
						found: true, tier: 'tier1_presentation_vid',
						blob_url: findBlobBySize(v.byteLen),
						asset: { source: 'presentation', msg_id: pr.msgId, seq: pr.seq, session_id: pr.sessionId, path: v.path, label: v.label, original_url: v.originalUrl, byte_len: v.byteLen },
						msg_count: st.messages.length, pres_count: st.presentations.length, blob_count: st.blobs.length
					});
				}
			}
		}

		// Tier 2: Find message containing VID, then correlate within same sessionId & turn seq window
		const msgs = [...st.messages].sort((a, b) => a.seq - b.seq);
		const hitIdx = msgs.findIndex(m => (m.text || '').toUpperCase().includes(vid));
		if (hitIdx >= 0) {
			const hit = msgs[hitIdx];
			let nextUserSeq = Number.MAX_SAFE_INTEGER;
			for (let i = hitIdx + 1; i < msgs.length; i++) {
				if (msgs[i].sessionId === hit.sessionId && msgs[i].role === 'user' && msgs[i].seq > hit.seq) {
					nextUserSeq = msgs[i].seq;
					break;
				}
			}
			// Prefer presentation in [hit.seq, nextUserSeq)
			const presCand = st.presentations
				.filter(pr => pr.sessionId === hit.sessionId && pr.seq >= hit.seq && pr.seq < nextUserSeq && pr.videos.length > 0)
				.sort((a, b) => b.seq - a.seq);
			if (presCand.length > 0) {
				const pr = presCand[0];
				const v = pr.videos[0];
				return JSON.stringify({
					found: true, tier: 'tier2_turn_seq_presentation',
					blob_url: findBlobBySize(v.byteLen),
					asset: { source: 'presentation', msg_id: pr.msgId, seq: pr.seq, session_id: pr.sessionId, path: v.path, label: v.label, original_url: v.originalUrl, byte_len: v.byteLen },
					msg_count: st.messages.length, pres_count: st.presentations.length, blob_count: st.blobs.length,
					debug_window: 'hitSeq=' + hit.seq + ' nextUserSeq=' + nextUserSeq
				});
			}
			// Fallback to message resources in [hit.seq, nextUserSeq)
			for (let i = msgs.length - 1; i >= hitIdx; i--) {
				const m = msgs[i];
				if (m.sessionId === hit.sessionId && m.seq >= hit.seq && m.seq < nextUserSeq && m.resources && m.resources.length > 0) {
					const r = m.resources[0];
					return JSON.stringify({
						found: true, tier: 'tier2_turn_seq_resource',
						blob_url: findBlobBySize(r.byteLen),
						asset: { source: 'resource', msg_id: m.msgId, seq: m.seq, session_id: m.sessionId, path: r.path, label: r.label, original_url: r.originalUrl, byte_len: r.byteLen },
						msg_count: st.messages.length, pres_count: st.presentations.length, blob_count: st.blobs.length,
						debug_window: 'hitSeq=' + hit.seq + ' nextUserSeq=' + nextUserSeq
					});
				}
			}
		}

		return JSON.stringify({
			found: false, tier: 'none',
			msg_count: st.messages.length, pres_count: st.presentations.length, blob_count: st.blobs.length
		});
	})()`, vid)

	raw, err := chromedp.Run(ctx, chromedp.Evaluate[string](js))
	if err != nil {
		return nil, err
	}
	var out protoMatchResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func fetchVideoBytesInPage(ctx context.Context, targetURL string) ([]byte, error) {
	js := fmt.Sprintf(`(() => {
		const url = %q;
		return fetch(url, { credentials: 'include' }).then(r => {
			if (!r.ok) return 'ERR:' + r.status;
			return r.blob().then(b => new Promise((resolve) => {
				const reader = new FileReader();
				reader.onloadend = () => resolve(reader.result || '');
				reader.onerror = () => resolve('ERR:read');
				reader.readAsDataURL(b);
			}));
		}).catch(e => 'ERR:' + (e && e.message ? e.message : String(e)));
	})()`, targetURL)
	dataURI, err := chromedp.Run(ctx, chromedp.Evaluate[string](js, chromedp.EvalAwaitPromise))
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(dataURI, "ERR:") {
		return nil, fmt.Errorf("in-page fetch failed: %s", dataURI)
	}
	parts := strings.SplitN(dataURI, ",", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("unexpected data URI format")
	}
	return base64.StdEncoding.DecodeString(parts[1])
}

func main() {
	gwCookie, gwProxy := getCookieFromLocalGateway()
	if strings.TrimSpace(gwCookie) == "" {
		log.Fatalf("no muse cookie available from http://127.0.0.1:18080")
	}

	var cookies []map[string]string
	for _, part := range strings.Split(gwCookie, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && kv[0] != "" {
			cookies = append(cookies, map[string]string{"name": strings.TrimSpace(kv[0]), "value": strings.TrimSpace(kv[1])})
		}
	}
	fmt.Printf("Loaded %d cookies (proxy=%q)\n", len(cookies), gwProxy)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(resolveDemoChromePath()),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("incognito", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("lang", "zh-CN"),
		chromedp.Flag("headless", "new"),
		chromedp.WindowSize(1440, 900),
	)
	if gwProxy != "" {
		opts = append(opts, chromedp.ProxyServer(gwProxy))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 240*time.Second)
	defer cancelTimeout()

	_, _ = chromedp.Call(ctx, network.Enable, network.EnableParams{})
	_, _ = chromedp.Call(ctx, page.Enable, page.EnableParams{})
	_, _ = chromedp.Call(ctx, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{
		Source: museProtoHookJS,
	})

	fmt.Println("Step 1: Cookie injection on https://muse.ai/about ...")
	_, _ = chromedp.Run(ctx, chromedp.Navigate("https://muse.ai/about"))
	isSecure := true
	for _, c := range cookies {
		params := network.SetCookieParams{
			Name: c["name"], Value: c["value"], URL: "https://muse.ai/", Path: "/", Secure: &isSecure,
		}
		if !strings.HasPrefix(c["name"], "__Host-") && c["name"] != "hatch_native_auth_device" {
			params.Domain = ".muse.ai"
		}
		_, _ = chromedp.Call(ctx, network.SetCookie, params)
	}

	fmt.Println("Step 2: Navigating to https://muse.ai/ ...")
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := chromedp.Run(ctx, chromedp.Navigate("https://muse.ai/")); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	time.Sleep(10 * time.Second)
	_, _ = chromedp.Run(ctx, chromedp.Evaluate[chromedp.Void](museProtoHookJS))

	// Test A: Protocol-level Rescue of previously failed VID-EADC575A and existing VID-24D0CA11!
	for _, rescueVID := range []string{"VID-EADC575A", "VID-24D0CA11"} {
		res, err := resolveVideoByVID(ctx, rescueVID)
		if err != nil {
			fmt.Printf("[Test A - Rescue %s] error: %v\n", rescueVID, err)
			continue
		}
		fmt.Printf("[Test A - Rescue %s] found=%v tier=%s msgs=%d pres=%d blobs=%d blobURL=%s\n",
			rescueVID, res.Found, res.Tier, res.MsgCount, res.PresCount, res.BlobCount, res.BlobURL)
		if res.Found && res.Asset != nil {
			fmt.Printf("   Asset: path=%q byte_len=%d url=%q\n", res.Asset.Path, res.Asset.ByteLen, res.Asset.OriginalURL)
			fetchTarget := res.BlobURL
			if fetchTarget == "" {
				fetchTarget = res.Asset.OriginalURL
			}
			if fetchTarget != "" {
				raw, err := fetchVideoBytesInPage(ctx, fetchTarget)
				if err != nil {
					fmt.Printf("   Fetch %s failed: %v\n", fetchTarget, err)
					if res.BlobURL != "" && res.Asset.OriginalURL != "" && fetchTarget == res.BlobURL {
						raw, err = fetchVideoBytesInPage(ctx, res.Asset.OriginalURL)
						fmt.Printf("   Fallback fetch original_url: len=%d err=%v\n", len(raw), err)
					}
				}
				if len(raw) > 0 {
					_ = os.MkdirAll("data/videos", 0755)
					out := filepath.Join("data/videos", fmt.Sprintf("e2e_proto_rescue_%s.mp4", rescueVID))
					_ = os.WriteFile(out, raw, 0644)
					fmt.Printf("   >>> RESCUE EXTRACTION SUCCESS: %s (%d bytes, expected=%d) <<<\n", out, len(raw), res.Asset.ByteLen)
				}
			}
		}
	}

	// Test B: Submit a brand-new prompt with VID-PROTO-... and verify live delta.presentation + byte_len extraction
	newVID := fmt.Sprintf("VID-PROTO-%06X", time.Now().UnixNano()&0xFFFFFF)
	prompt := fmt.Sprintf(`[工作空间约束：请在"muse video"工作空间中保存生成的视频文件，文件命名包含 %s。]
[视频标识：%s]
Generate a 5-second calm ocean wave at sunset video.`, newVID, newVID)

	fmt.Printf("\nStep 3 (Test B): Submitting live prompt for %s ...\n", newVID)
	submitErr := func() error {
		subCtx, subCancel := context.WithTimeout(ctx, 15*time.Second)
		defer subCancel()
		if err := chromedp.Do(subCtx,
			chromedp.Click(`textarea[aria-label="消息"]`),
			chromedp.SendKeys(`textarea[aria-label="消息"]`, prompt),
			chromedp.Sleep(800*time.Millisecond),
			chromedp.Click(`button[aria-label="发送"]`),
		); err != nil {
			return err
		}
		return nil
	}()
	if submitErr != nil {
		fmt.Printf("Submit via aria-label selector warning: %v\n", submitErr)
	} else {
		fmt.Println("Prompt submitted! Polling protocol state for up to 120s...")
		for i := 0; i < 40; i++ {
			time.Sleep(3 * time.Second)
			res, err := resolveVideoByVID(ctx, newVID)
			if err != nil {
				fmt.Printf("[%ds] resolve error: %v\n", (i+1)*3, err)
				continue
			}
			fmt.Printf("[%ds] found=%v tier=%s msgs=%d pres=%d blobs=%d\n",
				(i+1)*3, res.Found, res.Tier, res.MsgCount, res.PresCount, res.BlobCount)
			if res.Found && res.Asset != nil && (res.BlobURL != "" || res.Asset.OriginalURL != "") {
				fetchTarget := res.BlobURL
				if fetchTarget == "" {
					fetchTarget = res.Asset.OriginalURL
				}
				raw, err := fetchVideoBytesInPage(ctx, fetchTarget)
				if err == nil && len(raw) > 0 {
					_ = os.MkdirAll("data/videos", 0755)
					out := filepath.Join("data/videos", fmt.Sprintf("e2e_proto_live_%s.mp4", newVID))
					_ = os.WriteFile(out, raw, 0644)
					fmt.Printf(">>> LIVE PROTOCOL EXTRACTION SUCCESS: %s (%d bytes, expected=%d, tier=%s) <<<\n",
						out, len(raw), res.Asset.ByteLen, res.Tier)
					break
				}
			}
		}
	}
}
