package contract

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/zc310/ofd/pkg/reader"
)

func TestSearchContractPreservesMatchesAndEmptyArrays(t *testing.T) {
	report := reader.SearchReport{Scanned: 4, Truncated: true, Results: []reader.SearchResult{
		{Page: 2, Run: 3, Text: "中文", Start: 1, End: 2, Rects: []reader.Rect{{X: 1, Y: 2, Width: 3, Height: 4, Angle: 90}}},
		{Page: 3, Text: "空"},
	}}
	data, err := json.Marshal(Search(report))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"results":[{"end":2,"page":2,"rects":[{"angle":90,"height":4,"width":3,"x":1,"y":2}],"run":3,"start":1,"text":"中文"},{"end":0,"page":3,"rects":[],"run":0,"start":0,"text":"空"}],"scanned":4,"truncated":true}`
	var actual, expected any
	if err := json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("搜索契约 = %s", data)
	}
	empty, err := json.Marshal(Search(reader.SearchReport{}))
	if err != nil || string(empty) != `{"results":[],"scanned":0,"truncated":false}` {
		t.Fatalf("空搜索结果 = %s, %v", empty, err)
	}
	if report.Results[1].Rects != nil {
		t.Fatal("不能修改原搜索结果的矩形列表")
	}
	copyReport := Search(report)
	copyReport.Results[0].Rects[0].Angle = 0
	if report.Results[0].Rects[0].Angle != 90 {
		t.Fatal("不能共享原搜索结果的矩形切片")
	}
}
