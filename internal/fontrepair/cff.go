package fontrepair

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/benoitkugler/textlayout/fonts/glyphsnames"
	"github.com/zc310/ofd/internal/cidmap"
)

func isCFF(b []byte) bool { return len(b) >= 4 && b[0] == 1 && b[2] >= 4 && b[3] >= 1 && b[3] <= 4 }

// CFF INDEX 的数据偏移从 1 开始计数（Adobe 5176 §5）。
func cffIndex(b []byte, pos int) ([][]byte, int, error) {
	if pos < 0 || pos > len(b)-2 {
		return nil, pos, fmt.Errorf("CFF INDEX: missing count")
	}
	n := int(be.Uint16(b[pos:]))
	pos += 2
	if n == 0 {
		return nil, pos, nil
	}
	if pos >= len(b) {
		return nil, pos, fmt.Errorf("CFF INDEX: missing offSize")
	}
	size := int(b[pos])
	pos++
	if size < 1 || size > 4 || n+1 > (len(b)-pos)/size {
		return nil, pos, fmt.Errorf("CFF INDEX: invalid offsets")
	}
	base := pos + (n+1)*size
	offsets := make([]uint32, n+1)
	for i := range offsets {
		for _, v := range b[pos+i*size : pos+(i+1)*size] {
			offsets[i] = offsets[i]<<8 | uint32(v)
		}
	}
	if offsets[0] != 1 || uint64(offsets[n])-1 > uint64(len(b)-base) {
		return nil, pos, fmt.Errorf("CFF INDEX: invalid extent")
	}
	objects := make([][]byte, n)
	for i := range n {
		if offsets[i] > offsets[i+1] || offsets[i+1] > offsets[n] {
			return nil, pos, fmt.Errorf("CFF INDEX: decreasing offset")
		}
		objects[i] = b[base+int(offsets[i])-1 : base+int(offsets[i+1])-1]
	}
	return objects, base + int(offsets[n]) - 1, nil
}

type cffDict map[int][]float64

func dictNumber(b []byte) (float64, int, error) {
	if len(b) == 0 {
		return 0, 0, fmt.Errorf("CFF number: truncated")
	}
	switch c := b[0]; {
	case c >= 32 && c <= 246:
		return float64(int(c) - 139), 1, nil
	case c >= 247 && c <= 254:
		if len(b) >= 2 {
			if c <= 250 {
				return float64((int(c)-247)*256 + int(b[1]) + 108), 2, nil
			}
			return float64(-(int(c)-251)*256 - int(b[1]) - 108), 2, nil
		}
	case c == 28:
		if len(b) >= 3 {
			return float64(int16(be.Uint16(b[1:]))), 3, nil
		}
	case c == 29:
		if len(b) >= 5 {
			return float64(int32(be.Uint32(b[1:]))), 5, nil
		}
	case c == 30:
		var text strings.Builder
		for i, c := range b[1:] {
			for _, nibble := range []byte{c >> 4, c & 15} {
				switch {
				case nibble < 10:
					text.WriteByte('0' + nibble)
				case nibble == 10:
					text.WriteByte('.')
				case nibble == 11:
					text.WriteByte('E')
				case nibble == 12:
					text.WriteString("E-")
				case nibble == 14:
					text.WriteByte('-')
				case nibble == 15:
					v, e := strconv.ParseFloat(text.String(), 64)
					if e != nil || math.IsInf(v, 0) || math.IsNaN(v) {
						return 0, 0, fmt.Errorf("CFF number: invalid real")
					}
					return v, i + 2, nil
				default:
					return 0, 0, fmt.Errorf("CFF number: reserved nibble")
				}
			}
			if text.Len() > 128 {
				break
			}
		}
	}
	return 0, 0, fmt.Errorf("CFF number: invalid or truncated encoding")
}

