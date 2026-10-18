package compare

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func TestPageRangesMergeWithoutExpandingHugeEndpoints(t *testing.T) {
	got, err := ParsePageRanges("5, 1-3,2-4,8-9")
	if err != nil || !reflect.DeepEqual(got, []PageRange{{1, 5}, {8, 9}}) {
		t.Fatalf("%v %v", got, err)
	}
	maxInt := int(^uint(0) >> 1)
	got, err = ParsePageRanges(fmt.Sprintf("1-%d,%d", maxInt, maxInt))
	if err != nil || len(got) != 1 || got[0].Last != maxInt {
		t.Fatalf("%v %v", got, err)
	}
	for _, spec := range []string{"0", "-1", "3-1", "1,", "1,,2", "1-", "1-2-3", "x", "9999999999999999999999"} {
		if _, err := ParsePageRanges(spec); err == nil {
			t.Errorf("未拒绝 %q", spec)
		}
	}
}

func TestPageSelectionKeepsOriginalNumbersAndSkipsRendering(t *testing.T) {
	a, b := testOFD(t, "one", "two", "three"), testOFD(t, "changed one", "two", "three")
	r, err := CompareFiles(t.Context(), []string{a, b}, Options{Pages: "2-3", Visual: true, MaxTotalPixels: 21000})
	if err != nil {
		t.Fatal(err)
	}
	c := r.Comparisons[0]
	if len(c.Pages) != 2 || c.Pages[0].Before != 2 || c.Pages[0].After != 2 || c.Pages[1].Before != 3 || !c.PagesEqual {
		t.Fatalf("选页结果错误: %+v", c)
	}
	if r.Inputs[0].Pages[0].Selected || r.Inputs[0].Pages[0].Text != "" || r.Inputs[0].Pages[0].ContentSHA256 != "" {
		t.Fatal("范围之外的页面不应提取内容")
	}
	if c.Equal || len(c.Files) == 0 {
		t.Fatal("选页不应隐藏包内文件变化")
	}
	for _, format := range []string{"text", "markdown", "json", "html"} {
		if err := WriteReport(&bytes.Buffer{}, r, format); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := CompareFiles(t.Context(), []string{a, b}, Options{Pages: "99"}); err == nil {
		t.Fatal("空选择未报错")
	}
}

func TestSelectedAutoPairingUsesOriginalPageNumbers(t *testing.T) {
	a, b := testOFD(t, "one", "two", "three"), testOFD(t, "one", "insert", "two", "three")
	r, err := CompareFiles(t.Context(), []string{a, b}, Options{Pages: "2-4"})
	if err != nil {
		t.Fatal(err)
	}
	p := r.Comparisons[0].Pages
	if len(p) != 3 || p[0].Before != 2 || p[0].After != 3 || p[1].Before != 3 || p[1].After != 4 || p[2].Before != 0 || p[2].After != 2 {
		t.Fatalf("配对错误: %+v", p)
	}
}

func TestUnselectedPageIsNotParsedAsPageContent(t *testing.T) {
	a := testOFD(t, "one", "two")
	b := rewrite(t, a, func(n string, data []byte) (string, []byte) {
		if strings.Contains(n, "Page_0/") && strings.HasSuffix(n, "Content.xml") {
			return n, []byte("<broken>")
		}
		return n, data
	})
	r, err := CompareFiles(t.Context(), []string{a, b}, Options{Pages: "2", Visual: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Comparisons[0].Pages) != 1 || r.Comparisons[0].Pages[0].Before != 2 {
		t.Fatal("没有按范围跳过页内容加载")
	}
}

func reconstructText(spans []TextSpan) (string, string) {
	var before, after strings.Builder
	for _, span := range spans {
		if span.Kind != "insert" {
			before.WriteString(span.Text)
		}
		if span.Kind != "delete" {
			after.WriteString(span.Text)
		}
	}
	return before.String(), after.String()
}

func TestTextDifferencesFindSeparatedEditsAndPreserveUnicode(t *testing.T) {
	a := "合同金额：100元\n交付日期：10月20日\n收款账户：甲账户"
	b := "合同金额：120元\n交付日期：10月25日\n收款账户：乙账户"
	spans, coarse, err := textDifferences(t.Context(), a, b)
	if err != nil || coarse {
		t.Fatalf("%v %v", coarse, err)
	}
	x, y := reconstructText(spans)
	if x != a || y != b {
		t.Fatalf("重构失败 %q %q", x, y)
	}
	deletions := 0
	for _, span := range spans {
		if span.Kind == "delete" {
			deletions++
		}
	}
	if deletions != 3 {
		t.Fatalf("预期 3 处分开的文字修改: %+v", spans)
	}
}

func TestTextDiffReconstructsRandomEdits(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	alphabet := []rune("ab中😀 \n")
	for n := 0; n < 500; n++ {
		makeText := func() string {
			r := make([]rune, rng.Intn(40))
			for i := range r {
				r[i] = alphabet[rng.Intn(len(alphabet))]
			}
			return string(r)
		}
		a, b := makeText(), makeText()
		spans, coarse, err := textDifferences(t.Context(), a, b)
		if err != nil || coarse {
			t.Fatalf("%v %v", coarse, err)
		}
		x, y := reconstructText(spans)
		if a == b && len(spans) == 0 {
			continue
		}
		if x != a || y != b {
			t.Fatalf("%q → %q: %+v 重构为 %q → %q", a, b, spans, x, y)
		}
	}
}

func TestLongReplacementFallsBackExplicitlyAndHonorsCancellation(t *testing.T) {
	a, b := "prefix"+strings.Repeat("甲", 2000)+"suffix", "prefix"+strings.Repeat("乙", 2000)+"suffix"
	spans, coarse, err := textDifferences(t.Context(), a, b)
	if err != nil || !coarse {
		t.Fatalf("%v %v", coarse, err)
	}
	x, y := reconstructText(spans)
	if x != a || y != b {
		t.Fatal("粗粒度回退丢失原文")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := textDifferences(ctx, a, b); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
