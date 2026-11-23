package pdf

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	cimage "github.com/zc310/ofd/internal/thirdparty/canvas/image"
)

func TestPDFPNGIndexedPreservesIDATPaletteAndAlpha(t *testing.T) {
	for _, bits := range []byte{1, 2, 4, 8} {
		for _, mode := range []string{"不透明", "透明", "未使用透明索引"} {
			t.Run(strconv.Itoa(int(bits))+"/"+mode, func(t *testing.T) {
				source := pngIndexedSource(image.Pt(13, 7), bits, mode)
				for firstFilter := byte(0); firstFilter <= 4; firstFilter++ {
					data := pngIndexedFixture(t, source, bits, firstFilter)
					// 先由标准库独立检查测试图片及所有滤波器，不能让夹具与实现相互掩盖错误。
					decoded, err := png.Decode(bytes.NewReader(data))
					if err != nil {
						t.Fatal(err)
					}
					for y := range source.Rect.Dy() {
						for x := range source.Rect.Dx() {
							if color.NRGBAModel.Convert(decoded.At(x, y)) != source.At(x, y) {
								t.Fatalf("测试 PNG 像素不符：(%d,%d)", x, y)
							}
						}
					}
					for _, compress := range []bool{true, false} {
						checkPNGIndexedPDF(t, source, data, compress)
					}
				}
			})
		}
	}
}

