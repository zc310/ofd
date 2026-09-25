package creator

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/beevik/etree"
)

func rootXML(state *buildState) ([]byte, error) {
	doc := newXMLDocument()
	root := doc.CreateElement("OFD")
	root.CreateAttr("xmlns", ofNamespace)
	root.CreateAttr("Version", "1.0")
	root.CreateAttr("DocType", "OFD")
	body := root.CreateElement("DocBody")
	info := body.CreateElement("DocInfo")
	info.CreateElement("DocID").SetText(state.document.ID)
	createOptionalText(info, "Title", state.document.Title)
	createOptionalText(info, "Author", state.document.Author)
	createOptionalText(info, "Subject", state.document.Subject)
	createOptionalText(info, "Abstract", state.document.Abstract)
	createOptionalDate(info, "CreationDate", state.document.CreationDate)
	createOptionalDate(info, "ModDate", state.document.ModDate)
	createOptionalText(info, "DocUsage", state.document.DocUsage)
	if state.document.Cover != "" {
		info.CreateElement("Cover").SetText(state.document.Cover)
	}
	if len(state.document.Keywords) > 0 {
		keywords := info.CreateElement("Keywords")
		for _, keyword := range state.document.Keywords {
			if strings.TrimSpace(keyword) != "" {
				keywords.CreateElement("Keyword").SetText(keyword)
			}
		}
	}
	createOptionalText(info, "Creator", state.document.Creator)
	createOptionalText(info, "CreatorVersion", state.document.CreatorVersion)
	if len(state.document.CustomDatas) > 0 {
		customDatas := info.CreateElement("CustomDatas")
		for _, value := range state.document.CustomDatas {
			item := customDatas.CreateElement("CustomData")
			item.CreateAttr("Name", value.Name)
			item.SetText(value.Value)
		}
	}
	body.CreateElement("DocRoot").SetText(docDir + "/Document.xml")
	if len(state.versions) > 0 {
		versions := body.CreateElement("Versions")
		for _, version := range state.versions {
			element := versions.CreateElement("Version")
			element.CreateAttr("ID", version.value.ID)
			element.CreateAttr("Index", strconv.Itoa(version.value.Index))
			if version.value.Current {
				element.CreateAttr("Current", "true")
			}
			element.CreateAttr("BaseLoc", versionPath(version.baseName))
		}
	}
	if len(state.signatures) > 0 {
		body.CreateElement("Signatures").SetText(docDir + "/Signatures.xml")
	}
	return documentBytes(doc)
}

func documentXML(state *buildState) ([]byte, error) {
	doc := newXMLDocument()
	root := doc.CreateElement("Document")
	root.CreateAttr("xmlns", ofNamespace)
	common := root.CreateElement("CommonData")
	common.CreateElement("MaxUnitID").SetText(strconv.FormatUint(state.maxID, 10))
	area := common.CreateElement("PageArea")
	if state.document.Area != nil {
		pageAreaXML(area, state.document.Area, state.pageSize)
	} else {
		area.CreateElement("PhysicalBox").SetText(boxString(0, 0, state.pageSize.Width, state.pageSize.Height))
	}
	for _, resource := range state.publicResources {
		common.CreateElement("PublicRes").SetText(resource.name)
	}
	if len(state.drawParams) > 0 || len(state.fonts) > 0 || len(state.images) > 0 || len(state.media) > 0 || len(state.composites) > 0 || len(state.document.ColorSpaces) > 0 {
		common.CreateElement("DocumentRes").SetText("DocumentRes.xml")
	}
	if state.document.DefaultCS != 0 {
		common.CreateElement("DefaultCS").SetText(strconv.FormatUint(state.document.DefaultCS, 10))
	}
	pages := root.CreateElement("Pages")
	for templateIndex, template := range state.document.Templates {
		element := common.CreateElement("TemplatePage")
		element.CreateAttr("ID", strconv.FormatUint(state.templateIDs[templateIndex], 10))
		if template.Name != "" {
			element.CreateAttr("Name", template.Name)
		}
		if template.ZOrder != "" {
			element.CreateAttr("ZOrder", template.ZOrder)
		}
		element.CreateAttr("BaseLoc", templatePath(templateIndex)[len(docDir)+1:])
	}
	for pageIndex := 0; pageIndex < state.pageCount; pageIndex++ {
		page := pages.CreateElement("Page")
		page.CreateAttr("ID", strconv.FormatUint(state.pageIDs[pageIndex], 10))
		page.CreateAttr("BaseLoc", pagePath(pageIndex)[len(docDir)+1:])
	}
	if len(state.document.Outlines) > 0 {
		outlines := root.CreateElement("Outlines")
		for _, outline := range state.document.Outlines {
			outlineXML(outlines, outline, state.pageIDs)
		}
	}
	if state.document.Permissions != nil {
		permissionsXML(root, state.document.Permissions)
	}
	if len(state.document.Actions) > 0 {
		actions := root.CreateElement("Actions")
		for _, action := range state.document.Actions {
			actionXML(actions, action, state.pageIDs)
		}
	}
	if state.document.Preferences != nil {
		preferencesXML(root, state.document.Preferences)
	}
	if len(state.document.Bookmarks) > 0 {
		bookmarks := root.CreateElement("Bookmarks")
		for _, bookmark := range state.document.Bookmarks {
			element := bookmarks.CreateElement("Bookmark")
			element.CreateAttr("Name", bookmark.Name)
			gotoXML(element, bookmark.Goto, state.pageIDs)
		}
	}
	if len(state.annotationPages) > 0 {
		root.CreateElement("Annotations").SetText("Annotations.xml")
	}
	if len(state.customTags) > 0 {
		root.CreateElement("CustomTags").SetText("CustomTags/CustomTags.xml")
	}
	if len(state.attachments) > 0 {
		root.CreateElement("Attachments").SetText("Attachments/Attachments.xml")
	}
	if len(state.extensions) > 0 {
		root.CreateElement("Extensions").SetText("Extensions/Extensions.xml")
	}
	return documentBytes(doc)
}

