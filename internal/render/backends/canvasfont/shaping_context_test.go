package canvasfont

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/canvas/renderers/rasterizer"
	"github.com/zc310/canvas/text"
	"golang.org/x/image/font/gofont/goregular"
)

func TestShapingCachesSeparateDirectionScriptAndLanguage(t *testing.T) {
	family := canvas.NewFontFamily("shaping-context")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*canvas.FontFace)
	}{
		{"direction", func(face *canvas.FontFace) { face.Direction = text.RightToLeft }},
		{"vertical_direction", func(face *canvas.FontFace) { face.Direction = text.TopToBottom }},
		{"script", func(face *canvas.FontFace) { face.Script = text.Latin }},
		{"language", func(face *canvas.FontFace) { face.Language = "tr" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fonts := NewFonts(nil)
			defer fonts.Close()
			base := family.Face(12, canvas.Black)
			changed := *base
			tc.change(&changed)
			const value = "ABC fi"
			basePath, baseLine := fonts.shapedTextPath(base, value), fonts.shapedTextLine(base, value)
			gotPath, gotLine := fonts.shapedTextPath(&changed, value), fonts.shapedTextLine(&changed, value)
			if gotPath == nil || gotPath.Empty() || gotLine == nil {
				t.Fatal("缺少整形结果")
			}
			if gotPath == basePath || gotLine == baseLine {
				t.Fatal("不同整形上下文复用了相同缓存条目")
			}
			wantPath, _ := changed.ToPath(value)
			if wantPath == nil || gotPath.String() != wantPath.String() {
				t.Fatal("缓存轮廓与该字体面的直接计算结果不一致")
			}
			if !reflect.DeepEqual(gotLine, canvas.NewTextLine(&changed, value, canvas.Left)) {
				t.Fatal("缓存文字行与该字体面的直接计算结果不一致")
			}
			if fonts.shapedTextPath(base, value) != basePath || fonts.shapedTextLine(base, value) != baseLine ||
				fonts.shapedTextPath(&changed, value) != gotPath || fonts.shapedTextLine(&changed, value) != gotLine {
				t.Fatal("相同上下文没有命中各自的缓存")
			}
		})
	}
}

func TestShapingCachesSeparateActualFaceGeometry(t *testing.T) {
	family := canvas.NewFontFamily("face-geometry")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*canvas.FontFace)
	}{
		{"faux_bold", func(face *canvas.FontFace) { face.FauxBold = 0.02 }},
		{"faux_italic", func(face *canvas.FontFace) { face.FauxItalic = 0.3 }},
		{"x_offset", func(face *canvas.FontFace) { face.XOffset = 200 }},
		{"y_offset", func(face *canvas.FontFace) { face.YOffset = 200 }},
		{"scale", func(face *canvas.FontFace) { face.MmPerEm *= 1.2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fonts := NewFonts(nil)
			defer fonts.Close()
			base := family.Face(12, canvas.Black)
			changed := *base
			tc.change(&changed)
			const value = "HT"
			path, line := fonts.shapedTextPath(base, value), fonts.shapedTextLine(base, value)
			gotPath, gotLine := fonts.shapedTextPath(&changed, value), fonts.shapedTextLine(&changed, value)
			wantPath, _ := changed.ToPath(value)
			if gotPath == nil || wantPath == nil || gotPath == path || gotLine == line {
				t.Fatal("不同几何参数的字体面复用了相同条目")
			}
			if path.String() == wantPath.String() {
				t.Fatal("测试参数未改变实际轮廓")
			}
			if gotPath.String() != wantPath.String() || !reflect.DeepEqual(gotLine, canvas.NewTextLine(&changed, value, canvas.Left)) {
				t.Fatal("缓存结果与该字体面的直接计算不一致")
			}
			if fonts.shapedTextPath(&changed, value) != gotPath || fonts.shapedTextLine(&changed, value) != gotLine {
				t.Fatal("相同几何参数没有命中缓存")
			}
			fonts.cachedTextWidth(base, value)
			if got, want := fonts.cachedTextWidth(&changed, value), changed.TextWidth(value); got != want {
				t.Fatalf("缓存宽度 = %g，期望 %g", got, want)
			}
		})
	}
}

// 自定义装饰可能带有切片等不可比较状态，不能把接口本身当作 map key。
type sliceFontDecoration struct{ values []float64 }

func (d sliceFontDecoration) Decorate(r canvas.Renderer, m canvas.Matrix, face *canvas.FontFace, path *canvas.Path, width float64) {
	canvas.FontUnderline.Decorate(r, m, face, path, width)
}

func TestDecoratedTextBypassesLineCacheAndKeepsDecoration(t *testing.T) {
	family := canvas.NewFontFamily("decorated-text")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	fonts := NewFonts(nil)
	defer fonts.Close()
	base := family.Face(24, canvas.Black)
	const value = "ABC"
	basePath, baseLine := fonts.shapedTextPath(base, value), fonts.shapedTextLine(base, value)
	renderLine := func(line *canvas.Text) []byte {
		page := canvas.New(40, 20)
		page.RenderText(line, canvas.Identity.Translate(3, 8))
		return rasterizer.Draw(page, canvas.DPI(96), canvas.DefaultColorSpace).Pix
	}
	for _, deco := range []canvas.FontDecorator{canvas.FontUnderline, sliceFontDecoration{values: []float64{1}}} {
		decorated := *base
		decorated.Deco = []canvas.FontDecorator{deco}
		line := fonts.shapedTextLine(&decorated, value)
		if line == baseLine || fonts.shapedTextLine(&decorated, value) == line {
			t.Fatal("带装饰的字体面进入了文字行缓存")
		}
		got, want := renderLine(line), renderLine(canvas.NewTextLine(&decorated, value, canvas.Left))
		if !bytes.Equal(got, want) || bytes.Equal(got, renderLine(baseLine)) {
			t.Fatal("装饰丢失或缓存渲染结果与直接绘制不同")
		}
		if fonts.shapedTextPath(&decorated, value) != basePath {
			t.Fatal("不含装饰的轮廓几何应继续复用")
		}
	}
	if fonts.textLineCache.Len() != 1 || fonts.shapedTextLine(base, value) != baseLine {
		t.Fatal("带装饰的绘制污染了普通文字行缓存")
	}
}
