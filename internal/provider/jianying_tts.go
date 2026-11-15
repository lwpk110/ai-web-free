package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var (
	ErrEmptyTTSText    = errors.New("tts text cannot be empty")
	ErrTTSFailed       = errors.New("tts synthesis failed")
	DefaultMaxChunkLen = 250 // 剪映安全单段最大字符数
)

// SplitTextForTTS 智能长文本切片算法（按标点符号断句）
func SplitTextForTTS(text string, maxChunkLen int) []string {
	if maxChunkLen <= 0 {
		maxChunkLen = DefaultMaxChunkLen
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}

	// 优先以换行与句末标点符号分段
	delimiters := []rune{'\n', '。', '！', '？', '!', '?', '；', ';'}
	isDelimiter := func(r rune) bool {
		for _, d := range delimiters {
			if r == d {
				return true
			}
		}
		return false
	}

	var sentences []string
	var current strings.Builder

	for _, r := range trimmed {
		current.WriteRune(r)
		if isDelimiter(r) {
			s := strings.TrimSpace(current.String())
			if s != "" {
				sentences = append(sentences, s)
			}
			current.Reset()
		}
	}
	if current.Len() > 0 {
		s := strings.TrimSpace(current.String())
		if s != "" {
			sentences = append(sentences, s)
		}
	}

	// 二次合并：将过短的句子合并，不超过 maxChunkLen；若单句过长则按逗号切分
	var chunks []string
	var buf strings.Builder

	for _, s := range sentences {
		sRuneLen := utf8.RuneCountInString(s)
		if sRuneLen > maxChunkLen {
			// 如果当前 buffer 有内容，先刷出
			if buf.Len() > 0 {
				chunks = append(chunks, buf.String())
				buf.Reset()
			}
			// 单句超过 maxChunkLen，按逗号切分
			subClauses := splitByComma(s, maxChunkLen)
			chunks = append(chunks, subClauses...)
			continue
		}

		if utf8.RuneCountInString(buf.String())+sRuneLen <= maxChunkLen {
			if buf.Len() > 0 {
				buf.WriteString(" ")
			}
			buf.WriteString(s)
		} else {
			if buf.Len() > 0 {
				chunks = append(chunks, buf.String())
				buf.Reset()
			}
			buf.WriteString(s)
		}
	}

	if buf.Len() > 0 {
		chunks = append(chunks, buf.String())
	}

	return chunks
}

func splitByComma(s string, maxChunkLen int) []string {
	commas := []rune{'，', ',', '、'}
	isComma := func(r rune) bool {
		for _, c := range commas {
			if r == c {
				return true
			}
		}
		return false
	}

	var parts []string
	var cur strings.Builder

	for _, r := range s {
		cur.WriteRune(r)
		if isComma(r) && utf8.RuneCountInString(cur.String()) >= maxChunkLen/2 {
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, strings.TrimSpace(cur.String()))
	}

	// 若依然存在超长片断，进行字符硬截断
	var result []string
	for _, p := range parts {
		runes := []rune(p)
		for len(runes) > maxChunkLen {
			result = append(result, string(runes[:maxChunkLen]))
			runes = runes[maxChunkLen:]
		}
		if len(runes) > 0 {
			result = append(result, string(runes))
		}
	}
	return result
}

// JianyingTTSRequest 剪映语音合成上游请求体
type JianyingTTSRequest struct {
	Text    string `json:"text"`
	Speaker string `json:"speaker"`
	Stream  bool   `json:"stream"`
}

// JianyingTTSResponse 剪映语音合成上游响应体
type JianyingTTSResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"` // base64 编码的 mp3 数据
}

// SynthesizeSingleChunk 调用剪映接口合成单个文本分片
func SynthesizeSingleChunk(ctx context.Context, text, voice string, proxyStr string) ([]byte, error) {
	if strings.TrimSpace(text) == "" {
		return nil, ErrEmptyTTSText
	}

	apiURL := "https://api-normal-c-lq.zijieapi.com/media/api/text/speech/stream?app_name=jianying&device_id=718899223311&version_code=150000"

	reqBody := JianyingTTSRequest{
		Text:    text,
		Speaker: voice,
		Stream:  false,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "JianYing/15.0.0 (Windows NT 10.0; Win64; x64)")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")

	client := &http.Client{
		Timeout: 25 * time.Second,
	}

	if proxyStr != "" {
		if u, err := url.Parse(proxyStr); err == nil {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(u),
			}
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call jianying tts error: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应类型：如果是直接返回 audio/mpeg 二进制
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "audio/") {
		return io.ReadAll(resp.Body)
	}

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var apiResp JianyingTTSResponse
	if err := json.Unmarshal(respData, &apiResp); err == nil && apiResp.Data != "" {
		return base64.StdEncoding.DecodeString(apiResp.Data)
	}

	// 若上游直接返回了 MP3 原始流 (ID3 或 0xFF 帧头)
	if len(respData) >= 3 && (string(respData[:3]) == "ID3" || (respData[0] == 0xFF && (respData[1]&0xE0) == 0xE0)) {
		return respData, nil
	}

	if apiResp.Message != "" {
		return nil, fmt.Errorf("jianying tts upstream error: %s (code: %d)", apiResp.Message, apiResp.Code)
	}

	return nil, fmt.Errorf("%w: invalid upstream response", ErrTTSFailed)
}

// SynthesizeJianyingSpeech 自动切片并发合成并拼接完整 MP3
func SynthesizeJianyingSpeech(ctx context.Context, text, voice string, speed float64, proxyStr string) ([]byte, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, ErrEmptyTTSText
	}

	// 映射 OpenAI voice 到合法剪映音色
	resolvedVoice := MapOpenAIVoiceToJianying(voice)

	// 切片
	chunks := SplitTextForTTS(trimmed, DefaultMaxChunkLen)
	if len(chunks) == 0 {
		return nil, ErrEmptyTTSText
	}

	// 若只有单片，直接合成返回
	if len(chunks) == 1 {
		return SynthesizeSingleChunk(ctx, chunks[0], resolvedVoice, proxyStr)
	}

	// 多片并发合成（限制最大 3 并发避免 429）
	type chunkResult struct {
		index int
		audio []byte
		err   error
	}

	results := make([]chunkResult, len(chunks))
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup

	for i, chunk := range chunks {
		wg.Add(1)
		go func(idx int, cText string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			audio, err := SynthesizeSingleChunk(ctx, cText, resolvedVoice, proxyStr)
			results[idx] = chunkResult{
				index: idx,
				audio: audio,
				err:   err,
			}
		}(i, chunk)
	}

	wg.Wait()

	// 检查是否有失败，同时拼接全部有效音频
	var combined bytes.Buffer
	for _, res := range results {
		if res.err != nil {
			return nil, fmt.Errorf("chunk %d synthesis failed: %w", res.index, res.err)
		}
		combined.Write(res.audio)
	}

	return combined.Bytes(), nil
}