func parseDict(b []byte) (cffDict, error) {
	d := make(cffDict)
	args := make([]float64, 0, 48)
	for len(b) > 0 {
		if b[0] <= 21 {
			op := int(b[0])
			b = b[1:]
			if op == 12 {
				if len(b) == 0 {
					return nil, fmt.Errorf("CFF DICT: truncated operator")
				}
				op = 0xc00 + int(b[0])
				b = b[1:]
			}
			d[op] = slices.Clone(args)
			args = args[:0]
		} else {
			v, n, err := dictNumber(b)
			if err != nil {
				return nil, err
			}
			if len(args) == 48 {
				return nil, fmt.Errorf("CFF DICT: operand limit")
			}
			args = append(args, v)
			b = b[n:]
		}
	}
	if len(args) != 0 {
		return nil, fmt.Errorf("CFF DICT: missing operator")
	}
	return d, nil
}

func (d cffDict) integer(op int) int {
	v := d[op]
	if len(v) == 0 || v[0] < 0 || v[0] > math.MaxInt32 || math.Trunc(v[0]) != v[0] {
		return 0
	}
	return int(v[0])
}

type privateDict struct {
	defaultWidth, nominalWidth float64
	subrs                      [][]byte
}

func privateData(b []byte, d cffDict) (privateDict, error) {
	var p privateDict
	v := d[18]
	if len(v) == 0 {
		return p, nil
	}
	if len(v) != 2 || v[0] < 0 || v[1] < 0 || v[0] > math.MaxInt32 || v[1] > math.MaxInt32 {
		return p, fmt.Errorf("CFF Private: invalid extent")
	}
	payload, ok := sliceAt(b, uint32(v[1]), uint32(v[0]))
	if !ok {
		return p, fmt.Errorf("CFF Private: truncated")
	}
	fields, err := parseDict(payload)
	if err != nil {
		return p, err
	}
	if x := fields[20]; len(x) > 0 {
		p.defaultWidth = x[0]
	}
	if x := fields[21]; len(x) > 0 {
		p.nominalWidth = x[0]
	}
	if relative := fields.integer(19); relative > 0 {
		p.subrs, _, err = cffIndex(b, int(v[1])+relative)
	}
	return p, err
}

func cffCharset(b []byte, pos, count int, cid bool) ([]uint16, error) {
	ids := make([]uint16, count)
	if pos <= 2 {
		if cid {
			return nil, fmt.Errorf("CFF CID charset must be explicit")
		}
		if pos == 0 && count <= 229 {
			for i := range ids {
				ids[i] = uint16(i)
			}
			return ids, nil
		}
		var predefined []uint16
		if pos == 1 {
			predefined = expertCharset
		} else {
			predefined = expertSubsetCharset
		}
		if count > len(predefined) {
			return nil, fmt.Errorf("CFF predefined charset exceeds size")
		}
		copy(ids, predefined)
		return ids, nil
	}
	if pos >= len(b) {
		return nil, fmt.Errorf("CFF charset: missing format")
	}
	format := b[pos]
	pos++
	for g := 1; g < count; {
		if pos > len(b)-2 {
			return nil, fmt.Errorf("CFF charset: truncated SID")
		}
		first := int(be.Uint16(b[pos:]))
		pos += 2
		n := 1
		switch format {
		case 0:
		case 1:
			if pos >= len(b) {
				return nil, fmt.Errorf("CFF charset: truncated range")
			}
			n += int(b[pos])
			pos++
		case 2:
			if pos > len(b)-2 {
				return nil, fmt.Errorf("CFF charset: truncated range")
			}
			n += int(be.Uint16(b[pos:]))
			pos += 2
		default:
			return nil, fmt.Errorf("CFF charset: unsupported format %d", format)
		}
		if n > count-g || first+n > 65536 {
			return nil, fmt.Errorf("CFF charset: range exceeds glyph count")
		}
		for j := range n {
			ids[g+j] = uint16(first + j)
		}
		g += n
	}
	return ids, nil
}

