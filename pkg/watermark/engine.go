package watermark

import (
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/spec"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/replace"
)

type actionKind int

const (
	actionAdd actionKind = iota
	actionReplace
	actionRemove
)

// engine 收集水印编辑产生的包条目变更（新增/修改/删除），最后转换为 replace.Operation。
type engine struct {
	options Options
	pkg     *core.Package
	// filesInput 是传给 replace.Files 的原始输入（路径/字节/*core.Package）。
	filesInput any
	// closePkg 表示本包是否负责关闭 pkg；*core.Package 输入由 replace.Files 负责关闭。
	closePkg bool

	changes map[string][]byte
	deletes map[string]bool
	// newIDs 记录本次运行新分配的对象 ID，避免注解与图片资源互相冲突。
	newIDs map[uint64]bool
	// releasedMedia 记录本次 Remove/Replace 从注解中移除的图片资源 ID，
	// 用于在页面写回后回收不再被任何页面引用的水印图片。
	releasedMedia map[uint64]bool
}

func (e *engine) run(input any, target Target, wm Watermark, action actionKind) error {
	pkg, filesInput, closePkg, err := openAny(input)
	if err != nil {
		return err
	}
	e.pkg = pkg
	e.filesInput = filesInput
	e.closePkg = closePkg
	e.changes = make(map[string][]byte)
	e.deletes = make(map[string]bool)
	e.newIDs = make(map[uint64]bool)
	e.releasedMedia = make(map[uint64]bool)
	if e.closePkg {
		defer func() { _ = pkg.Close() }()
	}

	var root models.OFD
	if err := pkg.ReadXML(spec.RootDocument, &root); err != nil {
		return fmt.Errorf("读取 %s 失败: %w", spec.RootDocument, err)
	}

	bodyIndexes := target.docBodyIndexes(len(root.DocBodies))
	if len(bodyIndexes) == 0 {
		return docBodyOutOfRange(target.Document, len(root.DocBodies))
	}
	matched := false
	for _, bodyIndex := range bodyIndexes {
		found, err := e.runDocBody(root.DocBodies[bodyIndex], target, wm, action)
		if err != nil {
			return err
		}
		if found {
			matched = true
		}
	}
	if !matched {
		return watermarkExistsError()
	}
	return nil
}

func (t Target) docBodyIndexes(count int) []int {
	if t.Document < 0 {
		indexes := make([]int, count)
		for i := range indexes {
			indexes[i] = i
		}
		return indexes
	}
	if t.Document >= count {
		return nil
	}
	return []int{t.Document}
}

// runDocBody 处理单个文档体，返回本文档体内是否至少匹配到一处水印操作目标。
func (e *engine) runDocBody(body models.DocBody, target Target, wm Watermark, action actionKind) (bool, error) {
	docEntry := strings.TrimLeft(body.DocRoot.Resolve("/").String(), "/")
	if docEntry == "" {
		return false, fmt.Errorf("文档体 DocRoot 为空")
	}
	baseLoc := path.Dir(docEntry)

	var document models.Document
	if err := e.pkg.ReadXML(docEntry, &document); err != nil {
		return false, err
	}

	// 文档体前缀：无既有注解索引时，从 Document.xml 根元素推断命名空间风格，
	// 使新建的索引与页面注解文件与文档体保持一致。
	bodyPrefix := ""
	if _, root, err := indexDocRootOf(e.pkg, docEntry); err == nil && root != nil {
		bodyPrefix = root.Space
	}

	// 文档级水印权限检查。
	if document.Permissions != nil && document.Permissions.Watermark != nil &&
		!*document.Permissions.Watermark && !e.options.SkipPermissionsCheck {
		return false, fmt.Errorf("文档禁止添加或修改水印（Permissions/Watermark=false）")
	}

	pageIDs, err := targetPageIDs(target, document)
	if err != nil {
		return false, err
	}
	if len(pageIDs) == 0 {
		return false, watermarkExistsError()
	}

	index := &annotationIndex{
		baseLoc:    baseLoc,
		docEntry:   docEntry,
		bodyPrefix: bodyPrefix,
		pkg:        e.pkg,
		engine:     e,
	}
	if err := index.load(document); err != nil {
		return false, err
	}

	// 图片水印：嵌入图片资源并生成外观片段。
	if wm.Image != nil && (action == actionAdd || action == actionReplace) {
		fragment, err := index.addImageResource(document, wm.Image, wm.Boundary)
		if err != nil {
			return false, err
		}
		wm.Appearance = fragment
	}

	matched := false
	touched := make([]*pageTree, 0, len(pageIDs))
	for _, pageID := range pageIDs {
		tree, err := index.loadPage(pageID)
		if err != nil {
			return false, err
		}
		pageMatched, err := e.applyPage(index, tree, wm, action, target.MatchIDs)
		if err != nil {
			return false, err
		}
		if pageMatched {
			matched = true
			touched = append(touched, tree)
		}
	}
	if !matched {
		return false, nil
	}

	for _, tree := range touched {
		if err := index.flushPage(tree); err != nil {
			return false, err
		}
	}
	// 页面写回后回收不再被任何页面引用的水印图片资源。
	if err := index.reclaimMedia(document); err != nil {
		return false, err
	}
	if err := index.flush(); err != nil {
		return false, err
	}
	return true, nil
}

