package handler

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/davisu-china/tidings/backend/internal/pkg/metrics"
)

// Metrics 挂 /metrics 端点，供 Prometheus 抓取。
func (h *Handler) Metrics() gin.HandlerFunc {
	return gin.WrapH(promhttp.Handler())
}

// MetricsMiddleware 记录每个请求的方法、路径、状态码与耗时。
//
// 路径上带 id 的（如 /introductions/:id/respond）如果不归一化，
// 会让指标基数爆炸 —— 每个 id 都会产生一组新序列。
// 这里把数字段归一成 :id。
func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		path := normalizePath(c.Request.URL.Path)
		status := strconv.Itoa(c.Writer.Status())

		metrics.HTTPRequests.WithLabelValues(c.Request.Method, path, status).Inc()
		metrics.HTTPLatency.WithLabelValues(c.Request.Method, path).
			Observe(time.Since(start).Seconds())
	}
}

// normalizePath 把路径里的数字段归并，控制指标基数。
func normalizePath(path string) string {
	// 简单实现：逐段扫描，纯数字段替换为 :id
	parts := make([]byte, 0, len(path))
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			parts = append(parts, '/')
			continue
		}
		j := i
		for j < len(path) && path[j] != '/' {
			j++
		}
		seg := path[i:j]
		if isNumeric(seg) {
			parts = append(parts, ':', 'i', 'd')
		} else {
			parts = append(parts, seg...)
		}
		i = j - 1
	}
	return string(parts)
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
