package compare

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image/png"
	"strings"
	"testing"
)

func TestUnchangedXMLIsNotStructurallyAnalyzed(t *testing.T) {
	a := testOFD(t, "same")
	a = rewrite(t, a, func(n string, data []byte) (string, []byte) {
		if strings.HasSuffix(n, ".ttf") {
			return "unused.xml", []byte("<broken>")
		}
		return n, data
	})
	r, err := CompareFiles(t.Context(), []string{a, a}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range r.Inputs {
		for _, f := range input.Files {
			if f.Kind == "xml" && (f.XMLSHA256 != "" || f.XMLNote != "") {
				t.Fatalf("相同 XML 被结构分析: %+v", f)
			}
		}
	}
	if !r.Comparisons[0].Equal {
		t.Fatal("相同文件产生差异")
	}
}

func TestXMLFieldBudgetStopsExpandedPathsAndRetainsRawDifference(t *testing.T) {
	data := []byte(`<x:Root xmlns:x="` + strings.Repeat("namespace", 100) + `"><x:Child a="1"/></x:Root>`)
	if _, err := xmlFieldsLimit(t.Context(), data, 1000); err == nil || !strings.Contains(err.Error(), "字段表") {
		t.Fatal(err)
	}
	a, b := testOFD(t, "old"), testOFD(t, "new")
	r, err := CompareFiles(t.Context(), []string{a, b}, Options{MaxXMLStructureBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if r.Comparisons[0].Equal || len(r.Comparisons[0].Files) == 0 {
		t.Fatal("字段预算隐藏了原始文件差异")
	}
	f := r.Comparisons[0].Files[0]
	if !strings.Contains(f.Before.XMLNote, "字段表") || len(f.XMLChanges) != 0 {
		t.Fatalf("未说明字段表上限: %+v", f)
	}
}

func TestPreviewBudgetAppliesAcrossImagesAndTargets(t *testing.T) {
	a, b := testOFD(t, "Amount 100"), testOFD(t, "Amount 200")
	r, err := CompareFiles(t.Context(), []string{a, b}, Options{Visual: true})
	if err != nil {
		t.Fatal(err)
	}
	v := r.Comparisons[0].Pages[0].Visual
	var total int64
	for _, uri := range []string{v.BeforePNG, v.AfterPNG, v.DiffPNG} {
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(uri, "data:image/png;base64,"))
		if err != nil {
			t.Fatal(err)
		}
		total += int64(len(data))
	}
	for _, limit := range []int64{1, total - 1} {
		if _, err := CompareFiles(t.Context(), []string{a, b}, Options{Visual: true, MaxPreviewBytes: limit}); err == nil || !strings.Contains(err.Error(), "预览图") {
			t.Fatalf("未执行预览预算 %d: %v", limit, err)
		}
	}
	if _, err := CompareFiles(t.Context(), []string{a, b, b}, Options{Visual: true, MaxPreviewBytes: 2*total - 1}); err == nil || !strings.Contains(err.Error(), "预览图") {
		t.Fatal("预算没有跨目标累计", err)
	}
	if _, err := CompareFiles(t.Context(), []string{a, b, b}, Options{Visual: true, MaxPreviewBytes: 2 * total}); err != nil {
		t.Fatal(err)
	}
}

func TestExternalImagesAvoidBase64AndPropagateWriterFailures(t *testing.T) {
	a, b := testOFD(t, "Amount 100"), testOFD(t, "Amount 200")
	images := map[string][]byte{}
	r, err := CompareFiles(t.Context(), []string{a, b}, Options{Visual: true, ImageWriter: func(data []byte) (string, error) {
		if _, err := png.Decode(bytes.NewReader(data)); err != nil {
			return "", err
		}
		name := fmt.Sprintf("images/%d.png", len(images))
		images[name] = bytes.Clone(data)
		return name, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 3 {
		t.Fatalf("外置图片数 %d", len(images))
	}
	for _, format := range []string{"html", "json"} {
		var out bytes.Buffer
		if err := WriteReport(&out, r, format); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "data:image/png;base64,") || !strings.Contains(out.String(), "images/0.png") {
			t.Fatalf("未引用外置图片: %s", format)
		}
	}
	failure := errors.New("image write failed")
	if _, err := CompareFiles(t.Context(), []string{a, b}, Options{Visual: true, ImageWriter: func([]byte) (string, error) { return "", failure }}); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	if _, err := CompareFiles(ctx, []string{a, b}, Options{Visual: true, ImageWriter: func([]byte) (string, error) { cancel(); return "images/a.png", nil }}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := CompareFiles(t.Context(), []string{a, b}, Options{Visual: true, ImageWriter: func([]byte) (string, error) { return "https://example.com/a.png", nil }}); err == nil {
		t.Fatal("接受了外部网络图片地址")
	}
}

func TestReportDataAndOutputBudgetsAreEnforced(t *testing.T) {
	a := testOFD(t, "same")
	if _, err := CompareFiles(t.Context(), []string{a, a}, Options{MaxReportBytes: 100}); err == nil || !strings.Contains(err.Error(), "报告数据") {
		t.Fatal(err)
	}
	r, err := CompareFiles(t.Context(), []string{a, a}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	r.Options.MaxReportBytes = 128
	for _, format := range []string{"text", "markdown", "json", "html"} {
		var out bytes.Buffer
		err := WriteReport(&out, r, format)
		if err == nil || !strings.Contains(err.Error(), "报告输出") || out.Len() > 128 {
			t.Fatalf("%s: %v 输出 %d 字节", format, err, out.Len())
		}
	}
	r.Options.MaxReportBytes = 0
	var out bytes.Buffer
	if err := WriteReport(&out, r, " MARKDOWN "); err != nil || !strings.HasPrefix(out.String(), "# OFD") {
		t.Fatalf("格式归一化失败: %v", err)
	}
}

func TestRelativeImageURLsRejectTraversalAndSchemes(t *testing.T) {
	for _, value := range []string{"images/a.png", "images%20dir/a.png"} {
		if !relativeImageURL(value) {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"https://host/a.png", "//host/a.png", "/a.png", "../a.png", "images/%2e%2e/a.png", "images%2f..%2fa.png", "images\\a.png", "javascript:alert.png", "a.png?x=1", "a.png#fragment", "a.svg"} {
		if relativeImageURL(value) {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"a.png?", "a.png#"} {
		if relativeImageURL(value) {
			t.Fatal(value)
		}
	}
}
