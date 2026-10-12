package pdf2ofd

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/go-text/typesetting/font/opentype/tables"
	"github.com/zc310/canvas"
	fontparser "github.com/zc310/font"
)

var pdfVerticalFontPath = canvas.FindSystemFont

// 显式 CIDToGIDMap 与内嵌 CID 字形已经选定源 PDF 的实际字形；只有替代
// Unicode 字体需要应用 OpenType 竖排替换。
func (p *pdfInterpreter) loadVerticalSubstitute(font *pdfFontInfo) {
	if runtime.GOOS == "js" || runtime.GOOS == "android" {
		return
	}
	style := canvas.FontRegular
	bold, italic, serif, _ := pdfFontStyleFlags(font.familyName)
	if bold {
		style = canvas.FontBold
	}
	if italic {
		style |= canvas.FontItalic
	}
	region := "SC"
	collection := ""
	if font.predefined != nil {
		collection = font.predefined.collection
	} else if font.encodingCMap != nil {
		collection = font.encodingCMap.collection
	}
	if collection != "" {
		switch collection {
		case "Adobe-CNS1":
			region = "TC"
		case "Adobe-Japan1":
			region = "JP"
		case "Adobe-Korea1":
			region = "KR"
		}
	}
	family := "Sans"
	if serif {
		family = "Serif"
	}
	names := []string{font.familyName, "Noto " + family + " CJK " + region, "Source Han " + family + " " + region}
	for _, name := range names {
		if p.control.check() != nil {
			return
		}
		path, ok := pdfVerticalFontPath(name, style)
		if !ok {
			continue
		}
		data, err := p.readVerticalSubstitute(path)
		if err != nil {
			if p.control.check() != nil {
				return
			}
			continue
		}
		sfnt, err := fontparser.ParseSFNT(data, 0)
		if err != nil || sfnt.Head == nil {
			continue
		}
		// 通用 CJK 回退必须覆盖中文，避免在字体安装不完整时仅凭族名选中
		// 只能绘制拉丁文字的字体面。
		if name != font.familyName && sfnt.GlyphIndex('中') == 0 {
			continue
		}
		font.data, font.sfnt, font.substitute = sfnt.Write(), sfnt, true
		font.format = "ttf"
		if sfnt.IsCFF {
			font.format = "otf"
		}
		if p.control != nil && int64(len(font.data)) > p.control.options.Limits.MaxFontBytes {
			p.control.limit("font_bytes", int64(len(font.data)), p.control.options.Limits.MaxFontBytes)
			return
		}
		font.verticalSubstitutions, err = pdfVerticalGSUB(sfnt, p.control, collection)
		if err != nil {
			p.report("font.vertical_glyph", err.Error())
		}
		return
	}
}

