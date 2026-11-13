package middlewares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAdminAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 1. 未设置 ADMIN_TOKEN 时默认免登放行
	rOpen := gin.New()
	rOpen.Use(AdminAuthorization(""))
	rOpen.GET("/admin/api/v1/overview", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	w := httptest.NewRecorder()
	rOpen.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/api/v1/overview", nil))
	if w.Code != 200 {
		t.Fatalf("open admin expected 200, got %d", w.Code)
	}

	// 2. 配置 ADMIN_TOKEN 时拦截未授权请求，但放行 auth/status 与正确密钥
	rLocked := gin.New()
	rLocked.Use(AdminAuthorization("secret-123"))
	rLocked.GET("/admin/api/v1/auth/status", func(c *gin.Context) { c.JSON(200, gin.H{"auth_required": true}) })
	rLocked.GET("/admin/api/v1/overview", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	wStatus := httptest.NewRecorder()
	rLocked.ServeHTTP(wStatus, httptest.NewRequest(http.MethodGet, "/admin/api/v1/auth/status", nil))
	if wStatus.Code != 200 {
		t.Fatalf("auth/status should bypass lock, got %d", wStatus.Code)
	}

	wDenied := httptest.NewRecorder()
	rLocked.ServeHTTP(wDenied, httptest.NewRequest(http.MethodGet, "/admin/api/v1/overview", nil))
	if wDenied.Code != 401 {
		t.Fatalf("missing token expected 401, got %d", wDenied.Code)
	}

	reqOK := httptest.NewRequest(http.MethodGet, "/admin/api/v1/overview", nil)
	reqOK.Header.Set("X-Admin-Token", "secret-123")
	wOK := httptest.NewRecorder()
	rLocked.ServeHTTP(wOK, reqOK)
	if wOK.Code != 200 {
		t.Fatalf("valid X-Admin-Token expected 200, got %d", wOK.Code)
	}

	reqBearer := httptest.NewRequest(http.MethodGet, "/admin/api/v1/overview", nil)
	reqBearer.Header.Set("Authorization", "Bearer secret-123")
	wBearer := httptest.NewRecorder()
	rLocked.ServeHTTP(wBearer, reqBearer)
	if wBearer.Code != 200 {
		t.Fatalf("valid Bearer token expected 200, got %d", wBearer.Code)
	}
}
