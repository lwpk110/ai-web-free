package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"ai-web-free/internal/db"
	"ai-web-free/internal/drama"
	"ai-web-free/internal/mcp"
	"ai-web-free/internal/opslog"

	"github.com/gin-gonic/gin"
)

// MCPDeps 聚合统一 MCP 注册表所需的各领域处理器依赖。
type MCPDeps struct {
	Admin        *AdminHandler
	Drama        *DramaHandler
	Gallery      *GalleryHandler
	Audio        *AudioHandler
	Video        *VideoHandler
	MediaLibrary *MediaLibraryHandler
	ChatWork     *ChatWorkbenchHandler
	OpsLog       *opslog.Buffer
}

// MCPHandler 提供内外 Agent 共用的统一 MCP 服务与进程内工具注册表。
type MCPHandler struct {
	deps     MCPDeps
	registry *mcp.Registry
}

// NewMCPHandler 初始化统一 MCP 注册表并注册 ops / drama / studio 三大领域的工具与资源。
func NewMCPHandler(deps MCPDeps) *MCPHandler {
	reg := mcp.NewRegistry("ai-web-free-mcp", "1.0.0")
	if deps.OpsLog != nil {
		reg.SetAuditRecorder(func(ev mcp.AuditEvent) {
			level := "info"
			msg := fmt.Sprintf("MCP %s %s (%s)", ev.Method, ev.Target, ev.Caller)
			if !ev.Success {
				level = "warn"
			}
			deps.OpsLog.Record(opslog.Entry{
				Source:  "backend",
				Level:   level,
				Module:  "mcp:" + string(ev.Scope),
				Message: msg,
				Detail:  ev.ErrorMsg,
			})
		})
	}

	h := &MCPHandler{
		deps:     deps,
		registry: reg,
	}
	h.registerOpsTools()
	h.registerDramaTools()
	h.registerStudioTools()
	return h
}

// Registry 返回底层统一 MCP 注册表，供内部 Agent / Copilot 进程内直调。
func (h *MCPHandler) Registry() *mcp.Registry {
	return h.registry
}

// HandleMCP POST /admin/api/v1/mcp
// 统一 MCP Streamable HTTP (JSON-RPC 2.0) 入口，支持 ?scope=ops|studio|drama|all 与 ?mode=readonly|full。
func (h *MCPHandler) HandleMCP(c *gin.Context) {
	scope := mcp.ParseScope(c.Query("scope"))
	mode := mcp.ParseMode(c.Query("mode"))
	h.serveJSONRPC(c, scope, mode)
}

// HandleDramaMCP POST /admin/api/v1/drama/mcp
// 向后兼容的短剧领域 MCP 入口，强制限定 scope=drama。
func (h *MCPHandler) HandleDramaMCP(c *gin.Context) {
	mode := mcp.ParseMode(c.Query("mode"))
	h.serveJSONRPC(c, mcp.ScopeDrama, mode)
}

func (h *MCPHandler) serveJSONRPC(c *gin.Context, scope mcp.Scope, mode mcp.Mode) {
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusOK, mcp.RPCResponse{
			JSONRPC: "2.0",
			Error:   &mcp.RPCError{Code: mcp.CodeParseError, Message: "read body error: " + err.Error()},
		})
		return
	}
	resp := h.registry.HandleJSONRPC(c.Request.Context(), mcp.CallContext{
		Caller:   "external:mcp",
		Scope:    scope,
		Mode:     mode,
		ClientIP: c.ClientIP(),
	}, raw)
	c.JSON(http.StatusOK, resp)
}

// ListOpenAITools GET /admin/api/v1/mcp/openai-tools
// 将当前统一注册表中的工具导出为 OpenAI tools[] 格式，供内部 Chat Copilot 使用。
func (h *MCPHandler) ListOpenAITools(c *gin.Context) {
	scope := mcp.ParseScope(c.Query("scope"))
	mode := mcp.ParseMode(c.Query("mode"))
	c.JSON(http.StatusOK, gin.H{
		"scope": scope,
		"mode":  mode,
		"tools": h.registry.OpenAITools(scope, mode),
	})
}