func cffSelectors(b []byte, pos, count, fdCount int) ([]byte, error) {
	selectors := make([]byte, count)
	if pos <= 0 || pos >= len(b) {
		return nil, fmt.Errorf("CFF FDSelect: missing format")
	}
	format := b[pos]
	pos++
	switch format {
	case 0:
		if count > len(b)-pos {
			return nil, fmt.Errorf("CFF FDSelect: truncated array")
		}
		copy(selectors, b[pos:pos+count])
	case 3:
		if pos > len(b)-2 {
			return nil, fmt.Errorf("CFF FDSelect: missing ranges")
		}
		n := int(be.Uint16(b[pos:]))
		pos += 2
		if n == 0 || n > (len(b)-pos-2)/3 {
			return nil, fmt.Errorf("CFF FDSelect: truncated ranges")
		}
		if be.Uint16(b[pos:]) != 0 || int(be.Uint16(b[pos+3*n:])) != count {
			return nil, fmt.Errorf("CFF FDSelect: invalid coverage")
		}
		for i := range n {
			r := b[pos+3*i:]
			first, last := int(be.Uint16(r)), int(be.Uint16(r[3:]))
			if first >= last || last > count {
				return nil, fmt.Errorf("CFF FDSelect: invalid range")
			}
			for g := first; g < last; g++ {
				selectors[g] = r[2]
			}
		}
	default:
		return nil, fmt.Errorf("CFF FDSelect: unsupported format %d", format)
	}
	for _, fd := range selectors {
		if int(fd) >= fdCount {
			return nil, fmt.Errorf("CFF FDSelect: invalid dictionary")
		}
	}
	return selectors, nil
}

