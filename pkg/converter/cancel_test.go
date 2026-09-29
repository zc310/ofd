package converter

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
)

func testOFDPath(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"helloworld.ofd", "sample.ofd", "intro.ofd"} {
		path := filepath.Join("..", "..", "test", "testdata", name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	t.Skip("没有可用的 OFD 测试文件")
	return ""
}

// testMultiPageDocument 解析一份真实 OFD，返回至少 minPages 页的文档。
//
// 不用合成文档：parser.Page 的关键字段未导出且需要 load 回调，包外造不出
// 一个能被渲染的页。用真实样本顺带覆盖了"解析 + 遍历"这条真实路径。
func testMultiPageDocument(t *testing.T, minPages int) ([]*render.Document, int) {
	t.Helper()
	candidates := []string{"multi_demo.ofd", "actions.ofd", "link.ofd", "helloworld.ofd"}
	for _, name := range candidates {
		path := filepath.Join("..", "..", "test", "testdata", name)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		ofd, err := parser.NewOFDWithOptions(path, parser.Options{})
		if err != nil {
			continue
		}
		t.Cleanup(func() { _ = ofd.Close() })
		documents := make([]*render.Document, 0, len(ofd.Documents))
		for _, doc := range ofd.Documents {
			documents = append(documents, render.NewDocumentWithDPI(color.Transparent, doc, 72))
		}
		if total := len(collectDocumentPages(documents)); total >= minPages {
			return documents, total
		}
	}
	t.Skipf("没有页数不少于 %d 的可用 OFD 样本", minPages)
	return nil, 0
}

// 已取消的 ctx 必须在入口就被挡住：不该白走一遍格式解析才发现。
func TestCancelledContextRejectedAtEntry(t *testing.T) {
	input := testOFDPath(t)
	var output bytes.Buffer
	cases := []struct {
		name string
		call func(ctx context.Context) error
	}{
		{"Convert", func(ctx context.Context) error { return Convert(ctx, "ofd", "pdf", input, &output) }},
		{"Encode", func(ctx context.Context) error { return Encode(ctx, "pdf", input, &output) }},
		{"PDF", func(ctx context.Context) error { return PDF(ctx, input, &output) }},
		{"Text", func(ctx context.Context) error { return Text(ctx, input, &output) }},
		{"Markdown", func(ctx context.Context) error { return Markdown(ctx, input, &output) }},
		{"HTML", func(ctx context.Context) error { return HTML(ctx, input, &output) }},
		{"Image", func(ctx context.Context) error {
			return Image(ctx, input, ImageWriter(func(int, image.Image) error { return nil }))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output.Reset()
			err := tc.call(testCancelled())
			if err == nil {
				t.Fatal("已取消的 ctx 应报错")
			}
			// 关键：原样返回 ctx.Err()，不能被包成"转换失败"。
			// runner 要靠 errors.Is 区分关停（重排）与超时（重试）。
			if !errors.Is(err, context.Canceled) {
				t.Errorf("应返回可识别的 context.Canceled，实际 %v", err)
			}
			if output.Len() != 0 {
				t.Errorf("被取消的转换不该产出内容，实际 %d 字节", output.Len())
			}
		})
	}
}

// 超时也应与取消区分开：runner 对两者的处置完全不同。
func TestDeadlineExceededDistinguishable(t *testing.T) {
	input := testOFDPath(t)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	var output bytes.Buffer
	err := Convert(ctx, "ofd", "pdf", input, &output)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("应返回 context.DeadlineExceeded，实际 %v", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Error("超时不应当被识别为取消")
	}
}

// 转换中途取消：第一页处理完就取消，后面的页不该再渲染。
//
// 这是这次改动的核心价值：没有检查点时，一份 1000 页的文档在第 1 页之后
// 被取消，仍会把剩下的 999 页全部渲染完——算力白扔，而调用方早已不需要结果。
func TestCancelStopsPageLoop(t *testing.T) {
	documents, total := testMultiPageDocument(t, 3)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rendered := 0
	writer := ImageWriter(func(page int, _ image.Image) error {
		rendered++
		if page == 1 {
			cancel()
		}
		return nil
	})
	err := ImageDocuments(ctx, documents, writer)
	if err == nil {
		t.Fatal("中途取消应报错")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("应返回 context.Canceled，实际 %v", err)
	}
	if rendered >= total {
		t.Errorf("取消后仍处理了全部 %d 页（实际 %d），检查点没生效", total, rendered)
	}
	if rendered > 2 {
		t.Errorf("取消点之后还多跑了 %d 页", rendered-1)
	}
}

// 未取消时同样的输入要能跑完全部页——确认检查点没有误伤正常路径。
func TestUncancelledRendersAllPages(t *testing.T) {
	documents, total := testMultiPageDocument(t, 3)
	rendered := 0
	writer := ImageWriter(func(int, image.Image) error {
		rendered++
		return nil
	})
	if err := ImageDocuments(ctxTODO, documents, writer); err != nil {
		t.Fatalf("正常渲染不应失败: %v", err)
	}
	if rendered != total {
		t.Errorf("未取消时应渲染全部 %d 页，实际 %d", total, rendered)
	}
}

// 未取消时行为不变。
func TestUncancelledConversionUnaffected(t *testing.T) {
	input := testOFDPath(t)
	var pdf bytes.Buffer
	if err := Convert(ctxTODO, "ofd", "pdf", input, &pdf); err != nil {
		t.Fatalf("正常转换不应失败: %v", err)
	}
	if !strings.HasPrefix(pdf.String(), "%PDF-") {
		t.Errorf("输出不是 PDF: %q", truncateForTest(pdf.Bytes()))
	}
	var text bytes.Buffer
	if err := Text(ctxTODO, input, &text); err != nil {
		t.Fatalf("正常文本提取不应失败: %v", err)
	}
	if text.Len() == 0 {
		t.Error("文本输出为空")
	}
}

// ctx 为 nil 时按 Background 处理，不应 panic。
func TestNilContextTreatedAsBackground(t *testing.T) {
	input := testOFDPath(t)
	var output bytes.Buffer
	//nolint:staticcheck // 故意传 nil，验证防御
	if err := Convert(nil, "ofd", "pdf", input, &output); err != nil {
		t.Fatalf("nil ctx 应按 Background 处理，实际 %v", err)
	}
	if output.Len() == 0 {
		t.Error("nil ctx 下仍应正常产出")
	}
}

// Context 访问器永不为 nil。
func TestConverterContextAccessor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conv := newConverter(ctx)
	if conv.Context() != ctx {
		t.Error("Context() 应返回传入的 ctx")
	}
	if err := conv.checkCancelled(); err != nil {
		t.Errorf("未取消时 checkCancelled 应返回 nil，实际 %v", err)
	}
	cancel()
	if err := conv.checkCancelled(); !errors.Is(err, context.Canceled) {
		t.Errorf("取消后应返回 context.Canceled，实际 %v", err)
	}
	// nil 接收者也不能 panic。
	var nilConv *Converter
	if nilConv.Context() == nil {
		t.Error("nil Converter 的 Context() 不应为 nil")
	}
	if err := nilConv.checkCancelled(); err != nil {
		t.Errorf("nil Converter 应视为未取消，实际 %v", err)
	}
}

// 并发转换各自独立：取消其中一个不影响其它。
func TestConcurrentConversionsIndependentCancellation(t *testing.T) {
	input := testOFDPath(t)
	const ok = 6
	var wg sync.WaitGroup
	errs := make(chan error, ok*2)
	for i := 0; i < ok; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out bytes.Buffer
			errs <- Convert(ctxTODO, "ofd", "text", input, &out)
		}()
	}
	for i := 0; i < ok/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out bytes.Buffer
			errs <- Convert(testCancelled(), "ofd", "text", input, &out)
		}()
	}
	wg.Wait()
	close(errs)
	success, cancelled := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			success++
		case errors.Is(err, context.Canceled):
			cancelled++
		default:
			t.Errorf("意外错误: %v", err)
		}
	}
	if success != ok {
		t.Errorf("未取消的 %d 个转换应全部成功，实际 %d", ok, success)
	}
	if cancelled != ok/2 {
		t.Errorf("取消的 %d 个应全部返回 Canceled，实际 %d", ok/2, cancelled)
	}
}

func truncateForTest(b []byte) string {
	if len(b) > 40 {
		return string(b[:40]) + "..."
	}
	return string(b)
}
