package test

import (
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
			return os.Create(filepath.Join(tmpDir, fmt.Sprintf("intro_%d.png", page)))
		}),
		converter.BgColor(color.White),
		converter.JPG(),
		converter.Page(3),
		converter.DPI(300),
	))
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
