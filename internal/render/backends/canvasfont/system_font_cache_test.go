package canvasfont

import (
	"container/list"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/pkg/spec"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// 容量和计费测试单独保存/恢复全局表，不依赖其他测试留下的字体。
func emptyDiskFontCache(t *testing.T) {
	t.Helper()
	fontCacheMu.Lock()
	previousFonts, previousLocks := systemFontCache, fontRenderLocks
	previousIdle, previousBytes := idleSystemFonts, idleSystemFontBytes
	systemFontCache = make(map[systemFontKey]*systemFontCacheEntry)
	fontRenderLocks = make(map[*canvas.FontFamily]*sync.Mutex)
	idleSystemFonts, idleSystemFontBytes = list.New(), 0
	fontCacheMu.Unlock()
	t.Cleanup(func() {
		fontCacheMu.Lock()
		systemFontCache, fontRenderLocks = previousFonts, previousLocks
		idleSystemFonts, idleSystemFontBytes = previousIdle, previousBytes
		fontCacheMu.Unlock()
	})
}

func writeDiskCacheFont(tb testing.TB, data []byte) string {
	tb.Helper()
	file := filepath.Join(tb.TempDir(), "font.ttf")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		tb.Fatal(err)
	}
	return file
}

// 文档实例仍每次新建并关闭，只复用磁盘字体解析结果，不依赖常驻文档持有者。
func BenchmarkDiskFontReuseAfterClose(b *testing.B) {
	file := writeDiskCacheFont(b, goregular.TTF)
	first := NewFonts(nil)
	family, ok := loadCachedFontFile(first, file, "disk-reuse", FontRegular)
	first.Close()
	if !ok {
		b.Fatal("首次加载磁盘字体失败")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		fonts := NewFonts(nil)
		reused, ok := loadCachedFontFile(fonts, file, "disk-reuse", FontRegular)
		fonts.Close()
		if !ok || reused != family {
			b.Fatal("关闭后重新解析了磁盘字体")
		}
	}
}

func TestDiskFontReusesParsedFamilyAndRenderLockAfterDocumentClose(t *testing.T) {
	file := writeDiskCacheFont(t, goregular.TTF)
	first := NewFonts(nil)
	t.Cleanup(first.Close)
	family, ok := loadCachedFontFile(first, file, "first-disk-alias", FontRegular)
	if !ok {
		t.Fatal("加载磁盘字体失败")
	}
	lock := first.RenderLock(family)
	first.Close()
	if len(first.systemFonts) != 0 || len(first.renderLocks) != 0 {
		t.Fatal("关闭的文档仍持有字体或渲染锁")
	}

	second := NewFonts(nil)
	t.Cleanup(second.Close)
	reused, ok := loadCachedFontFile(second, file, "second-disk-alias", FontRegular)
	if !ok || reused != family || second.RenderLock(reused) != lock {
		t.Fatal("文档关闭后没有复用磁盘字体解析结果及同一把渲染锁")
	}
}

func TestDiskFontFileReplacementDoesNotReuseStaleParsedFamily(t *testing.T) {
	// 补齐到相同文件长度，确保失效判断真正覆盖 mtime，而不只是 size 变化。
	size := max(len(goregular.TTF), len(gobold.TTF))
	originalData, replacementData := make([]byte, size), make([]byte, size)
	copy(originalData, goregular.TTF)
	copy(replacementData, gobold.TTF)
	file := writeDiskCacheFont(t, originalData)
	first := NewFonts(nil)
	t.Cleanup(first.Close)
	original, ok := loadCachedFontFile(first, file, "replace-disk-font", FontRegular)
	if !ok {
		t.Fatal("加载原磁盘字体失败")
	}
	originalLock := first.RenderLock(original)
	first.Close()

	if err := os.WriteFile(file, replacementData, 0o600); err != nil {
		t.Fatal(err)
	}
	// 显式更新修改时间，避免依赖文件系统的时间戳分辨率。
	modified := time.Now().Add(time.Second)
	if err := os.Chtimes(file, modified, modified); err != nil {
		t.Fatal(err)
	}
	second := NewFonts(nil)
	t.Cleanup(second.Close)
	replacement, ok := loadCachedFontFile(second, file, "replace-disk-font", FontRegular)
	if !ok || replacement == original || second.RenderLock(replacement) == originalLock {
		t.Fatal("磁盘字体替换后仍使用旧字体或旧渲染锁")
	}
}

