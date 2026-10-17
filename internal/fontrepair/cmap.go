package fontrepair

import (
	"fmt"
	"slices"
)

// OpenType cmap 的编码记录引用各自具有独立长度的子表。
type encoding struct {
	platform, id uint16
	data         []byte
}

func encodings(b []byte) ([]encoding, error) {
	if len(b) == 0 {
		return nil, nil
	}
	if len(b) < 4 || be.Uint16(b) != 0 {
		return nil, fmt.Errorf("invalid cmap header")
	}
	n := int(be.Uint16(b[2:]))
	if n > (len(b)-4)/8 {
		return nil, fmt.Errorf("truncated cmap records")
	}
	result := make([]encoding, 0, n)
	for i := range n {
		r := b[4+8*i:]
		pos := be.Uint32(r[4:])
		h, ok := sliceAt(b, pos, 2)
		if !ok {
			return nil, fmt.Errorf("invalid cmap offset")
		}
		format := be.Uint16(h)
		var size uint32
		switch format {
		case 8, 10, 12, 13:
			h, ok = sliceAt(b, pos, 8)
			if ok {
				size = be.Uint32(h[4:])
			}
		case 14:
			h, ok = sliceAt(b, pos, 6)
			if ok {
				size = be.Uint32(h[2:])
			}
		default:
			h, ok = sliceAt(b, pos, 4)
			if ok {
				size = uint32(be.Uint16(h[2:]))
			}
		}
		if !ok || size < uint32(len(h)) {
			return nil, fmt.Errorf("invalid cmap length")
		}
		payload, ok := sliceAt(b, pos, size)
		if !ok {
			return nil, fmt.Errorf("truncated cmap subtable")
		}
		result = append(result, encoding{be.Uint16(r), be.Uint16(r[2:]), payload})
	}
	return result, nil
}

func unicodeEncoding(e encoding) bool {
	return e.platform == 0 || e.platform == 3 && (e.id == 1 || e.id == 10)
}

func cmapCharacters(b []byte) map[uint32]uint16 {
	entries, err := encodings(b)
	if err != nil {
		return nil
	}
	out := make(map[uint32]uint16)
	// 全字符集映射优先于基本多文种平面（BMP）映射，不依赖编码记录的排列顺序。
	for _, full := range []bool{false, true} {
		for _, e := range entries {
			if !unicodeEncoding(e) || (be.Uint16(e.data) == 12 || be.Uint16(e.data) == 13) != full {
				continue
			}
			readCharacters(e.data, out)
		}
	}
	return out
}

func readCharacters(b []byte, out map[uint32]uint16) {
	put := func(code, glyph uint32) {
		if code <= 0x10ffff && (code < 0xd800 || code > 0xdfff) && glyph > 0 && glyph <= 65535 {
			out[code] = uint16(glyph)
		}
	}
	switch be.Uint16(b) {
	case 0:
		if len(b) >= 262 {
			for c, g := range b[6:262] {
				put(uint32(c), uint32(g))
			}
		}
	case 4:
		if len(b) < 16 {
			return
		}
		n := int(be.Uint16(b[6:])) / 2
		if n == 0 || n > (len(b)-16)/8 {
			return
		}
		last := -1
		for i := range n {
			end := int(be.Uint16(b[14+2*i:]))
			start := int(be.Uint16(b[16+2*n+2*i:]))
			delta := be.Uint16(b[16+4*n+2*i:])
			ref := 16 + 6*n + 2*i
			offset := int(be.Uint16(b[ref:]))
			if start > end || start <= last {
				return
			}
			last = end
			for c := start; c <= end && c < 65535; c++ {
				g := uint16(c)
				if offset != 0 {
					p := ref + offset + 2*(c-start)
					if p+2 > len(b) {
						return
					}
					g = be.Uint16(b[p:])
					if g == 0 {
						continue
					}
				}
				put(uint32(c), uint32(g+delta))
			}
		}
	case 6:
		if len(b) < 10 {
			return
		}
		first, n := uint32(be.Uint16(b[6:])), int(be.Uint16(b[8:]))
		if n > (len(b)-10)/2 {
			return
		}
		for i := range n {
			put(first+uint32(i), uint32(be.Uint16(b[10+2*i:])))
		}
	case 10:
		if len(b) < 20 {
			return
		}
		first, n := be.Uint32(b[12:]), be.Uint32(b[16:])
		if uint64(n) > uint64((len(b)-20)/2) || uint64(first)+uint64(n) > 0x110000 {
			return
		}
		for i := uint32(0); i < n; i++ {
			put(first+i, uint32(be.Uint16(b[20+2*i:])))
		}
	case 12, 13:
		if len(b) < 16 {
			return
		}
		n := be.Uint32(b[12:])
		if uint64(n) > uint64((len(b)-16)/12) {
			return
		}
		var previous uint32
		for i := uint32(0); i < n; i++ {
			r := b[16+12*i:]
			first, last, glyph := be.Uint32(r), be.Uint32(r[4:]), be.Uint32(r[8:])
			if first > last || last > 0x10ffff || i > 0 && first <= previous {
				return
			}
			previous = last
			for c := first; c <= last; c++ {
				g := glyph
				if be.Uint16(b) == 12 {
					if uint64(g)+uint64(c-first) > 65535 {
						break
					}
					g += c - first
				}
				put(c, g)
			}
		}
	}
}

