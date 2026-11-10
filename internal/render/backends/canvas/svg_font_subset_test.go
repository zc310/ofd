package canvas

import (
	"bytes"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/internal/thirdparty/font"
	"golang.org/x/image/font/gofont/goregular"
)

// SVG 仅内嵌当前页的字形；复合字形的依赖、Unicode 映射和共享字体必须保持不变。
func TestSVGSubsetsFontsPerPageWithoutChangingSharedFont(t *testing.T) {
	family := canvas.NewFontFamily("SVG subset regression")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	defer family.Destroy()
	face := family.Face(12, canvas.Black)
	original := face.Font.Write()
	originalOS2 := *face.Font.OS2
	for _, value := range []string{"AÁ", "Z", "AÁ"} {
		page := canvas.New(60, 30)
		canvas.NewContext(page).DrawText(5, 10, canvas.NewTextLine(face, value, canvas.Left))
		surface := canvasVectorSurface{c: page}
		before := surface.Rasterize(geom.DPMM(2), nil)
		var output bytes.Buffer
		if err := surface.Write(&output, "svg"); err != nil {
			t.Fatal(err)
		}
		programs := svgFontPrograms(t, output.Bytes())
		if len(programs) != 1 {
			t.Fatalf("字体数量 = %d，期望 1", len(programs))
		}
		for _, program := range programs {
			if len(program) >= len(original)/2 {
				t.Fatalf("SVG 仍嵌入完整字体：输出 %d 字节，原字体 %d 字节", len(program), len(original))
			}
			subset, err := font.ParseSFNT(program, 0)
			if err != nil {
				t.Fatal(err)
			}
			if subset.NumGlyphs() >= face.Font.NumGlyphs() {
				t.Fatal("字体字形数量未减少")
			}
			for _, r := range value {
				glyph := subset.GlyphIndex(r)
				if glyph == 0 {
					t.Fatalf("子集丢失字符 %c", r)
				}
				var want, got canvas.Path
				if err := face.Font.GlyphPath(&want, face.Font.GlyphIndex(r), 0, 0, 0, 1, font.NoHinting); err != nil {
					t.Fatal(err)
				}
				if err := subset.GlyphPath(&got, glyph, 0, 0, 0, 1, font.NoHinting); err != nil {
					t.Fatal(err)
				}
				if want.String() != got.String() || face.Font.GlyphAdvance(face.Font.GlyphIndex(r)) != subset.GlyphAdvance(glyph) {
					t.Fatalf("子集改变了字符 %c 的轮廓或字宽", r)
				}
			}
			if subset.GlyphIndex('B') != 0 || subset.GlyphIndex('Z') != 0 && value != "Z" {
				t.Fatal("子集保留了未使用的字符")
			}
		}
		if !bytes.Equal(face.Font.Write(), original) || *face.Font.OS2 != originalOS2 {
			t.Fatal("SVG 子集化修改了共享字体")
		}
		if after := surface.Rasterize(geom.DPMM(2), nil); !bytes.Equal(before.Pix, after.Pix) {
			t.Fatal("SVG 导出改变了光栅结果")
		}
	}
}
