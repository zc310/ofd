package font

import (
	"testing"

	"github.com/tdewolff/parse/v2"
)

// cntrmask 可隐含 vstem：8 个 stem 应消耗 1 字节，9 个则需要 2 字节。
func TestCFFCounterMaskCountsImplicitVerticalStems(t *testing.T) {
	for _, op := range []byte{byte(cffHintmask), byte(cffCntrmask)} {
		index := &cffINDEX{}
		data := make([]byte, 0, 24)
		for range 7 {
			data = append(data, 139, 140)
		}
		data = append(data, byte(cffHstemhm), 139, 140, 139, 140, op, 0xff, 0x04, 139, byte(cffHmoveto), byte(cffEndchar))
		index.Add(data)
		cff := &cffTable{version: 1, charStrings: index}
		var moves int
		err := cff.parseCharString(0, func(_ *parse.BinaryReader, op int32, _ []int32) error {
			if op == cffHmoveto {
				moves++
			}
			return nil
		})
		if err != nil || moves != 1 {
			t.Fatalf("掩码操作符 %d：err=%v，移动次数=%d", op, err, moves)
		}
	}
}

func TestCFFEmptyInitialMovetoReturnsError(t *testing.T) {
	index := &cffINDEX{}
	index.Add([]byte{byte(cffVmoveto), byte(cffEndchar)})
	cff := &cffTable{version: 1, charStrings: index}
	if err := cff.parseCharString(0, func(_ *parse.BinaryReader, _ int32, _ []int32) error { return nil }); err == nil {
		t.Fatal("缺少位移操作数应返回错误，而不是 panic")
	}
}
