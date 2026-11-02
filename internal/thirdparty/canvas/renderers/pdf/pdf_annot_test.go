package pdf

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

var pageObjPattern = regexp.MustCompile(`(\d+) 0 obj\s*<</Type/Page[^s]`)

// anchorRefPattern matches one name tree entry, such as "(to-second) 11 0 R".
var anchorRefPattern = regexp.MustCompile(`\(([A-Za-z0-9_.-]+)\)\s*(\d+) 0 R`)

// pageObjects returns the indirect references of the document's page objects, in
// document order, each formatted like "5 0 R".
func pageObjects(out string) []string {
	matches := pageObjPattern.FindAllStringSubmatch(out, -1)
	refs := make([]string, 0, len(matches))
	for _, m := range matches {
		refs = append(refs, m[1]+" 0 R")
	}
	return refs
}

// anchorDest returns the /D destination array that the name tree maps key onto.
//
// The name tree holds an indirect reference to a <</D [...]>> object rather than
// the array itself, so the object number has to be resolved first.
func anchorDest(t *testing.T, out, key string) string {
	t.Helper()
	for _, m := range anchorRefPattern.FindAllStringSubmatch(out, -1) {
		if m[1] != key {
			continue
		}
		marker := "\n" + m[2] + " 0 obj"
		idx := strings.Index(out, marker)
		if idx < 0 {
			t.Fatalf("anchor object %s not found", marker)
		}
		rest := out[idx:]
		at := strings.Index(rest, "/D")
		if at < 0 {
			t.Fatalf("anchor object has no /D:\n%s", rest)
		}
		rest = rest[at:]
		end := strings.Index(rest, "]")
		if end < 0 {
			t.Fatalf("destination array is not closed:\n%s", rest)
		}
		return rest[:end+1]
	}
	t.Fatalf("name %q not found in the name tree:\n%s", key, out)
	return ""
}

// buildAnnotatedPDF writes a PDF with the given number of pages, calling register
// after each page is rendered, and returns the raw bytes.
func buildAnnotatedPDF(t *testing.T, pages int, register func(doc *PDF, index int)) string {
	t.Helper()
	buf := &bytes.Buffer{}
	opts := DefaultOptions
	opts.Compress = false
	doc := New(buf, 210, 297, &opts)
	for index := range pages {
		surface := canvas.New(210, 297)
		surface.RenderTo(doc)
		if register != nil {
			register(doc, index)
		}
		if index < pages-1 {
			doc.NewPage(210, 297)
		}
	}
	if err := doc.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// TestAddAnchorToPageBindsNotYetCreatedPage checks that an anchor can target a page
// that has not been written out yet.
//
// AddAnchor binds an anchor to whichever page is current, so it can only express a
// link whose target follows its source. Here a second-page anchor is registered
// while only the first page exists, and the name tree must still resolve it to the
// second page.
func TestAddAnchorToPageBindsNotYetCreatedPage(t *testing.T) {
	out := buildAnnotatedPDF(t, 2, func(doc *PDF, index int) {
		if index != 0 {
			return
		}
		// The first page has not been written yet; register an anchor for the
		// second one anyway.
		doc.AddAnchorToPage(1, "to-second", canvas.Rect{X0: 0, Y0: 100, X1: 50, Y1: 120})
		doc.AddLink("#to-second", canvas.Rect{X0: 10, Y0: 150, X1: 80, Y1: 165})
	})

	if !strings.Contains(out, "/Dest/to-second") {
		t.Fatalf("Dest is not written as a name object:\n%s", out)
	}
	pages := pageObjects(out)
	if len(pages) != 2 {
		t.Fatalf("page object count = %d, want 2:\n%s", len(pages), out)
	}
	if dest := anchorDest(t, out, "to-second"); !strings.Contains(dest, pages[1]) {
		t.Fatalf("to-second resolves to %q, want the second page %q", dest, pages[1])
	}
}

// TestAddAnchorBindsCurrentPage pins the existing AddAnchor behaviour, which binds
// to the current page, and covers how the destination is spelled.
func TestAddAnchorBindsCurrentPage(t *testing.T) {
	out := buildAnnotatedPDF(t, 1, func(doc *PDF, index int) {
		doc.AddAnchor("here", canvas.Rect{X0: 0, Y0: 100, X1: 50, Y1: 120})
		doc.AddLink("#here", canvas.Rect{X0: 10, Y0: 150, X1: 80, Y1: 165})
	})

	if !strings.Contains(out, "/Dest/here") {
		t.Fatalf("Dest is not written as a name object:\n%s", out)
	}
	pages := pageObjects(out)
	if len(pages) != 1 {
		t.Fatalf("page object count = %d, want 1:\n%s", len(pages), out)
	}
	if dest := anchorDest(t, out, "here"); !strings.Contains(dest, pages[0]) {
		t.Fatalf("here resolves to %q, want the first page %q", dest, pages[0])
	}
}

// TestAddAnchorToPageOutOfRangeIsDropped checks that an anchor naming a page that
// never gets written does not fail Close.
//
// AddAnchorToPage deliberately accepts a page index that does not exist yet, since
// the whole point is to register an anchor before its target page is created. The
// index only becomes checkable in Close, by which point a caller that stopped early
// may have left a dangling anchor behind. That must not panic.
func TestAddAnchorToPageOutOfRangeIsDropped(t *testing.T) {
	buf := &bytes.Buffer{}
	opts := DefaultOptions
	opts.Compress = false
	doc := New(buf, 210, 297, &opts)

	surface := canvas.New(210, 297)
	surface.RenderTo(doc)
	// Only one page is ever written, but the anchors name a second and a third.
	doc.AddAnchorToPage(1, "missing", canvas.Rect{X0: 0, Y0: 100, X1: 50, Y1: 120})
	doc.AddAnchorToPage(2, "also-missing", canvas.Rect{X0: 0, Y0: 100, X1: 50, Y1: 120})
	doc.AddLink("#missing", canvas.Rect{X0: 10, Y0: 150, X1: 80, Y1: 165})

	if err := doc.Close(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "%%EOF") {
		t.Fatalf("Close did not finish the document:\n%s", out)
	}
	if strings.Contains(out, "also-missing") {
		t.Fatalf("dangling anchor was written to the name tree:\n%s", out)
	}
}

// mm 把毫米换算成 PDF 点，保留两位小数，与写出格式一致。
func mm(v float64) string { return strconv.FormatFloat(v*ptPerMm, 'f', 2, 64) }

// destOf 返回名称树里 key 对应的目的地数组，去掉空白便于断言。
func destOf(t *testing.T, out, key string) string {
	t.Helper()
	return strings.Join(strings.Fields(anchorDest(t, out, key)), " ")
}

// destFields 返回名称树里 key 对应的目的地数组，已去掉 /D 前缀并按空白拆开。
func destFields(t *testing.T, out, key string) []string {
	t.Helper()
	raw := strings.TrimSpace(anchorDest(t, out, key))
	raw = strings.TrimPrefix(raw, "/D")
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "["))
	raw = strings.TrimSpace(strings.TrimSuffix(raw, "]"))
	return strings.Fields(raw)
}

