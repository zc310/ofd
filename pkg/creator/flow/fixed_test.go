package flow_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/color"
	"image/png"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/converter"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
)

func fixedRect(x, y, width, height float64) creator.Path {
	stroke := false
	return creator.Path{X: x, Y: y, Width: width, Height: height, Data: "M 0 0 L 10 0 L 10 10 L 0 10 C", Fill: true, StrokeSet: &stroke, FillColor: &creator.Color{R: 20, G: 100, B: 200}, CTM: &creator.CTM{width / 10, 0, 0, height / 10, 0, 0}}
}

func TestFixedBlockMovesWithCaptionAndKeepsLocalTransforms(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddParagraph(strings.Repeat("Prefix\n", 7))
	path := fixedRect(2, 3, 20, 10)
	flag := false
	path.Clips = &creator.Clips{TransFlag: &flag, Items: []creator.Clip{{Areas: []creator.ClipArea{{Path: &creator.ClipPath{Boundary: creator.Box{Width: 20, Height: 10}, Data: "M 0 0 L 20 0 L 20 10 L 0 10 C", Fill: true}}}}}}
	path.Actions = []creator.Action{{Event: creator.ActionEventClick, URI: &creator.URIAction{URI: "https://example.com"}}}
	nested := creator.PageBlock{Items: []creator.Item{creator.Text{X: 3, Y: 16, Width: 30, Height: 6, Size: 3.5, Value: "Signed"}}}
	fixed := flow.Fixed{Width: 50, Height: 35, Align: "center", Items: []creator.Item{path, nested}, Caption: "Signature area", Numbered: true}
	d.Blocks = append(d.Blocks, flow.Block{ID: "signature", Kind: "fixed", Fixed: &fixed})
	d.AddParagraph("Following text")
	d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{CaptionRef: "signature"}, {Text: ", page "}, {PageRef: "signature"}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	anchor := model.Bookmarks[0].Goto
	if anchor.Page != 1 || anchor.Top == nil || math.Abs(*anchor.Top-10) > 0.001 {
		t.Fatalf("固定块锚点未随分页移动: %+v", anchor)
	}
	var placed creator.PageBlock
	for _, item := range model.Pages[anchor.Page].Items {
		if p, ok := item.(creator.PageBlock); ok {
			placed = p
		}
		if text, ok := item.(creator.Text); ok && textValue(text) == "Following text" && text.Y < 45 {
			t.Fatalf("后续正文占用了预留空间: %+v", text)
		}
	}
	if len(placed.Items) != 2 {
		t.Fatal("固定区域图元丢失")
	}
	got := placed.Items[0].(creator.Path)
	if got.X != 27 || got.Y != 13 || !reflect.DeepEqual(got.CTM, path.CTM) || !reflect.DeepEqual(got.Clips, path.Clips) || !reflect.DeepEqual(got.Actions, path.Actions) || got.Data != path.Data {
		t.Fatalf("平移破坏局部变换、裁剪或动作: %+v", got)
	}
	text := placed.Items[1].(creator.PageBlock).Items[0].(creator.Text)
	if text.X != 28 || text.Y != 26 || text.Font != "Flow-Regular" {
		t.Fatalf("嵌套坐标或继承字体错误: %+v", text)
	}
	if !reflect.DeepEqual(fixed.Items[0], path) || fixed.Items[1].(creator.PageBlock).Items[0].(creator.Text).Font != "" || fixed.Caption != "Signature area" {
		t.Fatal("排版修改了原始图元")
	}
	var first, second bytes.Buffer
	for _, out := range []*bytes.Buffer{&first, &second} {
		if err := d.Write(context.Background(), out, creator.CreateOptions{Deterministic: true}); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("固定图元未保持确定性输出")
	}
	v, err := validator.New()
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(context.Background(), bytes.NewReader(first.Bytes()), "fixed.ofd")
	if report.HasErrors() || report.HasWarnings() {
		t.Fatalf("固定图元严格校验失败: %+v", report.Issues)
	}
}

