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

	fhttp "github.com/bogdanfinn/fhttp"
	"github.com/bogdanfinn/websocket"
	"github.com/google/uuid"
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

// GetDashScopeVoices 返回阿里云百炼 CosyVoice 官方精选音色列表。
func GetDashScopeVoices(available bool, statusReason string) []VoiceDescriptor {
	presets := []struct {
		id     string
		name   string
		gender string
		locale string
		sample string
	}{
		{"longanyang", "百炼龙安洋 (V3 阳光男声 · CosyVoice3)", "male", "zh-CN", "你好，我是阿里云百炼龙安洋，支持流畅自然的情绪表达与多场景配音。"},
		{"longanhuan_v3", "百炼龙安欢 (V3 元气女声 · 多方言)", "female", "zh-CN", "嗨！我是龙安欢，支持多种生动语气与方言切换，让配音更有感染力。"},
		{"longxiaochun", "百炼龙小春 (知性女声 · CosyVoice)", "female", "zh-CN", "你好，我是阿里云百炼龙小春，适合高质量女声旁白与解说。"},
		{"longxiaoxia", "百炼龙小夏 (温柔女声 · CosyVoice)", "female", "zh-CN", "微风拂过窗边，我是龙小夏，为你读一段温柔的对白。"},
		{"longlaotie", "百炼龙老铁 (幽默男声 · 东北特色)", "male", "zh-CN", "老铁没毛病，这段解说交给我龙老铁包你满意！"},
		{"longshu", "百炼龙书 (儒雅评书 · 男声)", "male", "zh-CN", "话说天下大势，且听我龙书为你细细道来。"},
		{"longshuo", "百炼龙硕 (阳光男声 · CosyVoice)", "male", "zh-CN", "大家好，我是龙硕，支持短剧配音与多情感对话。"},
	}
	out := make([]VoiceDescriptor, 0, len(presets))
	for _, item := range presets {
		out = append(out, VoiceDescriptor{
			ID:           item.id,
			Name:         item.name,
			Provider:     "dashscope",
			Category:     "百炼精品",
			Gender:       item.gender,
			Locale:       item.locale,
			SampleText:   item.sample,
			Tags:         []string{"阿里云百炼", "CosyVoice", "声音复刻"},
			Available:    available,
			StatusReason: statusReason,
		})
	}
	return out
}

// IsDashScopeVoice 判断音色 ID 是否属于百炼 CosyVoice 音色或定制复刻音色。
func IsDashScopeVoice(voice string) bool {
	lower := strings.ToLower(strings.TrimSpace(voice))
	return strings.HasPrefix(lower, "dashscope:") ||
		strings.HasPrefix(lower, "long") ||
		strings.HasPrefix(lower, "cosyvoice-")
}

func resolveDashScopeTTSModel(voice string) string {
	lower := strings.ToLower(strings.TrimSpace(voice))
	switch {
	case strings.Contains(lower, "cosyvoice-v2"), strings.HasSuffix(lower, "_v2"):
		return "cosyvoice-v2"
	case strings.Contains(lower, "cosyvoice-v1"):
		return "cosyvoice-v1"
	case strings.Contains(lower, "cosyvoice-v3"),
		strings.HasSuffix(lower, "_v3"),
		strings.HasPrefix(lower, "longanyang"),
		strings.HasPrefix(lower, "longanhuan"),
		strings.HasPrefix(lower, "longhuhu"),
		strings.HasPrefix(lower, "longpaopao"),
		strings.HasPrefix(lower, "longjielidou"),
		strings.HasPrefix(lower, "designed_"),
		strings.HasPrefix(lower, "custom_"):
		return "cosyvoice-v3-flash"
	default:
		return "cosyvoice-v1"
	}
}

func resolveDashScopeWSEndpoint(baseURL string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(baseURL))
	switch {
	case strings.HasPrefix(lower, "ws://"), strings.HasPrefix(lower, "wss://"):
		return strings.TrimSpace(baseURL), true
	case strings.Contains(lower, "dashscope-intl.aliyuncs.com"):
		return "wss://dashscope-intl.aliyuncs.com/api-ws/v1/inference", true
	case strings.Contains(lower, "dashscope.aliyuncs.com"):
		return "wss://dashscope.aliyuncs.com/api-ws/v1/inference", true
	default:
		return "", false
	}
}

