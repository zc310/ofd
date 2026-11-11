package contract

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/zc310/ofd/pkg/reader"
)

func TestTextRunsContractPreservesLayoutAndEmptyGlyphs(t *testing.T) {
	runs := []reader.TextRun{{Source: "template", Text: "中", X: 1, Y: 2, Width: 3, Height: 4, Scope: 2,
		TemplateID: 7, Font: 9, Size: 5, Weight: 700, ReadDirection: 90, CharDirection: 270,
		FontFamily: "字体", Bold: true, Italic: true,
		Glyphs: []reader.Glyph{{Text: "中", X: 1, Y: 2, Width: 3, Height: 4, Angle: 90}}}, {Text: "空"}}
	data, err := json.Marshal(TextRuns(runs))
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"bold":true,"charDirection":270,"font":9,"fontFamily":"字体","glyphs":[{"angle":90,"height":4,"text":"中","width":3,"x":1,"y":2}],"height":4,"italic":true,"readDirection":90,"scope":2,"size":5,"template_id":7,"text":"中","weight":700,"width":3,"x":1,"y":2},{"bold":false,"charDirection":0,"font":0,"fontFamily":"","glyphs":[],"height":0,"italic":false,"readDirection":0,"scope":0,"size":0,"template_id":0,"text":"空","weight":0,"width":0,"x":0,"y":0}]`
	var actual, expected any
	if err := json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("文字契约 = %s", data)
	}
	empty, err := json.Marshal(TextRuns(nil))
	if err != nil || string(empty) != `[]` {
		t.Fatalf("空文字 = %s, %v", empty, err)
	}
	if runs[1].Glyphs != nil {
		t.Fatal("不能修改原文字对象的字符列表")
	}
}
