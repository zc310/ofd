package container

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"strconv"
	"testing"

	"github.com/klauspost/compress/zip"
)

func TestReadAllHintKeepsContentsForSmallerLargerAndUntrustedHints(t *testing.T) {
	for _, size := range []int{0, 1, 511, 512, 4096, maxReadInitialCapacity, maxReadInitialCapacity + 127} {
		content := bytes.Repeat([]byte{'x'}, size)
		for _, hint := range []uint64{0, 1, uint64(size), uint64(size + 100), ^uint64(0)} {
			got, err := readAllHint(bytes.NewReader(content), hint)
			if err != nil || !bytes.Equal(got, content) {
				t.Fatalf("大小 %d、提示 %d 的内容改变：%v", size, hint, err)
			}
			if size < maxReadInitialCapacity && cap(got) > maxReadInitialCapacity+1 {
				t.Fatal("不可信大小触发了超过上限的初始分配")
			}
		}
	}
}

type hintedTailReader struct {
	data []byte
	err  error
	eof  bool
}

func (r *hintedTailReader) Read(p []byte) (int, error) {
	if len(r.data) != 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	r.eof = true
	return 0, r.err
}

func TestReadAllHintReadsPastExactSizeAndPreservesFinalErrors(t *testing.T) {
	wantErr := errors.New("最后一次读取校验失败")
	for _, final := range []error{io.EOF, wantErr} {
		reader := &hintedTailReader{data: []byte("content"), err: final}
		got, err := readAllHint(reader, 7)
		if !reader.eof || string(got) != "content" {
			t.Fatal("读满大小提示后提前返回，没有触发 EOF 校验")
		}
		if errors.Is(final, io.EOF) && err != nil || !errors.Is(final, io.EOF) && !errors.Is(err, final) {
			t.Fatalf("最后一次读取的错误改变：%v", err)
		}
	}
}

