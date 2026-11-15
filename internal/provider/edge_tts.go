package provider

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/accounts"

	fhttp "github.com/bogdanfinn/fhttp"
	"github.com/bogdanfinn/websocket"
	"github.com/google/uuid"
)

const (
	edgeTTSHost          = "speech.platform.bing.com"
	edgeTTSBasePath      = "/consumer/speech/synthesize/readaloud"
	edgeTTSTrustedToken  = "6A5AA1D4EAFF4E9FB37E23D68491D6F4"
	edgeTTSOrigin        = "chrome-extension://jdiccldimpdaibmpdkjnbmckianbfold"
	edgeChromiumFullVer  = "143.0.3650.75"
	edgeChromiumMajorVer = "143"
	edgeSecMSGECVersion  = "2-" + edgeChromiumFullVer
	edgeWinEpochSec      = int64(11644473600)
	edgeClockQuantumSec  = int64(300)
	edgeTicksPerSecond   = int64(10_000_000)
)

// EdgeTTSVoice 描述微软 Edge 朗读音色
type EdgeTTSVoice struct {
	VoiceID     string
	DisplayName string
	Gender      string
	Locale      string
	Category    string
	SampleText  string
	Description string
}

// BuiltinEdgeVoices 预置 Edge-TTS 常用高品质音色（涵盖影视解说、多情感、方言及多语种）
var BuiltinEdgeVoices = []EdgeTTSVoice{
	{
		VoiceID:     "zh-CN-XiaoxiaoNeural",
		DisplayName: "微软晓晓 (知性温柔 · 影视解说大女主)",
		Gender:      "female",
		Locale:      "zh-CN",
		Category:    "影视解说",
		SampleText:  "注意看，眼前发生的一切彻底颠覆了所有人的认知...",
		Description: "短剧大女主、温柔知性、沉浸式纪录片旁白首选",
	},
	{
		VoiceID:     "zh-CN-YunxiNeural",
		DisplayName: "微软云希 (沉浸故事 · 情感男主)",
		Gender:      "male",
		Locale:      "zh-CN",
		Category:    "影视解说",
		SampleText:  "在这个城市的角落里，有许多不为人知的故事悄然发生...",
		Description: "沉浸式悬疑解说、情感叙事、男主第一人称对白",
	},
	{
		VoiceID:     "zh-CN-YunyangNeural",
		DisplayName: "微软云扬 (权威播报 · 冷酷霸总)",
		Gender:      "male",
		Locale:      "zh-CN",
		Category:    "影视解说",
		SampleText:  "全场肃静，今天这笔合同，签还是不签，你只有一次机会。",
		Description: "专业影视播报、冷酷权威、霸总短剧主力音色",
	},
	{
		VoiceID:     "zh-CN-XiaoyiNeural",
		DisplayName: "微软晓伊 (灵动甜美 · 都市女主)",
		Gender:      "female",
		Locale:      "zh-CN",
		Category:    "自然情感",
		SampleText:  "早安，今天阳光真好，一起去开启新的故事吧！",
		Description: "清脆灵动、现代甜宠剧女主与有声小说对白",
	},
	{
		VoiceID:     "zh-CN-YunjianNeural",
		DisplayName: "微软云健 (热血少年 · 武侠剑客)",
		Gender:      "male",
		Locale:      "zh-CN",
		Category:    "特色角色",
		SampleText:  "我辈修仙之人，何惧一战！今日便以此剑斩破虚妄！",
		Description: "热血青年、修仙对决、武侠玄幻男主对白",
	},
	{
		VoiceID:     "zh-CN-YunxiaNeural",
		DisplayName: "微软云夏 (少年正太 · 灵动配角)",
		Gender:      "male",
		Locale:      "zh-CN",
		Category:    "特色角色",
		SampleText:  "嘻嘻，你以为你藏得很好吗？我早就发现你啦！",
		Description: "活泼少年音、二次元角色、古灵精怪弟弟",
	},
	{
		VoiceID:     "zh-CN-liaoning-XiaobeiNeural",
		DisplayName: "辽宁晓北 (幽默东北话 · 特色解说)",
		Gender:      "female",
		Locale:      "zh-CN-liaoning",
		Category:    "地域方言",
		SampleText:  "哎呀妈呀老铁，你可别逗我了，这玩意儿能靠谱吗？",
		Description: "东北方言特色短剧、搞笑解说、市井生活喜剧",
	},
	{
		VoiceID:     "zh-CN-shaanxi-XiaoniNeural",
		DisplayName: "陕西晓妮 (地道陕西方言 · 西北风味)",
		Gender:      "female",
		Locale:      "zh-CN-shaanxi",
		Category:    "地域方言",
		SampleText:  "额跟你说，这事儿办得真是嘹咋咧，没得挑！",
		Description: "关中/陕西方言短剧、地域特色短视频",
	},
	{
		VoiceID:     "zh-HK-HiuGaaiNeural",
		DisplayName: "香港晓佳 (粤语女声 · 港风情怀)",
		Gender:      "female",
		Locale:      "zh-HK",
		Category:    "地域方言",
		SampleText:  "做人最緊要開心，得唔得，試過先至知㗎嘛。",
		Description: "港风警匪剧、TVB经典对白、地道粤语旁白",
	},
	{
		VoiceID:     "zh-HK-HiuMaanNeural",
		DisplayName: "香港晓曼 (粤语知性 · 都市女主)",
		Gender:      "female",
		Locale:      "zh-HK",
		Category:    "地域方言",
		SampleText:  "各位聽眾朋友晚上好，歡迎收聽今集故事時間。",
		Description: "温婉港音都市剧女主、粤语电台播讲",
	},
	{
		VoiceID:     "zh-HK-WanLungNeural",
		DisplayName: "香港云龙 (粤语男声 · 港片硬汉)",
		Gender:      "male",
		Locale:      "zh-HK",
		Category:    "地域方言",
		SampleText:  "呢件事交俾我處理，你放心返屋企等消息啦。",
		Description: "香港警匪男主、地道粤语广播男声",
	},
	{
		VoiceID:     "zh-TW-HsiaoChenNeural",
		DisplayName: "台湾晓臻 (甜美台偶 · 软糯腔调)",
		Gender:      "female",
		Locale:      "zh-TW",
		Category:    "地域方言",
		SampleText:  "你到底在讲什么啦，人家真的很认真在跟你说耶。",
		Description: "台偶偶像剧、软萌女友对白、甜美抒情电台",
	},
	{
		VoiceID:     "zh-TW-HsiaoYuNeural",
		DisplayName: "台湾晓雨 (清甜文艺 · 台湾女声)",
		Gender:      "female",
		Locale:      "zh-TW",
		Category:    "地域方言",
		SampleText:  "今天的微风好舒服，一起走去海边看看夕阳吧。",
		Description: "青春校园、有声书与清新抒情旁白",
	},
	{
		VoiceID:     "zh-TW-YunJheNeural",
		DisplayName: "台湾云哲 (台湾男声 · 温润叙事)",
		Gender:      "male",
		Locale:      "zh-TW",
		Category:    "地域方言",
		SampleText:  "其实有些话，我一直放在心里，今天终于想对你说。",
		Description: "台湾偶像剧男主、温柔叙事男声",
	},
	{
		VoiceID:     "en-US-JennyNeural",
		DisplayName: "Jenny (标准美音 · 专业旁白)",
		Gender:      "female",
		Locale:      "en-US",
		Category:    "外语多语种",
		SampleText:  "Welcome to the next generation of artificial intelligence voice synthesis.",
		Description: "地道北美口音、商业演示、海外短剧双语出海",
	},
	{
		VoiceID:     "en-US-AriaNeural",
		DisplayName: "Aria (表现力美音 · 故事解说)",
		Gender:      "female",
		Locale:      "en-US",
		Category:    "外语多语种",
		SampleText:  "Every frame tells a story when the voice carries real emotion.",
		Description: "极佳叙事张力、电影预告片与美剧风格旁白",
	},
	{
		VoiceID:     "en-US-GuyNeural",
		DisplayName: "Guy (沉稳美音男声 · 商业解说)",
		Gender:      "male",
		Locale:      "en-US",
		Category:    "外语多语种",
		SampleText:  "Precision engineering delivers reliable results across every environment.",
		Description: "稳重男声、英文纪录片与品牌宣传片首选",
	},
	{
		VoiceID:     "en-US-AndrewMultilingualNeural",
		DisplayName: "Andrew (多语种男声 · 中英混读)",
		Gender:      "male",
		Locale:      "en-US",
		Category:    "外语多语种",
		SampleText:  "Every great story begins with a single unforgettable voice.",
		Description: "多语种混读自然流畅、纪录片与出海短剧男主",
	},
	{
		VoiceID:     "en-US-AvaMultilingualNeural",
		DisplayName: "Ava (多语种女声 · 中英混读)",
		Gender:      "female",
		Locale:      "en-US",
		Category:    "外语多语种",
		SampleText:  "Let us explore a world where creativity knows no boundaries.",
		Description: "中英混读自然、高品质海外广告与旁白",
	},
	{
		VoiceID:     "en-US-EmmaMultilingualNeural",
		DisplayName: "Emma (多语种清亮女声 · 叙述)",
		Gender:      "female",
		Locale:      "en-US",
		Category:    "外语多语种",
		SampleText:  "Clear storytelling connects audiences across languages effortlessly.",
		Description: "清亮多语种女声、跨语种短剧与知识解说",
	},
	{
		VoiceID:     "en-US-BrianMultilingualNeural",
		DisplayName: "Brian (多语种温厚男声 · 旁白)",
		Gender:      "male",
		Locale:      "en-US",
		Category:    "外语多语种",
		SampleText:  "From concept to final scene, every line matters.",
		Description: "深沉自然多语种男声、适合双语剧情解说",
	},
	{
		VoiceID:     "ja-JP-NanamiNeural",
		DisplayName: "七海 Nanami (标准日语 · 温柔女声)",
		Gender:      "female",
		Locale:      "ja-JP",
		Category:    "外语多语种",
		SampleText:  "こんにちは、今日も素敵な一日になりますように。",
		Description: "二次元番剧、日语有声书、多语种出海",
	},
	{
		VoiceID:     "ja-JP-KeitaNeural",
		DisplayName: "圭太 Keita (标准日语 · 青年男声)",
		Gender:      "male",
		Locale:      "ja-JP",
		Category:    "外语多语种",
		SampleText:  "本日の予定をご案内いたします。準備はよろしいでしょうか。",
		Description: "日语男主对白与播报旁白",
	},
}

