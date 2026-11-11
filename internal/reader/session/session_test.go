package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/reader"
)

func TestOpenPagesAndRenderNativeDocument(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "session-test", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	pages, err := s.Pages()
	if err != nil || len(pages) != 1 || pages[0].Width <= 0 || pages[0].Height <= 0 {
		t.Fatalf("页面信息 = %v, %v", pages, err)
	}
	var output bytes.Buffer
	if err := s.RenderPage(t.Context(), &output, 0, reader.RenderOptions{DPI: 24}); err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(output.Bytes())); err != nil {
		t.Fatalf("渲染结果不是有效 PNG: %v", err)
	}
}

type blockingDocument struct {
	started  chan struct{}
	canceled chan struct{}
	release  chan struct{}
	active   atomic.Int32
	peak     atomic.Int32
	closed   atomic.Int32
}

func (d *blockingDocument) Info() (reader.DocumentInfo, error)                         { return reader.DocumentInfo{}, nil }
func (d *blockingDocument) FontList() ([]reader.FontInfo, error)                       { return nil, nil }
func (d *blockingDocument) Stats() (reader.DocumentStats, error)                       { return reader.DocumentStats{}, nil }
func (d *blockingDocument) Attachments() ([]reader.AttachmentInfo, error)              { return nil, nil }
func (d *blockingDocument) Media() ([]reader.MediaInfo, error)                         { return nil, nil }
func (d *blockingDocument) MediaData(int, uint64, int64) ([]byte, error)               { return nil, nil }
func (d *blockingDocument) CheckDocumentPermission(reader.PermissionAction, int) error { return nil }
func (d *blockingDocument) AttachmentData(int, string, int64) ([]byte, error)          { return nil, nil }
func (d *blockingDocument) AnnotationsForPage(int) ([]reader.AnnotationInfo, error)    { return nil, nil }
func (d *blockingDocument) PageLinksForPage(int) ([]reader.PageLink, error)            { return nil, nil }
func (d *blockingDocument) Signatures() ([]reader.SignatureInfo, error)                { return nil, nil }
func (d *blockingDocument) SignatureValue(int, string) ([]byte, error)                 { return nil, nil }
func (d *blockingDocument) SignatureSeal(int, string, int) ([]byte, string, error) {
	return nil, "", nil
}
func (d *blockingDocument) SignatureCertificate(int, string, string) ([]byte, error) { return nil, nil }
func (d *blockingDocument) Layers(int) ([]reader.LayerInfo, error)                   { return nil, nil }
func (d *blockingDocument) SetHiddenLayers(int, []string) error                      { return nil }
func (d *blockingDocument) FontUsage(context.Context, reader.FontRef, reader.FontUsageOptions) (reader.FontUsage, error) {
	return reader.FontUsage{}, nil
}
func (d *blockingDocument) FontUsageAll(context.Context, reader.FontUsageOptions) (reader.FontUsageReport, error) {
	return reader.FontUsageReport{}, nil
}

type blockingSearchDocument struct {
	blockingDocument

	searchStarted chan struct{}
}

type mediaPreviewDocument struct {
	blockingDocument

	data  []byte
	err   error
	limit int64
}

func (d *mediaPreviewDocument) MediaData(_ int, _ uint64, limit int64) ([]byte, error) {
	d.limit = limit
	return d.data, d.err
}

func TestMediaImagePreviewLimitsFormatAndCancellation(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	doc := &mediaPreviewDocument{data: data.Bytes()}
	s := newSession(doc, 1)
	t.Cleanup(func() { _ = s.Close() })
	result, err := s.MediaImagePreview(t.Context(), 0, 1)
	if err != nil || result.MIME != "image/png" || !bytes.Equal(result.Data, data.Bytes()) || doc.limit != 8<<20 {
		t.Fatalf("预览 = %+v, %v", result, err)
	}
	doc.data = []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)
	if _, err := s.MediaImagePreview(t.Context(), 0, 1); err == nil {
		t.Fatal("不能预览 SVG")
	}
	doc.err = reader.ErrPermissionDenied
	if _, err := s.MediaImagePreview(t.Context(), 0, 1); !errors.Is(err, reader.ErrPermissionDenied) {
		t.Fatalf("权限错误 = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.MediaImagePreview(ctx, 0, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误 = %v", err)
	}
}

type blockingListDocument struct {
	blockingDocument

	listStarted chan struct{}
	listRelease chan struct{}
}

func (d *blockingListDocument) Attachments() ([]reader.AttachmentInfo, error) {
	d.listStarted <- struct{}{}
	<-d.listRelease
	return []reader.AttachmentInfo{{Name: "附件"}}, nil
}

