package render

import (
	"image/color"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

type blockingDocumentFontEngine struct {
	metricsFontEngine

	entered chan struct{}
	resume  chan struct{}
	closed  atomic.Int32
}

func (e *blockingDocumentFontEngine) LoadFont(id models.StRefID) (FontFamily, error) {
	close(e.entered)
	<-e.resume
	return e.metricsFontEngine.LoadFont(id)
}

func (e *blockingDocumentFontEngine) Close() { e.closed.Add(1) }

// Close 拒绝新操作，但不能释放仍被绘制或度量使用的字体。
func TestDocumentCloseWaitsForDrawingAndTextLayouts(t *testing.T) {
	for _, name := range []string{"Draw", "Page", "PageContent", "Text", "TextLayouts"} {
		t.Run(name, func(t *testing.T) {
			engine := &blockingDocumentFontEngine{
				metricsFontEngine: metricsFontEngine{face: metricsFontFace{ascent: 3, descent: 1}},
				entered:           make(chan struct{}), resume: make(chan struct{}),
			}
			doc := &Document{Document: &parser.Document{}, fonts: engine, background: color.White}
			object := textItem(models.StBox{Width: 20, Height: 10}, nil)
			object.Text.Size = 3
			object.Text.TextCode = []models.TextCode{{Value: "AAA", Y: 5}}
			page := parser.NewPage(models.PageContent{
				Area:    &models.CtPageArea{PhysicalBox: models.StBox{Width: 20, Height: 20}},
				Content: &models.Content{Layer: []*models.Layer{{CTPageBlock: models.CTPageBlock{Items: []models.PageItem{object}}}}},
			})
			drawn := make(chan error, 1)
			go func() {
				ctx := newCanvasBackend(canvas.NewContext(canvas.New(20, 20)))
				switch name {
				case "Draw":
					drawn <- doc.Draw(ctx, page)
				case "Page":
					_, err := doc.Page(page)
					drawn <- err
				case "PageContent":
					doc.PageContent(ctx, page, false)
					drawn <- nil
				case "Text":
					doc.Text(ctx, *object.Text, nil, models.StBox{Width: 20, Height: 20})
					drawn <- nil
				case "TextLayouts":
					_, err := doc.TextLayoutsWithVisibilityE(page, PageVisibility{})
					drawn <- err
				}
			}()
			select {
			case <-engine.entered:
			case err := <-drawn:
				t.Fatalf("未进入字体访问便结束: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("未进入字体访问")
			}
			closed := make(chan struct{})
			go func() { doc.Close(); close(closed) }()
			deadline := time.Now().Add(5 * time.Second)
			for !doc.closed.Load() && time.Now().Before(deadline) {
				runtime.Gosched()
			}
			if !doc.closed.Load() {
				t.Error("Close 未拒绝新操作")
			}
			select {
			case <-closed:
				t.Error("在途操作结束前 Close 已返回")
			default:
			}
			if engine.closed.Load() != 0 {
				t.Error("在途操作仍使用的字体已被释放")
			}
			if err := doc.Draw(nil, page); err == nil {
				t.Error("关闭期间仍接受新绘制")
			}
			if _, err := doc.TextLayoutsWithVisibilityE(page, PageVisibility{}); err == nil {
				t.Error("关闭期间仍接受新布局")
			}
			close(engine.resume)
			if err := <-drawn; err != nil {
				t.Errorf("在途操作失败: %v", err)
			}
			<-closed
			doc.Close()
			if engine.closed.Load() != 1 {
				t.Errorf("字体释放次数=%d，期望1", engine.closed.Load())
			}
		})
	}
}
