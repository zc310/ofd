// Package config 定义 PDF 导入配置，不依赖 PDF 解析器，供 converter 与导入器共享。
package config

import (
	"errors"
	"fmt"
	"math"
)

var ErrLimitExceeded = errors.New("PDF 导入超过资源上限")
var ErrUnsupported = errors.New("PDF 导入遇到无法保留的内容")

// Limits 控制一次转换的资源预算。零值字段使用默认值，负值无效。
type Limits struct {
	// 字节预算分别作用于原始输入、单编码流、单解码流和累计解码流。
	MaxInputBytes int64
	MaxPages      int64
	// MaxObjects 同时约束对象数及 xref 可引用的编号范围。
	MaxObjects            int64
	MaxStreamBytes        int64
	MaxDecodedStreamBytes int64
	MaxTotalDecodedBytes  int64
	MaxImagePixels        int64
	MaxTotalImagePixels   int64
	MaxFontBytes          int64
	MaxOutputObjects      int64
	// MaxOperations 计入内容解析、映射展开、遮罩/网格与 Type 4 函数解析和求值。
	MaxOperations int64
	MaxDepth      int64
}

func DefaultLimits() Limits {
	return Limits{
		MaxInputBytes: 512 << 20, MaxPages: 10_000, MaxObjects: 1_000_000,
		MaxStreamBytes: 256 << 20, MaxDecodedStreamBytes: 256 << 20, MaxTotalDecodedBytes: 1 << 30,
		MaxImagePixels: 100_000_000, MaxTotalImagePixels: 2_000_000_000, MaxFontBytes: 64 << 20,
		MaxOutputObjects: 1_000_000, MaxOperations: 1_000_000_000,
		MaxDepth: 64,
	}
}

// Diagnostic 描述无法完整保留的内容。Page 从 1 开始，未知的对象编号为 0。
type Diagnostic struct {
	Code       string
	Page       int
	Object     int
	Generation int
	Resource   string
	Message    string
}

// Options 控制 PDF 导入。Strict 遇到已报告的内容缺失或近似转换即失败，
// 不代表完整实现 PDF 规范。OnDiagnostic 在转换协程中同步执行；取消该转换的
// context 会原样返回 ctx.Err()。并行转换共用回调时，调用方应自行同步共享数据。
type Options struct {
	Limits       Limits
	Strict       bool
	OnDiagnostic func(Diagnostic)
}

// LimitError 可通过 errors.Is(err, ErrLimitExceeded) 和 errors.As 识别。
// Actual 是已观察到的用量；解码提前终止时为超过上限的最小已知用量。
type LimitError struct {
	Resource string
	Limit    int64
	Actual   int64
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("%s: %s 用量 %d，上限 %d", ErrLimitExceeded, e.Resource, e.Actual, e.Limit)
}
func (e *LimitError) Unwrap() error { return ErrLimitExceeded }

// Normalize 校验限制并补齐零值字段，返回独立的配置值。
func (o Options) Normalize() (Options, error) {
	d := DefaultLimits()
	for _, field := range []struct {
		name     string
		value    *int64
		fallback int64
	}{
		{"MaxInputBytes", &o.Limits.MaxInputBytes, d.MaxInputBytes},
		{"MaxPages", &o.Limits.MaxPages, d.MaxPages},
		{"MaxObjects", &o.Limits.MaxObjects, d.MaxObjects},
		{"MaxStreamBytes", &o.Limits.MaxStreamBytes, d.MaxStreamBytes},
		{"MaxDecodedStreamBytes", &o.Limits.MaxDecodedStreamBytes, d.MaxDecodedStreamBytes},
		{"MaxTotalDecodedBytes", &o.Limits.MaxTotalDecodedBytes, d.MaxTotalDecodedBytes},
		{"MaxImagePixels", &o.Limits.MaxImagePixels, d.MaxImagePixels},
		{"MaxTotalImagePixels", &o.Limits.MaxTotalImagePixels, d.MaxTotalImagePixels},
		{"MaxFontBytes", &o.Limits.MaxFontBytes, d.MaxFontBytes},
		{"MaxOutputObjects", &o.Limits.MaxOutputObjects, d.MaxOutputObjects},
		{"MaxOperations", &o.Limits.MaxOperations, d.MaxOperations},
		{"MaxDepth", &o.Limits.MaxDepth, d.MaxDepth},
	} {
		if *field.value < 0 || *field.value == math.MaxInt64 {
			return o, fmt.Errorf("PDF %s 必须为正数或 0（使用默认值），且小于 MaxInt64", field.name)
		}
		if *field.value == 0 {
			*field.value = field.fallback
		}
	}
	if o.Limits.MaxPages >= int64(int(^uint(0)>>1)) || o.Limits.MaxObjects >= int64(int(^uint(0)>>1)) || o.Limits.MaxDepth >= int64(int(^uint(0)>>1)) {
		return o, errors.New("PDF 页数、对象数或递归深度上限超出当前平台范围")
	}
	return o, nil
}
