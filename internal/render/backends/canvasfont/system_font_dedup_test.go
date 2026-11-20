package canvasfont

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
	cfont "github.com/zc310/ofd/internal/thirdparty/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// 在有效字体尾部加入不同标记，使容量测试的内容确实不同，而不改变字体轮廓。
func uniqueDiskFontData(marker int) []byte {
	data := make([]byte, len(goregular.TTF)+8)
	copy(data, goregular.TTF)
	binary.BigEndian.PutUint64(data[len(goregular.TTF):], uint64(marker))
	return data
}

func TestDiskFontIdenticalCopiesShareFamilyLockAndIdleWeight(t *testing.T) {
	for _, mode := range []string{"same-document", "two-documents", "after-close"} {
		t.Run(mode, func(t *testing.T) {
			emptyDiskFontCache(t)
			firstFile, secondFile := writeDiskCacheFont(t, goregular.TTF), writeDiskCacheFont(t, goregular.TTF)
			first, second := NewFonts(nil), NewFonts(nil)
			if mode == "same-document" {
				second = first
			}
			t.Cleanup(first.Close)
			t.Cleanup(second.Close)
			family, ok := loadCachedFontFile(first, firstFile, "first-alias", FontRegular)
			if !ok {
				t.Fatal("加载第一份磁盘字体失败")
			}
			lock := first.RenderLock(family)
			if mode == "after-close" {
				first.Close()
			}
			shared, ok := loadCachedFontFile(second, secondFile, "second-alias", FontRegular)
			if !ok || shared != family || second.RenderLock(shared) != lock {
				t.Fatal("不同路径的相同字体没有共享解析结果与渲染锁")
			}
			if len(second.systemFonts) != 1 {
				t.Fatal("同一文档按多个路径重复登记了相同字体的持有引用")
			}
			first.Close()
			if first != second {
				key, _ := systemFontKeyForFile(secondFile, FontRegular)
				fontCacheMu.Lock()
				entry := systemFontCache[key]
				if entry == nil || entry.idle != nil || len(entry.holders) != 1 {
					fontCacheMu.Unlock()
					t.Fatal("关闭另一文档提前释放或空闲化了仍在使用的重复字体")
				}
				fontCacheMu.Unlock()
			}
			second.Close()
			fontCacheMu.Lock()
			defer fontCacheMu.Unlock()
			if idleSystemFonts.Len() != 1 || idleSystemFontBytes != diskFontCacheWeight(int64(len(goregular.TTF))) {
				t.Fatal("相同内容的多个路径重复占用了空闲条目或权重")
			}
		})
	}
}

func TestDiskFontSameNameSizeAndTimestampDoNotMergeDifferentContents(t *testing.T) {
	emptyDiskFontCache(t)
	size := max(len(goregular.TTF), len(gobold.TTF))
	regular, bold := make([]byte, size), make([]byte, size)
	copy(regular, goregular.TTF)
	copy(bold, gobold.TTF)
	files := []string{writeDiskCacheFont(t, regular), writeDiskCacheFont(t, bold)}
	families := make([]*canvas.FontFamily, 0, len(files))
	modified := time.Unix(1_700_000_000, 0)
	for _, file := range files {
		if err := os.Chtimes(file, modified, modified); err != nil {
			t.Fatal(err)
		}
		fonts := NewFonts(nil)
		t.Cleanup(fonts.Close)
		family, ok := loadCachedFontFile(fonts, file, "same-name", FontRegular)
		if !ok {
			t.Fatal("加载磁盘字体失败")
		}
		families = append(families, family)
		fonts.Close()
	}
	if families[0] == families[1] {
		t.Fatal("仅凭名称、大小或时间戳合并了不同字体内容")
	}
}

func TestDiskFontIdenticalCopiesKeepStylesSeparate(t *testing.T) {
	emptyDiskFontCache(t)
	first, second := NewFonts(nil), NewFonts(nil)
	t.Cleanup(first.Close)
	t.Cleanup(second.Close)
	regular, ok := loadCachedFontFile(first, writeDiskCacheFont(t, goregular.TTF), "copy", FontRegular)
	if !ok {
		t.Fatal("加载常规字体失败")
	}
	bold, ok := loadCachedFontFile(second, writeDiskCacheFont(t, goregular.TTF), "copy", FontBold)
	if !ok || bold == regular {
		t.Fatal("相同内容但不同样式的磁盘字体被合并")
	}
}

