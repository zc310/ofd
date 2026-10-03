package validator

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/knroy/go-xml/xdm"
	"github.com/zc310/ofd/internal/spec"
)

// profileRule 是单条 profile 规则。规则只做判定并上报，不修改文档——
// GB/T 42133 中“去除×××”一类条款本质是转换动作，由归档处理流水线负责。
type profileRule struct {
	// Code 是问题码的前缀，与 Stage 组合成 profile.ofd_a.<code>。
	Code string
	// Title 是规则的简短说明，用于报告展示。
	Title string
	// Check 执行检查，发现违规时通过 ctx 上报。
	Check func(ctx *profileContext)
}

// profile 是按 DocType 判定的规则集合。
//
// profile 之间存在叠加关系：GB/T 42133—2022 在 GB/T 33190 基础 profile 之上
// 收紧特性，GB/T 48666-2026《电子病历版式文档技术要求》又声明数据内容与组织
// 应符合 GB/T 42133，因此 OFD-H 继承 OFD-A 的全部规则。
type profile struct {
	// Name 是 profile 名称，与 DocType 取值一致。
	Name string
	// Inherits 是父 profile，nil 表示基础 profile。
	Inherits *profile
	// Rules 是本 profile 自身新增的规则。
	Rules []profileRule
}

// docTypeProfiles 按 DocType 取值索引 profile 链的起点。
var docTypeProfiles = map[string]*profile{}

func init() {
	base := &profile{Name: spec.DocTypeOFD}
	archive := &profile{
		Name:     spec.DocTypeOFDA,
		Inherits: base,
		Rules:    ofdArchiveRules,
	}
	medical := &profile{
		Name:     spec.DocTypeOFDH,
		Inherits: archive,
		Rules:    ofdMedicalRules,
	}
	for _, item := range []*profile{base, archive, medical} {
		docTypeProfiles[item.Name] = item
	}
}

// profileFor 返回 docType 对应的 profile；未知或空取值返回基础 profile。
func profileFor(docType string) *profile {
	if item, ok := docTypeProfiles[strings.TrimSpace(docType)]; ok {
		return item
	}
	return docTypeProfiles[spec.DocTypeOFD]
}

// resolvedRules 沿继承链收集规则，父 profile 的规则排在前面。
//
// 同名规则由子 profile 覆盖：子 profile 重新声明相同 Code 即视为覆盖，父 profile
// 的版本被丢弃。标准之间对同一要素给出不同要求时必须这样处理，否则继承会把
// 父标准更宽松的约束带到子标准里——例如 GB/T 42133 6.2.6 e) 允许六种栅格图像
// 格式，GB/T 48666 7.2 d) 只允许四种，OFD-H 若直接继承六种就会比 48666 宽松。
func (p *profile) resolvedRules() []profileRule {
	if p == nil {
		return nil
	}
	inherited := p.Inherits.resolvedRules()
	overridden := make(map[string]bool, len(p.Rules))
	for _, rule := range p.Rules {
		overridden[rule.Code] = true
	}
	rules := make([]profileRule, 0, len(inherited)+len(p.Rules))
	for _, rule := range inherited {
		if !overridden[rule.Code] {
			rules = append(rules, rule)
		}
	}
	return append(rules, p.Rules...)
}

// profileContext 是规则执行上下文。复用校验器已解析的文档树与包索引，
// 规则不再重复解析任何内容。
type profileContext struct {
	docType   string
	documents map[string]*xmlDocument
	archive   *packageIndex
	report    *Report
	maxErrors int
	// media 按资源 ID 索引的多媒体与复合图元文件位置，用于判定图像格式。
	media map[string]string
}

// addIssue 按 profile 阶段上报问题。规则码不含 profile 前缀，这里统一补全，
// 避免每条规则各写一遍。
func (c *profileContext) addIssue(node *xdm.Node, file, ruleCode, format string, args ...any) {
	message := format
	if len(args) > 0 {
		message = fmt.Sprintf(format, args...)
	}
	c.report.addIssue(Issue{
		Severity: SeverityError,
		Stage:    StageProfile,
		Code:     "profile." + strings.ToLower(strings.ReplaceAll(c.profileName(), "-", "_")) + "." + ruleCode,
		Message:  message,
		File:     file,
	}, c.maxErrors)
}

