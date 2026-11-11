package spec_test

import (
	"encoding/xml"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestStIDAndReferenceShareCompatibilityMappingAcrossXMLForms(t *testing.T) {
	for _, value := range []string{"0", "18446744073709551615", "18446744073709551616", "object_1"} {
		var object struct {
			ID         spec.StID    `xml:"ID,attr"` //nolint:tagliatelle // 标准属性名为 ID。
			Ref        spec.StRefID `xml:"Ref,attr"`
			ElementID  spec.StID    `xml:"ID"` //nolint:tagliatelle // 标准元素名为 ID。
			ElementRef spec.StRefID `xml:"Ref"`
		}
		data := `<Object ID="` + value + `" Ref="` + value + `"><ID>` + value + `</ID><Ref>` + value + `</Ref></Object>`
		if err := xml.Unmarshal([]byte(data), &object); err != nil {
			t.Fatal(err)
		}
		if object.ID != object.ElementID || object.ID != spec.StID(object.Ref) || object.ID != spec.StID(object.ElementRef) {
			t.Fatalf("标识 %q 在不同入口的解析不一致: %+v", value, object)
		}
	}
}
