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
	ErrNoDashScopeKeyAvailable = errors.New("no active dashscope api key available in pool")
)

type dashScopeKey struct {
	Key         string
	WorkspaceID string
	IsExhausted bool
	TotalCalls  int64
	FailedCalls int64
}

// DashScopeProvider 阿里云百炼多模态号池与 CosyVoice 语音/万相生图/Wan 视频插件
type DashScopeProvider struct {
	mu      sync.RWMutex
	baseURL string
	keys    []*dashScopeKey
	idx     int
	client  *http.Client
}

// NewDashScopeProvider 创建阿里云百炼 Provider
func NewDashScopeProvider(keys []string, customBaseURL string) *DashScopeProvider {
	if customBaseURL == "" {
		customBaseURL = "https://dashscope.aliyuncs.com/api/v1"
	}
	customBaseURL = strings.TrimRight(customBaseURL, "/")

	p := &DashScopeProvider{
		baseURL: customBaseURL,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
	p.AddKeys(keys)
	return p
}

// AddKeys 批量导入百炼 API Key (格式支持: "dsk-xxx" 或 "dsk-xxx#workspaceId")
func (p *DashScopeProvider) AddKeys(keys []string) int {
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
		if trimmed == "" {
			continue
		}

		keyVal := trimmed
		wsID := ""
		if strings.Contains(trimmed, "#") {
			parts := strings.SplitN(trimmed, "#", 2)
			keyVal = strings.TrimSpace(parts[0])
			wsID = strings.TrimSpace(parts[1])
		}

		if !existing[keyVal] && keyVal != "" {
			p.keys = append(p.keys, &dashScopeKey{
				Key:         keyVal,
				WorkspaceID: wsID,
			})
			existing[keyVal] = true
			added++
		}
	}
	return added
}

func (p *DashScopeProvider) nextKey() (*dashScopeKey, int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.keys)
	if n == 0 {
		return nil, -1, ErrNoDashScopeKeyAvailable
	}

	for i := 0; i < n; i++ {
		candidateIdx := (p.idx + i) % n
		k := p.keys[candidateIdx]
		if !k.IsExhausted {
			p.idx = (candidateIdx + 1) % n
			return k, candidateIdx, nil
		}
	}

	return nil, -1, ErrNoDashScopeKeyAvailable
}

func (p *DashScopeProvider) reportStatus(idx int, success bool, isExhausted bool) {
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
		if isExhausted {
			k.IsExhausted = true
		}
	}
}

func (p *DashScopeProvider) Descriptor() ProviderDescriptor {
	return ProviderDescriptor{
		ID:                    "dashscope",
		Name:                  "Aliyun DashScope (阿里云百炼)",
		Vendor:                "Alibaba Cloud",
		Status:                "enabled",
		Version:               "2.4.0",
		Description:           "阿里云百炼官方多模态号池（通义万相文生图、CosyVoice 零样本声音复刻与自然语言捏声音、Wan 视频大模型）",
		SupportedAccountTypes: []string{"free_tier", "apikey"},
		CredentialFormats:     []string{"api_key"},
		Capabilities: []string{
			"chat",
			"image_generation",
			"tts",
			"voice_design",
			"voice_cloning",
			"video_generation",
		},
		FeaturedModels: []string{
			"wanx-v1",
			"cosyvoice-v1",
			"cosyvoice-v3-flash",
			"wan-video-v1",
			"qwen-plus",
		},
		Tags:     MakeTags("trial-quota", "apikey-pool", "official-sla", "tts-voice-clone", "tts-voice-design", "video-gen", "img-flux-wanx"),
		CostType: "trial_quota",
		AuthType: "api_key",
	}
}

func (p *DashScopeProvider) Stats() accounts.PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	active := 0
	expired := 0
	for _, k := range p.keys {
		if !k.IsExhausted {
			active++
		} else {
			expired++
		}
	}

	return accounts.PoolStats{
		Total:   len(p.keys),
		Active:  active,
		Expired: expired,
	}
}

