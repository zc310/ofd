package pdf2ofd

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
)

// convertLinkPDF 转换一份手工拼装的 PDF，返回解析后的 OFD 文档。
func convertLinkPDF(t *testing.T, objects []string) *parser.Document {
	t.Helper()
	var output bytes.Buffer
	if err := Convert(t.Context(), assemblePDF(objects), &output, ""); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ofd.Close() })
	return ofd.Documents[0]
}

// pageAnnotLinks 返回指定页面上带 CLICK 动作的链接注解。
func pageAnnotLinks(t *testing.T, document *parser.Document, pageIndex int) []*models.Annot {
	t.Helper()
	if pageIndex >= len(document.Pages) {
		return nil
	}
	page, err := document.LoadAnnotation(document.Pages[pageIndex].ID)
	if err != nil || page == nil {
		return nil
	}
	var links []*models.Annot
	for _, annot := range page.Annots {
		if annot == nil || annot.Type != "Link" || annot.Appearance == nil {
			continue
		}
		if appearanceHasClickAction(annot.Appearance.Items) {
			links = append(links, annot)
		}
	}
	return links
}

// appearanceHasClickAction 判断注解外观里是否有带 CLICK 动作的图元。
func appearanceHasClickAction(items []models.PageItem) bool {
	for _, item := range items {
		switch item.Kind {
		case models.PageItemBlock:
			if item.Block != nil && appearanceHasClickAction(item.Block.Items) {
				return true
			}
		case models.PageItemPath:
			if item.Path != nil && item.Path.Actions != nil {
				for _, action := range item.Path.Actions.Action {
					if action.Event == models.ActionEventClick {
						return true
					}
				}
			}
		}
	}
	return false
}

// annotClickAction 返回注解外观里第一个 CLICK 动作。
func annotClickAction(annot *models.Annot) models.CtAction {
	for _, item := range annot.Appearance.Items {
		if item.Kind != models.PageItemPath || item.Path == nil || item.Path.Actions == nil {
			continue
		}
		for _, action := range item.Path.Actions.Action {
			if action.Event == models.ActionEventClick {
				return action
			}
		}
	}
	return models.CtAction{}
}

