package handler

import (
	"fmt"
	"io"
	"strings"

	"ai-web-free/httpclient/bogdanfinn"
	"ai-web-free/internal/accounts"
	"ai-web-free/internal/chatgpt"
	"ai-web-free/internal/config"
	officialtypes "ai-web-free/typings/official"
	chatgptrequestconverter "ai-web-free/conversion/requests/chatgpt"

	"github.com/gin-gonic/gin"
)

type AudioHandler struct {
	accountPool *accounts.Pool
	cfg         *config.Config
	runtime     *GatewayRuntime
}

func NewAudioHandler(pool *accounts.Pool, cfg *config.Config) *AudioHandler {
	return &AudioHandler{accountPool: pool, cfg: cfg}
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