// EdgeTTSProvider 微软 Edge-TTS 纯免密、零额度消耗官方语音合成插件
type EdgeTTSProvider struct {
	mu           sync.RWMutex
	stats        accounts.PoolStats
	clockSkewSec float64
	lastHealthy  bool
	lastError    string
	cachedVoices []VoiceDescriptor
	lastFetched  time.Time
}

// NewEdgeTTSProvider 创建 Edge-TTS 插件实例
func NewEdgeTTSProvider() *EdgeTTSProvider {
	return &EdgeTTSProvider{
		stats: accounts.PoolStats{
			Total:       1,
			Active:      1,
			NoAuthCount: 1,
		},
		lastHealthy: true,
	}
}

func (p *EdgeTTSProvider) Descriptor() ProviderDescriptor {
	featured := make([]string, 0, len(BuiltinEdgeVoices))
	for _, v := range BuiltinEdgeVoices {
		featured = append(featured, v.VoiceID)
	}

	return ProviderDescriptor{
		ID:                    "edge_tts",
		Name:                  "Edge-TTS (微软官方免密语音)",
		Vendor:                "Microsoft",
		Status:                "enabled",
		Version:               "2026.2",
		Description:           "微软 Edge 浏览器大声朗读原生引擎（晓晓、云希、云扬及全量方言/多语种音色，支持 Sec-MS-GEC v2 握手与长文本自动分片）",
		SupportedAccountTypes: []string{"noauth", "anonymous"},
		CredentialFormats:     []string{"none"},
		Capabilities: []string{
			"tts",
			"stream",
		},
		FeaturedModels: featured,
		Tags:           MakeTags("permanent-free", "no-auth", "tts-presets", "official-sla"),
		CostType:       "permanent_free",
		AuthType:       "no_auth",
	}
}

