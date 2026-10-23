package crypt

import (
	"context"

	protocol "github.com/zc310/ofd/internal/crypt"
)

// Open 创建只读逻辑视图，并保留密文和恢复出的密钥。它不会创建或缓存明文 ZIP。
// 打开时会建立逻辑索引并检查填充；之后读取资源时会重新解密。
// 使用后应关闭视图。将已完整解锁的视图传给使用方时，会为其创建独立租约，
// 因此关闭转换器或解析器不会关闭视图所有者。AllowPartial 只公开已知且已解锁的条目，
// 不会回退到占位内容。
func Open(ctx context.Context, input any, opt OpenOptions) (*View, error) {
	configured, err := protocol.NormalizeDecryptOptions(opt.DecryptOptions)
	if err != nil {
		return nil, err
	}
	opt.DecryptOptions = configured
	pkg, closeInput, err := openInput(ctx, input, configured.Limits.MaxInputBytes)
	if err != nil {
		return nil, err
	}
	view, err := protocol.OpenView(ctx, pkg, opt, func() error { closeInput(); return nil })
	if err != nil {
		closeInput()
		return nil, err
	}
	return view, nil
}
