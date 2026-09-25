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
	ofdexport "github.com/zc310/ofd/internal/export"
	"github.com/zc310/ofd/internal/utils"
)

func runExport(args []string, stdout, stderr io.Writer) int {
	opts, err := parseExportArgs(args, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator export:", err)
		return exitUsage
	}
	if opts.help {
		return exitOK
	}
	if err := validateExportOptions(opts); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator export:", err)
		return exitUsage
	}
	assetRoot, assetPrefix, err := resolveExportAssetRoot(opts)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator export:", err)
		return exitOutput
	}
	input, err := exportInput(opts.input, os.Stdin)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator export:", err)
		return exitResource
	}
	var manifestData bytes.Buffer
	var exportErr error
	if opts.document >= 0 {
		exportErr = ofdexport.WriteDocumentManifest(input, opts.document, &manifestData, ofdexport.Options{AssetRoot: assetRoot, AssetPrefix: assetPrefix, Format: opts.format, JSONIndent: opts.jsonIndent})
	} else {
		exportErr = ofdexport.WriteManifest(input, &manifestData, ofdexport.Options{AssetRoot: assetRoot, AssetPrefix: assetPrefix, Format: opts.format, JSONIndent: opts.jsonIndent})
	}
	if exportErr != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator export:", exportErr)
		return exitResource
	}
	if err := writeOutput(opts.output, manifestData.Bytes(), stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator export:", err)
		return exitOutput
	}
	return exitOK
}

func runExportAll(args []string, _, stderr io.Writer) int {
	opts, err := parseExportAllArgs(args, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator export-all:", err)
		return exitUsage
	}
	if opts.help {
		return exitOK
	}
	if strings.TrimSpace(opts.input) == "" || strings.TrimSpace(opts.output) == "" {
		_, _ = fmt.Fprintln(stderr, "ofd-creator export-all: 必须设置 OFD 输入文件和导出目录")
		return exitUsage
	}
	if opts.output == "-" {
		_, _ = fmt.Fprintln(stderr, "ofd-creator export-all: 批量导出必须写入目录，不能使用标准输出")
		return exitUsage
	}
	if opts.output != "-" && opts.input != "-" && utils.SamePath(opts.input, opts.output) {
		_, _ = fmt.Fprintln(stderr, "ofd-creator export-all: 输出目录不能覆盖 OFD 输入文件")
		return exitUsage
	}
	input, err := exportInput(opts.input, os.Stdin)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator export-all:", err)
		return exitResource
	}
	if err := ofdexport.WriteBundle(input, opts.output, ofdexport.Options{Format: opts.format, JSONIndent: opts.jsonIndent}); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator export-all:", err)
		return exitResource
	}
	return exitOK
}

type exportAllOptions struct {
	input      string
	output     string
	format     string
	jsonIndent bool
	help       bool
}

func parseExportAllArgs(args []string, output io.Writer) (*exportAllOptions, error) {
	opts := &exportAllOptions{format: "yaml"}
	root := &cobra.Command{
		Use:           "ofd-creator export-all",
		Short:         "导出 OFD 的全部文档体",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, positional []string) error {
			if opts.help {
				return nil
			}
			if opts.input == "" && len(positional) > 0 {
				opts.input = positional[0]
			}
			if opts.output == "" && len(positional) > 1 {
				opts.output = positional[1]
			}
			return nil
		},
	}
	root.SetArgs(args)
	root.SetOut(output)
	root.SetErr(output)
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		opts.help = true
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "ofd-creator export-all - 导出 OFD 的全部文档体")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "用法：ofd-creator export-all -i input.ofd -o exported/ --format yaml")
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		flags := cmd.Flags()
		flags.SetOutput(cmd.OutOrStdout())
		flags.PrintDefaults()
	})
	flags := root.Flags()
	flags.StringVarP(&opts.input, "input", "i", "", "OFD 输入文件；使用 - 从标准输入读取")
	flags.StringVarP(&opts.output, "output", "o", "", "批量导出目录")
	flags.StringVar(&opts.format, "format", opts.format, "导出格式：留空时根据输出文件扩展名推断，也可指定 yaml、json 或 toml")
	flags.BoolVar(&opts.jsonIndent, "json-indent", false, "JSON 输出使用 2 空格缩进")
	if err := root.Execute(); err != nil {
		return nil, err
	}
	if opts.help {
		return opts, nil
	}
	format := strings.ToLower(strings.TrimSpace(opts.format))
	if format == "" {
		format = "yaml"
	}
	if format == "yml" {
		format = "yaml"
	}
	if format != "yaml" && format != "json" && format != "toml" {
		return nil, fmt.Errorf("不支持的导出格式 %q", opts.format)
	}
	opts.format = format
	return opts, nil
}

