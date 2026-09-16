// Package export 将 OFD 文档导出为 ofd-creator 可读取的 manifest。
package export

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/beevik/etree"
	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"go.yaml.in/yaml/v3"
)

// Options 控制 OFD manifest 导出。
type Options struct {
	// AssetRoot 是导出二进制资源的实际目录。
	AssetRoot string
	// AssetPrefix 是 manifest 中相对于 manifest 文件的资源目录前缀。
	AssetPrefix string
	// Format 是 manifest 输出格式，可选 yaml、json 或 toml。
	Format string
	// JSONIndent 控制 JSON 输出是否使用 2 空格缩进。
	JSONIndent bool
}

// BundleIndex 描述 ExportAll 生成的 manifest 集合。
type BundleIndex struct {
	Version   int              `json:"version" yaml:"version"`
	Documents []BundleDocument `json:"documents" yaml:"documents"`
}

// BundleDocument 标识导出目录包中的一个文档 manifest。
type BundleDocument struct {
	Index     int    `json:"index" yaml:"index"`
	ID        string `json:"id" yaml:"id"`
	Title     string `json:"title" yaml:"title"`
	Manifest  string `json:"manifest" yaml:"manifest"`
	AssetRoot string `json:"asset_root" yaml:"asset_root"`
	Pages     int    `json:"pages" yaml:"pages"`
}

// Export 将 OFD 导出为 manifest，并将字体和多媒体资源写入 AssetRoot。
// 导出的 manifest 可以作为 ofd-creator 的输入，但不承诺原始 OFD 的字节级还原。
func Export(input any, output io.Writer, options Options) error {
	return export(input, output, options, nil)
}

// ExportDocument 将指定索引的文档体导出为 manifest。
// index 从 0 开始；该 API 用于 OFD 包含多个文档体时选择其中一个文档。
func ExportDocument(input any, index int, output io.Writer, options Options) error {
	if index < 0 {
		return fmt.Errorf("文档体索引不能为负数: %d", index)
	}
	return export(input, output, options, &index)
}

// ExportAll 将 OFD 的全部文档体导出为一个目录包。
// 每个文档体拥有独立的 manifest 和资源目录，根目录下同时写入索引文件。
func ExportAll(input any, outputDir string) error {
	return ExportAllWithOptions(input, outputDir, Options{Format: "yaml"})
}

// ExportAllWithFormat 将 OFD 的全部文档体导出为指定格式的目录包。
func ExportAllWithFormat(input any, outputDir, format string) error {
	return ExportAllWithOptions(input, outputDir, Options{Format: format})
}

// ExportAllWithOptions 将 OFD 的全部文档体导出为指定格式的目录包，并应用输出选项。
func ExportAllWithOptions(input any, outputDir string, options Options) error {
	if strings.TrimSpace(outputDir) == "" {
		return errors.New("未设置批量导出目录")
	}
	root, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("批量导出目录无效: %w", err)
	}
	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return fmt.Errorf("创建批量导出父目录失败: %w", err)
	}
	if _, err := os.Stat(root); err == nil {
		return fmt.Errorf("批量导出目录已存在: %s", outputDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("检查批量导出目录失败: %w", err)
	}
	temporary, err := os.MkdirTemp(parent, "."+filepath.Base(root)+"-*")
	if err != nil {
		return fmt.Errorf("创建批量导出临时目录失败: %w", err)
	}
	defer func() { _ = os.RemoveAll(temporary) }()

	ofd, err := parser.NewOFD(input)
	if err != nil {
		return fmt.Errorf("解析 OFD 失败: %w", err)
	}
	defer func() { _ = ofd.Close() }()
	if len(ofd.Documents) == 0 {
		return errors.New("没有文档体")
	}
	format, err := normalizeFormat(options.Format)
	if err != nil {
		return err
	}
	index := BundleIndex{Version: 1, Documents: make([]BundleDocument, 0, len(ofd.Documents))}
	extension := format
	for documentIndex, document := range ofd.Documents {
		if document == nil {
			return fmt.Errorf("文档体为空: %d", documentIndex)
		}
		manifestData, documentInfo, err := exportDocument(ofd, documentIndex, Options{
			AssetRoot:   filepath.Join(temporary, "documents", fmt.Sprintf("%03d-assets", documentIndex)),
			AssetPrefix: fmt.Sprintf("%03d-assets", documentIndex),
			Format:      format,
			JSONIndent:  options.JSONIndent,
		})
		if err != nil {
			return fmt.Errorf("导出文档体 %d 失败: %w", documentIndex, err)
		}
		manifestPath := filepath.Join(temporary, "documents", fmt.Sprintf("%03d.%s", documentIndex, extension))
		if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
			return fmt.Errorf("创建文档 manifest 目录失败: %w", err)
		}
		if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
			return fmt.Errorf("写出文档体 %d manifest 失败: %w", documentIndex, err)
		}
		index.Documents = append(index.Documents, BundleDocument{
			Index: documentIndex, ID: documentInfo.ID, Title: documentInfo.Title,
			Manifest:  path.Join("documents", fmt.Sprintf("%03d.%s", documentIndex, extension)),
			AssetRoot: path.Join("documents", fmt.Sprintf("%03d-assets", documentIndex)), Pages: len(ofd.Documents[documentIndex].Pages),
		})
	}
	indexData, err := marshalBundleIndex(index, format, options.JSONIndent)
	if err != nil {
		return fmt.Errorf("编码批量导出索引失败: %w", err)
	}
	if err := os.WriteFile(filepath.Join(temporary, "index."+extension), indexData, 0644); err != nil {
		return fmt.Errorf("写出批量导出索引失败: %w", err)
	}
	if err := os.Rename(temporary, root); err != nil {
		return fmt.Errorf("替换批量导出目录失败: %w", err)
	}
	return nil
}

func export(input any, output io.Writer, options Options, documentIndex *int) error {
	if output == nil {
		return errors.New("未设置 manifest 输出参数")
	}
	if strings.TrimSpace(options.AssetRoot) == "" {
		return errors.New("未设置资源输出目录")
	}
	ofd, err := parser.NewOFD(input)
	if err != nil {
		return fmt.Errorf("解析 OFD 失败: %w", err)
	}
	defer func() { _ = ofd.Close() }()
	if len(ofd.Documents) == 0 {
		return errors.New("没有文档体")
	}
	if documentIndex == nil && len(ofd.Documents) > 1 {
		return errors.New("第一版导出暂不支持多个文档体")
	}
	index := 0
	if documentIndex != nil {
		index = *documentIndex
	}
	if index >= len(ofd.Documents) {
		return fmt.Errorf("文档体索引超出范围: %d，共有 %d 个文档体", index, len(ofd.Documents))
	}
	if ofd.Documents[index] == nil {
		return errors.New("文档体为空")
	}

	data, _, err := exportDocument(ofd, index, options)
	if err != nil {
		return err
	}
	if _, err := output.Write(data); err != nil {
		return fmt.Errorf("写入 manifest 失败: %w", err)
	}
	return nil
}

func exportDocument(ofd *parser.OFD, index int, options Options) ([]byte, manifest.Document, error) {
	document := ofd.Documents[index]
	for _, page := range document.Pages {
		if page == nil {
			continue
		}
		if err := page.EnsureLoaded(); err != nil {
			return nil, manifest.Document{}, fmt.Errorf("加载页面资源失败: %w", err)
		}
	}
	pageIndexes := make(map[models.StID]int, len(document.Pages))
	for pageIndex, page := range document.Pages {
		if page != nil {
			pageIndexes[page.ID] = pageIndex
		}
	}
	exporter := &documentExporter{
		ofd:               ofd,
		document:          document,
		documentIndex:     index,
		assetRoot:         options.AssetRoot,
		assetPrefix:       filepath.ToSlash(options.AssetPrefix),
		fonts:             make(map[models.StID]string),
		fontNames:         make(map[string]bool),
		media:             make(map[models.StID]string),
		drawParams:        make(map[models.StID]string),
		composites:        make(map[models.StID]bool),
		pageIndexes:       pageIndexes,
		attachmentIDs:     make(map[string]string),
		colorSpaces:       make(map[models.StID]string),
		pageDrawParams:    make(map[models.StID]string),
		pageFonts:         make(map[models.StID]bool),
		pageComposites:    make(map[models.StID]bool),
		pageMedia:         make(map[models.StID]bool),
		pageColorSpaces:   make(map[models.StID]bool),
		publicDrawParams:  make(map[models.StID]bool),
		publicFonts:       make(map[models.StID]bool),
		publicComposites:  make(map[models.StID]bool),
		publicMedia:       make(map[models.StID]bool),
		publicColorSpaces: make(map[models.StID]bool),
		pageResources:     nil,
	}
	result, err := exporter.build()
	if err != nil {
		return nil, manifest.Document{}, err
	}
	output, err := marshalManifest(result, options.Format, options.JSONIndent)
	if err != nil {
		return nil, manifest.Document{}, err
	}
	return output, result.Document, nil
}

