package analyzer

import (
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	gmx509 "github.com/emmansun/gmsm/smx509"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

type analyzer struct {
	options             Options
	report              Report
	files               map[string]core.Entry
	fileEdges           map[string]ReferenceEdge
	idEdges             map[string]ReferenceEdge
	resourceFiles       map[string]bool
	registeredResources map[string]struct{}
	resourceAssets      map[string]map[string]bool
	resources           []ResourceInfo
	drawParamValues     map[string]*models.DrawParam
	drawParamRelative   map[string]string
	templates           map[string]int
	usedTemplates       map[string]int
	patterns            map[string]int
	usedPatterns        map[string]int
	patternKeys         map[*models.CtPattern]string
	missingFiles        map[string]struct{}
	warningSet          map[string]struct{}
	images              map[string]int
	fonts               map[string]int
	drawParams          map[string]int
	composites          map[string]int
	colorSpaces         map[string]int
	usedImages          map[string]int
	usedFonts           map[string]int
	usedDrawParams      map[string]int
	usedComposites      map[string]int
	usedColorSpaces     map[string]int
	embeddedFonts       int
}

type signatureIndexFile struct {
	Signatures []signatureIndex `xml:"Signature,omitempty"`
}

type signatureIndex struct {
	ID      string       `xml:"ID,attr"`
	Type    string       `xml:"Type,attr"`
	BaseLoc models.StLoc `xml:"BaseLoc,attr"`
}

// Analyze 分析一个 OFD 文件。输入支持文件路径、[]byte 和 io.Reader。
func Analyze(input any, options ...Option) (Report, error) {
	configured := Options{
		IncludeTemplates:   true,
		IncludeAnnotations: true,
		IncludeSignatures:  true,
	}
	for _, option := range options {
		if option != nil {
			option(&configured)
		}
	}

	report := Report{
		SchemaVersion:   SchemaVersion,
		Tool:            ToolInfo{Name: "ofd-analyzer", Version: ToolVersion},
		Status:          StatusFailed,
		Documents:       make([]DocumentInfo, 0),
		Pages:           make([]PageInfo, 0),
		ResourceDetails: make([]ResourceInfo, 0),
		Attachments:     make([]AttachmentInfo, 0),
		Annotations:     make([]AnnotationInfo, 0),
		Signatures:      make([]SignatureInfo, 0),
		FileReferences:  make([]ReferenceEdge, 0),
		IDReferences:    make([]ReferenceEdge, 0),
	}
	inputPath, inputSize := inputInfo(input)
	report.Input = InputInfo{Path: inputPath, Size: inputSize}

	ofd, err := parser.NewOFD(input)
	if err != nil {
		report.Errors = []string{fmt.Sprintf("解析OFD失败: %v", err)}
		return report, err
	}
	defer ofd.Close()
	if len(ofd.Documents) == 0 || ofd.Documents[0] == nil || ofd.Documents[0].FileCache == nil {
		report.Errors = []string{"OFD 没有可分析的文档体"}
		return report, errors.New(report.Errors[0])
	}
	if err := configureSignatureVerification(ofd, configured); err != nil {
		report.Errors = []string{fmt.Sprintf("配置签名验证失败: %v", err)}
		return report, err
	}

	a := &analyzer{
		options:             configured,
		report:              report,
		files:               make(map[string]core.Entry),
		fileEdges:           make(map[string]ReferenceEdge),
		idEdges:             make(map[string]ReferenceEdge),
		resourceFiles:       make(map[string]bool),
		registeredResources: make(map[string]struct{}),
		resourceAssets:      make(map[string]map[string]bool),
		drawParamValues:     make(map[string]*models.DrawParam),
		drawParamRelative:   make(map[string]string),
		templates:           make(map[string]int),
		usedTemplates:       make(map[string]int),
		patterns:            make(map[string]int),
		usedPatterns:        make(map[string]int),
		patternKeys:         make(map[*models.CtPattern]string),
		missingFiles:        make(map[string]struct{}),
		warningSet:          make(map[string]struct{}),
		images:              make(map[string]int),
		fonts:               make(map[string]int),
		drawParams:          make(map[string]int),
		composites:          make(map[string]int),
		colorSpaces:         make(map[string]int),
		usedImages:          make(map[string]int),
		usedFonts:           make(map[string]int),
		usedDrawParams:      make(map[string]int),
		usedComposites:      make(map[string]int),
		usedColorSpaces:     make(map[string]int),
	}
	a.indexPackage(ofd.Documents[0].FileCache)
	a.analyzeDocuments(ofd)
	a.finish()
	return a.report, nil
}

func configureSignatureVerification(ofd *parser.OFD, options Options) error {
	if len(options.SignatureUID) == 0 && options.SignatureFormat == "" && len(options.SignatureTrustRootsPEM) == 0 && len(options.SignatureCRLsPEM) == 0 && len(options.SignatureRevocationIssuersPEM) == 0 {
		return nil
	}
	var trust *parser.CertificateTrustOptions
	if len(options.SignatureTrustRootsPEM) > 0 {
		roots := gmx509.NewCertPool()
		if !roots.AppendCertsFromPEM(options.SignatureTrustRootsPEM) {
			return errors.New("信任根 PEM 中没有可解析的证书")
		}
		trust = &parser.CertificateTrustOptions{Roots: roots}
	}
	var revocation *parser.CertificateRevocationOptions
	if len(options.SignatureCRLsPEM) > 0 || len(options.SignatureRevocationIssuersPEM) > 0 {
		crls, err := parsePEMOrDERBlocks(options.SignatureCRLsPEM, "X509 CRL")
		if err != nil {
			return fmt.Errorf("CRL: %w", err)
		}
		issuers, err := parsePEMOrDERBlocks(options.SignatureRevocationIssuersPEM, "CERTIFICATE")
		if err != nil {
			return fmt.Errorf("CRL 签发者证书: %w", err)
		}
		for _, crl := range crls {
			if _, err := gmx509.ParseCRL(crl); err != nil {
				return fmt.Errorf("解析 CRL 失败: %w", err)
			}
		}
		for _, issuer := range issuers {
			if _, err := gmx509.ParseCertificate(issuer); err != nil {
				return fmt.Errorf("解析 CRL 签发者证书失败: %w", err)
			}
		}
		revocation = &parser.CertificateRevocationOptions{CRLs: crls, Issuers: issuers}
	}
	verificationOptions := &parser.SignatureVerificationOptions{
		UID:             append([]byte(nil), options.SignatureUID...),
		SignatureFormat: parser.SM2SignatureFormat(strings.ToLower(strings.TrimSpace(options.SignatureFormat))),
		Trust:           trust,
		Revocation:      revocation,
	}
	for _, document := range ofd.Documents {
		if document == nil {
			continue
		}
		document.ForEachSignedValue(func(id string, signedValue *parser.SignedValue) bool {
			if signedValue == nil || signedValue.SES == nil {
				return true
			}
			result, err := parser.VerifySESSignedValueWithOptions(signedValue, verificationOptions)
			if err != nil {
				document.SetVerificationError(id, err)
				document.DeleteVerificationResult(id)
				return true
			}
			document.SetVerificationResult(id, result)
			document.DeleteVerificationError(id)
			return true
		})

	}
	return nil
}

func parsePEMOrDERBlocks(data []byte, blockType string) ([][]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var blocks [][]byte
	remaining := data
	for {
		block, rest := pem.Decode(remaining)
		if block == nil {
			break
		}
		if block.Type != blockType {
			return nil, fmt.Errorf("PEM 类型为 %q，期望 %q", block.Type, blockType)
		}
		blocks = append(blocks, block.Bytes)
		remaining = rest
	}
	if len(blocks) == 0 {
		return [][]byte{append([]byte(nil), data...)}, nil
	}
	if strings.TrimSpace(string(remaining)) != "" {
		return nil, errors.New("PEM 后包含无法解析的内容")
	}
	return blocks, nil
}

func inputInfo(input any) (string, int64) {
	switch value := input.(type) {
	case string:
		info, err := os.Stat(value)
		if err == nil {
			return value, info.Size()
		}
		return value, 0
	case []byte:
		return "", int64(len(value))
	default:
		return "", 0
	}
}

func (a *analyzer) indexPackage(packageReader *core.Package) {
	entries := packageReader.Entries()
	if a.options.IncludeTree {
		a.report.Package.Tree = buildPackageTree(entries)
	}
	a.report.Package.Entries = len(entries)
	for _, entry := range entries {
		name := cleanPackagePath(entry.Path)
		if name == "" {
			continue
		}
		a.files[name] = entry
		a.report.Package.CompressedBytes += entry.CompressedSize
		a.report.Package.UncompressedBytes += entry.UncompressedSize
		if entry.IsDir {
			a.report.Package.Directories++
			continue
		}
		a.report.Package.Files++
		if strings.EqualFold(path.Ext(name), ".xml") {
			a.report.Package.XMLFiles++
		}
	}
}

type packageTreeNode struct {
	tree     PackageTree
	parent   *packageTreeNode
	children map[string]*packageTreeNode
	entry    bool
}

func buildPackageTree(entries []core.Entry) *PackageTree {
	root := &packageTreeNode{
		tree:     PackageTree{Name: ".", Kind: "directory"},
		children: make(map[string]*packageTreeNode),
	}
	for _, entry := range entries {
		name := cleanPackagePath(entry.Path)
		if name == "" {
			continue
		}
		parts := strings.Split(name, "/")
		current := root
		for index, part := range parts {
			pathName := strings.Join(parts[:index+1], "/")
			child, exists := current.children[part]
			if !exists {
				child = &packageTreeNode{
					tree: PackageTree{
						Name: part,
						Path: pathName,
						Kind: "directory",
					},
					parent:   current,
					children: make(map[string]*packageTreeNode),
				}
				current.children[part] = child
				current = child
				continue
			}
			current = child
		}

		if current.entry {
			duplicate := &packageTreeNode{tree: current.tree, parent: current.parent, children: make(map[string]*packageTreeNode)}
			duplicate.tree.Duplicate = true
			current.parent.children[partTreeKey(parts[len(parts)-1], len(current.parent.children))] = duplicate
			setPackageTreeEntry(&duplicate.tree, entry)
			continue
		}
		setPackageTreeEntry(&current.tree, entry)
		current.entry = true
	}
	result := packageTreeValue(root)
	return &result
}

func setPackageTreeEntry(tree *PackageTree, entry core.Entry) {
	tree.Kind = "file"
	if entry.IsDir {
		tree.Kind = "directory"
	}
	tree.MediaType = packageMediaType(tree.Name, tree.Kind)
	tree.Size = entry.UncompressedSize
	tree.CompressedSize = entry.CompressedSize
}

func packageTreeValue(node *packageTreeNode) PackageTree {
	result := node.tree
	children := make([]*packageTreeNode, 0, len(node.children))
	for _, child := range node.children {
		children = append(children, child)
	}
	sort.SliceStable(children, func(i, j int) bool {
		left, right := children[i].tree, children[j].tree
		if left.Kind != right.Kind {
			return left.Kind == "directory"
		}
		return left.Name < right.Name
	})
	if len(children) > 0 {
		result.Children = make([]PackageTree, 0, len(children))
		for _, child := range children {
			result.Children = append(result.Children, packageTreeValue(child))
		}
	}
	return result
}

func partTreeKey(name string, index int) string {
	return fmt.Sprintf("%s#duplicate-%d", name, index)
}

func packageMediaType(name, kind string) string {
	if kind == "directory" {
		return ""
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".xml":
		return "xml"
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".tif", ".tiff", ".webp":
		return "image"
	case ".ttf", ".otf", ".ttc", ".cff":
		return "font"
	default:
		return "binary"
	}
}

func (a *analyzer) analyzeDocuments(ofd *parser.OFD) {
	a.report.OFD = OFDInfo{Version: ofd.Version, DocType: ofd.DocType}
	a.report.Summary.DocumentBodies = len(ofd.Documents)
	globalPage := 0
	for documentIndex, doc := range ofd.Documents {
		if doc == nil {
			continue
		}
		body := models.DocBody{}
		if documentIndex < len(ofd.DocBodies) {
			body = ofd.DocBodies[documentIndex]
		}
		a.registerDocumentResources(documentIndex, doc)
		if a.options.IncludeTemplates {
			a.registerTemplateDefinitions(documentIndex, doc)
		}
		a.report.Documents = append(a.report.Documents, makeDocumentInfo(documentIndex, body, doc))
		declaredPages := len(doc.Document.Pages.Pages)
		a.report.Summary.Pages += declaredPages
		a.report.Summary.ParsedPages += len(doc.Pages)
		pagePaths := make([]string, len(doc.Pages))
		for pageIndex, page := range doc.Pages {
			if page == nil {
				continue
			}
			if pageIndex < declaredPages {
				pagePaths[pageIndex] = resolveFrom(doc.BaseLoc, doc.Document.Pages.Pages[pageIndex].BaseLoc)
			}
			a.registerPageResources(documentIndex, doc, page, pagePaths[pageIndex])
		}
		for pageIndex, page := range doc.Pages {
			if page == nil {
				continue
			}
			globalPage++
			a.report.Pages = append(a.report.Pages, a.analyzePage(documentIndex, pageIndex+1, globalPage, pagePaths[pageIndex], doc, page))
		}
		if a.options.IncludeAnnotations {
			a.analyzeAnnotations(documentIndex, doc)
		}
		if a.options.IncludeSignatures {
			a.analyzeSignatures(documentIndex, body, doc)
		}
		a.analyzeAttachments(documentIndex, doc)
		a.addDocumentReferences(body, doc)
	}
}

func makeDocumentInfo(index int, body models.DocBody, doc *parser.Document) DocumentInfo {
	info := DocumentInfo{
		Index:          index,
		DocID:          body.DocInfo.DocID,
		Title:          body.DocInfo.Title,
		Author:         body.DocInfo.Author,
		Subject:        body.DocInfo.Subject,
		Abstract:       body.DocInfo.Abstract,
		DocUsage:       body.DocInfo.DocUsage,
		Creator:        body.DocInfo.Creator,
		CreatorVersion: body.DocInfo.CreatorVersion,
		DocRoot:        body.DocRoot.String(),
		DeclaredPages:  len(doc.Document.Pages.Pages),
		ParsedPages:    len(doc.Pages),
		TemplateCount:  len(doc.Document.CommonData.TemplatePages),
		ResourceFiles:  len(doc.PublicResourceList()) + len(doc.DocumentResourceList()),
		HasCover:       body.DocInfo.Cover != nil,
		HasAttachments: doc.Document.Attachments != nil,
		HasAnnotations: doc.Document.Annotations != nil,
		HasSignatures:  body.Signatures != nil,
	}
	if body.DocInfo.CreationDate != nil {
		value := body.DocInfo.CreationDate.Time
		info.CreationDate = &value
	}
	if body.DocInfo.ModDate != nil {
		value := body.DocInfo.ModDate.Time
		info.ModDate = &value
	}
	if body.DocInfo.Keywords != nil {
		info.Keywords = append([]string(nil), body.DocInfo.Keywords.Keyword...)
	}
	return info
}

func (a *analyzer) analyzePage(documentIndex, documentPage, pageNumber int, baseLoc string, doc *parser.Document, page *parser.Page) PageInfo {
	if page == nil {
		return PageInfo{DocumentIndex: documentIndex, DocumentPage: documentPage, PageNumber: pageNumber}
	}
	lease, err := page.AcquireLease()
	if err != nil {
		a.addWarning(fmt.Sprintf("页面资源读取失败: %v", err))
		return PageInfo{DocumentIndex: documentIndex, DocumentPage: documentPage, PageNumber: pageNumber, ID: uint64(page.ID), BaseLoc: baseLoc}
	}
	defer lease.Release()
	content := lease.Content()
	var area *models.CtPageArea
	if content != nil {
		area = content.Area
	}
	source := "page"
	if area == nil {
		area = &doc.CommonData.PageArea
		source = "document"
	}
	box := models.StBox{Width: 210, Height: 297}
	if area != nil {
		box = area.PhysicalBox
	}
	if box.Width <= 0 || box.Height <= 0 {
		source += ":fallback"
		a.addWarning(fmt.Sprintf("页面 %d 的 PhysicalBox 无效，使用 A4 回退尺寸", pageNumber))
		box = models.StBox{Width: 210, Height: 297}
	}
	orientation := "square"
	if box.Width > box.Height {
		orientation = "landscape"
	} else if box.Height > box.Width {
		orientation = "portrait"
	}
	counts := ObjectCounts{}
	text := TextSummary{}
	resources := PageResources{}
	layers := 0
	if content != nil && content.Content != nil {
		layers = len(content.Content.Layer)
		for _, layer := range content.Content.Layer {
			if layer == nil {
				continue
			}
			source := baseLoc
			if source == "" {
				source = fmt.Sprintf("page:%d", pageNumber)
			}
			a.analyzeItems(layer.Items, &counts, &text, &resources, documentIndex, source)
			if layer.DrawParam > 0 {
				resources.DrawParams = appendUnique(resources.DrawParams, uint64(layer.DrawParam))
			}
			a.useDrawParam(documentIndex, uint64(layer.DrawParam), source, &resources)
		}
	}
	if a.options.IncludeTemplates {
		var templates []models.Template
		if content != nil {
			templates = content.Template
		}
		for _, template := range templates {
			if template.TemplateID > 0 {
				resources.Templates = appendUnique(resources.Templates, uint64(template.TemplateID))
			}
			a.useTemplate(documentIndex, uint64(template.TemplateID), baseLoc)
		}
	}
	a.report.Objects.Pages++
	addObjectSummaryCounts(&a.report.Objects, counts)
	var pageContent *models.Content
	if content != nil {
		pageContent = content.Content
	}
	if depth := maxPageBlockDepth(pageContent); depth > a.report.Objects.MaxPageBlockDepth {
		a.report.Objects.MaxPageBlockDepth = depth
	}
	a.report.Text = addTextSummary(a.report.Text, text)
	a.report.Summary.PageObjects += counts.Total
	a.report.Summary.PageTextCharacters += text.UnicodeCodePoints
	a.report.Summary.TextCharacters += text.UnicodeCodePoints
	return PageInfo{
		DocumentIndex: documentIndex,
		DocumentPage:  documentPage,
		PageNumber:    pageNumber,
		ID:            uint64(page.ID),
		BaseLoc:       baseLoc,
		Layers:        layers,
		Size:          PageSize{X: box.X, Y: box.Y, Width: box.Width, Height: box.Height, Unit: "mm", Orientation: orientation, Source: source},
		Objects:       counts,
		Text:          text,
		Resources:     resources,
	}
}

func (a *analyzer) registerTemplateDefinitions(documentIndex int, doc *parser.Document) {
	ids := make([]models.StID, 0, len(doc.Document.CommonData.TemplatePages))
	for _, page := range doc.Document.CommonData.TemplatePages {
		ids = append(ids, page.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		key := resourceKey(documentIndex, "template", id)
		a.templates[key]++
		if template := doc.GetTemplate(id); template != nil {
			a.registerPageResourceContent(documentIndex, doc, template, templateSourcePath(doc, id), "template")
			for _, reference := range template.Template {
				a.useTemplate(documentIndex, uint64(reference.TemplateID), fmt.Sprintf("template:%d", id))
			}
			if template.Content != nil {
				for _, layer := range template.Content.Layer {
					if layer != nil {
						a.scanDefinitionPatterns(documentIndex, layer.Items)
					}
				}
			}
		}
	}
}

func templateSourcePath(doc *parser.Document, id models.StID) string {
	if doc == nil {
		return ""
	}
	for _, template := range doc.Document.CommonData.TemplatePages {
		if template.ID == id {
			return resolveFrom(doc.BaseLoc, template.BaseLoc)
		}
	}
	return ""
}

func (a *analyzer) analyzeItems(items []models.PageItem, counts *ObjectCounts, text *TextSummary, resources *PageResources, documentIndex int, source string) {
	for _, item := range items {
		counts.Total++
		switch item.Kind {
		case models.PageItemText:
			counts.Text++
			addTextObject(text, item.Text)
			if item.Text.Font > 0 {
				resources.Fonts = appendUnique(resources.Fonts, uint64(item.Text.Font))
				a.useFont(documentIndex, uint64(item.Text.Font), source)
			}
			a.useDrawParam(documentIndex, uint64(item.Text.DrawParam), source, resources)
			a.addColorReference(documentIndex, source, item.Text.FillColor, resources)
			a.addColorReference(documentIndex, source, item.Text.StrokeColor, resources)
		case models.PageItemPath:
			counts.Path++
			counts.PathCommands += len(item.Path.AbbreviatedData)
			a.useDrawParam(documentIndex, uint64(item.Path.DrawParam), source, resources)
			a.addColorReference(documentIndex, source, item.Path.FillColor, resources)
			a.addColorReference(documentIndex, source, item.Path.StrokeColor, resources)
		case models.PageItemImage:
			counts.Image++
			for _, id := range []models.StRefID{item.Image.ResourceID, item.Image.Substitution, item.Image.ImageMask} {
				if id == 0 {
					continue
				}
				resources.Images = appendUnique(resources.Images, uint64(id))
				a.useImage(documentIndex, uint64(id), source)
			}
			a.useDrawParam(documentIndex, uint64(item.Image.DrawParam), source, resources)
			if item.Image.Border != nil {
				a.addColorReference(documentIndex, source, item.Image.Border.BorderColor, resources)
			}
		case models.PageItemComposite:
			counts.Composite++
			id := uint64(item.Composite.ResourceID)
			if id > 0 {
				resources.Composites = appendUnique(resources.Composites, id)
				a.useComposite(documentIndex, id, source)
			}
			a.useDrawParam(documentIndex, uint64(item.Composite.DrawParam), source, resources)
		case models.PageItemBlock:
			counts.PageBlock++
			a.analyzeItems(item.Block.Items, counts, text, resources, documentIndex, source)
		}
	}
}

func addTextObject(summary *TextSummary, object models.TextObject) {
	summary.Objects++
	for _, code := range object.TextCode {
		summary.TextCodes++
		summary.UTF8Bytes += len(code.Value)
		for _, character := range code.Value {
			summary.UnicodeCodePoints++
			if unicode.IsSpace(character) {
				summary.WhitespaceCodePoints++
			} else {
				summary.NonWhitespaceCodePoints++
			}
		}
	}
	for _, transform := range object.CGTransform {
		if transform.GlyphCount > 0 {
			summary.Glyphs += transform.GlyphCount
		} else {
			summary.Glyphs += len(transform.Glyphs)
		}
	}
}

func addTextSummary(left, right TextSummary) TextSummary {
	left.Objects += right.Objects
	left.TextCodes += right.TextCodes
	left.UTF8Bytes += right.UTF8Bytes
	left.UnicodeCodePoints += right.UnicodeCodePoints
	left.WhitespaceCodePoints += right.WhitespaceCodePoints
	left.NonWhitespaceCodePoints += right.NonWhitespaceCodePoints
	left.Glyphs += right.Glyphs
	return left
}

func addObjectCounts(left, right ObjectCounts) ObjectCounts {
	left.Total += right.Total
	left.Text += right.Text
	left.Path += right.Path
	left.Image += right.Image
	left.Composite += right.Composite
	left.PageBlock += right.PageBlock
	left.PathCommands += right.PathCommands
	return left
}

func addObjectSummaryCounts(summary *ObjectSummary, counts ObjectCounts) {
	if summary == nil {
		return
	}
	summary.Total += counts.Total
	summary.Text += counts.Text
	summary.Path += counts.Path
	summary.Image += counts.Image
	summary.Composite += counts.Composite
	summary.PageBlock += counts.PageBlock
	summary.PathCommands += counts.PathCommands
}

func maxPageBlockDepth(content *models.Content) int {
	if content == nil {
		return 0
	}
	maximum := 0
	for _, layer := range content.Layer {
		if layer == nil {
			continue
		}
		for _, item := range layer.Items {
			if depth := pageBlockDepth(item, 0); depth > maximum {
				maximum = depth
			}
		}
	}
	return maximum
}

func pageBlockDepth(item models.PageItem, depth int) int {
	if item.Kind != models.PageItemBlock {
		return depth
	}
	maximum := depth + 1
	for _, child := range item.Block.Items {
		if childDepth := pageBlockDepth(child, depth+1); childDepth > maximum {
			maximum = childDepth
		}
	}
	return maximum
}

func appendUnique(values []uint64, value uint64) []uint64 {
	if value == 0 {
		return values
	}
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func (a *analyzer) registerDocumentResources(documentIndex int, doc *parser.Document) {
	for index, location := range doc.Document.CommonData.PublicRes {
		sourcePath := resolveFrom(doc.BaseLoc, location)
		exists := a.fileExists(sourcePath)
		a.trackResourceFile(sourcePath, exists)
		if !exists {
			a.addMissingFileWarning("资源文件不存在", sourcePath)
		}
		publicResources := doc.PublicResourceList()
		if index < len(publicResources) && publicResources[index] != nil {
			a.registerResourceFile(documentIndex, doc, publicResources[index], "public", sourcePath)
		}
	}
	for index, location := range doc.Document.CommonData.DocumentRes {
		sourcePath := resolveFrom(doc.BaseLoc, location)
		exists := a.fileExists(sourcePath)
		a.trackResourceFile(sourcePath, exists)
		if !exists {
			a.addMissingFileWarning("资源文件不存在", sourcePath)
		}
		documentResources := doc.DocumentResourceList()
		if index < len(documentResources) && documentResources[index] != nil {
			a.registerResourceFile(documentIndex, doc, documentResources[index], "document", sourcePath)
		}
	}
}

func (a *analyzer) registerPageResources(documentIndex int, doc *parser.Document, page *parser.Page, pagePath string) {
	if page == nil {
		return
	}
	err := page.WithPageContent(func(content *models.PageContent) error {
		a.registerPageResourceContent(documentIndex, doc, content, pagePath, "page")
		return nil
	})
	if err != nil {
		a.addWarning(fmt.Sprintf("页面资源读取失败: %v", err))
	}
}

func (a *analyzer) registerPageResourceContent(documentIndex int, doc *parser.Document, content *models.PageContent, contentPath, scope string) {
	if content == nil || doc == nil || doc.FileCache == nil {
		return
	}
	contentPath = cleanPackagePath(contentPath)
	if contentPath == "" {
		return
	}
	baseDir := models.StLoc(path.Dir(contentPath))
	for _, location := range content.PageRes {
		resourcePath := resolveFrom(baseDir, location)
		exists := a.fileExists(resourcePath)
		a.trackResourceFile(resourcePath, exists)
		a.addFileReference(contentPath, "page-resource", resourcePath, exists)
		if !exists {
			a.addMissingFileWarning("页面资源文件不存在", resourcePath)
			continue
		}
		var resource models.Res
		if err := doc.FileCache.ReadXML(resourcePath, &resource); err != nil {
			a.addWarning(fmt.Sprintf("读取页面资源失败(%s): %v", resourcePath, err))
			continue
		}
		a.registerResourceFile(documentIndex, doc, &resource, scope, resourcePath)
	}
}

func (a *analyzer) registerResourceFile(documentIndex int, doc *parser.Document, resource *models.Res, scope, sourcePath string) {
	if resource == nil {
		return
	}
	sourcePath = cleanPackagePath(sourcePath)
	if sourcePath != "" {
		a.trackResourceFile(sourcePath, a.fileExists(sourcePath))
		key := fmt.Sprintf("%d:%s", documentIndex, sourcePath)
		if _, registered := a.registeredResources[key]; registered {
			return
		}
		a.registeredResources[key] = struct{}{}
	}
	for _, media := range medias(resource) {
		if media == nil {
			continue
		}
		key := resourceKey(documentIndex, "image", media.ID)
		mediaPath := a.resolveResourceAsset(doc, resource, sourcePath, media.MediaFile)
		a.images[key]++
		exists := a.fileExists(mediaPath)
		a.resources = append(a.resources, ResourceInfo{DocumentIndex: documentIndex, ID: uint64(media.ID), Kind: "image", Scope: scope, SourceFile: sourcePath, Path: mediaPath, Exists: exists})
		a.trackResourceAsset("image", mediaPath, exists)
		a.addFileReference(sourcePath, "media-file", mediaPath, exists)
	}
	if resource.Fonts != nil {
		for _, font := range resource.Fonts.Font {
			key := resourceKey(documentIndex, "font", font.ID)
			a.fonts[key]++
			fontPath := a.resolveResourceAsset(doc, resource, sourcePath, font.FontFile)
			exists := fontPath == "" || a.fileExists(fontPath)
			embedded := fontPath != "" && exists
			if embedded {
				a.embeddedFonts++
			}
			a.resources = append(a.resources, ResourceInfo{DocumentIndex: documentIndex, ID: uint64(font.ID), Kind: "font", Scope: scope, SourceFile: sourcePath, Path: fontPath, Exists: exists, Embedded: embedded, FontName: font.FontName, FamilyName: font.FamilyName, Charset: font.Charset, Italic: font.Italic, Bold: font.Bold, Serif: font.Serif, FixedWidth: font.FixedWidth})
			a.trackResourceAsset("font", fontPath, exists)
			if fontPath != "" {
				a.addFileReference(sourcePath, "font-file", fontPath, exists)
			}
		}
	}
	if resource.DrawParams != nil {
		for _, param := range resource.DrawParams.DrawParam {
			if param == nil {
				continue
			}
			key := resourceKey(documentIndex, "draw-param", param.ID)
			a.drawParamValues[key] = param
			a.drawParams[key]++
			a.registerPattern(documentIndex, param.FillColor)
			a.registerPattern(documentIndex, param.StrokeColor)
			a.resources = append(a.resources, ResourceInfo{DocumentIndex: documentIndex, ID: uint64(param.ID), Kind: "draw-param", Scope: scope, SourceFile: sourcePath, Exists: a.fileExists(sourcePath)})
			if param.Relative > 0 {
				parentKey := resourceKey(documentIndex, "draw-param", models.StID(param.Relative))
				a.usedDrawParams[parentKey]++
				a.drawParamRelative[key] = parentKey
				a.addIDReference(displayResourceKey(documentIndex, "draw-param", param.ID), displayResourceKey(documentIndex, "draw-param", models.StID(param.Relative)), "relative", a.hasResource(documentIndex, "draw-param", models.StID(param.Relative)))
			}
		}
	}
	if resource.ColorSpaces != nil {
		for _, colorSpace := range resource.ColorSpaces.ColorSpace {
			key := resourceKey(documentIndex, "color-space", colorSpace.ID)
			a.colorSpaces[key]++
			profile := a.resolveResourceAsset(doc, resource, sourcePath, colorSpace.Profile)
			exists := profile == "" || a.fileExists(profile)
			a.resources = append(a.resources, ResourceInfo{DocumentIndex: documentIndex, ID: uint64(colorSpace.ID), Kind: "color-space", Scope: scope, SourceFile: sourcePath, Path: profile, Exists: exists})
			a.trackResourceAsset("color-space", profile, exists)
			if profile != "" {
				a.addFileReference(sourcePath, "color-profile", profile, exists)
			}
		}
	}
	if resource.CompositeGraphicUnits != nil {
		for _, unit := range resource.CompositeGraphicUnits.CompositeGraphicUnit {
			key := resourceKey(documentIndex, "composite", unit.ID)
			a.composites[key]++
			a.scanDefinitionPatterns(documentIndex, unit.Content.Items)
			a.resources = append(a.resources, ResourceInfo{DocumentIndex: documentIndex, ID: uint64(unit.ID), Kind: "composite", Scope: scope, SourceFile: sourcePath, Exists: a.fileExists(sourcePath)})
		}
	}
}

func medias(resource *models.Res) []*models.MultiMedia {
	if resource.MultiMedias == nil {
		return nil
	}
	return resource.MultiMedias.MultiMedia
}

func (a *analyzer) useImage(documentIndex int, id uint64, source string) {
	if id == 0 {
		return
	}
	key := resourceKey(documentIndex, "image", models.StID(id))
	a.usedImages[key]++
	a.addIDReference(source, displayResourceKey(documentIndex, "image", models.StID(id)), "image", a.hasResource(documentIndex, "image", models.StID(id)))
}

func (a *analyzer) useFont(documentIndex int, id uint64, source string) {
	if id == 0 {
		return
	}
	key := resourceKey(documentIndex, "font", models.StID(id))
	a.usedFonts[key]++
	a.addIDReference(source, displayResourceKey(documentIndex, "font", models.StID(id)), "font", a.hasResource(documentIndex, "font", models.StID(id)))
}

func (a *analyzer) useTemplate(documentIndex int, id uint64, source string) {
	if id == 0 {
		return
	}
	key := resourceKey(documentIndex, "template", models.StID(id))
	a.usedTemplates[key]++
	a.addIDReference(source, displayResourceKey(documentIndex, "template", models.StID(id)), "template", a.hasResource(documentIndex, "template", models.StID(id)))
}

func (a *analyzer) useDrawParam(documentIndex int, id uint64, source string, resources *PageResources) {
	if id == 0 {
		return
	}
	refID := models.StID(id)
	if resources != nil {
		resources.DrawParams = appendUnique(resources.DrawParams, id)
	}
	key := resourceKey(documentIndex, "draw-param", refID)
	a.usedDrawParams[key]++
	a.addIDReference(source, displayResourceKey(documentIndex, "draw-param", refID), "draw-param", a.hasResource(documentIndex, "draw-param", refID))
	if param := a.drawParamValues[key]; param != nil && resources != nil {
		a.addColorReference(documentIndex, source, param.FillColor, resources)
		a.addColorReference(documentIndex, source, param.StrokeColor, resources)
	}
}

func (a *analyzer) useComposite(documentIndex int, id uint64, source string) {
	if id == 0 {
		return
	}
	refID := models.StID(id)
	key := resourceKey(documentIndex, "composite", refID)
	a.usedComposites[key]++
	a.addIDReference(source, displayResourceKey(documentIndex, "composite", refID), "composite", a.hasResource(documentIndex, "composite", refID))
}

func (a *analyzer) addColorReference(documentIndex int, source string, color *models.CTColor, resources *PageResources) {
	if color != nil && color.Pattern != nil {
		a.usePattern(documentIndex, color)
	}
	if color == nil || color.ColorSpace == 0 {
		return
	}
	refID := models.StID(color.ColorSpace)
	if resources != nil {
		resources.ColorSpaces = appendUnique(resources.ColorSpaces, uint64(refID))
	}
	key := resourceKey(documentIndex, "color-space", refID)
	a.usedColorSpaces[key]++
	a.addIDReference(source, displayResourceKey(documentIndex, "color-space", refID), "color-space", a.hasResource(documentIndex, "color-space", refID))
}

func (a *analyzer) registerPattern(documentIndex int, color *models.CTColor) {
	if color == nil || color.Pattern == nil {
		return
	}
	pattern := color.Pattern
	if _, exists := a.patternKeys[pattern]; exists {
		return
	}
	key := fmt.Sprintf("%d:pattern:%d", documentIndex, len(a.patternKeys)+1)
	a.patternKeys[pattern] = key
	a.patterns[key] = 1
}

func (a *analyzer) usePattern(documentIndex int, color *models.CTColor) {
	if color == nil || color.Pattern == nil {
		return
	}
	a.registerPattern(documentIndex, color)
	if key := a.patternKeys[color.Pattern]; key != "" {
		a.usedPatterns[key]++
	}
}

func (a *analyzer) scanDefinitionPatterns(documentIndex int, items []models.PageItem) {
	for _, item := range items {
		switch item.Kind {
		case models.PageItemText:
			a.usePattern(documentIndex, item.Text.FillColor)
			a.usePattern(documentIndex, item.Text.StrokeColor)
		case models.PageItemPath:
			a.usePattern(documentIndex, item.Path.FillColor)
			a.usePattern(documentIndex, item.Path.StrokeColor)
		case models.PageItemImage:
			if item.Image.Border != nil {
				a.usePattern(documentIndex, item.Image.Border.BorderColor)
			}
		case models.PageItemBlock:
			a.scanDefinitionPatterns(documentIndex, item.Block.Items)
		}
	}
}

func (a *analyzer) hasResource(documentIndex int, kind string, id models.StID) bool {
	key := resourceKey(documentIndex, kind, id)
	switch kind {
	case "image":
		_, ok := a.images[key]
		return ok
	case "font":
		_, ok := a.fonts[key]
		return ok
	case "template":
		_, ok := a.templates[key]
		return ok
	case "draw-param":
		_, ok := a.drawParams[key]
		return ok
	case "composite":
		_, ok := a.composites[key]
		return ok
	case "color-space":
		_, ok := a.colorSpaces[key]
		return ok
	default:
		return false
	}
}

func resourceKey(documentIndex int, kind string, id models.StID) string {
	return fmt.Sprintf("%d:%s:%d", documentIndex, kind, id)
}

func displayResourceKey(documentIndex int, kind string, id models.StID) string {
	return fmt.Sprintf("doc[%d]/%s:%d", documentIndex, kind, id)
}

func (a *analyzer) analyzeAttachments(documentIndex int, doc *parser.Document) {
	if doc.Document.Attachments == nil || doc.FileCache == nil {
		return
	}
	attachmentsPath := resolveFrom(doc.BaseLoc, *doc.Document.Attachments)
	attachments := doc.GetAttachments()
	if attachments == nil {
		a.addWarning(fmt.Sprintf("读取附件清单失败(%s)", attachmentsPath))
		return
	}
	for _, attachment := range attachments.Attachments {
		assetPath := a.resolveAttachmentAsset(doc, attachmentsPath, attachment.FileLoc)
		entry, exists := a.files[assetPath]
		exists = exists && !entry.IsDir
		usage := attachment.Usage
		if usage == "" {
			usage = "none"
		}
		info := AttachmentInfo{DocumentIndex: documentIndex, ID: attachment.ID, Name: attachment.Name, Format: attachment.Format, Path: assetPath, DeclaredSize: attachment.Size, Exists: exists, Visible: attachment.Visible.Value(true), Usage: usage}
		if exists {
			info.ActualSize = entry.UncompressedSize
		}
		a.report.Attachments = append(a.report.Attachments, info)
		a.addFileReference(attachmentsPath, "attachment", assetPath, exists)
		if !exists {
			a.addMissingFileWarning("附件文件不存在", assetPath)
		}
	}
}

func (a *analyzer) resolveAttachmentAsset(doc *parser.Document, attachmentsPath string, value models.StLoc) string {
	if value == "" {
		return ""
	}
	if value.IsAbsolute() {
		return cleanPackagePath(value.String())
	}
	candidates := []string{
		path.Join(path.Dir(cleanPackagePath(attachmentsPath)), value.String()),
	}
	if doc != nil {
		candidates = append(candidates, path.Join(doc.BaseLoc.String(), value.String()))
	}
	for _, candidate := range candidates {
		candidate = cleanPackagePath(candidate)
		if candidate != "" && a.fileExists(candidate) {
			return candidate
		}
	}
	return cleanPackagePath(candidates[0])
}

func (a *analyzer) analyzeAnnotations(documentIndex int, doc *parser.Document) {
	for _, page := range doc.Document.Pages.Pages {
		pageID := page.ID
		pageAnnot := doc.GetAnnotation(pageID)
		if pageAnnot == nil {
			continue
		}
		for _, annot := range pageAnnot.Annots {
			if annot == nil {
				continue
			}
			counts := ObjectCounts{}
			text := TextSummary{}
			resources := PageResources{}
			if annot.Appearance != nil {
				a.analyzeItems(annot.Appearance.Items, &counts, &text, &resources, documentIndex, "annotation")
				a.report.Objects.AnnotationAppearances += counts.Total
				addObjectSummaryCounts(&a.report.Objects, counts)
				a.report.Objects.AnnotationObjectCounts = addObjectCounts(a.report.Objects.AnnotationObjectCounts, counts)
				a.report.AnnotationText = addTextSummary(a.report.AnnotationText, text)
				a.report.Summary.AnnotationObjects += counts.Total
				a.report.Summary.AnnotationTextChars += text.UnicodeCodePoints
				a.report.Summary.TextCharacters += text.UnicodeCodePoints
			}
			a.report.Annotations = append(a.report.Annotations, AnnotationInfo{DocumentIndex: documentIndex, PageID: uint64(pageID), ID: annot.ID, Type: string(annot.Type), Subtype: annot.Subtype, Creator: annot.Creator, Visible: annot.Visible.Value(true), Print: annot.Print.Value(true), HasAppearance: annot.Appearance != nil, Objects: counts})
		}
	}
}

func (a *analyzer) analyzeSignatures(documentIndex int, body models.DocBody, doc *parser.Document) {
	if body.Signatures == nil || doc.FileCache == nil {
		return
	}
	indexPath := resolveFrom(models.StLoc("/"), *body.Signatures)
	var index signatureIndexFile
	if err := doc.FileCache.ReadXML(indexPath, &index); err != nil {
		a.addWarning(fmt.Sprintf("读取签名清单失败(%s): %v", indexPath, err))
		return
	}
	baseDir := models.StLoc(path.Dir(indexPath))
	for _, item := range index.Signatures {
		signaturePath := item.BaseLoc.Resolve(baseDir).String()
		var signature models.Signature
		if err := doc.FileCache.ReadXML(signaturePath, &signature); err != nil {
			a.addWarning(fmt.Sprintf("读取签名文件失败(%s): %v", signaturePath, err))
			continue
		}
		pages := make([]uint64, 0, len(signature.SignedInfo.StampAnnot))
		for _, stamp := range signature.SignedInfo.StampAnnot {
			if stamp != nil {
				pages = appendUnique(pages, uint64(stamp.PageRef))
			}
		}
		signedValue := resolveFrom(models.StLoc(path.Dir(signaturePath)), signature.SignedValue)
		info := SignatureInfo{DocumentIndex: documentIndex, ID: item.ID, Type: item.Type, Path: signaturePath, Provider: signature.SignedInfo.Provider.ProviderName, Company: signature.SignedInfo.Provider.Company, Version: signature.SignedInfo.Provider.Version, Method: signature.SignedInfo.SignatureMethod, Date: signature.SignedInfo.SignatureDateTime, CheckMethod: signature.SignedInfo.References.CheckMethod, ReferenceCount: len(signature.SignedInfo.References.Reference), StampCount: len(signature.SignedInfo.StampAnnot), Pages: pages, SignedValue: signedValue, SignedValueExists: a.fileExists(signedValue)}
		if signedValueResult := doc.GetSignedValue(item.ID); signedValueResult != nil {
			info.SignedValueFormat = signedValueResult.Format
			info.SignedValueParsed = signedValueResult.ASN1 != nil
			if signedValueResult.SES != nil {
				info.SealInfo = signatureSealInfo(signedValueResult.SES.TBS.Seal.SealInfo)
				info.SealSignatureAlgorithm = signedValueResult.SES.TBS.Seal.SignatureAlgorithm.String()
				info.OuterSignatureAlgorithm = signedValueResult.SES.SignatureAlgorithm.String()
			}
		}
		if signedValueErr := doc.GetSignedValueError(item.ID); signedValueErr != nil {
			info.SignedValueParseError = signedValueErr.Error()
			a.addWarning(fmt.Sprintf("签名[%s] SignedValue 解析失败(%s): %v", item.ID, signedValue, signedValueErr))
		}
		if digest := doc.GetDigestResult(item.ID); digest != nil {
			info.DigestChecked = true
			info.DigestValid = digest.Valid
			info.DigestMethod = digest.Method
			info.DigestReferences = make([]SignatureDigestInfo, 0, len(digest.References))
			for _, reference := range digest.References {
				info.DigestReferences = append(info.DigestReferences, SignatureDigestInfo{FileRef: reference.FileRef, ResolvedPath: reference.ResolvedPath, Exists: reference.Exists, Match: reference.Match, Expected: parser.DigestBase64(reference.Expected), Actual: parser.DigestBase64(reference.Actual), Error: reference.Error})
			}
			if digest.DataHash != nil {
				info.DataHash = &SignatureDataHashInfo{Match: digest.DataHash.Match, Expected: parser.DigestBase64(digest.DataHash.Expected), Actual: parser.DigestBase64(digest.DataHash.Actual), Error: digest.DataHash.Error}
			}
		}
		if verification := doc.GetVerificationResult(item.ID); verification != nil {
			info.VerificationChecked = true
			info.VerificationValid = verification.Valid
			info.TrustChecked = verification.TrustChecked
			info.Trusted = verification.Trusted
			info.RevocationChecked = verification.RevocationChecked
			info.RevocationValid = verification.RevocationValid
			if !verification.VerificationTime.IsZero() {
				info.VerificationTime = verification.VerificationTime.Format(time.RFC3339)
			}
			info.SealVerification = signatureComponentInfo(verification.Seal)
			info.OuterVerification = signatureComponentInfo(verification.Outer)
		}
		if verificationErr := doc.GetVerificationError(item.ID); verificationErr != nil {
			info.VerificationChecked = true
			info.VerificationError = verificationErr.Error()
		}
		if signature.SignedInfo.Seal != nil {
			info.Seal = resolveFrom(models.StLoc(path.Dir(signaturePath)), signature.SignedInfo.Seal.BaseLoc)
			info.SealExists = a.fileExists(info.Seal)
		}
		a.report.Signatures = append(a.report.Signatures, info)
		a.addFileReference(indexPath, "signature", signaturePath, a.fileExists(signaturePath))
		a.addFileReference(signaturePath, "signed-value", signedValue, a.fileExists(signedValue))
		if info.Seal != "" {
			a.addFileReference(signaturePath, "seal", info.Seal, info.SealExists)
		}
		for _, reference := range signature.SignedInfo.References.Reference {
			resolved := resolveFrom(models.StLoc(path.Dir(signaturePath)), reference.FileRef)
			a.addFileReference(signaturePath, "signature-reference", resolved, a.fileExists(resolved))
		}
	}
}

func signatureComponentInfo(value parser.SignatureComponentResult) *SignatureComponentInfo {
	result := &SignatureComponentInfo{
		Valid:             value.Valid,
		Algorithm:         value.Algorithm,
		SignatureFormat:   value.SignatureFormat,
		CertificateValid:  value.CertificateValid,
		TrustChecked:      value.TrustChecked,
		Trusted:           value.Trusted,
		TrustError:        value.TrustError,
		RevocationChecked: value.RevocationChecked,
		RevocationStatus:  value.RevocationStatus,
		RevocationError:   value.RevocationError,
		Error:             value.Error,
	}
	if value.Certificate != nil {
		result.SerialNumber = value.Certificate.SerialNumber
		result.Subject = value.Certificate.Subject.String()
		result.Issuer = value.Certificate.Issuer.String()
		result.NotBefore = value.Certificate.NotBefore.Format(time.RFC3339)
		result.NotAfter = value.Certificate.NotAfter.Format(time.RFC3339)
		result.PublicKey = value.Certificate.PublicKey
	}
	return result
}

func signatureSealInfo(value parser.SESealInfo) *SignatureSealInfo {
	return &SignatureSealInfo{
		ID:            value.ESID,
		Name:          value.Property.Name,
		CreateTime:    value.Property.CreateTime.Format(time.RFC3339),
		ValidFrom:     value.Property.ValidFrom.Format(time.RFC3339),
		ValidTo:       value.Property.ValidTo.Format(time.RFC3339),
		PictureType:   value.Picture.Type,
		PictureWidth:  value.Picture.Width,
		PictureHeight: value.Picture.Height,
	}
}

func (a *analyzer) addDocumentReferences(body models.DocBody, doc *parser.Document) {
	documentPath := resolveFrom(models.StLoc("/"), body.DocRoot)
	a.addFileReference("OFD.xml", "document", documentPath, a.fileExists(documentPath))
	for _, page := range doc.Document.Pages.Pages {
		pagePath := resolveFrom(doc.BaseLoc, page.BaseLoc)
		a.addFileReference(documentPath, "page", pagePath, a.fileExists(pagePath))
	}
	for _, template := range doc.Document.CommonData.TemplatePages {
		templatePath := resolveFrom(doc.BaseLoc, template.BaseLoc)
		a.addFileReference(documentPath, "template", templatePath, a.fileExists(templatePath))
	}
	for _, resource := range doc.Document.CommonData.PublicRes {
		resourcePath := resolveFrom(doc.BaseLoc, resource)
		a.addFileReference(documentPath, "public-resource", resourcePath, a.fileExists(resourcePath))
	}
	for _, resource := range doc.Document.CommonData.DocumentRes {
		resourcePath := resolveFrom(doc.BaseLoc, resource)
		a.addFileReference(documentPath, "document-resource", resourcePath, a.fileExists(resourcePath))
	}
	if doc.Document.Annotations != nil {
		annotationPath := resolveFrom(doc.BaseLoc, *doc.Document.Annotations)
		a.addFileReference(documentPath, "annotations", annotationPath, a.fileExists(annotationPath))
		if a.options.IncludeAnnotations {
			a.addAnnotationReferences(annotationPath, doc)
		}
	}
	if doc.Document.Attachments != nil {
		attachmentPath := resolveFrom(doc.BaseLoc, *doc.Document.Attachments)
		a.addFileReference(documentPath, "attachments", attachmentPath, a.fileExists(attachmentPath))
	}
	if a.options.IncludeSignatures && body.Signatures != nil {
		signaturePath := resolveFrom(models.StLoc("/"), *body.Signatures)
		a.addFileReference("OFD.xml", "signatures", signaturePath, a.fileExists(signaturePath))
	}
}

func (a *analyzer) addAnnotationReferences(annotationPath string, doc *parser.Document) {
	if doc == nil || doc.FileCache == nil || annotationPath == "" {
		return
	}
	var annotations models.Annotations
	if err := doc.FileCache.ReadXML(annotationPath, &annotations); err != nil {
		a.addWarning(fmt.Sprintf("读取注解清单失败(%s): %v", annotationPath, err))
		return
	}
	baseDir := models.StLoc(path.Dir(annotationPath))
	for _, page := range annotations.Pages {
		pagePath := resolveFrom(baseDir, page.FileLoc)
		exists := a.fileExists(pagePath)
		a.addFileReference(annotationPath, "annotation-page", pagePath, exists)
		if !exists {
			a.addMissingFileWarning("注解页面文件不存在", pagePath)
		}
	}
}

func (a *analyzer) addFileReference(from, kind, to string, exists bool) {
	from = cleanPackagePath(from)
	to = cleanPackagePath(to)
	if from == "" || to == "" {
		return
	}
	key := from + "\x00" + to + "\x00" + kind
	edge := a.fileEdges[key]
	edge.From, edge.To, edge.Type, edge.Exists = from, to, kind, exists
	edge.Count++
	a.fileEdges[key] = edge
}

func (a *analyzer) addIDReference(from, to, kind string, exists bool) {
	key := from + "\x00" + to + "\x00" + kind
	edge := a.idEdges[key]
	edge.From, edge.To, edge.Type, edge.Exists = from, to, kind, exists
	edge.Count++
	a.idEdges[key] = edge
}

func (a *analyzer) fileExists(filename string) bool {
	entry, ok := a.files[cleanPackagePath(filename)]
	return ok && !entry.IsDir
}

func (a *analyzer) addWarning(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	if _, exists := a.warningSet[message]; exists {
		return
	}
	a.warningSet[message] = struct{}{}
	a.report.Warnings = append(a.report.Warnings, message)
}

func (a *analyzer) addMissingFileWarning(description, filename string) {
	filename = cleanPackagePath(filename)
	if filename == "" {
		return
	}
	a.missingFiles[filename] = struct{}{}
	a.addWarning(fmt.Sprintf("%s(%s)", description, filename))
}

func (a *analyzer) resolveResourceAsset(doc *parser.Document, resource *models.Res, sourcePath string, value models.StLoc) string {
	if value == "" {
		return ""
	}
	if value.IsAbsolute() {
		return cleanPackagePath(value.String())
	}

	// Document.parse 会将文档资源中的媒体和字体路径规范化为包内路径。
	// 优先使用规范化后的路径，同时保留页面资源和直接构造 models.Res
	// 时所需的 XML 相对路径候选项。
	raw := value.String()
	cleanRaw := cleanPackagePath(raw)
	base := ""
	if doc != nil {
		base = cleanPackagePath(doc.BaseLoc.String())
	}
	normalized := base != "" && (cleanRaw == base || strings.HasPrefix(cleanRaw, base+"/"))
	candidates := make([]string, 0, 5)
	if normalized {
		candidates = append(candidates, cleanRaw)
	}
	if sourcePath != "" {
		resourceDir := path.Dir(cleanPackagePath(sourcePath))
		if resource != nil && resource.BaseLoc != "" {
			candidates = append(candidates, path.Join(resourceDir, resource.BaseLoc.String(), raw))
		}
		candidates = append(candidates, path.Join(resourceDir, raw))
	}
	if base != "" {
		if resource != nil && resource.BaseLoc != "" {
			candidates = append(candidates, path.Join(base, resource.BaseLoc.String(), raw))
		}
		candidates = append(candidates, path.Join(base, raw))
	}
	if !normalized {
		candidates = append(candidates, cleanRaw)
	}
	for _, candidate := range candidates {
		candidate = cleanPackagePath(candidate)
		if candidate != "" && a.fileExists(candidate) {
			return candidate
		}
	}
	for _, candidate := range candidates {
		if candidate = cleanPackagePath(candidate); candidate != "" {
			return candidate
		}
	}
	return ""
}

func (a *analyzer) trackResourceFile(filename string, exists bool) {
	filename = cleanPackagePath(filename)
	if filename == "" {
		return
	}
	if previous, ok := a.resourceFiles[filename]; ok {
		a.resourceFiles[filename] = previous || exists
		return
	}
	a.resourceFiles[filename] = exists
}

func (a *analyzer) trackResourceAsset(kind, filename string, exists bool) {
	filename = cleanPackagePath(filename)
	if filename == "" {
		return
	}
	assets := a.resourceAssets[kind]
	if assets == nil {
		assets = make(map[string]bool)
		a.resourceAssets[kind] = assets
	}
	if previous, ok := assets[filename]; ok {
		exists = previous || exists
	}
	assets[filename] = exists
	if !exists {
		a.addMissingFileWarning("资源文件不存在", filename)
	}
}

func (a *analyzer) finish() {
	a.report.Summary.Attachments = len(a.report.Attachments)
	a.report.Summary.Annotations = len(a.report.Annotations)
	a.report.Summary.Signatures = len(a.report.Signatures)
	a.report.Images = a.resourceSummary("image", a.images, a.usedImages)
	a.report.Fonts = FontResourceSummary{ResourceSummary: a.resourceSummary("font", a.fonts, a.usedFonts), Embedded: a.embeddedFonts}
	a.report.DrawParams = DrawParamResourceSummary{ResourceSummary: a.resourceSummary("draw-param", a.drawParams, a.usedDrawParams), InheritanceCycles: a.countDrawParamInheritanceCycles()}
	a.report.ColorSpaces = a.resourceSummary("color-space", a.colorSpaces, a.usedColorSpaces)
	a.report.Templates = a.resourceSummary("template", a.templates, a.usedTemplates)
	a.report.Composites = a.resourceSummary("composite", a.composites, a.usedComposites)
	a.report.Patterns = a.resourceSummary("pattern", a.patterns, a.usedPatterns)
	a.report.Resources = a.allResourceSummary()
	a.report.ResourceDetails = append([]ResourceInfo(nil), a.resources...)
	for index := range a.report.ResourceDetails {
		resource := &a.report.ResourceDetails[index]
		key := resourceKey(resource.DocumentIndex, resource.Kind, models.StID(resource.ID))
		switch resource.Kind {
		case "image":
			resource.Used = a.usedImages[key]
		case "font":
			resource.Used = a.usedFonts[key]
		case "draw-param":
			resource.Used = a.usedDrawParams[key]
		case "composite":
			resource.Used = a.usedComposites[key]
		case "color-space":
			resource.Used = a.usedColorSpaces[key]
		}
	}
	a.report.Summary.Objects = a.report.Summary.PageObjects + a.report.Summary.AnnotationObjects
	a.report.Summary.Images = a.report.Images.Declared
	a.report.Summary.Fonts = a.report.Fonts.Declared
	a.report.Summary.DrawParams = a.report.DrawParams.Declared
	a.report.Summary.ColorSpaces = a.report.ColorSpaces.Declared
	a.report.Summary.Templates = a.report.Templates.Declared
	a.report.Summary.Composites = a.report.Composites.Declared
	a.report.Summary.Patterns = a.report.Patterns.Declared
	a.report.FileReferences = sortedEdges(a.fileEdges)
	a.report.IDReferences = sortedEdges(a.idEdges)
	for index := range a.report.IDReferences {
		a.report.IDReferences[index].Exists = a.displayResourceExists(a.report.IDReferences[index].To)
	}
	sort.SliceStable(a.report.Documents, func(i, j int) bool { return a.report.Documents[i].Index < a.report.Documents[j].Index })
	sort.SliceStable(a.report.Pages, func(i, j int) bool { return a.report.Pages[i].PageNumber < a.report.Pages[j].PageNumber })
	sort.SliceStable(a.report.ResourceDetails, func(i, j int) bool {
		left, right := a.report.ResourceDetails[i], a.report.ResourceDetails[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.DocumentIndex != right.DocumentIndex {
			return left.DocumentIndex < right.DocumentIndex
		}
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		if left.SourceFile != right.SourceFile {
			return left.SourceFile < right.SourceFile
		}
		if left.Scope != right.Scope {
			return left.Scope < right.Scope
		}
		return left.Path < right.Path
	})
	sort.SliceStable(a.report.Attachments, func(i, j int) bool {
		left, right := a.report.Attachments[i], a.report.Attachments[j]
		if left.DocumentIndex != right.DocumentIndex {
			return left.DocumentIndex < right.DocumentIndex
		}
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		return left.Path < right.Path
	})
	sort.SliceStable(a.report.Annotations, func(i, j int) bool {
		if a.report.Annotations[i].DocumentIndex != a.report.Annotations[j].DocumentIndex {
			return a.report.Annotations[i].DocumentIndex < a.report.Annotations[j].DocumentIndex
		}
		if a.report.Annotations[i].PageID != a.report.Annotations[j].PageID {
			return a.report.Annotations[i].PageID < a.report.Annotations[j].PageID
		}
		return a.report.Annotations[i].ID < a.report.Annotations[j].ID
	})
	sort.SliceStable(a.report.Signatures, func(i, j int) bool {
		left, right := a.report.Signatures[i], a.report.Signatures[j]
		if left.DocumentIndex != right.DocumentIndex {
			return left.DocumentIndex < right.DocumentIndex
		}
		if left.ID != right.ID {
			return left.ID < right.ID
		}
		return left.Path < right.Path
	})
	if a.report.Summary.Pages != a.report.Summary.ParsedPages {
		a.addWarning(fmt.Sprintf("声明页面数(%d)与成功解析页面数(%d)不一致", a.report.Summary.Pages, a.report.Summary.ParsedPages))
	}
	sort.Strings(a.report.Warnings)
	if len(a.report.Errors) > 0 {
		a.report.Status = StatusFailed
	} else if len(a.report.Warnings) > 0 {
		a.report.Status = StatusPartial
	} else {
		a.report.Status = StatusComplete
	}
}

func (a *analyzer) displayResourceExists(value string) bool {
	if !strings.HasPrefix(value, "doc[") {
		return false
	}
	closeBracket := strings.IndexByte(value, ']')
	if closeBracket <= len("doc[") || closeBracket+2 >= len(value) || value[closeBracket+1] != '/' {
		return false
	}
	documentIndex, err := strconv.Atoi(value[len("doc["):closeBracket])
	if err != nil {
		return false
	}
	parts := strings.SplitN(value[closeBracket+2:], ":", 2)
	if len(parts) != 2 {
		return false
	}
	id, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return false
	}
	return a.hasResource(documentIndex, parts[0], models.StID(id))
}

func (a *analyzer) resourceSummary(kind string, definitions map[string]int, used map[string]int) ResourceSummary {
	summary := ResourceSummary{ByType: make(map[string]int)}
	for key, declarations := range definitions {
		if declarations <= 0 {
			declarations = 1
		}
		summary.Declared += declarations
		id := key
		if count := used[id]; count > 0 {
			summary.Used += count
			summary.UniqueUsed++
		} else {
			summary.Unused += declarations
		}
		kindName := kind
		parts := strings.SplitN(key, ":", 3)
		if len(parts) == 3 {
			kindName = parts[1]
		}
		summary.ByType[kindName] += declarations
	}
	for key, count := range used {
		if _, exists := definitions[key]; !exists {
			summary.Unresolved += count
		}
	}
	summary.References = summary.Used + summary.Unresolved
	for filename, exists := range a.resourceAssets[kind] {
		summary.Files++
		if !exists {
			summary.MissingFiles++
		}
		_ = filename
	}
	return summary
}

func (a *analyzer) countDrawParamInheritanceCycles() int {
	cycles := make(map[string]struct{})
	for start := range a.drawParamRelative {
		position := make(map[string]int)
		path := make([]string, 0)
		current := start
		for current != "" {
			if index, exists := position[current]; exists {
				cycle := append([]string(nil), path[index:]...)
				sort.Strings(cycle)
				cycles[strings.Join(cycle, "\x00")] = struct{}{}
				break
			}
			position[current] = len(path)
			path = append(path, current)
			current = a.drawParamRelative[current]
		}
	}
	return len(cycles)
}

func (a *analyzer) allResourceSummary() ResourceSummary {
	result := ResourceSummary{ByType: make(map[string]int)}
	for _, kind := range []string{"image", "font", "draw-param", "composite", "color-space", "template", "pattern"} {
		var definitions, used map[string]int
		switch kind {
		case "image":
			definitions, used = a.images, a.usedImages
		case "font":
			definitions, used = a.fonts, a.usedFonts
		case "draw-param":
			definitions, used = a.drawParams, a.usedDrawParams
		case "composite":
			definitions, used = a.composites, a.usedComposites
		case "color-space":
			definitions, used = a.colorSpaces, a.usedColorSpaces
		case "template":
			definitions, used = a.templates, a.usedTemplates
		case "pattern":
			definitions, used = a.patterns, a.usedPatterns
		}
		category := a.resourceSummary(kind, definitions, used)
		result.Declared += category.Declared
		result.Used += category.Used
		result.References += category.References
		result.UniqueUsed += category.UniqueUsed
		result.Unresolved += category.Unresolved
		result.MissingFiles += category.MissingFiles
		result.Unused += category.Unused
		for typeName, count := range category.ByType {
			result.ByType[typeName] += count
		}
	}
	result.Files = len(a.resourceFiles)
	for _, exists := range a.resourceFiles {
		if !exists {
			result.MissingFiles++
		}
	}
	return result
}

func sortedEdges(values map[string]ReferenceEdge) []ReferenceEdge {
	result := make([]ReferenceEdge, 0, len(values))
	for _, edge := range values {
		result = append(result, edge)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].From != result[j].From {
			return result[i].From < result[j].From
		}
		if result[i].To != result[j].To {
			return result[i].To < result[j].To
		}
		return result[i].Type < result[j].Type
	})
	return result
}

func resolveFrom(base models.StLoc, value models.StLoc) string {
	if value == "" {
		return ""
	}
	if value.IsAbsolute() {
		return cleanPackagePath(value.String())
	}
	return cleanPackagePath(path.Join(base.String(), value.String()))
}

func cleanPackagePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimLeft(value, "/")
	if value == "" {
		return ""
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

// ValidateInputPath 检查输入路径是否为可读的普通文件，供 CLI 使用。
func ValidateInputPath(filename string) error {
	info, err := os.Stat(filename)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("输入路径是目录: %s", filepath.Clean(filename))
	}
	return nil
}

// IsValidUTF8 判断字符串是否包含有效 UTF-8。
func IsValidUTF8(value string) bool { return utf8.ValidString(value) }

// AnalyzeTextCharacters 返回字符串的 UTF-8 字节数和 Unicode 码点数。
func AnalyzeTextCharacters(value string) (bytes, codePoints int) {
	return len(value), utf8.RuneCountInString(value)
}