func templateXML(state *buildState, templateIndex int, w *streamXMLWriter) ([]byte, error) {
	return streamTemplateXML(state, templateIndex, w), nil
}

func appendBuiltLayers(parent *etree.Element, layers []builtLayer, state *buildState) {
	for _, builtLayer := range layers {
		layer := parent.CreateElement("Layer")
		layer.CreateAttr("ID", strconv.FormatUint(builtLayer.id, 10))
		layer.CreateAttr("Type", builtLayer.layerType)
		if builtLayer.drawParam != 0 {
			layer.CreateAttr("DrawParam", strconv.FormatUint(builtLayer.drawParam, 10))
		}
		appendBuiltItems(layer, builtLayer.items, state)
	}
}

func appendBuiltItems(parent *etree.Element, items []builtItem, state *buildState) {
	for _, item := range items {
		switch value := item.item.(type) {
		case Text:
			textXML(parent, item, value, state.pageIDs, state.drawParamIDs, state.fontIDs)
		case Path:
			pathXML(parent, item, value, state.pageIDs, state.drawParamIDs, state.fontIDs)
		case Image:
			imageXML(parent, item, value, state.pageIDs, state.drawParamIDs, state.fontIDs)
		case Composite:
			compositeXML(parent, item, value, state.pageIDs, state.drawParamIDs, state.fontIDs)
		case PageBlock:
			block := parent.CreateElement("PageBlock")
			block.CreateAttr("ID", strconv.FormatUint(item.id, 10))
			appendBuiltItems(block, item.pageBlock, state)
		}
	}
}

func annotationsXML(state *buildState) ([]byte, error) {
	doc := newXMLDocument()
	root := doc.CreateElement("Annotations")
	root.CreateAttr("xmlns", ofNamespace)
	for _, page := range state.annotationPages {
		element := root.CreateElement("Page")
		element.CreateAttr("PageID", strconv.FormatUint(page.pageID, 10))
		element.CreateElement("FileLoc").SetText(annotationPath(page.pageID)[len(docDir)+1:])
	}
	return documentBytes(doc)
}

func pageAnnotationsXML(state *buildState, page annotationResource) ([]byte, error) {
	doc := newXMLDocument()
	root := doc.CreateElement("PageAnnot")
	root.CreateAttr("xmlns", ofNamespace)
	for index, annotation := range page.items {
		element := root.CreateElement("Annot")
		element.CreateAttr("ID", strconv.FormatUint(annotation.value.ID, 10))
		element.CreateAttr("Type", annotation.value.Type)
		element.CreateAttr("Creator", annotation.value.Creator)
		date := annotation.value.LastModDate
		element.CreateAttr("LastModDate", date.Format("2006-01-02"))
		if annotation.value.Visible != nil {
			element.CreateAttr("Visible", strconv.FormatBool(*annotation.value.Visible))
		}
		if annotation.value.Subtype != "" {
			element.CreateAttr("Subtype", annotation.value.Subtype)
		}
		if annotation.value.Print != nil {
			element.CreateAttr("Print", strconv.FormatBool(*annotation.value.Print))
		}
		if annotation.value.NoZoom {
			element.CreateAttr("NoZoom", "true")
		}
		if annotation.value.NoRotate {
			element.CreateAttr("NoRotate", "true")
		}
		if annotation.value.ReadOnlyValue != nil {
			element.CreateAttr("ReadOnly", strconv.FormatBool(*annotation.value.ReadOnlyValue))
		} else if annotation.value.ReadOnly {
			element.CreateAttr("ReadOnly", "true")
		}
		createOptionalText(element, "Remark", annotation.value.Remark)
		if len(annotation.value.Parameters) > 0 {
			parameters := element.CreateElement("Parameters")
			for _, parameter := range annotation.value.Parameters {
				item := parameters.CreateElement("Parameter")
				item.CreateAttr("Name", parameter.Name)
				item.SetText(parameter.Value)
			}
		}
		appearance := element.CreateElement("Appearance")
		if annotation.value.Boundary != nil {
			appearance.CreateAttr("Boundary", boxString(annotation.value.Boundary.X, annotation.value.Boundary.Y, annotation.value.Boundary.Width, annotation.value.Boundary.Height))
		}
		appendBuiltItems(appearance, annotation.items, state)
		_ = index
	}
	return documentBytes(doc)
}

func pageAreaXML(parent *etree.Element, value *PageArea, pageSize PageSize) {
	physical := value.PhysicalBox
	if physical == nil {
		physical = &Box{Width: pageSize.Width, Height: pageSize.Height}
	}
	createBoxElement(parent, "PhysicalBox", physical)
	if value.ApplicationBox != nil {
		createBoxElement(parent, "ApplicationBox", value.ApplicationBox)
	}
	if value.ContentBox != nil {
		createBoxElement(parent, "ContentBox", value.ContentBox)
	}
	if value.BleedBox != nil {
		createBoxElement(parent, "BleedBox", value.BleedBox)
	}
}

