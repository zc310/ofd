package models

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	txml "github.com/tdewolff/parse/v2/xml"

	"github.com/zc310/ofd/internal/fastxml"
	"github.com/zc310/ofd/pkg/spec"
)

var (
	errResMissingRoot       = errors.New("资源 XML 缺少根元素")
	errResMissingRootEnd    = errors.New("资源 XML 缺少根元素结束标记")
	errResMismatchedRootEnd = errors.New("资源 XML 根元素结束标记不匹配")
	errColorSpacesEnd       = errors.New("ColorSpaces 结束标记不匹配")
	errColorSpaceEnd        = errors.New("ColorSpace 结束标记不匹配")
	errPaletteEnd           = errors.New("Palette 结束标记不匹配")
	errPaletteCVEnd         = errors.New("CV 结束标记不匹配")
)

// ParseResXML 使用 fastxml.Lexer 解析资源文件。低频且结构复杂的资源子树仍由
// fastxml.DecodeSpan 按模型解码，颜色空间和调色板则直接从词法 token 重建。
func ParseResXML(data []byte) (*Res, error) {
	data, utf16 := fastxml.StripBOM(data)
	if utf16 {
		var resource Res
		if err := xml.Unmarshal(data, &resource); err != nil {
			return nil, err
		}
		return &resource, nil
	}

	resource := &Res{}
	lexer := fastxml.NewLexer(data)
	var rootName []byte
	var rootVoid bool
	for {
		tt, token, _ := lexer.Next()
		if tt == txml.ErrorToken {
			if errors.Is(lexer.Err(), io.EOF) {
				return nil, errResMissingRoot
			}
			return nil, lexer.Err()
		}
		if tt != txml.StartTagToken {
			continue
		}
		rootName = fastxml.TargetName(token)
		var err error
		rootVoid, err = lexer.ReadAttrs(func(name, value []byte) error {
			if string(name) == "BaseLoc" {
				resource.BaseLoc = spec.StLoc(fastxml.Unescape(value))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		break
	}
	if rootVoid {
		return resource, nil
	}

	for {
		tt, token, start := lexer.Next()
		switch tt {
		case txml.ErrorToken:
			if errors.Is(lexer.Err(), io.EOF) {
				return nil, errResMissingRootEnd
			}
			return nil, lexer.Err()
		case txml.EndTagToken:
			if !bytes.Equal(fastxml.EndTagName(token), rootName) {
				return nil, errResMismatchedRootEnd
			}
			return resource, nil
		case txml.StartTagToken:
			var err error
			switch string(fastxml.TargetName(token)) {
			case "ColorSpaces":
				var spaces ColorSpaces
				err = parseColorSpaces(lexer, &spaces)
				if resource.ColorSpaces == nil {
					resource.ColorSpaces = &ColorSpaces{}
				}
				resource.ColorSpaces.ColorSpace = append(resource.ColorSpaces.ColorSpace, spaces.ColorSpace...)
			case "DrawParams":
				var value DrawParams
				err = parseDrawParams(lexer, &value)
				if err == nil {
					if resource.DrawParams == nil {
						resource.DrawParams = &DrawParams{}
					}
					resource.DrawParams.DrawParam = append(resource.DrawParams.DrawParam, value.DrawParam...)
				}
			case "Fonts":
				var value Fonts
				err = lexer.DecodeSpan(start, &value)
				if err == nil {
					if resource.Fonts == nil {
						resource.Fonts = &Fonts{}
					}
					resource.Fonts.Font = append(resource.Fonts.Font, value.Font...)
				}
			case "MultiMedias":
				var value MultiMedias
				err = lexer.DecodeSpan(start, &value)
				if err == nil {
					if resource.MultiMedias == nil {
						resource.MultiMedias = &MultiMedias{}
					}
					resource.MultiMedias.MultiMedia = append(resource.MultiMedias.MultiMedia, value.MultiMedia...)
				}
			case "CompositeGraphicUnits":
				var value CompositeGraphicUnits
				err = lexer.DecodeSpan(start, &value)
				if err == nil {
					if resource.CompositeGraphicUnits == nil {
						resource.CompositeGraphicUnits = &CompositeGraphicUnits{}
					}
					resource.CompositeGraphicUnits.CompositeGraphicUnit = append(resource.CompositeGraphicUnits.CompositeGraphicUnit, value.CompositeGraphicUnit...)
				}
			default:
				_, err = lexer.SpanElement(start)
			}
			if err != nil {
				return nil, err
			}
		case txml.CommentToken, txml.DOCTYPEToken, txml.CDATAToken, txml.StartTagPIToken,
			txml.StartTagCloseToken, txml.StartTagCloseVoidToken, txml.StartTagClosePIToken,
			txml.AttributeToken, txml.TextToken:
			continue
		default:
			continue
		}
	}
}

func parseDrawParams(lexer *fastxml.Lexer, params *DrawParams) error {
	void, err := lexer.ReadAttrs(func(_, _ []byte) error { return nil })
	if err != nil || void {
		return err
	}
	for {
		tt, token, start := lexer.Next()
		switch tt {
		case txml.ErrorToken:
			return lexer.Err()
		case txml.EndTagToken:
			return nil
		case txml.StartTagToken:
			if string(fastxml.TargetName(token)) != "DrawParam" {
				if _, err := lexer.SpanElement(start); err != nil {
					return err
				}
				continue
			}
			param, err := parseDrawParam(lexer)
			if err != nil {
				return err
			}
			params.DrawParam = append(params.DrawParam, param)
		case txml.CommentToken, txml.DOCTYPEToken, txml.CDATAToken, txml.StartTagPIToken,
			txml.StartTagCloseToken, txml.StartTagCloseVoidToken, txml.StartTagClosePIToken,
			txml.AttributeToken, txml.TextToken:
			continue
		default:
			continue
		}
	}
}

func parseDrawParam(lexer *fastxml.Lexer) (*DrawParam, error) {
	param := new(DrawParam)
	void, err := lexer.ReadAttrs(func(name, value []byte) error {
		attr := xml.Attr{Name: xml.Name{Local: string(name)}, Value: fastxml.Unescape(value)}
		switch string(name) {
		case "ID":
			return param.ID.UnmarshalXMLAttr(attr)
		case "Relative":
			return param.Relative.UnmarshalXMLAttr(attr)
		case "LineWidth":
			parsed, err := decodeFloatAttr(attr.Value, "LineWidth")
			if err != nil {
				return err
			}
			param.SetLineWidth(parsed)
		case "Join":
			param.Join = attr.Value
		case "Cap":
			param.Cap = attr.Value
		case "DashOffset":
			parsed, err := decodeFloatAttr(attr.Value, "DashOffset")
			if err != nil {
				return err
			}
			param.SetDashOffset(parsed)
		case "DashPattern":
			param.DashPattern = new(StArray[float64])
			if err := param.DashPattern.UnmarshalXMLAttr(attr); err != nil {
				return err
			}
		case "MiterLimit":
			parsed, err := decodeFloatAttr(attr.Value, "MiterLimit")
			if err != nil {
				return err
			}
			param.SetMiterLimit(parsed)
		}
		return nil
	})
	if err != nil || void {
		return param, err
	}
	for {
		tt, token, start := lexer.Next()
		switch tt {
		case txml.ErrorToken:
			return nil, lexer.Err()
		case txml.EndTagToken:
			return param, nil
		case txml.StartTagToken:
			switch string(fastxml.TargetName(token)) {
			case "FillColor":
				color := new(CTColor)
				if err := lexer.DecodeSpan(start, color); err != nil {
					return nil, err
				}
				param.FillColor = color
			case "StrokeColor":
				color := new(CTColor)
				if err := lexer.DecodeSpan(start, color); err != nil {
					return nil, err
				}
				param.StrokeColor = color
			default:
				if _, err := lexer.SpanElement(start); err != nil {
					return nil, err
				}
			}
		case txml.CommentToken, txml.DOCTYPEToken, txml.CDATAToken, txml.StartTagPIToken,
			txml.StartTagCloseToken, txml.StartTagCloseVoidToken, txml.StartTagClosePIToken,
			txml.AttributeToken, txml.TextToken:
			continue
		default:
			continue
		}
	}
}

func parseColorSpaces(lexer *fastxml.Lexer, spaces *ColorSpaces) error {
	void, err := lexer.ReadAttrs(func(_, _ []byte) error { return nil })
	if err != nil || void {
		return err
	}
	for {
		tt, token, start := lexer.Next()
		switch tt {
		case txml.ErrorToken:
			return lexer.Err()
		case txml.EndTagToken:
			if !bytes.Equal(fastxml.EndTagName(token), []byte("ColorSpaces")) {
				return errColorSpacesEnd
			}
			return nil
		case txml.StartTagToken:
			if string(fastxml.TargetName(token)) != "ColorSpace" {
				if _, err := lexer.SpanElement(start); err != nil {
					return err
				}
				continue
			}
			space, err := parseColorSpace(lexer)
			if err != nil {
				return err
			}
			spaces.ColorSpace = append(spaces.ColorSpace, space)
		case txml.CommentToken, txml.DOCTYPEToken, txml.CDATAToken, txml.StartTagPIToken,
			txml.StartTagCloseToken, txml.StartTagCloseVoidToken, txml.StartTagClosePIToken,
			txml.AttributeToken, txml.TextToken:
			continue
		default:
			continue
		}
	}
}

func parseColorSpace(lexer *fastxml.Lexer) (ColorSpace, error) {
	var space ColorSpace
	void, err := lexer.ReadAttrs(func(name, value []byte) error {
		text := fastxml.Unescape(value)
		switch string(name) {
		case "ID":
			return space.ID.UnmarshalText([]byte(text))
		case "Type":
			space.Type = text
		case "BitsPerComponent":
			parsed, err := strconv.Atoi(text)
			if err != nil {
				return fmt.Errorf("解析颜色空间 BitsPerComponent 失败: %w", err)
			}
			space.BitsPerComponent = parsed
		case "Profile":
			space.Profile = spec.StLoc(text)
		}
		return nil
	})
	if err != nil || void {
		return space, err
	}
	for {
		tt, token, start := lexer.Next()
		switch tt {
		case txml.ErrorToken:
			return space, lexer.Err()
		case txml.EndTagToken:
			if !bytes.Equal(fastxml.EndTagName(token), []byte("ColorSpace")) {
				return space, errColorSpaceEnd
			}
			return space, nil
		case txml.StartTagToken:
			if string(fastxml.TargetName(token)) != "Palette" {
				if _, err := lexer.SpanElement(start); err != nil {
					return space, err
				}
				continue
			}
			palette, err := parsePalette(lexer)
			if err != nil {
				return space, err
			}
			space.Palette = palette
		case txml.CommentToken, txml.DOCTYPEToken, txml.CDATAToken, txml.StartTagPIToken,
			txml.StartTagCloseToken, txml.StartTagCloseVoidToken, txml.StartTagClosePIToken,
			txml.AttributeToken, txml.TextToken:
			continue
		default:
			continue
		}
	}
}

func parsePalette(lexer *fastxml.Lexer) (*Palette, error) {
	palette := &Palette{}
	void, err := lexer.ReadAttrs(func(_, _ []byte) error { return nil })
	if err != nil || void {
		return palette, err
	}
	for {
		tt, token, start := lexer.Next()
		switch tt {
		case txml.ErrorToken:
			return nil, lexer.Err()
		case txml.EndTagToken:
			if !bytes.Equal(fastxml.EndTagName(token), []byte("Palette")) {
				return nil, errPaletteEnd
			}
			return palette, nil
		case txml.StartTagToken:
			if string(fastxml.TargetName(token)) != "CV" {
				if _, err := lexer.SpanElement(start); err != nil {
					return nil, err
				}
				continue
			}
			value, err := parseElementText(lexer)
			if err != nil {
				return nil, err
			}
			palette.CV = append(palette.CV, strings.Fields(string(value)))
		case txml.CommentToken, txml.DOCTYPEToken, txml.CDATAToken, txml.TextToken, txml.StartTagPIToken,
			txml.StartTagCloseToken, txml.StartTagCloseVoidToken, txml.StartTagClosePIToken,
			txml.AttributeToken:
			continue
		default:
			continue
		}
	}
}

func parseElementText(lexer *fastxml.Lexer) ([]byte, error) {
	void, err := lexer.ReadAttrs(func(_, _ []byte) error { return nil })
	if err != nil || void {
		return nil, err
	}
	var text []byte
	for {
		tt, token, start := lexer.Next()
		switch tt {
		case txml.ErrorToken:
			return nil, lexer.Err()
		case txml.TextToken:
			text = fastxml.AppendUnescape(text, token)
		case txml.CDATAToken:
			text = append(text, lexer.CData()...)
		case txml.StartTagToken:
			if _, err := lexer.SpanElement(start); err != nil {
				return nil, err
			}
		case txml.EndTagToken:
			if !bytes.Equal(fastxml.EndTagName(token), []byte("CV")) {
				return nil, errPaletteCVEnd
			}
			return text, nil
		case txml.CommentToken, txml.DOCTYPEToken, txml.StartTagPIToken,
			txml.StartTagCloseToken, txml.StartTagCloseVoidToken, txml.StartTagClosePIToken,
			txml.AttributeToken:
			continue
		default:
			continue
		}
	}
}
