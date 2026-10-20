package pdf2ofd

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
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

func assertPDFStoresRepairedFont(t *testing.T, data []byte, subtype string, cid bool) {
	t.Helper()
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
	expected, err := fontrepair.Repair(data)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Convert(t.Context(), assemblePDF(objects), &output, ""); err != nil {
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
		// 从 ZIP 直接读出的字节必须已修复，不经过 render.LoadFont。
		if !bytes.Equal(stored, expected) {
			t.Fatal("OFD 包内没有保存转换阶段修复后的字体字节")
		}
		again, err := fontrepair.Repair(stored)
		if err != nil || !bytes.Equal(stored, again) {
			t.Fatalf("包内字体仍需二次修复：%v", err)
		}
		parsed, err := fontparser.ParseSFNT(stored, 0)
		if err != nil || parsed == nil || parsed.CFF == nil {
			t.Fatalf("包内OTF字体无法直接解析：%v", err)
		}
		count++
		return true
	})
	if count != 1 {
		t.Fatalf("输出内嵌字体数=%d，期望1", count)
	}
}

func TestPDFImportStoresRepairedCFFInOFD(t *testing.T) {
	assertPDFStoresRepairedFont(t, legacyPrivateCFF(), "Type1C", false)
}

func TestGB38540PDFImportStoresRepairedOpenTypeAndCIDCFF(t *testing.T) {
	z, err := zip.OpenReader("../../testdata/pdf2ofd/GBT_38540-2020.ofd")
	if err != nil {
		t.Skipf("可选真实字体样例不可用：%v", err)
	}
	defer z.Close()
	for _, file := range z.File {
		if !strings.Contains(file.Name, "font-943540") {
			continue
		}
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		otf, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		var cff []byte
		for i := 0; i < int(binary.BigEndian.Uint16(otf[4:])); i++ {
			record := otf[12+16*i:]
			if string(record[:4]) == "CFF " {
				offset, size := binary.BigEndian.Uint32(record[8:]), binary.BigEndian.Uint32(record[12:])
				cff = otf[offset : offset+size]
			}
		}
		t.Run("OpenType", func(t *testing.T) { assertPDFStoresRepairedFont(t, otf, "OpenType", true) })
		t.Run("CIDFontType0C", func(t *testing.T) { assertPDFStoresRepairedFont(t, cff, "CIDFontType0C", true) })
		return
	}
	t.Fatal("样例中没有找到目标字体")
}
