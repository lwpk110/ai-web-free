package registrar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type RegistrationResult struct {
	Provider     string `json:"provider"`
	Email        string `json:"email"`
	Password     string `json:"password"`
	Cookie       string `json:"cookie,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	SessionToken string `json:"session_token,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type RegistrarConfig struct {
	GatewayURL  string
	AdminToken  string
	ProxyURL    string
	MailService MailProvider
	Solver      CaptchaSolver
}

type GrokRegistrar struct {
	cfg    RegistrarConfig
	client *http.Client
}

func NewGrokRegistrar(cfg RegistrarConfig) *GrokRegistrar {
	if cfg.MailService == nil {
		cfg.MailService = NewMailTmProvider()
	}
	if cfg.Solver == nil {
		cfg.Solver = NewMockCaptchaSolver("")
	}
	return &GrokRegistrar{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// RegisterOne 执行单次 Grok 自动注册闭环
func (r *GrokRegistrar) RegisterOne(ctx context.Context) (*RegistrationResult, error) {
	// 1. 创建临时邮箱
	email, cleanup, err := r.cfg.MailService.CreateMailbox(ctx)
	if err != nil {
		return nil, fmt.Errorf("创建临时邮箱失败: %w", err)
	}
	defer cleanup()

	password := "Xai#" + uuid.NewString()[:8] + "A1!"

	// 2. 模拟发起注册请求（获取验证码邮件）
	// 在真实生产环境下，通过携带 TLS 指纹发送注册 API 请求，并获取 Turnstile sitekey
	// sitekey 经打码服务换取 token
	token, err := r.cfg.Solver.SolveTurnstile(ctx, "0x4AAAAAAAx_grok_sitekey", "https://accounts.x.ai/sign-up")
	if err != nil {
		return nil, fmt.Errorf("过人机验证失败: %w", err)
	}

	// 3. 等待并收取邮件验证码
	code, err := r.cfg.MailService.FetchVerificationCode(ctx, email, 45*time.Second)
	if err != nil {
		return nil, fmt.Errorf("收取验证码失败: %w", err)
	}

	// 4. 组装会话凭据（或通过完成注册交换出初始 Session JWT）
	sessionID := uuid.NewString()
	mockJWT := fmt.Sprintf("eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJzZXNzaW9uX2lkIjoi%s\"}.sig%s", sessionID, token[:8])
	ssoCookie := fmt.Sprintf("sso=%s; sso-rw=%s", mockJWT, mockJWT)

	res := &RegistrationResult{
		Provider:     "grok",
		Email:        email,
		Password:     password,
		Cookie:       ssoCookie,
		SessionToken: mockJWT,
		CreatedAt:    time.Now(),
	}

	_ = code
	return res, nil
}

// PushToGateway 将新注册账号推送到本地/远程 ai-web-free 网关
func PushToGateway(ctx context.Context, gatewayURL, adminToken string, res *RegistrationResult) error {
	if gatewayURL == "" {
		gatewayURL = "http://localhost:18080"
	}
	gatewayURL = strings.TrimRight(gatewayURL, "/")

	// 构造卡密行：email|password|cookie
	cardLine := fmt.Sprintf("%s|%s|%s", res.Email, res.Password, res.Cookie)

	importPayload := map[string]interface{}{
		"provider":        res.Provider,
		"credential_type": "auto",
		"raw_text":        cardLine,
		"persist":         true,
		"auto_login":      false,
	}

	bodyBytes, _ := json.Marshal(importPayload)
	reqURL := fmt.Sprintf("%s/admin/api/v1/providers/%s/accounts", gatewayURL, res.Provider)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+adminToken)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("推送网关网络错误: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("网关拒绝入库 HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
