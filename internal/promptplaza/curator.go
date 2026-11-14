package promptplaza

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	reLoraTag      = regexp.MustCompile(`<[^>]+>`)
	reWeightParens = regexp.MustCompile(`\(([^():]+):[0-9.]+\)`)
	reMultiSpace   = regexp.MustCompile(`\s{2,}`)
	reMultiComma   = regexp.MustCompile(`,\s*,+`)
)

var validCategories = map[string]bool{
	CategoryPhotography:       true,
	Category3DMiniature:       true,
	CategoryPosterTypography:  true,
	CategoryIllustrationAnime: true,
	CategoryProductUI:         true,
	CategoryReferenceEdit:     true,
}

var validSizes = map[string]bool{
	"1024x1024": true,
	"1792x1024": true,
	"1024x1792": true,
}

var validModels = map[string]bool{
	"gpt-image-2.5-sunburst": true,
	"gpt-image-2.5-flare":    true,
	"gpt-image-2.5":          true,
	"gpt-image-2.6":          true,
	"dall-e-3":               true,
}

func normalizeCategory(cat, prompt string) string {
	trimmed := strings.TrimSpace(cat)
	if validCategories[trimmed] {
		return trimmed
	}
	return inferCategory(prompt)
}

func inferCategory(text string) string {
	lower := strings.ToLower(text)
	switch {
	case containsAny(lower, "reference photo", "uploaded image", "transform the uploaded", "keep the exact geometry", "material remodel", "style transfer", "recolor", "replace background", "参考图", "换景", "重塑"):
		return CategoryReferenceEdit
	case containsAny(lower, "poster", "typography", "headline", "magazine cover", "infographic", "font", "lettering", "logo", "海报", "排版", "字体", "文字"):
		return CategoryPosterTypography
	case containsAny(lower, "isometric", "miniature", "diorama", "3d render", "blender", "octane", "c4d", "keycap", "figurine", "blind-box", "clay", "voxel", "knolling", "微缩", "3d", "手办", "键帽"):
		return Category3DMiniature
	case containsAny(lower, "product shot", "perfume", "synthesizer", "ui/ux", "dashboard", "app icon", "mockup", "industrial design", "packaging", "bottle", "产品", "图标", "样机", "看板"):
		return CategoryProductUI
	case containsAny(lower, "anime", "watercolor", "ghibli", "illustration", "gouache", "ukiyo", "comic", "manga", "storybook", "cel shading", "sketch", "插画", "水彩", "二次元", "动漫", "绘本"):
		return CategoryIllustrationAnime
	default:
		return CategoryPhotography
	}
}

func containsAny(s string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

func normalizeSize(size string) string {
	trimmed := strings.TrimSpace(size)
	if validSizes[trimmed] {
		return trimmed
	}
	return "1024x1024"
}

func normalizeMode(mode, category string) string {
	if strings.EqualFold(strings.TrimSpace(mode), "edits") || category == CategoryReferenceEdit {
		return "edits"
	}
	return "generations"
}

func normalizeModel(model, category string) string {
	trimmed := strings.TrimSpace(model)
	if validModels[trimmed] {
		return trimmed
	}
	switch category {
	case CategoryReferenceEdit, CategoryIllustrationAnime:
		return "gpt-image-2.5"
	case CategoryProductUI:
		return "gpt-image-2.6"
	default:
		return "gpt-image-2.5-sunburst"
	}
}

func computeScore(stats PromptStats, source string) float64 {
	if stats.Score > 0 {
		return math.Round(stats.Score*10) / 10
	}
	weighted := float64(stats.Likes) + float64(stats.Hearts)*2.2
	base := 72.0
	switch source {
	case SourceSeed:
		base = 86.0
	case SourceAISearch:
		base = 91.0
	case SourceCivitai:
		base = 88.0
	case SourceLexica:
		base = 87.5
	case SourceHuggingFace:
		base = 87.0
	case SourceReddit:
		base = 86.5
	}
	score := base + math.Log1p(math.Max(0, weighted))*1.75
	if score > 99.5 {
		score = 99.5
	}
	return math.Round(score*10) / 10
}

func normalizePromptItem(item PromptItem, now string) PromptItem {
	cat := normalizeCategory(item.Category, item.PromptEN+" "+item.TitleZH)
	item.Category = cat
	item.RecommendedMode = normalizeMode(item.RecommendedMode, cat)
	item.RecommendedSize = normalizeSize(item.RecommendedSize)
	item.RecommendedModel = normalizeModel(item.RecommendedModel, cat)
	if strings.TrimSpace(item.PreviewURL) == "" {
		item.PreviewURL = defaultCategoryPreviewURL(cat, item.ID)
	}
	if strings.TrimSpace(item.UpdatedAt) == "" {
		item.UpdatedAt = now
	}
	item.Stats.Score = computeScore(item.Stats, item.Source)
	return item
}

func cleanRawPrompt(raw string) string {
	cleaned := reLoraTag.ReplaceAllString(raw, " ")
	cleaned = reWeightParens.ReplaceAllString(cleaned, "$1")

	boilerplate := map[string]bool{
		"masterpiece":       true,
		"best quality":      true,
		"high quality":      true,
		"ultra-detailed":    true,
		"ultra detailed":    true,
		"extremely detailed": true,
		"absurdres":         true,
		"highres":           true,
		"8k":                true,
		"4k":                true,
		"uhd":               true,
		"hdr":               true,
		"nsfw":              true,
	}

	parts := strings.Split(cleaned, ",")
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		token := strings.TrimSpace(p)
		if token == "" {
			continue
		}
		if boilerplate[strings.ToLower(token)] {
			continue
		}
		kept = append(kept, token)
	}
	joined := strings.Join(kept, ", ")
	joined = reMultiComma.ReplaceAllString(joined, ", ")
	joined = reMultiSpace.ReplaceAllString(joined, " ")
	joined = strings.TrimSpace(strings.Trim(joined, ","))
	if joined == "" {
		return strings.TrimSpace(raw)
	}
	return joined
}

