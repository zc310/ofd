package models

import "github.com/zc310/ofd/pkg/spec"

// ParsePathData 在分批迁移期间复用公开模型的 OFD 紧缩路径解码。
func ParsePathData(data string) (SVGPath, error) { return spec.ParsePathData(data) }
