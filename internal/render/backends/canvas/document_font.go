package canvas

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"image/color"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/text"
	"github.com/tdewolff/font"
	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/utils"
)

// 本文件实现 canvas 字体引擎 Fonts：字体资源解析、内嵌字体加载、字形映射
// 登记、回退字体全局注册表与串行化锁；系统字体候选匹配见 font_candidates.go，
// 字体面与文字样式适配见 font_face.go。

var (
	onceFonts         sync.Once
	fontCacheMu       sync.Mutex
	systemFontCache   = make(map[systemFontKey]*systemFontCacheEntry)
	embeddedFontCache = make(map[embeddedFontKey]*embeddedFontCacheEntry)
	// embeddedRepairerCache 按字体字节摘要缓存 fontfix.Repairer。同一份内嵌
	// 字体在文档逐页登记字形映射时会被反复修复，而修复结果只取决于字体字节；
	// Repairer 缓存修复后的基础字体与已解析 cmap，使每次修补只需重新合并与
	// 输出 cmap 表。WithGlyphs 不改写缓存状态，可安全并发调用。
	embeddedRepairerCache = make(map[[sha256.Size]byte]*fontfix.Repairer)
	fontRenderLocks       = make(map[*canvas.FontFamily]*sync.Mutex)
	// 全局回退字体注册表：同一字体族全局只保存一份解析结果和一份字体数据引用，
	// 首个 render.Document 注册后即锁定；后续文档缺字体时默认全部使用已锁定字体。
	fallbackRegistry = make(map[string]*fallbackRegistration)
)

