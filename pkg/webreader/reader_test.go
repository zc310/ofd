package webreader

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestOpenAndRenderPage(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	if reader.PageCount() <= 0 {
		t.Fatalf("page count = %d, want a positive count", reader.PageCount())
	}
	page, err := reader.Page(0)
	if err != nil {
		t.Fatal(err)
	}
	if page.Width <= 0 || page.Height <= 0 {
		t.Fatalf("invalid page size: %+v", page)
	}
	pngData, err := reader.RenderPage(0, RenderOptions{DPI: 36})
	if err != nil {
		t.Fatal(err)
	}
	if len(pngData) == 0 || !bytes.HasPrefix(pngData, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("rendered data is not PNG")
	}
	decoded, _, err := image.Decode(bytes.NewReader(pngData))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Empty() {
		t.Fatal("rendered image is empty")
	}
}

func TestPagesUsesA4PlaceholderAndPageLoadsRealSize(t *testing.T) {
	t.Skip("GBT_33190-2016.ofd")
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "GBT_33190-2016.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	pages, err := reader.Pages()
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != reader.PageCount() {
		t.Fatalf("page count = %d, want %d", len(pages), reader.PageCount())
	}
	if pages[0].Width != defaultPageWidth || pages[0].Height != defaultPageHeight {
		t.Fatalf("Pages()[0] = %+v, want A4 placeholder", pages[0])
	}
	actual, err := reader.Page(0)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Width != 209.9733 || actual.Height != 296.9260 {
		t.Fatalf("Page(0) = %+v, want physical size 209.9733 x 296.9260", actual)
	}
}

func TestRenderPageConcurrent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	workers := reader.PageCount()
	if workers > 4 {
		workers = 4
	}
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(pageIndex int) {
			defer wg.Done()
			pngData, renderErr := reader.RenderPage(pageIndex, RenderOptions{DPI: 36})
			if renderErr != nil {
				errs <- renderErr
				return
			}
			if len(pngData) == 0 || !bytes.HasPrefix(pngData, []byte("\x89PNG\r\n\x1a\n")) {
				errs <- errors.New("并发渲染结果不是 PNG")
			}
		}(index)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestTextAndSearchConcurrent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	const workers = 8
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			var operationErr error
			if index%2 == 0 {
				_, operationErr = reader.Text(0)
			} else {
				_, operationErr = reader.Search("你")
			}
			if operationErr != nil {
				errs <- operationErr
			}
		}(index)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestPageDocumentReusesBackgroundCache(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	first, err := reader.pageDocument(reader.pages[0], color.White)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reader.pageDocument(reader.pages[0], color.White)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("相同背景未复用页面渲染文档")
	}
}

func TestPageDocumentCacheHasBoundedCapacity(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	backgrounds := []color.Color{
		color.RGBA{R: 255, A: 255},
		color.RGBA{G: 255, A: 255},
		color.RGBA{B: 255, A: 255},
		color.RGBA{R: 255, G: 255, A: 255},
		color.RGBA{R: 128, A: 255},
	}
	for _, background := range backgrounds {
		if _, err := reader.pageDocument(reader.pages[0], background); err != nil {
			t.Fatal(err)
		}
	}
	if reader.renderDocs == nil {
		t.Fatal("临时渲染文档缓存未初始化")
	}
	if got := reader.renderDocs.Len(); got != maxRenderDocs {
		t.Fatalf("临时渲染文档缓存数量 = %d, want %d", got, maxRenderDocs)
	}
}

func TestAddFallbackFontInvalidatesBackgroundCache(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	first, err := reader.pageDocument(reader.pages[0], color.White)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.AddFallbackFont(FontSource{Family: "CacheInvalidationFallback", Data: []byte("invalid")}); err == nil {
		t.Fatal("无效回退字体应返回错误")
	}
	second, err := reader.pageDocument(reader.pages[0], color.White)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("添加失败的回退字体不应清空有效缓存")
	}
}

