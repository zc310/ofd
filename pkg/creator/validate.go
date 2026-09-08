package creator

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/beevik/etree"
	"github.com/knroy/go-xml/xdm"
	"github.com/knroy/go-xml/xsd"
	"github.com/zc310/ofd/internal/schema"
)

func validateOFDSchema(data []byte, rootName string) error {
	tree, err := xdm.Parse(bytes.NewReader(data), xdm.ParseOptions{MaxBytes: -1, MaxNodes: -1, MaxDepth: -1})
	if err != nil {
		return err
	}
	if tree.Root == nil || len(tree.Root.ChildElements()) != 1 {
		return errors.New("XML 文档必须且只能包含一个根元素")
	}
	root := tree.Root.ChildElements()[0]
	if root.Name.Local != rootName || root.Name.URI != ofNamespace {
		return fmt.Errorf("根元素必须是 OFD 命名空间中的 %s", rootName)
	}
	set, err := schema.Default()
	if err != nil {
		return err
	}
	compiled, ok := set.Schema(rootName)
	if !ok || compiled == nil {
		return fmt.Errorf("没有为根元素 %s 注册 XSD", rootName)
	}
	if err := compiled.Validate(root, xsd.ValidateOptions{MaxErrors: 20}); err != nil {
		return err
	}
	return nil
}

func validSize(width, height float64) bool {
	return width > 0 && height > 0 && finite(width) && finite(height)
}

func validateBox(x, y, width, height float64) error {
	if !finite(x) || !finite(y) || !validSize(width, height) {
		return fmt.Errorf("边界无效: %.6g %.6g %.6g %.6g", x, y, width, height)
	}
	return nil
}

func validateXMLDate(value time.Time, field string) error {
	if value.IsZero() {
		return nil
	}
	if year := value.Year(); year < 1 || year > 9999 {
		return fmt.Errorf("%s 年份超出 XML Schema 范围: %d", field, year)
	}
	return nil
}

func validateXMLDateTime(value time.Time, field string) error {
	return validateXMLDate(value, field)
}

func defaultAnnotationDate(document Document) time.Time {
	if !document.ModDate.IsZero() {
		return document.ModDate
	}
	if !document.CreationDate.IsZero() {
		return document.CreationDate
	}
	return time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
}

func validatePage(value Page) error {
	for index, template := range value.Templates {
		if template.ZOrder != "" && template.ZOrder != "Background" && template.ZOrder != "Foreground" {
			return fmt.Errorf("模板引用 %d 的叠放顺序无效: %q", index+1, template.ZOrder)
		}
		if template.ID == 0 {
			return fmt.Errorf("模板引用 %d 的 ID 不能为空", index+1)
		}
	}
	if len(value.Layers) > 0 {
		if value.LayerType != "" || len(value.Items) > 0 {
			return errors.New("Layers 不能与 LayerType 或 Items 同时设置")
		}
		for index, layer := range value.Layers {
			if err := validateLayerType(layer.Type); err != nil {
				return fmt.Errorf("图层 %d 无效: %w", index+1, err)
			}
		}
	} else if err := validateLayerType(value.LayerType); err != nil {
		return err
	}
	return validatePageArea(value.Area)
}

func validatePageArea(value *PageArea) error {
	if value == nil {
		return nil
	}
	boxes := []*Box{
		value.PhysicalBox,
		value.ApplicationBox,
		value.ContentBox,
		value.BleedBox,
	}
	for _, box := range boxes {
		if box == nil {
			continue
		}
		if err := validateBox(box.X, box.Y, box.Width, box.Height); err != nil {
			return err
		}
	}
	return nil
}

func validateAnnotationPage(value AnnotationPage, pageCount int) error {
	if value.Page < 0 || value.Page >= pageCount {
		return fmt.Errorf("目标页面无效: %d", value.Page)
	}
	if len(value.Items) == 0 {
		return errors.New("注解列表不能为空")
	}
	seen := make(map[uint64]bool, len(value.Items))
	for index, annotation := range value.Items {
		if annotation.ID == 0 {
			return fmt.Errorf("注解 %d ID 不能为空", index+1)
		}
		if annotation.ID > maxOFDID {
			return fmt.Errorf("注解 %d ID 超出 OFD 范围: %d", index+1, annotation.ID)
		}
		if seen[annotation.ID] {
			return fmt.Errorf("注解 ID 重复: %d", annotation.ID)
		}
		seen[annotation.ID] = true
		switch annotation.Type {
		case "Link", "Path", "Highlight", "Stamp", "Watermark":
		default:
			return fmt.Errorf("注解 %d 类型无效: %q", index+1, annotation.Type)
		}
		if strings.TrimSpace(annotation.Creator) == "" {
			return fmt.Errorf("注解 %d Creator 不能为空", index+1)
		}
		if err := validateXMLDate(annotation.LastModDate, fmt.Sprintf("注解 %d LastModDate", index+1)); err != nil {
			return err
		}
		if annotation.Boundary != nil {
			if err := validateBox(annotation.Boundary.X, annotation.Boundary.Y, annotation.Boundary.Width, annotation.Boundary.Height); err != nil {
				return fmt.Errorf("注解 %d 边界无效: %w", index+1, err)
			}
		}
		for parameterIndex, parameter := range annotation.Parameters {
			if strings.TrimSpace(parameter.Name) == "" {
				return fmt.Errorf("注解 %d 参数 %d 名称不能为空", index+1, parameterIndex+1)
			}
		}
	}
	return nil
}

func validateTemplate(value TemplatePage) error {
	if value.ZOrder != "" && value.ZOrder != "Background" && value.ZOrder != "Foreground" {
		return fmt.Errorf("叠放顺序无效: %q", value.ZOrder)
	}
	if value.Area != nil {
		if err := validatePage(Page{Area: value.Area}); err != nil {
			return err
		}
	}
	if len(value.Layers) > 0 && len(value.Items) > 0 {
		return errors.New("模板页 Layers 不能与 Items 同时设置")
	}
	return nil
}

func validateTemplateReferences(pages []Page, templates []TemplatePage, templateIDs []uint64) error {
	known := make(map[uint64]bool, len(templateIDs))
	for _, id := range templateIDs {
		known[id] = true
	}
	for pageIndex, page := range pages {
		for refIndex, ref := range page.Templates {
			if ref.ZOrder != "" && ref.ZOrder != "Background" && ref.ZOrder != "Foreground" {
				return fmt.Errorf("页面 %d 模板引用 %d 的叠放顺序无效: %q", pageIndex+1, refIndex+1, ref.ZOrder)
			}
			if !known[ref.ID] {
				return fmt.Errorf("页面 %d 模板引用 %d 未找到模板页 ID %d", pageIndex+1, refIndex+1, ref.ID)
			}
		}
	}
	_ = templates
	return nil
}

func validateDrawParam(value DrawParam) error {
	if strings.TrimSpace(value.Name) == "" {
		return errors.New("绘制参数名称不能为空")
	}
	if value.Relative != "" && strings.TrimSpace(value.Relative) == "" {
		return errors.New("绘制参数继承名称无效")
	}
	if err := validateGraphicStyle(value.LineWidth, value.Cap, value.Join, value.MiterLimit, value.DashOffset, value.DashPattern, nil); err != nil {
		return err
	}
	if err := validateColor(value.FillColor); err != nil {
		return fmt.Errorf("填充颜色无效: %w", err)
	}
	if err := validateColor(value.StrokeColor); err != nil {
		return fmt.Errorf("描边颜色无效: %w", err)
	}
	return nil
}

