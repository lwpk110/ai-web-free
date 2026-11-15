package provider

import (
	"errors"
	"sync"

	"ai-web-free/internal/accounts"
)

var ErrProviderNotFound = errors.New("provider plugin not found")
var ErrProviderNotImplemented = errors.New("provider plugin is registered as planned slot and not yet active")

// ChannelTag 描述渠道或模型标签的元数据（四维度体系）
type ChannelTag struct {
	Key      string `json:"key"`      // 唯一标识，如 "tts-voice-clone"
	Label    string `json:"label"`    // 中文展示名，如 "声音复刻"
	Category string `json:"category"` // "cost" | "auth" | "feature" | "reliability"
	Variant  string `json:"variant"`  // "default" | "success" | "warning" | "purple" | "cyan"
	Icon     string `json:"icon"`     // Lucide 图标名
	Tooltip  string `json:"tooltip"`  // 详细解释说明
}

// ProviderDescriptor 描述供应商插件的元信息、支持的能力与凭据格式
type ProviderDescriptor struct {
	ID                    string       `json:"id"`
	Name                  string       `json:"name"`
	Vendor                string       `json:"vendor"`
	Status                string       `json:"status"` // "enabled" | "planned"
	Version               string       `json:"version"`
	Description           string       `json:"description"`
	SupportedAccountTypes []string     `json:"supported_account_types"`
	CredentialFormats     []string     `json:"credential_formats"`
	Capabilities          []string     `json:"capabilities"`
	FeaturedModels        []string     `json:"featured_models"`
	Tags                  []ChannelTag `json:"tags,omitempty"`
	CostType              string       `json:"cost_type,omitempty"` // "permanent_free" | "daily_credits" | "trial_quota"
	AuthType              string       `json:"auth_type,omitempty"` // "no_auth" | "cookie" | "api_key"
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

// StandardChannelTags 预置标准四维度渠道标签
var StandardChannelTags = map[string]ChannelTag{
	"permanent-free": {
		Key:      "permanent-free",
		Label:    "永久免费",
		Category: "cost",
		Variant:  "success",
		Icon:     "Sparkles",
		Tooltip:  "官方或逆向通道永久免计费，无需充值",
	},
	"daily-credits": {
		Key:      "daily-credits",
		Label:    "每日重置积分",
		Category: "cost",
		Variant:  "purple",
		Icon:     "Coins",
		Tooltip:  "每日凌晨重置免费额度或登录赠送积分",
	},
	"trial-quota": {
		Key:      "trial-quota",
		Label:    "新人限期额度",
		Category: "cost",
		Variant:  "warning",
		Icon:     "Clock",
		Tooltip:  "官方新账号赠送 30~90 天免费体验配额",
	},
	"no-auth": {
		Key:      "no-auth",
		Label:    "免密免登",
		Category: "auth",
		Variant:  "cyan",
		Icon:     "ShieldCheck",
		Tooltip:  "无需配置账号密码或 API Key，开箱即用",
	},
	"cookie-pool": {
		Key:      "cookie-pool",
		Label:    "Cookie号池",
		Category: "auth",
		Variant:  "default",
		Icon:     "Cookie",
		Tooltip:  "基于 Web 端 Session/Cookie 凭据轮询调度",
	},
	"apikey-pool": {
		Key:      "apikey-pool",
		Label:    "官方Key池",
		Category: "auth",
		Variant:  "default",
		Icon:     "Key",
		Tooltip:  "基于官方开放平台 API Key 负载均衡",
	},
	"tts-presets": {
		Key:      "tts-presets",
		Label:    "200+爆款音色",
		Category: "feature",
		Variant:  "success",
		Icon:     "Mic",
		Tooltip:  "内置海量爆款短剧、影视解说与搞怪方言音色",
	},
	"tts-voice-clone": {
		Key:      "tts-voice-clone",
		Label:    "声音复刻(克隆)",
		Category: "feature",
		Variant:  "purple",
		Icon:     "AudioWaveform",
		Tooltip:  "上传 10~20 秒参考音频即可快速克隆指定声音",
	},
	"tts-voice-design": {
		Key:      "tts-voice-design",
		Label:    "自然语言捏声音",
		Category: "feature",
		Variant:  "cyan",
		Icon:     "Wand2",
		Tooltip:  "纯通过文本描述（Prompt）凭空捏造全新的独有音色",
	},
	"video-gen": {
		Key:      "video-gen",
		Label:    "AI视频生成",
		Category: "feature",
		Variant:  "purple",
		Icon:     "Film",
		Tooltip:  "支持文生视频与图生视频生成",
	},
	"img-flux-wanx": {
		Key:      "img-flux-wanx",
		Label:    "高清文生图",
		Category: "feature",
		Variant:  "warning",
		Icon:     "Image",
		Tooltip:  "支持 FLUX.1、通义万相、即梦等超清文生图",
	},
	"official-sla": {
		Key:      "official-sla",
		Label:    "官方企业稳定",
		Category: "reliability",
		Variant:  "default",
		Icon:     "Activity",
		Tooltip:  "走官方云服务标准开放协议，高可靠无频繁反爬变更",
	},
	"proxy-bound": {
		Key:      "proxy-bound",
		Label:    "代理限流保护",
		Category: "reliability",
		Variant:  "warning",
		Icon:     "Globe",
		Tooltip:  "绑定独立代理节点与令牌桶限频防 WAF",
	},
	"reverse-web": {
		Key:      "reverse-web",
		Label:    "Web客户端逆向",
		Category: "reliability",
		Variant:  "cyan",
		Icon:     "Code2",
		Tooltip:  "基于官方 Web/App 端协议逆向仿真",
	},
}

// MakeTags 快捷组装已定义的标准标签
func MakeTags(keys ...string) []ChannelTag {
	res := make([]ChannelTag, 0, len(keys))
	for _, k := range keys {
		if tag, ok := StandardChannelTags[k]; ok {
			res = append(res, tag)
		}
	}
	return res
}
