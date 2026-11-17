package layout

import (
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/spec"
)

func borderEngine() *engine {
	return &engine{author: &authorState{}, opts: Options{PageHeight: 100},
		contentLeft: 10, pageIndex: 0, pages: []creator.Page{{}}}
}

func leftBorderPath(t *testing.T, item creator.Item) creator.Path {
	t.Helper()
	path, ok := item.(creator.Path)
	if !ok {
		t.Fatalf("色条不是路径: %T", item)
	}
	return path
}

// 内容和段后间距共用一个括号状路径，只在完整色条上下端向右微弯。
func TestAuthorLeftBorderMergesAdjacentPiecesAndCurvesOnlyOuterEnds(t *testing.T) {
	e := borderEngine()
	color := &creator.Color{B: 218}
	e.drawAuthorLeftBorder(color, 6, 90, 10)
	e.add(creator.Text{Value: "first"})
	e.drawAuthorLeftBorder(color, 6, 80, 3)
	e.add(creator.Text{Value: "second"})
	e.drawAuthorLeftBorder(color, 6, 77, 8)
	if len(e.cur().Items) != 3 {
		t.Fatalf("连续色条产生内部接缝: %d 个图元", len(e.cur().Items))
	}
	path := leftBorderPath(t, e.cur().Items[0])
	if path.X != 11 || path.Y != 10 || path.Width != 2.4 || path.Height != 21 {
		t.Fatalf("合并改变了占位或边界: %+v", path)
	}
	if strings.Count(path.Data, " Q ") != 4 || path.Data != "M 2.4 0 Q 0 0 0 2.4 L 0 18.6 Q 0 21 2.4 21 Q 1.2 21 1.2 18.6 L 1.2 2.4 Q 1.2 0 2.4 0 C" {
		t.Fatalf("色条应中段平直、上下端向右微弯: %s", path.Data)
	}
	if _, err := spec.ParsePathData(path.Data); err != nil {
		t.Fatalf("弧形路径不符合 OFD 语法: %v", err)
	}
}

func TestAuthorLeftBorderDoesNotMergeAcrossGapsColorsColumnsOrPages(t *testing.T) {
	for _, change := range []string{"gap", "color", "column", "page", "plain"} {
		t.Run(change, func(t *testing.T) {
			e := borderEngine()
			color := &creator.Color{B: 218}
			e.drawAuthorLeftBorder(color, 6, 90, 10)
			top := 80.0
			switch change {
			case "gap":
				top--
			case "color":
				color = &creator.Color{R: 207}
			case "column":
				e.contentLeft += 40
			case "page":
				e.pages = append(e.pages, creator.Page{})
				e.pageIndex++
			case "plain":
				e.drawAuthorLeftBorder(nil, 6, 80, 1)
			}
			e.drawAuthorLeftBorder(color, 6, top, 5)
			if first := leftBorderPath(t, e.pages[0].Items[0]); first.Height != 10 {
				t.Fatalf("不连续的色条被合并: %+v", first)
			}
			count := 0
			for _, page := range e.pages {
				count += len(page.Items)
			}
			if count != 2 {
				t.Fatalf("应保留两条独立弧形色条: %d", count)
			}
		})
	}
}

func TestAuthorLeftBorderBendsClampToShortFragmentHeight(t *testing.T) {
	e := borderEngine()
	e.drawAuthorLeftBorder(&creator.Color{B: 218}, 6, 90, 0.2)
	path := leftBorderPath(t, e.cur().Items[0])
	if path.Data != "M 2.4 0 Q 0 0 0 0.1 L 0 0.1 Q 0 0.2 2.4 0.2 Q 1.2 0.2 1.2 0.1 L 1.2 0.1 Q 1.2 0 2.4 0 C" {
		t.Fatalf("短片段弯曲高度超过片段高度的一半: %s", path.Data)
	}
	if _, err := spec.ParsePathData(path.Data); err != nil {
		t.Fatal(err)
	}
}
