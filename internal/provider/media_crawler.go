package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
)

// CrawledMediaCandidate 描述通过网页爬虫或公开媒体检索发现的候选素材（音频样音 / 参考图像 / 视频片段）。
type CrawledMediaCandidate struct {
	ID            string   `json:"id"`
	MediaKind     string   `json:"media_kind"` // "voice" | "image" | "video"
	Title         string   `json:"title"`
	MediaURL      string   `json:"media_url"`
	SourcePage    string   `json:"source_page,omitempty"`
	SourceDomain  string   `json:"source_domain,omitempty"`
	MimeType      string   `json:"mime_type,omitempty"`
	SizeBytes     int64    `json:"size_bytes,omitempty"`
	Transcript    string   `json:"transcript,omitempty"`
	Gender        string   `json:"gender,omitempty"`
	Locale        string   `json:"locale,omitempty"`
	SuggestedRole string   `json:"suggested_role,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

// MediaCrawlOptions 控制爬虫行为（默认拒绝内网/回环地址以防 SSRF）。
type MediaCrawlOptions struct {
	Kind               string
	Limit              int
	AllowPrivateTest   bool
	Client             *http.Client
	WikimediaSearchURL string
	WebSearchURL       string
	FreesoundSearchURL string
}

// MediaCrawlReport 汇总检索结果与说明信息。
type MediaCrawlReport struct {
	Query      string                  `json:"query"`
	TargetURL  string                  `json:"target_url,omitempty"`
	MediaKind  string                  `json:"media_kind"`
	Candidates []CrawledMediaCandidate `json:"candidates"`
	Warnings   []string                `json:"warnings,omitempty"`
}

const (
	defaultCrawlerWikimediaAPI  = "https://commons.wikimedia.org/w/api.php"
	defaultCrawlerWebSearch     = "https://cn.bing.com/search"
	defaultCrawlerDDGSearch     = "https://html.duckduckgo.com/html/"
	defaultCrawlerFreesoundHTML = "https://freesound.org/search/"
	maxCrawlHTMLBytes           = 2 << 20  // 2 MB HTML cap
	maxVoiceDownloadBytes       = 15 << 20 // 15 MB audio cap
	maxImageDownloadBytes       = 15 << 20 // 15 MB image cap
	maxVideoDownloadBytes       = 40 << 20 // 40 MB video cap
)

var (
	audioExtRE       = regexp.MustCompile(`(?i)\.(mp3|wav|ogg|oga|m4a|aac|flac|opus)(?:[?#]|$)`)
	imageExtRE       = regexp.MustCompile(`(?i)\.(jpe?g|png|webp|gif)(?:[?#]|$)`)
	videoExtRE       = regexp.MustCompile(`(?i)\.(mp4|webm|mov|m4v)(?:[?#]|$)`)
	inlineAudioURLRE = regexp.MustCompile(`(?i)(?:https?:)?//[^\s"'<>\\()]+?\.(?:mp3|wav|ogg|oga|m4a|aac|flac)(?:\?[^\s"'<>\\()]*)?`)
	whitespaceRE     = regexp.MustCompile(`\s+`)
)

// NormalizeCrawlerKind 规整检索媒体类型（"voice" | "image" | "video" | "auto"）。
func NormalizeCrawlerKind(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "voice", "audio", "tts", "sound":
		return "voice"
	case "image", "img", "picture", "portrait":
		return "image"
	case "video", "clip", "mp4":
		return "video"
	default:
		return "auto"
	}
}

// ValidatePublicMediaURL 校验目标 URL 为合法 HTTP(S) 协议且未指向内网/回环/元数据地址（防止 SSRF）。
func ValidatePublicMediaURL(rawURL string, allowPrivate bool) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, errors.New("url is empty")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported url scheme: %s", u.Scheme)
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return nil, errors.New("url missing host")
	}
	if allowPrivate {
		return u, nil
	}

	lowerHost := strings.ToLower(host)
	if lowerHost == "localhost" || strings.HasSuffix(lowerHost, ".local") || strings.HasSuffix(lowerHost, ".internal") {
		return nil, fmt.Errorf("blocked private hostname: %s", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateOrBlockedIP(ip) {
			return nil, fmt.Errorf("blocked non-public ip: %s", ip.String())
		}
		return u, nil
	}
	ips, lookupErr := net.LookupIP(host)
	if lookupErr == nil {
		for _, ip := range ips {
			if isPrivateOrBlockedIP(ip) {
				return nil, fmt.Errorf("blocked resolved private ip for host %s: %s", host, ip.String())
			}
		}
	}
	return u, nil
}

func isPrivateOrBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		// Carrier-grade NAT 100.64.0.0/10
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true
		}
	}
	return false
}

