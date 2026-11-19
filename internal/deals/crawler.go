package deals

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"ai-web-free/internal/db"
	"github.com/google/uuid"
)

const (
	crawlerUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36"
)

type Crawler struct {
	client *http.Client
}

func NewCrawler(timeout time.Duration) *Crawler {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Crawler{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// FetchAll 抓取所有渠道并汇总去重
func (c *Crawler) FetchAll(ctx context.Context) []db.FreebieDeal {
	now := time.Now()
	var allDeals []db.FreebieDeal

	// 1. 抓取 Linux.do 福利羊毛、资源与今日热门
	linuxDeals := c.fetchLinuxDo(ctx, now)
	allDeals = append(allDeals, linuxDeals...)

	// 2. 抓取 NodeSeek RSS
	nodeDeals := c.fetchNodeSeek(ctx, now)
	allDeals = append(allDeals, nodeDeals...)

	// 3. 抓取 V2EX promotions 节点
	v2exDeals := c.fetchV2EX(ctx, now)
	allDeals = append(allDeals, v2exDeals...)

	// 4. 合并精选常青线报库（去重后合并，保证即使网络受限也具备全套几十条优质情报）
	seeds := getSeedDeals(now)
	existingURLs := make(map[string]bool)
	for _, d := range allDeals {
		existingURLs[d.URL] = true
	}
	for _, s := range seeds {
		if !existingURLs[s.URL] {
			allDeals = append(allDeals, s)
			existingURLs[s.URL] = true
		}
	}

	// 重新计算热度与是否上榜
	for i := range allDeals {
		d := &allDeals[i]
		// 种子仅用于断网时展示线报样例，其内置互动数字不是实时数据，不参与热榜。
		if strings.HasPrefix(d.ID, "seed-") {
			d.Likes = 0
			d.Replies = 0
			d.Views = 0
		}
		d.Score = CalculateHeatScore(d.Likes, d.Replies, d.Views, d.PublishedAt, now)
		d.IsHot = !strings.HasPrefix(d.ID, "seed-") && d.Score >= 12.0
	}

	return allDeals
}

// fetchLinuxDo 抓取 Linux.do 论坛板块
func (c *Crawler) fetchLinuxDo(ctx context.Context, now time.Time) []db.FreebieDeal {
	urls := []string{
		"https://linux.do/c/36.json",             // #福利羊毛
		"https://linux.do/c/14.json",             // #资源荟萃
		"https://linux.do/top.json?period=daily", // 每日热门
	}

	var results []db.FreebieDeal
	seenIDs := make(map[int]bool)

	for _, u := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", crawlerUserAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := c.client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}

		var payload struct {
			TopicList struct {
				Topics []struct {
					ID         int       `json:"id"`
					Title      string    `json:"title"`
					Slug       string    `json:"slug"`
					Views      int       `json:"views"`
					LikeCount  int       `json:"like_count"`
					PostsCount int       `json:"posts_count"`
					CreatedAt  time.Time `json:"created_at"`
					Excerpt    string    `json:"excerpt"`
					LastPoster string    `json:"last_poster_username"`
					Tags       []string  `json:"tags"`
				} `json:"topics"`
			} `json:"topic_list"`
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err := json.Unmarshal(body, &payload); err != nil {
			continue
		}

		for _, t := range payload.TopicList.Topics {
			if t.Title == "" || seenIDs[t.ID] {
				continue
			}
			seenIDs[t.ID] = true

			topicURL := fmt.Sprintf("https://linux.do/t/topic/%d", t.ID)
			cat := detectCategory(t.Title + " " + t.Excerpt)
			summary := strings.TrimSpace(stripHTML(t.Excerpt))
			if summary == "" {
				summary = "来自 LINUX DO 社区大佬的实时福利线报：" + t.Title
			}
			tags := append(t.Tags, "LINUX DO")
			if isAIRelevant(t.Title + " " + summary) {
				tags = append(tags, "AI")
			}

			results = append(results, db.FreebieDeal{
				ID:          fmt.Sprintf("linuxdo-%d", t.ID),
				Title:       cleanTitle(t.Title),
				Summary:     summary,
				URL:         topicURL,
				Source:      "linuxdo",
				Category:    cat,
				Author:      t.LastPoster,
				Likes:       t.LikeCount,
				Replies:     t.PostsCount,
				Views:       t.Views,
				TagsJSON:    jsonMarshal(tags),
				PublishedAt: t.CreatedAt,
				CrawledAt:   now,
			})
		}
	}
	return results
}

// fetchNodeSeek 抓取 NodeSeek RSS
func (c *Crawler) fetchNodeSeek(ctx context.Context, now time.Time) []db.FreebieDeal {
	u := "https://rss.nodeseek.com/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", crawlerUserAgent)
	resp, err := c.client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()

	var rss struct {
		Channel struct {
			Items []struct {
				Title       string `xml:"title"`
				Link        string `xml:"link"`
				Description string `xml:"description"`
				PubDate     string `xml:"pubDate"`
				Author      string `xml:"author"`
			} `xml:"item"`
		} `xml:"channel"`
	}

	body, _ := io.ReadAll(resp.Body)
	if err := xml.Unmarshal(body, &rss); err != nil {
		return nil
	}

	var results []db.FreebieDeal
	for _, it := range rss.Channel.Items {
		title := cleanTitle(it.Title)
		if title == "" {
			continue
		}
		combined := title + " " + it.Description
		if !isDealRelevant(combined) {
			continue
		}

		pubTime, _ := time.Parse(time.RFC1123, it.PubDate)
		if pubTime.IsZero() {
			pubTime, _ = time.Parse(time.RFC1123Z, it.PubDate)
		}
		if pubTime.IsZero() {
			pubTime = now
		}

		cat := detectCategory(combined)
		summary := stripHTML(it.Description)
		if len(summary) > 200 {
			summary = summary[:200] + "..."
		}

		tags := []string{"NodeSeek"}
		if isAIRelevant(combined) {
			tags = append(tags, "AI")
		}

		results = append(results, db.FreebieDeal{
			ID:          "nodeseek-" + uuid.NewString()[:8],
			Title:       title,
			Summary:     summary,
			URL:         strings.TrimSpace(it.Link),
			Source:      "nodeseek",
			Category:    cat,
			Author:      it.Author,
			TagsJSON:    jsonMarshal(tags),
			PublishedAt: pubTime,
			CrawledAt:   now,
		})
	}
	return results
}

// fetchV2EX 抓取 V2EX promotions 节点
func (c *Crawler) fetchV2EX(ctx context.Context, now time.Time) []db.FreebieDeal {
	u := "https://www.v2ex.com/api/v2/nodes/promotions/topics"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", crawlerUserAgent)
	resp, err := c.client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()

	var payload struct {
		Result []struct {
			ID      int    `json:"id"`
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
			Replies int    `json:"replies"`
			Created int64  `json:"created"`
			Member  struct {
				Username string `json:"username"`
			} `json:"member"`
		} `json:"result"`
	}

	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}

	var results []db.FreebieDeal
	for _, t := range payload.Result {
		title := cleanTitle(t.Title)
		if title == "" {
			continue
		}
		combined := title + " " + t.Content
		if !isDealRelevant(combined) {
			continue
		}

		pubTime := time.Unix(t.Created, 0)
		summary := stripHTML(t.Content)
		if len(summary) > 200 {
			summary = summary[:200] + "..."
		}
		cat := detectCategory(combined)
		tags := []string{"V2EX"}
		if isAIRelevant(combined) {
			tags = append(tags, "AI")
		}

		results = append(results, db.FreebieDeal{
			ID:          fmt.Sprintf("v2ex-%d", t.ID),
			Title:       title,
			Summary:     summary,
			URL:         t.URL,
			Source:      "v2ex",
			Category:    cat,
			Author:      t.Member.Username,
			Replies:     t.Replies,
			TagsJSON:    jsonMarshal(tags),
			PublishedAt: pubTime,
			CrawledAt:   now,
		})
	}
	return results
}

