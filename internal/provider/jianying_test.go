package provider

import (
	"context"
	"strings"
	"testing"
)

func TestSplitTextForTTS(t *testing.T) {
	// 1. 空文本测试
	if chunks := SplitTextForTTS("", 100); len(chunks) != 0 {
		t.Fatalf("expected empty slice for empty text, got %v", chunks)
	}

	// 2. 短文本不拆分
	shortText := "注意看，眼前这个女人叫小美。"
	chunks := SplitTextForTTS(shortText, 50)
	if len(chunks) != 1 || chunks[0] != shortText {
		t.Fatalf("expected single chunk, got %v", chunks)
	}

	// 3. 长标点文本拆分
	longText := "很久很久以前，在一座神秘的大森林里。住着一只聪明活泼的小猴子！它每天都在树林里采摘果子，帮助迷路的小动物？直到有一天，天边出现了一道奇怪的光芒；整个森林都开始震动了起来。"
	chunks = SplitTextForTTS(longText, 40)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for long text, got %d", len(chunks))
	}

	for i, c := range chunks {
		if strings.TrimSpace(c) == "" {
			t.Fatalf("chunk %d is empty", i)
		}
	}
}

func TestJianyingVoices(t *testing.T) {
	voices := GetJianyingVoices()
	if len(voices) < 10 {
		t.Fatalf("expected at least 10 voices, got %d", len(voices))
	}

	v, ok := FindJianyingVoice("zh_female_shuangkuai")
	if !ok || v.Name == "" {
		t.Fatalf("expected to find zh_female_shuangkuai voice")
	}

	mapped := MapOpenAIVoiceToJianying("alloy")
	if mapped != "zh_female_shuangkuai" {
		t.Fatalf("expected alloy to map to zh_female_shuangkuai, got %s", mapped)
	}
}

func TestMapEdgeVoiceToJianyingKeepsVoicesDistinct(t *testing.T) {
	legacyMapped := []string{
		"zh-CN-XiaoxiaoNeural",
		"zh-CN-YunxiNeural",
		"zh-CN-YunyangNeural",
		"zh-CN-YunxiaNeural",
		"zh-CN-YunjianNeural",
		"zh-CN-liaoning-XiaobeiNeural",
		"zh-HK-HiuGaaiNeural",
		"zh-TW-HsiaoChenNeural",
		"en-US-JennyNeural",
	}
	mapped := make(map[string]bool, len(legacyMapped))
	for _, voiceID := range legacyMapped {
		fallback := MapEdgeVoiceToJianying(voiceID)
		if _, ok := FindJianyingVoice(fallback); !ok {
			t.Errorf("Edge voice %q maps to unknown fallback %q", voiceID, fallback)
		}
		if mapped[fallback] {
			t.Errorf("Edge voice %q shares fallback %q with another Edge voice", voiceID, fallback)
		}
		mapped[fallback] = true
	}

	// 验证本地 Kokoro 映射同样保持音色独立
	localSeen := make(map[string]string)
	for _, openaiVoice := range []string{"alloy", "ash", "coral", "echo", "fable", "onyx", "nova", "sage", "shimmer"} {
		localVoice := MapOpenAIVoiceToLocal(openaiVoice)
		if prev, dup := localSeen[localVoice]; dup {
			t.Errorf("OpenAI voices %q and %q collapsed to same local voice %q", prev, openaiVoice, localVoice)
		}
		localSeen[localVoice] = openaiVoice
	}
}

func TestJianYingProvider(t *testing.T) {
	p := NewJianYingProvider(nil)
	desc := p.Descriptor()
	if desc.ID != "jianying" || desc.Status != "disabled" {
		t.Fatalf("unexpected descriptor: %+v", desc)
	}

	if len(desc.Tags) == 0 {
		t.Fatalf("expected provider to have tags")
	}

	stats := p.Stats()
	if stats.Active != 0 || stats.Disabled != 1 {
		t.Fatalf("expected disabled retired channel, got %+v", stats)
	}

	accounts := p.ListAccounts("", "", "")
	if len(accounts) != 1 || accounts[0].Status != "disabled" {
		t.Fatalf("expected 1 disabled account snapshot, got %+v", accounts)
	}
}

func TestSynthesizeSingleChunk_EmptyText(t *testing.T) {
	ctx := context.Background()
	_, err := SynthesizeSingleChunk(ctx, "", "zh_female_shuangkuai", "")
	if err == nil {
		t.Fatalf("expected error for empty text")
	}
}
