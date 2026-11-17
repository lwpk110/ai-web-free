package handler

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	chatgptrequestconverter "ai-web-free/conversion/requests/chatgpt"
	"ai-web-free/httpclient/bogdanfinn"
	"ai-web-free/internal/accounts"
	"ai-web-free/internal/chatgpt"
	"ai-web-free/internal/config"
	"ai-web-free/internal/db"
	"ai-web-free/internal/provider"
	officialtypes "ai-web-free/typings/official"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type AudioHandler struct {
	accountPool *accounts.Pool
	cfg         *config.Config
	runtime     *GatewayRuntime
	registry    *provider.Registry
	crawlOpts   *provider.MediaCrawlOptions
}

func NewAudioHandler(pool *accounts.Pool, cfg *config.Config) *AudioHandler {
	return &AudioHandler{accountPool: pool, cfg: cfg}
}

// SetCrawlerOptions 覆盖音色/媒体爬虫配置（供测试注入自定义 Client 与允许回环端点）。
func (h *AudioHandler) SetCrawlerOptions(opts provider.MediaCrawlOptions) {
	h.crawlOpts = &opts
}

// SetRegistry 注入多供应商插件注册中心
func (h *AudioHandler) SetRegistry(reg *provider.Registry) {
	h.registry = reg
}

func (h *AudioHandler) getJianyingProvider() *provider.JianYingProvider {
	if h.registry == nil {
		return nil
	}
	if p, ok := h.registry.Get("jianying"); ok {
		if casted, ok := p.(*provider.JianYingProvider); ok {
			return casted
		}
	}
	return nil
}

func (h *AudioHandler) getEdgeTTSProvider() *provider.EdgeTTSProvider {
	if h.registry == nil {
		return nil
	}
	if p, ok := h.registry.Get("edge_tts"); ok {
		if casted, ok := p.(*provider.EdgeTTSProvider); ok {
			return casted
		}
	}
	return nil
}

func (h *AudioHandler) getDashScopeProvider() *provider.DashScopeProvider {
	if h.registry == nil {
		return nil
	}
	if p, ok := h.registry.Get("dashscope"); ok {
		if casted, ok := p.(*provider.DashScopeProvider); ok {
			return casted
		}
	}
	return nil
}

func (h *AudioHandler) getSiliconFlowProvider() *provider.SiliconFlowProvider {
	if h.registry == nil {
		return nil
	}
	if p, ok := h.registry.Get("siliconflow"); ok {
		if casted, ok := p.(*provider.SiliconFlowProvider); ok {
			return casted
		}
	}
	return nil
}

func (h *AudioHandler) getLocalTTSProvider() *provider.LocalTTSProvider {
	if h.registry == nil {
		return nil
	}
	if p, ok := h.registry.Get("local_tts"); ok {
		if casted, ok := p.(*provider.LocalTTSProvider); ok {
			return casted
		}
	}
	return nil
}

func (h *AudioHandler) getVoiceCatalogStore() *provider.VoiceCatalogStore {
	rt := h.ensureRuntime()
	return provider.NewVoiceCatalogStore(rt.StorageEngine)
}

func (h *AudioHandler) activeChatGPTCount() int {
	if h.accountPool == nil {
		return 0
	}
	st := h.accountPool.Stats()
	n := st.Active - st.NoAuthCount
	if n < 0 {
		return 0
	}
	return n
}

// SetRuntime 注入网关共享的容灾、代理健康与审计运行时。
func (h *AudioHandler) SetRuntime(rt *GatewayRuntime) {
	h.runtime = rt
}

func (h *AudioHandler) ensureRuntime() *GatewayRuntime {
	if h.runtime != nil {
		return h.runtime
	}
	return &GatewayRuntime{AccountPool: h.accountPool, Config: h.cfg}
}

// ── TTS ──

var ttsFmtMap = map[string]string{
	"mp3":  "mp3",
	"opus": "opus",
	"aac":  "aac",
	"flac": "aac",
	"wav":  "aac",
	"pcm":  "aac",
}

var ttsTypeMap = map[string]string{
	"mp3":  "audio/mpeg",
	"opus": "audio/ogg",
	"aac":  "audio/aac",
}

var ttsVoiceMap = map[string]string{
	"alloy":   "cove",
	"ash":     "fathom",
	"coral":   "vale",
	"echo":    "ember",
	"fable":   "breeze",
	"onyx":    "orbit",
	"nova":    "maple",
	"sage":    "glimmer",
	"shimmer": "juniper",
}

var chatgptVoiceOrder = []string{
	"cove", "fathom", "vale", "ember", "breeze", "orbit", "maple", "glimmer", "juniper",
}

func parseBoolHeader(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func isEdgeVoiceID(voice string) bool {
	lower := strings.ToLower(strings.TrimSpace(voice))
	if lower == "" {
		return false
	}
	return strings.HasPrefix(lower, "edge:") ||
		strings.HasSuffix(lower, "neural") ||
		strings.HasPrefix(lower, "zh-cn-") ||
		strings.HasPrefix(lower, "zh-hk-") ||
		strings.HasPrefix(lower, "zh-tw-") ||
		strings.HasPrefix(lower, "en-us-") ||
		strings.HasPrefix(lower, "en-gb-") ||
		strings.HasPrefix(lower, "ja-jp-") ||
		strings.HasPrefix(lower, "ko-kr-")
}

func normalizeEdgeVoiceID(voice string) string {
	trimmed := strings.TrimSpace(voice)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "edge:") {
		trimmed = strings.TrimSpace(trimmed[len("edge:"):])
	}
	if trimmed == "" {
		return "zh-CN-XiaoxiaoNeural"
	}
	return trimmed
}

func isSiliconFlowVoiceID(voice string) bool {
	lower := strings.ToLower(strings.TrimSpace(voice))
	return strings.Contains(lower, "funaudiollm/") ||
		strings.HasPrefix(lower, "cosyvoice-v1:") ||
		strings.HasPrefix(lower, "cosyvoice-v2:") ||
		strings.HasPrefix(lower, "speech:") ||
		strings.HasPrefix(lower, "siliconflow:")
}

func isLegacyJianyingVoiceID(voice string) bool {
	trimmed := strings.TrimSpace(voice)
	if trimmed == "" {
		return false
	}
	if _, ok := provider.FindJianyingVoice(trimmed); ok {
		return true
	}
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "zh_") || strings.HasPrefix(lower, "dy_") || strings.HasPrefix(lower, "en_")
}

func hashVoiceIndex(voice string, n int) int {
	if n <= 0 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(voice))))
	return int(h.Sum32() % uint32(n))
}

