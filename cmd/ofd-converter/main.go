// 命令 ofd-converter 将 OFD 文档转换为 PDF、文本、Markdown 或图像等格式。
package main

import (
	"errors"
	"fmt"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zip"

	"github.com/spf13/cobra"
	"github.com/zc310/ofd/pkg/converter"
	// 注册 "ofd" 输出格式（PDF→OFD）；按需引入，避免默认链接 pdfcpu。
	_ "github.com/zc310/ofd/pkg/converter/pdfimport"
	// 注册 Markdown→OFD 导入器（输入格式 "md"）。
	_ "github.com/zc310/ofd/pkg/converter/mdimport"
	// 注册 Office（doc/docx/odt/rtf/wps/pptx/xlsx 等）导入器与→PDF 转换器。
	_ "github.com/zc310/ofd/pkg/converter/officeimport"
	// 注册 HTML/MHTML 导入器与→PDF 转换器（chromedp + Chrome/Chromium）。
	_ "github.com/zc310/ofd/pkg/converter/htmlimport"
)

const (
	defaultDPI           = 150
	defaultBgColor       = "white"
	defaultWorkers       = 4
	defaultOfficeWorkers = 2
)

var (
	ErrInvalidFormat   = errors.New("不支持的输出格式")
	ErrNoInput         = errors.New("未指定输入文件")
	ErrInputOutputSame = errors.New("输入文件和输出路径不能相同")
)

type options struct {
	input             string
	output            string
	inputDir          string
	outputDir         string
	format            string
	from              string
	htmlFormat        string
	dpi               int
	page              int
	bg                string
	dir               bool
	workers           int
	externalWorkers   int
	soffice           string
	officeTimeout     int
	chrome            string
	chromeNoSandbox   bool
	tempDir           string
	paper             string
	landscape         bool
	noPrintBackground bool
	allowRemote       bool
	markdownTables    bool
	recursive         bool
	overwrite         bool
	skipExisting      bool
	help              bool
}

func main() {
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if opts.help {
		return
	}
	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, "转换失败:", err)
		os.Exit(1)
	}
}

