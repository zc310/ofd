package pdf2ofd

import (
	"container/heap"
	"fmt"
	"math/bits"
	"sort"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// 内嵌代码在原始 32 位值的高位保存字节长度，使 <01> 与 <0001> 可以对应
// 不同且无歧义的代码空间。
func pdfEncodingCode(value uint32, length int) uint64 {
	return uint64(length)<<32 | uint64(value)
}

type pdfEncodingRange struct {
	first, last uint64
	target      uint32
}

type pdfEncodingCMap struct {
	base           *pdfEncodingCMap
	predefined     *pdfPredefinedCMap
	spaces         []pdfCMapSpace
	ranges, notdef []pdfEncodingRange
	vertical       bool
	collection     string
}

func pdfCIDCode(code uint64, font pdfFontInfo) uint16 {
	if font.encodingCMap != nil {
		return font.encodingCMap.cid(code)
	}
	if font.predefined != nil {
		return font.predefined.cid(uint32(code))
	}
	return uint16(code)
}

func pdfEncodingRangeLookup(ranges []pdfEncodingRange, code uint64, increment bool) (uint16, bool) {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].last >= code })
	if i < len(ranges) && ranges[i].first <= code {
		target := uint64(ranges[i].target)
		if increment {
			target += code - ranges[i].first
		}
		return uint16(target), true
	}
	return 0, false
}

func (c *pdfEncodingCMap) mappedCID(code uint64) (uint16, bool) {
	if !pdfEncodingContains(c.spaces, code) {
		return 0, false
	}
	if cid, ok := pdfEncodingRangeLookup(c.ranges, code, true); ok {
		return cid, true
	}
	if c.base != nil {
		return c.base.mappedCID(code)
	}
	if c.predefined != nil {
		if pdfEncodingContains(c.predefined.spaces, code) {
			return c.predefined.mappedCID(uint32(code))
		}
	}
	return 0, false
}

func (c *pdfEncodingCMap) cid(code uint64) uint16 {
	if !pdfEncodingContains(c.spaces, code) {
		return 0
	}
	if cid, ok := c.mappedCID(code); ok {
		return cid
	}
	for current := c; current != nil; current = current.base {
		if cid, ok := pdfEncodingRangeLookup(current.notdef, code, false); ok {
			return cid
		}
		if current.predefined != nil {
			if pdfEncodingContains(current.predefined.spaces, code) {
				return current.predefined.cid(uint32(code))
			}
		}
	}
	return 0
}

func (c *pdfEncodingCMap) unicode(code uint64) string {
	if !pdfEncodingContains(c.spaces, code) {
		return "\uFFFD"
	}
	if cid, ok := pdfEncodingRangeLookup(c.ranges, code, true); ok {
		switch c.collection {
		case "Adobe-GB1", "Adobe-CNS1", "Adobe-Japan1", "Adobe-Korea1":
			return pdfPredefinedCIDUnicode(c.collection, cid)
		}
		return "" // 已覆盖的代码不能继续使用基表中的 Unicode。
	}
	if c.base != nil {
		return c.base.unicode(code)
	}
	if c.predefined != nil {
		if pdfEncodingContains(c.predefined.spaces, code) {
			return c.predefined.unicode(uint32(code))
		}
	}
	return ""
}

func (p *pdfInterpreter) loadEncodingCMap(font *pdfFontInfo, stream *types.StreamDict) {
	cmap, err := p.readEncodingCMap(stream, 0, map[types.IndirectRef]bool{})
	if err != nil {
		p.report("font.cmap", err.Error())
		return
	}
	font.encodingCMap = cmap
	font.vertical = cmap.vertical
}

