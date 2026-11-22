package font

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
)

func nameTableWithStoragePadding(version, padding uint16) []byte {
	be := binary.BigEndian
	headerSize := uint16(18) // 表头和一条名称记录。
	language := uint16(0x0409)
	storageSize := 4
	if version == 1 {
		headerSize += 6 // langTagCount 和一条语言标签记录。
		language = 0x8000
		storageSize += 4
	}
	storageOffset := headerSize + padding
	data := make([]byte, int(storageOffset)+storageSize)
	be.PutUint16(data, version)
	be.PutUint16(data[2:], 1)
	be.PutUint16(data[4:], storageOffset)
	for i, value := range []uint16{3, 1, language, uint16(NameFontFamily), 4, 0} {
		be.PutUint16(data[6+2*i:], value)
	}
	copy(data[storageOffset:], []byte{0, 'G', 0, 'o'})
	if version == 1 {
		be.PutUint16(data[18:], 1)
		be.PutUint16(data[20:], 4)
		be.PutUint16(data[22:], 4)
		copy(data[int(storageOffset)+4:], []byte{0, 'e', 0, 'n'})
	}
	return data
}

// OpenType 1.9.1 name 表的 storageOffset 指向字符串区，允许与记录区之间存在填充。
func TestNameStoragePaddingPreservesNamesAndSystemDiscovery(t *testing.T) {
	for _, version := range []uint16{0, 1} {
		for _, padding := range []uint16{0, 2, 7} {
			t.Run(fmt.Sprintf("version-%d-padding-%d", version, padding), func(t *testing.T) {
				face, err := ParseSFNT(bytes.Clone(goregular.TTF), 0)
				if err != nil {
					t.Fatal(err)
				}
				face.Tables["name"] = nameTableWithStoragePadding(version, padding)
				data := face.Write()
				parsed, err := ParseSFNT(data, 0)
				if err != nil {
					t.Fatal(err)
				}
				if records := parsed.Name.Get(NameFontFamily); len(records) != 1 || records[0].String() != "Go" {
					t.Fatal("填充改变了字体名称")
				}
				if version == 1 && (len(parsed.Name.LangTag) != 1 ||
					!bytes.Equal(parsed.Name.LangTag[0].Value, []byte{0, 'e', 0, 'n'})) {
					t.Fatal("填充改变了语言标签")
				}
				for _, ext := range []string{".ttf", ".ttc"} {
					t.Run(ext, func(t *testing.T) {
						fileData := data
						if ext == ".ttc" {
							fileData = metadataTestCollection(data)
						}
						dir := t.TempDir()
						path := filepath.Join(dir, "padded"+ext)
						if err := os.WriteFile(path, fileData, 0600); err != nil {
							t.Fatal(err)
						}
						fonts, err := FindSystemFonts([]string{dir})
						if err != nil {
							t.Fatal(err)
						}
						if got, ok := fonts.Match("Go", Regular); !ok || got.Filename != path || got.Index != 0 {
							t.Fatal("系统索引漏掉了名称字符串区带填充的字体")
						}
					})
				}
			})
		}
	}
}

func TestNameStorageRejectsOverlappingAndOutOfBoundsOffsets(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version uint16
		change  func([]byte)
	}{
		{"name-record-overlap", 0, func(data []byte) { binary.BigEndian.PutUint16(data[4:], 17) }},
		{"language-record-overlap", 1, func(data []byte) { binary.BigEndian.PutUint16(data[4:], 23) }},
		{"storage-out-of-bounds", 0, func(data []byte) { binary.BigEndian.PutUint16(data[4:], uint16(len(data)+1)) }},
		{"name-string-out-of-bounds", 0, func(data []byte) { binary.BigEndian.PutUint16(data[16:], 0xffff) }},
		{"language-string-out-of-bounds", 1, func(data []byte) { binary.BigEndian.PutUint16(data[22:], 0xffff) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := nameTableWithStoragePadding(tc.version, 2)
			tc.change(data)
			face := &SFNT{Tables: map[string][]byte{"name": data}}
			if err := face.parseName(); err == nil {
				t.Fatal("重叠或越界的名称存储偏移未被拒绝")
			}
		})
	}
}
