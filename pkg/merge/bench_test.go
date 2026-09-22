package merge

import (
	"bytes"
	"os"
	"testing"
)

func benchmarkInputs(b *testing.B) []string {
	b.Helper()
	return []string{testdataPath("multi_demo.ofd"), testdataPath("helloworld.ofd")}
}

func benchmarkOptions() Options {
	return Options{Signatures: SignatureDrop, Orphans: OrphanIgnore}
}

func BenchmarkFiles(b *testing.B) {
	paths := benchmarkInputs(b)
	options := benchmarkOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buffer bytes.Buffer
		if err := Files(paths, &buffer, options); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBytes(b *testing.B) {
	paths := benchmarkInputs(b)
	documents := make([][]byte, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		documents = append(documents, data)
	}
	options := benchmarkOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buffer bytes.Buffer
		if err := Bytes(documents, &buffer, options); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSourcesReaderAt(b *testing.B) {
	paths := benchmarkInputs(b)
	sources := make([]Source, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			b.Fatal(err)
		}
		sources = append(sources, Source{Name: path, Reader: bytes.NewReader(data), Size: int64(len(data))})
	}
	options := benchmarkOptions()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buffer bytes.Buffer
		if err := Sources(sources, &buffer, options); err != nil {
			b.Fatal(err)
		}
	}
}
