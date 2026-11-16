package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
)

// AuditRecorder 接收每次 MCP 调用的审计事件。
type AuditRecorder func(event AuditEvent)

// Registry 是内外 Agent 共用的统一 MCP 工具与资源注册表。
type Registry struct {
	mu            sync.RWMutex
	serverName    string
	serverVersion string
	tools         map[string]ToolDef
	resources     map[string]ResourceDef
	auditRecorder AuditRecorder
}

// NewRegistry 创建一个统一 MCP 注册表实例。
func NewRegistry(serverName, serverVersion string) *Registry {
	if strings.TrimSpace(serverName) == "" {
		serverName = "ai-web-free-mcp"
	}
	if strings.TrimSpace(serverVersion) == "" {
		serverVersion = "1.0.0"
	}
	return &Registry{
		serverName:    serverName,
		serverVersion: serverVersion,
		tools:         make(map[string]ToolDef),
		resources:     make(map[string]ResourceDef),
	}
}

// SetAuditRecorder 注册 MCP 调用审计回调。
func (r *Registry) SetAuditRecorder(fn AuditRecorder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.auditRecorder = fn
}

// RegisterTool 注册一个 MCP 工具（同名覆盖）。
func (r *Registry) RegisterTool(def ToolDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if def.Scope == "" {
		def.Scope = ScopeAll
	}
	if def.InputSchema == nil {
		def.InputSchema = map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}
	r.tools[def.Name] = def
}

// RegisterResource 注册一个 MCP Resource（同 URI 覆盖）。
func (r *Registry) RegisterResource(def ResourceDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if def.Scope == "" {
		def.Scope = ScopeAll
	}
	if def.MimeType == "" {
		def.MimeType = "application/json"
	}
	r.resources[def.URI] = def
}

func scopeMatches(itemScope, targetScope Scope) bool {
	if targetScope == "" || targetScope == ScopeAll {
		return true
	}
	return itemScope == targetScope || itemScope == ScopeAll
}

// ListTools 按 scope 与 mode 返回可见工具列表（按名称稳定排序）。
func (r *Registry) ListTools(scope Scope, mode Mode) []ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		if !scopeMatches(t.Scope, scope) {
			continue
		}
		if mode == ModeReadOnly && !t.ReadOnly {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

// ListResources 按 scope 返回可见资源列表（按 URI 稳定排序）。
func (r *Registry) ListResources(scope Scope) []ResourceDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]ResourceDef, 0, len(r.resources))
	for _, res := range r.resources {
		if !scopeMatches(res.Scope, scope) {
			continue
		}
		out = append(out, res)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].URI < out[j].URI
	})
	return out
}

// CallTool 在进程内直接执行指定 MCP 工具，供外部 MCP 端点与内部 Agent 共用。
func (r *Registry) CallTool(ctx context.Context, callCtx CallContext, name string, args json.RawMessage) (interface{}, error) {
	r.mu.RLock()
	def, ok := r.tools[strings.TrimSpace(name)]
	recorder := r.auditRecorder
	r.mu.RUnlock()

	if !ok {
		err := ErrorWithCode(CodeMethodNotFound, "unknown tool: %s", name)
		r.emitAudit(recorder, callCtx, "tools/call", name, err)
		return nil, err
	}
	if !scopeMatches(def.Scope, callCtx.Scope) {
		err := ErrorWithCode(CodeMethodNotFound, "tool %q is outside scope %q", name, callCtx.Scope)
		r.emitAudit(recorder, callCtx, "tools/call", name, err)
		return nil, err
	}
	if callCtx.Mode == ModeReadOnly && !def.ReadOnly {
		err := ErrorWithCode(CodeReadOnlyDenied, "tool %q is forbidden in readonly mode", name)
		r.emitAudit(recorder, callCtx, "tools/call", name, err)
		return nil, err
	}
	if def.Handler == nil {
		err := ErrorWithCode(CodeInternalError, "tool %q has nil handler", name)
		r.emitAudit(recorder, callCtx, "tools/call", name, err)
		return nil, err
	}

	res, err := def.Handler(ctx, callCtx, args)
	r.emitAudit(recorder, callCtx, "tools/call", name, err)
	return res, err
}