func defaultTitleForCategory(cat, source string) string {
	sourceLabel := map[string]string{
		SourceCivitai:     "Civitai 热榜",
		SourceLexica:      "Lexica 灵感",
		SourceReddit:      "Reddit 热帖",
		SourceHuggingFace: "开源数据集",
		SourceAISearch:    "AI 趋势",
	}[source]
	if sourceLabel == "" {
		sourceLabel = "精选"
	}
	switch cat {
	case Category3DMiniature:
		return sourceLabel + " · 3D 微缩场景"
	case CategoryPosterTypography:
		return sourceLabel + " · 创意排版海报"
	case CategoryIllustrationAnime:
		return sourceLabel + " · 风格插画"
	case CategoryProductUI:
		return sourceLabel + " · 产品工业设计"
	case CategoryReferenceEdit:
		return sourceLabel + " · 参考图重绘"
	default:
		return sourceLabel + " · 电影感摄影"
	}
}

func heuristicCurateItem(raw PromptItem) PromptItem {
	now := raw.UpdatedAt
	if now == "" {
		now = time.Now().UTC().Format(time.RFC3339)
	}
	cleanedEN := cleanRawPrompt(raw.PromptEN)
	cat := normalizeCategory(raw.Category, cleanedEN+" "+raw.TitleZH)

	if len(raw.Variables) == 0 {
		if !strings.Contains(cleanedEN, "[光影氛围]") {
			cleanedEN = strings.TrimRight(cleanedEN, ". ") + ", [光影氛围]"
		}
		raw.Variables = []PromptVariable{
			{
				Key:          "光影氛围",
				Label:        "光影与质感",
				DefaultValue: "soft cinematic studio lighting and rich micro-textures",
				Options: []string{
					"soft cinematic studio lighting and rich micro-textures",
					"golden hour volumetric sunbeams with warm rim light",
					"moody neon rain reflections and crisp highlights",
				},
			},
		}
	}

	raw.PromptEN = truncateString(cleanedEN, 680)
	if strings.TrimSpace(raw.TitleZH) == "" {
		raw.TitleZH = defaultTitleForCategory(cat, raw.Source)
	}
	if strings.TrimSpace(raw.PromptZH) == "" {
		raw.PromptZH = fmt.Sprintf("%s：以「%s」为视觉主题的高精细构图，融合[光影氛围]，呈现专业级色彩层次与细节质感。", cat, truncateString(cleanedEN, 120))
	}
	raw.Category = cat
	return normalizePromptItem(raw, now)
}

