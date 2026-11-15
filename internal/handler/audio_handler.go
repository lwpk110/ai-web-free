package handler

import (
	"fmt"
	"io"
	"strings"
	"time"

	"ai-web-free/httpclient/bogdanfinn"
	"ai-web-free/internal/accounts"
	"ai-web-free/internal/chatgpt"
	"ai-web-free/internal/config"
	"ai-web-free/internal/db"
	"ai-web-free/internal/provider"
	officialtypes "ai-web-free/typings/official"
	chatgptrequestconverter "ai-web-free/conversion/requests/chatgpt"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type AudioHandler struct {
	accountPool *accounts.Pool
	cfg         *config.Config
	runtime     *GatewayRuntime
	registry    *provider.Registry
}

func NewAudioHandler(pool *accounts.Pool, cfg *config.Config) *AudioHandler {
	return &AudioHandler{accountPool: pool, cfg: cfg}
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

func (h *AudioHandler) TTS(c *gin.Context) {
	var original_request officialtypes.TTSAPIRequest
	err := c.BindJSON(&original_request)
	if err != nil {
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

	ttsModel := "tts-1"
	voiceLower := strings.ToLower(strings.TrimSpace(original_request.Voice))
	preferTag := c.GetHeader("X-Prefer-Tag")
	preferProvider := c.GetHeader("X-Prefer-Provider")

	// 0. 微软 Edge-TTS 官方免密高品质音色（晓晓、云希、云扬等）
	isEdgeVoice := strings.HasPrefix(original_request.Voice, "zh-CN-") || strings.HasPrefix(original_request.Voice, "zh-HK-") || strings.HasPrefix(original_request.Voice, "zh-TW-") || strings.HasPrefix(original_request.Voice, "en-US-") || strings.Contains(voiceLower, "xiaoxiao") || strings.Contains(voiceLower, "yunxi") || strings.Contains(voiceLower, "yunyang")
	if isEdgeVoice || preferProvider == "edge_tts" {
		if edge := h.getEdgeTTSProvider(); edge != nil {
			audioBytes, err := edge.Synthesize(c.Request.Context(), original_request.Input, original_request.Voice, original_request.Speed)
			if err == nil && len(audioBytes) > 0 {
				h.recordAudioHistory(original_request.Voice, "微软官方", "edge_tts", original_request.Input, original_request.Speed, audioBytes)
				c.Header("Content-Type", "audio/mpeg")
				c.Header("X-TTS-Provider", "edge_tts")
				c.Data(200, "audio/mpeg", audioBytes)
				return
			}
		}
		// 若 Edge-TTS 握手受阻，无缝映射为剪映免密爆款音色（晓晓 -> zh_female_shuangkuai，云希 -> zh_male_chunhou 等）
		if jy := h.getJianyingProvider(); jy != nil {
			mappedVoice := "zh_female_shuangkuai"
			if strings.Contains(voiceLower, "yunxi") || strings.Contains(voiceLower, "yunyang") || strings.Contains(voiceLower, "yunjian") {
				mappedVoice = "zh_male_chunhou"
			}
			audioBytes, err := jy.Synthesize(c.Request.Context(), original_request.Input, mappedVoice, original_request.Speed)
			if err == nil && len(audioBytes) > 0 {
				h.recordAudioHistory(original_request.Voice, "剪映免密(映射)", "jianying", original_request.Input, original_request.Speed, audioBytes)
				c.Header("Content-Type", "audio/mpeg")
				c.Header("X-TTS-Provider", "jianying")
				c.Header("X-TTS-Fallback", "true")
				c.Data(200, "audio/mpeg", audioBytes)
				return
			}
		}
	}

	// 1. 若指定剪映音色、带有免密偏好或为中文特色音色
	isJianyingVoice := strings.HasPrefix(voiceLower, "zh_") || strings.HasPrefix(voiceLower, "dy_") || strings.HasPrefix(voiceLower, "en_")
	if isJianyingVoice || preferProvider == "jianying" || preferTag == "permanent-free" {
		if jy := h.getJianyingProvider(); jy != nil {
			audioBytes, err := jy.Synthesize(c.Request.Context(), original_request.Input, original_request.Voice, original_request.Speed)
			if err == nil && len(audioBytes) > 0 {
				h.recordAudioHistory(original_request.Voice, "剪映爆款", "jianying", original_request.Input, original_request.Speed, audioBytes)
				c.Header("Content-Type", "audio/mpeg")
				c.Header("X-TTS-Provider", "jianying")
				c.Data(200, "audio/mpeg", audioBytes)
				return
			}
		}
	}

	// 2. 若指定 CosyVoice 或百炼定制音色
	if strings.Contains(voiceLower, "cosyvoice") || strings.HasPrefix(voiceLower, "long") || strings.HasPrefix(voiceLower, "designed_") || strings.HasPrefix(voiceLower, "custom_") {
		if ds := h.getDashScopeProvider(); ds != nil {
			audioBytes, err := ds.SynthesizeSpeech(c.Request.Context(), original_request.Input, original_request.Voice)
			if err == nil && len(audioBytes) > 0 {
				h.recordAudioHistory(original_request.Voice, "CosyVoice", "dashscope", original_request.Input, original_request.Speed, audioBytes)
				c.Header("Content-Type", "audio/mpeg")
				c.Header("X-TTS-Provider", "dashscope")
				c.Data(200, "audio/mpeg", audioBytes)
				return
			}
		}
		if sf := h.getSiliconFlowProvider(); sf != nil {
			audioBytes, err := sf.SynthesizeSpeech(c.Request.Context(), original_request.Input, original_request.Voice)
			if err == nil && len(audioBytes) > 0 {
				h.recordAudioHistory(original_request.Voice, "CosyVoice", "siliconflow", original_request.Input, original_request.Speed, audioBytes)
				c.Header("Content-Type", "audio/mpeg")
				c.Header("X-TTS-Provider", "siliconflow")
				c.Data(200, "audio/mpeg", audioBytes)
				return
			}
		}
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
		// 优雅降级：若 ChatGPT 逆向无可用账号，自动降级至剪映免费通道输出
		if jy := h.getJianyingProvider(); jy != nil {
			mappedVoice := provider.MapOpenAIVoiceToJianying(original_request.Voice)
			audioBytes, jyErr := jy.Synthesize(c.Request.Context(), original_request.Input, mappedVoice, original_request.Speed)
			if jyErr == nil && len(audioBytes) > 0 {
				h.recordAudioHistory(original_request.Voice, "剪映免密降级", "jianying", original_request.Input, original_request.Speed, audioBytes)
				c.Header("Content-Type", "audio/mpeg")
				c.Header("X-TTS-Provider", "jianying")
				c.Header("X-TTS-Fallback", "true")
				c.Data(200, "audio/mpeg", audioBytes)
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
	voice := ttsVoiceMap[original_request.Voice]
	if voice == "" {
		voice = "cove"
	}
	data, status, err := chatgpt.GetTTS(client, account, msgId, convId, voice, format, proxyUrl)
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
	h.recordAudioHistory(original_request.Voice, "ChatGPT官方", "chatgpt", original_request.Input, original_request.Speed, data)
	rt.RecordAudit(c, ttsModel, res, 200, nil)
	c.Data(200, ttsTypeMap[format], data)
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

// ListVoices 返回聚合音色清单与分类元数据: GET /v1/audio/voices
func (h *AudioHandler) ListVoices(c *gin.Context) {
	// 1. 微软 Edge-TTS 官方免密神级音色（晓晓、云希、云扬等）置顶
	voices := provider.GetEdgeTTSVoiceDescriptors()

	// 2. 剪映 200+ 热门爆款音色
	voices = append(voices, provider.GetJianyingVoices()...)

	// 3. 补充百炼/硅基流动 CosyVoice 音色
	voices = append(voices, provider.VoiceDescriptor{
		ID:         "cosyvoice-v1:alex",
		Name:       "Alex (沉稳自然男声)",
		Provider:   "siliconflow",
		Category:   "自然情感",
		Gender:     "male",
		SampleText: "这是由 CosyVoice 语音大模型合成的超自然拟真人声。",
		Tags:       []string{"CosyVoice", "大模型拟真", "永久免费"},
	}, provider.VoiceDescriptor{
		ID:         "cosyvoice-v1:anna",
		Name:       "Anna (温柔知性女声)",
		Provider:   "siliconflow",
		Category:   "自然情感",
		Gender:     "female",
		SampleText: "很高兴与你相遇，希望我的声音能给你带来一丝温暖。",
		Tags:       []string{"CosyVoice", "治愈温暖", "永久免费"},
	}, provider.VoiceDescriptor{
		ID:         "longxiaochun",
		Name:       "龙小春 (亲切女声)",
		Provider:   "dashscope",
		Category:   "自然情感",
		Gender:     "female",
		SampleText: "今天阳光明媚，微风正好，要一起出去走走吗？",
		Tags:       []string{"百炼官方", "多情感", "大模型"},
	})

	c.JSON(200, gin.H{
		"voices": voices,
		"total":  len(voices),
	})
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