func mapVoiceToDistinctEdge(voice string) string {
	trimmed := strings.TrimSpace(voice)
	if isEdgeVoiceID(trimmed) {
		return normalizeEdgeVoiceID(trimmed)
	}
	lower := strings.ToLower(trimmed)
	if ttsVoiceMap[lower] != "" {
		return provider.MapOpenAIVoiceToEdge(lower)
	}
	switch lower {
	case "zf_xiaoxiao", "zh_female_shuangkuai":
		return "zh-CN-XiaoxiaoNeural"
	case "zf_xiaobei", "zh_male_dongbei", "zh_male_dongbeilaozong":
		return "zh-CN-liaoning-XiaobeiNeural"
	case "zf_xiaoni", "zh_female_sichuan":
		return "zh-CN-shaanxi-XiaoniNeural"
	case "zf_xiaoyi", "zh_female_tianmei":
		return "zh-CN-XiaoyiNeural"
	case "zm_yunxi", "zh_male_chunhou":
		return "zh-CN-YunxiNeural"
	case "zm_yunjian", "zh_male_houge", "zh_male_yangguang":
		return "zh-CN-YunjianNeural"
	case "zm_yunxia":
		return "zh-CN-YunxiaNeural"
	case "zm_yunyang", "zh_male_lengku", "zh_male_chenwen":
		return "zh-CN-YunyangNeural"
	case "af_heart":
		return "en-US-JennyNeural"
	case "af_bella":
		return "en-US-AvaMultilingualNeural"
	case "am_adam":
		return "en-US-GuyNeural"
	case "am_michael":
		return "en-US-AndrewMultilingualNeural"
	}
	if len(provider.BuiltinEdgeVoices) == 0 {
		return "zh-CN-XiaoxiaoNeural"
	}
	return provider.BuiltinEdgeVoices[hashVoiceIndex(lower, len(provider.BuiltinEdgeVoices))].VoiceID
}

func mapVoiceToDistinctLocal(voice string) string {
	trimmed := strings.TrimSpace(voice)
	lower := strings.ToLower(trimmed)
	if ttsVoiceMap[lower] != "" {
		return provider.MapOpenAIVoiceToLocal(lower)
	}
	switch lower {
	case "zh-cn-xiaoxiaoneural", "zh_female_shuangkuai":
		return "zf_xiaoxiao"
	case "zh-cn-liaoning-xiaobeineural":
		return "zf_xiaobei"
	case "zh-cn-shaanxi-xiaonineural":
		return "zf_xiaoni"
	case "zh-cn-xiaoyineural", "zh_female_tianmei":
		return "zf_xiaoyi"
	case "zh-cn-yunxineural", "zh_male_chunhou":
		return "zm_yunxi"
	case "zh-cn-yunjianneural", "zh_male_houge", "zh_male_yangguang":
		return "zm_yunjian"
	case "zh-cn-yunxianeural":
		return "zm_yunxia"
	case "zh-cn-yunyangneural", "zh_male_chenwen", "zh_male_lengku":
		return "zm_yunyang"
	case "en-us-jennyneural", "en-us-avamultilingualneural":
		return "af_heart"
	case "en-us-guyneural", "en-us-andrewmultilingualneural":
		return "am_adam"
	}
	if strings.HasPrefix(lower, "zf_") || strings.HasPrefix(lower, "zm_") ||
		strings.HasPrefix(lower, "af_") || strings.HasPrefix(lower, "am_") {
		return trimmed
	}
	presets := provider.BuiltinLocalKokoroVoices
	if len(presets) == 0 {
		return "zf_xiaoxiao"
	}
	return presets[hashVoiceIndex(lower, len(presets))].VoiceID
}

func mapVoiceToDistinctChatGPT(voice string) string {
	lower := strings.ToLower(strings.TrimSpace(voice))
	if mapped := ttsVoiceMap[lower]; mapped != "" {
		return mapped
	}
	return chatgptVoiceOrder[hashVoiceIndex(lower, len(chatgptVoiceOrder))]
}

func normalizePreferredTTSProvider(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "local", "local_tts", "kokoro":
		return "local_tts"
	case "edge", "edge_tts", "edge-tts":
		return "edge_tts"
	case "siliconflow", "sf":
		return "siliconflow"
	case "dashscope", "aliyun", "bailian":
		return "dashscope"
	case "chatgpt", "openai":
		return "chatgpt"
	case "jianying", "jy":
		return "jianying"
	default:
		return ""
	}
}

func (h *AudioHandler) resolveTTSEngine(voice, model, preferProvider string) string {
	if pref := normalizePreferredTTSProvider(preferProvider); pref != "" {
		return pref
	}
	trimmedVoice := strings.TrimSpace(voice)
	voiceLower := strings.ToLower(trimmedVoice)
	modelLower := strings.ToLower(strings.TrimSpace(model))
	if trimmedVoice == "" || ttsVoiceMap[voiceLower] != "" {
		return "openai_auto"
	}
	if localP := h.getLocalTTSProvider(); localP != nil && localP.IsKnownVoice(trimmedVoice) {
		return "local_tts"
	}
	if strings.HasPrefix(modelLower, "kokoro") {
		return "local_tts"
	}
	if isEdgeVoiceID(trimmedVoice) {
		return "edge_tts"
	}
	if isSiliconFlowVoiceID(trimmedVoice) {
		return "siliconflow"
	}
	if provider.IsDashScopeVoice(trimmedVoice) || strings.HasPrefix(voiceLower, "designed_") || strings.HasPrefix(voiceLower, "custom_") {
		return "dashscope"
	}
	if isLegacyJianyingVoiceID(trimmedVoice) {
		return "jianying"
	}
	return "unknown"
}

