package creator

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"image"
	"image/png"
	"path"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/pkg/spec"
)

// 使用真实创建链写出 ZIP/XML，再用公开模型与内部快速入口分别解析，避免只验证编码器自洽。
func TestCreatorOutputDecodesThroughPublicPageAndResourceModels(t *testing.T) {
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	matrix := CTM{1, 0, 0, 1, 2, 3}
	data, err := Marshal(Document{
		ID: "spec-models", PageSize: A4,
		Fonts:       []Font{{Name: "logical", FontName: "测试逻辑字体", Charset: "unicode", FixedWidth: true}},
		ColorSpaces: []ColorSpace{{ID: 40, Type: "RGB", BitsPerComponent: 16, Palette: []string{"65535 0 0", "0 65535 0"}}},
		DrawParams: []DrawParam{
			{Name: "base", LineWidth: 2, DashOffset: 3, MiterLimit: 4, FillColor: &Color{ColorSpace: 40, Components: []int{65535, 0, 0}}},
			{Name: "child", Relative: "base"},
		},
		Composites: []CompositeGraphicUnit{{ID: 41, Width: 10, Height: 10, Items: []Item{Path{Width: 10, Height: 10, Data: "M 0 0 L 1 1"}}}},
		Templates:  []TemplatePage{{ID: 42, ZOrder: "Background", Items: []Item{Path{Width: 10, Height: 10, Data: "M 0 0 L 2 2"}}}},
		Pages: []Page{{Templates: []TemplateRef{{ID: 42}}, Layers: []Layer{
			{Type: "Body", DrawParam: "child", Items: []Item{
				Text{Width: 30, Height: 10, Value: "甲 乙", Font: "logical", Size: 3, Fill: new(false), Visible: new(false), CTM: &matrix},
				Image{Width: 10, Height: 10, Data: picture.Bytes(), Format: "PNG", Border: &ImageBorder{LineWidth: new(0.0)}},
				Path{Width: 10, Height: 10, Data: "M 0.123456789 0 L 10 0 C", Fill: true, Alpha: new(uint8(0)), FillColor: &Color{Axial: &AxialShading{
					StartPoint: "0 0", EndPoint: "10 0", Segments: []ColorStop{
						{Position: 0, PositionSet: true, Color: Color{R: 255}}, {Position: 1, PositionSet: true, Color: Color{B: 255}},
					},
				}}},
				PageBlock{Items: []Item{Composite{Width: 10, Height: 10, ResourceID: 41}}},
			}},
			{Type: "Foreground", Items: []Item{Path{Width: 10, Height: 10, Data: "M 0 0 L 3 3"}}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGeneratedPackage(t, data)
	var root spec.OFD
	if err := xml.Unmarshal(readArchiveEntry(t, data, spec.RootDocument), &root); err != nil {
		t.Fatal(err)
	}
	docPath := strings.TrimPrefix(root.DocBodies[0].DocRoot.String(), "/")
	var document spec.Document
	if err := xml.Unmarshal(readArchiveEntry(t, data, docPath), &document); err != nil {
		t.Fatal(err)
	}
	pageXML := readArchiveEntry(t, data, path.Join(path.Dir(docPath), document.Pages.Pages[0].BaseLoc.String()))
	var page spec.PageContent
	if err := xml.Unmarshal(pageXML, &page); err != nil {
		t.Fatal(err)
	}
	fastPage, err := models.ParsePageContentXML(pageXML)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(spec.PageContent(*fastPage), page) {
		t.Fatal("creator 页面在公开解码与快速解码间不一致")
	}
	if len(page.Content.Layer) != 2 || page.Content.Layer[0].Type != "Body" || page.Content.Layer[1].Type != "Foreground" || page.Template[0].TemplateID != 42 {
		t.Fatal("真实创建链的图层顺序或模板引用丢失")
	}
	items := page.Content.Layer[0].Items
	if len(items) != 4 || items[0].Kind != spec.PageItemText || items[1].Kind != spec.PageItemImage || items[2].Kind != spec.PageItemPath || items[3].Kind != spec.PageItemBlock {
		t.Fatal("真实创建链的混合图元顺序改变")
	}
	text := items[0].Text
	if text.TextCode[0].Value != "甲 乙" || text.Fill.Value(true) || text.VisibleValue() || text.CTM == nil || *text.CTM != (spec.CTM{1, 0, 0, 1, 2, 3}) {
		t.Fatalf("真实创建链的正文、可选标志或矩阵改变: 正文=%q Fill=%v Visible=%v CTM=%v", text.TextCode[0].Value, text.Fill.Value(true), text.VisibleValue(), text.CTM)
	}
	if items[1].Image.Border.LineWidth == nil || *items[1].Image.Border.LineWidth != 0 || items[2].Path.Alpha == nil || *items[2].Path.Alpha != 0 {
		t.Fatal("真实创建链的边框零线宽或透明度0丢失")
	}
	if items[2].Path.AbbreviatedData[0].Points[0].X != 0.123456789 || !items[2].Path.FillColor.AxialShd.Segment[0].PositionSet || items[3].Block.Items[0].Composite.ResourceID != 41 {
		t.Fatal("真实创建链的路径精度、渐变位置或嵌套复合引用改变")
	}
	resourceXML := readArchiveEntry(t, data, path.Join(path.Dir(docPath), document.CommonData.DocumentRes[0].String()))
	var resource spec.Res
	if err := xml.Unmarshal(resourceXML, &resource); err != nil {
		t.Fatal(err)
	}
	fastResource, err := models.ParseResXML(resourceXML)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fastResource, &resource) {
		t.Fatal("creator 资源在公开解码与快速解码间不一致")
	}
	if !resource.Fonts.Font[0].FixedWidth || resource.Fonts.Font[0].FontFile != "" || resource.ColorSpaces.ColorSpace[0].BitsPerComponent != 16 || resource.ColorSpaces.ColorSpace[0].Palette.CV[1][1] != "65535" {
		t.Fatal("真实创建链的逻辑字体或高位深调色板改变")
	}
	params := resource.DrawParams.DrawParam
	if params[1].Relative != spec.StRefID(params[0].ID) || page.Content.Layer[0].DrawParam != spec.StRefID(params[1].ID) {
		t.Fatal("真实创建链的绘制参数继承引用或图层资源绑定改变")
	}
	if resource.CompositeGraphicUnits.CompositeGraphicUnit[0].Content.Items[0].Kind != spec.PageItemPath {
		t.Fatal("复合资源图元未解码")
	}
}

// 签名值仅作为不透明测试数据；这里验证创建产物的声明和摘要，不验证密码学签名。
func TestCreatorOutputDecodesThroughPublicAnnotationAndSignatureModels(t *testing.T) {
	date := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	data, err := Marshal(Document{
		ID: "spec-annotation-signature", PageSize: A4, Pages: []Page{{}},
		Annotations: []AnnotationPage{{Page: 0, Items: []Annotation{
			{
				ID: 20, Type: "Path", Creator: "测试", LastModDate: date,
				Visible: new(false), Print: new(false), ReadOnlyValue: new(false),
				Remark: "迁移回归", Parameters: []AnnotationParameter{{Name: "purpose", Value: "测试"}},
				Boundary: &Box{X: 1, Y: 2, Width: 30, Height: 40},
				Items: []Item{
					Path{Width: 10, Height: 10, Data: "M 0 0 L 1 1"},
					PageBlock{Items: []Item{Path{Width: 10, Height: 10, Data: "M 0 0 L 2 2"}}},
				},
			},
			{ID: 21, Type: "Stamp", Creator: "测试", LastModDate: date},
		}}},
		Signatures: []Signature{{
			ID: "sig-models", Type: spec.SigTypeSign, ProviderName: "测试组件", ProviderVersion: "1", Company: "测试厂商",
			Method: "RSA", Date: date, CheckMethod: "SHA256",
			References: []SignatureReference{{FileRef: "../Document.xml"}},
			StampAnnots: []SignatureStamp{{
				ID: "stamp-models", Page: 0, Boundary: Box{X: 10, Y: 20, Width: 30, Height: 40},
				Clip: &Box{X: 1, Y: 2, Width: 3, Height: 4},
			}},
			SignedValue: []byte("opaque-signature-value"), SignedValueName: "value.dat",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGeneratedPackage(t, data)
	var root spec.OFD
	if err := xml.Unmarshal(readArchiveEntry(t, data, spec.RootDocument), &root); err != nil {
		t.Fatal(err)
	}
	body := root.DocBodies[0]
	docPath := strings.TrimPrefix(body.DocRoot.String(), "/")
	var document spec.Document
	if err := xml.Unmarshal(readArchiveEntry(t, data, docPath), &document); err != nil {
		t.Fatal(err)
	}
	docDir := spec.StLoc(path.Dir(docPath))
	var annotations spec.Annotations
	if err := xml.Unmarshal(readArchiveEntry(t, data, document.Annotations.Resolve(docDir).String()), &annotations); err != nil {
		t.Fatal(err)
	}
	annotationPath := annotations.Pages[0].FileLoc.Resolve(document.Annotations.Resolve(docDir).Dir())
	var page spec.PageAnnot
	if err := xml.Unmarshal(readArchiveEntry(t, data, annotationPath.String()), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Annots) != 2 || page.Annots[0].ID != "20" || page.Annots[1].ID != "21" || annotations.Pages[0].PageID != spec.StRefID(document.Pages.Pages[0].ID) {
		t.Fatal("真实创建链的注释顺序或页面引用改变")
	}
	annot := page.Annots[0]
	if !annot.Visible.IsSet() || annot.Visible.Value(true) || !annot.Print.IsSet() || annot.Print.Value(true) || !annot.ReadOnly.IsSet() || annot.ReadOnly.Value(true) {
		t.Fatal("真实创建链的注释显式 false 丢失")
	}
	if annot.Remark == nil || *annot.Remark != "迁移回归" || annot.Parameters.Parameters[0].Value != "测试" || annot.LastModDate.Format("2006-01-02") != "2026-01-02" {
		t.Fatal("真实创建链的注释正文、参数或日期改变")
	}
	appearance := annot.Appearance
	if appearance.Boundary == nil || *appearance.Boundary != (spec.StBox{X: 1, Y: 2, Width: 30, Height: 40}) || len(appearance.Items) != 2 || appearance.Items[0].Kind != spec.PageItemPath || appearance.Items[1].Kind != spec.PageItemBlock || appearance.Items[1].Block.Items[0].Kind != spec.PageItemPath {
		t.Fatal("真实创建链的注释外观边界或混合对象顺序改变")
	}
	var index spec.Signatures
	indexPath := body.Signatures.Resolve("/")
	if err := xml.Unmarshal(readArchiveEntry(t, data, strings.TrimPrefix(indexPath.String(), "/")), &index); err != nil {
		t.Fatal(err)
	}
	if len(index.Signatures) != 1 || index.Signatures[0].ID != "sig-models" || index.Signatures[0].Type != spec.SigTypeSign {
		t.Fatalf("真实创建链的签名索引改变: %+v", index)
	}
	signaturePath := index.Signatures[0].BaseLoc.Resolve(indexPath.Dir())
	var signature spec.Signature
	if err := xml.Unmarshal(readArchiveEntry(t, data, strings.TrimPrefix(signaturePath.String(), "/")), &signature); err != nil {
		t.Fatal(err)
	}
	info := signature.SignedInfo
	if info.Provider.ProviderName != "测试组件" || info.Provider.Version != "1" || info.Provider.Company != "测试厂商" || info.SignatureMethod != "RSA" || info.References.CheckMethod != "SHA256" {
		t.Fatal("真实创建链的签名组件或算法声明改变")
	}
	digest := sha256.Sum256(readArchiveEntry(t, data, docPath))
	if len(info.References.Reference) != 1 || string(info.References.Reference[0].CheckValue) != base64.StdEncoding.EncodeToString(digest[:]) {
		t.Fatal("真实创建链的摘要应保存为 Base64 文本")
	}
	if len(info.StampAnnot) != 1 || info.StampAnnot[0].PageRef != spec.StRefID(document.Pages.Pages[0].ID) || info.StampAnnot[0].Clip != (spec.StBox{X: 1, Y: 2, Width: 3, Height: 4}) {
		t.Fatal("真实创建链的签名外观引用或裁剪改变")
	}
	if value := readArchiveEntry(t, data, strings.TrimPrefix(signature.SignedValue.Resolve(signaturePath.Dir()).String(), "/")); string(value) != "opaque-signature-value" {
		t.Fatal("签名值路径未指向原始数据")
	}
	ofd := newTestOFD(t, data)
	defer ofd.Close()
	loaded := ofd.Documents[0].GetSignature("sig-models")
	if loaded == nil || !reflect.DeepEqual(loaded.Signature, signature) || loaded.Type != spec.SigTypeSign || loaded.FilePath == "" {
		t.Fatal("内部加载上下文与公开标准声明未正确分离")
	}
	if !reflect.DeepEqual(ofd.Documents[0].GetAnnotation(spec.StID(annotations.Pages[0].PageID)), &page) {
		t.Fatal("公开注释解码与内部读取不一致")
	}
}

func TestCreatorOutputDecodesThroughPublicExtensionModels(t *testing.T) {
	const inlineXML = `前<v:Config xmlns:v="urn:vendor" v:key="a">子内容&amp;</v:Config><!--保留-->尾`
	const text = "甲 <配置> & 尾"
	date := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("UTC+8", 8*60*60))
	data, err := Marshal(Document{
		ID: "spec-extensions", Pages: []Page{{}}, Extensions: []Extension{
			{AppName: "text", Company: "测试厂商", AppVersion: "2", Date: date, RefID: 1, Data: text,
				Properties: []ExtensionProperty{{Name: "mode", Type: "string", Value: "a&b"}}},
			{AppName: "xml", RefID: 1, DataXML: []byte(inlineXML)},
			{AppName: "external", RefID: 1, DataFile: []byte("扩展文件"), DataName: "extension.bin"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGeneratedPackage(t, data)
	var root spec.OFD
	if err := xml.Unmarshal(readArchiveEntry(t, data, spec.RootDocument), &root); err != nil {
		t.Fatal(err)
	}
	docPath := strings.TrimPrefix(root.DocBodies[0].DocRoot.String(), "/")
	var document spec.Document
	if err := xml.Unmarshal(readArchiveEntry(t, data, docPath), &document); err != nil {
		t.Fatal(err)
	}
	extensionsPath := document.Extensions.Resolve(spec.StLoc(path.Dir(docPath)))
	var extensions spec.Extensions
	if err := xml.Unmarshal(readArchiveEntry(t, data, extensionsPath.String()), &extensions); err != nil {
		t.Fatal(err)
	}
	if len(extensions.Extensions) != 3 {
		t.Fatalf("扩展数量改变: %+v", extensions)
	}
	plain, inline, external := extensions.Extensions[0], extensions.Extensions[1], extensions.Extensions[2]
	if plain.AppName != "text" || plain.Company == nil || *plain.Company != "测试厂商" || plain.AppVersion == nil || *plain.AppVersion != "2" || plain.RefID != 1 || plain.Date == nil || !plain.Date.Equal(date) {
		t.Fatalf("真实创建链的扩展属性改变: %+v", plain)
	}
	if len(plain.Properties) != 1 || plain.Properties[0].Value != "a&b" || plain.Data == nil || plain.Data.Text != text || !strings.Contains(plain.Data.InnerXML, "&lt;") {
		t.Fatal("真实创建链的扩展属性或转义文本丢失")
	}
	if inline.AppName != "xml" || inline.Data == nil || inline.Data.InnerXML != inlineXML || inline.Data.Text != "前尾" {
		t.Fatalf("真实创建链的混合 XML 丢失: %+v", inline.Data)
	}
	if external.AppName != "external" || external.Data != nil || external.ExtendData == nil {
		t.Fatalf("真实创建链的外部扩展引用改变: %+v", external)
	}
	if got := readArchiveEntry(t, data, external.ExtendData.Resolve(extensionsPath.Dir()).String()); string(got) != "扩展文件" {
		t.Fatal("扩展路径未指向外部扩展数据")
	}
	ofd := newTestOFD(t, data)
	defer ofd.Close()
	loaded, err := ofd.Documents[0].LoadExtensions()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, &extensions) {
		t.Fatal("公开扩展模型与内部加载结果不同")
	}
}

// 文档与附加结构通过唯一创建链写出，公开模型不需要为这类回归自行编码。
func TestCreatorOutputDecodesThroughPublicDocumentAndMetadataModels(t *testing.T) {
	date := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, tc := range []struct {
		name        string
		preferences ViewPreferences
	}{
		{name: "缩放模式", preferences: ViewPreferences{ZoomMode: spec.ZoomModeFitWidth, HideToolbar: new(false)}},
		{name: "缩放比例", preferences: ViewPreferences{Zoom: new(1.25), HideToolbar: new(false)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := Marshal(Document{
				ID: "spec-document-metadata", PageSize: A4, Pages: []Page{{}, {}}, Preferences: &tc.preferences,
				Attachments: []Attachment{
					{ID: "note", Name: "说明文本", FileName: "note.txt", Format: "txt", CreationDate: date, ModDate: date, Visible: new(false), Data: []byte("说明")},
					{ID: "extra", Name: "附加文本", FileName: "extra.txt", Data: []byte("附加")},
				},
				CustomTags: []CustomTag{
					{NameSpace: "urn:first", SchemaName: "first.xsd", Schema: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`), DataName: "first.xml", Data: []byte(`<Tags xmlns="urn:first"/>`)},
					{NameSpace: "urn:second", DataName: "second.xml", Data: []byte(`<Tags xmlns="urn:second"/>`)},
				},
				Annotations: []AnnotationPage{{Page: 0, Items: []Annotation{{ID: 9, Type: "Link", Creator: "测试", LastModDate: date}}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			checkGeneratedPackage(t, data)
			decode := func(name spec.StLoc, target any) {
				t.Helper()
				if err := xml.Unmarshal(readArchiveEntry(t, data, strings.TrimPrefix(name.String(), "/")), target); err != nil {
					t.Fatal(err)
				}
			}
			var root spec.OFD
			decode(spec.RootDocument, &root)
			docPath := root.DocBodies[0].DocRoot.Resolve("/")
			var document spec.Document
			decode(docPath, &document)
			if document.XMLName.Space != spec.Namespace || len(document.Pages.Pages) != 2 || document.Pages.Pages[0].ID == document.Pages.Pages[1].ID {
				t.Fatal("真实创建链的文档命名空间或页树声明丢失")
			}
			preferences := document.VPreferences
			if preferences == nil || preferences.HideToolbar == nil || *preferences.HideToolbar || preferences.Zoom == nil || !reflect.DeepEqual(preferences.Zoom.Value, tc.preferences.Zoom) {
				t.Fatal("真实创建链的可选显示标志或缩放比例丢失")
			}
			if tc.preferences.ZoomMode != "" {
				if preferences.Zoom.Mode == nil || *preferences.Zoom.Mode != tc.preferences.ZoomMode {
					t.Fatal("真实创建链的同级 ZoomMode 未解码到缩放分组")
				}
			} else if preferences.Zoom.Mode != nil {
				t.Fatal("真实创建链的缺省 ZoomMode 被提前填入")
			}
			if document.Attachments == nil || document.CustomTags == nil || document.Annotations == nil {
				t.Fatal("真实创建链的元数据索引引用丢失")
			}
			var attachments spec.Attachments
			decode(document.Attachments.Resolve(docPath.Dir()), &attachments)
			if attachments.XMLName.Space != spec.Namespace || len(attachments.Attachments) != 2 || attachments.Attachments[0].ID != "note" || attachments.Attachments[1].ID != "extra" {
				t.Fatal("真实创建链的附件命名空间或顺序改变")
			}
			note, extra := attachments.Attachments[0], attachments.Attachments[1]
			if !note.CreationDate.Equal(date) || !note.ModDate.Equal(date) || !note.Visible.IsSet() || note.Visible.Value(true) || note.Size == nil || *note.Size != float64(len("说明")) {
				t.Fatal("真实创建链的附件日期、可选 false 或大小丢失")
			}
			if !extra.CreationDate.IsZero() || !extra.ModDate.IsZero() || extra.Visible.IsSet() {
				t.Fatal("真实创建链的缺省附件属性被填入")
			}
			var tags spec.CustomTags
			decode(document.CustomTags.Resolve(docPath.Dir()), &tags)
			if tags.XMLName.Space != spec.Namespace || len(tags.CustomTags) != 2 || tags.CustomTags[0].NameSpace != "urn:first" || tags.CustomTags[1].NameSpace != "urn:second" || tags.CustomTags[0].SchemaLoc == nil || tags.CustomTags[1].SchemaLoc != nil {
				t.Fatal("真实创建链的自定义标引命名空间、顺序或可选模式引用改变")
			}
			var annotations spec.Annotations
			decode(document.Annotations.Resolve(docPath.Dir()), &annotations)
			if annotations.XMLName.Space != spec.Namespace || len(annotations.Pages) != 1 || annotations.Pages[0].PageID != spec.StRefID(document.Pages.Pages[0].ID) {
				t.Fatal("真实创建链的注释索引命名空间或页引用改变")
			}
		})
	}
}
