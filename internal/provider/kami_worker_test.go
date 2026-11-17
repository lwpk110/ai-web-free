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

func TestKamiRenewWorkerCancelAllQueued(t *testing.T) {
	pool := accounts.NewPool(nil)
	makeAcct := func(id, email string) *accounts.Account {
		a := accounts.CreateAccount("", accounts.TypeFree, accounts.DefaultProfiles)
		a.ID = id
		a.Email = email
		a.Password = "p"
		a.TOTPSecret = "SECRET"
		a.Status = accounts.StatusExpired
		pool.AddAccount(a)
		return a
	}

	a1 := makeAcct("kw-all-1", "kwall1@example.com")
	a2 := makeAcct("kw-all-2", "kwall2@example.com")
	a3 := makeAcct("kw-all-3", "kwall3@example.com")

	started := make(chan struct{})
	release := make(chan struct{})
	w := NewKamiRenewWorker(pool, "", true, nil, nil)
	w.SetLoginExecutor(func(acct *accounts.Account, _ string, _ bool) KamiLoginOutcome {
		close(started)
		<-release
		return KamiLoginOutcome{Email: acct.Email, OK: true, AccessToken: "kw-tok-all"}
	})
	w.Start()
	defer w.Stop()

	if !w.Enqueue(a1, "test") || !w.Enqueue(a2, "test") || !w.Enqueue(a3, "test") {
		t.Fatalf("expected all 3 accounts to enqueue")
	}
	<-started

	if st := w.Stats(); st.QueueLength != 2 {
		t.Fatalf("expected queue length 2 while kw-all-1 running, got %+v", st)
	}
	if n := w.CancelAllQueued(); n != 2 {
		t.Fatalf("expected CancelAllQueued to cancel 2 accounts, got %d", n)
	}
	if st := w.Stats(); st.QueueLength != 0 {
		t.Fatalf("expected queue length 0 after CancelAllQueued, got %+v", st)
	}
	for _, email := range []string{"kwall2@example.com", "kwall3@example.com"} {
		snap := pool.ListSnapshots("", "", email)
		if len(snap) != 1 || snap[0].ActiveAction != "" || snap[0].RenewState != "idle" {
			t.Fatalf("expected %s state cleared after CancelAllQueued, got %+v", email, snap)
		}
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !w.Stats().Running && w.Stats().TotalSucceeded == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if w.Stats().TotalSucceeded != 1 {
		t.Fatalf("expected only running account to complete, got %+v", w.Stats())
	}
}

func TestKamiRenewWorkerAccountDeactivatedBanned(t *testing.T) {
	pool := accounts.NewPool(nil)
	auditLog := audit.NewRingBuffer(50, "", 0)

	acct := accounts.CreateAccount("", accounts.TypeFree, accounts.DefaultProfiles)
	acct.ID = "kw-deact-1"
	acct.Email = "dead@example.com"
	acct.Password = "p"
	acct.TOTPSecret = "SECRET"
	acct.Status = accounts.StatusExpired
	pool.AddAccount(acct)

	w := NewKamiRenewWorker(pool, "", true, nil, auditLog)
	w.SetLoginExecutor(func(a *accounts.Account, _ string, _ bool) KamiLoginOutcome {
		return KamiLoginOutcome{
			Email: a.Email,
			OK:    false,
			Error: "account_deactivated: 该账户已被删除或停用 (error_code: account_deactivated)",
		}
	})
	w.Start()
	defer w.Stop()

	if !w.Enqueue(acct, "expired_test") {
		t.Fatalf("expected initial enqueue to succeed")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st := w.Stats()
		if !st.Running && st.QueueLength == 0 && st.TotalFailed == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if acct.Status != accounts.StatusBanned {
		t.Fatalf("expected account status to become StatusBanned, got %v", acct.Status)
	}
	if acct.RenewState != "" || !acct.RenewCooldownUntil.IsZero() {
		t.Fatalf("expected renew backoff state cleared for banned account, got state=%q until=%v", acct.RenewState, acct.RenewCooldownUntil)
	}

	// Banned account must NEVER be allowed into renew worker again
	if w.Enqueue(acct, "retry_banned") {
		t.Fatalf("expected banned account to be rejected from renew queue")
	}

	entries, _ := auditLog.Query("all", "", 10)
	foundBannedLog := false
	for _, entry := range entries {
		if entry.Note == "kami_renew_banned" {
			foundBannedLog = true
			break
		}
	}
	if !foundBannedLog {
		t.Fatalf("expected kami_renew_banned audit event recorded")
	}
}

