package spec_test

import (
	"encoding/json"
	"encoding/xml"
	"math"
	"reflect"
	"strings"
	"testing"

	jsonv2 "github.com/goccy/go-json/v2"

	"github.com/zc310/ofd/pkg/spec"
)

func TestNumericModelsRoundTripXMLAttributesAndElements(t *testing.T) {
	type geometry struct {
		XMLName       xml.Name              `xml:"Geometry"`
		Point         spec.StPos            `xml:"Point,attr"`
		Box           spec.StBox            `xml:"Box,attr"`
		Matrix        spec.CTM              `xml:"Matrix,attr"`
		Offsets       spec.StArray[float64] `xml:"Offsets,attr"`
		Glyphs        spec.StArray[int]     `xml:"Glyphs,attr"`
		PointElement  spec.StPos            `xml:"Point"`
		BoxElement    spec.StBox            `xml:"Box"`
		MatrixElement spec.CTM              `xml:"Matrix"`
		OffsetElement spec.StArray[float64] `xml:"Offsets"`
		GlyphElement  spec.StArray[int]     `xml:"Glyphs"`
	}
	input := `<Geometry Point="1.25 -2" Box="1 2 -3 4" Matrix="1 0 0 1 2.5 -3.5" Offsets="1 g 2 3" Glyphs="1 2 3"><Point>1.25 -2</Point><Box>1 2 -3 4</Box><Matrix>1 0 0 1 2.5 -3.5</Matrix><Offsets>1 g 2 3</Offsets><Glyphs>1 2 3</Glyphs></Geometry>`
	var original geometry
	if err := xml.Unmarshal([]byte(input), &original); err != nil {
		t.Fatal(err)
	}
	data, err := xml.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`Point="1.25 -2"`, `Box="1 2 -3 4"`, `Matrix="1 0 0 1 2.5 -3.5"`, `Offsets="1 3 3"`, `<Point>1.25 -2</Point>`, `<Box>1 2 -3 4</Box>`, `<Matrix>1 0 0 1 2.5 -3.5</Matrix>`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("XML %s 未包含 %s", data, want)
		}
	}
	var decoded geometry
	if err := xml.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Fatalf("XML 往返改变了数值: %+v，期望 %+v", decoded, original)
	}
}

func TestNumericModelsXMLRejectsNonFiniteValues(t *testing.T) {
	for _, nonFinite := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, value := range []any{
			spec.StPos{X: nonFinite},
			spec.StBox{Width: nonFinite},
			spec.CTM{1, 0, 0, 1, nonFinite, 0},
			spec.StArray[float64]{1, nonFinite},
		} {
			if _, err := xml.Marshal(value); err == nil {
				t.Fatalf("元素编码未拒绝 %T 的非有限数值", value)
			}
			attr, ok := value.(xml.MarshalerAttr)
			if !ok {
				t.Fatalf("%T 未提供属性编码", value)
			}
			if _, err := attr.MarshalXMLAttr(xml.Name{Local: "Value"}); err == nil {
				t.Fatalf("属性编码未拒绝 %T 的非有限数值", value)
			}
		}
	}
}

func TestNumericModelsKeepJSONObjectsArraysAndNumbers(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  string
	}{
		{value: spec.StPos{X: 1, Y: 2}, want: `{"X":1,"Y":2}`},
		{value: spec.StBox{X: 1, Y: 2, Width: 3, Height: 4}, want: `{"X":1,"Y":2,"Width":3,"Height":4}`},
		{value: spec.IdentityCTM(), want: `[1,0,0,1,0,0]`},
		{value: spec.StArray[float64]{1, 2}, want: `[1,2]`},
		{value: spec.StArray[int]{3, 4}, want: `[3,4]`},
		{value: spec.StID(7), want: `7`},
		{value: spec.StRefID(7), want: `7`},
	} {
		for _, encode := range []func(any) ([]byte, error){json.Marshal, func(v any) ([]byte, error) { return jsonv2.Marshal(v) }} {
			data, err := encode(tc.value)
			if err != nil || string(data) != tc.want {
				t.Fatalf("%T 的 JSON = %s, %v，期望 %s", tc.value, data, err, tc.want)
			}
		}
	}
	var reference spec.StRefID
	if err := json.Unmarshal([]byte(`7`), &reference); err != nil || reference != 7 {
		t.Fatalf("标识引用不再支持 JSON 数字: %v, %v", reference, err)
	}
}

func TestCTMIdentityAndMultiplicationPreserveTransformSemantics(t *testing.T) {
	identity := spec.IdentityCTM()
	modified := spec.IdentityCTM()
	modified[4] = 100
	if identity != spec.IdentityCTM() {
		t.Fatal("修改单位矩阵副本影响了后续调用")
	}
	translate := spec.CTM{1, 0, 0, 1, 10, 20}
	scale := spec.CTM{2, 0, 0, 3, 0, 0}
	combined := translate.Multiply(&scale)
	x, y := combined.TransformPoint(spec.StPos{X: 4, Y: 5})
	if x != 18 || y != 35 || translate != (spec.CTM{1, 0, 0, 1, 10, 20}) {
		t.Fatalf("矩阵乘法的变换或输入发生变化: %v，点=(%g,%g)", combined, x, y)
	}
	var absent *spec.CTM
	if !absent.IsFinite() || !absent.IsInvertible() {
		t.Fatal("nil 矩阵应按单位矩阵判断")
	}
	zero := spec.CTM{}
	if zero.IsInvertible() {
		t.Fatal("零矩阵不可逆")
	}
}
