package report

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/go-text/typesetting/segmenter"
	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
)

// PrepareText 将控制字符、非法 XML 字符和缺字转义为可见文本。
// 仅当 UAX #14 的不可断片段超过整行时按字素补硬换行，不向文字层插入零宽字符。
func (f Font) PrepareText(ctx context.Context, text string, size, width float64) (string, error) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	var clean strings.Builder
	var buffer sfnt.Buffer
	for i, r := range text {
		if i%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}
		if r == '\n' {
			clean.WriteRune(r)
			continue
		}
		if r == '\t' {
			clean.WriteString("    ")
			continue
		}
		glyph, err := f.Face.GlyphIndex(&buffer, r)
		if err != nil {
			return "", err
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == 0xfffe || r == 0xffff || glyph == 0 {
			if r <= 0xffff {
				fmt.Fprintf(&clean, "\\u%04X", r)
			} else {
				fmt.Fprintf(&clean, "\\U%08X", r)
			}
		} else {
			clean.WriteRune(r)
		}
	}
	measure := func(text []rune) (float64, error) {
		w := 0.0
		for _, r := range text {
			if r == '\n' {
				continue
			}
			glyph, err := f.Face.GlyphIndex(&buffer, r)
			if err != nil {
				return 0, err
			}
			advance, err := f.Face.GlyphAdvance(&buffer, glyph, 64000, font.HintingNone)
			if err != nil {
				return 0, err
			}
			w += float64(advance) / 64000 * size * 25.4 / 72
		}
		return w, nil
	}
	var breaker segmenter.Segmenter
	breaker.Init([]rune(clean.String()))
	var out strings.Builder
	last := rune('\n')
	lines := breaker.LineIterator()
	for lines.Next() {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		part := lines.Line().Text
		w, err := measure(part)
		if err != nil {
			return "", err
		}
		if w <= width {
			out.WriteString(string(part))
		} else {
			if last != '\n' {
				out.WriteByte('\n')
			}
			var graphemes segmenter.Segmenter
			graphemes.Init(part)
			iterator := graphemes.GraphemeIterator()
			used, skipTo := 0.0, 0
			for iterator.Next() {
				grapheme := iterator.Grapheme()
				if grapheme.Offset < skipTo {
					continue
				}
				cluster := grapheme.Text
				// 可见转义作为一个整体，避免把 \\uFFFF 或 \\U0010FFFF 拆到两行。
				if cluster[0] == '\\' && grapheme.Offset+2 < len(part) {
					length := 0
					switch part[grapheme.Offset+1] {
					case 'u':
						length = 6
					case 'U':
						length = 10
					}
					if length > 0 && grapheme.Offset+length <= len(part) {
						candidate := part[grapheme.Offset : grapheme.Offset+length]
						if _, err := strconv.ParseUint(string(candidate[2:]), 16, 32); err == nil {
							cluster, skipTo = candidate, grapheme.Offset+length
						}
					}
				}
				advance, err := measure(cluster)
				if err != nil {
					return "", err
				}
				if used > 0 && used+advance > width {
					out.WriteByte('\n')
					used = 0
				}
				out.WriteString(string(cluster))
				used += advance
			}
		}
		if len(part) > 0 {
			last = part[len(part)-1]
		}
	}
	return out.String(), nil
}
