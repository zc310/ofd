// Package preserve 实现 GB/T 42133—2022 第 6 章的长期保存处理。
//
// 该标准把条款分成两类：可机器判定的合规条件由 ofd-validator 按 profile 规则
// 上报；本质是转换动作的条款由本包执行。本包只产出新的字节流，不修改输入。
//
// 转换以 XML 树操作为主，改用 beevik/etree 对既有文档做定点编辑，实测在真实
// 样例上字节往返一致。
//
// 不用 internal/models 的原因是它的读路径无法安全回写：OFD.xml 的 xmlns 属性
// 在解码后不会落到模型的 XMLNS 字段（字段 tag 是 xmlns:ofd，与 Go 解码出的
// 属性名不匹配），因此「解析 → 序列化」会写出 <Document xmlns:ofd="">，命名
// 空间 URI 丢失，产出的不再是合法 OFD。该模型的 MarshalXML 系列方法已随之移除，
// 写回路径统一走 etree。
package preserve

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/spec"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/replace"
	"github.com/zc310/ofd/pkg/validator"
)

// Change 描述一处改动，供 --dry-run 展示与审计留痕。
type Change struct {
	// Entry 是被改动的包内路径。
	Entry string
	// Clause 是依据条款。
	Clause string
	// Action 是人类可读的改动说明。
	Action string
	// Count 是该条款在本次转换中实际改动的节点数。
	Count int
}

// String 按单行输出，便于文本报告逐条列举。
func (c Change) String() string {
	return fmt.Sprintf("%s\t%s\t%s（%d 处）", c.Entry, c.Clause, c.Action, c.Count)
}

// Result 是一次转换的结果。
type Result struct {
	// DocType 是本次转换实际应用的 profile 取值。
	DocType string
	// Entry 是被改动的包内路径，无改动时为空。
	Entry string
	// Changes 是已做出的改动；Plan 给出的是计划中的改动。
	Changes []Change
	// Entries 是被改动的包内路径集合，无改动时为空。
	Entries []string
	// BeforeErrors 是转换前的错误数，Validate 为真时有效。
	BeforeErrors int
	// Unreferenced 是包内无人引用的条目，即 6.2.1 c) 的删除候选。
	Unreferenced []string
	// UnreferencedDropped 是实际被删除的条目数。
	UnreferencedDropped int
	// ClosureIncomplete 为真时闭包不可信，删除候选不得据此删除。
	ClosureIncomplete bool
	// ClosureReason 说明闭包为何不可信。
	ClosureReason string
}

// Options 控制转换行为。
type Options struct {
	// DocType 是 OFD profile 取值，空值时按文件声明的 DocType 自动判定。
	DocType string
	// Limits 限制输入与输出的规模，零值使用默认限制。
	Limits replace.Limits
	// Validate 在写出前确认转换没有引入新问题。
	//
	// 语义刻意不是「输出必须零错误」：真实 OFD 常带既有 XSD 偏差，若以零错误
	// 为门槛，转换会在多数文件上直接拒绝输出。判据是错误问题码集合不扩大，
	// 详见 verifyNoRegression。
	Validate bool
	// OnWarning 接收非致命提示，例如输入本就不合规、签名摘要已失效。
	OnWarning func(string)
	// DropUnreferenced 允许真的删除无人引用的条目（GB/T 42133 6.2.1 c)）。
	//
	// 默认关闭，只报告不删除。删除不可逆，而闭包的正确性依赖引用识别：漏识别
	// 一种引用形式就会删掉在用文件。开启时若闭包不完整仍会拒绝删除。
	DropUnreferenced bool
}

func (o Options) warn(format string, args ...any) {
	if o.OnWarning != nil {
		o.OnWarning(fmt.Sprintf(format, args...))
	}
}

// fileKind 标识一类被转换的 XML 文件。同一条款只作用于特定类型的文件。
type fileKind int

const (
	// kindDocument 是文档主体 XML（Document.xml）。
	kindDocument fileKind = iota
	// kindPageContent 是页面描述 XML（各页面的 Content.xml）。
	kindPageContent
)

