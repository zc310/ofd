package flow

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v3"
	"github.com/alecthomas/chroma/v3/lexers"
)

func TestCodeHighlightUsesDeclaredLanguageAndPreservesPhysicalLines(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"go", "golang", "GO", "js", "javascript", "python", "json", "yaml", "sh"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			source := "\treturn 42 // note\r\n\n  value = \"hello\""
			lines, err := codeSpans(t.Context(), source, language)
			if err != nil {
				t.Fatal(err)
			}
			if len(lines) != 3 {
				t.Fatalf("高亮改变了物理行数: %d", len(lines))
			}
			for i, want := range splitCodeLines(source) {
				var got strings.Builder
				for _, span := range lines[i] {
					got.WriteString(span.text)
				}
				if got.String() != expandCodeTabs(want) {
					t.Fatalf("高亮改写了第 %d 行: %q", i, got.String())
				}
			}
		})
	}
}

func TestCodeHighlightKeepsMultilineCommentsAndStrings(t *testing.T) {
	lines, err := codeSpans(t.Context(), "/* first\nsecond */\nvar s = `first\nsecond`", "go")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 4 || len(lines[0]) != 1 || len(lines[1]) != 1 || len(lines[3]) == 0 {
		t.Fatalf("跨行词元被错误拆分: %+v", lines)
	}
	if color := lines[1][0].color; color.R != 0x57 || color.G != 0x60 || color.B != 0x6a {
		t.Fatalf("续行注释没有保持 GitHub 灰色: %+v", color)
	}
	for _, span := range lines[3] {
		if color := span.color; color.R != 0x0a || color.G != 0x30 || color.B != 0x69 {
			t.Fatalf("续行字符串没有保持 GitHub 深蓝色: %+v", color)
		}
	}
}

func TestCodeWithoutKnownLanguageStaysGrayAndDoesNotGuess(t *testing.T) {
	for _, language := range []string{"", " ", "unknown-ofd-language", "text", "plaintext", "txt"} {
		t.Run(language, func(t *testing.T) {
			lines, err := codeSpans(t.Context(), "func main() {\n    return 42\n}", language)
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range lines {
				if len(line) != 1 {
					t.Fatalf("未标注语言的代码不应分词: %+v", line)
				}
				if color := line[0].color; color.R != 0x57 || color.G != 0x60 || color.B != 0x6a {
					t.Fatalf("无语言或未知语言应使用灰字: %+v", color)
				}
			}
		})
	}
}

// 用可控词法器覆盖 Chroma 文档约定的 panic 错误边界和禁止改写原文的约束。
type testLexer struct {
	chroma.Lexer

	tokenise func() (iter.Seq[chroma.Token], error)
}

func (l testLexer) Tokenise(*chroma.TokeniseOptions, string) (iter.Seq[chroma.Token], error) {
	return l.tokenise()
}

func TestCodeHighlightRejectsLexerErrorsAndChangedText(t *testing.T) {
	for _, kind := range []string{"error", "panic", "iterator_panic", "changed", "missing"} {
		t.Run(kind, func(t *testing.T) {
			lexer := testLexer{tokenise: func() (iter.Seq[chroma.Token], error) {
				switch kind {
				case "error":
					return nil, errors.New("分词失败")
				case "panic":
					panic("词法器失败")
				case "iterator_panic":
					return func(func(chroma.Token) bool) { panic("迭代失败") }, nil
				case "changed":
					return chroma.Literator(chroma.Token{Type: chroma.Text, Value: "changed"}), nil
				default:
					return chroma.Literator(chroma.Token{Type: chroma.Text, Value: "code"}), nil
				}
			}}
			if _, err := highlightCode(t.Context(), lexer, "code\n", 1); err == nil {
				t.Fatal("分词错误或改写原文未报错")
			}
		})
	}
}

// v3 不再使用 EOF 哨兵，零值词元不应让遍历提前结束。
func TestCodeHighlightConsumesV3SequenceUntilCompletion(t *testing.T) {
	lexer := testLexer{tokenise: func() (iter.Seq[chroma.Token], error) {
		return chroma.Literator(chroma.Token{}, chroma.Token{Type: chroma.Text, Value: "code\n"}), nil
	}}
	lines, err := highlightCode(t.Context(), lexer, "code\n", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || len(lines[0]) != 1 || lines[0][0].text != "code" {
		t.Fatalf("词元序列被错误提前截断: %+v", lines)
	}
}

func TestCodeHighlightCancellationStopsV3Producer(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stopped := false
	lexer := testLexer{tokenise: func() (iter.Seq[chroma.Token], error) {
		return func(yield func(chroma.Token) bool) {
			defer func() { stopped = true }()
			if !yield(chroma.Token{Type: chroma.Text, Value: "co"}) {
				return
			}
			cancel()
			if yield(chroma.Token{Type: chroma.Text, Value: "de\n"}) {
				t.Error("取消后仍请求后续词元")
			}
		}, nil
	}}
	if _, err := highlightCode(ctx, lexer, "code\n", 1); err != context.Canceled { //nolint:errorlint // 验证原样返回取消错误。
		t.Fatalf("迭代期间取消错误被包装或吞掉: %v", err)
	}
	if !stopped {
		t.Fatal("取消后没有结束词元生产者")
	}
}

func TestCodeHighlightReturnsCancellationUnchanged(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := codeSpans(ctx, "return 42", "go"); err != context.Canceled { //nolint:errorlint // 转换链要求原样返回取消错误。
		t.Fatalf("取消错误被包装或吞掉: %v", err)
	}
	if _, err := highlightCode(ctx, lexers.Get("go"), "return 42\n", 1); err != context.Canceled { //nolint:errorlint // 验证未被包装。
		t.Fatalf("分词取消错误被包装或吞掉: %v", err)
	}
}
