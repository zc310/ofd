package models

import "github.com/zc310/ofd/pkg/spec"

// Signature 将标准签名描述与内部加载上下文分开；上下文不参与 XML 解码。
type Signature struct {
	spec.Signature

	Type     string `xml:"-"`
	Relative string `xml:"-"`
	FilePath string `xml:"-"`
}
