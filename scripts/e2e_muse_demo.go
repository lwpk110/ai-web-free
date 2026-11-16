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
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// 从 Python 获取解密后的所有 Cookie 列表
func getDecryptedCookies() ([]map[string]string, error) {
	cmd := exec.Command("python3", "-c", `
import os, sqlite3, shutil, secretstorage, hashlib, json
from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes

cookie_path = os.path.expanduser('~/.config/google-chrome/Default/Cookies')
if not os.path.exists(cookie_path):
    print(json.dumps([]))
    exit(0)

conn = secretstorage.dbus_init()
collection = secretstorage.get_default_collection(conn)
items = collection.search_items({'application': 'chrome', 'xdg:schema': 'chrome_libsecret_os_crypt_password_v2'})
chrome_secret = None
for item in items:
    chrome_secret = item.get_secret()
    break

if not chrome_secret:
    print(json.dumps([]))
    exit(0)

salt = b'saltysalt'
iv = b' ' * 16
key = hashlib.pbkdf2_hmac('sha1', chrome_secret, salt, 1, 16)

shutil.copyfile(cookie_path, '/tmp/chrome_cookies.db')
db_conn = sqlite3.connect('/tmp/chrome_cookies.db')
c = db_conn.cursor()
c.execute('SELECT host_key, name, path, is_secure, is_httponly, encrypted_value FROM cookies WHERE host_key LIKE "%muse.ai%"')
rows = c.fetchall()

result = []
for host, name, path, is_secure, is_httponly, enc in rows:
    if enc[:3] in (b'v10', b'v11'):
        cipher = Cipher(algorithms.AES(key), modes.CBC(iv))
        dec = cipher.decryptor().update(enc[3:]) + cipher.decryptor().finalize()
        pad = dec[-1]
        raw = dec[:-pad]
        val = raw[32:].decode('utf-8', errors='ignore')
        result.append({
            "name": name,
            "value": val,
            "domain": host,
            "path": path,
            "secure": bool(is_secure),
            "httpOnly": bool(is_httponly)
        })

print(json.dumps(result))
`)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("execute python decrypt: %w", err)
	}

	var cookies []map[string]string
	// json decode to slice of maps
	import_json := string(out)
	// simpler parser or json.Unmarshal
	var rawList []struct {
		Name     string `json:"name"`
		Value    string `json:"value"`
		Domain   string `json:"domain"`
		Path     string `json:"path"`
		Secure   bool   `json:"secure"`
		HttpOnly bool   `json:"httpOnly"`
	}
	if err := json.Unmarshal([]byte(import_json), &rawList); err != nil {
		return nil, err
	}

	for _, c := range rawList {
		cookies = append(cookies, map[string]string{
			"name":   c.Name,
			"value":  c.Value,
			"domain": c.Domain,
			"path":   c.Path,
		})
	}
	return cookies, nil
}

