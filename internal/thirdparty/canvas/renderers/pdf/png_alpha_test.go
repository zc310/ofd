package pdf

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"strconv"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	cimage "github.com/zc310/ofd/internal/thirdparty/canvas/image"
)

func TestPNGAlphaStreamsPreserveAllFiltersAndStraightSamples(t *testing.T) {
	for filter := byte(0); filter <= 4; filter++ {
		t.Run(strconv.Itoa(int(filter)), func(t *testing.T) {
			source := pngAlphaSource()
			data := pngAlphaFixture(t, source, filter)
			rgb, alpha, ok := pngAlphaStreams(data, source.Bounds().Size())
			if !ok {
				t.Fatal("拒绝了合法 RGBA PNG")
			}
			// 用独立的标准 PNG 解码器验证拆分后的预测流，不复用被测拆分算法。
			colorImage := pngAlphaDecodePlane(t, rgb, source.Bounds().Size(), 2)
			alphaImage := pngAlphaDecodePlane(t, alpha, source.Bounds().Size(), 0)
			for y := range source.Bounds().Dy() {
				for x := range source.Bounds().Dx() {
					want := source.NRGBAAt(x, y)
					r, g, b, _ := colorImage.At(x, y).RGBA()
					a, _, _, _ := alphaImage.At(x, y).RGBA()
					if got := (color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}); got != want {
						t.Fatalf("像素(%d,%d)=%v，期望原始非预乘颜色 %v", x, y, got, want)
					}
				}
			}
		})
	}
}

func TestPDFPNGAlphaUsesPredictorsWithoutPixelDecode(t *testing.T) {
	for _, compress := range []bool{true, false} {
		t.Run(strconv.FormatBool(compress), func(t *testing.T) {
			source := pngAlphaSource()
			data := pngAlphaFixture(t, source, 4)
			// 不设置像素解码函数；若嵌入过程尝试完整解码，这个包装会直接 panic。
			img := &cimage.Image{
				Bytes: data, Mimetype: "image/png",
				Config: image.Config{Width: source.Rect.Dx(), Height: source.Rect.Dy(), ColorModel: color.NRGBAModel},
			}
			var output bytes.Buffer
			writer := newPDFWriter(&output)
			writer.SetCompression(compress)
			page := writer.NewPage(50, 50)
			first := page.embedImage(img, cimage.Lossless)
			if second := page.embedImage(img, cimage.Lossless); second != first || len(writer.images) != 1 {
				t.Fatal("相同 PNG 没有复用 PDF 图像引用")
			}
			page.DrawImage(img, cimage.Lossless, canvas.Identity)
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			ctx, err := api.ReadAndValidate(t.Context(), bytes.NewReader(output.Bytes()), model.NewStatelessConfiguration())
			if err != nil {
				t.Fatal(err)
			}
			streams := 0
			for _, entry := range ctx.Table {
				if entry == nil {
					continue
				}
				sd, _, err := ctx.DereferenceStreamDict(entry.Object)
				if err != nil || sd == nil || sd.NameEntry("Subtype") == nil || *sd.NameEntry("Subtype") != "Image" {
					continue
				}
				streams++
				if err := ctx.DecodeStream(sd); err != nil {
					t.Fatal(err)
				}
				channels := 3
				if *sd.NameEntry("ColorSpace") == "DeviceGray" {
					channels = 1
				} else if sd.Dict["SMask"] == nil {
					t.Fatal("RGB 图片丢失透明蒙版")
				}
				var want []byte
				for y := range source.Rect.Dy() {
					for x := range source.Rect.Dx() {
						c := source.NRGBAAt(x, y)
						if channels == 1 {
							want = append(want, c.A)
						} else {
							want = append(want, c.R, c.G, c.B)
						}
					}
				}
				if !bytes.Equal(sd.Content, want) {
					t.Fatal("PDF 预测解码后的颜色或透明通道与 PNG 原采样不符")
				}
			}
			if streams != 2 {
				t.Fatalf("图像流数量=%d，期望颜色及透明蒙版各一个", streams)
			}
		})
	}
}

