package webreader

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/klauspost/compress/zip"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/pkg/creator"
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
	jpgData, err := reader.RenderPage(0, RenderOptions{Format: RenderJPG, DPI: 36})
	if err != nil {
		t.Fatal(err)
	}
	if len(jpgData) == 0 || !bytes.HasPrefix(jpgData, []byte{0xff, 0xd8, 0xff}) {
		t.Fatal("rendered data is not JPG")
	}

	svgData, err := reader.RenderPage(0, RenderOptions{Format: RenderSVG})
	if err != nil {
		t.Fatal(err)
	}
	if len(svgData) == 0 || !bytes.Contains(svgData, []byte("<svg")) {
		t.Fatal("rendered data is not SVG")
	}

	if _, err := reader.RenderPage(0, RenderOptions{Format: RenderFormat("gif")}); err == nil {
		t.Fatal("unsupported render format was accepted")
	}

	svgPages, err := reader.RenderPages([]int{0, 0}, RenderOptions{Format: RenderFormat("SVG")})
	if err != nil {
		t.Fatal(err)
	}
	for index, page := range svgPages {
		if len(page) == 0 || !bytes.Contains(page, []byte("<svg")) {
			t.Fatalf("rendered SVG page %d is empty or invalid", index)
		}
	}
}

func TestPagesWithIntroDocument(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "intro.ofd"))
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
	if len(pages) != 42 {
		t.Fatalf("page count = %d, want 42", len(pages))
	}
	for index, page := range pages {
		if page.Index != index {
			t.Errorf("Pages()[%d].Index = %d, want %d", index, page.Index, index)
		}
		if math.Abs(page.Width-320.0001) > 0.0001 || math.Abs(page.Height-240) > 0.0001 {
			t.Errorf("Pages()[%d] = %+v, want 320.0001 x 240 mm", index, page)
		}
	}
}

func TestPagesUsesPageMetadataAndPageLoadsRealSize(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "GBT_33190-2016.ofd"))
	if os.IsNotExist(err) {
		t.Skip("GBT_33190-2016.ofd  is unavailable")
	}
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
	if pages[0].Width != 209.9733 || pages[0].Height != 296.9260 {
		t.Fatalf("Pages()[0] = %+v, want document page size 209.9733 x 296.9260", pages[0])
	}
	for index, page := range pages {
		if page.Width != pages[0].Width || page.Height != pages[0].Height {
			t.Fatalf("Pages()[%d] = %+v, want document page size", index, page)
		}
	}
	actual, err := reader.Page(0)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Width != 209.9733 || actual.Height != 296.9260 {
		t.Fatalf("Page(0) = %+v, want physical size 209.9733 x 296.9260", actual)
	}
}

func TestPagesUsesDocumentSizeWhenPageMetadataIsMissing(t *testing.T) {
	data := makeMinimalOFDBundle(t)
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	pages, err := reader.Pages()
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 {
		t.Fatalf("page count = %d, want 2", len(pages))
	}
	if pages[0].Width != 100 || pages[0].Height != 200 {
		t.Fatalf("Pages()[0] = %+v, want page size 100 x 200", pages[0])
	}
	if pages[1].Width != 210 || pages[1].Height != 297 {
		t.Fatalf("Pages()[1] = %+v, want document size 210 x 297", pages[1])
	}

	if _, err := reader.Page(1); err == nil {
		t.Fatal("Page(1) should parse the intentionally invalid full page content")
	}
}