// ExecuteInternalTool POST /admin/api/v1/mcp/execute
// 供内部 Chat Copilot 通过统一注册表进程内执行工具调用（共用鉴权、护栏与审计日志）。
func (h *MCPHandler) ExecuteInternalTool(c *gin.Context) {
	var req struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		Scope     string          `json:"scope"`
		Mode      string          `json:"mode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	scope := mcp.ParseScope(req.Scope)
	mode := mcp.ParseMode(req.Mode)
	if len(req.Arguments) == 0 {
		req.Arguments = json.RawMessage(`{}`)
	}
	res, err := h.registry.CallTool(c.Request.Context(), mcp.CallContext{
		Caller:   "internal:copilot",
		Scope:    scope,
		Mode:     mode,
		ClientIP: c.ClientIP(),
	}, req.Name, req.Arguments)
	if err != nil {
		status := http.StatusBadRequest
		var coded *mcp.CodedError
		if errors.As(err, &coded) {
			switch coded.Code {
			case mcp.CodeReadOnlyDenied:
				status = http.StatusForbidden
			case mcp.CodeGuardrail:
				status = http.StatusConflict
			case mcp.CodeMethodNotFound:
				status = http.StatusNotFound
			}
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"result": res})
}

// invokeGinJSON 在进程内直接复用来着各域 Gin Handler 的校验、序列化与业务逻辑，避免重复实现。
func invokeGinJSON(
	ctx context.Context,
	fn gin.HandlerFunc,
	method string,
	path string,
	params gin.Params,
	query url.Values,
	body interface{},
) (interface{}, error) {
	if fn == nil {
		return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "handler not initialized")
	}
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	target := path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req, err := http.NewRequestWithContext(ctx, method, target, bodyReader)
	if err != nil {
		return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.Request = req
	c.Params = params

	fn(c)

	raw := bytes.TrimSpace(rec.Body.Bytes())
	var parsed interface{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &parsed); err != nil {
			parsed = string(raw)
		}
	}
	if rec.Code >= http.StatusBadRequest {
		return nil, mcp.ErrorWithCode(mcp.CodeToolError, "handler returned %d: %s", rec.Code, string(raw))
	}
	return parsed, nil
}

// ─── 1. 运维控制面 (ops) ────────────────────────────────────────────────────────

func (h *MCPHandler) registerOpsTools() {
	r := h.registry
	admin := h.deps.Admin

	r.RegisterTool(mcp.ToolDef{
		Name:        "ops_get_overview",
		Description: "获取平台运行总览、号池健康统计、代理汇总与系统状态",
		Scope:       mcp.ScopeOps,
		ReadOnly:    true,
		Annotations: &mcp.ToolAnnotations{Title: "Platform Overview", ReadOnlyHint: mcp.BoolPtr(true), DestructiveHint: mcp.BoolPtr(false), IdempotentHint: mcp.BoolPtr(true)},
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Handler: func(ctx context.Context, _ mcp.CallContext, _ json.RawMessage) (interface{}, error) {
			if admin == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "admin handler not initialized")
			}
			overview, err := invokeGinJSON(ctx, admin.Overview, http.MethodGet, "/admin/api/v1/overview", nil, nil, nil)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{
				"overview":      overview,
				"system_status": admin.BuildServerStatus(),
			}, nil
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "ops_diagnostics",
		Description: "执行全平台健康诊断，返回健康评分、根因分析、受影响账号/代理及推荐修复方案",
		Scope:       mcp.ScopeOps,
		ReadOnly:    true,
		Annotations: &mcp.ToolAnnotations{Title: "Platform Diagnostics", ReadOnlyHint: mcp.BoolPtr(true), DestructiveHint: mcp.BoolPtr(false), IdempotentHint: mcp.BoolPtr(true)},
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Handler: func(_ context.Context, _ mcp.CallContext, _ json.RawMessage) (interface{}, error) {
			if admin == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "admin handler not initialized")
			}
			return admin.BuildDiagnosticsReport(), nil
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "ops_repair",
		Description: "执行平台诊断修复动作（rebind_proxies / renew_expired / topup_free_uuid / disable_banned / delete_banned / smart_heal）。支持 dry_run 预览，实际执行必须显式传入 confirm=true",
		Scope:       mcp.ScopeOps,
		ReadOnly:    false,
		Annotations: &mcp.ToolAnnotations{Title: "Execute Diagnostic Repair", ReadOnlyHint: mcp.BoolPtr(false), DestructiveHint: mcp.BoolPtr(true)},
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"action"},
			"properties": map[string]interface{}{
				"action":   map[string]interface{}{"type": "string", "enum": []string{"rebind_proxies", "renew_expired", "topup_free_uuid", "disable_banned", "delete_banned", "smart_heal"}},
				"provider": map[string]interface{}{"type": "string", "default": "chatgpt"},
				"count":    map[string]interface{}{"type": "integer", "default": 2},
				"dry_run":  map[string]interface{}{"type": "boolean", "description": "为 true 时仅返回当前诊断报告与待修复评估，不产生写副作用"},
				"confirm":  map[string]interface{}{"type": "boolean", "description": "执行写/修复动作时必须显式传入 true"},
			},
		},
		Handler: func(ctx context.Context, _ mcp.CallContext, args json.RawMessage) (interface{}, error) {
			if admin == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "admin handler not initialized")
			}
			var p struct {
				Action   string `json:"action"`
				Provider string `json:"provider"`
				Count    int    `json:"count"`
				DryRun   bool   `json:"dry_run"`
				Confirm  bool   `json:"confirm"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "%v", err)
			}
			if strings.TrimSpace(p.Action) == "" {
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "action is required")
			}
			if p.DryRun {
				report := admin.BuildDiagnosticsReport()
				return map[string]interface{}{
					"dry_run":        true,
					"planned_action": p.Action,
					"health_score":   report.HealthScore,
					"severity":       report.OverallSeverity,
					"repair_options": report.RepairOptions,
				}, nil
			}
			if err := mcp.RequireConfirm(p.Confirm, "ops_repair:"+p.Action); err != nil {
				return nil, err
			}
			return invokeGinJSON(ctx, admin.ExecuteDiagnosticRepair, http.MethodPost, "/admin/api/v1/diagnostics/repair", nil, nil, map[string]interface{}{
				"action":   p.Action,
				"provider": p.Provider,
				"count":    p.Count,
			})
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "ops_query_logs",
		Description: "查询网关请求审计日志（request）或后台运维事件日志（ops），支持级别、模块与数量过滤",
		Scope:       mcp.ScopeOps,
		ReadOnly:    true,
		Annotations: &mcp.ToolAnnotations{Title: "Query Logs", ReadOnlyHint: mcp.BoolPtr(true), DestructiveHint: mcp.BoolPtr(false), IdempotentHint: mcp.BoolPtr(true)},
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"kind":   map[string]interface{}{"type": "string", "enum": []string{"ops", "request"}, "default": "ops"},
				"level":  map[string]interface{}{"type": "string"},
				"module": map[string]interface{}{"type": "string"},
				"limit":  map[string]interface{}{"type": "integer", "default": 50},
			},
		},
		Handler: func(ctx context.Context, _ mcp.CallContext, args json.RawMessage) (interface{}, error) {
			if admin == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "admin handler not initialized")
			}
			var p struct {
				Kind   string `json:"kind"`
				Level  string `json:"level"`
				Module string `json:"module"`
				Limit  int    `json:"limit"`
			}
			_ = json.Unmarshal(args, &p)
			q := url.Values{}
			if p.Limit > 0 {
				q.Set("limit", fmt.Sprintf("%d", p.Limit))
			}
			if p.Level != "" {
				q.Set("level", p.Level)
			}
			if p.Module != "" {
				q.Set("module", p.Module)
			}
			if strings.EqualFold(p.Kind, "request") {
				return invokeGinJSON(ctx, admin.ListLogs, http.MethodGet, "/admin/api/v1/logs", nil, q, nil)
			}
			return invokeGinJSON(ctx, admin.ListOpsLogs, http.MethodGet, "/admin/api/v1/ops-logs", nil, q, nil)
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "ops_manage_proxies",
		Description: "管理代理池：list（查看节点）、probe（探测连通性）、update（全量更新代理列表，需 confirm=true）",
		Scope:       mcp.ScopeOps,
		ReadOnly:    false,
		Annotations: &mcp.ToolAnnotations{Title: "Manage Proxies", ReadOnlyHint: mcp.BoolPtr(false), DestructiveHint: mcp.BoolPtr(true)},
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"action"},
			"properties": map[string]interface{}{
				"action":  map[string]interface{}{"type": "string", "enum": []string{"list", "probe", "update"}},
				"proxies": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"confirm": map[string]interface{}{"type": "boolean"},
			},
		},
		Handler: func(ctx context.Context, _ mcp.CallContext, args json.RawMessage) (interface{}, error) {
			if admin == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "admin handler not initialized")
			}
			var p struct {
				Action  string   `json:"action"`
				Proxies []string `json:"proxies"`
				Confirm bool     `json:"confirm"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "%v", err)
			}
			switch p.Action {
			case "list":
				return invokeGinJSON(ctx, admin.ListProxies, http.MethodGet, "/admin/api/v1/proxies", nil, nil, nil)
			case "probe":
				return invokeGinJSON(ctx, admin.ProbeProxies, http.MethodPost, "/admin/api/v1/proxies/probe", nil, nil, map[string]interface{}{})
			case "update":
				if err := mcp.RequireConfirm(p.Confirm, "ops_manage_proxies:update"); err != nil {
					return nil, err
				}
				return invokeGinJSON(ctx, admin.UpdateProxies, http.MethodPost, "/admin/api/v1/proxies", nil, nil, map[string]interface{}{"proxies": p.Proxies})
			default:
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "unknown action: %s", p.Action)
			}
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "ops_manage_accounts",
		Description: "管理供应商号池（零明文凭据）：list / stats / probe_all / account_action / kami_login / kami_task_status。高危操作（delete/disable/kami_login）需 confirm=true",
		Scope:       mcp.ScopeOps,
		ReadOnly:    false,
		Annotations: &mcp.ToolAnnotations{Title: "Manage Provider Accounts", ReadOnlyHint: mcp.BoolPtr(false), DestructiveHint: mcp.BoolPtr(true)},
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"action"},
			"properties": map[string]interface{}{
				"action":         map[string]interface{}{"type": "string", "enum": []string{"list", "stats", "probe_all", "account_action", "kami_login", "kami_task_status"}},
				"provider":       map[string]interface{}{"type": "string", "default": "chatgpt"},
				"account_id":     map[string]interface{}{"type": "string"},
				"account_action": map[string]interface{}{"type": "string", "description": "用于 action=account_action，如 enable/disable/delete/refresh/probe"},
				"task_id":        map[string]interface{}{"type": "string"},
				"accounts_text":  map[string]interface{}{"type": "string"},
				"confirm":        map[string]interface{}{"type": "boolean"},
			},
		},
		Handler: func(ctx context.Context, _ mcp.CallContext, args json.RawMessage) (interface{}, error) {
			if admin == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "admin handler not initialized")
			}
			var p struct {
				Action        string `json:"action"`
				Provider      string `json:"provider"`
				AccountID     string `json:"account_id"`
				AccountAction string `json:"account_action"`
				TaskID        string `json:"task_id"`
				AccountsText  string `json:"accounts_text"`
				Confirm       bool   `json:"confirm"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "%v", err)
			}
			if strings.TrimSpace(p.Provider) == "" {
				p.Provider = "chatgpt"
			}
			params := gin.Params{{Key: "provider", Value: p.Provider}}
			switch p.Action {
			case "list":
				return invokeGinJSON(ctx, admin.ListAccounts, http.MethodGet, "/admin/api/v1/providers/"+p.Provider+"/accounts", params, nil, nil)
			case "stats":
				return invokeGinJSON(ctx, admin.ProviderStats, http.MethodGet, "/admin/api/v1/providers/"+p.Provider+"/stats", params, nil, nil)
			case "probe_all":
				return invokeGinJSON(ctx, admin.ProbeAllAccounts, http.MethodPost, "/admin/api/v1/providers/"+p.Provider+"/pool/probe-all", params, nil, map[string]interface{}{})
			case "account_action":
				if p.AccountID == "" || p.AccountAction == "" {
					return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "account_id and account_action are required")
				}
				actLower := strings.ToLower(strings.TrimSpace(p.AccountAction))
				if actLower == "delete" || actLower == "disable" || actLower == "remove" {
					if err := mcp.RequireConfirm(p.Confirm, "account_action:"+actLower); err != nil {
						return nil, err
					}
				}
				actParams := gin.Params{{Key: "provider", Value: p.Provider}, {Key: "id", Value: p.AccountID}}
				return invokeGinJSON(ctx, admin.AccountAction, http.MethodPost, "/admin/api/v1/providers/"+p.Provider+"/accounts/"+p.AccountID+"/action", actParams, nil, map[string]interface{}{"action": p.AccountAction})
			case "kami_login":
				if err := mcp.RequireConfirm(p.Confirm, "kami_login"); err != nil {
					return nil, err
				}
				return invokeGinJSON(ctx, admin.LoginKamiAccounts, http.MethodPost, "/admin/api/v1/providers/"+p.Provider+"/accounts/kami-login", params, nil, map[string]interface{}{"accounts_text": p.AccountsText})
			case "kami_task_status":
				if p.TaskID == "" {
					return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "task_id is required")
				}
				tParams := gin.Params{{Key: "provider", Value: p.Provider}, {Key: "taskID", Value: p.TaskID}}
				return invokeGinJSON(ctx, admin.KamiLoginTask, http.MethodGet, "/admin/api/v1/providers/"+p.Provider+"/accounts/kami-login/tasks/"+p.TaskID, tParams, nil, nil)
			default:
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "unknown action: %s", p.Action)
			}
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "ops_list_models",
		Description: "获取平台可用模型目录与多供应商通道列表",
		Scope:       mcp.ScopeOps,
		ReadOnly:    true,
		Annotations: &mcp.ToolAnnotations{Title: "List Models and Channels", ReadOnlyHint: mcp.BoolPtr(true), DestructiveHint: mcp.BoolPtr(false), IdempotentHint: mcp.BoolPtr(true)},
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Handler: func(ctx context.Context, _ mcp.CallContext, _ json.RawMessage) (interface{}, error) {
			if admin == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "admin handler not initialized")
			}
			models, err := invokeGinJSON(ctx, admin.ListModelsCatalog, http.MethodGet, "/admin/api/v1/models", nil, nil, nil)
			if err != nil {
				return nil, err
			}
			channels, _ := invokeGinJSON(ctx, admin.ListChannels, http.MethodGet, "/admin/api/v1/providers/channels", nil, nil, nil)
			return map[string]interface{}{
				"models":   models,
				"channels": channels,
			}, nil
		},
	})

	// 运维只读 Resources（双模读取兼容）
	r.RegisterResource(mcp.ResourceDef{
		URI:         "ai-web-free://ops/overview",
		Name:        "Platform Overview",
		Description: "平台概览与系统运行状态",
		Scope:       mcp.ScopeOps,
		Handler: func(ctx context.Context, callCtx mcp.CallContext) (interface{}, string, error) {
			res, err := r.CallTool(ctx, callCtx, "ops_get_overview", json.RawMessage(`{}`))
			return res, "application/json", err
		},
	})
	r.RegisterResource(mcp.ResourceDef{
		URI:         "ai-web-free://ops/diagnostics",
		Name:        "Platform Diagnostics",
		Description: "当前平台健康诊断报告",
		Scope:       mcp.ScopeOps,
		Handler: func(ctx context.Context, callCtx mcp.CallContext) (interface{}, string, error) {
			res, err := r.CallTool(ctx, callCtx, "ops_diagnostics", json.RawMessage(`{}`))
			return res, "application/json", err
		},
	})
	r.RegisterResource(mcp.ResourceDef{
		URI:         "ai-web-free://ops/logs",
		Name:        "Recent Ops Logs",
		Description: "最近 50 条运维事件日志",
		Scope:       mcp.ScopeOps,
		Handler: func(ctx context.Context, callCtx mcp.CallContext) (interface{}, string, error) {
			res, err := r.CallTool(ctx, callCtx, "ops_query_logs", json.RawMessage(`{"kind":"ops","limit":50}`))
			return res, "application/json", err
		},
	})
	r.RegisterResource(mcp.ResourceDef{
		URI:         "ai-web-free://ops/models",
		Name:        "Models Catalog",
		Description: "平台可用模型目录与通道",
		Scope:       mcp.ScopeOps,
		Handler: func(ctx context.Context, callCtx mcp.CallContext) (interface{}, string, error) {
			res, err := r.CallTool(ctx, callCtx, "ops_list_models", json.RawMessage(`{}`))
			return res, "application/json", err
		},
	})
}