// ReadResource 在进程内读取指定 MCP Resource。
func (r *Registry) ReadResource(ctx context.Context, callCtx CallContext, uri string) (interface{}, string, error) {
	r.mu.RLock()
	def, ok := r.resources[strings.TrimSpace(uri)]
	recorder := r.auditRecorder
	r.mu.RUnlock()

	if !ok {
		err := ErrorWithCode(CodeMethodNotFound, "unknown resource: %s", uri)
		r.emitAudit(recorder, callCtx, "resources/read", uri, err)
		return nil, "", err
	}
	if !scopeMatches(def.Scope, callCtx.Scope) {
		err := ErrorWithCode(CodeMethodNotFound, "resource %q is outside scope %q", uri, callCtx.Scope)
		r.emitAudit(recorder, callCtx, "resources/read", uri, err)
		return nil, "", err
	}
	if def.Handler == nil {
		err := ErrorWithCode(CodeInternalError, "resource %q has nil handler", uri)
		r.emitAudit(recorder, callCtx, "resources/read", uri, err)
		return nil, "", err
	}

	content, mimeType, err := def.Handler(ctx, callCtx)
	if mimeType == "" {
		mimeType = def.MimeType
	}
	r.emitAudit(recorder, callCtx, "resources/read", uri, err)
	return content, mimeType, err
}

func (r *Registry) emitAudit(recorder AuditRecorder, callCtx CallContext, method, target string, err error) {
	if recorder == nil {
		return
	}
	ev := AuditEvent{
		Caller:   callCtx.Caller,
		Method:   method,
		Target:   target,
		Scope:    callCtx.Scope,
		Mode:     callCtx.Mode,
		ClientIP: callCtx.ClientIP,
		Success:  err == nil,
	}
	if err != nil {
		ev.ErrorMsg = err.Error()
	}
	recorder(ev)
}

// FormatToolResultText 将任意返回值格式化为标准 MCP tools/call content 结构。
func FormatToolResultText(v interface{}) map[string]interface{} {
	var text string
	switch s := v.(type) {
	case string:
		text = s
	default:
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			text = err.Error()
		} else {
			text = string(b)
		}
	}
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": text,
			},
		},
	}
}

// HandleJSONRPC 处理单条标准 MCP JSON-RPC 2.0 请求并返回响应对象。
func (r *Registry) HandleJSONRPC(ctx context.Context, callCtx CallContext, raw []byte) RPCResponse {
	var req RPCRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return RPCResponse{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: CodeParseError, Message: "Parse error: " + err.Error()},
		}
	}

	if strings.TrimSpace(req.Method) == "" {
		return RPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: CodeInvalidRequest, Message: "invalid request: missing method"},
		}
	}

	switch req.Method {
	case "initialize":
		serverName := r.serverName
		if callCtx.Scope != "" && callCtx.Scope != ScopeAll {
			serverName = serverName + "-" + string(callCtx.Scope)
		}
		return RPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"protocolVersion": ProtocolVersion,
				"capabilities": map[string]interface{}{
					"tools":     map[string]interface{}{},
					"resources": map[string]interface{}{},
				},
				"serverInfo": map[string]interface{}{
					"name":    serverName,
					"version": r.serverVersion,
				},
			},
		}

	case "notifications/initialized", "ping":
		return RPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]interface{}{},
		}

	case "tools/list":
		return RPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"tools": r.ListTools(callCtx.Scope, callCtx.Mode),
			},
		}

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return RPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &RPCError{Code: CodeInvalidParams, Message: "invalid params: " + err.Error()},
			}
		}
		res, err := r.CallTool(ctx, callCtx, params.Name, params.Arguments)
		if err != nil {
			return mapErrorToRPC(req.ID, err)
		}
		return RPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  FormatToolResultText(res),
		}

	case "resources/list":
		return RPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"resources": r.ListResources(callCtx.Scope),
			},
		}

	case "resources/read":
		var params struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil || strings.TrimSpace(params.URI) == "" {
			return RPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &RPCError{Code: CodeInvalidParams, Message: "invalid params: uri is required"},
			}
		}
		content, mimeType, err := r.ReadResource(ctx, callCtx, params.URI)
		if err != nil {
			return mapErrorToRPC(req.ID, err)
		}
		var text string
		if str, ok := content.(string); ok {
			text = str
		} else {
			b, mErr := json.MarshalIndent(content, "", "  ")
			if mErr != nil {
				text = mErr.Error()
			} else {
				text = string(b)
			}
		}
		return RPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"contents": []map[string]interface{}{
					{
						"uri":      params.URI,
						"mimeType": mimeType,
						"text":     text,
					},
				},
			},
		}

	default:
		return RPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: CodeMethodNotFound, Message: "method not found: " + req.Method},
		}
	}
}

func mapErrorToRPC(id interface{}, err error) RPCResponse {
	var coded *CodedError
	if errors.As(err, &coded) {
		return RPCResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &RPCError{Code: coded.Code, Message: coded.Message},
		}
	}
	return RPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: CodeToolError, Message: err.Error()},
	}
}
