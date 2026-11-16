package mcp

import (
	"context"
	"encoding/json"
	"strings"
)

// OpenAIToolFunction 描述 OpenAI tools[].function 结构。
type OpenAIToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// OpenAIToolDef 描述 OpenAI Chat Completions 兼容的工具定义。
type OpenAIToolDef struct {
	Type     string             `json:"type"`
	Function OpenAIToolFunction `json:"function"`
}

// OpenAITools 将统一注册表中的 MCP 工具导出为 OpenAI `tools` 列表，供内部 Agent / Chat Copilot 直调。
func (r *Registry) OpenAITools(scope Scope, mode Mode) []OpenAIToolDef {
	tools := r.ListTools(scope, mode)
	out := make([]OpenAIToolDef, 0, len(tools))
	for _, t := range tools {
		out = append(out, OpenAIToolDef{
			Type: "function",
			Function: OpenAIToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	return out
}

// ExecuteOpenAIToolCall 供内部 Agent / Chat Copilot 在进程内执行工具调用，并返回序列化后的文本结果。
func (r *Registry) ExecuteOpenAIToolCall(ctx context.Context, caller string, scope Scope, mode Mode, name string, rawArgs string) (string, error) {
	if strings.TrimSpace(caller) == "" {
		caller = "internal:copilot"
	}
	argsBytes := json.RawMessage(strings.TrimSpace(rawArgs))
	if len(argsBytes) == 0 {
		argsBytes = json.RawMessage(`{}`)
	}
	res, err := r.CallTool(ctx, CallContext{
		Caller: caller,
		Scope:  scope,
		Mode:   mode,
	}, name, argsBytes)
	if err != nil {
		return "", err
	}
	if str, ok := res.(string); ok {
		return str, nil
	}
	b, mErr := json.MarshalIndent(res, "", "  ")
	if mErr != nil {
		return "", mErr
	}
	return string(b), nil
}
