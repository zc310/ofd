package render

import (
	"image"
	"math"
	"testing"

	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/pkg/creator"
)

func TestTextHScaleDefaultsToOne(t *testing.T) {
	if got := textHScale(models.TextObject{}); got != 1 {
		t.Fatalf("expected default horizontal scale 1, got %v", got)
	}
	if got := textHScale(models.TextObject{CtText: models.CtText{HScale: 0.5}}); got != 0.5 {
		t.Fatalf("expected horizontal scale 0.5, got %v", got)
	}
}

func TestTextFillDisabled(t *testing.T) {
	if !textFillDisabled(models.TextObject{CtText: models.CtText{Fill: models.NewOptionalBool(false)}}) {
		t.Fatal("expected Fill=false to disable text fill")
	}
	if textFillDisabled(models.TextObject{CtText: models.CtText{Fill: models.NewOptionalBool(true)}}) {
		t.Fatal("expected Fill=true to keep text fill enabled")
	}
	if textFillDisabled(models.TextObject{}) {
		t.Fatal("expected missing Fill to keep text fill enabled")
	}
}

func TestNormalizeTextDirection(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
		{0, 0},
		{90, 90},
		{180, 180},
		{270, 270},
		{360, 0},
		{-90, 270},
		{44, 0},
		{45, 90},
		{315, 0},
	}
	for _, test := range tests {
		if got := normalizeTextDirection(test.input); got != test.want {
			t.Errorf("normalizeTextDirection(%d) = %d, want %d", test.input, got, test.want)
		}
	}
}

func TestTextReadAdvance(t *testing.T) {
	for _, test := range []struct {
		direction int
		wantX     float64
		wantY     float64
	}{
		{0, 3, 0},
		{90, 0, 3},
		{180, -3, 0},
		{270, 0, -3},
	} {
		gotX, gotY := textReadAdvance(3, test.direction)
		if gotX != test.wantX || gotY != test.wantY {
			t.Errorf("textReadAdvance(%d) = (%v, %v), want (%v, %v)", test.direction, gotX, gotY, test.wantX, test.wantY)
		}
	}
}

func TestTextAdvanceDiffersDetectsLineBreaks(t *testing.T) {
	// 正常水平步进不触发断串（否则中文会被逐字拆开、提取时插入空格）。
	if textAdvanceDiffers(3.175, 0, 3.175) {
		t.Fatal("horizontal advance should not break the run")
	}
	// 小负值字距不应断串。
	if textAdvanceDiffers(-0.1, 0, 3.175) {
		t.Fatal("small negative kerning should not break the run")
	}
	// 纵向位移（换行/基线调整）必须断串，否则多行文字会被合并成一行。
	if !textAdvanceDiffers(0, 4.5, 3.175) {
		t.Fatal("vertical advance should break the run")
	}
	// 横向明显回退（换行回到行首）也必须断串。
	if !textAdvanceDiffers(-67.5, 0, 2.5) {
		t.Fatal("backward advance should break the run")
	}
	// 前进步进与字体自然步进相差过大（子集字体 hmtx 占位全角）必须断串，
	// 否则原生文本串会按字体字宽排版、忽略 DeltaX，字距错误。
	if !textAdvanceDiffers(1.8486, 0, 3.4234) {
		t.Fatal("explicit advance far from natural advance should break the run")
	}
	// 前进步进接近自然步进的微小差异（正常字距）不应断串。
	if textAdvanceDiffers(3.175, 0, 3.2) {
		t.Fatal("advance close to natural should not break the run")
	}
}

func TestTextCharDirectionDegrees(t *testing.T) {
	for _, test := range []struct {
		direction int
		want      float64
	}{
		{0, 0},
		{90, 90},
		{180, 180},
		{270, 270},
		{-90, 270},
		{450, 90},
	} {
		object := models.TextObject{CtText: models.CtText{CharDirection: test.direction}}
		if got := textCharDirectionDegrees(object); got != test.want {
			t.Errorf("textCharDirectionDegrees(%d) = %v, want %v", test.direction, got, test.want)
		}
	}
}

func TestTextAdvanceUsesExplicitDeltasBeforeReadDirection(t *testing.T) {
	object := models.TextObject{CtText: models.CtText{ReadDirection: 90}}
	code := models.TextCode{DeltaX: models.StArrayF{2}, DeltaY: models.StArrayF{4}}
	gotX, gotY := textAdvance(10, object, code, 0)
	if gotX != 2 || gotY != 4 {
		t.Fatalf("textAdvance with explicit deltas = (%v, %v), want (2, 4)", gotX, gotY)
	}

	code = models.TextCode{}
	gotX, gotY = textAdvance(10, object, code, 0)
	if gotX != 0 || gotY != 10 {
		t.Fatalf("textAdvance with ReadDirection=90 = (%v, %v), want (0, 10)", gotX, gotY)
	}
}

