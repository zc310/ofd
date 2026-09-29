package ocr

import (
	"image"
	"strings"
	"testing"
)

func TestParseTSVAggregatesAndOrdersLines(t *testing.T) {
	input := strings.Join([]string{
		"level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext",
		"5\t1\t1\t1\t2\t1\t20\t40\t20\t10\t80\tworld",
		"5\t1\t1\t1\t2\t2\t45\t40\t20\t10\t90\t2026",
		"5\t1\t1\t1\t1\t1\t20\t10\t20\t10\t95\t你好",
	}, "\n")
	blocks, err := ParseTSV(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 || blocks[0].Text != "你好" || blocks[1].Text != "world 2026" {
		t.Fatalf("blocks = %#v", blocks)
	}
	if blocks[1].Bounds != image.Rect(20, 40, 65, 50) {
		t.Fatalf("bounds = %v", blocks[1].Bounds)
	}
}

func TestNormalizeClipsAndSorts(t *testing.T) {
	blocks := Normalize([]TextBlock{
		{Text: " second ", Bounds: image.Rect(50, 10, 80, 20), Confidence: 120},
		{Text: "first\nline", Bounds: image.Rect(-5, 8, 30, 20), Confidence: -1},
		{Text: "outside", Bounds: image.Rect(100, 100, 110, 110), Confidence: 50},
	}, image.Rect(0, 0, 100, 100))
	if len(blocks) != 2 || blocks[0].Text != "first line" || blocks[1].Text != "second" {
		t.Fatalf("blocks = %#v", blocks)
	}
	if blocks[0].Confidence != 0 || blocks[1].Confidence != 100 {
		t.Fatalf("confidence = %#v", blocks)
	}
}

func TestJoinWordsKeepsCJKTogether(t *testing.T) {
	if got := JoinWords([]string{"你好", "世界", "invoice", "2026"}); got != "你好世界 invoice 2026" {
		t.Fatalf("JoinWords = %q", got)
	}
}
