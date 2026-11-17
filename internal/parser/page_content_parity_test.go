package parser

// TestFastVsStdlibParity 用仓库内全部 *Content.xml 夹具对快速解析路径与
// encoding/xml 做差分对比，防止手写词法路径与 stdlib 语义偏离。

import (
	"encoding/xml"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/pkg/spec"
)

func TestFastVsStdlibParity(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("..", "..", "testdata", "ofd-creator", "*.ofd"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Skip("OFD 创建器样例不存在，跳过解析一致性测试")
	}
	count := 0
	for _, fixture := range matches {
		pkg, err := container.OpenFile(fixture)
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
			var std spec.PageContent
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
			fs := summarize((*spec.PageContent)(fast))
			if ss != fs {
				t.Errorf("%s\n  stdlib: %s\n  fast:   %s", fixture+"/"+e.Path, ss, fs)
			}
		}
		pkg.Close()
	}
	t.Logf("compared %d content files", count)
}

func summarize(c *spec.PageContent) string {
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
			case spec.PageItemText:
				b.WriteString(" T")
			case spec.PageItemPath:
				p := it.Path
				b.WriteString(" P")
				if p != nil {
					b.WriteString("lw")
					b.WriteString(ftoaInt(int(p.LineWidth)))
				}
			case spec.PageItemImage:
				b.WriteString(" I")
			case spec.PageItemComposite:
				b.WriteString(" C")
			case spec.PageItemBlock:
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
