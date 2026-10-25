package flow_test

import (
	"context"
	"strings"
	"testing"

	fontfile "github.com/zc310/font"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
)

// 五类公文字体都先注册和校验，仅实际使用的字体进入最终模型；使用楷体时仍须保留并子集化。
func TestOfficialFlowOmitsUnusedFontRolesAndSubsetsUsedFaces(t *testing.T) {
	for _, useKai := range []bool{false, true} {
		opts := flow.GBT9704Options(flow.OfficialFonts{Body: "Body", Title: "Title", Hei: "Hei", Kai: "Kai", Song: "Song"})
		opts.Fonts = append(opts.Fonts,
			creator.Font{Name: "Body", Data: goregular.TTF},
			creator.Font{Name: "Title", Data: gobold.TTF, Bold: true},
			creator.Font{Name: "Hei", Data: gobold.TTF, Bold: true},
			creator.Font{Name: "Kai", Data: goitalic.TTF, Italic: true},
			creator.Font{Name: "Song", Data: goregular.TTF},
		)
		opts.Official.Letterhead = &flow.OfficialLetterhead{Org: "EXAMPLE OFFICE", DocNo: "OF [2026] 1"}
		d := flow.New(opts)
		d.AddHeading(1, "Notice")
		d.AddParagraph("Please review all records.")
		d.AddHeading(2, "Requirements")
		d.AddParagraph("Keep the final document.")
		if useKai {
			d.AddHeading(3, "Details")
			d.AddParagraph("Include the records.")
		}
		model, err := d.Build(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		want := 4
		if useKai {
			want++
		}
		if len(model.Fonts) != want {
			t.Fatalf("公文携带未使用字体: useKai=%v fonts=%d want=%d", useKai, len(model.Fonts), want)
		}
		for _, font := range model.Fonts {
			if strings.HasPrefix(font.Name, "Flow-") || font.Name == "Kai" && !useKai {
				t.Fatalf("未使用字体进入输出模型: %q", font.Name)
			}
		}
		if len(opts.Fonts) != 9 || opts.Official.Fonts.Kai != "Kai" {
			t.Fatal("清理修改了调用方的字体配置")
		}
		data, err := creator.MarshalWithOptions(*model, creator.CreateOptions{Deterministic: true, CompleteTextCodeDeltas: true})
		if err != nil {
			t.Fatal(err)
		}
		ofd, err := parser.NewOFD(data)
		if err != nil {
			t.Fatal(err)
		}
		defer ofd.Close()
		document := ofd.Documents[0]
		if len(document.Fonts()) != want {
			t.Fatal("写出时重新加入了未使用字体")
		}
		for _, font := range document.Fonts() {
			embedded, err := document.FileCache.Read(font.FontFile.String())
			if err != nil {
				t.Fatal(err)
			}
			face, err := fontfile.ParseSFNT(embedded, 0)
			if err != nil {
				t.Fatal(err)
			}
			if face.NumGlyphs() >= 100 {
				t.Fatalf("已使用字体没有生成小子集: name=%q glyphs=%d", font.FontName, face.NumGlyphs())
			}
		}
	}
}

func TestFlowKeepsFontReferencedOnlyByCompositeMetadata(t *testing.T) {
	opts := flow.DefaultOptions()
	opts.Fonts = append(opts.Fonts, creator.Font{Name: "CompositeFont", Data: goregular.TTF})
	d := flow.New(opts)
	d.Metadata.Composites = []creator.CompositeGraphicUnit{{ID: 100, Width: 20, Height: 10, Items: []creator.Item{
		creator.Text{Width: 20, Height: 8, Size: 4, Font: "CompositeFont", Value: "Mark"},
	}}}
	d.AddParagraph("Body")
	d.AddFixed(flow.Fixed{Width: 20, Height: 10, Items: []creator.Item{creator.Composite{Width: 20, Height: 10, ResourceID: 100}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Fonts) != 2 || model.Fonts[1].Name != "CompositeFont" {
		t.Fatal("仅由复合图元引用的字体被误删")
	}
	if _, err := creator.Marshal(*model); err != nil {
		t.Fatal(err)
	}
}
