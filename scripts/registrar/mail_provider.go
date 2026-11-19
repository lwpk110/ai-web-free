package registrar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	reOTPCode = regexp.MustCompile(`\b([0-9]{6})\b`)
)

// MailProvider 临时邮箱抽象接口
type MailProvider interface {
	Name() string
	CreateMailbox(ctx context.Context) (email string, cleanup func(), err error)
	FetchVerificationCode(ctx context.Context, email string, timeout time.Duration) (code string, err error)
}

// MailTmProvider 基于 Mail.tm 公共免配临时邮箱 API 实现
type MailTmProvider struct {
	client   *http.Client
	baseURL  string
	password string
	tokenMap map[string]string // email -> auth token
}

func NewMailTmProvider() *MailTmProvider {
	return &MailTmProvider{
		client:   &http.Client{Timeout: 15 * time.Second},
		baseURL:  "https://api.mail.tm",
		password: "P@" + uuid.NewString()[:12] + "!",
		tokenMap: make(map[string]string),
	}
}

func (p *MailTmProvider) Name() string {
	return "mail.tm"
}

func (p *MailTmProvider) CreateMailbox(ctx context.Context) (string, func(), error) {
	// 1. 获取可用域名
	domain, err := p.getAvailableDomain(ctx)
	if err != nil {
		return "", nil, err
	}

	username := "u_" + uuid.NewString()[:8]
	email := fmt.Sprintf("%s@%s", username, domain)

	// 2. 注册账号
	createPayload := map[string]string{
		"address":  email,
		"password": p.password,
	}
	bodyBytes, _ := json.Marshal(createPayload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/accounts", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("create mailbox http error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", nil, fmt.Errorf("create mailbox failed HTTP %d", resp.StatusCode)
	}

	// 3. 换取 Token
	token, err := p.acquireToken(ctx, email)
	if err != nil {
		return "", nil, err
	}
	p.tokenMap[email] = token

	cleanup := func() {
		delete(p.tokenMap, email)
	}
	return email, cleanup, nil
}

func (p *MailTmProvider) FetchVerificationCode(ctx context.Context, email string, timeout time.Duration) (string, error) {
	token := p.tokenMap[email]
	if token == "" {
		var err error
		token, err = p.acquireToken(ctx, email)
		if err != nil {
			return "", err
		}
		p.tokenMap[email] = token
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(3 * time.Second):
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/messages", nil)
		if err != nil {
			continue
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := p.client.Do(req)
		if err != nil {
			continue
		}

		var payload struct {
			HydraMember []struct {
				ID    string `json:"id"`
				Intro string `json:"intro"`
			} `json:"hydra:member"`
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if json.Unmarshal(body, &payload) == nil && len(payload.HydraMember) > 0 {
			// 读取最新邮件正文并提取 6 位验证码
			msgID := payload.HydraMember[0].ID
			code, err := p.readMessageCode(ctx, token, msgID)
			if err == nil && code != "" {
				return code, nil
			}
			// 备选尝试从 intro 提取
			if match := reOTPCode.FindStringSubmatch(payload.HydraMember[0].Intro); len(match) > 1 {
				return match[1], nil
			}
		}
	}
	return "", errors.New("等待临时邮箱验证码超时")
}

func (p *MailTmProvider) getAvailableDomain(ctx context.Context) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/domains", nil)
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var payload struct {
		HydraMember []struct {
			Domain string `json:"domain"`
		} `json:"hydra:member"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &payload); err != nil || len(payload.HydraMember) == 0 {
		return "", errors.New("无法获取 mail.tm 可用域名列表")
	}
	return payload.HydraMember[0].Domain, nil
}

func (p *MailTmProvider) acquireToken(ctx context.Context, email string) (string, error) {
	loginPayload := map[string]string{
		"address":  email,
		"password": p.password,
	}
	bodyBytes, _ := json.Marshal(loginPayload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/token", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var payload struct {
		Token string `json:"token"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &payload); err != nil || payload.Token == "" {
		return "", errors.New("获取 mail.tm token 失败")
	}
	return payload.Token, nil
}

func (p *MailTmProvider) readMessageCode(ctx context.Context, token, messageID string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/messages/"+messageID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var payload struct {
		Text string `json:"text"`
		HTML []string `json:"html"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}

	content := payload.Text + " " + strings.Join(payload.HTML, " ")
	if match := reOTPCode.FindStringSubmatch(content); len(match) > 1 {
		return match[1], nil
	}
	return "", errors.New("邮件中未检测到 6 位验证码")
}

// MockMailProvider 单元测试与离线演示用 Mock 邮箱服务
type MockMailProvider struct {
	FixedCode string
}

func NewMockMailProvider(fixedCode string) *MockMailProvider {
	if fixedCode == "" {
		fixedCode = "889922"
	}
	return &MockMailProvider{FixedCode: fixedCode}
}

func (m *MockMailProvider) Name() string {
	return "mock_mail"
}

func (m *MockMailProvider) CreateMailbox(ctx context.Context) (string, func(), error) {
	email := fmt.Sprintf("auto_%s@mocktest.local", uuid.NewString()[:8])
	return email, func() {}, nil
}

func (m *MockMailProvider) FetchVerificationCode(ctx context.Context, email string, timeout time.Duration) (string, error) {
	return m.FixedCode, nil
}
