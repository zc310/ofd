package canvasfont

import (
	"archive/zip"
	"bytes"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"golang.org/x/image/font/gofont/goregular"
)

func fontOriginFonts(t *testing.T, family string, data []byte) *Fonts {
	t.Helper()
	files := map[string][]byte{
		"OFD.xml":            []byte(`<OFD><DocBody><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`),
		"Doc_0/Document.xml": []byte(`<Document><CommonData><DocumentRes>Res.xml</DocumentRes></CommonData><Pages/></Document>`),
		"Doc_0/Res.xml":      []byte(fmt.Sprintf(`<Res><Fonts><Font ID="1" FontName="OriginProbe" FamilyName="%s"><FontFile>primary.ttf</FontFile></Font></Fonts></Res>`, family)),
	}
	if data != nil {
		files["Doc_0/primary.ttf"] = data
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for path, data := range files {
		entry, err := writer.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(buffer.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	fonts := NewFonts(ofd.Documents[0])
	t.Cleanup(func() { fonts.Close(); _ = ofd.Close() })
	return fonts
}

func setOriginTestDefault(t *testing.T, ready bool) *canvas.FontFamily {
	t.Helper()
	defaultFallbackFont()
	family := canvas.NewFontFamily("origin-default")
	if err := family.LoadFont(goregular.TTF, 0, canvas.FontRegular); err != nil {
		t.Fatal(err)
	}
	fontCacheMu.Lock()
	previous := fallbackRegistry[defaultFallbackKey]
	fallbackRegistry[defaultFallbackKey] = &fallbackRegistration{family: family, ready: ready, renderMu: &sync.Mutex{}}
	fontCacheMu.Unlock()
	t.Cleanup(func() {
		fontCacheMu.Lock()
		fallbackRegistry[defaultFallbackKey] = previous
		fontCacheMu.Unlock()
	})
	return family
}

func TestMissingOrBrokenEmbeddedFontDoesNotReportDefaultFallbackAsEmbedded(t *testing.T) {
	for _, source := range [][]byte{nil, []byte("broken font")} {
		fonts := fontOriginFonts(t, "OFD-No-Such-Origin-Font", source)
		expected := setOriginTestDefault(t, true)
		loaded, err := fonts.LoadFont(1)
		if err != nil {
			t.Fatal(err)
		}
		if loaded != expected || fonts.HasLoadedEmbeddedFont(1) {
			t.Fatal("内嵌字体缺失或损坏后，默认回退被误报为内嵌加载成功")
		}
	}
}

func TestMissingEmbeddedFontDoesNotReportSystemFallbackAsEmbedded(t *testing.T) {
	if !systemFontLookupUsable() || runtime.GOOS == "android" {
		t.Skip("该平台不按字体族查询桌面系统字体")
	}
	if _, ok := canvas.FindSystemFont("sans-serif", canvas.FontRegular); !ok {
		t.Skip("没有可用的 sans-serif 系统字体")
	}
	fonts := fontOriginFonts(t, "sans-serif", nil)
	if _, err := fonts.LoadFont(1); err != nil {
		t.Fatal(err)
	}
	if len(fonts.systemFonts) == 0 {
		t.Fatal("测试没有经过系统磁盘字体加载")
	}
	if fonts.HasLoadedEmbeddedFont(1) {
		t.Fatal("系统回退被误报为内嵌字体")
	}
}

func TestMissingEmbeddedFontDoesNotReportRegisteredFallbackAsEmbedded(t *testing.T) {
	fonts := fontOriginFonts(t, "OFD-No-Such-Origin-Font", nil)
	name := t.Name()
	useFallback(t, fonts, goregular.TTF, name, FontRegular)
	t.Cleanup(func() { _ = UnregisterFallbackFont(name) })
	if _, err := fonts.LoadFont(1); err != nil {
		t.Fatal(err)
	}
	if fonts.HasLoadedEmbeddedFont(1) || fonts.FallbackFontFamily(1) != name {
		t.Fatal("注册回退的来源标记不正确")
	}
}

func TestEmbeddedOriginIsInvalidatedAndRestoredWithLoadedFont(t *testing.T) {
	fonts := fontOriginFonts(t, "OriginProbe", goregular.TTF)
	assertLoaded := func() {
		t.Helper()
		if _, err := fonts.LoadFont(1); err != nil {
			t.Fatal(err)
		}
		if !fonts.HasLoadedEmbeddedFont(1) {
			t.Fatal("实际加载的内嵌字体没有记录来源")
		}
	}
	if fonts.HasLoadedEmbeddedFont(1) {
		t.Fatal("尚未加载的字体不能报告内嵌加载成功")
	}
	assertLoaded()
	fonts.RegisterGlyphs(1, map[rune]uint16{'\uE000': 1})
	if fonts.HasLoadedEmbeddedFont(1) {
		t.Fatal("字形变更后仍保留旧字体的来源状态")
	}
	assertLoaded()
	name := t.Name()
	useFallback(t, fonts, goregular.TTF, name, FontRegular)
	t.Cleanup(func() { _ = UnregisterFallbackFont(name) })
	if fonts.HasLoadedEmbeddedFont(1) {
		t.Fatal("应用回退清空加载缓存后仍报告内嵌已加载")
	}
	assertLoaded()
	fonts.RemoveFallbackFont(name)
	if fonts.HasLoadedEmbeddedFont(1) {
		t.Fatal("移除回退清空加载缓存后仍报告内嵌已加载")
	}
	assertLoaded()
	fonts.Close()
	if fonts.HasLoadedEmbeddedFont(1) {
		t.Fatal("文档关闭后仍报告内嵌字体已加载")
	}
}

func TestFailedFontLoadDoesNotPublishEmbeddedOrigin(t *testing.T) {
	fonts := fontOriginFonts(t, "OFD-No-Such-Origin-Font", nil)
	setOriginTestDefault(t, false)
	if _, err := fonts.LoadFont(1); err == nil {
		t.Fatal("所有来源都不可用时应加载失败")
	}
	if fonts.HasLoadedEmbeddedFont(1) {
		t.Fatal("失败的加载发布了内嵌来源")
	}
}
