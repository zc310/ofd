package canvasfont

import (
	"sync"
	"testing"

	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"golang.org/x/image/font/gofont/goregular"
)

// 两个不同的主字体族回退到同一系统字体时，必须使用同一把实际字体锁。
// 使用 -race -tags harfbuzz 运行，同时覆盖 CGo shaper 的按字号缓存。
func TestFaceObjectFallbackSharesActualFamilyRenderLock(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	var faces [2][]drawing.FontFace
	var locks [2]*sync.Mutex
	var actual drawing.FontFamily
	for i := range faces {
		primary := canvas.NewFontFamily("latin-primary")
		if err := primary.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
			t.Fatal(err)
		}
		for size := 1; size <= 64; size++ {
			object := textObjectWith("中文")
			object.Size = float64(size)
			face, family := fonts.FaceObject(primary, object, nil)
			if family == primary {
				t.Skip("系统没有可用的 CJK 回退字体")
			}
			if actual == nil {
				actual = family
			} else if actual != family {
				t.Fatal("不同主族没有回退到同一字体族")
			}
			locks[i] = fonts.RenderLock(family)
			faces[i] = append(faces[i], face)
		}
	}
	if locks[0] != locks[1] {
		t.Fatal("同一实际字体族返回了不同渲染锁")
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range faces {
		wg.Go(func() {
			<-start
			for _, face := range faces[i] {
				locks[i].Lock()
				_ = face.TextWidth("中文")
				_ = face.ShapedRun("中文")
				locks[i].Unlock()
			}
		})
	}
	close(start)
	wg.Wait()
}
