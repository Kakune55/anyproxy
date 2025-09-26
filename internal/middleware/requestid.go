package middleware

import (
	"fmt"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

const RequestIDKey = "reqID"

var globalReqID atomic.Int64

// RequestID 生成自增的请求 ID 并注入上下文及响应头
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := globalReqID.Add(1)
		c.Set(RequestIDKey, id)
		c.Writer.Header().Set("X-Request-ID", fmt.Sprintf("%d", id))
		c.Next()
	}
}

// GetReqID 从上下文中获取请求 ID
func GetReqID(c *gin.Context) int64 {
	if v, ok := c.Get(RequestIDKey); ok {
		if id, ok2 := v.(int64); ok2 {
			return id
		}
	}
	return 0
}
