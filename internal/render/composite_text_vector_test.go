package render

import (
	"archive/zip"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/parser"
)

// writeTextCompositeOFD 生成只含一个文字型复合图元的最小 OFD。单元坐标系为
// unitW×unitH，CompositeObject 用 CTM 0.5 把单元声明尺寸映射到 100×100mm 的
// Boundary，因此与 y.ofd 一样声明尺寸不可信、按反推范围（200×200mm）建画布。
// textExtra 追加到 TextObject 上，用于构造需要回退栅格化的场景。
func writeTextCompositeOFD(t *testing.T, unitW, unitH float64, text, textAttrs, textChildren string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "text-composite.ofd")

	content := `<?xml version="1.0" encoding="UTF-8"?><ofd:Page xmlns:ofd="http://www.ofdspec.org/2016">` +
		`<ofd:Area><ofd:PhysicalBox>0 0 210 297</ofd:PhysicalBox></ofd:Area><ofd:Content>` +
		`<ofd:Layer ID="3" Type="Body">` +
		`<ofd:CompositeObject ID="4" Boundary="0 0 100 100" ResourceID="5" CTM="0.5 0 0 0.5 0 0"/>` +
		`</ofd:Layer></ofd:Content></ofd:Page>`

	res := `<?xml version="1.0" encoding="UTF-8"?><ofd:Res xmlns:ofd="http://www.ofdspec.org/2016">` +
		fmt.Sprintf(`<ofd:CompositeGraphicUnits><ofd:CompositeGraphicUnit ID="5" Width="%g" Height="%g">`, unitW, unitH) +
		`<ofd:Content ID="6">` +
		fmt.Sprintf(`<ofd:TextObject ID="7" Font="1" Size="5" CTM="10 0 0 10 0 0" `+
			`Boundary="20 100 120 30" Fill="true"%s>%s`, textAttrs, textChildren) +
		`<ofd:FillColor Value="0 0 0"/>` +
		fmt.Sprintf(`<ofd:TextCode X="0" Y="0" DeltaX="5">%s</ofd:TextCode>`, text) +
		`</ofd:TextObject></ofd:Content></ofd:CompositeGraphicUnit></ofd:CompositeGraphicUnits></ofd:Res>`

	document := `<?xml version="1.0" encoding="UTF-8"?><ofd:Document xmlns:ofd="http://www.ofdspec.org/2016">` +
		`<ofd:CommonData><ofd:MaxUnitID>9</ofd:MaxUnitID>` +
		`<ofd:PageArea><ofd:PhysicalBox>0 0 210 297</ofd:PhysicalBox></ofd:PageArea>` +
		`<ofd:DocumentRes>DocumentRes.xml</ofd:DocumentRes></ofd:CommonData>` +
		`<ofd:Pages><ofd:Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></ofd:Pages></ofd:Document>`

	ofd := `<?xml version="1.0" encoding="UTF-8"?><ofd:OFD xmlns:ofd="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD">` +
		`<ofd:DocBody><ofd:DocInfo><ofd:DocID>t</ofd:DocID><ofd:Version>1.0</ofd:Version></ofd:DocInfo>` +
		`<ofd:DocRoot>Doc_0/Document.xml</ofd:DocRoot></ofd:DocBody></ofd:OFD>`

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, data := range map[string]string{
		"OFD.xml":                        ofd,
		"Doc_0/Document.xml":             document,
		"Doc_0/DocumentRes.xml":          res,
		"Doc_0/Pages/Page_0/Content.xml": content,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// compositePageSVG 渲染合成 OFD 的第 1 页并输出 SVG。矢量后端把真实文字写成
// <text> 元素，被栅格化的内容则变成 <image>，因此可以直接区分两者。
func compositePageSVG(t *testing.T, path string) string {
	t.Helper()
	ofd, err := parser.NewOFD(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := ofd.Documents[0]
	surface, err := NewDocument(color.White, doc).Page(doc.Pages[0])
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	if err := surface.Write(&sb, "svg"); err != nil {
		t.Fatal(err)
	}
	return sb.String()
}

// 文字型复合图元必须保持矢量文字：y.ofd 的每个复合单元只有一行文字，此前
// 全部被烘焙成位图，PDF 里没有任何文字算子，pdftotext 提取 0 字符。
func TestCompositeTextStaysVectorText(t *testing.T) {
	svg := compositePageSVG(t, writeTextCompositeOFD(t, 600, 600, "海关总署", "", ""))
	if !strings.Contains(svg, "<text") {
		t.Fatalf("文字型复合图元应输出 <text> 而非位图，实际 SVG:\n%s", svg)
	}
	if !strings.Contains(svg, "海关总署") {
		t.Errorf("SVG 应保留原始文字内容，实际未找到:\n%s", svg)
	}
	if strings.Contains(svg, "<image") {
		t.Errorf("文字型复合图元不应产生位图，实际 SVG:\n%s", svg)
	}
}

// 复合图元自身带裁剪区时无法在矢量分支还原，必须回退栅格化，但内容不能丢。
func TestCompositeObjectClipFallsBackToRaster(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clipped.ofd")
	content := `<?xml version="1.0" encoding="UTF-8"?><ofd:Page xmlns:ofd="http://www.ofdspec.org/2016">` +
		`<ofd:Area><ofd:PhysicalBox>0 0 210 297</ofd:PhysicalBox></ofd:Area><ofd:Content>` +
		`<ofd:Layer ID="3" Type="Body">` +
		`<ofd:CompositeObject ID="4" Boundary="0 0 100 100" ResourceID="5" CTM="0.5 0 0 0.5 0 0">` +
		`<ofd:Clips><ofd:Clip><ofd:Area><ofd:Path Boundary="0 0 50 50" Stroke="false" Fill="true">` +
		`<ofd:AbbreviatedData>M 0 0 L 50 0 L 50 50 L 0 50 C</ofd:AbbreviatedData>` +
		`</ofd:Path></ofd:Area></ofd:Clip></ofd:Clips></ofd:CompositeObject>` +
		`</ofd:Layer></ofd:Content></ofd:Page>`
	res := `<?xml version="1.0" encoding="UTF-8"?><ofd:Res xmlns:ofd="http://www.ofdspec.org/2016">` +
		`<ofd:CompositeGraphicUnits><ofd:CompositeGraphicUnit ID="5" Width="600" Height="600">` +
		`<ofd:Content ID="6">` +
		`<ofd:TextObject ID="7" Font="1" Size="5" CTM="10 0 0 10 0 0" Boundary="20 100 120 30" Fill="true">` +
		`<ofd:FillColor Value="0 0 0"/><ofd:TextCode X="0" Y="0" DeltaX="5">海关总署</ofd:TextCode></ofd:TextObject>` +
		`</ofd:Content></ofd:CompositeGraphicUnit></ofd:CompositeGraphicUnits></ofd:Res>`
	document := `<?xml version="1.0" encoding="UTF-8"?><ofd:Document xmlns:ofd="http://www.ofdspec.org/2016">` +
		`<ofd:CommonData><ofd:MaxUnitID>9</ofd:MaxUnitID>` +
		`<ofd:PageArea><ofd:PhysicalBox>0 0 210 297</ofd:PhysicalBox></ofd:PageArea>` +
		`<ofd:DocumentRes>DocumentRes.xml</ofd:DocumentRes></ofd:CommonData>` +
		`<ofd:Pages><ofd:Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></ofd:Pages></ofd:Document>`
	ofd := `<?xml version="1.0" encoding="UTF-8"?><ofd:OFD xmlns:ofd="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD">` +
		`<ofd:DocBody><ofd:DocInfo><ofd:DocID>t</ofd:DocID><ofd:Version>1.0</ofd:Version></ofd:DocInfo>` +
		`<ofd:DocRoot>Doc_0/Document.xml</ofd:DocRoot></ofd:DocBody></ofd:OFD>`
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, data := range map[string]string{
		"OFD.xml": ofd, "Doc_0/Document.xml": document,
		"Doc_0/DocumentRes.xml": res, "Doc_0/Pages/Page_0/Content.xml": content,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	svg := compositePageSVG(t, path)
	if strings.Contains(svg, "<text") {
		t.Errorf("复合图元带裁剪区时应回退栅格化，不应输出 <text>")
	}
}

// 描边文字无法用 Canvas 原生文字接口表达（会静默丢描边），文字绘制内部会
// 改走字形轮廓。矢量分支必须保留这一行为，不能悄悄变成只有填充的 <text>。
func TestCompositeStrokedTextUsesGlyphOutlines(t *testing.T) {
	svg := compositePageSVG(t, writeTextCompositeOFD(t, 600, 600, "海关总署", ` Stroke="true"`, ""))
	if strings.Contains(svg, "<text") {
		t.Errorf("描边文字应走字形轮廓路径，不应输出只带填充的 <text>")
	}
	// 一条是页面背景矩形，另一条应是字形轮廓；字形轮廓直接写进矢量表面，
	// 不需要中间位图。
	if strings.Count(svg, "<path") < 2 {
		t.Errorf("描边文字应输出字形轮廓路径，实际 SVG:\n%s", truncateSVG(svg))
	}
	if strings.Contains(svg, "<image") {
		t.Errorf("描边文字不应回退到离屏位图，实际 SVG:\n%s", truncateSVG(svg))
	}
}

// 不可见文字不产生任何内容。
func TestCompositeInvisibleTextDrawsNothing(t *testing.T) {
	svg := compositePageSVG(t, writeTextCompositeOFD(t, 600, 600, "海关总署", ` Visible="false"`, ""))
	if strings.Contains(svg, "<text") || strings.Contains(svg, "海关总署") {
		t.Errorf("不可见文字不应被绘制，实际 SVG:\n%s", truncateSVG(svg))
	}
}

// truncateSVG 截断超长 SVG，避免测试日志刷屏。
func truncateSVG(svg string) string {
	if len(svg) <= 400 {
		return svg
	}
	return svg[:200] + " ...(截断)..."
}

// 混有路径的复合单元需要独立绘制表面，保持栅格化。
func TestCompositeMixedContentFallsBackToRaster(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mixed.ofd")
	content := `<?xml version="1.0" encoding="UTF-8"?><ofd:Page xmlns:ofd="http://www.ofdspec.org/2016">` +
		`<ofd:Area><ofd:PhysicalBox>0 0 210 297</ofd:PhysicalBox></ofd:Area><ofd:Content>` +
		`<ofd:Layer ID="3" Type="Body">` +
		`<ofd:CompositeObject ID="4" Boundary="0 0 100 100" ResourceID="5" CTM="0.5 0 0 0.5 0 0"/>` +
		`</ofd:Layer></ofd:Content></ofd:Page>`
	res := `<?xml version="1.0" encoding="UTF-8"?><ofd:Res xmlns:ofd="http://www.ofdspec.org/2016">` +
		`<ofd:CompositeGraphicUnits><ofd:CompositeGraphicUnit ID="5" Width="600" Height="600">` +
		`<ofd:Content ID="6">` +
		`<ofd:TextObject ID="7" Font="1" Size="5" CTM="10 0 0 10 0 0" Boundary="20 100 120 30" Fill="true">` +
		`<ofd:FillColor Value="0 0 0"/><ofd:TextCode X="0" Y="0" DeltaX="5">海关总署</ofd:TextCode></ofd:TextObject>` +
		`<ofd:PathObject ID="8" Boundary="0 0 40 40" Fill="true" Stroke="false">` +
		`<ofd:FillColor Value="0 0 0"/>` +
		`<ofd:AbbreviatedData>M 0 0 L 40 0 L 40 40 L 0 40 C</ofd:AbbreviatedData></ofd:PathObject>` +
		`</ofd:Content></ofd:CompositeGraphicUnit></ofd:CompositeGraphicUnits></ofd:Res>`
	document := `<?xml version="1.0" encoding="UTF-8"?><ofd:Document xmlns:ofd="http://www.ofdspec.org/2016">` +
		`<ofd:CommonData><ofd:MaxUnitID>9</ofd:MaxUnitID>` +
		`<ofd:PageArea><ofd:PhysicalBox>0 0 210 297</ofd:PhysicalBox></ofd:PageArea>` +
		`<ofd:DocumentRes>DocumentRes.xml</ofd:DocumentRes></ofd:CommonData>` +
		`<ofd:Pages><ofd:Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></ofd:Pages></ofd:Document>`
	ofd := `<?xml version="1.0" encoding="UTF-8"?><ofd:OFD xmlns:ofd="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD">` +
		`<ofd:DocBody><ofd:DocInfo><ofd:DocID>t</ofd:DocID><ofd:Version>1.0</ofd:Version></ofd:DocInfo>` +
		`<ofd:DocRoot>Doc_0/Document.xml</ofd:DocRoot></ofd:DocBody></ofd:OFD>`
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, data := range map[string]string{
		"OFD.xml": ofd, "Doc_0/Document.xml": document,
		"Doc_0/DocumentRes.xml": res, "Doc_0/Pages/Page_0/Content.xml": content,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	svg := compositePageSVG(t, path)
	if strings.Contains(svg, "<text") {
		t.Errorf("文字与路径混合的复合单元应回退栅格化，不应输出 <text>:\n%s", svg)
	}
}
