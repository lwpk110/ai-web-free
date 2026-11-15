package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ai-web-free/internal/db"
	"ai-web-free/internal/vectorstore"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ChatWorkbenchHandler 提供全功能 AI 对话工作台管理 API
type ChatWorkbenchHandler struct {
	db          *gorm.DB
	vectorStore vectorstore.VectorStore
}

// NewChatWorkbenchHandler 实例化 Chat 工作台处理器
func NewChatWorkbenchHandler(database *gorm.DB, vs vectorstore.VectorStore) *ChatWorkbenchHandler {
	return &ChatWorkbenchHandler{
		db:          database,
		vectorStore: vs,
	}
}

// ─── 会话 CRUD ─────────────────────────────────────────────────────────────

type CreateConversationRequest struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt"`
	PresetID     string `json:"preset_id"`
	IsPinned     bool   `json:"is_pinned"`
}

type UpdateConversationRequest struct {
	Title        *string `json:"title"`
	Model        *string `json:"model"`
	SystemPrompt *string `json:"system_prompt"`
	PresetID     *string `json:"preset_id"`
	IsPinned     *bool   `json:"is_pinned"`
	IsArchived   *bool   `json:"is_archived"`
	Tags         *string `json:"tags"`
}

// ListConversations 分页获取会话列表
func (h *ChatWorkbenchHandler) ListConversations(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusOK, gin.H{"conversations": []interface{}{}, "total": 0, "page": 1, "limit": 30})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	if limit < 1 || limit > 100 {
		limit = 30
	}
	offset := (page - 1) * limit

	query := h.db.Model(&db.Conversation{})

	if archivedStr := c.Query("archived"); archivedStr != "" {
		isArchived := archivedStr == "1" || strings.ToLower(archivedStr) == "true"
		query = query.Where("is_archived = ?", isArchived)
	} else {
		// 默认只返回未归档会话
		query = query.Where("is_archived = ?", false)
	}

	if q := strings.TrimSpace(c.Query("q")); q != "" {
		likePattern := "%" + q + "%"
		query = query.Where("title LIKE ?", likePattern)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var conversations []db.Conversation
	if err := query.Order("is_pinned desc, updated_at desc").Limit(limit).Offset(offset).Find(&conversations).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"conversations": conversations,
		"total":         total,
		"page":          page,
		"limit":         limit,
	})
}

// CreateConversation 新建会话
func (h *ChatWorkbenchHandler) CreateConversation(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	var req CreateConversationRequest
	_ = c.ShouldBindJSON(&req)

	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = "conv-" + uuid.NewString()
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "新建对话"
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = "auto"
	}

	now := time.Now().UTC()
	conv := db.Conversation{
		ID:           id,
		Title:        title,
		Model:        model,
		SystemPrompt: strings.TrimSpace(req.SystemPrompt),
		PresetID:     strings.TrimSpace(req.PresetID),
		IsPinned:     req.IsPinned,
		IsArchived:   false,
		ClientIP:     c.ClientIP(),
		MessageCount: 0,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := h.db.Create(&conv).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, conv)
}

// GetConversation 获取单个会话详情与消息
func (h *ChatWorkbenchHandler) GetConversation(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	id := c.Param("id")
	var conv db.Conversation
	if err := h.db.Where("id = ?", id).First(&conv).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var messages []db.ChatMessageRecord
	if err := h.db.Where("conversation_id = ? OR session_key = ?", id, id).Order("created_at asc").Find(&messages).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"conversation": conv,
		"messages":     messages,
	})
}

// UpdateConversation 修改会话元数据
func (h *ChatWorkbenchHandler) UpdateConversation(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	id := c.Param("id")
	var conv db.Conversation
	if err := h.db.Where("id = ?", id).First(&conv).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var req UpdateConversationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	updates := map[string]interface{}{
		"updated_at": time.Now().UTC(),
	}
	if req.Title != nil {
		updates["title"] = strings.TrimSpace(*req.Title)
	}
	if req.Model != nil {
		updates["model"] = strings.TrimSpace(*req.Model)
	}
	if req.SystemPrompt != nil {
		updates["system_prompt"] = strings.TrimSpace(*req.SystemPrompt)
	}
	if req.PresetID != nil {
		updates["preset_id"] = strings.TrimSpace(*req.PresetID)
	}
	if req.IsPinned != nil {
		updates["is_pinned"] = *req.IsPinned
	}
	if req.IsArchived != nil {
		updates["is_archived"] = *req.IsArchived
	}
	if req.Tags != nil {
		updates["tags"] = strings.TrimSpace(*req.Tags)
	}

	if err := h.db.Model(&conv).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	_ = h.db.Where("id = ?", id).First(&conv)
	c.JSON(http.StatusOK, conv)
}

