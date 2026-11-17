package spec_test

import (
	"encoding/xml"
	"fmt"

	"github.com/zc310/ofd/pkg/spec"
)

func ExampleOFD() {
	var root spec.OFD
	data := []byte(`<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><Title>示例文档</Title></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`)
	if err := xml.Unmarshal(data, &root); err != nil {
		panic(err)
	}
	body := root.DocBodies[0]
	fmt.Println(*body.DocInfo.Title)
	fmt.Println(body.DocRoot.Resolve("/"))
	// Output:
	// 示例文档
	// /Doc_0/Document.xml
}

func ExampleBool() {
	var value spec.Bool
	fmt.Println(value.IsSet(), value.Value(true))
	value = spec.NewBool(false)
	fmt.Println(value.IsSet(), value.Value(true))
	value.Set(true)
	fmt.Println(value.IsSet(), value.Value(false))
	value.Reset()
	fmt.Println(value.IsSet(), value.Value(false))
	// Output:
	// false true
	// true false
	// true true
	// false false
}

func ExampleAttachment() {
	var attachment spec.Attachment
	data := `<Attachment ID="attachment_1" Name="说明.txt" Visible="false"><FileLoc>Files/readme.txt</FileLoc></Attachment>`
	if err := xml.Unmarshal([]byte(data), &attachment); err != nil {
		panic(err)
	}
	fmt.Println(attachment.ID, attachment.Name, attachment.FileLoc)
	fmt.Println(attachment.Visible.IsSet(), attachment.Visible.Value(true))
	// Output:
	// attachment_1 说明.txt Files/readme.txt
	// true false
}

func ExampleDocument() {
	var document spec.Document
	data := `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></Pages></Document>`
	if err := xml.Unmarshal([]byte(data), &document); err != nil {
		panic(err)
	}
	fmt.Println(document.XMLName.Space)
	fmt.Println(document.Pages.Pages[0].ID, document.Pages.Pages[0].BaseLoc)
	// Output:
	// http://www.ofdspec.org/2016
	// 1 Pages/Page_0/Content.xml
}

func ExampleVPreferences() {
	var preferences spec.VPreferences
	data := `<VPreferences><ZoomMode>FitWidth</ZoomMode><Zoom>1.25</Zoom></VPreferences>`
	if err := xml.Unmarshal([]byte(data), &preferences); err != nil {
		panic(err)
	}
	fmt.Println(*preferences.Zoom.Mode, *preferences.Zoom.Value)
	// Output:
	// FitWidth 1.25
}

func ExamplePalette() {
	var palette spec.Palette
	data := `<Palette><CV>255 0 0</CV><CV>0 255 0</CV></Palette>`
	if err := xml.Unmarshal([]byte(data), &palette); err != nil {
		panic(err)
	}
	fmt.Println(palette.CV[0], palette.CV[1])
	// Output:
	// [255 0 0] [0 255 0]
}

func ExampleFont() {
	var font spec.Font
	if err := xml.Unmarshal([]byte(`<Font ID="1" FontName="Example" FixedWidth="true"><FontFile>Fonts/Subset.otf</FontFile></Font>`), &font); err != nil {
		panic(err)
	}
	fmt.Println(font.ID, font.FontName, font.FixedWidth, font.FontFile)
	// Output:
	// 1 Example true Fonts/Subset.otf
}

func ExampleMultiMedia() {
	var media spec.MultiMedia
	data := `<MultiMedia ID="2" Type="Image" Format="PNG"><MediaFile>Images/logo.png</MediaFile></MultiMedia>`
	if err := xml.Unmarshal([]byte(data), &media); err != nil {
		panic(err)
	}
	fmt.Println(media.ID, media.Type, media.Format, media.MediaFile)
	// Output:
	// 2 Image PNG Images/logo.png
}

func ExamplePathData() {
	path, err := spec.ParsePathData("M 1.23456789 2 B 3 4 5 6 7 8 C")
	if err != nil {
		panic(err)
	}
	data, err := xml.Marshal(path)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(data))
	// Output:
	// <PathData>M 1.23456789 2 B 3 4 5 6 7 8 C</PathData>
}

func ExampleColorValue() {
	var value spec.ColorValue
	if err := xml.Unmarshal([]byte(`<Value>#FFFF 0 0</Value>`), &value); err != nil {
		panic(err)
	}
	components, count, _ := value.Components()
	fmt.Println(components[0], count)
	attribute, err := value.MarshalXMLAttr(xml.Name{Local: "Value"})
	if err != nil {
		panic(err)
	}
	fmt.Println(attribute.Name.Local, attribute.Value)
	// Output:
	// 65535 3
	// Value #FFFF 0 0
}