// ─── 2. 短剧编排域 (drama) ───────────────────────────────────────────────────────

func (h *MCPHandler) registerDramaTools() {
	r := h.registry
	dh := h.deps.Drama

	r.RegisterTool(mcp.ToolDef{
		Name:        "drama_capabilities",
		Description: "获取短剧工坊支持的模型、配音、画幅与节点能力矩阵",
		Scope:       mcp.ScopeDrama,
		ReadOnly:    true,
		Annotations: &mcp.ToolAnnotations{Title: "Drama Capabilities", ReadOnlyHint: mcp.BoolPtr(true), DestructiveHint: mcp.BoolPtr(false), IdempotentHint: mcp.BoolPtr(true)},
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Handler: func(ctx context.Context, _ mcp.CallContext, _ json.RawMessage) (interface{}, error) {
			if dh == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "drama handler not initialized")
			}
			return invokeGinJSON(ctx, dh.Capabilities, http.MethodGet, "/admin/api/v1/drama/capabilities", nil, nil, nil)
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "drama_list_projects",
		Description: "列出所有短剧工程摘要（ID、标题、题材、分镜数）",
		Scope:       mcp.ScopeDrama,
		ReadOnly:    true,
		Annotations: &mcp.ToolAnnotations{Title: "List Drama Projects", ReadOnlyHint: mcp.BoolPtr(true), DestructiveHint: mcp.BoolPtr(false), IdempotentHint: mcp.BoolPtr(true)},
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Handler: func(_ context.Context, _ mcp.CallContext, _ json.RawMessage) (interface{}, error) {
			if dh == nil || dh.db == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "drama engine not initialized")
			}
			var records []db.DramaProjectRecord
			_ = dh.db.Order("updated_at desc").Limit(50).Find(&records).Error
			out := make([]map[string]interface{}, 0, len(records))
			for _, rec := range records {
				out = append(out, map[string]interface{}{
					"id":         rec.ID,
					"title":      rec.Title,
					"genre":      rec.Genre,
					"revision":   rec.Revision,
					"shot_count": rec.ShotCount,
					"status":     rec.Status,
				})
			}
			return out, nil
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "drama_get_project",
		Description: "获取指定短剧工程的结构化 DramaProject JSON（含 revision、characters、scenes、shots、nodes、timeline）",
		Scope:       mcp.ScopeDrama,
		ReadOnly:    true,
		Annotations: &mcp.ToolAnnotations{Title: "Get Drama Project", ReadOnlyHint: mcp.BoolPtr(true), DestructiveHint: mcp.BoolPtr(false), IdempotentHint: mcp.BoolPtr(true)},
		InputSchema: map[string]interface{}{
			"type":       "object",
			"required":   []string{"project_id"},
			"properties": map[string]interface{}{"project_id": map[string]interface{}{"type": "string"}},
		},
		Handler: func(_ context.Context, _ mcp.CallContext, args json.RawMessage) (interface{}, error) {
			if dh == nil || dh.engine == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "drama engine not initialized")
			}
			var p struct {
				ProjectID string `json:"project_id"`
			}
			_ = json.Unmarshal(args, &p)
			proj, _, _, err := dh.engine.LoadOrMigrateProject(strings.TrimSpace(p.ProjectID))
			if err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeToolError, "%v", err)
			}
			return proj, nil
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "drama_patch_project",
		Description: "使用 RFC 6902 JSON Patch 修改短剧工程并触发下游 stale 传播（需提供 revision 乐观锁）",
		Scope:       mcp.ScopeDrama,
		ReadOnly:    false,
		Annotations: &mcp.ToolAnnotations{Title: "Patch Drama Project", ReadOnlyHint: mcp.BoolPtr(false), DestructiveHint: mcp.BoolPtr(false)},
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"project_id", "revision", "ops"},
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{"type": "string"},
				"revision":   map[string]interface{}{"type": "integer"},
				"ops":        map[string]interface{}{"type": "array"},
			},
		},
		Handler: func(_ context.Context, _ mcp.CallContext, args json.RawMessage) (interface{}, error) {
			if dh == nil || dh.engine == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "drama engine not initialized")
			}
			var p struct {
				ProjectID string                 `json:"project_id"`
				Revision  int64                  `json:"revision"`
				Ops       []drama.PatchOperation `json:"ops"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "%v", err)
			}
			proj, layout, _, err := dh.engine.LoadOrMigrateProject(strings.TrimSpace(p.ProjectID))
			if err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeToolError, "%v", err)
			}
			if p.Revision > 0 && p.Revision != proj.Revision {
				return nil, mcp.ErrorWithCode(mcp.CodeGuardrail, "revision conflict")
			}
			updated, err := drama.ApplyJSONPatch(proj, p.Ops)
			if err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "%v", err)
			}
			if err := dh.engine.SaveProjectState(p.ProjectID, updated, layout); err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "%v", err)
			}
			return updated, nil
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "drama_run",
		Description: "异步触发后台 DAG 引擎执行指定节点或全流水线，立即返回 run_id 句柄",
		Scope:       mcp.ScopeDrama,
		ReadOnly:    false,
		Annotations: &mcp.ToolAnnotations{Title: "Run Drama Pipeline", ReadOnlyHint: mcp.BoolPtr(false), DestructiveHint: mcp.BoolPtr(false)},
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"project_id"},
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{"type": "string"},
				"targets":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"force":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"until":      map[string]interface{}{"type": "string"},
			},
		},
		Handler: func(ctx context.Context, _ mcp.CallContext, args json.RawMessage) (interface{}, error) {
			if dh == nil || dh.engine == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "drama engine not initialized")
			}
			var p struct {
				ProjectID string   `json:"project_id"`
				Targets   []string `json:"targets"`
				Force     []string `json:"force"`
				Until     string   `json:"until"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "%v", err)
			}
			runRec, dup, err := dh.engine.StartRun(ctx, strings.TrimSpace(p.ProjectID), drama.RunOptions{
				Targets: p.Targets,
				Force:   p.Force,
				Until:   p.Until,
			})
			if err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeToolError, "%v", err)
			}
			return map[string]interface{}{
				"run":             runRec,
				"already_running": dup,
			}, nil
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "drama_get_run",
		Description: "查询短剧 DAG 后台运行任务的进度与状态",
		Scope:       mcp.ScopeDrama,
		ReadOnly:    true,
		Annotations: &mcp.ToolAnnotations{Title: "Get Drama Run Status", ReadOnlyHint: mcp.BoolPtr(true), DestructiveHint: mcp.BoolPtr(false), IdempotentHint: mcp.BoolPtr(true)},
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"project_id", "run_id"},
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{"type": "string"},
				"run_id":     map[string]interface{}{"type": "string"},
			},
		},
		Handler: func(ctx context.Context, _ mcp.CallContext, args json.RawMessage) (interface{}, error) {
			if dh == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "drama handler not initialized")
			}
			var p struct {
				ProjectID string `json:"project_id"`
				RunID     string `json:"run_id"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "%v", err)
			}
			params := gin.Params{{Key: "id", Value: p.ProjectID}, {Key: "rid", Value: p.RunID}}
			return invokeGinJSON(ctx, dh.GetRun, http.MethodGet, "/admin/api/v1/drama/projects/"+p.ProjectID+"/runs/"+p.RunID, params, nil, nil)
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "drama_lock_node",
		Description: "锁定或解锁指定产物节点（锁定后不会被重跑或上游改动覆盖）",
		Scope:       mcp.ScopeDrama,
		ReadOnly:    false,
		Annotations: &mcp.ToolAnnotations{Title: "Lock Drama Node", ReadOnlyHint: mcp.BoolPtr(false), DestructiveHint: mcp.BoolPtr(false)},
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"project_id", "node_key", "locked"},
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{"type": "string"},
				"node_key":   map[string]interface{}{"type": "string"},
				"locked":     map[string]interface{}{"type": "boolean"},
			},
		},
		Handler: func(_ context.Context, _ mcp.CallContext, args json.RawMessage) (interface{}, error) {
			if dh == nil || dh.engine == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "drama engine not initialized")
			}
			var p struct {
				ProjectID string `json:"project_id"`
				NodeKey   string `json:"node_key"`
				Locked    bool   `json:"locked"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "%v", err)
			}
			proj, err := dh.engine.SetNodeLocked(strings.TrimSpace(p.ProjectID), strings.TrimSpace(p.NodeKey), p.Locked)
			if err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeToolError, "%v", err)
			}
			return map[string]interface{}{
				"revision": proj.Revision,
				"node_key": p.NodeKey,
				"locked":   p.Locked,
			}, nil
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "drama_delete_project",
		Description: "删除指定短剧工程（破坏性操作，必须显式传入 confirm=true）",
		Scope:       mcp.ScopeDrama,
		ReadOnly:    false,
		Annotations: &mcp.ToolAnnotations{Title: "Delete Drama Project", ReadOnlyHint: mcp.BoolPtr(false), DestructiveHint: mcp.BoolPtr(true)},
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"project_id", "confirm"},
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{"type": "string"},
				"confirm":    map[string]interface{}{"type": "boolean"},
			},
		},
		Handler: func(ctx context.Context, _ mcp.CallContext, args json.RawMessage) (interface{}, error) {
			if dh == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "drama handler not initialized")
			}
			var p struct {
				ProjectID string `json:"project_id"`
				Confirm   bool   `json:"confirm"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "%v", err)
			}
			if err := mcp.RequireConfirm(p.Confirm, "drama_delete_project:"+p.ProjectID); err != nil {
				return nil, err
			}
			params := gin.Params{{Key: "id", Value: p.ProjectID}}
			return invokeGinJSON(ctx, dh.DeleteProject, http.MethodDelete, "/admin/api/v1/drama/projects/"+p.ProjectID, params, nil, nil)
		},
	})

	r.RegisterResource(mcp.ResourceDef{
		URI:         "ai-web-free://drama/capabilities",
		Name:        "Drama Capabilities",
		Description: "短剧工坊能力矩阵",
		Scope:       mcp.ScopeDrama,
		Handler: func(ctx context.Context, callCtx mcp.CallContext) (interface{}, string, error) {
			res, err := r.CallTool(ctx, callCtx, "drama_capabilities", json.RawMessage(`{}`))
			return res, "application/json", err
		},
	})
	r.RegisterResource(mcp.ResourceDef{
		URI:         "ai-web-free://drama/projects",
		Name:        "Drama Projects",
		Description: "短剧工程列表",
		Scope:       mcp.ScopeDrama,
		Handler: func(ctx context.Context, callCtx mcp.CallContext) (interface{}, string, error) {
			res, err := r.CallTool(ctx, callCtx, "drama_list_projects", json.RawMessage(`{}`))
			return res, "application/json", err
		},
	})
}

// ─── 3. 产品创作与工坊域 (studio) ───────────────────────────────────────────────

func (h *MCPHandler) registerStudioTools() {
	r := h.registry
	admin := h.deps.Admin
	gallery := h.deps.Gallery
	media := h.deps.MediaLibrary
	video := h.deps.Video
	audio := h.deps.Audio
	chatWB := h.deps.ChatWork

	r.RegisterTool(mcp.ToolDef{
		Name:        "studio_gallery",
		Description: "图像画廊管理：list / favorite / update_metadata / delete / clear（delete 与 clear 需 confirm=true）",
		Scope:       mcp.ScopeStudio,
		ReadOnly:    false,
		Annotations: &mcp.ToolAnnotations{Title: "Gallery Works", ReadOnlyHint: mcp.BoolPtr(false), DestructiveHint: mcp.BoolPtr(true)},
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"action"},
			"properties": map[string]interface{}{
				"action":   map[string]interface{}{"type": "string", "enum": []string{"list", "favorite", "update_metadata", "delete", "clear"}},
				"id":       map[string]interface{}{"type": "string"},
				"query":    map[string]interface{}{"type": "string"},
				"limit":    map[string]interface{}{"type": "integer", "default": 20},
				"category": map[string]interface{}{"type": "string"},
				"tags":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"confirm":  map[string]interface{}{"type": "boolean"},
			},
		},
		Handler: func(ctx context.Context, _ mcp.CallContext, args json.RawMessage) (interface{}, error) {
			if gallery == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "gallery handler not initialized")
			}
			var p struct {
				Action   string   `json:"action"`
				ID       string   `json:"id"`
				Query    string   `json:"query"`
				Limit    int      `json:"limit"`
				Category string   `json:"category"`
				Tags     []string `json:"tags"`
				Confirm  bool     `json:"confirm"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "%v", err)
			}
			switch p.Action {
			case "list":
				if strings.TrimSpace(p.Query) != "" {
					limit := p.Limit
					if limit <= 0 {
						limit = 20
					}
					return map[string]interface{}{"works": gallery.SearchWorks(p.Query, limit)}, nil
				}
				return invokeGinJSON(ctx, gallery.ListWorks, http.MethodGet, "/admin/api/v1/gallery", nil, nil, nil)
			case "favorite":
				if p.ID == "" {
					return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "id is required")
				}
				return invokeGinJSON(ctx, gallery.ToggleFavorite, http.MethodPut, "/admin/api/v1/gallery/"+p.ID+"/favorite", gin.Params{{Key: "id", Value: p.ID}}, nil, map[string]interface{}{})
			case "update_metadata":
				if p.ID == "" {
					return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "id is required")
				}
				return invokeGinJSON(ctx, gallery.UpdateMetadata, http.MethodPut, "/admin/api/v1/gallery/"+p.ID+"/metadata", gin.Params{{Key: "id", Value: p.ID}}, nil, map[string]interface{}{"category": p.Category, "tags": p.Tags})
			case "delete":
				if p.ID == "" {
					return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "id is required")
				}
				if err := mcp.RequireConfirm(p.Confirm, "studio_gallery:delete:"+p.ID); err != nil {
					return nil, err
				}
				return invokeGinJSON(ctx, gallery.DeleteWork, http.MethodDelete, "/admin/api/v1/gallery/"+p.ID, gin.Params{{Key: "id", Value: p.ID}}, nil, nil)
			case "clear":
				if err := mcp.RequireConfirm(p.Confirm, "studio_gallery:clear"); err != nil {
					return nil, err
				}
				return invokeGinJSON(ctx, gallery.ClearWorks, http.MethodDelete, "/admin/api/v1/gallery", nil, nil, nil)
			default:
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "unknown action: %s", p.Action)
			}
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "studio_prompts",
		Description: "提示词广场与智能扩写：list / expand / video_expand / favorites / toggle_favorite",
		Scope:       mcp.ScopeStudio,
		ReadOnly:    false,
		Annotations: &mcp.ToolAnnotations{Title: "Prompt Plaza", ReadOnlyHint: mcp.BoolPtr(false), DestructiveHint: mcp.BoolPtr(false)},
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"action"},
			"properties": map[string]interface{}{
				"action":    map[string]interface{}{"type": "string", "enum": []string{"list", "expand", "video_expand", "favorites", "toggle_favorite"}},
				"prompt":    map[string]interface{}{"type": "string"},
				"style":     map[string]interface{}{"type": "string"},
				"prompt_id": map[string]interface{}{"type": "string"},
				"query":     map[string]interface{}{"type": "string"},
			},
		},
		Handler: func(ctx context.Context, _ mcp.CallContext, args json.RawMessage) (interface{}, error) {
			var p struct {
				Action   string `json:"action"`
				Prompt   string `json:"prompt"`
				Style    string `json:"style"`
				PromptID string `json:"prompt_id"`
				Query    string `json:"query"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "%v", err)
			}
			switch p.Action {
			case "list":
				if admin == nil {
					return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "admin handler not initialized")
				}
				q := url.Values{}
				if p.Query != "" {
					q.Set("q", p.Query)
				}
				return invokeGinJSON(ctx, admin.ListPrompts, http.MethodGet, "/admin/api/v1/prompts", nil, q, nil)
			case "expand":
				if admin == nil {
					return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "admin handler not initialized")
				}
				return invokeGinJSON(ctx, admin.ExpandPrompt, http.MethodPost, "/admin/api/v1/prompts/expand", nil, nil, map[string]interface{}{"prompt": p.Prompt, "style": p.Style})
			case "video_expand":
				if admin == nil {
					return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "admin handler not initialized")
				}
				return invokeGinJSON(ctx, admin.ExpandVideoPrompt, http.MethodPost, "/admin/api/v1/prompts/video-expand", nil, nil, map[string]interface{}{"prompt": p.Prompt, "style": p.Style})
			case "favorites":
				if gallery == nil {
					return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "gallery handler not initialized")
				}
				return invokeGinJSON(ctx, gallery.ListPromptFavorites, http.MethodGet, "/admin/api/v1/prompts/favorites", nil, nil, nil)
			case "toggle_favorite":
				if gallery == nil {
					return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "gallery handler not initialized")
				}
				return invokeGinJSON(ctx, gallery.TogglePromptFavorite, http.MethodPost, "/admin/api/v1/prompts/favorites/toggle", nil, nil, map[string]interface{}{"prompt_id": p.PromptID})
			default:
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "unknown action: %s", p.Action)
			}
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "studio_media_assets",
		Description: "跨工坊系统媒体资产库与网页媒体爬虫：list / crawl / import",
		Scope:       mcp.ScopeStudio,
		ReadOnly:    false,
		Annotations: &mcp.ToolAnnotations{Title: "Media Assets & Crawler", ReadOnlyHint: mcp.BoolPtr(false), DestructiveHint: mcp.BoolPtr(false)},
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"action"},
			"properties": map[string]interface{}{
				"action": map[string]interface{}{"type": "string", "enum": []string{"list", "crawl", "import"}},
				"query":  map[string]interface{}{"type": "string"},
				"kind":   map[string]interface{}{"type": "string", "enum": []string{"all", "image", "audio", "video"}},
				"url":    map[string]interface{}{"type": "string"},
				"title":  map[string]interface{}{"type": "string"},
				"limit":  map[string]interface{}{"type": "integer", "default": 20},
			},
		},
		Handler: func(ctx context.Context, _ mcp.CallContext, args json.RawMessage) (interface{}, error) {
			if media == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "media library handler not initialized")
			}
			var p struct {
				Action string `json:"action"`
				Query  string `json:"query"`
				Kind   string `json:"kind"`
				URL    string `json:"url"`
				Title  string `json:"title"`
				Limit  int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "%v", err)
			}
			switch p.Action {
			case "list":
				q := url.Values{}
				if p.Query != "" {
					q.Set("q", p.Query)
				}
				if p.Kind != "" {
					q.Set("kind", p.Kind)
				}
				if p.Limit > 0 {
					q.Set("limit", fmt.Sprintf("%d", p.Limit))
				}
				return invokeGinJSON(ctx, media.ListSystemAssets, http.MethodGet, "/admin/api/v1/media/assets", nil, q, nil)
			case "crawl":
				return invokeGinJSON(ctx, media.CrawlMedia, http.MethodPost, "/admin/api/v1/media/crawl", nil, nil, map[string]interface{}{"query": p.Query, "kind": p.Kind, "limit": p.Limit})
			case "import":
				return invokeGinJSON(ctx, media.ImportCrawledMedia, http.MethodPost, "/admin/api/v1/media/crawl-import", nil, nil, map[string]interface{}{"url": p.URL, "kind": p.Kind, "title": p.Title})
			default:
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "unknown action: %s", p.Action)
			}
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "studio_video_tasks",
		Description: "多模态视频生成与任务管理：generate（异步返回 task_id）/ get_task / list / rescue / delete / clear（delete 与 clear 需 confirm=true）",
		Scope:       mcp.ScopeStudio,
		ReadOnly:    false,
		Annotations: &mcp.ToolAnnotations{Title: "Video Generation & Tasks", ReadOnlyHint: mcp.BoolPtr(false), DestructiveHint: mcp.BoolPtr(true)},
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"action"},
			"properties": map[string]interface{}{
				"action":  map[string]interface{}{"type": "string", "enum": []string{"generate", "get_task", "list", "rescue", "delete", "clear"}},
				"task_id": map[string]interface{}{"type": "string"},
				"prompt":  map[string]interface{}{"type": "string"},
				"model":   map[string]interface{}{"type": "string"},
				"query":   map[string]interface{}{"type": "string"},
				"limit":   map[string]interface{}{"type": "integer", "default": 20},
				"confirm": map[string]interface{}{"type": "boolean"},
			},
		},
		Handler: func(ctx context.Context, _ mcp.CallContext, args json.RawMessage) (interface{}, error) {
			if video == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "video handler not initialized")
			}
			var p struct {
				Action  string `json:"action"`
				TaskID  string `json:"task_id"`
				Prompt  string `json:"prompt"`
				Model   string `json:"model"`
				Query   string `json:"query"`
				Limit   int    `json:"limit"`
				Confirm bool   `json:"confirm"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "%v", err)
			}
			switch p.Action {
			case "generate":
				if strings.TrimSpace(p.Prompt) == "" {
					return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "prompt is required")
				}
				task, err := video.SubmitVideoTaskInternal(VideoGenerationRequest{
					Prompt: p.Prompt,
					Model:  p.Model,
				})
				if err != nil {
					return nil, mcp.ErrorWithCode(mcp.CodeToolError, "%v", err)
				}
				return task, nil
			case "get_task":
				if p.TaskID == "" {
					return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "task_id is required")
				}
				task, ok := video.GetVideoTaskInternal(p.TaskID)
				if !ok {
					return nil, mcp.ErrorWithCode(mcp.CodeToolError, "video task not found: %s", p.TaskID)
				}
				return task, nil
			case "list":
				limit := p.Limit
				if limit <= 0 {
					limit = 20
				}
				return map[string]interface{}{"tasks": video.ListTasksSnapshot(p.Query, limit)}, nil
			case "rescue":
				if p.TaskID == "" {
					return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "task_id is required")
				}
				return invokeGinJSON(ctx, video.RescueTask, http.MethodPost, "/admin/api/v1/multimodal/video-tasks/"+p.TaskID+"/rescue", gin.Params{{Key: "id", Value: p.TaskID}}, nil, map[string]interface{}{})
			case "delete":
				if p.TaskID == "" {
					return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "task_id is required")
				}
				if err := mcp.RequireConfirm(p.Confirm, "studio_video_tasks:delete:"+p.TaskID); err != nil {
					return nil, err
				}
				return invokeGinJSON(ctx, video.DeleteTask, http.MethodDelete, "/admin/api/v1/multimodal/video-tasks/"+p.TaskID, gin.Params{{Key: "id", Value: p.TaskID}}, nil, nil)
			case "clear":
				if err := mcp.RequireConfirm(p.Confirm, "studio_video_tasks:clear"); err != nil {
					return nil, err
				}
				return invokeGinJSON(ctx, video.ClearTasks, http.MethodDelete, "/admin/api/v1/multimodal/video-tasks", nil, nil, nil)
			default:
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "unknown action: %s", p.Action)
			}
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "studio_audio",
		Description: "音频与 TTS 工坊：list_voices（音色列表）/ history（历史记录）/ tts（语音合成，返回 base64 音频摘要）",
		Scope:       mcp.ScopeStudio,
		ReadOnly:    false,
		Annotations: &mcp.ToolAnnotations{Title: "Audio & TTS Studio", ReadOnlyHint: mcp.BoolPtr(false), DestructiveHint: mcp.BoolPtr(false)},
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"action"},
			"properties": map[string]interface{}{
				"action": map[string]interface{}{"type": "string", "enum": []string{"list_voices", "history", "tts"}},
				"input":  map[string]interface{}{"type": "string"},
				"voice":  map[string]interface{}{"type": "string"},
				"model":  map[string]interface{}{"type": "string"},
				"speed":  map[string]interface{}{"type": "number"},
			},
		},
		Handler: func(ctx context.Context, _ mcp.CallContext, args json.RawMessage) (interface{}, error) {
			if audio == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "audio handler not initialized")
			}
			var p struct {
				Action string  `json:"action"`
				Input  string  `json:"input"`
				Voice  string  `json:"voice"`
				Model  string  `json:"model"`
				Speed  float64 `json:"speed"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "%v", err)
			}
			switch p.Action {
			case "list_voices":
				return invokeGinJSON(ctx, audio.ListVoices, http.MethodGet, "/v1/audio/voices", nil, nil, nil)
			case "history":
				return invokeGinJSON(ctx, audio.ListAudioHistory, http.MethodGet, "/admin/api/v1/multimodal/audio-history", nil, nil, nil)
			case "tts":
				if strings.TrimSpace(p.Input) == "" {
					return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "input is required")
				}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				payload, _ := json.Marshal(map[string]interface{}{
					"input": p.Input,
					"voice": p.Voice,
					"model": p.Model,
					"speed": p.Speed,
				})
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/v1/audio/speech", bytes.NewReader(payload))
				if err != nil {
					return nil, err
				}
				req.Header.Set("Content-Type", "application/json")
				c.Request = req
				audio.TTS(c)
				if rec.Code >= http.StatusBadRequest {
					return nil, mcp.ErrorWithCode(mcp.CodeToolError, "tts failed (%d): %s", rec.Code, rec.Body.String())
				}
				raw := rec.Body.Bytes()
				return map[string]interface{}{
					"content_type": rec.Header().Get("Content-Type"),
					"bytes":        len(raw),
					"audio_base64": base64.StdEncoding.EncodeToString(raw),
				}, nil
			default:
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "unknown action: %s", p.Action)
			}
		},
	})

	r.RegisterTool(mcp.ToolDef{
		Name:        "studio_chat_workbench",
		Description: "对话工作台与知识检索：list_conversations / get_conversation / search / list_presets / save_preset",
		Scope:       mcp.ScopeStudio,
		ReadOnly:    false,
		Annotations: &mcp.ToolAnnotations{Title: "Chat Workbench", ReadOnlyHint: mcp.BoolPtr(false), DestructiveHint: mcp.BoolPtr(false)},
		InputSchema: map[string]interface{}{
			"type":     "object",
			"required": []string{"action"},
			"properties": map[string]interface{}{
				"action":          map[string]interface{}{"type": "string", "enum": []string{"list_conversations", "get_conversation", "search", "list_presets", "save_preset"}},
				"conversation_id": map[string]interface{}{"type": "string"},
				"query":           map[string]interface{}{"type": "string"},
				"preset":          map[string]interface{}{"type": "object"},
			},
		},
		Handler: func(ctx context.Context, _ mcp.CallContext, args json.RawMessage) (interface{}, error) {
			if chatWB == nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInternalError, "chat workbench handler not initialized")
			}
			var p struct {
				Action         string                 `json:"action"`
				ConversationID string                 `json:"conversation_id"`
				Query          string                 `json:"query"`
				Preset         map[string]interface{} `json:"preset"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "%v", err)
			}
			switch p.Action {
			case "list_conversations":
				return invokeGinJSON(ctx, chatWB.ListConversations, http.MethodGet, "/admin/api/v1/chat/conversations", nil, nil, nil)
			case "get_conversation":
				if p.ConversationID == "" {
					return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "conversation_id is required")
				}
				return invokeGinJSON(ctx, chatWB.GetConversation, http.MethodGet, "/admin/api/v1/chat/conversations/"+p.ConversationID, gin.Params{{Key: "id", Value: p.ConversationID}}, nil, nil)
			case "search":
				q := url.Values{}
				q.Set("q", p.Query)
				return invokeGinJSON(ctx, chatWB.SearchChat, http.MethodGet, "/admin/api/v1/chat/search", nil, q, nil)
			case "list_presets":
				return invokeGinJSON(ctx, chatWB.ListPresets, http.MethodGet, "/admin/api/v1/chat/presets", nil, nil, nil)
			case "save_preset":
				return invokeGinJSON(ctx, chatWB.SavePreset, http.MethodPost, "/admin/api/v1/chat/presets", nil, nil, p.Preset)
			default:
				return nil, mcp.ErrorWithCode(mcp.CodeInvalidParams, "unknown action: %s", p.Action)
			}
		},
	})

	// 创作域只读 Resources
	r.RegisterResource(mcp.ResourceDef{
		URI:         "ai-web-free://studio/gallery",
		Name:        "Gallery Works",
		Description: "图像工坊作品列表",
		Scope:       mcp.ScopeStudio,
		Handler: func(ctx context.Context, callCtx mcp.CallContext) (interface{}, string, error) {
			res, err := r.CallTool(ctx, callCtx, "studio_gallery", json.RawMessage(`{"action":"list"}`))
			return res, "application/json", err
		},
	})
	r.RegisterResource(mcp.ResourceDef{
		URI:         "ai-web-free://studio/media-assets",
		Name:        "System Media Assets",
		Description: "跨工坊系统媒体资产库",
		Scope:       mcp.ScopeStudio,
		Handler: func(ctx context.Context, callCtx mcp.CallContext) (interface{}, string, error) {
			res, err := r.CallTool(ctx, callCtx, "studio_media_assets", json.RawMessage(`{"action":"list","limit":20}`))
			return res, "application/json", err
		},
	})
	r.RegisterResource(mcp.ResourceDef{
		URI:         "ai-web-free://studio/video-tasks",
		Name:        "Video Tasks",
		Description: "视频生成任务队列快照",
		Scope:       mcp.ScopeStudio,
		Handler: func(ctx context.Context, callCtx mcp.CallContext) (interface{}, string, error) {
			res, err := r.CallTool(ctx, callCtx, "studio_video_tasks", json.RawMessage(`{"action":"list","limit":20}`))
			return res, "application/json", err
		},
	})
}
