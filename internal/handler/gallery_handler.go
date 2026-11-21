package handler

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/db"
	"ai-web-free/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GalleryHandler 提供图像工坊持久化画廊、会话历史与收藏 API
type GalleryHandler struct {
	db            *gorm.DB
	storageEngine storage.StorageEngine
	mu            sync.Mutex
	memWorks      []db.GalleryWork
}

func NewGalleryHandler(database *gorm.DB, engine storage.StorageEngine) *GalleryHandler {
	return &GalleryHandler{
		db:            database,
		storageEngine: engine,
	}
}

// ─── 静态资源托管 ──────────────────────────────────────────────────

// ServeStaticImage 托管本地磁盘或数据库 Blob 中的生图文件，输出强缓存头并支持 Range/HEAD
func (h *GalleryHandler) ServeStaticImage(c *gin.Context) {
	filename := filepath.Base(strings.TrimSpace(c.Param("filename")))
	if filename == "" || filename == "." || strings.Contains(filename, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid image filename"})
		return
	}

	const cacheCtl = "public, max-age=31536000, immutable"
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Cross-Origin-Resource-Policy", "cross-origin")

	// 0. 按需返回并磁盘缓存轻量缩略图（?w=320|480|720|960），将 2MB+ 原图压缩为 ~30-60KB JPEG
	if thumbWidth := parseRequestedThumbWidth(c.Query("w")); thumbWidth > 0 {
		if h.serveStaticImageThumb(c, filename, thumbWidth, cacheCtl) {
			return
		}
	}

	// 1. 优先命中本地磁盘 L1 缓存（兼容 LocalStorageEngine 与 TieredStorageEngine）
	if serveLocalMediaFile(c, h.storageEngine, "images/"+filename, cacheCtl) {
		return
	}

	// 1.5 统一媒体存储引擎兜底（支持纯内存存储或未命中 L1 时从 L2 按需回填）
	if mse, ok := h.storageEngine.(storage.MediaStorageEngine); ok && mse != nil {
		if data, err := mse.Read(c.Request.Context(), "images/"+filename); err == nil && len(data) > 0 {
			serveRangeMediaBytes(c, filename, detectMediaMimeFromExt(filepath.Ext(filename), data), cacheCtl, time.Time{}, data)
			return
		}
	}

	// 2. 检查数据库中是否存在 BlobData 兜底（纯内存或文件未落盘场景）
	if h.db != nil {
		var work db.GalleryWork
		idWithoutExt := strings.TrimSuffix(filename, filepath.Ext(filename))
		if err := h.db.Where("id = ? OR file_path LIKE ? OR url LIKE ?", idWithoutExt, "%"+filename, "%"+filename).First(&work).Error; err == nil {
			if len(work.BlobData) > 0 {
				mime := work.MimeType
				if mime == "" {
					mime = detectMediaMimeFromExt(filepath.Ext(filename), work.BlobData)
				}
				serveRangeMediaBytes(c, filename, mime, cacheCtl, work.CreatedAt, work.BlobData)
				return
			}
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "image not found"})
}

// ─── 图像工坊画廊 API ────────────────────────────────────────────────

type SaveWorkRequest struct {
	ID            string   `json:"id"`
	Prompt        string   `json:"prompt" binding:"required"`
	RevisedPrompt string   `json:"revised_prompt"`
	Model         string   `json:"model"`
	Size          string   `json:"size"`
	Quality       string   `json:"quality"`
	Mode          string   `json:"mode"`
	Category      string   `json:"category"`
	Source        string   `json:"source"`
	ProjectID     string   `json:"project_id"`
	ProjectTitle  string   `json:"project_title"`
	RoleName      string   `json:"role_name"`
	ShotIndex     int      `json:"shot_index"`
	Tags          []string `json:"tags"`
	B64JSON       string   `json:"b64_json"`
	URL           string   `json:"url"`
	LatencyMs     int      `json:"latency_ms"`
	IsFavorite    bool     `json:"is_favorite"`
	Timestamp     int64    `json:"timestamp"`
}

