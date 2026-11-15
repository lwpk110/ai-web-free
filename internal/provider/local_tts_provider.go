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

const (
	defaultLocalTTSModel   = "kokoro"
	localTTSProbeCacheTTL  = 30 * time.Second
	localTTSProbeTimeout   = 3 * time.Second
	localTTSRequestTimeout = 90 * time.Second
)

// LocalTTSVoice 描述本地/自托管 OpenAI 兼容 TTS 音色（如 Kokoro-FastAPI）。
type LocalTTSVoice struct {
	VoiceID     string
	DisplayName string
	Gender      string
	Locale      string
	Category    string
	SampleText  string
}

// BuiltinLocalKokoroVoices 预置本地 Kokoro-v1.1-zh 与常用多语种音色元信息。
var BuiltinLocalKokoroVoices = []LocalTTSVoice{
	{
		VoiceID:     "zf_xiaoxiao",
		DisplayName: "本地 Kokoro 晓晓 (温柔女声 · 中英双语)",
		Gender:      "female",
		Locale:      "zh-CN",
		Category:    "本地零延迟",
		SampleText:  "你好，这是运行在本地 Kokoro 引擎上的晓晓音色，不依赖外部网络。",
	},
	{
		VoiceID:     "zf_xiaobei",
		DisplayName: "本地 Kokoro 晓北 (知性女声 · 叙事)",
		Gender:      "female",
		Locale:      "zh-CN",
		Category:    "本地零延迟",
		SampleText:  "夜色沉静，让我们一起走进这段娓娓道来的本地有声故事。",
	},
	{
		VoiceID:     "zf_xiaoni",
		DisplayName: "本地 Kokoro 晓妮 (甜美灵动 · 对白)",
		Gender:      "female",
		Locale:      "zh-CN",
		Category:    "本地零延迟",
		SampleText:  "欢迎回到工作台，今天想要合成哪一段角色对白呢？",
	},
	{
		VoiceID:     "zf_xiaoyi",
		DisplayName: "本地 Kokoro 晓伊 (清脆明快 · 播报)",
		Gender:      "female",
		Locale:      "zh-CN",
		Category:    "本地零延迟",
		SampleText:  "本地语音引擎状态正常，长文本分句与批量合成已就绪。",
	},
	{
		VoiceID:     "zm_yunxi",
		DisplayName: "本地 Kokoro 云希 (沉稳男声 · 故事旁白)",
		Gender:      "male",
		Locale:      "zh-CN",
		Category:    "本地零延迟",
		SampleText:  "在所有外部链路都波动的时候，本地离线推理依然保持毫秒级稳定输出。",
	},
	{
		VoiceID:     "zm_yunjian",
		DisplayName: "本地 Kokoro 云健 (热血青年 · 影视解说)",
		Gender:      "male",
		Locale:      "zh-CN",
		Category:    "本地零延迟",
		SampleText:  "注意看，这一步操作将整个短剧配音流水线直接搬进了本地节点！",
	},
	{
		VoiceID:     "zm_yunxia",
		DisplayName: "本地 Kokoro 云夏 (阳光活力 · 对白)",
		Gender:      "male",
		Locale:      "zh-CN",
		Category:    "本地零延迟",
		SampleText:  "全速推进，今天我们要把十集短剧配音一次性渲染完成。",
	},
	{
		VoiceID:     "zm_yunyang",
		DisplayName: "本地 Kokoro 云扬 (新闻播报 · 权威)",
		Gender:      "male",
		Locale:      "zh-CN",
		Category:    "本地零延迟",
		SampleText:  "本地语音集群已完成初始化，所有节点均可离线调用。",
	},
	{
		VoiceID:     "af_heart",
		DisplayName: "Kokoro Heart (美式女声 · Warm)",
		Gender:      "female",
		Locale:      "en-US",
		Category:    "多语种精选",
		SampleText:  "Hello, this is Kokoro running locally with OpenAI-compatible speech output.",
	},
	{
		VoiceID:     "af_bella",
		DisplayName: "Kokoro Bella (美式女声 · Expressive)",
		Gender:      "female",
		Locale:      "en-US",
		Category:    "多语种精选",
		SampleText:  "Every voice sample can be cached locally for instant offline playback.",
	},
	{
		VoiceID:     "am_adam",
		DisplayName: "Kokoro Adam (美式男声 · Deep)",
		Gender:      "male",
		Locale:      "en-US",
		Category:    "多语种精选",
		SampleText:  "Local synthesis ensures consistent character voices across every episode.",
	},
	{
		VoiceID:     "am_michael",
		DisplayName: "Kokoro Michael (美式男声 · Narrator)",
		Gender:      "male",
		Locale:      "en-US",
		Category:    "多语种精选",
		SampleText:  "Switching smoothly to local inference when cloud endpoints rate-limit.",
	},
}