func (p *pdfInterpreter) readEncodingCMap(stream *types.StreamDict, depth int, path map[types.IndirectRef]bool) (*pdfEncodingCMap, error) {
	maximum := int64(64)
	if p.control != nil {
		maximum = p.control.options.Limits.MaxDepth
	}
	if int64(depth) > maximum {
		return nil, p.control.limit("depth", int64(depth), maximum)
	}
	if err := p.decodeStream(stream, 0); err != nil {
		return nil, err
	}
	if err := p.control.consumeOperations(int64(len(stream.Content))); err != nil {
		return nil, err
	}
	tokens, complete := tokenizePDFCMapLimit(string(stream.Content), maxCMapTokens)
	if !complete {
		return nil, fmt.Errorf("Encoding CMap token 数量超过上限 %d", maxCMapTokens)
	}
	cmap := &pdfEncodingCMap{}
	baseObject := stream.Dict["UseCMap"]
	var writingMode *bool
	for i, token := range tokens {
		if err := p.control.consumeOperations(1); err != nil {
			return nil, err
		}
		switch token {
		case "usecmap":
			if i == 0 || !strings.HasPrefix(tokens[i-1], "/") {
				return nil, fmt.Errorf("Encoding usecmap 缺少名称")
			}
			// 流字典中的声明优先于程序操作符。
			if stream.Dict["UseCMap"] == nil {
				baseObject = types.Name(decodeCMapName(tokens[i-1]))
			}
		case "/WMode":
			if i+1 >= len(tokens) || (tokens[i+1] != "0" && tokens[i+1] != "1") {
				return nil, fmt.Errorf("CMap WMode 必须为 0 或 1")
			}
			v := tokens[i+1] == "1"
			writingMode = &v
		}
	}
	if baseObject != nil {
		if ref, ok := baseObject.(types.IndirectRef); ok {
			if path[ref] {
				return nil, fmt.Errorf("Encoding UseCMap 存在循环继承")
			}
			path[ref] = true
			defer delete(path, ref)
		}
		object, err := dereferencePDFObject(p.ctx, baseObject)
		if err != nil {
			return nil, fmt.Errorf("Encoding UseCMap 无法读取: %w", err)
		}
		if name, ok := object.(types.Name); ok {
			cmap.predefined, err = loadPDFPredefinedCMap(name.Value())
			if err != nil || cmap.predefined == nil {
				return nil, fmt.Errorf("Encoding UseCMap 名称不可用: %s", name.Value())
			}
			cmap.spaces = append(cmap.spaces, cmap.predefined.spaces...)
			cmap.vertical, cmap.collection = cmap.predefined.vertical, cmap.predefined.collection
		} else if base := pdfMaskStream(object); base != nil {
			cmap.base, err = p.readEncodingCMap(base, depth+1, path)
			if err != nil {
				return nil, err
			}
			cmap.spaces = append(cmap.spaces, cmap.base.spaces...)
			cmap.vertical, cmap.collection = cmap.base.vertical, cmap.base.collection
		} else {
			return nil, fmt.Errorf("Encoding UseCMap 必须为预定义名称或 CMap 流")
		}
	}
	if writingMode != nil {
		cmap.vertical = *writingMode
	}
	if object := stream.Dict["WMode"]; object != nil {
		mode, ok := dereferencedPDFNumber(p.ctx, object)
		if !ok || (mode != 0 && mode != 1) {
			return nil, fmt.Errorf("CMap WMode 必须为 0 或 1")
		}
		cmap.vertical = mode == 1
	}
	if match := pdfCMapOrdering.FindSubmatch(stream.Content); len(match) == 2 {
		cmap.collection = "Adobe-" + string(match[1])
	}
	if object := stream.Dict["CIDSystemInfo"]; object != nil {
		if info, err := p.ctx.XRefTable.DereferenceDict(object); err == nil && info != nil {
			registry, regErr := p.ctx.XRefTable.DereferenceStringOrHexLiteral(info["Registry"], 0, nil)
			ordering, ordErr := p.ctx.XRefTable.DereferenceStringOrHexLiteral(info["Ordering"], 0, nil)
			if regErr == nil && ordErr == nil {
				cmap.collection = registry + "-" + ordering
			}
		}
	}
	mode := ""
	for i := 0; i < len(tokens); i++ {
		if err := p.control.consumeOperations(1); err != nil {
			return nil, err
		}
		switch tokens[i] {
		case "begincodespacerange", "begincidchar", "begincidrange", "beginnotdefchar", "beginnotdefrange":
			mode = tokens[i]
			continue
		case "endcodespacerange", "endcidchar", "endcidrange", "endnotdefchar", "endnotdefrange":
			mode = ""
			continue
		}
		if mode == "" || !strings.HasPrefix(tokens[i], "<") {
			continue
		}
		first, length, ok := pdfEncodingHex(tokens[i])
		last, targetIndex := first, i+1
		if strings.HasSuffix(mode, "range") {
			if i+1 >= len(tokens) {
				return nil, fmt.Errorf("Encoding CMap 区间缺少终点")
			}
			var lastLength int
			var lastOK bool
			last, lastLength, lastOK = pdfEncodingHex(tokens[i+1])
			ok = ok && lastOK && lastLength == length
			targetIndex++
		}
		if !ok || last < first {
			return nil, fmt.Errorf("Encoding CMap 代码必须为同长度的 1–4 字节十六进制串")
		}
		if mode == "begincodespacerange" {
			cmap.spaces = append(cmap.spaces, pdfCMapSpace{first, last, length})
			i++
			continue
		}
		if targetIndex >= len(tokens) {
			return nil, fmt.Errorf("Encoding CMap 缺少 CID")
		}
		target, err := strconv.ParseUint(tokens[targetIndex], 10, 16)
		notdef := strings.HasPrefix(mode, "beginnotdef")
		if err != nil || (!notdef && uint64(last-first)+target > 65535) {
			return nil, fmt.Errorf("Encoding CMap CID 超出有效范围")
		}
		r := pdfEncodingRange{pdfEncodingCode(first, length), pdfEncodingCode(last, length), uint32(target)}
		if notdef {
			cmap.notdef = append(cmap.notdef, r)
		} else {
			cmap.ranges = append(cmap.ranges, r)
		}
		i = targetIndex
	}
	if len(cmap.spaces) == 0 {
		return nil, fmt.Errorf("Encoding CMap 缺少代码空间")
	}
	if err := p.validateEncodingSpaces(cmap.spaces); err != nil {
		return nil, err
	}
	for _, ranges := range [][]pdfEncodingRange{cmap.ranges, cmap.notdef} {
		for _, r := range ranges {
			if err := p.control.consumeOperations(int64(len(cmap.spaces))); err != nil {
				return nil, err
			}
			if !pdfEncodingContains(cmap.spaces, r.first) || !pdfEncodingContains(cmap.spaces, r.last) {
				return nil, fmt.Errorf("Encoding CID 映射超出代码空间")
			}
		}
	}
	var err error
	cmap.ranges, err = normalizePDFEncodingRanges(cmap.ranges, true, p.control)
	if err == nil {
		cmap.notdef, err = normalizePDFEncodingRanges(cmap.notdef, false, p.control)
	}
	return cmap, err
}