func TestPNGAlphaStreamsOmitOpaqueMaskForEveryFilter(t *testing.T) {
	for filter := byte(0); filter <= 4; filter++ {
		t.Run(strconv.Itoa(int(filter)), func(t *testing.T) {
			source := pngAlphaSource()
			for i := 3; i < len(source.Pix); i += 4 {
				source.Pix[i] = 255
			}
			data := pngAlphaFixture(t, source, filter)
			rgb, alpha, ok := pngAlphaStreams(data, source.Bounds().Size())
			if !ok || alpha != nil {
				t.Fatal("完全不透明的 RGBA PNG 不应生成 SMask")
			}
			decoded := pngAlphaDecodePlane(t, rgb, source.Bounds().Size(), 2)
			for y := range source.Rect.Dy() {
				for x := range source.Rect.Dx() {
					r, g, b, a := decoded.At(x, y).RGBA()
					wantR, wantG, wantB, wantA := source.At(x, y).RGBA()
					if r != wantR || g != wantG || b != wantB || a != wantA {
						t.Fatalf("不透明图片的颜色改变：(%d,%d)", x, y)
					}
				}
			}
			// 不能只检测首行或行首；最后一个像素的透明度同样必须保留。
			source.Pix[len(source.Pix)-1] = 254
			data = pngAlphaFixture(t, source, filter)
			if _, alpha, ok := pngAlphaStreams(data, source.Bounds().Size()); !ok || alpha == nil {
				t.Fatal("最后一个像素的透明度被误判为不透明")
			}
		})
	}
}

func TestPNGAlphaStreamsRejectUnsupportedAndDamagedData(t *testing.T) {
	source := pngAlphaSource()
	valid := pngAlphaFixture(t, source, 0)
	cases := map[string][]byte{
		"截断":   valid[:len(valid)-1],
		"头部不足": valid[:20],
		"尾随数据": append(append([]byte(nil), valid...), 0),
	}
	overflow := append([]byte(nil), valid...)
	binary.BigEndian.PutUint32(overflow[33:37], ^uint32(0))
	cases["块长度溢出"] = overflow
	compressed, ok := pngRGBAData(valid, source.Bounds().Size())
	if !ok {
		t.Fatal("测试 PNG 无效")
	}
	for _, index := range []int{0, len(compressed) - 1} {
		broken := append([]byte(nil), compressed...)
		broken[index] ^= 0xff
		data := pngAlphaChunk(append([]byte(nil), valid[:33]...), "IDAT", broken)
		cases["zlib损坏"+strconv.Itoa(index)] = pngAlphaChunk(data, "IEND", nil)
	}
	for _, tc := range []struct {
		name  string
		index int
		value byte
	}{
		{"16位", 24, 16}, {"RGB", 25, 2}, {"调色板", 25, 3},
		{"交错", 28, 1}, {"CRC损坏", 29, 0}, {"尺寸不同", 19, 12},
	} {
		data := append([]byte(nil), valid...)
		data[tc.index] = tc.value
		if tc.index < 29 {
			binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
		}
		cases[tc.name] = data
	}
	for _, kind := range []string{"PLTE", "tRNS", "ABCD", "IHDR", "IDAT"} {
		// 在最后一个 IDAT 之后插入块，覆盖不支持的块和不连续的 IDAT。
		data := append([]byte(nil), valid[:len(valid)-12]...)
		if kind == "IDAT" {
			data = pngAlphaChunk(data, "tEXt", []byte("key\x00value"))
		}
		data = pngAlphaChunk(data, kind, nil)
		cases[kind] = pngAlphaChunk(data, "IEND", nil)
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, ok := pngAlphaStreams(data, source.Bounds().Size()); ok {
				t.Fatal("快速路径接受了不支持或损坏的图片")
			}
		})
	}
	for _, size := range []image.Point{{}, {-1, 5}, {1 << 30, 1 << 30}} {
		if _, _, ok := pngAlphaStreams(valid, size); ok {
			t.Fatalf("快速路径接受了无效尺寸 %v", size)
		}
	}
	// 结构和 CRC 合法，但扫描行、滤波器或 zlib 校验无效。
	for name, raw := range map[string][]byte{
		"扫描行不足": {0, 1}, "扫描行过长": make([]byte, (source.Rect.Dx()*4+1)*source.Rect.Dy()+1),
		"滤波器无效": append([]byte{5}, make([]byte, (source.Rect.Dx()*4+1)*source.Rect.Dy()-1)...),
	} {
		t.Run(name, func(t *testing.T) {
			data := pngAlphaWithRows(t, source.Bounds().Size(), 6, raw)
			if _, _, ok := pngAlphaStreams(data, source.Bounds().Size()); ok {
				t.Fatal("快速路径接受了无效的扫描行数据")
			}
		})
	}
}