// LocalTTSProvider 对接自托管 OpenAI 兼容本地 TTS 服务（例如 Kokoro-FastAPI / OpenedAI-Speech）。
type LocalTTSProvider struct {
	mu            sync.RWMutex
	baseURL       string
	apiKey        string
	defaultModel  string
	httpClient    *http.Client
	healthy       bool
	lastErr       string
	lastCheckedAt time.Time
	discovered    []VoiceDescriptor
}

// NewLocalTTSProvider 创建本地/自托管 OpenAI 兼容 TTS 供应商插件。
func NewLocalTTSProvider(baseURL, apiKey, defaultModel string) *LocalTTSProvider {
	p := &LocalTTSProvider{
		httpClient: &http.Client{Timeout: localTTSRequestTimeout},
	}
	p.Configure(baseURL, apiKey, defaultModel)
	return p
}

// Configure 更新本地 TTS 接入地址、可选密钥与默认模型名。
func (p *LocalTTSProvider) Configure(baseURL, apiKey, defaultModel string) {
	trimmedBase := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	trimmedKey := strings.TrimSpace(apiKey)
	resolvedModel := strings.TrimSpace(defaultModel)
	if resolvedModel == "" {
		resolvedModel = defaultLocalTTSModel
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	endpointChanged := p.baseURL != trimmedBase
	p.baseURL = trimmedBase
	p.apiKey = trimmedKey
	p.defaultModel = resolvedModel
	if endpointChanged {
		p.discovered = nil
		p.lastCheckedAt = time.Time{}
	}
	if trimmedBase == "" {
		p.healthy = false
		p.lastErr = "未配置自定义 OpenAI 兼容 TTS 地址（可选设置 CUSTOM_TTS_BASE_URL / LOCAL_TTS_BASE_URL）"
	} else if endpointChanged || p.lastCheckedAt.IsZero() {
		p.healthy = true
		p.lastErr = ""
	}
}

func (p *LocalTTSProvider) Descriptor() ProviderDescriptor {
	return ProviderDescriptor{
		ID:                    "local_tts",
		Name:                  "OpenAI 兼容云端 / 自定义 TTS",
		Vendor:                "Custom-OpenAI-TTS",
		Status:                "enabled",
		Version:               "1.0.0",
		Description:           "可选外部云端 / 中继 OpenAI 兼容语音端点（配置 CUSTOM_TTS_BASE_URL 即生效，无需本地硬件部署）",
		SupportedAccountTypes: []string{"no_auth", "api_key"},
		CredentialFormats:     []string{"endpoint_url"},
		Capabilities:          []string{"tts"},
		FeaturedModels:        []string{"tts-1", "kokoro"},
		CostType:              "permanent_free",
		AuthType:              "no_auth",
		Tags: []ChannelTag{
			{Key: "custom-tts", Label: "自定义云端", Category: "reliability", Variant: "success", Icon: "Cloud", Tooltip: "外部 OpenAI 兼容 /v1/audio/speech 语音接口"},
			{Key: "permanent-free", Label: "灵活兼容", Category: "cost", Variant: "cyan", Icon: "Sparkles", Tooltip: "兼容标准 OpenAI TTS 与 Kokoro 接口"},
			{Key: "tts-voice", Label: "扩展音色", Category: "feature", Variant: "default", Icon: "AudioLines", Tooltip: "按端点动态发现可用音色"},
		},
	}
}

func (p *LocalTTSProvider) Stats() accounts.PoolStats {
	p.mu.RLock()
	configured := p.baseURL != ""
	healthy := p.healthy
	p.mu.RUnlock()

	if configured && healthy {
		return accounts.PoolStats{Total: 1, Active: 1, NoAuthCount: 1}
	}
	if configured {
		return accounts.PoolStats{Total: 1, Active: 0, Disabled: 1}
	}
	return accounts.PoolStats{Total: 0, Active: 0}
}

func (p *LocalTTSProvider) IsConfigured() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.baseURL != ""
}

