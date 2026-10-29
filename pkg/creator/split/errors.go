package split

import (
	"errors"
	"fmt"
)

// ErrInvalid 表示输入、选择、资源或保护关系不满足拆分要求。
// 具体原因包含在错误文本中；取消和底层读写错误保留原始错误链。
var ErrInvalid = errors.New("无法拆分 OFD")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}