// actionScope 标识动作所在宿主。划分方式与 pkg/validator 的规则保持一致：
// 转换只动 validator 会报出的那批动作，两者口径不同会让「转换后重跑校验」
// 这一验证手段失去意义。
type actionScope int

const (
	// scopeDocument 是文档级：Document.xml 根节点的直接子元素 Actions。
	scopeDocument actionScope = iota
	// scopePage 是页面级：Content.xml 根节点的直接子元素 Actions。
	scopePage
	// scopeOutline 是大纲级：OutlineElem 内的 Actions。大纲可多层嵌套，需逐层下探。
	scopeOutline
	// scopeGraphicUnit 是图元对象级：CT_GraphicUnit 自身的 Actions。
	scopeGraphicUnit
)

// step 是一处转换动作。刻意做成数据而非函数，好让 Plan 与 Apply 共用同一份
// 逻辑——两者一旦分叉，--dry-run 展示的计划就不可信了。
type step struct {
	// clause 是依据条款，写入 Change 供审计。
	clause string
	// action 是人类可读的改动说明。
	action string
	// kind 是该条款作用的文件类型。
	kind fileKind
	// apply 在文档根节点上执行改动，返回改动的节点数。
	apply func(root *etree.Element) int
}

// steps 返回当前 DocType 下应当执行的全部转换动作。
//
// 条款的强制性按标准原文区分：6.2 与 6.3.3 b) 用「去除」，属禁用清单，必须执行；
// 6.3.3 c) d) e) 用「宜去除」，是建议，本阶段不实施。
func steps(docType string) []step {
	switch docType {
	case spec.DocTypeOFDA, spec.DocTypeOFDH:
		// OFD-H 以 OFD-A 为基础，其数据内容与组织应符合 GB/T 42133，
		// 因此同一批转换动作对两者都适用。
		return []step{
			{"GB/T 42133 6.2.2 a)", "去除权限声明 Permissions", kindDocument, removeChild("Permissions")},
			{"GB/T 42133 6.2.2 b)", "去除视图首选项 VPreferences", kindDocument, removeChild("VPreferences")},
			{"GB/T 42133 6.2.2 c)", "去除非文档内跳转的文档动作", kindDocument, removeNonGotoActions(scopeDocument)},
			{"GB/T 42133 6.2.2 e)", "去除扩展信息 Extensions", kindDocument, removeChild("Extensions")},
			{"GB/T 42133 6.2.5 a)", "去除大纲节点中非文档内跳转的动作", kindDocument, removeNonGotoActions(scopeOutline)},
			{"GB/T 42133 6.2.3 c)", "去除非文档内跳转的页面动作", kindPageContent, removeNonGotoActions(scopePage)},
			{"GB/T 42133 6.3.3 b)", "去除图元对象中非文档内跳转的动作", kindPageContent, removeNonGotoActions(scopeGraphicUnit)},
		}
	default:
		// 基础 profile 不承诺满足 GB/T 42133，不施加档案转换。
		return nil
	}
}

// removeChild 返回一个删除指定子元素的步骤。只删 OFD 命名空间下的元素：外来
// 命名空间里的同名标签不受本标准约束，也不属于本工具的改动范围。
func removeChild(local string) func(*etree.Element) int {
	return func(root *etree.Element) int {
		var removed []*etree.Element
		for _, el := range root.ChildElements() {
			if el.Tag == local && el.NamespaceURI() == spec.Namespace {
				removed = append(removed, el)
			}
		}
		for _, el := range removed {
			root.RemoveChild(el)
		}
		return len(removed)
	}
}

// hasGotoChild 判断动作是否为文档内跳转。OFD 的动作是「多种选择项之一」，
// CT_Action 没有 Type 属性，Goto 子元素存在即视为文档内跳转——与
// pkg/validator 的 hasGotoChild 同一判定。
func hasGotoChild(action *etree.Element) bool {
	for _, child := range action.ChildElements() {
		if child.Tag == "Goto" && child.NamespaceURI() == spec.Namespace {
			return true
		}
	}
	return false
}