// DeleteConversation 级联删除会话及其关联消息
func (h *ChatWorkbenchHandler) DeleteConversation(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	id := c.Param("id")
	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("conversation_id = ? OR session_key = ?", id, id).Delete(&db.ChatMessageRecord{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&db.Conversation{}).Error
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "id": id})
}

// ─── 消息增删改查 ─────────────────────────────────────────────────────────

type SaveConversationMessagesRequest struct {
	Messages []ChatMessageItem `json:"messages" binding:"required"`
}

// SaveConversationMessages 保存或覆盖会话消息并自动更新元数据
func (h *ChatWorkbenchHandler) SaveConversationMessages(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	convID := c.Param("id")
	if strings.TrimSpace(convID) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "conversation id is required"})
		return
	}

	var req SaveConversationMessagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	err := h.db.Transaction(func(tx *gorm.DB) error {
		// 确保会话实体存在，若不存在则自动兜底创建
		var conv db.Conversation
		if findErr := tx.Where("id = ?", convID).First(&conv).Error; findErr != nil {
			if findErr == gorm.ErrRecordNotFound {
				now := time.Now().UTC()
				autoTitle := "对话 " + convID
				if len(req.Messages) > 0 {
					for _, m := range req.Messages {
						if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
							runes := []rune(strings.TrimSpace(m.Content))
							if len(runes) > 30 {
								autoTitle = string(runes[:30]) + "..."
							} else {
								autoTitle = string(runes)
							}
							break
						}
					}
				}
				conv = db.Conversation{
					ID:           convID,
					Title:        autoTitle,
					Model:        "auto",
					ClientIP:     c.ClientIP(),
					CreatedAt:    now,
					UpdatedAt:    now,
					MessageCount: len(req.Messages),
				}
				if createErr := tx.Create(&conv).Error; createErr != nil {
					return createErr
				}
			} else {
				return findErr
			}
		}

		// 清理旧消息并写入新消息
		if delErr := tx.Where("conversation_id = ? OR session_key = ?", convID, convID).Delete(&db.ChatMessageRecord{}).Error; delErr != nil {
			return delErr
		}

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
				ID:             msgID,
				ConversationID: convID,
				SessionKey:     convID,
				Role:           m.Role,
				Content:        m.Content,
				Model:          m.Model,
				OpenUIMode:     m.OpenUIMode,
				Skill:          m.Skill,
				TokensPrompt:   m.TokensPrompt,
				TokensComp:     m.TokensComp,
				LatencyMs:      m.LatencyMs,
				Citations:      m.Citations,
				CreatedAt:      createdAt,
			}
			if createErr := tx.Create(&rec).Error; createErr != nil {
				return createErr
			}
		}

		// 更新会话更新时间与消息条数
		return tx.Model(&db.Conversation{}).Where("id = ?", convID).Updates(map[string]interface{}{
			"updated_at":    time.Now().UTC(),
			"message_count": len(req.Messages),
		}).Error
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "count": len(req.Messages), "conversation_id": convID})
}

// DeleteConversationMessage 删除单条消息
func (h *ChatWorkbenchHandler) DeleteConversationMessage(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	convID := c.Param("id")
	msgID := c.Param("msg_id")

	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND (conversation_id = ? OR session_key = ?)", msgID, convID, convID).Delete(&db.ChatMessageRecord{}).Error; err != nil {
			return err
		}
		var count int64
		_ = tx.Model(&db.ChatMessageRecord{}).Where("conversation_id = ? OR session_key = ?", convID, convID).Count(&count)
		return tx.Model(&db.Conversation{}).Where("id = ?", convID).Updates(map[string]interface{}{
			"message_count": int(count),
			"updated_at":    time.Now().UTC(),
		}).Error
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "deleted_id": msgID})
}