func pngAlphaSource() *image.NRGBA {
	source := image.NewNRGBA(image.Rect(0, 0, 13, 7))
	alphas := [...]byte{0, 1, 63, 127, 128, 254, 255}
	for y := range source.Rect.Dy() {
		for x := range source.Rect.Dx() {
			source.SetNRGBA(x, y, color.NRGBA{R: byte(3 + x*19), G: byte(7 + y*37), B: byte(x*23 + y*17), A: alphas[(x+y)%len(alphas)]})
		}
	}
	return source
}

func pngAlphaFixture(tb testing.TB, source *image.NRGBA, firstFilter byte) []byte {
	tb.Helper()
	stride := source.Rect.Dx() * 4
	rows := make([]byte, 0, (1+stride)*source.Rect.Dy())
	for y := range source.Rect.Dy() {
		filter := (firstFilter + byte(y)) % 5
		rows = append(rows, filter)
		for i := range stride {
			value := source.Pix[y*source.Stride+i]
			left, up, upperLeft := byte(0), byte(0), byte(0)
			if i >= 4 {
				left = source.Pix[y*source.Stride+i-4]
			}
			if y > 0 {
				up = source.Pix[(y-1)*source.Stride+i]
				if i >= 4 {
					upperLeft = source.Pix[(y-1)*source.Stride+i-4]
				}
			}
			switch filter {
			case 1:
				value -= left
			case 2:
				value -= up
			case 3:
				value -= byte((int(left) + int(up)) / 2)
			case 4:
				// 独立实现 PNG Paeth 预测，显式覆盖平局时的选择顺序。
				p := int(left) + int(up) - int(upperLeft)
				dl := max(p-int(left), int(left)-p)
				du := max(p-int(up), int(up)-p)
				dul := max(p-int(upperLeft), int(upperLeft)-p)
				predicted := upperLeft
				if dl <= du && dl <= dul {
					predicted = left
				} else if du <= dul {
					predicted = up
				}
				value -= predicted
			}
			rows = append(rows, value)
		}
	}
	return pngAlphaWithRows(tb, source.Bounds().Size(), 6, rows)
}

func pngAlphaWithRows(tb testing.TB, size image.Point, colorType byte, rows []byte) []byte {
	tb.Helper()
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(rows); err != nil {
		tb.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		tb.Fatal(err)
	}
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header[:4], uint32(size.X))
	binary.BigEndian.PutUint32(header[4:8], uint32(size.Y))
	header[8], header[9] = 8, colorType
	data := pngAlphaChunk([]byte("\x89PNG\r\n\x1a\n"), "IHDR", header)
	// 跨越 zlib 头和采样边界拆分多个 IDAT，不能依赖块边界与扫描行对齐。
	for _, part := range [][]byte{compressed.Bytes()[:1], compressed.Bytes()[1:5], compressed.Bytes()[5:]} {
		data = pngAlphaChunk(data, "IDAT", part)
	}
	return pngAlphaChunk(data, "IEND", nil)
}

func pngAlphaChunk(data []byte, kind string, payload []byte) []byte {
	data = binary.BigEndian.AppendUint32(data, uint32(len(payload)))
	start := len(data)
	data = append(data, kind...)
	data = append(data, payload...)
	return binary.BigEndian.AppendUint32(data, crc32.ChecksumIEEE(data[start:]))
}

func pngAlphaDecodePlane(t *testing.T, rows []byte, size image.Point, colorType byte) image.Image {
	t.Helper()
	decoded, err := png.Decode(bytes.NewReader(pngAlphaWithRows(t, size, colorType, rows)))
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func BenchmarkEmbedPNGAlpha(b *testing.B) {
	const input = "../../../../../testdata/ofdrw/intro.ofd"
	archive, err := zip.OpenReader(input)
	if errors.Is(err, os.ErrNotExist) {
		b.Skipf("缺少样例 %s", input)
	}
	if err != nil {
		b.Fatal(err)
	}
	defer archive.Close()
	file, err := archive.Open("Doc_0/Res/image_687.png")
	if err != nil {
		b.Fatal(err)
	}
	defer file.Close()
	original, err := cimage.NewPNGImage(file)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		img, err := cimage.NewPNGImage(bytes.NewReader(original.Bytes))
		if err != nil {
			b.Fatal(err)
		}
		var output bytes.Buffer
		writer := newPDFWriter(&output)
		writer.NewPage(210, 297).DrawImage(img, cimage.Lossless, canvas.Identity)
		if err := writer.Close(); err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(output.Len()), "PDF-bytes/op")
	}
}
