package provider

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-web-free/internal/accounts"
)

func TestParseGrokCardAttachmentJSON(t *testing.T) {
	// 1. Standard cardAttachment.jsonData format
	raw1 := `{"image_chunk":{"imageUrl":"users/ad6002bb-9233-4f95-bdc7-be6fc073fa8e/generated/068ec53d-2dc4-4d89-9db8-02450fc21da4/image.jpg","imageUuid":"068ec53d-2dc4-4d89-9db8-02450fc21da4"}}`
	u1 := parseGrokCardAttachmentJSON(raw1)
	if u1 != "users/ad6002bb-9233-4f95-bdc7-be6fc073fa8e/generated/068ec53d-2dc4-4d89-9db8-02450fc21da4/image.jpg" {
		t.Fatalf("expected imageUrl extracted from image_chunk, got: %s", u1)
	}

	// 2. Direct imageUrl format
	raw2 := `{"imageUrl":"users/test/image.jpg"}`
	u2 := parseGrokCardAttachmentJSON(raw2)
	if u2 != "users/test/image.jpg" {
		t.Fatalf("expected imageUrl extracted, got: %s", u2)
	}

	// 3. Fallback regex extraction
	raw3 := `{"some_nested_data": {"imageUrl":"users/nested/image.jpg"}}`
	u3 := parseGrokCardAttachmentJSON(raw3)
	if u3 != "users/nested/image.jpg" {
		t.Fatalf("expected nested imageUrl extracted, got: %s", u3)
	}
}

func TestParseGrokImageStream(t *testing.T) {
	streamData := strings.Join([]string{
		`{"result":{"response":{"token":"Thinking..."}}}`,
		`{"result":{"response":{"cardAttachment":{"jsonData":"{\"image_chunk\":{\"imageUrl\":\"users/ad6002bb/generated/1234/image.jpg\",\"imageUuid\":\"1234\"}}","type":"IMAGE"}}}}`,
		`{"result":{"response":{"token":"Image generated successfully"}}}`,
	}, "\n")

	foundURL, err := parseGrokImageStream(strings.NewReader(streamData))
	if err != nil {
		t.Fatalf("unexpected error parsing grok stream: %v", err)
	}
	expected := "users/ad6002bb/generated/1234/image.jpg"
	if foundURL != expected {
		t.Fatalf("expected %s, got %s", expected, foundURL)
	}
}

func TestResolveGrokAssetURL(t *testing.T) {
	// Relative path
	u1 := resolveGrokAssetURL("https://assets.grok.com", "users/123/img.jpg")
	if u1 != "https://assets.grok.com/users/123/img.jpg" {
		t.Fatalf("expected joined url, got: %s", u1)
	}

	// Leading slash
	u2 := resolveGrokAssetURL("https://assets.grok.com/", "/users/123/img.jpg")
	if u2 != "https://assets.grok.com/users/123/img.jpg" {
		t.Fatalf("expected trimmed slash url, got: %s", u2)
	}

	// Absolute path
	u3 := resolveGrokAssetURL("https://assets.grok.com", "https://cdn.grok.com/custom/img.jpg")
	if u3 != "https://cdn.grok.com/custom/img.jpg" {
		t.Fatalf("expected preserved absolute url, got: %s", u3)
	}
}

