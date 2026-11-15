package opslog

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Entry 表示一条后台或前端运维日志，用于实时排错。
type Entry struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Source    string `json:"source"` // "backend" | "frontend"
	Level     string `json:"level"`  // "error" | "warn" | "info" | "debug"
	Module    string `json:"module,omitempty"`
	Message   string `json:"message"`
	Detail    string `json:"detail,omitempty"`
	Page      string `json:"page,omitempty"`
}

// Summary 汇总当前运维日志缓冲的统计信息。
type Summary struct {
	Capacity      int   `json:"capacity"`
	BufferedCount int   `json:"buffered_count"`
	TotalBackend  int64 `json:"total_backend"`
	TotalFrontend int64 `json:"total_frontend"`
	ErrorCount    int64 `json:"error_count"`
	WarnCount     int64 `json:"warn_count"`
}

// QueryOptions 控制运维日志查询过滤条件。
type QueryOptions struct {
	Source string
	Level  string
	Search string
	Limit  int
}

// Buffer 是零 CGO、线程安全的固定容量内存运维日志环形缓冲。
type Buffer struct {
	mu            sync.Mutex
	entries       []Entry
	capacity      int
	head          int
	count         int
	seq           int64
	totalBackend  int64
	totalFrontend int64
	errorCount    int64
	warnCount     int64
	hookOnce      sync.Once
}

var (
	globalOnce sync.Once
	globalBuf  *Buffer
	ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	ginStatus  = regexp.MustCompile(`\|\s*([1-5][0-9]{2})\s*\|`)
)

// Global 返回全局共享的运维日志环形缓冲实例。
func Global() *Buffer {
	globalOnce.Do(func() {
		globalBuf = NewBuffer(2000)
	})
	return globalBuf
}

// NewBuffer 创建指定容量的内存运维日志环形缓冲。
func NewBuffer(capacity int) *Buffer {
	if capacity <= 0 {
		capacity = 2000
	}
	return &Buffer{
		entries:  make([]Entry, capacity),
		capacity: capacity,
	}
}

// Record 写入一条结构化运维日志。
func (b *Buffer) Record(e Entry) Entry {
	if b == nil {
		return e
	}
	e.Source = normalizeSource(e.Source)
	e.Level = normalizeLevel(e.Level)
	e.Message = strings.TrimSpace(e.Message)
	if len(e.Message) > 4000 {
		e.Message = e.Message[:4000] + "...(truncated)"
	}
	if len(e.Detail) > 12000 {
		e.Detail = e.Detail[:12000] + "...(truncated)"
	}

	b.mu.Lock()
	b.seq++
	if e.ID == "" {
		e.ID = fmt.Sprintf("ops-%d", b.seq)
	}
	if e.Timestamp == "" {
		e.Timestamp = time.Now().Format(time.RFC3339Nano)
	}

	if e.Source == "frontend" {
		b.totalFrontend++
	} else {
		b.totalBackend++
	}
	switch e.Level {
	case "error":
		b.errorCount++
	case "warn":
		b.warnCount++
	}

	b.entries[b.head] = e
	b.head = (b.head + 1) % b.capacity
	if b.count < b.capacity {
		b.count++
	}
	b.mu.Unlock()
	return e
}

// Clear 清空当前环形缓冲中的历史日志与计数。
func (b *Buffer) Clear() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.entries {
		b.entries[i] = Entry{}
	}
	b.head = 0
	b.count = 0
	b.totalBackend = 0
	b.totalFrontend = 0
	b.errorCount = 0
	b.warnCount = 0
}