// PDF 的链接注解此前完全不转换：整份 PDF 转 OFD 后所有链接消失。OFD 侧没有等价
// 动作类型（Launch/GoToR/Named）的条目也要被丢掉，而不是留一个点了没反应的热区。
func TestConvertPageLinksToOFD(t *testing.T) {
	content := "q Q"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> " +
			"/Contents 5 0 R /Annots [6 0 R 7 0 R 8 0 R 9 0 R 10 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> /Contents 11 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + content + "\nendstream",
		"<< /Type /Annot /Subtype /Link /Rect [20 70 120 85] /A << /S /URI /URI (https://example.com/a) >> >>",
		"<< /Type /Annot /Subtype /Link /Rect [20 40 120 55] /Dest [4 0 R /XYZ 10 90 1] >>",
		"<< /Type /Annot /Subtype /Link /Rect [20 10 120 25] /A << /S /Launch /F (cmd.exe) >> >>",
		"<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /F 2 /A << /S /URI /URI (https://hidden.example) >> >>",
		"<< /Type /Annot /Subtype /Square /Rect [20 10 120 25] >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + content + "\nendstream",
	}
	document := convertLinkPDF(t, objects)

	links := pageAnnotLinks(t, document, 0)
	if len(links) != 2 {
		t.Fatalf("第 1 页应转换出 2 个链接（Launch 与隐藏注解被丢弃），实际 %d 个", len(links))
	}

	// 动作按注解顺序保留：先是 URI，再是页内跳转。
	first := annotClickAction(links[0])
	if first.URI == nil || first.URI.URI != "https://example.com/a" {
		t.Fatalf("第一个链接动作 = %+v, want URI", first)
	}
	second := annotClickAction(links[1])
	if second.Goto == nil {
		t.Fatalf("第二个链接动作 = %+v, want Goto", second)
	}

	// 热区位置取注解 Appearance 的 Boundary。/Rect [20 70 120 85] 在
	// [0 0 200 100] 页面上换算为 x=20..120pt、y=(100-85)..(100-70)pt。
	boundary := links[0].Appearance.Boundary
	if boundary == nil {
		t.Fatal("链接注解缺少 Appearance Boundary")
	}
	const mmPerPoint = 25.4 / 72
	if math.Abs(boundary.X-20*mmPerPoint) > 0.01 {
		t.Errorf("热区左边界 = %v, want %v", boundary.X, 20*mmPerPoint)
	}
	if math.Abs(boundary.Width-100*mmPerPoint) > 0.01 {
		t.Errorf("热区宽度 = %v, want %v", boundary.Width, 100*mmPerPoint)
	}
	if math.Abs(boundary.Y-15*mmPerPoint) > 0.01 {
		t.Errorf("热区上边界 = %v, want %v", boundary.Y, 15*mmPerPoint)
	}

	// 内部跳转的页号落在本文档范围内。
	if second.Goto.Dest == nil || models.StID(second.Goto.Dest.PageID) != document.Pages[1].ID {
		t.Fatalf("页内跳转目标 = %+v, want 第 2 页", second.Goto.Dest)
	}
	// 第 2 页没有链接，不应生成它的注解文件。
	second2, err := document.LoadAnnotation(document.Pages[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if second2 != nil && len(second2.Annots) != 0 {
		t.Errorf("第 2 页不应有注解，实际 %d 个", len(second2.Annots))
	}
}

func TestTencentPage2WidgetNavigationIsClickable(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "pdf", "腾讯官方ESG报告.pdf")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			t.Skipf("PDF fixture unavailable: %v", err)
		}
		t.Fatal(err)
	}
	ctx, err, panicked := readPDFContext(t.Context(), data, model.NewDefaultConfiguration())
	if panicked || err != nil {
		t.Fatalf("read PDF context: panicked=%v err=%v", panicked, err)
	}
	if err := api.ValidateContext(t.Context(), ctx); err != nil {
		t.Fatal(err)
	}
	pageDict, _, inherited, err := ctx.PageDict(t.Context(), 2, false)
	if err != nil {
		t.Fatal(err)
	}
	info, err := pdfPageInfoFor(ctx, pageDict, inherited)
	if err != nil {
		t.Fatal(err)
	}
	geometries := pdfPageGeometries(t.Context(), ctx)
	links := convertPageLinks(t.Context(), ctx, pageDict, info, geometries, new(uint64))
	if len(links) < 12 {
		t.Fatalf("page 2 clickable links = %d, want navigation widgets to be preserved", len(links))
	}

	const mmPerPoint = 25.4 / 72
	foundUpperLeft, foundLowerRight := false, false
	for _, link := range links {
		if link.Boundary == nil || len(link.Items) == 0 {
			continue
		}
		path, ok := link.Items[0].(creator.Path)
		if !ok || len(path.Actions) == 0 || path.Actions[0].Goto == nil {
			continue
		}
		box := link.Boundary
		if math.Abs(box.X-80*mmPerPoint) < 0.02 && math.Abs(box.Y-(1080-1061.6)*mmPerPoint) < 0.02 {
			foundUpperLeft = true
		}
		if math.Abs(box.X-1758.76*mmPerPoint) < 0.02 && math.Abs(box.Y-(1080-48.3299)*mmPerPoint) < 0.02 {
			foundLowerRight = true
		}
	}
	if !foundUpperLeft || !foundLowerRight {
		t.Fatalf("page 2 clickable navigation areas: upper-left=%t lower-right=%t", foundUpperLeft, foundLowerRight)
	}
}

