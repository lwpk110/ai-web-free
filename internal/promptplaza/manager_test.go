package promptplaza

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadSeedsAndCategories(t *testing.T) {
	items, err := LoadSeedItems()
	if err != nil {
		t.Fatalf("LoadSeedItems failed: %v", err)
	}
	if len(items) < 24 {
		t.Fatalf("expected >= 24 seed items, got %d", len(items))
	}

	catSeen := make(map[string]int)
	modeSeen := make(map[string]int)
	sizeSeen := make(map[string]int)

	for _, it := range items {
		catSeen[it.Category]++
		modeSeen[it.RecommendedMode]++
		sizeSeen[it.RecommendedSize]++
		if it.TitleZH == "" || it.PromptEN == "" || it.PromptZH == "" {
			t.Fatalf("seed %s has empty text fields", it.ID)
		}
		if !strings.HasPrefix(it.PreviewURL, "https://") {
			t.Fatalf("seed %s missing valid preview_url, got %q", it.ID, it.PreviewURL)
		}
		if len(it.Variables) == 0 {
			t.Fatalf("seed %s has no interactive variables", it.ID)
		}
		for _, v := range it.Variables {
			token := "[" + v.Key + "]"
			if !strings.Contains(it.PromptEN, token) {
				t.Fatalf("seed %s prompt_en missing placeholder %s", it.ID, token)
			}
			if !strings.Contains(it.PromptZH, token) {
				t.Fatalf("seed %s prompt_zh missing placeholder %s", it.ID, token)
			}
			if v.DefaultValue == "" || len(v.Options) == 0 {
				t.Fatalf("seed %s variable %s missing default or options", it.ID, v.Key)
			}
		}
	}

	for _, cat := range AllCategories() {
		if catSeen[cat] == 0 {
			t.Fatalf("expected category %q to have seed items", cat)
		}
	}
	if modeSeen["generations"] == 0 || modeSeen["edits"] == 0 {
		t.Fatalf("expected both generations and edits modes in seeds: %+v", modeSeen)
	}
	for _, sz := range []string{"1024x1024", "1792x1024", "1024x1792"} {
		if sizeSeen[sz] == 0 {
			t.Fatalf("expected size %s in seeds", sz)
		}
	}
}