func createBoxElement(parent *etree.Element, name string, value *Box) {
	parent.CreateElement(name).SetText(boxString(value.X, value.Y, value.Width, value.Height))
}

func appendActions(parent *etree.Element, values []Action, pageIDs []uint64) {
	if len(values) == 0 {
		return
	}
	actions := parent.CreateElement("Actions")
	for _, value := range values {
		actionXML(actions, value, pageIDs)
	}
}

func actionXML(parent *etree.Element, value Action, pageIDs []uint64) {
	action := parent.CreateElement("Action")
	event := value.Event
	if event == "" {
		event = ActionEventClick
	}
	action.CreateAttr("Event", string(event))
	if value.Region != nil {
		regionXML(action, value.Region)
	}
	if value.URI != nil {
		uri := action.CreateElement("URI")
		uri.CreateAttr("URI", value.URI.URI)
		if value.URI.Base != "" {
			uri.CreateAttr("Base", value.URI.Base)
		}
		if value.URI.Target != "" {
			uri.CreateAttr("Target", value.URI.Target)
		}
		return
	}
	if value.GotoA != nil {
		gotoA := action.CreateElement("GotoA")
		gotoA.CreateAttr("AttachID", value.GotoA.AttachID)
		if value.GotoA.NewWindow != nil {
			gotoA.CreateAttr("NewWindow", strconv.FormatBool(*value.GotoA.NewWindow))
		}
		return
	}
	if value.Sound != nil {
		sound := action.CreateElement("Sound")
		sound.CreateAttr("ResourceID", strconv.FormatUint(value.Sound.ResourceID, 10))
		if value.Sound.Volume != nil {
			sound.CreateAttr("Volume", strconv.Itoa(*value.Sound.Volume))
		}
		if value.Sound.Repeat != nil {
			sound.CreateAttr("Repeat", strconv.FormatBool(*value.Sound.Repeat))
		}
		if value.Sound.Synchronous != nil {
			sound.CreateAttr("Synchronous", strconv.FormatBool(*value.Sound.Synchronous))
		}
		return
	}
	if value.Movie != nil {
		movie := action.CreateElement("Movie")
		movie.CreateAttr("ResourceID", strconv.FormatUint(value.Movie.ResourceID, 10))
		if value.Movie.Operator != "" {
			movie.CreateAttr("Operator", value.Movie.Operator)
		}
		return
	}
	gotoElement := action.CreateElement("Goto")
	gotoXML(gotoElement, *value.Goto, pageIDs)
}

func regionXML(parent *etree.Element, value *ActionRegion) {
	region := parent.CreateElement("Region")
	for _, areaValue := range value.Areas {
		area := region.CreateElement("Area")
		area.CreateAttr("Start", pointString(areaValue.Start))
		for _, command := range areaValue.Commands {
			switch value := command.(type) {
			case RegionMove:
				element := area.CreateElement("Move")
				element.CreateAttr("Point1", pointString(value.Point))
			case RegionLine:
				element := area.CreateElement("Line")
				element.CreateAttr("Point1", pointString(value.Point))
			case RegionQuadraticBezier:
				element := area.CreateElement("QuadraticBezier")
				element.CreateAttr("Point1", pointString(value.Control))
				element.CreateAttr("Point2", pointString(value.End))
			case RegionCubicBezier:
				element := area.CreateElement("CubicBezier")
				element.CreateAttr("Point1", pointString(value.Control1))
				element.CreateAttr("Point2", pointString(value.Control2))
				element.CreateAttr("Point3", pointString(value.End))
			case RegionArc:
				element := area.CreateElement("Arc")
				element.CreateAttr("SweepDirection", strconv.FormatBool(value.SweepDirection))
				element.CreateAttr("LargeArc", strconv.FormatBool(value.LargeArc))
				element.CreateAttr("RotationAngle", number(value.RotationAngle))
				element.CreateAttr("EllipseSize", pointString(value.EllipseSize))
				element.CreateAttr("EndPoint", pointString(value.EndPoint))
			case RegionClose:
				area.CreateElement("Close")
			}
		}
	}
}

func pointString(value Point) string {
	return number(value.X) + " " + number(value.Y)
}

func gotoXML(parent *etree.Element, value GotoAction, pageIDs []uint64) {
	if strings.TrimSpace(value.Bookmark) != "" {
		bookmark := parent.CreateElement("Bookmark")
		bookmark.CreateAttr("Name", value.Bookmark)
		return
	}
	dest := parent.CreateElement("Dest")
	destType := value.Type
	if destType == "" {
		destType = "Fit"
	}
	dest.CreateAttr("Type", destType)
	dest.CreateAttr("PageID", strconv.FormatUint(pageIDs[value.Page], 10))
	optionalNumberAttr(dest, "Left", value.Left)
	optionalNumberAttr(dest, "Top", value.Top)
	optionalNumberAttr(dest, "Right", value.Right)
	optionalNumberAttr(dest, "Bottom", value.Bottom)
	optionalNumberAttr(dest, "Zoom", value.Zoom)
}