func hasGlyphAddress(b []byte) bool {
	entries, err := encodings(b)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.platform != 3 || e.id != 10 || len(e.data) < 16 || be.Uint16(e.data) != 12 {
			continue
		}
		n := be.Uint32(e.data[12:])
		if uint64(n) > uint64((len(e.data)-16)/12) {
			continue
		}
		for i := uint32(0); i < n; i++ {
			r := e.data[16+12*i:]
			if be.Uint32(r) >= 0xf0000 && be.Uint32(r) <= 0xfffff {
				return true
			}
		}
	}
	return false
}

func withGlyphAddresses(b []byte, count uint16) ([]byte, error) {
	entries, err := encodings(b)
	if err != nil {
		return nil, err
	}
	characters := cmapCharacters(b)
	if characters == nil {
		characters = make(map[uint32]uint16)
	}
	for g := uint32(1); g < uint32(count); g++ {
		characters[0xf0000+g] = uint16(g)
	}
	return replaceCharacters(entries, characters), nil
}

func encodeCharacters(characters map[uint32]uint16) []byte { return replaceCharacters(nil, characters) }

func replaceCharacters(entries []encoding, characters map[uint32]uint16) []byte {
	codes := make([]uint32, 0, len(characters))
	for c, g := range characters {
		if g != 0 {
			codes = append(codes, c)
		}
	}
	slices.Sort(codes)
	type group struct{ first, last, glyph uint32 }
	groups := make([]group, 0)
	for _, c := range codes {
		g := uint32(characters[c])
		if n := len(groups); n > 0 && c == groups[n-1].last+1 && g == groups[n-1].glyph+c-groups[n-1].first {
			groups[n-1].last = c
		} else {
			groups = append(groups, group{c, c, g})
		}
	}
	body := make([]byte, 16+12*len(groups))
	be.PutUint16(body, 12)
	be.PutUint32(body[4:], uint32(len(body)))
	be.PutUint32(body[12:], uint32(len(groups)))
	for i, g := range groups {
		r := body[16+12*i:]
		be.PutUint32(r, g.first)
		be.PutUint32(r[4:], g.last)
		be.PutUint32(r[8:], g.glyph)
	}
	kept := make([]encoding, 0, len(entries)+1)
	for _, e := range entries {
		if e.platform != 3 || e.id != 10 {
			kept = append(kept, e)
		}
	}
	kept = append(kept, encoding{3, 10, body})
	slices.SortStableFunc(kept, func(a, b encoding) int {
		if a.platform != b.platform {
			return int(a.platform) - int(b.platform)
		}
		return int(a.id) - int(b.id)
	})
	out := make([]byte, 4+8*len(kept))
	be.PutUint16(out[2:], uint16(len(kept)))
	for i, e := range kept {
		r := out[4+8*i:]
		be.PutUint16(r, e.platform)
		be.PutUint16(r[2:], e.id)
		be.PutUint32(r[4:], uint32(len(out)))
		out = append(out, e.data...)
	}
	return out
}
