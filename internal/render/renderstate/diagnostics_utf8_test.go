package renderstate

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDiagnosticMessageTruncationPreservesUTF8Boundary(t *testing.T) {
	for _, prefix := range []int{509, 510, 511, 512} {
		var budget Budget
		budget.Report = &ReportBuilder{}
		budget.Diagnose("test", strings.Repeat("a", prefix)+"中文错误", 7)
		message := budget.Report.Diagnostics[0].Message
		if !utf8.ValidString(message) || len(message) > 512 {
			t.Fatalf("截断破坏 UTF-8 或超过字节限额：%q", message)
		}
	}
}
