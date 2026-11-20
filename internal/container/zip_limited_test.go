package container

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
)

func limitedZIPData(t *testing.T, count int) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for i := range count {
		if _, err := writer.Create(fmt.Sprintf("%d.png", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestLimitedZIPRejectsDeclaredExcessBeforeParsingDirectory(t *testing.T) {
	data := limitedZIPData(t, 3)
	// 破坏首个目录头；超限必须在底层目录解析和 File 切片预分配之前返回。
	offset := bytes.Index(data, []byte("PK\x01\x02"))
	data[offset] = 0
	pkg, err := OpenBytesWithEntryLimit(t.Context(), data, 2)
	if pkg != nil || !errors.Is(err, ErrEntryLimitExceeded) {
		t.Fatalf("声明超限错误 = %v，包 = %v", err, pkg)
	}
}

func TestLimitedZIPRejectsActualExcessDespiteUnderstatedCount(t *testing.T) {
	data := limitedZIPData(t, 4)
	end := data[len(data)-22:]
	binary.LittleEndian.PutUint16(end[8:10], 1)
	binary.LittleEndian.PutUint16(end[10:12], 1)
	pkg, err := OpenBytesWithEntryLimit(t.Context(), data, 2)
	if pkg != nil || !errors.Is(err, ErrEntryLimitExceeded) {
		t.Fatalf("实际超限错误 = %v，包 = %v", err, pkg)
	}
}

func TestLimitedZIPAcceptsExactLimitWithoutBuildingLookupIndex(t *testing.T) {
	pkg, err := OpenBytesWithEntryLimit(t.Context(), limitedZIPData(t, 3), 3)
	if err != nil {
		t.Fatal(err)
	}
	defer pkg.Close()
	var seen int
	if err := pkg.WalkEntries(func(entry Entry) bool {
		seen++
		reader, err := pkg.OpenEntry(entry)
		if err != nil {
			t.Error(err)
			return false
		}
		_ = reader.Close()
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if seen != 3 || pkg.fileMap != nil || pkg.entries != nil {
		t.Fatal("受限遍历不应创建完整路径索引或元数据快照")
	}
}

func limitedZIP64Data(t *testing.T, count int, declared uint64) []byte {
	t.Helper()
	data := limitedZIPData(t, count)
	end := append([]byte(nil), data[len(data)-22:]...)
	data = data[:len(data)-22]
	end64 := make([]byte, 56)
	copy(end64, "PK\x06\x06")
	binary.LittleEndian.PutUint64(end64[4:12], 44)
	binary.LittleEndian.PutUint64(end64[24:32], declared)
	binary.LittleEndian.PutUint64(end64[32:40], declared)
	binary.LittleEndian.PutUint64(end64[40:48], uint64(binary.LittleEndian.Uint32(end[12:16])))
	binary.LittleEndian.PutUint64(end64[48:56], uint64(binary.LittleEndian.Uint32(end[16:20])))
	locator := make([]byte, 20)
	copy(locator, "PK\x06\x07")
	binary.LittleEndian.PutUint64(locator[8:16], uint64(len(data)))
	binary.LittleEndian.PutUint32(locator[16:20], 1)
	binary.LittleEndian.PutUint16(end[8:10], 0xffff)
	binary.LittleEndian.PutUint16(end[10:12], 0xffff)
	data = append(data, end64...)
	data = append(data, locator...)
	return append(data, end...)
}

func TestLimitedZIP64ChecksDeclaredAndActualCounts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		actual   int
		declared uint64
		maximum  int
		ok       bool
	}{
		{"边界允许", 2, 2, 2, true},
		{"声明超限", 3, 3, 2, false},
		{"声明不足", 3, 1, 2, false},
		{"声明溢出", 2, ^uint64(0), 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg, err := OpenBytesWithEntryLimit(t.Context(), limitedZIP64Data(t, tc.actual, tc.declared), tc.maximum)
			if tc.ok {
				if err != nil {
					t.Fatal(err)
				}
				_ = pkg.Close()
			} else if pkg != nil || !errors.Is(err, ErrEntryLimitExceeded) {
				t.Fatalf("ZIP64 超限错误 = %v", err)
			}
		})
	}
}

func TestLimitedZIPHandlesCommentsAndCancellation(t *testing.T) {
	data := limitedZIPData(t, 1)
	comment := bytes.Repeat([]byte("c"), 65000)
	binary.LittleEndian.PutUint16(data[len(data)-2:], uint16(len(comment)))
	data = append(data, comment...)
	pkg, err := OpenBytesWithEntryLimit(t.Context(), data, 1)
	if err != nil {
		t.Fatal(err)
	}
	_ = pkg.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := OpenBytesWithEntryLimit(ctx, data, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误 = %v", err)
	}
}

func TestLimitedZIPRejectsMalformedEndRecordsWithoutPanicking(t *testing.T) {
	badOffset := limitedZIP64Data(t, 1, 1)
	binary.LittleEndian.PutUint64(badOffset[len(badOffset)-34:len(badOffset)-26], ^uint64(0))
	badSignature := limitedZIP64Data(t, 1, 1)
	offset := bytes.Index(badSignature, []byte("PK\x06\x06"))
	badSignature[offset] = 0
	truncatedComment := limitedZIPData(t, 1)
	binary.LittleEndian.PutUint16(truncatedComment[len(truncatedComment)-2:], 20)
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"输入为空", nil},
		{"输入太短", []byte("PK")},
		{"ZIP64 定位越界", badOffset},
		{"ZIP64 签名错误", badSignature},
		{"注释截断", truncatedComment},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg, err := OpenBytesWithEntryLimit(t.Context(), tc.data, 2)
			if pkg != nil || err == nil {
				t.Fatalf("畸形归档未被拒绝: %v", err)
			}
		})
	}
}