// collectAnnotMedia 收集注解 Appearance 中 ImageObject 引用的图片资源 ID。
func collectAnnotMedia(annot *etree.Element, into map[uint64]bool) {
	appearance := annot.FindElement("Appearance")
	if appearance == nil {
		return
	}
	for _, obj := range appearance.ChildElements() {
		if obj.Tag != "ImageObject" {
			continue
		}
		if id, err := strconv.ParseUint(obj.SelectAttrValue("ResourceID", ""), 10, 64); err == nil {
			into[id] = true
		}
	}
}

func targetPageIDs(target Target, document models.Document) ([]uint64, error) {
	total := len(document.Pages.Pages)
	if total == 0 {
		return nil, fmt.Errorf("文档没有页面")
	}
	if len(target.Pages) == 0 {
		ids := make([]uint64, total)
		for i, page := range document.Pages.Pages {
			ids[i] = uint64(page.ID)
		}
		return ids, nil
	}
	ids := make([]uint64, 0, len(target.Pages))
	for _, index := range target.Pages {
		if index < 0 || index >= total {
			return nil, fmt.Errorf("页面下标 %d 超出范围（共 %d 页）", index, total)
		}
		ids = append(ids, uint64(document.Pages.Pages[index].ID))
	}
	return ids, nil
}

func (e *engine) applyPage(index *annotationIndex, tree *pageTree,
	wm Watermark, action actionKind, matchIDs []uint64) (bool, error) {
	root := tree.root
	if root == nil {
		root = tree.doc.CreateElement(prefixTag(tree.prefix, "PageAnnot"))
		setNamespace(root, tree.prefix)
		tree.root = root
	}

	switch action {
	case actionAdd:
		used := existingAnnotIDs(root)
		for id := range e.newIDs {
			used[id] = true
		}
		// 注解与文档级媒体共享 ID 空间，避免自动分配的注解 ID 撞上既有图片资源。
		for id := range index.mediaIDs {
			used[id] = true
		}
		id, err := allocateAnnotID(wm.ID, used, index.maxUnitID)
		if err != nil {
			return false, err
		}
		if wm.ID == 0 {
			e.newIDs[id] = true
		}
		if err := appendAnnotElement(root, tree.prefix, wm, id); err != nil {
			return false, err
		}
		tree.dirty = true
		return true, nil

	case actionReplace, actionRemove:
		matched, err := e.mutateWatermarks(root, tree.prefix, wm, action, matchIDs)
		if err != nil {
			return false, err
		}
		if !matched {
			return false, nil
		}
		if action == actionRemove && !hasAnnotElements(root) {
			// 页面注解文件已无任何注解：整文件删除。
			tree.empty = true
		}
		tree.dirty = true
		return true, nil
	}
	return false, fmt.Errorf("不支持的操作: %v", action)
}

// mutateWatermarks 对匹配的水印注解执行替换或删除，返回是否有匹配。
func (e *engine) mutateWatermarks(root *etree.Element, prefix string, wm Watermark,
	action actionKind, matchIDs []uint64) (bool, error) {
	var targets []*etree.Element
	for _, child := range root.ChildElements() {
		if child.Tag != "Annot" || child.SelectAttrValue("Type", "") != watermarkType {
			continue
		}
		if matchAnnotID(child, matchIDs) {
			targets = append(targets, child)
		}
	}
	matched := false
	for _, child := range targets {
		if e.options.readOnlyBlocked(child) && !e.options.SkipReadOnlyCheck {
			return false, fmt.Errorf("水印注解 %s 为只读，拒绝%s（可用 SkipReadOnlyCheck 跳过）",
				child.SelectAttrValue("ID", ""), actionVerb(action))
		}
		switch action {
		case actionReplace:
			// 替换前收集旧注解引用的水印图片资源，替换后按引用情况回收。
			collectAnnotMedia(child, e.releasedMedia)
			if err := replaceAnnotElement(root, child, prefix, wm); err != nil {
				return false, err
			}
		case actionRemove:
			collectAnnotMedia(child, e.releasedMedia)
			root.RemoveChild(child)
		}
		matched = true
	}
	return matched, nil
}

