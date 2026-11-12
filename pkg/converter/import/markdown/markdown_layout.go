package markdown

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	extast "github.com/yuin/goldmark/v2/extension/ast"
	"github.com/yuin/goldmark/v2/parser"
	"go.yaml.in/yaml/v4"

	"github.com/zc310/ofd/pkg/creator/flow"
)

// mdParser 只把 goldmark AST 转换为 flow 内容；测量、断行及分页由 flow 完成。
type mdParser struct {
	source  []byte
	baseDir string
	icons   map[string]flow.Icon
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
func splitFrontMatter(source []byte) ([]byte, *frontMatter) {
	lines := strings.Split(string(source), "\n")
	if len(lines) < 3 || strings.TrimSuffix(lines[0], "\r") != "---" {
		return source, nil
	}
	end := -1
	for index := 1; index < len(lines); index++ {
		if strings.TrimSuffix(lines[index], "\r") == "---" {
			end = index
			break
		}
	}
	if end <= 1 {
		return source, nil
	}
	var front frontMatter
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &front); err != nil {
		return source, nil
	}
	body := []byte(strings.Join(lines[end+1:], "\n"))
	if front.Letterhead == nil && front.Sign == nil && front.Footer == nil && front.Colophon == nil {
		return source, nil
	}
	return body, &front
}

func parseMarkdown(source []byte, baseDir string, options ...flow.Options) (*flow.Document, error) {
	body, front := splitFrontMatter(source)
	opts := markdownOptions()
	if len(options) > 1 {
		return nil, fmt.Errorf("%w：最多传入一份 flow.Options，实际 %d 份", errMarkdownContent, len(options))
	}
	if len(options) == 1 {
		opts = options[0]
	}
	if front != nil {
		var err error
		opts, err = front.options(opts)
		if err != nil {
			return nil, err
		}
	}
	document := flow.New(opts)
	document.Metadata.ID = "markdown"
	md := parser.New(parser.WithExtensions(extension.GFMParser))
	root := md.Parse(body)
	if root == nil {
		return document, nil
	}
	icons, err := loadAlertIcons()
	if err != nil {
		return nil, err
	}
	parser := &mdParser{source: body, baseDir: baseDir, icons: icons}
	document.Metadata.Title = parser.documentTitle(root)
	document.Blocks = parser.blocks(root, 0)
	if opts.Official != nil {
		// 保留旧导入器的主送识别规则，映射到 flow 的顶格主送及首页溢出迁移。
		// GB/T 9704—2012 §7.3.2：首个顶层正文段以冒号结束时按主送机关排布。
		for i := range document.Blocks {
			b := &document.Blocks[i]
			if b.Kind != "paragraph" || b.Indent != 0 {
				continue
			}
			var text strings.Builder
			for _, run := range b.Runs {
				text.WriteString(run.Text)
			}
			if value := strings.TrimSpace(text.String()); strings.HasSuffix(value, "：") || strings.HasSuffix(value, ":") {
				b.Kind = "recipient"
			}
			break
		}
	}
	return document, nil
}

func (p *mdParser) documentTitle(root ast.Node) string {
	for child := root.FirstChild(); child != nil; child = child.NextSibling() {
		if heading, ok := child.(*ast.Heading); ok {
			return p.plainText(heading)
		}
	}
	return ""
}

func (p *mdParser) blocks(parent ast.Node, indent int) []flow.Block {
	var result []flow.Block
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		result = append(result, p.block(child, indent)...)
	}
	return result
}

