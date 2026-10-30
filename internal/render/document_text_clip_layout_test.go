package render

import (
	"image"
	"math"
	"reflect"
	"testing"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// 固定轮廓使定位回归不依赖系统字体，同时提供可复用的共享字形路径。
type clipLayoutFace struct {
	metricsFontFace

	paths map[rune]*geom.Path
}

func (f clipLayoutFace) ToPath(value string) *geom.Path {
	var path *geom.Path
	for i, r := range []rune(value) {
		glyph := f.paths[r]
		if glyph == nil {
			continue
		}
		glyph = glyph.Copy().Translate(float64(i)*3, 0)
		if path == nil {
			path = glyph
		} else {
			path = path.Or(glyph)
		}
	}
	return path
}

func (f clipLayoutFace) DirectPath(value string) *geom.Path {
	runes := []rune(value)
	if len(runes) == 1 && fontrepair.ContainsGlyphRune(value) {
		return f.paths[runes[0]]
	}
	return nil
}

func (clipLayoutFace) Fill() geom.Paint { return geom.SolidPaint(geom.Black) }

func TestTextClipUsesExplicitDeltasDirectionsAndGlyphMapping(t *testing.T) {
	face := clipLayoutFace{paths: map[rune]*geom.Path{
		'A': geom.Rectangle(2, 4), 'B': geom.Rectangle(1, 3), fontrepair.GlyphRune(21): geom.Rectangle(3, 5),
	}}
	doc := &Document{fonts: metricsFontEngine{face: face}}
	for _, tc := range []struct {
		name      string
		code      models.TextCode
		read, dir int
		hScale    float64
		transform []models.CTCGTransform
		want      geom.Rect
	}{
		{"显式字距", models.TextCode{X: 2, Y: 10, Value: "AB", DeltaX: models.StArrayF{12}, DeltaY: models.StArrayF{3}}, 0, 0, 1, nil, geom.Rect{X0: 5, Y0: 42, X1: 18, Y1: 49}},
		{"竖向排列", models.TextCode{X: 2, Y: 10, Value: "AB"}, 90, 0, 1, nil, geom.Rect{X0: 5, Y0: 42, X1: 7, Y1: 49}},
		{"字形旋转与压缩", models.TextCode{X: 2, Y: 10, Value: "A"}, 0, 90, 0.5, nil, geom.Rect{X0: 5, Y0: 44, X1: 9, Y1: 45}},
		{"横向逐字旋转", models.TextCode{X: 2, Y: 10, Value: "AB"}, 0, 90, 1, nil, geom.Rect{X0: 5, Y0: 43, X1: 11, Y1: 45}},
		{"连字映射", models.TextCode{X: 2, Y: 10, Value: "AB"}, 0, 0, 1,
			[]models.CTCGTransform{{CodeCount: 2, GlyphCount: 1, Glyphs: models.StArrayI{21}}}, geom.Rect{X0: 5, Y0: 45, X1: 8, Y1: 50}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := models.CtText{CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 3, Y: 5, Width: 40, Height: 40}},
				Size: 4, TextCode: []models.TextCode{tc.code}, ReadDirection: tc.read, CharDirection: tc.dir, HScale: tc.hScale, CGTransform: tc.transform,
			}
			before := text
			for range 2 {
				path := doc.buildTextClipPath(&text, models.IdentityMatrix, models.StBox{}, 60)
				if path == nil || !rectNear(path.Bounds(), tc.want) {
					t.Fatalf("裁剪范围=%v，期望 %v", path, tc.want)
				}
			}
			if !reflect.DeepEqual(text, before) || face.paths['A'].Bounds() != (geom.Rect{X1: 2, Y1: 4}) {
				t.Fatal("修改了共享文字模型或字体轮廓")
			}
		})
	}
}

func TestTextClipComposesTextAndAreaTransforms(t *testing.T) {
	face := clipLayoutFace{paths: map[rune]*geom.Path{'A': geom.Rectangle(2, 4)}}
	doc := &Document{fonts: metricsFontEngine{face: face}}
	text := models.CtText{CTGraphicUnit: models.CTGraphicUnit{
		Boundary: models.StBox{X: 3, Y: 5}, CTM: &models.CTM{1, 0.2, 0.3, 1, 4, 6},
	}, Size: 4, TextCode: []models.TextCode{{X: 2, Y: 10, Value: "A"}}}
	path := doc.buildTextClipPath(&text, models.CTM{2, 0, 0, 3, 1, 2}, models.StBox{X: 10, Y: 7}, 100)
	want := geom.Rect{X0: 32.6, Y0: 25.6, X1: 39, Y1: 38.8}
	if path == nil || !rectNear(path.Bounds(), want) {
		t.Fatalf("嵌套变换裁剪范围=%v，期望 %v", path, want)
	}
}

