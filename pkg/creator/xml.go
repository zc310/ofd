package creator

import (
	"strconv"
	"strings"
	"time"

	"github.com/zc310/ofd/internal/ofdxml"
	"github.com/zc310/ofd/pkg/spec"
)

func rootXML(state *buildState) ([]byte, error) {
	w := ofdxml.New()
	w.Start("OFD")
	w.Attr("Version", "1.0")
	w.Attr("DocType", state.docType)
	w.Start("DocBody")
	w.Start("DocInfo")
	w.Element("DocID", state.document.ID)
	streamOptionalText(w, "Title", state.document.Title)
	streamOptionalText(w, "Author", state.document.Author)
	streamOptionalText(w, "Subject", state.document.Subject)
	streamOptionalText(w, "Abstract", state.document.Abstract)
	streamOptionalDate(w, "CreationDate", state.document.CreationDate)
	streamOptionalDate(w, "ModDate", state.document.ModDate)
	streamOptionalText(w, "DocUsage", state.document.DocUsage)
	if state.document.Cover != "" {
		w.Element("Cover", state.document.Cover)
	}
	if len(state.document.Keywords) > 0 {
		w.Start("Keywords")
		for _, keyword := range state.document.Keywords {
			streamOptionalText(w, "Keyword", keyword)
		}
		w.End()
	}
	streamOptionalText(w, "Creator", state.document.Creator)
	streamOptionalText(w, "CreatorVersion", state.document.CreatorVersion)
	if len(state.document.CustomDatas) > 0 {
		w.Start("CustomDatas")
		for _, value := range state.document.CustomDatas {
			w.Start("CustomData")
			w.Attr("Name", value.Name)
			w.Text(value.Value)
			w.End()
		}
		w.End()
	}
	w.End() // DocInfo
	w.Element("DocRoot", docDir+"/Document.xml")
	if len(state.versions) > 0 {
		w.Start("Versions")
		for _, version := range state.versions {
			w.Start("Version")
			w.Attr("ID", version.value.ID)
			w.AttrInt("Index", version.value.Index)
			if version.value.Current {
				w.Attr("Current", "true")
			}
			w.Attr("BaseLoc", "/"+versionPath(version.baseName))
			w.End()
		}
		w.End()
	}
	if len(state.signatures) > 0 {
		w.Element("Signatures", docDir+"/Signatures.xml")
	}
	w.End() // DocBody
	w.End() // OFD
	return w.Bytes(), nil
}

func streamOptionalText(w *ofdxml.Writer, name, value string) {
	if strings.TrimSpace(value) != "" {
		w.Element(name, value)
	}
}

func streamOptionalDate(w *ofdxml.Writer, name string, value time.Time) {
	if !value.IsZero() {
		w.Element(name, value.Format("2006-01-02"))
	}
}

func streamOptionalBool(w *ofdxml.Writer, name string, value *bool) {
	if value != nil {
		w.Element(name, strconv.FormatBool(*value))
	}
}

