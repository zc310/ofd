// 命令 ofd-archive 提供 OFD 档案预检、清单、归档准备和固定性验证。
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zc310/ofd/internal/utils"
	"github.com/zc310/ofd/pkg/archive"
	"github.com/zc310/ofd/pkg/validator"
)

const (
	exitOK     = 0
	exitFailed = 1
	exitUsage  = 2
	exitOutput = 3
)

type options struct {
	command           string
	input             string
	output            string
	metadata          string
	profile           string
	format            string
	formatSet         bool
	pretty            bool
	failOnWarning     bool
	mode              string
	skipXSD           bool
	noDigest          bool
	noScanXML         bool
	maxErrors         int
	maxInputSize      int64
	maxFileSize       int64
	maxTotalSize      int64
	maxEntries        int
	maxXMLBytes       int64
	maxXMLNodes       int
	maxXMLDepth       int
	maxAttachmentSize int64
	help              bool
	version           bool
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseArgs(args, stdout)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-archive:", err)
		return exitUsage
	}
	if opts.help {
		return exitOK
	}
	if opts.version {
		_, _ = fmt.Fprintln(stdout, archive.ToolVersion)
		return exitOK
	}
	if err := validateOptions(opts); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-archive:", err)
		return exitUsage
	}
	if opts.command == "verify" {
		return runVerify(opts, stdout, stderr)
	}
	metadata := archive.Metadata{}
	if opts.metadata != "" {
		metadata, err = archive.LoadMetadata(opts.metadata)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-archive:", err)
			return exitUsage
		}
	}
	archiveOptions := makeArchiveOptions(opts)
	if opts.command == "matrix" {
		matrix, matrixErr := archive.BuildMatrix(context.Background(), opts.input, archiveOptions)
		if matrixErr != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-archive:", matrixErr)
			return exitOutput
		}
		if err := writeMatrix(opts, matrix, stdout); err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-archive:", err)
			return exitOutput
		}
		return matrixExit(matrix.OverallStatus)
	}
	if opts.command == "prepare" {
		report, prepareErr := archive.Prepare(context.Background(), opts.input, opts.output, metadata, opts.profile, archiveOptions)
		if prepareErr != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-archive:", prepareErr)
			return exitOutput
		}
		if opts.format != "" {
			if err := writeReport(opts, report, stdout); err != nil {
				_, _ = fmt.Fprintln(stderr, "ofd-archive:", err)
				return exitOutput
			}
		}
		return statusExit(report.Status)
	}
	if opts.command == "manifest" {
		manifest, report, manifestErr := archive.BuildManifest(context.Background(), opts.input, metadata, opts.profile, archiveOptions)
		if manifestErr != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-archive:", manifestErr)
			return exitOutput
		}
		if err := writeReportValue(opts, manifest, stdout); err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-archive:", err)
			return exitOutput
		}
		return statusExit(report.Status)
	}
	report, checkErr := archive.Check(context.Background(), opts.input, archiveOptions)
	if checkErr != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-archive:", checkErr)
		return exitOutput
	}
	if err := writeReport(opts, report, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-archive:", err)
		return exitOutput
	}
	return statusExit(report.Status)
}