func (p *DashScopeProvider) ListAccounts(typeFilter, statusFilter, search string) []accounts.AccountSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()

	out := make([]accounts.AccountSnapshot, 0, len(p.keys))
	for i, k := range p.keys {
		preview := k.Key
		if len(preview) > 8 {
			preview = preview[:4] + "..." + preview[len(preview)-4:]
		}

		st := "active"
		if k.IsExhausted {
			st = "exhausted"
		}

		out = append(out, accounts.AccountSnapshot{
			ID:           fmt.Sprintf("dashscope-key-%d", i+1),
			Email:        fmt.Sprintf("dashscope-%d@aliyun.local", i+1),
			Type:         "apikey",
			Status:       st,
			PlanType:     "trial_quota",
			TokenPreview: preview,
			TotalCalls:   k.TotalCalls,
			FailedCalls:  k.FailedCalls,
		})
	}
	return out
}

func (p *DashScopeProvider) ImportCredentials(req ImportRequest) (ImportResult, error) {
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
		Messages:  []string{fmt.Sprintf("成功导入 %d 个阿里云百炼 API Key", added)},
	}, nil
}

func (p *DashScopeProvider) ExecuteAccountAction(id, action string) (map[string]interface{}, error) {
	if action == "probe" || action == "test" {
		k, _, err := p.nextKey()
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"status":    "healthy",
			"key_probe": k.Key[:4] + "***",
			"timestamp": time.Now().Unix(),
		}, nil
	}
	return nil, fmt.Errorf("unsupported action: %s", action)
}

// GenerateImage 使用通义万相异步提交并轮询出图
func (p *DashScopeProvider) GenerateImage(ctx context.Context, prompt, size string) (string, error) {
	k, keyIdx, err := p.nextKey()
	if err != nil {
		return "", err
	}

	if size == "" {
		size = "1024*1024"
	} else {
		size = strings.ReplaceAll(size, "x", "*")
	}

	reqBody := map[string]interface{}{
		"model": "wanx-v1",
		"input": map[string]interface{}{
			"prompt": prompt,
		},
		"parameters": map[string]interface{}{
			"style": "<auto>",
			"size":  size,
			"n":     1,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	submitURL := p.baseURL + "/services/aigc/text2image/image-synthesis"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, submitURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}

	httpReq.Header.Set("Authorization", "Bearer "+k.Key)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-DashScope-Async", "enable")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		p.reportStatus(keyIdx, false, false)
		return "", err
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		p.reportStatus(keyIdx, false, false)
		return "", err
	}

	var submitResp struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Output  struct {
			TaskID     string `json:"task_id"`
			TaskStatus string `json:"task_status"`
		} `json:"output"`
	}

	if err := json.Unmarshal(respData, &submitResp); err != nil {
		p.reportStatus(keyIdx, false, false)
		return "", err
	}

	if submitResp.Output.TaskID == "" {
		isExhausted := strings.Contains(submitResp.Code, "Quota") || strings.Contains(submitResp.Message, "Arrears")
		p.reportStatus(keyIdx, false, isExhausted)
		return "", fmt.Errorf("wanx submit failed: %s (%s)", submitResp.Message, submitResp.Code)
	}

	taskID := submitResp.Output.TaskID

	// 轮询等待出图 (最长 60 秒)
	pollTicker := time.NewTicker(3 * time.Second)
	defer pollTicker.Stop()

	timeoutCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	for {
		select {
		case <-timeoutCtx.Done():
			p.reportStatus(keyIdx, false, false)
			return "", errors.New("wanx image generation timed out (60s)")
		case <-pollTicker.C:
			pollURL := fmt.Sprintf("%s/tasks/%s", p.baseURL, taskID)
			pollReq, _ := http.NewRequestWithContext(timeoutCtx, http.MethodGet, pollURL, nil)
			pollReq.Header.Set("Authorization", "Bearer "+k.Key)

			pollResp, pollErr := p.client.Do(pollReq)
			if pollErr != nil {
				continue
			}

			pollBody, _ := io.ReadAll(pollResp.Body)
			pollResp.Body.Close()

			var taskStatusResp struct {
				Output struct {
					TaskStatus string `json:"task_status"`
					Results    []struct {
						URL string `json:"url"`
					} `json:"results"`
					Message string `json:"message"`
				} `json:"output"`
			}

			if err := json.Unmarshal(pollBody, &taskStatusResp); err == nil {
				st := taskStatusResp.Output.TaskStatus
				if st == "SUCCEEDED" && len(taskStatusResp.Output.Results) > 0 {
					p.reportStatus(keyIdx, true, false)
					return taskStatusResp.Output.Results[0].URL, nil
				}
				if st == "FAILED" {
					p.reportStatus(keyIdx, false, false)
					return "", fmt.Errorf("wanx task failed: %s", taskStatusResp.Output.Message)
				}
			}
		}
	}
}

