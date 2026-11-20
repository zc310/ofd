package converter_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hhrutter/tiff"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/converter"
	_ "github.com/zc310/ofd/pkg/converter/import/zip"
	"golang.org/x/image/bmp"
)

type zipImageEntry struct {
	name   string
	widths []int
}

func zipImageInput(t *testing.T, entries []zipImageEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for index, entry := range entries {
		// 时间和归档顺序均不作为页序依据。
		header := zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetModTime(time.Date(2026, 1, index+1, 0, 0, 0, 0, time.UTC))
		part, err := writer.CreateHeader(&header)
		if err != nil {
			t.Fatal(err)
		}
		if len(entry.widths) == 0 {
			continue
		}
		if filepath.Ext(entry.name) == ".tiff" {
			pages := make([]image.Image, len(entry.widths))
			for i, width := range entry.widths {
				pages[i] = image.NewRGBA(image.Rect(0, 0, width, 40))
			}
			if err := tiff.EncodeAll(part, pages, &tiff.Options{}); err != nil {
				t.Fatal(err)
			}
		} else if err := png.Encode(part, image.NewRGBA(image.Rect(0, 0, entry.widths[0], 40))); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

type zipWidthOCREngine struct {
	widths []int
}

func (e *zipWidthOCREngine) Recognize(_ context.Context, img image.Image) ([]converter.OCRTextBlock, error) {
	width := img.Bounds().Dx()
	e.widths = append(e.widths, width)
	return []converter.OCRTextBlock{{
		Text: fmt.Sprintf("page%d", width), Bounds: image.Rect(1, 1, width-1, 20), Confidence: 99,
	}}, nil
}

func TestZIPImportSortsFullPathsAndExpandsTIFFWithoutAuxiliaryPages(t *testing.T) {
	entries := []zipImageEntry{
		{"10.png", []int{110}}, {"2.tiff", []int{102, 103}}, {"1.PNG", []int{101}},
		{"dir10/1.png", []int{210}}, {"dir2/10.png", []int{202}}, {"dir2/2.png", []int{201}},
		{"__MACOSX/1.png", nil}, {".hidden/1.png", nil}, {"dir2/.1.png", nil},
		{"readme.txt", nil}, {"dir2/", nil},
	}
	want := []int{101, 102, 103, 110, 201, 202, 210}
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("反转归档顺序=%t", reverse), func(t *testing.T) {
			ordered := slices.Clone(entries)
			if reverse {
				slices.Reverse(ordered)
			}
			data := zipImageInput(t, ordered)
			engine := &zipWidthOCREngine{}
			var output bytes.Buffer
			err := converter.Convert(t.Context(), "zip", "ofd", data, &output,
				converter.ImageOCR(true), converter.WithImageOCREngine(engine))
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(engine.widths, want) {
				t.Fatalf("OCR 页序 = %v，期望 %v", engine.widths, want)
			}
			doc, err := parser.NewOFD(output.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			defer doc.Close()
			pages := doc.Documents[0].Pages
			if len(pages) != len(want) {
				t.Fatalf("页数 = %d，期望 %d", len(pages), len(want))
			}
			for i, page := range pages {
				box, err := page.PhysicalBox()
				if err != nil {
					t.Fatal(err)
				}
				if math.Abs(box.Width-float64(want[i])/300*25.4) > 0.001 {
					t.Fatalf("第 %d 页宽度 %f 与预期页序不符", i+1, box.Width)
				}
			}
			texts := csvPageTexts(t, output.Bytes())
			for i, text := range texts {
				if text != fmt.Sprintf("page%d\n", want[i]) {
					t.Fatalf("第 %d 页文字 = %q", i+1, text)
				}
			}
		})
	}
}

func TestZIPImportDetectsFilenameAndDefaultsOCRToOff(t *testing.T) {
	input := zipImageInput(t, []zipImageEntry{{"1.png", []int{40}}})
	path := filepath.Join(t.TempDir(), "scans.ZIP")
	if err := os.WriteFile(path, input, 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := converter.Convert(t.Context(), "", "ofd", path, &output); err != nil {
		t.Fatal(err)
	}
	doc, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer doc.Close()
	if got := len(doc.Documents[0].Pages[0].Content().Layer[0].Items); got != 1 {
		t.Fatalf("默认 OCR 应关闭，图元数 = %d", got)
	}
	if importer, ok := converter.ImporterByName("zip"); !ok || importer.MIME() != "application/zip" {
		t.Fatal("ZIP 导入器未正确注册")
	}
}

func TestZIPImportComposesPDFAndPNGOutputs(t *testing.T) {
	input := zipImageInput(t, []zipImageEntry{{"2.png", []int{60}}, {"1.png", []int{40}}})
	var pdf bytes.Buffer
	if err := converter.Convert(t.Context(), "zip", "pdf", input, &pdf); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf.Bytes(), []byte("%PDF-")) {
		t.Fatal("ZIP 经 OFD 导出的 PDF 无效")
	}
	var pages []int
	var pngBuffers []*bytes.Buffer
	err := converter.Convert(t.Context(), "zip", "png", input, nil,
		converter.DPI(300), converter.Writer(func(page int) (io.WriteCloser, error) {
			pages = append(pages, page)
			buffer := &bytes.Buffer{}
			pngBuffers = append(pngBuffers, buffer)
			return exampleBufferWriteCloser{buffer}, nil
		}))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(pages, []int{1, 2}) {
		t.Fatalf("PNG 输出页码 = %v", pages)
	}
	for index, buffer := range pngBuffers {
		img, err := png.Decode(bytes.NewReader(buffer.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		want := []int{40, 60}[index]
		if delta := img.Bounds().Dx() - want; delta < -1 || delta > 1 {
			t.Fatalf("PNG 第 %d 页宽度 = %d，期望约 %d", index+1, img.Bounds().Dx(), want)
		}
	}
}

func zipRawImageInput(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	part, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestZIPAndImageImportersDoNotFallbackToEachOther(t *testing.T) {
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 40, 30))); err != nil {
		t.Fatal(err)
	}
	archive := zipRawImageInput(t, "1.png", picture.Bytes())
	for _, tc := range []struct {
		name string
		from string
		data []byte
	}{
		{"ZIP 入口拒绝普通 PNG", "zip", picture.Bytes()},
		{"PNG 入口拒绝 ZIP", "png", archive},
		{"JPEG 入口拒绝 ZIP", "jpeg", archive},
		{"TIFF 入口拒绝 ZIP", "tiff", archive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := converter.Convert(t.Context(), tc.from, "ofd", tc.data, &output); err == nil || output.Len() != 0 {
				t.Fatalf("格式不符时错误 = %v，输出 %d 字节", err, output.Len())
			}
		})
	}
}

