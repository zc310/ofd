package creator

import "testing"

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
