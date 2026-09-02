package test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
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
func TestRender_PDF_intro(t *testing.T) {
	f, err := os.Create(filepath.Join(tmpDir, "intro.pdf"))
	assert.Nil(t, err)
	defer f.Close()
	assert.Nil(t, converter.PDF("testdata/intro.ofd", f))
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
func TestRender_PNG(t *testing.T) {
	assert.Nil(t, converter.Image("testdata/ano.ofd",
		converter.Writer(func(page int) (io.WriteCloser, error) {
			return os.Create(fmt.Sprintf(filepath.Join(tmpDir, "ano_%d.png"), page))
		}),
		converter.BgColor(color.White),
		converter.PNG(),
	))
}
func TestRender_JPG(t *testing.T) {
	assert.Nil(t, converter.Image("testdata/intro.ofd",
		converter.Writer(func(page int) (io.WriteCloser, error) {
			return os.Create(filepath.Join(tmpDir, fmt.Sprintf("intro_%d.jpg", page)))
		}),
		converter.BgColor(color.White),
		converter.JPG(),
		converter.Page(40),
		converter.DPI(300),
	))
}

func TestRender_SVG(t *testing.T) {
	var output bytes.Buffer
	err := converter.Image("testdata/helloworld.ofd",
		converter.Writer(func(int) (io.WriteCloser, error) {
			return bufferWriteCloser{Buffer: &output}, nil
		}),
		converter.SVG(),
		converter.Page(1),
	)
	assert.Nil(t, err)
	assert.Contains(t, output.String(), "<svg")
}

func TestRender_EPS(t *testing.T) {
	var output bytes.Buffer
	err := converter.Image("testdata/helloworld.ofd",
		converter.Writer(func(int) (io.WriteCloser, error) {
			return bufferWriteCloser{Buffer: &output}, nil
		}),
		converter.EPS(),
		converter.Page(1),
	)
	assert.Nil(t, err)
	assert.Contains(t, output.String(), "%!PS-Adobe-3.0 EPSF-3.0")
}

func TestRender_TeX(t *testing.T) {
	var output bytes.Buffer
	err := converter.Image("testdata/helloworld.ofd",
		converter.Writer(func(int) (io.WriteCloser, error) {
			return bufferWriteCloser{Buffer: &output}, nil
		}),
		converter.TeX(),
		converter.Page(1),
	)
	assert.Nil(t, err)
	assert.Contains(t, output.String(), "\\begin{pgfpicture}")
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
