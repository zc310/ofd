package creator

import (
	"strings"
	"testing"
)

// GB/T 33190—2016 11.4.1、表 48：按起始位置和字符数量确定字形变换的字符范围。
func TestCreateValidatesCGTransformCodeCountWithoutOverflow(t *testing.T) {
	for _, source := range []string{"整体文字", "分段码位"} {
		t.Run(source, func(t *testing.T) {
			for _, tc := range []struct {
				name    string
				count   int
				wantErr bool
			}{
				{"默认数量", 0, false},
				{"恰好到末尾", 1, false},
				{"超过末尾", 2, true},
				{"整数溢出", int(^uint(0) >> 1), true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					text := Text{Width: 20, Height: 10, Value: "甲乙", CGTransforms: []CGTransform{
						{CodePosition: 1, CodeCount: tc.count, GlyphCount: 1, Glyphs: []int{1}},
					}}
					if source == "分段码位" {
						text.Value = ""
						text.TextCodes = []TextCode{{Value: "甲"}, {Value: "乙"}}
					}
					_, err := Marshal(Document{ID: "cg-transform-range", Pages: []Page{{Items: []Item{text}}}})
					if tc.wantErr {
						if err == nil || !strings.Contains(err.Error(), "CodeCount 超出文字范围") {
							t.Fatalf("CodeCount=%d 应被字符范围校验拒绝，实际错误: %v", tc.count, err)
						}
					} else if err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}