func (o Options) readOnlyBlocked(el *etree.Element) bool {
	value := el.SelectAttrValue("ReadOnly", "")
	if value == "" {
		return true // XSD 默认 true
	}
	blocked, err := strconv.ParseBool(value)
	return err != nil || blocked
}

func actionVerb(action actionKind) string {
	if action == actionReplace {
		return "修改"
	}
	return "修改或删除"
}

func matchAnnotID(el *etree.Element, matchIDs []uint64) bool {
	if len(matchIDs) == 0 {
		return true
	}
	id, err := strconv.ParseUint(el.SelectAttrValue("ID", ""), 10, 64)
	if err != nil {
		return false
	}
	for _, want := range matchIDs {
		if want == id {
			return true
		}
	}
	return false
}

func existingAnnotIDs(root *etree.Element) map[uint64]bool {
	used := make(map[uint64]bool)
	for _, child := range root.ChildElements() {
		if child.Tag == "Annot" {
			if id, err := strconv.ParseUint(child.SelectAttrValue("ID", ""), 10, 64); err == nil {
				used[id] = true
			}
		}
	}
	return used
}

func allocateAnnotID(want uint64, used map[uint64]bool, maxUnitID uint64) (uint64, error) {
	if want != 0 {
		if used[want] {
			return 0, fmt.Errorf("注解 ID %d 已存在", want)
		}
		return want, nil
	}
	next := maxUnitID
	for next < ^uint64(0) {
		next++
		if !used[next] {
			return next, nil
		}
	}
	return 0, fmt.Errorf("无法分配空闲的注解 ID")
}

// nextFreeID 返回从 maxUnitID+1 起第一个未被占用的对象 ID。
func nextFreeID(used map[uint64]bool, maxUnitID uint64) (uint64, error) {
	next := maxUnitID
	for next < ^uint64(0) {
		next++
		if !used[next] {
			return next, nil
		}
	}
	return 0, fmt.Errorf("无法分配空闲的对象 ID")
}

func hasAnnotElements(root *etree.Element) bool {
	return len(root.ChildElements()) > 0
}

// operations 把所有条目变更转换为替换操作，条目存在则 set、否则 add，删除条目为 delete。
func (e *engine) operations() ([]replace.Operation, error) {
	names := make([]string, 0, len(e.changes)+len(e.deletes))
	for name := range e.changes {
		names = append(names, name)
	}
	for name := range e.deletes {
		names = append(names, name)
	}
	sort.Strings(names)
	var operations []replace.Operation
	for _, name := range names {
		if e.deletes[name] {
			operations = append(operations, replace.Operation{Kind: replace.OpDelete, Name: name})
			continue
		}
		kind := replace.OpSet
		if !e.pkg.Has(name) {
			kind = replace.OpAdd
		}
		operations = append(operations, replace.Operation{Kind: kind, Name: name, Data: e.changes[name]})
	}
	return operations, nil
}

// openAny 打开输入并同时保留传给 replace.Files 的原始输入。io.Reader 会被读到内存。
// 返回 closePkg 表示调用方是否应关闭 pkg；输入为 *core.Package 时由 replace.Files 关闭。
func openAny(input any) (*core.Package, any, bool, error) {
	switch value := input.(type) {
	case string:
		pkg, err := core.OpenFile(value)
		return pkg, value, true, err
	case []byte:
		pkg, err := core.OpenBytes(value)
		return pkg, value, true, err
	case io.Reader:
		data, err := io.ReadAll(value)
		if err != nil {
			return nil, nil, false, err
		}
		pkg, err := core.OpenBytes(data)
		return pkg, data, true, err
	case *core.Package:
		return value, value, false, nil
	default:
		return nil, nil, false, fmt.Errorf("不支持的输入类型: %T", input)
	}
}

// pageTree 表示一个页面的注解文件及其状态。
type pageTree struct {
	doc    *etree.Document
	root   *etree.Element
	prefix string
	entry  string // 条目名；首次写入前决定
	pageID uint64
	dirty  bool
	empty  bool // Remove 后已无注解，应删除文件
}

// annotationIndex 管理单个文档体的注解索引（Annotations.xml）与页面注解文件布局。
type annotationIndex struct {
	baseLoc    string
	docEntry   string
	bodyPrefix string
	pkg        *core.Package
	engine     *engine

	indexEntry string // Annotations.xml 条目名，空表示文档没有注解索引
	indexDir   string
	indexExist bool

	maxUnitID uint64
	prefix    string
	// mediaIDs 是文档级已注册的媒体资源 ID，与注解共享 ID 空间。
	mediaIDs map[uint64]bool

	pages map[uint64]string // pageID -> 页面注解文件条目名
}