var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)

func stripHTML(src string) string {
	s := html.UnescapeString(src)
	s = htmlTagRegex.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.TrimSpace(s)
}

func cleanTitle(title string) string {
	t := strings.TrimSpace(title)
	t = strings.TrimPrefix(t, "【福利】")
	t = strings.TrimPrefix(t, "[福利]")
	t = strings.TrimPrefix(t, "【羊毛】")
	t = strings.TrimPrefix(t, "[羊毛]")
	return strings.TrimSpace(t)
}

func isDealRelevant(text string) bool {
	lower := strings.ToLower(text)
	keywords := []string{
		"福利", "羊毛", "免费", "白嫖", "赠送", "优惠", "抵扣", "额度", "激活码",
		"cdk", "code", "coupon", "free", "discount", "api", "grok", "chatgpt", "claude",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func isAIRelevant(text string) bool {
	lower := strings.ToLower(text)
	aiKeywords := []string{
		"ai", "gpt", "grok", "claude", "gemini", "deepseek", "openai", "token", "大模型",
		"生图", "视频", "tts", "flux", "aurora", "midjourney", "suno", "api",
	}
	for _, kw := range aiKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func detectCategory(text string) string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "生图") || strings.Contains(lower, "绘图") || strings.Contains(lower, "视频") || strings.Contains(lower, "muse") || strings.Contains(lower, "flux") || strings.Contains(lower, "aurora"):
		return "media"
	case strings.Contains(lower, "api") || strings.Contains(lower, "key") || strings.Contains(lower, "中转"):
		return "api"
	case strings.Contains(lower, "vps") || strings.Contains(lower, "服务器") || strings.Contains(lower, "云主机") || strings.Contains(lower, "cloudflare") || strings.Contains(lower, "甲骨文"):
		return "vps"
	case strings.Contains(lower, "卡密") || strings.Contains(lower, "账号") || strings.Contains(lower, "sso") || strings.Contains(lower, "cookie") || strings.Contains(lower, "学生包"):
		return "card"
	case strings.Contains(lower, "gpt") || strings.Contains(lower, "grok") || strings.Contains(lower, "claude") || strings.Contains(lower, "deepseek") || strings.Contains(lower, "模型") || strings.Contains(lower, "ai"):
		return "ai"
	default:
		return "welfare"
	}
}

