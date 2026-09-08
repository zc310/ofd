// 命令 ofd-creator 根据 JSON 或 YAML manifest 创建 OFD 文件。
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

	"github.com/zc310/ofd/internal/manifest"
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
)

type options struct {
	input         string
	output        string
	assetRoot     string
	format        string
	compression   string
	validate      bool
	check         bool
	deterministic bool
	help          bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseArgs(args, stderr)
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stderr, "ofd-creator:", err)
		}
		return exitUsage
	}
	if opts.help {
		return exitOK
	}
	if err := validateOptions(opts); err != nil {
		fmt.Fprintln(stderr, "ofd-creator:", err)
		return exitUsage
	}
	m, baseDir, err := manifest.Load(opts.input, opts.format)
	if err != nil {
		fmt.Fprintln(stderr, "ofd-creator:", err)
		return exitResource
	}
	document, err := m.Build(baseDir, opts.assetRoot)
	if err != nil {
		fmt.Fprintln(stderr, "ofd-creator:", err)
		return exitResource
	}
	compression := creator.CompressionMode(strings.ToLower(strings.TrimSpace(opts.compression)))
	data, err := creator.MarshalWithOptions(document, creator.CreateOptions{Compression: compression, Deterministic: opts.deterministic})
	if err != nil {
		fmt.Fprintln(stderr, "ofd-creator:", err)
		return exitBuild
	}
	if opts.check {
		return exitOK
	}
	if opts.validate {
		instance, validatorErr := validator.New()
		if validatorErr != nil {
			fmt.Fprintln(stderr, "ofd-creator:", validatorErr)
			return exitValidate
		}
		report := instance.ValidateReader(context.Background(), bytes.NewReader(data), "generated.ofd")
		if report.HasErrors() {
			_ = validator.RenderText(stderr, report)
			return exitValidate
		}
	}
	if err := writeOutput(opts.output, data, stdout); err != nil {
		fmt.Fprintln(stderr, "ofd-creator:", err)
		return exitOutput
	}
	return exitOK
}

func parseArgs(args []string, output io.Writer) (*options, error) {
	opts := &options{compression: string(creator.CompressionAuto)}
	flags := flag.NewFlagSet("ofd-creator", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&opts.input, "i", "", "manifest 文件路径；使用 - 从标准输入读取")
	flags.StringVar(&opts.input, "input", "", "manifest 文件路径；使用 - 从标准输入读取")
	flags.StringVar(&opts.output, "o", "", "OFD 输出路径；使用 - 写入标准输出")
	flags.StringVar(&opts.output, "output", "", "OFD 输出路径；使用 - 写入标准输出")
	flags.StringVar(&opts.assetRoot, "asset-root", "", "资源根目录，默认使用 manifest 所在目录")
	flags.StringVar(&opts.format, "format", "auto", "manifest 格式：auto、json、yaml 或 toml")
	flags.StringVar(&opts.compression, "compression", opts.compression, "ZIP 压缩策略：auto、deflate 或 store")
	flags.BoolVar(&opts.validate, "validate", false, "生成后执行严格 OFD 校验")
	flags.BoolVar(&opts.check, "check", false, "只解析并校验 manifest，不写出 OFD")
	flags.BoolVar(&opts.deterministic, "deterministic", false, "使用固定 ZIP 时间，生成可复现的 OFD")
	flags.BoolVar(&opts.help, "help", false, "显示帮助")
	flags.Usage = func() {
		_, _ = fmt.Fprintln(output, "ofd-creator - OFD 文件创建工具")
		_, _ = fmt.Fprintln(output, "用法：ofd-creator -i document.yaml -o result.ofd [选项]")
		_, _ = fmt.Fprintln(output)
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return &options{help: true}, nil
		}
		return nil, err
	}
	if opts.input == "" && flags.NArg() > 0 {
		opts.input = flags.Arg(0)
	}
	if opts.output == "" && flags.NArg() > 1 {
		opts.output = flags.Arg(1)
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
	if opts.output != "-" && samePath(opts.input, opts.output) {
		return errors.New("输出文件不能覆盖 manifest 输入文件")
	}
	switch creator.CompressionMode(strings.ToLower(strings.TrimSpace(opts.compression))) {
	case creator.CompressionAuto, creator.CompressionDeflate, creator.CompressionStore:
	default:
		return fmt.Errorf("不支持的 ZIP 压缩策略 %q", opts.compression)
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
	defer os.Remove(temporaryName)
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

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}
