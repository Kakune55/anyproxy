package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

// Recovery 捕获 panic 并记录堆栈信息
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rcv := recover(); rcv != nil {
				logger.Error("发生Panic",
					"req_id", GetReqID(c),
					"error", rcv,
					"stack", string(debug.Stack()),
					"path", c.Request.URL.Path,
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error":  "内部服务器错误",
					"req_id": GetReqID(c),
					"source": "anyproxy",
				})
			}
		}()
		c.Next()
	}
}
