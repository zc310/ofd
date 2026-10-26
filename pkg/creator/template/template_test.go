package template_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zc310/ofd/internal/manifest"
	ofdexport "github.com/zc310/ofd/internal/manifest/export"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/converter"
	"github.com/zc310/ofd/pkg/creator"
	ofdtemplate "github.com/zc310/ofd/pkg/creator/template"
	"github.com/zc310/ofd/pkg/validator"
	"go.yaml.in/yaml/v3"
	"golang.org/x/image/font/gofont/goregular"
)

func TestExportedDocumentBecomesReusableTemplateWithPageResources(t *testing.T) {
	var imageData bytes.Buffer
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&imageData, img); err != nil {
		t.Fatal(err)
	}
	source, err := creator.Marshal(creator.Document{
		ID: "export-template", Fonts: []creator.Font{{Name: "Body", Data: goregular.TTF}},
		Templates: []creator.TemplatePage{{ID: 100, Items: []creator.Item{creator.Text{X: 10, Y: 5, Width: 100, Height: 8, Size: 4, Font: "Body", Value: "Invoice"}}}},
		Pages: []creator.Page{{Templates: []creator.TemplateRef{{ID: 100}}, Resources: []creator.PageResource{
			{Images: []creator.PageImage{{ID: 9002, Format: "PNG", Name: "logo.png", Data: imageData.Bytes()}}},
			{Data: []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."><DrawParams><DrawParam ID="9001" LineWidth="0.3"/></DrawParams></Res>`)},
		}, Layers: []creator.Layer{{Type: "Body", Items: []creator.Item{
			creator.Path{X: 10, Y: 10, Width: 100, Height: 60, Data: "M 0 0 L 100 0 L 100 60 L 0 60 C", Stroke: true, DrawParam: "#9001"},
			creator.Text{X: 20, Y: 20, Width: 80, Height: 10, Size: 4, Font: "Body", Value: "OLD CUSTOMER"},
			creator.Text{X: 20, Y: 40, Width: 80, Height: 10, Size: 4, Font: "Body", Value: "OLD AMOUNT"},
			creator.Image{X: 120, Y: 10, Width: 10, Height: 10, ResourceID: 9002},
		}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"yaml", "json"} {
		t.Run(format, func(t *testing.T) {
			assets := t.TempDir()
			m, err := ofdexport.BuildManifest(source, 0, ofdexport.Options{AssetRoot: assets, Format: format})
			if err != nil {
				t.Fatal(err)
			}
			m.Pages[0].Repeat = "invoices"
			items := m.Pages[0].Layers[0].Items
			items[1].Bind = &manifest.Binding{Path: "buyer"}
			items[2].Bind = &manifest.Binding{Path: "amount", Align: "right"}
			// 导出字体是子集，可变字段改用完整字体，同时保留静态标题字体。
			m.Resources.Fonts = append(m.Resources.Fonts, manifest.Font{Name: "FullBody", File: "full.ttf"})
			items[1].Font, items[2].Font = "FullBody", "FullBody"
			encoded, err := json.Marshal(m)
			if format == "yaml" {
				encoded, err = yaml.Marshal(m)
			}
			if err != nil {
				t.Fatal(err)
			}
			tmpl, err := ofdtemplate.Parse(encoded, format, assets)
			if err != nil {
				t.Fatal(err)
			}
			// 使用资源加载回调同时读取导出资源，避免修改模板内部状态。
			options := ofdtemplate.Options{LoadAsset: func(name string) ([]byte, error) {
				if name == "full.ttf" {
					return goregular.TTF, nil
				}
				return os.ReadFile(filepath.Join(assets, filepath.FromSlash(name)))
			}}
			doc, err := tmpl.Render(strings.NewReader(`{"invoices":[{"buyer":"A & <B>","amount":"1234.50"},{"buyer":"Other Company","amount":9007199254740993}]}`), options)
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Pages) != 2 {
				t.Fatalf("页数 = %d", len(doc.Pages))
			}
			output, err := creator.MarshalWithOptions(doc, creator.CreateOptions{Deterministic: true})
			if err != nil {
				t.Fatal(err)
			}
			instance, err := validator.New()
			if err != nil {
				t.Fatal(err)
			}
			report := instance.ValidateReader(context.Background(), bytes.NewReader(output), "template.ofd")
			if report.HasErrors() || report.HasWarnings() {
				var diagnostics bytes.Buffer
				_ = validator.RenderText(&diagnostics, report)
				t.Fatal(diagnostics.String())
			}
			var text bytes.Buffer
			if err := converter.Text(context.Background(), output, &text); err != nil {
				t.Fatal(err)
			}
			for _, expected := range []string{"A & <B>", "Other Company", "1234.50", "9007199254740993"} {
				if !strings.Contains(text.String(), expected) {
					t.Fatalf("文字提取缺少 %q: %s", expected, text.String())
				}
			}
			if strings.Contains(text.String(), "OLD") {
				t.Fatalf("旧字段仍可提取: %s", text.String())
			}
			parsed, err := parser.NewOFD(output)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = parsed.Close() }()
			if len(parsed.Documents[0].Pages) != 2 {
				t.Fatal("生成包页面数量错误")
			}
			// 再导出、重建，验证共享资源引用可被其他创作流程正常解析。
			reexported, err := ofdexport.BuildManifest(output, 0, ofdexport.Options{AssetRoot: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			if len(reexported.Pages) != 2 {
				t.Fatal("再次导出页数错误")
			}
		})
	}
}

func TestTemplateCanRenderConcurrentlyWithIndependentData(t *testing.T) {
	tmpl, err := ofdtemplate.Parse([]byte(`{"version":1,"document":{"id":"concurrent"},"pages":[{"items":[{"type":"text","width":80,"height":10,"bind":{"path":"name"}}]}]}`), "json", "")
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for _, name := range []string{"First", "Second", "Third"} {
		group.Go(func() {
			doc, err := tmpl.Render(strings.NewReader(`{"name":"`+name+`"}`), ofdtemplate.Options{})
			if err != nil {
				t.Error(err)
				return
			}
			if got := doc.Pages[0].Items[0].(creator.Text).TextCodes[0].Value; got != name {
				t.Errorf("数据串用: %q != %q", got, name)
			}
		})
	}
	group.Wait()
}
