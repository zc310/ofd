package webreader

import (
	"context"
	"io"
	"runtime"
	"time"
)

// renderCheckpoint 在浏览器中让出事件循环，使取消消息能在无输出时被处理。
// 不可取消的上下文不引入调度等待；单次底层绘制或编码仍需执行到阶段边界。
func renderCheckpoint(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if runtime.GOOS == "js" && runtime.GOARCH == "wasm" && ctx.Done() != nil {
		timer := time.NewTimer(time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
	}
	return ctx.Err()
}

// renderWriter 保留首次输出错误，避免 PDF 后端延迟报错后仍继续渲染后续页。
type renderWriter struct {
	ctx context.Context
	out io.Writer
	err error
}

func (w *renderWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.out.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}
