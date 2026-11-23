package pdf

import (
	"bytes"
	"compress/zlib"
	"io"
	"math/rand"
	"testing"
)

func TestPDFCompressionRemainsStandardZlibAcrossWriterResets(t *testing.T) {
	random := make([]byte, 80<<10)
	_, _ = rand.New(rand.NewSource(1)).Read(random)
	// 覆盖大、小、空流，以及切换 DCT 低级别压缩后回到普通级别的状态重置。
	inputs := [][]byte{
		bytes.Repeat([]byte("中文图片颜色、透明度及 PDF 内容流\n"), 4000),
		[]byte("short"), nil, random, bytes.Repeat([]byte{0, 128, 255}, 2000),
	}
	var output bytes.Buffer
	writer := newPDFWriter(&output)
	for _, input := range inputs {
		for _, filters := range []pdfArray{
			{pdfFilterFlate}, {pdfFilterDCT, pdfFilterFlate}, {pdfFilterFlate},
		} {
			start := output.Len()
			writer.writeVal(pdfStream{dict: pdfDict{"Filter": filters}, stream: input})
			stream := output.Bytes()[start:]
			first := bytes.Index(stream, []byte("stream\n"))
			last := bytes.LastIndex(stream, []byte("\nendstream"))
			if first < 0 || last < first {
				t.Fatal("未写出有效的 PDF 流边界")
			}
			reader, err := zlib.NewReader(bytes.NewReader(stream[first+7 : last]))
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(decoded, input) {
				t.Fatal("优化压缩器改变了流采样或复用了上一条流的状态")
			}
		}
	}
	if writer.flateWriter == nil {
		t.Fatal("没有建立可复用的压缩器")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if writer.flateWriter != nil {
		t.Fatal("Close 未释放压缩器")
	}
}
