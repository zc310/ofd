package utils

import (
	"container/list"
	"sync"
)

type lruEntry[K comparable, V any] struct {
	key        K
	value      V
	weight     int64
	generation uint64
}

// LRU 是一个并发安全的最近最少使用缓存。
// 缓存同时受条目数量和可选总权重限制；条目淘汰后会在缓存锁释放后调用 onEvict。
type LRU[K comparable, V any] struct {
	capacity  int
	maxWeight int64
	weight    int64
	items     map[K]*list.Element
	order     *list.List
	onEvict   func(K, V)
	canEvict  func(K, V) bool
	mu        sync.Mutex
}

type lruCandidate[K comparable, V any] struct {
	element *list.Element
	entry   lruEntry[K, V]
}

// NewLRU 创建只按条目数量限制的 LRU 缓存。
func NewLRU[K comparable, V any](capacity int, onEvict func(K, V)) *LRU[K, V] {
	return NewWeightedLRU(capacity, 0, onEvict)
}

// NewWeightedLRU 创建按条目数量和总权重限制的 LRU 缓存。
// maxWeight 小于等于 0 表示不启用权重限制。可选的 canEvict 只用于暂时保护条目。
func NewWeightedLRU[K comparable, V any](capacity int, maxWeight int64, onEvict func(K, V), canEvict ...func(K, V) bool) *LRU[K, V] {
	if capacity < 1 {
		capacity = 1
	}
	if maxWeight < 0 {
		maxWeight = 0
	}
	var policy func(K, V) bool
	if len(canEvict) > 0 {
		policy = canEvict[0]
	}
	return &LRU[K, V]{
		capacity:  capacity,
		maxWeight: maxWeight,
		items:     make(map[K]*list.Element, capacity),
		order:     list.New(),
		onEvict:   onEvict,
		canEvict:  policy,
	}
}

// Get 获取 key 对应的值，并将该条目提升为最近使用条目。
func (c *LRU[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.items[key]; ok {
		c.order.MoveToFront(element)
		return element.Value.(lruEntry[K, V]).value, true
	}
	var zero V
	return zero, false
}

// Add 添加一个权重为 1 的条目。
func (c *LRU[K, V]) Add(key K, value V) {
	c.AddWeighted(key, value, 1)
}

// AddWeighted 添加一个带权重的条目；负权重按 0 处理。
func (c *LRU[K, V]) AddWeighted(key K, value V, weight int64) {
	c.mu.Lock()
	if weight < 0 {
		weight = 0
	}
	if element, ok := c.items[key]; ok {
		entry := element.Value.(lruEntry[K, V])
		c.weight -= entry.weight
		element.Value = lruEntry[K, V]{key: key, value: value, weight: weight, generation: entry.generation + 1}
		c.weight += weight
		c.order.MoveToFront(element)
	} else {
		c.items[key] = c.order.PushFront(lruEntry[K, V]{key: key, value: value, weight: weight, generation: 1})
		c.weight += weight
	}
	c.mu.Unlock()
	evicted := c.trim()
	if c.onEvict != nil {
		for _, entry := range evicted {
			c.onEvict(entry.key, entry.value)
		}
	}
}

// Trim 主动按当前容量和权重限制清理可淘汰条目。
// 被淘汰策略保护的条目会暂时保留，待之后再次调用 Trim 时处理。
func (c *LRU[K, V]) Trim() {
	evicted := c.trim()
	if c.onEvict != nil {
		for _, entry := range evicted {
			c.onEvict(entry.key, entry.value)
		}
	}
}

func (c *LRU[K, V]) trim() []lruEntry[K, V] {
	var evicted []lruEntry[K, V]
	for {
		c.mu.Lock()
		if c.order.Len() <= c.capacity && (c.maxWeight <= 0 || c.weight <= c.maxWeight) {
			c.mu.Unlock()
			return evicted
		}
		candidates := make([]lruCandidate[K, V], 0, c.order.Len())
		for element := c.order.Back(); element != nil; element = element.Prev() {
			candidates = append(candidates, lruCandidate[K, V]{element: element, entry: element.Value.(lruEntry[K, V])})
		}
		c.mu.Unlock()

		var selected *lruCandidate[K, V]
		for index := range candidates {
			candidate := &candidates[index]
			if c.canEvict == nil || c.canEvict(candidate.entry.key, candidate.entry.value) {
				selected = candidate
				break
			}
		}
		if selected == nil {
			return evicted
		}

		c.mu.Lock()
		current, ok := c.items[selected.entry.key]
		if !ok || current != selected.element || current.Value.(lruEntry[K, V]).generation != selected.entry.generation {
			c.mu.Unlock()
			continue
		}
		entry := current.Value.(lruEntry[K, V])
		delete(c.items, entry.key)
		c.order.Remove(current)
		c.weight -= entry.weight
		c.mu.Unlock()
		evicted = append(evicted, entry)
	}
}

// Len 返回当前缓存中的条目数量。
func (c *LRU[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// Weight 返回当前缓存条目的总权重。
func (c *LRU[K, V]) Weight() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.weight
}
