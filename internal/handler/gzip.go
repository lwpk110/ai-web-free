package handler

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

var gzipWriterPool = sync.Pool{
	New: func() interface{} {
		w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
		return w
	},
}

func shouldSkipGzipPath(path string) bool {
	if strings.HasPrefix(path, "/storage/") ||
		strings.HasPrefix(path, "/v1/videos/") ||
		strings.HasPrefix(path, "/v1/audio/files/") ||
		strings.HasSuffix(path, "/events") {
		return true
	}
	switch path {
	case "/v1/chat/completions", "/v1/responses", "/backend-api/conversation":
		return true
	}
	return false
}

func shouldBypassGzipContentType(ct string) bool {
	if ct == "" {
		return false
	}
	lower := strings.ToLower(ct)
	return strings.Contains(lower, "text/event-stream") ||
		strings.HasPrefix(lower, "image/") ||
		strings.HasPrefix(lower, "video/") ||
		strings.HasPrefix(lower, "audio/") ||
		strings.Contains(lower, "application/zip") ||
		strings.Contains(lower, "application/gzip") ||
		strings.Contains(lower, "application/octet-stream")
}

type gzipResponseWriter struct {
	gin.ResponseWriter
	gz          *gzip.Writer
	decided     bool
	compress    bool
	wroteHeader bool
	statusCode  int
}

func (w *gzipResponseWriter) decideCompression(status int) {
	if w.decided {
		return
	}
	w.decided = true

	if status == http.StatusNoContent || status == http.StatusNotModified || status == http.StatusPartialContent {
		w.compress = false
		return
	}
	h := w.ResponseWriter.Header()
	if h.Get("Content-Encoding") != "" {
		w.compress = false
		return
	}
	if shouldBypassGzipContentType(h.Get("Content-Type")) {
		w.compress = false
		return
	}
	w.compress = true
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	h.Add("Vary", "Accept-Encoding")
	w.gz = gzipWriterPool.Get().(*gzip.Writer)
	w.gz.Reset(w.ResponseWriter)
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.statusCode = code
	w.wroteHeader = true
	w.decideCompression(code)
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipResponseWriter) WriteHeaderNow() {
	if !w.wroteHeader {
		code := w.statusCode
		if code == 0 {
			code = http.StatusOK
		}
		w.WriteHeader(code)
	}
	if w.compress {
		w.ResponseWriter.Header().Del("Content-Length")
	}
	w.ResponseWriter.WriteHeaderNow()
}

func (w *gzipResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		code := w.statusCode
		if code == 0 {
			code = http.StatusOK
		}
		w.WriteHeader(code)
	}
	if w.compress && w.gz != nil {
		w.ResponseWriter.Header().Del("Content-Length")
		return w.gz.Write(data)
	}
	return w.ResponseWriter.Write(data)
}

func (w *gzipResponseWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *gzipResponseWriter) Flush() {
	if w.compress && w.gz != nil {
		w.ResponseWriter.Header().Del("Content-Length")
		_ = w.gz.Flush()
	}
	w.ResponseWriter.Flush()
}

func (w *gzipResponseWriter) Close() {
	if w.gz != nil {
		if w.compress {
			w.ResponseWriter.Header().Del("Content-Length")
		}
		_ = w.gz.Close()
		w.gz.Reset(io.Discard)
		gzipWriterPool.Put(w.gz)
		w.gz = nil
	}
}

// gzipMiddleware 对前端 JS/CSS/HTML 与常规 JSON 管理接口启用轻量 BestSpeed gzip 压缩；
// 自动跳过 SSE、WebSocket、Range 与音视频/图片静态资源，零额外第三方依赖。
func gzipMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		req := c.Request
		if req == nil ||
			req.Method == http.MethodHead ||
			!strings.Contains(req.Header.Get("Accept-Encoding"), "gzip") ||
			req.Header.Get("Range") != "" ||
			strings.EqualFold(req.Header.Get("Upgrade"), "websocket") ||
			shouldSkipGzipPath(req.URL.Path) {
			c.Next()
			return
		}

		origWriter := c.Writer
		gzw := &gzipResponseWriter{
			ResponseWriter: origWriter,
			statusCode:     http.StatusOK,
		}
		c.Writer = gzw
		defer func() {
			gzw.Close()
			c.Writer = origWriter
		}()

		c.Next()
	}
}