func checkPNGIndexedPDF(t *testing.T, source *image.Paletted, data []byte, compress bool) {
	t.Helper()
	// 不提供像素解码函数，确保快路径不创建完整 RGB 或索引位图。
	img := &cimage.Image{
		Bytes: data, Mimetype: "image/png",
		Config: image.Config{Width: source.Rect.Dx(), Height: source.Rect.Dy(), ColorModel: source.Palette},
	}
	var output bytes.Buffer
	writer := newPDFWriter(&output)
	writer.SetCompression(compress)
	page := writer.NewPage(50, 50)
	page.DrawImage(img, cimage.Lossless, canvas.Identity)
	first := writer.images[img]
	if first == 0 || page.embedImage(img, cimage.Lossless) != first || len(writer.images) != 1 {
		t.Fatal("相同 Indexed 图片未复用引用")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, err := api.ReadAndValidate(t.Context(), bytes.NewReader(output.Bytes()), model.NewStatelessConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	info, ok := pngRasterData(data, source.Bounds().Size(), 3)
	if !ok {
		t.Fatal("测试 PNG 无效")
	}
	wantAlpha := make([]byte, 0, source.Rect.Dx()*source.Rect.Dy())
	hasAlpha := false
	for y := range source.Rect.Dy() {
		for x := range source.Rect.Dx() {
			_, _, _, a := source.At(x, y).RGBA()
			wantAlpha = append(wantAlpha, byte(a>>8))
			hasAlpha = hasAlpha || a != 65535
		}
	}
	found := false
	for _, entry := range ctx.Table {
		if entry == nil {
			continue
		}
		sd, _, err := ctx.DereferenceStreamDict(entry.Object)
		if err != nil || sd == nil {
			continue
		}
		space, ok := sd.Dict["ColorSpace"].(types.Array)
		if !ok || len(space) != 4 || space[0] != types.Name("Indexed") {
			continue
		}
		found = true
		if space[1] != types.Name("DeviceRGB") || space[2] != types.Integer(len(source.Palette)-1) {
			t.Fatal("Indexed 基础色彩空间或调色板长度不符")
		}
		if !bytes.Equal(sd.Raw, info.compressed) {
			t.Fatal("原始 PNG IDAT 被重新压缩")
		}
		if got := sd.IntEntry("BitsPerComponent"); got == nil || *got != info.bits {
			t.Fatal("索引位深改变")
		}
		lookup, _, err := ctx.DereferenceStreamDict(space[3])
		if err != nil || lookup == nil {
			t.Fatalf("读取调色板：%v", err)
		}
		if err := ctx.DecodeStream(lookup); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(lookup.Content, info.palette) {
			t.Fatal("二进制调色板颜色改变")
		}
		if err := ctx.DecodeStream(sd); err != nil {
			t.Fatal(err)
		}
		stride := (source.Rect.Dx()*info.bits + 7) / 8
		for y := range source.Rect.Dy() {
			for x := range source.Rect.Dx() {
				bit := x * info.bits
				index := sd.Content[y*stride+bit/8] >> (8 - info.bits - bit%8) & byte((1<<info.bits)-1)
				if index != source.ColorIndexAt(x, y) {
					t.Fatalf("PDF 预测解码后的索引不符：(%d,%d)", x, y)
				}
			}
		}
		if !hasAlpha {
			if sd.Dict["SMask"] != nil {
				t.Fatal("实际使用的颜色均不透明，不应生成 SMask")
			}
			continue
		}
		mask, _, err := ctx.DereferenceStreamDict(sd.Dict["SMask"])
		if err != nil || mask == nil {
			t.Fatalf("读取透明蒙版：%v", err)
		}
		if err := ctx.DecodeStream(mask); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(mask.Content, wantAlpha) {
			t.Fatal("tRNS 透明度或未指定透明度的默认值未精确保留")
		}
	}
	if !found {
		t.Fatal("图片没有使用 Indexed 色彩空间")
	}
}

func TestPDFPNGIndexedHandlesShortPaletteAndOpaqueTRNS(t *testing.T) {
	source := pngIndexedSource(image.Pt(13, 7), 2, "透明")
	source.Palette = source.Palette[:3]
	for i, index := range source.Pix {
		source.Pix[i] = index % 3
	}
	data := pngIndexedFixture(t, source, 2, 4)
	checkPNGIndexedPDF(t, source, data, true)
	for i, entry := range source.Palette {
		c, _ := entry.(color.NRGBA)
		c.A = 255
		source.Palette[i] = c
	}
	// tRNS 存在不等于图片透明；全 255 的透明度表不能增加无意义的蒙版。
	data = pngIndexedReplaceChunk(data, "tRNS", []byte{255, 255, 255})
	checkPNGIndexedPDF(t, source, data, true)
}

func TestPDFPNGIndexedKeepsLossyModeAndExactAlpha(t *testing.T) {
	source := pngIndexedSource(image.Pt(13, 7), 4, "透明")
	data := pngIndexedFixture(t, source, 4, 3)
	img, err := cimage.NewPNGImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	streams := lossyImageStreams(t, img)
	if len(streams) != 2 {
		t.Fatalf("有损图片及透明蒙版流数量=%d，期望2", len(streams))
	}
	wantAlpha := make([]byte, 0, len(source.Pix))
	for y := range source.Rect.Dy() {
		for x := range source.Rect.Dx() {
			_, _, _, a := source.At(x, y).RGBA()
			wantAlpha = append(wantAlpha, byte(a>>8))
		}
	}
	for _, stream := range streams {
		space := stream.NameEntry("ColorSpace")
		if space != nil && *space == "DeviceRGB" {
			if len(stream.FilterPipeline) != 1 || stream.FilterPipeline[0].Name != "DCTDecode" {
				t.Fatal("Indexed 快路径覆盖了显式有损模式")
			}
		} else if !bytes.Equal(stream.Content, wantAlpha) {
			t.Fatal("有损模式改变了调色板透明度")
		}
	}
}

func TestPNGIndexedRejectsUnsupportedAndDamagedData(t *testing.T) {
	source := pngIndexedSource(image.Pt(13, 7), 2, "透明")
	valid := pngIndexedFixture(t, source, 2, 0)
	cases := map[string][]byte{
		"截断":          valid[:len(valid)-1],
		"尾随数据":        append(append([]byte(nil), valid...), 0),
		"缺少调色板":       pngIndexedReplaceChunk(valid, "PLTE", nil),
		"空调色板":        pngIndexedReplaceChunk(valid, "PLTE", []byte{}),
		"调色板长度不是3的倍数": pngIndexedReplaceChunk(valid, "PLTE", []byte{1, 2}),
		"调色板超过位深":     pngIndexedReplaceChunk(valid, "PLTE", make([]byte, 15)),
		"透明度超过调色板":    pngIndexedReplaceChunk(valid, "tRNS", make([]byte, 5)),
		"空透明度":        pngIndexedReplaceChunk(valid, "tRNS", []byte{}),
	}
	for _, tc := range []struct {
		name  string
		index int
		value byte
	}{
		{"位深16", 24, 16}, {"交错", 28, 1}, {"RGB", 25, 2}, {"CRC损坏", 29, 0},
	} {
		data := append([]byte(nil), valid...)
		data[tc.index] = tc.value
		if tc.index < 29 {
			data = append(pngAlphaChunk([]byte("\x89PNG\r\n\x1a\n"), "IHDR", data[16:29]), data[33:]...)
		}
		cases[tc.name] = data
	}
	overflow := append([]byte(nil), valid...)
	binary.BigEndian.PutUint32(overflow[33:37], ^uint32(0))
	cases["块长度溢出"] = overflow
	info, ok := pngRasterData(valid, source.Bounds().Size(), 3)
	if !ok {
		t.Fatal("测试 PNG 无效")
	}
	for _, kind := range []string{"PLTE", "tRNS", "ABCD", "IHDR", "IDAT"} {
		data := append([]byte(nil), valid[:len(valid)-12]...)
		if kind == "IDAT" {
			data = pngAlphaChunk(data, "tEXt", []byte("key\x00value"))
		}
		data = pngAlphaChunk(data, kind, nil)
		cases["IDAT之后的"+kind] = pngAlphaChunk(data, "IEND", nil)
	}
	duplicate := pngAlphaChunk(append([]byte(nil), valid[:33]...), "PLTE", info.palette)
	cases["重复调色板"] = append(duplicate, valid[33:]...)
	beforePalette := pngAlphaChunk(append([]byte(nil), valid[:33]...), "tRNS", info.transparency)
	cases["透明度早于调色板"] = append(beforePalette, valid[33:]...)
	broken := append([]byte(nil), info.compressed...)
	broken[len(broken)-1] ^= 0xff
	cases["zlib校验损坏"] = pngIndexedWithIDAT(valid, broken)
	stride := (source.Rect.Dx()*2 + 7) / 8
	for name, raw := range map[string][]byte{
		"扫描行不足": {0, 1}, "扫描行过长": make([]byte, (stride+1)*source.Rect.Dy()+1),
		"滤波器无效": append([]byte{5}, make([]byte, (stride+1)*source.Rect.Dy()-1)...),
	} {
		cases[name] = pngIndexedWithIDAT(valid, pngIndexedCompress(t, raw))
	}
	// 2 位索引可以表示 3，但这个调色板只有 3 项，不能让 PDF 静默截断到末项。
	raw := make([]byte, (stride+1)*source.Rect.Dy())
	raw[1] = 0xc0
	shortPalette := pngIndexedReplaceChunk(valid, "PLTE", info.palette[:9])
	cases["索引超出调色板"] = pngIndexedWithIDAT(shortPalette, pngIndexedCompress(t, raw))
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			info, ok := pngRasterData(data, source.Bounds().Size(), 3)
			if ok {
				_, ok = pngIndexedAlpha(info, source.Bounds().Size())
			}
			if ok {
				t.Fatal("快速路径接受了不支持或损坏的图片")
			}
		})
	}
	for _, size := range []image.Point{{}, {-1, 5}, {1 << 30, 1 << 30}} {
		if _, ok := pngIndexedAlpha(info, size); ok {
			t.Fatalf("接受了无效尺寸 %v", size)
		}
	}
}

