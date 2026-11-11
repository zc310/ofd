// Package preview 提供受限的图片预览、缩略图和独立 OFD 印章光栅化。
package preview

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"image/color"
	"math"

	"github.com/zc310/ofd/pkg/reader"
)

var ErrSealLimit = errors.New("OFD 印章超过预览限制（输入 8 MiB、2048 条目、解压 64 MiB、输出 8 MiB）")

type imageBuffer struct{ bytes.Buffer }

func (b *imageBuffer) Write(data []byte) (int, error) {
	if len(data) > (8<<20)-b.Len() {
		return 0, ErrSealLimit
	}
	return b.Buffer.Write(data)
}

// OFDSeal 将印章子文档第一页光栅化为透明 PNG；不执行链接、动作或解锁嵌套文档。
// edge 只允许缩略图 256 或大图 2048 像素，不代表盖章变换后的页面外观或验签结论。
func OFDSeal(ctx context.Context, data []byte, edge int, fallbackFamilies ...string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) > 8<<20 || (edge != 256 && edge != 2048) {
		return nil, ErrSealLimit
	}
	pkg, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	if len(pkg.File) > 2048 {
		return nil, ErrSealLimit
	}
	var total uint64
	for _, file := range pkg.File {
		if file.UncompressedSize64 > 8<<20 || file.UncompressedSize64 > (64<<20)-total {
			return nil, ErrSealLimit
		}
		total += file.UncompressedSize64
	}
	//nolint:contextcheck // Reader.Open 为同步解析接口；渲染阶段继续传递取消信号。
	doc, err := reader.Open(data)
	if err != nil {
		return nil, err
	}
	defer doc.Close()
	// 与页面内印章一致，内嵌字体仍优先；浏览器把当前注册的回退字体传入子文档。
	for _, family := range fallbackFamilies {
		if err := doc.UseFallbackFont(family); err != nil {
			return nil, err
		}
	}
	page, err := doc.Page(0)
	if err != nil {
		return nil, err
	}
	// 为光栅化的向上取整预留一像素，避免浮点误差突破长边上限。
	dpi := math.Min(600, float64(edge-1)*25.4/math.Max(page.Width, page.Height))
	if math.IsNaN(dpi) || math.IsInf(dpi, 0) || dpi < 1 || page.Width <= 0 || page.Height <= 0 {
		return nil, ErrSealLimit
	}
	var output imageBuffer
	if err := doc.RenderPage(ctx, &output, 0, reader.RenderOptions{DPI: dpi, Format: "png", Background: color.Transparent, PNGCompressionLevel: 1}); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
