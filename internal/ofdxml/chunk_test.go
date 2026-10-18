package ofdxml

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type chunkRecorder struct {
	bytes.Buffer
	calls    int
	maxChunk int
}

func (r *chunkRecorder) Write(data []byte) (int, error) {
	r.calls++
	r.maxChunk = max(r.maxChunk, len(data))
	return r.Buffer.Write(data)
}

func TestChunkedOutputMatchesBufferedXML(t *testing.T) {
	ints := make([]int, 20000)
	floats := make([]float64, len(ints))
	for index := range ints {
		ints[index] = index - 10000
		floats[index] = float64(index) / 17
	}
	for _, test := range []struct {
		name string
		emit func(*Writer)
	}{
		{"ASCII", func(w *Writer) { w.Element("Text", strings.Repeat("x", 1<<20)) }},
		{"CJK", func(w *Writer) { w.Element("Text", strings.Repeat("正文𠀀", 1<<16)) }},
		{"escaped and invalid", func(w *Writer) {
			value := strings.Repeat("甲&<>\"\t\n\r\x00\xff\xef\xbf\xbf", 10000)
			w.Start("Text")
			w.Attr("Value", value)
			w.Text(value)
		}},
		{"raw fragment", func(w *Writer) {
			w.Start("Data")
			w.Raw([]byte(strings.Repeat(`<cfg:Value xmlns:cfg="urn:config"><![CDATA[甲<&]]></cfg:Value>`, 10000)))
		}},
		{"numeric lists", func(w *Writer) {
			w.Start("Numbers")
			w.AttrIntList("Ints", ints)
			w.AttrFloatList("Floats", floats)
			w.ElementIntList("Ints", ints)
			w.ElementFloatList("Floats", floats)
		}},
		{"many small elements", func(w *Writer) {
			for index := 0; index < 10000; index++ {
				w.Start("Item")
				w.AttrUint("ID", uint64(index))
				w.AttrInt("Index", -index)
				w.AttrFloat("Size", 4.2333)
				w.AttrBox("Boundary", 1, 2, 210, 297)
				w.ElementUint("Uint", uint64(index))
				w.ElementInt("Int", -index)
				w.ElementFloat("Float", 0.125)
				w.ElementBox("Box", 1, 2, 210, 297)
				w.End()
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			buffered := New()
			defer Put(buffered)
			buffered.Start("Root")
			test.emit(buffered)
			want := buffered.Bytes()

			w := New()
			defer Put(w)
			out := &chunkRecorder{}
			w.ResetTo(out)
			w.Start("Root")
			test.emit(w)
			if out.calls == 0 {
				t.Fatal("生成过程未分块写出数据")
			}
			if err := w.Finish(); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out.Bytes(), want) {
				t.Fatal("分块输出与内存输出不一致")
			}
			if out.maxChunk > streamBufferSize+256 || cap(w.buf) > 2*streamBufferSize {
				t.Fatalf("缓冲区未保持有界: chunk=%d capacity=%d", out.maxChunk, cap(w.buf))
			}
		})
	}
}

type failingChunkWriter struct {
	err   error
	calls int
}

func (w *failingChunkWriter) Write([]byte) (int, error) {
	w.calls++
	return 1, w.err
}

func TestChunkedOutputStopsAfterFirstErrorAndCanBeReused(t *testing.T) {
	sentinel := errors.New("output failed")
	for _, test := range []struct {
		name string
		err  error
		want error
	}{{"short write", nil, io.ErrShortWrite}, {"write failure", sentinel, sentinel}} {
		t.Run(test.name, func(t *testing.T) {
			w := New()
			defer Put(w)
			out := &failingChunkWriter{err: test.err}
			w.ResetTo(out)
			w.Element("Long", strings.Repeat("x", 4*streamBufferSize))
			for index := 0; index < 10000; index++ {
				w.ElementInt("Ignored", index)
			}
			if !errors.Is(w.Err(), test.want) || !errors.Is(w.Finish(), test.want) {
				t.Fatalf("错误未保留: %v", w.Err())
			}
			if out.calls != 1 || len(w.buf) != 0 || w.dst != nil {
				t.Fatalf("失败后仍写入或持有输出目标: calls=%d buffered=%d dst=%v", out.calls, len(w.buf), w.dst)
			}
			var next bytes.Buffer
			w.ResetTo(&next)
			w.Element("Next", "value")
			if err := w.Finish(); err != nil || !bytes.Contains(next.Bytes(), []byte(">value</ofd:Next>")) {
				t.Fatalf("错误后复用失败: %v %s", err, next.Bytes())
			}
		})
	}
}

func TestPutDropsStreamingDestinationAndError(t *testing.T) {
	w := newWriter()
	useSingleWriterPool(t, w)
	out := &failingChunkWriter{err: errors.New("failed")}
	w.ResetTo(out)
	w.Element("Long", strings.Repeat("x", 4*streamBufferSize))
	Put(w)
	if w.dst != nil || w.err != nil {
		t.Fatal("池仍持有输出目标或错误")
	}
	reused := New()
	defer Put(reused)
	reused.Element("Memory", "value")
	if !bytes.Contains(reused.Bytes(), []byte(">value</ofd:Memory>")) || out.calls != 1 {
		t.Fatal("池复用后仍向旧目标写入")
	}
}
