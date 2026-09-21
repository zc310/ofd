package render

import (
	"testing"

	"github.com/tdewolff/canvas"
)

// TestRegisterBackendRejectsInvalid 回归注册表入参校验：空名、nil 工厂以及
// 内置的 canvas 名都不能注册，避免遮蔽始终可用的 canvas 后端。
func TestRegisterBackendRejectsInvalid(t *testing.T) {
	factory := func(width, height float64, resolution canvas.Resolution) (Backend, error) {
		return newCanvasRasterBackend(width, height, resolution), nil
	}

	if err := RegisterBackend("", factory); err == nil {
		t.Error("空名称未返回错误")
	}
	if err := RegisterBackend("test-nil-factory", nil); err == nil {
		t.Error("nil 工厂未返回错误")
	}
	if err := RegisterBackend(BackendCanvas, factory); err == nil {
		t.Error("注册内置 canvas 后端未返回错误")
	}
	if err := RegisterBackend("test-valid", factory); err != nil {
		t.Fatalf("合法后端注册失败: %v", err)
	}
	if err := RegisterBackend("test-valid", factory); err == nil {
		t.Error("重复注册同名后端未返回错误")
	}
}
