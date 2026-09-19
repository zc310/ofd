package creator

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// xmlReplacementChar 用于替换 XML 1.0 不允许的字符（如 PDF 文本里的控制符）。
// 直接写入会让生成的 OFD 无法解析（PCDATA invalid Char value）。
var xmlReplacementChar = []byte("\uFFFD")

// isXMLChar 判断 rune 是否允许出现在 XML 1.0 文档中。
func isXMLChar(r rune) bool {
	switch {
	case r == 0x09 || r == 0x0A || r == 0x0D:
		return true
	case r >= 0x20 && r <= 0xD7FF:
		return true
	case r >= 0xE000 && r <= 0xFFFD:
		return true
	case r >= 0x10000 && r <= 0x10FFFF:
		return true
	default:
		return false
	}
}

// streamXMLWriter 是页面高频生成路径使用的轻量 XML 写入器。
// 它只支持 creator 所需的操作，不在内存中保留 XML 树。
type streamXMLWriter struct {
	buf   []byte
	stack []string
	open  bool
}

func newStreamXMLWriter() *streamXMLWriter {
	w := &streamXMLWriter{buf: make([]byte, 0, 4096), stack: make([]string, 0, 8)}
	w.buf = append(w.buf, `<?xml version="1.0" encoding="UTF-8"?>`...)
	return w
}

func (w *streamXMLWriter) Start(name string) {
	w.closeStart()
	w.buf = append(w.buf, '<')
	w.buf = append(w.buf, name...)
	w.stack = append(w.stack, name)
	w.open = true
}

func (w *streamXMLWriter) Attr(name, value string) {
	if !w.open {
		return
	}
	w.startAttr(name)
	w.appendAttrText(value)
	w.buf = append(w.buf, '"')
}

func (w *streamXMLWriter) AttrUint(name string, value uint64) {
	if !w.open {
		return
	}
	w.startAttr(name)
	w.buf = strconv.AppendUint(w.buf, value, 10)
	w.buf = append(w.buf, '"')
}

func (w *streamXMLWriter) AttrInt(name string, value int) {
	if !w.open {
		return
	}
	w.startAttr(name)
	w.buf = strconv.AppendInt(w.buf, int64(value), 10)
	w.buf = append(w.buf, '"')
}

func (w *streamXMLWriter) AttrFloat(name string, value float64) {
	if !w.open {
		return
	}
	w.startAttr(name)
	w.buf = appendFloat(w.buf, value)
	w.buf = append(w.buf, '"')
}

func (w *streamXMLWriter) AttrBox(name string, x, y, width, height float64) {
	if !w.open {
		return
	}
	w.startAttr(name)
	w.buf = appendFloat(w.buf, x)
	w.buf = append(w.buf, ' ')
	w.buf = appendFloat(w.buf, y)
	w.buf = append(w.buf, ' ')
	w.buf = appendFloat(w.buf, width)
	w.buf = append(w.buf, ' ')
	w.buf = appendFloat(w.buf, height)
	w.buf = append(w.buf, '"')
}

func (w *streamXMLWriter) AttrFloatList(name string, values []float64) {
	if !w.open {
		return
	}
	w.startAttr(name)
	for index, value := range values {
		if index > 0 {
			w.buf = append(w.buf, ' ')
		}
		w.buf = appendFloat(w.buf, value)
	}
	w.buf = append(w.buf, '"')
}

func (w *streamXMLWriter) AttrIntList(name string, values []int) {
	if !w.open {
		return
	}
	w.startAttr(name)
	for index, value := range values {
		if index > 0 {
			w.buf = append(w.buf, ' ')
		}
		w.buf = strconv.AppendInt(w.buf, int64(value), 10)
	}
	w.buf = append(w.buf, '"')
}

func (w *streamXMLWriter) startAttr(name string) {
	w.buf = append(w.buf, ' ')
	w.buf = append(w.buf, name...)
	w.buf = append(w.buf, '=', '"')
}

// numberPrecision 是 OFD XML 中浮点数保留的小数位数。0.0001mm（0.1µm）远低于
// 任何渲染分辨率，同时避免浮点噪声导致的超长输出（如 96.34784444444446）。
const numberPrecision = 4