func outlineXML(parent *etree.Element, value Outline, pageIDs []uint64) {
	element := parent.CreateElement("OutlineElem")
	element.CreateAttr("Title", value.Title)
	if value.Count != nil {
		element.CreateAttr("Count", strconv.Itoa(*value.Count))
	}
	if value.Expanded != nil {
		element.CreateAttr("Expanded", strconv.FormatBool(*value.Expanded))
	}
	if len(value.Actions) > 0 {
		actions := element.CreateElement("Actions")
		for _, action := range value.Actions {
			actionXML(actions, action, pageIDs)
		}
	}
	for _, child := range value.Children {
		outlineXML(element, child, pageIDs)
	}
}

func permissionsXML(parent *etree.Element, value *Permissions) {
	element := parent.CreateElement("Permissions")
	optionalBoolElement(element, "Edit", value.Edit)
	optionalBoolElement(element, "Annot", value.Annot)
	optionalBoolElement(element, "Export", value.Export)
	optionalBoolElement(element, "Signature", value.Signature)
	optionalBoolElement(element, "Watermark", value.Watermark)
	optionalBoolElement(element, "PrintScreen", value.PrintScreen)
	if value.Print != nil {
		print := element.CreateElement("Print")
		print.CreateAttr("Printable", strconv.FormatBool(value.Print.Printable))
		if value.Print.Copies != nil {
			print.CreateAttr("Copies", strconv.Itoa(*value.Print.Copies))
		}
	}
	if value.ValidPeriod != nil {
		period := element.CreateElement("ValidPeriod")
		if !value.ValidPeriod.Start.IsZero() {
			period.CreateAttr("StartDate", value.ValidPeriod.Start.Format("2006-01-02T15:04:05"))
		}
		if !value.ValidPeriod.End.IsZero() {
			period.CreateAttr("EndDate", value.ValidPeriod.End.Format("2006-01-02T15:04:05"))
		}
	}
}

func preferencesXML(parent *etree.Element, value *ViewPreferences) {
	element := parent.CreateElement("VPreferences")
	createOptionalText(element, "PageMode", value.PageMode)
	createOptionalText(element, "PageLayout", value.PageLayout)
	createOptionalText(element, "TabDisplay", value.TabDisplay)
	optionalBoolElement(element, "HideToolbar", value.HideToolbar)
	optionalBoolElement(element, "HideMenubar", value.HideMenubar)
	optionalBoolElement(element, "HideWindowUI", value.HideWindowUI)
	if value.ZoomMode != "" {
		element.CreateElement("ZoomMode").SetText(value.ZoomMode)
	} else if value.Zoom != nil {
		element.CreateElement("Zoom").SetText(number(*value.Zoom))
	}
}

func resourceXML(state *buildState) ([]byte, error) {
	doc := newXMLDocument()
	root := doc.CreateElement("Res")
	root.CreateAttr("xmlns", ofNamespace)
	root.CreateAttr("BaseLoc", "Res")
	if len(state.document.ColorSpaces) > 0 {
		spaces := root.CreateElement("ColorSpaces")
		for _, space := range state.document.ColorSpaces {
			element := spaces.CreateElement("ColorSpace")
			element.CreateAttr("ID", strconv.FormatUint(space.ID, 10))
			element.CreateAttr("Type", space.Type)
			if space.BitsPerComponent != 0 {
				element.CreateAttr("BitsPerComponent", strconv.Itoa(space.BitsPerComponent))
			}
			if space.Profile != "" {
				element.CreateAttr("Profile", space.Profile)
			}
			if len(space.Palette) > 0 {
				palette := element.CreateElement("Palette")
				for _, value := range space.Palette {
					palette.CreateElement("CV").SetText(value)
				}
			}
		}
	}
	if len(state.drawParams) > 0 {
		drawParams := root.CreateElement("DrawParams")
		for _, param := range state.drawParams {
			element := drawParams.CreateElement("DrawParam")
			element.CreateAttr("ID", strconv.FormatUint(param.id, 10))
			if param.relative != 0 {
				element.CreateAttr("Relative", strconv.FormatUint(param.relative, 10))
			}
			if param.lineWidth != 0 {
				element.CreateAttr("LineWidth", number(param.lineWidth))
			}
			if param.join != "" {
				element.CreateAttr("Join", param.join)
			}
			if param.cap != "" {
				element.CreateAttr("Cap", param.cap)
			}
			if param.dashOffset != 0 {
				element.CreateAttr("DashOffset", number(param.dashOffset))
			}
			if len(param.dashPattern) > 0 {
				element.CreateAttr("DashPattern", numberList(param.dashPattern))
			}
			if param.miterLimit != 0 {
				element.CreateAttr("MiterLimit", number(param.miterLimit))
			}
			if param.fillColor != nil {
				colorXML(element, "FillColor", param.fillColor)
			}
			if param.strokeColor != nil {
				colorXML(element, "StrokeColor", param.strokeColor)
			}
		}
	}
	if len(state.fonts) > 0 {
		fonts := root.CreateElement("Fonts")
		for _, font := range state.fonts {
			element := fonts.CreateElement("Font")
			element.CreateAttr("ID", strconv.FormatUint(font.id, 10))
			element.CreateAttr("FontName", font.name)
			element.CreateAttr("Charset", font.charset)
			if font.familyName != "" {
				element.CreateAttr("FamilyName", font.familyName)
			}
			if font.italic {
				element.CreateAttr("Italic", "true")
			}
			if font.bold {
				element.CreateAttr("Bold", "true")
			}
			if font.serif {
				element.CreateAttr("Serif", "true")
			}
			if font.fixedWidth {
				element.CreateAttr("FixedWidth", "true")
			}
			if font.fileName != "" {
				element.CreateElement("FontFile").SetText("Fonts/" + font.fileName)
			}
		}
	}
	if len(state.images) > 0 || len(state.media) > 0 {
		medias := root.CreateElement("MultiMedias")
		for _, image := range state.images {
			element := medias.CreateElement("MultiMedia")
			element.CreateAttr("ID", strconv.FormatUint(image.id, 10))
			element.CreateAttr("Type", "Image")
			element.CreateAttr("Format", image.format)
			element.CreateElement("MediaFile").SetText("Images/" + image.name)
		}
		for _, media := range state.media {
			element := medias.CreateElement("MultiMedia")
			element.CreateAttr("ID", strconv.FormatUint(media.id, 10))
			element.CreateAttr("Type", media.type_)
			if media.format != "" {
				element.CreateAttr("Format", media.format)
			}
			element.CreateElement("MediaFile").SetText("Media/" + media.name)
		}
	}
	if len(state.composites) > 0 {
		units := root.CreateElement("CompositeGraphicUnits")
		for _, composite := range state.composites {
			element := units.CreateElement("CompositeGraphicUnit")
			element.CreateAttr("ID", strconv.FormatUint(composite.id, 10))
			element.CreateAttr("Width", number(composite.width))
			element.CreateAttr("Height", number(composite.height))
			if composite.thumbnail != 0 {
				element.CreateElement("Thumbnail").SetText(strconv.FormatUint(composite.thumbnail, 10))
			}
			if composite.substitution != 0 {
				element.CreateElement("Substitution").SetText(strconv.FormatUint(composite.substitution, 10))
			}
			content := element.CreateElement("Content")
			for _, layer := range composite.layers {
				appendBuiltItems(content, layer.items, state)
			}
		}
	}
	return documentBytes(doc)
}

