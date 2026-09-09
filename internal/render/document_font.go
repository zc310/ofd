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
)

type Fonts struct {
	*parser.Document
	Fonts map[models.StRefID]*canvas.FontFamily
	mu    sync.Mutex
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
	return &Fonts{Document: doc, Fonts: make(map[models.StRefID]*canvas.FontFamily)}
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
	p.mu.Lock()
	defer p.mu.Unlock()

	var err error
	var f *canvas.FontFamily
	if f = p.Fonts[id]; f != nil {
		return f, nil
	}
	ft := p.FontRes[models.StID(id)]
	if ft == nil {
		if !defaultFontFamilyReady {
			return nil, fmt.Errorf("字体 %d 不存在且没有可用的默认字体", id)
		}
		p.Fonts[id] = defaultFontFamily
		return defaultFontFamily, nil
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
		f = canvas.NewFontFamily(fontName)
		var buf []byte
		if buf, err = p.FileCache.Read(string(ft.FontFile)); err != nil {
			return nil, err
		}
		if err = loadEmbeddedFont(f, buf, fontStyle); err == nil {
			p.Fonts[id] = f
			return f, nil
		}
	}
	for candidateID, candidate := range p.FontRes {
		if candidateID == models.StID(id) || candidate.FontFile == "" || !sameFontName(*ft, *candidate) {
			continue
		}
		buf, parseErr := p.FileCache.Read(string(candidate.FontFile))
		if parseErr != nil {
			continue
		}
		if fixed, fixErr := fontfix.Repair(buf); fixErr == nil {
			buf = fixed
		}
		f = canvas.NewFontFamily(fontName)
		if err = f.LoadFont(buf, 0, fontStyle); err == nil && fontFamilyUsable(f) {
			p.Fonts[id] = f
			return f, nil
		}
	}
	f = canvas.NewFontFamily(fontName)

	if runtime.GOOS == "js" {
		if defaultFontFamilyReady {
			p.Fonts[id] = defaultFontFamily
			return defaultFontFamily, nil
		}
		return nil, fmt.Errorf("浏览器没有可用的字体 %q，请使用内嵌字体", fontName)
	}
	if runtime.GOOS == "android" {
		// 新版 Android 的系统字体索引可能为空，因此默认字体族已改为
		// 直接从 /system/fonts 加载。
		if defaultFontFamilyReady {
			p.Fonts[id] = defaultFontFamily
			return defaultFontFamily, nil
		}
		err = fmt.Errorf("Android 没有可用的系统字体")
	} else if err = f.LoadSystemFont(fontName, fontStyle); err == nil {
		p.Fonts[id] = f
		return f, nil
	}
	if fontName == "宋体" || strings.ToLower(fontName) == "simsun" {
		var fontPath string
		if fontPath, err = utils.FindFirstFileInDirs(font.DefaultFontDirs(), "simsun.ttc"); err == nil {
			if err = f.LoadFontFile(fontPath, fontStyle); err == nil {
				p.Fonts[id] = f
				return f, nil
			}
		}
	}
	if fontName == "黑体" || strings.ToLower(fontName) == "simhei" {
		var fontPath string
		if fontPath, err = utils.FindFirstFileInDirs(font.DefaultFontDirs(), "simhei.ttf"); err == nil {
			if err = f.LoadFontFile(fontPath, fontStyle); err == nil {
				p.Fonts[id] = f
				return f, nil
			}
		}
	}
	if defaultFontFamily != nil {
		if !defaultFontFamilyReady {
			return nil, fmt.Errorf("字体 %d 无法加载且没有可用的默认字体", id)
		}
		slog.Warn("字体不可用，使用默认字体", "id", uint64(id), "name", ft.FontName)
		p.Fonts[id] = defaultFontFamily
		return defaultFontFamily, nil
	}
	return defaultFontFamily, nil
}

// loadEmbeddedFont 保留字体原有的 Unicode cmap。只有原始数据无法加载时，
// 才使用 fontfix 为缺少 glyph 映射的嵌入字体补表。
func loadEmbeddedFont(family *canvas.FontFamily, data []byte, style canvas.FontStyle) error {
	if err := family.LoadFont(data, 0, style); err == nil && fontFamilyUsable(family) {
		return nil
	}
	fixed, err := fontfix.Repair(data)
	if err != nil {
		return err
	}
	if err := family.LoadFont(fixed, 0, style); err != nil {
		return err
	}
	if !fontFamilyUsable(family) {
		return fmt.Errorf("嵌入字体结构不可用")
	}
	return nil
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
