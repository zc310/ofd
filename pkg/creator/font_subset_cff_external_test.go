package creator

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	fontfile "github.com/zc310/font"
	"github.com/zc310/ofd/internal/fontrepair"
	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
)

// 外部字体仅供本地回归验证，不随仓库分发；显式路径优先于系统候选。
func creatorExternalMultiFDCFF(t *testing.T, paths []string) []byte {
	t.Helper()
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("读取 CFF 测试字体 %q: %v", path, err)
		}
		if isFontCollection(data) {
			data, err = fontrepair.ExtractCollectionFace(data, 0)
			if err != nil {
				t.Fatalf("提取 CFF 测试字体 %q: %v", path, err)
			}
		}
		face, err := fontfile.ParseSFNT(data, 0)
		if err != nil {
			t.Fatalf("解析 CFF 测试字体 %q: %v", path, err)
		}
		if face.CFF == nil || face.CFF.NumFontDicts() < 2 {
			t.Fatalf("CFF 测试字体 %q 必须包含多个 Font DICT", path)
		}
		t.Logf("本地 CFF 字体（%d 个 Font DICT）: %s", face.CFF.NumFontDicts(), path)
		return data
	}
	t.Skipf("本地 CFF 测试字体不存在（候选路径 %q）；可设置 OFD_CFF_TEST_FONT 指向多 Font DICT OTF/TTC", paths)
	return nil
}

// 真实中文字体含有合成小字体未覆盖的子程序，需同时保护重排后的轮廓和度量。
func TestExternalMultiFDCFFSubsetPreservesGlyphOutlinesAndMetrics(t *testing.T) {
	paths := []string{os.Getenv("OFD_CFF_TEST_FONT")}
	if paths[0] == "" {
		paths = []string{
			"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
			"/usr/share/fonts/opentype/noto/NotoSansCJKjp-Regular.otf",
			"/usr/share/fonts/opentype/noto/NotoSansJP-Regular.otf",
		}
	}
	data := creatorExternalMultiFDCFF(t, paths)
	input := bytes.Clone(data)
	original, err := sfnt.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	originalCFF, err := fontfile.ParseSFNT(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	usage := &fontUsage{runes: []rune("中文⽂（），。ABC123")}
	var buffer sfnt.Buffer
	for _, r := range usage.runes {
		glyph, err := original.GlyphIndex(&buffer, r)
		if err != nil || glyph == 0 {
			t.Fatalf("测试字体缺少字符 %q 或查询失败: %v", r, err)
		}
	}
	// 显式 GID 不要求对应正文字符；非顺序选择以覆盖不同区域的字形及其 FDSelect。
	usage.glyphs = []uint16{uint16(original.NumGlyphs() - 1), uint16(original.NumGlyphs() / 2), 2}
	resource := Font{Name: "MultiFDCFF", Data: data}
	if err := subsetFont(&resource, usage); err != nil {
		t.Fatalf("多 Font DICT 字体子集化失败: %v", err)
	}
	result, err := sfnt.Parse(resource.Data)
	if err != nil {
		t.Fatal(err)
	}
	resultCFF, err := fontfile.ParseSFNT(resource.Data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if resource.Format != "otf" || resultCFF.CFF == nil || result.NumGlyphs() >= original.NumGlyphs() || result.NumGlyphs() != len(usage.remap) {
		t.Fatalf("未生成有效 CFF 子集: format=%q glyphs=%d original=%d remap=%d", resource.Format, result.NumGlyphs(), original.NumGlyphs(), len(usage.remap))
	}
	for _, r := range usage.runes {
		before, _ := original.GlyphIndex(&buffer, r)
		after, err := result.GlyphIndex(&buffer, r)
		mapped, ok := usage.remap[uint16(before)]
		if err != nil || after == 0 || !ok || int(after) != mapped {
			t.Fatalf("字符 %q 的 cmap 与 GID 重映射不一致: before=%d after=%d remap=%d err=%v", r, before, after, mapped, err)
		}
	}
	for _, glyph := range usage.glyphs {
		if _, ok := usage.remap[glyph]; !ok {
			t.Fatalf("显式字形 %d 未进入子集", glyph)
		}
	}
	for before, after := range usage.remap {
		if before == 0 {
			continue // 子集化允许清空 .notdef。
		}
		oldCID, oldOK := originalCFF.CFF.GlyphCID(before)
		newCID, newOK := resultCFF.CFF.GlyphCID(uint16(after))
		if !oldOK || !newOK || oldCID != newCID {
			t.Fatalf("字形 %d 的 CID 改变: before=%d after=%d", before, oldCID, newCID)
		}
		oldAdvance, err := original.GlyphAdvance(&buffer, sfnt.GlyphIndex(before), 64000, font.HintingNone)
		if err != nil {
			t.Fatal(err)
		}
		newAdvance, err := result.GlyphAdvance(&buffer, sfnt.GlyphIndex(after), 64000, font.HintingNone)
		if err != nil || oldAdvance != newAdvance {
			t.Fatalf("字形 %d 的推进量改变: before=%d after=%d err=%v", before, oldAdvance, newAdvance, err)
		}
		oldOutline, err := original.LoadGlyph(&buffer, sfnt.GlyphIndex(before), 64000, nil)
		if err != nil {
			t.Fatal(err)
		}
		oldOutline = append(sfnt.Segments(nil), oldOutline...)
		newOutline, err := result.LoadGlyph(&buffer, sfnt.GlyphIndex(after), 64000, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(oldOutline, append(sfnt.Segments(nil), newOutline...)) {
			t.Fatalf("字形 %d 的轮廓在子程序重排后改变", before)
		}
	}
	if !bytes.Equal(input, data) {
		t.Fatal("子集化修改了调用方的字体字节")
	}
}

func TestExternalMultiFDCFFSkipsMissingLocalFont(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.otf")
	skipped := false
	t.Run("missing", func(t *testing.T) {
		defer func() { skipped = t.Skipped() }()
		creatorExternalMultiFDCFF(t, []string{path})
		t.Fatal("缺失字体没有跳过测试")
	})
	if !skipped {
		t.Fatal("本地字体不存在时应跳过测试")
	}
}
