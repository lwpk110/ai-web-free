package handler

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"ai-web-free/httpclient"
	"ai-web-free/httpclient/bogdanfinn"
	"ai-web-free/internal/accounts"
	"ai-web-free/internal/chatgpt"
	"ai-web-free/internal/config"
	officialtypes "ai-web-free/typings/official"

	"github.com/gin-gonic/gin"
)

type ImageHandler struct {
	accountPool *accounts.Pool
	cfg         *config.Config
	runtime     *GatewayRuntime
}

func NewImageHandler(pool *accounts.Pool, cfg *config.Config) *ImageHandler {
	return &ImageHandler{accountPool: pool, cfg: cfg}
}

// SetRuntime 注入网关共享的容灾、代理健康与审计运行时。
func (h *ImageHandler) SetRuntime(rt *GatewayRuntime) {
	h.runtime = rt
}

func (h *ImageHandler) ensureRuntime() *GatewayRuntime {
	if h.runtime != nil {
		return h.runtime
	}
	return &GatewayRuntime{AccountPool: h.accountPool, Config: h.cfg}
}

// ─── Image stream types ──────────────────────────────────────────

type imageStreamChunk struct {
	Object            string `json:"object"`
	Index             int    `json:"index"`
	Total             int    `json:"total"`
	Created           int64  `json:"created"`
	ProgressText      string `json:"progress_text,omitempty"`
	UpstreamEventType string `json:"upstream_event_type,omitempty"`
	Model             string `json:"model,omitempty"`
	AccountEmail      string `json:"_account_email,omitempty"`
	ConversationID    string `json:"_conversation_id,omitempty"`
}

type imageStreamResult struct {
	Object  string                              `json:"object"`
	Index   int                                 `json:"index"`
	Total   int                                 `json:"total"`
	Created int64                               `json:"created"`
	Model   string                              `json:"model,omitempty"`
	Data    []officialtypes.ImageGenerationData `json:"data"`
}

type imageStreamCompleted struct {
	Object  string                              `json:"object"`
	Created int64                               `json:"created"`
	Model   string                              `json:"model,omitempty"`
	Data    []officialtypes.ImageGenerationData `json:"data"`
}

func writeImageStreamHeader(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(200)
}

func writeImageStreamEvent(c *gin.Context, event string, payload interface{}) bool {
	data, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	if event != "" {
		if _, err := c.Writer.WriteString("event: " + event + "\n"); err != nil {
			return false
		}
	}
	if _, err := c.Writer.WriteString("data: "); err != nil {
		return false
	}
	if _, err := c.Writer.Write(data); err != nil {
		return false
	}
	if _, err := c.Writer.WriteString("\n\n"); err != nil {
		return false
	}
	c.Writer.Flush()
	return true
}

func writeImageStreamDone(c *gin.Context) bool {
	if _, err := c.Writer.WriteString("data: [DONE]\n\n"); err != nil {
		return false
	}
	c.Writer.Flush()
	return true
}

// requestStreamFlag 解析 stream 参数,支持 JSON body 的 stream 字段或 ?stream=true 查询参数。
func requestStreamFlag(c *gin.Context, jsonStream bool) bool {
	if jsonStream {
		return true
	}
	if v := strings.ToLower(strings.TrimSpace(c.Query("stream"))); v == "true" || v == "1" || v == "yes" {
		return true
	}
	if v := strings.ToLower(strings.TrimSpace(c.PostForm("stream"))); v == "true" || v == "1" || v == "yes" {
		return true
	}
	return false
}

// isStreamTrue 把任意形式的 stream 字段值转换为 bool。
func isStreamTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes", "on":
		return true
	}
	return false
}

// ─── /v1/images/generations ──────────────────────────────────────

