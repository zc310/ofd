package spec_test

import (
	"encoding/json"
	"encoding/xml"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestPathDataXMLRoundTripPreservesAllCommandsAndFullPrecision(t *testing.T) {
	input := "M 0.12345678901234567 -0.000000000123456789 L 1 2 Q 3 4 5 6 B 7 8 9 10 11 12 A 1.2345678901234567 2.345678901234567 30.123456789012345 1 0 13 14 C"
	original, err := spec.ParsePathData(input)
	if err != nil {
		t.Fatal(err)
	}
	var holder struct {
		XMLName xml.Name      `xml:"Path"`
		Data    spec.PathData `xml:"AbbreviatedData"`
	}
	holder.Data = original
	data, err := xml.Marshal(holder)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "M 0.12345678901234566") || strings.Contains(string(data), "<PathCommand>") {
		t.Fatalf("路径应编码为精确保留 float64 的文本，而不是命令结构体: %s", data)
	}
	if err := xml.Unmarshal(data, &holder); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(holder.Data, original) {
		t.Fatalf("路径 XML 往返改变了坐标或命令: %s", data)
	}
	if original.String() == input {
		t.Fatal("诊断 String 仍应沿用两位小数格式，与 XML 编码分离")
	}
}

func TestPathDataXMLEncodingRejectsIncompleteCommandsAndNonFiniteParameters(t *testing.T) {
	for _, command := range []spec.PathCommand{
		{Type: "Z"}, {Type: spec.MoveTo},
		{Type: spec.LineTo, Points: []spec.StPos{{}, {}}},
		{Type: spec.QuadTo, Points: []spec.StPos{{}}},
		{Type: spec.CubicBezier, Points: []spec.StPos{{}, {}}},
		{Type: spec.Close, Points: []spec.StPos{{}}},
		{Type: spec.MoveTo, Points: []spec.StPos{{}}, Arc: &spec.ArcData{}},
		{Type: spec.ArcTo},
		{Type: spec.ArcTo, Points: []spec.StPos{{}}, Arc: &spec.ArcData{}},
		{Type: spec.MoveTo, Points: []spec.StPos{{X: math.NaN()}}},
		{Type: spec.ArcTo, Arc: &spec.ArcData{RX: math.Inf(1)}},
		{Type: spec.ArcTo, Arc: &spec.ArcData{EndPoint: spec.StPos{Y: math.Inf(-1)}}},
	} {
		if _, err := xml.Marshal(spec.PathData{command}); err == nil {
			t.Fatalf("不完整或非有限路径参数应返回错误而非崩溃或输出无效 XML: %#v", command)
		}
	}
}

func TestPathDataXMLCodecKeepsJSONCommandArray(t *testing.T) {
	path, err := spec.ParsePathData("M 1 2 L 3 4 C")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), `[{"Type":"M","Points":[{"X":1,"Y":2}]`) {
		t.Fatalf("XML 编码不应将 JSON 命令数组改为字符串: %s", data)
	}
}

func TestEmptyPathDataAndRepeatedXMLDecode(t *testing.T) {
	for _, original := range []spec.PathData{nil, {}} {
		data, err := xml.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != `<PathData></PathData>` {
			t.Fatalf("空路径 XML 编码错误: %s", data)
		}
	}
	path, err := spec.ParsePathData("M 1 2 L 3 4")
	if err != nil {
		t.Fatal(err)
	}
	if err := xml.Unmarshal([]byte(`<AbbreviatedData/>`), &path); err != nil {
		t.Fatal(err)
	}
	if len(path) != 0 {
		t.Fatal("重复解码空路径应清除旧命令")
	}
}