// removeNonGotoActions 返回一个删除指定宿主下非 Goto 动作的步骤。
//
// XSD 里 Actions 的子元素 Action 没有 minOccurs（默认 1），因此动作删光后
// 必须连 Actions 容器一起删，否则留下空的 <Actions/> 会让输出通不过 XSD 校验。
func removeNonGotoActions(scope actionScope) func(*etree.Element) int {
	return func(root *etree.Element) int {
		switch scope {
		case scopeDocument, scopePage:
			// 只看根节点的直接子元素。不能递归：Document.xml 里大纲节点内的
			// Actions 属于大纲级，笼统递归会把它们当文档动作处理。
			return pruneActions(root)
		case scopeOutline:
			total := 0
			var walk func(node *etree.Element)
			walk = func(node *etree.Element) {
				for _, child := range node.ChildElements() {
					if child.Tag == "OutlineElem" && child.NamespaceURI() == spec.Namespace {
						total += pruneActions(child)
					}
					walk(child)
				}
			}
			walk(root)
			return total
		case scopeGraphicUnit:
			// 图元对象自身的动作：Action 可挂在页面内容里的文字、路径、图像等
			// 各种图元上，不在根节点这一层，需要下探到对象元素。
			total := 0
			var walk func(node *etree.Element)
			walk = func(node *etree.Element) {
				if isGraphicUnit(node) {
					total += pruneActions(node)
				}
				for _, child := range node.ChildElements() {
					walk(child)
				}
			}
			walk(root)
			return total
		}
		return 0
	}
}

// graphicUnitTags 是 CT_GraphicUnit 派生类型的元素名，它们自身可以带 Actions。
var graphicUnitTags = map[string]bool{
	"TextObject": true, "PathObject": true, "ImageObject": true,
	"CompositeObject": true, "BlockObject": true,
}

// isGraphicUnit 判断元素是否为可带动作的图元对象。
func isGraphicUnit(el *etree.Element) bool {
	return el.NamespaceURI() == spec.Namespace && graphicUnitTags[el.Tag]
}

// pruneActions 删除 container 的 Actions 里所有非 Goto 动作，并在动作删光时
// 一并删除 Actions 容器，返回删除的动作数。
func pruneActions(container *etree.Element) int {
	var target *etree.Element
	for _, el := range container.ChildElements() {
		if el.Tag == "Actions" && el.NamespaceURI() == spec.Namespace {
			target = el
			break
		}
	}
	if target == nil {
		return 0
	}
	var doomed []*etree.Element
	kept := 0
	for _, action := range target.ChildElements() {
		if action.Tag != "Action" || action.NamespaceURI() != spec.Namespace {
			continue
		}
		if hasGotoChild(action) {
			kept++
			continue
		}
		doomed = append(doomed, action)
	}
	for _, action := range doomed {
		target.RemoveChild(action)
	}
	if kept == 0 {
		container.RemoveChild(target)
	}
	return len(doomed)
}

// Plan 计算将要做出的改动，不写任何文件。--dry-run 走这条路径。
func Plan(input any, options Options) (Result, error) {
	src, err := open(input)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = src.pkg.Close() }()

	result, _, err := convert(src.pkg, options)
	return result, err
}

