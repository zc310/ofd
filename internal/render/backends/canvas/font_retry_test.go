package canvas

import (
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zc310/ofd/internal/models"
	"golang.org/x/image/font/gofont/goregular"
)

func waitFontPublish(t *testing.T) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		buffer := make([]byte, 1<<20)
		n := runtime.Stack(buffer, true)
		for _, stack := range strings.Split(string(buffer[:n]), "\n\n") {
			if strings.Contains(stack, "[sync.Mutex.Lock]") && strings.Contains(stack, "(*Fonts).LoadFont") &&
				!strings.Contains(stack, "defaultFallbackFont") {
				return true
			}
		}
		runtime.Gosched()
	}
	return false
}

func TestFontLoadIgnoresUnrelatedGlyphChanges(t *testing.T) {
	testFontLoadChanges(t, 2, 1, nil)
}

func TestFontLoadStopsAfterRepeatedOwnGlyphChanges(t *testing.T) {
	testFontLoadChanges(t, 1, maxFontLoadAttempts, errFontChangedDuringLoad)
}

func testFontLoadChanges(t *testing.T, changedID models.StRefID, changes int, wantErr error) {
	t.Helper()
	fonts := NewFonts(nil)
	defer fonts.Close()
	name := t.Name()
	if err := registerFallbackFont(goregular.TTF, name, FontRegular); err != nil {
		t.Fatal(err)
	}
	defer UnregisterFallbackFont(name)
	if err := fonts.UseFallbackFont(name); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	fontCacheMu.Lock()
	go func() { _, err := fonts.LoadFont(1); done <- err }()
	for i := 0; i < changes; i++ {
		if !waitFontMutex(t, "defaultFallbackFont") {
			fontCacheMu.Unlock()
			t.Fatal("未能暂停字体加载")
		}
		fonts.RegisterGlyphs(changedID, map[rune]uint16{'A': uint16(i + 1)})
		fonts.mu.Lock()
		fontCacheMu.Unlock()
		blocked := waitFontPublish(t)
		fontCacheMu.Lock()
		fonts.mu.Unlock()
		if !blocked {
			fontCacheMu.Unlock()
			t.Fatal("未能暂停字体结果发布")
		}
	}
	// 保持全局锁，若错误地开始另一轮加载就会停在 defaultFallbackFont。
	var got error
	select {
	case got = <-done:
	case <-time.After(3 * time.Second):
		fontCacheMu.Unlock()
		<-done
		t.Fatal("字体加载在允许轮次后仍继续重试")
	}
	fontCacheMu.Unlock()
	if !errors.Is(got, wantErr) {
		t.Fatalf("加载结果 = %v，期望 %v", got, wantErr)
	}
	if wantErr != nil {
		if _, err := fonts.LoadFont(1); err != nil {
			t.Fatalf("停止配置更新后应该可以重新加载：%v", err)
		}
	}
}
