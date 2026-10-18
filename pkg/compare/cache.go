package compare

import (
	"container/list"
	"image"
)

// 缓存只属于一次比较的基准快照，DPI 固定；按实际像素缓冲区字节数淘汰。
type pageCache struct {
	limit, bytes int64
	items        map[int]*list.Element
	order        list.List
}

type cachedPage struct {
	number int
	image  *image.RGBA
}

func newPageCache(limit int64) *pageCache {
	return &pageCache{limit: limit, items: map[int]*list.Element{}}
}

func (c *pageCache) get(number int) *image.RGBA {
	if c == nil {
		return nil
	}
	if e := c.items[number]; e != nil {
		c.order.MoveToFront(e)
		return e.Value.(cachedPage).image
	}
	return nil
}

func (c *pageCache) put(number int, img *image.RGBA) {
	if c == nil || img == nil {
		return
	}
	size := int64(len(img.Pix))
	if size > c.limit {
		return
	}
	if old := c.items[number]; old != nil {
		c.bytes -= int64(len(old.Value.(cachedPage).image.Pix))
		c.order.Remove(old)
		delete(c.items, number)
	}
	for c.bytes > c.limit-size {
		e := c.order.Back()
		p := e.Value.(cachedPage)
		c.bytes -= int64(len(p.image.Pix))
		delete(c.items, p.number)
		c.order.Remove(e)
	}
	c.items[number] = c.order.PushFront(cachedPage{number, img})
	c.bytes += size
}