func validateActions(values []Action, pageCount int, mediaIDs map[uint64]bool, mediaTypes map[uint64]string, attachmentIDs, bookmarkNames map[string]bool) error {
	for index, value := range values {
		if err := validateActionRegion(value.Region); err != nil {
			return fmt.Errorf("动作 %d 的区域无效: %w", index+1, err)
		}
		event := value.Event
		if event != "" && event != ActionEventDO && event != ActionEventPO && event != ActionEventClick {
			return fmt.Errorf("动作 %d 的事件无效: %q", index+1, event)
		}
		count := 0
		if value.URI != nil {
			count++
		}
		if value.Goto != nil {
			count++
		}
		if value.GotoA != nil {
			count++
		}
		if value.Sound != nil {
			count++
		}
		if value.Movie != nil {
			count++
		}
		if count != 1 {
			return fmt.Errorf("动作 %d 必须且只能设置一种动作类型", index+1)
		}
		if value.URI != nil {
			if strings.TrimSpace(value.URI.URI) == "" {
				return fmt.Errorf("动作 %d 的 URI 不能为空", index+1)
			}
			continue
		}
		if value.Goto != nil && strings.TrimSpace(value.Goto.Bookmark) != "" {
			if value.Goto.Page != 0 || value.Goto.Type != "" || value.Goto.Left != nil || value.Goto.Top != nil || value.Goto.Right != nil || value.Goto.Bottom != nil || value.Goto.Zoom != nil {
				return fmt.Errorf("动作 %d 的 Goto 不能同时设置 Bookmark 和页面目标", index+1)
			}
			if !bookmarkNames[value.Goto.Bookmark] {
				return fmt.Errorf("动作 %d 引用的书签不存在: %s", index+1, value.Goto.Bookmark)
			}
			continue
		}
		if value.GotoA != nil {
			if strings.TrimSpace(value.GotoA.AttachID) == "" || !attachmentIDs[value.GotoA.AttachID] {
				return fmt.Errorf("动作 %d 的附件不存在: %s", index+1, value.GotoA.AttachID)
			}
			continue
		}
		if value.Sound != nil {
			if mediaIDs == nil || !mediaIDs[value.Sound.ResourceID] {
				return fmt.Errorf("动作 %d 的声音资源不存在: %d", index+1, value.Sound.ResourceID)
			}
			if mediaTypes[value.Sound.ResourceID] != "Audio" {
				return fmt.Errorf("动作 %d 引用的资源不是音频: %d", index+1, value.Sound.ResourceID)
			}
			if value.Sound.Volume != nil && (*value.Sound.Volume < 0 || *value.Sound.Volume > 100) {
				return fmt.Errorf("动作 %d 的音量必须在 0 到 100 之间", index+1)
			}
			continue
		}
		if value.Movie != nil {
			if mediaIDs == nil || !mediaIDs[value.Movie.ResourceID] {
				return fmt.Errorf("动作 %d 的影片资源不存在: %d", index+1, value.Movie.ResourceID)
			}
			if mediaTypes[value.Movie.ResourceID] != "Video" {
				return fmt.Errorf("动作 %d 引用的资源不是视频: %d", index+1, value.Movie.ResourceID)
			}
			switch value.Movie.Operator {
			case "", "Play", "Stop", "Pause", "Resume":
			default:
				return fmt.Errorf("动作 %d 的影片操作无效: %q", index+1, value.Movie.Operator)
			}
			continue
		}
		if err := validateGoto(value.Goto, pageCount, fmt.Sprintf("动作 %d", index+1)); err != nil {
			return err
		}
	}
	return nil
}

func validateActionRegion(value *ActionRegion) error {
	if value == nil {
		return nil
	}
	if len(value.Areas) == 0 {
		return errors.New("区域至少需要一个 Area")
	}
	for areaIndex, area := range value.Areas {
		if err := validatePoint(area.Start); err != nil {
			return fmt.Errorf("Area %d 的 Start 无效: %w", areaIndex+1, err)
		}
		if len(area.Commands) == 0 {
			return fmt.Errorf("Area %d 至少需要一个路径命令", areaIndex+1)
		}
		for commandIndex, command := range area.Commands {
			if command == nil {
				return fmt.Errorf("Area %d 的路径命令 %d 为空", areaIndex+1, commandIndex+1)
			}
			var points []Point
			switch value := command.(type) {
			case RegionMove:
				points = []Point{value.Point}
			case RegionLine:
				points = []Point{value.Point}
			case RegionQuadraticBezier:
				points = []Point{value.Control, value.End}
			case RegionCubicBezier:
				points = []Point{value.Control1, value.Control2, value.End}
			case RegionArc:
				if !finite(value.RotationAngle) || value.EllipseSize.X < 0 || value.EllipseSize.Y < 0 {
					return fmt.Errorf("Area %d 的 Arc 参数无效", areaIndex+1)
				}
				points = []Point{value.EllipseSize, value.EndPoint}
			case RegionClose:
			default:
				return fmt.Errorf("Area %d 的路径命令类型 %T 不受支持", areaIndex+1, command)
			}
			for _, point := range points {
				if err := validatePoint(point); err != nil {
					return fmt.Errorf("Area %d 的路径命令 %d 参数无效: %w", areaIndex+1, commandIndex+1, err)
				}
			}
		}
	}
	return nil
}

func validatePoint(value Point) error {
	if !finite(value.X) || !finite(value.Y) {
		return fmt.Errorf("点坐标无效: %.6g %.6g", value.X, value.Y)
	}
	return nil
}

func validateGoto(value *GotoAction, pageCount int, context string) error {
	if value == nil {
		return fmt.Errorf("%s 的 Goto 不能为空", context)
	}
	if strings.TrimSpace(value.Bookmark) != "" {
		if value.Page != 0 || value.Type != "" || value.Left != nil || value.Top != nil || value.Right != nil || value.Bottom != nil || value.Zoom != nil {
			return fmt.Errorf("%s 不能同时设置 Bookmark 和页面目标", context)
		}
		return nil
	}
	if value.Page < 0 || value.Page >= pageCount {
		return fmt.Errorf("%s 的目标页面无效: %d", context, value.Page)
	}
	switch value.Type {
	case "", "XYZ", "Fit", "FitH", "FitV", "FitR":
	default:
		return fmt.Errorf("%s 的目标类型无效: %q", context, value.Type)
	}
	for _, number := range []*float64{value.Left, value.Top, value.Right, value.Bottom, value.Zoom} {
		if number != nil && !finite(*number) {
			return fmt.Errorf("%s 的目标参数无效", context)
		}
	}
	if value.Zoom != nil && *value.Zoom <= 0 {
		return fmt.Errorf("%s 的 Zoom 必须大于 0", context)
	}
	hasLeft, hasTop := value.Left != nil, value.Top != nil
	hasRight, hasBottom := value.Right != nil, value.Bottom != nil
	switch value.Type {
	case "", "Fit":
		if hasLeft || hasTop || hasRight || hasBottom || value.Zoom != nil {
			return fmt.Errorf("%s 的 Fit 目标不能设置位置或缩放参数", context)
		}
	case "XYZ":
		if hasRight || hasBottom {
			return fmt.Errorf("%s 的 XYZ 目标不能设置 Right 或 Bottom", context)
		}
	case "FitH":
		if hasLeft || hasRight || hasBottom || value.Zoom != nil {
			return fmt.Errorf("%s 的 FitH 目标只能设置 Top", context)
		}
	case "FitV":
		if hasTop || hasRight || hasBottom || value.Zoom != nil {
			return fmt.Errorf("%s 的 FitV 目标只能设置 Left", context)
		}
	case "FitR":
		if !hasLeft || !hasTop || !hasRight || !hasBottom || value.Zoom != nil {
			return fmt.Errorf("%s 的 FitR 目标必须设置完整矩形且不能设置 Zoom", context)
		}
		if *value.Right < *value.Left || *value.Bottom < *value.Top {
			return fmt.Errorf("%s 的 FitR 目标矩形边界顺序无效", context)
		}
	}
	return nil
}

