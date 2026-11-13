package provider

import (
	"errors"
	"sync"

	"ai-web-free/internal/accounts"
)

var ErrProviderNotFound = errors.New("provider plugin not found")
var ErrProviderNotImplemented = errors.New("provider plugin is registered as planned slot and not yet active")

// ProviderDescriptor 描述供应商插件的元信息、支持的能力与凭据格式
type ProviderDescriptor struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	Vendor                string   `json:"vendor"`
	Status                string   `json:"status"` // "enabled" | "planned"
	Version               string   `json:"version"`
	Description           string   `json:"description"`
	SupportedAccountTypes []string `json:"supported_account_types"`
	CredentialFormats     []string `json:"credential_formats"`
	Capabilities          []string `json:"capabilities"`
	FeaturedModels        []string `json:"featured_models"`
}

// ImportRequest 批量导入号池凭据请求
type ImportRequest struct {
	CredentialType string   `json:"credential_type"` // "access_token" | "session_token" | "refresh_token" | "free_uuid" | "kami" | "sub2api_json"
	Tokens         []string `json:"tokens"`          // 凭据行列表
	RawText        string   `json:"raw_text"`        // 或多行原始文本 / JSON
	Persist        bool     `json:"persist"`         // 是否持久化写回文件
	GenerateCount  int      `json:"generate_count"`  // 仅当 credential_type == "free_uuid" 且自动生成时使用
	AutoLogin      bool     `json:"auto_login"`      // 仅当 credential_type == "kami" 时，导入后自动调用 Playwright 登录提取 Token
	Headless       bool     `json:"headless"`        // Playwright 是否以无头模式运行
}

// ImportResult 批量导入结果统计
type ImportResult struct {
	Added     int      `json:"added"`
	Skipped   int      `json:"skipped"`
	Failed    int      `json:"failed"`
	TotalPool int      `json:"total_pool"`
	Persisted bool     `json:"persisted"`
	Messages  []string `json:"messages,omitempty"`
}

// ProviderPlugin 多供应商免费号池插件统一契约接口
type ProviderPlugin interface {
	Descriptor() ProviderDescriptor
	Stats() accounts.PoolStats
	ListAccounts(typeFilter, statusFilter, search string) []accounts.AccountSnapshot
	ImportCredentials(req ImportRequest) (ImportResult, error)
	ExecuteAccountAction(id, action string) (map[string]interface{}, error)
}

// Registry 供应商插件注册中心
type Registry struct {
	mu        sync.RWMutex
	order     []string
	providers map[string]ProviderPlugin
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]ProviderPlugin),
	}
}

func (r *Registry) Register(p ProviderPlugin) {
	if p == nil {
		return
	}
	desc := p.Descriptor()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[desc.ID]; !exists {
		r.order = append(r.order, desc.ID)
	}
	r.providers[desc.ID] = p
}

func (r *Registry) Get(id string) (ProviderPlugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	return p, ok
}

func (r *Registry) ListDescriptors() []ProviderDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ProviderDescriptor, 0, len(r.order))
	for _, id := range r.order {
		if p, ok := r.providers[id]; ok {
			out = append(out, p.Descriptor())
		}
	}
	return out
}

// StubProvider 预留的插件扩展槽位（用于后续接入 Claude / Gemini / Grok / DeepSeek 等免费 Web 号池）
type StubProvider struct {
	desc ProviderDescriptor
}

func NewStubProvider(desc ProviderDescriptor) *StubProvider {
	return &StubProvider{desc: desc}
}

func (s *StubProvider) Descriptor() ProviderDescriptor {
	return s.desc
}

func (s *StubProvider) Stats() accounts.PoolStats {
	return accounts.PoolStats{}
}

func (s *StubProvider) ListAccounts(typeFilter, statusFilter, search string) []accounts.AccountSnapshot {
	return []accounts.AccountSnapshot{}
}

func (s *StubProvider) ImportCredentials(req ImportRequest) (ImportResult, error) {
	return ImportResult{}, ErrProviderNotImplemented
}

func (s *StubProvider) ExecuteAccountAction(id, action string) (map[string]interface{}, error) {
	return nil, ErrProviderNotImplemented
}
