package render

import (
	"bytes"
	"fmt"
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

func TestSealImageSharesPreparedRasterAndResourceCache(t *testing.T) {
	// 白底与墨迹便于核对预处理结果；印章与普通资源必须共用有界图片缓存。
	doc := &Document{}
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
	// 默认图片缓存最多保留 128 项。填满普通资源后，最旧的印章也应被淘汰。
	for i := range 128 {
		if _, err := doc.cachedImage(fmt.Sprintf("resource-%d.png", i), func() (image.Image, error) { return src, nil }); err != nil {
			t.Fatal(err)
		}
	}
	again, err := doc.decodeSealImage(encoded.Bytes())
	if err != nil || again == results[0] {
		t.Fatalf("图片预算淘汰或重新加载印章失败：%v", err)
	}
	doc.Close()
	if _, err := doc.decodeSealImage(encoded.Bytes()); err == nil {
		t.Fatal("关闭后重新加载了印章")
	}
}

func TestSealDocumentCacheEvictsLeastRecentlyUsedAndWaitsForDrawing(t *testing.T) {
	setFontEngineFactory(t, func(*parser.Document) FontEngine { return &closeTrackingFont{} })
	doc := &Document{}
	defer doc.Close()
	data := make([][]byte, sealDocCacheCapacity+2)
	entries := make([]*sealDocEntry, sealDocCacheCapacity)
	for i := range data {
		var err error
		data[i], err = creator.Marshal(creator.Document{ID: fmt.Sprintf("seal-cache-%d", i), Pages: []creator.Page{{}}})
		if err != nil {
			t.Fatal(err)
		}
		if i < len(entries) {
			entries[i], err = doc.sealDocument(data[i])
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if doc.sealDocs.Len() != sealDocCacheCapacity {
		t.Fatal("印章未进入有界缓存")
	}
	if hot, err := doc.sealDocument(data[0]); err != nil || hot != entries[0] {
		t.Fatal("缓存命中未复用印章")
	}
	if _, err := doc.sealDocument(data[sealDocCacheCapacity]); err != nil {
		t.Fatal(err)
	}
	if entries[0].closed || !entries[1].closed || entries[1].doc.fonts.(*closeTrackingFont).closes.Load() != 1 {
		t.Fatal("未按最近使用顺序释放旧印章及其字体")
	}
	// 将一个条目固定为正在绘制，再把它变为最久未使用的缓存条目。
	pinned, err := doc.loadSealDocument(data[2], true)
	if err != nil {
		t.Fatal(err)
	}
	unlock := sync.OnceFunc(pinned.mu.RUnlock)
	defer unlock()
	for i := range sealDocCacheCapacity + 1 {
		if i == 1 || i == 2 {
			continue
		}
		if _, err := doc.sealDocument(data[i]); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := doc.sealDocument(data[sealDocCacheCapacity+1]); err != nil {
			t.Error(err)
		}
	}()
	defer func() { unlock(); <-done }()
	deadline := time.Now().Add(5 * time.Second)
	for pinned.mu.TryRLock() {
		pinned.mu.RUnlock()
		if time.Now().After(deadline) {
			t.Fatal("淘汰未等待在途印章绘制")
		}
		runtime.Gosched()
	}
	if pinned.doc.closed.Load() {
		t.Fatal("正在绘制的印章被提前关闭")
	}
	unlock()
	awaitSealOperation(t, done)
	if !pinned.closed || doc.sealDocs.Len() != sealDocCacheCapacity {
		t.Fatal("绘制结束后未释放被淘汰条目或缓存超过上限")
	}
	doc.Close()
	for _, entry := range entries {
		if entry.doc.fonts.(*closeTrackingFont).closes.Load() != 1 {
			t.Fatal("印章字体没有恰好释放一次")
		}
	}
}
