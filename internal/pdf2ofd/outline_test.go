package pdf2ofd

import (
	"bytes"
	"math"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

func TestConvertOutlinesToOFD(t *testing.T) {
	// PDF 目录的 /Outlines 必须转换为 OFD 大纲：标题、层级、跳转目标（含 XYZ/
	// FitH/FitR 坐标换算）和 URI 动作都要保留，否则书签会全部丢失。
	content := "q Q"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R /Outlines 6 0 R /PageMode /UseOutlines >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> /Contents 5 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << >> /Contents 5 0 R >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + content + "\nendstream",
		"<< /Type /Outlines /First 7 0 R /Last 9 0 R /Count 4 >>",
		"<< /Title (第一章) /Parent 6 0 R /Dest [3 0 R /XYZ 0 100 1] /Next 8 0 R >>",
		"<< /Title (链接) /Parent 6 0 R /A << /S /URI /URI (https://example.com) >> /Next 9 0 R >>",
		"<< /Title (第二章) /Parent 6 0 R /First 10 0 R /Last 10 0 R /Count 1 /Dest [4 0 R /FitH 50] >>",
		"<< /Title (2.1 节) /Parent 9 0 R /Dest [4 0 R /FitR 10 20 100 80] >>",
	}
	var output bytes.Buffer
	if err := Convert(assemblePDF(objects), &output); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	document := ofd.Documents[0]
	if document.Outlines == nil {
		t.Fatal("OFD 大纲缺失")
	}
	items := document.Outlines.OutlineElems
	if len(items) != 3 {
		t.Fatalf("顶层大纲项 = %d, want 3", len(items))
	}
	if items[0].Title != "第一章" || items[1].Title != "链接" || items[2].Title != "第二章" {
		t.Fatalf("大纲标题 = %q/%q/%q", items[0].Title, items[1].Title, items[2].Title)
	}

	// [0 0 200 100] 页面上 /XYZ 0 100 → 左上角，Zoom=1。
	xyz := outlineGoto(t, items[0])
	if xyz.Type != models.DestTypeXYZ {
		t.Fatalf("第一项目标类型 = %q, want XYZ", xyz.Type)
	}
	if xyz.Left == nil || math.Abs(*xyz.Left) > 0.01 || xyz.Top == nil || math.Abs(*xyz.Top) > 0.01 {
		t.Fatalf("XYZ 目标位置 = (%v,%v), want (0,0)", xyz.Left, xyz.Top)
	}
	if xyz.Zoom == nil || *xyz.Zoom != 1 {
		t.Fatalf("XYZ 目标缩放 = %v, want 1", xyz.Zoom)
	}

	// URI 动作保留为 OFD URI 动作。
	uriAction := items[1].Actions
	if uriAction == nil || len(uriAction.Actions) != 1 || uriAction.Actions[0].URI == nil {
		t.Fatalf("第二项 URI 动作缺失: %+v", uriAction)
	}
	if uriAction.Actions[0].URI.URI != "https://example.com" {
		t.Fatalf("URI = %q", uriAction.Actions[0].URI.URI)
	}

	// 第二章有 1 个子项且展开。
	if items[2].Count == nil || *items[2].Count != 1 || items[2].Expanded == nil || !*items[2].Expanded {
		t.Fatalf("第二章 Count/Expanded = %v/%v", items[2].Count, items[2].Expanded)
	}
	// /FitH 50 → Top = (100-50)pt = 17.64mm。
	fitH := outlineGoto(t, items[2])
	if fitH.Type != models.DestTypeFitH {
		t.Fatalf("第二章目标类型 = %q, want FitH", fitH.Type)
	}
	if fitH.Top == nil || math.Abs(*fitH.Top-17.6389) > 0.01 {
		t.Fatalf("FitH Top = %v, want ~17.64", fitH.Top)
	}

	if len(items[2].OutlineElem) != 1 {
		t.Fatalf("第二章子项 = %d, want 1", len(items[2].OutlineElem))
	}
	// /FitR 10 20 100 80 → Left/Right 为 x，Top/Bottom 为换算后的 y。
	fitR := outlineGoto(t, items[2].OutlineElem[0])
	if fitR.Type != models.DestTypeFitR {
		t.Fatalf("子项目标类型 = %q, want FitR", fitR.Type)
	}
	if fitR.Left == nil || fitR.Right == nil || fitR.Top == nil || fitR.Bottom == nil {
		t.Fatalf("FitR 目标缺少边界: %+v", fitR)
	}
	if math.Abs(*fitR.Left-3.5278) > 0.01 || math.Abs(*fitR.Right-35.2778) > 0.01 {
		t.Fatalf("FitR Left/Right = %v/%v", *fitR.Left, *fitR.Right)
	}
	if math.Abs(*fitR.Top-7.0556) > 0.01 || math.Abs(*fitR.Bottom-28.2222) > 0.01 {
		t.Fatalf("FitR Top/Bottom = %v/%v", *fitR.Top, *fitR.Bottom)
	}

	if document.VPreferences == nil || document.VPreferences.PageMode == nil || *document.VPreferences.PageMode != models.PageModeUseOutlines {
		t.Fatalf("显示偏好未设置 UseOutlines: %+v", document.VPreferences)
	}
}

func TestConvertOutlineNamedDestinationAndRotation(t *testing.T) {
	// 命名目标（目录 /Dests 字典）和 90° 旋转页面也要正确解析：FitH 在旋转后
	// 应落到纵向轴（FitV）。
	content := "q Q"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R /Outlines 4 0 R /Dests 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 200] /Rotate 90 /Resources << >> /Contents 6 0 R >>",
		"<< /Type /Outlines /First 7 0 R /Last 7 0 R /Count 1 >>",
		"<< /chapter [3 0 R /FitH 100] >>",
		"<< /Length " + itoa(len(content)) + " >>\nstream\n" + content + "\nendstream",
		"<< /Title (命名目标) /Parent 4 0 R /Dest (chapter) >>",
	}
	var output bytes.Buffer
	if err := Convert(assemblePDF(objects), &output); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	document := ofd.Documents[0]
	if document.Outlines == nil || len(document.Outlines.OutlineElems) != 1 {
		t.Fatalf("大纲缺失: %+v", document.Outlines)
	}
	dest := outlineGoto(t, document.Outlines.OutlineElems[0])
	// 页面旋转 90°，MediaBox [0 0 100 200]，FitH 100 落在旋转后的纵向轴：
	// Left = 100pt = 35.28mm。
	if dest.Type != models.DestTypeFitV {
		t.Fatalf("旋转页面目标类型 = %q, want FitV", dest.Type)
	}
	if dest.Left == nil || math.Abs(*dest.Left-35.2778) > 0.01 {
		t.Fatalf("旋转页面 FitV Left = %v, want ~35.28", dest.Left)
	}
}

func outlineGoto(t *testing.T, item models.CTOutlineElem) *models.CtDest {
	t.Helper()
	if item.Actions == nil || len(item.Actions.Actions) == 0 {
		t.Fatalf("大纲项 %q 缺少动作", item.Title)
	}
	action := item.Actions.Actions[0]
	if action.Goto == nil || action.Goto.Dest == nil {
		t.Fatalf("大纲项 %q 缺少跳转目标", item.Title)
	}
	return action.Goto.Dest
}
