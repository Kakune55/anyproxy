package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// Logger 使用 slog 输出结构化访问日志
func Logger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery
		c.Next()
		if raw != "" { path = path + "?" + raw }
		latency := time.Since(start)
		status := c.Writer.Status()
		logger.Info("HTTP请求",
			"req_id", GetReqID(c),
			"method", c.Request.Method,
			"path", path,
			"status", status,
			"latency_ms", latency.Milliseconds(),
			"size", c.Writer.Size(),
			"ip", c.ClientIP(),
			"ua", c.GetHeader("User-Agent"),
		)
	}
}
