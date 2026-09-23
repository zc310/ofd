package creator

import (
	"bytes"
	"testing"

	"github.com/klauspost/compress/zip"
)

func TestEntryMethodDefaults(t *testing.T) {
	tests := []struct {
		name string
		want uint16
	}{
		{"Doc_0/Document.xml", zip.Deflate},
		{"Doc_0/Res/a.png", zip.Store},
		{"Doc_0/Res/a.jpg", zip.Store},
		{"Doc_0/Res/a.pdf", zip.Store},
		{"Doc_0/Res/a.ofd", zip.Store},
		{"Doc_0/Res/a.docx", zip.Store},
		{"Doc_0/Res/a.xlsx", zip.Store},
		{"Doc_0/Res/a.pptx", zip.Store},
		{"Doc_0/Res/a.7z", zip.Store},
		{"Doc_0/Res/a.rar", zip.Store},
		{"Doc_0/Res/a.m4v", zip.Store},
		{"Doc_0/Res/a.wma", zip.Store},
		{"Doc_0/Res/a.ttf", zip.Deflate},
		{"Doc_0/Res/a.txt", zip.Deflate},
	}
	for _, test := range tests {
		if got := EntryMethod(test.name, CompressionAuto); got != test.want {
			t.Errorf("EntryMethod(%q) = %d, want %d", test.name, got, test.want)
		}
	}
}

func TestEntryMethodForced(t *testing.T) {
	if got := EntryMethod("Doc_0/Res/a.png", CompressionDeflate); got != zip.Deflate {
		t.Errorf("deflate 强制应返回 Deflate, got %d", got)
	}
	if got := EntryMethod("Doc_0/Document.xml", CompressionStore); got != zip.Store {
		t.Errorf("store 强制应返回 Store, got %d", got)
	}
}

func TestLimitWriterRejectsOverflow(t *testing.T) {
	var buffer bytes.Buffer
	remaining := int64(100)
	writer := NewLimitWriter(&buffer, "x.xml", 50, 100, &remaining)
	if _, err := writer.Write(bytes.Repeat([]byte("a"), 51)); err == nil {
		t.Fatal("超过单条上限应报错")
	}
	if _, err := writer.Write(bytes.Repeat([]byte("b"), 10)); err != nil {
		t.Fatalf("写入应成功: %v", err)
	}
	if remaining != 90 {
		t.Fatalf("剩余预算应为 90, got %d", remaining)
	}
}

func TestLimitWriterTotalBudget(t *testing.T) {
	var buffer bytes.Buffer
	remaining := int64(10)
	writer := NewLimitWriter(&buffer, "x.xml", 100, 10, &remaining)
	if _, err := writer.Write(bytes.Repeat([]byte("a"), 11)); err == nil {
		t.Fatal("超过总预算应报错")
	}
}

func TestCheckSize(t *testing.T) {
	remaining := int64(50)
	if err := CheckSize("a.xml", 60, 100, 50, &remaining); err == nil {
		t.Fatal("超过单条上限应报错")
	}
	remaining = int64(50)
	if err := CheckSize("a.xml", 10, 100, 50, &remaining); err != nil {
		t.Fatalf("合法大小应通过: %v", err)
	}
	if remaining != 40 {
		t.Fatalf("剩余预算应为 40, got %d", remaining)
	}
}

func TestIsSignatureEntry(t *testing.T) {
	valid := []string{
		"Doc_0/Signatures.xml",
		"Doc_0/Signs.xml",
		"Doc_0/Signatures/Signature_1.xml",
		"Doc_0/Signatures/Data/sign-1.dat",
		"Doc_0/Signs/Signature_1.xml",
		"Signatures.xml",
		"Doc_0/Signatures/Signature.xml",
	}
	for _, name := range valid {
		if !IsSignatureEntry(name) {
			t.Errorf("IsSignatureEntry(%q) 应为 true", name)
		}
	}
	for _, name := range []string{"Doc_0/Document.xml", "Doc_0/Res/Seal.svg", "Doc_0/Signature.xml.tmp"} {
		if IsSignatureEntry(name) {
			t.Errorf("IsSignatureEntry(%q) 应为 false", name)
		}
	}
}

func TestInSignatureDir(t *testing.T) {
	if !InSignatureDir("Doc_0/Signatures/Signature_1.xml", "Doc_0/Signatures") {
		t.Fatal("签名目录内条目应命中")
	}
	if InSignatureDir("Doc_0/Document.xml", "Doc_0/Signatures") {
		t.Fatal("签名目录外条目不应命中")
	}
	if InSignatureDir("Doc_0/Signatures", "") {
		t.Fatal("空目录应忽略")
	}
	if !InSignatureDir("Doc_0/Signs/a.dat", "Doc_0/Signatures", "Doc_0/Signs") {
		t.Fatal("多个目录时应命中任一")
	}
}
