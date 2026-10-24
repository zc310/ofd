package entrywriter

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/container"
)

func writeAll(t *testing.T, writer *Writer, writerZip *zip.Writer, entries map[string][]byte) {
	t.Helper()
	for name, data := range entries {
		if err := writer.Write(name, data); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNormalizeOptions(t *testing.T) {
	for _, test := range []struct {
		name        string
		compression container.CompressionMode
		level       int
		limits      container.Limits
		wantMode    container.CompressionMode
		wantLevel   int
		wantErr     bool
	}{
		{name: "defaults", wantMode: container.CompressionAuto, wantLevel: container.DefaultCompressionLevel},
		{name: "configured", compression: container.CompressionStore, level: 9, wantMode: container.CompressionStore, wantLevel: 9},
		{name: "invalid compression", compression: "invalid", wantErr: true},
		{name: "invalid level", level: 10, wantErr: true},
		{name: "invalid limits", limits: container.Limits{MaxEntryBytes: -1}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			mode, level, err := NormalizeOptions(test.compression, test.level, test.limits)
			if (err != nil) != test.wantErr {
				t.Fatalf("NormalizeOptions error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && (mode != test.wantMode || level != test.wantLevel) {
				t.Fatalf("NormalizeOptions = (%q, %d), want (%q, %d)", mode, level, test.wantMode, test.wantLevel)
			}
		})
	}
}

func readPackage(t *testing.T, data []byte) map[string]string {
	t.Helper()
	pkg, err := container.OpenBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()
	result := make(map[string]string)
	for _, entry := range pkg.Entries() {
		value, readErr := pkg.Read(entry.Path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		result[entry.Path] = string(value)
	}
	return result
}

func TestWritePreservesEntries(t *testing.T) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	writer := New(archive, Config{})
	writeAll(t, writer, archive, map[string][]byte{
		"Doc_0/Document.xml":             []byte(`<Document/>`),
		"Doc_0/Res/Image_3.png":          []byte("png-bytes"),
		"Doc_0/Pages/Page_0/Content.xml": []byte(`<Content/>`),
	})
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	entries := readPackage(t, buffer.Bytes())
	if entries["Doc_0/Document.xml"] != `<Document/>` {
		t.Fatalf("条目内容不符: %q", entries["Doc_0/Document.xml"])
	}
	if entries["Doc_0/Res/Image_3.png"] != "png-bytes" {
		t.Fatalf("条目内容不符: %q", entries["Doc_0/Res/Image_3.png"])
	}
}

func TestWriteRejectsDuplicatePath(t *testing.T) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	writer := New(archive, Config{})
	if err := writer.Write("a.xml", []byte("<A/>")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Write("a.xml", []byte("<B/>")); err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("重复路径应报错, got %v", err)
	}
}

func TestWriteSourceStreamsPreserved(t *testing.T) {
	source := buildSourceZip(t, map[string][]byte{"Doc_0/Document.xml": []byte(`<Document/>`)})
	pkg, err := container.OpenBytes(source)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()

	var target bytes.Buffer
	archive := zip.NewWriter(&target)
	writer := New(archive, Config{})
	for _, entry := range pkg.Entries() {
		if err := writer.WriteSource(pkg, entry, entry.Path); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	entries := readPackage(t, target.Bytes())
	if entries["Doc_0/Document.xml"] != `<Document/>` {
		t.Fatalf("流转条目内容不符: %q", entries["Doc_0/Document.xml"])
	}
}

func TestWriteSourceSizeLimit(t *testing.T) {
	source := buildSourceZip(t, map[string][]byte{"Doc_0/big.dat": bytes.Repeat([]byte("x"), 4096)})
	pkg, err := container.OpenBytes(source)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkg.Close() }()

	var target bytes.Buffer
	archive := zip.NewWriter(&target)
	writer := New(archive, Config{Limits: container.Limits{MaxEntryBytes: 1024, MaxTotalBytes: 4096}})
	entry, _ := pkg.Lookup("Doc_0/big.dat")
	if err := writer.WriteSource(pkg, entry, entry.Path); err == nil || !strings.Contains(err.Error(), "超过单条上限") {
		t.Fatalf("超限条目应报错, got %v", err)
	}
}

func TestDeterministicTimestamps(t *testing.T) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	writer := New(archive, Config{Deterministic: true})
	if err := writer.Write("a.xml", []byte("<A/>")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		expected := time.Unix(0, 0).UTC()
		if !file.Modified.Equal(expected) {
			t.Fatalf("确定性条目时间应为 %v, got %v", expected, file.Modified)
		}
	}
}

func TestWarningCallback(t *testing.T) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	var warned []string
	writer := New(archive, Config{OnWarning: func(message string) { warned = append(warned, message) }})
	writer.Warn("注意")
	if len(warned) != 1 || warned[0] != "注意" {
		t.Fatalf("警告回调未被调用: %v", warned)
	}
}

func TestInvalidCompressionLevelFallsBackWithWarning(t *testing.T) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	var warned []string
	writer := New(archive, Config{CompressionLevel: 12, OnWarning: func(message string) { warned = append(warned, message) }})
	if err := writer.Write("a.xml", []byte("<A/>")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "无效的 DEFLATE 压缩级别") {
		t.Fatalf("无效级别应回退默认并警告: %v", warned)
	}
	entries := readPackage(t, buffer.Bytes())
	if entries["a.xml"] != "<A/>" {
		t.Fatalf("无效级别回退后仍应正常写入: %q", entries["a.xml"])
	}
}

func TestDeflateCompressionLevelWrites(t *testing.T) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	writer := New(archive, Config{Compression: container.CompressionDeflate, CompressionLevel: 1})
	if err := writer.Write("Doc_0/Document.xml", []byte(`<Document/>`)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 1 || reader.File[0].Method != zip.Deflate {
		t.Fatalf("deflate 级别条目方法不符: %+v", reader.File)
	}
	entries := readPackage(t, buffer.Bytes())
	if entries["Doc_0/Document.xml"] != `<Document/>` {
		t.Fatalf("deflate 级别条目内容不符: %q", entries["Doc_0/Document.xml"])
	}
}

func buildSourceZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, data := range files {
		file, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
