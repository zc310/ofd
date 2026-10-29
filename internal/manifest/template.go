package manifest

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	jsonv1 "github.com/goccy/go-json"
	"github.com/goccy/go-json/jsontext"
	"github.com/goccy/go-json/v2"
	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/sfnt"
)

// TemplateOptions 控制固定版式模板的资源加载与页面展开。
type TemplateOptions struct {
	BuildOptions
	// MaxPages 限制最终页数，0 使用 10000，负数无效。
	MaxPages int
}

type templateContext struct {
	root, record               any
	index, page, source, pages int
	detail                     *templateDetailPage
}

type templateRenderer struct {
	store       assetStore
	fonts       map[string]*sfnt.Font
	manifest    *Manifest
	defaultFont string
}

// RenderTemplate 在清单副本中绑定数据并展开页面，再使用普通创建流程构建文档。
// 原清单和数据不被修改，同一模板可以并发用于不同数据。
func (m Manifest) RenderTemplate(data io.Reader, baseDir, assetRoot string, opts TemplateOptions) (creator.Document, error) {
	if data == nil {
		return creator.Document{}, errors.New("模板 JSON 数据读取器不能为空")
	}
	if m.Version != 1 || m.Flow != nil || len(m.Pages) == 0 {
		return creator.Document{}, errors.New("模板需要 version: 1 和固定 pages，不支持 flow")
	}
	if len(m.Document.Signatures) > 0 || len(m.Document.Versions) > 0 {
		return creator.Document{}, errors.New("模板不能携带既有 signatures/versions；请在生成后签署新文档")
	}
	if opts.MaxPages < 0 {
		return creator.Document{}, errors.New("max_pages 不能为负数")
	}
	if opts.MaxPages == 0 {
		opts.MaxPages = 10000
	}
	var root map[string]any
	decoder := jsontext.NewDecoder(data, json.WithUnmarshalers(json.UnmarshalFromFunc(unmarshalTemplateNumber)))
	if err := json.UnmarshalDecode(decoder, &root); err != nil {
		return creator.Document{}, fmt.Errorf("解析模板 JSON 数据: %w", err)
	}
	if root == nil {
		return creator.Document{}, errors.New("模板数据必须是 JSON 对象")
	}
	var extra any
	if err := json.UnmarshalDecode(decoder, &extra); !errors.Is(err, io.EOF) {
		return creator.Document{}, errors.New("模板数据必须只有一个 JSON 对象")
	}
	copy, err := cloneTemplate(m)
	if err != nil {
		return creator.Document{}, err
	}
	m = copy
	// 空数组也先检查字段配置，避免无效模板因数据为空而通过检查。
	err = walkTemplate(reflect.ValueOf(&m), "", func(v reflect.Value, path string) (bool, error) {
		switch value := v.Addr().Interface().(type) {
		case *Page:
			if len(value.Items) > 0 && len(value.Layers) > 0 {
				return false, fmt.Errorf("%s: layers 不能与 items 同时设置", path)
			}
		case *Template:
			if len(value.Items) > 0 && len(value.Layers) > 0 {
				return false, fmt.Errorf("%s: layers 不能与 items 同时设置", path)
			}
		case *PageVariant:
			if len(value.Items) > 0 && len(value.Layers) > 0 {
				return false, fmt.Errorf("%s: layers 不能与 items 同时设置", path)
			}
		}
		if item, ok := v.Addr().Interface().(*Item); ok && item.Bind != nil {
			item.Type = strings.ToLower(strings.TrimSpace(item.Type))
			if err := validateBinding(*item); err != nil {
				return false, fmt.Errorf("%s.bind: %w", path, err)
			}
		}
		return false, nil
	})
	if err != nil {
		return creator.Document{}, err
	}
	if assetRoot == "" {
		assetRoot = baseDir
	} else if !filepath.IsAbs(assetRoot) {
		assetRoot = filepath.Join(baseDir, assetRoot)
	}
	if assetRoot != "" {
		assetRoot, err = filepath.Abs(assetRoot)
		if err != nil {
			return creator.Document{}, err
		}
	}
	r := templateRenderer{store: assetStore{root: assetRoot, loader: opts.LoadAsset}, fonts: map[string]*sfnt.Font{}, manifest: &m}
	original := m.Pages
	targets := make([][]int, len(original))
	var contexts []templateContext
	for source, page := range original {
		if err := validateDetailPagination(page); err != nil {
			return creator.Document{}, fmt.Errorf("pages[%d].paginate: %w", source, err)
		}
		records := []any{root}
		if page.Repeat != "" {
			value, found := templateLookup(root, page.Repeat)
			var ok bool
			records, ok = value.([]any)
			if !found || !ok {
				return creator.Document{}, fmt.Errorf("pages[%d].repeat %q: 需要数组", source, page.Repeat)
			}
		}
		if len(records) > opts.MaxPages-len(contexts) {
			return creator.Document{}, fmt.Errorf("模板展开超过 max_pages=%d", opts.MaxPages)
		}
		for index, record := range records {
			ctx := templateContext{root: root, record: record, index: index, source: source}
			parts, err := r.paginateRecord(page, ctx, opts.MaxPages-len(contexts))
			if err != nil {
				return creator.Document{}, fmt.Errorf("pages[%d] 记录 %d: %w", source, index, err)
			}
			for _, part := range parts {
				part.page = len(contexts)
				targets[source] = append(targets[source], part.page)
				contexts = append(contexts, part)
			}
		}
	}
	if len(contexts) == 0 {
		return creator.Document{}, errors.New("模板展开后没有页面；空 repeat 数组不生成页面")
	}
	// 页面资源提升为共享资源，只注册一次，重复页沿用原有资源引用。
	if err := promoteTemplateResources(&m); err != nil {
		return creator.Document{}, err
	}
	original = m.Pages
	annotations := m.Document.Annotations
	nextAnnotationID := uint64(0)
	for _, page := range annotations {
		for _, annotation := range page.Items {
			if annotation.ID > nextAnnotationID {
				nextAnnotationID = annotation.ID
			}
		}
	}
	m.Pages, m.Document.Annotations = nil, nil
	global := templateContext{root: root, record: root, page: -1, source: -1, pages: len(contexts)}
	if err := r.bind(&m, global, targets); err != nil {
		return creator.Document{}, err
	}
	for _, ctx := range contexts {
		ctx.pages = len(contexts)
		page, err := cloneTemplate(original[ctx.source])
		if err != nil {
			return creator.Document{}, err
		}
		page.Repeat = ""
		if ctx.detail != nil {
			applyPageVariant(&page, detailVariant(page, ctx.detail.kind))
		}
		page.Paginate, page.Variants = nil, nil
		if err := r.bind(&page, ctx, targets); err != nil {
			return creator.Document{}, fmt.Errorf("pages[%d] → 输出第 %d 页，记录 %d: %w", ctx.source, ctx.page+1, ctx.index, err)
		}
		if ctx.detail != nil {
			items, err := cloneTemplate(ctx.detail.items())
			if err != nil {
				return creator.Document{}, err
			}
			table := Page{Items: items}
			if err := r.bind(&table, ctx, targets); err != nil {
				return creator.Document{}, fmt.Errorf("pages[%d].paginate 第 %d 页: %w", ctx.source, ctx.page+1, err)
			}
			appendPageItems(&page, table.Items)
		}
		m.Pages = append(m.Pages, page)
		for _, annotation := range annotations {
			if annotation.Page != ctx.source {
				continue
			}
			copy, err := cloneTemplate(annotation)
			if err != nil {
				return creator.Document{}, err
			}
			copy.Page = ctx.page
			if ctx.page != targets[ctx.source][0] {
				for i := range copy.Items {
					if nextAnnotationID >= uint64(^uint32(0)) {
						return creator.Document{}, errors.New("重复批注的 ID 超出 OFD 范围")
					}
					nextAnnotationID++
					copy.Items[i].ID = nextAnnotationID
				}
			}
			if err := r.bind(&copy, ctx, targets); err != nil {
				return creator.Document{}, err
			}
			m.Document.Annotations = append(m.Document.Annotations, copy)
		}
	}
	for _, annotation := range annotations {
		if annotation.Page < 0 || annotation.Page >= len(targets) {
			return creator.Document{}, fmt.Errorf("批注目标页 %d 不存在", annotation.Page)
		}
	}
	return m.BuildWithOptions(baseDir, assetRoot, opts.BuildOptions)
}

