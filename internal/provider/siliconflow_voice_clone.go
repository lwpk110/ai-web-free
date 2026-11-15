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
	"strings"
)

// SanitizeSiliconFlowCustomName 将任意名称规整为硅基流动 `/uploads/audio/voice` 接受的 ASCII customName（字母/数字/下划线/短横线，最长 32 字符）。
func SanitizeSiliconFlowCustomName(raw string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(raw) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteByte('_')
		}
		if b.Len() >= 32 {
			break
		}
	}
	cleaned := strings.Trim(b.String(), "_-")
	if cleaned == "" {
		return "crawled_voice"
	}
	return cleaned
}

// UploadReferenceVoice 上传本地音频样音至硅基流动 CosyVoice2-0.5B 云端音色接口，返回可直接用于 `/v1/audio/speech` 的 `speech:...` URI。
func (p *SiliconFlowProvider) UploadReferenceVoice(ctx context.Context, customName, transcript string, audioBytes []byte, mimeType string) (string, error) {
	if len(audioBytes) == 0 {
		return "", errors.New("audio reference bytes are empty")
	}

	key, keyIdx, err := p.nextKey()
	if err != nil {
		return "", err
	}

	mimeClean := strings.TrimSpace(mimeType)
	if mimeClean == "" {
		mimeClean = http.DetectContentType(audioBytes)
	}
	if idx := strings.Index(mimeClean, ";"); idx > 0 {
		mimeClean = strings.TrimSpace(mimeClean[:idx])
	}
	if !strings.HasPrefix(strings.ToLower(mimeClean), "audio/") {
		mimeClean = "audio/mpeg"
	}

	safeName := SanitizeSiliconFlowCustomName(customName)
	refText := strings.TrimSpace(transcript)
	if refText == "" {
		refText = "你好，这是一段用于云端音色复刻与短剧角色配音的参考样音。"
	}

	dataURI := "data:" + mimeClean + ";base64," + base64.StdEncoding.EncodeToString(audioBytes)
	reqBody := map[string]interface{}{
		"model":      "FunAudioLLM/CosyVoice2-0.5B",
		"customName": safeName,
		"text":       refText,
		"audio":      dataURI,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	reqURL := p.baseURL + "/uploads/audio/voice"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+key)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		p.reportStatus(keyIdx, false, false)
		return "", err
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		p.reportStatus(keyIdx, false, false)
		return "", err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		p.reportStatus(keyIdx, false, true)
		return "", errors.New("siliconflow upload voice rate limit reached (429)")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		p.reportStatus(keyIdx, false, false)
		return "", fmt.Errorf("siliconflow upload voice failed (status %d): %s", resp.StatusCode, strings.TrimSpace(string(respData)))
	}

	var parsed struct {
		URI     string `json:"uri"`
		VoiceID string `json:"voice_id"`
		Data    struct {
			URI string `json:"uri"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respData, &parsed); err != nil {
		p.reportStatus(keyIdx, false, false)
		return "", err
	}
	uri := strings.TrimSpace(parsed.URI)
	if uri == "" {
		uri = strings.TrimSpace(parsed.VoiceID)
	}
	if uri == "" {
		uri = strings.TrimSpace(parsed.Data.URI)
	}
	if uri == "" {
		p.reportStatus(keyIdx, false, false)
		return "", fmt.Errorf("siliconflow upload voice succeeded without uri: %s", strings.TrimSpace(string(respData)))
	}

	p.reportStatus(keyIdx, true, false)
	return uri, nil
}