func TestDiskFontRelativeAndAbsolutePathsReuseSameFile(t *testing.T) {
	file := writeDiskCacheFont(t, goregular.TTF)
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(directory, file)
	if err != nil {
		t.Fatal(err)
	}
	first := NewFonts(nil)
	t.Cleanup(first.Close)
	family, ok := loadCachedFontFile(first, relative, t.Name(), FontRegular)
	if !ok {
		t.Fatal("无法按相对路径加载磁盘字体")
	}
	first.Close()
	second := NewFonts(nil)
	t.Cleanup(second.Close)
	reused, ok := loadCachedFontFile(second, file, t.Name(), FontRegular)
	if !ok || reused != family {
		t.Fatal("同一磁盘文件的相对/绝对路径产生了多个缓存条目")
	}
}

func TestDiskFontReuseDoesNotShareEmbeddedFontsWithSameBytesOrName(t *testing.T) {
	file := writeDiskCacheFont(t, goregular.TTF)
	disk := NewFonts(nil)
	t.Cleanup(disk.Close)
	diskFamily, ok := loadCachedFontFile(disk, file, "same-font-name", FontRegular)
	if !ok {
		t.Fatal("加载磁盘字体失败")
	}
	disk.Close()

	first, second := NewFonts(nil), NewFonts(nil)
	t.Cleanup(first.Close)
	t.Cleanup(second.Close)
	embeddedA, err := loadCachedEmbeddedFont(first, "same-font-name", goregular.TTF, FontRegular, nil)
	if err != nil {
		t.Fatal(err)
	}
	embeddedB, err := loadCachedEmbeddedFont(second, "same-font-name", goregular.TTF, FontRegular, nil)
	if err != nil {
		t.Fatal(err)
	}
	if embeddedA == diskFamily || embeddedB == diskFamily || embeddedA == embeddedB {
		t.Fatal("同字节或同名字体越过了磁盘/内嵌及文档作用域边界")
	}
	if len(first.systemFonts) != 0 || len(second.systemFonts) != 0 {
		t.Fatal("内嵌字体取得了全局磁盘字体缓存引用")
	}
	first.Close()
	second.Close()
	if len(first.embeddedFonts) != 0 || len(second.embeddedFonts) != 0 {
		t.Fatal("文档关闭后内嵌字体没有释放")
	}
	third := NewFonts(nil)
	t.Cleanup(third.Close)
	reused, ok := loadCachedFontFile(third, file, "same-font-name", FontRegular)
	if !ok || reused != diskFamily {
		t.Fatal("关闭内嵌字体文档影响了磁盘字体复用")
	}
}

func TestDiskFontReuseDoesNotShareRegisteredFallbackFontBytes(t *testing.T) {
	file := writeDiskCacheFont(t, goregular.TTF)
	fonts := NewFonts(nil)
	t.Cleanup(fonts.Close)
	diskFamily, ok := loadCachedFontFile(fonts, file, t.Name(), FontRegular)
	if !ok {
		t.Fatal("加载磁盘字体失败")
	}
	if err := registerFallbackFont(goregular.TTF, t.Name(), FontRegular); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := UnregisterFallbackFont(t.Name()); err != nil {
			t.Error(err)
		}
	})
	if err := fonts.UseFallbackFont(t.Name()); err != nil {
		t.Fatal(err)
	}
	if fonts.fallbacks[t.Name()] == diskFamily {
		t.Fatal("内存注册的回退字体与磁盘字体混用了解析结果")
	}
}