func getCookieFromLocalGateway() (string, string) {
	body := strings.NewReader(`{"action":"get_cookie"}`)
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:18080/admin/api/v1/providers/meta_muse/accounts/muse_acct_1/action", body)
	if err != nil {
		return "", ""
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Result struct {
			Cookie string `json:"cookie"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", ""
	}
	// 同时查询账号绑定的代理
	var proxy string
	if accResp, err := client.Get("http://127.0.0.1:18080/admin/api/v1/providers/meta_muse/accounts"); err == nil {
		defer accResp.Body.Close()
		var accList struct {
			Accounts []struct {
				ID    string `json:"id"`
				Proxy string `json:"proxy"`
			} `json:"accounts"`
		}
		if json.NewDecoder(accResp.Body).Decode(&accList) == nil {
			for _, a := range accList.Accounts {
				if a.ID == "muse_acct_1" {
					proxy = a.Proxy
					break
				}
			}
		}
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

func main() {
	var cookies []map[string]string
	var gwProxy string

	if envCookie := strings.TrimSpace(os.Getenv("MUSE_COOKIE")); envCookie != "" {
		for _, part := range strings.Split(envCookie, ";") {
			kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
			if len(kv) == 2 && kv[0] != "" {
				cookies = append(cookies, map[string]string{"name": strings.TrimSpace(kv[0]), "value": strings.TrimSpace(kv[1]), "path": "/"})
			}
		}
	} else if c, err := getDecryptedCookies(); err == nil && len(c) > 0 {
		cookies = c
	} else {
		gwCookie, p := getCookieFromLocalGateway()
		gwProxy = p
		for _, part := range strings.Split(gwCookie, ";") {
			kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
			if len(kv) == 2 && kv[0] != "" {
				cookies = append(cookies, map[string]string{"name": strings.TrimSpace(kv[0]), "value": strings.TrimSpace(kv[1]), "path": "/"})
			}
		}
	}

	if len(cookies) == 0 {
		log.Fatalf("no muse cookies available (checked Linux keyring, MUSE_COOKIE, and local gateway)")
	}
	fmt.Printf("Loaded %d cookies\n", len(cookies))

	proxyURL := os.Getenv("MUSE_PROXY")
	if proxyURL == "" {
		proxyURL = gwProxy
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("ALL_PROXY")
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("HTTP_PROXY")
	}

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
	if proxyURL != "" {
		opts = append(opts, chromedp.ProxyServer(proxyURL))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, cancelTimeout := context.WithTimeout(ctx, 300*time.Second)
	defer cancelTimeout()

	// 1. 先导航到 muse.ai 以设定 cookie 域
	fmt.Println("Step 1: Initial navigation...")
	_, _ = chromedp.Run(ctx, chromedp.Navigate("https://muse.ai/"))

	// 2. 注入所有真实 Session Cookies
	fmt.Println("Step 2: Injecting cookies...")
	isSecure := true
	for _, c := range cookies {
		name := c["name"]
		path := c["path"]
		if path == "" {
			path = "/"
		}
		params := network.SetCookieParams{
			Name:   name,
			Value:  c["value"],
			URL:    "https://muse.ai/",
			Path:   path,
			Secure: &isSecure,
		}
		if !strings.HasPrefix(name, "__Host-") && name != "hatch_native_auth_device" {
			domain := c["domain"]
			if domain == "" {
				domain = ".muse.ai"
			}
			params.Domain = domain
		}
		_, _ = chromedp.Call(ctx, network.SetCookie, params)
	}

	// 3. 重新导航到主页
	fmt.Println("Step 3: Navigating with authenticated session...")
	if _, err := chromedp.Run(ctx, chromedp.Navigate("https://muse.ai/")); err != nil {
		log.Fatalf("Navigate failed: %v", err)
	}

	// 等待 8 秒让页面 websocket 与 vue/react 加载
	time.Sleep(8 * time.Second)

	currentURL, _ := chromedp.Run(ctx, chromedp.Location())
	title, _ := chromedp.Run(ctx, chromedp.Title())
	fmt.Printf("Loaded Page URL: %s | Title: %s\n", currentURL, title)

	// 截图验证登录态
	buf, err := chromedp.Run(ctx, chromedp.FullScreenshot(90))
	if err == nil && len(buf) > 0 {
		screenshotPath := filepath.Join(os.TempDir(), "e2e_muse_logged_in.png")
		if err := os.WriteFile(screenshotPath, buf, 0644); err == nil {
			fmt.Printf("Screenshot saved to %s\n", screenshotPath)
		}
	}

	// 检查当前页面上的所有 video 标签和相关 blob URL，并异步转存为 Base64
	fmt.Println("Step 4: Extracting video blob with EvalAwaitPromise...")
	videoBase64, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
		const v = document.querySelector('video');
		if (!v || !v.src) return '';
		return fetch(v.src).then(r => r.blob()).then(b => new Promise((resolve) => {
			const reader = new FileReader();
			reader.onloadend = () => resolve(reader.result);
			reader.onerror = () => resolve('');
			reader.readAsDataURL(b);
		}));
	})()`, chromedp.EvalAwaitPromise))

	if err != nil {
		fmt.Printf("Extraction error: %v\n", err)
	} else if strings.HasPrefix(videoBase64, "data:") {
		parts := strings.SplitN(videoBase64, ",", 2)
		if len(parts) == 2 {
			rawBytes, err := base64.StdEncoding.DecodeString(parts[1])
			if err != nil {
				fmt.Printf("Base64 decode error: %v\n", err)
			} else {
				outPath := "data/videos/e2e_real_muse_video.mp4"
				_ = os.MkdirAll("data/videos", 0755)
				_ = os.WriteFile(outPath, rawBytes, 0644)
				fmt.Printf(">>> SUCCESS! Extracted real Muse video to %s, size: %d bytes <<<\n", outPath, len(rawBytes))
			}
		}
	} else {
		fmt.Printf("Video data prefix: %s\n", videoBase64[:min(50, len(videoBase64))])
	}

	// Step 5: 测试真实键盘输入 Prompt 并点击发送
	fmt.Println("\nStep 5: Focusing textarea and typing via SendKeys...")
	promptText := "生成一段仙鹤在云海中飞翔的5秒唯美短视频"

	// 记录发送前的 video 数量
	var initialVideoCount int
	initialVideoCount, _ = chromedp.Run(ctx, chromedp.Evaluate[int](`document.querySelectorAll('video').length`))
	fmt.Printf("Initial video count on page: %d\n", initialVideoCount)

	_ = chromedp.Do(ctx,
		chromedp.Focus(`textarea[aria-label="消息"]`),
		chromedp.SendKeys(`textarea[aria-label="消息"]`, promptText),
		chromedp.Sleep(1*time.Second),
	)

	// Step 6: 点击发送并等待新视频生成完成
	fmt.Println("\nStep 6: Clicking send button and polling for new video...")
	_ = chromedp.Do(ctx, chromedp.Click(`button[aria-label="发送"]`))

	startTime := time.Now()
	var newVideoURL string
	var newVideoBase64 string

	for i := 0; i < 60; i++ {
		time.Sleep(3 * time.Second)
		elapsed := time.Since(startTime).Round(time.Second)

		// 检查视频数量和状态
		statusInfo, _ := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
			const videos = Array.from(document.querySelectorAll('video'));
			const lastVideo = videos[videos.length - 1];
			const count = videos.length;
			
			// 查找页面最近的 AI 消息文本
			const bubbles = Array.from(document.querySelectorAll('[data-testid="hatch-chat-bubble"], p, span')).map(e => e.innerText.trim()).filter(t => t.length > 5 && t.length < 100);
			const lastText = bubbles.slice(-3).join(' | ');

			return JSON.stringify({
				count,
				lastSrc: lastVideo ? lastVideo.src : '',
				duration: lastVideo ? lastVideo.duration : 0,
				readyState: lastVideo ? lastVideo.readyState : 0,
				statusText: lastText
			});
		})()`))

		fmt.Printf("[%s] Polling state: %s\n", elapsed, statusInfo)

		// 如果发现新视频已渲染且具备时长
		type pollStatus struct {
			Count      int     `json:"count"`
			LastSrc    string  `json:"lastSrc"`
			Duration   float64 `json:"duration"`
			ReadyState int     `json:"readyState"`
			StatusText string  `json:"statusText"`
		}
		var st pollStatus
		_ = json.Unmarshal([]byte(statusInfo), &st)

		if st.Count > initialVideoCount && st.LastSrc != "" && st.Duration > 0 {
			fmt.Printf("\n>>> NEW VIDEO DETECTED! Duration: %.2fs, Src: %s <<<\n", st.Duration, st.LastSrc)
			newVideoURL = st.LastSrc

			// 提取该视频的 Base64
			newVideoBase64, _ = chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
				const videos = document.querySelectorAll('video');
				const v = videos[videos.length - 1];
				if (!v || !v.src) return '';
				return fetch(v.src).then(r => r.blob()).then(b => new Promise((resolve) => {
					const reader = new FileReader();
					reader.onloadend = () => resolve(reader.result);
					reader.onerror = () => resolve('');
					reader.readAsDataURL(b);
				}));
			})()`, chromedp.EvalAwaitPromise))
			break
		}
	}

	if strings.HasPrefix(newVideoBase64, "data:") {
		parts := strings.SplitN(newVideoBase64, ",", 2)
		if len(parts) == 2 {
			rawBytes, err := base64.StdEncoding.DecodeString(parts[1])
			if err != nil {
				fmt.Printf("Base64 decode error: %v\n", err)
			} else {
				outPath := "data/videos/e2e_newly_generated_crane_video.mp4"
				_ = os.WriteFile(outPath, rawBytes, 0644)
				fmt.Printf("\n=======================================================\n")
				fmt.Printf("🎉 E2E TEST FULL SUCCESS! 🎉\n")
				fmt.Printf("Newly generated video saved to %s\n", outPath)
				fmt.Printf("File size: %d bytes (%.2f MB)\n", len(rawBytes), float64(len(rawBytes))/(1024*1024))
				fmt.Printf("=======================================================\n")

				// 截图保存成片后的交互界面
				finalBuf, _ := chromedp.Run(ctx, chromedp.FullScreenshot(90))
				if len(finalBuf) > 0 {
					finalPic := filepath.Join(os.TempDir(), "e2e_muse_after_generation.png")
					_ = os.WriteFile(finalPic, finalBuf, 0644)
					fmt.Printf("Final evidence screenshot saved to %s\n", finalPic)
				}
			}
		}
	} else {
		fmt.Printf("Generation finished or timed out. Last URL: %s\n", newVideoURL)
	}
}