// profileName 返回当前 profile 的 DocType 取值。
func (c *profileContext) profileName() string {
	return c.docType
}

// ofdMedicalRules 是 GB/T 48666-2026《电子病历版式文档技术要求》在本阶段
// 可机器判定的规则。该标准目前为征求意见稿，条款可能变化，因此暂不加入需要
// 阈值或启发式判断的条款。
var ofdMedicalRules = []profileRule{
	{
		// 7.2 d)：版式文档内使用的图像格式应限 BMP、JPEG、TIFF 及 PNG。
		// 该标准比 GB/T 42133 6.2.6 e) 少了 JBIG2 与 JPEG2000，因此覆盖父
		// profile 的同名规则；直接继承会让判定比标准宽松。
		Code:  "image_format",
		Title: "栅格图像格式在允许清单内",
		Check: checkImageFormats(allowedImageFormatsMedical, "GB/T 48666 7.2 d)"),
	},
	{
		// 8 c)：签名的保护范围应涵盖不包含注释列表、签名列表等文件的全部内容。
		Code:  "signature_coverage",
		Title: "签名保护范围覆盖全部内容",
		Check: checkSignatureCoverage,
	},
}

// ofdArchiveRules 是 GB/T 42133—2022《信息技术 OFD档案应用指南》在本阶段
// 可机器判定的规则。条款号标注在每条规则的注释中。
var ofdArchiveRules = []profileRule{
	{
		// 6.2.1 c)：归档的 OFD 文件不使用多文档机制。
		Code:  "single_document",
		Title: "归档文件不使用多文档机制",
		Check: checkSingleDocument,
	},
	{
		// 6.16：用于长期保存的 OFD 文件不使用任何加密选项。
		Code:  "encrypted",
		Title: "长期保存文件不含加密",
		Check: checkNoEncryption,
	},
	{
		// 6.2.2 a)：去除文件中的权限声明（Permissions）。
		Code:  "permissions_present",
		Title: "文档根节点不含权限声明",
		Check: checkDocumentNodeAbsent("permissions_present", "Permissions", "权限声明"),
	},
	{
		// 6.2.2 b)：去除文件中的视图首选项（VPreferences）。
		Code:  "vpreferences_present",
		Title: "文档根节点不含视图首选项",
		Check: checkDocumentNodeAbsent("vpreferences_present", "VPreferences", "视图首选项"),
	},
	{
		// 6.2.2 e)：去除文件中的扩展信息（Extensions）。
		Code:  "extensions_present",
		Title: "文档根节点不含扩展信息",
		Check: checkDocumentNodeAbsent("extensions_present", "Extensions", "扩展信息"),
	},
	{
		// 6.2.2 c)：去除类型不是文档内跳转（Goto）的文档动作。
		Code:  "document_action_not_goto",
		Title: "文档动作仅保留文档内跳转",
		Check: checkActionOnlyGoto("文档"),
	},
	{
		// 6.2.3 c)：去除其中类型不是文档内跳转的页面动作。
		Code:  "page_action_not_goto",
		Title: "页面动作仅保留文档内跳转",
		Check: checkActionOnlyGoto("页面"),
	},
	{
		// 6.2.5 a)：去除大纲节点中类型不为文档内跳转的动作。
		Code:  "outline_action_not_goto",
		Title: "大纲节点动作仅保留文档内跳转",
		Check: checkOutlineActionOnlyGoto,
	},
	{
		// 6.2.6 e)：栅格图像格式限于 BMP、JPEG、PNG、JBIG2、JPEG2000 和 TIFF。
		Code:  "image_format",
		Title: "栅格图像格式在允许清单内",
		Check: checkImageFormats(allowedImageFormatsArchive, "GB/T 42133 6.2.6 e)"),
	},
	{
		// 6.3.1 b)：颜色空间类型限于灰度、RGB、CMYK 之一。
		Code:  "colorspace_type",
		Title: "颜色空间类型在允许清单内",
		Check: checkColorSpaceTypes,
	},
	{
		// 6.2.3 e)：去除页面内容中的页面块（PageBlock）嵌套，必要时不超 3 层。
		Code:  "pageblock_depth",
		Title: "页面块嵌套不超过 3 层",
		Check: checkPageBlockDepth,
	},
}