// SynthesizeSpeech 使用 CosyVoice 或定制音色合成语音（默认语速 1.0）。
func (p *DashScopeProvider) SynthesizeSpeech(ctx context.Context, text, voice string) ([]byte, error) {
	return p.SynthesizeSpeechWithRate(ctx, text, voice, 1.0)
}

// SynthesizeSpeechWithRate 使用百炼 CosyVoice（WebSocket 实时流或 REST 兜底）合成 MP3 音频并支持语速控制。
func (p *DashScopeProvider) SynthesizeSpeechWithRate(ctx context.Context, text, voice string, rate float64) ([]byte, error) {
	k, keyIdx, err := p.nextKey()
	if err != nil {
		return nil, err
	}

	voice = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(voice, "dashscope:"), "DashScope:"))
	if voice == "" {
		voice = "longanyang"
	}
	if rate <= 0 {
		rate = 1.0
	} else if rate < 0.5 {
		rate = 0.5
	} else if rate > 2.0 {
		rate = 2.0
	}

	model := resolveDashScopeTTSModel(voice)
	if wsURL, useWS := resolveDashScopeWSEndpoint(p.baseURL); useWS {
		audioBytes, wsErr := p.synthesizeSpeechWS(ctx, wsURL, k, model, text, voice, rate)
		if wsErr == nil && len(audioBytes) > 0 {
			p.reportStatus(keyIdx, true, false)
			return audioBytes, nil
		}
		// 若 WebSocket 握手被代理阻断，回退尝试 HTTP REST（非官方 custom endpoint 则直接走 REST）
		if !strings.HasPrefix(strings.ToLower(p.baseURL), "http") {
			p.reportStatus(keyIdx, false, isDashScopeQuotaError(wsErr))
			return nil, wsErr
		}
		if httpAudio, httpErr := p.synthesizeSpeechHTTP(ctx, k, model, text, voice, rate); httpErr == nil && len(httpAudio) > 0 {
			p.reportStatus(keyIdx, true, false)
			return httpAudio, nil
		}
		p.reportStatus(keyIdx, false, isDashScopeQuotaError(wsErr))
		return nil, wsErr
	}

	audioBytes, httpErr := p.synthesizeSpeechHTTP(ctx, k, model, text, voice, rate)
	if httpErr != nil {
		p.reportStatus(keyIdx, false, isDashScopeQuotaError(httpErr))
		return nil, httpErr
	}
	p.reportStatus(keyIdx, true, false)
	return audioBytes, nil
}

func isDashScopeQuotaError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "arrears") ||
		strings.Contains(lower, "quota") ||
		strings.Contains(lower, "exhausted") ||
		strings.Contains(lower, "insufficient")
}

