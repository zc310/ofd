package pdf

import (
	"bytes"
	"image"
	"io"

	"github.com/klauspost/compress/zlib"
	cimage "github.com/zc310/ofd/internal/thirdparty/canvas/image"
)

// pngAlphaDecoder 只属于一个 PDF 文档；扫描行同步写出后即可复用，不跨图片保存结果。
// 通过固定的 input 转接压缩字节，Close 后清掉输入，避免解压器钉住上一张 PNG。
type pngAlphaDecoder struct {
	input   bytes.Reader
	reader  io.ReadCloser
	samples []byte
	row     []byte
}

// 两个缓冲区分别最多保留 4 MiB；超出上限只作当前图片的临时分配。
const pngAlphaScratchLimit = 4 << 20

func pngAlphaScratch(buffer *[]byte, size int) []byte {
	if size > pngAlphaScratchLimit {
		return make([]byte, size)
	}
	if cap(*buffer) < size {
		*buffer = make([]byte, size)
	}
	return (*buffer)[:size]
}

// embedPNGAlpha 保留 PNG 的非预乘颜色与行滤波，避免解码整幅 RGBA 图片。
// ISO 32000-1 §7.4.4.4、§8.9.5、§11.6.5.3：RGB 与 SMask 分别使用
// PNG Predictor 15；即使关闭通用流压缩，这两个预测流也必须使用 FlateDecode。
func (w *pdfPageWriter) embedPNGAlpha(img *cimage.Image) (pdfRef, bool) {
	size := img.Bounds().Size()
	if w.pdf.pngAlpha == nil {
		w.pdf.pngAlpha = &pngAlphaDecoder{}
	}
	rgb, alpha, ok := w.pdf.pngAlpha.streams(img.Bytes, size)
	if !ok {
		return 0, false
	}
	color := pngPredictorStream(rgb, size, 3)
	if alpha != nil {
		color.dict["SMask"] = w.pdf.writeObject(pngPredictorStream(alpha, size, 1))
	}
	return w.pdf.writeObject(color), true
}

func pngPredictorStream(data []byte, size image.Point, colors int) pdfStream {
	space := pdfName("DeviceRGB")
	if colors == 1 {
		space = "DeviceGray"
	}
	return pdfStream{
		dict: pdfDict{
			"Type":             pdfName("XObject"),
			"Subtype":          pdfName("Image"),
			"Width":            size.X,
			"Height":           size.Y,
			"ColorSpace":       space,
			"BitsPerComponent": 8,
			"Interpolate":      true,
			"Filter":           pdfArray{pdfFilterFlate},
			"DecodeParms": pdfArray{pdfDict{
				"Predictor":        15,
				"Colors":           colors,
				"BitsPerComponent": 8,
				"Columns":          size.X,
			}},
		},
		stream: data,
	}
}

// pngAlphaStreams 按通道拆分滤波后的扫描行，而非先恢复像素再做预乘/反预乘。
// PNG 的五种滤波器均只依赖同通道的左、上、左上采样；拆分后保留行首滤波
// 类型，并把每像素字节数从 4 改成 3/1，预测解码即可精确还原 RGB/Alpha。
func pngAlphaStreams(data []byte, size image.Point) ([]byte, []byte, bool) {
	return new(pngAlphaDecoder).streams(data, size)
}

// streams 返回借用的扫描行切片，有效期到该解码器下次调用；调用方须先同步写出。
func (d *pngAlphaDecoder) streams(data []byte, size image.Point) ([]byte, []byte, bool) {
	// 快路径独立限制临时扫描行的内存；大图交回原有受预算约束的解码流程。
	const maxBytes = 64 << 20
	if size.X <= 0 || size.Y <= 0 || size.X > (maxBytes-2)/4 || size.Y > maxBytes/(size.X*4+2) {
		return nil, nil, false
	}
	compressed, ok := pngRGBAData(data, size)
	if !ok {
		return nil, nil, false
	}
	d.input.Reset(compressed)
	defer d.input.Reset(nil)
	var err error
	if d.reader == nil {
		d.reader, err = zlib.NewReader(&d.input)
	} else {
		// zlib.NewReader 保证返回值实现 Resetter。
		resetter, _ := d.reader.(zlib.Resetter)
		err = resetter.Reset(&d.input, nil)
	}
	if err != nil {
		return nil, nil, false
	}
	defer d.reader.Close()

	rgbStride, alphaStride := size.X*3+1, size.X+1
	samples := pngAlphaScratch(&d.samples, (rgbStride+alphaStride)*size.Y)
	rgb, alpha := samples[:rgbStride*size.Y], samples[rgbStride*size.Y:]
	row := pngAlphaScratch(&d.row, size.X*4+1)
	opaque := true
	for y := range size.Y {
		if _, err := io.ReadFull(d.reader, row); err != nil || row[0] > 4 {
			return nil, nil, false
		}
		colorRow := rgb[y*rgbStride : (y+1)*rgbStride]
		alphaRow := alpha[y*alphaStride : (y+1)*alphaStride]
		colorRow[0], alphaRow[0] = row[0], row[0]
		for x := range size.X {
			source, target := 1+x*4, 1+x*3
			copy(colorRow[target:target+3], row[source:source+3])
			alphaRow[1+x] = row[source+3]
		}
		if opaque {
			opaque = pngOpaqueAlphaRow(alphaRow, y == 0)
		}
	}
	// 必须读到 zlib EOF，校验 Adler-32，并拒绝长度不符的扫描行数据。
	var extra [1]byte
	if n, err := d.reader.Read(extra[:]); n != 0 || err != io.EOF {
		return nil, nil, false
	}
	if opaque {
		alpha = nil
	}
	return rgb, alpha, true
}

// pngOpaqueAlphaRow 在此前所有行都不透明时，匹配全 255 行的滤波残差。
// 无需恢复 alpha 像素；发现首个透明采样后便停止检查，完全不透明时不生成 SMask。
func pngOpaqueAlphaRow(row []byte, firstRow bool) bool {
	first, rest := byte(255), byte(255)
	switch row[0] {
	case 1, 4:
		rest = 0
		if row[0] == 4 && !firstRow {
			first = 0
		}
	case 2:
		if !firstRow {
			first, rest = 0, 0
		}
	case 3:
		rest = 128
		if !firstRow {
			first, rest = 128, 0
		}
	}
	if row[1] != first {
		return false
	}
	for _, value := range row[2:] {
		if value != rest {
			return false
		}
	}
	return true
}

// pngRGBAData 只接受完整、非交错的 8 位 RGBA PNG；不支持或损坏时原样回退。
func pngRGBAData(data []byte, size image.Point) ([]byte, bool) {
	info, ok := pngRasterData(data, size, 6)
	return info.compressed, ok
}
