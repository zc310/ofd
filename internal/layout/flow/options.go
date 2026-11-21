package flow

import (
	"github.com/zc310/ofd/pkg/creator"
)

// Options 控制流式创作的页面、主题与字体。页面长度为毫米。
type Options struct {
	PageSize             creator.PageSize
	MarginTop            float64
	MarginRight          float64
	MarginBottom         float64
	MarginLeft           float64
	Theme                Theme
	Official             *Official
	Columns              Columns
	Styles               []NamedStyle
	Footnotes            []Footnote
	FootnoteStyle        *TextStyle
	Fonts                []creator.Font
	AllowLogicalFonts    bool
	CodeFont             string
	FontFaces            map[string]int
	FontFamilies         []FontFamily
	FallbackFonts        []string
	BoldFont             string
	ItalicFont           string
	BoldItalicFont       string
	PageNumbers          bool
	Header               string
	Footer               string
	GenerateOutlines     bool
	previousPages        map[string]int
	ReferencePageNumbers string
	previousLabels       map[string]string
	resolvedLabels       map[string]string
	captionLabels        map[string]string
	titleLabels          map[string]string
	noteNumbers          map[string]int
	noteTargets          map[string]Footnote
	// 字体快照已从原始字体读取名称，重排时不再采纳修复器补造的名称。
	fontNamesResolved bool
	balance           *balancePass
	// 主送迁移仅在一次 Compose 内重排，不修改公开的创作源或版记配置。
	relocatedRecipients    []Block
	officialOriginalBlocks []Block
}
