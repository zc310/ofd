package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const preserveFixture = "../../testdata/"

// TestPreserveRequiresOutput 确认转换必须显式给出输出路径。这是本工具唯一会
// 改写文件的子命令，默认路径一旦有误就会覆盖用户的原始文件。
func TestPreserveRequiresOutput(t *testing.T) {
	code := run([]string{"preserve", "--doc-type", "OFD-A", preserveFixture + "permissions.ofd"},
		&bytes.Buffer{}, &bytes.Buffer{})
	if code == exitOK {
		t.Error("缺少 --output 时不应成功")
	}
}

// TestPreserveDryRunRejectsOutput 确认 --dry-run 不接受输出路径。静默忽略用户
// 指定的输出路径，比报错更难排查。
func TestPreserveDryRunRejectsOutput(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ofd")
	code := run([]string{"preserve", "--dry-run", "-o", out, preserveFixture + "permissions.ofd"},
		&bytes.Buffer{}, &bytes.Buffer{})
	if code == exitOK {
		t.Error("--dry-run 配合 --output 时不应成功")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("--dry-run 不应产生输出文件")
	}
}

// TestPreserveDryRunReportsPlan 确认 --dry-run 列出计划改动且不写文件。
func TestPreserveDryRunReportsPlan(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"preserve", "--doc-type", "OFD-A", "--dry-run", preserveFixture + "permissions.ofd"},
		&stdout, &stderr)
	if code != exitOK {
		t.Fatalf("退出码 = %d，stderr: %s", code, stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{"GB/T 42133 6.2.2 a)", "Permissions", "dry-run"} {
		if !strings.Contains(got, want) {
			t.Errorf("输出缺少 %q:\n%s", want, got)
		}
	}
}

// TestPreserveWritesNewFile 确认转换写出新文件且输入不变。
func TestPreserveWritesNewFile(t *testing.T) {
	input := preserveFixture + "permissions.ofd"
	before, err := os.ReadFile(input)
	if err != nil {
		t.Fatalf("读取输入失败: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.ofd")
	var stdout, stderr bytes.Buffer
	code := run([]string{"preserve", "--doc-type", "OFD-A", "-o", out, input}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("退出码 = %d，stderr: %s", code, stderr.String())
	}
	after, err := os.ReadFile(input)
	if err != nil {
		t.Fatalf("读取输入失败: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("输入文件被修改，转换必须只产出新文件")
	}
	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("读取输出失败: %v", err)
	}
	if len(written) == 0 {
		t.Fatal("输出为空")
	}
	if bytes.Equal(written, before) {
		t.Error("输出与输入完全相同，转换没有生效")
	}
}

// TestPreserveRefusesToOverwriteInput 确认拒绝覆盖输入文件。
func TestPreserveRefusesToOverwriteInput(t *testing.T) {
	input := preserveFixture + "permissions.ofd"
	var stdout, stderr bytes.Buffer
	code := run([]string{"preserve", "--doc-type", "OFD-A", "-o", input, input}, &stdout, &stderr)
	if code == exitOK {
		t.Error("覆盖输入文件时不应成功")
	}
}

// TestPreserveBaseDocTypeProducesNothing 确认基础 profile 下不产出文件。
// 未声明 OFD-A 的文件若被擅自改造，就是无依据地损坏用户数据。
func TestPreserveBaseDocTypeProducesNothing(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ofd")
	var stdout, stderr bytes.Buffer
	code := run([]string{"preserve", "--doc-type", "OFD", "-o", out, preserveFixture + "permissions.ofd"},
		&stdout, &stderr)
	if code == exitOK {
		t.Error("基础 profile 无改动可做时不应按成功处理")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("不应产生输出文件")
	}
}