func normalizeFormat(format string) (string, error) {
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" || format == "yml" {
		format = "yaml"
	}
	if format != "yaml" && format != "json" && format != "toml" {
		return "", fmt.Errorf("不支持的 manifest 格式 %q", format)
	}
	return format, nil
}

func marshalManifest(value manifest.Manifest, format string, jsonIndent bool) ([]byte, error) {
	format, err := normalizeFormat(format)
	if err != nil {
		return nil, err
	}
	switch format {
	case "json":
		var data []byte
		if jsonIndent {
			data, err = json.MarshalIndent(value, "", "  ")
		} else {
			data, err = json.Marshal(value)
		}
		if err != nil {
			return nil, fmt.Errorf("编码 JSON manifest 失败: %w", err)
		}
		return append(data, '\n'), nil
	case "toml":
		data, err := toml.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("编码 TOML manifest 失败: %w", err)
		}
		return data, nil
	default:
		var output bytes.Buffer
		encoder := yaml.NewEncoder(&output)
		encoder.SetIndent(2)
		if err := encoder.Encode(value); err != nil {
			_ = encoder.Close()
			return nil, fmt.Errorf("编码 YAML manifest 失败: %w", err)
		}
		if err := encoder.Close(); err != nil {
			return nil, fmt.Errorf("关闭 YAML manifest 编码器失败: %w", err)
		}
		return output.Bytes(), nil
	}
}

func marshalBundleIndex(value BundleIndex, format string, jsonIndent bool) ([]byte, error) {
	format, err := normalizeFormat(format)
	if err != nil {
		return nil, err
	}
	switch format {
	case "json":
		var data []byte
		if jsonIndent {
			data, err = json.MarshalIndent(value, "", "  ")
		} else {
			data, err = json.Marshal(value)
		}
		if err != nil {
			return nil, fmt.Errorf("编码 JSON 批量导出索引失败: %w", err)
		}
		return append(data, '\n'), nil
	case "toml":
		data, err := toml.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("编码 TOML 批量导出索引失败: %w", err)
		}
		return data, nil
	default:
		data, err := yaml.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("编码 YAML 批量导出索引失败: %w", err)
		}
		return data, nil
	}
}

type documentExporter struct {
	ofd               *parser.OFD
	document          *parser.Document
	documentIndex     int
	assetRoot         string
	assetPrefix       string
	fonts             map[models.StID]string
	fontNames         map[string]bool
	media             map[models.StID]string
	drawParams        map[models.StID]string
	composites        map[models.StID]bool
	pageIndexes       map[models.StID]int
	attachmentIDs     map[string]string
	colorSpaces       map[models.StID]string
	pageDrawParams    map[models.StID]string
	pageFonts         map[models.StID]bool
	pageComposites    map[models.StID]bool
	pageMedia         map[models.StID]bool
	pageColorSpaces   map[models.StID]bool
	publicDrawParams  map[models.StID]bool
	publicFonts       map[models.StID]bool
	publicComposites  map[models.StID]bool
	publicMedia       map[models.StID]bool
	publicColorSpaces map[models.StID]bool
	pageResources     []resourceSource
}

type resourceSource struct {
	value *models.Res
	path  models.StLoc
	data  []byte
}

func (e *documentExporter) preparePageResourceMetadata() error {
	for _, page := range e.document.Pages {
		if page == nil {
			continue
		}
		for _, location := range page.PageResourceLocations() {
			data, err := e.document.FileCache.Read(location.String())
			if err != nil {
				return fmt.Errorf("读取页面资源 %q 失败: %w", location, err)
			}
			doc := etree.NewDocument()
			if err := doc.ReadFromBytes(data); err != nil {
				return fmt.Errorf("页面资源 %q XML 无效: %w", location, err)
			}
			root := doc.Root()
			if root == nil || strings.TrimPrefix(root.Tag, "ofd:") != "Res" {
				return fmt.Errorf("页面资源 %q 根元素不是 Res", location)
			}
			e.normalizeResourceXML(root)
			data, err = doc.WriteToBytes()
			if err != nil {
				return fmt.Errorf("序列化页面资源 %q 失败: %w", location, err)
			}
			var resource models.Res
			if err := xml.Unmarshal(data, &resource); err != nil {
				return fmt.Errorf("读取页面资源 %q 失败: %w", location, err)
			}
			e.pageResources = append(e.pageResources, resourceSource{value: &resource, path: location, data: data})
			if resource.ColorSpaces != nil {
				for _, space := range resource.ColorSpaces.ColorSpace {
					e.colorSpaces[space.ID] = space.Type
					e.pageColorSpaces[space.ID] = true
				}
			}
			if resource.DrawParams != nil {
				for _, param := range resource.DrawParams.DrawParam {
					if param == nil {
						continue
					}
					e.pageDrawParams[param.ID] = "#" + strconv.FormatUint(uint64(param.ID), 10)
				}
			}
			if resource.Fonts != nil {
				for _, font := range resource.Fonts.Font {
					e.pageFonts[font.ID] = true
				}
			}
			if resource.CompositeGraphicUnits != nil {
				for _, composite := range resource.CompositeGraphicUnits.CompositeGraphicUnit {
					e.pageComposites[composite.ID] = true
				}
			}
			if resource.MultiMedias != nil {
				for _, media := range resource.MultiMedias.MultiMedia {
					if media != nil {
						e.pageMedia[media.ID] = true
					}
				}
			}
		}
	}
	for _, resource := range e.document.PublicResourceList() {
		e.markPublicResource(resource)
	}
	return nil
}

func (e *documentExporter) markPublicResource(resource *models.Res) {
	if resource == nil {
		return
	}
	if resource.ColorSpaces != nil {
		for _, space := range resource.ColorSpaces.ColorSpace {
			e.colorSpaces[space.ID] = space.Type
			e.publicColorSpaces[space.ID] = true
		}
	}
	if resource.DrawParams != nil {
		for _, param := range resource.DrawParams.DrawParam {
			if param != nil {
				e.publicDrawParams[param.ID] = true
				e.drawParams[param.ID] = "#" + strconv.FormatUint(uint64(param.ID), 10)
			}
		}
	}
	if resource.Fonts != nil {
		for _, font := range resource.Fonts.Font {
			e.publicFonts[font.ID] = true
			e.fonts[font.ID] = font.FontName
		}
	}
	if resource.CompositeGraphicUnits != nil {
		for _, composite := range resource.CompositeGraphicUnits.CompositeGraphicUnit {
			e.publicComposites[composite.ID] = true
		}
	}
	if resource.MultiMedias != nil {
		for _, media := range resource.MultiMedias.MultiMedia {
			if media != nil {
				e.publicMedia[media.ID] = true
			}
		}
	}
}

func (e *documentExporter) build() (manifest.Manifest, error) {
	if err := os.MkdirAll(e.assetRoot, 0755); err != nil {
		return manifest.Manifest{}, fmt.Errorf("创建资源目录失败: %w", err)
	}
	result := manifest.Manifest{Version: 1}
	if err := e.preparePageResourceMetadata(); err != nil {
		return manifest.Manifest{}, err
	}
	documentInfo, err := e.documentInfo()
	if err != nil {
		return manifest.Manifest{}, err
	}
	result.Document = documentInfo
	if err := e.exportPublicResources(&result); err != nil {
		return manifest.Manifest{}, err
	}
	if err := e.exportFonts(&result); err != nil {
		return manifest.Manifest{}, err
	}
	if err := e.exportMedia(&result); err != nil {
		return manifest.Manifest{}, err
	}
	if err := e.exportColorSpaces(&result); err != nil {
		return manifest.Manifest{}, err
	}
	if err := e.exportPages(&result); err != nil {
		return manifest.Manifest{}, err
	}
	if err := e.exportTemplates(&result); err != nil {
		return manifest.Manifest{}, err
	}
	customTags, err := e.exportCustomTags()
	if err != nil {
		return manifest.Manifest{}, err
	}
	result.Resources.CustomTags = customTags
	if err := e.exportComposites(&result); err != nil {
		return manifest.Manifest{}, err
	}
	if err := e.exportDrawParams(&result); err != nil {
		return manifest.Manifest{}, err
	}
	if len(result.Pages) == 0 {
		return manifest.Manifest{}, errors.New("文档没有页面")
	}
	return result, nil
}

