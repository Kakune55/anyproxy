package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// 环形秒级窗口，用于计算 QPS / QPM。
// 只针对转发请求调用 Inc。

type bucket struct {
	second int64 // Unix 秒
	count  int64
}

var (
	buckets [60]bucket
	mu      sync.Mutex
	total   atomic.Int64
)

// Inc 增加一次请求计数
func Inc() {
	now := time.Now().Unix()
	idx := now % 60
	mu.Lock()
	b := &buckets[idx]
	if b.second != now { // 该槽位属于旧秒，重置
		b.second = now
		b.count = 0
	}
	b.count++
	mu.Unlock()
	total.Add(1)
}

// QPS 返回当前秒内的请求数
func QPS() int64 {
	now := time.Now().Unix()
	idx := now % 60
	mu.Lock()
	b := buckets[idx]
	mu.Unlock()
	if b.second == now { return b.count }
	return 0
}

// QPM 返回最近 60 秒内的请求总数
func QPM() int64 {
	now := time.Now().Unix()
	var sum int64
	mu.Lock()
	for i := 0; i < 60; i++ {
		b := buckets[i]
		if now-b.second < 60 { // 在窗口内
			sum += b.count
		}
	}
	mu.Unlock()
	return sum
}

// Total 返回累计转发请求数
func Total() int64 { return total.Load() }
