package core

import (
	"bufio"
	"io"
	"sync"

	"github.com/klauspost/compress/flate"
	"github.com/klauspost/compress/zip"
)

// deflateBufferSize 与 bufio.NewReader 的默认缓冲一致（4KB）。ZIP deflate 条目
// 的解压器通过 ReadByte 逐字节消费输入，缓冲大小需覆盖一次解压的预读量。
const deflateBufferSize = 4096

// klauspost/compress/zip 已用 sync.Pool 复用 flate 解压器，但 flate.Reset 会为
// io.SectionReader 重新调用 bufio.NewReader，每次打开条目都分配一个 4KB 缓冲。
// 大文档逐页打开页面时，这是页面加载的主要分配。这里把 bufio.Reader 与 flate
// 解压器一起放进池，成对复用。
var (
	deflateBufferPool = sync.Pool{
		New: func() any { return bufio.NewReaderSize(nil, deflateBufferSize) },
	}
	deflateReaderPool = sync.Pool{}
)

// pooledDeflate 是注册给 zip.Reader 的 deflate 解压器。它保证 Close 时把
// bufio.Reader 与 flate 解压器归还到池中，供后续条目复用。
func pooledDeflate(r io.Reader) io.ReadCloser {
	br := deflateBufferPool.Get().(*bufio.Reader)
	br.Reset(r)

	var fr io.ReadCloser
	if pooled := deflateReaderPool.Get(); pooled != nil {
		fr = pooled.(io.ReadCloser)
		fr.(flate.Resetter).Reset(br, nil)
	} else {
		fr = flate.NewReader(br)
	}
	return &pooledDeflateReader{br: br, fr: fr}
}

type pooledDeflateReader struct {
	br *bufio.Reader
	fr io.ReadCloser
}

func (d *pooledDeflateReader) Read(p []byte) (int, error) { return d.fr.Read(p) }

func (d *pooledDeflateReader) Close() error {
	err := d.fr.Close()
	// 断开对底层读取器的引用后再归还，避免池里的对象持有 SectionReader/包数据。
	d.br.Reset(nil)
	deflateReaderPool.Put(d.fr)
	deflateBufferPool.Put(d.br)
	return err
}

// registerDecompressor 为包的 ZIP reader 安装可复用的 deflate 解压器。
func registerDecompressor(reader *zip.Reader) {
	if reader == nil {
		return
	}
	reader.RegisterDecompressor(zip.Deflate, pooledDeflate)
}
