package crypt

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/emmansun/gmsm/sm3"
)

func TestProtectionPreservesCancellationAndDeadlineErrors(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		for _, name := range []string{"OFD.xml", "Signature.xml", "data.bin"} {
			t.Run(fmt.Sprintf("timeout=%t/%s", timeout, name), func(t *testing.T) {
				var ctx context.Context
				var cancel context.CancelFunc
				if timeout {
					ctx, cancel = context.WithDeadline(t.Context(), time.Unix(1, 0))
				} else {
					ctx, cancel = context.WithCancel(t.Context())
					cancel()
				}
				defer cancel()
				options, err := NormalizeOptions(InspectOptions{})
				if err != nil {
					t.Fatal(err)
				}
				guard := newProtection(&inspector{ctx: ctx, opt: options}, nil, nil)
				for _, data := range [][]byte{testPlainFiles()["OFD.xml"], {}} {
					err := guard.observe(name, data)
					if err != ctx.Err() || !errors.Is(err, ctx.Err()) || errors.Is(err, ErrDecryptFailed) {
						t.Fatalf("取消或超时必须原样传播: got=%v want=%v", err, ctx.Err())
					}
				}
			})
		}
	}
}

func protectedSignature(path, method string, digest []byte) []byte {
	return []byte(`<Signature xmlns="` + Namespace + `"><SignedInfo><References CheckMethod="` + method + `"><Reference FileRef="` + path + `"><CheckValue>` + base64.StdEncoding.EncodeToString(digest) + `</CheckValue></Reference></References></SignedInfo></Signature>`)
}

func TestPackageRoundTripPreservesSignatureReferenceDigests(t *testing.T) {
	for _, method := range []string{"SM3", "1.2.156.10197.1.401", "SHA1", "MD5", "SHA256", "SHA384", "SHA512"} {
		t.Run(method, func(t *testing.T) {
			original := testPlainFiles()
			payload := original["Doc_0/Res/data.bin"]
			var digest []byte
			switch method {
			case "SHA256":
				sum := sha256.Sum256(payload)
				digest = sum[:]
			case "SHA384":
				sum := sha512.Sum384(payload)
				digest = sum[:]
			case "SHA512":
				sum := sha512.Sum512(payload)
				digest = sum[:]
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
	for _, method := range []string{"SM3", "SHA256", "SHA384", "SHA512", "SHA224"} {
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