// appendFloat 以固定小数位输出浮点数，去掉末尾多余的 0 和小数点，且不使用
// 科学计数法（部分阅读器无法解析）。极小值四舍五入为 0，避免 "-0"。
func appendFloat(dst []byte, value float64) []byte {
	if !finite(value) {
		value = 0
	}
	start := len(dst)
	dst = strconv.AppendFloat(dst, value, 'f', numberPrecision, 64)
	end := len(dst)
	for end > start+1 && dst[end-1] == '0' {
		end--
	}
	if end > start+1 && dst[end-1] == '.' {
		end--
	}
	dst = dst[:end]
	if end == start+2 && dst[start] == '-' && dst[start+1] == '0' {
		dst[start] = '0'
		dst = dst[:start+1]
	}
	return dst
}

func (w *streamXMLWriter) Text(value string) {
	w.closeStart()
	w.appendText(value)
}

func (w *streamXMLWriter) End() {
	if len(w.stack) == 0 {
		return
	}
	name := w.stack[len(w.stack)-1]
	w.stack = w.stack[:len(w.stack)-1]
	if w.open {
		w.buf = append(w.buf, '/', '>')
		w.open = false
		return
	}
	w.buf = append(w.buf, '<', '/')
	w.buf = append(w.buf, name...)
	w.buf = append(w.buf, '>')
}

func (w *streamXMLWriter) Bytes() []byte {
	for len(w.stack) > 0 {
		w.End()
	}
	return w.buf
}

func (w *streamXMLWriter) closeStart() {
	if w.open {
		w.buf = append(w.buf, '>')
		w.open = false
	}
}

func (w *streamXMLWriter) appendText(value string) {
	for index := 0; index < len(value); {
		r, size := utf8.DecodeRuneInString(value[index:])
		if r == utf8.RuneError && size == 1 {
			w.buf = append(w.buf, xmlReplacementChar...)
			index++
			continue
		}
		switch r {
		case '&':
			w.buf = append(w.buf, "&amp;"...)
		case '<':
			w.buf = append(w.buf, "&lt;"...)
		case '>':
			w.buf = append(w.buf, "&gt;"...)
		default:
			if isXMLChar(r) {
				w.buf = append(w.buf, value[index:index+size]...)
			} else {
				w.buf = append(w.buf, xmlReplacementChar...)
			}
		}
		index += size
	}
}

func (w *streamXMLWriter) appendAttrText(value string) {
	for index := 0; index < len(value); {
		r, size := utf8.DecodeRuneInString(value[index:])
		if r == utf8.RuneError && size == 1 {
			w.buf = append(w.buf, xmlReplacementChar...)
			index++
			continue
		}
		switch r {
		case '&':
			w.buf = append(w.buf, "&amp;"...)
		case '<':
			w.buf = append(w.buf, "&lt;"...)
		case '"':
			w.buf = append(w.buf, "&quot;"...)
		case '\t':
			w.buf = append(w.buf, "&#x9;"...)
		case '\n':
			w.buf = append(w.buf, "&#xA;"...)
		case '\r':
			w.buf = append(w.buf, "&#xD;"...)
		default:
			if isXMLChar(r) {
				w.buf = append(w.buf, value[index:index+size]...)
			} else {
				w.buf = append(w.buf, xmlReplacementChar...)
			}
		}
		index += size
	}
}

func streamPageXML(state *buildState, page Page, pageResources []pageResource, layers []builtLayer) []byte {
	w := newStreamXMLWriter()
	w.Start("Page")
	w.Attr("xmlns", ofNamespace)
	for _, template := range page.Templates {
		w.Start("Template")
		w.AttrUint("TemplateID", template.ID)
		if template.ZOrder != "" {
			w.Attr("ZOrder", template.ZOrder)
		}
		w.End()
	}
	for _, resource := range pageResources {
		streamElementText(w, "PageRes", resource.name)
	}
	if page.Area != nil {
		w.Start("Area")
		streamPageArea(w, page.Area, state.pageSize)
		w.End()
	}
	w.Start("Content")
	streamBuiltLayers(w, layers, state)
	w.End()
	if len(page.Actions) > 0 {
		streamActions(w, page.Actions, state.pageIDs)
	}
	w.End()
	return w.Bytes()
}

