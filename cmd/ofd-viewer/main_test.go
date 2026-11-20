package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/version"
	"github.com/zc310/ofd/pkg/creator"
)

func TestParseArgsOutputAndExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		file string
		stop bool
		code int
		out  string
	}{
		{"默认启动", nil, "", false, 0, ""},
		{"打开文件", []string{"example.ofd"}, "example.ofd", false, 0, ""},
		{"长版本", []string{"--version"}, "", true, 0, version.Version},
		{"短版本", []string{"-v"}, "", true, 0, version.Version},
		{"帮助", []string{"--help"}, "", true, 0, "用法:"},
		{"非法选项", []string{"--unknown"}, "", true, 2, ""},
		{"多个输入", []string{"a.ofd", "b.ofd"}, "", true, 2, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			file, stop, code := parseArgs(tc.args, &stdout, &stderr)
			if file != tc.file || stop != tc.stop || code != tc.code {
				t.Fatalf("参数处理结果 = %q/%v/%d", file, stop, code)
			}
			if tc.out != "" && !strings.Contains(stdout.String(), tc.out) {
				t.Fatalf("标准输出缺少 %q: %s", tc.out, &stdout)
			}
			if tc.code == 0 && stderr.Len() != 0 {
				t.Fatalf("正常操作污染标准错误: %s", &stderr)
			}
			if tc.code != 0 && (stderr.Len() == 0 || stdout.Len() != 0) {
				t.Fatalf("参数错误输出通道错误: stdout=%s stderr=%s", &stdout, &stderr)
			}
		})
	}
}

func TestFitPageRejectsInvalidSizesAndFitsViewport(t *testing.T) {
	for _, tc := range []struct {
		available image.Point
		w, h      float64
		want      image.Point
		ok        bool
	}{
		{image.Pt(800, 600), 210, 297, image.Pt(424, 600), true},
		{image.Pt(600, 800), 297, 210, image.Pt(600, 424), true},
		{image.Pt(1, 1), 210, 297, image.Pt(1, 1), true},
		{image.Pt(0, 600), 210, 297, image.Point{}, false},
		{image.Pt(800, 600), 0, 297, image.Point{}, false},
		{image.Pt(800, 600), math.NaN(), 297, image.Point{}, false},
		{image.Pt(800, 600), 210, math.Inf(1), image.Point{}, false},
	} {
		if got, ok := fitPage(tc.available, tc.w, tc.h); got != tc.want || ok != tc.ok {
			t.Fatalf("fitPage(%v,%v,%v) = %v/%v", tc.available, tc.w, tc.h, got, ok)
		}
	}
}

