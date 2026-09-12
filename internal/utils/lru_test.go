package utils

import (
	"sync"
	"testing"
	"time"
)

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