func (a *annotationIndex) load(document models.Document) error {
	a.prefix = a.bodyPrefix
	a.pages = make(map[uint64]string)
	a.indexExist = false
	a.maxUnitID = uint64(document.CommonData.MaxUnitID)
	a.mediaIDs = a.loadMediaIDs(document)
	// 尚未声明索引时使用默认布局，首次添加水印时再创建。
	a.indexEntry = a.baseLoc + "/Annotations.xml"
	a.indexDir = a.baseLoc

	if document.Annotations != nil {
		loc := document.Annotations.Resolve(models.StLoc(a.baseLoc))
		a.indexEntry = strings.TrimLeft(loc.String(), "/")
		a.indexDir = path.Dir(a.indexEntry)
	}
	if _, err := a.pkg.Read(a.indexEntry); err != nil {
		// 索引声明但缺失：按无索引处理。
		return nil
	}
	a.indexExist = true

	var index models.Annotations
	if err := a.pkg.ReadXML(a.indexEntry, &index); err != nil {
		return fmt.Errorf("解析 %s 失败: %w", a.indexEntry, err)
	}
	if root, err := a.indexDocRoot(); err == nil && root != nil {
		a.prefix = root.Space
	}
	for _, page := range index.Pages {
		if page.FileLoc == "" {
			continue
		}
		location := page.FileLoc
		if !strings.HasPrefix(location.String(), "/") {
			location = location.Resolve(models.StLoc(a.indexDir))
		}
		a.pages[uint64(page.PageID)] = strings.TrimLeft(location.String(), "/")
	}
	return nil
}

func (a *annotationIndex) indexDocRoot() (*etree.Element, error) {
	_, root, err := a.docRoot(a.indexEntry)
	return root, err
}

// docRoot 解析条目为 etree 文档，返回文档与其根元素。
func (a *annotationIndex) docRoot(entry string) (*etree.Document, *etree.Element, error) {
	return indexDocRootOf(a.pkg, entry)
}

// indexDocRootOf 解析包内条目为 etree 文档，返回文档与其根元素。
func indexDocRootOf(pkg *core.Package, entry string) (*etree.Document, *etree.Element, error) {
	data, err := pkg.Read(entry)
	if err != nil {
		return nil, nil, err
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		return nil, nil, err
	}
	if root := doc.Root(); root != nil {
		return doc, root, nil
	}
	return nil, nil, fmt.Errorf("%s 缺少根元素", entry)
}

// pendingDocRoot 解析条目为 etree 文档，优先使用本引擎尚未写回的最新内容。
func (a *annotationIndex) pendingDocRoot(entry string) (*etree.Document, *etree.Element, error) {
	if data, ok := a.engine.changes[entry]; ok {
		doc := etree.NewDocument()
		if err := doc.ReadFromBytes(data); err != nil {
			return nil, nil, err
		}
		if root := doc.Root(); root != nil {
			return doc, root, nil
		}
		return nil, nil, fmt.Errorf("%s 缺少根元素", entry)
	}
	return a.docRoot(entry)
}

// pageEntryFor 返回某页的注解文件条目名（存在或新建）。新建文件使用 creator
// 风格命名：已有索引时放 <索引目录>/Page_<id>.xml，新建索引时放
// <BaseLoc>/Annotations/Page_<id>.xml。索引中的 FileLoc 总是相对索引目录。
func (a *annotationIndex) pageEntryFor(pageID uint64) (string, bool) {
	if entry, ok := a.pages[pageID]; ok {
		return entry, true
	}
	if a.indexExist {
		return fmt.Sprintf("%s/Page_%d.xml", a.indexDir, pageID), false
	}
	return fmt.Sprintf("%s/Annotations/Page_%d.xml", a.baseLoc, pageID), false
}

// loadPage 加载某个页面的注解文件；不存在时创建空的 PageAnnot 文档。
func (a *annotationIndex) loadPage(pageID uint64) (*pageTree, error) {
	entry, existed := a.pageEntryFor(pageID)
	tree := &pageTree{entry: entry, pageID: pageID}
	if existed {
		data, err := a.pkg.Read(entry)
		if err == nil {
			doc := etree.NewDocument()
			if err := doc.ReadFromBytes(data); err != nil {
				return nil, fmt.Errorf("解析 %s 失败: %w", entry, err)
			}
			tree.doc = doc
			tree.root = doc.Root()
			if tree.root != nil {
				tree.prefix = tree.root.Space
			}
			return tree, nil
		}
	}
	// 索引声明但文件缺失，或为首建页面：新建文件。
	tree.doc = etree.NewDocument()
	tree.doc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)
	tree.prefix = a.prefix
	return tree, nil
}

