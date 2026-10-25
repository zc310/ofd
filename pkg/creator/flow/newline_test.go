package flow_test

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
	"github.com/zc310/ofd/pkg/validator"
)

func newlineDocument(kind string, columns bool, runs []flow.Run) *flow.Document {
	opts := smallOptions()
	opts.Theme.Body.Align = "justify"
	if columns {
		opts.Columns = flow.Columns{Count: 2, Gap: 8}
	}
	d := flow.New(opts)
	d.AddParagraph(strings.Repeat("Prefix\n", 5))
	switch kind {
	case "paragraph", "list_item":
		d.Blocks = append(d.Blocks, flow.Block{ID: "content", Kind: kind, Runs: runs})
	case "table":
		d.AddTable(flow.Table{Grid: [][]flow.TableCell{{{Runs: runs}}}})
	case "footnote":
		d.Options.Footnotes = []flow.Footnote{{ID: "note", Runs: runs}}
		d.Blocks = append(d.Blocks, flow.Block{Runs: []flow.Run{{Text: "Source"}, {Footnote: "note"}}})
	}
	d.AddParagraph("Following")
	return d
}

// CRLF 的换行语义不应受富文本分片、空片段、字体和链接边界影响；连续换行仍需保留。
func TestSplitCRLFMatchesSingleNewlineAcrossContentKinds(t *testing.T) {
	for _, kind := range []string{"paragraph", "list_item", "table", "footnote"} {
		for _, columns := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/columns=%v", kind, columns), func(t *testing.T) {
				for _, test := range []struct {
					name   string
					prefix string
					middle []flow.Run
					suffix string
					breaks string
				}{
					{"split CRLF", "First\r", nil, "\nSecond", "\n"},
					{"empty runs", "First\r", []flow.Run{{}, {Text: "", Bold: true}}, "\nSecond", "\n"},
					{"LF only run", "First\r", []flow.Run{{Text: "\n", Italic: true}, {}}, "Second", "\n"},
					{"two split CRLF", "First\r", []flow.Run{{Text: "\n\r"}}, "\nSecond", "\n\n"},
					{"CRLF plus LF", "First\r", nil, "\n\nSecond", "\n\n"},
					{"CR plus CRLF", "First\r", nil, "\r\nSecond", "\n\n"},
					{"standalone CR", "First\r", nil, "Second", "\n"},
					{"standalone LF", "First\n", nil, "\nSecond", "\n\n"},
				} {
					t.Run(test.name, func(t *testing.T) {
						runs := append([]flow.Run{{Text: test.prefix}}, test.middle...)
						runs = append(runs, flow.Run{Text: test.suffix, Bold: true, Href: "https://example.com"})
						original := append([]flow.Run(nil), runs...)
						got, err := newlineDocument(kind, columns, runs).Build(context.Background())
						if err != nil {
							t.Fatal(err)
						}
						want, err := newlineDocument(kind, columns, []flow.Run{{Text: "First" + test.breaks}, {Text: "Second", Bold: true, Href: "https://example.com"}}).Build(context.Background())
						if err != nil {
							t.Fatal(err)
						}
						if !reflect.DeepEqual(got.Pages, want.Pages) || !reflect.DeepEqual(got.Bookmarks, want.Bookmarks) {
							t.Fatalf("富文本分片改变换行或分页: got pages=%d want=%d", len(got.Pages), len(want.Pages))
						}
						if !reflect.DeepEqual(runs, original) {
							t.Fatal("换行规范化修改了输入片段")
						}
					})
				}
			})
		}
	}
}

func TestSplitCRLFDoesNotSkipPastVisibleFootnoteReferences(t *testing.T) {
	build := func(prefix string) *creator.Document {
		t.Helper()
		d := flow.New(smallOptions())
		d.Options.Footnotes = []flow.Footnote{{ID: "note", Text: "Note"}}
		d.Blocks = []flow.Block{{Runs: []flow.Run{{Text: prefix}, {Footnote: "note"}, {Text: "\nSecond"}}}}
		model, err := d.Build(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return model
	}
	got, want := build("First\r"), build("First\n")
	if !reflect.DeepEqual(got.Pages, want.Pages) {
		t.Fatal("规范化跨过可见脚注标记，错误合并了两次换行")
	}
}

// 分片方式不同的 CRLF 应生成相同的确定性 OFD，并通过严格校验。
func TestSplitCRLFProducesSameDeterministicOFD(t *testing.T) {
	v, err := validator.New(validator.WithMode(validator.ModeStrict))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"paragraph", "list_item", "table", "footnote"} {
		t.Run(kind, func(t *testing.T) {
			var got, want bytes.Buffer
			opts := creator.CreateOptions{Deterministic: true}
			if err := newlineDocument(kind, true, []flow.Run{{Text: "First\r"}, {}, {Text: "\nSecond", Bold: true}}).Write(context.Background(), &got, opts); err != nil {
				t.Fatal(err)
			}
			if err := newlineDocument(kind, true, []flow.Run{{Text: "First\n"}, {Text: "Second", Bold: true}}).Write(context.Background(), &want, opts); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got.Bytes(), want.Bytes()) {
				t.Fatal("CRLF 分片方式改变确定性 OFD")
			}
			report := v.ValidateReader(context.Background(), bytes.NewReader(got.Bytes()), "newlines.ofd")
			if report.HasErrors() || report.HasWarnings() {
				t.Fatalf("严格校验失败: %+v", report.Issues)
			}
		})
	}
}
