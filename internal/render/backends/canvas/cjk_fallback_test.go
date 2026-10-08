package canvas

import (
	"runtime"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/drawing"
)

func textObjectWith(value string) models.TextObject {
	return models.TextObject{
		TextCode: []models.TextCode{{Value: value}},
		Size:     4,
	}
}

// firstCJKCapableFamily 返回候选表里第一个既能载入又覆盖中文的系统字体族。
// 找不到时返回 nil——无 CJK 字体的环境本来就该画成豆腐块，不算回归。
func firstCJKCapableFamily(t *testing.T, style drawing.FontStyle, families []string) *canvas.FontFamily {
	t.Helper()
	for _, name := range families {
		family := canvas.NewFontFamily(name)
		if err := family.LoadSystemFont(name, canvasStyle(style)); err != nil {
			continue
		}
		if familyCoversCJK(family, style) {
			return family
		}
	}
	return nil
}

// familyCoversCJK 必须按该 style 实际生效的字体判断。canvas 的 FontFamily
// 按 style 存放字体，载入第二个字体会覆盖第一个，因此覆盖结果要与 style 对应。
func TestFamilyCoversCJKFollowsStyle(t *testing.T) {
	regular := firstCJKCapableFamily(t, FontRegular, cjkFallbackFamilies)
	if regular == nil {
		t.Skip("系统没有可用的 CJK 字体")
	}
	if !familyCoversCJK(regular, FontRegular) {
		t.Fatal("候选 CJK 字体应报告覆盖中文")
	}
}

// familyCoversText 是替代字体的触发条件：Latin-only 字体遇到中文必须返回
// false，纯 ASCII 必须返回 true，否则每个西文文档都会被无谓地换字体。
func TestFamilyCoversTextRejectsMissingGlyphs(t *testing.T) {
	latin := canvas.NewFontFamily("DejaVu Sans")
	if err := latin.LoadSystemFont("DejaVu Sans", canvasStyle(FontRegular)); err != nil {
		t.Skip("DejaVu Sans is unavailable")
	}
	if !familyCoversText(latin, textObjectWith("Hello, world 123"), FontRegular) {
		t.Fatal("纯 ASCII 文本应被 Latin 字体覆盖")
	}
	if !familyCoversText(latin, textObjectWith(""), FontRegular) {
		t.Fatal("空文本不应判定为缺字")
	}
	cjk := firstCJKCapableFamily(t, FontRegular, cjkFallbackFamilies)
	if cjk == nil {
		t.Skip("系统没有可用的 CJK 字体")
	}
	if !familyCoversText(cjk, textObjectWith("中文与 Latin 混排"), FontRegular) {
		t.Fatal("CJK 字体应覆盖中英混排")
	}
	// 覆盖探测有上限，超过上限就认为覆盖充分，避免长文本逐字符查表。
	long := make([]rune, textCoverageProbeLimit+10)
	for i := range long {
		long[i] = 'A'
	}
	if !familyCoversText(cjk, textObjectWith(string(long)), FontRegular) {
		t.Fatalf("超过 %d 个字符后应停止探测并判定为覆盖", textCoverageProbeLimit)
	}
}

// 缺字时 FaceObject 要换到真能画出来的字体族；画得出来时必须保持原族，
// 否则纯西文文档的外观会被整体换掉。
func TestFaceObjectSwapsFamilyWhenGlyphsMissing(t *testing.T) {
	if !systemFontLookupUsable() {
		t.Skip("该平台不枚举系统字体，缺字由注册的回退字体处理")
	}
	cjk := firstCJKCapableFamily(t, FontRegular, cjkFallbackFamilies)
	if cjk == nil {
		t.Skip("系统没有可用的 CJK 字体")
	}
	latin := canvas.NewFontFamily("DejaVu Sans")
	if err := latin.LoadSystemFont("DejaVu Sans", canvasStyle(FontRegular)); err != nil {
		t.Skip("DejaVu Sans is unavailable")
	}
	if familyCoversCJK(latin, FontRegular) {
		t.Skip("DejaVu Sans 意外覆盖中文，无法构造缺字场景")
	}
	fonts := NewFonts(nil)
	object := textObjectWith("代码块绘制")
	if fonts.cjkFallbackFamily(latin, object) == nil {
		t.Fatal("缺字的文字对象应找到替代字体族")
	}
	if got := fonts.cjkFallbackFamily(latin, textObjectWith("plain ASCII")); got != nil {
		t.Fatalf("不缺字时不应替换字体族，实际 %q", got.Name())
	}
	// 已经覆盖中文的字体族不应再被替换。
	if got := fonts.cjkFallbackFamily(cjk, textObjectWith("中文")); got != nil {
		t.Fatalf("已覆盖中文的字体族不应被替换，实际 %q", got.Name())
	}
}

