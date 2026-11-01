package parser

import (
	"errors"
	"os"
	"testing"

	"github.com/zc310/ofd/internal/models"
)

func TestPageAreaInheritsTemplateBeforeDocumentWithoutLoadingContent(t *testing.T) {
	for _, tc := range []struct {
		name, pageArea, templateArea string
		width, height                float64
	}{
		{"模板区域", "", `<Area><PhysicalBox>0 0 40 30</PhysicalBox></Area>`, 40, 30},
		{"页面优先", `<Area><PhysicalBox>0 0 10 15</PhysicalBox></Area>`, `<Area><PhysicalBox>0 0 40 30</PhysicalBox></Area>`, 10, 15},
		{"文档回退", "", "", 20, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := parserTestArchive(t, map[string]string{
				"OFD.xml":          `<OFD><DocBody><DocRoot>Doc/Document.xml</DocRoot></DocBody></OFD>`,
				"Doc/Document.xml": `<Document><CommonData><PageArea><PhysicalBox>0 0 20 20</PhysicalBox></PageArea><TemplatePage ID="10" BaseLoc="Template.xml"/></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`,
				"Doc/Page.xml":     `<Page><Template TemplateID="10"/>` + tc.pageArea + `<Content><Layer ID="2"/></Content></Page>`,
				"Doc/Template.xml": `<Page>` + tc.templateArea + `<Content><Layer ID="3"/></Content></Page>`,
			})
			ofd, err := NewOFD(data)
			if err != nil {
				t.Fatal(err)
			}
			defer ofd.Close()
			page := ofd.Documents[0].Pages[0]
			want := models.StBox{Width: tc.width, Height: tc.height}
			for range 2 {
				got, err := page.EffectivePhysicalBoxMetadata()
				if err != nil || got != want {
					t.Fatalf("有效元数据=%v %v，期望%v", got, err, want)
				}
			}
			if page.loaded {
				t.Fatal("尺寸查询加载了页面正文")
			}
			got, err := page.PhysicalBox()
			if err != nil || got != want {
				t.Fatalf("完整页面=%v %v，期望%v", got, err, want)
			}
			if ofd.Documents[0].CommonData.PageArea.PhysicalBox.Width != 20 {
				t.Fatal("污染了文档默认区域")
			}
		})
	}
}

func TestInheritedPageAreaPreservesTemplateReadError(t *testing.T) {
	data := parserTestArchive(t, map[string]string{
		"OFD.xml":      `<OFD><DocBody><DocRoot>Document.xml</DocRoot></DocBody></OFD>`,
		"Document.xml": `<Document><CommonData><PageArea><PhysicalBox>0 0 20 20</PhysicalBox></PageArea><TemplatePage ID="10" BaseLoc="missing.xml"/></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`,
		"Page.xml":     `<Page><Template TemplateID="10"/></Page>`,
	})
	ofd, err := NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	page := ofd.Documents[0].Pages[0]
	if _, err := page.EffectivePhysicalBoxMetadata(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("元数据读取丢失错误：%v", err)
	}
	if _, err := page.PhysicalBox(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("页面读取丢失错误：%v", err)
	}
}

func TestPhysicalBoxOrUsesDocumentFallbackAndThenA4(t *testing.T) {
	fallback := models.CtPageArea{PhysicalBox: models.StBox{Width: 180, Height: 260}}
	for _, tc := range []struct {
		name    string
		content models.PageContent
		want    models.StBox
	}{
		{"有效页面区域优先", models.PageContent{Area: &models.CtPageArea{PhysicalBox: models.StBox{Width: 100, Height: 200}}}, models.StBox{Width: 100, Height: 200}},
		{"非法页面区域回退文档", models.PageContent{Area: &models.CtPageArea{PhysicalBox: models.StBox{Width: -1, Height: 20}}}, fallback.PhysicalBox},
		{"空页面区域回退文档", models.PageContent{}, fallback.PhysicalBox},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := &Page{effectiveMetadata: func() (models.StBox, error) {
				if tc.content.Area == nil {
					return models.StBox{}, nil
				}
				return tc.content.Area.PhysicalBox, nil
			}}
			if got := page.PhysicalBoxOr(fallback); got != tc.want {
				t.Fatalf("有效页面尺寸=%+v，期望 %+v", got, tc.want)
			}
		})
	}
	if got := NewPage(models.PageContent{}).PhysicalBoxOr(models.CtPageArea{}); got != (models.StBox{Width: 210, Height: 297}) {
		t.Fatalf("A4 回退=%+v", got)
	}
}
