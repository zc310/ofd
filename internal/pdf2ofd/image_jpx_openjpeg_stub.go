//go:build !openjpeg || !cgo || !linux

package pdf2ofd

import (
	"errors"

	jpeg2000 "github.com/mrjoshuak/go-jpeg2000"
)

func decodeJPXOpenJPEG([]byte) (*jpeg2000.FloatImage, error) {
	return nil, errors.New("OpenJPEG 后端未启用；Linux 需安装 libopenjp2 开发文件，并使用 CGO_ENABLED=1 和 -tags openjpeg 构建")
}