func (p *EdgeTTSProvider) Stats() accounts.PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.stats
}

// HealthSnapshot 返回最近一次调用的健康状态，用于前端可观测性展示
func (p *EdgeTTSProvider) HealthSnapshot() (healthy bool, lastErr string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastHealthy, p.lastError
}

func (p *EdgeTTSProvider) ListAccounts(typeFilter, statusFilter, search string) []accounts.AccountSnapshot {
	p.mu.RLock()
	healthy := p.lastHealthy
	lastErr := p.lastError
	p.mu.RUnlock()

	status := "active"
	if !healthy && lastErr != "" {
		status = "degraded"
	}
	return []accounts.AccountSnapshot{
		{
			ID:           "edge-tts-public-gateway",
			Email:        "edge-tts@microsoft.public",
			Type:         "noauth",
			Status:       status,
			PlanType:     "permanent_free",
			TokenPreview: "public-wss-v2",
			LastError:    lastErr,
		},
	}
}

func (p *EdgeTTSProvider) ImportCredentials(req ImportRequest) (ImportResult, error) {
	return ImportResult{
		Added:     0,
		Skipped:   0,
		Failed:    0,
		TotalPool: 1,
		Messages:  []string{"Edge-TTS 为微软官方免密通道，直接基于公共 WebSocket 调度，无需导入凭据"},
	}, nil
}

func (p *EdgeTTSProvider) ExecuteAccountAction(id, action string) (map[string]interface{}, error) {
	if action == "probe" || action == "test" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		audio, err := p.Synthesize(ctx, "测试微软Edge语音合成连通性", "zh-CN-XiaoxiaoNeural", 1.0)
		if err != nil {
			return nil, fmt.Errorf("edge-tts probe failed: %w", err)
		}
		return map[string]interface{}{
			"status":     "healthy",
			"bytes_size": len(audio),
			"timestamp":  time.Now().Unix(),
		}, nil
	}
	return nil, fmt.Errorf("unsupported action: %s", action)
}

// Synthesize 纯 Go WebSocket 合成 Edge-TTS 音频（支持长文本分片、时钟校准重试与 v2 握手签名）
func (p *EdgeTTSProvider) Synthesize(ctx context.Context, text, voice string, speed float64) ([]byte, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, ErrEmptyTTSText
	}
	if strings.TrimSpace(voice) == "" {
		voice = "zh-CN-XiaoxiaoNeural"
	}
	if speed <= 0 {
		speed = 1.0
	}

	chunks := SplitTextForTTS(trimmed, 1000)
	if len(chunks) == 0 {
		return nil, ErrEmptyTTSText
	}

	var combined bytes.Buffer
	for _, chunk := range chunks {
		audio, err := p.synthesizeChunkWithRetry(ctx, chunk, voice, speed)
		if err != nil {
			if !strings.Contains(err.Error(), "Unsupported voice") {
				p.markHealth(false, err.Error())
			}
			return nil, err
		}
		combined.Write(audio)
	}
	p.markHealth(true, "")
	return combined.Bytes(), nil
}

func (p *EdgeTTSProvider) markHealth(ok bool, errMsg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastHealthy = ok
	p.lastError = errMsg
	if ok {
		p.stats.Active = 1
	}
}