func (p *LocalTTSProvider) IsAvailable() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.baseURL != "" && p.healthy
}

func (p *LocalTTSProvider) Endpoint() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.baseURL
}

func (p *LocalTTSProvider) HealthSnapshot() (bool, string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.healthy, p.lastErr
}

func (p *LocalTTSProvider) ListAccounts(typeFilter, statusFilter, search string) []accounts.AccountSnapshot {
	p.mu.RLock()
	baseURL := p.baseURL
	healthy := p.healthy
	p.mu.RUnlock()

	if baseURL == "" {
		return nil
	}
	status := "active"
	preview := "local-tts:" + baseURL
	if !healthy {
		status = "cooldown"
	}
	if statusFilter != "" && statusFilter != status {
		return nil
	}
	if search != "" && !strings.Contains(strings.ToLower(baseURL), strings.ToLower(search)) {
		return nil
	}
	return []accounts.AccountSnapshot{
		{
			ID:           "local-tts-endpoint",
			Type:         "no_auth",
			Email:        baseURL,
			Status:       status,
			PlanType:     "permanent_free",
			TokenPreview: preview,
		},
	}
}

func (p *LocalTTSProvider) ImportCredentials(req ImportRequest) (ImportResult, error) {
	raw := strings.TrimSpace(req.RawText)
	if raw == "" && len(req.Tokens) > 0 {
		raw = strings.TrimSpace(req.Tokens[0])
	}
	if raw == "" {
		return ImportResult{}, errors.New("请提供自定义 OpenAI 兼容 TTS 地址，例如 https://api.openai.com/v1|sk-...|tts-1")
	}
	parts := strings.SplitN(raw, "|", 3)
	baseURL := strings.TrimSpace(parts[0])
	apiKey := ""
	model := ""
	if len(parts) > 1 {
		apiKey = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		model = strings.TrimSpace(parts[2])
	}
	p.Configure(baseURL, apiKey, model)
	ctx, cancel := context.WithTimeout(context.Background(), localTTSProbeTimeout)
	defer cancel()
	if err := p.Probe(ctx); err != nil {
		return ImportResult{Added: 1, TotalPool: 1, Messages: []string{"已保存地址，但连通探测失败: " + err.Error()}}, nil
	}
	return ImportResult{Added: 1, TotalPool: 1, Messages: []string{"自定义 TTS 端点配置并连通成功"}}, nil
}

func (p *LocalTTSProvider) ExecuteAccountAction(id, action string) (map[string]interface{}, error) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "probe", "check", "health", "sync":
		ctx, cancel := context.WithTimeout(context.Background(), localTTSProbeTimeout)
		defer cancel()
		err := p.Probe(ctx)
		voices := p.VoiceDescriptors()
		if err != nil {
			return map[string]interface{}{
				"status":      "unhealthy",
				"error":       err.Error(),
				"voice_count": len(voices),
			}, nil
		}
		return map[string]interface{}{
			"status":      "active",
			"endpoint":    p.Endpoint(),
			"voice_count": len(voices),
		}, nil
	default:
		return map[string]interface{}{"status": "ok", "action": action, "id": id}, nil
	}
}

// ProbeMaybeCached 在缓存 TTL 内跳过重复网络探测，适合状态展示端点调用。
func (p *LocalTTSProvider) ProbeMaybeCached(ctx context.Context) error {
	p.mu.RLock()
	configured := p.baseURL != ""
	lastChecked := p.lastCheckedAt
	lastErr := p.lastErr
	healthy := p.healthy
	p.mu.RUnlock()

	if !configured {
		return errors.New("LOCAL_TTS_BASE_URL 未配置")
	}
	if !lastChecked.IsZero() && time.Since(lastChecked) < localTTSProbeCacheTTL {
		if healthy {
			return nil
		}
		return errors.New(lastErr)
	}
	return p.Probe(ctx)
}

