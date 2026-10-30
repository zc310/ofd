package reader

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/zc310/ofd/internal/parser"
)

func TestOpenAcceptsBytesFileAndReaderWithOptionalConfig(t *testing.T) {
	data := pageResourcePackage(t)
	path := filepath.Join(t.TempDir(), "document.ofd")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		name string
		make func() any
	}{
		{name: "bytes", make: func() any { return data }},
		{name: "file", make: func() any { return path }},
		{name: "reader", make: func() any { return bytes.NewReader(data) }},
	} {
		for _, config := range []struct {
			name    string
			options []OpenOptions
		}{
			{name: "default"},
			{name: "configured", options: []OpenOptions{{PageCacheCapacity: 1, PageCacheBytes: 1024}}},
		} {
			t.Run(input.name+"/"+config.name, func(t *testing.T) {
				reader, err := Open(input.make(), config.options...)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := reader.Close(); err != nil {
						t.Error(err)
					}
				})
				if got := reader.PageCount(); got != 2 {
					t.Fatalf("页数 = %d，期望 2", got)
				}
				for index := range 2 {
					page, err := reader.Page(index)
					if err != nil {
						t.Fatal(err)
					}
					if page.Width != 20 || page.Height != 20 {
						t.Fatalf("页面尺寸 = %v×%v，期望 20×20", page.Width, page.Height)
					}
				}
			})
		}
	}
}

func TestOpenRejectsInvalidInputsAndOptions(t *testing.T) {
	data := pageResourcePackage(t)
	for _, test := range []struct {
		name    string
		input   any
		options []OpenOptions
		want    string
	}{
		{name: "nil", want: "数据为空"},
		{name: "typed_nil", input: []byte(nil), want: "数据为空"},
		{name: "empty_bytes", input: []byte{}, want: "数据为空"},
		{name: "unsupported", input: 42, want: "不支持的类型"},
		{name: "invalid_zip", input: []byte("invalid"), want: "解析 OFD 失败"},
		{name: "negative_capacity", input: data, options: []OpenOptions{{PageCacheCapacity: -1}}, want: "不能为负数"},
		{name: "negative_bytes", input: data, options: []OpenOptions{{PageCacheBytes: -1}}, want: "不能为负数"},
		{name: "multiple_options", input: data, options: []OpenOptions{{}, {}}, want: "最多传入一组 OpenOptions"},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader, err := Open(test.input, test.options...)
			if reader != nil || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Open = (%v, %v)，期望错误包含 %q", reader, err, test.want)
			}
		})
	}
}

func TestOpenPreservesReaderErrors(t *testing.T) {
	want := errors.New("输入读取失败")
	if _, err := Open(iotest.ErrReader(want)); !errors.Is(err, want) {
		t.Fatalf("Open 错误 = %v，期望保留 %v", err, want)
	}
}

func TestOpenRejectsOversizedFileBeforeReading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.ofd")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// 稀疏文件只设置长度，不分配超过 256 MiB 的测试内容。
	truncateErr := file.Truncate(maxInputBytes + 1)
	closeErr := file.Close()
	if truncateErr != nil {
		t.Fatal(truncateErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	if _, err := Open(path); !errors.Is(err, parser.ErrInputTooLarge) {
		t.Fatalf("Open 错误 = %v，期望拒绝超大输入", err)
	}
}
