package canvasfont

// shapingCache 在调用方持有对应缓存锁时使用。命中/淘汰均为 O(1)，
// 满容量时复用最旧节点，避免每次缓存未命中都分配链表节点。
type shapingCache[K comparable, V any] struct {
	entries map[K]*shapingEntry[K, V]
	first   *shapingEntry[K, V]
	last    *shapingEntry[K, V]
}

type shapingEntry[K comparable, V any] struct {
	key        K
	value      V
	prev, next *shapingEntry[K, V]
}

func (c *shapingCache[K, V]) Len() int { return len(c.entries) }

func (c *shapingCache[K, V]) Get(key K) (V, bool) {
	if entry := c.entries[key]; entry != nil {
		c.touch(entry)
		return entry.value, true
	}
	var zero V
	return zero, false
}

func (c *shapingCache[K, V]) Add(key K, value V, capacity int) {
	if entry := c.entries[key]; entry != nil {
		entry.value = value
		c.touch(entry)
		return
	}
	var entry *shapingEntry[K, V]
	if len(c.entries) >= capacity && c.last != nil {
		entry = c.last
		delete(c.entries, entry.key)
		c.unlink(entry)
	} else {
		entry = &shapingEntry[K, V]{}
		if c.entries == nil {
			c.entries = make(map[K]*shapingEntry[K, V])
		}
	}
	entry.key, entry.value = key, value
	c.entries[key] = entry
	c.pushFront(entry)
}

func (c *shapingCache[K, V]) RemoveIf(match func(K) bool) {
	for key, entry := range c.entries {
		if match(key) {
			delete(c.entries, key)
			c.unlink(entry)
		}
	}
}

func (c *shapingCache[K, V]) touch(entry *shapingEntry[K, V]) {
	if c.first != entry {
		c.unlink(entry)
		c.pushFront(entry)
	}
}

func (c *shapingCache[K, V]) unlink(entry *shapingEntry[K, V]) {
	if entry.prev != nil {
		entry.prev.next = entry.next
	} else {
		c.first = entry.next
	}
	if entry.next != nil {
		entry.next.prev = entry.prev
	} else {
		c.last = entry.prev
	}
	entry.prev, entry.next = nil, nil
}

func (c *shapingCache[K, V]) pushFront(entry *shapingEntry[K, V]) {
	entry.next = c.first
	if c.first != nil {
		c.first.prev = entry
	} else {
		c.last = entry
	}
	c.first = entry
}
