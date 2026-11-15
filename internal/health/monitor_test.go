package health

import "testing"

func TestTrackerLifecycle(t *testing.T) {
	tr := NewTracker()
	tr.RegisterService("account_health_check", "账号健康巡检", "scheduler", "10m0s", "每 10 分钟检查过期 Token", true, true)
	tr.RegisterService("kami_renew_worker", "卡密自动续期 Worker", "worker", "", "已禁用", false, false)

	snap := tr.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 services, got %d", len(snap))
	}
	if snap[0].Status != "running" || !snap[0].Running || snap[0].StartedAt == "" {
		t.Fatalf("expected first service running with StartedAt, got %+v", snap[0])
	}
	if snap[1].Status != "disabled" || snap[1].Running {
		t.Fatalf("expected second service disabled, got %+v", snap[1])
	}

	tr.MarkTick("account_health_check", "最近一次巡检完成")
	tr.MarkRunning("account_health_check", false, "服务已停止")
	snap = tr.Snapshot()
	if snap[0].Status != "stopped" || snap[0].Running {
		t.Fatalf("expected service stopped, got %+v", snap[0])
	}
	if snap[0].LastTickAt == "" || snap[0].Detail != "服务已停止" {
		t.Fatalf("expected LastTickAt and updated Detail, got %+v", snap[0])
	}

	total, running, disabled, stopped := SummarizeServices(snap)
	if total != 2 || running != 0 || disabled != 1 || stopped != 1 {
		t.Fatalf("unexpected summary: total=%d running=%d disabled=%d stopped=%d", total, running, disabled, stopped)
	}
}

func TestCollectRuntimeMetrics(t *testing.T) {
	m := CollectRuntimeMetrics()
	if m.PID <= 0 || m.GoVersion == "" || m.NumCPU <= 0 || m.Goroutines <= 0 {
		t.Fatalf("invalid runtime metrics: %+v", m)
	}
	if m.MemSysBytes == 0 {
		t.Fatalf("expected non-zero MemSysBytes, got %+v", m)
	}
}