func (h *AudioHandler) synthesizeDirectEngine(ctx context.Context, engine, input, voice, model string, speed float64) (audio []byte, usedProvider, usedVoice string, status int, err error) {
	if resolvedEngine, resolvedVoice, pendingErr := h.resolveCustomVoiceBeforeSynth(ctx, voice); pendingErr != nil {
		return nil, "custom", voice, http.StatusBadRequest, pendingErr
	} else if resolvedEngine != "" && resolvedVoice != "" {
		engine = resolvedEngine
		voice = resolvedVoice
	}
	switch engine {
	case "local_tts":
		localP := h.getLocalTTSProvider()
		if localP == nil || !localP.IsConfigured() {
			return nil, "local_tts", voice, http.StatusServiceUnavailable, errors.New("外部/自定义 OpenAI 兼容 TTS 服务未配置（可选设置 CUSTOM_TTS_BASE_URL / LOCAL_TTS_BASE_URL）")
		}
		targetVoice := provider.NormalizeLocalVoiceID(voice)
		if targetVoice == "" || ttsVoiceMap[strings.ToLower(targetVoice)] != "" || isEdgeVoiceID(targetVoice) || isLegacyJianyingVoiceID(targetVoice) {
			targetVoice = mapVoiceToDistinctLocal(voice)
		}
		audioBytes, synErr := localP.Synthesize(ctx, input, targetVoice, model, speed)
		if synErr != nil {
			return nil, "local_tts", targetVoice, http.StatusBadGateway, synErr
		}
		return audioBytes, "local_tts", targetVoice, http.StatusOK, nil

	case "edge_tts":
		edge := h.getEdgeTTSProvider()
		if edge == nil {
			return nil, "edge_tts", voice, http.StatusServiceUnavailable, errors.New("微软 Edge-TTS 插件未注册")
		}
		targetVoice := mapVoiceToDistinctEdge(voice)
		audioBytes, synErr := edge.Synthesize(ctx, input, targetVoice, speed)
		if synErr != nil {
			return nil, "edge_tts", targetVoice, http.StatusBadGateway, fmt.Errorf("Edge-TTS 合成失败: %w", synErr)
		}
		return audioBytes, "edge_tts", targetVoice, http.StatusOK, nil

	case "siliconflow":
		sf := h.getSiliconFlowProvider()
		if sf == nil || sf.Stats().Active == 0 {
			return nil, "siliconflow", voice, http.StatusServiceUnavailable, errors.New("硅基流动未配置有效 API Key，请设置 SILICONFLOW_API_KEY 或点击「配置云端 API Key」导入密钥")
		}
		targetVoice := provider.NormalizeSiliconFlowVoice(voice)
		if strings.TrimSpace(voice) == "" || ttsVoiceMap[strings.ToLower(strings.TrimSpace(voice))] != "" {
			sfVoices := provider.GetSiliconFlowVoices(true, "")
			targetVoice = sfVoices[hashVoiceIndex(voice, len(sfVoices))].ID
		}
		audioBytes, synErr := sf.SynthesizeSpeechWithSpeed(ctx, input, targetVoice, speed)
		if synErr != nil {
			return nil, "siliconflow", targetVoice, http.StatusBadGateway, synErr
		}
		return audioBytes, "siliconflow", targetVoice, http.StatusOK, nil

	case "dashscope":
		ds := h.getDashScopeProvider()
		if ds == nil || ds.Stats().Active == 0 {
			return nil, "dashscope", voice, http.StatusServiceUnavailable, errors.New("阿里云百炼未配置有效 API Key，请设置 DASHSCOPE_API_KEY 或点击「配置云端 API Key」导入密钥")
		}
		targetVoice := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(voice, "dashscope:"), "DashScope:"))
		if targetVoice == "" || ttsVoiceMap[strings.ToLower(targetVoice)] != "" {
			dsVoices := provider.GetDashScopeVoices(true, "")
			targetVoice = dsVoices[hashVoiceIndex(voice, len(dsVoices))].ID
		}
		audioBytes, synErr := ds.SynthesizeSpeechWithRate(ctx, input, targetVoice, speed)
		if synErr != nil {
			return nil, "dashscope", targetVoice, http.StatusBadGateway, synErr
		}
		return audioBytes, "dashscope", targetVoice, http.StatusOK, nil

	case "jianying":
		return nil, "jianying", voice, http.StatusGone, errors.New("剪映旧免密接口 (api-normal-c-lq.zijieapi.com) 已永久下线，请切换至微软 Edge-TTS 或硅基流动/百炼云端音色，或传递 X-Allow-Fallback: true 自动迁移")

	case "unknown":
		return nil, "unknown", voice, http.StatusBadRequest, fmt.Errorf("未识别的音色 ID %q；请先调用 GET /v1/audio/voices 查询可用音色，或传 X-Allow-Fallback: true", voice)

	default:
		return nil, engine, voice, http.StatusBadRequest, fmt.Errorf("不支持的语音引擎: %s", engine)
	}
}

func (h *AudioHandler) tryFallbackSynthesis(ctx context.Context, failedEngine, input, voice, model string, speed float64) ([]byte, string, string, bool) {
	if failedEngine != "local_tts" {
		if localP := h.getLocalTTSProvider(); localP != nil && localP.IsAvailable() {
			if audio, usedProv, usedVoice, _, err := h.synthesizeDirectEngine(ctx, "local_tts", input, mapVoiceToDistinctLocal(voice), model, speed); err == nil && len(audio) > 0 {
				return audio, usedProv, usedVoice, true
			}
		}
	}
	if failedEngine != "edge_tts" {
		if audio, usedProv, usedVoice, _, err := h.synthesizeDirectEngine(ctx, "edge_tts", input, mapVoiceToDistinctEdge(voice), model, speed); err == nil && len(audio) > 0 {
			return audio, usedProv, usedVoice, true
		}
	}
	return nil, "", "", false
}

func writeTTSErrorJSON(c *gin.Context, status int, errType, codeVal, prov, message string) {
	if status <= 0 {
		status = http.StatusBadGateway
	}
	c.JSON(status, gin.H{
		"error": gin.H{
			"message":  message,
			"type":     errType,
			"param":    "voice",
			"code":     codeVal,
			"provider": prov,
		},
	})
}

func (h *AudioHandler) writeTTSAudioResponse(c *gin.Context, contentType, usedProvider, usedVoice, requestedVoice, input string, speed float64, fallback bool, fallbackReason string, audioBytes []byte) {
	c.Header("Content-Type", contentType)
	c.Header("X-TTS-Provider", usedProvider)
	c.Header("X-TTS-Voice", usedVoice)
	if fallback {
		c.Header("X-TTS-Fallback", "true")
		if fallbackReason != "" {
			c.Header("X-TTS-Fallback-Reason", fallbackReason)
		}
	} else {
		c.Header("X-TTS-Fallback", "false")
	}
	label := usedProvider
	if fallback {
		label = usedProvider + "(降级:" + requestedVoice + "->" + usedVoice + ")"
	}
	h.recordAudioHistory(usedVoice, label, usedProvider, input, speed, audioBytes)
	c.Data(http.StatusOK, contentType, audioBytes)
}

