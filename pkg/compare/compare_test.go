package compare

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/gofont/goregular"
)

func testOFD(t *testing.T, texts ...string) string {
	t.Helper()
	pages := make([]creator.Page, len(texts))
	for i, text := range texts {
		pages[i] = creator.Page{Items: []creator.Item{creator.Text{X: 2, Y: 2, Width: 25, Height: 8, Font: "Go", Size: 3, Value: text}}}
	}
	data, err := creator.Marshal(creator.Document{ID: "compare-fixture", PageSize: creator.PageSize{Width: 30, Height: 20}, Fonts: []creator.Font{{Name: "Go", Data: goregular.TTF}}, Pages: pages})
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(t.TempDir(), "input.ofd")
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	return name
}

func rewrite(t *testing.T, name string, transform func(string, []byte) (string, []byte)) string {
	t.Helper()
	r, err := zip.OpenReader(name)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for i := len(r.File) - 1; i >= 0; i-- {
		f := r.File[i]
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		n, data := transform(f.Name, data)
		if n == "" {
			continue
		}
		entry, err := w.CreateHeader(&zip.FileHeader{Name: n, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "repacked.ofd")
	if err := os.WriteFile(out, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRepackingDoesNotProduceContentDifferences(t *testing.T) {
	a := testOFD(t, "same")
	b := rewrite(t, a, func(n string, b []byte) (string, []byte) { return n, b })
	r, err := CompareFiles(t.Context(), []string{a, b}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Comparisons[0].Equal || len(r.Comparisons[0].Files) != 0 {
		t.Fatalf("重打包应保持内容一致: %+v", r.Comparisons[0])
	}
}

func TestMultipleInputsRespectSelectedBaselineAndInsertedPages(t *testing.T) {
	a := testOFD(t, "alpha", "beta")
	b := testOFD(t, "alpha", "inserted", "beta")
	c := testOFD(t, "alpha", "changed beta")
	r, err := CompareFiles(t.Context(), []string{b, a, c}, Options{Baseline: 1})
	if err != nil {
		t.Fatal(err)
	}
	if r.Baseline != 2 || len(r.Comparisons) != 2 || r.Comparisons[0].Target != 1 {
		t.Fatalf("错误的多文件基准: %+v", r)
	}
	first := r.Comparisons[0]
	if first.Summary.PagesAdded != 1 || first.Pages[1].Before != 2 || first.Pages[1].After != 3 {
		t.Fatalf("插页后未匹配 beta: %+v", first.Pages)
	}
	if !r.Comparisons[1].Pages[1].TextChanged {
		t.Fatal("未发现尾页文字修改")
	}
}

func TestPageMatchingSupportsMovesAndNumberMode(t *testing.T) {
	a := []Page{{Document: 1, ContentSHA256: "a", Text: "one"}, {Document: 1, ContentSHA256: "b", Text: "two"}}
	b := []Page{a[1], a[0]}
	if got, err := matchPages(t.Context(), a, b, Options{PageMatch: "auto"}); err != nil || !reflect.DeepEqual(pagePairsForTest(got), [][2]int{{1, 2}, {2, 1}}) {
		t.Fatal(got, err)
	}
	if got, err := matchPages(t.Context(), a, b, Options{PageMatch: "number"}); err != nil || !reflect.DeepEqual(pagePairsForTest(got), [][2]int{{1, 1}, {2, 2}}) {
		t.Fatal(got, err)
	}
}

func TestXMLComparisonPreservesMeaningfulWhitespaceAndOrder(t *testing.T) {
	ctx := t.Context()
	a := []byte(`<a:Root xmlns:a="urn:test" x="1" y="2"><a:TextCode> a </a:TextCode><a:Other/></a:Root>`)
	b := []byte("<b:Root y=\"2\" xmlns:b=\"urn:test\" x=\"1\">\n  <b:TextCode> a </b:TextCode>\n<!--ignore--><b:Other/>\n</b:Root>")
	af, err := xmlFields(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	bf, err := xmlFields(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(af, bf) {
		t.Fatalf("前缀和缩进产生差异: %v / %v", af, bf)
	}
	for _, changed := range []string{strings.Replace(string(b), "> a <", ">a<", 1), strings.Replace(string(b), "urn:test", "urn:other", 1), `<Root><A/><B/></Root>`} {
		f, err := xmlFields(ctx, []byte(changed))
		if err != nil {
			t.Fatal(err)
		}
		changes, _ := fieldChanges(af, f, 200)
		if len(changes) == 0 {
			t.Fatal("有意义的 XML 变化被忽略")
		}
	}
	x, _ := xmlFields(ctx, []byte(`<Root><A/><B/></Root>`))
	y, _ := xmlFields(ctx, []byte(`<Root><B/><A/></Root>`))
	changes, truncated := fieldChanges(x, y, 1)
	if len(changes) != 1 || changes[0].Path != "/Root[1]/#children" || truncated {
		t.Fatalf("未保留元素顺序: %+v", changes)
	}
	_, truncated = fieldChanges(af, x, 1)
	if !truncated {
		t.Fatal("未报告差异明细截断")
	}
}

func TestFileMovesAndXMLFormattingDifferences(t *testing.T) {
	a := testOFD(t, "same")
	b := rewrite(t, a, func(n string, data []byte) (string, []byte) {
		if strings.HasSuffix(n, ".xml") {
			return n, bytes.ReplaceAll(data, []byte("><"), []byte(">\n<"))
		}
		return n, data
	})
	r, err := CompareFiles(t.Context(), []string{a, b}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Comparisons[0].Summary.FilesModified == 0 {
		t.Fatal("缺少原始 XML 字节变化")
	}
	for _, f := range r.Comparisons[0].Files {
		if f.Before.XMLSHA256 != f.After.XMLSHA256 || len(f.XMLChanges) > 0 {
			t.Fatalf("缩进不应修改 XML 结构: %+v", f)
		}
	}
	base := &snapshot{input: Input{Files: []File{{Path: "old/image.png", SHA256: "hash"}}}, files: map[string]*File{}}
	target := &snapshot{input: Input{Files: []File{{Path: "new/image.png", SHA256: "hash"}}}, files: map[string]*File{}}
	base.files[base.input.Files[0].Path] = &base.input.Files[0]
	target.files[target.input.Files[0].Path] = &target.input.Files[0]
	remaining := int64(1)
	c, err := compareSnapshots(t.Context(), base, target, Options{}, &remaining)
	if err != nil {
		t.Fatal(err)
	}
	if c.Summary.FilesMoved != 1 || c.Summary.FilesAdded != 0 || c.Summary.FilesRemoved != 0 {
		t.Fatalf("同内容移动: %+v", c)
	}
}

func TestVisualComparisonAndStandaloneReports(t *testing.T) {
	a, b := testOFD(t, "Amount 100"), testOFD(t, "Amount 200")
	r, err := CompareFiles(t.Context(), []string{a, b}, Options{Visual: true, DPI: 72})
	if err != nil {
		t.Fatal(err)
	}
	v := r.Comparisons[0].Pages[0].Visual
	if v == nil || v.ChangedPixels == 0 || v.DiffPNG == "" || v.BeforePNG == "" || v.AfterPNG == "" {
		t.Fatalf("没有产生画面差异: %+v", v)
	}
	r.Inputs[0].Name = `<script>alert("x")</script>`
	for _, format := range []string{"text", "markdown", "json", "html"} {
		var out bytes.Buffer
		if err := WriteReport(&out, r, format); err != nil {
			t.Fatal(err)
		}
		if out.Len() == 0 {
			t.Fatal("报告为空")
		}
		if format == "html" {
			if strings.Contains(out.String(), r.Inputs[0].Name) || !strings.Contains(out.String(), "data:image/png;base64,") {
				t.Fatal("HTML 未正确转义或未内嵌图片")
			}
		}
	}
	if err := WriteReport(failingWriter{}, r, "json"); err == nil {
		t.Fatal("输出错误被吞掉")
	}
}

func TestSharedImageChangeIsVisibleWithUnchangedPageXML(t *testing.T) {
	encode := func(c color.RGBA) []byte {
		img := image.NewRGBA(image.Rect(0, 0, 12, 12))
		for y := 0; y < 12; y++ {
			for x := 0; x < 12; x++ {
				img.SetRGBA(x, y, c)
			}
		}
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	data, err := creator.Marshal(creator.Document{ID: "image-change", PageSize: creator.PageSize{Width: 30, Height: 20}, Pages: []creator.Page{{Items: []creator.Item{creator.Image{X: 2, Y: 2, Width: 12, Height: 12, Data: encode(color.RGBA{R: 255, A: 255}), Format: "PNG"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(t.TempDir(), "before.ofd")
	if err := os.WriteFile(a, data, 0600); err != nil {
		t.Fatal(err)
	}
	b := rewrite(t, a, func(n string, data []byte) (string, []byte) {
		if strings.HasSuffix(strings.ToLower(n), ".png") {
			return n, encode(color.RGBA{B: 255, A: 255})
		}
		return n, data
	})
	r, err := CompareFiles(t.Context(), []string{a, b}, Options{Visual: true})
	if err != nil {
		t.Fatal(err)
	}
	c := r.Comparisons[0]
	if r.Inputs[0].Pages[0].ContentSHA256 != r.Inputs[1].Pages[0].ContentSHA256 {
		t.Fatal("测试改变了页面模型")
	}
	if c.Summary.VisualPagesChanged != 1 || c.Pages[0].Status != "modified" || c.Pages[0].TextChanged {
		t.Fatalf("未检测到共享图片变化: %+v", c.Pages)
	}
	if len(c.Files) != 1 || c.Files[0].BeforePreview == "" || c.Files[0].AfterPreview == "" {
		t.Fatal("图片资源差异未生成预览")
	}
	r, err = CompareFiles(t.Context(), []string{a, b}, Options{Visual: true, PixelTolerance: 255})
	if err != nil {
		t.Fatal(err)
	}
	if r.Comparisons[0].Pages[0].Visual.ChangedPixels != 0 || r.Comparisons[0].Equal {
		t.Fatal("像素容差不应隐藏文件差异")
	}
}

func TestInvalidAndTruncatedXMLIsMarked(t *testing.T) {
	a := testOFD(t, "same")
	b := rewrite(t, a, func(n string, data []byte) (string, []byte) {
		if strings.HasSuffix(n, ".ttf") {
			return "unused.xml", []byte(`<broken>`)
		}
		return n, data
	})
	// 页面文字快照不加载字体；无关的畸形 XML 仍应作为文件差异展示，
	// 并明确说明结构分析失败。
	r, err := CompareFiles(t.Context(), []string{a, b}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	note := ""
	for _, f := range r.Inputs[1].Files {
		if f.Path == "unused.xml" {
			note = f.XMLNote
		}
	}
	if note == "" {
		t.Fatal("无效 XML 没有诊断")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestLimitsAndCancellationAreExplicit(t *testing.T) {
	a, b := testOFD(t, "same"), testOFD(t, "changed")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := CompareFiles(ctx, []string{a, b}, Options{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := CompareFiles(t.Context(), []string{a, b}, Options{Visual: true, MaxPagePixels: 1}); err == nil {
		t.Fatal("超出像素预算未返回错误")
	}
	r, err := CompareFiles(t.Context(), []string{a, b}, Options{MaxXMLBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	notes := 0
	for _, f := range r.Inputs[0].Files {
		if f.XMLNote != "" {
			notes++
		}
	}
	if notes == 0 {
		t.Fatal("跳过 XML 结构分析没有说明")
	}
}
