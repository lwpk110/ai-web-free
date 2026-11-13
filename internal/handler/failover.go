package handler

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"ai-web-free/httpclient/bogdanfinn"
	"ai-web-free/internal/accounts"
	"ai-web-free/internal/audit"
	"ai-web-free/internal/chatgpt"
	"ai-web-free/internal/config"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/proxy"

	"github.com/bogdanfinn/websocket"
	"github.com/gin-gonic/gin"
)

// GatewayRuntime 聚合网关路由共享的高可用与可观测组件，nil-safe。
type GatewayRuntime struct {
	AccountPool *accounts.Pool
	ProxyPool   *proxy.Pool
	KamiWorker  *provider.KamiRenewWorker
	AuditLog    *audit.RingBuffer
	Config      *config.Config
}

// FailoverOptions 控制单次请求账号容灾尝试策略与能力过滤。
type FailoverOptions struct {
	NeedsPaid   bool
	Capability  string
	Model       string
	IsImage     bool
	MaxAttempts int
}

// AttemptOutcome 封装单次上游尝试返回的原始 HTTP/WS 响应或内部结果负载。
type AttemptOutcome struct {
	Response   *http.Response
	WSConn     *websocket.Conn
	TurnStile  *chatgpt.TurnStile
	Payload    any
	StatusCode int
	Err        error
}

// FailoverResult 记录容灾调度最终命中的账号、响应结果、尝试次数及轨迹。
type FailoverResult struct {
	Account    *accounts.Account
	Outcome    AttemptOutcome
	Attempts   int
	Trace      string
	DurationMs int64
}

type failureKind int

const (
	failFatal failureKind = iota
	failUnauthorized
	failRateLimit
	failProxyTransport
	failRetryable
)

// EnsureAccountClient 复用账号粘性指纹 TLS 客户端，缺失时按账号绑定代理创建。
func EnsureAccountClient(acct *accounts.Account) *bogdanfinn.TlsClient {
	if acct != nil {
		if c, ok := acct.Client.(*bogdanfinn.TlsClient); ok && c != nil {
			return c
		}
		return setupClientWithProxy(acct.Proxy)
	}
	return setupClientWithProxy("")
}

func isPoolAuthorization(c *gin.Context, cfg *config.Config) bool {
	if c == nil {
		return true
	}
	payload := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(payload) >= 7 && strings.EqualFold(payload[:7], "Bearer ") {
		payload = strings.TrimSpace(payload[7:])
	}
	parts := strings.SplitN(payload, ",", 2)
	token := strings.TrimSpace(parts[0])
	if token == "" {
		return true
	}
	return cfg != nil && cfg.Authorization != "" && token == cfg.Authorization
}

func isProxyTransportError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	for _, kw := range []string{
		"proxyconnect",
		"connection refused",
		"connection reset",
		"broken pipe",
		"unexpected eof",
		"eof",
		"tls:",
		"handshake",
		"i/o timeout",
		"context deadline exceeded",
		"no such host",
		"dial tcp",
		"socks",
	} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func classifyFailure(status int, err error, body string, isImageHint bool) (failureKind, bool) {
	uStatus, uImgLimit, ok := chatgpt.ExtractUpstreamStatus(err)
	if status == 0 && ok {
		status = uStatus
	}
	msg := body
	if msg == "" && err != nil {
		msg = err.Error()
	}
	isImgLimit := uImgLimit || chatgpt.LooksLikeImageLimit(status, msg)

	switch {
	case status == http.StatusUnauthorized:
		return failUnauthorized, false
	case status == http.StatusTooManyRequests || (isImgLimit && status == 0):
		return failRateLimit, isImgLimit || isImageHint
	case status == 0 && isProxyTransportError(err):
		return failProxyTransport, false
	case status == http.StatusForbidden || status == http.StatusRequestTimeout || status >= http.StatusInternalServerError:
		return failRetryable, false
	default:
		return failFatal, false
	}
}

func isRetryableHTTPStatus(status int) bool {
	return status == http.StatusUnauthorized ||
		status == http.StatusForbidden ||
		status == http.StatusRequestTimeout ||
		status == http.StatusTooManyRequests ||
		status >= http.StatusInternalServerError
}

func formatAttemptStep(acct *accounts.Account, status int, note string) string {
	label := "anon"
	if acct != nil {
		if acct.Email != "" {
			label = acct.Email
		} else if len(acct.ID) > 8 {
			label = acct.ID[:8]
		} else if acct.ID != "" {
			label = acct.ID
		}
	}
	if note != "" {
		return fmt.Sprintf("%s(%d:%s)", label, status, note)
	}
	return fmt.Sprintf("%s(%d)", label, status)
}

