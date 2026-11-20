package accounts

import (
	"fmt"
	"testing"
	"time"
)

func newFreeAccounts(n int) []*Account {
	out := make([]*Account, 0, n)
	for i := 0; i < n; i++ {
		a := NewAccount(fmt.Sprintf("acc-%02d", i), TypeFree, fmt.Sprintf("token-%02d", i))
		a.Status = StatusActive
		a.RefreshToken = "rt"
		out = append(out, a)
	}
	return out
}

func countStatus(p *Pool, s AccountStatus) int {
	n := 0
	for _, a := range p.free {
		if a.Status == s {
			n++
		}
	}
	return n
}

func TestRebalanceShrinksToTarget(t *testing.T) {
	p := NewPool(newFreeAccounts(20))
	p.SetPolicy(TypeFree, PoolPolicy{ActiveTarget: 5, MinHealthy: 2})
	p.Rebalance()
	if got := countStatus(p, StatusActive); got != 5 {
		t.Fatalf("active=%d want 5", got)
	}
	if got := countStatus(p, StatusStandby); got != 15 {
		t.Fatalf("standby=%d want 15", got)
	}
	// Acquire 只应命中热池账号
	for i := 0; i < 30; i++ {
		a, err := p.Acquire(TypeFree)
		if err != nil || a.Status != StatusActive {
			t.Fatalf("acquire got %v err=%v", a, err)
		}
	}
	if st := p.Stats(); st.Standby != 15 {
		t.Fatalf("stats standby=%d", st.Standby)
	}
}

func TestRebalanceBackfillsOnBan(t *testing.T) {
	p := NewPool(newFreeAccounts(8))
	p.SetPolicy(TypeFree, PoolPolicy{ActiveTarget: 3, MinHealthy: 2, ActivateBatch: 5})
	p.Rebalance()
	for _, a := range p.free {
		if a.Status == StatusActive {
			a.Status = StatusBanned
		}
	}
	res := p.Rebalance()
	if len(res) != 1 || res[0].Activated != 3 {
		t.Fatalf("unexpected result %+v", res)
	}
	if got := countStatus(p, StatusActive); got != 3 {
		t.Fatalf("active=%d want 3", got)
	}
}

func TestRebalanceActivateBatchAndMaxActive(t *testing.T) {
	accs := newFreeAccounts(10)
	for _, a := range accs {
		a.Status = StatusStandby
	}
	p := NewPool(accs)
	p.SetPolicy(TypeFree, PoolPolicy{ActiveTarget: 8, MinHealthy: 1, MaxActive: 4, ActivateBatch: 3})
	p.Rebalance()
	if got := countStatus(p, StatusActive); got != 3 {
		t.Fatalf("batch limited active=%d want 3", got)
	}
	p.Rebalance()
	if got := countStatus(p, StatusActive); got != 4 {
		t.Fatalf("max active limited active=%d want 4", got)
	}
}

func TestRebalanceIgnoresPendingAccountsWithoutTokens(t *testing.T) {
	accountsList := make([]*Account, 0, 106)
	for i := 0; i < 81; i++ {
		account := NewAccount(fmt.Sprintf("pending-%02d", i), TypeFree, "")
		account.Status = StatusPending
		account.Password = "password"
		account.TOTPSecret = "totp"
		accountsList = append(accountsList, account)
	}
	for i := 0; i < 25; i++ {
		account := NewAccount(fmt.Sprintf("standby-%02d", i), TypeFree, fmt.Sprintf("token-%02d", i))
		account.Status = StatusStandby
		account.ExpiresAt = time.Now().Add(24 * time.Hour)
		accountsList = append(accountsList, account)
	}

	pool := NewPool(accountsList)
	pool.SetPolicy(TypeFree, PoolPolicy{
		ActiveTarget:  15,
		MinHealthy:    8,
		MaxActive:     15,
		ActivateBatch: 15,
	})

	results := pool.Rebalance()
	if len(results) != 1 || results[0].Activated != 15 {
		t.Fatalf("unexpected rebalance result: %+v", results)
	}
	if got := countStatus(pool, StatusActive); got != 15 {
		t.Fatalf("active=%d want 15", got)
	}
	if got := countStatus(pool, StatusPending); got != 81 {
		t.Fatalf("pending=%d want 81", got)
	}
	if got := countStatus(pool, StatusStandby); got != 10 {
		t.Fatalf("standby=%d want 10", got)
	}
}

func TestRebalanceRotation(t *testing.T) {
	p := NewPool(newFreeAccounts(10))
	p.SetPolicy(TypeFree, PoolPolicy{ActiveTarget: 5, MinHealthy: 2, RotateInterval: time.Hour, RotateRatio: 0.4})
	p.Rebalance() // 建立基线并收缩
	before := map[string]bool{}
	for _, a := range p.free {
		if a.Status == StatusActive {
			before[a.ID] = true
		}
	}
	p.lastRotate[TypeFree] = time.Now().Add(-2 * time.Hour)
	res := p.Rebalance()
	if res[0].Rotated != 2 || res[0].Activated != 2 {
		t.Fatalf("rotation result %+v", res[0])
	}
	changed := 0
	for _, a := range p.free {
		if a.Status == StatusActive && !before[a.ID] {
			changed++
		}
	}
	if changed != 2 || countStatus(p, StatusActive) != 5 {
		t.Fatalf("changed=%d active=%d", changed, countStatus(p, StatusActive))
	}
}

