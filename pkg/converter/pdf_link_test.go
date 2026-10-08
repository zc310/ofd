package converter

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
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

// readPDFDestinations 读回 Catalog 名称树里的全部命名目标，返回 名称 → 目的地描述。
func readPDFDestinations(t *testing.T, data []byte) map[string]string {
	t.Helper()
	ctx := readPDFContext(t, data)
	namesEntry, ok := ctx.RootDict.Find("Names")
	if !ok {
		return nil
	}
	namesDict, ok := namesEntry.(types.Dict)
	if !ok {
		return nil
	}
	destsEntry, ok := namesDict.Find("Dests")
	if !ok {
		return nil
	}
	destsDict, ok := destsEntry.(types.Dict)
	if !ok {
		return nil
	}
	listEntry, ok := destsDict.Find("Names")
	if !ok {
		return nil
	}
	list, ok := listEntry.(types.Array)
	if !ok {
		return nil
	}
	destinations := make(map[string]string, len(list)/2)
	for i := 0; i+1 < len(list); i += 2 {
		key := stringValue(list[i])
		ref, ok := list[i+1].(types.IndirectRef)
		if !ok {
			continue
		}
		dict, err := ctx.Dereference(ref)
		if err != nil {
			t.Fatalf("解析命名目标 %q 失败: %v", key, err)
		}
		target, ok := dict.(types.Dict)
		if !ok {
			continue
		}
		destination, ok := target.Find("D")
		if !ok {
			continue
		}
		values, ok := destination.(types.Array)
		if !ok {
			continue
		}
		parts := make([]string, 0, len(values))
		for _, value := range values {
			if pageRef, ok := value.(types.IndirectRef); ok {
				if pageNumber, err := ctx.XRefTable.PageNumber(context.Background(), int(pageRef.ObjectNumber)); err == nil {
					parts = append(parts, "page"+strconv.Itoa(pageNumber))
					continue
				}
				parts = append(parts, "pageObj"+strconv.Itoa(int(pageRef.ObjectNumber)))
				continue
			}
			switch v := value.(type) {
			case types.Name:
				parts = append(parts, v.Value())
			case types.Float:
				parts = append(parts, strconv.FormatFloat(float64(v), 'f', -1, 64))
			case types.Integer:
				parts = append(parts, fmt.Sprintf("%d", int(v)))
			}
		}
		destinations[key] = strings.Join(parts, " ")
	}
	return destinations
}

// readPDFLinkAnnotations 回读 PDF 中的全部链接注解。
//
// pdfcpu 只在测试二进制里被引入：pkg/converter 的库代码刻意不链接它，
// 以免只做 OFD→X 的使用方拖进这份依赖。
func readPDFContext(t *testing.T, data []byte) *model.Context {
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
	return ctx
}