// isOFDXML 判断文档是否为包入口 OFD.xml。xmlDocument.ofd 只表示根元素位于 OFD
// 命名空间，Document.xml 与 Page 同样为 true，必须另按根元素名区分。
func isOFDXML(doc *xmlDocument) bool {
	return doc != nil && doc.ofd && doc.root != nil && doc.rootName == "OFD"
}

// documentRoots 返回包内全部 Document.xml。GB/T 42133 6.2.2 的“文档根节点”
// 指 Document.xml，而不是包入口 OFD.xml。
func (c *profileContext) documentRoots() []*xmlDocument {
	var docs []*xmlDocument
	for _, name := range sortedDocumentNames(c.documents) {
		doc := c.documents[name]
		if doc != nil && doc.root != nil && doc.rootName == "Document" {
			docs = append(docs, doc)
		}
	}
	return docs
}

// ofdRoots 返回包内全部 OFD.xml 根文档。
func (c *profileContext) ofdRoots() []*xmlDocument {
	var docs []*xmlDocument
	for _, name := range sortedDocumentNames(c.documents) {
		if doc := c.documents[name]; isOFDXML(doc) {
			docs = append(docs, doc)
		}
	}
	return docs
}

// issueCode 生成带 profile 前缀的问题码，例如 profile.ofd_a.image_format。
func (c *profileContext) issueCode(rule string) string {
	return "profile." + strings.ToLower(strings.ReplaceAll(c.docType, "-", "_")) + "." + rule
}

// actionRuleCode 把动作宿主映射为规则码后缀。
func actionRuleCode(scope string) string {
	switch scope {
	case actionScopeDocument:
		return "document_action_not_goto"
	case actionScopePage:
		return "page_action_not_goto"
	case actionScopeOutline:
		return "outline_action_not_goto"
	default:
		return "action_not_goto"
	}
}

// imageResource 是一次待判定的图像资源。
type imageResource struct {
	// id 是资源 ID，仅用于排查。
	id string
	// file 是包内文件路径。
	file string
}

// imageResources 收集被页面引用的图像资源。图像对象通过 ResourceID 引用
// 资源，实际格式取决于 Res 下的文件，因此需要先建立 ID 到文件的索引。
func (c *profileContext) imageResources() []imageResource {
	if c.media == nil {
		c.media = collectMediaFiles(c.documents)
	}
	used := make(map[string]bool)
	for _, doc := range c.documents {
		if doc == nil || doc.root == nil || doc.rootName != "Page" {
			continue
		}
		walkOFDElements(doc.root, func(node *xdm.Node) {
			if node.Name.Local != "ImageObject" {
				return
			}
			if id := strings.TrimSpace(node.AttrValue("ResourceID")); id != "" {
				used[id] = true
			}
		})
	}
	var resources []imageResource
	for _, id := range sortedKeys(used) {
		if file, ok := c.media[id]; ok {
			resources = append(resources, imageResource{id: id, file: file})
		}
	}
	return resources
}

// collectMediaFiles 建立资源 ID 到包内文件的索引。
//
// 只索引 MultiMedia：复合图元 CompositeGraphicUnit 的图形内容内联在
// <Content> 中，没有独立文件，不参与图像格式判定。
func collectMediaFiles(documents map[string]*xmlDocument) map[string]string {
	files := make(map[string]string)
	for _, name := range sortedDocumentNames(documents) {
		doc := documents[name]
		if doc == nil || doc.root == nil || doc.rootName != "Res" {
			continue
		}
		base := doc.baseDir
		walkOFDElements(doc.root, func(node *xdm.Node) {
			// 只索引声明为图像的多媒体：音视频的处理见 GB/T 42133 6.2.6 g)，
			// 不属于图像格式规则的范围。
			if node.Name.Local != "MultiMedia" || !strings.EqualFold(strings.TrimSpace(node.AttrValue("Type")), "Image") {
				return
			}
			id := strings.TrimSpace(node.AttrValue("ID"))
			location := strings.TrimSpace(childText(node, "MediaFile"))
			if id == "" || location == "" {
				return
			}
			files[id] = resolveLocation(base, location)
		})
	}
	return files
}

