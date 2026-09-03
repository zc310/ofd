package parser

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestNewOFDAcceptsReader(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
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
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
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
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "helloworld.ofd"))
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
