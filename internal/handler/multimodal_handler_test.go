package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/config"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/storage"

	"github.com/gin-gonic/gin"
)

func setupMultimodalTestRouter() (*gin.Engine, *provider.Registry) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	reg := provider.NewRegistry()
	provider.RegisterDefaultProviders(reg, nil)

	pool := accounts.NewPool(nil)
	cfg := &config.Config{}
	memStore := storage.NewMemoryStorageEngine()
	rt := &GatewayRuntime{
		AccountPool:   pool,
		Config:        cfg,
		StorageEngine: memStore,
	}

	audioHandler := NewAudioHandler(pool, cfg)
	audioHandler.SetRegistry(reg)
	audioHandler.SetRuntime(rt)

	imageHandler := NewImageHandler(pool, cfg)
	imageHandler.SetRegistry(reg)

	videoHandler := NewVideoHandler(reg, nil)

	router.GET("/v1/audio/voices", audioHandler.ListVoices)
	router.GET("/v1/audio/voices/:id/preview", audioHandler.PreviewVoice)
	router.GET("/v1/audio/voice-preview", audioHandler.PreviewVoice)
	router.POST("/v1/audio/voices/pull-local", audioHandler.PullLocalVoices)
	router.POST("/v1/audio/speech", audioHandler.TTS)
	router.POST("/v1/videos/generations", videoHandler.Generations)
	router.GET("/v1/videos/tasks/:id", videoHandler.GetTask)
	router.GET("/admin/api/v1/multimodal/video-tasks", videoHandler.ListTasks)

	return router, reg
}

