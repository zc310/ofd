package canvas

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/pdfcpuwriter"
	"github.com/zc310/ofd/internal/testutil"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func prototypeCFFFamily(t *testing.T, filename string) *canvas.FontFamily {
	t.Helper()
	const input = "../../../../testdata/ofdrw/intro.ofd"
	z, err := zip.OpenReader(input)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("缺少样例 %s", input)
		}
		t.Fatal(err)
	}
	defer z.Close()
	for _, file := range z.File {
		if !strings.HasSuffix(file.Name, "/"+filename) {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		data, err = fontrepair.Repair(data)
		if err != nil {
			t.Fatal(err)
		}
		family := canvas.NewFontFamily(filename)
		if err := family.LoadFont(data, 0, canvas.FontRegular); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(family.Destroy)
		return family
	}
	t.Fatalf("样例%s缺少字体%s", input, filename)
	return nil
}

func TestPDFCPUPrototypeEmbeddedFontProgramsPreserveGlyphsAndAliases(t *testing.T) {
	for index, filename := range []string{"font_83_83.cff", "font_92_92.cff", "font_225_225.cff", "font_764_764.otf"} {
		t.Run(filename, func(t *testing.T) {
			family := prototypeCFFFamily(t, filename)
			face := family.Face(24, canvas.Black)
			ids := make([]uint16, 0, 2)
			for gid := uint16(1); gid < face.Font.NumGlyphs() && len(ids) < 2; gid++ {
				x0, y0, x1, y1 := face.Font.GlyphBounds(gid)
				if x0 < x1 && y0 < y1 {
					ids = append(ids, gid)
				}
			}
			if len(ids) != 2 {
				t.Fatal("样例字体没有两个可见字形")
			}
			value := string([]rune{fontrepair.GlyphRune(ids[0]), fontrepair.GlyphRune(ids[1]), fontrepair.GlyphRune(ids[0])})
			text := canvas.NewTextLine(face, value, canvas.Left)
			text.WalkSpans(func(_, _ float64, span canvas.TextSpan) {
				if len(span.Glyphs) != 3 || span.Glyphs[0].ID != ids[0] || span.Glyphs[1].ID != ids[1] || span.Glyphs[2].ID != ids[0] {
					t.Fatalf("样例未按实际GID寻址：%v", span.Glyphs)
				}
				span.Glyphs[0].Text, span.Glyphs[1].Text, span.Glyphs[2].Text = "A", "B", "😀"
			})
			r, err := pdfcpuwriter.New(t.Context(), 70, 30)
			if err != nil {
				t.Fatal(err)
			}
			matrix := canvas.Identity.Translate(5, 12)
			r.RenderText(text, matrix)
			var output bytes.Buffer
			if err := r.Write(&output); err != nil {
				t.Fatal(err)
			}
			data := output.Bytes()
			if err := api.Validate(t.Context(), bytes.NewReader(data), model.NewDefaultConfiguration(), nil); err != nil {
				t.Fatal(err)
			}
			if face.Font.IsCFF {
				_, _, _, cid := face.Font.CFF.CIDSystemInfo()
				t.Logf("CID键控=%v，Font DICT数量=%d，UPM=%d，FontMatrix=%v，PDF=%d字节", cid, face.Font.CFF.NumFontDicts(), face.Font.UnitsPerEm(), face.Font.CFF.TopDICT().FontMatrix, len(data))
			} else {
				t.Logf("真实格式为TrueType，不能依据扩展名推断CFF：%s", filename)
			}
			if got := prototypeExtractText(t, data); !bytes.Contains(got, []byte("AB😀")) {
				t.Fatalf("CFF字形别名或非BMP语义丢失：%q", got)
			}
			t.Run("源字形像素", func(t *testing.T) {
				tool, err := exec.LookPath("pdftoppm")
				if err != nil {
					t.Skip("缺少pdftoppm")
				}
				// The independent reference draws the source outlines, not an embedded
				// CFF font, so an incorrect CID/name mapping cannot cancel itself out.
				reference := renderPDFText(t, 70, 30, func(pdf pdfTextRenderer) { text.RenderTo(pdf, matrix, 0) })
				prototypeSavePage(t, 700+index, data, reference)
				first, err := testutil.RasterizePDFPages(t.Context(), tool, data, 144)
				if err != nil {
					t.Fatal(err)
				}
				second, err := testutil.RasterizePDFPages(t.Context(), tool, reference, 144)
				if err != nil {
					t.Fatal(err)
				}
				comparison, err := testutil.ComparePageImages(first, second, 20)
				if err != nil || comparison.PixelMatchRate < .99 {
					t.Fatalf("CFF可见字形与源轮廓不符：%+v，%v", comparison, err)
				}
				prototypeCheckFontInk(t, first[0], second[0])
				t.Logf("像素对照：%+v", comparison)
			})
		})
	}
}

// Native font hinting can move edge pixels relative to raw outline fills. Check
// ink in both directions with at most one pixel of displacement so missing or
// substituted glyphs cannot pass just because the rest of the page is white.
func prototypeCheckFontInk(t *testing.T, first, second image.Image) {
	t.Helper()
	ink, unmatched := 0, 0
	for _, pair := range [][2]image.Image{{first, second}, {second, first}} {
		for y := pair[0].Bounds().Min.Y; y < pair[0].Bounds().Max.Y; y++ {
			for x := pair[0].Bounds().Min.X; x < pair[0].Bounds().Max.X; x++ {
				if !prototypeFontInkAt(pair[0], x, y) {
					continue
				}
				ink++
				matched := false
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						matched = matched || prototypeFontInkAt(pair[1], x+dx, y+dy)
					}
				}
				if !matched {
					unmatched++
				}
			}
		}
	}
	if ink < 50 || float64(unmatched)/float64(ink) > .01 {
		t.Fatalf("原生字体与源字形不符：墨迹%d像素，超过1像素位移的墨迹%d像素", ink, unmatched)
	}
}

func prototypeFontInkAt(img image.Image, x, y int) bool {
	if !image.Pt(x, y).In(img.Bounds()) {
		return false
	}
	r, g, b, _ := img.At(x, y).RGBA()
	return r < 0x8000 && g < 0x8000 && b < 0x8000
}

func TestPDFCPUPrototypeSimpleCFFRejectsCodeOverflowWithoutPartialOutput(t *testing.T) {
	family := prototypeCFFFamily(t, "font_764_764.otf")
	face := family.Face(24, canvas.Black)
	r, err := pdfcpuwriter.New(t.Context(), 70, 30)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 256 {
		text := canvas.NewTextLine(face, string(fontrepair.GlyphRune(1)), canvas.Left)
		text.WalkSpans(func(_, _ float64, span canvas.TextSpan) { span.Glyphs[0].Text = fmt.Sprintf("alias-%d", i) })
		r.RenderText(text, canvas.Identity.Translate(5, 10))
	}
	var output bytes.Buffer
	if err := r.Write(&output); !errors.Is(err, pdfcpuwriter.ErrUnsupported) || output.Len() != 0 {
		t.Fatalf("名称键控CFF超过255个编码时未明确失败：%v，输出%d字节", err, output.Len())
	}
}