func validateOutlines(values []Outline, pageCount int, mediaIDs map[uint64]bool, mediaTypes map[uint64]string, attachmentIDs, bookmarkNames map[string]bool) error {
	for index, value := range values {
		if strings.TrimSpace(value.Title) == "" {
			return fmt.Errorf("大纲 %d 标题不能为空", index+1)
		}
		if value.Count != nil && *value.Count < 0 {
			return fmt.Errorf("大纲 %d Count 不能为负数", index+1)
		}
		if err := validateActions(value.Actions, pageCount, mediaIDs, mediaTypes, attachmentIDs, bookmarkNames); err != nil {
			return fmt.Errorf("大纲 %d 动作无效: %w", index+1, err)
		}
		if err := validateOutlines(value.Children, pageCount, mediaIDs, mediaTypes, attachmentIDs, bookmarkNames); err != nil {
			return fmt.Errorf("大纲 %d 子项无效: %w", index+1, err)
		}
	}
	return nil
}

func validatePermissions(value *Permissions) error {
	if value == nil {
		return nil
	}
	if value.Print != nil && value.Print.Copies != nil && *value.Print.Copies < -1 {
		return errors.New("打印份数不能小于 -1")
	}
	if value.ValidPeriod != nil && !value.ValidPeriod.Start.IsZero() && !value.ValidPeriod.End.IsZero() && value.ValidPeriod.End.Before(value.ValidPeriod.Start) {
		return errors.New("权限有效期结束时间不能早于开始时间")
	}
	if value.ValidPeriod != nil {
		if err := validateXMLDateTime(value.ValidPeriod.Start, "权限有效期开始时间"); err != nil {
			return err
		}
		if err := validateXMLDateTime(value.ValidPeriod.End, "权限有效期结束时间"); err != nil {
			return err
		}
	}
	return nil
}

func validatePreferences(value *ViewPreferences) error {
	if value == nil {
		return nil
	}
	switch value.PageMode {
	case "", PageModeNone, PageModeFullScreen, PageModeUseOutlines, PageModeUseThumbs, PageModeUseCustomTags, PageModeUseLayers, PageModeUseAttatchs, PageModeUseBookmarks:
	default:
		return fmt.Errorf("页面模式无效: %q", value.PageMode)
	}
	switch value.PageLayout {
	case "", PageLayoutOnePage, PageLayoutOneColumn, PageLayoutTwoPageL, PageLayoutTwoColumnL, PageLayoutTwoPageR, PageLayoutTwoColumnR:
	default:
		return fmt.Errorf("页面布局无效: %q", value.PageLayout)
	}
	switch value.TabDisplay {
	case "", TabDisplayDocTitle, TabDisplayFileName:
	default:
		return fmt.Errorf("标签显示方式无效: %q", value.TabDisplay)
	}
	if value.ZoomMode != "" && value.Zoom != nil {
		return errors.New("ZoomMode 与 Zoom 不能同时设置")
	}
	switch value.ZoomMode {
	case "", ZoomModeDefault, ZoomModeFitHeight, ZoomModeFitWidth, ZoomModeFitRect:
	default:
		return fmt.Errorf("缩放模式无效: %q", value.ZoomMode)
	}
	if value.Zoom != nil && (!finite(*value.Zoom) || *value.Zoom <= 0) {
		return fmt.Errorf("缩放比例无效: %.6g", *value.Zoom)
	}
	return nil
}

func validateLayerType(value string) error {
	if value != "" && value != LayerBody && value != LayerBackground && value != LayerForeground && value != LayerCustom {
		return fmt.Errorf("图层类型无效: %q", value)
	}
	return nil
}

func validateText(value Text) error {
	if err := validateBox(value.X, value.Y, value.Width, value.Height); err != nil {
		return err
	}
	if strings.TrimSpace(value.Value) == "" && len(value.TextCodes) == 0 {
		return errors.New("文字内容不能为空")
	}
	if value.Size < 0 || !finite(value.Size) {
		return fmt.Errorf("字号无效: %.6g", value.Size)
	}
	if value.HScale < 0 || value.HScale > 1 || !finite(value.HScale) {
		return fmt.Errorf("水平缩放无效: %.6g", value.HScale)
	}
	if value.Weight != 0 && (value.Weight < 100 || value.Weight > 1000 || value.Weight%100 != 0) {
		return fmt.Errorf("字重无效: %d", value.Weight)
	}
	if value.CTM != nil {
		for index, item := range value.CTM {
			if !finite(item) {
				return fmt.Errorf("CTM 第 %d 个参数无效: %.6g", index+1, item)
			}
		}
	}
	for index, code := range value.TextCodes {
		if strings.TrimSpace(code.Value) == "" {
			return fmt.Errorf("TextCode %d 内容不能为空", index+1)
		}
		if code.X != nil && !finite(*code.X) {
			return fmt.Errorf("TextCode %d 的 X 坐标无效: %.6g", index+1, *code.X)
		}
		if code.Y != nil && !finite(*code.Y) {
			return fmt.Errorf("TextCode %d 的 Y 坐标无效: %.6g", index+1, *code.Y)
		}
		for deltaIndex, delta := range code.DeltaX {
			if !finite(delta) {
				return fmt.Errorf("TextCode %d 的 DeltaX[%d] 无效: %.6g", index+1, deltaIndex, delta)
			}
		}
		for deltaIndex, delta := range code.DeltaY {
			if !finite(delta) {
				return fmt.Errorf("TextCode %d 的 DeltaY[%d] 无效: %.6g", index+1, deltaIndex, delta)
			}
		}
	}
	if err := validateCGTransforms(value.CGTransforms, value.TextCodes, value.Value); err != nil {
		return err
	}
	if err := validateCTMAndClips(value.CTM, value.Clips); err != nil {
		return err
	}
	if err := validateColor(value.FillColor); err != nil {
		return fmt.Errorf("填充颜色无效: %w", err)
	}
	if err := validateColor(value.StrokeColor); err != nil {
		return fmt.Errorf("描边颜色无效: %w", err)
	}
	return nil
}

func validateCGTransforms(values []CGTransform, codes []TextCode, value string) error {
	if len(values) == 0 {
		return nil
	}
	totalCharacters := 0
	if len(codes) == 0 {
		totalCharacters = len([]rune(value))
	} else {
		for _, code := range codes {
			totalCharacters += len([]rune(code.Value))
		}
	}
	for index, transform := range values {
		if transform.CodePosition < 0 || transform.CodePosition >= totalCharacters {
			return fmt.Errorf("CGTransform %d 的 CodePosition 无效: %d", index+1, transform.CodePosition)
		}
		if transform.CodeCount < 0 || transform.GlyphCount < 0 {
			return fmt.Errorf("CGTransform %d 的数量参数不能为负数", index+1)
		}
		if transform.CodeCount > 0 && transform.CodePosition+transform.CodeCount > totalCharacters {
			return fmt.Errorf("CGTransform %d 的 CodeCount 超出文字范围", index+1)
		}
		if transform.GlyphCount > len(transform.Glyphs) {
			return fmt.Errorf("CGTransform %d 的 GlyphCount 超出 Glyphs 数量", index+1)
		}
		for glyphIndex, glyph := range transform.Glyphs {
			if glyph < 0 || glyph > 65535 {
				return fmt.Errorf("CGTransform %d 的 Glyphs[%d] 无效: %d", index+1, glyphIndex, glyph)
			}
		}
	}
	return nil
}

func validatePath(value Path) error {
	if err := validateBox(value.X, value.Y, value.Width, value.Height); err != nil {
		return err
	}
	if strings.TrimSpace(value.Data) == "" {
		return errors.New("路径数据不能为空")
	}
	if value.Rule != "" && value.Rule != "NonZero" && value.Rule != "Even-Odd" {
		return fmt.Errorf("路径填充规则无效: %q", value.Rule)
	}
	if err := validateGraphicStyle(value.LineWidth, value.Cap, value.Join, value.MiterLimit, value.DashOffset, value.DashPattern, value.Alpha); err != nil {
		return err
	}
	if err := validateCTMAndClips(value.CTM, value.Clips); err != nil {
		return err
	}
	if err := validateColor(value.FillColor); err != nil {
		return fmt.Errorf("填充颜色无效: %w", err)
	}
	if err := validateColor(value.StrokeColor); err != nil {
		return fmt.Errorf("描边颜色无效: %w", err)
	}
	return nil
}

