package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"golang.org/x/image/font/gofont/goregular"
)

func TestPDFFontStorageFlagReachesOutput(t *testing.T) {
	// 模拟 PDF 子集缺少 name 表，确保原始字节与修复结果确实不同。
	raw := bytes.Clone(goregular.TTF)
	for i := 0; i < int(binary.BigEndian.Uint16(raw[4:6])); i++ {
		entry := raw[12+16*i:]
		if string(entry[:4]) == "name" {
			copy(entry[:4], "zzzz")
		}
	}
	repaired, err := fontrepair.Repair(raw)
	if err != nil || bytes.Equal(raw, repaired) {
		t.Fatalf("测试字体未触发修复: %v", err)
	}
	content := "BT /F1 12 Tf 20 100 Td (AB) Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /TrueType /BaseFont /GoRegular /Encoding /WinAnsiEncoding /FirstChar 65 /LastChar 66 /Widths [600 600] /FontDescriptor 6 0 R >>",
		"<< /Type /FontDescriptor /FontName /GoRegular /Flags 32 /Ascent 1000 /Descent -200 /FontBBox [0 -200 1100 1000] /CapHeight 800 /ItalicAngle 0 /StemV 80 /FontFile2 7 0 R >>",
		fmt.Sprintf("<< /Length %d /Length1 %d >>\nstream\n%s\nendstream", len(raw), len(raw), raw),
	}
	var input bytes.Buffer
	input.WriteString("%PDF-1.4\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = input.Len()
		fmt.Fprintf(&input, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := input.Len()
	fmt.Fprintf(&input, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&input, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&input, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.pdf")
	if err := os.WriteFile(inputPath, input.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want []byte
	}{
		{"默认原始", nil, raw},
		{"修复字体", []string{"--pdf-save-repaired-fonts"}, repaired},
		{"历史参数格式", []string{"-pdf-save-repaired-fonts"}, repaired},
		{"显式关闭", []string{"--pdf-save-repaired-fonts=false"}, raw},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outputPath := filepath.Join(dir, tc.name+".ofd")
			opts, err := parseArgs(append(append([]string{}, tc.args...), inputPath, outputPath))
			if err != nil {
				t.Fatal(err)
			}
			if got := opts.pdfImportOptions().SaveRepairedFonts; got != opts.pdfSaveRepairedFonts {
				t.Fatalf("字体选项未传递: %t", got)
			}
			if err := run(opts); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := parser.NewOFD(data)
			if err != nil {
				t.Fatal(err)
			}
			defer doc.Close()
			count := 0
			doc.Documents[0].ForEachFont(func(_ models.StID, font *models.Font) bool {
				if font.FontFile != "" {
					stored, err := doc.Documents[0].FileCache.Read(string(font.FontFile))
					if err != nil || !bytes.Equal(stored, tc.want) {
						t.Fatalf("字体保存策略不符合选项: %v", err)
					}
					count++
				}
				return true
			})
			if count != 1 {
				t.Fatalf("内嵌字体数量 = %d，期望 1", count)
			}
		})
	}
}