func createTestDocument(t *testing.T) string {
	t.Helper()
	var encoded bytes.Buffer
	pixel := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	pixel.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&encoded, pixel); err != nil {
		t.Fatal(err)
	}
	data, err := creator.Marshal(creator.Document{
		ID: "gio-test", PageSize: creator.A4,
		Pages: []creator.Page{
			{Items: []creator.Item{
				creator.Path{X: 10, Y: 10, Width: 20, Height: 20,
					Data: "M 0 0 L 20 0 L 20 20 L 0 20 C", Fill: true, FillColor: &creator.Color{}},
				creator.Image{X: 50, Y: 10, Width: 20, Height: 20, Data: encoded.Bytes(), Format: "PNG"},
			}},
			{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "test.ofd")
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestDocumentCachesCurrentSceneAndKeepsPageOnFailure(t *testing.T) {
	m, err := openDocument(createTestDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	first := m.scene
	if err := m.selectPage(0); err != nil || first != m.scene {
		t.Fatalf("重选当前页未复用记录: %v", err)
	}
	if err := m.selectPage(2); err == nil || m.current != 0 || m.scene != first {
		t.Fatal("非法页码改变了当前页面")
	}
	var ops op.Ops
	v := viewer{scene: m.scene}
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(800, 600))}
	for range 2 {
		ops.Reset()
		if got := v.layoutViewport(gtx).Size; got != image.Pt(800, 600) {
			t.Fatalf("固定约束下页面尺寸 = %v", got)
		}
	}
	if err := m.selectPage(1); err != nil || m.current != 1 || m.scene == first {
		t.Fatalf("切页未替换记录: %v", err)
	}
	if err := m.selectPage(0); err != nil || m.scene != first {
		t.Fatalf("返回最近页面未复用有界缓存: %v", err)
	}
	if m.scenes.Len() > pageSceneCacheEntries || m.scenes.Weight() > pageSceneCacheBytes {
		t.Fatalf("页面缓存超出预算: entries=%d bytes=%d", m.scenes.Len(), m.scenes.Weight())
	}
}

func TestDocumentSceneCacheEvictsLeastRecentlyUsedPage(t *testing.T) {
	pages := make([]creator.Page, pageSceneCacheEntries+1)
	for i := range pages {
		pages[i] = creator.Page{Items: []creator.Item{creator.Path{X: 10, Y: 10, Width: 20, Height: 20,
			Data: "M 0 0 L 20 0 L 20 20 L 0 20 C", Fill: true, FillColor: &creator.Color{}}}}
	}
	data, err := creator.Marshal(creator.Document{ID: "gio-cache-test", PageSize: creator.A4, Pages: pages})
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "cache.ofd")
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := openDocument(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	for i := 1; i < len(pages); i++ {
		if err := m.selectPage(i); err != nil {
			t.Fatal(err)
		}
	}
	if m.scenes.Len() > pageSceneCacheEntries || m.scenes.Weight() > pageSceneCacheBytes {
		t.Fatalf("页面缓存超出预算: entries=%d bytes=%d", m.scenes.Len(), m.scenes.Weight())
	}
	if _, ok := m.scenes.Get(0); ok {
		t.Fatal("最久未使用的页面未淘汰")
	}
	if _, ok := m.scenes.Get(len(pages) - 1); !ok {
		t.Fatal("当前页应在预算允许时保留")
	}
}

func TestFailedAndCanceledOpenPreservesDocument(t *testing.T) {
	m, err := openDocument(createTestDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { m.close() }()
	first, scene := m, m.scene
	if err := handleRequest(&m, request{filename: filepath.Join(t.TempDir(), "missing.ofd")}, nil); err == nil {
		t.Fatal("不存在的输入未报告错误")
	}
	if err := handleRequest(&m, request{choose: true}, func() (string, error) { return "", nil }); err != nil {
		t.Fatal(err)
	}
	if m != first || m.scene != scene {
		t.Fatal("打开失败或取消时丢失原文档")
	}
	if err := handleRequest(&m, request{page: 1}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerCancellationDoesNotBlockOnPendingResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requests := make(chan request, 1)
	results := make(chan result)
	finished := make(chan struct{})
	requests <- request{filename: createTestDocument(t)}
	go func() {
		defer close(finished)
		runWorker(ctx, requests, results, func() {}, nil)
	}()
	// 接收结果后立即退出；还可覆盖 worker 等待请求时的取消。
	select {
	case response := <-results:
		if response.err != nil {
			t.Fatal(response.err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("后台加载未完成")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(20 * time.Second):
		t.Fatal("取消后 worker 没有释放资源并退出")
	}
}

func TestWorkerSelectedBytesSupportThumbnailsAndCancel(t *testing.T) {
	data, err := os.ReadFile(createTestDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	requests, results := make(chan request, 1), make(chan result, 1)
	count := 0
	go runWorkerWithPicker(ctx, requests, results, func() {}, func() (selectedFile, error) {
		count++
		if count > 1 {
			return selectedFile{}, nil
		}
		return selectedFile{name: "内存文档.ofd", data: data}, nil
	})
	requests <- request{choose: true}
	opened := <-results
	if opened.err != nil || !opened.transient || opened.session == nil || opened.session.input == nil {
		t.Fatalf("内存文档打开失败: %+v", opened)
	}
	thumb, err := openThumbnailInput(opened.filename, opened.session.input)
	if err != nil {
		t.Fatalf("内存文档缩略图打开失败: %v", err)
	}
	thumb.close()
	requests <- request{choose: true}
	canceled := <-results
	if canceled.err != nil || !canceled.transient || canceled.scene != opened.scene {
		t.Fatalf("取消选择应保留原文档: %+v", canceled)
	}
}

func TestWorkerChoosesBeforeWaitingForFallbackFont(t *testing.T) {
	data, err := os.ReadFile(createTestDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	requests, results := make(chan request, 1), make(chan result, 1)
	ready, chosen := make(chan struct{}), make(chan struct{})
	go runWorkerWithPickerReady(ctx, requests, results, func() {}, func() (selectedFile, error) {
		close(chosen)
		return selectedFile{name: "内存文档.ofd", data: data}, nil
	}, ready)
	requests <- request{choose: true}
	<-chosen
	select {
	case response := <-results:
		t.Fatalf("字体未就绪不应打开文档: %+v", response)
	default:
	}
	close(ready)
	if response := <-results; response.err != nil || response.scene == nil {
		t.Fatalf("字体就绪后应打开文档: %+v", response)
	}
}

func TestWorkerCanceledWhilePublishingReleasesDocument(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	requests := make(chan request, 1)
	results := make(chan result)
	finished, selected := make(chan struct{}), make(chan struct{})
	requests <- request{choose: true}
	go func() {
		defer close(finished)
		runWorker(ctx, requests, results, func() {}, func() (string, error) {
			close(selected)
			return "", errors.New("选择失败")
		})
	}()
	<-selected
	cancel()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("发布结果阻塞取消")
	}
}

func TestWorkerRetiresReplacedDocumentAfterSceneLeaseReleased(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	requests := make(chan request, 1)
	results := make(chan result, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runWorker(ctx, requests, results, func() {}, nil)
	}()
	defer func() { cancel(); <-done }()
	filename := createTestDocument(t)
	requests <- request{filename: filename}
	first := <-results
	if first.err != nil || first.session == nil {
		t.Fatalf("打开文档失败: %v", first.err)
	}
	lease := first.session.acquire()
	if lease == nil {
		t.Fatal("无法取得页面租约")
	}
	defer lease.release()
	closed := make(chan struct{})
	first.session.mu.Lock()
	closeDocument := first.session.close
	first.session.close = func() { closeDocument(); close(closed) }
	first.session.mu.Unlock()
	requests <- request{filename: filename}
	second := <-results
	if second.err != nil || second.session == first.session {
		t.Fatalf("重新打开未建立新会话: %v", second.err)
	}
	if first.session.acquire() != nil {
		t.Fatal("旧会话退役后仍可取得租约")
	}
	select {
	case <-closed:
		t.Fatal("旧页面仍被引用时文档已关闭")
	default:
	}
	lease.release()
	select {
	case <-closed:
	case <-time.After(20 * time.Second):
		t.Fatal("归还页面租约后文档未关闭")
	}
}

func TestHeadlessGioRendersFilledPage(t *testing.T) {
	window, err := headless.NewWindow(210, 297)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	m, err := openDocument(createTestDocument(t))
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	var ops op.Ops
	v := viewer{scene: m.scene}
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(210, 297))}
	v.layoutViewport(gtx)
	if err := window.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 210, 297))
	if err := window.Screenshot(pixels); err != nil {
		t.Fatal(err)
	}
	cpu, err := m.pages[0].document.RasterizePage(m.pages[0].page, render.BackendCanvas, geom.DPMM(1))
	if err != nil {
		t.Fatal(err)
	}
	for _, point := range []image.Point{image.Pt(20, 20), image.Pt(60, 20)} {
		got, want := pixels.RGBAAt(point.X, point.Y), cpu.RGBAAt(point.X, point.Y)
		if got != want || got.A != 255 {
			t.Fatalf("路径或图片像素 %v 与 CPU 不一致: GPU=%v CPU=%v", point, got, want)
		}
	}
	outside := pixels.RGBAAt(200, 280)
	if outside.R < 245 || outside.G < 245 || outside.B < 245 || outside.A != 255 {
		t.Fatalf("页面底色不是白色: %v", outside)
	}
	// 跨帧复用操作，确认缩放和平移应用于路径与图片，且不会破坏缓存引用。
	v.viewport.zoom(2, f32.Point{}, 1)
	v.viewport.pan(f32.Pt(-10, -10))
	ops.Reset()
	v.layoutViewport(gtx)
	if err := window.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	if err := window.Screenshot(pixels); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]image.Point{
		{image.Pt(20, 20), image.Pt(20, 20)},
		{image.Pt(110, 20), image.Pt(60, 20)},
	} {
		if got, want := pixels.RGBAAt(pair[0].X, pair[0].Y), cpu.RGBAAt(pair[1].X, pair[1].Y); got != want {
			t.Fatalf("缓存缩放平移像素错误: %v GPU=%v CPU=%v", pair[0], got, want)
		}
	}
}
