package fontrepair

import (
	"bytes"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

// 两个字体面共享同一目录及全部表，验证偏移相对于整个集合、而非目录起点。
func sharedFontCollection() []byte {
	data := make([]byte, 20+len(goregular.TTF))
	copy(data, "ttcf")
	be.PutUint32(data[4:], 0x00010000)
	be.PutUint32(data[8:], 2)
	be.PutUint32(data[12:], 20)
	be.PutUint32(data[16:], 20)
	copy(data[20:], goregular.TTF)
	n := int(be.Uint16(data[24:]))
	for i := 0; i < n; i++ {
		record := 20 + 12 + i*16
		be.PutUint32(data[record+8:], be.Uint32(data[record+8:])+20)
	}
	return data
}

func TestExtractCollectionFacePreservesSharedTablesAndChecksums(t *testing.T) {
	data := sharedFontCollection()
	before := append([]byte(nil), data...)
	first, err := ExtractCollectionFace(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	last, err := ExtractCollectionFace(data, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, last) || !bytes.Equal(before, data) {
		t.Fatal("提取修改了共享表或输入字节")
	}
	if _, err := sfnt.Parse(last); err != nil {
		t.Fatal(err)
	}
	if checksum(last) != 0xb1b0afba {
		t.Fatal("独立字体的整体校验和不正确")
	}
	source, err := readDirectory(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	output, err := readDirectory(last)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range source.tables {
		want, got := append([]byte(nil), table.data...), append([]byte(nil), output.get(table.tag)...)
		if table.tag == "head" {
			clear(want[8:12])
			clear(got[8:12])
		}
		if !bytes.Equal(want, got) {
			t.Fatalf("字体表 %s 被改写", table.tag)
		}
	}
}

func TestExtractCollectionFaceRejectsInvalidDirectories(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func([]byte) []byte
		index  int
	}{
		{"truncated", func(b []byte) []byte { return b[:10] }, 0},
		{"wrong signature", func(b []byte) []byte { copy(b, "bad!"); return b }, 0},
		{"wrong version", func(b []byte) []byte { be.PutUint32(b[4:], 0); return b }, 0},
		{"bad count", func(b []byte) []byte { be.PutUint32(b[8:], 0xffffffff); return b }, 0},
		{"negative index", func(b []byte) []byte { return b }, -1},
		{"large index", func(b []byte) []byte { return b }, 2},
		{"bad offset", func(b []byte) []byte { be.PutUint32(b[12:], 0xfffffffc); return b }, 0},
		{"bad table", func(b []byte) []byte { be.PutUint32(b[20+12+8:], 0xfffffffc); return b }, 0},
		{"duplicate table", func(b []byte) []byte { copy(b[20+12+16:], b[20+12:20+12+4]); return b }, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ExtractCollectionFace(test.change(sharedFontCollection()), test.index); err == nil {
				t.Fatal("非法集合被接受")
			}
		})
	}
}
