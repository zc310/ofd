package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/emmansun/gmsm/sm3"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/spec"
)

func TestRunVersionAndHelp(t *testing.T) {
	for _, flag := range []string{"-v", "--version", "-h", "--help"} {
		t.Run(flag, func(t *testing.T) {
			clearSigningEnvironment(t)
			stdout, err := runWithInput(t, []string{flag}, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{demoProvider, demoVersion, producer} {
				if !bytes.Contains(stdout, []byte(want)) {
					t.Errorf("输出应包含 %q，实际 %q", want, stdout)
				}
			}
			if flag == "-v" || flag == "--version" {
				want := fmt.Sprintf("%s %s (%s)\n", demoProvider, demoVersion, producer)
				if string(stdout) != want {
					t.Errorf("版本输出 = %q，期望 %q", stdout, want)
				}
			} else {
				for _, want := range []string{"Signature.xml", "SignedValue.dat", "--digital", "OFD_SIGN_ID", "OFD_SIGN_TIME"} {
					if !bytes.Contains(stdout, []byte(want)) {
						t.Errorf("帮助应说明 %q", want)
					}
				}
			}
		})
	}
}

func TestRunRejectsUnknownArgumentWithoutOutput(t *testing.T) {
	clearSigningEnvironment(t)
	stdout, err := runWithInput(t, []string{"--unknown"}, []byte("payload"))
	if err == nil || !strings.Contains(err.Error(), "未知参数") || !strings.Contains(err.Error(), "--unknown") {
		t.Fatalf("应返回包含未知参数的诊断，实际 %v", err)
	}
	if len(stdout) != 0 {
		t.Fatalf("参数错误时不应写出签名，实际 %q", stdout)
	}
}

func TestRunSignsStandardInput(t *testing.T) {
	for _, digital := range []bool{false, true} {
		t.Run(fmt.Sprintf("digital=%t", digital), func(t *testing.T) {
			clearSigningEnvironment(t)
			// --digital 应覆盖外部传入的签章类型。
			t.Setenv("OFD_SIGN_TYPE", spec.SigTypeSeal)
			var args []string
			if digital {
				args = []string{"--digital"}
			}
			input := []byte("<Signature>stdin payload</Signature>")
			stdout, err := runWithInput(t, args, input)
			if err != nil {
				t.Fatal(err)
			}
			if digital {
				result, err := parser.VerifyDigitalSignedValue(input, stdout, nil)
				if err != nil {
					t.Fatal(err)
				}
				if !result.Valid || !result.Outer.Valid {
					t.Fatalf("普通签名应绑定标准输入: %+v", result)
				}
			} else {
				parsed, err := parser.ParseSignedValue(stdout)
				if err != nil {
					t.Fatal(err)
				}
				result, err := parser.VerifySESSignedValue(parsed)
				if err != nil {
					t.Fatal(err)
				}
				if !result.Valid {
					t.Fatalf("默认模式应写出可验签的 SES: %+v", result)
				}
				digest := sm3.Sum(input)
				if !bytes.Equal(parsed.SES.TBS.DataHash.Bytes, digest[:]) {
					t.Fatal("默认模式的签章摘要应绑定完整的标准输入")
				}
			}
		})
	}
}

func TestMainReportsUnknownArgumentAndExitCode(t *testing.T) {
	const subprocessEnv = "OFD_SIGNER_DEMO_TEST_UNKNOWN_ARGUMENT"
	if os.Getenv(subprocessEnv) == "1" {
		os.Args = []string{demoProvider, "--unknown"}
		main()
		t.Fatal("未知参数应使 main 退出")
	}

	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMainReportsUnknownArgumentAndExitCode$")
	command.Env = append(os.Environ(), subprocessEnv+"=1")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
		t.Fatalf("退出码应为 1，实际错误 %v，stderr=%q", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "未知参数") || !strings.Contains(stderr.String(), "--unknown") {
		t.Fatalf("标准错误应包含参数诊断，实际 %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("错误诊断不应污染标准输出，实际 %q", stdout.String())
	}
}

// runWithInput 临时替换命令入口的参数和标准流；使用临时文件避免管道容量造成阻塞。
// 本目录中替换标准流或环境变量的测试不能使用 t.Parallel。
func runWithInput(t *testing.T, args []string, input []byte) ([]byte, error) {
	t.Helper()
	directory := t.TempDir()
	stdin, err := os.CreateTemp(directory, "stdin-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close() })
	if _, err := stdin.Write(input); err != nil {
		t.Fatal(err)
	}
	if _, err := stdin.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	stdout, err := os.CreateTemp(directory, "stdout-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdout.Close() })

	oldArgs, oldStdin, oldStdout := os.Args, os.Stdin, os.Stdout
	os.Args, os.Stdin, os.Stdout = append([]string{demoProvider}, args...), stdin, stdout
	defer func() { os.Args, os.Stdin, os.Stdout = oldArgs, oldStdin, oldStdout }()
	runError := run()
	if _, err := stdout.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatal(err)
	}
	return output, runError
}
