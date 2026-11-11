package session

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/permission"
	"github.com/zc310/ofd/pkg/reader"
	"github.com/zc310/ofd/pkg/spec"
	"golang.org/x/image/font/gofont/goregular"
)

func TestSignatureDetailsExposeRealSignedDocumentChecks(t *testing.T) {
	for _, name := range []string{"zsbk.ofd", "999.ofd"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "..", "..", "testdata", "ofdrw", name)
			data, err := os.ReadFile(path)
			if errors.Is(err, os.ErrNotExist) {
				t.Skipf("缺少签章样本 %s", path)
			}
			if err != nil {
				t.Fatal(err)
			}
			service := &Service{}
			t.Cleanup(func() { _ = service.Close() })
			opened, err := service.Open(data, Options{})
			if err != nil {
				t.Fatal(err)
			}
			items, err := service.SignatureDetails(t.Context(), opened.PagePrefix)
			if err != nil || len(items) == 0 {
				t.Fatalf("签名详情 = %+v, %v", items, err)
			}
			foundReferences, foundCertificates := false, false
			for _, item := range items {
				if !item.HasDigest {
					t.Fatalf("缺少摘要结果: %+v", item)
				}
				foundReferences = foundReferences || len(item.References) > 0
				foundCertificates = foundCertificates || len(item.Certificates) > 0
			}
			if !foundReferences || !foundCertificates {
				t.Fatalf("缺少引用或证书: %+v", items)
			}
		})
	}
}

