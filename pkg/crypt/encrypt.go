package crypt

import (
	"context"
	"fmt"
	"io"

	protocol "github.com/zc310/ofd/internal/crypt"
)

// Encrypt 按 GM/T 0099-2020 写入新操作，支持独立条目密钥及页面范围。
// Append 非 nil 时先用旧凭据预检，再在现有线性加密链外追加一层。
// 输入/输出所有权及失败处理与 Decrypt 一致，文档条目不重新序列化。
func Encrypt(ctx context.Context, input any, w io.Writer, opt EncryptOptions) (*EncryptReport, error) {
	opt, err := protocol.NormalizeEncryptOptions(opt)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if w == nil {
		return nil, fmt.Errorf("加密输出为空")
	}
	pkg, closeInput, err := openInput(ctx, input, opt.Limits.MaxInputBytes)
	if err != nil {
		return nil, err
	}
	defer closeInput()
	var report *EncryptReport
	err = writeZIP(ctx, w, opt.Limits, func(emit func(string, []byte) error) error {
		report, err = protocol.EncryptPackage(ctx, pkg, opt, emit)
		return err
	})
	if err != nil {
		return nil, err
	}
	return report, nil
}
