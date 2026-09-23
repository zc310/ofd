package models

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/tdewolff/parse/v2"
	txml "github.com/tdewolff/parse/v2/xml"
)

// 页面内容文件的快速解析路径。页面图层内的对象、字符、路径数据是解析量最大的部分，
// 直接用 tdewolff/parse 的零分配 XML lexer 在原始字节上重建模型，避免 encoding/xml
// 对每个元素、属性做字符串拷贝与反射匹配。低频且复杂的子树（Area、Actions、颜色、
// 边框等）用 spanElement 捕获字节区间后回退到 encoding/xml，语义与改造前一致。

var (
	utf8BOM    = []byte{0xEF, 0xBB, 0xBF}
	utf16LEBOM = []byte{0xFF, 0xFE}
	utf16BEBOM = []byte{0xFE, 0xFF}
)

// pageLexer 包装 tdewolff lexer。绝对字节偏移直接取自底层 parse.Input 的
// 绝对位置 Offset()，避免按 token 缓冲长度累加时被词法器内部跳过的空白字节
// （如开始标签 attribute 之间的空格）所扰动。
type pageLexer struct {
	lx   *txml.Lexer
	in   *parse.Input
	data []byte
	abs  int
}

func newPageLexer(data []byte) *pageLexer {
	in := parse.NewInputBytes(data)
	return &pageLexer{
		lx:   txml.NewLexer(in),
		in:   in,
		data: data,
	}
}

// next 返回下一个 token，start 是该 token 起始处的绝对字节偏移。
func (p *pageLexer) next() (tt txml.TokenType, buf []byte, start int) {
	start = p.in.Offset()
	tt, buf = p.lx.Next()
	p.abs = p.in.Offset()
	return tt, buf, start
}

func (p *pageLexer) err() error {
	if err := p.lx.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("解析 XML 失败: %w", err)
	}
	return io.EOF
}

// absAfter 返回当前已消费位置的绝对字节偏移。
func (p *pageLexer) absAfter() int { return p.abs }

// localName 去掉可能的命名空间前缀。
func localName(b []byte) []byte {
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == ':' {
			return b[i+1:]
		}
	}
	return b
}

// endTagName 从 </Name> 结束标记中取出元素名。
func endTagName(b []byte) []byte {
	name := b[2 : len(b)-1]
	for len(name) > 0 && (name[len(name)-1] == ' ' || name[len(name)-1] == '\t' || name[len(name)-1] == '\n' || name[len(name)-1] == '\r') {
		name = name[:len(name)-1]
	}
	return localName(name)
}

// targetName 从 <Name ...> 开始标记中取出元素名。
func targetName(b []byte) []byte {
	s := b[1:]
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\n', '\r', '/', '>':
			return localName(s[:i])
		}
	}
	return localName(s)
}

// unquote 去掉属性值的引号。
func unquote(b []byte) []byte {
	if len(b) >= 2 && (b[0] == '"' || b[0] == '\'') && b[len(b)-1] == b[0] {
		return b[1 : len(b)-1]
	}
	return b
}

// spanElement 从元素起始偏移 start 消费到匹配的结束标记，返回该元素的原始字节区间。
// 调用时该元素的 StartTag token 已被消费。
func (p *pageLexer) spanElement(start int) ([]byte, error) {
	depth := 1
	for {
		tt, _, _ := p.next()
		switch tt {
		case txml.ErrorToken:
			return nil, p.err()
		case txml.StartTagToken:
			depth++
		case txml.EndTagToken:
			depth--
			if depth == 0 {
				return p.data[start:p.abs], nil
			}
		case txml.StartTagCloseVoidToken:
			depth--
			if depth == 0 {
				return p.data[start:p.abs], nil
			}
		}
	}
}