func TestZIPRejectsGIFAndBMPDisguisedAsSupportedImages(t *testing.T) {
	var animated, bitmap bytes.Buffer
	frame := image.NewPaletted(image.Rect(0, 0, 40, 30), color.Palette{color.Black, color.White})
	if err := gif.EncodeAll(&animated, &gif.GIF{
		Image: []*image.Paletted{frame, frame}, Delay: []int{1, 1},
	}); err != nil {
		t.Fatal(err)
	}
	if err := bmp.Encode(&bitmap, frame); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		format string
		data   []byte
	}{
		{"动画 GIF", "gif", animated.Bytes()},
		{"BMP", "bmp", bitmap.Bytes()},
	} {
		for _, name := range []string{"1.png", "1.jpg", "1.tiff"} {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				var output bytes.Buffer
				err := converter.Convert(t.Context(), "zip", "ofd", zipRawImageInput(t, name, tc.data), &output)
				if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), tc.format) || output.Len() != 0 {
					t.Fatalf("伪装图片未被拒绝: %v，输出 %d 字节", err, output.Len())
				}
			})
		}
	}
}

func ExampleConvert_zipImages() {
	var pngData, input, output bytes.Buffer
	_ = png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 40, 30)))
	writer := zip.NewWriter(&input)
	for _, name := range []string{"10.png", "2.png", "1.png"} {
		part, _ := writer.Create(name)
		_, _ = part.Write(pngData.Bytes())
	}
	_ = writer.Close()
	// 需空白导入 pkg/converter/import/zip；页面顺序为 1、2、10。
	err := converter.Convert(context.Background(), "zip", "ofd", input.Bytes(), &output)
	fmt.Println(err == nil && bytes.HasPrefix(output.Bytes(), []byte("PK")))
	// Output: true
}
