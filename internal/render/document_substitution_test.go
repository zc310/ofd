package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/spec"
	"golang.org/x/image/font/gofont/goregular"
)

// 独立 XML 覆盖 §10 表43、§13 表50；不同颜色用来辨别实际选中的图像。
func substitutionDocument(t *testing.T, object, composite string, dpi float64, broken bool) (*Document, *parser.Page) {
	t.Helper()
	files := map[string][]byte{
		"OFD.xml":      []byte(`<OFD><DocBody><DocRoot>Document.xml</DocRoot></DocBody></OFD>`),
		"Document.xml": []byte(`<Document><CommonData><PageArea><PhysicalBox>0 0 25.4 25.4</PhysicalBox></PageArea><DocumentRes>Res.xml</DocumentRes></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`),
		"Page.xml":     []byte(`<Page><Content><Layer ID="2">` + object + `</Layer></Content></Page>`),
		"Res.xml":      []byte(`<Res><Fonts><Font ID="9" FontName="Go"><FontFile>font.ttf</FontFile></Font></Fonts><MultiMedias><MultiMedia ID="10" Type="Image" Format="PNG"><MediaFile>low.png</MediaFile></MultiMedia><MultiMedia ID="11" Type="Image" Format="PNG"><MediaFile>high.png</MediaFile></MultiMedia></MultiMedias><CompositeGraphicUnits>` + composite + `</CompositeGraphicUnits></Res>`),
		"font.ttf":     goregular.TTF,
	}
	for index, name := range []string{"low.png", "high.png"} {
		size := []int{100, 600}[index]
		img := image.NewNRGBA(image.Rect(0, 0, size, size))
		ink := []color.NRGBA{{R: 255, A: 255}, {B: 255, A: 255}}[index]
		for y := range size {
			for x := range size {
				img.SetNRGBA(x, y, ink)
			}
		}
		var data bytes.Buffer
		if err := png.Encode(&data, img); err != nil {
			t.Fatal(err)
		}
		files[name] = data.Bytes()
	}
	if broken {
		files["high.png"] = []byte("损坏的替代图")
	}
	var archive bytes.Buffer
	w := zip.NewWriter(&archive)
	for name, data := range files {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(archive.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	doc := NewDocument(color.White, ofd.Documents[0], geom.DPI(dpi))
	t.Cleanup(func() { doc.Close(); _ = ofd.Close() })
	return doc, ofd.Documents[0].Pages[0]
}

func TestImageSubstitutionFollowsOutputPixelsAndDoesNotDegradePrimary(t *testing.T) {
	for _, tc := range []struct {
		name          string
		dpi           float64
		resource, sub int
		broken        bool
		blue          bool
	}{
		{"低分辨率保留主图", 72, 10, 11, false, false},
		{"高分辨率选择替代图", 300, 10, 11, false, true},
		{"损坏替代图回退主图", 300, 10, 11, true, false},
		{"禁止降低主图质量", 1200, 11, 10, false, true},
		{"缺失主图使用替代图", 72, 99, 11, false, true},
		{"损坏主图使用替代图", 72, 11, 10, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := fmt.Sprintf(`<ImageObject ID="3" ResourceID="%d" Substitution="%d" Boundary="0 0 25.4 25.4"/>`, tc.resource, tc.sub)
			doc, page := substitutionDocument(t, object, "", tc.dpi, tc.broken)
			checkStandardSurface(t, doc, page, func(t *testing.T, img image.Image) {
				t.Helper()
				got := standardPixel(t, img, 50, 50)
				if (got.B > 200) != tc.blue || (got.R > 200) == tc.blue {
					t.Fatalf("实际选择颜色=%v", got)
				}
			})
		})
	}
}

func TestSubstitutionPixelDemandIncludesObjectParentAndContextTransforms(t *testing.T) {
	// 文档的300DPI不能覆盖直接光栅后端实际的72DPI。
	doc, _ := substitutionDocument(t, "", "", 300, false)
	factory := newOffscreenSurface(30, 30, geom.DPI(72))
	if factory == nil {
		t.Fatal("没有离屏绘制后端")
	}
	ctm := spec.CTM{25.4, 0, 0, 25.4, 0, 0}
	if got := doc.selectImageResource(factory, 10, 11, ctm); got != 10 {
		t.Fatalf("未缩放应取主图：%d", got)
	}
	factory.Scale(2, 2)
	if got := doc.selectImageResource(factory, 10, 11, ctm); got != 11 {
		t.Fatalf("上下文缩放未计入：%d", got)
	}
	factory.Scale(.5, .5)
	ctm = spec.CTM{0, 50.8, -50.8, 0, 0, 0}
	if got := doc.selectImageResource(factory, 10, 11, ctm); got != 11 {
		t.Fatalf("旋转/缩放未计入：%d", got)
	}
}

// 图片需求必须与实际放置使用同一个 Y 向下线性变换；两层错切的符号会相互影响，
// 只用缩放或正交旋转无法发现遗留的 F·L·F 共轭。
func TestImagePixelDemandMatchesPlacedAxesUnderOuterShear(t *testing.T) {
	doc, _ := substitutionDocument(t, "", "", 300, false)
	surface := newOffscreenSurface(30, 30, geom.DPI(72))
	if surface == nil {
		t.Fatal("没有离屏绘制后端")
	}
	outer := geom.Matrix{{1, .8, 0}, {.3, 1, 0}}
	surface.Transform(outer)
	ctm := spec.CTM{25.4, 10.16, 7.62, 20.32, 0, 0}
	width, height := doc.imagePixelDemand(surface, ctm)
	// 独立从 OFD 的两条单位轴计算页面向量，再施加外层变换；后端 Y 翻转不改变长度。
	xAxis := outer.Dot(geom.Point{X: ctm[0], Y: ctm[1]})
	yAxis := outer.Dot(geom.Point{X: ctm[2], Y: ctm[3]})
	wantWidth, wantHeight := math.Hypot(xAxis.X, xAxis.Y)*72/25.4, math.Hypot(yAxis.X, yAxis.Y)*72/25.4
	if math.Abs(width-wantWidth) > 1e-9 || math.Abs(height-wantHeight) > 1e-9 {
		t.Fatalf("图片需求=%g×%g，实际放置轴需求=%g×%g", width, height, wantWidth, wantHeight)
	}
	if got := doc.selectImageResource(surface, 10, 11, ctm); got != 11 {
		t.Fatalf("实际需求超过100像素时未选择高分辨率替代图：%d", got)
	}
}

func TestCompositeMissingPreviewsRetainsFullUnitCoordinates(t *testing.T) {
	unit := `<CompositeGraphicUnit ID="20" Width="25.4" Height="25.4"><Thumbnail>98</Thumbnail><Substitution>99</Substitution><Content><PathObject ID="4" Boundary="5 5 10 10" Fill="true" Stroke="false"><FillColor Value="0 255 0"/><AbbreviatedData>M 0 0 L 10 0 L 10 10 L 0 10 C</AbbreviatedData></PathObject></Content></CompositeGraphicUnit>`
	doc, page := substitutionDocument(t, `<CompositeObject ID="3" ResourceID="20" Boundary="0 0 25.4 25.4"/>`, unit, 72, false)
	checkStandardSurface(t, doc, page, func(t *testing.T, img image.Image) {
		t.Helper()
		if got := standardPixel(t, img, 20, 20); got != (color.NRGBA{R: 255, G: 255, B: 255, A: 255}) {
			t.Fatalf("不能把矢量墨迹放大铺满单元：%v", got)
		}
		if got := standardPixel(t, img, 80, 80); got.G < 250 || got.R > 5 || got.B > 5 {
			t.Fatalf("矢量单元坐标错误：%v", got)
		}
	})
}

func TestCompositePreviewUsesThumbnailSubstitutionAndVectorFallback(t *testing.T) {
	unit := `<CompositeGraphicUnit ID="20" Width="25.4" Height="25.4"><Thumbnail>10</Thumbnail><Substitution>11</Substitution><Content>` +
		`<PathObject ID="4" Boundary="0 0 25.4 25.4" Fill="true" Stroke="false"><FillColor Value="0 255 0"/><AbbreviatedData>M 0 0 L 25.4 0 L 25.4 25.4 L 0 25.4 C</AbbreviatedData></PathObject>` +
		`<TextObject ID="5" Boundary="2 2 20 8" Font="9" Size="3"><TextCode X="0" Y="4">preview</TextCode></TextObject></Content></CompositeGraphicUnit>`
	for _, tc := range []struct {
		name   string
		dpi    float64
		broken bool
		want   color.NRGBA
	}{
		{"缩略图", 72, false, color.NRGBA{R: 255, A: 255}},
		{"高分辨率替代图", 300, false, color.NRGBA{B: 255, A: 255}},
		{"更高需求恢复矢量", 1200, false, color.NRGBA{G: 255, A: 255}},
		{"损坏替代图恢复矢量", 300, true, color.NRGBA{G: 255, A: 255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, page := substitutionDocument(t, `<CompositeObject ID="3" ResourceID="20" Boundary="0 0 25.4 25.4"/>`, unit, tc.dpi, tc.broken)
			checkStandardSurface(t, doc, page, func(t *testing.T, img image.Image) {
				t.Helper()
				if got := standardPixel(t, img, 150, 150); absChannel(got.R, tc.want.R) > 2 || absChannel(got.G, tc.want.G) > 2 || absChannel(got.B, tc.want.B) > 2 || got.A != tc.want.A {
					t.Fatalf("复合预览颜色=%v，期望%v", got, tc.want)
				}
			})
			surface, err := doc.Page(page)
			if err != nil {
				t.Fatal(err)
			}
			if text := extractPDFText(t, surface); !strings.Contains(text, "preview") {
				t.Fatalf("PDF丢失矢量文字：%q", text)
			}
			if values, err := doc.QueryTextValues(page, TextQueryOptions{}); err != nil || len(values) != 1 || values[0] != "preview" {
				t.Fatalf("提取文字=%v", values)
			}
		})
	}
}

func absChannel(a, b uint8) int {
	return max(int(a)-int(b), int(b)-int(a))
}

func TestCompositePreviewPreservesAlphaClipAndNestedTextAndActions(t *testing.T) {
	unit := `<CompositeGraphicUnit ID="20" Width="25.4" Height="25.4"><Thumbnail>10</Thumbnail><Substitution>11</Substitution><Content><CompositeObject ID="6" ResourceID="21" Boundary="0 0 25.4 25.4"/></Content></CompositeGraphicUnit>` +
		`<CompositeGraphicUnit ID="21" Width="25.4" Height="25.4"><Content><TextObject ID="7" Boundary="2 2 20 8" Font="9" Size="3"><Actions><Action Event="CLICK"><URI URI="https://example.test/preview"/></Action></Actions><TextCode X="0" Y="4">nested</TextCode></TextObject></Content></CompositeGraphicUnit>`
	object := `<CompositeObject ID="3" ResourceID="20" Boundary="0 0 25.4 25.4" Alpha="128"><Clips><Clip><Area><Path><AbbreviatedData>M 0 0 L 12.7 0 L 12.7 25.4 L 0 25.4 C</AbbreviatedData></Path></Area></Clip></Clips></CompositeObject>`
	doc, page := substitutionDocument(t, object, unit, 72, false)
	checkStandardSurface(t, doc, page, func(t *testing.T, img image.Image) {
		t.Helper()
		inside, outside := standardPixel(t, img, 50, 150), standardPixel(t, img, 180, 150)
		if inside.R < 250 || inside.G < 120 || inside.G > 135 || inside.B < 120 || inside.B > 135 {
			t.Fatalf("整体透明度错误：%v", inside)
		}
		if outside != (color.NRGBA{R: 255, G: 255, B: 255, A: 255}) {
			t.Fatalf("预览越过裁剪：%v", outside)
		}
	})
	surface, err := doc.Page(page)
	if err != nil {
		t.Fatal(err)
	}
	if text := extractPDFText(t, surface); !strings.Contains(text, "nested") {
		t.Fatalf("嵌套文字丢失：%q", text)
	}
	links, err := doc.PageLinksWithVisibilityE(page, nil, PageVisibility{})
	if err != nil || len(links) != 1 || links[0].URI != "https://example.test/preview" {
		t.Fatalf("预览替代丢失链接：%+v %v", links, err)
	}
}
