package spec_test

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestPageAnnotDecodePreservesOptionalFlagsAndOrderedAppearance(t *testing.T) {
	data := `<PageAnnot xmlns="http://www.ofdspec.org/2016"><Annot ID="9" Type="Link" Creator="测试" LastModDate="2026-01-02" Visible="false" Print="false" ReadOnly="false"><Remark></Remark><Parameters><Parameter Name="target">页2</Parameter></Parameters><Appearance Boundary="1 2 30 40"><PathObject ID="10" Boundary="0 0 1 1"><AbbreviatedData>M 0 0 L 1 1</AbbreviatedData></PathObject><PageBlock ID="11"><TextObject ID="12" Boundary="0 0 10 10" Font="8" Size="3"><TextCode>甲\0020乙</TextCode></TextObject></PageBlock></Appearance></Annot><Annot ID="13" Type="Stamp" Creator="测试" LastModDate="2026-01-03"><Appearance/></Annot></PageAnnot>`
	var page spec.PageAnnot
	if err := xml.Unmarshal([]byte(data), &page); err != nil {
		t.Fatal(err)
	}
	if page.XMLName.Space != spec.Namespace || len(page.Annots) != 2 {
		t.Fatalf("注释命名空间或顺序丢失: %+v", page)
	}
	annot := page.Annots[0]
	if annot.ID != "9" || annot.Type != spec.AnnotTypeLink || annot.LastModDate.Format("2006-01-02") != "2026-01-02" {
		t.Fatalf("注释声明改变: %+v", annot)
	}
	for _, flag := range []spec.OptionalBool{annot.Visible, annot.Print, annot.ReadOnly} {
		if !flag.IsSet() || flag.Value(true) {
			t.Fatal("显式 false 被省略或改成默认值")
		}
	}
	if annot.Remark == nil || *annot.Remark != "" || annot.Parameters.Parameters[0].Value != "页2" {
		t.Fatal("空备注或参数内容丢失")
	}
	appearance := annot.Appearance
	if appearance.Boundary == nil || *appearance.Boundary != (spec.StBox{X: 1, Y: 2, Width: 30, Height: 40}) || len(appearance.Items) != 2 {
		t.Fatalf("注释外观边界或对象丢失: %+v", appearance)
	}
	if appearance.Items[0].Kind != spec.PageItemPath || appearance.Items[1].Kind != spec.PageItemBlock || appearance.Items[1].Block.Items[0].Text.TextCode[0].Value != "甲 乙" {
		t.Fatal("外观未复用有序页面块和文字解码")
	}
	for _, flag := range []spec.OptionalBool{page.Annots[1].Visible, page.Annots[1].Print, page.Annots[1].ReadOnly} {
		if flag.IsSet() || !flag.Value(true) {
			t.Fatal("缺省标志被提前填入")
		}
	}
	if page.Annots[1].Appearance.Boundary != nil || page.Annots[1].Remark != nil {
		t.Fatal("缺省边界或备注被提前填入")
	}
}

func TestAnnotTypeDecodeAcceptsOnlyDeclaredTypes(t *testing.T) {
	for _, kind := range []spec.AnnotType{spec.AnnotTypeLink, spec.AnnotTypePath, spec.AnnotTypeHighlight, spec.AnnotTypeStamp, spec.AnnotTypeWatermark} {
		var annot spec.Annot
		if err := xml.Unmarshal(fmt.Appendf(nil, `<Annot Type="%s"/>`, kind), &annot); err != nil || annot.Type != kind {
			t.Fatalf("无法解析注释类型 %q: %v", kind, err)
		}
	}
	var annot spec.Annot
	if err := xml.Unmarshal([]byte(`<Annot Type="unknown"/>`), &annot); err == nil {
		t.Fatal("接受了未知注释类型")
	}
}

func TestAnnotationAppearanceUsesSharedPageBlockDepthLimit(t *testing.T) {
	data := `<PageAnnot><Annot Type="Path"><Appearance>` + strings.Repeat(`<PageBlock>`, spec.MaxPageBlockDepth+1) + strings.Repeat(`</PageBlock>`, spec.MaxPageBlockDepth+1) + `</Appearance></Annot></PageAnnot>`
	var page spec.PageAnnot
	if err := xml.Unmarshal([]byte(data), &page); !errors.Is(err, spec.ErrPageBlockDepth) {
		t.Fatalf("注释外观未遵守共享嵌套上限: %v", err)
	}
}