func TestSyncAllSourcesAndAICurator(t *testing.T) {
	civitaiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"id":     9001,
					"url":    "https://cdn.example.test/civitai-9001.jpg",
					"width":  1400,
					"height": 800,
					"nsfw":   "None",
					"stats":  map[string]int{"likeCount": 420, "heartCount": 180},
					"meta":   map[string]string{"prompt": "masterpiece, best quality, (isometric crystal terrarium:1.2), <lora:detail:0.8>, warm sunlit moss"},
				},
				{
					"id":     9002,
					"url":    "https://cdn.example.test/nsfw.jpg",
					"width":  1024,
					"height": 1024,
					"nsfw":   true,
					"stats":  map[string]int{"likeCount": 999, "heartCount": 999},
					"meta":   map[string]string{"prompt": "should be skipped because nsfw=true"},
				},
			},
		})
	}))
	defer civitaiSrv.Close()

	lexicaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"images": []map[string]any{
				{
					"id":       "lex-1",
					"prompt":   "Editorial minimalist perfume bottle product photography on travertine stone",
					"srcSmall": "https://cdn.example.test/lex-1-sm.jpg",
					"width":    800,
					"height":   1200,
					"nsfw":     false,
				},
			},
		})
	}))
	defer lexicaSrv.Close()

	redditSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"children": []map[string]any{
					{
						"data": map[string]any{
							"id":                     "red1",
							"title":                  "Vintage Swiss typography poster made with gpt-image-1",
							"selftext":               "Swiss modernist exhibition poster reading FUTURE 2026 in bold red and cream",
							"ups":                    350,
							"over_18":                false,
							"url_overridden_by_dest": "https://i.redd.it/sample.png",
						},
					},
					{
						"data": map[string]any{
							"id":      "red-nsfw",
							"title":   "NSFW filtered",
							"over_18": true,
						},
					},
				},
			},
		})
	}))
	defer redditSrv.Close()

	hfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"rows": []map[string]any{
				{
					"row_idx": 42,
					"row": map[string]any{
						"Prompt": "Watercolor Kyoto street scene with cozy paper lanterns at twilight",
						"image": map[string]any{
							"src":    "https://cdn.example.test/hf-42.jpg",
							"width":  1400,
							"height": 800,
						},
					},
				},
			},
		})
	}))
	defer hfSrv.Close()

	var sawWebSearch bool
	mockExec := func(ctx context.Context, sysPrompt, userPrompt string, useWebSearch bool) (string, error) {
		if useWebSearch {
			sawWebSearch = true
			return `[{"title_zh":"AI 趋势 · 3D 透明键帽","category":"3D 与微缩","prompt_en":"Translucent crystal keycap enclosing a miniature [主体], studio macro","prompt_zh":"内部封装微缩[主体]的透明树脂键帽，棚拍微距","recommended_mode":"generations","recommended_model":"gpt-image-2.5-sunburst","recommended_size":"1024x1024","variables":[{"key":"主体","label":"内嵌景物","default_value":"glowing coral reef","options":["glowing coral reef","tiny Fuji mountain"]}]}]`, nil
		}
		return `[{"id":"civitai-9001","title_zh":"AI 提炼 · 晶体生态瓶","category":"3D 与微缩","prompt_en":"An isometric crystal terrarium containing a miniature [主体] bathed in warm sunlight","prompt_zh":"沐浴在温暖阳光下的等距视角水晶生态瓶，内有微缩[主体]","recommended_mode":"generations","recommended_model":"gpt-image-2.5-sunburst","recommended_size":"1792x1024","variables":[{"key":"主体","label":"生态主题","default_value":"mossy bonsai garden","options":["mossy bonsai garden","desert cactus oasis"]}]}]`, nil
	}

	persistPath := filepath.Join(t.TempDir(), "plaza.json")
	mgr := NewManager(Options{
		PersistFile:  persistPath,
		ChatExecutor: mockExec,
		Endpoints: Endpoints{
			CivitaiURL:     civitaiSrv.URL,
			LexicaURL:      lexicaSrv.URL,
			RedditURL:      redditSrv.URL,
			HuggingFaceURL: hfSrv.URL,
			LexicaTopics:   []string{"minimalist product"},
		},
	})

	status := mgr.TriggerSync(context.Background(), false)
	if status.Running {
		t.Fatal("expected sync to complete synchronously")
	}
	if status.LastError != "" {
		t.Fatalf("unexpected sync error: %s", status.LastError)
	}
	if !sawWebSearch {
		t.Fatal("expected AI search executor with useWebSearch=true to be invoked")
	}

	resp := mgr.List(ListFilter{Source: SourceCivitai})
	if len(resp.Items) != 1 || resp.Items[0].ID != "civitai-9001" {
		t.Fatalf("expected only civitai-9001 (NSFW skipped), got %+v", resp.Items)
	}
	if resp.Items[0].TitleZH != "AI 提炼 · 晶体生态瓶" {
		t.Fatalf("expected AI curated title, got %q", resp.Items[0].TitleZH)
	}

	aiSearchResp := mgr.List(ListFilter{Source: SourceAISearch})
	if len(aiSearchResp.Items) != 1 || aiSearchResp.Items[0].TitleZH != "AI 趋势 · 3D 透明键帽" {
		t.Fatalf("expected ai_search item, got %+v", aiSearchResp.Items)
	}

	// Verify Lexica / Reddit / HF also merged cleanly
	for _, src := range []string{SourceLexica, SourceReddit, SourceHuggingFace} {
		sub := mgr.List(ListFilter{Source: src})
		if len(sub.Items) == 0 {
			t.Fatalf("expected items from source %s", src)
		}
		if src == SourceHuggingFace && sub.Items[0].PreviewURL != "https://cdn.example.test/hf-42.jpg" {
			t.Fatalf("expected hf preview_url parsed from row.image.src, got %q", sub.Items[0].PreviewURL)
		}
	}

	// Verify disk persistence reload
	mgr2 := NewManager(Options{PersistFile: persistPath})
	reloaded := mgr2.List(ListFilter{Source: SourceAISearch})
	if len(reloaded.Items) != 1 {
		t.Fatalf("expected persisted ai_search item on reload, got %d", len(reloaded.Items))
	}
}

