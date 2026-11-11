package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	readersession "github.com/zc310/ofd/internal/reader/session"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/crypt"
)

func TestLaunchPathAcceptsSingleFileAndForwardedWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{"中文 空格.OFD"}, {"--", "-文件.ofd"}} {
		path, err := launchPath(args, dir)
		if err != nil || filepath.Dir(path) != dir {
			t.Fatalf("启动路径 %v = %q, %v", args, path, err)
		}
	}
	for _, args := range [][]string{{"--unknown"}, {"a.ofd", "b.ofd"}, {"a.pdf"}, {"-文件.ofd"}} {
		if _, err := launchPath(args, dir); err == nil {
			t.Fatalf("非法启动参数被接受: %v", args)
		}
	}
}

func TestLaunchEncryptedFileRequiresUnlockAndKeepsOldDocumentOnCancel(t *testing.T) {
	d, old := capabilityDesktop(t, capabilityPackage(t, ""))
	var encrypted bytes.Buffer
	if _, err := crypt.Encrypt(t.Context(), capabilityPackage(t, ""), &encrypted,
		crypt.EncryptOptions{Recipients: []crypt.EncryptRecipient{{UserName: "User", Password: []byte("secret")}}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "加密.ofd")
	if err := os.WriteFile(path, encrypted.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	d.queueLaunch(path)
	challenge, err := d.OpenLaunch()
	if err != nil || challenge.Challenge == "" || challenge.PagePrefix != "" {
		t.Fatalf("系统打开未进入解锁流程: %+v, %v", challenge, err)
	}
	d.CancelUnlock(challenge.Challenge)
	if _, err := d.PageInfo(old.PagePrefix, 0); err != nil {
		t.Fatalf("取消启动文件解锁不能关闭旧文档: %v", err)
	}
}

func TestCommandPassesFileAndDistinguishesInputErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "文档.ofd")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	called := false
	if code := command([]string{path}, &stdout, &stderr, func(input string) error {
		called = true
		if input != path {
			t.Fatalf("启动文件 = %q", input)
		}
		return nil
	}); code != 0 || !called || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("启动结果 = %d, %s", code, stderr.String())
	}
	if code := command([]string{path + ".ofd"}, &stdout, &stderr, func(string) error {
		t.Fatal("缺失文件不能启动窗口")
		return nil
	}); code != 1 {
		t.Fatalf("缺失文件退出码 = %d", code)
	}
	if err := validateLaunchFile(filepath.Dir(path) + ".ofd"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("文件错误 = %v", err)
	}
}

func TestLaunchQueueSurvivesFrontendReadinessAndConsumesOnce(t *testing.T) {
	doc := &readersession.Service{}
	t.Cleanup(func() { _ = doc.Close() })
	d := &Desktop{document: doc}
	path := filepath.Join(t.TempDir(), "启动.ofd")
	data, err := creator.Marshal(creator.Document{ID: "launch", Pages: []creator.Page{{}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	d.queueLaunch(path)
	for range 2 {
		if !d.PendingLaunch() {
			t.Fatal("探测不能消费尚未处理的启动请求")
		}
	}
	opened, err := d.OpenLaunch()
	if err != nil || opened.Name != "启动.ofd" || len(opened.Pages) != 1 || d.inputPath != path {
		t.Fatalf("系统打开结果 = %+v, %v", opened, err)
	}
	if d.PendingLaunch() {
		t.Fatal("已打开请求未被消费")
	}
	if opened, err := d.OpenLaunch(); opened != nil || err != nil {
		t.Fatalf("重复消费 = %+v, %v", opened, err)
	}
	d.queueLaunch(path + ".ofd")
	d.queueLaunch(path)
	if _, err := d.OpenLaunch(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("失效请求 = %v", err)
	}
	if opened, err := d.OpenLaunch(); err != nil || opened == nil {
		t.Fatalf("失败后未处理后续请求: %v", err)
	}
}