// checkDest 逐项比较目的地：名称精确匹配，数值按容差比较。写出时的浮点位数由
// canvas.Precision 与 minify.Decimal 决定，不便按字符串断言。
func checkDest(t *testing.T, got []string, wantKind string, wantNums ...float64) {
	t.Helper()
	// 目的地数组形如 [N 0 R /Kind num...]，页引用占三项。
	if len(got) != 4+len(wantNums) {
		t.Fatalf("目的地 = %v，期望 %d 项", got, 4+len(wantNums))
	}
	if got[1] != "0" || got[2] != "R" {
		t.Fatalf("前三项应为页引用，实际 %v", got[:3])
	}
	if got[3] != "/"+wantKind {
		t.Fatalf("目的地类型 = %q，期望 /%s", got[3], wantKind)
	}
	for i, want := range wantNums {
		token := got[4+i]
		num, err := strconv.ParseFloat(token, 64)
		if err != nil {
			t.Fatalf("第 %d 项 %q 不是数值", 4+i, token)
		}
		if diff := num - want; diff > 0.01 || diff < -0.01 {
			t.Fatalf("第 %d 项 = %v，期望 %v", 4+i, num, want)
		}
	}
}

// TestAddDestToPageWritesExplicitKinds 验证 AddDestToPage 按显式类型写出目的地，
// 不像 AddAnchor 那样从矩形形状反推。
func TestAddDestToPageWritesExplicitKinds(t *testing.T) {
	const height = 297.0
	mm := func(v float64) float64 { return v * ptPerMm }
	cases := []struct {
		name string
		dest Dest
		kind string
		nums []float64
	}{
		{"fit", Dest{Kind: DestFit}, "Fit", nil},
		// 距页顶 100mm → 距页底 197mm。
		{"fith", Dest{Kind: DestFitH, Y: 100}, "FitH", []float64{mm(197)}},
		{"fitv", Dest{Kind: DestFitV, X: 50}, "FitV", []float64{mm(50)}},
		{
			"xyz", Dest{Kind: DestXYZ, X: 50, Y: 100}, "XYZ",
			[]float64{mm(50), mm(197), 0},
		},
		{"fith-top", Dest{Kind: DestFitH, Y: 0}, "FitH", []float64{mm(297)}},
		// 这两种正是 AddAnchor 无法表达的：x 为 0 的 XYZ 曾被反推成 FitH，
		// 而 FitV 的 x 为 0 曾被反推成整页 Fit。
		{"xyz-origin-x", Dest{Kind: DestXYZ, X: 0, Y: 100}, "XYZ", []float64{0, mm(197), 0}},
		{"fitv-origin-x", Dest{Kind: DestFitV, X: 0}, "FitV", []float64{0}},
		{
			"fitr", Dest{Kind: DestFitR, X0: 10, Y0: 60, X1: 110, Y1: 20}, "FitR",
			[]float64{mm(10), mm(277), mm(110), mm(237)},
		},
	}
	for _, c := range cases {
		out := buildAnnotatedPDF(t, 1, func(doc *PDF, index int) {
			doc.AddDestToPage(0, height, c.name, c.dest)
			doc.AddLink("#"+c.name, canvas.Rect{X0: 10, Y0: 150, X1: 80, Y1: 165})
		})
		t.Run(c.name, func(t *testing.T) {
			checkDest(t, destFields(t, out, c.name), c.kind, c.nums...)
		})
	}
}

