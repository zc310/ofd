package cache

import (
	"math"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWeightedLRUWithoutProtectionKeepsRecencyAndEvictionOrder(t *testing.T) {
	var evicted []string
	cache := NewWeightedLRU[string, int](3, 6, func(key string, _ int) {
		evicted = append(evicted, key)
	})
	cache.AddWeighted("a", 1, 2)
	cache.AddWeighted("b", 2, 2)
	cache.AddWeighted("c", 3, 2)
	_, _ = cache.Get("a")
	cache.AddWeighted("a", 4, 4)
	// 增加已存在条目的权重应淘汰最久未访问的 b；大条目 d 再淘汰 c 和 a。
	cache.AddWeighted("d", 5, 5)
	if !slices.Equal(evicted, []string{"b", "c", "a"}) {
		t.Fatalf("淘汰顺序=%v，期望[b c a]", evicted)
	}
	if cache.Len() != 1 || cache.Weight() != 5 {
		t.Fatalf("条目数/权重=%d/%d，期望1/5", cache.Len(), cache.Weight())
	}
	if value, ok := cache.Get("d"); !ok || value != 5 {
		t.Fatal("应保留最新条目 d")
	}
}

// 满容量下持续插入新条目，检查淘汰成本不再随缓存容量线性增长。
func BenchmarkLRUEvictionWithoutProtection(b *testing.B) {
	for _, capacity := range []int{16, 1024, 4096} {
		b.Run(strconv.Itoa(capacity), func(b *testing.B) {
			cache := NewLRU[int, int](capacity, nil)
			for key := range capacity {
				cache.Add(key, key)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for index := range b.N {
				key := capacity + index
				cache.Add(key, key)
			}
		})
	}
}

func TestWeightedLRUEvictsByWeight(t *testing.T) {
	var evicted []string
	cache := NewWeightedLRU[string, int](3, 10, func(key string, _ int) {
		evicted = append(evicted, key)
	})
	cache.AddWeighted("small", 1, 4)
	cache.AddWeighted("large", 2, 8)
	if cache.Len() != 1 || cache.Weight() != 8 {
		t.Fatalf("cache size = %d/%d, want 1/8", cache.Len(), cache.Weight())
	}
	if len(evicted) != 1 || evicted[0] != "small" {
		t.Fatalf("evicted = %v, want [small]", evicted)
	}
}

func TestWeightedLRUEvictsOversizedEntry(t *testing.T) {
	var evicted []string
	cache := NewWeightedLRU[string, int](2, 5, func(key string, _ int) {
		evicted = append(evicted, key)
	})
	cache.AddWeighted("large", 1, 10)
	if cache.Len() != 0 || cache.Weight() != 0 {
		t.Fatalf("cache size = %d/%d, want 0/0", cache.Len(), cache.Weight())
	}
	if len(evicted) != 1 || evicted[0] != "large" {
		t.Fatalf("evicted = %v, want [large]", evicted)
	}
}

func TestWeightedLRUSkipsProtectedEntry(t *testing.T) {
	var evicted []string
	protected := map[string]bool{"pinned": true}
	cache := NewWeightedLRU[string, int](1, 0, func(key string, _ int) {
		evicted = append(evicted, key)
	}, func(key string, _ int) bool {
		return !protected[key]
	})
	cache.Add("pinned", 1)
	cache.Add("other", 2)
	if cache.Len() != 1 {
		t.Fatalf("cache size = %d, want 1 while protected entry is retained", cache.Len())
	}
	if _, ok := cache.Get("pinned"); !ok {
		t.Fatal("protected entry was evicted")
	}
	if len(evicted) != 1 || evicted[0] != "other" {
		t.Fatalf("evicted = %v, want [other]", evicted)
	}
}

func TestWeightedLRUTrimAfterProtectionIsRemoved(t *testing.T) {
	protected := map[string]bool{"pinned": true, "other": true}
	cache := NewWeightedLRU[string, int](1, 0, nil, func(key string, _ int) bool {
		return !protected[key]
	})
	cache.Add("pinned", 1)
	cache.Add("other", 2)
	if cache.Len() != 2 {
		t.Fatalf("cache size = %d, want 2 while protected entries are retained", cache.Len())
	}
	protected["pinned"] = false
	protected["other"] = false
	cache.Trim()
	if cache.Len() != 1 {
		t.Fatalf("cache size after trim = %d, want 1", cache.Len())
	}
	if _, ok := cache.Get("other"); !ok {
		t.Fatal("newer entry should remain after trim")
	}
}

func TestWeightedLRUConcurrentAccess(t *testing.T) {
	cache := NewWeightedLRU[int, int](32, 128, nil)
	var group sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		group.Add(1)
		go func(offset int) {
			defer group.Done()
			for index := 0; index < 200; index++ {
				key := offset*200 + index
				cache.AddWeighted(key, key, int64((key%4)+1))
				_, _ = cache.Get(key - 1)
				_ = cache.Len()
				_ = cache.Weight()
			}
		}(worker)
	}
	group.Wait()
	if cache.Len() > 32 || cache.Weight() > 128 {
		t.Fatalf("cache size/weight = %d/%d, want at most 32/128", cache.Len(), cache.Weight())
	}
}

func TestWeightedLRUEvictCallbackCanReenterCache(t *testing.T) {
	cacheReady := make(chan *LRU[string, int], 1)
	finished := make(chan struct{})
	cache := NewLRU[string, int](1, func(key string, _ int) {
		cache := <-cacheReady
		_, _ = cache.Get(key)
		_ = cache.Len()
		close(finished)
	})
	cacheReady <- cache
	cache.Add("first", 1)
	cache.Add("second", 2)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("淘汰回调重入缓存时发生死锁")
	}
}

