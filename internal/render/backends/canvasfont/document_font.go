package canvasfont

import (
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"image/color"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
	"weak"

	"github.com/zc310/canvas"
	"github.com/zc310/canvas/text"
	"github.com/zc310/font"
	"github.com/zc310/ofd/internal/cache"
	"github.com/zc310/ofd/internal/fileutil"
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/drawing"
)

// 本文件实现 canvas 字体引擎 Fonts：字体资源解析、内嵌字体加载、字形映射
// 登记、回退字体全局注册表与串行化锁；系统字体候选匹配见 font_candidates.go，
// 字体面与文字样式适配见 font_face.go。

var (
	onceFonts   sync.Once
	fontCacheMu sync.Mutex
	// 系统字体冷加载、回退字体首次注册各自串行化；等待、读盘和解析不占用缓存锁。
	systemFontLoadMu   sync.Mutex
	fallbackFontLoadMu sync.Mutex
	systemFontCache    = make(map[systemFontKey]*systemFontCacheEntry)
	// fontRenderLocks 串行化系统字体族的绘制。内嵌字体族属于单个文档，其渲染锁
	// 由 Fonts.renderLocks 按实例持有，不进这张全局表——表键是指针，留在全局表
	// 会一直强引用字体族，文档关闭后也无法回收。
	fontRenderLocks = make(map[*canvas.FontFamily]*sync.Mutex)
	// 全局回退字体注册表：同一字体族共用解析结果，每种样式只保存当前数据引用；
	// 相同来源重复注册幂等，后续文档可直接复用已注册字体。
	fallbackRegistry = make(map[string]*fallbackRegistration)
)

// fallbackRegistration 保存某个字体族全局唯一定位后的解析家族与来源引用。
type fallbackRegistration struct {
	family   *canvas.FontFamily
	sources  []fallbackFontSource
	ready    bool
	renderMu *sync.Mutex
	// refs 是当前仍持有该族字形引用的 Fonts 实例数。
	//
	// 注册表持有字体字节的唯一理由是"还有文档要用它"。没有这个计数就无法判断
	// 能否释放，而提前释放会让并发的两份渲染拿到同一族名的两个 FontFamily——
	// renderMu 也随之变成两把锁，等宽度量与绘制就可能错开。
	refs int
	// dropped 表示调用方已声明不再需要这个族。若此刻仍有人持有（refs>0），
	// 真正删除要等最后一个持有者放手，见 releaseFallbackFamily。
	dropped bool
}

// 空字符串只用于 fallbackRegistry 中的进程级默认字体；公开回退字体族名禁止为空。
const defaultFallbackKey = ""

// systemFontKey 以字体**文件**而非字体名标识一次系统字体加载。
//
// 此前用 (name, style) 作键，但 name 是逻辑名、文件是系统匹配的结果，两者不是
// 一一对应：多个逻辑名（文档里声明的族名、回退候选表里的名字）会解析到同一个
// 字体文件。实测渲染一个 5 页的中文文档时 simkai.ttf 被加载两次，各持一份
// 11.8 MB 的字体数据与解析结果——键不同，资源却是同一份。
//
// 改用文件路径后这类重复自然合并。带上 size 与 mtime 是为了区分"同一路径上被
// 换掉的字体"（测试环境换字体、或用户升级字体包）：那种情况下旧的解析结果必须
// 失效，否则会继续用旧字形渲染。
type systemFontKey struct {
	path  string
	size  int64
	mtime int64
	style drawing.FontStyle
}

// fontHolders 登记正在使用某个共享字体族的字体引擎实例，用于引用计数淘汰。
//
// 字体族本身很重：一个 CJK 字体约 50 MB（文件字节只占五分之一，其余是解码
// 后的字形轮廓）。此前缓存无上限也无淘汰，而每个 render.Document 各自持有
// 指针，长会话里浏览多个文档会持续累积。有了引用计数才能安全淘汰：只要还有
// 文档持有该族就不能动它——这同时也保证了 fontRenderLocks 不会在文档仍在绘制
// 时被删掉，导致同一字体拿到两把锁而被并发渲染。降到 0 时才连同渲染锁释放。
type fontHolders struct {
	holders map[*Fonts]struct{}
}

// retain 登记持有者。
func (h *fontHolders) retain(f *Fonts) {
	if h.holders == nil {
		h.holders = make(map[*Fonts]struct{}, 1)
	}
	h.holders[f] = struct{}{}
}

// release 解除一个持有者，返回是否已无人再持有。
func (h *fontHolders) release(f *Fonts) bool {
	delete(h.holders, f)
	return len(h.holders) == 0
}

type systemFontCacheEntry struct {
	fontHolders

	family   *canvas.FontFamily
	renderMu *sync.Mutex
}

// embeddedFontKey 标识一次内嵌字体解析结果。相同字体数据、样式、族名和字形
// 映射只需解析一次；字形映射来自 map，摘要按排序后的内容计算以保证顺序无关。
type embeddedFontKey struct {
	name           string
	style          drawing.FontStyle
	dataDigest     [sha256.Size]byte
	mappingsDigest [sha256.Size]byte
}

// Fonts embeddedFonts/embeddedRepairers 按文档（Fonts 实例）持有内嵌字体解析结果，
// 随文档关闭一并释放。内嵌字体是文档自带资源，跨文档复用价值有限，却会因全局
// 缓存长期钉住重资源，因此不放在包级缓存里。
type Fonts struct {
	*parser.Document

	Fonts            map[models.StRefID]*canvas.FontFamily
	fallbacks        map[string]*canvas.FontFamily
	fallbackFaces    []fallbackFace
	fallbackByFont   map[models.StRefID]string
	glyphMappings    map[models.StRefID]map[rune]uint16
	glyphCollections *cache.LRU[*models.Content, glyphCollection]
	mu               sync.Mutex
	loadLocksMu      sync.Mutex
	loadLocks        map[models.StRefID]*sync.Mutex
	generation       uint64
	// 仅在 fontCacheMu 下访问，独立于会因字形/回退更新而重建的 Fonts 表。
	systemFonts     map[systemFontKey]*systemFontCacheEntry
	glyphVersions   map[models.StRefID]uint64
	fallbackVersion uint64
	renderLocksMu   sync.Mutex
	renderLocks     map[weak.Pointer[canvas.FontFamily]]*sync.Mutex
	embeddedMu      sync.Mutex
	embeddedFonts   map[embeddedFontKey]*canvas.FontFamily
	embeddedSources *cache.LRU[string, *embeddedFontSource]
	glyphSnapshots  *cache.LRU[models.StRefID, *fontGlyphSnapshot] // 由 mu 保护
	// embeddedRepairers 按字体字节摘要缓存 fontrepair.Repairer。同一份内嵌字体在
	// 文档逐页登记字形映射时会被反复修复，而修复结果只取决于字体字节；Repairer
	// 缓存修复后的基础字体与已解析 cmap，使每次修补只需重新合并与输出 cmap 表。
	// WithGlyphs 不改写缓存状态，可安全并发调用。
	embeddedRepairers map[[sha256.Size]byte]*fontrepair.Repairer
	pathCacheMu       sync.Mutex
	pathCache         shapingCache[fontPathKey, *canvas.Path]
	// closed 在关闭开始时置位。各缓存持有自己的锁时也可检查，
	// 避免为了拒绝关闭后的发布而反向获取实例锁。
	closed         atomic.Bool
	textLineMu     sync.Mutex
	textLineCache  shapingCache[textLineKey, *canvas.Text]
	textWidthMu    sync.Mutex
	textWidthCache shapingCache[textWidthKey, float64]
	// 弱键只记录退休状态，不反向持有旧字体。仅缓存未命中后的发布路径查询它。
	retiredFonts sync.Map // weak.Pointer[canvas.Font] -> struct{}
	retirements  atomic.Uint64
}

