package export

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/internal/models"
)

// errSkipAction 表示在批注外观转换中跳过目标页面缺失的无效动作。
var errSkipAction = errors.New("跳转引用目标页面不存在")

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
	versions, err := e.exportVersions()
	if err != nil {
		return manifest.Document{}, err
	}
	info.Versions = versions
	return info, nil
}

func (e *documentExporter) exportAnnotationPages() ([]manifest.AnnotationPage, error) {
	var result []manifest.AnnotationPage
	for pageIndex, page := range e.document.Pages {
		if page == nil {
			continue
		}
		annot := e.document.GetAnnotation(page.ID)
		if annot == nil || len(annot.Annots) == 0 {
			continue
		}
		items := make([]manifest.Annotation, 0, len(annot.Annots))
		for annotIndex, value := range annot.Annots {
			converted, err := e.exportAnnotation(value)
			if err != nil {
				return nil, fmt.Errorf("页面 %d 注解 %d: %w", pageIndex, annotIndex, err)
			}
			if converted != nil {
				items = append(items, *converted)
			}
		}
		if len(items) == 0 {
			continue
		}
		result = append(result, manifest.AnnotationPage{Page: pageIndex, Items: items})
	}
	return result, nil
}

func (e *documentExporter) exportAnnotation(value *models.Annot) (*manifest.Annotation, error) {
	if value == nil {
		return nil, nil
	}
	if strings.TrimSpace(value.Creator) == "" {
		slog.Warn("跳过无创建者的注解", "annotation_id", value.ID, "type", value.Type)
		return nil, nil
	}
	id, err := strconv.ParseUint(value.ID, 10, 64)
	if err != nil {
		slog.Warn("跳过 ID 非数字的注解", "annotation_id", value.ID, "type", value.Type)
		return nil, nil
	}
	result := &manifest.Annotation{
		ID:          id,
		Type:        string(value.Type),
		Creator:     value.Creator,
		Subtype:     value.Subtype,
		NoZoom:      value.NoZoom,
		NoRotate:    value.NoRotate,
		Visible:     value.Visible.Bool(),
		Print:       value.Print.Bool(),
		Remark:      valueOrEmpty(value.Remark),
		LastModDate: formatDateTime(value.LastModDate),
		ReadOnly:    value.ReadOnly.Bool(),
	}
	if value.Parameters != nil {
		for _, parameter := range value.Parameters.Parameters {
			result.Parameters = append(result.Parameters, manifest.AnnotationParameter{Name: parameter.Name, Value: parameter.Value})
		}
	}
	if value.Appearance != nil {
		if value.Appearance.Boundary != nil {
			result.Boundary = exportBox(value.Appearance.Boundary)
		}
		previous := e.skipInvalidDestinations
		e.skipInvalidDestinations = true
		items, err := e.convertItems(value.Appearance.Items)
		e.skipInvalidDestinations = previous
		if err != nil {
			return nil, fmt.Errorf("转换批注外观对象失败: %w", err)
		}
		result.Items = items
	}
	return result, nil
}

func (e *documentExporter) exportVersions() ([]manifest.Version, error) {
	if e.documentIndex < 0 || e.documentIndex >= len(e.ofd.DocBodies) {
		return nil, nil
	}
	body := e.ofd.DocBodies[e.documentIndex]
	if body.Versions == nil {
		return nil, nil
	}
	result := make([]manifest.Version, 0, len(body.Versions.VersionList))
	for _, version := range body.Versions.VersionList {
		value, err := e.document.LoadVersion(version.ID)
		if err != nil {
			return nil, fmt.Errorf("读取文档版本 %s 失败: %w", version.ID, err)
		}
		if value == nil {
			continue
		}
		item := manifest.Version{
			ID:      version.ID,
			Index:   version.Index,
			Current: version.Current,
			Version: valueOrEmpty(value.Version),
			Name:    valueOrEmpty(value.Name),
		}
		if value.CreationDate != nil && !value.CreationDate.IsZero() {
			item.CreationDate = value.CreationDate.Format(time.RFC3339)
		}
		for _, file := range value.FileList.Files {
			item.Files = append(item.Files, manifest.VersionFile{ID: file.ID, Path: file.Path.String()})
		}
		location := version.BaseLoc.Resolve("/")
		docRoot := value.DocRoot.Resolve(location.Dir())
		name := docRoot.Base()
		if !docRoot.IsEmpty() && name != "" && name != "." && name != ".." {
			data, err := e.document.FileCache.Read(docRoot.String())
			if err != nil {
				return nil, fmt.Errorf("读取文档版本 %s 文档根失败: %w", version.ID, err)
			}
			asset := filepath.Join("document", fmt.Sprintf("version-%s-%s", version.ID, name))
			if err := e.writeAsset(asset, data); err != nil {
				return nil, fmt.Errorf("写出文档版本 %s 文档根失败: %w", version.ID, err)
			}
			item.DocRoot = e.assetPath(asset)
			item.DocRootName = name
		}
		result = append(result, item)
	}
	return result, nil
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

func formatDateTime(value models.DateTime) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}
