package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/spec"
)

func clearSigningEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"OFD_SIGN_ID", "OFD_SIGN_TIME", "OFD_SIGN_TYPE", "OFD_SIGN_DOCUMENT"} {
		t.Setenv(name, "")
	}
}

func TestSignUsesEnvironmentMetadata(t *testing.T) {
	for _, test := range []struct {
		name, id, document, wantID, wantDocument string
	}{
		{name: "defaults", wantID: "sign-1", wantDocument: "Doc_0"},
		{name: "overrides", id: "approval-2", document: "Doc_7", wantID: "approval-2", wantDocument: "Doc_7"},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearSigningEnvironment(t)
			t.Setenv("OFD_SIGN_ID", test.id)
			t.Setenv("OFD_SIGN_DOCUMENT", test.document)
			t.Setenv("OFD_SIGN_TIME", "2026-01-02T03:04:05+08:00")
			value, err := sign([]byte("environment payload"))
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := parser.ParseSignedValue(value)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.SES == nil {
				t.Fatal("应输出 SES 签章")
			}
			tbs := parsed.SES.TBS
			wantTime := time.Date(2026, 1, 1, 19, 4, 5, 0, time.UTC)
			if !tbs.SignTime.Equal(wantTime) || !tbs.Seal.SealInfo.Property.CreateTime.Equal(wantTime) {
				t.Errorf("签名时间和制章时间应为 %v，实际 %v / %v", wantTime, tbs.SignTime, tbs.Seal.SealInfo.Property.CreateTime)
			}
			if want := "/" + test.wantDocument + "/Signatures/Signature_" + test.wantID + ".xml"; tbs.PropertyInfo != want {
				t.Errorf("签名路径 = %q，期望 %q", tbs.PropertyInfo, want)
			}
			if want := demoProvider + "-" + test.wantID + "@" + producer; tbs.Seal.SealInfo.ESID != want {
				t.Errorf("ESID = %q，期望 %q", tbs.Seal.SealInfo.ESID, want)
			}
			result, err := parser.VerifySESSignedValue(parsed)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Valid {
				t.Fatalf("设置环境变量后签章仍应可验签: %+v", result)
			}
		})
	}
}

func TestSignUsesCurrentTimeWhenEnvironmentTimeIsMissingOrInvalid(t *testing.T) {
	for _, value := range []string{"", "not-a-time"} {
		t.Run(value, func(t *testing.T) {
			clearSigningEnvironment(t)
			t.Setenv("OFD_SIGN_TIME", value)
			before := time.Now().UTC().Truncate(time.Second)
			signedValue, err := sign([]byte("current time payload"))
			if err != nil {
				t.Fatal(err)
			}
			after := time.Now()
			parsed, err := parser.ParseSignedValue(signedValue)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.SES == nil {
				t.Fatal("应输出 SES 签章")
			}
			if got := parsed.SES.TBS.SignTime; got.Before(before) || got.After(after) {
				t.Fatalf("签名时间 %v 应落在调用区间 [%v, %v] 内", got, before, after)
			}
		})
	}
}

func TestSignDigitalBindsOriginalBytesAndSigningTime(t *testing.T) {
	clearSigningEnvironment(t)
	t.Setenv("OFD_SIGN_TYPE", spec.SigTypeSign)
	t.Setenv("OFD_SIGN_TIME", "2026-01-02T03:04:05Z")
	original := []byte("<Signature>digital payload</Signature>")
	value, err := sign(original)
	if err != nil {
		t.Fatal(err)
	}
	wantTime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	options := &parser.SignatureVerificationOptions{CurrentTime: wantTime.Add(time.Minute)}
	result, err := parser.VerifyDigitalSignedValue(original, value, options)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || !result.Outer.Valid || result.Kind != spec.SigTypeSign {
		t.Fatalf("应生成可验签的普通数字签名: %+v", result)
	}
	if !result.VerificationTime.Equal(wantTime) {
		t.Errorf("签名时间 = %v，期望 %v", result.VerificationTime, wantTime)
	}
	if result.FullyValid() || result.Trusted {
		t.Error("演示自签名证书不应被视为具有完整信任证据")
	}
	tampered := append(bytes.Clone(original), '\n')
	result, err = parser.VerifyDigitalSignedValue(tampered, value, options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Fatal("原文增加一个换行后应验签失败")
	}
}

func TestWriteSignedValuePreservesReadErrorWithoutOutput(t *testing.T) {
	readError := errors.New("读取中断")
	reader := io.MultiReader(strings.NewReader("partial payload"), iotest.ErrReader(readError))
	var output bytes.Buffer
	err := writeSignedValue(&output, reader)
	if !errors.Is(err, readError) || !strings.Contains(err.Error(), "读取 Signature.xml 失败") {
		t.Fatalf("应保留读取错误及上下文，实际 %v", err)
	}
	if output.Len() != 0 {
		t.Fatal("读取未完成时不应为部分输入生成签名")
	}
}

func TestWriteSignedValuePreservesWriteError(t *testing.T) {
	clearSigningEnvironment(t)
	writeError := errors.New("写入中断")
	err := writeSignedValue(errorWriter{writeError}, strings.NewReader("payload"))
	if !errors.Is(err, writeError) || !strings.Contains(err.Error(), "写出 SignedValue.dat 失败") {
		t.Fatalf("应保留写入错误及上下文，实际 %v", err)
	}
}

func TestWriteSignedValueDoesNotWriteOnSigningFailure(t *testing.T) {
	for _, test := range []struct {
		name, payload, id, document, signTime string
	}{
		{name: "empty input"},
		{name: "non-ASCII seal ID", payload: "payload", id: "签名"},
		{name: "non-ASCII document path", payload: "payload", document: "文档"},
		{name: "certificate validity overflow", payload: "payload", signTime: "9999-12-31T23:59:59Z"},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearSigningEnvironment(t)
			t.Setenv("OFD_SIGN_ID", test.id)
			t.Setenv("OFD_SIGN_DOCUMENT", test.document)
			t.Setenv("OFD_SIGN_TIME", test.signTime)
			var output bytes.Buffer
			if err := writeSignedValue(&output, strings.NewReader(test.payload)); err == nil {
				t.Fatal("无法签署的输入应返回错误")
			}
			if output.Len() != 0 {
				t.Fatal("签名失败时不应写出 SignedValue.dat")
			}
		})
	}
}

type errorWriter struct{ err error }

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }
