package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegistryScopeModeAndGuardrails(t *testing.T) {
	reg := NewRegistry("ai-web-free-test", "1.0.0")

	var audited []AuditEvent
	reg.SetAuditRecorder(func(ev AuditEvent) {
		audited = append(audited, ev)
	})

	reg.RegisterTool(ToolDef{
		Name:        "ops_overview",
		Description: "read ops overview",
		Scope:       ScopeOps,
		ReadOnly:    true,
		Annotations: &ToolAnnotations{ReadOnlyHint: BoolPtr(true), DestructiveHint: BoolPtr(false)},
		Handler: func(ctx context.Context, callCtx CallContext, args json.RawMessage) (interface{}, error) {
			return map[string]interface{}{"healthy": true, "caller": callCtx.Caller}, nil
		},
	})

	reg.RegisterTool(ToolDef{
		Name:        "ops_clear_logs",
		Description: "clear logs",
		Scope:       ScopeOps,
		ReadOnly:    false,
		Annotations: &ToolAnnotations{ReadOnlyHint: BoolPtr(false), DestructiveHint: BoolPtr(true)},
		Handler: func(ctx context.Context, callCtx CallContext, args json.RawMessage) (interface{}, error) {
			var p struct {
				Confirm bool `json:"confirm"`
			}
			_ = json.Unmarshal(args, &p)
			if err := RequireConfirm(p.Confirm, "clear_logs"); err != nil {
				return nil, err
			}
			return map[string]interface{}{"cleared": true}, nil
		},
	})

	reg.RegisterTool(ToolDef{
		Name:        "drama_list_projects",
		Description: "list drama projects",
		Scope:       ScopeDrama,
		ReadOnly:    true,
		Handler: func(ctx context.Context, callCtx CallContext, args json.RawMessage) (interface{}, error) {
			return []string{"proj-1"}, nil
		},
	})

	reg.RegisterResource(ResourceDef{
		URI:      "ai-web-free://ops/overview",
		Name:     "Ops Overview",
		Scope:    ScopeOps,
		MimeType: "application/json",
		Handler: func(ctx context.Context, callCtx CallContext) (interface{}, string, error) {
			return map[string]interface{}{"status": "ok"}, "application/json", nil
		},
	})

	// 1. Scope filtering on tools/list
	opsTools := reg.ListTools(ScopeOps, ModeFull)
	if len(opsTools) != 2 {
		t.Fatalf("expected 2 ops tools, got %d", len(opsTools))
	}
	dramaTools := reg.ListTools(ScopeDrama, ModeFull)
	if len(dramaTools) != 1 || dramaTools[0].Name != "drama_list_projects" {
		t.Fatalf("unexpected drama tools: %+v", dramaTools)
	}

	// 2. ReadOnly mode filters out write tools and blocks CallTool
	roTools := reg.ListTools(ScopeOps, ModeReadOnly)
	if len(roTools) != 1 || roTools[0].Name != "ops_overview" {
		t.Fatalf("expected only ops_overview in readonly mode, got %+v", roTools)
	}

	resp := reg.HandleJSONRPC(context.Background(), CallContext{
		Caller: "external:mcp",
		Scope:  ScopeOps,
		Mode:   ModeReadOnly,
	}, []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ops_clear_logs","arguments":{"confirm":true}}}`))
	if resp.Error == nil || resp.Error.Code != CodeReadOnlyDenied {
		t.Fatalf("expected CodeReadOnlyDenied, got %+v", resp.Error)
	}

	// 3. Guardrail blocks destructive action without confirm=true
	resp = reg.HandleJSONRPC(context.Background(), CallContext{
		Caller: "external:mcp",
		Scope:  ScopeOps,
		Mode:   ModeFull,
	}, []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ops_clear_logs","arguments":{}}}`))
	if resp.Error == nil || resp.Error.Code != CodeGuardrail {
		t.Fatalf("expected CodeGuardrail, got %+v", resp.Error)
	}

	// 4. Confirm=true succeeds
	resp = reg.HandleJSONRPC(context.Background(), CallContext{
		Caller: "external:mcp",
		Scope:  ScopeOps,
		Mode:   ModeFull,
	}, []byte(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"ops_clear_logs","arguments":{"confirm":true}}}`))
	if resp.Error != nil {
		t.Fatalf("expected success with confirm=true, got %+v", resp.Error)
	}

	// 5. Resources list and read
	resListResp := reg.HandleJSONRPC(context.Background(), CallContext{
		Caller: "external:mcp",
		Scope:  ScopeOps,
		Mode:   ModeReadOnly,
	}, []byte(`{"jsonrpc":"2.0","id":4,"method":"resources/list"}`))
	if resListResp.Error != nil {
		t.Fatalf("resources/list failed: %+v", resListResp.Error)
	}
	resReadResp := reg.HandleJSONRPC(context.Background(), CallContext{
		Caller: "external:mcp",
		Scope:  ScopeOps,
		Mode:   ModeReadOnly,
	}, []byte(`{"jsonrpc":"2.0","id":5,"method":"resources/read","params":{"uri":"ai-web-free://ops/overview"}}`))
	if resReadResp.Error != nil {
		t.Fatalf("resources/read failed: %+v", resReadResp.Error)
	}

	// 6. Internal Copilot OpenAI bridge
	oaiTools := reg.OpenAITools(ScopeAll, ModeFull)
	if len(oaiTools) != 3 {
		t.Fatalf("expected 3 OpenAI tools, got %d", len(oaiTools))
	}
	out, err := reg.ExecuteOpenAIToolCall(context.Background(), "internal:copilot", ScopeAll, ModeFull, "ops_overview", `{}`)
	if err != nil {
		t.Fatalf("ExecuteOpenAIToolCall failed: %v", err)
	}
	if out == "" {
		t.Fatalf("expected non-empty output from ExecuteOpenAIToolCall")
	}

	if len(audited) < 4 {
		t.Fatalf("expected audit events to be recorded, got %d", len(audited))
	}
}
