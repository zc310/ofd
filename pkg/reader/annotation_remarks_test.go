package reader

import (
	"bytes"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
)

// 表61的Print/NoZoom/NoRotate只影响Remark，不影响Appearance。
func remarkFixture(t *testing.T, brokenSecond bool) []byte {
	t.Helper()
	files := map[string]string{
		"OFD.xml":      `<OFD><DocBody><DocRoot>Document.xml</DocRoot></DocBody></OFD>`,
		"Document.xml": `<Document><CommonData><PageArea><PhysicalBox>0 0 20 20</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="P1.xml"/><Page ID="2" BaseLoc="P2.xml"/></Pages><Annotations>Annotations.xml</Annotations></Document>`,
		"P1.xml":       `<Page><Content/></Page>`, "P2.xml": `<Page><Content/></Page>`,
		"Annotations.xml": `<Annotations><Page PageID="1"><FileLoc>A1.xml</FileLoc></Page><Page PageID="2"><FileLoc>A2.xml</FileLoc></Page></Annotations>`,
		"A1.xml":          `<PageAnnot><Annot ID="10" Type="Stamp"><Remark>缺省备注</Remark></Annot><Annot ID="11" Type="Stamp" Print="false" NoZoom="true" NoRotate="true" ReadOnly="false"><Remark>不打印的备注</Remark><Appearance Boundary="0 0 10 10"><PathObject ID="12" Boundary="0 0 10 10" Fill="true" Stroke="false"><FillColor Value="255 0 0"/><AbbreviatedData>M 0 0 L 10 0 L 10 10 L 0 10 C</AbbreviatedData></PathObject></Appearance></Annot><Annot ID="13" Type="Stamp" Visible="false"><Remark>隐藏备注</Remark></Annot></PageAnnot>`,
		"A2.xml":          `<PageAnnot/>`,
	}
	if brokenSecond {
		files["A2.xml"] = `<PageAnnot><Annot`
	}
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for name, value := range files {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestAnnotationRemarkFlagsUseTable61DefaultsAndKeepAppearance(t *testing.T) {
	r, err := Open(remarkFixture(t, false))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	infos, err := r.AnnotationsForPage(0)
	if err != nil || len(infos) != 3 {
		t.Fatalf("注解=%+v %v", infos, err)
	}
	if !infos[0].Print || infos[0].NoZoom || infos[0].NoRotate || !infos[0].ReadOnly {
		t.Fatalf("缺省值错误：%+v", infos[0])
	}
	if infos[1].Print || !infos[1].NoZoom || !infos[1].NoRotate || infos[1].ReadOnly || infos[1].Boundary == nil {
		t.Fatalf("显式标志丢失：%+v", infos[1])
	}
	for _, purpose := range []PermissionAction{"", PermissionPrint, PermissionExport} {
		var output bytes.Buffer
		if err := r.RenderPage(t.Context(), &output, 0, RenderOptions{DPI: 96, Purpose: purpose}); err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(&output)
		if err != nil {
			t.Fatal(err)
		}
		got := color.NRGBAModel.Convert(img.At(10, 10))
		if got != (color.NRGBA{R: 255, A: 255}) {
			t.Fatalf("用途%q的外观被备注标志改变：%v", purpose, got)
		}
	}
}

func TestPageAnnotationsDoNotScanUnrelatedBrokenPagesAndAllReportsErrors(t *testing.T) {
	r, err := Open(remarkFixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if infos, err := r.AnnotationsForPage(0); err != nil || len(infos) != 3 {
		t.Fatalf("逐页加载扫描了其他页：%+v %v", infos, err)
	}
	if _, err := r.AnnotationsForPage(1); err == nil {
		t.Fatal("损坏目标页未返回错误")
	}
	if _, err := r.Annotations(); err == nil || !strings.Contains(err.Error(), "第 1 页") {
		t.Fatalf("全量注解错误被吞掉：%v", err)
	}
	if _, err := r.AnnotationsForPage(-1); err == nil {
		t.Fatal("负页码应失败")
	}
	if _, err := r.AnnotationsForPage(2); err == nil {
		t.Fatal("越界页码应失败")
	}
}
