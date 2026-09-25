package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zc310/ofd/internal/utils"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/merge"
	"github.com/zc310/ofd/pkg/replace"
	"github.com/zc310/ofd/pkg/sign"
)

type replaceOptions struct {
	input            string
	output           string
	compression      string
	compressionLevel int
	signatures       string
	sets             []string
	adds             []string
	deletes          []string
	maxEntries       int
	maxEntryMB       int
	maxTotalMB       int
	deterministic    bool
	validate         bool
	noValidate       bool
	verifySignatures bool
	help             bool
	signFlags
}

func parseReplaceArgs(args []string, output io.Writer) (*replaceOptions, error) {
	opts := &replaceOptions{compression: string(creator.CompressionAuto), signatures: string(creator.SignatureDrop)}
	root := &cobra.Command{
		Use:           "ofd-creator replace",
		Short:         "替换、新增或删除 OFD 包内条目",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, positional []string) error {
			if opts.help {
				return nil
			}
			if len(positional) > 0 && strings.TrimSpace(opts.input) == "" {
				opts.input = positional[0]
			}
			return nil
		},
	}
	root.SetArgs(args)
	root.SetOut(output)
	root.SetErr(output)
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		opts.help = true
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "ofd-creator replace - 替换、新增或删除 OFD 包内条目")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "用法：ofd-creator replace -i in.ofd -o out.ofd --set NAME=FILE --add NAME=FILE --delete NAME")
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		flags := cmd.Flags()
		flags.SetOutput(cmd.OutOrStdout())
		flags.PrintDefaults()
	})
	flags := root.Flags()
	flags.StringVarP(&opts.input, "input", "i", "", "输入的 OFD 文件；也可作为位置参数")
	flags.StringVarP(&opts.output, "output", "o", "", "输出 OFD 路径；使用 - 写入标准输出")
	flags.StringVar(&opts.compression, "compression", opts.compression, "ZIP 压缩策略：auto、deflate 或 store")
	flags.IntVar(&opts.compressionLevel, "compression-level", 0, "DEFLATE 压缩级别：0 使用默认级别 5，1（最快）到 9（最紧凑）")
	flags.StringVar(&opts.signatures, "signatures", opts.signatures, "签名处理方式：drop、preserve 或 rewrite")
	flags.StringArrayVar(&opts.sets, "set", nil, "替换已有条目，格式 NAME=FILE（FILE 为 - 时读标准输入），可重复")
	flags.StringArrayVar(&opts.adds, "add", nil, "新增条目，格式 NAME=FILE（FILE 为 - 时读标准输入），可重复")
	flags.StringArrayVar(&opts.deletes, "delete", nil, "删除已有条目，格式 NAME，可重复")
	flags.IntVar(&opts.maxEntries, "max-entries", 0, "最多搬运的条目数，0 表示使用默认值 10000")
	flags.IntVar(&opts.maxEntryMB, "max-entry-mb", 0, "单个条目解压后的最大 MB，0 表示使用默认值 64")
	flags.IntVar(&opts.maxTotalMB, "max-total-mb", 0, "所有条目解压后的总 MB 上限，0 表示使用默认值 512")
	flags.BoolVar(&opts.deterministic, "deterministic", false, "使用固定 ZIP 时间，生成可复现的 OFD")
	flags.BoolVar(&opts.validate, "validate", false, "替换后对整体输出执行严格 OFD 校验")
	flags.BoolVar(&opts.noValidate, "no-validate", false, "关闭默认的 XML 良构检查（新内容为 .xml 条目时默认解析校验）")
	flags.BoolVar(&opts.verifySignatures, "verify-signatures", false, "替换后校验输出文档的签名摘要与密码学签名")
	registerSignFlags(flags, &opts.signFlags)
	if err := root.Execute(); err != nil {
		return nil, err
	}
	return opts, nil
}

func validateReplaceOptions(opts *replaceOptions) error {
	if strings.TrimSpace(opts.input) == "" {
		return errors.New("缺少输入的 OFD 文件")
	}
	if strings.TrimSpace(opts.output) == "" {
		return errors.New("缺少输出 OFD 文件")
	}
	if opts.input == "-" {
		return errors.New("replace 不支持从标准输入读取 OFD")
	}
	if opts.output != "-" && utils.SamePath(opts.input, opts.output) {
		return errors.New("输出文件不能覆盖输入 OFD 文件")
	}
	switch creator.CompressionMode(strings.ToLower(strings.TrimSpace(opts.compression))) {
	case creator.CompressionAuto, creator.CompressionDeflate, creator.CompressionStore:
	default:
		return fmt.Errorf("不支持的 ZIP 压缩策略 %q", opts.compression)
	}
	if _, err := creator.NormalizeCompressionLevel(opts.compressionLevel); err != nil {
		return err
	}
	switch creator.SignatureMode(strings.ToLower(strings.TrimSpace(opts.signatures))) {
	case "", creator.SignatureDrop, creator.SignaturePreserve, creator.SignatureRewrite:
	default:
		return fmt.Errorf("不支持的签名处理方式 %q", opts.signatures)
	}
	if opts.maxEntries < 0 || opts.maxEntryMB < 0 || opts.maxTotalMB < 0 {
		return errors.New("替换规模限制不能为负数")
	}
	if len(opts.sets)+len(opts.adds)+len(opts.deletes) == 0 {
		return errors.New("至少需要一个 --set、--add 或 --delete")
	}
	return nil
}

