package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	grokCredentialLLMBatchSize = 20
	grokCredentialLLMTimeout   = 45 * time.Second
)

type grokCredentialLLMResult struct {
	Index int    `json:"index"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type grokCredentialLLMFn func(context.Context, []string) ([]grokCredentialLLMResult, error)

var (
	grokCookieValuePattern  = regexp.MustCompile(`(?i)(?:^|;\s*)(sso|sso-rw)\s*=\s*([^;]+)`)
	grokLLMReasoningPattern = regexp.MustCompile(`(?is)<think>.*?</think>`)
)

// SetGrokCredentialLLM installs an optional LLM fallback for unrecognized auto-import lines.
// The callback receives credential data; callers must never log its input, output, or errors.
func (p *GrokProvider) SetGrokCredentialLLM(fn grokCredentialLLMFn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.grokCredentialLLMFn = fn
}

func grokCredentialLLMModel(defaultModel string) string {
	model := strings.TrimSpace(defaultModel)
	if model == "" || strings.EqualFold(model, "auto") {
		return "deepseek-ai/DeepSeek-R1-Distill-Qwen-7B"
	}
	return model
}

func normalizeGrokCredentialsWithLLM(ctx context.Context, sf *SiliconFlowProvider, model string, lines []string) ([]grokCredentialLLMResult, error) {
	if sf == nil {
		return nil, errors.New("siliconflow provider is not configured")
	}
	if len(lines) == 0 {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, grokCredentialLLMTimeout)
	defer cancel()

	systemPrompt := strings.Join([]string{
		"You normalize Grok credential lines. Return only strict JSON.",
		"Never invent, complete, transform, or omit any secret value.",
		"For a card, kind is \"kami\" and value is exactly email|password|totp_secret; append |old_sso_cookie or |proxy_url only when present in the original line.",
		"For an existing SSO cookie, kind is \"cookie\" and value is either email----sso=...; sso-rw=... or sso=...; sso-rw=...",
		"If a line is not a complete Grok credential, use kind \"unknown\" and omit value.",
		"Keep the input index for every item and include one result for every input line.",
	}, "\n")
	model = grokCredentialLLMModel(model)
	results := make([]grokCredentialLLMResult, 0, len(lines))

	for start := 0; start < len(lines); start += grokCredentialLLMBatchSize {
		end := start + grokCredentialLLMBatchSize
		if end > len(lines) {
			end = len(lines)
		}
		batch := lines[start:end]
		input, err := json.Marshal(map[string][]string{"lines": batch})
		if err != nil {
			return nil, err
		}
		output, err := sf.ChatCompletionText(ctx, systemPrompt, string(input), model)
		if err != nil {
			return nil, fmt.Errorf("batch %d: %w", start/grokCredentialLLMBatchSize+1, err)
		}
		var parsed struct {
			Results []grokCredentialLLMResult `json:"results"`
		}
		if err := decodeGrokCredentialLLMJSON(output, &parsed); err != nil {
			return nil, fmt.Errorf("batch %d: %w", start/grokCredentialLLMBatchSize+1, err)
		}
		seen := make(map[int]bool, len(parsed.Results))
		for _, item := range parsed.Results {
			if item.Index < 0 || item.Index >= len(batch) || seen[item.Index] {
				return nil, fmt.Errorf("batch %d: invalid result index", start/grokCredentialLLMBatchSize+1)
			}
			seen[item.Index] = true
			item.Index += start
			results = append(results, item)
		}
		if len(seen) != len(batch) {
			return nil, fmt.Errorf("batch %d: missing result index", start/grokCredentialLLMBatchSize+1)
		}
	}
	return results, nil
}

func decodeGrokCredentialLLMJSON(output string, target interface{}) error {
	text := strings.TrimSpace(grokLLMReasoningPattern.ReplaceAllString(output, ""))
	if text == "" {
		return errors.New("empty LLM response")
	}
	if start := strings.Index(text, "{"); start >= 0 {
		if end := strings.LastIndex(text, "}"); end > start {
			text = text[start : end+1]
		}
	}
	if err := json.Unmarshal([]byte(text), target); err != nil {
		return errors.New("invalid LLM JSON")
	}
	return nil
}

func validateGrokCredentialLLMResult(original string, item grokCredentialLLMResult) (string, error) {
	kind := strings.ToLower(strings.TrimSpace(item.Kind))
	value := strings.TrimSpace(item.Value)
	if kind == "unknown" {
		return "unknown", nil
	}
	if kind != "kami" && kind != "cookie" || value == "" {
		return "", errors.New("invalid result kind")
	}
	detected, card, err := DetectGrokCredentialLine(value)
	if err != nil || detected != kind {
		return "", errors.New("normalized value failed Grok validation")
	}
	if kind == "kami" {
		values := []string{card.Email, card.Password, card.Proxy}
		for _, secret := range values {
			if secret != "" && !strings.Contains(original, secret) {
				return "", errors.New("normalized value contains data not present in the original line")
			}
		}
		if card.LegacyToken != "" && !grokSecretOccurs(original, card.LegacyToken) {
			return "", errors.New("normalized legacy token is not present in the original line")
		}
		if card.TOTPSecret != "" && !grokSecretOccurs(original, card.TOTPSecret) {
			return "", errors.New("normalized TOTP secret is not present in the original line")
		}
		if card.OldCookie != "" && !grokCookieValuesOccur(original, card.OldCookie) {
			return "", errors.New("normalized cookie contains data not present in the original line")
		}
		return detected, nil
	}
	if strings.Contains(value, "----") {
		parts := strings.SplitN(value, "----", 2)
		if email := strings.TrimSpace(parts[0]); email != "" && !strings.Contains(original, email) {
			return "", errors.New("normalized email is not present in the original line")
		}
	}
	if !grokCookieValuesOccur(original, value) {
		return "", errors.New("normalized cookie contains data not present in the original line")
	}
	return detected, nil
}

func grokSecretOccurs(original, secret string) bool {
	if strings.Contains(original, secret) {
		return true
	}
	normalize := func(value string) string {
		return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(value))
	}
	return strings.Contains(normalize(original), normalize(secret))
}

func grokCookieValuesOccur(original, cookie string) bool {
	matches := grokCookieValuePattern.FindAllStringSubmatch(cookie, -1)
	if len(matches) == 0 {
		return strings.Contains(original, cookie)
	}
	for _, match := range matches {
		value := strings.Trim(strings.TrimSpace(match[2]), "\"'")
		if value == "" || !strings.Contains(original, value) {
			return false
		}
	}
	return true
}

func grokCredentialLLMError(err error) string {
	if errors.Is(err, ErrNoSiliconFlowKeyAvailable) {
		return "LLM 兜底未执行：未配置可用的 SILICONFLOW_API_KEY"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "LLM 兜底超时，未识别行保持失败"
	}
	return "LLM 兜底未完成，未识别行保持失败"
}