func (e *documentExporter) exportDrawParams(result *manifest.Manifest) error {
	params := make(map[models.StID]*models.DrawParam)
	for _, resources := range [][]*models.Res{e.document.PublicResourceList(), e.document.DocumentResourceList()} {
		for _, resource := range resources {
			if resource == nil || resource.DrawParams == nil {
				continue
			}
			for _, param := range resource.DrawParams.DrawParam {
				if param != nil {
					if _, pageOnly := e.pageDrawParams[param.ID]; pageOnly {
						continue
					}
					if e.publicDrawParams[param.ID] {
						continue
					}
					params[param.ID] = param
				}
			}
		}
	}
	for _, page := range e.document.Pages {
		if page == nil {
			continue
		}
		_ = page.WithPageContent(func(content *models.PageContent) error {
			if content.Content != nil {
				for _, layer := range content.Content.Layer {
					if layer != nil {
						if layer.DrawParam != 0 {
							if param := e.document.GetDrawParam(models.StID(layer.DrawParam)); param != nil {
								params[models.StID(layer.DrawParam)] = param
							}
						}
						e.collectDrawParams(params, layer.Items)
					}
				}
			}
			return nil
		})
	}
	for id := range e.composites {
		if unit := e.document.GetCompositeUnit(id); unit != nil {
			e.collectDrawParams(params, unit.Content.Items)
		}
	}
	for _, definition := range e.document.CommonData.TemplatePages {
		content, err := e.document.LoadTemplate(definition.ID)
		if err != nil {
			return fmt.Errorf("加载模板页 %d 失败: %w", definition.ID, err)
		}
		if content != nil && content.Content != nil {
			for _, layer := range content.Content.Layer {
				if layer == nil {
					continue
				}
				if layer.DrawParam != 0 {
					if param := e.document.GetDrawParam(models.StID(layer.DrawParam)); param != nil {
						params[models.StID(layer.DrawParam)] = param
					}
				}
				e.collectDrawParams(params, layer.Items)
			}
		}
	}
	for id := range e.drawParams {
		if _, ok := params[id]; !ok {
			params[id] = e.document.GetDrawParam(id)
		}
	}
	ids := sortedIDs(params)
	for _, id := range ids {
		if _, pageOnly := e.pageDrawParams[id]; pageOnly || e.publicDrawParams[id] {
			continue
		}
		param := params[id]
		if param == nil {
			return fmt.Errorf("绘制参数 %d 不存在", id)
		}
		value := manifest.DrawParam{
			Name:        e.drawParamName(models.StRefID(id)),
			LineWidth:   param.LineWidth,
			Join:        param.Join,
			Cap:         param.Cap,
			DashOffset:  param.DashOffset,
			MiterLimit:  param.MiterLimit,
			DashPattern: exportFloatArray(param.DashPattern),
			FillColor:   e.exportColor(param.FillColor),
			StrokeColor: e.exportColor(param.StrokeColor),
		}
		if param.Relative != 0 {
			value.Relative = e.drawParamName(param.Relative)
		}
		result.Resources.DrawParams = append(result.Resources.DrawParams, value)
	}
	return nil
}

func (e *documentExporter) collectDrawParams(params map[models.StID]*models.DrawParam, items []models.PageItem) {
	for _, item := range items {
		var id models.StRefID
		switch item.Kind {
		case models.PageItemText:
			id = item.Text.DrawParam
		case models.PageItemPath:
			id = item.Path.DrawParam
		case models.PageItemImage:
			id = item.Image.DrawParam
		case models.PageItemComposite:
			id = item.Composite.DrawParam
			composite := item.Composite.CtComposite
			compositeID := models.StID(composite.ResourceID)
			if compositeID != 0 && !e.composites[compositeID] {
				e.composites[compositeID] = true
				if unit := e.document.GetCompositeUnit(compositeID); unit != nil {
					e.collectDrawParams(params, unit.Content.Items)
				}
			}
		case models.PageItemBlock:
			e.collectDrawParams(params, item.Block.Items)
		}
		if id != 0 {
			if param := e.document.GetDrawParam(models.StID(id)); param != nil {
				params[models.StID(id)] = param
			}
		}
	}
}

