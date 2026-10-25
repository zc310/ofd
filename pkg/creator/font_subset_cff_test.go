package creator

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"strings"
	"testing"

	fontfile "github.com/zc310/font"
	"github.com/zc310/ofd/internal/fontrepair"
	"golang.org/x/image/font/gofont/goregular"
)

// 按 Adobe 5176 §5、§8、§18 构造独立 CID CFF，三字形、两个独立 Private DICT。
// 字形使用两种推进量，CID 与 GID 故意不同，避免测试依赖本机中文字体。
func creatorCIDTestFont(t *testing.T, fdCount int) []byte {
	t.Helper()
	index := func(items ...[]byte) []byte {
		if len(items) == 0 {
			return []byte{0, 0}
		}
		b := make([]byte, 3+4*(len(items)+1))
		binary.BigEndian.PutUint16(b, uint16(len(items)))
		b[2] = 4
		position := 1
		for i, item := range items {
			binary.BigEndian.PutUint32(b[3+4*i:], uint32(position))
			position += len(item)
		}
		binary.BigEndian.PutUint32(b[3+4*len(items):], uint32(position))
		for _, item := range items {
			b = append(b, item...)
		}
		return b
	}
	dict := func(op int, operands ...int) []byte {
		var b []byte
		for _, v := range operands {
			b = append(b, 29, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
		}
		if op > 255 {
			b = append(b, 12, byte(op))
		} else {
			b = append(b, byte(op))
		}
		return b
	}
	names, stringsIndex, global := index([]byte("TestCID")), index([]byte("Adobe"), []byte("Identity")), index()
	top := append(dict(0xc1e, 391, 392, 0), dict(0xc22, 64)...)
	charsetRef := len(top) + 1
	top = append(top, dict(15, 0)...)
	charsRef := len(top) + 1
	top = append(top, dict(17, 0)...)
	fdRef := len(top) + 1
	top = append(top, dict(0xc24, 0)...)
	selectRef := len(top) + 1
	top = append(top, dict(0xc25, 0)...)
	base := 4 + len(names) + len(index(top)) + len(stringsIndex) + len(global)
	charset := []byte{0, 0, 10, 0, 20}
	chars := index([]byte{14}, []byte{139, 139, 21, 239, 139, 139, 239, 39, 139, 5, 14}, []byte{139, 139, 21, 247, 92, 139, 139, 239, 251, 92, 139, 5, 14})
	var fds [][]byte
	var private []byte
	for i := 0; i < fdCount; i++ {
		body := dict(20, 600+200*i)
		private = append(private, body...)
		fds = append(fds, dict(18, len(body), 0))
	}
	fdPos := base + len(charset) + len(chars)
	selectPos := fdPos + len(index(fds...))
	privatePos := selectPos + 4
	for i := range fds {
		binary.BigEndian.PutUint32(fds[i][6:], uint32(privatePos+i*len(dict(20, 600))))
	}
	binary.BigEndian.PutUint32(top[charsetRef:], uint32(base))
	binary.BigEndian.PutUint32(top[charsRef:], uint32(base+len(charset)))
	binary.BigEndian.PutUint32(top[fdRef:], uint32(fdPos))
	binary.BigEndian.PutUint32(top[selectRef:], uint32(selectPos))
	cff := []byte{1, 0, 4, 4}
	for _, part := range [][]byte{names, index(top), stringsIndex, global, charset, chars, index(fds...), {0, 0, 0, byte(fdCount - 1)}, private} {
		cff = append(cff, part...)
	}
	data, err := fontrepair.RepairWithGlyphs(cff, []fontrepair.GlyphMapping{{Rune: 'A', Glyph: 1}, {Rune: 'B', Glyph: 2}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCIDCFFSubsettingPreservesGlyphsAndOtherFontSubsetting(t *testing.T) {
	for _, fdCount := range []int{1, 2} {
		t.Run(string(rune('0'+fdCount))+"-FD", func(t *testing.T) {
			data := creatorCIDTestFont(t, fdCount)
			originalBytes := bytes.Clone(data)
			original, err := fontfile.ParseSFNT(data, 0)
			if err != nil {
				t.Fatal(err)
			}
			if original.CFF == nil || original.CFF.NumFontDicts() != fdCount {
				t.Fatal("未构造指定数量的 Font DICT")
			}
			resource := Font{Name: "CID", Data: data}
			usage := &fontUsage{runes: []rune{'B'}, glyphs: []uint16{2}, remap: map[uint16]int{99: 1}}
			if err := subsetFont(&resource, usage); err != nil {
				t.Fatal(err)
			}
			result, err := fontfile.ParseSFNT(resource.Data, 0)
			if err != nil {
				t.Fatal(err)
			}
			if resource.Format != "otf" {
				t.Fatal("CID CFF 输出没有声明独立 OTF")
			}
			if result.NumGlyphs() != 2 || usage.remap[2] != 1 {
				t.Fatalf("没有生成子集或重映射 GID: glyphs=%d remap=%v", result.NumGlyphs(), usage.remap)
			}
			glyph := uint16(1)
			if result.GlyphIndex('B') != glyph || result.GlyphIndex('B') == 0 {
				t.Fatal("字符没有指向最终嵌入的字形")
			}
			if result.Hmtx.Advance(glyph) != original.Hmtx.Advance(2) {
				t.Fatal("每个 Font DICT 的字形推进量改变")
			}
			if cid, ok := result.CFF.GlyphCID(glyph); !ok || cid != 20 {
				t.Fatalf("CID 映射改变: %d %v", cid, ok)
			}
			if !bytes.Equal(originalBytes, data) {
				t.Fatal("子集化修改调用方字体数据")
			}
			other := Font{Name: "Latin", Data: goregular.TTF}
			if err := subsetFont(&other, &fontUsage{runes: []rune("ABC123")}); err != nil {
				t.Fatal(err)
			}
			latin, err := fontfile.ParseSFNT(other.Data, 0)
			if err != nil {
				t.Fatal(err)
			}
			if latin.NumGlyphs() >= 100 {
				t.Fatal("CFF 子集化影响其他 TrueType 字体子集")
			}
		})
	}
}

func TestCIDCFFRejectsOutOfRangeExplicitGlyph(t *testing.T) {
	data := creatorCIDTestFont(t, 2)
	original := bytes.Clone(data)
	resource := Font{Name: "CID", Data: data}
	err := subsetFont(&resource, &fontUsage{runes: []rune{'B'}, glyphs: []uint16{65535}})
	if err == nil || !strings.Contains(err.Error(), "字形编号超出字体范围") {
		t.Fatalf("未拒绝无效字形引用: %v", err)
	}
	if !reflect.DeepEqual(data, original) {
		t.Fatal("失败操作修改原字体")
	}
}