func makeMinimalOFDBundle(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	entries := map[string]string{
		"OFD.xml":                `<OFD Version="1.1"><DocBody><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc_0/Document.xml":     `<Document><CommonData><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Pages/Page_0.xml"/><Page ID="2" BaseLoc="Pages/Page_1.xml"/></Pages></Document>`,
		"Doc_0/Pages/Page_0.xml": `<Page><Area><PhysicalBox>0 0 100 200</PhysicalBox></Area><Content><Layer/></Content></Page>`,
		"Doc_0/Pages/Page_1.xml": `<Page><Content>` + strings.Repeat("<Layer>", 2),
	}
	for name, content := range entries {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
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

	first, err := reader.pageDocument(reader.pages[0], color.White, canvas.DPI(96))
	if err != nil {
		t.Fatal(err)
	}
	second, err := reader.pageDocument(reader.pages[0], color.White, canvas.DPI(96))
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
		if _, err := reader.pageDocument(reader.pages[0], background, canvas.DPI(96)); err != nil {
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

func TestUseFallbackFontInvalidatesBackgroundCache(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	first, err := reader.pageDocument(reader.pages[0], color.White, canvas.DPI(96))
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.UseFallbackFont("CacheInvalidationFallback"); err == nil {
		t.Fatal("未注册回退字体应返回错误")
	}
	second, err := reader.pageDocument(reader.pages[0], color.White, canvas.DPI(96))
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
	if !bytes.Contains(pdfData, []byte("/ToUnicode")) {
		t.Fatal("PDF has no selectable text mapping")
	}
	highDPI, err := reader.RenderPDF([]int{0}, RenderOptions{DPI: 150, Background: color.White})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(highDPI, []byte("%PDF-")) || !bytes.HasSuffix(bytes.TrimSpace(highDPI), []byte("%%EOF")) {
		t.Fatal("high-DPI PDF is invalid")
	}
	if _, err := reader.RenderPDF(nil, RenderOptions{}); err == nil {
		t.Fatal("empty PDF page list was accepted")
	}
	if _, err := reader.RenderPDF([]int{0}, RenderOptions{DPI: 601}); err == nil {
		t.Fatal("excessive PDF DPI was accepted")
	}
}

func TestRenderPDFToPreservesSelectableText(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	var output bytes.Buffer
	if err := reader.RenderPDFTo(&output, []int{0}, RenderOptions{Background: color.White}); err != nil {
		t.Fatal(err)
	}
	outputData := output.Bytes()
	if !bytes.HasPrefix(outputData, []byte("%PDF-")) || !bytes.HasSuffix(bytes.TrimSpace(outputData), []byte("%%EOF")) {
		t.Fatal("streamed PDF is invalid")
	}
	if !bytes.Contains(outputData, []byte("/ToUnicode")) {
		t.Fatal("streamed PDF has no selectable text mapping")
	}
}

func TestRenderPDFToStreamsMoreThanBatchLimit(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	indices := make([]int, 65)
	var output bytes.Buffer
	if err := reader.RenderPDFTo(&output, indices, RenderOptions{DPI: 12, Background: color.White}); err != nil {
		t.Fatalf("streamed PDF rejected oversized page batch: %v", err)
	}
	if !bytes.HasPrefix(output.Bytes(), []byte("%PDF-")) || !bytes.HasSuffix(bytes.TrimSpace(output.Bytes()), []byte("%%EOF")) {
		t.Fatal("streamed PDF is invalid")
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
	if err := RegisterFallbackFont(FontSource{Family: family, Data: fonts[0].Data}); err != nil {
		t.Fatal(err)
	}
	if err := reader.UseFallbackFont(family); err != nil {
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
	indexed, ok := reader.search.Get(0)
	if !ok || len(indexed.runs) == 0 {
		t.Fatalf("search index was not populated: %+v", indexed)
	}
	if len(indexed.byRune['你']) == 0 {
		t.Fatalf("rune index was not populated: %+v", indexed.byRune)
	}
	firstRun := indexed.runs[0]
	if len(firstRun) == 0 {
		t.Fatal("first indexed run is empty")
	}
	if _, err := reader.Search("好"); err != nil {
		t.Fatal(err)
	}
	reused, ok := reader.search.Get(0)
	if !ok || len(reused.runs) == 0 || len(reused.runs[0]) == 0 {
		t.Fatal("search index was not reused")
	}
	if &reused.runs[0][0] != &firstRun[0] {
		t.Fatal("search index was rebuilt instead of reused")
	}
}

func TestTextAndSearchCachesStayBounded(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "1000-pages.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	pageCount := reader.PageCount()
	if pageCount <= textCacheCapacity {
		t.Fatalf("测试文档页数 %d 未超过缓存上限 %d", pageCount, textCacheCapacity)
	}
	for index := 0; index < pageCount; index++ {
		if _, err := reader.Text(index); err != nil {
			t.Fatalf("读取第 %d 页文字失败: %v", index, err)
		}
	}
	if length := reader.text.Len(); length > textCacheCapacity {
		t.Fatalf("文字缓存条目 = %d, 期望 <= %d", length, textCacheCapacity)
	}
	if _, err := reader.Search("the"); err != nil {
		t.Fatal(err)
	}
	if length := reader.search.Len(); length > searchCacheCapacity {
		t.Fatalf("搜索缓存条目 = %d, 期望 <= %d", length, searchCacheCapacity)
	}
}

func TestOutlineResolvesPageAndBookmark(t *testing.T) {
	top := 20.0
	zoom := 1.5
	document := creator.Document{
		ID:          "outline-test",
		Title:       "大纲测试",
		PageSize:    creator.A4,
		Pages:       []creator.Page{{}, {}},
		Preferences: &creator.ViewPreferences{PageMode: creator.PageModeUseOutlines},
		Bookmarks:   []creator.Bookmark{{Name: "第三章", Goto: creator.GotoAction{Page: 1}}},
		Outlines: []creator.Outline{
			{
				Title:   "第一章",
				Actions: []creator.Action{{Event: creator.ActionEventClick, Goto: &creator.GotoAction{Page: 0}}},
				Children: []creator.Outline{
					{Title: "1.1 节", Actions: []creator.Action{{Event: creator.ActionEventClick, Goto: &creator.GotoAction{Page: 1, Type: "XYZ", Top: &top, Zoom: &zoom}}}},
				},
			},
			{Title: "书签跳转", Actions: []creator.Action{{Event: creator.ActionEventClick, Goto: &creator.GotoAction{Bookmark: "第三章"}}}},
			{Title: "外部链接", Actions: []creator.Action{{Event: creator.ActionEventClick, URI: &creator.URIAction{URI: "https://example.com"}}}},
		},
	}
	var buffer bytes.Buffer
	if err := creator.Create(document, &buffer); err != nil {
		t.Fatalf("创建测试 OFD 失败: %v", err)
	}
	reader, err := Open(buffer.Bytes())
	if err != nil {
		t.Fatalf("打开测试 OFD 失败: %v", err)
	}
	defer reader.Close()

	tree, err := reader.Outline()
	if err != nil {
		t.Fatalf("读取大纲失败: %v", err)
	}
	if tree.PageMode != string(creator.PageModeUseOutlines) {
		t.Fatalf("PageMode = %q, 期望 UseOutlines", tree.PageMode)
	}
	if len(tree.Nodes) != 3 {
		t.Fatalf("顶层大纲项 = %d, 期望 3", len(tree.Nodes))
	}
	if tree.Nodes[0].Title != "第一章" || tree.Nodes[0].Page != 0 {
		t.Fatalf("节点 0 = %+v", tree.Nodes[0])
	}
	child := tree.Nodes[0].Children
	if len(child) != 1 || child[0].Page != 1 {
		t.Fatalf("节点 0 子项 = %+v", child)
	}
	if child[0].Dest == nil || child[0].Dest.Top == nil || *child[0].Dest.Top != top || child[0].Dest.Zoom == nil || *child[0].Dest.Zoom != zoom {
		t.Fatalf("子项目标位置/缩放未解析: %+v", child[0].Dest)
	}
	if tree.Nodes[1].Page != 1 || tree.Nodes[1].Dest == nil {
		t.Fatalf("书签跳转节点 = %+v", tree.Nodes[1])
	}
	if tree.Nodes[2].URI != "https://example.com" || tree.Nodes[2].Page != -1 {
		t.Fatalf("链接节点 = %+v", tree.Nodes[2])
	}
	if len(tree.Bookmarks) != 1 || tree.Bookmarks[0].Name != "第三章" || tree.Bookmarks[0].Page != 1 {
		t.Fatalf("书签列表 = %+v", tree.Bookmarks)
	}
}

func TestPreferencesExposeDocumentZoom(t *testing.T) {
	openWithPreferences := func(t *testing.T, preferences *creator.ViewPreferences) *Reader {
		t.Helper()
		document := creator.Document{
			ID:          "preferences-test",
			Title:       "偏好测试",
			PageSize:    creator.A4,
			Pages:       []creator.Page{{}},
			Preferences: preferences,
		}
		var buffer bytes.Buffer
		if err := creator.Create(document, &buffer); err != nil {
			t.Fatalf("创建测试 OFD 失败: %v", err)
		}
		reader, err := Open(buffer.Bytes())
		if err != nil {
			t.Fatalf("打开测试 OFD 失败: %v", err)
		}
		t.Cleanup(func() { _ = reader.Close() })
		return reader
	}

	modeReader := openWithPreferences(t, &creator.ViewPreferences{ZoomMode: creator.ZoomModeFitWidth})
	modePreferences, err := modeReader.Preferences()
	if err != nil {
		t.Fatalf("读取显示偏好失败: %v", err)
	}
	if modePreferences.ZoomMode != creator.ZoomModeFitWidth {
		t.Fatalf("ZoomMode = %q, 期望 %q", modePreferences.ZoomMode, creator.ZoomModeFitWidth)
	}

	zoomValue := 1.75
	zoomReader := openWithPreferences(t, &creator.ViewPreferences{Zoom: &zoomValue})
	zoomPreferences, err := zoomReader.Preferences()
	if err != nil {
		t.Fatalf("读取显示偏好失败: %v", err)
	}
	if zoomPreferences.Zoom == nil || *zoomPreferences.Zoom != zoomValue {
		t.Fatalf("Zoom = %v, 期望 %v", zoomPreferences.Zoom, zoomValue)
	}
}

func TestFontListIncludesDeclaredFonts(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	fonts, err := reader.FontList()
	if err != nil {
		t.Fatalf("读取字体列表失败: %v", err)
	}
	if len(fonts) == 0 {
		t.Fatal("字体列表为空")
	}
	embedded := 0
	for _, font := range fonts {
		if font.Name == "" && font.Family == "" {
			t.Fatalf("字体缺少名称: %+v", font)
		}
		if font.Embedded {
			embedded++
			if font.Format == "" {
				t.Fatalf("嵌入字体缺少格式: %+v", font)
			}
		}
	}
	if embedded == 0 {
		t.Fatal("没有识别到嵌入字体")
	}
}