func (p *DashScopeProvider) synthesizeSpeechWS(ctx context.Context, wsURL string, k *dashScopeKey, model, text, voice string, rate float64) ([]byte, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		Proxy:            fhttp.ProxyFromEnvironment,
	}
	header := fhttp.Header{}
	header.Set("Authorization", "Bearer "+k.Key)
	if k.WorkspaceID != "" {
		header.Set("X-DashScope-WorkSpace", k.WorkspaceID)
	}

	conn, resp, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		return nil, fmt.Errorf("dashscope ws connect failed (status=%d): %w", status, err)
	}
	defer conn.Close()
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancel()

	taskID := uuid.New().String()
	runTask := map[string]interface{}{
		"header": map[string]interface{}{
			"action":    "run-task",
			"task_id":   taskID,
			"streaming": "duplex",
		},
		"payload": map[string]interface{}{
			"task_group": "audio",
			"task":       "tts",
			"function":   "SpeechSynthesizer",
			"model":      model,
			"input":      map[string]interface{}{},
			"parameters": map[string]interface{}{
				"text_type":   "PlainText",
				"voice":       voice,
				"format":      "mp3",
				"sample_rate": 22050,
				"volume":      50,
				"rate":        rate,
				"pitch":       1.0,
			},
		},
	}
	if err := conn.WriteJSON(runTask); err != nil {
		return nil, fmt.Errorf("dashscope ws run-task failed: %w", err)
	}

	var audioBuf bytes.Buffer
	sentContinue := false
	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("dashscope ws read failed: %w", err)
		}
		if msgType == websocket.BinaryMessage {
			audioBuf.Write(msg)
			continue
		}
		var ev struct {
			Header struct {
				Event        string `json:"event"`
				ErrorCode    string `json:"error_code"`
				ErrorMessage string `json:"error_message"`
			} `json:"header"`
		}
		if err := json.Unmarshal(msg, &ev); err != nil {
			continue
		}
		switch ev.Header.Event {
		case "task-started":
			if !sentContinue {
				sentContinue = true
				continueMsg := map[string]interface{}{
					"header": map[string]interface{}{
						"action":    "continue-task",
						"task_id":   taskID,
						"streaming": "duplex",
					},
					"payload": map[string]interface{}{
						"input": map[string]interface{}{
							"text": text,
						},
					},
				}
				if err := conn.WriteJSON(continueMsg); err != nil {
					return nil, fmt.Errorf("dashscope ws continue-task failed: %w", err)
				}
				finishMsg := map[string]interface{}{
					"header": map[string]interface{}{
						"action":    "finish-task",
						"task_id":   taskID,
						"streaming": "duplex",
					},
					"payload": map[string]interface{}{},
				}
				if err := conn.WriteJSON(finishMsg); err != nil {
					return nil, fmt.Errorf("dashscope ws finish-task failed: %w", err)
				}
			}
		case "task-finished":
			if audioBuf.Len() == 0 {
				return nil, errors.New("dashscope ws finished without audio output")
			}
			return audioBuf.Bytes(), nil
		case "task-failed":
			return nil, fmt.Errorf("dashscope ws task failed (%s): %s", ev.Header.ErrorCode, ev.Header.ErrorMessage)
		}
	}
}

func (p *DashScopeProvider) synthesizeSpeechHTTP(ctx context.Context, k *dashScopeKey, model, text, voice string, rate float64) ([]byte, error) {
	reqBody := map[string]interface{}{
		"model": model,
		"input": map[string]interface{}{
			"text":   text,
			"voice":  voice,
			"format": "mp3",
			"rate":   rate,
		},
		"parameters": map[string]interface{}{
			"format": "mp3",
			"rate":   rate,
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	reqURL := strings.TrimRight(p.baseURL, "/") + "/services/audio/tts/SpeechSynthesizer"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+k.Key)
	httpReq.Header.Set("Content-Type", "application/json")
	if k.WorkspaceID != "" {
		httpReq.Header.Set("X-DashScope-WorkSpace", k.WorkspaceID)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 25*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dashscope speech synthesis error (status %d): %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	ctype := strings.ToLower(resp.Header.Get("Content-Type"))
	trimmed := bytes.TrimSpace(respBytes)
	if strings.Contains(ctype, "application/json") || (len(trimmed) > 0 && trimmed[0] == '{') {
		var parsed struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Output  struct {
				Audio struct {
					URL string `json:"url"`
				} `json:"audio"`
			} `json:"output"`
		}
		if err := json.Unmarshal(trimmed, &parsed); err != nil {
			return nil, fmt.Errorf("dashscope json parse error: %w", err)
		}
		audioURL := strings.TrimSpace(parsed.Output.Audio.URL)
		if audioURL == "" {
			if parsed.Message != "" {
				return nil, fmt.Errorf("dashscope speech error (%s): %s", parsed.Code, parsed.Message)
			}
			return nil, errors.New("dashscope response missing output.audio.url")
		}
		dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, audioURL, nil)
		if err != nil {
			return nil, err
		}
		dlResp, err := p.client.Do(dlReq)
		if err != nil {
			return nil, err
		}
		defer dlResp.Body.Close()
		audioData, err := io.ReadAll(io.LimitReader(dlResp.Body, 25*1024*1024))
		if err != nil {
			return nil, err
		}
		if dlResp.StatusCode != http.StatusOK || len(audioData) == 0 {
			return nil, fmt.Errorf("dashscope download audio failed (status %d)", dlResp.StatusCode)
		}
		return audioData, nil
	}

	if len(respBytes) == 0 {
		return nil, errors.New("dashscope returned empty audio body")
	}
	return respBytes, nil
}

