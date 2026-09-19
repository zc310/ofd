package mdimport

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"go.yaml.in/yaml/v3"

	"github.com/zc310/ofd/internal/layout"
)

// mdParser 把 goldmark AST 转换为与版式无关的 layout.Document。
type mdParser struct {
	source  []byte
	baseDir string
}

// frontMatterLetterhead 是 front matter 中 letterhead 的字段定义。
type frontMatterLetterhead struct {
	Org       string  `yaml:"org"`
	DocNo     string  `yaml:"doc_no"`
	Signatory string  `yaml:"signatory"`
	SerialNo  string  `yaml:"serial_no"`
	Security  string  `yaml:"security"`
	Urgency   string  `yaml:"urgency"`
	Height    float64 `yaml:"height"`
	OrgSize   float64 `yaml:"org_size"`
	DocNoSize float64 `yaml:"doc_no_size"`
}

// frontMatterFooter 是 front matter 中 footer 的字段定义。
type frontMatterFooter struct {
	PageNumber bool    `yaml:"page_number"`
	Size       float64 `yaml:"size"`
}

// frontMatterColophon 是 front matter 中 colophon 的字段定义。
type frontMatterColophon struct {
	Cc         string  `yaml:"cc"`
	IssuedBy   string  `yaml:"issued_by"`
	IssuedDate string  `yaml:"issued_date"`
	Size       float64 `yaml:"size"`
}

// frontMatterSign 是 front matter 中 sign（落款）的字段定义。
type frontMatterSign struct {
	Org  string  `yaml:"org"`
	Date string  `yaml:"date"`
	Size float64 `yaml:"size"`
}

type frontMatter struct {
	Letterhead *frontMatterLetterhead `yaml:"letterhead"`
	Sign       *frontMatterSign       `yaml:"sign"`
	Footer     *frontMatterFooter     `yaml:"footer"`
	Colophon   *frontMatterColophon   `yaml:"colophon"`
}

// splitFrontMatter 提取文档开头的 ---...--- YAML 头；没有可用的 letterhead、
// sign、footer 或 colophon 时返回原始内容，避免破坏以 --- 开头的普通 Markdown。
func splitFrontMatter(source []byte) ([]byte, *layout.Letterhead, *layout.Signature, *layout.Footer, *layout.Colophon) {
	lines := strings.Split(string(source), "\n")
	if len(lines) < 3 || strings.TrimSuffix(lines[0], "\r") != "---" {
		return source, nil, nil, nil, nil
	}
	end := -1
	for index := 1; index < len(lines); index++ {
		if strings.TrimSuffix(lines[index], "\r") == "---" {
			end = index
			break
		}
	}
	if end <= 1 {
		return source, nil, nil, nil, nil
	}
	var front frontMatter
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &front); err != nil {
		return source, nil, nil, nil, nil
	}
	body := []byte(strings.Join(lines[end+1:], "\n"))
	if front.Letterhead == nil && front.Sign == nil && front.Footer == nil && front.Colophon == nil {
		return source, nil, nil, nil, nil
	}
	var heading *layout.Letterhead
	if front.Letterhead != nil {
		h := front.Letterhead
		heading = &layout.Letterhead{
			Org:       h.Org,
			DocNo:     h.DocNo,
			Signatory: h.Signatory,
			SerialNo:  h.SerialNo,
			Security:  h.Security,
			Urgency:   h.Urgency,
			Height:    h.Height,
			OrgSize:   h.OrgSize,
			DocNoSize: h.DocNoSize,
		}
	}
	var sign *layout.Signature
	if front.Sign != nil {
		s := front.Sign
		sign = &layout.Signature{Org: s.Org, Date: s.Date, Size: s.Size}
	}
	var footer *layout.Footer
	if front.Footer != nil {
		footer = &layout.Footer{PageNumber: front.Footer.PageNumber, Size: front.Footer.Size}
	}
	var colophon *layout.Colophon
	if front.Colophon != nil {
		c := front.Colophon
		colophon = &layout.Colophon{Cc: c.Cc, IssuedBy: c.IssuedBy, IssuedDate: c.IssuedDate, Size: c.Size}
	}
	return body, heading, sign, footer, colophon
}

func parseMarkdown(source []byte, baseDir string) (*layout.Document, error) {
	body, letterhead, sign, footer, colophon := splitFrontMatter(source)
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	root := md.Parser().Parse(text.NewReader(body))
	if root == nil {
		return &layout.Document{Letterhead: letterhead, Sign: sign, Footer: footer, Colophon: colophon}, nil
	}
	parser := &mdParser{source: body, baseDir: baseDir}
	return &layout.Document{
		Title:      parser.documentTitle(root),
		Letterhead: letterhead,
		Sign:       sign,
		Footer:     footer,
		Colophon:   colophon,
		Blocks:     parser.blocks(root, 0),
	}, nil
}