func streamTemplateXML(state *buildState, templateIndex int) []byte {
	w := newStreamXMLWriter()
	w.Start("Page")
	w.Attr("xmlns", ofNamespace)
	template := state.document.Templates[templateIndex]
	if template.Area != nil {
		w.Start("Area")
		streamPageArea(w, template.Area, state.pageSize)
		w.End()
	}
	w.Start("Content")
	streamBuiltLayers(w, state.templateLayers[templateIndex], state)
	w.End()
	w.End()
	return w.Bytes()
}

func streamElementText(w *streamXMLWriter, name, value string) {
	w.Start(name)
	w.Text(value)
	w.End()
}

func streamPageArea(w *streamXMLWriter, value *PageArea, pageSize PageSize) {
	physical := value.PhysicalBox
	if physical == nil {
		physical = &Box{Width: pageSize.Width, Height: pageSize.Height}
	}
	streamElementText(w, "PhysicalBox", boxString(physical.X, physical.Y, physical.Width, physical.Height))
	if value.ApplicationBox != nil {
		streamElementText(w, "ApplicationBox", boxString(value.ApplicationBox.X, value.ApplicationBox.Y, value.ApplicationBox.Width, value.ApplicationBox.Height))
	}
	if value.ContentBox != nil {
		streamElementText(w, "ContentBox", boxString(value.ContentBox.X, value.ContentBox.Y, value.ContentBox.Width, value.ContentBox.Height))
	}
	if value.BleedBox != nil {
		streamElementText(w, "BleedBox", boxString(value.BleedBox.X, value.BleedBox.Y, value.BleedBox.Width, value.BleedBox.Height))
	}
}

func streamBuiltLayers(w *streamXMLWriter, layers []builtLayer, state *buildState) {
	for _, builtLayer := range layers {
		w.Start("Layer")
		w.AttrUint("ID", builtLayer.id)
		w.Attr("Type", builtLayer.layerType)
		if builtLayer.drawParam != 0 {
			w.AttrUint("DrawParam", builtLayer.drawParam)
		}
		streamBuiltItems(w, builtLayer.items, state)
		w.End()
	}
}

func streamBuiltItems(w *streamXMLWriter, items []builtItem, state *buildState) {
	for _, item := range items {
		switch value := item.item.(type) {
		case Text:
			streamText(w, item, value, state.pageIDs, state.drawParamIDs, state.fontIDs)
		case Path:
			streamPath(w, item, value, state.pageIDs, state.drawParamIDs, state.fontIDs)
		case Image:
			streamImage(w, item, value, state.pageIDs, state.drawParamIDs, state.fontIDs)
		case Composite:
			streamComposite(w, item, value, state.pageIDs, state.drawParamIDs, state.fontIDs)
		case PageBlock:
			w.Start("PageBlock")
			w.AttrUint("ID", item.id)
			streamBuiltItems(w, item.pageBlock, state)
			w.End()
		}
	}
}

func streamGraphicAttributes(w *streamXMLWriter, id uint64, x, y, width, height float64, name string, visible *bool, drawParam uint64) {
	w.AttrUint("ID", id)
	w.AttrBox("Boundary", x, y, width, height)
	if name != "" {
		w.Attr("Name", name)
	}
	if visible != nil {
		w.Attr("Visible", strconv.FormatBool(*visible))
	}
	if drawParam != 0 {
		w.AttrUint("DrawParam", drawParam)
	}
}

func streamGraphicAttributesWithoutID(w *streamXMLWriter, boundary Box, name string, visible *bool) {
	w.AttrBox("Boundary", boundary.X, boundary.Y, boundary.Width, boundary.Height)
	if name != "" {
		w.Attr("Name", name)
	}
	if visible != nil {
		w.Attr("Visible", strconv.FormatBool(*visible))
	}
}

