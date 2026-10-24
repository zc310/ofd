package crypt

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestEncryptRejectsPathsThatCannotRoundTripThroughXMLBeforeOutput(t *testing.T) {
	for _, suffixes := range [][]string{{"a\x01.bin"}, {"a\x01.bin", "a\x02.bin"}, {"a\x0b.bin"}, {"a\x1f.bin"}, {"a\ufffe.bin"}, {"a\uffff.bin"}, {"a\xff.bin"}} {
		t.Run(fmt.Sprintf("%q", suffixes), func(t *testing.T) {
			files := testPlainFiles()
			for _, suffix := range suffixes {
				files["Doc_0/Res/"+suffix] = []byte("resource bytes")
			}
			called := false
			report, err := EncryptPackage(t.Context(), testPackage(t, files), testPasswordOptions(), func(string, []byte) error { called = true; return nil })
			if !errors.Is(err, ErrInvalidMetadata) || report != nil || called {
				t.Fatalf("不能无损写入映射表的路径应在输出前拒绝: report=%+v err=%v emitted=%t", report, err, called)
			}
		})
	}
}

func TestEncryptedMappingsPreserveXMLCharactersInPaths(t *testing.T) {
	for _, plaintextMap := range []bool{false, true} {
		t.Run(fmt.Sprint(plaintextMap), func(t *testing.T) {
			files := testPlainFiles()
			for _, suffix := range []string{"a&<>'\".bin", "a\t\n\r.bin", "a\ufffd.bin", "中文😀.bin", "a\x7f.bin"} {
				files["Doc_0/Res/"+suffix] = []byte(suffix)
			}
			options := testPasswordOptions()
			options.PlaintextMap = plaintextMap
			options.IndependentKeys = true
			encrypted, _ := encryptFiles(t, files, options)
			output, _ := decryptedFiles(t, testPackage(t, encrypted), DecryptOptions{Password: []byte("secret")})
			if !reflect.DeepEqual(output, files) {
				t.Fatal("XML 转义不应替换或归一化任何原始文件名或字节")
			}
		})
	}
}
