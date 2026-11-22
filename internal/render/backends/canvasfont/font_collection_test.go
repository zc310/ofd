package canvasfont

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	fontfile "github.com/zc310/ofd/internal/thirdparty/font"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/spec"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
)

func renderTestCollection(faces ...[]byte) []byte {
	be := binary.BigEndian
	var data []byte
	data = append(data, make([]byte, 12+4*len(faces))...)
	copy(data, "ttcf")
	be.PutUint32(data[4:], 0x00010000)
	be.PutUint32(data[8:], uint32(len(faces)))
	for i, face := range faces {
		base := len(data)
		be.PutUint32(data[12+4*i:], uint32(base))
		data = append(data, face...)
		for j := range int(be.Uint16(face[4:])) {
			record := data[base+12+16*j:]
			be.PutUint32(record[8:], be.Uint32(record[8:])+uint32(base))
		}
		for len(data)%4 != 0 {
			data = append(data, 0)
		}
	}
	return data
}

func TestEmbeddedCollectionSelectsNamedNonzeroFaceBeforeRepair(t *testing.T) {
	data := renderTestCollection(goregular.TTF, gomono.TTF)
	fonts := fontOriginFonts(t, "Go Mono", data)
	family, err := fonts.LoadFont(1)
	if err != nil {
		t.Fatal(err)
	}
	if !fonts.HasLoadedEmbeddedFont(1) {
		t.Fatal("所选字体面未作为内嵌字体使用")
	}
	loaded := canvasFamily(family).Face(12).Font
	want, err := fontfile.ParseSFNT(gomono.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.GlyphAdvance(loaded.GlyphIndex('W')) != want.GlyphAdvance(want.GlyphIndex('W')) ||
		loaded.Post.IsFixedPitch == 0 {
		t.Fatal("内嵌 TTC 忽略资源名称，仍加载第 0 面")
	}
}

// 缺失名称表属于兼容修复，不是标准字体；不能在选面时挡住后续 name/cmap 修复。
func TestEmbeddedCollectionRepairsDefaultFaceWhenNamesMissing(t *testing.T) {
	original, err := fontfile.ParseSFNT(bytes.Clone(goregular.TTF), 0)
	if err != nil {
		t.Fatal(err)
	}
	glyph := original.GlyphIndex('B')
	delete(original.Tables, "name")
	for _, tc := range []struct {
		name  string
		faces [][]byte
	}{
		{"single", [][]byte{original.Write()}},
		{"unmatched-other-face", [][]byte{original.Write(), gomono.TTF}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := renderTestCollection(tc.faces...)
			snapshot := bytes.Clone(data)
			fonts := fontOriginFonts(t, "OFD-No-Such-Collection-Family", data)
			fonts.RegisterGlyphs(1, map[rune]uint16{'A': glyph})
			family, err := fonts.LoadFont(1)
			if err != nil {
				t.Fatal(err)
			}
			if !fonts.HasLoadedEmbeddedFont(1) {
				t.Fatal("可修复的集合字体被外部回退替换")
			}
			loaded := canvasFamily(family).Face(12).Font
			if !bytes.Equal(loaded.Tables["glyf"], original.Tables["glyf"]) {
				t.Fatal("修复未保留默认字体面的原始轮廓")
			}
			if loaded.GlyphIndex('A') != glyph || loaded.GlyphIndex(fontrepair.GlyphRune(glyph)) != glyph {
				t.Fatal("默认字体面修复丢失了文档字形映射")
			}
			if !bytes.Equal(data, snapshot) {
				t.Fatal("修复改写了原始集合数据")
			}
		})
	}
}

func TestEmbeddedCollectionRejectsInvalidDefaultFaceDirectory(t *testing.T) {
	data := renderTestCollection(goregular.TTF, gomono.TTF)
	binary.BigEndian.PutUint32(data[12:], 0xffffffff)
	_, _, err := selectCollectionSource(&embeddedFontSource{data: data},
		&spec.Font{FontName: "OFD-No-Such-Collection-Family"}, FontRegular)
	if err == nil {
		t.Fatal("默认面的目录损坏时不能任意选取其他面")
	}
}

