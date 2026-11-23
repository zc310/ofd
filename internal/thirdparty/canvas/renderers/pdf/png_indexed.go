package pdf

import (
	"bytes"
	"compress/zlib"
	"image"
	"io"

	cimage "github.com/zc310/ofd/internal/thirdparty/canvas/image"
)

// embedPNGIndexed 按 ISO 32000-1 §8.6.6.3、§7.4.4.4 原样复用 PNG 索引压缩流，
// 调色板使用二进制流，避免 PDF 字面字符串对 CR/LF 的规范化改变颜色值。
// tRNS 的透明度按实际索引生成无损 SMask，不把整幅图片展开成 RGB/RGBA。
func (w *pdfPageWriter) embedPNGIndexed(img *cimage.Image) (pdfRef, bool) {
	size := img.Bounds().Size()
	info, ok := pngRasterData(img.Bytes, size, 3)
	if !ok {
		return 0, false
	}
	alpha, ok := pngIndexedAlpha(info, size)
	if !ok {
		return 0, false
	}
	color := pngPredictorStream(info.compressed, size, 1)
	color.dict["Filter"] = pdfArray{pdfFilterStoredFlate}
	color.dict["BitsPerComponent"] = info.bits
	color.dict["DecodeParms"] = pdfArray{pdfDict{
		"Predictor": 15, "Colors": 1, "BitsPerComponent": info.bits, "Columns": size.X,
	}}
	color.dict["ColorSpace"] = pdfArray{
		pdfName("Indexed"), pdfName("DeviceRGB"), len(info.palette)/3 - 1,
		w.pdf.writeObject(pdfStream{stream: info.palette}),
	}
	if alpha != nil {
		color.dict["SMask"] = w.pdf.writeObject(pngPredictorStream(alpha, size, 1))
	}
	return w.pdf.writeObject(color), true
}

// pngIndexedAlpha 逐行恢复打包索引以检查数据并提取透明度，不解码 RGB 像素。
// 即使没有 tRNS，也检查全部索引、扫描行长度及 zlib 校验，避免快速嵌入接受损坏数据。
func pngIndexedAlpha(info pngRasterInfo, size image.Point) ([]byte, bool) {
	const maxBytes = 64 << 20
	if size.X <= 0 || size.Y <= 0 || size.X >= maxBytes || size.Y > maxBytes/(size.X+1) {
		return nil, false
	}
	reader, err := zlib.NewReader(bytes.NewReader(info.compressed))
	if err != nil {
		return nil, false
	}
	defer reader.Close()

	var alpha, previousAlpha []byte
	for _, a := range info.transparency {
		if a != 255 {
			alpha = make([]byte, (size.X+1)*size.Y)
			previousAlpha = make([]byte, size.X)
			break
		}
	}
	stride := (size.X*info.bits + 7) / 8
	row, previous := make([]byte, stride+1), make([]byte, stride+1)
	count, sampleMask := len(info.palette)/3, byte((1<<info.bits)-1)
	hasAlpha := false
	for y := range size.Y {
		if _, err := io.ReadFull(reader, row); err != nil || !pngUnfilterIndexRow(row, previous[1:]) {
			return nil, false
		}
		var alphaRow []byte
		if alpha != nil {
			alphaRow = alpha[y*(size.X+1) : (y+1)*(size.X+1)]
			alphaRow[0] = 2 // Up 滤波保留相邻行相同透明度区域的压缩率。
		}
		for x := range size.X {
			bit := x * info.bits
			index := int(row[1+bit/8] >> (8 - info.bits - bit%8) & sampleMask)
			if index >= count {
				return nil, false
			}
			if alpha != nil {
				a := byte(255)
				if index < len(info.transparency) {
					a = info.transparency[index]
				}
				alphaRow[1+x] = a - previousAlpha[x]
				previousAlpha[x] = a
				hasAlpha = hasAlpha || a != 255
			}
		}
		row, previous = previous, row
	}
	var extra [1]byte
	if n, err := reader.Read(extra[:]); n != 0 || err != io.EOF {
		return nil, false
	}
	if !hasAlpha {
		alpha = nil
	}
	return alpha, true
}

// pngUnfilterIndexRow 恢复一条索引扫描行；打包索引的滤波字节步长始终为 1。
func pngUnfilterIndexRow(row, previous []byte) bool {
	filter := row[0]
	if filter > 4 {
		return false
	}
	for i := 1; i < len(row); i++ {
		left, upperLeft := byte(0), byte(0)
		if i > 1 {
			left, upperLeft = row[i-1], previous[i-2]
		}
		up := previous[i-1]
		switch filter {
		case 1:
			row[i] += left
		case 2:
			row[i] += up
		case 3:
			row[i] += byte((int(left) + int(up)) / 2)
		case 4:
			row[i] += pngPaeth(left, up, upperLeft)
		}
	}
	return true
}

func pngPaeth(left, up, upperLeft byte) byte {
	p := int(left) + int(up) - int(upperLeft)
	dl := max(p-int(left), int(left)-p)
	du := max(p-int(up), int(up)-p)
	dul := max(p-int(upperLeft), int(upperLeft)-p)
	if dl <= du && dl <= dul {
		return left
	}
	if du <= dul {
		return up
	}
	return upperLeft
}