func defaultCrawlerHTTPClient(allowPrivate bool) *http.Client {
	return &http.Client{
		Timeout: 18 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			_, err := ValidatePublicMediaURL(req.URL.String(), allowPrivate)
			return err
		},
	}
}

func resolveCrawlerClient(opts MediaCrawlOptions) *http.Client {
	if opts.Client != nil {
		return opts.Client
	}
	return defaultCrawlerHTTPClient(opts.AllowPrivateTest)
}

func newCrawlerRequest(ctx context.Context, target string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	if strings.Contains(target, "wikimedia.org") {
		req.Header.Set("User-Agent", "AIWebFreeMediaCrawler/1.0 (https://github.com/ai-web-free)")
	} else {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json,audio/*,image/*,video/*;q=0.9,*/*;q=0.5")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	return req, nil
}

// SearchAndCrawlMedia 同时执行指定网页 URL 抓取与关键词公开媒体搜索，返回去重后的音色/图像/视频候选列表。
func SearchAndCrawlMedia(ctx context.Context, query, targetURL string, opts MediaCrawlOptions) MediaCrawlReport {
	kind := NormalizeCrawlerKind(opts.Kind)
	limit := opts.Limit
	if limit <= 0 {
		limit = 12
	}
	if limit > 30 {
		limit = 30
	}

	report := MediaCrawlReport{
		Query:     strings.TrimSpace(query),
		TargetURL: strings.TrimSpace(targetURL),
		MediaKind: kind,
	}

	seen := make(map[string]struct{})
	addCandidate := func(c CrawledMediaCandidate) {
		if strings.TrimSpace(c.MediaURL) == "" {
			return
		}
		if kind != "auto" && c.MediaKind != kind {
			return
		}
		if _, dup := seen[c.MediaURL]; dup {
			return
		}
		if len(report.Candidates) >= limit {
			return
		}
		seen[c.MediaURL] = struct{}{}
		if c.ID == "" {
			c.ID = fmt.Sprintf("crawl_%s_%d", c.MediaKind, len(report.Candidates)+1)
		}
		if c.SourceDomain == "" {
			if pu, err := url.Parse(c.MediaURL); err == nil {
				c.SourceDomain = pu.Hostname()
			}
		}
		report.Candidates = append(report.Candidates, c)
	}

	// 1. 直接抓取用户给出的网页或直链 URL
	if report.TargetURL != "" {
		directList, err := CrawlMediaURL(ctx, report.TargetURL, opts)
		if err != nil {
			report.Warnings = append(report.Warnings, "目标网页抓取失败: "+err.Error())
		} else {
			for _, item := range directList {
				addCandidate(item)
			}
		}
	}

	// 2. 若提供了关键词且仍需补充候选，并发执行多源公开媒体检索（Freesound 音色样音 + Wikimedia + 网页检索）
	if report.Query != "" && len(report.Candidates) < limit {
		var (
			fsItems, wikiItems, webItems []CrawledMediaCandidate
			fsErr, wikiErr, webErr       error
			wg                           sync.WaitGroup
		)

		runVoiceFreesound := kind == "voice" && (opts.FreesoundSearchURL != "" || (opts.WebSearchURL == "" && opts.WikimediaSearchURL == ""))
		if runVoiceFreesound {
			wg.Add(1)
			go func() {
				defer wg.Done()
				subCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
				defer cancel()
				fsItems, fsErr = searchFreesoundPreviews(subCtx, report.Query, limit, opts)
			}()
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			subCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			wikiItems, wikiErr = searchWikimediaCommons(subCtx, report.Query, kind, limit, opts)
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			subCtx, cancel := context.WithTimeout(ctx, 9*time.Second)
			defer cancel()
			webItems, webErr = searchWebAndCrawlPages(subCtx, report.Query, kind, limit, opts)
		}()

		wg.Wait()

		if fsErr != nil {
			report.Warnings = append(report.Warnings, "Freesound 音色检索提示: "+fsErr.Error())
		} else {
			for _, item := range fsItems {
				addCandidate(item)
			}
		}
		if wikiErr != nil {
			report.Warnings = append(report.Warnings, "Wikimedia 检索警告: "+wikiErr.Error())
		} else {
			for _, item := range wikiItems {
				addCandidate(item)
			}
		}
		if webErr != nil {
			report.Warnings = append(report.Warnings, "网页搜索扩展提示: "+webErr.Error())
		} else {
			for _, item := range webItems {
				addCandidate(item)
			}
		}
	}

	return report
}

// CrawlMediaURL 抓取单个网页或媒体直链并提取符合 kind 的音色/图像/视频资源。
func CrawlMediaURL(ctx context.Context, rawURL string, opts MediaCrawlOptions) ([]CrawledMediaCandidate, error) {
	parsedURL, err := ValidatePublicMediaURL(rawURL, opts.AllowPrivateTest)
	if err != nil {
		return nil, err
	}
	kind := NormalizeCrawlerKind(opts.Kind)
	limit := opts.Limit
	if limit <= 0 {
		limit = 12
	}

	if directKind, mime := detectKindFromExtension(parsedURL.String()); directKind != "" {
		if kind == "auto" || kind == directKind {
			title := cleanupMediaTitle(path.Base(parsedURL.Path))
			role, gender, tags := inferVoiceAttrsFromText(title)
			return []CrawledMediaCandidate{{
				MediaKind:     directKind,
				Title:         title,
				MediaURL:      parsedURL.String(),
				SourcePage:    parsedURL.String(),
				SourceDomain:  parsedURL.Hostname(),
				MimeType:      mime,
				Gender:        gender,
				Locale:        inferCrawlerLocale(title),
				SuggestedRole: role,
				Tags:          tags,
			}}, nil
		}
		return nil, nil
	}

	client := resolveCrawlerClient(opts)
	req, err := newCrawlerRequest(ctx, parsedURL.String())
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
	}

	ct := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if directKind := detectKindFromContentType(ct); directKind != "" {
		if kind != "auto" && kind != directKind {
			return nil, nil
		}
		title := cleanupMediaTitle(path.Base(parsedURL.Path))
		role, gender, tags := inferVoiceAttrsFromText(title)
		return []CrawledMediaCandidate{{
			MediaKind:     directKind,
			Title:         title,
			MediaURL:      resp.Request.URL.String(),
			SourcePage:    resp.Request.URL.String(),
			SourceDomain:  resp.Request.URL.Hostname(),
			MimeType:      stripMimeParams(ct),
			SizeBytes:     resp.ContentLength,
			Gender:        gender,
			Locale:        inferCrawlerLocale(title),
			SuggestedRole: role,
			Tags:          tags,
		}}, nil
	}

	htmlBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxCrawlHTMLBytes))
	if err != nil {
		return nil, err
	}
	return ExtractMediaFromHTML(resp.Request.URL, string(htmlBytes), kind, limit, opts.AllowPrivateTest), nil
}

