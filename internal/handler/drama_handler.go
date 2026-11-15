package handler

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/db"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// DramaHandler 为短剧工坊无限画布提供全平台能力探测、多工程图状态持久化及同源媒体缓存接口
type DramaHandler struct {
	db            *gorm.DB
	pool          *accounts.Pool
	registry      *provider.Registry
	storageEngine storage.StorageEngine
}

func NewDramaHandler(database *gorm.DB, pool *accounts.Pool, reg *provider.Registry, engine storage.StorageEngine) *DramaHandler {
	return &DramaHandler{
		db:            database,
		pool:          pool,
		registry:      reg,
		storageEngine: engine,
	}
}

func (h *DramaHandler) providerActive(id string) (bool, int) {
	if h.registry == nil {
		return false, 0
	}
	p, ok := h.registry.Get(id)
	if !ok || p == nil {
		return false, 0
	}
	st := p.Stats()
	return st.Active > 0, st.Active
}

func (h *DramaHandler) chatGPTStats() (active int, paid int) {
	if h.pool == nil {
		return 0, 0
	}
	st := h.pool.Stats()
	paidCount := st.Active - st.FreeCount - st.NoAuthCount
	if paidCount < 0 {
		paidCount = 0
	}
	return st.Active, paidCount
}

// Capabilities GET /admin/api/v1/drama/capabilities
// 实时探测脚本分析、角色/分镜生图、图生图改图、TTS配音、Whisper转写与视频/口型合成可用性
func (h *DramaHandler) Capabilities(c *gin.Context) {
	cgptActive, cgptPaid := h.chatGPTStats()
	_, dsActive := h.providerActive("dashscope")
	_, sfActive := h.providerActive("siliconflow")
	_, jmActive := h.providerActive("jimeng")
	_, museActive := h.providerActive("meta_muse")

	imageProviders := map[string]gin.H{
		"chatgpt": {
			"available": cgptActive > 0,
			"active":    cgptActive,
			"models":    []string{"gpt-image-1", "dall-e-3"},
		},
		"siliconflow": {
			"available": sfActive > 0,
			"active":    sfActive,
			"models":    []string{"flux-schnell", "black-forest-labs/FLUX.1-schnell"},
		},
		"dashscope": {
			"available": dsActive > 0,
			"active":    dsActive,
			"models":    []string{"wanx-v1"},
		},
		"jimeng": {
			"available": jmActive > 0,
			"active":    jmActive,
			"models":    []string{"jimeng-2.1"},
		},
	}

	defaultImageModel := "flux-schnell"
	if sfActive > 0 {
		defaultImageModel = "flux-schnell"
	} else if dsActive > 0 {
		defaultImageModel = "wanx-v1"
	} else if jmActive > 0 {
		defaultImageModel = "jimeng-2.1"
	} else if cgptActive > 0 {
		defaultImageModel = "gpt-image-1"
	}

	videoProviders := map[string]gin.H{
		"siliconflow": {
			"available":    sfActive > 0,
			"active":       sfActive,
			"models":       []string{"THUDM/CogVideoX-5b", "THUDM/CogVideoX-2b"},
			"supports_i2v": true,
			"supports_a2v": false,
		},
		"dashscope": {
			"available":    dsActive > 0,
			"active":       dsActive,
			"models":       []string{"wan-video-v1"},
			"supports_i2v": true,
			"supports_a2v": false,
		},
		"meta_muse": {
			"available":    museActive > 0,
			"active":       museActive,
			"models":       []string{"muse-video", "muse-talk"},
			"supports_i2v": true,
			"supports_a2v": true,
		},
		"jimeng": {
			"available":    jmActive > 0,
			"active":       jmActive,
			"models":       []string{"jimeng-video-2.0"},
			"supports_i2v": true,
			"supports_a2v": false,
		},
	}

	imageAvailable := cgptActive > 0 || sfActive > 0 || dsActive > 0 || jmActive > 0
	videoAvailable := sfActive > 0 || dsActive > 0 || museActive > 0 || jmActive > 0

	pickVideo := func(order ...string) string {
		for _, candidate := range order {
			switch candidate {
			case "THUDM/CogVideoX-5b", "THUDM/CogVideoX-2b":
				if sfActive > 0 {
					return candidate
				}
			case "wan-video-v1":
				if dsActive > 0 {
					return candidate
				}
			case "muse-video", "muse-talk":
				if museActive > 0 {
					return candidate
				}
			case "jimeng-video-2.0":
				if jmActive > 0 {
					return candidate
				}
			}
		}
		return ""
	}

	pickImage := func(order ...string) string {
		for _, candidate := range order {
			switch candidate {
			case "gpt-image-1":
				if cgptActive > 0 {
					return candidate
				}
			case "flux-schnell":
				if sfActive > 0 {
					return candidate
				}
			case "wanx-v1":
				if dsActive > 0 {
					return candidate
				}
			case "jimeng-2.1":
				if jmActive > 0 {
					return candidate
				}
			}
		}
		return defaultImageModel
	}

	c.JSON(http.StatusOK, gin.H{
		"llm": gin.H{
			"available": cgptActive > 0,
			"active":    cgptActive,
			"models":    []string{"gpt-4o", "gpt-5"},
			"reason": func() string {
				if cgptActive > 0 {
					return ""
				}
				return "暂无可用 ChatGPT 号池账号，可使用内置模板/手工编排或先导入账号"
			}(),
		},
		"image": gin.H{
			"available":      imageAvailable,
			"edit_available": cgptActive > 0,
			"default_model":  defaultImageModel,
			"providers":      imageProviders,
			"reason": func() string {
				if imageAvailable {
					return ""
				}
				return "暂无已激活生图供应商，请在账号/密钥池配置 SiliconFlow、百炼、即梦或 ChatGPT 账号"
			}(),
		},
		"tts": gin.H{
			"available":              true,
			"voice_design_available": dsActive > 0,
			"voice_clone_available":  dsActive > 0,
			"providers": gin.H{
				"edge_tts":    gin.H{"available": true, "zero_config": true},
				"jianying":    gin.H{"available": true, "zero_config": true},
				"dashscope":   gin.H{"available": dsActive > 0, "active": dsActive},
				"siliconflow": gin.H{"available": sfActive > 0, "active": sfActive},
				"chatgpt":     gin.H{"available": cgptActive > 0, "active": cgptActive},
			},
		},
		"stt": gin.H{
			"available": cgptPaid > 0,
			"active":    cgptPaid,
			"models":    []string{"whisper-1"},
			"reason": func() string {
				if cgptPaid > 0 {
					return ""
				}
				return "Whisper 语音转写需要至少 1 个 Plus/付费 ChatGPT 会话账号；也可直接手动输入剧本"
			}(),
		},
		"video": gin.H{
			"available":         videoAvailable,
			"lipsync_available": museActive > 0,
			"providers":         videoProviders,
			"reason": func() string {
				if videoAvailable {
					return ""
				}
				return "暂无可用视频大模型凭据（CogVideoX / 通义万相 / Meta Muse / 即梦），将自动切换高清关键帧 Ken Burns 运镜合成成片"
			}(),
		},
		"recommended": gin.H{
			"free_first": gin.H{
				"llm_model":     "gpt-4o",
				"image_model":   pickImage("flux-schnell", "jimeng-2.1", "wanx-v1", "gpt-image-1"),
				"tts_voice":     "zh-CN-YunxiNeural",
				"video_model":   pickVideo("THUDM/CogVideoX-5b", "wan-video-v1", "jimeng-video-2.0", "muse-video"),
				"fallback_mode": "ken_burns",
			},
			"quality_first": gin.H{
				"llm_model":     "gpt-4o",
				"image_model":   pickImage("gpt-image-1", "jimeng-2.1", "flux-schnell", "wanx-v1"),
				"tts_voice":     "zh-CN-XiaoxiaoNeural",
				"video_model":   pickVideo("muse-talk", "jimeng-video-2.0", "THUDM/CogVideoX-5b", "wan-video-v1"),
				"fallback_mode": "ken_burns",
			},
			"fast": gin.H{
				"llm_model":     "gpt-4o",
				"image_model":   pickImage("flux-schnell", "wanx-v1", "jimeng-2.1", "gpt-image-1"),
				"tts_voice":     "zh-CN-YunxiNeural",
				"video_model":   pickVideo("THUDM/CogVideoX-2b", "wan-video-v1", "muse-video", "jimeng-video-2.0"),
				"fallback_mode": "ken_burns",
			},
		},
	})
}