func decodeCMapName(token string) string {
	name := strings.TrimPrefix(token, "/")
	var result strings.Builder
	for i := 0; i < len(name); i++ {
		if name[i] == '#' && i+2 < len(name) {
			hi, hiOK := hexNibble(name[i+1])
			lo, loOK := hexNibble(name[i+2])
			if hiOK && loOK {
				result.WriteByte(hi<<4 | lo)
				i += 2
				continue
			}
		}
		result.WriteByte(name[i])
	}
	return result.String()
}

func pdfEncodingHex(token string) (uint32, int, bool) {
	if !strings.HasPrefix(token, "<") || !strings.HasSuffix(token, ">") {
		return 0, 0, false
	}
	value := normalizePDFHex(token)
	if len(value) == 0 || len(value) > 8 {
		return 0, 0, false
	}
	code, ok := pdfHexUint(value)
	return code, len(value) / 2, ok
}

// 代码空间的上下界逐字节判断，不把整个代码作为整数区间判断。
func pdfCMapSpaceContains(space pdfCMapSpace, code uint32, length int) bool {
	if space.bytes != length {
		return false
	}
	for i := 0; i < length; i++ {
		shift := uint(i * 8)
		b, low, high := byte(code>>shift), byte(space.first>>shift), byte(space.last>>shift)
		if b < low || b > high {
			return false
		}
	}
	return true
}