func (h *ImageHandler) Generations(c *gin.Context) {
	var imageRequest officialtypes.ImageGenerationRequest
	err := c.BindJSON(&imageRequest)
	if err != nil {
		c.JSON(400, gin.H{"error": gin.H{
			"message": "Request must be proper JSON",
			"type":    "invalid_request_error",
			"param":   nil,
			"code":    err.Error(),
		}})
		return
	}
	if imageRequest.Prompt == "" {
		c.JSON(400, gin.H{"error": gin.H{
			"message": "Missing required parameter: prompt",
			"type":    "invalid_request_error",
			"param":   "prompt",
			"code":    "missing_required_parameter",
		}})
		return
	}
	if imageRequest.N <= 0 {
		imageRequest.N = 1
	}
	if imageRequest.N > 10 {
		imageRequest.N = 10
	}
	if imageRequest.ResponseFormat == "" {
		imageRequest.ResponseFormat = "b64_json"
	}

	effectivePrompt := chatgpt.DecorateImagePrompt(imageRequest.Prompt, imageRequest.Model, imageRequest.Size, imageRequest.Quality)
	stream := requestStreamFlag(c, imageRequest.Stream)
	h.executeImagePipeline(c, imagePipelineSpec{
		Capability:     accounts.CapImageGenerate.Name,
		Model:          imageRequest.Model,
		Prompt:         effectivePrompt,
		RevisedPrompt:  imageRequest.Prompt,
		N:              imageRequest.N,
		ResponseFormat: imageRequest.ResponseFormat,
		Stream:         stream,
	})
}

type imagePipelineSpec struct {
	Capability     string
	Model          string
	Prompt         string
	RevisedPrompt  string
	N              int
	ResponseFormat string
	Stream         bool
	AsVariation    bool
	IsEditFlow     bool
	Sources        []editImageInput
}

type imageAttemptBundle struct {
	client     *bogdanfinn.TlsClient
	turnStile  *chatgpt.TurnStile
	references []chatgpt.ImageEditReference
	items      []officialtypes.ImageGenerationData
}

func ensureImageClient(acct *accounts.Account) *bogdanfinn.TlsClient {
	if acct != nil {
		if c, ok := acct.Client.(*bogdanfinn.TlsClient); ok && c != nil {
			return c
		}
		client := setupClientWithProxy(acct.Proxy)
		client.SetCookies("https://chatgpt.com", chatgpt.BasicCookies)
		return client
	}
	client := setupClientWithProxy("")
	client.SetCookies("https://chatgpt.com", chatgpt.BasicCookies)
	return client
}

func materializeImageResults(
	imgClient *bogdanfinn.TlsClient,
	account *accounts.Account,
	imageResults []chatgpt.ImageGenerationResult,
	revisedPrompt string,
	responseFormat string,
	maxTotal int,
	currentCount int,
) ([]officialtypes.ImageGenerationData, error) {
	items := make([]officialtypes.ImageGenerationData, 0, len(imageResults))
	for _, imageResult := range imageResults {
		item := officialtypes.ImageGenerationData{
			RevisedPrompt: revisedPrompt,
		}
		if responseFormat == "b64_json" {
			if imageResult.B64JSON != "" {
				item.B64JSON = imageResult.B64JSON
			} else if imageResult.URL != "" {
				imageBytes, err := chatgpt.DownloadImageBytes(imgClient, imageResult.URL, account)
				if err != nil {
					return nil, err
				}
				item.B64JSON = base64.StdEncoding.EncodeToString(imageBytes)
			}
		} else {
			item.URL = imageResult.URL
			if item.URL == "" && imageResult.B64JSON != "" {
				item.B64JSON = imageResult.B64JSON
			}
		}
		items = append(items, item)
		if maxTotal > 0 && currentCount+len(items) >= maxTotal {
			break
		}
	}
	return items, nil
}

