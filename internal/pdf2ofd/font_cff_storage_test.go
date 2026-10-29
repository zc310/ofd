package pdf2ofd

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"strings"
	"testing"

	fontparser "github.com/zc310/font"
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

// 用独立构造的两字形 CFF 模拟 PDF 生成器遗留的私有字段，避免依赖系统字体。
func legacyPrivateCFF() []byte {
	index := func(objects ...[]byte) []byte {
		if len(objects) == 0 {
			return []byte{0, 0}
		}
		b := make([]byte, 3+4*(len(objects)+1))
		binary.BigEndian.PutUint16(b, uint16(len(objects)))
		b[2] = 4
		offset := 1
		for i, object := range objects {
			binary.BigEndian.PutUint32(b[3+4*i:], uint32(offset))
			offset += len(object)
		}
		binary.BigEndian.PutUint32(b[3+4*len(objects):], uint32(offset))
		for _, object := range objects {
			b = append(b, object...)
		}
		return b
	}
	dict := func(op byte, values ...int) []byte {
		var b []byte
		for _, value := range values {
			b = append(b, 29, byte(value>>24), byte(value>>16), byte(value>>8), byte(value))
		}
		return append(b, op)
	}
	names := index([]byte("LegacyFixture"))
	private := append(dict(20, 600), 139, 12, 15, 138, 12, 16)
	charset := []byte{0, 0, 34} // 标准 SID 34 为 A。
	chars := index([]byte{14}, []byte{139, 139, 21, 239, 139, 139, 239, 39, 139, 139, 39, 5, 14})
	top := append(dict(15, 0), dict(17, 0)...)
	top = append(top, dict(18, len(private), 0)...)
	base := 4 + len(names) + len(index(top)) + 4
	binary.BigEndian.PutUint32(top[1:], uint32(base))
	binary.BigEndian.PutUint32(top[7:], uint32(base+len(charset)))
	binary.BigEndian.PutUint32(top[18:], uint32(base+len(charset)+len(chars)))
	b := append([]byte{1, 0, 4, 4}, names...)
	b = append(b, index(top)...)
	b = append(b, index()...)
	b = append(b, index()...)
	b = append(b, charset...)
	b = append(b, chars...)
	return append(b, private...)
}

func fontStoragePDF(data []byte, subtype string, cid bool) []byte {
	content := "BT /F1 12 Tf 20 100 Td (A) Tj ET"
	font := "<< /Type /Font /Subtype /Type1 /BaseFont /LegacyFixture /Encoding /WinAnsiEncoding /FirstChar 65 /LastChar 65 /Widths [600] /FontDescriptor 6 0 R >>"
	if cid {
		content = "BT /F1 12 Tf 20 100 Td <004a> Tj ET"
		font = "<< /Type /Font /Subtype /Type0 /BaseFont /LegacyFixture /Encoding /Identity-H /DescendantFonts [8 0 R] >>"
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		font,
		"<< /Type /FontDescriptor /FontName /LegacyFixture /Flags 32 /Ascent 800 /Descent -200 /FontBBox [0 -200 1000 800] /CapHeight 700 /ItalicAngle 0 /StemV 80 /FontFile3 7 0 R >>",
		fmt.Sprintf("<< /Subtype /%s /Length %d >>\nstream\n%s\nendstream", subtype, len(data), data),
	}
	if cid {
		objects = append(objects, "<< /Type /Font /Subtype /CIDFontType0 /BaseFont /LegacyFixture /CIDSystemInfo << /Registry (Founder) /Ordering (PKUE1) /Supplement 0 >> /DW 1000 /FontDescriptor 6 0 R >>")
	}
	return assemblePDF(objects)
}

