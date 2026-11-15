package handler

import (
	"net/http"
	"strings"

	"ai-web-free/internal/provider"
	"ai-web-free/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// MediaLibraryHandler 为 Chat Copilot 及四大创作工坊（语音 / 生图 / 视频 / 短剧）提供统一的网页爬虫检索、
// 本地缓存入库与跨工坊资产复用接口。
type MediaLibraryHandler struct {
	audioHandler   *AudioHandler
	galleryHandler *GalleryHandler
	videoHandler   *VideoHandler
	storageEngine  storage.StorageEngine
	crawlOpts      *provider.MediaCrawlOptions
}

// NewMediaLibraryHandler 构建统一媒体资源库与网页爬虫控制器。
func NewMediaLibraryHandler(
	audioH *AudioHandler,
	galleryH *GalleryHandler,
	videoH *VideoHandler,
	storageEng storage.StorageEngine,
) *MediaLibraryHandler {
	return &MediaLibraryHandler{
		audioHandler:   audioH,
		galleryHandler: galleryH,
		videoHandler:   videoH,
		storageEngine:  storageEng,
	}
}

// SetCrawlerOptions 覆盖媒体爬虫选项（供测试注入离线 mock 搜索端与允许私有地址）。
func (h *MediaLibraryHandler) SetCrawlerOptions(opts provider.MediaCrawlOptions) {
	h.crawlOpts = &opts
	if h.audioHandler != nil {
		h.audioHandler.SetCrawlerOptions(opts)
	}
}

func (h *MediaLibraryHandler) resolveCrawlOpts(kind string, limit int) provider.MediaCrawlOptions {
	opts := provider.MediaCrawlOptions{
		Kind:  kind,
		Limit: limit,
	}
	if h.crawlOpts != nil {
		opts = *h.crawlOpts
		opts.Kind = provider.NormalizeCrawlerKind(kind)
		if limit > 0 {
			opts.Limit = limit
		}
	}
	return opts
}

// UnifiedMediaCrawlRequest 统一全工坊媒体检索/网页爬虫请求。
type UnifiedMediaCrawlRequest struct {
	Query     string `json:"query"`
	URL       string `json:"url"`
	MediaKind string `json:"media_kind"` // "auto" | "voice" | "image" | "video"
	Limit     int    `json:"limit"`
}

// UnifiedMediaImportRequest 统一全工坊媒体缓存入库请求。
type UnifiedMediaImportRequest struct {
	MediaKind   string   `json:"media_kind"` // "voice" | "image" | "video"
	URL         string   `json:"url" binding:"required"`
	SourcePage  string   `json:"source_page"`
	Title       string   `json:"title"`
	Role        string   `json:"role"`
	Transcript  string   `json:"transcript"`
	Gender      string   `json:"gender"`
	Locale      string   `json:"locale"`
	Tags        []string `json:"tags"`
	CloneEngine string   `json:"clone_engine"` // 仅 voice 时生效: "auto" | "siliconflow" | "dashscope" | "cache_only"
	Prefix      string   `json:"prefix"`
}

// CrawlMedia POST /v1/media/crawl — 先查当前系统本地缓存库，再执行网页爬虫提取音频/参考图/视频。
func (h *MediaLibraryHandler) CrawlMedia(c *gin.Context) {
	var req UnifiedMediaCrawlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid crawl request: " + err.Error()}})
		return
	}
	if strings.TrimSpace(req.Query) == "" && strings.TrimSpace(req.URL) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "请提供搜索关键词 query 或目标网页 url"}})
		return
	}

	kind := provider.NormalizeCrawlerKind(req.MediaKind)
	limit := req.Limit
	if limit <= 0 {
		limit = 12
	}
	ctx := c.Request.Context()

	var existingVoices []provider.VoiceDescriptor
	if (kind == "auto" || kind == "voice") && h.audioHandler != nil {
		existingVoices = h.audioHandler.searchExistingSystemVoices(ctx, req.Query, 8)
	}
	var existingImages interface{}
	if (kind == "auto" || kind == "image") && h.galleryHandler != nil {
		existingImages = h.galleryHandler.SearchWorks(req.Query, 8)
	}
	var existingVideos interface{}
	if (kind == "auto" || kind == "video") && h.videoHandler != nil {
		existingVideos = h.videoHandler.ListTasksSnapshot(req.Query, 8)
	}

	report := provider.SearchAndCrawlMedia(ctx, req.Query, req.URL, h.resolveCrawlOpts(kind, limit))
	c.JSON(http.StatusOK, gin.H{
		"query":           report.Query,
		"target_url":      report.TargetURL,
		"media_kind":      kind,
		"existing_voices": existingVoices,
		"existing_images": existingImages,
		"existing_videos": existingVideos,
		"candidates":      report.Candidates,
		"warnings":        report.Warnings,
	})
}

