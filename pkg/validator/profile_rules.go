package validator

import (
	"path"
	"strings"

	"github.com/knroy/go-xml/xdm"
)

// maxPageBlockDepth 是 GB/T 42133 6.2.3 e) 规定的页面块嵌套层数上限。
const maxPageBlockDepth = 3

// allowedImageFormats 是 GB/T 42133 6.2.6 e) 允许的六种栅格图像格式。
// 比较用小写扩展名，不含点号。
var allowedImageFormats = map[string]bool{
	"bmp": true, "jpeg": true, "jpg": true, "png": true,
	"jbig2": true, "jp2": true, "jpeg2000": true, "tif": true, "tiff": true,
}

// allowedColorSpaceTypes 是 GB/T 42133 6.3.1 b) 允许的颜色空间类型，比较不区分大小写。
var allowedColorSpaceTypes = map[string]bool{
	"gray": true, "grey": true, "rgb": true, "cmyk": true,
}

// checkSingleDocument 对应 GB/T 42133 6.2.1 c)：归档文件不使用多文档机制。
// OFD.xml 里出现一个以上 DocBody 即为违规。
func checkSingleDocument(ctx *profileContext) {
	for _, doc := range ctx.ofdRoots() {
		count := 0
		for _, child := range doc.root.Children {
			if child.Kind == xdm.KindElement && child.Name.Local == "DocBody" {
				count++
			}
		}
		if count > 1 {
			ctx.addIssue(doc.root, doc.file.name, "single_document",
				"归档文件不使用多文档机制，OFD.xml 出现 %d 个 DocBody（GB/T 42133 6.2.1 c）", count)
		}
	}
}

// checkNoEncryption 对应 GB/T 42133 6.16：用于长期保存的 OFD 文件不使用任何
// 加密选项。加密的标志是包内出现 Encryptions.xml。
func checkNoEncryption(ctx *profileContext) {
	for _, name := range ctx.archive.sortedNames() {
		if strings.EqualFold(path.Base(name), "Encryptions.xml") {
			ctx.report.addIssue(Issue{
				Severity: SeverityError,
				Stage:    StageProfile,
				Code:     ctx.issueCode("encrypted"),
				Message:  "长期保存文件不含加密选项，但包内存在 " + name + "（GB/T 42133 6.16）",
				File:     name,
			}, ctx.maxErrors)
		}
	}
}

// checkDocumentNodeAbsent 生成“文档根节点不得出现指定元素”的规则。
// GB/T 42133 6.2.2 要求去除权限声明、视图首选项与扩展信息。
func checkDocumentNodeAbsent(ruleCode, element, label string) func(*profileContext) {
	return func(ctx *profileContext) {
		for _, doc := range ctx.documentRoots() {
			for _, child := range doc.root.Children {
				if child.Kind == xdm.KindElement && child.Name.Local == element {
					ctx.addIssue(child, doc.file.name, ruleCode,
						"文档根节点不应包含%s（%s）（GB/T 42133 6.2.2）", label, element)
				}
			}
		}
	}
}

// checkActionOnlyGoto 生成“动作仅保留文档内跳转”的规则。GB/T 42133 6.2.2 c)
// 针对文档动作、6.2.3 c) 针对页面动作，两者都只保留 Goto。
func checkActionOnlyGoto(scope string) func(*profileContext) {
	return func(ctx *profileContext) {
		for _, entry := range ctx.actionNodes() {
			node := entry.node
			if entry.scope != scope {
				continue
			}
			if !hasGotoChild(node) {
				ctx.addIssue(node, entry.file, actionRuleCode(scope),
					"%s动作不是文档内跳转（Goto），长期保存时应去除（GB/T 42133 6.2.2 c / 6.2.3 c）", scope)
			}
		}
	}
}

// checkOutlineActionOnlyGoto 对应 GB/T 42133 6.2.5 a)：大纲节点动作去除非
// 文档内跳转的部分。
func checkOutlineActionOnlyGoto(ctx *profileContext) {
	for _, entry := range ctx.actionNodes() {
		if entry.scope != "大纲" {
			continue
		}
		if !hasGotoChild(entry.node) {
			ctx.addIssue(entry.node, entry.file, "outline_action_not_goto",
				"大纲节点动作不是文档内跳转（Goto），长期保存时应去除（GB/T 42133 6.2.5 a）")
		}
	}
}

// actionScope 标识动作所在的宿主，用于把规则限定到文档、页面或大纲。
const (
	actionScopeDocument = "文档"
	actionScopePage     = "页面"
	actionScopeOutline  = "大纲"
)

// actionEntry 是一次待判定的动作。
type actionEntry struct {
	node  *xdm.Node
	scope string
	file  string
}