func pageResourceXML(resource pageResource) ([]byte, error) {
	if len(resource.data) > 0 {
		return append([]byte(nil), resource.data...), nil
	}
	doc := newXMLDocument()
	root := doc.CreateElement("Res")
	root.CreateAttr("xmlns", ofNamespace)
	root.CreateAttr("BaseLoc", ".")
	medias := root.CreateElement("MultiMedias")
	for _, image := range resource.images {
		element := medias.CreateElement("MultiMedia")
		element.CreateAttr("ID", strconv.FormatUint(image.id, 10))
		element.CreateAttr("Type", "Image")
		element.CreateAttr("Format", image.format)
		element.CreateElement("MediaFile").SetText("Images/" + strconv.FormatUint(image.id, 10) + "-" + image.name)
	}
	return documentBytes(doc)
}

func textXML(parent *etree.Element, item builtItem, value Text, pageIDs []uint64, drawParamIDs, fontIDs map[string]uint64) {
	element := parent.CreateElement("TextObject")
	setGraphicAttributes(element, item.id, value.X, value.Y, value.Width, value.Height, "", value.Visible, item.drawParam)
	appendActions(element, value.Actions, pageIDs)
	if value.CTM != nil {
		element.CreateAttr("CTM", numberList(value.CTM[:]))
	}
	appendClips(element, value.Clips, drawParamIDs, fontIDs)
	element.CreateAttr("Font", strconv.FormatUint(item.font, 10))
	size := value.Size
	if size == 0 {
		size = 4.2333333333
	}
	element.CreateAttr("Size", number(size))
	if value.Stroke {
		element.CreateAttr("Stroke", "true")
	}
	if value.Fill != nil {
		element.CreateAttr("Fill", strconv.FormatBool(*value.Fill))
	}
	if value.HScale != 0 {
		element.CreateAttr("HScale", number(value.HScale))
	}
	if value.ReadDirection != 0 {
		element.CreateAttr("ReadDirection", strconv.Itoa(value.ReadDirection))
	}
	if value.CharDirection != 0 {
		element.CreateAttr("CharDirection", strconv.Itoa(value.CharDirection))
	}
	if value.Weight != 0 {
		element.CreateAttr("Weight", strconv.Itoa(value.Weight))
	}
	if value.Italic {
		element.CreateAttr("Italic", "true")
	}
	if value.FillColor != nil {
		colorXML(element, "FillColor", value.FillColor)
	}
	if value.StrokeColor != nil {
		colorXML(element, "StrokeColor", value.StrokeColor)
	}
	for _, transform := range value.CGTransforms {
		cgTransformXML(element, transform)
	}
	codes := value.TextCodes
	if len(codes) == 0 {
		codes = []TextCode{{Value: value.Value}}
	}
	for _, code := range codes {
		textCodeXML(element, code)
	}
}

func cgTransformXML(parent *etree.Element, value CGTransform) {
	element := parent.CreateElement("CGTransform")
	element.CreateAttr("CodePosition", strconv.Itoa(value.CodePosition))
	if value.CodeCount != 0 {
		element.CreateAttr("CodeCount", strconv.Itoa(value.CodeCount))
	}
	if value.GlyphCount != 0 {
		element.CreateAttr("GlyphCount", strconv.Itoa(value.GlyphCount))
	}
	if len(value.Glyphs) > 0 {
		element.CreateElement("Glyphs").SetText(intList(value.Glyphs))
	}
}