func TestDiskFontConcurrentCopiesShareOneParsedFamily(t *testing.T) {
	emptyDiskFontCache(t)
	const count = 16
	files := make([]string, count)
	families := make([]*canvas.FontFamily, count)
	locks := make([]*sync.Mutex, count)
	for index := range files {
		files[index] = writeDiskCacheFont(t, goregular.TTF)
	}
	start := make(chan struct{})
	var workers sync.WaitGroup
	var parses atomic.Int32
	for index := range files {
		workers.Go(func() {
			<-start
			fonts := NewFonts(nil)
			defer fonts.Close()
			key, ok := systemFontKeyForFile(files[index], FontRegular)
			if !ok {
				t.Error("无法读取磁盘字体副本身份")
				return
			}
			families[index], ok = loadCachedFontSource(fonts, key, "concurrent-copy", func() (*systemFontSource, error) {
				source, err := readDiskFontSource(key, "concurrent-copy")
				if err != nil {
					return nil, err
				}
				load := source.load
				source.load = func(family *canvas.FontFamily) error {
					parses.Add(1)
					return load(family)
				}
				return source, nil
			})
			if !ok {
				t.Error("并发加载磁盘字体副本失败")
				return
			}
			locks[index] = fonts.RenderLock(families[index])
		})
	}
	close(start)
	workers.Wait()
	if parses.Load() != 1 {
		t.Fatalf("相同内容的副本重复解析了%d次", parses.Load())
	}
	for index := range families {
		if families[index] == nil || families[index] != families[0] || locks[index] != locks[0] {
			t.Fatal("相同内容并发首次加载产生了多份解析结果或渲染锁")
		}
	}
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if idleSystemFonts.Len() != 1 {
		t.Fatal("并发关闭后重复登记了相同内容的空闲字体")
	}
}

func TestDiskFontEvictionDropsAllCopyPathsAndContentReuse(t *testing.T) {
	emptyDiskFontCache(t)
	firstFile, secondFile := writeDiskCacheFont(t, goregular.TTF), writeDiskCacheFont(t, goregular.TTF)
	first, second := NewFonts(nil), NewFonts(nil)
	t.Cleanup(first.Close)
	t.Cleanup(second.Close)
	family, ok := loadCachedFontFile(first, firstFile, "evict-copy", FontRegular)
	if !ok {
		t.Fatal("加载第一份字体失败")
	}
	shared, ok := loadCachedFontFile(second, secondFile, "evict-copy", FontRegular)
	if !ok || shared != family {
		t.Fatal("两份副本没有共享字体")
	}
	first.Close()
	pressure := func(offset int) {
		for index := range systemFontIdleCapacity {
			fonts := NewFonts(nil)
			file := writeDiskCacheFont(t, uniqueDiskFontData(offset+index))
			if _, ok := loadCachedFontFile(fonts, file, "pressure", FontRegular); !ok {
				fonts.Close()
				t.Fatal("加载容量测试字体失败")
			}
			fonts.Close()
		}
	}
	pressure(1)
	keyA, _ := systemFontKeyForFile(firstFile, FontRegular)
	keyB, _ := systemFontKeyForFile(secondFile, FontRegular)
	fontCacheMu.Lock()
	retained := systemFontCache[keyA] != nil && systemFontCache[keyB] != nil && fontRenderLocks[family] != nil
	fontCacheMu.Unlock()
	if !retained {
		t.Fatal("尚有文档持有时淘汰了副本共享的字体")
	}
	second.Close()
	pressure(100)
	fontCacheMu.Lock()
	evicted := systemFontCache[keyA] == nil && systemFontCache[keyB] == nil && fontRenderLocks[family] == nil
	fontCacheMu.Unlock()
	if !evicted {
		t.Fatal("淘汰内容后仍残留副本路径索引或共享锁")
	}
	reopened := NewFonts(nil)
	t.Cleanup(reopened.Close)
	reloaded, ok := loadCachedFontFile(reopened, secondFile, "evict-copy", FontRegular)
	if !ok || reloaded == family {
		t.Fatal("内容已淘汰后仍复用了本应释放的解析结果")
	}
}