func streamGraphicStyle(w *streamXMLWriter, lineWidth float64, cap, join string, miterLimit, dashOffset float64, dashPattern []float64, alpha *uint8) {
	if lineWidth != 0 {
		w.AttrFloat("LineWidth", lineWidth)
	}
	if cap != "" {
		w.Attr("Cap", cap)
	}
	if join != "" {
		w.Attr("Join", join)
	}
	if miterLimit != 0 {
		w.AttrFloat("MiterLimit", miterLimit)
	}
	if dashOffset != 0 {
		w.AttrFloat("DashOffset", dashOffset)
	}
	if len(dashPattern) > 0 {
		w.AttrFloatList("DashPattern", dashPattern)
	}
	if alpha != nil {
		w.AttrInt("Alpha", int(*alpha))
	}
}

func streamText(w *streamXMLWriter, item builtItem, value Text, pageIDs []uint64, drawParamIDs, fontIDs map[string]uint64) {
	w.Start("TextObject")
	streamGraphicAttributes(w, item.id, value.X, value.Y, value.Width, value.Height, "", value.Visible, item.drawParam)
	if value.CTM != nil {
		w.AttrFloatList("CTM", value.CTM[:])
	}
	w.AttrUint("Font", item.font)
	size := value.Size
	if size == 0 {
		size = 4.2333333333
	}
	w.AttrFloat("Size", size)
	if value.Stroke {
		w.Attr("Stroke", "true")
	}
	if value.Fill != nil {
		w.Attr("Fill", strconv.FormatBool(*value.Fill))
	}
	if value.HScale != 0 {
		w.AttrFloat("HScale", value.HScale)
	}
	if value.ReadDirection != 0 {
		w.AttrInt("ReadDirection", value.ReadDirection)
	}
	if value.CharDirection != 0 {
		w.AttrInt("CharDirection", value.CharDirection)
	}
	if value.Weight != 0 {
		w.AttrInt("Weight", value.Weight)
	}
	if value.Italic {
		w.Attr("Italic", "true")
	}
	streamActionsIfAny(w, value.Actions, pageIDs)
	streamClips(w, value.Clips, drawParamIDs, fontIDs)
	streamColor(w, "FillColor", value.FillColor)
	streamColor(w, "StrokeColor", value.StrokeColor)
	for _, transform := range value.CGTransforms {
		streamCGTransform(w, transform)
	}
	codes := value.TextCodes
	if len(codes) == 0 {
		codes = []TextCode{{Value: value.Value}}
	}
	for _, code := range codes {
		streamTextCode(w, code)
	}
	w.End()
}

func streamCGTransform(w *streamXMLWriter, value CGTransform) {
	w.Start("CGTransform")
	w.AttrInt("CodePosition", value.CodePosition)
	if value.CodeCount != 0 {
		w.AttrInt("CodeCount", value.CodeCount)
	}
	if value.GlyphCount != 0 {
		w.AttrInt("GlyphCount", value.GlyphCount)
	}
	if len(value.Glyphs) > 0 {
		streamElementText(w, "Glyphs", intList(value.Glyphs))
	}
	w.End()
}

func streamTextCode(w *streamXMLWriter, value TextCode) {
	w.Start("TextCode")
	if value.X != nil {
		w.AttrFloat("X", *value.X)
	}
	if value.Y != nil {
		w.AttrFloat("Y", *value.Y)
	}
	if len(value.DeltaX) > 0 {
		w.AttrFloatList("DeltaX", value.DeltaX)
	}
	if len(value.DeltaY) > 0 {
		w.AttrFloatList("DeltaY", value.DeltaY)
	}
	w.Text(value.Value)
	w.End()
}

func streamPath(w *streamXMLWriter, item builtItem, value Path, pageIDs []uint64, drawParamIDs, fontIDs map[string]uint64) {
	w.Start("PathObject")
	streamGraphicAttributes(w, item.id, value.X, value.Y, value.Width, value.Height, value.Name, value.Visible, item.drawParam)
	if value.CTM != nil {
		w.AttrFloatList("CTM", value.CTM[:])
	}
	streamGraphicStyle(w, value.LineWidth, value.Cap, value.Join, value.MiterLimit, value.DashOffset, value.DashPattern, value.Alpha)
	if value.StrokeSet != nil {
		w.Attr("Stroke", strconv.FormatBool(*value.StrokeSet))
	} else if value.Stroke {
		w.Attr("Stroke", "true")
	}
	if value.Fill {
		w.Attr("Fill", "true")
	}
	if value.Rule != "" {
		w.Attr("Rule", value.Rule)
	}
	streamActionsIfAny(w, value.Actions, pageIDs)
	streamClips(w, value.Clips, drawParamIDs, fontIDs)
	streamColor(w, "StrokeColor", value.StrokeColor)
	streamColor(w, "FillColor", value.FillColor)
	streamElementText(w, "AbbreviatedData", strings.TrimSpace(value.Data))
	w.End()
}

