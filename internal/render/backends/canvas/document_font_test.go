package canvas

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"unsafe"

	"github.com/tdewolff/canvas"
	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

func TestCJKFontGroupMapsLogicalFamilyToSystemGroup(t *testing.T) {
	tests := []struct {
		family string
		name   string
		group  string
	}{
		{family: "宋体", name: "F0", group: "simsun"},
		{family: "方正小标宋_GBK", name: "F1", group: "simsun"},
		{family: "仿宋_GB2312", name: "F2", group: "simfang"},
		{family: "方正仿宋_GBK", name: "F3", group: "simfang"},
		{family: "楷体", name: "F4", group: "simkai"},
		{family: "方正楷体_GBK", name: "F5", group: "simkai"},
		{family: "黑体", name: "F6", group: "simhei"},
		{family: "微软雅黑", name: "F7", group: "yahei"},
		{family: "Smiley Sans", name: "F9", group: "smileysans"},
		{family: "得意黑", name: "F10", group: "smileysans"},
		{family: "", name: "F8", group: ""},
		{family: "ZGCCnm-1", name: "TT-0", group: ""},
	}
	for _, tt := range tests {
		got := cjkFontGroup(&models.Font{FamilyName: tt.family, FontName: tt.name})
		if got != tt.group {
			t.Errorf("cjkFontGroup(%q/%q) = %q, want %q", tt.family, tt.name, got, tt.group)
		}
	}
}

// useFallback 注册全局回退字体并将其应用到指定字体上下文。
func useFallback(t *testing.T, fonts *Fonts, data []byte, family string, style FontStyle) {
	t.Helper()
	if err := registerFallbackFont(data, family, style); err != nil {
		t.Fatal(err)
	}
	if err := fonts.UseFallbackFont(family); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFontConcurrentUsesOneCachedFamily(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "..", "..", "test", "testdata", "intro.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	fonts := NewFonts(ofd.Documents[0])
	const workers = 16
	families := make(chan FontFamily, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			family, loadErr := fonts.LoadFont(models.StRefID(128))
			if loadErr != nil {
				errs <- loadErr
				return
			}
			families <- family
		}()
	}
	wg.Wait()
	close(families)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var first *canvas.FontFamily
	for family := range families {
		current := family.(*canvas.FontFamily)
		if first == nil {
			first = current
			continue
		}
		if current != first {
			t.Fatal("同一字体的并发加载创建了多个字体族")
		}
	}
}

func TestSystemFontCacheReusesFamilyAndRenderLock(t *testing.T) {
	first, ok := loadCachedSystemFont("DejaVu Sans", FontRegular)
	if !ok {
		t.Skip("DejaVu Sans is unavailable")
	}
	second, ok := loadCachedSystemFont("DejaVu Sans", FontRegular)
	if !ok {
		t.Fatal("cached DejaVu Sans could not be loaded")
	}
	if first != second {
		t.Fatal("system font cache created multiple font families")
	}

	firstFonts := NewFonts(nil)
	secondFonts := NewFonts(nil)
	if firstFonts.RenderLock(first) != secondFonts.RenderLock(second) {
		t.Fatal("system font cache did not share the render lock")
	}
}

func TestEmbeddedFontCacheReusesFamilyAndRenderLock(t *testing.T) {
	data, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans is unavailable: %v", err)
	}
	first, err := loadCachedEmbeddedFont("EmbeddedCache", data, FontRegular, nil)
	if err != nil {
		t.Fatalf("加载嵌入字体失败: %v", err)
	}
	second, err := loadCachedEmbeddedFont("EmbeddedCache", data, FontRegular, nil)
	if err != nil {
		t.Fatalf("复用嵌入字体失败: %v", err)
	}
	if first != second {
		t.Fatal("嵌入字体缓存创建了多个字体族")
	}
	mapped, err := loadCachedEmbeddedFont("EmbeddedCache", data, FontRegular, []fontfix.GlyphMapping{{Rune: 'A', Glyph: 1}})
	if err != nil {
		t.Fatalf("加载带映射的嵌入字体失败: %v", err)
	}
	if mapped == first {
		t.Fatal("不同字形映射不应复用同一字体族")
	}
	firstFonts := NewFonts(nil)
	secondFonts := NewFonts(nil)
	if firstFonts.RenderLock(first) != secondFonts.RenderLock(second) {
		t.Fatal("嵌入字体缓存未共享渲染锁")
	}
}