func (h *ImageHandler) executeImagePipeline(c *gin.Context, spec imagePipelineSpec) {
	rt := h.ensureRuntime()
	res, err := rt.RunWithFailover(c, h.accountPool, h.cfg, FailoverOptions{
		NeedsPaid:  true,
		Capability: spec.Capability,
		Model:      spec.Model,
		IsImage:    true,
	}, func(acct *accounts.Account, _ *bogdanfinn.TlsClient) AttemptOutcome {
		if acct.Token == "" {
			return AttemptOutcome{StatusCode: 400, Err: errMissingAccessToken}
		}
		if !AccountSatisfies(acct, accounts.CapImageGenerate.Name) || !AccountSatisfies(acct, spec.Capability) {
			return AttemptOutcome{StatusCode: 403, Err: errAccountCapDenied}
		}
		imgClient := ensureImageClient(acct)
		turnStile, status, callErr := chatgpt.InitSentinel(imgClient, acct, acct.Proxy, 0)
		if callErr != nil {
			if status == http.StatusUnauthorized {
				h.accountPool.MarkExpired(acct, callErr.Error())
			}
			return AttemptOutcome{StatusCode: status, Err: callErr}
		}

		var references []chatgpt.ImageEditReference
		if spec.IsEditFlow {
			references = make([]chatgpt.ImageEditReference, 0, len(spec.Sources))
			for _, src := range spec.Sources {
				uploaded, upStatus, upErr := chatgpt.UploadFile(imgClient, acct, acct.Proxy, src.Filename, src.ContentType, src.Data)
				if upErr != nil {
					return AttemptOutcome{
						StatusCode: upStatus,
						Err:        chatgpt.NewUpstreamError("upload image failed ("+src.Filename+")", upStatus, upErr.Error()),
					}
				}
				references = append(references, chatgpt.ImageEditReference{
					FileID:   uploaded.FileID,
					Width:    uploaded.Width,
					Height:   uploaded.Height,
					Size:     int(uploaded.Bytes),
					MimeType: uploaded.MimeType,
					Filename: uploaded.Filename,
				})
			}
		}

		var imageResults []chatgpt.ImageGenerationResult
		var upstreamText string
		var genErr error
		if spec.IsEditFlow {
			imageResults, upstreamText, genErr = chatgpt.GeneratePictureConversationImagesWithReferences(imgClient, acct, turnStile, spec.Prompt, spec.Model, acct.Proxy, references)
		} else {
			imageResults, upstreamText, genErr = chatgpt.GeneratePictureConversationImages(imgClient, acct, turnStile, spec.Prompt, spec.Model, acct.Proxy)
		}
		if genErr != nil {
			return AttemptOutcome{Err: genErr}
		}
		if len(imageResults) == 0 {
			if chatgpt.LooksLikeImageLimit(http.StatusTooManyRequests, upstreamText) {
				return AttemptOutcome{
					StatusCode: http.StatusTooManyRequests,
					Err:        chatgpt.NewUpstreamError("image generation limit", http.StatusTooManyRequests, upstreamText),
				}
			}
			msg := "No image result found in response"
			if upstreamText != "" {
				msg = "No image result found in response: " + upstreamText
			}
			return AttemptOutcome{StatusCode: 500, Err: fmt.Errorf("%s", msg)}
		}
		items, dlErr := materializeImageResults(imgClient, acct, imageResults, spec.RevisedPrompt, spec.ResponseFormat, spec.N, 0)
		if dlErr != nil {
			return AttemptOutcome{StatusCode: 500, Err: dlErr}
		}
		return AttemptOutcome{
			Payload: imageAttemptBundle{
				client:     imgClient,
				turnStile:  turnStile,
				references: references,
				items:      items,
			},
			StatusCode: 200,
		}
	})
	if err != nil {
		errStatus := res.Outcome.StatusCode
		if uStatus, _, ok := chatgpt.ExtractUpstreamStatus(err); ok && uStatus > 0 {
			errStatus = uStatus
		}
		if errStatus <= 0 {
			errStatus = 500
		}
		if err == errMissingAccessToken {
			c.JSON(400, gin.H{"error": "Images API requires a logged-in ChatGPT access token."})
			rt.RecordAudit(c, spec.Model, res, 400, err)
			return
		}
		if err == errAccountCapDenied {
			c.JSON(403, gin.H{"error": "Images API requires a logged-in ChatGPT account."})
			rt.RecordAudit(c, spec.Model, res, 403, err)
			return
		}
		if res.Attempts == 0 {
			respondError(c, 400, err)
			rt.RecordAudit(c, spec.Model, res, 400, err)
			return
		}
		c.JSON(errStatus, gin.H{"error": gin.H{
			"message": err.Error(),
			"type":    "image_generation_error",
			"param":   nil,
			"code":    "image_generation_error",
		}})
		rt.RecordAudit(c, spec.Model, res, errStatus, err)
		return
	}

	account := res.Account
	bundle := res.Outcome.Payload.(imageAttemptBundle)
	data := make([]officialtypes.ImageGenerationData, 0, spec.N)
	data = append(data, bundle.items...)

	if spec.Stream {
		writeImageStreamHeader(c)
		writeImageStreamEvent(c, "image.generation.chunk", imageStreamChunk{
			Object:       "image.generation.chunk",
			Index:        0,
			Total:        spec.N,
			Created:      0,
			Model:        spec.Model,
			ProgressText: fmt.Sprintf("Generating image 1/%d ...", spec.N),
		})
		for idx, item := range bundle.items {
			writeImageStreamEvent(c, "image.generation.result", imageStreamResult{
				Object:  "image.generation.result",
				Index:   idx,
				Total:   spec.N,
				Created: 0,
				Model:   spec.Model,
				Data:    []officialtypes.ImageGenerationData{item},
			})
		}
	}

	for i := 1; i < spec.N && len(data) < spec.N; i++ {
		if spec.Stream {
			writeImageStreamEvent(c, "image.generation.chunk", imageStreamChunk{
				Object:       "image.generation.chunk",
				Index:        i,
				Total:        spec.N,
				Created:      0,
				Model:        spec.Model,
				ProgressText: fmt.Sprintf("Generating image %d/%d ...", i+1, spec.N),
			})
		}
		var imageResults []chatgpt.ImageGenerationResult
		var upstreamText string
		var genErr error
		if spec.IsEditFlow {
			imageResults, upstreamText, genErr = chatgpt.GeneratePictureConversationImagesWithReferences(bundle.client, account, bundle.turnStile, spec.Prompt, spec.Model, account.Proxy, bundle.references)
		} else {
			imageResults, upstreamText, genErr = chatgpt.GeneratePictureConversationImages(bundle.client, account, bundle.turnStile, spec.Prompt, spec.Model, account.Proxy)
		}
		if genErr != nil {
			h.writeImageFailure(c, spec.Stream, i, spec.N, "image_generation_error", genErr)
			rt.RecordAudit(c, spec.Model, res, 500, genErr)
			return
		}
		items, dlErr := materializeImageResults(bundle.client, account, imageResults, spec.RevisedPrompt, spec.ResponseFormat, spec.N, len(data))
		if dlErr != nil {
			h.writeImageFailure(c, spec.Stream, i, spec.N, "image_download_error", dlErr)
			rt.RecordAudit(c, spec.Model, res, 500, dlErr)
			return
		}
		for _, item := range items {
			data = append(data, item)
			if spec.Stream {
				writeImageStreamEvent(c, "image.generation.result", imageStreamResult{
					Object:  "image.generation.result",
					Index:   len(data) - 1,
					Total:   spec.N,
					Created: 0,
					Model:   spec.Model,
					Data:    []officialtypes.ImageGenerationData{item},
				})
			}
		}
		if len(imageResults) == 0 && upstreamText != "" {
			errMsg := fmt.Errorf("No image result found in response: %s", upstreamText)
			h.writeImageFailure(c, spec.Stream, i, spec.N, "image_generation_error", errMsg)
			rt.RecordAudit(c, spec.Model, res, 500, errMsg)
			return
		}
	}

	if len(data) == 0 {
		noImgErr := fmt.Errorf("No image result found in response")
		h.writeImageFailure(c, spec.Stream, 0, spec.N, "image_generation_error", noImgErr)
		rt.RecordAudit(c, spec.Model, res, 500, noImgErr)
		return
	}

	rt.RecordAudit(c, spec.Model, res, 200, nil)
	if spec.Stream {
		writeImageStreamEvent(c, "image.generation.completed", imageStreamCompleted{
			Object:  "image.generation.completed",
			Created: 0,
			Model:   spec.Model,
			Data:    data,
		})
		writeImageStreamDone(c)
		return
	}
	if spec.IsEditFlow {
		if spec.AsVariation {
			c.JSON(200, officialtypes.NewImageVariationResponse(data))
		} else {
			c.JSON(200, officialtypes.NewImageEditResponse(data))
		}
		return
	}
	c.JSON(200, officialtypes.NewImageGenerationResponse(data))
}