// childText 返回指定子元素的文本内容，找不到时返回空串。OFD 的资源位置既可能
// 写成属性也可能写成子元素，两种形式都要读。
func childText(node *xdm.Node, name string) string {
	if value := strings.TrimSpace(node.AttrValue(name)); value != "" {
		return value
	}
	for _, child := range node.Children {
		if child.Kind == xdm.KindElement && child.Name.Local == name {
			return strings.TrimSpace(child.StringValue())
		}
	}
	return ""
}

// resolveLocation 把资源位置解析为包内相对路径。
func resolveLocation(base, location string) string {
	if location == "" {
		return ""
	}
	cleaned := strings.TrimPrefix(path.Clean(strings.ReplaceAll(location, "\\", "/")), "./")
	if base == "" || base == "." {
		return cleaned
	}
	return path.Join(base, cleaned)
}

// sortedKeys 返回映射的键，按字典序排列，保证问题上报顺序稳定。
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// hasBaseName 判断包内是否存在指定基名的条目。OFD 规范允许同一类文件出现在
// 不同目录层级（例如各文档体下各有 Signatures.xml），按基名判断更贴近语义。
func (p *packageIndex) hasBaseName(base string) bool {
	for name, entry := range p.files {
		if !entry.isDir && path.Base(name) == base {
			return true
		}
	}
	return false
}

// sortedNames 返回包索引中的条目名，按字典序排列。
func (p *packageIndex) sortedNames() []string {
	if p == nil {
		return nil
	}
	return sortedPackageNames(p.files)
}

// profileChecks 按 profile 执行规则。文件声明了非基础 DocType 时自动应用对应
// 规则——声明即承诺，自称档案长期保存文件就该被 GB/T 42133 约束；显式指定
// Options.DocType 时以指定 profile 为准，可在不改写 DocType 的前提下预检。
//
// 规则只判定并上报，不修改文档。GB/T 42133 中“去除×××”一类条款本质是转换
// 动作，归档处理流水线负责，不在校验器内实现。
func (v *Validator) profileChecks(documents map[string]*xmlDocument, archive *packageIndex, report *Report) {
	docType := v.profileDocType(documents)
	target := profileFor(docType)
	if v.opts.DocType != "" {
		target = profileFor(v.opts.DocType)
		docType = target.Name
	}
	if !v.opts.CheckProfile && target == profileFor(spec.DocTypeOFD) {
		report.setCheck("profile", "skipped")
		return
	}
	rules := target.resolvedRules()
	if len(rules) == 0 {
		report.setCheck("profile", "skipped")
		return
	}
	report.setProfile(docType)
	ctx := &profileContext{
		docType:   docType,
		documents: documents,
		archive:   archive,
		report:    report,
		maxErrors: v.opts.MaxErrors,
	}
	for _, rule := range rules {
		if rule.Check == nil {
			continue
		}
		rule.Check(ctx)
	}
	if report.hasStageErrors(StageProfile) {
		report.setCheck("profile", "failed")
	} else {
		report.setCheck("profile", "passed")
	}
}

// profileDocType 读取包内声明的 DocType。存在多个 OFD.xml 时以第一份为准——
// 多文档本身已由其它阶段报告，这里只取 profile 判定依据。
func (v *Validator) profileDocType(documents map[string]*xmlDocument) string {
	for _, name := range sortedDocumentNames(documents) {
		if doc := documents[name]; isOFDXML(doc) {
			return strings.TrimSpace(doc.root.AttrValue("DocType"))
		}
	}
	return ""
}
