package reader_test

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/zc310/ofd/pkg/crypt"
	ofdreader "github.com/zc310/ofd/pkg/reader"
)

func TestReaderOpensCryptViewAndReleasesOnlyItsLease(t *testing.T) {
	data, err := os.ReadFile("../../testdata/ofd-creator/helloworld.ofd")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("missing fixture")
	}
	if err != nil {
		t.Fatal(err)
	}
	var encrypted bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), data, &encrypted, crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "reader", Password: []byte("secret")}}}); err != nil {
		t.Fatal(err)
	}
	view, err := crypt.Open(t.Context(), encrypted.Bytes(), crypt.OpenOptions{DecryptOptions: crypt.DecryptOptions{Password: []byte("secret")}})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	reader, err := ofdreader.Open(view)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := view.Read("OFD.xml"); err != nil {
		t.Fatalf("reader closed view: %v", err)
	}
	reader, err = ofdreader.Open(view, ofdreader.OpenOptions{PageCacheCapacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
}