func TestPackageHintedReadKeepsLimitsAndIndependentResults(t *testing.T) {
	content := bytes.Repeat([]byte("资源读取"), 1024)
	p, err := OpenBytes(newTestZip(t, map[string][]byte{"data": content, "empty": {}}))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for _, limit := range []int64{0, int64(len(content)), int64(len(content) + 1), maxInt64} {
		got, err := p.ReadLimit("DATA", limit)
		if err != nil || !bytes.Equal(got, content) {
			t.Fatalf("限制 %d 下读取内容错误：%v", limit, err)
		}
		if cap(got) > len(content)+1 {
			t.Fatal("没有利用 ZIP 大小提示限制正常资源的扩容")
		}
		got[0] ^= 255
		again, err := p.Read("data")
		if err != nil || !bytes.Equal(again, content) {
			t.Fatal("重复读取没有返回独立可写的字节切片")
		}
	}
	if _, err := p.ReadLimit("data", int64(len(content)-1)); !errors.Is(err, ErrReadLimitExceeded) {
		t.Fatalf("声明大小没有提前限流：%v", err)
	}
	if got, err := p.Read("empty"); err != nil || len(got) != 0 {
		t.Fatalf("空条目读取错误：%v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Read("data"); !errors.Is(err, ErrPackageClosed) {
		t.Fatalf("关闭后的读取错误改变：%v", err)
	}
}

func TestPackageHintedReadKeepsCRCAndDeclaredSizeValidation(t *testing.T) {
	for _, method := range []uint16{zip.Store, zip.Deflate} {
		t.Run(strconv.FormatUint(uint64(method), 10), func(t *testing.T) {
			var archive bytes.Buffer
			writer := zip.NewWriter(&archive)
			file, err := writer.CreateHeader(&zip.FileHeader{Name: "data", Method: method})
			if err != nil {
				t.Fatal(err)
			}
			content := []byte("精确读满长度后仍须检查 CRC 和描述符")
			if _, err := file.Write(content); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			base := archive.Bytes()
			central := bytes.Index(base, []byte{'P', 'K', 1, 2})
			descriptor := bytes.Index(base, []byte{'P', 'K', 7, 8})
			if central < 0 || descriptor < 0 {
				t.Fatal("样例缺少中央目录或数据描述符")
			}
			for _, tc := range []struct {
				name   string
				change func([]byte)
				want   error
			}{
				{"CRC", func(b []byte) { b[central+16] ^= 1 }, zip.ErrChecksum},
				{"描述符CRC", func(b []byte) { b[descriptor+4] ^= 1 }, zip.ErrChecksum},
				{"声明偏大", func(b []byte) { binary.LittleEndian.PutUint32(b[central+24:], uint32(len(content)+1)) }, io.ErrUnexpectedEOF},
				{"声明偏小", func(b []byte) { binary.LittleEndian.PutUint32(b[central+24:], uint32(len(content)-1)) }, zip.ErrFormat},
				{"声明极大", func(b []byte) { binary.LittleEndian.PutUint32(b[central+24:], 1<<30) }, io.ErrUnexpectedEOF},
			} {
				t.Run(tc.name, func(t *testing.T) {
					data := bytes.Clone(base)
					tc.change(data)
					p, err := OpenBytes(data)
					if err != nil {
						t.Fatal(err)
					}
					defer p.Close()
					if _, err := p.Read("data"); !errors.Is(err, tc.want) {
						t.Fatalf("读取错误 %v，期望 %v", err, tc.want)
					}
				})
			}
		})
	}
}

type hintedReadCloser struct {
	io.Reader

	err    error
	closed bool
}

func (r *hintedReadCloser) Close() error {
	r.closed = true
	return r.err
}

func TestVirtualReadKeepsActualLimitsCloseAndUnknownSizeBehavior(t *testing.T) {
	wantErr := errors.New("关闭失败")
	for _, unknown := range []bool{false, true} {
		for _, limit := range []int64{0, 6, 7, maxInt64} {
			reader := &hintedReadCloser{Reader: bytes.NewReader([]byte("content"))}
			p, err := NewVirtual([]Entry{{Path: "data", SizeUnknown: unknown}}, func(string) (io.ReadCloser, error) {
				return reader, nil
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			got, readErr := p.ReadLimit("data", limit)
			if err := p.Close(); err != nil {
				t.Fatal(err)
			}
			if !reader.closed {
				t.Fatal("读取结束未关闭条目")
			}
			if limit == 6 {
				if !errors.Is(readErr, ErrReadLimitExceeded) {
					t.Fatalf("实际数据超限未被拒绝：%v", readErr)
				}
			} else if readErr != nil || string(got) != "content" {
				t.Fatalf("虚拟资源读取结果改变：%q %v", got, readErr)
			}
		}
	}
	reader := &hintedReadCloser{Reader: bytes.NewReader([]byte("x")), err: wantErr}
	p, err := NewVirtual([]Entry{{Path: "data"}}, func(string) (io.ReadCloser, error) { return reader, nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err := p.Read("data"); !errors.Is(err, wantErr) || !reader.closed {
		t.Fatalf("关闭错误未保留：%v", err)
	}
}

func BenchmarkIntroResourceReadAll(b *testing.B) {
	const input = "../../testdata/ofdrw/intro.ofd"
	p, err := OpenFile(input)
	if errors.Is(err, os.ErrNotExist) {
		b.Skipf("缺少样例 %s", input)
	}
	if err != nil {
		b.Fatal(err)
	}
	defer p.Close()
	entries := p.Entries()
	var size int64
	for _, entry := range entries {
		if !entry.IsDir {
			size += int64(entry.UncompressedSize)
		}
	}
	b.SetBytes(size)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, entry := range entries {
			if entry.IsDir {
				continue
			}
			if _, err := p.ReadLimit(entry.Path, 64<<20); err != nil {
				b.Fatal(err)
			}
		}
	}
}