func TestListFilteringAndSorting(t *testing.T) {
	mgr := NewManager(Options{})
	edits := mgr.List(ListFilter{Mode: "edits"})
	if len(edits.Items) == 0 {
		t.Fatal("expected edits items")
	}
	for _, it := range edits.Items {
		if it.RecommendedMode != "edits" {
			t.Fatalf("got non-edits item %s in edits filter", it.ID)
		}
	}

	qResp := mgr.List(ListFilter{Query: "钛金属"})
	if len(qResp.Items) == 0 {
		t.Fatal("expected query match for 钛金属")
	}
}

func TestExpandIdeaAIAndFallback(t *testing.T) {
	mgr := NewManager(Options{})
	if _, err := mgr.Expand(context.Background(), ExpandRequest{Idea: "   "}); err == nil {
		t.Fatal("expected error for blank idea")
	}

	// Fallback path when ChatExecutor is nil
	fb, err := mgr.Expand(context.Background(), ExpandRequest{
		Idea: "赛博朋克猫咪在霓虹雨夜喝咖啡",
		Mode: "generations",
		Size: "1792x1024",
	})
	if err != nil {
		t.Fatalf("fallback expand failed: %v", err)
	}
	if fb.Source != "smart_fallback" || !strings.Contains(fb.PromptEN, "[主体]") || len(fb.Variables) == 0 {
		t.Fatalf("unexpected fallback expand response: %+v", fb)
	}

	// AI pool path
	mgrAI := NewManager(Options{
		ChatExecutor: func(ctx context.Context, sys, user string, webSearch bool) (string, error) {
			return `{"title_zh":"霓虹雨夜咖啡猫","category":"摄影写实","prompt_en":"Cyberpunk ginger cat sipping espresso in a neon rain alley, [光影材质]","prompt_zh":"霓虹雨夜小巷里喝浓缩咖啡的赛博朋克橘猫，[光影材质]","recommended_mode":"generations","recommended_model":"gpt-image-2.5-sunburst","recommended_size":"1792x1024","variables":[{"key":"光影材质","label":"光影质感","default_value":"wet neon puddle reflections","options":["wet neon puddle reflections"]}]}`, nil
		},
	})
	res, err := mgrAI.Expand(context.Background(), ExpandRequest{
		Idea: "赛博朋克猫咪喝咖啡",
		Mode: "generations",
		Size: "1792x1024",
	})
	if err != nil {
		t.Fatalf("AI expand failed: %v", err)
	}
	if res.Source != "ai_pool" || res.TitleZH != "霓虹雨夜咖啡猫" {
		t.Fatalf("unexpected AI expand result: %+v", res)
	}

	// Graceful fallback when AI fails
	mgrFail := NewManager(Options{
		ChatExecutor: func(ctx context.Context, sys, user string, webSearch bool) (string, error) {
			return "", errors.New("pool exhausted")
		},
	})
	res2, err := mgrFail.Expand(context.Background(), ExpandRequest{Idea: "玻璃质感太阳图标", Size: "1024x1024"})
	if err != nil || res2.Source != "smart_fallback" {
		t.Fatalf("expected smart_fallback on AI failure, got %+v err=%v", res2, err)
	}
}

func TestSyncAllSourcesFailureRetainsSeeds(t *testing.T) {
	failSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream down", http.StatusBadGateway)
	}))
	defer failSrv.Close()

	mgr := NewManager(Options{
		HTTPTimeout: 2 * time.Second,
		Endpoints: Endpoints{
			CivitaiURL:     failSrv.URL,
			LexicaURL:      failSrv.URL,
			RedditURL:      failSrv.URL,
			HuggingFaceURL: failSrv.URL,
			LexicaTopics:   []string{"test"},
		},
	})
	before := len(mgr.List(ListFilter{}).Items)
	status := mgr.TriggerSync(context.Background(), false)
	if status.State != "last_error" || status.LastError == "" {
		t.Fatalf("expected last_error when all sources fail, got %+v", status)
	}
	after := len(mgr.List(ListFilter{}).Items)
	if after != before || after < 24 {
		t.Fatalf("expected seeds preserved on failure, before=%d after=%d", before, after)
	}
}
