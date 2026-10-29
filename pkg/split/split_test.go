package split

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/merge"
	"github.com/zc310/ofd/pkg/spec"
	"github.com/zc310/ofd/pkg/validator"
)

func splitFixture(t *testing.T) []byte {
	t.Helper()
	var pngData bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&pngData, img); err != nil {
		t.Fatal(err)
	}
	doc := creator.Document{
		ID: "split-source", Title: "拆分测试", PageSize: creator.PageSize{Width: 40, Height: 50},
		CoverName: "cover.png", CoverData: pngData.Bytes(),
		Media: []creator.Media{
			{ID: 100, Type: "Image", Format: "PNG", Name: "first.png", Data: pngData.Bytes()},
			{ID: 101, Type: "Image", Format: "PNG", Name: "second.png", Data: pngData.Bytes()},
		},
		DrawParams: []creator.DrawParam{{Name: "parent", LineWidth: 0.2}, {Name: "child", Relative: "parent", StrokeColor: &creator.Color{R: 255}}},
		Composites: []creator.CompositeGraphicUnit{{ID: 300, Width: 10, Height: 10, Items: []creator.Item{creator.Path{Width: 10, Height: 10, Data: "M 0 0 L 10 10", Stroke: true, DrawParam: "child"}}}},
		Templates:  []creator.TemplatePage{{ID: 200, Items: []creator.Item{creator.Image{Width: 5, Height: 5, ResourceID: 100}}}, {ID: 201}},
		Pages: []creator.Page{
			{Templates: []creator.TemplateRef{{ID: 200}}, Items: []creator.Item{creator.Composite{Width: 10, Height: 10, ResourceID: 300}}, Actions: []creator.Action{{Goto: &creator.GotoAction{Bookmark: "second"}}, {GotoA: &creator.GotoAAction{AttachID: "attachment"}}}},
			{Items: []creator.Item{creator.Image{Width: 10, Height: 10, ResourceID: 101}}, Actions: []creator.Action{{Goto: &creator.GotoAction{Page: 0}}}},
			{},
		},
		Bookmarks:   []creator.Bookmark{{Name: "second", Goto: creator.GotoAction{Page: 1}}},
		Outlines:    []creator.Outline{{Title: "parent", Children: []creator.Outline{{Title: "second", Actions: []creator.Action{{Goto: &creator.GotoAction{Page: 1}}}}}}},
		Attachments: []creator.Attachment{{ID: "attachment", Name: "whole.txt", FileName: "whole.txt", Data: []byte("whole document")}},
		Annotations: []creator.AnnotationPage{{Page: 0, Items: []creator.Annotation{{ID: 500, Type: spec.AnnotationTypeHighlight, Creator: "test", LastModDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Items: []creator.Item{creator.Image{Width: 2, Height: 2, ResourceID: 100}}}}}},
	}
	data, err := creator.MarshalWithOptions(doc, creator.CreateOptions{Deterministic: true})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func openTest(t *testing.T, data []byte, options Options) *Session {
	t.Helper()
	s, err := Open(t.Context(), data, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func writeTest(t *testing.T, s *Session, part Part) ([]byte, Report) {
	t.Helper()
	var out bytes.Buffer
	report, err := s.Write(t.Context(), part, &out)
	if err != nil {
		t.Fatal(err)
	}
	v, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	result := v.ValidateReader(t.Context(), bytes.NewReader(out.Bytes()), "split.ofd")
	if result.HasErrors() || result.HasWarnings() {
		var description bytes.Buffer
		_ = validator.RenderText(&description, result)
		t.Fatal(description.String())
	}
	return out.Bytes(), report
}

func readFiles(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	pkg, err := container.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer pkg.Close()
	files := make(map[string][]byte)
	for _, entry := range pkg.Entries() {
		if !entry.IsDir {
			value, err := pkg.Read(entry.Path)
			if err != nil {
				t.Fatal(err)
			}
			files[entry.Path] = value
		}
	}
	return files
}

func TestSplitPrunesResourcesNavigationAndAnnotations(t *testing.T) {
	source := splitFixture(t)
	inputFiles := readFiles(t, source)
	inputFiles["OFD.xml"] = bytes.ReplaceAll(inputFiles["OFD.xml"], []byte(`DocType="OFD"`), []byte(`DocType="OFD-A"`))
	source = repack(t, inputFiles)
	s := openTest(t, source, Options{Deterministic: true})
	data, report := writeTest(t, s, Part{Document: 1, Pages: []int{1}})
	if report.DocID == "split-source" || len(report.DocID) != 32 || report.Bytes != int64(len(data)) {
		t.Fatalf("分卷身份/大小错误: %+v", report)
	}
	files := readFiles(t, data)
	for _, forbidden := range []string{"second.png", "Template_1", "Page_1", "Page_2", "Attachments"} {
		for name := range files {
			if strings.Contains(name, forbidden) {
				t.Errorf("带入无关文件: %s", name)
			}
		}
	}
	if !bytes.Contains(files["OFD.xml"], []byte(`DocType="OFD-A"`)) {
		t.Fatal("DocType 丢失")
	}
	if !bytes.Contains(files["Doc_0/DocumentRes.xml"], []byte("Relative=")) {
		t.Fatal("绘制参数的继承依赖丢失")
	}
	for _, value := range files {
		if bytes.Contains(value, []byte("<ofd:Actions>")) || bytes.Contains(value, []byte("<ofd:Bookmarks>")) || bytes.Contains(value, []byte("<ofd:Outlines>")) {
			t.Fatal("无效导航没有清理")
		}
	}
	if len(report.Dropped) == 0 {
		t.Fatal("未报告附件移除")
	}
	parsed, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer parsed.Close()
	if len(parsed.Documents[0].Pages) != 1 {
		t.Fatal("页数错误")
	}
	if annotations, err := parsed.Documents[0].LoadAnnotation(parsed.Documents[0].Pages[0].ID); err != nil || annotations == nil {
		t.Fatalf("注解丢失: %v", err)
	}
}

func TestSplitPartsAreIndependentAndPreservePageBytes(t *testing.T) {
	source := splitFixture(t)
	s := openTest(t, source, Options{Deterministic: true, KeepAttachments: true})
	original := readFiles(t, source)
	first, _ := writeTest(t, s, Part{Document: 1, Pages: []int{2, 1}})
	_, _ = writeTest(t, s, Part{Document: 1, Pages: []int{3}})
	again, _ := writeTest(t, s, Part{Document: 1, Pages: []int{2, 1}})
	if !bytes.Equal(first, again) {
		t.Fatal("前一次分卷修改了共享数据或确定性结果")
	}
	files := readFiles(t, first)
	for _, name := range []string{"Doc_0/Pages/Page_0/Content.xml", "Doc_0/Pages/Page_1/Content.xml", "Doc_0/Pages/Template_0/Content.xml"} {
		if !bytes.Equal(files[name], original[name]) {
			t.Errorf("完整保留导航时原始页面不应变化: %s", name)
		}
	}
	parsed, err := parser.NewOFD(first)
	if err != nil {
		t.Fatal(err)
	}
	defer parsed.Close()
	originalOFD, err := parser.NewOFD(source)
	if err != nil {
		t.Fatal(err)
	}
	defer originalOFD.Close()
	if parsed.Documents[0].Pages[0].ID != originalOFD.Documents[0].Pages[1].ID {
		t.Fatal("重排后页面 ID 改变")
	}
}

func TestSplitWholeDocumentsKeepPathsAndBytes(t *testing.T) {
	source := splitFixture(t)
	var merged bytes.Buffer
	if err := merge.Bytes([][]byte{source, source}, &merged, merge.Options{}); err != nil {
		t.Fatal(err)
	}
	s := openTest(t, merged.Bytes(), Options{Deterministic: true})
	parts, err := s.Plan(Selection{ByDocument: true})
	if err != nil || len(parts) != 2 {
		t.Fatalf("文档体计划错误: %v", err)
	}
	if _, err := s.Plan(Selection{Every: 1}); err == nil {
		t.Fatal("多文档输入不应隐式展平")
	}
	data, report := writeTest(t, s, parts[1])
	if report.DocID != s.Documents()[1].ID {
		t.Fatal("原始 DocID 丢失")
	}
	original := readFiles(t, merged.Bytes())
	files := readFiles(t, data)
	if _, ok := files["Doc_1/Document.xml"]; !ok {
		t.Fatal("原包内路径未保留")
	}
	for name, value := range files {
		if name != "OFD.xml" && !bytes.Equal(value, original[name]) {
			t.Errorf("文档体文件改变: %s", name)
		}
	}
	for name := range files {
		// ZIP 合并后的第二个 DocInfo 仍引用 Doc_0 的封面；共享文件必须随引用保留。
		if name == "Doc_0/Document.xml" || strings.HasPrefix(name, "Doc_0/Pages/") {
			t.Errorf("带入其他文档体: %s", name)
		}
	}
}

func TestSplitPlansBoundsAndCancellation(t *testing.T) {
	s := openTest(t, splitFixture(t), Options{Limits: Limits{MaxParts: 3}})
	parts, err := s.Plan(Selection{Every: 2})
	if err != nil || len(parts) != 2 || !reflect.DeepEqual(parts[1].Pages, []int{3}) {
		t.Fatalf("分卷计划错误: %+v %v", parts, err)
	}
	for _, selection := range []Selection{{}, {Every: -1}, {Every: 1, ByDocument: true}, {Groups: [][]int{{}}}, {Groups: [][]int{{1, 1}}}, {Groups: [][]int{{4}}}, {ByDocument: true, Document: 1}} {
		if _, err := s.Plan(selection); err == nil {
			t.Errorf("未拒绝非法计划: %+v", selection)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out bytes.Buffer
	if _, err := s.Write(ctx, parts[0], &out); !errors.Is(err, context.Canceled) || out.Len() != 0 {
		t.Fatalf("取消未生效: %v", err)
	}
	if _, err := Open(ctx, splitFixture(t), Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open 取消未生效: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(t.Context(), parts[0], &out); !errors.Is(err, container.ErrPackageClosed) {
		t.Fatalf("关闭后仍可写入: %v", err)
	}
}

func TestSplitRejectsUnknownReferencesAndMissingResourcesBeforeWriting(t *testing.T) {
	for _, mutate := range []func(*etree.Document){
		func(tree *etree.Document) { tree.Root().CreateElement("ofd:Unknown").SetText("unknown.bin") },
		func(tree *etree.Document) {
			tree.Root().FindElement(".//ImageObject").CreateAttr("ResourceID", "99999")
		},
	} {
		s := openTest(t, splitFixture(t), Options{})
		name := "Doc_0/Pages/Template_0/Content.xml"
		tree, err := s.readXML(name, "Page")
		if err != nil {
			t.Fatal(err)
		}
		mutate(tree)
		// 同时替换输入文件，确保闭包校验检查到未知结构。
		files := readFiles(t, splitFixture(t))
		data, err := tree.WriteToBytes()
		if err != nil {
			t.Fatal(err)
		}
		files[name] = data
		modified := repack(t, files)
		other := openTest(t, modified, Options{})
		var out bytes.Buffer
		if _, err := other.Write(t.Context(), Part{Document: 1, Pages: []int{1}}, &out); err == nil || out.Len() != 0 {
			t.Fatalf("非法引用应在写入前失败: %v", err)
		}
	}
}

func TestParseGroupsRejectsOversizedAndDuplicateRanges(t *testing.T) {
	groups, err := ParseGroups("1-2;3,1", 3, 2)
	if err != nil || !reflect.DeepEqual(groups, [][]int{{1, 2}, {3, 1}}) {
		t.Fatalf("分组解析错误: %v %v", groups, err)
	}
	for _, value := range []string{"", "1;", "0", "2-1", "1,1", "1-9223372036854775807", "1;2;3"} {
		if _, err := ParseGroups(value, 3, 2); err == nil {
			t.Errorf("未拒绝 %q", value)
		}
	}
}

func TestSplitRejectsTemplateAndResourceCyclesBeforeWriting(t *testing.T) {
	for _, templateCycle := range []bool{true, false} {
		files := readFiles(t, splitFixture(t))
		name := "Doc_0/DocumentRes.xml"
		if templateCycle {
			name = "Doc_0/Pages/Template_0/Content.xml"
		}
		tree := etree.NewDocument()
		if err := tree.ReadFromBytes(files[name]); err != nil {
			t.Fatal(err)
		}
		if templateCycle {
			tree.Root().CreateElement("ofd:Template").CreateAttr("TemplateID", "200")
		} else {
			params := tree.Root().FindElements(".//DrawParam")
			params[0].CreateAttr("Relative", params[1].SelectAttrValue("ID", ""))
		}
		value, err := tree.WriteToBytes()
		if err != nil {
			t.Fatal(err)
		}
		files[name] = value
		s := openTest(t, repack(t, files), Options{})
		var out bytes.Buffer
		if _, err := s.Write(t.Context(), Part{Document: 1, Pages: []int{1}}, &out); err == nil || !strings.Contains(err.Error(), "循环") || out.Len() != 0 {
			t.Fatalf("循环未在写入前拦截: %v", err)
		}
	}
}

type errorWriter struct{ err error }

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }

func TestSplitOutputBudgetErrorsAndConcurrentParts(t *testing.T) {
	s := openTest(t, splitFixture(t), Options{Limits: Limits{MaxOutputBytes: 1}})
	if _, err := s.Write(t.Context(), Part{Document: 1, Pages: []int{1}}, io.Discard); err == nil {
		t.Fatal("输出预算未生效")
	}
	s = openTest(t, splitFixture(t), Options{Deterministic: true})
	sentinel := errors.New("writer failed")
	if _, err := s.Write(t.Context(), Part{Document: 1, Pages: []int{1}}, errorWriter{sentinel}); !errors.Is(err, sentinel) {
		t.Fatalf("写入错误丢失: %v", err)
	}
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			var out bytes.Buffer
			if _, err := s.Write(t.Context(), Part{Document: 1, Pages: []int{1}}, &out); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}

func repack(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var out bytes.Buffer
	archive := zip.NewWriter(&out)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
