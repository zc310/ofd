package parser

import (
	"bytes"
	"encoding/xml"
	"path/filepath"
	"testing"
	"time"

	"github.com/klauspost/compress/zip"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
)

func TestDrawParamSampleParsesAndResolvesStyles(t *testing.T) {
	ofd, err := NewOFD(filepath.Join("..", "..", "test", "testdata", "drawparam.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	if len(ofd.Documents) != 1 {
		t.Fatalf("document count = %d, want 1", len(ofd.Documents))
	}
	doc := ofd.Documents[0]
	for _, test := range []struct {
		id        uint64
		lineWidth float64
		cap       string
		join      string
		stroke    string
	}{
		{10, 0.353, "Butt", "Miter", "0 0 0"},
		{12, 3, "Round", "Round", "0 0 200"},
		{20, 4, "Butt", "Miter", "0 100 255"},
		{21, 3, "Round", "Round", "255 100 0"},
		{22, 1, "Butt", "Miter", "0 180 0"},
	} {
		dp := doc.GetDrawParam(models.StID(test.id))
		if dp == nil {
			t.Fatalf("DrawParam %d is nil", test.id)
		}
		if dp.LineWidth != test.lineWidth || dp.Cap != test.cap || dp.Join != test.join {
			t.Fatalf("DrawParam %d = width %g cap %q join %q", test.id, dp.LineWidth, dp.Cap, dp.Join)
		}
		if dp.StrokeColor == nil || dp.StrokeColor.Value == nil || dp.StrokeColor.Value.String() != test.stroke {
			t.Fatalf("DrawParam %d stroke color = %#v, want %q", test.id, dp.StrokeColor, test.stroke)
		}
	}

	for _, page := range doc.Pages {
		if err := page.EnsureLoaded(); err != nil {
			t.Fatal(err)
		}
	}
	path := findPathAt(doc.Pages[0].Content().Layer[0].Items, 1)
	if path.LineWidth != 1 {
		t.Fatalf("PathObject LineWidth = %g, want 1", path.LineWidth)
	}
	if path.StrokeColor == nil || path.StrokeColor.Value == nil {
		t.Fatal("PathObject StrokeColor was not parsed")
	}
	miterPath := findPathByID(doc.Pages[1].Content().Layer[0].Items, 51)
	if miterPath == nil || miterPath.LineWidth != 3 {
		if miterPath == nil {
			t.Fatal("page 2 PathObject ID=51 was not parsed")
		}
		t.Fatalf("page 2 PathObject ID=51 LineWidth = %g, want 3", miterPath.LineWidth)
	}
	drawParamPath := findPathByID(doc.Pages[3].Content().Layer[0].Items, 31)
	if drawParamPath == nil || drawParamPath.DrawParam != 18 {
		if drawParamPath == nil {
			t.Fatal("page 4 PathObject ID=31 was not parsed")
		}
		t.Fatalf("PathObject ID=31 DrawParam = %d, want 18", drawParamPath.DrawParam)
	}
}

// findPathByID 从页面块文档序列表中查找指定 ID 的路径对象。
func findPathByID(items []models.PageItem, id models.StID) *models.PathObject {
	for i := range items {
		if items[i].Kind != models.PageItemPath {
			continue
		}
		if items[i].Path.ID == id {
			return items[i].Path
		}
	}
	return nil
}

// findPathAt 返回页面块文档序列表中第 n 个路径对象。
func findPathAt(items []models.PageItem, n int) *models.PathObject {
	for i := range items {
		if items[i].Kind != models.PageItemPath {
			continue
		}
		if n == 0 {
			return items[i].Path
		}
		n--
	}
	return nil
}

func TestNewOFDWithOptionsRejectsNegativeLimits(t *testing.T) {
	if _, err := NewOFDWithOptions(nil, Options{MaxInputBytes: -1}); err == nil {
		t.Fatal("负数输入大小应返回错误")
	}
}

func TestNewOFDWithOptionsRejectsNegativeCacheLimits(t *testing.T) {
	if _, err := NewOFDWithOptions(nil, Options{PageCacheCapacity: -1}); err == nil {
		t.Fatal("负数页面缓存数量应返回错误")
	}
	if _, err := NewOFDWithOptions(nil, Options{PageCacheBytes: -1}); err == nil {
		t.Fatal("负数页面缓存大小应返回错误")
	}
}

func TestPageCacheEvictsOldestLoadedPage(t *testing.T) {
	doc := &Document{pageCacheSize: 2}
	loads := make([]int, 3)
	pages := make([]*Page, 3)
	for index := range pages {
		pageIndex := index
		pages[index] = &Page{
			document: doc,
			load: func(page *Page) error {
				loads[pageIndex]++
				page.pageContent = models.PageContent{Content: &models.Content{}}
				return nil
			},
		}
	}
	for _, page := range pages[:2] {
		if err := page.EnsureLoaded(); err != nil {
			t.Fatal(err)
		}
	}
	if err := pages[2].EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if pages[0].loaded || pages[0].pageContent.Content != nil {
		t.Fatal("最旧页面未被淘汰")
	}
	if !pages[1].loaded || !pages[2].loaded {
		t.Fatal("最近访问页面不应被淘汰")
	}
	if err := pages[0].EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if loads[0] != 2 {
		t.Fatalf("重新访问页面加载次数 = %d, want 2", loads[0])
	}
	if pages[1].loaded || pages[1].pageContent.Content != nil {
		t.Fatal("重新加载后最旧页面未被淘汰")
	}
}

func TestPageCacheUsesPageZipEntrySize(t *testing.T) {
	ofd, err := NewOFD(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	doc := ofd.Documents[0]
	page := doc.Document.Pages.Pages[0]
	pagePath := page.BaseLoc.Resolve(doc.BaseLoc).String()
	entry, ok := doc.FileCache.Lookup(pagePath)
	if !ok || entry.UncompressedSize == 0 {
		t.Fatalf("页面 XML ZIP 条目不存在或大小为空: %s", pagePath)
	}
	if err := doc.Pages[0].EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if got := doc.pageCache.Weight(); got != int64(entry.UncompressedSize) {
		t.Fatalf("页面缓存权重 = %d, want ZIP 解压大小 %d", got, entry.UncompressedSize)
	}
}

func TestPageArchiveSizeIncludesUniqueResourceXML(t *testing.T) {
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	entries := map[string]string{
		"Doc_0/Pages/Page_0/Content.xml": "page-content",
		"Doc_0/Pages/Page_0/Res.xml":     "resource",
	}
	for name, value := range entries {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	fileCache, err := core.OpenBytes(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer fileCache.Close()
	pagePath := models.StLoc("Doc_0/Pages/Page_0/Content.xml")
	resourceSize := int64(len("resource"))
	want := int64(len("page-content")) + resourceSize
	if got := pageArchiveSize(fileCache, pagePath, []models.StLoc{"Res.xml", "Res.xml"}); got != want {
		t.Fatalf("页面归档权重 = %d, want %d", got, want)
	}
}

func TestReadPagePhysicalBoxOnlyReadsArea(t *testing.T) {
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	content := `<?xml version="1.0" encoding="UTF-8"?>
<Page>
  <Area><PhysicalBox>0 0 100 200</PhysicalBox></Area>
  <Content><Layer><TextObject>This content must not be decoded.</TextObject></Layer></Content>
</Page>`
	writer, err := archive.Create("Pages/Page_0/Content.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}

	fileCache, err := core.OpenBytes(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer fileCache.Close()

	box, err := readPagePhysicalBox(fileCache, models.StLoc("Pages/Page_0/Content.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if box != (models.StBox{Width: 100, Height: 200}) {
		t.Fatalf("页面尺寸 = %+v, want 100 x 200", box)
	}
}

func TestPageCacheEvictsPageResources(t *testing.T) {
	doc := &Document{pageCacheSize: 1, pageCacheBytes: 1 << 20}
	media := &models.MultiMedia{}
	page := &Page{
		document: doc,
		load: func(page *Page) error {
			page.pageContent = models.PageContent{Content: &models.Content{}}
			page.resources.media = map[models.StID]*models.MultiMedia{1: media}
			doc.res = map[models.StID]*models.MultiMedia{1: media}
			return nil
		},
	}
	other := &Page{document: doc, load: func(page *Page) error {
		page.pageContent = models.PageContent{Content: &models.Content{}}
		return nil
	}}
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if err := other.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.res[1]; ok {
		t.Fatal("页面淘汰后资源仍在文档资源表中")
	}
}

func TestPageCacheKeepsSharedPageResources(t *testing.T) {
	doc := &Document{pageCacheSize: 1, pageCacheBytes: 1 << 20}
	media := &models.MultiMedia{}
	load := func(page *Page) error {
		page.pageContent = models.PageContent{Content: &models.Content{}}
		page.resources.media = map[models.StID]*models.MultiMedia{1: media}
		doc.res[1] = media
		return nil
	}
	doc.res = make(map[models.StID]*models.MultiMedia)
	page := &Page{document: doc, load: load}
	other := &Page{document: doc, load: load}
	doc.Pages = []*Page{page, other}
	if err := page.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if err := other.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.res[1]; !ok {
		t.Fatal("共享资源被页面淘汰错误删除")
	}
}

func TestPageCacheRestoresResourcesWithDuplicateIDs(t *testing.T) {
	doc := &Document{pageCacheSize: 2, pageCacheBytes: 1 << 20}
	doc.fontRes = make(map[models.StID]*models.Font)
	doc.compositeUnits = make(map[models.StID]*models.CompositeGraphicUnit)
	doc.drawParams = make(map[models.StID]*models.DrawParam)
	fontOne := &models.Font{ID: 7, FontName: "first"}
	fontTwo := &models.Font{ID: 7, FontName: "second"}
	unitOne := &models.CompositeGraphicUnit{ID: 8}
	unitTwo := &models.CompositeGraphicUnit{ID: 8}
	paramOne := &models.DrawParam{ID: 9}
	paramTwo := &models.DrawParam{ID: 9}
	makePage := func(font *models.Font, unit *models.CompositeGraphicUnit, param *models.DrawParam) *Page {
		return &Page{document: doc, load: func(page *Page) error {
			page.pageContent = models.PageContent{Content: &models.Content{}}
			page.resources = pageResources{
				fonts:      map[models.StID]*models.Font{font.ID: font},
				composites: map[models.StID]*models.CompositeGraphicUnit{unit.ID: unit},
				drawParams: map[models.StID]*models.DrawParam{param.ID: param},
			}
			doc.fontRes[font.ID] = font
			doc.compositeUnits[unit.ID] = unit
			doc.drawParams[param.ID] = param
			return nil
		}}
	}
	first := makePage(fontOne, unitOne, paramOne)
	second := makePage(fontTwo, unitTwo, paramTwo)
	third := &Page{document: doc, load: func(page *Page) error {
		page.pageContent = models.PageContent{Content: &models.Content{}}
		return nil
	}}
	doc.Pages = []*Page{first, second, third}
	if err := first.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if err := second.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if err := first.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if err := third.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if doc.GetFont(7) != fontOne || doc.GetCompositeUnit(8) != unitOne || doc.drawParams[9] != paramOne {
		t.Fatal("淘汰重复 ID 资源后未恢复存活页面的资源")
	}
}

func TestPageCacheKeepsAcquiredPage(t *testing.T) {
	doc := &Document{pageCacheSize: 1}
	page := &Page{document: doc, load: func(page *Page) error {
		page.pageContent = models.PageContent{Content: &models.Content{}}
		return nil
	}}
	other := &Page{document: doc, load: func(page *Page) error {
		page.pageContent = models.PageContent{Content: &models.Content{}}
		return nil
	}}
	lease, err := page.AcquireLease()
	if err != nil {
		t.Fatal(err)
	}
	if err := other.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if !page.loaded || page.pageContent.Content == nil {
		t.Fatal("已固定页面不应被淘汰")
	}
	if other.loaded || other.pageContent.Content != nil {
		t.Fatal("固定页面存在时，新的页面应被优先淘汰")
	}
	lease.Release()
	third := &Page{document: doc, load: func(page *Page) error {
		page.pageContent = models.PageContent{Content: &models.Content{}}
		return nil
	}}
	if err := third.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if page.loaded || page.pageContent.Content != nil {
		t.Fatal("解除固定后页面应允许被后续缓存压力淘汰")
	}
}

func TestWithPageContentHoldsPageLease(t *testing.T) {
	page := NewPage(models.PageContent{Content: &models.Content{}})
	called := false
	err := page.WithPageContent(func(content *models.PageContent) error {
		called = content != nil && content.Content != nil
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("页面内容回调未收到已加载内容")
	}
}

func TestPageLeaseReleaseIsIdempotent(t *testing.T) {
	page := NewPage(models.PageContent{Content: &models.Content{}})
	lease, err := page.AcquireLease()
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	lease.Release()
	if lease.Content() != nil {
		t.Fatal("释放后的租约不应继续返回页面内容")
	}
	if page.pinCount.Load() != 0 {
		t.Fatalf("页面租约计数 = %d, want 0", page.pinCount.Load())
	}
}

func TestPageLeaseWithContentRejectsReleasedLease(t *testing.T) {
	page := NewPage(models.PageContent{Content: &models.Content{}})
	lease, err := page.AcquireLease()
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	if err := lease.WithContent(func(*models.PageContent) error { return nil }); err == nil {
		t.Fatal("释放后的租约不应继续访问页面内容")
	}
}

func TestAcquireMarksStandalonePageLoaded(t *testing.T) {
	page := &Page{}
	lease, err := page.AcquireLease()
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	if !page.loaded {
		t.Fatal("无文档页面 Acquire 后应标记为已加载")
	}
}

func TestPhysicalBoxReturnsValueCopy(t *testing.T) {
	page := NewPage(models.PageContent{Area: &models.CtPageArea{PhysicalBox: models.StBox{Width: 100, Height: 200}}})
	box, err := page.PhysicalBox()
	if err != nil {
		t.Fatal(err)
	}
	box.Width = 999
	original, err := page.PhysicalBox()
	if err != nil {
		t.Fatal(err)
	}
	if original.Width != 100 {
		t.Fatalf("页面物理区域被外部修改: %v", original.Width)
	}
}

func TestPageCacheDoesNotEvictPageDuringConcurrentLoad(t *testing.T) {
	doc := &Document{pageCacheSize: 1}
	started := make(chan struct{})
	continueLoad := make(chan struct{})
	page := &Page{document: doc, load: func(page *Page) error {
		close(started)
		<-continueLoad
		page.pageContent = models.PageContent{Content: &models.Content{}}
		return nil
	}}
	other := &Page{document: doc, load: func(page *Page) error {
		page.pageContent = models.PageContent{Content: &models.Content{}}
		return nil
	}}
	third := &Page{document: doc, load: func(page *Page) error {
		page.pageContent = models.PageContent{Content: &models.Content{}}
		return nil
	}}

	acquired := make(chan error, 1)
	pageLease := make(chan *PageLease, 1)
	go func() {
		lease, err := page.AcquireLease()
		if err == nil {
			pageLease <- lease
		}
		acquired <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("页面加载未开始")
	}
	close(continueLoad)
	if err := <-acquired; err != nil {
		t.Fatal(err)
	}
	lease := <-pageLease
	if err := other.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if err := third.EnsureLoaded(); err != nil {
		t.Fatal(err)
	}
	if err := page.WithPageContent(func(content *models.PageContent) error {
		if content.Content == nil {
			t.Fatal("并发缓存压力下固定页面内容被清空")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	lease.Release()
}

func TestDocumentCloseWaitsForPageLease(t *testing.T) {
	doc := &Document{}
	page := NewPage(models.PageContent{Content: &models.Content{}})
	page.document = doc
	doc.Pages = []*Page{page}
	lease, err := page.AcquireLease()
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() {
		doc.clearCaches()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("文档清理不应在页面租约释放前完成")
	case <-time.After(20 * time.Millisecond):
	}
	lease.Release()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("页面租约释放后文档清理未完成")
	}
	if page.loaded || page.pageContent.Content != nil {
		t.Fatal("文档清理后页面内容仍存在")
	}
}

func TestMetadataFieldsArePrivate(t *testing.T) {
	doc := &Document{}
	if doc.GetCustomTags() != nil || doc.GetExtensions() != nil || doc.GetVersion("missing") != nil {
		t.Fatal("未配置附属数据时应返回 nil")
	}
}

func TestAttachmentsLoadOnDemand(t *testing.T) {
	doc := &Document{attachmentsPath: "Attachments.xml"}
	if got := doc.GetAttachments(); got != nil {
		t.Fatalf("无文件缓存时附件返回 = %+v", got)
	}
}

func TestAnnotationsLoadOnDemand(t *testing.T) {
	doc := &Document{
		annotations:         make(map[models.StID]*models.PageAnnot),
		annotationLocations: make(map[models.StID]models.StLoc),
	}
	if got := doc.GetAnnotation(1); got != nil {
		t.Fatalf("未登记注解页面返回 = %+v", got)
	}
	if len(doc.annotations) != 0 {
		t.Fatalf("未加载注解缓存 = %d, want 0", len(doc.annotations))
	}
}

func TestGetDrawParamPreservesExplicitZeroOverrides(t *testing.T) {
	var params models.DrawParams
	if err := xml.Unmarshal([]byte(`<DrawParams>
  <DrawParam ID="1" LineWidth="2" DashOffset="4" MiterLimit="8"/>
  <DrawParam ID="2" Relative="1" LineWidth="0" DashOffset="0" MiterLimit="0"/>
</DrawParams>`), &params); err != nil {
		t.Fatal(err)
	}

	drawParams := make(map[models.StID]*models.DrawParam, len(params.DrawParam))
	for _, param := range params.DrawParam {
		drawParams[param.ID] = param
	}
	doc := &Document{drawParams: drawParams}
	got := doc.GetDrawParam(2)
	if got == nil {
		t.Fatal("resolved DrawParam is nil")
	}
	if got.LineWidth != 0 || got.DashOffset != 0 || got.MiterLimit != 0 {
		t.Fatalf("explicit zero overrides = width %g offset %g miter %g", got.LineWidth, got.DashOffset, got.MiterLimit)
	}
}