func assertPDFFontStorageOptions(t *testing.T, data []byte, subtype string, cid bool) {
	t.Helper()
	input := fontStoragePDF(data, subtype, cid)
	expected, err := fontrepair.Repair(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, repaired := range []bool{false, true} {
		t.Run(fmt.Sprintf("修复后存储=%t", repaired), func(t *testing.T) {
			var output bytes.Buffer
			var err error
			if repaired {
				err = ConvertWithOptions(t.Context(), input, &output, "", Options{SaveRepairedFonts: true})
			} else {
				err = Convert(t.Context(), input, &output, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			ofd, err := parser.NewOFD(output.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			defer ofd.Close()
			count := 0
			ofd.Documents[0].ForEachFont(func(_ models.StID, font *models.Font) bool {
				if font.FontFile == "" {
					return true
				}
				stored, err := ofd.Documents[0].FileCache.Read(string(font.FontFile))
				if err != nil {
					t.Fatal(err)
				}
				want := data
				if repaired {
					want = expected
				}
				if !bytes.Equal(stored, want) {
					t.Fatal("OFD 包内字体不符合选定的存储策略")
				}
				again, err := fontrepair.Repair(stored)
				if err != nil || !bytes.Equal(expected, again) {
					t.Fatalf("存储的字体无法还原同一修复结果：%v", err)
				}
				parsed, err := fontparser.ParseSFNT(again, 0)
				if err != nil || parsed == nil || parsed.CFF == nil {
					t.Fatalf("修复后的字体无法解析：%v", err)
				}
				count++
				return true
			})
			if count != 1 {
				t.Fatalf("输出内嵌字体数=%d，期望1", count)
			}
		})
	}
}

func TestPDFImportCFFFontStorageOptions(t *testing.T) {
	assertPDFFontStorageOptions(t, legacyPrivateCFF(), "Type1C", false)
}

func TestPDFFontStoragePreservesRenderingAndText(t *testing.T) {
	for name, input := range map[string][]byte{
		"CFF":      fontStoragePDF(legacyPrivateCFF(), "Type1C", false),
		"TrueType": embeddedTextPDF("BT /F1 12 Tf 20 100 Td (A) Tj ET"),
	} {
		t.Run(name, func(t *testing.T) {
			var previous *image.RGBA
			var previousText string
			for _, repaired := range []bool{false, true} {
				var output bytes.Buffer
				if err := ConvertWithOptions(t.Context(), input, &output, "", Options{SaveRepairedFonts: repaired}); err != nil {
					t.Fatal(err)
				}
				doc, err := parser.NewOFD(output.Bytes())
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = doc.Close() })
				page := doc.Documents[0].Pages[0]
				got := textFidelityRaster(t, doc, page)
				if previous != nil && !bytes.Equal(previous.Pix, got.Pix) {
					t.Fatal("字体存储选项改变了 OFD 光栅结果")
				}
				previous = got
				pdf := textFidelityPDFOutput(t, doc, page)
				text := strings.TrimSpace(textFidelityExtractPDF(t, pdf))
				if text != "A" || previousText != "" && text != previousText {
					t.Fatalf("往返 PDF 文字错误: %q", text)
				}
				previousText = text
			}
		})
	}
}

func TestIntroPDFImportOpenTypeAndCIDCFFStorageOptions(t *testing.T) {
	z, err := zip.OpenReader("../../testdata/ofdrw/intro.ofd")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skip("缺少可选真实字体样例: ../../testdata/ofdrw/intro.ofd")
		}
		t.Fatal(err)
	}
	defer z.Close()
	for _, file := range z.File {
		// intro.ofd 的这个 .ttf 条目实际保存裸 CID CFF，不能按扩展名判断格式。
		if file.Name != "Doc_0/Res/font_85_85.ttf" {
			continue
		}
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		cff, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		otf, err := fontrepair.Repair(cff)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := fontparser.ParseSFNT(otf, 0)
		if err != nil || parsed == nil || parsed.CFF == nil {
			t.Fatalf("intro.ofd 内嵌字体无法包装为 OpenType CFF: %v", err)
		}
		if _, _, _, cid := parsed.CFF.CIDSystemInfo(); !cid {
			t.Fatal("intro.ofd 的目标字体不是 CID CFF")
		}
		t.Run("OpenType", func(t *testing.T) { assertPDFFontStorageOptions(t, otf, "OpenType", true) })
		t.Run("CIDFontType0C", func(t *testing.T) { assertPDFFontStorageOptions(t, cff, "CIDFontType0C", true) })
		return
	}
	t.Fatal("intro.ofd 中缺少 Doc_0/Res/font_85_85.ttf")
}