// unmarshalTemplateNumber 保留数字原文，避免模板金额和大整数经过 float64 丢失精度。
func unmarshalTemplateNumber(decoder *jsontext.Decoder, value *any) error {
	if decoder.PeekKind() != '0' {
		return errors.ErrUnsupported
	}
	token, err := decoder.ReadToken()
	if err != nil {
		return err
	}
	*value = jsonv1.Number(token.String())
	return nil
}

func cloneTemplate[T any](value T) (T, error) {
	var result T
	data, err := json.Marshal(value)
	if err == nil {
		err = json.Unmarshal(data, &result)
	}
	return result, err
}

// walkTemplate 统一遍历清单中的嵌套块、图层、图案、裁剪和注解，防止遗漏其中的绑定或跳转。
// visit 只可移除 []Item 中的图元，其他切片（尤其 pages）保持原有结构。
func walkTemplate(v reflect.Value, path string, visit func(reflect.Value, string) (bool, error)) error {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return walkTemplate(v.Elem(), path, visit)
	}
	switch v.Kind() {
	case reflect.Struct:
		_, err := visit(v, path)
		if err != nil {
			return err
		}
		for i := 0; i < v.NumField(); i++ {
			name := strings.Split(v.Type().Field(i).Tag.Get("json"), ",")[0]
			if name == "" {
				name = v.Type().Field(i).Name
			}
			child := name
			if path != "" {
				child = path + "." + name
			}
			if err := walkTemplate(v.Field(i), child, visit); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if v.Type() == reflect.TypeFor[[]Item]() {
			out := reflect.MakeSlice(v.Type(), 0, v.Len())
			for i := 0; i < v.Len(); i++ {
				child := fmt.Sprintf("%s[%d]", path, i)
				drop, err := visit(v.Index(i), child)
				if err != nil {
					return err
				}
				if drop {
					continue
				}
				// 当前图元已访问，递归仅处理其字段。
				for j := 0; j < v.Index(i).NumField(); j++ {
					name := strings.Split(v.Index(i).Type().Field(j).Tag.Get("json"), ",")[0]
					if err := walkTemplate(v.Index(i).Field(j), child+"."+name, visit); err != nil {
						return err
					}
				}
				out = reflect.Append(out, v.Index(i))
			}
			v.Set(out)
			return nil
		}
		for i := 0; i < v.Len(); i++ {
			if err := walkTemplate(v.Index(i), fmt.Sprintf("%s[%d]", path, i), visit); err != nil {
				return err
			}
		}
	}
	return nil
}

