package creator

import (
	"fmt"
	"io"
	"path"
	"time"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/core"
)

// entrySink 接收生成阶段产出的 OFD 包条目。实现可以是内存收集（构建/基准）
// 或直接写入 ZIP 的流式写入器，生成阶段无需区分。
type entrySink interface {
	write(name string, data []byte) error
	// writeSource 流式写入惰性来源，避免资源整体驻留内存。
	writeSource(name string, source DataSource) error
	// lookup 返回先前写入条目的数据，仅用于签名摘要计算。
	lookup(name string) ([]byte, bool)
	// has 判断条目是否已写入，用于版本文件等引用校验。
	has(name string) bool
}

// writeResource 按资源是否提供惰性来源选择写入方式。
func writeResource(sink entrySink, name string, data []byte, source DataSource) error {
	if source != nil {
		return sink.writeSource(name, source)
	}
	return sink.write(name, data)
}

// collectSink 把所有条目收集到内存中的 packageState。
type collectSink struct {
	state *packageState
	seen  map[string]bool
	index map[string]int
}

func newCollectSink(state *packageState) *collectSink {
	return &collectSink{state: state, seen: make(map[string]bool), index: make(map[string]int)}
}

func (s *collectSink) write(name string, data []byte) error {
	if err := validateEntryName(name, s.seen); err != nil {
		return err
	}
	s.index[name] = len(s.state.entries)
	s.state.entries = append(s.state.entries, zipEntry{name: name, data: append([]byte(nil), data...)})
	return nil
}

func (s *collectSink) writeSource(name string, source DataSource) error {
	data, err := readDataSource(source)
	if err != nil {
		return fmt.Errorf("读取 OFD 包条目 %q 失败: %w", name, err)
	}
	return s.write(name, data)
}

func (s *collectSink) lookup(name string) ([]byte, bool) {
	index, ok := s.index[name]
	if !ok {
		return nil, false
	}
	return s.state.entries[index].data, true
}

func (s *collectSink) has(name string) bool {
	return s.seen[name]
}

// zipSink 把条目直接写入 ZIP，不在内存中保留全部 XML 与资源数据。
// 只保留签名引用目标的字节，用于计算摘要。
type zipSink struct {
	archive  *zip.Writer
	options  CreateOptions
	seen     map[string]bool
	targets  map[string]bool
	retained map[string][]byte
}

func newZipSink(archive *zip.Writer, options CreateOptions, targets map[string]bool) *zipSink {
	if targets == nil {
		targets = map[string]bool{}
	}
	ApplyCompressionLevel(archive, options.CompressionLevel)
	return &zipSink{
		archive:  archive,
		options:  options,
		seen:     make(map[string]bool),
		targets:  targets,
		retained: make(map[string][]byte),
	}
}

func (s *zipSink) write(name string, data []byte) error {
	if err := validateEntryName(name, s.seen); err != nil {
		return err
	}
	header := &zip.FileHeader{
		Name:   name,
		Method: EntryMethod(name, s.options.Compression),
	}
	if s.options.Deterministic {
		header.Modified = time.Unix(0, 0).UTC()
	}
	file, err := s.archive.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("创建 ZIP 条目 %q 失败: %w", name, err)
	}
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("写入 ZIP 条目 %q 失败: %w", name, err)
	}
	if s.targets[name] {
		s.retained[name] = append([]byte(nil), data...)
	}
	return nil
}

func (s *zipSink) writeSource(name string, source DataSource) error {
	if err := validateEntryName(name, s.seen); err != nil {
		return err
	}
	header := &zip.FileHeader{
		Name:   name,
		Method: EntryMethod(name, s.options.Compression),
	}
	if s.options.Deterministic {
		header.Modified = time.Unix(0, 0).UTC()
	}
	file, err := s.archive.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("创建 ZIP 条目 %q 失败: %w", name, err)
	}
	if s.targets[name] {
		data, readErr := readDataSource(source)
		if readErr != nil {
			return fmt.Errorf("读取 OFD 包条目 %q 失败: %w", name, readErr)
		}
		if _, writeErr := file.Write(data); writeErr != nil {
			return fmt.Errorf("写入 ZIP 条目 %q 失败: %w", name, writeErr)
		}
		s.retained[name] = data
		return nil
	}
	reader, err := source.Open()
	if err != nil {
		return fmt.Errorf("打开 OFD 包条目 %q 数据源失败: %w", name, err)
	}
	defer func() { _ = reader.Close() }()
	if _, err := io.Copy(file, reader); err != nil {
		return fmt.Errorf("写入 ZIP 条目 %q 失败: %w", name, err)
	}
	return nil
}

func (s *zipSink) lookup(name string) ([]byte, bool) {
	data, ok := s.retained[name]
	return data, ok
}

func (s *zipSink) has(name string) bool {
	return s.seen[name]
}

// signatureReferenceTargets 计算所有签名引用指向的包内条目路径，
// 使流式 sink 能在写入这些条目时保留字节用于摘要。
func signatureReferenceTargets(state *buildState) map[string]bool {
	if len(state.signatures) == 0 {
		return nil
	}
	targets := make(map[string]bool)
	for _, signature := range state.signatures {
		for _, reference := range signature.value.References {
			targets[path.Clean(path.Join(docDir+"/Signatures", reference.FileRef))] = true
		}
	}
	return targets
}

// validateEntryName 校验包内条目路径安全且不重复。
func validateEntryName(name string, seen map[string]bool) error {
	if err := core.ValidateEntryName(name); err != nil {
		return err
	}
	if seen[name] {
		return fmt.Errorf("OFD 包条目路径重复: %s", name)
	}
	seen[name] = true
	return nil
}
