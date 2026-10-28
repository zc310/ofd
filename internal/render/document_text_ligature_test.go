package render

import (
	"image/color"
	"math"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/canvas/renderers/rasterizer"
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	textlayout "github.com/zc310/ofd/internal/render/layout/text"
	"golang.org/x/image/font/gofont/goregular"
)

func TestZPageTwoLigaturePositionsFollowGlyphDeltas(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "testdata", "ofdrw", "z.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	page := ofd.Documents[0].Pages[1]
	lease, err := page.AcquireLease()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	wantX := []float64{0, 10.75, 21, 27.52, 32.26, 37.17, 41.32, 48.68, 55.29, 67.38, 74.83, 78.89, 88.38, 92.47, 96.54, 103.99, 114.23, 119.99, 124.14, 132.50, 139.95}
	found := 0
	var objects []models.TextObject
	for _, item := range lease.Content().Content.Layer[0].Items {
		if item.Text == nil || (item.Text.ID != 11056 && item.Text.ID != 11077) {
			continue
		}
		found++
		object := *item.Text
		objects = append(objects, object)
		code := object.TextCode[0]
		original := code
		original.DeltaX = append(models.StArrayF(nil), code.DeltaX...)
		original.DeltaY = append(models.StArrayF(nil), code.DeltaY...)
		if code.Value != "Heaffixedhisfinesign." {
			t.Fatalf("object %d text = %q; XML formatting must not consume a character", object.ID, code.Value)
		}
		glyphs := textlayout.CodeGlyphs(nil, []rune(code.Value), object.CGTransform, 0)
		glyphDeltas := textlayout.UsesGlyphDeltas(code, glyphs)
		x := 0.0
		for i, glyph := range glyphs {
			if i > 0 {
				dx, _ := textlayout.PositionedAdvance(0, object, code, glyphs[i-1], glyph, glyphDeltas)
				x += dx
			}
			if math.Abs(x-wantX[glyph.CodePosition]) > 1e-6 {
				t.Errorf("object %d character %d x=%g, want %g", object.ID, glyph.CodePosition, x, wantX[glyph.CodePosition])
			}
		}
		layout := buildTextLayout(nil, object, code, 0)
		for i, glyph := range layout.Glyphs {
			// 搜索框将连字内部的两个源字符等分；其他字符须与绘制位置一致。
			if object.ID == 11077 && (i == 5 || i == 13) {
				continue
			}
			if math.Abs(glyph.X-object.Boundary.X-wantX[i]) > 1e-6 {
				t.Errorf("object %d search character %d x=%g, want %g", object.ID, i, glyph.X, object.Boundary.X+wantX[i])
			}
		}
		if !reflect.DeepEqual(original, object.TextCode[0]) {
			t.Fatal("layout modified shared source deltas")
		}
	}
	if found != 2 {
		t.Fatalf("found %d English objects, want 2", found)
	}
	// 两行使用同一逻辑字体与几乎相同的定位，连字变换后应保持相同的视觉内容。
	// 比较整行墨迹，防止只修搜索坐标而实际绘制仍错位或输出 .notdef 方框。
	if err := RegisterFallbackFont(goregular.TTF, t.Name(), FontRegular); err != nil {
		t.Fatal(err)
	}
	defer UnregisterFallbackFont(t.Name())
	doc := NewDocument(color.White, ofd.Documents[0])
	defer doc.Close()
	if err := doc.UseFallbackFont(t.Name()); err != nil {
		t.Fatal(err)
	}
	var masks [][]bool
	for _, object := range objects {
		object.Boundary.X, object.Boundary.Y = 0, 0
		c := canvas.New(150, 20)
		ctx := canvas.NewContext(c)
		ctx.SetFillColor(color.White)
		ctx.DrawPath(0, 0, canvas.Rectangle(150, 20))
		doc.Text(newCanvasBackend(ctx), object, nil, models.StBox{Width: 150, Height: 20})
		img := rasterizer.Draw(c, canvas.DPI(96), canvas.DefaultColorSpace)
		mask := make([]bool, img.Bounds().Dx()*img.Bounds().Dy())
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				r, _, _, _ := img.At(x, y).RGBA()
				mask[y*img.Bounds().Dx()+x] = r < 0x8000
			}
		}
		masks = append(masks, mask)
	}
	different, ink := 0, 0
	for i, value := range masks[0] {
		if value || masks[1][i] {
			ink++
		}
		if value != masks[1][i] {
			different++
		}
	}
	if ink < 200 || float64(different)/float64(ink) > 0.3 {
		t.Fatalf("ligature line differs from source: %d changed / %d ink pixels", different, ink)
	}
}

func TestLigatureUsesSourceTextWhenSubstituteFontHasNoGlyphMapping(t *testing.T) {
	transform := []models.CTCGTransform{{CodeCount: 2, GlyphCount: 1, Glyphs: models.StArrayI{191}}}
	face := testGlyphIndexFace{glyphs: map[rune]uint16{'f': 1, 'i': 2, 'A': 3}}
	glyphs := textlayout.CodeGlyphs(face, []rune("fiA"), transform, 0)
	if len(glyphs) != 2 || glyphs[0].Value != "fi" || glyphs[0].Semantic != "" || glyphs[1].GlyphPosition != 1 {
		t.Fatalf("substitute glyphs = %+v, want source fi followed by A", glyphs)
	}
	face.glyphs[fontrepair.GlyphRune(191)] = 191
	glyphs = textlayout.CodeGlyphs(face, []rune("fiA"), transform, 0)
	if len(glyphs) != 2 || glyphs[0].Value != string(fontrepair.GlyphRune(191)) || glyphs[0].Semantic != "fi" {
		t.Fatalf("embedded glyphs = %+v, want original mapped ligature and semantic fi", glyphs)
	}
}