// ImportCrawledMedia POST /v1/media/crawl-import — 按媒体类型下载缓存并注册到对应系统库复用。
func (h *MediaLibraryHandler) ImportCrawledMedia(c *gin.Context) {
	var req UnifiedMediaImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "invalid import request: " + err.Error()}})
		return
	}
	kind := provider.NormalizeCrawlerKind(req.MediaKind)
	if kind == "auto" {
		if detected, _ := detectMediaKindByURLHint(req.URL); detected != "" {
			kind = detected
		} else {
			kind = "voice"
		}
	}

	ctx := c.Request.Context()
	switch kind {
	case "voice":
		if h.audioHandler == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "audio handler unavailable"}})
			return
		}
		rec, desc, err := h.audioHandler.ExecuteVoiceImportAndClone(ctx, VoiceCrawlImportRequest{
			URL:         req.URL,
			SourcePage:  req.SourcePage,
			Name:        req.Title,
			Role:        req.Role,
			Transcript:  req.Transcript,
			Gender:      req.Gender,
			Locale:      req.Locale,
			Tags:        req.Tags,
			CloneEngine: req.CloneEngine,
			Prefix:      req.Prefix,
		})
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"media_kind":   "voice",
			"id":           rec.ID,
			"local_url":    provider.SamplePreviewURL(rec.ID),
			"custom_voice": rec,
			"voice":        desc,
			"status":       rec.CloneStatus,
			"target_view":  "#/tts",
		})

	case "image":
		if h.galleryHandler == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "gallery handler unavailable"}})
			return
		}
		rawBytes, mimeType, err := provider.DownloadMediaBytes(ctx, req.URL, "image", h.resolveCrawlOpts("image", 1))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
			return
		}
		title := firstNonEmptyString(req.Title, "网页参考图")
		mode := firstNonEmptyString(req.Role, "crawled")
		summary := firstNonEmptyString(strings.Join(req.Tags, ","), req.SourcePage, req.URL)
		workID := "crawl_img_" + uuid.NewString()[:8]
		work, err := h.galleryHandler.ImportCrawledImage(ctx, workID, title, summary, mode, req.URL, mimeType, rawBytes)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error()}})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"media_kind":  "image",
			"id":          work.ID,
			"local_url":   work.URL,
			"work":        work,
			"status":      "cached",
			"target_view": "#/images",
		})

	case "video":
		if h.videoHandler == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "video handler unavailable"}})
			return
		}
		rawBytes, mimeType, err := provider.DownloadMediaBytes(ctx, req.URL, "video", h.resolveCrawlOpts("video", 1))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
			return
		}
		taskID := "crawl-vid-" + uuid.NewString()[:8]
		if mse, ok := h.storageEngine.(storage.MediaStorageEngine); ok && mse != nil {
			if _, _, saveErr := mse.SaveInCategory(ctx, "videos", taskID, rawBytes, mimeType); saveErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": saveErr.Error()}})
				return
			}
		}
		task := h.videoHandler.RegisterImportedVideo(taskID, firstNonEmptyString(req.Title, "网页视频参考素材"), req.URL)
		c.JSON(http.StatusOK, gin.H{
			"media_kind":  "video",
			"id":          task.TaskID,
			"task_id":     task.TaskID,
			"local_url":   task.LocalURL,
			"task":        task,
			"status":      "cached",
			"target_view": "#/video",
		})

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "unsupported media_kind: " + req.MediaKind}})
	}
}

// ListSystemAssets GET /v1/media/assets — 汇总全平台已缓存/克隆的系统音频、图像、视频资产。
func (h *MediaLibraryHandler) ListSystemAssets(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	kind := provider.NormalizeCrawlerKind(c.Query("kind"))
	ctx := c.Request.Context()

	resp := gin.H{
		"query": q,
		"kind":  kind,
	}
	if (kind == "auto" || kind == "voice") && h.audioHandler != nil {
		allVoices, _, _ := h.audioHandler.collectVoiceCatalog(ctx, false)
		customVoices := make([]provider.VoiceDescriptor, 0)
		dramaVoices := make([]provider.VoiceDescriptor, 0)
		for _, v := range allVoices {
			if q != "" && !strings.Contains(strings.ToLower(v.ID+v.Name+strings.Join(v.Tags, " ")), strings.ToLower(q)) {
				continue
			}
			if v.Provider == "custom" {
				customVoices = append(customVoices, v)
			}
			for _, tag := range v.Tags {
				if tag == "短剧常用" {
					dramaVoices = append(dramaVoices, v)
					break
				}
			}
		}
		resp["custom_voices"] = customVoices
		resp["short_drama_voices"] = dramaVoices
	}
	if (kind == "auto" || kind == "image") && h.galleryHandler != nil {
		resp["images"] = h.galleryHandler.SearchWorks(q, 30)
	}
	if (kind == "auto" || kind == "video") && h.videoHandler != nil {
		resp["videos"] = h.videoHandler.ListTasksSnapshot(q, 30)
	}
	c.JSON(http.StatusOK, resp)
}

func detectMediaKindByURLHint(rawURL string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(rawURL))
	for _, ext := range []string{".mp3", ".wav", ".ogg", ".m4a", ".aac", ".flac"} {
		if strings.Contains(lower, ext) {
			return "voice", true
		}
	}
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp", ".gif"} {
		if strings.Contains(lower, ext) {
			return "image", true
		}
	}
	for _, ext := range []string{".mp4", ".webm", ".mov", ".m4v"} {
		if strings.Contains(lower, ext) {
			return "video", true
		}
	}
	return "", false
}
