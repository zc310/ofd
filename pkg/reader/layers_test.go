package reader

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/canvas"
	"golang.org/x/image/font/gofont/goregular"
)

// 独立 XML 包：两页复用 Layer ID；模板故意与正文层同 ID，验证过滤作用域。
func layerTestPackage(t *testing.T) []byte {
	t.Helper()
	path := func(id int, box, ink, uri string) string {
		action := ""
		if uri != "" {
			action = `<Actions><Action Event="CLICK"><URI URI="` + uri + `"/></Action></Actions>`
		}
		return fmt.Sprintf(`<PathObject ID="%d" Boundary="%s" Fill="true" Stroke="false">%s<FillColor Value="%s"/><AbbreviatedData>M 0 0 L 4 0 L 4 4 L 0 4 C</AbbreviatedData></PathObject>`, id, box, action, ink)
	}
	text := func(id int, value string) string {
		return fmt.Sprintf(`<TextObject ID="%d" Boundary="1 10 18 5" Font="1" Size="2"><TextCode X="0" Y="2">%s</TextCode></TextObject>`, id, value)
	}
	page := `<Page><Template TemplateID="99"/><Content><Layer ID="20">` + path(21, "4 4 4 4", "255 0 0", "https://example.test/hidden") + text(22, "hidden") + `</Layer><Layer ID="30" Type="Foreground">` + path(31, "10 4 4 4", "0 0 255", "") + text(32, "visible") + `</Layer></Content></Page>`
	entries := map[string][]byte{
		"OFD.xml":          []byte(`<OFD Version="1.0"><DocBody><DocRoot>Doc/Document.xml</DocRoot></DocBody></OFD>`),
		"Doc/Document.xml": []byte(`<Document><CommonData><MaxUnitID>999</MaxUnitID><PageArea><PhysicalBox>0 0 20 20</PhysicalBox></PageArea><DocumentRes>Res.xml</DocumentRes><TemplatePage ID="99" BaseLoc="Template.xml"/></CommonData><Pages><Page ID="1" BaseLoc="P1.xml"/><Page ID="2" BaseLoc="P2.xml"/></Pages><Annotations>Annotations.xml</Annotations></Document>`),
		"Doc/Res.xml":      []byte(`<Res><Fonts><Font ID="1" FontName="LayerTest"><FontFile>font.ttf</FontFile></Font></Fonts></Res>`),
		// 使用覆盖测试文字的完整字体，避免子集字体缺字后依赖机器的回退字体。
		"Doc/font.ttf": goregular.TTF, "Doc/P1.xml": []byte(page), "Doc/P2.xml": []byte(page),
		"Doc/Template.xml":    []byte(`<Page><Content><Layer ID="20">` + path(41, "0 0 4 4", "0 255 0", "") + `</Layer></Content></Page>`),
		"Doc/Annotations.xml": []byte(`<Annotations><Page PageID="1"><FileLoc>Annot.xml</FileLoc></Page></Annotations>`),
		"Doc/Annot.xml":       []byte(`<PageAnnot><Annot ID="50" Type="Stamp" Creator="test" LastModDate="2026-01-01"><Appearance Boundary="16 16 4 4">` + path(51, "0 0 4 4", "255 0 255", "") + `</Appearance></Annot></PageAnnot>`),
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
	return data.Bytes()
}

func TestLayerVisibilityIsPageScopedAndFiltersTextSearchAndLinks(t *testing.T) {
	r, err := Open(layerTestPackage(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	layers, err := r.Layers(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 3 || layers[0].ID != "20" || layers[0].Type != "Body" || layers[0].Objects != 2 || !layers[0].Visible || layers[2].Kind != "template" || layers[2].Key != "template:99" {
		t.Fatalf("图层=%+v", layers)
	}
	// 预热两种缓存，以及全局链接缓存。
	if _, err := r.Text(0); err != nil {
		t.Fatal(err)
	}
	if report, err := r.Search(t.Context(), "hidden", SearchOptions{}); err != nil || len(report.Results) != 2 {
		t.Fatalf("初始搜索=%+v %v", report.Results, err)
	}
	if links, err := r.PageLinks(); err != nil || len(links) != 2 {
		t.Fatalf("初始链接=%+v %v", links, err)
	}
	if err := r.SetHiddenLayers(0, []string{"20"}); err != nil {
		t.Fatal(err)
	}
	if report, err := r.Search(t.Context(), "hidden", SearchOptions{}); err != nil || len(report.Results) != 1 || report.Results[0].Page != 1 {
		t.Fatalf("隐藏文字仍参与搜索: %+v %v", report.Results, err)
	}
	// 在搜索先构建轻量索引后，run 索引必须与过滤后的 Text 保持一致。
	if report, err := r.Search(t.Context(), "visible", SearchOptions{}); err != nil || len(report.Results) != 2 || report.Results[0].Run != 0 || report.Results[0].Text != "visible" {
		t.Fatalf("过滤后的搜索布局=%+v %v", report.Results, err)
	}
	runs, err := r.Text(0)
	if err != nil || len(runs) != 1 || runs[0].Text != "visible" {
		t.Fatalf("隐藏文字仍可选择: %+v %v", runs, err)
	}
	if links, err := r.PageLinksForPage(0); err != nil || len(links) != 0 {
		t.Fatalf("隐藏链接仍可点击: %+v %v", links, err)
	}
	if links, err := r.PageLinks(); err != nil || len(links) != 1 || links[0].Page != 1 {
		t.Fatalf("全局链接缓存未刷新: %+v %v", links, err)
	}
	if err := r.SetHiddenLayers(0, []string{"unknown"}); err == nil {
		t.Fatal("未知图层未拒绝")
	}
	other, _ := r.Layers(1)
	if !other[0].Visible {
		t.Fatal("设置泄漏到其他页面")
	}
	if err := r.SetHiddenLayers(0, nil); err != nil {
		t.Fatal(err)
	}
	if report, _ := r.Search(t.Context(), "hidden", SearchOptions{}); len(report.Results) != 2 {
		t.Fatal("恢复后文字未恢复")
	}
	if _, err := r.Layers(-1); err == nil {
		t.Fatal("负页码未拒绝")
	}
	if err := r.SetHiddenLayers(2, nil); err == nil {
		t.Fatal("越界页码未拒绝")
	}
}

func TestLayerVisibilityRendersPNGSVGAndPDFWithoutChangingSource(t *testing.T) {
	source := layerTestPackage(t)
	r, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	renderPNG := func() []byte {
		t.Helper()
		data, err := r.RenderPage(t.Context(), 0, RenderOptions{DPI: 127, Background: color.White})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	baseline := renderPNG()
	baselineSVG, err := r.RenderPage(t.Context(), 0, RenderOptions{Format: RenderSVG})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetHiddenLayers(0, []string{"20"}); err != nil {
		t.Fatal(err)
	}
	filtered := renderPNG()
	if bytes.Equal(baseline, filtered) {
		t.Fatal("隐藏图层未改变渲染")
	}
	img, err := png.Decode(bytes.NewReader(filtered))
	if err != nil {
		t.Fatal(err)
	}
	checkLayerPixels(t, img, 5)
	svg, err := r.RenderPage(t.Context(), 0, RenderOptions{Format: RenderSVG})
	if err != nil || bytes.Equal(svg, baselineSVG) {
		t.Fatalf("SVG 未过滤: %v", err)
	}
	if links, err := r.pdfPageLinks(t.Context(), []int{0}); err != nil || len(links[0]) != 0 {
		t.Fatalf("PDF 链接未过滤: %v", err)
	}
	pdf, err := r.RenderPDF(t.Context(), []int{0}, RenderOptions{DPI: 72, Background: color.White})
	if err != nil {
		t.Fatal(err)
	}
	// 原始共享页面仍含完整图层，恢复后渲染结果应完全一致。
	if len(contentLayers(r.pages[0].page)) != 2 {
		t.Fatal("共享页面被修改")
	}
	if err := r.SetHiddenLayers(0, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(renderPNG(), baseline) {
		t.Fatal("恢复图层后渲染不一致")
	}
	if err := r.SetHiddenLayers(0, []string{"20", "30"}); err != nil {
		t.Fatal(err)
	}
	if err := r.SelectVersion(0, ""); err != nil {
		t.Fatal(err)
	}
	layers, _ := r.Layers(0)
	if !layers[0].Visible || !layers[1].Visible {
		t.Fatal("切换版本未清空显示设置")
	}
	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "filtered.pdf")
	if err := os.WriteFile(pdfPath, pdf, 0o600); err != nil {
		t.Fatal(err)
	}
	if tool, err := exec.LookPath("pdftoppm"); err == nil {
		prefix := filepath.Join(dir, "page")
		if out, err := exec.CommandContext(t.Context(), tool, "-r", "127", "-singlefile", "-png", pdfPath, prefix).CombinedOutput(); err != nil {
			t.Fatalf("PDF 栅格化失败: %v %s", err, out)
		}
		f, err := os.Open(prefix + ".png")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		img, err := png.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		checkLayerPixels(t, img, 5)
	}
	if tool, err := exec.LookPath("pdftotext"); err == nil {
		text, err := exec.CommandContext(t.Context(), tool, pdfPath, "-").Output()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(text), "hidden") || !strings.Contains(string(text), "visible") {
			t.Fatalf("PDF 可复制文字未同步过滤: %q", text)
		}
	}
}

func checkLayerPixels(t *testing.T, img image.Image, scale int) {
	t.Helper()
	for _, test := range []struct {
		x, y int
		want color.NRGBA
	}{{6, 6, color.NRGBA{R: 255, G: 255, B: 255, A: 255}}, {12, 6, color.NRGBA{B: 255, A: 255}}, {2, 2, color.NRGBA{G: 255, A: 255}}, {18, 18, color.NRGBA{R: 255, B: 255, A: 255}}} {
		got := color.NRGBAModel.Convert(img.At(test.x*scale, test.y*scale)).(color.NRGBA)
		if got != test.want {
			t.Fatalf("像素 (%d,%d)=%v，期望 %v", test.x, test.y, got, test.want)
		}
	}
}

func TestLayerVisibilityConcurrentReaders(t *testing.T) {
	r, err := Open(layerTestPackage(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Go(func() {
			for j := 0; j < 4; j++ {
				if _, err := r.Layers(0); err != nil {
					t.Error(err)
				}
				if _, err := r.Text(0); err != nil {
					t.Error(err)
				}
				if _, err := r.Search(t.Context(), "visible", SearchOptions{}); err != nil {
					t.Error(err)
				}
				if _, err := r.RenderPage(t.Context(), 0, RenderOptions{DPI: 25.4}); err != nil {
					t.Error(err)
				}
			}
		})
	}
	for i := 0; i < 4; i++ {
		ids := []string{"20", "template:99"}
		if i%2 == 0 {
			ids = nil
		}
		if err := r.SetHiddenLayers(0, ids); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}

func TestLayerVisibilitySurvivesFailedVersionSwitchAndResetsOnSuccess(t *testing.T) {
	r, err := Open(historicalVersionPackage(t, false))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.SetHiddenLayers(0, []string{"20"}); err != nil {
		t.Fatal(err)
	}
	if err := r.SelectVersion(0, "broken"); err == nil {
		t.Fatal("无效版本应失败")
	}
	layers, err := r.Layers(0)
	if err != nil || layers[0].Visible {
		t.Fatalf("失败时应保留设置: %+v %v", layers, err)
	}
	if err := r.SelectVersion(0, "v1"); err != nil {
		t.Fatal(err)
	}
	layers, err = r.Layers(0)
	if err != nil || !layers[0].Visible {
		t.Fatalf("成功切换版本应重置设置: %+v %v", layers, err)
	}
}

func templateLayerTestPackage(t *testing.T) []byte {
	t.Helper()
	source := layerTestPackage(t)
	archive, err := zip.NewReader(bytes.NewReader(source), int64(len(source)))
	if err != nil {
		t.Fatal(err)
	}
	entries := make(map[string][]byte)
	for _, file := range archive.File {
		f, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		entries[file.Name], err = io.ReadAll(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	text := func(id int, value string) string {
		y := 0
		if value == "foregroundword" {
			y = 13
		}
		return fmt.Sprintf(`<TextObject ID="%d" Boundary="5 %d 15 2" Font="1" Size="1.5"><TextCode X="0" Y="1.5">%s</TextCode></TextObject>`, id, y, value)
	}
	entries["Doc/Template.xml"] = []byte(strings.Replace(string(entries["Doc/Template.xml"]), `</Layer>`, text(45, "backgroundword")+`</Layer>`, 1))
	entries["Doc/Template.xml"] = []byte(strings.Replace(string(entries["Doc/Template.xml"]), `<FillColor`, `<Actions><Action Event="CLICK"><URI URI="https://example.test/background"/></Action></Actions><FillColor`, 1))
	entries["Doc/Foreground.xml"] = []byte(`<Page><Content><Layer ID="60"><PathObject ID="61" Boundary="10 16 4 4" Fill="true" Stroke="false"><Actions><Action Event="CLICK"><URI URI="https://example.test/foreground"/></Action></Actions><FillColor Value="255 128 0"/><AbbreviatedData>M 0 0 L 4 0 L 4 4 L 0 4 C</AbbreviatedData></PathObject>` + text(62, "foregroundword") + `</Layer></Content></Page>`)
	entries["Doc/Document.xml"] = []byte(strings.Replace(string(entries["Doc/Document.xml"]), `</CommonData>`, `<TemplatePage ID="100" Name="前景测试" ZOrder="Foreground" BaseLoc="Foreground.xml"/></CommonData>`, 1))
	for _, name := range []string{"Doc/P1.xml", "Doc/P2.xml"} {
		entries[name] = []byte(strings.Replace(string(entries[name]), `<Template TemplateID="99"/>`, `<Template TemplateID="99"/><Template TemplateID="100"/>`, 1))
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, data := range entries {
		w, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestTemplateVisibilityIsPageScopedAndFiltersAllOutputs(t *testing.T) {
	checkTemplateVisibilityOutputs(t)
}

// canvas 的系统字体列表属于进程全局状态；在子进程中清空，避免影响其他字体测试。
func TestTemplateVisibilityWorksWithoutSystemFonts(t *testing.T) {
	const isolated = "OFD_TEST_TEMPLATE_WITHOUT_SYSTEM_FONTS"
	if os.Getenv(isolated) == "1" {
		dir := t.TempDir()
		if err := canvas.CacheSystemFonts(filepath.Join(dir, "fonts.cache"), []string{dir}); err != nil {
			t.Fatal(err)
		}
		checkTemplateVisibilityOutputs(t)
		return
	}
	if runtime.GOOS == "js" || runtime.GOOS == "wasip1" {
		t.Skip("当前平台不支持子进程")
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTemplateVisibilityWorksWithoutSystemFonts$", "-test.v")
	cmd.Env = append(os.Environ(), isolated+"=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("无系统字体的模板输出失败: %v\n%s", err, output)
	}
}

func checkTemplateVisibilityOutputs(t *testing.T) {
	t.Helper()
	r, err := Open(templateLayerTestPackage(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	layers, err := r.Layers(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 4 || layers[2].Kind != "template" || layers[2].Type != "Background" || layers[3].Type != "Foreground" || layers[3].Name != "前景测试" {
		t.Fatalf("正文与模板清单=%+v", layers)
	}
	baselineOther, err := r.RenderPage(t.Context(), 1, RenderOptions{DPI: 127, Background: color.White})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Text(0); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Search(t.Context(), "backgroundword", SearchOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.PageLinks(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name                   string
		keys                   []string
		background, foreground bool
	}{
		{"background", []string{"template:99"}, false, true},
		{"foreground", []string{"template:100"}, true, false},
		{"both", []string{"template:99", "template:100"}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := r.SetHiddenLayers(0, test.keys); err != nil {
				t.Fatal(err)
			}
			data, err := r.RenderPage(t.Context(), 0, RenderOptions{DPI: 127, Background: color.White})
			if err != nil {
				t.Fatal(err)
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			check := func(img image.Image) {
				t.Helper()
				wantBackground := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
				if test.background {
					wantBackground = color.NRGBA{G: 255, A: 255}
				}
				wantForeground := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
				if test.foreground {
					wantForeground = color.NRGBA{R: 255, G: 128, A: 255}
				}
				for _, pixel := range []struct {
					x, y int
					want color.NRGBA
				}{{2, 2, wantBackground}, {12, 18, wantForeground}, {6, 6, color.NRGBA{R: 255, A: 255}}, {18, 18, color.NRGBA{R: 255, B: 255, A: 255}}} {
					if got := color.NRGBAModel.Convert(img.At(pixel.x*5, pixel.y*5)).(color.NRGBA); got != pixel.want {
						t.Fatalf("%s 像素(%d,%d)=%v，期望 %v", test.name, pixel.x, pixel.y, got, pixel.want)
					}
				}
			}
			check(img)
			runs, err := r.Text(0)
			if err != nil {
				t.Fatal(err)
			}
			var text strings.Builder
			for _, run := range runs {
				text.WriteString(run.Text)
			}
			if strings.Contains(text.String(), "backgroundword") != test.background || strings.Contains(text.String(), "foregroundword") != test.foreground || !strings.Contains(text.String(), "hidden") {
				t.Fatalf("模板文字过滤错误: %s", text.String())
			}
			for word, visible := range map[string]bool{"backgroundword": test.background, "foregroundword": test.foreground} {
				report, err := r.Search(t.Context(), word, SearchOptions{})
				want := 1
				if visible {
					want = 2
				}
				if err != nil || len(report.Results) != want {
					t.Fatalf("模板搜索 %s=%+v %v", word, report.Results, err)
				}
			}
			links, err := r.PageLinksForPage(0)
			if err != nil {
				t.Fatal(err)
			}
			wantLinks := 1
			if test.background {
				wantLinks++
			}
			if test.foreground {
				wantLinks++
			}
			pdfLinks, err := r.pdfPageLinks(t.Context(), []int{0})
			if err != nil {
				t.Fatal(err)
			}
			if len(links) != wantLinks || len(pdfLinks[0]) != wantLinks {
				t.Fatalf("模板链接未同步过滤: %+v", links)
			}
			other, err := r.RenderPage(t.Context(), 1, RenderOptions{DPI: 127, Background: color.White})
			if err != nil || !bytes.Equal(other, baselineOther) {
				t.Fatalf("模板设置影响了共用模板的另一页: %v", err)
			}
			svg, err := r.RenderPage(t.Context(), 0, RenderOptions{Format: RenderSVG})
			if err != nil || len(svg) == 0 {
				t.Fatalf("SVG 输出失败: %v", err)
			}
			pdf, err := r.RenderPDF(t.Context(), []int{0}, RenderOptions{DPI: 72, Background: color.White})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			path := filepath.Join(dir, "templates.pdf")
			if err := os.WriteFile(path, pdf, 0o600); err != nil {
				t.Fatal(err)
			}
			if tool, err := exec.LookPath("pdftoppm"); err == nil {
				prefix := filepath.Join(dir, "page")
				if out, err := exec.CommandContext(t.Context(), tool, "-r", "127", "-singlefile", "-png", path, prefix).CombinedOutput(); err != nil {
					t.Fatalf("PDF 栅格化失败: %v %s", err, out)
				}
				f, err := os.Open(prefix + ".png")
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				img, err := png.Decode(f)
				if err != nil {
					t.Fatal(err)
				}
				check(img)
			}
			if tool, err := exec.LookPath("pdftotext"); err == nil {
				text, err := exec.CommandContext(t.Context(), tool, path, "-").Output()
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(text), "backgroundword") != test.background || strings.Contains(string(text), "foregroundword") != test.foreground {
					t.Fatalf("PDF 文字过滤错误: %q", text)
				}
			}
		})
	}
}

func TestInvoice999ListsBodyAndBackgroundTemplateOnEveryPage(t *testing.T) {
	data, err := os.ReadFile("../../testdata/ofdrw/999.ofd")
	if err != nil {
		t.Fatal(err)
	}
	r, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for index, ids := range [][2]string{{"67", "9"}, {"130", "91"}, {"309", "270"}, {"488", "449"}, {"668", "628"}} {
		layers, err := r.Layers(index)
		if err != nil {
			t.Fatal(err)
		}
		if len(layers) != 2 || layers[0].ID != ids[0] || layers[0].Kind != "layer" || layers[1].ID != ids[1] || layers[1].Kind != "template" || layers[1].Type != "Background" {
			t.Fatalf("第 %d 页=%+v", index+1, layers)
		}
	}
	before, err := r.RenderPage(t.Context(), 0, RenderOptions{DPI: 50})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetHiddenLayers(0, []string{"template:9"}); err != nil {
		t.Fatal(err)
	}
	after, err := r.RenderPage(t.Context(), 0, RenderOptions{DPI: 50})
	if err != nil || bytes.Equal(before, after) {
		t.Fatalf("999.ofd 背景模板未隐藏: %v", err)
	}
	if err := r.SetHiddenLayers(0, nil); err != nil {
		t.Fatal(err)
	}
	restored, err := r.RenderPage(t.Context(), 0, RenderOptions{DPI: 50})
	if err != nil || !bytes.Equal(before, restored) {
		t.Fatalf("999.ofd 模板未恢复: %v", err)
	}
}
