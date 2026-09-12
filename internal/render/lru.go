package render

import "github.com/zc310/ofd/internal/utils"

type lruCache[K comparable, V any] struct {
	*utils.LRU[K, V]
}

func newLRU[K comparable, V any](capacity int) *lruCache[K, V] {
	return &lruCache[K, V]{LRU: utils.NewLRU[K, V](capacity, nil)}
}

func (c *lruCache[K, V]) get(key K) (V, bool) {
	return c.Get(key)
}

func (c *lruCache[K, V]) add(key K, value V) {
	c.Add(key, value)
}

func (c *lruCache[K, V]) len() int {
	return c.Len()
}
