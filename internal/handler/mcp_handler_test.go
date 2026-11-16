package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/config"
	"ai-web-free/internal/opslog"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/proxy"

	"github.com/gin-gonic/gin"
)

func setupMCPTestRouter(t *testing.T) (*gin.Engine, *MCPHandler, *opslog.Buffer) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	pool := accounts.NewPool(nil)
	reg := provider.NewRegistry()
	proxyPool := proxy.NewPool(nil)
	cfg := &config.Config{}
	opsBuf := opslog.NewBuffer(100)

	rt := &GatewayRuntime{
		AccountPool: pool,
		ProxyPool:   proxyPool,
		OpsLog:      opsBuf,
		Config:      cfg,
	}
	adminH := NewAdminHandler(reg, proxyPool, cfg)
	adminH.SetRuntime(rt)

	audioH := NewAudioHandler(pool, cfg)
	audioH.SetRuntime(rt)
	audioH.SetRegistry(reg)

	videoH := NewVideoHandler(reg, nil)
	galleryH := NewGalleryHandler(nil, nil)
	mediaH := NewMediaLibraryHandler(audioH, galleryH, videoH, nil)
	chatWB := NewChatWorkbenchHandler(nil, nil)
	dramaH := NewDramaHandler(nil, pool, reg, nil)

	mcpH := NewMCPHandler(MCPDeps{
		Admin:        adminH,
		Drama:        dramaH,
		Gallery:      galleryH,
		Audio:        audioH,
		Video:        videoH,
		MediaLibrary: mediaH,
		ChatWork:     chatWB,
		OpsLog:       opsBuf,
	})

	r := gin.New()
	admin := r.Group("/admin/api/v1")
	admin.POST("/mcp", mcpH.HandleMCP)
	admin.GET("/mcp/openai-tools", mcpH.ListOpenAITools)
	admin.POST("/mcp/execute", mcpH.ExecuteInternalTool)
	admin.POST("/drama/mcp", mcpH.HandleDramaMCP)
	return r, mcpH, opsBuf
}

func postJSON(r *gin.Engine, path string, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestUnifiedMCPScopeAndReadOnly(t *testing.T) {
	r, _, opsBuf := setupMCPTestRouter(t)

	// 1. Unified tools/list with scope=ops
	rec := postJSON(r, "/admin/api/v1/mcp?scope=ops", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var listResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal tools/list: %v", err)
	}
	if len(listResp.Result.Tools) != 7 {
		t.Fatalf("expected 7 ops tools, got %d", len(listResp.Result.Tools))
	}

	// 2. ReadOnly mode filters out ops_repair, ops_manage_proxies, ops_manage_accounts
	recRO := postJSON(r, "/admin/api/v1/mcp?scope=ops&mode=readonly", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	var roResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	_ = json.Unmarshal(recRO.Body.Bytes(), &roResp)
	if len(roResp.Result.Tools) != 4 {
		t.Fatalf("expected 4 readonly ops tools, got %d", len(roResp.Result.Tools))
	}

	// 3. ReadOnly mode rejects ops_repair call
	recDenied := postJSON(r, "/admin/api/v1/mcp?scope=ops&mode=readonly", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ops_repair","arguments":{"action":"smart_heal","confirm":true}}}`)
	var deniedResp struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(recDenied.Body.Bytes(), &deniedResp)
	if deniedResp.Error == nil || deniedResp.Error.Code != -32003 {
		t.Fatalf("expected -32003 readonly error, got %s", recDenied.Body.String())
	}

	// 4. ops_repair dry_run works without confirm, but without dry_run & confirm=true returns guardrail -32001
	recDry := postJSON(r, "/admin/api/v1/mcp?scope=ops", `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"ops_repair","arguments":{"action":"smart_heal","dry_run":true}}}`)
	if !bytes.Contains(recDry.Body.Bytes(), []byte("dry_run")) {
		t.Fatalf("expected dry_run response, got %s", recDry.Body.String())
	}
	recGuard := postJSON(r, "/admin/api/v1/mcp?scope=ops", `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"ops_repair","arguments":{"action":"smart_heal"}}}`)
	var guardResp struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(recGuard.Body.Bytes(), &guardResp)
	if guardResp.Error == nil || guardResp.Error.Code != -32001 {
		t.Fatalf("expected -32001 guardrail error, got %s", recGuard.Body.String())
	}

	// 5. Drama compatibility endpoint /admin/api/v1/drama/mcp forces scope=drama
	recDrama := postJSON(r, "/admin/api/v1/drama/mcp", `{"jsonrpc":"2.0","id":6,"method":"tools/list"}`)
	var dramaResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	_ = json.Unmarshal(recDrama.Body.Bytes(), &dramaResp)
	if len(dramaResp.Result.Tools) == 0 {
		t.Fatalf("expected drama tools from /drama/mcp")
	}
	for _, tl := range dramaResp.Result.Tools {
		if len(tl.Name) < 6 || tl.Name[:6] != "drama_" {
			t.Fatalf("unexpected non-drama tool %q in /drama/mcp", tl.Name)
		}
	}

	// 6. Internal Copilot OpenAI bridge
	reqOAI := httptest.NewRequest(http.MethodGet, "/admin/api/v1/mcp/openai-tools?scope=ops&mode=readonly", nil)
	recOAI := httptest.NewRecorder()
	r.ServeHTTP(recOAI, reqOAI)
	if recOAI.Code != http.StatusOK {
		t.Fatalf("expected 200 for openai-tools, got %d", recOAI.Code)
	}

	recExec := postJSON(r, "/admin/api/v1/mcp/execute", `{"name":"ops_diagnostics","arguments":{},"scope":"ops","mode":"readonly"}`)
	if recExec.Code != http.StatusOK {
		t.Fatalf("expected 200 for internal execute, got %d: %s", recExec.Code, recExec.Body.String())
	}

	// 7. OpsLog should have recorded MCP audit entries
	entries, _ := opsBuf.Query(opslog.QueryOptions{Search: "mcp", Limit: 20})
	if len(entries) == 0 {
		t.Fatalf("expected MCP audit entries in opslog")
	}
}
