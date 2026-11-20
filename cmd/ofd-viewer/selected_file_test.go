package main

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestReadSelectedFileBoundedAndTransient(t *testing.T) {
	file, err := readSelectedFile("../文档.ofd", strings.NewReader("abcd"), 4)
	if err != nil || file.name != "文档.ofd" || string(file.data) != "abcd" || file.data == nil {
		t.Fatalf("读取结果不符合预期: %+v, %v", file, err)
	}
	if _, err := readSelectedFile("文档.ofd", strings.NewReader("abcde"), 4); err == nil {
		t.Fatal("超出限制的文档应被拒绝")
	}
	if _, err := readSelectedFile("文档.ofd", failingReader{}, 4); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("读取错误应保留: %v", err)
	}
}
