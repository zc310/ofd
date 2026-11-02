package render

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/renderstate"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

func TestUnavailableFontFaceAndUnknownSealHaveDiagnostics(t *testing.T) {
	doc := &Document{fonts: &metricsFontEngine{}}
	t.Cleanup(doc.Close)
	ctx := newCanvasBackend(canvas.NewContext(canvas.New(20, 20)))
	var budget renderBudget
	budget.reset()
	budget.Report = &renderstate.ReportBuilder{}
	budget.ObjectID = 47
	object := models.TextObject{CtText: models.CtText{CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 10, Height: 10}}, Font: 7, Size: 3}}
	doc.textWithBudget(ctx, object, nil, models.StBox{Width: 20, Height: 20}, nil, nil, &budget)
	if len(budget.Report.Diagnostics) != 1 || budget.Report.Diagnostics[0].Code != "font_face_unavailable" || budget.Report.Diagnostics[0].ResourceID != 7 || budget.Report.Diagnostics[0].ObjectID != 47 {
		t.Fatalf("字体面失败未记录对象和字体 ID：%+v", budget.Report.Diagnostics)
	}
	info := &parser.SealInfo{SealData: &parser.SealData{FileType: "unknown"}, StampAnnot: &models.StampAnnot{}}
	if err := doc.sealWithBudget(ctx, info, models.StBox{Width: 20, Height: 20}, &budget); err != nil {
		t.Fatal(err)
	}
	if len(budget.Report.Diagnostics) != 2 || budget.Report.Diagnostics[1].Code != "seal_type_unsupported" {
		t.Fatal("未知印章类型被静默忽略")
	}
}

func TestEmptyOFDSealIsReportedInsteadOfSilentlySkipped(t *testing.T) {
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for _, entry := range []struct{ name, xml string }{
		{"OFD.xml", `<OFD><DocBody><DocRoot>Doc.xml</DocRoot></DocBody></OFD>`},
		{"Doc.xml", `<Document><Pages/></Document>`},
	} {
		file, err := w.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(entry.xml)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	doc := &Document{}
	t.Cleanup(doc.Close)
	var budget renderBudget
	budget.reset()
	budget.Report = &renderstate.ReportBuilder{}
	info := &parser.SealInfo{SealData: &parser.SealData{FileType: "ofd", Data: data.Bytes()}, StampAnnot: &models.StampAnnot{}}
	if err := doc.sealWithBudget(nil, info, models.StBox{}, &budget); err != nil {
		t.Fatal(err)
	}
	if len(budget.Report.Diagnostics) != 1 || budget.Report.Diagnostics[0].Code != "seal_document_empty" {
		t.Fatalf("空印章未报告：%+v", budget.Report.Diagnostics)
	}
}