func wrapCFF(input []byte) ([]byte, error) {
	// 所有兼容性修改都在副本上进行，避免改动文档中的原始字体资源。
	b := slices.Clone(repairCFFPrivate(input))
	if int(b[2]) > len(b) {
		return nil, fmt.Errorf("CFF: truncated header")
	}
	names, pos, err := cffIndex(b, int(b[2]))
	if err != nil {
		return nil, err
	}
	tops, pos, err := cffIndex(b, pos)
	if err != nil {
		return nil, err
	}
	if len(names) != 1 || len(tops) != 1 {
		return nil, fmt.Errorf("CFF: OpenType requires one font")
	}
	stringsIndex, pos, err := cffIndex(b, pos)
	if err != nil {
		return nil, err
	}
	global, _, err := cffIndex(b, pos)
	if err != nil {
		return nil, err
	}
	d, err := parseDict(tops[0])
	if err != nil {
		return nil, err
	}
	if t := d[0xc06]; len(t) > 0 && t[0] != 2 {
		return nil, fmt.Errorf("CFF: unsupported CharstringType")
	}
	charstrings, _, err := cffIndex(b, d.integer(17))
	if err != nil {
		return nil, err
	}
	if len(charstrings) == 0 {
		return nil, fmt.Errorf("CFF: empty CharStrings")
	}
	_, cid := d[0xc1e]
	charset, err := cffCharset(b, d.integer(15), len(charstrings), cid)
	if err != nil {
		return nil, err
	}
	p, err := privateData(b, d)
	if err != nil {
		return nil, err
	}
	privates := []privateDict{p}
	selectors := make([]byte, len(charstrings))
	if cid {
		fds, _, e := cffIndex(b, d.integer(0xc24))
		if e != nil {
			return nil, e
		}
		if len(fds) == 0 || len(fds) > 256 {
			return nil, fmt.Errorf("CFF: invalid FDArray")
		}
		privates = make([]privateDict, len(fds))
		for i, fd := range fds {
			fields, e := parseDict(fd)
			if e != nil {
				return nil, e
			}
			privates[i], e = privateData(b, fields)
			if e != nil {
				return nil, e
			}
		}
		selectors, err = cffSelectors(b, d.integer(0xc25), len(charstrings), len(fds))
		if err != nil {
			return nil, err
		}
	}
	units := uint16(1000)
	if matrix := d[0xc07]; len(matrix) == 6 && matrix[0] > 0 {
		units = uint16(math.Round(min(16384, max(16, 1/matrix[0]))))
	}
	hmtx := make([]byte, 4*len(charstrings))
	var widest uint16
	budget := max(1_000_000, min(16_000_000, len(b)*64))
	for g, program := range charstrings {
		p := privates[selectors[g]]
		vm := type2State{private: p, global: global, width: p.defaultWidth, budget: budget}
		// 轮廓损坏时，字符集和度量仍可能可用；无法求出字宽时保留保守的缺省值，
		// 轮廓错误由使用字体的渲染后端诊断。
		_ = vm.run(program, 0)
		budget = vm.budget
		if budget < 0 {
			return nil, fmt.Errorf("CFF: charstring instruction limit")
		}
		width := uint16(math.Round(min(65535, max(0, vm.width))))
		be.PutUint16(hmtx[4*g:], width)
		widest = max(widest, width)
	}
	characters := make(map[uint32]uint16, len(charstrings)*2)
	stringAt := func(sid int) string {
		if sid >= 0 && sid < len(standardStrings) {
			return standardStrings[sid]
		}
		if sid -= 391; sid >= 0 && sid < len(stringsIndex) {
			return string(stringsIndex[sid])
		}
		return ""
	}
	gb1 := false
	if ros := d[0xc1e]; len(ros) == 3 {
		ordering := stringAt(int(ros[1]))
		// 部分 OFD 生成器将 GB1 子集标记为 Adobe-Identity，intro.ofd 即属此例。
		// 此处仅作兼容回退，文档提供的显式映射仍然优先。
		gb1 = stringAt(int(ros[0])) == "Adobe" && (ordering == "GB1" || ordering == "Identity")
	}
	for g := 1; g < len(charstrings); g++ {
		id := charset[g]
		// 部分稀疏 OFD 子集的字形引用仍保留原始 CID，未使用子集内的紧凑 GID，
		// 因此还需要提供 CID 寻址映射。
		address := g
		if cid {
			address = int(id)
		}
		characters[0xf0000+uint32(address)] = uint16(g)
		var r rune
		var ok bool
		if gb1 {
			r, ok = cidmap.GB1GlyphUnicode(int(id))
		} else if !cid {
			r, ok = glyphsnames.GlyphToRune(stringAt(int(id)))
		}
		if ok && r > 0 {
			if _, exists := characters[uint32(r)]; !exists {
				characters[uint32(r)] = uint16(g)
			}
		}
	}
	head := words(54, map[int]uint16{0: 1, 4: 1, 16: 3, 18: units, 46: 8})
	be.PutUint32(head[12:], 0x5f0f3cf5)
	if bbox := d[5]; len(bbox) == 4 {
		for i, v := range bbox {
			be.PutUint16(head[36+2*i:], uint16(int16(min(32767, max(-32768, v)))))
		}
	}
	hhea := words(36, map[int]uint16{0: 1, 4: metric(800, units), 6: uint16(-int16(metric(200, units))), 10: widest, 18: 1, 34: uint16(len(charstrings))})
	maxp := make([]byte, 6)
	be.PutUint32(maxp, 0x5000)
	be.PutUint16(maxp[4:], uint16(len(charstrings)))
	out := directory{signature: 0x4f54544f}
	for tag, body := range map[string][]byte{"CFF ": b, "head": head, "hhea": hhea, "hmtx": hmtx, "maxp": maxp, "OS/2": os2Table(units), "name": nameTable(), "post": postTable(), "cmap": encodeCharacters(characters)} {
		out.put(tag, body)
	}
	return out.encode(), nil
}
