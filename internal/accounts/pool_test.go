package accounts

import (
	"testing"
	"time"
)

func TestTieredCooldownAndCapabilityAcquire(t *testing.T) {
	p := NewPool(nil)
	a1 := CreateAccount("tok-1", TypeFree, DefaultProfiles)
	a1.ID = "acc-1"
	a1.Status = StatusActive
	a2 := CreateAccount("tok-2", TypeFree, DefaultProfiles)
	a2.ID = "acc-2"
	a2.Status = StatusActive
	p.AddAccount(a1)
	p.AddAccount(a2)

	// Case 1: Image-only cooldown on a1 leaves chat available on a1, skips a1 for images.
	p.ReportRateLimit(a1, true, 5*time.Minute, "image limit")
	if a1.Status != StatusActive {
		t.Fatalf("image cooldown should keep account active for chat, got %v", a1.Status)
	}
	if !a1.IsImageInCooldown(time.Now()) {
		t.Fatalf("expected a1 to be in image cooldown")
	}

	// Acquire image capability: should skip a1 and always pick a2 (excluding a2 -> ErrNoAvailable).
	imgAcct, err := p.AcquireForCapability(TypeFree, CapImageGenerate.Name, nil)
	if err != nil || imgAcct.ID != "acc-2" {
		t.Fatalf("expected acc-2 for image generate, got acct=%v err=%v", imgAcct, err)
	}
	if _, err := p.AcquireForCapability(TypeFree, CapImageEdit.Name, map[string]bool{"acc-2": true}); err != ErrNoAvailable {
		t.Fatalf("expected ErrNoAvailable when acc-2 excluded and acc-1 in image cooldown, got %v", err)
	}

	// Text chat capability should still be able to acquire a1 when a2 is excluded.
	chatAcct, err := p.AcquireForCapability(TypeFree, CapChat.Name, map[string]bool{"acc-2": true})
	if err != nil || chatAcct.ID != "acc-1" {
		t.Fatalf("expected acc-1 still usable for text chat, got acct=%v err=%v", chatAcct, err)
	}

	// Case 2: Full rate limit on a2 sets StatusRateLimited and skips for both chat and image.
	p.ReportRateLimit(a2, false, 5*time.Minute, "429 too many requests")
	if a2.Status != StatusRateLimited {
		t.Fatalf("expected StatusRateLimited, got %v", a2.Status)
	}
	if stats := p.Stats(); stats.RateLimited != 1 || stats.ImageLimited != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	// Case 3: RecoverCooldowns restores expired full cooldown and clears expired image cooldown.
	a1.ImageCooldownUntil = time.Now().Add(-1 * time.Second)
	a2.CooldownUntil = time.Now().Add(-1 * time.Second)
	if n := p.RecoverCooldowns(); n != 1 {
		t.Fatalf("expected 1 full cooldown recovery, got %d", n)
	}
	if a2.Status != StatusActive || !a1.ImageCooldownUntil.IsZero() {
		t.Fatalf("expected recovered status and cleared image cooldown, a2=%v img=%v", a2.Status, a1.ImageCooldownUntil)
	}
}

func TestNeedsProactiveRenewAndProxyReconcile(t *testing.T) {
	p := NewPool(nil)
	soon := CreateAccount("tok-soon", TypeFree, DefaultProfiles)
	soon.ID = "kami-soon"
	soon.Status = StatusActive
	soon.Email = "soon@example.com"
	soon.Password = "pass"
	soon.TOTPSecret = "SECRET"
	soon.ExpiresAt = time.Now().Add(6 * time.Hour)
	soon.Proxy = "http://bad-proxy:8080"

	fresh := CreateAccount("tok-fresh", TypeFree, DefaultProfiles)
	fresh.ID = "kami-fresh"
	fresh.Status = StatusActive
	fresh.Email = "fresh@example.com"
	fresh.Password = "pass"
	fresh.TOTPSecret = "SECRET"
	fresh.ExpiresAt = time.Now().Add(72 * time.Hour)
	fresh.Proxy = "http://good-proxy:8080"

	oauth := CreateAccount("tok-oauth", TypeFree, DefaultProfiles)
	oauth.ID = "oauth-1"
	oauth.Status = StatusExpired
	oauth.Password = "pass"
	oauth.TOTPSecret = "SECRET"
	oauth.RefreshToken = "rt-123"

	p.AddAccount(soon)
	p.AddAccount(fresh)
	p.AddAccount(oauth)

	due := p.NeedsProactiveRenew(24 * time.Hour)
	if len(due) != 1 || due[0].ID != "kami-soon" {
		t.Fatalf("expected only kami-soon due for proactive renew, got %+v", due)
	}

	counts := p.BoundProxyCounts()
	if counts["http://bad-proxy:8080"] != 1 || counts["http://good-proxy:8080"] != 1 {
		t.Fatalf("unexpected bound proxy counts: %+v", counts)
	}

	migrated := p.ReconcileProxies(
		func(u string) bool { return u == "http://bad-proxy:8080" },
		func(exclude map[string]bool) string { return "http://good-proxy:8080" },
	)
	if migrated != 1 || soon.Proxy != "http://good-proxy:8080" {
		t.Fatalf("expected 1 migrated to good-proxy, got migrated=%d proxy=%q", migrated, soon.Proxy)
	}
}

func TestRebindAccountProxyKeepsOldBindingWhenClientRebuildFails(t *testing.T) {
	p := NewPool(nil)
	acct := CreateAccount("token", TypeFree, DefaultProfiles)
	acct.Proxy = "http://127.0.0.1:8080"

	if err := p.RebindAccountProxy(acct, "://invalid-proxy"); err == nil {
		t.Fatal("expected invalid proxy to fail client rebuild")
	}
	if acct.Proxy != "http://127.0.0.1:8080" {
		t.Fatalf("failed rebind changed the existing proxy: %q", acct.Proxy)
	}
}

func TestReconcileProxiesKeepsBindingWhenNoHealthyAlternativeExists(t *testing.T) {
	p := NewPool(nil)
	acct := CreateAccount("token", TypeFree, DefaultProfiles)
	acct.Proxy = "http://bad-proxy:8080"
	p.AddAccount(acct)

	migrated := p.ReconcileProxies(
		func(u string) bool { return u == acct.Proxy },
		func(exclude map[string]bool) string { return "" },
	)
	if migrated != 0 || acct.Proxy != "http://bad-proxy:8080" {
		t.Fatalf("expected existing proxy binding to be preserved without a healthy alternative, migrated=%d proxy=%q", migrated, acct.Proxy)
	}
}