// flushPage 把页面注解文档写回对应条目或标记删除。
func (a *annotationIndex) flushPage(tree *pageTree) error {
	if tree.empty {
		a.engine.deletes[tree.entry] = true
		delete(a.pages, tree.pageID)
		return nil
	}
	if !tree.dirty {
		return nil
	}
	if a.pages[tree.pageID] == "" {
		a.pages[tree.pageID] = tree.entry
	}
	data, err := tree.doc.WriteToBytes()
	if err != nil {
		return fmt.Errorf("序列化页面注解失败: %w", err)
	}
	a.engine.changes[tree.entry] = data
	return nil
}

// flush 写回注解索引文件，并按需更新 Document.xml。
func (a *annotationIndex) flush() error {
	if len(a.pages) == 0 {
		// 文档不再有注解索引：删除索引与 Document.xml 中的 <Annotations>。
		if a.indexExist {
			a.engine.deletes[a.indexEntry] = true
		}
		return a.engine.dropDocumentAnnotations(a)
	}
	indexData, err := a.buildIndex()
	if err != nil {
		return err
	}
	if a.indexExist {
		a.engine.changes[a.indexEntry] = indexData
	} else {
		a.engine.changes[a.indexEntry] = indexData
		if err := a.engine.setDocumentAnnotations(a); err != nil {
			return err
		}
	}
	return nil
}

func (a *annotationIndex) buildIndex() ([]byte, error) {
	prefix := a.prefix
	tree := etree.NewDocument()
	tree.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)
	root := tree.CreateElement(prefixTag(prefix, "Annotations"))
	setNamespace(root, prefix)
	ids := make([]uint64, 0, len(a.pages))
	for pageID := range a.pages {
		ids = append(ids, pageID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, pageID := range ids {
		entry := a.pages[pageID]
		pageEl := root.CreateElement(prefixTag(prefix, "Page"))
		pageEl.CreateAttr("PageID", strconv.FormatUint(pageID, 10))
		locEl := pageEl.CreateElement(prefixTag(prefix, "FileLoc"))
		locEl.SetText(a.fileLocValue(entry))
	}
	return tree.WriteToBytes()
}

// fileLocValue 计算 FileLoc 在索引中的相对路径（相对索引目录）。
func (a *annotationIndex) fileLocValue(entry string) string {
	if a.indexDir != "" && strings.HasPrefix(entry, a.indexDir+"/") {
		return strings.TrimPrefix(entry, a.indexDir+"/")
	}
	return entry
}

// indexEntryValue 是 Document.xml 中 <Annotations> 的文本值（相对 BaseLoc）。
func (a *annotationIndex) indexEntryValue() string {
	if a.indexDir != "" && strings.HasPrefix(a.indexEntry, a.indexDir+"/") {
		return strings.TrimPrefix(a.indexEntry, a.indexDir+"/")
	}
	return "Annotations.xml"
}

func (e *engine) setDocumentAnnotations(a *annotationIndex) error {
	data, err := e.pkg.Read(a.docEntry)
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %w", a.docEntry, err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		return fmt.Errorf("解析 %s 失败: %w", a.docEntry, err)
	}
	root := doc.Root()
	prefix := root.Space
	for _, child := range root.ChildElements() {
		if child.Tag == "Annotations" {
			return nil // 已存在
		}
	}
	el := root.CreateElement(prefixTag(prefix, "Annotations"))
	el.SetText(a.indexEntryValue())
	if err := insertAnnotationsInOrder(root, el); err != nil {
		return err
	}
	data, err = doc.WriteToBytes()
	if err != nil {
		return fmt.Errorf("序列化 %s 失败: %w", a.docEntry, err)
	}
	e.changes[a.docEntry] = data
	return nil
}

func (e *engine) dropDocumentAnnotations(a *annotationIndex) error {
	data, err := e.pkg.Read(a.docEntry)
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %w", a.docEntry, err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		return fmt.Errorf("解析 %s 失败: %w", a.docEntry, err)
	}
	root := doc.Root()
	var target *etree.Element
	for _, child := range root.ChildElements() {
		if child.Tag == "Annotations" {
			target = child
			break
		}
	}
	if target == nil {
		return nil
	}
	root.RemoveChild(target)
	data, err = doc.WriteToBytes()
	if err != nil {
		return fmt.Errorf("序列化 %s 失败: %w", a.docEntry, err)
	}
	e.changes[a.docEntry] = data
	return nil
}