func TestWeightedLRUCanEvictCallbackCanReenterCache(t *testing.T) {
	cacheReady := make(chan *LRU[string, int], 1)
	checked := make(chan struct{})
	cache := NewWeightedLRU[string, int](1, 0, nil, func(key string, _ int) bool {
		cache := <-cacheReady
		_, _ = cache.Get(key)
		close(checked)
		return true
	})
	cacheReady <- cache
	cache.Add("first", 1)
	cache.Add("second", 2)
	select {
	case <-checked:
	case <-time.After(time.Second):
		t.Fatal("canEvict 回调重入缓存时发生死锁")
	}
}

func TestWeightedLRUConcurrentTrimStopsWhenWithinLimits(t *testing.T) {
	for _, limit := range []struct {
		name      string
		capacity  int
		maxWeight int64
	}{
		{"capacity", 1, 0},
		{"weight", 2, 1},
	} {
		t.Run(limit.name, func(t *testing.T) {
			var enabled atomic.Bool
			var checks atomic.Int32
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			evicted := make(chan string, 2)
			cache := NewWeightedLRU[string, int](limit.capacity, limit.maxWeight, func(key string, _ int) {
				evicted <- key
			}, func(key string, _ int) bool {
				if !enabled.Load() {
					return false
				}
				if checks.Add(1) == 1 {
					close(entered)
					<-release
					return true
				}
				return key == "b"
			})
			cache.Add("a", 1)
			cache.Add("b", 2)
			enabled.Store(true)
			done := make(chan struct{})
			go func() {
				cache.Trim()
				close(done)
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("第一次 Trim 未进入候选检查")
			}
			cache.Trim()
			unblock()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("第一次 Trim 未结束")
			}
			if cache.Len() != 1 || cache.Weight() != 1 {
				t.Fatalf("并发 Trim 后条目数/权重 = %d/%d; want 1/1", cache.Len(), cache.Weight())
			}
			if _, ok := cache.Get("a"); !ok {
				t.Fatal("已满足限制后仍淘汰了剩余条目")
			}
			if len(evicted) != 1 || <-evicted != "b" {
				t.Fatal("应仅通知 b 被淘汰")
			}
		})
	}
}

func TestWeightedLRUWeightOverflowDoesNotBypassLimit(t *testing.T) {
	for _, operation := range []string{"insert", "replace"} {
		t.Run(operation, func(t *testing.T) {
			cache := NewWeightedLRU[string, int](2, math.MaxInt64, nil)
			cache.AddWeighted("a", 1, math.MaxInt64-1)
			if operation == "replace" {
				cache.AddWeighted("b", 2, 1)
			}
			cache.AddWeighted("b", 2, 2)
			if cache.Len() != 1 || cache.Weight() != 2 {
				t.Fatalf("溢出后条目数/权重 = %d/%d; want 1/2", cache.Len(), cache.Weight())
			}
			if _, ok := cache.Get("b"); !ok {
				t.Fatal("应保留新条目 b")
			}
		})
	}
}

func TestWeightedLRUTracksProtectedWeightBeyondUint64(t *testing.T) {
	protected := true
	cache := NewWeightedLRU[string, int](3, math.MaxInt64, nil, func(string, int) bool {
		return !protected
	})
	cache.AddWeighted("a", 1, math.MaxInt64)
	cache.AddWeighted("b", 2, math.MaxInt64)
	cache.AddWeighted("c", 3, 2)
	if cache.Len() != 3 || cache.Weight() != math.MaxInt64 {
		t.Fatalf("受保护的总权重应以 MaxInt64 返回: %d/%d", cache.Len(), cache.Weight())
	}
	protected = false
	cache.Trim()
	if cache.Len() != 1 || cache.Weight() != 2 {
		t.Fatalf("移除保护后条目数/权重 = %d/%d; want 1/2", cache.Len(), cache.Weight())
	}
	if _, ok := cache.Get("c"); !ok {
		t.Fatal("应保留 c")
	}
}

func TestWeightedLRURecoversExactWeightAfterOverflowAndReplacement(t *testing.T) {
	cache := NewWeightedLRU[string, int](2, math.MaxInt64, nil, func(string, int) bool { return false })
	cache.AddWeighted("a", 1, math.MaxInt64)
	cache.AddWeighted("b", 2, 1)
	if cache.Weight() != math.MaxInt64 {
		t.Fatalf("超范围权重 = %d; want MaxInt64", cache.Weight())
	}
	cache.AddWeighted("a", 1, 1)
	if cache.Len() != 2 || cache.Weight() != 2 {
		t.Fatalf("替换后条目数/权重 = %d/%d; want 2/2", cache.Len(), cache.Weight())
	}
}
