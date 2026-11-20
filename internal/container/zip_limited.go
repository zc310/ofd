package container

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/klauspost/compress/zip"
)

// ErrEntryLimitExceeded 表示 ZIP 中声明或实际读取的条目数超过上限。
var ErrEntryLimitExceeded = errors.New("ZIP 条目数超过上限")

// OpenBytesWithEntryLimit 在 ZIP 目录解析阶段限制条目数，不创建包内路径索引。
// maxEntries 必须为正数；先检查声明数以限制底层预分配，再逐条计数以防伪造声明。
func OpenBytesWithEntryLimit(ctx context.Context, data []byte, maxEntries int) (*Package, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxEntries <= 0 {
		return nil, fmt.Errorf("%w: 条目数上限必须为正数", ErrEntryLimitExceeded)
	}
	count, err := declaredEntryCount(data)
	if err != nil {
		return nil, err
	}
	if count > uint64(maxEntries) {
		return nil, fmt.Errorf("%w %d", ErrEntryLimitExceeded, maxEntries)
	}
	var actual int
	reader, err := zip.NewReaderWithOptions(bytes.NewReader(data), int64(len(data)), zip.ReaderOptions{
		NameDecoder: func(*zip.FileHeader) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if actual >= maxEntries {
				return fmt.Errorf("%w %d", ErrEntryLimitExceeded, maxEntries)
			}
			actual++
			return nil
		},
	})
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return nil, err
	}
	pkg := newPackage(reader, nil)
	pkg.rawSize = int64(len(data))
	return pkg, nil
}

// declaredEntryCount 只读取 EOCD/ZIP64 的声明数，避免 ZIP reader 按不受限声明预分配。
// 字段布局见 ZIP APPNOTE 4.3.16、4.3.14、4.3.15；完整目录校验仍由 ZIP reader 完成。
func declaredEntryCount(data []byte) (uint64, error) {
	// 与底层 ZIP reader 一致，先搜索末尾 1 KiB，再搜索末尾 65 KiB。
	for _, size := range []int{1024, 65 * 1024} {
		start := max(0, len(data)-size)
		for offset := len(data) - 22; offset >= start; offset-- {
			header := data[offset:]
			if !bytes.HasPrefix(header, []byte("PK\x05\x06")) {
				continue
			}
			if int(binary.LittleEndian.Uint16(header[20:22])) > len(header)-22 {
				break
			}
			count := uint64(binary.LittleEndian.Uint16(header[10:12]))
			if count != 0xffff && binary.LittleEndian.Uint32(header[12:16]) != 0xffffffff &&
				binary.LittleEndian.Uint32(header[16:20]) != 0xffffffff {
				return count, nil
			}
			// 底层仅在 EOCD 前紧邻有效的单磁盘 ZIP64 locator 时使用 ZIP64。
			if offset < 20 {
				return count, nil
			}
			locator := data[offset-20 : offset]
			if !bytes.HasPrefix(locator, []byte("PK\x06\x07")) ||
				binary.LittleEndian.Uint32(locator[4:8]) != 0 || binary.LittleEndian.Uint32(locator[16:20]) != 1 {
				return count, nil
			}
			position := binary.LittleEndian.Uint64(locator[8:16])
			if len(data) < 56 || position > uint64(len(data)-56) {
				return 0, zip.ErrFormat
			}
			end64 := data[int(position) : int(position)+56]
			if !bytes.HasPrefix(end64, []byte("PK\x06\x06")) {
				return 0, zip.ErrFormat
			}
			return binary.LittleEndian.Uint64(end64[32:40]), nil
		}
		if start == 0 {
			break
		}
	}
	return 0, zip.ErrFormat
}
