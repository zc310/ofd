package main

import (
	"context"
	"runtime"
)

func launchWebURL(ctx context.Context, raw string, results chan<- error, wake func()) {
	uri, ok := webURL(raw)
	if !ok {
		return
	}
	open := func() {
		if err := openWebURL(ctx, uri); err != nil && ctx.Err() == nil {
			select {
			case results <- err:
				wake()
			case <-ctx.Done():
			default:
				// 连续失败合并为已有提示，不能阻塞浏览器的界面线程。
			}
		}
	}
	if runtime.GOOS == "js" {
		// 浏览器要求尽量在用户交互期间打开窗口，不能延后到后台任务。
		open()
	} else {
		go open()
	}
}