// fallbackRegistration 保存某个字体族全局唯一定位后的解析家族与来源引用。
type fallbackRegistration struct {
	family   *canvas.FontFamily
	sources  []fallbackFontSource
	ready    bool
	renderMu *sync.Mutex
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

type systemFontCacheEntry struct {
	family   *canvas.FontFamily
	renderMu *sync.Mutex
	// holders 登记正在使用该字体族的字体引擎实例。
	//
	// 字体族本身很重：一个 CJK 字体约 50 MB（文件字节只占五分之一，其余是解码
	// 后的字形轮廓）。此前缓存无上限也无淘汰，而每个 render.Document 各自持有
	// 指针，长会话里浏览多个文档会持续累积。
	//
	// 有了 holders 才能安全淘汰：只要还有文档持有该族就不能动它——这同时也
	// 保证了 fontRenderLocks 不会在文档仍在绘制时被删掉，导致同一字体拿到两把
	// 锁而被并发渲染。降到 0 时才可以连同渲染锁一起释放。
	holders map[*Fonts]struct{}
}

// retain 登记持有者。
func (e *systemFontCacheEntry) retain(f *Fonts) {
	if e.holders == nil {
		e.holders = make(map[*Fonts]struct{}, 1)
	}
	e.holders[f] = struct{}{}
}

// embeddedFontKey 标识一次内嵌字体解析结果。相同字体数据、样式、族名和字形
// 映射只需解析一次；字形映射来自 map，摘要按排序后的内容计算以保证顺序无关。
type embeddedFontKey struct {
	name           string
	style          drawing.FontStyle
	dataDigest     [sha256.Size]byte
	mappingsDigest [sha256.Size]byte
}

type embeddedFontCacheEntry struct {
	family   *canvas.FontFamily
	renderMu *sync.Mutex
}

type Fonts struct {
	*parser.Document
	Fonts          map[models.StRefID]*canvas.FontFamily
	fallbacks      map[string]*canvas.FontFamily
	fallbackFaces  []fallbackFace
	fallbackByFont map[models.StRefID]string
	glyphMappings  map[models.StRefID]map[rune]uint16
	mu             sync.Mutex
	loadLocksMu    sync.Mutex
	loadLocks      map[models.StRefID]*sync.Mutex
	generation     uint64
	renderLocksMu  sync.Mutex
	renderLocks    map[*canvas.FontFamily]*sync.Mutex
	pathCacheMu    sync.Mutex
	pathCache      map[fontPathKey]*canvas.Path
	// closed 标记 Close 已调用，重复调用直接返回。
	closed         bool
	textLineMu     sync.Mutex
	textLineCache  map[textLineKey]*canvas.Text
	textWidthMu    sync.Mutex
	textWidthCache map[textWidthKey]float64
}

// fontPathKey 标识一次确定的字形轮廓计算结果：轮廓几何只由字体数据、
// 字号、样式与文本决定（ToPath 不受画笔颜色与绘制矩阵影响），因此跨
// TextObject、跨页面复用同一轮廓是安全的。字形轮廓拆解（必要时含
// harfbuzz 整形）是渲染热点，缓存后相同字符串只计算一次。
type fontPathKey struct {
	font    *canvas.Font
	size    float64
	style   drawing.FontStyle
	variant canvas.FontVariant
	value   string
}

// textLineKey 标识一次 canvas 原生文字整形结果（NewTextLine）。native 文字
// 绘制是 canvas 后端的渲染热点，相同字体/字号/样式/纯色画笔/文本整形成果
// 可复用；带渐变/图案画笔的文字不缓存（画笔会随文本对象变化）。
type textLineKey struct {
	font    *canvas.Font
	size    float64
	style   drawing.FontStyle
	variant canvas.FontVariant
	fill    color.RGBA
	value   string
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
		if fontPath, err = utils.FindFirstFileInDirs(font.DefaultFontDirs(), "simhei.ttf", "simfang.ttf", "simsun.ttc", "simkai.ttf"); err == nil {
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
				if fontPath, err := utils.FindFirstFileInDirs(font.DefaultFontDirs(), file); err == nil {
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
		Document:       doc,
		Fonts:          make(map[models.StRefID]*canvas.FontFamily),
		fallbacks:      make(map[string]*canvas.FontFamily),
		fallbackByFont: make(map[models.StRefID]string),
		glyphMappings:  make(map[models.StRefID]map[rune]uint16),
		loadLocks:      make(map[models.StRefID]*sync.Mutex),
		renderLocks:    make(map[*canvas.FontFamily]*sync.Mutex),
		pathCache:      make(map[fontPathKey]*canvas.Path),
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
	p.generation++
}

// glyphMappingList 返回某个字体已登记的映射，调用方需持有 p.mu。
func (p *Fonts) glyphMappingList(id models.StRefID) []fontfix.GlyphMapping {
	mapping := p.glyphMappings[id]
	if len(mapping) == 0 {
		return nil
	}
	list := make([]fontfix.GlyphMapping, 0, len(mapping))
	for r, glyph := range mapping {
		list = append(list, fontfix.GlyphMapping{Rune: r, Glyph: glyph})
	}
	return list
}

func (p *Fonts) loadLock(id models.StRefID) *sync.Mutex {
	p.loadLocksMu.Lock()
	defer p.loadLocksMu.Unlock()
	if p.loadLocks == nil {
		p.loadLocks = make(map[models.StRefID]*sync.Mutex)
	}
	if lock := p.loadLocks[id]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	p.loadLocks[id] = lock
	return lock
}

func (p *Fonts) RenderLock(handle drawing.FontFamily) *sync.Mutex {
	family, ok := handle.(*canvas.FontFamily)
	if !ok || family == nil {
		return &sync.Mutex{}
	}
	fontCacheMu.Lock()
	for _, registration := range fallbackRegistry {
		if registration.family == family && registration.renderMu != nil {
			lock := registration.renderMu
			fontCacheMu.Unlock()
			return lock
		}
	}
	if lock := fontRenderLocks[family]; lock != nil {
		fontCacheMu.Unlock()
		return lock
	}
	fontCacheMu.Unlock()
	p.renderLocksMu.Lock()
	defer p.renderLocksMu.Unlock()
	if p.renderLocks == nil {
		p.renderLocks = make(map[*canvas.FontFamily]*sync.Mutex)
	}
	if lock := p.renderLocks[family]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	p.renderLocks[family] = lock
	return lock
}

// shapedTextPath 返回 (face, value) 拍平后的字形轮廓路径，带正文级缓存。
// 相同字体数据 + 字号 + 样式 + 文本的轮廓对完全相同，直接复用可避免对
// 重复字符串反复整形与解析字形轮廓。返回的 canvas.Path 不可变，跨对象
// 复用安全；缓存有界，超出后整体清空。

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
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	// 三个字段都持有字体族：按资源 id 登记的、已锁定的回退族、以及回退面列表。
	// 漏掉任何一个都会让对应字体留在全局缓存里。
	families := make([]*canvas.FontFamily, 0, len(p.Fonts)+len(p.fallbacks)+len(p.fallbackFaces))
	for _, family := range p.Fonts {
		families = append(families, family)
	}
	for _, family := range p.fallbacks {
		families = append(families, family)
	}
	for _, face := range p.fallbackFaces {
		families = append(families, face.family)
	}
	p.Fonts = map[models.StRefID]*canvas.FontFamily{}
	p.fallbacks = map[string]*canvas.FontFamily{}
	p.fallbackFaces = nil
	p.pathCache = map[fontPathKey]*canvas.Path{}
	p.mu.Unlock()

	releaseFontFamilies(families)
}

// releaseFontFamilies 解除这些字体族的持有者引用，无人再持有的从缓存与渲染锁
// 表中移除。
func releaseFontFamilies(families []*canvas.FontFamily) {
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	released := make(map[*canvas.FontFamily]bool, len(families))
	for _, family := range families {
		if family == nil || released[family] {
			continue
		}
		released[family] = true
		for key, entry := range systemFontCache {
			if entry.family != family {
				continue
			}
			entry.holders = nil
			// 没人再持有：连同渲染锁一起移除，字体族随之可被回收。此刻仍持有
			// 该族的文档都已释放过，删锁不会让同一字体拿到两把锁。
			delete(systemFontCache, key)
			delete(fontRenderLocks, family)
		}
	}
}

func loadCachedSystemFont(p *Fonts, name string, style drawing.FontStyle) (*canvas.FontFamily, bool) {
	// 先把逻辑名解析成字体文件，键才建得准。解析不出来就走原路径让 canvas 报错，
	// 不在这里替它猜。
	filename, ok := canvas.FindSystemFont(name, canvasStyle(style))
	if !ok {
		return nil, false
	}
	key, ok := systemFontKeyForFile(filename, style)
	if !ok {
		return nil, false
	}

	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if entry := systemFontCache[key]; entry != nil {
		entry.retain(p)
		slog.Debug("reuse system font", "family", name, "file", filename, "style", style)
		return entry.family, true
	}

	family := canvas.NewFontFamily(name)
	slog.Debug("load system font", "family", name, "file", filename, "style", style)
	if err := family.LoadSystemFont(name, canvasStyle(style)); err != nil {
		return nil, false
	}
	entry := &systemFontCacheEntry{family: family, renderMu: &sync.Mutex{}}
	entry.retain(p)
	systemFontCache[key] = entry
	fontRenderLocks[family] = entry.renderMu
	return family, true
}

// loadCachedFontFile 按确切的文件路径加载并缓存字体，返回共享的 FontFamily。
//
// 与 loadCachedSystemFont 的区别是调用方已经拿到了文件路径，不必再让 canvas 去
// 匹配系统字体。两者共用同一张缓存表，因此"经族名匹配到的字体"与"直接按路径
// 加载的同一文件"会合并成一份——这正是重复加载的来源。
func loadCachedFontFile(p *Fonts, filename, name string, style drawing.FontStyle) (*canvas.FontFamily, bool) {
	key, ok := systemFontKeyForFile(filename, style)
	if !ok {
		return nil, false
	}
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	if entry := systemFontCache[key]; entry != nil {
		entry.retain(p)
		return entry.family, true
	}
	family := canvas.NewFontFamily(name)
	slog.Debug("load font file", "family", name, "file", filename, "style", style)
	if err := family.LoadFontFile(filename, canvasStyle(style)); err != nil {
		return nil, false
	}
	entry := &systemFontCacheEntry{family: family, renderMu: &sync.Mutex{}}
	entry.retain(p)
	systemFontCache[key] = entry
	fontRenderLocks[family] = entry.renderMu
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

// loadCachedEmbeddedFont 按字体数据、样式、族名与字形映射缓存内嵌字体解析结果，
// 使同一字体在多次转换（或多文档）间只做一次 fontfix 修复与 canvas 解析。返回的
// 字体族注册了共享渲染锁，与系统字体缓存一致地串行化同一字体的绘制。
func loadCachedEmbeddedFont(p *Fonts, name string, data []byte, style drawing.FontStyle, mappings []fontfix.GlyphMapping) (*canvas.FontFamily, error) {
	key := embeddedFontKey{
		name:           name,
		style:          style,
		dataDigest:     sha256.Sum256(data),
		mappingsDigest: hashGlyphMappings(mappings),
	}
	fontCacheMu.Lock()
	if entry := embeddedFontCache[key]; entry != nil {
		fontCacheMu.Unlock()
		return entry.family, nil
	}
	fontCacheMu.Unlock()

	family := canvas.NewFontFamily(name)
	if err := loadEmbeddedFont(family, data, style, mappings); err != nil {
		return nil, err
	}
	renderMu := &sync.Mutex{}
	fontCacheMu.Lock()
	if existing := embeddedFontCache[key]; existing != nil {
		fontCacheMu.Unlock()
		return existing.family, nil
	}
	embeddedFontCache[key] = &embeddedFontCacheEntry{family: family, renderMu: renderMu}
	fontRenderLocks[family] = renderMu
	fontCacheMu.Unlock()
	return family, nil
}

// embeddedRepairer 返回该字体字节对应的 fontfix.Repairer，必要时创建。
// 与 embeddedFontCache 一样按进程生命周期缓存，不做淘汰。
func embeddedRepairer(data []byte) *fontfix.Repairer {
	digest := sha256.Sum256(data)
	fontCacheMu.Lock()
	repairer := embeddedRepairerCache[digest]
	fontCacheMu.Unlock()
	if repairer != nil {
		return repairer
	}
	repairer = fontfix.NewRepairer(data)
	fontCacheMu.Lock()
	if existing := embeddedRepairerCache[digest]; existing != nil {
		fontCacheMu.Unlock()
		return existing
	}
	embeddedRepairerCache[digest] = repairer
	fontCacheMu.Unlock()
	return repairer
}

// hashGlyphMappings 以与遍历顺序无关的方式对字形映射求摘要。
func hashGlyphMappings(mappings []fontfix.GlyphMapping) [sha256.Size]byte {
	if len(mappings) == 0 {
		return [sha256.Size]byte{}
	}
	sorted := append([]fontfix.GlyphMapping(nil), mappings...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Rune != sorted[j].Rune {
			return sorted[i].Rune < sorted[j].Rune
		}
		return sorted[i].Glyph < sorted[j].Glyph
	})
	h := sha256.New()
	var buf [4]byte
	for _, mapping := range sorted {
		binary.LittleEndian.PutUint32(buf[:], uint32(mapping.Rune))
		h.Write(buf[:])
		binary.LittleEndian.PutUint32(buf[:], uint32(mapping.Glyph))
		h.Write(buf[:])
	}
	var digest [sha256.Size]byte
	copy(digest[:], h.Sum(nil))
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

func (p *Fonts) LoadFont(id models.StRefID) (drawing.FontFamily, error) {
	if p == nil {
		return nil, fmt.Errorf("字体上下文为空")
	}
	loadLock := p.loadLock(id)
	loadLock.Lock()
	defer loadLock.Unlock()

	for {
		p.mu.Lock()
		if family := p.Fonts[id]; family != nil {
			p.mu.Unlock()
			return family, nil
		}
		var ft *models.Font
		if p.Document != nil {
			ft = p.Document.GetFont(models.StID(id))
		}
		fallbacks := append([]fallbackFace(nil), p.fallbackFaces...)
		generation := p.generation
		p.mu.Unlock()

		family, fallbackName, err := p.loadFontUncached(id, ft, fallbacks)

		p.mu.Lock()
		if generation != p.generation {
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
}

func (p *Fonts) loadFontUncached(id models.StRefID, ft *models.Font, fallbacks []fallbackFace) (*canvas.FontFamily, string, error) {
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
	if ft.FontFile != "" {
		if data, err := p.FileCache.Read(string(ft.FontFile)); err == nil {
			p.mu.Lock()
			mappings := p.glyphMappingList(id)
			p.mu.Unlock()
			if family, err := loadCachedEmbeddedFont(p, fontName, data, fontStyle, mappings); err == nil {
				return family, "", nil
			}
		}
	}

	var matched *canvas.FontFamily
	p.Document.ForEachFont(func(candidateID models.StID, candidate *models.Font) bool {
		// 没有 FontFile 的资源只是逻辑字体，不能借用同名的 OFD 子集字体。
		if ft.FontFile == "" || candidateID == models.StID(id) || candidate.FontFile == "" || !sameFontName(*ft, *candidate) {
			return true
		}
		data, err := p.FileCache.Read(string(candidate.FontFile))
		if err != nil {
			return true
		}
		slog.Debug("load embedded font candidate", "family", fontName, "style", fontStyle, "bytes", len(data))
		if family, err := loadCachedEmbeddedFont(p, fontName, data, fontStyle, nil); err == nil {
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
	if family, ok := loadFixedWidthFont(ft, fontStyle); ok {
		return family, "", nil
	}
	family := canvas.NewFontFamily(fontName)
	// 逻辑字体没有嵌入数据时按族名匹配系统字体。OFD 的 FontName 通常是
	// 资源名（如 F0），不能拿来匹配，必须以 FamilyName（如"仿宋_GB2312"、
	// "方正小标宋_GBK"）为准。
	group := cjkFontGroup(ft)
	if group != "" {
		if files, ok := cjkFontFiles[group]; ok && len(files) > 0 {
			if fontPath, err := utils.FindFirstFileInDirs(font.DefaultFontDirs(), files...); err == nil {
				// 走缓存：这条路此前直接 LoadFontFile，同一个字体系在文档里出现多次就会
				// 重复读入同一份十几 MB 的字体。实测渲染一个 5 页中文文档时 simkai.ttf
				// 被读了两遍。
				if cached, ok := loadCachedFontFile(p, fontPath, group, fontStyle); ok {
					return cached, "", nil
				}
				slog.Debug("load fallback system font file", "family", group, "path", fontPath, "style", fontStyle)
				if err := family.LoadFontFile(fontPath, canvasStyle(fontStyle)); err == nil {
					return family, "", nil
				}
			}
		}
	}
	if defaultFamily != nil {
		if !defaultReady {
			return nil, "", fmt.Errorf("字体 %d 无法加载且没有可用的默认字体", id)
		}
		slog.Warn("字体不可用，使用默认字体", "id", uint64(id), "name", ft.FontName)
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
	_, err := registerFallbackFamily(family, style, data)
	return err
}

// FallbackFontData 返回已全局注册回退字体族的首个来源数据（用于字体签名等
// 只读检查）；未注册时 ok 为 false。
func fallbackFontData(family string) ([]byte, bool) {
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	reg := fallbackRegistry[family]
	if reg == nil || len(reg.sources) == 0 {
		return nil, false
	}
	return reg.sources[0].data, true
}

// UseFallbackFont 使当前字体上下文在缺失字体时使用已全局注册的回退字体族。
// 该字体族必须先通过 RegisterFallbackFont 注册；其全部已注册样式都会生效。
func (p *Fonts) UseFallbackFont(family string) error {
	if p == nil {
		return fmt.Errorf("字体上下文为空")
	}
	if family == "" {
		return fmt.Errorf("回退字体族名为空")
	}

	fontCacheMu.Lock()
	reg := fallbackRegistry[family]
	fontCacheMu.Unlock()
	if reg == nil {
		return fmt.Errorf("回退字体族 %q 未注册", family)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fallbacks[family] == reg.family && p.hasAllFallbackFaces(family, reg.sources) {
		// 该实例已应用全局字体族，无需再次登记。
		return nil
	}
	p.fallbacks[family] = reg.family
	for _, source := range reg.sources {
		p.applyFallbackFace(family, source.style, reg.family)
	}
	// 回退集合变化后需重建按字体 ID 的解析缓存。
	p.Fonts = make(map[models.StRefID]*canvas.FontFamily)
	p.fallbackByFont = make(map[models.StRefID]string)
	p.generation++
	return nil
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
	if _, ok := p.fallbacks[family]; !ok && !p.hasFallbackFace(family, drawing.FontRegular) {
		return
	}
	delete(p.fallbacks, family)
	kept := p.fallbackFaces[:0]
	for _, face := range p.fallbackFaces {
		if face.name != family {
			kept = append(kept, face)
		}
	}
	p.fallbackFaces = kept
	p.Fonts = make(map[models.StRefID]*canvas.FontFamily)
	p.fallbackByFont = make(map[models.StRefID]string)
	p.generation++
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
			p.fallbackFaces[index] = fallbackFace{family: globalFamily, style: style, name: family}
			return
		}
	}
	p.fallbackFaces = append(p.fallbackFaces, fallbackFace{family: globalFamily, style: style, name: family})
}

// registerFallbackFamily 把 字体族+样式+数据 注册到全局注册表。
// 同一 字体族+样式+数据 只处理一次（锁定）；相同字体族引入不同样式/字体时
// 构建包含全部来源的全局多样式家族，同样只构建一次。
func registerFallbackFamily(family string, style drawing.FontStyle, data []byte) (*canvas.FontFamily, error) {
	digest := sha256.Sum256(data)
	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()

	reg := fallbackRegistry[family]
	if reg == nil {
		reg = &fallbackRegistration{renderMu: &sync.Mutex{}}
		fallbackRegistry[family] = reg
	}
	for _, source := range reg.sources {
		if source.style == style && source.digest == digest {
			return reg.family, nil
		}
	}

	reg.renderMu.Lock()
	defer reg.renderMu.Unlock()
	fontFamily := reg.family
	if len(reg.sources) == 0 || !sameFallbackFontData(reg.sources, digest) {
		fontFamily = canvas.NewFontFamily(family)
		for _, source := range reg.sources {
			if err := loadFallbackFace(fontFamily, source.data, source.style); err != nil {
				if len(reg.sources) == 0 {
					delete(fallbackRegistry, family)
				}
				return nil, err
			}
		}
	}
	if err := loadFallbackFace(fontFamily, data, style); err != nil {
		if len(reg.sources) == 0 {
			delete(fallbackRegistry, family)
		}
		return nil, err
	}
	reg.family = fontFamily
	// 字体数据由调用方（webreader.Reader）持有，注册表只保留引用，整个进程
	// 只保存一份字体字节。
	reg.sources = append(reg.sources, fallbackFontSource{data: data, style: style, digest: digest})
	return reg.family, nil
}

func loadFallbackFace(family *canvas.FontFamily, data []byte, style drawing.FontStyle) error {
	slog.Debug("load fallback font", "family", family.Name(), "style", style, "bytes", len(data))
	if err := family.LoadFont(data, 0, canvasStyle(style)); err != nil {
		return err
	}
	if !fontFamilyUsable(family) {
		return fmt.Errorf("回退字体结构不可用")
	}
	return nil
}

func sameFallbackFontData(sources []fallbackFontSource, digest [sha256.Size]byte) bool {
	for _, source := range sources {
		if source.digest != digest {
			return false
		}
	}
	return true
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

// loadEmbeddedFont 优先使用 fontfix 修复后的内嵌字体，修复失败时再尝试
// 原始字体数据。只有两者都无法加载时，调用方才会继续使用外部字体回退。
// mappings 为文档提供的 Unicode→字形映射，用于让缺少 Unicode cmap 的子集字体
// 也能按原始文本成形，从而在 PDF 等输出中保留可复制文字。
func loadEmbeddedFont(family *canvas.FontFamily, data []byte, style drawing.FontStyle, mappings []fontfix.GlyphMapping) error {
	if len(mappings) > 0 {
		if fixed, err := embeddedRepairer(data).WithGlyphs(mappings); err == nil {
			slog.Debug("load embedded font with glyph mappings", "family", family.Name(), "style", style, "mappings", len(mappings))
			if err = family.LoadFont(fixed, 0, canvasStyle(style)); err == nil && fontFamilyUsable(family) {
				return nil
			}
		}
	}
	if fixed, err := fontfix.Repair(data); err == nil {
		slog.Debug("load repaired embedded font", "family", family.Name(), "style", style, "bytes", len(fixed))
		if err = family.LoadFont(fixed, 0, canvasStyle(style)); err == nil && fontFamilyUsable(family) {
			return nil
		}
	}
	slog.Debug("load original embedded font", "family", family.Name(), "style", style, "bytes", len(data))
	if err := family.LoadFont(data, 0, canvasStyle(style)); err == nil && fontFamilyUsable(family) {
		return nil
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

// canvasFontFace 是 FontFace 的 canvas 实现：把 canvas.FontFace 的字形轮廓
// 与度量转换为与绘制库无关的 geom 类型，是核心包内字体能力的适配层。
