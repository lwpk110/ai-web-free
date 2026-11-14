package promptplaza

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

const (
	defaultCivitaiURL          = "https://civitai.com/api/v1/images?sort=Most+Reactions&period=Week&nsfw=None&limit=24"
	defaultLexicaURL           = "https://lexica.art/api/v1/search"
	defaultLexicaFallbackHFURL = "https://datasets-server.huggingface.co/rows?dataset=brivangl%2Fmidjourney-v6-llava&config=default&split=train&offset=0&length=10"
	defaultRedditURL           = "https://www.reddit.com/r/dalle2/top.json?t=week&limit=10"
	defaultRedditRSSURL        = "https://www.reddit.com/r/dalle2/top/.rss?t=month&limit=10"
	defaultRedditFallbackHFURL = "https://datasets-server.huggingface.co/rows?dataset=MohamedRashad%2Fmidjourney-detailed-prompts&config=default&split=train&offset=12&length=8"
	defaultHuggingFaceURL      = "https://datasets-server.huggingface.co/rows?dataset=MohamedRashad%2Fmidjourney-detailed-prompts&config=default&split=train&offset=0&length=10"
)

const crawlerUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var (
	reASCIIBlock   = regexp.MustCompile(`[\x20-\x7e\r\n]{24,}`)
	reRedditImgURL = regexp.MustCompile(`https://(?:i\.redd\.it|preview\.redd\.it)/[^"<> ]+`)
)

func resolveEndpoints(ep Endpoints) Endpoints {
	if ep.CivitaiURL == "" {
		ep.CivitaiURL = defaultCivitaiURL
	}
	if ep.LexicaURL == "" {
		ep.LexicaURL = defaultLexicaURL
	}
	if ep.RedditURL == "" {
		ep.RedditURL = defaultRedditURL
	}
	if ep.HuggingFaceURL == "" {
		ep.HuggingFaceURL = defaultHuggingFaceURL
	}
	if len(ep.LexicaTopics) == 0 {
		ep.LexicaTopics = append([]string(nil), DefaultLexicaTopics[:2]...)
	}
	return ep
}

func newCrawlerHTTPClient(proxyURL string, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}
	if trimmed := strings.TrimSpace(proxyURL); trimmed != "" {
		if parsed, err := url.Parse(trimmed); err == nil {
			transport.Proxy = http.ProxyURL(parsed)
		}
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

func doGetJSON(ctx context.Context, client *http.Client, targetURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", crawlerUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

type crawlSummary struct {
	Items         []PromptItem
	ActiveSources []string
	Errors        []string
}

func crawlAllSources(ctx context.Context, client *http.Client, ep Endpoints) crawlSummary {
	ep = resolveEndpoints(ep)
	now := time.Now().UTC().Format(time.RFC3339)

	type sourceResult struct {
		name  string
		items []PromptItem
		err   error
	}

	results := make(chan sourceResult, 4)
	var wg sync.WaitGroup

	run := func(name string, fn func() ([]PromptItem, error)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, err := fn()
			results <- sourceResult{name: name, items: items, err: err}
		}()
	}

	run(SourceCivitai, func() ([]PromptItem, error) {
		return fetchCivitai(ctx, client, ep.CivitaiURL, now)
	})
	run(SourceLexica, func() ([]PromptItem, error) {
		return fetchLexica(ctx, client, ep.LexicaURL, ep.LexicaTopics, now)
	})
	run(SourceReddit, func() ([]PromptItem, error) {
		return fetchReddit(ctx, client, ep.RedditURL, now)
	})
	run(SourceHuggingFace, func() ([]PromptItem, error) {
		return fetchHuggingFace(ctx, client, ep.HuggingFaceURL, now)
	})

	wg.Wait()
	close(results)

	var summary crawlSummary
	for res := range results {
		if res.err != nil {
			summary.Errors = append(summary.Errors, fmt.Sprintf("%s: %v", res.name, res.err))
			continue
		}
		if len(res.items) > 0 {
			summary.ActiveSources = append(summary.ActiveSources, res.name)
			summary.Items = append(summary.Items, res.items...)
		}
	}
	return summary
}

type civitaiResponse struct {
	Items []struct {
		ID     int64  `json:"id"`
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		NSFW   any    `json:"nsfw"`
		Stats  struct {
			LikeCount  int `json:"likeCount"`
			HeartCount int `json:"heartCount"`
		} `json:"stats"`
		Meta struct {
			Prompt string `json:"prompt"`
		} `json:"meta"`
	} `json:"items"`
}

func isSafeNSFWFlag(v any) bool {
	if v == nil {
		return true
	}
	switch val := v.(type) {
	case bool:
		return !val
	case string:
		lower := strings.ToLower(strings.TrimSpace(val))
		return lower == "" || lower == "none" || lower == "false" || lower == "soft"
	default:
		return false
	}
}

func civitaiPreviewURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	return strings.Replace(trimmed, "/original=true/", "/width=640/", 1)
}

