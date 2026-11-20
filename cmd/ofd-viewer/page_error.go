package main

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
)

// pageOperationFailure 在 recover 所在的延迟调用中使用，保留原始异常位置。
// 界面只展示函数和行号，完整堆栈写入诊断日志，不把整段堆栈塞进状态栏。
func pageOperationFailure(recovered any) error {
	location := pagePanicLocation()
	err := fmt.Errorf("%w: %v", errPageOperation, recovered)
	if location != "" {
		err = fmt.Errorf("%w (%s)", err, location)
	}
	slog.Error("页面操作异常", "error", err, "stack", string(debug.Stack()))
	return err
}

func pagePanicLocation() string {
	var callers [32]uintptr
	n := runtime.Callers(2, callers[:])
	frames := runtime.CallersFrames(callers[:n])
	inPanic := false
	for {
		frame, more := frames.Next()
		if frame.Function == "runtime.gopanic" {
			inPanic = true
		} else if inPanic && !strings.HasPrefix(frame.Function, "runtime.") {
			return fmt.Sprintf("%s %s:%d", frame.Function, filepath.Base(frame.File), frame.Line)
		}
		if !more {
			return ""
		}
	}
}
