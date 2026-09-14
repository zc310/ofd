package test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nao1215/imaging"
	"github.com/stretchr/testify/assert"

	"github.com/zc310/ofd/pkg/converter"
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
	assert.Nil(t, converter.PDF("testdata/helloworld.ofd", f))
}
func TestRender_PDF_999(t *testing.T) {
	f, err := os.Create(filepath.Join(tmpDir, "999.pdf"))
	assert.Nil(t, err)
	defer f.Close()
	assert.Nil(t, converter.PDF("testdata/999.ofd", f))
}
func TestRender_PDF_ano(t *testing.T) {
	f, err := os.Create(filepath.Join(tmpDir, "ano.pdf"))
	assert.Nil(t, err)
	defer f.Close()
	assert.Nil(t, converter.PDF("testdata/ano.ofd", f))
}
func BenchmarkRenderPDFIntro(b *testing.B) {
	var output bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		output.Reset()
		if err := converter.PDF("testdata/intro.ofd", &output); err != nil {
			b.Fatal(err)
		}
	}
}

func TestRender_PDF_huawei(t *testing.T) {
	var output bytes.Buffer
	assert.Nil(t, converter.PDF("testdata/huawei.ofd", &output))
	assert.Contains(t, output.String(), "/ShadingType 2")
}
func TestRender_PDF_intro_page7(t *testing.T) {
	f, err := os.Create(filepath.Join(tmpDir, "intro_page_7.pdf"))
	assert.Nil(t, err)
	defer f.Close()
	assert.Nil(t, converter.PDF("testdata/intro.ofd", f, converter.Page(40)))
}

func TestRender_SVG_intro_page15KeepsSimpleCompositesVector(t *testing.T) {
	var output bytes.Buffer
	err := converter.Image("testdata/intro.ofd",
		converter.Writer(func(int) (io.WriteCloser, error) {
			return bufferWriteCloser{Buffer: &output}, nil
		}),
		converter.SVG(),
		converter.Page(15),
	)
	assert.NoError(t, err)
	assert.Equal(t, 2, strings.Count(output.String(), "<image "))
	assert.Contains(t, output.String(), `fill-opacity=".6"`)
}

func TestRender_Image(t *testing.T) {
	assert.Nil(t, converter.Image("testdata/ano.ofd",
		converter.ImageWriter(func(page int, img image.Image) error {
			return imaging.Save(img, filepath.Join(tmpDir, fmt.Sprintf("ano_%d.png", page)))
		}),
		converter.BgColor(color.White),
		converter.PNG(),
	))
}
