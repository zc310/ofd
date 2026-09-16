package utils

import "testing"

func TestReportFormatFromOutput(t *testing.T) {
	for _, test := range []struct {
		output string
		format string
		ok     bool
	}{
		{output: "report.txt", format: "text", ok: true},
		{output: "report.MD", format: "markdown", ok: true},
		{output: "report.markdown", format: "markdown", ok: true},
		{output: "report.json", format: "json", ok: true},
		{output: "report.PDF", format: "pdf", ok: true},
		{output: "report.xlsx", format: "xlsx", ok: true},
		{output: "report.csv", ok: false},
		{output: "-", ok: false},
	} {
		t.Run(test.output, func(t *testing.T) {
			format, ok := ReportFormatFromOutput(test.output)
			if format != test.format || ok != test.ok {
				t.Fatalf("ReportFormatFromOutput(%q) = %q, %t; want %q, %t", test.output, format, ok, test.format, test.ok)
			}
		})
	}
}