func streamImage(w *streamXMLWriter, item builtItem, value Image, pageIDs []uint64, drawParamIDs, fontIDs map[string]uint64) {
	w.Start("ImageObject")
	streamGraphicAttributes(w, item.id, value.X, value.Y, value.Width, value.Height, value.Name, value.Visible, item.drawParam)
	if value.CTM != nil {
		w.AttrFloatList("CTM", value.CTM[:])
	}
	streamGraphicStyle(w, value.LineWidth, value.Cap, value.Join, value.MiterLimit, value.DashOffset, value.DashPattern, value.Alpha)
	resourceID := item.image
	if value.ResourceID != 0 {
		resourceID = value.ResourceID
	}
	w.AttrUint("ResourceID", resourceID)
	if value.Substitution != 0 {
		w.AttrUint("Substitution", value.Substitution)
	}
	if value.ImageMask != 0 {
		w.AttrUint("ImageMask", value.ImageMask)
	}
	streamActionsIfAny(w, value.Actions, pageIDs)
	streamClips(w, value.Clips, drawParamIDs, fontIDs)
	if value.Border != nil {
		w.Start("Border")
		if value.Border.Color != nil {
			streamColor(w, "BorderColor", value.Border.Color)
		}
		if value.Border.LineWidth != 0 {
			w.AttrFloat("LineWidth", value.Border.LineWidth)
		}
		if value.Border.HorizontalRadius != 0 {
			w.AttrFloat("HorizonalCornerRadius", value.Border.HorizontalRadius)
		}
		if value.Border.VerticalRadius != 0 {
			w.AttrFloat("VerticalCornerRadius", value.Border.VerticalRadius)
		}
		if value.Border.DashOffset != 0 {
			w.AttrFloat("DashOffset", value.Border.DashOffset)
		}
		if len(value.Border.DashPattern) > 0 {
			w.AttrFloatList("DashPattern", value.Border.DashPattern)
		}
		w.End()
	}
	w.End()
}

func streamComposite(w *streamXMLWriter, item builtItem, value Composite, pageIDs []uint64, drawParamIDs, fontIDs map[string]uint64) {
	w.Start("CompositeObject")
	streamGraphicAttributes(w, item.id, value.X, value.Y, value.Width, value.Height, value.Name, value.Visible, item.drawParam)
	if value.CTM != nil {
		w.AttrFloatList("CTM", value.CTM[:])
	}
	w.AttrUint("ResourceID", item.composite)
	streamActionsIfAny(w, value.Actions, pageIDs)
	streamClips(w, value.Clips, drawParamIDs, fontIDs)
	w.End()
}

func streamActionsIfAny(w *streamXMLWriter, values []Action, pageIDs []uint64) {
	if len(values) > 0 {
		streamActions(w, values, pageIDs)
	}
}

func streamActions(w *streamXMLWriter, values []Action, pageIDs []uint64) {
	w.Start("Actions")
	for _, value := range values {
		streamAction(w, value, pageIDs)
	}
	w.End()
}

