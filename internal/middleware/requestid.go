package middleware

import (
	"strconv"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

const RequestIDKey = "reqID"

var globalReqID atomic.Int64

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := globalReqID.Add(1)
		c.Set(RequestIDKey, id)
		c.Writer.Header().Set("X-Request-ID", strconv.FormatInt(id, 10))
		c.Next()
	}
}

func GetReqID(c *gin.Context) int64 {
	if v, ok := c.Get(RequestIDKey); ok {
		if id, ok2 := v.(int64); ok2 {
			return id
		}
	}
	return 0
}