func TestGlyphDeltasSupportExpandedGlyphsAndCharacterDeltas(t *testing.T) {
	transforms := []models.CTCGTransform{{CodeCount: 1, GlyphCount: 2, Glyphs: models.StArrayI{1, 2}}}
	code := models.TextCode{Value: "ab", DeltaX: models.StArrayF{2, 3}, DeltaY: models.StArrayF{1, -1}}
	glyphs := textlayout.CodeGlyphs(nil, []rune(code.Value), transforms, 0)
	if !textlayout.UsesGlyphDeltas(code, glyphs) {
		t.Fatal("expected three glyph positions for two characters")
	}
	x, y := textlayout.PositionedAdvance(10, models.TextObject{}, code, glyphs[0], glyphs[1], true)
	if x != 2 || y != 1 {
		t.Fatalf("second glyph at (%g,%g), want (2,1)", x, y)
	}
	layout := buildTextLayout(nil, models.TextObject{CtText: models.CtText{CGTransform: transforms}}, code, 0)
	if len(layout.Glyphs) != 2 || layout.Glyphs[1].X != 5 || layout.Glyphs[1].Y != 0 {
		t.Fatalf("character positions = %+v, want second character at (5,0)", layout.Glyphs)
	}
	// 保留已有的变换前字符增量表示：fi 两个码位的步进都要消费。
	code = models.TextCode{Value: "fiA", DeltaX: models.StArrayF{3, 4}}
	glyphs = textlayout.CodeGlyphs(nil, []rune(code.Value), []models.CTCGTransform{{CodeCount: 2, GlyphCount: 1, Glyphs: models.StArrayI{191}}}, 0)
	if textlayout.UsesGlyphDeltas(code, glyphs) {
		t.Fatal("source character deltas were mistaken for glyph deltas")
	}
	x, _ = textlayout.PositionedAdvance(10, models.TextObject{}, code, glyphs[0], glyphs[1], false)
	if x != 7 {
		t.Fatalf("character delta advance = %g, want 7", x)
	}
}

func TestCombiningMarkUsesMappedGlyphWithoutStandaloneShaping(t *testing.T) {
	const mark = '\u0bcb'
	const gid = 97
	face := testGlyphIndexFace{glyphs: map[rune]uint16{mark: gid, fontrepair.GlyphRune(gid): gid}}
	glyphs := textlayout.CodeGlyphs(face, []rune{mark}, []models.CTCGTransform{{CodeCount: 1, GlyphCount: 1, Glyphs: models.StArrayI{gid}}}, 0)
	if len(glyphs) != 1 || glyphs[0].Value != string(fontrepair.GlyphRune(gid)) || glyphs[0].Semantic != string(mark) {
		t.Fatalf("combining vowel = %+v, want original outline and source Unicode", glyphs)
	}
}

func TestZPagesThreeAndFourUseExplicitExpandedGlyphPositions(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "testdata", "ofdrw", "z.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	for _, tc := range []struct {
		page int
		id   models.StID
		text string
		gids []int
		xs   []float64
	}{
		{2, 13, "ฤำ", []int{0, 1148, 1125}, []float64{0, 8.79, 9.58}},
		{3, 32, "போ", []int{94, 76, 88}, []float64{0, 10.92, 21.15}},
	} {
		lease, err := ofd.Documents[0].Pages[tc.page].AcquireLease()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range lease.Content().Content.Layer[0].Items {
			if item.Text == nil || item.Text.ID != tc.id {
				continue
			}
			found = true
			code := item.Text.TextCode[0]
			if code.Value != tc.text {
				t.Fatalf("object %d source = %q, want %q", tc.id, code.Value, tc.text)
			}
			glyphs := textlayout.CodeGlyphs(nil, []rune(code.Value), item.Text.CGTransform, 0)
			if len(glyphs) != len(tc.gids) || !textlayout.UsesGlyphDeltas(code, glyphs) {
				t.Fatalf("object %d glyphs = %+v, want expanded glyph positioning", tc.id, glyphs)
			}
			x := 0.0
			for i, glyph := range glyphs {
				if i > 0 {
					dx, _ := textlayout.PositionedAdvance(0, *item.Text, code, glyphs[i-1], glyph, true)
					x += dx
				}
				if math.Abs(x-tc.xs[i]) > 1e-9 || tc.gids[i] != 0 && glyph.Value != string(fontrepair.GlyphRune(uint16(tc.gids[i]))) {
					t.Errorf("object %d glyph %d = %+v at %g, want GID %d at %g", tc.id, i, glyph, x, tc.gids[i], tc.xs[i])
				}
			}
		}
		lease.Release()
		if !found {
			t.Fatalf("object %d missing", tc.id)
		}
	}
}
