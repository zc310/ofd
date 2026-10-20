package render

import (
	"image"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

type templateTestFonts struct {
	fakeFontEngine

	generation atomic.Uint64
}

type templateClosingFonts struct {
	templateTestFonts

	closing chan struct{}
}

func (f *templateClosingFonts) Close() { close(f.closing) }

func (f *templateTestFonts) CacheGeneration() uint64 { return f.generation.Load() }

type templateTestSurface struct{ weight int64 }

func (*templateTestSurface) Width() float64                        { return 20 }
func (*templateTestSurface) Height() float64                       { return 20 }
func (*templateTestSurface) Write(io.Writer, string) error         { return nil }
func (*templateTestSurface) Rasterize(geom.Resolution) *image.RGBA { return nil }
func (s *templateTestSurface) Weight() int64                       { return s.weight }

type templateTestDrawer struct {
	DrawContext

	surface VectorSurface
}

func (*templateTestDrawer) TemplateSurfaces() bool { return true }
func (d *templateTestDrawer) DrawTemplateSurface(surface VectorSurface) bool {
	d.surface = surface
	return true
}

func TestTemplateCacheReusesSurfaceConcurrentlyAndInvalidates(t *testing.T) {
	previous := newVectorSurface
	t.Cleanup(func() { newVectorSurface = previous })
	var builds atomic.Int32
	newVectorSurface = func(_, _ float64, _ func(DrawContext)) VectorSurface {
		builds.Add(1)
		return &templateTestSurface{weight: 1024}
	}
	fonts := &templateTestFonts{}
	doc := &Document{fonts: fonts}
	t.Cleanup(doc.Close)
	content := &models.PageContent{Content: &models.Content{}}
	box := models.StBox{Width: 20, Height: 20}
	const workers = 8
	var wg sync.WaitGroup
	surfaces := make([]VectorSurface, workers)
	for i := range surfaces {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var budget renderBudget
			budget.reset()
			ctx := &templateTestDrawer{}
			if !doc.drawCachedTemplate(ctx, content, box, &budget) {
				t.Error("template was not cached")
			}
			surfaces[i] = ctx.surface
		}()
	}
	wg.Wait()
	if builds.Load() != 1 {
		t.Fatalf("builds=%d, want 1", builds.Load())
	}
	for _, surface := range surfaces {
		if surface != surfaces[0] {
			t.Fatal("concurrent callers did not share immutable surface")
		}
	}
	var budget renderBudget
	budget.reset()
	ctx := &templateTestDrawer{}
	fonts.generation.Add(1)
	if !doc.drawCachedTemplate(ctx, content, box, &budget) || ctx.surface == surfaces[0] {
		t.Fatal("font generation did not invalidate template")
	}
	if doc.templateScenes.Len() != 1 {
		t.Fatal("font generation change retained superseded template surfaces")
	}
	box.Height = 30
	if !doc.drawCachedTemplate(ctx, content, box, &budget) || builds.Load() != 3 {
		t.Fatal("page dimensions were omitted from template key")
	}
	content.PageRes = []models.StLoc{"PageRes.xml"}
	if doc.drawCachedTemplate(ctx, content, box, &budget) {
		t.Fatal("template with page resources should draw directly")
	}
	content.PageRes = nil
	budget.templateCacheable = false
	if doc.drawCachedTemplate(ctx, content, box, &budget) {
		t.Fatal("page with private resources should draw templates directly")
	}
	doc.Close()
	if doc.templateScenes != nil {
		t.Fatal("Close retained template surfaces")
	}
}

