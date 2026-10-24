package contextio

import (
	"context"
	"io"
	"strings"
	"testing"
)

type fullReader struct{}

func (fullReader) Read(data []byte) (int, error) { return len(data), nil }

func benchmarkContexts(b *testing.B, run func(*testing.B, context.Context)) {
	b.Run("Background", func(b *testing.B) { run(b, context.Background()) })
	b.Run("Cancelable", func(b *testing.B) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		run(b, ctx)
	})
}

func BenchmarkReader(b *testing.B) {
	benchmarkContexts(b, func(b *testing.B, ctx context.Context) {
		r := Reader(ctx, fullReader{})
		data := make([]byte, 4096)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if _, err := r.Read(data); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkWriter(b *testing.B) {
	benchmarkContexts(b, func(b *testing.B, ctx context.Context) {
		w := Writer(ctx, io.Discard)
		data := make([]byte, 4096)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if _, err := w.Write(data); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkWriterString(b *testing.B) {
	benchmarkContexts(b, func(b *testing.B, ctx context.Context) {
		w := Writer(ctx, io.Discard)
		data := strings.Repeat("x", 64<<10)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if _, err := io.WriteString(w, data); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkRepeatedWriter(b *testing.B) {
	benchmarkContexts(b, func(b *testing.B, ctx context.Context) {
		w := Writer(ctx, io.Discard)
		data := make([]byte, 4096)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if _, err := Writer(ctx, w).Write(data); err != nil {
				b.Fatal(err)
			}
		}
	})
}