func TestOutlinePreservesTargetsAndRejectsStaleSession(t *testing.T) {
	top := 20.0
	data, err := creator.Marshal(creator.Document{
		ID: "outline", Pages: []creator.Page{{}, {}},
		Preferences: &creator.ViewPreferences{PageMode: spec.PageModeUseOutlines},
		Bookmarks:   []creator.Bookmark{{Name: "书签", Goto: creator.GotoAction{Page: 1}}},
		Outlines: []creator.Outline{{Title: "章节", Children: []creator.Outline{{Title: "子项",
			Actions: []creator.Action{{Event: creator.ActionEventClick, Goto: &creator.GotoAction{Page: 1, Type: spec.DestTypeXYZ, Top: &top}}}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	t.Cleanup(func() { _ = s.Close() })
	opened, err := s.Open(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := s.Outline(opened.PagePrefix)
	if err != nil || tree.PageMode != string(spec.PageModeUseOutlines) || len(tree.Nodes) != 1 || len(tree.Nodes[0].Children) != 1 || len(tree.Bookmarks) != 1 {
		t.Fatalf("大纲快照 = %+v, %v", tree, err)
	}
	child := tree.Nodes[0].Children[0]
	if child.Page != 1 || child.Dest == nil || child.Dest.Top == nil || *child.Dest.Top != top || tree.Bookmarks[0].Page != 1 {
		t.Fatalf("大纲或书签目标错误: %+v", tree)
	}
	if _, err := s.Open(data, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Outline(opened.PagePrefix); !errors.Is(err, ErrStaleDocument) {
		t.Fatalf("旧会话大纲错误 = %v", err)
	}
}

func TestInfoPreservesMetadataAndRejectsStaleSession(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "info", Title: "文档标题", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	t.Cleanup(func() { _ = s.Close() })
	opened, err := s.Open(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	info, permissions, err := s.Info(opened.PagePrefix)
	if err != nil || info.Title != "文档标题" || len(permissions) != 1 || !permissions[0].Accessible {
		t.Fatalf("文档信息 = %+v, 权限 = %+v, 错误 = %v", info, permissions, err)
	}
	if _, err := s.Open(data, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Info(opened.PagePrefix); !errors.Is(err, ErrStaleDocument) {
		t.Fatalf("旧会话错误 = %v", err)
	}
}

func TestFontListPreservesScopeAndRejectsStaleSession(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "fonts", Pages: []creator.Page{{}},
		Fonts: []creator.Font{{Name: "ExampleFont"}}})
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	t.Cleanup(func() { _ = s.Close() })
	opened, err := s.Open(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	fonts, err := s.FontList(opened.PagePrefix)
	if err != nil || len(fonts) != 1 || fonts[0].Name != "ExampleFont" || fonts[0].Scope != 0 {
		t.Fatalf("字体列表 = %+v, %v", fonts, err)
	}
	if _, err := s.Open(data, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FontList(opened.PagePrefix); !errors.Is(err, ErrStaleDocument) {
		t.Fatalf("旧会话错误 = %v", err)
	}
}

func TestPreferencesPreserveDocumentZoomAndRejectStaleSession(t *testing.T) {
	zoom := 1.75
	data, err := creator.Marshal(creator.Document{ID: "preferences", Title: "偏好测试", Pages: []creator.Page{{}},
		Preferences: &creator.ViewPreferences{Zoom: &zoom}})
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	t.Cleanup(func() { _ = s.Close() })
	opened, err := s.Open(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	preferences, err := s.Preferences(opened.PagePrefix)
	if err != nil || preferences.Zoom == nil || *preferences.Zoom != zoom {
		t.Fatalf("显示偏好 = %+v, %v", preferences, err)
	}
	if _, err := s.Open(data, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Preferences(opened.PagePrefix); !errors.Is(err, ErrStaleDocument) {
		t.Fatalf("旧会话显示偏好错误 = %v", err)
	}
}

func TestCloseCurrentDoesNotCloseReplacement(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "close-current", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	t.Cleanup(func() { _ = s.Close() })
	old, err := s.Open(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.Open(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CloseCurrent(old.PagePrefix); !errors.Is(err, ErrStaleDocument) {
		t.Fatalf("过期关闭 = %v", err)
	}
	if _, err := s.Page(current.PagePrefix, 0); err != nil {
		t.Fatalf("新文档被关闭: %v", err)
	}
	if err := s.CloseCurrent(current.PagePrefix); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Page(current.PagePrefix, 0); !errors.Is(err, ErrStaleDocument) {
		t.Fatalf("关闭后的页面 = %v", err)
	}
}

func TestServiceReplacesAndClosesDocumentResources(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "service-test", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	t.Cleanup(func() { _ = s.Close() })
	first, err := s.Open(data, Options{})
	if err != nil || len(first.Pages) != 1 {
		t.Fatalf("打开文档 = %v, %v", first, err)
	}
	request := func(path string) int {
		response := httptest.NewRecorder()
		s.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path+"0?dpi=24", nil))
		return response.Code
	}
	if status := request(first.PagePrefix); status != http.StatusOK {
		t.Fatalf("第一页状态 = %d", status)
	}
	if _, err := s.Open([]byte("invalid"), Options{}); err == nil {
		t.Fatal("无效文档应被拒绝")
	}
	if status := request(first.PagePrefix); status != http.StatusOK {
		t.Fatalf("打开失败后旧文档状态 = %d", status)
	}
	second, err := s.Open(data, Options{})
	if err != nil || first.PagePrefix == second.PagePrefix {
		t.Fatalf("切换文档 = %v, %v", second, err)
	}
	if status := request(first.PagePrefix); status != http.StatusNotFound {
		t.Fatalf("旧页面状态 = %d，期望 404", status)
	}
	staleSVG := httptest.NewRecorder()
	s.ServeHTTP(staleSVG, httptest.NewRequestWithContext(t.Context(), http.MethodGet, first.PagePrefix+"0?format=svg", nil))
	if staleSVG.Code != http.StatusNotFound {
		t.Fatalf("旧 SVG 页面状态 = %d，期望 404", staleSVG.Code)
	}
	if status := request(second.PagePrefix); status != http.StatusOK {
		t.Fatalf("新页面状态 = %d", status)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if status := request(second.PagePrefix); status != http.StatusNotFound {
		t.Fatalf("关闭后页面状态 = %d，期望 404", status)
	}
	if _, err := s.Open(data, Options{}); err != nil {
		t.Fatalf("关闭后重新打开失败: %v", err)
	}
}

func TestServiceRechecksPermissionAfterExpiry(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	data, err := creator.Marshal(creator.Document{ID: "validity", Pages: []creator.Page{{}},
		Permissions: &creator.Permissions{ValidPeriod: &creator.ValidPeriod{Start: start, End: end}}})
	if err != nil {
		t.Fatal(err)
	}
	var now atomic.Int64
	now.Store(start.Add(time.Minute).Unix())
	s := &Service{}
	t.Cleanup(func() { _ = s.Close() })
	opened, err := s.Open(data, Options{Reader: reader.OpenOptions{PermissionPolicy: permission.Policy{
		Now: func() time.Time { return time.Unix(now.Load(), 0) },
	}}})
	if err != nil || len(opened.Permissions) != 1 || !opened.Permissions[0].Accessible {
		t.Fatalf("打开权限 = %+v, %v", opened.Permissions, err)
	}
	if err := s.CheckPermission(opened.PagePrefix, reader.PermissionCopy, []int{0}); err != nil {
		t.Fatalf("有效期内复制被拒绝: %v", err)
	}
	if _, err := s.Annotations(t.Context(), opened.PagePrefix); err != nil {
		t.Fatalf("有效期内读取注解失败: %v", err)
	}
	if _, err := s.SignatureSummaries(t.Context(), opened.PagePrefix); err != nil {
		t.Fatalf("有效期内读取签名失败: %v", err)
	}
	if _, err := s.SignatureDetails(t.Context(), opened.PagePrefix); err != nil {
		t.Fatalf("有效期内读取签名详情失败: %v", err)
	}
	now.Store(end.Add(time.Second).Unix())
	if err := s.CheckPermission(opened.PagePrefix, reader.PermissionCopy, []int{0}); !errors.Is(err, reader.ErrPermissionDenied) {
		t.Fatalf("过期后复制检查 = %v", err)
	}
	if err := s.CheckPermission(opened.PagePrefix, reader.PermissionAccess, []int{0}); !errors.Is(err, reader.ErrPermissionDenied) {
		t.Fatalf("过期后注解缓存访问检查 = %v", err)
	}
	if report, err := s.Annotations(t.Context(), opened.PagePrefix); !errors.Is(err, reader.ErrPermissionDenied) || len(report.Items) != 0 {
		t.Fatalf("过期后注解扫描 = %+v, %v", report, err)
	}
	if items, err := s.SignatureSummaries(t.Context(), opened.PagePrefix); !errors.Is(err, reader.ErrPermissionDenied) || len(items) != 0 {
		t.Fatalf("过期后签名声明 = %+v, %v", items, err)
	}
	if items, err := s.SignatureDetails(t.Context(), opened.PagePrefix); !errors.Is(err, reader.ErrPermissionDenied) || len(items) != 0 {
		t.Fatalf("过期后签名详情 = %+v, %v", items, err)
	}
	snapshot, err := s.Permissions(opened.PagePrefix)
	if err != nil || len(snapshot) != 1 || snapshot[0].Accessible || snapshot[0].AccessReason != permission.Expired {
		t.Fatalf("过期快照 = %+v, %v", snapshot, err)
	}
	if err := s.CheckPermission("old", reader.PermissionCopy, []int{0}); !errors.Is(err, ErrStaleDocument) {
		t.Fatalf("旧文档权限检查 = %v", err)
	}
}

func TestServiceTextAndPageRespectDocumentAndCopyPermission(t *testing.T) {
	makeDocument := func(permissions *creator.Permissions) []byte {
		t.Helper()
		data, err := creator.Marshal(creator.Document{ID: "text-test", Permissions: permissions,
			Fonts: []creator.Font{{Name: "Go", Data: goregular.TTF}},
			Pages: []creator.Page{{Items: []creator.Item{creator.Text{Width: 25, Height: 8, Font: "Go", Size: 4, Value: "Example"}}}}})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	s := &Service{}
	t.Cleanup(func() { _ = s.Close() })
	opened, err := s.Open(makeDocument(nil), Options{})
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.Page(opened.PagePrefix, 0)
	if err != nil || page.Index != 0 || page.Width <= 0 || page.Height <= 0 {
		t.Fatalf("页面 = %+v, %v", page, err)
	}
	runs, err := s.Text(opened.PagePrefix, 0)
	if err != nil || len(runs) == 0 || runs[0].Text != "Example" {
		t.Fatalf("文字 = %+v, %v", runs, err)
	}
	report, err := s.Search(t.Context(), opened.PagePrefix, "Exam", reader.SearchOptions{MaxPages: 2, MaxResults: 5})
	if err != nil || len(report.Results) != 1 || report.Results[0].Text != "Example" {
		t.Fatalf("搜索 = %+v, %v", report, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Search(ctx, opened.PagePrefix, "Exam", reader.SearchOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消搜索错误 = %v", err)
	}
	denied := false
	restricted, err := s.Open(makeDocument(&creator.Permissions{Export: &denied}), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Text(opened.PagePrefix, 0); !errors.Is(err, ErrStaleDocument) {
		t.Fatalf("旧文档文字错误 = %v", err)
	}
	if _, err := s.Page(opened.PagePrefix, 0); !errors.Is(err, ErrStaleDocument) {
		t.Fatalf("旧文档页面错误 = %v", err)
	}
	if _, err := s.Text(restricted.PagePrefix, 0); !errors.Is(err, reader.ErrPermissionDenied) {
		t.Fatalf("受限文字错误 = %v", err)
	}
	report, err = s.Search(t.Context(), restricted.PagePrefix, "Exam", reader.SearchOptions{})
	if err != nil || len(report.Results) != 1 || report.Results[0].Text != "Exam" {
		t.Fatalf("受限搜索不应泄露整段文字: %+v, %v", report, err)
	}
	if _, err := s.Search(t.Context(), opened.PagePrefix, "Exam", reader.SearchOptions{}); !errors.Is(err, ErrStaleDocument) {
		t.Fatalf("旧文档搜索错误 = %v", err)
	}
}

func TestServiceConcurrentRenderAndClose(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "service-concurrent", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{}
	result, err := s.Open(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			response := httptest.NewRecorder()
			s.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, result.PagePrefix+"0?dpi=24", nil))
			if response.Code != http.StatusOK && response.Code != http.StatusNotFound && response.Code != http.StatusUnprocessableEntity {
				t.Errorf("并发关闭时状态 = %d", response.Code)
			}
		})
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
}

func TestServiceCloseRevokesAddressAndWaitsForActiveRender(t *testing.T) {
	doc := &blockingDocument{started: make(chan struct{}, 1), canceled: make(chan struct{}), release: make(chan struct{})}
	session := newSession(doc, 1)
	handler, err := NewPageHandler(session)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{session: session, pages: handler}
	response := httptest.NewRecorder()
	renderDone := make(chan struct{})
	go func() {
		s.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), http.MethodGet, handler.Prefix()+"0", nil))
		close(renderDone)
	}()
	<-doc.started
	closeDone := make(chan error, 1)
	go func() { closeDone <- s.Close() }()
	<-doc.canceled
	stale := httptest.NewRecorder()
	s.ServeHTTP(stale, httptest.NewRequestWithContext(t.Context(), http.MethodGet, handler.Prefix()+"0", nil))
	if stale.Code != http.StatusNotFound || doc.closed.Load() != 0 {
		t.Fatalf("关闭期间旧地址状态=%d，底层关闭次数=%d", stale.Code, doc.closed.Load())
	}
	close(doc.release)
	<-renderDone
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusUnprocessableEntity || doc.closed.Load() != 1 {
		t.Fatalf("渲染状态=%d，底层关闭次数=%d", response.Code, doc.closed.Load())
	}
}