// fontPathKey 标识一次确定的字形轮廓计算结果：轮廓几何只由字体数据、
// 字号、样式、方向、脚本、语言及实际几何参数决定。ToPath 不使用 Fill/Deco，
// 装饰在绘制时另行添加，因此纯色和渐变 run 可复用同一轮廓。字形轮廓拆解（必要时含
// harfbuzz 整形）是渲染热点，缓存后相同字符串只计算一次。
type fontPathKey struct {
	font             *canvas.Font
	size             float64
	style            drawing.FontStyle
	variant          canvas.FontVariant
	direction        text.Direction
	script           text.Script
	language         string
	value            string
	fauxBold         float64
	fauxItalic       float64
	xOffset, yOffset int32
	mmPerEm          float64
}

// textLineKey 标识一次 canvas 原生文字整形结果（NewTextLine）。native 文字
// 绘制是 canvas 后端的渲染热点，相同字体/字号/样式/纯色画笔/文本整形成果
// 可复用；提示模式影响像素定位，须独立缓存。带渐变/图案画笔的文字不缓存。
// 方向、脚本和语言传入底层整形器，不能依赖 buildTextFace 当前使用零值的约定。
// FauxBold/FauxItalic 也可能由 FontFamily.Face 根据实际可用样式自动生成。
// Deco 可含无法比较的自定义状态，带装饰的文字行直接绕过缓存。
type textLineKey struct {
	font             *canvas.Font
	size             float64
	style            drawing.FontStyle
	variant          canvas.FontVariant
	direction        text.Direction
	script           text.Script
	language         string
	fill             color.RGBA
	value            string
	hinting          font.Hinting
	fauxBold         float64
	fauxItalic       float64
	xOffset, yOffset int32
	mmPerEm          float64
}

// textWidthKey 标识一次确定的文本排版宽度。宽度只由字体数据、字号、样式、
// 书写方向与文本决定（整形成果不含画笔颜色），因此不把填充色放进 key，
// 渐变与图案画笔的文字同样可以复用。
type textWidthKey struct {
	font      *canvas.Font
	size      float64
	style     drawing.FontStyle
	variant   canvas.FontVariant
	direction text.Direction
	script    text.Script
	language  string
	value     string
	mmPerEm   float64
}

type fallbackFace struct {
	family *canvas.FontFamily
	style  drawing.FontStyle
	name   string
}

type fallbackFontSource struct {
	data   []byte
	style  drawing.FontStyle
	digest [sha256.Size]byte
}

func NewFonts(doc *parser.Document) *Fonts {
	onceFonts.Do(func() {
		defaultRegistration := &fallbackRegistration{
			family:   canvas.NewFontFamily("default"),
			renderMu: &sync.Mutex{},
		}
		fallbackRegistry[defaultFallbackKey] = defaultRegistration
		if runtime.GOOS == "android" {
			defaultRegistration.ready = loadAndroidDefaultFont(defaultRegistration.family)
			return
		}
		if runtime.GOOS == "js" {
			// 浏览器没有可供 canvas 查询的本地字体目录。Web 文档应优先
			// 使用 OFD 内嵌字体，避免调用系统字体索引导致 WASM panic。
			return
		}
		var fontPath string
		var err error
		if fontPath, err = fileutil.FindFirstFileInDirs(font.DefaultFontDirs(), "simhei.ttf", "simfang.ttf", "simsun.ttc", "simkai.ttf"); err == nil {
			slog.Debug("load default font file", "path", fontPath)
			if err = defaultRegistration.family.LoadFontFile(fontPath, canvasStyle(drawing.FontRegular)); err == nil {
				defaultRegistration.ready = true
				return
			}
		}
		// 缺失字体的兜底：优先中文字体（OFD 多为中文），再补跨平台普遍存在的
		// 西文无衬线字体与通用族，尽量保证任何系统都能找到一个可渲染的字体。
		for _, name := range []string{
			"仿宋", "FangSong", "NSimSum", "楷体", "KaiTi", "黑体", "SimHei",
			"Noto Sans CJK SC", "Noto Sans SC", "Source Han Sans SC", "WenQuanYi Micro Hei",
			"Microsoft YaHei", "微软雅黑", "PingFang SC", "Hiragino Sans GB", "STHeiti",
			"Arial", "Helvetica", "Helvetica Neue", "Segoe UI", "Tahoma", "Verdana",
			"Liberation Sans", "DejaVu Sans", "Noto Sans", "Cantarell",
			"sans-serif", "Noto Serif", "DejaVu Serif", "Times",
		} {
			slog.Debug("load default system font", "family", name, "style", drawing.FontRegular)
			if err := defaultRegistration.family.LoadSystemFont(name, canvasStyle(drawing.FontRegular)); err == nil {
				defaultRegistration.ready = true
				break
			}
		}
		if !defaultRegistration.ready {
			// 族名都匹配不到时按常见字体文件名兜底：部分精简系统没有可用的
			// fontconfig 别名，但字体文件仍在标准目录中。
			for _, file := range []string{"DejaVuSans.ttf", "LiberationSans-Regular.ttf", "NotoSans-Regular.ttf", "Arial.ttf", "arial.ttf"} {
				if fontPath, err := fileutil.FindFirstFileInDirs(font.DefaultFontDirs(), file); err == nil {
					slog.Debug("load default font file", "path", fontPath)
					if err := defaultRegistration.family.LoadFontFile(fontPath, canvasStyle(drawing.FontRegular)); err == nil {
						defaultRegistration.ready = true
						break
					}
				}
			}
		}
	})
	return &Fonts{
		Document:          doc,
		Fonts:             make(map[models.StRefID]*canvas.FontFamily),
		fallbacks:         make(map[string]*canvas.FontFamily),
		fallbackByFont:    make(map[models.StRefID]string),
		glyphMappings:     make(map[models.StRefID]map[rune]uint16),
		loadLocks:         make(map[models.StRefID]*sync.Mutex),
		renderLocks:       make(map[weak.Pointer[canvas.FontFamily]]*sync.Mutex),
		embeddedFonts:     make(map[embeddedFontKey]*canvas.FontFamily),
		embeddedRepairers: make(map[[sha256.Size]byte]*fontrepair.Repairer),
	}
}

// RegisterGlyphs 登记某个字体的 Unicode→字形映射。新增或变更映射时会作废该
// 字体已加载的族，使后续 LoadFont 用完整映射重建，保证渲染使用原始文本。
func (p *Fonts) RegisterGlyphs(id models.StRefID, pairs map[rune]uint16) {
	if p == nil || len(pairs) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.isClosed() {
		return
	}
	mapping := p.glyphMappings[id]
	if mapping == nil {
		mapping = make(map[rune]uint16, len(pairs))
		p.glyphMappings[id] = mapping
	}
	changed := false
	for r, glyph := range pairs {
		if current, ok := mapping[r]; !ok || current != glyph {
			mapping[r] = glyph
			changed = true
		}
	}
	if !changed {
		return
	}
	delete(p.Fonts, id)
	delete(p.fallbackByFont, id)
	if p.glyphVersions == nil {
		p.glyphVersions = make(map[models.StRefID]uint64)
	}
	p.glyphVersions[id]++
	p.generation++
}

