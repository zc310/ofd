package image

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/png"
	"io/fs"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hhrutter/tiff"
	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/ocr"
)

type zipTestEntry struct {
	name string
	data []byte
	mode fs.FileMode
}

func zipTestData(t *testing.T, entries ...zipTestEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for i, entry := range entries {
		header := zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetModTime(time.Date(2026, 1, i+1, 0, 0, 0, 0, time.UTC))
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		w, err := writer.CreateHeader(&header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func zipTestPNG(t *testing.T) []byte {
	t.Helper()
	var output bytes.Buffer
	if err := png.Encode(&output, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestNaturalPathSortIsNumericAndDeterministic(t *testing.T) {
	want := []string{
		"01.png", "1.png", "2.png", "10.png",
		"999999999999999999999.png", "1000000000000000000000.png",
		"A.png", "a.png", "dir2/2.png", "dir2/10.png", "dir10/1.png",
	}
	// 数字开头的路径应排在字母之前；超长数字不得依赖整数解析。
	for _, reverse := range []bool{false, true} {
		got := slices.Clone(want)
		if reverse {
			slices.Reverse(got)
		}
		sort.Slice(got, func(i, j int) bool { return naturalPathLess(got[i], got[j]) })
		if !slices.Equal(got, want) {
			t.Fatalf("自然排序 = %q，期望 %q", got, want)
		}
	}
	if !naturalPathLess("001a.png", "1b.png") {
		t.Fatal("等值数字应继续比较后续片段")
	}
}

func TestZIPRejectsUnsafeDuplicateAndNonRegularEntriesBeforeOutput(t *testing.T) {
	pngData := zipTestPNG(t)
	for _, tc := range []struct {
		name    string
		entries []zipTestEntry
		message string
	}{
		{"父路径", []zipTestEntry{{name: "../1.png"}}, "路径不安全"},
		{"绝对路径", []zipTestEntry{{name: "/1.png"}}, "路径不安全"},
		{"反斜杠", []zipTestEntry{{name: `dir\1.png`}}, "路径不安全"},
		{"盘符", []zipTestEntry{{name: "C:/1.png"}}, "冒号"},
		{"非规范路径", []zipTestEntry{{name: "dir/../1.png"}}, "路径不安全"},
		{"空段", []zipTestEntry{{name: "dir//1.png"}}, "路径不安全"},
		{"隐藏危险路径", []zipTestEntry{{name: "__MACOSX/../../1.png"}}, "路径不安全"},
		{"图片重名", []zipTestEntry{{name: "1.png"}, {name: "1.png"}}, "重复路径"},
		{"忽略文件重名", []zipTestEntry{{name: ".DS_Store"}, {name: ".DS_Store"}}, "重复路径"},
		{"符号链接", []zipTestEntry{{name: "1.png", mode: fs.ModeSymlink | 0o777}}, "不是普通文件"},
		{"空包", nil, "没有可用"},
		{"非图片", []zipTestEntry{{name: "readme.txt"}}, "没有可用"},
		{"隐藏图片", []zipTestEntry{{name: ".hidden/1.png"}, {name: "__MACOSX/1.png"}}, "没有可用"},
		{"损坏图片", []zipTestEntry{{name: "1.png", data: pngData}, {name: "2.png", data: []byte("bad")}}, `"2.png"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			err := ConvertZIP(t.Context(), zipTestData(t, tc.entries...), &output, WithOCR(false))
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("错误 = %v，期望包含 %q", err, tc.message)
			}
			if output.Len() != 0 {
				t.Fatal("失败时不应写出部分 OFD")
			}
		})
	}
}

func TestZIPEnforcesArchivePageAndPixelBudgets(t *testing.T) {
	pngData := zipTestPNG(t)
	input := zipTestData(t,
		zipTestEntry{name: "1.png", data: pngData},
		zipTestEntry{name: "2.png", data: pngData},
		zipTestEntry{name: "readme.txt", data: []byte("ignored")},
	)
	for _, tc := range []struct {
		name    string
		option  Option
		message string
	}{
		{"输入大小", WithMaxInputBytes(int64(len(input) - 1)), "输入超过"},
		{"条目数含忽略文件", WithMaxArchiveEntries(2), "条目数"},
		{"单条解压", WithMaxEntryBytes(int64(len(pngData) - 1)), "单条"},
		{"累计解压", WithMaxTotalBytes(int64(len(pngData))), "总大小"},
		{"总页数", WithMaxPages(1), "总页数"},
		{"单页像素", WithMaxPagePixels(11), "图片像素"},
		{"累计像素", WithMaxTotalPixels(23), "总像素"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			err := ConvertZIP(t.Context(), input, &output, WithOCR(false), tc.option)
			if err == nil || !strings.Contains(err.Error(), tc.message) || output.Len() != 0 {
				t.Fatalf("错误 = %v，输出 %d 字节", err, output.Len())
			}
		})
	}
}

func TestZIPReadChecksActualDecompressedSize(t *testing.T) {
	data := zipTestData(t, zipTestEntry{name: "1.png", data: bytes.Repeat([]byte("x"), 1024)})
	pkg, err := container.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer pkg.Close()
	for _, tc := range []struct {
		name  string
		entry int64
		total int64
	}{
		{"单条预算", 100, 2000},
		{"累计预算", 2000, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := defaultOptions()
			opts.MaxEntryBytes, opts.MaxTotalBytes = tc.entry, tc.total
			remaining := tc.total
			if _, err := readImageEntry(t.Context(), pkg, pkg.Entries()[0], opts, &remaining); err == nil {
				t.Fatal("实际解压字节超限未被拒绝")
			}
		})
	}
}

func TestZIPChecksTIFFPagesAgainstRemainingDocumentBudget(t *testing.T) {
	var input bytes.Buffer
	if err := tiff.EncodeAll(&input, []image.Image{
		image.NewRGBA(image.Rect(0, 0, 4, 3)),
		image.NewRGBA(image.Rect(0, 0, 4, 3)),
	}, &tiff.Options{}); err != nil {
		t.Fatal(err)
	}
	data := zipTestData(t,
		zipTestEntry{name: "1.png", data: zipTestPNG(t)},
		zipTestEntry{name: "2.tiff", data: input.Bytes()},
	)
	var output bytes.Buffer
	err := ConvertZIP(t.Context(), data, &output, WithOCR(false), WithMaxPages(2))
	if err == nil || !strings.Contains(err.Error(), `"2.tiff"`) || !strings.Contains(err.Error(), "页数超过") {
		t.Fatalf("累计 TIFF 页数超限错误 = %v", err)
	}
	if output.Len() != 0 {
		t.Fatal("页数超限不能产出部分文档")
	}
}

func TestZIPRejectsCRCFailure(t *testing.T) {
	var input bytes.Buffer
	writer := zip.NewWriter(&input)
	payload := zipTestPNG(t)
	part, err := writer.CreateHeader(&zip.FileHeader{Name: "1.png", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data := input.Bytes()
	offset := bytes.Index(data, payload)
	if offset < 0 {
		t.Fatal("未找到 ZIP 原始图片数据")
	}
	data[offset+len(payload)-1] ^= 1
	var output bytes.Buffer
	err = ConvertZIP(t.Context(), data, &output, WithOCR(false))
	if err == nil || !strings.Contains(err.Error(), `"1.png"`) || output.Len() != 0 {
		t.Fatalf("CRC 错误 = %v，输出 %d 字节", err, output.Len())
	}
}

type cancellingZIPEngine struct {
	cancel context.CancelFunc
	calls  int
}

func (e *cancellingZIPEngine) Recognize(context.Context, image.Image) ([]ocr.TextBlock, error) {
	e.calls++
	e.cancel()
	return nil, context.Canceled
}

func TestZIPCancellationStopsBeforeNextImageAndReturnsContextError(t *testing.T) {
	data := zipTestPNG(t)
	input := zipTestData(t, zipTestEntry{name: "1.png", data: data}, zipTestEntry{name: "2.png", data: data})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	engine := &cancellingZIPEngine{cancel: cancel}
	var output bytes.Buffer
	err := ConvertZIP(ctx, input, &output, WithOCREngine(engine))
	if err != context.Canceled || engine.calls != 1 || output.Len() != 0 { //nolint:errorlint // 转换接口要求原样返回 ctx.Err()，不能只判断可识别性。
		t.Fatalf("取消错误 = %v，识别次数 %d，输出 %d 字节", err, engine.calls, output.Len())
	}
}
