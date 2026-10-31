package reader

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/backends/canvasfont"
	"golang.org/x/image/font/gofont/goregular"
)

type versionFallbackEngine struct {
	render.FontEngine
	reject *bool
}

func (f *versionFallbackEngine) UseFallbackFont(family string) error {
	if *f.reject {
		return errors.New("拒绝回退字体")
	}
	return f.FontEngine.UseFallbackFont(family)
}

// 独立历史文档复用主文档的页面、字体和图片 ID，内容与相对路径各自独立。
func historicalVersionPackage(t *testing.T, absolute bool) []byte {
	t.Helper()
	wrap := func(name, content string) string {
		return `<ofd:` + name + ` xmlns:ofd="http://www.ofdspec.org/2016">` + content + `</ofd:` + name + `>`
	}
	document := func(title string, pages int) string {
		list := ""
		for i := 0; i < pages; i++ {
			list += fmt.Sprintf(`<ofd:Page ID="%d" BaseLoc="Pages/P%d.xml"/>`, i+10, i)
		}
		return wrap("Document", `<ofd:CommonData><ofd:MaxUnitID>99</ofd:MaxUnitID><ofd:PageArea><ofd:PhysicalBox>0 0 30 20</ofd:PhysicalBox></ofd:PageArea><ofd:DocumentRes>Res.xml</ofd:DocumentRes></ofd:CommonData><ofd:Pages>`+list+`</ofd:Pages><ofd:Outlines><ofd:OutlineElem Title="`+title+`"><ofd:Actions><ofd:Action Event="CLICK"><ofd:Goto><ofd:Dest Type="Fit" PageID="10"/></ofd:Goto></ofd:Action></ofd:Actions></ofd:OutlineElem></ofd:Outlines><ofd:Attachments>Attachments.xml</ofd:Attachments>`)
	}
	entries := map[string][]byte{}
	fontFixture, err := os.ReadFile("../../testdata/ofdrw/intro.ofd")
	if err != nil {
		t.Fatal(err)
	}
	fixtureZIP, err := zip.NewReader(bytes.NewReader(fontFixture), int64(len(fontFixture)))
	if err != nil {
		t.Fatal(err)
	}
	var fontData []byte
	for _, file := range fixtureZIP.File {
		if strings.HasSuffix(file.Name, ".ttf") || strings.HasSuffix(file.Name, ".otf") {
			source, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			fontData, err = io.ReadAll(source)
			source.Close()
			if err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if len(fontData) == 0 {
		t.Fatal("fixture has no embedded font")
	}
	for _, item := range []struct {
		dir, text, font string
		pages           int
		ink             color.NRGBA
	}{
		{"Main", "latest", "MainFont", 2, color.NRGBA{R: 255, A: 255}},
		{"History/Old", "history", "HistoricalFont", 1, color.NRGBA{B: 255, A: 255}},
	} {
		entries[item.dir+"/Document.xml"] = []byte(document(item.text, item.pages))
		entries[item.dir+"/Res.xml"] = []byte(wrap("Res", `<ofd:Fonts><ofd:Font ID="1" FontName="`+item.font+`" FamilyName="`+item.font+`"><ofd:FontFile>font.ttf</ofd:FontFile></ofd:Font></ofd:Fonts><ofd:MultiMedias><ofd:MultiMedia ID="2" Type="Image" Format="PNG"><ofd:MediaFile>pixel.png</ofd:MediaFile></ofd:MultiMedia></ofd:MultiMedias>`))
		entries[item.dir+"/font.ttf"] = fontData
		for i := 0; i < item.pages; i++ {
			entries[fmt.Sprintf("%s/Pages/P%d.xml", item.dir, i)] = []byte(wrap("Page", `<ofd:Content><ofd:Layer ID="20"><ofd:TextObject ID="21" Font="1" Size="2" Boundary="1 1 25 4"><ofd:TextCode X="0" Y="2" DeltaX="2 2 2 2 2 2">`+item.text+`</ofd:TextCode></ofd:TextObject><ofd:ImageObject ID="22" ResourceID="2" Boundary="10 10 10 5" CTM="10 0 0 5 0 0"/></ofd:Layer></ofd:Content>`))
		}
		img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		img.SetNRGBA(0, 0, item.ink)
		var imageData bytes.Buffer
		if err := png.Encode(&imageData, img); err != nil {
			t.Fatal(err)
		}
		entries[item.dir+"/pixel.png"] = imageData.Bytes()
		entries[item.dir+"/Attachments.xml"] = []byte(wrap("Attachments", `<ofd:Attachment ID="attachment" Name="`+item.text+`"><ofd:FileLoc>note.txt</ofd:FileLoc></ofd:Attachment>`))
		entries[item.dir+"/note.txt"] = []byte(item.text)
	}
	root := "Old/Document.xml"
	if absolute {
		root = "/History/Old/Document.xml"
	}
	files := ""
	for _, name := range []string{"Document.xml", "Res.xml", "Pages/P0.xml", "pixel.png", "font.ttf", "Attachments.xml", "note.txt"} {
		files += `<ofd:File ID="f` + fmt.Sprint(len(files)) + `">Old/` + name + `</ofd:File>`
	}
	entries["History/Version.xml"] = []byte(`<ofd:DocVersion xmlns:ofd="http://www.ofdspec.org/2016" ID="v1" Name="Old"><ofd:FileList>` + files + `</ofd:FileList><ofd:DocRoot>` + root + `</ofd:DocRoot></ofd:DocVersion>`)
	entries["History/Broken.xml"] = []byte(`<ofd:DocVersion xmlns:ofd="http://www.ofdspec.org/2016" ID="broken"><ofd:FileList><ofd:File ID="missing">missing.png</ofd:File></ofd:FileList><ofd:DocRoot>` + root + `</ofd:DocRoot></ofd:DocVersion>`)
	entries["OFD.xml"] = []byte(`<ofd:OFD xmlns:ofd="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><ofd:DocBody><ofd:DocInfo><ofd:DocID>test</ofd:DocID></ofd:DocInfo><ofd:DocRoot>Main/Document.xml</ofd:DocRoot><ofd:Versions><ofd:Version ID="v1" Index="1" BaseLoc="History/Version.xml"/><ofd:Version ID="broken" Index="2" BaseLoc="History/Broken.xml"/></ofd:Versions></ofd:DocBody><ofd:DocBody><ofd:DocInfo><ofd:DocID>second</ofd:DocID></ofd:DocInfo><ofd:DocRoot>Main/Document.xml</ofd:DocRoot></ofd:DocBody></ofd:OFD>`)
	var result bytes.Buffer
	w := zip.NewWriter(&result)
	for name, data := range entries {
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
	return result.Bytes()
}

func TestSelectVersionHandsOffDroppedFallbackFont(t *testing.T) {
	family := t.Name()
	if err := RegisterFallbackFont(FontSource{Family: family, Data: goregular.TTF}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, ok := FallbackFontData(family); ok {
			_ = UnregisterFallbackFont(family)
		}
	})
	r, err := Open(duplicatePageIDsPackage(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.UseFallbackFont(family); err != nil {
		t.Fatal(err)
	}
	if err := UnregisterFallbackFont(family); err != nil {
		t.Fatal(err)
	}
	if _, ok := FallbackFontData(family); !ok {
		t.Fatal("打开的文档未延迟注销回退字体")
	}
	if err := r.SelectVersion(0, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := FallbackFontData(family); !ok {
		t.Fatal("版本切换时旧文档先释放，回退字体未交接给新文档")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := FallbackFontData(family); ok {
		t.Fatal("关闭切换后的文档后，已注销回退字体仍被持有")
	}
}

func TestSelectVersionFallbackFailureKeepsCurrentView(t *testing.T) {
	reject := false
	render.RegisterFontEngineFactory(func(doc *parser.Document) render.FontEngine {
		return &versionFallbackEngine{FontEngine: canvasfont.NewFonts(doc), reject: &reject}
	})
	t.Cleanup(func() {
		render.RegisterFontEngineFactory(func(doc *parser.Document) render.FontEngine {
			return canvasfont.NewFonts(doc)
		})
	})
	family := t.Name()
	if err := RegisterFallbackFont(FontSource{Family: family, Data: goregular.TTF}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, ok := FallbackFontData(family); ok {
			_ = UnregisterFallbackFont(family)
		}
	})
	r, err := Open(historicalVersionPackage(t, false))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.UseFallbackFont(family); err != nil {
		t.Fatal(err)
	}
	if err := r.SelectVersion(0, "v1"); err != nil {
		t.Fatal(err)
	}
	reject = true
	if err := r.SelectVersion(0, ""); err == nil {
		t.Fatal("新视图拒绝回退字体时仍报告切换成功")
	}
	runs, err := r.Text(0)
	if err != nil || len(runs) == 0 || runs[0].Text != "history" {
		t.Fatalf("失败后未保留历史版本视图: runs=%+v err=%v", runs, err)
	}
}

func TestSelectVersionLoadsIndependentPagesAndResources(t *testing.T) {
	for _, absolute := range []bool{false, true} {
		t.Run(fmt.Sprint("absolute=", absolute), func(t *testing.T) {
			r, err := Open(historicalVersionPackage(t, absolute))
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			check := func(text, font string, count int, blue bool) {
				t.Helper()
				if r.PageCount() != count {
					t.Fatalf("pages=%d want %d", r.PageCount(), count)
				}
				runs, err := r.Text(0)
				if err != nil || len(runs) == 0 || runs[0].Text != text {
					t.Fatalf("text=%+v err=%v", runs, err)
				}
				report, err := r.Search(t.Context(), text, SearchOptions{})
				if err != nil || len(report.Results) == 0 {
					t.Fatalf("search=%+v err=%v", report.Results, err)
				}
				fonts, err := r.FontList()
				if err != nil || len(fonts) < 1 || fonts[0].Name != font {
					t.Fatalf("fonts=%+v err=%v", fonts, err)
				}
				embedded, err := r.Fonts()
				if err != nil || len(embedded) < 1 || embedded[0].Name != font || len(embedded[0].Data) == 0 {
					t.Fatalf("embedded fonts missing for %s: %v", font, err)
				}
				outline, err := r.Outline()
				if err != nil || outline.Nodes[0].Title != text || outline.Nodes[0].Page != 0 {
					t.Fatalf("outline=%+v err=%v", outline, err)
				}
				attachments, err := r.Attachments()
				if err != nil || attachments[0].Name != text {
					t.Fatalf("attachments=%+v err=%v", attachments, err)
				}
				data, err := r.AttachmentData(0, "attachment", 0)
				if err != nil || string(data) != text {
					t.Fatalf("attachment=%q err=%v", data, err)
				}
				data, err = renderPageBytes(t.Context(), r, 0, RenderOptions{DPI: 25.4})
				if err != nil {
					t.Fatal(err)
				}
				img, err := png.Decode(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				red, _, b, alpha := img.At(15, 12).RGBA()
				if alpha == 0 || (blue && b <= red) || (!blue && red <= b) {
					t.Fatalf("wrong version image: %v", img.At(15, 12))
				}
				pdf, err := renderPDFBytes(t.Context(), r, []int{0}, RenderOptions{DPI: 25.4})
				if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
					t.Fatalf("version PDF export failed: %v", err)
				}
			}
			check("latest", "MainFont", 4, false)
			versions, err := r.Versions()
			if err != nil || versions[0].DocRoot != "/History/Old/Document.xml" || len(versions[0].Pages) != 0 {
				t.Fatalf("versions=%+v err=%v", versions, err)
			}
			for i := 0; i < 3; i++ {
				if err := r.SelectVersion(0, "v1"); err != nil {
					t.Fatal(err)
				}
				check("history", "HistoricalFont", 3, true)
				versions, err = r.Versions()
				if err != nil || !slices.Equal(versions[0].Pages, []int{0}) {
					t.Fatalf("selected versions=%+v err=%v", versions, err)
				}
				for _, id := range []string{"unknown", "broken"} {
					if err := r.SelectVersion(0, id); err == nil {
						t.Fatalf("accepted %s", id)
					}
					check("history", "HistoricalFont", 3, true)
				}
				if err := r.SelectVersion(0, ""); err != nil {
					t.Fatal(err)
				}
				check("latest", "MainFont", 4, false)
			}
			if err := r.SelectVersion(2, "v1"); err == nil {
				t.Fatal("accepted invalid scope")
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			if err := r.SelectVersion(0, "v1"); err == nil {
				t.Fatal("accepted closed reader")
			}
		})
	}
}

func TestSelectVersionConcurrentMetadataAndText(t *testing.T) {
	r, err := Open(historicalVersionPackage(t, false))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; i < 20; i++ {
			if _, err := r.Versions(); err != nil {
				t.Error(err)
			}
			if _, err := r.Outline(); err != nil {
				t.Error(err)
			}
			runs, err := r.Text(0)
			if err != nil || len(runs) == 0 || !strings.Contains("latest history", runs[0].Text) {
				t.Errorf("text=%+v err=%v", runs, err)
			}
		}
	})
	for i := 0; i < 10; i++ {
		if err := r.SelectVersion(0, "v1"); err != nil {
			t.Fatal(err)
		}
		if err := r.SelectVersion(0, ""); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}