func (d *blockingListDocument) Media() ([]reader.MediaInfo, error) {
	d.listStarted <- struct{}{}
	<-d.listRelease
	return []reader.MediaInfo{{Name: "资源"}}, nil
}

func TestListMetadataLimitsAndDiscardsCancelledResults(t *testing.T) {
	doc := &blockingListDocument{listStarted: make(chan struct{}, 3), listRelease: make(chan struct{})}
	s := newSession(doc, 1)
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(doc.listRelease) }); _ = s.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	results := make(chan error, 2)
	go func() { _, err := s.Attachments(ctx); results <- err }()
	go func() { _, err := s.Media(t.Context()); results <- err }()
	<-doc.listStarted
	<-doc.listStarted
	queuedCtx, cancelQueued := context.WithCancel(t.Context())
	queued := make(chan error, 1)
	go func() { _, err := s.Media(queuedCtx); queued <- err }()
	cancelQueued()
	if err := <-queued; !errors.Is(err, context.Canceled) {
		t.Fatalf("排队取消 = %v", err)
	}
	select {
	case <-doc.listStarted:
		t.Fatal("超过清单读取并发上限")
	default:
	}
	cancel()
	release.Do(func() { close(doc.listRelease) })
	first, second := <-results, <-results
	if (!errors.Is(first, context.Canceled) || second != nil) && (!errors.Is(second, context.Canceled) || first != nil) {
		t.Fatalf("运行中取消 = %v, %v", first, second)
	}
}

func (d *blockingSearchDocument) Search(ctx context.Context, _ string, _ reader.SearchOptions) (reader.SearchReport, error) {
	close(d.searchStarted)
	<-ctx.Done()
	return reader.SearchReport{}, ctx.Err()
}

func TestCloseCancelsActiveSearch(t *testing.T) {
	doc := &blockingSearchDocument{searchStarted: make(chan struct{})}
	s := newSession(doc, 1)
	result := make(chan error, 1)
	go func() {
		_, err := s.Search(context.Background(), "test", reader.SearchOptions{})
		result <- err
	}()
	<-doc.searchStarted
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("关闭时搜索错误 = %v", err)
	}
}

type signatureSummaryDocument struct {
	blockingDocument

	infos []reader.SignatureInfo
}

func (d *signatureSummaryDocument) Signatures() ([]reader.SignatureInfo, error) { return d.infos, nil }

