package converter

import (
	"errors"
	"testing"

	"github.com/zc310/ofd/internal/render"
)

// closeErrStub 是只关心 Close 行为的 PDF 写入器桩。
type closeErrStub struct {
	closed int
	err    error
}

func (s *closeErrStub) AddPage(render.VectorSurface, []render.PageLink) error { return nil }

func (s *closeErrStub) Close() error {
	s.closed++
	return s.err
}

var errCloseFailed = errors.New("Close 失败")

// TestClosePDFDocumentAlwaysCloses 守住 closePDFDocument 一定会调用 Close。
//
// PDF 的交叉引用表与 trailer 都由 Close 写出，不调用就会在输出里留下半截文件：
// 能打开但缺页面，或干脆无法解析。串行与并行两条导出路径都靠 defer 调用它，
// 覆盖取消、渲染失败、写页失败与正常结束四种退出方式。
func TestClosePDFDocumentAlwaysCloses(t *testing.T) {
	for _, initial := range []error{nil, errors.New("处理第 2 页失败")} {
		stub := &closeErrStub{}
		err := initial
		closePDFDocument(stub, &err)
		if stub.closed != 1 {
			t.Fatalf("初始错误 %v 时 Close 调用 %d 次，期望 1 次", initial, stub.closed)
		}
	}
}

// TestClosePDFDocumentReportsCloseFailureWhenNothingElseFailed 守住没有其它错误时
// 收尾失败能被报出来——写盘出错往往只在 Close 写 trailer 时才暴露。
func TestClosePDFDocumentReportsCloseFailureWhenNothingElseFailed(t *testing.T) {
	err := error(nil)
	closePDFDocument(&closeErrStub{err: errCloseFailed}, &err)
	if !errors.Is(err, errCloseFailed) {
		t.Fatalf("上报的错误 = %v，期望 %v", err, errCloseFailed)
	}
}

// TestClosePDFDocumentKeepsOriginalError 守住收尾失败不覆盖真正的失败原因。
//
// 转换失败的原因比收尾失败重要得多：把它包成「关闭 PDF 文档失败」会让调用方
// 误判，也没法据此决定是否重试。
func TestClosePDFDocumentKeepsOriginalError(t *testing.T) {
	original := errors.New("处理第 2 页失败")
	err := original
	closePDFDocument(&closeErrStub{err: errCloseFailed}, &err)
	if !errors.Is(err, original) {
		t.Fatalf("原始错误被收尾结果覆盖: %v", err)
	}
}