// ListWorks 分页查询持久化画廊列表
func (h *GalleryHandler) ListWorks(c *gin.Context) {
	if h.db == nil {
		q := strings.ToLower(strings.TrimSpace(c.Query("q")))
		model := strings.TrimSpace(c.Query("model"))
		category := strings.TrimSpace(c.Query("category"))
		source := strings.TrimSpace(c.Query("source"))
		projectID := strings.TrimSpace(c.Query("project_id"))
		h.mu.Lock()
		items := make([]db.GalleryWork, 0, len(h.memWorks))
		for _, w := range h.memWorks {
			if category != "" && category != "all" && w.Category != category {
				continue
			}
			if source != "" && source != "all" && w.Source != source {
				continue
			}
			if projectID != "" && w.ProjectID != projectID {
				continue
			}
			if model != "" && model != "all" && w.Model != model {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(w.Prompt+" "+w.RevisedPrompt+" "+w.Category+" "+w.Source+" "+w.ProjectTitle+" "+w.RoleName+" "+strings.Join(w.Tags, " ")), q) {
				continue
			}
			items = append(items, w)
		}
		h.mu.Unlock()
		c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items), "page": 1, "page_size": 50})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}

	model := strings.TrimSpace(c.Query("model"))
	category := strings.TrimSpace(c.Query("category"))
	source := strings.TrimSpace(c.Query("source"))
	projectID := strings.TrimSpace(c.Query("project_id"))
	favoriteOnly := strings.EqualFold(strings.TrimSpace(c.Query("favorite")), "true")
	q := strings.TrimSpace(c.Query("q"))

	query := h.db.Model(&db.GalleryWork{})
	if model != "" && model != "all" {
		query = query.Where("model = ?", model)
	}
	if category != "" && category != "all" {
		query = query.Where("category = ?", category)
	}
	if source != "" && source != "all" {
		query = query.Where("source = ?", source)
	}
	if projectID != "" {
		query = query.Where("project_id = ?", projectID)
	}
	if favoriteOnly {
		query = query.Where("is_favorite = ?", true)
	}
	if q != "" {
		pattern := "%" + q + "%"
		query = query.Where("(prompt LIKE ? OR revised_prompt LIKE ? OR project_title LIKE ? OR role_name LIKE ? OR tags LIKE ? OR model LIKE ? OR category LIKE ? OR source LIKE ?)", pattern, pattern, pattern, pattern, pattern, pattern, pattern, pattern)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var items []db.GalleryWork
	offset := (page - 1) * pageSize
	if err := query.Omit("blob_data").Order("created_at desc").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

type UpdateGalleryMetadataRequest struct {
	Category *string   `json:"category"`
	Tags     *[]string `json:"tags"`
}

func (h *GalleryHandler) UpdateMetadata(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}
	var req UpdateGalleryMetadataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	var work db.GalleryWork
	if err := h.db.First(&work, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "work not found"})
		return
	}
	if req.Category != nil {
		work.Category = normalizeGalleryCategory(*req.Category)
	}
	if req.Tags != nil {
		work.Tags = normalizeGalleryTags(*req.Tags)
	}
	if err := h.db.Save(&work).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, work)
}

func normalizeGalleryCategory(category string) string {
	switch strings.TrimSpace(category) {
	case "character_portrait", "scene_anchor", "storyboard_frame", "reference", "general":
		return strings.TrimSpace(category)
	default:
		return "general"
	}
}

func normalizeGalleryTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || len(tag) > 48 {
			continue
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
		if len(out) == 16 {
			break
		}
	}
	return out
}

// SaveWork 保存或上传单张画廊作品
func (h *GalleryHandler) SaveWork(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	var req SaveWorkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	work, err := h.processAndSaveSingleWork(c.Request.Context(), req, c.ClientIP())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, work)
}

