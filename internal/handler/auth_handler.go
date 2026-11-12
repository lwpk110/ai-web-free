package handler

import (
	"net/http"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/chatgpt"
	officialtypes "ai-web-free/typings/official"
	bogdanfinn "ai-web-free/httpclient/bogdanfinn"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	accountPool *accounts.Pool
}

func NewAuthHandler(pool *accounts.Pool) *AuthHandler {
	return &AuthHandler{accountPool: pool}
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	var req officialtypes.OpenAIRefreshToken
	if err := c.BindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	client := bogdanfinn.NewStdClient()
	result, status, err := chatgpt.GETTokenForRefreshToken(client, req.RefreshToken, "")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"message": "Request must be proper JSON",
			"type":    "invalid_request_error",
			"param":   nil,
			"code":    err.Error(),
		})
		return
	}
	c.JSON(status, result)
}

func (h *AuthHandler) Session(c *gin.Context) {
	var req officialtypes.OpenAISessionToken
	if err := c.BindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	client := bogdanfinn.NewStdClient()
	result, status, err := chatgpt.GETTokenForSessionToken(client, req.SessionToken, "")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "Request must be proper JSON",
				"type":    "invalid_request_error",
				"param":   nil,
				"code":    err.Error(),
			},
		})
		return
	}
	c.JSON(status, result)
}
