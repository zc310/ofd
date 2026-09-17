package export

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/beevik/etree"
	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

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
		fillColor, err := e.exportColor(param.FillColor)
		if err != nil {
			return fmt.Errorf("绘制参数 %s.fill_color: %w", e.drawParamName(models.StRefID(id)), err)
		}
		strokeColor, err := e.exportColor(param.StrokeColor)
		if err != nil {
			return fmt.Errorf("绘制参数 %s.stroke_color: %w", e.drawParamName(models.StRefID(id)), err)
		}
		value := manifest.DrawParam{
			Name:        e.drawParamName(models.StRefID(id)),
			LineWidth:   param.LineWidth,
			Join:        param.Join,
			Cap:         param.Cap,
			DashOffset:  param.DashOffset,
			MiterLimit:  param.MiterLimit,
			DashPattern: exportFloatArray(param.DashPattern),
			FillColor:   fillColor,
			StrokeColor: strokeColor,
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
