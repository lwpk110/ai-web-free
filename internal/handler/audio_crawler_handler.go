package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"ai-web-free/internal/provider"

	"github.com/gin-gonic/gin"
)

// VoiceCrawlRequest 描述音色检索与网页爬虫采集请求。
type VoiceCrawlRequest struct {
	Query string `json:"query"`
	URL   string `json:"url"`
	Limit int    `json:"limit"`
}

// VoiceCrawlImportRequest 将网页爬虫找到的样音下载缓存到本地并接入云端克隆/系统音色库。
type VoiceCrawlImportRequest struct {
	URL         string   `json:"url" binding:"required"`
	SourcePage  string   `json:"source_page"`
	Name        string   `json:"name"`
	Role        string   `json:"role"`
	Transcript  string   `json:"transcript"`
	Gender      string   `json:"gender"`
	Locale      string   `json:"locale"`
	Tags        []string `json:"tags"`
	CloneEngine string   `json:"clone_engine"` // "auto" | "siliconflow" | "dashscope" | "cache_only"
	Prefix      string   `json:"prefix"`
}

func (h *AudioHandler) resolveVoiceCrawlOpts(limit int) provider.MediaCrawlOptions {
	opts := provider.MediaCrawlOptions{
		Kind:  "voice",
		Limit: limit,
	}
	if h.crawlOpts != nil {
		opts = *h.crawlOpts
		opts.Kind = "voice"
		if limit > 0 {
			opts.Limit = limit
		}
	}
	return opts
}

func (h *AudioHandler) resolveCustomVoiceBeforeSynth(ctx context.Context, voice string) (engine string, clonedVoice string, err error) {
	store := h.getVoiceCatalogStore()
	rec, found := store.FindCustomVoice(ctx, voice)
	if !found {
		return "", "", nil
	}
	if rec.CloneStatus == "cloned" && strings.TrimSpace(rec.ClonedVoiceID) != "" && strings.TrimSpace(rec.CloneEngine) != "" {
		return rec.CloneEngine, rec.ClonedVoiceID, nil
	}
	return "", "", fmt.Errorf("自定义音色 %s (%s) 尚处于本地样音缓存状态，请先配置 SILICONFLOW_API_KEY 或 DASHSCOPE_API_KEY 并在工坊点击「一键云端复刻」", rec.Name, rec.ID)
}

func (h *AudioHandler) searchExistingSystemVoices(ctx context.Context, query string, limit int) []provider.VoiceDescriptor {
	voices, _, _ := h.collectVoiceCatalog(ctx, false)
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		if len(voices) > limit {
			return voices[:limit]
		}
		return voices
	}
	tokens := strings.Fields(q)
	matched := make([]provider.VoiceDescriptor, 0, limit)
	for _, v := range voices {
		haystack := strings.ToLower(v.ID + " " + v.Name + " " + v.Category + " " + strings.Join(v.Tags, " ") + " " + v.SampleText + " " + v.Locale)
		allHit := true
		for _, tok := range tokens {
			if !strings.Contains(haystack, tok) {
				allHit = false
				break
			}
		}
		if allHit {
			matched = append(matched, v)
			if len(matched) >= limit {
				break
			}
		}
	}
	return matched
}

// ExecuteVoiceCrawl 供 `/v1/audio/voices/crawl` 与统一 `/v1/media/crawl` 共享调用。
func (h *AudioHandler) ExecuteVoiceCrawl(ctx context.Context, query, rawURL string, limit int) ([]provider.VoiceDescriptor, provider.MediaCrawlReport) {
	if limit <= 0 {
		limit = 10
	}
	existing := h.searchExistingSystemVoices(ctx, query, 8)
	report := provider.SearchAndCrawlMedia(ctx, query, rawURL, h.resolveVoiceCrawlOpts(limit))
	return existing, report
}

