package registrar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// CaptchaSolver 人机验证打码服务抽象接口
type CaptchaSolver interface {
	Name() string
	SolveTurnstile(ctx context.Context, sitekey, pageURL string) (token string, err error)
}

// CapSolverClient CapSolver API 实现
type CapSolverClient struct {
	clientKey string
	baseURL   string
	client    *http.Client
}

func NewCapSolverClient(clientKey string) *CapSolverClient {
	return &CapSolverClient{
		clientKey: clientKey,
		baseURL:   "https://api.capsolver.com",
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *CapSolverClient) Name() string {
	return "capsolver"
}

func (c *CapSolverClient) SolveTurnstile(ctx context.Context, sitekey, pageURL string) (string, error) {
	if c.clientKey == "" {
		return "", errors.New("CapSolver API clientKey 为空")
	}

	// 1. 创建打码任务
	createPayload := map[string]interface{}{
		"clientKey": c.clientKey,
		"task": map[string]string{
			"type":       "AntiTurnstileTaskProxyLess",
			"websiteURL": pageURL,
			"websiteKey": sitekey,
		},
	}
	body, _ := json.Marshal(createPayload)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/createTask", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("createTask failed: %w", err)
	}
	defer resp.Body.Close()

	var createResp struct {
		ErrorID          int    `json:"errorId"`
		ErrorDescription string `json:"errorDescription"`
		TaskID           string `json:"taskId"`
	}
	respBytes, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(respBytes, &createResp); err != nil || createResp.ErrorID != 0 || createResp.TaskID == "" {
		return "", fmt.Errorf("createTask error: %s (ID %d)", createResp.ErrorDescription, createResp.ErrorID)
	}

	taskID := createResp.TaskID

	// 2. 轮询结果 (最多等 60 秒)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}

		resultPayload := map[string]string{
			"clientKey": c.clientKey,
			"taskId":    taskID,
		}
		rBody, _ := json.Marshal(resultPayload)
		req, _ = http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/getTaskResult", bytes.NewReader(rBody))
		req.Header.Set("Content-Type", "application/json")
		res, err := c.client.Do(req)
		if err != nil {
			continue
		}

		var pollResp struct {
			Status   string `json:"status"` // processing, ready, failed
			Solution struct {
				Token string `json:"token"`
			} `json:"solution"`
			ErrorDescription string `json:"errorDescription"`
		}
		pBytes, _ := io.ReadAll(res.Body)
		res.Body.Close()

		if json.Unmarshal(pBytes, &pollResp) == nil {
			if pollResp.Status == "ready" && pollResp.Solution.Token != "" {
				return pollResp.Solution.Token, nil
			}
			if pollResp.Status == "failed" {
				return "", fmt.Errorf("打码任务失败: %s", pollResp.ErrorDescription)
			}
		}
	}
	return "", errors.New("等待打码 Token 超时")
}

// MockCaptchaSolver 模拟打码器（用于测试）
type MockCaptchaSolver struct {
	FixedToken string
}

func NewMockCaptchaSolver(fixedToken string) *MockCaptchaSolver {
	if fixedToken == "" {
		fixedToken = "mock_cf_turnstile_response_token_123456"
	}
	return &MockCaptchaSolver{FixedToken: fixedToken}
}

func (m *MockCaptchaSolver) Name() string {
	return "mock_solver"
}

func (m *MockCaptchaSolver) SolveTurnstile(ctx context.Context, sitekey, pageURL string) (string, error) {
	return m.FixedToken, nil
}
