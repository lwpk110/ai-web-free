package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/gatewaykeys"

	"github.com/gin-gonic/gin"
)

func TestGatewayKeyRoutesToGroupAccounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	free := accounts.NewAccount("free-1", accounts.TypeFree, "tok-free")
	free.Status = accounts.StatusActive
	free.Models = []string{"gpt-5-mini", "o3"}
	paid := accounts.NewAccount("paid-1", accounts.TypePUID, "tok-paid")
	paid.Status = accounts.StatusActive
	pool := accounts.NewPool([]*accounts.Account{free, paid})

	store, _ := gatewaykeys.NewStore(nil, "")
	g, _ := store.CreateGroup(gatewaykeys.Group{Name: "free", Enabled: true,
		ModelAllowlist: []string{"gpt-5-mini"}, ModelAliases: map[string]string{"gpt-4o": "gpt-5-mini"}})
	_, plain, _ := store.CreateKey("agent", g.ID, 0, 0)

	r := gin.New()
	r.Use(GatewayKeyAuth(store), LegacyAuthorization(func(c *gin.Context) {
		c.AbortWithStatus(http.StatusTeapot) // 旧鉴权不应被网关 Key 请求触发
	}))
	r.GET("/acct", func(c *gin.Context) {
		a, st, err, ok := acquireForGateway(c, pool, true, "", nil)
		if !ok || err != nil {
			c.JSON(st, gin.H{"err": "none"})
			return
		}
		c.JSON(200, gin.H{"id": a.ID, "auth": c.GetHeader("Authorization")})
	})
	r.GET("/models", func(c *gin.Context) {
		res, ok := gatewayModelsResponse(c, pool)
		if !ok {
			c.Status(204)
			return
		}
		c.JSON(200, res)
	})

	do := func(path, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if w := do("/acct", plain); w.Code != 200 || !contains(w.Body.String(), `"id":"free-1"`) || !contains(w.Body.String(), `"auth":""`) {
		t.Fatalf("expected free account without auth header, got %d %s", w.Code, w.Body.String())
	}
	if w := do("/acct", "sk-gw-bogus"); w.Code != http.StatusUnauthorized {
		t.Fatalf("bad key should 401, got %d", w.Code)
	}
	w := do("/models", plain)
	if w.Code != 200 || contains(w.Body.String(), `"o3"`) || !contains(w.Body.String(), `"gpt-4o"`) || !contains(w.Body.String(), `"gpt-5-mini"`) {
		t.Fatalf("models should be whitelist+alias only, got %d %s", w.Code, w.Body.String())
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
