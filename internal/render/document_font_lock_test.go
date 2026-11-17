package render

import (
	"sync"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/pkg/spec"
)

type lockTestFamily string

func (f lockTestFamily) Name() string { return string(f) }

type fallbackLockEngine struct {
	fakeFontEngine

	t       *testing.T
	primary sync.Mutex
	actual  sync.Mutex
	visits  int
}

func (e *fallbackLockEngine) LoadFont(spec.StRefID) (FontFamily, error) {
	return lockTestFamily("primary"), nil
}

func (e *fallbackLockEngine) FaceObject(FontFamily, spec.TextObject, *CTColor) (FontFace, FontFamily) {
	return lockedFontFace{metricsFontFace: metricsFontFace{ascent: 3, descent: 1}, engine: e}, lockTestFamily("actual")
}

func (e *fallbackLockEngine) RenderLock(family FontFamily) *sync.Mutex {
	if family == lockTestFamily("actual") {
		return &e.actual
	}
	return &e.primary
}

func (e *fallbackLockEngine) check() {
	e.visits++
	if e.actual.TryLock() {
		e.actual.Unlock()
		e.t.Error("访问回退字体面时没有持有实际字体族的锁")
	}
}

type lockedFontFace struct {
	metricsFontFace

	engine *fallbackLockEngine
}

func (f lockedFontFace) Fill() geom.Paint {
	f.engine.check()
	return geom.SolidPaint(geom.Black)
}

func (f lockedFontFace) TextWidth(string) float64 { f.engine.check(); return 3 }
func (f lockedFontFace) VerticalMetrics() (float64, float64) {
	f.engine.check()
	return 3, 1
}
func (f lockedFontFace) ToPath(string) *geom.Path {
	f.engine.check()
	return geom.Rectangle(3, 4)
}

func TestTextConsumersLockActualFallbackFamily(t *testing.T) {
	for _, name := range []string{"text", "outline", "layout"} {
		t.Run(name, func(t *testing.T) {
			engine := &fallbackLockEngine{t: t}
			doc := &Document{Document: &parser.Document{}, fonts: engine}
			t.Cleanup(doc.Close)
			object := spec.TextObject{CtText: spec.CtText{
				CTGraphicUnit: spec.CTGraphicUnit{Boundary: spec.StBox{Width: 20, Height: 10}},
				Size:          3, TextCode: []spec.TextCode{{Value: "AB", Y: 5}},
			}}
			switch name {
			case "text":
				doc.Text(newCanvasBackend(canvas.NewContext(canvas.New(20, 20))), object, nil, spec.StBox{Width: 20, Height: 20})
			case "outline":
				doc.patternTextOutline(object, 20, nil)
			case "layout":
				buildTextLayout(doc, object, object.TextCode[0], 0)
			}
			if engine.visits == 0 {
				t.Fatal("没有访问字体面")
			}
		})
	}
}
