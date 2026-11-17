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