// actionNodes 收集全部动作节点并标注宿主。宿主决定适用哪条规则，因此必须
// 精确定位动作容器：Document.xml 根节点的直接子元素 Actions 属文档级，
// Outlines 下 OutlineElem 内的 Actions 属大纲级，页面 Content.xml 根节点的
// 直接子元素 Actions 属页面级。不能笼统递归——否则大纲节点内的动作会被
// 误判成文档动作，反之亦然。
func (c *profileContext) actionNodes() []actionEntry {
	var entries []actionEntry
	for _, name := range sortedDocumentNames(c.documents) {
		doc := c.documents[name]
		if doc == nil || doc.root == nil {
			continue
		}
		switch doc.rootName {
		case "Document":
			collectContainerActions(doc.root, actionScopeDocument, name, &entries)
			collectOutlineActions(doc.root, name, &entries)
		case "Page":
			collectContainerActions(doc.root, actionScopePage, name, &entries)
		}
	}
	return entries
}

// collectContainerActions 收集根节点直接子元素 Actions 下的动作。
func collectContainerActions(root *xdm.Node, scope, file string, entries *[]actionEntry) {
	for _, child := range root.Children {
		if child.Kind != xdm.KindElement || child.Name.Local != "Actions" {
			continue
		}
		for _, action := range child.Children {
			if action.Kind == xdm.KindElement && action.Name.Local == "Action" {
				*entries = append(*entries, actionEntry{node: action, scope: scope, file: file})
			}
		}
	}
}

// collectOutlineActions 收集大纲节点内的动作。大纲可多层嵌套，逐层下探。
func collectOutlineActions(node *xdm.Node, file string, entries *[]actionEntry) {
	for _, child := range node.Children {
		if child.Kind != xdm.KindElement {
			continue
		}
		if child.Name.Local == "OutlineElem" {
			collectContainerActions(child, actionScopeOutline, file, entries)
		}
		collectOutlineActions(child, file, entries)
	}
}

// hasGotoChild 判断动作是否包含 Goto 子元素。OFD 的动作是“多种选择项之一”，
// Goto 存在即视为文档内跳转。
func hasGotoChild(node *xdm.Node) bool {
	for _, child := range node.Children {
		if child.Kind == xdm.KindElement && child.Name.Local == "Goto" {
			return true
		}
	}
	return false
}

// checkImageFormats 对应 GB/T 42133 6.2.6 e)：栅格图像格式限于 BMP、JPEG、
// PNG、JBIG2、JPEG2000 和 TIFF，其他格式应在归档前转换。
func checkImageFormats(ctx *profileContext) {
	for _, entry := range ctx.imageResources() {
		extension := strings.ToLower(strings.TrimPrefix(path.Ext(entry.file), "."))
		if extension == "" || allowedImageFormats[extension] {
			continue
		}
		ctx.report.addIssue(Issue{
			Severity: SeverityError,
			Stage:    StageProfile,
			Code:     ctx.issueCode("image_format"),
			Message: "栅格图像格式 ." + extension + " 不在允许清单内，" +
				"应转换为 BMP、JPEG、PNG、JBIG2、JPEG2000 或 TIFF（GB/T 42133 6.2.6 e）",
			File: entry.file,
		}, ctx.maxErrors)
	}
}

// checkColorSpaceTypes 对应 GB/T 42133 6.3.1 b)：颜色空间类型限于灰度、RGB、
// CMYK 之一。
func checkColorSpaceTypes(ctx *profileContext) {
	for _, name := range sortedDocumentNames(ctx.documents) {
		doc := ctx.documents[name]
		if doc == nil || doc.root == nil || doc.rootName != "Res" {
			continue
		}
		walkOFDElements(doc.root, func(node *xdm.Node) {
			if node.Name.Local != "ColorSpace" {
				return
			}
			value := strings.TrimSpace(node.AttrValue("Type"))
			if value == "" || allowedColorSpaceTypes[strings.ToLower(value)] {
				return
			}
			ctx.addIssue(node, name, "colorspace_type",
				"颜色空间类型 %q 不是灰度、RGB 或 CMYK（GB/T 42133 6.3.1 b）", value)
		})
	}
}

// checkPageBlockDepth 对应 GB/T 42133 6.2.3 e)：去除页面内容中的页面块嵌套，
// 不可避免时嵌套不宜超过 3 层。
func checkPageBlockDepth(ctx *profileContext) {
	for _, name := range sortedDocumentNames(ctx.documents) {
		doc := ctx.documents[name]
		if doc == nil || doc.root == nil || doc.rootName != "Page" {
			continue
		}
		walkOFDElements(doc.root, func(node *xdm.Node) {
			if node.Name.Local != "PageBlock" {
				return
			}
			if depth := pageBlockDepth(node, 1); depth > maxPageBlockDepth {
				ctx.addIssue(node, name, "pageblock_depth",
					"页面块嵌套 %d 层，超过 %d 层上限（GB/T 42133 6.2.3 e）", depth, maxPageBlockDepth)
			}
		})
	}
}

// pageBlockDepth 计算以 node 为起点的页面块嵌套层数。
func pageBlockDepth(node *xdm.Node, current int) int {
	deepest := current
	for _, child := range node.Children {
		if child.Kind != xdm.KindElement {
			continue
		}
		next := current
		if child.Name.Local == "PageBlock" {
			next++
		}
		if nested := pageBlockDepth(child, next); nested > deepest {
			deepest = nested
		}
	}
	return deepest
}