func TestTextClipSkipsInvalidCodesWithoutShiftingGlyphReferences(t *testing.T) {
	face := clipLayoutFace{paths: map[rune]*geom.Path{
		'A': geom.Rectangle(2, 4), fontrepair.GlyphRune(21): geom.Rectangle(3, 5),
	}}
	doc := &Document{fonts: metricsFontEngine{face: face}}
	text := models.CtText{Size: 4, TextCode: []models.TextCode{
		{X: math.NaN(), Value: "A"}, {X: 10, Y: 10, Value: "AB"},
	}, CGTransform: []models.CTCGTransform{{CodePosition: 1, CodeCount: 2, GlyphCount: 1, Glyphs: models.StArrayI{21}}}}
	got := doc.buildTextClipPath(&text, models.IdentityMatrix, models.StBox{}, 40)
	if got == nil || !rectNear(got.Bounds(), geom.Rect{X0: 10, Y0: 30, X1: 13, Y1: 35}) {
		t.Fatalf("无效前缀改变了后续字形映射: %v", got)
	}
	for _, bad := range []models.CtText{
		{Size: 4, CTGraphicUnit: models.CTGraphicUnit{CTM: &models.CTM{1, 0, 0, 1, math.NaN(), 0}}},
		{Size: math.Inf(1)},
	} {
		if path := doc.buildTextClipPath(&bad, models.IdentityMatrix, models.StBox{}, 40); path != nil {
			t.Fatal("非法文字裁剪产生了路径")
		}
	}
}

func TestPatternTextOutlineSharesClipPositioning(t *testing.T) {
	face := clipLayoutFace{paths: map[rune]*geom.Path{'A': geom.Rectangle(2, 4), 'B': geom.Rectangle(1, 3)}}
	doc := &Document{fonts: metricsFontEngine{face: face}}
	object := models.TextObject{CtText: models.CtText{Size: 4, CharDirection: 90,
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 3, Y: 5}},
		TextCode:      []models.TextCode{{X: 2, Y: 10, Value: "AB", DeltaX: models.StArrayF{12}}},
	}}
	pattern := doc.patternTextOutline(object, 60, nil)
	clip := doc.buildTextClipPath(&object.CtText, models.IdentityMatrix, models.StBox{}, 60)
	if pattern == nil || clip == nil || !pattern.Equals(clip) {
		t.Fatal("底纹和裁剪没有共用相同字形轮廓")
	}
}

func rectNear(a, b geom.Rect) bool {
	return math.Abs(a.X0-b.X0) < 1e-9 && math.Abs(a.Y0-b.Y0) < 1e-9 && math.Abs(a.X1-b.X1) < 1e-9 && math.Abs(a.Y1-b.Y1) < 1e-9
}

type textClipMatrixContext struct {
	DrawContext

	matrix geom.Matrix
}

func (c textClipMatrixContext) CurrentMatrix() geom.Matrix { return c.matrix }

func TestLocalTextClipRejectsOverflowedInverse(t *testing.T) {
	ctx := textClipMatrixContext{matrix: geom.Matrix{{0.1, 0, math.MaxFloat64}, {0, 0.1, math.MaxFloat64}}}
	clip := geom.Rectangle(10, 10)
	if got := localTextClip(ctx, clip); got == nil || !got.Empty() {
		t.Fatal("逆矩阵溢出时应返回空裁剪")
	}
}

// 真实字体核对正文与裁剪像素；Clip 中 Fill/Stroke/Alpha 不决定字形区域。
func TestTextClipLayoutMatchesBodyInRasterAndPDF(t *testing.T) {
	doc := clipTestDocument(t)
	for _, tc := range []struct {
		name      string
		read, dir int
		dx, dy    models.StArrayF
	}{
		{"显式字距", 0, 0, models.StArrayF{12}, models.StArrayF{3}},
		{"竖排", 90, 0, nil, nil},
		{"逐字旋转", 0, 90, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := models.CtText{CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{Width: 40, Height: 40}},
				Size: 6, HScale: 0.8, ReadDirection: tc.read, CharDirection: tc.dir,
				TextCode: []models.TextCode{{X: 10, Y: 10, Value: "AB", DeltaX: tc.dx, DeltaY: tc.dy}},
			}
			body := clipTestSurface(t, func(ctx DrawContext) {
				doc.Text(ctx, models.TextObject{CtText: text}, nil, models.StBox{Width: 40, Height: 40})
			})
			text.Fill.Set(false)
			text.Stroke, text.Alpha = true, new(uint8(0))
			clipped := clipTestSurface(t, func(ctx DrawContext) {
				path := clipTestPath(t, `<PathObject Boundary="0 0 40 40" Fill="true" Stroke="false"><FillColor Value="0 0 0"/><AbbreviatedData>M 0 0 L 40 0 L 40 40 L 0 40 C</AbbreviatedData></PathObject>`)
				path.Clips = &models.Clips{Clip: []models.CtClip{{Area: []models.ClipArea{{Text: &text}}}}}
				doc.Path(ctx, path, nil, models.StBox{Width: 40, Height: 40})
			})
			compare := func(t *testing.T, a, b image.Image) {
				t.Helper()
				ink, mismatch := 0, 0
				for y := range 400 {
					for x := range 400 {
						ar, _, _, aa := a.At(x, y).RGBA()
						br, _, _, ba := b.At(x, y).RGBA()
						ap, bp := aa > 32767 && ar < 32767, ba > 32767 && br < 32767
						if ap || bp {
							ink++
						}
						if ap != bp {
							mismatch++
						}
					}
				}
				if ink == 0 || float64(mismatch)/float64(ink) > 0.08 {
					t.Fatalf("正文与文字裁剪不一致: ink=%d mismatch=%d", ink, mismatch)
				}
			}
			compare(t, body.Rasterize(geom.DPMM(10), nil), clipped.Rasterize(geom.DPMM(10), nil))
			t.Run("PDF", func(t *testing.T) { compare(t, clipPDFImage(t, body, 254), clipPDFImage(t, clipped, 254)) })
		})
	}
}