func (h *ImageHandler) writeImageFailure(c *gin.Context, stream bool, idx, total int, errType string, err error) {
	if stream {
		writeImageStreamEvent(c, "image.generation.error", gin.H{
			"object":  "image.generation.error",
			"index":   idx,
			"total":   total,
			"message": err.Error(),
		})
		writeImageStreamDone(c)
		return
	}
	c.JSON(500, gin.H{"error": gin.H{
		"message": err.Error(),
		"type":    errType,
		"param":   nil,
		"code":    errType,
	}})
}

// ─── Image Edit / Variation types ────────────────────────────────

// editImageInput 一张待编辑/变体使用的源图,支持 multipart 文件上传与 JSON 引用。
type editImageInput struct {
	Data        []byte
	Filename    string
	ContentType string
}

// imageEditImageReferenceFields 与 chatgpt2api/api/image_inputs.IMAGE_REFERENCE_FIELDS 对齐。
var imageEditImageReferenceFields = map[string]bool{
	"image":       true,
	"image[]":     true,
	"images":      true,
	"images[]":    true,
	"image_url":   true,
	"image_url[]": true,
}

func normalizeImageEditImages(rawImages []interface{}) []editImageInput {
	out := make([]editImageInput, 0, len(rawImages))
	for _, raw := range rawImages {
		switch v := raw.(type) {
		case *multipart.FileHeader:
			if v == nil {
				continue
			}
			f, err := v.Open()
			if err != nil {
				continue
			}
			data, err := io.ReadAll(f)
			f.Close()
			if err != nil || len(data) == 0 {
				continue
			}
			ct := v.Header.Get("Content-Type")
			if ct == "" {
				ct = "image/png"
			}
			name := v.Filename
			if name == "" {
				name = "image.png"
			}
			out = append(out, editImageInput{Data: data, Filename: name, ContentType: ct})
		case editImageInput:
			if len(v.Data) > 0 {
				out = append(out, v)
			}
		default:
			// JSON 形态的 image 引用由 collectImageEditSourcesFromValue 处理,这里跳过
		}
	}
	return out
}