// ExtractMediaFromHTML 从 HTML 文档中提取音频样音、图像参考图、视频片段及其上下文说明。
func ExtractMediaFromHTML(baseURL *url.URL, htmlBody, kind string, limit int, allowPrivate bool) []CrawledMediaCandidate {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlBody))
	if err != nil {
		return nil
	}
	pageTitle := strings.TrimSpace(doc.Find("title").First().Text())
	normalizedKind := NormalizeCrawlerKind(kind)
	if limit <= 0 {
		limit = 12
	}

	seen := make(map[string]struct{})
	out := make([]CrawledMediaCandidate, 0, limit)
	add := func(mediaKind, rawRef, titleHint, transcriptHint, mimeHint string) {
		if len(out) >= limit || rawRef == "" {
			return
		}
		if normalizedKind != "auto" && mediaKind != normalizedKind {
			return
		}
		absURL := resolveAbsoluteHTTPURL(baseURL, rawRef)
		if absURL == "" {
			return
		}
		if _, err := ValidatePublicMediaURL(absURL, allowPrivate); err != nil {
			return
		}
		if _, dup := seen[absURL]; dup {
			return
		}
		seen[absURL] = struct{}{}

		title := cleanText(titleHint)
		if title == "" {
			title = cleanupMediaTitle(path.Base(absURL))
		}
		if title == "" {
			title = cleanText(pageTitle)
		}
		transcript := cleanText(transcriptHint)
		if mediaKind == "voice" && transcript == "" {
			transcript = title
		}
		role, gender, tags := inferVoiceAttrsFromText(title + " " + transcript + " " + pageTitle)
		if mimeHint == "" {
			_, mimeHint = detectKindFromExtension(absURL)
		}
		out = append(out, CrawledMediaCandidate{
			MediaKind:     mediaKind,
			Title:         title,
			MediaURL:      absURL,
			SourcePage:    baseURL.String(),
			SourceDomain:  baseURL.Hostname(),
			MimeType:      mimeHint,
			Transcript:    truncateRuneStr(transcript, 140),
			Gender:        gender,
			Locale:        inferCrawlerLocale(title + " " + transcript),
			SuggestedRole: role,
			Tags:          tags,
		})
	}

	// OpenGraph Meta 提取
	doc.Find(`meta[property="og:audio"], meta[property="og:audio:url"], meta[property="og:audio:secure_url"]`).Each(func(_ int, s *goquery.Selection) {
		if val, ok := s.Attr("content"); ok {
			add("voice", val, pageTitle, "", "")
		}
	})
	doc.Find(`meta[property="og:image"], meta[property="og:image:url"], meta[name="twitter:image"]`).Each(func(_ int, s *goquery.Selection) {
		if val, ok := s.Attr("content"); ok {
			add("image", val, pageTitle, "", "")
		}
	})
	doc.Find(`meta[property="og:video"], meta[property="og:video:url"], meta[property="og:video:secure_url"]`).Each(func(_ int, s *goquery.Selection) {
		if val, ok := s.Attr("content"); ok {
			add("video", val, pageTitle, "", "")
		}
	})

	// <audio> / <source> 提取
	doc.Find("audio").Each(func(_ int, s *goquery.Selection) {
		ctxText := cleanText(s.Parent().Text())
		title := firstNonEmptyStr(s.AttrOr("title", ""), s.AttrOr("aria-label", ""), ctxText)
		if src, ok := s.Attr("src"); ok {
			add("voice", src, title, ctxText, "")
		}
		s.Find("source").Each(func(_ int, srcSel *goquery.Selection) {
			if src, ok := srcSel.Attr("src"); ok {
				add("voice", src, title, ctxText, srcSel.AttrOr("type", ""))
			}
		})
	})

	// <video> / <source> 提取
	doc.Find("video").Each(func(_ int, s *goquery.Selection) {
		ctxText := cleanText(s.Parent().Text())
		title := firstNonEmptyStr(s.AttrOr("title", ""), s.AttrOr("aria-label", ""), ctxText)
		if src, ok := s.Attr("src"); ok {
			add("video", src, title, "", "")
		}
		s.Find("source").Each(func(_ int, srcSel *goquery.Selection) {
			if src, ok := srcSel.Attr("src"); ok {
				add("video", src, title, "", srcSel.AttrOr("type", ""))
			}
		})
	})

	// <a> 直链提取 (.mp3/.wav/.ogg/.jpg/.png/.mp4 等)
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href := strings.TrimSpace(s.AttrOr("href", ""))
		k, mime := detectKindFromExtension(href)
		if k == "" {
			return
		}
		text := cleanText(firstNonEmptyStr(s.Text(), s.AttrOr("title", ""), s.AttrOr("download", "")))
		ctxText := cleanText(s.Parent().Text())
		add(k, href, text, ctxText, mime)
	})

	// data-* 音频/视频播放器属性提取（如 Freesound data-mp3 / data-title，各音频站点 data-audio / data-url）
	if normalizedKind == "auto" || normalizedKind == "voice" || normalizedKind == "video" {
		audioDataAttrs := []string{"data-mp3", "data-wav", "data-audio", "data-sound", "data-preview", "data-file", "data-url", "data-src", "data-ogg"}
		doc.Find("[data-mp3], [data-wav], [data-audio], [data-sound], [data-preview], [data-file], [data-url], [data-src], [data-ogg]").Each(func(_ int, s *goquery.Selection) {
			if len(out) >= limit {
				return
			}
			titleHint := cleanText(firstNonEmptyStr(s.AttrOr("data-title", ""), s.AttrOr("title", ""), s.AttrOr("aria-label", ""), s.AttrOr("alt", "")))
			ctxText := cleanText(s.Parent().Text())
			for _, attr := range audioDataAttrs {
				val := strings.TrimSpace(s.AttrOr(attr, ""))
				if val == "" {
					continue
				}
				k, mime := detectKindFromExtension(val)
				if k == "" && (attr == "data-mp3" || attr == "data-wav" || attr == "data-audio" || attr == "data-sound" || attr == "data-ogg") {
					k = "voice"
					if attr == "data-ogg" {
						mime = "audio/ogg"
					} else if attr == "data-wav" {
						mime = "audio/wav"
					} else {
						mime = "audio/mpeg"
					}
				}
				if k != "" {
					add(k, val, titleHint, ctxText, mime)
					break
				}
			}
		})
	}

	// <img> 参考图提取
	if normalizedKind == "auto" || normalizedKind == "image" {
		doc.Find("img").Each(func(_ int, s *goquery.Selection) {
			src := firstNonEmptyStr(s.AttrOr("src", ""), s.AttrOr("data-src", ""), s.AttrOr("data-original", ""))
			if src == "" || strings.HasPrefix(strings.ToLower(src), "data:image/svg") {
				return
			}
			alt := firstNonEmptyStr(s.AttrOr("alt", ""), s.AttrOr("title", ""))
			if k, _ := detectKindFromExtension(src); k == "" {
				// 某些图床无扩展名但有正常 http 路径，仅当声明为 image 模式时保留明显非图标的图片
				if strings.Contains(strings.ToLower(src), "favicon") || strings.Contains(strings.ToLower(src), "logo") || strings.Contains(strings.ToLower(src), "pixel") {
					return
				}
			}
			add("image", src, alt, "", "")
		})
	}

	// JSON-LD AudioObject / VideoObject / ImageObject 提取
	doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
		rawJSON := strings.TrimSpace(s.Text())
		if rawJSON == "" {
			return
		}
		var obj map[string]interface{}
		if json.Unmarshal([]byte(rawJSON), &obj) == nil {
			extractJSONLDMedia(obj, add)
		}
	})

	// 内联脚本/JSON 音频直链提取（适配动态渲染播放列表页）
	if (normalizedKind == "auto" || normalizedKind == "voice") && len(out) < limit {
		unescaped := strings.NewReplacer(`\/`, `/`, `\u002F`, `/`, `\u002f`, `/`).Replace(htmlBody)
		for _, rawURL := range inlineAudioURLRE.FindAllString(unescaped, limit*3) {
			if len(out) >= limit {
				break
			}
			k, mime := detectKindFromExtension(rawURL)
			if k == "voice" {
				add("voice", rawURL, "", "", mime)
			}
		}
	}

	return out
}