type BatchMigrateRequest struct {
	Items []SaveWorkRequest `json:"items" binding:"required"`
}

// BatchMigrate 接收前端 IndexedDB 批量上报的作品数据，幂等迁移保存
func (h *GalleryHandler) BatchMigrate(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	var req BatchMigrateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid batch request: " + err.Error()})
		return
	}

	migrated := 0
	skipped := 0
	ctx := c.Request.Context()
	clientIP := c.ClientIP()

	for _, item := range req.Items {
		cleanID := strings.TrimSpace(item.ID)
		if cleanID != "" {
			var count int64
			h.db.Model(&db.GalleryWork{}).Where("id = ?", cleanID).Count(&count)
			if count > 0 {
				skipped++
				continue
			}
		}

		_, err := h.processAndSaveSingleWork(ctx, item, clientIP)
		if err != nil {
			skipped++
		} else {
			migrated++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"migrated": migrated,
		"skipped":  skipped,
		"total":    len(req.Items),
	})
}

// ToggleFavorite 切换单张作品的收藏状态
func (h *GalleryHandler) ToggleFavorite(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	var work db.GalleryWork
	if err := h.db.First(&work, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "work not found"})
		return
	}

	// 若 Body 携带了 is_favorite 则按指定值设定，否则取反
	var body struct {
		IsFavorite *bool `json:"is_favorite"`
	}
	if err := c.ShouldBindJSON(&body); err == nil && body.IsFavorite != nil {
		work.IsFavorite = *body.IsFavorite
	} else {
		work.IsFavorite = !work.IsFavorite
	}

	work.UpdatedAt = time.Now().UTC()
	if err := h.db.Save(&work).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, work)
}

// DeleteWork 删除单张作品并清理物理文件
func (h *GalleryHandler) DeleteWork(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	var work db.GalleryWork
	if err := h.db.First(&work, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "work not found"})
		return
	}

	if work.FilePath != "" && h.storageEngine != nil {
		_ = h.storageEngine.Delete(c.Request.Context(), work.FilePath)
	}

	if err := h.db.Delete(&work).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ClearWorks 清空或批量删除作品
func (h *GalleryHandler) ClearWorks(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	var works []db.GalleryWork
	if err := h.db.Find(&works).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	for _, w := range works {
		if w.FilePath != "" && h.storageEngine != nil {
			_ = h.storageEngine.Delete(ctx, w.FilePath)
		}
	}

	result := h.db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&db.GalleryWork{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"deleted_count": result.RowsAffected,
	})
}

// ─── 会话历史 API ────────────────────────────────────────────────────

type ChatMessageItem struct {
	ID              string `json:"id"`
	ParentID        string `json:"parentId,omitempty"`
	Role            string `json:"role"`
	Content         string `json:"content"`
	PartsJSON       string `json:"partsJson,omitempty"`
	AttachmentsJSON string `json:"attachmentsJson,omitempty"`
	MetadataJSON    string `json:"metadataJson,omitempty"`
	StatusJSON      string `json:"statusJson,omitempty"`
	Model           string `json:"model,omitempty"`
	OpenUIMode      bool   `json:"openuiMode"`
	Skill           string `json:"skill,omitempty"`
	TokensPrompt    int    `json:"tokensPrompt,omitempty"`
	TokensComp      int    `json:"tokensComp,omitempty"`
	LatencyMs       int64  `json:"latencyMs,omitempty"`
	Citations       string `json:"citations,omitempty"`
	Timestamp       int64  `json:"timestamp,omitempty"`
}

type SaveChatMessagesRequest struct {
	SessionKey string            `json:"session_key"`
	Messages   []ChatMessageItem `json:"messages" binding:"required"`
}

