package canvas

import (
	"runtime"
	"strings"
	"testing"

	"github.com/zc310/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/drawing"
	"golang.org/x/image/font/gofont/goregular"
)

func textObjectWith(value string) models.TextObject {
	return models.TextObject{
		CtText: models.CtText{
			TextCode: []models.TextCode{{Value: value}},
			Size:     4,
		},
	}
}

// familyCoversCJK 仅供测试筛选样例字体，复用生产代码的实际文字覆盖判断。
func familyCoversCJK(family *canvas.FontFamily, style drawing.FontStyle) bool {
	return familyCoversText(family, textObjectWith("中"), style)
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
}

func TestFamilyCoversTextChecksBeyondDedupLimit(t *testing.T) {
	family := canvas.NewFontFamily("Go Regular")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	face := family.Face(cjkProbeSize, canvas.Black)
	// 收集真实字体支持的不同字符，确保前缀确实达到边界；重复 ASCII 不会触发它。
	covered := make([]rune, 0, textCoverageDedupLimit+1)
	for r := rune(0x20); r <= 0x2fff && len(covered) <= textCoverageDedupLimit; r++ {
		if face.Font.GlyphIndex(r) != 0 {
			covered = append(covered, r)
		}
	}
	if len(covered) != textCoverageDedupLimit+1 {
		t.Fatalf("测试字体仅找到 %d 个不同字符，无法覆盖边界", len(covered))
	}
	if face.Font.GlyphIndex('中') != 0 {
		t.Fatal("测试字体意外包含中文字形，无法构造末尾缺字")
	}
	for _, tc := range []struct {
		name  string
		codes []models.TextCode
		want  bool
	}{
		{"below limit", []models.TextCode{{Value: string(covered[:textCoverageDedupLimit-1])}}, true},
		{"at limit", []models.TextCode{{Value: string(covered[:textCoverageDedupLimit])}}, true},
		{"beyond limit", []models.TextCode{{Value: string(covered)}}, true},
		{"missing at limit", []models.TextCode{{Value: string(covered[:textCoverageDedupLimit-1]) + "中"}}, false},
		{"missing after limit", []models.TextCode{{Value: string(covered[:textCoverageDedupLimit]) + "中"}}, false},
		{"missing beyond limit", []models.TextCode{{Value: string(covered) + "中"}}, false},
		{"missing in later TextCode", []models.TextCode{{Value: string(covered)}, {Value: "中"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			object := textObjectWith("")
			object.TextCode = tc.codes
			if got := familyCoversText(family, object, FontRegular); got != tc.want {
				t.Fatalf("覆盖检查 = %v，期望 %v", got, tc.want)
			}
		})
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

// 长文本中的重复字符应复用已确认的覆盖结果。
func TestFamilyCoversTextCountsDistinctRunes(t *testing.T) {
	cjk := firstCJKCapableFamily(t, FontRegular, cjkFallbackFamilies)
	if cjk == nil {
		t.Skip("系统没有可用的 CJK 字体")
	}
	repeated := strings.Repeat("中", textCoverageDedupLimit*2)
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