func TestBuildTextLayoutUsesFallbackGlyphWidthsAndDirections(t *testing.T) {
	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 10, Y: 20, Width: 12, Height: 4}},
		Size:          4,
		HScale:        0.5,
		ReadDirection: 90,
	}}
	layout := buildTextLayout(nil, object, models.TextCode{Value: "ab", X: 1, Y: 4}, 0)
	if len(layout.Glyphs) != 2 {
		t.Fatalf("glyph count = %d, want 2", len(layout.Glyphs))
	}
	if layout.Glyphs[0].X != 11 || layout.Glyphs[0].Y != 20 {
		t.Fatalf("first glyph = %+v, want x=11 y=20", layout.Glyphs[0])
	}
	if layout.Glyphs[0].Width != 3 || layout.Glyphs[0].Height != 4 {
		t.Fatalf("first glyph size = %.2fx%.2f, want 3x4", layout.Glyphs[0].Width, layout.Glyphs[0].Height)
	}
	if layout.Glyphs[1].X != 11 || layout.Glyphs[1].Y != 23 {
		t.Fatalf("second glyph = %+v, want x=11 y=23", layout.Glyphs[1])
	}
}

func TestBuildTextLayoutUsesExplicitDeltas(t *testing.T) {
	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{Boundary: models.StBox{X: 10, Y: 20, Width: 12, Height: 4}},
		Size:          4,
		ReadDirection: 90,
	}}
	layout := buildTextLayout(nil, object, models.TextCode{
		Value:  "ab",
		X:      1,
		Y:      4,
		DeltaX: models.StArrayF{2},
		DeltaY: models.StArrayF{3},
	}, 0)
	if len(layout.Glyphs) != 2 {
		t.Fatalf("glyph count = %d, want 2", len(layout.Glyphs))
	}
	if layout.Glyphs[1].X != 13 || layout.Glyphs[1].Y != 23 {
		t.Fatalf("second glyph = %+v, want x=13 y=23", layout.Glyphs[1])
	}
}

func TestBuildTextLayoutAppliesCTMScaleToGlyphGeometry(t *testing.T) {
	ctm := models.CTM{2, 0, 0, 3, 0, 0}
	object := models.TextObject{CtText: models.CtText{
		CTGraphicUnit: models.CTGraphicUnit{
			Boundary: models.StBox{X: 10, Y: 20, Width: 12, Height: 4},
			CTM:      &ctm,
		},
		Size: 4,
	}}
	layout := buildTextLayout(nil, object, models.TextCode{Value: "ab", X: 1, Y: 4}, 0)
	if len(layout.Glyphs) != 2 {
		t.Fatalf("glyph count = %d, want 2", len(layout.Glyphs))
	}
	if layout.Glyphs[0].X != 12 || layout.Glyphs[0].Y != 20 || layout.Glyphs[0].Width != 6 || layout.Glyphs[0].Height != 12 {
		t.Fatalf("first glyph = %+v, want x=12 y=20 width=6 height=12", layout.Glyphs[0])
	}
	if layout.Glyphs[1].X != 24 || layout.Glyphs[1].Y != 20 {
		t.Fatalf("second glyph = %+v, want x=24 y=20", layout.Glyphs[1])
	}
}

func TestApplyCGTransformWidthsUsesMappedGlyphs(t *testing.T) {
	widths := []float64{2, 3, 4}
	runes := []rune("abc")
	widthOf := func(value string) float64 {
		if value == string(fontfix.GlyphRune(65)) {
			return 9
		}
		if value == string(fontfix.GlyphRune(66)) {
			return 6
		}
		return 0
	}
	applyCGTransformWidths(widths, runes, []models.CTCGTransform{{
		CodePosition: 1,
		CodeCount:    2,
		GlyphCount:   2,
		Glyphs:       models.StArrayI{65, 66},
	}}, 0, widthOf)
	if widths[0] != 2 || widths[1] != 9 || widths[2] != 6 {
		t.Fatalf("mapped widths = %v, want [2 9 6]", widths)
	}
}

func TestTextCodeGlyphsClampsExcessiveCodeCount(t *testing.T) {
	runes := []rune("ab")
	glyphs := textCodeGlyphs(nil, runes, []models.CTCGTransform{{
		CodePosition: 0,
		CodeCount:    int(^uint(0) >> 1),
		Glyphs:       []int{65},
	}}, 0)

	if len(glyphs) != 1 {
		t.Fatalf("glyphs = %+v, want one mapped glyph", glyphs)
	}
}