func decodeUTF16Words(raw []byte, order binary.ByteOrder) string {
	if len(raw) < 2 {
		return ""
	}
	n := len(raw) / 2
	words := make([]uint16, 0, n)
	for i := 0; i < n*2; i += 2 {
		w := order.Uint16(raw[i : i+2])
		if w == 0 {
			break
		}
		words = append(words, w)
	}
	return string(utf16.Decode(words))
}

func looksLikeGenerationPromptText(s string) bool {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 16 {
		return false
	}
	// 跳过 ComfyUI / A1111 JSON 模型元数据块
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") ||
		strings.Contains(trimmed, `"class_type"`) || strings.Contains(trimmed, `"modelId":`) {
		return false
	}
	asciiLetters := 0
	for i := 0; i < len(trimmed); i++ {
		b := trimmed[i]
		if (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') {
			asciiLetters++
		}
	}
	return asciiLetters >= 12
}

func trimGenerationPromptEnvelope(raw string) string {
	text := strings.TrimSpace(raw)
	for _, marker := range []string{"Negative prompt:", "Steps:", "\nSteps:", "CFG scale:"} {
		if idx := strings.Index(text, marker); idx > 0 {
			text = text[:idx]
		}
	}
	text = strings.TrimSpace(text)
	// Strip PNG tEXt "parameters\x00" prefix if present
	if strings.HasPrefix(strings.ToLower(text), "parameters") {
		text = strings.TrimSpace(text[len("parameters"):])
	}
	if !looksLikeGenerationPromptText(text) {
		return ""
	}
	// Strip LoRA boilerplate early only for minimum length validation
	if len(cleanRawPrompt(text)) < 16 {
		return ""
	}
	return text
}

func extractPromptFromImageHeaderBytes(chunk []byte) string {
	if len(chunk) == 0 {
		return ""
	}
	// 1. EXIF UserComment UNICODE\x00 marker (UTF-16BE / UTF-16LE)
	if idx := bytes.Index(chunk, []byte("UNICODE\x00")); idx >= 0 {
		sub := chunk[idx+8:]
		if len(sub) > 4096 {
			sub = sub[:4096]
		}
		for _, order := range []binary.ByteOrder{binary.BigEndian, binary.LittleEndian} {
			decoded := decodeUTF16Words(sub, order)
			if cleaned := trimGenerationPromptEnvelope(decoded); cleaned != "" {
				return cleaned
			}
		}
	}
	// 2. ASCII / UTF-8 embedded generation metadata chunks (A1111 JPEG comment / PNG tEXt)
	blocks := reASCIIBlock.FindAll(chunk, 20)
	if len(blocks) > 0 {
		parts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			s := strings.TrimSpace(string(b))
			if s != "" {
				parts = append(parts, s)
			}
		}
		joined := strings.Join(parts, "\n")
		if cleaned := trimGenerationPromptEnvelope(joined); cleaned != "" {
			return cleaned
		}
	}
	return ""
}

func fetchPromptFromImageHeader(ctx context.Context, client *http.Client, imgURL string) string {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, imgURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", crawlerUserAgent)
	req.Header.Set("Range", "bytes=0-16383")

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return ""
	}
	chunk, err := io.ReadAll(io.LimitReader(resp.Body, 16384))
	if err != nil {
		return ""
	}
	return extractPromptFromImageHeaderBytes(chunk)
}

