package render

import (
	"testing"

	"github.com/zc310/ofd/internal/render/geom"
)

// TestRegisterBackendRejectsInvalid 回归注册表入参校验：空名、nil 工厂不能注册。
func TestRegisterBackendRejectsInvalid(t *testing.T) {
	factory := func(width, height float64, resolution geom.Resolution) (Backend, error) {
		return nil, nil
	}

	if err := RegisterBackend("", factory); err == nil {
		t.Error("空名称未返回错误")
	}
	if err := RegisterBackend("test-nil-factory", nil); err == nil {
		t.Error("nil 工厂未返回错误")
	}
	const name = "render-test-valid-backend"
	if err := RegisterBackend(name, factory); err != nil {
		t.Fatalf("合法后端注册失败: %v", err)
	}
	if err := RegisterBackend(name, factory); err == nil {
		t.Error("重复注册同名后端未返回错误")
	}
}
