package parser

import "errors"

// 稳定的解析器错误类别。调用方应使用 errors.Is 判断错误类型，
// 不要匹配可能被本地化的错误文本。
var (
	ErrDocumentClosed          = errors.New("parser: document closed")
	ErrPageIndexOutOfRange     = errors.New("parser: page index out of range")
	ErrDocumentScopeOutOfRange = errors.New("parser: document scope index out of range")
	ErrInputTooLarge           = errors.New("parser: input too large")
	ErrVersionNotFound         = errors.New("parser: version not found")
)

type categorizedError struct {
	message string
	target  error
}

func (e *categorizedError) Error() string { return e.message }
func (e *categorizedError) Is(target error) bool {
	return target == e.target
}

func categorized(target error, message string) error {
	return &categorizedError{message: message, target: target}
}
