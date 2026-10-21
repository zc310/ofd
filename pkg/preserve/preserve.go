// Package preserve 实现 GB/T 42133—2022 第 6 章的长期保存处理。
//
// 该标准把条款分成两类：可机器判定的合规条件由 ofd-validator 按 profile 规则
// 上报；本质是转换动作的条款由本包执行。本包只产出新的字节流，不修改输入。
//
// 转换使用 internal/ofdxml 读取和写出，使用 etree API 对既有 XML 做定点编辑。
// 重写的 OFD 元素统一使用 ofd: 前缀，并保留扩展命名空间与混合内容；
// 未发生改动的条目继续逐字节复制。
package preserve

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/ofdxml"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/replace"
	"github.com/zc310/ofd/pkg/spec"
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
	// Entry 是按字典序排列的首个实际改动路径，无改动时为空。
	Entry string
	// Changes 是已做出的改动；Plan 给出的是计划中的改动。
	Changes []Change
	// Entries 是被修改或删除的包内路径，按字典序去重，无改动时为空。
	// Plan 的删除候选另见 Unreferenced，不计为已删除条目。
	Entries []string
	// BeforeErrors 是转换前的错误数，Validate 为真时有效。
	BeforeErrors int
	// Unreferenced 是包内无人引用的条目，即 6.2.1 e) 的删除候选。
	Unreferenced []string
	// UnreferencedDropped 是输出成功后确认删除的条目数；Plan 或 Apply 失败时为 0。
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
	// DropUnreferenced 允许真的删除无人引用的条目（GB/T 42133 6.2.1 e)）。
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
	kindDocument fileKind = 1 << iota
	// kindPageContent 是页面描述 XML（各页面的 Content.xml）。
	kindPageContent
	// kindTemplateContent 只执行图元动作清理，不参与正文页树的默认设置归并。
	kindTemplateContent
	// kindResource 包括公共、文档、页面和模板资源中的复合图元及底纹内容。
	kindResource
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
			{"GB/T 42133 6.3.3 b)", "去除图元对象中非文档内跳转的动作", kindPageContent | kindTemplateContent | kindResource, removeNonGotoActions(scopeGraphicUnit)},
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

// pruneActions 删除非 Goto 动作。空容器的注释和处理指令移到宿主中保留；
// 容器仍有扩展属性、元素或文本时保留原容器，返回删除的动作数。
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
	for _, action := range target.ChildElements() {
		if action.Tag != "Action" || action.NamespaceURI() != spec.Namespace {
			continue
		}
		if hasGotoChild(action) {
			continue
		}
		doomed = append(doomed, action)
	}
	for _, action := range doomed {
		target.RemoveChild(action)
	}
	if len(doomed) > 0 && !hasAdditionalAttributes(target) && onlyFormatting(target) {
		for _, token := range slices.Clone(target.Child) {
			switch token.(type) {
			case *etree.Comment, *etree.ProcInst:
				container.InsertChild(target, token)
			}
		}
		container.RemoveChild(target)
	}
	return len(doomed)
}

// 命名空间声明和格式空白不携带业务内容；其他属性、节点均须保留。
func hasAdditionalAttributes(element *etree.Element) bool {
	for _, attr := range element.Attr {
		if attr.Space != "xmlns" && !(attr.Space == "" && attr.Key == "xmlns") {
			return true
		}
	}
	return false
}

func onlyFormatting(element *etree.Element) bool {
	for _, token := range element.Child {
		switch token.(type) {
		case *etree.Comment, *etree.ProcInst:
			continue
		}
		if text, ok := token.(*etree.CharData); !ok || strings.TrimSpace(text.Data) != "" {
			return false
		}
	}
	return true
}

// Plan 计算将要做出的改动，不写任何文件。--dry-run 走这条路径。
func Plan(input any, options Options) (Result, error) {
	src, err := open(input, options.Limits)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = src.pkg.Close() }()
	options.Limits = src.pkg.limits

	// 预检不删除任何东西，因此不写 deletes、也不填 UnreferencedDropped。
	// 让 convert 区分这一点：否则 Result 会把"计划删 N 个"报成"已删除 N 个"，
	// 而 UnreferencedDropped 的语义是"实际被删除的条目数"。
	result, _, err := convert(src.pkg, options, true)
	return result, err
}