func TestHashGlyphMappingsIsOrderIndependent(t *testing.T) {
	a := hashGlyphMappings([]fontfix.GlyphMapping{{Rune: 'A', Glyph: 1}, {Rune: 'B', Glyph: 2}})
	b := hashGlyphMappings([]fontfix.GlyphMapping{{Rune: 'B', Glyph: 2}, {Rune: 'A', Glyph: 1}})
	if a != b {
		t.Fatal("字形映射摘要应与顺序无关")
	}
	c := hashGlyphMappings([]fontfix.GlyphMapping{{Rune: 'A', Glyph: 2}, {Rune: 'B', Glyph: 1}})
	if a == c {
		t.Fatal("不同字形映射应产生不同摘要")
	}
}

func TestFallbackFontRegistryReusesFamilyAndRenderLock(t *testing.T) {
	data, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans is unavailable: %v", err)
	}
	firstFonts := NewFonts(nil)
	secondFonts := NewFonts(nil)
	useFallback(t, firstFonts, data, "ProcessFallback", FontRegular)
	useFallback(t, secondFonts, data, "ProcessFallback", FontRegular)
	first := firstFonts.fallbacks["ProcessFallback"]
	second := secondFonts.fallbacks["ProcessFallback"]
	if firstFonts.fallbacks["ProcessFallback"] != secondFonts.fallbacks["ProcessFallback"] {
		t.Fatal("fallback font registry created multiple font families")
	}
	if firstFonts.RenderLock(first) != secondFonts.RenderLock(second) {
		t.Fatal("fallback font registry did not share the render lock")
	}
}

func TestFallbackFontRegisteredOnceGlobally(t *testing.T) {
	font, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans is unavailable: %v", err)
	}
	firstFonts := NewFonts(nil)
	secondFonts := NewFonts(nil)
	useFallback(t, firstFonts, font, "LockedFamily", FontRegular)
	useFallback(t, secondFonts, font, "LockedFamily", FontRegular)
	reg := fallbackRegistry["LockedFamily"]
	if reg == nil {
		t.Fatal("fallback family was not registered globally")
	}
	if len(reg.sources) != 1 {
		t.Fatalf("global fallback sources = %d, want 1", len(reg.sources))
	}
	if firstFonts.fallbacks["LockedFamily"] != reg.family {
		t.Fatal("first fonts instance does not use the locked global family")
	}
	if secondFonts.fallbacks["LockedFamily"] != reg.family {
		t.Fatal("second fonts instance does not reuse the locked global family")
	}
	if &reg.sources[0].data[0] != &font[0] {
		t.Fatal("fallback font data was copied instead of shared")
	}
}

func TestFallbackFontIsDefaultWhenNoOtherFonts(t *testing.T) {
	font, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans is unavailable: %v", err)
	}
	fonts := NewFonts(nil)
	useFallback(t, fonts, font, "DefaultFallback", FontRegular)
	// 缺字体的样式也必须匹配到同一个全局家族（Bold 落回 Regular face）。
	regular, ok := selectFallback(fonts.fallbackFaces, FontRegular)
	if !ok {
		t.Fatal("no fallback face available for Regular")
	}
	bold, ok := selectFallback(fonts.fallbackFaces, FontBold)
	if !ok {
		t.Fatal("no fallback face available for Bold")
	}
	if regular.family != bold.family || regular.family != fallbackRegistry["DefaultFallback"].family {
		t.Fatal("missing-font fallback did not default to the locked global family")
	}
}

