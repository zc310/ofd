// 命令 ofd-analyzer 分析 OFD 文件结构并生成分析报告。
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zc310/ofd/internal/utils"
	"github.com/zc310/ofd/pkg/analyzer"
)

const (
	exitOK     = 0
	exitFailed = 1
	exitUsage  = 2
)

type options struct {
	input            string
	output           string
	format           string
	formatSet        bool
	font             string
	signatureUID     string
	signatureFormat  string
	signatureRoots   string
	signatureCRLs    string
	signatureIssuers string
	pretty           bool
	noTemplates      bool
	noAnnotations    bool
	noSignatures     bool
	tree             bool
	failOnWarning    bool
	version          bool
	help             bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseArgs(args, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-analyzer:", err)
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
	if opts.signatureUID != "" {
		analysisOptions = append(analysisOptions, analyzer.WithSignatureUID(opts.signatureUID))
	}
	if opts.signatureFormat != "" {
		analysisOptions = append(analysisOptions, analyzer.WithSignatureFormat(opts.signatureFormat))
	}
	if opts.signatureRoots != "" {
		certificates, err := os.ReadFile(opts.signatureRoots)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-analyzer: 读取签名信任根失败:", err)
			return exitUsage
		}
		analysisOptions = append(analysisOptions, analyzer.WithSignatureTrustRootsPEM(certificates))
	}
	if opts.signatureCRLs != "" {
		crls, err := os.ReadFile(opts.signatureCRLs)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-analyzer: 读取签名 CRL 失败:", err)
			return exitUsage
		}
		analysisOptions = append(analysisOptions, analyzer.WithSignatureCRLsPEM(crls))
	}
	if opts.signatureIssuers != "" {
		issuers, err := os.ReadFile(opts.signatureIssuers)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-analyzer: 读取 CRL 签发者证书失败:", err)
			return exitUsage
		}
		analysisOptions = append(analysisOptions, analyzer.WithSignatureRevocationIssuersPEM(issuers))
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
	opts := &options{format: "text"}
	root := &cobra.Command{
		Use:           "ofd-analyzer [flags] input.ofd",
		Short:         "OFD 结构分析工具",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, positional []string) error {
			if opts.version || opts.help {
				return nil
			}
			if len(positional) == 0 {
				return errors.New("缺少输入 OFD 文件")
			}
			if len(positional) != 1 {
				return errors.New("必须且只能指定一个输入 OFD 文件")
			}
			opts.input = positional[0]
			return nil
		},
	}
	root.SetArgs(args)
	root.SetOut(output)
	root.SetErr(output)
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		opts.help = true
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "ofd-analyzer - OFD 结构分析工具")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "用法：ofd-analyzer [选项] input.ofd")
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		flags := cmd.Flags()
		flags.SetOutput(cmd.OutOrStdout())
		flags.PrintDefaults()
	})
	flags := root.Flags()
	flags.StringVarP(&opts.output, "output", "o", "", "报告输出路径；使用 - 输出到标准输出")
	flags.StringVar(&opts.format, "format", opts.format, "报告格式：text、markdown、json、pdf 或 xlsx；默认为 text")
	flags.StringVar(&opts.font, "font", "", "PDF 报告使用的字体文件")
	flags.StringVar(&opts.signatureUID, "signature-uid", "", "SM2 签名用户标识")
	flags.StringVar(&opts.signatureFormat, "signature-format", "", "SM2 签名值格式：auto、der 或 raw")
	flags.StringVar(&opts.signatureRoots, "signature-roots", "", "签名证书链校验使用的 PEM 信任根文件")
	flags.StringVar(&opts.signatureCRLs, "signature-crls", "", "离线签名证书吊销校验使用的 PEM/DER CRL 文件")
	flags.StringVar(&opts.signatureIssuers, "signature-revocation-issuers", "", "验证 CRL 签名使用的 PEM 签发者证书文件")
	flags.BoolVar(&opts.pretty, "pretty", false, "缩进 JSON 输出")
	flags.BoolVar(&opts.noTemplates, "no-templates", false, "跳过模板定义、引用和 PageRes 资源分析，但保留模板元数据")
	flags.BoolVar(&opts.noAnnotations, "no-annotations", false, "跳过注解及 Appearance 分析")
	flags.BoolVar(&opts.noSignatures, "no-signatures", false, "跳过签名清单分析")
	flags.BoolVar(&opts.tree, "tree", false, "输出 OFD ZIP 包目录结构")
	flags.BoolVar(&opts.failOnWarning, "fail-on-warning", false, "发现警告时返回退出码 1")
	flags.BoolVar(&opts.version, "version", false, "输出 analyzer 版本")
	if err := root.Execute(); err != nil {
		return nil, err
	}
	if opts.help || opts.version {
		return opts, nil
	}
	if flag := root.Flags().Lookup("format"); flag != nil {
		opts.formatSet = flag.Changed
	}
	return opts, nil
}

func validateOptions(opts *options) error {
	if !opts.formatSet {
		if format, ok := utils.ReportFormatFromOutput(opts.output); ok {
			opts.format = format
		}
	}
	opts.format = strings.ToLower(strings.TrimSpace(opts.format))
	switch opts.format {
	case "text", "markdown", "json", "pdf", "xlsx":
	default:
		return fmt.Errorf("不支持的报告格式 %q", opts.format)
	}
	if opts.font != "" && opts.format != "pdf" {
		return errors.New("--font 只能与 --format pdf 一起使用")
	}
	if opts.signatureFormat != "" {
		switch strings.ToLower(strings.TrimSpace(opts.signatureFormat)) {
		case "auto", "der", "raw":
			opts.signatureFormat = strings.ToLower(strings.TrimSpace(opts.signatureFormat))
		default:
			return fmt.Errorf("不支持的签名值格式 %q", opts.signatureFormat)
		}
	}
	if opts.signatureRoots != "" {
		if info, err := os.Stat(opts.signatureRoots); err != nil || info.IsDir() {
			if err != nil {
				return fmt.Errorf("签名信任根文件：%w", err)
			}
			return errors.New("签名信任根路径不能是目录")
		}
	}
	if opts.signatureCRLs != "" {
		if info, err := os.Stat(opts.signatureCRLs); err != nil || info.IsDir() {
			if err != nil {
				return fmt.Errorf("签名 CRL 文件：%w", err)
			}
			return errors.New("签名 CRL 路径不能是目录")
		}
	}
	if opts.signatureIssuers != "" {
		if info, err := os.Stat(opts.signatureIssuers); err != nil || info.IsDir() {
			if err != nil {
				return fmt.Errorf("CRL 签发者证书文件：%w", err)
			}
			return errors.New("CRL 签发者证书路径不能是目录")
		}
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
	case "xlsx":
		err = analyzer.RenderXLSX(&buffer, report)
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
