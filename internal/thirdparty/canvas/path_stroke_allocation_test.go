package canvas

import (
	"slices"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

func TestMergeAllocatesOnceAndPreservesSourcePaths(t *testing.T) {
	paths := Paths{nil, {}, MustParseSVGPath("M9 9"), Rectangle(10, 10), MustParseSVGPath("M0 0Q5 10 10 0C15 0 15 10 20 10A5 5 0 0 1 25 15z")}
	want := &Path{}
	for _, path := range paths {
		want = want.Append(path)
	}
	got := paths.Merge()
	if !slices.Equal(got.d, want.d) {
		t.Fatal("合并改变了空路径过滤或命令顺序")
	}
	got.Transform(Identity.Translate(3, 4))
	if !slices.Equal(paths.Merge().d, want.d) {
		t.Fatal("合并结果与源路径共享了坐标缓冲")
	}
	allocations := testing.AllocsPerRun(100, func() {
		merged := paths.Merge()
		if len(merged.d) != len(want.d) {
			t.Fatal("合并结果长度不一致")
		}
	})
	// Path 结构可能被编译器放在栈上；唯一必要的堆分配是坐标缓冲。
	if allocations > 2 {
		t.Fatalf("合并不应反复扩容: %g 次分配", allocations)
	}
}

func TestSweepEventGrowthPreservesAllSubpathEndpoints(t *testing.T) {
	boInitPoolsOnce()
	var queue SweepEvents
	defer func() {
		for _, event := range queue {
			boPointPool.Put(event)
		}
	}()
	segment := 0
	for i := range 128 {
		path := Rectangle(10, 5).Translate(float64(i*20), 0)
		previous := slices.Clone(queue)
		segment = queue.AddPathEndpoints(path, segment, false)
		if len(queue) != (i+1)*8 {
			t.Fatalf("矩形应产生 8 个端点事件: got=%d", len(queue))
		}
		if !slices.Equal(queue[:len(previous)], previous) {
			t.Fatal("事件队列扩容改变了先前端点引用或顺序")
		}
		for _, event := range queue[len(previous):] {
			if event.other.other != event || event.segment <= i*4 || (i+1)*4 < event.segment {
				t.Fatal("事件队列扩容破坏了端点配对或段编号")
			}
		}
	}
	if segment != 128*4 {
		t.Fatalf("总段数改变: %d", segment)
	}
}

func TestToleranceSquareInsertionPreservesOrderAndExistingEvents(t *testing.T) {
	boInitPoolsOnce()
	var squares toleranceSquares
	defer func() {
		for _, square := range squares {
			boSquarePool.Put(square)
		}
	}()
	// 包含首部、中间、尾部插入，以及同一容差方格的事件合并。
	for _, y := range []float64{3, 1, 4, 2, 2} {
		squares.Add(0, &SweepPoint{Point: Point{X: 0, Y: y}, left: true}, nil)
	}
	if len(squares) != 4 {
		t.Fatalf("容差方格未正确合并: %d", len(squares))
	}
	for i, square := range squares {
		if square.Y != float64(i+1) {
			t.Fatalf("方格次序改变: index=%d Y=%g", i, square.Y)
		}
		wantEvents := 1
		if i == 1 {
			wantEvents = 2
		}
		if len(square.Events) != wantEvents {
			t.Fatalf("方格事件丢失: index=%d events=%d", i, len(square.Events))
		}
		for _, event := range square.Events {
			if event.Y != square.Y {
				t.Fatal("移动方格时混淆了事件引用")
			}
		}
	}
}

func BenchmarkStrokeAllocation(b *testing.B) {
	family := NewFontFamily("stroke-allocation")
	if err := family.LoadFont(goregular.TTF, 0, FontRegular); err != nil {
		b.Fatal(err)
	}
	text, _ := family.Face(24).ToPath("Trial Version OFD 0123456789")
	dashes := Rectangle(160, 100).Dash(0, 3, 1)
	for _, tc := range []struct {
		name string
		path *Path
	}{
		{"TextOutline", text},
		{"Dashes", dashes},
		{"Curve", MustParseSVGPath("M0 0Q10 30 20 0C25 -10 35 20 40 0")},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = tc.path.Stroke(0.5, RoundCap, MiterJoin, Tolerance)
			}
		})
	}
}