func TestRebalanceDisabledPolicyKeepsLegacyBehavior(t *testing.T) {
	p := NewPool(newFreeAccounts(6))
	p.SetPolicy(TypeFree, PoolPolicy{})
	if res := p.Rebalance(); len(res) != 0 {
		t.Fatalf("expected no-op, got %+v", res)
	}
	if got := countStatus(p, StatusActive); got != 6 {
		t.Fatalf("active=%d", got)
	}
}

func TestStandbySkippedByProactiveRenew(t *testing.T) {
	a := NewAccount("k1", TypeFree, "")
	a.Password, a.TOTPSecret = "pw", "SECRET"
	a.Status = StatusStandby
	p := NewPool([]*Account{a})
	if got := p.NeedsProactiveRenew(time.Hour); len(got) != 0 {
		t.Fatalf("standby account must not be renewed: %v", got)
	}
}

func TestRebalanceShrinksExpiredAccountsFirst(t *testing.T) {
	accs := make([]*Account, 0, 10)
	for i := 0; i < 5; i++ {
		a := NewAccount(fmt.Sprintf("act-%d", i), TypeFree, fmt.Sprintf("token-%d", i))
		a.Status = StatusActive
		a.ExpiresAt = time.Now().Add(24 * time.Hour)
		accs = append(accs, a)
	}
	for i := 0; i < 5; i++ {
		a := NewAccount(fmt.Sprintf("exp-%d", i), TypeFree, "")
		a.Status = StatusExpired
		a.Password = "pw"
		a.TOTPSecret = "SECRET"
		accs = append(accs, a)
	}
	p := NewPool(accs)
	// target=5: working is 10 (5 active + 5 expired with credentials). Expired should be demoted first.
	p.SetPolicy(TypeFree, PoolPolicy{ActiveTarget: 5, MinHealthy: 2})
	res := p.Rebalance()
	if len(res) != 1 || res[0].Demoted != 5 {
		t.Fatalf("expected 5 demoted, got %+v", res)
	}
	if got := countStatus(p, StatusActive); got != 5 {
		t.Fatalf("expected 5 active accounts kept, got %d", got)
	}
	if got := countStatus(p, StatusStandby); got != 5 {
		t.Fatalf("expected 5 standby accounts, got %d", got)
	}
	// Verify that the 5 demoted to standby were the expired ones
	for _, a := range p.free {
		if a.Status == StatusActive && a.Token == "" {
			t.Fatalf("active account should have token: %+v", a)
		}
	}
}

func TestNeedsProactiveRenewRespectsPoolPolicy(t *testing.T) {
	accs := make([]*Account, 0, 30)
	// 15 healthy active accounts
	for i := 0; i < 15; i++ {
		a := NewAccount(fmt.Sprintf("act-%02d", i), TypeFree, fmt.Sprintf("tok-%02d", i))
		a.Status = StatusActive
		a.Password = "pw"
		a.TOTPSecret = "SECRET"
		a.ExpiresAt = time.Now().Add(48 * time.Hour)
		accs = append(accs, a)
	}
	// 10 pending accounts without token
	for i := 0; i < 10; i++ {
		a := NewAccount(fmt.Sprintf("pend-%02d", i), TypeFree, "")
		a.Status = StatusPending
		a.Password = "pw"
		a.TOTPSecret = "SECRET"
		accs = append(accs, a)
	}
	// 5 expired accounts
	for i := 0; i < 5; i++ {
		a := NewAccount(fmt.Sprintf("exp-%02d", i), TypeFree, "")
		a.Status = StatusExpired
		a.Password = "pw"
		a.TOTPSecret = "SECRET"
		accs = append(accs, a)
	}

	p := NewPool(accs)
	p.SetPolicy(TypeFree, PoolPolicy{ActiveTarget: 15, MinHealthy: 8, ActivateBatch: 5})

	// 1) When healthy accounts == 15 (target met), 0 pending/expired accounts should be queued for renewal!
	due := p.NeedsProactiveRenew(24 * time.Hour)
	if len(due) != 0 {
		t.Fatalf("expected 0 accounts due when hot pool is satisfied, got %d: %+v", len(due), due)
	}

	// 2) Proactively renew active accounts expiring within 24h
	accs[0].ExpiresAt = time.Now().Add(2 * time.Hour)
	due = p.NeedsProactiveRenew(24 * time.Hour)
	if len(due) != 1 || due[0].ID != "act-00" {
		t.Fatalf("expected act-00 due for proactive renewal, got %+v", due)
	}

	// 3) When healthy count drops below target (deficit = 3), at most deficit inactive accounts are queued
	accs[0].ExpiresAt = time.Now().Add(48 * time.Hour) // reset act-00
	accs[1].Status = StatusBanned
	accs[2].Status = StatusBanned
	accs[3].Status = StatusBanned
	// Now healthy active = 12, target = 15 => deficit = 3
	due = p.NeedsProactiveRenew(24 * time.Hour)
	if len(due) != 3 {
		t.Fatalf("expected exactly 3 accounts queued to fill deficit, got %d", len(due))
	}
}
