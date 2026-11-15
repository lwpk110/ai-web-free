package provider

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestEdgeSecMSGECv2Format(t *testing.T) {
	ts := float64(1730000000)
	token := generateEdgeSecMSGECv2(ts)
	parts := strings.Split(token, "-")
	if len(parts) != 2 || len(parts[0]) != 16 || len(parts[1]) != 64 {
		t.Fatalf("expected <16-hex>-<64-hex> format, got %q", token)
	}
	for _, seg := range parts {
		for i := 0; i < len(seg); i++ {
			c := seg[i]
			if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'F')) {
				t.Fatalf("expected uppercase hex char, got %q in %q", c, token)
			}
		}
	}
	// 同一 5 分钟时间窗口内应生成稳定 token
	if again := generateEdgeSecMSGECv2(ts + 60); again != token {
		t.Fatalf("expected deterministic token within 300s window, got %q vs %q", again, token)
	}
	// 跨过 5 分钟窗口后应变化
	if next := generateEdgeSecMSGECv2(ts + 305); next == token {
		t.Fatalf("expected different token across 300s window")
	}
}

func TestMapOpenAIVoiceToEdgeDistinct(t *testing.T) {
	seen := make(map[string]string)
	for _, v := range []string{"alloy", "ash", "coral", "echo", "fable", "onyx", "nova", "sage", "shimmer"} {
		mapped := MapOpenAIVoiceToEdge(v)
		if !strings.HasSuffix(mapped, "Neural") {
			t.Fatalf("expected Neural voice for %s, got %s", v, mapped)
		}
		if prev, dup := seen[mapped]; dup {
			t.Fatalf("voice %s and %s collapsed to same Edge voice %s", prev, v, mapped)
		}
		seen[mapped] = v
	}
}

func TestEdgeTTSSeedCachedVoicesAndFetchLive(t *testing.T) {
	p := NewEdgeTTSProvider()
	p.SeedCachedVoices([]VoiceDescriptor{
		{
			ID:       "zh-CN-TestCachedNeural",
			Name:     "Cached Test",
			Provider: "edge_tts",
			Gender:   "female",
			Locale:   "zh-CN",
		},
	})
	descs := p.VoiceDescriptors()
	found := false
	for _, d := range descs {
		if d.ID == "zh-CN-TestCachedNeural" {
			found = true
			if !d.Available {
				t.Fatalf("expected seeded Edge voice to be marked available")
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected seeded cached voice in VoiceDescriptors()")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	live, err := p.FetchLiveVoices(ctx)
	if err != nil {
		t.Logf("FetchLiveVoices offline/blocked (acceptable in offline CI): %v", err)
		return
	}
	if len(live) < len(BuiltinEdgeVoices) {
		t.Fatalf("expected at least %d live/curated voices, got %d", len(BuiltinEdgeVoices), len(live))
	}
	t.Logf("FetchLiveVoices returned %d Edge voices", len(live))
}

func TestEdgeTTSSynthesize(t *testing.T) {
	p := NewEdgeTTSProvider()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	audio, err := p.Synthesize(ctx, "测试EdgeTTS合成", "zh-CN-XiaoxiaoNeural", 1.0)
	if err != nil {
		t.Logf("Edge-TTS live dial error: %v (expected if sandbox network blocks outbound WSS)", err)
		return
	}
	if len(audio) == 0 {
		t.Fatalf("expected non-empty audio")
	}
	t.Logf("successfully synthesized %d bytes from Edge-TTS", len(audio))
}