func (h *AudioHandler) TTS(c *gin.Context) {
	var original_request officialtypes.TTSAPIRequest
	if err := c.BindJSON(&original_request); err != nil {
		c.JSON(400, gin.H{"error": gin.H{
			"message": "Request must be proper JSON",
			"type":    "invalid_request_error",
			"param":   nil,
			"code":    err.Error(),
		}})
		return
	}
	if original_request.Input == "" {
		c.JSON(400, gin.H{"error": gin.H{
			"message": "Missing required parameter: input",
			"type":    "invalid_request_error",
			"param":   "input",
			"code":    "missing_required_parameter",
		}})
		return
	}

	ttsModel := strings.TrimSpace(original_request.Model)
	if ttsModel == "" {
		ttsModel = "tts-1"
	}
	requestedVoice := strings.TrimSpace(original_request.Voice)
	preferProvider := c.GetHeader("X-Prefer-Provider")
	allowFallback := parseBoolHeader(c.GetHeader("X-Allow-Fallback"))
	engine := h.resolveTTSEngine(requestedVoice, ttsModel, preferProvider)

	// 1. 非 ChatGPT 语音引擎（Local / Edge / SiliconFlow / DashScope / 报错拦截）
	if engine != "chatgpt" && engine != "openai_auto" {
		audioBytes, usedProv, usedVoice, status, err := h.synthesizeDirectEngine(
			c.Request.Context(),
			engine,
			original_request.Input,
			requestedVoice,
			ttsModel,
			original_request.Speed,
		)
		if err == nil && len(audioBytes) > 0 {
			h.writeTTSAudioResponse(c, "audio/mpeg", usedProv, usedVoice, requestedVoice, original_request.Input, original_request.Speed, false, "", audioBytes)
			return
		}
		if allowFallback {
			if fbAudio, fbProv, fbVoice, ok := h.tryFallbackSynthesis(c.Request.Context(), engine, original_request.Input, requestedVoice, ttsModel, original_request.Speed); ok {
				h.writeTTSAudioResponse(c, "audio/mpeg", fbProv, fbVoice, requestedVoice, original_request.Input, original_request.Speed, true, engine+"_unavailable", fbAudio)
				return
			}
		}
		writeTTSErrorJSON(c, status, "tts_synthesis_error", engine+"_failed", engine, err.Error())
		return
	}

	// 2. OpenAI 兼容标准音色（alloy/nova/...）：有 ChatGPT 登录账号时优先走官方语音；否则无 ChatGPT 账号且未强制 chatgpt 时自动走 local_tts -> edge_tts（各音色独立映射）
	if engine == "openai_auto" && h.activeChatGPTCount() == 0 {
		openAIVoice := requestedVoice
		if openAIVoice == "" {
			openAIVoice = "alloy"
		}
		if localP := h.getLocalTTSProvider(); localP != nil && localP.IsAvailable() {
			if audioBytes, usedProv, usedVoice, _, err := h.synthesizeDirectEngine(c.Request.Context(), "local_tts", original_request.Input, provider.MapOpenAIVoiceToLocal(openAIVoice), ttsModel, original_request.Speed); err == nil && len(audioBytes) > 0 {
				h.writeTTSAudioResponse(c, "audio/mpeg", usedProv, usedVoice, openAIVoice, original_request.Input, original_request.Speed, false, "", audioBytes)
				return
			}
		}
		if audioBytes, usedProv, usedVoice, status, err := h.synthesizeDirectEngine(c.Request.Context(), "edge_tts", original_request.Input, provider.MapOpenAIVoiceToEdge(openAIVoice), ttsModel, original_request.Speed); err == nil && len(audioBytes) > 0 {
			h.writeTTSAudioResponse(c, "audio/mpeg", usedProv, usedVoice, openAIVoice, original_request.Input, original_request.Speed, false, "", audioBytes)
			return
		} else {
			writeTTSErrorJSON(c, status, "tts_synthesis_error", "edge_tts_failed", "edge_tts", err.Error())
			return
		}
	}

	// 3. ChatGPT 官方 TTS（严格校验 OpenAI 音色映射，禁止静默塌缩为 cove）
	chatgptVoice := ttsVoiceMap[strings.ToLower(requestedVoice)]
	if requestedVoice == "" {
		chatgptVoice = ttsVoiceMap["alloy"]
	}
	if chatgptVoice == "" {
		if !allowFallback {
			writeTTSErrorJSON(c, http.StatusBadRequest, "invalid_request_error", "unsupported_chatgpt_voice", "chatgpt", fmt.Sprintf("ChatGPT 语音通道不支持音色 %q（仅支持 alloy/ash/coral/echo/fable/onyx/nova/sage/shimmer），或传 X-Allow-Fallback: true", requestedVoice))
			return
		}
		chatgptVoice = mapVoiceToDistinctChatGPT(requestedVoice)
	}

	rt := h.ensureRuntime()
	res, err := rt.RunWithFailover(c, h.accountPool, h.cfg, FailoverOptions{
		NeedsPaid:  true,
		Capability: accounts.CapTTS.Name,
		Model:      ttsModel,
	}, func(acct *accounts.Account, client *bogdanfinn.TlsClient) AttemptOutcome {
		if acct.Token == "" {
			return AttemptOutcome{StatusCode: 400, Err: errMissingAccessToken}
		}
		if !acct.Type.Satisfies(accounts.CapTTS) {
			return AttemptOutcome{StatusCode: 403, Err: errAccountCapDenied}
		}
		translated := chatgptrequestconverter.ConvertTTSAPIRequest(original_request.Input)
		state := chatgpt.NewChatClientState()
		state.ConversationID = translated.ConversationID
		state.ParentMessageID = translated.ParentMessageID
		resp, wsConn, _, status, callErr := conversationClientOrder(&client, acct, translated, acct.Proxy, false, state, h.accountPool)
		return AttemptOutcome{
			Response:   resp,
			WSConn:     wsConn,
			Payload:    client,
			StatusCode: status,
			Err:        callErr,
		}
	})
	if err != nil {
		if allowFallback {
			if fbAudio, fbProv, fbVoice, ok := h.tryFallbackSynthesis(c.Request.Context(), "chatgpt", original_request.Input, requestedVoice, ttsModel, original_request.Speed); ok {
				h.writeTTSAudioResponse(c, "audio/mpeg", fbProv, fbVoice, requestedVoice, original_request.Input, original_request.Speed, true, "chatgpt_unavailable", fbAudio)
				return
			}
		}

		errStatus := res.Outcome.StatusCode
		if errStatus <= 0 {
			errStatus = 400
		}
		if err == errMissingAccessToken {
			c.JSON(400, gin.H{"error": "TTS requires a logged-in ChatGPT access token."})
			rt.RecordAudit(c, ttsModel, res, 400, err)
			return
		}
		if err == errAccountCapDenied {
			c.JSON(403, gin.H{"error": "TTS requires a logged-in ChatGPT account."})
			rt.RecordAudit(c, ttsModel, res, 403, err)
			return
		}
		errType := "request_conversion_error"
		errParam := interface{}("model")
		codeVal := interface{}("request_conversion_error")
		if res.Attempts == 0 {
			errType = "authorization_error"
			errParam = "Authorization"
			codeVal = 400
		}
		c.JSON(errStatus, gin.H{"error": gin.H{
			"message": err.Error(),
			"type":    errType,
			"param":   errParam,
			"code":    codeVal,
		}})
		rt.RecordAudit(c, ttsModel, res, errStatus, err)
		return
	}

	account := res.Account
	client, _ := res.Outcome.Payload.(*bogdanfinn.TlsClient)
	if client == nil {
		client = EnsureAccountClient(account)
	}
	proxyUrl := account.Proxy
	response := res.Outcome.Response
	wsConn := res.Outcome.WSConn
	defer response.Body.Close()
	if chatgpt.Handle_request_error(c, response) {
		if wsConn != nil {
			wsConn.Close()
		}
		rt.RecordAudit(c, ttsModel, res, response.StatusCode, FailoverStatusError(res, response.StatusCode))
		return
	}

	msgId, convId := chatgpt.HandlerTTS(response, original_request.Input)
	if msgId == "" || convId == "" {
		c.JSON(500, gin.H{"error": "failed to get TTS message id"})
		rt.RecordAudit(c, ttsModel, res, 500, FailoverStatusError(res, 500))
		return
	}
	defer chatgpt.RemoveConversation(client, account, convId, proxyUrl)

	format := ttsFmtMap[original_request.Format]
	if format == "" {
		format = "aac"
	}
	data, status, err := chatgpt.GetTTS(client, account, msgId, convId, chatgptVoice, format, proxyUrl)
	if err != nil {
		c.JSON(status, gin.H{"error": gin.H{
			"message": err.Error(),
			"type":    "synthesize_request_error",
			"param":   nil,
			"code":    status,
		}})
		rt.RecordAudit(c, ttsModel, res, status, err)
		return
	}
	isMappedFallback := requestedVoice != "" && ttsVoiceMap[strings.ToLower(requestedVoice)] == ""
	rt.RecordAudit(c, ttsModel, res, 200, nil)
	h.writeTTSAudioResponse(c, ttsTypeMap[format], "chatgpt", chatgptVoice, requestedVoice, original_request.Input, original_request.Speed, isMappedFallback, "", data)
}

