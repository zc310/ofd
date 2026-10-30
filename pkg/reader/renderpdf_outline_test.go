package reader

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestRenderPDFToWritesOutline 验证阅读器导出 PDF 时写出文档大纲。
//
// 这条路径与 pkg/converter 的导出是两条独立实现：转换器按全局页序建映射，
// 阅读器则按调用方传入的 indices 建映射——indices 可以乱序、也可以只包含部分页，
// 因此必须单独覆盖，否则会出现「转换器导出有目录、阅读器导出没有」的不一致。
func TestRenderPDFToWritesOutline(t *testing.T) {
	data, err := os.ReadFile("../../testdata/ofd-creator/outlines.ofd")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()

	var output bytes.Buffer
	if err := reader.RenderPDFTo(t.Context(), &output, []int{0, 1, 2}, RenderOptions{}); err != nil {
		t.Fatalf("导出 PDF 失败: %v", err)
	}
	body := output.String()
	if !strings.Contains(body, "/Outlines") {
		t.Fatal("导出的 PDF 没有大纲")
	}
	// 六个内部跳转与一个外部URI条目都应保留。
	if got := strings.Count(body, "/Dest["); got != 6 {
		t.Errorf("大纲条目数 = %d，期望 6", got)
	}
	if !strings.Contains(body, "/S/URI") {
		t.Error("外部URI大纲未导出")
	}
}

// TestRenderPDFToDropsOutlineEntriesOutsideSelection 守住只导出部分页时，指向
// 未导出页的大纲条目被丢弃。
func TestRenderPDFToDropsOutlineEntriesOutsideSelection(t *testing.T) {
	data, err := os.ReadFile("../../testdata/ofd-creator/outlines.ofd")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()

	// 只导出第 1 页：指向第 2、3 页的条目都应消失，只剩第 1 页的三个。
	var output bytes.Buffer
	if err := reader.RenderPDFTo(t.Context(), &output, []int{0}, RenderOptions{}); err != nil {
		t.Fatalf("导出 PDF 失败: %v", err)
	}
	body := output.String()
	if !strings.Contains(body, "/Outlines") {
		t.Fatal("导出的 PDF 没有大纲")
	}
	if got := strings.Count(body, "/Dest["); got != 3 {
		t.Errorf("大纲条目数 = %d，期望 3", got)
	}
}

// TestRenderPDFToOutlineFollowsSelectionOrder 守住页序按导出序列而非文档顺序计算：
// indices 乱序时目标页仍要落在正确的输出页上。
func TestRenderPDFToOutlineFollowsSelectionOrder(t *testing.T) {
	data, err := os.ReadFile("../../testdata/ofd-creator/outlines.ofd")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()

	// 倒序导出。「概述」指向文档第 1 页，输出里应是最后一页。
	var output bytes.Buffer
	if err := reader.RenderPDFTo(t.Context(), &output, []int{2, 1, 0}, RenderOptions{}); err != nil {
		t.Fatalf("导出 PDF 失败: %v", err)
	}
	body := output.String()
	if !strings.Contains(body, "/Outlines") {
		t.Fatal("导出的 PDF 没有大纲")
	}
	// 最后一个大纲对象的 /Dest 应指向最后一页的对象。
	at := strings.LastIndex(body, "/Dest[")
	if at < 0 {
		t.Fatal("找不到大纲目的地")
	}
	// 深度优先顺序下末条是「引用附录」，它按书签名指向文档第 3 页；indices
	// 为 {2,1,0}，该页落在输出首页，因此目标引用应是第 1 页的对象。
	fragment := body[at : at+40]
	if !strings.Contains(fragment, "/FitH") {
		t.Errorf("末条目的地 = %q，期望 FitH", fragment)
	}
	firstPageRef := pageObjectRefAfter(body, "/Type/Page")
	if firstPageRef != "" && !strings.Contains(fragment, firstPageRef+" /FitH") {
		t.Errorf("末条目标 = %q，期望指向输出首页 %q", fragment, firstPageRef)
	}
}

// pageObjectRefAfter 返回文档中第一个页对象的间接引用，形如 "6 0 R"。
func pageObjectRefAfter(body string, marker string) string {
	at := strings.Index(body, marker)
	if at < 0 {
		return ""
	}
	head := body[:at]
	if nl := strings.LastIndex(head, "\n"); nl >= 0 {
		head = head[:nl]
	}
	fields := strings.Fields(head)
	if len(fields) >= 3 && fields[1] == "0" && fields[2] == "obj" {
		return fields[0] + " 0 R"
	}
	return ""
}