func (p *mdParser) block(node ast.Node, indent int) []flow.Block {
	switch value := node.(type) {
	case *ast.Heading:
		return []flow.Block{{
			Kind:   "heading",
			Level:  value.Level,
			Indent: float64(indent) * 6,
			Runs:   p.inlines(node, false, false, false),
		}}
	case *ast.Paragraph:
		if image, ok := p.standaloneImage(node); ok {
			return []flow.Block{{Kind: "image", Image: image, Indent: float64(indent) * 6}}
		}
		return []flow.Block{{
			Kind:   "paragraph",
			Indent: float64(indent) * 6,
			Runs:   p.inlines(node, false, false, false),
		}}
	case *ast.List:
		return p.list(value, indent)
	case *ast.Blockquote:
		return p.blockquote(value, indent)
	case *ast.CodeBlock:
		block := p.codeBlock(node)
		block.Indent = float64(indent) * 6
		return []flow.Block{block}
	case *ast.ThematicBreak:
		return []flow.Block{{Kind: "thematic_break", Indent: float64(indent) * 6}}
	case *extast.Table:
		return []flow.Block{{Kind: "table", Table: p.table(value), Indent: float64(indent) * 6}}
	case *ast.HTMLBlock:
		// 原始 HTML 不做渲染，避免引入 HTML 布局引擎。
		return nil
	default:
		return nil
	}
}

func (p *mdParser) list(list *ast.List, indent int) []flow.Block {
	var result []flow.Block
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
		if status, ok := extension.TaskStatusOf(item); ok {
			if status == extension.TaskStatusCompleted {
				marker += " [x]"
			} else {
				marker += " [ ]"
			}
		}
		attached := false
		for index := range itemBlocks {
			if !attached && (itemBlocks[index].Kind == "paragraph" || itemBlocks[index].Kind == "heading") {
				itemBlocks[index].Kind = "list_item"
				itemBlocks[index].Marker = marker
				itemBlocks[index].Indent = float64(indent) * 6
				attached = true
			}
		}
		if !attached && len(itemBlocks) == 0 {
			itemBlocks = append(itemBlocks, flow.Block{Kind: "list_item", Marker: marker, Indent: float64(indent) * 6})
		}
		result = append(result, itemBlocks...)
	}
	return result
}

func (p *mdParser) blockquote(quote *ast.Blockquote, indent int) []flow.Block {
	if alert, ok := p.alert(quote, indent); ok {
		return alert
	}
	inner := p.blocks(quote, indent+1)
	for index := range inner {
		if inner[index].Kind == "paragraph" {
			inner[index].Kind = "quote"
			inner[index].Indent -= 6 // quote 自身额外预留边线和正文间距。
		}
	}
	return inner
}

func (p *mdParser) table(table *extast.Table) *flow.Table {
	result := &flow.Table{HeaderRows: 1, SplitRows: true}
	for row := table.FirstChild(); row != nil; row = row.NextSibling() {
		switch value := row.(type) {
		case *extast.TableHeader:
			var cells []flow.TableCell
			for cell := value.FirstChild(); cell != nil; cell = cell.NextSibling() {
				tableCell, ok := cell.(*extast.TableCell)
				if !ok {
					continue
				}
				cells = append(cells, flow.TableCell{Runs: p.inlines(tableCell, true, false, false)})
				result.Columns = append(result.Columns, flow.TableColumn{Align: alignmentOf(tableCell.Alignment)})
			}
			result.Grid = append(result.Grid, cells)
		case *extast.TableBody:
			for row := value.FirstChild(); row != nil; row = row.NextSibling() {
				tableRow, ok := row.(*extast.TableRow)
				if !ok {
					continue
				}
				var cells []flow.TableCell
				for cell := tableRow.FirstChild(); cell != nil; cell = cell.NextSibling() {
					tableCell, ok := cell.(*extast.TableCell)
					if !ok {
						continue
					}
					cells = append(cells, flow.TableCell{Runs: p.inlines(tableCell, false, false, false)})
				}
				result.Grid = append(result.Grid, cells)
			}
		}
	}
	return result
}

func alignmentOf(alignment extast.Alignment) string {
	switch alignment {
	case extast.AlignCenter:
		return "center"
	case extast.AlignRight:
		return "right"
	default:
		return "left"
	}
}

func (p *mdParser) codeBlock(node ast.Node) flow.Block {
	block := flow.Block{Kind: "code", Text: p.codeText(node), Orphans: 1, Widows: 1}
	// 缩进代码块没有语言标记，只有围栏代码块才带 ``` 后的标识符。
	if code, ok := node.(*ast.CodeBlock); ok && code.CodeBlockKind == ast.CodeBlockKindFenced {
		language, _ := code.Language(p.source)
		block.CodeLang = strings.TrimSpace(language)
	}
	return block
}