func TestPDFPNGIndexedRendersPartialTransparency(t *testing.T) {
	tool, err := exec.LookPath("pdftoppm")
	if err != nil {
		t.Skip("缺少 pdftoppm")
	}
	for _, bits := range []byte{1, 2, 4, 8} {
		t.Run(strconv.Itoa(int(bits)), func(t *testing.T) {
			source := pngIndexedSource(image.Pt(13, 7), bits, "透明")
			// 使用色块内部的像素，避免不同 PDF 阅读器的插值采样位置影响透明合成断言。
			blocks := image.NewPaletted(image.Rect(0, 0, source.Rect.Dx()*16, source.Rect.Dy()*16), source.Palette)
			for y := range blocks.Rect.Dy() {
				for x := range blocks.Rect.Dx() {
					blocks.SetColorIndex(x, y, source.ColorIndexAt(x/16, y/16))
				}
			}
			img, err := cimage.NewPNGImage(bytes.NewReader(pngIndexedFixture(t, blocks, bits, 4)))
			if err != nil {
				t.Fatal(err)
			}
			const scale = 12 * 25.4 / 72
			background := color.RGBA{R: 30, G: 60, B: 90, A: 255}
			var output bytes.Buffer
			r := New(&output, float64(source.Rect.Dx())*scale, float64(source.Rect.Dy())*scale, nil)
			r.RenderPath(canvas.Rectangle(float64(source.Rect.Dx())*scale, float64(source.Rect.Dy())*scale),
				canvas.Style{Fill: canvas.Paint{Color: background}}, canvas.Identity)
			r.RenderImage(img, canvas.Identity.Scale(scale/16, scale/16))
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			input, prefix := filepath.Join(root, "indexed.pdf"), filepath.Join(root, "page")
			if err := os.WriteFile(input, output.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			if log, err := exec.CommandContext(t.Context(), tool, "-r", "72", "-png", "-singlefile", input, prefix).CombinedOutput(); err != nil {
				t.Fatalf("pdftoppm: %v: %s", err, log)
			}
			data, err := os.ReadFile(prefix + ".png")
			if err != nil {
				t.Fatal(err)
			}
			page, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			for y := range source.Rect.Dy() {
				for x := range source.Rect.Dx() {
					c, _ := source.At(x, y).(color.NRGBA)
					got, _ := color.RGBAModel.Convert(page.At(x*12+6, y*12+6)).(color.RGBA)
					want := [3]byte{
						byte((int(c.R)*int(c.A) + int(background.R)*(255-int(c.A)) + 127) / 255),
						byte((int(c.G)*int(c.A) + int(background.G)*(255-int(c.A)) + 127) / 255),
						byte((int(c.B)*int(c.A) + int(background.B)*(255-int(c.A)) + 127) / 255),
					}
					for i, value := range []byte{got.R, got.G, got.B} {
						if d := int(value) - int(want[i]); d < -1 || d > 1 {
							t.Fatalf("透明合成像素(%d,%d)=%v，期望 RGB %v", x, y, got, want)
						}
					}
				}
			}
		})
	}
}

func pngIndexedSource(size image.Point, bits byte, mode string) *image.Paletted {
	palette := make(color.Palette, 1<<bits)
	for i := range palette {
		a := byte(255)
		if mode != "不透明" && i < min(5, len(palette)-1) {
			a = [...]byte{0, 1, 127, 128, 254}[i]
		}
		// CR/LF、反斜线及高位字节均需精确保留，不能把调色板当作普通文本写入。
		palette[i] = color.NRGBA{R: byte(i*41 + 13), G: byte(i*19 + 10), B: byte(i*37 + 92), A: a}
	}
	source := image.NewPaletted(image.Rectangle{Max: size}, palette)
	for y := range size.Y {
		for x := range size.X {
			index := byte((x + y*3) % len(palette))
			if mode == "未使用透明索引" || x == 0 && y == 0 {
				index = byte(len(palette) - 1)
			}
			source.SetColorIndex(x, y, index)
		}
	}
	return source
}

func pngIndexedFixture(tb testing.TB, source *image.Paletted, bits, firstFilter byte) []byte {
	tb.Helper()
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header[:4], uint32(source.Rect.Dx()))
	binary.BigEndian.PutUint32(header[4:8], uint32(source.Rect.Dy()))
	header[8], header[9] = bits, 3
	palette := make([]byte, 0, 3*len(source.Palette))
	transparency := make([]byte, 0, len(source.Palette))
	lastTransparent := -1
	for i, c := range source.Palette {
		nrgba, _ := c.(color.NRGBA)
		palette = append(palette, nrgba.R, nrgba.G, nrgba.B)
		transparency = append(transparency, nrgba.A)
		if nrgba.A != 255 {
			lastTransparent = i
		}
	}
	data := pngAlphaChunk([]byte("\x89PNG\r\n\x1a\n"), "IHDR", header)
	data = pngAlphaChunk(data, "PLTE", palette)
	if lastTransparent >= 0 {
		data = pngAlphaChunk(data, "tRNS", transparency[:lastTransparent+1])
	}
	stride := (source.Rect.Dx()*int(bits) + 7) / 8
	rows := make([]byte, 0, (stride+1)*source.Rect.Dy())
	previous := make([]byte, stride)
	for y := range source.Rect.Dy() {
		current := make([]byte, stride)
		for x := range source.Rect.Dx() {
			bit := x * int(bits)
			current[bit/8] |= source.ColorIndexAt(x, y) << (8 - int(bits) - bit%8)
		}
		if padding := 8 - source.Rect.Dx()*int(bits)%8; padding < 8 {
			current[stride-1] |= byte((1 << padding) - 1)
		}
		filter := (firstFilter + byte(y%5)) % 5
		rows = append(rows, filter)
		for i, value := range current {
			left, upperLeft := byte(0), byte(0)
			if i > 0 {
				left, upperLeft = current[i-1], previous[i-1]
			}
			up := previous[i]
			switch filter {
			case 1:
				value -= left
			case 2:
				value -= up
			case 3:
				value -= byte((int(left) + int(up)) / 2)
			case 4:
				// 标准库会独立验证这份滤波夹具。
				value -= pngPaeth(left, up, upperLeft)
			}
			rows = append(rows, value)
		}
		previous = current
	}
	compressed := pngIndexedCompress(tb, rows)
	for _, part := range [][]byte{compressed[:1], compressed[1:5], compressed[5:]} {
		data = pngAlphaChunk(data, "IDAT", part)
	}
	return pngAlphaChunk(data, "IEND", nil)
}