func textCodeXML(parent *etree.Element, value TextCode) {
	element := parent.CreateElement("TextCode")
	if value.X != nil {
		element.CreateAttr("X", number(*value.X))
	}
	if value.Y != nil {
		element.CreateAttr("Y", number(*value.Y))
	}
	if len(value.DeltaX) > 0 {
		element.CreateAttr("DeltaX", numberList(value.DeltaX))
	}
	if len(value.DeltaY) > 0 {
		element.CreateAttr("DeltaY", numberList(value.DeltaY))
	}
	element.SetText(value.Value)
}

func pathXML(parent *etree.Element, item builtItem, value Path, pageIDs []uint64, drawParamIDs, fontIDs map[string]uint64) {
	element := parent.CreateElement("PathObject")
	setGraphicAttributes(element, item.id, value.X, value.Y, value.Width, value.Height, value.Name, value.Visible, item.drawParam)
	appendActions(element, value.Actions, pageIDs)
	if value.CTM != nil {
		element.CreateAttr("CTM", numberList(value.CTM[:]))
	}
	appendClips(element, value.Clips, drawParamIDs, fontIDs)
	setGraphicStyleAttributes(element, value.LineWidth, value.Cap, value.Join, value.MiterLimit, value.DashOffset, value.DashPattern, value.Alpha)
	if value.StrokeSet != nil {
		element.CreateAttr("Stroke", strconv.FormatBool(*value.StrokeSet))
	} else if value.Stroke {
		element.CreateAttr("Stroke", "true")
	}
	if value.Fill {
		element.CreateAttr("Fill", "true")
	}
	if value.Rule != "" {
		element.CreateAttr("Rule", value.Rule)
	}
	if value.StrokeColor != nil {
		colorXML(element, "StrokeColor", value.StrokeColor)
	}
	if value.FillColor != nil {
		colorXML(element, "FillColor", value.FillColor)
	}
	element.CreateElement("AbbreviatedData").SetText(strings.TrimSpace(value.Data))
}

func imageXML(parent *etree.Element, item builtItem, value Image, pageIDs []uint64, drawParamIDs, fontIDs map[string]uint64) {
	element := parent.CreateElement("ImageObject")
	setGraphicAttributes(element, item.id, value.X, value.Y, value.Width, value.Height, value.Name, value.Visible, item.drawParam)
	appendActions(element, value.Actions, pageIDs)
	if value.CTM != nil {
		element.CreateAttr("CTM", numberList(value.CTM[:]))
	}
	appendClips(element, value.Clips, drawParamIDs, fontIDs)
	setGraphicStyleAttributes(element, value.LineWidth, value.Cap, value.Join, value.MiterLimit, value.DashOffset, value.DashPattern, value.Alpha)
	resourceID := item.image
	if value.ResourceID != 0 {
		resourceID = value.ResourceID
	}
	element.CreateAttr("ResourceID", strconv.FormatUint(resourceID, 10))
	if value.Substitution != 0 {
		element.CreateAttr("Substitution", strconv.FormatUint(value.Substitution, 10))
	}
	if value.ImageMask != 0 {
		element.CreateAttr("ImageMask", strconv.FormatUint(value.ImageMask, 10))
	}
	if value.Border != nil {
		border := element.CreateElement("Border")
		if value.Border.Color != nil {
			colorXML(border, "BorderColor", value.Border.Color)
		}
		if value.Border.LineWidth != 0 {
			border.CreateAttr("LineWidth", number(value.Border.LineWidth))
		}
		if value.Border.HorizontalRadius != 0 {
			border.CreateAttr("HorizonalCornerRadius", number(value.Border.HorizontalRadius))
		}
		if value.Border.VerticalRadius != 0 {
			border.CreateAttr("VerticalCornerRadius", number(value.Border.VerticalRadius))
		}
		if value.Border.DashOffset != 0 {
			border.CreateAttr("DashOffset", number(value.Border.DashOffset))
		}
		if len(value.Border.DashPattern) > 0 {
			border.CreateAttr("DashPattern", numberList(value.Border.DashPattern))
		}
	}
}

func compositeXML(parent *etree.Element, item builtItem, value Composite, pageIDs []uint64, drawParamIDs, fontIDs map[string]uint64) {
	element := parent.CreateElement("CompositeObject")
	setGraphicAttributes(element, item.id, value.X, value.Y, value.Width, value.Height, value.Name, value.Visible, item.drawParam)
	appendActions(element, value.Actions, pageIDs)
	if value.CTM != nil {
		element.CreateAttr("CTM", numberList(value.CTM[:]))
	}
	appendClips(element, value.Clips, drawParamIDs, fontIDs)
	element.CreateAttr("ResourceID", strconv.FormatUint(item.composite, 10))
}

func appendClips(parent *etree.Element, value *Clips, drawParamIDs, fontIDs map[string]uint64) {
	if value == nil || len(value.Items) == 0 {
		return
	}
	element := parent.CreateElement("Clips")
	for _, clip := range value.Items {
		clipXML(element, clip, drawParamIDs, fontIDs)
	}
}

func clipXML(parent *etree.Element, value Clip, drawParamIDs, fontIDs map[string]uint64) {
	element := parent.CreateElement("Clip")
	for _, area := range value.Areas {
		areaElement := element.CreateElement("Area")
		if name := strings.TrimSpace(area.DrawParam); name != "" {
			if id, ok := drawParamIDs[name]; ok {
				areaElement.CreateAttr("DrawParam", strconv.FormatUint(id, 10))
			}
		}
		if area.CTM != nil {
			areaElement.CreateAttr("CTM", numberList(area.CTM[:]))
		}
		if area.Path != nil {
			clipPathXML(areaElement, *area.Path)
		}
		if area.Text != nil {
			clipTextXML(areaElement, *area.Text, fontIDs)
		}
	}
}

