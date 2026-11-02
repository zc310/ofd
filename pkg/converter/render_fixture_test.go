package converter

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func requireRenderFixture(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("缺少样例 %s", path)
		}
		t.Fatal(err)
	}
}

// 文字渐变必须写出完整着色图案，不能生成只有 /ColorSpace 的空字典。
func TestRender_PDF_gradientTextHasNoEmptyShading(t *testing.T) {
	const input = "../../testdata/ofd-creator/shading.ofd"
	requireRenderFixture(t, input)
	var output bytes.Buffer
	if err := PDF(t.Context(), input, &output); err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?s)/PatternType\s*2\s*/Shading\s*<<(.*?)>>`)
	for _, match := range re.FindAllStringSubmatch(output.String(), -1) {
		shading := match[1]
		if !strings.Contains(shading, "/ShadingType") && !strings.Contains(shading, "/Function") {
			t.Fatalf("空的着色图案 /Shading: %q", shading)
		}
	}
	// 文字渐变的 Extend 位要写成 PDF 的 /Extend 数组（ofd-creator/shading.ofd 的渐变文字为 Extend=0）。
	if !strings.Contains(output.String(), "/Extend[false false]") {
		t.Fatal("文字渐变缺少 /Extend[false false]")
	}
}

// 渐变文字必须保留真实文字 + 原生着色图案（可复制），而不是栅格化成图片。
func TestRender_PDF_gradientTextKeepsText(t *testing.T) {
	const input = "../../testdata/ofdrw/intro.ofd"
	requireRenderFixture(t, input)
	var output bytes.Buffer
	if err := PDF(t.Context(), input, &output); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, stream := range inflatePDFStreams(output.Bytes()) {
		// canvas 把着色图案文字写成 “/Pattern cs /P? scn ... [..]TJ”。
		if regexp.MustCompile(`/Pattern cs\s*/P\d+ scn[^\n]*T[jJ]`).Match(stream) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("未找到用着色图案绘制的文字：渐变文字可能被栅格化，PDF 中不可复制")
	}
}

// inflatePDFStreams 返回 PDF 中所有可 inflate 的内容流（字典流未压缩，无需处理）。
func inflatePDFStreams(data []byte) [][]byte {
	var out [][]byte
	search := 0
	for {
		index := bytes.Index(data[search:], []byte("stream\n"))
		if index < 0 {
			break
		}
		index += search
		// "endstream\n" 里也含 "stream\n"，跳过它以免把对象头当成流起点。
		if index >= 3 && string(data[index-3:index]) == "end" {
			search = index + len("stream\n")
			continue
		}
		start := index + len("stream\n")
		end := bytes.Index(data[start:], []byte("endstream"))
		if end < 0 {
			break
		}
		raw := data[start : start+end]
		search = start + end + len("endstream")
		reader, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			continue
		}
		if decoded, err := io.ReadAll(reader); err == nil {
			out = append(out, decoded)
		}
		_ = reader.Close()
	}
	return out
}

func TestRender_PDF_huawei(t *testing.T) {
	const input = "../../testdata/ofd-creator/huawei.ofd"
	requireRenderFixture(t, input)
	var output bytes.Buffer
	if err := PDF(t.Context(), input, &output); err != nil {
		t.Fatal(err)
	}
	assert.Contains(t, output.String(), "/ShadingType 2")
}

func TestRender_SVG_intro_page15KeepsSimpleCompositesVector(t *testing.T) {
	const input = "../../testdata/ofdrw/intro.ofd"
	requireRenderFixture(t, input)
	var output bytes.Buffer
	err := Image(t.Context(), input,
		Writer(func(int) (io.WriteCloser, error) {
			return nopWriteCloser{&output}, nil
		}),
		SVG(),
		Page(15),
	)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, 2, strings.Count(output.String(), "<image "))
	assert.Contains(t, output.String(), `fill-opacity=".4"`)
}
