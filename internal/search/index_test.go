package search

import (
	"reflect"
	"testing"
)

func TestIndexRegistersEachRuneOncePerRun(t *testing.T) {
	index := NewBuilder(3)
	for _, text := range []string{"abcabc", "abc", "xyz"} {
		index.Add(text)
	}
	completed := index.Build()
	for _, test := range []struct {
		rune rune
		want []int
	}{{'a', []int{0, 1}}, {'b', []int{0, 1}}, {'c', []int{0, 1}}, {'x', []int{2}}, {'y', []int{2}}, {'z', []int{2}}} {
		if got := completed.Candidates(test.rune); !reflect.DeepEqual(got, test.want) {
			t.Errorf("Candidates(%q) = %v, want %v", test.rune, got, test.want)
		}
	}
	if got := completed.Run(0); !reflect.DeepEqual(got, []rune("abcabc")) {
		t.Errorf("Run(0) = %q, want abcabc", string(got))
	}
}

func TestIndexLowercases(t *testing.T) {
	index := NewBuilder(1)
	index.Add("HeLLo")
	completed := index.Build()
	for _, value := range []rune{'h', 'e', 'l', 'o'} {
		if got := completed.Candidates(value); !reflect.DeepEqual(got, []int{0}) {
			t.Errorf("Candidates(%q) = %v, want [0]", value, got)
		}
	}
	if got := string(completed.Run(0)); got != "hello" {
		t.Errorf("Run(0) = %q, want hello", got)
	}
}

func TestIndexEmpty(t *testing.T) {
	index := NewBuilder(0).Build()
	if index.Len() != 0 || len(index.byRune) != 0 {
		t.Fatalf("empty index has %d runs and %d rune keys", index.Len(), len(index.byRune))
	}
}

func TestRuneSearch(t *testing.T) {
	tests := []struct {
		text, query string
		want        int
	}{
		{"abababc", "ababd", -1},
		{"abcabcabcd", "abcabcd", 3},
		{"文档阅读器", "阅读", 2},
		{"短", "很长的查询", -1},
		{"text", "", 0},
	}
	for _, test := range tests {
		t.Run(test.text+"/"+test.query, func(t *testing.T) {
			text, query := []rune(test.text), []rune(test.query)
			got := IndexOfRunes(text, query, RuneSearchTable(query))
			if got != test.want {
				t.Fatalf("IndexOfRunes(%q, %q) = %d, want %d", test.text, test.query, got, test.want)
			}
		})
	}
}