func TestListVoicesEndpoint(t *testing.T) {
	r, _ := setupMultimodalTestRouter()

	req, _ := http.NewRequest(http.MethodGet, "/v1/audio/voices", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Voices           []provider.VoiceDescriptor `json:"voices"`
		Total            int                        `json:"total"`
		AvailableTotal   int                        `json:"available_total"`
		CachedLocalTotal int                        `json:"cached_local_total"`
		Providers        map[string]json.RawMessage `json:"providers"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if res.Total < 10 || res.AvailableTotal < 10 {
		t.Fatalf("expected at least 10 active voices, got total=%d available=%d", res.Total, res.AvailableTotal)
	}

	foundLegacyJianying := false
	foundCosy := false
	foundEdge := false
	foundLocal := false
	foundDash := false
	for _, v := range res.Voices {
		if v.ID == "zh_female_shuangkuai" {
			foundLegacyJianying = true
		}
		if v.ID == "FunAudioLLM/CosyVoice2-0.5B:alex" {
			foundCosy = true
			if !strings.HasPrefix(v.PreviewURL, "/v1/audio/voice-preview?id=") {
				t.Fatalf("expected query-escaped preview url for slash voice ID, got %q", v.PreviewURL)
			}
		}
		if v.ID == "zh-CN-XiaoxiaoNeural" {
			foundEdge = true
			if !v.Available || v.PreviewURL == "" {
				t.Fatalf("expected Edge voice available with preview url, got %+v", v)
			}
		}
		if v.ID == "zf_xiaoxiao" {
			foundLocal = true
		}
		if v.ID == "longxiaochun" {
			foundDash = true
		}
	}

	if foundLegacyJianying {
		t.Fatalf("retired jianying voice zh_female_shuangkuai should not appear in active catalog")
	}
	if !foundCosy || !foundEdge || !foundLocal || !foundDash {
		t.Fatalf("missing expected voices: cosy=%v edge=%v local=%v dash=%v", foundCosy, foundEdge, foundLocal, foundDash)
	}
	if _, ok := res.Providers["local_tts"]; !ok {
		t.Fatalf("expected providers summary to include local_tts")
	}
}

func startMockLocalTTSServer(t *testing.T, hits *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/voices":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"voices":["zf_xiaoxiao","zm_yunxi","zf_xiaobei","am_adam"]}`))
		case "/v1/audio/speech":
			if hits != nil {
				atomic.AddInt32(hits, 1)
			}
			var req struct {
				Voice string `json:"voice"`
				Input string `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("MOCK-MP3::" + req.Voice + "::" + req.Input))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestTTSStrictNoSilentCoveFallbackAndDistinctRouting(t *testing.T) {
	r, reg := setupMultimodalTestRouter()
	var upstreamHits int32
	mockLocal := startMockLocalTTSServer(t, &upstreamHits)
	defer mockLocal.Close()

	if p, ok := reg.Get("local_tts"); ok {
		p.(*provider.LocalTTSProvider).Configure(mockLocal.URL, "", "kokoro")
	}

	postSpeech := func(voice string, allowFallback bool) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]interface{}{
			"model": "tts-1",
			"input": "测试音色独立输出",
			"voice": voice,
		})
		req, _ := http.NewRequest(http.MethodPost, "/v1/audio/speech", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if allowFallback {
			req.Header.Set("X-Allow-Fallback", "true")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// 1. 未知音色未开启降级时必须显式报错（禁止静默塌缩为 ChatGPT cove）
	wUnknown := postSpeech("unknown-voice-999", false)
	if wUnknown.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown voice without fallback, got %d: %s", wUnknown.Code, wUnknown.Body.String())
	}

	// 2. 未配置 SiliconFlow Key 时应返回 503，而不是偷偷出同一音色
	wSF := postSpeech("FunAudioLLM/CosyVoice2-0.5B:alex", false)
	if wSF.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for unconfigured SiliconFlow, got %d: %s", wSF.Code, wSF.Body.String())
	}

	// 3. 已退役剪映音色在严格模式返回 410 Gone
	wJYStrict := postSpeech("zh_female_shuangkuai", false)
	if wJYStrict.Code != http.StatusGone {
		t.Fatalf("expected 410 for retired JianYing voice, got %d: %s", wJYStrict.Code, wJYStrict.Body.String())
	}

	// 4. 已退役音色在 X-Allow-Fallback: true 时分别映射到不同本地音色并带清晰降级响应头
	wJYFemale := postSpeech("zh_female_shuangkuai", true)
	wJYMale := postSpeech("zh_male_chunhou", true)
	if wJYFemale.Code != http.StatusOK || wJYMale.Code != http.StatusOK {
		t.Fatalf("expected 200 on explicit fallback, got female=%d male=%d", wJYFemale.Code, wJYMale.Code)
	}
	if wJYFemale.Header().Get("X-TTS-Fallback") != "true" || wJYFemale.Header().Get("X-TTS-Voice") == wJYMale.Header().Get("X-TTS-Voice") {
		t.Fatalf("expected distinct fallback voices and header, got female=%q male=%q",
			wJYFemale.Header().Get("X-TTS-Voice"), wJYMale.Header().Get("X-TTS-Voice"))
	}

	// 5. OpenAI 兼容标准音色 alloy vs onyx 在无 ChatGPT 号池时自动路由到本地不同音色
	wAlloy := postSpeech("alloy", false)
	wOnyx := postSpeech("onyx", false)
	if wAlloy.Code != http.StatusOK || wOnyx.Code != http.StatusOK {
		t.Fatalf("expected 200 on OpenAI auto mapping, got alloy=%d onyx=%d", wAlloy.Code, wOnyx.Code)
	}
	if wAlloy.Header().Get("X-TTS-Provider") != "local_tts" || wAlloy.Header().Get("X-TTS-Voice") == wOnyx.Header().Get("X-TTS-Voice") {
		t.Fatalf("expected distinct local_tts OpenAI mapping, got alloy=%q onyx=%q",
			wAlloy.Header().Get("X-TTS-Voice"), wOnyx.Header().Get("X-TTS-Voice"))
	}
}

func TestPullLocalVoicesAndCachedPreview(t *testing.T) {
	r, reg := setupMultimodalTestRouter()
	var upstreamHits int32
	mockLocal := startMockLocalTTSServer(t, &upstreamHits)
	defer mockLocal.Close()

	if p, ok := reg.Get("local_tts"); ok {
		p.(*provider.LocalTTSProvider).Configure(mockLocal.URL, "", "kokoro")
	}

	pullBody, _ := json.Marshal(map[string]interface{}{
		"voices":          []string{"zf_xiaoxiao", "zm_yunxi"},
		"include_samples": true,
		"refresh_live":    false,
	})
	pullReq, _ := http.NewRequest(http.MethodPost, "/v1/audio/voices/pull-local", bytes.NewReader(pullBody))
	pullReq.Header.Set("Content-Type", "application/json")
	pullW := httptest.NewRecorder()
	r.ServeHTTP(pullW, pullReq)

	if pullW.Code != http.StatusOK {
		t.Fatalf("pull-local failed: %d %s", pullW.Code, pullW.Body.String())
	}

	var pullRes struct {
		CatalogSaved  bool `json:"catalog_saved"`
		PulledSamples int  `json:"pulled_samples"`
		CachedTotal   int  `json:"cached_total"`
		FailedSamples int  `json:"failed_samples"`
	}
	if err := json.Unmarshal(pullW.Body.Bytes(), &pullRes); err != nil {
		t.Fatalf("parse pull-local response: %v", err)
	}
	if !pullRes.CatalogSaved || pullRes.PulledSamples != 2 || pullRes.CachedTotal != 2 || pullRes.FailedSamples != 0 {
		t.Fatalf("unexpected pull-local summary: %+v", pullRes)
	}

	beforeHits := atomic.LoadInt32(&upstreamHits)
	prevReq, _ := http.NewRequest(http.MethodGet, "/v1/audio/voices/zf_xiaoxiao/preview", nil)
	prevW := httptest.NewRecorder()
	r.ServeHTTP(prevW, prevReq)

	if prevW.Code != http.StatusOK {
		t.Fatalf("preview failed: %d %s", prevW.Code, prevW.Body.String())
	}
	if prevW.Header().Get("X-Voice-Cache") != "hit" {
		t.Fatalf("expected X-Voice-Cache: hit, got %q", prevW.Header().Get("X-Voice-Cache"))
	}
	if atomic.LoadInt32(&upstreamHits) != beforeHits {
		t.Fatalf("expected cached preview to avoid upstream hit")
	}
	if !strings.Contains(prevW.Body.String(), "zf_xiaoxiao") {
		t.Fatalf("unexpected cached preview audio body: %q", prevW.Body.String())
	}
}

func TestVideoGenerationsFlow(t *testing.T) {
	r, _ := setupMultimodalTestRouter()

	// 1. 提交视频任务
	reqBody := VideoGenerationRequest{
		Model:    "jimeng-video-2.0",
		Prompt:   "金色的树叶飘落",
		Duration: 5,
		Wait:     false,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest(http.MethodPost, "/v1/videos/generations", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d: %s", w.Code, w.Body.String())
	}

	var submitRes struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &submitRes); err != nil {
		t.Fatalf("failed to parse submit response: %v", err)
	}

	if submitRes.TaskID == "" || submitRes.Status != "PENDING" {
		t.Fatalf("invalid submit response: %+v", submitRes)
	}

	// 2. 查询视频任务状态
	queryReq, _ := http.NewRequest(http.MethodGet, "/v1/videos/tasks/"+submitRes.TaskID, nil)
	queryW := httptest.NewRecorder()
	r.ServeHTTP(queryW, queryReq)

	if queryW.Code != http.StatusOK {
		t.Fatalf("expected status 200 on task query, got %d", queryW.Code)
	}

	var taskRes VideoTask
	if err := json.Unmarshal(queryW.Body.Bytes(), &taskRes); err != nil {
		t.Fatalf("failed to parse task query response: %v", err)
	}

	if taskRes.TaskID != submitRes.TaskID {
		t.Fatalf("expected task id %s, got %s", submitRes.TaskID, taskRes.TaskID)
	}

	// 3. 管理端任务列表
	listReq, _ := http.NewRequest(http.MethodGet, "/admin/api/v1/multimodal/video-tasks", nil)
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Fatalf("expected status 200 on list tasks, got %d", listW.Code)
	}
}
