package core

import (
	"fmt"
	"io"
	"strings"
)

// PackageSource 提供由使用方持有的逻辑 OFD 包租约。
// 关闭返回的包不得关闭同一数据源的其他租约。
type PackageSource interface{ OFDPackage() (*Package, error) }

type virtualSource struct {
	entries []Entry
	open    func(string) (io.ReadCloser, error)
}

// NewVirtual 创建不可变的包访问视图。元数据及其 Virtual 标记由调用方提供，
// 每次打开条目均返回新的读取器。closer 释放当前视图的租约，
// 不影响同一数据源的其他视图。
func NewVirtual(entries []Entry, open func(string) (io.ReadCloser, error), closer io.Closer) (*Package, error) {
	if open == nil {
		return nil, fmt.Errorf("虚拟包读取入口为空")
	}
	copyEntries := append([]Entry(nil), entries...)
	seen := map[string]bool{}
	for i := range copyEntries {
		e := &copyEntries[i]
		if e.IsDir {
			e.Path = strings.TrimSuffix(e.Path, "/")
		}
		if err := ValidateEntryName(e.Path); err != nil {
			return nil, err
		}
		lower := strings.ToLower(e.Path)
		if seen[lower] {
			return nil, fmt.Errorf("虚拟包路径重复或大小写歧义: %s", e.Path)
		}
		seen[lower] = true
		if e.Name == "" {
			e.Name = e.Path
		}
	}
	return &Package{virtual: &virtualSource{copyEntries, open}, closer: closer, token: &packageToken{}}, nil
}
