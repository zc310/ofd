package models

import "github.com/zc310/ofd/pkg/spec"

// decodeTextCodeValue 让内部快速入口复用公开模型的文字转义及既有排版空白兼容。
func decodeTextCodeValue(text string) string { return spec.DecodeTextCodeValue(text) }
