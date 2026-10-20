package webreader

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/creator"
)

func imageViewTestPackage(t *testing.T) []byte {
	t.Helper()
	pixel := image.NewRGBA(image.Rect(0, 0, 1, 1))
	pixel.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixel); err != nil {
		t.Fatal(err)
	}
	data, err := creator.Marshal(creator.Document{ID: "view-lifecycle", Pages: []creator.Page{{
		Area: &creator.PageArea{PhysicalBox: &creator.Box{Width: 20, Height: 20}},
		Items: []creator.Item{creator.Image{
			X: 5, Y: 5, Width: 10, Height: 10, Format: "PNG", Data: encoded.Bytes(),
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPageDocumentStaysOpenWhileRenderingAcrossCacheEviction(t *testing.T) {
	r, err := Open(imageViewTestPackage(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.mu.RLock()
	defer r.mu.RUnlock()
	view, err := r.pageDocument(r.pages[0], color.White, geom.DPI(25.4))
	if err != nil {
		t.Fatal(err)
	}
	defer r.releasePageDocument(view)
	// 固定交错：A 已取出视图但尚未绘制，其他请求依次插入不同背景的视图。
	for i := 0; i < maxRenderDocs+1; i++ {
		background := color.RGBA{R: uint8(i + 1), A: 255}
		other, err := r.pageDocument(r.pages[0], background, geom.DPI(25.4))
		if err != nil {
			t.Fatal(err)
		}
		r.releasePageDocument(other)
	}
	page, err := view.PageWithVisibility(r.pages[0].page, r.hiddenLayers[0])
	if err != nil {
		t.Fatal(err)
	}
	got := page.Rasterize(geom.DPI(25.4)).RGBAAt(10, 10)
	if want := (color.RGBA{R: 255, A: 255}); got != want {
		t.Fatalf("视图仍在使用时被关闭，图片像素=%v，期望 %v", got, want)
	}
}

func TestPageDocumentReleasesConcurrentUsersAndTrimsCache(t *testing.T) {
	r, err := Open(imageViewTestPackage(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.mu.RLock()
	defer r.mu.RUnlock()
	var views []*render.Document
	for i := 0; i < maxRenderDocs+2; i++ {
		view, err := r.pageDocument(r.pages[0], color.White, geom.DPI(float64(30+i)))
		if err != nil {
			t.Fatal(err)
		}
		views = append(views, view)
	}
	if got := r.renderDocs.Len(); got != len(views) {
		t.Fatalf("正在使用的视图被淘汰：缓存数量=%d，期望 %d", got, len(views))
	}
	// 同一视图两个使用者：释放一次后仍要保护另一使用者。
	same, err := r.pageDocument(r.pages[0], color.White, geom.DPI(30))
	if err != nil {
		t.Fatal(err)
	}
	if same != views[0] {
		t.Fatal("正在使用的视图没有复用")
	}
	r.releasePageDocument(same)
	for _, view := range views {
		probe, err := view.NewView(color.White, geom.DPI(25.4))
		if err != nil {
			t.Fatalf("仍在使用的视图被提前关闭：%v", err)
		}
		probe.Close()
	}
	for _, view := range views {
		r.releasePageDocument(view)
	}
	if got := r.renderDocs.Len(); got != maxRenderDocs || len(r.renderDocUsers) != 0 {
		t.Fatalf("使用结束后缓存未收敛：视图=%d，持有记录=%d", got, len(r.renderDocUsers))
	}
	if probe, err := views[0].NewView(color.White, geom.DPI(25.4)); err == nil {
		probe.Close()
		t.Fatal("最后一次使用结束后，被淘汰的视图没有关闭")
	}
}

func TestReleasePageDocumentHandlesMissingCacheAndUnheldViews(t *testing.T) {
	var absent *Reader
	absent.releasePageDocument(nil)
	view := &render.Document{}
	zero := &Reader{}
	zero.releasePageDocument(nil)
	zero.releasePageDocument(view)
	if len(zero.renderDocUsers) != 0 {
		t.Fatal("释放未持有的视图创建了计数记录")
	}
	for _, users := range []int{-1, 0, 1} {
		r := &Reader{renderDocUsers: map[*render.Document]int{view: users}}
		r.releasePageDocument(view)
		r.releasePageDocument(view)
		if len(r.renderDocUsers) != 0 {
			t.Fatalf("初始计数=%d，释放后仍保留无效记录：%v", users, r.renderDocUsers)
		}
	}
}

func TestRepeatedPageDocumentReleaseDoesNotPreventEviction(t *testing.T) {
	r, err := Open(imageViewTestPackage(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.mu.RLock()
	defer r.mu.RUnlock()
	view, err := r.pageDocument(r.pages[0], color.White, geom.DPI(30))
	if err != nil {
		t.Fatal(err)
	}
	r.releasePageDocument(view)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.releasePageDocument(view)
		}()
	}
	wg.Wait()
	for i := 0; i < maxRenderDocs; i++ {
		other, err := r.pageDocument(r.pages[0], color.White, geom.DPI(float64(31+i)))
		if err != nil {
			t.Fatal(err)
		}
		r.releasePageDocument(other)
	}
	if len(r.renderDocUsers) != 0 || r.renderDocs.Len() != maxRenderDocs {
		t.Fatal("重复释放留下负计数或阻止缓存收敛")
	}
	if probe, err := view.NewView(color.White, geom.DPI(25.4)); err == nil {
		probe.Close()
		t.Fatal("重复释放导致旧视图永久不可淘汰")
	}
}

// 第一次 RGBA 用于缓存键，第二次在默认 canvas 后端绘制背景时阻塞。
type blockingViewBackground struct {
	calls   atomic.Int32
	entered chan struct{}
	resume  chan struct{}
}

func (c *blockingViewBackground) RGBA() (uint32, uint32, uint32, uint32) {
	if c.calls.Add(1) == 2 {
		close(c.entered)
		<-c.resume
	}
	return color.White.RGBA()
}

func TestConcurrentRenderKeepsViewUntilOutputCompletes(t *testing.T) {
	for _, format := range []string{"png", "svg", "pdf"} {
		t.Run(format, func(t *testing.T) {
			r, err := Open(imageViewTestPackage(t))
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			background := &blockingViewBackground{entered: make(chan struct{}), resume: make(chan struct{})}
			var once sync.Once
			resume := func() { once.Do(func() { close(background.resume) }) }
			defer resume()
			type result struct {
				data []byte
				err  error
			}
			done := make(chan result, 1)
			go func() {
				options := RenderOptions{DPI: 25.4, Background: background, Format: RenderFormat(format)}
				var data []byte
				var err error
				if format == "pdf" {
					data, err = r.RenderPDF([]int{0}, options)
				} else {
					data, err = r.RenderPage(0, options)
				}
				done <- result{data, err}
			}()
			select {
			case <-background.entered:
			case <-time.After(10 * time.Second):
				t.Fatal("渲染没有进入背景绘制")
			}
			for i := 0; i < maxRenderDocs+1; i++ {
				if _, err := r.RenderPage(0, RenderOptions{DPI: 25.4, Background: color.RGBA{B: uint8(i + 1), A: 255}}); err != nil {
					t.Fatal(err)
				}
			}
			resume()
			var output result
			select {
			case output = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("渲染没有结束")
			}
			if output.err != nil || len(output.data) == 0 {
				t.Fatalf("输出失败：%v", output.err)
			}
			if format == "png" {
				img, err := png.Decode(bytes.NewReader(output.data))
				if err != nil {
					t.Fatal(err)
				}
				if got := color.RGBAModel.Convert(img.At(10, 10)); got != (color.RGBA{R: 255, A: 255}) {
					t.Errorf("并发淘汰导致图片丢失：%v", got)
				}
			} else if format == "svg" && !bytes.Contains(output.data, []byte("<image")) {
				t.Error("并发淘汰导致 SVG 图片丢失")
			} else if format == "pdf" {
				if !bytes.HasPrefix(output.data, []byte("%PDF-")) || !regexp.MustCompile(`/Subtype\s*/Image\b`).Match(output.data) {
					t.Error("PDF 输出无效或图片丢失")
				}
			}
			r.renderDocsMu.Lock()
			users, count := len(r.renderDocUsers), r.renderDocs.Len()
			r.renderDocsMu.Unlock()
			if users != 0 || count > maxRenderDocs {
				t.Errorf("输出完成后持有记录=%d，缓存数量=%d", users, count)
			}
		})
	}
}