func TestFallbackFontRegistryKeepsDifferentStylesInOneFamily(t *testing.T) {
	regular, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans is unavailable: %v", err)
	}
	bold, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans Bold is unavailable: %v", err)
	}
	fonts := NewFonts(nil)
	useFallback(t, fonts, regular, "ProcessStyleFallback", FontRegular)
	useFallback(t, fonts, bold, "ProcessStyleFallback", FontBold)
	family := fonts.fallbacks["ProcessStyleFallback"]
	if family.Face(12, canvas.Black, canvas.FontRegular).Font == family.Face(12, canvas.Black, canvas.FontBold).Font {
		t.Fatal("different fallback styles did not keep separate font faces")
	}
}

func TestSelectFallbackPrefersMatchingFontFamily(t *testing.T) {
	generic := canvas.NewFontFamily("OFD-NotoSansSC")
	kaiti := canvas.NewFontFamily("楷体")
	fallback, ok := selectFallback([]fallbackFace{
		{family: generic, style: FontRegular, name: "OFD-NotoSansSC"},
		{family: kaiti, style: FontRegular, name: "楷体"},
	}, FontRegular, "KaiTi")
	if !ok {
		t.Fatal("no fallback font was selected")
	}
	if fallback.family != kaiti {
		t.Fatalf("fallback family = %q, want 楷体", fallback.family.Name())
	}
	for _, pair := range [][2]string{
		{"宋体", "SimSun_GB2312"},
		{"楷体", "KaiTi_GB2312"},
		{"黑体", "Microsoft HeiTi"},
		{"仿宋", "FangSong_GB2312"},
		{"微软雅黑", "Microsoft YaHei UI"},
		{"思源黑体", "Noto Sans CJK SC"},
		{"思源宋体", "Noto Serif CJK SC"},
	} {
		if !sameFallbackName(pair[0], pair[1]) {
			t.Errorf("font aliases %q and %q were not recognized", pair[0], pair[1])
		}
	}
}

func TestIntroEmbeddedFontsLoad(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "..", "..", "test", "testdata", "intro.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	fonts := NewFonts(ofd.Documents[0])
	for _, id := range []uint16{128, 396} {
		family, err := fonts.LoadFont(models.StRefID(id))
		if err != nil {
			t.Fatalf("font %d: %v", id, err)
		}
		defaultFamily, _ := defaultFallbackFont()
		if family == defaultFamily || !fontFamilyUsable(family.(*canvas.FontFamily)) {
			t.Fatalf("font %d was not loaded as an embedded usable font", id)
		}
	}
	family, err := fonts.LoadFont(models.StRefID(128))
	if err != nil {
		t.Fatal(err)
	}
	cf := family.(*canvas.FontFamily)
	face := cf.Face(1, canvas.Black)
	for _, glyphID := range []uint16{4947, 3773, 6809} {
		if got := face.Font.GlyphIndex(fontfix.GlyphRune(glyphID)); got == 0 {
			t.Fatalf("CFF CID %d has no glyph mapping", glyphID)
		}
		path, _ := face.ToPath(string(fontfix.GlyphRune(glyphID)))
		if path == nil || path.Empty() {
			t.Fatalf("CFF glyph %d has no path", glyphID)
		}
	}
}

func TestAnoFont115UsesDeclaredEmbeddedFont(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	doc := ofd.Documents[0]
	if got := string(doc.GetFont(115).FontFile); got != "Doc_0/Res/font_13132_0.ttf" {
		t.Fatalf("font 115 file = %q", got)
	}
	family, err := NewFonts(doc).LoadFont(115)
	if err != nil {
		t.Fatal(err)
	}
	if !fontFamilyUsable(family.(*canvas.FontFamily)) {
		t.Fatal("font 115 is not usable")
	}
	defaultFamily, _ := defaultFallbackFont()
	if family == defaultFamily {
		t.Fatal("font 115 fell back to the default font")
	}
}

