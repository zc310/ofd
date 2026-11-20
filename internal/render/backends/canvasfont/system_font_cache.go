package canvasfont

import (
	"container/list"
	"crypto/sha256"
	"errors"
	"log/slog"
	"os"
	"sync"

	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

const (
	systemFontIdleCapacity = 16
	systemFontIdleBytes    = 384 << 20
	// 路径只是内容索引的快捷入口，必须另设上限，避免同一字体的大量副本撑大元数据。
	systemFontPathCapacity = 512
	// 字体解析后的轮廓、整形表与原始数据都占用内存，不能只按文件字节计费。
	// 使用保守估算控制空闲缓存，而不是把该值当作精确的进程内存上限。
	systemFontWeightFactor = 6
)

// 系统字体表只由磁盘字体加载入口填充。内嵌字体、修复结果和内存注册的回退字体
// 各有独立的加载链，不进入这里。以下空闲队列与计费均由 fontCacheMu 保护。
var (
	idleSystemFonts     = list.New()
	idleSystemFontBytes int64
	systemFontPaths     = list.New()
	systemFontContents  = make(map[diskFontContentKey]*systemFontCacheEntry)
)

// 内容去重仅适用于磁盘字体；字体面和样式都参与身份，不与文档内嵌资源共用索引。
type diskFontContentKey struct {
	digest [sha256.Size]byte
	face   int
	style  drawing.FontStyle
}

type systemFontSource struct {
	content *diskFontContentKey
	load    func(*canvas.FontFamily) error
}

type systemFontCacheEntry struct {
	fontHolders

	family   *canvas.FontFamily
	renderMu *sync.Mutex
	weight   int64
	idle     *list.Element
	content  *diskFontContentKey
	paths    map[systemFontKey]*list.Element
}

var errDiskFontChanged = errors.New("磁盘字体在读取期间变化")

func loadCachedDiskFont(p *Fonts, key systemFontKey, name string) (*canvas.FontFamily, bool) {
	return loadCachedFontSource(p, key, name, func() (*systemFontSource, error) {
		return readDiskFontSource(key, name)
	})
}

// readDiskFontSource 只在路径身份未命中时读盘、计算摘要；解析也使用同一份字节，
// 不能在计算摘要后再按路径重读，否则文件替换可能使缓存身份与实际字体不一致。
func readDiskFontSource(key systemFontKey, name string) (*systemFontSource, error) {
	data, err := os.ReadFile(key.path)
	if err != nil {
		return nil, err
	}
	current, ok := systemFontKeyForFile(key.path, key.style)
	current.face = key.face
	if !ok || current != key || int64(len(data)) != key.size {
		return nil, errDiskFontChanged
	}
	return &systemFontSource{
		content: &diskFontContentKey{digest: sha256.Sum256(data), face: key.face, style: key.style},
		load: func(family *canvas.FontFamily) error {
			slog.Debug("load font file", "family", name, "file", key.path, "style", key.style)
			return loadExternalFont(family, data, key.face, key.style)
		},
	}, nil
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

// rememberSystemFontPathLocked 保存有界的路径快捷索引。移除路径不会释放字体：
// 活跃持有引用与按内容计费的空闲条目均独立于路径数量。
func rememberSystemFontPathLocked(key systemFontKey, entry *systemFontCacheEntry) {
	if element := entry.paths[key]; element != nil {
		systemFontPaths.MoveToFront(element)
		return
	}
	if previous := systemFontCache[key]; previous != nil {
		forgetSystemFontPathLocked(key, previous)
	}
	if entry.paths == nil {
		entry.paths = make(map[systemFontKey]*list.Element)
	}
	entry.paths[key] = systemFontPaths.PushFront(key)
	systemFontCache[key] = entry
	for systemFontPaths.Len() > systemFontPathCapacity {
		oldest := systemFontPaths.Back()
		oldKey := oldest.Value.(systemFontKey) //nolint:forcetypeassert // 私有路径队列只保存上方写入的字体键。
		forgetSystemFontPathLocked(oldKey, systemFontCache[oldKey])
	}
}

func forgetSystemFontPathLocked(key systemFontKey, entry *systemFontCacheEntry) {
	if element := entry.paths[key]; element != nil {
		systemFontPaths.Remove(element)
		delete(entry.paths, key)
	}
	delete(systemFontCache, key)
}

// evictSystemFontLocked 一并删除内容索引、全部路径别名及共享锁，不留旁路强引用。
func evictSystemFontLocked(entry *systemFontCacheEntry) {
	removeIdleSystemFontLocked(entry)
	for key := range entry.paths {
		forgetSystemFontPathLocked(key, entry)
	}
	if entry.content != nil {
		delete(systemFontContents, *entry.content)
	}
	delete(fontRenderLocks, entry.family)
}

// cacheIdleSystemFontLocked 只留存已无文档持有的磁盘字体，空闲超限按 LRU 淘汰。
// 活跃字体不进入空闲队列，不能为了满足缓存预算而提前释放其共享渲染锁。
func cacheIdleSystemFontLocked(entry *systemFontCacheEntry) {
	if entry.weight <= 0 || entry.weight > systemFontIdleBytes {
		evictSystemFontLocked(entry)
		return
	}
	entry.idle = idleSystemFonts.PushFront(entry)
	idleSystemFontBytes += entry.weight
	for idleSystemFonts.Len() > systemFontIdleCapacity || idleSystemFontBytes > systemFontIdleBytes {
		oldest := idleSystemFonts.Back()
		oldEntry := oldest.Value.(*systemFontCacheEntry) //nolint:forcetypeassert // 私有空闲队列只保存上方写入的字体条目。
		evictSystemFontLocked(oldEntry)
	}
}