func validateImage(value Image) error {
	if err := validateBox(value.X, value.Y, value.Width, value.Height); err != nil {
		return err
	}
	if len(value.Data) == 0 {
		return errors.New("图片数据不能为空")
	}
	if imageFormat(value) == "" {
		return errors.New("无法识别图片格式，请设置 Format")
	}
	if err := validateGraphicStyle(value.LineWidth, value.Cap, value.Join, value.MiterLimit, value.DashOffset, value.DashPattern, value.Alpha); err != nil {
		return err
	}
	if err := validateCTMAndClips(value.CTM, value.Clips); err != nil {
		return err
	}
	if err := validateBorder(value.Border); err != nil {
		return err
	}
	if value.Border != nil {
		if err := validateColor(value.Border.Color); err != nil {
			return fmt.Errorf("边框颜色无效: %w", err)
		}
	}
	return nil
}

func validatePageImage(value PageImage) error {
	if value.ID == 0 {
		return errors.New("页面图片 ID 不能为空")
	}
	if len(value.Data) == 0 {
		return errors.New("页面图片数据不能为空")
	}
	if pageImageFormat(value) == "" {
		return errors.New("无法识别页面图片格式，请设置 Format")
	}
	if err := validateLeafFileName(value.Name); err != nil {
		return fmt.Errorf("页面图片文件名无效: %w", err)
	}
	return nil
}

func pageImageFormat(value PageImage) string {
	format := strings.ToUpper(strings.TrimSpace(value.Format))
	switch format {
	case "JPG":
		return "JPEG"
	case "JPEG", "PNG", "BMP", "TIFF", "GIF", "WEBP":
		return format
	}
	if len(value.Data) >= 8 && bytes.Equal(value.Data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return "PNG"
	}
	if len(value.Data) >= 3 && bytes.Equal(value.Data[:3], []byte{0xff, 0xd8, 0xff}) {
		return "JPEG"
	}
	return ""
}

func validateMedia(value Media) error {
	if value.ID == 0 {
		return errors.New("多媒体资源 ID 不能为空")
	}
	if value.Type != "Image" && value.Type != "Audio" && value.Type != "Video" {
		return fmt.Errorf("多媒体类型必须是 Image、Audio 或 Video: %q", value.Type)
	}
	if len(value.Data) == 0 {
		return errors.New("多媒体数据不能为空")
	}
	if err := validateLeafFileName(value.Name); err != nil {
		return fmt.Errorf("多媒体文件名无效: %w", err)
	}
	if strings.TrimSpace(value.Format) != "" && (strings.ContainsAny(value.Format, "/\\\x00") || mediaFormat(value) == "") {
		return errors.New("多媒体格式无效")
	}
	return nil
}

func validateAttachment(value Attachment) error {
	if strings.TrimSpace(value.ID) == "" {
		return errors.New("附件 ID 不能为空")
	}
	if !validXMLID(value.ID) {
		return fmt.Errorf("附件 ID 不是合法的 XML ID: %q", value.ID)
	}
	if strings.TrimSpace(value.Name) == "" {
		return errors.New("附件名称不能为空")
	}
	if len(value.Data) == 0 {
		return errors.New("附件数据不能为空")
	}
	if err := validateXMLDateTime(value.CreationDate, "附件 CreationDate"); err != nil {
		return err
	}
	if err := validateXMLDateTime(value.ModDate, "附件 ModDate"); err != nil {
		return err
	}
	if err := validateLeafFileName(value.FileName); err != nil {
		return fmt.Errorf("附件文件名无效: %w", err)
	}
	if value.Format != "" && attachmentFormat(value.Format) == "" {
		return errors.New("附件格式不能为空或包含路径")
	}
	return nil
}

func validateCustomTag(value CustomTag) error {
	if strings.TrimSpace(value.NameSpace) == "" {
		return errors.New("自定义标签命名空间不能为空")
	}
	if len(value.Data) == 0 {
		return errors.New("自定义标签数据不能为空")
	}
	if len(value.Schema) == 0 && strings.TrimSpace(value.SchemaName) != "" {
		return errors.New("自定义标签 SchemaName 必须同时提供 Schema")
	}
	if err := validateLeafFileName(value.DataName); err != nil {
		return fmt.Errorf("自定义标签数据文件名无效: %w", err)
	}
	if err := validateLeafFileName(value.SchemaName); err != nil {
		return fmt.Errorf("自定义标签 Schema 文件名无效: %w", err)
	}
	return nil
}

func validateExtension(value Extension) error {
	if strings.TrimSpace(value.AppName) == "" {
		return errors.New("扩展 AppName 不能为空")
	}
	if value.RefID == 0 {
		return errors.New("扩展 RefId 不能为空")
	}
	if err := validateXMLDateTime(value.Date, "扩展 Date"); err != nil {
		return err
	}
	if value.Data == "" && len(value.DataXML) == 0 && len(value.DataFile) == 0 {
		return errors.New("扩展必须包含 Data 或 ExtendData")
	}
	if value.Data != "" && (len(value.DataXML) > 0 || len(value.DataFile) > 0) {
		return errors.New("扩展不能同时设置 Data 和 ExtendData")
	}
	if len(value.DataXML) > 0 && len(value.DataFile) > 0 {
		return errors.New("扩展不能同时设置 DataXML 和 ExtendData")
	}
	if value.Data != "" && strings.TrimSpace(value.DataName) != "" {
		return errors.New("内联扩展 Data 不能设置 DataName")
	}
	if len(value.DataXML) > 0 && strings.TrimSpace(value.DataName) != "" {
		return errors.New("内联扩展 DataXML 不能设置 DataName")
	}
	if len(value.DataXML) > 0 {
		if _, err := rawXMLChildren(value.DataXML); err != nil {
			return fmt.Errorf("扩展 DataXML 无效: %w", err)
		}
	}
	if err := validateLeafFileName(value.DataName); err != nil {
		return fmt.Errorf("扩展数据文件名无效: %w", err)
	}
	for index, property := range value.Properties {
		if strings.TrimSpace(property.Name) == "" {
			return fmt.Errorf("扩展属性 %d 名称不能为空", index+1)
		}
	}
	return nil
}

func validateSignature(value Signature, pageCount int) error {
	if !validXMLID(value.ID) {
		return errors.New("签名 ID 不能为空")
	}
	if value.Type != "" && value.Type != "Seal" && value.Type != "Sign" {
		return fmt.Errorf("签名类型无效: %q", value.Type)
	}
	if strings.TrimSpace(value.ProviderName) == "" {
		return errors.New("签名 ProviderName 不能为空")
	}
	if err := validateXMLDateTime(value.Date, "签名 SignatureDateTime"); err != nil {
		return err
	}
	if len(value.References) == 0 {
		return errors.New("签名引用不能为空")
	}
	method := strings.ToUpper(strings.TrimSpace(value.CheckMethod))
	if method != "" && method != "MD5" && method != "SHA1" && method != "SM3" && method != "1.2.156.10197.1.401" {
		return fmt.Errorf("签名摘要算法无效: %q", value.CheckMethod)
	}
	for index, reference := range value.References {
		if strings.TrimSpace(reference.FileRef) == "" {
			return fmt.Errorf("签名引用 %d 不完整", index+1)
		}
		if err := validatePackagePath(reference.FileRef, docDir+"/Signatures", true); err != nil {
			return fmt.Errorf("签名引用 %d 路径无效: %w", index+1, err)
		}
	}
	for index, stamp := range value.StampAnnots {
		if !validXMLID(stamp.ID) {
			return fmt.Errorf("签名盖章 %d ID 无效", index+1)
		}
		if stamp.Page < 0 || stamp.Page >= pageCount {
			return fmt.Errorf("签名盖章 %d 页面无效: %d", index+1, stamp.Page)
		}
		if err := validateBox(stamp.Boundary.X, stamp.Boundary.Y, stamp.Boundary.Width, stamp.Boundary.Height); err != nil {
			return fmt.Errorf("签名盖章 %d 边界无效: %w", index+1, err)
		}
		if stamp.Clip != nil {
			if err := validateBox(stamp.Clip.X, stamp.Clip.Y, stamp.Clip.Width, stamp.Clip.Height); err != nil {
				return fmt.Errorf("签名盖章 %d 裁剪边界无效: %w", index+1, err)
			}
		}
	}
	seenStampIDs := make(map[string]bool, len(value.StampAnnots))
	for _, stamp := range value.StampAnnots {
		if seenStampIDs[stamp.ID] {
			return fmt.Errorf("签名盖章 ID 重复: %s", stamp.ID)
		}
		seenStampIDs[stamp.ID] = true
	}
	if value.Type == "Sign" && len(value.SignedValue) == 0 {
		return errors.New("Sign 签名必须设置 SignedValue")
	}
	if value.Type == "Seal" && len(value.SealFile) == 0 && len(value.SignedValue) == 0 {
		return errors.New("Seal 签名必须设置 SealFile 或 SignedValue")
	}
	if len(value.SealFile) > 0 && len(value.SignedValue) > 0 {
		return errors.New("签名不能同时设置 SealFile 和 SignedValue")
	}
	if err := validateLeafFileName(value.SealName); err != nil {
		return fmt.Errorf("签名 Seal 文件名无效: %w", err)
	}
	if err := validateLeafFileName(value.SignedValueName); err != nil {
		return fmt.Errorf("签名值文件名无效: %w", err)
	}
	return nil
}