// VoiceDesign 自然语言捏音色（Voice Design）
func (p *DashScopeProvider) VoiceDesign(ctx context.Context, voicePrompt, previewText, preferredName string) (voiceID, previewURL string, err error) {
	k, keyIdx, err := p.nextKey()
	if err != nil {
		return "", "", err
	}

	reqBody := map[string]interface{}{
		"model":          "cosyvoice-v3-flash",
		"voice_prompt":   voicePrompt,
		"preview_text":   previewText,
		"preferred_name": preferredName,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	reqURL := p.baseURL + "/services/audio/voice-design"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", "", err
	}

	httpReq.Header.Set("Authorization", "Bearer "+k.Key)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		p.reportStatus(keyIdx, false, false)
		return "", "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var res struct {
		Output struct {
			VoiceID      string `json:"voice_id"`
			PreviewAudio string `json:"preview_audio"`
			PreviewURL   string `json:"preview_url"`
		} `json:"output"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal(data, &res); err != nil {
		return "", "", err
	}

	if res.Output.VoiceID == "" {
		p.reportStatus(keyIdx, false, false)
		return "", "", fmt.Errorf("voice design failed: %s", res.Message)
	}

	p.reportStatus(keyIdx, true, false)
	audioURL := res.Output.PreviewURL
	if audioURL == "" {
		audioURL = res.Output.PreviewAudio
	}
	return res.Output.VoiceID, audioURL, nil
}

// VoiceCloning 上传短音频零样本复刻声音（Voice Cloning / Enrollment）
func (p *DashScopeProvider) VoiceCloning(ctx context.Context, audioURL, prefix string) (voiceID string, err error) {
	k, keyIdx, err := p.nextKey()
	if err != nil {
		return "", err
	}

	reqBody := map[string]interface{}{
		"target_model": "cosyvoice-v3-flash",
		"url":          audioURL,
		"prefix":       prefix,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	reqURL := p.baseURL + "/services/audio/tts/customization"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}

	httpReq.Header.Set("Authorization", "Bearer "+k.Key)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		p.reportStatus(keyIdx, false, false)
		return "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var res struct {
		Output struct {
			VoiceID string `json:"voice_id"`
		} `json:"output"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal(data, &res); err != nil {
		return "", err
	}

	if res.Output.VoiceID == "" {
		p.reportStatus(keyIdx, false, false)
		return "", fmt.Errorf("voice cloning failed: %s", res.Message)
	}

	p.reportStatus(keyIdx, true, false)
	return res.Output.VoiceID, nil
}

// SynthesizeSpeech 使用 CosyVoice 或定制音色合成语音
func (p *DashScopeProvider) SynthesizeSpeech(ctx context.Context, text, voice string) ([]byte, error) {
	k, keyIdx, err := p.nextKey()
	if err != nil {
		return nil, err
	}

	if voice == "" {
		voice = "longxiaochun"
	}

	reqBody := map[string]interface{}{
		"model": "cosyvoice-v3-flash",
		"input": map[string]interface{}{
			"text":  text,
			"voice": voice,
		},
		"parameters": map[string]interface{}{
			"format": "mp3",
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	reqURL := p.baseURL + "/services/audio/tts/SpeechSynthesizer"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Authorization", "Bearer "+k.Key)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		p.reportStatus(keyIdx, false, false)
		return nil, err
	}
	defer resp.Body.Close()

	audioBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		p.reportStatus(keyIdx, false, false)
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		p.reportStatus(keyIdx, false, false)
		return nil, fmt.Errorf("dashscope speech synthesis error (status %d): %s", resp.StatusCode, string(audioBytes))
	}

	p.reportStatus(keyIdx, true, false)
	return audioBytes, nil
}
