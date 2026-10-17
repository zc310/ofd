// Package cidmap 为 PDF 解码和字体修复提供共享的 Adobe 字符集映射数据。
// 本包只负责数据查询，不包含字体或 PDF 解析器。
package cidmap

import (
	"bytes"
	"compress/zlib"
	_ "embed"
	"encoding/binary"
	"io"
	"sync"
)

//go:generate python3 generate.py /usr/share/poppler
//go:embed gb1.zlib
var gb1Compressed []byte

var gb1Values = sync.OnceValue(func() []uint16 {
	r, err := zlib.NewReader(bytes.NewReader(gb1Compressed))
	if err != nil {
		panic(err)
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, 2*gb1Count+1))
	if err != nil || len(data) != 2*gb1Count {
		panic("invalid embedded Adobe-GB1 data")
	}
	values := make([]uint16, gb1Count)
	for i := range values {
		values[i] = binary.BigEndian.Uint16(data[2*i:])
	}
	return values
})

// GB1GlyphUnicode 返回字体修复使用的单个 Unicode 码位，
// 保留 cidToUnicode/Adobe-GB1 中的竖排字符、私用字符和兼容字符。
func GB1GlyphUnicode(cid int) (rune, bool) {
	if cid < 0 || cid >= gb1Count {
		return 0, false
	}
	if r, ok := gb1Supplementary[cid]; ok {
		return r, true
	}
	r := rune(gb1Values()[cid])
	return r, r != 0
}

// GB1Text 按 Adobe-GB1-UCS2 查询文字提取映射。
// 同一 CID 的文字映射可能使用普通标点，或返回包含异体选择符的字符序列。
func GB1Text(cid int) (string, bool) {
	if cid < 0 || cid >= gb1Count {
		return "", false
	}
	if text, ok := gb1TextDifferences[cid]; ok {
		return text, true
	}
	r, ok := GB1GlyphUnicode(cid)
	if !ok {
		return "", false
	}
	return string(r), true
}
