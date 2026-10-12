package pdf2ofd

import (
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

func pdfCIDCode(code uint16, font pdfFontInfo) uint16 {
	if font.codeToCID == nil {
		return code
	}
	if cid, ok := font.codeToCID[code]; ok {
		return cid
	}
	if font.identityCMap {
		return code
	}
	return 0
}

// 支持 1/2 字节固定宽度 Encoding CMap；CID 决定 W/W2 和 CIDToGIDMap 的索引，
// ToUnicode 仍按原代码索引。变长、3/4 字节及非 Identity UseCMap 明确诊断。
func (p *pdfInterpreter) loadEncodingCMap(font *pdfFontInfo, stream *types.StreamDict, tokens []string) {
	base := ""
	if name, ok := dereferenceName(p.ctx, stream.Dict["UseCMap"]); ok {
		base = name
	} else if stream.Dict["UseCMap"] != nil {
		p.report("font.cmap", "流形式的 Encoding UseCMap 尚未支持")
		return
	}
	for i, token := range tokens {
		if token == "usecmap" && i > 0 {
			base = strings.TrimPrefix(tokens[i-1], "/")
		}
	}
	if base != "" {
		if base != "Identity-H" && base != "Identity-V" {
			p.report("font.cmap", "非 Identity 的 Encoding UseCMap 尚未支持")
			return
		}
		font.identityCMap = true
		if base == "Identity-V" {
			font.vertical = true
		}
		// 显式 WMode 覆盖继承的值。
		for i, token := range tokens {
			if token == "/WMode" && i+1 < len(tokens) {
				font.vertical = tokens[i+1] == "1"
			}
		}
	}
	if object := stream.Dict["WMode"]; object != nil {
		mode, ok := dereferencedPDFNumber(p.ctx, object)
		if !ok || (mode != 0 && mode != 1) {
			p.report("font.cmap", "CMap WMode 必须为 0 或 1")
			return
		}
		font.vertical = mode == 1
	}
	font.codeToCID = make(map[uint16]uint16)
	mode := ""
	width := 0
	source := func(token string) (uint16, bool) {
		if len(token) != 4 && len(token) != 6 {
			return 0, false
		}
		return pdfHexUint(token)
	}
	for i := 0; i < len(tokens); i++ {
		if p.control.consumeOperations(1) != nil {
			return
		}
		switch tokens[i] {
		case "begincodespacerange":
			mode = "space"
			continue
		case "begincidchar":
			mode = "char"
			continue
		case "begincidrange":
			mode = "range"
			continue
		case "endcodespacerange", "endcidchar", "endcidrange":
			mode = ""
			continue
		}
		if mode == "" || !strings.HasPrefix(tokens[i], "<") {
			continue
		}
		first, ok := source(tokens[i])
		if !ok {
			p.report("font.cmap", "Encoding CMap 仅支持 1/2 字节代码")
			return
		}
		if mode == "space" {
			if i+1 >= len(tokens) || len(tokens[i+1]) != len(tokens[i]) {
				p.report("font.cmap", "CMap 代码空间无效")
				return
			}
			last, ok := source(tokens[i+1])
			if !ok || last < first {
				p.report("font.cmap", "CMap 代码空间无效")
				return
			}
			w := (len(tokens[i]) - 2) / 2
			if width != 0 && width != w {
				p.report("font.cmap", "变长嵌入 Encoding CMap 尚未支持")
				return
			}
			width = w
			i++
			continue
		}
		last := first
		targetIndex := i + 1
		if mode == "range" {
			if i+2 >= len(tokens) {
				p.report("font.cmap", "CID 区间不完整")
				return
			}
			last, ok = source(tokens[i+1])
			targetIndex++
		}
		if !ok || last < first || targetIndex >= len(tokens) {
			p.report("font.cmap", "CID 映射不完整或无效")
			return
		}
		target, err := strconv.ParseUint(tokens[targetIndex], 10, 16)
		if err != nil || int(target)+int(last-first) > 65535 {
			p.report("font.cmap", "CID 映射超出有效范围")
			return
		}
		for code := int(first); code <= int(last); code++ {
			if p.control.consumeOperations(1) != nil {
				return
			}
			font.codeToCID[uint16(code)] = uint16(target + uint64(code-int(first)))
		}
		i = targetIndex
	}
	if width != 0 {
		font.codeBytes = width
	}
}
