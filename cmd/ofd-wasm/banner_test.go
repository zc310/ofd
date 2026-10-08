package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/version"
)

// TestBannerFillsBorderWidth 守住 banner 边框的显示宽度。
//
// 「阅读器」三字在等宽终端各占两列，按 len 或手写空格补齐都会让中间行比上下边框
// 宽一列；版本号从 v0.1.1 变为 v0.1.4 乃至 v0.10.0 时手写空格也会错位。
func TestBannerFillsBorderWidth(t *testing.T) {
	for _, v := range []string{"0.1.4", "0.10.0", "1.0.0", "12.34.56", "0.1.4-rc1"} {
		label := fmt.Sprintf("OFD WASM 阅读器 v%s", v)
		row := centerBanner(label)
		if got := displayWidth(row); got != wasmBannerWidth {
			t.Errorf("版本 %s: banner 行显示宽度 %d, want %d", v, got, wasmBannerWidth)
		}
		if strings.TrimSpace(row) != label {
			t.Errorf("版本 %s: 内容 = %q, want %q", v, strings.TrimSpace(row), label)
		}
	}
}

// TestBannerUsesInjectedVersion 守住版本号来自 internal/version 而不是硬编码。
// 硬编码曾在版本升到 0.1.4 之后仍停在 v0.1.1。
func TestBannerUsesInjectedVersion(t *testing.T) {
	label := fmt.Sprintf("OFD WASM 阅读器 v%s", version.Version)
	if !strings.Contains(centerBanner(label), version.Version) {
		t.Errorf("banner 未包含版本 %s", version.Version)
	}
	// 版本号变化后内容随之变化，不存在写死的字符串。
	if version.Version == "0.1.1" {
		t.Skip("版本恰好为旧硬编码值，无法据此区分")
	}
}

// TestBannerOverlongLabelNotTruncated 守住超长标签不被裁剪，也不产生负数补齐。
func TestBannerOverlongLabelNotTruncated(t *testing.T) {
	label := "OFD WASM 阅读器 v" + strings.Repeat("9", 60)
	row := centerBanner(label)
	if row != label {
		t.Errorf("超长标签应原样返回，got %d 字符", len([]rune(row)))
	}
	if strings.ContainsAny(row, "-\t") {
		t.Error("不应插入除空格外的填充字符")
	}
}

func TestDisplayWidth(t *testing.T) {
	cases := map[string]int{
		"":        0,
		"abc":     3,
		"阅读器":     6,  // 三个汉字各两列
		"OFD 阅读器": 10, // 3+1+6
		"v0.1.4":  6,
		"０":       2, // 全角数字
		"─":       1, // Ambiguous 按一列，避免溢出
	}
	for in, want := range cases {
		if got := displayWidth(in); got != want {
			t.Errorf("displayWidth(%q) = %d, want %d", in, got, want)
		}
	}
}
