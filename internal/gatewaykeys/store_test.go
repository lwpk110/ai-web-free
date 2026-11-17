package gatewaykeys

import (
	"errors"
	"testing"
	"time"
)

func newTestStore(t *testing.T, secret string) (*Store, *Group) {
	t.Helper()
	s, err := NewStore(nil, secret)
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.CreateGroup(Group{Name: "free-text", Enabled: true,
		ModelAllowlist: []string{"gpt-5-mini"}, ModelAliases: map[string]string{"gpt-4o": "gpt-5-mini"}})
	if err != nil {
		t.Fatal(err)
	}
	return s, g
}

func TestKeyLifecycleAndReveal(t *testing.T) {
	s, g := newTestStore(t, "secret")
	k, plain, err := s.CreateKey("agent-a", g.ID, 0, 0)
	if err != nil || !IsGatewayKey(plain) {
		t.Fatalf("create: %v %q", err, plain)
	}
	if got, _, err := s.Authenticate(plain); err != nil || got.ID != k.ID {
		t.Fatalf("auth failed: %v", err)
	}
	if rev, err := s.RevealKey(k.ID); err != nil || rev != plain {
		t.Fatalf("reveal: %v %q", err, rev)
	}
	if _, err := s.UpdateKey(k.ID, "", g.ID, 0, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Authenticate(plain); err == nil {
		t.Fatal("disabled key must not authenticate")
	}
	if err := s.DeleteGroup(g.ID); err == nil {
		t.Fatal("group bound by key must not be deletable")
	}
}

func TestRevealWithoutConfiguredSecret(t *testing.T) {
	s, g := newTestStore(t, "")
	k, plain, err := s.CreateKey("a", g.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rev, err := s.RevealKey(k.ID); err != nil || rev != plain {
		t.Fatalf("reveal without configured secret: %v %q", err, rev)
	}
}

func TestModelResolution(t *testing.T) {
	_, g := newTestStore(t, "")
	if m, err := g.ResolveModel("gpt-4o"); err != nil || m != "gpt-5-mini" {
		t.Fatalf("alias: %v %q", err, m)
	}
	if m, err := g.ResolveModel("gpt-5-mini"); err != nil || m != "gpt-5-mini" {
		t.Fatalf("allow: %v %q", err, m)
	}
	if _, err := g.ResolveModel("o3"); !errors.Is(err, ErrModelNotAllowed) {
		t.Fatalf("want not allowed, got %v", err)
	}
	vis := g.VisibleModels([]string{"gpt-5-mini", "o3"})
	if len(vis) != 2 || vis[0] != "gpt-5-mini" || vis[1] != "gpt-4o" {
		t.Fatalf("visible: %v", vis)
	}
}

func TestLimiter(t *testing.T) {
	l := &limiter{}
	now := time.Now()
	r1, err := l.admit(2, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.admit(2, 1, now); !errors.Is(err, ErrConcurrencyLimit) {
		t.Fatalf("want concurrency limit, got %v", err)
	}
	r1()
	if _, err := l.admit(2, 1, now); err != nil {
		t.Fatal(err)
	}
	if _, err := l.admit(2, 0, now); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want rate limit, got %v", err)
	}
	if _, err := l.admit(2, 0, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("window should reset: %v", err)
	}
}
