package models

import (
	"encoding/xml"
	"testing"
)

func TestImageBorderLineWidthPreservesOmissionAndExplicitZero(t *testing.T) {
	for _, tc := range []struct {
		name, attrs string
		width       *float64
	}{
		{"缺省", "", nil}, {"关闭", `LineWidth="0"`, new(0.0)}, {"正线宽", `LineWidth="0.5"`, new(0.5)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var object ImageObject
			if err := xml.Unmarshal([]byte(`<ImageObject Boundary="0 0 10 10" ResourceID="1"><Border `+tc.attrs+`/></ImageObject>`), &object); err != nil {
				t.Fatal(err)
			}
			if object.Border == nil {
				t.Fatal("图片边框未解析")
			}
			width := object.Border.LineWidth
			if (width == nil) != (tc.width == nil) || (width != nil && *width != *tc.width) {
				t.Fatalf("线宽缺省/零值语义发生变化: %s", tc.attrs)
			}
		})
	}
}