// ─── 全文检索 (Search) ───────────────────────────────────────────────────

type SearchHit struct {
	ConversationID    string    `json:"conversation_id"`
	ConversationTitle string    `json:"conversation_title"`
	MessageID         string    `json:"message_id"`
	Role              string    `json:"role"`
	Snippet           string    `json:"snippet"`
	CreatedAt         time.Time `json:"created_at"`
}

// SearchChat 全局跨会话即时模糊/全文检索
func (h *ChatWorkbenchHandler) SearchChat(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusOK, gin.H{"hits": []interface{}{}, "query": ""})
		return
	}

	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusOK, gin.H{"hits": []interface{}{}, "query": ""})
		return
	}

	likePattern := "%" + q + "%"
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit < 1 || limit > 50 {
		limit = 20
	}

	var records []db.ChatMessageRecord
	if err := h.db.Where("content LIKE ?", likePattern).Order("created_at desc").Limit(limit).Find(&records).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 汇总所有 ConversationID
	convIDs := make([]string, 0, len(records))
	for _, r := range records {
		targetID := r.ConversationID
		if targetID == "" {
			targetID = r.SessionKey
		}
		if targetID != "" {
			convIDs = append(convIDs, targetID)
		}
	}

	convMap := make(map[string]string)
	if len(convIDs) > 0 {
		var convs []db.Conversation
		if err := h.db.Where("id IN ?", convIDs).Find(&convs).Error; err == nil {
			for _, cv := range convs {
				convMap[cv.ID] = cv.Title
			}
		}
	}

	hits := make([]SearchHit, 0, len(records))
	for _, r := range records {
		cid := r.ConversationID
		if cid == "" {
			cid = r.SessionKey
		}
		title := convMap[cid]
		if title == "" {
			title = "未命名对话 (" + cid + ")"
		}

		// 截取关键词上下文片段（前后 40 字符）
		idx := strings.Index(strings.ToLower(r.Content), strings.ToLower(q))
		snippet := r.Content
		if idx != -1 {
			start := idx - 40
			if start < 0 {
				start = 0
			}
			end := idx + len(q) + 40
			if end > len(r.Content) {
				end = len(r.Content)
			}
			snippet = r.Content[start:end]
			if start > 0 {
				snippet = "..." + snippet
			}
			if end < len(r.Content) {
				snippet = snippet + "..."
			}
		} else if len(r.Content) > 120 {
			snippet = r.Content[:120] + "..."
		}

		hits = append(hits, SearchHit{
			ConversationID:    cid,
			ConversationTitle: title,
			MessageID:         r.ID,
			Role:              r.Role,
			Snippet:           snippet,
			CreatedAt:         r.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"query": q,
		"hits":  hits,
		"count": len(hits),
	})
}

// ─── 角色预设 (Presets) ───────────────────────────────────────────────────

// ListPresets 获取角色预设列表
func (h *ChatWorkbenchHandler) ListPresets(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusOK, gin.H{"presets": []interface{}{}})
		return
	}

	var presets []db.ChatPreset
	if err := h.db.Order("sort_order asc, created_at asc").Find(&presets).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"presets": presets})
}

// SavePreset 保存自定义角色预设
func (h *ChatWorkbenchHandler) SavePreset(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	var preset db.ChatPreset
	if err := c.ShouldBindJSON(&preset); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	preset.Name = strings.TrimSpace(preset.Name)
	preset.SystemPrompt = strings.TrimSpace(preset.SystemPrompt)
	if preset.Name == "" || preset.SystemPrompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name and system_prompt are required"})
		return
	}

	if preset.ID == "" {
		preset.ID = "preset-custom-" + uuid.NewString()[:8]
		preset.CreatedAt = time.Now().UTC()
		preset.IsBuiltin = false
	}
	preset.UpdatedAt = time.Now().UTC()

	if err := h.db.Save(&preset).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, preset)
}

// DeletePreset 删除自定义角色预设
func (h *ChatWorkbenchHandler) DeletePreset(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	id := c.Param("id")
	var existing db.ChatPreset
	if err := h.db.Where("id = ?", id).First(&existing).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "preset not found"})
		return
	}
	if existing.IsBuiltin {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot delete builtin preset"})
		return
	}

	if err := h.db.Where("id = ?", id).Delete(&db.ChatPreset{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "id": id})
}

