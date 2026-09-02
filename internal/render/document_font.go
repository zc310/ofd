package render

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/font"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/utils"
)

var (
	onceFonts         sync.Once
	defaultFontFamily *canvas.FontFamily
)

type Fonts struct {
	*parser.Document
	Fonts map[models.StRefID]*canvas.FontFamily
}

func NewFonts(doc *parser.Document) *Fonts {
	onceFonts.Do(func() {
		defaultFontFamily = canvas.NewFontFamily("default")
		var filepath string
		var err error
		if filepath, err = utils.FindFirstFileInDirs(font.DefaultFontDirs(), "simhei.ttf", "simfang.ttf", "simsun.ttc", "simkai.ttf"); err == nil {
			if err = defaultFontFamily.LoadFontFile(filepath, canvas.FontRegular); err == nil {
				slog.Info("load default font: " + filepath)
				return
			}
		}
		for _, name := range []string{"仿宋", "FangSong", "NSimSum", "楷体", "KaiTi", "黑体", "SimHei", "Noto Sans CJK SC", "WenQuanYi Micro Hei", "Cantarell", "Noto Sans", "Noto Serif", "DejaVu Sans", "DejaVu Serif", "Times"} {
			if err := defaultFontFamily.LoadSystemFont(name, canvas.FontRegular); err == nil {
				slog.Info("load default font: " + name)
				break
			}
		}
	})
	return &Fonts{Document: doc, Fonts: make(map[models.StRefID]*canvas.FontFamily)}
}
func (p *Fonts) LoadFont(id models.StRefID) (*canvas.FontFamily, error) {
	var err error
	var f *canvas.FontFamily
	if f = p.Fonts[id]; f != nil {
		return f, nil
	}
	ft := p.FontRes[models.StID(id)]
	if ft == nil {
		p.Fonts[id] = defaultFontFamily
		slog.Error(fmt.Sprintf("font %d not exist", id))
		return defaultFontFamily, nil
	}
	fontName := ft.FontName
	f = canvas.NewFontFamily(fontName)

	fontStyle := canvas.FontRegular
	if ft.Italic {
		fontStyle = fontStyle | canvas.FontItalic
	}
	if ft.Bold {
		fontStyle = fontStyle | canvas.FontBold
	}
	if ft.FontFile != "" {
		var buf []byte
		if buf, err = p.FileCache.ParseContent(string(ft.FontFile)); err != nil {
			return nil, err
		}

		if err = f.LoadFont(buf, 0, fontStyle); err == nil && fontFamilyUsable(f) {
			p.Fonts[id] = f
			return f, nil
		}
		slog.Error(fmt.Sprintf("load font %s %s: %s", ft.FontName, ft.FontFile, err))
	}

	if err = f.LoadSystemFont(fontName, fontStyle); err == nil {
		p.Fonts[id] = f
		return f, nil
	}
	if fontName == "宋体" || strings.ToLower(fontName) == "simsun" {
		var filepath string
		if filepath, err = utils.FindFirstFileInDirs(font.DefaultFontDirs(), "simsun.ttc"); err == nil {
			if err = f.LoadFontFile(filepath, fontStyle); err == nil {
				p.Fonts[id] = f
				return f, nil
			}
		}
	}
	if fontName == "黑体" || strings.ToLower(fontName) == "simhei" {
		var filepath string
		if filepath, err = utils.FindFirstFileInDirs(font.DefaultFontDirs(), "simhei.ttf"); err == nil {
			if err = f.LoadFontFile(filepath, fontStyle); err == nil {
				p.Fonts[id] = f
				return f, nil
			}
		}
	}
	slog.Info(fmt.Sprintf("font %d %s %s not exist", id, ft.FontName, ft.FontFile))
	if defaultFontFamily != nil {
		p.Fonts[id] = defaultFontFamily
		return defaultFontFamily, nil
	}

	return defaultFontFamily, nil
}

// fontFamilyUsable 确保字体包含 canvas 绘制文字所需的基本字形表。
func fontFamilyUsable(family *canvas.FontFamily) bool {
	if family == nil {
		return false
	}
	face := family.Face(1, canvas.Black)
	return face != nil && face.Font != nil && face.Font.SFNT != nil &&
		!face.Font.SFNT.IsCFF &&
		face.Font.SFNT.Head != nil && face.Font.SFNT.Hhea != nil &&
		face.Font.SFNT.OS2 != nil && face.Font.SFNT.Cmap != nil &&
		face.Font.SFNT.Maxp != nil
}