// glyphMappingList 返回某个字体已登记的映射，调用方需持有 p.mu。
func (p *Fonts) glyphMappingList(id models.StRefID) []fontrepair.GlyphMapping {
	mapping := p.glyphMappings[id]
	if len(mapping) == 0 {
		return nil
	}
	list := make([]fontrepair.GlyphMapping, 0, len(mapping))
	for r, glyph := range mapping {
		list = append(list, fontrepair.GlyphMapping{Rune: r, Glyph: glyph})
	}
	return list
}

func (p *Fonts) loadLock(id models.StRefID) *sync.Mutex {
	p.loadLocksMu.Lock()
	defer p.loadLocksMu.Unlock()
	if lock := p.loadLocks[id]; lock != nil {
		return lock
	}
	if p.isClosed() {
		return &sync.Mutex{}
	}
	if p.loadLocks == nil {
		p.loadLocks = make(map[models.StRefID]*sync.Mutex)
	}
	lock := &sync.Mutex{}
	p.loadLocks[id] = lock
	return lock
}

func (p *Fonts) isClosed() bool {
	return p.closed.Load()
}

// CacheGeneration 返回当前字形映射与回退字体集合的版本号。
func (p *Fonts) CacheGeneration() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.generation
}

func (p *Fonts) RenderLock(handle drawing.FontFamily) *sync.Mutex {
	family, ok := handle.(*canvas.FontFamily)
	if !ok || family == nil {
		return &sync.Mutex{}
	}
	key := weak.Make(family)
	p.renderLocksMu.Lock()
	lock := p.renderLocks[key]
	p.renderLocksMu.Unlock()
	if lock != nil {
		return lock
	}
	fontCacheMu.Lock()
	for _, registration := range fallbackRegistry {
		if registration.family == family && registration.renderMu != nil {
			lock := registration.renderMu
			p.rememberRenderLock(family, lock)
			fontCacheMu.Unlock()
			return lock
		}
	}
	if lock := fontRenderLocks[family]; lock != nil {
		p.rememberRenderLock(family, lock)
		fontCacheMu.Unlock()
		return lock
	}
	fontCacheMu.Unlock()
	p.renderLocksMu.Lock()
	defer p.renderLocksMu.Unlock()
	if p.isClosed() {
		return &sync.Mutex{}
	}
	if p.renderLocks == nil {
		p.renderLocks = make(map[weak.Pointer[canvas.FontFamily]]*sync.Mutex)
	}
	if lock := p.renderLocks[key]; lock != nil {
		return lock
	}
	lock = &sync.Mutex{}
	p.trimRenderLocksLocked()
	p.renderLocks[key] = lock
	return lock
}

// rememberRenderLock 保存共享锁原指针，后续文字对象无需再查询全局注册表。
// 调用方可持有 fontCacheMu；不能在持有 renderLocksMu 时反向获取全局锁。
func (p *Fonts) rememberRenderLock(family *canvas.FontFamily, lock *sync.Mutex) {
	p.renderLocksMu.Lock()
	defer p.renderLocksMu.Unlock()
	if p.isClosed() {
		return
	}
	if p.renderLocks == nil {
		p.renderLocks = make(map[weak.Pointer[canvas.FontFamily]]*sync.Mutex)
	}
	p.trimRenderLocksLocked()
	p.renderLocks[weak.Make(family)] = lock
}

// 锁表不能强引用已被字形映射替换的字体族，否则丢弃解析缓存也无法回收字体。
// 弱键保留同一存活字体的锁身份；定期清掉失效键，避免锁条目本身持续增长。
func (p *Fonts) trimRenderLocksLocked() {
	if len(p.renderLocks) < 64 {
		return
	}
	for key := range p.renderLocks {
		if key.Value() == nil {
			delete(p.renderLocks, key)
		}
	}
}

// Close 释放本实例持有的全部字体族引用。
//
// 字体族很重（一个 CJK 字体约 50 MB：文件字节只占五分之一，其余是解码后的
// 字形轮廓），而渲染文档通常只在打开期间需要它们。文档关闭后若不释放，引用
// 计数降不到 0，包级缓存就永远留着这份轮廓；WASM 侧浏览多个文档时这些内存
// 会累积，而线性内存不归还给宿主。
//
// 幂等：重复调用不会重复释放。
func (p *Fonts) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.isClosed() {
		p.mu.Unlock()
		return
	}
	p.closed.Store(true)
	// 逐族放手，否则注册表的引用计数只增不减，字体字节永远等不到释放。
	// 调用方没声明删除时 releaseFallbackFamily 只减计数不删表，行为不变。
	fontCacheMu.Lock()
	for family := range p.fallbacks {
		releaseFallbackFamily(family)
	}
	fontCacheMu.Unlock()
	p.Fonts = map[models.StRefID]*canvas.FontFamily{}
	p.fallbacks = map[string]*canvas.FontFamily{}
	p.fallbackFaces = nil
	p.fallbackByFont = nil
	p.glyphMappings = nil
	p.glyphVersions = nil
	p.glyphCollections = nil
	p.glyphSnapshots = nil
	p.mu.Unlock()
	p.loadLocksMu.Lock()
	p.loadLocks = nil
	p.loadLocksMu.Unlock()

	p.pathCacheMu.Lock()
	p.pathCache = shapingCache[fontPathKey, *canvas.Path]{}
	p.pathCacheMu.Unlock()
	p.textLineMu.Lock()
	p.textLineCache = shapingCache[textLineKey, *canvas.Text]{}
	p.textLineMu.Unlock()
	p.textWidthMu.Lock()
	p.textWidthCache = shapingCache[textWidthKey, float64]{}
	p.textWidthMu.Unlock()

	// 内嵌字体与修复器只属于本实例，清空即可回收（返回的回退族由全局注册表持有，
	// 不在此列）。置为空表而非 nil，避免关闭后仍有并发访问时向 nil map 写入 panic。
	p.embeddedMu.Lock()
	p.embeddedFonts = map[embeddedFontKey]*canvas.FontFamily{}
	p.embeddedRepairers = make(map[[sha256.Size]byte]*fontrepair.Repairer)
	p.embeddedSources = nil
	p.embeddedMu.Unlock()
	p.renderLocksMu.Lock()
	p.renderLocks = make(map[weak.Pointer[canvas.FontFamily]]*sync.Mutex)
	p.renderLocksMu.Unlock()
	p.retiredFonts.Clear()

	releaseSystemFonts(p)
}

// releaseSystemFonts 解除 p 对系统字体缓存中所有条目（含 fontRenderLocks）的持有；
// 仅当再无任何字体引擎实例持有时才移除条目，使字体族可被回收。
//
// 反向索引独立于会重建的 p.Fonts，只遍历本实例实际持有的条目。
//
// 必须按引用计数判断而不能无条件删除：同一字体文件常被多个文档共享（这正是缓存
// 去重的目的），若在第一个文档关闭时就删掉条目与渲染锁，后续文档仍在绘制该字体族
// ——fontRenderLocks 缺失会让 RenderLock 回退到实例级锁，同一字体族就可能拿到两把
// 锁而被并发渲染。
func releaseSystemFonts(p *Fonts) {
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	for key, entry := range p.systemFonts {
		if entry.release(p) && systemFontCache[key] == entry {
			delete(systemFontCache, key)
			delete(fontRenderLocks, entry.family)
		}
	}
	p.systemFonts = nil
}

// retainSystemFontLocked 与全局持有者表同步登记反向索引，调用方持有 fontCacheMu。
func (p *Fonts) retainSystemFontLocked(key systemFontKey, entry *systemFontCacheEntry) {
	entry.retain(p)
	if p.systemFonts == nil {
		p.systemFonts = make(map[systemFontKey]*systemFontCacheEntry)
	}
	p.systemFonts[key] = entry
}

