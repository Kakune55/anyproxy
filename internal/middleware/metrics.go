package middleware

import (
	"time"

	"github.com/gin-gonic/gin"

	"anyproxy/internal/metrics"
)

// MetricsHandler 输出当前指标
func MetricsHandler(c *gin.Context) {
	qps := metrics.QPS()
	qpm := metrics.QPM()
	c.JSON(200, gin.H{
		"qps_current": qps,
		"qps_avg_60s": float64(qpm) / 60.0,
		"qpm_current": qpm,
		"qpm_avg_60m": float64(qpm),
		"total":       metrics.Total(),
		"timestamp":   time.Now().Unix(),
	})
}
