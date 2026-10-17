package fontrepair

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"slices"
)

// 按 OpenType 1.9.1 的字体组织规则，偏移相对于字体起点，
// 目录项按标签排序，表数据按 4 字节对齐。
type table struct {
	tag  string
	data []byte
	sum  uint32 // 不计入 head.checkSumAdjustment 的校验和
}

type directory struct {
	signature uint32
	tables    []table
}

var be = binary.BigEndian

func sliceAt(b []byte, offset, size uint32) ([]byte, bool) {
	if uint64(offset)+uint64(size) > uint64(len(b)) {
		return nil, false
	}
	return b[int(offset):int(uint64(offset)+uint64(size))], true
}

func sfntSignature(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	switch be.Uint32(b) {
	case 0x00010000, 0x4f54544f, 0x74727565:
		return true
	}
	return false
}

func readDirectory(b []byte) (directory, error) {
	if len(b) < 12 {
		return directory{}, fmt.Errorf("font directory: truncated header")
	}
	n := int(be.Uint16(b[4:]))
	if n > (len(b)-12)/16 {
		return directory{}, fmt.Errorf("font directory: truncated records")
	}
	d := directory{signature: be.Uint32(b), tables: make([]table, 0, n)}
	seen := make(map[string]bool, n)
	for record := 12; record < 12+n*16; record += 16 {
		r := b[record : record+16]
		tag := string(r[:4])
		if seen[tag] {
			return directory{}, fmt.Errorf("font directory: duplicate table %q", tag)
		}
		payload, ok := sliceAt(b, be.Uint32(r[8:]), be.Uint32(r[12:]))
		if !ok {
			return directory{}, fmt.Errorf("font directory: table %q exceeds input", tag)
		}
		seen[tag] = true
		sum := checksum(payload)
		if tag == "head" && len(payload) >= 12 {
			sum -= be.Uint32(payload[8:])
		}
		d.tables = append(d.tables, table{tag: tag, data: payload, sum: sum})
	}
	return d, nil
}

func (d directory) get(tag string) []byte {
	for _, t := range d.tables {
		if t.tag == tag {
			return t.data
		}
	}
	return nil
}

func (d *directory) put(tag string, data []byte) {
	for i := range d.tables {
		if d.tables[i].tag == tag {
			d.tables[i] = table{tag: tag, data: data}
			return
		}
	}
	d.tables = append(d.tables, table{tag: tag, data: data})
}

func align4(n int) int { return (n + 3) &^ 3 }

func checksum(b []byte) uint32 {
	var sum uint32
	for len(b) >= 4 {
		sum += be.Uint32(b)
		b = b[4:]
	}
	var tail [4]byte
	copy(tail[:], b)
	return sum + be.Uint32(tail[:])
}

func (d directory) encode() []byte {
	records := slices.Clone(d.tables)
	slices.SortFunc(records, func(a, b table) int { return compareTag(a.tag, b.tag) })
	size := 12 + 16*len(records)
	for _, t := range records {
		size += align4(len(t.data))
	}
	out := make([]byte, size)
	be.PutUint32(out, d.signature)
	be.PutUint16(out[4:], uint16(len(records)))
	if len(records) > 0 {
		power := bits.Len(uint(len(records))) - 1
		be.PutUint16(out[6:], uint16(16<<power))
		be.PutUint16(out[8:], uint16(power))
		be.PutUint16(out[10:], uint16(16*len(records)-(16<<power)))
	}
	pos, adjustment := 12+16*len(records), -1
	var payloadSum uint32
	for i, t := range records {
		body := out[pos : pos+len(t.data)]
		copy(body, t.data)
		if t.tag == "head" && len(body) >= 12 {
			clear(body[8:12])
			adjustment = pos + 8
		}
		sum := t.sum
		if sum == 0 {
			sum = checksum(body)
		}
		r := out[12+i*16 : 12+(i+1)*16]
		copy(r, t.tag)
		be.PutUint32(r[4:], sum)
		be.PutUint32(r[8:], uint32(pos))
		be.PutUint32(r[12:], uint32(len(body)))
		payloadSum += sum
		pos += align4(len(body))
	}
	if adjustment >= 0 {
		// 仅修改 cmap 时可复用其他表的校验和；填充字节均为零。
		be.PutUint32(out[adjustment:], 0xb1b0afba-payloadSum-checksum(out[:12+16*len(records)]))
	}
	return out
}

func compareTag(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// 字段位置与表版本遵循 OpenType 的 OS/2、name 和 post 规范。
// 缺失度量采用以设计单位表示的保守缺省值。
func words(size int, values map[int]uint16) []byte {
	b := make([]byte, size)
	for offset, value := range values {
		be.PutUint16(b[offset:], value)
	}
	return b
}

func os2Table(units uint16) []byte {
	b := words(96, map[int]uint16{0: 2, 2: metric(500, units), 4: 400, 6: 5, 62: 64,
		66: 255, 68: metric(800, units), 70: uint16(-int16(metric(200, units))),
		74: metric(800, units), 76: metric(200, units)})
	copy(b[58:62], "OFD ")
	return b
}

func nameTable() []byte {
	names := []struct {
		id    uint16
		value string
	}{{1, "OFD Embedded"}, {2, "Regular"}, {4, "OFD Embedded Regular"}, {6, "OFDEmbedded-Regular"}}
	b := words(6+12*len(names), map[int]uint16{2: uint16(len(names)), 4: uint16(6 + 12*len(names))})
	for i, name := range names {
		r := b[6+i*12 : 6+(i+1)*12]
		be.PutUint16(r, 3)
		be.PutUint16(r[2:], 1)
		be.PutUint16(r[4:], 0x409)
		be.PutUint16(r[6:], name.id)
		be.PutUint16(r[8:], uint16(len(name.value)*2))
		be.PutUint16(r[10:], uint16(len(b)-(6+12*len(names))))
		for _, c := range []byte(name.value) {
			b = append(b, 0, c)
		}
	}
	return b
}

func postTable() []byte { return words(32, map[int]uint16{0: 3}) }

func metric(value int, units uint16) uint16 {
	return uint16(min(65535, (uint64(value)*uint64(units)+500)/1000))
}