func TestRenderPages(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	pages, err := reader.RenderPages([]int{0, 0}, RenderOptions{DPI: 36})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("rendered pages = %d, want 2", len(pages))
	}
	for index, page := range pages {
		if len(page) == 0 || !bytes.HasPrefix(page, []byte("\x89PNG\r\n\x1a\n")) {
			t.Fatalf("rendered page %d is not PNG", index)
		}
	}
	if _, err := reader.RenderPages([]int{-1}, RenderOptions{DPI: 36}); err == nil {
		t.Fatal("negative page index was accepted")
	}
	indices := make([]int, 65)
	if _, err := reader.RenderPages(indices, RenderOptions{DPI: 36}); err == nil {
		t.Fatal("oversized page batch was accepted")
	}
}

func TestRenderPDF(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	pdfData, err := reader.RenderPDF([]int{0}, RenderOptions{Background: color.White})
	if err != nil {
		t.Fatal(err)
	}
	if len(pdfData) == 0 || !bytes.HasPrefix(pdfData, []byte("%PDF-")) {
		t.Fatal("rendered data is not PDF")
	}
	if !bytes.HasSuffix(bytes.TrimSpace(pdfData), []byte("%%EOF")) {
		t.Fatal("PDF has no EOF marker")
	}
	highDPI, err := reader.RenderPDF([]int{0}, RenderOptions{DPI: 150, Background: color.White})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(pdfData, highDPI) {
		t.Fatal("PDF DPI did not affect output")
	}
	if _, err := reader.RenderPDF(nil, RenderOptions{}); err == nil {
		t.Fatal("empty PDF page list was accepted")
	}
	if _, err := reader.RenderPDF([]int{0}, RenderOptions{DPI: 601}); err == nil {
		t.Fatal("excessive PDF DPI was accepted")
	}
}

