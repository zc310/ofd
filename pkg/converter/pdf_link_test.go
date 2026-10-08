package converter

import (
	"bytes"
	"context"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// pdfLinkAnnotation 是从生成结果里读回的一条链接注解。
type pdfLinkAnnotation struct {
	page     int
	rect     []float64
	uri      string
	destName string
}

// readPDFLinkAnnotations 回读 PDF 中的全部链接注解。
//
// pdfcpu 只在测试二进制里被引入：pkg/converter 的库代码刻意不链接它，
// 以免只做 OFD→X 的使用方拖进这份依赖。
func readPDFLinkAnnotations(t *testing.T, data []byte) []pdfLinkAnnotation {
	t.Helper()
	gctx := context.Background()
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	ctx, err := api.ReadContext(gctx, bytes.NewReader(data), conf)
	if err != nil {
		t.Fatalf("读取 PDF 失败: %v", err)
	}
	if err := api.ValidateContext(gctx, ctx); err != nil {
		t.Fatalf("校验 PDF 失败: %v", err)
	}
	pages := ctx.PageCount
	results := make([]pdfLinkAnnotation, 0, 4)
	for page := 1; page <= pages; page++ {
		pageDict, _, _, err := ctx.PageDict(gctx, page, false)
		if err != nil {
			t.Fatalf("读取第 %d 页失败: %v", page, err)
		}
		entry, ok := pageDict.Find("Annots")
		if !ok {
			continue
		}
		annots, ok := entry.(types.Array)
		if !ok {
			continue
		}
		for _, object := range annots {
			dict, ok := object.(types.Dict)
			if !ok {
				continue
			}
			subtype, ok := dict.Find("Subtype")
			if !ok || subtype != types.Name("Link") {
				continue
			}
			annotation := pdfLinkAnnotation{page: page}
			if rect, ok := dict.Find("Rect"); ok {
				if values, ok := rect.(types.Array); ok {
					annotation.rect = floatValues(values)
				}
			}
			if action, ok := dict.Find("A"); ok {
				if actionDict, ok := action.(types.Dict); ok {
					if uri, ok := actionDict.Find("URI"); ok {
						annotation.uri = stringValue(uri)
					}
				}
			}
			if dest, ok := dict.Find("Dest"); ok {
				annotation.destName = stringValue(dest)
			}
			results = append(results, annotation)
		}
	}
	return results
}

func floatValues(values types.Array) []float64 {
	result := make([]float64, 0, len(values))
	for _, value := range values {
		number, ok := value.(types.Float)
		if !ok {
			continue
		}
		result = append(result, float64(number))
	}
	return result
}

func stringValue(object types.Object) string {
	switch value := object.(type) {
	case types.Name:
		return value.Value()
	case types.StringLiteral:
		return string(value)
	case types.HexLiteral:
		return string(value)
	}
	return ""
}

// TestPDFExportEmitsExternalLinksFromAnnotation 验证 OFD 页面注解上的外部链接会
// 变成 PDF 里可点击的链接注解。
//
// 用例样本 testdata/links.ofd 的第 1 页有一个指向 GitHub 的 Link 注解；另外两个
// 是页面跳转，PDF 导出暂不支持，应被丢弃而不是写成坏的注解。
func TestPDFExportEmitsExternalLinksFromAnnotation(t *testing.T) {
	var output bytes.Buffer
	if err := PDF(context.Background(), "../../testdata/links.ofd", &output); err != nil {
		t.Fatalf("导出 PDF 失败: %v", err)
	}
	annots := readPDFLinkAnnotations(t, output.Bytes())

	uris := make([]string, 0, len(annots))
	for _, annot := range annots {
		uris = append(uris, annot.uri)
	}
	if len(uris) != 1 || uris[0] != "https://github.com/zc310/ofd" {
		t.Fatalf("PDF 应只含 1 条外部链接注解，实际 %d 条: %+v", len(uris), uris)
	}
	for _, annot := range annots {
		if annot.uri == "" {
			t.Errorf("第 %d 页存在没有 URI 的链接注解（页面跳转不应被导出）: %+v", annot.page, annot)
		}
		if len(annot.rect) != 4 {
			t.Errorf("链接注解 Rect 不完整: %+v", annot.rect)
		}
	}
}

// TestPDFExportLinkRectMatchesAnnotationBoundary 验证热区落在注解声明的位置上。
//
// 样本 testdata/links.ofd 第 1 页指向 GitHub 的 Link 注解其 Appearance Boundary
// 是 "30 180 150 10"（距页顶，mm），A4 页面高 297mm。PDF 坐标原点在左下角且
// y 向上，换算后应为 X=30、宽 150、上边 297-190、下边 297-180，最后按 72/25.4
// 转成点。
func TestPDFExportLinkRectMatchesAnnotationBoundary(t *testing.T) {
	var output bytes.Buffer
	if err := PDF(context.Background(), "../../testdata/links.ofd", &output); err != nil {
		t.Fatalf("导出 PDF 失败: %v", err)
	}
	annots := readPDFLinkAnnotations(t, output.Bytes())
	if len(annots) != 1 {
		t.Fatalf("链接注解数 = %d", len(annots))
	}
	const ptPerMm = 72.0 / 25.4
	want := []float64{30 * ptPerMm, (297 - 190) * ptPerMm, 180 * ptPerMm, (297 - 180) * ptPerMm}
	got := annots[0].rect
	if len(got) != len(want) {
		t.Fatalf("Rect = %v，期望 %v", got, want)
	}
	for i := range want {
		if diff := got[i] - want[i]; diff > 0.5 || diff < -0.5 {
			t.Fatalf("Rect = %v，期望约 %v", got, want)
		}
	}
}

// TestPDFExportWithoutLinksHasNoAnnotations 守住没有链接的文档不会凭空产生注解，
// 也不该因为链接导出而让 PDF 结构变化。
func TestPDFExportWithoutLinksHasNoAnnotations(t *testing.T) {
	var output bytes.Buffer
	if err := PDF(context.Background(), "../../testdata/helloworld.ofd", &output); err != nil {
		t.Fatalf("导出 PDF 失败: %v", err)
	}
	if annots := readPDFLinkAnnotations(t, output.Bytes()); len(annots) != 0 {
		t.Fatalf("无链接文档不应产生链接注解，实际 %+v", annots)
	}
}
