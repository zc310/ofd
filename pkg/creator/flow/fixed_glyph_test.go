package flow_test

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

func fixedTestGlyph(t *testing.T, r rune) int {
	t.Helper()
	face, err := sfnt.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	var buffer sfnt.Buffer
	glyph, err := face.GlyphIndex(&buffer, r)
	if err != nil || glyph == 0 {
		t.Fatalf("测试字形不存在: %q glyph=%d err=%v", r, glyph, err)
	}
	return int(glyph)
}

func TestFixedTextRejectsUnmappedMissingGlyphs(t *testing.T) {
	glyph := fixedTestGlyph(t, 'A')
	for _, test := range []struct {
		name string
		text creator.Text
		want string
	}{
		{"value", creator.Text{Value: "中文"}, "'中'"},
		{"explicit font", creator.Text{Font: "Flow-Bold", Value: "中文"}, "'中'"},
		{"text codes", creator.Text{TextCodes: []creator.TextCode{{Value: "A"}, {Value: "中文"}}}, "'中'"},
		{"partial mapping", creator.Text{Value: "中文", CGTransforms: []creator.CGTransform{{Glyphs: []int{glyph}}}}, "'文'"},
		{"partial text codes", creator.Text{TextCodes: []creator.TextCode{{Value: "中"}, {Value: "文"}}, CGTransforms: []creator.CGTransform{{Glyphs: []int{glyph}}}}, "'文'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			text := test.text
			text.Width, text.Height, text.Size = 40, 8, 4
			d := flow.New(smallOptions())
			// 嵌套容器也必须检查，并且错误需保留完整的块与图元位置。
			d.AddFixed(flow.Fixed{Width: 50, Height: 10, Items: []creator.Item{creator.PageBlock{Items: []creator.Item{text}}}})
			_, err := d.Build(context.Background())
			if err == nil || !strings.Contains(err.Error(), "缺少字符") || !strings.Contains(err.Error(), test.want) || !strings.Contains(err.Error(), "blocks[0]") || !strings.Contains(err.Error(), "items[0]") {
				t.Fatalf("缺字错误未定位到原始文字: %v", err)
			}
		})
	}
}

// 显式 GID 可以表示 cmap 中没有的字符；默认数量为 1，位置按所有 TextCode 的字符累计。
func TestFixedTextPreservesExplicitGlyphMappingsAndPositions(t *testing.T) {
	glyph := fixedTestGlyph(t, 'A')
	x, y := 1.0, 6.0
	for _, test := range []struct {
		name string
		text creator.Text
	}{
		{"unicode", creator.Text{Value: "ABC"}},
		{"default counts", creator.Text{Value: "中", CGTransforms: []creator.CGTransform{{Glyphs: []int{glyph}}}}},
		{"many to one", creator.Text{Value: "中文", CGTransforms: []creator.CGTransform{{CodeCount: 2, GlyphCount: 1, Glyphs: []int{glyph}}}}},
		{"one to many", creator.Text{Value: "中", CGTransforms: []creator.CGTransform{{CodeCount: 1, GlyphCount: 2, Glyphs: []int{glyph, glyph}}}}},
		{"unmapped unicode", creator.Text{Value: "中A", CGTransforms: []creator.CGTransform{{Glyphs: []int{glyph}}}}},
		{"explicit text codes", creator.Text{TextCodes: []creator.TextCode{{Value: "A", X: &x, Y: &y}, {Value: "中"}}, CGTransforms: []creator.CGTransform{{CodePosition: 1, Glyphs: []int{glyph}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			text := test.text
			text.Width, text.Height, text.Size = 40, 8, 4
			d := flow.New(smallOptions())
			d.AddFixed(flow.Fixed{Width: 50, Height: 10, Items: []creator.Item{text}})
			model, err := d.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			placed := model.Pages[0].Items[0].(creator.PageBlock).Items[0].(creator.Text)
			if !reflect.DeepEqual(placed.CGTransforms, text.CGTransforms) || len(text.TextCodes) > 0 && !reflect.DeepEqual(placed.TextCodes, text.TextCodes) || !reflect.DeepEqual(d.Blocks[0].Fixed.Items[0], text) {
				t.Fatal("原始文字、显式映射或基线被修改")
			}
			var output bytes.Buffer
			if err := d.Write(context.Background(), &output, creator.CreateOptions{Deterministic: true}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFixedTextRejectsInvalidGlyphMappingCoverage(t *testing.T) {
	glyph := fixedTestGlyph(t, 'A')
	for _, transform := range []creator.CGTransform{
		{CodePosition: -1, Glyphs: []int{glyph}},
		{CodePosition: 1, Glyphs: []int{glyph}},
		{CodeCount: -1, Glyphs: []int{glyph}},
		{CodeCount: 2, Glyphs: []int{glyph}},
		{GlyphCount: -1, Glyphs: []int{glyph}},
		{GlyphCount: 2, Glyphs: []int{glyph}},
		{},
		{Glyphs: []int{-1}},
		{Glyphs: []int{65535}},
	} {
		d := flow.New(smallOptions())
		d.AddFixed(flow.Fixed{Width: 50, Height: 10, Items: []creator.Item{
			creator.Text{Width: 40, Height: 8, Size: 4, Value: "中", CGTransforms: []creator.CGTransform{transform}},
		}})
		if _, err := d.Build(context.Background()); err == nil || !strings.Contains(err.Error(), "CGTransform[0]") {
			t.Fatalf("非法映射不能绕过检查: transform=%+v err=%v", transform, err)
		}
	}
}
