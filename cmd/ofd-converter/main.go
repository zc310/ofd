// Command ofd-converter 将 OFD 文档转换为 PDF、文本、Markdown 或图像等格式。
package main

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/klauspost/compress/zip"

	"github.com/spf13/cobra"
	"github.com/zc310/ofd/pkg/converter"
	// 注册 CSV/TSV→OFD 表格导入器。
	_ "github.com/zc310/ofd/pkg/converter/import/csv"
	// 注册 "ofd" 输出格式（PDF→OFD）；按需引入，避免默认链接 pdfcpu。
	_ "github.com/zc310/ofd/pkg/converter/import/pdf"
	// 注册 Markdown→OFD 导入器（输入格式 "md"）。
	_ "github.com/zc310/ofd/pkg/converter/import/markdown"
	// 注册 Office（doc/docx/odt/rtf/wps/pptx/xlsx 等）导入器与→PDF 转换器。
	_ "github.com/zc310/ofd/pkg/converter/import/office"
	// 注册 HTML/MHTML 导入器与→PDF 转换器（chromedp + Chrome/Chromium）。
	"github.com/zc310/ofd/internal/fileutil"
	"github.com/zc310/ofd/internal/version"
	_ "github.com/zc310/ofd/pkg/converter/import/html"
	// 注册 PNG/JPEG/TIFF→OFD 图片导入器。
	_ "github.com/zc310/ofd/pkg/converter/import/image"
	// 注册 XPS→PDF/OFD 导入器（依赖外部 xpstopdf）。
	_ "github.com/zc310/ofd/pkg/converter/import/xps"

	// 注册可选栅格后端，供 --raster-backend 选择（PNG/JPG）。
	_ "github.com/zc310/ofd/internal/render/backends/canvas"
	_ "github.com/zc310/ofd/internal/render/backends/ftgg"
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
	// ErrUsage 标记参数与配置错误，用来和转换执行失败区分：前者是命令没写对，
	// 退出码 2；后者是文档没转出来，退出码 1。批量模式里逐个文件的转换失败
	// 仍按 1 处理，不受这个哨兵影响。
	ErrUsage = errors.New("参数错误")
)

// usageError 把参数校验错误标记为 ErrUsage，保留原始错误信息。
func usageError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w：%w", ErrUsage, err)
}

// usageErrorf 是 usageError 的格式化版本。
func usageErrorf(format string, args ...any) error {
	return usageError(fmt.Errorf(format, args...))
}

type options struct {
	// ctx 是本次运行的取消信号。
	//
	// 放在 options 里而不是给下面十几个函数逐个加参数：这个结构体已经承载了
	// format、dpi、timeout 等全部运行参数，转换调用链上的函数一律只收
	// *options，再单开一个 ctx 参数只会让签名重复而信息量不增。
	ctx                     context.Context
	input                   string
	output                  string
	inputDir                string
	outputDir               string
	format                  string
	from                    string
	htmlFormat              string
	noHTMLTextLayer         bool
	dpi                     int
	rasterBackend           string
	page                    int
	bg                      string
	dir                     bool
	workers                 int
	externalWorkers         int
	soffice                 string
	xpstopdf                string
	officeTimeout           int
	chrome                  string
	chromeNoSandbox         bool
	tempDir                 string
	paper                   string
	landscape               bool
	noPrintBackground       bool
	allowRemote             bool
	password                string
	pdfStrict               bool
	pdfImageMode            string
	pdfJPEGQuality          int
	pdfJPXBackend           string
	pdfMaxInputBytes        int64
	pdfMaxTotalDecodedBytes int64
	pdfMaxImagePixels       int64
	pdfMaxTotalImagePixels  int64
	pdfMaxOperations        int64
	diagnosticsWriter       io.Writer
	ocr                     bool
	ocrLanguage             string
	csvOptions              converter.CSVImportOptions
	markdownTables          bool
	noDocxTables            bool
	noDocxImages            bool
	docxAnnotations         bool
	recursive               bool
	overwrite               bool
	skipExisting            bool
	help                    bool
	version                 bool
}