// ── Audio Transcriptions ──

var transcriptionsSupportedFormats = map[string]string{
	"json":         "application/json",
	"text":         "text/plain",
	"verbose_json": "application/json",
}

func (h *AudioHandler) Transcriptions(c *gin.Context) {
	h.handleTranscription(c, false)
}

func (h *AudioHandler) Translations(c *gin.Context) {
	h.handleTranscription(c, true)
}

func (h *AudioHandler) handleTranscription(c *gin.Context, isTranslation bool) {
	contentType := strings.Split(c.GetHeader("Content-Type"), ";")[0]
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		c.JSON(400, gin.H{"error": gin.H{
			"message": "Request must be multipart/form-data",
			"type":    "invalid_request_error",
			"param":   "Content-Type",
			"code":    "invalid_content_type",
		}})
		return
	}

	if err := c.Request.ParseMultipartForm(50 << 20); err != nil {
		c.JSON(400, gin.H{"error": gin.H{
			"message": "Failed to parse multipart form: " + err.Error(),
			"type":    "invalid_request_error",
			"param":   nil,
			"code":    "parse_error",
		}})
		return
	}

	model := strings.TrimSpace(c.Request.FormValue("model"))
	language := strings.TrimSpace(c.Request.FormValue("language"))
	prompt := c.Request.FormValue("prompt")
	responseFormat := strings.TrimSpace(c.Request.FormValue("response_format"))

	if model == "" {
		model = "whisper-1"
	}
	if responseFormat == "" {
		responseFormat = "json"
	}

	respContentType, formatOK := transcriptionsSupportedFormats[responseFormat]
	if !formatOK {
		c.JSON(400, gin.H{"error": gin.H{
			"message": fmt.Sprintf("Unsupported response_format: %s. Supported values: json, text, verbose_json", responseFormat),
			"type":    "invalid_request_error",
			"param":   "response_format",
			"code":    "invalid_response_format",
		}})
		return
	}

	if len(prompt) > 1000 {
		prompt = prompt[:1000]
	}

	formFile, fileHeader, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(400, gin.H{"error": gin.H{
			"message": "Missing required multipart field: file",
			"type":    "invalid_request_error",
			"param":   "file",
			"code":    "missing_required_parameter",
		}})
		return
	}
	defer formFile.Close()

	audioData, err := io.ReadAll(formFile)
	if err != nil {
		c.JSON(400, gin.H{"error": gin.H{
			"message": err.Error(),
			"type":    "invalid_request_error",
			"param":   "file",
			"code":    "file_read_error",
		}})
		return
	}
	if len(audioData) == 0 {
		c.JSON(400, gin.H{"error": gin.H{
			"message": "Uploaded audio file is empty",
			"type":    "invalid_request_error",
			"param":   "file",
			"code":    "empty_file",
		}})
		return
	}

	modelName := c.PostForm("model")
	if modelName == "" {
		modelName = "whisper-1"
	}
	mimeType := fileHeader.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "audio/mpeg"
	}

	rt := h.ensureRuntime()
	res, err := rt.RunWithFailover(c, h.accountPool, h.cfg, FailoverOptions{
		NeedsPaid:  true,
		Capability: accounts.CapTranscribe.Name,
		Model:      modelName,
	}, func(acct *accounts.Account, client *bogdanfinn.TlsClient) AttemptOutcome {
		if acct.Token == "" {
			return AttemptOutcome{StatusCode: 400, Err: errMissingAccessToken}
		}
		if !acct.Type.Satisfies(accounts.CapTranscribe) {
			return AttemptOutcome{StatusCode: 403, Err: errAccountCapDenied}
		}
		text, status, callErr := chatgpt.TranscribeAudio(client, acct, acct.Proxy, audioData, fileHeader.Filename, mimeType, language)
		return AttemptOutcome{Payload: text, StatusCode: status, Err: callErr}
	})
	if err != nil {
		errStatus := res.Outcome.StatusCode
		if errStatus <= 0 {
			errStatus = 400
		}
		errType := "transcription_error"
		if errStatus == 400 || errStatus == 401 || errStatus == 403 {
			if err == errMissingAccessToken {
				c.JSON(400, gin.H{"error": gin.H{
					"message": "Audio transcription requires a logged-in ChatGPT access token.",
					"type":    "invalid_request_error",
					"param":   "file",
					"code":    "missing_access_token",
				}})
			} else if err == errAccountCapDenied {
				c.JSON(403, gin.H{"error": "Audio transcription requires a logged-in ChatGPT account."})
			} else {
				errType = "authorization_error"
				c.JSON(errStatus, gin.H{"error": gin.H{
					"message": err.Error(),
					"type":    errType,
					"param":   "Authorization",
					"code":    errStatus,
				}})
			}
			rt.RecordAudit(c, modelName, res, errStatus, err)
			return
		}
		c.JSON(errStatus, gin.H{"error": gin.H{
			"message": err.Error(),
			"type":    errType,
			"param":   nil,
			"code":    "transcription_error",
		}})
		rt.RecordAudit(c, modelName, res, errStatus, err)
		return
	}

	text, _ := res.Outcome.Payload.(string)
	rt.RecordAudit(c, modelName, res, 200, nil)

	switch responseFormat {
	case "json":
		c.JSON(200, officialtypes.TranscriptionResponse{Text: text})
	case "text":
		c.Data(200, "text/plain; charset=utf-8", []byte(text))
	case "verbose_json":
		c.JSON(200, officialtypes.VerboseTranscriptionResponse{
			Task:     "transcribe",
			Language: language,
			Duration: 0,
			Text:     text,
			Segments: []officialtypes.TranscriptionSegment{},
			Words:    []officialtypes.TranscriptionWord{},
		})
	default:
		c.Data(200, respContentType, []byte(text))
	}
}

