//go:build openjpeg && cgo && linux

package pdf2ofd

import (
	"testing"

	"github.com/mrjoshuak/go-jpeg2000"
	"github.com/zc310/ofd/internal/pdf2ofd/config"
)

func TestConvertJPXImageWithOpenJPEGBackend(t *testing.T) {
	jp2 := encodeJPX(t, jpxTestSource(), jpeg2000.FormatJP2)
	assertJPXGrayscale(t, convertJPXImageWithOptions(t, jp2, config.Options{JPXBackend: config.JPXBackendOpenJPEG}))
}
