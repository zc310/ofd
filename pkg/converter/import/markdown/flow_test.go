package markdown

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator/flow"
)

// AST 直接生成公开 flow 类型，不能重新绕回 layout.Document 的旧排版入口。
func TestMarkdownASTBuildsFlowBlocksAndRichTableCells(t *testing.T) {
	doc, err := parseMarkdown([]byte("# Title\n\n3. first\n   - nested\n\n> quoted\n\n---\n\n```go\n\tx()\n```\n\n| A | B |\n| :-- | --: |\n| **bold** | `code` |\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"heading", "list_item", "list_item", "quote", "thematic_break", "code", "table"}
	if len(doc.Blocks) != len(want) {
		t.Fatalf("块数 = %d, blocks=%+v", len(doc.Blocks), doc.Blocks)
	}
	for i, kind := range want {
		if doc.Blocks[i].Kind != kind {
			t.Fatalf("blocks[%d] = %s, want %s", i, doc.Blocks[i].Kind, kind)
		}
	}
	if doc.Blocks[1].Marker != "3." || doc.Blocks[2].Indent != 6 {
		t.Fatal("有序起始号或嵌套缩进丢失")
	}
	table := doc.Blocks[6].Table
	if table.HeaderRows != 1 || !table.SplitRows || table.Columns[1].Align != "right" || !table.Grid[1][0].Runs[0].Bold || !table.Grid[1][1].Runs[0].Code {
		t.Fatalf("表格语义未保留: %+v", table)
	}
	if _, err := doc.Build(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestMarkdownConvertAcceptsExplicitFlowOptionsAndKeepsStrictFonts(t *testing.T) {
	opts := flow.DefaultOptions()
	opts.GenerateOutlines = true
	var output bytes.Buffer
	if err := Convert(t.Context(), []byte("# Title\n\nText and `code`."), &output, opts); err != nil {
		t.Fatal(err)
	}
	if output.Len() == 0 {
		t.Fatal("没有生成 OFD")
	}
	output.Reset()
	if err := Convert(t.Context(), []byte("中文"), &output, opts); err == nil || output.Len() != 0 {
		t.Fatalf("显式字体缺字应在写出前报错: %v", err)
	}
}

func TestMarkdownTrackedFixtureUsesFlow(t *testing.T) {
	path := "../../../../testdata/markdown/sample.md"
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("缺少样本 %s", path)
	}
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Convert(t.Context(), data, &output); err != nil {
		t.Fatal(err)
	}
}

func TestMarkdownFrontMatterMapsAllOfficialSettings(t *testing.T) {
	doc, err := parseMarkdown([]byte("---\nletterhead:\n  org: OFFICE\n  height: 90\n  org_size: 30\n  doc_no_size: 15\nsign:\n  org: OFFICE\n  size: 15\nfooter:\n  page_number: true\n  size: 13\ncolophon:\n  cc: unit\n  size: 12\n---\n# Notice\n\nBody"), "")
	if err != nil {
		t.Fatal(err)
	}
	o := doc.Options.Official
	if o == nil || o.Letterhead.Height != 90 || o.Letterhead.DocNoSize != 15 || o.Signature.Size != 15 || o.Colophon.Size != 12 || o.PageNumberSize != 13 {
		t.Fatalf("公文配置未传递: %+v", o)
	}
	if _, err := doc.Build(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc.Metadata.Title, "Notice") {
		t.Fatal("标题元数据丢失")
	}
}

func TestMarkdownOfficialFirstColonParagraphBecomesRecipient(t *testing.T) {
	doc, err := parseMarkdown([]byte("---\nletterhead:\n  org: OFFICE\n---\n# Notice\n\n各单位：\n\n正文：\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Blocks[1].Kind != "recipient" || doc.Blocks[2].Kind != "paragraph" {
		t.Fatalf("仅首个正文段可识别为主送: %+v", doc.Blocks)
	}
	if _, err := doc.Build(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestMarkdownFlowOptionsControlPageOutlinesAndEmbeddedFonts(t *testing.T) {
	opts := flow.DefaultOptions()
	opts.PageSize.Width = 150
	opts.GenerateOutlines = true
	doc, err := parseMarkdown([]byte("# Title\n\nText with `code`."), "", opts)
	if err != nil {
		t.Fatal(err)
	}
	model, err := doc.Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if model.PageSize.Width != 150 || len(model.Outlines) != 1 {
		t.Fatal("没有使用指定的页面和大纲设置")
	}
	mono := false
	for _, font := range model.Fonts {
		if len(font.Data) == 0 {
			t.Fatalf("显式字体选项被替换为逻辑字体: %+v", font)
		}
		mono = mono || font.FixedWidth
	}
	if !mono {
		t.Fatal("未嵌入代码等宽字体")
	}
}
