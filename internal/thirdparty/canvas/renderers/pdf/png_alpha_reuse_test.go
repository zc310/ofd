package pdf

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	cimage "github.com/zc310/ofd/internal/thirdparty/canvas/image"
)

func TestPNGAlphaDecoderReusesBuffersAcrossSizesFiltersAndOpaqueMasks(t *testing.T) {
	decoder := &pngAlphaDecoder{}
	for i, size := range []image.Point{{31, 23}, {13, 7}, {1, 1}, {13, 7}, {31, 23}} {
		source := image.NewNRGBA(image.Rectangle{Max: size})
		for y := range size.Y {
			for x := range size.X {
				alpha := byte(255)
				if i%2 == 0 {
					alpha = byte((x + 17*y) % 256)
				}
				source.SetNRGBA(x, y, color.NRGBA{R: byte(x), G: byte(y), B: byte(x + y), A: alpha})
			}
		}
		input := pngAlphaFixture(t, source, byte(i))
		before := bytes.Clone(input)
		wantRGB, wantAlpha, ok := pngAlphaStreams(input, size)
		if !ok {
			t.Fatal("独立解码拒绝了合法 PNG")
		}
		oldReader := decoder.reader
		var oldSamples, oldRow *byte
		if i != 0 {
			oldSamples, oldRow = &decoder.samples[0], &decoder.row[0]
		}
		rgb, alpha, ok := decoder.streams(input, size)
		if !ok || !bytes.Equal(rgb, wantRGB) || !bytes.Equal(alpha, wantAlpha) || (alpha == nil) != (wantAlpha == nil) {
			t.Fatalf("第%d张图片残留了上张图片的扫描行、滤波器或透明标记", i+1)
		}
		if i != 0 && (decoder.reader != oldReader || &decoder.samples[0] != oldSamples || &decoder.row[0] != oldRow) {
			t.Fatal("较小或相同尺寸的图片没有复用解压器与扫描行缓冲")
		}
		if decoder.input.Size() != 0 || !bytes.Equal(input, before) {
			t.Fatal("解压器保留了源数据引用或修改了共享 PNG")
		}
	}
}

func TestPNGAlphaDecoderRecoversAfterBadHeadersRowsAndChecksums(t *testing.T) {
	source := pngAlphaSource()
	size := source.Bounds().Size()
	valid := pngAlphaFixture(t, source, 0)
	compressed, ok := pngRGBAData(valid, size)
	if !ok {
		t.Fatal("测试 PNG 无效")
	}
	withIDAT := func(data []byte) []byte {
		png := pngAlphaChunk(bytes.Clone(valid[:33]), "IDAT", data)
		return pngAlphaChunk(png, "IEND", nil)
	}
	header, checksum := bytes.Clone(compressed), bytes.Clone(compressed)
	header[0] ^= 255
	checksum[len(checksum)-1] ^= 255
	badRows := pngAlphaWithRows(t, size, 6, append([]byte{5}, make([]byte, (size.X*4+1)*size.Y-1)...))
	extraRows := pngAlphaWithRows(t, size, 6, make([]byte, (size.X*4+1)*size.Y+1))
	decoder := &pngAlphaDecoder{}
	for _, damaged := range [][]byte{
		withIDAT(header), withIDAT(compressed[:len(compressed)/2]), withIDAT(checksum),
		badRows, extraRows, valid[:len(valid)-1],
	} {
		// 同时覆盖首次创建失败，以及成功解压后 Reset 到损坏输入再恢复。
		if _, _, ok := decoder.streams(damaged, size); ok {
			t.Fatal("复用解码器接受了损坏数据")
		}
		if decoder.input.Size() != 0 {
			t.Fatal("失败路径保留了压缩输入")
		}
		gotRGB, gotAlpha, ok := decoder.streams(valid, size)
		wantRGB, wantAlpha, _ := pngAlphaStreams(valid, size)
		if !ok || !bytes.Equal(gotRGB, wantRGB) || !bytes.Equal(gotAlpha, wantAlpha) {
			t.Fatal("失败后的有效图片受到了前次解压状态影响")
		}
	}
}

