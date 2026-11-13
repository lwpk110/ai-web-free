package proxy

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProxyHealthTripAndFailoverAllocation(t *testing.T) {
	p := NewPool([]string{"http://p1:8080", "http://p2:8080"})
	p.failThreshold = 2
	p.cooldown = 50 * time.Millisecond

	if p.ReportFailure("http://p1:8080", "dial error 1") {
		t.Fatal("first failure should not trip breaker")
	}
	if !p.ReportFailure("http://p1:8080", "dial error 2") {
		t.Fatal("second failure should trip breaker")
	}
	if p.IsHealthy("http://p1:8080") {
		t.Fatal("p1 should be cooling after trip")
	}

	// AllocateHealthy 应避开 cooling 状态的 p1
	for i := 0; i < 4; i++ {
		got := p.AllocateHealthy(nil)
		if got != "http://p2:8080" {
			t.Fatalf("expected healthy p2, got %q", got)
		}
	}

	// 冷却期过后自动恢复
	time.Sleep(60 * time.Millisecond)
	if !p.IsHealthy("http://p1:8080") {
		t.Fatal("p1 should recover automatically after cooldown")
	}
}

func TestProxyProbeAll(t *testing.T) {
	p := NewPool([]string{"http://good:8080", "http://bad:8080"})
	snaps := p.ProbeAll(context.Background(), func(_ context.Context, proxyURL string) (int64, error) {
		if proxyURL == "http://bad:8080" {
			return 0, errors.New("connection refused")
		}
		return 42, nil
	}, map[string]int{"http://good:8080": 5})

	if len(snaps) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snaps))
	}
	if snaps[0].Status != "active" || snaps[0].LatencyMs != 42 || snaps[0].BoundAccounts != 5 {
		t.Fatalf("unexpected good snapshot: %+v", snaps[0])
	}
	if snaps[1].Status != "cooling" || snaps[1].LastError == "" {
		t.Fatalf("unexpected bad snapshot: %+v", snaps[1])
	}
}
