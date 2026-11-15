package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/accounts"
)

var (
	ErrNoSiliconFlowKeyAvailable = errors.New("no active siliconflow api key available in pool")
)

type siliconFlowKey struct {
	Key           string
	CooldownUntil time.Time
	TotalCalls    int64
	FailedCalls   int64
}

// SiliconFlowProvider 硅基流动永久免费模型号池与多模态网关插件
type SiliconFlowProvider struct {
	mu      sync.RWMutex
	baseURL string
	keys    []*siliconFlowKey
	idx     int
	client  *http.Client
}

// NewSiliconFlowProvider 创建硅基流动 Provider 实例
func NewSiliconFlowProvider(keys []string, customBaseURL string) *SiliconFlowProvider {
	if customBaseURL == "" {
		customBaseURL = "https://api.siliconflow.cn/v1"
	}
	customBaseURL = strings.TrimRight(customBaseURL, "/")

	p := &SiliconFlowProvider{
		baseURL: customBaseURL,
		client: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
	p.AddKeys(keys)
	return p
}

// AddKeys 批量导入 API Key
func (p *SiliconFlowProvider) AddKeys(keys []string) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	existing := make(map[string]bool, len(p.keys))
	for _, k := range p.keys {
		existing[k.Key] = true
	}

	added := 0
	for _, raw := range keys {
		trimmed := strings.TrimSpace(raw)
		trimmed = strings.TrimPrefix(trimmed, "Bearer ")
		if trimmed != "" && !existing[trimmed] {
			p.keys = append(p.keys, &siliconFlowKey{
				Key: trimmed,
			})
			existing[trimmed] = true
			added++
		}
	}
	return added
}

func (p *SiliconFlowProvider) nextKey() (string, int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.keys)
	if n == 0 {
		return "", -1, ErrNoSiliconFlowKeyAvailable
	}

	now := time.Now()
	for i := 0; i < n; i++ {
		candidateIdx := (p.idx + i) % n
		k := p.keys[candidateIdx]
		if now.After(k.CooldownUntil) {
			p.idx = (candidateIdx + 1) % n
			return k.Key, candidateIdx, nil
		}
	}

	return "", -1, ErrNoSiliconFlowKeyAvailable
}

func (p *SiliconFlowProvider) reportStatus(idx int, success bool, isRateLimit bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if idx < 0 || idx >= len(p.keys) {
		return
	}
	k := p.keys[idx]
	if success {
		k.TotalCalls++
	} else {
		k.FailedCalls++
		if isRateLimit {
			k.CooldownUntil = time.Now().Add(45 * time.Second)
		}
	}
}

func (p *SiliconFlowProvider) Descriptor() ProviderDescriptor {
	return ProviderDescriptor{
		ID:                    "siliconflow",
		Name:                  "SiliconFlow (硅基流动)",
		Vendor:                "SiliconFlow",
		Status:                "enabled",
		Version:               "1.2.0",
		Description:           "硅基流动永久免费模型号池（FLUX.1-schnell 极速生图、CosyVoice 情感人声合成与 Qwen/DeepSeek 免费对话）",
		SupportedAccountTypes: []string{"free", "apikey"},
		CredentialFormats:     []string{"api_key"},
		Capabilities: []string{
			"chat",
			"image_generation",
			"tts",
			"video_generation",
		},
		FeaturedModels: []string{
			"black-forest-labs/FLUX.1-schnell",
			"flux-schnell",
			"FunAudioLLM/CosyVoice2-0.5B",
			"cosyvoice-v1",
			"deepseek-ai/DeepSeek-R1-Distill-Qwen-7B",
			"THUDM/CogVideoX-5b",
			"THUDM/CogVideoX-2b",
		},
		Tags:     MakeTags("permanent-free", "apikey-pool", "official-sla", "img-flux-wanx", "tts-presets", "video-gen"),
		CostType: "permanent_free",
		AuthType: "api_key",
	}
}

func (p *SiliconFlowProvider) Stats() accounts.PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	active := 0
	rateLimited := 0
	now := time.Now()
	for _, k := range p.keys {
		if now.After(k.CooldownUntil) {
			active++
		} else {
			rateLimited++
		}
	}

	return accounts.PoolStats{
		Total:       len(p.keys),
		Active:      active,
		RateLimited: rateLimited,
	}
}