func extractJSONLDMedia(obj map[string]interface{}, add func(kind, url, title, transcript, mime string)) {
	contentURL, _ := obj["contentUrl"].(string)
	name, _ := obj["name"].(string)
	desc, _ := obj["description"].(string)
	typ, _ := obj["@type"].(string)
	encodingFmt, _ := obj["encodingFormat"].(string)
	if contentURL != "" {
		switch {
		case strings.Contains(strings.ToLower(typ), "audio"):
			add("voice", contentURL, name, desc, encodingFmt)
		case strings.Contains(strings.ToLower(typ), "video"):
			add("video", contentURL, name, desc, encodingFmt)
		case strings.Contains(strings.ToLower(typ), "image"):
			add("image", contentURL, name, desc, encodingFmt)
		}
	}
}

func containsCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// translateVoiceQueryToEnglish 将中文音色描述词映射为英文语音检索关键词，提升公开音源库命中率。
func translateVoiceQueryToEnglish(query string) string {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return "voice dialogue"
	}
	if !containsCJK(trimmed) {
		return trimmed
	}

	rules := []struct {
		zh string
		en string
	}{
		{"霸总", "authoritative"},
		{"总裁", "authoritative"},
		{"低沉", "deep"},
		{"磁性", "deep"},
		{"浑厚", "deep"},
		{"沉稳", "calm"},
		{"男声", "male voice"},
		{"男音", "male voice"},
		{"男生", "male voice"},
		{"男主", "male voice"},
		{"大叔", "older male voice"},
		{"少年", "young male voice"},
		{"女声", "female voice"},
		{"女音", "female voice"},
		{"女生", "female voice"},
		{"女主", "female voice"},
		{"御姐", "mature female voice"},
		{"少女", "young female voice"},
		{"萝莉", "cute girl voice"},
		{"甜宠", "sweet female voice"},
		{"萌娃", "child voice"},
		{"反派", "villain dark"},
		{"冷酷", "cold dark"},
		{"热血", "energetic"},
		{"战神", "heroic"},
		{"悬疑", "suspense"},
		{"旁白", "narration"},
		{"解说", "narration"},
		{"古风", "traditional chinese"},
		{"仙侠", "epic"},
		{"方言", "dialect"},
		{"粤语", "cantonese"},
		{"笑声", "laugh"},
		{"哭声", "crying"},
		{"喘息", "breathing"},
	}

	var parts []string
	seen := make(map[string]struct{})
	addPart := func(p string) {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			parts = append(parts, p)
		}
	}
	for _, r := range rules {
		if strings.Contains(trimmed, r.zh) {
			addPart(r.en)
		}
	}
	if len(parts) == 0 {
		if strings.Contains(trimmed, "男") {
			addPart("male voice")
		} else if strings.Contains(trimmed, "女") {
			addPart("female voice")
		} else {
			addPart("human voice speech")
		}
	} else {
		joined := strings.Join(parts, " ")
		if !strings.Contains(joined, "voice") && !strings.Contains(joined, "narration") && !strings.Contains(joined, "laugh") && !strings.Contains(joined, "crying") {
			addPart("voice")
		}
	}
	// 限制词数避免过严检索条件导致无结果
	if len(parts) > 3 {
		parts = parts[:3]
	}
	return strings.Join(parts, " ")
}

