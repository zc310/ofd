// Package xps 通过 xpstopdf 命令把 XPS 文档转换为 PDF。
package xps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

const (
	// EnvXpstopdf 是覆盖 xpstopdf 可执行文件路径的环境变量名。
	EnvXpstopdf = "OFD_XPSTOPDF"
	// DefaultTimeout 是单次 XPS 转换的默认超时时间。
	DefaultTimeout = 2 * time.Minute
)

// Options 控制 XPS 转换行为。
type Options struct {
	// Xpstopdf 是 xpstopdf 可执行文件路径；为空时自动查找。
	Xpstopdf string
	// Timeout 是转换超时时间；为 0 时使用 DefaultTimeout。
	Timeout time.Duration
	// TempDir 是临时文件根目录；为空时使用系统临时目录。
	TempDir string
}

// FindXpstopdf 定位 xpstopdf。查找顺序为显式路径、OFD_XPSTOPDF 和 PATH。
func FindXpstopdf(explicit string) (string, error) {
	if path := strings.TrimSpace(explicit); path != "" {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
		return "", fmt.Errorf("指定的 xpstopdf 可执行文件不存在: %s", path)
	}
	if path := strings.TrimSpace(os.Getenv(EnvXpstopdf)); path != "" {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
		return "", fmt.Errorf("%s 指向的可执行文件不存在: %s", EnvXpstopdf, path)
	}
	if path, err := exec.LookPath("xpstopdf"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("未找到 xpstopdf，请安装 libgxps 后重试，或通过 --xpstopdf 或 %s 指定可执行文件", EnvXpstopdf)
}

// ConvertToPDF 把 XPS 文件转换为 PDF 并返回 PDF 字节。输入必须是可读的本地文件。
func ConvertToPDF(ctx context.Context, inputPath string, options Options) ([]byte, error) {
	if strings.TrimSpace(inputPath) == "" {
		return nil, errors.New("XPS 转换输入文件为空")
	}
	if info, err := os.Stat(inputPath); err != nil || info.IsDir() {
		return nil, fmt.Errorf("XPS 转换输入文件不可读: %s", inputPath)
	}
	xpstopdf, err := FindXpstopdf(options.Xpstopdf)
	if err != nil {
		return nil, err
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if ctx == nil {
		ctx = context.Background()
	}

	workDir, err := os.MkdirTemp(options.TempDir, "ofd-xps-")
	if err != nil {
		return nil, fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(workDir)
	localInput := filepath.Join(workDir, "input.xps")
	if err := copyFile(inputPath, localInput); err != nil {
		return nil, fmt.Errorf("准备 XPS 输入失败: %w", err)
	}
	outputPath := filepath.Join(workDir, "output.pdf")

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(runCtx, xpstopdf, localInput, outputPath)
	command.Dir = workDir
	command.SysProcAttr = processGroupAttr()
	command.Cancel = func() error { return killProcessGroup(command) }
	commandOutput, runErr := command.CombinedOutput()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if runCtx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("xpstopdf 转换超时（%s）", timeout)
	}
	data, readErr := os.ReadFile(outputPath)
	if readErr != nil || len(data) == 0 {
		message := strings.TrimSpace(string(commandOutput))
		if runErr != nil {
			return nil, fmt.Errorf("xpstopdf 转换失败: %w: %s", runErr, message)
		}
		if readErr != nil {
			return nil, fmt.Errorf("xpstopdf 未生成 PDF: %s", message)
		}
		return nil, fmt.Errorf("xpstopdf 生成的 PDF 为空")
	}
	if runErr != nil {
		return nil, fmt.Errorf("xpstopdf 转换失败: %w: %s", runErr, strings.TrimSpace(string(commandOutput)))
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return nil, errors.New("xpstopdf 生成的 PDF 无效")
	}
	if err := validatePDF(ctx, data); err != nil {
		return nil, fmt.Errorf("xpstopdf 生成的 PDF 无效: %w", err)
	}
	return data, nil
}

func validatePDF(ctx context.Context, data []byte) error {
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	pdfContext, err := api.ReadContext(ctx, bytes.NewReader(data), conf)
	if err != nil {
		return fmt.Errorf("读取 PDF 失败: %w", err)
	}
	if err := api.ValidateContext(ctx, pdfContext); err != nil {
		pdfContext.Info = nil
		if retryErr := api.ValidateContext(ctx, pdfContext); retryErr != nil {
			return fmt.Errorf("校验 PDF 结构失败: %w", retryErr)
		}
	}
	if pdfContext.PageCount == 0 {
		return errors.New("PDF 没有页面")
	}
	return nil
}

func copyFile(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0600)
}