// ListProjects GET /admin/api/v1/drama/projects
func (h *DramaHandler) ListProjects(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusOK, gin.H{"data": []db.DramaProjectRecord{}})
		return
	}
	var records []db.DramaProjectRecord
	if err := h.db.Order("updated_at desc").Limit(100).Find(&records).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": records})
}

// GetProject GET /admin/api/v1/drama/projects/:id
func (h *DramaHandler) GetProject(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	var rec db.DramaProjectRecord
	if err := h.db.First(&rec, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "drama project not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rec)
}

type saveDramaProjectRequest struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Genre         string `json:"genre"`
	AspectRatio   string `json:"aspect_ratio"`
	StylePreset   string `json:"style_preset"`
	RoutingPolicy string `json:"routing_policy"`
	Outline       string `json:"outline"`
	GraphJSON     string `json:"graph_json"`
	Status        string `json:"status"`
	ShotCount     int    `json:"shot_count"`
}

// SaveProject POST /admin/api/v1/drama/projects 与 PUT /admin/api/v1/drama/projects/:id
func (h *DramaHandler) SaveProject(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}
	var req saveDramaProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	paramID := strings.TrimSpace(c.Param("id"))
	if paramID != "" {
		req.ID = paramID
	}
	if strings.TrimSpace(req.ID) == "" {
		req.ID = "drama_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	}
	if strings.TrimSpace(req.Title) == "" {
		req.Title = "未命名短剧工程"
	}
	if strings.TrimSpace(req.AspectRatio) == "" {
		req.AspectRatio = "9:16"
	}
	if strings.TrimSpace(req.RoutingPolicy) == "" {
		req.RoutingPolicy = "free_first"
	}
	if strings.TrimSpace(req.Status) == "" {
		req.Status = "draft"
	}

	var existing db.DramaProjectRecord
	err := h.db.First(&existing, "id = ?", req.ID).Error
	if err == nil {
		existing.Title = req.Title
		existing.Genre = req.Genre
		existing.AspectRatio = req.AspectRatio
		existing.StylePreset = req.StylePreset
		existing.RoutingPolicy = req.RoutingPolicy
		existing.Outline = req.Outline
		existing.GraphJSON = req.GraphJSON
		existing.Status = req.Status
		existing.ShotCount = req.ShotCount
		if saveErr := h.db.Save(&existing).Error; saveErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": saveErr.Error()})
			return
		}
		c.JSON(http.StatusOK, existing)
		return
	}

	rec := db.DramaProjectRecord{
		ID:            req.ID,
		Title:         req.Title,
		Genre:         req.Genre,
		AspectRatio:   req.AspectRatio,
		StylePreset:   req.StylePreset,
		RoutingPolicy: req.RoutingPolicy,
		Outline:       req.Outline,
		GraphJSON:     req.GraphJSON,
		Status:        req.Status,
		ShotCount:     req.ShotCount,
	}
	if createErr := h.db.Create(&rec).Error; createErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": createErr.Error()})
		return
	}
	c.JSON(http.StatusOK, rec)
}

