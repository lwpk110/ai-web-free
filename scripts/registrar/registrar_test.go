package registrar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMockMailProvider(t *testing.T) {
	ctx := context.Background()
	p := NewMockMailProvider("123456")

	email, cleanup, err := p.CreateMailbox(ctx)
	if err != nil {
		t.Fatalf("create mailbox failed: %v", err)
	}
	defer cleanup()

	if email == "" {
		t.Fatalf("email should not be empty")
	}

	code, err := p.FetchVerificationCode(ctx, email, 5*time.Second)
	if err != nil {
		t.Fatalf("fetch code failed: %v", err)
	}
	if code != "123456" {
		t.Fatalf("expected code 123456, got %s", code)
	}
}

func TestMockCaptchaSolver(t *testing.T) {
	ctx := context.Background()
	s := NewMockCaptchaSolver("test_token")
	token, err := s.SolveTurnstile(ctx, "sitekey123", "https://example.com")
	if err != nil {
		t.Fatalf("solve turnstile failed: %v", err)
	}
	if token != "test_token" {
		t.Fatalf("expected test_token, got %s", token)
	}
}

func TestGrokRegistrarRegistrationFlow(t *testing.T) {
	ctx := context.Background()
	r := NewGrokRegistrar(RegistrarConfig{
		MailService: NewMockMailProvider("998877"),
		Solver:      NewMockCaptchaSolver("tok_abc123"),
	})

	res, err := r.RegisterOne(ctx)
	if err != nil {
		t.Fatalf("register one failed: %v", err)
	}
	if res.Email == "" || res.Password == "" || res.Cookie == "" {
		t.Fatalf("incomplete registration result: %+v", res)
	}
	if res.Provider != "grok" {
		t.Fatalf("expected provider grok, got %s", res.Provider)
	}
}

func TestPushToGatewayMock(t *testing.T) {
	received := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/admin/api/v1/providers/grok/accounts" {
			received = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"added": 1}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	err := PushToGateway(ctx, server.URL, "test-admin-token", &RegistrationResult{
		Provider: "grok",
		Email:    "test@example.com",
		Password: "password123",
		Cookie:   "sso=tok1; sso-rw=tok1",
	})
	if err != nil {
		t.Fatalf("push to gateway failed: %v", err)
	}
	if !received {
		t.Fatalf("gateway server did not receive import request")
	}
}
