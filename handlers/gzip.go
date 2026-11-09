package handlers

import (
	"compress/gzip"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type gzipResponseWriter struct {
	gin.ResponseWriter
	gz *gzip.Writer
}

func (w *gzipResponseWriter) ensureGzip() bool {
	status := w.Status()
	if status == http.StatusNotModified || status == http.StatusNoContent {
		return false
	}
	if w.gz == nil {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length")
		w.gz = gzip.NewWriter(w.ResponseWriter)
	}
	return true
}

func (w *gzipResponseWriter) Write(data []byte) (int, error) {
	if !w.ensureGzip() {
		return 0, nil
	}
	return w.gz.Write(data)
}

func (w *gzipResponseWriter) WriteString(s string) (int, error) {
	if !w.ensureGzip() {
		return 0, nil
	}
	return w.gz.Write([]byte(s))
}

// GzipMiddleware 基于标准库 compress/gzip 的轻量协商压缩中间件（304 响应不压缩）
func GzipMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Vary", "Accept-Encoding")
		if !strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
			c.Next()
			return
		}

		gw := &gzipResponseWriter{ResponseWriter: c.Writer}
		c.Writer = gw
		defer func() {
			if gw.gz != nil {
				_ = gw.gz.Close()
			}
		}()

		c.Next()
	}
}
