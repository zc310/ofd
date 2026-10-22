package parser

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewOFDAcceptsReader(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "ofd-creator/helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}

	reader := bytes.NewReader(data)
	ofd, err := NewOFD(reader)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	if len(ofd.Documents) == 0 {
		t.Fatal("reader input produced no documents")
	}
}

func TestOFDOpenReaderDoesNotCloseReader(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "ofd-creator/helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}

	reader := &trackingReader{Reader: bytes.NewReader(data)}
	ofd, err := NewOFD(reader)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	if reader.closed {
		t.Fatal("Open closed the input reader")
	}
}

func TestOFDOpenReaderReturnsReadError(t *testing.T) {
	wantErr := errors.New("read failed")
	_, err := NewOFD(errorReader{err: wantErr})
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("NewOFD error = %v, want wrapped %v", err, wantErr)
	}
}

func TestOFDOpenFailurePreservesCurrentDocument(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "ofd-creator/helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}

	ofd, err := NewOFD(data)
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()

	wantDocuments := len(ofd.Documents)
	if err := ofd.Open([]byte("not an OFD archive")); err == nil {
		t.Fatal("Open returned nil error for invalid archive")
	}
	if len(ofd.Documents) != wantDocuments {
		t.Fatalf("document count after failed Open = %d, want %d", len(ofd.Documents), wantDocuments)
	}
}

func TestOFDInputSizeLimitAppliesToAllInputKinds(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "ofd-creator/helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "input.ofd")
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"file", "bytes", "reader"} {
		for _, exact := range []bool{true, false} {
			name := kind + "/over_limit"
			limit := int64(len(data) - 1)
			if exact {
				name, limit = kind+"/exact_limit", int64(len(data))
			}
			t.Run(name, func(t *testing.T) {
				var input any = data
				if kind == "file" {
					input = filename
				} else if kind == "reader" {
					input = bytes.NewReader(data)
				}
				ofd, err := NewOFDWithOptions(input, Options{MaxInputBytes: limit})
				if ofd != nil {
					defer ofd.Close()
				}
				if exact {
					if err != nil {
						t.Fatalf("恰好达到大小上限时应能打开：%v", err)
					}
				} else if err == nil || !errors.Is(err, ErrInputTooLarge) || !strings.Contains(err.Error(), "大小限制") {
					t.Fatalf("超过上限应报大小限制错误，实际为 %v", err)
				}
			})
		}
	}
}

func TestOFDReaderStopsAfterInputLimitProbe(t *testing.T) {
	reader := bytes.NewReader(make([]byte, 1024))
	ofd, err := NewOFDWithOptions(reader, Options{MaxInputBytes: 64})
	if ofd != nil {
		defer ofd.Close()
	}
	if err == nil || !errors.Is(err, ErrInputTooLarge) || !strings.Contains(err.Error(), "大小限制") {
		t.Fatalf("超过上限应报大小限制错误，实际为 %v", err)
	}
	if got := reader.Len(); got != 1024-65 {
		t.Fatalf("剩余输入 = %d 字节，期望只读取上限加 1 字节", got)
	}
}

func TestParserCategorizedErrors(t *testing.T) {
	var ofd OFD
	if err := ofd.SelectVersion(0, "v1"); !errors.Is(err, ErrDocumentClosed) {
		t.Fatalf("closed document error = %v, want ErrDocumentClosed", err)
	}
	closedDocument := &Document{closing: true}
	if err := (&Page{document: closedDocument}).EnsureLoaded(); !errors.Is(err, ErrDocumentClosed) {
		t.Fatalf("closed page document error = %v, want ErrDocumentClosed", err)
	}
	if _, err := (&Document{}).GetPage(0); !errors.Is(err, ErrPageIndexOutOfRange) {
		t.Fatalf("page index error = %v, want ErrPageIndexOutOfRange", err)
	}
	parsed, err := NewOFD(filepath.Join("..", "..", "testdata", "ofd-creator/helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer parsed.Close()
	if err := parsed.SelectVersion(-1, ""); !errors.Is(err, ErrDocumentScopeOutOfRange) {
		t.Fatalf("document scope error = %v, want ErrDocumentScopeOutOfRange", err)
	}
	if err := parsed.SelectVersion(0, "missing-version"); !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("version error = %v, want ErrVersionNotFound", err)
	}
}

func TestOFDReaderAcceptsMaxInt64InputLimit(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "ofd-creator/helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	ofd, err := NewOFDWithOptions(bytes.NewReader(data), Options{MaxInputBytes: 1<<63 - 1})
	if err != nil {
		t.Fatalf("最大 int64 上限不应溢出导致输入为空：%v", err)
	}
	defer ofd.Close()
	if len(ofd.Documents) != 1 {
		t.Fatalf("文档数量 = %d，期望 1", len(ofd.Documents))
	}
}

func TestOFDMalformedPackagePreservesUsableCurrentDocument(t *testing.T) {
	ofd, err := NewOFD(filepath.Join("..", "..", "testdata", "ofd-creator/helloworld.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	document := ofd.Documents[0]
	page := document.Pages[0]
	box, err := page.PhysicalBox()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{"missing_root", map[string]string{"other.xml": `<Other/>`}},
		{"malformed_root", map[string]string{"OFD.xml": `<OFD><DocBody></OFD>`}},
		{"missing_document", map[string]string{"OFD.xml": `<OFD><DocBody><DocRoot>missing.xml</DocRoot></DocBody></OFD>`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ofd.Open(parserTestArchive(t, tc.files)); err == nil {
				t.Fatal("损坏 OFD 包应打开失败")
			}
			if len(ofd.Documents) != 1 || ofd.Documents[0] != document {
				t.Fatal("打开失败改变了当前文档")
			}
			if got, err := page.PhysicalBox(); err != nil || got != box {
				t.Fatalf("原页面不再可用：%+v, %v，期望 %+v", got, err, box)
			}
		})
	}
}

type trackingReader struct {
	*bytes.Reader

	closed bool
}

func (r *trackingReader) Close() error {
	r.closed = true
	return nil
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

var _ io.Reader = errorReader{}