// Apply 执行转换并把结果写入 w。输出始终是新的字节流，输入不受影响。
//
// 全部校验与写出都成功后才真正写给 w，因此失败时 w 不会拿到半成品。
func Apply(input any, w io.Writer, options Options) (Result, error) {
	if w == nil {
		return Result{}, fmt.Errorf("OFD 输出写入器为空")
	}
	src, err := open(input)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = src.pkg.Close() }()

	result, operations, err := convert(src.pkg, options)
	if err != nil {
		return result, err
	}
	if len(operations) == 0 {
		return result, fmt.Errorf("转换未产生改动，无需写出（DocType=%s）", result.DocType)
	}

	var baseline map[string]bool
	if options.Validate {
		before := src.validate(options)
		result.BeforeErrors = before.Summary.Errors
		if before.Summary.Errors > 0 {
			options.warn("输入已存在 %d 个错误（%s），本次只保证转换不新增问题，不要求输出零错误",
				before.Summary.Errors, before.StatusZh)
		}
		baseline = errorCodes(before)
	}

	var buffer bytes.Buffer
	// 不让 replace 自己做严格校验：它要求输出零错误，会把「输入本来就不合规」
	// 误判成转换失败。改为写出后与基线比对，见 verifyNoRegression。
	if err := replace.Files(src.pkg, operations, &buffer, replace.Options{
		Limits: options.Limits,
		// 必须显式指定：replace 的零值会归一化成 SignatureDrop，即删除全部
		// 签名条目。归档件应当保留签名作为证据，只提示摘要可能失效。
		Signatures: creator.SignaturePreserve,
		OnWarning:  options.OnWarning,
	}); err != nil {
		return result, err
	}
	if baseline != nil {
		if err := verifyNoRegression(baseline, buffer.Bytes(), options.DocType); err != nil {
			return result, err
		}
	}
	if _, err := w.Write(buffer.Bytes()); err != nil {
		return result, fmt.Errorf("写入输出失败: %w", err)
	}
	return result, nil
}

// source 是归一化后的输入。保留路径形态是为了校验器能自己施加规模限制，
// 也让报告里的输入名称可读。
type source struct {
	path string
	data []byte
	pkg  *core.Package
}

// validate 对原始输入做校验，取得转换前的问题基线。
func (s source) validate(options Options) validator.Report {
	report, err := validator.New(validator.WithDocType(options.DocType))
	if err != nil {
		options.warn("无法创建校验器，转换前基线缺失：%v", err)
		return validator.Report{}
	}
	if s.path != "" {
		return report.ValidatePath(context.Background(), s.path)
	}
	return report.ValidateReader(context.Background(), bytes.NewReader(s.data), "")
}

// open 把各种输入形态统一成可读可校验的源。
func open(input any) (source, error) {
	switch v := input.(type) {
	case string:
		pkg, err := core.OpenFile(v)
		if err != nil {
			return source{}, err
		}
		return source{path: v, pkg: pkg}, nil
	case []byte:
		pkg, err := core.OpenBytes(v)
		if err != nil {
			return source{}, err
		}
		return source{data: v, pkg: pkg}, nil
	default:
		return source{}, fmt.Errorf("不支持的输入类型 %T", input)
	}
}

// errorCodes 收集报告里的错误问题码，作为回归比对的基线集合。
func errorCodes(report validator.Report) map[string]bool {
	codes := make(map[string]bool, len(report.Issues))
	for _, issue := range report.Issues {
		if issue.Severity == validator.SeverityError {
			codes[issue.Code] = true
		}
	}
	return codes
}

// verifyNoRegression 确认转换后的错误问题码没有超出基线。
//
// 按问题码而非位置比对：重写 XML 会让行列号整体偏移，按位置比对必然误判。
// 代价是基线里已有的错误无法区分「同一条」与「另一条」——输入本就 XSD 不合规
// 时，转换引入的新的 XSD 错误不会被发现。这是当前实现的已知边界。
func verifyNoRegression(baseline map[string]bool, output []byte, docType string) error {
	report, err := validator.New(validator.WithDocType(docType))
	if err != nil {
		return fmt.Errorf("创建校验器失败: %w", err)
	}
	after := report.ValidateReader(context.Background(), bytes.NewReader(output), "")
	var added []string
	for _, issue := range after.Issues {
		if issue.Severity != validator.SeverityError || baseline[issue.Code] {
			continue
		}
		if !slices.Contains(added, issue.Code) {
			added = append(added, issue.Code)
		}
	}
	if len(added) > 0 {
		return fmt.Errorf("转换引入了新的错误问题码 %s，已放弃写出以免产出更差的文件",
			strings.Join(added, "、"))
	}
	return nil
}

