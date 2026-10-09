package creator

import (
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
}

// cellMarkdown 是表格单元格行内 Markdown 的解析器。creator 逐页串行构建，
// 复用同一实例避免每个单元格重复构造解析器。
var cellMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// parseMarkdownRuns 把单元格文本解析为带样式的行内片段。仅处理行内语法
// （粗体、斜体、行内代码、删除线）；块级结构在单元格内无意义，按行内内容
// 摊平处理。
func parseMarkdownRuns(value string) []styledRun {
	source := []byte(value)
	root := cellMarkdown.Parser().Parse(text.NewReader(source))
	var runs []styledRun
	for child := root.FirstChild(); child != nil; child = child.NextSibling() {
		runs = append(runs, inlineRuns(child, source, styleState{})...)
	}
	return runs
}

// styleState 是行内样式的累积状态，随递归进入 Emphasis 等节点时叠加。
type styleState struct {
	bold   bool
	italic bool
	code   bool
	strike bool
}

func inlineRuns(node ast.Node, source []byte, st styleState) []styledRun {
	switch value := node.(type) {
	case *ast.Text:
		var runs []styledRun
		if content := string(value.Segment.Value(source)); content != "" {
			runs = append(runs, styledRun{text: content, bold: st.bold, italic: st.italic, code: st.code, strike: st.strike})
		}
		if value.HardLineBreak() {
			runs = append(runs, styledRun{text: "\n"})
		} else if value.SoftLineBreak() {
			runs = append(runs, styledRun{text: " "})
		}
		return runs
	case *ast.String:
		return []styledRun{{text: string(value.Value), bold: st.bold, italic: st.italic, code: st.code, strike: st.strike}}
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