func (e *documentExporter) exportComposites(result *manifest.Manifest) error {
	exported := make(map[models.StID]bool, len(e.composites))
	for len(exported) < len(e.composites) {
		ids := make([]models.StID, 0, len(e.composites))
		for id := range e.composites {
			if !exported[id] {
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			exported[id] = true
			if e.pageComposites[id] || e.publicComposites[id] {
				continue
			}
			unit := e.document.GetCompositeUnit(id)
			if unit == nil {
				continue
			}
			items, err := e.convertItems(unit.Content.Items)
			if err != nil {
				return fmt.Errorf("转换复合图元 %d 失败: %w", id, err)
			}
			result.Resources.Composites = append(result.Resources.Composites, manifest.Composite{ID: uint64(id), Width: unit.Width, Height: unit.Height, Thumbnail: uint64(unit.Thumbnail), Substitution: uint64(unit.Substitution), Items: items})
		}
	}
	return nil
}

func (e *documentExporter) documentInfo() (manifest.Document, error) {
	info := manifest.Document{}
	if err := e.prepareAttachmentIDs(); err != nil {
		return manifest.Document{}, err
	}
	if e.documentIndex >= 0 && e.documentIndex < len(e.ofd.DocBodies) {
		docInfo := e.ofd.DocBodies[e.documentIndex].DocInfo
		info.ID = docInfo.DocID
		info.Title = valueOrEmpty(docInfo.Title)
		info.Author = valueOrEmpty(docInfo.Author)
		info.Subject = valueOrEmpty(docInfo.Subject)
		info.Abstract = valueOrEmpty(docInfo.Abstract)
		info.Creator = valueOrEmpty(docInfo.Creator)
		info.CreatorVersion = valueOrEmpty(docInfo.CreatorVersion)
		info.DocUsage = valueOrEmpty(docInfo.DocUsage)
		if docInfo.Keywords != nil {
			info.Keywords = append([]string(nil), docInfo.Keywords.Keyword...)
		}
		if docInfo.CustomDatas != nil {
			for _, value := range docInfo.CustomDatas.CustomData {
				info.CustomData = append(info.CustomData, manifest.CustomData{Name: value.Name, Value: value.Value})
			}
		}
		if docInfo.CreationDate != nil && !docInfo.CreationDate.IsZero() {
			info.CreationDate = docInfo.CreationDate.Format(time.RFC3339)
		}
		if docInfo.ModDate != nil && !docInfo.ModDate.IsZero() {
			info.ModDate = docInfo.ModDate.Format(time.RFC3339)
		}
		if docInfo.Cover != nil && *docInfo.Cover != "" {
			file := docInfo.Cover.Resolve(e.document.BaseLoc)
			data, err := e.document.FileCache.Read(file.String())
			if err != nil {
				return manifest.Document{}, fmt.Errorf("读取封面失败: %w", err)
			}
			name := docInfo.Cover.Base()
			if name == "" || name == "." || name == ".." {
				name = "cover.bin"
			}
			asset := filepath.Join("document", "cover-"+name)
			if err := e.writeAsset(asset, data); err != nil {
				return manifest.Document{}, fmt.Errorf("写出封面失败: %w", err)
			}
			info.Cover = e.assetPath(asset)
			info.CoverName = name
		}
		if e.document.Actions != nil {
			actions, err := e.exportActionList(e.document.Actions.Actions)
			if err != nil {
				return manifest.Document{}, fmt.Errorf("转换文档动作失败: %w", err)
			}
			info.Actions = actions
		}
		if e.document.Bookmarks != nil {
			for index, bookmark := range e.document.Bookmarks.Bookmarks {
				gotoValue, err := e.exportDestination(bookmark.Dest)
				if err != nil {
					return manifest.Document{}, fmt.Errorf("转换书签 %d 失败: %w", index, err)
				}
				info.Bookmarks = append(info.Bookmarks, manifest.Bookmark{Name: bookmark.Name, Goto: gotoValue})
			}
		}
		if e.document.Outlines != nil {
			outlines, err := e.exportOutlines(e.document.Outlines.OutlineElems)
			if err != nil {
				return manifest.Document{}, fmt.Errorf("转换文档大纲失败: %w", err)
			}
			info.Outlines = outlines
		}
	}
	area := e.document.CommonData.PageArea
	info.PageSize = manifest.PageSize{Width: area.PhysicalBox.Width, Height: area.PhysicalBox.Height}
	info.Area = exportPageArea(&area)
	if e.document.CommonData.DefaultCS != nil {
		info.DefaultCS = uint64(*e.document.CommonData.DefaultCS)
	}
	if value := e.document.Permissions; value != nil {
		info.Permissions = exportPermissions(value)
	}
	if value := e.document.VPreferences; value != nil {
		info.Preferences = exportPreferences(value)
	}
	if err := e.exportAttachments(&info); err != nil {
		return manifest.Document{}, err
	}
	if err := e.exportExtensions(&info); err != nil {
		return manifest.Document{}, err
	}
	return info, nil
}

func (e *documentExporter) exportAttachments(info *manifest.Document) error {
	value, err := e.document.LoadAttachments()
	if err != nil {
		return fmt.Errorf("读取附件清单失败: %w", err)
	}
	if value == nil || e.document.Document.Attachments == nil {
		return nil
	}
	listPath := e.document.Document.Attachments.Resolve(e.document.BaseLoc)
	for index, attachment := range value.Attachments {
		_, data, err := e.readDocumentFile(attachment.FileLoc, listPath)
		if err != nil {
			return fmt.Errorf("读取附件 %q 失败: %w", attachment.Name, err)
		}
		asset := filepath.Join("attachments", fmt.Sprintf("attachment-%03d%s", index, safeExtension(attachment.FileLoc.Base())))
		if err := e.writeAsset(asset, data); err != nil {
			return fmt.Errorf("写出附件 %q 失败: %w", attachment.Name, err)
		}
		id := e.attachmentIDs[attachment.ID]
		if id == "" {
			id = fmt.Sprintf("attachment-%d", index)
		}
		item := manifest.Attachment{ID: id, Name: attachment.Name, Usage: attachment.Usage, File: e.assetPath(asset), FileName: attachment.FileLoc.Base()}
		if attachment.Format != nil {
			item.Format = *attachment.Format
		}
		item.CreationDate = formatDateTime(attachment.CreationDate)
		item.ModDate = formatDateTime(attachment.ModDate)
		item.Visible = attachment.Visible.Bool()
		info.Attachments = append(info.Attachments, item)
	}
	return nil
}

func (e *documentExporter) prepareAttachmentIDs() error {
	value, err := e.document.LoadAttachments()
	if err != nil {
		return fmt.Errorf("读取附件清单失败: %w", err)
	}
	if value == nil {
		return nil
	}
	for index, attachment := range value.Attachments {
		id := attachment.ID
		if !validXMLID(id) {
			id = fmt.Sprintf("attachment-%d", index)
		}
		e.attachmentIDs[attachment.ID] = id
	}
	return nil
}

func validXMLID(value string) bool {
	if value == "" {
		return false
	}
	for index, r := range value {
		if index == 0 {
			if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
				return false
			}
			continue
		}
		if !(r == '_' || r == '-' || r == '.' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
}

func (e *documentExporter) exportCustomTags() ([]manifest.CustomTag, error) {
	value, err := e.document.LoadCustomTags()
	if err != nil {
		return nil, fmt.Errorf("读取自定义标签清单失败: %w", err)
	}
	if value == nil || e.document.Document.CustomTags == nil {
		return nil, nil
	}
	listPath := e.document.Document.CustomTags.Resolve(e.document.BaseLoc)
	result := make([]manifest.CustomTag, 0, len(value.CustomTags))
	for index, tag := range value.CustomTags {
		dataFile, data, err := e.readDocumentFile(tag.FileLoc, listPath)
		if err != nil {
			return nil, fmt.Errorf("读取自定义标签数据 %d 失败: %w", index, err)
		}
		nameSpace := tag.NameSpace
		if nameSpace == "" {
			nameSpace = tag.TypeID
		}
		if nameSpace == "" {
			// manifest 模式要求键稳定且非空，但部分旧版 OFD 文件的两个
			// 命名空间属性都为空。
			nameSpace = fmt.Sprintf("legacy-tag-%d", index)
		}
		item := manifest.CustomTag{NameSpace: nameSpace}
		dataAsset := filepath.Join("custom-tags", fmt.Sprintf("data-%03d.xml", index))
		if err := e.writeAsset(dataAsset, data); err != nil {
			return nil, fmt.Errorf("写出自定义标签数据 %d 失败: %w", index, err)
		}
		item.Data = e.assetPath(dataAsset)
		item.DataName = dataFile.Base()
		if tag.SchemaLoc != nil && *tag.SchemaLoc != "" {
			schemaFile, schema, err := e.readDocumentFile(*tag.SchemaLoc, listPath)
			if err != nil {
				return nil, fmt.Errorf("读取自定义标签 Schema %d 失败: %w", index, err)
			}
			schemaAsset := filepath.Join("custom-tags", fmt.Sprintf("schema-%03d.xsd", index))
			if err := e.writeAsset(schemaAsset, schema); err != nil {
				return nil, fmt.Errorf("写出自定义标签 Schema %d 失败: %w", index, err)
			}
			item.Schema = e.assetPath(schemaAsset)
			item.SchemaName = schemaFile.Base()
		}
		result = append(result, item)
	}
	return result, nil
}

func (e *documentExporter) exportColorSpaces(result *manifest.Manifest) error {
	sources := make([]resourceSource, 0, len(e.document.CommonData.PublicRes)+len(e.document.CommonData.DocumentRes))
	for index, location := range e.document.CommonData.PublicRes {
		resources := e.document.PublicResourceList()
		if index < len(resources) {
			sources = append(sources, resourceSource{value: resources[index], path: location.Resolve(e.document.BaseLoc)})
		}
	}
	for index, location := range e.document.CommonData.DocumentRes {
		resources := e.document.DocumentResourceList()
		if index < len(resources) {
			sources = append(sources, resourceSource{value: resources[index], path: location.Resolve(e.document.BaseLoc)})
		}
	}
	seen := make(map[models.StID]bool)
	for _, source := range sources {
		if source.value == nil || source.value.ColorSpaces == nil {
			continue
		}
		for _, space := range source.value.ColorSpaces.ColorSpace {
			if seen[space.ID] || e.pageColorSpaces[space.ID] || e.publicColorSpaces[space.ID] {
				continue
			}
			seen[space.ID] = true
			e.colorSpaces[space.ID] = space.Type
			item := manifest.ColorSpace{ID: uint64(space.ID), Type: space.Type, BitsPerComponent: space.BitsPerComponent}
			if space.Palette != nil {
				for _, value := range space.Palette.CV {
					item.Palette = append(item.Palette, strings.Join(value, " "))
				}
			}
			if space.Profile != "" {
				profilePath := space.Profile.Resolve(source.path.Dir().Join(string(source.value.BaseLoc)))
				data, err := e.document.FileCache.Read(profilePath.String())
				if err != nil {
					return fmt.Errorf("读取颜色空间 %d Profile 失败: %w", space.ID, err)
				}
				asset := filepath.Join("profiles", fmt.Sprintf("profile-%d%s", space.ID, safeExtension(profilePath.Base())))
				if err := e.writeAsset(asset, data); err != nil {
					return fmt.Errorf("写出颜色空间 %d Profile 失败: %w", space.ID, err)
				}
				item.ProfileFile = e.assetPath(asset)
				item.ProfileName = profilePath.Base()
			}
			result.Resources.ColorSpaces = append(result.Resources.ColorSpaces, item)
		}
	}
	return nil
}

func (e *documentExporter) exportExtensions(info *manifest.Document) error {
	value, err := e.document.LoadExtensions()
	if err != nil {
		return fmt.Errorf("读取扩展清单失败: %w", err)
	}
	if value == nil || e.document.Document.Extensions == nil {
		return nil
	}
	listPath := e.document.Document.Extensions.Resolve(e.document.BaseLoc)
	raw, err := e.document.FileCache.Read(listPath.String())
	if err != nil {
		return fmt.Errorf("读取扩展 XML 失败: %w", err)
	}
	for index, extension := range value.Extensions {
		dataXML := extensionDataXML(raw, index)
		item := manifest.Extension{AppName: extension.AppName, Company: valueOrEmpty(extension.Company), AppVersion: valueOrEmpty(extension.AppVersion), RefID: uint64(extension.RefID), DataXML: dataXML}
		if dataXML == "" && extension.Data != nil {
			item.Data = fmt.Sprint(*extension.Data)
		}
		if extension.Date != nil {
			item.Date = extension.Date.Format(time.RFC3339)
		}
		for _, property := range extension.Properties {
			item.Properties = append(item.Properties, manifest.ExtensionProperty{Name: property.Name, Type: valueOrEmpty(property.Type), Value: property.Value})
		}
		if extension.ExtendData != nil && *extension.ExtendData != "" {
			file, data, err := e.readDocumentFile(*extension.ExtendData, listPath)
			if err != nil {
				return fmt.Errorf("读取扩展数据 %d 失败: %w", index, err)
			}
			asset := filepath.Join("extensions", fmt.Sprintf("data-%03d%s", index, safeExtension(file.Base())))
			if err := e.writeAsset(asset, data); err != nil {
				return fmt.Errorf("写出扩展数据 %d 失败: %w", index, err)
			}
			item.DataFile = e.assetPath(asset)
			item.DataName = file.Base()
		}
		info.Extensions = append(info.Extensions, item)
	}
	return nil
}

func safeExtension(name string) string {
	ext := filepath.Ext(name)
	if ext == "" || strings.ContainsAny(ext, "/\\\x00") {
		return ".bin"
	}
	return strings.ToLower(ext)
}

// readDocumentFile 同时支持现有 OFD 生产者使用的相对于文档根目录的路径，
// 以及 creator 生成的相对于清单文件的路径。
func (e *documentExporter) readDocumentFile(location models.StLoc, listPath models.StLoc) (models.StLoc, []byte, error) {
	if location == "" {
		return "", nil, errors.New("资源文件位置为空")
	}
	candidates := []models.StLoc{location.Resolve(e.document.BaseLoc)}
	listRelative := location.Resolve(listPath.Dir())
	if listRelative != candidates[0] {
		candidates = append(candidates, listRelative)
	}
	var lastErr error
	for _, candidate := range candidates {
		data, err := e.document.FileCache.Read(candidate.String())
		if err == nil {
			return candidate, data, nil
		}
		lastErr = err
	}
	return candidates[0], nil, lastErr
}

// extensionDataXML 返回指定 Extension/Data 元素的子节点。
// 保留 XML 形式可以保留厂商自定义的扩展元素。
func extensionDataXML(data []byte, wanted int) string {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	extensionIndex := -1
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Extension" {
			continue
		}
		extensionIndex++
		if extensionIndex != wanted {
			continue
		}
		for {
			token, err = decoder.Token()
			if err != nil {
				return ""
			}
			if child, ok := token.(xml.StartElement); ok && child.Name.Local == "Data" {
				var content bytes.Buffer
				encoder := xml.NewEncoder(&content)
				depth := 1
				for depth > 0 {
					token, err = decoder.Token()
					if err != nil {
						return ""
					}
					switch token.(type) {
					case xml.StartElement:
						depth++
					case xml.EndElement:
						depth--
					}
					if depth > 0 {
						if err := encoder.EncodeToken(token); err != nil {
							return ""
						}
					}
				}
				if err := encoder.Flush(); err != nil {
					return ""
				}
				return strings.TrimSpace(content.String())
			}
			if end, ok := token.(xml.EndElement); ok && end.Name.Local == "Extension" {
				return ""
			}
		}
	}
}