// RunWithFailover 在响应首包写出前对池内账号执行最多 MaxAttempts 次能力感知容灾切换。
func (rt *GatewayRuntime) RunWithFailover(
	c *gin.Context,
	pool *accounts.Pool,
	cfg *config.Config,
	opts FailoverOptions,
	fn func(acct *accounts.Account, client *bogdanfinn.TlsClient) AttemptOutcome,
) (FailoverResult, error) {
	start := time.Now()
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	canRetryPool := isPoolAuthorization(c, cfg)
	if !canRetryPool {
		maxAttempts = 1
	}

	excluded := make(map[string]bool, maxAttempts)
	var steps []string
	var lastAcct *accounts.Account
	var lastOut AttemptOutcome
	var lastErr error
	attempts := 0

	for i := 0; i < maxAttempts; i++ {
		acct, acqStatus, acqErr := resolveAccountForCapability(c, pool, cfg, opts.NeedsPaid, opts.Capability, excluded)
		if acqErr != nil || acct == nil {
			if i == 0 {
				return FailoverResult{
					Outcome:    AttemptOutcome{StatusCode: acqStatus, Err: acqErr},
					Attempts:   0,
					DurationMs: time.Since(start).Milliseconds(),
				}, acqErr
			}
			break
		}
		excluded[acct.ID] = true
		attempts++
		client := EnsureAccountClient(acct)

		attemptStart := time.Now()
		out := fn(acct, client)

		httpOK := out.Response == nil || out.Response.StatusCode == http.StatusOK
		if out.Err == nil && httpOK {
			if acct.Proxy != "" && rt != nil && rt.ProxyPool != nil {
				rt.ProxyPool.ReportSuccess(acct.Proxy, time.Since(attemptStart).Milliseconds())
			}
			traceStr := ""
			if len(steps) > 0 {
				steps = append(steps, formatAttemptStep(acct, http.StatusOK, "ok"))
				traceStr = strings.Join(steps, " -> ")
			}
			return FailoverResult{
				Account:    acct,
				Outcome:    out,
				Attempts:   attempts,
				Trace:      traceStr,
				DurationMs: time.Since(start).Milliseconds(),
			}, nil
		}

		// 检查 HTTP 非 200 响应体（同时保留可读副本，避免最后一次失败无法交给 Handle_request_error）
		status := out.StatusCode
		var bodySnippet string
		if out.Response != nil && out.Response.StatusCode != http.StatusOK {
			status = out.Response.StatusCode
			if !isRetryableHTTPStatus(status) {
				steps = append(steps, formatAttemptStep(acct, status, "client_err"))
				return FailoverResult{
					Account:    acct,
					Outcome:    out,
					Attempts:   attempts,
					Trace:      joinFailoverSteps(steps, attempts),
					DurationMs: time.Since(start).Milliseconds(),
				}, nil
			}
			if out.Response.Body != nil {
				raw, _ := io.ReadAll(io.LimitReader(out.Response.Body, 64*1024))
				out.Response.Body.Close()
				out.Response.Body = io.NopCloser(bytes.NewReader(raw))
				bodySnippet = strings.TrimSpace(string(raw))
			}
		}

		if status == 0 {
			if uStatus, _, ok := chatgpt.ExtractUpstreamStatus(out.Err); ok {
				status = uStatus
			}
		}
		errMsg := bodySnippet
		if errMsg == "" && out.Err != nil {
			errMsg = out.Err.Error()
		}
		if errMsg == "" && status > 0 {
			errMsg = http.StatusText(status)
		}

		kind, imgLimit := classifyFailure(status, out.Err, bodySnippet, opts.IsImage)
		steps = append(steps, formatAttemptStep(acct, status, failureTag(kind, imgLimit)))
		if rt != nil {
			rt.applyFailureSideEffects(pool, acct, kind, imgLimit, errMsg)
		}

		lastAcct = acct
		lastOut = out
		if out.Err != nil {
			lastErr = out.Err
		} else {
			lastErr = errors.New(errMsg)
		}

		if kind == failFatal || !canRetryPool || i+1 >= maxAttempts {
			break
		}
		if out.WSConn != nil {
			_ = out.WSConn.Close()
			out.WSConn = nil
		}
	}

	return FailoverResult{
		Account:    lastAcct,
		Outcome:    lastOut,
		Attempts:   attempts,
		Trace:      joinFailoverSteps(steps, attempts),
		DurationMs: time.Since(start).Milliseconds(),
	}, lastErr
}

func joinFailoverSteps(steps []string, attempts int) string {
	if attempts <= 1 && len(steps) <= 1 {
		return ""
	}
	return strings.Join(steps, " -> ")
}

