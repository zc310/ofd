package converter_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/converter"
	_ "github.com/zc310/ofd/pkg/converter/import/csv"
	"github.com/zc310/ofd/pkg/validator"
	"github.com/zc310/ofd/pkg/webreader"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func csvPageTexts(t *testing.T, data []byte) []string {
	t.Helper()
	r, err := webreader.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	pages := make([]string, r.PageCount())
	for index := range pages {
		runs, err := r.Text(index)
		if err != nil {
			t.Fatal(err)
		}
		for _, run := range runs {
			pages[index] += run.Text + "\n"
		}
	}
	return pages
}

// 所有数据只出现一次，表头在每页重复；原始字符串与带引号的换行不丢失。
func TestCSVImportPaginatesWithRepeatedHeadersAndPreservesValues(t *testing.T) {
	var source strings.Builder
	source.WriteString("\ufeffIdentifier,Description,Amount,Empty\r\n")
	for index := range 120 {
		fmt.Fprintf(&source, "%06d,\"part, \"\"quoted\"\"\r\nnext\",12.30,\r\n", index)
	}
	var output bytes.Buffer
	if err := converter.Convert(t.Context(), "csv", "ofd", []byte(source.String()), &output); err != nil {
		t.Fatal(err)
	}
	pages := csvPageTexts(t, output.Bytes())
	if len(pages) < 2 {
		t.Fatal("长表格没有分页")
	}
	for _, page := range pages {
		if strings.Count(page, "Identifier\n") != 1 || strings.Count(page, "Amount\n") != 1 {
			t.Fatalf("表头没有正确重复: %q", page)
		}
	}
	all := strings.Join(pages, "\n")
	for index := range 120 {
		if strings.Count(all, fmt.Sprintf("%06d\n", index)) != 1 {
			t.Fatalf("数据行 %d 缺失或重复", index)
		}
	}
	for _, value := range []string{"part, \"quoted\"", "next", "12.30"} {
		if strings.Count(all, value) != 120 {
			t.Fatalf("单元格内容 %q 丢失或改变", value)
		}
	}
	v, err := validator.New(validator.WithMode(validator.ModeStrict))
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(t.Context(), bytes.NewReader(output.Bytes()), "table.ofd")
	if report.Summary.Errors != 0 || report.Summary.Warnings != 0 {
		t.Fatalf("严格校验未通过: %+v", report)
	}
}

