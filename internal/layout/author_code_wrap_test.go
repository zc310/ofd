package layout

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"unicode"

	"github.com/zc310/ofd/pkg/creator"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/sfnt"
)

func codeWrapEngine(t *testing.T) *engine {
	t.Helper()
	face, err := sfnt.Parse(gomono.TTF)
	if err != nil {
		t.Fatal(err)
	}
	return &engine{author: &authorState{ctx: t.Context(),
		faces: map[string]*sfnt.Font{"Mono": face},
		fontResources: map[string]creator.Font{"Mono": {
			Name: "Mono", Data: gomono.TTF, FixedWidth: true,
		}},
	}}
}

func TestCodeWrapPreservesWhitespaceTokensAndBreaksIndependentOfHighlight(t *testing.T) {
	e := codeWrapEngine(t)
	source := "    var value = \"" + strings.Repeat("x", 80) + "\"; // " + strings.Repeat("tail  ", 8)
	width := e.author.width("0123456789012345", 3, "Mono")
	var plain []string
	for _, language := range []string{"", "go"} {
		spans, err := authorCodeSpans(t.Context(), source, language)
		if err != nil {
			t.Fatal(err)
		}
		var inlines []Inline
		for _, span := range spans[0] {
			inlines = append(inlines, Inline{Text: span.text, Color: span.color})
		}
		segments := e.segments(inlines, 3*72/25.4, metricKey{font: "Mono", mono: true})
		lines, err := e.wrapAuthorCode(segments, width)
		if err != nil {
			t.Fatal(err)
		}
		rows := make([]string, 0, len(lines))
		colors := make(map[*creator.Color]bool)
		for _, line := range lines {
			var row strings.Builder
			for _, item := range line {
				row.WriteString(item.text)
				colors[item.color] = true
			}
			if e.lineWidth(line) > width+0.001 {
				t.Fatalf("代码折行后仍越界: %q", row.String())
			}
			rows = append(rows, row.String())
		}
		if len(rows) < 3 || strings.Join(rows, "") != source || !strings.HasPrefix(rows[0], "    ") {
			t.Fatalf("折行丢失文字、空白或缩进: %q", rows)
		}
		if language == "" {
			plain = rows
		} else if !reflect.DeepEqual(rows, plain) || len(colors) < 3 {
			t.Fatalf("高亮改变断点或折行丢失颜色: %q / %q / %d 色", rows, plain, len(colors))
		}
	}
}

func TestCodeWrapDoesNotSplitGraphemeClusters(t *testing.T) {
	e := codeWrapEngine(t)
	source := strings.Repeat("e\u0301", 20)
	item := atom{text: source, size: 3, key: metricKey{font: "Mono", mono: true}, width: e.author.width(source, 3, "Mono")}
	width := e.author.width("e\u0301", 3, "Mono") * 3.2
	lines, err := e.wrapAuthorCode([][]atom{{item}}, width)
	if err != nil {
		t.Fatal(err)
	}
	var restored strings.Builder
	for _, line := range lines {
		for i, part := range line {
			if i == 0 && unicode.IsMark([]rune(part.text)[0]) {
				t.Fatalf("组合字符被拆到下一行: %q", part.text)
			}
			restored.WriteString(part.text)
		}
	}
	if restored.String() != source {
		t.Fatalf("按字素折行改写了代码: %q", restored.String())
	}
}

func TestCodeWrapPreservesEmptyLineAndRejectsUnplaceableGrapheme(t *testing.T) {
	e := codeWrapEngine(t)
	lines, err := e.wrapAuthorCode(nil, 10)
	if err != nil || len(lines) != 1 || len(lines[0]) != 0 {
		t.Fatalf("内部空行没有保留: %v / %+v", err, lines)
	}
	item := atom{text: "W", size: 3, key: metricKey{font: "Mono"}, width: e.author.width("W", 3, "Mono")}
	if _, err := e.wrapAuthorCode([][]atom{{item}}, item.width/2); err == nil {
		t.Fatal("单字素无法放入版心时应明确报错")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	e.author.ctx = ctx
	if _, err := e.wrapAuthorCode([][]atom{{item}}, 10); err != context.Canceled { //nolint:errorlint // 验证转换链原样返回取消错误。
		t.Fatalf("折行取消错误被包装或吞掉: %v", err)
	}
}