func pdfEncodingContains(spaces []pdfCMapSpace, code uint64) bool {
	for _, space := range spaces {
		if pdfCMapSpaceContains(space, uint32(code), int(code>>32)) {
			return true
		}
	}
	return false
}

func pdfCMapCodeLength(spaces []pdfCMapSpace, data []byte) int {
	var code uint32
	for n := 1; n <= min(4, len(data)); n++ {
		code = code<<8 | uint32(data[n-1])
		for _, space := range spaces {
			if pdfCMapSpaceContains(space, code, n) {
				return n
			}
		}
	}
	return 1 // 无效或截断代码只消耗一个字节，保留后续有效代码。
}

func (p *pdfInterpreter) validateEncodingSpaces(spaces []pdfCMapSpace) error {
	for i, a := range spaces {
		if err := p.control.consumeOperations(int64(len(spaces)) + 4); err != nil {
			return err
		}
		if a.bytes < 1 || a.bytes > 4 || !pdfCMapSpaceContains(a, a.first, a.bytes) {
			return fmt.Errorf("Encoding CMap 代码空间无效")
		}
		for _, b := range spaces[:i] {
			if a.bytes == b.bytes {
				continue // 继承与本地代码空间可以在相同长度上重复或重叠。
			}
			overlap := true
			for j := 0; j < min(a.bytes, b.bytes); j++ {
				sa, sb := uint((a.bytes-1-j)*8), uint((b.bytes-1-j)*8)
				if byte(a.last>>sa) < byte(b.first>>sb) || byte(b.last>>sb) < byte(a.first>>sa) {
					overlap = false
					break
				}
			}
			if overlap {
				return fmt.Errorf("Encoding CMap 不同长度的代码空间存在歧义")
			}
		}
	}
	return nil
}

type pdfEncodingHeap []int

func (h pdfEncodingHeap) Len() int           { return len(h) }
func (h pdfEncodingHeap) Less(i, j int) bool { return h[i] > h[j] }
func (h pdfEncodingHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *pdfEncodingHeap) Push(v any)        { *h = append(*h, v.(int)) }
func (h *pdfEncodingHeap) Pop() any {
	v := (*h)[len(*h)-1]
	*h = (*h)[:len(*h)-1]
	return v
}

// 扫描排序后的端点，以 O(n log n) 保留重叠区间的最后声明，避免把四字节
// notdef 范围展开成数十亿条映射。
func normalizePDFEncodingRanges(ranges []pdfEncodingRange, increment bool, control *conversionControl) ([]pdfEncodingRange, error) {
	if len(ranges) == 0 {
		return nil, nil
	}
	if err := control.consumeOperations(int64(len(ranges)) * int64(8+4*bits.Len(uint(len(ranges))))); err != nil {
		return nil, err
	}
	type event struct {
		position uint64
		index    int
		start    bool
	}
	events := make([]event, 0, 2*len(ranges))
	for i, r := range ranges {
		events = append(events, event{r.first, i, true}, event{r.last + 1, i, false})
	}
	sort.Slice(events, func(i, j int) bool { return events[i].position < events[j].position })
	active := make([]bool, len(ranges))
	var priority pdfEncodingHeap
	var result []pdfEncodingRange
	for i := 0; i < len(events); {
		if err := control.check(); err != nil {
			return nil, err
		}
		position := events[i].position
		for i < len(events) && events[i].position == position {
			e := events[i]
			active[e.index] = e.start
			if e.start {
				heap.Push(&priority, e.index)
			}
			i++
		}
		for len(priority) > 0 && !active[priority[0]] {
			heap.Pop(&priority)
		}
		if len(priority) > 0 && i < len(events) {
			r := ranges[priority[0]]
			target := r.target
			if increment {
				target += uint32(position - r.first)
			}
			result = append(result, pdfEncodingRange{position, events[i].position - 1, target})
		}
	}
	return result, nil
}
