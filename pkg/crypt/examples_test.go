package crypt_test

import (
	"bytes"
	"context"
	"fmt"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/pkg/crypt"
)

func ExampleEncrypt() {
	var input bytes.Buffer
	archive := zip.NewWriter(&input)
	entry, err := archive.Create("OFD.xml")
	if err != nil {
		panic(err)
	}
	if _, err := entry.Write([]byte(`<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"/>`)); err != nil {
		panic(err)
	}
	if err := archive.Close(); err != nil {
		panic(err)
	}
	ctx := context.Background()
	var encrypted, plaintext bytes.Buffer
	report, err := crypt.Encrypt(ctx, input.Bytes(), &encrypted, crypt.EncryptOptions{
		Recipients:      []crypt.EncryptRecipient{{UserName: "reader", Password: []byte("示例口令")}},
		IndependentKeys: true,
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(report.State, report.EncryptedEntries)
	result, err := crypt.Decrypt(ctx, encrypted.Bytes(), &plaintext, crypt.DecryptOptions{Password: []byte("示例口令")})
	if err != nil {
		panic(err)
	}
	fmt.Println(result.State, result.DecryptedEntries)
	info, err := crypt.Inspect(ctx, plaintext.Bytes(), crypt.InspectOptions{})
	if err != nil {
		panic(err)
	}
	fmt.Println(info.Encrypted)
	// Output:
	// locked 1
	// unlocked 1
	// false
}

func ExampleEncrypt_append() {
	var input, inner, outer bytes.Buffer
	z := zip.NewWriter(&input)
	w, err := z.Create("OFD.xml")
	if err != nil {
		panic(err)
	}
	if _, err := w.Write([]byte(`<OFD xmlns="http://www.ofdspec.org/2016"/>`)); err != nil {
		panic(err)
	}
	if err := z.Close(); err != nil {
		panic(err)
	}
	ctx := context.Background()
	if _, err := crypt.Encrypt(ctx, input.Bytes(), &inner, crypt.EncryptOptions{
		Recipients: []crypt.EncryptRecipient{{UserName: "reader", Password: []byte("inner")}},
	}); err != nil {
		panic(err)
	}
	report, err := crypt.Encrypt(ctx, inner.Bytes(), &outer, crypt.EncryptOptions{
		Recipients: []crypt.EncryptRecipient{{UserName: "reader", Password: []byte("outer")}},
		Append:     &crypt.DecryptOptions{Password: []byte("inner")},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(report.OperationID, report.Relative)
	// Output: enc2 enc1
}