func TestGrokGenerateImageAssetSuccessAndFailover(t *testing.T) {
	fakeImageBytes := []byte("FAKE_JPEG_IMAGE_BINARY_DATA_FOR_GROK_TEST")
	expectedB64 := base64.StdEncoding.EncodeToString(fakeImageBytes)

	// Mock Grok Upstream Server
	account1Hit := false
	account2Hit := false
	assetServerHit := false

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/app-chat/conversations/new":
			cookie := r.Header.Get("Cookie")
			if strings.Contains(cookie, "token-bad") {
				account1Hit = true
				// Return 401 Unauthorized for expired account
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"code":16,"message":"Invalid credentials"}}`))
				return
			}
			if strings.Contains(cookie, "token-good") {
				account2Hit = true
				// Return 200 with cardAttachment stream
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				payload := `data: {"result":{"response":{"cardAttachment":{"jsonData":"{\"image_chunk\":{\"imageUrl\":\"users/test-uuid/generated/img-001/image.jpg\",\"imageUuid\":\"img-001\"}}","type":"IMAGE"}}}}`
				_, _ = w.Write([]byte(payload + "\n\n"))
				return
			}
			w.WriteHeader(http.StatusBadRequest)
		case "/users/test-uuid/generated/img-001/image.jpg":
			assetServerHit = true
			// Validate that cookie and referer were sent
			if !strings.Contains(r.Header.Get("Cookie"), "token-good") {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte("Forbidden: missing cookie"))
				return
			}
			w.Header().Set("Content-Type", "image/jpeg")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(fakeImageBytes)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	p := NewGrokProvider(nil, ts.URL, "", accounts.PoolPolicy{})
	p.SetAssetBaseURL(ts.URL)

	// Inject 2 accounts: account 1 (bad token) and account 2 (good token)
	added := p.AddAccounts([]string{
		"sso=token-bad; sso-rw=token-bad",
		"sso=token-good; sso-rw=token-good",
	}, false)
	if added != 2 {
		t.Fatalf("expected 2 accounts added, got %d", added)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := p.GenerateImageAsset(ctx, "a futuristic cyber city", "grok-2-image", "1024x1024", 1)
	if err != nil {
		t.Fatalf("GenerateImageAsset failed: %v", err)
	}

	if !account1Hit {
		t.Fatalf("expected account 1 to be attempted first")
	}
	if !account2Hit {
		t.Fatalf("expected account 2 to be hit on failover")
	}
	if !assetServerHit {
		t.Fatalf("expected image asset server to be called to download bytes")
	}

	if len(res.Images) == 0 {
		t.Fatalf("expected at least 1 image returned")
	}
	if res.Images[0].B64JSON != expectedB64 {
		t.Fatalf("expected base64 matches fakeImageBytes")
	}
	if !strings.Contains(res.Trace, "401") || !strings.Contains(res.Trace, "200:ok") {
		t.Fatalf("expected failover trace showing 401 then 200, got: %s", res.Trace)
	}
	if res.Attempts != 2 {
		t.Fatalf("expected 2 attempts, got: %d", res.Attempts)
	}

	// Verify account 1 was marked expired
	p.mu.RLock()
	var acct1 *grokAccount
	for _, a := range p.accounts {
		if a.Cookie == "token-bad" || a.Cookie == "sso=token-bad; sso-rw=token-bad" {
			acct1 = a
			break
		}
	}
	p.mu.RUnlock()

	if acct1 == nil {
		t.Fatalf("account 1 not found in pool")
	}
	if acct1.Status != accounts.StatusExpired {
		t.Fatalf("expected account 1 marked StatusExpired, got: %s", acct1.Status)
	}
}

func TestGrokGenerateImageAssetRateLimit429(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":8,"message":"Rate limited"}}`))
	}))
	defer ts.Close()

	p := NewGrokProvider(nil, ts.URL, "", accounts.PoolPolicy{})
	added := p.AddAccounts([]string{
		"sso=token-429; sso-rw=token-429",
	}, false)
	if added != 1 {
		t.Fatalf("expected 1 account added")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := p.GenerateImageAsset(ctx, "a prompt", "grok-2-image", "1024x1024", 1)
	if err == nil {
		t.Fatalf("expected error on 429 rate limit")
	}

	p.mu.RLock()
	acct := p.accounts[0]
	p.mu.RUnlock()

	if acct.Status != accounts.StatusRateLimited {
		t.Fatalf("expected account marked StatusRateLimited, got: %s", acct.Status)
	}
	if !acct.IsInCooldown(time.Now()) {
		t.Fatalf("expected account in cooldown")
	}
}
