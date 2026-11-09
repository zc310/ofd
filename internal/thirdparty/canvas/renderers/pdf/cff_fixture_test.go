package pdf

import (
	"encoding/binary"
	"testing"

	"github.com/zc310/ofd/internal/fontrepair"
)

// multiFDCFFFixture 按 Adobe 5176 §5、§18–19 在内存构造两个 FD 的稀疏 CID 字体。
// 全局子程序调用所属 FD 的局部子程序，子集化不得改变这种调用关系。
func multiFDCFFFixture(t *testing.T) []byte {
	t.Helper()
	index := func(objects ...[]byte) []byte {
		if len(objects) == 0 {
			return []byte{0, 0}
		}
		length := 3 + 4*(len(objects)+1)
		for _, object := range objects {
			length += len(object)
		}
		data := make([]byte, length)
		binary.BigEndian.PutUint16(data, uint16(len(objects)))
		data[2] = 4
		offset := uint32(1)
		for i, object := range objects {
			binary.BigEndian.PutUint32(data[3+4*i:], offset)
			offset += uint32(len(object))
		}
		binary.BigEndian.PutUint32(data[3+4*len(objects):], offset)
		cursor := 3 + 4*(len(objects)+1)
		for _, object := range objects {
			cursor += copy(data[cursor:], object)
		}
		return data
	}
	dict := func(operator int, operands ...int) []byte {
		var data []byte
		for _, value := range operands {
			data = append(data, 29, byte(value>>24), byte(value>>16), byte(value>>8), byte(value))
		}
		if operator >= 256 {
			return append(data, 12, byte(operator-256))
		}
		return append(data, byte(operator))
	}
	names := index([]byte("MultiFDFixture"))
	strings := index([]byte("Adobe"), []byte("Identity"))
	global := index([]byte{32, 10, 11}) // callsubr 0，return
	charset := []byte{0, 2, 68, 86, 15, 0, 40, 4, 210, 253, 232, 0, 8, 2, 43}
	charstrings := make([][]byte, 1, 8)
	charstrings[0] = []byte{14}
	for range 7 {
		charstrings = append(charstrings, []byte{139, 139, 21, 32, 29, 14})
	}
	chars := index(charstrings...)
	selectors := []byte{0, 0, 0, 1, 0, 1, 0, 1, 1}
	local0 := index([]byte{239, 139, 139, 239, 39, 139, 5, 11}, []byte{11})
	local1 := index([]byte{247, 92, 139, 139, 239, 251, 92, 139, 5, 11}, []byte{11})
	private := func(width int) []byte { return append(dict(20, width), dict(19, 12)...) }
	private0, private1 := private(500), private(900)
	makeTop := func(base int) []byte {
		top := dict(256+30, 391, 392, 0)
		top = append(top, dict(15, base)...)
		top = append(top, dict(17, base+len(charset))...)
		top = append(top, dict(256+37, base+len(charset)+len(chars))...)
		return append(top, dict(256+36, base+len(charset)+len(chars)+len(selectors)+len(private0)+len(local0)+len(private1)+len(local1))...)
	}
	base := 4 + len(names) + len(index(makeTop(0))) + len(strings) + len(global)
	privateOffset := base + len(charset) + len(chars) + len(selectors)
	fds := index(dict(18, len(private0), privateOffset), dict(18, len(private1), privateOffset+len(private0)+len(local0)))
	data := []byte{1, 0, 4, 4}
	for _, section := range [][]byte{names, index(makeTop(base)), strings, global, charset, chars, selectors, private0, local0, private1, local1, fds} {
		data = append(data, section...)
	}
	wrapped, err := fontrepair.Repair(data)
	if err != nil {
		t.Fatal(err)
	}
	return wrapped
}
