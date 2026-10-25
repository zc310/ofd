package creator

import (
	"bytes"
	"encoding/binary"
	"testing"

	fontfile "github.com/zc310/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

// 构造 Macintosh 编码优先且与 Unicode 不同的字体，保护旧仿宋的数字和西文字形。
func TestFontSubsetUsesUnicodeCmapInsteadOfFirstLegacyEncoding(t *testing.T) {
	face, err := fontfile.ParseSFNT(goregular.TTF, 0)
	if err != nil {
		t.Fatal(err)
	}
	unicodeCmap := face.Tables["cmap"]
	be := binary.BigEndian
	count := int(be.Uint16(unicodeCmap[2:]))
	legacy := make([]byte, 262)
	be.PutUint16(legacy, 0)
	be.PutUint16(legacy[2:], 262)
	legacy[6+'0'] = byte(face.GlyphIndex('N'))
	legacy[6+'1'] = byte(face.GlyphIndex('O'))
	legacy[6+'2'] = byte(face.GlyphIndex('P'))
	cmap := make([]byte, 4+8*(count+1))
	be.PutUint16(cmap[2:], uint16(count+1))
	be.PutUint16(cmap[4:], 1)
	be.PutUint32(cmap[8:], uint32(len(cmap)))
	cmap = append(cmap, legacy...)
	start := len(cmap)
	cmap = append(cmap, unicodeCmap...)
	for i := 0; i < count; i++ {
		copy(cmap[12+i*8:20+i*8], unicodeCmap[4+i*8:12+i*8])
		be.PutUint32(cmap[16+i*8:], uint32(start)+be.Uint32(unicodeCmap[8+i*8:]))
	}
	face.Tables["cmap"] = cmap
	data := face.Write()
	input := bytes.Clone(data)
	legacyFace, err := fontfile.ParseSFNT(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	original, err := sfnt.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	var buffer sfnt.Buffer
	zero, err := original.GlyphIndex(&buffer, '0')
	if err != nil {
		t.Fatal(err)
	}
	if legacyFace.GlyphIndex('0') == uint16(zero) {
		t.Fatal("测试未构造两种编码冲突")
	}
	resource := Font{Name: "Legacy", Data: data}
	usage := &fontUsage{runes: []rune("2010"), glyphs: []uint16{uint16(zero)}}
	if err := subsetFont(&resource, usage); err != nil {
		t.Fatal(err)
	}
	result, err := sfnt.Parse(resource.Data)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range "012" {
		originalGlyph, _ := original.GlyphIndex(&buffer, r)
		resultGlyph, _ := result.GlyphIndex(&buffer, r)
		if resultGlyph == 0 || int(resultGlyph) != usage.remap[uint16(originalGlyph)] {
			t.Fatalf("子集命中了旧编码字形: %q original=%d result=%d remap=%v", r, originalGlyph, resultGlyph, usage.remap)
		}
		before, err := original.LoadGlyph(&buffer, originalGlyph, 64000, nil)
		if err != nil {
			t.Fatal(err)
		}
		beforeCopy := append(sfnt.Segments(nil), before...)
		after, err := result.LoadGlyph(&buffer, resultGlyph, 64000, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(beforeCopy) != len(after) {
			t.Fatal("数字轮廓改变")
		}
		for i := range after {
			if beforeCopy[i] != after[i] {
				t.Fatal("数字轮廓换成了其他字母")
			}
		}
	}
	if !bytes.Equal(input, data) {
		t.Fatal("规范化修改了原字体字节")
	}
}
