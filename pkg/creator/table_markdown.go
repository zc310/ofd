package creator

import (
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// styledRun 是单元格内一段统一样式的文本。Markdown 行内样式在此拍平，
// 供断行与按样式拆分 TextObject 使用。
type styledRun struct {
	text   string
	bold   bool
	italic bool
	code   bool
	strike bool
	// href 是这一段的点击目标，来自 Markdown 的 [文字](网址) 与自动链接。
	// 非空时只让这一段可点，为空时用单元格自己的 Actions。
	href string
	// link 标记这一段处在链接语法内，决定是否按链接着色。与 href 分开：着色由
	// 语法决定，点击由目标决定，纯锚点也是链接着色但点不动——与正文一致。
	link bool
	// rule 表示这是一条 Markdown 分隔线（---），不是文字。它独占一行，
	// 由 createCellTexts 画成横贯单元格的横线。
	rule bool
}

// linkHref 判断 Markdown 链接目标能否作为点击目标。
//
// 纯锚点（#章节）指向文档内部的标题，表格单元格没有大纲可跳，挂上去只是个点了
// 没反应的热区，因此不生成动作。空目标同理。
func linkHref(destination string) string {
	trimmed := strings.TrimSpace(destination)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return ""
	}
	return trimmed
}

// cellMarkdown 是表格单元格行内 Markdown 的解析器。creator 逐页串行构建，
// 复用同一实例避免每个单元格重复构造解析器。
var cellMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// parseMarkdownRuns 把单元格文本解析为带样式的行内片段。仅处理行内语法
// （粗体、斜体、行内代码、删除线）；块级结构在单元格内无意义，按行内内容
// 摊平处理，但块与块之间仍按换行分隔。
func parseMarkdownRuns(value string) []styledRun {
	source := []byte(value)
	root := cellMarkdown.Parser().Parse(text.NewReader(source))
	return blockRuns(root, source, styleState{})
}

// blockRuns 依次展开块级子节点，并在相邻块之间补换行。不补的话两段文字会
// 首尾相接粘成一行（列表的每个条目就是一个独立块）。
func blockRuns(node ast.Node, source []byte, st styleState) []styledRun {
	var runs []styledRun
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if len(runs) > 0 {
			runs = append(runs, styledRun{text: "\n"})
		}
		runs = append(runs, inlineRuns(child, source, st)...)
	}
	return runs
}

// styleState 是行内样式的累积状态，随递归进入 Emphasis 等节点时叠加。
type styleState struct {
	bold   bool
	italic bool
	code   bool
	strike bool
	// href 是当前所处的链接目标，随递归进入链接节点叠加。
	href string
	// link 表示当前处在链接语法内。
	link bool
	// listDepth 是当前所处的列表嵌套层数，用于缩进项目符号。
	listDepth int
}

// listIndentUnit 是每层列表嵌套的缩进。用两个空格：断行时单个空格会被当作词
// 分隔符丢弃，两个及以上则作为下一词的前缀保留下来。
const listIndentUnit = "  "

// listMarker 返回列表条目前的符号：无序列表用实心圆点，有序列表用"1." 这样的
// 序号（起始值取自源码的列表起始编号）。
func listMarker(list *ast.List, index int) string {
	if !list.IsOrdered() {
		return "• "
	}
	return strconv.Itoa(list.Start+index-1) + ". "
}

// listRuns 把列表展开为带项目符号的行内片段：每个条目一行，嵌套列表按层级缩进。
// 单元格宽度有限，条目文字换行后不会跟随项目符号缩进——那种排版要用单元格的
// HangingIndent 表达。
func listRuns(list *ast.List, source []byte, st styleState) []styledRun {
	var runs []styledRun
	index := 0
	for child := list.FirstChild(); child != nil; child = child.NextSibling() {
		if index > 0 {
			runs = append(runs, styledRun{text: "\n"})
		}
		index++
		if indent := strings.Repeat(listIndentUnit, st.listDepth); indent != "" {
			runs = append(runs, styledRun{text: indent})
		}
		runs = append(runs, styledRun{text: listMarker(list, index)})
		nested := st
		nested.listDepth++
		runs = append(runs, blockRuns(child, source, nested)...)
	}
	return runs
}