func exportPermissions(value *models.CT_Permission) *manifest.Permissions {
	result := &manifest.Permissions{Edit: value.Edit, Annot: value.Annot, Export: value.Export, Signature: value.Signature, Watermark: value.Watermark, PrintScreen: value.PrintScreen}
	if value.Print != nil {
		copies := value.Print.Copies
		result.Print = &manifest.PrintSettings{Printable: value.Print.Printable, Copies: &copies}
	}
	if value.ValidPeriod != nil && (!value.ValidPeriod.StartDate.IsZero() || !value.ValidPeriod.EndDate.IsZero()) {
		result.ValidPeriod = &manifest.ValidPeriod{Start: formatDateTime(value.ValidPeriod.StartDate), End: formatDateTime(value.ValidPeriod.EndDate)}
	}
	return result
}

func exportPreferences(value *models.CT_VPreferences) *manifest.Preferences {
	result := &manifest.Preferences{}
	if value.PageMode != nil {
		result.PageMode = string(*value.PageMode)
	}
	if value.PageLayout != nil {
		result.PageLayout = string(*value.PageLayout)
	}
	if value.TabDisplay != nil {
		result.TabDisplay = string(*value.TabDisplay)
	}
	result.HideToolbar = value.HideToolbar
	result.HideMenubar = value.HideMenubar
	result.HideWindowUI = value.HideWindowUI
	if value.Zoom != nil {
		if value.Zoom.Mode != nil {
			result.ZoomMode = *value.Zoom.Mode
		}
		result.Zoom = value.Zoom.Value
	}
	return result
}

func formatDateTime(value models.DateTime) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func (e *documentExporter) exportDestination(value models.CtDest) (manifest.GotoAction, error) {
	page, ok := e.pageIndexes[models.StID(value.PageID)]
	if !ok {
		return manifest.GotoAction{}, fmt.Errorf("跳转引用了不存在的页面 ID %d", value.PageID)
	}
	return manifest.GotoAction{Page: page, Type: string(value.Type), Left: value.Left, Top: value.Top, Right: value.Right, Bottom: value.Bottom, Zoom: value.Zoom}, nil
}

func (e *documentExporter) exportOutlines(values []models.CTOutlineElem) ([]manifest.Outline, error) {
	result := make([]manifest.Outline, 0, len(values))
	for index, value := range values {
		item := manifest.Outline{Title: value.Title, Count: value.Count, Expanded: value.Expanded}
		if value.Actions != nil {
			actions, err := e.exportActionList(value.Actions.Actions)
			if err != nil {
				return nil, fmt.Errorf("[%d] 动作转换失败: %w", index, err)
			}
			item.Actions = actions
		}
		children, err := e.exportOutlines(value.OutlineElem)
		if err != nil {
			return nil, err
		}
		item.Children = children
		result = append(result, item)
	}
	return result, nil
}

func (e *documentExporter) exportTemplates(result *manifest.Manifest) error {
	for _, definition := range e.document.CommonData.TemplatePages {
		content, err := e.document.LoadTemplate(definition.ID)
		if err != nil {
			return fmt.Errorf("加载模板页 %d 失败: %w", definition.ID, err)
		}
		if content == nil {
			return fmt.Errorf("模板页 %d 内容为空", definition.ID)
		}
		page, err := e.convertPageContent(content)
		if err != nil {
			return fmt.Errorf("转换模板页 %d 失败: %w", definition.ID, err)
		}
		name := valueOrEmpty(definition.Name)
		zOrder := valueOrEmpty(definition.ZOrder)
		result.Templates = append(result.Templates, manifest.Template{ID: uint64(definition.ID), Name: name, ZOrder: zOrder, Area: page.Area, Layers: page.Layers, Items: page.Items})
	}
	return nil
}

func (e *documentExporter) exportFonts(result *manifest.Manifest) error {
	fonts := e.document.Fonts()
	ids := sortedIDs(fonts)
	for _, id := range ids {
		if e.pageFonts[id] || e.publicFonts[id] {
			continue
		}
		font := fonts[id]
		if font == nil {
			continue
		}
		file := ""
		format := ""
		if font.FontFile != "" {
			data, err := e.document.FileCache.Read(font.FontFile.String())
			if err != nil {
				return fmt.Errorf("读取字体 %d 失败: %w", id, err)
			}
			format = detectFontFormat(data, filepath.Ext(font.FontFile.Base()))
			if format == "" {
				return fmt.Errorf("无法识别字体 %d 的格式", id)
			}
			file = filepath.Join("fonts", fmt.Sprintf("font-%d.%s", id, format))
			if err := e.writeAsset(file, data); err != nil {
				return fmt.Errorf("写出字体 %d 失败: %w", id, err)
			}
		}
		name := font.FontName
		if name == "" {
			name = "font-" + strconv.FormatUint(uint64(id), 10)
		}
		name = e.uniqueFontName(name, id)
		e.fontNames[name] = true
		e.fonts[id] = name
		result.Resources.Fonts = append(result.Resources.Fonts, manifest.Font{
			Name: name, FamilyName: font.FamilyName, Charset: font.Charset,
			Format: format, File: e.assetPath(file),
			Italic: font.Italic, Bold: font.Bold, Serif: font.Serif, FixedWidth: font.FixedWidth,
		})
	}
	return nil
}

func (e *documentExporter) uniqueFontName(name string, id models.StID) string {
	if !e.fontNames[name] {
		return name
	}
	base := fmt.Sprintf("%s-%d", name, id)
	name = base
	for e.fontNames[name] {
		name = base + "-1"
	}
	return name
}