func TestRenderPageValidation(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	if _, err := reader.RenderPage(3, RenderOptions{}); err == nil {
		t.Fatal("out-of-range page was accepted")
	}
	if _, err := reader.RenderPage(0, RenderOptions{DPI: 601}); err == nil {
		t.Fatal("excessive DPI was accepted")
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Pages(); err == nil {
		t.Fatal("closed reader returned pages")
	}
	if _, err := reader.Search(""); err == nil {
		t.Fatal("closed reader accepted an empty search")
	}
}

func TestFontsClosedReaderReturnsError(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Fonts(); err == nil {
		t.Fatal("closed reader returned fonts")
	}
}

func TestFontsExposeEmbeddedDataAndTextFamily(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	fonts, err := reader.Fonts()
	if err != nil {
		t.Fatal(err)
	}
	if len(fonts) == 0 {
		t.Fatal("expected at least one embedded font")
	}
	for _, font := range fonts {
		if font.Family == "" || len(font.Data) == 0 || font.Format == "" {
			t.Fatalf("invalid embedded font resource: %+v", font)
		}
	}
	fontFamilies := make(map[string]struct{}, len(fonts))
	for _, font := range fonts {
		fontFamilies[font.Family] = struct{}{}
	}
	foundRun := false
	for page := 0; page < reader.PageCount(); page++ {
		runs, textErr := reader.Text(page)
		if textErr != nil {
			t.Fatal(textErr)
		}
		if len(runs) > 0 {
			foundRun = true
			if runs[0].FontFamily == "" {
				t.Fatalf("text run has no browser font family: %+v", runs)
			}
			if _, ok := fontFamilies[runs[0].FontFamily]; !ok {
				t.Fatalf("text run references unknown browser font family %q", runs[0].FontFamily)
			}
			break
		}
	}
	if !foundRun {
		t.Log("embedded-font document has no text run on its parsed pages")
	}
}

func TestFallbackFontInvalidatesTextFamily(t *testing.T) {
	readData := func(name string) []byte {
		data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	embedded, err := Open(readData("ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer embedded.Close()
	fonts, err := embedded.Fonts()
	if err != nil || len(fonts) == 0 {
		t.Fatalf("embedded fonts = %d, err = %v", len(fonts), err)
	}

	reader, err := Open(readData("helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err := reader.Text(0); err != nil {
		t.Fatal(err)
	}
	const family = "TestFallback"
	if err := reader.AddFallbackFont(FontSource{Family: family, Data: fonts[0].Data}); err != nil {
		t.Fatal(err)
	}
	runs, err := reader.Text(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) == 0 {
		t.Fatal("fallback document has no text runs")
	}
	if runs[0].FontFamily != family {
		t.Fatalf("fallback family = %q, want %q", runs[0].FontFamily, family)
	}
}

func TestTextAndSearch(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	text, err := reader.Text(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(text) == 0 {
		t.Fatal("page text is empty")
	}
	if text[0].X != 31.7 || text[0].Y != 25.4 || text[0].Height != 3 {
		t.Fatalf("invalid text geometry: %+v", text[0])
	}
	if len(text[0].Glyphs) != len([]rune(text[0].Text)) || text[0].Glyphs[0].X != text[0].X || text[0].Glyphs[0].Y != text[0].Y {
		t.Fatalf("invalid glyph geometry: %+v", text[0].Glyphs)
	}
	if text[0].Glyphs[1].X != 34.7 || text[0].Glyphs[1].Y != 25.4 {
		t.Fatalf("second glyph geometry = %+v, want x=34.7 y=25.4", text[0].Glyphs[1])
	}
	query := []rune(text[0].Text)
	if len(query) == 0 {
		t.Fatal("first text run is empty")
	}
	results, err := reader.Search(string(query[0]))
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].Page != 0 {
		t.Fatalf("search results = %+v, want a hit on page 0", results)
	}
	if results[0].End <= results[0].Start {
		t.Fatalf("search range = %d:%d, want a non-empty range", results[0].Start, results[0].End)
	}
	if len(results[0].Rects) != results[0].End-results[0].Start {
		t.Fatalf("search rects = %+v, want one rect per matched glyph", results[0].Rects)
	}
	wholeRun, err := reader.Search(text[0].Text)
	if err != nil {
		t.Fatal(err)
	}
	if len(wholeRun) == 0 || wholeRun[0].Start != 0 || wholeRun[0].End != len([]rune(text[0].Text)) {
		t.Fatalf("whole-run search = %+v, want a match spanning the run", wholeRun)
	}
	if len(wholeRun[0].Rects) != len([]rune(text[0].Text)) {
		t.Fatalf("whole-run rect count = %d, want %d", len(wholeRun[0].Rects), len([]rune(text[0].Text)))
	}
}

func TestTextReturnsIndependentSnapshots(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	first, err := reader.Text(0)
	if err != nil {
		t.Fatal(err)
	}
	originalText, originalX := first[0].Text, first[0].Glyphs[0].X
	first[0].Text = "changed"
	first[0].Glyphs[0].X = -1

	second, err := reader.Text(0)
	if err != nil {
		t.Fatal(err)
	}
	if second[0].Text != originalText || second[0].Glyphs[0].X != originalX {
		t.Fatalf("text cache leaked mutations: got %+v", second[0])
	}
}

func TestSearchCachesNormalizedPageText(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	if _, err := reader.Search("你"); err != nil {
		t.Fatal(err)
	}
	if !reader.searchSet[0] || len(reader.search[0].runs) == 0 {
		t.Fatalf("search index was not populated: %+v", reader.search)
	}
	if len(reader.search[0].byRune['你']) == 0 {
		t.Fatalf("rune index was not populated: %+v", reader.search[0].byRune)
	}
	indexed := reader.search[0].runs[0]
	if len(indexed) == 0 {
		t.Fatal("first indexed run is empty")
	}
	if _, err := reader.Search("好"); err != nil {
		t.Fatal(err)
	}
	if &reader.search[0].runs[0][0] != &indexed[0] {
		t.Fatal("search index was rebuilt instead of reused")
	}
}
