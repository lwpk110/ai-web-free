package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/config"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/storage"

	"github.com/gin-gonic/gin"
)

func TestUnifiedMediaCrawlImportCloneAndReuseAcrossStudios(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 1. 构建离线来源网页与媒体资源 + 模拟 SiliconFlow 音色上传复刻与 TTS 合成端
	var webSrvURL string
	originMux := http.NewServeMux()
	originMux.HandleFunc("/drama-assets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>短剧素材库</title></head><body>
<div><p>霸总低沉对白：签下协议，从此两清。</p><audio controls src="` + webSrvURL + `/files/ceo.mp3" title="霸总低沉音色"></audio></div>
<img src="` + webSrvURL + `/files/heroine.png" alt="古风大女主定妆参考图" />
<video controls><source src="` + webSrvURL + `/files/palace.mp4" type="video/mp4" /></video>
</body></html>`))
	})
	originMux.HandleFunc("/files/ceo.mp3", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3-ceo-sample-audio-bytes-for-cloning"))
	})
	originMux.HandleFunc("/files/heroine.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nMOCK_PNG_REFERENCE_BYTES"))
	})
	originMux.HandleFunc("/files/palace.mp4", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("\x00\x00\x00\x18ftypmp42MOCK_VIDEO_BYTES"))
	})
	originSrv := httptest.NewServer(originMux)
	defer originSrv.Close()
	webSrvURL = originSrv.URL

	var synthesizedVoice string
	sfMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/uploads/audio/voice":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"uri":"speech:custom_ceo:mock999"}`))
		case "/audio/speech":
			var req struct {
				Voice string `json:"voice"`
				Input string `json:"input"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			synthesizedVoice = req.Voice
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("ID3-synthesized-by-" + req.Voice))
		default:
			http.NotFound(w, r)
		}
	}))
	defer sfMock.Close()

	memStore := storage.NewMemoryStorageEngine()
	sfProv := provider.NewSiliconFlowProvider(nil, sfMock.URL)
	reg := provider.NewRegistry()
	reg.Register(provider.NewEdgeTTSProvider())
	reg.Register(sfProv)
	reg.Register(provider.NewDashScopeProvider(nil, ""))
	rt := &GatewayRuntime{
		StorageEngine: memStore,
	}
	audioH := NewAudioHandler(accounts.NewPool(nil), &config.Config{})
	audioH.SetRuntime(rt)
	audioH.SetRegistry(reg)
	galleryH := NewGalleryHandler(nil, memStore)
	videoH := NewVideoHandler(reg, nil)
	videoH.SetStorageEngine(memStore)
	mediaH := NewMediaLibraryHandler(audioH, galleryH, videoH, memStore)
	mediaH.SetCrawlerOptions(provider.MediaCrawlOptions{AllowPrivateTest: true})

	r := gin.New()
	r.GET("/v1/audio/voices", audioH.ListVoices)
	r.GET("/v1/audio/voices/:id/preview", audioH.PreviewVoice)
	r.POST("/v1/audio/speech", audioH.TTS)
	r.POST("/v1/audio/voices/crawl", audioH.CrawlVoices)
	r.POST("/v1/audio/voices/crawl-import", audioH.ImportCrawledVoice)
	r.POST("/v1/audio/voices/custom/:id/clone", audioH.CloneCustomVoice)
	r.DELETE("/v1/audio/voices/custom/:id", audioH.DeleteCustomVoice)
	r.POST("/v1/media/crawl", mediaH.CrawlMedia)
	r.POST("/v1/media/crawl-import", mediaH.ImportCrawledMedia)
	r.GET("/v1/media/assets", mediaH.ListSystemAssets)
	r.GET("/admin/api/v1/gallery", galleryH.ListWorks)
	r.GET("/storage/images/:filename", galleryH.ServeStaticImage)
	r.GET("/admin/api/v1/multimodal/video-tasks", videoH.ListTasks)
	r.GET("/v1/videos/files/:id", videoH.StreamVideoFile)

	// 2. 执行统一爬虫：同时发现现有短剧音色与网页音色/参考图/视频候选
	crawlReq := httptest.NewRequest(http.MethodPost, "/v1/media/crawl", bytes.NewBufferString(`{"query":"霸总","url":"`+webSrvURL+`/drama-assets","media_kind":"auto"}`))
	crawlReq.Header.Set("Content-Type", "application/json")
	crawlW := httptest.NewRecorder()
	r.ServeHTTP(crawlW, crawlReq)
	if crawlW.Code != http.StatusOK {
		t.Fatalf("crawl expected 200, got %d: %s", crawlW.Code, crawlW.Body.String())
	}
	var crawlRes struct {
		ExistingVoices []provider.VoiceDescriptor      `json:"existing_voices"`
		Candidates     []provider.CrawledMediaCandidate `json:"candidates"`
	}
	_ = json.Unmarshal(crawlW.Body.Bytes(), &crawlRes)
	if len(crawlRes.ExistingVoices) == 0 || len(crawlRes.Candidates) < 3 {
		t.Fatalf("expected existing drama voice matches and 3+ web candidates, got existing=%d cands=%d", len(crawlRes.ExistingVoices), len(crawlRes.Candidates))
	}

	// 3. 无 API Key 时导入抓取音频：先缓存本地样音且可试听预览（status=cached_pending_key）
	impVoiceReq := httptest.NewRequest(http.MethodPost, "/v1/media/crawl-import", bytes.NewBufferString(`{
		"media_kind":"voice",
		"url":"`+webSrvURL+`/files/ceo.mp3",
		"title":"网页霸总样本",
		"role":"霸总男主",
		"transcript":"签下协议，从此两清。"
	}`))
	impVoiceReq.Header.Set("Content-Type", "application/json")
	impVoiceW := httptest.NewRecorder()
	r.ServeHTTP(impVoiceW, impVoiceReq)
	if impVoiceW.Code != http.StatusOK {
		t.Fatalf("import crawled voice failed %d: %s", impVoiceW.Code, impVoiceW.Body.String())
	}
	var impVoiceRes struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal(impVoiceW.Body.Bytes(), &impVoiceRes)
	if impVoiceRes.ID == "" || impVoiceRes.Status != "cached_pending_key" {
		t.Fatalf("expected cached_pending_key without cloud key, got %+v", impVoiceRes)
	}

	// 本地样音立即可在 Preview 路由试听
	prevReq := httptest.NewRequest(http.MethodGet, "/v1/audio/voices/"+impVoiceRes.ID+"/preview", nil)
	prevW := httptest.NewRecorder()
	r.ServeHTTP(prevW, prevReq)
	if prevW.Code != http.StatusOK || !strings.Contains(prevW.Body.String(), "ID3-ceo-sample") {
		t.Fatalf("expected cached sample playable from /preview, got %d %q", prevW.Code, prevW.Body.String())
	}

	// 4. 用户补充云端 SILICONFLOW_API_KEY 后一键复刻并在 /v1/audio/speech 复用
	_, err := sfProv.ImportCredentials(provider.ImportRequest{CredentialType: "api_key", RawText: "sk-live-test"})
	if err != nil {
		t.Fatalf("import sf key failed: %v", err)
	}
	cloneReq := httptest.NewRequest(http.MethodPost, "/v1/audio/voices/custom/"+impVoiceRes.ID+"/clone", bytes.NewBufferString(`{"clone_engine":"siliconflow"}`))
	cloneReq.Header.Set("Content-Type", "application/json")
	cloneW := httptest.NewRecorder()
	r.ServeHTTP(cloneW, cloneReq)
	if cloneW.Code != http.StatusOK || !strings.Contains(cloneW.Body.String(), "speech:custom_ceo:mock999") {
		t.Fatalf("expected custom voice clone promotion, got %d: %s", cloneW.Code, cloneW.Body.String())
	}

	// 调用 /v1/audio/speech 使用自定义 ID 合成台词 -> 透明转发至 speech:custom_ceo:mock999
	ttsReq := httptest.NewRequest(http.MethodPost, "/v1/audio/speech", bytes.NewBufferString(`{"model":"tts-1","voice":"`+impVoiceRes.ID+`","input":"今晚别走"}`))
	ttsReq.Header.Set("Content-Type", "application/json")
	ttsW := httptest.NewRecorder()
	r.ServeHTTP(ttsW, ttsReq)
	if ttsW.Code != http.StatusOK || synthesizedVoice != "speech:custom_ceo:mock999" {
		t.Fatalf("expected TTS synthesis routed via cloned voice, code=%d voice=%q body=%s", ttsW.Code, synthesizedVoice, ttsW.Body.String())
	}

	// 5. 同套爬虫入库规则应用于图片工坊与视频工坊：抓取图/视频后写入系统库复用
	impImgReq := httptest.NewRequest(http.MethodPost, "/v1/media/crawl-import", bytes.NewBufferString(`{
		"media_kind":"image",
		"url":"`+webSrvURL+`/files/heroine.png",
		"title":"古风大女主定妆图",
		"role":"character"
	}`))
	impImgReq.Header.Set("Content-Type", "application/json")
	impImgW := httptest.NewRecorder()
	r.ServeHTTP(impImgW, impImgReq)
	if impImgW.Code != http.StatusOK || !strings.Contains(impImgW.Body.String(), "/storage/images/") {
		t.Fatalf("expected crawled image cached to /storage/images/, got %d: %s", impImgW.Code, impImgW.Body.String())
	}

	impVidReq := httptest.NewRequest(http.MethodPost, "/v1/media/crawl-import", bytes.NewBufferString(`{
		"media_kind":"video",
		"url":"`+webSrvURL+`/files/palace.mp4",
		"title":"古风宫殿空镜"
	}`))
	impVidReq.Header.Set("Content-Type", "application/json")
	impVidW := httptest.NewRecorder()
	r.ServeHTTP(impVidW, impVidReq)
	if impVidW.Code != http.StatusOK || !strings.Contains(impVidW.Body.String(), "/v1/videos/files/") {
		t.Fatalf("expected crawled video cached to /v1/videos/files/, got %d: %s", impVidW.Code, impVidW.Body.String())
	}

	// 检查各工坊接口与统一 /v1/media/assets 均能看到已缓存复用的资产
	galW := httptest.NewRecorder()
	r.ServeHTTP(galW, httptest.NewRequest(http.MethodGet, "/admin/api/v1/gallery", nil))
	if galW.Code != http.StatusOK || !strings.Contains(galW.Body.String(), "古风大女主定妆图") {
		t.Fatalf("expected gallery list to expose crawled reference image, got %d: %s", galW.Code, galW.Body.String())
	}

	vidListW := httptest.NewRecorder()
	r.ServeHTTP(vidListW, httptest.NewRequest(http.MethodGet, "/admin/api/v1/multimodal/video-tasks", nil))
	if vidListW.Code != http.StatusOK || !strings.Contains(vidListW.Body.String(), "古风宫殿空镜") {
		t.Fatalf("expected video task list to expose crawled video clip, got %d: %s", vidListW.Code, vidListW.Body.String())
	}

	assetsW := httptest.NewRecorder()
	r.ServeHTTP(assetsW, httptest.NewRequest(http.MethodGet, "/v1/media/assets?kind=auto", nil))
	if assetsW.Code != http.StatusOK || !strings.Contains(assetsW.Body.String(), impVoiceRes.ID) || !strings.Contains(assetsW.Body.String(), "古风大女主定妆图") {
		t.Fatalf("expected /v1/media/assets to include custom voice & crawled image, got %s", assetsW.Body.String())
	}

	// 6. 删除自定义音色确认生效
	delW := httptest.NewRecorder()
	r.ServeHTTP(delW, httptest.NewRequest(http.MethodDelete, "/v1/audio/voices/custom/"+impVoiceRes.ID, nil))
	if delW.Code != http.StatusOK {
		t.Fatalf("expected delete custom voice 200, got %d: %s", delW.Code, delW.Body.String())
	}
}
