// Package office 通过 LibreOffice 命令行把 Office 文档（docx/doc/odt/rtf/wps/
// pptx/xlsx 等）转换为 PDF。转换依赖目标机器已安装 LibreOffice，本包只做进程
// 调用与临时文件管理，不引入任何第三方依赖。
package office

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// EnvSoffice 是覆盖 LibreOffice 可执行文件路径的环境变量名。
const EnvSoffice = "OFD_SOFFICE"

// DefaultTimeout 是单次转换的默认超时时间。
const DefaultTimeout = 2 * time.Minute

// 常见安装路径，按平台排列；PATH 查找优先于这些路径。
var commonPaths = map[string][]string{
	"windows": {
		`C:\Program Files\LibreOffice\program\soffice.exe`,
		`C:\Program Files (x86)\LibreOffice\program\soffice.exe`,
	},
	"darwin": {
		"/Applications/LibreOffice.app/Contents/MacOS/soffice",
	},
}

// Options 控制单次转换行为。
type Options struct {
	// Soffice 是可执行文件路径；为空时自动查找。
	Soffice string
	// Timeout 是转换超时；为 0 时使用 DefaultTimeout。
	Timeout time.Duration
	// Filter 覆盖 LibreOffice 的导出过滤器；为空时使用通用 pdf。
	Filter string
	// TempDir 是临时文件根目录；为空时使用系统临时目录。
	TempDir string
}

// FindSoffice 定位 LibreOffice 可执行文件。查找顺序为：显式路径、
// OFD_SOFFICE 环境变量、PATH 中的 soffice/libreoffice、平台常见安装路径。
// 找不到时返回错误，调用方应据此给出可操作的提示。
func FindSoffice(explicit string) (string, error) {
	if path := strings.TrimSpace(explicit); path != "" {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
		return "", fmt.Errorf("指定的 LibreOffice 可执行文件不存在: %s", path)
	}
	if path := strings.TrimSpace(os.Getenv(EnvSoffice)); path != "" {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
		return "", fmt.Errorf("%s 指向的可执行文件不存在: %s", EnvSoffice, path)
	}
	for _, name := range []string{"soffice", "libreoffice"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	for _, path := range commonPaths[runtime.GOOS] {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("未找到 LibreOffice，请安装后重试，或通过 --soffice 或 %s 指定 soffice 可执行文件", EnvSoffice)
}

// ConvertToPDF 把 inputPath 转换为 PDF 并返回 PDF 字节。inputPath 必须是
// 可读的本地文件；转换在独立临时目录和独立用户配置目录中进行，互不干扰。
func ConvertToPDF(ctx context.Context, inputPath string, options Options) ([]byte, error) {
	if strings.TrimSpace(inputPath) == "" {
		return nil, errors.New("Office 转换输入文件为空")
	}
	if info, err := os.Stat(inputPath); err != nil || info.IsDir() {
		return nil, fmt.Errorf("Office 转换输入文件不可读: %s", inputPath)
	}
	soffice, err := FindSoffice(options.Soffice)
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

	workDir, err := os.MkdirTemp(options.TempDir, "ofd-office-")
	if err != nil {
		return nil, fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(workDir)

	// 输入复制到临时目录，避免原始路径中的特殊字符影响命令行，也便于输出定位。
	localInput := filepath.Join(workDir, "input"+strings.ToLower(filepath.Ext(inputPath)))
	if err := copyFile(inputPath, localInput); err != nil {
		return nil, fmt.Errorf("准备转换输入失败: %w", err)
	}
	outDir := filepath.Join(workDir, "out")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return nil, fmt.Errorf("创建输出目录失败: %w", err)
	}
	profileDir := filepath.Join(workDir, "profile")

	filter := strings.TrimSpace(options.Filter)
	if filter == "" {
		filter = "pdf"
	}
	args := []string{
		"--headless",
		"--norestore",
		"--nolockcheck",
		"--nodefault",
		"--nologo",
		"--nofirststartwizard",
		"-env:UserInstallation=" + fileURL(profileDir),
		"--convert-to", filter,
		"--outdir", outDir,
		localInput,
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(runCtx, soffice, args...)
	command.Dir = workDir
	// 独立进程组，超时或取消时可以连同 soffice 派生的子进程一起结束。
	command.SysProcAttr = processGroupAttr()
	command.Cancel = func() error {
		return killProcessGroup(command)
	}
	output, runErr := command.CombinedOutput()

	// LibreOffice 有时在失败时仍返回 0，必须校验输出文件本身。
	pdfPath, pdfErr := findPDF(outDir)
	if pdfErr != nil {
		if runCtx.Err() != nil {
			return nil, fmt.Errorf("LibreOffice 转换超时（%s）", timeout)
		}
		message := strings.TrimSpace(string(output))
		if runErr != nil {
			return nil, fmt.Errorf("LibreOffice 转换失败: %w: %s", runErr, message)
		}
		return nil, fmt.Errorf("LibreOffice 未生成 PDF: %s", message)
	}
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("读取转换结果失败: %w", err)
	}
	if len(data) == 0 {
		return nil, errors.New("LibreOffice 生成的 PDF 为空")
	}
	return data, nil
}

// findPDF 在输出目录中查找转换结果。转换成功后通常只生成一个 PDF 文件。
func findPDF(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".pdf") {
			return filepath.Join(dir, entry.Name()), nil
		}
	}
	return "", errors.New("输出目录中没有 PDF")
}

func copyFile(source, target string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0600)
}

// fileURL 把本地路径转换为 file:// URL，供 -env:UserInstallation 使用。
func fileURL(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}
	slashed := filepath.ToSlash(absolute)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return "file://" + slashed
}