// TestAddDestToPageKeepsZoom 验证 Zoom 不会被写成 0。
//
// AddAnchor 从矩形形状反推目的地类型，XYZ 的缩放被硬编码为 0；显式 API 必须
// 原样写出。
func TestAddDestToPageKeepsZoom(t *testing.T) {
	for _, zoom := range []float64{2.5, 1, 0.5} {
		out := buildAnnotatedPDF(t, 1, func(doc *PDF, index int) {
			doc.AddDestToPage(0, 297, "zoomed", Dest{Kind: DestXYZ, X: 10, Y: 20, Zoom: zoom})
			doc.AddLink("#zoomed", canvas.Rect{X0: 10, Y0: 150, X1: 80, Y1: 165})
		})
		checkDest(t, destFields(t, out, "zoomed"), "XYZ", 10*ptPerMm, (297-20)*ptPerMm, zoom*ptPerMm)
	}
}

// TestAddDestToPageOutOfRangeIsDropped 验证目标页未写出时该目的地被丢弃，且不影响
// 同一文档里其它内容。
func TestAddDestToPageOutOfRangeIsDropped(t *testing.T) {
	out := buildAnnotatedPDF(t, 1, func(doc *PDF, index int) {
		doc.AddDestToPage(3, 297, "dangling", Dest{Kind: DestFit})
		doc.AddDestToPage(0, 297, "valid", Dest{Kind: DestFit})
		doc.AddLink("#dangling", canvas.Rect{X0: 10, Y0: 150, X1: 80, Y1: 165})
		doc.AddLink("#valid", canvas.Rect{X0: 10, Y0: 200, X1: 80, Y1: 215})
	})
	if !strings.Contains(out, "%%EOF") {
		t.Fatalf("Close 未完成文档:\n%s", out)
	}
	// 名称树以 "(name) N 0 R" 的形式出现；链接注解里的 /Dest 也会提到这个名字，
	// 所以只检查名称树本身。
	if strings.Contains(out, "(dangling) ") {
		t.Fatalf("悬空目的地被写入名称树:\n%s", out)
	}
	checkDest(t, destFields(t, out, "valid"), "Fit")
}

// outlineEntry 是从 PDF 产物里读回的一条大纲记录。
type outlineEntry struct {
	title   string
	pageRef string // 目标页对象的间接引用，形如 "7 0 R"
	kind    string // 目的地类型，不含前导斜杠
	nums    []float64
}

// outlineEntries 解析全部大纲条目。
//
// 大纲对象的字典键按字母序写出（/Count /Dest /First /Parent /Prev /Title），
// /Title 通常不在开头，因此按对象边界逐个解析而不是靠固定顺序。
func outlineEntries(out string) []outlineEntry {
	entries := make([]outlineEntry, 0, 4)
	for _, m := range regexp.MustCompile(`(?s)\d+ 0 obj\s*<<[^>]*?/Title\(([^)]*)\)`).FindAllStringSubmatch(out, -1) {
		dest := regexp.MustCompile(`/Dest\[\s*(\d+) 0 R\s*/(\w+)([^\]]*)\]`).FindStringSubmatch(m[0])
		if dest == nil {
			continue
		}
		entry := outlineEntry{title: m[1], pageRef: dest[1] + " 0 R", kind: dest[2]}
		for _, token := range strings.Fields(dest[3]) {
			if num, err := strconv.ParseFloat(token, 64); err == nil {
				entry.nums = append(entry.nums, num)
			}
		}
		entries = append(entries, entry)
	}
	return entries
}

