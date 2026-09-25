// 命令 ofd-creator 根据 JSON、YAML 或 TOML manifest 创建 OFD 文件。
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/internal/utils"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/validator"
)

const (
	exitOK       = 0
	exitUsage    = 2
	exitBuild    = 3
	exitResource = 4
	exitOutput   = 5
	exitValidate = 6

	defaultMergeWorkers = 4
)

type options struct {
	input                  string
	output                 string
	assetRoot              string
	format                 string
	compression            string
	compressionLevel       int
	validate               bool
	check                  bool
	deterministic          bool
	completeTextCodeDeltas bool
	stream                 bool
	help                   bool
}

var errOFDInvalid = errors.New("OFD 校验失败")

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && strings.EqualFold(args[0], "export-all") {
		return runExportAll(args[1:], stdout, stderr)
	}
	if len(args) > 0 && strings.EqualFold(args[0], "export") {
		return runExport(args[1:], stdout, stderr)
	}
	if len(args) > 0 && strings.EqualFold(args[0], "merge") {
		return runMerge(args[1:], stdout, stderr)
	}
	if len(args) > 0 && strings.EqualFold(args[0], "replace") {
		return runReplace(args[1:], stdout, stderr)
	}
	if len(args) > 0 && strings.EqualFold(args[0], "watermark") {
		return runWatermark(args[1:], stdout, stderr)
	}
	opts, err := parseArgs(args, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator:", err)
		return exitUsage
	}
	if opts.help {
		return exitOK
	}
	if err := validateOptions(opts); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator:", err)
		return exitUsage
	}
	m, baseDir, err := manifest.Load(opts.input, opts.format)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator:", err)
		return exitResource
	}
	document, err := m.BuildWithOptions(baseDir, opts.assetRoot, manifest.BuildOptions{StreamAssets: opts.stream})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator:", err)
		return exitResource
	}
	compression := creator.CompressionMode(strings.ToLower(strings.TrimSpace(opts.compression)))
	createOptions := creator.CreateOptions{Compression: compression, CompressionLevel: opts.compressionLevel, Deterministic: opts.deterministic, CompleteTextCodeDeltas: opts.completeTextCodeDeltas}
	if opts.stream && !opts.check && opts.output != "-" {
		if err := writeOutputStream(opts, document, createOptions, stderr); err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator:", err)
			if errors.Is(err, errOFDInvalid) {
				return exitValidate
			}
			return exitOutput
		}
		return exitOK
	}
	data, err := creator.MarshalWithOptions(document, createOptions)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator:", err)
		return exitBuild
	}
	if opts.check {
		return exitOK
	}
	if opts.validate {
		instance, validatorErr := validator.New()
		if validatorErr != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator:", validatorErr)
			return exitValidate
		}
		report := instance.ValidateReader(context.Background(), bytes.NewReader(data), "generated.ofd")
		if report.HasErrors() {
			_ = validator.RenderText(stderr, report)
			return exitValidate
		}
	}
	if err := writeOutput(opts.output, data, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator:", err)
		return exitOutput
	}
	return exitOK
}

// writeOutputStream 以流式方式生成 OFD 并原子写入目标文件，避免把整个
// 文件包驻留内存。启用 --validate 时在临时文件上执行严格校验。
func writeOutputStream(opts *options, document creator.Document, createOptions creator.CreateOptions, stderr io.Writer) error {
	dir := filepath.Dir(opts.output)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}
	temporary, err := os.CreateTemp(dir, ".ofd-creator-*")
	if err != nil {
		return fmt.Errorf("创建临时输出文件失败: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryName)
	}()
	if err := creator.CreateWithOptions(document, temporary, createOptions); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭临时输出文件失败: %w", err)
	}
	if opts.validate {
		instance, validatorErr := validator.New()
		if validatorErr != nil {
			return validatorErr
		}
		file, openErr := os.Open(temporaryName)
		if openErr != nil {
			return fmt.Errorf("读取临时 OFD 失败: %w", openErr)
		}
		report := instance.ValidateReader(context.Background(), file, "generated.ofd")
		_ = file.Close()
		if report.HasErrors() {
			_ = validator.RenderText(stderr, report)
			return errOFDInvalid
		}
	}
	if err := os.Rename(temporaryName, opts.output); err != nil {
		return fmt.Errorf("替换输出文件失败: %w", err)
	}
	return nil
}