func streamAction(w *streamXMLWriter, value Action, pageIDs []uint64) {
	w.Start("Action")
	event := value.Event
	if event == "" {
		event = ActionEventClick
	}
	w.Attr("Event", string(event))
	if value.Region != nil {
		streamRegion(w, value.Region)
	}
	if value.URI != nil {
		w.Start("URI")
		w.Attr("URI", value.URI.URI)
		if value.URI.Base != "" {
			w.Attr("Base", value.URI.Base)
		}
		if value.URI.Target != "" {
			w.Attr("Target", value.URI.Target)
		}
		w.End()
	} else if value.GotoA != nil {
		w.Start("GotoA")
		w.Attr("AttachID", value.GotoA.AttachID)
		if value.GotoA.NewWindow != nil {
			w.Attr("NewWindow", strconv.FormatBool(*value.GotoA.NewWindow))
		}
		w.End()
	} else if value.Sound != nil {
		w.Start("Sound")
		w.AttrUint("ResourceID", value.Sound.ResourceID)
		if value.Sound.Volume != nil {
			w.AttrInt("Volume", *value.Sound.Volume)
		}
		if value.Sound.Repeat != nil {
			w.Attr("Repeat", strconv.FormatBool(*value.Sound.Repeat))
		}
		if value.Sound.Synchronous != nil {
			w.Attr("Synchronous", strconv.FormatBool(*value.Sound.Synchronous))
		}
		w.End()
	} else if value.Movie != nil {
		w.Start("Movie")
		w.AttrUint("ResourceID", value.Movie.ResourceID)
		if value.Movie.Operator != "" {
			w.Attr("Operator", value.Movie.Operator)
		}
		w.End()
	} else if value.Goto != nil {
		w.Start("Goto")
		streamGoto(w, *value.Goto, pageIDs)
		w.End()
	}
	w.End()
}

func streamRegion(w *streamXMLWriter, value *ActionRegion) {
	w.Start("Region")
	for _, areaValue := range value.Areas {
		w.Start("Area")
		w.Attr("Start", pointString(areaValue.Start))
		for _, command := range areaValue.Commands {
			switch value := command.(type) {
			case RegionMove:
				w.Start("Move")
				w.Attr("Point1", pointString(value.Point))
				w.End()
			case RegionLine:
				w.Start("Line")
				w.Attr("Point1", pointString(value.Point))
				w.End()
			case RegionQuadraticBezier:
				w.Start("QuadraticBezier")
				w.Attr("Point1", pointString(value.Control))
				w.Attr("Point2", pointString(value.End))
				w.End()
			case RegionCubicBezier:
				w.Start("CubicBezier")
				w.Attr("Point1", pointString(value.Control1))
				w.Attr("Point2", pointString(value.Control2))
				w.Attr("Point3", pointString(value.End))
				w.End()
			case RegionArc:
				w.Start("Arc")
				w.Attr("SweepDirection", strconv.FormatBool(value.SweepDirection))
				w.Attr("LargeArc", strconv.FormatBool(value.LargeArc))
				w.AttrFloat("RotationAngle", value.RotationAngle)
				w.Attr("EllipseSize", pointString(value.EllipseSize))
				w.Attr("EndPoint", pointString(value.EndPoint))
				w.End()
			case RegionClose:
				w.Start("Close")
				w.End()
			}
		}
		w.End()
	}
	w.End()
}

func streamGoto(w *streamXMLWriter, value GotoAction, pageIDs []uint64) {
	if strings.TrimSpace(value.Bookmark) != "" {
		w.Start("Bookmark")
		w.Attr("Name", value.Bookmark)
		w.End()
		return
	}
	destType := value.Type
	if destType == "" {
		destType = "Fit"
	}
	w.Start("Dest")
	w.Attr("Type", destType)
	w.AttrUint("PageID", pageIDs[value.Page])
	streamOptionalNumberAttr(w, "Left", value.Left)
	streamOptionalNumberAttr(w, "Top", value.Top)
	streamOptionalNumberAttr(w, "Right", value.Right)
	streamOptionalNumberAttr(w, "Bottom", value.Bottom)
	streamOptionalNumberAttr(w, "Zoom", value.Zoom)
	w.End()
}

func streamOptionalNumberAttr(w *streamXMLWriter, name string, value *float64) {
	if value != nil {
		w.AttrFloat(name, *value)
	}
}

func streamClips(w *streamXMLWriter, value *Clips, drawParamIDs, fontIDs map[string]uint64) {
	if value == nil || len(value.Items) == 0 {
		return
	}
	w.Start("Clips")
	for _, clip := range value.Items {
		w.Start("Clip")
		for _, area := range clip.Areas {
			w.Start("Area")
			if name := strings.TrimSpace(area.DrawParam); name != "" {
				if id, ok := drawParamIDs[name]; ok {
					w.AttrUint("DrawParam", id)
				}
			}
			if area.CTM != nil {
				w.AttrFloatList("CTM", area.CTM[:])
			}
			if area.Path != nil {
				streamClipPath(w, *area.Path)
			}
			if area.Text != nil {
				streamClipText(w, *area.Text, fontIDs)
			}
			w.End()
		}
		w.End()
	}
	w.End()
}