func openAIStandardVoices(chatgptActive int) []provider.VoiceDescriptor {
	reason := ""
	if chatgptActive == 0 {
		reason = "暂无已登录 ChatGPT 账号；OpenAI 标准音色在直连 /v1/audio/speech 时会自动映射为本地/Edge 独立音色"
	}
	presets := []struct {
		id     string
		name   string
		gender string
		sample string
	}{
		{"alloy", "OpenAI Alloy (均衡中性 · 多语种)", "female", "Hello, this is OpenAI Alloy voice with clear balanced tone."},
		{"nova", "OpenAI Nova (清亮活力 · 女声)", "female", "Hello, I am Nova, bright and expressive for narration."},
		{"echo", "OpenAI Echo (温和叙事 · 男声)", "male", "Welcome, this is Echo speaking in a warm narrative style."},
		{"onyx", "OpenAI Onyx (低沉权威 · 男声)", "male", "Attention everyone, this is Onyx with a deep authoritative voice."},
		{"fable", "OpenAI Fable (故事演绎 · 英式)", "male", "Once upon a time, Fable brought every character to life."},
		{"shimmer", "OpenAI Shimmer (轻柔清澈 · 女声)", "female", "Good morning, shimmer gently guides your audience through the story."},
		{"ash", "OpenAI Ash (沉稳理性 · 男声)", "male", "Here is the concise technical briefing read by Ash."},
		{"coral", "OpenAI Coral (亲和自然 · 女声)", "female", "Hi there, Coral brings warm everyday conversation to your project."},
		{"sage", "OpenAI Sage (从容知性 · 女声)", "female", "Step by step, Sage explains complex ideas with clarity."},
	}
	out := make([]provider.VoiceDescriptor, 0, len(presets))
	for _, item := range presets {
		out = append(out, provider.VoiceDescriptor{
			ID:           item.id,
			Name:         item.name,
			Provider:     "chatgpt",
			Category:     "多语种精选",
			Gender:       item.gender,
			Locale:       "en-US",
			SampleText:   item.sample,
			Tags:         []string{"OpenAI标准", "ChatGPT", "多语种"},
			Available:    chatgptActive > 0,
			StatusReason: reason,
		})
	}
	return out
}

func (h *AudioHandler) collectVoiceCatalog(ctx context.Context, probeLocal bool) ([]provider.VoiceDescriptor, gin.H, int) {
	voices := make([]provider.VoiceDescriptor, 0, 80)

	localConfigured := false
	localAvailable := false
	localEndpoint := ""
	localHealthy := false
	localLastErr := ""
	if localP := h.getLocalTTSProvider(); localP != nil {
		localConfigured = localP.IsConfigured()
		localEndpoint = localP.Endpoint()
		if probeLocal && localConfigured {
			_ = localP.ProbeMaybeCached(ctx)
		}
		localAvailable = localP.IsAvailable()
		localHealthy, localLastErr = localP.HealthSnapshot()
		voices = append(voices, localP.VoiceDescriptors()...)
	}

	edgeHealthy := true
	edgeLastErr := ""
	if edge := h.getEdgeTTSProvider(); edge != nil {
		edgeHealthy, edgeLastErr = edge.HealthSnapshot()
		voices = append(voices, edge.VoiceDescriptors()...)
	} else {
		voices = append(voices, provider.GetEdgeTTSVoiceDescriptors()...)
	}

	sfActive := 0
	if sf := h.getSiliconFlowProvider(); sf != nil {
		sfActive = sf.Stats().Active
	}
	sfReason := ""
	if sfActive == 0 {
		sfReason = "未配置 SILICONFLOW_API_KEY 或账号池中无有效 Key"
	}
	voices = append(voices, provider.GetSiliconFlowVoices(sfActive > 0, sfReason)...)

	dsActive := 0
	if ds := h.getDashScopeProvider(); ds != nil {
		dsActive = ds.Stats().Active
	}
	dsReason := ""
	if dsActive == 0 {
		dsReason = "未配置 DASHSCOPE_API_KEY 或账号池中无有效 Key"
	}
	voices = append(voices, provider.GetDashScopeVoices(dsActive > 0, dsReason)...)

	cgptActive := h.activeChatGPTCount()
	voices = append(voices, openAIStandardVoices(cgptActive)...)

	store := h.getVoiceCatalogStore()
	customRecords, _ := store.LoadCustomVoices(ctx)
	for _, rec := range customRecords {
		voices = append(voices, provider.CustomVoiceToDescriptor(rec, sfActive > 0, dsActive > 0))
	}
	voices = provider.EnrichShortDramaVoices(voices)
	annotated, cachedCount := store.AnnotateDescriptors(ctx, voices)

	summary := gin.H{
		"local_tts": gin.H{
			"available":  localAvailable,
			"configured": localConfigured,
			"endpoint":   localEndpoint,
			"healthy":    localHealthy,
			"last_error": localLastErr,
		},
		"edge_tts": gin.H{
			"available":   true,
			"zero_config": true,
			"healthy":     edgeHealthy,
			"last_error":  edgeLastErr,
		},
		"siliconflow": gin.H{
			"available": sfActive > 0,
			"active":    sfActive,
			"reason":    sfReason,
		},
		"dashscope": gin.H{
			"available": dsActive > 0,
			"active":    dsActive,
			"reason":    dsReason,
		},
		"chatgpt": gin.H{
			"available": cgptActive > 0,
			"active":    cgptActive,
		},
		"jianying": gin.H{
			"available": false,
			"retired":   true,
			"reason":    "字节旧免密域名 api-normal-c-lq.zijieapi.com 已返回 NXDOMAIN 下线",
		},
	}

	return annotated, summary, cachedCount
}

// ListVoices 返回聚合音色清单与实时引擎状态: GET /v1/audio/voices
func (h *AudioHandler) ListVoices(c *gin.Context) {
	voices, summary, cachedCount := h.collectVoiceCatalog(c.Request.Context(), true)

	filterProvider := strings.ToLower(strings.TrimSpace(c.Query("provider")))
	availableOnly := parseBoolHeader(c.Query("available_only"))
	filtered := make([]provider.VoiceDescriptor, 0, len(voices))
	availableTotal := 0
	for _, v := range voices {
		if v.Available {
			availableTotal++
		}
		if filterProvider != "" && strings.ToLower(v.Provider) != filterProvider {
			continue
		}
		if availableOnly && !v.Available {
			continue
		}
		filtered = append(filtered, v)
	}

	c.JSON(http.StatusOK, gin.H{
		"voices":             filtered,
		"total":              len(filtered),
		"available_total":    availableTotal,
		"cached_local_total": cachedCount,
		"providers":          summary,
	})
}

func (h *AudioHandler) synthesizeVoiceSampleStrict(ctx context.Context, voiceID, sampleText string) (audio []byte, usedProv, usedVoice string, status int, err error) {
	text := strings.TrimSpace(sampleText)
	if text == "" {
		text = "你好，这是一段语音音色的本地试听样音。"
	}
	engine := h.resolveTTSEngine(voiceID, "tts-1", "")
	if engine == "openai_auto" {
		if localP := h.getLocalTTSProvider(); localP != nil && localP.IsAvailable() {
			return h.synthesizeDirectEngine(ctx, "local_tts", text, provider.MapOpenAIVoiceToLocal(voiceID), "tts-1", 1.0)
		}
		return h.synthesizeDirectEngine(ctx, "edge_tts", text, provider.MapOpenAIVoiceToEdge(voiceID), "tts-1", 1.0)
	}
	return h.synthesizeDirectEngine(ctx, engine, text, voiceID, "tts-1", 1.0)
}