// insertAnnotationsInOrder 按 XSD 顺序插入 <Annotations>：位于 CustomTags 之前、
// Bookmarks 之后。
func insertAnnotationsInOrder(root, el *etree.Element) error {
	// 插入位置放在 CustomTags/Attachments/Extensions 之前。
	for _, tag := range []string{"CustomTags", "Attachments", "Extensions"} {
		if target := findElementByTag(root, tag); target != nil {
			target.Parent().InsertChildAt(target.Index(), el)
			return nil
		}
	}
	root.AddChild(el)
	return nil
}

func findElementByTag(root *etree.Element, tag string) *etree.Element {
	for _, child := range root.ChildElements() {
		if child.Tag == tag {
			return child
		}
	}
	return nil
}

func setNamespace(el *etree.Element, prefix string) {
	if prefix == "" {
		el.CreateAttr("xmlns", spec.Namespace)
	} else {
		el.CreateAttr("xmlns:"+prefix, spec.Namespace)
	}
}

func prefixTag(prefix, tag string) string {
	if prefix == "" {
		return tag
	}
	return prefix + ":" + tag
}

// loadMediaIDs 收集文档级资源文件中已注册的媒体资源 ID。
func (a *annotationIndex) loadMediaIDs(document models.Document) map[uint64]bool {
	ids := make(map[uint64]bool)
	if len(document.CommonData.DocumentRes) == 0 {
		return ids
	}
	resEntry := strings.TrimLeft(document.CommonData.DocumentRes[0].Resolve(models.StLoc(a.baseLoc)).String(), "/")
	if resEntry == "" {
		return ids
	}
	if _, root, err := a.docRoot(resEntry); err == nil && root != nil {
		for id := range multiMediaIDs(root) {
			ids[id] = true
		}
	}
	return ids
}

// addImageResource 把图片水印写入文档资源，返回生成的图片外观 XML 片段。
// 图片条目写入 <BaseLoc>/Images/，并在 DocumentRes.xml 的 <MultiMedias> 中注册。
func (a *annotationIndex) addImageResource(document models.Document, img *Image, boundary *creator.Box) ([]byte, error) {
	if len(img.Data) == 0 {
		return nil, fmt.Errorf("图片水印数据为空")
	}
	format := strings.ToUpper(strings.TrimSpace(img.Format))
	if format == "" {
		format = "PNG"
	}
	if detected := detectImageFormat(img.Data); detected != "" {
		format = detected
	}
	if format != "PNG" && format != "JPEG" {
		return nil, fmt.Errorf("不支持的图片格式 %q，仅支持 PNG/JPEG", format)
	}
	data := img.Data
	if img.Opacity != nil {
		if format != "PNG" {
			return nil, fmt.Errorf("仅支持对 PNG 图片应用不透明度")
		}
		baked, err := bakeImageAlpha(data, *img.Opacity)
		if err != nil {
			return nil, err
		}
		data = baked
	}

	if len(document.CommonData.DocumentRes) == 0 {
		return nil, fmt.Errorf("文档没有文档级资源（DocumentRes），无法嵌入图片水印")
	}
	resEntry := strings.TrimLeft(document.CommonData.DocumentRes[0].Resolve(models.StLoc(a.baseLoc)).String(), "/")
	if resEntry == "" {
		return nil, fmt.Errorf("无法确定文档级资源文件位置")
	}

	// 媒体文件按「资源文件目录 + Res/BaseLoc」寻址，字节放入该目录。
	mediaBase := strings.TrimLeft(path.Join(path.Dir(resEntry), ""), "/")
	if _, root, err := a.docRoot(resEntry); err == nil {
		baseLoc := root.SelectAttrValue("BaseLoc", "")
		mediaBase = strings.TrimLeft(path.Join(path.Dir(resEntry), baseLoc), "/")
	}

	used := make(map[uint64]bool)
	if _, root, err := a.docRoot(resEntry); err == nil {
		for id := range multiMediaIDs(root) {
			used[id] = true
		}
	}
	for id := range a.engine.newIDs {
		used[id] = true
	}
	imageID, err := nextFreeID(used, a.maxUnitID)
	if err != nil {
		return nil, err
	}
	a.engine.newIDs[imageID] = true
	if a.mediaIDs != nil {
		a.mediaIDs[imageID] = true
	}

	name := fmt.Sprintf("Image_%d.%s", imageID, formatExt(format))
	mediaFile := "Images/" + name
	entry := mediaFile
	if mediaBase != "" {
		entry = mediaBase + "/" + mediaFile
	}
	a.engine.changes[entry] = data
	if err := a.patchResImage(resEntry, imageID, format, mediaFile); err != nil {
		return nil, err
	}

	aspect := 0.0
	if img.Height == 0 {
		if width, height := imagePixels(data); width > 0 && height > 0 {
			aspect = float64(width) / float64(height)
		}
	}
	area := creator.Box{Width: 210, Height: 297}
	if boundary != nil {
		area = *boundary
	}
	return ImageAppearance(ImageOptions{
		ImageID:  imageID,
		Boundary: area,
		Width:    img.Width,
		Height:   img.Height,
		Aspect:   aspect,
		Layout:   img.Layout,
	})
}

