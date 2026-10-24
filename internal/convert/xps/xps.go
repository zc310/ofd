// Package xps 通过 xpstopdf 命令把 XPS 文档转换为 PDF。
package xps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/zc310/ofd/internal/convert/process"
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
func ConvertToPDF(ctx context.Context, inputPath string, options Options) (data []byte, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(inputPath) == "" {
		return nil, errors.New("XPS 转换输入文件为空")
	}
	input, err := os.Open(inputPath)
	if err != nil {
		return nil, fmt.Errorf("XPS 转换输入文件不可读: %w", err)
	}
	defer input.Close()
	if info, err := input.Stat(); err != nil {
		return nil, fmt.Errorf("检查 XPS 转换输入失败: %w", err)
	} else if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("XPS 转换输入必须是普通文件: %s", inputPath)
	}
	xpstopdf, err := FindXpstopdf(options.Xpstopdf)
	if err != nil {
		return nil, err
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// 取消错误按转换链约定原样返回；同一超时也覆盖 PDF 校验。
	defer func() {
		if ctx.Err() != nil {
			data, err = nil, ctx.Err()
		} else if runCtx.Err() == context.DeadlineExceeded {
			data, err = nil, fmt.Errorf("xpstopdf 转换超时（%s）: %w", timeout, context.DeadlineExceeded)
		}
	}()

	workDir, err := os.MkdirTemp(options.TempDir, "ofd-xps-")
	if err != nil {
		return nil, fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(workDir)
	localInput := filepath.Join(workDir, "input.xps")
	if err := copyFile(input, localInput); err != nil {
		return nil, fmt.Errorf("准备 XPS 输入失败: %w", err)
	}
	outputPath := filepath.Join(workDir, "output.pdf")

	command := exec.CommandContext(runCtx, xpstopdf, localInput, outputPath)
	command.Dir = workDir
	process.Configure(command)
	commandOutput, runErr := command.CombinedOutput()

	message := strings.TrimSpace(string(commandOutput))
	if runErr != nil {
		if message != "" {
			runErr = fmt.Errorf("%w: %s", runErr, message)
		}
		return nil, fmt.Errorf("xpstopdf 转换失败: %w", runErr)
	}
	data, readErr := os.ReadFile(outputPath)
	if readErr != nil || len(data) == 0 {
		if readErr != nil {
			if message != "" {
				return nil, fmt.Errorf("xpstopdf 未生成 PDF: %w: %s", readErr, message)
			}
			return nil, fmt.Errorf("xpstopdf 未生成 PDF: %w", readErr)
		}
		return nil, fmt.Errorf("xpstopdf 生成的 PDF 为空")
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return nil, errors.New("xpstopdf 生成的 PDF 无效")
	}
	if err := validatePDF(runCtx, data); err != nil {
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
		return fmt.Errorf("校验 PDF 结构失败: %w", err)
	}
	if pdfContext.PageCount == 0 {
		return errors.New("PDF 没有页面")
	}
	return nil
}

// source 是已经打开并检查过的输入，避免检查后按路径重新打开另一文件。
func copyFile(source io.Reader, target string) error {
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, source)
	return errors.Join(copyErr, output.Close())
}
