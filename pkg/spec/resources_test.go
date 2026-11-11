package spec_test

import (
	"encoding/json"
	"encoding/xml"
	"reflect"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestColorSpacesDecodePreservesPaletteIndicesAndLexicalComponents(t *testing.T) {
	input := `<ColorSpaces><ColorSpace ID="3" Type="RGB" BitsPerComponent="16" Profile="../Profiles/./rgb.icc"><Palette><CV>65535 0 0</CV><CV>0 65535 0</CV><CV>0 0 65535</CV></Palette></ColorSpace><ColorSpace ID="2" Type="GRAY"/><ColorSpace ID="1" Type="CMYK" BitsPerComponent="4"><Palette><CV>15 0 0 0</CV></Palette></ColorSpace></ColorSpaces>`
	var decoded spec.ColorSpaces
	if err := xml.Unmarshal([]byte(input), &decoded); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"65535", "0", "0"}, {"0", "65535", "0"}, {"0", "0", "65535"}}
	if !reflect.DeepEqual(decoded.ColorSpace[0].Palette.CV, want) {
		t.Fatalf("每个 CV 元素应解码为独立的调色板索引: %+v", decoded.ColorSpace[0].Palette)
	}
	if decoded.ColorSpace[0].ID != 3 || decoded.ColorSpace[0].Profile != "../Profiles/./rgb.icc" || decoded.ColorSpace[1].Palette != nil || decoded.ColorSpace[1].BitsPerComponent != 0 {
		t.Fatal("颜色空间解码不应重排资源、规范化路径或补入缺省值")
	}
}

func TestPaletteDecodePreservesEmptyRowsAndCompatibleLexicalComponents(t *testing.T) {
	// 表27允许 # 十六进制；空行、负值等沿用既有解码兼容行为，不转换为8位通道。
	input := "<Palette><CV> #F &#x30;\t0 </CV><CV><![CDATA[0 15 0]]></CV><CV/><CV>65535 -1 65536</CV></Palette>"
	var palette spec.Palette
	if err := xml.Unmarshal([]byte(input), &palette); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"#F", "0", "0"}, {"0", "15", "0"}, {}, {"65535", "-1", "65536"}}
	if !reflect.DeepEqual(palette.CV, want) {
		t.Fatalf("调色板词法分量改变: %v", palette.CV)
	}
	if err := xml.Unmarshal([]byte(`<Palette/>`), &palette); err != nil {
		t.Fatal(err)
	}
	if len(palette.CV) != 0 {
		t.Fatal("重复解码不应保留前一个调色板的颜色")
	}
}

func TestPaletteKeepsJSONRowsAsArrays(t *testing.T) {
	palette := spec.Palette{CV: [][]string{{"1", "2", "3"}, {"4", "5", "6"}}}
	data, err := json.Marshal(palette) //nolint:musttag // 验证既有默认 JSON 字段及二维数组表示，不增加 JSON 标签。
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"CV":[["1","2","3"],["4","5","6"]]}` {
		t.Fatalf("调色板的 JSON 结构应保持二维数组: %s", data)
	}
}

func TestFontsRoundTripPreservesDeclarationsWithoutLoadingOrMatching(t *testing.T) {
	input := `<Fonts><Font ID="2" FontName="嵌入字体" FamilyName="测试族" Charset="prc" Italic="true" Bold="true" Serif="true" FixedWidth="true"><FontFile>../Fonts/./Subset.otf</FontFile></Font><Font ID="1" FontName="逻辑字体"/></Fonts>`
	var original spec.Fonts
	if err := xml.Unmarshal([]byte(input), &original); err != nil {
		t.Fatal(err)
	}
	data, err := xml.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded spec.Fonts
	if err := xml.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, original) || decoded.Font[0].ID != 2 || decoded.Font[0].FontFile != "../Fonts/./Subset.otf" {
		t.Fatalf("字体往返改变了声明、资源顺序或词法路径: %s", data)
	}
	embedded := decoded.Font[0]
	if embedded.FamilyName != "测试族" || embedded.Charset != "prc" || !embedded.Italic || !embedded.Bold || !embedded.Serif || !embedded.FixedWidth {
		t.Fatalf("内嵌字体的匹配属性或字型标志丢失: %+v", embedded)
	}
	logical := decoded.Font[1]
	if logical.FontFile != "" || logical.Charset != "" || logical.Italic || logical.Bold || logical.Serif || logical.FixedWidth {
		t.Fatalf("逻辑字体不应自动加载同名字体或补入字符集: %+v", logical)
	}
}

func TestFontFalseFlagsUseTable44DefaultsAndOmitOnEncode(t *testing.T) {
	var font spec.Font
	if err := xml.Unmarshal([]byte(`<Font ID="1" FontName="Logical" Italic="false" Bold="false" Serif="false" FixedWidth="false"/>`), &font); err != nil {
		t.Fatal(err)
	}
	data, err := xml.Marshal(font)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `<Font ID="1" FontName="Logical"></Font>` {
		t.Fatalf("既有 bool 字段应保留 false 与缺省值等价的编码约定: %s", data)
	}
}

func TestMultiMediasRoundTripPreservesTypeFormatOrderAndLexicalPaths(t *testing.T) {
	input := `<MultiMedias><MultiMedia ID="3" Type="Video" Format="AVS"><MediaFile>../Media/./video.avs</MediaFile></MultiMedia><MultiMedia ID="2" Type="Image"><MediaFile>Images/../raw.bin</MediaFile></MultiMedia><MultiMedia ID="1" Type="Audio" Format="mp3"><MediaFile>./sound.mp3</MediaFile></MultiMedia></MultiMedias>`
	var original spec.MultiMedias
	if err := xml.Unmarshal([]byte(input), &original); err != nil {
		t.Fatal(err)
	}
	data, err := xml.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded spec.MultiMedias
	if err := xml.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, original) || decoded.MultiMedia[0].ID != 3 || decoded.MultiMedia[0].MediaFile != "../Media/./video.avs" || decoded.MultiMedia[1].Format != "" {
		t.Fatalf("多媒体模型不应探测格式、规范化路径或重排资源: %s", data)
	}
}
