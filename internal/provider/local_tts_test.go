package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-web-free/internal/config"
	"ai-web-free/internal/storage"
)

func TestLocalTTSProviderLifecycleAndSynthesize(t *testing.T) {
	var seenModel, seenVoice, seenAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/v1/audio/voices":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"voices":["zf_xiaoxiao","zm_yunxi","custom_local_voice"]}`))
		case "/v1/audio/speech":
			var req struct {
				Model string  `json:"model"`
				Input string  `json:"input"`
				Voice string  `json:"voice"`
				Speed float64 `json:"speed"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			seenModel = req.Model
			seenVoice = req.Voice
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("ID3mock-mp3-" + req.Voice + "-" + req.Input))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewLocalTTSProvider("", "", "")
	if p.IsConfigured() || p.IsAvailable() {
		t.Fatalf("expected unconfigured provider to report unavailable")
	}

	p.Configure(srv.URL, "local-secret", "kokoro-v1")
	if !p.IsConfigured() || !p.IsAvailable() {
		t.Fatalf("expected configured provider to report ready")
	}

	if err := p.Probe(context.Background()); err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if seenAuth != "Bearer local-secret" {
		t.Fatalf("expected Authorization header forwarded, got %q", seenAuth)
	}

	descs := p.VoiceDescriptors()
	if len(descs) != 3 {
		t.Fatalf("expected 3 discovered voices, got %d", len(descs))
	}
	if !descs[0].Available {
		t.Fatalf("expected discovered voices to be available")
	}
	if !p.IsKnownVoice("custom_local_voice") || !p.IsKnownVoice("zf_xiaoxiao") || !p.IsKnownVoice("kokoro:af_heart") {
		t.Fatalf("expected IsKnownVoice to match discovered and kokoro voices")
	}

	audio, err := p.Synthesize(context.Background(), "测试本地声音。", "kokoro:zm_yunxi", "tts-1", 1.1)
	if err != nil {
		t.Fatalf("synthesize failed: %v", err)
	}
	if seenModel != "kokoro-v1" || seenVoice != "zm_yunxi" {
		t.Fatalf("unexpected upstream request model=%q voice=%q", seenModel, seenVoice)
	}
	if !strings.Contains(string(audio), "zm_yunxi") {
		t.Fatalf("unexpected synthesized payload: %q", string(audio))
	}
}

func TestVoiceCatalogStoreLocalAndMemory(t *testing.T) {
	// 1. 内存存储验证（模拟只读容器降级）
	memStore := NewVoiceCatalogStore(storage.NewMemoryStorageEngine())
	if !memStore.Enabled() {
		t.Fatalf("expected memory store enabled")
	}
	if _, err := memStore.SaveSample(context.Background(), "zh-CN-XiaoxiaoNeural", []byte("mp3-mem")); err != nil {
		t.Fatalf("save memory sample failed: %v", err)
	}
	if !memStore.HasSample(context.Background(), "zh-CN-XiaoxiaoNeural") {
		t.Fatalf("expected HasSample true in memory")
	}

	// 2. 本地磁盘存储验证
	tmpDir := t.TempDir()
	localEng, err := storage.NewLocalStorageEngine(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalStorageEngine failed: %v", err)
	}
	diskStore := NewVoiceCatalogStore(localEng)

	voices := []VoiceDescriptor{
		{ID: "zh-CN-XiaoxiaoNeural", Name: "晓晓", Provider: "edge_tts", Available: true},
		{ID: "FunAudioLLM/CosyVoice2-0.5B:alex", Name: "Alex", Provider: "siliconflow", Available: false},
	}
	if err := diskStore.SaveCatalog(context.Background(), voices); err != nil {
		t.Fatalf("SaveCatalog failed: %v", err)
	}
	loaded, updatedAt, err := diskStore.LoadCatalog(context.Background())
	if err != nil || updatedAt.IsZero() || len(loaded) != 2 {
		t.Fatalf("LoadCatalog mismatch: len=%d updatedAt=%v err=%v", len(loaded), updatedAt, err)
	}

	relPath, err := diskStore.SaveSample(context.Background(), "FunAudioLLM/CosyVoice2-0.5B:alex", []byte("FAKE_MP3_BYTES"))
	if err != nil {
		t.Fatalf("SaveSample failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, relPath)); err != nil {
		t.Fatalf("expected sample file on disk at %s: %v", relPath, err)
	}

	annotated, cachedCount := diskStore.AnnotateDescriptors(context.Background(), loaded)
	if cachedCount != 1 {
		t.Fatalf("expected 1 cached voice, got %d", cachedCount)
	}
	if annotated[0].CachedLocal || !annotated[1].CachedLocal {
		t.Fatalf("unexpected cached flags: %+v", annotated)
	}
	if annotated[1].PreviewURL == "" {
		t.Fatalf("expected preview URL populated")
	}
}

func TestApplyConfigWiresTTSProviders(t *testing.T) {
	reg := NewRegistry()
	RegisterDefaultProviders(reg, nil)

	cfg := &config.Config{
		SiliconFlowAPIKey: "sf-key-1, sf-key-2",
		DashScopeAPIKey:   "ds-key-1",
		LocalTTSBaseURL:   "http://127.0.0.1:8880",
		LocalTTSAPIKey:    "local-token",
		LocalTTSModel:     "kokoro",
	}
	ApplyConfig(reg, cfg)

	sf, _ := reg.Get("siliconflow")
	if sf.Stats().Active != 2 {
		t.Fatalf("expected 2 active SiliconFlow keys, got %d", sf.Stats().Active)
	}
	ds, _ := reg.Get("dashscope")
	if ds.Stats().Active != 1 {
		t.Fatalf("expected 1 active DashScope key, got %d", ds.Stats().Active)
	}
	localP, _ := reg.Get("local_tts")
	if localP.Stats().Active != 1 {
		t.Fatalf("expected active LocalTTS provider, got %+v", localP.Stats())
	}
}