// patchResImage 在 DocumentRes.xml 的 <MultiMedias> 中追加一条图片媒体。
func (a *annotationIndex) patchResImage(resEntry string, imageID uint64, format, mediaFile string) error {
	doc, root, err := a.docRoot(resEntry)
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %w", resEntry, err)
	}
	prefix := root.Space
	multiMedias := findElementByTag(root, "MultiMedias")
	if multiMedias == nil {
		multiMedias = etree.NewElement(prefixTag(prefix, "MultiMedias"))
		insertResMultiMedias(root, multiMedias)
	}
	media := multiMedias.CreateElement(prefixTag(prefix, "MultiMedia"))
	media.CreateAttr("ID", strconv.FormatUint(imageID, 10))
	media.CreateAttr("Type", "Image")
	media.CreateAttr("Format", format)
	media.CreateElement(prefixTag(prefix, "MediaFile")).SetText(mediaFile)
	output, err := doc.WriteToBytes()
	if err != nil {
		return fmt.Errorf("序列化 %s 失败: %w", resEntry, err)
	}
	a.engine.changes[resEntry] = output
	return nil
}

// insertResMultiMedias 按 XSD 顺序插入 <MultiMedias>：在 CompositeGraphicUnits
// 之前、DrawParams/ColorSpaces/Fonts 之后。
func insertResMultiMedias(root, el *etree.Element) {
	if target := findElementByTag(root, "CompositeGraphicUnits"); target != nil {
		target.Parent().InsertChildAt(target.Index(), el)
		return
	}
	var anchor *etree.Element
	for _, tag := range []string{"DrawParams", "ColorSpaces", "Fonts"} {
		if target := findElementByTag(root, tag); target != nil {
			anchor = target
		}
	}
	if anchor != nil {
		anchor.Parent().InsertChildAt(anchor.Index()+1, el)
		return
	}
	root.AddChild(el)
}

// multiMediaIDs 收集资源文件中已注册的媒体资源 ID。
func multiMediaIDs(root *etree.Element) map[uint64]bool {
	used := make(map[uint64]bool)
	if container := findElementByTag(root, "MultiMedias"); container != nil {
		for _, el := range container.ChildElements() {
			if el.Tag == "MultiMedia" {
				if id, err := strconv.ParseUint(el.SelectAttrValue("ID", ""), 10, 64); err == nil {
					used[id] = true
				}
			}
		}
	}
	return used
}

// reclaimMedia 回收本次 Remove/Replace 释放、且不再被任何页面注解引用的水印图片：
// 从 DocumentRes.xml 的 <MultiMedias> 移除条目，并删除对应图片文件。
func (a *annotationIndex) reclaimMedia(document models.Document) error {
	if len(a.engine.releasedMedia) == 0 {
		return nil
	}
	referenced, err := a.referencedMediaIDs()
	if err != nil {
		return err
	}
	// 仅回收仍未被引用、且本次确实释放的图片资源。
	pending := make(map[uint64]bool)
	for id := range a.engine.releasedMedia {
		if !referenced[id] {
			pending[id] = true
		}
	}
	if len(pending) == 0 {
		return nil
	}
	if len(document.CommonData.DocumentRes) == 0 {
		return nil
	}
	resEntry := strings.TrimLeft(document.CommonData.DocumentRes[0].Resolve(models.StLoc(a.baseLoc)).String(), "/")
	if resEntry == "" {
		return nil
	}
	// 资源文件可能刚被本次 addImageResource 修改过，优先使用待写回的最新内容，
	// 否则会丢失新注册的媒体。
	doc, root, err := a.pendingDocRoot(resEntry)
	if err != nil {
		return nil
	}
	container := findElementByTag(root, "MultiMedias")
	if container == nil {
		return nil
	}
	mediaBase := strings.TrimLeft(path.Join(path.Dir(resEntry), ""), "/")
	if baseLoc := root.SelectAttrValue("BaseLoc", ""); baseLoc != "" {
		mediaBase = strings.TrimLeft(path.Join(path.Dir(resEntry), baseLoc), "/")
	}
	removed := false
	for _, media := range container.ChildElements() {
		if media.Tag != "MultiMedia" {
			continue
		}
		id, err := strconv.ParseUint(media.SelectAttrValue("ID", ""), 10, 64)
		if err != nil || !pending[id] {
			continue
		}
		if fileEl := media.FindElement("MediaFile"); fileEl != nil {
			mediaFile := strings.TrimSpace(fileEl.Text())
			if mediaFile != "" {
				entry := mediaFile
				if mediaBase != "" {
					entry = mediaBase + "/" + mediaFile
				}
				// 只删除水印自行写入的资源，避免误删用户已有的同名媒体。
				if isWatermarkMediaEntry(mediaFile) {
					a.engine.deletes[entry] = true
				}
			}
		}
		container.RemoveChild(media)
		removed = true
	}
	if !removed {
		return nil
	}
	// <MultiMedias> 为空时一并移除，避免留下空容器。
	if len(container.ChildElements()) == 0 {
		if parent := container.Parent(); parent != nil {
			parent.RemoveChild(container)
		}
	}
	output, err := doc.WriteToBytes()
	if err != nil {
		return fmt.Errorf("序列化 %s 失败: %w", resEntry, err)
	}
	a.engine.changes[resEntry] = output
	a.engine.releasedMedia = make(map[uint64]bool)
	return nil
}

