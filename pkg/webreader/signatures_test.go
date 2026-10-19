package webreader

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestSignatureStampsExposeClippedVisibleBoundary(t *testing.T) {
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	files := map[string]string{
		"OFD.xml":            `<OFD Version="1.0"><DocBody><DocRoot>Doc/Document.xml</DocRoot><Signatures>Doc/Signatures.xml</Signatures></DocBody></OFD>`,
		"Doc/Document.xml":   `<Document><CommonData><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`,
		"Doc/Page.xml":       `<Page><Content><Layer ID="2"/></Content></Page>`,
		"Doc/Signatures.xml": `<Signatures><Signature ID="s1" BaseLoc="Signature.xml"/></Signatures>`,
		"Doc/Signature.xml": `<Signature><SignedInfo><Provider ProviderName="test"/>
<StampAnnot ID="whole" PageRef="1" Boundary="170 128.5 40 40"/>
<StampAnnot ID="left" PageRef="1" Boundary="202 128.5 40 40" Clip="0 0 8 40"/>
<StampAnnot ID="middle" PageRef="1" Boundary="186 128.5 40 40" Clip="16 0 8 40"/>
<StampAnnot ID="bottom" PageRef="1" Boundary="85 277 40 40" Clip="0 12 40 8"/>
<StampAnnot ID="overflow" PageRef="1" Boundary="10 20 40 40" Clip="-5 30 20 20"/>
<StampAnnot ID="outside" PageRef="1" Boundary="10 20 40 40" Clip="50 0 8 40"/>
</SignedInfo></Signature>`,
	}
	for name, content := range files {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	infos, err := reader.Signatures()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || len(infos[0].Stamps) != 6 {
		t.Fatalf("签章位置缺失: %+v", infos)
	}
	want := map[string]AnnotationBoundary{
		"whole":    {X: 170, Y: 128.5, Width: 40, Height: 40},
		"left":     {X: 202, Y: 128.5, Width: 8, Height: 40},
		"middle":   {X: 202, Y: 128.5, Width: 8, Height: 40},
		"bottom":   {X: 85, Y: 289, Width: 40, Height: 8},
		"overflow": {X: 10, Y: 50, Width: 15, Height: 10},
		"outside":  {X: 10, Y: 20, Width: 40, Height: 40},
	}
	for _, stamp := range infos[0].Stamps {
		if stamp.Page != 0 || stamp.VisibleBoundary == nil || *stamp.VisibleBoundary != want[stamp.ID] {
			t.Errorf("签章 %s 可见区域 = %+v，期望 %+v", stamp.ID, stamp.VisibleBoundary, want[stamp.ID])
		}
		if stamp.Boundary == nil || stamp.Boundary.Width != 40 || stamp.Boundary.Height != 40 {
			t.Errorf("原始 Boundary 应保留完整印章尺寸: %+v", stamp)
		}
	}
}
