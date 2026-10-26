package creator

import "testing"

func TestPermissionsPrintCopiesPreserveStandardValues(t *testing.T) {
	for _, copies := range []int{-2147483648, -5, -1, 0, 2, 2147483647} {
		data, err := Marshal(Document{ID: "copies", Permissions: &Permissions{Print: &PrintSettings{Printable: true, Copies: &copies}}, Pages: []Page{{}}})
		if err != nil {
			t.Fatalf("Copies=%d: %v", copies, err)
		}
		document := newTestOFD(t, data)
		if got := document.Documents[0].Permissions.Print.Copies; got != copies {
			t.Fatalf("Copies=%d，回读为 %d", copies, got)
		}
		if err := document.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
