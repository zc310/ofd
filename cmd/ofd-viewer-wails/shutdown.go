package main

// shutdown 与原生文件操作串行释放会话及解密视图；窗口由宿主自身销毁。
func (d *Desktop) shutdown() {
	d.openMu.Lock()
	defer d.openMu.Unlock()
	d.unlock = nil
	d.printMu.Lock()
	if d.printing != nil && d.printing.timer != nil {
		d.printing.timer.Stop()
	}
	d.printing = nil
	d.printMu.Unlock()
	_ = d.document.Close()
	if d.unlockedView != nil {
		_ = d.unlockedView.Close()
		d.unlockedView = nil
	}
}
