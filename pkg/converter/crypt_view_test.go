package converter_test

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/zc310/ofd/pkg/converter"
	"github.com/zc310/ofd/pkg/crypt"
)

func TestConvertConsumesLogicalCryptViewAndLeavesOwnerOpen(t *testing.T) {
	input, err := os.ReadFile("../../testdata/ofd-creator/helloworld.ofd")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("missing fixture")
	}
	if err != nil {
		t.Fatal(err)
	}
	var encrypted bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), input, &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "reader", Password: []byte("secret")}}}); err != nil {
		t.Fatal(err)
	}
	view, err := crypt.Open(t.Context(), encrypted.Bytes(), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Password: []byte("secret")}})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	for _, format := range []string{"text", "pdf"} {
		var original, logical bytes.Buffer
		if err := converter.Convert(t.Context(), "ofd", format, input, &original); err != nil {
			t.Fatal(err)
		}
		if err := converter.Convert(t.Context(), "", format, view, &logical); err != nil {
			t.Fatal(err)
		}
		if format == "text" && !bytes.Equal(original.Bytes(), logical.Bytes()) {
			t.Fatalf("view text changed: %s", &logical)
		}
		if format == "pdf" && !bytes.HasPrefix(logical.Bytes(), []byte("%PDF-")) {
			t.Fatal("view PDF missing header")
		}
		if _, err := view.Read("OFD.xml"); err != nil {
			t.Fatalf("converter closed owner view: %v", err)
		}
	}
}