func imageEditReadJSONImage(data []byte, filename, contentType string) (editImageInput, error) {
	if len(data) == 0 {
		return editImageInput{}, fmt.Errorf("image data is empty")
	}
	if filename == "" {
		filename = "image.png"
	}
	if contentType == "" {
		contentType = "image/png"
	}
	return editImageInput{Data: data, Filename: filename, ContentType: contentType}, nil
}

func imageEditDecodeDataURL(url string) (editImageInput, error) {
	comma := strings.Index(url, ",")
	if comma < 0 {
		return editImageInput{}, fmt.Errorf("invalid data URL")
	}
	header := url[:comma]
	payload := url[comma+1:]
	contentType := "image/png"
	if semi := strings.Index(header, ";"); semi > 5 {
		contentType = header[5:semi]
		if !strings.HasPrefix(contentType, "image/") {
			return editImageInput{}, fmt.Errorf("data URL must be an image, got %q", contentType)
		}
	}
	dec := base64.StdEncoding
	if strings.Contains(header, ";base64") {
		dec = base64.StdEncoding
	} else {
		dec = base64.URLEncoding
	}
	raw, err := dec.DecodeString(payload)
	if err != nil {
		raw, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return editImageInput{}, err
		}
		contentType = "image/png"
	}
	ext := "png"
	switch strings.ToLower(contentType) {
	case "image/jpeg":
		ext = "jpg"
	case "image/gif":
		ext = "gif"
	case "image/webp":
		ext = "webp"
	}
	return imageEditReadJSONImage(raw, "image_url."+ext, contentType)
}

