package metrics

import (
	"sync/atomic"
	"time"
)

// 环形秒级窗口，用于计算 QPS / QPM。
// 只针对转发请求调用 Inc。

type bucket struct {
	second atomic.Int64 // Unix 秒
	count  atomic.Int64
}

var (
	buckets [60]bucket
	total   atomic.Int64
)

// Inc 增加一次请求计数
func Inc() {
	now := time.Now().Unix()
	idx := now % 60
	b := &buckets[idx]
	for {
		sec := b.second.Load()
		if sec == now {
			b.count.Add(1)
			break
		}
		if b.second.CompareAndSwap(sec, now) {
			b.count.Store(1)
			break
		}
	}
	total.Add(1)
}

// QPS 返回当前秒内的请求数
func QPS() int64 {
	now := time.Now().Unix()
	idx := now % 60
	b := &buckets[idx]
	if b.second.Load() == now { return b.count.Load() }
	return 0
}

// QPM 返回最近 60 秒内的请求总数
func QPM() int64 {
	now := time.Now().Unix()
	var sum int64
	for i := range 60 {
		sec := buckets[i].second.Load()
		if sec <= now && now-sec < 60 { // 在窗口内
			sum += buckets[i].count.Load()
		}
	}
	return sum
}

// Total 返回累计转发请求数
func Total() int64 { return total.Load() }
