package health

import (
	"os"
	"runtime"
	"sync"
	"time"
)

// BackgroundServiceStatus 描述单个后台协程、调度器或队列 Worker 的启停与运行状态。
type BackgroundServiceStatus struct {
	Name       string `json:"name"`
	Label      string `json:"label"`
	Category   string `json:"category"` // worker | scheduler
	Enabled    bool   `json:"enabled"`
	Running    bool   `json:"running"`
	Status     string `json:"status"` // running | disabled | stopped
	Interval   string `json:"interval,omitempty"`
	StartedAt  string `json:"started_at,omitempty"`
	LastTickAt string `json:"last_tick_at,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// SubsystemHealth 描述网关核心运行子系统（数据库、存储引擎、号池、代理池等）的健康状态。
type SubsystemHealth struct {
	Name   string         `json:"name"`
	Label  string         `json:"label"`
	Status string         `json:"status"` // healthy | degraded | disabled | unhealthy
	Detail string         `json:"detail"`
	Meta   map[string]any `json:"meta,omitempty"`
}

// RuntimeMetrics 采集当前 Go 进程的运行时资源与 GC 健康指标（零 CGO）。
type RuntimeMetrics struct {
	PID            int     `json:"pid"`
	GoVersion      string  `json:"go_version"`
	OS             string  `json:"os"`
	Arch           string  `json:"arch"`
	NumCPU         int     `json:"num_cpu"`
	Goroutines     int     `json:"goroutines"`
	MemAllocBytes  uint64  `json:"mem_alloc_bytes"`
	MemAllocMB     float64 `json:"mem_alloc_mb"`
	MemSysBytes    uint64  `json:"mem_sys_bytes"`
	MemSysMB       float64 `json:"mem_sys_mb"`
	HeapInuseBytes uint64  `json:"heap_inuse_bytes"`
	HeapObjects    uint64  `json:"heap_objects"`
	NumGC          uint32  `json:"num_gc"`
	LastGCAt       string  `json:"last_gc_at,omitempty"`
}

// ServerHealthSummary 聚合后台服务与核心子系统的健康计数，用于总览与 /health 探测。
type ServerHealthSummary struct {
	Status             string  `json:"status"` // healthy | degraded | unhealthy
	ServicesTotal      int     `json:"services_total"`
	ServicesRunning    int     `json:"services_running"`
	ServicesDisabled   int     `json:"services_disabled"`
	ServicesStopped    int     `json:"services_stopped"`
	SubsystemsTotal    int     `json:"subsystems_total"`
	SubsystemsHealthy  int     `json:"subsystems_healthy"`
	SubsystemsDegraded int     `json:"subsystems_degraded"`
	Goroutines         int     `json:"goroutines"`
	MemAllocMB         float64 `json:"mem_alloc_mb"`
}

// ServerStatusReport 为管理端 /admin/api/v1/system/status 返回的完整服务器健康监控报告。
type ServerStatusReport struct {
	Status             string                    `json:"status"`
	Service            string                    `json:"service"`
	Version            string                    `json:"version"`
	UptimeSeconds      int64                     `json:"uptime_seconds"`
	StartedAt          string                    `json:"started_at"`
	CheckedAt          string                    `json:"checked_at"`
	Summary            ServerHealthSummary       `json:"summary"`
	Runtime            RuntimeMetrics            `json:"runtime"`
	BackgroundServices []BackgroundServiceStatus `json:"background_services"`
	Subsystems         []SubsystemHealth         `json:"subsystems"`
}

// Tracker 线程安全地记录后台服务（goroutine/worker/scheduler）的启动、运行与停止状态。
type Tracker struct {
	mu       sync.RWMutex
	order    []string
	services map[string]*BackgroundServiceStatus
}

// NewTracker 创建一个新的后台服务状态追踪器。
func NewTracker() *Tracker {
	return &Tracker{
		services: make(map[string]*BackgroundServiceStatus),
	}
}

func resolveServiceStatus(enabled, running bool) string {
	if !enabled {
		return "disabled"
	}
	if running {
		return "running"
	}
	return "stopped"
}

// RegisterService 注册或更新一个后台服务的初始状态。
func (t *Tracker) RegisterService(name, label, category, interval, detail string, enabled, running bool) {
	if t == nil || name == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.services[name]; !exists {
		t.order = append(t.order, name)
	}
	startedAt := ""
	if running {
		startedAt = time.Now().UTC().Format(time.RFC3339)
	}
	t.services[name] = &BackgroundServiceStatus{
		Name:      name,
		Label:     label,
		Category:  category,
		Enabled:   enabled,
		Running:   running,
		Status:    resolveServiceStatus(enabled, running),
		Interval:  interval,
		StartedAt: startedAt,
		Detail:    detail,
	}
}

// MarkRunning 更新指定后台服务的运行状态，并在启动时记录 StartedAt。
func (t *Tracker) MarkRunning(name string, running bool, detail ...string) {
	if t == nil || name == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	svc, exists := t.services[name]
	if !exists {
		return
	}
	svc.Running = running
	if running && svc.StartedAt == "" {
		svc.StartedAt = time.Now().UTC().Format(time.RFC3339)
	}
	svc.Status = resolveServiceStatus(svc.Enabled, running)
	if len(detail) > 0 && detail[0] != "" {
		svc.Detail = detail[0]
	}
}

// MarkTick 记录后台服务最近一次执行心跳的时间与可选说明。
func (t *Tracker) MarkTick(name string, detail ...string) {
	if t == nil || name == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	svc, exists := t.services[name]
	if !exists {
		return
	}
	svc.LastTickAt = time.Now().UTC().Format(time.RFC3339)
	if len(detail) > 0 && detail[0] != "" {
		svc.Detail = detail[0]
	}
}

// Snapshot 按注册顺序返回所有后台服务状态的只读副本。
func (t *Tracker) Snapshot() []BackgroundServiceStatus {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()

	out := make([]BackgroundServiceStatus, 0, len(t.order))
	for _, name := range t.order {
		if svc, ok := t.services[name]; ok && svc != nil {
			out = append(out, *svc)
		}
	}
	return out
}

// CollectRuntimeMetrics 采集当前 Go 进程的 CPU、Goroutine 与内存 GC 指标。
func CollectRuntimeMetrics() RuntimeMetrics {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	lastGC := ""
	if m.LastGC > 0 {
		lastGC = time.Unix(0, int64(m.LastGC)).UTC().Format(time.RFC3339)
	}

	const mb = 1024 * 1024
	roundMB := func(b uint64) float64 {
		v := float64(b) / mb
		return float64(int(v*100+0.5)) / 100
	}

	return RuntimeMetrics{
		PID:            os.Getpid(),
		GoVersion:      runtime.Version(),
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		NumCPU:         runtime.NumCPU(),
		Goroutines:     runtime.NumGoroutine(),
		MemAllocBytes:  m.Alloc,
		MemAllocMB:     roundMB(m.Alloc),
		MemSysBytes:    m.Sys,
		MemSysMB:       roundMB(m.Sys),
		HeapInuseBytes: m.HeapInuse,
		HeapObjects:    m.HeapObjects,
		NumGC:          m.NumGC,
		LastGCAt:       lastGC,
	}
}

// SummarizeServices 统计后台服务列表中总计、运行中、未启用和已停止的数量。
func SummarizeServices(services []BackgroundServiceStatus) (total, running, disabled, stopped int) {
	total = len(services)
	for _, s := range services {
		switch s.Status {
		case "running":
			running++
		case "disabled":
			disabled++
		default:
			stopped++
		}
	}
	return total, running, disabled, stopped
}
