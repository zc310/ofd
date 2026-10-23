package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/zc310/ofd/pkg/crypt"
)

func TestRunEncryptedOFDEmitsSameLockThumbnailForEveryScope(t *testing.T) {
	plain := writeTestOFD(t, 200, 100)
	var expected []byte
	for _, test := range []struct {
		name    string
		options crypt.EncryptOptions
	}{
		{"whole-document", crypt.EncryptOptions{}},
		{"plaintext-root", crypt.EncryptOptions{KeepRoot: true}},
		{"second-page-only", crypt.EncryptOptions{Entries: []string{"Doc_0/Pages/Second.xml"}}},
		{"plaintext-map", crypt.EncryptOptions{PlaintextMap: true}},
		{"independent-keys", crypt.EncryptOptions{IndependentKeys: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.options.Recipients = []crypt.EncryptRecipient{{UserName: "User", Password: []byte("123456")}}
			var encrypted bytes.Buffer
			if _, err := crypt.Encrypt(t.Context(), plain, &encrypted, test.options); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			input, output := filepath.Join(dir, "encrypted.OFD"), filepath.Join(dir, "thumbnail")
			if err := os.WriteFile(input, encrypted.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			if err := run(t.Context(), []string{input, output, "160"}); err != nil {
				t.Fatal(err)
			}
			img := readThumbnail(t, output)
			if img.Bounds().Dx() != 128 || img.Bounds().Dy() != 160 {
				t.Fatalf("加密缩略图尺寸 = %v，期望 128×160", img.Bounds())
			}
			// 锁体为金黄色，锁孔为深色；不能返回首页的纯红页面或空白图。
			r, g, b, a := img.At(40, 120).RGBA()
			if r < 0xe000 || g < 0x8000 || b > 0x5000 || a != 0xffff {
				t.Fatalf("缺少金色锁体: %d,%d,%d,%d", r, g, b, a)
			}
			r, g, b, a = img.At(64, 112).RGBA()
			if r > 0x6000 || g > 0x6000 || b > 0x6000 || a != 0xffff {
				t.Fatalf("缺少深色锁孔: %d,%d,%d,%d", r, g, b, a)
			}
			data, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if expected == nil {
				expected = data
			} else if !bytes.Equal(data, expected) {
				t.Fatal("不同加密范围没有使用相同的固定缩略图")
			}
			after, err := os.ReadFile(input)
			if err != nil || !bytes.Equal(after, encrypted.Bytes()) {
				t.Fatalf("生成缩略图修改了原文档: %v", err)
			}
			assertNoTemporaryThumbnails(t, dir)
		})
	}
}

func TestEncryptedThumbnailHonorsRequestedSize(t *testing.T) {
	input := writeTestArchive(t, "encrypted.ofd", zipTestEntry{"Encryptions.xml", []byte("unreadable metadata")})
	for _, size := range []int{1, 16, 32, 128, 256, maxThumbnailSize} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "thumbnail.png")
			if err := run(t.Context(), []string{input, output, strconv.Itoa(size)}); err != nil {
				t.Fatal(err)
			}
			bounds := readThumbnail(t, output).Bounds()
			if bounds.Dx() < 1 || bounds.Dx() > size || bounds.Dy() != size {
				t.Fatalf("请求长边 %d，实际 %v", size, bounds)
			}
		})
	}
}

func TestUnencryptedBrokenOFDDoesNotBecomeEncryptedThumbnail(t *testing.T) {
	for _, test := range []struct {
		name    string
		entries []zipTestEntry
	}{
		{"invalid-root", []zipTestEntry{{"OFD.xml", []byte("not XML")}}},
		{"nested-encryption-marker", []zipTestEntry{{"Other/Encryptions.xml", []byte("not a root marker")}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := writeTestArchive(t, "broken.ofd", test.entries...)
			output := filepath.Join(t.TempDir(), "thumbnail.png")
			if err := os.WriteFile(output, []byte("existing thumbnail"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := run(context.Background(), []string{input, output, "128"}); err == nil {
				t.Fatal("未加密的损坏文件被替换为加密缩略图")
			}
			data, err := os.ReadFile(output)
			if err != nil || string(data) != "existing thumbnail" {
				t.Fatalf("失败后修改了已有缩略图: %v", err)
			}
			assertNoTemporaryThumbnails(t, filepath.Dir(output))
		})
	}
}
