package crypt

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"reflect"
	"testing"

	"github.com/emmansun/gmsm/sm3"
)

func protectedSignature(path, method string, digest []byte) []byte {
	return []byte(`<Signature xmlns="` + Namespace + `"><SignedInfo><References CheckMethod="` + method + `"><Reference FileRef="` + path + `"><CheckValue>` + base64.StdEncoding.EncodeToString(digest) + `</CheckValue></Reference></References></SignedInfo></Signature>`)
}

func TestPackageRoundTripPreservesSignatureReferenceDigests(t *testing.T) {
	for _, method := range []string{"SM3", "1.2.156.10197.1.401", "SHA1", "MD5"} {
		t.Run(method, func(t *testing.T) {
			original := testPlainFiles()
			payload := original["Doc_0/Res/data.bin"]
			var digest []byte
			switch method {
			case "SHA1":
				sum := sha1.Sum(payload)
				digest = sum[:]
			case "MD5":
				sum := md5.Sum(payload)
				digest = sum[:]
			default:
				sum := sm3.Sum(payload)
				digest = sum[:]
			}
			original["Doc_0/Signature.xml"] = protectedSignature("Res/data.bin", method, digest)
			encrypted, _ := encryptFiles(t, original, testPasswordOptions())
			output, _ := decryptedFiles(t, testPackage(t, encrypted), DecryptOptions{Password: []byte("secret")})
			if !reflect.DeepEqual(output, original) {
				t.Fatal("加解密应保留受引用条目及签名 XML 的全部原始字节")
			}
		})
	}
}

func TestEncryptRejectsMismatchedOrUnsupportedProtectedDigests(t *testing.T) {
	for _, method := range []string{"SM3", "SHA256"} {
		t.Run(method, func(t *testing.T) {
			files := testPlainFiles()
			files["Doc_0/Signature.xml"] = protectedSignature("Res/data.bin", method, make([]byte, 32))
			report, err := EncryptPackage(t.Context(), testPackage(t, files), testPasswordOptions(), func(string, []byte) error { return nil })
			if !errors.Is(err, ErrProtectedContent) || report != nil {
				t.Fatalf("无法确认受保护字节一致时应拒绝变换: report=%+v, err=%v", report, err)
			}
		})
	}
}

func TestDecryptRefusesToRemoveReferencedEncryptionMetadata(t *testing.T) {
	for _, reference := range []string{"/Encryptions.xml", "/encryptions.xml"} {
		t.Run(reference, func(t *testing.T) {
			files, _ := encryptFiles(t, testPlainFiles(), testPasswordOptions())
			digest := sm3.Sum(files[EntryPoint])
			files["Signature.xml"] = protectedSignature(reference, "SM3", digest[:])
			report, err := DecryptPackage(t.Context(), testPackage(t, files), DecryptOptions{Password: []byte("secret")}, func(string, []byte) error { return nil })
			if !errors.Is(err, ErrProtectedContent) || report != nil {
				t.Fatalf("签名引用的加密控制文件不能被解密导出删除: report=%+v, err=%v", report, err)
			}
		})
	}
}