func TestDiskFontCopyReplacementDoesNotInvalidateUnchangedCopy(t *testing.T) {
	emptyDiskFontCache(t)
	firstFile, secondFile := writeDiskCacheFont(t, goregular.TTF), writeDiskCacheFont(t, goregular.TTF)
	first, second, observer := NewFonts(nil), NewFonts(nil), NewFonts(nil)
	t.Cleanup(first.Close)
	t.Cleanup(second.Close)
	t.Cleanup(observer.Close)
	original, ok := loadCachedFontFile(first, firstFile, "copy-update", FontRegular)
	if !ok {
		t.Fatal("加载原字体失败")
	}
	shared, ok := loadCachedFontFile(first, secondFile, "copy-update", FontRegular)
	if !ok || shared != original {
		t.Fatal("更新前的两份副本未共享")
	}
	lock := first.RenderLock(original)
	if err := os.WriteFile(secondFile, gobold.TTF, 0o600); err != nil {
		t.Fatal(err)
	}
	modified := time.Now().Add(time.Second)
	if err := os.Chtimes(secondFile, modified, modified); err != nil {
		t.Fatal(err)
	}
	changed, ok := loadCachedFontFile(second, secondFile, "copy-update", FontRegular)
	if !ok || changed == original {
		t.Fatal("副本替换后仍命中旧内容")
	}
	first.Close()
	unchanged, ok := loadCachedFontFile(observer, firstFile, "copy-update", FontRegular)
	if !ok || unchanged != original || observer.RenderLock(unchanged) != lock {
		t.Fatal("替换一个副本影响了未变副本的字体身份或共享锁")
	}
}

func TestDiskFontCopyPathIndexIsBoundedWithoutDroppingOwnership(t *testing.T) {
	emptyDiskFontCache(t)
	file := writeDiskCacheFont(t, goregular.TTF)
	fonts := NewFonts(nil)
	t.Cleanup(fonts.Close)
	family, ok := loadCachedFontFile(fonts, file, "bounded-paths", FontRegular)
	if !ok {
		t.Fatal("加载磁盘字体失败")
	}
	key, _ := systemFontKeyForFile(file, FontRegular)
	fontCacheMu.Lock()
	entry := systemFontCache[key]
	// 直接添加别名元数据，避免为测试路径索引上限复制数百份字体文件。
	for index := range systemFontPathCapacity + 1 {
		alias := key
		alias.path = fmt.Sprintf("test-alias-%d", index)
		rememberSystemFontPathLocked(alias, entry)
	}
	bounded := len(systemFontCache) == systemFontPathCapacity &&
		len(entry.paths) == systemFontPathCapacity && systemFontPaths.Len() == systemFontPathCapacity
	missingPath := systemFontCache[key] == nil
	fontCacheMu.Unlock()
	if !bounded || !missingPath {
		t.Fatal("路径快捷索引没有按上限淘汰最早的别名")
	}
	fonts.Close()
	reopened := NewFonts(nil)
	t.Cleanup(reopened.Close)
	reused, ok := loadCachedFontFile(reopened, file, "bounded-paths", FontRegular)
	if !ok || reused != family {
		t.Fatal("路径索引淘汰后丢失了按内容保留的空闲字体")
	}
	reopened.Close()
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if idleSystemFonts.Len() != 1 || len(systemFontContents) != 1 || idleSystemFontBytes != entry.weight {
		t.Fatal("大量副本路径使持有引用或空闲内容重复计费")
	}
}

func TestDiskFontCloseDuringCopyReadDoesNotPublishAliasOrRetainContent(t *testing.T) {
	emptyDiskFontCache(t)
	firstFile, secondFile := writeDiskCacheFont(t, goregular.TTF), writeDiskCacheFont(t, goregular.TTF)
	first := NewFonts(nil)
	t.Cleanup(first.Close)
	if _, ok := loadCachedFontFile(first, firstFile, "read-close", FontRegular); !ok {
		t.Fatal("加载原字体失败")
	}
	first.Close()
	key, ok := systemFontKeyForFile(secondFile, FontRegular)
	if !ok {
		t.Fatal("无法读取副本文件身份")
	}
	closing := NewFonts(nil)
	t.Cleanup(closing.Close)
	entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	go func() {
		defer close(done)
		if _, ok := loadCachedFontSource(closing, key, "read-close", func() (*systemFontSource, error) {
			close(entered)
			<-resume
			return readDiskFontSource(key, "read-close")
		}); ok {
			t.Error("关闭后仍复用了副本的字体")
		}
	}()
	defer func() { release(); <-done }()
	awaitFontSignal(t, entered)
	closing.Close()
	release()
	awaitFontSignal(t, done)
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if systemFontCache[key] != nil || len(closing.systemFonts) != 0 || len(closing.renderLocks) != 0 || idleSystemFonts.Len() != 1 {
		t.Fatal("关闭后的读取重新登记了副本、字体持有者或渲染锁")
	}
}