func TestDiskFontReuseKeepsStylesSeparate(t *testing.T) {
	file := writeDiskCacheFont(t, goregular.TTF)
	first := NewFonts(nil)
	t.Cleanup(first.Close)
	regular, ok := loadCachedFontFile(first, file, "disk-style", FontRegular)
	if !ok {
		t.Fatal("加载常规字体失败")
	}
	first.Close()
	second := NewFonts(nil)
	t.Cleanup(second.Close)
	bold, ok := loadCachedFontFile(second, file, "disk-style", FontBold)
	if !ok || bold == regular {
		t.Fatal("不同样式复用了同一字体族")
	}
	if face := bold.Face(12, canvas.Black, canvas.FontBold); face == nil {
		t.Fatal("磁盘字体样式不可用")
	}
}

func TestDiskFontIdleCacheEvictsLeastRecentlyReleasedFamilyAndLock(t *testing.T) {
	emptyDiskFontCache(t)
	load := func() (string, *canvas.FontFamily) {
		file := writeDiskCacheFont(t, goregular.TTF)
		fonts := NewFonts(nil)
		t.Cleanup(fonts.Close)
		family, ok := loadCachedFontFile(fonts, file, t.Name(), FontRegular)
		if !ok {
			t.Fatal("加载磁盘字体失败")
		}
		fonts.Close()
		return file, family
	}
	firstFile, first := load()
	secondFile, second := load()
	reuser := NewFonts(nil)
	t.Cleanup(reuser.Close)
	if family, ok := loadCachedFontFile(reuser, firstFile, t.Name(), FontRegular); !ok || family != first {
		t.Fatal("无法复用空闲字体")
	}
	reuser.Close()
	for range systemFontIdleCapacity - 1 {
		load()
	}
	firstKey, _ := systemFontKeyForFile(firstFile, FontRegular)
	secondKey, _ := systemFontKeyForFile(secondFile, FontRegular)
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if idleSystemFonts.Len() != systemFontIdleCapacity || systemFontCache[firstKey] == nil {
		t.Fatal("容量或最近复用字体的保留顺序不正确")
	}
	if systemFontCache[secondKey] != nil || fontRenderLocks[second] != nil {
		t.Fatal("最久空闲字体或其共享锁未随容量淘汰")
	}
}

func TestDiskFontIdleCacheHonorsWeightAndDoesNotRetainOversizedFont(t *testing.T) {
	emptyDiskFontCache(t)
	files := []string{writeDiskCacheFont(t, goregular.TTF), writeDiskCacheFont(t, gobold.TTF), writeDiskCacheFont(t, goregular.TTF)}
	families := make([]*canvas.FontFamily, len(files))
	keys := make([]systemFontKey, len(files))
	for index, file := range files {
		fonts := NewFonts(nil)
		t.Cleanup(fonts.Close)
		var ok bool
		families[index], ok = loadCachedFontFile(fonts, file, t.Name(), FontRegular)
		if !ok {
			t.Fatal("加载磁盘字体失败")
		}
		keys[index], _ = systemFontKeyForFile(file, FontRegular)
		// 使用小字体模拟较大解析权重，避免为了测试预算实际分配数百 MiB。
		weight := int64(systemFontIdleBytes/2 + 1)
		if index == 2 {
			weight = systemFontIdleBytes + 1
		}
		fontCacheMu.Lock()
		systemFontCache[keys[index]].weight = weight
		fontCacheMu.Unlock()
		fonts.Close()
	}
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if systemFontCache[keys[0]] != nil || fontRenderLocks[families[0]] != nil {
		t.Fatal("累计权重超限后未淘汰最早的空闲字体")
	}
	if systemFontCache[keys[1]] == nil || idleSystemFonts.Len() != 1 || idleSystemFontBytes != systemFontIdleBytes/2+1 {
		t.Fatal("空闲计费或新字体保留结果不正确")
	}
	if systemFontCache[keys[2]] != nil || fontRenderLocks[families[2]] != nil {
		t.Fatal("单项权重超限的字体在关闭后仍被缓存")
	}
	if got := diskFontCacheWeight(1 << 62); got <= systemFontIdleBytes {
		t.Fatal("大文件权重溢出后绕过了空闲缓存限制")
	}
}