// isWatermarkMediaEntry 判断媒体文件是否为水位自身生成的命名（Images/Image_<id>.<ext>）。
func isWatermarkMediaEntry(mediaFile string) bool {
	mediaFile = strings.TrimSpace(mediaFile)
	dir, name := path.Split(mediaFile)
	if path.Clean(dir) != "Images" && path.Clean(dir) != "Images/" {
		return false
	}
	return strings.HasPrefix(name, "Image_")
}

// referencedMediaIDs 汇总所有页面注解文件（含尚未写回的本次修改）引用的图片资源 ID。
func (a *annotationIndex) referencedMediaIDs() (map[uint64]bool, error) {
	referenced := make(map[uint64]bool)
	for _, entry := range a.pages {
		// 本次修改过的页面优先使用内存中的最新内容，其余读取原始条目。
		if data, ok := a.engine.changes[entry]; ok {
			if err := collectMediaFromXML(data, referenced); err != nil {
				return nil, err
			}
			continue
		}
		data, err := a.pkg.Read(entry)
		if err != nil {
			continue
		}
		if err := collectMediaFromXML(data, referenced); err != nil {
			return nil, err
		}
	}
	// 除注解文件外，页面内容/模板也可能引用媒体资源；保守起见一并扫描。
	if err := a.collectNonAnnotMedia(referenced); err != nil {
		return nil, err
	}
	return referenced, nil
}

// collectMediaFromXML 解析 XML 数据并收集所有 ImageObject 的 ResourceID。
func collectMediaFromXML(data []byte, into map[uint64]bool) error {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		return err
	}
	if root := doc.Root(); root != nil {
		collectImageObjectIDs(root, into)
	}
	return nil
}

// collectImageObjectIDs 递归收集元素树中 ImageObject 的 ResourceID。
func collectImageObjectIDs(el *etree.Element, into map[uint64]bool) {
	if el.Tag == "ImageObject" {
		if id, err := strconv.ParseUint(el.SelectAttrValue("ResourceID", ""), 10, 64); err == nil {
			into[id] = true
		}
	}
	for _, child := range el.ChildElements() {
		collectImageObjectIDs(child, into)
	}
}

// collectNonAnnotMedia 扫描文档体内非注解 XML 中的图片引用，避免误删页面自身资源。
// 注解文件（含历史布局 Pages/Page_*/Annotation.xml）已被 referencedMediaIDs 用最新内容
// 单独扫描，这里必须跳过，否则会读到尚未更新的原始注解而漏回收水印图片。
func (a *annotationIndex) collectNonAnnotMedia(into map[uint64]bool) error {
	annotEntries := make(map[string]bool, len(a.pages))
	for _, entry := range a.pages {
		annotEntries[entry] = true
	}
	prefix := a.baseLoc
	if prefix != "" {
		prefix += "/"
	}
	return a.pkg.WalkEntries(func(entry core.Entry) bool {
		name := entry.Name
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".xml") {
			return true
		}
		if annotEntries[name] || name == a.indexEntry {
			return true
		}
		// 兜底跳过其它命名风格的注解文件（如 */Annotation.xml）。
		if strings.Contains(name, "/Annotations") || strings.Contains(name, "/Annots") ||
			strings.HasSuffix(name, "/Annotation.xml") {
			return true
		}
		data, err := a.pkg.Read(name)
		if err != nil {
			return true
		}
		_ = collectMediaFromXML(data, into)
		return true
	})
}