// systemFontLookupUsable 报告能否按族名查系统字体。浏览器/wasm 环境没有可枚举
// 的系统字体目录，tdewolff/font 在那里遍历目录时会空指针崩溃，因此必须先挡住。
// wasm 的字体由 RegisterFallbackFont 注册后走 selectFallback 路径。
func systemFontLookupUsable() bool { return runtime.GOOS != "js" }

func loadCachedSystemFont(p *Fonts, name string, style drawing.FontStyle) (*canvas.FontFamily, bool) {
	if !systemFontLookupUsable() {
		return nil, false
	}
	// 逻辑名只匹配一次；缓存键和实际加载都使用同一个文件路径。
	filename, ok := canvas.FindSystemFont(name, canvasStyle(style))
	if !ok {
		return nil, false
	}
	return loadCachedFontFile(p, filename, name, style)
}

// loadCachedFontFile 按确切的文件路径加载并缓存字体，返回共享的 FontFamily。
//
// 与 loadCachedSystemFont 的区别是调用方已经拿到了文件路径，不必再让 canvas 去
// 匹配系统字体。两者共用同一张缓存表，因此"经族名匹配到的字体"与"直接按路径
// 加载的同一文件"会合并成一份——这正是重复加载的来源。
// FontFamily 的名称沿用首次加载的别名，仅用于输出和诊断；回退分类不能依赖它。
func loadCachedFontFile(p *Fonts, filename, name string, style drawing.FontStyle) (*canvas.FontFamily, bool) {
	key, ok := systemFontKeyForFile(filename, style)
	if !ok {
		return nil, false
	}
	return loadCachedFont(p, key, name, func(family *canvas.FontFamily) error {
		slog.Debug("load font file", "family", name, "file", filename, "style", style)
		return family.LoadFontFile(filename, canvasStyle(style))
	})
}

// loadCachedFont 将耗时加载与缓存发布分开；同一文件并发首次加载仍只解析一份。
// load 仅在未命中缓存时调用，期间不持有 fontCacheMu。
func loadCachedFont(p *Fonts, key systemFontKey, name string, load func(*canvas.FontFamily) error) (*canvas.FontFamily, bool) {
	acquire := func() (*canvas.FontFamily, bool) {
		if entry := systemFontCache[key]; entry != nil {
			p.retainSystemFontLocked(key, entry)
			p.rememberRenderLock(entry.family, entry.renderMu)
			return entry.family, true
		}
		return nil, false
	}
	fontCacheMu.Lock()
	if p.isClosed() {
		fontCacheMu.Unlock()
		return nil, false
	}
	if family, ok := acquire(); ok {
		fontCacheMu.Unlock()
		return family, true
	}
	fontCacheMu.Unlock()

	systemFontLoadMu.Lock()
	defer systemFontLoadMu.Unlock()
	fontCacheMu.Lock()
	if p.isClosed() {
		fontCacheMu.Unlock()
		return nil, false
	}
	if family, ok := acquire(); ok {
		fontCacheMu.Unlock()
		return family, true
	}
	fontCacheMu.Unlock()

	family := canvas.NewFontFamily(cssFontFamilyName(name))
	if err := load(family); err != nil {
		return nil, false
	}
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if p.isClosed() {
		return nil, false
	}
	entry := &systemFontCacheEntry{family: family, renderMu: &sync.Mutex{}}
	p.retainSystemFontLocked(key, entry)
	systemFontCache[key] = entry
	fontRenderLocks[family] = entry.renderMu
	p.rememberRenderLock(family, entry.renderMu)
	return family, true
}

// systemFontKeyForFile 用字体文件的身份构造缓存键。
func systemFontKeyForFile(filename string, style drawing.FontStyle) (systemFontKey, bool) {
	info, err := os.Stat(filename)
	if err != nil {
		return systemFontKey{}, false
	}
	return systemFontKey{
		path:  filename,
		size:  info.Size(),
		mtime: info.ModTime().UnixNano(),
		style: style,
	}, true
}