// PreviewVoice 获取指定音色的试听音频（优先读取本地 voice_samples 缓存）:
// GET /v1/audio/voices/:id/preview 与 GET /v1/audio/voice-preview?id=...
func (h *AudioHandler) PreviewVoice(c *gin.Context) {
	rawID := strings.TrimSpace(c.Param("id"))
	if rawID == "" {
		rawID = strings.TrimSpace(c.Query("id"))
	}
	if rawID == "" {
		rawID = strings.TrimSpace(c.Query("voice"))
	}
	if unescaped, err := url.PathUnescape(rawID); err == nil && strings.TrimSpace(unescaped) != "" {
		rawID = strings.TrimSpace(unescaped)
	}
	if rawID == "" {
		writeTTSErrorJSON(c, http.StatusBadRequest, "invalid_request_error", "missing_voice_id", "", "请指定有效的音色 ID")
		return
	}

	refresh := parseBoolHeader(c.Query("refresh"))
	store := h.getVoiceCatalogStore()
	if !refresh && store.Enabled() {
		if cachedBytes, hit, err := store.ReadSample(c.Request.Context(), rawID); err == nil && hit {
			c.Header("Content-Type", "audio/mpeg")
			c.Header("X-Voice-Cache", "hit")
			c.Header("X-TTS-Provider", "local_cache")
			c.Header("X-TTS-Voice", rawID)
			c.Header("Cache-Control", "public, max-age=3600")
			c.Data(http.StatusOK, "audio/mpeg", cachedBytes)
			return
		}
	}

	sampleText := ""
	voices, _, _ := h.collectVoiceCatalog(c.Request.Context(), false)
	for _, v := range voices {
		if strings.EqualFold(v.ID, rawID) {
			sampleText = v.SampleText
			break
		}
	}

	audioBytes, usedProv, usedVoice, status, err := h.synthesizeVoiceSampleStrict(c.Request.Context(), rawID, sampleText)
	if err != nil {
		writeTTSErrorJSON(c, status, "tts_preview_error", "preview_synthesis_failed", usedProv, err.Error())
		return
	}

	cacheHeader := "miss"
	if store.Enabled() {
		if _, saveErr := store.SaveSample(c.Request.Context(), rawID, audioBytes); saveErr == nil {
			cacheHeader = "stored"
		}
	}
	c.Header("Content-Type", "audio/mpeg")
	c.Header("X-Voice-Cache", cacheHeader)
	c.Header("X-TTS-Provider", usedProv)
	c.Header("X-TTS-Voice", usedVoice)
	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, "audio/mpeg", audioBytes)
}

// PullLocalVoicesRequest 将音色目录与精选试听样音拉取到本地缓存的请求体。
type PullLocalVoicesRequest struct {
	Voices         []string `json:"voices"`
	IncludeSamples *bool    `json:"include_samples"`
	RefreshLive    *bool    `json:"refresh_live"`
	Force          bool     `json:"force"`
	Limit          int      `json:"limit"`
}

// PullLocalVoices 同步远端/本地音色目录并批量拉取样音至本地存储: POST /v1/audio/voices/pull-local
func (h *AudioHandler) PullLocalVoices(c *gin.Context) {
	var req PullLocalVoicesRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON request body: " + err.Error()})
			return
		}
	}

	includeSamples := true
	if req.IncludeSamples != nil {
		includeSamples = *req.IncludeSamples
	}
	refreshLive := true
	if req.RefreshLive != nil {
		refreshLive = *req.RefreshLive
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 24
	}
	if limit > 60 {
		limit = 60
	}

	ctx := c.Request.Context()
	edgeSynced := 0
	var warnings []string

	if refreshLive {
		if edge := h.getEdgeTTSProvider(); edge != nil {
			syncCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
			liveVoices, err := edge.FetchLiveVoices(syncCtx)
			cancel()
			if err != nil {
				warnings = append(warnings, "Edge 音色目录刷新失败: "+err.Error())
			} else {
				edgeSynced = len(liveVoices)
			}
		}
		if localP := h.getLocalTTSProvider(); localP != nil && localP.IsConfigured() {
			if err := localP.Probe(ctx); err != nil {
				warnings = append(warnings, "本地 TTS 节点探测失败: "+err.Error())
			}
		}
	}

	voices, _, _ := h.collectVoiceCatalog(ctx, false)
	store := h.getVoiceCatalogStore()
	catalogSaved := false
	if store.Enabled() {
		if err := store.SaveCatalog(ctx, voices); err == nil {
			catalogSaved = true
		} else {
			warnings = append(warnings, "写入本地 voices/catalog.json 失败: "+err.Error())
		}
	}

	pulledSamples := 0
	alreadyCached := 0
	failedSamples := 0
	if includeSamples && store.Enabled() {
		targets := selectVoicesForLocalPull(voices, req.Voices, limit)
		type pullResult struct {
			cached bool
			pulled bool
			errMsg string
		}
		results := make([]pullResult, len(targets))
		concurrency := 2
		if localP := h.getLocalTTSProvider(); localP != nil && localP.IsAvailable() {
			concurrency = 4
		}
		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		for idx, item := range targets {
			wg.Add(1)
			go func(i int, desc provider.VoiceDescriptor) {
				defer wg.Done()
				if !req.Force && store.HasSample(ctx, desc.ID) {
					results[i] = pullResult{cached: true}
					return
				}
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					results[i] = pullResult{errMsg: desc.ID + ": cancelled"}
					return
				}
				if desc.Provider != "local_tts" && i > 0 {
					select {
					case <-time.After(150 * time.Millisecond):
					case <-ctx.Done():
						results[i] = pullResult{errMsg: desc.ID + ": cancelled"}
						return
					}
				}
				audio, _, _, _, err := h.synthesizeVoiceSampleStrict(ctx, desc.ID, desc.SampleText)
				if err != nil {
					results[i] = pullResult{errMsg: fmt.Sprintf("%s: %v", desc.ID, err)}
					return
				}
				if _, err := store.SaveSample(ctx, desc.ID, audio); err != nil {
					results[i] = pullResult{errMsg: fmt.Sprintf("%s cache write: %v", desc.ID, err)}
					return
				}
				results[i] = pullResult{pulled: true}
			}(idx, item)
		}
		wg.Wait()
		for _, r := range results {
			switch {
			case r.cached:
				alreadyCached++
			case r.pulled:
				pulledSamples++
			case r.errMsg != "":
				failedSamples++
				if len(warnings) < 12 {
					warnings = append(warnings, r.errMsg)
				}
			}
		}
	}

	updatedVoices, summary, cachedTotal := h.collectVoiceCatalog(ctx, false)
	availableTotal := 0
	for _, v := range updatedVoices {
		if v.Available {
			availableTotal++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"catalog_saved":    catalogSaved,
		"catalog_total":    len(updatedVoices),
		"available_total":  availableTotal,
		"edge_live_synced": edgeSynced,
		"pulled_samples":   pulledSamples,
		"already_cached":   alreadyCached,
		"failed_samples":   failedSamples,
		"cached_total":     cachedTotal,
		"errors":           warnings,
		"providers":        summary,
	})
}