func (e *documentExporter) exportMedia(result *manifest.Manifest) error {
	medias := make(map[models.StID]*models.MultiMedia)
	e.document.ForEachMedia(func(id models.StID, media *models.MultiMedia) bool {
		medias[id] = media
		return true
	})
	ids := sortedIDs(medias)
	for _, id := range ids {
		if e.pageMedia[id] || e.publicMedia[id] {
			continue
		}
		media := medias[id]
		if media == nil || media.MediaFile == "" {
			continue
		}
		data, err := e.document.FileCache.Read(media.MediaFile.String())
		if err != nil {
			return fmt.Errorf("读取多媒体 %d 失败: %w", id, err)
		}
		ext := strings.ToLower(filepath.Ext(media.MediaFile.Base()))
		if ext == "" && media.Format != "" {
			ext = "." + strings.ToLower(strings.TrimPrefix(media.Format, "."))
		}
		if ext == "" {
			ext = ".bin"
		}
		directory := "media"
		if strings.EqualFold(media.Type, "Image") {
			directory = "images"
		}
		file := filepath.Join(directory, fmt.Sprintf("media-%d%s", id, ext))
		if err := e.writeAsset(file, data); err != nil {
			return fmt.Errorf("写出多媒体 %d 失败: %w", id, err)
		}
		e.media[id] = e.assetPath(file)
		name := media.MediaFile.Base()
		if strings.EqualFold(media.Type, "Image") {
			result.Resources.Images = append(result.Resources.Images, manifest.Image{ID: uint64(id), Format: media.Format, Name: name, File: e.assetPath(file)})
		} else {
			result.Resources.Media = append(result.Resources.Media, manifest.Media{ID: uint64(id), Type: media.Type, Format: media.Format, Name: name, File: e.assetPath(file)})
		}
	}
	return nil
}

func (e *documentExporter) exportPages(result *manifest.Manifest) error {
	for index, page := range e.document.Pages {
		if page == nil {
			continue
		}
		var content *models.PageContent
		err := page.WithPageContent(func(value *models.PageContent) error {
			content = value
			return nil
		})
		if err != nil {
			return fmt.Errorf("读取第 %d 页失败: %w", index+1, err)
		}
		converted, err := e.convertPageContent(content)
		if err != nil {
			return fmt.Errorf("转换第 %d 页失败: %w", index+1, err)
		}
		if err := e.exportPageResources(index, page, &converted); err != nil {
			return fmt.Errorf("导出第 %d 页资源失败: %w", index+1, err)
		}
		result.Pages = append(result.Pages, converted)
	}
	return nil
}

func (e *documentExporter) exportPageResources(index int, page *parser.Page, target *manifest.Page) error {
	if page == nil || target == nil {
		return nil
	}
	locations := page.PageResourceLocations()
	for resourceIndex, location := range locations {
		data := e.pageResourceData(location)
		if len(data) == 0 {
			var err error
			data, err = e.document.FileCache.Read(location.String())
			if err != nil {
				return fmt.Errorf("读取页面资源 %q 失败: %w", location, err)
			}
		}
		doc := etree.NewDocument()
		if err := doc.ReadFromBytes(data); err != nil {
			return fmt.Errorf("页面资源 %q XML 无效: %w", location, err)
		}
		root := doc.Root()
		if root == nil || root.Tag != "Res" {
			return fmt.Errorf("页面资源 %q 根元素不是 Res", location)
		}
		resourceDir := filepath.ToSlash(filepath.Join("pages", fmt.Sprintf("page-%03d", index)))
		resourceName := filepath.Join(resourceDir, fmt.Sprintf("resource-%03d.xml", resourceIndex))
		files, err := e.exportResourceFiles(root, location, resourceDir)
		if err != nil {
			return fmt.Errorf("页面资源 %q 外部文件: %w", location, err)
		}
		serialized, err := doc.WriteToBytes()
		if err != nil {
			return fmt.Errorf("序列化页面资源 %q 失败: %w", location, err)
		}
		if err := e.writeAsset(resourceName, serialized); err != nil {
			return fmt.Errorf("写出页面资源 %q 失败: %w", location, err)
		}
		resource := manifest.PageResource{File: e.assetPath(resourceName)}
		for _, file := range files {
			resource.Files = append(resource.Files, manifest.ResourceFile{Path: file.path, File: e.assetPath(file.asset)})
		}
		target.Resources = append(target.Resources, resource)
	}
	return nil
}

func (e *documentExporter) pageResourceData(location models.StLoc) []byte {
	for _, source := range e.pageResources {
		if source.path == location {
			return append([]byte(nil), source.data...)
		}
	}
	return nil
}

func (e *documentExporter) exportPublicResources(result *manifest.Manifest) error {
	resources := e.document.PublicResourceList()
	for index, resource := range resources {
		if resource == nil || index >= len(e.document.CommonData.PublicRes) {
			continue
		}
		location := e.document.CommonData.PublicRes[index].Resolve(e.document.BaseLoc)
		data, err := e.document.FileCache.Read(location.String())
		if err != nil {
			return fmt.Errorf("读取公共资源 %q 失败: %w", location, err)
		}
		doc := etree.NewDocument()
		if err := doc.ReadFromBytes(data); err != nil {
			return fmt.Errorf("公共资源 %q XML 无效: %w", location, err)
		}
		root := doc.Root()
		if root == nil || root.Tag != "Res" {
			return fmt.Errorf("公共资源 %q 根元素不是 Res", location)
		}
		e.normalizeResourceXML(root)
		resourceName := filepath.ToSlash(filepath.Join("public", fmt.Sprintf("resource-%03d.xml", index)))
		resourceDir := filepath.ToSlash(filepath.Join("public", fmt.Sprintf("resource-%03d-files", index)))
		files, err := e.exportResourceFiles(root, location, resourceDir)
		if err != nil {
			return fmt.Errorf("公共资源 %q 外部文件: %w", location, err)
		}
		serialized, err := doc.WriteToBytes()
		if err != nil {
			return fmt.Errorf("序列化公共资源 %q 失败: %w", location, err)
		}
		if err := e.writeAsset(resourceName, serialized); err != nil {
			return fmt.Errorf("写出公共资源 %q 失败: %w", location, err)
		}
		item := manifest.PublicResource{Name: resourceName, File: e.assetPath(resourceName)}
		for _, file := range files {
			item.Files = append(item.Files, manifest.ResourceFile{Path: file.path, File: e.assetPath(file.asset)})
		}
		result.Resources.Public = append(result.Resources.Public, item)
	}
	return nil
}

type exportedResourceFile struct {
	path  string
	asset string
}

func (e *documentExporter) normalizeResourceXML(root *etree.Element) {
	if root == nil {
		return
	}
	var walk func(*etree.Element)
	walk = func(element *etree.Element) {
		if strings.TrimPrefix(element.Tag, "ofd:") == "Font" {
			if attribute := element.SelectAttr("CharSet"); attribute != nil && element.SelectAttr("Charset") == nil {
				attribute.Key = "Charset"
			}
			name := strings.TrimSpace(element.SelectAttrValue("FontName", ""))
			if name != "" {
				if e.fontNames[name] {
					id, _ := strconv.ParseUint(strings.TrimSpace(element.SelectAttrValue("ID", "0")), 10, 64)
					name = fmt.Sprintf("%s-%d", name, id)
					for e.fontNames[name] {
						name += "-1"
					}
					element.SelectAttr("FontName").Value = name
				}
				e.fontNames[name] = true
				if id, err := strconv.ParseUint(strings.TrimSpace(element.SelectAttrValue("ID", "0")), 10, 64); err == nil && id != 0 {
					e.fonts[models.StID(id)] = name
				}
			}
		}
		for _, child := range element.ChildElements() {
			walk(child)
		}
	}
	walk(root)
}