// TestAddOutlineToPageWritesExplicitDests 验证 AddOutlineToPage 按显式类型写出每个
// 大纲条目的目的地。
//
// AddOutline 只能从 y 推断 Fit 或 FitH，XYZ/FitV/FitR 都表达不了。
func TestAddOutlineToPageWritesExplicitDests(t *testing.T) {
	const height = 297.0
	mm := func(v float64) float64 { return v * ptPerMm }
	// 注册回调对每页都会调用，这里只在第一页注册，避免重复。
	out := buildAnnotatedPDF(t, 2, func(doc *PDF, index int) {
		if index != 0 {
			return
		}
		doc.AddOutlineToPage(1, height, "fit", 0, Dest{Kind: DestFit})
		doc.AddOutlineToPage(1, height, "fith", 0, Dest{Kind: DestFitH, Y: 100})
		doc.AddOutlineToPage(1, height, "fitv", 1, Dest{Kind: DestFitV, X: 60})
		doc.AddOutlineToPage(1, height, "xyz", 1, Dest{Kind: DestXYZ, X: 40, Y: 80, Zoom: 2})
		doc.AddOutlineToPage(0, height, "fitr", 0, Dest{Kind: DestFitR, X0: 10, Y0: 60, X1: 110, Y1: 20})
	})

	got := outlineEntries(out)
	want := []struct {
		title string
		kind  string
		// page 是 0 基的目标页，用来确认绑到的是显式页序而非当前页。
		page int
		nums []float64
	}{
		{"fit", "Fit", 1, nil},
		{"fith", "FitH", 1, []float64{mm(197)}},
		{"fitv", "FitV", 1, []float64{mm(60)}},
		{"xyz", "XYZ", 1, []float64{mm(40), mm(217), mm(2)}},
		{"fitr", "FitR", 0, []float64{mm(10), mm(277), mm(110), mm(237)}},
	}
	if len(got) != len(want) {
		t.Fatalf("大纲条目 %d 条，期望 %d 条: %+v", len(got), len(want), got)
	}
	pageRefs := pageObjects(out)
	for i, w := range want {
		entry := got[i]
		if entry.title != w.title || entry.kind != w.kind {
			t.Errorf("第 %d 条 = %s/%s，期望 %s/%s", i, entry.title, entry.kind, w.title, w.kind)
			continue
		}
		if entry.pageRef != pageRefs[w.page] {
			t.Errorf("%s 目标 = %s，期望第 %d 页 %s", w.title, entry.pageRef, w.page+1, pageRefs[w.page])
		}
		if len(entry.nums) != len(w.nums) {
			t.Errorf("%s 数值项 %d 个，期望 %d 个: %v", w.title, len(entry.nums), len(w.nums), entry.nums)
			continue
		}
		for j, expect := range w.nums {
			if diff := entry.nums[j] - expect; diff > 0.01 || diff < -0.01 {
				t.Errorf("%s 第 %d 个数值 = %v，期望 %v", w.title, j, entry.nums[j], expect)
			}
		}
	}
}

func TestAddOutlineURIKeepsActionsAndMixedTree(t *testing.T) {
	out := buildAnnotatedPDF(t, 2, func(doc *PDF, index int) {
		if index != 0 {
			return
		}
		doc.AddOutlineURI("external", 0, `https://example.com/a(b)\c`)
		doc.AddOutlineToPage(1, 297, "internal-child", 1, Dest{Kind: DestXYZ, X: 0, Y: 20, Zoom: 2})
		doc.AddOutlineURI("外部链接", 0, "https://example.com/second")
		doc.AddOutlineURI("empty", 0, "")
	})
	var externalRef, child string
	for _, object := range regexp.MustCompile(`(?s)(\d+) 0 obj\s*(.*?)\s*endobj`).FindAllStringSubmatch(out, -1) {
		body := object[2]
		if strings.Contains(body, "/Title(external)") {
			externalRef = object[1] + " 0 R"
			if !strings.Contains(body, "/S/URI") || !strings.Contains(body, `https://example.com/a\(b\)\\c`) || strings.Contains(body, "/Dest") {
				t.Fatalf("URI动作编码错误：%s", body)
			}
		}
		if strings.Contains(body, "/Title(internal-child)") {
			child = body
		}
	}
	if externalRef == "" || !strings.Contains(child, "/Parent "+externalRef) || !strings.Contains(child, "/XYZ") {
		t.Fatalf("URI与内部跳转混排破坏树：%s", out)
	}
	if strings.Contains(out, "/Title(empty)") || !strings.Contains(out, pdfTextString("外部链接")) {
		t.Fatal("空URI或中文标题处理错误")
	}
}

