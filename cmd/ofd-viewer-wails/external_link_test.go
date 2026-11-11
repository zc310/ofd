package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	readersession "github.com/zc310/ofd/internal/reader/session"
)

func TestExternalLinksRejectUnsafeSchemesCredentialsControlsAndInvalidPorts(t *testing.T) {
	for _, raw := range []string{"", "javascript:alert(1)", "file:///etc/passwd", "mailto:user@example.test", "//example.test", "https://user:secret@example.test", "https://example.test\\@evil.test", "https://example.test\n", "https://example.test/\u202eabc", "https://example.test:99999", "https://example.test:", "https:///missing", "https://" + strings.Repeat("a", 4096)} {
		if _, err := checkedExternalLink(raw); !errors.Is(err, errExternalLink) {
			t.Fatalf("不安全地址未拒绝 %q: %v", raw, err)
		}
	}
	for raw, want := range map[string]string{
		"HTTPS://example.test/":           "https://example.test/",
		"https://example.test/a?q=1#part": "https://example.test/a?q=1#part",
		"http://[::1]:8080/a":             "http://[::1]:8080/a",
		"https://例子.测试/":                  "https://xn--fsqu00a.xn--0zwm56d/",
	} {
		if got, err := checkedExternalLink(raw); err != nil || got != want {
			t.Fatalf("地址规范化 = %q, %v，期望 %q", got, err, want)
		}
	}
}

func TestExternalLinkRequiresConfirmationAndRechecksSessionAfterDialog(t *testing.T) {
	for _, mode := range []string{"允许", "拒绝", "取消", "关闭文档", "切换文档", "打开失败"} {
		t.Run(mode, func(t *testing.T) {
			d, opened := capabilityDesktop(t, capabilityPackage(t, ""))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			confirmations, opens := 0, 0
			failure := errors.New("浏览器启动失败")
			got, err := d.openExternalWith(ctx, opened.PagePrefix, 0, "https://example.test/path", func(_ context.Context, uri string) (bool, error) {
				confirmations++
				if uri != "https://example.test/path" {
					t.Fatal(uri)
				}
				switch mode {
				case "取消":
					cancel()
				case "关闭文档":
					if err := d.Close(opened.PagePrefix); err != nil {
						t.Fatal(err)
					}
				case "切换文档":
					if _, err := d.SelectVersion(opened.PagePrefix, 0, "v1"); err != nil { //nolint:contextcheck // 版本切换是同步发布接口；取消在确认后单独检查。
						t.Fatal(err)
					}
				}
				return mode != "拒绝", nil
			}, func(string) error {
				opens++
				if mode == "打开失败" {
					return failure
				}
				return nil
			})
			if confirmations != 1 {
				t.Fatal("未显示确认")
			}
			switch mode {
			case "允许":
				if err != nil || !got || opens != 1 {
					t.Fatalf("确认后未打开: %v, %v", got, err)
				}
			case "拒绝":
				if err != nil || got || opens != 0 {
					t.Fatal("拒绝后仍打开")
				}
			case "取消":
				if !errors.Is(err, context.Canceled) || got || opens != 0 {
					t.Fatalf("取消错误 = %v", err)
				}
			case "关闭文档", "切换文档":
				if !errors.Is(err, readersession.ErrStaleDocument) || got || opens != 0 {
					t.Fatalf("旧确认仍有效: %v", err)
				}
			case "打开失败":
				if !errors.Is(err, failure) || got || opens != 1 {
					t.Fatalf("未传播浏览器错误: %v", err)
				}
			}
		})
	}
}

func TestInvalidOrStaleExternalLinksNeverShowDialog(t *testing.T) {
	d, opened := capabilityDesktop(t, capabilityPackage(t, ""))
	confirm := func(context.Context, string) (bool, error) { t.Fatal("不应显示确认框"); return false, nil }
	open := func(string) error { t.Fatal("不应打开浏览器"); return nil }
	if _, err := d.openExternalWith(t.Context(), opened.PagePrefix, 0, "file:///etc/passwd", confirm, open); !errors.Is(err, errExternalLink) {
		t.Fatal(err)
	}
	if err := d.Close(opened.PagePrefix); err != nil {
		t.Fatal(err)
	}
	if _, err := d.openExternalWith(t.Context(), opened.PagePrefix, 0, "https://example.test", confirm, open); !errors.Is(err, readersession.ErrStaleDocument) {
		t.Fatal(err)
	}
}