// Probe 探测本地 TTS 节点可达性并尽可能发现可用音色列表。
func (p *LocalTTSProvider) Probe(ctx context.Context) error {
	p.mu.RLock()
	baseURL := p.baseURL
	apiKey := p.apiKey
	p.mu.RUnlock()

	if baseURL == "" {
		p.markHealth(false, "未配置自定义 OpenAI 兼容 TTS 地址（可选设置 CUSTOM_TTS_BASE_URL / LOCAL_TTS_BASE_URL）")
		return errors.New("LOCAL_TTS_BASE_URL 未配置")
	}

	probeCtx, cancel := context.WithTimeout(ctx, localTTSProbeTimeout)
	defer cancel()

	// 1. 优先尝试 Kokoro-FastAPI 音色发现接口 /v1/audio/voices
	if discovered, statusCode, body, err := p.fetchVoicesEndpoint(probeCtx, baseURL, apiKey); err == nil {
		p.mu.Lock()
		if len(discovered) > 0 {
			p.discovered = discovered
		}
		p.healthy = true
		p.lastErr = ""
		p.lastCheckedAt = time.Now()
		p.mu.Unlock()
		return nil
	} else if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		msg := fmt.Sprintf("本地 TTS 鉴权失败 (%d): %s", statusCode, strings.TrimSpace(body))
		p.markHealth(false, msg)
		return errors.New(msg)
	} else if statusCode >= 500 {
		msg := fmt.Sprintf("本地 TTS 服务异常 (%d): %s", statusCode, strings.TrimSpace(body))
		p.markHealth(false, msg)
		return errors.New(msg)
	} else if err != nil && statusCode == 0 {
		// 连接层面彻底失败时无需继续探测 /models
		msg := fmt.Sprintf("本地 TTS 服务不可达 (%s): %v", baseURL, err)
		p.markHealth(false, msg)
		return errors.New(msg)
	}

	// 2. 兼容未实现 /audio/voices 的 OpenAI 兼容服务（检查 /v1/models 或 /health）
	for _, path := range []string{"/models", "/health"} {
		target := buildLocalTTSEndpoint(baseURL, path)
		if path == "/health" {
			target = strings.TrimSuffix(baseURL, "/v1") + "/health"
		}
		req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, target, nil)
		if err != nil {
			continue
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		resp, err := p.httpClient.Do(req)
		if err != nil {
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			p.markHealth(true, "")
			return nil
		}
	}

	msg := fmt.Sprintf("本地 TTS 探测未返回有效健康端点 (%s)", baseURL)
	p.markHealth(false, msg)
	return errors.New(msg)
}

func (p *LocalTTSProvider) markHealth(healthy bool, errMsg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.healthy = healthy
	p.lastErr = errMsg
	p.lastCheckedAt = time.Now()
}

func (p *LocalTTSProvider) fetchVoicesEndpoint(ctx context.Context, baseURL, apiKey string) ([]VoiceDescriptor, int, string, error) {
	target := buildLocalTTSEndpoint(baseURL, "/audio/voices")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, 0, "", err
	}
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, 0, "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, resp.StatusCode, "", err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, string(bodyBytes), fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	ids := parseLocalVoiceIDs(bodyBytes)
	if len(ids) == 0 {
		return nil, resp.StatusCode, string(bodyBytes), nil
	}

	builtinIndex := make(map[string]LocalTTSVoice, len(BuiltinLocalKokoroVoices))
	for _, b := range BuiltinLocalKokoroVoices {
		builtinIndex[b.VoiceID] = b
	}

	out := make([]VoiceDescriptor, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		if preset, ok := builtinIndex[id]; ok {
			out = append(out, VoiceDescriptor{
				ID:         preset.VoiceID,
				Name:       preset.DisplayName,
				Provider:   "local_tts",
				Category:   preset.Category,
				Gender:     preset.Gender,
				Locale:     preset.Locale,
				SampleText: preset.SampleText,
				Tags:       []string{"本地节点", "Kokoro", "零成本"},
				Available:  true,
			})
			continue
		}
		locale, gender, sample := inferLocalVoiceMetadata(id)
		out = append(out, VoiceDescriptor{
			ID:         id,
			Name:       fmt.Sprintf("本地音色 (%s)", id),
			Provider:   "local_tts",
			Category:   "本地扩展",
			Gender:     gender,
			Locale:     locale,
			SampleText: sample,
			Tags:       []string{"本地节点", "免密免费"},
			Available:  true,
		})
	}
	return out, resp.StatusCode, "", nil
}