func TestAnoAnnotationFontWithoutFileDoesNotUseSubsetFont(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	doc := ofd.Documents[0]
	fonts := NewFonts(doc)
	family, err := fonts.LoadFont(13134)
	if err != nil {
		t.Fatal(err)
	}
	if family == nil {
		t.Fatal("annotation font is nil")
	}
	embedded, err := fonts.LoadFont(91)
	if err != nil {
		t.Fatal(err)
	}
	if family == embedded {
		t.Fatal("annotation font incorrectly reused the subset font")
	}
}

func TestRepairFontDataProducesUsableEmbeddedFont(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "..", "..", "test", "testdata", "ano.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	data, err := ofd.Documents[0].FileCache.Read("Doc_0/Res/font_13132.ttf")
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := fontfix.Repair(data)
	if err != nil {
		t.Fatal(err)
	}
	family := canvas.NewFontFamily("test")
	if err := family.LoadFont(fixed, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	if !fontFamilyUsable(family) {
		t.Fatal("repaired font is not usable")
	}
	face := family.Face(1, canvas.Black)
	if got := face.Font.GlyphIndex(fontfix.GlyphRune(1)); got != 1 {
		t.Fatalf("repaired cmap maps glyph 1 to %d", got)
	}
}

func TestFallbackFontKeepsRegularAndBoldFaces(t *testing.T) {
	regular, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans is unavailable: %v", err)
	}
	bold, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans Bold is unavailable: %v", err)
	}

	fonts := NewFonts(nil)
	useFallback(t, fonts, regular, "fallback", FontRegular)
	useFallback(t, fonts, bold, "fallback", FontBold)

	regularFace := fonts.fallbacks["fallback"].Face(12, canvas.Black, canvas.FontRegular)
	boldFace := fonts.fallbacks["fallback"].Face(12, canvas.Black, canvas.FontBold)
	if regularFace == nil || boldFace == nil || regularFace.Font == boldFace.Font {
		t.Fatal("regular and bold fallback faces were not kept separately")
	}
}

func TestIsFixedWidthNameRecognizesMonospaceFamilies(t *testing.T) {
	for _, name := range []string{"monospace", "DejaVu Sans Mono", "Consolas", "Menlo", "Courier New", "Noto Sans Mono CJK SC"} {
		if !isFixedWidthName(name) {
			t.Fatalf("应识别为等宽字体: %s", name)
		}
	}
	for _, name := range []string{"", "FangSong", "SimSun", "Arial"} {
		if isFixedWidthName(name) {
			t.Fatalf("不应识别为等宽字体: %s", name)
		}
	}
}

func TestIsFixedWidthFontUsesFlagAndFamily(t *testing.T) {
	if !isFixedWidthFont(&models.Font{FixedWidth: true}) {
		t.Fatalf("FixedWidth 标志应生效")
	}
	if !isFixedWidthFont(&models.Font{FamilyName: "monospace"}) {
		t.Fatalf("族名 monospace 应生效")
	}
	if isFixedWidthFont(&models.Font{FamilyName: "SimSun"}) {
		t.Fatalf("普通族名不应识别为等宽字体")
	}
}

func TestIsGenericFontFamily(t *testing.T) {
	for _, name := range []string{"monospace", "sans-serif", "serif", "fixed"} {
		if !isGenericFontFamily(name) {
			t.Fatalf("应为通用族名: %s", name)
		}
	}
	for _, name := range []string{"Menlo", "Consolas", "DejaVu Sans Mono"} {
		if isGenericFontFamily(name) {
			t.Fatalf("不应为通用族名: %s", name)
		}
	}
}