// convert 在内存中执行全部转换，返回改动清单与待写入的条目操作。
func convert(pkg *core.Package, options Options) (Result, []replace.Operation, error) {
	docType, err := resolveDocType(pkg, options.DocType)
	if err != nil {
		return Result{}, nil, err
	}
	result := Result{DocType: docType}
	plan := steps(docType)
	if len(plan) == 0 {
		return result, nil, nil
	}

	docEntry, err := documentEntry(pkg)
	if err != nil {
		return result, nil, err
	}
	result.Entry = docEntry

	operations := map[string][]byte{}
	applyTo := func(path string, kind fileKind) error {
		data, err := pkg.Read(path)
		if err != nil {
			return fmt.Errorf("读取 %s 失败: %w", path, err)
		}
		doc := etree.NewDocument()
		if err := doc.ReadFromBytes(data); err != nil {
			return fmt.Errorf("解析 %s 失败: %w", path, err)
		}
		root := doc.Root()
		if root == nil {
			return fmt.Errorf("%s 没有根元素", path)
		}
		var total int
		for _, s := range plan {
			if s.kind != kind {
				continue
			}
			count := s.apply(root)
			if count == 0 {
				continue
			}
			total += count
			result.Changes = append(result.Changes, Change{
				Entry: path, Clause: s.clause, Action: s.action, Count: count,
			})
		}
		if total == 0 {
			return nil
		}
		out, err := doc.WriteToBytes()
		if err != nil {
			return fmt.Errorf("序列化 %s 失败: %w", path, err)
		}
		operations[path] = out
		result.Entries = append(result.Entries, path)
		return nil
	}

	if err := applyTo(docEntry, kindDocument); err != nil {
		return result, nil, err
	}
	pages, err := pageEntries(pkg, docEntry)
	if err != nil {
		return result, nil, err
	}
	for _, page := range pages {
		if err := applyTo(page, kindPageContent); err != nil {
			return result, nil, err
		}
	}

	var deletes []string
	if err := applyUnreferenced(pkg, options, &result, &deletes); err != nil {
		return result, nil, err
	}

	if len(operations) == 0 && len(deletes) == 0 {
		return result, nil, nil
	}
	// 先按路径排序再组装，保证同一输入产生稳定的操作顺序。
	paths := make([]string, 0, len(operations))
	for path := range operations {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	list := make([]replace.Operation, 0, len(paths)+len(deletes))
	for _, path := range paths {
		list = append(list, replace.Operation{Kind: replace.OpSet, Name: path, Data: operations[path]})
	}
	slices.Sort(deletes)
	for _, path := range deletes {
		list = append(list, replace.Operation{Kind: replace.OpDelete, Name: path})
	}
	return result, list, nil
}

// applyUnreferenced 实现 GB/T 42133 6.2.1 c)：去除与主入口及其嵌套引出文件
// 无关的文件。
//
// 只在显式开启 DropUnreferenced 时才删除，且闭包不完整时一律拒绝。删除不可逆，
// 而闭包的正确性完全依赖引用识别是否穷尽——漏掉一种引用形式就等于删掉在用文件。
// test/testdata/intro.ofd 的命名空间缺 "/2016" 后缀，解析失败后其引用的字体
// 全部落进候选，正是这个保护要拦住的情形。
func applyUnreferenced(pkg *core.Package, options Options, result *Result, deletes *[]string) error {
	index, err := validator.PackageReferences(context.Background(), pkg, validatorOptions(options)...)
	if err != nil {
		return fmt.Errorf("解析引用闭包失败: %w", err)
	}
	orphans := index.Unreachable(pkg)
	if len(orphans) == 0 {
		return nil
	}
	result.Unreferenced = orphans
	result.ClosureIncomplete = !index.Complete()
	if result.ClosureIncomplete {
		result.ClosureReason = index.IncompleteReason()
		options.warn("引用闭包不完整，6.2.1 c) 的 %d 个删除候选未予删除：%s",
			len(orphans), result.ClosureReason)
		return nil
	}
	if !options.DropUnreferenced {
		options.warn("有 %d 个条目无人引用（GB/T 42133 6.2.1 c)），本次只报告不删除；加 --drop-unreferenced 执行删除",
			len(orphans))
		return nil
	}
	*deletes = append(*deletes, orphans...)
	result.UnreferencedDropped = len(orphans)
	return nil
}

// validatorOptions 把转换选项里与校验相关的部分转成校验器选项。
func validatorOptions(options Options) []validator.Option {
	var out []validator.Option
	if options.DocType != "" && spec.IsDocType(options.DocType) {
		out = append(out, validator.WithDocType(options.DocType))
	}
	return out
}

// pageEntries 解析出全部页面描述 XML 的包内路径。
//
// 路径取自 Document.xml 的 Pages/Page/@BaseLoc，按文档所在目录解析，不假定
// Doc_0 或固定的页目录名。模板页（TemplatePage）不在页树内，不参与 6.2.3。
func pageEntries(pkg *core.Package, docEntry string) ([]string, error) {
	data, err := pkg.Read(docEntry)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", docEntry, err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", docEntry, err)
	}
	base := models.StLoc(docEntry).Dir()
	var out []string
	seen := map[string]bool{}
	for _, el := range doc.Root().FindElements("./Pages/Page") {
		loc := strings.TrimSpace(el.SelectAttrValue("BaseLoc", ""))
		if loc == "" {
			continue
		}
		resolved := string(models.StLoc(loc).Resolve(base))
		if resolved == "" || seen[resolved] {
			continue
		}
		if !pkg.Has(resolved) {
			// 页面描述缺失时不在此处报错：validator 会报引用缺失，转换不应
			// 因此中断，跳过即可。
			continue
		}
		seen[resolved] = true
		out = append(out, resolved)
	}
	return out, nil
}

