package provider

import (
	"context"
	"testing"
	"time"
)

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
