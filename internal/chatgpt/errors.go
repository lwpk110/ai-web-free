package chatgpt

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// UpstreamError 携带上游 HTTP 状态码与限流作用域信息，供网关 Failover 精准归类。
type UpstreamError struct {
	Operation    string
	StatusCode   int
	Body         string
	IsImageLimit bool
}

func (e *UpstreamError) Error() string {
	if e == nil {
		return ""
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("%s (HTTP %d): %s", e.Operation, e.StatusCode, e.Body)
	}
	return fmt.Sprintf("%s: %s", e.Operation, e.Body)
}

// NewUpstreamError 构造包含 HTTP 状态码的上游错误，并自动识别是否属于生图配额/频次受限。
func NewUpstreamError(operation string, statusCode int, body string) error {
	trimmed := strings.TrimSpace(body)
	if len(trimmed) > 512 {
		trimmed = trimmed[:512]
	}
	return &UpstreamError{
		Operation:    operation,
		StatusCode:   statusCode,
		Body:         trimmed,
		IsImageLimit: LooksLikeImageLimit(statusCode, body),
	}
}

// LooksLikeImageLimit 判断错误响应/文本是否属于生图专属限额或频次受限。
func LooksLikeImageLimit(statusCode int, message string) bool {
	lower := strings.ToLower(message)
	if (strings.Contains(lower, "image") || strings.Contains(lower, "picture") || strings.Contains(lower, "dall")) &&
		(strings.Contains(lower, "limit") || strings.Contains(lower, "quota") || strings.Contains(lower, "cap") || strings.Contains(lower, "rate") || strings.Contains(lower, "try again")) {
		return true
	}
	if statusCode == http.StatusTooManyRequests && (strings.Contains(lower, "image") || strings.Contains(lower, "generation")) {
		return true
	}
	return false
}

// ExtractUpstreamStatus 从错误链提取 UpstreamError 中的状态码与生图限流标记。
func ExtractUpstreamStatus(err error) (status int, isImageLimit bool, ok bool) {
	if err == nil {
		return 0, false, false
	}
	var ue *UpstreamError
	if errors.As(err, &ue) && ue != nil {
		return ue.StatusCode, ue.IsImageLimit, true
	}
	return 0, false, false
}

// ErrorCategory 常量定义诊断与状态机共用的根因分类标识。
const (
	ErrCategoryBanned         = "banned"
	ErrCategoryWAFProxy       = "waf_proxy_block"
	ErrCategoryTokenExpired   = "token_expired"
	ErrCategoryImageLimit     = "image_rate_limit"
	ErrCategoryRateLimit      = "rate_limit"
	ErrCategoryProxyTransport = "proxy_transport"
	ErrCategoryOther          = "other"
)

func containsAny(lower string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

var accountBannedKeywords = []string{
	"account_deactivated",
	"account deactivated",
	"account has been deactivated",
	"account is deactivated",
	"user is deactivated",
	"user has been deactivated",
	"account_suspended",
	"account suspended",
	"account has been suspended",
	"account_disabled",
	"account disabled",
	"account is disabled",
	"account has been disabled",
	"user_disabled",
	"user is disabled",
	"account_deleted",
	"account deleted",
	"account_terminated",
	"account terminated",
	"permanently banned",
	"has been banned",
	"user_banned",
	"abuse_detected",
	"terms of service violation",
	"violation of our terms",
	"账号已封禁",
	"账号封禁",
	"已被停用",
}

var cloudflareWAFKeywords = []string{
	"cloudflare",
	"cf-ray",
	"cf_chl",
	"just a moment",
	"attention required",
	"you have been blocked",
	"checking your browser",
	"enable javascript and cookies to continue",
	"challenge-platform",
	"challenge_required",
	"turnstile",
	"arkose",
	"sentinel",
	"forbidden by waf",
	"waf",
	"ip blocked",
	"unable to load site",
	"unusual activity",
}

var proxyTransportKeywords = []string{
	"socks",
	"proxy",
	"connection refused",
	"connection reset",
	"broken pipe",
	"unexpected eof",
	"eof",
	"i/o timeout",
	"tls:",
	"no such host",
	"network is unreachable",
	"context deadline exceeded",
}

var tokenExpiredKeywords = []string{
	"http 401",
	"status 401",
	"unauthorized",
	"token_expired",
	"token_invalidated",
	"invalid_token",
	"invalidated",
	"session expired",
	"token expired",
	"refresh_token",
}

// LooksLikeAccountBanned 判断上游错误或账号错误信息是否明确指向账号永久封禁/停用。
func LooksLikeAccountBanned(statusCode int, message string) bool {
	lower := strings.ToLower(message)
	return containsAny(lower, accountBannedKeywords)
}

// LooksLikeCloudflareOrWAF 判断错误是否属于 Cloudflare / Sentinel / WAF / IP 风控拦截。
// 必须先排除明确账号封禁，避免将账号永久封禁误判为代理风控。
func LooksLikeCloudflareOrWAF(statusCode int, message string) bool {
	if LooksLikeAccountBanned(statusCode, message) {
		return false
	}
	lower := strings.ToLower(message)
	if containsAny(lower, cloudflareWAFKeywords) {
		return true
	}
	if statusCode == http.StatusForbidden || strings.Contains(lower, "http 403") || strings.Contains(lower, "status 403") {
		// 仅当 403 明确声明 token 自身过期且无 WAF 痕迹时才不视作 WAF
		if containsAny(lower, []string{"token_expired", "token_invalidated"}) {
			return false
		}
		return true
	}
	return false
}

// ClassifyAccountErrorReason 对账号最近错误与可选 HTTP 状态码进行统一 5 类根因归类。
func ClassifyAccountErrorReason(statusCode int, message string) string {
	lower := strings.ToLower(strings.TrimSpace(message))
	if LooksLikeAccountBanned(statusCode, message) {
		return ErrCategoryBanned
	}
	if LooksLikeCloudflareOrWAF(statusCode, message) {
		return ErrCategoryWAFProxy
	}
	if LooksLikeImageLimit(statusCode, message) {
		return ErrCategoryImageLimit
	}
	if statusCode == http.StatusTooManyRequests ||
		strings.Contains(lower, "http 429") ||
		strings.Contains(lower, "status 429") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "too many requests") {
		return ErrCategoryRateLimit
	}
	if statusCode == http.StatusUnauthorized || containsAny(lower, tokenExpiredKeywords) {
		return ErrCategoryTokenExpired
	}
	if containsAny(lower, proxyTransportKeywords) {
		return ErrCategoryProxyTransport
	}
	return ErrCategoryOther
}
