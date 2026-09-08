package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zc310/ofd/pkg/analyzer"
)

const (
	exitOK     = 0
	exitFailed = 1
	exitUsage  = 2
)

type options struct {
	input         string
	output        string
	format        string
	font          string
	pretty        bool
	noTemplates   bool
	noAnnotations bool
	noSignatures  bool
	tree          bool
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
			_, _ = fmt.Fprintln(stderr, "ofd-analyzer:", err)
		}
		return exitUsage
	}
	if opts.help {
		return exitOK
	}
	if opts.version {
		_, _ = fmt.Fprintln(stdout, analyzer.ToolVersion)
		return exitOK
	}
	if err := validateOptions(opts); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-analyzer:", err)
		return exitUsage
	}

	analysisOptions := []analyzer.Option{
		analyzer.WithTemplates(!opts.noTemplates),
		analyzer.WithAnnotations(!opts.noAnnotations),
		analyzer.WithSignatures(!opts.noSignatures),
		analyzer.WithTree(opts.tree),
	}
	report, analyzeErr := analyzer.Analyze(opts.input, analysisOptions...)
	if err := writeReport(opts, report, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-analyzer:", err)
		return exitUsage
	}
	if analyzeErr != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-analyzer: 分析失败:", analyzeErr)
		return exitFailed
	}
	if opts.failOnWarning && len(report.Warnings) > 0 {
		return exitFailed
	}
	return exitOK
}

func parseArgs(args []string, output io.Writer) (*options, error) {
	opts := &options{}
	opts.format = "text"
	flags := flag.NewFlagSet("ofd-analyzer", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&opts.output, "o", "", "报告输出路径；使用 - 输出到标准输出")
	flags.StringVar(&opts.output, "output", "", "报告输出路径；使用 - 输出到标准输出")
	flags.StringVar(&opts.format, "format", opts.format, "报告格式：text、markdown、json 或 pdf；默认为 text")
	flags.StringVar(&opts.font, "font", "", "PDF 报告使用的字体文件")
	flags.BoolVar(&opts.pretty, "pretty", false, "缩进 JSON 输出")
	flags.BoolVar(&opts.noTemplates, "no-templates", false, "跳过模板定义、引用和 PageRes 资源分析，但保留模板元数据")
	flags.BoolVar(&opts.noAnnotations, "no-annotations", false, "跳过注解及 Appearance 分析")
	flags.BoolVar(&opts.noSignatures, "no-signatures", false, "跳过签名清单分析")
	flags.BoolVar(&opts.tree, "tree", false, "输出 OFD ZIP 包目录结构")
	flags.BoolVar(&opts.failOnWarning, "fail-on-warning", false, "发现警告时返回退出码 1")
	flags.BoolVar(&opts.version, "version", false, "输出 analyzer 版本")
	flags.Usage = func() {
		_, _ = fmt.Fprintln(output, "ofd-analyzer - OFD 结构分析工具")
		_, _ = fmt.Fprintln(output, "用法：ofd-analyzer [选项] input.ofd")
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
	opts.format = strings.ToLower(strings.TrimSpace(opts.format))
	switch opts.format {
	case "text", "markdown", "json", "pdf":
	default:
		return fmt.Errorf("不支持的报告格式 %q", opts.format)
	}
	if opts.font != "" && opts.format != "pdf" {
		return errors.New("--font 只能与 --format pdf 一起使用")
	}
	if err := analyzer.ValidateInputPath(opts.input); err != nil {
		return fmt.Errorf("输入文件：%w", err)
	}
	if opts.output != "" && opts.output != "-" && samePath(opts.input, opts.output) {
		return errors.New("报告输出不能覆盖输入 OFD 文件")
	}
	return nil
}

func writeReport(opts *options, report analyzer.Report, stdout io.Writer) error {
	var buffer bytes.Buffer
	var err error
	switch opts.format {
	case "text":
		err = analyzer.RenderText(&buffer, report)
	case "markdown":
		err = analyzer.RenderMarkdown(&buffer, report)
	case "pdf":
		err = analyzer.RenderPDF(&buffer, report, analyzer.PDFOptions{Font: opts.font})
	default:
		err = analyzer.RenderJSON(&buffer, report, opts.pretty)
	}
	if err != nil {
		return fmt.Errorf("生成 %s 报告失败：%w", opts.format, err)
	}
	if opts.output == "" || opts.output == "-" {
		if _, err := stdout.Write(buffer.Bytes()); err != nil {
			return fmt.Errorf("写入标准输出失败：%w", err)
		}
		return nil
	}
	file, err := os.Create(opts.output)
	if err != nil {
		return fmt.Errorf("创建报告输出文件失败：%w", err)
	}
	if _, err := file.Write(buffer.Bytes()); err != nil {
		_ = file.Close()
		return fmt.Errorf("写入报告输出文件失败：%w", err)
	}
	if err := file.Close(); err != nil {
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
