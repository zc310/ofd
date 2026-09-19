package creator

import (
	"bytes"
	"io"
	"runtime"
	"testing"

	"github.com/klauspost/compress/zip"
)

type testPageProvider struct {
	pages []Page
	at    int
}

func (p *testPageProvider) PageCount() int { return len(p.pages) }

func (p *testPageProvider) PageAt(index int) (Page, error) {
	p.at++
	return p.pages[index], nil
}

type generatedPageProvider struct {
	count int
	at    int
}

func (p *generatedPageProvider) PageCount() int { return p.count }

func (p *generatedPageProvider) PageAt(index int) (Page, error) {
	p.at++
	return Page{Items: []Item{Path{X: 1, Y: 1, Width: 2, Height: 2, Data: "M 0 0 L 2 2 C"}}}, nil
}

func TestCreateWithPagesMatchesCreate(t *testing.T) {
	document := Document{
		ID:       "pages-match",
		PageSize: A4,
		Pages: []Page{
			{Items: []Item{Path{X: 1, Y: 1, Width: 2, Height: 2, Data: "M 0 0 L 2 2 C", Actions: []Action{{Event: ActionEventClick, Goto: &GotoAction{Page: 1}}}}}},
			{Items: []Item{Text{X: 3, Y: 3, Width: 10, Height: 5, Value: "page two"}}},
		},
	}
	want, err := Marshal(document)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	meta := document
	meta.Pages = nil
	provider := &testPageProvider{pages: document.Pages}
	got, err := MarshalWithPages(meta, provider, CreateOptions{Compression: CompressionAuto})
	if err != nil {
		t.Fatalf("MarshalWithPages 失败: %v", err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("PageProvider 输出与内存 Document 输出不一致")
	}
	if provider.at < len(document.Pages) {
		t.Fatalf("PageProvider 未被访问: %d", provider.at)
	}
}

func TestCreateWithPagesStreamsLargePageCount(t *testing.T) {
	const pageCount = 50000
	provider := &generatedPageProvider{count: pageCount}
	var buffer bytes.Buffer
	if err := CreateWithPages(Document{ID: "stream-pages", PageSize: A4}, provider, &buffer); err != nil {
		t.Fatalf("CreateWithPages 失败: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatalf("解析生成的 OFD 失败: %v", err)
	}
	pageEntries := 0
	for _, file := range reader.File {
		if len(file.Name) > len(docDir+"/Pages/Page_") && file.Name[:len(docDir)+7] == docDir+"/Pages/" {
			pageEntries++
		}
	}
	if pageEntries != pageCount {
		t.Fatalf("页面条目数 = %d, want %d", pageEntries, pageCount)
	}

	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	if stats.HeapAlloc > 32<<20 {
		t.Fatalf("流式页面创建后常驻内存过大: %d 字节", stats.HeapAlloc)
	}
}

func TestCreateWithPagesRejectsUnsubsettedFonts(t *testing.T) {
	meta := Document{
		ID:       "stream-font",
		PageSize: A4,
		Fonts:    []Font{{Name: "TestFont", Format: "ttf", Data: []byte("\x00\x01\x00\x00font")}},
	}
	provider := &testPageProvider{pages: []Page{{}}}
	err := CreateWithPages(meta, provider, io.Discard)
	if err == nil {
		t.Fatalf("未设置 PreserveEmbeddedFonts 的流式字体应报错")
	}
}
