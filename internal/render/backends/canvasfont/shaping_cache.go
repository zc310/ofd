package canvasfont

// shapingCache 在调用方持有对应缓存锁时使用。命中/淘汰均为 O(1)，
// 满容量时复用最旧节点，避免每次缓存未命中都分配链表节点。
type shapingCache[K comparable, V any] struct {
	entries map[K]*shapingEntry[K, V]
	first   *shapingEntry[K, V]
	last    *shapingEntry[K, V]
	weight  int64
}

type shapingEntry[K comparable, V any] struct {
	key        K
	value      V
	weight     int64
	prev, next *shapingEntry[K, V]
}

func (c *shapingCache[K, V]) Len() int { return len(c.entries) }

func (c *shapingCache[K, V]) Weight() int64 { return c.weight }

func (c *shapingCache[K, V]) Get(key K) (V, bool) {
	if entry := c.entries[key]; entry != nil {
		c.touch(entry)
		return entry.value, true
	}
	var zero V
	return zero, false
}

func (c *shapingCache[K, V]) Add(key K, value V, capacity int) {
	c.AddWeighted(key, value, capacity, 0, 1)
}

// AddWeighted 同时限制条目和估算字节；超大单项不冲掉已有热条目。
func (c *shapingCache[K, V]) AddWeighted(key K, value V, capacity int, maxWeight, weight int64) {
	if capacity <= 0 || weight < 0 || maxWeight > 0 && weight > maxWeight {
		return
	}
	entry := c.entries[key]
	if entry != nil {
		delete(c.entries, key)
		c.weight -= entry.weight
		c.unlink(entry)
	}
	for c.last != nil && (len(c.entries) >= capacity || maxWeight > 0 && c.weight > maxWeight-weight) {
		entry = c.last
		delete(c.entries, entry.key)
		c.weight -= entry.weight
		c.unlink(entry)
	}
	if entry == nil {
		entry = &shapingEntry[K, V]{}
		if c.entries == nil {
			c.entries = make(map[K]*shapingEntry[K, V])
		}
	}
	entry.key, entry.value = key, value
	entry.weight = weight
	c.weight += weight
	c.entries[key] = entry
	c.pushFront(entry)
}

func (c *shapingCache[K, V]) RemoveIf(match func(K) bool) {
	for key, entry := range c.entries {
		if match(key) {
			delete(c.entries, key)
			c.weight -= entry.weight
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