func streamClipPath(w *streamXMLWriter, value ClipPath) {
	w.Start("Path")
	streamGraphicAttributesWithoutID(w, value.Boundary, value.Name, value.Visible)
	if value.CTM != nil {
		w.AttrFloatList("CTM", value.CTM[:])
	}
	streamGraphicStyle(w, value.LineWidth, value.Cap, value.Join, value.MiterLimit, value.DashOffset, value.DashPattern, value.Alpha)
	if value.StrokeSet != nil {
		w.Attr("Stroke", strconv.FormatBool(*value.StrokeSet))
	} else if value.Stroke {
		w.Attr("Stroke", "true")
	}
	if value.Fill {
		w.Attr("Fill", "true")
	}
	if value.Rule != "" {
		w.Attr("Rule", value.Rule)
	}
	streamColor(w, "StrokeColor", value.StrokeColor)
	streamColor(w, "FillColor", value.FillColor)
	streamElementText(w, "AbbreviatedData", strings.TrimSpace(value.Data))
	w.End()
}

func streamClipText(w *streamXMLWriter, value ClipText, fontIDs map[string]uint64) {
	w.Start("Text")
	streamGraphicAttributesWithoutID(w, value.Boundary, "", nil)
	if value.CTM != nil {
		w.AttrFloatList("CTM", value.CTM[:])
	}
	font := strings.TrimSpace(value.Font)
	if font == "" {
		font = "SimSun"
	}
	w.AttrUint("Font", fontIDs[font])
	size := value.Size
	if size == 0 {
		size = 4.2333333333
	}
	w.AttrFloat("Size", size)
	if value.Stroke {
		w.Attr("Stroke", "true")
	}
	if value.Fill != nil {
		w.Attr("Fill", strconv.FormatBool(*value.Fill))
	}
	if value.HScale != 0 {
		w.AttrFloat("HScale", value.HScale)
	}
	if value.ReadDirection != 0 {
		w.AttrInt("ReadDirection", value.ReadDirection)
	}
	if value.CharDirection != 0 {
		w.AttrInt("CharDirection", value.CharDirection)
	}
	if value.Weight != 0 {
		w.AttrInt("Weight", value.Weight)
	}
	if value.Italic {
		w.Attr("Italic", "true")
	}
	streamColor(w, "FillColor", value.FillColor)
	streamColor(w, "StrokeColor", value.StrokeColor)
	codes := value.TextCodes
	if len(codes) == 0 {
		codes = []TextCode{{Value: value.Value}}
	}
	for _, code := range codes {
		streamTextCode(w, code)
	}
	w.End()
}

func streamColor(w *streamXMLWriter, name string, value *Color) {
	if value == nil {
		return
	}
	w.Start(name)
	if len(value.Components) > 0 {
		w.Attr("Value", intList(value.Components))
	} else if value.Index != nil {
		w.AttrInt("Index", *value.Index)
	} else {
		w.Attr("Value", strconv.Itoa(int(value.R))+" "+strconv.Itoa(int(value.G))+" "+strconv.Itoa(int(value.B)))
	}
	if value.ColorSpace != 0 {
		w.AttrUint("ColorSpace", value.ColorSpace)
	}
	if value.Alpha != nil {
		w.AttrInt("Alpha", int(*value.Alpha))
	}
	if value.Pattern != nil {
		pattern := value.Pattern
		w.Start("Pattern")
		w.Attr("Width", number(pattern.Width))
		w.Attr("Height", number(pattern.Height))
		if pattern.XStep != 0 {
			w.Attr("XStep", number(pattern.XStep))
		}
		if pattern.YStep != 0 {
			w.Attr("YStep", number(pattern.YStep))
		}
		if pattern.ReflectMethod != "" {
			w.Attr("ReflectMethod", pattern.ReflectMethod)
		}
		if pattern.RelativeTo != "" {
			w.Attr("RelativeTo", pattern.RelativeTo)
		}
		if pattern.CTM != nil {
			w.AttrFloatList("CTM", pattern.CTM[:])
		}
		w.Start("CellContent")
		if pattern.Thumbnail != 0 {
			w.AttrUint("Thumbnail", pattern.Thumbnail)
		}
		if pattern.builtState != nil {
			streamBuiltItems(w, pattern.builtItems, pattern.builtState)
		}
		w.End()
		w.End()
	}
	if value.Axial != nil {
		streamShading(w, value.Axial)
	}
	if value.Radial != nil {
		streamShading(w, value.Radial)
	}
	if value.Gouraud != nil {
		streamShading(w, value.Gouraud)
	}
	if value.LaGouraud != nil {
		streamShading(w, value.LaGouraud)
	}
	w.End()
}

