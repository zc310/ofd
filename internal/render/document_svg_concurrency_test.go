package render

import (
	"bytes"
	"image"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/creator"
)

type checkedSVGScene struct {
	fakeSVGScene

	vector func()
	raster func(geom.Resolution) image.Image
}

func (s *checkedSVGScene) RenderVector(any, geom.Matrix) bool {
	if s.vector != nil {
		s.vector()
	}
	return true
}

func (s *checkedSVGScene) Rasterize(resolution geom.Resolution) image.Image {
	return s.raster(resolution)
}

func TestDifferentSVGResourcesEmbedConcurrently(t *testing.T) {
	previous := svgSceneFactory
	defer func() { svgSceneFactory = previous }()
	entered, resume := make(chan struct{}, 2), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	svgSceneFactory = func([]byte) (SVGScene, error) {
		return &checkedSVGScene{fakeSVGScene: fakeSVGScene{w: 10, h: 10}, vector: func() {
			entered <- struct{}{}
			<-resume
		}}, nil
	}
	data, err := creator.Marshal(creator.Document{
		ID: "svg-concurrency",
		Media: []creator.Media{
			{ID: 30, Type: "Image", Format: "SVG", Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10" fill="red"/></svg>`)},
			{ID: 31, Type: "Image", Format: "SVG", Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10" fill="blue"/></svg>`)},
		},
		Pages: []creator.Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := NewDocument(nil, ofd.Documents[0])
	defer doc.Close()
	var wg sync.WaitGroup
	defer func() { release(); wg.Wait() }()
	for _, id := range []models.StRefID{30, 31} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := NewVectorSurface(20, 20, func(ctx DrawContext) {
				doc.Image(ctx, models.ImageObject{
					CtImage: models.CtImage{
						CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 10, Height: 10}},
						ResourceID:    id,
					},
				}, nil, models.StBox{Width: 20, Height: 20})
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	awaitSealOperation(t, entered)
	awaitSealOperation(t, entered)
}

func TestSVGSceneSynchronizesRasterAndVectorTogether(t *testing.T) {
	previous := svgSceneFactory
	defer func() { svgSceneFactory = previous }()
	var active atomic.Int32
	check := func() {
		if active.Add(1) != 1 {
			t.Error("同一 SVG 的矢量嵌入与栅格化同时修改共享状态")
		}
		runtime.Gosched()
		active.Add(-1)
	}
	svgSceneFactory = func([]byte) (SVGScene, error) {
		return &checkedSVGScene{vector: check, raster: func(geom.Resolution) image.Image { check(); return nil }}, nil
	}
	scene, err := parseSVGScene(nil)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(2)
		go func() { defer wg.Done(); scene.RenderVector(nil, geom.Identity) }()
		go func() { defer wg.Done(); scene.Rasterize(geom.DPI(96)) }()
	}
	wg.Wait()
}

func TestSVGSealCachesSceneAndRasterByResolution(t *testing.T) {
	previous := svgSceneFactory
	defer func() { svgSceneFactory = previous }()
	var parses, rasters atomic.Int32
	svgSceneFactory = func([]byte) (SVGScene, error) {
		parses.Add(1)
		return &checkedSVGScene{fakeSVGScene: fakeSVGScene{w: 10, h: 10}, raster: func(resolution geom.Resolution) image.Image {
			rasters.Add(1)
			return sealQuadrantImage(int(10 * resolution.DPMM()))
		}}, nil
	}
	doc := &Document{}
	defer doc.Close()
	data := []byte("svg-seal")
	box := models.StBox{Width: 10, Height: 10}
	images := make([]image.Image, 8)
	var wg sync.WaitGroup
	for i := range images {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var err error
			images[i], err = doc.decodeSVGSealImage(data, box)
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, img := range images {
		if img == nil || img != images[0] {
			t.Fatal("相同 SVG 印章没有共享完整位图")
		}
	}
	if parses.Load() != 1 || rasters.Load() != 1 {
		t.Fatalf("重复解析或栅格化：解析%d次，栅格化%d次", parses.Load(), rasters.Load())
	}
	box.X, box.Y = 20, 30
	if img, err := doc.decodeSVGSealImage(data, box); err != nil || img != images[0] {
		t.Fatal("页面位置改变导致印章位图失去复用")
	}
	box.Width, box.Height = 20, 20
	other, err := doc.decodeSVGSealImage(data, box)
	if err != nil || other == images[0] || other.Bounds() == images[0].Bounds() || parses.Load() != 1 || rasters.Load() != 2 {
		t.Fatalf("不同实际分辨率共用了位图或重复解析场景：%v", err)
	}
	if doc.images.Len() != 2 || doc.svgCanvases.Len() != 1 || len(doc.imageLocks) != 0 {
		t.Fatal("SVG 印章没有复用有界图片/场景缓存，或保留加载锁")
	}
	doc.Close()
	if doc.images != nil || doc.svgCanvases != nil {
		t.Fatal("关闭后保留 SVG 印章缓存")
	}
	if _, err := doc.decodeSVGSealImage(data, box); err == nil {
		t.Fatal("关闭后重新加载 SVG 印章")
	}
}

func TestCloseDuringSVGParseDoesNotRefillSceneCache(t *testing.T) {
	previous := svgSceneFactory
	defer func() { svgSceneFactory = previous }()
	entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	svgSceneFactory = func([]byte) (SVGScene, error) {
		close(entered)
		<-resume
		return fakeSVGScene{w: 10, h: 10}, nil
	}
	doc := &Document{}
	defer doc.Close()
	go func() {
		defer close(done)
		if _, err := doc.cachedSVGScene("slow.svg", func() ([]byte, error) { return nil, nil }); err != nil {
			t.Error(err)
		}
	}()
	defer func() { release(); <-done }()
	awaitSealOperation(t, entered)
	doc.Close()
	release()
	awaitSealOperation(t, done)
	if doc.svgCanvases != nil || len(doc.imageLocks) != 0 {
		t.Fatal("关闭后在途 SVG 解析重新填充了场景缓存")
	}
}

func TestSVGVectorEmbeddingKeepsParallelPixelsAndVectorOutput(t *testing.T) {
	data, err := creator.Marshal(creator.Document{
		ID: "svg-vector-parallel", PageSize: creator.PageSize{Width: 20, Height: 20},
		Media: []creator.Media{{ID: 30, Type: "Image", Format: "SVG", Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><defs><linearGradient id="g"><stop stop-color="red"/><stop offset="1" stop-color="blue"/></linearGradient></defs><rect width="10" height="10" fill="url(#g)"/></svg>`)}},
		Pages: []creator.Page{{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := NewDocument(nil, ofd.Documents[0])
	defer doc.Close()
	draw := func() VectorSurface {
		surface, err := NewVectorSurface(20, 20, func(ctx DrawContext) {
			doc.Image(ctx, models.ImageObject{CtImage: models.CtImage{
				CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 2, Y: 2, Width: 10, Height: 10}},
				ResourceID:    30,
			}}, nil, models.StBox{Width: 20, Height: 20})
		})
		if err != nil {
			t.Error(err)
		}
		return surface
	}
	baseline := draw()
	var svg bytes.Buffer
	if err := baseline.Write(&svg, "svg"); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(svg.Bytes(), []byte("<image")) || !bytes.Contains(svg.Bytes(), []byte("<linearGradient")) {
		t.Fatal("生产后端的 SVG 嵌入未保留矢量渐变")
	}
	want := baseline.Rasterize(geom.DPI(96))
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			surface := draw()
			if surface != nil && !bytes.Equal(want.Pix, surface.Rasterize(geom.DPI(96)).Pix) {
				t.Error("SVG 并发矢量嵌入改变了像素")
			}
		}()
	}
	wg.Wait()
}
