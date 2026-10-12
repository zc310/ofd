package render

import (
	"errors"
	"image"
	"image/color"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/canvas/renderers/rasterizer"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
)

func renderRegressionDocument(t *testing.T, input creator.Document) image.Image {
	t.Helper()
	data, err := creator.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := NewDocument(color.Transparent, ofd.Documents[0])
	defer doc.Close()
	page := ofd.Documents[0].Pages[0]
	box, err := page.PhysicalBox()
	if err != nil {
		t.Fatal(err)
	}
	c := canvas.New(box.Width, box.Height)
	doc.PageContent(newCanvasBackend(canvas.NewContext(c)), page, false)
	return rasterizer.Draw(c, canvas.DPI(96), canvas.DefaultColorSpace)
}

func TestTemplateZOrderControlsPageStacking(t *testing.T) {
	red := creator.Path{Width: 20, Height: 20, Data: "M 0 0 L 20 0 L 20 20 L 0 20 C", Fill: true, FillColor: &creator.Color{R: 255}}
	black := red
	black.FillColor = &creator.Color{}
	for _, tc := range []struct {
		name, definition, reference string
		wantRed                     bool
	}{
		{"foreground reference", "Background", "Foreground", true},
		{"background overrides definition", "Foreground", "Background", false},
		{"inherit foreground", "Foreground", "", true},
		{"default background", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := renderRegressionDocument(t, creator.Document{
				ID: "template-order", PageSize: creator.PageSize{Width: 20, Height: 20},
				Templates: []creator.TemplatePage{{ID: 10, ZOrder: tc.definition, Items: []creator.Item{red}}},
				Pages:     []creator.Page{{Templates: []creator.TemplateRef{{ID: 10, ZOrder: tc.reference}}, Items: []creator.Item{black}}},
			})
			r, g, b, _ := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA()
			if (r>>8 > 200) != tc.wantRed || g != 0 || b != 0 {
				t.Fatalf("pixel=%d,%d,%d, want red=%v", r>>8, g>>8, b>>8, tc.wantRed)
			}
		})
	}
}

func TestPathStrokeMultipliesObjectAndColorAlpha(t *testing.T) {
	for _, tc := range []struct {
		name          string
		object, color uint8
		explicit      bool
		want          uint8
	}{
		{"transparent default black", 0, 255, false, 0},
		{"half default black", 128, 255, false, 128},
		{"half explicit", 128, 128, true, 64},
		{"opaque explicit", 255, 128, true, 128},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := models.CtPath{CTGraphicUnit: models.CTGraphicUnit{Alpha: &tc.object}}
			object.Stroke.Set(true)
			if tc.explicit {
				object.StrokeColor = &models.CTColor{Value: &models.Color{RGBA: color.RGBA{A: 255}}, Alpha: &tc.color}
			}
			var doc Document
			ctx := canvas.NewContext(canvas.New(20, 20))
			doc.updateCtPathStyle(newCanvasBackend(ctx), &object, nil, 1)
			if got := ctx.Style.Stroke.Color.A; got != tc.want {
				t.Fatalf("stroke alpha=%d, want %d", got, tc.want)
			}
		})
	}
}