// DeleteProject DELETE /admin/api/v1/drama/projects/:id
func (h *DramaHandler) DeleteProject(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing project id"})
		return
	}
	if err := h.db.Delete(&db.DramaProjectRecord{}, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted", "id": id})
}

type cacheMediaRequest struct {
	URL      string `json:"url"`
	DataURL  string `json:"data_url"`
	Category string `json:"category"` // images | videos | audio
	ID       string `json:"id"`
	MimeType string `json:"mime_type"`
}

func inferCategoryFromMime(mimeType string) string {
	lower := strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.HasPrefix(lower, "video/"):
		return "videos"
	case strings.HasPrefix(lower, "audio/"):
		return "audio"
	default:
		return "images"
	}
}

func decodeDataURLPayload(raw string) (mimeType string, data []byte, err error) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "data:") {
		return "", nil, fmt.Errorf("not a data URL")
	}
	rest := strings.TrimPrefix(trimmed, "data:")
	commaIdx := strings.Index(rest, ",")
	if commaIdx <= 0 {
		return "", nil, fmt.Errorf("malformed data URL")
	}
	meta := rest[:commaIdx]
	payload := rest[commaIdx+1:]
	if !strings.Contains(meta, ";base64") {
		return "", nil, fmt.Errorf("only base64 data URLs are supported")
	}
	mimeType = strings.TrimSpace(strings.SplitN(meta, ";", 2)[0])
	decoded, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
	if decErr != nil {
		decoded, decErr = base64.RawStdEncoding.DecodeString(strings.TrimSpace(payload))
		if decErr != nil {
			return "", nil, decErr
		}
	}
	return mimeType, decoded, nil
}

