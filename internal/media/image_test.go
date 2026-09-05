package media

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"
)

func TestDecodeZipImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	filename := writeTestZip(t, map[string][]byte{"image.png": encoded.Bytes()})

	archive, err := zip.OpenReader(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()

	decoded, ok := decodeZipImage(archive.File[0])
	if !ok {
		t.Fatal("decodeZipImage returned false")
	}
	if decoded.Bounds() != img.Bounds() {
		t.Fatalf("image bounds = %v, want %v", decoded.Bounds(), img.Bounds())
	}
}

func TestExtractFirstImageReturnsErrorForInvalidImage(t *testing.T) {
	filename := writeTestZip(t, map[string][]byte{"page.png": []byte("not an image")})
	if _, err := ExtractFirstImage(filename); err == nil {
		t.Fatal("ExtractFirstImage returned nil error for invalid image")
	}
}

func TestIsImageExtension(t *testing.T) {
	for _, extension := range []string{".png", ".JPG", ".tiff"} {
		if !IsImageExtension(extension) {
			t.Fatalf("IsImageExtension(%q) = false", extension)
		}
	}
	if IsImageExtension(".txt") {
		t.Fatal("IsImageExtension(.txt) = true")
	}
}

func writeTestZip(t *testing.T, files map[string][]byte) string {
	t.Helper()
	filename := t.TempDir() + "/document.ofd"
	file, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for name, content := range files {
		entry, err := archive.Create(name)
		if err != nil {
			file.Close()
			t.Fatal(err)
		}
		if _, err := entry.Write(content); err != nil {
			archive.Close()
			file.Close()
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return filename
}
