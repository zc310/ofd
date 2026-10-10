package render

import (
	"archive/zip"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
)

// writeSealLikeCompositeOFD 生成一枚「印章型」复合图元：CompositeGraphicUnit 里
// 只有两个描边方环，CompositeObject 没有 CTM。unitW/unitH 声明单元坐标系尺寸。
//
// 铁路电子客票的发票监制章就是这个写法——章只有 30×20mm，单元却声明 210×297，
// 于是「单元内容铺满单元」这个 DPI 推算前提不成立。
func writeSealLikeCompositeOFD(t *testing.T, unitW, unitH, boxW, boxH float64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "seal-composite.ofd")

	content := `<?xml version="1.0" encoding="UTF-8"?><ofd:Page xmlns:ofd="http://www.ofdspec.org/2016">` +
		`<ofd:Area><ofd:PhysicalBox>0 0 210 297</ofd:PhysicalBox></ofd:Area><ofd:Content>` +
		`<ofd:Layer ID="3" Type="Body">` +
		fmt.Sprintf(`<ofd:CompositeObject ID="4" Boundary="90 4 %g %g" ResourceID="5"/>`, boxW, boxH) +
		`</ofd:Layer></ofd:Content></ofd:Page>`

	res := `<?xml version="1.0" encoding="UTF-8"?><ofd:Res xmlns:ofd="http://www.ofdspec.org/2016">` +
		fmt.Sprintf(`<ofd:CompositeGraphicUnits><ofd:CompositeGraphicUnit ID="5" Width="%g" Height="%g">`, unitW, unitH) +
		`<ofd:Content ID="6">` +
		`<ofd:PathObject ID="7" Boundary="-0.5 -0.5 31 21" Stroke="true" Fill="false" LineWidth="1">` +
		`<ofd:StrokeColor Value="255 0 0"/>` +
		`<ofd:AbbreviatedData>M 1 10.5 L 15.5 1 L 30 10.5 L 23.5 20 L 7.5 20 C</ofd:AbbreviatedData>` +
		`</ofd:PathObject>` +
		`<ofd:PathObject ID="8" Boundary="1.2 1.2 27.6 17.6" Stroke="true" Fill="false" LineWidth="0.3">` +
		`<ofd:StrokeColor Value="255 0 0"/>` +
		`<ofd:AbbreviatedData>M 0.3 8.8 L 13.8 0.3 L 27.3 8.8 L 21.3 17.3 L 6.3 17.3 C</ofd:AbbreviatedData>` +
		`</ofd:PathObject></ofd:Content></ofd:CompositeGraphicUnit></ofd:CompositeGraphicUnits></ofd:Res>`

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

// renderSealComposite 以给定单元声明尺寸渲染印章型复合图元。
func renderSealComposite(t *testing.T, unitW, unitH, boxW, boxH float64, dpi float64) *image.RGBA {
	t.Helper()
	ofd, err := parser.NewOFD(writeSealLikeCompositeOFD(t, unitW, unitH, boxW, boxH))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := ofd.Documents[0]
	raster, err := NewDocument(color.Transparent, doc).
		RasterizePage(doc.Pages[0], BackendCanvas, geom.DPI(dpi))
	if err != nil {
		t.Fatal(err)
	}
	return raster
}

// 复合单元声明尺寸远大于实际内容时，内容不能被烘焙成低分辨率位图再放大。
//
// 离屏 DPI 由「输出 DPI × Boundary 宽 ÷ 单元宽」推得，前提是内容铺满单元。声明
// 210×297 的单元里只放一枚 30×20mm 的章时这个前提不成立，DPI 塌到四十左右，章在
// 离屏画布上只有五十像素宽，再拉到 300dpi 输出就是一圈锯齿。
//
// 判据取「同一枚章在单元声明尺寸刚好贴合时」的渲染作参照：两种写法画的是同一份
// 内容、放在同一个 Boundary，输出本应几乎一致。实测不修复时相差 0.27% 的像素
// （环被放大成锯齿），修复后 0.11%（只剩抗锯齿与取整噪声），阈值取 0.2%。
func TestCompositeResamplesWhenDeclaredUnitIsLargerThanContent(t *testing.T) {
	const dpi = 300
	oversized := renderSealComposite(t, 210, 297, 30, 20, dpi)
	fitted := renderSealComposite(t, 32, 22, 30, 20, dpi)

	if diff, total := differingPixels(oversized, fitted); diff*500 > total {
		t.Errorf("单元声明 210×297 与贴合声明的渲染相差 %d/%d 像素（%.3f%%，阈值 0.2%%），"+
			"离屏 DPI 应按实测可见范围而不是单元声明尺寸推算",
			diff, total, float64(diff)*100/float64(total))
	}
}

// differingPixels 返回两幅同尺寸图像中通道值不同的像素数。
func differingPixels(a, b *image.RGBA) (diff, total int) {
	if a.Bounds() != b.Bounds() {
		return -1, 1
	}
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			diff++
		}
		total++
	}
	return diff, total
}

// 重画之后印章仍要铺满 Boundary 并落在原位，不能因为画布收缩而偏移或缩小。
func TestCompositeResampleKeepsPlacement(t *testing.T) {
	const dpi = 300
	raster := renderSealComposite(t, 210, 297, 30, 20, dpi)
	x0, y0, x1, y1 := contentImageBounds(raster)
	mmPerPx := 25.4 / dpi
	// Boundary 是 90 4 30 20；描边环比它略小，留 1mm 余量吸收取整。
	if got := float64(x0) * mmPerPx; got < 89 || got > 91 {
		t.Errorf("印章左边界应在 90mm 附近，实测 %.1fmm", got)
	}
	if got := float64(y0) * mmPerPx; got < 3 || got > 5 {
		t.Errorf("印章上边界应在 4mm 附近，实测 %.1fmm", got)
	}
	if got := float64(x1-x0) * mmPerPx; got < 28 || got > 31 {
		t.Errorf("印章宽应约 30mm，实测 %.1fmm", got)
	}
	if got := float64(y1-y0) * mmPerPx; got < 18 || got > 21 {
		t.Errorf("印章高应约 20mm，实测 %.1fmm", got)
	}
}
