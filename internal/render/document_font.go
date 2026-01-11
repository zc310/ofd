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
	onceFonts  sync.Once
	fontFamily *canvas.FontFamily
)

type Fonts struct {
	*parser.Document
	Fonts map[models.StRefID]*canvas.FontFamily
}

func NewFonts(doc *parser.Document) *Fonts {
	onceFonts.Do(func() {
		fontFamily = canvas.NewFontFamily("default")
		for _, name := range []string{"仿宋", "楷体", "黑体", "Cantarell", "Noto Sans"} {
			if err := fontFamily.LoadSystemFont(name, canvas.FontRegular); err == nil {
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
		p.Fonts[id] = fontFamily
		slog.Error(fmt.Sprintf("font %d not exist", id))
		return fontFamily, nil
	}
	fontName := ft.FontName
	f = canvas.NewFontFamily(fontName)

	//if ft.FontFile != "" {
	//	var buf []byte
	//	if buf, err = p.FileCache.ParseContent(string(ft.FontFile)); err != nil {
	//		return nil, err
	//	}
	//
	//	if err = f.LoadFont(buf, 0, canvas.FontRegular); err == nil {
	//		p.Fonts[id] = f
	//		return f, nil
	//	}
	//	slog.Error(fmt.Sprintf("load font %s %s: %s", ft.FontName, ft.FontFile, err))
	//}

	fontStyle := canvas.FontRegular
	if ft.Italic {
		fontStyle = fontStyle | canvas.FontItalic
	}
	if ft.Bold {
		fontStyle = fontStyle | canvas.FontBold
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
	if fontFamily != nil {
		p.Fonts[id] = fontFamily
		return fontFamily, nil
	}

	return fontFamily, nil
}
