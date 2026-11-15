package canvasfont

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"

	"github.com/zc310/ofd/internal/fileutil"
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"golang.org/x/image/font/gofont/goregular"
)

func TestDefaultFontIsLoadedOnlyWhenNeeded(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("WASM 环境不支持启动隔离测试子进程")
	}
	// 用独立进程验证冷启动，避免重置全局 sync.Once 或受其他测试顺序影响。
	const childEnv = "OFD_TEST_DEFAULT_FONT_LAZY"
	if os.Getenv(childEnv) != "1" {
		t.Setenv(childEnv, "1")
		command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestDefaultFontIsLoadedOnlyWhenNeeded$")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("冷启动子进程失败：%v\n%s", err, output)
		}
		return
	}
	assertUninitialized := func() {
		t.Helper()
		fontCacheMu.Lock()
		initialized := fallbackRegistry[defaultFallbackKey] != nil
		fontCacheMu.Unlock()
		if initialized {
			t.Fatal("未使用默认字体时不应初始化它")
		}
	}
	fonts, _ := sourceCacheFonts(t)
	assertUninitialized()
	fonts.ForEachFont(func(id models.StID, _ *models.Font) bool {
		if _, err := fonts.LoadFont(models.StRefID(id)); err != nil {
			t.Fatal(err)
		}
		return true
	})
	assertUninitialized()
	fallbackFonts := NewFonts(nil)
	defer fallbackFonts.Close()
	useFallback(t, fallbackFonts, goregular.TTF, t.Name(), FontRegular)
	defer func() {
		if err := UnregisterFallbackFont(t.Name()); err != nil {
			t.Error(err)
		}
	}()
	if _, err := fallbackFonts.LoadFont(1); err != nil {
		t.Fatal(err)
	}
	assertUninitialized()

	// 真正需要默认字体时，并发首次查询仍应共享同一个完整发布的字体族。
	var wg sync.WaitGroup
	families := make([]*canvas.FontFamily, 8)
	for i := range families {
		wg.Go(func() { families[i], _ = defaultFallbackFont() })
	}
	wg.Wait()
	for _, family := range families {
		if family == nil || family != families[0] {
			t.Fatal("并发初始化没有共享默认字体族")
		}
	}
}

func TestDefaultFontFilesKeepCandidatePriorityAndIgnoreCase(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a/NotoSansCJK-Regular.ttc", "z/SIMHEI.TTF"} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// 目录遍历先遇到 Noto，但返回顺序仍以候选优先级为准。
	got := findDefaultFontFiles([]string{filepath.Join(dir, "missing"), dir}, []string{"simhei.ttf", "missing.ttf", "NotoSansCJK-Regular.ttc"})
	want := []string{filepath.Join(dir, "z/SIMHEI.TTF"), "", filepath.Join(dir, "a/NotoSansCJK-Regular.ttc")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("候选路径 = %v，期望 %v", got, want)
	}
}

func TestDefaultFontFilesKeepFirstDirectoryMatch(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	for _, dir := range []string{first, second} {
		if err := os.WriteFile(filepath.Join(dir, "simhei.ttf"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := findDefaultFontFiles([]string{first, second}, []string{"simhei.ttf"})
	if got[0] != filepath.Join(first, "simhei.ttf") {
		t.Fatalf("后续目录覆盖了已找到的字体：%v", got)
	}
}

func TestDefaultFontFilesFollowRootAndFileSymlinks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "font.data")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "fonts")
	if err := os.Symlink(dir, root); err != nil {
		t.Skipf("环境不支持创建符号链接：%v", err)
	}
	for name, target := range map[string]string{"simhei.ttf": path, "simsun.ttc": dir, "simkai.ttf": filepath.Join(dir, "missing")} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	got := findDefaultFontFiles([]string{root}, []string{"simhei.ttf", "simsun.ttc", "simkai.ttf"})
	want := []string{filepath.Join(root, "simhei.ttf"), "", ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("链接候选路径 = %v，期望 %v", got, want)
	}
}

func BenchmarkDefaultFontFileSearch(b *testing.B) {
	dir := b.TempDir()
	for i := range 32 {
		nested := filepath.Join(dir, fmt.Sprintf("family-%02d", i))
		if err := os.MkdirAll(nested, 0o755); err != nil {
			b.Fatal(err)
		}
		for j := range 8 {
			if err := os.WriteFile(filepath.Join(nested, fmt.Sprintf("font-%02d.ttf", j)), nil, 0o600); err != nil {
				b.Fatal(err)
			}
		}
	}
	names := []string{"simhei.ttf", "simfang.ttf", "simsun.ttc", "simkai.ttf", "NotoSansCJK-Regular.ttc", "NotoSerifCJK-Regular.ttc", "wqy-zenhei.ttc", "wqy-microhei.ttc"}
	if err := os.WriteFile(filepath.Join(dir, names[len(names)-1]), nil, 0o600); err != nil {
		b.Fatal(err)
	}
	dirs := []string{dir}
	b.Run("PerCandidate", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			for _, name := range names {
				if _, err := fileutil.FindFirstFileInDirs(dirs, name); err == nil {
					break
				}
			}
		}
	})
	b.Run("SingleScan", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			findDefaultFontFiles(dirs, names)
		}
	})
}

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