func TestSignatureSummariesPreserveStampsWithoutVerification(t *testing.T) {
	box := &reader.AnnotationBoundary{X: 1, Width: 3, Height: 4}
	doc := &signatureSummaryDocument{infos: []reader.SignatureInfo{{ID: "s1", Provider: "测试", Version: "v1", HasVerification: true, Verified: true,
		Stamps: []reader.SignatureStamp{{ID: "a", Page: 2, Boundary: box}, {ID: "b", Page: -1}, {ID: "c", Page: 2}}}}}
	s := newSession(doc, 1)
	t.Cleanup(func() { _ = s.Close() })
	items, err := s.SignatureSummaries(t.Context())
	if err != nil || len(items) != 1 || items[0].Version != "v1" || len(items[0].Stamps) != 3 || items[0].Stamps[1].Page != -1 || items[0].Stamps[2].Page != 2 {
		t.Fatalf("签名声明 = %+v, %v", items, err)
	}
	items[0].Stamps[0].Boundary.X = 9
	if box.X != 1 {
		t.Fatal("签章边界不能修改阅读器缓存")
	}
	encoded, err := json.Marshal(items)
	if err != nil || bytes.Contains(encoded, []byte("verified")) {
		t.Fatalf("不应暴露验签结果 = %s, %v", encoded, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.SignatureSummaries(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误 = %v", err)
	}
}

type blockedPageMetadataDocument struct {
	blockingSearchDocument

	pageStarted chan int
	pageRelease chan struct{}
}

func (d *blockedPageMetadataDocument) AnnotationsForPage(index int) ([]reader.AnnotationInfo, error) {
	d.pageStarted <- index
	<-d.pageRelease
	return nil, nil
}

func (d *blockedPageMetadataDocument) PageLinksForPage(index int) ([]reader.PageLink, error) {
	d.pageStarted <- index
	<-d.pageRelease
	return nil, nil
}

func TestPageMetadataIndependentOfSearchAndBounded(t *testing.T) {
	doc := &blockedPageMetadataDocument{blockingSearchDocument: blockingSearchDocument{searchStarted: make(chan struct{})},
		pageStarted: make(chan int, 3), pageRelease: make(chan struct{})}
	s := newSession(doc, 1)
	searchCtx, cancelSearch := context.WithCancel(t.Context())
	defer cancelSearch()
	searchDone := make(chan error, 1)
	go func() { _, err := s.Search(searchCtx, "test", reader.SearchOptions{}); searchDone <- err }()
	<-doc.searchStarted
	pageDone := make(chan error, 3)
	go func() { _, err := s.AnnotationsForPage(t.Context(), 0); pageDone <- err }()
	go func() { _, err := s.PageLinksForPage(t.Context(), 1); pageDone <- err }()
	for range 2 {
		select {
		case <-doc.pageStarted:
		case <-time.After(3 * time.Second):
			close(doc.pageRelease)
			cancelSearch()
			t.Fatal("搜索不应阻塞逐页元数据")
		}
	}
	queuedCtx, cancelQueued := context.WithCancel(t.Context())
	go func() { _, err := s.AnnotationsForPage(queuedCtx, 2); pageDone <- err }()
	cancelQueued()
	if err := <-pageDone; !errors.Is(err, context.Canceled) {
		t.Errorf("排队取消错误 = %v", err)
	}
	select {
	case index := <-doc.pageStarted:
		t.Errorf("超过逐页元数据并发上限：%d", index)
	default:
	}
	close(doc.pageRelease)
	for range 2 {
		if err := <-pageDone; err != nil {
			t.Errorf("逐页读取失败：%v", err)
		}
	}
	cancelSearch()
	if err := <-searchDone; !errors.Is(err, context.Canceled) {
		t.Errorf("搜索取消错误 = %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentPageMetadataAndAnnotationScan(t *testing.T) {
	data, err := creator.Marshal(creator.Document{ID: "metadata-concurrent", Pages: []creator.Page{{}, {}, {}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(data, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	errorsOut := make(chan error, 3)
	go func() { _, e := s.Annotations(t.Context()); errorsOut <- e }()
	go func() { _, e := s.AnnotationsForPage(t.Context(), 0); errorsOut <- e }()
	go func() { _, e := s.PageLinksForPage(t.Context(), 0); errorsOut <- e }()
	for range 3 {
		if err := <-errorsOut; err != nil {
			t.Errorf("并发元数据读取失败：%v", err)
		}
	}
}

func TestCloseCancelsQueuedPageMetadataAndWaitsForActiveReads(t *testing.T) {
	doc := &blockedPageMetadataDocument{pageStarted: make(chan int, 3), pageRelease: make(chan struct{})}
	s := newSession(doc, 1)
	results := make(chan error, 3)
	for index := range 2 {
		go func() { _, err := s.AnnotationsForPage(context.Background(), index); results <- err }()
	}
	<-doc.pageStarted
	<-doc.pageStarted
	go func() { _, err := s.PageLinksForPage(context.Background(), 2); results <- err }()
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	<-s.ctx.Done()
	select {
	case err := <-closed:
		t.Errorf("读取尚未结束时会话已关闭: %v", err)
	default:
	}
	close(doc.pageRelease)
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := <-results; !errors.Is(err, context.Canceled) && !errors.Is(err, ErrClosed) {
			t.Errorf("关闭后逐页读取错误 = %v", err)
		}
	}
}

type blockingFontUsageDocument struct {
	blockingDocument

	started chan struct{}
}

type blockingLayerUpdateDocument struct {
	blockingDocument

	started       chan struct{}
	releaseUpdate chan struct{}
}

func (d *blockingLayerUpdateDocument) SetHiddenLayers(int, []string) error {
	close(d.started)
	<-d.releaseUpdate
	return nil
}

func TestCloseWaitsForStartedLayerUpdate(t *testing.T) {
	doc := &blockingLayerUpdateDocument{started: make(chan struct{}), releaseUpdate: make(chan struct{})}
	s := newSession(doc, 1)
	result := make(chan error, 1)
	go func() { result <- s.SetHiddenLayers(0, nil) }()
	<-doc.started
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	<-s.ctx.Done()
	select {
	case err := <-closed:
		t.Errorf("图层写入尚未结束就关闭: %v", err)
	default:
	}
	close(doc.releaseUpdate)
	if err := <-result; err != nil {
		t.Errorf("已开始的图层写入错误 = %v", err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if err := s.SetHiddenLayers(0, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("关闭后写入错误 = %v", err)
	}
}

type blockingAnnotationDocument struct {
	blockingDocument

	started           chan struct{}
	releaseAnnotation chan struct{}
}

func (d *blockingAnnotationDocument) AnnotationsForPage(int) ([]reader.AnnotationInfo, error) {
	close(d.started)
	<-d.releaseAnnotation
	return []reader.AnnotationInfo{{Remark: "旧结果"}}, nil
}

func (d *blockingAnnotationDocument) Pages() ([]reader.PageInfo, error) {
	return make([]reader.PageInfo, 3), nil
}

func TestAnnotationCancellationDiscardsInFlightResult(t *testing.T) {
	doc := &blockingAnnotationDocument{started: make(chan struct{}), releaseAnnotation: make(chan struct{})}
	s := newSession(doc, 1)
	t.Cleanup(func() { _ = s.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		items, err := s.AnnotationsForPage(ctx, 0)
		if len(items) != 0 {
			result <- errors.New("取消后不应返回注解")
			return
		}
		result <- err
	}()
	<-doc.started
	cancel()
	close(doc.releaseAnnotation)
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误 = %v", err)
	}
}

func TestAnnotationScanCancellationDiscardsPartialReport(t *testing.T) {
	doc := &blockingAnnotationDocument{started: make(chan struct{}), releaseAnnotation: make(chan struct{})}
	s := newSession(doc, 1)
	t.Cleanup(func() { _ = s.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		report, err := s.Annotations(ctx)
		if report.Items != nil || report.Scanned != 0 {
			result <- errors.New("取消后不应返回注解报告")
			return
		}
		result <- err
	}()
	<-doc.started
	cancel()
	close(doc.releaseAnnotation)
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误 = %v", err)
	}
}

type annotationScanDocument struct {
	blockingDocument

	pages  [][]reader.AnnotationInfo
	failAt int
	calls  []int
}

func (d *annotationScanDocument) Pages() ([]reader.PageInfo, error) {
	return make([]reader.PageInfo, len(d.pages)), nil
}

func (d *annotationScanDocument) AnnotationsForPage(index int) ([]reader.AnnotationInfo, error) {
	d.calls = append(d.calls, index)
	if index == d.failAt {
		return nil, errors.New("读取失败")
	}
	return d.pages[index], nil
}

func TestAnnotationScanBoundsAndDiscardsPartialErrors(t *testing.T) {
	doc := &annotationScanDocument{failAt: -1, pages: [][]reader.AnnotationInfo{
		{{ID: "a"}, {ID: "b"}}, {{ID: "c"}, {ID: "d"}}, {{ID: "e"}},
	}}
	s := newSession(doc, 1)
	t.Cleanup(func() { _ = s.Close() })
	report, err := s.annotations(t.Context(), 3, 3)
	if err != nil || !report.Truncated || report.Scanned != 2 || len(report.Items) != 3 || len(doc.calls) != 2 {
		t.Fatalf("条数上限 = %+v, 调用 %v, %v", report, doc.calls, err)
	}
	doc.calls = nil
	report, err = s.annotations(t.Context(), 2, 10)
	if err != nil || !report.Truncated || report.Scanned != 2 || len(report.Items) != 4 {
		t.Fatalf("页数上限 = %+v, %v", report, err)
	}
	doc.failAt = 1
	report, err = s.annotations(t.Context(), 3, 10)
	if err == nil || report.Scanned != 0 || report.Items != nil {
		t.Fatalf("读取失败不应返回部分结果 = %+v, %v", report, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.annotations(ctx, 3, 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误 = %v", err)
	}
}

func (d *blockingFontUsageDocument) FontUsageAll(ctx context.Context, _ reader.FontUsageOptions) (reader.FontUsageReport, error) {
	close(d.started)
	<-ctx.Done()
	return reader.FontUsageReport{}, ctx.Err()
}

func TestCloseCancelsActiveFontUsage(t *testing.T) {
	doc := &blockingFontUsageDocument{started: make(chan struct{})}
	s := newSession(doc, 1)
	result := make(chan error, 1)
	go func() {
		_, err := s.FontUsageAll(context.Background(), reader.FontUsageOptions{})
		result <- err
	}()
	<-doc.started
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("关闭时字体统计错误 = %v", err)
	}
}

func (d *blockingDocument) Pages() ([]reader.PageInfo, error)                  { return nil, nil }
func (d *blockingDocument) Page(int) (reader.PageInfo, error)                  { return reader.PageInfo{}, nil }
func (d *blockingDocument) Text(int) ([]reader.TextRun, error)                 { return nil, nil }
func (d *blockingDocument) Permissions() ([]reader.DocumentPermissions, error) { return nil, nil }
func (d *blockingDocument) Outline() (reader.OutlineTree, error)               { return reader.OutlineTree{}, nil }
func (d *blockingDocument) Preferences() (reader.ViewPreferences, error) {
	return reader.ViewPreferences{}, nil
}
func (d *blockingDocument) CheckPermission(reader.PermissionAction, []int) error { return nil }
func (d *blockingDocument) Search(context.Context, string, reader.SearchOptions) (reader.SearchReport, error) {
	return reader.SearchReport{}, nil
}

func (d *blockingDocument) RenderPDF(ctx context.Context, output io.Writer, _ []int, options reader.RenderOptions) error {
	return d.RenderPage(ctx, output, 0, options)
}

func (d *blockingDocument) RenderPage(ctx context.Context, _ io.Writer, _ int, _ reader.RenderOptions) error {
	active := d.active.Add(1)
	defer d.active.Add(-1)
	for {
		peak := d.peak.Load()
		if active <= peak || d.peak.CompareAndSwap(peak, active) {
			break
		}
	}
	d.started <- struct{}{}
	select {
	case <-ctx.Done():
		// 模拟不能立即中断的底层绘制，保证 Close 等待资源释放。
		if d.canceled != nil {
			close(d.canceled)
		}
		<-d.release
		return ctx.Err()
	case <-d.release:
		return nil
	}
}

func (d *blockingDocument) Close() error {
	d.closed.Add(1)
	return nil
}

func TestCloseCancelsRendersWaitsForCompletionAndRejectsNewOperations(t *testing.T) {
	doc := &blockingDocument{started: make(chan struct{}, 2), canceled: make(chan struct{}), release: make(chan struct{})}
	s := newSession(doc, 1)
	renderDone := make(chan error, 1)
	go func() { renderDone <- s.RenderPage(context.Background(), io.Discard, 0, reader.RenderOptions{}) }()
	<-doc.started
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	<-s.ctx.Done()
	<-doc.canceled
	if _, err := s.Pages(); !errors.Is(err, ErrClosed) {
		t.Fatalf("关闭后 Pages 错误 = %v", err)
	}
	if doc.closed.Load() != 0 {
		t.Fatal("渲染未退出时不能关闭底层文档")
	}
	close(doc.release)
	if err := <-renderDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("关闭时渲染错误 = %v", err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	var closers sync.WaitGroup
	for range 8 {
		closers.Go(func() {
			if err := s.Close(); err != nil {
				t.Errorf("重复关闭: %v", err)
			}
		})
	}
	closers.Wait()
	if doc.closed.Load() != 1 {
		t.Fatalf("底层文档关闭次数 = %d，期望 1", doc.closed.Load())
	}
}

func TestRenderQueueHonorsLimitAndCancellation(t *testing.T) {
	doc := &blockingDocument{started: make(chan struct{}, 3), release: make(chan struct{})}
	s := newSession(doc, 1)
	var wg sync.WaitGroup
	wg.Go(func() { _ = s.RenderPage(context.Background(), io.Discard, 0, reader.RenderOptions{}) })
	<-doc.started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.RenderPage(ctx, io.Discard, 0, reader.RenderOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("已取消请求错误 = %v", err)
	}
	queuedCtx, cancelQueued := context.WithCancel(context.Background())
	queuedDone := make(chan error, 1)
	go func() { queuedDone <- s.RenderPage(queuedCtx, io.Discard, 2, reader.RenderOptions{}) }()
	cancelQueued()
	if err := <-queuedDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("取消排队请求错误 = %v", err)
	}
	wg.Go(func() { _ = s.RenderPage(context.Background(), io.Discard, 1, reader.RenderOptions{}) })
	close(doc.release)
	wg.Wait()
	if doc.peak.Load() != 1 {
		t.Fatalf("最大并发数 = %d，期望 1", doc.peak.Load())
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRenderAllowsConfiguredParallelism(t *testing.T) {
	doc := &blockingDocument{started: make(chan struct{}, 2), release: make(chan struct{})}
	s := newSession(doc, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { _ = s.RenderPage(context.Background(), io.Discard, 0, reader.RenderOptions{}) })
	}
	<-doc.started
	<-doc.started
	if doc.peak.Load() != 2 {
		t.Fatalf("最大并发数 = %d，期望 2", doc.peak.Load())
	}
	close(doc.release)
	wg.Wait()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRejectsInvalidRenderLimit(t *testing.T) {
	for _, limit := range []int{-1, 33} {
		if _, err := Open(nil, Options{MaxConcurrentRenders: limit}); err == nil {
			t.Fatalf("并发数 %d 应被拒绝", limit)
		}
	}
}