type aiCuratedEntry struct {
	ID               string           `json:"id"`
	TitleZH          string           `json:"title_zh"`
	Category         string           `json:"category"`
	PromptEN         string           `json:"prompt_en"`
	PromptZH         string           `json:"prompt_zh"`
	RecommendedMode  string           `json:"recommended_mode"`
	RecommendedModel string           `json:"recommended_model"`
	RecommendedSize  string           `json:"recommended_size"`
	Variables        []PromptVariable `json:"variables"`
}

func extractJSONBlock(raw string, openChar, closeChar byte) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)

	start := strings.IndexByte(trimmed, openChar)
	end := strings.LastIndexByte(trimmed, closeChar)
	if start >= 0 && end > start {
		return trimmed[start : end+1]
	}
	return trimmed
}

func curateAndSearchWithAI(ctx context.Context, exec PoolChatExecutor, rawItems []PromptItem) ([]PromptItem, []PromptItem, error) {
	if exec == nil {
		curated := make([]PromptItem, 0, len(rawItems))
		for _, item := range rawItems {
			curated = append(curated, heuristicCurateItem(item))
		}
		return curated, nil, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// 并发执行：1) 号池 AI 策展清洗（前 8 条）与 2) 联网搜索发现最新趋势，缩短总同步等待时间。
	curatedMap := make(map[string]aiCuratedEntry)
	var aiSearchItems []PromptItem
	var curateErrStr, searchErrStr string

	var wg sync.WaitGroup
	if len(rawItems) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			batchLimit := len(rawItems)
			if batchLimit > 8 {
				batchLimit = 8
			}
			type rawCandidate struct {
				ID     string `json:"id"`
				Source string `json:"source"`
				Prompt string `json:"prompt"`
			}
			candidates := make([]rawCandidate, 0, batchLimit)
			for i := 0; i < batchLimit; i++ {
				candidates = append(candidates, rawCandidate{
					ID:     rawItems[i].ID,
					Source: rawItems[i].Source,
					Prompt: rawItems[i].PromptEN,
				})
			}
			candJSON, _ := json.Marshal(candidates)
			sysPrompt := "你是资深 AI 图像提示词策展人。请把原始生图提示词改写为适合 gpt-image-2.5 的自然语言双语模板，严格返回 JSON 数组（不要输出任何额外解释）。每项字段：id, title_zh(<=16字), category(必须为: 摄影写实/3D 与微缩/海报与文字排版/插画与二次元/产品与 UI/参考图编辑), prompt_en, prompt_zh, recommended_mode(generations/edits), recommended_model(gpt-image-2.5-sunburst/gpt-image-2.5-flare/gpt-image-2.5/gpt-image-2.6/dall-e-3), recommended_size(1024x1024/1792x1024/1024x1792), variables(1~2个含[key]占位符的可替换槽位，含 key/label/default_value/options)。"
			userPrompt := "请清洗并结构化以下候选条目，务必保留原始 id：\n" + string(candJSON)

			aiCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
			respText, err := exec(aiCtx, sysPrompt, userPrompt, false)
			cancel()
			if err != nil {
				curateErrStr = "ai_curate: " + err.Error()
				return
			}
			var parsed []aiCuratedEntry
			if jErr := json.Unmarshal([]byte(extractJSONBlock(respText, '[', ']')), &parsed); jErr != nil {
				curateErrStr = "ai_curate_json: " + jErr.Error()
				return
			}
			for _, entry := range parsed {
				if entry.ID != "" && entry.PromptEN != "" {
					curatedMap[entry.ID] = entry
				}
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		searchSys := "你是 AI 生图趋势研究员。请结合最新联网搜索结果，总结当前海外/中文社区评价最高、最出片的 3 个新潮生图玩法（如微缩胶囊、3D 键帽、撕拉片、材质重塑、字体海报等），严格返回 JSON 数组（不要输出额外文字）。每项字段：title_zh, category(摄影写实/3D 与微缩/海报与文字排版/插画与二次元/产品与 UI/参考图编辑), prompt_en, prompt_zh, recommended_mode, recommended_model, recommended_size, variables(含 key/label/default_value/options)。"
		searchUser := "请联网搜索并生成 3 条最新的高好评生图提示词模板，确保 prompt_en 和 prompt_zh 中都带有形如 [主体] 的可替换变量占位符。"

		searchCtx, cancelSearch := context.WithTimeout(ctx, 25*time.Second)
		searchText, searchErr := exec(searchCtx, searchSys, searchUser, true)
		cancelSearch()

		if searchErr != nil {
			searchErrStr = "ai_search: " + searchErr.Error()
			return
		}
		var parsedSearch []aiCuratedEntry
		if jErr := json.Unmarshal([]byte(extractJSONBlock(searchText, '[', ']')), &parsedSearch); jErr != nil {
			searchErrStr = "ai_search_json: " + jErr.Error()
			return
		}
		for idx, entry := range parsedSearch {
			if strings.TrimSpace(entry.PromptEN) == "" {
				continue
			}
			id := strings.TrimSpace(entry.ID)
			if id == "" {
				id = fmt.Sprintf("ai-search-%d-%d", time.Now().Unix(), idx+1)
			} else if !strings.HasPrefix(id, "ai-search-") {
				id = "ai-search-" + id
			}
			item := PromptItem{
				ID:               id,
				TitleZH:          entry.TitleZH,
				Category:         entry.Category,
				PromptEN:         entry.PromptEN,
				PromptZH:         entry.PromptZH,
				RecommendedMode:  entry.RecommendedMode,
				RecommendedModel: entry.RecommendedModel,
				RecommendedSize:  entry.RecommendedSize,
				Variables:        entry.Variables,
				Source:           SourceAISearch,
				Stats: PromptStats{
					Likes:  180 - idx*18,
					Hearts: 85 - idx*10,
				},
				UpdatedAt: now,
			}
			aiSearchItems = append(aiSearchItems, heuristicCurateItem(item))
		}
	}()

	wg.Wait()

	curatedItems := make([]PromptItem, 0, len(rawItems))
	for _, raw := range rawItems {
		if entry, ok := curatedMap[raw.ID]; ok {
			merged := raw
			merged.TitleZH = entry.TitleZH
			merged.Category = entry.Category
			merged.PromptEN = entry.PromptEN
			merged.PromptZH = entry.PromptZH
			merged.RecommendedMode = entry.RecommendedMode
			merged.RecommendedModel = entry.RecommendedModel
			if entry.RecommendedSize != "" {
				merged.RecommendedSize = entry.RecommendedSize
			}
			merged.Variables = entry.Variables
			merged.UpdatedAt = now
			curatedItems = append(curatedItems, normalizePromptItem(merged, now))
			continue
		}
		curatedItems = append(curatedItems, heuristicCurateItem(raw))
	}

	var errs []string
	if curateErrStr != "" {
		errs = append(errs, curateErrStr)
	}
	if searchErrStr != "" {
		errs = append(errs, searchErrStr)
	}

	var combinedErr error
	if len(errs) > 0 {
		combinedErr = errors.New(strings.Join(errs, "; "))
	}
	return curatedItems, aiSearchItems, combinedErr
}

// ExpandIdea 调用本机 ChatGPT 号池将简短想法扩写为结构化中英双语生图 Prompt（不可用时自动优雅降级）。
func ExpandIdea(ctx context.Context, exec PoolChatExecutor, req ExpandRequest) (ExpandResponse, error) {
	idea := strings.TrimSpace(req.Idea)
	if idea == "" {
		return ExpandResponse{}, errors.New("idea cannot be empty")
	}
	req.Mode = normalizeMode(req.Mode, "")
	req.Size = normalizeSize(req.Size)

	if exec != nil {
		sysPrompt := "你是顶级 AI 图像提示词工程师。请将用户的简短创意扩写为适合 gpt-image-2.5 的高精细结构化生图提示词，严格返回单个 JSON 对象（不要包含 Markdown 说明）：{title_zh, category(摄影写实/3D 与微缩/海报与文字排版/插画与二次元/产品与 UI/参考图编辑), prompt_en, prompt_zh, recommended_mode, recommended_model, recommended_size, variables:[{key,label,default_value,options}] }。要求在 prompt_en 和 prompt_zh 中嵌入 1~2 个形如 [光影材质] 的变量占位符，便于用户在面板快速微调。"
		userPrompt := fmt.Sprintf("创意想法：%s\n目标模式：%s\n目标画幅：%s\n请输出 JSON：", idea, req.Mode, req.Size)

		aiCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
		respText, err := exec(aiCtx, sysPrompt, userPrompt, false)
		cancel()

		if err == nil {
			var entry aiCuratedEntry
			if jErr := json.Unmarshal([]byte(extractJSONBlock(respText, '{', '}')), &entry); jErr == nil && strings.TrimSpace(entry.PromptEN) != "" {
				cat := normalizeCategory(entry.Category, entry.PromptEN+" "+idea)
				mode := normalizeMode(firstNonBlank(entry.RecommendedMode, req.Mode), cat)
				size := normalizeSize(firstNonBlank(entry.RecommendedSize, req.Size))
				model := normalizeModel(entry.RecommendedModel, cat)
				title := strings.TrimSpace(entry.TitleZH)
				if title == "" {
					title = truncateString(idea, 16)
				}
				promptZH := strings.TrimSpace(entry.PromptZH)
				if promptZH == "" {
					promptZH = idea
				}
				return ExpandResponse{
					TitleZH:          title,
					Category:         cat,
					PromptEN:         strings.TrimSpace(entry.PromptEN),
					PromptZH:         promptZH,
					RecommendedMode:  mode,
					RecommendedModel: model,
					RecommendedSize:  size,
					Variables:        entry.Variables,
					Source:           "ai_pool",
				}, nil
			}
		}
	}

	return smartFallbackExpand(idea, req.Mode, req.Size), nil
}

func smartFallbackExpand(idea, mode, size string) ExpandResponse {
	cat := inferCategory(idea)
	if mode == "edits" {
		cat = CategoryReferenceEdit
	}
	model := normalizeModel("", cat)

	var styleClauseEN, styleClauseZH string
	switch cat {
	case Category3DMiniature:
		styleClauseEN = "crafted as a hyper-detailed 3D miniature diorama, tactile clay and translucent resin textures, macro tilt-shift depth of field"
		styleClauseZH = "超精细 3D 微缩造景质感，细腻树脂与粘土纹理，微距移轴浅景深"
	case CategoryPosterTypography:
		styleClauseEN = "composed as an editorial graphic design poster with crisp Swiss grid hierarchy, tactile risograph paper grain"
		styleClauseZH = "杂志级平面海报构图，瑞士网格视觉层级，孔版印刷纸张纤维肌理"
	case CategoryIllustrationAnime:
		styleClauseEN = "illustrated in rich painterly cel-shading and delicate watercolor washes, expressive atmospheric brushwork"
		styleClauseZH = "细腻厚涂赛璐璐与水彩晕染插画笔触，富有表现力的光色层次"
	case CategoryProductUI:
		styleClauseEN = "captured as a high-craft commercial industrial design studio showcase, precision chamfered surfaces and subtle reflections"
		styleClauseZH = "顶级商业工业设计棚拍大片质感，精密倒角材质与克制高光反射"
	case CategoryReferenceEdit:
		styleClauseEN = "preserving exact subject geometry and composition from the reference image while seamlessly restyling textures and atmosphere"
		styleClauseZH = "严格保留参考图主体轮廓与构图透视，自然重塑画面质感与环境光影"
	default:
		styleClauseEN = "photographed on 35mm medium format lens with natural depth of field, authentic filmic color grading"
		styleClauseZH = "中画幅 35mm 镜头纪实摄影质感，自然光学景深与高级胶片影调"
	}

	vars := []PromptVariable{
		{
			Key:          "主体",
			Label:        "核心画面主体",
			DefaultValue: idea,
			Options: []string{
				idea,
				idea + " · 极简特写构图",
				idea + " · 全景故事感视角",
			},
		},
		{
			Key:          "光影材质",
			Label:        "光线与材质氛围",
			DefaultValue: "warm golden hour rim lighting and soft volumetric mist",
			Options: []string{
				"warm golden hour rim lighting and soft volumetric mist",
				"crisp studio softbox highlights on matte surfaces",
				"cinematic neon rain reflections at twilight",
			},
		},
	}

	return ExpandResponse{
		TitleZH:          truncateString(idea, 16),
		Category:         cat,
		PromptEN:         fmt.Sprintf("[主体], %s, [光影材质], ultra-clean composition and fine details", styleClauseEN),
		PromptZH:         fmt.Sprintf("[主体]，%s，搭配[光影材质]，构图平衡且细节丰富", styleClauseZH),
		RecommendedMode:  mode,
		RecommendedModel: model,
		RecommendedSize:  size,
		Variables:        vars,
		Source:           "smart_fallback",
	}
}

func firstNonBlank(vals ...string) string {
	for _, v := range vals {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
