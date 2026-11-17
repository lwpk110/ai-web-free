package handler

import (
	"math/rand"
	"strings"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/provider"
)

type dramaModelProviderInfo struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	Available bool     `json:"available"`
	Active    int      `json:"active"`
	Models    []string `json:"models"`
	Reason    string   `json:"reason,omitempty"`
}

func dramaProviderActive(pool *accounts.Pool, registry *provider.Registry, id string) (bool, int) {
	if id == "chatgpt" {
		if pool == nil {
			return false, 0
		}
		st := pool.Stats()
		return st.Active > 0, st.Active
	}
	if registry == nil {
		return false, 0
	}
	p, ok := registry.Get(id)
	if !ok || p == nil {
		return false, 0
	}
	st := p.Stats()
	return st.Active > 0, st.Active
}

func looksLikeTextModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" || m == "auto" {
		return false
	}
	for _, part := range []string{"image", "dall-e", "tts", "transcribe", "whisper", "sora", "imagen", "wanx", "flux", "jimeng", "muse", "video"} {
		if strings.Contains(m, part) {
			return false
		}
	}
	return true
}

func looksLikeImageModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return false
	}
	for _, part := range []string{"image", "dall-e", "imagen", "wanx", "flux", "jimeng"} {
		if strings.Contains(m, part) {
			return true
		}
	}
	return false
}

func normalizeModelList(models []string, textOnly bool) []string {
	seen := make(map[string]bool, len(models))
	out := make([]string, 0, len(models))
	for _, raw := range models {
		model := strings.TrimSpace(raw)
		if model == "" || strings.EqualFold(model, "auto") || seen[model] {
			continue
		}
		if textOnly && !looksLikeTextModel(model) {
			continue
		}
		if !textOnly && !looksLikeImageModel(model) {
			continue
		}
		seen[model] = true
		out = append(out, model)
	}
	return out
}

func dramaModelsForProvider(providerID string, featured []string, cachedChatGPT []string, capability string) []string {
	textOnly := capability == "chat"
	filter := func(models []string) []string { return normalizeModelList(models, textOnly) }
	if capability == "video_generation" {
		switch providerID {
		case "siliconflow":
			return []string{"THUDM/CogVideoX-5b", "THUDM/CogVideoX-2b"}
		case "dashscope":
			return []string{"wan-video-v1"}
		case "meta_muse":
			return []string{"muse-video", "muse-talk"}
		case "jimeng":
			return []string{"jimeng-video-2.0"}
		default:
			return nil
		}
	}
	switch providerID {
	case "chatgpt":
		return filter(cachedChatGPT)
	case "siliconflow":
		if textOnly {
			return filter([]string{"deepseek-ai/DeepSeek-R1-Distill-Qwen-7B"})
		}
		return filter([]string{"flux-schnell", "black-forest-labs/FLUX.1-schnell"})
	case "dashscope":
		if textOnly {
			return filter([]string{"qwen-plus"})
		}
		return filter([]string{"wanx-v1"})
	case "jimeng":
		if textOnly {
			return nil
		}
		return filter([]string{"jimeng-4.5", "jimeng-3.0"})
	default:
		return filter(featured)
	}
}

func dramaModelCatalog(pool *accounts.Pool, registry *provider.Registry, capability string, cachedChatGPT []string) []dramaModelProviderInfo {
	capabilityName := capability
	out := make([]dramaModelProviderInfo, 0, 8)
	if registry == nil {
		registry = provider.NewRegistry()
	}
	for _, desc := range registry.ListDescriptors() {
		hasCapability := false
		for _, item := range desc.Capabilities {
			if item == capabilityName {
				hasCapability = true
				break
			}
		}
		if !hasCapability {
			continue
		}
		available, active := dramaProviderActive(pool, registry, desc.ID)
		models := dramaModelsForProvider(desc.ID, desc.FeaturedModels, cachedChatGPT, capabilityName)
		if len(models) == 0 {
			continue
		}
		reason := ""
		if !available {
			reason = "供应商未配置或暂无活跃凭据"
			if desc.Status == "planned" {
				reason = "预留供应商，执行器尚未接入"
			}
		}
		out = append(out, dramaModelProviderInfo{
			ID:        desc.ID,
			Name:      desc.Name,
			Status:    desc.Status,
			Available: available,
			Active:    active,
			Models:    models,
			Reason:    reason,
		})
	}
	return out
}

func flattenDramaModelCatalog(catalog []dramaModelProviderInfo, availableOnly bool) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, 16)
	for _, p := range catalog {
		if availableOnly && !p.Available {
			continue
		}
		for _, model := range p.Models {
			if !seen[model] {
				seen[model] = true
				out = append(out, model)
			}
		}
	}
	return out
}

func randomDramaModel(models []string) string {
	if len(models) == 0 {
		return ""
	}
	return models[rand.Intn(len(models))]
}

func randomDramaProviderModel(catalog []dramaModelProviderInfo, allowed func(id string) bool) string {
	if allowed == nil {
		allowed = func(string) bool { return true }
	}
	providers := make([]dramaModelProviderInfo, 0, len(catalog))
	for _, item := range catalog {
		if item.Available && len(item.Models) > 0 && allowed(item.ID) {
			providers = append(providers, item)
		}
	}
	if len(providers) == 0 {
		return ""
	}
	provider := providers[rand.Intn(len(providers))]
	return randomDramaModel(provider.Models)
}

func dramaLLMProviderForModel(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(m, "qwen"):
		return "dashscope"
	case strings.Contains(m, "deepseek-ai/"):
		return "siliconflow"
	case strings.HasPrefix(m, "grok-") && !strings.Contains(m, "image"):
		return "grok"
	case strings.HasPrefix(m, "claude"):
		return "claude"
	case strings.HasPrefix(m, "gemini"):
		return "gemini"
	case strings.HasPrefix(m, "deepseek"):
		return "deepseek"
	default:
		return "chatgpt"
	}
}
