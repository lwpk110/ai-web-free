package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ProtocolVersion 是当前实现的 MCP 协议版本。
const ProtocolVersion = "2024-11-05"

// Scope 定义 MCP 工具与资源的域过滤范围，避免单次挂载过多工具挤占 Agent 上下文。
type Scope string

const (
	ScopeAll    Scope = "all"
	ScopeOps    Scope = "ops"
	ScopeStudio Scope = "studio"
	ScopeDrama  Scope = "drama"
)

// ParseScope 解析并规范化 scope 参数，非法或空值回退为 ScopeAll。
func ParseScope(raw string) Scope {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ScopeOps):
		return ScopeOps
	case string(ScopeStudio):
		return ScopeStudio
	case string(ScopeDrama):
		return ScopeDrama
	case "", string(ScopeAll):
		return ScopeAll
	default:
		return ScopeAll
	}
}

// Mode 定义 MCP 调用权限模式。
type Mode string

const (
	ModeFull     Mode = "full"
	ModeReadOnly Mode = "readonly"
)

// ParseMode 解析并规范化 mode 参数，默认 full。
func ParseMode(raw string) Mode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(ModeReadOnly), "read-only", "ro":
		return ModeReadOnly
	default:
		return ModeFull
	}
}

// ToolAnnotations 遵循 MCP ToolAnnotations 规范，向 Agent 声明只读与破坏性提示。
type ToolAnnotations struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// BoolPtr 返回 bool 指针，便于构造 ToolAnnotations。
func BoolPtr(v bool) *bool {
	return &v
}

// CallContext 描述单次 MCP 工具或资源调用的上下文信息。
type CallContext struct {
	Caller   string
	Scope    Scope
	Mode     Mode
	ClientIP string
}

// ToolHandler 定义单个 MCP 工具的执行函数签名。
type ToolHandler func(ctx context.Context, callCtx CallContext, args json.RawMessage) (interface{}, error)

// ToolDef 描述一个注册到统一注册表的 MCP 工具。
type ToolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Scope       Scope                  `json:"-"`
	ReadOnly    bool                   `json:"-"`
	InputSchema map[string]interface{} `json:"inputSchema"`
	Annotations *ToolAnnotations       `json:"annotations,omitempty"`
	Handler     ToolHandler            `json:"-"`
}

// ResourceHandler 定义单个 MCP Resource 的读取函数签名。
type ResourceHandler func(ctx context.Context, callCtx CallContext) (content interface{}, mimeType string, err error)

// ResourceDef 描述一个注册到统一注册表的 MCP Resource。
type ResourceDef struct {
	URI         string          `json:"uri"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	MimeType    string          `json:"mimeType,omitempty"`
	Scope       Scope           `json:"-"`
	Handler     ResourceHandler `json:"-"`
}

// AuditEvent 记录每次 MCP 工具或资源调用的审计事件。
type AuditEvent struct {
	Caller   string
	Method   string
	Target   string
	Scope    Scope
	Mode     Mode
	ClientIP string
	Success  bool
	ErrorMsg string
}

// RPCRequest 表示标准 JSON-RPC 2.0 请求。
type RPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// RPCError 表示标准 JSON-RPC 2.0 错误对象。
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// RPCResponse 表示标准 JSON-RPC 2.0 响应。
type RPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// 标准及业务扩展 JSON-RPC 错误码。
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
	CodeToolError      = -32000
	CodeGuardrail      = -32001
	CodeReadOnlyDenied = -32003
)

// CodedError 允许工具处理器返回携带明确 JSON-RPC 错误码的错误。
type CodedError struct {
	Code    int
	Message string
}

func (e *CodedError) Error() string {
	return e.Message
}

// ErrorWithCode 构造一个带 JSON-RPC 错误码的 error。
func ErrorWithCode(code int, format string, args ...interface{}) error {
	return &CodedError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// RequireConfirm 检查高危/破坏性操作是否显式携带 confirm=true；未携带时返回护栏错误。
func RequireConfirm(confirm bool, actionDesc string) error {
	if confirm {
		return nil
	}
	return ErrorWithCode(CodeGuardrail, "guardrail: %s is destructive and requires explicit confirm=true (or use dry_run=true to preview)", actionDesc)
}
