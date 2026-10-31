package reader

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/gofont/goregular"
)

func sourceOptionsReader(t *testing.T) *Reader {
	t.Helper()
	data, err := creator.Marshal(creator.Document{ID: "reader-text-sources", PageSize: creator.PageSize{Width: 100, Height: 100}, Fonts: []creator.Font{{Name: "Go", Data: goregular.TTF}},
		Composites: []creator.CompositeGraphicUnit{{ID: 100, Width: 100, Height: 100, Items: []creator.Item{
			creator.Text{X: 5, Y: 5, Width: 25, Height: 8, Font: "Go", Size: 4, Value: "INNER", Actions: []creator.Action{{Event: creator.ActionEventClick, URI: &creator.URIAction{URI: "https://inner.example"}}}},
		}}},
		Pages:       []creator.Page{{Items: []creator.Item{creator.Text{X: 2, Y: 2, Width: 25, Height: 8, Font: "Go", Size: 4, Value: "BODY"}, creator.Composite{X: 20, Y: 20, Width: 40, Height: 40, ResourceID: 100}}}},
		Annotations: []creator.AnnotationPage{{Page: 0, Items: []creator.Annotation{{ID: 201, Type: "Stamp", Creator: "test", LastModDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Boundary: &creator.Box{X: 5, Y: 70, Width: 30, Height: 10}, Items: []creator.Item{creator.Text{Width: 25, Height: 8, Font: "Go", Size: 4, Value: "NOTE"}}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestTextSourceOptionsUpdateCopySearchAndHighlightsTogether(t *testing.T) {
	r := sourceOptionsReader(t)
	runs, err := r.Text(0)
	if err != nil || len(runs) != 2 || runs[1].Text != "INNER" || runs[1].Glyphs[0].X < 20 {
		t.Fatalf("复合文字未进入覆盖层：%v %+v", err, runs)
	}
	before, err := r.Search(t.Context(), "NOTE", SearchOptions{})
	if err != nil || len(before.Results) != 0 {
		t.Fatal("默认不应搜索批注")
	}
	if err := r.SetTextOptions(TextOptions{IncludeAnnotations: true}); err != nil {
		t.Fatal(err)
	}
	after, err := r.Search(t.Context(), "NOTE", SearchOptions{})
	if err != nil || len(after.Results) != 1 || after.Results[0].Rects[0].Y < 65 {
		t.Fatalf("来源切换没有刷新搜索与高亮：%v %+v", err, after)
	}
	runs, err = r.Text(0)
	if err != nil || len(runs) != 3 || runs[2].Source != "annotation" {
		t.Fatalf("覆盖层未采用相同来源：%v %+v", err, runs)
	}
	links, err := r.PageLinksForPage(0)
	if err != nil || len(links) != 1 || links[0].URI != "https://inner.example" || links[0].Boundary.X < 20 {
		t.Fatalf("复合链接热区未映射：%v %+v", err, links)
	}
	if err := r.SetTextOptions(TextOptions{}); err != nil {
		t.Fatal(err)
	}
	reset, err := r.Search(t.Context(), "NOTE", SearchOptions{})
	if err != nil || len(reset.Results) != 0 {
		t.Fatal("恢复来源后仍命中旧批注索引")
	}
}

func TestRenderPageWithReportPreservesCancellationAndPageOutput(t *testing.T) {
	r := sourceOptionsReader(t)
	data, report, err := renderPageWithReportBytes(t.Context(), r, 0, RenderOptions{DPI: 25.4})
	if err != nil || len(data) == 0 || report.Stats.ObjectsVisited == 0 {
		t.Fatalf("页面或报告缺失：%v %+v", err, report)
	}
	plain, err := renderPageBytes(t.Context(), r, 0, RenderOptions{DPI: 25.4})
	if err != nil || !bytes.Equal(data, plain) {
		t.Fatal("请求诊断改变了页面输出")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := renderPageWithReportBytes(ctx, r, 0, RenderOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误被改写：%v", err)
	}
}

func TestTextSourceOptionsConcurrentWithSearchAndCopy(t *testing.T) {
	r := sourceOptionsReader(t)
	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for turn := range 4 {
				if worker == 0 {
					if err := r.SetTextOptions(TextOptions{IncludeAnnotations: turn%2 == 0}); err != nil {
						t.Error(err)
					}
				} else {
					if _, err := r.Text(0); err != nil {
						t.Error(err)
					}
					matches, err := r.Search(t.Context(), "INNER", SearchOptions{})
					if err != nil || len(matches.Results) != 1 {
						t.Errorf("来源切换破坏搜索索引：%v %+v", err, matches)
					}
				}
			}
		}()
	}
	wg.Wait()
}