func parseArgs(args []string, output io.Writer) (*options, error) {
	opts := &options{command: "check", format: "text", mode: string(validator.ModeStrict), maxErrors: 100, maxInputSize: 512 << 20, maxFileSize: 64 << 20, maxTotalSize: 512 << 20, maxEntries: 10000, maxXMLBytes: 64 << 20, maxXMLNodes: 2_000_000, maxXMLDepth: 1000, maxAttachmentSize: 64 << 20}
	root := &cobra.Command{
		Use:           "ofd-archive [flags] input.ofd",
		Short:         "OFD 档案预检和归档准备工具",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE:          captureCommand(opts, "check"),
		Args:          cobra.ArbitraryArgs,
	}
	root.SetOut(output)
	root.SetErr(output)
	root.SetArgs(args)
	helpFunc := func(cmd *cobra.Command, args []string) {
		opts.help = true
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "ofd-archive - OFD 档案预检和归档准备工具")
		switch cmd.Name() {
		case "verify":
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "用法：ofd-archive verify [选项] archive-directory/")
		case "check", "manifest", "matrix", "prepare":
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "用法：ofd-archive %s [选项] input.ofd\n", cmd.Name())
		default:
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "用法：ofd-archive [check|manifest|matrix|prepare] [选项] input.ofd")
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "      ofd-archive verify [选项] archive-directory/")
		}
		flags := cmd.Root().PersistentFlags()
		flags.SetOutput(cmd.OutOrStdout())
		flags.PrintDefaults()
	}
	root.SetHelpFunc(helpFunc)
	flags := root.PersistentFlags()
	flags.StringVarP(&opts.output, "output", "o", "", "报告输出路径；prepare 使用它作为归档目录")
	flags.StringVar(&opts.metadata, "metadata", "", "档案著录元数据 JSON/YAML 文件")
	flags.StringVar(&opts.profile, "profile", "", "档案字段 profile JSON/YAML 文件")
	flags.StringVar(&opts.format, "format", opts.format, "输出格式：text、json、markdown 或 xlsx")
	flags.BoolVar(&opts.pretty, "pretty", false, "缩进 JSON 输出")
	flags.BoolVar(&opts.failOnWarning, "fail-on-warning", false, "发现警告时返回退出码 1")
	flags.StringVar(&opts.mode, "mode", opts.mode, "校验模式：strict、compat 或 structural")
	flags.BoolVar(&opts.skipXSD, "skip-xsd", false, "跳过 XSD 校验")
	flags.BoolVar(&opts.noDigest, "no-digest", false, "跳过签名摘要校验")
	flags.BoolVar(&opts.noScanXML, "no-scan-xml", false, "只解析由 OFD 引用到的 XML 文件")
	flags.IntVar(&opts.maxErrors, "max-errors", opts.maxErrors, "最多记录的校验错误数")
	flags.Int64Var(&opts.maxInputSize, "max-input-size", opts.maxInputSize, "ZIP 原始输入数据的最大字节数")
	flags.Int64Var(&opts.maxFileSize, "max-file-size", opts.maxFileSize, "ZIP 条目解压后的最大字节数")
	flags.Int64Var(&opts.maxTotalSize, "max-total-size", opts.maxTotalSize, "OFD 包解压后的最大总字节数")
	flags.IntVar(&opts.maxEntries, "max-entries", opts.maxEntries, "ZIP 条目的最大数量")
	flags.Int64Var(&opts.maxXMLBytes, "max-xml-bytes", opts.maxXMLBytes, "单个 XML 文件的最大字节数")
	flags.IntVar(&opts.maxXMLNodes, "max-xml-nodes", opts.maxXMLNodes, "单个 XML 文件的最大节点数")
	flags.IntVar(&opts.maxXMLDepth, "max-xml-depth", opts.maxXMLDepth, "单个 XML 文件的最大嵌套深度")
	flags.Int64Var(&opts.maxAttachmentSize, "max-attachment-size", opts.maxAttachmentSize, "提取附件的最大字节数")
	flags.BoolVar(&opts.version, "version", false, "输出工具版本")
	for _, name := range []string{"check", "manifest", "matrix", "prepare", "verify"} {
		command := name
		child := &cobra.Command{Use: command + " [flags] input.ofd", SilenceErrors: true, SilenceUsage: true, RunE: captureCommand(opts, command), Args: cobra.ArbitraryArgs}
		if command == "verify" {
			child.Use = "verify [flags] archive-directory/"
		}
		child.SetHelpFunc(helpFunc)
		root.AddCommand(child)
	}
	if err := root.Execute(); err != nil {
		return nil, err
	}
	if opts.help {
		return opts, nil
	}
	if !opts.formatFlagChanged(root) {
		switch opts.command {
		case "manifest":
			opts.format = "json"
		case "matrix":
			opts.format = "markdown"
		}
	}
	if opts.version {
		return opts, nil
	}
	if opts.input == "" {
		return nil, fmt.Errorf("%s 必须且只能指定一个输入路径", opts.command)
	}
	return opts, nil
}

func captureCommand(opts *options, command string) func(*cobra.Command, []string) error {
	return func(_ *cobra.Command, args []string) error {
		opts.command = command
		if opts.version {
			return nil
		}
		if len(args) != 1 {
			if command == "verify" {
				return fmt.Errorf("verify 必须且只能指定一个归档目录")
			}
			return fmt.Errorf("必须且只能指定一个输入 OFD 文件")
		}
		opts.input = args[0]
		return nil
	}
}

func (opts *options) formatFlagChanged(root *cobra.Command) bool {
	flag := root.PersistentFlags().Lookup("format")
	if flag == nil {
		return false
	}
	opts.formatSet = flag.Changed
	return flag.Changed
}