func TestDiskFontIdleEvictionNeverRemovesActiveFamilyOrLock(t *testing.T) {
	emptyDiskFontCache(t)
	activeFile := writeDiskCacheFont(t, goregular.TTF)
	active := NewFonts(nil)
	t.Cleanup(active.Close)
	family, ok := loadCachedFontFile(active, activeFile, t.Name(), FontRegular)
	if !ok {
		t.Fatal("加载活跃磁盘字体失败")
	}
	lock := active.RenderLock(family)
	for range systemFontIdleCapacity + 1 {
		other := NewFonts(nil)
		file := writeDiskCacheFont(t, goregular.TTF)
		if _, ok := loadCachedFontFile(other, file, t.Name(), FontRegular); !ok {
			other.Close()
			t.Fatal("加载用于淘汰的磁盘字体失败")
		}
		other.Close()
	}
	key, _ := systemFontKeyForFile(activeFile, FontRegular)
	fontCacheMu.Lock()
	entry := systemFontCache[key]
	retainedLock := fontRenderLocks[family]
	fontCacheMu.Unlock()
	if entry == nil || entry.idle != nil || retainedLock != lock {
		t.Fatal("空闲淘汰删除了仍被文档持有的字体或改变了共享锁")
	}
	other := NewFonts(nil)
	t.Cleanup(other.Close)
	reused, ok := loadCachedFontFile(other, activeFile, t.Name(), FontRegular)
	if !ok || reused != family || other.RenderLock(reused) != lock {
		t.Fatal("活跃字体在缓存压力下被重复解析")
	}
}

func TestDiskFontIdleCacheConcurrentReuseKeepsOneFamilyAndLock(t *testing.T) {
	emptyDiskFontCache(t)
	file := writeDiskCacheFont(t, goregular.TTF)
	first := NewFonts(nil)
	family, ok := loadCachedFontFile(first, file, t.Name(), FontRegular)
	if !ok {
		first.Close()
		t.Fatal("首次加载失败")
	}
	lock := first.RenderLock(family)
	first.Close()
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			<-start
			for range 8 {
				fonts := NewFonts(nil)
				shared, ok := loadCachedFontFile(fonts, file, t.Name(), FontRegular)
				if !ok || shared != family || fonts.RenderLock(shared) != lock {
					t.Error("并发关闭/复用时字体身份或共享锁发生变化")
				}
				fonts.Close()
			}
		})
	}
	close(start)
	workers.Wait()
}

func TestDiskFontIdleCacheDoesNotServeDeletedFile(t *testing.T) {
	file := writeDiskCacheFont(t, goregular.TTF)
	first := NewFonts(nil)
	t.Cleanup(first.Close)
	if _, ok := loadCachedFontFile(first, file, t.Name(), FontRegular); !ok {
		t.Fatal("加载磁盘字体失败")
	}
	first.Close()
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	second := NewFonts(nil)
	t.Cleanup(second.Close)
	if _, ok := loadCachedFontFile(second, file, t.Name(), FontRegular); ok {
		t.Fatal("磁盘文件已删除仍返回空闲缓存中的字体")
	}
}

func TestOFDDeclaredEmbeddedFontsNeverPopulateDiskFontIdleCache(t *testing.T) {
	emptyDiskFontCache(t)
	fonts, _ := sourceCacheFonts(t)
	fonts.ForEachFont(func(id spec.StID, _ *spec.Font) bool {
		if _, err := fonts.LoadFont(spec.StRefID(id)); err != nil {
			t.Error(err)
		}
		return true
	})
	fonts.Close()
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if len(systemFontCache) != 0 || len(fontRenderLocks) != 0 || idleSystemFonts.Len() != 0 || idleSystemFontBytes != 0 {
		t.Fatal("OFD 内嵌字体或修复结果进入了全局磁盘字体缓存")
	}
}