// VoiceDescriptors 返回本地/自托管 TTS 音色列表及实时可用状态。
func (p *LocalTTSProvider) VoiceDescriptors() []VoiceDescriptor {
	p.mu.RLock()
	configured := p.baseURL != ""
	healthy := p.healthy
	lastErr := p.lastErr
	discovered := make([]VoiceDescriptor, len(p.discovered))
	copy(discovered, p.discovered)
	p.mu.RUnlock()

	available := configured && healthy
	statusReason := ""
	if !configured {
		statusReason = "未配置自定义 OpenAI 兼容 TTS 地址（可选设置 CUSTOM_TTS_BASE_URL / LOCAL_TTS_BASE_URL）"
	} else if !healthy {
		statusReason = lastErr
	}

	if len(discovered) > 0 {
		for i := range discovered {
			discovered[i].Available = available
			discovered[i].StatusReason = statusReason
		}
		return discovered
	}

	out := make([]VoiceDescriptor, 0, len(BuiltinLocalKokoroVoices))
	for _, v := range BuiltinLocalKokoroVoices {
		out = append(out, VoiceDescriptor{
			ID:           v.VoiceID,
			Name:         v.DisplayName,
			Provider:     "local_tts",
			Category:     v.Category,
			Gender:       v.Gender,
			Locale:       v.Locale,
			SampleText:   v.SampleText,
			Tags:         []string{"本地节点", "Kokoro", "零成本"},
			Available:    available,
			StatusReason: statusReason,
		})
	}
	return out
}

// IsKnownVoice 判断音色是否属于本地/Kokoro 音色或显式 local:/kokoro: 前缀。
func (p *LocalTTSProvider) IsKnownVoice(voiceID string) bool {
	trimmed := strings.TrimSpace(voiceID)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "kokoro:") || strings.HasPrefix(lower, "local:") {
		return true
	}
	for _, v := range BuiltinLocalKokoroVoices {
		if strings.EqualFold(v.VoiceID, trimmed) {
			return true
		}
	}
	// Kokoro 标准命名前缀：zf_/zm_/af_/am_/bf_/bm_/jf_/jm_/ef_/em_/ff_/hf_/hm_/if_/im_/pf_/pm_
	if len(lower) > 3 && lower[2] == '_' {
		prefix := lower[:3]
		switch prefix {
		case "zf_", "zm_", "af_", "am_", "bf_", "bm_", "jf_", "jm_", "ef_", "em_", "ff_", "hf_", "hm_", "if_", "im_", "pf_", "pm_":
			return true
		}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, d := range p.discovered {
		if strings.EqualFold(d.ID, trimmed) {
			return true
		}
	}
	return false
}

// NormalizeLocalVoiceID 去除可选的 local:/kokoro: 路由前缀。
func NormalizeLocalVoiceID(voiceID string) string {
	trimmed := strings.TrimSpace(voiceID)
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "kokoro:"):
		return strings.TrimSpace(trimmed[len("kokoro:"):])
	case strings.HasPrefix(lower, "local:"):
		return strings.TrimSpace(trimmed[len("local:"):])
	default:
		return trimmed
	}
}

// MapOpenAIVoiceToLocal 将标准 OpenAI 音色映射至 Kokoro 区别度明确的中/英文音色。
func MapOpenAIVoiceToLocal(voice string) string {
	switch strings.ToLower(strings.TrimSpace(voice)) {
	case "alloy":
		return "zf_xiaoxiao"
	case "ash":
		return "zm_yunyang"
	case "coral":
		return "zf_xiaoni"
	case "echo":
		return "zm_yunxi"
	case "fable":
		return "zm_yunjian"
	case "onyx":
		return "am_adam"
	case "nova":
		return "zf_xiaobei"
	case "sage":
		return "af_heart"
	case "shimmer":
		return "zf_xiaoyi"
	default:
		return "zf_xiaoxiao"
	}
}

