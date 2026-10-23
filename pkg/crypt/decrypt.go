package crypt

import (
	"context"
	"fmt"
	"io"

	"github.com/klauspost/compress/zip"
	protocol "github.com/zc310/ofd/internal/crypt"
	"github.com/zc310/ofd/internal/entrywriter"
	"github.com/zc310/ofd/pkg/creator"
)

// Decrypt 将单个加密操作恢复为明文 OFD ZIP，支持口令和证书接收者以及明文和加密映射表。
// 输入类型及资源所有权与 Inspect 一致；不会关闭 w，也不会创建明文临时文件。发生错误时，
// 输出可能不完整，必须丢弃；写文件的调用方应先写入临时文件，成功后再重命名。
// 只有完整写出结果后才会返回非 nil 的报告。
func Decrypt(ctx context.Context, input any, w io.Writer, opt DecryptOptions) (*DecryptReport, error) {
	opt, err := protocol.NormalizeDecryptOptions(opt)
	if err != nil {
		return nil, err
	}
	err = ctx.Err()
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, fmt.Errorf("解密输出为空")
	}
	pkg, closeInput, err := openInput(ctx, input, opt.Limits.MaxInputBytes)
	if err != nil {
		return nil, err
	}
	defer closeInput()
	var report *DecryptReport
	err = writeZIP(ctx, w, opt.Limits, func(emit func(string, []byte) error) error {
		report, err = protocol.DecryptPackage(ctx, pkg, opt, emit)
		return err
	})
	if err != nil {
		return nil, err
	}
	return report, nil
}

func writeZIP(ctx context.Context, w io.Writer, limits Limits, produce func(func(string, []byte) error) error) error {
	var archive *zip.Writer
	var entries *entrywriter.Writer
	defer func() {
		if archive != nil {
			_ = archive.Close()
		}
	}()
	err := produce(func(name string, data []byte) error {
		if entries == nil {
			archive = zip.NewWriter(contextWriter{ctx, w})
			entries = entrywriter.New(archive, entrywriter.Config{Limits: creator.Limits{MaxEntries: limits.MaxEntries, MaxEntryBytes: limits.MaxEntryBytes, MaxTotalBytes: limits.MaxTotalBytes}})
		}
		return entries.Write(name, data)
	})
	if err != nil {
		return err
	}
	if archive == nil {
		return fmt.Errorf("未生成 OFD 输出")
	}
	completed := archive
	archive = nil
	err = completed.Close()
	if err != nil {
		return err
	}
	err = ctx.Err()
	if err != nil {
		return err
	}
	return nil
}

type contextWriter struct {
	ctx    context.Context
	writer io.Writer
}

func (w contextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.writer.Write(p)
}
