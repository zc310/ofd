package canvasfont

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"golang.org/x/image/font/gofont/goregular"
)

func TestDefaultCJKFontFilesTryLinuxPackagesWithoutWindowsFonts(t *testing.T) {
	// 用带中文 cmap 的小字体验证候选查找，不依赖宿主机安装的字体。
	data, err := fontrepair.RepairWithGlyphs(goregular.TTF, []fontrepair.GlyphMapping{{Rune: '中', Glyph: 1}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"NotoSansCJK-Regular.ttc", "NotoSerifCJK-Regular.ttc", "wqy-zenhei.ttc", "wqy-microhei.ttc"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			nested := filepath.Join(dir, "fonts", "cjk")
			if err := os.MkdirAll(nested, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(nested, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
			// 更早命中的候选损坏或仅覆盖西文时，必须继续查找 Linux 字体。
			if err := os.WriteFile(filepath.Join(dir, "simhei.ttf"), []byte("invalid font"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "simsun.ttc"), goregular.TTF, 0o600); err != nil {
				t.Fatal(err)
			}
			family := canvas.NewFontFamily("default")
			if !loadDefaultCJKFontFiles(family, []string{dir}) || !fontFamilySupportsCJK(family) {
				t.Fatal("未从 Linux 字体包候选加载中文字体")
			}
		})
	}
}

func TestDefaultCJKFontFilesRejectLatinOnlyFont(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "simsun.ttc"), goregular.TTF, 0o600); err != nil {
		t.Fatal(err)
	}
	if loadDefaultCJKFontFiles(canvas.NewFontFamily("default"), []string{dir}) {
		t.Fatal("仅覆盖西文的字体不能终止中文候选搜索")
	}
}

func TestDefaultCJKFontFilesLoadInstalledLinuxCollections(t *testing.T) {
	for _, path := range []string{
		"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
		"/usr/share/fonts/opentype/noto/NotoSerifCJK-Regular.ttc",
		"/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
	} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					t.Skipf("未安装字体 %s", path)
				}
				t.Fatal(err)
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, filepath.Base(path)), data, 0o600); err != nil {
				t.Fatal(err)
			}
			family := canvas.NewFontFamily("default")
			if !loadDefaultCJKFontFiles(family, []string{dir}) || !familyCoversText(family, textObjectWith("中文与 Latin 混排"), FontRegular) {
				t.Fatal("Linux 字体集合未能作为默认字体覆盖中英混排")
			}
		})
	}
}
