package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zc310/ofd/pkg/validator"
)

const (
	exitOK    = 0
	exitUsage = 2
)

type options struct {
	input         string
	output        string
	format        string
	mode          string
	font          string
	maxErrors     int
	maxFileSize   int64
	maxTotalSize  int64
	maxEntries    int
	maxXMLBytes   int64
	maxXMLNodes   int
	maxXMLDepth   int
	pretty        bool
	skipXSD       bool
	noDigest      bool
	noScanXML     bool
	failOnWarning bool
	version       bool
	help          bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseArgs(args, stderr)
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stderr, "ofd-validator:", err)
		}
		return exitUsage
	}
	if opts.help {
		return exitOK
	}
	if opts.version {
		_, _ = fmt.Fprintln(stdout, validator.ToolVersion)
		return exitOK
	}
	if err := validateOptions(opts); err != nil {
		fmt.Fprintln(stderr, "ofd-validator:", err)
		return exitUsage
	}

	validatorOptions := []validator.Option{
		validator.WithMode(validator.Mode(opts.mode)),
		validator.WithMaxErrors(opts.maxErrors),
		validator.WithMaxFileSize(opts.maxFileSize),
		validator.WithMaxTotalSize(opts.maxTotalSize),
		validator.WithMaxEntries(opts.maxEntries),
		validator.WithMaxXMLBytes(opts.maxXMLBytes),
		validator.WithMaxXMLNodes(opts.maxXMLNodes),
		validator.WithMaxXMLDepth(opts.maxXMLDepth),
		validator.WithSkipXSD(opts.skipXSD),
		validator.WithCheckDigest(!opts.noDigest),
		validator.WithScanXML(!opts.noScanXML),
		validator.WithFailOnWarning(opts.failOnWarning),
	}
	instance, err := validator.New(validatorOptions...)
	if err != nil {
		fmt.Fprintln(stderr, "ofd-validator:", err)
		return exitUsage
	}
	report := instance.ValidatePath(context.Background(), opts.input)
	if err := writeReport(opts, report, stdout); err != nil {
		fmt.Fprintln(stderr, "ofd-validator:", err)
		return exitUsage
	}
	return report.ExitCode(opts.failOnWarning)
}

func parseArgs(args []string, output io.Writer) (*options, error) {
	opts := &options{
		format:       "text",
		mode:         string(validator.ModeStrict),
		maxErrors:    100,
		maxFileSize:  64 << 20,
		maxTotalSize: 512 << 20,
		maxEntries:   10000,
		maxXMLBytes:  64 << 20,
		maxXMLNodes:  2_000_000,
		maxXMLDepth:  1000,
	}
	flags := flag.NewFlagSet("ofd-validator", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&opts.output, "o", "", "报告输出路径；使用 - 输出到标准输出")
	flags.StringVar(&opts.output, "output", "", "报告输出路径；使用 - 输出到标准输出")
	flags.StringVar(&opts.format, "format", opts.format, "报告格式：text、markdown、json 或 pdf")
	flags.StringVar(&opts.mode, "mode", opts.mode, "校验模式：strict、compat 或 structural")
	flags.StringVar(&opts.font, "font", "", "PDF 报告使用的中文字体文件")
	flags.IntVar(&opts.maxErrors, "max-errors", opts.maxErrors, "最多记录的校验错误数")
	flags.Int64Var(&opts.maxFileSize, "max-file-size", opts.maxFileSize, "ZIP 条目解压后的最大字节数")
	flags.Int64Var(&opts.maxTotalSize, "max-total-size", opts.maxTotalSize, "OFD 包解压后的最大总字节数")
	flags.IntVar(&opts.maxEntries, "max-entries", opts.maxEntries, "ZIP 条目的最大数量")
	flags.Int64Var(&opts.maxXMLBytes, "max-xml-bytes", opts.maxXMLBytes, "单个 XML 文件的最大字节数")
	flags.IntVar(&opts.maxXMLNodes, "max-xml-nodes", opts.maxXMLNodes, "单个 XML 文件的最大节点数")
	flags.IntVar(&opts.maxXMLDepth, "max-xml-depth", opts.maxXMLDepth, "单个 XML 文件的最大嵌套深度")
	flags.BoolVar(&opts.pretty, "pretty", false, "缩进 JSON 输出")
	flags.BoolVar(&opts.skipXSD, "skip-xsd", false, "跳过 XSD 校验；等价于 structural 模式的 XSD 阶段")
	flags.BoolVar(&opts.noDigest, "no-digest", false, "跳过签名摘要校验")
	flags.BoolVar(&opts.noScanXML, "no-scan-xml", false, "只解析由 OFD 引用到的 XML 文件")
	flags.BoolVar(&opts.failOnWarning, "fail-on-warning", false, "发现警告时返回退出码 1")
	flags.BoolVar(&opts.version, "version", false, "输出校验器版本")
	flags.Usage = func() {
		_, _ = fmt.Fprintln(output, "ofd-validator - OFD 文件校验工具")
		_, _ = fmt.Fprintln(output, "用法：ofd-validator [选项] input.ofd")
		_, _ = fmt.Fprintln(output)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return &options{help: true}, nil
		}
		return nil, err
	}
	if opts.version {
		return opts, nil
	}
	if flags.NArg() != 1 {
		if flags.NArg() == 0 {
			return nil, errors.New("缺少输入 OFD 文件")
		}
		return nil, errors.New("必须且只能指定一个输入 OFD 文件")
	}
	opts.input = flags.Arg(0)
	return opts, nil
}