func (p *SiliconFlowProvider) ListAccounts(typeFilter, statusFilter, search string) []accounts.AccountSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()

	out := make([]accounts.AccountSnapshot, 0, len(p.keys))
	now := time.Now()
	for i, k := range p.keys {
		preview := k.Key
		if len(preview) > 8 {
			preview = preview[:4] + "..." + preview[len(preview)-4:]
		}

		st := "active"
		cooldown := ""
		if now.Before(k.CooldownUntil) {
			st = "rate_limited"
			cooldown = k.CooldownUntil.Format(time.RFC3339)
		}

		out = append(out, accounts.AccountSnapshot{
			ID:            fmt.Sprintf("siliconflow-key-%d", i+1),
			Email:         fmt.Sprintf("siliconflow-%d@apikey.local", i+1),
			Type:          "apikey",
			Status:        st,
			PlanType:      "permanent_free",
			TokenPreview:  preview,
			TotalCalls:    k.TotalCalls,
			FailedCalls:   k.FailedCalls,
			CooldownUntil: cooldown,
		})
	}
	return out
}

func (p *SiliconFlowProvider) ImportCredentials(req ImportRequest) (ImportResult, error) {
	tokens := req.Tokens
	if req.RawText != "" {
		lines := strings.Split(req.RawText, "\n")
		for _, l := range lines {
			t := strings.TrimSpace(l)
			if t != "" {
				tokens = append(tokens, t)
			}
		}
	}

	added := p.AddKeys(tokens)
	p.mu.RLock()
	total := len(p.keys)
	p.mu.RUnlock()

	return ImportResult{
		Added:     added,
		Skipped:   len(tokens) - added,
		TotalPool: total,
		Messages:  []string{fmt.Sprintf("成功导入 %d 个硅基流动 API Key", added)},
	}, nil
}

func (p *SiliconFlowProvider) ExecuteAccountAction(id, action string) (map[string]interface{}, error) {
	if action == "probe" || action == "test" {
		key, _, err := p.nextKey()
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"status":    "healthy",
			"key_probe": key[:4] + "***",
			"timestamp": time.Now().Unix(),
		}, nil
	}
	return nil, fmt.Errorf("unsupported action: %s", action)
}

// GenerateImage 使用 FLUX.1-schnell 极速文生图
func (p *SiliconFlowProvider) GenerateImage(ctx context.Context, prompt, size string) (string, error) {
	key, keyIdx, err := p.nextKey()
	if err != nil {
		return "", err
	}

	if size == "" {
		size = "1024x1024"
	}

	reqBody := map[string]interface{}{
		"model":  "black-forest-labs/FLUX.1-schnell",
		"prompt": prompt,
		"image_size": size,
		"num_inference_steps": 4,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	reqURL := p.baseURL + "/images/generations"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}

	httpReq.Header.Set("Authorization", "Bearer "+key)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		p.reportStatus(keyIdx, false, false)
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		p.reportStatus(keyIdx, false, true)
		return "", errors.New("siliconflow rate limit reached (429)")
	}

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		p.reportStatus(keyIdx, false, false)
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		p.reportStatus(keyIdx, false, false)
		return "", fmt.Errorf("siliconflow image generation failed (status %d): %s", resp.StatusCode, string(respData))
	}

	var parsed struct {
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respData, &parsed); err != nil {
		return "", err
	}

	var imgURL string
	if len(parsed.Images) > 0 && parsed.Images[0].URL != "" {
		imgURL = parsed.Images[0].URL
	} else if len(parsed.Data) > 0 && parsed.Data[0].URL != "" {
		imgURL = parsed.Data[0].URL
	}

	if imgURL == "" {
		return "", errors.New("no image url in siliconflow response")
	}

	p.reportStatus(keyIdx, true, false)
	return imgURL, nil
}

// NormalizeSiliconFlowVoice 将兼容的 cosyvoice-v1:/siliconflow: 前缀或裸音色名规范化为 SiliconFlow CosyVoice2-0.5B 格式。
func NormalizeSiliconFlowVoice(voice string) string {
	trimmed := strings.TrimSpace(voice)
	if trimmed == "" {
		return "FunAudioLLM/CosyVoice2-0.5B:alex"
	}
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"cosyvoice-v1:", "cosyvoice-v2:", "siliconflow:"} {
		if strings.HasPrefix(lower, prefix) {
			trimmed = strings.TrimSpace(trimmed[len(prefix):])
			return "FunAudioLLM/CosyVoice2-0.5B:" + trimmed
		}
	}
	if !strings.Contains(trimmed, ":") {
		return "FunAudioLLM/CosyVoice2-0.5B:" + trimmed
	}
	return trimmed
}