// documentEntry 从 OFD.xml 的 DocRoot 解析出文档主体 XML 的包内路径，而不是
// 假定 Doc_0/Document.xml——目录名由生成方决定，写死会在非默认布局上找不到文件。
func documentEntry(pkg *core.Package) (string, error) {
	root, err := readRoot(pkg)
	if err != nil {
		return "", err
	}
	for _, el := range root.FindElements("./DocBody/DocRoot") {
		if path := strings.TrimSpace(el.Text()); path != "" {
			return strings.TrimPrefix(path, "/"), nil
		}
	}
	return "", fmt.Errorf("%s 未声明 DocBody/DocRoot，无法定位文档主体 XML", spec.RootDocument)
}

// readRoot 解析并返回 OFD.xml 的根元素。
func readRoot(pkg *core.Package) (*etree.Element, error) {
	entry, ok := pkg.Lookup(spec.RootDocument)
	if !ok {
		return nil, fmt.Errorf("包内找不到 %s", spec.RootDocument)
	}
	data, err := pkg.Read(entry.Path)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", spec.RootDocument, err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", spec.RootDocument, err)
	}
	root := doc.Root()
	if root == nil || root.Tag != "OFD" {
		return nil, fmt.Errorf("%s 的根元素不是 ofd:OFD", spec.RootDocument)
	}
	return root, nil
}

// resolveDocType 判定本次转换使用的 profile：显式指定优先，否则按文件声明。
func resolveDocType(pkg *core.Package, requested string) (string, error) {
	if requested != "" {
		if !spec.IsDocType(requested) {
			return "", fmt.Errorf("未知的 DocType %q，取值只能是 %s",
				requested, strings.Join(spec.DocTypes, "、"))
		}
		return requested, nil
	}
	root, err := readRoot(pkg)
	if err != nil {
		return "", err
	}
	if declared := root.SelectAttrValue("DocType", ""); spec.IsDocType(declared) {
		return declared, nil
	}
	// 未声明或声明为未知取值时按基础 profile 处理，不擅自施加档案转换。
	return spec.DocTypeOFD, nil
}
