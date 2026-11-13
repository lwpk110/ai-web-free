package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRingBufferWrapAndFilter(t *testing.T) {
	rb := NewRingBuffer(3, "", 1)

	rb.Record(Entry{Path: "/v1/chat/completions", Model: "gpt-5-6", Status: 200})
	rb.Record(Entry{Path: "/v1/images/generations", Model: "gpt-image-2.5", Status: 200, FailoverTrace: "a1(429) -> a2(200)"})
	rb.Record(Entry{Path: "/v1/images/edits", Model: "gpt-image-2.5", Status: 500, Error: "upstream timeout"})
	// 超过容量 3，应淘汰第 1 条
	rb.RecordEvent("kami-worker", "foo@icloud.com", "", "renewed token", "")

	all, sum := rb.Query("all", "", 10)
	if len(all) != 3 {
		t.Fatalf("expected 3 entries after wrap, got %d", len(all))
	}
	if all[0].Kind != "event" || all[2].Path != "/v1/images/generations" {
		t.Fatalf("unexpected newest-first order: %+v", all)
	}
	if sum.TotalRequests != 3 || sum.TotalEvents != 1 || sum.FailoverRecovered != 1 || sum.FailedRequests != 1 {
		t.Fatalf("unexpected summary: %+v", sum)
	}

	failoverItems, _ := rb.Query("failover", "", 10)
	if len(failoverItems) != 1 || failoverItems[0].Path != "/v1/images/generations" {
		t.Fatalf("unexpected failover filter result: %+v", failoverItems)
	}

	errItems, _ := rb.Query("error", "", 10)
	if len(errItems) != 1 || errItems[0].Status != 500 {
		t.Fatalf("unexpected error filter result: %+v", errItems)
	}

	pathItems, _ := rb.Query("all", "/v1/images/edits", 10)
	if len(pathItems) != 1 || pathItems[0].Path != "/v1/images/edits" {
		t.Fatalf("unexpected path filter result: %+v", pathItems)
	}
}

func TestRingBufferJSONLRotation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "gateway.jsonl")

	// 设置一个极小 maxBytes 强制触发轮转
	rb := NewRingBuffer(10, logPath, 1)
	rb.maxBytes = 120

	for i := 0; i < 6; i++ {
		rb.Record(Entry{Path: "/v1/chat/completions", Model: "gpt-5-6", Status: 200, Note: strings.Repeat("x", 40)})
	}

	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("expected active log file: %v", err)
	}
	if _, err := os.Stat(logPath + ".1"); err != nil {
		t.Fatalf("expected rotated backup file .1: %v", err)
	}
}
