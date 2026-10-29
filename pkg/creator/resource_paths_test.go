package creator

import (
	"bytes"
	"image"
	"image/png"
	"path"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

func resourcePathTestPNG(t *testing.T) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

// GB/T 33190—2016 §6.2、§7.3：资源通过包内路径引用，文件名与显示名称分别保存。
func TestCreateUsesShortResourcePathsAndReusesContent(t *testing.T) {
	pngData := resourcePathTestPNG(t)
	longName := strings.Repeat("附件说明", 80)
	schema := []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`)
	document := Document{
		ID: "short-resource-paths", CoverData: pngData,
		Fonts: []Font{
			{Name: "Body", FontName: "Go Regular", Data: goregular.TTF},
			{Name: "Other", FontName: "Go Regular", Source: &memorySource{data: goregular.TTF}},
		},
		ColorSpaces: []ColorSpace{
			{ID: 90, Type: "RGB", ProfileData: []byte("shared-profile")},
			{ID: 91, Type: "RGB", ProfileData: []byte("shared-profile")},
		},
		Media: []Media{
			{ID: 80, Type: "Audio", Format: "wav", Data: []byte("sound")},
			{ID: 81, Type: "Audio", Format: "wav", Source: &memorySource{data: []byte("sound")}},
		},
		Attachments: []Attachment{
			{ID: "first", Name: longName, Format: "txt", Data: []byte("attachment")},
			{ID: "second", Name: longName, Format: "txt", Source: &memorySource{data: []byte("attachment")}},
		},
		CustomTags: []CustomTag{
			{NameSpace: "urn:one", Data: []byte("<Tags/>"), Schema: schema},
			{NameSpace: "urn:two", Data: []byte("<Tags/>"), Schema: schema},
		},
		Extensions: []Extension{
			{AppName: "one", RefID: 70, DataFile: []byte("extension")},
			{AppName: "two", RefID: 70, DataFileSource: &memorySource{data: []byte("extension")}},
		},
		Pages: []Page{{
			Resources: []PageResource{{Images: []PageImage{{ID: 70, Format: "PNG", Data: pngData}}}},
			Items: []Item{
				Text{Width: 30, Height: 10, Value: "AB", Font: "Body"},
				Text{Y: 10, Width: 30, Height: 10, Value: "AB", Font: "Other"},
				Image{Width: 10, Height: 10, Data: pngData},
				Image{X: 10, Width: 10, Height: 10, Source: &memorySource{data: pngData}},
				Image{X: 20, Width: 10, Height: 10, ResourceID: 70},
			},
		}},
	}
	for _, preserve := range []bool{false, true} {
		name := "子集字体"
		if preserve {
			name = "完整字体"
		}
		t.Run(name, func(t *testing.T) {
			options := CreateOptions{Deterministic: true, PreserveEmbeddedFonts: preserve}
			data, err := MarshalWithOptions(document, options)
			if err != nil {
				t.Fatal(err)
			}
			second, err := MarshalWithOptions(document, options)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, second) {
				t.Fatal("短文件名导致重复生成结果不同")
			}
			entries := openZipEntries(t, data)
			shortName := regexp.MustCompile(`^[0-9]+\.[a-z0-9]+$`)
			for prefix, want := range map[string]int{
				"Doc_0/Res/Fonts/": 1, "Doc_0/Res/Images/": 1, "Doc_0/Res/Media/": 2,
				"Doc_0/Res/Profiles/": 1, "Doc_0/Cover/": 1, "Doc_0/Pages/Page_0/Images/": 1,
				"Doc_0/Attachments/Files/": 2, "Doc_0/CustomTags/Data/": 2,
				"Doc_0/CustomTags/Schemas/": 2, "Doc_0/Extensions/Data/": 2,
			} {
				count := 0
				for entry := range entries {
					if strings.HasPrefix(entry, prefix) {
						count++
						if !shortName.MatchString(path.Base(entry)) {
							t.Errorf("资源文件名未缩短: %s", entry)
						}
					}
				}
				if count != want {
					t.Errorf("%s 的文件数 = %d，期望 %d", prefix, count, want)
				}
			}
			for entry := range entries {
				if len(entry) > 64 {
					t.Errorf("自动生成的路径过长: %s", entry)
				}
			}
			for _, ref := range []struct {
				file, selector, base string
				count                int
			}{
				{"OFD.xml", "DocBody/DocInfo/Cover", "", 1},
				{"Doc_0/DocumentRes.xml", "Fonts/Font/FontFile", "Doc_0/Res", 2},
				{"Doc_0/DocumentRes.xml", "MultiMedias/MultiMedia/MediaFile", "Doc_0/Res", 3},
				{"Doc_0/Pages/Page_0/PageRes_0_0.xml", "MultiMedias/MultiMedia/MediaFile", "Doc_0/Pages/Page_0", 1},
				{"Doc_0/Attachments/Attachments.xml", "Attachment/FileLoc", "Doc_0/Attachments", 2},
				{"Doc_0/CustomTags/CustomTags.xml", "CustomTag/FileLoc", "Doc_0/CustomTags", 2},
				{"Doc_0/CustomTags/CustomTags.xml", "CustomTag/SchemaLoc", "Doc_0/CustomTags", 2},
				{"Doc_0/Extensions/Extensions.xml", "Extension/ExtendData", "Doc_0/Extensions", 2},
			} {
				root, err := parseXMLRoot(entries[ref.file])
				if err != nil {
					t.Fatal(err)
				}
				refs := xmlElements(root, ref.selector)
				if len(refs) != ref.count {
					t.Fatalf("%s 的 %s 数量 = %d，期望 %d", ref.file, ref.selector, len(refs), ref.count)
				}
				for _, element := range refs {
					target := path.Join(ref.base, element.StringValue())
					if _, ok := entries[target]; !ok {
						t.Errorf("%s 引用了不存在的文件 %s", ref.file, target)
					}
				}
			}
			root, err := parseXMLRoot(entries["Doc_0/Attachments/Attachments.xml"])
			if err != nil {
				t.Fatal(err)
			}
			for _, attachment := range xmlElements(root, "Attachment") {
				if attachment.AttrValue("Name") != longName {
					t.Error("附件显示名称未保留")
				}
			}
			checkGeneratedPackage(t, data)
		})
	}
}

func TestCreateShortResourcePathsAvoidExplicitNames(t *testing.T) {
	pngData := resourcePathTestPNG(t)
	document := Document{
		ID: "reserved-resource-names", CoverData: pngData, CoverName: "cover.png",
		Media: []Media{
			{ID: 80, Type: "Audio", Format: "wav", Data: []byte("auto")},
			{ID: 81, Type: "Audio", Format: "wav", Name: "80.wav", Data: []byte("explicit")},
		},
		ColorSpaces: []ColorSpace{
			{ID: 90, Type: "RGB", ProfileData: []byte("auto")},
			{ID: 91, Type: "RGB", Profile: "Profiles/90.icc", ProfileData: []byte("explicit")},
		},
		Attachments: []Attachment{
			{ID: "one", Name: "自动", Format: "txt", Data: []byte("auto")},
			{ID: "two", Name: "显式", FileName: "1.txt", Data: []byte("explicit")},
		},
		CustomTags: []CustomTag{
			{NameSpace: "urn:auto", Data: []byte("<Auto/>"), Schema: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`)},
			{NameSpace: "urn:explicit", DataName: "1.xml", Data: []byte("<Explicit/>"), SchemaName: "1.xsd", Schema: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"/>`)},
		},
		Extensions: []Extension{
			{AppName: "auto", RefID: 80, DataFile: []byte("auto")},
			{AppName: "explicit", RefID: 80, DataName: "1.bin", DataFile: []byte("explicit")},
		},
		Pages: []Page{{
			Resources: []PageResource{{Images: []PageImage{{ID: 70, Name: "page.png", Data: pngData}}}},
		}},
	}
	data, err := Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	entries := openZipEntries(t, data)
	for entry, want := range map[string][]byte{
		"Doc_0/Cover/cover.png":                 pngData,
		"Doc_0/Res/Media/80.wav":                []byte("explicit"),
		"Doc_0/Res/Media/81.wav":                []byte("auto"),
		"Doc_0/Res/Profiles/90.icc":             []byte("explicit"),
		"Doc_0/Res/Profiles/91.icc":             []byte("auto"),
		"Doc_0/Attachments/Files/1.txt":         []byte("explicit"),
		"Doc_0/Attachments/Files/2.txt":         []byte("auto"),
		"Doc_0/CustomTags/Data/1.xml":           []byte("<Explicit/>"),
		"Doc_0/CustomTags/Data/2.xml":           []byte("<Auto/>"),
		"Doc_0/Extensions/Data/1.bin":           []byte("explicit"),
		"Doc_0/Extensions/Data/2.bin":           []byte("auto"),
		"Doc_0/Pages/Page_0/Images/70-page.png": pngData,
	} {
		if !bytes.Equal(entries[entry], want) {
			t.Errorf("条目 %s 内容错误: %q", entry, entries[entry])
		}
	}
	if _, ok := entries["Doc_0/CustomTags/Schemas/2.xsd"]; !ok {
		t.Error("自动 Schema 名称未避开显式 1.xsd")
	}
	if document.ColorSpaces[0].Profile != "" || document.Attachments[0].FileName != "" || document.CustomTags[0].DataName != "" {
		t.Error("自动命名修改了调用方模型")
	}
	checkGeneratedPackage(t, data)
}

