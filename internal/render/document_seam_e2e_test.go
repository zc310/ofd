package render

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/ses"
	"github.com/zc310/ofd/pkg/sign"
)

// seamGradientImage 生成一枚颜色随位置连续变化的测试印章：R 恒为 255，G 随
// 行、B 随列递增（封顶 200，避免右下角退化成会被键出的纯白）。这样任一裁片
// 的取色点都能唯一反推出它在原印章上的纵向/横向位置，从而验证骑缝章拼片取
// 的是印章上正确的那一段。
func seamGradientImage(size int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	last := float64(size - 1)
	for y := range size {
		for x := range size {
			img.SetNRGBA(x, y, color.NRGBA{
				R: 255,
				G: uint8(float64(y) / last * 200),
				B: uint8(float64(x) / last * 200),
				A: 255,
			})
		}
	}
	return img
}

// seamExpected 是一枚骑缝章裁片在页面上应出现的落点与原印章取色点。
type seamExpected struct {
	edge         string
	dst          models.StBox // 页面上墨迹应覆盖的矩形（毫米，左上原点）
	probeDst     [2]float64   // 取色点相对于 dst 中心的比例
	wantG, wantB uint8
}

// seamExpectedFor 独立于 pkg/sign 的实现重算骑缝章几何：验证端到端链路时，
// 期望值不能复用被测代码，否则错误会被一并复制。
func seamExpectedFor(edge string, index, pieces int, width, height, size float64) seamExpected {
	strip := size / float64(pieces)
	ratio := (float64(index) + 0.5) / float64(pieces)
	e := seamExpected{edge: edge, probeDst: [2]float64{0.5, 0.5}}
	switch edge {
	case "left":
		e.dst = models.StBox{X: 0, Y: (height - size) / 2, Width: strip, Height: size}
		e.wantG, e.wantB = 100, uint8(ratio*200)
	case "right":
		e.dst = models.StBox{X: width - strip, Y: (height - size) / 2, Width: strip, Height: size}
		e.wantG, e.wantB = 100, uint8(ratio*200)
	case "top":
		e.dst = models.StBox{X: (width - size) / 2, Y: 0, Width: size, Height: strip}
		e.wantG, e.wantB = uint8(ratio*200), 100
	case "bottom":
		e.dst = models.StBox{X: (width - size) / 2, Y: height - strip, Width: size, Height: strip}
		e.wantG, e.wantB = uint8(ratio*200), 100
	}
	return e
}