func TestTSVImportDetectsPathAndAppliesNoHeaderPaperAndTitle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.TSV")
	if err := os.WriteFile(path, []byte("0001\tFirst\n0002\tSecond\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := converter.Convert(t.Context(), "", "ofd", path, &output,
		converter.WithCSVImportOptions(converter.CSVImportOptions{NoHeader: true, Title: "Inventory", FontData: goregular.TTF}),
		converter.WithPaperSize("A3"), converter.WithLandscape(true))
	if err != nil {
		t.Fatal(err)
	}
	r, err := webreader.Open(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	info, err := r.Pages()
	if err != nil {
		t.Fatal(err)
	}
	if len(info) != 1 || info[0].Width != 420 || info[0].Height != 297 {
		t.Fatalf("纸张设置没有传递: %+v", info)
	}
	text := strings.Join(csvPageTexts(t, output.Bytes()), "\n")
	for _, value := range []string{"Inventory", "0001", "First", "0002", "Second"} {
		if !strings.Contains(text, value) {
			t.Fatalf("无表头模式丢失 %q: %q", value, text)
		}
	}
}

func TestCSVImportAcceptsReaderAndComposesPDF(t *testing.T) {
	for _, from := range []string{"csv", ".csv", "tsv", ".tsv"} {
		var output bytes.Buffer
		if err := converter.Convert(t.Context(), from, "pdf", strings.NewReader("Code\n0001\n"), &output); err != nil {
			t.Fatalf("%s→PDF: %v", from, err)
		}
		if !bytes.HasPrefix(output.Bytes(), []byte("%PDF-")) {
			t.Fatal("输出不是 PDF")
		}
	}
}

func TestCSVImportSplitsLongCellsAndAcceptsHeaderOnly(t *testing.T) {
	var source strings.Builder
	source.WriteString("Note\n\"")
	for index := range 140 {
		fmt.Fprintf(&source, "Line%03d\n", index)
	}
	source.WriteString("\"\n")
	var output bytes.Buffer
	if err := converter.Convert(t.Context(), "csv", "ofd", []byte(source.String()), &output); err != nil {
		t.Fatal(err)
	}
	pages := csvPageTexts(t, output.Bytes())
	if len(pages) < 2 {
		t.Fatal("超长单元格没有跨页续排")
	}
	for _, page := range pages {
		if strings.Count(page, "Note\n") != 1 {
			t.Fatal("单元格续页没有重复表头")
		}
	}
	all := strings.Join(pages, "\n")
	for index := range 140 {
		if strings.Count(all, fmt.Sprintf("Line%03d\n", index)) != 1 {
			t.Fatalf("长单元格中的行 %d 缺失或重复", index)
		}
	}
	output.Reset()
	if err := converter.Convert(t.Context(), "csv", "ofd", []byte("Code,Name\n"), &output); err != nil {
		t.Fatalf("仅含表头的表格不应失败: %v", err)
	}
}

func TestCSVImportRejectsBadInputBeforeWriting(t *testing.T) {
	for _, tc := range []struct {
		input any
		opts  converter.CSVImportOptions
	}{
		{[]byte("a,b\n1\n"), converter.CSVImportOptions{}},
		{[]byte(""), converter.CSVImportOptions{}},
		{[]byte{0xFF, 0xFE}, converter.CSVImportOptions{}},
		{[]byte("a\n1\n"), converter.CSVImportOptions{Encoding: "unknown"}},
		{[]byte("a\n1\n"), converter.CSVImportOptions{FontFile: "missing.ttf"}},
		{[]byte("a\n1\n"), converter.CSVImportOptions{FontData: []byte("broken font")}},
		{42, converter.CSVImportOptions{}},
	} {
		var output bytes.Buffer
		err := converter.Convert(t.Context(), "csv", "ofd", tc.input, &output, converter.WithCSVImportOptions(tc.opts))
		if err == nil || output.Len() != 0 {
			t.Fatalf("无效输入写出了 OFD: error=%v bytes=%d", err, output.Len())
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	reader := &csvCancelReader{cancel: cancel}
	var output bytes.Buffer
	if err := converter.Convert(ctx, "csv", "ofd", reader, &output); err != context.Canceled || output.Len() != 0 {
		t.Fatalf("取消错误或输出错误: %v bytes=%d", err, output.Len())
	}
}

type csvCancelReader struct{ cancel context.CancelFunc }

func (r *csvCancelReader) Read(p []byte) (int, error) {
	r.cancel()
	return copy(p, "a\n1\n"), io.EOF
}

// 使用已跟踪 OFD 中的真实中文字体，验证解码、度量及字体嵌入，无需系统字体。
func TestCSVImportEmbedsChineseFontAndDecodesGB18030(t *testing.T) {
	archive, err := zip.OpenReader("../../testdata/ofdrw/intro.ofd")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("缺少 testdata/ofdrw/intro.ofd")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	var fontData []byte
	var character rune
	for _, file := range archive.File {
		if !strings.HasSuffix(strings.ToLower(file.Name), ".ttf") {
			continue
		}
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		data, err = fontrepair.Repair(data)
		if err != nil {
			continue
		}
		face, err := sfnt.Parse(data)
		if err != nil {
			continue
		}
		var buffer sfnt.Buffer
		for ch := rune(0x4e00); ch <= 0x9fff; ch++ {
			if glyph, err := face.GlyphIndex(&buffer, ch); err == nil && glyph != 0 {
				fontData, character = data, ch
				break
			}
		}
		if fontData != nil {
			break
		}
	}
	if fontData == nil {
		t.Fatal("intro.ofd 没有可用的中文字体")
	}
	// GB18030 的任意中文码点都可编码，包括不在 GBK 中的码点。
	input := string(character) + ",Code\n" + string(character) + ",0001\n"
	encoded, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := converter.Convert(t.Context(), "csv", "ofd", encoded, &output, converter.WithCSVImportOptions(converter.CSVImportOptions{Encoding: "gb18030", FontData: fontData})); err != nil {
		t.Fatal(err)
	}
	if text := strings.Join(csvPageTexts(t, output.Bytes()), "\n"); strings.Count(text, string(character)) != 2 {
		t.Fatalf("中文单元格丢失: %q", text)
	}
	doc, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	for _, font := range doc.Documents[0].Fonts() {
		if font.FontFile == "" {
			t.Fatal("字体没有嵌入输出")
		}
	}
}
