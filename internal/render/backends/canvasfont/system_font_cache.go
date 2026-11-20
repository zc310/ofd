package canvasfont

import (
	"container/list"
	"sync"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

const (
	systemFontIdleCapacity = 16
	systemFontIdleBytes    = 384 << 20
	// 字体解析后的轮廓、整形表与原始数据都占用内存，不能只按文件字节计费。
	// 使用保守估算控制空闲缓存，而不是把该值当作精确的进程内存上限。
	systemFontWeightFactor = 6
)

// 系统字体表只由磁盘字体加载入口填充。内嵌字体、修复结果和内存注册的回退字体
// 各有独立的加载链，不进入这里。以下空闲队列与计费均由 fontCacheMu 保护。
var (
	idleSystemFonts     = list.New()
	idleSystemFontBytes int64
)

type systemFontCacheEntry struct {
	fontHolders

	family   *canvas.FontFamily
	renderMu *sync.Mutex
	weight   int64
	idle     *list.Element
}

func diskFontCacheWeight(sourceBytes int64) int64 {
	if sourceBytes <= 0 {
		// 无磁盘文件身份的加载（例如测试注入）不允许在关闭后留存。
		return 0
	}
	if sourceBytes > systemFontIdleBytes/systemFontWeightFactor {
		return systemFontIdleBytes + 1
	}
	return sourceBytes * systemFontWeightFactor
}

// removeIdleSystemFontLocked 将重新启用或待淘汰的字体移出空闲队列。
func removeIdleSystemFontLocked(entry *systemFontCacheEntry) {
	if entry.idle != nil {
		idleSystemFonts.Remove(entry.idle)
		idleSystemFontBytes -= entry.weight
		entry.idle = nil
	}
}

// cacheIdleSystemFontLocked 只留存已无文档持有的磁盘字体，空闲超限按 LRU 淘汰。
// 活跃字体不进入空闲队列，不能为了满足缓存预算而提前释放其共享渲染锁。
func cacheIdleSystemFontLocked(key systemFontKey, entry *systemFontCacheEntry) {
	if entry.weight <= 0 || entry.weight > systemFontIdleBytes {
		delete(systemFontCache, key)
		delete(fontRenderLocks, entry.family)
		return
	}
	entry.idle = idleSystemFonts.PushFront(key)
	idleSystemFontBytes += entry.weight
	for idleSystemFonts.Len() > systemFontIdleCapacity || idleSystemFontBytes > systemFontIdleBytes {
		oldest := idleSystemFonts.Back()
		oldKey := oldest.Value.(systemFontKey) //nolint:forcetypeassert // 私有空闲队列只保存上方写入的磁盘字体键。
		oldEntry := systemFontCache[oldKey]
		removeIdleSystemFontLocked(oldEntry)
		delete(systemFontCache, oldKey)
		delete(fontRenderLocks, oldEntry.family)
	}
}