func validateDocumentVersion(value DocumentVersion) error {
	if !validXMLID(value.ID) {
		return fmt.Errorf("文档版本 ID 不是合法的 XML ID: %q", value.ID)
	}
	if value.Index < 0 {
		return errors.New("文档版本 Index 不能为负数")
	}
	if err := validateXMLDate(value.CreationDate, "文档版本 CreationDate"); err != nil {
		return err
	}
	if len(value.Files) == 0 {
		return errors.New("文档版本文件列表不能为空")
	}
	if len(value.DocRoot) > 0 {
		if err := validateOFDSchema(value.DocRoot, "Document"); err != nil {
			return fmt.Errorf("文档版本根文件不符合 Document.xsd: %w", err)
		}
		doc := etree.NewDocument()
		if err := doc.ReadFromBytes(value.DocRoot); err != nil {
			return fmt.Errorf("文档版本根文件 XML 无效: %w", err)
		}
		if doc.Root() == nil || doc.Root().Tag != "Document" || doc.Root().NamespaceURI() != ofNamespace {
			return errors.New("文档版本根文件必须是 OFD 命名空间中的 Document")
		}
	}
	if err := validateLeafFileName(value.DocRootName); err != nil {
		return fmt.Errorf("文档版本根文件名无效: %w", err)
	}
	if len(value.DocRoot) == 0 && strings.TrimSpace(value.DocRootName) != "" {
		return errors.New("文档版本 DocRootName 必须同时提供 DocRoot")
	}
	seen := make(map[string]bool, len(value.Files))
	seen[value.ID] = true
	for index, file := range value.Files {
		if !validXMLID(file.ID) || strings.TrimSpace(file.Path) == "" {
			return fmt.Errorf("文档版本文件 %d 无效", index+1)
		}
		if seen[file.ID] {
			return fmt.Errorf("文档版本文件 ID 重复: %s", file.ID)
		}
		seen[file.ID] = true
		if err := validatePackagePath(file.Path, docDir, false); err != nil {
			return fmt.Errorf("文档版本文件路径无效: %w", err)
		}
	}
	return nil
}

func validatePackagePath(value, base string, allowParent bool) error {
	if value == "" || value != strings.TrimSpace(value) || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\\x00") {
		return errors.New("必须是包内相对路径")
	}
	cleanValue := path.Clean(value)
	if cleanValue == "." || cleanValue != value {
		return errors.New("路径必须规范化")
	}
	depth := len(strings.Split(path.Clean(base), "/"))
	for _, segment := range strings.Split(strings.ReplaceAll(value, "\\", "/"), "/") {
		switch segment {
		case "", ".":
		case "..":
			if !allowParent || depth == 0 {
				return errors.New("路径穿越不被允许")
			}
			depth--
		default:
			depth++
		}
	}
	if clean := path.Clean(path.Join(base, value)); clean == "." || strings.HasPrefix(clean, "../") {
		return errors.New("路径穿越不被允许")
	}
	return nil
}

