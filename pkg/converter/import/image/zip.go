package image

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/contextio"
)

var errInvalidImageArchive = errors.New("无效的 ZIP 图片包")

func (d *imageDocument) addZIP(ctx context.Context, data []byte) error {
	pkg, err := container.OpenBytesWithEntryLimit(ctx, data, d.opts.MaxArchiveEntries)
	if err != nil {
		return fmt.Errorf("读取 ZIP 图片包失败: %w", err)
	}
	defer func() { _ = pkg.Close() }()
	entries, err := imageEntries(ctx, pkg, d.opts)
	if err != nil {
		return err
	}
	remaining := d.opts.MaxTotalBytes
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := readImageEntry(ctx, pkg, entry, d.opts, &remaining)
		if err != nil {
			return fmt.Errorf("ZIP 图片 %q: %w", entry.Name, err)
		}
		if err := d.addImage(ctx, data); err != nil {
			return fmt.Errorf("ZIP 图片 %q: %w", entry.Name, err)
		}
	}
	return nil
}

func imageEntries(ctx context.Context, pkg *container.Package, opts Options) ([]container.Entry, error) {
	// 按需遍历，不为整个包创建路径索引和多份元数据快照。
	var entries []container.Entry
	var scanErr error
	if err := pkg.WalkEntries(func(entry container.Entry) bool {
		if err := ctx.Err(); err != nil {
			scanErr = err
			return false
		}
		if len(entries) >= opts.MaxArchiveEntries {
			scanErr = fmt.Errorf("%w: ZIP 条目数超过上限 %d", errImageLimit, opts.MaxArchiveEntries)
			return false
		}
		entries = append(entries, entry)
		return true
	}); err != nil {
		return nil, err
	}
	if scanErr != nil {
		return nil, scanErr
	}
	seen := make(map[string]bool, len(entries))
	images := make([]container.Entry, 0)
	remaining := uint64(opts.MaxTotalBytes)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name
		if entry.IsDir {
			name = strings.TrimSuffix(name, "/")
		}
		if err := container.ValidateEntryName(name); err != nil {
			return nil, fmt.Errorf("ZIP 条目 %q 路径不安全: %w", entry.Name, err)
		}
		if strings.Contains(name, ":") {
			return nil, fmt.Errorf("%w: ZIP 条目 %q 路径不能包含冒号", errInvalidImageArchive, entry.Name)
		}
		if seen[name] {
			return nil, fmt.Errorf("%w: ZIP 存在重复路径 %q", errInvalidImageArchive, name)
		}
		seen[name] = true
		if !entry.IsDir && !entry.Mode.IsRegular() {
			return nil, fmt.Errorf("%w: ZIP 条目 %q 不是普通文件", errInvalidImageArchive, entry.Name)
		}
		// 忽略项也计入声明预算；实际解压时另行检查，不信任 ZIP 大小声明。
		if entry.UncompressedSize > uint64(opts.MaxEntryBytes) {
			return nil, fmt.Errorf("%w: ZIP 条目 %q 解压后超过单条上限 %d 字节", errImageLimit, entry.Name, opts.MaxEntryBytes)
		}
		if entry.UncompressedSize > remaining {
			return nil, fmt.Errorf("%w: ZIP 解压总大小超过上限 %d 字节", errImageLimit, opts.MaxTotalBytes)
		}
		remaining -= entry.UncompressedSize
		if entry.IsDir || ignoredImagePath(name) {
			continue
		}
		switch strings.ToLower(path.Ext(name)) {
		case ".png", ".jpg", ".jpeg", ".tif", ".tiff":
			images = append(images, entry)
		}
	}
	if len(images) == 0 {
		return nil, fmt.Errorf("%w: ZIP 中没有可用的 PNG、JPEG 或 TIFF 图片", errInvalidImageArchive)
	}
	sort.Slice(images, func(i, j int) bool { return naturalPathLess(images[i].Name, images[j].Name) })
	return images, nil
}

func ignoredImagePath(name string) bool {
	for component := range strings.SplitSeq(name, "/") {
		if strings.HasPrefix(component, ".") || component == "__MACOSX" {
			return true
		}
	}
	return false
}

func readImageEntry(ctx context.Context, pkg *container.Package, entry container.Entry, opts Options, remaining *int64) ([]byte, error) {
	reader, err := pkg.OpenEntry(entry)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	var output bytes.Buffer
	limited := container.NewLimitWriter(&output, entry.Name, opts.MaxEntryBytes, opts.MaxTotalBytes, remaining)
	if _, err := io.Copy(limited, contextio.Reader(ctx, reader)); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// naturalPathLess 按 ASCII 数字段的数值比较，其余按 UTF-8 字节比较，区分大小写。
// 数值相同时继续比较后续片段，最终以原始路径打破平局，不依赖归档条目顺序。
func naturalPathLess(a, b string) bool {
	originalA, originalB := a, b
	for len(a) > 0 && len(b) > 0 {
		an, bn := digitPrefix(a), digitPrefix(b)
		if an > 0 && bn > 0 {
			ad, bd := strings.TrimLeft(a[:an], "0"), strings.TrimLeft(b[:bn], "0")
			if len(ad) != len(bd) {
				return len(ad) < len(bd)
			}
			if ad != bd {
				return ad < bd
			}
			a, b = a[an:], b[bn:]
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return originalA < originalB
}

func digitPrefix(value string) int {
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return i
		}
	}
	return len(value)
}