// TestAddOutlineToPageBuildsTree 验证层级被正确还原成大纲树。
func TestAddOutlineToPageBuildsTree(t *testing.T) {
	out := buildAnnotatedPDF(t, 2, func(doc *PDF, index int) {
		doc.AddOutlineToPage(0, 297, "top", 0, Dest{Kind: DestFit})
		doc.AddOutlineToPage(1, 297, "child-a", 1, Dest{Kind: DestFit})
		doc.AddOutlineToPage(1, 297, "child-b", 1, Dest{Kind: DestFit})
		doc.AddOutlineToPage(0, 297, "top2", 0, Dest{Kind: DestFit})
	})
	// child-a 的 /Parent 应指向 top 的对象号，top 的 /Count 应为 2。
	if !regexp.MustCompile(`/Count 2[^>]*?/Title\(top\)`).MatchString(out) {
		t.Errorf("top 未记录 2 个后代:\n%s", out)
	}
	objOf := func(title string) string {
		m := regexp.MustCompile(`(?s)(\d+) 0 obj\s*<[^>]*?/Title\(` + title + `\)`).FindStringSubmatch(out)
		if m == nil {
			t.Errorf("找不到条目对象 %s", title)
			return ""
		}
		return m[1]
	}
	top := objOf("top")
	for _, title := range []string{"child-a", "child-b"} {
		re := regexp.MustCompile(`(?s)\d+ 0 obj\s*<[^>]*?/Parent (\d+) 0 R[^>]*?/Title\(` + title + `\)`)
		m := re.FindStringSubmatch(out)
		if m == nil {
			t.Errorf("%s 缺少 /Parent:\n%s", title, out)
			continue
		}
		if m[1] != top {
			t.Errorf("%s 的 Parent = %s，期望 top 的对象号 %s", title, m[1], top)
		}
	}
}

// TestAddOutlineToPageOutOfRangeDropped 验证目标页未写出时该条目及其子树被丢弃。
func TestAddOutlineToPageOutOfRangeDropped(t *testing.T) {
	out := buildAnnotatedPDF(t, 1, func(doc *PDF, index int) {
		doc.AddOutlineToPage(5, 297, "gone", 0, Dest{Kind: DestFit})
		doc.AddOutlineToPage(5, 297, "gone-child", 1, Dest{Kind: DestFit})
		doc.AddOutlineToPage(0, 297, "kept", 0, Dest{Kind: DestFit})
		doc.AddOutlineToPage(0, 297, "kept-child", 1, Dest{Kind: DestFit})
	})
	entries := outlineEntries(out)
	for _, e := range entries {
		if strings.HasPrefix(e.title, "gone") {
			t.Errorf("悬空条目未丢弃: %+v", entries)
			break
		}
	}
	if len(entries) != 2 {
		t.Fatalf("应只剩 2 条，丢弃整棵子树后实际 %d 条: %+v", len(entries), entries)
	}
}

// TestOutlineTitleEncodesNonASCII 验证非 ASCII 标题写成带 BOM 的 UTF-16BE。
//
// PDF 文本字符串在无 BOM 时按 PDFDocEncoding 解释，直接写入 UTF-8 字节会在阅读器里
// 显示为乱码。
func TestOutlineTitleEncodesNonASCII(t *testing.T) {
	out := buildAnnotatedPDF(t, 1, func(doc *PDF, index int) {
		doc.AddOutlineToPage(0, 297, "概述", 0, Dest{Kind: DestFit})
		doc.AddOutlineToPage(0, 297, "ASCII only", 1, Dest{Kind: DestFit})
	})
	// 「概述」的 UTF-16BE 编码：BOM + 0x6982 0x8ff0。
	if !strings.Contains(out, "/Title(\xfe\xff\x69\x82\x8f\xf0)") {
		t.Errorf("中文标题未写成 UTF-16BE:\n%s", out)
	}
	if !strings.Contains(out, "/Title(ASCII only)") {
		t.Errorf("ASCII 标题不应加 BOM:\n%s", out)
	}
}
