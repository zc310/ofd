package render

import (
	"archive/zip"
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
)

// writeCompositeOFD 生成只含一个复合图元的最小 OFD。unitW/unitH 是复合单元
// 声明的尺寸，rect 是单元坐标系内的一个实心方块，用于量出它在页面上的落点。
func writeCompositeOFD(t *testing.T, unitW, unitH float64, rect string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "composite.ofd")

	content := `<?xml version="1.0" encoding="UTF-8"?><ofd:Page xmlns:ofd="http://www.ofdspec.org/2016">` +
		`<ofd:Area><ofd:PhysicalBox>0 0 210 297</ofd:PhysicalBox></ofd:Area><ofd:Content>` +
		`<ofd:Layer ID="3" Type="Body">` +
		`<ofd:CompositeObject ID="4" Boundary="0 0 100 100" ResourceID="5" CTM="0.5 0 0 0.5 0 0"/>` +
		`</ofd:Layer></ofd:Content></ofd:Page>`

	res := `<?xml version="1.0" encoding="UTF-8"?><ofd:Res xmlns:ofd="http://www.ofdspec.org/2016">` +
		fmt.Sprintf(`<ofd:CompositeGraphicUnits><ofd:CompositeGraphicUnit ID="5" Width="%g" Height="%g">`, unitW, unitH) +
		`<ofd:Content ID="6">` +
		`<ofd:PathObject ID="7" Boundary="0 0 10 10" Fill="true" Stroke="false">` +
		`<ofd:FillColor Value="0 0 0"/>` +
		// Clips 使单路径复合图元不走矢量快路径，与 y.ofd 的复合单元一致。
		`<ofd:Clips><ofd:Clip><ofd:Area>` +
		`<ofd:Path Boundary="0 0 4000 4000" Stroke="false" Fill="true">` +
		`<ofd:AbbreviatedData>M 0 0 L 4000 0 L 4000 4000 L 0 4000 C</ofd:AbbreviatedData>` +
		`</ofd:Path></ofd:Area></ofd:Clip></ofd:Clips>` + rect +
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

