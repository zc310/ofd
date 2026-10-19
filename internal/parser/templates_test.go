package parser

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/models"
)

func parserTestArchive(tb testing.TB, files map[string]string) []byte {
	tb.Helper()
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	for name, content := range files {
		entry, err := archive.Create(name)
		if err != nil {
			tb.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			tb.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		tb.Fatal(err)
	}
	return data.Bytes()
}

func templateTestDocument(tb testing.TB, count int, content string) *Document {
	tb.Helper()
	files := map[string]string{
		"OFD.xml": `<OFD><DocBody><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`,
	}
	var document strings.Builder
	document.WriteString(`<Document><CommonData>`)
	for id := 1; id <= count; id++ {
		fmt.Fprintf(&document, `<TemplatePage ID="%d" BaseLoc="Templates/%d.xml"/>`, id, id)
		files[fmt.Sprintf("Doc_0/Templates/%d.xml", id)] = content
	}
	document.WriteString(`</CommonData><Pages/></Document>`)
	files["Doc_0/Document.xml"] = document.String()
	ofd, err := NewOFD(parserTestArchive(tb, files))
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = ofd.Close() })
	return ofd.Documents[0]
}

func TestTemplateCacheKeepsRecentlyUsedTemplate(t *testing.T) {
	doc := templateTestDocument(t, 33, `<Page><Content><Layer ID="1"/></Content></Page>`)
	var first *models.PageContent
	for id := models.StID(1); id <= 32; id++ {
		content, err := doc.LoadTemplate(id)
		if err != nil {
			t.Fatal(err)
		}
		if id == 1 {
			first = content
		}
	}
	if got, err := doc.LoadTemplate(1); err != nil || got != first {
		t.Fatalf("缓存命中 = %p, %v，期望 %p", got, err, first)
	}
	if _, err := doc.LoadTemplate(33); err != nil {
		t.Fatal(err)
	}
	if got, err := doc.LoadTemplate(1); err != nil || got != first {
		t.Fatalf("最近使用的模板被重新解析：%p, %v，期望 %p", got, err, first)
	}
	if _, ok := doc.templates[2]; ok {
		t.Fatal("最近未使用的模板 2 未被淘汰")
	}
	if got := doc.templateCache.Len(); got != 32 {
		t.Fatalf("模板缓存数量 = %d，期望 32", got)
	}
}

func TestTemplateCacheUsesTemplateZipEntrySize(t *testing.T) {
	doc := templateTestDocument(t, 1, `<Page><Content><Layer ID="1"/></Content></Page>`)
	if _, err := doc.LoadTemplate(1); err != nil {
		t.Fatal(err)
	}
	entry, ok := doc.FileCache.Lookup("Doc_0/Templates/1.xml")
	if !ok {
		t.Fatal("模板条目不存在")
	}
	if got := doc.templateCache.Weight(); got != int64(entry.UncompressedSize) {
		t.Fatalf("模板缓存权重 = %d，期望 ZIP 解压大小 %d", got, entry.UncompressedSize)
	}
}

func TestLoadTemplateCachesParseError(t *testing.T) {
	doc := templateTestDocument(t, 1, `<Page><Area><PhysicalBox>invalid</PhysicalBox></Area></Page>`)
	content, firstErr := doc.LoadTemplate(1)
	if content != nil || firstErr == nil {
		t.Fatalf("损坏模板应返回解析错误，实际为 %p, %v", content, firstErr)
	}
	if content, err := doc.LoadTemplate(1); content != nil || err != firstErr {
		t.Fatalf("重复加载应保留原解析错误，实际为 %p, %v", content, err)
	}
	if content, err := doc.LoadTemplate(2); content != nil || err != nil {
		t.Fatalf("未声明模板应返回 nil, nil，实际为 %p, %v", content, err)
	}
}

func TestLoadTemplateReportsMissingFile(t *testing.T) {
	doc := templateTestDocument(t, 1, `<Page/>`)
	doc.templateLocations[1] = "Doc_0/Templates/missing.xml"
	if content, err := doc.LoadTemplate(1); content != nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("缺失模板应保留文件不存在错误，实际为 %p, %v", content, err)
	}
}

func TestConcurrentLoadTemplateReturnsSharedContent(t *testing.T) {
	doc := templateTestDocument(t, 1, `<Page><Content><Layer ID="1"/></Content></Page>`)
	contents := make([]*models.PageContent, 16)
	errors := make([]error, len(contents))
	var workers sync.WaitGroup
	for i := range contents {
		workers.Go(func() { contents[i], errors[i] = doc.LoadTemplate(1) })
	}
	workers.Wait()
	for i, content := range contents {
		if errors[i] != nil || content == nil || content != contents[0] {
			t.Errorf("并发加载 %d = %p, %v，期望共享 %p", i, content, errors[i], contents[0])
		}
	}
}