func TestPNGAlphaScratchDoesNotRetainOversizedTemporaryBuffers(t *testing.T) {
	var buffer []byte
	small := pngAlphaScratch(&buffer, 1024)
	large := pngAlphaScratch(&buffer, pngAlphaScratchLimit+1)
	if len(large) != pngAlphaScratchLimit+1 || cap(buffer) > pngAlphaScratchLimit || &buffer[0] != &small[0] {
		t.Fatal("超大临时缓冲扩大了长期保留的缓存")
	}
	for i := range small {
		small[i] = 17
	}
	for i := range large {
		large[i] = 29
	}
	if small[0] != 17 || large[0] != 29 {
		t.Fatal("临时大缓冲与缓存别名")
	}

	decoder := &pngAlphaDecoder{}
	size := image.Pt(1024, 1025)
	data := pngAlphaWithRows(t, size, 6, make([]byte, (size.X*4+1)*size.Y))
	rgb, alpha, ok := decoder.streams(data, size)
	if !ok || len(rgb) != (size.X*3+1)*size.Y || len(alpha) != (size.X+1)*size.Y {
		t.Fatal("超过保留上限的图片仍应按原预算正常解压")
	}
	if cap(decoder.samples) > pngAlphaScratchLimit || cap(decoder.row) > pngAlphaScratchLimit || decoder.input.Size() != 0 {
		t.Fatal("解码器保留了超大缓冲或压缩源数据")
	}
}

func TestPDFPNGAlphaDecoderPublishesOnlyValidImagesAndReleasesOnClose(t *testing.T) {
	var output bytes.Buffer
	writer := newPDFWriter(&output)
	page := writer.NewPage(50, 50)
	source := pngAlphaSource()
	data := pngAlphaFixture(t, source, 3)
	makeImage := func(data []byte) *cimage.Image {
		return &cimage.Image{
			Bytes: data, Mimetype: "image/png",
			Config: image.Config{Width: source.Rect.Dx(), Height: source.Rect.Dy(), ColorModel: color.NRGBAModel},
		}
	}
	if _, ok := page.embedPNGAlpha(makeImage(data)); !ok {
		t.Fatal("合法图片未进入快路径")
	}
	decoder, objects := writer.pngAlpha, len(writer.objOffsets)
	compressed, _ := pngRGBAData(data, source.Bounds().Size())
	badCompressed := bytes.Clone(compressed)
	badCompressed[len(badCompressed)-1] ^= 255
	damaged := pngAlphaChunk(pngAlphaChunk(bytes.Clone(data[:33]), "IDAT", badCompressed), "IEND", nil)
	if _, ok := page.embedPNGAlpha(makeImage(damaged)); ok || len(writer.objOffsets) != objects {
		t.Fatal("校验失败的 PNG 发布了部分 PDF 图像对象")
	}
	if _, ok := page.embedPNGAlpha(makeImage(data)); !ok || writer.pngAlpha != decoder {
		t.Fatal("损坏图片之后未复用并恢复解码器")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if writer.pngAlpha != nil {
		t.Fatal("关闭 PDF 后未释放解压器和扫描行缓冲")
	}
}

func BenchmarkPNGAlphaDecoderReuse(b *testing.B) {
	source := image.NewNRGBA(image.Rect(0, 0, 595, 754))
	for i := range source.Pix {
		source.Pix[i] = byte(i * 17)
	}
	data := pngAlphaFixture(b, source, 0)
	for _, reuse := range []bool{false, true} {
		name := "fresh"
		if reuse {
			name = "reuse"
		}
		b.Run(name, func(b *testing.B) {
			decoder := &pngAlphaDecoder{}
			b.ReportAllocs()
			for range b.N {
				if !reuse {
					decoder = &pngAlphaDecoder{}
				}
				if _, _, ok := decoder.streams(data, source.Bounds().Size()); !ok {
					b.Fatal("拒绝了合法 PNG")
				}
			}
		})
	}
}
