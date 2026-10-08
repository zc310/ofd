package converter

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// cancelAfterN 前 n 次 Err() 报 nil、之后报 Canceled，用来在已经写出若干页之后
// 触发失败。
type cancelAfterN struct {
	context.Context
	limit int
	calls int
}

func (c *cancelAfterN) Err() error {
	c.calls++
	if c.calls <= c.limit {
		return nil
	}
	return context.Canceled
}

func (c *cancelAfterN) Done() <-chan struct{} { return nil }

// 两条导出路径的取消检查点位置不同——入口、解析前后、页循环各查一次，并行路径还
// 在每批派发前多查一次——因此让第一页写出、第二页开始前失败所需的 Err() 调用次数
// 不一样。这些值只依赖当前的检查点分布；调用次数整体变化时本用例会失败，届时
// 需要重新确认阈值，而不是放宽断言。
const (
	cancelAfterFirstPageSerial   = 5
	cancelAfterFirstPageParallel = 6
)

// TestPDFExportFinalizesDocumentWhenCancelledMidway 守住转换在写页途中失败时，
// 输出仍是一份结构完整的 PDF。
//
// PDF 的交叉引用表与 trailer 都由 Close 写出。若失败路径不收尾，输出会停在半截：
// 能打开但缺页面，或无法解析。这里用会在第一页写出后失败的可控 context 触发该路径，
// 并断言输出以 %%EOF 结尾、带有 startxref。
//
// 同时覆盖一个曾经的崩溃：此时页面上的链接已经为第 2 页登记了锚点，而第 2 页永远
// 不会被写出，收尾时按页序取引用会越界。
func TestPDFExportFinalizesDocumentWhenCancelledMidway(t *testing.T) {
	for _, parallel := range []bool{false, true} {
		name, limit := "串行", cancelAfterFirstPageSerial
		if parallel {
			name, limit = "并行", cancelAfterFirstPageParallel
		}
		t.Run(name, func(t *testing.T) {
			ctx := &cancelAfterN{Context: context.Background(), limit: limit}
			var output bytes.Buffer
			opts := []Option{}
			if parallel {
				opts = append(opts, PDFParallel(true))
			}
			err := PDF(ctx, "../../testdata/links.ofd", &output, opts...)
			if err == nil {
				t.Fatal("被取消的转换应当报错")
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("错误 = %v，期望 context.Canceled", err)
			}
			if output.Len() == 0 {
				t.Fatal("第一页已写出，输出不应为空")
			}
			body := output.String()
			if !strings.Contains(body, "%%EOF") {
				t.Fatalf("输出缺少 %%EOF，未完成收尾:\n%s", tail(body, 200))
			}
			if !strings.Contains(body, "startxref") {
				t.Fatalf("输出缺少 startxref:\n%s", tail(body, 200))
			}
		})
	}
}

// tail 返回字节串末尾 n 个字符，便于失败时展示。
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