// cssFontFamilyName 把字体族名规范成合法的 CSS 标识符。canvas 的 SVG/TeX 输出把
// 族名直接拼进 font 简写且不加引号，而 OFD 内嵌字体常带子集前缀（如
// "BCCDEE+FZHTK--GBK1-0"），"+" 不是合法的 CSS 标识符字符，会让整条 font 声明
// 作废，浏览器退回默认字号（文字明显变大）。把非法字符替换为 "-" 既让声明生效，
// 又保留各族名之间的差异。
func cssFontFamilyName(name string) string {
	if name == "" {
		return name
	}
	var b strings.Builder
	b.Grow(len(name) + 1)
	for _, r := range name {
		switch {
		case r == '-' || r == '_' || r == ' ' || unicode.IsLetter(r) || unicode.IsDigit(r) || r >= 0x80:
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	safe := b.String()
	// 标识符不能以数字开头（如 "0"），补一个字母前缀。
	if safe != "" && unicode.IsDigit([]rune(safe)[0]) {
		return "f" + safe
	}
	return safe
}

// loadCachedEmbeddedFont 按字体数据、样式、族名与字形映射缓存内嵌字体解析结果，
// 同一字体仅保留最新映射版本，避免逐页修补 cmap 时累计保留全部旧字体。
// 缓存在 Fonts 实例上，随文档关闭释放。
func loadCachedEmbeddedFont(p *Fonts, name string, data []byte, style drawing.FontStyle, mappings []fontrepair.GlyphMapping) (*canvas.FontFamily, error) {
	return loadCachedEmbeddedFontSnapshot(p, name, &embeddedFontSource{data: data, digest: sha256.Sum256(data)}, style,
		&fontGlyphSnapshot{mappings: mappings})
}

func loadCachedEmbeddedFontSnapshot(p *Fonts, name string, source *embeddedFontSource, style drawing.FontStyle, snapshot *fontGlyphSnapshot) (*canvas.FontFamily, error) {
	key := embeddedFontKey{
		name:           name,
		style:          style,
		dataDigest:     source.digest,
		mappingsDigest: snapshot.hash(),
	}
	p.embeddedMu.Lock()
	if p.isClosed() {
		p.embeddedMu.Unlock()
		return nil, fmt.Errorf("字体上下文已关闭")
	}
	if family := p.embeddedFonts[key]; family != nil {
		p.embeddedMu.Unlock()
		return family, nil
	}
	p.embeddedMu.Unlock()

	// 同名字体资源可能嵌入不同子集，同一资源也会按页面登记不同的字形映射。
	// SVG按CSS族名选字体，不能只凭原名称合并，否则同名@font-face会互相覆盖。
	// 字体数据和映射摘要作为内部族名的一部分；资源列表仍显示文档原始字体名。
	safeName := cssFontFamilyName(name)
	if safeName == "" {
		safeName = "OFD-Embedded"
	}
	familyName := fmt.Sprintf("%s-%x-%x", safeName, key.dataDigest[:8], key.mappingsDigest[:8])
	family := canvas.NewFontFamily(familyName)
	var mappings []fontrepair.GlyphMapping
	if snapshot != nil {
		mappings = snapshot.mappings
	}
	if err := loadEmbeddedFont(p, family, source, style, mappings); err != nil {
		return nil, err
	}
	p.embeddedMu.Lock()
	if p.isClosed() {
		p.embeddedMu.Unlock()
		return nil, fmt.Errorf("字体上下文已关闭")
	}
	if existing := p.embeddedFonts[key]; existing != nil {
		p.embeddedMu.Unlock()
		return existing, nil
	}
	// 同一字体逐页补充 cmap 时只缓存最新映射，旧族仍由正在绘制的页面持有。
	// 旧族保持只读，清掉的仅是可重新计算的缓存引用。
	for previousKey, previous := range p.embeddedFonts {
		if previousKey.name == key.name && previousKey.style == key.style && previousKey.dataDigest == key.dataDigest {
			delete(p.embeddedFonts, previousKey)
			p.forgetShapingCache(previous, style)
		}
	}
	p.embeddedFonts[key] = family
	p.embeddedMu.Unlock()
	return family, nil
}

// embeddedRepairer 返回该字体字节对应的 fontrepair.Repairer，必要时创建。修复器与
// 内嵌字体族一样按 Fonts 实例缓存，随文档关闭释放。
func embeddedRepairer(p *Fonts, data []byte) *fontrepair.Repairer {
	return embeddedRepairerWithDigest(p, data, sha256.Sum256(data))
}

func embeddedRepairerWithDigest(p *Fonts, data []byte, digest [sha256.Size]byte) *fontrepair.Repairer {
	p.embeddedMu.Lock()
	if repairer := p.embeddedRepairers[digest]; repairer != nil {
		p.embeddedMu.Unlock()
		return repairer
	}
	p.embeddedMu.Unlock()

	repairer := fontrepair.NewRepairer(data)
	p.embeddedMu.Lock()
	if p.isClosed() {
		p.embeddedMu.Unlock()
		return repairer
	}
	if existing := p.embeddedRepairers[digest]; existing != nil {
		p.embeddedMu.Unlock()
		return existing
	}
	p.embeddedRepairers[digest] = repairer
	p.embeddedMu.Unlock()
	return repairer
}

// hashGlyphMappings 以与遍历顺序无关的方式对字形映射求摘要。
func hashGlyphMappings(mappings []fontrepair.GlyphMapping) [sha256.Size]byte {
	if len(mappings) == 0 {
		return [sha256.Size]byte{}
	}
	sorted := append([]fontrepair.GlyphMapping(nil), mappings...)
	slices.SortFunc(sorted, func(a, b fontrepair.GlyphMapping) int {
		if order := cmp.Compare(a.Rune, b.Rune); order != 0 {
			return order
		}
		return cmp.Compare(a.Glyph, b.Glyph)
	})
	h := sha256.New()
	var buf [8]byte
	for _, mapping := range sorted {
		binary.LittleEndian.PutUint32(buf[:4], uint32(mapping.Rune))
		binary.LittleEndian.PutUint32(buf[4:], uint32(mapping.Glyph))
		h.Write(buf[:])
	}
	var digest [sha256.Size]byte
	h.Sum(digest[:0])
	return digest
}

func loadAndroidDefaultFont(family *canvas.FontFamily) bool {
	preferredNames := []string{
		"NotoSansCJK-Regular.ttc",
		"NotoSansCJK-Regular.ttf",
		"NotoSansCJK-Regular.otf",
		"NotoSansCJK-VF.ttf",
		"NotoSansSC-Regular.otf",
		"NotoSansSC-Regular.ttf",
		"DroidSansFallback.ttf",
		"NotoSans-Regular.ttf",
		"Roboto-Regular.ttf",
	}
	for _, dir := range font.DefaultFontDirs() {
		for _, name := range preferredNames {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err != nil {
				continue
			}
			if loadFontFileSafely(family, path) {
				return true
			}
		}
	}

	// 不同 Android 版本和厂商使用的字体文件名可能不同。继续尝试其他
	// 常规字体文件，但不使用 canvas 的系统字体索引，因为新版 Android
	// 可能无法提供该索引。
	for _, dir := range font.DefaultFontDirs() {
		found := false
		_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			if !isAndroidFontFile(entry.Name()) {
				return nil
			}
			if loadFontFileSafely(family, path) {
				found = true
				return fs.SkipAll
			}
			return nil
		})
		if found {
			return true
		}
	}
	return false
}

func loadFontFileSafely(family *canvas.FontFamily, path string) (loaded bool) {
	slog.Debug("load android font file", "path", path)
	defer func() {
		if recover() != nil {
			loaded = false
		}
	}()
	return family.LoadFontFile(path, canvasStyle(drawing.FontRegular)) == nil && fontFamilySupportsCJK(family)
}

func isAndroidFontFile(name string) bool {
	extension := strings.ToLower(filepath.Ext(name))
	return extension == ".ttf" || extension == ".ttc" || extension == ".otf"
}

func fontFamilySupportsCJK(family *canvas.FontFamily) bool {
	if !fontFamilyUsable(family) {
		return false
	}
	face := family.Face(1, canvas.Black)
	return face != nil && face.Font != nil && face.Font.GlyphIndex('中') != 0
}

const maxFontLoadAttempts = 4

var errFontChangedDuringLoad = errors.New("字体配置在加载期间持续变化，请重试")

func (p *Fonts) LoadFont(id models.StRefID) (drawing.FontFamily, error) {
	if p == nil {
		return nil, fmt.Errorf("字体上下文为空")
	}
	loadLock := p.loadLock(id)
	loadLock.Lock()
	defer loadLock.Unlock()

	for attempt := 0; attempt < maxFontLoadAttempts; attempt++ {
		p.mu.Lock()
		if p.isClosed() {
			p.mu.Unlock()
			return nil, fmt.Errorf("字体上下文已关闭")
		}
		if family := p.Fonts[id]; family != nil {
			p.mu.Unlock()
			return family, nil
		}
		var ft *models.Font
		if p.Document != nil {
			ft = p.Document.GetFont(models.StID(id))
		}
		fallbacks := append([]fallbackFace(nil), p.fallbackFaces...)
		glyphVersion, fallbackVersion := p.glyphVersions[id], p.fallbackVersion
		mappings := p.glyphSnapshot(id)
		p.mu.Unlock()

		family, fallbackName, err := p.loadFontUncached(id, ft, fallbacks, mappings)

		p.mu.Lock()
		if p.isClosed() {
			p.mu.Unlock()
			releaseSystemFonts(p)
			return nil, fmt.Errorf("字体上下文已关闭")
		}
		if glyphVersion != p.glyphVersions[id] || fallbackVersion != p.fallbackVersion {
			p.mu.Unlock()
			continue
		}
		if err != nil {
			p.mu.Unlock()
			return nil, err
		}
		if current := p.Fonts[id]; current != nil {
			p.mu.Unlock()
			return current, nil
		}
		p.Fonts[id] = family
		if fallbackName != "" {
			p.fallbackByFont[id] = fallbackName
		} else {
			delete(p.fallbackByFont, id)
		}
		p.mu.Unlock()
		return family, nil
	}
	return nil, errFontChangedDuringLoad
}

