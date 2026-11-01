package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"reflect"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"golang.org/x/image/font/gofont/goregular"
)

// 独立 XML 避免创建器的默认值掩盖待验证的标准语义。
func standardSemanticsDocument(t *testing.T, page, template, common string) (*Document, *parser.Page) {
	t.Helper()
	entries := map[string][]byte{
		"OFD.xml":             []byte(`<OFD><DocBody><DocRoot>Doc/Document.xml</DocRoot></DocBody></OFD>`),
		"Doc/Document.xml":    []byte(`<Document><CommonData><MaxUnitID>100</MaxUnitID><PageArea><PhysicalBox>0 0 20 20</PhysicalBox></PageArea><DocumentRes>Res.xml</DocumentRes><TemplatePage ID="10" BaseLoc="Template.xml"/>` + common + `</CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages><Annotations>Annotations.xml</Annotations></Document>`),
		"Doc/Page.xml":        []byte(`<Page>` + page + `</Page>`),
		"Doc/Template.xml":    []byte(`<Page>` + template + `</Page>`),
		"Doc/Res.xml":         []byte(`<Res BaseLoc="."><ColorSpaces><ColorSpace ID="7" Type="CMYK"/><ColorSpace ID="8" Type="RGB"/></ColorSpaces><Fonts><Font ID="9" FontName="Test"><FontFile>font.ttf</FontFile></Font></Fonts></Res>`),
		"Doc/font.ttf":        goregular.TTF,
		"Doc/Annotations.xml": []byte(`<Annotations><Page PageID="1"><FileLoc>Annot.xml</FileLoc></Page></Annotations>`),
		"Doc/Annot.xml":       []byte(`<PageAnnot><Annot ID="40" Type="Stamp" Creator="test" LastModDate="2026-01-01"><Appearance Boundary="0 0 4 4"><PathObject ID="41" Boundary="0 0 4 4" Fill="true" Stroke="false"><FillColor ColorSpace="8" Value="255 0 0"/><AbbreviatedData>M 0 0 L 4 0 L 4 4 L 0 4 C</AbbreviatedData></PathObject></Appearance></Annot></PageAnnot>`),
	}
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	for name, content := range entries {
		w, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	doc := NewDocument(color.White, ofd.Documents[0])
	t.Cleanup(func() { doc.Close(); _ = ofd.Close() })
	return doc, ofd.Documents[0].Pages[0]
}

func standardRect(id int, ink string) string {
	return fmt.Sprintf(`<PathObject ID="%d" Boundary="0 0 20 20" Fill="true" Stroke="false"><FillColor Value="%s"/><AbbreviatedData>M 0 0 L 20 0 L 20 20 L 0 20 C</AbbreviatedData></PathObject>`, id, ink)
}

func standardLayer(id int, kind, objects string) string {
	return fmt.Sprintf(`<Layer ID="%d" Type="%s">%s</Layer>`, id, kind, objects)
}

func checkStandardSurface(t *testing.T, doc *Document, page *parser.Page, check func(*testing.T, image.Image)) {
	t.Helper()
	surface, err := doc.Page(page)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("PNG", func(t *testing.T) { check(t, surface.Rasterize(geom.DPMM(10), nil)) })
	t.Run("PDF", func(t *testing.T) { check(t, clipPDFImage(t, surface, 254)) })
}

func standardPixel(t *testing.T, img image.Image, x, y int) color.NRGBA {
	t.Helper()
	value, ok := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	if !ok {
		t.Fatal("颜色模型没有返回 NRGBA")
	}
	return value
}

func TestDefaultColorSpaceAppliesWithoutOverridingExplicitColorSpace(t *testing.T) {
	// §7.5 表6：缺省 ColorSpace 时必须使用 DefaultCS，而非把 K 当作 Alpha。
	doc, page := standardSemanticsDocument(t, `<Content>`+standardLayer(2, "Body", standardRect(3, "0 255 255 0"))+`</Content>`, "", `<DefaultCS>7</DefaultCS>`)
	checkStandardSurface(t, doc, page, func(t *testing.T, img image.Image) {
		t.Helper()
		got := standardPixel(t, img, 100, 100)
		if got.R < 200 || got.G > 80 || got.B > 80 {
			t.Fatalf("默认 CMYK 红色=%v", got)
		}
	})
	source := &models.CTColor{ColorSpace: 8, Value: &models.Color{RGBA: color.RGBA{G: 255, A: 255}}}
	got := doc.updateCtColor(source)
	if got == nil || got.Value != (color.RGBA{G: 255, A: 255}) || source.ColorSpace != 8 {
		t.Fatalf("显式 RGB 被覆盖：%+v", got)
	}
}

func TestApplicationBoxClipsPageTemplatesAndAnnotations(t *testing.T) {
	for _, box := range []string{"5 5 10 10", "30 30 10 10", "-5 -5 15 15"} {
		t.Run(box, func(t *testing.T) {
			doc, page := standardSemanticsDocument(t, `<Template TemplateID="10"/><Area><PhysicalBox>0 0 20 20</PhysicalBox><ApplicationBox>`+box+`</ApplicationBox></Area><Content>`+standardLayer(2, "Body", standardRect(3, "255 0 0"))+`</Content>`, `<Content>`+standardLayer(4, "Body", standardRect(5, "0 0 255"))+`</Content>`, "")
			checkStandardSurface(t, doc, page, func(t *testing.T, img image.Image) {
				t.Helper()
				for _, point := range []struct {
					x, y int
					red  bool
				}{
					{10, 10, box == "-5 -5 15 15"}, {70, 70, box != "30 30 10 10"}, {180, 180, false},
				} {
					got := standardPixel(t, img, point.x, point.y)
					want := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
					if point.red {
						want = color.NRGBA{R: 255, A: 255}
					}
					if got != want {
						t.Errorf("(%d,%d)=%v，期望%v", point.x, point.y, got, want)
					}
				}
			})
		})
	}
}

func TestForegroundLayersRenderAboveBodyAndForegroundTemplates(t *testing.T) {
	red, blue := standardRect(3, "255 0 0"), standardRect(5, "0 0 255")
	for _, tc := range []struct{ name, page, template string }{
		{"前景层先于正文", `<Content>` + standardLayer(2, "Foreground", red) + standardLayer(4, "Body", blue) + `</Content>`, ""},
		{"前景模板低于前景层", `<Template TemplateID="10" ZOrder="Foreground"/><Content>` + standardLayer(2, "Foreground", red) + `</Content>`, `<Content>` + standardLayer(4, "Body", blue) + `</Content>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, page := standardSemanticsDocument(t, tc.page, tc.template, "")
			checkStandardSurface(t, doc, page, func(t *testing.T, img image.Image) {
				t.Helper()
				if got := standardPixel(t, img, 100, 100); got != (color.NRGBA{R: 255, A: 255}) {
					t.Fatalf("前景中心=%v", got)
				}
			})
		})
	}
}

func TestApplicationBoxAndForegroundOrderApplyToTextAndPDFText(t *testing.T) {
	text := func(id int, box, value string) string {
		return fmt.Sprintf(`<TextObject ID="%d" Boundary="%s" Font="9" Size="2"><TextCode X="0" Y="2">%s</TextCode></TextObject>`, id, box, value)
	}
	pageXML := `<Template TemplateID="10" ZOrder="Foreground"/><Area><PhysicalBox>0 0 20 20</PhysicalBox><ApplicationBox>5 5 10 10</ApplicationBox></Area><Content>` +
		standardLayer(2, "Foreground", text(3, "5 5 10 4", "FRONT")) +
		standardLayer(4, "Body", text(5, "5 9 10 4", "BODY")+text(6, "0 0 4 4", "OUTSIDE")) + `</Content>`
	doc, page := standardSemanticsDocument(t, pageXML, `<Content>`+standardLayer(7, "Body", text(8, "5 5 10 4", "TEMPLATE"))+`</Content>`, "")
	if got := doc.TextValues(page); !reflect.DeepEqual(got, []string{"BODY", "TEMPLATE", "FRONT"}) {
		t.Fatalf("文字来源顺序或显示区过滤=%v", got)
	}
	layouts := doc.TextLayouts(page)
	if len(layouts) != 3 || layouts[0].Text != "BODY" || layouts[2].Text != "FRONT" {
		t.Fatalf("文字布局与提取顺序不一致：%+v", layouts)
	}
	surface, err := doc.Page(page)
	if err != nil {
		t.Fatal(err)
	}
	textPDF := extractPDFText(t, surface)
	if strings.Contains(textPDF, "OUTSIDE") || !strings.Contains(textPDF, "BODY") || !strings.Contains(textPDF, "FRONT") {
		t.Fatalf("PDF 文字层未应用显示区域：%q", textPDF)
	}
}

func TestPageLinkRegionUsesRelativeURIAndApplicationClip(t *testing.T) {
	pageXML := `<Area><PhysicalBox>0 0 20 20</PhysicalBox><ApplicationBox>5 5 10 10</ApplicationBox></Area><Content><Layer ID="2"><PathObject ID="3" Boundary="0 0 20 20"><Actions><Action Event="CLICK"><Region><Area Start="4 4"><Line Point1="10 4"/><Line Point1="4 10"/><Close/></Area></Region><URI URI="guide" Base="https://example.test/"/></Action></Actions><AbbreviatedData>M 0 0 L 20 20</AbbreviatedData></PathObject></Layer></Content>`
	doc, page := standardSemanticsDocument(t, pageXML, "", "")
	links, err := doc.PageLinksWithVisibilityE(page, nil, PageVisibility{})
	if err != nil || len(links) != 1 {
		t.Fatalf("PDF 链接=%+v %v", links, err)
	}
	link := links[0]
	if link.URI != "https://example.test/guide" || link.X != 5 || link.Y != 5 || link.Width != 4 || link.Height != 4 || link.Region == "" {
		t.Fatalf("链接没有应用 Region、Base 与显示区：%+v", link)
	}
}