func TestTemplateCacheBoundsRetentionAndRejectsStaleBuild(t *testing.T) {
	previous := newVectorSurface
	t.Cleanup(func() { newVectorSurface = previous })
	fonts := &templateTestFonts{}
	doc := &Document{fonts: fonts}
	t.Cleanup(doc.Close)
	weight := int64(2 << 20)
	newVectorSurface = func(_, _ float64, _ func(DrawContext)) VectorSurface {
		return &templateTestSurface{weight: weight}
	}
	var budget renderBudget
	budget.reset()
	ctx := &templateTestDrawer{}
	box := models.StBox{Width: 20, Height: 20}
	for range 40 {
		if !doc.drawCachedTemplate(ctx, &models.PageContent{Content: &models.Content{}}, box, &budget) {
			t.Fatal("template drawing failed")
		}
	}
	if doc.templateScenes.Len() > 32 || doc.templateScenes.Weight() > 32<<20 {
		t.Fatal("cache exceeded capacity or byte limit")
	}
	before := doc.templateScenes.Len()
	newVectorSurface = func(_, _ float64, _ func(DrawContext)) VectorSurface {
		fonts.generation.Add(1)
		return &templateTestSurface{weight: weight}
	}
	if doc.drawCachedTemplate(ctx, &models.PageContent{Content: &models.Content{}}, box, &budget) || doc.templateScenes.Len() != before {
		t.Fatal("font generation changed during build but stale surface was published")
	}
}

func TestTemplateCacheHitStillConsumesPageBudget(t *testing.T) {
	fonts := &templateTestFonts{}
	doc := &Document{fonts: fonts}
	t.Cleanup(doc.Close)
	content := &models.PageContent{Content: &models.Content{}}
	box := models.StBox{Width: 20, Height: 20}
	previous := newVectorSurface
	t.Cleanup(func() { newVectorSurface = previous })
	newVectorSurface = func(_, _ float64, _ func(DrawContext)) VectorSurface { return &templateTestSurface{weight: 1024} }
	var budget renderBudget
	budget.reset()
	ctx := &templateTestDrawer{}
	if !doc.drawCachedTemplate(ctx, content, box, &budget) {
		t.Fatal("template build failed")
	}
	entry := doc.templateScenes.Values()[0]
	entry.usage.compositeExpansions = 2
	entry.usage.compositeResources[7] = 2
	entry.usage.patternTiles = 3
	entry.usage.offscreenPixels = 4
	if !doc.drawCachedTemplate(ctx, content, box, &budget) || budget.compositeExpansions != 2 || budget.compositeResources[7] != 2 || budget.patternTiles != 3 || budget.offscreenPixels != 4 {
		t.Fatal("cache hit bypassed resource accounting")
	}
	for _, exhausted := range []renderBudget{
		{compositeExpansions: maxCompositeExpansions},
		{compositeResources: map[models.StID]int{7: maxCompositeResourceExpansions}},
		{patternTiles: maxPatternTilesPerRender},
		{offscreenPixels: maxOffscreenPixels},
	} {
		exhausted.templateCacheable = true
		if doc.drawCachedTemplate(ctx, content, box, &exhausted) {
			t.Error("cache hit ignored exhausted page budget")
		}
	}
}

func TestTemplateCacheCloseDoesNotPublishInFlightBuild(t *testing.T) {
	previous := newVectorSurface
	t.Cleanup(func() { newVectorSurface = previous })
	building, release := make(chan struct{}), make(chan struct{})
	newVectorSurface = func(_, _ float64, _ func(DrawContext)) VectorSurface {
		close(building)
		<-release
		return &templateTestSurface{weight: 1024}
	}
	fonts := &templateClosingFonts{closing: make(chan struct{})}
	doc := &Document{fonts: fonts}
	result := make(chan bool, 1)
	go func() {
		var budget renderBudget
		budget.reset()
		result <- doc.drawCachedTemplate(&templateTestDrawer{}, &models.PageContent{Content: &models.Content{}}, models.StBox{Width: 20, Height: 20}, &budget)
	}()
	<-building
	closed := make(chan struct{})
	go func() { doc.Close(); close(closed) }()
	<-fonts.closing
	close(release)
	if <-result {
		t.Fatal("in-flight build was reused after Close started")
	}
	<-closed
	if doc.templateScenes != nil {
		t.Fatal("in-flight build republished cache after Close")
	}
}
