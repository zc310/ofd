package render

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
)

// 这些样例使用数字签名 ID。索引的非规范写法不能阻止页面绘制；资源路径
// 确实有误时，绘制报告仍保留诊断，不通过静默跳过整个 fixture 掩盖错误。
func TestLegacySignedFixturesRenderRasterPDFAndText(t *testing.T) {
	for _, tc := range []struct {
		file string
		text []string
	}{
		{"testImageNotFound.ofd", []string{"矿产资源勘查许可证", "test123"}},
		{"testPathClip.ofd", []string{"采矿权延续"}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			ofd, err := parser.NewOFD(filepath.Join("..", "..", "testdata", "ofdrw", tc.file))
			if errors.Is(err, os.ErrNotExist) {
				t.Skipf("缺少 fixture: %v", err)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer ofd.Close()
			doc := NewDocument(color.White, ofd.Documents[0])
			defer doc.Close()
			if len(doc.Pages) != len(tc.text) || doc.GetSignature("1") == nil || doc.GetSignature("2") == nil {
				t.Fatal("兼容读取丢失了页面或原始签名索引")
			}
			for i, page := range doc.Pages {
				t.Run(fmt.Sprintf("page-%d", i+1), func(t *testing.T) {
					surface, report, err := doc.PageWithReport(page, PageVisibility{})
					if err != nil {
						t.Fatal(err)
					}
					check := func(t *testing.T, img image.Image) {
						t.Helper()
						ink := 0
						bounds := img.Bounds()
						for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
							for x := bounds.Min.X; x < bounds.Max.X; x++ {
								r, g, b, _ := img.At(x, y).RGBA()
								if r < 120*257 && g < 120*257 && b < 120*257 {
									ink++
								}
							}
						}
						if ink < 1000 {
							t.Fatalf("页面墨迹仅%d像素，正文可能未绘制", ink)
						}
					}
					check(t, surface.Rasterize(geom.DPI(96)))
					if tc.file == "testImageNotFound.ofd" && i == 0 {
						found := false
						for _, diagnostic := range report.Diagnostics {
							found = found || diagnostic.Code == "image_decode_failed" && diagnostic.ResourceID == 135
						}
						if !found {
							t.Fatal("缺失注解图片的资源135诊断丢失")
						}
					}
					t.Run("PDF pixels", func(t *testing.T) { check(t, clipPDFImage(t, surface, 96)) })
					t.Run("PDF text", func(t *testing.T) { checkPDFExtractedText(t, surface, tc.text[i]) })
				})
			}
		})
	}
}