func parseArgs(args []string, output io.Writer) (*options, error) {
	opts := &options{compression: string(creator.CompressionAuto)}
	root := &cobra.Command{
		Use:           "ofd-creator [flags]",
		Short:         "OFD 文件创建工具",
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
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "ofd-creator - OFD 文件创建工具")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "用法：ofd-creator -i document.yaml -o result.ofd [选项]")
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "子命令：")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  merge                           合并多个 OFD 为多文档 OFD")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  replace                         替换、新增或删除 OFD 包内条目")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  watermark <add|replace|remove>   添加、替换或删除文档水印")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  export                          从 OFD 导出 creator manifest")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "  export-all                      导出 OFD 的全部文档体")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "运行 ofd-creator <子命令> -h 查看对应细节。")
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		flags := cmd.Flags()
		flags.SetOutput(cmd.OutOrStdout())
		flags.PrintDefaults()
	})
	flags := root.Flags()
	flags.StringVarP(&opts.input, "input", "i", "", "manifest 文件路径；使用 - 从标准输入读取")
	flags.StringVarP(&opts.output, "output", "o", "", "OFD 输出路径；使用 - 写入标准输出")
	flags.StringVar(&opts.assetRoot, "asset-root", "", "资源根目录，默认使用 manifest 所在目录")
	flags.StringVar(&opts.format, "format", "auto", "manifest 格式：auto、json、yaml 或 toml")
	flags.StringVar(&opts.compression, "compression", opts.compression, "ZIP 压缩策略：auto、deflate 或 store")
	flags.IntVar(&opts.compressionLevel, "compression-level", 0, "DEFLATE 压缩级别：0 使用默认级别 5，1（最快）到 9（最紧凑）")
	flags.BoolVar(&opts.validate, "validate", false, "生成后执行严格 OFD 校验")
	flags.BoolVar(&opts.check, "check", false, "只解析并校验 manifest，不写出 OFD")
	flags.BoolVar(&opts.deterministic, "deterministic", false, "使用固定 ZIP 时间，生成可复现的 OFD")
	flags.BoolVar(&opts.completeTextCodeDeltas, "complete-text-code-deltas", false, "自动补全多字符 TextCode 的 DeltaX 和 DeltaY")
	flags.BoolVar(&opts.stream, "stream", false, "以流式方式读取资源和写出 OFD，避免大资源整体驻留内存")
	if err := root.Execute(); err != nil {
		return nil, err
	}
	if opts.help {
		return opts, nil
	}
	return opts, nil
}

func validateOptions(opts *options) error {
	if strings.TrimSpace(opts.input) == "" {
		return errors.New("缺少 manifest 输入文件")
	}
	if strings.TrimSpace(opts.output) == "" && !opts.check {
		return errors.New("缺少 OFD 输出文件")
	}
	if opts.output != "-" && utils.SamePath(opts.input, opts.output) {
		return errors.New("输出文件不能覆盖 manifest 输入文件")
	}
	switch creator.CompressionMode(strings.ToLower(strings.TrimSpace(opts.compression))) {
	case creator.CompressionAuto, creator.CompressionDeflate, creator.CompressionStore:
	default:
		return fmt.Errorf("不支持的 ZIP 压缩策略 %q", opts.compression)
	}
	if _, err := creator.NormalizeCompressionLevel(opts.compressionLevel); err != nil {
		return err
	}
	if opts.format != "auto" {
		format := strings.ToLower(strings.TrimSpace(opts.format))
		if format != "json" && format != "yaml" && format != "yml" && format != "toml" {
			return fmt.Errorf("不支持的 manifest 格式 %q", opts.format)
		}
	}
	return nil
}

func writeOutput(name string, data []byte, stdout io.Writer) error {
	if name == "-" {
		_, err := stdout.Write(data)
		return err
	}
	dir := filepath.Dir(name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}
	temporary, err := os.CreateTemp(dir, ".ofd-creator-*")
	if err != nil {
		return fmt.Errorf("创建临时输出文件失败: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = os.Remove(temporaryName)
	}()
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("写入临时输出文件失败: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭临时输出文件失败: %w", err)
	}
	if err := os.Rename(temporaryName, name); err != nil {
		return fmt.Errorf("替换输出文件失败: %w", err)
	}
	return nil
}
