package fontrepair

import "slices"

type dictEntry struct {
	start, operator, end int
	op                   int
}

// dictEntries 保留 DICT 条目的字节边界，以便只重写目标操作数。
func dictEntries(b []byte) ([]dictEntry, bool) {
	var entries []dictEntry
	start := 0
	for pos := 0; pos < len(b); {
		if b[pos] > 21 {
			_, n, err := dictNumber(b[pos:])
			if err != nil {
				return nil, false
			}
			pos += n
			continue
		}
		operator, op := pos, int(b[pos])
		pos++
		if op == 12 {
			if pos == len(b) {
				return nil, false
			}
			op = 0xc00 + int(b[pos])
			pos++
		}
		entries = append(entries, dictEntry{start, operator, pos, op})
		start = pos
	}
	return entries, start == len(b)
}

// fixedDictInteger 使用原操作数的字节数编码整数，保持其他 CFF 偏移不变。
func fixedDictInteger(b []byte, value int) bool {
	switch len(b) {
	case 1:
		if value < -107 || value > 107 {
			return false
		}
		b[0] = byte(value + 139)
	case 2:
		if value < 108 || value > 1131 {
			return false
		}
		value -= 108
		b[0], b[1] = byte(247+value/256), byte(value)
	case 3:
		if value < -32768 || value > 32767 {
			return false
		}
		b[0] = 28
		be.PutUint16(b[1:], uint16(value))
	case 5:
		if value < 0 || uint64(value) > 0x7fffffff {
			return false
		}
		b[0] = 29
		be.PutUint32(b[1:], uint32(value))
	default:
		return false
	}
	return true
}

// repairCFFPrivate 移除旧生成器遗留的 ForceBoldThreshold、lenIV。
// Adobe 5176 表23已不定义这两个旧 Type1 私有项，浏览器 OTS 会拒绝包含它们的 CFF。
// 新私有字典追加在表尾，只改写原有 Private 引用；字形、FD、子程序均不重排。
func repairCFFPrivate(input []byte) []byte {
	if !isCFF(input) || int(input[2]) > len(input) {
		return input
	}
	_, pos, err := cffIndex(input, int(input[2]))
	if err != nil {
		return input
	}
	tops, _, err := cffIndex(input, pos)
	if err != nil || len(tops) != 1 {
		return input
	}
	top, err := parseDict(tops[0])
	if err != nil {
		return input
	}
	dicts := tops
	if _, cid := top[0xc1e]; cid {
		fds, _, err := cffIndex(input, top.integer(0xc24))
		if err != nil {
			return input
		}
		dicts = append(slices.Clone(tops), fds...)
	}
	var result []byte
	changed := false
	for _, dict := range dicts {
		entries, ok := dictEntries(dict)
		if !ok {
			continue
		}
		for _, entry := range entries {
			if entry.op != 18 {
				continue
			}
			size, n, err := dictNumber(dict[entry.start:entry.operator])
			if err != nil {
				continue
			}
			offset, m, err := dictNumber(dict[entry.start+n : entry.operator])
			if err != nil || entry.start+n+m != entry.operator || size < 0 || offset < 0 ||
				offset+size > float64(len(input)) || size != float64(int(size)) || offset != float64(int(offset)) {
				continue
			}
			private := input[int(offset):int(offset+size)]
			parsed, err := parseDict(private)
			if err != nil {
				continue
			}
			// 只处理明确声明未加密的 Type2 字形；其他 lenIV 值可能表示
			// 加密的 Type1 程序，不能通过删除标记假装它可以直接读取。
			if lenIV := parsed[0xc10]; len(lenIV) > 0 && (len(lenIV) != 1 || lenIV[0] != -1) {
				continue
			}
			fields, ok := dictEntries(private)
			if !ok {
				continue
			}
			var clean, subrs []byte
			removed, valid := false, true
			for _, field := range fields {
				if field.op == 0xc0f || field.op == 0xc10 {
					removed = true
					continue
				}
				clean = append(clean, private[field.start:field.end]...)
			}
			if !removed {
				continue
			}
			// Local Subrs 相对于私有字典起点寻址，复制到新字典后方并同步引用。
			cleanFields, _ := dictEntries(clean)
			for _, field := range cleanFields {
				if field.op != 19 {
					continue
				}
				relative, _, err := dictNumber(clean[field.start:field.operator])
				if err != nil || relative < 0 || offset+relative > float64(len(input)) || relative != float64(int(relative)) {
					valid = false
					break
				}
				start := int(offset + relative)
				_, end, err := cffIndex(input, start)
				if err != nil || !fixedDictInteger(clean[field.start:field.operator], len(clean)) {
					valid = false
					break
				}
				subrs = input[start:end]
			}
			if !valid {
				continue
			}
			// 多个 FD 可共用同一私有字典，限制重复复制的总量，避免恶意
			// 引用把很小的字体扩成巨大输出。
			if len(clean)+len(subrs) > len(input)-max(0, len(result)-len(input)) {
				continue
			}
			if result == nil {
				result = slices.Clone(input)
			}
			reference := slices.Clone(dict[entry.start:entry.operator])
			if !fixedDictInteger(reference[:n], len(clean)) || !fixedDictInteger(reference[n:], len(result)) {
				continue
			}
			// dict 是原输入的切片；通过其起点定位原始引用，避免修改输入。
			// INDEX 返回的子切片容量延伸至表尾，可用容量计算确切起点。
			dictOffset := cap(input) - cap(dict)
			copy(result[dictOffset+entry.start:dictOffset+entry.operator], reference)
			result = append(result, clean...)
			result = append(result, subrs...)
			changed = true
		}
	}
	if !changed {
		return input
	}
	return result
}