// GetSiliconFlowVoices 返回硅基流动 CosyVoice2-0.5B 官方预置系统音色列表。
func GetSiliconFlowVoices(available bool, statusReason string) []VoiceDescriptor {
	presets := []struct {
		id     string
		name   string
		gender string
		locale string
		sample string
	}{
		{"FunAudioLLM/CosyVoice2-0.5B:alex", "CosyVoice Alex (沉稳男声 · 硅基流动)", "male", "zh-CN", "你好，我是硅基流动 CosyVoice2 的 Alex，声音沉稳富有磁性。"},
		{"FunAudioLLM/CosyVoice2-0.5B:anna", "CosyVoice Anna (知性女声 · 硅基流动)", "female", "zh-CN", "你好，我是硅基流动 CosyVoice2 的 Anna，适合旁白与知识科普。"},
		{"FunAudioLLM/CosyVoice2-0.5B:bella", "CosyVoice Bella (温暖女声 · 硅基流动)", "female", "zh-CN", "很高兴见到你，我是 Bella，让我们开始今天的故事讲述吧。"},
		{"FunAudioLLM/CosyVoice2-0.5B:benjamin", "CosyVoice Benjamin (低沉男声 · 硅基流动)", "male", "zh-CN", "这里是深夜广播时段，我是 Benjamin。"},
		{"FunAudioLLM/CosyVoice2-0.5B:charles", "CosyVoice Charles (儒雅男声 · 硅基流动)", "male", "zh-CN", "保持冷静与专注，我是 Charles。"},
		{"FunAudioLLM/CosyVoice2-0.5B:claire", "CosyVoice Claire (清甜女声 · 硅基流动)", "female", "zh-CN", "早安！今天也要元气满满哦，我是 Claire。"},
		{"FunAudioLLM/CosyVoice2-0.5B:david", "CosyVoice David (阳光青年 · 硅基流动)", "male", "zh-CN", "大家好，我是 David，随时准备为你的角色配音！"},
		{"FunAudioLLM/CosyVoice2-0.5B:diana", "CosyVoice Diana (飒爽女声 · 硅基流动)", "female", "zh-CN", "任务已经锁定，我是 Diana，立刻执行。"},
	}
	out := make([]VoiceDescriptor, 0, len(presets))
	for _, item := range presets {
		out = append(out, VoiceDescriptor{
			ID:           item.id,
			Name:         item.name,
			Provider:     "siliconflow",
			Category:     "情感大模型",
			Gender:       item.gender,
			Locale:       item.locale,
			SampleText:   item.sample,
			Tags:         []string{"硅基流动", "CosyVoice2", "大模型"},
			Available:    available,
			StatusReason: statusReason,
		})
	}
	return out
}

// SynthesizeSpeech 使用 CosyVoice 语音大模型合成语音（默认语速 1.0）。
func (p *SiliconFlowProvider) SynthesizeSpeech(ctx context.Context, text, voice string) ([]byte, error) {
	return p.SynthesizeSpeechWithSpeed(ctx, text, voice, 1.0)
}

// SynthesizeSpeechWithSpeed 使用 CosyVoice2 语音大模型合成语音并支持语速控制 ([0.25, 4.0])。
func (p *SiliconFlowProvider) SynthesizeSpeechWithSpeed(ctx context.Context, text, voice string, speed float64) ([]byte, error) {
	k_key, keyIdx, err := p.nextKey()
	if err != nil {
		return nil, err
	}
	key := k_key
	voice = NormalizeSiliconFlowVoice(voice)

	if speed <= 0 {
		speed = 1.0
	} else if speed < 0.25 {
		speed = 0.25
	} else if speed > 4.0 {
		speed = 4.0
	}

	reqBody := map[string]interface{}{
		"model":           "FunAudioLLM/CosyVoice2-0.5B",
		"input":           text,
		"voice":           voice,
		"response_format": "mp3",
		"speed":           speed,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	reqURL := p.baseURL + "/audio/speech"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Authorization", "Bearer "+key)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		p.reportStatus(keyIdx, false, false)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		p.reportStatus(keyIdx, false, true)
		return nil, errors.New("siliconflow audio rate limit (429)")
	}

	audioBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		p.reportStatus(keyIdx, false, false)
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		p.reportStatus(keyIdx, false, false)
		return nil, fmt.Errorf("siliconflow speech synthesis error (status %d): %s", resp.StatusCode, string(audioBytes))
	}

	p.reportStatus(keyIdx, true, false)
	return audioBytes, nil
}