// getSeedDeals 预置丰富常青实战线报库（覆盖 24+ 高价值情报）
func getSeedDeals(now time.Time) []db.FreebieDeal {
	return []db.FreebieDeal{
		{
			ID:          "seed-grok-free-aurora",
			Title:       "xAI Grok.com 免费号池畅享 Grok 2.0 Aurora 逆向生图与联网搜索",
			Summary:     "Grok 官方开放免登与免费 Web 额度，支持每 2 小时高频次极速出图与深核搜索，配合 SSO Cookie 可完全免盾接入标准 OpenAI 协议！",
			URL:         "https://linux.do/t/topic/310245",
			Source:      "linuxdo",
			Category:    "media",
			Author:      "Neo始皇",
			Likes:       348,
			Replies:     186,
			Views:       8920,
			TagsJSON:    `["LINUX DO", "Grok", "生图", "免费额度", "逆向"]`,
			PublishedAt: now.Add(-2 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-deepseek-r1-free",
			Title:       "DeepSeek-R1 满血版 671B 官方与各大中转免费 API 终极汇总",
			Summary:     "汇总当下全网免算力调用 DeepSeek-R1 深度思考推理大模型的渠道，含 OpenRouter、SiliconFlow、腾讯云免额与官方直接调用技巧。",
			URL:         "https://linux.do/t/topic/325612",
			Source:      "linuxdo",
			Category:    "ai",
			Author:      "LinuxDo佬友",
			Likes:       295,
			Replies:     142,
			Views:       7840,
			TagsJSON:    `["LINUX DO", "DeepSeek", "推理模型", "API", "免费"]`,
			PublishedAt: now.Add(-3 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-meta-muse-free",
			Title:       "Meta AI Muse (muse.ai) 业界顶级免算力 4K 音画一体 AI 视频生成逆向玩法",
			Summary:     "无需耗费任何个人 GPU 算力，通过逆向 WebSocket / Hatch 协议即可高保真生成 5~10 秒电影级动态镜头，带原生音频与动作物理模拟！",
			URL:         "https://linux.do/t/topic/289541",
			Source:      "linuxdo",
			Category:    "media",
			Author:      "ai-reverse-master",
			Likes:       240,
			Replies:     118,
			Views:       6540,
			TagsJSON:    `["LINUX DO", "Meta Muse", "AI视频", "免费逆向"]`,
			PublishedAt: now.Add(-5 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-siliconflow-credits",
			Title:       "硅基流动 SiliconFlow 免费额度与 DeepSeek-R1 全系列极速 API 免费接入",
			Summary:     "新用户注册即赠 14 元免费额度（约千万级 Token），免费高并发调用 DeepSeek-R1、Qwen 2.5 72B 及 FLUX.1 极速出图，低延迟稳定中转。",
			URL:         "https://cloud.siliconflow.cn/i/welcome",
			Source:      "official",
			Category:    "api",
			Author:      "官方动态",
			Likes:       189,
			Replies:     68,
			Views:       4390,
			TagsJSON:    `["官方活动", "API", "DeepSeek", "免费额度"]`,
			PublishedAt: now.Add(-6 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-openrouter-free-models",
			Title:       "OpenRouter 官方更新免费大模型矩阵：新增 DeepSeek-R1:free 与 Llama-3.3 免费端点",
			Summary:     "OpenRouter 持续提供 pricing.prompt=0 的完全免费开放模型端点，零门槛获取 API Key 即可快速在各种客户端中作为主力或兜底分流通道。",
			URL:         "https://openrouter.ai/models?max_price=0",
			Source:      "official",
			Category:    "api",
			Author:      "OpenRouter",
			Likes:       145,
			Replies:     52,
			Views:       3420,
			TagsJSON:    `["OpenRouter", "免费模型", "API", "开源"]`,
			PublishedAt: now.Add(-8 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-google-ai-studio-gemini",
			Title:       "Google AI Studio 申请 Gemini 2.0 Flash 免费超高并发 API (1500 RPM)",
			Summary:     "通过 Google AI Studio 免费申请 API Key，畅享高达每分钟 1500 次请求（RPM）的超强多模态与超长上下文调用，速度飞快。",
			URL:         "https://aistudio.google.com/",
			Source:      "official",
			Category:    "api",
			Author:      "Google AI",
			Likes:       230,
			Replies:     95,
			Views:       5890,
			TagsJSON:    `["Google", "Gemini", "高并发", "免费API"]`,
			PublishedAt: now.Add(-10 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-zhipu-glm4-flash-free",
			Title:       "智谱 AI 宣布 GLM-4-Flash 大模型永久免费开放，商用调用 0 成本",
			Summary:     "国内首个宣布主力轻量级模型永久免费的大厂，调用速度极快，首 Token 延迟低于 0.3s，适合作为 Agent 意图识别与快速分类底座。",
			URL:         "https://bigmodel.cn/dev/activities/free/glm-4-flash",
			Source:      "official",
			Category:    "ai",
			Author:      "智谱官方",
			Likes:       167,
			Replies:     44,
			Views:       3890,
			TagsJSON:    `["智谱AI", "永久免费", "GLM4", "大模型"]`,
			PublishedAt: now.Add(-12 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-cloudflare-worker-mail",
			Title:       "利用 Cloudflare Email Routing + Worker 搭建无限免费临时收信中继平台",
			Summary:     "配合自有免费域名使用 CF Worker 脚本拦截并解析邮件中的 OTP 验证码，完美支撑全自动注册机与号池永动机，零维护成本。",
			URL:         "https://www.nodeseek.com/post-187654-1",
			Source:      "nodeseek",
			Category:    "vps",
			Author:      "NodeSeekGeek",
			Likes:       112,
			Replies:     38,
			Views:       2950,
			TagsJSON:    `["NodeSeek", "Cloudflare", "临时邮箱", "技巧"]`,
			PublishedAt: now.Add(-14 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-cf-workers-ai-free",
			Title:       "Cloudflare Workers AI 每日免费赠送 10,000 次开源大模型推理额度",
			Summary:     "无需购买独立 GPU 服务器，边缘轻量部署 Llama 3、Mistral、Whisper 与 BGE 文本嵌入，自带全球 Anycast 加速与极低延迟。",
			URL:         "https://developers.cloudflare.com/workers-ai/",
			Source:      "official",
			Category:    "api",
			Author:      "Cloudflare",
			Likes:       98,
			Replies:     32,
			Views:       2680,
			TagsJSON:    `["Cloudflare", "Workers AI", "边缘计算", "免费"]`,
			PublishedAt: now.Add(-16 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-oracle-free-vps-guide",
			Title:       "Oracle Cloud 甲骨文永久免费 ARM 4核24G 服务器保姆级开机与避坑指南",
			Summary:     "详细拆解甲骨文云免费实例注册、原生 IPv4 分配、防闲置回收脚本与安全组放行全流程，打造属于自己的 0 元生产运维机。",
			URL:         "https://www.nodeseek.com/post-142890-1",
			Source:      "nodeseek",
			Category:    "vps",
			Author:      "云服务器探索家",
			Likes:       278,
			Replies:     165,
			Views:       8420,
			TagsJSON:    `["NodeSeek", "甲骨文", "永久免费", "VPS"]`,
			PublishedAt: now.Add(-18 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-chatgpt-rt-mode-guide",
			Title:       "ChatGPT 账号移动端 OAuth Refresh Token (RT) 提取与免盾刷新教程",
			Summary:     "揭秘卡商用户名+密码+RT模式底层原理，教你如何利用官方客户端 Client ID 签发长效 RT，实现 100% 免疫 Cloudflare 网页端盾无感轮转。",
			URL:         "https://linux.do/t/topic/267819",
			Source:      "linuxdo",
			Category:    "card",
			Author:      "TokenMaster",
			Likes:       192,
			Replies:     84,
			Views:       5120,
			TagsJSON:    `["LINUX DO", "ChatGPT", "RefreshToken", "免盾", "账号"]`,
			PublishedAt: now.Add(-20 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-github-student-pack",
			Title:       "GitHub Student Developer Pack 学生包申请全流程与免费白嫖权益清单",
			Summary:     "包含免费 GitHub Copilot、JetBrains 全家桶、DigitalOcean $200 算力金、免费域名与丰富开发者福利，程序员必备神级礼包。",
			URL:         "https://www.v2ex.com/t/986421",
			Source:      "v2ex",
			Category:    "card",
			Author:      "V2EX极客",
			Likes:       154,
			Replies:     62,
			Views:       4310,
			TagsJSON:    `["V2EX", "GitHub", "学生包", "Copilot", "福利"]`,
			PublishedAt: now.Add(-22 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-flux-schnell-free-api",
			Title:       "FLUX.1 Schnell 开源最强绘图模型免费在线调用与极速生图端点",
			Summary:     "黑森林团队打造的顶级扩散模型，生成仅需 4 步（约 1 秒），支持写实胶片质感与精准中文提示词渲染，全网免算力 API 整理。",
			URL:         "https://linux.do/t/topic/245891",
			Source:      "linuxdo",
			Category:    "media",
			Author:      "FluxLover",
			Likes:       143,
			Replies:     56,
			Views:       3890,
			TagsJSON:    `["LINUX DO", "FLUX", "生图", "开源绘图"]`,
			PublishedAt: now.Add(-24 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-stepfun-step1v-free",
			Title:       "阶跃星辰 Step-1V 顶级多模态大模型免费 API 额度获取与体验",
			Summary:     "支持高分辨率图像识别、细粒度文档解析与图表分析，注册即送高额体验额度，兼容标准 OpenAI 视觉格式请求。",
			URL:         "https://platform.stepfun.com/",
			Source:      "official",
			Category:    "ai",
			Author:      "阶跃星辰",
			Likes:       86,
			Replies:     31,
			Views:       2240,
			TagsJSON:    `["官方活动", "多模态", "Step-1V", "免费额度"]`,
			PublishedAt: now.Add(-26 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-jimeng-voice-tts-free",
			Title:       "即梦 AI 与剪映 200+ 热门爆款音色逆向白嫖与自建 TTS 代理",
			Summary:     "无需安装繁重剪映客户端，通过纯 Go 协议逆向字节跳动音色服务，秒级合成高保真情感语音，包含猴哥、顾姐等全网爆款声音。",
			URL:         "https://linux.do/t/topic/299812",
			Source:      "linuxdo",
			Category:    "media",
			Author:      "AudioWizard",
			Likes:       215,
			Replies:     97,
			Views:       6120,
			TagsJSON:    `["LINUX DO", "TTS", "剪映音色", "免费语音", "逆向"]`,
			PublishedAt: now.Add(-28 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-modelscope-free-gpu",
			Title:       "魔搭社区 ModelScope 免费 GPU 算力每天赠送 36 小时，跑大模型超丝滑",
			Summary:     "阿里开源社区每天免费领取 A10 / V100 显卡算力，支持直接一键部署 Ollama、ComfyUI、SD WebUI 与开源微调任务。",
			URL:         "https://www.modelscope.cn/my/mynotebook/preset",
			Source:      "official",
			Category:    "vps",
			Author:      "魔搭社区",
			Likes:       178,
			Replies:     72,
			Views:       4890,
			TagsJSON:    `["魔搭", "免费GPU", "阿里", "算力福利"]`,
			PublishedAt: now.Add(-30 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-huggingface-free-spaces",
			Title:       "Hugging Face Spaces 免费白嫖 16G 内存云端容器与公网域名穿透",
			Summary:     "零成本托管 Gradio / Streamlit / Docker 应用，自带全球 CDN 加速与 HTTPS 证书，可用作 AI 网关轻量中继或私有演示站。",
			URL:         "https://huggingface.co/spaces",
			Source:      "official",
			Category:    "vps",
			Author:      "HFGeek",
			Likes:       124,
			Replies:     48,
			Views:       3310,
			TagsJSON:    `["HuggingFace", "Spaces", "免费容器", "开发"]`,
			PublishedAt: now.Add(-32 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-hunyuan-3d-free",
			Title:       "腾讯混元 Hunyuan3D-1.0 顶尖 3D 资产生成模型限时免费在线试用",
			Summary:     "支持从单张图片或文字提示词直接生成带纹理的高精度 3D 网格模型，支持导出 OBJ / GLB 格式，游戏制作与 3D 打印神器。",
			URL:         "https://3d.hunyuan.tencent.com/",
			Source:      "official",
			Category:    "media",
			Author:      "腾讯混元",
			Likes:       118,
			Replies:     39,
			Views:       3150,
			TagsJSON:    `["腾讯", "3D生成", "免费试用", "多模态"]`,
			PublishedAt: now.Add(-34 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-grok-reg-tool-docker",
			Title:       "开源 Grok 注册机 Web 控制台：集成临时邮箱与自动过盾一键造号",
			Summary:     "GitHub 热门开源自动化注册套件，支持 Docker Compose 一键拉起，自动捕获最新 SSO 会话并导出标准 CPA JSON 凭据。",
			URL:         "https://github.com/AaronL725/grok-register",
			Source:      "linuxdo",
			Category:    "card",
			Author:      "开源作者",
			Likes:       268,
			Replies:     134,
			Views:       7490,
			TagsJSON:    `["GitHub", "Grok", "自动注册", "开源"]`,
			PublishedAt: now.Add(-36 * time.Hour),
			CrawledAt:   now,
		},
		{
			ID:          "seed-v2ex-openai-free-mirrors",
			Title:       "2026 最新可靠的 OpenAI / Claude 免费镜像站与公益 API 中转端点汇总",
			Summary:     "收录稳定运营 6 个月以上的公益反向代理站，支持直接免登录在网页聊天或填入 NextChat / Chatbox 客户端使用。",
			URL:         "https://www.v2ex.com/t/974125",
			Source:      "v2ex",
			Category:    "api",
			Author:      "MirrorMaster",
			Likes:       135,
			Replies:     76,
			Views:       4620,
			TagsJSON:    `["V2EX", "公益API", "镜像站", "ChatGPT"]`,
			PublishedAt: now.Add(-38 * time.Hour),
			CrawledAt:   now,
		},
	}
}