func ExamplePageContent() {
	var page spec.PageContent
	data := `<Page><Content><Layer ID="1"><TextObject ID="2" Boundary="0 0 10 10" Font="3" Size="3"><TextCode>甲\0020乙</TextCode></TextObject><PathObject ID="4" Boundary="0 0 10 10"><AbbreviatedData>M 0 0 L 1 1</AbbreviatedData></PathObject></Layer></Content></Page>`
	if err := xml.Unmarshal([]byte(data), &page); err != nil {
		panic(err)
	}
	items := page.Content.Layer[0].Items
	fmt.Println(items[0].Text.TextCode[0].Value)
	fmt.Println(items[1].Path.ID)
	// Output:
	// 甲 乙
	// 4
}

func ExamplePageAnnot() {
	var page spec.PageAnnot
	data := `<PageAnnot><Annot ID="1" Type="Link" Creator="示例" LastModDate="2026-01-02" Print="false"><Remark>说明</Remark><Appearance Boundary="1 2 30 40"/></Annot></PageAnnot>`
	if err := xml.Unmarshal([]byte(data), &page); err != nil {
		panic(err)
	}
	annot := page.Annots[0]
	fmt.Println(annot.Visible.IsSet(), annot.Visible.Value(true))
	fmt.Println(annot.Print.IsSet(), annot.Print.Value(true))
	fmt.Println(*annot.Remark)
	// Output:
	// false true
	// true false
	// 说明
}

func ExampleSignatures() {
	var index spec.Signatures
	data := `<Signatures><Signature ID="s1" BaseLoc="Sign_0/Signature.xml" Type="Sign"/></Signatures>`
	if err := xml.Unmarshal([]byte(data), &index); err != nil {
		panic(err)
	}
	ref := index.Signatures[0]
	fmt.Println(ref.ID, ref.Type)
	fmt.Println(ref.BaseLoc.Resolve("/Doc_0/Signs"))
	// Output:
	// s1 Sign
	// /Doc_0/Signs/Sign_0/Signature.xml
}

func ExampleExtension() {
	var extension spec.Extension
	data := `<Extension AppName="示例" RefId="1"><Data>前<Config xmlns="urn:example">子内容</Config>后</Data></Extension>`
	if err := xml.Unmarshal([]byte(data), &extension); err != nil {
		panic(err)
	}
	fmt.Println(extension.AppName, extension.RefID)
	fmt.Println(extension.Data.Text)
	fmt.Println(extension.Data.InnerXML)
	// Output:
	// 示例 1
	// 前后
	// 前<Config xmlns="urn:example">子内容</Config>后
}

func ExampleDecryptSeed() {
	var seed spec.DecryptSeed
	data := `<DecyptSeed ID="e1" EncryptCaseId="1.1.1"><UserInfo UserName="示例"><EncryptedWK>YWJj</EncryptedWK></UserInfo><ExtendParams/></DecyptSeed>`
	if err := xml.Unmarshal([]byte(data), &seed); err != nil {
		panic(err)
	}
	fmt.Println(seed.ID, seed.Scheme)
	fmt.Println(seed.Users[0].Name, seed.Users[0].WrappedKey)
	fmt.Println(seed.Users[0].IV == nil, seed.ExtendParams != nil)
	// Output:
	// e1 1.1.1
	// 示例 YWJj
	// true true
}

func ExampleIntegrityManifest() {
	var manifest spec.IntegrityManifest
	data := `<OFDEntries ID="m1"><FileList><File ID="f1" FileLoc="/OFD.xml"/></FileList></OFDEntries>`
	if err := xml.Unmarshal([]byte(data), &manifest); err != nil {
		panic(err)
	}
	fmt.Println(manifest.ID, manifest.Files[0].ID)
	fmt.Println(manifest.Files[0].Location)
	// Output:
	// m1 f1
	// /OFD.xml
}

func ExampleDrawParam() {
	parent := spec.DrawParam{LineWidth: 2}
	var override spec.DrawParam
	override.SetLineWidth(0)
	parent.Override(&override)
	fmt.Println(parent.LineWidth, parent.HasLineWidth())
	// Output:
	// 0 true
}