func (p *pdfInterpreter) readVerticalSubstitute(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	maximum := int64(64 << 20)
	if p.control != nil {
		maximum = p.control.options.Limits.MaxFontBytes
	}
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maximum {
		return nil, p.control.limit("font_bytes", info.Size(), maximum)
	}
	data, err := io.ReadAll(io.LimitReader(f, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, p.control.limit("font_bytes", int64(len(data)), maximum)
	}
	if p.control != nil {
		if int64(len(data)) > p.control.options.Limits.MaxTotalDecodedBytes-p.control.decoded {
			return nil, p.control.limit("total_decoded_bytes", p.control.decoded+int64(len(data)), p.control.options.Limits.MaxTotalDecodedBytes)
		}
		p.control.decoded += int64(len(data))
	}
	return data, nil
}

func pdfVerticalGSUB(sfnt *fontparser.SFNT, control *conversionControl, collections ...string) ([][]tables.SingleSubs, error) {
	data := sfnt.Tables["GSUB"]
	if len(data) == 0 {
		return nil, nil
	}
	if control.consumeOperations(int64(len(data))) != nil {
		return nil, control.check()
	}
	layout, _, err := tables.ParseLayout(data)
	if err != nil {
		return nil, fmt.Errorf("竖排 GSUB 无法解析: %w", err)
	}
	// vrt2 优先于 vert，避免在同一字形上连续执行两种替换。
	var indices []uint16
	features := map[int]bool{}
	if len(collections) > 0 {
		script, language := tables.Tag(0x68616e69), tables.Tag(0x5a485320) // 汉字脚本、简体中文（hani/ZHS）
		switch collections[0] {
		case "Adobe-CNS1":
			language = 0x5a485420 // 繁体中文（ZHT）
		case "Adobe-Japan1":
			script, language = 0x6b616e61, 0x4a414e20 // 假名脚本、日文（kana/JAN）
		case "Adobe-Korea1":
			script, language = 0x68616e67, 0x4b4f5220 // 韩文脚本、韩文（hang/KOR）
		}
		for _, wanted := range []tables.Tag{script, 0x44464c54} { // 再查默认脚本（DFLT）
			for i, record := range layout.ScriptList.Records {
				if record.Tag != wanted {
					continue
				}
				s := layout.ScriptList.Scripts[i]
				lang := s.DefaultLangSys
				for j, record := range s.LangSysRecords {
					if record.Tag == language {
						lang = &s.LangSys[j]
						break
					}
				}
				if lang != nil {
					for _, index := range lang.FeatureIndices {
						features[int(index)] = true
					}
					if lang.RequiredFeatureIndex != 65535 {
						features[int(lang.RequiredFeatureIndex)] = true
					}
				}
			}
			if len(features) > 0 {
				break
			}
		}
	}
	for _, tag := range []tables.Tag{0x76727432, 0x76657274} { // 先查 vrt2，再查 vert
		for i, record := range layout.FeatureList.Records {
			if record.Tag == tag && (len(features) == 0 || features[i]) {
				indices = append(indices, layout.FeatureList.Features[i].LookupListIndices...)
			}
		}
		if len(indices) > 0 {
			break
		}
	}
	seen := map[uint16]bool{}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	var result [][]tables.SingleSubs
	for _, index := range indices {
		if control.consumeOperations(1) != nil {
			return nil, control.check()
		}
		if seen[index] {
			continue
		}
		seen[index] = true
		if int(index) >= len(layout.LookupList.Lookups) {
			return nil, fmt.Errorf("竖排 GSUB 查找索引越界")
		}
		subtables, err := layout.LookupList.Lookups[index].AsGSUBLookups()
		if err != nil {
			return nil, err
		}
		var lookup []tables.SingleSubs
		for _, sub := range subtables {
			if extension, ok := sub.(tables.ExtensionSubs); ok {
				sub, err = extension.Resolve()
				if err != nil {
					return nil, err
				}
			}
			if single, ok := sub.(tables.SingleSubs); ok {
				lookup = append(lookup, single)
			} else {
				return nil, fmt.Errorf("竖排 GSUB 暂不支持上下文或多字形替换")
			}
		}
		result = append(result, lookup)
	}
	return result, nil
}

func pdfVerticalSubstituteGlyph(text string, font pdfFontInfo) uint16 {
	r, _ := utf8.DecodeRuneInString(text)
	g := font.sfnt.GlyphIndex(r)
	changed := false
	for _, lookup := range font.verticalSubstitutions {
		for _, sub := range lookup {
			index, ok := sub.Data.Cov().Index(tables.GlyphID(g))
			if !ok {
				continue
			}
			switch data := sub.Data.(type) {
			case tables.SingleSubstData1:
				g = uint16(int(g) + int(data.DeltaGlyphID))
				changed = true
			case tables.SingleSubstData2:
				if index < len(data.SubstituteGlyphIDs) {
					g = uint16(data.SubstituteGlyphIDs[index])
					changed = true
				}
			}
			break // 同一 lookup 中的子表择一使用。
		}
	}
	if !changed {
		if alternate := pdfVerticalPresentationRune(r); alternate != r {
			if glyph := font.sfnt.GlyphIndex(alternate); glyph != 0 {
				g = glyph
			}
		}
	}
	return g
}

// 替代字体没有匹配的 vert/vrt2 替换时，尝试 Unicode 竖排兼容形式；
// TextCode 始终保留原始 Unicode。
func pdfVerticalPresentationRune(r rune) rune {
	switch r {
	case '(':
		return '\uFE35'
	case ')':
		return '\uFE36'
	case '[':
		return '\uFE47'
	case ']':
		return '\uFE48'
	case '{':
		return '\uFE37'
	case '}':
		return '\uFE38'
	case ',':
		return '\uFE10'
	case ':':
		return '\uFE13'
	case ';':
		return '\uFE14'
	case '!':
		return '\uFE15'
	case '?':
		return '\uFE16'
	}
	const horizontal = "、。，：；！？（）［］｛｝〈〉《》「」『』【】〔〕〖〗—–…‥_"
	const vertical = "︑︒︐︓︔︕︖︵︶﹇﹈︷︸︿﹀︽︾﹁﹂﹃﹄︻︼︹︺︗︘︱︲︙︰︳"
	i := strings.IndexRune(horizontal, r)
	if i < 0 {
		return r
	}
	position := utf8.RuneCountInString(horizontal[:i])
	return []rune(vertical)[position]
}