func imageEditDownloadHTTPURL(client httpclient.HttpClient, url string) (editImageInput, error) {
	if client == nil {
		client = bogdanfinn.NewStdClient()
	}
	headers := httpclient.Headers{
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
		"Accept":     "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8",
	}
	resp, err := client.Request(httpclient.GET, url, headers, nil, nil)
	if err != nil {
		return editImageInput{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return editImageInput{}, fmt.Errorf("download image failed: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return editImageInput{}, err
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	filename := "image_url"
	if idx := strings.LastIndex(url, "/"); idx >= 0 && idx < len(url)-1 {
		filename = url[idx+1:]
	}
	if dot := strings.Index(filename, "?"); dot >= 0 {
		filename = filename[:dot]
	}
	if filename == "" {
		filename = "image_url.png"
	}
	return imageEditReadJSONImage(data, filename, contentType)
}

func imageEditConvertURL(client httpclient.HttpClient, raw string) (editImageInput, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return editImageInput{}, false, nil
	}
	if strings.HasPrefix(raw, "data:") {
		item, err := imageEditDecodeDataURL(raw)
		return item, true, err
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		item, err := imageEditDownloadHTTPURL(client, raw)
		return item, true, err
	}
	// 非 URL、非 data URL:当 base64 处理
	item, err := imageEditReadJSONImage([]byte(raw), "image.png", "image/png")
	return item, true, err
}

func resolveEditImageSources(c *gin.Context, body map[string]interface{}, client httpclient.HttpClient) ([]editImageInput, error) {
	out := make([]editImageInput, 0, 2)
	appendValue := func(v interface{}) error {
		switch t := v.(type) {
		case string:
			item, ok, err := imageEditConvertURL(client, t)
			if err != nil {
				return err
			}
			if ok {
				out = append(out, item)
			}
		case map[string]interface{}:
			if urlVal, ok := t["image_url"]; ok {
				if s, ok := urlVal.(string); ok {
					item, _, err := imageEditConvertURL(client, s)
					if err != nil {
						return err
					}
					out = append(out, item)
				}
			} else if u, ok := t["url"]; ok {
				if s, ok := u.(string); ok {
					item, _, err := imageEditConvertURL(client, s)
					if err != nil {
						return err
					}
					out = append(out, item)
				}
			} else if b64, ok := t["b64_json"].(string); ok && b64 != "" {
				item, err := imageEditReadJSONImage([]byte(b64), "image.png", "image/png")
				if err != nil {
					return err
				}
				out = append(out, item)
			} else if b64, ok := t["base64"].(string); ok && b64 != "" {
				item, err := imageEditReadJSONImage([]byte(b64), "image.png", "image/png")
				if err != nil {
					return err
				}
				out = append(out, item)
			}
		}
		return nil
	}

	for _, key := range []string{"images", "image", "image_url"} {
		val, ok := body[key]
		if !ok || val == nil {
			continue
		}
		switch arr := val.(type) {
		case []interface{}:
			for _, item := range arr {
				if err := appendValue(item); err != nil {
					return nil, err
				}
			}
		case string:
			if err := appendValue(arr); err != nil {
				return nil, err
			}
		case map[string]interface{}:
			if err := appendValue(arr); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// collectResponsesAPIParts 从 Responses API 风格的 input / content / messages 提取文本和图片。
func collectResponsesAPIParts(raw interface{}) (string, []string) {
	if raw == nil {
		return "", nil
	}
	var textParts []string
	var imageURLs []string

	appendFromContent := func(content interface{}) {
		switch c := content.(type) {
		case string:
			if strings.TrimSpace(c) != "" {
				textParts = append(textParts, c)
			}
		case []interface{}:
			for _, item := range c {
				part, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				partType := strings.ToLower(strings.TrimSpace(stringFromAny(part["type"])))
				switch partType {
				case "input_text", "text", "output_text":
					if s := stringFromAny(part["text"]); s != "" {
						textParts = append(textParts, s)
					}
				case "input_image", "image", "image_url":
					switch u := part["image_url"].(type) {
					case string:
						imageURLs = append(imageURLs, u)
					case map[string]interface{}:
						if s := stringFromAny(u["url"]); s != "" {
							imageURLs = append(imageURLs, s)
						}
					}
					if s := stringFromAny(part["url"]); s != "" {
						imageURLs = append(imageURLs, s)
					}
				}
			}
		}
	}

	switch v := raw.(type) {
	case string:
		textParts = append(textParts, v)
	case map[string]interface{}:
		appendFromContent(v["content"])
	case []interface{}:
		for _, item := range v {
			switch m := item.(type) {
			case string:
				textParts = append(textParts, m)
			case map[string]interface{}:
				partType := strings.ToLower(strings.TrimSpace(stringFromAny(m["type"])))
				if partType == "input_image" || partType == "image" || partType == "image_url" {
					switch u := m["image_url"].(type) {
					case string:
						imageURLs = append(imageURLs, u)
					case map[string]interface{}:
						if s := stringFromAny(u["url"]); s != "" {
							imageURLs = append(imageURLs, s)
						}
					}
					if s := stringFromAny(m["url"]); s != "" {
						imageURLs = append(imageURLs, s)
					}
					continue
				}
				appendFromContent(m["content"])
			}
		}
	}
	return strings.Join(textParts, "\n"), imageURLs
}

func stringFromAny(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// ─── /v1/images/edits 与 /v1/images/variations ───────────────────

func (h *ImageHandler) Edits(c *gin.Context) {
	h.runImageEditFlow(c, false)
}

func (h *ImageHandler) Variations(c *gin.Context) {
	h.runImageEditFlow(c, true)
}

func (h *ImageHandler) runImageEditFlow(c *gin.Context, asVariation bool) {
	contentType := strings.Split(c.GetHeader("Content-Type"), ";")[0]
	contentType = strings.ToLower(strings.TrimSpace(contentType))

	var imageSources []editImageInput
	var prompt, model, responseFormat string
	var n int
	var stream bool

	parseFormFields := func(promptVal, modelVal, nVal, responseFormatVal, streamVal string) {
		prompt = strings.TrimSpace(promptVal)
		model = strings.TrimSpace(modelVal)
		responseFormat = strings.TrimSpace(responseFormatVal)
		if nVal != "" {
			if v, err := strconv.Atoi(nVal); err == nil {
				n = v
			}
		}
		stream = isStreamTrue(streamVal)
	}

	if contentType == "application/json" {
		var body struct {
			Prompt         string                          `json:"prompt"`
			Model          string                          `json:"model"`
			N              int                             `json:"n"`
			Size           string                          `json:"size"`
			ResponseFormat string                          `json:"response_format"`
			Stream         bool                            `json:"stream"`
			Images         []officialtypes.ImageEditSource `json:"images,omitempty"`
			Image          *officialtypes.ImageEditSource  `json:"image,omitempty"`
			ImageURL       interface{}                     `json:"image_url,omitempty"`
			Input          interface{}                     `json:"input,omitempty"`
			Content        interface{}                     `json:"content,omitempty"`
			Messages       interface{}                     `json:"messages,omitempty"`
		}
		if err := c.BindJSON(&body); err != nil {
			c.JSON(400, gin.H{"error": gin.H{
				"message": "Request must be proper JSON",
				"type":    "invalid_request_error",
				"param":   nil,
				"code":    err.Error(),
			}})
			return
		}
		prompt = strings.TrimSpace(body.Prompt)
		model = body.Model
		n = body.N
		responseFormat = body.ResponseFormat
		stream = body.Stream

		client := bogdanfinn.NewStdClient()

		for _, src := range body.Images {
			item, _, err := imageEditConvertURL(client, src.ImageURL)
			if err != nil {
				c.JSON(400, gin.H{"error": gin.H{
					"message": "invalid image reference: " + err.Error(),
					"type":    "invalid_request_error",
					"param":   "images",
					"code":    "invalid_image",
				}})
				return
			}
			if len(item.Data) > 0 {
				imageSources = append(imageSources, item)
			}
		}
		if body.Image != nil {
			item, _, err := imageEditConvertURL(client, body.Image.ImageURL)
			if err != nil {
				c.JSON(400, gin.H{"error": gin.H{
					"message": "invalid image reference: " + err.Error(),
					"type":    "invalid_request_error",
					"param":   "image",
					"code":    "invalid_image",
				}})
				return
			}
			if len(item.Data) > 0 {
				imageSources = append(imageSources, item)
			}
		}
		if body.ImageURL != nil {
			switch t := body.ImageURL.(type) {
			case string:
				item, _, err := imageEditConvertURL(client, t)
				if err == nil && len(item.Data) > 0 {
					imageSources = append(imageSources, item)
				}
			case map[string]interface{}:
				if u, ok := t["url"].(string); ok {
					item, _, err := imageEditConvertURL(client, u)
					if err == nil && len(item.Data) > 0 {
						imageSources = append(imageSources, item)
					}
				}
			}
		}

		promptFromParts, imageParts := collectResponsesAPIParts(body.Input)
		if len(imageParts) == 0 {
			if p, imgs := collectResponsesAPIParts(body.Content); len(imgs) > 0 {
				promptFromParts = p
				imageParts = imgs
			}
		}
		if len(imageParts) == 0 {
			if p, imgs := collectResponsesAPIParts(body.Messages); len(imgs) > 0 {
				promptFromParts = p
				imageParts = imgs
			}
		}
		for _, p := range imageParts {
			item, _, err := imageEditConvertURL(client, p)
			if err != nil {
				c.JSON(400, gin.H{"error": gin.H{
					"message": "invalid image reference: " + err.Error(),
					"type":    "invalid_request_error",
					"param":   "input",
					"code":    "invalid_image",
				}})
				return
			}
			if len(item.Data) > 0 {
				imageSources = append(imageSources, item)
			}
		}
		if prompt == "" {
			prompt = strings.TrimSpace(promptFromParts)
		}
	} else {
		form, err := c.MultipartForm()
		if err != nil {
			c.JSON(400, gin.H{"error": gin.H{
				"message": "Request must be multipart/form-data or application/json: " + err.Error(),
				"type":    "invalid_request_error",
				"param":   nil,
				"code":    "invalid_multipart",
			}})
			return
		}
		parseFormFields(
			strings.TrimSpace(c.PostForm("prompt")),
			strings.TrimSpace(c.PostForm("model")),
			c.PostForm("n"),
			strings.TrimSpace(c.PostForm("response_format")),
			c.PostForm("stream"),
		)
		rawSources := make([]interface{}, 0, 4)
		for _, key := range []string{"image", "image[]", "images", "images[]"} {
			if vs, ok := form.File[key]; ok {
				for _, fh := range vs {
					rawSources = append(rawSources, fh)
				}
			}
		}
		if vs, ok := form.Value["image_url"]; ok {
			client := bogdanfinn.NewStdClient()
			for _, s := range vs {
				item, _, err := imageEditConvertURL(client, s)
				if err == nil && len(item.Data) > 0 {
					imageSources = append(imageSources, item)
				}
			}
		}
		imageSources = append(imageSources, normalizeImageEditImages(rawSources)...)
	}

	if asVariation {
		if prompt == "" {
			prompt = "Generate a variation of the provided image(s). Return only the generated image, not a text description."
		}
	}

	if len(imageSources) == 0 {
		c.JSON(400, gin.H{"error": gin.H{
			"message": "Missing required image input. Provide multipart 'image'/'images' field, or JSON 'image'/'images' field with image_url.",
			"type":    "invalid_request_error",
			"param":   "image",
			"code":    "missing_required_parameter",
		}})
		return
	}
	if !asVariation && prompt == "" {
		c.JSON(400, gin.H{"error": gin.H{
			"message": "Missing required parameter: prompt",
			"type":    "invalid_request_error",
			"param":   "prompt",
			"code":    "missing_required_parameter",
		}})
		return
	}
	if n <= 0 {
		n = 1
	}
	if n > 10 {
		n = 10
	}
	if responseFormat == "" {
		responseFormat = "b64_json"
	}
	stream = requestStreamFlag(c, stream)

	capName := accounts.CapImageEdit.Name
	if asVariation {
		capName = accounts.CapImageVariation.Name
	}
	h.executeImagePipeline(c, imagePipelineSpec{
		Capability:     capName,
		Model:          model,
		Prompt:         prompt,
		RevisedPrompt:  prompt,
		N:              n,
		ResponseFormat: responseFormat,
		Stream:         stream,
		AsVariation:    asVariation,
		IsEditFlow:     true,
		Sources:        imageSources,
	})
}