func (p *Fonts) loadFontUncached(id models.StRefID, ft *models.Font, fallbacks []fallbackFace, mappings *fontGlyphSnapshot) (*canvas.FontFamily, string, error) {
	defaultFamily, defaultReady := defaultFallbackFont()
	if ft == nil {
		if fallback, ok := selectFallback(fallbacks, drawing.FontRegular); ok {
			return fallback.family, fallback.family.Name(), nil
		}
		if !defaultReady {
			return nil, "", fmt.Errorf("字体 %d 不存在且没有可用的默认字体", id)
		}
		return defaultFamily, "", nil
	}

	fontName := ft.FontName
	fontStyle := drawing.FontRegular
	if ft.Italic {
		fontStyle |= drawing.FontItalic
	}
	if ft.Bold {
		fontStyle |= drawing.FontBold
	}
	var embeddedErr error
	if ft.FontFile != "" {
		source, readErr := p.readEmbeddedFont(string(ft.FontFile))
		if readErr == nil {
			if family, err := loadCachedEmbeddedFontSnapshot(p, fontName, source, fontStyle, mappings); err == nil {
				return family, "", nil
			} else {
				embeddedErr = err
			}
		} else {
			embeddedErr = readErr
		}
	}

	var matched *canvas.FontFamily
	p.Document.ForEachFont(func(candidateID models.StID, candidate *models.Font) bool {
		// 没有 FontFile 的资源只是逻辑字体，不能借用同名的 OFD 子集字体。
		if ft.FontFile == "" || candidateID == models.StID(id) || candidate.FontFile == "" || !sameFontName(*ft, *candidate) {
			return true
		}
		source, err := p.readEmbeddedFont(string(candidate.FontFile))
		if err != nil {
			return true
		}
		slog.Debug("load embedded font candidate", "family", fontName, "style", fontStyle, "bytes", len(source.data))
		// mappings 来自请求资源的 CGTransform，只能解释其声明字体的字形编号。
		// 同名候选可能是另一份子集，编号不同，不能将请求资源的映射注入候选。
		// 该兼容回退仅使用候选自身可恢复的 Unicode cmap；缺字仍可能无法按原文
		// 成形，交由后续缺字回退处理，不能凭同名猜测字形对应关系。
		if family, err := loadCachedEmbeddedFontSnapshot(p, fontName, source, fontStyle, nil); err == nil {
			matched = family
			return false
		}
		return true
	})
	if matched != nil {
		return matched, "", nil
	}

	if runtime.GOOS == "js" {
		if fallback, ok := selectFallback(fallbacks, fontStyle, ft.FontName, ft.FamilyName); ok {
			return fallback.family, fallback.family.Name(), nil
		}
		if defaultReady {
			return defaultFamily, "", nil
		}
		return nil, "", fmt.Errorf("浏览器没有可用的字体 %q，请使用内嵌字体", fontName)
	}
	if fallback, ok := selectFallback(fallbacks, fontStyle, ft.FontName, ft.FamilyName); ok {
		return fallback.family, fallback.family.Name(), nil
	}
	if runtime.GOOS == "android" {
		if defaultReady {
			return defaultFamily, "", nil
		}
	} else {
		for _, candidate := range systemFontCandidates(ft) {
			if family, ok := loadCachedSystemFont(p, candidate, fontStyle); ok {
				return family, "", nil
			}
		}
	}
	if family, ok := p.loadFixedWidthFont(ft, fontStyle); ok {
		return family, "", nil
	}
	// 逻辑字体没有嵌入数据时按族名匹配系统字体。OFD 的 FontName 通常是
	// 资源名（如 F0），不能拿来匹配，必须以 FamilyName（如"仿宋_GB2312"、
	// "方正小标宋_GBK"）为准。
	group := cjkFontGroup(ft)
	if group != "" {
		if files, ok := cjkFontFiles[group]; ok && len(files) > 0 {
			if fontPath, err := fileutil.FindFirstFileInDirs(font.DefaultFontDirs(), files...); err == nil {
				// 走缓存：这条路此前直接 LoadFontFile，同一个字体系在文档里出现多次就会
				// 重复读入同一份十几 MB 的字体。实测渲染一个 5 页中文文档时 simkai.ttf
				// 被读了两遍。
				if cached, ok := loadCachedFontFile(p, fontPath, group, fontStyle); ok {
					return cached, "", nil
				}
			}
		}
	}
	if defaultFamily != nil {
		if !defaultReady {
			return nil, "", fmt.Errorf("字体 %d 无法加载且没有可用的默认字体", id)
		}
		if embeddedErr != nil {
			slog.Warn("字体不可用，使用默认字体", "id", uint64(id), "name", ft.FontName, "error", embeddedErr)
		} else {
			slog.Warn("字体不可用，使用默认字体", "id", uint64(id), "name", ft.FontName)
		}
		return defaultFamily, "", nil
	}
	return nil, "", fmt.Errorf("字体 %d 无法加载", id)
}

// systemFontCandidates 返回用于匹配系统字体的候选族名。OFD 逻辑字体的
// FamilyName 通常是 PDF 的 PostScript 名（如 NimbusRomNo9L-Regu），不能直接
// 作为系统族名使用；这里给出常见 PostScript 名的别名和 serif/sans-serif/
// monospace 通用族，便于回退到同类的系统字体，避免衬线/无衬线风格错乱。

func defaultFallbackFont() (*canvas.FontFamily, bool) {
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	registration := fallbackRegistry[defaultFallbackKey]
	if registration == nil {
		return nil, false
	}
	return registration.family, registration.ready
}

func selectFallback(fallbacks []fallbackFace, style drawing.FontStyle, fontNames ...string) (fallbackFace, bool) {
	bestScore := int(^uint(0) >> 1)
	var best fallbackFace
	found := false
	for _, face := range fallbacks {
		score := absInt(face.style.CSS() - style.CSS())
		if face.style.Italic() != style.Italic() {
			score += 1000
		}
		for _, fontName := range fontNames {
			if sameFallbackName(face.name, fontName) {
				score -= 1000000
				break
			}
		}
		if !found || score < bestScore {
			bestScore = score
			best = face
			found = true
		}
	}
	return best, found
}

func registerFallbackFont(data []byte, family string, style drawing.FontStyle) error {
	if len(data) == 0 {
		return fmt.Errorf("回退字体数据为空")
	}
	if family == "" {
		return fmt.Errorf("回退字体族名为空")
	}
	_, err := registerFallbackFamily(family, style, data, loadFallbackFace)
	return err
}

// FallbackFontData 返回已全局注册回退字体族的首个来源数据（用于字体签名等
// 只读检查）；未注册时 ok 为 false。
// 返回的源数据由注册表共享，调用方不得修改。
func fallbackFontData(family string) ([]byte, bool) {
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	reg := fallbackRegistry[family]
	if reg == nil || len(reg.sources) == 0 {
		return nil, false
	}
	return reg.sources[0].data, true
}

// RemoveFallbackFont 移除本字体上下文先前通过 UseFallbackFont 登记的回退字体族，
// 使缺失字体不再回退到该族。全局注册表不回滚，但本上下文的回退集合与解析缓存会
// 重建，后续渲染即恢复为内嵌或默认字体。
func (p *Fonts) RemoveFallbackFont(family string) {
	if p == nil || family == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.isClosed() {
		return
	}
	if _, ok := p.fallbacks[family]; !ok && !p.hasFallbackFace(family, drawing.FontRegular) {
		return
	}
	delete(p.fallbacks, family)
	fontCacheMu.Lock()
	releaseFallbackFamily(family)
	fontCacheMu.Unlock()
	kept := p.fallbackFaces[:0]
	for _, face := range p.fallbackFaces {
		if face.name != family {
			kept = append(kept, face)
		} else {
			p.forgetShapingCache(face.family, face.style)
		}
	}
	clear(p.fallbackFaces[len(kept):])
	p.fallbackFaces = kept
	p.Fonts = make(map[models.StRefID]*canvas.FontFamily)
	p.fallbackByFont = make(map[models.StRefID]string)
	p.fallbackVersion++
	p.generation++
}