func documentXML(state *buildState) ([]byte, error) {
	w := ofdxml.New()
	w.Start("Document")
	w.Start("CommonData")
	w.Element("MaxUnitID", strconv.FormatUint(state.maxID, 10))
	w.Start("PageArea")
	streamPageArea(w, state.document.Area, state.pageSize)
	w.End()
	for _, resource := range state.publicResources {
		w.Element("PublicRes", resource.name)
	}
	if len(state.drawParams) > 0 || len(state.fonts) > 0 || len(state.images) > 0 || len(state.media) > 0 || len(state.composites) > 0 || len(state.document.ColorSpaces) > 0 {
		w.Element("DocumentRes", "DocumentRes.xml")
	}
	if state.document.DefaultCS != 0 {
		w.Element("DefaultCS", strconv.FormatUint(state.document.DefaultCS, 10))
	}
	for templateIndex, template := range state.document.Templates {
		w.Start("TemplatePage")
		w.AttrUint("ID", state.templateIDs[templateIndex])
		if template.Name != "" {
			w.Attr("Name", template.Name)
		}
		if template.ZOrder != "" {
			w.Attr("ZOrder", template.ZOrder)
		}
		w.Attr("BaseLoc", templatePath(templateIndex)[len(docDir)+1:])
		w.End()
	}
	w.End() // CommonData
	w.Start("Pages")
	for pageIndex := 0; pageIndex < state.pageCount; pageIndex++ {
		w.Start("Page")
		w.AttrUint("ID", state.pageIDs[pageIndex])
		w.Attr("BaseLoc", pagePath(pageIndex)[len(docDir)+1:])
		w.End()
	}
	w.End()
	if len(state.document.Outlines) > 0 {
		w.Start("Outlines")
		for _, outline := range state.document.Outlines {
			streamOutline(w, outline, state.pageIDs)
		}
		w.End()
	}
	if state.document.Permissions != nil {
		streamPermissions(w, state.document.Permissions)
	}
	streamActionsIfAny(w, state.document.Actions, state.pageIDs)
	if state.document.Preferences != nil {
		streamPreferences(w, state.document.Preferences)
	}
	if len(state.document.Bookmarks) > 0 {
		w.Start("Bookmarks")
		for _, bookmark := range state.document.Bookmarks {
			w.Start("Bookmark")
			w.Attr("Name", bookmark.Name)
			streamGoto(w, bookmark.Goto, state.pageIDs)
			w.End()
		}
		w.End()
	}
	if len(state.annotationPages) > 0 {
		w.Element("Annotations", "Annotations.xml")
	}
	if len(state.customTags) > 0 {
		w.Element("CustomTags", "CustomTags/CustomTags.xml")
	}
	if len(state.attachments) > 0 {
		w.Element("Attachments", "Attachments/Attachments.xml")
	}
	if len(state.extensions) > 0 {
		w.Element("Extensions", "Extensions/Extensions.xml")
	}
	w.End()
	return w.Bytes(), nil
}

func templateXML(state *buildState, templateIndex int, w *streamXMLWriter) ([]byte, error) {
	return streamTemplateXML(state, templateIndex, w), nil
}

func annotationsXML(state *buildState) ([]byte, error) {
	w := ofdxml.New()
	w.Start("Annotations")
	for _, page := range state.annotationPages {
		w.Start("Page")
		w.AttrUint("PageID", page.pageID)
		w.Element("FileLoc", annotationPath(page.pageID)[len(docDir)+1:])
		w.End()
	}
	w.End()
	return w.Bytes(), nil
}

func pageAnnotationsXML(state *buildState, page annotationResource) ([]byte, error) {
	w := ofdxml.New()
	w.Start("PageAnnot")
	for _, annotation := range page.items {
		w.Start("Annot")
		w.AttrUint("ID", annotation.value.ID)
		w.Attr("Type", annotation.value.Type)
		w.Attr("Creator", annotation.value.Creator)
		w.Attr("LastModDate", annotation.value.LastModDate.Format("2006-01-02"))
		if annotation.value.Visible != nil {
			w.Attr("Visible", strconv.FormatBool(*annotation.value.Visible))
		}
		if annotation.value.Subtype != "" {
			w.Attr("Subtype", annotation.value.Subtype)
		}
		if annotation.value.Print != nil {
			w.Attr("Print", strconv.FormatBool(*annotation.value.Print))
		}
		if annotation.value.NoZoom {
			w.Attr("NoZoom", "true")
		}
		if annotation.value.NoRotate {
			w.Attr("NoRotate", "true")
		}
		if annotation.value.ReadOnlyValue != nil {
			w.Attr("ReadOnly", strconv.FormatBool(*annotation.value.ReadOnlyValue))
		} else if annotation.value.ReadOnly {
			w.Attr("ReadOnly", "true")
		}
		streamOptionalText(w, "Remark", annotation.value.Remark)
		if len(annotation.value.Parameters) > 0 {
			w.Start("Parameters")
			for _, parameter := range annotation.value.Parameters {
				w.Start("Parameter")
				w.Attr("Name", parameter.Name)
				w.Text(parameter.Value)
				w.End()
			}
			w.End()
		}
		w.Start("Appearance")
		if box := annotation.value.Boundary; box != nil {
			w.AttrBox("Boundary", box.X, box.Y, box.Width, box.Height)
		}
		streamBuiltItems(w, annotation.items, state)
		w.End()
		w.End()
	}
	w.End()
	return w.Bytes(), nil
}

