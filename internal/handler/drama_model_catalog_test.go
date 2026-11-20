package handler

import (
	"testing"

	"ai-web-free/internal/provider"
)

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func TestDramaModelCatalogAggregatesExecutableModels(t *testing.T) {
	reg := provider.NewRegistry()
	reg.Register(provider.NewSiliconFlowProvider([]string{"sf-key"}, ""))
	reg.Register(provider.NewDashScopeProvider([]string{"ds-key"}, ""))
	reg.Register(provider.NewStubProvider(provider.ProviderDescriptor{
		ID:           "claude",
		Name:         "Claude Web",
		Status:       "planned",
		Capabilities: []string{"chat"},
		FeaturedModels: []string{
			"claude-4-5-sonnet",
			"claude-4-5-haiku",
		},
	}))

	textCatalog := dramaModelCatalog(nil, reg, "chat", nil)
	var textModels []string
	textAvailable := false
	for _, item := range textCatalog {
		textModels = append(textModels, item.Models...)
		if item.ID == "claude" && item.Available {
			t.Fatalf("planned Claude provider should not be available")
		}
		if item.Available {
			textAvailable = true
		}
	}
	if !textAvailable || !containsString(textModels, "qwen-plus") || !containsString(textModels, "deepseek-ai/DeepSeek-R1-Distill-Qwen-7B") {
		t.Fatalf("expected active text models, got %+v", textCatalog)
	}
	if chatgptModels := dramaModelsForProvider("chatgpt", nil, []string{"auto", "gpt-4o"}, "chat"); containsString(chatgptModels, "auto") || !containsString(chatgptModels, "gpt-4o") {
		t.Fatalf("auto should be filtered from cached ChatGPT text models: %+v", chatgptModels)
	}

	imageCatalog := dramaModelCatalog(nil, reg, "image_generation", nil)
	var imageModels []string
	for _, item := range imageCatalog {
		imageModels = append(imageModels, item.Models...)
	}
	if !containsString(imageModels, "wanx-v1") || !containsString(imageModels, "black-forest-labs/FLUX.1-schnell") {
		t.Fatalf("expected provider-specific image models, got %+v", imageCatalog)
	}
	if grokImageModels := dramaModelsForProvider("grok", []string{"grok-3"}, nil, "image_generation"); !containsString(grokImageModels, "grok-2-image") {
		t.Fatalf("expected grok-2-image in drama models for grok, got: %+v", grokImageModels)
	}
	if containsString(imageModels, "auto") {
		t.Fatalf("auto should only be the UI sentinel, not a provider model")
	}

	videoCatalog := dramaModelCatalog(nil, reg, "video_generation", nil)
	var videoModels []string
	for _, item := range videoCatalog {
		videoModels = append(videoModels, item.Models...)
	}
	if !containsString(videoModels, "THUDM/CogVideoX-5b") || !containsString(videoModels, "wan-video-v1") {
		t.Fatalf("expected provider-specific video models, got %+v", videoCatalog)
	}

	picked := randomDramaModel([]string{"a", "b"})
	if !containsString([]string{"a", "b"}, picked) {
		t.Fatalf("random model returned unknown value: %s", picked)
	}

	providerPicked := randomDramaProviderModel(textCatalog, func(id string) bool { return id == "dashscope" })
	if providerPicked != "qwen-plus" {
		t.Fatalf("expected filtered DashScope model, got %s", providerPicked)
	}
}
