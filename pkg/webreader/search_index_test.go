package webreader

import (
	"reflect"
	"testing"
)

// TestBuildSearchIndexRegistersRunOnce 同一字符在同一 run 里只登记一次。
//
// 索引的用途是"先按首字筛出候选 run，再逐个精确匹配"，byRune 里同一个 run
// 出现两次等于把那次扫描做两遍——结果仍正确，只是白费一倍。
//
// 这条守住 searchAt 里那个"代次标记"的写法：值取 runIndex+1，靠与当前代次
// 相等来判断本 run 已登记，因此不逐次 clear map。若有人改成每 run 新建 map 或
// 忘了换代表达方式，重复登记就会回来。
func TestBuildSearchIndexRegistersRunOnce(t *testing.T) {
	runs := []TextRun{
		{Text: "abcabc"},
		{Text: "abc"},
		{Text: "xyz"},
	}
	indexed := buildSearchIndex(runs)

	for _, tc := range []struct {
		r    rune
		want []int
	}{
		{'a', []int{0, 1}}, // 两个 run 各登记一次，run 0 内不重复
		{'b', []int{0, 1}},
		{'c', []int{0, 1}},
		{'x', []int{2}},
		{'y', []int{2}},
		{'z', []int{2}},
	} {
		if got := indexed.byRune[tc.r]; !reflect.DeepEqual(got, tc.want) {
			t.Errorf("byRune[%q] = %v，期望 %v", tc.r, got, tc.want)
		}
	}

	// 索引只是旁路：每个 run 的字符序列必须原样保留，搜索要靠它定位。
	wantRuns := [][]rune{[]rune("abcabc"), []rune("abc"), []rune("xyz")}
	for i, want := range wantRuns {
		if !reflect.DeepEqual(indexed.runs[i], want) {
			t.Errorf("runs[%d] = %q，期望 %q", i, indexed.runs[i], want)
		}
	}
}

// TestBuildSearchIndexLowercases 索引按小写建，查询也按小写比。
//
// 中文没有大小写，但 PDF 里的拉丁文有：索引若不折叠大小写，"Hello" 的 'H'
// 与 "hello" 的 'h' 会各占一个 key，而 Search 只对 needle 做 ToLower，于是
// 搜 "hello" 永远匹配不上 "Hello"。
func TestBuildSearchIndexLowercases(t *testing.T) {
	indexed := buildSearchIndex([]TextRun{{Text: "HeLLo"}})

	for _, want := range []rune{'h', 'e', 'l', 'o'} {
		if got := indexed.byRune[want]; !reflect.DeepEqual(got, []int{0}) {
			t.Errorf("byRune[%q] = %v，期望 [0]", want, got)
		}
	}
	if _, ok := indexed.byRune['H']; ok {
		t.Error("索引里仍留着大写 'H'，查询按小写走会匹配不上")
	}
	if got := indexed.runs[0]; !reflect.DeepEqual(got, []rune("hello")) {
		t.Errorf("runs[0] = %q，期望 %q", got, "hello")
	}
}

// TestBuildSearchIndexEmptyRuns 空输入不应 panic，也不该留下可命中的 key。
func TestBuildSearchIndexEmptyRuns(t *testing.T) {
	indexed := buildSearchIndex(nil)
	if len(indexed.runs) != 0 {
		t.Errorf("runs 长度 = %d，期望 0", len(indexed.runs))
	}
	if len(indexed.byRune) != 0 {
		t.Errorf("byRune 有 %d 个 key，期望 0", len(indexed.byRune))
	}
}

// TestBuildSearchIndexMatchesSearch 索引筛出的候选必须包含真实命中。
//
// 这是索引与 Search 之间的一致性：若索引漏登记某个首字，用例看不出问题——
// 搜索会安静地少报结果。把这里与 Search 的实际结果对照，漏登记才会暴露。
func TestBuildSearchIndexMatchesSearch(t *testing.T) {
	runs := []TextRun{
		{Text: "The quick brown fox"},
		{Text: "中文段落一"},
		{Text: "another Quick one"},
		{Text: "no match here"},
	}
	indexed := buildSearchIndex(runs)

	// needle 走 Search 里同样的处理
	needle := []rune("quick")
	first := needle[0]
	candidates := indexed.byRune[first]
	if len(candidates) == 0 {
		t.Fatalf("首字 %q 在索引里没有候选，Search 将永远搜不到", first)
	}
	// 每个候选 run 都应确实含有该 needle
	for _, runIndex := range candidates {
		text := indexed.runs[runIndex]
		found := false
		for i := 0; i+len(needle) <= len(text); i++ {
			match := true
			for j := range needle {
				if text[i+j] != needle[j] {
					match = false
					break
				}
			}
			if match {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("run %d 被索引为候选，但实际不含 %q", runIndex, "quick")
		}
	}
	if !reflect.DeepEqual(candidates, []int{0, 2}) {
		t.Errorf("候选 = %v，期望 [0 2]（第 0 段小写、第 2 段大写开头）", candidates)
	}
}

func TestIndexRunesWithPrebuiltTable(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		query string
		want  int
	}{
		{name: "KMP fallback", text: "abababc", query: "ababd", want: -1},
		{name: "match after fallback", text: "abcabcabcd", query: "abcabcd", want: 3},
		{name: "unicode", text: "文档阅读器", query: "阅读", want: 2},
		{name: "query longer than text", text: "短", query: "很长的查询", want: -1},
		{name: "empty query", text: "text", query: "", want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			text, query := []rune(test.text), []rune(test.query)
			got := indexRunesWithTable(text, query, buildRuneSearchTable(query))
			if got != test.want {
				t.Fatalf("indexRunesWithTable(%q, %q) = %d, want %d", test.text, test.query, got, test.want)
			}
		})
	}
}