func (e *documentExporter) exportResourceFiles(root *etree.Element, source models.StLoc, resourceDir string) ([]exportedResourceFile, error) {
	base := source.Dir()
	baseLoc := root.SelectAttrValue("BaseLoc", ".")
	if baseLoc == "" {
		baseLoc = "."
	}
	if baseLoc != "." {
		base = base.Join(baseLoc).Clean()
	}
	if attribute := root.SelectAttr("BaseLoc"); attribute != nil {
		attribute.Value = "."
	} else {
		root.CreateAttr("BaseLoc", ".")
	}
	refs := make([]string, 0)
	for _, element := range root.FindElements("ColorSpaces/ColorSpace") {
		if value := strings.TrimSpace(element.SelectAttrValue("Profile", "")); value != "" {
			refs = append(refs, value)
		}
	}
	for _, element := range root.FindElements("Fonts/Font") {
		if value := element.FindElement("FontFile"); value != nil && strings.TrimSpace(value.Text()) != "" {
			refs = append(refs, strings.TrimSpace(value.Text()))
		}
	}
	for _, element := range root.FindElements("MultiMedias/MultiMedia") {
		if value := element.FindElement("MediaFile"); value != nil && strings.TrimSpace(value.Text()) != "" {
			refs = append(refs, strings.TrimSpace(value.Text()))
		}
	}
	seen := make(map[string]bool, len(refs))
	result := make([]exportedResourceFile, 0, len(refs))
	for refIndex, ref := range refs {
		clean := path.Clean(filepath.ToSlash(ref))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(clean, "\\") {
			return nil, fmt.Errorf("引用路径无效: %q", ref)
		}
		if seen[clean] {
			continue
		}
		seen[clean] = true
		filePath := base.Join(clean).Clean()
		data, err := e.document.FileCache.Read(filePath.String())
		if err != nil {
			return nil, fmt.Errorf("读取 %q 失败: %w", filePath, err)
		}
		asset := filepath.Join(resourceDir, "files", fmt.Sprintf("%03d-%s", refIndex, filepath.Base(clean)))
		if err := e.writeAsset(asset, data); err != nil {
			return nil, err
		}
		result = append(result, exportedResourceFile{path: clean, asset: asset})
	}
	return result, nil
}

func (e *documentExporter) convertPageContent(content *models.PageContent) (manifest.Page, error) {
	page := manifest.Page{}
	if content == nil {
		return page, errors.New("页面内容为空")
	}
	page.Area = exportPageArea(content.Area)
	for _, template := range content.Template {
		page.Templates = append(page.Templates, manifest.TemplateRef{ID: uint64(template.TemplateID), ZOrder: template.ZOrder})
	}
	if content.Actions != nil {
		actions, err := e.exportActionList(content.Actions.Action)
		if err != nil {
			return page, fmt.Errorf("转换页面动作失败: %w", err)
		}
		page.Actions = actions
	}
	if content.Content == nil {
		return page, nil
	}
	if len(content.Content.Layer) > 0 {
		for _, layer := range content.Content.Layer {
			if layer == nil {
				continue
			}
			items, err := e.convertItems(layer.Items)
			if err != nil {
				return page, err
			}
			page.Layers = append(page.Layers, manifest.Layer{Type: layer.Type, DrawParam: e.drawParamName(layer.DrawParam), Items: items})
		}
		return page, nil
	}
	return page, nil
}

func (e *documentExporter) exportActionList(values []models.CtAction) ([]manifest.Action, error) {
	result := make([]manifest.Action, 0, len(values))
	for index, value := range values {
		action, err := e.exportAction(value)
		if err != nil {
			return nil, fmt.Errorf("[%d]: %w", index, err)
		}
		result = append(result, action)
	}
	return result, nil
}

func (e *documentExporter) exportAction(value models.CtAction) (manifest.Action, error) {
	result := manifest.Action{Event: string(value.Event)}
	if value.Region != nil {
		region, err := exportActionRegion(value.Region)
		if err != nil {
			return manifest.Action{}, err
		}
		result.Region = region
	}
	if value.URI != nil {
		result.URI = &manifest.URIAction{URI: value.URI.URI, Base: valueOrEmpty(value.URI.Base), Target: valueOrEmpty(value.URI.Target)}
	}
	if value.Goto != nil {
		gotoValue := manifest.GotoAction{}
		if value.Goto.Bookmark != nil {
			gotoValue.Bookmark = value.Goto.Bookmark.Name
		} else if value.Goto.Dest != nil {
			var err error
			gotoValue, err = e.exportDestination(*value.Goto.Dest)
			if err != nil {
				return manifest.Action{}, err
			}
		} else {
			return manifest.Action{}, errors.New("Goto 动作缺少 Dest 或 Bookmark")
		}
		result.Goto = &gotoValue
	}
	if value.GotoA != nil {
		newWindow := value.GotoA.NewWindow
		attachID := value.GotoA.AttachID
		if mapped, ok := e.attachmentIDs[attachID]; ok {
			attachID = mapped
		}
		result.GotoA = &manifest.GotoAAction{AttachID: attachID, NewWindow: &newWindow}
	}
	if value.Sound != nil {
		result.Sound = &manifest.SoundAction{ResourceID: uint64(value.Sound.ResourceID), Volume: value.Sound.Volume, Repeat: value.Sound.Repeat, Synchronous: value.Sound.Synchronous}
	}
	if value.Movie != nil {
		result.Movie = &manifest.MovieAction{ResourceID: uint64(value.Movie.ResourceID), Operator: string(value.Movie.Operator)}
	}
	return result, nil
}

func exportActionRegion(value *models.CtRegion) (*manifest.ActionRegion, error) {
	result := &manifest.ActionRegion{}
	for _, area := range value.Areas {
		converted := manifest.ActionArea{Start: manifest.Point{X: area.Start.X, Y: area.Start.Y}}
		for _, pathValue := range area.Paths {
			command := manifest.RegionCommand{}
			switch pathValue.XMLName.Local {
			case "Move":
				command.Type = "move"
				command.X, command.Y = pointValue(pathValue.Point1)
			case "Line":
				command.Type = "line"
				command.X, command.Y = pointValue(pathValue.Point1)
			case "QuadraticBezier":
				command.Type = "quadratic"
				command.ControlX, command.ControlY = pointValue(pathValue.Point1)
				command.X, command.Y = pointValue(pathValue.Point2)
			case "CubicBezier":
				command.Type = "cubic"
				command.Control1X, command.Control1Y = pointValue(pathValue.Point1)
				command.Control2X, command.Control2Y = pointValue(pathValue.Point2)
				command.X, command.Y = pointValue(pathValue.Point3)
			case "Arc":
				command.Type = "arc"
				if pathValue.SweepDirection != nil {
					command.SweepDirection = *pathValue.SweepDirection
				}
				if pathValue.LargeArc != nil {
					command.LargeArc = *pathValue.LargeArc
				}
				if pathValue.RotationAngle != nil {
					command.RotationAngle = *pathValue.RotationAngle
				}
				command.EllipseWidth, command.EllipseHeight = arrayPoint(pathValue.EllipseSize)
				command.X, command.Y = pointValue(pathValue.EndPoint)
			case "Close":
				command.Type = "close"
			default:
				return nil, fmt.Errorf("不支持的动作区域命令类型 %q", pathValue.XMLName.Local)
			}
			converted.Commands = append(converted.Commands, command)
		}
		result.Areas = append(result.Areas, converted)
	}
	return result, nil
}

func pointValue(value *models.StPos) (float64, float64) {
	if value == nil {
		return 0, 0
	}
	return value.X, value.Y
}

func arrayPoint(value *models.StArray) (float64, float64) {
	if value == nil || len(*value) < 2 {
		return 0, 0
	}
	x, _ := strconv.ParseFloat((*value)[0], 64)
	y, _ := strconv.ParseFloat((*value)[1], 64)
	return x, y
}

func (e *documentExporter) convertItems(items []models.PageItem) ([]manifest.Item, error) {
	result := make([]manifest.Item, 0, len(items))
	for _, item := range items {
		converted, err := e.convertItem(item)
		if err != nil {
			return nil, err
		}
		if converted.Type == "text" && strings.TrimSpace(converted.Value) == "" && len(converted.TextCodes) == 0 {
			continue
		}
		result = append(result, converted)
	}
	return result, nil
}

