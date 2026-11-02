package render

import "testing"

func TestTextQuerySharesSelectionAndReportsErrorsEvenForEmptySelection(t *testing.T) {
	doc, page := privateTemplateDocument(t)
	for _, test := range []struct {
		runs []int
		want int
	}{{nil, 3}, {[]int{}, 0}, {[]int{-1, 1, 1, 99}, 1}} {
		options := TextQueryOptions{RunIndices: test.runs}
		layouts, err := doc.QueryTextLayouts(page, options)
		if err != nil {
			t.Fatal(err)
		}
		values, err := doc.QueryTextValues(page, options)
		if err != nil || len(layouts) != test.want || len(values) != test.want {
			t.Fatalf("布局和文字查询选择不一致：%d/%d %v", len(layouts), len(values), err)
		}
	}
	options := TextQueryOptions{RunIndices: []int{}}
	if _, err := doc.QueryTextLayouts(nil, options); err == nil {
		t.Fatal("空选择掩盖了无效页面错误")
	}
	if _, err := doc.QueryTextValues(nil, options); err == nil {
		t.Fatal("空选择掩盖了无效页面错误")
	}
}