func (p *Fonts) UseFallbackFont(family string) error {
	if p == nil {
		return fmt.Errorf("字体上下文为空")
	}
	if family == "" {
		return fmt.Errorf("回退字体族名为空")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.isClosed() {
		return fmt.Errorf("字体上下文已关闭")
	}
	// 与关闭、移除保持实例锁→全局缓存锁的顺序；选取字体和取得引用
	// 在同一临界区完成，不能把旧快照的引用登记到同名的新注册条目。
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	reg := fallbackRegistry[family]
	if reg == nil || reg.family == nil || len(reg.sources) == 0 {
		return fmt.Errorf("回退字体族 %q 未注册", family)
	}
	globalFamily, sources := reg.family, reg.sources
	if p.fallbacks[family] == globalFamily && p.hasAllFallbackFaces(family, sources) {
		// 该实例已应用全局字体族，无需再次登记。
		return nil
	}
	alreadyHeld := p.fallbacks[family] != nil
	p.fallbacks[family] = globalFamily
	for _, source := range sources {
		p.applyFallbackFace(family, source.style, globalFamily)
	}
	// 登记持有：注册表据此判断字节何时可以真正释放。
	if !alreadyHeld {
		acquireFallbackFamily(family)
	}
	// 新样式可能重建全局字体族，已有页面仍引用旧族；各实例保留原渲染锁，
	// 让新旧字体面都按同一注册条目的锁串行绘制，关闭时一并清理。
	p.renderLocksMu.Lock()
	p.trimRenderLocksLocked()
	p.renderLocks[weak.Make(globalFamily)] = reg.renderMu
	p.renderLocksMu.Unlock()
	// 回退集合变化后需重建按字体 ID 的解析缓存。
	p.Fonts = make(map[models.StRefID]*canvas.FontFamily)
	p.fallbackByFont = make(map[models.StRefID]string)
	p.fallbackVersion++
	p.generation++
	return nil
}

func (p *Fonts) hasFallbackFace(family string, style drawing.FontStyle) bool {
	for _, face := range p.fallbackFaces {
		if face.name == family && face.style == style {
			return true
		}
	}
	return false
}

func (p *Fonts) hasAllFallbackFaces(family string, sources []fallbackFontSource) bool {
	for _, source := range sources {
		if !p.hasFallbackFace(family, source.style) {
			return false
		}
	}
	return true
}

func (p *Fonts) applyFallbackFace(family string, style drawing.FontStyle, globalFamily *canvas.FontFamily) {
	for index, face := range p.fallbackFaces {
		if face.name == family && face.style == style {
			if face.family != globalFamily {
				p.forgetShapingCache(face.family, face.style)
			}
			p.fallbackFaces[index] = fallbackFace{family: globalFamily, style: style, name: family}
			return
		}
	}
	p.fallbackFaces = append(p.fallbackFaces, fallbackFace{family: globalFamily, style: style, name: family})
}

// acquireFallbackFamily 记一次持有。必须在 fontCacheMu 已持有时调用。
// 后续持有不能撤销注销意图；只有显式注册才能重置 dropped。
func acquireFallbackFamily(family string) {
	if family == "" {
		return
	}
	if reg := fallbackRegistry[family]; reg != nil {
		reg.refs++
	}
}

// releaseFallbackFamily 记一次放手；引用归零且调用方已声明不再需要时释放字节。
//
// 未声明不再需要（dropped=false）时只减计数不删：这是在正常关闭一个文档，
// 用户没点删除，字体还要留给下一个文档用。
func releaseFallbackFamily(family string) {
	if family == "" {
		return
	}
	reg := fallbackRegistry[family]
	if reg == nil {
		return
	}
	if reg.refs > 0 {
		reg.refs--
	}
	if reg.refs == 0 && reg.dropped {
		delete(fallbackRegistry, family)
	}
}

// UnregisterFallbackFont 声明某个字体族不再需要，释放其字节。
//
// 仍被打开的文档引用着它时不会立即删除，而是等最后一个文档放手——那些文档
// 还要用它渲染，提前释放会出问题。因此界面上的"删除"对已打开的文档是延迟
// 生效的，这是有意的。
func UnregisterFallbackFont(family string) error {
	if family == "" {
		return fmt.Errorf("回退字体族名为空")
	}
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	reg := fallbackRegistry[family]
	if reg == nil {
		return fmt.Errorf("回退字体族 %q 未注册", family)
	}
	if family == defaultFallbackKey {
		// 进程级默认字体是所有文档的最后一道兜底，反注册会让后续文档直接
		// 渲染失败，而用户界面上并没有、也不该有它的条目。
		return fmt.Errorf("回退字体族 %q 是内置默认字体，不能移除", family)
	}
	reg.dropped = true
	if reg.refs == 0 {
		delete(fallbackRegistry, family)
	}
	return nil
}

// registerFallbackFamily 把 字体族+样式+数据 注册到全局注册表。
// 相同来源重复注册幂等；新增或替换样式时构建包含各样式当前来源的家族，
// 成功后一次性发布，不保留被替换的历史来源。
func registerFallbackFamily(family string, style drawing.FontStyle, data []byte, load func(*canvas.FontFamily, []byte, drawing.FontStyle) error) (*canvas.FontFamily, error) {
	digest := sha256.Sum256(data)
	for {
		fontCacheMu.Lock()
		reg := fallbackRegistry[family]
		if reg == nil {
			if err := fallbackBudgetLocked(family, style, int64(len(data))); err != nil {
				fontCacheMu.Unlock()
				return nil, err
			}
			fontCacheMu.Unlock()
			// 首次注册单独串行化，重复请求仍只解析一次，不向全局表发布半成品。
			fallbackFontLoadMu.Lock()
			fontCacheMu.Lock()
			if fallbackRegistry[family] != nil {
				fontCacheMu.Unlock()
				fallbackFontLoadMu.Unlock()
				continue
			}
			fontCacheMu.Unlock()
			fontFamily := canvas.NewFontFamily(cssFontFamilyName(family))
			if err := load(fontFamily, data, style); err != nil {
				fallbackFontLoadMu.Unlock()
				return nil, err
			}
			reg = &fallbackRegistration{
				family: fontFamily, renderMu: &sync.Mutex{},
				sources: []fallbackFontSource{{data: data, style: style, digest: digest}},
			}
			fontCacheMu.Lock()
			if err := fallbackBudgetLocked(family, style, int64(len(data))); err != nil {
				fontCacheMu.Unlock()
				fallbackFontLoadMu.Unlock()
				return nil, err
			}
			fallbackRegistry[family] = reg
			fontCacheMu.Unlock()
			fallbackFontLoadMu.Unlock()
			return fontFamily, nil
		}
		for _, source := range reg.sources {
			if source.style == style && source.digest == digest {
				reg.dropped = false
				fontFamily := reg.family
				fontCacheMu.Unlock()
				return fontFamily, nil
			}
		}
		if err := fallbackBudgetLocked(family, style, int64(len(data))); err != nil {
			fontCacheMu.Unlock()
			return nil, err
		}
		renderMu := reg.renderMu
		fontCacheMu.Unlock()

		// 渲染持有 renderMu 时可能查询全局缓存；等待渲染锁前必须释放
		// fontCacheMu，避免与缺字回退的字体查询互相等待。
		renderMu.Lock()
		fontCacheMu.Lock()
		if fallbackRegistry[family] != reg {
			fontCacheMu.Unlock()
			renderMu.Unlock()
			continue
		}
		for _, source := range reg.sources {
			if source.style == style && source.digest == digest {
				reg.dropped = false
				fontFamily := reg.family
				fontCacheMu.Unlock()
				renderMu.Unlock()
				return fontFamily, nil
			}
		}
		// 每个样式只保留当前数据。复制来源切片后替换，失败时仍保留完整旧快照。
		sources := append([]fallbackFontSource(nil), reg.sources...)
		fontCacheMu.Unlock()
		current := fallbackFontSource{data: data, style: style, digest: digest}
		replaced := false
		for i := range sources {
			if sources[i].style == style {
				sources[i] = current
				replaced = true
				break
			}
		}
		if !replaced {
			sources = append(sources, current)
		}
		// 已发布的家族保持只读，新增样式也构造新快照；使用旧字体的页面仍可绘制。
		fontFamily := canvas.NewFontFamily(cssFontFamilyName(family))
		for _, source := range sources {
			if err := load(fontFamily, source.data, source.style); err != nil {
				renderMu.Unlock()
				return nil, err
			}
		}

		fontCacheMu.Lock()
		// 解析时可能已注销并同名重注册，不能把旧条目的结果写回新条目。
		if fallbackRegistry[family] != reg {
			fontCacheMu.Unlock()
			renderMu.Unlock()
			continue
		}
		if err := fallbackBudgetLocked(family, style, int64(len(data))); err != nil {
			fontCacheMu.Unlock()
			renderMu.Unlock()
			return nil, err
		}
		reg.family = fontFamily
		reg.dropped = false
		// 字体数据由调用方持有，注册表只保留引用。
		reg.sources = sources
		fontCacheMu.Unlock()
		renderMu.Unlock()
		return fontFamily, nil
	}
}

const (
	maxFallbackFamilies    = 64
	maxFallbackSourceBytes = 256 << 20
)

// 注册表示显式配置，不自动淘汰仍需供后续文档使用的字体。超限时拒绝新来源，
// 保留现有注册；按各样式当前编码字节计费，解析后的堆占用不等于此预算。
// 必须持有 fontCacheMu；默认字体不占用户注册额度。
func fallbackBudgetLocked(family string, style drawing.FontStyle, size int64) error {
	count, total := 0, size
	for name, reg := range fallbackRegistry {
		if name == defaultFallbackKey {
			continue
		}
		count++
		for _, source := range reg.sources {
			if name != family || source.style != style {
				total += int64(len(source.data))
			}
		}
	}
	if fallbackRegistry[family] == nil {
		count++
	}
	if count > maxFallbackFamilies {
		return fmt.Errorf("回退字体族数量超过上限 %d，请先注销不再使用的字体", maxFallbackFamilies)
	}
	if total > maxFallbackSourceBytes {
		return fmt.Errorf("回退字体源数据总量超过上限 %d MB，请先注销不再使用的字体", maxFallbackSourceBytes>>20)
	}
	return nil
}

func loadFallbackFace(family *canvas.FontFamily, data []byte, style drawing.FontStyle) error {
	slog.Debug("load fallback font", "family", family.Name(), "style", style, "bytes", len(data))
	if err := family.LoadFont(data, 0, canvasStyle(style)); err != nil {
		return err
	}
	selectUnicodeCmap(family.Face(1, canvasStyle(style)).Font)
	if !fontFamilyUsable(family) {
		return fmt.Errorf("回退字体结构不可用")
	}
	return nil
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// FallbackFontFamily 返回为文档字体选择的外部字体族。
// 必须先调用 LoadFont；TextLayouts 和 Text 会自动完成这一步。
func (p *Fonts) FallbackFontFamily(id models.StRefID) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fallbackByFont[id]
}

// HasLoadedEmbeddedFont 判断文档字体是否解析为可用且已加载的字体族。
// 仅声明 FontFile 并不够，因为文件可能缺失、格式错误或被字体解析器拒绝。
func (p *Fonts) HasLoadedEmbeddedFont(id models.StRefID) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Document == nil {
		return false
	}
	ft := p.Document.GetFont(models.StID(id))
	if ft == nil || ft.FontFile == "" {
		return false
	}
	_, ok := p.Fonts[id]
	return ok && p.fallbackByFont[id] == ""
}

