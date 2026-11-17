package models

import (
	"encoding/xml"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestColorPreservesUnicodeWhitespaceAndValidatesExtraComponents(t *testing.T) {
	var color spec.ColorValue
	err := color.UnmarshalXMLAttr(xml.Attr{Value: "1\u00852\u00a03\u30004\t5"})
	values, count, ok := color.Components()
	if err != nil || values != [4]int{1, 2, 3, 4} || count != 5 || !ok {
		t.Fatalf("Unicode 颜色: %+v, %v", color, err)
	}
	if err := color.UnmarshalXMLAttr(xml.Attr{Value: "1 2 3 4 invalid"}); err == nil {
		t.Fatal("未校验多余的颜色分量")
	}
}

func BenchmarkColorAttribute(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		var value spec.ColorValue
		if err := value.UnmarshalXMLAttr(xml.Attr{Value: "128 64 255"}); err != nil {
			b.Fatal(err)
		}
	}
}