func validateOptions(opts *options) error {
	if opts.command == "matrix" && !opts.formatSet && strings.EqualFold(filepath.Ext(opts.output), ".xlsx") {
		opts.format = "xlsx"
	}
	if opts.command == "matrix" {
		if opts.format != "markdown" && opts.format != "json" && opts.format != "xlsx" {
			return fmt.Errorf("matrix 不支持的输出格式 %q", opts.format)
		}
	} else if (opts.command == "check" || opts.command == "prepare" || opts.command == "verify") && opts.format != "text" && opts.format != "json" && opts.format != "markdown" {
		return fmt.Errorf("不支持的输出格式 %q", opts.format)
	} else if opts.command == "manifest" && opts.format != "json" {
		return fmt.Errorf("不支持的输出格式 %q", opts.format)
	}
	if _, err := os.Stat(opts.input); err != nil {
		return fmt.Errorf("输入路径：%w", err)
	}
	if opts.command == "prepare" {
		if opts.output == "" || opts.output == "-" {
			return errors.New("prepare 必须使用 --output 指定归档目录")
		}
		if opts.metadata == "" {
			return errors.New("prepare 必须使用 --metadata 指定档案元数据")
		}
	}
	if opts.command != "matrix" && opts.output != "" && opts.output != "-" && opts.format == "xlsx" {
		return fmt.Errorf("只有 matrix 命令支持 xlsx 输出")
	}
	if opts.metadata != "" {
		if _, err := os.Stat(opts.metadata); err != nil {
			return fmt.Errorf("元数据文件：%w", err)
		}
	}
	if opts.profile != "" {
		if _, err := os.Stat(opts.profile); err != nil {
			return fmt.Errorf("profile 文件：%w", err)
		}
	}
	if opts.command != "prepare" && opts.output != "" && opts.output != "-" && utils.SamePath(opts.input, opts.output) {
		return errors.New("报告输出不能覆盖输入 OFD 文件")
	}
	if opts.maxErrors < 0 || opts.maxInputSize < 0 || opts.maxFileSize < 0 || opts.maxTotalSize < 0 || opts.maxEntries < 0 || opts.maxXMLBytes < 0 || opts.maxXMLNodes < 0 || opts.maxXMLDepth < 0 || opts.maxAttachmentSize < 0 {
		return errors.New("大小和错误数量限制不能为负数")
	}
	if opts.mode != string(validator.ModeStrict) && opts.mode != string(validator.ModeCompat) && opts.mode != string(validator.ModeStructural) {
		return fmt.Errorf("不支持的校验模式 %q", opts.mode)
	}
	if opts.skipXSD && opts.mode == string(validator.ModeStrict) {
		opts.mode = string(validator.ModeStructural)
	}
	return nil
}

func makeArchiveOptions(opts *options) archive.Options {
	return archive.Options{ValidatorOptions: []validator.Option{validator.WithMode(validator.Mode(opts.mode)), validator.WithMaxErrors(opts.maxErrors), validator.WithMaxInputSize(opts.maxInputSize), validator.WithMaxFileSize(opts.maxFileSize), validator.WithMaxTotalSize(opts.maxTotalSize), validator.WithMaxEntries(opts.maxEntries), validator.WithMaxXMLBytes(opts.maxXMLBytes), validator.WithMaxXMLNodes(opts.maxXMLNodes), validator.WithMaxXMLDepth(opts.maxXMLDepth), validator.WithSkipXSD(opts.skipXSD), validator.WithCheckDigest(!opts.noDigest), validator.WithScanXML(!opts.noScanXML)}, FailOnWarning: opts.failOnWarning, MaxAttachmentSize: opts.maxAttachmentSize, MaxXMLBytes: opts.maxXMLBytes}
}

func runVerify(opts *options, stdout, stderr io.Writer) int {
	report, err := archive.Verify(opts.input)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-archive:", err)
		return exitOutput
	}
	if err := writeReport(opts, report, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-archive:", err)
		return exitOutput
	}
	return statusExit(report.Status)
}

func statusExit(status string) int {
	if status == archive.StatusFailed {
		return exitFailed
	}
	return exitOK
}

func matrixExit(status string) int {
	if status == archive.MatrixFailed {
		return exitFailed
	}
	return exitOK
}

func writeMatrix(opts *options, matrix archive.MatrixReport, stdout io.Writer) error {
	if opts.output != "" && opts.output != "-" {
		file, err := os.OpenFile(opts.output, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		err = renderMatrix(opts, file, matrix)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		return err
	}
	return renderMatrix(opts, stdout, matrix)
}

func renderMatrix(opts *options, writer io.Writer, matrix archive.MatrixReport) error {
	if opts.format == "json" {
		return archive.RenderJSON(writer, matrix, opts.pretty)
	}
	if opts.format == "xlsx" {
		return archive.RenderMatrixXLSX(writer, matrix)
	}
	return archive.RenderMatrixMarkdown(writer, matrix)
}

func writeReport(opts *options, report archive.Report, stdout io.Writer) error {
	if opts.command == "prepare" {
		return render(opts, stdout, report)
	}
	return writeReportValue(opts, report, stdout)
}

func writeReportValue(opts *options, value any, stdout io.Writer) error {
	if opts.output != "" && opts.output != "-" {
		file, err := os.OpenFile(opts.output, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		err = render(opts, file, value)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		return err
	}
	return render(opts, stdout, value)
}

func render(opts *options, writer io.Writer, value any) error {
	if opts.format == "json" {
		return archive.RenderJSON(writer, value, opts.pretty)
	}
	report, ok := value.(archive.Report)
	if !ok {
		return archive.RenderJSON(writer, value, true)
	}
	if opts.format == "markdown" {
		return archive.RenderMarkdown(writer, report)
	}
	return archive.RenderText(writer, report)
}
