package creator

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/xmlcodec"
	"github.com/zc310/ofd/pkg/spec"
)

// bufferedStreamSink 用完整缓冲区写入同一 ZIP sink，作为分块输出的字节参考。
type bufferedStreamSink struct{ entrySink }

func (s bufferedStreamSink) writeXML(name string, w *xmlcodec.Writer, emit func()) error {
	w.Reset()
	emit()
	return s.write(name, w.Bytes())
}

func TestChunkedPagesMatchBufferedOutputAndSignatureDigests(t *testing.T) {
	text := Text{Width: 20, Height: 10, Value: strings.Repeat("甲 & <text>\r\n", 10000)}
	document := Document{
		ID:        "chunked-pages",
		Templates: []TemplatePage{{ID: 70, Items: []Item{text}}},
		Pages:     []Page{{Templates: []TemplateRef{{ID: 70}}, Items: []Item{text}}},
		Signatures: []Signature{{
			ID: "sig-1", Type: spec.SigTypeSign, ProviderName: "test", CheckMethod: "SHA1",
			References: []SignatureReference{
				{FileRef: "../Pages/Page_0/Content.xml"},
				{FileRef: "../Pages/Template_0/Content.xml"},
			},
			SignedValue: []byte("value"),
		}},
	}
	for _, mode := range []CompressionMode{CompressionStore, CompressionDeflate} {
		t.Run(string(mode), func(t *testing.T) {
			options := CreateOptions{Compression: mode, Deterministic: true}
			got, err := MarshalWithOptions(document, options)
			if err != nil {
				t.Fatal(err)
			}
			state, err := prepare(document, slicePages{pages: document.Pages}, options)
			if err != nil {
				t.Fatal(err)
			}
			defer state.clearPatternCaches()
			var reference bytes.Buffer
			archive := zip.NewWriter(&reference)
			sink := bufferedStreamSink{newZipSink(archive, options, signatureReferenceTargets(state))}
			if err := generatePackage(state, sink); err != nil {
				t.Fatal(err)
			}
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			wantEntries := openZipEntries(t, reference.Bytes())
			gotEntries := openZipEntries(t, got)
			if len(wantEntries) != len(gotEntries) {
				t.Fatalf("条目数不一致: got %d, want %d", len(gotEntries), len(wantEntries))
			}
			for name, want := range wantEntries {
				if !bytes.Equal(gotEntries[name], want) {
					t.Errorf("分块写入改变了条目 %s", name)
				}
			}
			checkGeneratedPackage(t, got)
			second, err := MarshalWithOptions(document, options)
			if err != nil || !bytes.Equal(got, second) {
				t.Fatalf("分块写入的 ZIP 输出不确定: %v", err)
			}
		})
	}
}

type limitedOutput struct {
	remaining int
	err       error
}

func (w *limitedOutput) Write(data []byte) (int, error) {
	n := min(len(data), w.remaining)
	w.remaining -= n
	if n < len(data) {
		return n, w.err
	}
	return n, nil
}

func TestChunkedPagePropagatesOutputErrors(t *testing.T) {
	document := Document{ID: "chunk-error", Pages: []Page{{Items: []Item{
		Text{Width: 20, Height: 10, Value: strings.Repeat("x", 1<<20)},
	}}}}
	for _, test := range []struct {
		name string
		err  error
		want error
	}{{"canceled output", context.Canceled, context.Canceled}, {"short write", nil, io.ErrShortWrite}} {
		t.Run(test.name, func(t *testing.T) {
			out := &limitedOutput{remaining: 1024, err: test.err}
			err := CreateWithOptions(document, out, CreateOptions{Compression: CompressionStore})
			if !errors.Is(err, test.want) || !strings.Contains(err.Error(), pagePath(0)) {
				t.Fatalf("页面写入错误未保留: %v, want %v", err, test.want)
			}
		})
	}
}
