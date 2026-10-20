package pdf2ofd

import (
	"fmt"
	"reflect"
	"testing"
)

func TestContentTokenizerRecognizesPDFTokenBoundaries(t *testing.T) {
	// ISO 32000-1, 7.2.2: comments delimit tokens, and NUL/FF are whitespace.
	for _, separator := range []string{" ", "\t", "\r", "\n", "\f", "\x00", "%comment\r\n"} {
		t.Run(fmt.Sprintf("%q", separator), func(t *testing.T) {
			tokens := newPDFContentTokenizer([]byte("/F#31" + separator + "12" + separator + "Tf" + separator + "<41" + separator[:1] + "42>"))
			// Comments are not allowed inside hex strings.
			if separator[0] == '%' {
				tokens = newPDFContentTokenizer([]byte("/F#31" + separator + "12" + separator + "Tf" + separator + "<4142>"))
			}
			for _, want := range []any{pdfName("F1"), float64(12), "Tf", pdfString("AB")} {
				got, ok, err := tokens.next()
				if err != nil || !ok || !reflect.DeepEqual(got, want) {
					t.Fatalf("token = %#v, %v, %v; want %#v", got, ok, err, want)
				}
			}
			page := parseContentPage(t, "10 10 100 100 re f"+separator)
			if got := len(layerPaths(page.Content().Layer[0])); got != 1 {
				t.Fatalf("painted paths = %d, want 1", got)
			}
		})
	}
}

func TestLiteralStringLineEndingsAndEscapes(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"(HELLO\\\nWORLD)", "HELLOWORLD"},
		{"(HELLO\\\rWORLD)", "HELLOWORLD"},
		{"(HELLO\\\r\nWORLD)", "HELLOWORLD"},
		{"(A\nB\rC\r\nD)", "A\nB\nC\nD"},
		{"(A\\nB\\rC)", "A\nB\rC"},
		{"(A\\050B\\051\\\\C)", "A(B)\\C"},
		{"(A(B\\\r\nC)D)", "A(BC)D"},
	} {
		t.Run(fmt.Sprintf("%q", tc.input), func(t *testing.T) {
			tokens := newPDFContentTokenizer([]byte(tc.input + " Tj"))
			got, ok, err := tokens.next()
			if err != nil || !ok || !reflect.DeepEqual(got, pdfString(tc.want)) {
				t.Fatalf("string=%q, %v, %v; want %q", got, ok, err, tc.want)
			}
			if next, ok, err := tokens.next(); err != nil || !ok || next != "Tj" {
				t.Fatalf("following operator=%v, %v, %v", next, ok, err)
			}
		})
	}
}
