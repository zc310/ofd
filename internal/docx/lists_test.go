package docx

import (
	"testing"
)

// listMarkerOf 把测试里的整数标记还原成 marker 字符串。
func listMarkerOf(code int) string {
	if code == 2 {
		return "bullet"
	}
	return "arabic"
}

func TestDetectListItem(t *testing.T) {
	cases := []struct {
		text      string
		wantOK    bool
		wantLevel int
		wantOrder int
		wantMark  string
	}{
		{"1. 第一项", true, 0, 1, "arabic"},
		{"2. 第二项", true, 0, 2, "arabic"},
		{"1.2. 二级条目", false, 0, 0, ""}, // 多段序号是章节号或目录条目，不做列表
		{"1.2.3. 三级条目", false, 0, 0, ""},
		{"3) 第三项", true, 0, 3, "arabic"},
		{"2.第二项", true, 0, 2, "arabic"}, // 分隔符后直接是汉字
		{"一、第一条", true, 0, 1, "chinese"},
		{"十二、第十二条", true, 0, 12, "chinese"},
		{"（三）第三条", true, 0, 3, "chinese"},
		{"4.0mm", false, 0, 0, ""}, // 单位紧跟，不是序号
		{"· 项目符号", true, 0, 1, "bullet"},
		{"• 项目符号", true, 0, 1, "bullet"},

		// 以下都不该判成条目
		{"12", false, 0, 0, ""},
		{"- 12 -", false, 0, 0, ""},
		{"普通段落文字", false, 0, 0, ""},
		{"2024 年的报告", false, 0, 0, ""}, // 行首数字后没有序号分隔符
		// 序号从 0 起的「0.1mm」行级即可排除；从 1 起的「4.0mm」行级排除不掉，
		// 靠序列规则拦下整段（见 TestListSequenceIsPlausible）。
		{"0.1mm", false, 0, 0, ""},
		{"", false, 0, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			item, ok := DetectListItem(tc.text)
			if ok != tc.wantOK {
				t.Fatalf("判定为条目 = %v，期望 %v（得到 %+v）", ok, tc.wantOK, item)
			}
			if !ok {
				return
			}
			if item.level != tc.wantLevel || item.order != tc.wantOrder || item.marker != tc.wantMark {
				t.Errorf("得到 %+v，期望 level=%d order=%d marker=%s",
					item, tc.wantLevel, tc.wantOrder, tc.wantMark)
			}
		})
	}
}

func TestFilterCredibleListItems(t *testing.T) {
	// 构造按行号对齐的切片，中间可用 gap() 插入非条目行来切断连续段。
	items := func(parts ...any) []ListItem {
		var out []ListItem
		for _, part := range parts {
			switch value := part.(type) {
			case [2]int:
				out = append(out, ListItem{level: 0, order: value[1], marker: listMarkerOf(value[0])})
			case int:
				for i := 0; i < value; i++ {
					out = append(out, ListItem{})
				}
			}
		}
		return out
	}
	arabic, bullet := 1, 2
	const gap = 1
	cases := []struct {
		name  string
		items []ListItem
		want  bool
	}{
		{"连续递增", items([2]int{arabic, 1}, [2]int{arabic, 2}, [2]int{arabic, 3}), true},
		// 同一页里的两个独立列表：中间隔着非条目行，各自成段校验。
		{"两个独立列表", items([2]int{arabic, 1}, [2]int{arabic, 2},
			gap, [2]int{arabic, 1}, [2]int{arabic, 2}), true},
		{"跳号", items([2]int{arabic, 1}, [2]int{arabic, 3}), false},
		// 回到 1 按「重新编号」处理：同一页里「一、二、」之后接「1. 2. 3.」
		// 是常见写法。
		{"回到 1 视为重新编号", items([2]int{arabic, 2}, [2]int{arabic, 1}, [2]int{arabic, 2}), true},
		// 倒序的末行自成新段且不可信，被丢掉，前面的 1.2.3. 仍是可信列表。
		{"真倒序", items([2]int{arabic, 1}, [2]int{arabic, 2}, [2]int{arabic, 3}, [2]int{arabic, 2}), true},
		{"只有一条编号", items([2]int{arabic, 1}), false},
		{"孤立的项目符号", items([2]int{bullet, 1}), true},
		// 跨页延续的列表（每页只有一条）会漏判，这是有意的取舍。
		{"每段只有一条编号", items([2]int{arabic, 1}, gap, [2]int{arabic, 2}), false},
		{"空", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FilterCredibleListItems(tc.items)
			kept := 0
			for _, item := range got {
				if item.marker != "" {
					kept++
				}
			}
			if (kept > 0) != tc.want {
				t.Errorf("保留条目 = %d，期望可信 = %v（输入 %+v）", kept, tc.want, tc.items)
			}
		})
	}
}

func TestStripListMarker(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1. 第一项", "第一项"},
		{"1.2. 二级条目", "1.2. 二级条目"}, // 多段序号不是列表，不剥离
		{"一、第一条", "第一条"},
		{"（三）第三条", "第三条"},
		{"· 项目符号", "项目符号"},
		{"普通段落", "普通段落"},
	}
	for _, tc := range cases {
		if got := StripListMarker(tc.in); got != tc.want {
			t.Errorf("StripListMarker(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

func TestParseChineseNumeral(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"一", 1}, {"三", 3}, {"十", 10}, {"十二", 12}, {"二十", 20}, {"二十一", 21},
		{"百", 100}, {"", 0}, {"零", 0},
	}
	for _, tc := range cases {
		if got := parseChineseNumeral(tc.in); got != tc.want {
			t.Errorf("parseChineseNumeral(%q) = %d，期望 %d", tc.in, got, tc.want)
		}
	}
}
