package flow

import (
	"context"
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v3"
	"github.com/alecthomas/chroma/v3/lexers"
	"github.com/alecthomas/chroma/v3/styles"
	"github.com/zc310/ofd/pkg/creator"
)

type codeSpan struct {
	text  string
	color *creator.Color
}

// 先按原有规则展开制表符，再对完整代码分词，保留跨行注释、字符串及空行。
// 未声明语言时不猜测；未知语言和分词失败都退回灰色纯文本，不影响代码内容。
func codeSpans(ctx context.Context, text, language string) ([][]codeSpan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lines := splitCodeLines(text)
	plain := make([][]codeSpan, len(lines))
	gray := &creator.Color{R: 0x57, G: 0x60, B: 0x6a}
	for i, line := range lines {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lines[i] = expandCodeTabs(line)
		plain[i] = []codeSpan{{text: lines[i], color: gray}}
	}
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" || len(lines) == 0 {
		return plain, nil
	}
	lexer := lexers.Get(language)
	if lexer == nil || lexer.Config().Name == "plaintext" {
		return plain, ctx.Err()
	}
	highlighted, err := highlightCode(ctx, lexer, strings.Join(lines, "\n")+"\n", len(lines))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return plain, nil //nolint:nilerr // 高亮是可选装饰；分词失败降级为原始灰字，不中断转换。
	}
	return highlighted, nil
}

// Chroma 的词元序列仍通过 panic 传播迭代错误，在该边界恢复并由调用者降级为纯文本。
// 使用 GitHub 浅色主题的前景色，不引入词元背景、字重或斜体，避免改变等宽布局。
//
//nolint:nonamedreturns // defer 恢复第三方词法器 panic 时需要设置返回错误。
func highlightCode(ctx context.Context, lexer chroma.Lexer, text string, lineCount int) (lines [][]codeSpan, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			lines, err = nil, fmt.Errorf("%w：代码分词失败: %v", errContent, failure)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	iterator, err := lexer.Tokenise(&chroma.TokeniseOptions{State: "root"}, text)
	if err != nil {
		return nil, err
	}
	lines = make([][]codeSpan, lineCount)
	style := styles.Get("github")
	foreground := &creator.Color{R: 0x1f, G: 0x23, B: 0x28}
	palette := make(map[chroma.Colour]*creator.Color)
	offset, row := 0, 0
	for token := range iterator {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// 高亮只能着色，禁止词法器增删或改写输入文字。
		if !strings.HasPrefix(text[offset:], token.Value) {
			return nil, fmt.Errorf("%w：代码分词改变了原文", errContent)
		}
		offset += len(token.Value)
		color := foreground
		if value := style.Get(token.Type).Colour; value.IsSet() && token.Type != chroma.Error && !token.Type.InCategory(chroma.Text) {
			color = palette[value]
			if color == nil {
				color = &creator.Color{R: value.Red(), G: value.Green(), B: value.Blue()}
				palette[value] = color
			}
		}
		for _, part := range strings.SplitAfter(token.Value, "\n") {
			if content := strings.TrimSuffix(part, "\n"); content != "" && row < lineCount {
				lines[row] = append(lines[row], codeSpan{text: content, color: color})
			}
			if strings.HasSuffix(part, "\n") {
				row++
			}
		}
	}
	if offset != len(text) || row != lineCount {
		return nil, fmt.Errorf("%w：代码分词丢失文字或换行", errContent)
	}
	return lines, nil
}