func pointString(value Point) string {
	return number(value.X) + " " + number(value.Y)
}

func streamOutline(w *ofdxml.Writer, value Outline, pageIDs []uint64) {
	w.Start("OutlineElem")
	w.Attr("Title", value.Title)
	if value.Count != nil {
		w.AttrInt("Count", *value.Count)
	}
	if value.Expanded != nil {
		w.Attr("Expanded", strconv.FormatBool(*value.Expanded))
	}
	streamActionsIfAny(w, value.Actions, pageIDs)
	for _, child := range value.Children {
		streamOutline(w, child, pageIDs)
	}
	w.End()
}

func streamPermissions(w *ofdxml.Writer, value *Permissions) {
	w.Start("Permissions")
	streamOptionalBool(w, "Edit", value.Edit)
	streamOptionalBool(w, "Annot", value.Annot)
	streamOptionalBool(w, "Export", value.Export)
	streamOptionalBool(w, "Signature", value.Signature)
	streamOptionalBool(w, "Watermark", value.Watermark)
	streamOptionalBool(w, "PrintScreen", value.PrintScreen)
	if value.Print != nil {
		w.Start("Print")
		w.Attr("Printable", strconv.FormatBool(value.Print.Printable))
		if value.Print.Copies != nil {
			w.AttrInt("Copies", *value.Print.Copies)
		}
		w.End()
	}
	if value.ValidPeriod != nil {
		w.Start("ValidPeriod")
		if !value.ValidPeriod.Start.IsZero() {
			w.Attr("StartDate", value.ValidPeriod.Start.Format("2006-01-02T15:04:05"))
		}
		if !value.ValidPeriod.End.IsZero() {
			w.Attr("EndDate", value.ValidPeriod.End.Format("2006-01-02T15:04:05"))
		}
		w.End()
	}
	w.End()
}

func streamPreferences(w *ofdxml.Writer, value *ViewPreferences) {
	w.Start("VPreferences")
	streamOptionalText(w, "PageMode", value.PageMode)
	streamOptionalText(w, "PageLayout", value.PageLayout)
	streamOptionalText(w, "TabDisplay", value.TabDisplay)
	streamOptionalBool(w, "HideToolbar", value.HideToolbar)
	streamOptionalBool(w, "HideMenubar", value.HideMenubar)
	streamOptionalBool(w, "HideWindowUI", value.HideWindowUI)
	if value.ZoomMode != "" {
		w.Element("ZoomMode", value.ZoomMode)
	} else if value.Zoom != nil {
		w.Element("Zoom", number(*value.Zoom))
	}
	w.End()
}

