package converter

import (
	"bytes"
	"fmt"
	"os/exec"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/testutil"
)

func TestPDFLossyImagesDefaultAndParallelEncodingPreserveMasks(t *testing.T) {
	const input = "../../testdata/ofd-creator/image-effects.ofd"
	requireRenderFixture(t, input)
	var reference []byte
	var referenceMasks [][]byte
	for _, parallel := range []bool{false, true} {
		for _, lossy := range []bool{false, true} {
			t.Run(fmt.Sprintf("parallel=%t/lossy=%t", parallel, lossy), func(t *testing.T) {
				var output bytes.Buffer
				opts := []Option{PDFParallel(parallel), Page(1)}
				if lossy {
					opts = append(opts, PDFLossyImages(true))
				}
				if err := PDF(t.Context(), input, &output, opts...); err != nil {
					t.Fatal(err)
				}
				jpegCount, masks := pdfLossyImageInfo(t, output.Bytes())
				if lossy && jpegCount == 0 || !lossy && jpegCount != 0 {
					t.Fatalf("图片有损开关未生效：JPEG=%d", jpegCount)
				}
				if !parallel && !lossy {
					reference = bytes.Clone(output.Bytes())
					referenceMasks = masks
				}
				if len(masks) == 0 || len(masks) != len(referenceMasks) {
					t.Fatalf("SMask数量变化：%d，期望%d", len(masks), len(referenceMasks))
				}
				for _, mask := range masks {
					found := false
					for _, expected := range referenceMasks {
						found = found || bytes.Equal(mask, expected)
					}
					if !found {
						t.Fatal("有损开关改变了透明蒙版采样值")
					}
				}
				if lossy && !parallel {
					t.Run("文字与像素", func(t *testing.T) {
						if tool, err := exec.LookPath("pdftotext"); err == nil {
							texts := make([][]byte, 0, 2)
							for _, data := range [][]byte{reference, output.Bytes()} {
								command := exec.CommandContext(t.Context(), tool, "-layout", "-", "-")
								command.Stdin = bytes.NewReader(data)
								text, err := command.Output()
								if err != nil {
									t.Fatal(err)
								}
								texts = append(texts, text)
							}
							if !bytes.Equal(texts[0], texts[1]) {
								t.Fatal("图片编码开关改变了文字提取")
							}
						}
						tool, err := exec.LookPath("pdftoppm")
						if err != nil {
							t.Skip("缺少pdftoppm")
						}
						first, err := testutil.RasterizePDFPages(t.Context(), tool, reference, 96)
						if err != nil {
							t.Fatal(err)
						}
						second, err := testutil.RasterizePDFPages(t.Context(), tool, output.Bytes(), 96)
						if err != nil {
							t.Fatal(err)
						}
						result, err := testutil.ComparePageImages(first, second, 30)
						if err != nil || result.PixelMatchRate < .95 {
							t.Fatalf("有损图片视觉差异过大：%+v，%v", result, err)
						}
					})
				}
			})
		}
	}
}

func pdfLossyImageInfo(t *testing.T, data []byte) (int, [][]byte) {
	t.Helper()
	ctx := readPDFContext(t, data)
	jpegCount := 0
	var masks [][]byte
	for _, entry := range ctx.Table {
		if entry == nil {
			continue
		}
		sd, _, err := ctx.DereferenceStreamDict(entry.Object)
		if err != nil || sd == nil {
			continue
		}
		if subtype := sd.NameEntry("Subtype"); subtype == nil || *subtype != "Image" {
			continue
		}
		for _, filter := range sd.FilterPipeline {
			if filter.Name == "DCTDecode" {
				jpegCount++
			}
		}
		if maskRef, ok := sd.Dict["SMask"].(types.IndirectRef); ok {
			mask, _, err := ctx.DereferenceStreamDict(maskRef)
			if err != nil {
				t.Fatal(err)
			}
			for _, filter := range mask.FilterPipeline {
				if filter.Name == "DCTDecode" {
					t.Fatal("透明蒙版不得新增有损编码")
				}
			}
			if err := ctx.DecodeStream(mask); err != nil {
				t.Fatal(err)
			}
			masks = append(masks, bytes.Clone(mask.Content))
		}
	}
	return jpegCount, masks
}
