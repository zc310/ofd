// Package browser 通过 chromedp 驱动 Chrome/Chromium 把 HTML/MHTML 渲染为 PDF。
// 它使用浏览器原生打印引擎，支持纸张尺寸、背景和外部资源访问控制。
package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// EnvChrome 是覆盖 Chrome/Chromium 可执行文件路径的环境变量名。
const EnvChrome = "OFD_CHROME"

// DefaultTimeout 是单次渲染的默认超时时间。
const DefaultTimeout = 2 * time.Minute

// mmPerInch 是 1 英寸对应的毫米数。
const mmPerInch = 25.4

// Options 控制单次渲染行为。
type Options struct {
	// Chrome 是可执行文件路径；为空时自动查找。
	Chrome string
	// Timeout 是渲染超时；为 0 时使用 DefaultTimeout。
	Timeout time.Duration
	// Width 和 Height 是纸张尺寸（毫米）。
	Width  float64
	Height float64
	// Landscape 为真时横向打印。
	Landscape bool
	// PrintBackground 为真时打印背景颜色和图片。
	PrintBackground bool
	// AllowRemoteResources 为真时允许加载外部资源；默认禁止。
	AllowRemoteResources bool
	// NoSandbox 为真时给 Chrome 加 --no-sandbox。
	NoSandbox bool
	// TempDir 是临时文件根目录；为空时使用系统临时目录。
	TempDir string
}

// chromeNames 是 PATH 中可能出现的 Chrome/Chromium 可执行文件名。
var chromeNames = []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome", "headless_shell"}

// FindChrome 定位 Chrome/Chromium 可执行文件。显式路径与 OFD_CHROME 会被校验；
// 否则在 PATH 中查找，仍未命中时返回空路径，交由 chromedp 在其内置的
// PATH 与平台常见安装目录中查找。显式路径无效时返回明确错误。
func FindChrome(explicit string) (string, error) {
	if path := strings.TrimSpace(explicit); path != "" {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
		return "", fmt.Errorf("指定的 Chrome 可执行文件不存在: %s", path)
	}
	if path := strings.TrimSpace(os.Getenv(EnvChrome)); path != "" {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
		return "", fmt.Errorf("%s 指向的可执行文件不存在: %s", EnvChrome, path)
	}
	for _, name := range chromeNames {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", nil
}

// ConvertFileToPDF 把本地 HTML/MHTML 文件渲染为 PDF 字节。
func ConvertFileToPDF(ctx context.Context, path string, options Options) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("HTML 渲染输入文件为空")
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return nil, fmt.Errorf("HTML 渲染输入文件不可读: %s", path)
	}
	chrome, err := FindChrome(options.Chrome)
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
	workDir, err := os.MkdirTemp(options.TempDir, "ofd-browser-")
	if err != nil {
		return nil, fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer removeAllRetry(workDir)

	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("解析输入路径失败: %w", err)
	}

	allocatorOptions := append([]chromedp.ExecAllocatorOption(nil), chromedp.DefaultExecAllocatorOptions[:]...)
	allocatorOptions = append(allocatorOptions,
		chromedp.UserDataDir(filepath.Join(workDir, "profile")),
		chromedp.Flag("headless", "new"),
		chromedp.Flag("allow-file-access-from-files", true),
	)
	// 显式路径或 PATH 命中时使用该路径；否则交由 chromedp 自行查找。
	if chrome != "" {
		allocatorOptions = append(allocatorOptions, chromedp.ExecPath(chrome))
	}
	if options.NoSandbox {
		allocatorOptions = append(allocatorOptions, chromedp.NoSandbox)
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(runCtx, allocatorOptions...)
	defer allocatorCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()

	width := options.Width
	height := options.Height
	if width <= 0 || height <= 0 {
		width, height = 210, 297
	}
	printParams := page.PrintToPDF().
		WithPaperWidth(width / mmPerInch).
		WithPaperHeight(height / mmPerInch).
		WithLandscape(options.Landscape).
		WithPrintBackground(options.PrintBackground).
		WithPreferCSSPageSize(false)

	actions := []chromedp.Action{}
	if !options.AllowRemoteResources {
		// 默认只允许本地与 data: 资源，阻断所有外部网络请求，保证安全与确定性。
		// CDP 使用 WHATWG URLPattern 语法，模式必须包含 host 与 path（如
		// "http://*:*/*"），写成 "http://*" 不会命中任何请求。
		actions = append(actions,
			network.Enable(),
			network.SetBlockedURLs().WithURLPatterns([]*network.BlockPattern{
				{URLPattern: "http://*:*/*", Block: true},
				{URLPattern: "https://*:*/*", Block: true},
				{URLPattern: "ftp://*:*/*", Block: true},
				{URLPattern: "ws://*:*/*", Block: true},
				{URLPattern: "wss://*:*/*", Block: true},
			}),
		)
	}
	var pdfData []byte
	actions = append(actions,
		chromedp.Navigate("file://"+filepath.ToSlash(absolute)),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if err := waitForLoad(ctx); err != nil {
				return err
			}
			var err error
			pdfData, _, err = printParams.Do(ctx)
			return err
		}),
	)
	if err := chromedp.Run(browserCtx, actions...); err != nil {
		if runCtx.Err() != nil {
			return nil, fmt.Errorf("Chrome 渲染超时（%s）", timeout)
		}
		if chrome == "" {
			return nil, fmt.Errorf("未找到 Chrome/Chromium，请安装后重试，或通过 --chrome 或 %s 指定可执行文件: %w", EnvChrome, err)
		}
		return nil, fmt.Errorf("Chrome 渲染失败: %w", err)
	}
	if len(pdfData) == 0 {
		return nil, errors.New("Chrome 生成的 PDF 为空")
	}
	return pdfData, nil
}

// waitForLoad 等待文档加载完成，确保内联资源已就绪后再打印。
func waitForLoad(ctx context.Context) error {
	return chromedp.Evaluate(`new Promise(resolve => {
        if (document.readyState === 'complete') { resolve(true); return; }
        window.addEventListener('load', () => resolve(true), { once: true });
    })`, nil).Do(ctx)
}

// removeAllRetry 删除临时目录。Chrome 退出后可能仍在写用户数据目录，
// 导致首次 RemoveAll 因目录非空失败，因此短暂重试几次。
func removeAllRetry(dir string) {
	for attempt := 0; attempt < 5; attempt++ {
		if err := os.RemoveAll(dir); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}
