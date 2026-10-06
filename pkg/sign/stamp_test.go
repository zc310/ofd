package sign

import (
	"math"
	"testing"
)

func TestResolveStampSeamsSupportsTopAndBottom(t *testing.T) {
	entries := []entry{{
		name: "Doc_0/Document.xml",
		data: []byte(`<Document><CommonData><PageArea><PhysicalBox>0 0 210 140</PhysicalBox></PageArea></CommonData><Pages><Page ID="1"/><Page ID="2"/><Page ID="3"/><Page ID="4"/></Pages></Document>`),
	}}

	tests := []struct {
		name   string
		edge   string
		want   []string
		wantID []string
	}{
		{
			name:   "top",
			edge:   "top",
			want:   []string{"85 0 40 40", "85 -10 40 40", "85 -20 40 40", "85 -30 40 40"},
			wantID: []string{"1", "2", "3", "4"},
		},
		{
			name:   "bottom",
			edge:   "bottom",
			want:   []string{"85 130 40 40", "85 120 40 40", "85 110 40 40", "85 100 40 40"},
			wantID: []string{"1", "2", "3", "4"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stamps, err := resolveStampSeams(StampSeamOptions{Edge: test.edge, Size: 40, X: -1}, "Doc_0", entries)
			if err != nil {
				t.Fatal(err)
			}
			if len(stamps) != len(test.want) {
				t.Fatalf("得到 %d 个 StampAnnot，期望 %d", len(stamps), len(test.want))
			}
			for index, stamp := range stamps {
				if stamp.pageRef != test.wantID[index] || stamp.boundary != test.want[index] {
					t.Errorf("stamp[%d] = page=%q boundary=%q，期望 page=%q boundary=%q",
						index, stamp.pageRef, stamp.boundary, test.wantID[index], test.want[index])
				}
				if stamp.clip != formatBoundary(0, float64(index)*10, 40, 10) {
					t.Errorf("stamp[%d] clip = %q", index, stamp.clip)
				}
			}
		})
	}
}

func TestResolveStampSeamsAllEdges(t *testing.T) {
	entries := []entry{{
		name: "Doc_0/Document.xml",
		data: []byte(`<Document><CommonData><PageArea><PhysicalBox>0 0 210 140</PhysicalBox></PageArea></CommonData><Pages><Page ID="1"/><Page ID="2"/></Pages></Document>`),
	}}
	stamps, err := resolveStampSeams(StampSeamOptions{Edge: "all", Size: 40, X: -1, Y: -1}, "Doc_0", entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(stamps) != 8 {
		t.Fatalf("两页四边骑缝章应生成 8 个 StampAnnot，实际 %d", len(stamps))
	}
	for index, edge := range []string{"left", "right", "top", "bottom"} {
		stamp := stamps[index*2]
		if stamp.pageRef != "1" {
			t.Errorf("四边模式第 %d 个标注页面 = %q，期望 1", index, stamp.pageRef)
		}
		if stamp.clip == "" || stamp.boundary == "" {
			t.Errorf("四边模式 %s 标注缺少 Boundary 或 Clip", edge)
		}
	}
}

func TestResolveStampSeamsUsesPerPageSize(t *testing.T) {
	entries := []entry{
		{
			name: "Doc_0/Document.xml",
			data: []byte(`<Document><CommonData><PageArea><PhysicalBox>0 0 210 140</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/><Page ID="2" BaseLoc="Pages/Page_1/Content.xml"/></Pages></Document>`),
		},
		{
			name: "Doc_0/Pages/Page_0/Content.xml",
			data: []byte(`<Page><Area><PhysicalBox>0 0 200 100</PhysicalBox></Area></Page>`),
		},
		{
			name: "Doc_0/Pages/Page_1/Content.xml",
			data: []byte(`<Page><Area><PhysicalBox>0 0 300 160</PhysicalBox></Area></Page>`),
		},
	}
	stamps, err := resolveStampSeams(StampSeamOptions{Edge: "right", Size: 40, Y: -1}, "Doc_0", entries)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`PageRef="1" Boundary="180 30 40 40" Clip="0 0 20 40"`,
		`PageRef="2" Boundary="260 60 40 40" Clip="20 0 20 40"`,
	}
	for index, value := range want {
		stamp := stamps[index]
		got := `PageRef="` + stamp.pageRef + `" Boundary="` + stamp.boundary + `" Clip="` + stamp.clip + `"`
		if got != value {
			t.Errorf("stamp[%d] = %s，期望 %s", index, got, value)
		}
	}
}

func TestResolveStampSeamsUsesAbsoluteBaseLoc(t *testing.T) {
	entries := []entry{
		{
			name: "Doc_0/Document.xml",
			data: []byte(`<Document><CommonData><PageArea><PhysicalBox>0 0 210 140</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="/Doc_0/Pages/Page_0/Content.xml"/><Page ID="2" BaseLoc="/Doc_0/Pages/Page_1/Content.xml"/></Pages></Document>`),
		},
		{
			name: "Doc_0/Pages/Page_0/Content.xml",
			data: []byte(`<Page><Area><PhysicalBox>0 0 200 100</PhysicalBox></Area></Page>`),
		},
		{
			name: "Doc_0/Pages/Page_1/Content.xml",
			data: []byte(`<Page><Area><PhysicalBox>0 0 300 160</PhysicalBox></Area></Page>`),
		},
	}
	stamps, err := resolveStampSeams(StampSeamOptions{Edge: "right", Size: 40, Y: -1}, "Doc_0", entries)
	if err != nil {
		t.Fatal(err)
	}
	if stamps[0].boundary != "180 30 40 40" || stamps[1].boundary != "260 60 40 40" {
		t.Fatalf("绝对 BaseLoc 页面定位错误: page1=%q page2=%q", stamps[0].boundary, stamps[1].boundary)
	}
}