func (e *documentExporter) convertItem(item models.PageItem) (manifest.Item, error) {
	switch item.Kind {
	case models.PageItemText:
		text := item.Text.CtText
		result, err := e.graphicItem("text", text.CTGraphicUnit)
		if err != nil {
			return manifest.Item{}, err
		}
		result.Value = textValue(text.TextCode)
		result.Font = e.fonts[models.StID(text.Font)]
		result.Size = text.Size
		result.Stroke = text.Stroke
		result.Fill = optionalFill(text.Fill)
		result.HScale = text.HScale
		result.ReadDirection = text.ReadDirection
		result.CharDirection = text.CharDirection
		result.Weight = text.Weight
		result.Italic = text.Italic
		result.FillColor = e.exportColor(text.FillColor)
		result.StrokeColor = e.exportColor(text.StrokeColor)
		for _, code := range text.TextCode {
			if strings.TrimSpace(code.Value) == "" {
				continue
			}
			x, y := code.X, code.Y
			result.TextCodes = append(result.TextCodes, manifest.TextCode{Value: code.Value, X: &x, Y: &y, DeltaX: append([]float64(nil), code.DeltaX...), DeltaY: append([]float64(nil), code.DeltaY...)})
		}
		for _, transform := range text.CGTransform {
			result.CGTransforms = append(result.CGTransforms, manifest.CGTransform{CodePosition: transform.CodePosition, CodeCount: transform.CodeCount, GlyphCount: transform.GlyphCount, Glyphs: append([]int(nil), transform.Glyphs...)})
		}
		return result, nil
	case models.PageItemPath:
		pathValue := item.Path.CtPath
		result, err := e.graphicItem("path", pathValue.CTGraphicUnit)
		if err != nil {
			return manifest.Item{}, err
		}
		result.Data = pathValue.AbbreviatedData.String()
		result.Stroke = pathValue.Stroke != "false"
		if pathValue.Stroke != "" {
			value := result.Stroke
			result.StrokeSet = &value
		}
		fill := pathValue.Fill
		result.Fill = &fill
		result.Rule = pathValue.Rule
		result.FillColor = e.exportColor(pathValue.FillColor)
		result.StrokeColor = e.exportColor(pathValue.StrokeColor)
		return result, nil
	case models.PageItemImage:
		imageValue := item.Image.CtImage
		result, err := e.graphicItem("image", imageValue.CTGraphicUnit)
		if err != nil {
			return manifest.Item{}, err
		}
		result.ResourceID = uint64(imageValue.ResourceID)
		result.Substitution = uint64(imageValue.Substitution)
		result.ImageMask = uint64(imageValue.ImageMask)
		result.Border = e.exportImageBorder(imageValue.Border)
		return result, nil
	case models.PageItemComposite:
		composite := item.Composite.CtComposite
		result, err := e.graphicItem("composite", composite.CTGraphicUnit)
		if err != nil {
			return manifest.Item{}, err
		}
		result.ResourceID = uint64(composite.ResourceID)
		if composite.ResourceID != 0 {
			e.composites[models.StID(composite.ResourceID)] = true
		}
		return result, nil
	case models.PageItemBlock:
		items, err := e.convertItems(item.Block.Items)
		return manifest.Item{Type: "page-block", Items: items}, err
	default:
		return manifest.Item{}, fmt.Errorf("不支持的页面对象类型: %d", item.Kind)
	}
}

func (e *documentExporter) graphicItem(kind string, graphic models.CTGraphicUnit) (manifest.Item, error) {
	var dashPattern []float64
	if graphic.DashPattern != nil {
		dashPattern = append([]float64(nil), (*graphic.DashPattern)...)
	}
	boundary := exportBox(&graphic.Boundary)
	result := manifest.Item{Type: kind, X: boundary.X, Y: boundary.Y, Width: boundary.Width, Height: boundary.Height, Name: graphic.Name, DrawParam: e.drawParamName(graphic.DrawParam), LineWidth: graphic.LineWidth, Cap: graphic.Cap, Join: graphic.Join, MiterLimit: graphic.MiterLimit, DashOffset: graphic.DashOffset, DashPattern: dashPattern, Alpha: graphic.Alpha, CTM: exportCTM(graphic.CTM), Visible: graphic.Visible.Bool()}
	if graphic.Actions != nil {
		actions, err := e.exportActionList(graphic.Actions.Action)
		if err != nil {
			return manifest.Item{}, fmt.Errorf("转换图元动作失败: %w", err)
		}
		result.Actions = actions
	}
	return result, nil
}

func (e *documentExporter) drawParamName(id models.StRefID) string {
	if id == 0 {
		return ""
	}
	if e.document.GetDrawParam(models.StID(id)) == nil {
		// 部分生产者会在图层中留下过期的 DrawParam 引用。
		// creator manifest 无法安全地表示未解析的引用。
		return ""
	}
	if name, ok := e.pageDrawParams[models.StID(id)]; ok {
		return name
	}
	if name, ok := e.drawParams[models.StID(id)]; ok {
		return name
	}
	name := "dp-" + strconv.FormatUint(uint64(id), 10)
	e.drawParams[models.StID(id)] = name
	return name
}

func exportPageArea(area *models.CtPageArea) *manifest.PageArea {
	if area == nil {
		return nil
	}
	return &manifest.PageArea{PhysicalBox: exportBox(&area.PhysicalBox), ApplicationBox: exportBox(area.ApplicationBox), ContentBox: exportBox(area.ContentBox), BleedBox: exportBox(area.BleedBox)}
}

func exportBox(box *models.StBox) *manifest.Box {
	if box == nil {
		return nil
	}
	width, height := box.Width, box.Height
	// creator 要求对象边界为正数，而部分阅读器会生成零尺寸文本框。
	if width <= 0 {
		width = 0.001
	}
	if height <= 0 {
		height = 0.001
	}
	return &manifest.Box{X: box.X, Y: box.Y, Width: width, Height: height}
}

func exportCTM(ctm *models.CTM) []float64 {
	if ctm == nil {
		return nil
	}
	return append([]float64(nil), ctm[:]...)
}

func (e *documentExporter) exportColor(value *models.CTColor) *manifest.Color {
	if value == nil {
		return nil
	}
	result := &manifest.Color{ColorSpace: uint64(value.ColorSpace), Alpha: value.Alpha}
	if value.Value != nil {
		result.R, result.G, result.B = value.Value.R, value.Value.G, value.Value.B
		if value.Value.A != 255 {
			alpha := value.Value.A
			result.Alpha = &alpha
		}
		if value.ColorSpace != 0 {
			components := 3
			switch e.colorSpaces[models.StID(value.ColorSpace)] {
			case "GRAY":
				components = 1
			case "CMYK":
				components = 4
			}
			result.Components = make([]int, components)
			for index := range result.Components {
				result.Components[index] = int(value.Value.R)
			}
		}
	}
	if value.ColorSpace != 0 && value.Value == nil && value.Index == 0 {
		// OFD 允许省略 Value，此时表示所引用颜色空间的零分量。
		// creator 要求显式表达这一语义。
		components := 3
		switch e.colorSpaces[models.StID(value.ColorSpace)] {
		case "GRAY":
			components = 1
		case "CMYK":
			components = 4
		}
		result.Components = make([]int, components)
	}
	if value.Index != 0 {
		index := value.Index
		result.Index = &index
	}
	return result
}

func (e *documentExporter) exportImageBorder(border *models.Border) *manifest.ImageBorder {
	if border == nil {
		return nil
	}
	return &manifest.ImageBorder{LineWidth: border.LineWidth, HorizontalRadius: border.HorizonalCornerRadius, VerticalRadius: border.VerticalCornerRadius, DashOffset: border.DashOffset, DashPattern: exportStringFloatArray(border.DashPattern), Color: e.exportColor(border.BorderColor)}
}

func exportFloatArray(value *models.StArrayF) []float64 {
	if value == nil {
		return nil
	}
	return append([]float64(nil), (*value)...)
}

func exportStringFloatArray(value models.StArray) []float64 {
	result := make([]float64, 0, len(value))
	for _, item := range value {
		parsed, err := strconv.ParseFloat(item, 64)
		if err == nil {
			result = append(result, parsed)
		}
	}
	return result
}

func detectFontFormat(data []byte, hint string) string {
	format := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(hint)), ".")
	switch format {
	case "ttf", "otf", "ttc":
		return format
	}
	if len(data) < 4 {
		return ""
	}
	switch string(data[:4]) {
	case "\x00\x01\x00\x00", "true":
		return "ttf"
	case "OTTO":
		return "otf"
	case "ttcf":
		return "ttc"
	default:
		return ""
	}
}

func optionalFill(value string) *bool {
	if value == "" {
		return nil
	}
	result := strings.EqualFold(value, "true")
	return &result
}

func textValue(codes []models.TextCode) string {
	var result strings.Builder
	for _, code := range codes {
		result.WriteString(code.Value)
	}
	return result.String()
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func sortedIDs[T any](values map[models.StID]T) []models.StID {
	ids := make([]models.StID, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func (e *documentExporter) assetPath(file string) string {
	if strings.TrimSpace(file) == "" {
		return ""
	}
	if e.assetPrefix == "" {
		return filepath.ToSlash(file)
	}
	return path.Join(e.assetPrefix, filepath.ToSlash(file))
}

func (e *documentExporter) writeAsset(file string, data []byte) error {
	name := filepath.Join(e.assetRoot, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		return err
	}
	return os.WriteFile(name, data, 0644)
}
