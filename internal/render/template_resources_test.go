package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/cache"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

// 同 ID 的三个资源集是非规范冲突样本，隔离兼容不能污染正文或另一模板。
// 正常模板 PageRes 的加载与路径解析依据 §7.7 表12、§7.9。
func privateTemplateDocument(t *testing.T) (*Document, *parser.Page) {
	t.Helper()
	files := map[string][]byte{
		"OFD.xml":          []byte(`<OFD><DocBody><DocRoot>Doc/Document.xml</DocRoot></DocBody></OFD>`),
		"Doc/Document.xml": []byte(`<Document><CommonData><PageArea><PhysicalBox>0 0 60 30</PhysicalBox></PageArea><DocumentRes>Res.xml</DocumentRes><TemplatePage ID="10" BaseLoc="T1/Page.xml"/><TemplatePage ID="20" BaseLoc="T2/Page.xml"/></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages></Document>`),
	}
	for i, dir := range []string{"Doc", "Doc/T1", "Doc/T2"} {
		x := float64(40)
		if i > 0 {
			x = float64(i-1) * 20
		}
		ink := []string{"0 255 0", "255 0 0", "0 0 255"}[i]
		var fill bytes.Buffer
		img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
		c := []color.NRGBA{{G: 255, A: 255}, {R: 255, A: 255}, {B: 255, A: 255}}[i]
		for j := 0; j < len(img.Pix); j += 4 {
			img.Pix[j], img.Pix[j+1], img.Pix[j+2], img.Pix[j+3] = c.R, c.G, c.B, c.A
		}
		if err := png.Encode(&fill, img); err != nil {
			t.Fatal(err)
		}
		files[dir+"/Assets/image.png"] = fill.Bytes()
		font := goregular.TTF
		if i == 2 {
			font = gomono.TTF
		}
		files[dir+"/Assets/font.ttf"] = font
		files[dir+"/Res.xml"] = fmt.Appendf(nil, `<Res BaseLoc="Assets"><ColorSpaces><ColorSpace ID="5" Type="RGB"><Palette><CV>%s</CV></Palette></ColorSpace></ColorSpaces><DrawParams><DrawParam ID="6"><FillColor ColorSpace="5" Index="0"/></DrawParam></DrawParams><Fonts><Font ID="9" FontName="Private"><FontFile>font.ttf</FontFile></Font></Fonts><MultiMedias><MultiMedia ID="7" Type="Image" Format="PNG"><MediaFile>image.png</MediaFile></MultiMedia></MultiMedias><CompositeGraphicUnits><CompositeGraphicUnit ID="8" Width="5" Height="5"><Content><PathObject ID="81" Boundary="0 0 5 5" DrawParam="6" Fill="true" Stroke="false"><Actions><Action Event="CLICK"><URI URI="https://example.test/%d"/></Action></Actions><AbbreviatedData>M 0 0 L 5 0 L 5 5 L 0 5 C</AbbreviatedData></PathObject></Content></CompositeGraphicUnit></CompositeGraphicUnits></Res>`, ink, i)
		var refs string
		if i == 0 {
			refs = `<Template TemplateID="10"/><Template TemplateID="20"/>`
		} else {
			refs = `<PageRes>Res.xml</PageRes>`
		}
		files[dir+"/Page.xml"] = fmt.Appendf(nil, `<Page>%s<Content><Layer ID="2"><PathObject ID="3" Boundary="%g 0 5 5" DrawParam="6" Fill="true" Stroke="false"><AbbreviatedData>M 0 0 L 5 0 L 5 5 L 0 5 C</AbbreviatedData></PathObject><ImageObject ID="4" ResourceID="7" Boundary="%g 6 5 5"/><CompositeObject ID="11" ResourceID="8" Boundary="%g 12 5 5"/><TextObject ID="12" Boundary="%g 20 19 6" Font="9" Size="3"><TextCode X="0" Y="3">iiii</TextCode></TextObject></Layer></Content></Page>`, refs, x, x, x, x)
	}
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for name, value := range files {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(value); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	doc := NewDocument(color.White, ofd.Documents[0], 0)
	t.Cleanup(func() { doc.Close(); _ = ofd.Close() })
	return doc, ofd.Documents[0].Pages[0]
}

func TestTemplatePrivateScopeReusesFontsAndSurvivesEvictionUntilRelease(t *testing.T) {
	doc, page := privateTemplateDocument(t)
	lease, err := page.AcquireLease()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	doc.templateScopes = cache.NewLRU[templateScopeKey, *templateScopeEntry](1, func(_ templateScopeKey, entry *templateScopeEntry) { entry.release() })
	_, first, release, err := doc.templateScope(10)
	if err != nil {
		t.Fatal(err)
	}
	release = sync.OnceFunc(release)
	defer release()
	_, again, releaseAgain, err := doc.templateScope(10)
	if err != nil {
		t.Fatal(err)
	}
	if again != first || again.fonts != first.fonts {
		t.Fatal("重复绘制模板重建了上下文或字体引擎")
	}
	releaseAgain()
	_, second, releaseSecond, err := doc.templateScope(20)
	if err != nil {
		t.Fatal(err)
	}
	releaseSecond()
	if first.closed.Load() || second == first || second.fonts == first.fonts {
		t.Fatal("淘汰提前关闭在途模板，或不同模板串用了字体作用域")
	}
	// 淘汰与最终 Close 都必须释放上下文，不影响仍在使用的旧条目。
	_, replacement, releaseReplacement, err := doc.templateScope(10)
	if err != nil {
		t.Fatal(err)
	}
	releaseReplacement()
	if replacement == first || !second.closed.Load() || doc.templateScopes.Len() != 1 {
		t.Fatal("模板上下文缓存没有遵守容量或释放淘汰资源")
	}
	doc.Close()
	if !replacement.closed.Load() {
		t.Fatal("关闭父文档后仍保留模板字体引擎")
	}
	release()
	if !first.closed.Load() {
		t.Fatal("淘汰模板的最后一个绘制引用归还后没有释放字体引擎")
	}
}

func TestTemplatePrivateScopeInvalidatesFallbackConfiguration(t *testing.T) {
	doc, page := privateTemplateDocument(t)
	lease, err := page.AcquireLease()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	_, first, release, err := doc.templateScope(10)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err := RegisterFallbackFont(goregular.TTF, t.Name(), FontRegular); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := UnregisterFallbackFont(t.Name()); err != nil {
			t.Error(err)
		}
	})
	if err := doc.UseFallbackFont(t.Name()); err != nil {
		t.Fatal(err)
	}
	_, changed, releaseChanged, err := doc.templateScope(10)
	if err != nil {
		t.Fatal(err)
	}
	releaseChanged()
	if changed == first || len(changed.fallbackFontFamilies()) != 1 {
		t.Fatal("回退字体配置变化后仍复用旧模板上下文")
	}
}