func TestCreateShortResourcePathsPreserveRawReferencesAndStreaming(t *testing.T) {
	pngData := resourcePathTestPNG(t)
	publicXML := []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><Fonts><Font ID="70" FontName="Raw"><FontFile>Fonts/71.ttf</FontFile></Font></Fonts></Res>`)
	pageXML := []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><MultiMedias><MultiMedia ID="72" Type="Image" Format="PNG"><MediaFile>Images/73.png</MediaFile></MultiMedia></MultiMedias></Res>`)
	document := Document{
		ID:    "raw-short-paths",
		Fonts: []Font{{Name: "Body", Data: goregular.TTF}},
		PublicRes: []PublicResource{{
			Name: " Res/PublicRes.xml ", Data: publicXML,
			Files: []PublicResourceFile{{Path: "Fonts/71.ttf", Data: goregular.TTF}},
		}},
		Pages: []Page{{
			Resources: []PageResource{
				{Data: pageXML, Files: []PageResourceFile{{Path: "Images/73.png", Data: pngData}}},
				{Images: []PageImage{{ID: 73, Data: pngData}}},
			},
			Items: []Item{Text{Width: 20, Height: 10, Font: "Body", Value: "AB"}},
		}},
	}
	options := CreateOptions{Deterministic: true, PreserveEmbeddedFonts: true}
	data, err := MarshalWithOptions(document, options)
	if err != nil {
		t.Fatal(err)
	}
	meta := document
	meta.Pages = nil
	streamed, err := MarshalWithPages(meta, &testPageProvider{pages: document.Pages}, options)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, streamed) {
		t.Fatal("资源短名在流式页面和内存页面之间不一致")
	}
	entries := openZipEntries(t, data)
	for entry, want := range map[string][]byte{
		"Doc_0/Res/PublicRes.xml":            publicXML,
		"Doc_0/Res/Fonts/71.ttf":             goregular.TTF,
		"Doc_0/Res/Fonts/72.ttf":             goregular.TTF,
		"Doc_0/Pages/Page_0/PageRes_0_0.xml": pageXML,
		"Doc_0/Pages/Page_0/Images/73.png":   pngData,
		"Doc_0/Pages/Page_0/Images/74.png":   pngData,
	} {
		if !bytes.Equal(entries[entry], want) {
			t.Errorf("条目 %s 内容未保留", entry)
		}
	}
	checkGeneratedPackage(t, data)
}
