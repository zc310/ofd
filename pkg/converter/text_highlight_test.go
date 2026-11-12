package converter

import (
	"testing"

	"github.com/zc310/ofd/internal/textdoc"
)

func TestTextLayoutJoinsTouchingColoredRunsWithoutInventingSpaces(t *testing.T) {
	row := []textdoc.Entry{
		{Text: "print", X: 10, Y: 20, Width: 5, Size: 3, FontName: "Go Mono"},
		{Text: "(\"hello\"", X: 15, Y: 20, Width: 8, Size: 3, FontName: "Go Mono"},
		{Text: ", ", X: 23, Y: 20, Width: 2, Size: 3, FontName: "Go Mono"},
		{Text: "42)", X: 25, Y: 20, Width: 3, Size: 3, FontName: "Go Mono"},
	}
	if got := layoutTextRow(row, 10, 2); got != "print(\"hello\", 42)" {
		t.Fatalf("颜色边界引入了额外空格: %q", got)
	}
}

func TestTextLayoutDoesNotJoinSeparatedOrUncertainTextRuns(t *testing.T) {
	for _, change := range []string{"gap", "overlap", "font", "unknown_font", "size", "baseline"} {
		t.Run(change, func(t *testing.T) {
			left := textdoc.Entry{Text: "a", X: 10, Y: 20, Width: 2, Size: 3, FontName: "Go Mono"}
			right := left
			right.Text, right.X = "b", 12
			switch change {
			case "gap":
				right.X++
			case "overlap":
				right.X--
			case "font":
				right.FontName = "Other"
			case "unknown_font":
				left.FontName, right.FontName = "", ""
			case "size":
				right.Size++
			case "baseline":
				right.Y++
			}
			if got := layoutTextRow([]textdoc.Entry{left, right}, 10, 2); got != "a b" {
				t.Fatalf("不确定或不连续的片段被粘连: %q", got)
			}
		})
	}
}