// decodeSpan 捕获 start 起的元素字节区间，交给 encoding/xml 完整解码。
func (p *pageLexer) decodeSpan(start int, dst any) error {
	span, err := p.spanElement(start)
	if err != nil {
		return err
	}
	if err := xml.NewDecoder(bytes.NewReader(span)).Decode(dst); err != nil {
		name := targetName(span)
		pre := span
		if len(pre) > 64 {
			pre = pre[:64]
		}
		return fmt.Errorf("解析 XML 失败: 元素 %s 区间 [%d:%d] %q: %w", name, start, start+len(span), pre, err)
	}
	return nil
}

// readAttrs 消费开始标签后的属性序列，直到 '>' 或自闭合 '/>'。
func (p *pageLexer) readAttrs(h func(name string, value []byte) error) (void bool, err error) {
	for {
		tt, _, _ := p.next()
		switch tt {
		case txml.AttributeToken:
			if err := h(string(localName(p.lx.Text())), unquote(p.lx.AttrVal())); err != nil {
				return false, err
			}
		case txml.StartTagCloseToken:
			return false, nil
		case txml.StartTagCloseVoidToken:
			return true, nil
		case txml.ErrorToken:
			return false, p.err()
		}
	}
}

func (p *pageLexer) skipAttrs() (bool, error) {
	return p.readAttrs(func(string, []byte) error { return nil })
}

// ParsePageContentXML 解析 OFD 页面内容 XML，入口语义与 xml.Unmarshal 一致：
// 第一个元素作为根元素跳过，子元素按名称匹配。
func ParsePageContentXML(data []byte) (*PageContent, error) {
	if bytes.HasPrefix(data, utf8BOM) {
		data = data[len(utf8BOM):]
	} else if bytes.HasPrefix(data, utf16LEBOM) || bytes.HasPrefix(data, utf16BEBOM) {
		// UTF-16 编码的页面内容极罕见，回退到标准库。
		var pc PageContent
		if err := xml.Unmarshal(data, &pc); err != nil {
			return nil, err
		}
		return &pc, nil
	}
	pc := &PageContent{}
	r := newPageLexer(data)

	// 跳过 XML 声明等前导 token，直到根元素。
	for {
		tt, _, start := r.next()
		switch tt {
		case txml.ErrorToken:
			if errors.Is(r.lx.Err(), io.EOF) {
				return pc, nil
			}
			return nil, r.err()
		case txml.StartTagToken:
			void, err := r.skipAttrs()
			if err != nil {
				return nil, err
			}
			if void {
				return pc, nil
			}
			_ = start
			goto children
		}
	}

children:
	for {
		tt, buf, start := r.next()
		switch tt {
		case txml.ErrorToken:
			if errors.Is(r.lx.Err(), io.EOF) {
				return pc, nil
			}
			return nil, r.err()
		case txml.StartTagToken:
			name := string(targetName(buf))
			switch name {
			case "Template":
				var t Template
				if err := r.decodeSpan(start, &t); err != nil {
					return nil, err
				}
				pc.Template = append(pc.Template, t)
			case "PageRes":
				var loc StLoc
				if err := r.decodeSpan(start, &loc); err != nil {
					return nil, err
				}
				pc.PageRes = append(pc.PageRes, loc)
			case "Area":
				area := CtPageArea{}
				if err := r.decodeSpan(start, &area); err != nil {
					return nil, err
				}
				pc.Area = &area
			case "Content":
				content := Content{}
				if err := r.parseContent(&content); err != nil {
					return nil, err
				}
				pc.Content = &content
			case "Actions":
				var a Actions
				if err := r.decodeSpan(start, &a); err != nil {
					return nil, err
				}
				pc.Actions = &a
			default:
				// 未知顶层元素整棵跳过。
				if _, err := r.spanElement(start); err != nil {
					return nil, err
				}
			}
		}
	}
}

func (p *pageLexer) parseContent(content *Content) error {
	for {
		tt, buf, start := p.next()
		switch tt {
		case txml.ErrorToken:
			return p.err()
		case txml.EndTagToken:
			return nil
		case txml.StartTagToken:
			if string(targetName(buf)) != "Layer" {
				if _, err := p.spanElement(start); err != nil {
					return err
				}
				continue
			}
			layer := &Layer{}
			if err := p.parseLayer(layer); err != nil {
				return err
			}
			content.Layer = append(content.Layer, layer)
		}
	}
}