func validXMLID(value string) bool {
	if value == "" || value != strings.TrimSpace(value) {
		return false
	}
	for index, r := range value {
		if index == 0 {
			if !(r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
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

func attachmentFormat(value string) string {
	value = strings.TrimPrefix(strings.TrimSpace(value), ".")
	if value == "" || strings.ContainsAny(value, "/\\") {
		return ""
	}
	return strings.ToLower(value)
}

func validateLeafFileName(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.ContainsAny(value, "/\\\x00") || value == "." || value == ".." {
		return errors.New("不能包含路径分隔符、NUL 或路径段")
	}
	return nil
}

func validateResExternalFiles(data []byte, resourceName string, files []string) error {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(data); err != nil {
		return err
	}
	root := doc.Root()
	if root == nil {
		return errors.New("资源 XML 根元素为空")
	}
	baseLoc := strings.TrimSpace(root.SelectAttrValue("BaseLoc", "."))
	if baseLoc == "" {
		baseLoc = "."
	}
	if baseLoc != "." {
		if err := validatePackagePath(baseLoc, path.Dir(resourceName), true); err != nil {
			return fmt.Errorf("BaseLoc 无效: %w", err)
		}
	}
	baseDir := path.Clean(path.Join(path.Dir(resourceName), baseLoc))
	if baseDir == ".." || strings.HasPrefix(baseDir, "../") {
		return errors.New("BaseLoc 不能超出文档目录")
	}
	provided := make(map[string]bool, len(files))
	for _, file := range files {
		provided[path.Clean(path.Join(path.Dir(resourceName), file))] = true
	}
	refs := make([]string, 0)
	for _, element := range root.FindElements("ColorSpaces/ColorSpace") {
		if value := strings.TrimSpace(element.SelectAttrValue("Profile", "")); value != "" {
			refs = append(refs, value)
		}
	}
	for _, element := range root.FindElements("Fonts/Font") {
		fontFile := element.FindElement("FontFile")
		if fontFile != nil {
			if value := strings.TrimSpace(fontFile.Text()); value != "" {
				refs = append(refs, value)
			}
		}
	}
	for _, element := range root.FindElements("MultiMedias/MultiMedia") {
		media := element.FindElement("MediaFile")
		if media != nil && strings.TrimSpace(media.Text()) != "" {
			refs = append(refs, strings.TrimSpace(media.Text()))
		}
	}
	for index, reference := range refs {
		if err := validatePackagePath(reference, baseDir, true); err != nil {
			return fmt.Errorf("第 %d 个引用路径无效: %w", index+1, err)
		}
		target := path.Clean(path.Join(baseDir, reference))
		if target == "." || strings.HasPrefix(target, "../") {
			return fmt.Errorf("第 %d 个引用路径超出文档目录: %s", index+1, reference)
		}
		if !provided[target] {
			return fmt.Errorf("引用的文件不存在: %s", reference)
		}
	}
	return nil
}

func mediaFormat(value Media) string {
	format := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value.Format)), ".")
	if format != "" {
		return format
	}
	return "bin"
}

func validateColorSpace(value ColorSpace) error {
	if value.ID == 0 {
		return errors.New("颜色空间 ID 不能为空")
	}
	if value.ID > maxOFDID {
		return fmt.Errorf("颜色空间 ID 超出 OFD 范围: %d", value.ID)
	}
	switch value.Type {
	case "GRAY", "RGB", "CMYK":
	default:
		return fmt.Errorf("颜色空间类型无效: %q", value.Type)
	}
	if value.BitsPerComponent < 0 || value.BitsPerComponent > 32 {
		return errors.New("颜色分量位数无效")
	}
	channels := colorSpaceChannels(value.Type)
	for index, palette := range value.Palette {
		parts := strings.Fields(palette)
		if len(parts) != channels {
			return fmt.Errorf("调色板第 %d 项需要 %d 个分量", index+1, channels)
		}
		if err := validatePaletteComponents(parts, value.BitsPerComponent); err != nil {
			return fmt.Errorf("调色板第 %d 项无效: %w", index+1, err)
		}
	}
	if strings.TrimSpace(value.Profile) != "" {
		if err := validatePackagePath(value.Profile, resDir, false); err != nil {
			return fmt.Errorf("颜色空间 Profile 路径无效: %w", err)
		}
	}
	if strings.TrimSpace(value.Profile) != "" && len(value.ProfileData) == 0 {
		return errors.New("颜色空间 Profile 必须同时提供 ProfileData")
	}
	return nil
}

func colorSpaceChannels(value string) int {
	switch value {
	case "GRAY":
		return 1
	case "CMYK":
		return 4
	default:
		return 3
	}
}

func maxColorComponent(bits int) float64 {
	if bits <= 0 {
		bits = 8
	}
	if bits > 32 {
		bits = 32
	}
	return float64((uint64(1) << bits) - 1)
}

func validatePaletteComponents(parts []string, bits int) error {
	maximum := maxColorComponent(bits)
	for _, part := range parts {
		component, err := strconv.ParseFloat(part, 64)
		if err != nil || !finite(component) || component < 0 || component > maximum {
			return fmt.Errorf("分量必须位于 0 到 %.0f 之间", maximum)
		}
	}
	return nil
}

func validateColor(value *Color) error {
	if value == nil {
		return nil
	}
	if value.ColorSpace == 0 && len(value.Components) > 0 && len(value.Components) != 3 {
		return errors.New("未指定颜色空间时颜色必须包含 3 个分量")
	}
	if value.ColorSpace == 0 {
		for _, component := range value.Components {
			if component < 0 || component > 255 {
				return errors.New("默认 RGB 颜色分量必须位于 0 到 255 之间")
			}
		}
	}
	shadingCount := 0
	if value.Pattern != nil {
		shadingCount++
	}
	if value.Axial != nil {
		shadingCount++
	}
	if value.Radial != nil {
		shadingCount++
	}
	if value.Gouraud != nil {
		shadingCount++
	}
	if value.LaGouraud != nil {
		shadingCount++
	}
	if value.ColorSpace != 0 && len(value.Components) == 0 && value.Index == nil && shadingCount == 0 {
		return errors.New("颜色空间引用需要颜色值或调色板索引")
	}
	if value.Index != nil && *value.Index < 0 {
		return errors.New("调色板索引不能为负数")
	}
	for _, component := range value.Components {
		if component < 0 {
			return errors.New("颜色分量无效")
		}
	}
	if value.Axial != nil {
		if err := validateAxial(value.Axial); err != nil {
			return err
		}
	}
	if value.Radial != nil {
		if err := validateRadial(value.Radial); err != nil {
			return err
		}
	}
	if value.Gouraud != nil {
		if err := validateGouraud(value.Gouraud); err != nil {
			return err
		}
	}
	if value.LaGouraud != nil {
		if err := validateLaGouraud(value.LaGouraud); err != nil {
			return err
		}
	}
	if value.Pattern != nil {
		if err := validatePattern(value.Pattern); err != nil {
			return err
		}
	}
	if shadingCount > 1 {
		return errors.New("颜色只能设置一种图案或渐变")
	}
	return nil
}

func validateDocumentColors(document Document, state *buildState) error {
	known := make(map[uint64]bool, len(document.ColorSpaces))
	spaceChannels := make(map[uint64]int, len(document.ColorSpaces))
	spacePaletteSizes := make(map[uint64]int, len(document.ColorSpaces))
	checkingPatterns := make(map[*Pattern]bool)
	checkedPatterns := make(map[*Pattern]bool)
	for _, space := range document.ColorSpaces {
		known[space.ID] = true
		spaceChannels[space.ID] = colorSpaceChannels(space.Type)
		spacePaletteSizes[space.ID] = len(space.Palette)
	}
	for id, space := range state.colorSpaces {
		known[id] = true
		spaceChannels[id] = space.channels
		spacePaletteSizes[id] = space.paletteSize
	}
	var check func(*Color) error
	check = func(value *Color) error {
		if value == nil {
			return nil
		}
		if value.ColorSpace != 0 && !known[value.ColorSpace] {
			return fmt.Errorf("颜色引用了不存在的颜色空间 ID %d", value.ColorSpace)
		}
		if value.ColorSpace != 0 {
			if len(value.Components) > 0 && len(value.Components) != spaceChannels[value.ColorSpace] {
				return fmt.Errorf("颜色空间 ID %d 需要 %d 个分量", value.ColorSpace, spaceChannels[value.ColorSpace])
			}
			if len(value.Components) > 0 {
				bits := 0
				if space, ok := state.colorSpaces[value.ColorSpace]; ok {
					bits = space.bits
				} else {
					for _, space := range document.ColorSpaces {
						if space.ID == value.ColorSpace {
							bits = space.BitsPerComponent
							break
						}
					}
				}
				maximum := maxColorComponent(bits)
				for _, component := range value.Components {
					if float64(component) > maximum {
						return fmt.Errorf("颜色空间 ID %d 的颜色分量超出 0 到 %.0f 的范围", value.ColorSpace, maximum)
					}
				}
			}
			if value.Index != nil && (spacePaletteSizes[value.ColorSpace] == 0 || *value.Index >= spacePaletteSizes[value.ColorSpace]) {
				return fmt.Errorf("颜色空间 ID %d 的调色板索引超出范围", value.ColorSpace)
			}
		}
		if value.Pattern != nil {
			if checkingPatterns[value.Pattern] {
				return errors.New("颜色图案包含循环引用")
			}
			if checkedPatterns[value.Pattern] {
				return nil
			}
			checkingPatterns[value.Pattern] = true
			if err := state.preparePattern(value.Pattern, len(document.Pages), "颜色图案"); err != nil {
				delete(checkingPatterns, value.Pattern)
				return err
			}
			for _, item := range value.Pattern.Items {
				if err := validateItemColors(item, check); err != nil {
					return err
				}
			}
			for _, layer := range value.Pattern.Layers {
				for _, item := range layer.Items {
					if err := validateItemColors(item, check); err != nil {
						return err
					}
				}
			}
			delete(checkingPatterns, value.Pattern)
			checkedPatterns[value.Pattern] = true
		}
		if value.Axial != nil {
			for _, segment := range value.Axial.Segments {
				if err := check(&segment.Color); err != nil {
					return err
				}
			}
		}
		if value.Radial != nil {
			for _, segment := range value.Radial.Segments {
				if err := check(&segment.Color); err != nil {
					return err
				}
			}
		}
		if value.Gouraud != nil {
			for _, point := range value.Gouraud.Points {
				if err := check(&point.Color); err != nil {
					return err
				}
			}
			if err := check(value.Gouraud.BackColor); err != nil {
				return err
			}
		}
		if value.LaGouraud != nil {
			for _, point := range value.LaGouraud.Points {
				if err := check(&point.Color); err != nil {
					return err
				}
			}
			if err := check(value.LaGouraud.BackColor); err != nil {
				return err
			}
		}
		return nil
	}
	for _, param := range document.DrawParams {
		if err := check(param.FillColor); err != nil {
			return fmt.Errorf("绘制参数颜色无效: %w", err)
		}
		if err := check(param.StrokeColor); err != nil {
			return fmt.Errorf("绘制参数颜色无效: %w", err)
		}
	}
	for _, page := range append(append([]Page{}, document.Pages...), templatePages(document.Templates)...) {
		for _, layer := range page.Layers {
			for _, item := range layer.Items {
				if err := validateItemColors(item, check); err != nil {
					return err
				}
			}
		}
		for _, item := range page.Items {
			if err := validateItemColors(item, check); err != nil {
				return err
			}
		}
	}
	for _, composite := range document.Composites {
		for _, item := range composite.Items {
			if err := validateItemColors(item, check); err != nil {
				return err
			}
		}
	}
	for _, annotationPage := range document.Annotations {
		for _, annotation := range annotationPage.Items {
			for _, item := range annotation.Items {
				if err := validateItemColors(item, check); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateItemColors(item Item, check func(*Color) error) error {
	switch value := item.(type) {
	case Text:
		if err := check(value.FillColor); err != nil {
			return err
		}
		if err := check(value.StrokeColor); err != nil {
			return err
		}
		return validateClipColors(value.Clips, check)
	case Path:
		if err := check(value.FillColor); err != nil {
			return err
		}
		if err := check(value.StrokeColor); err != nil {
			return err
		}
		return validateClipColors(value.Clips, check)
	case Image:
		if value.Border != nil {
			if err := check(value.Border.Color); err != nil {
				return err
			}
		}
		return validateClipColors(value.Clips, check)
	case Composite:
		return validateClipColors(value.Clips, check)
	case PageBlock:
		for _, nested := range value.Items {
			if err := validateItemColors(nested, check); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateClipColors(clips *Clips, check func(*Color) error) error {
	if clips == nil {
		return nil
	}
	for _, clip := range clips.Items {
		for _, area := range clip.Areas {
			if area.Path != nil {
				if err := check(area.Path.StrokeColor); err != nil {
					return err
				}
				if err := check(area.Path.FillColor); err != nil {
					return err
				}
			}
			if area.Text != nil {
				if err := check(area.Text.FillColor); err != nil {
					return err
				}
				if err := check(area.Text.StrokeColor); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func templatePages(values []TemplatePage) []Page {
	pages := make([]Page, len(values))
	for index, value := range values {
		pages[index] = Page{Items: value.Items, Layers: value.Layers}
	}
	return pages
}

func validatePattern(value *Pattern) error {
	if value == nil {
		return nil
	}
	if !validSize(value.Width, value.Height) {
		return errors.New("图案宽高必须为正数")
	}
	if value.XStep < 0 || value.YStep < 0 || !finite(value.XStep) || !finite(value.YStep) {
		return errors.New("图案步长无效")
	}
	switch value.ReflectMethod {
	case "", "Normal", "Row", "Column", "RowAndColumn":
	default:
		return fmt.Errorf("图案反射方式无效: %q", value.ReflectMethod)
	}
	switch value.RelativeTo {
	case "", "Page", "Object":
	default:
		return fmt.Errorf("图案相对坐标系无效: %q", value.RelativeTo)
	}
	if err := validateCTM(value.CTM); err != nil {
		return fmt.Errorf("图案 CTM 无效: %w", err)
	}
	if len(value.Items) == 0 && len(value.Layers) == 0 {
		return errors.New("图案单元不能为空")
	}
	return nil
}

func validateAxial(value *AxialShading) error {
	if len(value.Segments) < 2 || strings.TrimSpace(value.StartPoint) == "" || strings.TrimSpace(value.EndPoint) == "" {
		return errors.New("轴向渐变参数不完整")
	}
	if err := validatePosition(value.StartPoint); err != nil {
		return fmt.Errorf("轴向渐变 StartPoint 无效: %w", err)
	}
	if err := validatePosition(value.EndPoint); err != nil {
		return fmt.Errorf("轴向渐变 EndPoint 无效: %w", err)
	}
	if value.MapType != "" && value.MapType != "Direct" && value.MapType != "Repeat" && value.MapType != "Reflect" {
		return errors.New("轴向渐变映射方式无效")
	}
	if value.Extend < 0 || value.Extend > 3 || value.MapUnit < 0 || !finite(value.MapUnit) {
		return errors.New("轴向渐变参数无效")
	}
	for _, segment := range value.Segments {
		if !finite(segment.Position) || segment.Position < 0 || segment.Position > 1 {
			return errors.New("轴向渐变位置无效")
		}
		if err := validateColor(&segment.Color); err != nil {
			return err
		}
	}
	return nil
}

func validateRadial(value *RadialShading) error {
	if len(value.Segments) < 2 || strings.TrimSpace(value.StartPoint) == "" || strings.TrimSpace(value.EndPoint) == "" || value.EndRadius < 0 {
		return errors.New("径向渐变参数不完整")
	}
	if err := validatePosition(value.StartPoint); err != nil {
		return fmt.Errorf("径向渐变 StartPoint 无效: %w", err)
	}
	if err := validatePosition(value.EndPoint); err != nil {
		return fmt.Errorf("径向渐变 EndPoint 无效: %w", err)
	}
	if value.MapType != "" && value.MapType != "Direct" && value.MapType != "Repeat" && value.MapType != "Reflect" {
		return errors.New("径向渐变映射方式无效")
	}
	if value.Extend < 0 || value.Extend > 3 || value.MapUnit < 0 || !finite(value.MapUnit) || !finite(value.Eccentricity) || !finite(value.Angle) || value.StartRadius < 0 || !finite(value.StartRadius) || !finite(value.EndRadius) {
		return errors.New("径向渐变参数无效")
	}
	for _, segment := range value.Segments {
		if !finite(segment.Position) || segment.Position < 0 || segment.Position > 1 {
			return errors.New("径向渐变位置无效")
		}
		if err := validateColor(&segment.Color); err != nil {
			return err
		}
	}
	return nil
}

func validatePosition(value string) error {
	parts := strings.Fields(value)
	if len(parts) != 2 {
		return errors.New("位置必须包含两个数值")
	}
	for _, part := range parts {
		number, err := strconv.ParseFloat(part, 64)
		if err != nil || !finite(number) {
			return errors.New("位置必须包含有限数值")
		}
	}
	return nil
}

func validateGouraud(value *GouraudShading) error {
	if len(value.Points) < 3 {
		return errors.New("Gouraud 渐变至少需要 3 个控制点")
	}
	if value.Extend < 0 {
		return errors.New("Gouraud 渐变 Extend 无效")
	}
	for _, point := range value.Points {
		if !finite(point.X) || !finite(point.Y) || point.EdgeFlag < 0 || point.EdgeFlag > 2 {
			return errors.New("Gouraud 控制点参数无效")
		}
		if err := validateColor(&point.Color); err != nil {
			return err
		}
	}
	if err := validateColor(value.BackColor); err != nil {
		return err
	}
	return nil
}

func validateLaGouraud(value *LaGouraudShading) error {
	if value.VerticesPerRow < 2 || len(value.Points) < 4 {
		return errors.New("LaGouraud 渐变参数不完整")
	}
	if len(value.Points)%value.VerticesPerRow != 0 {
		return errors.New("LaGouraud 控制点数量必须是每行顶点数的整数倍")
	}
	if value.Extend < 0 {
		return errors.New("LaGouraud 渐变 Extend 无效")
	}
	for _, point := range value.Points {
		if !finite(point.X) || !finite(point.Y) {
			return errors.New("LaGouraud 控制点坐标无效")
		}
		if err := validateColor(&point.Color); err != nil {
			return err
		}
	}
	if err := validateColor(value.BackColor); err != nil {
		return err
	}
	return nil
}

func validateComposite(value Composite) error {
	if err := validateBox(value.X, value.Y, value.Width, value.Height); err != nil {
		return err
	}
	if value.ResourceID == 0 {
		return errors.New("复合图元资源 ID 不能为空")
	}
	return validateCTM(value.CTM)
}

func validateCompositeResource(value CompositeGraphicUnit) error {
	if value.ID == 0 {
		return errors.New("复合图元资源 ID 不能为空")
	}
	if !validSize(value.Width, value.Height) {
		return fmt.Errorf("复合图元尺寸无效: %.6g x %.6g", value.Width, value.Height)
	}
	if len(value.Items) == 0 {
		return errors.New("复合图元内容不能为空")
	}
	return nil
}

func validateCTMAndClips(ctm *CTM, clips *Clips) error {
	if err := validateCTM(ctm); err != nil {
		return err
	}
	if clips == nil {
		return nil
	}
	if len(clips.Items) == 0 {
		return errors.New("裁剪集合不能为空")
	}
	for clipIndex, clip := range clips.Items {
		if len(clip.Areas) == 0 {
			return fmt.Errorf("裁剪区域 %d 不能为空", clipIndex+1)
		}
		for areaIndex, area := range clip.Areas {
			if (area.Path == nil) == (area.Text == nil) {
				return fmt.Errorf("裁剪区域 %d 的 Area %d 必须且只能设置路径或文字", clipIndex+1, areaIndex+1)
			}
			if area.CTM != nil {
				for index, value := range area.CTM {
					if !finite(value) {
						return fmt.Errorf("裁剪区域 %d 的 Area %d CTM 第 %d 个参数无效", clipIndex+1, areaIndex+1, index+1)
					}
				}
			}
			if area.Path != nil {
				path := area.Path
				if err := validateCTM(path.CTM); err != nil {
					return fmt.Errorf("裁剪区域 %d 的 Area %d 路径 CTM 无效: %w", clipIndex+1, areaIndex+1, err)
				}
				if err := validateBox(path.Boundary.X, path.Boundary.Y, path.Boundary.Width, path.Boundary.Height); err != nil {
					return fmt.Errorf("裁剪区域 %d 的 Area %d 边界无效: %w", clipIndex+1, areaIndex+1, err)
				}
				if strings.TrimSpace(path.Data) == "" {
					return fmt.Errorf("裁剪区域 %d 的 Area %d 路径数据不能为空", clipIndex+1, areaIndex+1)
				}
				if path.Rule != "" && path.Rule != "NonZero" && path.Rule != "Even-Odd" {
					return fmt.Errorf("裁剪区域 %d 的 Area %d 填充规则无效", clipIndex+1, areaIndex+1)
				}
				if err := validateGraphicStyle(path.LineWidth, path.Cap, path.Join, path.MiterLimit, path.DashOffset, path.DashPattern, path.Alpha); err != nil {
					return fmt.Errorf("裁剪区域 %d 的 Area %d 线条参数无效: %w", clipIndex+1, areaIndex+1, err)
				}
			} else if err := validateClipText(*area.Text); err != nil {
				return fmt.Errorf("裁剪区域 %d 的 Area %d 文字参数无效: %w", clipIndex+1, areaIndex+1, err)
			}
		}
	}
	return nil
}

func validateClips(clips *Clips) error {
	return validateCTMAndClips(nil, clips)
}

func validateClipText(value ClipText) error {
	if err := validateCTM(value.CTM); err != nil {
		return err
	}
	if err := validateBox(value.Boundary.X, value.Boundary.Y, value.Boundary.Width, value.Boundary.Height); err != nil {
		return err
	}
	if strings.TrimSpace(value.Value) == "" && len(value.TextCodes) == 0 {
		return errors.New("文字内容不能为空")
	}
	if value.Size < 0 || !finite(value.Size) {
		return fmt.Errorf("字号无效: %.6g", value.Size)
	}
	if value.HScale < 0 || value.HScale > 1 || !finite(value.HScale) {
		return fmt.Errorf("水平缩放无效: %.6g", value.HScale)
	}
	if value.Weight != 0 && (value.Weight < 100 || value.Weight > 1000 || value.Weight%100 != 0) {
		return fmt.Errorf("字重无效: %d", value.Weight)
	}
	for index, code := range value.TextCodes {
		if strings.TrimSpace(code.Value) == "" {
			return fmt.Errorf("TextCode %d 内容不能为空", index+1)
		}
		if code.X != nil && !finite(*code.X) || code.Y != nil && !finite(*code.Y) {
			return fmt.Errorf("TextCode %d 坐标无效", index+1)
		}
		for _, delta := range append(append([]float64{}, code.DeltaX...), code.DeltaY...) {
			if !finite(delta) {
				return fmt.Errorf("TextCode %d 间距参数无效", index+1)
			}
		}
	}
	return nil
}

func validateCTM(value *CTM) error {
	if value == nil {
		return nil
	}
	for index, item := range value {
		if !finite(item) {
			return fmt.Errorf("CTM 第 %d 个参数无效: %.6g", index+1, item)
		}
	}
	return nil
}

func validateGraphicStyle(lineWidth float64, cap, join string, miterLimit, dashOffset float64, dashPattern []float64, alpha *uint8) error {
	if lineWidth < 0 || !finite(lineWidth) || miterLimit < 0 || !finite(miterLimit) || dashOffset < 0 || !finite(dashOffset) {
		return errors.New("图元线条参数无效")
	}
	if cap != "" && cap != "Butt" && cap != "Round" && cap != "Square" {
		return fmt.Errorf("线端点样式无效: %q", cap)
	}
	if join != "" && join != "Miter" && join != "Round" && join != "Bevel" {
		return fmt.Errorf("线连接样式无效: %q", join)
	}
	for _, value := range dashPattern {
		if value < 0 || !finite(value) {
			return errors.New("虚线模式参数无效")
		}
	}
	_ = alpha
	return nil
}

func validateBorder(border *ImageBorder) error {
	if border == nil {
		return nil
	}
	if border.LineWidth < 0 || !finite(border.LineWidth) || border.HorizontalRadius < 0 || !finite(border.HorizontalRadius) || border.VerticalRadius < 0 || !finite(border.VerticalRadius) || border.DashOffset < 0 || !finite(border.DashOffset) {
		return errors.New("图片边框参数无效")
	}
	for _, value := range border.DashPattern {
		if value < 0 || !finite(value) {
			return errors.New("图片边框虚线模式参数无效")
		}
	}
	return nil
}

func validateFont(value Font) error {
	if strings.TrimSpace(value.Name) == "" {
		return errors.New("字体名称不能为空")
	}
	if value.Charset != "" && !validFontCharset(value.Charset) {
		return fmt.Errorf("字符集无效: %q", value.Charset)
	}
	if len(value.Data) > 0 && !validFontFormat(fontFormat(value)) {
		return errors.New("无法识别字体格式，请设置 Format")
	}
	return nil
}

func fontCharset(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unicode"
	}
	return value
}

func validFontCharset(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "symbol", "prc", "big5", "shift-jis", "wansung", "johab", "unicode":
		return true
	default:
		return false
	}
}

func fontFormat(value Font) string {
	format := strings.ToLower(strings.TrimSpace(value.Format))
	if format != "" {
		return strings.TrimPrefix(format, ".")
	}
	if len(value.Data) >= 4 {
		switch string(value.Data[:4]) {
		case "\x00\x01\x00\x00", "true":
			return "ttf"
		case "OTTO":
			return "otf"
		case "ttcf":
			return "ttc"
		}
	}
	return ""
}

func validFontFormat(value string) bool {
	switch value {
	case "ttf", "otf", "ttc":
		return true
	default:
		return false
	}
}

func imageFormat(value Image) string {
	format := strings.ToUpper(strings.TrimSpace(value.Format))
	switch format {
	case "JPG":
		return "JPEG"
	case "JPEG", "PNG", "BMP", "TIFF", "GIF", "WEBP":
		return format
	}
	if len(value.Data) >= 8 && bytes.Equal(value.Data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return "PNG"
	}
	if len(value.Data) >= 3 && bytes.Equal(value.Data[:3], []byte{0xff, 0xd8, 0xff}) {
		return "JPEG"
	}
	return ""
}

func number(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func boxString(x, y, width, height float64) string {
	return number(x) + " " + number(y) + " " + number(width) + " " + number(height)
}

func numberList(values []float64) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = number(value)
	}
	return strings.Join(parts, " ")
}

func intList(values []int) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = strconv.Itoa(value)
	}
	return strings.Join(parts, " ")
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