func (p *EdgeTTSProvider) synthesizeChunkWithRetry(ctx context.Context, text, voice string, speed float64) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			waitDur := time.Duration(attempt) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(waitDur):
			}
		}

		audio, status, serverDate, err := p.synthesizeOnce(ctx, text, voice, speed)
		if err == nil {
			return audio, nil
		}
		lastErr = err

		if status == http.StatusForbidden && serverDate != "" {
			if parsed, parseErr := http.ParseTime(serverDate); parseErr == nil {
				p.mu.Lock()
				clientNow := float64(time.Now().UTC().UnixNano())/1e9 + p.clockSkewSec
				p.clockSkewSec += float64(parsed.UTC().Unix()) - clientNow
				p.mu.Unlock()
				continue
			}
		}

		msg := err.Error()
		transient := strings.Contains(msg, "empty audio") ||
			strings.Contains(msg, "EOF") ||
			strings.Contains(msg, "reset by peer") ||
			strings.Contains(msg, "timeout")
		if !transient {
			break
		}
	}
	return nil, lastErr
}

func (p *EdgeTTSProvider) synthesizeOnce(ctx context.Context, text, voice string, speed float64) ([]byte, int, string, error) {
	p.mu.RLock()
	skew := p.clockSkewSec
	p.mu.RUnlock()

	nowUnix := float64(time.Now().UTC().UnixNano())/1e9 + skew
	secMSGEC := generateEdgeSecMSGECv2(nowUnix)
	connID := strings.ReplaceAll(uuid.New().String(), "-", "")

	wssURL := fmt.Sprintf(
		"wss://%s%s/edge/v1?TrustedClientToken=%s&ConnectionId=%s&Sec-MS-GEC=%s&Sec-MS-GEC-Version=%s",
		edgeTTSHost,
		edgeTTSBasePath,
		edgeTTSTrustedToken,
		url.QueryEscape(connID),
		url.QueryEscape(secMSGEC),
		url.QueryEscape(edgeSecMSGECVersion),
	)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		Proxy:            fhttp.ProxyFromEnvironment,
	}

	header := fhttp.Header{}
	header.Set("Pragma", "no-cache")
	header.Set("Cache-Control", "no-cache")
	header.Set("Origin", edgeTTSOrigin)
	header.Set("User-Agent", fmt.Sprintf("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s.0.0.0 Safari/537.36 Edg/%s.0.0.0", edgeChromiumMajorVer, edgeChromiumMajorVer))
	header.Set("Accept-Language", "en-US,en;q=0.9")
	header.Set("Cookie", "muid="+randomEdgeMUID()+";")

	conn, resp, err := dialer.DialContext(ctx, wssURL, header)
	if err != nil {
		status := 0
		serverDate := ""
		if resp != nil {
			status = resp.StatusCode
			serverDate = resp.Header.Get("Date")
		}
		return nil, status, serverDate, fmt.Errorf("dial edge tts wss failed (status=%d): %w", status, err)
	}
	defer conn.Close()
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancel()

	ts := formatEdgeTimestamp(time.Now().UTC())
	configMsg := fmt.Sprintf(
		"X-Timestamp:%s\r\nContent-Type:application/json; charset=utf-8\r\nPath:speech.config\r\n\r\n{\"context\":{\"synthesis\":{\"audio\":{\"metadataoptions\":{\"sentenceBoundaryEnabled\":\"false\",\"wordBoundaryEnabled\":\"false\"},\"outputFormat\":\"audio-24khz-48kbitrate-mono-mp3\"}}}}\r\n",
		ts,
	)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(configMsg)); err != nil {
		return nil, 0, "", err
	}

	ratePct := int(math.Round((speed - 1.0) * 100))
	if ratePct > 100 {
		ratePct = 100
	} else if ratePct < -50 {
		ratePct = -50
	}
	rateStr := fmt.Sprintf("%+d%%", ratePct)

	escapedText := sanitizeEdgeXML(text)
	reqID := strings.ReplaceAll(uuid.New().String(), "-", "")
	ssml := fmt.Sprintf(
		"<speak version='1.0' xmlns='http://www.w3.org/2001/10/synthesis' xml:lang='en-US'><voice name='%s'><prosody pitch='+0Hz' rate='%s' volume='+0%%'>%s</prosody></voice></speak>",
		voice, rateStr, escapedText,
	)
	ssmlMsg := fmt.Sprintf(
		"X-RequestId:%s\r\nContent-Type:application/ssml+xml\r\nX-Timestamp:%sZ\r\nPath:ssml\r\n\r\n%s",
		reqID, formatEdgeTimestamp(time.Now().UTC()), ssml,
	)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(ssmlMsg)); err != nil {
		return nil, 0, "", err
	}
	if err := conn.SetReadDeadline(time.Now().Add(20 * time.Second)); err != nil {
		return nil, 0, "", err
	}

	var audioBuf []byte
	for {
		select {
		case <-ctx.Done():
			return nil, 0, "", ctx.Err()
		default:
		}

		messageType, pData, readErr := conn.ReadMessage()
		if readErr != nil {
			if ctx.Err() != nil {
				return nil, 0, "", ctx.Err()
			}
			if len(audioBuf) > 0 {
				break
			}
			return nil, 0, "", fmt.Errorf("read edge tts wss failed: %w", readErr)
		}

		switch messageType {
		case websocket.TextMessage:
			if strings.Contains(string(pData), "Path:turn.end") {
				if len(audioBuf) == 0 {
					return nil, 0, "", errors.New("edge-tts returned turn.end with empty audio")
				}
				return audioBuf, http.StatusOK, "", nil
			}
		case websocket.BinaryMessage:
			if len(pData) > 2 {
				headerLen := int(pData[0])<<8 | int(pData[1])
				if len(pData) > headerLen+2 {
					audioBuf = append(audioBuf, pData[headerLen+2:]...)
				}
			}
		}
	}

	if len(audioBuf) == 0 {
		return nil, 0, "", errors.New("no audio received from edge-tts")
	}
	return audioBuf, http.StatusOK, "", nil
}

