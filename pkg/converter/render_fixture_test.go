package converter

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kovidgoyal/imaging"
	"github.com/stretchr/testify/assert"
)

var tmpDir = filepath.Join(os.TempDir(), "ofd_test")

type bufferWriteCloser struct {
	*bytes.Buffer
}

func (bufferWriteCloser) Close() error { return nil }

func init() {
	_ = os.Mkdir(tmpDir, 0777)
}
func TestRender_PDF_helloworld(t *testing.T) {
	f, err := os.Create(filepath.Join(tmpDir, "helloworld.pdf"))
	assert.Nil(t, err)
	defer f.Close()
	assert.Nil(t, PDF(ctxTODO, "../../testdata/helloworld.ofd", f))
}

// 文字渐变必须先栅格化：否则 PDF 后端会把自定义 Gradient 写成只有
// /ColorSpace 的空着色图案，MuPDF/PyMuPDF 会报 "cannot load shading function"。
func TestRender_PDF_gradientTextHasNoEmptyShading(t *testing.T) {
	var output bytes.Buffer
	assert.Nil(t, PDF(ctxTODO, "../../testdata/shading.ofd", &output))
	re := regexp.MustCompile(`(?s)/PatternType\s*2\s*/Shading\s*<<(.*?)>>`)
	for _, match := range re.FindAllStringSubmatch(output.String(), -1) {
		shading := match[1]
		if !strings.Contains(shading, "/ShadingType") && !strings.Contains(shading, "/Function") {
			t.Fatalf("空的着色图案 /Shading: %q", shading)
		}
	}
	// 文字渐变的 Extend 位要写成 PDF 的 /Extend 数组（shading.ofd 的渐变文字为 Extend=0）。
	if !strings.Contains(output.String(), "/Extend[false false]") {
		t.Fatal("文字渐变缺少 /Extend[false false]")
	}
}

// 渐变文字必须保留真实文字 + 原生着色图案（可复制），而不是栅格化成图片。
func TestRender_PDF_gradientTextKeepsText(t *testing.T) {
	var output bytes.Buffer
	assert.Nil(t, PDF(ctxTODO, "../../testdata/intro.ofd", &output))
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

func TestRender_PDF_999(t *testing.T) {
	f, err := os.Create(filepath.Join(tmpDir, "999.pdf"))
	assert.Nil(t, err)
	defer f.Close()
	assert.Nil(t, PDF(ctxTODO, "../../testdata/999.ofd", f))
}
func TestRender_PDF_ano(t *testing.T) {
	f, err := os.Create(filepath.Join(tmpDir, "ano.pdf"))
	assert.Nil(t, err)
	defer f.Close()
	assert.Nil(t, PDF(ctxTODO, "../../testdata/ano.ofd", f))
}
func TestRender_PDF_huawei(t *testing.T) {
	var output bytes.Buffer
	assert.Nil(t, PDF(ctxTODO, "../../testdata/huawei.ofd", &output))
	assert.Contains(t, output.String(), "/ShadingType 2")
}
func TestRender_PDF_intro_page7(t *testing.T) {
	f, err := os.Create(filepath.Join(tmpDir, "intro_page_7.pdf"))
	assert.Nil(t, err)
	defer f.Close()
	assert.Nil(t, PDF(ctxTODO, "../../testdata/intro.ofd", f, Page(40)))
}

func TestRender_SVG_intro_page15KeepsSimpleCompositesVector(t *testing.T) {
	var output bytes.Buffer
	err := Image(ctxTODO, "../../testdata/intro.ofd",
		Writer(func(int) (io.WriteCloser, error) {
			return bufferWriteCloser{Buffer: &output}, nil
		}),
		SVG(),
		Page(15),
	)
	assert.NoError(t, err)
	assert.Equal(t, 2, strings.Count(output.String(), "<image "))
	assert.Contains(t, output.String(), `fill-opacity=".4"`)
}

func TestRender_Image(t *testing.T) {
	assert.Nil(t, Image(ctxTODO, "../../testdata/ano.ofd",
		ImageWriter(func(page int, img image.Image) error {
			return imaging.Save(img, filepath.Join(tmpDir, fmt.Sprintf("ano_%d.png", page)))
		}),
		BgColor(color.White),
		PNG(),
	))
}