// buildSeamTestSeal 用真实 SES 链路生成一枚带渐变图案的 Seal.esl。
func buildSeamTestSeal(t *testing.T, dir string) string {
	t.Helper()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, seamGradientImage(100)); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1700000000, 0)
	key, _, certDER, err := ses.NewSelfSignedCertificate("骑缝章测试", "测试", now)
	if err != nil {
		t.Fatal(err)
	}
	seal, err := ses.BuildSeal(ses.SealParams{
		Provider:    "seam-test",
		ESID:        "seam-test@example.com",
		Name:        "骑缝章测试",
		PictureType: "png",
		PictureData: encoded.Bytes(),
		Width:       100,
		Height:      100,
	}, certDER, key, now)
	if err != nil {
		t.Fatal(err)
	}
	der, err := seal.MarshalDER()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Seal.esl")
	if err := os.WriteFile(path, der, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// measureSealWindow 在给定窗口内测量不透明墨迹的包围盒（毫米）。
func measureSealWindow(img *image.RGBA, window models.StBox) (models.StBox, bool) {
	const mmPerPx = 25.4 / 96.0
	b := img.Bounds()
	x0 := max(int(window.X/mmPerPx), b.Min.X)
	y0 := max(int(window.Y/mmPerPx), b.Min.Y)
	x1 := min(int((window.X+window.Width)/mmPerPx), b.Max.X)
	y1 := min(int((window.Y+window.Height)/mmPerPx), b.Max.Y)
	loX, hiX, loY, hiY := b.Max.X, b.Min.X, b.Max.Y, b.Min.Y
	for py := y0; py < y1; py++ {
		for px := x0; px < x1; px++ {
			r, g, bl, a := img.At(px, py).RGBA()
			if a == 0 {
				continue
			}
			// 只统计“红色主导”的像素（渐变印章 R 恒为 255、G/B 不超过
			// 200），避免把页面正文、边框等灰黑色内容算成印章墨迹。
			if r>>8 < 220 || int(r>>8)-int(g>>8) < 30 || int(r>>8)-int(bl>>8) < 30 {
				continue
			}
			loX, hiX = min(loX, px), max(hiX, px)
			loY, hiY = min(loY, py), max(hiY, py)
		}
	}
	if loX > hiX || loY > hiY {
		return models.StBox{}, false
	}
	return models.StBox{
		X:      float64(loX) * mmPerPx,
		Y:      float64(loY) * mmPerPx,
		Width:  float64(hiX-loX+1) * mmPerPx,
		Height: float64(hiY-loY+1) * mmPerPx,
	}, true
}

// TestSeamStampEndToEndRendersOnCorrectEdge 端到端回归：签出的骑缝章经过
// Signature.xml -> parser -> renderer 后，top/bottom/all 每个裁片必须落在正确
// 页面边缘的正确位置，并取到原印章上对应的那一段（取色验证拼片顺序）。
func TestSeamStampEndToEndRendersOnCorrectEdge(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("外部签名命令依赖类 Unix 工具")
	}
	input, err := os.ReadFile(filepath.Join("..", "..", "testdata", "ofdrw/999.ofd"))
	if err != nil {
		t.Skipf("999.ofd 不可用: %v", err)
	}
	sealPath := buildSeamTestSeal(t, t.TempDir())

	const (
		size   = 40.0
		pieces = 5 // 999.ofd 有 5 页
	)
	for _, edge := range []string{"top", "bottom", "all"} {
		t.Run(edge, func(t *testing.T) {
			var signed bytes.Buffer
			if err := sign.Sign(input, &signed, sign.Options{
				Command: "cat",
				Seal:    sealPath,
				StampSeam: &sign.StampSeamOptions{
					Edge: edge,
					Size: size,
					X:    -1,
					Y:    -1,
				},
			}); err != nil {
				t.Fatalf("签名失败: %v", err)
			}

			ofd, err := parser.NewOFD(signed.Bytes())
			if err != nil {
				t.Fatalf("解析签出的文档失败: %v", err)
			}
			defer ofd.Close()
			if len(ofd.Documents) == 0 {
				t.Fatal("文档体缺失")
			}
			document := ofd.Documents[0]
			if len(document.Pages) != pieces {
				t.Fatalf("页数 = %d, 期望 %d", len(document.Pages), pieces)
			}

			expectedEdges := []string{edge}
			if edge == "all" {
				expectedEdges = []string{"left", "right", "top", "bottom"}
			}

			for index, page := range document.Pages {
				box, err := page.PhysicalBox()
				if err != nil {
					t.Fatalf("页面 %v 尺寸读取失败: %v", page.ID, err)
				}
				seals := document.GetSeals(page.ID)
				if len(seals) != len(expectedEdges) {
					t.Fatalf("页面 %v 印章数 = %d, 期望 %d", page.ID, len(seals), len(expectedEdges))
				}
				doc := NewDocumentWithDPI(canvas.White, document, geom.DPI(96))
				raster, err := doc.RasterizePage(page, BackendCanvas, geom.DPI(96))
				if err != nil {
					t.Fatalf("页面 %v 渲染失败: %v", page.ID, err)
				}
				for e, edgeName := range expectedEdges {
					want := seamExpectedFor(edgeName, index, pieces, box.Width, box.Height, size)
					if seals[e].StampAnnot == nil {
						t.Fatalf("页面 %v %s 印章缺少 StampAnnot", page.ID, edgeName)
					}
					window := want.dst
					window.X -= 1.5
					window.Y -= 1.5
					window.Width += 3
					window.Height += 3
					got, ok := measureSealWindow(raster, window)
					if !ok {
						t.Fatalf("页面 %v %s 裁片未渲染", page.ID, edgeName)
					}
					const tol = 1.2
					for _, c := range []struct {
						label     string
						got, want float64
					}{
						{"左边界", got.X, want.dst.X},
						{"上边界", got.Y, want.dst.Y},
						{"宽度", got.Width, want.dst.Width},
						{"高度", got.Height, want.dst.Height},
					} {
						if abs(c.got-c.want) > tol {
							t.Errorf("页面 %v %s 墨迹%s = %.1fmm, want %.1fmm", page.ID, edgeName, c.label, c.got, c.want)
						}
					}

					// 取色点验证裁片来自原印章的正确区段。
					const mmPerPx = 25.4 / 96.0
					px := int((want.dst.X + want.dst.Width*want.probeDst[0]) / mmPerPx)
					py := int((want.dst.Y + want.dst.Height*want.probeDst[1]) / mmPerPx)
					r, g, b, _ := raster.At(px, py).RGBA()
					if diff := int(r>>8) - 255; diff < -20 || diff > 20 {
						t.Errorf("页面 %v %s 取色 R = %d, want ~255", page.ID, edgeName, r>>8)
					}
					if abs(float64(int(g>>8))-float64(want.wantG)) > 30 {
						t.Errorf("页面 %v %s 取色 G = %d, want ~%d", page.ID, edgeName, g>>8, want.wantG)
					}
					if abs(float64(int(b>>8))-float64(want.wantB)) > 30 {
						t.Errorf("页面 %v %s 取色 B = %d, want ~%d", page.ID, edgeName, b>>8, want.wantB)
					}
				}
			}
		})
	}
}