// CacheMedia POST /admin/api/v1/drama/media/cache
// 将远程媒体 URL 或前端本地上传 Data URL 缓存为站内同源 `/storage/...` 静态资源，避免 Canvas 跨域污染与 URL 过期失效
func (h *DramaHandler) CacheMedia(c *gin.Context) {
	if h.storageEngine == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage engine unavailable"})
		return
	}
	mse, ok := h.storageEngine.(storage.MediaStorageEngine)
	if !ok || mse == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage engine does not support categorized media"})
		return
	}

	var req cacheMediaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rawURL := strings.TrimSpace(req.URL)
	rawDataURL := strings.TrimSpace(req.DataURL)
	if rawDataURL == "" && strings.HasPrefix(rawURL, "data:") {
		rawDataURL = rawURL
		rawURL = ""
	}

	if rawDataURL == "" && strings.HasPrefix(rawURL, "/storage/") {
		c.JSON(http.StatusOK, gin.H{
			"local_url": rawURL,
			"cached":    true,
		})
		return
	}

	var data []byte
	mimeType := strings.TrimSpace(req.MimeType)
	if rawDataURL != "" {
		decMime, decBytes, err := decodeDataURLPayload(rawDataURL)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		data = decBytes
		if mimeType == "" {
			mimeType = decMime
		}
	} else if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		fetchCtx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
		defer cancel()
		httpReq, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, rawURL, nil)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		client := &http.Client{Timeout: 45 * time.Second}
		resp, err := client.Do(httpReq)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("upstream returned status %d", resp.StatusCode)})
			return
		}
		readBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 128<<20))
		if readErr != nil || len(readBytes) == 0 {
			c.JSON(http.StatusBadGateway, gin.H{"error": "empty or unreadable upstream media"})
			return
		}
		data = readBytes
		if mimeType == "" {
			mimeType = strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0])
		}
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "either url or data_url must be provided"})
		return
	}

	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	category := strings.TrimSpace(req.Category)
	if category == "" {
		category = inferCategoryFromMime(mimeType)
	}

	fileID := strings.TrimSpace(req.ID)
	if fileID == "" {
		fileID = "drama_media_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	}

	localURL, relPath, err := mse.SaveInCategory(context.Background(), category, fileID, data, mimeType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"local_url":  localURL,
		"rel_path":   relPath,
		"category":   category,
		"mime_type":  mimeType,
		"size_bytes": len(data),
		"cached":     true,
	})
}

// ServeCachedAudio GET /storage/audio/:filename
func (h *DramaHandler) ServeCachedAudio(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	filename := filepath.Base(strings.TrimSpace(c.Param("filename")))
	if filename == "" || filename == "." || strings.Contains(filename, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid audio filename"})
		return
	}
	if mse, ok := h.storageEngine.(storage.MediaStorageEngine); ok && mse != nil {
		if data, err := mse.Read(c.Request.Context(), "audio/"+filename); err == nil && len(data) > 0 {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
			c.Data(http.StatusOK, detectMediaMimeFromExt(filepath.Ext(filename), data), data)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "audio not found"})
}
