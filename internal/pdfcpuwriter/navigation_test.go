package pdfcpuwriter

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/render/drawing"
)

func readWrittenPDF(t *testing.T, doc *Document) *model.Context {
	t.Helper()
	var output bytes.Buffer
	if err := doc.Write(&output); err != nil {
		t.Fatal(err)
	}
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationStrict
	ctx, err := api.ReadContext(t.Context(), bytes.NewReader(output.Bytes()), conf)
	if err != nil {
		t.Fatal(err)
	}
	if err := api.ValidateContext(t.Context(), ctx); err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestNavigationCopiesTargetsAndResolvesForwardPagesWithActualHeight(t *testing.T) {
	doc, err := New(t.Context(), 40, 30)
	if err != nil {
		t.Fatal(err)
	}
	target := drawing.LinkTarget{Page: 1, Type: drawing.DestXYZ, Left: 3, Top: 4, Zoom: 2, PageHeight: 999}
	if err := doc.AddLinks([]drawing.PageLink{{Target: &target, X: 1, Y: 2, Width: 3, Height: 4}}); err != nil {
		t.Fatal(err)
	}
	if err := doc.AddOutlines([]drawing.PageOutline{
		{Title: "根😀", Target: &target},
		{Title: "子", Level: 1, URI: "https://example.com/(a)"},
		{Title: "孙", Level: 2, Target: &target},
		{Title: "同级", URI: "https://example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	target.Page = 100 // 登记后修改调用方数据，不得改变已登记目标。
	if err := doc.NextPage(60, 50); err != nil {
		t.Fatal(err)
	}
	ctx := readWrittenPDF(t, doc)
	page, _, _, err := ctx.PageDict(t.Context(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	annot, err := ctx.DereferenceDict(page.ArrayEntry("Annots")[0])
	if err != nil {
		t.Fatal(err)
	}
	dest := annot.ArrayEntry("Dest")
	if len(dest) != 5 || dest[1] != types.Name("XYZ") {
		t.Fatalf("未保留显式目标：%v", dest)
	}
	if got := pdfNumber(dest[3]); math.Abs(got-46*prototypePointsPerMM) > 1e-6 || pdfNumber(dest[4]) != 2 {
		t.Fatalf("页高或无量纲倍率错误：%v", dest)
	}
	pageRef, ok := dest[0].(types.IndirectRef)
	if !ok {
		t.Fatalf("目的地不是间接页引用：%v", dest[0])
	}
	pageNumber, err := ctx.PageNumber(t.Context(), int(pageRef.ObjectNumber))
	if err != nil || pageNumber != 2 {
		t.Fatalf("向前跳页序号错误：%d，%v", pageNumber, err)
	}
	root, err := ctx.DereferenceDict(ctx.RootDict["Outlines"])
	if err != nil {
		t.Fatal(err)
	}
	first, err := ctx.DereferenceDict(root["First"])
	if err != nil {
		t.Fatal(err)
	}
	if first["Count"] != types.Integer(2) || root["Count"] != types.Integer(4) {
		t.Fatalf("大纲后代数错误：%v / %v", first["Count"], root["Count"])
	}
	title, err := ctx.DereferenceText(first["Title"])
	if err != nil || title != "根😀" {
		t.Fatalf("大纲 Unicode 标题错误：%q，%v", title, err)
	}
	if err := doc.AddLinks(nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("写出后仍能添加链接：%v", err)
	}
	if err := doc.AddOutlines(nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("写出后仍能添加大纲：%v", err)
	}
}

func pdfNumber(object types.Object) float64 {
	switch value := object.(type) {
	case types.Integer:
		return float64(value)
	case types.Float:
		return float64(value)
	}
	return math.NaN()
}

func TestInvalidNavigationDoesNotWritePartialPDF(t *testing.T) {
	for _, target := range []drawing.LinkTarget{
		{Page: -1}, {Page: 1}, {Type: 999}, {Type: drawing.DestXYZ, Zoom: -1}, {Left: math.NaN()},
	} {
		doc, err := New(t.Context(), 40, 30)
		if err != nil {
			t.Fatal(err)
		}
		if err := doc.AddLinks([]drawing.PageLink{{Target: &target}}); err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := doc.Write(&output); !errors.Is(err, errInvalidDestination) || output.Len() != 0 {
			t.Fatalf("无效目标未在输出前拒绝：%+v，%v", target, err)
		}
	}
}

func TestNavigationCancellationPreservesContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	doc, err := New(ctx, 40, 30)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := doc.AddLinks(nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := doc.AddOutlines(nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
