package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialRegularFilesPreserveBytesAndLimits(t *testing.T) {
	for _, data := range [][]byte{{}, []byte("口令\n"), []byte("secret\x00bytes")} {
		t.Run(string(data), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "password")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := readCredential(t.Context(), path, nil, int64(len(data)))
			if err != nil || got == nil || !bytes.Equal(got, data) {
				t.Fatalf("普通凭据文件应保留原始字节，包括空口令: %q, %v", got, err)
			}
			if len(data) > 0 {
				got, err := readCredential(t.Context(), path, nil, int64(len(data)-1))
				if err == nil || got != nil {
					t.Fatalf("超过大小上限时不应返回部分口令: %q, %v", got, err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if got, err := readCredential(ctx, path, nil, 64<<10); !errors.Is(err, context.Canceled) || got != nil {
				t.Fatalf("读取前应保留取消错误: %q, %v", got, err)
			}
		})
	}
	if got, err := readCredential(t.Context(), t.TempDir(), nil, 64<<10); err == nil || got != nil {
		t.Fatalf("目录不应作为凭据文件: %q, %v", got, err)
	}
	if got, err := readCredential(t.Context(), filepath.Join(t.TempDir(), "missing"), nil, 64<<10); !errors.Is(err, os.ErrNotExist) || got != nil {
		t.Fatalf("缺失文件应保留文件系统错误: %q, %v", got, err)
	}
}