// compositeInkBox 渲染合成 OFD 的第 1 页，返回页面中非白内容的包围盒（毫米）。
func compositeInkBox(t *testing.T, path string) (x0, y0, x1, y1 float64) {
	t.Helper()
	ofd, err := parser.NewOFD(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := ofd.Documents[0]
	// 72dpi：1mm = 72/25.4 px，取整误差约 0.35mm。
	const dpi = 72
	img, err := NewDocument(color.White, doc).RasterizePage(doc.Pages[0], BackendCanvas, geom.DPI(dpi))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	mmPerPx := 25.4 / dpi
	const threshold = 250
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X, b.Min.Y
	found := false
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r>>8 < threshold || g>>8 < threshold || bl>>8 < threshold {
				found = true
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if !found {
		t.Fatal("页面没有任何内容")
	}
	return float64(minX) * mmPerPx, float64(minY) * mmPerPx,
		float64(maxX+1) * mmPerPx, float64(maxY+1) * mmPerPx
}

const compositeRect20 = `<ofd:AbbreviatedData>M 50 50 L 70 50 L 70 70 L 50 70 C</ofd:AbbreviatedData>`

// TestCompositeUnitDeclaredSizeMismatchKeepsContentScale 回归：复合单元声明的
// Width/Height 与 Boundary/CTM 反推的内容范围严重不符时，声明尺寸不可信，必须
// 按反推范围作为内容画布且不按透明像素裁剪，否则单元里的小图元会被拉伸铺满
// 整个 Boundary（y.ofd 每个单元只有一个字，整页会变成黑块）。
func TestCompositeUnitDeclaredSizeMismatchKeepsContentScale(t *testing.T) {
	// Boundary/CTM = 100/0.5 = 200mm；声明 600mm（3 倍）应被判为不可信。
	path := writeCompositeOFD(t, 600, 600, compositeRect20)
	x0, y0, x1, y1 := compositeInkBox(t, path)

	// 20x20mm 的方块在 200mm 画布上按 CTM 0.5 缩放后应为 10x10mm，
	// 位置 (50,50)*0.5 = (25,25)mm。
	const tol = 1.2
	if got := x1 - x0; abs(got-10) > tol {
		t.Errorf("方块宽 %.2fmm, want 10mm（不应被拉伸铺满 100mm Boundary）", got)
	}
	if got := y1 - y0; abs(got-10) > tol {
		t.Errorf("方块高 %.2fmm, want 10mm（不应被拉伸铺满 100mm Boundary）", got)
	}
	if abs(x0-25) > tol || abs(y0-25) > tol {
		t.Errorf("方块左上角 (%.2f, %.2f)mm, want (25, 25)mm", x0, y0)
	}
}

// TestCompositeUnitDeclaredSizeMatchKeepsInkCrop 回归：声明尺寸与 Boundary/CTM
// 一致时保持原有行为——裁掉单元四周透明区域后把可见内容映射到 Boundary，
// intro.ofd 等样例依赖该行为。
func TestCompositeUnitDeclaredSizeMatchKeepsInkCrop(t *testing.T) {
	path := writeCompositeOFD(t, 200, 200, compositeRect20)
	x0, y0, x1, y1 := compositeInkBox(t, path)

	// 裁剪后 20x20mm 的方块铺满 100x100mm 的 Boundary。
	const tol = 1.2
	if got := x1 - x0; abs(got-100) > tol {
		t.Errorf("铺满 Boundary 时方块宽 %.2fmm, want 100mm", got)
	}
	if got := y1 - y0; abs(got-100) > tol {
		t.Errorf("铺满 Boundary 时方块高 %.2fmm, want 100mm", got)
	}
	if abs(x0) > tol || abs(y0) > tol {
		t.Errorf("铺满 Boundary 时左上角 (%.2f, %.2f)mm, want (0, 0)mm", x0, y0)
	}
}

func TestCompositeContentExtent(t *testing.T) {
	ctm := models.CTM{0.3528, 0, 0, 0.3528, 0, 0}
	box := models.StBox{Width: 204.0008, Height: 264.0013}

	w, h, ok := compositeContentExtent(models.CompositeObject{
		CtComposite: models.CtComposite{CTGraphicUnit: models.CTGraphicUnit{CTM: &ctm, Boundary: box}},
	})
	if !ok {
		t.Fatal("轴向缩放的 CTM 应能反推内容范围")
	}
	if abs(w-578.2) > 0.5 || abs(h-748.3) > 0.5 {
		t.Errorf("反推范围 = %.2fx%.2f, want 578.2x748.3", w, h)
	}

	for name, object := range map[string]models.CompositeObject{
		"无 CTM": {CtComposite: models.CtComposite{CTGraphicUnit: models.CTGraphicUnit{Boundary: box}}},
		"旋转 CTM": {CtComposite: models.CtComposite{CTGraphicUnit: models.CTGraphicUnit{
			CTM: &models.CTM{0.3528, 0.2, -0.2, 0.3528, 0, 0}, Boundary: box}}},
		"零缩放": {CtComposite: models.CtComposite{CTGraphicUnit: models.CTGraphicUnit{
			CTM: &models.CTM{0, 0, 0, 0, 0, 0}, Boundary: box}}},
	} {
		if _, _, ok := compositeContentExtent(object); ok {
			t.Errorf("%s 不应反推出内容范围", name)
		}
	}
}

func TestCompositeExtentMismatch(t *testing.T) {
	cases := []struct {
		declared, extent float64
		want             bool
	}{
		{1639.19, 578.23, true}, // y.ofd：声明尺寸是反推范围的 2.83 倍
		{2653.63, 936.08, true}, // intro.ofd：同样是 2.83 倍
		{200, 200, false},       // 一致
		{200, 198, false},       // 1% 误差仍视为一致
		{200, 150, true},        // 明显偏小
		{0, 578, false},         // 声明尺寸非法时不判定
	}
	for _, tc := range cases {
		if got := compositeExtentMismatch(tc.declared, tc.extent); got != tc.want {
			t.Errorf("compositeExtentMismatch(%g, %g) = %v, want %v", tc.declared, tc.extent, got, tc.want)
		}
	}
}

func TestCompositeContentExtentIgnoresNonFinite(t *testing.T) {
	bad := models.CTM{1, 0, 0, 1, math.NaN(), 0}
	if _, _, ok := compositeContentExtent(models.CompositeObject{
		CtComposite: models.CtComposite{CTGraphicUnit: models.CTGraphicUnit{
			CTM: &bad, Boundary: models.StBox{Width: 10, Height: 10}}},
	}); ok {
		t.Fatal("非有限 CTM 不应反推出内容范围")
	}
}
