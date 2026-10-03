package models

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"

	txml "github.com/tdewolff/parse/v2/xml"

	"github.com/zc310/ofd/internal/fastxml"
)

// ParseDocumentXML 解析 OFD 文档主体 XML。页面列表（Pages/Page）是打开文档时
// 解析量最大的部分——大文档动辄上千个仅含 ID/BaseLoc 的 Page 元素，用
// encoding/xml 反射逐元素 unmarshal 会产生大量分配；这里直接用 tdewolff 的
// 零分配 XML lexer 重建。其余低频且复杂的子树（CommonData、Outlines、颜色、
// 权限等）用 fastxml.SpanElement 捕获字节区间后回退 encoding/xml，语义与
// xml.Unmarshal 一致。
func ParseDocumentXML(data []byte) (*Document, error) {
	data, utf16 := fastxml.StripBOM(data)
	if utf16 {
		// UTF-16 编码的文档主体极罕见，回退到标准库。
		var doc Document
		if err := xml.Unmarshal(data, &doc); err != nil {
			return nil, err
		}
		return &doc, nil
	}

	doc := &Document{}
	r := newPageLexer(data)

	// 跳过 XML 声明等前导 token，直到根元素。
	for {
		tt, buf, _ := r.Next()
		switch tt {
		case txml.ErrorToken:
			if errors.Is(r.Err(), io.EOF) {
				return doc, nil
			}
			return nil, r.Err()
		case txml.StartTagToken:
			name := fastxml.TargetName(buf)
			if string(name) != "Document" {
				return nil, fmt.Errorf("解析 XML 失败: 根元素为 %s，期望 Document", name)
			}
			doc.XMLName = xml.Name{Local: "Document"}
			void, err := r.SkipAttrs()
			if err != nil {
				return nil, err
			}
			if void {
				return doc, nil
			}
			goto children
		}
	}

children:
	for {
		tt, buf, start := r.Next()
		switch tt {
		case txml.ErrorToken:
			if errors.Is(r.Err(), io.EOF) {
				return doc, nil
			}
			return nil, r.Err()
		case txml.StartTagToken:
			name := string(fastxml.TargetName(buf))
			switch name {
			case "CommonData":
				if err := r.DecodeSpan(start, &doc.CommonData); err != nil {
					return nil, err
				}
			case "Pages":
				if err := r.parsePageList(&doc.Pages); err != nil {
					return nil, err
				}
			case "Outlines":
				doc.Outlines = new(OutlineList)
				if err := r.DecodeSpan(start, doc.Outlines); err != nil {
					return nil, err
				}
			case "Permissions":
				doc.Permissions = new(CT_Permission)
				if err := r.DecodeSpan(start, doc.Permissions); err != nil {
					return nil, err
				}
			case "Actions":
				doc.Actions = new(ActionList)
				if err := r.DecodeSpan(start, doc.Actions); err != nil {
					return nil, err
				}
			case "VPreferences":
				doc.VPreferences = new(CT_VPreferences)
				if err := r.DecodeSpan(start, doc.VPreferences); err != nil {
					return nil, err
				}
			case "Bookmarks":
				doc.Bookmarks = new(BookmarkList)
				if err := r.DecodeSpan(start, doc.Bookmarks); err != nil {
					return nil, err
				}
			case "Annotations":
				doc.Annotations = new(StLoc)
				if err := r.DecodeSpan(start, doc.Annotations); err != nil {
					return nil, err
				}
			case "CustomTags":
				doc.CustomTags = new(StLoc)
				if err := r.DecodeSpan(start, doc.CustomTags); err != nil {
					return nil, err
				}
			case "Attachments":
				doc.Attachments = new(StLoc)
				if err := r.DecodeSpan(start, doc.Attachments); err != nil {
					return nil, err
				}
			case "Extensions":
				doc.Extensions = new(StLoc)
				if err := r.DecodeSpan(start, doc.Extensions); err != nil {
					return nil, err
				}
			default:
				// 未知顶层元素整棵跳过。
				if _, err := r.SpanElement(start); err != nil {
					return nil, err
				}
			}
		}
	}
}

// parsePageList 解析 <Pages> 下的 <Page ID BaseLoc/> 列表。页面数量与元素数
// 正相关，先按 <Page 出现次数预估容量，避免逐次 append 触发的切片翻倍；属性
// 扫描内联而不走 ReadAttrs 回调，避免每页一个闭包逃逸。
func (p *pageLexer) parsePageList(list *PageList) error {
	void, err := p.SkipAttrs()
	if err != nil {
		return err
	}
	if void {
		return nil
	}
	if list.Pages == nil {
		if capacity := bytes.Count(p.Data(), []byte("<Page")); capacity > 0 {
			list.Pages = make([]Page, 0, capacity)
		}
	}
	for {
		tt, buf, start := p.Next()
		switch tt {
		case txml.ErrorToken:
			return p.Err()
		case txml.EndTagToken:
			return nil
		case txml.StartTagToken:
			if string(fastxml.TargetName(buf)) != "Page" {
				if _, err := p.SpanElement(start); err != nil {
					return err
				}
				continue
			}
			page, void, err := p.parsePageAttrs()
			if err != nil {
				return err
			}
			if !void {
				// <Page> 规范上只有属性，出现子元素时跳过其内容。
				if err := p.skipElementBody("Page"); err != nil {
					return err
				}
			}
			list.Pages = append(list.Pages, page)
		}
	}
}

// parsePageAttrs 解析单个 <Page> 的属性，返回是否自闭合。
func (p *pageLexer) parsePageAttrs() (Page, bool, error) {
	var page Page
	for {
		tt, _, _ := p.Next()
		switch tt {
		case txml.AttributeToken:
			switch string(fastxml.LocalName(p.Text())) {
			case "ID":
				page.ID = parseStIDBytes(fastxml.Unquote(p.AttrVal()))
			case "BaseLoc":
				page.BaseLoc = StLoc(string(fastxml.Unquote(p.AttrVal())))
			}
		case txml.StartTagCloseToken:
			return page, false, nil
		case txml.StartTagCloseVoidToken:
			return page, true, nil
		case txml.ErrorToken:
			return page, false, p.Err()
		}
	}
}

// skipElementBody 消费当前元素的子内容，直到匹配 endName 的结束标记。
func (p *pageLexer) skipElementBody(endName string) error {
	for {
		tt, buf, start := p.Next()
		switch tt {
		case txml.ErrorToken:
			return p.Err()
		case txml.EndTagToken:
			if string(fastxml.EndTagName(buf)) == endName {
				return nil
			}
			return fmt.Errorf("解析 XML 失败: 多余的结束标记 %s", buf)
		case txml.StartTagToken:
			if _, err := p.SpanElement(start); err != nil {
				return err
			}
		}
	}
}

// parseStIDBytes 从十进制字节直接解析 StID，避免 UnmarshalText 的 string 转换
// 分配；非法输入回退到原有 CRC64 兜底逻辑。
func parseStIDBytes(b []byte) StID {
	if len(b) == 0 {
		return 0
	}
	var v uint64
	for _, c := range b {
		if c < '0' || c > '9' || v > (^uint64(0))/10 {
			var s StID
			return StID(s.parseUint(string(b)))
		}
		v = v*10 + uint64(c-'0')
	}
	return StID(v)
}