func (p *mdParser) documentTitle(root ast.Node) string {
	for child := root.FirstChild(); child != nil; child = child.NextSibling() {
		if heading, ok := child.(*ast.Heading); ok {
			return p.plainText(heading)
		}
	}
	return ""
}

func (p *mdParser) blocks(parent ast.Node, indent int) []layout.Block {
	var result []layout.Block
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		result = append(result, p.block(child, indent)...)
	}
	return result
}

func (p *mdParser) block(node ast.Node, indent int) []layout.Block {
	switch value := node.(type) {
	case *ast.Heading:
		return []layout.Block{{
			Kind:    layout.KindHeading,
			Level:   value.Level,
			Inlines: p.inlines(node, false, false, false),
		}}
	case *ast.Paragraph, *ast.TextBlock:
		if image, ok := p.standaloneImage(node); ok {
			return []layout.Block{{Kind: layout.KindImage, Image: image}}
		}
		return []layout.Block{{
			Kind:    layout.KindParagraph,
			Inlines: p.inlines(node, false, false, false),
		}}
	case *ast.List:
		return p.list(value, indent)
	case *ast.Blockquote:
		return p.blockquote(value, indent)
	case *ast.FencedCodeBlock, *ast.CodeBlock:
		return []layout.Block{p.codeBlock(node)}
	case *ast.ThematicBreak:
		return []layout.Block{{Kind: layout.KindThematicBreak}}
	case *extast.Table:
		return []layout.Block{{Kind: layout.KindTable, Table: p.table(value)}}
	case *ast.HTMLBlock:
		// 原始 HTML 不做渲染，避免引入 HTML 布局引擎。
		return nil
	default:
		return nil
	}
}

func (p *mdParser) list(list *ast.List, indent int) []layout.Block {
	var result []layout.Block
	number := list.Start
	if number == 0 {
		number = 1
	}
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		if _, ok := item.(*ast.ListItem); !ok {
			continue
		}
		marker := "•"
		if list.IsOrdered() {
			marker = fmt.Sprintf("%d.", number)
			number++
		}
		itemBlocks := p.blocks(item, indent+1)
		attached := false
		for index := range itemBlocks {
			if !attached && (itemBlocks[index].Kind == layout.KindParagraph || itemBlocks[index].Kind == layout.KindHeading) {
				itemBlocks[index].Marker = marker
				itemBlocks[index].Indent = indent + 1
				attached = true
			}
		}
		if !attached && len(itemBlocks) == 0 {
			itemBlocks = append(itemBlocks, layout.Block{Kind: layout.KindParagraph, Marker: marker, Indent: indent + 1})
		}
		result = append(result, itemBlocks...)
	}
	return result
}

func (p *mdParser) blockquote(quote *ast.Blockquote, indent int) []layout.Block {
	inner := p.blocks(quote, indent+1)
	for index := range inner {
		inner[index].Indent = indent + 1
		if inner[index].Kind == layout.KindParagraph {
			inner[index].Kind = layout.KindQuote
		}
	}
	return inner
}

func (p *mdParser) table(table *extast.Table) *layout.Table {
	result := &layout.Table{}
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		switch value := row.(type) {
		case *extast.TableHeader:
			for cell := value.FirstChild(); cell != nil; cell = cell.NextSibling() {
				tableCell, ok := cell.(*extast.TableCell)
				if !ok {
					continue
				}
				result.Header = append(result.Header, layout.Cell(p.inlines(tableCell, true, false, false)))
				result.Align = append(result.Align, alignmentOf(tableCell.Alignment))
			}
		case *extast.TableRow:
			var cells []layout.Cell
			for cell := value.FirstChild(); cell != nil; cell = cell.NextSibling() {
				tableCell, ok := cell.(*extast.TableCell)
				if !ok {
					continue
				}
				cells = append(cells, layout.Cell(p.inlines(tableCell, false, false, false)))
			}
			result.Rows = append(result.Rows, cells)
		}
	}
	return result
}

func alignmentOf(alignment extast.Alignment) layout.Align {
	switch alignment {
	case extast.AlignCenter:
		return layout.AlignCenter
	case extast.AlignRight:
		return layout.AlignRight
	default:
		return layout.AlignLeft
	}
}

func (p *mdParser) codeBlock(node ast.Node) layout.Block {
	return layout.Block{Kind: layout.KindCode, Code: p.codeText(node)}
}