func pngIndexedCompress(tb testing.TB, rows []byte) []byte {
	tb.Helper()
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(rows); err != nil {
		tb.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		tb.Fatal(err)
	}
	return compressed.Bytes()
}

func pngIndexedReplaceChunk(data []byte, kind string, payload []byte) []byte {
	result := append([]byte(nil), data[:8]...)
	for pos := 8; pos < len(data); {
		end := pos + 12 + int(binary.BigEndian.Uint32(data[pos:pos+4]))
		if string(data[pos+4:pos+8]) == kind {
			if payload != nil {
				result = pngAlphaChunk(result, kind, payload)
			}
		} else {
			result = append(result, data[pos:end]...)
		}
		pos = end
	}
	return result
}

func pngIndexedWithIDAT(data, compressed []byte) []byte {
	data = pngIndexedReplaceChunk(data, "IDAT", nil)
	data = pngAlphaChunk(data[:len(data)-12], "IDAT", compressed)
	return pngAlphaChunk(data, "IEND", nil)
}

func BenchmarkEmbedPNGIndexed(b *testing.B) {
	source := pngIndexedSource(image.Pt(1024, 768), 8, "不透明")
	data := pngIndexedFixture(b, source, 8, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		img, err := cimage.NewPNGImage(bytes.NewReader(data))
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