func inlineRuns(node ast.Node, source []byte, st styleState) []styledRun {
	switch value := node.(type) {
	case *ast.Text:
		var runs []styledRun
		if content := string(value.Segment.Value(source)); content != "" {
			runs = append(runs, styledRun{text: content, bold: st.bold, italic: st.italic, code: st.code, strike: st.strike, href: st.href, link: st.link})
		}
		// 硬换行（行尾两空格）与软换行（源码里的单个换行）都按强制换行处理。
		// Markdown 的软换行默认渲染成空格，那是给正文的排版约定；表格单元格
		// 里换行是用户明确写出来的意图，按空格吞掉会让多行内容挤成一行，
		// 且与同单元格的纯文本格式（\n 即换行）不一致。
		if value.HardLineBreak() || value.SoftLineBreak() {
			runs = append(runs, styledRun{text: "\n"})
		}
		return runs
	case *ast.String:
		return []styledRun{{text: string(value.Value), bold: st.bold, italic: st.italic, code: st.code, strike: st.strike, href: st.href}}
	case *ast.CodeSpan:
		return []styledRun{{text: plainTextOf(value, source), code: true, strike: st.strike}}
	case *ast.Emphasis:
		next := st
		if value.Level >= 2 {
			next.bold = true
		} else {
			next.italic = true
		}
		return childrenRuns(value, source, next)
	case *extast.Strikethrough:
		next := st
		next.strike = true
		return childrenRuns(value, source, next)
	case *ast.Link:
		next := st
		next.href = linkHref(string(value.Destination))
		next.link = true
		return childrenRuns(value, source, next)
	case *ast.AutoLink:
		// 自动链接没有子节点，显示文字就是网址本身。
		url := string(value.URL(source))
		return []styledRun{{text: url, bold: st.bold, italic: st.italic, code: st.code, strike: st.strike,
			href: linkHref(url), link: true}}
	case *extast.TaskCheckBox:
		// 任务列表的勾选状态是内容的一部分，不能丢：默认分支只展开子节点，
		// 而 TaskCheckBox 是叶子，丢掉后"- [x] 完成"与"- [ ] 待办"长得一样。
		//
		// 用 ASCII 方括号而不是 ☑/☐：实测思源黑体与 SmileySans 都没有这两个
		// 字形，会渲染成豆腐块，而方括号任何字体都有。
		mark := "[ ] "
		if value.IsChecked {
			mark = "[x] "
		}
		return []styledRun{{text: mark, strike: st.strike}}
	case *ast.ThematicBreak:
		// 分隔线不是文字，但也不是可以丢掉的东西：丢掉后 "上\n---\n下" 里的
		// 分界就消失了。用一个空文字的 rule 片段占一行，由 createCellTexts 画线。
		return []styledRun{{text: "\n", rule: true}}
	case *ast.List:
		// 每个列表项是一个独立块，项与项之间要换行，否则 "- 甲\n- 乙" 挤成"甲乙"。
		return listRuns(value, source, st)
	case *ast.ListItem:
		// 列表项自身也可能含块（嵌套列表），同样按块分隔。
		return blockRuns(value, source, st)
	default:
		return childrenRuns(node, source, st)
	}
}

func childrenRuns(node ast.Node, source []byte, st styleState) []styledRun {
	var runs []styledRun
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		runs = append(runs, inlineRuns(child, source, st)...)
	}
	return runs
}

func plainTextOf(node ast.Node, source []byte) string {
	var builder strings.Builder
	appendPlainText(&builder, node, source)
	return builder.String()
}

func appendPlainText(builder *strings.Builder, node ast.Node, source []byte) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch value := child.(type) {
		case *ast.Text:
			builder.Write(value.Segment.Value(source))
		case *ast.String:
			builder.Write(value.Value)
		default:
			appendPlainText(builder, child, source)
		}
	}
}