// ExecuteVoiceImportAndClone 下载样音到本地缓存并执行云端音色克隆（无 Key 时保留为待复刻系统记录）。
func (h *AudioHandler) ExecuteVoiceImportAndClone(ctx context.Context, req VoiceCrawlImportRequest) (provider.CustomVoiceRecord, provider.VoiceDescriptor, error) {
	rawURL := strings.TrimSpace(req.URL)
	if rawURL == "" {
		return provider.CustomVoiceRecord{}, provider.VoiceDescriptor{}, errors.New("url is required")
	}

	opts := h.resolveVoiceCrawlOpts(1)
	audioBytes, mimeType, err := provider.DownloadMediaBytes(ctx, rawURL, "voice", opts)
	if err != nil {
		return provider.CustomVoiceRecord{}, provider.VoiceDescriptor{}, err
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "网页采集音色"
	}
	id := provider.BuildSystemCustomVoiceID(name + "_" + rawURL)
	store := h.getVoiceCatalogStore()
	if store.Enabled() {
		_, _ = store.SaveSample(ctx, id, audioBytes)
	}

	rec := provider.CustomVoiceRecord{
		ID:          id,
		Name:        name,
		Role:        strings.TrimSpace(req.Role),
		Category:    "系统克隆库",
		Gender:      firstNonEmptyString(req.Gender, "female"),
		Locale:      firstNonEmptyString(req.Locale, "zh-CN"),
		Transcript:  firstNonEmptyString(req.Transcript, "你好，这是从网页搜索采集并缓存到系统库的参考音色。"),
		Tags:        req.Tags,
		SourceURL:   rawURL,
		SourcePage:  strings.TrimSpace(req.SourcePage),
		SampleBytes: len(audioBytes),
	}

	rec = h.attemptCloudVoiceClone(ctx, rec, audioBytes, mimeType, req.CloneEngine, req.Prefix, rawURL)
	saved, saveErr := store.UpsertCustomVoice(ctx, rec)
	if saveErr != nil {
		return rec, provider.VoiceDescriptor{}, saveErr
	}

	sfActive := 0
	if sf := h.getSiliconFlowProvider(); sf != nil {
		sfActive = sf.Stats().Active
	}
	dsActive := 0
	if ds := h.getDashScopeProvider(); ds != nil {
		dsActive = ds.Stats().Active
	}
	desc := provider.CustomVoiceToDescriptor(saved, sfActive > 0, dsActive > 0)
	if len(audioBytes) > 0 && store.Enabled() {
		desc.CachedLocal = true
	}
	return saved, desc, nil
}

func (h *AudioHandler) attemptCloudVoiceClone(
	ctx context.Context,
	rec provider.CustomVoiceRecord,
	audioBytes []byte,
	mimeType string,
	preferEngine string,
	prefix string,
	publicAudioURL string,
) provider.CustomVoiceRecord {
	engine := strings.ToLower(strings.TrimSpace(preferEngine))
	if engine == "" {
		engine = "auto"
	}
	if engine == "cache_only" {
		rec.CloneStatus = "cached_pending_key"
		rec.Error = "已保存本地参考样音，随时可在配音工坊点击「一键云端复刻」"
		return rec
	}

	// 1. 优先尝试硅基流动 CosyVoice2-0.5B（直接将本地缓存音频字节编码上传，不受内网回环 URL 限制）
	if engine == "auto" || engine == "siliconflow" {
		if sf := h.getSiliconFlowProvider(); sf != nil && sf.Stats().Active > 0 {
			uri, err := sf.UploadReferenceVoice(ctx, firstNonEmptyString(prefix, rec.ID), rec.Transcript, audioBytes, mimeType)
			if err == nil && uri != "" {
				rec.CloneEngine = "siliconflow"
				rec.ClonedVoiceID = uri
				rec.CloneStatus = "cloned"
				rec.Error = ""
				return rec
			}
			if engine == "siliconflow" && err != nil {
				rec.CloneStatus = "clone_failed"
				rec.Error = err.Error()
				return rec
			}
			if err != nil {
				rec.Error = err.Error()
			}
		}
	}

	// 2. 次选阿里云百炼 VoiceCloning（基于源音频 URL 复刻）
	if engine == "auto" || engine == "dashscope" {
		if ds := h.getDashScopeProvider(); ds != nil && ds.Stats().Active > 0 && publicAudioURL != "" {
			voiceID, err := ds.VoiceCloning(ctx, publicAudioURL, firstNonEmptyString(prefix, rec.ID))
			if err == nil && voiceID != "" {
				rec.CloneEngine = "dashscope"
				rec.ClonedVoiceID = voiceID
				rec.CloneStatus = "cloned"
				rec.Error = ""
				return rec
			}
			if err != nil {
				rec.CloneStatus = "clone_failed"
				rec.Error = err.Error()
				return rec
			}
		}
	}

	rec.CloneStatus = "cached_pending_key"
	if rec.Error == "" {
		rec.Error = "本地参考样音已缓存入库；当前尚未检测到有效的云端克隆 Key（需要 SILICONFLOW_API_KEY 或 DASHSCOPE_API_KEY），配置后点击「云端复刻」即可直接使用"
	}
	return rec
}

