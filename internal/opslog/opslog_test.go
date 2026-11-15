package opslog

import (
	"testing"
)

func TestBufferRecordQueryAndClear(t *testing.T) {
	buf := NewBuffer(3)
	buf.Record(Entry{Source: "backend", Level: "info", Module: "boot", Message: "started"})
	buf.Record(Entry{Source: "frontend", Level: "warn", Module: "fetch", Message: "GET /api -> 404", Page: "#/drama"})
	buf.Record(Entry{Source: "backend", Level: "error", Module: "kami-login", Message: "login failed", Detail: "timeout"})
	// 超过容量 3 后覆盖最旧记录
	buf.Record(Entry{Source: "frontend", Level: "error", Module: "react", Message: "render crash"})

	entries, sum := buf.Query(QueryOptions{Limit: 10})
	if sum.BufferedCount != 3 {
		t.Fatalf("expected buffered_count 3, got %d", sum.BufferedCount)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Module != "react" || entries[1].Module != "kami-login" || entries[2].Module != "fetch" {
		t.Fatalf("unexpected reverse chronological order: %+v", entries)
	}

	errOnly, _ := buf.Query(QueryOptions{Level: "error", Limit: 10})
	if len(errOnly) != 2 {
		t.Fatalf("expected 2 error entries, got %d", len(errOnly))
	}

	feOnly, _ := buf.Query(QueryOptions{Source: "frontend", Search: "drama", Limit: 10})
	if len(feOnly) != 1 || feOnly[0].Module != "fetch" {
		t.Fatalf("expected 1 frontend drama entry, got %+v", feOnly)
	}

	buf.Clear()
	entries, sum = buf.Query(QueryOptions{Limit: 10})
	if len(entries) != 0 || sum.BufferedCount != 0 || sum.ErrorCount != 0 {
		t.Fatalf("expected empty buffer after Clear, got entries=%d sum=%+v", len(entries), sum)
	}
}

func TestLineWriterParsingAndANSIStrip(t *testing.T) {
	buf := NewBuffer(20)
	w := buf.NewLineWriter("backend", "log", "info")

	_, _ = w.Write([]byte("2026/11/15 14:40:00 [kami-login] failed account=a@b.com error=timeout\n"))
	_, _ = w.Write([]byte("\x1b[31m[GIN] 2026/11/15 - 14:40:01 | 500 |   12.3ms | 127.0.0.1 | POST \"/v1/chat/completions\"\x1b[0m\n"))
	_, _ = w.Write([]byte("[GIN] 2026/11/15 - 14:40:02 | 404 |    1.2ms | 127.0.0.1 | GET \"/missing\"\n"))

	entries, _ := buf.Query(QueryOptions{Limit: 10})
	if len(entries) != 3 {
		t.Fatalf("expected 3 parsed lines, got %d", len(entries))
	}
	if entries[0].Level != "warn" || entries[0].Module != "GIN" {
		t.Fatalf("expected 404 gin line to be warn/GIN, got %+v", entries[0])
	}
	if entries[1].Level != "error" || entries[1].Module != "GIN" {
		t.Fatalf("expected 500 gin line to be error/GIN, got %+v", entries[1])
	}
	if entries[2].Level != "error" || entries[2].Module != "kami-login" {
		t.Fatalf("expected kami-login error line, got %+v", entries[2])
	}
}