func parseReplaceOperation(kind replace.OperationKind, spec string) (replace.Operation, error) {
	switch kind {
	case replace.OpDelete:
		name := strings.TrimSpace(spec)
		if name == "" {
			return replace.Operation{}, errors.New("--delete 缺少条目路径")
		}
		return replace.Operation{Kind: kind, Name: name}, nil
	default:
		index := strings.Index(spec, "=")
		if index < 0 {
			return replace.Operation{}, fmt.Errorf("%s 参数需为 NAME=FILE 格式: %q", kind, spec)
		}
		name := strings.TrimSpace(spec[:index])
		file := spec[index+1:]
		if name == "" {
			return replace.Operation{}, fmt.Errorf("%s 缺少条目路径: %q", kind, spec)
		}
		if file == "" {
			return replace.Operation{}, fmt.Errorf("%s 缺少来源文件: %q", kind, spec)
		}
		data, err := readReplaceSource(file)
		if err != nil {
			return replace.Operation{}, err
		}
		return replace.Operation{Kind: kind, Name: name, Data: data}, nil
	}
}

func readReplaceSource(file string) ([]byte, error) {
	if file == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("读取标准输入失败: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", file, err)
	}
	return data, nil
}

func runReplace(args []string, stdout, stderr io.Writer) int {
	opts, err := parseReplaceArgs(args, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator replace:", err)
		return exitUsage
	}
	if opts.help {
		return exitOK
	}
	if err := validateReplaceOptions(opts); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator replace:", err)
		return exitUsage
	}
	operations := make([]replace.Operation, 0, len(opts.sets)+len(opts.adds)+len(opts.deletes))
	for _, item := range opts.sets {
		operation, err := parseReplaceOperation(replace.OpSet, item)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator replace:", err)
			return exitUsage
		}
		operations = append(operations, operation)
	}
	for _, item := range opts.adds {
		operation, err := parseReplaceOperation(replace.OpAdd, item)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator replace:", err)
			return exitUsage
		}
		operations = append(operations, operation)
	}
	for _, item := range opts.deletes {
		operation, err := parseReplaceOperation(replace.OpDelete, item)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator replace:", err)
			return exitUsage
		}
		operations = append(operations, operation)
	}
	var buffer bytes.Buffer
	if err := replace.Files(opts.input, operations, &buffer, replace.Options{
		Compression:      creator.CompressionMode(strings.ToLower(strings.TrimSpace(opts.compression))),
		CompressionLevel: opts.compressionLevel,
		Deterministic:    opts.deterministic,
		Signatures:       creator.SignatureMode(strings.ToLower(strings.TrimSpace(opts.signatures))),
		Limits: replace.Limits{
			MaxEntries:    opts.maxEntries,
			MaxEntryBytes: int64(opts.maxEntryMB) << 20,
			MaxTotalBytes: int64(opts.maxTotalMB) << 20,
		},
		Validate:     opts.validate,
		SkipXMLCheck: opts.noValidate,
		OnWarning: func(message string) {
			_, _ = fmt.Fprintln(stderr, "ofd-creator replace: 警告:", message)
		},
	}); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator replace:", err)
		return exitResource
	}
	data := buffer.Bytes()
	if strings.TrimSpace(opts.signCmd) != "" {
		var signed bytes.Buffer
		if err := sign.Sign(data, &signed, buildSignOptions(&opts.signFlags, opts.deterministic)); err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator replace:", err)
			return exitResource
		}
		data = signed.Bytes()
	}
	if opts.verifySignatures {
		statuses, verifyErr := merge.VerifySignatures(data)
		if verifyErr != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator replace:", verifyErr)
			return exitResource
		}
		reportSignatureVerification(stderr, "ofd-creator replace", statuses)
	}
	if err := writeOutput(opts.output, data, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator replace:", err)
		return exitOutput
	}
	return exitOK
}