func TestCreatorCollectionSubsetKeepsNamedFaceInsteadOfFirstFace(t *testing.T) {
	collection := renderTestCollection(goregular.TTF, gomono.TTF)
	data, err := creator.Marshal(creator.Document{
		ID:    "selected-collection-subset",
		Fonts: []creator.Font{{Name: "Selected", FontName: "Go Mono", Data: collection}},
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Text{Font: "Selected", Size: 4, Width: 30, Height: 6, Value: "Wi"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	fonts := NewFonts(ofd.Documents[0])
	defer fonts.Close()
	var id spec.StRefID
	fonts.ForEachFont(func(key spec.StID, _ *spec.Font) bool { id = spec.StRefID(key); return false })
	family, err := fonts.LoadFont(id)
	if err != nil {
		t.Fatal(err)
	}
	face := canvasFamily(family).Face(12)
	if face.Font.GlyphAdvance(face.Font.GlyphIndex('W')) != face.Font.GlyphAdvance(face.Font.GlyphIndex('i')) {
		t.Fatal("创建器提前子集化了 TTC 第 0 面，丢失所选等宽字体")
	}
}

func TestRegisteredCollectionSelectsNamedFaceWithoutChangingSourceBytes(t *testing.T) {
	data := renderTestCollection(goregular.TTF, gomono.TTF)
	family := canvas.NewFontFamily("Go Mono")
	if err := loadFallbackFace(family, data, FontRegular); err != nil {
		t.Fatal(err)
	}
	want, err := fontfile.ParseSFNT(gomono.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(family.Face(12).Font.Tables["glyf"], want.Tables["glyf"]) {
		t.Fatal("注册的 TTC 没有按字体族名称选面")
	}
	original := renderTestCollection(goregular.TTF, gomono.TTF)
	if !bytes.Equal(data, original) {
		t.Fatal("选面改写了调用方的集合字节")
	}
}

func TestSystemCollectionUsesNamedFacesNativeStylesAndDistinctCacheEntries(t *testing.T) {
	if !systemFontLookupUsable() {
		t.Skip("该平台不枚举系统字体")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "multi.ttc")
	data := renderTestCollection(goregular.TTF, gomono.TTF, gobold.TTF, goitalic.TTF)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := canvas.CacheSystemFonts(filepath.Join(dir, "index.gob"), []string{dir}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := canvas.CacheSystemFonts(filepath.Join(t.TempDir(), "restore.gob"), nil); err != nil {
			t.Error(err)
		}
	})
	fonts := NewFonts(nil)
	defer fonts.Close()
	for _, tc := range []struct {
		name   string
		style  FontStyle
		index  int
		source []byte
	}{
		{"Go", FontRegular, 0, goregular.TTF},
		{"Go Mono", FontRegular, 1, gomono.TTF},
		{"Go", FontBold, 2, gobold.TTF},
		{"Go", FontItalic, 3, goitalic.TTF},
		{"Go-Bold", FontRegular, 2, gobold.TTF},
		{"Go-Italic", FontRegular, 3, goitalic.TTF},
	} {
		family, ok := loadCachedSystemFont(fonts, tc.name, tc.style)
		if !ok {
			t.Fatalf("无法匹配 %s/%v", tc.name, tc.style)
		}
		got := family.Face(12, canvasStyle(tc.style))
		want, err := fontfile.ParseSFNT(tc.source, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Font.Tables["glyf"], want.Tables["glyf"]) {
			t.Fatalf("%s/%v 未加载第 %d 面的轮廓", tc.name, tc.style, tc.index)
		}
		if tc.name == "Go" && (got.FauxBold != 0 || got.FauxItalic != 0) {
			t.Fatal("原生字体面存在时仍合成粗体或斜体")
		}
		key, ok := systemFontKeyForFile(file, tc.style)
		if !ok {
			t.Fatal("字体文件身份不可用")
		}
		if tc.name == "Go-Bold" {
			key.style = FontBold
		}
		if tc.name == "Go-Italic" {
			key.style = FontItalic
		}
		key.face = tc.index
		fontCacheMu.Lock()
		entry := systemFontCache[key]
		fontCacheMu.Unlock()
		if entry == nil || entry.family != family {
			t.Fatal("缓存没有保留字体面索引及实际样式")
		}
	}
	model, err := creator.Marshal(creator.Document{
		ID:    "native-collection-styles",
		Fonts: []creator.Font{{Name: "Logical", FontName: "Go-Regular", FamilyName: "Go"}},
		Pages: []creator.Page{{Items: []creator.Item{
			creator.Text{Font: "Logical", Size: 4, Width: 30, Height: 6, Value: "Native"},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := parser.NewOFD(model)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	logical := NewFonts(ofd.Documents[0])
	defer logical.Close()
	var id spec.StRefID
	logical.ForEachFont(func(key spec.StID, _ *spec.Font) bool { id = spec.StRefID(key); return false })
	base, err := logical.LoadFont(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		weight int
		italic bool
		data   []byte
	}{{700, false, gobold.TTF}, {400, true, goitalic.TTF}} {
		object := textObjectWith("Native")
		object.Font, object.Weight, object.Italic = id, tc.weight, tc.italic
		face, actual := logical.FaceObject(base, object, nil)
		canvasFace, ok := face.(canvasFontFace)
		if !ok {
			t.Fatal("没有返回 canvas 字体面")
		}
		loaded := canvasFace.face
		want, err := fontfile.ParseSFNT(tc.data, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(loaded.Font.Tables["glyf"], want.Tables["glyf"]) ||
			loaded.FauxBold != 0 || loaded.FauxItalic != 0 {
			t.Fatal("文字对象样式没有优先匹配 TTC 原生字体面")
		}
		rebuilt, _ := logical.FaceObject(actual, object, nil)
		if face.ToPath("Native").String() != rebuilt.ToPath("Native").String() ||
			face.TextWidth("Native") != rebuilt.TextWidth("Native") {
			t.Fatal("原生字体面在重建画笔或度量时不一致")
		}
	}
}
