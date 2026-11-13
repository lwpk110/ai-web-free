package middlewares

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

func Authorization(c *gin.Context) {
	customer_key := os.Getenv("Authorization")
	if customer_key != "" {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(401, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}
		tokenParts := strings.Split(strings.Replace(authHeader, "Bearer ", "", 1), " ")
		customAccessToken := tokenParts[0]
		if customer_key != customAccessToken {
			c.JSON(401, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}
		if len(tokenParts) > 1 {
			openaiAccessToken := tokenParts[1]
			c.Request.Header.Set("Authorization", "Bearer "+openaiAccessToken)
		}
	}
	c.Next()
}

// ExtractAdminToken 从请求头读取管理员密钥，支持 Authorization: Bearer 与 X-Admin-Token。
func ExtractAdminToken(c *gin.Context) string {
	if xToken := strings.TrimSpace(c.GetHeader("X-Admin-Token")); xToken != "" {
		return xToken
	}
	auth := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(auth) >= 7 && strings.EqualFold(auth[:7], "Bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return auth
}

// MatchAdminToken 使用常量时间比对校验管理员密钥。
func MatchAdminToken(expected, actual string) bool {
	if expected == "" {
		return true
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

// AdminAuthorization 当配置了 adminToken 时保护 /admin/api/v1/* 接口，未配置时免登放行。
func AdminAuthorization(adminToken string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if adminToken == "" {
			c.Next()
			return
		}
		if c.Request.URL.Path == "/admin/api/v1/auth/status" {
			c.Next()
			return
		}
		provided := ExtractAdminToken(c)
		if !MatchAdminToken(adminToken, provided) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{
					"message": "管理端已启用 ADMIN_TOKEN 保护，请先解锁",
					"code":    "admin_unauthorized",
				},
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