func TestFixedBlocksHonorSectionWidthsAndHorizontalAlignment(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddFixed(flow.Fixed{Height: 10, Items: []creator.Item{fixedRect(0, 0, 80, 10)}})
	d.AddSection(flow.Section{PageSize: &creator.PageSize{Width: 140, Height: 90}})
	d.AddFixed(flow.Fixed{Width: 30, Height: 10, Align: "right", Items: []creator.Item{fixedRect(0, 0, 30, 10)}})
	d.AddFixed(flow.Fixed{Height: 10, Items: []creator.Item{fixedRect(0, 0, 120, 10)}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) != 2 {
		t.Fatalf("分节页数错误: %d", len(model.Pages))
	}
	var xs []float64
	for _, item := range model.Pages[1].Items {
		if p, ok := item.(creator.PageBlock); ok {
			xs = append(xs, p.Items[0].(creator.Path).X)
		}
	}
	if !reflect.DeepEqual(xs, []float64{100, 10}) {
		t.Fatalf("分节后的右对齐或默认宽度错误: %v", xs)
	}
}

func TestFixedImageAndCompositePreserveResourceReferences(t *testing.T) {
	d := flow.New(smallOptions())
	d.Metadata.Composites = []creator.CompositeGraphicUnit{{ID: 100, Width: 10, Height: 10, Items: []creator.Item{fixedRect(0, 0, 10, 10)}}}
	image, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGPgDr8AAAGjATO977V+AAAAAElFTkSuQmCC")
	if err != nil {
		t.Fatal(err)
	}
	d.Metadata.Media = []creator.Media{{ID: 200, Type: "Image", Format: "PNG", Name: "shared.png", Data: image}}
	d.AddFixed(flow.Fixed{Width: 50, Height: 20, Items: []creator.Item{
		creator.Image{X: 2, Y: 2, Width: 10, Height: 10, Data: image, Format: "PNG"},
		creator.Composite{X: 20, Y: 2, Width: 10, Height: 10, ResourceID: 100},
		creator.Image{X: 36, Y: 2, Width: 10, Height: 10, ResourceID: 200},
	}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	group := model.Pages[0].Items[0].(creator.PageBlock)
	img, composite := group.Items[0].(creator.Image), group.Items[1].(creator.Composite)
	if img.X != 12 || img.Y != 12 || !bytes.Equal(img.Data, image) || composite.X != 30 || composite.Y != 12 || composite.ResourceID != 100 {
		t.Fatal("图像数据或复合资源引用没有完整传递")
	}
	if group.Items[2].(creator.Image).ResourceID != 200 || len(model.Media) != 1 {
		t.Fatal("共享图片资源没有保留")
	}
	var out bytes.Buffer
	if err := d.Write(context.Background(), &out, creator.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestHeadingFollowsFixedBlockAndExplicitBreakDoesNotAddTrailingPage(t *testing.T) {
	d := flow.New(smallOptions())
	d.AddParagraph(strings.Repeat("Prefix\n", 7))
	d.Blocks = append(d.Blocks, flow.Block{ID: "heading", Kind: "heading", Level: 2, Text: "Approval"})
	d.Blocks = append(d.Blocks, flow.Block{ID: "fixed", Kind: "fixed", PageBreakAfter: true, Fixed: &flow.Fixed{Height: 20, Items: []creator.Item{fixedRect(0, 0, 10, 10)}}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Pages) != 2 || len(model.Bookmarks) != 2 || model.Bookmarks[0].Goto.Page != 1 || model.Bookmarks[1].Goto.Page != 1 {
		t.Fatalf("标题、固定块跟随或结尾换页错误: pages=%d bookmarks=%+v", len(model.Pages), model.Bookmarks)
	}
}

func TestFixedFiguresShareNumberingWithImages(t *testing.T) {
	d := flow.New(smallOptions())
	image, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGPgDr8AAAGjATO977V+AAAAAElFTkSuQmCC")
	if err != nil {
		t.Fatal(err)
	}
	d.AddImage(flow.Image{Data: image, Width: 3, Numbered: true})
	d.AddFixed(flow.Fixed{Height: 5, Items: []creator.Item{fixedRect(0, 0, 5, 5)}, Numbered: true, Caption: "Chart"})
	d.AddImage(flow.Image{Data: image, Width: 3, Numbered: true})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	text := ""
	for _, page := range model.Pages {
		text += pageText(page)
	}
	for _, want := range []string{"Figure 1", "Figure 2  Chart", "Figure 3"} {
		if !strings.Contains(text, want) {
			t.Fatalf("固定块未接入图序号: %q", text)
		}
	}
}

// Value 简写的下划线必须可见，同时显式 TextCode 的基线保持原值。
func TestFixedTextValueRendersDescendersAndPreservesExplicitBaseline(t *testing.T) {
	d := flow.New(smallOptions())
	x, y := 0.0, 7.0
	d.AddFixed(flow.Fixed{Height: 25, Items: []creator.Item{
		creator.Text{Width: 30, Height: 10, Size: 6, Value: "___"},
		creator.Text{Y: 12, Width: 30, Height: 10, Size: 6, TextCodes: []creator.TextCode{{Value: "___", X: &x, Y: &y}}},
	}})
	model, err := d.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	group := model.Pages[0].Items[0].(creator.PageBlock)
	if *group.Items[1].(creator.Text).TextCodes[0].Y != 7 || y != 7 {
		t.Fatal("显式基线被修改")
	}
	var output, raster bytes.Buffer
	if err := d.Write(context.Background(), &output, creator.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := converter.Convert(context.Background(), "ofd", "png", output.Bytes(), &raster, converter.Page(1), converter.DPI(144), converter.BgColor(color.White)); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raster.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	scale := 144.0 / 25.4
	for _, top := range []float64{10, 22} {
		dark := 0
		for py := int(top * scale); py < int((top+10)*scale); py++ {
			for px := int(10 * scale); px < int(40*scale); px++ {
				r, g, b, _ := img.At(px, py).RGBA()
				if r < 0x8000 && g < 0x8000 && b < 0x8000 {
					dark++
				}
			}
		}
		if dark < 30 {
			t.Fatalf("第 %gmm 处的下划线被裁掉，可见像素=%d", top, dark)
		}
	}
}

func TestFixedBlocksRejectInvalidGeometryContentAndNesting(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*flow.Fixed)
		want   string
	}{
		{"negative", func(f *flow.Fixed) { f.Width = -1 }, "有限非负数"},
		{"infinite", func(f *flow.Fixed) { f.Height = math.Inf(1) }, "有限非负数"},
		{"missing height", func(f *flow.Fixed) { f.Height = 0 }, "必须为正数"},
		{"too wide", func(f *flow.Fixed) { f.Width = 81 }, "超出版心"},
		{"too tall", func(f *flow.Fixed) { f.Height = 71 }, "整页版心"},
		{"bad align", func(f *flow.Fixed) { f.Align = "justify" }, "align"},
		{"no items", func(f *flow.Fixed) { f.Items = nil }, "不能为空"},
		{"nil item", func(f *flow.Fixed) { f.Items = []creator.Item{nil} }, "不支持"},
		{"negative box", func(f *flow.Fixed) { f.Items = []creator.Item{fixedRect(-1, 0, 10, 10)} }, "边界"},
		{"overflow", func(f *flow.Fixed) { f.Items = []creator.Item{fixedRect(19, 0, 10, 10)} }, "位于固定区域"},
		{"nan box", func(f *flow.Fixed) { f.Items = []creator.Item{fixedRect(math.NaN(), 0, 10, 10)} }, "边界"},
		{"nan transform", func(f *flow.Fixed) { p := fixedRect(0, 0, 10, 10); p.CTM[0] = math.NaN(); f.Items = []creator.Item{p} }, "变换"},
		{"invalid path", func(f *flow.Fixed) { p := fixedRect(0, 0, 10, 10); p.Data = "Z bad"; f.Items = []creator.Item{p} }, "data"},
		{"font", func(f *flow.Fixed) {
			f.Items = []creator.Item{creator.Text{Width: 10, Height: 5, Size: 3, Font: "missing", Value: "X"}}
		}, "未注册"},
		{"size", func(f *flow.Fixed) { f.Items = []creator.Item{creator.Text{Width: 10, Height: 5, Value: "X"}} }, "字号"},
		{"text height", func(f *flow.Fixed) { f.Items = []creator.Item{creator.Text{Width: 10, Height: 1, Size: 6, Value: "Q"}} }, "文字高度不足"},
		{"invalid text", func(f *flow.Fixed) {
			f.Items = []creator.Item{creator.Text{Width: 10, Height: 5, Size: 3, Value: "bad\x00"}}
		}, "控制字符"},
		{"paginating item", func(f *flow.Fixed) { f.Items = []creator.Item{creator.Table{Width: 10}} }, "不支持"},
		{"cycle", func(f *flow.Fixed) {
			items := make([]creator.Item, 1)
			items[0] = creator.PageBlock{Items: items}
			f.Items = items
		}, "嵌套超过"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixed := flow.Fixed{Width: 20, Height: 20, Items: []creator.Item{fixedRect(0, 0, 10, 10)}}
			test.change(&fixed)
			d := flow.New(smallOptions())
			d.AddFixed(fixed)
			_, err := d.Build(context.Background())
			if err == nil || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "blocks[0]") {
				t.Fatalf("错误=%v，期望包含 %q 及块下标", err, test.want)
			}
		})
	}
	for _, block := range []flow.Block{{Kind: "fixed"}, {Kind: "paragraph", Fixed: &flow.Fixed{}}, {Kind: "fixed", Text: "bad", Fixed: &flow.Fixed{}}, {Kind: "section", Section: &flow.Section{}, Fixed: &flow.Fixed{}}, {Kind: "toc", Fixed: &flow.Fixed{}}} {
		d := flow.New()
		d.Blocks = []flow.Block{block}
		if _, err := d.Build(context.Background()); err == nil {
			t.Fatalf("未拒绝内容类型冲突: %+v", block)
		}
	}
}