func TestDiskFontSourceRejectsChangedFileIdentityBeforePublishing(t *testing.T) {
	emptyDiskFontCache(t)
	file := writeDiskCacheFont(t, goregular.TTF)
	key, ok := systemFontKeyForFile(file, FontRegular)
	if !ok {
		t.Fatal("无法读取原文件身份")
	}
	if err := os.WriteFile(file, gobold.TTF, 0o600); err != nil {
		t.Fatal(err)
	}
	fonts := NewFonts(nil)
	t.Cleanup(fonts.Close)
	if _, ok := loadCachedDiskFont(fonts, key, "changed-read"); ok {
		t.Fatal("读盘后没有重新验证字体文件身份")
	}
	if len(systemFontCache) != 0 || len(systemFontContents) != 0 || len(fonts.systemFonts) != 0 {
		t.Fatal("变化中的文件污染了路径或内容缓存")
	}
	if _, ok := loadCachedFontFile(fonts, file, "changed-read", FontRegular); !ok {
		t.Fatal("文件稳定后无法重新加载")
	}
}

func TestDiskFontIdenticalCollectionsKeepFaceIndicesSeparate(t *testing.T) {
	emptyDiskFontCache(t)
	// TTC 的表偏移相对于整个集合；构造多面集合，分别读取前两面的完整字体。
	faces := [][]byte{goregular.TTF, gobold.TTF, goregular.TTF}
	offset := 12 + 4*len(faces)
	offsets := make([]int, 0, len(faces))
	for _, face := range faces {
		offsets = append(offsets, offset)
		offset = (offset + len(face) + 3) &^ 3
	}
	data := make([]byte, offset)
	copy(data, "ttcf")
	binary.BigEndian.PutUint32(data[4:], 0x00010000)
	binary.BigEndian.PutUint32(data[8:], uint32(len(faces)))
	for index, face := range faces {
		base := offsets[index]
		binary.BigEndian.PutUint32(data[12+4*index:], uint32(base))
		copy(data[base:], face)
		for table := range int(binary.BigEndian.Uint16(face[4:])) {
			record := base + 12 + 16*table
			binary.BigEndian.PutUint32(data[record+8:], binary.BigEndian.Uint32(data[record+8:])+uint32(base))
		}
	}
	files := []string{writeDiskCacheFont(t, data), writeDiskCacheFont(t, data)}
	fonts := NewFonts(nil)
	t.Cleanup(fonts.Close)
	first, ok := loadCachedFontFile(fonts, files[0], "collection", FontRegular)
	if !ok {
		t.Fatal("加载集合第一个字体面失败")
	}
	copyFace, ok := loadCachedFontFile(fonts, files[1], "collection", FontRegular)
	if !ok || copyFace != first {
		t.Fatal("集合副本的同一字体面没有共享")
	}
	key, ok := systemFontKeyForFile(files[1], FontRegular)
	if !ok {
		t.Fatal("无法读取集合文件身份")
	}
	key.face = 1
	second, ok := loadCachedDiskFont(fonts, key, "collection")
	if !ok || second == first {
		t.Fatal("相同集合的不同字体面被内容去重合并")
	}
	firstFace, secondFace := first.Face(12, canvas.Black), second.Face(12, canvas.Black)
	regularSource, err := cfont.ParseSFNT(goregular.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	boldSource, err := cfont.ParseSFNT(gobold.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstFace.Font.Tables["glyf"], regularSource.Tables["glyf"]) ||
		!bytes.Equal(secondFace.Font.Tables["glyf"], boldSource.Tables["glyf"]) {
		t.Fatal("缓存身份虽区分了字体面，但实际加载的轮廓表不对应所选面")
	}
	key.face = 99
	if _, ok := loadCachedDiskFont(fonts, key, "collection"); ok {
		t.Fatal("无效字体面通过内容缓存绕过了解析验证")
	}
}
