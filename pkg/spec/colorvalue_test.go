package spec_test

import (
	"encoding/json"
	"encoding/xml"
	"image/color"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestColorValueXMLRoundTripPreservesRawChannelsInsteadOfRGBAFallback(t *testing.T) {
	for _, input := range []string{
		"65535 0 0", "15 0 0 0", "#FFFF #0000 #1234", "0 0 0 65535",
		"-1 65536 0", "1 2 3 4 5 6", "1\u00852\u00a03\u30004\t5", "",
	} {
		t.Run(input, func(t *testing.T) {
			var original spec.ColorValue
			if err := original.UnmarshalXMLAttr(xml.Attr{Value: input}); err != nil {
				t.Fatal(err)
			}
			attribute, err := original.MarshalXMLAttr(xml.Name{Local: "Value"})
			if err != nil {
				t.Fatal(err)
			}
			if attribute.Value != input {
				t.Fatalf("颜色编码改变了原始通道或位深: %q，期望 %q", attribute.Value, input)
			}
			var holder struct {
				XMLName xml.Name        `xml:"Color"`
				Value   spec.ColorValue `xml:"Value,attr"`
			}
			holder.Value = original
			data, err := xml.Marshal(holder)
			if err != nil {
				t.Fatal(err)
			}
			if err := xml.Unmarshal(data, &holder); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(holder.Value, original) {
				t.Fatalf("颜色属性往返改变了原始通道: %s", data)
			}
			data, err = xml.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var decoded spec.ColorValue
			if err := xml.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded, original) {
				t.Fatalf("颜色元素往返改变了原始通道: %s", data)
			}
		})
	}
}

func TestColorValueComponentsAndFallbackKeepExistingInterpretation(t *testing.T) {
	var value spec.ColorValue
	if err := value.UnmarshalXMLAttr(xml.Attr{Value: "#FFFF 0 0 65535 17"}); err != nil {
		t.Fatal(err)
	}
	components, count, ok := value.Components()
	if !ok || count != 5 || components != [4]int{65535, 0, 0, 65535} || value.RGBA != (color.RGBA{R: 255, A: 255}) {
		t.Fatalf("原始分量和既有8位回退表示改变: %+v, %v, %d", value, components, count)
	}
	if err := xml.Unmarshal([]byte(`<ColorValue/>`), &value); err != nil {
		t.Fatal(err)
	}
	if _, count, ok = value.Components(); count != 0 || ok || value.RGBA != (color.RGBA{A: 255}) {
		t.Fatal("重复解码空颜色应清除先前的分量")
	}
}

func TestConstructedColorValueUsesRGBAFallbackWithoutChangingJSON(t *testing.T) {
	value := spec.ColorValue{RGBA: color.RGBA{R: 1, G: 2, B: 3, A: 255}}
	data, err := xml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `<ColorValue>1 2 3</ColorValue>` {
		t.Fatalf("直接构造的颜色应沿用既有8位表示: %s", data)
	}
	data, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"R":1,"G":2,"B":3,"A":255}` || strings.Contains(string(data), "text") {
		t.Fatalf("XML 编码不应改变既有 JSON RGBA 对象: %s", data)
	}
}