func parseArgs(args []string) (*options, error) {
	opts := &options{dpi: defaultDPI, bg: defaultBgColor, htmlFormat: "png", workers: defaultWorkers, externalWorkers: defaultOfficeWorkers, recursive: true, overwrite: true}
	var output, format string
	args = normalizeConverterArgs(args)
	root := &cobra.Command{
		Use:           "ofd-converter [flags] input.ofd [output]",
		Short:         "OFD 文档转换命令行工具",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, positional []string) error {
			batchMode := opts.inputDir != "" || opts.outputDir != ""
			if batchMode {
				if opts.inputDir == "" || opts.outputDir == "" {
					return errors.New("批量转换必须同时指定 --input-dir 和 --output-dir")
				}
				if output != "" {
					return errors.New("批量转换请使用 --output-dir，不能同时指定 -o 或 --output")
				}
				if len(positional) != 0 {
					return errors.New("批量转换不能再指定位置参数输入文件")
				}
			} else {
				if len(positional) == 0 {
					return ErrNoInput
				}
				if len(positional) > 2 {
					return errors.New("最多只能指定输入文件和输出路径")
				}
				opts.input = positional[0]
			}
			if output != "" {
				opts.output = output
			} else if len(positional) >= 2 {
				opts.output = positional[1]
			}
			opts.format = format
			return nil
		},
	}
	root.SetArgs(args)
	root.SetOut(os.Stdout)
	root.SetErr(io.Discard)
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		opts.help = true
		fmt.Fprintln(cmd.OutOrStdout(), "ofd-converter - OFD 文档转换命令行工具")
		fmt.Fprintln(cmd.OutOrStdout(), "用法:")
		fmt.Fprintln(cmd.OutOrStdout(), "  ofd-converter [选项] <输入文件> [输出文件或目录]")
		fmt.Fprintln(cmd.OutOrStdout(), "  ofd-converter [选项] --input-dir <输入目录> --output-dir <输出目录>")
		fmt.Fprintln(cmd.OutOrStdout(), "")
		flags := cmd.Flags()
		flags.SetOutput(cmd.OutOrStdout())
		flags.PrintDefaults()
	})
	flags := root.Flags()
	flags.StringVarP(&output, "output", "o", "", "输出文件路径或目录，多页图片时可为 .zip 文件或目录")
	flags.StringVar(&opts.inputDir, "input-dir", "", "批量转换的输入目录")
	flags.StringVar(&opts.outputDir, "output-dir", "", "批量转换的输出目录")
	flags.StringVar(&format, "format", "", "输出格式: ofd, pdf, txt, md, markdown, html, png, jpg, svg, eps, tex")
	flags.StringVar(&opts.from, "from", "", "输入格式（可选）: pdf, md, docx, doc, odt, rtf, wps, pptx, xlsx, mhtml, html 等；缺省按输入文件扩展名推断")
	flags.StringVar(&opts.htmlFormat, "html-format", opts.htmlFormat, "HTML 页面格式: png, jpg, svg")
	flags.IntVar(&opts.dpi, "dpi", opts.dpi, "输出分辨率 (1-1200)")
	flags.IntVar(&opts.page, "page", opts.page, "指定全局页码 (从 1 开始)，0 表示全部文档体页面")
	flags.StringVar(&opts.bg, "bg", opts.bg, "背景颜色: transparent, white, black")
	flags.BoolVar(&opts.dir, "dir", opts.dir, "不压缩，将多页图片直接保存到输出目录下的多个文件")
	flags.IntVar(&opts.workers, "workers", opts.workers, "批量转换并发数，默认 4")
	flags.IntVar(&opts.externalWorkers, "external-workers", opts.externalWorkers, "外部工具（LibreOffice/Chrome）批量转换并发数，默认 2")
	flags.StringVar(&opts.soffice, "soffice", "", "LibreOffice 可执行文件路径；缺省按 OFD_SOFFICE、PATH 和常见安装路径查找")
	flags.IntVar(&opts.officeTimeout, "office-timeout", 0, "Office/HTML 文档转换超时秒数；0 表示默认 120 秒")
	flags.StringVar(&opts.chrome, "chrome", "", "Chrome/Chromium 可执行文件路径；缺省按 OFD_CHROME、PATH 和常见安装路径查找")
	flags.StringVar(&opts.tempDir, "temp-dir", "", "外部工具（LibreOffice/Chrome）临时文件目录；缺省使用系统临时目录")
	flags.StringVar(&opts.paper, "paper", "", "打印纸张尺寸: A4, A3, A5, Letter, Legal, B5, 16开；默认 A4（仅 HTML/MHTML 生效）")
	flags.BoolVar(&opts.landscape, "landscape", false, "横向打印，交换纸张宽高（仅 HTML/MHTML 生效）")
	flags.BoolVar(&opts.noPrintBackground, "no-print-background", false, "不打印背景颜色和图片（HTML/MHTML）")
	flags.BoolVar(&opts.allowRemote, "allow-remote", false, "允许加载外部资源（HTML/MHTML，默认禁止）")
	flags.BoolVar(&opts.markdownTables, "md-tables", false, "OFD 转 Markdown 时按位置识别并输出表格（默认关闭，双栏正文可能误判）")
	flags.BoolVar(&opts.chromeNoSandbox, "chrome-no-sandbox", false, "禁用 Chrome 沙箱（容器或 root 环境可能需要）")
	flags.BoolVar(&opts.recursive, "recursive", opts.recursive, "批量转换时递归扫描输入目录")
	flags.BoolVar(&opts.overwrite, "overwrite", opts.overwrite, "批量转换时覆盖已有输出文件，默认开启")
	flags.BoolVar(&opts.skipExisting, "skip-existing", opts.skipExisting, "批量转换时跳过已有输出文件")
	if err := root.Execute(); err != nil {
		if strings.Contains(err.Error(), "unknown flag") || strings.Contains(err.Error(), "unknown command") {
			return nil, err
		}
		return nil, err
	}
	return opts, nil
}

func normalizeConverterArgs(args []string) []string {
	longFlags := map[string]bool{
		"format": true, "from": true, "html-format": true, "input-dir": true, "output-dir": true,
		"output": true,
		"dpi":    true, "page": true, "bg": true, "dir": true, "workers": true,
		"external-workers": true, "soffice": true, "office-timeout": true,
		"chrome": true, "paper": true, "landscape": true, "no-print-background": true, "temp-dir": true,
		"allow-remote": true, "chrome-no-sandbox": true, "md-tables": true,
		"recursive": true, "overwrite": true, "skip-existing": true,
	}
	result := make([]string, len(args))
	for index, arg := range args {
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			name := strings.TrimPrefix(arg, "-")
			if name != "o" {
				if base, _, hasValue := strings.Cut(name, "="); longFlags[base] && hasValue {
					result[index] = "--" + name
					continue
				}
				if longFlags[name] {
					result[index] = "--" + name
					continue
				}
			}
		}
		result[index] = arg
	}
	return result
}

