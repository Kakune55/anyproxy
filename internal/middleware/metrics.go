package middleware

import (
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// 简易 QPS / QPM 统计：使用滑动窗口环形数组按秒/按分钟聚合
// secondBuckets: 最近 60 秒每秒的请求计数
// minuteBuckets: 最近 60 分钟每分钟的请求计数

var (
	secondBuckets [60]atomic.Int64
	minuteBuckets [60]atomic.Int64
	lastSecond int64
	lastMinute int64
	// 总请求数 (复用可选)
	totalRequests atomic.Int64
)

func init() {
	now := time.Now()
	lastSecond = now.Unix()
	lastMinute = now.Unix() / 60
}

// AddRequest 在收到一个请求时调用，通常在请求完成后计数
func AddRequest() {
	now := time.Now()
	sec := now.Unix()
	min := sec / 60

	// 处理秒级 bucket
	oldSec := atomic.LoadInt64(&lastSecond)
	if sec != oldSec {
		// 跨秒：清理可能跨越多个秒的间隙
		if atomic.CompareAndSwapInt64(&lastSecond, oldSec, sec) {
			steps := int(sec - oldSec)
			if steps > 60 { steps = 60 }
			for i := 1; i <= steps; i++ {
				idx := int((oldSec+int64(i)) % 60)
				secondBuckets[idx].Store(0)
			}
		}
	}
	secIdx := int(sec % 60)
	secondBuckets[secIdx].Add(1)

	// 处理分钟级 bucket
	oldMin := atomic.LoadInt64(&lastMinute)
	if min != oldMin {
		if atomic.CompareAndSwapInt64(&lastMinute, oldMin, min) {
			steps := int(min - oldMin)
			if steps > 60 { steps = 60 }
			for i := 1; i <= steps; i++ {
				idx := int((oldMin+int64(i)) % 60)
				minuteBuckets[idx].Store(0)
			}
		}
	}
	minIdx := int(min % 60)
	minuteBuckets[minIdx].Add(1)

	totalRequests.Add(1)
}

// CurrentQPS 返回最近 1 秒（当前秒）的请求数
func CurrentQPS() int64 {
	sec := time.Now().Unix()
	if sec != atomic.LoadInt64(&lastSecond) { return 0 }
	return secondBuckets[sec%60].Load()
}

// AvgQPSRecent60 返回最近 60 秒平均 QPS
func AvgQPSRecent60() float64 {
	sec := time.Now().Unix()
	total := int64(0)
	last := atomic.LoadInt64(&lastSecond)
	for i := 0; i < 60; i++ {
		// 只统计在窗口内(未被清零)的 bucket
		bucketSec := sec - int64(i)
		if bucketSec <= last && last-bucketSec < 60 {
			idx := bucketSec % 60
			total += secondBuckets[idx].Load()
		}
	}
	return float64(total) / 60.0
}

// CurrentQPM 返回当前分钟的请求数
func CurrentQPM() int64 {
	min := time.Now().Unix() / 60
	if min != atomic.LoadInt64(&lastMinute) { return 0 }
	return minuteBuckets[min%60].Load()
}

// AvgQPMRecent60 返回最近 60 分钟的平均 QPM
func AvgQPMRecent60() float64 {
	min := time.Now().Unix() / 60
	total := int64(0)
	last := atomic.LoadInt64(&lastMinute)
	for i := 0; i < 60; i++ {
		bucketMin := min - int64(i)
		if bucketMin <= last && last-bucketMin < 60 {
			idx := bucketMin % 60
			total += minuteBuckets[idx].Load()
		}
	}
	return float64(total) / 60.0
}

// TotalRequests 返回总请求量（从进程启动以来）
func TotalRequests() int64 { return totalRequests.Load() }

// MetricsHandler 输出当前指标
func MetricsHandler(c *gin.Context) {
	c.JSON(200, gin.H{
		"qps_current": CurrentQPS(),
		"qps_avg_60s": AvgQPSRecent60(),
		"qpm_current": CurrentQPM(),
		"qpm_avg_60m": AvgQPMRecent60(),
		"total":       TotalRequests(),
		"timestamp":   time.Now().Unix(),
	})
}