func clipPathXML(parent *etree.Element, value ClipPath) {
	element := parent.CreateElement("Path")
	setGraphicAttributesWithoutID(element, value.Boundary, value.Name, value.Visible)
	if value.CTM != nil {
		element.CreateAttr("CTM", numberList(value.CTM[:]))
	}
	setGraphicStyleAttributes(element, value.LineWidth, value.Cap, value.Join, value.MiterLimit, value.DashOffset, value.DashPattern, value.Alpha)
	if value.StrokeSet != nil {
		element.CreateAttr("Stroke", strconv.FormatBool(*value.StrokeSet))
	} else if value.Stroke {
		element.CreateAttr("Stroke", "true")
	}
	if value.Fill {
		element.CreateAttr("Fill", "true")
	}
	if value.Rule != "" {
		element.CreateAttr("Rule", value.Rule)
	}
	if value.StrokeColor != nil {
		colorXML(element, "StrokeColor", value.StrokeColor)
	}
	if value.FillColor != nil {
		colorXML(element, "FillColor", value.FillColor)
	}
	element.CreateElement("AbbreviatedData").SetText(strings.TrimSpace(value.Data))
}

func clipTextXML(parent *etree.Element, value ClipText, fontIDs map[string]uint64) {
	element := parent.CreateElement("Text")
	setGraphicAttributesWithoutID(element, value.Boundary, "", nil)
	if value.CTM != nil {
		element.CreateAttr("CTM", numberList(value.CTM[:]))
	}
	font := strings.TrimSpace(value.Font)
	if font == "" {
		font = "SimSun"
	}
	element.CreateAttr("Font", strconv.FormatUint(fontIDs[font], 10))
	size := value.Size
	if size == 0 {
		size = 4.2333333333
	}
	element.CreateAttr("Size", number(size))
	if value.Stroke {
		element.CreateAttr("Stroke", "true")
	}
	if value.Fill != nil {
		element.CreateAttr("Fill", strconv.FormatBool(*value.Fill))
	}
	if value.HScale != 0 {
		element.CreateAttr("HScale", number(value.HScale))
	}
	if value.ReadDirection != 0 {
		element.CreateAttr("ReadDirection", strconv.Itoa(value.ReadDirection))
	}
	if value.CharDirection != 0 {
		element.CreateAttr("CharDirection", strconv.Itoa(value.CharDirection))
	}
	if value.Weight != 0 {
		element.CreateAttr("Weight", strconv.Itoa(value.Weight))
	}
	if value.Italic {
		element.CreateAttr("Italic", "true")
	}
	if value.FillColor != nil {
		colorXML(element, "FillColor", value.FillColor)
	}
	if value.StrokeColor != nil {
		colorXML(element, "StrokeColor", value.StrokeColor)
	}
	codes := value.TextCodes
	if len(codes) == 0 {
		codes = []TextCode{{Value: value.Value}}
	}
	for _, code := range codes {
		textCodeXML(element, code)
	}
}

func colorXML(parent *etree.Element, name string, value *Color) {
	element := parent.CreateElement(name)
	if len(value.Components) > 0 {
		element.CreateAttr("Value", intList(value.Components))
	} else if value.Index != nil {
		element.CreateAttr("Index", strconv.Itoa(*value.Index))
	} else {
		element.CreateAttr("Value", fmt.Sprintf("%d %d %d", value.R, value.G, value.B))
	}
	if value.ColorSpace != 0 {
		element.CreateAttr("ColorSpace", strconv.FormatUint(value.ColorSpace, 10))
	}
	if value.Alpha != nil {
		element.CreateAttr("Alpha", strconv.Itoa(int(*value.Alpha)))
	}
	if value.Pattern != nil {
		pattern := element.CreateElement("Pattern")
		pattern.CreateAttr("Width", number(value.Pattern.Width))
		pattern.CreateAttr("Height", number(value.Pattern.Height))
		if value.Pattern.XStep != 0 {
			pattern.CreateAttr("XStep", number(value.Pattern.XStep))
		}
		if value.Pattern.YStep != 0 {
			pattern.CreateAttr("YStep", number(value.Pattern.YStep))
		}
		if value.Pattern.ReflectMethod != "" {
			pattern.CreateAttr("ReflectMethod", value.Pattern.ReflectMethod)
		}
		if value.Pattern.RelativeTo != "" {
			pattern.CreateAttr("RelativeTo", value.Pattern.RelativeTo)
		}
		if value.Pattern.CTM != nil {
			pattern.CreateAttr("CTM", numberList(value.Pattern.CTM[:]))
		}
		content := pattern.CreateElement("CellContent")
		if value.Pattern.Thumbnail != 0 {
			content.CreateAttr("Thumbnail", strconv.FormatUint(value.Pattern.Thumbnail, 10))
		}
		if value.Pattern.builtState != nil {
			appendBuiltItems(content, value.Pattern.builtItems, value.Pattern.builtState)
		}
	}
	if value.Axial != nil {
		shadingXML(element, value.Axial)
	}
	if value.Radial != nil {
		shadingXML(element, value.Radial)
	}
	if value.Gouraud != nil {
		shadingXML(element, value.Gouraud)
	}
	if value.LaGouraud != nil {
		shadingXML(element, value.LaGouraud)
	}
}