func run(opts *options) error {
	if opts.inputDir != "" || opts.outputDir != "" {
		return runBatch(opts)
	}
	return runSingle(opts)
}

func runSingle(opts *options) error {
	if _, err := os.Stat(opts.input); err != nil {
		return fmt.Errorf("输入文件: %w", err)
	}
	from := resolveInputFormat(opts)
	format, err := resolveOutputFormat(opts)
	if err != nil {
		return err
	}
	// 非 OFD 输入（如 PDF）先导入再导出，统一走 converter.Convert。
	if from != "ofd" {
		return convertImported(opts, from, format)
	}
	if _, ok := converter.FormatByName(registryFormatName(format)); !ok {
		return fmt.Errorf("%w: %s", ErrInvalidFormat, format)
	}
	if opts.dpi < 1 || opts.dpi > 1200 {
		return errors.New("dpi 必须在 1-1200 之间")
	}
	if opts.page < 0 {
		return errors.New("page 不能小于 0")
	}
	if format == "html" {
		htmlFormat := strings.ToLower(strings.TrimSpace(opts.htmlFormat))
		if htmlFormat != "png" && htmlFormat != "jpg" && htmlFormat != "svg" {
			return errors.New("html-format 必须是 png、jpg 或 svg")
		}
		opts.htmlFormat = htmlFormat
	}
	if err := validateOutputPath(opts, format); err != nil {
		return err
	}
	if format == "pdf" {
		return convertToPDF(opts, format)
	}
	if format == "txt" || format == "md" {
		return convertToText(opts, format)
	}
	if format == "html" {
		return convertToHTML(opts)
	}
	return convertToImage(opts, format)
}

type batchJob struct {
	input  string
	output string
}

type batchResult struct {
	job batchJob
	err error
}