// 等宽对象要换到同样等宽的 CJK 字体：代码块里的中文注释若落到比例字体上，
// 缩进会全部错位。
func TestCJKFallbackKeepsMonospaceForFixedWidthFamily(t *testing.T) {
	if !systemFontLookupUsable() {
		t.Skip("该平台不枚举系统字体")
	}
	var mono *canvas.FontFamily
	for _, name := range fixedWidthFontFamilies {
		candidate := canvas.NewFontFamily("fixed-width")
		if err := candidate.LoadSystemFont(name, canvasStyle(FontRegular)); err != nil {
			continue
		}
		mono = candidate
		break
	}
	if mono == nil {
		t.Skip("系统没有可用的等宽字体")
	}
	if familyCoversCJK(mono, FontRegular) {
		t.Skip("首个等宽候选已覆盖中文，无法验证替换")
	}
	if !isFixedWidthName(mono.Name()) {
		t.Fatalf("等宽回退族名 %q 应被识别为等宽", mono.Name())
	}
	cjk := firstCJKCapableFamily(t, FontRegular, cjkFixedWidthFamilies)
	if cjk == nil {
		t.Skip("系统没有可用的等宽 CJK 字体")
	}
	if !familyCoversText(cjk, textObjectWith("中文注释"), FontRegular) {
		t.Fatal("等宽 CJK 字体应覆盖中文注释")
	}
}

// 替代字体表里不能出现通用别名：serif/sans-serif/monospace 会被 fontconfig
// 解析回原来那个 Latin-only 字体，「换字体」就白换了。
func TestCJKFallbackListsRejectGenericAliases(t *testing.T) {
	for label, list := range map[string][]string{
		"cjkFallbackFamilies":   cjkFallbackFamilies,
		"cjkFixedWidthFamilies": cjkFixedWidthFamilies,
	} {
		if len(list) == 0 {
			t.Fatalf("%s 不能为空", label)
		}
		for _, name := range list {
			if isGenericFontFamily(name) {
				t.Fatalf("%s 含通用别名 %q", label, name)
			}
			if strings.TrimSpace(name) == "" {
				t.Fatalf("%s 含空族名", label)
			}
		}
	}
}

// 覆盖探测有上限时按去重字符集判断：重复字符不应消耗探测额度，否则一段
// 重复文本会被误判成「只检查了几个字符」。
func TestFamilyCoversTextCountsDistinctRunes(t *testing.T) {
	cjk := firstCJKCapableFamily(t, FontRegular, cjkFallbackFamilies)
	if cjk == nil {
		t.Skip("系统没有可用的 CJK 字体")
	}
	repeated := strings.Repeat("中", textCoverageProbeLimit*2)
	if !familyCoversText(cjk, textObjectWith(repeated), FontRegular) {
		t.Fatal("重复字符不应触发探测上限")
	}
}

// wasm/浏览器没有可枚举的系统字体目录，tdewolff/font 在那里遍历目录会空指针
// 崩溃。系统字体查找必须先被挡住，替代字体这条路径才会安全地退化成 no-op。
func TestSystemFontLookupGuardsUnsupportedPlatforms(t *testing.T) {
	if runtime.GOOS != "js" {
		if !systemFontLookupUsable() {
			t.Fatalf("GOOS=%s 应允许系统字体查找", runtime.GOOS)
		}
		return
	}
	if systemFontLookupUsable() {
		t.Fatal("js 平台必须禁用系统字体查找")
	}
	if _, ok := loadCachedSystemFont(NewFonts(nil), cjkFallbackFamilies[0], FontRegular); ok {
		t.Fatal("js 平台不应加载到系统字体")
	}
}