// ListChatMessages 获取 Copilot 历史会话消息
func (h *GalleryHandler) ListChatMessages(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusOK, gin.H{"messages": []interface{}{}})
		return
	}

	sessionKey := c.DefaultQuery("session_key", "default")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if limit < 1 || limit > 500 {
		limit = 100
	}

	var records []db.ChatMessageRecord
	if err := h.db.Where("session_key = ?", sessionKey).Order("created_at asc").Limit(limit).Find(&records).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"messages": records})
}

// SaveChatMessages 覆盖或增量保存会话消息
func (h *GalleryHandler) SaveChatMessages(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	var req SaveChatMessagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	sessionKey := strings.TrimSpace(req.SessionKey)
	if sessionKey == "" {
		sessionKey = "default"
	}

	// 事务内替换/保存
	err := h.db.Transaction(func(tx *gorm.DB) error {
		_ = tx.Where("session_key = ?", sessionKey).Delete(&db.ChatMessageRecord{}).Error

		for _, m := range req.Messages {
			msgID := strings.TrimSpace(m.ID)
			if msgID == "" {
				msgID = "msg-" + uuid.NewString()
			}
			createdAt := time.Now().UTC()
			if m.Timestamp > 0 {
				createdAt = time.UnixMilli(m.Timestamp).UTC()
			}
			rec := db.ChatMessageRecord{
				ID:         msgID,
				SessionKey: sessionKey,
				Role:       m.Role,
				Content:    m.Content,
				OpenUIMode: m.OpenUIMode,
				Skill:      m.Skill,
				CreatedAt:  createdAt,
			}
			if err := tx.Create(&rec).Error; err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "count": len(req.Messages)})
}

// ClearChatMessages 清空指定会话历史
func (h *GalleryHandler) ClearChatMessages(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	sessionKey := c.DefaultQuery("session_key", "default")
	if err := h.db.Where("session_key = ?", sessionKey).Delete(&db.ChatMessageRecord{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ─── 提示词广场收藏 API ──────────────────────────────────────────────

// ListPromptFavorites 获取服务端收藏的 Prompt ID 列表
func (h *GalleryHandler) ListPromptFavorites(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusOK, gin.H{"favorite_ids": []string{}})
		return
	}

	var favorites []db.PromptFavorite
	if err := h.db.Order("created_at desc").Find(&favorites).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ids := make([]string, 0, len(favorites))
	for _, f := range favorites {
		ids = append(ids, f.PromptID)
	}

	c.JSON(http.StatusOK, gin.H{"favorite_ids": ids})
}

// TogglePromptFavorite 切换单个 Prompt 的收藏状态
func (h *GalleryHandler) TogglePromptFavorite(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	var body struct {
		ID string `json:"id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id is required"})
		return
	}

	id := strings.TrimSpace(body.ID)
	var fav db.PromptFavorite
	isFav := false

	if err := h.db.First(&fav, "prompt_id = ?", id).Error; err == nil {
		// 已存在，取消收藏
		_ = h.db.Delete(&fav).Error
		isFav = false
	} else {
		// 不存在，添加收藏
		fav = db.PromptFavorite{
			PromptID:  id,
			CreatedAt: time.Now().UTC(),
		}
		_ = h.db.Create(&fav).Error
		isFav = true
	}

	var allFavs []db.PromptFavorite
	h.db.Order("created_at desc").Find(&allFavs)
	ids := make([]string, 0, len(allFavs))
	for _, f := range allFavs {
		ids = append(ids, f.PromptID)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":           id,
		"is_favorite":  isFav,
		"favorite_ids": ids,
	})
}

// SyncPromptFavorites 批量合并前端 LocalStorage 收藏列表至服务端
func (h *GalleryHandler) SyncPromptFavorites(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	var body struct {
		IDs []string `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	for _, id := range body.IDs {
		cleanID := strings.TrimSpace(id)
		if cleanID == "" {
			continue
		}
		var count int64
		h.db.Model(&db.PromptFavorite{}).Where("prompt_id = ?", cleanID).Count(&count)
		if count == 0 {
			_ = h.db.Create(&db.PromptFavorite{
				PromptID:  cleanID,
				CreatedAt: time.Now().UTC(),
			}).Error
		}
	}

	var allFavs []db.PromptFavorite
	h.db.Order("created_at desc").Find(&allFavs)
	ids := make([]string, 0, len(allFavs))
	for _, f := range allFavs {
		ids = append(ids, f.PromptID)
	}

	c.JSON(http.StatusOK, gin.H{
		"favorite_ids": ids,
	})
}

// ─── 内部助手函数 ────────────────────────────────────────────────────

func (h *GalleryHandler) processAndSaveSingleWork(ctx context.Context, req SaveWorkRequest, clientIP string) (*db.GalleryWork, error) {
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = fmt.Sprintf("img-%d-%s", time.Now().UnixMilli(), uuid.NewString()[:8])
	}

	var binaryData []byte
	mimeType := "image/png"

	// 1. 解析 Base64 数据
	if req.B64JSON != "" {
		cleanB64 := req.B64JSON
		if idx := strings.Index(cleanB64, ","); idx != -1 {
			cleanB64 = cleanB64[idx+1:]
		}
		decoded, err := base64.StdEncoding.DecodeString(cleanB64)
		if err == nil && len(decoded) > 0 {
			binaryData = decoded
		}
	} else if strings.HasPrefix(req.URL, "data:") {
		// 2. 解析 data:image/...;base64,
		commaIdx := strings.Index(req.URL, ",")
		if commaIdx != -1 {
			header := req.URL[:commaIdx]
			if strings.Contains(header, "image/jpeg") || strings.Contains(header, "image/jpg") {
				mimeType = "image/jpeg"
			} else if strings.Contains(header, "image/webp") {
				mimeType = "image/webp"
			}
			b64 := req.URL[commaIdx+1:]
			decoded, err := base64.StdEncoding.DecodeString(b64)
			if err == nil && len(decoded) > 0 {
				binaryData = decoded
			}
		}
	} else if strings.HasPrefix(req.URL, "http://") || strings.HasPrefix(req.URL, "https://") {
		// 3. 远端 URL：如果可用则尝试下载转存为本地文件
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(req.URL)
		if err == nil && resp.StatusCode == 200 {
			data, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr == nil && len(data) > 0 {
				binaryData = data
				if ct := resp.Header.Get("Content-Type"); ct != "" {
					mimeType = ct
				}
			}
		}
	}

	storageType := "local"
	url := req.URL
	filePath := ""
	fileSize := int64(len(binaryData))

	if len(binaryData) > 0 && h.storageEngine != nil {
		savedURL, savedPath, saveErr := h.storageEngine.Save(ctx, id, binaryData, mimeType)
		if saveErr == nil {
			url = savedURL
			filePath = savedPath
			storageType = h.storageEngine.Type()
		} else {
			// 文件存储引擎写入失败时，退回到数据库内 BlobData
			storageType = "blob"
			url = "/storage/images/" + id + ".png"
		}
	} else if url == "" {
		return nil, fmt.Errorf("no valid image data or URL provided for work %s", id)
	}

	createdAt := time.Now().UTC()
	if req.Timestamp > 0 {
		createdAt = time.UnixMilli(req.Timestamp).UTC()
	}

	work := db.GalleryWork{
		ID:            id,
		Prompt:        req.Prompt,
		RevisedPrompt: req.RevisedPrompt,
		Model:         req.Model,
		Size:          req.Size,
		Quality:       req.Quality,
		Mode:          req.Mode,
		Category:      normalizeGalleryCategory(req.Category),
		Source:        normalizeGallerySource(req.Source),
		ProjectID:     strings.TrimSpace(req.ProjectID),
		ProjectTitle:  strings.TrimSpace(req.ProjectTitle),
		RoleName:      strings.TrimSpace(req.RoleName),
		ShotIndex:     req.ShotIndex,
		Tags:          normalizeGalleryTags(req.Tags),
		StorageType:   storageType,
		FilePath:      filePath,
		URL:           url,
		FileSize:      fileSize,
		MimeType:      mimeType,
		LatencyMs:     req.LatencyMs,
		IsFavorite:    req.IsFavorite,
		ClientIP:      clientIP,
		CreatedAt:     createdAt,
		UpdatedAt:     time.Now().UTC(),
	}

	// 仅在无物理文件落盘时保存 BlobData 作为兜底
	if storageType == "blob" || storageType == "memory" {
		work.BlobData = binaryData
	}

	if err := h.db.Save(&work).Error; err != nil {
		return nil, fmt.Errorf("save gallery work to db: %w", err)
	}

	return &work, nil
}

func normalizeGallerySource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "image_studio"
	}
	return source
}