func shadingXML(parent *etree.Element, value interface{}) {
	switch shading := value.(type) {
	case *AxialShading:
		element := parent.CreateElement("AxialShd")
		gradientCommonXML(element, shading.MapType, shading.MapUnit, shading.Extend, shading.StartPoint, shading.EndPoint)
		for _, stop := range shading.Segments {
			segment := element.CreateElement("Segment")
			if stop.PositionSet {
				segment.CreateAttr("Position", number(stop.Position))
			}
			colorXML(segment, "Color", &stop.Color)
		}
	case *RadialShading:
		element := parent.CreateElement("RadialShd")
		if shading.MapType != "" {
			element.CreateAttr("MapType", shading.MapType)
		}
		if shading.MapUnit != 0 {
			element.CreateAttr("MapUnit", number(shading.MapUnit))
		}
		if shading.Eccentricity != 0 {
			element.CreateAttr("Eccentricity", number(shading.Eccentricity))
		}
		if shading.Angle != 0 {
			element.CreateAttr("Angle", number(shading.Angle))
		}
		element.CreateAttr("StartPoint", shading.StartPoint)
		if shading.StartRadius != 0 {
			element.CreateAttr("StartRadius", number(shading.StartRadius))
		}
		element.CreateAttr("EndPoint", shading.EndPoint)
		element.CreateAttr("EndRadius", number(shading.EndRadius))
		if shading.Extend != 0 {
			element.CreateAttr("Extend", strconv.Itoa(shading.Extend))
		}
		for _, stop := range shading.Segments {
			segment := element.CreateElement("Segment")
			if stop.PositionSet {
				segment.CreateAttr("Position", number(stop.Position))
			}
			colorXML(segment, "Color", &stop.Color)
		}
	case *GouraudShading:
		element := parent.CreateElement("GouraudShd")
		if shading.Extend != 0 {
			element.CreateAttr("Extend", strconv.Itoa(shading.Extend))
		}
		for _, point := range shading.Points {
			pointElement := element.CreateElement("Point")
			pointElement.CreateAttr("X", number(point.X))
			pointElement.CreateAttr("Y", number(point.Y))
			if point.EdgeFlag != 0 {
				pointElement.CreateAttr("EdgeFlag", strconv.Itoa(point.EdgeFlag))
			}
			colorXML(pointElement, "Color", &point.Color)
		}
		if shading.BackColor != nil {
			colorXML(element, "BackColor", shading.BackColor)
		}
	case *LaGouraudShading:
		element := parent.CreateElement("LaGourandShd")
		element.CreateAttr("VerticesPerRow", strconv.Itoa(shading.VerticesPerRow))
		if shading.Extend != 0 {
			element.CreateAttr("Extend", strconv.Itoa(shading.Extend))
		}
		for _, point := range shading.Points {
			pointElement := element.CreateElement("Point")
			pointElement.CreateAttr("X", number(point.X))
			pointElement.CreateAttr("Y", number(point.Y))
			colorXML(pointElement, "Color", &point.Color)
		}
		if shading.BackColor != nil {
			colorXML(element, "BackColor", shading.BackColor)
		}
	}
}

func gradientCommonXML(element *etree.Element, mapType string, mapUnit float64, extend int, start, end string) {
	if mapType != "" {
		element.CreateAttr("MapType", mapType)
	}
	if mapUnit != 0 {
		element.CreateAttr("MapUnit", number(mapUnit))
	}
	if extend != 0 {
		element.CreateAttr("Extend", strconv.Itoa(extend))
	}
	element.CreateAttr("StartPoint", start)
	element.CreateAttr("EndPoint", end)
}

func setGraphicAttributesWithoutID(element *etree.Element, boundary Box, name string, visible *bool) {
	element.CreateAttr("Boundary", boxString(boundary.X, boundary.Y, boundary.Width, boundary.Height))
	if name != "" {
		element.CreateAttr("Name", name)
	}
	if visible != nil {
		element.CreateAttr("Visible", strconv.FormatBool(*visible))
	}
}

func setGraphicAttributes(element *etree.Element, id uint64, x, y, width, height float64, name string, visible *bool, drawParam uint64) {
	element.CreateAttr("ID", strconv.FormatUint(id, 10))
	element.CreateAttr("Boundary", boxString(x, y, width, height))
	if name != "" {
		element.CreateAttr("Name", name)
	}
	if visible != nil {
		element.CreateAttr("Visible", strconv.FormatBool(*visible))
	}
	if drawParam != 0 {
		element.CreateAttr("DrawParam", strconv.FormatUint(drawParam, 10))
	}
}

func setGraphicStyleAttributes(element *etree.Element, lineWidth float64, cap, join string, miterLimit, dashOffset float64, dashPattern []float64, alpha *uint8) {
	if lineWidth != 0 {
		element.CreateAttr("LineWidth", number(lineWidth))
	}
	if cap != "" {
		element.CreateAttr("Cap", cap)
	}
	if join != "" {
		element.CreateAttr("Join", join)
	}
	if miterLimit != 0 {
		element.CreateAttr("MiterLimit", number(miterLimit))
	}
	if dashOffset != 0 {
		element.CreateAttr("DashOffset", number(dashOffset))
	}
	if len(dashPattern) > 0 {
		element.CreateAttr("DashPattern", numberList(dashPattern))
	}
	if alpha != nil {
		element.CreateAttr("Alpha", strconv.Itoa(int(*alpha)))
	}
}