// Apply 执行转换并把结果写入 w。输出始终是新的字节流，输入不受影响。
//
// 转换、重建和校验全部成功后才写给 w；底层写入错误或短写会返回错误。
func Apply(input any, w io.Writer, options Options) (Result, error) {
	if w == nil {
		return Result{}, fmt.Errorf("OFD 输出写入器为空")
	}
	src, err := open(input, options.Limits)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = src.pkg.Close() }()
	options.Limits = src.pkg.limits

	result, operations, err := convert(src.pkg, options, false)
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
	if err := replace.Files(src.pkg.Package, operations, &buffer, replace.Options{
		Limits: options.Limits,
		// 必须显式指定：replace 的零值会归一化成 SignatureDrop，即删除全部
		// 签名条目。归档件应当保留签名作为证据，只提示摘要可能失效。
		Signatures: creator.SignaturePreserve,
		OnWarning:  options.OnWarning,
	}); err != nil {
		return result, err
	}
	if baseline != nil {
		if err := verifyNoRegression(baseline, buffer.Bytes(), options); err != nil {
			return result, err
		}
	}
	n, err := w.Write(buffer.Bytes())
	if err != nil {
		return result, fmt.Errorf("写入输出失败: %w", err)
	}
	if n != buffer.Len() {
		return result, fmt.Errorf("写入输出失败: %w", io.ErrShortWrite)
	}
	for _, operation := range operations {
		if operation.Kind == replace.OpDelete {
			result.UnreferencedDropped++
		}
	}
	return result, nil
}

// source 是归一化后的输入。保留路径形态是为了校验器能自己施加规模限制，
// 也让报告里的输入名称可读。
type source struct {
	path string
	data []byte
	pkg  *inputPackage
}

