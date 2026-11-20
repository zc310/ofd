package canvasfont

import (
	"runtime"
	"testing"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"golang.org/x/image/font/gofont/goregular"
)

func registerCoverageStyles(t *testing.T, fonts *Fonts, name string, regularCovers bool) *canvas.FontFamily {
	t.Helper()
	reference := canvas.NewFontFamily("reference")
	if err := reference.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	mapped, err := fontrepair.RepairWithGlyphs(goregular.TTF,
		[]fontrepair.GlyphMapping{{Rune: '中', Glyph: reference.Face(12).Font.GlyphIndex('A')}})
	if err != nil {
		t.Fatal(err)
	}
	regular, bold := goregular.TTF, mapped
	if regularCovers {
		regular, bold = mapped, goregular.TTF
	}
	t.Cleanup(func() {
		if err := UnregisterFallbackFont(name); err != nil {
			t.Error(err)
		}
	})
	if err := registerFallbackFont(regular, name, FontRegular); err != nil {
		t.Fatal(err)
	}
	if err := registerFallbackFont(bold, name, FontBold); err != nil {
		t.Fatal(err)
	}
	if err := fonts.UseFallbackFont(name); err != nil {
		t.Fatal(err)
	}
	return fonts.fallbacks[name]
}

func TestRegisteredFallbackChecksOtherStyleWhenRequestedStyleLacksGlyph(t *testing.T) {
	fonts := NewFonts(nil)
	t.Cleanup(fonts.Close)
	family := registerCoverageStyles(t, fonts, t.Name(), false)
	current := canvas.NewFontFamily("latin-only")
	if err := current.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	object := textObjectWith("中")
	for _, from := range []*canvas.FontFamily{current, family} {
		got := fonts.registeredFallbackCovering(from, object, FontRegular)
		if got.family != family || got.style != FontBold {
			t.Fatal("注册族的粗体包含文字，却没有作为其他样式回退")
		}
		face := buildTextFaceStyle(got.family, object, nil, got.style)
		if face.Font != family.Face(12, canvas.FontBold).Font || face.Font.GlyphIndex('中') == 0 {
			t.Fatal("覆盖检查选中的样式没有应用到实际绘制面")
		}
		path, _ := face.ToPath("中")
		if path == nil || path.Empty() {
			t.Fatal("选中的回退样式没有可绘制的轮廓")
		}
	}
}

func TestRegisteredFallbackPrefersCoveringRequestedStyleAcrossFamilies(t *testing.T) {
	fonts := NewFonts(nil)
	t.Cleanup(fonts.Close)
	first := registerCoverageStyles(t, fonts, t.Name()+"-first", false)
	second := registerCoverageStyles(t, fonts, t.Name()+"-second", true)
	object := textObjectWith("中")
	if got := fonts.registeredFallbackCovering(first, object, FontRegular); got.family != second || got.style != FontRegular {
		t.Fatal("其他家族的匹配样式可用时，不应优先使用第一个家族的非匹配样式")
	}
	if got := fonts.registeredFallbackCovering(first, textObjectWith("不存在的𰻞"), FontRegular); got.family != nil {
		t.Fatal("所有样式都缺字时不应硬换字体")
	}
}

func TestSelectedFallbackStyleKeepsFamilyLockAndSurvivesRemoval(t *testing.T) {
	fonts := NewFonts(nil)
	t.Cleanup(fonts.Close)
	name := t.Name()
	family := registerCoverageStyles(t, fonts, name, false)
	selected := selectedFontFamily{family: family, style: FontBold}
	object := textObjectWith("中")
	lock := fonts.RenderLock(family)
	face, actual := fonts.FaceObject(selected, object, nil)
	if face == nil || face.GlyphIndex('中') == 0 || actual != selected || actual.Name() != family.Name() ||
		fonts.RenderLock(actual) != lock {
		t.Fatal("固定样式的句柄没有保留实际字体或共享锁")
	}
	fonts.RemoveFallbackFont(name)
	// 正在绘制的旧页面持有此句柄。移除配置不能使渐变 run 重建画笔时切回缺字样式。
	next, nextFamily := fonts.FaceObject(actual, object, nil)
	if next == nil || next.GlyphIndex('中') == 0 || nextFamily != actual || fonts.RenderLock(nextFamily) != lock {
		t.Fatal("移除回退配置后旧页面的实际样式或渲染锁发生变化")
	}
}

func TestFaceObjectKeepsOtherStyleFallbackWhenRecreatingPaint(t *testing.T) {
	if runtime.GOOS != "js" {
		t.Skip("此测试覆盖浏览器中仅使用注册字体的回退链")
	}
	for _, regularCovers := range []bool{false, true} {
		fonts := NewFonts(nil)
		t.Cleanup(fonts.Close)
		name := t.Name() + map[bool]string{false: "-bold", true: "-regular"}[regularCovers]
		family := registerCoverageStyles(t, fonts, name, regularCovers)
		object := textObjectWith("中")
		style := FontRegular
		if regularCovers {
			object.Weight, style = 700, FontBold
		}
		face, actual := fonts.FaceObject(family, object, nil)
		if face == nil || face.GlyphIndex('中') == 0 || canvasFamily(actual) != family {
			t.Fatal("同一注册族的请求样式缺字时没有使用另一可用样式")
		}
		if path := face.ToPath("中"); path == nil || path.Empty() {
			t.Fatal("回退后的字体面无法绘制对象文字")
		}
		chosenStyle := FontBold
		if regularCovers {
			chosenStyle = FontRegular
		}
		reference := buildTextFaceStyle(family, object, nil, chosenStyle)
		if got, want := face.TextWidth("中"), reference.TextWidth("中"); got != want {
			t.Fatalf("文字度量没有使用选中的样式：%v，期望 %v", got, want)
		}
		lock := fonts.RenderLock(actual)
		next, nextFamily := fonts.FaceObject(actual, object, nil)
		if next == nil || next.GlyphIndex('中') == 0 || nextFamily != actual || fonts.RenderLock(nextFamily) != lock {
			t.Fatal("重建画笔时丢失了先前选中的字体面或共享渲染锁")
		}
		if got := family.Face(12, canvasStyle(style)).Font.GlyphIndex('中'); got != 0 {
			t.Fatal("覆盖探测改写了原家族的样式映射")
		}
		fonts.RemoveFallbackFont(name)
		retained, retainedFamily := fonts.FaceObject(actual, object, nil)
		if retained == nil || retained.GlyphIndex('中') == 0 || retainedFamily != actual || fonts.RenderLock(retainedFamily) != lock {
			t.Fatal("移除注册回退后，正在绘制的旧句柄丢失了实际样式或渲染锁")
		}
	}
}
