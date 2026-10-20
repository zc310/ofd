package converter

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

// cancelOnFirstWrite 在输出写下第一个字节时触发取消。PDF 的头部与页面对象都在
// 写页时落盘，第一次 Write 即对应第一页开始写出，取消点因此与写出进度绑定，不
// 依赖 Err() 的调用次数——并行路径里 Err() 由多个 worker 并发调用，计数既有数据
// 竞争，触发取消的调用落在哪个页面上也不确定（落在第一页就会让输出为空，测试
// 随机失败）。
type cancelOnFirstWrite struct {
	w      io.Writer
	cancel context.CancelFunc
	once   sync.Once
}

func (c *cancelOnFirstWrite) Write(p []byte) (int, error) {
	c.once.Do(c.cancel)
	return c.w.Write(p)
}

// TestPDFExportFinalizesDocumentWhenCancelledMidway 守住转换在写页途中失败时，
// 输出仍是一份结构完整的 PDF。
//
// PDF 的交叉引用表与 trailer 都由 Close 写出。若失败路径不收尾，输出会停在半截：
// 能打开但缺页面，或无法解析。这里在第一页开始写出后触发取消，并断言输出以
// %%EOF 结尾、带有 startxref。
//
// 同时覆盖一个曾经的崩溃：此时页面上的链接已经为第 2 页登记了锚点，而第 2 页永远
// 不会被写出，收尾时按页序取引用会越界。
func TestPDFExportFinalizesDocumentWhenCancelledMidway(t *testing.T) {
	for _, parallel := range []bool{false, true} {
		name := "串行"
		if parallel {
			name = "并行"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var output bytes.Buffer
			// 第一次写出即取消：此后下一次取消检查就会中止转换，此时第一页已经写出。
			writer := &cancelOnFirstWrite{w: &output, cancel: cancel}
			opts := []Option{}
			if parallel {
				opts = append(opts, PDFParallel(true))
			}
			err := PDF(ctx, "../../testdata/ofd-creator/links.ofd", writer, opts...)
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