func TestSVGImageRespectsAlphaMaskAndBorder(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="20mm" height="20mm" viewBox="0 0 20 20"><rect width="20" height="20" fill="red"/></svg>`)
	for _, tc := range []struct {
		name         string
		alpha        uint8
		mask, border bool
		want         uint8
	}{
		{"transparent", 0, false, false, 0},
		{"half", 128, false, false, 128},
		{"opaque vector", 255, false, false, 255},
		{"mask", 255, true, false, 0},
		{"border vector", 255, false, true, 255},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := creator.Image{X: 2, Y: 2, Width: 16, Height: 16, ResourceID: 90, Alpha: &tc.alpha}
			media := []creator.Media{{ID: 90, Type: "Image", Format: "SVG", Data: svg}}
			if tc.mask {
				object.ImageMask = 91
				media = append(media, creator.Media{ID: 91, Type: "Image", Format: "PNG", Data: solidPNG(t, 2, 2, color.NRGBA{A: 255})})
			}
			if tc.border {
				object.Border = &creator.ImageBorder{LineWidth: 2, Color: &creator.Color{B: 255}}
			}
			img := renderRegressionDocument(t, creator.Document{ID: "svg-properties", PageSize: creator.PageSize{Width: 20, Height: 20}, Media: media, Pages: []creator.Page{{Items: []creator.Item{object}}}})
			_, _, _, a := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA()
			if got := uint8(a >> 8); got != tc.want {
				t.Fatalf("SVG alpha=%d, want %d", got, tc.want)
			}
			if tc.border {
				x := img.Bounds().Dx() / 10
				_, _, blue, _ := img.At(x, img.Bounds().Dy()/2).RGBA()
				if blue>>8 < 200 {
					t.Fatal("SVG border missing")
				}
			}
		})
	}
}

func TestCompositeAlphaReusesColorsAndMultipliesChildAlpha(t *testing.T) {
	parent, child := uint8(128), uint8(128)
	fill := &models.CTColor{Value: &models.Color{RGBA: color.RGBA{R: 255, A: 255}}}
	items := []models.PageItem{{Kind: models.PageItemPath, Path: &models.PathObject{CtPath: models.CtPath{CTGraphicUnit: models.CTGraphicUnit{Alpha: &child}, Fill: true, FillColor: fill}}}}
	var doc Document
	for range 100 {
		copies, ok := copyCompositeVectorItems(items, &parent, 0)
		if !ok {
			t.Fatal("copy failed")
		}
		path := copies[0].Path
		if path.FillColor != fill || *path.Alpha != 64 {
			t.Fatal("composite must reuse color and multiply alpha")
		}
		doc.updateCtColor(path.FillColor)
	}
	if child != 128 || fill.Value.RGBA.A != 255 {
		t.Fatal("resource model mutated")
	}
	if got := doc.ctColorCache.Len(); got != 1 {
		t.Fatalf("color entries=%d, want 1", got)
	}
}

func TestColorCacheIsBoundedAndReturnsIndependentPaint(t *testing.T) {
	var doc Document
	source := &models.CTColor{Value: &models.Color{RGBA: color.RGBA{A: 255}}}
	first := doc.updateCtColor(source)
	first.Value = color.RGBA{R: 255, A: 255}
	if second := doc.updateCtColor(source); second.Value.R != 0 {
		t.Fatal("caller modified cached paint")
	}
	for range ctColorCacheCapacity * 2 {
		doc.updateCtColor(&models.CTColor{Value: &models.Color{RGBA: color.RGBA{A: 255}}})
	}
	if n := doc.ctColorCache.Len(); n > ctColorCacheCapacity {
		t.Fatalf("cache=%d exceeds capacity", n)
	}
	doc.Close()
	if doc.ctColorCache != nil {
		t.Fatal("Close retained color cache")
	}
}

type closeTrackingFont struct {
	fakeFontEngine
	closes atomic.Int32
	fail   bool
}

func (f *closeTrackingFont) Close() { f.closes.Add(1) }
func (f *closeTrackingFont) UseFallbackFont(string) error {
	if f.fail {
		return errors.New("fallback failed")
	}
	return nil
}

func sealLifecycleData(t *testing.T) []byte {
	t.Helper()
	data, err := creator.Marshal(creator.Document{ID: "seal-lifecycle", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCloseReleasesSealDocumentsOnce(t *testing.T) {
	previous := newFontEngine
	newFontEngine = func(*parser.Document) FontEngine { return &closeTrackingFont{} }
	t.Cleanup(func() { newFontEngine = previous })
	doc := NewDocument(color.Transparent, nil)
	entry, err := doc.sealDocument(sealLifecycleData(t))
	if err != nil {
		t.Fatal(err)
	}
	font := entry.doc.fonts.(*closeTrackingFont)
	doc.Close()
	doc.Close()
	if font.closes.Load() != 1 || len(doc.sealDocs) != 0 || !entry.closed {
		t.Fatal("seal resources not released once")
	}
	if len(entry.ofd.Documents) != 0 {
		t.Fatal("seal parser remains open")
	}
	if _, err := doc.sealDocument(sealLifecycleData(t)); err == nil {
		t.Fatal("closed document accepted seal")
	}
}

func TestSealInitializationFailureReleasesFont(t *testing.T) {
	previous := newFontEngine
	font := &closeTrackingFont{fail: true}
	newFontEngine = func(*parser.Document) FontEngine { return font }
	t.Cleanup(func() { newFontEngine = previous })
	doc := &Document{fallbacks: []string{"missing"}}
	if _, err := doc.sealDocument(sealLifecycleData(t)); err == nil {
		t.Fatal("fallback error lost")
	}
	if font.closes.Load() != 1 {
		t.Fatal("failed seal font not released")
	}
}

func TestConcurrentSealCreationReleasesDiscardedFonts(t *testing.T) {
	previous := newFontEngine
	var mu sync.Mutex
	var fonts []*closeTrackingFont
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	newFontEngine = func(*parser.Document) FontEngine {
		f := &closeTrackingFont{}
		mu.Lock()
		fonts = append(fonts, f)
		mu.Unlock()
		ready <- struct{}{}
		<-release
		return f
	}
	t.Cleanup(func() { newFontEngine = previous })
	doc := &Document{}
	data := sealLifecycleData(t)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := doc.sealDocument(data); err != nil {
				t.Error(err)
			}
		}()
	}
	<-ready
	<-ready
	close(release)
	wg.Wait()
	if got := fonts[0].closes.Load() + fonts[1].closes.Load(); got != 1 {
		t.Fatalf("discarded font closes=%d", got)
	}
	doc.Close()
	for _, font := range fonts {
		if font.closes.Load() != 1 {
			t.Fatal("font not closed exactly once")
		}
	}
}

func TestSealCreationDuringCloseReleasesNewFont(t *testing.T) {
	previous := newFontEngine
	font := &closeTrackingFont{}
	ready, release := make(chan struct{}), make(chan struct{})
	newFontEngine = func(*parser.Document) FontEngine {
		close(ready)
		<-release
		return font
	}
	t.Cleanup(func() { newFontEngine = previous })
	doc := &Document{}
	data := sealLifecycleData(t)
	result := make(chan error, 1)
	go func() {
		_, err := doc.sealDocument(data)
		result <- err
	}()
	<-ready
	doc.Close()
	close(release)
	if err := <-result; err == nil {
		t.Fatal("seal inserted after Close")
	}
	if font.closes.Load() != 1 || len(doc.sealDocs) != 0 {
		t.Fatal("seal created during Close retained resources")
	}
}