func TestTemplatePrivateResourcesRenderImagesColorsCompositesAndFonts(t *testing.T) {
	doc, page := privateTemplateDocument(t)
	checkStandardSurface(t, doc, page, func(t *testing.T, img image.Image) {
		t.Helper()
		for i, want := range []color.NRGBA{{R: 255, A: 255}, {B: 255, A: 255}, {G: 255, A: 255}} {
			for _, y := range []int{20, 80, 140} {
				if got := standardPixel(t, img, i*200+20, y); got != want {
					t.Errorf("区域%d y=%d 颜色=%v，期望%v", i, y, got, want)
				}
			}
		}
	})
	layouts, err := doc.QueryTextLayouts(page, TextQueryOptions{})
	if err != nil || len(layouts) != 3 {
		t.Fatalf("文字布局=%+v %v", layouts, err)
	}
	if layouts[1].Glyphs[0].Width <= layouts[0].Glyphs[0].Width*1.5 {
		t.Fatalf("同 ID 模板字体没有隔离：%+v", layouts)
	}
	if values, err := doc.QueryTextValues(page, TextQueryOptions{}); err != nil || !reflect.DeepEqual(values, []string{"iiii", "iiii", "iiii"}) {
		t.Fatal("模板文字丢失")
	}
	surface, err := doc.Page(page)
	if err != nil {
		t.Fatal(err)
	}
	if text := extractPDFText(t, surface); strings.Count(text, "iiii") != 3 {
		t.Fatalf("PDF 模板文字不完整：%q", text)
	}
	links, err := doc.PageLinksWithVisibilityE(page, nil, PageVisibility{})
	if err != nil || len(links) != 3 {
		t.Fatalf("模板复合链接=%+v %v", links, err)
	}
	want := map[string]float64{"https://example.test/0": 40, "https://example.test/1": 0, "https://example.test/2": 20}
	for _, link := range links {
		if x, ok := want[link.URI]; !ok || link.X != x {
			t.Fatalf("复合链接资源串用：%+v", link)
		}
	}
	if doc.GetMedia(7).MediaFile != "Doc/Assets/image.png" || doc.GetFont(9).FontFile != "Doc/Assets/font.ttf" {
		t.Fatal("模板资源污染了正文索引")
	}
}

func TestConcurrentTemplateResourceScopesDoNotChangeBodyOrEachOther(t *testing.T) {
	doc, page := privateTemplateDocument(t)
	var workers sync.WaitGroup
	for range 6 {
		workers.Go(func() {
			for range 2 {
				surface, err := doc.Page(page)
				if err != nil {
					t.Error(err)
					return
				}
				img := surface.Rasterize(geom.DPMM(2), nil)
				if got := standardPixel(t, img, 4, 4); got.R != 255 || got.G != 0 || got.B != 0 {
					t.Errorf("并发模板颜色=%v", got)
				}
				if _, err := doc.QueryTextLayouts(page, TextQueryOptions{}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	workers.Wait()
}