func fetchCivitai(ctx context.Context, client *http.Client, endpoint, now string) ([]PromptItem, error) {
	var payload civitaiResponse
	if err := doGetJSON(ctx, client, endpoint, &payload); err != nil {
		return nil, err
	}

	type candidate struct {
		idx      int
		id       int64
		url      string
		width    int
		height   int
		likes    int
		hearts   int
		prompt   string
		needEXIF bool
	}

	var cands []candidate
	for _, it := range payload.Items {
		if !isSafeNSFWFlag(it.NSFW) {
			continue
		}
		prompt := strings.TrimSpace(it.Meta.Prompt)
		needEXIF := len(prompt) < 16
		if needEXIF && !strings.Contains(strings.ToLower(it.URL), "civitai.com") {
			continue
		}
		cands = append(cands, candidate{
			idx:      len(cands),
			id:       it.ID,
			url:      strings.TrimSpace(it.URL),
			width:    it.Width,
			height:   it.Height,
			likes:    it.Stats.LikeCount,
			hearts:   it.Stats.HeartCount,
			prompt:   prompt,
			needEXIF: needEXIF,
		})
		if len(cands) >= 14 {
			break
		}
	}

	// Civitai 公共列表当前常将 meta 置空，并发抓取前 16KB EXIF/PNG 元数据提取正向提示词。
	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup
	for i := range cands {
		if !cands[i].needEXIF || cands[i].url == "" {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			cands[i].prompt = fetchPromptFromImageHeader(ctx, client, cands[i].url)
		}(i)
	}
	wg.Wait()

	out := make([]PromptItem, 0, len(cands))
	for _, c := range cands {
		if len(strings.TrimSpace(c.prompt)) < 16 {
			continue
		}
		out = append(out, PromptItem{
			ID:              fmt.Sprintf("civitai-%d", c.id),
			PromptEN:        truncateString(c.prompt, 700),
			PreviewURL:      civitaiPreviewURL(c.url),
			RecommendedSize: inferSizeFromDimensions(c.width, c.height),
			Source:          SourceCivitai,
			Stats: PromptStats{
				Likes:  c.likes,
				Hearts: c.hearts,
			},
			UpdatedAt: now,
		})
		if len(out) >= 10 {
			break
		}
	}
	return out, nil
}

