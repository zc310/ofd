package pdf2ofd

import "testing"

func TestPDFSoftMaskCoordinateMappingAvoids32BitOverflow(t *testing.T) {
	// 单图预算允许的长条图在 32 位平台上也应正确对齐；乘积可能超过 int32。
	for _, tc := range []struct{ coordinate, source, target, want int }{
		{75000, 100000, 150000, 50000},
		{149999, 100000, 150000, 99999},
		{150000, 100000, 150000, 99999},
	} {
		if got := pdfSoftMaskColumn(tc.coordinate, tc.source, tc.target); got != tc.want {
			t.Fatalf("column mapping=%d want %d", got, tc.want)
		}
		if got := pdfSoftMaskRow(tc.coordinate, tc.source, tc.target); got != tc.want {
			t.Fatalf("row mapping=%d want %d", got, tc.want)
		}
	}
}
