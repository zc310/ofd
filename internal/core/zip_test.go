package core

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/klauspost/compress/zip"
)

func TestPackageReadsContentAndXML(t *testing.T) {
	archiveData := newTestZip(t, map[string][]byte{
		"OFD.xml":      []byte(`<OFD><DocBody/></OFD>`),
		"nested/data":  []byte("content"),
		"/leading.txt": []byte("leading slash"),
	})
	archive, err := OpenBytes(archiveData)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()

	var document struct {
		DocBodies []struct{} `xml:"DocBody"`
	}
	if err := archive.ReadXML("/OFD.xml", &document); err != nil {
		t.Fatal(err)
	}
	if len(document.DocBodies) != 1 {
		t.Fatalf("DocBody count = %d, want 1", len(document.DocBodies))
	}
	content, err := archive.Read("/nested/data")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "content" {
		t.Fatalf("content = %q, want %q", content, "content")
	}
	leading, err := archive.Read("leading.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(leading) != "leading slash" {
		t.Fatalf("leading = %q, want %q", leading, "leading slash")
	}
}

func TestPackageExposesEntryMetadataAndLookup(t *testing.T) {
	archiveData := newTestZip(t, map[string][]byte{
		"nested/data": []byte("content"),
	})
	archive, err := OpenBytes(archiveData)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()

	if !archive.Has("/nested/data") {
		t.Fatal("Has returned false for an existing entry")
	}
	entry, ok := archive.Lookup("nested/data")
	if !ok {
		t.Fatal("Entry returned false for an existing entry")
	}
	if entry.Name != "nested/data" || entry.Path != "nested/data" || entry.UncompressedSize != uint64(len("content")) || entry.IsDir {
		t.Fatalf("entry = %+v", entry)
	}
	entries := archive.Entries()
	if len(entries) != 1 || entries[0].Name != "nested/data" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestPackageWalkEntriesStopsEarlyWithoutBuildingIndex(t *testing.T) {
	archive, err := OpenBytes(newTestZip(t, map[string][]byte{
		"first":  []byte("first"),
		"second": []byte("second"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()

	count := 0
	if err := archive.WalkEntries(func(entry Entry) bool {
		count++
		return false
	}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("walk callback count = %d, want 1", count)
	}
	if archive.entries != nil || archive.fileMap != nil {
		t.Fatal("WalkEntries built the full package index")
	}
}

func TestPackageWalkEntriesRejectsClosedPackage(t *testing.T) {
	archive, err := OpenBytes(newTestZip(t, map[string][]byte{"data": []byte("content")}))
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.WalkEntries(func(Entry) bool { return true }); !errors.Is(err, ErrPackageClosed) {
		t.Fatalf("WalkEntries error = %v, want ErrPackageClosed", err)
	}
}

func TestPackageOpenEntryUsesExactEntry(t *testing.T) {
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for _, content := range []string{"first", "second"} {
		entry, err := writer.Create("data")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	archive, err := OpenBytes(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	entries := archive.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	for i, want := range []string{"first", "second"} {
		reader, err := archive.OpenEntry(entries[i])
		if err != nil {
			t.Fatal(err)
		}
		content, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		if string(content) != want {
			t.Fatalf("entry %d content = %q, want %q", i, content, want)
		}
	}
}

func TestPackageOpenEntryRejectsEntryFromAnotherPackage(t *testing.T) {
	first, err := OpenBytes(newTestZip(t, map[string][]byte{"data": []byte("first")}))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenBytes(newTestZip(t, map[string][]byte{"data": []byte("second")}))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	entry := first.Entries()[0]
	if _, err := second.OpenEntry(entry); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("OpenEntry error = %v, want os.ErrNotExist", err)
	}
}

func TestPackageReadLimit(t *testing.T) {
	archiveData := newTestZip(t, map[string][]byte{"data": []byte("content")})
	archive, err := OpenBytes(archiveData)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()

	content, err := archive.ReadLimit("data", 7)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "content" {
		t.Fatalf("content = %q", content)
	}
	if _, err := archive.ReadLimit("data", 6); !errors.Is(err, ErrReadLimitExceeded) {
		t.Fatalf("ReadLimit error = %v, want ErrReadLimitExceeded", err)
	}
	if _, err := archive.ReadLimit("data", -1); err == nil {
		t.Fatal("ReadLimit accepted a negative limit")
	}
}

func TestPackageReadXMLLimit(t *testing.T) {
	xmlData := []byte(`<Document><Title>测试</Title></Document>`)
	archive, err := OpenBytes(newTestZip(t, map[string][]byte{
		"document.xml": xmlData,
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()

	var document struct {
		Title string `xml:"Title"`
	}
	if err := archive.ReadXMLLimit("document.xml", &document, int64(len(xmlData))); err != nil {
		t.Fatal(err)
	}
	if document.Title != "测试" {
		t.Fatalf("title = %q, want 测试", document.Title)
	}
	if err := archive.ReadXMLLimit("document.xml", &document, 8); !errors.Is(err, ErrReadLimitExceeded) {
		t.Fatalf("ReadXMLLimit error = %v, want ErrReadLimitExceeded", err)
	}
}

func TestOpenReaderReturnsReadError(t *testing.T) {
	wantErr := errors.New("读取失败")
	_, err := OpenReader(errorReader{err: wantErr})
	if !errors.Is(err, wantErr) {
		t.Fatalf("OpenReader error = %v, want %v", err, wantErr)
	}
}

func TestPackageOpenReaderDoesNotCloseInput(t *testing.T) {
	input := &trackingReader{Reader: bytes.NewReader(newTestZip(t, map[string][]byte{
		"data": []byte("content"),
	}))}
	archive, err := OpenReader(input)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if input.closed {
		t.Fatal("OpenReader closed the input reader")
	}
}

func TestPackageOpenFileReadsAndCloses(t *testing.T) {
	filename := t.TempDir() + "/document.ofd"
	if err := os.WriteFile(filename, newTestZip(t, map[string][]byte{
		"data": []byte("content"),
	}), 0o600); err != nil {
		t.Fatal(err)
	}
	archive, err := OpenFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	content, err := archive.Read("data")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "content" {
		t.Fatalf("content = %q, want content", content)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Read("data"); !errors.Is(err, ErrPackageClosed) {
		t.Fatalf("Read after file-backed Close error = %v, want ErrPackageClosed", err)
	}
}

func TestPackageAcceptsZipInsecurePath(t *testing.T) {
	t.Setenv("GODEBUG", "zipinsecurepath=0")
	filename := t.TempDir() + "/document.ofd"
	if err := os.WriteFile(filename, newTestZip(t, map[string][]byte{
		"../data": []byte("content"),
	}), 0o600); err != nil {
		t.Fatal(err)
	}
	archive, err := OpenFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	content, err := archive.Read("../data")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "content" {
		t.Fatalf("content = %q, want content", content)
	}
}

func TestPackageEntriesReturnsMetadataSnapshot(t *testing.T) {
	archive, err := OpenBytes(newTestZip(t, map[string][]byte{
		"data": []byte("content"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()

	entries := archive.Entries()
	if len(entries) != 1 {
		t.Fatalf("entries = %+v", entries)
	}
	entries[0].Name = "changed"
	entry, ok := archive.Lookup("data")
	if !ok || entry.Name != "data" {
		t.Fatalf("Lookup after changing snapshot = %+v, %v", entry, ok)
	}
}

func TestPackageRejectsDirectoryRead(t *testing.T) {
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	if _, err := writer.Create("directory/"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	archive, err := OpenBytes(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if _, err := archive.Read("directory/"); err == nil {
		t.Fatal("Read accepted a directory entry")
	}
}

func TestPackageRejectsReadAfterClose(t *testing.T) {
	archive, err := OpenBytes(newTestZip(t, map[string][]byte{"data": []byte("content")}))
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Read("data"); !errors.Is(err, ErrPackageClosed) {
		t.Fatalf("Read after Close error = %v, want ErrPackageClosed", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("second Close error = %v", err)
	}
}

func TestPackageCloseCachesCloseError(t *testing.T) {
	wantErr := errors.New("关闭失败")
	closer := &errorCloser{err: wantErr}
	archive := newPackage(nil, closer)

	if err := archive.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("Close error = %v, want %v", err, wantErr)
	}
	if err := archive.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("second Close error = %v, want %v", err, wantErr)
	}
	if closer.calls != 1 {
		t.Fatalf("Close calls = %d, want 1", closer.calls)
	}
}

func newTestZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

type trackingReader struct {
	*bytes.Reader
	closed bool
}

func (r *trackingReader) Close() error {
	r.closed = true
	return nil
}

var _ io.Reader = (*trackingReader)(nil)

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

var _ io.Reader = errorReader{}

type errorCloser struct {
	err   error
	calls int
}

func (c *errorCloser) Close() error {
	c.calls++
	return c.err
}

var _ io.Closer = (*errorCloser)(nil)
