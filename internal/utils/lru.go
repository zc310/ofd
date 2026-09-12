package utils

import "container/list"

type lruEntry[K comparable, V any] struct {
	key   K
	value V
}

type LRU[K comparable, V any] struct {
	capacity int
	items    map[K]*list.Element
	order    *list.List
	onEvict  func(K, V)
}

func NewLRU[K comparable, V any](capacity int, onEvict func(K, V)) *LRU[K, V] {
	if capacity < 1 {
		capacity = 1
	}
	return &LRU[K, V]{
		capacity: capacity,
		items:    make(map[K]*list.Element, capacity),
		order:    list.New(),
		onEvict:  onEvict,
	}
}

func (c *LRU[K, V]) Get(key K) (V, bool) {
	if element, ok := c.items[key]; ok {
		c.order.MoveToFront(element)
		return element.Value.(lruEntry[K, V]).value, true
	}
	var zero V
	return zero, false
}

func (c *LRU[K, V]) Add(key K, value V) {
	if element, ok := c.items[key]; ok {
		element.Value = lruEntry[K, V]{key: key, value: value}
		c.order.MoveToFront(element)
		return
	}
	c.items[key] = c.order.PushFront(lruEntry[K, V]{key: key, value: value})
	if c.order.Len() <= c.capacity {
		return
	}
	element := c.order.Back()
	if element == nil {
		return
	}
	entry := element.Value.(lruEntry[K, V])
	delete(c.items, entry.key)
	c.order.Remove(element)
	if c.onEvict != nil {
		c.onEvict(entry.key, entry.value)
	}
}

func (c *LRU[K, V]) Len() int {
	return c.order.Len()
}