// SubmitVideoTask 提交阿里云百炼通义万相异步视频生成任务（支持文生视频与首帧图生视频）
func (p *DashScopeProvider) SubmitVideoTask(ctx context.Context, model, prompt, imageURL string, duration int) (string, error) {
	k, keyIdx, err := p.nextKey()
	if err != nil {
		return "", err
	}

	targetModel := strings.TrimSpace(model)
	targetModel = strings.TrimPrefix(targetModel, "dashscope/")
	lower := strings.ToLower(targetModel)
	if lower == "" || lower == "wan-video-v1" || lower == "wan-video" || lower == "wanx-video" {
		if strings.TrimSpace(imageURL) != "" {
			targetModel = "wanx2.1-i2v-turbo"
		} else {
			targetModel = "wanx2.1-t2v-turbo"
		}
	}

	input := map[string]interface{}{
		"prompt": prompt,
	}
	if img := strings.TrimSpace(imageURL); img != "" {
		input["img_url"] = img
	}

	params := map[string]interface{}{
		"prompt_extend": true,
	}
	if duration > 0 {
		params["duration"] = duration
	}

	reqBody := map[string]interface{}{
		"model":      targetModel,
		"input":      input,
		"parameters": params,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	reqURL := p.baseURL + "/services/aigc/video-generation/video-synthesis"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+k.Key)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-DashScope-Async", "enable")
	if k.WorkspaceID != "" {
		httpReq.Header.Set("X-DashScope-WorkSpace", k.WorkspaceID)
	}

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
		Output struct {
			TaskID     string `json:"task_id"`
			TaskStatus string `json:"task_status"`
		} `json:"output"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(respData, &submitResp); err != nil {
		p.reportStatus(keyIdx, false, false)
		return "", fmt.Errorf("dashscope video submit decode error (status %d): %w", resp.StatusCode, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 || submitResp.Output.TaskID == "" {
		isExhausted := strings.Contains(submitResp.Code, "Quota") || strings.Contains(submitResp.Message, "Arrears")
		p.reportStatus(keyIdx, false, isExhausted)
		return "", fmt.Errorf("dashscope video submit error (status %d, code=%s): %s", resp.StatusCode, submitResp.Code, submitResp.Message)
	}

	p.reportStatus(keyIdx, true, false)
	return submitResp.Output.TaskID, nil
}

// QueryVideoTask 轮询阿里云百炼通义万相异步视频生成任务状态与成片 URL
func (p *DashScopeProvider) QueryVideoTask(ctx context.Context, taskID string) (string, string, int, error) {
	k, keyIdx, err := p.nextKey()
	if err != nil {
		return "", "", 0, err
	}

	pollURL := fmt.Sprintf("%s/tasks/%s", p.baseURL, strings.TrimSpace(taskID))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
	if err != nil {
		return "", "", 0, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+k.Key)
	if k.WorkspaceID != "" {
		httpReq.Header.Set("X-DashScope-WorkSpace", k.WorkspaceID)
	}

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

	var taskResp struct {
		Output struct {
			TaskStatus string `json:"task_status"`
			VideoURL   string `json:"video_url"`
			URL        string `json:"url"`
			Message    string `json:"message"`
		} `json:"output"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(respData, &taskResp); err != nil {
		p.reportStatus(keyIdx, false, false)
		return "", "", 0, fmt.Errorf("dashscope video query decode error (status %d): %w", resp.StatusCode, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		p.reportStatus(keyIdx, false, false)
		return "FAILED", "", 0, fmt.Errorf("dashscope video query error (status %d, code=%s): %s", resp.StatusCode, taskResp.Code, taskResp.Message)
	}

	p.reportStatus(keyIdx, true, false)
	status := strings.ToUpper(strings.TrimSpace(taskResp.Output.TaskStatus))
	videoURL := strings.TrimSpace(taskResp.Output.VideoURL)
	if videoURL == "" {
		videoURL = strings.TrimSpace(taskResp.Output.URL)
	}

	switch status {
	case "SUCCEEDED":
		return "SUCCEEDED", videoURL, 100, nil
	case "FAILED", "CANCELED":
		msg := taskResp.Output.Message
		if msg == "" {
			msg = taskResp.Message
		}
		if msg != "" {
			return "FAILED", "", 0, fmt.Errorf("dashscope video task failed: %s", msg)
		}
		return "FAILED", "", 0, nil
	case "RUNNING":
		return "RUNNING", "", 55, nil
	default:
		return "PENDING", "", 20, nil
	}
}