func main() {
	// Ctrl-C 立即停止：批量转换时不该跑完当前文件乃至整个目录。转换入口
	// 会在页与页之间检查这个信号。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if opts.help {
		return
	}
	if opts.version {
		fmt.Fprintln(os.Stdout, version.Version)
		return
	}
	opts.ctx = ctx
	if err := run(opts); err != nil {
		// Ctrl-C 是用户主动停止，不是转换失败。报"转换失败"既不准确，
		// 也会让人以为文档有问题而重试——而它根本没有失败。
		// 130 是 SIGINT 的惯例退出码。
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "已取消。")
			os.Exit(130)
		}
		// 参数错误要让脚本能区分「命令写错了」和「文档有问题」，
		// 前者重试没有意义。
		if errors.Is(err, ErrUsage) {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "转换失败:", err)
		os.Exit(1)
	}
}

func parseArgs(args []string) (*options, error) {
	opts := &options{dpi: defaultDPI, bg: defaultBgColor, htmlFormat: "png", ocrLanguage: "chi_sim+eng", workers: defaultWorkers, externalWorkers: defaultOfficeWorkers, recursive: true, overwrite: true, pdfImageMode: string(converter.PDFImageModeCompatible), pdfJPEGQuality: converter.PDFDefaultJPEGQuality, pdfJPXBackend: string(converter.PDFJPXBackendGo)}
	var output, format string
	args = normalizeConverterArgs(args)
	root := &cobra.Command{
		Use:           "ofd-converter [flags] input.ofd [output]",
		Short:         "OFD 文档转换命令行工具",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, positional []string) error {
			if opts.version {
				return nil
			}
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
	flags.StringVarP(&output, "output", "o", "", "输出文件路径或目录，多页 PNG/JPEG 等图像可为 .zip 文件或目录，TIFF 始终为单个文件")
	flags.StringVar(&opts.inputDir, "input-dir", "", "批量转换的输入目录")
	flags.StringVar(&opts.outputDir, "output-dir", "", "批量转换的输出目录")
	flags.StringVar(&format, "format", "", "输出格式: ofd, pdf, docx, txt, md, markdown, html, png, jpg, tiff, svg, eps, tex")
	flags.StringVar(&opts.from, "from", "", "输入格式（可选）: csv, tsv, pdf, xps, md, docx, doc, odt, rtf, wps, pptx, xlsx, mhtml, html 等；缺省按输入文件扩展名推断")
	flags.StringVar(&opts.htmlFormat, "html-format", opts.htmlFormat, "HTML 页面格式: png, jpg, svg")
	flags.BoolVar(&opts.noHTMLTextLayer, "no-html-text-layer", false, "OFD 转 HTML 时不输出透明文字层（默认输出，文字因此可选中、可查找、可朗读）")
	flags.IntVar(&opts.dpi, "dpi", opts.dpi, "输出分辨率 (1-1200)")
	flags.StringVar(&opts.rasterBackend, "raster-backend", "", "PNG/JPG 栅格后端: canvas, ftgg；缺省 canvas(矢量表面光栅化)")
	flags.IntVar(&opts.page, "page", opts.page, "指定全局页码 (从 1 开始)，0 表示全部文档体页面")
	flags.StringVar(&opts.bg, "bg", opts.bg, "背景颜色: transparent, white, black")
	flags.BoolVar(&opts.dir, "dir", opts.dir, "不压缩，将多页图片直接保存到输出目录下的多个文件")
	flags.IntVar(&opts.workers, "workers", opts.workers, "批量转换并发数，默认 4")
	flags.IntVar(&opts.externalWorkers, "external-workers", opts.externalWorkers, "外部工具（LibreOffice/Chrome/xpstopdf）批量转换并发数，默认 2")
	flags.StringVar(&opts.soffice, "soffice", "", "LibreOffice 可执行文件路径；缺省按 OFD_SOFFICE、PATH 和常见安装路径查找")
	flags.StringVar(&opts.xpstopdf, "xpstopdf", "", "xpstopdf 可执行文件路径；缺省按 OFD_XPSTOPDF 和 PATH 查找")
	flags.IntVar(&opts.officeTimeout, "office-timeout", 0, "Office/HTML/XPS 文档转换超时秒数；0 表示默认 120 秒")
	flags.StringVar(&opts.chrome, "chrome", "", "Chrome/Chromium 可执行文件路径；缺省按 OFD_CHROME、PATH 和常见安装路径查找")
	flags.StringVar(&opts.tempDir, "temp-dir", "", "外部工具（LibreOffice/Chrome/xpstopdf）临时文件目录；缺省使用系统临时目录")
	flags.StringVar(&opts.paper, "paper", "", "打印纸张尺寸: A4, A3, A5, Letter, Legal, B5, 16开；默认 A4（HTML/MHTML/CSV/TSV）")
	flags.BoolVar(&opts.landscape, "landscape", false, "横向打印，交换纸张宽高（HTML/MHTML/CSV/TSV）")
	flags.BoolVar(&opts.noPrintBackground, "no-print-background", false, "不打印背景颜色和图片（HTML/MHTML）")
	flags.BoolVar(&opts.allowRemote, "allow-remote", false, "允许加载外部资源（HTML/MHTML，默认禁止）")
	flags.StringVar(&opts.password, "password", "", "加密输入文档的打开口令（目前仅 PDF）")
	flags.BoolVar(&opts.pdfStrict, "pdf-strict", false, "PDF 导入遇到内容缺失或近似转换诊断时失败")
	flags.StringVar(&opts.pdfImageMode, "pdf-image-mode", opts.pdfImageMode, "PDF 图片模式: compatible (默认，无损兼容) 或 compact (不透明 CMYK JPEG 转有损 RGB JPEG)")
	flags.IntVar(&opts.pdfJPEGQuality, "pdf-jpeg-quality", opts.pdfJPEGQuality, "PDF compact 图片模式的 JPEG 质量 (1-100；0 使用默认 90)")
	flags.StringVar(&opts.pdfJPXBackend, "pdf-jpx-backend", opts.pdfJPXBackend, "PDF JPX 解码器: go (默认) 或 openjpeg (需以 -tags openjpeg 构建并安装 libopenjp2)")
	flags.Var(byteSizeValue{value: &opts.pdfMaxInputBytes}, "pdf-max-input-bytes", "PDF 输入字节上限；支持 B、KiB、MiB、GiB、TiB；0 使用默认 512MiB")
	flags.Var(byteSizeValue{value: &opts.pdfMaxTotalDecodedBytes}, "pdf-max-total-decoded-bytes", "PDF 累计解码字节上限；支持 B、KiB、MiB、GiB、TiB；0 使用默认 1GiB")
	flags.Int64Var(&opts.pdfMaxImagePixels, "pdf-max-image-pixels", 0, "PDF 单图片像素上限；0 使用默认 1 亿像素")
	flags.Int64Var(&opts.pdfMaxTotalImagePixels, "pdf-max-total-image-pixels", 0, "PDF 累计图片解码像素上限；0 使用默认 20 亿像素")
	flags.Int64Var(&opts.pdfMaxOperations, "pdf-max-operations", 0, "PDF 转换处理量上限；0 使用默认 1,000,000,000")
	flags.BoolVar(&opts.ocr, "ocr", false, "图片转 OFD 时启用 OCR 文字层，默认关闭")
	flags.StringVar(&opts.ocrLanguage, "ocr-language", opts.ocrLanguage, "图片 OCR 语言，需与本机 Tesseract 语言包一致")
	flags.BoolVar(&opts.csvOptions.NoHeader, "csv-no-header", false, "CSV/TSV 首行也是数据，不作为重复表头")
	flags.StringVar(&opts.csvOptions.Encoding, "csv-encoding", "utf-8", "CSV/TSV 输入编码: utf-8, gb18030, gbk")
	flags.StringVar(&opts.csvOptions.Title, "csv-title", "", "CSV/TSV 文档标题；默认输入文件名（不含扩展名）")
	flags.StringVar(&opts.csvOptions.FontFile, "csv-font", "", "CSV/TSV 嵌入字体文件（TTF/OTF/TTC）；默认按内容使用内置或系统中文字体")
	flags.BoolVar(&opts.markdownTables, "md-tables", false, "OFD 转 Markdown 时按位置识别并输出表格（默认关闭，双栏正文可能误判）")
	flags.BoolVar(&opts.noDocxTables, "no-docx-tables", false, "OFD 转 DOCX 时不按位置识别表格（默认开启，双栏正文可能误判）")
	flags.BoolVar(&opts.noDocxImages, "no-docx-images", false, "OFD 转 DOCX 时不内嵌图片（默认内嵌，每页上限 64 张，SVG 等矢量图始终跳过）")
	flags.BoolVar(&opts.docxAnnotations, "docx-annotations", false, "OFD 转 DOCX 时保留批注层文字（默认剔除水印与印章等叠加标记）")
	flags.BoolVar(&opts.chromeNoSandbox, "chrome-no-sandbox", false, "禁用 Chrome 沙箱（容器或 root 环境可能需要）")
	flags.BoolVar(&opts.recursive, "recursive", opts.recursive, "批量转换时递归扫描输入目录")
	flags.BoolVar(&opts.overwrite, "overwrite", opts.overwrite, "批量转换时覆盖已有输出文件，默认开启")
	flags.BoolVar(&opts.skipExisting, "skip-existing", opts.skipExisting, "批量转换时跳过已有输出文件")
	flags.BoolVar(&opts.version, "version", false, "输出工具版本")
	if err := root.Execute(); err != nil {
		if strings.Contains(err.Error(), "unknown flag") || strings.Contains(err.Error(), "unknown command") {
			return nil, err
		}
		return nil, err
	}
	return opts, nil
}

type byteSizeValue struct {
	value *int64
}

func (v byteSizeValue) Set(input string) error {
	input = strings.TrimSpace(input)
	unitStart := 0
	for unitStart < len(input) && (input[unitStart] == '+' || input[unitStart] == '-' || input[unitStart] >= '0' && input[unitStart] <= '9') {
		unitStart++
	}
	number, err := strconv.ParseInt(input[:unitStart], 10, 64)
	if err != nil || unitStart == 0 {
		return fmt.Errorf("无效的字节数 %q", input)
	}
	unit := strings.ToUpper(strings.TrimSpace(input[unitStart:]))
	multiplier := int64(1)
	switch unit {
	case "", "B":
	case "KIB":
		multiplier = 1 << 10
	case "MIB":
		multiplier = 1 << 20
	case "GIB":
		multiplier = 1 << 30
	case "TIB":
		multiplier = 1 << 40
	case "KB":
		multiplier = 1_000
	case "MB":
		multiplier = 1_000_000
	case "GB":
		multiplier = 1_000_000_000
	case "TB":
		multiplier = 1_000_000_000_000
	default:
		return fmt.Errorf("不支持的字节单位 %q（可用 B、KB、MB、GB、TB、KiB、MiB、GiB、TiB）", unit)
	}
	if number > 0 && number > math.MaxInt64/multiplier || number < 0 && number < math.MinInt64/multiplier {
		return fmt.Errorf("字节数 %q 超出 int64 范围", input)
	}
	*v.value = number * multiplier
	return nil
}

func (v byteSizeValue) String() string {
	if v.value == nil {
		return "0"
	}
	return strconv.FormatInt(*v.value, 10)
}

func (byteSizeValue) Type() string { return "bytes" }

func normalizeConverterArgs(args []string) []string {
	longFlags := map[string]bool{
		"format": true, "from": true, "html-format": true, "no-html-text-layer": true, "input-dir": true, "output-dir": true,
		"output": true,
		"dpi":    true, "page": true, "bg": true, "dir": true, "workers": true,
		"raster-backend":   true,
		"external-workers": true, "soffice": true, "xpstopdf": true, "office-timeout": true,
		"chrome": true, "paper": true, "landscape": true, "no-print-background": true, "temp-dir": true,
		"allow-remote": true, "chrome-no-sandbox": true, "md-tables": true,
		"no-docx-tables": true, "no-docx-images": true, "docx-annotations": true,
		"password":   true,
		"pdf-strict": true, "pdf-image-mode": true, "pdf-jpeg-quality": true, "pdf-jpx-backend": true, "pdf-max-input-bytes": true, "pdf-max-total-decoded-bytes": true, "pdf-max-image-pixels": true, "pdf-max-total-image-pixels": true, "pdf-max-operations": true,
		"ocr": true, "ocr-language": true,
		"csv-no-header": true, "csv-encoding": true, "csv-title": true, "csv-font": true,
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
	if _, err := opts.csvOptions.Normalize(); err != nil {
		return usageError(err)
	}
	if opts.paper != "" && (opts.inputDir != "" || resolveInputFormat(opts) == "csv" || resolveInputFormat(opts) == "tsv") {
		if _, err := converter.PaperByName(opts.paper); err != nil {
			return usageError(err)
		}
	}
	if _, err := opts.pdfImportOptions().Normalize(); err != nil {
		return usageError(err)
	}
	if opts.inputDir != "" || opts.outputDir != "" {
		return runBatch(opts)
	}
	return runSingle(opts)
}

func (opts *options) pdfLimits() converter.PDFImportLimits {
	return converter.PDFImportLimits{MaxInputBytes: opts.pdfMaxInputBytes, MaxTotalDecodedBytes: opts.pdfMaxTotalDecodedBytes, MaxImagePixels: opts.pdfMaxImagePixels, MaxTotalImagePixels: opts.pdfMaxTotalImagePixels, MaxOperations: opts.pdfMaxOperations}
}

func (opts *options) pdfImportOptions() converter.PDFImportOptions {
	return converter.PDFImportOptions{
		Limits:      opts.pdfLimits(),
		ImageMode:   converter.PDFImageMode(opts.pdfImageMode),
		JPEGQuality: opts.pdfJPEGQuality,
		JPXBackend:  converter.PDFJPXBackend(opts.pdfJPXBackend),
	}
}

func runSingle(opts *options) error {
	// 本函数的参数校验一律包成 ErrUsage：命令写错了退出码是 2，文档本身
	// 转不出来才是 1。混在一起时脚本无法区分该改命令还是该换文件。
	if _, err := os.Stat(opts.input); err != nil {
		return usageErrorf("输入文件: %w", err)
	}
	from := resolveInputFormat(opts)
	format, err := resolveOutputFormat(opts)
	if err != nil {
		return usageError(err)
	}
	// 非 OFD 输入（如 PDF）先导入再导出，统一走 converter.Convert。
	if from != "ofd" {
		return convertImported(opts, from, format)
	}
	if _, ok := converter.FormatByName(registryFormatName(format)); !ok {
		return usageErrorf("%w: %s", ErrInvalidFormat, format)
	}
	if opts.dpi < 1 || opts.dpi > 1200 {
		return usageError(errors.New("dpi 必须在 1-1200 之间"))
	}
	if opts.page < 0 {
		return usageError(errors.New("page 不能小于 0"))
	}
	if format == "html" {
		htmlFormat := strings.ToLower(strings.TrimSpace(opts.htmlFormat))
		if htmlFormat != "png" && htmlFormat != "jpg" && htmlFormat != "svg" {
			return usageError(errors.New("html-format 必须是 png、jpg 或 svg"))
		}
		opts.htmlFormat = htmlFormat
	}
	if err := validateOutputPath(opts, format); err != nil {
		return usageError(err)
	}
	if format == "pdf" {
		return convertToPDF(opts, format)
	}
	if format == "tiff" {
		return convertToTIFF(opts)
	}
	if format == "txt" || format == "md" {
		return convertToText(opts, format)
	}
	if format == "html" {
		return convertToHTML(opts)
	}
	if format == "docx" {
		return convertToDOCX(opts)
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
	// 批量模式的参数校验同样标记为 ErrUsage：目录没给对、格式拼错都是命令
	// 写错了，退出码 2。逐个文件的转换失败由 batchError 汇总，仍是 1。
	if opts.workers < 1 {
		return usageError(errors.New("workers 必须大于 0"))
	}
	// 未显式设置（或传入非正值）时使用默认并发，兼容直接构造 options 的调用方。
	if opts.externalWorkers < 1 {
		opts.externalWorkers = defaultOfficeWorkers
	}
	if opts.page < 0 {
		return usageError(errors.New("page 不能小于 0"))
	}
	if opts.skipExisting && !opts.overwrite {
		return usageError(errors.New("--skip-existing 不能与 --overwrite=false 同时使用"))
	}
	if opts.format == "" {
		return usageError(errors.New("批量转换必须通过 --format 指定输出格式"))
	}
	format := normalizeFormat(opts.format)
	if format != "ofd" {
		if _, ok := converter.FormatByName(registryFormatName(format)); !ok {
			return usageErrorf("%w: %s", ErrInvalidFormat, format)
		}
	}
	if format == "html" {
		htmlFormat := strings.ToLower(strings.TrimSpace(opts.htmlFormat))
		if htmlFormat != "png" && htmlFormat != "jpg" && htmlFormat != "svg" {
			return usageError(errors.New("html-format 必须是 png、jpg 或 svg"))
		}
	}
	// 批量模式的目录级校验同样是命令写错了，退出码 2；单个文件转换失败仍然
	// 汇总成一次批量错误（退出码 1）。
	inputRoot, err := filepath.Abs(opts.inputDir)
	if err != nil {
		return usageErrorf("输入目录路径无效: %w", err)
	}
	outputRoot, err := filepath.Abs(opts.outputDir)
	if err != nil {
		return usageErrorf("输出目录路径无效: %w", err)
	}
	inputInfo, err := os.Stat(inputRoot)
	if err != nil {
		return usageErrorf("输入目录: %w", err)
	}
	if !inputInfo.IsDir() {
		return usageError(errors.New("批量输入路径不是目录"))
	}
	if outputInfo, statErr := os.Stat(outputRoot); statErr == nil && !outputInfo.IsDir() {
		return usageError(errors.New("批量输出路径不是目录"))
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return usageErrorf("输出目录: %w", statErr)
	}
	if fileutil.SamePath(inputRoot, outputRoot) {
		return usageError(errors.New("批量输入目录和输出目录不能相同"))
	}

	inputs, err := collectBatchInputs(inputRoot, outputRoot, opts.recursive)
	if err != nil {
		return usageError(err)
	}
	if len(inputs) == 0 {
		return usageError(errors.New("输入目录中没有可转换的文件"))
	}

	jobs := make([]batchJob, 0, len(inputs))
	preflightFailed := make([]batchResult, 0)
	skipped := 0
	seenOutputs := make(map[string]string, len(inputs))
	for _, input := range inputs {
		output := batchOutputPath(inputRoot, outputRoot, input, format, opts.page == 0 && isImageFormat(format) && format != "tiff")
		key := filepath.Clean(output)
		if previous, exists := seenOutputs[key]; exists {
			return usageErrorf("多个输入文件映射到同一输出路径: %s 和 %s", previous, input)
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
// Chrome、XPS 经 xpstopdf）。这些格式都注册了到 PDF 的直接转换器。
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
	if from == "csv" || from == "tsv" {
		if _, err := opts.csvOptions.Normalize(); err != nil {
			return usageError(err)
		}
		if opts.paper != "" {
			if _, err := converter.PaperByName(opts.paper); err != nil {
				return usageError(err)
			}
		}
	}
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
	diagnostics := opts.diagnosticsWriter
	if diagnostics == nil {
		diagnostics = os.Stderr
	}
	option = append(option, converter.WithPDFImportOptions(converter.PDFImportOptions{
		Limits:      opts.pdfLimits(),
		Strict:      opts.pdfStrict,
		ImageMode:   converter.PDFImageMode(opts.pdfImageMode),
		JPEGQuality: opts.pdfJPEGQuality,
		JPXBackend:  converter.PDFJPXBackend(opts.pdfJPXBackend),
		OnDiagnostic: func(d converter.PDFImportDiagnostic) {
			_, _ = fmt.Fprintf(diagnostics, "PDF 导入警告 [%s] %s 第 %d 页，对象 %d/%d，资源 %q：%s\n", d.Code, opts.input, d.Page, d.Object, d.Generation, d.Resource, d.Message)
		},
	}))
	if opts.page > 0 {
		option = append(option, converter.Page(opts.page))
	}
	if opts.password != "" {
		option = append(option, converter.WithPassword(opts.password))
	}
	option = append(option, officeOptions(opts)...)
	option = append(option, imageOCROptions(opts)...)
	option = append(option, converter.WithCSVImportOptions(opts.csvOptions))
	option = append(option, markdownTablesOption(opts, to)...)
	err := converter.Convert(opts.ctx, from, to, opts.input, output, option...)
	if fileOutput != nil {
		if closeErr := fileOutput.Finish(err == nil); err == nil {
			err = closeErr
		}
	}
	return err
}

func imageOCROptions(opts *options) []converter.Option {
	return []converter.Option{
		converter.ImageOCR(opts.ocr),
		converter.ImageOCRLanguage(opts.ocrLanguage),
	}
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
	if strings.TrimSpace(opts.xpstopdf) != "" {
		option = append(option, converter.WithXpstopdf(opts.xpstopdf))
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
	} else if format == "tiff" || (format != "pdf" && opts.page > 0) {
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
	option = append(option, converter.WithHTMLTextLayer(!opts.noHTMLTextLayer))
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
	err := converter.Encode(opts.ctx, "html", opts.input, output, option...)
	if fileOutput != nil {
		if closeErr := fileOutput.Finish(err == nil); err == nil {
			err = closeErr
		}
	}
	return err
}

func convertToDOCX(opts *options) error {
	var output io.Writer = os.Stdout
	var fileOutput *lazyFileWriter
	if opts.output != "" && opts.output != "-" {
		fileOutput = &lazyFileWriter{path: ensureExtension(opts.output, "docx")}
		output = fileOutput
	}
	option := []converter.Option{
		converter.WithDOCXImages(!opts.noDocxImages),
		converter.WithDOCXAnnotations(opts.docxAnnotations),
	}
	if opts.page > 0 {
		option = append(option, converter.Page(opts.page))
	}
	err := converter.Encode(opts.ctx, "docx", opts.input, output, option...)
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
	err := converter.Encode(opts.ctx, registryFormatName(format), opts.input, output, option...)
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
	err := converter.Encode(opts.ctx, "pdf", opts.input, output, option...)
	if fileOutput != nil {
		if closeErr := fileOutput.Finish(err == nil); err == nil {
			err = closeErr
		}
	}
	return err
}

func convertToTIFF(opts *options) error {
	var output io.Writer = os.Stdout
	var fileOutput *lazyFileWriter
	if opts.output != "" && opts.output != "-" {
		fileOutput = &lazyFileWriter{path: ensureExtension(opts.output, "tiff")}
		output = fileOutput
	}
	option := []converter.Option{
		converter.DPI(float64(opts.dpi)),
		converter.BgColor(parseBgColor(opts.bg)),
	}
	if opts.rasterBackend != "" {
		option = append(option, converter.RasterBackend(opts.rasterBackend))
	}
	if opts.page > 0 {
		option = append(option, converter.Page(opts.page))
	}
	err := converter.Encode(opts.ctx, "tiff", opts.input, output, option...)
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
	if opts.rasterBackend != "" {
		option = append(option, converter.RasterBackend(opts.rasterBackend))
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
		return converter.Image(opts.ctx, opts.input, append(option, converter.Writer(func(int) (io.WriteCloser, error) {
			return nopWriteCloser{Writer: os.Stdout}, nil
		}))...)
	}
	output = ensureExtension(output, format)
	return converter.Image(opts.ctx, opts.input, append(option, converter.Writer(func(int) (io.WriteCloser, error) {
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
	err := converter.Image(opts.ctx, opts.input, append(option, converter.Writer(func(page int) (io.WriteCloser, error) {
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
	return converter.Image(opts.ctx, opts.input, append(option, converter.Writer(func(page int) (io.WriteCloser, error) {
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
