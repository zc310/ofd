package crypt

import (
	"bytes"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/core"
)

func memoryZIP(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var data bytes.Buffer
	z := zip.NewWriter(&data)
	for name, content := range files {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestViewErasesOwnedKeysAfterLastConsumerLease(t *testing.T) {
	input, err := core.OpenBytes(memoryZIP(t, map[string][]byte{"OFD.xml": []byte(`<OFD xmlns="http://www.ofdspec.org/2016"/>`)}))
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	encrypted := map[string][]byte{}
	if _, err := EncryptPackage(t.Context(), input, EncryptOptions{Recipients: []EncryptRecipient{{UserName: "reader", Password: []byte("secret")}}}, func(name string, data []byte) error { encrypted[name] = bytes.Clone(data); return nil }); err != nil {
		t.Fatal(err)
	}
	pkg, err := core.OpenBytes(memoryZIP(t, encrypted))
	if err != nil {
		t.Fatal(err)
	}
	closed := 0
	view, err := OpenView(t.Context(), pkg, OpenOptions{DecryptOptions: DecryptOptions{Password: []byte("secret")}}, func() error { closed++; return pkg.Close() })
	if err != nil {
		t.Fatal(err)
	}
	if len(view.keys) != 1 || view.opt.Password != nil || view.opt.PrivateKey != nil || view.opt.Resolve != nil {
		t.Fatal("view retained caller credentials or failed to recover owned key")
	}
	key := view.keys[0]
	before := bytes.Clone(key.fek)
	lease, err := view.OFDPackage()
	if err != nil {
		t.Fatal(err)
	}
	if err := view.Close(); err != nil {
		t.Fatal(err)
	}
	if closed != 0 || !bytes.Equal(key.fek, before) {
		t.Fatal("owner close erased live consumer key")
	}
	if _, err := lease.Read("OFD.xml"); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if closed != 1 || !bytes.Equal(key.fek, make([]byte, 16)) || !bytes.Equal(key.iv, make([]byte, 16)) {
		t.Fatal("last lease did not clear key/IV and release input")
	}
	if err := view.Close(); err != nil || closed != 1 {
		t.Fatal("close was not idempotent")
	}
}
