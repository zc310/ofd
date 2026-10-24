// Package cache 提供支持容量、权重和淘汰保护的并发安全缓存。
package cache

import (
	"container/list"
	"math"
	"math/bits"
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
	// 用两个字保存精确总权重，允许受保护条目的总权重暂时超过 int64。
	weight   uint64
	weightHi uint64
	items    map[K]*list.Element
	order    *list.List
	onEvict  func(K, V)
	canEvict func(K, V) bool
	mu       sync.Mutex
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
		c.subtractWeightLocked(entry.weight)
		element.Value = lruEntry[K, V]{key: key, value: value, weight: weight, generation: entry.generation + 1}
		c.order.MoveToFront(element)
	} else {
		c.items[key] = c.order.PushFront(lruEntry[K, V]{key: key, value: value, weight: weight, generation: 1})
	}
	var carry uint64
	c.weight, carry = bits.Add64(c.weight, uint64(weight), 0)
	c.weightHi += carry
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
		if !c.overLimitLocked() {
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
		// 锁外检查候选期间，其他 Trim 或权重更新可能已使缓存满足限制。
		if !c.overLimitLocked() {
			c.mu.Unlock()
			return evicted
		}
		current, ok := c.items[selected.entry.key]
		if !ok || current != selected.element || current.Value.(lruEntry[K, V]).generation != selected.entry.generation {
			c.mu.Unlock()
			continue
		}
		entry := current.Value.(lruEntry[K, V])
		delete(c.items, entry.key)
		c.order.Remove(current)
		c.subtractWeightLocked(entry.weight)
		c.mu.Unlock()
		evicted = append(evicted, entry)
	}
}

func (c *LRU[K, V]) overLimitLocked() bool {
	return c.order.Len() > c.capacity ||
		(c.maxWeight > 0 && (c.weightHi > 0 || c.weight > uint64(c.maxWeight)))
}

func (c *LRU[K, V]) subtractWeightLocked(weight int64) {
	var borrow uint64
	c.weight, borrow = bits.Sub64(c.weight, uint64(weight), 0)
	c.weightHi -= borrow
}

// Len 返回当前缓存中的条目数量。
func (c *LRU[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// Values 返回当前缓存的全部值，按最近使用顺序。
//
// 调用方据此做收尾（如释放条目持有的重资源）时要注意：拿到的只是快照，之后
// 缓存仍可能被并发修改；因此适合在已经停用该缓存之后调用。
func (c *LRU[K, V]) Values() []V {
	c.mu.Lock()
	defer c.mu.Unlock()
	values := make([]V, 0, len(c.items))
	for element := c.order.Front(); element != nil; element = element.Next() {
		if entry, ok := element.Value.(lruEntry[K, V]); ok {
			values = append(values, entry.value)
		}
	}
	return values
}

// Weight 返回当前缓存条目的总权重，超过 int64 范围时返回 math.MaxInt64。
// 内部仍精确记账，淘汰或替换条目后会恢复实际可表示的权重。
func (c *LRU[K, V]) Weight() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.weightHi > 0 || c.weight > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(c.weight)
}
