package split

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/pkg/converter"
)

func TestSplitPreservesRasterAppearanceAndPDFOutput(t *testing.T) {
	source := splitFixture(t)
	s := openTest(t, source, Options{})
	part, _ := writeTest(t, s, Part{Document: 1, Pages: []int{2, 1}})
	for outputPage, sourcePage := range []int{2, 1} {
		var before, after bytes.Buffer
		if err := converter.Convert(t.Context(), "ofd", "png", source, &before, converter.Page(sourcePage)); err != nil {
			t.Fatal(err)
		}
		if err := converter.Convert(t.Context(), "ofd", "png", part, &after, converter.Page(outputPage+1)); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before.Bytes(), after.Bytes()) {
			t.Fatalf("输出第 %d 页栅格外观改变", outputPage+1)
		}
	}
	var pdf bytes.Buffer
	if err := converter.Convert(t.Context(), "ofd", "pdf", part, &pdf); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf.Bytes(), []byte("%PDF-")) {
		t.Fatal("PDF 输出无效")
	}
}

func TestSplitCreatorFixturesPreserveTextAndResourceDependencies(t *testing.T) {
	for _, name := range []string{"helloworld.ofd", "cover-thumbnail.ofd", "default-cs.ofd", "pattern-fill.ofd", "clips.ofd", "media-actions.ofd", "color-spaces.ofd", "draw-params.ofd", "text-directions.ofd"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "..", "testdata", "ofd-creator", name)
			data, err := os.ReadFile(path)
			if errors.Is(err, os.ErrNotExist) {
				t.Skipf("缺少样例 %s", path)
			}
			if err != nil {
				t.Fatal(err)
			}
			s := openTest(t, data, Options{})
			var out bytes.Buffer
			if _, err := s.Write(t.Context(), Part{Document: 1, Pages: []int{1}}, &out); err != nil {
				t.Fatal(err)
			}
			var before, after bytes.Buffer
			if err := converter.Convert(t.Context(), "ofd", "text", data, &before, converter.Page(1)); err != nil {
				t.Fatal(err)
			}
			if err := converter.Convert(t.Context(), "ofd", "text", out.Bytes(), &after); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before.Bytes(), after.Bytes()) {
				t.Fatal("文字提取结果改变")
			}
		})
	}
}
