package canvas

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"unsafe"

	"crypto/sha256"
	"github.com/tdewolff/canvas"
	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"strings"
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
	first, ok := loadCachedSystemFont(NewFonts(nil), "DejaVu Sans", FontRegular)
	if !ok {
		t.Skip("DejaVu Sans is unavailable")
	}
	second, ok := loadCachedSystemFont(NewFonts(nil), "DejaVu Sans", FontRegular)
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
	fonts := NewFonts(nil)
	first, err := loadCachedEmbeddedFont(fonts, "EmbeddedCache", data, FontRegular, nil)
	if err != nil {
		t.Fatalf("加载嵌入字体失败: %v", err)
	}
	second, err := loadCachedEmbeddedFont(fonts, "EmbeddedCache", data, FontRegular, nil)
	if err != nil {
		t.Fatalf("复用嵌入字体失败: %v", err)
	}
	if first != second {
		t.Fatal("同一文档内嵌字体缓存创建了多个字体族")
	}
	if fonts.RenderLock(first) != fonts.RenderLock(second) {
		t.Fatal("同一文档内同一字体族应共享渲染锁")
	}
	mapped, err := loadCachedEmbeddedFont(fonts, "EmbeddedCache", data, FontRegular, []fontfix.GlyphMapping{{Rune: 'A', Glyph: 1}})
	if err != nil {
		t.Fatalf("加载带映射的嵌入字体失败: %v", err)
	}
	if mapped == first {
		t.Fatal("不同字形映射不应复用同一字体族")
	}
	// 内嵌字体缓存绑定在文档上：不同文档各有各的字体族。
	if other, err := loadCachedEmbeddedFont(NewFonts(nil), "EmbeddedCache", data, FontRegular, nil); err != nil {
		t.Fatalf("加载内嵌字体失败: %v", err)
	} else if other == first {
		t.Fatal("不同文档不应共享内嵌字体族")
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

func TestRemoveFallbackFontRestoresContext(t *testing.T) {
	data, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans is unavailable: %v", err)
	}
	fonts := NewFonts(nil)
	useFallback(t, fonts, data, "RemovableFallback", FontRegular)
	if fonts.fallbacks["RemovableFallback"] == nil {
		t.Fatal("回退字体未登记")
	}
	fonts.RemoveFallbackFont("RemovableFallback")
	if fonts.fallbacks["RemovableFallback"] != nil {
		t.Fatal("移除后仍保留回退字体族")
	}
	for _, face := range fonts.fallbackFaces {
		if face.name == "RemovableFallback" {
			t.Fatal("移除后仍保留回退字体面")
		}
	}
	if fonts.FallbackFontFamily(0) == "RemovableFallback" {
		t.Fatal("移除后仍为字体选择该回退族")
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

// TestImageNotFoundLeftTextFontLoads 回归：testImageNotFound.ofd 左侧正文使用的
// 内嵌子集字体（font_6.ttf / Font69.ttf）hmtx 尾部多出字节，之前会导致整个字体
// 加载失败并回退到系统字体、正文渲染成乱码；修复后必须按内嵌字体加载并登记好
// 页面字形映射。
func TestImageNotFoundLeftTextFontLoads(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "..", "..", "test", "testdata", "ofdrw", "testImageNotFound.ofd"))
	if err != nil {
		t.Skipf("样例不可用: %v", err)
	}
	defer ofd.Close()

	doc := ofd.Documents[0]
	page, err := doc.GetPage(0)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := page.AcquireLease()
	if err != nil {
		t.Fatal(err)
	}
	content := lease.Content()
	fonts := NewFonts(doc)
	fonts.RegisterPageGlyphs(doc, page, content)
	lease.Release()

	family, err := fonts.LoadFont(6)
	if err != nil {
		t.Fatal(err)
	}
	if !fonts.HasLoadedEmbeddedFont(6) {
		t.Fatal("字体 6 应以内嵌字体加载，而不是回退到系统字体")
	}
	face := family.(*canvas.FontFamily).Face(1, canvas.Black)
	if face == nil || face.Font == nil {
		t.Fatal("字体 6 的字体面为空")
	}
	if got := face.Font.GlyphIndex('矿'); got == 0 {
		t.Fatal("字体 6 在登记页面字形映射后仍缺少“矿”的字形")
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

// TestSystemFontCacheKeyedByFileNotName 同一字体文件经不同逻辑名加载时只解析一次。
//
// 缓存键此前是 (name, style)，而 name 是逻辑名、文件是系统匹配的结果，两者不是
// 一一对应：多个逻辑名会解析到同一个字体文件，于是同一个文件被读入并解析多份。
// 中文文档上尤其明显——实测渲染一个 5 页文档时 simkai.ttf 被读了两遍，各持一份
// 11.8 MB 的字体数据与解析结果。
//
// 这条守住"按文件身份作键"：两个不同的逻辑名解析到同一文件时必须命中同一条目。
func TestSystemFontCacheKeyedByFileNotName(t *testing.T) {
	file := findTestFontFile(t)

	// 同一个文件，用两个不同的逻辑名走按路径加载的入口。
	first, ok := loadCachedFontFile(NewFonts(nil), file, "AliasOne", FontRegular)
	if !ok {
		t.Fatalf("加载字体文件失败: %s", file)
	}
	second, ok := loadCachedFontFile(NewFonts(nil), file, "AliasTwo", FontRegular)
	if !ok {
		t.Fatal("第二次加载字体文件失败")
	}
	if first != second {
		t.Fatal("同一字体文件经不同逻辑名加载时创建了多个字体族")
	}
	if first.Name() != "AliasOne" {
		t.Logf("复用到的字体族名是 %q（以首次加载的为准）", first.Name())
	}
}

// TestSystemFontCacheKeyIncludesFileIdentity 文件换了内容必须重新解析。
//
// 键里带 size 与 mtime 就是为了这个：同一路径上被替换掉的字体（测试环境换字体、
// 用户升级字体包）不能让旧解析结果继续生效，否则会一直用旧字形渲染。
func TestSystemFontCacheKeyIncludesFileIdentity(t *testing.T) {
	file := findTestFontFile(t)
	first, ok := systemFontKeyForFile(file, FontRegular)
	if !ok {
		t.Fatalf("无法为 %s 构造缓存键", file)
	}
	second, ok := systemFontKeyForFile(file, FontRegular)
	if !ok {
		t.Fatal("同一文件两次构造键失败")
	}
	if first != second {
		t.Error("同一文件两次构造的键不相等，键里应含稳定的 size 与 mtime")
	}
	// 不存在的文件不应产生可用键，否则会把加载失败缓存下来。
	if _, ok := systemFontKeyForFile(file+".nonexistent", FontRegular); ok {
		t.Error("不存在的文件也返回了可用键")
	}
}

// TestSystemFontAndFileCacheShareEntries 两条加载路径必须共用一张缓存表。
//
// loadCachedSystemFont 走族名匹配，loadCachedFontFile 走确切路径；两者解析到
// 同一文件时应当合并，否则"经族名加载"与"按路径加载"仍会各存一份。
func TestSystemFontAndFileCacheShareEntries(t *testing.T) {
	file := findTestFontFile(t)
	viaFile, ok := loadCachedFontFile(NewFonts(nil), file, "SharedEntry", FontRegular)
	if !ok {
		t.Skipf("字体文件不可用: %s", file)
	}
	byPath, ok := systemFontKeyForFile(file, FontRegular)
	if !ok {
		t.Fatal("构造键失败")
	}
	entry := systemFontCache[byPath]
	if entry == nil {
		t.Fatal("按路径加载的字体没有进缓存表")
	}
	if entry.family != viaFile {
		t.Error("缓存表里的字体族与返回的不一致")
	}
}

// findTestFontFile 找一个可用的测试字体文件。
func findTestFontFile(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{
		"/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
		"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
		"/usr/share/fonts/TTF/DejaVuSans.ttf",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	// 扫常见字体目录找任意 .ttf，不引入 font.DefaultFontDirs 以免为测试多一个依赖。
	for _, dir := range []string{
		"/usr/share/fonts", "/usr/local/share/fonts",
		filepath.Join(os.Getenv("HOME"), ".fonts"),
		filepath.Join(os.Getenv("HOME"), ".local/share/fonts"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if name := e.Name(); strings.HasSuffix(name, ".ttf") {
				return filepath.Join(dir, name)
			}
		}
	}
	t.Skip("找不到可用的测试字体文件")
	return ""
}

// TestReleaseSystemFontsHonorsRefCount 保护缓存按引用计数淘汰：一个实例释放时
// 若仍有其它实例持有同一字体族，系统字体缓存条目与渲染锁都不能被删。无条件删除
// 会让去重失效，并让同一字体族在缓存表外被再次加载、拿到另一把渲染锁并发绘制。
func TestReleaseSystemFontsHonorsRefCount(t *testing.T) {
	file := copyTestFont(t)
	first := NewFonts(nil)
	second := NewFonts(nil)
	family, ok := loadCachedFontFile(first, file, "RefCount", FontRegular)
	if !ok {
		t.Skipf("字体文件不可用: %s", file)
	}
	other, ok := loadCachedFontFile(second, file, "RefCount", FontRegular)
	if !ok || other != family {
		t.Fatal("同一文件未能复用同一字体族")
	}
	key, _ := systemFontKeyForFile(file, FontRegular)

	releaseSystemFonts(first)
	if systemFontCache[key] == nil {
		t.Error("仍有实例持有时系统字体缓存条目被删除")
	}
	if fontRenderLocks[family] == nil {
		t.Error("仍有实例持有时渲染锁被删除")
	}

	releaseSystemFonts(second)
	if systemFontCache[key] != nil {
		t.Error("无人持有后系统字体缓存条目未删除")
	}
	if fontRenderLocks[family] != nil {
		t.Error("无人持有后渲染锁未删除")
	}
}

// TestReleaseSystemFontsIgnoresFontMapReset 保护释放不依赖 p.Fonts：字体表被
// RegisterGlyphs/UseFallbackFont 重置后，已加载过的字体族会从表中消失，释放必须
// 仍按持有者解除引用计数，否则缓存条目永远淘汰不掉。
func TestReleaseSystemFontsIgnoresFontMapReset(t *testing.T) {
	file := copyTestFont(t)
	fonts := NewFonts(nil)
	family, ok := loadCachedFontFile(fonts, file, "Orphan", FontRegular)
	if !ok {
		t.Skipf("字体文件不可用: %s", file)
	}
	// 模拟字体表被重置：此时已无法从 p.Fonts 反查到该字体族。
	fonts.mu.Lock()
	fonts.Fonts = map[models.StRefID]*canvas.FontFamily{}
	fonts.mu.Unlock()

	releaseSystemFonts(fonts)
	key, _ := systemFontKeyForFile(file, FontRegular)
	if systemFontCache[key] != nil || fontRenderLocks[family] != nil {
		t.Fatal("字体表重置后引用计数未解除，缓存条目与渲染锁仍在")
	}
}

// TestCloseDropsEmbeddedFontCaches 保护内嵌字体与修复器缓存绑定在文档上：
// Close 后本实例的缓存必须被丢弃，不再钉住解析后的重资源。
func TestCloseDropsEmbeddedFontCaches(t *testing.T) {
	data, err := os.ReadFile(findTestFontFile(t))
	if err != nil {
		t.Skipf("读取测试字体失败: %v", err)
	}
	fonts := NewFonts(nil)
	if _, err := loadCachedEmbeddedFont(fonts, "CloseEmbedded", data, FontRegular, nil); err != nil {
		t.Fatalf("加载内嵌字体失败: %v", err)
	}
	_ = embeddedRepairer(fonts, data)

	fonts.embeddedMu.Lock()
	fontsCount := len(fonts.embeddedFonts)
	repairerCount := len(fonts.embeddedRepairers)
	fonts.embeddedMu.Unlock()
	if fontsCount == 0 || repairerCount == 0 {
		t.Fatalf("内嵌字体/修复器未进入实例缓存: fonts=%d repairers=%d", fontsCount, repairerCount)
	}

	fonts.Close()
	fonts.embeddedMu.Lock()
	defer fonts.embeddedMu.Unlock()
	if len(fonts.embeddedFonts) != 0 || len(fonts.embeddedRepairers) != 0 {
		t.Fatalf("Close 后内嵌字体/修复器缓存未被丢弃: fonts=%d repairers=%d",
			len(fonts.embeddedFonts), len(fonts.embeddedRepairers))
	}
}

// copyTestFont 把测试字体复制到独立临时路径，使缓存键（含路径）不与其它测试共享。
func copyTestFont(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(findTestFontFile(t))
	if err != nil {
		t.Skipf("读取测试字体失败: %v", err)
	}
	path := filepath.Join(t.TempDir(), "refcount.ttf")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("写入临时字体失败: %v", err)
	}
	return path
}

// TestSystemFontCacheStoresOneEntryPerFile 同一个字体文件在缓存里只存一份。
//
// 这是一次性排查的结论固化下来的：系统字体缓存的键曾是 (逻辑名, 样式)，而逻辑名
// 与文件不是一一对应，多个名字会解析到同一个文件，于是同一份字体被读入并解析多
// 遍——一个 CJK 字体约 50 MB，重复一份就是白付 50 MB 和一次解析。
//
// 直接数缓存里每个文件路径有几条即可判定，不需要暴露任何诊断接口。
func TestSystemFontCacheStoresOneEntryPerFile(t *testing.T) {
	file := findTestFontFile(t)

	// 同一个文件用两个不同的逻辑名加载。
	if _, ok := loadCachedFontFile(NewFonts(nil), file, "AliasOne", FontRegular); !ok {
		t.Skipf("字体文件不可用: %s", file)
	}
	if _, ok := loadCachedFontFile(NewFonts(nil), file, "AliasTwo", FontRegular); !ok {
		t.Fatal("第二次加载字体文件失败")
	}
	// 样式不进这项检查：不同样式是不同的渲染需求，本就该各存一条。这里只守
	// "同一文件 + 同一样式"不被重复存储。

	fontCacheMu.Lock()
	defer fontCacheMu.Unlock()
	perFile := map[string]int{}
	for key := range systemFontCache {
		perFile[key.path]++
	}
	for path, n := range perFile {
		if n > 1 {
			t.Errorf("字体文件 %s 在缓存里有 %d 条，期望 1 条", filepath.Base(path), n)
		}
	}
}

// TestEmbeddedFontsArePerDocument 内嵌字体按文档持有，不进全局表。
//
// 内嵌字体是文档自带资源，每份文档的字节都不同，跨文档复用价值低；而放进全局
// 缓存意味着永远无法回收。改成按 Fonts 实例持有后随文档关闭释放。
//
// 这条守着"别把它挪回全局"：那是内存问题，不是风格问题。
func TestEmbeddedFontsArePerDocument(t *testing.T) {
	fontsA := NewFonts(nil)
	fontsB := NewFonts(nil)

	if fontsA.embeddedFonts == nil || fontsB.embeddedFonts == nil {
		t.Fatal("两个实例都应有独立的内嵌字体表")
	}
	key := embeddedFontKey{name: "X", dataDigest: sha256.Sum256([]byte("x"))}
	fontsA.embeddedFonts[key] = nil
	if _, shared := fontsB.embeddedFonts[key]; shared {
		t.Error("两个文档实例的内嵌字体表是同一个")
	}
}

// TestCloseReleasesEmbeddedFonts 关闭后内嵌字体与修复器一并释放。
//
// 内嵌字体的解析结果（含 fontfix.Repairer 持有的修复后字体与 cmap）只对本实例
// 有意义，文档关闭即成垃圾；不清掉的话它们会随 Fonts 实例一直活着。
func TestCloseReleasesEmbeddedFonts(t *testing.T) {
	fonts := NewFonts(nil)
	fonts.embeddedRepairers[[sha256.Size]byte{7}] = nil
	fonts.Fonts[models.StRefID(1)] = nil

	fonts.Close()

	if len(fonts.embeddedFonts) != 0 {
		t.Errorf("关闭后仍持有 %d 个内嵌字体族", len(fonts.embeddedFonts))
	}
	if len(fonts.embeddedRepairers) != 0 {
		t.Errorf("关闭后仍持有 %d 个修复器", len(fonts.embeddedRepairers))
	}
	// 幂等：重复关闭不应出问题。
	fonts.Close()
}