// CrawlVoices POST /v1/audio/voices/crawl
func (h *AudioHandler) CrawlVoices(c *gin.Context) {
	var req VoiceCrawlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid crawl request: " + err.Error()}})
		return
	}
	if strings.TrimSpace(req.Query) == "" && strings.TrimSpace(req.URL) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "请至少提供检索关键词 query 或目标网页/音频 url"}})
		return
	}

	existing, report := h.ExecuteVoiceCrawl(c.Request.Context(), req.Query, req.URL, req.Limit)
	c.JSON(http.StatusOK, gin.H{
		"query":            report.Query,
		"target_url":       report.TargetURL,
		"existing_matches": existing,
		"candidates":       report.Candidates,
		"warnings":         report.Warnings,
	})
}

// ImportCrawledVoice POST /v1/audio/voices/crawl-import
func (h *AudioHandler) ImportCrawledVoice(c *gin.Context) {
	var req VoiceCrawlImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid crawl import request: " + err.Error()}})
		return
	}
	rec, desc, err := h.ExecuteVoiceImportAndClone(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"custom_voice": rec,
		"voice":        desc,
		"preview_url":  provider.SamplePreviewURL(rec.ID),
		"status":       rec.CloneStatus,
	})
}

// CloneCustomVoice POST /v1/audio/voices/custom/:id/clone
func (h *AudioHandler) CloneCustomVoice(c *gin.Context) {
	voiceID := strings.TrimSpace(c.Param("id"))
	if voiceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "missing custom voice id"}})
		return
	}
	var body struct {
		CloneEngine string `json:"clone_engine"`
		Transcript  string `json:"transcript"`
		Prefix      string `json:"prefix"`
	}
	_ = c.ShouldBindJSON(&body)

	ctx := c.Request.Context()
	store := h.getVoiceCatalogStore()
	rec, found := store.FindCustomVoice(ctx, voiceID)
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到该自定义音色记录"}})
		return
	}

	if strings.TrimSpace(body.Transcript) != "" {
		rec.Transcript = strings.TrimSpace(body.Transcript)
	}
	audioBytes, hit, _ := store.ReadSample(ctx, rec.ID)
	if !hit || len(audioBytes) == 0 {
		if rec.SourceURL != "" {
			downloaded, _, dlErr := provider.DownloadMediaBytes(ctx, rec.SourceURL, "voice", h.resolveVoiceCrawlOpts(1))
			if dlErr == nil && len(downloaded) > 0 {
				audioBytes = downloaded
				_, _ = store.SaveSample(ctx, rec.ID, downloaded)
			}
		}
	}
	if len(audioBytes) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "本地参考样音字节不存在且源地址无法下载"}})
		return
	}

	mimeType := http.DetectContentType(audioBytes)
	updated := h.attemptCloudVoiceClone(ctx, rec, audioBytes, mimeType, body.CloneEngine, body.Prefix, rec.SourceURL)
	saved, err := store.UpsertCustomVoice(ctx, updated)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}

	sfActive := 0
	if sf := h.getSiliconFlowProvider(); sf != nil {
		sfActive = sf.Stats().Active
	}
	dsActive := 0
	if ds := h.getDashScopeProvider(); ds != nil {
		dsActive = ds.Stats().Active
	}
	c.JSON(http.StatusOK, gin.H{
		"custom_voice": saved,
		"voice":        provider.CustomVoiceToDescriptor(saved, sfActive > 0, dsActive > 0),
		"status":       saved.CloneStatus,
	})
}

// DeleteCustomVoice DELETE /v1/audio/voices/custom/:id
func (h *AudioHandler) DeleteCustomVoice(c *gin.Context) {
	voiceID := strings.TrimSpace(c.Param("id"))
	if voiceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "missing voice id"}})
		return
	}
	removed, err := h.getVoiceCatalogStore().DeleteCustomVoice(c.Request.Context(), voiceID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	if !removed {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "custom voice not found"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": voiceID})
}

func firstNonEmptyString(vals ...string) string {
	for _, v := range vals {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
