package drawing

import (
	"errors"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestUnregisterFallbackRemovesCoreRegistryName(t *testing.T) {
	oldRegister, oldUnregister, oldNames := registerFallbackFontFn, unregisterFallbackFontFn, registeredFallbacks
	t.Cleanup(func() {
		registerFallbackFontFn, unregisterFallbackFontFn, registeredFallbacks = oldRegister, oldUnregister, oldNames
	})
	registeredFallbacks = nil
	registerFallbackFontFn = func([]byte, string, FontStyle) error { return nil }
	var unregisterErr error
	unregisterFallbackFontFn = func(string) error { return unregisterErr }
	if err := RegisterFallbackFont([]byte{1}, "removed", FontRegular); err != nil {
		t.Fatal(err)
	}
	if err := RegisterFallbackFont([]byte{1}, "kept", FontRegular); err != nil {
		t.Fatal(err)
	}
	unregisterErr = errors.New("failed")
	if err := UnregisterFallbackFont("removed"); err == nil || len(RegisteredFallbackFamilies()) != 2 {
		t.Fatal("失败的注销改变了字体列表")
	}
	unregisterErr = nil
	if err := UnregisterFallbackFont("removed"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(RegisteredFallbackFamilies(), []string{"kept"}) {
		t.Fatal("注销后核心注册列表仍包含旧字体名")
	}
	if err := RegisterFallbackFont([]byte{1}, "removed", FontRegular); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(RegisteredFallbackFamilies(), []string{"kept", "removed"}) {
		t.Fatal("同名重注册没有恢复当前列表")
	}
}

func TestSlowFallbackBackendDoesNotBlockListOrOtherFamilies(t *testing.T) {
	for _, unregister := range []bool{false, true} {
		name := "register"
		if unregister {
			name = "unregister"
		}
		t.Run(name, func(t *testing.T) {
			oldRegister, oldUnregister, oldNames := registerFallbackFontFn, unregisterFallbackFontFn, registeredFallbacks
			defer func() {
				registerFallbackFontFn, unregisterFallbackFontFn, registeredFallbacks = oldRegister, oldUnregister, oldNames
			}()
			registeredFallbacks = []string{"existing"}
			if unregister {
				registeredFallbacks = append(registeredFallbacks, "slow")
			}
			entered, resume, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			release := sync.OnceFunc(func() { close(resume) })
			backend := func(family string) error {
				// 后端可以读取核心列表，不应与外层全局锁自锁。
				RegisteredFallbackFamilies()
				if family == "slow" {
					close(entered)
					<-resume
				}
				return nil
			}
			registerFallbackFontFn = func(_ []byte, family string, _ FontStyle) error { return backend(family) }
			unregisterFallbackFontFn = backend
			go func() {
				if unregister {
					done <- UnregisterFallbackFont("slow")
				} else {
					done <- RegisterFallbackFont([]byte{1}, "slow", FontRegular)
				}
			}()
			defer func() {
				release()
				if err := <-done; err != nil {
					t.Errorf("慢速后端操作失败：%v", err)
				}
			}()
			awaitFallbackSignal(t, entered)
			observed := make(chan []string, 1)
			go func() { observed <- RegisteredFallbackFamilies() }()
			select {
			case snapshot := <-observed:
				if slices.Contains(snapshot, "slow") != unregister {
					t.Fatal("后端操作尚未完成就改变了注册列表")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("读取列表被后端解析阻塞")
			}
			otherDone := make(chan error, 1)
			go func() { otherDone <- RegisterFallbackFont([]byte{1}, "other", FontRegular) }()
			select {
			case err := <-otherDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("其他字体族无法进入后端")
			}
		})
	}
}

func TestFallbackSameFamilyOperationsKeepBackendAndListOrder(t *testing.T) {
	oldRegister, oldUnregister, oldNames := registerFallbackFontFn, unregisterFallbackFontFn, registeredFallbacks
	defer func() {
		registerFallbackFontFn, unregisterFallbackFontFn, registeredFallbacks = oldRegister, oldUnregister, oldNames
	}()
	registeredFallbacks = nil
	entered, resume := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	registerDone, unregisterDone := make(chan error, 1), make(chan error, 1)
	unregisterEntered := make(chan struct{})
	registerFallbackFontFn = func([]byte, string, FontStyle) error {
		close(entered)
		<-resume
		return nil
	}
	unregisterFallbackFontFn = func(string) error {
		close(unregisterEntered)
		if !slices.Contains(RegisteredFallbackFamilies(), "same") {
			return errors.New("同族注册完成前注销已进入后端")
		}
		return nil
	}
	go func() { registerDone <- RegisterFallbackFont([]byte{1}, "same", FontRegular) }()
	defer func() {
		release()
		if err := <-registerDone; err != nil {
			t.Errorf("同族注册失败：%v", err)
		}
	}()
	awaitFallbackSignal(t, entered)
	go func() { unregisterDone <- UnregisterFallbackFont("same") }()
	defer func() {
		release()
		if err := <-unregisterDone; err != nil {
			t.Errorf("同族注销失败：%v", err)
		}
	}()
	// 确认注销已登记等待，不能靠固定延时判断它是否开始执行。
	deadline := time.Now().Add(3 * time.Second)
	for {
		registeredFallbackMu.Lock()
		operation := registeredFallbackOps["same"]
		waiting := operation != nil && operation.users == 2
		registeredFallbackMu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("注销操作没有开始等待同族操作锁")
		}
		runtime.Gosched()
	}
	select {
	case <-unregisterEntered:
		t.Fatal("同族注册未结束时注销已进入后端")
	default:
	}
	release()
	awaitFallbackSignal(t, unregisterEntered)
	// 注册/注销返回值由 defer 消费；等待锁表删除代表两个完整操作均已结束。
	for {
		registeredFallbackMu.Lock()
		_, retained := registeredFallbackOps["same"]
		registeredFallbackMu.Unlock()
		if !retained {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("已完成操作仍保留族锁条目")
		}
		runtime.Gosched()
	}
	if slices.Contains(RegisteredFallbackFamilies(), "same") {
		t.Fatal("最终注销后列表仍保留同族名称")
	}
}

func awaitFallbackSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("后端调用未完成预期阶段")
	}
}