func TestRenderableTextValueSkipsControlCharacters(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"A", true},
		{"ą", true},
		{"\u009b", false},
		{"\u0000", false},
		{"\u007f", false},
		{"\uFFFD", false},
		{"", false},
		{"a\u009b", true},
	}
	for _, tc := range cases {
		if got := renderableTextValue(tc.value); got != tc.want {
			t.Fatalf("renderableTextValue(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

// 文字图元的 CTM 线性部分必须整体作用到字形轮廓上。text-directions.ofd 第 5 页
// ID=96 使用 CTM="1 0 0.3 1 0 0"（水平倾斜）。OFD 对象空间 Y 轴向下，x'=x+0.3y
// 表示越靠下越右移，即上端偏左、下端偏右；此前渲染器只用 CTM 变换文字原点再调用
// 一次 Rotate，倾斜分量被静默丢弃，字形仍是直立的。之后改为套用线性部分时没有对
// Y 翻转做共轭，方向被上下镜像成上端偏右，因此这里同时断言倾斜方向。
func TestTextCTMShearTiltsGlyphs(t *testing.T) {
	// 用 shear=1 让倾斜足够明显：字形顶端相对底端应横向偏移约一个字高。
	const shear = 1.0
	plain := textInkEdgesMM(t, renderCreatorPage(t, shearedText(0)))
	leaned := textInkEdgesMM(t, renderCreatorPage(t, shearedText(shear)))

	if plain.height <= 0 || leaned.height <= 0 {
		t.Fatalf("墨迹高度 单位CTM=%.1fmm 倾斜CTM=%.1fmm，期望都大于 0", plain.height, leaned.height)
	}
	// shear=1 时整个字形高度都会变成横向偏移，倾斜带来的增量应接近一个字高。
	// 字形自身的收放（"水" 上宽下窄）在两次渲染中相同，用增量比较即可排除。
	plainOffset := plain.topLeft - plain.bottomLeft
	leanedOffset := leaned.topLeft - leaned.bottomLeft
	delta := leanedOffset - plainOffset
	// 正 shear 在对象空间把下半部右移、上半部左移，所以上缘相对下缘的偏移量应
	// 减少约一个字高（差值显著为负），而不是增加。
	if delta > -0.5*leaned.height {
		t.Fatalf("倾斜使上下左缘偏移从 %+.1fmm 变为 %+.1fmm，增量 %+.1fmm，期望 <= -字高一半 %.1fmm（倾斜方向应为上端偏左）",
			plainOffset, leanedOffset, delta, 0.5*leaned.height)
	}
}

// CTM 的缩放只能作用到字形一次：drawTextGlyph 已经用 textCTMLinearMatrix
// 施加了完整线性变换，不能再把 CTM.YScale 乘进 Size。否则 ano.ofd 这类
// 整篇文字都带 0.3528 缩放的文档，字号会被缩小到 0.3528²。
func TestTextCTMScaleAppliedOnce(t *testing.T) {
	scaledText := func(scale float64) creator.Text {
		return creator.Text{
			X: 20, Y: 100, Width: 160, Height: 30, Font: "楷体", Size: 30,
			Fill: boolPtrT(), FillColor: &creator.Color{R: 0, G: 0, B: 0},
			CTM:       &creator.CTM{scale, 0, 0, scale, 0, 0},
			TextCodes: []creator.TextCode{{Value: "水"}},
		}
	}
	full := textInkEdgesMM(t, renderCreatorPage(t, scaledText(1)))
	half := textInkEdgesMM(t, renderCreatorPage(t, scaledText(0.5)))
	if full.height <= 0 || half.height <= 0 {
		t.Fatalf("墨迹高度 scale=1: %.1fmm scale=0.5: %.1fmm，期望都大于 0", full.height, half.height)
	}
	ratio := half.height / full.height
	if ratio < 0.4 || ratio > 0.6 {
		t.Fatalf("CTM 缩放 0.5 时墨迹高度比 %.3f，期望约 0.5（缩放被应用了两次？）", ratio)
	}
}

func shearedText(shear float64) creator.Text {
	return creator.Text{
		X: 20, Y: 270, Width: 160, Height: 30, Font: "楷体", Size: 30,
		Fill: boolPtrT(), FillColor: &creator.Color{R: 0, G: 0, B: 0},
		CTM:       &creator.CTM{1, 0, shear, 1, 0, 0},
		TextCodes: []creator.TextCode{{Value: "水"}},
	}
}

type inkEdges struct {
	topLeft    float64
	bottomLeft float64
	top        float64
	bottom     float64
	height     float64
}

// textInkEdgesMM 返回墨迹上下四分之一高度处的左缘位置和墨迹高度（毫米）。
func textInkEdgesMM(t *testing.T, img image.Image) inkEdges {
	t.Helper()
	bounds := img.Bounds()
	scale := float64(bounds.Dx()) / 210.0
	rows := map[int]int{}
	top, bottom := -1, -1
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		left := -1
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 < 200 || g>>8 < 200 || b>>8 < 200 {
				left = x
				break
			}
		}
		if left < 0 {
			continue
		}
		rows[y] = left
		if top < 0 {
			top = y
		}
		bottom = y
	}
	if top < 0 {
		return inkEdges{}
	}
	span := float64(bottom - top)
	pick := func(fraction float64) float64 {
		y := top + int(span*fraction)
		return float64(rows[y]) / scale
	}
	return inkEdges{
		topLeft:    pick(0.1),
		bottomLeft: pick(0.9),
		top:        float64(top) / scale,
		bottom:     float64(bottom) / scale,
		height:     span / scale,
	}
}

// waveArrangedText 返回 8 个汉字横向排开、纵向按 deltaY 逐字累加的文字。
// deltaY 为 nil 时不写增量，用于对照平直排版。
func waveArrangedText(deltaY []float64) creator.Text {
	code := creator.TextCode{Value: "波浪形文字效果演示"}
	baseX, boxHeight := 0.0, 20.0
	code.X, code.Y = &baseX, &boxHeight
	if deltaY != nil {
		code.DeltaX = []float64{10, 10, 10, 10, 10, 10, 10, 10}
		code.DeltaY = deltaY
	}
	return creator.Text{
		X: 20, Y: 230, Width: 170, Height: 20, Font: "楷体", Size: 8,
		Fill: boolPtrT(), FillColor: &creator.Color{R: 30, G: 100, B: 180},
		TextCodes: []creator.TextCode{code},
	}
}

// glyphInkCentersMM 把 fromXMM 起 widthMM 宽的区域等分成 count 列，
// 返回每列墨迹的纵向中心（毫米）。列两侧内缩 0.5mm，避免相邻字互相串列。
func glyphInkCentersMM(t *testing.T, img image.Image, fromXMM, widthMM float64, count int) []float64 {
	t.Helper()
	bounds := img.Bounds()
	scale := float64(bounds.Dx()) / 210.0
	cell := widthMM / float64(count)
	centers := make([]float64, 0, count)
	for i := 0; i < count; i++ {
		left := int((fromXMM + float64(i)*cell + 0.5) * scale)
		right := int((fromXMM + float64(i+1)*cell - 0.5) * scale)
		top, bottom := -1, -1
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := left; x < right; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if r>>8 < 200 || g>>8 < 200 || b>>8 < 200 {
					if top < 0 {
						top = y
					}
					bottom = y
					break
				}
			}
		}
		if top >= 0 {
			centers = append(centers, (float64(top)+float64(bottom))/2/scale)
		}
	}
	return centers
}

