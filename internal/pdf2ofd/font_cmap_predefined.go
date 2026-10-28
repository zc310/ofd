package pdf2ofd

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/zc310/ofd/internal/cidmap"
)

// Adobe BSD 映射数据保留原始版权声明，来源与重建方法见 cmaps/README.md。
// Adobe-GB1 CID→Unicode 由 internal/cidmap 共用，不在这里重复嵌入。
//
//go:embed cmaps/adobe.zip
var pdfAdobeCMaps []byte

var pdfCMapArchive = sync.OnceValues(func() (map[string]*zip.File, error) {
	archive, err := zip.NewReader(bytes.NewReader(pdfAdobeCMaps), int64(len(pdfAdobeCMaps)))
	if err != nil {
		return nil, err
	}
	files := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		files[file.Name] = file
	}
	return files, nil
})

var pdfPredefinedCMaps sync.Map
var pdfCIDUnicodeMaps sync.Map
var pdfCMapOrdering = regexp.MustCompile(`/Ordering\s+\(([^)]+)\)`)

type pdfCMapRange struct{ first, last, target uint32 }
type pdfCMapSpace struct {
	first, last uint32
	bytes       int
}

type pdfPredefinedCMap struct {
	name, collection string
	vertical         bool
	base             *pdfPredefinedCMap
	spaces           []pdfCMapSpace
	ranges, notdef   []pdfCMapRange
}

