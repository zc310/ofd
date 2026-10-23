package signeddata

import (
	"bytes"
	"encoding/asn1"
	"errors"
	"testing"
	"time"

	"github.com/emmansun/gmsm/pkcs7"
	"github.com/zc310/ofd/internal/ses"
)

func TestVerifyRejectsAmbiguousAttributesAlgorithmsAndTrailingFields(t *testing.T) {
	key, _, cert, err := ses.NewSelfSignedCertificate("test", "test", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("original")
	value, err := Create(data, cert, key, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*signedData)
		want   error
	}{
		{"duplicate", func(sd *signedData) { sd.Signers[0].Signed = append(sd.Signers[0].Signed, sd.Signers[0].Signed[0]) }, ErrInvalid},
		{"unordered_attributes", func(sd *signedData) {
			sd.Signers[0].Signed[0], sd.Signers[0].Signed[1] = sd.Signers[0].Signed[1], sd.Signers[0].Signed[0]
		}, ErrInvalid},
		{"content_type", func(sd *signedData) { sd.Content.Type = pkcs7.OIDData }, ErrInvalid},
		{"digest_algorithm", func(sd *signedData) { sd.Signers[0].Digest.Algorithm = pkcs7.OIDDigestAlgorithmSHA1 }, ErrUnsupported},
		{"signature_algorithm", func(sd *signedData) { sd.Signers[0].Algorithm.Algorithm = pkcs7.OIDEncryptionAlgorithmRSA }, ErrUnsupported},
		{"empty_signers", func(sd *signedData) { sd.Signers = nil }, ErrUnsupported},
	} {
		t.Run(test.name, func(t *testing.T) {
			sd, err := parse(value)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(sd)
			inner, err := asn1.Marshal(*sd)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := asn1.Marshal(contentInfo{Type: pkcs7.SM2OIDSignedData, Content: asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: inner}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Verify(wire, data, nil); !errors.Is(err, test.want) {
				t.Fatalf("err=%v want=%v", err, test.want)
			}
		})
	}
	if _, err := Verify(append(bytes.Clone(value), 0), data, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("尾随字节被接受: %v", err)
	}
	var envelope contentInfo
	if err := decode(value, &envelope); err != nil {
		t.Fatal(err)
	}
	var sequence asn1.RawValue
	if err := decode(envelope.Content.Bytes, &sequence); err != nil {
		t.Fatal(err)
	}
	sequence.FullBytes = nil
	sequence.Bytes = append(sequence.Bytes, 5, 0)
	inner, _ := asn1.Marshal(sequence)
	bad, _ := asn1.Marshal(contentInfo{Type: envelope.Type, Content: asn1.RawValue{Class: 2, Tag: 0, IsCompound: true, Bytes: inner}})
	if _, err := Verify(bad, data, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("隐藏的额外结构字段被接受: %v", err)
	}
}