// ─── 导入导出 (Import / Export) ───────────────────────────────────────────

// ExportConversation 导出单个会话为 Markdown 或 JSON
func (h *ChatWorkbenchHandler) ExportConversation(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	id := c.Param("id")
	var conv db.Conversation
	if err := h.db.Where("id = ?", id).First(&conv).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "conversation not found"})
		return
	}

	var messages []db.ChatMessageRecord
	_ = h.db.Where("conversation_id = ? OR session_key = ?", id, id).Order("created_at asc").Find(&messages)

	format := strings.ToLower(strings.TrimSpace(c.DefaultQuery("format", "markdown")))
	if format == "json" {
		c.JSON(http.StatusOK, gin.H{
			"conversation": conv,
			"messages":     messages,
		})
		return
	}

	// 默认导出清晰的 GitHub Markdown
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# %s\n\n", conv.Title))
	sb.WriteString(fmt.Sprintf("- **会话 ID**：%s\n", conv.ID))
	sb.WriteString(fmt.Sprintf("- **模型**：%s\n", conv.Model))
	sb.WriteString(fmt.Sprintf("- **创建时间**：%s\n", conv.CreatedAt.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("- **更新时间**：%s\n\n---\n\n", conv.UpdatedAt.Format("2006-01-02 15:04:05")))

	for _, m := range messages {
		roleName := "👤 **User**"
		if m.Role == "assistant" {
			roleName = "🤖 **Assistant**"
		} else if m.Role == "system" {
			roleName = "⚙️ **System**"
		}
		sb.WriteString(fmt.Sprintf("### %s *(%s)*\n\n%s\n\n---\n\n",
			roleName,
			m.CreatedAt.Format("15:04:05"),
			strings.TrimSpace(m.Content),
		))
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.md", conv.ID))
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(sb.String()))
}

// ImportConversations 批量导入外部 JSON 会话包
func (h *ChatWorkbenchHandler) ImportConversations(c *gin.Context) {
	if h.db == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not initialized"})
		return
	}

	var payload struct {
		Conversations []struct {
			Conversation db.Conversation     `json:"conversation"`
			Messages     []ChatMessageItem   `json:"messages"`
		} `json:"conversations"`
	}

	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	importedCount := 0
	err := h.db.Transaction(func(tx *gorm.DB) error {
		for _, item := range payload.Conversations {
			conv := item.Conversation
			if conv.ID == "" {
				conv.ID = "conv-" + uuid.NewString()
			}
			if conv.Title == "" {
				conv.Title = "已导入对话"
			}
			conv.MessageCount = len(item.Messages)
			conv.UpdatedAt = time.Now().UTC()
			if conv.CreatedAt.IsZero() {
				conv.CreatedAt = time.Now().UTC()
			}

			if err := tx.Save(&conv).Error; err != nil {
				return err
			}

			_ = tx.Where("conversation_id = ? OR session_key = ?", conv.ID, conv.ID).Delete(&db.ChatMessageRecord{}).Error
			for _, m := range item.Messages {
				msgID := strings.TrimSpace(m.ID)
				if msgID == "" {
					msgID = "msg-" + uuid.NewString()
				}
				createdAt := time.Now().UTC()
				if m.Timestamp > 0 {
					createdAt = time.UnixMilli(m.Timestamp).UTC()
				}
				rec := db.ChatMessageRecord{
					ID:             msgID,
					ConversationID: conv.ID,
					SessionKey:     conv.ID,
					Role:           m.Role,
					Content:        m.Content,
					Model:          m.Model,
					OpenUIMode:     m.OpenUIMode,
					Skill:          m.Skill,
					TokensPrompt:   m.TokensPrompt,
					TokensComp:     m.TokensComp,
					LatencyMs:      m.LatencyMs,
					Citations:      m.Citations,
					CreatedAt:      createdAt,
				}
				if err := tx.Create(&rec).Error; err != nil {
					return err
				}
			}
			importedCount++
		}
		return nil
	})

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "imported_count": importedCount})
}