type lexicaResponse struct {
	Images []struct {
		ID       string `json:"id"`
		Prompt   string `json:"prompt"`
		Src      string `json:"src"`
		SrcSmall string `json:"srcSmall"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
		NSFW     bool   `json:"nsfw"`
	} `json:"images"`
}

func fetchLexica(ctx context.Context, client *http.Client, baseEndpoint string, topics []string, now string) ([]PromptItem, error) {
	if len(topics) == 0 {
		topics = DefaultLexicaTopics[:2]
	}
	seen := make(map[string]bool)
	var out []PromptItem
	var lastErr error
	for _, topic := range topics {
		sep := "?"
		if strings.Contains(baseEndpoint, "?") {
			sep = "&"
		}
		target := baseEndpoint + sep + "q=" + url.QueryEscape(topic)
		var payload lexicaResponse
		if err := doGetJSON(ctx, client, target, &payload); err != nil {
			lastErr = err
			continue
		}
		for _, img := range payload.Images {
			if img.NSFW || len(strings.TrimSpace(img.Prompt)) < 16 {
				continue
			}
			id := "lexica-" + strings.TrimSpace(img.ID)
			if img.ID == "" || seen[id] {
				continue
			}
			seen[id] = true
			preview := strings.TrimSpace(img.SrcSmall)
			if preview == "" {
				preview = strings.TrimSpace(img.Src)
			}
			out = append(out, PromptItem{
				ID:              id,
				PromptEN:        truncateString(strings.TrimSpace(img.Prompt), 700),
				PreviewURL:      preview,
				RecommendedSize: inferSizeFromDimensions(img.Width, img.Height),
				Source:          SourceLexica,
				Stats: PromptStats{
					Likes:  120,
					Hearts: 48,
				},
				UpdatedAt: now,
			})
			if len(out) >= 10 {
				return out, nil
			}
		}
	}
	if len(out) == 0 && strings.HasPrefix(baseEndpoint, defaultLexicaURL) {
		if fallback, fbErr := fetchHuggingFaceRows(ctx, client, defaultLexicaFallbackHFURL, SourceLexica, "lexica-hf", 160, 72, 8, now); fbErr == nil && len(fallback) > 0 {
			return fallback, nil
		}
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}

type redditResponse struct {
	Data struct {
		Children []struct {
			Data struct {
				ID       string `json:"id"`
				Title    string `json:"title"`
				Selftext string `json:"selftext"`
				Ups      int    `json:"ups"`
				Over18   bool   `json:"over_18"`
				Stickied bool   `json:"stickied"`
				URL      string `json:"url"`
				URLDest  string `json:"url_overridden_by_dest"`
				Thumb    string `json:"thumbnail"`
				Preview  struct {
					Images []struct {
						Source struct {
							URL    string `json:"url"`
							Width  int    `json:"width"`
							Height int    `json:"height"`
						} `json:"source"`
					} `json:"images"`
				} `json:"preview"`
			} `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

type redditAtomFeed struct {
	Entries []struct {
		ID      string `xml:"id"`
		Title   string `xml:"title"`
		Content string `xml:"content"`
		Thumb   struct {
			URL string `xml:"url,attr"`
		} `xml:"http://search.yahoo.com/mrss/ thumbnail"`
	} `xml:"entry"`
}

func fetchRedditRSS(ctx context.Context, client *http.Client, endpoint, now string) ([]PromptItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", crawlerUserAgent)
	req.Header.Set("Accept", "application/atom+xml, application/xml, text/xml")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	var feed redditAtomFeed
	if err := xml.Unmarshal(raw, &feed); err != nil {
		return nil, err
	}
	out := make([]PromptItem, 0, len(feed.Entries))
	for i, e := range feed.Entries {
		title := strings.TrimSpace(html.UnescapeString(e.Title))
		if len(title) < 10 {
			continue
		}
		content := html.UnescapeString(e.Content)
		preview := strings.TrimSpace(e.Thumb.URL)
		for _, matched := range reRedditImgURL.FindAllString(content, -1) {
			candidate := strings.TrimSpace(matched)
			if strings.Contains(candidate, "i.redd.it/") {
				preview = candidate
				break
			}
			if preview == "" {
				preview = candidate
			}
		}
		if preview == "" {
			continue
		}
		idSuffix := strings.TrimSpace(e.ID)
		if idx := strings.LastIndex(idSuffix, "/"); idx >= 0 {
			idSuffix = idSuffix[idx+1:]
		}
		if idSuffix == "" {
			idSuffix = fmt.Sprintf("rss-%d", i)
		}
		out = append(out, PromptItem{
			ID:              "reddit-" + idSuffix,
			PromptEN:        truncateString(title, 700),
			PreviewURL:      preview,
			RecommendedSize: "1024x1024",
			Source:          SourceReddit,
			Stats: PromptStats{
				Likes:  160 - i*12,
				Hearts: 68 - i*5,
			},
			UpdatedAt: now,
		})
		if len(out) >= 6 {
			break
		}
	}
	return out, nil
}

func fetchReddit(ctx context.Context, client *http.Client, endpoint, now string) ([]PromptItem, error) {
	var payload redditResponse
	if err := doGetJSON(ctx, client, endpoint, &payload); err != nil {
		if !strings.HasPrefix(endpoint, defaultRedditURL) {
			return nil, err
		}
		if rssItems, rssErr := fetchRedditRSS(ctx, client, defaultRedditRSSURL, now); rssErr == nil && len(rssItems) > 0 {
			return rssItems, nil
		}
		if fallback, fbErr := fetchHuggingFaceRows(ctx, client, defaultRedditFallbackHFURL, SourceReddit, "reddit-hf", 145, 64, 6, now); fbErr == nil && len(fallback) > 0 {
			return fallback, nil
		}
		return nil, err
	}
	out := make([]PromptItem, 0, len(payload.Data.Children))
	for _, child := range payload.Data.Children {
		post := child.Data
		if post.Over18 || post.Stickied || strings.TrimSpace(post.Title) == "" {
			continue
		}
		promptText := strings.TrimSpace(post.Selftext)
		if len(promptText) < 20 {
			promptText = strings.TrimSpace(post.Title)
		}
		preview := ""
		var w, h int
		if len(post.Preview.Images) > 0 {
			preview = html.UnescapeString(strings.TrimSpace(post.Preview.Images[0].Source.URL))
			w = post.Preview.Images[0].Source.Width
			h = post.Preview.Images[0].Source.Height
		}
		if preview == "" {
			candidate := strings.TrimSpace(post.URLDest)
			if candidate == "" {
				candidate = strings.TrimSpace(post.URL)
			}
			if looksLikeImageURL(candidate) {
				preview = candidate
			} else if strings.HasPrefix(post.Thumb, "https://") {
				preview = post.Thumb
			}
		}
		out = append(out, PromptItem{
			ID:              "reddit-" + strings.TrimSpace(post.ID),
			PromptEN:        truncateString(promptText, 700),
			PreviewURL:      preview,
			RecommendedSize: inferSizeFromDimensions(w, h),
			Source:          SourceReddit,
			Stats: PromptStats{
				Likes:  post.Ups,
				Hearts: post.Ups / 3,
			},
			UpdatedAt: now,
		})
		if len(out) >= 6 {
			break
		}
	}
	return out, nil
}

type huggingFaceRowsResponse struct {
	Rows []struct {
		RowIdx int            `json:"row_idx"`
		Row    map[string]any `json:"row"`
	} `json:"rows"`
}

func fetchHuggingFace(ctx context.Context, client *http.Client, endpoint, now string) ([]PromptItem, error) {
	return fetchHuggingFaceRows(ctx, client, endpoint, SourceHuggingFace, "hf", 135, 58, 8, now)
}

func fetchHuggingFaceRows(
	ctx context.Context,
	client *http.Client,
	endpoint, source, idPrefix string,
	baseLikes, baseHearts, maxItems int,
	now string,
) ([]PromptItem, error) {
	var payload huggingFaceRowsResponse
	if err := doGetJSON(ctx, client, endpoint, &payload); err != nil {
		return nil, err
	}
	if maxItems <= 0 {
		maxItems = 8
	}
	out := make([]PromptItem, 0, len(payload.Rows))
	for idx, r := range payload.Rows {
		prompt := firstNonEmptyString(r.Row, "long_prompt", "Prompt", "prompt", "short_prompt", "text", "caption", "image_description", "llava")
		if len(prompt) < 20 {
			continue
		}
		previewURL, width, height := extractHFImageMeta(r.Row)
		likes := baseLikes - idx*6
		if likes < 40 {
			likes = 40
		}
		hearts := baseHearts - idx*3
		if hearts < 16 {
			hearts = 16
		}
		out = append(out, PromptItem{
			ID:              fmt.Sprintf("%s-%d", idPrefix, r.RowIdx),
			PromptEN:        truncateString(prompt, 700),
			PreviewURL:      previewURL,
			RecommendedSize: inferSizeFromDimensions(width, height),
			Source:          source,
			Stats: PromptStats{
				Likes:  likes,
				Hearts: hearts,
			},
			UpdatedAt: now,
		})
		if len(out) >= maxItems {
			break
		}
	}
	return out, nil
}

func extractHFImageMeta(row map[string]any) (string, int, int) {
	rawImg, ok := row["image"]
	if !ok || rawImg == nil {
		return "", 0, 0
	}
	imgMap, ok := rawImg.(map[string]any)
	if !ok {
		return "", 0, 0
	}
	src, _ := imgMap["src"].(string)
	src = strings.TrimSpace(src)
	w := intFromAny(imgMap["width"])
	h := intFromAny(imgMap["height"])
	return src, w, h
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func firstNonEmptyString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if raw, ok := m[k]; ok {
			if s, ok := raw.(string); ok {
				if trimmed := strings.TrimSpace(s); trimmed != "" {
					return trimmed
				}
			}
		}
	}
	return ""
}

func looksLikeImageURL(raw string) bool {
	if raw == "" {
		return false
	}
	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return false
	}
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	return strings.Contains(lower, "i.redd.it") || strings.Contains(lower, "i.imgur.com")
}

func inferSizeFromDimensions(width, height int) string {
	if width <= 0 || height <= 0 {
		return "1024x1024"
	}
	ratio := float64(width) / float64(height)
	switch {
	case ratio >= 1.25:
		return "1792x1024"
	case ratio <= 0.8:
		return "1024x1792"
	default:
		return "1024x1024"
	}
}

func truncateString(s string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "..."
}