// validate 对原始输入做校验，取得转换前的问题基线。
func (s source) validate(options Options) validator.Report {
	report, err := validator.New(validatorOptions(options)...)
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
func open(input any, requested replace.Limits) (source, error) {
	limits, err := inputLimits(requested)
	if err != nil {
		return source{}, err
	}
	var src source
	var pkg *core.Package
	switch v := input.(type) {
	case string:
		pkg, err = core.OpenFile(v)
		src.path = v
	case []byte:
		pkg, err = core.OpenBytes(v)
		src.data = v
	default:
		return source{}, fmt.Errorf("不支持的输入类型 %T", input)
	}
	if err != nil {
		return source{}, err
	}
	src.pkg, err = checkInput(pkg, limits)
	if err != nil {
		_ = pkg.Close()
		return source{}, err
	}
	return src, nil
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
func verifyNoRegression(baseline map[string]bool, output []byte, options Options) error {
	report, err := validator.New(validatorOptions(options)...)
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
// dryRun 为真时只计算计划：不产生删除操作，也不虚报删除数量。
func convert(pkg *inputPackage, options Options, dryRun bool) (Result, []replace.Operation, error) {
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
	operations := map[string][]byte{}
	applyTo := func(path string, kind fileKind) error {
		doc, err := loadCurrentXML(pkg, path, operations)
		if err != nil {
			return err
		}
		root := doc.Root()
		if root == nil {
			return fmt.Errorf("%s 没有根元素", path)
		}
		var total int
		for _, s := range plan {
			if s.kind&kind == 0 {
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
		out, err := ofdxml.MarshalDocument(doc)
		if err != nil {
			return fmt.Errorf("序列化 %s 失败: %w", path, err)
		}
		operations[path] = out
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
	templates, err := contentEntries(pkg, docEntry, "./CommonData/TemplatePage")
	if err != nil {
		return result, nil, err
	}
	for _, template := range templates {
		if err := applyTo(template, kindTemplateContent); err != nil {
			return result, nil, err
		}
	}
	resources, err := resourceEntries(pkg, docEntry, append(slices.Clone(pages), templates...), operations)
	if err != nil {
		return result, nil, err
	}
	for _, resource := range resources {
		if err := applyTo(resource, kindResource); err != nil {
			return result, nil, err
		}
	}

	if err := normalizePageArea(pkg, docType, docEntry, pages, &result, operations, options); err != nil {
		return result, nil, err
	}

	var deletes []string
	if err := applyUnreferenced(pkg, operations, options, &result, &deletes, dryRun); err != nil {
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
	result.Entries = append(paths, deletes...)
	slices.Sort(result.Entries)
	result.Entries = slices.Compact(result.Entries)
	if len(result.Entries) > 0 {
		result.Entry = result.Entries[0]
	}
	return result, list, nil
}

// applyUnreferenced 实现 GB/T 42133 6.2.1 e)：去除与主入口及其嵌套引出文件
// 无关的文件。
//
// 只在显式开启 DropUnreferenced 时才删除，且闭包不完整时一律拒绝。删除不可逆，
// 而闭包的正确性完全依赖引用识别是否穷尽——漏掉一种引用形式就等于删掉在用文件。
// testdata/ofdrw/intro.ofd 的命名空间缺 "/2016" 后缀，解析失败后其引用的字体
// 全部落进候选，正是这个保护要拦住的情形。
func applyUnreferenced(pkg *inputPackage, operations map[string][]byte, options Options, result *Result, deletes *[]string, dryRun bool) error {
	index, err := validator.PackageReferencesWithOverrides(context.Background(), pkg.Package, operations, validatorOptions(options)...)
	if err != nil {
		return fmt.Errorf("解析引用闭包失败: %w", err)
	}
	orphans := index.Unreachable(pkg.Package)
	slices.Sort(orphans)
	result.ClosureIncomplete = !index.Complete()
	result.ClosureReason = index.IncompleteReason()
	if len(orphans) == 0 {
		return nil
	}
	result.Unreferenced = orphans
	if result.ClosureIncomplete {
		options.warn("引用闭包不完整，6.2.1 e) 的 %d 个删除候选未予删除：%s",
			len(orphans), result.ClosureReason)
		return nil
	}
	if !options.DropUnreferenced {
		options.warn("有 %d 个条目无人引用（GB/T 42133 6.2.1 e)），本次只报告不删除；加 --drop-unreferenced 执行删除",
			len(orphans))
		return nil
	}
	if dryRun {
		// 预检：候选照列，但不产生删除操作，UnreferencedDropped 保持 0。
		// 调用方据此显示"将删除"，而不是"已删除"。
		return nil
	}
	*deletes = append(*deletes, orphans...)
	for _, name := range orphans {
		delete(operations, name)
	}
	return nil
}

// validatorOptions 把转换选项里与校验相关的部分转成校验器选项。
func validatorOptions(options Options) []validator.Option {
	out := []validator.Option{
		validator.WithMaxEntries(options.Limits.MaxEntries),
		validator.WithMaxFileSize(options.Limits.MaxEntryBytes),
		validator.WithMaxXMLBytes(options.Limits.MaxEntryBytes),
		validator.WithMaxTotalSize(options.Limits.MaxTotalBytes),
	}
	if options.DocType != "" && spec.IsDocType(options.DocType) {
		out = append(out, validator.WithDocType(options.DocType))
	}
	return out
}

// pageEntries 解析出全部页面描述 XML 的包内路径。
//
// 路径取自 Document.xml 的 Pages/Page/@BaseLoc，按文档所在目录解析，不假定
// Doc_0 或固定的页目录名。模板页（TemplatePage）不在页树内，不参与 6.2.3。
func pageEntries(pkg *inputPackage, docEntry string) ([]string, error) {
	return contentEntries(pkg, docEntry, "./Pages/Page")
}

func contentEntries(pkg *inputPackage, docEntry, selector string) ([]string, error) {
	doc, err := loadXML(pkg, docEntry)
	if err != nil {
		return nil, err
	}
	base := models.StLoc(docEntry).Dir()
	var out []string
	for _, el := range doc.Root().FindElements(selector) {
		if el.NamespaceURI() != doc.Root().NamespaceURI() {
			continue
		}
		loc := strings.TrimSpace(el.SelectAttrValue("BaseLoc", ""))
		if loc == "" {
			continue
		}
		resolved, err := resolveEntryLocation(base, loc)
		if err != nil {
			return nil, fmt.Errorf("%s 的页面路径 %q 无效: %w", docEntry, loc, err)
		}
		entry, exists := pkg.Lookup(resolved)
		if !exists {
			// 页面描述缺失时不在此处报错：validator 会报引用缺失，转换不应
			// 因此中断，跳过即可。
			continue
		}
		resolved = entry.Path
		out = append(out, resolved)
	}
	return out, nil
}

// resolveEntryLocation 把 ST_Loc 引用规范化为安全的 ZIP 条目名。
func resolveEntryLocation(base models.StLoc, location string) (string, error) {
	name := strings.TrimPrefix(string(models.StLoc(strings.TrimSpace(location)).Resolve(base)), "/")
	if err := core.ValidateEntryName(name); err != nil {
		return "", err
	}
	return name, nil
}

// documentEntry 从 OFD.xml 的 DocRoot 解析出文档主体 XML 的包内路径，而不是
// 假定 Doc_0/Document.xml——目录名由生成方决定，写死会在非默认布局上找不到文件。
func documentEntry(pkg *inputPackage) (string, error) {
	root, err := readRoot(pkg)
	if err != nil {
		return "", err
	}
	var bodies []*etree.Element
	for _, el := range root.ChildElements() {
		if el.Tag == "DocBody" && el.NamespaceURI() == root.NamespaceURI() {
			bodies = append(bodies, el)
		}
	}
	if len(bodies) > 1 {
		return "", fmt.Errorf("%s 包含 %d 个 DocBody；GB/T 42133 6.2.1 c) 要求先拆分或展平，当前转换不支持多文档输入", spec.RootDocument, len(bodies))
	}
	if len(bodies) == 1 {
		for _, el := range bodies[0].ChildElements() {
			if el.Tag == "DocRoot" && el.NamespaceURI() == bodies[0].NamespaceURI() {
				if path := strings.TrimSpace(el.Text()); path != "" {
					name, err := resolveEntryLocation("", path)
					if err != nil {
						return "", fmt.Errorf("DocRoot %q 无效: %w", path, err)
					}
					if entry, exists := pkg.Lookup(name); exists {
						name = entry.Path
					}
					return name, nil
				}
			}
		}
	}
	return "", fmt.Errorf("%s 未声明 DocBody/DocRoot，无法定位文档主体 XML", spec.RootDocument)
}

// readRoot 解析并返回 OFD.xml 的根元素。
func readRoot(pkg *inputPackage) (*etree.Element, error) {
	entry, ok := pkg.Lookup(spec.RootDocument)
	if !ok {
		return nil, fmt.Errorf("包内找不到 %s", spec.RootDocument)
	}
	doc, err := loadXML(pkg, entry.Path)
	if err != nil {
		return nil, err
	}
	root := doc.Root()
	if root == nil || root.Tag != "OFD" {
		return nil, fmt.Errorf("%s 的根元素不是 ofd:OFD", spec.RootDocument)
	}
	return root, nil
}

// resolveDocType 判定本次转换使用的 profile：显式指定优先，否则按文件声明。
func resolveDocType(pkg *inputPackage, requested string) (string, error) {
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

// pageArea 是页面设置（CT_PageArea）的可比较形式。
//
// 刻意解析成数值再比较，而不是比对 XML 文本：文本比对会把 "210" 与 "210.0"
// 判成不同设置，于是本该省略的 Area 留在原地，6.2.3 b) 静默失效。
type pageArea struct {
	physical             models.StBox
	application, content *models.StBox
	bleed                *models.StBox
	// source 是原始 Area 或 PageArea 元素，跨文件补写设置时克隆它。
	//
	// 克隆而非新建节点：OFD 的命名空间由源文件声明形式决定（默认 xmlns 或
	// xmlns:ofd 前缀），手工给 etree 的 Space 赋 URI 会写出未声明的前缀，
	// 产出非良构 XML。
	source *etree.Element
}

// key 返回用于去重与比较的规范键。
func (a pageArea) key() string {
	var b strings.Builder
	writeBox(&b, &a.physical)
	for _, box := range []*models.StBox{a.application, a.content, a.bleed} {
		if box == nil {
			b.WriteString("-|")
			continue
		}
		writeBox(&b, box)
	}
	return b.String()
}

func writeBox(b *strings.Builder, box *models.StBox) {
	fmt.Fprintf(b, "%g %g %g %g|", box.X, box.Y, box.Width, box.Height)
}

// parsePageArea 解析同属 CT_PageArea 的 Area 或 PageArea。
func parsePageArea(el *etree.Element) (pageArea, bool) {
	if el == nil || (el.Tag != "Area" && el.Tag != "PageArea") || el.NamespaceURI() != spec.Namespace {
		return pageArea{}, false
	}
	// 数值比较不足以判断扩展属性、子节点或混合内容是否等价，保留原容器。
	if hasAdditionalAttributes(el) {
		return pageArea{}, false
	}
	area := pageArea{}
	physical := false
	seen := map[string]bool{}
	for _, token := range el.Child {
		if text, ok := token.(*etree.CharData); ok && strings.TrimSpace(text.Data) == "" {
			continue
		}
		child, ok := token.(*etree.Element)
		if !ok || child.NamespaceURI() != spec.Namespace || hasAdditionalAttributes(child) || seen[child.Tag] {
			return pageArea{}, false
		}
		seen[child.Tag] = true
		for _, token := range child.Child {
			if _, ok := token.(*etree.CharData); !ok {
				return pageArea{}, false
			}
		}
		box, ok := parseBox(child)
		if !ok {
			return pageArea{}, false
		}
		switch child.Tag {
		case "PhysicalBox":
			area.physical = box
			physical = true
		case "ApplicationBox":
			area.application = &box
		case "ContentBox":
			area.content = &box
		case "BleedBox":
			area.bleed = &box
		default:
			return pageArea{}, false
		}
	}
	if !physical {
		return pageArea{}, false
	}
	area.source = el
	return area, true
}

// parseBox 解析 ST_Box 文本值。ST_Box 是空格分隔的四个数字。
func parseBox(el *etree.Element) (models.StBox, bool) {
	var box models.StBox
	fields := strings.Fields(el.Text())
	if len(fields) != 4 {
		return box, false
	}
	targets := []*float64{&box.X, &box.Y, &box.Width, &box.Height}
	for i, field := range fields {
		value, err := strconv.ParseFloat(field, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return box, false
		}
		*targets[i] = value
	}
	// GB/T 33190—2016 §7.3 表 2：ST_Box 的宽和高应大于 0。
	if box.Width <= 0 || box.Height <= 0 {
		return box, false
	}
	return box, true
}

// normalizePageArea 实现 GB/T 42133 6.2.3 a) 与 b)。
//
// a) 把使用最多的页面设置确定为文档默认设置，写入 Document.xml 的
//
//	CommonData/PageArea；b) 页面设置与默认设置相同时，省略该页的 Area 节点。
//
// b) 的前提是省略后仍能正确渲染——已确认 internal/parser 与 pkg/analyzer 在
// 页面缺 Area 时都会回退到 CommonData/PageArea。CommonData/PageArea 在 XSD 中
// 没有 minOccurs（即必填），因此 a) 不可省略，即使所有页面都没写 Area 也必须
// 保留原有的默认值。
func normalizePageArea(pkg *inputPackage, docType, docEntry string, pages []string,
	result *Result, operations map[string][]byte, options Options) error {

	if docType != spec.DocTypeOFDA && docType != spec.DocTypeOFDH {
		return nil
	}
	if len(pages) == 0 {
		return nil
	}

	// §6.2.3 a) 统计页面实际使用的设置，包含继承 CommonData/PageArea 的页面。
	document, err := loadCurrentXML(pkg, docEntry, operations)
	if err != nil {
		return err
	}
	var original pageArea
	hasDefault := false
	if common := findChild(document.Root(), "CommonData"); common != nil {
		if existing := findChild(common, "PageArea"); existing != nil {
			original, hasDefault = parsePageArea(existing)
			if !hasDefault {
				options.warn("文档默认页面设置包含无法安全归并的内容或数值，页面设置归并已跳过")
				return nil
			}
		}
	}
	var order []string
	counts := map[string]int{}
	byKey := map[string]pageArea{}
	inherited := map[string]bool{}
	for _, page := range pages {
		doc, err := loadCurrentXML(pkg, page, operations)
		if err != nil {
			return err
		}
		if doc.Root().NamespaceURI() != spec.Namespace {
			options.warn("%s 不在 OFD 命名空间下，无法确定全部页面的有效设置，页面设置归并已跳过", page)
			return nil
		}
		el := findChild(doc.Root(), "Area")
		area, ok := parsePageArea(el)
		if el == nil {
			area, ok = original, hasDefault
			inherited[page] = true
		}
		if !ok {
			options.warn("%s 的有效页面设置无法确定，页面设置归并已跳过", page)
			return nil
		}
		key := area.key()
		if _, seen := byKey[key]; !seen {
			order = append(order, key)
			byKey[key] = area
		}
		counts[key]++
	}
	if len(order) == 0 {
		return nil
	}

	// 使用最多者胜出；并列时取页面顺序靠前者，保证同一输入结果稳定。
	winner := order[0]
	for _, key := range order[1:] {
		if counts[key] > counts[winner] {
			winner = key
		}
	}
	defaultArea := byKey[winner]

	// a) 写入 CommonData/PageArea。
	//
	// established 为假表示默认设置没能落到 CommonData/PageArea——例如 Document.xml
	// 自身不在 OFD 命名空间下（testdata/ofdrw/intro.ofd 就是这种，28 页用
	// http://www.ofdspec.org/2016、42 页用遗留的 http://www.ofdspec.org，混在一个包里）。
	// 此时页面省略 Area 后会回退到 CommonData 里那个未必相同的旧默认值，页面尺寸
	// 会静默改变，所以必须跳过 b)。
	established, err := writeDefaultPageArea(pkg, docEntry, defaultArea, operations)
	if err != nil {
		return err
	}
	if !established {
		options.warn("未能把页面默认设置写入 CommonData/PageArea（文档根节点不在 OFD 命名空间下或缺少 CommonData），" +
			"GB/T 42133 6.2.3 b) 的页面 Area 省略已跳过")
		return nil
	}
	if !hasDefault || original.key() != winner {
		result.Changes = append(result.Changes, Change{
			Entry:  docEntry,
			Clause: "GB/T 42133 6.2.3 a)",
			Action: fmt.Sprintf("将使用最多的页面设置（%d/%d 页）确定为文档默认设置", counts[winner], len(pages)),
			Count:  1,
		})
	}

	// b) 省略与默认设置相同的页面 Area
	omitted := 0
	processed := map[string]bool{}
	for _, page := range pages {
		if processed[page] {
			continue
		}
		processed[page] = true
		doc, err := loadCurrentXML(pkg, page, operations)
		if err != nil {
			return err
		}
		if inherited[page] {
			if original.key() == winner {
				continue
			}
			// 默认值变化时，把旧默认值显式落到原本依赖它的页面，保持显示尺寸。
			insertPageSetting(doc.Root(), original.element("Area"))
			result.Changes = append(result.Changes, Change{Entry: page, Clause: "GB/T 42133 6.2.3 a)", Action: "显式保留页面原先继承的默认设置", Count: 1})
		} else {
			el := findChild(doc.Root(), "Area")
			area, ok := parsePageArea(el)
			if !ok || area.key() != winner {
				continue
			}
			doc.Root().RemoveChild(el)
			omitted++
		}
		out, err := ofdxml.MarshalDocument(doc)
		if err != nil {
			return fmt.Errorf("序列化 %s 失败: %w", page, err)
		}
		operations[page] = out
	}
	if omitted > 0 {
		result.Changes = append(result.Changes, Change{
			Entry:  "(页面设置)",
			Clause: "GB/T 42133 6.2.3 b)",
			Action: "省略与文档默认设置相同的页面 Area 节点",
			Count:  omitted,
		})
	}
	return nil
}

// insertPageSetting 保持 Page 的 xs:sequence：Area 位于 Template/PageRes 之后、Content/Actions 之前。
func insertPageSetting(page, area *etree.Element) {
	for _, child := range page.ChildElements() {
		if child.NamespaceURI() == spec.Namespace && (child.Tag == "Content" || child.Tag == "Actions") {
			page.InsertChild(child, area)
			return
		}
	}
	page.AddChild(area)
}

func loadCurrentXML(pkg *inputPackage, name string, operations map[string][]byte) (*etree.Document, error) {
	if data, ok := operations[name]; ok {
		doc, err := ofdxml.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("解析 %s 失败: %w", name, err)
		}
		return doc, nil
	}
	return loadXML(pkg, name)
}

// loadXML 读取并解析包内一个 XML 条目。
func loadXML(pkg *inputPackage, path string) (*etree.Document, error) {
	data, err := pkg.Read(path)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	doc, err := ofdxml.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", path, err)
	}
	return doc, nil
}

// writeDefaultPageArea 把默认页面设置写入 CommonData/PageArea。
//
// 返回值表示「文档默认设置是否已确立为 area」，而非「本次是否改动了字节」：
// 既有默认值已经等于 area 时同样算确立，此时不重复改写文件。
func writeDefaultPageArea(pkg *inputPackage, docEntry string, area pageArea,
	operations map[string][]byte) (bool, error) {

	doc, err := loadCurrentXML(pkg, docEntry, operations)
	if err != nil {
		return false, err
	}
	root := doc.Root()
	common := findChild(root, "CommonData")
	if common == nil {
		// 没有 CommonData 就无处安放默认设置，属文件本身不合规，不在此处构造。
		return false, nil
	}
	if existing := findChild(common, "PageArea"); existing != nil {
		if current, ok := parsePageArea(existing); ok && current.key() == area.key() {
			// 既有默认设置已是目标值，无需改写；b) 仍可安全执行。
			return true, nil
		}
		common.RemoveChild(existing)
	}
	replacement := area.element("PageArea")
	if replacement == nil {
		return false, nil
	}
	insertPageArea(common, replacement)
	out, err := ofdxml.MarshalDocument(doc)
	if err != nil {
		return false, fmt.Errorf("序列化 %s 失败: %w", docEntry, err)
	}
	operations[docEntry] = out
	return true, nil
}

// insertPageArea 按 CT_CommonData 的 xs:sequence 顺序插入 PageArea：紧随
// MaxUnitID，且在 PublicRes 之前。序列类型要求顺序固定，插到末尾会让输出
// 通不过 XSD 校验。
func insertPageArea(common, area *etree.Element) {
	// 插在 MaxUnitID 之后的第一个元素之前；若 MaxUnitID 是唯一元素则追加到末尾。
	// 用 InsertChild(before) 而不是按下标插入：etree 的子节点序列含空白文本，
	// 下标的含义随原文件有无格式化空白而变，按元素定位才稳定。
	for _, child := range common.ChildElements() {
		if child.Tag == "MaxUnitID" && child.NamespaceURI() == spec.Namespace {
			continue
		}
		if child.NamespaceURI() != spec.Namespace {
			continue
		}
		common.InsertChild(child, area)
		return
	}
	common.AddChild(area)
}

// findChild 查找 OFD 命名空间下的直接子元素。
func findChild(parent *etree.Element, tag string) *etree.Element {
	for _, el := range parent.ChildElements() {
		if el.Tag == tag && el.NamespaceURI() == spec.Namespace {
			return el
		}
	}
	return nil
}

// element 返回该页面设置的副本，改成指定标签名，并补齐从源文件继承的命名空间。
func (a pageArea) element(tag string) *etree.Element {
	if a.source == nil {
		// 理论上不会发生：pageArea 只由 parsePageArea 产生。
		return nil
	}
	clone := a.source.Copy()
	clone.Tag = tag
	for parent := a.source.Parent(); parent != nil; parent = parent.Parent() {
		for _, attr := range parent.Attr {
			if (attr.Space == "xmlns" || attr.Key == "xmlns") && clone.SelectAttr(attr.FullKey()) == nil {
				clone.CreateAttr(attr.FullKey(), attr.Value)
			}
		}
	}
	return clone
}