func failureTag(kind failureKind, imgLimit bool) string {
	switch kind {
	case failUnauthorized:
		return "401"
	case failRateLimit:
		if imgLimit {
			return "img_429"
		}
		return "429"
	case failProxyTransport:
		return "proxy_err"
	case failRetryable:
		return "retryable"
	default:
		return "err"
	}
}

func (rt *GatewayRuntime) applyFailureSideEffects(pool *accounts.Pool, acct *accounts.Account, kind failureKind, imgLimit bool, errMsg string) {
	if acct == nil || pool == nil {
		return
	}
	switch kind {
	case failUnauthorized:
		pool.MarkExpired(acct, errMsg)
		if rt.KamiWorker != nil {
			rt.KamiWorker.Enqueue(acct, "gateway_401")
		}
	case failRateLimit:
		cd := 5 * time.Minute
		if imgLimit {
			cd = 15 * time.Minute
		}
		pool.ReportRateLimit(acct, imgLimit, cd, errMsg)
	case failProxyTransport:
		if rt.ProxyPool != nil && acct.Proxy != "" {
			failedProxy := acct.Proxy
			if tripped := rt.ProxyPool.ReportFailure(failedProxy, errMsg); tripped {
				migrated := pool.ReconcileProxies(
					func(u string) bool { return !rt.ProxyPool.IsHealthy(u) },
					func(excl map[string]bool) string { return rt.ProxyPool.AllocateHealthy(excl) },
				)
				log.Printf("[proxy-breaker] node %s tripped after consecutive errors; migrated %d account(s)", failedProxy, migrated)
				if rt.AuditLog != nil {
					rt.AuditLog.RecordEvent("/internal/proxy-breaker", acct.Email, failedProxy, fmt.Sprintf("tripped_migrated_%d", migrated), errMsg)
				}
			}
		}
	}
}

// RecordAudit 将网关请求结果与容灾切换路径写入环形审计缓冲。
func (rt *GatewayRuntime) RecordAudit(c *gin.Context, model string, res FailoverResult, status int, err error) {
	if rt == nil || rt.AuditLog == nil || c == nil {
		return
	}
	if status == 0 {
		status = c.Writer.Status()
	}
	if status == 0 {
		status = http.StatusOK
	}
	var acctID, email, proxyURL string
	if res.Account != nil {
		acctID = res.Account.ID
		email = res.Account.Email
		proxyURL = res.Account.Proxy
	}
	errText := ""
	if err != nil {
		errText = err.Error()
		if len(errText) > 240 {
			errText = errText[:240]
		}
	}
	rt.AuditLog.Record(audit.Entry{
		Kind:          "request",
		Method:        c.Request.Method,
		Path:          c.Request.URL.Path,
		Model:         model,
		AccountID:     acctID,
		AccountEmail:  email,
		Proxy:         proxyURL,
		Status:        status,
		DurationMs:    res.DurationMs,
		FailoverTrace: res.Trace,
		AttemptCount:  res.Attempts,
		Error:         errText,
	})
}

// FailoverStatusError 从 FailoverResult 构造可读错误用于审计日志与错误返回。
func FailoverStatusError(res FailoverResult, status int) error {
	if res.Outcome.Err != nil {
		return res.Outcome.Err
	}
	return fmt.Errorf("upstream HTTP %d", status)
}

var (
	errMissingAccessToken = errors.New("requires a logged-in ChatGPT access token")
	errAccountCapDenied   = errors.New("account does not satisfy required capability")

	capabilityRegistry = map[string]accounts.Capability{
		accounts.CapChat.Name:           accounts.CapChat,
		accounts.CapResponses.Name:      accounts.CapResponses,
		accounts.CapToolCalling.Name:    accounts.CapToolCalling,
		accounts.CapImageGenerate.Name:  accounts.CapImageGenerate,
		accounts.CapImageEdit.Name:      accounts.CapImageEdit,
		accounts.CapImageVariation.Name: accounts.CapImageVariation,
		accounts.CapTTS.Name:            accounts.CapTTS,
		accounts.CapTranscribe.Name:     accounts.CapTranscribe,
		accounts.CapFileUpload.Name:     accounts.CapFileUpload,
		accounts.CapWebSocket.Name:      accounts.CapWebSocket,
	}
)

// AccountSatisfies 按能力名称校验账号是否具备该项能力，未识别的能力名称默认放行。
func AccountSatisfies(acct *accounts.Account, capName string) bool {
	if acct == nil {
		return false
	}
	if capName == "" {
		return true
	}
	capDef, ok := capabilityRegistry[capName]
	if !ok {
		return true
	}
	return acct.Type.Satisfies(capDef)
}
