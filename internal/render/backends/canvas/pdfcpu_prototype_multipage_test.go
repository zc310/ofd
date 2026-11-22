package canvas

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/pdfcpuwriter"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/testutil"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestPDFCPUPrototypeMultipageSharesFontsAndTransparentImages(t *testing.T) {
	r, err := pdfcpuwriter.New(t.Context(), 40, 30)
	if err != nil {
		t.Fatal(err)
	}
	family := pdfTextTestFamily(t)
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.SetNRGBA(x, y, color.NRGBA{R: 200, A: uint8(30 * x)})
		}
	}
	var reference bytes.Buffer
	doc, err := newCanvasPDFDocument(&reference, drawing.PDFOptions{Compress: true, SubsetFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	for i, value := range []string{"AB", "CD", "ABCD"} {
		width, height := 40+float64(i)*5, 30+float64(i)*3
		if i > 0 {
			if err := r.NextPage(width, height); err != nil {
				t.Fatal(err)
			}
		}
		text := canvas.NewTextLine(family.Face(18, canvas.Black), value, canvas.Left)
		scene := canvas.New(width, height)
		for _, target := range []canvas.Renderer{r, scene} {
			target.RenderText(text, canvas.Identity.Translate(2, 18))
			target.RenderImage(img, canvas.Identity.Translate(2, 2))
			// A separate allocation with identical pixels must also reuse the resource.
			clone := image.NewNRGBA(img.Bounds())
			copy(clone.Pix, img.Pix)
			target.RenderImage(clone, canvas.Identity.Translate(15, 2))
		}
		if err := doc.AddPage(canvasVectorSurface{c: scene}, nil); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	if err := r.Write(&output); err != nil {
		t.Fatal(err)
	}
	if err := doc.Close(); err != nil {
		t.Fatal(err)
	}
	stats := r.Stats()
	if stats.Fonts != 1 || stats.Glyphs != 5 || stats.Images != 1 || stats.Pages != 3 {
		t.Fatalf("跨页资源未复用：%+v", stats)
	}
	if err := api.Validate(t.Context(), bytes.NewReader(output.Bytes()), model.NewStatelessConfiguration(), nil); err != nil {
		t.Fatal(err)
	}
	images := pdfTextImages(t, output.Bytes())
	if len(images) != 2 {
		t.Fatalf("应仅有颜色图片及SMask各一张，实际%d张", len(images))
	}
	ctx := prototypePDFContext(t, output.Bytes())
	masks := 0
	for _, image := range images {
		if image.Dict["Interpolate"] != types.Boolean(true) {
			t.Fatal("颜色图片与SMask必须同时启用插值")
		}
		if ref := image.Dict["SMask"]; ref != nil {
			masks++
			mask, _, err := ctx.DereferenceStreamDict(ref)
			if err != nil || mask == nil {
				t.Fatalf("透明图片未保留有效SMask：%v", err)
			}
			if mask.Dict["Interpolate"] != types.Boolean(true) {
				t.Fatal("关联的SMask未开启插值")
			}
		}
	}
	if masks != 1 {
		t.Fatalf("透明图片的SMask关联数=%d，期望1", masks)
	}
	prototypeComparePage(t, output.Bytes(), reference.Bytes())
}

func TestPDFCPUPrototypeIntroMultipage(t *testing.T) {
	prototypeIntroPages(t, []int{7, 26, 7}, true)
}

func TestPDFCPUPrototypeIntroImageEdgesPages32To35(t *testing.T) {
	data, reference := prototypeIntroPages(t, []int{32, 33, 34, 35}, false)
	prototypeComparePageAtMatchRate(t, data, reference, .999999)
}

func TestPDFCPUPrototypeIntroFullDocument(t *testing.T) {
	prototypeIntroPages(t, nil, false)
}

func prototypeIntroPages(t *testing.T, numbers []int, comparePixels bool) ([]byte, []byte) {
	t.Helper()
	document := prototypeIntroDocument(t)
	if numbers == nil {
		numbers = make([]int, len(document.Pages))
		for i := range numbers {
			numbers[i] = i + 1
		}
	}
	var r *pdfcpuwriter.Document
	var reference bytes.Buffer
	doc, err := newCanvasPDFDocument(&reference, drawing.PDFOptions{Compress: true, SubsetFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, number := range numbers {
		page := document.Pages[number-1]
		box, err := page.PhysicalBox()
		if err != nil {
			t.Fatal(err)
		}
		if r == nil {
			r, err = pdfcpuwriter.New(t.Context(), box.Width, box.Height)
		} else {
			err = r.NextPage(box.Width, box.Height)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := document.Draw(newCanvasBackend(canvas.NewContext(r)), page); err != nil {
			t.Fatal(err)
		}
		if r.Err() != nil {
			t.Fatalf("第%d页：%v", number, r.Err())
		}
		scene := canvas.New(box.Width, box.Height)
		if err := document.Draw(newCanvasBackend(canvas.NewContext(scene)), page); err != nil {
			t.Fatal(err)
		}
		if err := doc.AddPage(canvasVectorSurface{c: scene}, nil); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	if err := r.Write(&output); err != nil {
		t.Fatal(err)
	}
	if err := doc.Close(); err != nil {
		t.Fatal(err)
	}
	if err := api.Validate(t.Context(), bytes.NewReader(output.Bytes()), model.NewStatelessConfiguration(), nil); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d页：pdfcpu=%d，canvas=%d；字体=%d，图片=%d，Pattern=%d", len(numbers), output.Len(), reference.Len(), r.Stats().Fonts, r.Stats().Images, r.Stats().Patterns)
	if dir := os.Getenv("OFD_PDFCPU_PROTOTYPE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range map[string][]byte{"multipage-pdfcpu.pdf": output.Bytes(), "multipage-canvas.pdf": reference.Bytes()} {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if comparePixels {
		prototypeComparePage(t, output.Bytes(), reference.Bytes())
	} else if first, second := prototypeExtractText(t, output.Bytes()), prototypeExtractText(t, reference.Bytes()); !bytes.Equal(first, second) {
		t.Fatal("完整文档文字提取不同")
	}
	if !comparePixels && os.Getenv("OFD_PDFCPU_FULL_PIXELS") != "" {
		prototypeComparePagesIndividually(t, output.Bytes(), reference.Bytes(), len(numbers))
	}
	return output.Bytes(), reference.Bytes()
}

// Rasterize one page at a time to avoid retaining 84 full-resolution images.
func prototypeComparePagesIndividually(t *testing.T, actual, reference []byte, count int) {
	t.Helper()
	tool, err := exec.LookPath("pdftoppm")
	if err != nil {
		t.Skip("缺少pdftoppm")
	}
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "actual.pdf"), filepath.Join(dir, "reference.pdf")}
	for i, data := range [][]byte{actual, reference} {
		if err := os.WriteFile(paths[i], data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for number := 1; number <= count; number++ {
		t.Run("第"+strconv.Itoa(number)+"页像素", func(t *testing.T) {
			images := make([]*image.RGBA, 0, 2)
			for _, path := range paths {
				command := exec.CommandContext(t.Context(), tool, "-f", strconv.Itoa(number), "-l", strconv.Itoa(number), "-singlefile", "-png", "-r", "144", path)
				var stderr bytes.Buffer
				command.Stderr = &stderr
				data, err := command.Output()
				if err != nil {
					t.Fatalf("栅格化失败：%v，%s", err, stderr.Bytes())
				}
				decoded, err := png.Decode(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				img := image.NewRGBA(decoded.Bounds())
				draw.Draw(img, img.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
				images = append(images, img)
			}
			result, err := testutil.ComparePageImages(images[:1], images[1:], 20)
			if err != nil || result.PixelMatchRate < .99 {
				t.Fatalf("像素差异过大：%+v，%v", result, err)
			}
			t.Logf("像素匹配率：%.8f；平均绝对误差：%.8f", result.PixelMatchRate, result.MeanAbsoluteError)
		})
	}
}

func TestPDFCPUPrototypeInvisibleTextRemainsSearchableWithoutInk(t *testing.T) {
	r, err := pdfcpuwriter.New(t.Context(), 40, 30)
	if err != nil {
		t.Fatal(err)
	}
	family := pdfTextTestFamily(t)
	r.RenderText(canvas.NewTextLine(family.Face(18, canvas.Transparent), "Invisible", canvas.Left), canvas.Identity.Translate(2, 15))
	var output bytes.Buffer
	if err := r.Write(&output); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(prototypePDFContent(t, output.Bytes(), 1), []byte("BT 3 Tr")) {
		t.Fatal("不可见文字未使用不可见渲染模式")
	}
	if !bytes.Contains(prototypeExtractText(t, output.Bytes()), []byte("Invisible")) {
		t.Fatal("不可见文字丢失搜索语义")
	}
}
