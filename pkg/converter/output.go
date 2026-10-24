package converter

import "io"

// outputErrorRecorder 记录后端延迟返回的首次写入错误，供页循环及时终止。
// 不因取消拦截写入，PDF 失败路径仍按约定完成 Close 收尾。
type outputErrorRecorder struct {
	output io.Writer
	err    error
}

func (w *outputErrorRecorder) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.output.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}