// Synthesize 调用本地/自托管 OpenAI 兼容 /v1/audio/speech 合成语音（自动分句拼接）。
func (p *LocalTTSProvider) Synthesize(ctx context.Context, text, voice, model string, speed float64) ([]byte, error) {
	p.mu.RLock()
	baseURL := p.baseURL
	apiKey := p.apiKey
	defaultModel := p.defaultModel
	p.mu.RUnlock()

	if baseURL == "" {
		return nil, errors.New("LOCAL_TTS_BASE_URL 未配置，无法调用本地 TTS")
	}

	resolvedVoice := NormalizeLocalVoiceID(voice)
	if resolvedVoice == "" {
		resolvedVoice = "zf_xiaoxiao"
	}
	resolvedModel := strings.TrimSpace(model)
	if resolvedModel == "" || resolvedModel == "tts-1" || resolvedModel == "tts-1-hd" {
		resolvedModel = defaultModel
	}
	if resolvedModel == "" {
		resolvedModel = defaultLocalTTSModel
	}
	if speed <= 0 {
		speed = 1.0
	}

	chunks := SplitTextForTTS(text, 300)
	if len(chunks) == 0 {
		return nil, errors.New("输入文本不能为空")
	}

	endpoint := buildLocalTTSEndpoint(baseURL, "/audio/speech")
	var combined bytes.Buffer
	for i, chunk := range chunks {
		audio, err := p.synthesizeChunk(ctx, endpoint, apiKey, resolvedModel, chunk, resolvedVoice, speed)
		if err != nil {
			p.markHealth(false, err.Error())
			return nil, fmt.Errorf("local_tts chunk %d/%d failed: %w", i+1, len(chunks), err)
		}
		combined.Write(audio)
	}
	p.markHealth(true, "")
	return combined.Bytes(), nil
}

func (p *LocalTTSProvider) synthesizeChunk(ctx context.Context, endpoint, apiKey, model, text, voice string, speed float64) ([]byte, error) {
	payload := map[string]interface{}{
		"model":           model,
		"input":           text,
		"voice":           voice,
		"speed":           speed,
		"response_format": "mp3",
	}
	rawBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(rawBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 25*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("local_tts returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if len(data) == 0 {
		return nil, errors.New("local_tts returned empty audio payload")
	}
	return data, nil
}

func buildLocalTTSEndpoint(baseURL, suffix string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	cleanSuffix := "/" + strings.TrimLeft(suffix, "/")
	if strings.HasSuffix(trimmed, "/v1") {
		return trimmed + cleanSuffix
	}
	return trimmed + "/v1" + cleanSuffix
}

func parseLocalVoiceIDs(raw []byte) []string {
	var wrapped struct {
		Voices json.RawMessage `json:"voices"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		if len(wrapped.Voices) > 0 {
			if ids := extractVoiceIDSlice(wrapped.Voices); len(ids) > 0 {
				return ids
			}
		}
		if len(wrapped.Data) > 0 {
			if ids := extractVoiceIDSlice(wrapped.Data); len(ids) > 0 {
				return ids
			}
		}
	}
	return extractVoiceIDSlice(raw)
}

func extractVoiceIDSlice(raw json.RawMessage) []string {
	var strList []string
	if err := json.Unmarshal(raw, &strList); err == nil && len(strList) > 0 {
		return strList
	}
	var objList []struct {
		ID        string `json:"id"`
		VoiceID   string `json:"voice_id"`
		Name      string `json:"name"`
		ShortName string `json:"ShortName"`
	}
	if err := json.Unmarshal(raw, &objList); err == nil && len(objList) > 0 {
		out := make([]string, 0, len(objList))
		for _, item := range objList {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				id = strings.TrimSpace(item.VoiceID)
			}
			if id == "" {
				id = strings.TrimSpace(item.ShortName)
			}
			if id == "" {
				id = strings.TrimSpace(item.Name)
			}
			if id != "" {
				out = append(out, id)
			}
		}
		return out
	}
	return nil
}

func inferLocalVoiceMetadata(voiceID string) (locale, gender, sample string) {
	lower := strings.ToLower(strings.TrimSpace(voiceID))
	gender = "female"
	locale = "zh-CN"
	if strings.Contains(lower, "male") || (len(lower) > 2 && lower[1] == 'm') {
		gender = "male"
	}
	switch {
	case strings.HasPrefix(lower, "af_") || strings.HasPrefix(lower, "am_"):
		locale = "en-US"
	case strings.HasPrefix(lower, "bf_") || strings.HasPrefix(lower, "bm_"):
		locale = "en-GB"
	case strings.HasPrefix(lower, "jf_") || strings.HasPrefix(lower, "jm_"):
		locale = "ja-JP"
	}
	if strings.HasPrefix(locale, "zh") {
		sample = "你好，这是本地自托管音色 " + voiceID + " 的离线试听样音。"
	} else {
		sample = "Hello, this is local voice " + voiceID + " synthesized offline."
	}
	return locale, gender, sample
}
