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
	"strconv"
	"strings"

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

// BundleIndex 描述 WriteBundle 生成的 manifest 集合。
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

// WriteManifest 将 OFD 写入 manifest，并将字体和多媒体资源写入 AssetRoot。
// 导出的 manifest 可以作为 ofd-creator 的输入，但不承诺原始 OFD 的字节级还原。
func WriteManifest(input any, output io.Writer, options Options) error {
	return export(input, output, options, nil)
}

// WriteDocumentManifest 将指定索引的文档体写入 manifest。
// index 从 0 开始；该 API 用于 OFD 包含多个文档体时选择其中一个文档。
func WriteDocumentManifest(input any, index int, output io.Writer, options Options) error {
	if index < 0 {
		return fmt.Errorf("文档体索引不能为负数: %d", index)
	}
	return export(input, output, options, &index)
}

// WriteBundle 将 OFD 的全部文档体写入一个目录包。
// 每个文档体拥有独立的 manifest 和资源目录，根目录下同时写入索引文件。
func WriteBundle(input any, outputDir string, options Options) error {
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
	ofd                     *parser.OFD
	document                *parser.Document
	documentIndex           int
	assetRoot               string
	assetPrefix             string
	fonts                   map[models.StID]string
	fontNames               map[string]bool
	media                   map[models.StID]string
	drawParams              map[models.StID]string
	composites              map[models.StID]bool
	pageIndexes             map[models.StID]int
	attachmentIDs           map[string]string
	colorSpaces             map[models.StID]string
	pageDrawParams          map[models.StID]string
	pageFonts               map[models.StID]bool
	pageComposites          map[models.StID]bool
	pageMedia               map[models.StID]bool
	pageColorSpaces         map[models.StID]bool
	publicDrawParams        map[models.StID]bool
	publicFonts             map[models.StID]bool
	publicComposites        map[models.StID]bool
	publicMedia             map[models.StID]bool
	publicColorSpaces       map[models.StID]bool
	pageResources           []resourceSource
	skipInvalidDestinations bool
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
	var err error
	if err = os.MkdirAll(e.assetRoot, 0755); err != nil {
		return manifest.Manifest{}, fmt.Errorf("创建资源目录失败: %w", err)
	}
	result := manifest.Manifest{Version: 1}
	if err = e.preparePageResourceMetadata(); err != nil {
		return manifest.Manifest{}, err
	}
	documentInfo, err := e.documentInfo()
	if err != nil {
		return manifest.Manifest{}, err
	}
	result.Document = documentInfo
	if err = e.exportPublicResources(&result); err != nil {
		return manifest.Manifest{}, err
	}
	if err = e.exportFonts(&result); err != nil {
		return manifest.Manifest{}, err
	}
	if err = e.exportMedia(&result); err != nil {
		return manifest.Manifest{}, err
	}
	if err = e.exportColorSpaces(&result); err != nil {
		return manifest.Manifest{}, err
	}
	if err = e.exportPages(&result); err != nil {
		return manifest.Manifest{}, err
	}
	if err = e.exportTemplates(&result); err != nil {
		return manifest.Manifest{}, err
	}
	customTags, err := e.exportCustomTags()
	if err != nil {
		return manifest.Manifest{}, err
	}
	result.Resources.CustomTags = customTags
	if err = e.exportComposites(&result); err != nil {
		return manifest.Manifest{}, err
	}
	if err = e.exportDrawParams(&result); err != nil {
		return manifest.Manifest{}, err
	}
	annotations, err := e.exportAnnotationPages()
	if err != nil {
		return manifest.Manifest{}, err
	}
	result.Document.Annotations = annotations
	if len(result.Pages) == 0 {
		return manifest.Manifest{}, errors.New("文档没有页面")
	}
	return result, nil
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
