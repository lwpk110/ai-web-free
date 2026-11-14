package provider

import (
	"sync/atomic"
	"testing"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/audit"
)

func TestKamiRenewWorkerSerialAndBackoff(t *testing.T) {
	pool := accounts.NewPool(nil)
	auditLog := audit.NewRingBuffer(50, "", 0)

	acct1 := accounts.CreateAccount("", accounts.TypeFree, accounts.DefaultProfiles)
	acct1.ID = "k-1"
	acct1.Email = "one@example.com"
	acct1.Password = "p1"
	acct1.TOTPSecret = "SECRET1"
	acct1.Status = accounts.StatusExpired

	acct2 := accounts.CreateAccount("", accounts.TypeFree, accounts.DefaultProfiles)
	acct2.ID = "k-2"
	acct2.Email = "two@example.com"
	acct2.Password = "p2"
	acct2.TOTPSecret = "SECRET2"
	acct2.Status = accounts.StatusExpired

	pool.AddAccount(acct1)
	pool.AddAccount(acct2)

	var persisted atomic.Int32
	var inFlight atomic.Int32
	var maxConcurrent atomic.Int32

	w := NewKamiRenewWorker(pool, "", true, func() error {
		persisted.Add(1)
		return nil
	}, auditLog)
	w.backoff = []time.Duration{50 * time.Millisecond, 200 * time.Millisecond, 500 * time.Millisecond}

	w.SetLoginExecutor(func(acct *accounts.Account, _ string, _ bool) KamiLoginOutcome {
		cur := inFlight.Add(1)
		if cur > maxConcurrent.Load() {
			maxConcurrent.Store(cur)
		}
		time.Sleep(15 * time.Millisecond)
		inFlight.Add(-1)

		if acct.ID == "k-1" {
			return KamiLoginOutcome{
				Email:            acct.Email,
				OK:               true,
				AccessToken:      "new-access-token-1",
				ChatGPTAccountID: "acct-123",
				ExpiresAt:        time.Now().Add(9 * 24 * time.Hour).Unix(),
			}
		}
		return KamiLoginOutcome{
			Email: acct.Email,
			OK:    false,
			Error: "2FA timeout",
		}
	})

	w.Start()
	defer w.Stop()

	// Duplicate enqueue on k-1 must be ignored.
	if !w.Enqueue(acct1, "test") {
		t.Fatalf("expected enqueue acct1 to succeed")
	}
	if w.Enqueue(acct1, "dup") {
		t.Fatalf("expected duplicate enqueue acct1 to be deduplicated")
	}
	if !w.Enqueue(acct2, "test") {
		t.Fatalf("expected enqueue acct2 to succeed")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st := w.Stats()
		if !st.Running && st.QueueLength == 0 && st.TotalSucceeded+st.TotalFailed == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if maxConcurrent.Load() != 1 {
		t.Fatalf("expected strictly serial execution (max concurrency 1), got %d", maxConcurrent.Load())
	}
	if acct1.Status != accounts.StatusActive || acct1.Token != "new-access-token-1" || persisted.Load() != 1 {
		t.Fatalf("unexpected acct1 after renew: status=%v tok=%q persisted=%d", acct1.Status, acct1.Token, persisted.Load())
	}
	if acct2.RenewState != "cooldown" || acct2.RenewFailures != 1 || acct2.RenewCooldownUntil.IsZero() {
		t.Fatalf("expected acct2 in failure backoff cooldown, got state=%q failures=%d until=%v", acct2.RenewState, acct2.RenewFailures, acct2.RenewCooldownUntil)
	}
	// Backoff must block immediate re-enqueue while inside cooldown.
	if w.Enqueue(acct2, "too_soon") {
		t.Fatalf("expected enqueue during backoff cooldown to be rejected")
	}

	entries, _ := auditLog.Query("all", "", 10)
	if len(entries) != 2 {
		t.Fatalf("expected 2 audit events for renew success+failure, got %d", len(entries))
	}
}

func TestKamiRenewWorkerCancelQueuedAndPhaseSync(t *testing.T) {
	pool := accounts.NewPool(nil)
	acct1 := accounts.CreateAccount("", accounts.TypeFree, accounts.DefaultProfiles)
	acct1.ID = "kw-1"
	acct1.Email = "kw1@example.com"
	acct1.Password = "p1"
	acct1.TOTPSecret = "SECRET1"
	acct1.Status = accounts.StatusExpired

	acct2 := accounts.CreateAccount("", accounts.TypeFree, accounts.DefaultProfiles)
	acct2.ID = "kw-2"
	acct2.Email = "kw2@example.com"
	acct2.Password = "p2"
	acct2.TOTPSecret = "SECRET2"
	acct2.Status = accounts.StatusExpired

	pool.AddAccount(acct1)
	pool.AddAccount(acct2)

	started := make(chan struct{})
	release := make(chan struct{})
	w := NewKamiRenewWorker(pool, "", true, nil, nil)
	w.SetLoginExecutor(func(acct *accounts.Account, _ string, _ bool) KamiLoginOutcome {
		close(started)
		<-release
		return KamiLoginOutcome{Email: acct.Email, OK: true, AccessToken: "kw-tok-1"}
	})
	w.Start()
	defer w.Stop()

	if !w.Enqueue(acct1, "test") || !w.Enqueue(acct2, "test") {
		t.Fatalf("expected both accounts to enqueue")
	}
	<-started

	// kw-1 should expose running action state, kw-2 is queued and cancellable.
	st := w.Stats()
	if !st.Running || st.RunningAccountID != acct1.ID || st.QueueLength != 1 {
		t.Fatalf("unexpected worker stats while running: %+v", st)
	}
	if !w.CancelQueued(acct2.ID) {
		t.Fatalf("expected CancelQueued(kw-2) to return true")
	}
	if w.Stats().QueueLength != 0 {
		t.Fatalf("expected queue length 0 after cancel")
	}
	snap2 := pool.ListSnapshots("", "", "kw2@example.com")
	if len(snap2) != 1 || snap2[0].ActiveAction != "" || snap2[0].RenewState != "idle" {
		t.Fatalf("expected kw-2 state cleared on cancel, got %+v", snap2)
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !w.Stats().Running && w.Stats().TotalSucceeded == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if w.Stats().TotalSucceeded != 1 || acct1.LastActionSummary != "后台自动续期成功，Token 已更新" {
		t.Fatalf("expected acct1 completed renew action summary, stats=%+v sum=%q", w.Stats(), acct1.LastActionSummary)
	}
}
