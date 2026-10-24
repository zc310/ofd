package ses

import (
	"encoding/asn1"
	"net"
	"net/url"
	"testing"

	"github.com/emmansun/gmsm/smx509"
)

func TestTimestampAccuracyValidatesRangesAndFieldOrder(t *testing.T) {
	// RFC 3161 的 Accuracy 使用秒、[0] 毫秒、[1] 微秒；后两者范围为 1..999。
	for _, test := range []struct {
		name  string
		data  []byte
		valid bool
	}{
		{"absent", nil, true},
		{"zero seconds", []byte{2, 1, 0}, true},
		{"all fields", []byte{2, 1, 1, 0x80, 2, 3, 0xe7, 0x81, 1, 1}, true},
		{"negative seconds", []byte{2, 1, 0xff}, false},
		{"zero millis", []byte{0x80, 1, 0}, false},
		{"millis overflow", []byte{0x80, 2, 3, 0xe8}, false},
		{"zero micros", []byte{0x81, 1, 0}, false},
		{"duplicate seconds", []byte{2, 1, 0, 2, 1, 1}, false},
		{"reversed fields", []byte{0x81, 1, 1, 0x80, 1, 1}, false},
		{"unknown tag", []byte{5, 0}, false},
		{"truncated integer", []byte{2, 2, 1}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := checkTimestampAccuracy(test.data); (err == nil) != test.valid {
				t.Fatalf("精度字段验证结果 = %v，期望有效=%t", err, test.valid)
			}
		})
	}
}

func TestTimestampNameMatchesCertificateIdentity(t *testing.T) {
	uri, err := url.Parse("https://tsa.example.test/service")
	if err != nil {
		t.Fatal(err)
	}
	certificate := &smx509.Certificate{
		EmailAddresses: []string{"tsa@example.test"}, DNSNames: []string{"tsa.example.test"},
		URIs: []*url.URL{uri}, IPAddresses: []net.IP{net.ParseIP("192.0.2.1")}, RawSubject: []byte{0x30, 0},
	}
	for _, test := range []struct {
		name  string
		tag   int
		value []byte
		valid bool
	}{
		{"email", 1, []byte("tsa@example.test"), true},
		{"wrong email", 1, []byte("other@example.test"), false},
		{"DNS", 2, []byte("tsa.example.test"), true},
		{"wrong DNS", 2, []byte("other.example.test"), false},
		{"directory name", 4, []byte{0x30, 0}, true},
		{"wrong directory name", 4, []byte{0x30, 1}, false},
		{"URI", 6, []byte(uri.String()), true},
		{"wrong URI", 6, []byte("https://other.example.test/"), false},
		{"IPv4", 7, []byte{192, 0, 2, 1}, true},
		{"wrong IPv4", 7, []byte{192, 0, 2, 2}, false},
		{"invalid IP length", 7, []byte{192, 0, 2}, false},
		{"unsupported name", 8, []byte("other"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			name := asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: test.tag, Bytes: test.value}
			if got := timestampNameMatches(name, certificate); got != test.valid {
				t.Fatalf("TSA 名称匹配=%t，期望 %t", got, test.valid)
			}
		})
	}
}
