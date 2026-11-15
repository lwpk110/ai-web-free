package provider

import (
	"context"
	"fmt"
	"sync"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/proxy"
)

// JianYingProvider 剪映爆款 TTS 免密号池与合成插件
type JianYingProvider struct {
	mu        sync.RWMutex
	proxyPool *proxy.Pool
	stats     accounts.PoolStats
}

// NewJianYingProvider 创建剪映 TTS 插件实例
func NewJianYingProvider(proxyPool *proxy.Pool) *JianYingProvider {
	return &JianYingProvider{
		proxyPool: proxyPool,
		stats: accounts.PoolStats{
			Total:       1,
			Active:      1,
			NoAuthCount: 1,
		},
	}
}

func (p *JianYingProvider) Descriptor() ProviderDescriptor {
	featured := []string{
		"tts-1",
		"zh_female_shuangkuai",
		"zh_male_chunhou",
		"zh_male_houge",
		"zh_male_dongbeilaozong",
		"zh_female_sichuan",
		"zh_female_tianmei",
		"en_female_emotional",
	}

	return ProviderDescriptor{
		ID:                    "jianying",
		Name:                  "JianYing TTS (剪映语音)",
		Vendor:                "ByteDance",
		Status:                "enabled",
		Version:               "15.0.0",
		Description:           "剪映 200+ 热门爆款音色免密逆向合成引擎（支持影视解说小帅/小美、猴哥、方言、长文本智能分句流式拼接）",
		SupportedAccountTypes: []string{"noauth", "anonymous"},
		CredentialFormats:     []string{"none"},
		Capabilities: []string{
			"tts",
			"stream",
		},
		FeaturedModels: featured,
		Tags:           MakeTags("permanent-free", "no-auth", "tts-presets", "proxy-bound"),
		CostType:       "permanent_free",
		AuthType:       "no_auth",
	}
}

func (p *JianYingProvider) Stats() accounts.PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.stats
}

func (p *JianYingProvider) ListAccounts(typeFilter, statusFilter, search string) []accounts.AccountSnapshot {
	return []accounts.AccountSnapshot{
		{
			ID:           "jianying-anonymous-default",
			Email:        "jianying-tts@anonymous.local",
			Type:         "noauth",
			Status:       "active",
			PlanType:     "permanent_free",
			TokenPreview: "anonymous-token-free",
		},
	}
}

func (p *JianYingProvider) ImportCredentials(req ImportRequest) (ImportResult, error) {
	return ImportResult{
		Added:     0,
		Skipped:   0,
		Failed:    0,
		TotalPool: 1,
		Messages:  []string{"JianYing TTS 免密逆向通道无需导入凭据，开箱即用"},
	}, nil
}

func (p *JianYingProvider) ExecuteAccountAction(id, action string) (map[string]interface{}, error) {
	if action == "probe" || action == "test" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		var proxyStr string
		if p.proxyPool != nil {
			proxyStr = p.proxyPool.AllocateHealthy(nil)
		}

		audio, err := SynthesizeJianyingSpeech(ctx, "测试剪映语音合成通道连通性", "zh_female_shuangkuai", 1.0, proxyStr)
		if err != nil {
			return map[string]interface{}{
				"status": "failed",
				"error":  err.Error(),
			}, err
		}

		return map[string]interface{}{
			"status":     "success",
			"audio_size": len(audio),
			"timestamp":  time.Now().Unix(),
		}, nil
	}

	return nil, fmt.Errorf("unsupported action: %s", action)
}

// Synthesize 提供给网关 HTTP Handler 调用的核心语音合成入口
func (p *JianYingProvider) Synthesize(ctx context.Context, text, voice string, speed float64) ([]byte, error) {
	var proxyStr string
	if p.proxyPool != nil {
		proxyStr = p.proxyPool.AllocateHealthy(nil)
	}
	return SynthesizeJianyingSpeech(ctx, text, voice, speed, proxyStr)
}