// TestSystemFontCandidatesMapsPostScriptNames 验证 PDF 的 PostScript 子集名
// 会映射到可匹配的系统族名与通用族，避免非嵌入字体回退成风格不符的默认字体。
func TestSystemFontCandidatesMapsPostScriptNames(t *testing.T) {
	cases := []struct {
		font     models.Font
		contains []string
	}{
		{models.Font{FamilyName: "NimbusRomNo9L-Medi", Bold: true}, []string{"Nimbus Roman", "serif"}},
		{models.Font{FamilyName: "NimbusRomNo9L-ReguItal", Italic: true}, []string{"Nimbus Roman", "serif"}},
		{models.Font{FamilyName: "NimbusSanNo9L-Regu"}, []string{"Nimbus Sans", "sans-serif"}},
		{models.Font{FamilyName: "NimbusMonNo9L-Regu"}, []string{"Nimbus Mono PS", "monospace"}},
		{models.Font{FamilyName: "CMTT9", FixedWidth: true}, []string{"monospace"}},
		{models.Font{FamilyName: "CMSY8", Serif: true}, []string{"serif"}},
		{models.Font{FamilyName: "Helvetica-Bold", Bold: true}, []string{"sans-serif"}},
	}
	for _, test := range cases {
		names := systemFontCandidates(&test.font)
		for _, want := range test.contains {
			found := false
			for _, name := range names {
				if name == want {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("systemFontCandidates(%q) = %v, 缺少 %q", test.font.FamilyName, names, want)
			}
		}
	}
}

// TestSystemFontCandidatesSkipsGenericForCJK 验证 CJK 逻辑字体不会被套用
// 拉丁通用族，避免中文字体回退成西文字体。
func TestSystemFontCandidatesSkipsGenericForCJK(t *testing.T) {
	names := systemFontCandidates(&models.Font{FamilyName: "方正小标宋_GBK"})
	for _, name := range names {
		if name == "serif" || name == "sans-serif" || name == "monospace" {
			t.Fatalf("CJK 字体不应加入通用族候选: %v", names)
		}
	}
}

// TestShapedTextLineCacheReusesShaping 回归 canvas 原生文字整形的缓存：
// 相同字体/字号/样式/纯色画笔/文本必须复用同一个 *canvas.Text，避免重复
// 整形；渐变画笔不进入缓存（每次返回新对象）。
func TestShapedTextLineCacheReusesShaping(t *testing.T) {
	fontPath := filepath.Join("..", "..", "..", "..", "test", "testdata", "DejaVuSans.ttf")
	if _, err := os.Stat(fontPath); err != nil {
		if _, err := os.Stat("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"); err == nil {
			fontPath = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
		} else {
			t.Skip("缺少 DejaVuSans 测试字体")
		}
	}
	family := canvas.NewFontFamily("shaped-text-line-test")
	if err := family.LoadFontFile(fontPath, canvas.FontRegular); err != nil {
		t.Skipf("字体加载失败: %v", err)
	}

	fonts := &Fonts{Fonts: map[models.StRefID]*canvas.FontFamily{}}
	face := family.Face(12, canvas.Black)

	first := fonts.shapedTextLine(face, "OFD 渲染缓存")
	if first == nil {
		t.Fatal("shapedTextLine 返回 nil")
	}
	second := fonts.shapedTextLine(face, "OFD 渲染缓存")
	if first != second {
		t.Fatal("相同文本未命中整形缓存")
	}
	if *(*uintptr)(unsafe.Pointer(&first)) != *(*uintptr)(unsafe.Pointer(&second)) {
		t.Fatal("缓存返回的不是同一对象")
	}

	// 渐变画笔不缓存，避免不同文本对象互相污染。
	grad := canvas.Grad{}
	grad.Add(0, canvas.Black)
	grad.Add(1, canvas.White)
	faceGrad := family.Face(12, grad.ToLinear(canvas.Point{}, canvas.Point{X: 10, Y: 10}))
	a := fonts.shapedTextLine(faceGrad, "OFD 渲染缓存")
	b := fonts.shapedTextLine(faceGrad, "OFD 渲染缓存")
	if a == b {
		t.Fatal("渐变画笔文字不应进入缓存")
	}
}