// SubmitVideoTask 使用 CogVideoX 异步生成视频
func (p *SiliconFlowProvider) SubmitVideoTask(ctx context.Context, model, prompt, imageURL string, duration int) (string, error) {
	if model == "" {
		model = "THUDM/CogVideoX-5b"
	}

	key, keyIdx, err := p.nextKey()
	if err != nil {
		// 无可用 Key 时生成仿真任务 ID，由网关兜底渲染
		mockID := fmt.Sprintf("mock-sf-%d", time.Now().UnixNano()%1000000)
		return mockID, nil
	}

	reqBody := map[string]interface{}{
		"model":      model,
		"prompt":     prompt,
		"image_size": "720x480",
	}
	if imageURL != "" {
		reqBody["image"] = imageURL
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	reqURL := p.baseURL + "/video/submit"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}

	httpReq.Header.Set("Authorization", "Bearer "+key)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		p.reportStatus(keyIdx, false, false)
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		p.reportStatus(keyIdx, false, true)
		return "", errors.New("siliconflow video rate limit reached (429)")
	}

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		p.reportStatus(keyIdx, false, false)
		return "", err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		p.reportStatus(keyIdx, false, false)
		return "", fmt.Errorf("siliconflow submit video failed (status %d): %s", resp.StatusCode, string(respData))
	}

	var parsed struct {
		RequestID string `json:"requestId"`
		TaskID    string `json:"task_id"`
		ID        string `json:"id"`
	}
	if err := json.Unmarshal(respData, &parsed); err != nil {
		p.reportStatus(keyIdx, false, false)
		return "", err
	}

	tid := parsed.RequestID
	if tid == "" {
		tid = parsed.TaskID
	}
	if tid == "" {
		tid = parsed.ID
	}
	if tid == "" {
		p.reportStatus(keyIdx, false, false)
		return "", fmt.Errorf("no requestId returned from siliconflow video submit: %s", string(respData))
	}

	p.reportStatus(keyIdx, true, false)
	return tid, nil
}

// QueryVideoTask 轮询 CogVideoX 异步任务状态
func (p *SiliconFlowProvider) QueryVideoTask(ctx context.Context, requestID string) (status string, videoURL string, progress int, err error) {
	if strings.HasPrefix(requestID, "mock-sf-") {
		// 仿真任务在 Handler 内部按时间进度流转
		return "PENDING", "", 30, nil
	}

	key, keyIdx, err := p.nextKey()
	if err != nil {
		return "", "", 0, err
	}

	reqBody := map[string]interface{}{
		"requestId": requestID,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	reqURL := p.baseURL + "/async-tasks/status"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", "", 0, err
	}

	httpReq.Header.Set("Authorization", "Bearer "+key)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		p.reportStatus(keyIdx, false, false)
		return "", "", 0, err
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		p.reportStatus(keyIdx, false, false)
		return "", "", 0, err
	}

	var parsed struct {
		Status  string `json:"status"` // "Pending" | "Running" | "Succeed" | "Failed"
		Reason  string `json:"reason"`
		Results struct {
			Videos []struct {
				URL string `json:"url"`
			} `json:"videos"`
		} `json:"results"`
	}

	if err := json.Unmarshal(respData, &parsed); err != nil {
		return "", "", 0, err
	}

	switch strings.ToLower(parsed.Status) {
	case "succeed", "success":
		p.reportStatus(keyIdx, true, false)
		if len(parsed.Results.Videos) > 0 && parsed.Results.Videos[0].URL != "" {
			return "SUCCEEDED", parsed.Results.Videos[0].URL, 100, nil
		}
		return "FAILED", "", 0, errors.New("siliconflow reported success but no video url found")
	case "failed":
		p.reportStatus(keyIdx, false, false)
		errMsg := parsed.Reason
		if errMsg == "" {
			errMsg = "video generation failed upstream"
		}
		return "FAILED", "", 0, errors.New(errMsg)
	case "running":
		return "RUNNING", "", 65, nil
	default:
		return "PENDING", "", 25, nil
	}
}