func validateOptions(opts *options) error {
	switch strings.ToLower(strings.TrimSpace(opts.format)) {
	case "text", "markdown", "json", "pdf":
	default:
		return fmt.Errorf("不支持的报告格式 %q", opts.format)
	}
	opts.format = strings.ToLower(strings.TrimSpace(opts.format))
	opts.mode = strings.ToLower(strings.TrimSpace(opts.mode))
	switch validator.Mode(opts.mode) {
	case validator.ModeStrict, validator.ModeCompat, validator.ModeStructural:
	default:
		return fmt.Errorf("不支持的校验模式 %q", opts.mode)
	}
	if opts.skipXSD && opts.mode == string(validator.ModeStrict) {
		opts.mode = string(validator.ModeStructural)
	}
	if opts.maxErrors < 0 || opts.maxFileSize < 0 || opts.maxTotalSize < 0 || opts.maxEntries < 0 || opts.maxXMLBytes < 0 || opts.maxXMLNodes < 0 || opts.maxXMLDepth < 0 {
		return errors.New("大小和错误数量限制不能为负数")
	}
	if opts.font != "" && opts.format != "pdf" {
		return errors.New("--font 只能与 --format pdf 一起使用")
	}
	if _, err := os.Stat(opts.input); err != nil {
		return fmt.Errorf("输入文件：%w", err)
	}
	if opts.output != "" && opts.output != "-" && samePath(opts.input, opts.output) {
		return errors.New("报告输出不能覆盖输入 OFD 文件")
	}
	return nil
}

func writeReport(opts *options, report validator.Report, stdout io.Writer) error {
	// 先在内存中完成渲染，避免 PDF 字体加载失败时留下空报告文件。
	var buffer bytes.Buffer
	var output io.Writer = &buffer

	var err error
	switch opts.format {
	case "text":
		err = validator.RenderText(output, report)
	case "markdown":
		err = validator.RenderMarkdown(output, report)
	case "json":
		err = validator.RenderJSON(output, report, opts.pretty)
	case "pdf":
		err = validator.RenderPDF(output, report, validator.PDFOptions{Font: opts.font})
	}
	if err != nil {
		return err
	}
	if opts.output == "" || opts.output == "-" {
		_, err = stdout.Write(buffer.Bytes())
		return err
	}
	file, err := os.Create(opts.output)
	if err != nil {
		return fmt.Errorf("创建报告输出文件失败：%w", err)
	}
	if _, err = file.Write(buffer.Bytes()); err != nil {
		_ = file.Close()
		return fmt.Errorf("写入报告输出文件失败：%w", err)
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("关闭报告输出文件失败：%w", err)
	}
	return nil
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	if filepath.Clean(leftAbs) == filepath.Clean(rightAbs) {
		return true
	}
	leftReal, leftErr := filepath.EvalSymlinks(leftAbs)
	rightReal, rightErr := filepath.EvalSymlinks(rightAbs)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftReal) == filepath.Clean(rightReal)
}
