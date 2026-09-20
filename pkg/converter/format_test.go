package converter

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"
)

const formatFixture = "../../test/testdata/helloworld.ofd"

func TestFormatRegistryCoversAllEncoders(t *testing.T) {
	kinds := map[string]Kind{
		"pdf":      KindDocument,
		"html":     KindDocument,
		"text":     KindDocument,
		"markdown": KindDocument,
		"png":      KindImage,
		"jpeg":     KindImage,
		"svg":      KindImage,
		"eps":      KindImage,
		"tex":      KindImage,
	}
	registered := map[string]*Format{}
	for _, format := range Formats() {
		registered[format.Name] = format
		if format.MIME == "" {
			t.Errorf("格式 %s 缺少 MIME", format.Name)
		}
		if len(format.Extensions) == 0 {
			t.Errorf("格式 %s 缺少扩展名", format.Name)
		}
	}
	for name, kind := range kinds {
		format, ok := registered[name]
		if !ok {
			t.Errorf("格式 %s 未注册", name)
			continue
		}
		if format.Kind != kind {
			t.Errorf("格式 %s Kind = %d, want %d", name, format.Kind, kind)
		}
	}
}

func TestFormatRegistryResolvesAliasesAndExtensions(t *testing.T) {
	for _, name := range []string{"txt", "text"} {
		format, ok := FormatByName(name)
		if !ok || format.Name != "text" {
			t.Fatalf("FormatByName(%q) = %v, %v; want text", name, format, ok)
		}
	}
	for _, name := range []string{"md", "markdown"} {
		format, ok := FormatByName(name)
		if !ok || format.Name != "markdown" {
			t.Fatalf("FormatByName(%q) = %v, %v; want markdown", name, format, ok)
		}
	}
	for _, name := range []string{"jpg", "jpeg"} {
		format, ok := FormatByName(name)
		if !ok || format.Name != "jpeg" {
			t.Fatalf("FormatByName(%q) = %v, %v; want jpeg", name, format, ok)
		}
	}
	for ext, want := range map[string]string{
		".png":      "png",
		"PNG":       "png",
		".jpg":      "jpeg",
		".jpeg":     "jpeg",
		".md":       "markdown",
		".markdown": "markdown",
		".txt":      "text",
		".html":     "html",
		".htm":      "html",
	} {
		format, ok := FormatByExtension(ext)
		if !ok || format.Name != want {
			t.Fatalf("FormatByExtension(%q) = %v, %v; want %s", ext, format, ok, want)
		}
	}
	if !IsImageFormat("png") || !IsImageFormat("jpg") || IsImageFormat("pdf") {
		t.Fatal("IsImageFormat 未按 Kind 区分图像与文档格式")
	}
}

func TestEncodeDispatchesByFormatName(t *testing.T) {
	cases := []struct {
		format string
		prefix []byte
	}{
		{"png", []byte{0x89, 'P', 'N', 'G'}},
		{"jpg", []byte{0xFF, 0xD8}},
		{"jpeg", []byte{0xFF, 0xD8}},
	}
	for _, tc := range cases {
		var output bytes.Buffer
		if err := Encode(tc.format, formatFixture, &output, Page(1), DPI(72)); err != nil {
			t.Fatalf("Encode(%q) 失败: %v", tc.format, err)
		}
		if !bytes.HasPrefix(output.Bytes(), tc.prefix) {
			t.Fatalf("Encode(%q) 输出前缀 %v, want %v", tc.format, output.Bytes()[:min(4, output.Len())], tc.prefix)
		}
	}

	var text bytes.Buffer
	if err := Encode("txt", formatFixture, &text, Page(1)); err != nil {
		t.Fatalf("Encode(txt) 失败: %v", err)
	}
	if text.Len() == 0 {
		t.Fatal("Encode(txt) 没有输出文本")
	}

	var combined bytes.Buffer
	if err := Encode("md", formatFixture, &combined, Page(1)); err != nil {
		t.Fatalf("Encode(md) 失败: %v", err)
	}
	if !bytes.HasPrefix(combined.Bytes(), []byte("# Hello World")) {
		t.Fatalf("Encode(md) 输出 = %q", combined.String())
	}
}

func TestEncodeRejectsUnknownFormatAndMissingImageOutput(t *testing.T) {
	fixture := filepath.Clean(formatFixture)
	if err := Encode("webp", fixture, &bytes.Buffer{}); err == nil {
		t.Fatal("Encode(webp) 期望返回错误")
	}
	if err := Encode("png", fixture, nil); err == nil {
		t.Fatal("Encode(png, nil) 期望返回未设置图像输出参数错误")
	}
}

func TestWithFormatOptionSelectsRegisteredEncoder(t *testing.T) {
	var output bytes.Buffer
	if err := Image(formatFixture, WithFormat("svg"), Page(1), Writer(func(int) (io.WriteCloser, error) {
		return &nopWriteCloser{&output}, nil
	})); err != nil {
		t.Fatalf("Image(WithFormat(svg)) 失败: %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte("<svg")) {
		t.Fatalf("Image(WithFormat(svg)) 输出不是 SVG: %q", output.String()[:min(64, output.Len())])
	}
}
