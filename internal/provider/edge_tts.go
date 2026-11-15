package provider

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/accounts"

	fhttp "github.com/bogdanfinn/fhttp"
	"github.com/bogdanfinn/websocket"
	"github.com/google/uuid"
)

const (
	edgeTTSWSSURL = "wss://speech.platform.bing.com/consumer/speech/synthesize/readaheadwork/v1?TrustedClientToken=6A5AA1D4EA654941A6D454A62D3721F4"
	edgeTTSOrigin = "chrome-extension://jdiccldimpdaibmpdkjnbmckianbfold"
)

// EdgeTTSVoice 描述微软 Edge 朗读音色
type EdgeTTSVoice struct {
	VoiceID     string
	DisplayName string
	Gender      string
	Category    string
	SampleText  string
	Description string
}

// BuiltinEdgeVoices 预置 Edge-TTS 常用高品质音色
var BuiltinEdgeVoices = []EdgeTTSVoice{
	{
		VoiceID:     "zh-CN-XiaoxiaoNeural",
		DisplayName: "微软晓晓 (知性温柔 · 影视解说大女主)",
		Gender:      "female",
		Category:    "影视解说",
		SampleText:  "注意看，眼前发生的一切彻底颠覆了所有人的认知...",
		Description: "短剧大女主、温柔知性、沉浸式纪录片旁白首选",
	},
	{
		VoiceID:     "zh-CN-YunxiNeural",
		DisplayName: "微软云希 (沉浸故事 · 情感男主)",
		Gender:      "male",
		Category:    "影视解说",
		SampleText:  "在这个城市的角落里，有许多不为人知的故事悄然发生...",
		Description: "沉浸式悬疑解说、情感叙事、男主第一人称对白",
	},
	{
		VoiceID:     "zh-CN-YunyangNeural",
		DisplayName: "微软云扬 (权威播报 · 冷酷霸总)",
		Gender:      "male",
		Category:    "影视解说",
		SampleText:  "全场肃静，今天这笔合同，签还是不签，你只有一次机会。",
		Description: "专业影视播报、冷酷权威、霸总短剧主力音色",
	},
	{
		VoiceID:     "zh-CN-YunxiaNeural",
		DisplayName: "微软云夏 (活泼少女 · 灵动千金)",
		Gender:      "female",
		Category:    "特色搞怪",
		SampleText:  "嘻嘻，你以为你藏得很好吗？我早就发现你啦！",
		Description: "活泼灵动少女、豪门真千金、短剧二次元萌系角色",
	},
	{
		VoiceID:     "zh-CN-YunjianNeural",
		DisplayName: "微软云健 (热血少年 · 武侠剑客)",
		Gender:      "male",
		Category:    "自然情感",
		SampleText:  "我辈修仙之人，何惧一战！今日便以此剑斩破虚妄！",
		Description: "热血青年、修仙对决、武侠玄幻男主对白",
	},
	{
		VoiceID:     "zh-CN-liaoning-XiaobeiNeural",
		DisplayName: "辽宁晓北 (幽默东北话 · 特色解说)",
		Gender:      "female",
		Category:    "地域方言",
		SampleText:  "哎呀妈呀老铁，你可别逗我了，这玩意儿能靠谱吗？",
		Description: "东北方言特色短剧、搞笑解说、市井生活喜剧",
	},
	{
		VoiceID:     "zh-HK-HiuGaaiNeural",
		DisplayName: "香港晓佳 (粤语原声 · 港风情怀)",
		Gender:      "female",
		Category:    "地域方言",
		SampleText:  "做人最緊要開心，得唔得，試過先至知㗎嘛。",
		Description: "港风警匪剧、TVB经典对白、地道粤语旁白",
	},
	{
		VoiceID:     "zh-TW-HsiaoChenNeural",
		DisplayName: "台湾晓臻 (甜美台偶 · 软糯腔调)",
		Gender:      "female",
		Category:    "自然情感",
		SampleText:  "你到底在讲什么啦，人家真的很认真在跟你说耶。",
		Description: "台偶偶像剧、软萌女友对白、甜美抒情电台",
	},
	{
		VoiceID:     "en-US-JennyNeural",
		DisplayName: "Jenny (标准美音 · 专业旁白)",
		Gender:      "female",
		Category:    "外语英语",
		SampleText:  "Welcome to the next generation of artificial intelligence voice synthesis.",
		Description: "地道北美口音、商业演示、海外短剧双语出海",
	},
}

// EdgeTTSProvider 微软 Edge-TTS 纯免密、零额度消耗官方语音合成插件
type EdgeTTSProvider struct {
	mu    sync.RWMutex
	stats accounts.PoolStats
}