func resourceXML(state *buildState) ([]byte, error) {
	w := ofdxml.New()
	w.Start("Res")
	w.Attr("BaseLoc", "Res")
	if len(state.document.ColorSpaces) > 0 {
		w.Start("ColorSpaces")
		for _, space := range state.document.ColorSpaces {
			w.Start("ColorSpace")
			w.AttrUint("ID", space.ID)
			w.Attr("Type", space.Type)
			if space.BitsPerComponent != 0 {
				w.AttrInt("BitsPerComponent", space.BitsPerComponent)
			}
			if space.Profile != "" {
				w.Attr("Profile", space.Profile)
			}
			if len(space.Palette) > 0 {
				w.Start("Palette")
				for _, value := range space.Palette {
					w.Element("CV", value)
				}
				w.End()
			}
			w.End()
		}
		w.End()
	}
	if len(state.drawParams) > 0 {
		w.Start("DrawParams")
		for _, param := range state.drawParams {
			w.Start("DrawParam")
			w.AttrUint("ID", param.id)
			if param.relative != 0 {
				w.AttrUint("Relative", param.relative)
			}
			streamGraphicStyle(w, param.lineWidth, param.cap, param.join, param.miterLimit, param.dashOffset, param.dashPattern, nil)
			streamColor(w, "FillColor", param.fillColor)
			streamColor(w, "StrokeColor", param.strokeColor)
			w.End()
		}
		w.End()
	}
	if len(state.fonts) > 0 {
		w.Start("Fonts")
		for _, font := range state.fonts {
			w.Start("Font")
			w.AttrUint("ID", font.id)
			w.Attr("FontName", font.name)
			w.Attr("Charset", font.charset)
			if font.familyName != "" {
				w.Attr("FamilyName", font.familyName)
			}
			if font.italic {
				w.Attr("Italic", "true")
			}
			if font.bold {
				w.Attr("Bold", "true")
			}
			if font.serif {
				w.Attr("Serif", "true")
			}
			if font.fixedWidth {
				w.Attr("FixedWidth", "true")
			}
			if font.fileName != "" {
				w.Element("FontFile", "Fonts/"+font.fileName)
			}
			w.End()
		}
		w.End()
	}
	if len(state.images) > 0 || len(state.media) > 0 {
		w.Start("MultiMedias")
		for _, image := range state.images {
			w.Start("MultiMedia")
			w.AttrUint("ID", image.id)
			w.Attr("Type", spec.MediaTypeImage)
			w.Attr("Format", image.format)
			w.Element("MediaFile", "Images/"+image.name)
			w.End()
		}
		for _, media := range state.media {
			w.Start("MultiMedia")
			w.AttrUint("ID", media.id)
			w.Attr("Type", media.type_)
			if media.format != "" {
				w.Attr("Format", media.format)
			}
			w.Element("MediaFile", "Media/"+media.name)
			w.End()
		}
		w.End()
	}
	if len(state.composites) > 0 {
		w.Start("CompositeGraphicUnits")
		for _, composite := range state.composites {
			w.Start("CompositeGraphicUnit")
			w.AttrUint("ID", composite.id)
			w.AttrFloat("Width", composite.width)
			w.AttrFloat("Height", composite.height)
			if composite.thumbnail != 0 {
				w.Element("Thumbnail", strconv.FormatUint(composite.thumbnail, 10))
			}
			if composite.substitution != 0 {
				w.Element("Substitution", strconv.FormatUint(composite.substitution, 10))
			}
			w.Start("Content")
			for _, layer := range composite.layers {
				streamBuiltItems(w, layer.items, state)
			}
			w.End()
			w.End()
		}
		w.End()
	}
	w.End()
	return w.Bytes(), nil
}

func pageResourceXML(resource pageResource) ([]byte, error) {
	if len(resource.data) > 0 {
		return append([]byte(nil), resource.data...), nil
	}
	w := ofdxml.New()
	w.Start("Res")
	w.Attr("BaseLoc", ".")
	w.Start("MultiMedias")
	for _, image := range resource.images {
		w.Start("MultiMedia")
		w.AttrUint("ID", image.id)
		w.Attr("Type", spec.MediaTypeImage)
		w.Attr("Format", image.format)
		w.Element("MediaFile", "Images/"+strconv.FormatUint(image.id, 10)+"-"+image.name)
		w.End()
	}
	w.End()
	w.End()
	return w.Bytes(), nil
}

// pathStrokeValue 显式写出 Stroke，防止只填充的路径继承 schema 的描边缺省值 true。
func pathStrokeValue(stroke bool, set *bool) string {
	if set != nil {
		return strconv.FormatBool(*set)
	}
	return strconv.FormatBool(stroke)
}
