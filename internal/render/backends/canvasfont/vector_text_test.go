package canvasfont

import (
	"bytes"
	"encoding/base64"
	"regexp"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/canvas/renderers/svg"
	"github.com/zc310/ofd/internal/fontrepair"
	"golang.org/x/image/font/gofont/goregular"
)

func TestDirectTextPathHandlesHighestGlyphAddresses(t *testing.T) {
	base := canvas.NewFontFamily("glyph address source")
	if err := base.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	glyph := base.Face(12, canvas.Black).Font.GlyphIndex('A')
	fonts := NewFonts(nil)
	defer fonts.Close()
	for _, address := range []uint16{0xfffe, 0xffff} {
		r := fontrepair.GlyphRune(address)
		family, err := loadCachedEmbeddedFont(fonts, "high glyph address", goregular.TTF, FontRegular,
			[]fontrepair.GlyphMapping{{Rune: r, Glyph: glyph}})
		if err != nil {
			t.Fatal(err)
		}
		face := family.Face(12, canvas.Black)
		if face.Font.GlyphIndex(r) == 0 {
			t.Fatalf("测试字体缺少内部地址 %U", r)
		}
		if path := fonts.directTextPath(face, string(r)); path == nil || path.Empty() {
			t.Errorf("内部地址 %U 未通过字形路径绘制", r)
		}
	}
}

// 同一资源按不同文档映射修复后，需要独立 CSS 族名；否则浏览器会选中错误 cmap。
func TestSVGSeparatesSameNameEmbeddedGlyphMappings(t *testing.T) {
	fonts := NewFonts(nil)
	defer fonts.Close()
	page := canvas.New(80, 30)
	ctx := canvas.NewContext(page)
	for i, mapping := range [][]fontrepair.GlyphMapping{
		{{Rune: 'A', Glyph: 1}}, {{Rune: 'A', Glyph: 2}},
	} {
		family, err := loadCachedEmbeddedFont(fonts, "SameName", goregular.TTF, FontRegular, mapping)
		if err != nil {
			t.Fatal(err)
		}
		ctx.DrawText(5+float64(i)*30, 15, canvas.NewTextLine(family.Face(12, canvas.Black), "A", canvas.Left))
	}
	var output bytes.Buffer
	renderer := svg.New(&output, page.W, page.H, nil)
	page.RenderTo(renderer)
	if err := renderer.Close(); err != nil {
		t.Fatal(err)
	}
	programs := make(map[string][]byte)
	pattern := regexp.MustCompile(`@font-face\{font-family:'([^']+)'[^}]*base64,([^']+)`)
	for _, match := range pattern.FindAllSubmatch(output.Bytes(), -1) {
		name := string(match[1])
		data, err := base64.StdEncoding.DecodeString(string(match[2]))
		if err != nil {
			t.Fatal(err)
		}
		if previous, exists := programs[name]; exists && !bytes.Equal(previous, data) {
			t.Errorf("SVG 中的同名字体 %q 指向不同子集数据", name)
		}
		programs[name] = data
	}
	if count := len(programs); count != 2 {
		t.Fatalf("SVG 含 %d 个独立字体，应为 2 个", count)
	}
	if count := bytes.Count(output.Bytes(), []byte("<text")); count != 2 {
		t.Fatalf("应保留两段原生文字，实际 %d 段", count)
	}
}
