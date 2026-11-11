package main

import (
	"context"
	"io"
)

// SAF 目标可能来自云端 Provider，不支持 rename；分块写入并保留取消错误。
func writeAndroidDocument(ctx context.Context, output io.Writer, data []byte) error {
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		size := min(len(data), 64<<10)
		n, err := output.Write(data[:size])
		if err != nil {
			return err
		}
		if n != size {
			return io.ErrShortWrite
		}
		data = data[size:]
	}
	return ctx.Err()
}