func (p *mdParser) codeText(block ast.Node) string {
	value, ok := block.(*ast.CodeBlock)
	if !ok {
		return ""
	}
	// goldmark 的行段 Value 自带该行的换行符（代码块内的软换行也包含在内），
	// 直接拼接即可；再补一个 '\n' 会让每行之间多出空行，代码块高度翻倍。
	return strings.TrimRight(value.Value.Str(p.source), "\n")
}

func (p *mdParser) standaloneImage(node ast.Node) (*flow.Image, bool) {
	count := 0
	var image *ast.Image
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch value := child.(type) {
		case *ast.Image:
			count++
			image = value
		case *ast.Text:
			if strings.TrimSpace(value.Value.Value(p.source)) != "" {
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
func (p *mdParser) loadImage(node *ast.Image) (*flow.Image, bool) {
	source := node.Destination.Value(p.source)
	if source == "" {
		return nil, false
	}
	if isRemote(source) {
		slog.Warn("跳过远程图片", "source", source)
		return nil, false
	}
	data, err := loadLocalImage(source, p.baseDir)
	if err != nil {
		slog.Warn("跳过无法读取的图片", "source", source, "error", err)
		return nil, false
	}
	return &flow.Image{Data: data}, true
}

func (p *mdParser) inlines(parent ast.Node, bold, italic, strike bool) []flow.Run {
	var result []flow.Run
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		result = append(result, p.inline(child, bold, italic, strike)...)
	}
	return result
}

func (p *mdParser) inline(node ast.Node, bold, italic, strike bool) []flow.Run {
	switch value := node.(type) {
	case *ast.Text:
		var result []flow.Run
		if content := value.Value.Value(p.source); content != "" {
			result = append(result, flow.Run{Text: content, Bold: bold, Italic: italic, Strike: strike})
		}
		if value.HardLineBreak() {
			result = append(result, flow.Run{Text: "\n"})
		} else if value.SoftLineBreak() {
			result = append(result, flow.Run{Text: " "})
		}
		return result
	case *ast.CodeSpan:
		return []flow.Run{{Text: value.Value.Value(p.source), Code: true, Strike: strike}}
	case *ast.Emphasis:
		return p.inlines(value, bold, true, strike)
	case *ast.Strong:
		return p.inlines(value, true, italic, strike)
	case *extast.Strikethrough:
		return p.inlines(value, bold, italic, true)
	case *ast.Link:
		children := p.inlines(value, bold, italic, strike)
		href := linkHref(value.Destination.Value(p.source))
		for index := range children {
			children[index].Link = true
			children[index].Href = href
		}
		if len(children) == 0 {
			children = []flow.Run{{Text: value.Destination.Value(p.source), Link: true, Href: href}}
		}
		return children
	case *ast.AutoLink:
		url := value.Label.Value(p.source)
		return []flow.Run{{Text: url, Bold: bold, Italic: italic, Strike: strike, Link: true, Href: linkHref(url)}}
	case *ast.Image:
		alt := p.plainText(value)
		if alt == "" {
			alt = value.Destination.Value(p.source)
		}
		return []flow.Run{{Text: alt, Bold: bold, Italic: italic, Strike: strike}}
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
			builder.WriteString(value.Value.Value(p.source))
		case *ast.AutoLink:
			builder.WriteString(value.Label.Value(p.source))
		default:
			p.appendText(builder, child)
		}
	}
}

// linkHref 判断 Markdown 链接目标能否作为图元的点击目标。
//
// 纯锚点（`#章节`）指向 Markdown 标题 ID；转换器尚未生成这些稳定 ID，
// 不能仅凭标题大纲匹配目标，因此不生成动作。空目标同理——`[文字]()`
// 仍按链接着色，但不可点击。
//
// 其余目标原样保留：相对路径也照抄，不拿 Markdown 文件所在目录拼成绝对路径。
// 文档可能被单独分发，把本机路径写进产物既不可移植也泄漏了目录结构。
func linkHref(destination string) string {
	trimmed := strings.TrimSpace(destination)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return ""
	}
	return trimmed
}
