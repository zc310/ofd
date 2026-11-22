package font

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
)

func metadataTestFont(t *testing.T, data []byte, family, local, full, post string) []byte {
	t.Helper()
	f, err := ParseSFNT(bytes.Clone(data), 0)
	if err != nil {
		t.Fatal(err)
	}
	type record struct {
		id    NameID
		lang  uint16
		value string
	}
	records := []record{
		{NameFontFamily, 0x0409, family}, {NamePreferredFamily, 0x0409, family},
		{NameFontFamily, 0x0804, local}, {NameFull, 0x0409, full},
		{NamePostScript, 0x0409, post}, {NameFontSubfamily, 0x0409, "Regular"},
	}
	be := binary.BigEndian
	names := make([]byte, 0, 1024)
	names = append(names, make([]byte, 6+12*len(records))...)
	be.PutUint16(names[2:], uint16(len(records)))
	be.PutUint16(names[4:], uint16(len(names)))
	storage := []byte{}
	for i, record := range records {
		value := utf16.Encode([]rune(record.value))
		entry := names[6+12*i:]
		for j, number := range []uint16{3, 1, record.lang, uint16(record.id), uint16(2 * len(value)), uint16(len(storage))} {
			be.PutUint16(entry[2*j:], number)
		}
		for _, char := range value {
			storage = be.AppendUint16(storage, char)
		}
	}
	f.Tables["name"] = append(names, storage...)
	return f.Write()
}

func metadataTestCollection(faces ...[]byte) []byte {
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

func TestSystemCollectionMatchesEveryFaceNameAndNativeStyle(t *testing.T) {
	faces := [][]byte{
		metadataTestFont(t, goregular.TTF, "Other", "其他", "Other Regular", "Other-Regular"),
		metadataTestFont(t, goregular.TTF, "Demo", "示例", "Demo Regular", "Demo-Regular"),
		metadataTestFont(t, gobold.TTF, "Demo", "示例", "Demo Bold", "Demo-Bold"),
		metadataTestFont(t, goitalic.TTF, "Demo", "示例", "Demo Italic", "Demo-Italic"),
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "MULTI.TTC")
	data := metadataTestCollection(faces...)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	fonts, err := FindSystemFonts([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		style Style
		index int
	}{
		{"Other", Regular, 0}, {"demo", Regular, 1}, {"示例", Bold, 2},
		{"Demo", Regular | Italic, 3}, {"Demo-Bold", Regular, 2},
		{"Demo Italic", Regular | Italic, 3},
		{"Demo-Italic", Regular, 3},
	} {
		got, ok := fonts.Match(tc.name, tc.style)
		if !ok || got.Filename != file || got.Index != tc.index {
			t.Fatalf("%q/%v 匹配到 %+v/%v，期望字体面 %d", tc.name, tc.style, got, ok, tc.index)
		}
		parsed, err := ParseSFNT(data, got.Index)
		if err != nil {
			t.Fatal(err)
		}
		want, err := ParseSFNT(faces[tc.index], 0)
		if err != nil || !bytes.Equal(parsed.Tables["glyf"], want.Tables["glyf"]) {
			t.Fatal("匹配索引与实际加载轮廓不一致")
		}
	}
	cache := filepath.Join(t.TempDir(), "fonts.gob")
	if err := fonts.Save(cache); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadSystemFonts(cache)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reloaded.Match("示例", Regular|Italic); !ok || got.Index != 3 {
		t.Fatal("索引持久化丢失了 TTC 字体面或本地化名称")
	}
}

func TestCollectionDirectoriesMayBeSharedOrUnordered(t *testing.T) {
	data := metadataTestCollection(goregular.TTF, gobold.TTF)
	be := binary.BigEndian
	first, second := be.Uint32(data[12:]), be.Uint32(data[16:])
	for _, offsets := range [][2]uint32{{second, first}, {first, first}} {
		be.PutUint32(data[12:], offsets[0])
		be.PutUint32(data[16:], offsets[1])
		for index := range 2 {
			if _, err := ParseSFNT(data, index); err != nil {
				t.Fatalf("字体面 %d: %v", index, err)
			}
		}
		metadata, err := ReadFontMetadata(bytes.NewReader(data))
		if err != nil || len(metadata) != 2 {
			t.Fatalf("共享/乱序目录枚举失败: %v", err)
		}
	}
}

func TestCollectionMetadataSkipsInvalidFaceWithoutLosingOthers(t *testing.T) {
	data := metadataTestCollection(goregular.TTF, gobold.TTF)
	binary.BigEndian.PutUint32(data[12:], 0xffffffff)
	metadata, err := ReadFontMetadata(bytes.NewReader(data))
	if err != nil || len(metadata) != 1 || metadata[0].Index != 1 {
		t.Fatalf("损坏的第 0 面遮蔽了可用字体面: %+v/%v", metadata, err)
	}
}

func TestCollectionDiscoveryKeepsStandalonePriorityButAddsNativeStyles(t *testing.T) {
	fonts := &SystemFonts{Fonts: map[string]map[Style]FontMetadata{}}
	fonts.Add(FontMetadata{Filename: "regular.ttf", Family: "Demo", Style: Regular})
	fonts.Add(FontMetadata{Filename: "multi.ttc", Family: "Demo", Style: Regular, Index: 2})
	fonts.Add(FontMetadata{Filename: "multi.ttc", Family: "Demo", Style: Bold, Index: 3})
	if got, ok := fonts.Match("Demo", Regular); !ok || got.Filename != "regular.ttf" {
		t.Fatal("新增 TTC 扫描改变了已安装独立字体的匹配优先级")
	}
	if got, ok := fonts.Match("Demo", Bold); !ok || got.Filename != "multi.ttc" || got.Index != 3 {
		t.Fatal("独立字体缺少粗体时没有使用集合内的原生粗体面")
	}
}
