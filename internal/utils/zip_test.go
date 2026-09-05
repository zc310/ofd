package utils

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

func TestZipFileCacheReadsContentAndXML(t *testing.T) {
	archiveData := newTestZip(t, map[string][]byte{
		"OFD.xml":      []byte(`<OFD><DocBody/></OFD>`),
		"nested/data":  []byte("content"),
		"/leading.txt": []byte("leading slash"),
	})
	reader, err := zip.NewReader(bytes.NewReader(archiveData), int64(len(archiveData)))
	if err != nil {
		t.Fatal(err)
	}
	cache := NewZipFileCache(reader)

	var document struct {
		DocBodies []struct{} `xml:"DocBody"`
	}
	if err := cache.ParseXMLContent("/OFD.xml", &document); err != nil {
		t.Fatal(err)
	}
	if len(document.DocBodies) != 1 {
		t.Fatalf("DocBody count = %d, want 1", len(document.DocBodies))
	}
	content, err := cache.ParseContent("/nested/data")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "content" {
		t.Fatalf("content = %q, want %q", content, "content")
	}
	leading, err := cache.ParseContent("leading.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(leading) != "leading slash" {
		t.Fatalf("leading = %q, want %q", leading, "leading slash")
	}
}

func TestZipFileCacheParseImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	archiveData := newTestZip(t, map[string][]byte{"image.png": encoded.Bytes()})
	reader, err := zip.NewReader(bytes.NewReader(archiveData), int64(len(archiveData)))
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := NewZipFileCache(reader).ParseImage("image.png")
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds() != img.Bounds() {
		t.Fatalf("image bounds = %v, want %v", decoded.Bounds(), img.Bounds())
	}
}

func TestExtractFirstImageReturnsErrorForInvalidImage(t *testing.T) {
	archiveData := newTestZip(t, map[string][]byte{"page.png": []byte("not an image")})
	file := t.TempDir() + "/document.ofd"
	if err := ioWriteFile(file, archiveData); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtractFirstImage(file); err == nil {
		t.Fatal("ExtractFirstImage returned nil error for invalid image")
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

func ioWriteFile(name string, data []byte) error {
	return os.WriteFile(name, data, 0o600)
}
