package render

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/font"
	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/utils"
)

var (
	onceFonts              sync.Once
	defaultFontFamily      *canvas.FontFamily
	defaultFontFamilyReady bool
	defaultFontRenderMu    sync.Mutex
)

type Fonts struct {
	*parser.Document
	Fonts          map[models.StRefID]*canvas.FontFamily
	fallbacks      map[string]*canvas.FontFamily
	fallbackFaces  []fallbackFace
	fallbackByFont map[models.StRefID]string
	mu             sync.Mutex
	loadLocksMu    sync.Mutex
	loadLocks      map[models.StRefID]*sync.Mutex
	generation     uint64
	renderLocksMu  sync.Mutex
	renderLocks    map[*canvas.FontFamily]*sync.Mutex
}

type fallbackFace struct {
	family *canvas.FontFamily
	style  canvas.FontStyle
}

func NewFonts(doc *parser.Document) *Fonts {
	onceFonts.Do(func() {
		defaultFontFamily = canvas.NewFontFamily("default")
		if runtime.GOOS == "android" {
			defaultFontFamilyReady = loadAndroidDefaultFont(defaultFontFamily)
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
			if err = defaultFontFamily.LoadFontFile(fontPath, canvas.FontRegular); err == nil {
				defaultFontFamilyReady = true
				return
			}
		}
		for _, name := range []string{"仿宋", "FangSong", "NSimSum", "楷体", "KaiTi", "黑体", "SimHei", "Noto Sans CJK SC", "WenQuanYi Micro Hei", "Cantarell", "Noto Sans", "Noto Serif", "DejaVu Sans", "DejaVu Serif", "Times"} {
			if err := defaultFontFamily.LoadSystemFont(name, canvas.FontRegular); err == nil {
				defaultFontFamilyReady = true
				break
			}
		}
	})
	return &Fonts{
		Document:       doc,
		Fonts:          make(map[models.StRefID]*canvas.FontFamily),
		fallbacks:      make(map[string]*canvas.FontFamily),
		fallbackByFont: make(map[models.StRefID]string),
		loadLocks:      make(map[models.StRefID]*sync.Mutex),
		renderLocks:    make(map[*canvas.FontFamily]*sync.Mutex),
	}
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

func (p *Fonts) renderLock(family *canvas.FontFamily) *sync.Mutex {
	if family == defaultFontFamily {
		return &defaultFontRenderMu
	}
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
	defer func() {
		if recover() != nil {
			loaded = false
		}
	}()
	return family.LoadFontFile(path, canvas.FontRegular) == nil && fontFamilySupportsCJK(family)
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

func (p *Fonts) LoadFont(id models.StRefID) (*canvas.FontFamily, error) {
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
	if ft == nil {
		if fallback, ok := selectFallback(fallbacks, canvas.FontRegular); ok {
			return fallback.family, fallback.family.Name(), nil
		}
		if !defaultFontFamilyReady {
			return nil, "", fmt.Errorf("字体 %d 不存在且没有可用的默认字体", id)
		}
		return defaultFontFamily, "", nil
	}

	fontName := ft.FontName
	fontStyle := canvas.FontRegular
	if ft.Italic {
		fontStyle |= canvas.FontItalic
	}
	if ft.Bold {
		fontStyle |= canvas.FontBold
	}
	if ft.FontFile != "" {
		family := canvas.NewFontFamily(fontName)
		if data, err := p.FileCache.Read(string(ft.FontFile)); err == nil {
			if err := loadEmbeddedFont(family, data, fontStyle); err == nil {
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
		if fixed, fixErr := fontfix.Repair(data); fixErr == nil {
			data = fixed
		}
		candidateFamily := canvas.NewFontFamily(fontName)
		if err := candidateFamily.LoadFont(data, 0, fontStyle); err == nil && fontFamilyUsable(candidateFamily) {
			matched = candidateFamily
			return false
		}
		return true
	})
	if matched != nil {
		return matched, "", nil
	}

	family := canvas.NewFontFamily(fontName)
	if runtime.GOOS == "js" {
		if fallback, ok := selectFallback(fallbacks, fontStyle); ok {
			return fallback.family, fallback.family.Name(), nil
		}
		if defaultFontFamilyReady {
			return defaultFontFamily, "", nil
		}
		return nil, "", fmt.Errorf("浏览器没有可用的字体 %q，请使用内嵌字体", fontName)
	}
	if fallback, ok := selectFallback(fallbacks, fontStyle); ok {
		return fallback.family, fallback.family.Name(), nil
	}
	if runtime.GOOS == "android" {
		if defaultFontFamilyReady {
			return defaultFontFamily, "", nil
		}
	} else if err := family.LoadSystemFont(fontName, fontStyle); err == nil {
		return family, "", nil
	}
	if fontName == "宋体" || strings.ToLower(fontName) == "simsun" {
		if fontPath, err := utils.FindFirstFileInDirs(font.DefaultFontDirs(), "simsun.ttc"); err == nil {
			if err := family.LoadFontFile(fontPath, fontStyle); err == nil {
				return family, "", nil
			}
		}
	}
	if fontName == "黑体" || strings.ToLower(fontName) == "simhei" {
		if fontPath, err := utils.FindFirstFileInDirs(font.DefaultFontDirs(), "simhei.ttf"); err == nil {
			if err := family.LoadFontFile(fontPath, fontStyle); err == nil {
				return family, "", nil
			}
		}
	}
	if defaultFontFamily != nil {
		if !defaultFontFamilyReady {
			return nil, "", fmt.Errorf("字体 %d 无法加载且没有可用的默认字体", id)
		}
		slog.Warn("字体不可用，使用默认字体", "id", uint64(id), "name", ft.FontName)
		return defaultFontFamily, "", nil
	}
	return nil, "", fmt.Errorf("字体 %d 无法加载", id)
}

func selectFallback(fallbacks []fallbackFace, style canvas.FontStyle) (fallbackFace, bool) {
	bestScore := int(^uint(0) >> 1)
	var best fallbackFace
	found := false
	for _, face := range fallbacks {
		score := absInt(face.style.CSS() - style.CSS())
		if face.style.Italic() != style.Italic() {
			score += 1000
		}
		if !found || score < bestScore {
			bestScore = score
			best = face
			found = true
		}
	}
	return best, found
}

// AddFallbackFont 为缺失的文档字体注册调用方提供的字体。
// 主要供 WASM 调用方在浏览器中获取 Web 字体后使用。
func (p *Fonts) AddFallbackFont(data []byte, family string, style canvas.FontStyle) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(data) == 0 {
		return fmt.Errorf("回退字体数据为空")
	}
	if family == "" {
		return fmt.Errorf("回退字体族名为空")
	}
	f := p.fallbacks[family]
	isNew := f == nil
	if isNew {
		f = canvas.NewFontFamily(family)
	}
	renderLock := p.renderLock(f)
	renderLock.Lock()
	err := f.LoadFont(data, 0, style)
	renderLock.Unlock()
	if err != nil {
		return err
	}
	if isNew {
		p.fallbacks[family] = f
	}
	for index, face := range p.fallbackFaces {
		if face.family == f && face.style == style {
			p.fallbackFaces[index] = fallbackFace{family: f, style: style}
			p.Fonts = make(map[models.StRefID]*canvas.FontFamily)
			p.fallbackByFont = make(map[models.StRefID]string)
			p.generation++
			return nil
		}
	}
	p.fallbackFaces = append(p.fallbackFaces, fallbackFace{family: f, style: style})
	p.Fonts = make(map[models.StRefID]*canvas.FontFamily)
	p.fallbackByFont = make(map[models.StRefID]string)
	p.generation++
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

// loadEmbeddedFont 优先使用 fontfix 修复后的内嵌字体，修复失败时再尝试
// 原始字体数据。只有两者都无法加载时，调用方才会继续使用外部字体回退。
func loadEmbeddedFont(family *canvas.FontFamily, data []byte, style canvas.FontStyle) error {
	if fixed, err := fontfix.Repair(data); err == nil {
		if err = family.LoadFont(fixed, 0, style); err == nil && fontFamilyUsable(family) {
			return nil
		}
	}
	if err := family.LoadFont(data, 0, style); err == nil && fontFamilyUsable(family) {
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
