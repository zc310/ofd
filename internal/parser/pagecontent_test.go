package parser

import (
	"errors"
	"os"
	"testing"

	"github.com/zc310/ofd/internal/core"
)

func TestReadPageContentXMLPreservesReadErrors(t *testing.T) {
	content := `<Page><Content><Layer ID="1"/></Content></Page>`
	fc, err := core.OpenBytes(parserTestArchive(t, map[string]string{"Page.xml": content}))
	if err != nil {
		t.Fatal(err)
	}
	defer fc.Close()
	for _, tc := range []struct {
		name  string
		path  string
		limit int64
		want  error
	}{
		{"missing_file", "missing.xml", 0, os.ErrNotExist},
		{"over_limit", "Page.xml", int64(len(content) - 1), core.ErrReadLimitExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if page, err := readPageContentXML(fc, tc.path, tc.limit); page != nil || !errors.Is(err, tc.want) {
				t.Fatalf("页面读取 = %p, %v，期望 nil, %v", page, err, tc.want)
			}
		})
	}
	page, err := readPageContentXML(fc, "Page.xml", int64(len(content)))
	if err != nil {
		t.Fatalf("恰好达到上限应能读取：%v", err)
	}
	if page.Content == nil || len(page.Content.Layer) != 1 || page.Content.Layer[0].ID != 1 {
		t.Fatalf("页面内容不匹配：%+v", page)
	}
}

func TestXMLReadLimitSaturatesWithoutOverflow(t *testing.T) {
	const maxInt64 = 1<<63 - 1
	for _, tc := range []struct {
		size uint64
		want int64
	}{
		{0, 1024},
		{1, 1025},
		{maxInt64 - 1024, maxInt64},
		{maxInt64 - 1023, maxInt64},
		{maxInt64, maxInt64},
		{^uint64(0), maxInt64},
	} {
		if got := xmlReadLimit(tc.size); got != tc.want {
			t.Errorf("xmlReadLimit(%d) = %d，期望 %d", tc.size, got, tc.want)
		}
	}
}
