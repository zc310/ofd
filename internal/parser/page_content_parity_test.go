package parser

// TestFastVsStdlibParity 用仓库内全部 *Content.xml 夹具对快速解析路径与
// encoding/xml 做差分对比，防止手写词法路径与 stdlib 语义偏离。

import (
	"encoding/xml"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/models"
)

func TestFastVsStdlibParity(t *testing.T) {
	matches, _ := filepath.Glob(filepath.Join("..", "..", "test", "testdata", "*.ofd"))
	if len(matches) == 0 {
		matches, _ = filepath.Glob(filepath.Join("..", "..", "test", "testdata", "**", "*.ofd"))
	}
	count := 0
	for _, fixture := range matches {
		pkg, err := core.OpenFile(fixture)
		if err != nil {
			t.Fatalf("%s: %v", fixture, err)
		}
		for _, e := range pkg.Entries() {
			if !strings.HasSuffix(e.Path, "Content.xml") {
				continue
			}
			raw, err := pkg.ReadLimit(e.Path, 64<<20)
			if err != nil {
				t.Fatalf("%s: %v", e.Path, err)
			}
			var std models.PageContent
			if err := xml.Unmarshal(raw, &std); err != nil {
				t.Fatalf("%s stdlib: %v", e.Path, err)
			}
			fast, err := models.ParsePageContentXML(raw)
			if err != nil {
				t.Errorf("%s fast: %v", fixture+"/"+e.Path, err)
				continue
			}
			count++
			ss := summarize(&std)
			fs := summarize(fast)
			if ss != fs {
				t.Errorf("%s\n  stdlib: %s\n  fast:   %s", fixture+"/"+e.Path, ss, fs)
			}
		}
		pkg.Close()
	}
	t.Logf("compared %d content files", count)
}

func summarize(c *models.PageContent) string {
	var b strings.Builder
	b.WriteString("t=")
	b.WriteString(itoa(len(c.Template)))
	b.WriteString(" pr=")
	b.WriteString(itoa(len(c.PageRes)))
	b.WriteString(" act=")
	if c.Actions != nil {
		b.WriteString("y")
	} else {
		b.WriteString("n")
	}
	b.WriteString(" area=")
	if c.Area != nil {
		b.WriteString(c.Area.PhysicalBox.String())
	} else {
		b.WriteString("nil")
	}
	if c.Content == nil {
		b.WriteString(" content=nil")
		return b.String()
	}
	b.WriteString(" layers=")
	b.WriteString(itoa(len(c.Content.Layer)))
	for _, l := range c.Content.Layer {
		b.WriteString(" [")
		b.WriteString(itoa(int(l.ID)))
		b.WriteString(" ")
		b.WriteString(itoa(len(l.Items)))
		for _, it := range l.Items {
			switch it.Kind {
			case models.PageItemText:
				b.WriteString(" T")
			case models.PageItemPath:
				p := it.Path
				b.WriteString(" P")
				if p != nil {
					b.WriteString("lw")
					b.WriteString(ftoaInt(int(p.LineWidth)))
				}
			case models.PageItemImage:
				b.WriteString(" I")
			case models.PageItemComposite:
				b.WriteString(" C")
			case models.PageItemBlock:
				b.WriteString(" B")
			default:
				b.WriteString(" ?")
			}
		}
		b.WriteString("]")
	}
	return b.String()
}

func itoa(i int) string { return string(rune('0' + i)) }

func ftoaInt(i int) string {
	return string(rune('A' + i%26))
}
