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
