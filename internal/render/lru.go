package render

import "container/list"

type lruEntry[K comparable, V any] struct {
	key   K
	value V
}

type lruCache[K comparable, V any] struct {
	capacity int
	items    map[K]*list.Element
	order    *list.List
}

func newLRU[K comparable, V any](capacity int) *lruCache[K, V] {
	if capacity < 1 {
		capacity = 1
	}
	return &lruCache[K, V]{
		capacity: capacity,
		items:    make(map[K]*list.Element, capacity),
		order:    list.New(),
	}
}

func (c *lruCache[K, V]) get(key K) (V, bool) {
	if element, ok := c.items[key]; ok {
		c.order.MoveToFront(element)
		return element.Value.(lruEntry[K, V]).value, true
	}
	var zero V
	return zero, false
}

func (c *lruCache[K, V]) add(key K, value V) {
	if element, ok := c.items[key]; ok {
		element.Value = lruEntry[K, V]{key: key, value: value}
		c.order.MoveToFront(element)
		return
	}
	c.items[key] = c.order.PushFront(lruEntry[K, V]{key: key, value: value})
	if c.order.Len() > c.capacity {
		element := c.order.Back()
		delete(c.items, element.Value.(lruEntry[K, V]).key)
		c.order.Remove(element)
	}
}

func (c *lruCache[K, V]) len() int {
	return c.order.Len()
}