// 波浪形文字依赖逐字 DeltaX/DeltaY 增量：OFD 语义下第 i 项增量是第 i+1 字
// 相对第 i 字的位移，所以第 1 字落在 X/Y 上，之后每字累加一次增量。
// text-directions.ofd 第 2 页 ID=40「波浪形文字效果演示」的说明写着
// “通过 delta_y 正负交替，实现波浪形排列效果”，但示例 manifest 原本既没有
// delta_x 也没有 delta_y，渲染结果是一行平直文字，看上去完全没有波浪。
func TestTextDeltaYProducesWaveArrangement(t *testing.T) {
	got := glyphInkCentersMM(t, renderCreatorPage(t, waveArrangedText([]float64{6, -6, -6, 6, 6, -6, -6, 0})), 20, 80, 8)
	if len(got) != 8 {
		t.Fatalf("切分出 %d 个字，实际应为 8 个", len(got))
	}
	want := []float64{0, 6, 0, -6, 0, 6, 0, -6}
	base := got[0]
	for i, center := range got {
		if offset, expect := center-base, want[i]; math.Abs(offset-expect) > 1.2 {
			t.Fatalf("第 %d 字墨迹中心相对第 1 字偏移 %+.1fmm，期望 %+.1fmm（偏差 %+.1fmm）", i+1, offset, expect, offset-expect)
		}
	}
	if spread := inkSpreadMM(got); spread < 10 {
		t.Fatalf("波浪文字纵向跨度仅 %.1fmm，期望 >= 10mm（未形成波浪）", spread)
	}
	// 对照组：同一段文字不带 delta_y 时应排成一行，证明波浪来自增量而非字形自身形状。
	if spread := inkSpreadMM(glyphInkCentersMM(t, renderCreatorPage(t, waveArrangedText(nil)), 20, 80, 8)); spread > 1.2 {
		t.Fatalf("不带 delta_y 时纵向跨度 %.1fmm，期望 <= 1.2mm（应排成一行）", spread)
	}
}

func inkSpreadMM(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	low, high := values[0], values[0]
	for _, v := range values[1:] {
		low, high = math.Min(low, v), math.Max(high, v)
	}
	return high - low
}