// loadEmbeddedFont 优先使用 fontrepair 修复后的内嵌字体，修复失败时再尝试
// 原始字体数据。只有两者都无法加载时，调用方才会继续使用外部字体回退。
// mappings 为文档提供的 Unicode→字形映射，用于让缺少 Unicode cmap 的子集字体
// 也能按原始文本成形，从而在 PDF 等输出中保留可复制文字。
func loadEmbeddedFont(p *Fonts, family *canvas.FontFamily, source *embeddedFontSource, style drawing.FontStyle, mappings []fontrepair.GlyphMapping) error {
	data := source.data
	var failures []error
	if len(mappings) > 0 {
		fixed, err := embeddedRepairerWithDigest(p, data, source.digest).WithGlyphReferences(mappings)
		if err != nil {
			failures = append(failures, fmt.Errorf("内嵌字体字形映射修复: %w", err))
		} else {
			slog.Debug("load embedded font with glyph mappings", "family", family.Name(), "style", style, "mappings", len(mappings))
			if err = family.LoadFont(fixed, 0, canvasStyle(style)); err != nil {
				failures = append(failures, fmt.Errorf("加载映射字体: %w", err))
			} else if !fontFamilyUsable(family) {
				failures = append(failures, fmt.Errorf("映射字体缺少必要字形表"))
			} else {
				return nil
			}
		}
	}
	if fixed, err := fontrepair.Repair(data); err != nil {
		failures = append(failures, fmt.Errorf("内嵌字体修复: %w", err))
	} else {
		slog.Debug("load repaired embedded font", "family", family.Name(), "style", style, "bytes", len(fixed))
		if err = family.LoadFont(fixed, 0, canvasStyle(style)); err != nil {
			failures = append(failures, fmt.Errorf("加载修复字体: %w", err))
		} else if !fontFamilyUsable(family) {
			failures = append(failures, fmt.Errorf("修复字体缺少必要字形表"))
		} else {
			return nil
		}
	}
	slog.Debug("load original embedded font", "family", family.Name(), "style", style, "bytes", len(data))
	if err := family.LoadFont(data, 0, canvasStyle(style)); err != nil {
		failures = append(failures, fmt.Errorf("加载原始字体: %w", err))
	} else if !fontFamilyUsable(family) {
		failures = append(failures, fmt.Errorf("原始字体缺少必要字形表"))
	} else {
		return nil
	}
	if len(failures) > 0 {
		return fmt.Errorf("嵌入字体结构不可用: %w", errors.Join(failures...))
	}
	return fmt.Errorf("嵌入字体结构不可用")
}

func sameFontName(left, right models.Font) bool {
	if left.FontName != "" && left.FontName == right.FontName {
		return true
	}
	return left.FamilyName != "" && left.FamilyName == right.FamilyName
}

// fontFamilyUsable 确保字体包含 canvas 绘制文字所需的基本字形表。
func fontFamilyUsable(family *canvas.FontFamily) bool {
	return fontFamilyUsableSafe(family)
}

func fontFamilyUsableSafe(family *canvas.FontFamily) (usable bool) {
	defer func() {
		if recover() != nil {
			usable = false
		}
	}()
	if family == nil {
		return false
	}
	face := family.Face(1, canvas.Black)
	return face != nil && face.Font != nil && face.Font.SFNT != nil &&
		face.Font.SFNT.Head != nil && face.Font.SFNT.Hhea != nil &&
		face.Font.SFNT.OS2 != nil && face.Font.SFNT.Cmap != nil &&
		face.Font.SFNT.Maxp != nil
}
