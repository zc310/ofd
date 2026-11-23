package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/spec"
)

func navigationTestData(t *testing.T) []byte {
	t.Helper()
	collapsed, top := false, 100.0
	data, err := creator.Marshal(creator.Document{
		ID: "viewer-navigation", PageSize: creator.A4, Pages: make([]creator.Page, 4),
		Bookmarks: []creator.Bookmark{{Name: "chapter", Goto: creator.GotoAction{Page: 3, Type: "XYZ", Top: &top}}},
		Outlines: []creator.Outline{
			{Title: "章节", Expanded: &collapsed, Children: []creator.Outline{
				{Title: "目标", Actions: []creator.Action{{Goto: &creator.GotoAction{Bookmark: "chapter"}}}},
			}},
			{Title: "直达", Actions: []creator.Action{{Goto: &creator.GotoAction{Page: 2}}}},
			{Title: "网站", Actions: []creator.Action{{URI: &creator.URIAction{URI: "https://example.test"}}}},
			{Title: "空条目"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func openNavigationTestModel(t *testing.T) *documentModel {
	t.Helper()
	model, err := openDocumentInput("navigation.ofd", navigationTestData(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(model.close)
	return model
}

func TestNavigationModelPreservesHeadingsHierarchyAndBookmarkDestinations(t *testing.T) {
	m := openNavigationTestModel(t)
	model := m.navigation
	if model == nil || len(model.outlines) != 5 || len(model.bookmarks) != 1 {
		t.Fatalf("导航数量错误: %+v", model)
	}
	parent, child := model.outlines[0], model.outlines[1]
	if parent.title != "章节" || parent.link.target != nil || parent.depth != 0 || parent.end != 2 || parent.expanded {
		t.Fatalf("纯标题父项或显式折叠状态丢失: %+v", parent)
	}
	if child.depth != 1 || child.end != 2 || !child.expanded || child.link.target == nil ||
		child.link.target.Page != 3 || child.link.target.Top != 100 || child.link.target.Type != drawing.DestXYZ {
		t.Fatalf("层级、默认展开或书签位置丢失: %+v", child)
	}
	if model.bookmarks[0].link.target == nil || *model.bookmarks[0].link.target != *child.link.target {
		t.Fatal("目录和书签必须使用一致的目标解析")
	}
	if model.outlines[2].link.target.Page != 2 || model.outlines[3].link.uri != "https://example.test" {
		t.Fatal("目录页面目标或网址丢失")
	}
}

func TestNavigationModelKeepsDocumentBodyScopesAndInvalidEntries(t *testing.T) {
	entries := map[string]string{
		"OFD.xml": `<OFD Version="1.0"><DocBody><DocRoot>A/Document.xml</DocRoot></DocBody><DocBody><DocRoot>B/Document.xml</DocRoot></DocBody></OFD>`,
	}
	for _, dir := range []string{"A", "B"} {
		entries[dir+"/Document.xml"] = `<Document><CommonData><MaxUnitID>10</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="P0.xml"/><Page ID="2" BaseLoc="P1.xml"/></Pages>
<Outlines><OutlineElem Title="group"><OutlineElem Title="chapter"><Actions><Action><Goto><Bookmark Name="chapter"/></Goto></Action></Actions></OutlineElem><OutlineElem Title="missing"><Actions><Action><Goto><Dest Type="Fit" PageID="999"/></Goto></Action></Actions></OutlineElem></OutlineElem><OutlineElem Title="unsafe"><Actions><Action><URI URI="javascript:alert(1)"/></Action></Actions></OutlineElem></Outlines>
<Bookmarks><Bookmark Name="chapter"><Dest Type="XYZ" PageID="2" Top="50"/></Bookmark><Bookmark Name="missing"><Dest Type="Fit" PageID="999"/></Bookmark></Bookmarks></Document>`
		entries[dir+"/P0.xml"] = `<Page><Content><Layer ID="3"/></Content></Page>`
		entries[dir+"/P1.xml"] = `<Page><Content><Layer ID="4"/></Content></Page>`
	}
	var encoded bytes.Buffer
	archive := zip.NewWriter(&encoded)
	for name, content := range entries {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	m, err := openDocumentInput("scoped-navigation.ofd", encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer m.close()
	if len(m.navigation.outlines) != 8 || len(m.navigation.bookmarks) != 4 {
		t.Fatalf("不能丢弃纯标题或失效条目: %+v", m.navigation)
	}
	for scope := range 2 {
		items := m.navigation.outlines[scope*4 : scope*4+4]
		bookmark := m.navigation.bookmarks[scope*2]
		if items[1].link.target == nil || items[1].link.target.Page != scope*2+1 || items[1].link.target.Top != 50 ||
			bookmark.link.target == nil || bookmark.link.target.Page != scope*2+1 {
			t.Fatalf("文档体%d的目录或同名书签串页: %+v / %+v", scope, items[1], bookmark)
		}
		if items[0].end != scope*4+3 || items[2].link.target != nil || items[3].link.uri != "" ||
			m.navigation.bookmarks[scope*2+1].link.target != nil {
			t.Fatalf("失效目标和危险协议必须保持不可执行: %+v", items)
		}
	}
}

func TestNavigationDepthLimitPreservesFollowingSiblings(t *testing.T) {
	elem := spec.OutlineElem{Title: "深层"}
	for range maxNavigationDepth + 10 {
		elem = spec.OutlineElem{Title: "父项", OutlineElem: []spec.OutlineElem{elem}}
	}
	var items []navigationItem
	appendNavigationOutlines(&items, []spec.OutlineElem{elem, {Title: "后续兄弟"}}, 0, nil, nil)
	if len(items) != maxNavigationDepth+1 || items[len(items)-1].title != "后续兄弟" {
		t.Fatalf("深度限制影响后续兄弟项: %d", len(items))
	}
}

func TestNavigationCollapsesWithoutMutatingPublishedModelAndKeepsTabsIndependent(t *testing.T) {
	m := openNavigationTestModel(t)
	v := viewer{scene: m.scene, total: 4}
	v.navigation.open = true
	v.navigation.reset(m.navigation)
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(280, 400)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	settings := viewerSettings{}
	saved := 0
	persist := func() { saved++ }
	frame := func() {
		ops.Reset()
		v.navigation.layout(gtx, continuousTestTheme(), 0, false)
	}
	frame()
	if !slices.Equal(v.navigation.visible, []int{0, 2, 3, 4}) {
		t.Fatalf("初始折叠未生效: %v", v.navigation.visible)
	}
	v.navigation.rows[0].selectButton.Click()
	v.handleNavigation(gtx, &settings, persist)
	if !slices.Equal(v.navigation.visible, []int{0, 1, 2, 3, 4}) || m.navigation.outlines[0].expanded {
		t.Fatal("纯标题点击应展开，且不能修改后台发布的模型")
	}
	v.navigation.lists[navigationOutlines].Position.First = 2
	v.navigation.tabs[navigationBookmarks].Click()
	v.handleNavigation(gtx, &settings, persist)
	if v.navigation.tab != navigationBookmarks || len(v.navigation.visible) != 1 || settings.NavigationTab != "bookmarks" {
		t.Fatal("书签标签未切换并保存")
	}
	v.navigation.tabs[navigationOutlines].Click()
	v.handleNavigation(gtx, &settings, persist)
	if v.navigation.lists[navigationOutlines].Position.First != 2 || !v.navigation.items[0][0].expanded || saved != 2 {
		t.Fatal("切换标签不能丢失目录滚动和展开状态")
	}
}

func TestNavigationListVirtualizesButtonsForLargeDocuments(t *testing.T) {
	model := &documentNavigation{bookmarks: make([]navigationItem, 100000)}
	for index := range model.bookmarks {
		model.bookmarks[index].title = fmt.Sprintf("书签%d", index)
	}
	var s navigationState
	s.reset(model)
	if s.tab != navigationBookmarks {
		t.Fatal("仅有书签时应自动切换书签标签")
	}
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(280, 400)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	s.layout(gtx, continuousTestTheme(), 0, false)
	if len(s.rows) == 0 || len(s.rows) > 16 {
		t.Fatalf("大书签列表未虚拟化: %d", len(s.rows))
	}
	s.lists[navigationBookmarks].Position.First = 99990
	ops.Reset()
	s.layout(gtx, continuousTestTheme(), 0, false)
	if _, ok := s.rows[99990]; !ok || len(s.rows) > 16 {
		t.Fatalf("滚动后未复用可见按钮窗口: %d", len(s.rows))
	}
}

func TestNavigationClickThroughFullWindowUsesDestinationInAllReadingModes(t *testing.T) {
	for _, mode := range []string{"单页", "连续", "双页"} {
		for _, width := range []int{900, 400} {
			t.Run(fmt.Sprintf("%s/宽%d", mode, width), func(t *testing.T) {
				m := openNavigationTestModel(t)
				v := viewer{scene: m.scene, viewScene: m.scene, total: 4, viewport: pageViewport{mode: fitManual, scale: 3}}
				v.thumbnails.reset("navigation.ofd")
				v.navigation.open = true
				v.navigation.reset(m.navigation)
				v.navigation.tab = navigationBookmarks
				v.navigation.rebuildVisible()
				v.continuous.enabled, v.continuous.double = mode != "单页", mode == "双页"
				v.continuous.reset(0)
				for index := range m.pages {
					scene, err := m.sceneAt(index)
					if err != nil {
						t.Fatal(err)
					}
					v.continuous.metadata = append(v.continuous.metadata, continuousPageMetadata{size: continuousPageSize{210, 297}})
					v.continuous.entries.AddWeighted(index, &continuousEntry{scene: scene}, 1)
				}
				var router input.Router
				var ops op.Ops
				gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(width, 700)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
				settings := viewerSettings{Navigation: true, NavigationTab: "bookmarks"}
				var requests []request
				frame := func() {
					ops.Reset()
					v.processLinkJump(func(req request) { requests = append(requests, req) })
					v.handleNavigation(gtx, &settings, func() {})
					v.layout(gtx, continuousTestTheme())
					router.Frame(&ops)
				}
				frame()
				bounds, ok := navigationSemanticBounds(&router, "chapter")
				if !ok || bounds.Empty() {
					t.Fatalf("书签条目没有可见热区: %v", bounds)
				}
				point := f32.Pt(float32(bounds.Min.X+bounds.Dx()/2), float32(bounds.Min.Y+bounds.Dy()/2))
				router.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: point})
				router.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: point})
				frame()
				frame()
				if len(requests) != 1 || requests[0].page != 3 {
					t.Fatalf("书签点击没有提交全局目标页: %+v", requests)
				}
				if width == 400 && (v.navigation.open || settings.Navigation) {
					t.Fatal("窄窗口选择书签后应自动收起浮层")
				}
				if mode == "单页" {
					scene, err := m.sceneAt(3)
					if err != nil {
						t.Fatal(err)
					}
					v.acceptResponse(result{scene: scene, filename: "navigation.ofd", page: 3, total: 4}, func() {}, func(string) {})
					frame()
				}
				if v.page != 3 || v.linkDestination != nil || v.thumbnails.list.Position.First != 3 {
					t.Fatalf("未应用目标位置或同步全局页码: page=%d destination=%+v", v.page, v.linkDestination)
				}
			})
		}
	}
}

func TestNavigationInvalidLeafIsDisabledAndExternalTargetUsesSafeOpener(t *testing.T) {
	m := openNavigationTestModel(t)
	v := viewer{scene: m.scene, total: 4}
	v.navigation.open = true
	v.navigation.reset(m.navigation)
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Constraints: layout.Exact(image.Pt(280, 400)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	v.navigation.layout(gtx, continuousTestTheme(), 0, false)
	var opened string
	v.openURL = func(uri string) { opened = uri }
	v.navigation.rows[3].selectButton.Click()
	v.handleNavigation(gtx, &viewerSettings{}, func() {})
	if opened != "https://example.test" || v.linkJump != nil {
		t.Fatal("外部目录项未使用安全网址打开逻辑")
	}
	v.navigation.rows[4].selectButton.Click()
	v.handleNavigation(gtx, &viewerSettings{}, func() {})
	if v.linkJump != nil {
		t.Fatal("无目标叶项不能提交页面跳转")
	}
}

func TestNavigationGPUHighlightsCurrentGlobalPageAndSelectedTab(t *testing.T) {
	window, err := headless.NewWindow(280, 400)
	if err != nil {
		t.Skipf("无可用的无头 GPU 环境: %v", err)
	}
	defer window.Release()
	m := openNavigationTestModel(t)
	var s navigationState
	s.reset(m.navigation)
	s.tab = navigationBookmarks
	s.rebuildVisible()
	var ops op.Ops
	var router input.Router
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(280, 400)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	s.layout(gtx, continuousTestTheme(), 3, false)
	router.Frame(&ops)
	bounds, ok := navigationSemanticBounds(&router, "chapter")
	if !ok {
		t.Fatal("当前书签未显示")
	}
	if err := window.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	pixels := image.NewRGBA(image.Rect(0, 0, 280, 400))
	if err := window.Screenshot(pixels); err != nil {
		t.Fatal(err)
	}
	row := pixels.RGBAAt(4, bounds.Min.Y+5)
	tab := pixels.RGBAAt(170, 5)
	if row.B < row.R+20 || tab.B < tab.R+20 {
		t.Fatalf("当前目标页或选中标签没有高亮: row=%v tab=%v", row, tab)
	}
}

func TestNavigationKeepsStateOnFailedOpenAndClearsOnNewDocumentOrClose(t *testing.T) {
	m := openNavigationTestModel(t)
	v := viewer{scene: m.scene, viewScene: m.scene, total: 4}
	v.navigation.reset(m.navigation)
	v.navigation.items[0][0].expanded = true
	for _, err := range []error{nil, errors.New("open failed")} {
		v.pendingOpen = true
		v.acceptResponse(result{scene: m.scene, filename: "navigation.ofd", total: 4, err: err}, func() {}, func(string) {})
		if len(v.navigation.items[0]) != 5 || !v.navigation.items[0][0].expanded {
			t.Fatal("打开失败或取消不能丢失原文档导航状态")
		}
	}
	v.pendingOpen = true
	v.acceptResponse(result{scene: &countedScene{Canvas: canvas.New(210, 297)}, filename: "empty.ofd", total: 1,
		navigation: &documentNavigation{}}, func() {}, func(string) {})
	if len(v.navigation.visible) != 0 || len(v.navigation.items[0]) != 0 || len(v.navigation.items[1]) != 0 {
		t.Fatal("新文档不能沿用旧目录或书签")
	}
	v.navigation.reset(m.navigation)
	v.closeDocument(func(request) {})
	if len(v.navigation.visible) != 0 || len(v.navigation.rows) != 0 {
		t.Fatal("关闭文档后必须清空导航引用")
	}
}

func TestNavigationWorkerPublishesMetadataOnceForMemoryInput(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	requests, results := make(chan request, 1), make(chan result, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runWorker(ctx, requests, results, func() {}, func() (string, error) { return "", nil })
	}()
	defer func() {
		cancel()
		<-done
	}()
	var model *documentNavigation
	for _, req := range []request{
		{filename: "memory.ofd", input: navigationTestData(t)}, {page: 1}, {metadata: true}, {pages: []int{2}}, {choose: true},
		{filename: filepath.Join(t.TempDir(), "missing.ofd")},
	} {
		requests <- req
		select {
		case response := <-results:
			if response.navigation == nil || len(response.navigation.outlines) != 5 {
				t.Fatalf("worker 未发布导航快照: %+v", response)
			}
			if model != nil && model != response.navigation {
				t.Fatal("翻页或打开失败不能重复构建或更换导航模型")
			}
			model = response.navigation
		case <-time.After(20 * time.Second):
			t.Fatal("导航元数据发布超时")
		}
	}
}

func TestNavigationToggleReplacesThumbnailsAndEscapeDoesNotExit(t *testing.T) {
	v := viewer{scene: &countedScene{Canvas: canvas.New(210, 297)}, total: 1}
	v.thumbnails.open = true
	settings := viewerSettings{Thumbnails: true}
	v.navigationButton.Click()
	v.handleNavigation(layout.Context{}, &settings, func() {})
	if !v.navigation.open || v.thumbnails.open || !settings.Navigation || settings.Thumbnails {
		t.Fatal("打开导航应替换缩略图并保存偏好")
	}
	var ops op.Ops
	var router input.Router
	gtx := layout.Context{Ops: &ops, Source: router.Source(), Constraints: layout.Exact(image.Pt(900, 700)), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	exited := false
	v.handleKeys(gtx, func(request) {}, func() { exited = true })
	v.layout(gtx, continuousTestTheme())
	router.Frame(&ops)
	router.Queue(key.Event{Name: key.NameEscape, State: key.Press})
	v.handleKeys(gtx, func(request) {}, func() { exited = true })
	if exited || v.navigation.open {
		t.Fatal("Esc 应先关闭导航侧栏，不退出阅读器")
	}
	v.navigation.open, v.compactToolbar = true, true
	v.handleAndroidBack(layout.Context{}, func(request) { t.Fatal("不能关闭文档") }, func() { t.Fatal("不能退出阅读器") })
	if v.navigation.open || v.total != 1 {
		t.Fatal("Android 返回应仅关闭导航浮层")
	}
}

func navigationSemanticBounds(router *input.Router, description string) (image.Rectangle, bool) {
	nodes := router.AppendSemantics(nil)
	for len(nodes) > 0 {
		node := nodes[0]
		nodes = append(nodes[1:], node.Children...)
		if node.Desc.Description == description {
			return node.Desc.Bounds, true
		}
	}
	return image.Rectangle{}, false
}