// parseLayer 解析 <Layer> 属性与图层内的页面对象。
func (p *pageLexer) parseLayer(layer *Layer) error {
	void, err := p.readAttrs(func(name string, val []byte) error {
		switch name {
		case "ID":
			_ = layer.ID.UnmarshalText(val)
		case "Type":
			layer.Type = string(val)
		case "DrawParam":
			var id StID
			_ = id.UnmarshalText(val)
			layer.DrawParam = StRefID(id)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if void {
		return nil
	}
	items, err := p.parsePageItems("Layer")
	if err != nil {
		return err
	}
	layer.CTPageBlock.Items = items
	return nil
}

// parsePageItems 解析页面对象序列，直到遇到 endName 的结束标记。
func (p *pageLexer) parsePageItems(endName string) ([]PageItem, error) {
	var items []PageItem
	for {
		tt, buf, start := p.next()
		switch tt {
		case txml.ErrorToken:
			return nil, p.err()
		case txml.EndTagToken:
			if string(endTagName(buf)) == endName {
				return items, nil
			}
			return nil, fmt.Errorf("解析页面对象失败: 多余的结束标记 %s", buf)
		case txml.StartTagToken:
			name := string(targetName(buf))
			var item PageItem
			switch name {
			case "TextObject":
				o, err := p.parseTextObject()
				if err != nil {
					return nil, err
				}
				item.Kind = PageItemText
				item.Text = o
			case "PathObject":
				o, err := p.parsePathObject()
				if err != nil {
					return nil, err
				}
				item.Kind = PageItemPath
				item.Path = o
			case "ImageObject":
				o, err := p.parseImageObject()
				if err != nil {
					return nil, err
				}
				item.Kind = PageItemImage
				item.Image = o
			case "CompositeObject":
				o, err := p.parseCompositeObject()
				if err != nil {
					return nil, err
				}
				item.Kind = PageItemComposite
				item.Composite = o
			case "PageBlock":
				o, err := p.parsePageBlock()
				if err != nil {
					return nil, err
				}
				item.Kind = PageItemBlock
				item.Block = o
			default:
				if _, err := p.spanElement(start); err != nil {
					return nil, err
				}
				continue
			}
			items = append(items, item)
		}
	}
}

func (p *pageLexer) parsePageBlock() (*PageBlock, error) {
	o := &PageBlock{}
	void, err := p.readAttrs(func(name string, val []byte) error {
		if name == "ID" {
			_ = o.ID.UnmarshalText(val)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if void {
		return nil, nil
	}
	items, err := p.parsePageItems("PageBlock")
	if err != nil {
		return nil, err
	}
	o.CTPageBlock.Items = items
	return o, nil
}

// applyGraphicAttr 应用图元通用属性，与 CTGraphicUnit 的 xml 标签一致。
func applyGraphicAttr(g *CTGraphicUnit, name string, val []byte) error {
	switch name {
	case "Boundary":
		return g.Boundary.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
	case "Name":
		g.Name = string(val)
	case "Visible":
		return g.Visible.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
	case "CTM":
		if g.CTM == nil {
			g.CTM = new(CTM)
		}
		return g.CTM.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
	case "DrawParam":
		return g.DrawParam.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
	case "LineWidth":
		v, err := decodeFloatAttr(string(val), "LineWidth")
		if err != nil {
			return err
		}
		g.LineWidth = v
	case "Cap":
		g.Cap = string(val)
	case "Join":
		g.Join = string(val)
	case "MiterLimit":
		v, err := decodeFloatAttr(string(val), "MiterLimit")
		if err != nil {
			return err
		}
		g.MiterLimit = v
	case "DashOffset":
		v, err := decodeFloatAttr(string(val), "DashOffset")
		if err != nil {
			return err
		}
		g.DashOffset = v
	case "DashPattern":
		if g.DashPattern == nil {
			g.DashPattern = new(StArrayF)
		}
		return g.DashPattern.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
	case "Alpha":
		v, err := decodeAlphaAttr(string(val))
		if err != nil {
			return err
		}
		g.Alpha = v
	}
	return nil
}

// parseGraphicAttrs 解析图元通用属性与对象自身属性（单次属性扫描），返回是否自闭合。
func (p *pageLexer) parseObjectAttrs(g *CTGraphicUnit, extra func(name string, val []byte) error) (bool, error) {
	return p.readAttrs(func(name string, val []byte) error {
		if err := applyGraphicAttr(g, name, val); err != nil {
			return err
		}
		if extra != nil {
			return extra(name, val)
		}
		return nil
	})
}

func (p *pageLexer) parseTextObject() (*TextObject, error) {
	o := &TextObject{}
	void, err := p.parseObjectAttrs(&o.CTGraphicUnit, func(name string, val []byte) error {
		switch name {
		case "ID":
			_ = o.ID.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
		case "Font":
			_ = o.Font.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
		case "Size":
			v, err := decodeFloatAttr(string(val), "Size")
			if err != nil {
				return err
			}
			o.Size = v
		case "Stroke":
			v, err := decodeBoolAttr(string(val), "Stroke")
			if err != nil {
				return err
			}
			o.Stroke = v
		case "Fill":
			return o.Fill.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
		case "HScale":
			v, err := decodeFloatAttr(string(val), "HScale")
			if err != nil {
				return err
			}
			o.HScale = v
		case "ReadDirection":
			v, err := decodeIntAttr(string(val), "ReadDirection")
			if err != nil {
				return err
			}
			o.ReadDirection = v
		case "CharDirection":
			v, err := decodeIntAttr(string(val), "CharDirection")
			if err != nil {
				return err
			}
			o.CharDirection = v
		case "Weight":
			v, err := decodeIntAttr(string(val), "Weight")
			if err != nil {
				return err
			}
			o.Weight = v
		case "Italic":
			v, err := decodeBoolAttr(string(val), "Italic")
			if err != nil {
				return err
			}
			o.Italic = v
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if void {
		o.CTGraphicUnit.normalizeDrawParams()
		return o, nil
	}

	for {
		tt, buf, start := p.next()
		switch tt {
		case txml.ErrorToken:
			return nil, p.err()
		case txml.EndTagToken:
			o.CTGraphicUnit.normalizeDrawParams()
			return o, nil
		case txml.StartTagToken:
			name := string(targetName(buf))
			switch name {
			case "FillColor":
				var c CTColor
				if err := p.decodeSpan(start, &c); err != nil {
					return nil, err
				}
				o.FillColor = &c
			case "StrokeColor":
				var c CTColor
				if err := p.decodeSpan(start, &c); err != nil {
					return nil, err
				}
				o.StrokeColor = &c
			case "TextCode":
				var tc TextCode
				if err := p.parseTextCode(&tc); err != nil {
					return nil, err
				}
				o.TextCode = append(o.TextCode, tc)
			case "CGTransform":
				var g CTCGTransform
				if err := p.parseCGTransform(&g); err != nil {
					return nil, err
				}
				o.CGTransform = append(o.CGTransform, g)
			case "Actions":
				var a Actions
				if err := p.decodeSpan(start, &a); err != nil {
					return nil, err
				}
				o.Actions = &a
			case "Clips":
				var c Clips
				if err := p.decodeSpan(start, &c); err != nil {
					return nil, err
				}
				o.Clips = &c
			case "DrawParam", "LineWidth", "Cap", "Join", "MiterLimit", "DashOffset", "DashPattern":
				if err := p.parseGraphicChildElement(&o.CTGraphicUnit, name); err != nil {
					return nil, err
				}
			default:
				if _, err := p.spanElement(start); err != nil {
					return nil, err
				}
			}
		}
	}
}

// parseGraphicChildElement 解析以子元素形式编码的图元通用属性。
func (p *pageLexer) parseGraphicChildElement(g *CTGraphicUnit, name string) error {
	var sb strings.Builder
	for {
		tt, buf, start := p.next()
		switch tt {
		case txml.TextToken:
			sb.Write(buf)
		case txml.CDATAToken:
			sb.Write(buf)
		case txml.StartTagToken:
			if _, err := p.spanElement(start); err != nil {
				return err
			}
		case txml.EndTagToken:
			text := []byte(unescapeXMLText([]byte(sb.String())))
			switch name {
			case "DrawParam":
				var id StID
				_ = id.UnmarshalText(text)
				g.DrawParamElement = &id
			case "LineWidth":
				v, err := decodeFloatAttr(string(text), "LineWidth")
				if err != nil {
					return err
				}
				g.LineWidthElement = &v
			case "Cap":
				s := string(text)
				g.CapElement = &s
			case "Join":
				s := string(text)
				g.JoinElement = &s
			case "MiterLimit":
				v, err := decodeFloatAttr(string(text), "MiterLimit")
				if err != nil {
					return err
				}
				g.MiterLimitElement = &v
			case "DashOffset":
				v, err := decodeFloatAttr(string(text), "DashOffset")
				if err != nil {
					return err
				}
				g.DashOffsetElement = &v
			case "DashPattern":
				var a StArrayF
				if err := a.UnmarshalXMLAttr(xml.Attr{Value: string(text)}); err != nil {
					return err
				}
				g.DashPatternElement = &a
			}
			return nil
		case txml.ErrorToken:
			return p.err()
		}
	}
}

// parseTextCode 解析 <TextCode> 元素。
func (p *pageLexer) parseTextCode(tc *TextCode) error {
	void, err := p.readAttrs(func(name string, val []byte) error {
		switch name {
		case "X":
			v, err := decodeFloatAttr(string(val), "X")
			if err != nil {
				return err
			}
			tc.X = v
		case "Y":
			v, err := decodeFloatAttr(string(val), "Y")
			if err != nil {
				return err
			}
			tc.Y = v
		case "DeltaX":
			return tc.DeltaX.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
		case "DeltaY":
			return tc.DeltaY.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
		}
		return nil
	})
	if err != nil {
		return err
	}
	if void {
		return nil
	}
	var sb strings.Builder
	for {
		tt, buf, start := p.next()
		switch tt {
		case txml.ErrorToken:
			return p.err()
		case txml.TextToken:
			sb.WriteString(unescapeXMLText(buf))
		case txml.CDATAToken:
			sb.Write(buf)
		case txml.StartTagToken:
			if _, err := p.spanElement(start); err != nil {
				return err
			}
		case txml.EndTagToken:
			tc.Value = sb.String()
			return nil
		}
	}
}

func (p *pageLexer) parseCGTransform(g *CTCGTransform) error {
	void, err := p.readAttrs(func(name string, val []byte) error {
		switch name {
		case "CodePosition":
			v, err := decodeIntAttr(string(val), "CodePosition")
			if err != nil {
				return err
			}
			g.CodePosition = v
		case "CodeCount":
			v, err := decodeIntAttr(string(val), "CodeCount")
			if err != nil {
				return err
			}
			g.CodeCount = v
		case "GlyphCount":
			v, err := decodeIntAttr(string(val), "GlyphCount")
			if err != nil {
				return err
			}
			g.GlyphCount = v
		}
		return nil
	})
	if err != nil {
		return err
	}
	if void {
		return nil
	}
	for {
		tt, buf, start := p.next()
		switch tt {
		case txml.ErrorToken:
			return p.err()
		case txml.EndTagToken:
			return nil
		case txml.StartTagToken:
			if string(targetName(buf)) != "Glyphs" {
				if _, err := p.spanElement(start); err != nil {
					return err
				}
				continue
			}
			if err := p.decodeSpan(start, &g.Glyphs); err != nil {
				return err
			}
		}
	}
}

func (p *pageLexer) parsePathObject() (*PathObject, error) {
	o := &PathObject{}
	void, err := p.parseObjectAttrs(&o.CTGraphicUnit, func(name string, val []byte) error {
		switch name {
		case "ID":
			_ = o.ID.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
		case "Stroke":
			return o.Stroke.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
		case "Fill":
			v, err := decodeBoolAttr(string(val), "Fill")
			if err != nil {
				return err
			}
			o.Fill = v
		case "Rule":
			o.Rule = string(val)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if void {
		o.CTGraphicUnit.normalizeDrawParams()
		return o, nil
	}

	for {
		tt, buf, start := p.next()
		switch tt {
		case txml.ErrorToken:
			return nil, p.err()
		case txml.EndTagToken:
			o.CTGraphicUnit.normalizeDrawParams()
			return o, nil
		case txml.StartTagToken:
			name := string(targetName(buf))
			switch name {
			case "StrokeColor":
				var c CTColor
				if err := p.decodeSpan(start, &c); err != nil {
					return nil, err
				}
				o.StrokeColor = &c
			case "FillColor":
				var c CTColor
				if err := p.decodeSpan(start, &c); err != nil {
					return nil, err
				}
				o.FillColor = &c
			case "AbbreviatedData":
				data, err := p.parseElementText()
				if err != nil {
					return nil, err
				}
				if err := o.AbbreviatedData.UnmarshalXMLText(data); err != nil {
					return nil, err
				}
			case "Actions":
				var a Actions
				if err := p.decodeSpan(start, &a); err != nil {
					return nil, err
				}
				o.Actions = &a
			case "Clips":
				var c Clips
				if err := p.decodeSpan(start, &c); err != nil {
					return nil, err
				}
				o.Clips = &c
			case "DrawParam", "LineWidth", "Cap", "Join", "MiterLimit", "DashOffset", "DashPattern":
				if err := p.parseGraphicChildElement(&o.CTGraphicUnit, name); err != nil {
					return nil, err
				}
			default:
				if _, err := p.spanElement(start); err != nil {
					return nil, err
				}
			}
		}
	}
}

func (p *pageLexer) parseImageObject() (*ImageObject, error) {
	o := &ImageObject{}
	void, err := p.parseObjectAttrs(&o.CTGraphicUnit, func(name string, val []byte) error {
		switch name {
		case "ID":
			_ = o.ID.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
		case "ResourceID":
			_ = o.ResourceID.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
		case "Substitution":
			_ = o.Substitution.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
		case "ImageMask":
			_ = o.ImageMask.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if void {
		o.CTGraphicUnit.normalizeDrawParams()
		return o, nil
	}

	for {
		tt, buf, start := p.next()
		switch tt {
		case txml.ErrorToken:
			return nil, p.err()
		case txml.EndTagToken:
			o.CTGraphicUnit.normalizeDrawParams()
			return o, nil
		case txml.StartTagToken:
			name := string(targetName(buf))
			switch name {
			case "Border":
				var b Border
				if err := p.decodeSpan(start, &b); err != nil {
					return nil, err
				}
				o.Border = &b
			case "Actions":
				var a Actions
				if err := p.decodeSpan(start, &a); err != nil {
					return nil, err
				}
				o.Actions = &a
			case "Clips":
				var c Clips
				if err := p.decodeSpan(start, &c); err != nil {
					return nil, err
				}
				o.Clips = &c
			case "DrawParam", "LineWidth", "Cap", "Join", "MiterLimit", "DashOffset", "DashPattern":
				if err := p.parseGraphicChildElement(&o.CTGraphicUnit, name); err != nil {
					return nil, err
				}
			default:
				if _, err := p.spanElement(start); err != nil {
					return nil, err
				}
			}
		}
	}
}

func (p *pageLexer) parseCompositeObject() (*CompositeObject, error) {
	o := &CompositeObject{}
	void, err := p.parseObjectAttrs(&o.CTGraphicUnit, func(name string, val []byte) error {
		switch name {
		case "ID":
			_ = o.ID.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
		case "ResourceID":
			_ = o.ResourceID.UnmarshalXMLAttr(xml.Attr{Value: string(val)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if void {
		o.CTGraphicUnit.normalizeDrawParams()
		return o, nil
	}

	for {
		tt, buf, start := p.next()
		switch tt {
		case txml.ErrorToken:
			return nil, p.err()
		case txml.EndTagToken:
			o.CTGraphicUnit.normalizeDrawParams()
			return o, nil
		case txml.StartTagToken:
			name := string(targetName(buf))
			switch name {
			case "Actions":
				var a Actions
				if err := p.decodeSpan(start, &a); err != nil {
					return nil, err
				}
				o.Actions = &a
			case "Clips":
				var c Clips
				if err := p.decodeSpan(start, &c); err != nil {
					return nil, err
				}
				o.Clips = &c
			case "DrawParam", "LineWidth", "Cap", "Join", "MiterLimit", "DashOffset", "DashPattern":
				if err := p.parseGraphicChildElement(&o.CTGraphicUnit, name); err != nil {
					return nil, err
				}
			default:
				if _, err := p.spanElement(start); err != nil {
					return nil, err
				}
			}
		}
	}
}

// parseElementText 读取元素文本并返回，消费到匹配的结束标记。
func (p *pageLexer) parseElementText() ([]byte, error) {
	var sb strings.Builder
	for {
		tt, buf, start := p.next()
		switch tt {
		case txml.ErrorToken:
			return nil, p.err()
		case txml.TextToken:
			sb.Write(buf)
		case txml.CDATAToken:
			sb.Write(buf)
		case txml.StartTagToken:
			if _, err := p.spanElement(start); err != nil {
				return nil, err
			}
		case txml.EndTagToken:
			return []byte(sb.String()), nil
		}
	}
}

// UnmarshalXMLText 从元素文本解析路径数据，供快速路径解析 AbbreviatedData 使用。
func (p *SVGPath) UnmarshalXMLText(data []byte) error {
	text := unescapeXMLText(data)
	text = strings.TrimSpace(text)
	if text == "" {
		*p = SVGPath{}
		return nil
	}
	commands, err := p.parsePathData(text)
	if err != nil {
		return fmt.Errorf("路径数据解析失败: %w", err)
	}
	*p = commands
	return nil
}

// unescapeXMLText 反解码 XML 文本实体，无实体时零拷贝返回。
func unescapeXMLText(b []byte) string {
	if bytes.IndexByte(b, '&') < 0 {
		return string(b)
	}
	var sb strings.Builder
	for i := 0; i < len(b); i++ {
		if b[i] != '&' {
			sb.WriteByte(b[i])
			continue
		}
		j := bytes.IndexByte(b[i:], ';')
		if j < 0 || j > 16 {
			sb.WriteByte(b[i])
			continue
		}
		ent := b[i+1 : i+j]
		if r, ok := xmlEntity(ent); ok {
			sb.WriteRune(r)
		} else {
			sb.Write(b[i : i+j+1])
		}
		i += j
	}
	return sb.String()
}

// xmlEntity 识别 XML 预定义实体与数字字符引用。
func xmlEntity(ent []byte) (rune, bool) {
	switch string(ent) {
	case "amp":
		return '&', true
	case "lt":
		return '<', true
	case "gt":
		return '>', true
	case "quot":
		return '"', true
	case "apos":
		return '\'', true
	}
	if len(ent) > 1 && ent[0] == '#' {
		s := ent[1:]
		base := 10
		if len(s) > 0 && (s[0] == 'x' || s[0] == 'X') {
			s = s[1:]
			base = 16
		}
		if v, err := strconv.ParseUint(string(s), base, 32); err == nil {
			return rune(v), true
		}
	}
	return 0, false
}