// Query 按新到旧返回过滤后的运维日志列表及统计摘要。
func (b *Buffer) Query(opts QueryOptions) ([]Entry, Summary) {
	if b == nil {
		return []Entry{}, Summary{}
	}
	source := strings.ToLower(strings.TrimSpace(opts.Source))
	level := strings.ToLower(strings.TrimSpace(opts.Level))
	search := strings.ToLower(strings.TrimSpace(opts.Search))
	limit := opts.Limit
	if limit <= 0 || limit > b.capacity {
		limit = 200
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	sum := Summary{
		Capacity:      b.capacity,
		BufferedCount: b.count,
		TotalBackend:  b.totalBackend,
		TotalFrontend: b.totalFrontend,
		ErrorCount:    b.errorCount,
		WarnCount:     b.warnCount,
	}

	out := make([]Entry, 0, minInt(b.count, limit))
	for i := 0; i < b.count && len(out) < limit; i++ {
		idx := (b.head - 1 - i + b.capacity) % b.capacity
		item := b.entries[idx]

		if source != "" && source != "all" && item.Source != source {
			continue
		}
		if level != "" && level != "all" {
			if level == "warn_error" {
				if item.Level != "warn" && item.Level != "error" {
					continue
				}
			} else if item.Level != level {
				continue
			}
		}
		if search != "" {
			haystack := strings.ToLower(item.Module + " " + item.Message + " " + item.Detail + " " + item.Page)
			if !strings.Contains(haystack, search) {
				continue
			}
		}
		out = append(out, item)
	}
	return out, sum
}

// InstallStdLogHook 将标准库 log 与 Gin 输出同时镜像写入运维日志缓冲。
func (b *Buffer) InstallStdLogHook() {
	if b == nil {
		return
	}
	b.hookOnce.Do(func() {
		log.SetOutput(io.MultiWriter(os.Stderr, b.NewLineWriter("backend", "log", "info")))
		gin.DefaultWriter = io.MultiWriter(os.Stdout, b.NewLineWriter("backend", "gin", "info"))
		gin.DefaultErrorWriter = io.MultiWriter(os.Stderr, b.NewLineWriter("backend", "gin-err", "error"))
	})
}

// NewLineWriter 返回一个按行解析并写入 Buffer 的线程安全 io.Writer。
func (b *Buffer) NewLineWriter(source, defaultModule, defaultLevel string) io.Writer {
	return &lineWriter{
		buf:           b,
		source:        normalizeSource(source),
		defaultModule: defaultModule,
		defaultLevel:  normalizeLevel(defaultLevel),
	}
}

type lineWriter struct {
	mu            sync.Mutex
	partial       bytes.Buffer
	buf           *Buffer
	source        string
	defaultModule string
	defaultLevel  string
}

func (w *lineWriter) Write(p []byte) (int, error) {
	if w == nil || w.buf == nil || len(p) == 0 {
		return len(p), nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	_, _ = w.partial.Write(p)
	for {
		data := w.partial.Bytes()
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := string(data[:idx])
		// 移除已消费行及换行符
		rest := make([]byte, len(data[idx+1:]))
		copy(rest, data[idx+1:])
		w.partial.Reset()
		if len(rest) > 0 {
			_, _ = w.partial.Write(rest)
		}
		w.recordLine(line)
	}
	return len(p), nil
}

func (w *lineWriter) recordLine(raw string) {
	clean := strings.TrimSpace(ansiRegexp.ReplaceAllString(raw, ""))
	if clean == "" {
		return
	}
	module, msg := extractModuleAndMessage(clean, w.defaultModule)
	level := inferLevel(clean, w.defaultLevel)
	w.buf.Record(Entry{
		Source:  w.source,
		Level:   level,
		Module:  module,
		Message: msg,
	})
}

func extractModuleAndMessage(line, defaultModule string) (string, string) {
	module := defaultModule
	msg := line

	// 跳过标准 log.Printf 前缀如 "2026/11/15 14:40:00 "
	if len(msg) > 20 && msg[4] == '/' && msg[7] == '/' && msg[10] == ' ' && msg[13] == ':' && msg[16] == ':' && msg[19] == ' ' {
		msg = strings.TrimSpace(msg[20:])
	}

	if strings.HasPrefix(msg, "[") {
		if end := strings.Index(msg, "]"); end > 1 && end < 48 {
			tag := strings.TrimSpace(msg[1:end])
			if tag != "" {
				module = tag
				rest := strings.TrimSpace(msg[end+1:])
				if rest != "" {
					msg = rest
				}
			}
		}
	}
	return module, msg
}

func inferLevel(line, fallback string) string {
	lower := strings.ToLower(line)
	if m := ginStatus.FindStringSubmatch(line); len(m) == 2 {
		switch m[1][0] {
		case '5':
			return "error"
		case '4':
			return "warn"
		}
	}
	if strings.Contains(lower, "panic") ||
		strings.Contains(lower, "fatal") ||
		strings.Contains(lower, "error=") ||
		strings.Contains(lower, "err=") ||
		strings.Contains(lower, "failed") ||
		strings.Contains(lower, " error") ||
		strings.Contains(line, "失败") ||
		strings.Contains(line, "异常") {
		return "error"
	}
	if strings.Contains(lower, "warn") ||
		strings.Contains(lower, "retry") ||
		strings.Contains(lower, "fallback") ||
		strings.Contains(line, "重试") ||
		strings.Contains(line, "冷却") {
		return "warn"
	}
	return fallback
}

func normalizeSource(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "frontend":
		return "frontend"
	default:
		return "backend"
	}
}

func normalizeLevel(l string) string {
	switch strings.ToLower(strings.TrimSpace(l)) {
	case "error", "err":
		return "error"
	case "warn", "warning":
		return "warn"
	case "debug":
		return "debug"
	default:
		return "info"
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
