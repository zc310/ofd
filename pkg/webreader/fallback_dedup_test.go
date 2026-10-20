package webreader

import (
	"image/color"
	"os"
	"slices"
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

func TestUseFallbackFontDeduplicatesFamiliesAndStillAppliesUpdates(t *testing.T) {
	data, err := os.ReadFile("../../testdata/ofd-creator/helloworld.ofd")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	first, second := t.Name()+"-first", t.Name()+"-second"
	for _, name := range []string{first, second} {
		if err := RegisterFallbackFont(FontSource{Family: name, Data: goregular.TTF}); err != nil {
			t.Fatal(err)
		}
		defer UnregisterFallbackFont(name)
	}
	for _, name := range []string{first, first, second, first} {
		if err := reader.UseFallbackFont(name); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(reader.fallbackFamilies, []string{first, second}) || reader.fallbackFamily != first {
		t.Fatalf("回退列表 = %v，当前字体 = %q", reader.fallbackFamilies, reader.fallbackFamily)
	}
	if err := reader.UseFallbackFont(t.Name() + "-missing"); err == nil || !slices.Equal(reader.fallbackFamilies, []string{first, second}) {
		t.Fatal("未注册字体应报错且保留已有列表")
	}
	// 同一族新增样式后，再次应用仍必须刷新各文档和快照，不能因已在列表中而早退。
	view, err := reader.pageDocument(reader.pages[0], color.Transparent, geom.DPI(96))
	if err != nil {
		t.Fatal(err)
	}
	reader.releasePageDocument(view)
	previousText, previousSearch := reader.text, reader.search
	if err := RegisterFallbackFont(FontSource{Family: first, Data: gobold.TTF, Weight: 700}); err != nil {
		t.Fatal(err)
	}
	if err := reader.UseFallbackFont(first); err != nil {
		t.Fatal(err)
	}
	if reader.renderDocs != nil || reader.text == previousText || reader.search == previousSearch {
		t.Fatal("同族字体更新后没有刷新渲染、文字和搜索缓存")
	}
	if !slices.Equal(reader.fallbackFamilies, []string{first, second}) {
		t.Fatal("同族字体更新导致列表重复")
	}
	if err := reader.RemoveFallbackFont(first); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(reader.fallbackFamilies, []string{second}) || reader.fallbackFamily != second {
		t.Fatal("移除当前族后没有恢复剩余字体")
	}
	if err := reader.RemoveFallbackFont(second); err != nil {
		t.Fatal(err)
	}
	if len(reader.fallbackFamilies) != 0 || reader.fallbackFamily != "" {
		t.Fatal("移除全部回退字体后仍有残留")
	}
}
