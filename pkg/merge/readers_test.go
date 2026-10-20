package merge

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type countedReaderAt struct {
	reader io.ReaderAt
	calls  int
}

func (r *countedReaderAt) ReadAt(p []byte, off int64) (int, error) {
	r.calls++
	return r.reader.ReadAt(p, off)
}

func TestPagesChecksReaderLimitsBeforeReadingAnyInput(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sizes   []int64
		limits  PageLimits
		message string
	}{
		{"single input", []int64{1 << 20}, PageLimits{MaxInputBytes: 8}, "单输入上限"},
		{"total input", []int64{16, 16}, PageLimits{MaxTotalBytes: 24}, "输入总大小"},
		{"total overflow", []int64{1<<63 - 1, 1}, PageLimits{MaxInputBytes: 1<<63 - 1, MaxTotalBytes: 1<<63 - 1}, "输入总大小"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var readers []*countedReaderAt
			var sources []Source
			for _, size := range tc.sizes {
				r := &countedReaderAt{reader: bytes.NewReader(nil)}
				readers = append(readers, r)
				sources = append(sources, Source{Reader: r, Size: size})
			}
			var out bytes.Buffer
			err := Pages(sources, &out, PageOptions{Limits: tc.limits})
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("应返回大小限制错误，实际 %v", err)
			}
			for index, r := range readers {
				if r.calls != 0 {
					t.Errorf("拒绝前读取了输入 %d，共 %d 次", index, r.calls)
				}
			}
		})
	}
}

func TestPagesReaderMatchesBytes(t *testing.T) {
	data := smallDocument(t, "reader")
	var fromBytes, fromReader bytes.Buffer
	options := PageOptions{Deterministic: true}
	if err := Pages([]Source{{Data: data}}, &fromBytes, options); err != nil {
		t.Fatal(err)
	}
	if err := Pages([]Source{{Reader: bytes.NewReader(data), Size: int64(len(data))}}, &fromReader, options); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fromBytes.Bytes(), fromReader.Bytes()) {
		t.Fatal("Reader 与 Data 输入的确定性合并结果不同")
	}
}

type failingReaderAt struct{ err error }

func (r failingReaderAt) ReadAt([]byte, int64) (int, error) { return 0, r.err }

func TestPagesPreservesReaderErrors(t *testing.T) {
	want := errors.New("reader failure")
	err := Pages([]Source{{Reader: failingReaderAt{err: want}, Size: 64}}, &bytes.Buffer{}, PageOptions{})
	if !errors.Is(err, want) {
		t.Fatalf("读取错误丢失: %v", err)
	}
}