func streamShading(w *streamXMLWriter, value interface{}) {
	switch shading := value.(type) {
	case *AxialShading:
		w.Start("AxialShd")
		streamGradientCommon(w, shading.MapType, shading.MapUnit, shading.Extend, shading.StartPoint, shading.EndPoint)
		for _, stop := range shading.Segments {
			w.Start("Segment")
			w.AttrFloat("Position", stop.Position)
			streamColor(w, "Color", &stop.Color)
			w.End()
		}
		w.End()
	case *RadialShading:
		w.Start("RadialShd")
		if shading.MapType != "" {
			w.Attr("MapType", shading.MapType)
		}
		if shading.MapUnit != 0 {
			w.AttrFloat("MapUnit", shading.MapUnit)
		}
		if shading.Eccentricity != 0 {
			w.AttrFloat("Eccentricity", shading.Eccentricity)
		}
		if shading.Angle != 0 {
			w.AttrFloat("Angle", shading.Angle)
		}
		w.Attr("StartPoint", shading.StartPoint)
		if shading.StartRadius != 0 {
			w.AttrFloat("StartRadius", shading.StartRadius)
		}
		w.Attr("EndPoint", shading.EndPoint)
		w.AttrFloat("EndRadius", shading.EndRadius)
		if shading.Extend != 0 {
			w.AttrInt("Extend", shading.Extend)
		}
		for _, stop := range shading.Segments {
			w.Start("Segment")
			w.AttrFloat("Position", stop.Position)
			streamColor(w, "Color", &stop.Color)
			w.End()
		}
		w.End()
	case *GouraudShading:
		w.Start("GouraudShd")
		if shading.Extend != 0 {
			w.AttrInt("Extend", shading.Extend)
		}
		for _, point := range shading.Points {
			w.Start("Point")
			w.AttrFloat("X", point.X)
			w.AttrFloat("Y", point.Y)
			if point.EdgeFlag != 0 {
				w.AttrInt("EdgeFlag", point.EdgeFlag)
			}
			streamColor(w, "Color", &point.Color)
			w.End()
		}
		if shading.BackColor != nil {
			streamColor(w, "BackColor", shading.BackColor)
		}
		w.End()
	case *LaGouraudShading:
		w.Start("LaGourandShd")
		w.AttrInt("VerticesPerRow", shading.VerticesPerRow)
		if shading.Extend != 0 {
			w.AttrInt("Extend", shading.Extend)
		}
		for _, point := range shading.Points {
			w.Start("Point")
			w.AttrFloat("X", point.X)
			w.AttrFloat("Y", point.Y)
			streamColor(w, "Color", &point.Color)
			w.End()
		}
		if shading.BackColor != nil {
			streamColor(w, "BackColor", shading.BackColor)
		}
		w.End()
	}
}

func streamGradientCommon(w *streamXMLWriter, mapType string, mapUnit float64, extend int, start, end string) {
	if mapType != "" {
		w.Attr("MapType", mapType)
	}
	if mapUnit != 0 {
		w.AttrFloat("MapUnit", mapUnit)
	}
	if extend != 0 {
		w.AttrInt("Extend", extend)
	}
	w.Attr("StartPoint", start)
	w.Attr("EndPoint", end)
}
