package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"ai-web-free/internal/deals"
	"ai-web-free/internal/promptplaza"
	"github.com/gin-gonic/gin"
)

type DealsHandler struct {
	service      *deals.Service
	chatExecutor promptplaza.PoolChatExecutor
}

func NewDealsHandler(s *deals.Service) *DealsHandler {
	return &DealsHandler{service: s}
}

func (h *DealsHandler) SetChatExecutor(executor promptplaza.PoolChatExecutor) {
	h.chatExecutor = executor
}

func (h *DealsHandler) getService() *deals.Service {
	if h.service != nil {
		return h.service
	}
	return deals.GetService()
}

type dealsChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatAnswers 通过本机号池回答线报问题，可按需开启联网搜索。
func (h *DealsHandler) Chat(c *gin.Context) {
	if h.chatExecutor == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "线报 AI 助手暂不可用"})
		return
	}
	var req struct {
		Messages  []dealsChatMessage `json:"messages"`
		WebSearch *bool              `json:"web_search"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "对话内容格式无效"})
		return
	}
	if len(req.Messages) == 0 || len(req.Messages) > 12 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供最近 1 至 12 条对话消息"})
		return
	}
	var history strings.Builder
	for _, message := range req.Messages {
		role := strings.TrimSpace(message.Role)
		content := strings.TrimSpace(message.Content)
		if (role != "user" && role != "assistant") || content == "" || utf8.RuneCountInString(content) > 4000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "每条对话需包含有效的用户或助手文本，且不超过 4000 字符"})
			return
		}
		label := "用户"
		if role == "assistant" {
			label = "助手"
		}
		history.WriteString(label)
		history.WriteString("：\n")
		history.WriteString(content)
		history.WriteString("\n\n")
	}
	if strings.TrimSpace(req.Messages[len(req.Messages)-1].Role) != "user" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "最后一条消息必须来自用户"})
		return
	}
	useWebSearch := req.WebSearch == nil || *req.WebSearch
	systemPrompt := "你是免费额度与开发者福利线报助手。使用中文回答，结合上下文持续对话；对时效性问题优先联网核查，区分已核实信息与未核实线索。不要编造平台政策、互动数据或搜索来源。若联网结果提供来源，请附上可点击的来源标题和完整 URL；搜索不可用时要明确说明。回答简洁并指出信息时效。"
	userPrompt := "以下是本轮对话历史，请结合上下文回答最后一个用户问题：\n\n" + history.String()
	if srv := h.getService(); srv != nil {
		items, _, listErr := srv.ListDeals(deals.DealsQuery{Sort: "hot", Limit: 12})
		if listErr == nil {
			var contextLines []string
			for _, item := range items {
				if strings.HasPrefix(item.ID, "seed-") {
					continue
				}
				summary := []rune(item.Summary)
				if len(summary) > 240 {
					summary = summary[:240]
				}
				contextLines = append(contextLines, "- "+item.Title+"（"+item.Source+"；热度指数 "+strconv.FormatFloat(item.Score, 'f', 1, 64)+"）\n  "+string(summary)+"\n  "+item.URL)
			}
			if len(contextLines) > 0 {
				userPrompt = "以下是线报板块当前采集到的线索，仅作背景参考，需核实时效信息时请以联网来源为准：\n" + strings.Join(contextLines, "\n") + "\n\n" + userPrompt
			}
		}
	}
	if useWebSearch {
		userPrompt += "本轮已启用全网搜索，请检索最新公开信息并附来源链接。"
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()
	model := "auto"
	if useWebSearch {
		model = "gpt-5-6" // 当前仅 ChatGPT 号池的搜索提示支持真实联网检索。
	}
	answer, err := h.chatExecutor(ctx, systemPrompt, userPrompt, useWebSearch, model)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"answer": answer})
}

// ListDeals 查询羊毛与福利资讯列表
// GET /admin/api/v1/deals?sort=hot|newest&hot_only=true&source=all|linuxdo|nodeseek|v2ex&category=all|ai|api|vps|card&keyword=...&limit=30&offset=0
func (h *DealsHandler) ListDeals(c *gin.Context) {
	srv := h.getService()
	if srv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "deals service not initialized"})
		return
	}

	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	query := deals.DealsQuery{
		Sort:     c.DefaultQuery("sort", "hot"),
		HotOnly:  c.Query("hot_only") == "true" || c.Query("hot_only") == "1",
		Source:   c.DefaultQuery("source", "all"),
		Category: c.DefaultQuery("category", "all"),
		Keyword:  c.Query("keyword"),
		Limit:    limit,
		Offset:   offset,
	}

	items, total, err := srv.ListDeals(query)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"total": total,
		"query": query,
	})
}

// RefreshDeals 手动触发即时抓取更新
// POST /admin/api/v1/deals/refresh
func (h *DealsHandler) RefreshDeals(c *gin.Context) {
	srv := h.getService()
	if srv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "deals service not initialized"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	count, err := srv.RefreshNow(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "刷新成功",
		"updated": count,
		"status":  srv.Status(),
	})
}

// GetDealsStatus 查询抓取调度状态
// GET /admin/api/v1/deals/status
func (h *DealsHandler) GetDealsStatus(c *gin.Context) {
	srv := h.getService()
	if srv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "deals service not initialized"})
		return
	}

	c.JSON(http.StatusOK, srv.Status())
}
