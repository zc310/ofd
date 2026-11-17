package canvas

import (
	"fmt"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/thirdparty/canvas/text"
	"golang.org/x/image/font/gofont/goregular"
)

type optionalBreakLinebreaker struct{}

func (optionalBreakLinebreaker) Linebreak(items []text.Item, _ float64) []text.Break {
	var result []text.Break
	width := 0.0
	for i, item := range items {
		if item.Type != text.PenaltyType {
			width += item.Width
		} else if item.Size > 0 && item.Flagged || item.Penalty <= -text.Infinity {
			result = append(result, text.Break{Position: i, Width: width + item.Width})
			width = 0
		}
	}
	return result
}

func TestRichTextUnusedOptionalBreaksPreserveFollowingText(t *testing.T) {
	family := NewFontFamily("optional-breaks")
	if err := family.LoadFont(goregular.TTF, 0, FontRegular); err != nil {
		t.Fatal(err)
	}
	face := family.Face(12, Black)

	for _, sep := range []string{"\u00ad", "\u200b"} {
		for _, align := range []TextAlign{Left, Justify} {
			t.Run(fmt.Sprintf("%U_%v", []rune(sep)[0], align), func(t *testing.T) {
				input := "abc" + sep + "def ghi" + sep + "jkl mno"
				rich := NewRichText(face)
				rich.WriteString(input)
				result := rich.ToText(1000, 0, align, Top, nil)
				var got strings.Builder
				count := 0
				result.WalkSpans(func(_, _ float64, span TextSpan) {
					got.WriteString(span.Text)
					for _, glyph := range span.Glyphs {
						if glyph.Text == sep {
							count++
							if glyph.XAdvance != 0 {
								t.Errorf("optional break has nonzero advance: %d", glyph.XAdvance)
							}
							if space := glyph.SFNT.GlyphIndex(' '); space != 0 && glyph.ID != space {
								t.Errorf("optional break glyph = %d, want space glyph %d", glyph.ID, space)
							}
						}
					}
				})
				if got.String() != input {
					t.Errorf("text = %q, want %q", got.String(), input)
				}
				if count != 2 {
					t.Errorf("optional break glyph count = %d, want 2", count)
				}
			})
		}
	}
}

func TestRichTextUsedOptionalBreakPreservesNextLine(t *testing.T) {
	family := NewFontFamily("used-optional-break")
	if err := family.LoadFont(goregular.TTF, 0, FontRegular); err != nil {
		t.Fatal(err)
	}
	for _, sep := range []string{"\u00ad", "\u200b"} {
		t.Run(fmt.Sprintf("%U", []rune(sep)[0]), func(t *testing.T) {
			rich := NewRichText(family.Face(12, Black))
			rich.WriteString("abc" + sep + "def")
			result := rich.ToText(100, 0, Left, Top, &TextOptions{Linebreaker: optionalBreakLinebreaker{}})
			var lines []string
			var firstGlyphs []string
			result.WalkLines(func(_ float64, spans []TextSpan) {
				var line strings.Builder
				for _, span := range spans {
					line.WriteString(span.Text)
					if len(lines) == 0 {
						for _, glyph := range span.Glyphs {
							firstGlyphs = append(firstGlyphs, glyph.Text)
						}
					}
				}
				lines = append(lines, line.String())
			})
			if len(lines) != 2 || lines[0] != "abc"+sep || lines[1] != "def" {
				t.Fatalf("lines = %q, want %q and %q", lines, "abc"+sep, "def")
			}
			if sep == "\u00ad" && (len(firstGlyphs) == 0 || firstGlyphs[len(firstGlyphs)-1] != "-") {
				t.Errorf("used soft hyphen glyphs = %q, want visible hyphen", firstGlyphs)
			}
		})
	}
}
