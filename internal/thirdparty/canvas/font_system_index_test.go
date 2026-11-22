package canvas

import (
	"os"
	"path/filepath"
	"testing"

	fontfile "github.com/zc310/ofd/internal/thirdparty/font"
	"golang.org/x/image/font/gofont/goregular"
)

func TestLegacySystemFontIndexIsRebuiltForCollectionMetadata(t *testing.T) {
	systemFonts.Lock()
	previous := systemFonts.SystemFonts
	systemFonts.Unlock()
	defer func() {
		systemFonts.Lock()
		systemFonts.SystemFonts = previous
		systemFonts.Unlock()
	}()
	dir := t.TempDir()
	path := filepath.Join(dir, "regular.ttf")
	if err := os.WriteFile(path, goregular.TTF, 0600); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(dir, "old.gob")
	legacy := &fontfile.SystemFonts{Fonts: map[string]map[fontfile.Style]fontfile.FontMetadata{}}
	legacy.Add(fontfile.FontMetadata{Family: "Stale", Filename: "missing.ttf", Style: fontfile.Regular})
	if err := legacy.Save(index); err != nil {
		t.Fatal(err)
	}
	if err := CacheSystemFonts(index, []string{dir}); err != nil {
		t.Fatal(err)
	}
	if _, ok := FindSystemFontFace("Stale", FontRegular); ok {
		t.Fatal("旧版索引没有重建")
	}
	if got, ok := FindSystemFontFace("Go", FontRegular); !ok || got.Filename != path || got.Index != 0 {
		t.Fatal("重建后的字体索引不可用")
	}
	saved, err := fontfile.LoadSystemFonts(index)
	if err != nil || saved.Version != fontfile.SystemFontIndexVersion {
		t.Fatal("新索引格式版本没有写回")
	}
}

// 请求粗体而系统仅有正体时，按原生样式入槽，Face 再合成请求字重。
func TestLoadSystemFontUsesNativeSlotWhenRequestedBoldIsUnavailable(t *testing.T) {
	systemFonts.Lock()
	previous := systemFonts.SystemFonts
	systemFonts.Unlock()
	t.Cleanup(func() {
		systemFonts.Lock()
		systemFonts.SystemFonts = previous
		systemFonts.Unlock()
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "regular.ttf")
	if err := os.WriteFile(path, goregular.TTF, 0600); err != nil {
		t.Fatal(err)
	}
	if err := CacheSystemFonts(filepath.Join(dir, "index.gob"), []string{dir}); err != nil {
		t.Fatal(err)
	}
	family := NewFontFamily(t.Name())
	if err := family.LoadSystemFont("Go", FontBold); err != nil {
		t.Fatal(err)
	}
	defer family.Destroy()
	if family.fonts[FontRegular] == nil || family.fonts[FontBold] != nil {
		t.Fatal("系统正体被误登记为原生粗体")
	}
	regular := family.Face(12, Black, FontRegular)
	bold := family.Face(12, Black, FontBold)
	if regular.Font.Style() != FontRegular || regular.FauxBold != 0 ||
		bold.Font != regular.Font || bold.FauxBold != FontBold.FauxWeight()-FontRegular.FauxWeight() {
		t.Fatal("原生样式与请求字重的合成不一致")
	}
}