// FetchLiveVoices 从微软 Edge 官方 /voices/list 接口拉取在线全量音色目录并转换为网关标准清单
func (p *EdgeTTSProvider) FetchLiveVoices(ctx context.Context) ([]VoiceDescriptor, error) {
	voicesURL := fmt.Sprintf("https://%s%s/voices/list?trustedclienttoken=%s", edgeTTSHost, edgeTTSBasePath, edgeTTSTrustedToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, voicesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authority", edgeTTSHost)
	req.Header.Set("User-Agent", fmt.Sprintf("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%s.0.0.0 Safari/537.36 Edg/%s.0.0.0", edgeChromiumMajorVer, edgeChromiumMajorVer))
	req.Header.Set("Accept", "*/*")

	client := &http.Client{
		Timeout: 12 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("edge voices list status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var raw []struct {
		ShortName string `json:"ShortName"`
		Gender    string `json:"Gender"`
		Locale    string `json:"Locale"`
		Status    string `json:"Status"`
		VoiceTag  struct {
			ContentCategories  []string `json:"ContentCategories"`
			VoicePersonalities []string `json:"VoicePersonalities"`
		} `json:"VoiceTag"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	rawLive := make(map[string]bool, len(raw))
	for _, item := range raw {
		id := strings.TrimSpace(item.ShortName)
		if id != "" && isSupportedEdgeLocale(item.Locale) {
			rawLive[id] = true
		}
	}

	out := make([]VoiceDescriptor, 0, len(rawLive))
	seen := make(map[string]bool, len(rawLive))
	// 先放入仍在微软在线列表中存活的预置精选（保持优质中文文案与展示排序）
	for _, v := range GetEdgeTTSVoiceDescriptors() {
		if len(rawLive) > 0 && !rawLive[v.ID] {
			continue
		}
		out = append(out, v)
		seen[v.ID] = true
	}

	// 再合并微软在线返回的其他可用中文 / 粤语 / 台语 / 英 / 日 / 韩音色
	for _, item := range raw {
		id := strings.TrimSpace(item.ShortName)
		if id == "" || seen[id] {
			continue
		}
		if !isSupportedEdgeLocale(item.Locale) {
			continue
		}
		seen[id] = true

		gender := "female"
		if strings.EqualFold(item.Gender, "Male") {
			gender = "male"
		}
		category := "外语多语种"
		sample := "Hello, this is a sample of natural neural voice synthesis."
		if strings.HasPrefix(item.Locale, "zh-CN") {
			if strings.Contains(id, "-") && strings.Count(id, "-") >= 3 {
				category = "地域方言"
			} else {
				category = "自然情感"
			}
			sample = fmt.Sprintf("你好，我是微软神经语音 %s，这是一段中文配音试听。", strings.TrimSuffix(strings.TrimPrefix(id, "zh-CN-"), "Neural"))
		} else if strings.HasPrefix(item.Locale, "zh-HK") || strings.HasPrefix(item.Locale, "zh-TW") {
			category = "地域方言"
			sample = "你好，這是一段繁體中文與粵語語音合成試聽。"
		} else if strings.HasPrefix(item.Locale, "ja-JP") {
			sample = "こんにちは、これは自然な音声合成のサンプルです。"
		} else if strings.HasPrefix(item.Locale, "ko-KR") {
			sample = "안녕하세요, 자연스러운 음성 합성 샘플입니다."
		}

		tags := []string{"微软Edge", item.Locale}
		if len(item.VoiceTag.VoicePersonalities) > 0 {
			tags = append(tags, item.VoiceTag.VoicePersonalities[0])
		}

		out = append(out, VoiceDescriptor{
			ID:         id,
			Name:       formatEdgeVoiceName(id, item.Locale, gender),
			Provider:   "edge_tts",
			Category:   category,
			Gender:     gender,
			Locale:     item.Locale,
			SampleText: sample,
			Tags:       tags,
			Available:  true,
		})
	}

	p.mu.Lock()
	p.cachedVoices = out
	p.lastFetched = time.Now()
	p.mu.Unlock()
	p.markHealth(true, "")
	return out, nil
}

// GetEdgeTTSVoiceDescriptors 转换为通用的 VoiceDescriptor 清单（含缓存动态拉取结果）
func (p *EdgeTTSProvider) VoiceDescriptors() []VoiceDescriptor {
	p.mu.RLock()
	cached := p.cachedVoices
	healthy := p.lastHealthy
	lastErr := p.lastError
	p.mu.RUnlock()

	if len(cached) > 0 {
		out := make([]VoiceDescriptor, len(cached))
		copy(out, cached)
		for i := range out {
			out[i].Available = healthy
			if !healthy && lastErr != "" {
				out[i].StatusReason = lastErr
			}
		}
		return out
	}

	out := GetEdgeTTSVoiceDescriptors()
	for i := range out {
		out[i].Available = healthy
		if !healthy && lastErr != "" {
			out[i].StatusReason = lastErr
		}
	}
	return out
}

// SeedCachedVoices 从本地磁盘目录加载之前同步下来的音色清单
func (p *EdgeTTSProvider) SeedCachedVoices(voices []VoiceDescriptor) {
	if len(voices) == 0 {
		return
	}
	filtered := make([]VoiceDescriptor, 0, len(voices))
	for _, v := range voices {
		if v.Provider == "edge_tts" {
			filtered = append(filtered, v)
		}
	}
	if len(filtered) == 0 {
		return
	}
	p.mu.Lock()
	p.cachedVoices = filtered
	p.mu.Unlock()
}

// GetEdgeTTSVoiceDescriptors 转换为通用的预置 VoiceDescriptor 清单
func GetEdgeTTSVoiceDescriptors() []VoiceDescriptor {
	out := make([]VoiceDescriptor, 0, len(BuiltinEdgeVoices))
	for _, v := range BuiltinEdgeVoices {
		out = append(out, VoiceDescriptor{
			ID:         v.VoiceID,
			Name:       v.DisplayName,
			Provider:   "edge_tts",
			Category:   v.Category,
			Gender:     v.Gender,
			Locale:     v.Locale,
			SampleText: v.SampleText,
			Tags:       []string{"微软官方", "免密免费", "神经音色"},
			Available:  true,
		})
	}
	return out
}

// MapOpenAIVoiceToEdge 将 OpenAI 标准音色名映射为音色区分度明确且经微软在线验证存活的 Edge-TTS 神经音色
func MapOpenAIVoiceToEdge(voice string) string {
	switch strings.ToLower(strings.TrimSpace(voice)) {
	case "alloy":
		return "zh-CN-XiaoxiaoNeural"
	case "ash":
		return "zh-TW-YunJheNeural"
	case "coral":
		return "zh-CN-XiaoyiNeural"
	case "echo":
		return "zh-CN-YunxiNeural"
	case "fable":
		return "zh-CN-YunjianNeural"
	case "onyx":
		return "zh-CN-YunyangNeural"
	case "nova":
		return "zh-TW-HsiaoChenNeural"
	case "sage":
		return "en-US-JennyNeural"
	case "shimmer":
		return "zh-CN-YunxiaNeural"
	default:
		return "zh-CN-XiaoxiaoNeural"
	}
}

// MapEdgeVoiceToJianying 保留兼容旧测试与性别区分映射
func MapEdgeVoiceToJianying(voice string) string {
	switch strings.ToLower(strings.TrimSpace(voice)) {
	case "zh-cn-xiaoxiaoneural":
		return "zh_female_shuangkuai"
	case "zh-cn-yunxineural":
		return "zh_male_chunhou"
	case "zh-cn-yunyangneural":
		return "zh_male_chenwen"
	case "zh-cn-yunxianeural":
		return "zh_female_tianmei"
	case "zh-cn-yunjianneural":
		return "zh_male_yangguang"
	case "zh-cn-liaoning-xiaobeineural":
		return "zh_female_sichuan"
	case "zh-hk-hiugaaineural":
		return "zh_female_yueyu"
	case "zh-tw-hsiaochenneural":
		return "zh_female_taiwan"
	case "en-us-jennyneural":
		return "en_female_emotional"
	default:
		return MapOpenAIVoiceToJianying(voice)
	}
}

func isSupportedEdgeLocale(locale string) bool {
	return strings.HasPrefix(locale, "zh-") ||
		strings.HasPrefix(locale, "en-US") ||
		strings.HasPrefix(locale, "en-GB") ||
		strings.HasPrefix(locale, "ja-JP") ||
		strings.HasPrefix(locale, "ko-KR")
}

func formatEdgeVoiceName(id, locale, gender string) string {
	parts := strings.Split(id, "-")
	short := id
	if len(parts) > 0 {
		short = strings.TrimSuffix(parts[len(parts)-1], "Neural")
	}
	genderLabel := "女声"
	if gender == "male" {
		genderLabel = "男声"
	}
	return fmt.Sprintf("Edge %s (%s · %s)", short, locale, genderLabel)
}

func inferLocaleFromVoice(voice string) string {
	parts := strings.Split(voice, "-")
	if len(parts) >= 2 {
		return parts[0] + "-" + parts[1]
	}
	return "zh-CN"
}

func sanitizeEdgeXML(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if (r >= 0 && r <= 8) || (r >= 11 && r <= 12) || (r >= 14 && r <= 31) {
			b.WriteByte(' ')
			continue
		}
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func formatEdgeTimestamp(t time.Time) string {
	return t.UTC().Format("Mon Jan 02 2006 15:04:05") + " GMT+0000 (Coordinated Universal Time)"
}

func randomEdgeMUID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return strings.ToUpper(hex.EncodeToString(buf[:]))
}

// ── Edge Read Aloud Sec-MS-GEC v2 握手令牌计算 ────────────────────────────

var (
	edgeSHAInitial = [8]uint32{
		0x6A09E667, 0xBB67AE85, 0x3C6EF372, 0xA54FF53A,
		0x510E527F, 0x9B05688C, 0x1F83D9AB, 0x5BE0CD19,
	}
	edgeSHAStateSeed = [8]uint32{
		0xFD9FBEA2, 0x91243CE7, 0xBE499094, 0xCD2E1E0A,
		0x6C006F49, 0x04BD35AB, 0xFEB0A7E4, 0x03EFFBD2,
	}
	edgeSHASchedule = [14]uint32{
		0xEB24BF9E, 0x1558691F, 0xA0C09EE2, 0x0CFA32BA,
		0xA622EE8C, 0xD1DD9491, 0xC0DD25A0, 0xF9E70AB6,
		0xF0C31573, 0x7058CE4E, 0x7188E606, 0x00E5AB33,
		0xD258B060, 0x72766564,
	}
	edgeSHARoundConstants = [64]uint32{
		0x428A2F98, 0x71374491, 0xB5C0FBCF, 0xE9B5DBA5,
		0x3956C25B, 0x59F111F1, 0x923F82A4, 0xAB1C5ED5,
		0xD807AA98, 0x12835B01, 0x243185BE, 0x550C7DC3,
		0x72BE5D74, 0x80DEB1FE, 0x9BDC06A7, 0xC19BF174,
		0xE49B69C1, 0xEFBE4786, 0x0FC19DC6, 0x240CA1CC,
		0x2DE92C6F, 0x4A7484AA, 0x5CB0A9DC, 0x76F988DA,
		0x983E5152, 0xA831C66D, 0xB00327C8, 0xBF597FC7,
		0xC6E00BF3, 0xD5A79147, 0x06CA6351, 0x14292967,
		0x27B70A85, 0x2E1B2138, 0x4D2C6DFC, 0x53380D13,
		0x650A7354, 0x766A0ABB, 0x81C2C92E, 0x92722C85,
		0xA2BFE8A1, 0xA81A664B, 0xC24B8B70, 0xC76C51A3,
		0xD192E819, 0xD6990624, 0xF40E3585, 0x106AA070,
		0x19A4C116, 0x1E376C08, 0x2748774C, 0x34B0BCB5,
		0x391C0CB3, 0x4ED8AA4A, 0x5B9CCA4F, 0x682E6FF3,
		0x748F82EE, 0x78A5636F, 0x84C87814, 0x8CC70208,
		0x90BEFFFA, 0xA4506CEB, 0xBEF9A3F7, 0xC67178F2,
	}
	edgeCS64Coeffs = [14][2]uint32{
		{0xE82E91B1, 0xB32700D0},
		{0x44DE24C6, 0x88DFF51D},
		{0x3BEB65BC, 0x29514038},
		{0xC89C5933, 0xB328CBDF},
		{0x8D1EC211, 0xD405B4D5},
		{0x314EF5D5, 0x26E55F3C},
		{0xE92491B1, 0x607801C6},
		{0xF0A5B9D5, 0x82B2C1FB},
		{0x06064CF3, 0xFA51A3DA},
		{0xDA79FA60, 0x207CC464},
		{0xBED20560, 0x3C3C74F3},
		{0x89F9C9F8, 0xD2D14B61},
		{0x35FA38C6, 0xBB04A2C1},
		{0xCB49E3C6, 0xC5626167},
	}
)

func generateEdgeSecMSGECv2(unixTimestampSec float64) string {
	ticks := int64(math.Floor(unixTimestampSec)) + edgeWinEpochSec
	ticks -= ticks % edgeClockQuantumSec
	ticks *= edgeTicksPerSecond
	data := []byte(fmt.Sprintf("%d%s", ticks, edgeTTSTrustedToken))
	digest := edgeTokenDigest(data)
	csA, csB := edgeCS64Mix(data, digest)
	var csBuf [8]byte
	binary.LittleEndian.PutUint32(csBuf[0:4], csA)
	binary.LittleEndian.PutUint32(csBuf[4:8], csB)
	return strings.ToUpper(hex.EncodeToString(csBuf[:])) + "-" + strings.ToUpper(hex.EncodeToString(digest[:]))
}

func rotr32(v uint32, n uint) uint32 {
	return (v >> n) | (v << (32 - n))
}

func rol16(v uint32) uint32 {
	return (v << 16) | (v >> 16)
}

func edgeSigma0(v uint32) uint32 {
	return rotr32(v, 7) ^ rotr32(v, 18) ^ (v >> 3)
}

func edgeSigma1(v uint32) uint32 {
	return rotr32(v, 17) ^ rotr32(v, 19) ^ (v >> 10)
}

func edgeRounds(state [8]uint32, words *[64]uint32, firstRound int) [8]uint32 {
	a, b, c, d, e, f, g, h := state[0], state[1], state[2], state[3], state[4], state[5], state[6], state[7]
	const adjRound14 uint32 = 0xC9DC06A7
	for idx := firstRound; idx < 64; idx++ {
		rc := edgeSHARoundConstants[idx]
		if firstRound == 14 && idx == 14 {
			rc = adjRound14
		}
		s1 := rotr32(e, 6) ^ rotr32(e, 11) ^ rotr32(e, 25)
		ch := (e & f) ^ ((^e) & g)
		t1 := h + s1 + ch + rc + words[idx]
		s0 := rotr32(a, 2) ^ rotr32(a, 13) ^ rotr32(a, 22)
		maj := (a & b) ^ (a & c) ^ (b & c)
		t2 := s0 + maj
		h = g
		g = f
		f = e
		e = d + t1
		d = c
		c = b
		b = a
		a = t1 + t2
	}
	return [8]uint32{a, b, c, d, e, f, g, h}
}

func edgeInitState(prefix []byte) [8]uint32 {
	var words [64]uint32
	words[14] = uint32(prefix[0])<<16 | uint32(prefix[1])<<8 | uint32(prefix[2])
	words[15] = binary.BigEndian.Uint32(prefix[3:7])
	for idx := 16; idx < 30; idx++ {
		v := edgeSHASchedule[idx-16]
		s1 := edgeSigma1(words[idx-2])
		if idx == 16 {
			s1 ^= 0x000B92C0
		}
		v += s1
		if idx >= 21 {
			v += words[idx-7]
		}
		if idx == 29 {
			v += edgeSigma0(words[idx-15]) ^ 0x059C0B80
		}
		words[idx] = v
	}
	words[30] = edgeSigma1(words[28]) + words[23] + edgeSigma0(words[15]) + words[14] + 0xC9DC06A7 - edgeSHARoundConstants[14]
	for idx := 31; idx < 64; idx++ {
		words[idx] = edgeSigma1(words[idx-2]) + words[idx-7] + edgeSigma0(words[idx-15]) + words[idx-16]
	}
	working := edgeRounds(edgeSHAStateSeed, &words, 14)
	var out [8]uint32
	for i := 0; i < 8; i++ {
		out[i] = edgeSHAInitial[i] + working[i]
	}
	return out
}

func edgeTokenDigest(data []byte) [32]byte {
	totalLen := 57 + len(data)
	padZeros := ((55 - totalLen)%64 + 64) % 64
	buf := make([]byte, 0, len(data)-7+1+padZeros+8)
	buf = append(buf, data[7:]...)
	buf = append(buf, 0x80)
	for i := 0; i < padZeros; i++ {
		buf = append(buf, 0)
	}
	var lenBuf [8]byte
	binary.BigEndian.PutUint64(lenBuf[:], uint64(totalLen)*8)
	buf = append(buf, lenBuf[:]...)

	state := edgeInitState(data[:7])
	for off := 0; off < len(buf); off += 64 {
		var words [64]uint32
		for i := 0; i < 16; i++ {
			words[i] = binary.BigEndian.Uint32(buf[off+i*4 : off+i*4+4])
		}
		for i := 16; i < 64; i++ {
			words[i] = edgeSigma1(words[i-2]) + words[i-7] + edgeSigma0(words[i-15]) + words[i-16]
		}
		working := edgeRounds(state, &words, 0)
		for i := 0; i < 8; i++ {
			state[i] += working[i]
		}
	}

	var out [32]byte
	for i := 0; i < 8; i++ {
		binary.BigEndian.PutUint32(out[i*4:i*4+4], state[i])
	}
	return out
}

func edgeCS64Mix(data []byte, digest [32]byte) (uint32, uint32) {
	keyA := binary.LittleEndian.Uint32(digest[0:4]) | 1
	keyB := binary.LittleEndian.Uint32(digest[4:8]) | 1
	const factor uint32 = 0x82893933
	facA := keyA * factor
	facB := keyB * factor

	terms := make([]uint32, 0, 14+len(data)/4+4)
	offsets := make([]uint32, 0, 14+len(data)/4+4)
	for i, c := range edgeCS64Coeffs {
		fac := facA
		if i%2 == 1 {
			fac = facB
		}
		terms = append(terms, c[0]*fac)
		offsets = append(offsets, c[1])
	}

	raw := make([]byte, 1+len(data))
	raw[0] = 0
	copy(raw[1:], data)
	for len(raw)%4 != 0 {
		raw = append(raw, 0)
	}
	wordCount := len(raw) / 4
	words := make([]uint32, wordCount)
	for i := 0; i < wordCount; i++ {
		words[i] = binary.LittleEndian.Uint32(raw[i*4 : i*4+4])
	}
	if len(words)%2 != 0 {
		words = append(words, 0)
	}

	for i, w := range words {
		key := keyB
		coeff := uint32(0xC31713DB)
		if i%2 == 0 {
			key = keyA
			coeff = 0xEF0569FB
		}
		term := w * key
		offset := rol16(w) * coeff
		if i == 0 {
			term += 0xF2F90B1A * facA
			offset += 0x0B1A0000
		}
		terms = append(terms, term)
		offsets = append(offsets, offset)
	}

	var swapAcc, swapLast, state, acc uint32
	for i := 0; i < len(terms); i += 2 {
		x, y := terms[i], terms[i+1]
		t := rol16(offsets[i] + x)
		q := t * 0x76F50F34
		swapAcc += rol16(q)*0xC3EFEA97 + q
		t = rol16(offsets[i+1] + y)
		q = t * 0x1881CE3C
		swapLast = rol16(q)*0x35BD1EC9 + q
		swapAcc += swapLast

		state = state*keyA + x
		state = rol16(state) * 0xCF98B111
		state = rol16(state) * 0x87085B9F
		state = rol16(state) * 0x12CEB96D
		state = rol16(state) * 0x257E1D83
		acc += state
		t = state*keyB + y
		t = rol16(t) * 0xA27416F5
		t = rol16(t) * 0xD38396FF
		state = rol16(rol16(t)*0x7C932B89) * 0xBFA49F69
		acc += state
	}
	return swapLast ^ state, swapAcc ^ acc
}