func pdfCMapResource(name string) ([]byte, error) {
	files, err := pdfCMapArchive()
	if err != nil || files[name] == nil {
		return nil, err
	}
	r, err := files[name].Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func loadPDFPredefinedCMap(name string) (*pdfPredefinedCMap, error) {
	return loadPDFPredefinedCMapDepth(name, 0)
}

func loadPDFPredefinedCMapDepth(name string, depth int) (*pdfPredefinedCMap, error) {
	if name == "Identity-H" || name == "Identity-V" {
		return &pdfPredefinedCMap{name: name, vertical: name == "Identity-V", spaces: []pdfCMapSpace{{0, 65535, 2}}, ranges: []pdfCMapRange{{0, 65535, 0}}}, nil
	}
	if value, ok := pdfPredefinedCMaps.Load(name); ok {
		return value.(*pdfPredefinedCMap), nil
	}
	data, err := pdfCMapResource(name)
	if err != nil || data == nil {
		return nil, err
	}
	if strings.HasSuffix(name, "-UCS2") {
		return nil, nil // CID→Unicode 表不属于 Encoding CMap。
	}
	if depth > 16 {
		return nil, fmt.Errorf("预定义 CMap 继承过深: %s", name)
	}
	cmap := &pdfPredefinedCMap{name: name}
	if match := pdfCMapOrdering.FindSubmatch(data); len(match) == 2 {
		cmap.collection = "Adobe-" + string(match[1])
	}
	tokens, complete := tokenizePDFCMapLimit(string(data), maxCMapTokens)
	if !complete {
		return nil, fmt.Errorf("预定义 CMap token 数量超过上限: %s", name)
	}
	mode := ""
	for i := 0; i < len(tokens); i++ {
		switch tokens[i] {
		case "usecmap":
			if i == 0 {
				return nil, fmt.Errorf("预定义 CMap 缺少继承名称: %s", name)
			}
			cmap.base, err = loadPDFPredefinedCMapDepth(strings.TrimPrefix(tokens[i-1], "/"), depth+1)
			if err != nil || cmap.base == nil {
				return nil, fmt.Errorf("预定义 CMap 继承无效: %s", name)
			}
			cmap.spaces = append([]pdfCMapSpace(nil), cmap.base.spaces...)
		case "/WMode":
			if i+1 < len(tokens) {
				cmap.vertical = tokens[i+1] == "1"
			}
		case "begincodespacerange", "begincidchar", "begincidrange", "beginnotdefchar", "beginnotdefrange":
			mode = tokens[i]
		case "endcodespacerange", "endcidchar", "endcidrange", "endnotdefchar", "endnotdefrange":
			mode = ""
		default:
			if mode == "" || !strings.HasPrefix(tokens[i], "<") {
				continue
			}
			first, ok := pdfHexUint(tokens[i])
			last, targetIndex := first, i+1
			if mode == "begincodespacerange" || strings.HasSuffix(mode, "range") {
				if i+1 >= len(tokens) {
					return nil, fmt.Errorf("预定义 CMap 区间缺少终点: %s", name)
				}
				last, ok = pdfHexUint(tokens[i+1])
				targetIndex++
			}
			if !ok || last < first {
				return nil, fmt.Errorf("预定义 CMap 区间无效: %s", name)
			}
			if mode == "begincodespacerange" {
				cmap.spaces = append(cmap.spaces, pdfCMapSpace{first, last, (len(tokens[i]) - 2) / 2})
				i++
				continue
			}
			if targetIndex >= len(tokens) {
				return nil, fmt.Errorf("预定义 CMap 缺少 CID: %s", name)
			}
			target, err := strconv.ParseUint(tokens[targetIndex], 10, 16)
			if err != nil {
				return nil, fmt.Errorf("预定义 CMap CID 无效: %s", name)
			}
			r := pdfCMapRange{first, last, uint32(target)}
			if strings.HasPrefix(mode, "beginnotdef") {
				cmap.notdef = append(cmap.notdef, r)
			} else {
				if uint64(last-first)+target > 65535 {
					return nil, fmt.Errorf("预定义 CMap CID 区间溢出: %s", name)
				}
				cmap.ranges = append(cmap.ranges, r)
			}
			i = targetIndex
		}
	}
	sort.Slice(cmap.ranges, func(i, j int) bool { return cmap.ranges[i].last < cmap.ranges[j].last })
	value, _ := pdfPredefinedCMaps.LoadOrStore(name, cmap)
	return value.(*pdfPredefinedCMap), nil
}

func (c *pdfPredefinedCMap) mappedCID(code uint32) (uint16, bool) {
	i := sort.Search(len(c.ranges), func(i int) bool { return c.ranges[i].last >= code })
	if i < len(c.ranges) && c.ranges[i].first <= code {
		return uint16(c.ranges[i].target + code - c.ranges[i].first), true
	}
	if c.base != nil {
		return c.base.mappedCID(code)
	}
	return 0, false
}

func (c *pdfPredefinedCMap) cid(code uint32) uint16 {
	if cid, ok := c.mappedCID(code); ok {
		return cid
	}
	for current := c; current != nil; current = current.base {
		for _, r := range current.notdef {
			if r.first <= code && code <= r.last {
				return uint16(r.target)
			}
		}
	}
	return 0
}

func (c *pdfPredefinedCMap) codeLength(data []byte) int {
	return pdfCMapCodeLength(c.spaces, data)
}

func (c *pdfPredefinedCMap) unicode(code uint32) string {
	if c.name == "Identity-H" || c.name == "Identity-V" {
		return ""
	}
	if strings.Contains(c.name, "UTF16") || strings.Contains(c.name, "UCS2") {
		if code > 65535 {
			return string(utf16.Decode([]uint16{uint16(code >> 16), uint16(code)}))
		}
		return string(utf16.Decode([]uint16{uint16(code)}))
	}
	if strings.Contains(c.name, "UTF32") {
		if code <= utf8.MaxRune && (code < 0xd800 || code > 0xdfff) {
			return string(rune(code))
		}
		return "\uFFFD"
	}
	if strings.Contains(c.name, "UTF8") {
		var raw [4]byte
		n := 4
		for i := 3; i >= 0; i-- {
			raw[i] = byte(code)
			code >>= 8
		}
		for n > 1 && raw[4-n] == 0 {
			n--
		}
		if utf8.Valid(raw[4-n:]) {
			return string(raw[4-n:])
		}
		return "\uFFFD"
	}
	// 提取文本使用横排映射，避免把原始标点变为兼容区竖排码位。
	semantic := c
	for semantic.vertical && semantic.base != nil {
		semantic = semantic.base
	}
	return pdfPredefinedCIDUnicode(c.collection, semantic.cid(code))
}

func pdfPredefinedCIDUnicode(collection string, cid uint16) string {
	if collection == "Adobe-GB1" {
		if text, ok := cidmap.GB1Text(int(cid)); ok {
			return text
		}
		return "\uFFFD"
	}
	value, ok := pdfCIDUnicodeMaps.Load(collection)
	if !ok {
		data, err := pdfCMapResource(collection + "-UCS2")
		if err != nil || data == nil {
			return "\uFFFD"
		}
		mapping := parsePDFToUnicode(data)
		value, _ = pdfCIDUnicodeMaps.LoadOrStore(collection, mapping)
	}
	if text := value.(map[uint32]string)[uint32(cid)]; text != "" {
		return text
	}
	return "\uFFFD"
}

func pdfCodeWordSpacing(code uint64, font pdfFontInfo) bool {
	if uint32(code) != 32 {
		return false
	}
	if font.encodingCMap != nil {
		return code>>32 == 1
	}
	if font.predefined != nil {
		for _, space := range font.predefined.spaces {
			if space.bytes == 1 && space.first <= 32 && 32 <= space.last {
				return true
			}
		}
		return false
	}
	return font.codeBytes != 2 || pdfCJKVariableWidth(font.encoding)
}

func pdfCodeUnicode(code uint64, font pdfFontInfo) string {
	if font.encodingCMap != nil {
		if value, ok := font.encodedUnicode[code]; ok {
			return value
		}
		return font.encodingCMap.unicode(code)
	}
	if value, ok := font.toUnicode[uint32(code)]; ok {
		return value
	}
	if font.predefined != nil {
		return font.predefined.unicode(uint32(code))
	}
	return ""
}