// ImportCrawledImage 将爬虫下载的参考图字节写入统一存储与画廊系统库，供生图/视频/短剧工坊直接复用。
func (h *GalleryHandler) ImportCrawledImage(ctx context.Context, id, title, summary, mode, sourceURL, mimeType string, data []byte) (*db.GalleryWork, error) {
	cleanID := strings.TrimSpace(id)
	if cleanID == "" {
		cleanID = "crawl_img_" + uuid.NewString()[:8]
	}
	if mimeType == "" {
		mimeType = "image/jpeg"
	}
	fileURL := sourceURL
	filePath := ""
	storageType := "local"
	if len(data) > 0 && h.storageEngine != nil {
		if mse, ok := h.storageEngine.(storage.MediaStorageEngine); ok && mse != nil {
			if u, p, err := mse.SaveInCategory(ctx, "images", cleanID, data, mimeType); err == nil {
				fileURL = u
				filePath = p
				storageType = h.storageEngine.Type()
			}
		} else if u, p, err := h.storageEngine.Save(ctx, cleanID, data, mimeType); err == nil {
			fileURL = u
			filePath = p
			storageType = h.storageEngine.Type()
		}
	}

	now := time.Now().UTC()
	work := db.GalleryWork{
		ID:            cleanID,
		Prompt:        strings.TrimSpace(title),
		RevisedPrompt: strings.TrimSpace(summary),
		Model:         "crawled-ref",
		Size:          "1024x1024",
		Quality:       "hd",
		Mode:          mode,
		StorageType:   storageType,
		FilePath:      filePath,
		URL:           fileURL,
		FileSize:      int64(len(data)),
		MimeType:      mimeType,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if storageType == "memory" || storageType == "blob" {
		work.BlobData = data
	}
	if h.db != nil {
		if err := h.db.Save(&work).Error; err != nil {
			return nil, fmt.Errorf("save crawled image work: %w", err)
		}
	} else {
		h.mu.Lock()
		h.memWorks = append([]db.GalleryWork{work}, h.memWorks...)
		h.mu.Unlock()
	}
	return &work, nil
}

// SearchWorks 返回与关键词匹配的画廊与爬虫系统参考图，供 Chat 爬虫先行检查现有缓存库。
func (h *GalleryHandler) SearchWorks(query string, limit int) []db.GalleryWork {
	if limit <= 0 {
		limit = 20
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if h.db != nil {
		var items []db.GalleryWork
		tx := h.db.Model(&db.GalleryWork{}).Omit("blob_data").Order("created_at desc").Limit(limit)
		if q != "" {
			pat := "%" + q + "%"
			tx = tx.Where("LOWER(prompt) LIKE ? OR LOWER(revised_prompt) LIKE ? OR LOWER(mode) LIKE ?", pat, pat, pat)
		}
		if err := tx.Find(&items).Error; err == nil {
			return items
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]db.GalleryWork, 0, limit)
	for _, w := range h.memWorks {
		if q == "" || strings.Contains(strings.ToLower(w.Prompt+" "+w.RevisedPrompt+" "+w.Mode), q) {
			out = append(out, w)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}