func (p *mdParser) codeText(block ast.Node) string {
	type lineBlock interface {
		Lines() *text.Segments
	}
	value, ok := block.(lineBlock)
	if !ok {
		return ""
	}
	lines := value.Lines()
	var builder strings.Builder
	for index := 0; index < lines.Len(); index++ {
		segment := lines.At(index)
		builder.Write(segment.Value(p.source))
		builder.WriteByte('\n')
	}
	return strings.TrimRight(builder.String(), "\n")
}

func (p *mdParser) standaloneImage(node ast.Node) (*layout.Image, bool) {
	count := 0
	var image *ast.Image
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch value := child.(type) {
		case *ast.Image:
			count++
			image = value
		case *ast.Text:
			if strings.TrimSpace(string(value.Segment.Value(p.source))) != "" {
				return nil, false
			}
		default:
			return nil, false
		}
	}
	if count != 1 || image == nil {
		return nil, false
	}
	return p.loadImage(image)
}

// loadImage 读取本地或内联图片。远程图片按设计不下载，只记录警告并跳过。
func (p *mdParser) loadImage(node *ast.Image) (*layout.Image, bool) {
	source := string(node.Destination)
	if source == "" {
		return nil, false
	}
	if isRemote(source) {
		slog.Warn("跳过远程图片", "source", source)
		return nil, false
	}
	data, format, width, height, err := loadLocalImage(source, p.baseDir)
	if err != nil {
		slog.Warn("跳过无法读取的图片", "source", source, "error", err)
		return nil, false
	}
	return &layout.Image{
		Source:      source,
		Alt:         p.plainText(node),
		Data:        data,
		Format:      format,
		PixelWidth:  width,
		PixelHeight: height,
	}, true
}

func (p *mdParser) inlines(parent ast.Node, bold, italic, strike bool) []layout.Inline {
	var result []layout.Inline
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		result = append(result, p.inline(child, bold, italic, strike)...)
	}
	return result
}

func (p *mdParser) inline(node ast.Node, bold, italic, strike bool) []layout.Inline {
	switch value := node.(type) {
	case *ast.Text:
		var result []layout.Inline
		if content := string(value.Segment.Value(p.source)); content != "" {
			result = append(result, layout.Inline{Text: content, Bold: bold, Italic: italic, Strike: strike})
		}
		if value.HardLineBreak() {
			result = append(result, layout.Inline{Text: "\n"})
		} else if value.SoftLineBreak() {
			result = append(result, layout.Inline{Text: " "})
		}
		return result
	case *ast.String:
		return []layout.Inline{{Text: string(value.Value), Bold: bold, Italic: italic, Strike: strike}}
	case *ast.CodeSpan:
		return []layout.Inline{{Text: p.plainText(value), Code: true, Strike: strike}}
	case *ast.Emphasis:
		if value.Level >= 2 {
			return p.inlines(value, true, italic, strike)
		}
		return p.inlines(value, bold, true, strike)
	case *extast.Strikethrough:
		return p.inlines(value, bold, italic, true)
	case *ast.Link:
		children := p.inlines(value, bold, italic, strike)
		for index := range children {
			children[index].Link = true
		}
		if len(children) == 0 {
			children = []layout.Inline{{Text: string(value.Destination), Link: true}}
		}
		return children
	case *ast.AutoLink:
		return []layout.Inline{{Text: string(value.URL(p.source)), Bold: bold, Italic: italic, Strike: strike, Link: true}}
	case *ast.Image:
		alt := p.plainText(value)
		if alt == "" {
			alt = string(value.Destination)
		}
		return []layout.Inline{{Text: alt, Bold: bold, Italic: italic, Strike: strike}}
	case *extast.TaskCheckBox:
		if value.IsChecked {
			return []layout.Inline{{Text: "[x] "}}
		}
		return []layout.Inline{{Text: "[ ] "}}
	case *ast.RawHTML:
		return nil
	default:
		return p.inlines(node, bold, italic, strike)
	}
}

func (p *mdParser) plainText(node ast.Node) string {
	var builder strings.Builder
	p.appendText(&builder, node)
	return strings.TrimSpace(builder.String())
}

func (p *mdParser) appendText(builder *strings.Builder, node ast.Node) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch value := child.(type) {
		case *ast.Text:
			builder.Write(value.Segment.Value(p.source))
		case *ast.String:
			builder.Write(value.Value)
		case *ast.AutoLink:
			builder.Write(value.URL(p.source))
		case *extast.TaskCheckBox:
			// 任务框不参与纯文本。
		default:
			p.appendText(builder, child)
		}
	}
}