func runBatch(opts *options) error {
	if opts.workers < 1 {
		return errors.New("workers 必须大于 0")
	}
	// 未显式设置（或传入非正值）时使用默认并发，兼容直接构造 options 的调用方。
	if opts.externalWorkers < 1 {
		opts.externalWorkers = defaultOfficeWorkers
	}
	if opts.page < 0 {
		return errors.New("page 不能小于 0")
	}
	if opts.skipExisting && !opts.overwrite {
		return errors.New("--skip-existing 不能与 --overwrite=false 同时使用")
	}
	if opts.format == "" {
		return errors.New("批量转换必须通过 --format 指定输出格式")
	}
	format := normalizeFormat(opts.format)
	if format != "ofd" {
		if _, ok := converter.FormatByName(registryFormatName(format)); !ok {
			return fmt.Errorf("%w: %s", ErrInvalidFormat, format)
		}
	}
	if format == "html" {
		htmlFormat := strings.ToLower(strings.TrimSpace(opts.htmlFormat))
		if htmlFormat != "png" && htmlFormat != "jpg" && htmlFormat != "svg" {
			return errors.New("html-format 必须是 png、jpg 或 svg")
		}
	}
	inputRoot, err := filepath.Abs(opts.inputDir)
	if err != nil {
		return fmt.Errorf("输入目录路径无效: %w", err)
	}
	outputRoot, err := filepath.Abs(opts.outputDir)
	if err != nil {
		return fmt.Errorf("输出目录路径无效: %w", err)
	}
	inputInfo, err := os.Stat(inputRoot)
	if err != nil {
		return fmt.Errorf("输入目录: %w", err)
	}
	if !inputInfo.IsDir() {
		return errors.New("批量输入路径不是目录")
	}
	if outputInfo, statErr := os.Stat(outputRoot); statErr == nil && !outputInfo.IsDir() {
		return errors.New("批量输出路径不是目录")
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return fmt.Errorf("输出目录: %w", statErr)
	}
	if samePath(inputRoot, outputRoot) {
		return errors.New("批量输入目录和输出目录不能相同")
	}

	inputs, err := collectBatchInputs(inputRoot, outputRoot, opts.recursive)
	if err != nil {
		return err
	}
	if len(inputs) == 0 {
		return errors.New("输入目录中没有可转换的文件")
	}

	jobs := make([]batchJob, 0, len(inputs))
	preflightFailed := make([]batchResult, 0)
	skipped := 0
	seenOutputs := make(map[string]string, len(inputs))
	for _, input := range inputs {
		output := batchOutputPath(inputRoot, outputRoot, input, format, opts.page == 0 && isImageFormat(format))
		key := filepath.Clean(output)
		if previous, exists := seenOutputs[key]; exists {
			return fmt.Errorf("多个输入文件映射到同一输出路径: %s 和 %s", previous, input)
		}
		seenOutputs[key] = input
		if batchOutputExists(output) {
			switch {
			case opts.skipExisting:
				skipped++
				continue
			case !opts.overwrite:
				preflightFailed = append(preflightFailed, batchResult{
					job: batchJob{input: input, output: output},
					err: errors.New("输出已存在，且未启用覆盖"),
				})
				continue
			}
		}
		jobs = append(jobs, batchJob{input: input, output: output})
	}
	if len(jobs) == 0 {
		return batchError(len(inputs), preflightFailed, skipped)
	}

	workerCount := minInt(opts.workers, len(jobs))
	// LibreOffice 每次启动都会拉起完整进程，代价高且不宜高并发。用独立信号量
	// 限制同时进行的 Office 转换数量，普通输入不受影响。
	externalSemaphore := make(chan struct{}, minInt(opts.externalWorkers, len(jobs)))
	jobCh := make(chan batchJob)
	resultCh := make(chan batchResult, len(jobs))
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for job := range jobCh {
				if err := os.MkdirAll(filepath.Dir(job.output), 0755); err != nil {
					resultCh <- batchResult{job: job, err: err}
					continue
				}
				jobOptions := *opts
				jobOptions.input = job.input
				jobOptions.output = job.output
				jobOptions.format = format
				release := acquireExternalSlot(&jobOptions, externalSemaphore)
				err := runSingle(&jobOptions)
				release()
				resultCh <- batchResult{job: job, err: err}
			}
		}()
	}
	for _, job := range jobs {
		jobCh <- job
	}
	close(jobCh)
	workers.Wait()
	close(resultCh)

	failed := preflightFailed
	for result := range resultCh {
		if result.err != nil {
			failed = append(failed, result)
		}
	}
	sort.Slice(failed, func(i, j int) bool { return failed[i].job.input < failed[j].job.input })
	return batchError(len(inputs), failed, skipped)
}

func batchError(total int, failed []batchResult, skipped int) error {
	if len(failed) == 0 {
		return nil
	}
	var summary strings.Builder
	fmt.Fprintf(&summary, "批量转换完成：成功 %d，失败 %d", total-len(failed)-skipped, len(failed))
	if skipped > 0 {
		fmt.Fprintf(&summary, "，跳过 %d", skipped)
	}
	for _, result := range failed {
		fmt.Fprintf(&summary, "\n- %s: %v", result.job.input, result.err)
	}
	return errors.New(summary.String())
}

// acquireExternalSlot 对需要外部工具的输入获取并发槽位，返回释放函数；其他
// 输入返回空操作。这样可以在不阻塞普通输入的前提下限制 LibreOffice/Chrome 并发数。
func acquireExternalSlot(opts *options, semaphore chan struct{}) func() {
	if !isExternalInput(opts) {
		return func() {}
	}
	semaphore <- struct{}{}
	return func() { <-semaphore }
}

// isExternalInput 判断输入是否需要外部工具（Office 经 LibreOffice、HTML 经
// Chrome）。这些格式都注册了到 PDF 的直接转换器。
func isExternalInput(opts *options) bool {
	from := resolveInputFormat(opts)
	imp, ok := converter.ImporterByName(from)
	if !ok {
		return false
	}
	_, ok = converter.TransformerFor(imp.Name(), "pdf")
	return ok
}

func batchOutputExists(output string) bool {
	_, err := os.Stat(output)
	return err == nil
}

