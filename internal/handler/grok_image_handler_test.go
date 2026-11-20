package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/audit"
	"ai-web-free/internal/config"
	"ai-web-free/internal/provider"
	officialtypes "ai-web-free/typings/official"

	"github.com/gin-gonic/gin"
)

func TestGrokImageGenerationsHandlerSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	fakeImageBytes := []byte("FAKE_GROK_JPEG_DATA")
	expectedB64 := base64.StdEncoding.EncodeToString(fakeImageBytes)

	// Mock Grok Upstream Server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/app-chat/conversations/new":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			payload := `data: {"result":{"response":{"cardAttachment":{"jsonData":"{\"image_chunk\":{\"imageUrl\":\"users/test/generated/img-001/image.jpg\",\"imageUuid\":\"img-001\"}}","type":"IMAGE"}}}}`
			_, _ = w.Write([]byte(payload + "\n\n"))
		case "/users/test/generated/img-001/image.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(fakeImageBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	// Setup Grok Provider
	grokProv := provider.NewGrokProvider(nil, ts.URL, "", accounts.PoolPolicy{})
	grokProv.SetAssetBaseURL(ts.URL)
	grokProv.AddAccounts([]string{"sso=grok-token-ok; sso-rw=grok-token-ok"}, false)

	reg := provider.NewRegistry()
	reg.Register(grokProv)

	// Setup ImageHandler with audit log
	auditLog := audit.NewRingBuffer(100, "", 0)
	rt := &GatewayRuntime{
		AuditLog: auditLog,
		Config:   &config.Config{},
	}

	h := NewImageHandler(nil, &config.Config{})
	h.SetRegistry(reg)
	h.SetRuntime(rt)

	r := gin.New()
	r.POST("/v1/images/generations", h.Generations)

	// 1. Test standard JSON response (b64_json format)
	reqBody := []byte(`{"prompt":"a red panda astronaut","model":"grok-2-image","response_format":"b64_json"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("X-Image-Provider") != "grok" {
		t.Fatalf("expected X-Image-Provider: grok, got: %s", w.Header().Get("X-Image-Provider"))
	}

	var resp officialtypes.ImageGenerationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if len(resp.Data) == 0 {
		t.Fatalf("expected non-empty image data")
	}
	if resp.Data[0].B64JSON != expectedB64 {
		t.Fatalf("expected b64_json to match encoded bytes")
	}

	// Verify AuditLog was recorded with Grok model
	entries, _ := auditLog.Query("all", "", 10)
	if len(entries) == 0 {
		t.Fatalf("expected audit log entry recorded")
	}
	lastEntry := entries[0]
	if lastEntry.Model != "grok-2-image" {
		t.Fatalf("expected audit log Model=grok-2-image, got: %s", lastEntry.Model)
	}
	if lastEntry.Status != 200 {
		t.Fatalf("expected audit log Status=200, got: %d", lastEntry.Status)
	}
	if !strings.Contains(lastEntry.AccountID, "grok-") {
		t.Fatalf("expected audit log AccountID containing grok-, got: %s", lastEntry.AccountID)
	}
}

func TestGrokImageGenerationsStreamHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	fakeImageBytes := []byte("STREAM_GROK_BYTES")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/app-chat/conversations/new":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			payload := `data: {"result":{"response":{"cardAttachment":{"jsonData":"{\"image_chunk\":{\"imageUrl\":\"users/test/image.jpg\"}}","type":"IMAGE"}}}}`
			_, _ = w.Write([]byte(payload + "\n\n"))
		case "/users/test/image.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(fakeImageBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	grokProv := provider.NewGrokProvider(nil, ts.URL, "", accounts.PoolPolicy{})
	grokProv.SetAssetBaseURL(ts.URL)
	grokProv.AddAccounts([]string{"sso=grok-stream-tok; sso-rw=grok-stream-tok"}, false)

	reg := provider.NewRegistry()
	reg.Register(grokProv)

	h := NewImageHandler(nil, &config.Config{})
	h.SetRegistry(reg)
	h.SetRuntime(&GatewayRuntime{Config: &config.Config{}})

	r := gin.New()
	r.POST("/v1/images/generations", h.Generations)

	reqBody := []byte(`{"prompt":"a starry night sky","model":"grok-2-image","stream":true}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for stream, got %d", w.Code)
	}
	bodyStr := w.Body.String()
	if !strings.Contains(bodyStr, "image.generation.result") {
		t.Fatalf("expected stream to contain image.generation.result event")
	}
	if !strings.Contains(bodyStr, "image.generation.completed") {
		t.Fatalf("expected stream to contain image.generation.completed event")
	}
	if !strings.Contains(bodyStr, "data: [DONE]") {
		t.Fatalf("expected stream to end with [DONE]")
	}
}

func TestGrokImageGenerationsFailureNeverFallsThroughToChatGPT(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Mock Grok upstream that fails with 500
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"Internal upstream failure"}}`))
	}))
	defer ts.Close()

	grokProv := provider.NewGrokProvider(nil, ts.URL, "", accounts.PoolPolicy{})
	grokProv.AddAccounts([]string{"sso=tok-fail; sso-rw=tok-fail"}, false)

	reg := provider.NewRegistry()
	reg.Register(grokProv)

	// We pass a nil/empty ChatGPT pool so if it ever fell through to ChatGPT pool, it would panic or error on ChatGPT
	auditLog := audit.NewRingBuffer(100, "", 0)
	rt := &GatewayRuntime{
		AuditLog: auditLog,
		Config:   &config.Config{},
	}

	h := NewImageHandler(nil, &config.Config{})
	h.SetRegistry(reg)
	h.SetRuntime(rt)

	r := gin.New()
	r.POST("/v1/images/generations", h.Generations)

	reqBody := []byte(`{"prompt":"test grok failure","model":"grok-2-image"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	// MUST be 500/502 error from Grok, NOT a ChatGPT prepare sentinel EOF
	if w.Code == http.StatusOK {
		t.Fatalf("expected error code, got 200")
	}
	if strings.Contains(w.Body.String(), "chatgpt.com") || strings.Contains(w.Body.String(), "sentinel") {
		t.Fatalf("violation: Grok failure fell through to ChatGPT: %s", w.Body.String())
	}

	entries, _ := auditLog.Query("all", "", 10)
	if len(entries) > 0 {
		lastEntry := entries[0]
		if strings.Contains(lastEntry.Error, "chatgpt.com") {
			t.Fatalf("violation: audit log shows ChatGPT touched: %s", lastEntry.Error)
		}
	}
}

func TestGrokImageEditsReturnsBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewImageHandler(nil, &config.Config{})
	r := gin.New()
	r.POST("/v1/images/edits", h.Edits)

	reqBody := []byte(`{"prompt":"make it blue","model":"grok-2-image"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for Grok edits, got: %d (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Grok does not support image editing") {
		t.Fatalf("expected rejection message, got: %s", w.Body.String())
	}
}
