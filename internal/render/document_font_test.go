package render

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/tdewolff/canvas"
	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

// useFallback 注册全局回退字体并将其应用到指定字体上下文。
func useFallback(t *testing.T, fonts *Fonts, data []byte, family string, style canvas.FontStyle) {
	t.Helper()
	if err := RegisterFallbackFont(data, family, style); err != nil {
		t.Fatal(err)
	}
	if err := fonts.UseFallbackFont(family); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFontConcurrentUsesOneCachedFamily(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "intro.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	fonts := NewFonts(ofd.Documents[0])
	const workers = 16
	families := make(chan *canvas.FontFamily, workers)
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
		if first == nil {
			first = family
			continue
		}
		if family != first {
			t.Fatal("同一字体的并发加载创建了多个字体族")
		}
	}
}

func TestSystemFontCacheReusesFamilyAndRenderLock(t *testing.T) {
	first, ok := loadCachedSystemFont("DejaVu Sans", canvas.FontRegular)
	if !ok {
		t.Skip("DejaVu Sans is unavailable")
	}
	second, ok := loadCachedSystemFont("DejaVu Sans", canvas.FontRegular)
	if !ok {
		t.Fatal("cached DejaVu Sans could not be loaded")
	}
	if first != second {
		t.Fatal("system font cache created multiple font families")
	}

	firstFonts := NewFonts(nil)
	secondFonts := NewFonts(nil)
	if firstFonts.renderLock(first) != secondFonts.renderLock(second) {
		t.Fatal("system font cache did not share the render lock")
	}
}

func TestFallbackFontRegistryReusesFamilyAndRenderLock(t *testing.T) {
	data, err := os.ReadFile("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf")
	if err != nil {
		t.Skipf("DejaVu Sans is unavailable: %v", err)
	}
	firstFonts := NewFonts(nil)
	secondFonts := NewFonts(nil)
	useFallback(t, firstFonts, data, "ProcessFallback", canvas.FontRegular)
	useFallback(t, secondFonts, data, "ProcessFallback", canvas.FontRegular)
	first := firstFonts.fallbacks["ProcessFallback"]
	second := secondFonts.fallbacks["ProcessFallback"]
	if firstFonts.fallbacks["ProcessFallback"] != secondFonts.fallbacks["ProcessFallback"] {
		t.Fatal("fallback font registry created multiple font families")
	}
	if firstFonts.renderLock(first) != secondFonts.renderLock(second) {
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
	useFallback(t, firstFonts, font, "LockedFamily", canvas.FontRegular)
	useFallback(t, secondFonts, font, "LockedFamily", canvas.FontRegular)
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
	useFallback(t, fonts, font, "DefaultFallback", canvas.FontRegular)
	// 缺字体的样式也必须匹配到同一个全局家族（Bold 落回 Regular face）。
	regular, ok := selectFallback(fonts.fallbackFaces, canvas.FontRegular)
	if !ok {
		t.Fatal("no fallback face available for Regular")
	}
	bold, ok := selectFallback(fonts.fallbackFaces, canvas.FontBold)
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
	useFallback(t, fonts, regular, "ProcessStyleFallback", canvas.FontRegular)
	useFallback(t, fonts, bold, "ProcessStyleFallback", canvas.FontBold)
	family := fonts.fallbacks["ProcessStyleFallback"]
	if family.Face(12, canvas.Black, canvas.FontRegular).Font == family.Face(12, canvas.Black, canvas.FontBold).Font {
		t.Fatal("different fallback styles did not keep separate font faces")
	}
}

func TestSelectFallbackPrefersMatchingFontFamily(t *testing.T) {
	generic := canvas.NewFontFamily("OFD-NotoSansSC")
	kaiti := canvas.NewFontFamily("楷体")
	fallback, ok := selectFallback([]fallbackFace{
		{family: generic, style: canvas.FontRegular, name: "OFD-NotoSansSC"},
		{family: kaiti, style: canvas.FontRegular, name: "楷体"},
	}, canvas.FontRegular, "KaiTi")
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
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "intro.ofd"))
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
		if family == defaultFamily || !fontFamilyUsable(family) {
			t.Fatalf("font %d was not loaded as an embedded usable font", id)
		}
	}
	family, err := fonts.LoadFont(models.StRefID(128))
	if err != nil {
		t.Fatal(err)
	}
	face := family.Face(1, canvas.Black)
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
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
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
	if !fontFamilyUsable(family) {
		t.Fatal("font 115 is not usable")
	}
	defaultFamily, _ := defaultFallbackFont()
	if family == defaultFamily {
		t.Fatal("font 115 fell back to the default font")
	}
}

func TestAnoAnnotationFontWithoutFileDoesNotUseSubsetFont(t *testing.T) {
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
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
	ofd, err := parser.NewOFD(filepath.Join("..", "..", "test", "testdata", "ano.ofd"))
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
	useFallback(t, fonts, regular, "fallback", canvas.FontRegular)
	useFallback(t, fonts, bold, "fallback", canvas.FontBold)

	regularFace := fonts.fallbacks["fallback"].Face(12, canvas.Black, canvas.FontRegular)
	boldFace := fonts.fallbacks["fallback"].Face(12, canvas.Black, canvas.FontBold)
	if regularFace == nil || boldFace == nil || regularFace.Font == boldFace.Font {
		t.Fatal("regular and bold fallback faces were not kept separately")
	}
}
