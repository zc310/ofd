package converter

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/textdoc"
)

func brokenTextPageFixture(t *testing.T, missing bool) []byte {
	t.Helper()
	input := lifecycleOFD(t, true)
	archive, err := zip.NewReader(bytes.NewReader(input), int64(len(input)))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	w := zip.NewWriter(&output)
	for _, file := range archive.File {
		if missing && file.Name == "Doc_1/Page.xml" {
			continue
		}
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if file.Name == "Doc_1/Page.xml" {
			data = []byte("<Page><Content>")
		}
		entry, err := w.Create(file.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestTextConversionsRejectUnreadablePagesWithoutProducingEmptyDocuments(t *testing.T) {
	for _, missing := range []bool{false, true} {
		for _, format := range []string{"text", "markdown", "docx"} {
			var output bytes.Buffer
			err := Encode(t.Context(), format, brokenTextPageFixture(t, missing), &output)
			if err == nil || !strings.Contains(err.Error(), "第2页") || output.Len() != 0 {
				t.Fatalf("format=%s missing=%v: err=%v output=%d", format, missing, err, output.Len())
			}
		}
	}
}

type textTestPackageSource struct{ pkg *core.Package }

func (s textTestPackageSource) OFDPackage() (*core.Package, error) { return s.pkg, nil }

func TestTextConversionsStopCollectingPagesAfterCancellation(t *testing.T) {
	for _, format := range []string{"text", "markdown", "docx"} {
		t.Run(format, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			source, err := core.OpenBytes(lifecycleOFD(t, true))
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			var reads []string
			pkg, err := core.NewVirtual(source.Entries(), func(name string) (io.ReadCloser, error) {
				if strings.HasSuffix(name, "/Page.xml") {
					reads = append(reads, name)
					cancel()
				}
				return source.Open(name)
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer pkg.Close()
			var output bytes.Buffer
			err = Encode(ctx, format, textTestPackageSource{pkg: pkg}, &output)
			if err != context.Canceled || len(reads) != 1 || output.Len() != 0 {
				t.Fatalf("取消后仍读取或输出: err=%v reads=%v output=%d", err, reads, output.Len())
			}
		})
	}
}

func TestImageExtractionPropagatesPageReadErrors(t *testing.T) {
	ofd, err := parser.NewOFD(brokenTextPageFixture(t, false))
	if err != nil {
		t.Fatal(err)
	}
	defer ofd.Close()
	document := ofd.Documents[1]
	if _, err := textdoc.ExtractImages(t.Context(), document, document.Pages[0], 10); err == nil {
		t.Fatal("损坏页面的图片提取静默成功")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := textdoc.ExtractImages(ctx, document, document.Pages[0], 10); err != context.Canceled {
		t.Fatal(err)
	}
}
