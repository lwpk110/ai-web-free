package provider

import (
	"context"
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
	defaultCrawlerWikimediaAPI = "https://commons.wikimedia.org/w/api.php"
	defaultCrawlerWebSearch    = "https://cn.bing.com/search"
	maxCrawlHTMLBytes          = 2 << 20  // 2 MB HTML cap
	maxVoiceDownloadBytes      = 15 << 20 // 15 MB audio cap
	maxImageDownloadBytes      = 15 << 20 // 15 MB image cap
	maxVideoDownloadBytes      = 40 << 20 // 40 MB video cap
)

var (
	audioExtRE = regexp.MustCompile(`(?i)\.(mp3|wav|ogg|oga|m4a|aac|flac|opus)(?:[?#]|$)`)
	imageExtRE = regexp.MustCompile(`(?i)\.(jpe?g|png|webp|gif)(?:[?#]|$)`)
	videoExtRE = regexp.MustCompile(`(?i)\.(mp4|webm|mov|m4v)(?:[?#]|$)`)
	whitespaceRE = regexp.MustCompile(`\s+`)
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
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; AIWebFreeMediaCrawler/1.0)")
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

	// 2. 若提供了关键词且仍需补充候选，执行公开媒体搜索
	if report.Query != "" && len(report.Candidates) < limit {
		wikiItems, wikiErr := searchWikimediaCommons(ctx, report.Query, kind, limit, opts)
		if wikiErr != nil {
			report.Warnings = append(report.Warnings, "Wikimedia 检索警告: "+wikiErr.Error())
		} else {
			for _, item := range wikiItems {
				addCandidate(item)
			}
		}

		if len(report.Candidates) < 4 {
			webItems, webErr := searchWebAndCrawlPages(ctx, report.Query, kind, limit-len(report.Candidates), opts)
			if webErr != nil {
				report.Warnings = append(report.Warnings, "网页搜索扩展提示: "+webErr.Error())
			} else {
				for _, item := range webItems {
					addCandidate(item)
				}
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

func searchWebAndCrawlPages(ctx context.Context, query, kind string, limit int, opts MediaCrawlOptions) ([]CrawledMediaCandidate, error) {
	if limit <= 0 {
		return nil, nil
	}
	baseSearch := strings.TrimSpace(opts.WebSearchURL)
	if baseSearch == "" {
		baseSearch = defaultCrawlerWebSearch
	}
	u, err := ValidatePublicMediaURL(baseSearch, opts.AllowPrivateTest)
	if err != nil {
		return nil, err
	}

	suffix := ""
	switch kind {
	case "voice":
		suffix = " mp3 wav 试听 音色 配音"
	case "image":
		suffix = " jpg png 高清 参考图 剧照"
	case "video":
		suffix = " mp4 素材 视频"
	}
	q := u.Query()
	q.Set("q", strings.TrimSpace(query)+suffix)
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
	pageLinks := make([]string, 0, 4)
	seenLinks := make(map[string]struct{})
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		if len(pageLinks) >= 4 {
			return
		}
		href := strings.TrimSpace(s.AttrOr("href", ""))
		if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
			return
		}
		if strings.Contains(href, "bing.com") || strings.Contains(href, "microsoft.com") {
			return
		}
		if _, dup := seenLinks[href]; dup {
			return
		}
		seenLinks[href] = struct{}{}
		pageLinks = append(pageLinks, href)
	})

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
	case strings.Contains(lower, "男") || strings.Contains(lower, "霸总") || strings.Contains(lower, "战神") || strings.Contains(lower, "大叔") || strings.Contains(lower, "male") || strings.Contains(lower, "boss") || strings.Contains(lower, "villain"):
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
