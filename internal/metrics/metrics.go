package metrics

import (
	"sync/atomic"
	"time"
)

type bucket struct {
	second atomic.Int64
	count  atomic.Int64
}

var (
	buckets [60]bucket
	total   atomic.Int64
)

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

func QPS() int64 {
	now := time.Now().Unix()
	idx := now % 60
	b := &buckets[idx]
	if b.second.Load() == now {
		return b.count.Load()
	}
	return 0
}

func QPM() int64 {
	now := time.Now().Unix()
	var sum int64
	for i := range 60 {
		sec := buckets[i].second.Load()
		if sec <= now && now-sec < 60 {
			sum += buckets[i].count.Load()
		}
	}
	return sum
}

func Total() int64 { return total.Load() }