func selectVoicesForLocalPull(catalog []provider.VoiceDescriptor, requested []string, limit int) []provider.VoiceDescriptor {
	byID := make(map[string]provider.VoiceDescriptor, len(catalog))
	for _, v := range catalog {
		byID[strings.ToLower(v.ID)] = v
	}

	out := make([]provider.VoiceDescriptor, 0, limit)
	seen := make(map[string]struct{}, limit)
	appendIfNew := func(v provider.VoiceDescriptor) bool {
		key := strings.ToLower(v.ID)
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
		out = append(out, v)
		return len(out) >= limit
	}

	if len(requested) > 0 {
		for _, rawID := range requested {
			id := strings.TrimSpace(rawID)
			if id == "" {
				continue
			}
			if desc, ok := byID[strings.ToLower(id)]; ok {
				if appendIfNew(desc) {
					return out
				}
			} else {
				if appendIfNew(provider.VoiceDescriptor{ID: id, Available: true}) {
					return out
				}
			}
		}
		return out
	}

	// 默认拉取顺序：已连通本地音色 -> 微软 Edge 精选主推音色 -> 其它当前可用音色
	for _, v := range catalog {
		if v.Provider == "local_tts" && v.Available {
			if appendIfNew(v) {
				return out
			}
		}
	}
	for _, b := range provider.BuiltinEdgeVoices {
		if desc, ok := byID[strings.ToLower(b.VoiceID)]; ok && desc.Available {
			if appendIfNew(desc) {
				return out
			}
		}
	}
	for _, v := range catalog {
		if v.Available {
			if appendIfNew(v) {
				return out
			}
		}
	}
	return out
}

// VoiceDesignRequest 声音设计请求体
type VoiceDesignRequest struct {
	VoicePrompt   string `json:"voice_prompt"`
	PreviewText   string `json:"preview_text"`
	PreferredName string `json:"preferred_name"`
}

// VoiceDesign 自然语言文字捏声音: POST /v1/audio/voices/design
func (h *AudioHandler) VoiceDesign(c *gin.Context) {
	var req VoiceDesignRequest
	if err := c.BindJSON(&req); err != nil || strings.TrimSpace(req.VoicePrompt) == "" {
		c.JSON(400, gin.H{"error": "voice_prompt is required"})
		return
	}

	ds := h.getDashScopeProvider()
	if ds == nil {
		c.JSON(503, gin.H{"error": "dashscope provider is not configured or unavailable"})
		return
	}

	voiceID, previewURL, err := ds.VoiceDesign(c.Request.Context(), req.VoicePrompt, req.PreviewText, req.PreferredName)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"voice_id":    voiceID,
		"preview_url": previewURL,
		"status":      "success",
	})
}

// VoiceCloneRequest 声音克隆请求体
type VoiceCloneRequest struct {
	AudioURL string `json:"audio_url"`
	Prefix   string `json:"prefix"`
}

// VoiceClone 上传短音频克隆声音: POST /v1/audio/voices/clone
func (h *AudioHandler) VoiceClone(c *gin.Context) {
	var req VoiceCloneRequest
	if err := c.BindJSON(&req); err != nil || strings.TrimSpace(req.AudioURL) == "" {
		c.JSON(400, gin.H{"error": "audio_url is required"})
		return
	}

	ds := h.getDashScopeProvider()
	if ds == nil {
		c.JSON(503, gin.H{"error": "dashscope provider is not configured or unavailable"})
		return
	}

	voiceID, err := ds.VoiceCloning(c.Request.Context(), req.AudioURL, req.Prefix)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"voice_id": voiceID,
		"status":   "success",
	})
}

func (h *AudioHandler) recordAudioHistory(voiceID, voiceName, prov, input string, speed float64, audioBytes []byte) {
	gormDB := db.GetDB()
	if gormDB == nil {
		return
	}
	recordID := "aud_" + uuid.New().String()[:12]
	audioURL := fmt.Sprintf("/v1/audio/files/%s", recordID)
	rec := db.AudioGenerationRecord{
		ID:        recordID,
		VoiceID:   voiceID,
		VoiceName: voiceName,
		Provider:  prov,
		InputText: input,
		Speed:     speed,
		Format:    "mp3",
		AudioURL:  audioURL,
		BlobData:  audioBytes,
		Duration:  float64(len(audioBytes)) / 16000.0,
		CreatedAt: time.Now().UTC(),
	}
	_ = gormDB.Create(&rec).Error
}

// ListAudioHistory 返回配音历史列表: GET /v1/audio/history 与 GET /admin/api/v1/multimodal/audio-history
func (h *AudioHandler) ListAudioHistory(c *gin.Context) {
	gormDB := db.GetDB()
	if gormDB == nil {
		c.JSON(200, gin.H{"records": []interface{}{}, "total": 0})
		return
	}
	var list []db.AudioGenerationRecord
	_ = gormDB.Order("created_at desc").Limit(100).Find(&list).Error
	c.JSON(200, gin.H{"records": list, "total": len(list)})
}

// GetAudioFile 根据音频 ID 直取二进制音频流: GET /v1/audio/files/:id
func (h *AudioHandler) GetAudioFile(c *gin.Context) {
	id := c.Param("id")
	gormDB := db.GetDB()
	if gormDB == nil {
		c.Status(404)
		return
	}
	var rec db.AudioGenerationRecord
	if err := gormDB.Where("id = ?", id).First(&rec).Error; err != nil || len(rec.BlobData) == 0 {
		c.Status(404)
		return
	}
	c.Header("Content-Type", "audio/mpeg")
	c.Data(200, "audio/mpeg", rec.BlobData)
}

// SynthesizeSpeechData 供内部后台服务直接调用 TTS 合成并返回音频字节。
func (h *AudioHandler) SynthesizeSpeechData(ctx context.Context, input, voice, model string, speed float64) ([]byte, string, string, error) {
	return h.SynthesizeSpeechDataWithPreferredProvider(ctx, input, voice, model, "", speed)
}

// SynthesizeSpeechDataWithPreferredProvider 按项目能力设置强制优先使用指定 TTS 引擎。
func (h *AudioHandler) SynthesizeSpeechDataWithPreferredProvider(ctx context.Context, input, voice, model, preferredProvider string, speed float64) ([]byte, string, string, error) {
	if strings.TrimSpace(input) == "" {
		return nil, "", "", errors.New("input text is empty")
	}
	if speed <= 0 {
		speed = 1.0
	}
	if model == "" {
		model = "tts-1"
	}
	engine := h.resolveTTSEngine(voice, model, preferredProvider)
	if engine == "openai_auto" {
		engine = "edge_tts"
	}
	// 画布后台 DAG 目前只内置直连引擎；ChatGPT 语音保留给 /v1/audio/speech 标准接口。
	if engine == "chatgpt" {
		return nil, "", "", errors.New("短剧画布暂不支持 ChatGPT TTS 通道，请选择 Edge-TTS / 本地兼容 / 百炼 / 硅基流动")
	}
	audioBytes, usedProvider, usedVoice, _, err := h.synthesizeDirectEngine(ctx, engine, input, voice, model, speed)
	if err == nil && len(audioBytes) > 0 {
		return audioBytes, usedProvider, usedVoice, nil
	}
	if fbAudio, fbProv, fbVoice, ok := h.tryFallbackSynthesis(ctx, engine, input, voice, model, speed); ok && len(fbAudio) > 0 {
		return fbAudio, fbProv, fbVoice, nil
	}
	if err != nil {
		return nil, "", "", err
	}
	return nil, "", "", errors.New("TTS synthesis returned empty audio")
}
