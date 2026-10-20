package canvas

import (
	"fmt"
	"testing"

	"github.com/zc310/canvas"
	"golang.org/x/image/font/gofont/goregular"
)

func TestShapingCachesKeepHotEntriesAtCapacity(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	family, err := loadCachedEmbeddedFont(fonts, t.Name(), goregular.TTF, FontRegular, nil)
	if err != nil {
		t.Fatal(err)
	}
	face := family.Face(12, canvas.Black)
	path, line := fonts.shapedTextPath(face, "hot"), fonts.shapedTextLine(face, "hot")
	for i := 0; i < maxTextWidthCacheEntries+10; i++ {
		if fonts.shapedTextPath(face, "hot") != path || fonts.shapedTextLine(face, "hot") != line {
			t.Fatal("容量压力淘汰了最近访问的轮廓或整形结果")
		}
		fonts.cachedTextWidth(face, "hot")
		cold := fmt.Sprintf("cold-%d", i)
		fonts.shapedTextPath(face, cold)
		fonts.shapedTextLine(face, cold)
		fonts.cachedTextWidth(face, cold)
	}
	key := textWidthKey{font: face.Font, size: face.Size, style: FontRegular, variant: face.Variant,
		direction: face.Direction, script: face.Script, language: face.Language, value: "hot", mmPerEm: face.MmPerEm}
	if _, ok := fonts.textWidthCache.Get(key); !ok {
		t.Fatal("容量压力淘汰了最近访问的宽度")
	}
	if fonts.pathCache.Len() != 2048 || fonts.textLineCache.Len() != 2048 || fonts.textWidthCache.Len() != maxTextWidthCacheEntries {
		t.Fatal("整形缓存没有遵守各自容量上限")
	}
}

func TestShapingCacheRemoveAndEvict(t *testing.T) {
	var cache shapingCache[int, string]
	for i := 0; i < 3; i++ {
		cache.Add(i, fmt.Sprint(i), 3)
	}
	cache.Get(0)
	cache.Add(3, "3", 3)
	if _, ok := cache.Get(1); ok {
		t.Fatal("最久未用条目没有被淘汰")
	}
	cache.RemoveIf(func(key int) bool { return key != 0 })
	cache.Add(4, "4", 3)
	cache.Add(5, "5", 3)
	cache.Add(6, "6", 3)
	if _, ok := cache.Get(0); ok || cache.Len() != 3 {
		t.Fatal("删除部分条目后 LRU 链表没有正确维护")
	}
	cache.RemoveIf(func(int) bool { return true })
	cache.Add(7, "7", 3)
	if value, ok := cache.Get(7); !ok || value != "7" || cache.Len() != 1 {
		t.Fatal("清空后无法重新使用缓存")
	}
}

func BenchmarkShapingCacheWorkingSet(b *testing.B) {
	for _, reset := range []bool{true, false} {
		name := "LRU"
		if reset {
			name = "ResetAll"
		}
		b.Run(name, func(b *testing.B) {
			const capacity = 2048
			family := canvas.NewFontFamily("working-set")
			if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
				b.Fatal(err)
			}
			face := family.Face(12, canvas.Black)
			values := make([]string, capacity+1)
			for i := range values {
				values[i] = fmt.Sprintf("text-%d", i)
			}
			var lru shapingCache[string, float64]
			previous := make(map[string]float64)
			misses, requests := 0, 0
			get := func(value string) {
				requests++
				if reset {
					if _, ok := previous[value]; ok {
						return
					}
					if len(previous) == capacity {
						previous = make(map[string]float64)
					}
					previous[value] = face.TextWidth(value)
				} else {
					if _, ok := lru.Get(value); ok {
						return
					}
					lru.Add(value, face.TextWidth(value), capacity)
				}
				misses++
			}
			b.ReportAllocs()
			cold := 0
			for b.Loop() {
				// 保持一半热条目，另一半由持续进入的冷条目竞争。
				for i := 0; i < 1024; i++ {
					get(values[i])
				}
				for range 64 {
					get(values[1024+cold%1025])
					cold++
				}
			}
			b.ReportMetric(float64(misses)/float64(requests), "miss/request")
		})
	}
}