func templateLookup(value any, path string) (any, bool) {
	path = strings.TrimPrefix(path, "$.")
	if path == "" {
		return nil, false
	}
	for _, part := range strings.Split(path, ".") {
		object, ok := value.(map[string]any)
		if !ok || part == "" {
			return nil, false
		}
		value, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

func validateBinding(item Item) error {
	b := item.Bind
	if strings.TrimSpace(b.Path) == "" {
		return errors.New("path 不能为空")
	}
	if strings.HasPrefix(b.Path, "@") {
		switch b.Path {
		case "@index", "@number", "@page", "@pages", "@detail_page", "@detail_pages":
		default:
			return fmt.Errorf("未知的内置字段 %q", b.Path)
		}
	} else {
		for _, part := range strings.Split(strings.TrimPrefix(b.Path, "$."), ".") {
			if part == "" {
				return errors.New("path 必须为非空的点分隔对象键")
			}
		}
	}
	if item.Type != "text" && item.Type != "image" {
		return errors.New("仅 text/image 支持 bind")
	}
	if b.Aggregate != "" {
		if item.Type != "text" || (b.Aggregate != "page_sum" && b.Aggregate != "record_sum") {
			return errors.New("aggregate 仅支持文字 page_sum/record_sum")
		}
		if strings.HasPrefix(b.Path, "@") || strings.HasPrefix(b.Path, "$.") {
			return errors.New("求和 path 必须相对于明细行")
		}
	} else if b.DecimalPlaces != nil {
		return errors.New("decimal_places 仅用于 aggregate")
	}
	if b.DecimalPlaces != nil && (*b.DecimalPlaces < 0 || *b.DecimalPlaces > 18) {
		return errors.New("decimal_places 必须在 0—18 之间")
	}
	if item.Width <= 0 || item.Height <= 0 || math.IsNaN(item.Width+item.Height) || math.IsInf(item.Width+item.Height, 0) {
		return errors.New("绑定区域 width/height 必须是有限正数")
	}
	if item.Type == "image" {
		if b.Overflow != "" || b.Align != "" || b.MinSize != 0 || b.MaxLines != 0 || b.LineHeight != 0 {
			return errors.New("图片绑定不能配置文字排版参数")
		}
		return nil
	}
	if item.ReadDirection != 0 || item.CharDirection != 0 {
		return errors.New("文字绑定目前仅支持水平方向")
	}
	if item.HScale < 0 || math.IsNaN(item.HScale) || math.IsInf(item.HScale, 0) {
		return errors.New("h_scale 必须为有限正数或默认值 0")
	}
	if b.Align != "" && b.Align != "left" && b.Align != "center" && b.Align != "right" {
		return errors.New("align 必须为 left/center/right")
	}
	if b.Overflow != "" && b.Overflow != "error" && b.Overflow != "wrap" && b.Overflow != "shrink" && b.Overflow != "ellipsis" {
		return errors.New("overflow 必须为 error/wrap/shrink/ellipsis")
	}
	if b.MaxLines < 0 || b.LineHeight < 0 || math.IsNaN(b.LineHeight) || math.IsInf(b.LineHeight, 0) {
		return errors.New("max_lines/line_height 无效")
	}
	if b.Overflow != "wrap" && (b.MaxLines != 0 || b.LineHeight != 0) {
		return errors.New("max_lines/line_height 仅用于 wrap")
	}
	size := item.Size
	if size == 0 {
		size = 4.2
	}
	if size <= 0 || math.IsNaN(size) || math.IsInf(size, 0) {
		return errors.New("size 必须是有限正数")
	}
	if b.Overflow == "shrink" {
		if b.MinSize <= 0 || b.MinSize > size || math.IsNaN(b.MinSize) {
			return errors.New("shrink 需要 0 < min_size <= size（单位毫米）")
		}
	} else if b.MinSize != 0 {
		return errors.New("min_size 仅用于 shrink")
	}
	return nil
}

func (r *templateRenderer) bind(value any, ctx templateContext, targets [][]int) error {
	return walkTemplate(reflect.ValueOf(value), "", func(v reflect.Value, location string) (bool, error) {
		switch value := v.Addr().Interface().(type) {
		case *GotoAction:
			if value.Bookmark != "" {
				return false, nil
			}
			old := value.Page
			if old < 0 || old >= len(targets) || len(targets[old]) == 0 {
				return false, fmt.Errorf("%s: 跳转目标原始页 %d 不存在或已被空数组移除", location, old)
			}
			value.Page = targets[old][0]
			if old == ctx.source {
				value.Page = ctx.page
			}
		case *Item:
			if value.Bind == nil {
				return false, nil
			}
			path := value.Bind.Path
			drop, _, err := r.bindItem(value, ctx)
			if err != nil {
				return false, fmt.Errorf("%s.bind %q: %w", location, path, err)
			}
			return drop, nil
		}
		return false, nil
	})
}

func (r *templateRenderer) bindItem(item *Item, ctx templateContext) (bool, float64, error) {
	b := item.Bind
	data, found, err := resolveTemplateValue(*b, ctx)
	if err != nil {
		return false, 0, err
	}
	if !found || data == nil {
		if b.Default != nil {
			data = *b.Default
		} else if b.Required != nil && !*b.Required {
			return true, 0, nil
		} else {
			return false, 0, errors.New("缺少必填字段")
		}
	}
	if text, ok := data.(string); ok && text == "" {
		return true, 0, nil
	}
	height := 0.0
	if item.Type == "image" {
		err = r.bindImage(item, data)
	} else {
		var text string
		switch value := data.(type) {
		case string:
			text = value
		case jsonv1.Number:
			text = string(value)
		case bool:
			text = strconv.FormatBool(value)
		default:
			err = errors.New("文字字段必须是字符串、数字或布尔值")
		}
		if err == nil {
			err = r.layoutText(item, text, &height)
		}
	}
	if err != nil {
		return false, 0, err
	}
	item.Bind = nil
	return item.Type == "text" && len(item.TextCodes) == 0, height, nil
}

func resolveTemplateValue(b Binding, ctx templateContext) (any, bool, error) {
	if b.Aggregate != "" {
		value, err := aggregateTemplateRows(b, ctx)
		return value, true, err
	}
	switch b.Path {
	case "@index":
		return jsonv1.Number(strconv.Itoa(ctx.index)), true, nil
	case "@number":
		return jsonv1.Number(strconv.Itoa(ctx.index + 1)), true, nil
	case "@page":
		if ctx.page < 0 {
			return nil, false, errors.New("@page 只能用于页面或页面注解")
		}
		return jsonv1.Number(strconv.Itoa(ctx.page + 1)), true, nil
	case "@pages":
		return jsonv1.Number(strconv.Itoa(ctx.pages)), true, nil
	case "@detail_page", "@detail_pages":
		if ctx.detail == nil {
			return nil, false, fmt.Errorf("%s 只能用于 paginate 页面或页面注解", b.Path)
		}
		value := ctx.detail.number
		if b.Path == "@detail_pages" {
			value = ctx.detail.pages
		}
		return jsonv1.Number(strconv.Itoa(value)), true, nil
	}
	var value any
	var found bool
	if strings.HasPrefix(b.Path, "$.") {
		value, found = templateLookup(ctx.root, b.Path)
	} else {
		value, found = templateLookup(ctx.record, b.Path)
	}
	return value, found, nil
}

func (r *templateRenderer) bindImage(item *Item, value any) error {
	var image struct {
		File       string `json:"file"`
		DataBase64 string `json:"data_base64"`
		Format     string `json:"format"`
	}
	if file, ok := value.(string); ok {
		image.File = file
	} else {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &image, json.RejectUnknownMembers(true)); err != nil {
			return fmt.Errorf("图片字段需要文件路径或 {file/data_base64, format}: %w", err)
		}
	}
	if image.File == "" && image.DataBase64 == "" {
		return errors.New("图片 file/data_base64 不能为空")
	}
	data, err := r.store.loadData(image.File, image.DataBase64)
	if err != nil {
		return err
	}
	item.ResourceID, item.Substitution, item.ImageMask = 0, 0, 0
	item.Data, item.DataBase64, item.Format = "", base64.StdEncoding.EncodeToString(data), image.Format
	return nil
}

func promoteTemplateResources(m *Manifest) error {
	used := map[string]bool{}
	mediaNames := map[string]bool{}
	for _, image := range m.Resources.Images {
		mediaNames[image.Name] = true
	}
	for _, media := range m.Resources.Media {
		mediaNames[media.Name] = true
	}
	for _, resource := range m.Resources.Public {
		used[resource.Name] = true
	}
	for p := range m.Pages {
		for j, resource := range m.Pages[p].Resources {
			if len(resource.Images) > 0 {
				if resource.File != "" || resource.DataBase64 != "" || len(resource.Files) > 0 {
					return fmt.Errorf("pages[%d].resources[%d]: images 与原始资源互斥", p, j)
				}
				for _, image := range resource.Images {
					if image.Name != "" {
						if err := validateLeafName(image.Name); err != nil {
							return fmt.Errorf("pages[%d].resources[%d].images: %w", p, j, err)
						}
					}
					name := fmt.Sprintf("TemplatePage_%d_%d_%d", p, j, image.ID)
					if image.Name != "" {
						name += "_" + image.Name
					}
					for mediaNames[name] {
						name = "_" + name
					}
					mediaNames[name] = true
					m.Resources.Images = append(m.Resources.Images, Image{ID: image.ID, Name: name, Format: image.Format, File: image.File, DataBase64: image.DataBase64})
				}
			} else {
				name := fmt.Sprintf("TemplateResources/Page_%d_%d/Res.xml", p, j)
				for used[name] {
					name = "_" + name
				}
				used[name] = true
				m.Resources.Public = append(m.Resources.Public, PublicResource{Name: name, File: resource.File, DataBase64: resource.DataBase64, Files: resource.Files})
			}
		}
		m.Pages[p].Resources = nil
	}
	return nil
}