// NewEdgeTTSProvider 创建 Edge-TTS 插件实例
func NewEdgeTTSProvider() *EdgeTTSProvider {
	return &EdgeTTSProvider{
		stats: accounts.PoolStats{
			Total:       1,
			Active:      1,
			NoAuthCount: 1,
		},
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
		Version:               "2026.1",
		Description:           "微软 Edge 浏览器大声朗读原生引擎（晓晓、云希、云扬等影视解说神级音色，纯 Go WSS 直连，零登录无额度限制）",
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

func (p *EdgeTTSProvider) ListAccounts(typeFilter, statusFilter, search string) []accounts.AccountSnapshot {
	return []accounts.AccountSnapshot{
		{
			ID:           "edge-tts-public-gateway",
			Email:        "edge-tts@microsoft.public",
			Type:         "noauth",
			Status:       "active",
			PlanType:     "permanent_free",
			TokenPreview: "public-wss-unlimited",
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
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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

// Synthesize 纯 Go WebSocket 合成 Edge-TTS 音频
func (p *EdgeTTSProvider) Synthesize(ctx context.Context, text, voice string, speed float64) ([]byte, error) {
	if voice == "" {
		voice = "zh-CN-XiaoxiaoNeural"
	}
	if speed <= 0 {
		speed = 1.0
	}

	ratePct := int((speed - 1.0) * 100)
	rateStr := fmt.Sprintf("%+d%%", ratePct)

	escapedText := strings.ReplaceAll(text, "&", "&amp;")
	escapedText = strings.ReplaceAll(escapedText, "<", "&lt;")
	escapedText = strings.ReplaceAll(escapedText, ">", "&gt;")

	reqID := strings.ReplaceAll(uuid.New().String(), "-", "")
	ssml := fmt.Sprintf(`<speak version='1.0' xmlns='http://www.w3.org/2001/10/synthesis' xml:lang='zh-CN'><voice name='%s'><prosody rate='%s' pitch='+0Hz'>%s</prosody></voice></speak>`,
		voice, rateStr, escapedText)

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
		Proxy:            fhttp.ProxyFromEnvironment,
	}

	header := fhttp.Header{}
	header.Set("Origin", edgeTTSOrigin)
	header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0")

	conn, _, err := dialer.DialContext(ctx, edgeTTSWSSURL, header)
	if err != nil {
		return nil, fmt.Errorf("dial edge tts wss failed: %w", err)
	}
	defer conn.Close()

	// 1. 发送 Speech.Config
	configMsg := fmt.Sprintf("X-Timestamp:%s\r\nContent-Type:application/json; charset=utf-8\r\nPath:speech.config\r\n\r\n{\"context\":{\"synthesis\":{\"audio\":{\"metadataoptions\":{\"sentenceBoundaryEnabled\":\"false\",\"wordBoundaryEnabled\":\"false\"},\"outputFormat\":\"audio-24khz-48kbitrate-mono-mp3\"}}}}",
		time.Now().UTC().Format("Mon Jan 02 2006 15:04:05 GMT-0700 (MST)"))
	if err := conn.WriteMessage(websocket.TextMessage, []byte(configMsg)); err != nil {
		return nil, err
	}

	// 2. 发送 SSML 合成请求
	ssmlMsg := fmt.Sprintf("X-RequestId:%s\r\nContent-Type:application/ssml+xml\r\nX-Timestamp:%s\r\nPath:ssml\r\n\r\n%s",
		reqID, time.Now().UTC().Format("Mon Jan 02 2006 15:04:05 GMT-0700 (MST)"), ssml)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(ssmlMsg)); err != nil {
		return nil, err
	}

	// 3. 读取流并组装音频字节
	var audioBuf []byte
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		messageType, pData, err := conn.ReadMessage()
		if err != nil {
			break
		}

		if messageType == websocket.TextMessage {
			msgStr := string(pData)
			if strings.Contains(msgStr, "Path:turn.end") {
				break
			}
		} else if messageType == websocket.BinaryMessage {
			if len(pData) > 2 {
				headerLen := int(pData[0])<<8 | int(pData[1])
				if len(pData) > headerLen+2 {
					audioData := pData[headerLen+2:]
					audioBuf = append(audioBuf, audioData...)
				}
			}
		}
	}

	if len(audioBuf) == 0 {
		return nil, fmt.Errorf("no audio received from edge-tts")
	}

	return audioBuf, nil
}

// GetEdgeTTSVoiceDescriptors 转换为通用的 VoiceDescriptor 清单
func GetEdgeTTSVoiceDescriptors() []VoiceDescriptor {
	out := make([]VoiceDescriptor, 0, len(BuiltinEdgeVoices))
	for _, v := range BuiltinEdgeVoices {
		out = append(out, VoiceDescriptor{
			ID:         v.VoiceID,
			Name:       v.DisplayName,
			Provider:   "edge_tts",
			Category:   v.Category,
			Gender:     v.Gender,
			SampleText: v.SampleText,
			Tags:       []string{"微软官方", "免密无限制", "影视解说神级音色"},
		})
	}
	return out
}
