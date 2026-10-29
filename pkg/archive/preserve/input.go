package preserve

import (
	"fmt"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/pkg/creator/replace"
)

// inputPackage 在任何 XML 解压前检查整个输入的预算，并限制后续每次条目读取。
type inputPackage struct {
	*container.Package

	limits replace.Limits
}

func inputLimits(limits replace.Limits) (replace.Limits, error) {
	if err := container.ValidateLimits(limits); err != nil {
		return replace.Limits{}, err
	}
	if limits.MaxEntries == 0 {
		limits.MaxEntries = container.DefaultMaxEntries
	}
	if limits.MaxEntryBytes == 0 {
		limits.MaxEntryBytes = container.DefaultMaxEntryBytes
	}
	if limits.MaxTotalBytes == 0 {
		limits.MaxTotalBytes = container.DefaultMaxTotalBytes
	}
	return limits, nil
}

func checkInput(pkg *container.Package, limits replace.Limits) (*inputPackage, error) {
	count := 0
	remaining := uint64(limits.MaxTotalBytes)
	seen := map[string]bool{}
	var checkErr error
	err := pkg.WalkEntries(func(entry container.Entry) bool {
		if entry.IsDir {
			return true
		}
		if err := container.ValidateEntryName(entry.Path); err != nil {
			checkErr = err
			return false
		}
		if seen[entry.Path] {
			checkErr = fmt.Errorf("输入条目路径重复: %s", entry.Path)
			return false
		}
		seen[entry.Path] = true
		count++
		if count > limits.MaxEntries {
			checkErr = fmt.Errorf("输入条目数超过上限 %d", limits.MaxEntries)
		} else if entry.UncompressedSize > uint64(limits.MaxEntryBytes) {
			checkErr = fmt.Errorf("输入条目 %s 解压大小超过单条上限 %d 字节", entry.Path, limits.MaxEntryBytes)
		} else if entry.UncompressedSize > remaining {
			checkErr = fmt.Errorf("输入解压总大小超过上限 %d 字节", limits.MaxTotalBytes)
		} else {
			remaining -= entry.UncompressedSize
		}
		return checkErr == nil
	})
	if err != nil {
		return nil, err
	}
	if checkErr != nil {
		return nil, checkErr
	}
	return &inputPackage{Package: pkg, limits: limits}, nil
}

func (p *inputPackage) Read(name string) ([]byte, error) {
	return p.Package.ReadLimit(name, p.limits.MaxEntryBytes)
}
