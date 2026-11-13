package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Entry 表示网关内记录的一条请求追踪或号池运维事件。
type Entry struct {
	ID            string `json:"id"`
	Timestamp     string `json:"timestamp"`
	Kind          string `json:"kind"` // "request" | "event"
	Method        string `json:"method,omitempty"`
	Path          string `json:"path,omitempty"`
	Model         string `json:"model,omitempty"`
	AccountID     string `json:"account_id,omitempty"`
	AccountEmail  string `json:"account_email,omitempty"`
	Proxy         string `json:"proxy,omitempty"`
	Status        int    `json:"status,omitempty"`
	DurationMs    int64  `json:"duration_ms,omitempty"`
	FailoverTrace string `json:"failover_trace,omitempty"`
	AttemptCount  int    `json:"attempt_count,omitempty"`
	Error         string `json:"error,omitempty"`
	Note          string `json:"note,omitempty"`
}

// Summary 汇总当前缓冲内的请求统计与自愈情况。
type Summary struct {
	Capacity          int   `json:"capacity"`
	BufferedCount     int   `json:"buffered_count"`
	TotalRequests     int64 `json:"total_requests"`
	FailedRequests    int64 `json:"failed_requests"`
	FailoverRecovered int64 `json:"failover_recovered"`
	TotalEvents       int64 `json:"total_events"`
	FileLogging       bool  `json:"file_logging"`
	LogFile           string `json:"log_file,omitempty"`
}

// RingBuffer 是零 CGO、线程安全的固定容量内存环形日志器，可选按体积轮转写入 JSONL。
type RingBuffer struct {
	mu          sync.Mutex
	entries     []Entry
	capacity    int
	head        int
	count       int
	seq         int64
	totalReq    int64
	failedReq   int64
	recovered   int64
	totalEvents int64

	filePath string
	maxBytes int64
}

// NewRingBuffer 创建指定容量的环形日志器；filePath 非空时同步写 JSONL。
func NewRingBuffer(capacity int, filePath string, maxMB int) *RingBuffer {
	if capacity <= 0 {
		capacity = 1000
	}
	maxBytes := int64(maxMB) * 1024 * 1024
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024
	}
	return &RingBuffer{
		entries:  make([]Entry, capacity),
		capacity: capacity,
		filePath: strings.TrimSpace(filePath),
		maxBytes: maxBytes,
	}
}

// Record 写入一条结构化日志或运维事件。
func (r *RingBuffer) Record(e Entry) Entry {
	if r == nil {
		return e
	}
	r.mu.Lock()
	r.seq++
	if e.ID == "" {
		e.ID = fmt.Sprintf("evt-%d", r.seq)
	}
	if e.Timestamp == "" {
		e.Timestamp = time.Now().Format(time.RFC3339)
	}
	if e.Kind == "" {
		e.Kind = "request"
	}

	if e.Kind == "event" {
		r.totalEvents++
	} else {
		r.totalReq++
		if e.Status >= 400 || e.Error != "" {
			r.failedReq++
		} else if e.FailoverTrace != "" {
			r.recovered++
		}
	}

	r.entries[r.head] = e
	r.head = (r.head + 1) % r.capacity
	if r.count < r.capacity {
		r.count++
	}
	filePath := r.filePath
	maxBytes := r.maxBytes
	r.mu.Unlock()

	if filePath != "" {
		_ = appendRotatedJSONL(filePath, maxBytes, e)
	}
	return e
}

// RecordEvent 便捷记录号池 / 代理 / 卡密续期运维事件。
func (r *RingBuffer) RecordEvent(path, accountEmail, proxy, statusNote, errMsg string) Entry {
	return r.Record(Entry{
		Kind:         "event",
		Path:         path,
		AccountEmail: accountEmail,
		Proxy:        proxy,
		Error:        errMsg,
		Note:         statusNote,
	})
}

// Query 按新到旧返回筛选后的记录列表与统计摘要。
func (r *RingBuffer) Query(filter, pathFilter string, limit int) ([]Entry, Summary) {
	if r == nil {
		return []Entry{}, Summary{}
	}
	filter = strings.ToLower(strings.TrimSpace(filter))
	pathFilter = strings.TrimSpace(pathFilter)
	if limit <= 0 || limit > r.capacity {
		limit = 200
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	sum := Summary{
		Capacity:          r.capacity,
		BufferedCount:     r.count,
		TotalRequests:     r.totalReq,
		FailedRequests:    r.failedReq,
		FailoverRecovered: r.recovered,
		TotalEvents:       r.totalEvents,
		FileLogging:       r.filePath != "",
		LogFile:           r.filePath,
	}

	out := make([]Entry, 0, minInt(r.count, limit))
	for i := 0; i < r.count && len(out) < limit; i++ {
		idx := (r.head - 1 - i + r.capacity) % r.capacity
		item := r.entries[idx]

		if pathFilter != "" && pathFilter != "all" && item.Path != pathFilter {
			continue
		}
		switch filter {
		case "failover":
			if item.FailoverTrace == "" {
				continue
			}
		case "error":
			if item.Status < 400 && item.Error == "" {
				continue
			}
		case "event":
			if item.Kind != "event" {
				continue
			}
		}
		out = append(out, item)
	}
	return out, sum
}

var fileWriteMu sync.Mutex

func appendRotatedJSONL(filePath string, maxBytes int64, entry Entry) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	fileWriteMu.Lock()
	defer fileWriteMu.Unlock()

	if dir := filepath.Dir(filePath); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}
	if info, err := os.Stat(filePath); err == nil && info.Size()+int64(len(line)) > maxBytes {
		_ = os.Remove(filePath + ".1")
		_ = os.Rename(filePath, filePath+".1")
	}

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
