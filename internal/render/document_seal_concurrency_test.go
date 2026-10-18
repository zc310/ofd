package render

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/testutil"
	"github.com/zc310/ofd/internal/utils"
	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/gofont/goregular"
)

func awaitSealOperation(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("印章操作未能并发完成")
	}
}

type pausedSealContext struct {
	DrawContext
	once    sync.Once
	entered chan<- struct{}
	resume  <-chan struct{}
}

func (ctx *pausedSealContext) DrawPath(x, y float64, path *geom.Path) {
	ctx.once.Do(func() { ctx.entered <- struct{}{}; <-ctx.resume })
	ctx.DrawContext.DrawPath(x, y, path)
}

func concurrentSealData(t *testing.T) []byte {
	t.Helper()
	data, err := creator.Marshal(creator.Document{
		ID: "concurrent-seal", PageSize: creator.PageSize{Width: 10, Height: 10},
		Fonts: []creator.Font{{Name: "Go", Data: goregular.TTF}},
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Path{X: 2, Y: 2, Width: 6, Height: 6, Data: "M 0 0 L 6 0 L 6 6 L 0 6 C", Fill: true, FillColor: &creator.Color{R: 255}},
			creator.Text{X: 2, Y: 3, Width: 6, Height: 3, Font: "Go", Size: 2, Value: "Seal"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestOFDSealDrawsConcurrentlyAndCloseWaitsForDraws(t *testing.T) {
	data := concurrentSealData(t)
	doc := &Document{}
	defer doc.Close()
	entry, err := doc.sealDocument(data)
	if err != nil || entry == nil {
		t.Fatalf("解析印章：%v", err)
	}
	info := &parser.SealInfo{
		SealData:   &parser.SealData{FileType: "ofd", Data: data},
		StampAnnot: &models.StampAnnot{Boundary: models.StBox{X: 4, Y: 5, Width: 10, Height: 10}},
	}
	box := models.StBox{Width: 30, Height: 30}
	entered, resume := make(chan struct{}, 2), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	var wg sync.WaitGroup
	defer func() { release(); wg.Wait() }()
	surfaces := make([]Backend, 2)
	for i := range surfaces {
		surfaces[i] = newOffscreenSurface(box.Width, box.Height, geom.DPI(96))
		if surfaces[i] == nil {
			t.Skip("离屏表面未注册")
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := &pausedSealContext{DrawContext: surfaces[i], entered: entered, resume: resume}
			if err := doc.drawOFDSeal(ctx, info, box); err != nil {
				t.Error(err)
			}
		}()
	}
	// 两个调用都必须进入嵌套 pageContent 的绘制，而不是轮流持有整枚印章的锁。
	awaitSealOperation(t, entered)
	awaitSealOperation(t, entered)
	closed := make(chan struct{})
	go func() { doc.Close(); close(closed) }()
	defer func() { release(); <-closed }()
	deadline := time.Now().Add(5 * time.Second)
	for entry.mu.TryRLock() {
		entry.mu.RUnlock()
		if time.Now().After(deadline) {
			t.Fatal("关闭没有等待在途绘制")
		}
		runtime.Gosched()
	}
	if entry.doc.closed.Load() {
		t.Fatal("在途绘制尚未完成，子文档已提前关闭")
	}
	release()
	wg.Wait()
	awaitSealOperation(t, closed)
	if !entry.closed || len(entry.ofd.Documents) != 0 {
		t.Fatal("绘制结束后没有释放印章子文档")
	}
	first, second := surfaces[0].Raster(), surfaces[1].Raster()
	if !bytes.Equal(first.Pix, second.Pix) {
		t.Fatal("并发绘制同一印章产生了不同图像")
	}
	var alpha uint64
	for i := 3; i < len(first.Pix); i += 4 {
		alpha += uint64(first.Pix[i])
	}
	if alpha == 0 {
		t.Fatal("并发绘制没有输出印章")
	}
}

func TestConcurrentSealsKeepRasterPDFPixelsAndText(t *testing.T) {
	data := concurrentSealData(t)
	var stamp bytes.Buffer
	if err := png.Encode(&stamp, sealQuadrantImage(40)); err != nil {
		t.Fatal(err)
	}
	render := func(parallel bool) ([]*image.RGBA, []byte) {
		doc := &Document{}
		defer doc.Close()
		box := models.StBox{Width: 30, Height: 30}
		surfaces := make([]VectorSurface, 4)
		errors := make([]error, len(surfaces))
		draw := func(i int) {
			surfaces[i], errors[i] = NewVectorSurface(box.Width, box.Height, func(ctx DrawContext) {
				ctx.SetFillColor(color.White)
				ctx.DrawPath(0, 0, geom.Rectangle(box.Width, box.Height))
				ofd := &parser.SealInfo{
					SealData:   &parser.SealData{FileType: "ofd", Data: data},
					StampAnnot: &models.StampAnnot{Boundary: models.StBox{X: 3, Y: 5, Width: 10, Height: 10}},
				}
				if i%2 == 1 {
					ofd.StampAnnot.Clip = models.StBox{Width: 10, Height: 10}
				}
				if err := doc.drawOFDSeal(ctx, ofd, box); err != nil {
					t.Error(err)
				}
				bitmap := &parser.SealInfo{
					SealData: &parser.SealData{FileType: "png", Data: stamp.Bytes()},
					StampAnnot: &models.StampAnnot{
						Boundary: models.StBox{X: 18, Y: 5, Width: 10, Height: 10},
						Clip:     models.StBox{X: float64(i%2) * 5, Width: 5, Height: 10},
					},
				}
				if err := doc.drawImageSeal(ctx, bitmap, box); err != nil {
					t.Error(err)
				}
				svg := &parser.SealInfo{
					SealData: &parser.SealData{FileType: "svg", Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="5" height="10" fill="red"/><rect x="5" width="5" height="10" fill="blue"/></svg>`)},
					StampAnnot: &models.StampAnnot{
						Boundary: models.StBox{X: 3, Y: 18, Width: 10, Height: 10},
						Clip:     models.StBox{X: float64(i%2) * 5, Width: 5, Height: 10},
					},
				}
				if err := doc.drawSVGSeal(ctx, svg, box); err != nil {
					t.Error(err)
				}
			})
		}
		var wg sync.WaitGroup
		for i := range surfaces {
			if parallel {
				wg.Add(1)
				go func() { defer wg.Done(); draw(i) }()
			} else {
				draw(i)
			}
		}
		wg.Wait()
		var output bytes.Buffer
		pdf, err := NewPDFDocument(&output, PDFOptions{SubsetFonts: true})
		if err != nil {
			t.Fatal(err)
		}
		images := make([]*image.RGBA, len(surfaces))
		for i, surface := range surfaces {
			if errors[i] != nil {
				t.Fatal(errors[i])
			}
			images[i] = surface.Rasterize(geom.DPI(96))
			if err := pdf.AddPage(surface, nil); err != nil {
				t.Fatal(err)
			}
		}
		if err := pdf.Close(); err != nil {
			t.Fatal(err)
		}
		return images, output.Bytes()
	}
	serialImages, serialPDF := render(false)
	parallelImages, parallelPDF := render(true)
	for i := range serialImages {
		if !bytes.Equal(serialImages[i].Pix, parallelImages[i].Pix) {
			t.Fatalf("第%d页印章并发绘制改变了光栅输出", i+1)
		}
	}
	t.Run("PDFPixels", func(t *testing.T) {
		tool, err := exec.LookPath("pdftoppm")
		if err != nil {
			t.Skip("缺少 pdftoppm")
		}
		serial, err := testutil.RasterizePDFPages(t.Context(), tool, serialPDF, 96)
		if err != nil {
			t.Fatal(err)
		}
		parallel, err := testutil.RasterizePDFPages(t.Context(), tool, parallelPDF, 96)
		if err != nil {
			t.Fatal(err)
		}
		if len(serial) != 4 || len(parallel) != 4 {
			t.Fatal("PDF 丢失页面")
		}
		for i := range serial {
			if !bytes.Equal(serial[i].Pix, parallel[i].Pix) {
				t.Fatalf("第%d页印章并发绘制改变了 PDF 栅格", i+1)
			}
		}
	})
	t.Run("PDFText", func(t *testing.T) {
		tool, err := exec.LookPath("pdftotext")
		if err != nil {
			t.Skip("缺少 pdftotext")
		}
		path := filepath.Join(t.TempDir(), "seals.pdf")
		if err := os.WriteFile(path, parallelPDF, 0o600); err != nil {
			t.Fatal(err)
		}
		text, err := exec.CommandContext(t.Context(), tool, path, "-").CombinedOutput()
		if err != nil || strings.Count(string(text), "Seal") != 2 {
			t.Fatalf("两页矢量印章丢失可提取文字：%v %s", err, text)
		}
	})
}

func TestSealImageSharesPreparedRasterAndImageCacheBudget(t *testing.T) {
	// 4x4 图片恰好占 64 字节，缓存只容纳一张；白底与墨迹便于核对预处理结果。
	doc := &Document{images: utils.NewWeightedLRU[string, image.Image](imageCacheCapacity, 64, nil)}
	defer doc.Close()
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			src.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	src.SetRGBA(1, 1, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, src); err != nil {
		t.Fatal(err)
	}
	results := make([]image.Image, 8)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var err error
			// 不同字节切片、相同内容也应共享同一枚印章的解码结果。
			results[i], err = doc.decodeSealImage(bytes.Clone(encoded.Bytes()))
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, result := range results {
		if result == nil || result != results[0] {
			t.Fatal("相同内容的印章未复用预处理后的位图")
		}
	}
	if _, _, _, alpha := results[0].At(0, 0).RGBA(); alpha != 0 {
		t.Fatal("缓存印章仍带不透明白底")
	}
	if r, _, _, alpha := results[0].At(1, 1).RGBA(); r != 65535 || alpha != 65535 {
		t.Fatal("缓存印章丢失了红色墨迹")
	}
	if doc.images.Len() != 1 || doc.images.Weight() != 64 || len(doc.imageLocks) != 0 {
		t.Fatal("印章没有进入文档图片预算，或保留了无用加载锁")
	}
	// 普通资源图片与印章共用预算，资源图进入后应淘汰旧印章。
	if _, err := doc.cachedImage("resource.png", func() (image.Image, error) { return src, nil }); err != nil {
		t.Fatal(err)
	}
	again, err := doc.decodeSealImage(encoded.Bytes())
	if err != nil || again == results[0] || doc.images.Len() != 1 || doc.images.Weight() > 64 {
		t.Fatalf("图片预算淘汰或重新加载印章失败：%v", err)
	}
	doc.Close()
	if doc.images != nil {
		t.Fatal("关闭后仍保留印章位图缓存")
	}
	if _, err := doc.decodeSealImage(encoded.Bytes()); err == nil || len(doc.imageLocks) != 0 {
		t.Fatal("关闭后重新加载了印章或保留加载锁")
	}
}

func TestSlowImageLoadDoesNotBlockOtherImagesOrRefillAfterClose(t *testing.T) {
	doc := &Document{}
	defer doc.Close()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	if _, err := doc.cachedImage("hot", func() (image.Image, error) { return img, nil }); err != nil {
		t.Fatal(err)
	}
	entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	go func() {
		defer close(done)
		if _, err := doc.cachedImage("slow", func() (image.Image, error) {
			close(entered)
			<-resume
			return img, nil
		}); err != nil {
			t.Error(err)
		}
	}()
	defer func() { release(); <-done }()
	awaitSealOperation(t, entered)
	available := make(chan struct{})
	go func() {
		defer close(available)
		if got, err := doc.cachedImage("hot", func() (image.Image, error) {
			t.Error("缓存命中重复加载")
			return img, nil
		}); err != nil || got != img {
			t.Error("慢加载阻塞或破坏了其他缓存命中")
		}
		doc.Close()
	}()
	awaitSealOperation(t, available)
	release()
	awaitSealOperation(t, done)
	if doc.images != nil || len(doc.imageLocks) != 0 {
		t.Fatal("关闭后在途图片解码重新填回缓存或保留加载锁")
	}
}