type exportOptions struct {
	input      string
	output     string
	assetRoot  string
	format     string
	jsonIndent bool
	document   int
	help       bool
}

func parseExportArgs(args []string, output io.Writer) (*exportOptions, error) {
	opts := &exportOptions{assetRoot: "assets", document: -1}
	root := &cobra.Command{
		Use:           "ofd-creator export",
		Short:         "从 OFD 导出 creator manifest",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, positional []string) error {
			if opts.help {
				return nil
			}
			if opts.input == "" && len(positional) > 0 {
				opts.input = positional[0]
			}
			if opts.output == "" && len(positional) > 1 {
				opts.output = positional[1]
			}
			return nil
		},
	}
	root.SetArgs(args)
	root.SetOut(output)
	root.SetErr(output)
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		opts.help = true
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "ofd-creator export - 从 OFD 导出 creator manifest")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "用法：ofd-creator export -i input.ofd -o document.yaml --format yaml --asset-root assets")
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		flags := cmd.Flags()
		flags.SetOutput(cmd.OutOrStdout())
		flags.PrintDefaults()
	})
	flags := root.Flags()
	flags.StringVarP(&opts.input, "input", "i", "", "OFD 输入文件；使用 - 从标准输入读取")
	flags.StringVarP(&opts.output, "output", "o", "", "manifest 输出文件；使用 - 写入标准输出")
	flags.StringVar(&opts.assetRoot, "asset-root", opts.assetRoot, "资源输出目录")
	flags.StringVar(&opts.format, "format", "", "导出格式：留空时根据输出文件扩展名推断，也可指定 yaml、json 或 toml")
	flags.BoolVar(&opts.jsonIndent, "json-indent", false, "JSON 输出使用 2 空格缩进")
	flags.IntVar(&opts.document, "document", -1, "要导出的文档体索引，从 0 开始；默认拒绝多文档输入")
	if err := root.Execute(); err != nil {
		return nil, err
	}
	if opts.help {
		return opts, nil
	}
	return opts, nil
}

func validateExportOptions(opts *exportOptions) error {
	if strings.TrimSpace(opts.input) == "" {
		return errors.New("缺少 OFD 输入文件")
	}
	if strings.TrimSpace(opts.output) == "" {
		return errors.New("缺少 manifest 输出文件")
	}
	format := strings.ToLower(strings.TrimSpace(opts.format))
	if format == "" {
		format = exportFormatFromPath(opts.output)
	}
	if format == "yml" {
		format = "yaml"
	}
	if format != "yaml" && format != "json" && format != "toml" {
		return fmt.Errorf("不支持的导出格式 %q", opts.format)
	}
	opts.format = format
	if opts.output != "-" && opts.input != "-" && utils.SamePath(opts.input, opts.output) {
		return errors.New("输出文件不能覆盖 OFD 输入文件")
	}
	if opts.document < -1 {
		return errors.New("文档体索引不能小于 0")
	}
	return nil
}

func exportFormatFromPath(output string) string {
	if output == "-" {
		return "yaml"
	}
	switch strings.ToLower(filepath.Ext(output)) {
	case ".json":
		return "json"
	case ".toml":
		return "toml"
	case ".yaml", ".yml":
		return "yaml"
	default:
		return "yaml"
	}
}

func resolveExportAssetRoot(opts *exportOptions) (string, string, error) {
	manifestDir, err := os.Getwd()
	if err != nil {
		return "", "", fmt.Errorf("获取当前目录失败: %w", err)
	}
	if opts.output != "-" {
		manifestDir, err = filepath.Abs(filepath.Dir(opts.output))
		if err != nil {
			return "", "", fmt.Errorf("获取 manifest 输出目录失败: %w", err)
		}
	}
	assetRoot := opts.assetRoot
	if !filepath.IsAbs(assetRoot) {
		assetRoot = filepath.Join(manifestDir, assetRoot)
	}
	assetRoot, err = filepath.Abs(assetRoot)
	if err != nil {
		return "", "", fmt.Errorf("资源输出目录无效: %w", err)
	}
	prefix, err := filepath.Rel(manifestDir, assetRoot)
	if err != nil {
		return "", "", fmt.Errorf("计算资源相对路径失败: %w", err)
	}
	if prefix == ".." || strings.HasPrefix(prefix, ".."+string(filepath.Separator)) || filepath.IsAbs(prefix) {
		return "", "", errors.New("资源输出目录必须位于 manifest 输出目录内")
	}
	if prefix == "." {
		prefix = ""
	}
	return assetRoot, filepath.ToSlash(prefix), nil
}

func exportInput(name string, stdin io.Reader) (any, error) {
	if name != "-" {
		return name, nil
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return nil, fmt.Errorf("读取标准输入失败: %w", err)
	}
	return data, nil
}
