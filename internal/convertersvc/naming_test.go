package convertersvc

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestOutputNamingSingle 单文件产物的命名。
func TestOutputNamingSingle(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		extension string
		want      string
	}{
		{"未指定用默认值", "", ".pdf", "output.pdf"},
		{"自定义基名", "invoice", ".pdf", "invoice.pdf"},
		{"基名带点", "report.final", ".pdf", "report.final.pdf"},
		{"已含正确扩展名不重复追加", "invoice.pdf", ".pdf", "invoice.pdf"},
		{"扩展名大小写不同也识别", "Invoice.PDF", ".pdf", "Invoice.pdf"},
		{"扩展名不匹配则原样拼接", "invoice.txt", ".pdf", "invoice.txt.pdf"},
		{"首尾空白被裁掉", "  invoice  ", ".pdf", "invoice.pdf"},
		{"显式指定 output 也照用", "output", ".pdf", "output.pdf"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := newOutputNaming(tc.requested, tc.extension)
			if err != nil {
				t.Fatalf("newOutputNaming: %v", err)
			}
			if got := n.single(); got != tc.want {
				t.Errorf("single() = %q，期望 %q", got, tc.want)
			}
		})
	}
}

// TestOutputNamingPage 逐页产物的命名。
func TestOutputNamingPage(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		extension string
		index     int
		want      string
	}{
		{"未指定维持既有行为", "", ".png", 1, "page-0001.png"},
		{"自定义基名替换 page", "thumb", ".png", 1, "thumb-0001.png"},
		{"页号补零到四位", "thumb", ".png", 42, "thumb-0042.png"},
		{"基名带点", "scan.v2", ".jpg", 1, "scan.v2-0001.jpg"},
		{"百页以上", "p", ".png", 123, "p-0123.png"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n, err := newOutputNaming(tc.requested, tc.extension)
			if err != nil {
				t.Fatalf("newOutputNaming: %v", err)
			}
			if got := n.page(tc.index); got != tc.want {
				t.Errorf("page(%d) = %q，期望 %q", tc.index, got, tc.want)
			}
		})
	}
}

// TestOutputNamingRejectsPathLike 基名只要可能变成路径就必须拒绝。
//
// 这是新增 `filename` 的全部安全边界：一旦放过含分隔符的值，调用方就能把
// 文件写到 output_dir 之外，任务隔离（output_dir/<job_id>/）形同虚设。
func TestOutputNamingRejectsPathLike(t *testing.T) {
	cases := []struct {
		name      string
		requested string
	}{
		{"父目录穿越", "../escape"},
		{"内层穿越", "sub/../../escape"},
		{"绝对路径", "/etc/passwd"},
		{"正斜杠", "a/b"},
		{"反斜杠", `a\b`},
		{"Windows 盘符", `C:\Windows\x`},
		{"当前目录", "."},
		{"父目录", ".."},
		{"空字节", "a\x00b"},
		{"换行", "a\nb"},
		{"回车", "a\rb"},
		{"制表符", "a\tb"},
		{"超长", strings.Repeat("x", maxBaseNameBytes+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := newOutputNaming(tc.requested, ".pdf"); err == nil {
				t.Errorf("newOutputNaming(%q) 应被拒绝", tc.requested)
			} else if !errors.Is(err, ErrBadRequest) {
				t.Errorf("错误应为 ErrBadRequest，实际 %v", err)
			}
		})
	}
}

// TestOutputNamingRejectsExtensionOnly 只剩扩展名时必须报错而不是产出无名文件。
func TestOutputNamingRejectsExtensionOnly(t *testing.T) {
	if _, err := newOutputNaming(".pdf", ".pdf"); err == nil {
		t.Error("只有扩展名的基名应被拒绝")
	}
}

// TestValidateOutputFileName 提交路径的提前校验与内部一致。
func TestValidateOutputFileName(t *testing.T) {
	if err := ValidateOutputFileName(""); err != nil {
		t.Errorf("空文件名应通过校验: %v", err)
	}
	if err := ValidateOutputFileName("   "); err != nil {
		t.Errorf("纯空白应按未指定处理: %v", err)
	}
	if err := ValidateOutputFileName("invoice"); err != nil {
		t.Errorf("正常基名应通过: %v", err)
	}
	// 带扩展名也通过——扩展名只在归一化时才处理，校验阶段不该拦。
	if err := ValidateOutputFileName("invoice.pdf"); err != nil {
		t.Errorf("带扩展名的基名应通过: %v", err)
	}
	if err := ValidateOutputFileName("../x"); err == nil {
		t.Error("穿越路径应在提交阶段就被拒")
	}
}

// TestFileNameStaysInsideJobDir 产物路径必须落在任务自己的目录里。
//
// 端到端地验一遍：请求的 filename 再怎么写，最终路径都不能越出
// <output_dir>/<job_id>/。
func TestFileNameStaysInsideJobDir(t *testing.T) {
	dir := t.TempDir()
	svc := New(dir, nil)
	res, err := svc.Run(t.Context(), Spec{
		Input:  Input{Kind: InputUpload, FileName: "a.ofd", Bytes: ofdSample(t)},
		Output: Output{Kind: OutputDir, Format: "pdf", FileName: "../../../etc/passwd"},
	})
	// 非法文件名应被拒，而不是悄悄落到别处。
	if err == nil {
		t.Fatalf("穿越文件名应被拒绝，实际产出了: %+v", res)
	}
	if !errors.Is(err, ErrBadRequest) {
		t.Errorf("错误应为 ErrBadRequest，实际 %v", err)
	}
}

// TestPageNamingStartsAtOne 第一页必须编号 1。
//
// 转换器的 Writer 回调给的页号已经是从 1 开始的（pages.go 里 len(pages)+1），
// 命名处再 +1 会让第一页变成 page-0002.png，而且缺了 0001——目录里凭空少一个
// 序号，任何按序号拼路径的下游都会错位。
func TestPageNamingStartsAtOne(t *testing.T) {
	dir := t.TempDir()
	svc := New(dir, nil)
	res, err := svc.Run(t.Context(), Spec{
		Input: Input{Kind: InputUpload, FileName: "a.ofd", Bytes: ofdSample(t)},
		Output: Output{Kind: OutputDir, Format: "png", FileName: "thumb",
			Dir: filepath.Join(dir, "out")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) == 0 {
		t.Fatal("逐页输出没有产出文件")
	}
	if res.Files[0].Name != "thumb-0001.png" {
		t.Errorf("首个产物 = %q，期望 thumb-0001.png", res.Files[0].Name)
	}
	// 编号必须连续，不能跳号。
	for i, file := range res.Files {
		want := fmt.Sprintf("thumb-%04d.png", i+1)
		if file.Name != want {
			t.Errorf("第 %d 个产物 = %q，期望 %q", i+1, file.Name, want)
		}
	}
}
