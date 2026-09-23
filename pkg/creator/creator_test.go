package creator

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"errors"
	"io"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zip"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/font"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/validator"
)

func newTestOFD(t *testing.T, data []byte) *parser.OFD {
	t.Helper()
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range ofd.Documents {
		for _, page := range document.Pages {
			if err := page.EnsureLoaded(); err != nil {
				t.Fatal(err)
			}
		}
	}
	return ofd
}

func TestCreateTextDocumentCanBeParsed(t *testing.T) {
	data, err := Marshal(Document{
		ID:       "creator-test",
		Title:    "创建测试",
		PageSize: A4,
		Pages: []Page{{Items: []Item{
			Text{X: 20, Y: 30, Width: 100, Height: 10, Value: "你好，OFD", Font: "SimSun"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(archive.File) != 4 {
		t.Fatalf("entry count = %d, want 4", len(archive.File))
	}

	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	if len(ofd.Documents) != 1 || len(ofd.Documents[0].Pages) != 1 {
		t.Fatalf("documents/pages = %d/%d, want 1/1", len(ofd.Documents), len(ofd.Documents[0].Pages))
	}
	if err := ofd.Documents[0].Pages[0].EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if got := ofd.Documents[0].Pages[0].Content().Layer[0].TextObject[0].TextCode[0].Value; got != "你好，OFD" {
		t.Fatalf("text = %q", got)
	}
	checkGeneratedPackage(t, data)
}

func TestMarshalWithOptionsDeterministic(t *testing.T) {
	document := Document{
		ID:       "deterministic-test",
		Title:    "deterministic",
		PageSize: A4,
		Pages: []Page{{Items: []Item{
			Text{X: 10, Y: 10, Width: 40, Height: 10, Value: "stable", Font: "SimSun", Size: 4},
		}}},
	}
	options := CreateOptions{Compression: CompressionAuto, Deterministic: true}
	first, err := MarshalWithOptions(document, options)
	if err != nil {
		t.Fatal(err)
	}
	second, err := MarshalWithOptions(document, options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("deterministic output changed between identical builds")
	}
	archive, err := zip.NewReader(bytes.NewReader(first), int64(len(first)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range archive.File {
		if !file.Modified.Equal(time.Unix(0, 0).UTC()) {
			t.Fatalf("entry %q modified time = %v", file.Name, file.Modified)
		}
	}
}

func TestCreateUsesStoreForAlreadyCompressedEntries(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}
	data, err := Marshal(Document{
		ID:        "zip-method-test",
		CoverName: "cover.png",
		CoverData: png,
		Pages:     []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	methods := make(map[string]uint16, len(archive.File))
	for _, file := range archive.File {
		methods[file.Name] = file.Method
	}
	if methods["Doc_0/Cover/cover.png"] != zip.Store {
		t.Fatalf("cover method = %d, want Store", methods["Doc_0/Cover/cover.png"])
	}
	if methods["OFD.xml"] != zip.Deflate {
		t.Fatalf("OFD.xml method = %d, want Deflate", methods["OFD.xml"])
	}
}

func TestZipEntryMethodClassifiesCompressedExtensions(t *testing.T) {
	for _, name := range []string{"image.PNG", "photo.jpeg", "sound.m4v", "movie.mp4", "document.pdf", "archive.zip", "sheet.xlsx"} {
		if method := EntryMethod(name, CompressionAuto); method != zip.Store {
			t.Errorf("EntryMethod(%q) = %d, want Store", name, method)
		}
	}
	for _, name := range []string{"OFD.xml", "Document.xml", "data.json", "font.ttf", "file.bin"} {
		if method := EntryMethod(name, CompressionAuto); method != zip.Deflate {
			t.Errorf("EntryMethod(%q) = %d, want Deflate", name, method)
		}
	}
}

func TestCreateDocumentMetadata(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}
	data, err := Marshal(Document{
		ID: "metadata-test", Title: "标题", Author: "作者", Subject: "主题", Abstract: "摘要", DocUsage: "Report",
		CoverData: png, Keywords: []string{"ofd", "metadata"}, CustomDatas: []CustomData{{Name: "Department", Value: "Engineering"}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	info := ofd.DocBodies[0].DocInfo
	if info.Abstract == nil || *info.Abstract != "摘要" || info.DocUsage == nil || *info.DocUsage != "Report" || info.Keywords == nil || len(info.Keywords.Keyword) != 2 || info.CustomDatas == nil || len(info.CustomDatas.CustomData) != 1 || info.Cover == nil {
		t.Fatalf("文档元数据未正确生成: %+v", info)
	}
	checkGeneratedPackage(t, data)
}

func TestCreatePageResources(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 1, 2, 3}
	data, err := Marshal(Document{
		ID: "page-resource-test",
		Pages: []Page{{
			Resources: []PageResource{{Images: []PageImage{{ID: 70, Data: png}}}},
			Items:     []Item{Image{X: 10, Y: 10, Width: 20, Height: 20, ResourceID: 70}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	page := ofd.Documents[0].Pages[0]
	if len(page.PageRes()) != 1 || page.Content().Layer[0].ImageObject[0].ResourceID != 70 {
		t.Fatalf("页面资源引用未正确生成: %+v", page.PageRes())
	}
	if ofd.Documents[0].GetMedia(70) == nil || ofd.Documents[0].GetMedia(70).Type != "Image" {
		t.Fatalf("页面图片资源未加载: %+v", ofd.Documents[0].GetMedia(70))
	}
	checkGeneratedPackage(t, data)
}

func TestCreateAllowsExplicitResourceIDsOutOfOrder(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	data, err := Marshal(Document{
		ID:    "out-of-order-resource-ids",
		Media: []Media{{ID: 200, Type: "Audio", Format: "wav", Data: []byte("audio")}},
		Pages: []Page{{
			Resources: []PageResource{{Images: []PageImage{{ID: 100, Format: "PNG", Data: png}}}},
			Items:     []Item{Image{X: 1, Y: 1, Width: 2, Height: 2, ResourceID: 100}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	if len(ofd.Documents[0].Pages) != 1 || uint64(ofd.Documents[0].Pages[0].ID) == 100 || uint64(ofd.Documents[0].Pages[0].ID) == 200 {
		t.Fatalf("automatic page ID collided with an explicit resource ID: %+v", ofd.Documents[0].Pages[0].ID)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateRejectsRawResourceIDCollision(t *testing.T) {
	_, err := Marshal(Document{
		ID:    "raw-resource-id-collision",
		Media: []Media{{ID: 70, Type: "Audio", Format: "wav", Data: []byte("audio")}},
		Pages: []Page{{Resources: []PageResource{{
			Data:  []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><MultiMedias><MultiMedia ID="70" Type="Audio"><MediaFile>audio.wav</MediaFile></MultiMedia></MultiMedias></Res>`),
			Files: []PageResourceFile{{Path: "audio.wav", Data: []byte("audio")}},
		}}}},
	})
	if err == nil {
		t.Fatal("Marshal accepted a raw resource ID colliding with a document resource ID")
	}
}

func TestCreateRejectsMissingRawDrawParamRelative(t *testing.T) {
	_, err := Marshal(Document{
		ID: "missing-raw-draw-param-relative",
		Pages: []Page{{Resources: []PageResource{{
			Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><DrawParams><DrawParam ID="70" Relative="999"/></DrawParams></Res>`),
		}}}},
	})
	if err == nil {
		t.Fatal("Marshal accepted a raw DrawParam Relative reference to a missing ID")
	}
}

func TestCreateRejectsRawDrawParamRelativeCycle(t *testing.T) {
	_, err := Marshal(Document{
		ID: "raw-draw-param-relative-cycle",
		Pages: []Page{{Resources: []PageResource{{
			Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><DrawParams><DrawParam ID="70" Relative="71"/><DrawParam ID="71" Relative="70"/></DrawParams></Res>`),
		}}}},
	})
	if err == nil {
		t.Fatal("Marshal accepted a cyclic raw DrawParam Relative chain")
	}
}

func TestCreateValidatesRawCompositeImageReferences(t *testing.T) {
	data, err := Marshal(Document{
		ID: "raw-composite-image-reference",
		Pages: []Page{{Resources: []PageResource{{
			Data:  []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><MultiMedias><MultiMedia ID="70" Type="Image"><MediaFile>image.png</MediaFile></MultiMedia></MultiMedias><CompositeGraphicUnits><CompositeGraphicUnit ID="71" Width="1" Height="1"><Thumbnail>70</Thumbnail><Content/></CompositeGraphicUnit></CompositeGraphicUnits></Res>`),
			Files: []PageResourceFile{{Path: "image.png", Data: []byte("image")}},
		}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateRejectsMissingRawCompositeImageReference(t *testing.T) {
	_, err := Marshal(Document{
		ID: "missing-raw-composite-image-reference",
		Pages: []Page{{Resources: []PageResource{{
			Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><CompositeGraphicUnits><CompositeGraphicUnit ID="71" Width="1" Height="1"><Thumbnail>70</Thumbnail><Content/></CompositeGraphicUnit></CompositeGraphicUnits></Res>`),
		}}}},
	})
	if err == nil {
		t.Fatal("Marshal accepted a raw composite image reference to a missing ID")
	}
}

func TestCreateRejectsMissingPublicResourceDrawParamRelative(t *testing.T) {
	_, err := Marshal(Document{
		ID: "missing-public-draw-param-relative",
		PublicRes: []PublicResource{{
			Name: "Public/SharedRes.xml",
			Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><DrawParams><DrawParam ID="70" Relative="999"/></DrawParams></Res>`),
		}},
		Pages: []Page{{}},
	})
	if err == nil {
		t.Fatal("Marshal accepted a public DrawParam Relative reference to a missing ID")
	}
}

func TestCreateRejectsMissingRawColorAndFontReferences(t *testing.T) {
	_, err := Marshal(Document{
		ID: "missing-raw-color-font-references",
		Pages: []Page{{Resources: []PageResource{{
			Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><DrawParams><DrawParam ID="70"><FillColor ColorSpace="999"/></DrawParam></DrawParams><Fonts><Font ID="71" FontName="RawFont"/></Fonts><CompositeGraphicUnits><CompositeGraphicUnit ID="72" Width="1" Height="1"><Content/></CompositeGraphicUnit></CompositeGraphicUnits></Res>`),
		}}}},
	})
	if err == nil {
		t.Fatal("Marshal accepted missing raw ColorSpace references")
	}
}

func TestCreateValidatesRawFontAndColorReferences(t *testing.T) {
	data, err := Marshal(Document{
		ID: "valid-raw-color-font-references",
		Pages: []Page{{Resources: []PageResource{{
			Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><ColorSpaces><ColorSpace ID="70" Type="RGB"/></ColorSpaces><DrawParams><DrawParam ID="71"><FillColor ColorSpace="70"/></DrawParam></DrawParams><Fonts><Font ID="72" FontName="RawFont"/></Fonts><CompositeGraphicUnits><CompositeGraphicUnit ID="73" Width="1" Height="1"><Content/></CompositeGraphicUnit></CompositeGraphicUnits></Res>`),
		}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateAttachments(t *testing.T) {
	visible := true
	data, err := Marshal(Document{
		ID: "attachment-test",
		Attachments: []Attachment{{
			ID: "att-1", Name: "说明文本", Format: "txt", FileName: "readme.txt",
			CreationDate: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Visible: &visible,
			Usage: "Data", Data: []byte("attachment content"),
		}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	document := ofd.Documents[0]
	if document.GetAttachments() == nil || len(document.GetAttachments().Attachments) != 1 {
		t.Fatalf("附件清单未生成")
	}
	attachment := document.GetAttachments().Attachments[0]
	if attachment.ID != "att-1" || attachment.Name != "说明文本" || attachment.FileLoc.String() != "Files/readme.txt" || attachment.Size == nil || *attachment.Size != 18 {
		t.Fatalf("附件内容错误: %+v", attachment)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateCustomTags(t *testing.T) {
	data, err := Marshal(Document{
		ID: "custom-tags-test",
		CustomTags: []CustomTag{{
			NameSpace: "urn:example:tags", SchemaName: "tags.xsd", DataName: "tags.xml",
			Schema: []byte("<xs:schema xmlns:xs=\"http://www.w3.org/2001/XMLSchema\"/>"),
			Data:   []byte("<Tags xmlns=\"urn:example:tags\"><Value>one</Value></Tags>"),
		}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	if ofd.Documents[0].GetCustomTags() == nil || len(ofd.Documents[0].GetCustomTags().CustomTags) != 1 {
		t.Fatalf("自定义标签清单未生成: %+v", ofd.Documents[0].GetCustomTags())
	}
	tag := ofd.Documents[0].GetCustomTags().CustomTags[0]
	if tag.NameSpace != "urn:example:tags" || tag.SchemaLoc == nil || tag.FileLoc.String() != "Data/tags.xml" {
		t.Fatalf("自定义标签内容错误: %+v", tag)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateExtensions(t *testing.T) {
	data, err := Marshal(Document{
		ID: "extensions-test",
		Extensions: []Extension{{
			AppName: "creator-test", Company: "example", AppVersion: "1.0", RefID: 1,
			Properties: []ExtensionProperty{{Name: "Mode", Type: "string", Value: "test"}},
			Data:       "<Config><Enabled>true</Enabled></Config>",
		}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	if ofd.Documents[0].GetExtensions() == nil || len(ofd.Documents[0].GetExtensions().Extensions) != 1 {
		t.Fatalf("扩展清单未生成: %+v", ofd.Documents[0].GetExtensions())
	}
	extension := ofd.Documents[0].GetExtensions().Extensions[0]
	if extension.AppName != "creator-test" || extension.RefID != 1 || len(extension.Properties) != 1 || extension.Data == nil {
		t.Fatalf("扩展内容错误: %+v", extension)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateExternalExtensionData(t *testing.T) {
	data, err := Marshal(Document{
		ID: "external-extension-test",
		Extensions: []Extension{{
			AppName: "creator-test", RefID: 1, DataName: "extension.bin", DataFile: []byte("extension data"),
		}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	if ofd.Documents[0].GetExtensions() == nil || len(ofd.Documents[0].GetExtensions().Extensions) != 1 || ofd.Documents[0].GetExtensions().Extensions[0].ExtendData == nil || ofd.Documents[0].GetExtensions().Extensions[0].ExtendData.String() != "Data/extension.bin" {
		t.Fatalf("外部扩展数据未生成: %+v", ofd.Documents[0].GetExtensions())
	}
	checkGeneratedPackage(t, data)
}

func TestCreateRawExtensionXML(t *testing.T) {
	data, err := Marshal(Document{
		ID: "raw-extension-test",
		Extensions: []Extension{{
			AppName: "creator-test", RefID: 1,
			DataXML: []byte(`<Config><Enabled>true</Enabled></Config>`),
		}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	extensionXML := readArchiveEntry(t, data, "Doc_0/Extensions/Extensions.xml")
	if !bytes.Contains(extensionXML, []byte("<Config>")) || !bytes.Contains(extensionXML, []byte("<Enabled>true</Enabled>")) {
		t.Fatalf("raw extension XML was not preserved: %s", extensionXML)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateRawExtensionXMLPreservesMixedText(t *testing.T) {
	data, err := Marshal(Document{
		ID: "raw-extension-mixed-text-test",
		Extensions: []Extension{{
			AppName: "creator-test", RefID: 1,
			DataXML: []byte(`before<Config>inside</Config>after`),
		}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	extensionXML := readArchiveEntry(t, data, "Doc_0/Extensions/Extensions.xml")
	if !bytes.Contains(extensionXML, []byte(`>before`)) || !bytes.Contains(extensionXML, []byte(`<Config>inside</Config>after</Data>`)) {
		t.Fatalf("raw extension mixed text was not preserved: %s", extensionXML)
	}
	checkGeneratedPackage(t, data)
}

func TestCreatePatternThumbnail(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}
	data, err := Marshal(Document{
		ID:    "pattern-thumbnail-test",
		Media: []Media{{ID: 40, Type: "Image", Format: "png", Data: png}},
		Pages: []Page{{Items: []Item{Path{
			X: 0, Y: 0, Width: 20, Height: 20, Data: "M 0 0 L 20 20 C",
			FillColor: &Color{Pattern: &Pattern{
				Width: 5, Height: 5, Thumbnail: 40,
				Items: []Item{Path{X: 0, Y: 0, Width: 5, Height: 5, Data: "M 0 0 L 5 5 C"}},
			}},
		}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	patternXML := readArchiveEntry(t, data, "Doc_0/Pages/Page_0/Content.xml")
	if !bytes.Contains(patternXML, []byte(`CellContent Thumbnail="40"`)) {
		t.Fatal("pattern thumbnail was not serialized")
	}
	checkGeneratedPackage(t, data)
}

func TestCreateAnnotationReadOnlyFalse(t *testing.T) {
	readOnly := false
	data, err := Marshal(Document{
		ID: "annotation-readonly-test",
		Annotations: []AnnotationPage{{Page: 0, Items: []Annotation{{
			ID: 1, Type: "Link", Creator: "creator-test", ReadOnlyValue: &readOnly,
		}}}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	annotationXML := readArchiveEntry(t, data, "Doc_0/Annotations/Page_1.xml")
	if !bytes.Contains(annotationXML, []byte(`ReadOnly="false"`)) {
		t.Fatal("ReadOnly=false was not serialized")
	}
	checkGeneratedPackage(t, data)
}

func TestCreateRawPageResource(t *testing.T) {
	data, err := Marshal(Document{
		ID: "raw-page-resource-test",
		Pages: []Page{{Resources: []PageResource{{
			Data:  []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><MultiMedias><MultiMedia ID="70" Type="Audio" Format="wav"><MediaFile>Media/sound.wav</MediaFile></MultiMedia></MultiMedias></Res>`),
			Files: []PageResourceFile{{Path: "Media/sound.wav", Data: []byte("sound")}},
		}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	seenXML, seenFile := false, false
	for _, file := range archive.File {
		seenXML = seenXML || file.Name == "Doc_0/Pages/Page_0/PageRes_0_0.xml"
		seenFile = seenFile || file.Name == "Doc_0/Pages/Page_0/Media/sound.wav"
	}
	if !seenXML || !seenFile {
		t.Fatalf("raw page resource entries missing: xml=%t file=%t", seenXML, seenFile)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateRawPageResourceMediaReference(t *testing.T) {
	data, err := Marshal(Document{
		ID: "raw-page-resource-media-test",
		Pages: []Page{{
			Resources: []PageResource{{
				Data:  []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><MultiMedias><MultiMedia ID="70" Type="Image" Format="png"><MediaFile>Images/raw.png</MediaFile></MultiMedia></MultiMedias></Res>`),
				Files: []PageResourceFile{{Path: "Images/raw.png", Data: []byte("image")}},
			}},
			Items: []Item{Image{X: 1, Y: 1, Width: 2, Height: 2, ResourceID: 70}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateRawPageResourceObjectReferences(t *testing.T) {
	data, err := Marshal(Document{
		ID: "raw-page-resource-object-reference-test",
		Pages: []Page{{
			Resources: []PageResource{{
				Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><ColorSpaces><ColorSpace ID="74" Type="RGB"/></ColorSpaces><DrawParams><DrawParam ID="70" LineWidth="1"/></DrawParams><Fonts><Font ID="71" FontName="RawFont"/></Fonts><CompositeGraphicUnits><CompositeGraphicUnit ID="73" Width="1" Height="1"><Content/></CompositeGraphicUnit></CompositeGraphicUnits></Res>`),
			}},
			Items: []Item{
				Text{X: 1, Y: 1, Width: 10, Height: 5, Value: "raw font", Font: "RawFont"},
				Path{X: 1, Y: 10, Width: 10, Height: 10, DrawParam: "70", Data: "M 0 0 L 10 10 C", FillColor: &Color{ColorSpace: 74, Components: []int{1, 2, 3}}},
				Composite{X: 1, Y: 20, Width: 1, Height: 1, ResourceID: 73},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGeneratedPackage(t, data)
}

func TestCreatePublicResourceObjectReferences(t *testing.T) {
	data, err := Marshal(Document{
		ID: "public-resource-object-reference-test",
		PublicRes: []PublicResource{{
			Name: "Public/SharedRes.xml",
			Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><ColorSpaces><ColorSpace ID="74" Type="RGB"/></ColorSpaces><DrawParams><DrawParam ID="70" LineWidth="1"/></DrawParams><Fonts><Font ID="71" FontName="PublicFont"/></Fonts></Res>`),
		}},
		Pages: []Page{{Items: []Item{
			Text{X: 1, Y: 1, Width: 10, Height: 5, Value: "public font", Font: "PublicFont"},
			Path{X: 1, Y: 10, Width: 10, Height: 10, DrawParam: "70", Data: "M 0 0 L 10 10 C", FillColor: &Color{ColorSpace: 74, Components: []int{1, 2, 3}}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGeneratedPackage(t, data)
}

func TestCreatePublicResourceExternalFiles(t *testing.T) {
	data, err := Marshal(Document{
		ID: "public-resource-external-file-test",
		PublicRes: []PublicResource{{
			Name:  "Public/SharedRes.xml",
			Data:  []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><MultiMedias><MultiMedia ID="70" Type="Audio"><MediaFile>Media/sound.wav</MediaFile></MultiMedia></MultiMedias></Res>`),
			Files: []PublicResourceFile{{Path: "Media/sound.wav", Data: []byte("sound")}},
		}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range archive.File {
		if file.Name == "Doc_0/Public/Media/sound.wav" {
			return
		}
	}
	t.Fatal("public resource external file was not written")
}

func TestCreateResourceExternalFilesWithBaseLoc(t *testing.T) {
	data, err := Marshal(Document{
		ID: "resource-external-files-base-loc-test",
		PublicRes: []PublicResource{{
			Name: "Public/SharedRes.xml",
			Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="Assets"><ColorSpaces><ColorSpace ID="70" Type="RGB" Profile="profile.icc"/></ColorSpaces><Fonts><Font ID="71" FontName="EmbeddedFont"><FontFile>font.ttf</FontFile></Font></Fonts><MultiMedias><MultiMedia ID="72" Type="Audio"><MediaFile>sound.wav</MediaFile></MultiMedia></MultiMedias></Res>`),
			Files: []PublicResourceFile{
				{Path: "Assets/profile.icc", Data: []byte("profile")},
				{Path: "Assets/font.ttf", Data: []byte("font")},
				{Path: "Assets/sound.wav", Data: []byte("sound")},
			},
		}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"Doc_0/Public/Assets/profile.icc",
		"Doc_0/Public/Assets/font.ttf",
		"Doc_0/Public/Assets/sound.wav",
	} {
		readArchiveEntry(t, data, name)
	}
}

func TestCreateResourceExternalFileCanReferenceParentDirectory(t *testing.T) {
	data, err := Marshal(Document{
		ID: "resource-external-parent-file-test",
		PublicRes: []PublicResource{{
			Name:  "Public/SharedRes.xml",
			Data:  []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><MultiMedias><MultiMedia ID="70" Type="Audio"><MediaFile>../Shared/sound.wav</MediaFile></MultiMedia></MultiMedias></Res>`),
			Files: []PublicResourceFile{{Path: "../Shared/sound.wav", Data: []byte("sound")}},
		}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	readArchiveEntry(t, data, "Doc_0/Shared/sound.wav")
}

func TestCreateRejectsMissingResourceExternalFile(t *testing.T) {
	_, err := Marshal(Document{
		ID: "missing-resource-external-file-test",
		PublicRes: []PublicResource{{
			Name: "Public/SharedRes.xml",
			Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><MultiMedias><MultiMedia ID="70" Type="Audio"><MediaFile>Media/missing.wav</MediaFile></MultiMedia></MultiMedias></Res>`),
		}},
		Pages: []Page{{}},
	})
	if err == nil {
		t.Fatal("Marshal accepted a missing public resource file")
	}
}

func TestCreateClipColorsAreValidated(t *testing.T) {
	_, err := Marshal(Document{
		ID:          "clip-color-validation-test",
		ColorSpaces: []ColorSpace{{ID: 50, Type: "RGB"}},
		Pages: []Page{{
			Items: []Item{Path{
				X: 0, Y: 0, Width: 10, Height: 10, Data: "M 0 0 L 10 10 C",
				Clips: &Clips{Items: []Clip{{Areas: []ClipArea{{
					Path: &ClipPath{
						Boundary: Box{Width: 10, Height: 10}, Data: "M 0 0 L 10 10 C",
						FillColor: &Color{ColorSpace: 99, Components: []int{1, 2, 3}},
					},
				}}}}},
			}},
		}},
	})
	if err == nil {
		t.Fatal("Marshal accepted an undefined clip color space")
	}
}

func TestCreateSignatures(t *testing.T) {
	data, err := Marshal(Document{
		ID: "signature-test",
		Signatures: []Signature{{
			ID: "sig-1", Type: "Sign", ProviderName: "creator-test", Method: "RSA", CheckMethod: "SHA1",
			References:  []SignatureReference{{FileRef: "../Document.xml"}},
			SignedValue: []byte("signed-value"), SignedValueName: "value.bin",
		}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	if ofd.Documents[0].GetSignature("sig-1") == nil {
		t.Fatalf("签名未正确解析")
	}
	checkGeneratedPackage(t, data)
}

func TestCreateSignaturesNormalizesCheckMethod(t *testing.T) {
	data, err := Marshal(Document{
		ID: "signature-check-method-case",
		Signatures: []Signature{{
			ID: "sig-case", Type: "Sign", ProviderName: "creator-test", CheckMethod: "sha1",
			References: []SignatureReference{{FileRef: "../Document.xml"}}, SignedValue: []byte("value"),
		}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	signatureXML := readArchiveEntry(t, data, "Doc_0/Signatures/Signature_sig-case.xml")
	if !bytes.Contains(signatureXML, []byte(`CheckMethod="SHA1"`)) {
		t.Fatalf("explicit signature digest method was not preserved: %s", signatureXML)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateSignaturesDefaultCheckMethodMatchesSchema(t *testing.T) {
	data, err := Marshal(Document{
		ID: "signature-default-check-method",
		Signatures: []Signature{{
			ID: "sig-default", Type: "Sign", ProviderName: "creator-test",
			References: []SignatureReference{{FileRef: "../Document.xml"}}, SignedValue: []byte("value"),
		}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	signatureXML := readArchiveEntry(t, data, "Doc_0/Signatures/Signature_sig-default.xml")
	if !bytes.Contains(signatureXML, []byte(`CheckMethod="MD5"`)) {
		t.Fatalf("default signature digest method is not MD5: %s", signatureXML)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateSignaturesUseSelectedDigestMethod(t *testing.T) {
	for _, method := range []string{"MD5", "SM3", "1.2.156.10197.1.401"} {
		t.Run(method, func(t *testing.T) {
			data, err := Marshal(Document{
				ID: "signature-digest-method",
				Signatures: []Signature{{
					ID: "sig-digest-method", Type: "Sign", ProviderName: "creator-test", CheckMethod: method,
					References: []SignatureReference{{FileRef: "../Document.xml"}}, SignedValue: []byte("value"),
				}},
				Pages: []Page{{}},
			})
			if err != nil {
				t.Fatal(err)
			}
			checkGeneratedPackage(t, data)
		})
	}
}

func TestCreateSignaturesDigestAdditionalPackageFile(t *testing.T) {
	data, err := Marshal(Document{
		ID:          "signature-additional-reference",
		ColorSpaces: []ColorSpace{{ID: 30, Type: "RGB"}},
		Signatures: []Signature{{
			ID: "sig-resource", Type: "Sign", ProviderName: "creator-test", CheckMethod: "MD5",
			References:  []SignatureReference{{FileRef: "../DocumentRes.xml"}},
			SignedValue: []byte("value"),
		}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resource := readArchiveEntry(t, data, "Doc_0/DocumentRes.xml")
	digest := md5.Sum(resource)
	want := base64.StdEncoding.EncodeToString(digest[:])
	signature := readArchiveEntry(t, data, "Doc_0/Signatures/Signature_sig-resource.xml")
	if !bytes.Contains(signature, []byte(`<CheckValue>`+want+`</CheckValue>`)) {
		t.Fatalf("signature digest for additional package file is incorrect: %s", signature)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateRejectsIncorrectProvidedSignatureDigest(t *testing.T) {
	_, err := Marshal(Document{
		ID: "signature-incorrect-provided-digest",
		Signatures: []Signature{{
			ID: "sig-incorrect-digest", Type: "Sign", ProviderName: "creator-test",
			References:  []SignatureReference{{FileRef: "../Document.xml", CheckValue: []byte("wrong")}},
			SignedValue: []byte("value"),
		}},
		Pages: []Page{{}},
	})
	if err == nil {
		t.Fatal("Marshal accepted an incorrect provided signature digest")
	}
}

func TestCreateSignaturesRejectsMissingAutomaticDigestTarget(t *testing.T) {
	_, err := Marshal(Document{
		ID: "signature-missing-digest-target",
		Signatures: []Signature{{
			ID: "sig-missing-target", Type: "Sign", ProviderName: "creator-test",
			References:  []SignatureReference{{FileRef: "../missing.xml"}},
			SignedValue: []byte("value"),
		}},
		Pages: []Page{{}},
	})
	if err == nil {
		t.Fatal("Marshal accepted a missing automatic signature digest target")
	}
}

func TestCreateSignaturesAvoidsMaxSignIDCollision(t *testing.T) {
	data, err := Marshal(Document{
		ID: "signature-max-id-collision",
		Signatures: []Signature{{
			ID: "MaxSignId-1", Type: "Sign", ProviderName: "creator-test",
			References:  []SignatureReference{{FileRef: "../Document.xml"}},
			SignedValue: []byte("value"),
		}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	signatures := readArchiveEntry(t, data, "Doc_0/Signatures.xml")
	if bytes.Contains(signatures, []byte(`<MaxSignId>MaxSignId-1</MaxSignId>`)) {
		t.Fatalf("MaxSignId collided with signature ID: %s", signatures)
	}
	checkGeneratedPackage(t, data)
}

func TestMarshalDoesNotPersistSignatureDigest(t *testing.T) {
	document := Document{
		ID: "signature-digest-test",
		Signatures: []Signature{{
			ID: "sig-digest", Type: "Sign", ProviderName: "creator-test",
			References: []SignatureReference{{FileRef: "../Document.xml"}}, SignedValue: []byte("value"),
		}},
		Pages: []Page{{}},
	}
	first, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if document.Signatures[0].References[0].CheckValue != nil {
		t.Fatal("Marshal persisted the generated signature digest")
	}
	second, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("Marshal output changed for the same signature input")
	}
	checkGeneratedPackage(t, first)
}

func TestCreateDocumentVersions(t *testing.T) {
	data, err := Marshal(Document{
		ID: "versions-test",
		Versions: []DocumentVersion{{
			ID: "version-1", Index: 1, Current: true, Version: "1.0", Name: "初始版本",
			CreationDate: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
			Files:        []VersionFile{{ID: "file-1", Path: "Document.xml"}},
			DocRoot:      []byte("<Document xmlns=\"http://www.ofdspec.org/2016\"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID=\"1\" BaseLoc=\"../../Pages/Page_0/Content.xml\"/></Pages></Document>"),
			DocRootName:  "version-document.xml",
		}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	if ofd.Documents[0].GetVersion("version-1") == nil || ofd.Documents[0].GetVersion("version-1").DocRoot.String() != "Files/version-document.xml" {
		t.Fatalf("文档版本未正确解析")
	}
	checkGeneratedPackage(t, data)
}

func TestCreateRejectsVersionRootNameWithoutRootData(t *testing.T) {
	_, err := Marshal(Document{
		ID: "missing-version-root-data",
		Versions: []DocumentVersion{{
			ID: "version-1", Index: 1, DocRootName: "version.xml",
			Files: []VersionFile{{ID: "file-1", Path: "Document.xml"}},
		}},
		Pages: []Page{{}},
	})
	if err == nil {
		t.Fatal("Marshal accepted DocRootName without DocRoot")
	}
}

func TestCreateRejectsDuplicateVersionFileAndRootIDs(t *testing.T) {
	_, err := Marshal(Document{
		ID: "duplicate-version-root-file-id",
		Versions: []DocumentVersion{{
			ID: "same", Index: 1,
			Files: []VersionFile{{ID: "same", Path: "Document.xml"}},
		}},
		Pages: []Page{{}},
	})
	if err == nil {
		t.Fatal("Marshal accepted a version file ID colliding with the version ID")
	}
}

func TestCreateRejectsDuplicateSignatureStampIDs(t *testing.T) {
	_, err := Marshal(Document{
		ID: "duplicate-signature-stamp-id",
		Signatures: []Signature{{
			ID: "signature-1", Type: "Sign", ProviderName: "creator-test",
			References:  []SignatureReference{{FileRef: "../Document.xml"}},
			SignedValue: []byte("value"),
			StampAnnots: []SignatureStamp{
				{ID: "stamp-1", Page: 0, Boundary: Box{Width: 10, Height: 10}},
				{ID: "stamp-1", Page: 0, Boundary: Box{Width: 10, Height: 10}},
			},
		}},
		Pages: []Page{{}},
	})
	if err == nil {
		t.Fatal("Marshal accepted duplicate signature stamp IDs")
	}
}

func TestCreateRejectsMissingExtensionReference(t *testing.T) {
	_, err := Marshal(Document{
		ID: "missing-extension-reference",
		Extensions: []Extension{{
			AppName: "creator-test", RefID: 999, Data: "data",
		}},
		Pages: []Page{{}},
	})
	if err == nil {
		t.Fatal("Marshal accepted an extension with a missing RefID target")
	}
}

func TestCreateRejectsNegativeRawColorBits(t *testing.T) {
	_, err := Marshal(Document{
		ID: "negative-raw-color-bits",
		Pages: []Page{{
			Resources: []PageResource{{
				Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><ColorSpaces><ColorSpace ID="70" Type="RGB" BitsPerComponent="-1"/></ColorSpaces></Res>`),
			}},
			Items: []Item{Path{
				X: 0, Y: 0, Width: 10, Height: 10,
				Data:      "M 0 0 L 10 10 C",
				FillColor: &Color{ColorSpace: 70, Components: []int{1, 2, 3}},
			}},
		}},
	})
	if err == nil {
		t.Fatal("Marshal accepted negative raw color bits")
	}
}

func TestCreateImageDocumentWritesImageResource(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}
	data, err := Marshal(Document{
		ID: "image-test",
		Pages: []Page{{Items: []Item{
			Image{X: 1, Y: 2, Width: 10, Height: 20, Data: png},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range archive.File {
		if bytes.HasSuffix([]byte(file.Name), []byte(".PNG")) {
			found = true
		}
	}
	if !found {
		t.Fatal("PNG resource was not written")
	}
	newTestOFD(t, data)
	checkGeneratedPackage(t, data)
}

func TestCreateEmbeddedFontWritesFontResource(t *testing.T) {
	fontData := testEmbeddedFontData(t)
	data, err := Marshal(Document{
		ID: "font-test",
		Fonts: []Font{{
			Name:       "Test Sans",
			FamilyName: "Test",
			Bold:       true,
			Data:       fontData,
		}},
		Pages: []Page{{Items: []Item{
			Text{X: 1, Y: 2, Width: 10, Height: 5, Value: "font", Font: "Test Sans"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range archive.File {
		if bytes.HasPrefix([]byte(file.Name), []byte("Doc_0/Res/Fonts/")) {
			found = true
			got, readErr := file.Open()
			if readErr != nil {
				t.Fatal(readErr)
			}
			contents, readErr := io.ReadAll(got)
			if readErr != nil {
				t.Fatal(readErr)
			}
			_ = got.Close()
			if !bytes.Equal(contents, fontData) {
				t.Fatalf("embedded font data = %v, want %v", contents, fontData)
			}
		}
	}
	if !found {
		t.Fatal("embedded font resource was not written")
	}

	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	ft := ofd.Documents[0].GetFont(1)
	if ft == nil {
		t.Fatal("embedded font was not parsed")
	}
	if got := ft.FontName; got != "Test Sans" {
		t.Fatalf("font name = %q", got)
	}
	if got := string(ft.FontFile); !strings.HasPrefix(got, "Doc_0/Res/Fonts/") {
		t.Fatalf("font file = %q", got)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateEmbeddedTTCFontResource(t *testing.T) {
	fontData, err := os.ReadFile("../../cmd/ofd-creator/examples/NotoSansCJK-Regular.ttc")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("NotoSansCJK-Regular.ttc is not available")
	}
	if err != nil {
		t.Fatal(err)
	}
	if !isFontCollection(fontData) {
		t.Fatal("NotoSansCJK-Regular.ttc is not a TrueType collection")
	}
	if _, err := font.ParseSFNT(fontData, 0); err != nil {
		t.Fatalf("parse TTC font: %v", err)
	}
	family := canvas.NewFontFamily("Noto Sans CJK SC")
	if err := family.LoadFont(fontData, 0, canvas.FontRegular); err != nil {
		t.Fatalf("load TTC font: %v", err)
	}
	face := family.Face(10, canvas.Black)
	if face == nil || face.Font == nil || face.Font.GlyphIndex('中') == 0 {
		t.Fatal("TTC font does not provide a usable CJK glyph")
	}

	data, err := Marshal(Document{
		ID:    "ttc-font-test",
		Fonts: []Font{{Name: "Noto Sans CJK SC", Format: "ttc", Data: fontData}},
		Pages: []Page{{Items: []Item{
			Text{X: 20, Y: 30, Width: 100, Height: 12, Size: 10, Font: "Noto Sans CJK SC", Value: "你好，OFD"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := readEmbeddedFontData(t, data)
	t.Logf("TTC input size=%d, extracted font size=%d", len(fontData), len(got))
	if bytes.Equal(got, fontData) || len(got) >= len(fontData) {
		t.Fatalf("TTC font was not reduced: got %d bytes, want less than %d", len(got), len(fontData))
	}
	if string(got[:4]) != "OTTO" {
		t.Fatalf("TTC face was not converted to standalone OTF: header=%q", got[:4])
	}
	parsed, err := font.ParseSFNT(got, 0)
	if err != nil {
		t.Fatalf("parse extracted OTF: %v", err)
	}
	if parsed.GlyphIndex('中') == 0 {
		t.Fatal("extracted OTF does not provide a usable CJK glyph")
	}
	extractedFamily := canvas.NewFontFamily("Noto Sans CJK SC")
	if err := extractedFamily.LoadFont(got, 0, canvas.FontRegular); err != nil {
		t.Fatalf("load extracted OTF: %v", err)
	}
	if face := extractedFamily.Face(10, canvas.Black); face == nil || face.Font == nil || face.Font.GlyphIndex('中') == 0 {
		t.Fatal("extracted OTF cannot be used by Canvas")
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	fontFile := ofd.Documents[0].GetFont(1).FontFile
	if !strings.HasSuffix(string(fontFile), ".otf") {
		t.Fatalf("extracted TTC font resource uses unexpected file name: %q", fontFile)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateTextWritesStyleAttributes(t *testing.T) {
	fill := false
	data, err := Marshal(Document{
		ID: "text-style-test",
		Pages: []Page{{Items: []Item{
			Text{
				X:             1,
				Y:             2,
				Width:         10,
				Height:        5,
				Value:         "样式",
				HScale:        0.8,
				ReadDirection: 1,
				CharDirection: 2,
				Weight:        700,
				Italic:        true,
				Stroke:        true,
				Fill:          &fill,
			},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	text := ofd.Documents[0].Pages[0].Content().Layer[0].TextObject[0]
	if text.HScale != 0.8 || text.ReadDirection != 1 || text.CharDirection != 2 || text.Weight != 700 || !text.Italic || !text.Stroke || text.Fill.Value(true) {
		t.Fatalf("文字样式未正确生成: %+v", text)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateTextCodesAndCTM(t *testing.T) {
	x := 12.5
	y := 23.5
	data, err := Marshal(Document{
		ID: "text-code-test",
		Pages: []Page{{Items: []Item{
			Text{
				X: 1, Y: 2, Width: 60, Height: 12,
				Font: "SimSun", CTM: &CTM{1, 0, 0, 1, 3, 4},
				TextCodes: []TextCode{
					{Value: "第一段", X: &x, Y: &y, DeltaX: []float64{1, 2, 3}},
					{Value: "第二段", DeltaY: []float64{4, 5}},
				},
			},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	text := ofd.Documents[0].Pages[0].Content().Layer[0].TextObject[0]
	if text.CTM == nil || text.CTM[4] != 3 || text.CTM[5] != 4 {
		t.Fatalf("CTM 未正确生成: %+v", text.CTM)
	}
	if len(text.TextCode) != 2 || text.TextCode[0].Value != "第一段" || text.TextCode[0].X != x || text.TextCode[0].Y != y || len(text.TextCode[0].DeltaX) != 3 || text.TextCode[0].DeltaX[1] != 2 || len(text.TextCode[1].DeltaY) != 2 || text.TextCode[1].DeltaY[1] != 5 {
		t.Fatalf("TextCode 未正确生成: %+v", text.TextCode)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateCompletesMissingTextCodeDeltas(t *testing.T) {
	document := Document{
		ID: "text-code-delta-test",
		Pages: []Page{{Items: []Item{
			Text{X: 1, Y: 2, Width: 30, Height: 10, Size: 10, TextCodes: []TextCode{{Value: "abc"}}},
		}}},
	}
	data, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	code := ofd.Documents[0].Pages[0].Content().Layer[0].TextObject[0].TextCode[0]
	if code.Value != "abc" || len(code.DeltaX) != 0 || len(code.DeltaY) != 0 {
		t.Fatalf("默认不应自动补全 DeltaX/DeltaY: %+v", code)
	}
	if code.X != 0 || code.Y != 10 {
		t.Fatalf("TextCode 默认基线坐标错误: X=%v Y=%v", code.X, code.Y)
	}
	data, err = MarshalWithOptions(document, CreateOptions{CompleteTextCodeDeltas: true})
	if err != nil {
		t.Fatal(err)
	}
	ofd = newTestOFD(t, data)
	defer ofd.Close()
	code = ofd.Documents[0].Pages[0].Content().Layer[0].TextObject[0].TextCode[0]
	if len(code.DeltaX) != 2 || len(code.DeltaY) != 2 || code.DeltaX[0] <= 0 || code.DeltaX[1] <= 0 || code.DeltaY[0] != 0 || code.DeltaY[1] != 0 {
		t.Fatalf("启用参数后 TextCode 自动补全值错误: DeltaX=%v DeltaY=%v", code.DeltaX, code.DeltaY)
	}
}

func TestCreateTextCGTransforms(t *testing.T) {
	data, err := Marshal(Document{
		ID: "cg-transform-test",
		Pages: []Page{{Items: []Item{
			Text{
				X: 1, Y: 2, Width: 60, Height: 12,
				Value: "字形映射",
				CGTransforms: []CGTransform{
					{CodePosition: 1, CodeCount: 1, GlyphCount: 2, Glyphs: []int{120, 121}},
				},
			},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	text := ofd.Documents[0].Pages[0].Content().Layer[0].TextObject[0]
	if len(text.CGTransform) != 1 {
		t.Fatalf("CGTransform 数量 = %d, want 1", len(text.CGTransform))
	}
	transform := text.CGTransform[0]
	if transform.CodePosition != 1 || transform.CodeCount != 1 || transform.GlyphCount != 2 || len(transform.Glyphs) != 2 || transform.Glyphs[0] != 120 || transform.Glyphs[1] != 121 {
		t.Fatalf("CGTransform 未正确生成: %+v", transform)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateSubsetsEmbeddedFontAndRemapsCGTransforms(t *testing.T) {
	fontData, err := os.ReadFile("../../test/testdata/simkai.ttf")
	if os.IsNotExist(err) {
		t.Skip("simkai.ttf  is unavailable")
	}
	if err != nil {
		t.Fatal(err)
	}
	original, err := font.ParseSFNT(fontData, 0)
	if err != nil {
		t.Fatal(err)
	}
	transformedGlyph := original.GlyphIndex('测')
	if transformedGlyph == 0 {
		t.Fatal("simkai.ttf does not contain 测")
	}
	data, err := Marshal(Document{
		ID:    "font-subset-test",
		Fonts: []Font{{Name: "SimKai", Format: "ttf", Data: fontData}},
		Pages: []Page{{Items: []Item{Text{
			X: 1, Y: 2, Width: 60, Height: 12, Font: "SimKai", Value: "测试",
			CGTransforms: []CGTransform{{CodePosition: 0, CodeCount: 1, GlyphCount: 1, Glyphs: []int{int(transformedGlyph)}}},
		}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var subsetData []byte
	for _, file := range archive.File {
		if strings.HasPrefix(file.Name, "Doc_0/Res/Fonts/") {
			reader, readErr := file.Open()
			if readErr != nil {
				t.Fatal(readErr)
			}
			subsetData, readErr = io.ReadAll(reader)
			_ = reader.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
		}
	}
	if len(subsetData) == 0 {
		t.Fatal("embedded subset font was not written")
	}
	subset, err := font.ParseSFNT(subsetData, 0)
	if err != nil {
		t.Fatal(err)
	}
	if subset.NumGlyphs() >= original.NumGlyphs() {
		t.Fatalf("subset glyph count = %d, original = %d", subset.NumGlyphs(), original.NumGlyphs())
	}
	if subset.GlyphIndex('试') == 0 || subset.GlyphIndex('测') == 0 {
		t.Fatalf("subset cmap lost used characters: 试=%d 测=%d", subset.GlyphIndex('试'), subset.GlyphIndex('测'))
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	transform := ofd.Documents[0].Pages[0].Content().Layer[0].TextObject[0].CGTransform[0]
	if len(transform.Glyphs) != 1 || transform.Glyphs[0] != int(subset.GlyphIndex('测')) {
		t.Fatalf("CGTransform glyph = %v, want subset glyph %d", transform.Glyphs, subset.GlyphIndex('测'))
	}
}

func TestCreateActionsAndPageGoto(t *testing.T) {
	top := 40.0
	data, err := Marshal(Document{
		ID: "action-test",
		Actions: []Action{{
			Event: ActionEventDO,
			URI:   &URIAction{URI: "https://example.com/document", Target: "_blank"},
		}},
		Pages: []Page{
			{
				Actions: []Action{{URI: &URIAction{URI: "https://example.com/page"}}},
				Items: []Item{
					Text{
						X: 1, Y: 2, Width: 30, Height: 8, Value: "跳转",
						Actions: []Action{{Goto: &GotoAction{Page: 1, Type: "FitH", Top: &top}}},
					},
				},
			},
			{Items: []Item{
				Path{X: 1, Y: 1, Width: 20, Height: 20, Data: "M 0 0 L 20 20 C", Actions: []Action{{URI: &URIAction{URI: "https://example.com/path"}}}},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	document := ofd.Documents[0]
	if document.Actions == nil || len(document.Actions.Actions) != 1 || document.Actions.Actions[0].Event != "DO" || document.Actions.Actions[0].URI == nil || document.Actions.Actions[0].URI.URI != "https://example.com/document" || document.Actions.Actions[0].URI.Target == nil || *document.Actions.Actions[0].URI.Target != "_blank" {
		t.Fatalf("文档动作未正确生成: %+v", document.Actions)
	}

	firstPage := document.Pages[0]
	if firstPage.Actions() == nil || len(firstPage.Actions().Action) != 1 || firstPage.Actions().Action[0].URI == nil || firstPage.Actions().Action[0].URI.URI != "https://example.com/page" {
		t.Fatalf("页面动作未正确生成: %+v", firstPage.Actions())
	}
	textAction := firstPage.Content().Layer[0].TextObject[0].Actions
	if textAction == nil || len(textAction.Action) != 1 || textAction.Action[0].Goto == nil || textAction.Action[0].Goto.Dest == nil {
		t.Fatalf("文字跳转动作未正确生成: %+v", textAction)
	}
	if uint64(textAction.Action[0].Goto.Dest.PageID) != uint64(document.Pages[1].ID) || textAction.Action[0].Goto.Dest.Type != "FitH" || textAction.Action[0].Goto.Dest.Top == nil || *textAction.Action[0].Goto.Dest.Top != top {
		t.Fatalf("文字跳转目标未正确生成: %+v", textAction.Action[0].Goto.Dest)
	}

	pathAction := document.Pages[1].Content().Layer[0].PathObject[0].Actions
	if pathAction == nil || len(pathAction.Action) != 1 || pathAction.Action[0].URI == nil || pathAction.Action[0].URI.URI != "https://example.com/path" {
		t.Fatalf("路径动作未正确生成: %+v", pathAction)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateBookmarkGotoAction(t *testing.T) {
	data, err := Marshal(Document{
		ID:        "bookmark-action-test",
		Bookmarks: []Bookmark{{Name: "chapter-1", Goto: GotoAction{Page: 0, Type: "Fit"}}},
		Actions:   []Action{{Goto: &GotoAction{Bookmark: "chapter-1"}}},
		Pages:     []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	action := ofd.Documents[0].Document.Actions.Actions[0]
	if action.Goto == nil || action.Goto.Bookmark == nil || action.Goto.Bookmark.Name != "chapter-1" {
		t.Fatalf("书签跳转动作未正确生成: %+v", action)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateMediaActions(t *testing.T) {
	volume := 80
	repeat := true
	data, err := Marshal(Document{
		ID: "media-action-test",
		Media: []Media{
			{ID: 40, Type: "Audio", Format: ".MP3", Data: []byte("audio")},
			{ID: 41, Type: "Video", Format: "mp4", Data: []byte("video")},
		},
		Actions: []Action{
			{Event: ActionEventDO, Sound: &SoundAction{ResourceID: 40, Volume: &volume, Repeat: &repeat}},
		},
		Pages: []Page{{Actions: []Action{{Movie: &MovieAction{ResourceID: 41, Operator: "Play"}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	document := ofd.Documents[0]
	if len(document.DocumentResourceList()) != 1 || document.DocumentResourceList()[0].MultiMedias == nil || len(document.DocumentResourceList()[0].MultiMedias.MultiMedia) != 2 {
		t.Fatalf("多媒体资源未生成: %+v", document.DocumentResourceList())
	}
	if document.Actions == nil || document.Actions.Actions[0].Sound == nil || uint64(document.Actions.Actions[0].Sound.ResourceID) != 40 || document.Actions.Actions[0].Sound.Volume == nil || *document.Actions.Actions[0].Sound.Volume != volume {
		t.Fatalf("声音动作未正确生成: %+v", document.Actions)
	}
	if document.DocumentResourceList()[0].MultiMedias.MultiMedia[0].Format != "mp3" {
		t.Fatalf("媒体格式未规范化: %+v", document.DocumentResourceList()[0].MultiMedias.MultiMedia[0])
	}
	pageActions := document.Pages[0].Actions()
	if pageActions == nil || pageActions.Action[0].Movie == nil || uint64(pageActions.Action[0].Movie.ResourceID) != 41 || pageActions.Action[0].Movie.Operator != "Play" {
		t.Fatalf("影片动作未正确生成: %+v", pageActions)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateAttachmentAction(t *testing.T) {
	newWindow := false
	data, err := Marshal(Document{
		ID:          "attachment-action-test",
		Attachments: []Attachment{{ID: "att-1", Name: "说明", Format: "txt", Data: []byte("content")}},
		Actions:     []Action{{GotoA: &GotoAAction{AttachID: "att-1", NewWindow: &newWindow}}},
		Pages:       []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	action := ofd.Documents[0].Document.Actions.Actions[0]
	if action.GotoA == nil || action.GotoA.AttachID != "att-1" || action.GotoA.NewWindow {
		t.Fatalf("附件跳转动作未正确生成: %+v", action)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateActionRegion(t *testing.T) {
	data, err := Marshal(Document{
		ID: "action-region-test",
		Actions: []Action{{
			Region: &ActionRegion{Areas: []ActionArea{{
				Start: Point{X: 1, Y: 2},
				Commands: []RegionCommand{
					RegionMove{Point: Point{X: 3, Y: 4}},
					RegionLine{Point: Point{X: 10, Y: 4}},
					RegionQuadraticBezier{Control: Point{X: 12, Y: 5}, End: Point{X: 14, Y: 6}},
					RegionCubicBezier{Control1: Point{X: 15, Y: 7}, Control2: Point{X: 16, Y: 8}, End: Point{X: 17, Y: 9}},
					RegionArc{SweepDirection: true, LargeArc: false, RotationAngle: 45, EllipseSize: Point{X: 5, Y: 6}, EndPoint: Point{X: 20, Y: 10}},
					RegionClose{},
				},
			}}},
			URI: &URIAction{URI: "https://example.com"},
		}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	region := ofd.Documents[0].Document.Actions.Actions[0].Region
	if region == nil || len(region.Areas) != 1 || len(region.Areas[0].Paths) != 6 || region.Areas[0].Paths[4].EndPoint == nil {
		t.Fatalf("动作区域未正确生成: %+v", region)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateImageMediaAndCompositeReferences(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}
	data, err := Marshal(Document{
		ID:    "image-media-test",
		Media: []Media{{ID: 50, Type: "Image", Format: "png", Data: png}},
		Composites: []CompositeGraphicUnit{{
			ID: 60, Width: 20, Height: 20, Thumbnail: 50, Substitution: 50,
			Items: []Item{Path{X: 0, Y: 0, Width: 20, Height: 20, Data: "M 0 0 L 20 20 C", Stroke: true}},
		}},
		Pages: []Page{{Items: []Item{Image{X: 1, Y: 1, Width: 20, Height: 20, ResourceID: 50}, Composite{X: 1, Y: 1, Width: 20, Height: 20, ResourceID: 60}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	resource := ofd.Documents[0].DocumentResourceList()[0]
	if resource.MultiMedias == nil || len(resource.MultiMedias.MultiMedia) != 1 || resource.MultiMedias.MultiMedia[0].Type != "Image" {
		t.Fatalf("文档级图片多媒体未正确生成: %+v", resource.MultiMedias)
	}
	composite := resource.CompositeGraphicUnits.CompositeGraphicUnit[0]
	if uint64(composite.Thumbnail) != 50 || uint64(composite.Substitution) != 50 {
		t.Fatalf("复合图元图片引用未正确生成: %+v", composite)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateOutlinesAndBookmarks(t *testing.T) {
	data, err := Marshal(Document{
		ID: "navigation-test",
		Outlines: []Outline{{
			Title:    "第一章",
			Expanded: func() *bool { value := true; return &value }(),
			Actions:  []Action{{Goto: &GotoAction{Page: 1, Type: "FitH"}}},
			Children: []Outline{{
				Title:   "第一节",
				Actions: []Action{{URI: &URIAction{URI: "https://example.com/section"}}},
			}},
		}},
		Bookmarks: []Bookmark{{Name: "末页", Goto: GotoAction{Page: 1, Type: "Fit"}}},
		Pages:     []Page{{}, {}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	if ofd.Documents[0].Outlines == nil || len(ofd.Documents[0].Outlines.OutlineElems) != 1 {
		t.Fatalf("文档大纲未生成: %+v", ofd.Documents[0].Outlines)
	}
	outline := ofd.Documents[0].Outlines.OutlineElems[0]
	if outline.Title != "第一章" || outline.Expanded == nil || !*outline.Expanded || len(outline.Actions.Actions) != 1 || outline.Actions.Actions[0].Goto == nil || outline.Actions.Actions[0].Goto.Dest == nil || uint64(outline.Actions.Actions[0].Goto.Dest.PageID) != uint64(ofd.Documents[0].Pages[1].ID) {
		t.Fatalf("文档大纲属性未正确生成: %+v", outline)
	}
	if len(outline.OutlineElem) != 1 || outline.OutlineElem[0].Title != "第一节" || len(outline.OutlineElem[0].Actions.Actions) != 1 {
		t.Fatalf("嵌套大纲未正确生成: %+v", outline.OutlineElem)
	}
	if ofd.Documents[0].Bookmarks == nil || len(ofd.Documents[0].Bookmarks.Bookmarks) != 1 || ofd.Documents[0].Bookmarks.Bookmarks[0].Name != "末页" || uint64(ofd.Documents[0].Bookmarks.Bookmarks[0].Dest.PageID) != uint64(ofd.Documents[0].Pages[1].ID) {
		t.Fatalf("文档书签未正确生成: %+v", ofd.Documents[0].Bookmarks)
	}
	checkGeneratedPackage(t, data)
}

func TestCreatePermissionsAndViewPreferences(t *testing.T) {
	edit := false
	annot := true
	hideToolbar := true
	zoom := 1.25
	copies := 3
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC)
	data, err := Marshal(Document{
		ID: "document-settings-test",
		Permissions: &Permissions{
			Edit:        &edit,
			Annot:       &annot,
			Print:       &PrintSettings{Printable: true, Copies: &copies},
			ValidPeriod: &ValidPeriod{Start: start, End: end},
		},
		Preferences: &ViewPreferences{
			PageMode: PageModeFullScreen, PageLayout: PageLayoutTwoPageL,
			TabDisplay: TabDisplayFileName, HideToolbar: &hideToolbar, Zoom: &zoom,
		},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	document := ofd.Documents[0]
	if document.Permissions == nil || document.Permissions.Edit == nil || *document.Permissions.Edit || document.Permissions.Annot == nil || !*document.Permissions.Annot {
		t.Fatalf("权限未正确生成: %+v", document.Permissions)
	}
	if document.Permissions.Print == nil || !document.Permissions.Print.Printable || document.Permissions.Print.Copies != 3 || document.Permissions.ValidPeriod == nil || document.Permissions.ValidPeriod.StartDate.IsZero() || document.Permissions.ValidPeriod.EndDate.IsZero() {
		t.Fatalf("打印或有效期设置未正确生成: %+v", document.Permissions)
	}
	if document.VPreferences == nil || document.VPreferences.PageMode == nil || string(*document.VPreferences.PageMode) != PageModeFullScreen || document.VPreferences.PageLayout == nil || string(*document.VPreferences.PageLayout) != PageLayoutTwoPageL || document.VPreferences.TabDisplay == nil || string(*document.VPreferences.TabDisplay) != TabDisplayFileName || document.VPreferences.HideToolbar == nil || !*document.VPreferences.HideToolbar {
		t.Fatalf("视图首选项未正确生成: %+v", document.VPreferences)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var documentXML []byte
	for _, file := range archive.File {
		if file.Name != "Doc_0/Document.xml" {
			continue
		}
		reader, readErr := file.Open()
		if readErr != nil {
			t.Fatal(readErr)
		}
		documentXML, readErr = io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
	}
	if !bytes.Contains(documentXML, []byte("<Zoom>1.25</Zoom>")) {
		t.Fatalf("缩放比例 XML 未正确生成: %+v", document.VPreferences.Zoom)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateTemplatePages(t *testing.T) {
	data, err := Marshal(Document{
		ID: "template-test",
		Templates: []TemplatePage{{
			ID: 10, Name: "页眉模板", ZOrder: "Background",
			Items: []Item{Path{X: 1, Y: 1, Width: 20, Height: 5, Data: "M 0 0 L 20 0 L 20 5 C", Fill: true}},
		}},
		Pages: []Page{{
			Templates: []TemplateRef{{ID: 10, ZOrder: "Background"}},
			Items:     []Item{Text{X: 10, Y: 20, Width: 40, Height: 8, Value: "模板页面正文"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	document := ofd.Documents[0]
	if document.GetTemplate(10) == nil {
		t.Fatal("模板页内容加载失败")
	}
	if len(document.CommonData.TemplatePages) != 1 || uint64(document.CommonData.TemplatePages[0].ID) != 10 || document.CommonData.TemplatePages[0].Name == nil || *document.CommonData.TemplatePages[0].Name != "页眉模板" || document.CommonData.TemplatePages[0].ZOrder != "Background" {
		t.Fatalf("模板页定义未正确生成: %+v", document.CommonData.TemplatePages)
	}
	if document.GetTemplate(10) == nil || document.GetTemplate(10).Content == nil || len(document.GetTemplate(10).Content.Layer) != 1 || len(document.GetTemplate(10).Content.Layer[0].PathObject) != 1 {
		t.Fatalf("模板页内容未正确生成")
	}
	if len(document.Pages[0].Template()) != 1 || uint64(document.Pages[0].Template()[0].TemplateID) != 10 || document.Pages[0].Template()[0].ZOrder != "Background" {
		t.Fatalf("页面模板引用未正确生成: %+v", document.Pages[0].Template())
	}
	checkGeneratedPackage(t, data)
}

func TestCreateCompositeGraphicUnit(t *testing.T) {
	data, err := Marshal(Document{
		ID: "composite-test",
		Composites: []CompositeGraphicUnit{{
			ID: 20, Width: 40, Height: 30,
			Items: []Item{Path{X: 0, Y: 0, Width: 40, Height: 30, Data: "M 0 0 L 40 0 L 40 30 C", Fill: true}},
		}},
		Pages: []Page{{Items: []Item{
			Composite{X: 10, Y: 20, Width: 40, Height: 30, ResourceID: 20, CTM: &CTM{1, 0, 0, 1, 2, 3}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	document := ofd.Documents[0]
	if len(document.DocumentResourceList()) != 1 || document.DocumentResourceList()[0].CompositeGraphicUnits == nil || len(document.DocumentResourceList()[0].CompositeGraphicUnits.CompositeGraphicUnit) != 1 {
		t.Fatalf("复合图元资源未生成: %+v", document.DocumentResourceList())
	}
	resource := document.DocumentResourceList()[0].CompositeGraphicUnits.CompositeGraphicUnit[0]
	if uint64(resource.ID) != 20 || resource.Width != 40 || resource.Height != 30 || len(resource.Content.PathObject) != 1 {
		t.Fatalf("复合图元资源内容错误: %+v", resource)
	}
	object := document.Pages[0].Content().Layer[0].CompositeObject[0]
	if uint64(object.ResourceID) != 20 || object.CTM == nil || object.CTM[4] != 2 {
		t.Fatalf("复合图元对象错误: %+v", object)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateAdvancedColors(t *testing.T) {
	data, err := Marshal(Document{
		ID:          "advanced-color-test",
		ColorSpaces: []ColorSpace{{ID: 30, Type: "RGB", BitsPerComponent: 8}},
		DrawParams:  []DrawParam{{Name: "gradient", FillColor: &Color{ColorSpace: 30, Axial: &AxialShading{StartPoint: "0 0", EndPoint: "20 20", Segments: []ColorStop{{Position: 0, Color: Color{R: 255, G: 0, B: 0}}, {Position: 1, Color: Color{R: 0, G: 0, B: 255}}}}}}},
		Pages: []Page{{Items: []Item{
			Path{X: 1, Y: 1, Width: 20, Height: 20, Data: "M 0 0 L 20 0 L 20 20 C", Fill: true, FillColor: &Color{ColorSpace: 30, Components: []int{51, 102, 153}}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	res := ofd.Documents[0].DocumentResourceList()[0]
	if res.ColorSpaces == nil || len(res.ColorSpaces.ColorSpace) != 1 || uint64(res.ColorSpaces.ColorSpace[0].ID) != 30 {
		t.Fatalf("颜色空间未正确生成: %+v", res.ColorSpaces)
	}
	path := ofd.Documents[0].Pages[0].Content().Layer[0].PathObject[0]
	if path.FillColor == nil || uint64(path.FillColor.ColorSpace) != 30 || path.FillColor.Value == nil || path.FillColor.Value.R != 51 {
		t.Fatalf("高级颜色值未正确生成: %+v", path.FillColor)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateDefaultColorSpace(t *testing.T) {
	data, err := Marshal(Document{
		ID:          "default-color-space-test",
		ColorSpaces: []ColorSpace{{ID: 30, Type: "RGB", BitsPerComponent: 8}},
		DefaultCS:   30,
		Pages:       []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	defaultCS := ofd.Documents[0].Document.CommonData.DefaultCS
	if defaultCS == nil || uint64(*defaultCS) != 30 {
		t.Fatalf("默认颜色空间未正确生成: %+v", defaultCS)
	}
	checkGeneratedPackage(t, data)
}

func TestCreatePublicResource(t *testing.T) {
	data, err := Marshal(Document{
		ID: "public-resource-test",
		PublicRes: []PublicResource{{
			Name: "Public/SharedRes.xml",
			Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."></Res>`),
		}},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	publicRes := ofd.Documents[0].Document.CommonData.PublicRes
	if len(publicRes) != 1 || publicRes[0].String() != "Public/SharedRes.xml" {
		t.Fatalf("公共资源引用未正确生成: %+v", publicRes)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateColorSpaceProfile(t *testing.T) {
	data, err := Marshal(Document{
		ID:          "color-profile-test",
		ColorSpaces: []ColorSpace{{ID: 30, Type: "CMYK", BitsPerComponent: 8, ProfileData: []byte("ICC profile")}},
		Pages:       []Page{{Items: []Item{Path{X: 1, Y: 1, Width: 20, Height: 20, Data: "M 0 0 L 20 0 L 20 20 C", Fill: true, FillColor: &Color{ColorSpace: 30, Components: []int{1, 2, 3, 4}}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	space := ofd.Documents[0].DocumentResourceList()[0].ColorSpaces.ColorSpace[0]
	if space.Profile == "" || !strings.HasPrefix(space.Profile.String(), "Profiles/") {
		t.Fatalf("颜色空间 Profile 未生成: %+v", space)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateColorSpaceProfilesHaveDeterministicEntryOrder(t *testing.T) {
	document := Document{
		ID: "deterministic-color-profiles",
		ColorSpaces: []ColorSpace{
			{ID: 10, Type: "RGB", ProfileData: []byte("profile-a")},
			{ID: 20, Type: "RGB", ProfileData: []byte("profile-b")},
		},
		Pages: []Page{{}},
	}
	first, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("Marshal output changed for the same color profile input")
	}
	if document.ColorSpaces[0].Profile != "" || document.ColorSpaces[1].Profile != "" {
		t.Fatalf("Marshal modified color space profiles: %+v", document.ColorSpaces)
	}
	checkGeneratedPackage(t, first)
}

func TestCreateColorSpacesReuseIdenticalProfiles(t *testing.T) {
	data, err := Marshal(Document{
		ID: "shared-color-profile",
		ColorSpaces: []ColorSpace{
			{ID: 10, Type: "RGB", ProfileData: []byte("shared-profile")},
			{ID: 20, Type: "RGB", ProfileData: []byte("shared-profile")},
		},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	profileCount := 0
	for _, file := range archive.File {
		if strings.HasPrefix(file.Name, "Doc_0/Res/Profiles/") {
			profileCount++
		}
	}
	if profileCount != 1 {
		t.Fatalf("expected one shared color profile entry, got %d", profileCount)
	}
	checkGeneratedPackage(t, data)
}

func TestCreatePatternColor(t *testing.T) {
	data, err := Marshal(Document{
		ID: "pattern-color-test",
		Pages: []Page{{Items: []Item{
			Path{X: 1, Y: 1, Width: 30, Height: 30, Data: "M 0 0 L 30 0 L 30 30 C", Fill: true, FillColor: &Color{
				Pattern: &Pattern{
					Width: 10, Height: 10, XStep: 10, YStep: 10, ReflectMethod: "RowAndColumn", RelativeTo: "Object",
					Items: []Item{Path{X: 0, Y: 0, Width: 10, Height: 10, Data: "M 0 0 L 10 10 C", Fill: true}},
				},
			}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	color := ofd.Documents[0].Pages[0].Content().Layer[0].PathObject[0].FillColor
	if color == nil || color.Pattern == nil || color.Pattern.Width != 10 || color.Pattern.ReflectMethod != "RowAndColumn" || len(color.Pattern.CellContent.PathObject) != 1 {
		t.Fatalf("图案颜色未正确生成: %+v", color)
	}
	checkGeneratedPackage(t, data)
}

func TestMarshalDoesNotPersistPatternCache(t *testing.T) {
	pattern := &Pattern{
		Width:  10,
		Height: 10,
		Items: []Item{Path{
			X: 1, Y: 1, Width: 8, Height: 8,
			Data: "M 0 0 L 8 8 C", Fill: true,
		}},
	}
	document := Document{
		ID: "pattern-cache-test",
		Pages: []Page{{Items: []Item{Path{
			X: 1, Y: 1, Width: 20, Height: 20,
			Data: "M 0 0 L 20 0 L 20 20 C", Fill: true,
			FillColor: &Color{Pattern: pattern},
		}}}},
	}
	first, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if pattern.prepared || pattern.building || pattern.builtState != nil || pattern.builtItems != nil {
		t.Fatal("Marshal persisted Pattern build state")
	}
	second, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("Marshal output changed for the same pattern input")
	}
	checkGeneratedPackage(t, first)
}

func TestCreateRadialColor(t *testing.T) {
	data, err := Marshal(Document{
		ID: "radial-color-test",
		Pages: []Page{{Items: []Item{
			Path{X: 1, Y: 1, Width: 20, Height: 20, Data: "M 0 0 L 20 0 L 20 20 C", Fill: true, FillColor: &Color{Radial: &RadialShading{
				StartPoint: "10 10", EndPoint: "10 10", EndRadius: 10,
				Segments: []ColorStop{{Position: 0, Color: Color{R: 255, G: 255, B: 255}}, {Position: 1, Color: Color{R: 0, G: 0, B: 0}}},
			}}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	color := ofd.Documents[0].Pages[0].Content().Layer[0].PathObject[0].FillColor
	if color == nil || color.RadialShd == nil || len(color.RadialShd.Segment) != 2 || color.RadialShd.EndRadius != 10 {
		t.Fatalf("径向渐变未正确生成: %+v", color)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateMeshColors(t *testing.T) {
	data, err := Marshal(Document{
		ID: "mesh-color-test",
		Pages: []Page{{Items: []Item{
			Path{X: 1, Y: 1, Width: 20, Height: 20, Data: "M 0 0 L 20 0 L 20 20 C", Fill: true, FillColor: &Color{Gouraud: &GouraudShading{
				Points: []GouraudPoint{{X: 0, Y: 0, Color: Color{R: 255, G: 0, B: 0}}, {X: 20, Y: 0, Color: Color{R: 0, G: 255, B: 0}}, {X: 0, Y: 20, Color: Color{R: 0, G: 0, B: 255}}}}}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	color := ofd.Documents[0].Pages[0].Content().Layer[0].PathObject[0].FillColor
	if color == nil || color.GouraudShd == nil || len(color.GouraudShd.Point) != 3 {
		t.Fatalf("Gouraud 渐变未正确生成: %+v", color)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateAnnotations(t *testing.T) {
	visible := true
	data, err := Marshal(Document{
		ID: "annotation-test",
		Annotations: []AnnotationPage{{Page: 0, Items: []Annotation{{
			ID: 50, Type: "Highlight", Creator: "creator-test", LastModDate: time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC),
			Visible: &visible, Remark: "重点内容", Parameters: []AnnotationParameter{{Name: "Color", Value: "yellow"}},
			Boundary: &Box{X: 10, Y: 20, Width: 30, Height: 8},
			Items:    []Item{Path{X: 10, Y: 20, Width: 30, Height: 8, Data: "M 0 0 L 30 0 L 30 8 C", Fill: true}},
		}}}},
		Pages: []Page{{Items: []Item{Text{X: 10, Y: 10, Width: 40, Height: 8, Value: "正文"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	pageID := ofd.Documents[0].Pages[0].ID
	pageAnnot := ofd.Documents[0].GetAnnotation(pageID)
	if pageAnnot == nil || len(pageAnnot.Annots) != 1 {
		t.Fatalf("页面注解未生成: %+v", pageAnnot)
	}
	annotation := pageAnnot.Annots[0]
	if annotation.ID != "50" || annotation.Type != "Highlight" || annotation.Creator != "creator-test" || annotation.Remark == nil || *annotation.Remark != "重点内容" || annotation.Appearance == nil || len(annotation.Appearance.PathObject) != 1 {
		t.Fatalf("注解内容未正确生成: %+v", annotation)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateAnnotationsUsesDeterministicDefaultDate(t *testing.T) {
	document := Document{
		ID: "annotation-default-date",
		Annotations: []AnnotationPage{{Page: 0, Items: []Annotation{{
			ID: 1, Type: "Path", Creator: "creator-test",
			Items: []Item{Path{X: 0, Y: 0, Width: 1, Height: 1, Data: "M 0 0 L 1 1 C"}},
		}}}},
		Pages: []Page{{}},
	}
	first, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("Marshal output changed for the same annotation input")
	}
	checkGeneratedPackage(t, first)
}

func TestCreateGraphicCTMAndPathClips(t *testing.T) {
	data, err := Marshal(Document{
		ID:         "clip-test",
		DrawParams: []DrawParam{{Name: "clip-line", LineWidth: 0.2}},
		Pages: []Page{{Items: []Item{
			Path{
				X: 1, Y: 1, Width: 40, Height: 30, Data: "M 0 0 L 40 30 C",
				CTM: &CTM{1, 0, 0, 1, 5, 6},
				Clips: &Clips{
					Items: []Clip{{Areas: []ClipArea{{
						DrawParam: " clip-line ",
						CTM:       &CTM{1, 0, 0, 1, 1, 2},
						Path: &ClipPath{
							Boundary: Box{X: 0, Y: 0, Width: 20, Height: 20},
							CTM:      &CTM{1, 0, 0, 1, 3, 4},
							Data:     "M 0 0 L 20 0 L 20 20 C",
							Fill:     true,
						},
					}}}},
				},
			},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	path := ofd.Documents[0].Pages[0].Content().Layer[0].PathObject[0]
	if path.CTM == nil || path.CTM[4] != 5 || path.CTM[5] != 6 {
		t.Fatalf("路径 CTM 未正确生成: %+v", path.CTM)
	}
	if path.Clips == nil || len(path.Clips.Clip) != 1 || len(path.Clips.Clip[0].Area) != 1 {
		t.Fatalf("路径裁剪未正确生成: %+v", path.Clips)
	}
	area := path.Clips.Clip[0].Area[0]
	if area.DrawParam == nil || uint64(*area.DrawParam) != 1 || area.CTM == nil || area.CTM[4] != 1 || area.Path == nil || area.Path.CTM == nil || area.Path.CTM[5] != 4 || area.Path.Fill != true {
		t.Fatalf("裁剪区域属性未正确生成: %+v", area)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateTextClip(t *testing.T) {
	data, err := Marshal(Document{
		ID: "text-clip-test",
		Pages: []Page{{Items: []Item{
			Text{
				X: 1, Y: 1, Width: 40, Height: 10, Value: "主体文字",
				Clips: &Clips{Items: []Clip{{Areas: []ClipArea{{
					CTM:  &CTM{1, 0, 0, 1, 2, 3},
					Text: &ClipText{Boundary: Box{X: 0, Y: 0, Width: 20, Height: 5}, CTM: &CTM{1, 0, 0, 1, 4, 5}, Font: "ClipFont", Value: "裁剪文字"},
				}}}}},
			},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	text := ofd.Documents[0].Pages[0].Content().Layer[0].TextObject[0]
	area := text.Clips.Clip[0].Area[0]
	if area.Text == nil || uint64(area.Text.Font) == 0 || len(area.Text.TextCode) != 1 || area.Text.TextCode[0].Value != "裁剪文字" || area.CTM == nil || area.CTM[5] != 3 || area.Text.CTM == nil || area.Text.CTM[4] != 4 {
		t.Fatalf("文字裁剪未正确生成: area=%+v text=%+v", area, area.Text)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateImageCTMAndClips(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}
	data, err := Marshal(Document{
		ID: "image-ctm-test",
		Pages: []Page{{Items: []Item{
			Image{
				X: 1, Y: 2, Width: 30, Height: 20, Data: png,
				CTM: &CTM{1, 0, 0, 1, 7, 8},
				Clips: &Clips{Items: []Clip{{Areas: []ClipArea{{
					Path: &ClipPath{Boundary: Box{Width: 10, Height: 10}, Data: "M 0 0 L 10 10 C"},
				}}}}},
			},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	image := ofd.Documents[0].Pages[0].Content().Layer[0].ImageObject[0]
	if image.CTM == nil || image.CTM[4] != 7 || image.CTM[5] != 8 || image.Clips == nil || image.Clips.Clip[0].Area[0].Path == nil {
		t.Fatalf("图片 CTM 或裁剪未正确生成: %+v", image)
	}
	checkGeneratedPackage(t, data)
}

func TestCreatePathAndImageWritesGraphicAttributes(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}
	alpha := uint8(128)
	visible := false
	data, err := Marshal(Document{
		ID: "graphic-style-test",
		Pages: []Page{{Items: []Item{
			Path{
				X: 1, Y: 2, Width: 30, Height: 20,
				Data: "M 0 0 L 10 10 C", StrokeSet: &visible, Fill: true,
				Rule: "Even-Odd", Name: "outline", Visible: &visible,
				LineWidth: 0.5, Cap: "Round", Join: "Bevel", MiterLimit: 2,
				DashOffset: 1, DashPattern: []float64{2, 1}, Alpha: &alpha,
			},
			Image{
				X: 2, Y: 3, Width: 20, Height: 15, Data: png,
				Name: "photo", Visible: &visible, LineWidth: 0.25,
				Cap: "Square", Join: "Round", DashPattern: []float64{1, 2},
				Border: &ImageBorder{LineWidth: 0.3, HorizontalRadius: 1, VerticalRadius: 2, DashOffset: 0.5, DashPattern: []float64{3, 1}},
			},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	layer := ofd.Documents[0].Pages[0].Content().Layer[0]
	path := layer.PathObject[0]
	if path.Name != "outline" || path.Visible.Value(true) || path.Stroke.Value(true) || !path.Fill || path.Rule != "Even-Odd" {
		t.Fatalf("路径属性未正确生成: %+v", path)
	}
	if path.LineWidth != 0.5 || path.Cap != "Round" || path.Join != "Bevel" || path.MiterLimit != 2 || path.DashOffset != 1 || path.Alpha == nil || *path.Alpha != alpha {
		t.Fatalf("路径线条属性未正确生成: %+v", path.CTGraphicUnit)
	}

	image := layer.ImageObject[0]
	if image.Name != "photo" || image.Visible.Value(true) || image.LineWidth != 0.25 || image.Cap != "Square" || image.Join != "Round" {
		t.Fatalf("图片属性未正确生成: %+v", image)
	}
	if image.Border == nil || image.Border.LineWidth != 0.3 || image.Border.HorizonalCornerRadius != 1 || image.Border.VerticalCornerRadius != 2 || image.Border.DashOffset != 0.5 {
		t.Fatalf("图片边框属性未正确生成: %+v", image.Border)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateImageResourceReferences(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}
	data, err := Marshal(Document{
		ID: "image-reference-test",
		Pages: []Page{{
			Resources: []PageResource{{Images: []PageImage{{ID: 70, Format: "PNG", Data: png}}}},
			Items:     []Item{Image{X: 1, Y: 1, Width: 20, Height: 20, ResourceID: 70, Substitution: 70, ImageMask: 70}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	image := ofd.Documents[0].Pages[0].Content().Layer[0].ImageObject[0]
	if image.Substitution != 70 || image.ImageMask != 70 {
		t.Fatalf("图片替代资源或蒙版引用未正确生成: %+v", image)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateNestedPageBlock(t *testing.T) {
	data, err := Marshal(Document{
		ID: "page-block-test",
		Pages: []Page{{Items: []Item{
			PageBlock{Items: []Item{
				Text{X: 1, Y: 1, Width: 20, Height: 5, Value: "嵌套文字"},
				PageBlock{Items: []Item{
					Path{X: 0, Y: 0, Width: 10, Height: 10, Data: "M 0 0 L 10 10 C", Stroke: true},
				}},
			}},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	pageBlock := ofd.Documents[0].Pages[0].Content().Layer[0].PageBlock[0]
	if len(pageBlock.TextObject) != 1 || len(pageBlock.PageBlock) != 1 || len(pageBlock.PageBlock[0].PathObject) != 1 {
		t.Fatalf("嵌套 PageBlock 未正确生成: %+v", pageBlock)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateWritesColors(t *testing.T) {
	textAlpha := uint8(220)
	borderAlpha := uint8(180)
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3}
	data, err := Marshal(Document{
		ID: "color-test",
		Pages: []Page{{Items: []Item{
			Text{
				X: 1, Y: 2, Width: 20, Height: 8, Value: "颜色",
				FillColor:   &Color{R: 255, G: 0, B: 0, Alpha: &textAlpha},
				StrokeColor: &Color{R: 0, G: 0, B: 255},
			},
			Path{
				X: 1, Y: 12, Width: 20, Height: 8,
				Data: "M 0 0 L 10 10 C", Stroke: true,
				StrokeColor: &Color{R: 0, G: 255, B: 0},
				FillColor:   &Color{R: 255, G: 255, B: 0},
			},
			Image{
				X: 1, Y: 22, Width: 20, Height: 8, Data: png,
				Border: &ImageBorder{Color: &Color{R: 10, G: 20, B: 30, Alpha: &borderAlpha}},
			},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	layer := ofd.Documents[0].Pages[0].Content().Layer[0]
	text := layer.TextObject[0]
	if text.FillColor == nil || text.FillColor.Value == nil || text.FillColor.Value.R != 255 || text.FillColor.Value.G != 0 || text.FillColor.Value.B != 0 || text.FillColor.Alpha == nil || *text.FillColor.Alpha != textAlpha {
		t.Fatalf("文字填充颜色未正确生成: %+v", text.FillColor)
	}
	if text.StrokeColor == nil || text.StrokeColor.Value == nil || text.StrokeColor.Value.B != 255 {
		t.Fatalf("文字描边颜色未正确生成: %+v", text.StrokeColor)
	}

	path := layer.PathObject[0]
	if path.StrokeColor == nil || path.StrokeColor.Value == nil || path.StrokeColor.Value.G != 255 || path.FillColor == nil || path.FillColor.Value == nil || path.FillColor.Value.R != 255 {
		t.Fatalf("路径颜色未正确生成: %+v", path)
	}

	image := layer.ImageObject[0]
	if image.Border == nil || image.Border.BorderColor == nil || image.Border.BorderColor.Value == nil || image.Border.BorderColor.Value.R != 10 || image.Border.BorderColor.Value.G != 20 || image.Border.BorderColor.Value.B != 30 || image.Border.BorderColor.Alpha == nil || *image.Border.BorderColor.Alpha != borderAlpha {
		t.Fatalf("图片边框颜色未正确生成: %+v", image.Border)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateWritesPageAreaAndLayerType(t *testing.T) {
	data, err := Marshal(Document{
		ID: "page-area-test",
		Pages: []Page{{
			Area: &PageArea{
				PhysicalBox:    &Box{X: 0, Y: 0, Width: 210, Height: 297},
				ApplicationBox: &Box{X: 5, Y: 5, Width: 200, Height: 287},
				ContentBox:     &Box{X: 10, Y: 10, Width: 190, Height: 277},
				BleedBox:       &Box{X: -3, Y: -3, Width: 216, Height: 303},
			},
			LayerType: LayerForeground,
			Items: []Item{
				Path{X: 1, Y: 1, Width: 10, Height: 10, Data: "M 0 0 L 10 10 C"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	page := ofd.Documents[0].Pages[0]
	if page.Area() == nil {
		t.Fatal("页面区域未生成")
	}
	area := page.Area()
	if area.PhysicalBox.Width != 210 || area.PhysicalBox.Height != 297 || area.ApplicationBox == nil || area.ApplicationBox.X != 5 || area.ContentBox == nil || area.ContentBox.Width != 190 || area.BleedBox == nil || area.BleedBox.X != -3 {
		t.Fatalf("页面区域未正确生成: %+v", area)
	}
	if page.Content() == nil || len(page.Content().Layer) != 1 || page.Content().Layer[0].Type != LayerForeground {
		t.Fatalf("图层类型未正确生成: %+v", page.Content())
	}
	checkGeneratedPackage(t, data)
}

func TestCreateDocumentPageArea(t *testing.T) {
	data, err := Marshal(Document{
		ID: "document-area-test",
		Area: &PageArea{
			PhysicalBox:    &Box{X: 0, Y: 0, Width: 210, Height: 297},
			ApplicationBox: &Box{X: 5, Y: 5, Width: 200, Height: 287},
			ContentBox:     &Box{X: 10, Y: 10, Width: 190, Height: 277},
			BleedBox:       &Box{X: -3, Y: -3, Width: 216, Height: 303},
		},
		Pages: []Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	area := ofd.Documents[0].Document.CommonData.PageArea
	if area.ApplicationBox == nil || area.ApplicationBox.X != 5 || area.ContentBox == nil || area.ContentBox.Width != 190 || area.BleedBox == nil || area.BleedBox.X != -3 {
		t.Fatalf("文档页面区域未正确生成: %+v", area)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateMultipleLayersPreservesOrderAndIDs(t *testing.T) {
	data, err := Marshal(Document{
		ID:    "multi-layer-test",
		Fonts: []Font{{Name: "Test Sans"}},
		Pages: []Page{{
			Layers: []Layer{
				{
					Type: LayerBackground,
					Items: []Item{
						Path{X: 1, Y: 1, Width: 20, Height: 20, Data: "M 0 0 L 20 20 C"},
					},
				},
				{
					Type: LayerForeground,
					Items: []Item{
						Text{X: 2, Y: 2, Width: 20, Height: 5, Value: "前景", Font: "Test Sans"},
					},
				},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	document := ofd.Documents[0]
	if document.CommonData.MaxUnitID != 6 {
		t.Fatalf("MaxUnitID = %d, want 6", document.CommonData.MaxUnitID)
	}
	page := document.Pages[0]
	if page.Content() == nil || len(page.Content().Layer) != 2 {
		t.Fatalf("图层数量 = %d, want 2", len(page.Content().Layer))
	}
	layers := page.Content().Layer
	if layers[0].Type != LayerBackground || layers[1].Type != LayerForeground {
		t.Fatalf("图层顺序 = %q, %q", layers[0].Type, layers[1].Type)
	}
	if len(layers[0].PathObject) != 1 || len(layers[1].TextObject) != 1 {
		t.Fatalf("图层对象未按类型生成")
	}
	if layers[0].ID == layers[1].ID || layers[0].PathObject[0].ID == layers[1].TextObject[0].ID {
		t.Fatalf("图层或对象 ID 重复: %d, %d, %d, %d", layers[0].ID, layers[1].ID, layers[0].PathObject[0].ID, layers[1].TextObject[0].ID)
	}
	checkGeneratedPackage(t, data)
}

func TestCreateDrawParamsAndReferences(t *testing.T) {
	data, err := Marshal(Document{
		ID: "draw-param-test",
		DrawParams: []DrawParam{
			{
				Name:      "base",
				LineWidth: 0.5,
				Cap:       "Round",
				FillColor: &Color{R: 240, G: 240, B: 240},
			},
			{
				Name:        "accent",
				Relative:    "base",
				Join:        "Bevel",
				StrokeColor: &Color{R: 20, G: 40, B: 200},
			},
		},
		Pages: []Page{{
			Layers: []Layer{{
				Type:      LayerForeground,
				DrawParam: "accent",
				Items: []Item{
					Path{X: 1, Y: 1, Width: 20, Height: 20, Data: "M 0 0 L 20 20 C", DrawParam: "base"},
					Text{X: 2, Y: 2, Width: 20, Height: 5, Value: "绘制参数", DrawParam: "accent"},
				},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	ofd := newTestOFD(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	document := ofd.Documents[0]
	if len(document.DocumentResourceList()) != 1 || document.DocumentResourceList()[0].DrawParams == nil || len(document.DocumentResourceList()[0].DrawParams.DrawParam) != 2 {
		t.Fatalf("绘制参数资源未生成: %+v", document.DocumentResourceList())
	}
	params := document.DocumentResourceList()[0].DrawParams.DrawParam
	if params[0].LineWidth != 0.5 || params[0].Cap != "Round" || uint64(params[1].Relative) != uint64(params[0].ID) || params[1].Join != "Bevel" {
		t.Fatalf("绘制参数内容未正确生成: %+v", params)
	}
	layer := document.Pages[0].Content().Layer[0]
	if uint64(layer.DrawParam) != uint64(params[1].ID) {
		t.Fatalf("图层 DrawParam = %d, want %d", layer.DrawParam, params[1].ID)
	}
	if uint64(layer.PathObject[0].DrawParam) != uint64(params[0].ID) || uint64(layer.TextObject[0].DrawParam) != uint64(params[1].ID) {
		t.Fatalf("图元 DrawParam 引用错误: path=%d text=%d", layer.PathObject[0].DrawParam, layer.TextObject[0].DrawParam)
	}
	checkGeneratedPackage(t, data)
}

func checkGeneratedPackage(t *testing.T, data []byte) {
	t.Helper()
	v, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(context.Background(), bytes.NewReader(data), "generated.ofd")
	if report.HasErrors() {
		t.Fatalf("generated OFD failed validation: %+v", report.Issues)
	}
}

func testEmbeddedFontData(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../test/testdata/intro.ofd")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range archive.File {
		if file.Name != "Doc_0/Res/font_83_83.cff" {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		fontData, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		return fontData
	}
	t.Fatal("test embedded font is missing")
	return nil
}

func readEmbeddedFontData(t *testing.T, data []byte) []byte {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range archive.File {
		if !strings.HasPrefix(file.Name, "Doc_0/Res/Fonts/") {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		fontData, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		return fontData
	}
	t.Fatal("embedded font is missing")
	return nil
}

func readArchiveEntry(t *testing.T, data []byte, name string) []byte {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range archive.File {
		if file.Name != name {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, readErr := io.ReadAll(reader)
		_ = reader.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		return content
	}
	t.Fatalf("ZIP entry %q not found", name)
	return nil
}

func TestCreateRejectsInvalidDocument(t *testing.T) {
	if _, err := Marshal(Document{ID: "missing-pages"}); err == nil {
		t.Fatal("Marshal accepted a document without pages")
	}
	if _, err := Marshal(Document{
		ID:           "invalid-document-date",
		CreationDate: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
		Pages:        []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a document date outside the XML Schema range")
	}
	if _, err := Marshal(Document{
		ID: "invalid-image",
		Pages: []Page{{Items: []Item{
			Image{X: 0, Y: 0, Width: 10, Height: 10, Data: []byte("not-an-image")},
		}}},
	}); err == nil {
		t.Fatal("Marshal accepted an unrecognized image")
	}
	if _, err := Marshal(Document{
		ID:    "invalid-font",
		Fonts: []Font{{Name: "Test", Format: "../../bad", Data: []byte{1}}},
		Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an unsafe font format")
	}
	if _, err := Marshal(Document{
		ID: "invalid-text-scale",
		Pages: []Page{{Items: []Item{
			Text{X: 0, Y: 0, Width: 1, Height: 1, Value: "文字", HScale: 1.1},
		}}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid text scale")
	}
	if _, err := Marshal(Document{
		ID:    "invalid-layer",
		Pages: []Page{{LayerType: "Unknown"}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid layer type")
	}
	if _, err := Marshal(Document{
		ID: "invalid-page-area",
		Pages: []Page{{Area: &PageArea{
			ContentBox: &Box{X: 0, Y: 0, Width: -1, Height: 10},
		}}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid page area")
	}
	if _, err := Marshal(Document{
		ID: "invalid-layers",
		Pages: []Page{{
			LayerType: LayerForeground,
			Layers:    []Layer{{Type: LayerBackground}},
		}},
	}); err == nil {
		t.Fatal("Marshal accepted mixed layer configuration")
	}
	if _, err := Marshal(Document{
		ID:         "invalid-draw-param-reference",
		DrawParams: []DrawParam{{Name: "base", Relative: "missing"}},
		Pages:      []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a missing draw parameter relation")
	}
	if _, err := Marshal(Document{
		ID: "invalid-draw-param-cycle",
		DrawParams: []DrawParam{
			{Name: "a", Relative: "b"},
			{Name: "b", Relative: "a"},
		},
		Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a draw parameter cycle")
	}
	if _, err := Marshal(Document{
		ID: "invalid-text-code",
		Pages: []Page{{Items: []Item{
			Text{X: 0, Y: 0, Width: 10, Height: 5, TextCodes: []TextCode{{Value: "", DeltaX: []float64{math.NaN()}}}},
		}}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid TextCode")
	}
	if _, err := Marshal(Document{
		ID: "invalid-cg-transform",
		Pages: []Page{{Items: []Item{
			Text{X: 0, Y: 0, Width: 10, Height: 5, Value: "文字", CGTransforms: []CGTransform{{CodePosition: 1, Glyphs: []int{70000}}}},
		}}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid CGTransform")
	}
	if _, err := Marshal(Document{
		ID:    "invalid-action-target",
		Pages: []Page{{Actions: []Action{{Goto: &GotoAction{Page: 1}}}}},
	}); err == nil {
		t.Fatal("Marshal accepted an out-of-range action target")
	}
	if _, err := Marshal(Document{
		ID:    "invalid-action-choice",
		Pages: []Page{{Actions: []Action{{URI: &URIAction{URI: "https://example.com"}, Goto: &GotoAction{Page: 0}}}}},
	}); err == nil {
		t.Fatal("Marshal accepted an action with two targets")
	}
	if _, err := Marshal(Document{
		ID: "invalid-clip",
		Pages: []Page{{Items: []Item{
			Path{X: 0, Y: 0, Width: 10, Height: 10, Data: "M 0 0 L 10 10 C", Clips: &Clips{Items: []Clip{{Areas: []ClipArea{{}}}}}},
		}}},
	}); err == nil {
		t.Fatal("Marshal accepted an empty clip area")
	}
	if _, err := Marshal(Document{
		ID:         "invalid-clip-draw-param",
		DrawParams: []DrawParam{{Name: "known"}},
		Pages: []Page{{Items: []Item{
			Path{X: 0, Y: 0, Width: 10, Height: 10, Data: "M 0 0 L 10 10 C", Clips: &Clips{Items: []Clip{{Areas: []ClipArea{{DrawParam: "missing", Path: &ClipPath{Boundary: Box{Width: 1, Height: 1}, Data: "M 0 0 L 1 1 C"}}}}}}},
		}}},
	}); err == nil {
		t.Fatal("Marshal accepted an unresolved clip DrawParam")
	}
	if _, err := Marshal(Document{
		ID: "invalid-ctm",
		Pages: []Page{{Items: []Item{
			Path{X: 0, Y: 0, Width: 10, Height: 10, Data: "M 0 0 L 10 10 C", CTM: &CTM{1, 0, 0, 1, math.NaN(), 0}},
		}}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid CTM")
	}
	if _, err := Marshal(Document{
		ID:       "invalid-outline",
		Outlines: []Outline{{Title: "", Actions: []Action{{Goto: &GotoAction{Page: 3}}}}},
		Pages:    []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid outline")
	}
	if _, err := Marshal(Document{
		ID:        "invalid-bookmark",
		Bookmarks: []Bookmark{{Name: "书签", Goto: GotoAction{Page: 1}}},
		Pages:     []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid bookmark target")
	}
	if _, err := Marshal(Document{
		ID:          "invalid-preferences",
		Preferences: &ViewPreferences{ZoomMode: ZoomModeFitWidth, Zoom: func() *float64 { value := 1.0; return &value }()},
		Pages:       []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted both ZoomMode and Zoom")
	}
	if _, err := Marshal(Document{
		ID:          "invalid-permissions",
		Permissions: &Permissions{Print: &PrintSettings{Copies: func() *int { value := -2; return &value }()}},
		Pages:       []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid print copy count")
	}
	if _, err := Marshal(Document{
		ID:    "resource-id-overflow",
		Media: []Media{{ID: ^uint64(0), Type: "Image", Name: "image.png", Format: "PNG", Data: []byte{1}}},
		Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an overflowing resource ID")
	}
	if _, err := Marshal(Document{
		ID:    "resource-id-over-uint32",
		Media: []Media{{ID: uint64(^uint32(0)) + 1, Type: "Image", Name: "image.png", Format: "PNG", Data: []byte{1}}},
		Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a resource ID outside the OFD unsignedInt range")
	}
	if _, err := Marshal(Document{
		ID:         "extension-refid-over-uint32",
		Extensions: []Extension{{AppName: "test", RefID: uint64(^uint32(0)) + 1, Data: "<Data/>"}},
		Pages:      []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an extension RefID outside the OFD unsignedInt range")
	}
	if _, err := Marshal(Document{
		ID:    "invalid-template-reference",
		Pages: []Page{{Templates: []TemplateRef{{ID: 99}}}},
	}); err == nil {
		t.Fatal("Marshal accepted an unresolved template reference")
	}
	if _, err := Marshal(Document{
		ID:        "invalid-template-order",
		Templates: []TemplatePage{{ID: 10, ZOrder: "Middle"}},
		Pages:     []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid template z-order")
	}
	if _, err := Marshal(Document{
		ID:    "invalid-composite-reference",
		Pages: []Page{{Items: []Item{Composite{X: 0, Y: 0, Width: 10, Height: 10, ResourceID: 99}}}},
	}); err == nil {
		t.Fatal("Marshal accepted an unresolved composite resource")
	}
	if _, err := Marshal(Document{
		ID:         "invalid-composite-resource",
		Composites: []CompositeGraphicUnit{{ID: 10, Width: 0, Height: 10}},
		Pages:      []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid composite resource")
	}
	if _, err := Marshal(Document{
		ID:          "invalid-color-space",
		ColorSpaces: []ColorSpace{{ID: 10, Type: "LAB"}},
		Pages:       []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid color space")
	}
	if _, err := Marshal(Document{
		ID:    "invalid-gradient",
		Pages: []Page{{Items: []Item{Path{X: 0, Y: 0, Width: 10, Height: 10, Data: "M 0 0 L 10 10 C", FillColor: &Color{Axial: &AxialShading{StartPoint: "0 0", EndPoint: "1 1", Segments: []ColorStop{{Position: 0, Color: Color{R: 0, G: 0, B: 0}}}}}}}}},
	}); err == nil {
		t.Fatal("Marshal accepted an incomplete gradient")
	}
	if _, err := Marshal(Document{
		ID:    "invalid-gradient-position",
		Pages: []Page{{Items: []Item{Path{X: 0, Y: 0, Width: 10, Height: 10, Data: "M 0 0 L 10 10 C", FillColor: &Color{Axial: &AxialShading{StartPoint: "0", EndPoint: "1 1", Segments: []ColorStop{{Position: 0, Color: Color{R: 0, G: 0, B: 0}}, {Position: 1.5, Color: Color{R: 255}}}}}}}}},
	}); err == nil {
		t.Fatal("Marshal accepted invalid gradient position data")
	}
	if _, err := Marshal(Document{
		ID:    "invalid-color-reference",
		Pages: []Page{{Items: []Item{Path{X: 0, Y: 0, Width: 10, Height: 10, Data: "M 0 0 L 10 10 C", FillColor: &Color{ColorSpace: 99, Components: []int{1, 2, 3}}}}}},
	}); err == nil {
		t.Fatal("Marshal accepted an unresolved color space reference")
	}
	if _, err := Marshal(Document{
		ID:    "invalid-mesh-gradient",
		Pages: []Page{{Items: []Item{Path{X: 0, Y: 0, Width: 10, Height: 10, Data: "M 0 0 L 10 10 C", FillColor: &Color{Gouraud: &GouraudShading{Points: []GouraudPoint{{Color: Color{R: 1}}}}}}}}},
	}); err == nil {
		t.Fatal("Marshal accepted an incomplete mesh gradient")
	}
	if _, err := Marshal(Document{
		ID:          "invalid-annotation-page",
		Annotations: []AnnotationPage{{Page: 2, Items: []Annotation{{ID: 1, Type: "Link", Creator: "test"}}}},
		Pages:       []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid annotation page")
	}
	if _, err := Marshal(Document{
		ID:          "invalid-annotation-type",
		Annotations: []AnnotationPage{{Page: 0, Items: []Annotation{{ID: 1, Type: "Unknown", Creator: "test"}}}},
		Pages:       []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid annotation type")
	}
	if _, err := Marshal(Document{
		ID: "invalid-pattern",
		Pages: []Page{{Items: []Item{Path{
			X: 0, Y: 0, Width: 10, Height: 10, Data: "M 0 0 L 10 10 C", Fill: true,
			FillColor: &Color{Pattern: &Pattern{Width: 0, Height: 10, Items: []Item{Path{X: 0, Y: 0, Width: 1, Height: 1, Data: "M 0 0 L 1 1 C"}}}},
		}}}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid pattern")
	}
	if _, err := Marshal(Document{
		ID:    "invalid-media-type",
		Media: []Media{{ID: 1, Type: "Text", Data: []byte("data")}},
		Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid media type")
	}
	if _, err := Marshal(Document{
		ID:    "invalid-media-format",
		Media: []Media{{ID: 1, Type: "Audio", Format: "../mp3", Data: []byte("data")}},
		Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a media format containing a path")
	}
	if _, err := Marshal(Document{
		ID:    "missing-media-reference",
		Pages: []Page{{Actions: []Action{{Sound: &SoundAction{ResourceID: 99}}}}},
	}); err == nil {
		t.Fatal("Marshal accepted a missing media reference")
	}
	if _, err := Marshal(Document{
		ID:    "missing-page-resource-reference",
		Pages: []Page{{Items: []Item{Image{X: 0, Y: 0, Width: 10, Height: 10, ResourceID: 99}}}},
	}); err == nil {
		t.Fatal("Marshal accepted a missing page resource reference")
	}
	if _, err := Marshal(Document{
		ID:    "invalid-page-resource",
		Pages: []Page{{Resources: []PageResource{{Images: []PageImage{{ID: 1, Format: "PNG", Data: []byte("data")}}}}}},
	}); err != nil {
		t.Fatalf("Marshal rejected a page resource ID reserved before automatic allocation: %v", err)
	}
	if _, err := Marshal(Document{
		ID:          "invalid-attachment",
		Attachments: []Attachment{{ID: "", Name: "file", Data: []byte("data")}},
		Pages:       []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an attachment without ID")
	}
	if _, err := Marshal(Document{
		ID:          "invalid-attachment-id-whitespace",
		Attachments: []Attachment{{ID: " att", Name: "file", Data: []byte("data")}},
		Pages:       []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an attachment ID with surrounding whitespace")
	}
	if _, err := Marshal(Document{
		ID:          "invalid-attachment-path",
		Attachments: []Attachment{{ID: "att", Name: "file", FileName: "../file", Data: []byte("data")}},
		Pages:       []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an unsafe attachment filename")
	}
	if _, err := Marshal(Document{
		ID:         "invalid-custom-tag",
		CustomTags: []CustomTag{{NameSpace: "", Data: []byte("<Tag/>")}},
		Pages:      []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a custom tag without namespace")
	}
	if _, err := Marshal(Document{
		ID:         "invalid-custom-tag-schema-reference",
		CustomTags: []CustomTag{{NameSpace: "urn:test", SchemaName: "tag.xsd", Data: []byte("<Tag/>")}},
		Pages:      []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a custom tag SchemaName without Schema data")
	}
	if _, err := Marshal(Document{
		ID:         "invalid-custom-tag-path",
		CustomTags: []CustomTag{{NameSpace: "urn:test", DataName: "../tag.xml", Data: []byte("<Tag/>")}},
		Pages:      []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an unsafe custom tag filename")
	}
	if _, err := Marshal(Document{
		ID: "duplicate-custom-tag-file",
		CustomTags: []CustomTag{
			{NameSpace: "urn:one", DataName: "same.xml", Data: []byte("<One/>")},
			{NameSpace: "urn:two", DataName: "same.xml", Data: []byte("<Two/>")},
		},
		Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted duplicate custom tag files")
	}
	if _, err := Marshal(Document{
		ID:         "invalid-extension",
		Extensions: []Extension{{AppName: "", RefID: 1, Data: "<Data/>"}},
		Pages:      []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an extension without AppName")
	}
	if _, err := Marshal(Document{
		ID:         "invalid-extension-data",
		Extensions: []Extension{{AppName: "test", RefID: 1}},
		Pages:      []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an extension without data")
	}
	if _, err := Marshal(Document{
		ID:         "invalid-inline-extension-data-name",
		Extensions: []Extension{{AppName: "test", RefID: 1, Data: "<Data/>", DataName: "data.xml"}},
		Pages:      []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an inline extension with an external data name")
	}
	if _, err := Marshal(Document{
		ID:         "invalid-signature",
		Signatures: []Signature{{ID: "bad id", Type: "Sign", ProviderName: "test", References: []SignatureReference{{FileRef: "../Document.xml"}}, SignedValue: []byte("value")}},
		Pages:      []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid signature ID")
	}
	if _, err := Marshal(Document{
		ID:         "missing-signature-value",
		Signatures: []Signature{{ID: "sig-2", Type: "Sign", ProviderName: "test", References: []SignatureReference{{FileRef: "../Document.xml"}}}},
		Pages:      []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a signature without signed value")
	}
	if _, err := Marshal(Document{
		ID:         "missing-signature-reference-target",
		Signatures: []Signature{{ID: "sig-missing", Type: "Sign", ProviderName: "test", References: []SignatureReference{{FileRef: "missing.xml"}}, SignedValue: []byte("value")}},
		Pages:      []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a signature reference to a missing file")
	}
	if _, err := Marshal(Document{
		ID:       "invalid-version",
		Versions: []DocumentVersion{{ID: "bad/id", Index: -1, Files: []VersionFile{{ID: "file", Path: "../Document.xml"}}}},
		Pages:    []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid document version")
	}
	if _, err := Marshal(Document{
		ID:       "invalid-version-root-namespace",
		Versions: []DocumentVersion{{ID: "version-root", Index: 1, Files: []VersionFile{{ID: "file-root", Path: "Document.xml"}}, DocRoot: []byte(`<Document xmlns="http://www.ofdspec.org/2016"/>`), DocRootName: "version.xml"}},
		Pages:    []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a version root without the OFD namespace")
	}
	if _, err := Marshal(Document{
		ID:       "missing-version-file-target",
		Versions: []DocumentVersion{{ID: "version-missing", Index: 1, Files: []VersionFile{{ID: "file-missing", Path: "missing.xml"}}}},
		Pages:    []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a version file pointing to a missing file")
	}
	if _, err := Marshal(Document{
		ID:       "empty-version-file-list",
		Versions: []DocumentVersion{{ID: "version-empty", Index: 1}},
		Pages:    []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a document version without files")
	}
	if _, err := Marshal(Document{
		ID: "duplicate-version-index",
		Versions: []DocumentVersion{
			{ID: "version-one", Index: 1, Files: []VersionFile{{ID: "file-one", Path: "Document.xml"}}},
			{ID: "version-two", Index: 1, Files: []VersionFile{{ID: "file-two", Path: "Document.xml"}}},
		},
		Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted duplicate document version indexes")
	}
	if _, err := Marshal(Document{
		ID: "multiple-current-versions",
		Versions: []DocumentVersion{
			{ID: "version-a", Index: 1, Current: true, Files: []VersionFile{{ID: "file-a", Path: "Document.xml"}}},
			{ID: "version-b", Index: 2, Current: true, Files: []VersionFile{{ID: "file-b", Path: "Document.xml"}}},
		},
		Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted multiple current versions")
	}
	if _, err := Marshal(Document{
		ID:          "invalid-color-components",
		ColorSpaces: []ColorSpace{{ID: 30, Type: "CMYK"}},
		Pages:       []Page{{Items: []Item{Path{X: 0, Y: 0, Width: 10, Height: 10, Data: "M 0 0 L 10 10 C", FillColor: &Color{ColorSpace: 30, Components: []int{1, 2, 3}}}}}},
	}); err == nil {
		t.Fatal("Marshal accepted invalid color component count")
	}
	if _, err := Marshal(Document{
		ID:    "invalid-default-rgb-components",
		Pages: []Page{{Items: []Item{Path{X: 0, Y: 0, Width: 10, Height: 10, Data: "M 0 0 L 10 10 C", FillColor: &Color{Components: []int{1, 2}}}}}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid default RGB component count")
	}
	if _, err := Marshal(Document{
		ID:    "invalid-default-rgb-range",
		Pages: []Page{{Items: []Item{Path{X: 0, Y: 0, Width: 10, Height: 10, Data: "M 0 0 L 10 10 C", FillColor: &Color{Components: []int{0, 256, 0}}}}}},
	}); err == nil {
		t.Fatal("Marshal accepted an out-of-range default RGB component")
	}
	if _, err := Marshal(Document{
		ID:          "invalid-palette-range",
		ColorSpaces: []ColorSpace{{ID: 30, Type: "RGB", BitsPerComponent: 8, Palette: []string{"0 256 0"}}},
		Pages:       []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an out-of-range palette component")
	}
	if _, err := Marshal(Document{
		ID:          "invalid-profile-path",
		ColorSpaces: []ColorSpace{{ID: 30, Type: "RGB", Profile: "../profile.icc", ProfileData: []byte("profile")}},
		Pages:       []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an unsafe color profile path")
	}
	if _, err := Marshal(Document{
		ID: "invalid-cover", Cover: "Cover/cover.png", Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a cover without data")
	}
	if _, err := Marshal(Document{
		ID:          "duplicate-custom-data",
		CustomDatas: []CustomData{{Name: "A", Value: "1"}, {Name: "A", Value: "2"}}, Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted duplicate custom metadata")
	}
	if _, err := Marshal(Document{
		ID:    "invalid-image-mask",
		Media: []Media{{ID: 80, Type: "Audio", Data: []byte("audio")}},
		Pages: []Page{{Items: []Item{Image{X: 1, Y: 1, Width: 2, Height: 2, Data: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, ImageMask: 80}}}},
	}); err == nil {
		t.Fatal("Marshal accepted a non-image ImageMask resource")
	}
	if _, err := Marshal(Document{
		ID:         "invalid-composite-thumbnail",
		Media:      []Media{{ID: 81, Type: "Audio", Data: []byte("audio")}},
		Composites: []CompositeGraphicUnit{{ID: 82, Width: 10, Height: 10, Thumbnail: 81, Items: []Item{Path{X: 0, Y: 0, Width: 1, Height: 1, Data: "M 0 0 L 1 1 C"}}}},
		Pages:      []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a non-image composite thumbnail resource")
	}
	if _, err := Marshal(Document{
		ID: "invalid-default-color-space", DefaultCS: 30, Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an undefined default color space")
	}
	if _, err := Marshal(Document{
		ID: "invalid-nested-color-space",
		Pages: []Page{{Items: []Item{PageBlock{Items: []Item{Path{
			X: 0, Y: 0, Width: 1, Height: 1, Data: "M 0 0 L 1 1 C",
			FillColor: &Color{ColorSpace: 99, Components: []int{1, 2, 3}},
		}}}}}},
	}); err == nil {
		t.Fatal("Marshal accepted an undefined nested color space")
	}
	if _, err := Marshal(Document{
		ID: "invalid-gouraud-back-color-space",
		Pages: []Page{{Items: []Item{Path{
			X: 0, Y: 0, Width: 1, Height: 1, Data: "M 0 0 L 1 1 C",
			FillColor: &Color{Gouraud: &GouraudShading{
				Points:    []GouraudPoint{{Color: Color{}}, {Color: Color{}}, {Color: Color{}}},
				BackColor: &Color{ColorSpace: 99, Components: []int{1, 2, 3}},
			}},
		}}}},
	}); err == nil {
		t.Fatal("Marshal accepted an undefined Gouraud back color space")
	}
	if _, err := Marshal(Document{
		ID:        "invalid-public-resource-path",
		PublicRes: []PublicResource{{Name: "../SharedRes.xml", Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."/>`)}}, Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an unsafe public resource path")
	}
	if _, err := Marshal(Document{
		ID:        "invalid-public-resource-root",
		PublicRes: []PublicResource{{Name: "SharedRes.xml", Data: []byte(`<NotRes xmlns="http://www.ofdspec.org/2016"/>`)}}, Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a public resource with an invalid root")
	}
	if _, err := Marshal(Document{
		ID:        "invalid-public-resource-schema",
		PublicRes: []PublicResource{{Name: "SharedRes.xml", Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016"/>`)}}, Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a public resource that failed Res.xsd")
	}
	if _, err := Marshal(Document{
		ID:        "invalid-public-resource-namespace",
		PublicRes: []PublicResource{{Name: "SharedRes.xml", Data: []byte(`<Res BaseLoc="."/>`)}}, Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a public resource without the OFD namespace")
	}
	if _, err := Marshal(Document{
		ID:        "public-resource-entry-collision",
		PublicRes: []PublicResource{{Name: "Document.xml", Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."/>`)}},
		Pages:     []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a public resource colliding with Document.xml")
	}
	if _, err := Marshal(Document{
		ID:        "invalid-public-resource-absolute-path",
		PublicRes: []PublicResource{{Name: "/SharedRes.xml", Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."/>`)}}, Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an absolute public resource path")
	}
	if _, err := Marshal(Document{
		ID: "invalid-signature-noncanonical-path",
		Signatures: []Signature{{
			ID: "sig-noncanonical", Type: "Sign", ProviderName: "test",
			References: []SignatureReference{{FileRef: "./../Document.xml"}}, SignedValue: []byte("value"),
		}},
		Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a non-canonical signature reference path")
	}
	if _, err := Marshal(Document{
		ID:       "invalid-version-noncanonical-path",
		Versions: []DocumentVersion{{ID: "version-noncanonical", Index: 1, Files: []VersionFile{{ID: "file", Path: "./Document.xml"}}}},
		Pages:    []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a non-canonical version file path")
	}
	if _, err := Marshal(Document{
		ID:   "invalid-document-area",
		Area: &PageArea{ContentBox: &Box{Width: 0, Height: 10}}, Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid document page area")
	}
	if _, err := Marshal(Document{
		ID:      "missing-attachment-action",
		Actions: []Action{{GotoA: &GotoAAction{AttachID: "missing"}}}, Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an action to a missing attachment")
	}
	if _, err := Marshal(Document{
		ID: "invalid-action-region",
		Actions: []Action{{
			Region: &ActionRegion{Areas: []ActionArea{{Start: Point{X: math.Inf(1), Y: 0}, Commands: []RegionCommand{RegionClose{}}}}},
			URI:    &URIAction{URI: "https://example.com"},
		}},
		Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid action region")
	}
	if _, err := Marshal(Document{
		ID: "duplicate-attachment-file-name",
		Attachments: []Attachment{
			{ID: "a", Name: "A", FileName: "same.bin", Data: []byte("a")},
			{ID: "b", Name: "B", FileName: "same.bin", Data: []byte("b")},
		},
		Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted duplicate attachment file names")
	}
	if _, err := Marshal(Document{
		ID: "duplicate-media-file-name",
		Media: []Media{
			{ID: 90, Type: "Audio", Name: "same.bin", Data: []byte("a")},
			{ID: 91, Type: "Video", Name: "same.bin", Data: []byte("b")},
		},
		Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted duplicate media file names")
	}
	if _, err := Marshal(Document{
		ID: "duplicate-page-image-file-name",
		Pages: []Page{{Resources: []PageResource{{Images: []PageImage{
			{ID: 90, Format: "PNG", Name: "same.png", Data: []byte{0x89, 'P', 'N', 'G'}},
			{ID: 91, Format: "PNG", Name: "same.png", Data: []byte{0x89, 'P', 'N', 'G'}},
		}}}}},
	}); err == nil {
		t.Fatal("Marshal accepted duplicate page image file names")
	}
	if _, err := Marshal(Document{
		ID:    "invalid-media-file-name",
		Media: []Media{{ID: 92, Type: "Audio", Name: "bad\x00.bin", Data: []byte("audio")}},
		Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a media file name containing NUL")
	}
	if _, err := Marshal(Document{
		ID: "invalid-page-image-file-name",
		Pages: []Page{{
			Resources: []PageResource{{
				Images: []PageImage{{ID: 93, Format: "PNG", Name: "bad\x00.png", Data: []byte{0x89, 'P', 'N', 'G'}}},
			}},
		}},
	}); err == nil {
		t.Fatal("Marshal accepted a page image file name containing NUL")
	}
	if _, err := Marshal(Document{
		ID:         "invalid-extension-file-name",
		Extensions: []Extension{{AppName: "test", RefID: 1, DataFile: []byte("data"), DataName: "bad\x00.bin"}},
		Pages:      []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an extension file name containing NUL")
	}
	if _, err := Marshal(Document{
		ID:         "invalid-signature-file-name",
		Signatures: []Signature{{ID: "sig", Type: "Sign", ProviderName: "test", References: []SignatureReference{{FileRef: "../Document.xml"}}, SignedValue: []byte("value"), SignedValueName: "bad\x00.bin"}},
		Pages:      []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a signature file name containing NUL")
	}
	if _, err := Marshal(Document{
		ID:         "invalid-signature-reference-path",
		Signatures: []Signature{{ID: "sig-path", Type: "Sign", ProviderName: "test", References: []SignatureReference{{FileRef: "/Document.xml"}}, SignedValue: []byte("value")}},
		Pages:      []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an absolute signature reference path")
	}
	if _, err := Marshal(Document{
		ID:         "invalid-signature-reference-whitespace",
		Signatures: []Signature{{ID: "sig-space", Type: "Sign", ProviderName: "test", References: []SignatureReference{{FileRef: " ../Document.xml"}}, SignedValue: []byte("value")}},
		Pages:      []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a signature reference with surrounding whitespace")
	}
	if _, err := Marshal(Document{
		ID: "duplicate-signature-data-name",
		Signatures: []Signature{
			{ID: "sig-a", Type: "Sign", ProviderName: "test", References: []SignatureReference{{FileRef: "../Document.xml"}}, SignedValue: []byte("a"), SignedValueName: "same.bin"},
			{ID: "sig-b", Type: "Sign", ProviderName: "test", References: []SignatureReference{{FileRef: "../Document.xml"}}, SignedValue: []byte("b"), SignedValueName: "same.bin"},
		},
		Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted duplicate signature data file names")
	}
	if _, err := Marshal(Document{
		ID:       "invalid-version-file-path",
		Versions: []DocumentVersion{{ID: "version-path", Index: 1, Files: []VersionFile{{ID: "file-path", Path: "\\Document.xml"}}}},
		Pages:    []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an absolute version file path")
	}
	if _, err := Marshal(Document{
		ID:       "invalid-version-file-whitespace",
		Versions: []DocumentVersion{{ID: "version-space", Index: 1, Files: []VersionFile{{ID: "file-space", Path: " Document.xml"}}}},
		Pages:    []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a version file path with surrounding whitespace")
	}
	if _, err := Marshal(Document{
		ID: "duplicate-version-root-name",
		Versions: []DocumentVersion{
			{ID: "version-a", Index: 1, DocRoot: []byte(`<Document xmlns="http://www.ofdspec.org/2016"/>`), DocRootName: "same.xml", Files: []VersionFile{{ID: "file-a", Path: "Document.xml"}}},
			{ID: "version-b", Index: 2, DocRoot: []byte(`<Document xmlns="http://www.ofdspec.org/2016"/>`), DocRootName: "same.xml", Files: []VersionFile{{ID: "file-b", Path: "Document.xml"}}},
		},
		Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted duplicate version root file names")
	}
	if _, err := Marshal(Document{
		ID:      "missing-bookmark-action",
		Actions: []Action{{Goto: &GotoAction{Bookmark: "missing"}}}, Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an action to a missing bookmark")
	}
	if _, err := Marshal(Document{
		ID:        "invalid-template-reference-order",
		Templates: []TemplatePage{{ID: 10}},
		Pages:     []Page{{Templates: []TemplateRef{{ID: 10, ZOrder: "Middle"}}}},
	}); err == nil {
		t.Fatal("Marshal accepted an invalid template reference z-order")
	}
	if _, err := Marshal(Document{
		ID:        "invalid-bookmark-target",
		Bookmarks: []Bookmark{{Name: "self", Goto: GotoAction{Bookmark: "self"}}},
		Pages:     []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a bookmark with a Bookmark target")
	}
	top := 10.0
	if _, err := Marshal(Document{
		ID:      "invalid-fit-target-parameter",
		Actions: []Action{{Goto: &GotoAction{Page: 0, Type: "Fit", Top: &top}}}, Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a Fit target with a position parameter")
	}
	if _, err := Marshal(Document{
		ID:      "invalid-fit-rectangle-target",
		Actions: []Action{{Goto: &GotoAction{Page: 0, Type: "FitR", Left: &top, Top: &top, Right: &top}}}, Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted an incomplete FitR target")
	}
	left, right := 20.0, 10.0
	if _, err := Marshal(Document{
		ID:      "invalid-fit-rectangle-order",
		Actions: []Action{{Goto: &GotoAction{Page: 0, Type: "FitR", Left: &left, Top: &top, Right: &right, Bottom: &top}}}, Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted a reversed FitR rectangle")
	}
	if _, err := Marshal(Document{
		ID: "duplicate-annotation-page",
		Annotations: []AnnotationPage{
			{Page: 0, Items: []Annotation{{ID: 1, Type: "Link", Creator: "test", Items: []Item{Path{X: 0, Y: 0, Width: 1, Height: 1, Data: "M 0 0 L 1 1 C"}}}}},
			{Page: 0, Items: []Annotation{{ID: 2, Type: "Path", Creator: "test", Items: []Item{Path{X: 0, Y: 0, Width: 1, Height: 1, Data: "M 0 0 L 1 1 C"}}}}},
		},
		Pages: []Page{{}},
	}); err == nil {
		t.Fatal("Marshal accepted duplicate annotation pages")
	}
}

func TestCreateCompressionLevelRoundTrip(t *testing.T) {
	document := Document{
		ID:       "compression-level",
		Title:    "压缩级别测试",
		PageSize: A4,
		Pages: []Page{{Items: []Item{
			Text{X: 20, Y: 30, Width: 100, Height: 10, Value: "压缩级别测试文字", Font: "SimSun"},
		}}},
	}
	for _, level := range []int{1, 5, 9} {
		data, err := MarshalWithOptions(document, CreateOptions{Compression: CompressionDeflate, CompressionLevel: level})
		if err != nil {
			t.Fatalf("level %d: %v", level, err)
		}
		newTestOFD(t, data)
	}
}

func TestCreateCompressionLevelDefaultInvariant(t *testing.T) {
	document := Document{
		ID:       "compression-level-default",
		Title:    "默认级别不变",
		PageSize: A4,
		Pages: []Page{{Items: []Item{
			Text{X: 20, Y: 30, Width: 100, Height: 10, Value: "默认压缩级别输出不变", Font: "SimSun"},
		}}},
	}
	zero, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	five, err := MarshalWithOptions(document, CreateOptions{CompressionLevel: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(zero, five) {
		t.Fatal("显式默认级别 5 应保持与不设置级别时的逐字节输出一致")
	}
	stored, err := MarshalWithOptions(document, CreateOptions{Compression: CompressionStore, CompressionLevel: 9})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(stored), int64(len(stored)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range archive.File {
		if file.Method != zip.Store {
			t.Fatalf("store 模式应忽略压缩级别，条目 %s 使用 %d", file.Name, file.Method)
		}
	}
}