func searchFreesoundPreviews(ctx context.Context, query string, limit int, opts MediaCrawlOptions) ([]CrawledMediaCandidate, error) {
	baseAPI := strings.TrimSpace(opts.FreesoundSearchURL)
	if baseAPI == "" {
		baseAPI = defaultCrawlerFreesoundHTML
	}
	u, err := ValidatePublicMediaURL(baseAPI, opts.AllowPrivateTest)
	if err != nil {
		return nil, err
	}

	searchQuery := translateVoiceQueryToEnglish(query)
	if !strings.Contains(strings.ToLower(searchQuery), "voice") &&
		!strings.Contains(strings.ToLower(searchQuery), "speech") &&
		!strings.Contains(strings.ToLower(searchQuery), "narration") {
		searchQuery += " voice"
	}

	q := u.Query()
	q.Set("q", searchQuery)
	u.RawQuery = q.Encode()

	client := resolveCrawlerClient(opts)
	req, err := newCrawlerRequest(ctx, u.String())
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("freesound status %d", resp.StatusCode)
	}
	htmlBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxCrawlHTMLBytes))
	if err != nil {
		return nil, err
	}

	items := ExtractMediaFromHTML(resp.Request.URL, string(htmlBytes), "voice", limit*2, opts.AllowPrivateTest)
	out := make([]CrawledMediaCandidate, 0, limit)
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.MediaURL), ".mp3") || strings.Contains(strings.ToLower(it.MimeType), "mpeg") {
			it.Tags = mergeUniqueVoiceTags(it.Tags, "Freesound公开库")
			out = append(out, it)
		}
		if len(out) >= limit {
			break
		}
	}
	if len(out) == 0 {
		for _, it := range items {
			it.Tags = mergeUniqueVoiceTags(it.Tags, "Freesound公开库")
			out = append(out, it)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func searchWikimediaCommons(ctx context.Context, query, kind string, limit int, opts MediaCrawlOptions) ([]CrawledMediaCandidate, error) {
	baseAPI := strings.TrimSpace(opts.WikimediaSearchURL)
	if baseAPI == "" {
		baseAPI = defaultCrawlerWikimediaAPI
	}
	u, err := ValidatePublicMediaURL(baseAPI, opts.AllowPrivateTest)
	if err != nil {
		return nil, err
	}

	searchQuery := strings.TrimSpace(query)
	if opts.WikimediaSearchURL == "" && kind == "voice" && containsCJK(searchQuery) {
		searchQuery = translateVoiceQueryToEnglish(searchQuery)
	}
	switch kind {
	case "voice":
		if !strings.Contains(strings.ToLower(searchQuery), "filetype:") {
			searchQuery += " filetype:audio"
		}
	case "image":
		if !strings.Contains(strings.ToLower(searchQuery), "filetype:") {
			searchQuery += " filetype:bitmap"
		}
	case "video":
		if !strings.Contains(strings.ToLower(searchQuery), "filetype:") {
			searchQuery += " filetype:video"
		}
	}

	q := u.Query()
	q.Set("action", "query")
	q.Set("format", "json")
	q.Set("generator", "search")
	q.Set("gsrnamespace", "6")
	q.Set("gsrsearch", searchQuery)
	q.Set("gsrlimit", fmt.Sprintf("%d", limit*2))
	q.Set("prop", "imageinfo")
	q.Set("iiprop", "url|mime|size")
	u.RawQuery = q.Encode()

	client := resolveCrawlerClient(opts)
	req, err := newCrawlerRequest(ctx, u.String())
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("search status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Query struct {
			Pages map[string]struct {
				Title     string `json:"title"`
				ImageInfo []struct {
					URL            string `json:"url"`
					DescriptionURL string `json:"descriptionurl"`
					Mime           string `json:"mime"`
					Size           int64  `json:"size"`
				} `json:"imageinfo"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}

	results := make([]CrawledMediaCandidate, 0, limit)
	for _, page := range parsed.Query.Pages {
		if len(page.ImageInfo) == 0 {
			continue
		}
		info := page.ImageInfo[0]
		mediaKind := detectKindFromContentType(info.Mime)
		if mediaKind == "" {
			mediaKind, _ = detectKindFromExtension(info.URL)
		}
		if mediaKind == "" || (kind != "auto" && mediaKind != kind) {
			continue
		}
		title := strings.TrimPrefix(page.Title, "File:")
		title = cleanupMediaTitle(title)
		role, gender, tags := inferVoiceAttrsFromText(title)
		results = append(results, CrawledMediaCandidate{
			MediaKind:     mediaKind,
			Title:         title,
			MediaURL:      info.URL,
			SourcePage:    info.DescriptionURL,
			SourceDomain:  "commons.wikimedia.org",
			MimeType:      info.Mime,
			SizeBytes:     info.Size,
			Gender:        gender,
			Locale:        inferCrawlerLocale(title),
			SuggestedRole: role,
			Tags:          mergeUniqueVoiceTags(tags, "Wikimedia公开库"),
		})
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

func unwrapSearchResultURL(rawHref string) string {
	href := strings.TrimSpace(rawHref)
	if href == "" {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())

	// 解码 Bing 跳转链接 https://www.bing.com/ck/a?!&&p=...&u=a1<base64url>
	if strings.Contains(host, "bing.com") && strings.HasPrefix(u.Path, "/ck/a") {
		if uParam := u.Query().Get("u"); strings.HasPrefix(uParam, "a1") && len(uParam) > 2 {
			encoded := strings.TrimPrefix(uParam, "a1")
			if decoded, decErr := base64.RawURLEncoding.DecodeString(encoded); decErr == nil {
				target := strings.TrimSpace(string(decoded))
				if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
					return target
				}
			}
		}
		return ""
	}

	// 解码 DuckDuckGo 跳转链接 //duckduckgo.com/l/?uddg=<url>
	if strings.Contains(host, "duckduckgo.com") && strings.Contains(u.Path, "/l/") {
		if uddg := u.Query().Get("uddg"); strings.HasPrefix(uddg, "http://") || strings.HasPrefix(uddg, "https://") {
			return uddg
		}
		return ""
	}

	if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
		return ""
	}
	// 过滤搜索引擎自身域名与纯词条站（无音频文件）
	for _, skip := range []string{"bing.com", "microsoft.com", "duckduckgo.com", "baike.baidu.com", "zdic.net"} {
		if strings.Contains(host, skip) {
			return ""
		}
	}
	return href
}

func fetchSearchResultLinks(ctx context.Context, searchEndpoint, queryStr string, opts MediaCrawlOptions) ([]string, error) {
	u, err := ValidatePublicMediaURL(searchEndpoint, opts.AllowPrivateTest)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", queryStr)
	u.RawQuery = q.Encode()

	client := resolveCrawlerClient(opts)
	req, err := newCrawlerRequest(ctx, u.String())
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("web search status %d", resp.StatusCode)
	}
	htmlBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxCrawlHTMLBytes))
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(htmlBytes)))
	if err != nil {
		return nil, err
	}
	pageLinks := make([]string, 0, 5)
	seenLinks := make(map[string]struct{})
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		if len(pageLinks) >= 5 {
			return
		}
		resolved := unwrapSearchResultURL(s.AttrOr("href", ""))
		if resolved == "" {
			return
		}
		if _, dup := seenLinks[resolved]; dup {
			return
		}
		seenLinks[resolved] = struct{}{}
		pageLinks = append(pageLinks, resolved)
	})
	return pageLinks, nil
}

func searchWebAndCrawlPages(ctx context.Context, query, kind string, limit int, opts MediaCrawlOptions) ([]CrawledMediaCandidate, error) {
	if limit <= 0 {
		return nil, nil
	}
	baseSearch := strings.TrimSpace(opts.WebSearchURL)
	if baseSearch == "" {
		baseSearch = defaultCrawlerWebSearch
	}

	suffix := ""
	switch kind {
	case "voice":
		suffix = " mp3 试听 音色 配音"
	case "image":
		suffix = " jpg png 高清 参考图 剧照"
	case "video":
		suffix = " mp4 素材 视频"
	}
	searchQuery := strings.TrimSpace(query) + suffix

	pageLinks, err := fetchSearchResultLinks(ctx, baseSearch, searchQuery, opts)
	if (err != nil || len(pageLinks) == 0) && opts.WebSearchURL == "" {
		if ddgLinks, ddgErr := fetchSearchResultLinks(ctx, defaultCrawlerDDGSearch, searchQuery, opts); ddgErr == nil && len(ddgLinks) > 0 {
			pageLinks = ddgLinks
			err = nil
		}
	}
	if err != nil && len(pageLinks) == 0 {
		return nil, err
	}

	var mu sync.Mutex
	var out []CrawledMediaCandidate
	var wg sync.WaitGroup
	for _, pageURL := range pageLinks {
		wg.Add(1)
		go func(link string) {
			defer wg.Done()
			subCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			items, err := CrawlMediaURL(subCtx, link, opts)
			if err != nil {
				return
			}
			mu.Lock()
			out = append(out, items...)
			mu.Unlock()
		}(pageURL)
	}
	wg.Wait()
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// DownloadMediaBytes 安全下载爬虫发现的远程媒体文件并验证字节流类型（拒绝 HTML 伪装错误页）。
func DownloadMediaBytes(ctx context.Context, rawURL, expectedKind string, opts MediaCrawlOptions) ([]byte, string, error) {
	parsed, err := ValidatePublicMediaURL(rawURL, opts.AllowPrivateTest)
	if err != nil {
		return nil, "", err
	}
	maxBytes := int64(maxVoiceDownloadBytes)
	switch NormalizeCrawlerKind(expectedKind) {
	case "image":
		maxBytes = maxImageDownloadBytes
	case "video":
		maxBytes = maxVideoDownloadBytes
	}

	client := resolveCrawlerClient(opts)
	req, err := newCrawlerRequest(ctx, parsed.String())
	if err != nil {
		return nil, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("download media failed (status %d)", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(raw)) > maxBytes {
		return nil, "", fmt.Errorf("media exceeds max size (%d MB)", maxBytes>>20)
	}
	if len(raw) < 16 {
		return nil, "", errors.New("downloaded media payload is too small")
	}

	ct := stripMimeParams(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml") {
		return nil, "", errors.New("target returned an HTML page rather than a direct media stream")
	}
	if ct == "" || ct == "application/octet-stream" {
		if _, extMime := detectKindFromExtension(parsed.String()); extMime != "" {
			ct = extMime
		} else {
			ct = stripMimeParams(http.DetectContentType(raw))
		}
	}
	return raw, ct, nil
}

func detectKindFromExtension(raw string) (kind string, mimeType string) {
	lower := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case audioExtRE.MatchString(lower):
		switch {
		case strings.Contains(lower, ".wav"):
			return "voice", "audio/wav"
		case strings.Contains(lower, ".ogg"), strings.Contains(lower, ".oga"), strings.Contains(lower, ".opus"):
			return "voice", "audio/ogg"
		case strings.Contains(lower, ".m4a"), strings.Contains(lower, ".aac"):
			return "voice", "audio/mp4"
		default:
			return "voice", "audio/mpeg"
		}
	case imageExtRE.MatchString(lower):
		switch {
		case strings.Contains(lower, ".jpg"), strings.Contains(lower, ".jpeg"):
			return "image", "image/jpeg"
		case strings.Contains(lower, ".webp"):
			return "image", "image/webp"
		case strings.Contains(lower, ".gif"):
			return "image", "image/gif"
		default:
			return "image", "image/png"
		}
	case videoExtRE.MatchString(lower):
		switch {
		case strings.Contains(lower, ".webm"):
			return "video", "video/webm"
		default:
			return "video", "video/mp4"
		}
	default:
		return "", ""
	}
}

func detectKindFromContentType(ct string) string {
	lower := strings.ToLower(strings.TrimSpace(ct))
	switch {
	case strings.HasPrefix(lower, "audio/"), lower == "application/ogg":
		return "voice"
	case strings.HasPrefix(lower, "image/"):
		return "image"
	case strings.HasPrefix(lower, "video/"):
		return "video"
	default:
		return ""
	}
}

func resolveAbsoluteHTTPURL(base *url.URL, ref string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" || strings.HasPrefix(trimmed, "javascript:") || strings.HasPrefix(trimmed, "data:") {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

func stripMimeParams(ct string) string {
	clean := strings.TrimSpace(strings.ToLower(ct))
	if idx := strings.Index(clean, ";"); idx > 0 {
		clean = strings.TrimSpace(clean[:idx])
	}
	return clean
}

func cleanupMediaTitle(raw string) string {
	decoded, err := url.QueryUnescape(strings.TrimSpace(raw))
	if err == nil && decoded != "" {
		raw = decoded
	}
	raw = strings.TrimSuffix(raw, path.Ext(raw))
	raw = strings.NewReplacer("_", " ", "-", " ").Replace(raw)
	return cleanText(raw)
}

func cleanText(s string) string {
	return strings.TrimSpace(whitespaceRE.ReplaceAllString(s, " "))
}

func truncateRuneStr(s string, maxRunes int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= maxRunes {
		return string(r)
	}
	return strings.TrimSpace(string(r[:maxRunes])) + "..."
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func inferCrawlerLocale(text string) string {
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF {
			return "zh-CN"
		}
		if (r >= 0x3040 && r <= 0x30FF) {
			return "ja-JP"
		}
	}
	return "en-US"
}

func inferVoiceAttrsFromText(text string) (role string, gender string, tags []string) {
	lower := strings.ToLower(text)
	gender = "female"
	switch {
	case strings.Contains(lower, "女") || strings.Contains(lower, "female") || strings.Contains(lower, "girl") || strings.Contains(lower, "woman"):
		gender = "female"
	case strings.Contains(lower, "男") || strings.Contains(lower, "霸总") || strings.Contains(lower, "战神") || strings.Contains(lower, "大叔") || strings.Contains(lower, "male") || strings.Contains(lower, "boss") || strings.Contains(lower, "villain") || strings.Contains(lower, "deep voice"):
		gender = "male"
	}
	switch {
	case strings.Contains(lower, "霸总") || strings.Contains(lower, "总裁") || strings.Contains(lower, "boss"):
		role = "霸总男主"
	case strings.Contains(lower, "战神") || strings.Contains(lower, "热血"):
		role = "热血战神"
	case strings.Contains(lower, "反派") || strings.Contains(lower, "阴鸷") || strings.Contains(lower, "villain"):
		role = "冷酷反派"
	case strings.Contains(lower, "甜") || strings.Contains(lower, "萝莉") || strings.Contains(lower, "少女") || strings.Contains(lower, "萌娃"):
		role = "甜宠千金"
	case strings.Contains(lower, "悬疑") || strings.Contains(lower, "旁白") || strings.Contains(lower, "narrat"):
		role = "悬疑旁白"
	case strings.Contains(lower, "古风") || strings.Contains(lower, "仙侠") || strings.Contains(lower, "女主"):
		role = "大女主"
	}
	if role != "" {
		tags = []string{"短剧常用", role}
	}
	return role, gender, tags
}