func TestResolveStampSeamsRejectsInvisibleStrips(t *testing.T) {
	entries := []entry{{
		name: "Doc_0/Document.xml",
		data: []byte(`<Document><CommonData><PageArea><PhysicalBox>0 0 210 140</PhysicalBox></PageArea></CommonData><Pages><Page ID="1"/><Page ID="2"/></Pages></Document>`),
	}}
	if _, err := resolveStampSeams(StampSeamOptions{Size: 40, MinStrip: 21}, "Doc_0", entries); err == nil {
		t.Fatal("低于最小条带宽度时应返回错误")
	}
	if _, err := resolveStampSeams(StampSeamOptions{Size: 40, MinStrip: 20}, "Doc_0", entries); err != nil {
		t.Fatalf("达到最小条带宽度时不应报错: %v", err)
	}
}

func TestResolveStampSeamsRejectsInvalidNumericOptions(t *testing.T) {
	entries := []entry{{
		name: "Doc_0/Document.xml",
		data: []byte(`<Document><CommonData><PageArea><PhysicalBox>0 0 210 140</PhysicalBox></PageArea></CommonData><Pages><Page ID="1"/><Page ID="2"/></Pages></Document>`),
	}}
	nan := math.NaN()
	inf := math.Inf(1)
	cases := []struct {
		name    string
		options StampSeamOptions
	}{
		{name: "负边长", options: StampSeamOptions{Size: -1}},
		{name: "NaN 边长", options: StampSeamOptions{Size: nan}},
		{name: "Inf 边长", options: StampSeamOptions{Size: inf}},
		{name: "负最小条带", options: StampSeamOptions{Size: 40, MinStrip: -1}},
		{name: "NaN 最小条带", options: StampSeamOptions{Size: 40, MinStrip: nan}},
		{name: "NaN X", options: StampSeamOptions{Size: 40, X: nan}},
		{name: "Inf Y", options: StampSeamOptions{Size: 40, Y: inf}},
		{name: "负拆分份数", options: StampSeamOptions{Size: 40, Pieces: -1}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := resolveStampSeams(test.options, "Doc_0", entries); err == nil {
				t.Fatal("非法数值参数应返回错误")
			}
		})
	}
}

func TestResolveStampSeamsGroupsPages(t *testing.T) {
	entries := []entry{{
		name: "Doc_0/Document.xml",
		data: []byte(`<Document><CommonData><PageArea><PhysicalBox>0 0 210 140</PhysicalBox></PageArea></CommonData><Pages><Page ID="1"/><Page ID="2"/><Page ID="3"/><Page ID="4"/><Page ID="5"/></Pages></Document>`),
	}}
	stamps, err := resolveStampSeams(StampSeamOptions{Edge: "right", GroupPages: 2, Size: 40, MinStrip: 20}, "Doc_0", entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(stamps) != 5 {
		t.Fatalf("5 页按 2 页分组应生成 5 个 StampAnnot，实际 %d", len(stamps))
	}
	wantClips := []string{"0 0 20 40", "20 0 20 40", "0 0 20 40", "20 0 20 40", "0 0 40 40"}
	for index, want := range wantClips {
		if stamps[index].clip != want {
			t.Errorf("stamp[%d] clip = %q，期望 %q", index, stamps[index].clip, want)
		}
	}
}

// TestResolveStampSeamsSinglePageGroupDrawsWholeStamp 锁定分组余数为 1 时的
// 语义：单页无法自成一枚骑缝章，末组那一页改画完整印章（裁片等于整章），
// 而不是报错或画半枚。分组页数不整除总页数时这会经常出现（如 5 页按 2 页
// 分组得到 2+2+1）。
func TestResolveStampSeamsSinglePageGroupDrawsWholeStamp(t *testing.T) {
	entries := []entry{{
		name: "Doc_0/Document.xml",
		data: []byte(`<Document><CommonData><PageArea><PhysicalBox>0 0 210 140</PhysicalBox></PageArea></CommonData><Pages><Page ID="1"/><Page ID="2"/><Page ID="3"/><Page ID="4"/><Page ID="5"/></Pages></Document>`),
	}}
	stamps, err := resolveStampSeams(StampSeamOptions{Edge: "right", GroupPages: 2, Size: 40, Y: -1}, "Doc_0", entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(stamps) != 5 {
		t.Fatalf("应生成 5 个 StampAnnot，实际 %d", len(stamps))
	}
	last := stamps[4]
	if last.pageRef != "5" {
		t.Fatalf("末组页面 = %q，期望 5", last.pageRef)
	}
	if last.clip != "0 0 40 40" {
		t.Errorf("末组单页裁片 = %q，期望整章 0 0 40 40", last.clip)
	}
	if last.boundary != "170 50 40 40" {
		t.Errorf("末组单页印章框 = %q，期望 170 50 40 40（右缘居中完整章）", last.boundary)
	}
}
