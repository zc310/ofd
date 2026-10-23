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

// Decrypt restores a single encryption operation to a plaintext OFD ZIP. It
// supports password and certificate recipients and plaintext/encrypted maps.
// Input kinds and ownership match Inspect; w is never closed. No plaintext
// temporary files are created. Output may be partial on any error and must be
// discarded; file callers should write a temporary file and rename on success.
// The report is non-nil only after the complete output has been written.
func Decrypt(ctx context.Context, input any, w io.Writer, opt DecryptOptions) (*DecryptReport, error) {
	opt, err := protocol.NormalizeDecryptOptions(opt)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
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
	if err := completed.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
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