// 页面带 /Rotate 时矩形要跟着换算，否则热区会落到页外。
func TestConvertPageLinksWithRotation(t *testing.T) {
	content := "q Q"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Rotate 90 /Resources << >> " +
			"/Contents 4 0 R /Annots [5 0 R] >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + content + "\nendstream",
		"<< /Type /Annot /Subtype /Link /Rect [10 20 60 40] /A << /S /URI /URI (https://example.com) >> >>",
	}
	document := convertLinkPDF(t, objects)

	links := pageAnnotLinks(t, document, 0)
	if len(links) != 1 {
		t.Fatalf("应转换出 1 个链接，实际 %d 个", len(links))
	}
	boundary := links[0].Appearance.Boundary
	if boundary == nil {
		t.Fatal("链接注解缺少 Appearance Boundary")
	}
	const mmPerPoint = 25.4 / 72
	// /Rotate 90 下 x 变为页高方向、y 变为页宽方向，且页面宽高互换。
	if math.Abs(boundary.X-20*mmPerPoint) > 0.01 || math.Abs(boundary.Y-10*mmPerPoint) > 0.01 {
		t.Errorf("旋转页面热区位置 = (%v,%v), want (%v,%v)",
			boundary.X, boundary.Y, 20*mmPerPoint, 10*mmPerPoint)
	}
	if math.Abs(boundary.Width-20*mmPerPoint) > 0.01 || math.Abs(boundary.Height-50*mmPerPoint) > 0.01 {
		t.Errorf("旋转页面热区尺寸 = %vx%v, want %vx%v",
			boundary.Width, boundary.Height, 20*mmPerPoint, 50*mmPerPoint)
	}
	// 热区必须落在页面内，页面旋转后是 100x200mm。
	if boundary.X < 0 || boundary.Y < 0 ||
		boundary.X+boundary.Width > 100*mmPerPoint+0.01 ||
		boundary.Y+boundary.Height > 200*mmPerPoint+0.01 {
		t.Errorf("旋转页面热区越界: %+v", *boundary)
	}
}

// 链接矩形完全在页面之外时丢弃；部分越界时裁到版心，避免阅读器在页外生成热区。
func TestConvertPageLinksClipsRectToPage(t *testing.T) {
	content := "q Q"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> " +
			"/Contents 4 0 R /Annots [5 0 R 6 0 R] >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + content + "\nendstream",
		"<< /Type /Annot /Subtype /Link /Rect [-50 -50 -10 -10] /A << /S /URI /URI (https://outside.example) >> >>",
		"<< /Type /Annot /Subtype /Link /Rect [190 90 400 200] /A << /S /URI /URI (https://edge.example) >> >>",
	}
	document := convertLinkPDF(t, objects)

	links := pageAnnotLinks(t, document, 0)
	if len(links) != 1 {
		t.Fatalf("完全越界的链接应被丢弃，实际 %d 个", len(links))
	}
	boundary := links[0].Appearance.Boundary
	const mmPerPoint = 25.4 / 72
	const pageWidth = 200 * mmPerPoint
	const pageHeight = 100 * mmPerPoint
	if boundary.X < 0 || boundary.Y < 0 ||
		boundary.X+boundary.Width > pageWidth+0.01 ||
		boundary.Y+boundary.Height > pageHeight+0.01 {
		t.Errorf("裁剪后热区应完全落在页面内: %+v", *boundary)
	}
	// /Rect [190 90 400 200] 只在页面内留下右下角一小块：x 贴右边缘，
	// y 从页底往上 10pt。
	if math.Abs(boundary.X+boundary.Width-pageWidth) > 0.01 {
		t.Errorf("热区右边界应贴到页面右边缘: %+v", *boundary)
	}
	if math.Abs(boundary.Y+boundary.Height-10*mmPerPoint) > 0.01 {
		t.Errorf("热区下边界应在页底上方 10pt: %+v", *boundary)
	}
}

// 端点顺序颠倒的 /Rect 也要得到正宽高，不能产生负尺寸热区。
func TestConvertPageLinksNormalizesReversedRect(t *testing.T) {
	content := "q Q"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> " +
			"/Contents 4 0 R /Annots [5 0 R] >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + content + "\nendstream",
		"<< /Type /Annot /Subtype /Link /Rect [120 70 20 85] /A << /S /URI /URI (https://example.com) >> >>",
	}
	document := convertLinkPDF(t, objects)

	links := pageAnnotLinks(t, document, 0)
	if len(links) != 1 {
		t.Fatalf("应转换出 1 个链接，实际 %d 个", len(links))
	}
	boundary := links[0].Appearance.Boundary
	if boundary == nil || boundary.Width <= 0 || boundary.Height <= 0 {
		t.Fatalf("颠倒的 /Rect 应归一化为正宽高: %+v", boundary)
	}
}