func collectBatchInputs(inputRoot, outputRoot string, recursive bool) ([]string, error) {
	inputs := make([]string, 0)
	err := filepath.WalkDir(inputRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path != inputRoot && isPathWithin(outputRoot, path) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if path != inputRoot && !recursive {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() && batchInputExtension(entry.Name()) {
			absolute, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			inputs = append(inputs, absolute)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("扫描输入目录失败: %w", err)
	}
	sort.Strings(inputs)
	return inputs, nil
}

// batchInputExtension 判断文件名是否为批量转换支持的输入类型：OFD，或已注册
// 导入器（PDF、Markdown、Office 等）的扩展名。
func batchInputExtension(name string) bool {
	ext := filepath.Ext(name)
	if strings.EqualFold(ext, ".ofd") {
		return true
	}
	_, ok := converter.ImporterByExtension(ext)
	return ok
}

func batchOutputPath(inputRoot, outputRoot, input, format string, imageDirectory bool) string {
	relative, err := filepath.Rel(inputRoot, input)
	if err != nil {
		relative = filepath.Base(input)
	}
	relative = strings.TrimSuffix(relative, filepath.Ext(relative))
	if imageDirectory {
		return filepath.Join(outputRoot, relative)
	}
	return filepath.Join(outputRoot, relative+"."+format)
}

func normalizeFormat(format string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	switch format {
	case "jpeg":
		return "jpg"
	case "markdown":
		return "md"
	default:
		return format
	}
}

func isImageFormat(format string) bool {
	return converter.IsImageFormat(registryFormatName(format))
}

// registryFormatName 把 CLI 的格式名转换为注册表名称。
func registryFormatName(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "txt":
		return "text"
	case "md":
		return "markdown"
	case "jpg":
		return "jpeg"
	default:
		return strings.ToLower(strings.TrimSpace(format))
	}
}

// cliFormatName 把注册表名称转换为 CLI 的格式名。
func cliFormatName(name string) string {
	switch name {
	case "text":
		return "txt"
	case "markdown":
		return "md"
	case "jpeg":
		return "jpg"
	default:
		return name
	}
}

func isPathWithin(parent, path string) bool {
	relative, err := filepath.Rel(parent, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func formatFromExtension(output string) string {
	if strings.EqualFold(filepath.Ext(output), ".ofd") {
		return "ofd"
	}
	if f, ok := converter.FormatByExtension(filepath.Ext(output)); ok {
		return cliFormatName(f.Name)
	}
	return "pdf"
}

// resolveInputFormat 解析输入格式：优先 --from，其次按输入文件扩展名匹配
// 已注册的导入器；都没有时按 OFD 处理。
func resolveInputFormat(opts *options) string {
	if from := strings.ToLower(strings.TrimSpace(opts.from)); from != "" {
		if imp, ok := converter.ImporterByName(from); ok {
			return imp.Name()
		}
		return from
	}
	if imp, ok := converter.ImporterByExtension(filepath.Ext(opts.input)); ok {
		return imp.Name()
	}
	return "ofd"
}

// resolveOutputFormat 解析输出格式：优先 --format，其次按输出扩展名推断。
func resolveOutputFormat(opts *options) (string, error) {
	format := strings.ToLower(strings.TrimSpace(opts.format))
	switch format {
	case "":
		if strings.EqualFold(filepath.Ext(opts.output), ".zip") {
			return "", errors.New("输出为 .zip 时需要通过 -format 指定图片格式")
		}
		return formatFromExtension(opts.output), nil
	case "jpeg":
		return "jpg", nil
	case "markdown":
		return "md", nil
	}
	return format, nil
}

// convertImported 处理非 OFD 输入：通过 converter.Convert 导入并按需导出。
func convertImported(opts *options, from, to string) error {
	if _, ok := converter.ImporterByName(from); !ok {
		return fmt.Errorf("%w: 不支持的输入格式: %s", ErrInvalidFormat, from)
	}
	if to == "" {
		return fmt.Errorf("%w: 未指定输出格式", ErrInvalidFormat)
	}
	if to != "ofd" {
		if isImageFormat(to) {
			return fmt.Errorf("%w: 暂不支持从 %s 直接输出图像，请先转换为 OFD", ErrInvalidFormat, from)
		}
		if _, ok := converter.FormatByName(registryFormatName(to)); !ok {
			return fmt.Errorf("%w: %s", ErrInvalidFormat, to)
		}
	}
	if err := validateOutputPath(opts, to); err != nil {
		return err
	}
	var output io.Writer = os.Stdout
	var fileOutput *lazyFileWriter
	if opts.output != "" && opts.output != "-" {
		fileOutput = &lazyFileWriter{path: ensureExtension(opts.output, to)}
		output = fileOutput
	}
	var option []converter.Option
	if opts.page > 0 {
		option = append(option, converter.Page(opts.page))
	}
	option = append(option, officeOptions(opts)...)
	option = append(option, markdownTablesOption(opts, to)...)
	err := converter.Convert(from, to, opts.input, output, option...)
	if fileOutput != nil {
		if closeErr := fileOutput.Finish(err == nil); err == nil {
			err = closeErr
		}
	}
	return err
}

// markdownTablesOption 在输出 Markdown 且启用 -md-tables 时返回表格识别选项。
func markdownTablesOption(opts *options, format string) []converter.Option {
	if !opts.markdownTables {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "md", "markdown":
		return []converter.Option{converter.WithMarkdownTables(true)}
	}
	return nil
}

// officeOptions 生成外部工具（LibreOffice/Chrome）与纸张相关选项；未设置时
// 返回空切片。
func officeOptions(opts *options) []converter.Option {
	var option []converter.Option
	if strings.TrimSpace(opts.soffice) != "" {
		option = append(option, converter.WithSoffice(opts.soffice))
	}
	if opts.officeTimeout > 0 {
		option = append(option, converter.WithOfficeTimeout(time.Duration(opts.officeTimeout)*time.Second))
	}
	if strings.TrimSpace(opts.chrome) != "" {
		option = append(option, converter.WithChrome(opts.chrome))
	}
	if strings.TrimSpace(opts.tempDir) != "" {
		option = append(option, converter.WithTempDir(opts.tempDir))
	}
	if strings.TrimSpace(opts.paper) != "" {
		option = append(option, converter.WithPaperSize(opts.paper))
	}
	if opts.landscape {
		option = append(option, converter.WithLandscape(true))
	}
	if opts.noPrintBackground {
		option = append(option, converter.WithPrintBackground(false))
	}
	if opts.allowRemote {
		option = append(option, converter.WithAllowRemoteResources(true))
	}
	if opts.chromeNoSandbox {
		option = append(option, converter.WithChromeNoSandbox(true))
	}
	return option
}

func validateOutputPath(opts *options, format string) error {
	if opts.output == "" || opts.output == "-" {
		return nil
	}
	output := opts.output
	if format == "txt" || format == "md" {
		output = ensureExtension(output, format)
	} else if format == "html" {
		output = ensureExtension(output, "html")
	} else if format != "pdf" && opts.page > 0 {
		output = ensureExtension(output, format)
	}
	if sameFilePath(opts.input, output) {
		return ErrInputOutputSame
	}
	return nil
}

func convertToHTML(opts *options) error {
	var output io.Writer = os.Stdout
	var fileOutput *lazyFileWriter
	if opts.output != "" && opts.output != "-" {
		fileOutput = &lazyFileWriter{path: ensureExtension(opts.output, "html")}
		output = fileOutput
	}
	option := []converter.Option{
		converter.DPI(float64(opts.dpi)),
		converter.BgColor(parseBgColor(opts.bg)),
	}
	if opts.htmlFormat == "svg" {
		option = append(option, converter.HTMLSVG())
	} else if opts.htmlFormat == "jpg" {
		option = append(option, converter.HTMLJPG())
	} else {
		option = append(option, converter.HTMLPNG())
	}
	if opts.page > 0 {
		option = append(option, converter.Page(opts.page))
	}
	err := converter.Encode("html", opts.input, output, option...)
	if fileOutput != nil {
		if closeErr := fileOutput.Finish(err == nil); err == nil {
			err = closeErr
		}
	}
	return err
}

func sameFilePath(left, right string) bool {
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	if leftErr == nil && rightErr == nil {
		return os.SameFile(leftInfo, rightInfo)
	}
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func convertToText(opts *options, format string) error {
	var output io.Writer = os.Stdout
	var fileOutput *lazyFileWriter
	if opts.output != "" && opts.output != "-" {
		fileOutput = &lazyFileWriter{path: ensureExtension(opts.output, format)}
		output = fileOutput
	}
	var option []converter.Option
	if opts.page > 0 {
		option = append(option, converter.Page(opts.page))
	}
	option = append(option, markdownTablesOption(opts, registryFormatName(format))...)
	err := converter.Encode(registryFormatName(format), opts.input, output, option...)
	if fileOutput != nil {
		if closeErr := fileOutput.Finish(err == nil); err == nil {
			err = closeErr
		}
	}
	return err
}

func convertToPDF(opts *options, _ string) error {
	var output io.Writer = os.Stdout
	var fileOutput *lazyFileWriter
	if opts.output != "" && opts.output != "-" {
		fileOutput = &lazyFileWriter{path: opts.output}
		output = fileOutput
	}
	var option []converter.Option
	if opts.page > 0 {
		option = append(option, converter.Page(opts.page))
	}
	err := converter.Encode("pdf", opts.input, output, option...)
	if fileOutput != nil {
		if closeErr := fileOutput.Finish(err == nil); err == nil {
			err = closeErr
		}
	}
	return err
}

func convertToImage(opts *options, format string) error {
	option := []converter.Option{
		converter.DPI(float64(opts.dpi)),
		converter.BgColor(parseBgColor(opts.bg)),
		imageFormatOption(format),
	}
	if opts.page > 0 {
		option = append(option, converter.Page(opts.page))
	}

	if opts.page > 0 {
		return convertSinglePage(opts, format, option)
	}
	return convertAllPages(opts, format, option)
}

func convertSinglePage(opts *options, format string, option []converter.Option) error {
	output := opts.output
	if output == "" || output == "-" {
		return converter.Image(opts.input, append(option, converter.Writer(func(int) (io.WriteCloser, error) {
			return nopWriteCloser{Writer: os.Stdout}, nil
		}))...)
	}
	output = ensureExtension(output, format)
	return converter.Image(opts.input, append(option, converter.Writer(func(int) (io.WriteCloser, error) {
		return os.OpenFile(output, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	}))...)
}

func convertAllPages(opts *options, format string, option []converter.Option) error {
	if opts.output == "" || opts.output == "-" {
		return errors.New("多页输出需要指定输出目录或 .zip 文件 (-o)")
	}
	if opts.dir {
		return convertToDirectory(opts, format, option)
	}
	if strings.EqualFold(filepath.Ext(opts.output), ".zip") {
		return convertToZip(opts, format, option)
	}
	return convertToDirectory(opts, format, option)
}

func convertToZip(opts *options, format string, option []converter.Option) error {
	var file *os.File
	var archive *zip.Writer
	err := converter.Image(opts.input, append(option, converter.Writer(func(page int) (io.WriteCloser, error) {
		if archive == nil {
			var err error
			file, err = os.Create(opts.output)
			if err != nil {
				return nil, err
			}
			archive = zip.NewWriter(file)
		}
		entry, err := archive.Create(fmt.Sprintf("page-%04d.%s", page, format))
		if err != nil {
			return nil, err
		}
		return nopWriteCloser{Writer: entry}, nil
	}))...)
	if archive != nil {
		closeErr := archive.Close()
		fileErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = fileErr
		}
	}
	return err
}

func convertToDirectory(opts *options, format string, option []converter.Option) error {
	created := false
	return converter.Image(opts.input, append(option, converter.Writer(func(page int) (io.WriteCloser, error) {
		if !created {
			if err := os.MkdirAll(opts.output, 0755); err != nil {
				return nil, err
			}
			created = true
		}
		path := filepath.Join(opts.output, fmt.Sprintf("page-%04d.%s", page, format))
		return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	}))...)
}

func imageFormatOption(format string) converter.Option {
	return converter.WithFormat(registryFormatName(format))
}

func parseBgColor(s string) color.Color {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "transparent", "none":
		return color.Transparent
	case "black":
		return color.Black
	default:
		return color.White
	}
}

func ensureExtension(path, format string) string {
	if filepath.Ext(path) != "" {
		return path
	}
	return path + "." + format
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error {
	return nil
}

type lazyFileWriter struct {
	path string
	file *os.File
}

func (w *lazyFileWriter) Write(data []byte) (int, error) {
	if w.file == nil {
		file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return 0, err
		}
		w.file = file
	}
	return w.file.Write(data)
}

func (w *lazyFileWriter) Finish(createEmpty bool) error {
	if createEmpty && w.file == nil {
		file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return err
		}
		w.file = file
	}
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}