func readPDFLinkAnnotations(t *testing.T, data []byte) []pdfLinkAnnotation {
	t.Helper()
	gctx := context.Background()
	ctx := readPDFContext(t, data)
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

// floatValues 取出数组里的全部数值。值为 0 的坐标会被 pdfcpu 解析成 Integer 而
// 不是 Float（PDF 数字对象不区分整数与小数），两种都要收。
func floatValues(values types.Array) []float64 {
	result := make([]float64, 0, len(values))
	for _, value := range values {
		switch number := value.(type) {
		case types.Float:
			result = append(result, float64(number))
		case types.Integer:
			result = append(result, float64(number))
		}
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
// 用例样本 testdata/links.ofd 的第 1 页有一个指向 GitHub 的 Link 注解。
func TestPDFExportEmitsExternalLinksFromAnnotation(t *testing.T) {
	var output bytes.Buffer
	if err := PDF(context.Background(), "../../testdata/links.ofd", &output); err != nil {
		t.Fatalf("导出 PDF 失败: %v", err)
	}
	annots := readPDFLinkAnnotations(t, output.Bytes())

	uris := make([]string, 0, len(annots))
	for _, annot := range annots {
		if annot.uri != "" {
			uris = append(uris, annot.uri)
		}
	}
	if len(uris) != 1 || uris[0] != "https://github.com/zc310/ofd" {
		t.Fatalf("PDF 应只含 1 条外部链接注解，实际 %d 条: %+v", len(uris), uris)
	}
	for _, annot := range annots {
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
	readAllAnnots := readPDFLinkAnnotations(t, output.Bytes())
	var uriAnnot *pdfLinkAnnotation
	for i, annot := range readAllAnnots {
		if annot.uri == "https://github.com/zc310/ofd" {
			uriAnnot = &readAllAnnots[i]
		}
	}
	if uriAnnot == nil {
		t.Fatal("未找到指向 GitHub 的链接注解")
	}
	annots := []pdfLinkAnnotation{*uriAnnot}
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

// TestPDFExportEmitsInternalJumpsFromAnnotations 验证页面跳转变成 PDF 命名目标，
// 且前后两个方向都成立。
//
// 样本 testdata/links.ofd 有两个跳转：第 1 页跳到第 2 页（向前），第 2 页跳回第 1
// 页（向后）。向后跳是必须覆盖的方向——canvas 的 AddAnchor 只能把锚点绑到当前
// 页，实现改用 AddAnchorToPage 才能表达。
func TestPDFExportEmitsInternalJumpsFromAnnotations(t *testing.T) {
	var output bytes.Buffer
	if err := PDF(context.Background(), "../../testdata/links.ofd", &output); err != nil {
		t.Fatalf("导出 PDF 失败: %v", err)
	}
	data := output.Bytes()
	annots := readPDFLinkAnnotations(t, data)
	destinations := readPDFDestinations(t, data)

	// 每条内部跳转注解都要有 /Dest，且该名字必须能在名称树里查到。
	internal := make([]pdfLinkAnnotation, 0, 2)
	for _, annot := range annots {
		if annot.uri == "" {
			internal = append(internal, annot)
		}
	}
	if len(internal) != 2 {
		t.Fatalf("内部跳转注解数 = %d（应为 2）: %+v", len(internal), annots)
	}
	forward, backward := internal[0], internal[1]
	if forward.page != 1 || backward.page != 2 {
		t.Fatalf("跳转所在页不符：第 1 页向前跳、第 2 页向后跳，实际 %d / %d", forward.page, backward.page)
	}
	for _, annot := range internal {
		if annot.destName == "" {
			t.Fatalf("内部跳转缺少 /Dest: %+v", annot)
		}
		if _, ok := destinations[annot.destName]; !ok {
			t.Fatalf("名称树里查不到 %q: %+v", annot.destName, destinations)
		}
	}
	if forward.destName == backward.destName {
		t.Fatalf("指向不同页的两个跳转不应共用锚点: %q", forward.destName)
	}
	// 目标页必须正确：第 1 页的跳转指向 page2，第 2 页的指向 page1。
	//
	// 样本的跳转是 Dest@Type="XYZ" Left="0" Top="0"，即定位到目标页左上角。坐标 0
	// 曾让目的地类型被误判，现按显式类型写出；841.9pt 是 A4 页高，即顶端位置。
	if want := "page2 XYZ 0 841.88976 0"; destinations[forward.destName] != want {
		t.Errorf("向前跳目标 = %q，期望 %q", destinations[forward.destName], want)
	}
	if want := "page1 XYZ 0 841.88976 0"; destinations[backward.destName] != want {
		t.Errorf("向后跳目标 = %q，期望 %q", destinations[backward.destName], want)
	}
}

// TestPDFExportDropsJumpsToPagesOutsidePageRange 守住目标页被页码范围裁掉时跳转
// 被丢弃，而不是写出指向不存在页面的链接。
func TestPDFExportDropsJumpsToPagesOutsidePageRange(t *testing.T) {
	// 只导出第 2 页：第 2 页那条跳回第 1 页的链接目标已不在输出里。
	var output bytes.Buffer
	if err := PDF(context.Background(), "../../testdata/links.ofd", &output,
		Page(2)); err != nil {
		t.Fatalf("导出 PDF 失败: %v", err)
	}
	data := output.Bytes()
	ctx := readPDFContext(t, data)
	if ctx.PageCount != 1 {
		t.Fatalf("输出页数 = %d，期望 1", ctx.PageCount)
	}
	annots := readPDFLinkAnnotations(t, data)
	if len(annots) != 0 {
		t.Fatalf("目标页被裁掉后不应留下任何链接: %+v", annots)
	}
	if destinations := readPDFDestinations(t, data); len(destinations) != 0 {
		t.Fatalf("目标页被裁掉后不应留下命名目标: %+v", destinations)
	}
}

// TestPDFExportInternalJumpsMatchInParallelPath 守住并行渲染路径产出与串行一致的
// 链接。两条路径各自建一次页序映射，容易只改一处。
func TestPDFExportInternalJumpsMatchInParallelPath(t *testing.T) {
	export := func(opts ...Option) (int, int) {
		var output bytes.Buffer
		if err := PDF(context.Background(), "../../testdata/links.ofd", &output, opts...); err != nil {
			t.Fatalf("导出 PDF 失败: %v", err)
		}
		return len(readPDFLinkAnnotations(t, output.Bytes())), len(readPDFDestinations(t, output.Bytes()))
	}
	serialAnnots, serialDests := export()
	parallelAnnots, parallelDests := export(PDFParallel(true))
	if serialAnnots != parallelAnnots || serialDests != parallelDests {
		t.Fatalf("串行 (注解 %d, 目标 %d) 与并行 (注解 %d, 目标 %d) 不一致",
			serialAnnots, serialDests, parallelAnnots, parallelDests)
	}
	if serialAnnots != 3 || serialDests != 2 {
		t.Fatalf("应导出 3 条链接注解与 2 个命名目标，实际 %d / %d", serialAnnots, serialDests)
	}
}

// TestPDFExportWritesEveryDestinationType 验证五种跳转目标类型与缩放比例都精确写入
// PDF 命名目标。
//
// 用例样本 testdata/link-destinations.ofd 的六个热区分别使用 Fit、FitH、FitV、
// XYZ、XYZ 带缩放、FitR，坐标均不为零——坐标为零时相邻类型无法区分（坐标为零的
// XYZ 会被判成 FitH），所以这里刻意用非零坐标。
func TestPDFExportWritesEveryDestinationType(t *testing.T) {
	var output bytes.Buffer
	if err := PDF(context.Background(), "../../testdata/link-destinations.ofd", &output); err != nil {
		t.Fatalf("导出 PDF 失败: %v", err)
	}
	data := output.Bytes()
	annots := readPDFLinkAnnotations(t, data)
	destinations := readPDFDestinations(t, data)

	// 六个向前跳加一个向后跳。
	if len(annots) != 7 || len(destinations) != 7 {
		t.Fatalf("链接注解 %d 个、命名目标 %d 个，期望各 7 个", len(annots), len(destinations))
	}
	byKind := make(map[string][]string)
	for name, destination := range destinations {
		fields := strings.Fields(destination)
		if len(fields) < 2 {
			t.Fatalf("命名目标 %q 内容异常: %q", name, destination)
		}
		byKind[fields[1]] = append(byKind[fields[1]], destination)
	}

	const ptPerMm = 72.0 / 25.4
	mm := func(v float64) float64 { return v * ptPerMm }
	want := []struct {
		kind string
		nums []float64
		page string
	}{
		// Fit 无参数。
		{"Fit", nil, "page2"},
		// top=120 → y=297-120=177；left=60 直接取 x。
		{"FitH", []float64{mm(177)}, "page2"},
		{"FitV", []float64{mm(60)}, "page2"},
		// (40,80) → x=40，y=297-80=217。
		{"XYZ", []float64{mm(40), mm(217), 0}, "page2"},
		// 同一位置，zoom=2。
		{"XYZ", []float64{mm(40), mm(217), mm(2)}, "page2"},
		// FitR 30,60–180,150 → PDF 的四个数是 [左下x 左下y 右上x 右上y]，即
		// [left, 297-top, right, 297-bottom]。
		{"FitR", []float64{mm(30), mm(297 - 60), mm(180), mm(297 - 150)}, "page2"},
		// 第 2 页跳回第 1 页，(0,0) 即顶端。
		{"XYZ", []float64{0, mm(297), 0}, "page1"},
	}
	if len(byKind["XYZ"]) != 3 || len(byKind["FitH"]) != 1 || len(byKind["FitV"]) != 1 ||
		len(byKind["FitR"]) != 1 || len(byKind["Fit"]) != 1 {
		t.Fatalf("目的地类型分布异常: %+v", byKind)
	}
	for _, w := range want {
		candidates := byKind[w.kind]
		matched := false
		for _, candidate := range candidates {
			fields := strings.Fields(candidate)
			if fields[0] != w.page {
				continue
			}
			nums := fields[2:]
			if len(nums) != len(w.nums) {
				continue
			}
			ok := true
			for i, wantNum := range w.nums {
				num, err := strconv.ParseFloat(nums[i], 64)
				if err != nil || math.Abs(num-wantNum) > 0.01 {
					ok = false
					break
				}
			}
			if ok {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("/%s 目标页 %s 坐标不匹配，期望 %v，实际候选 %v",
				w.kind, w.page, w.nums, candidates)
		}
	}
}
