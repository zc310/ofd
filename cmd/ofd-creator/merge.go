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
	"github.com/zc310/ofd/internal/utils"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/merge"
	"github.com/zc310/ofd/pkg/sign"
	"github.com/zc310/ofd/pkg/validator"
)

func runMerge(args []string, stdout, stderr io.Writer) int {
	opts, err := parseMergeArgs(args, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator merge:", err)
		return exitUsage
	}
	if opts.help {
		return exitOK
	}
	if err := validateMergeOptions(opts); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator merge:", err)
		return exitUsage
	}
	collector := &signatureCollector{}
	mergeOptions := merge.Options{
		Compression:      creator.CompressionMode(strings.ToLower(strings.TrimSpace(opts.compression))),
		CompressionLevel: opts.compressionLevel,
		Deterministic:    opts.deterministic,
		Signatures:       creator.SignatureMode(strings.ToLower(strings.TrimSpace(opts.signatures))),
		Orphans:          merge.OrphanMode(strings.ToLower(strings.TrimSpace(opts.orphans))),
		Limits: merge.Limits{
			MaxEntries:    opts.maxEntries,
			MaxEntryBytes: int64(opts.maxEntryMB) << 20,
			MaxTotalBytes: int64(opts.maxTotalMB) << 20,
		},
		OnWarning: func(message string) {
			_, _ = fmt.Fprintln(stderr, "ofd-creator merge: 警告:", message)
		},
		OnSignature: collector.add,
	}
	run := func(w io.Writer) error {
		if strings.TrimSpace(opts.pages) == "" {
			return merge.Files(opts.inputs, w, mergeOptions)
		}
		pages, err := merge.ParsePageSelection(opts.pages)
		if err != nil {
			return err
		}
		sources := make([]merge.Source, 0, len(opts.inputs))
		for _, input := range opts.inputs {
			sources = append(sources, merge.Source{Path: input})
		}
		return merge.Pages(sources, w, merge.PageOptions{
			Compression:      mergeOptions.Compression,
			CompressionLevel: mergeOptions.CompressionLevel,
			Deterministic:    mergeOptions.Deterministic,
			Selectors:        pages,
			ID:               opts.documentID,
			Title:            opts.title,
			Author:           opts.author,
			Concurrency:      opts.workers,
			OnSignature:      collector.add,
		})
	}
	if strings.TrimSpace(opts.signCmd) != "" {
		var raw bytes.Buffer
		if err := run(&raw); err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator merge:", err)
			return exitResource
		}
		reportSignatureEvents(stderr, collector.snapshot())
		var signed bytes.Buffer
		if err := sign.Sign(raw.Bytes(), &signed, buildSignOptions(&opts.signFlags, opts.deterministic)); err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator merge:", err)
			return exitResource
		}
		data := signed.Bytes()
		if opts.verifySignatures {
			statuses, verifyErr := merge.VerifySignatures(data)
			if verifyErr != nil {
				_, _ = fmt.Fprintln(stderr, "ofd-creator merge:", verifyErr)
				return exitResource
			}
			reportSignatureVerification(stderr, "ofd-creator merge", statuses)
		}
		if opts.validate {
			if err := validateMerged(bytes.NewReader(data), "merged.ofd", stderr); err != nil {
				return exitValidate
			}
		}
		if err := writeOutput(opts.output, data, stdout); err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator merge:", err)
			return exitOutput
		}
		return exitOK
	}
	if opts.output == "-" {
		var buffer bytes.Buffer
		if err := run(&buffer); err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator merge:", err)
			return exitResource
		}
		reportSignatureEvents(stderr, collector.snapshot())
		if opts.verifySignatures {
			statuses, verifyErr := merge.VerifySignatures(buffer.Bytes())
			if verifyErr != nil {
				_, _ = fmt.Fprintln(stderr, "ofd-creator merge:", verifyErr)
				return exitResource
			}
			reportSignatureVerification(stderr, "ofd-creator merge", statuses)
		}
		if opts.validate {
			if err := validateMerged(bytes.NewReader(buffer.Bytes()), "merged.ofd", stderr); err != nil {
				return exitValidate
			}
		}
		if _, err := stdout.Write(buffer.Bytes()); err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator merge:", err)
			return exitOutput
		}
		return exitOK
	}
	dir := filepath.Dir(opts.output)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator merge:", err)
		return exitOutput
	}
	temporary, err := os.CreateTemp(dir, ".ofd-creator-merge-*")
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator merge:", err)
		return exitOutput
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()
	if err := run(temporary); err != nil {
		_ = temporary.Close()
		_, _ = fmt.Fprintln(stderr, "ofd-creator merge:", err)
		return exitResource
	}
	if err := temporary.Close(); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator merge:", err)
		return exitOutput
	}
	reportSignatureEvents(stderr, collector.snapshot())
	if opts.verifySignatures {
		statuses, verifyErr := merge.VerifySignatures(temporaryName)
		if verifyErr != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator merge:", verifyErr)
			return exitResource
		}
		reportSignatureVerification(stderr, "ofd-creator merge", statuses)
	}
	if opts.validate {
		file, openErr := os.Open(temporaryName)
		if openErr != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator merge:", openErr)
			return exitOutput
		}
		validateErr := validateMerged(file, "merged.ofd", stderr)
		_ = file.Close()
		if validateErr != nil {
			return exitValidate
		}
	}
	if err := os.Rename(temporaryName, opts.output); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator merge:", err)
		return exitOutput
	}
	return exitOK
}

func validateMerged(reader io.Reader, name string, stderr io.Writer) error {
	instance, err := validator.New()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator merge:", err)
		return err
	}
	report := instance.ValidateReader(context.Background(), reader, name)
	if report.HasErrors() {
		_ = validator.RenderText(stderr, report)
		return errOFDInvalid
	}
	return nil
}

type mergeOptions struct {
	inputs           []string
	output           string
	compression      string
	compressionLevel int
	signatures       string
	orphans          string
	pages            string
	documentID       string
	title            string
	author           string
	workers          int
	maxEntries       int
	maxEntryMB       int
	maxTotalMB       int
	deterministic    bool
	verifySignatures bool
	validate         bool
	help             bool
	signFlags
}

func parseMergeArgs(args []string, output io.Writer) (*mergeOptions, error) {
	opts := &mergeOptions{compression: string(creator.CompressionAuto), signatures: string(creator.SignaturePreserve), orphans: string(merge.OrphanError)}
	root := &cobra.Command{
		Use:           "ofd-creator merge",
		Short:         "合并多个 OFD 为多文档 OFD",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, positional []string) error {
			if opts.help {
				return nil
			}
			opts.inputs = append(opts.inputs, positional...)
			return nil
		},
	}
	root.SetArgs(args)
	root.SetOut(output)
	root.SetErr(output)
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		opts.help = true
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "ofd-creator merge - 合并多个 OFD 为多文档 OFD")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "用法：ofd-creator merge -o merged.ofd input1.ofd input2.ofd")
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		flags := cmd.Flags()
		flags.SetOutput(cmd.OutOrStdout())
		flags.PrintDefaults()
	})
	flags := root.Flags()
	flags.StringArrayVarP(&opts.inputs, "input", "i", nil, "OFD 输入文件，可重复指定；也可作为位置参数")
	flags.StringVarP(&opts.output, "output", "o", "", "合并后的 OFD 输出路径；使用 - 写入标准输出")
	flags.StringVar(&opts.compression, "compression", opts.compression, "ZIP 压缩策略：auto、deflate 或 store")
	flags.IntVar(&opts.compressionLevel, "compression-level", 0, "DEFLATE 压缩级别：0 使用默认级别 5，1（最快）到 9（最紧凑）")
	flags.StringVar(&opts.signatures, "signatures", opts.signatures, "签名处理方式：preserve、rewrite 或 drop")
	flags.StringVar(&opts.orphans, "orphans", opts.orphans, "文档目录外条目处理方式：error、ignore 或 preserve")
	flags.StringVar(&opts.pages, "pages", "", "选页并重排：全局 1,3-5 或按源 s1:1,3-5;s2:2；设置后使用模型级合并")
	flags.StringVar(&opts.documentID, "document-id", "", "模型级合并输出文档 ID，默认沿用首源")
	flags.StringVar(&opts.title, "title", "", "模型级合并输出文档标题，默认沿用首源")
	flags.StringVar(&opts.author, "author", "", "模型级合并输出文档作者，默认沿用首源")
	flags.IntVar(&opts.workers, "workers", defaultMergeWorkers, "模型级合并并行解析输入的并发数，默认 4")
	flags.IntVar(&opts.maxEntries, "max-entries", 0, "最多搬运的条目数，0 表示使用默认值 10000")
	flags.IntVar(&opts.maxEntryMB, "max-entry-mb", 0, "单个条目解压后的最大 MB，0 表示使用默认值 64")
	flags.IntVar(&opts.maxTotalMB, "max-total-mb", 0, "所有条目解压后的总 MB 上限，0 表示使用默认值 512")
	flags.BoolVar(&opts.deterministic, "deterministic", false, "使用固定 ZIP 时间，生成可复现的 OFD")
	flags.BoolVar(&opts.validate, "validate", false, "合并后执行严格 OFD 校验")
	flags.BoolVar(&opts.verifySignatures, "verify-signatures", false, "合并后校验输出文档的签名摘要与密码学签名")
	registerSignFlags(flags, &opts.signFlags)
	if err := root.Execute(); err != nil {
		return nil, err
	}
	if opts.help {
		return opts, nil
	}
	return opts, nil
}

func validateMergeOptions(opts *mergeOptions) error {
	if len(opts.inputs) == 0 {
		return errors.New("至少需要一个 OFD 输入文件")
	}
	if strings.TrimSpace(opts.output) == "" {
		return errors.New("缺少合并后的 OFD 输出文件")
	}
	for _, input := range opts.inputs {
		if input == "-" {
			return errors.New("merge 不支持从标准输入读取")
		}
	}
	if opts.output != "-" {
		for _, input := range opts.inputs {
			if utils.SamePath(input, opts.output) {
				return errors.New("输出文件不能覆盖输入 OFD 文件")
			}
		}
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
	case "", creator.SignaturePreserve, creator.SignatureRewrite, creator.SignatureDrop:
	default:
		return fmt.Errorf("不支持的签名处理方式 %q", opts.signatures)
	}
	switch merge.OrphanMode(strings.ToLower(strings.TrimSpace(opts.orphans))) {
	case "", merge.OrphanError, merge.OrphanIgnore, merge.OrphanPreserve:
	default:
		return fmt.Errorf("不支持的目录外条目处理方式 %q", opts.orphans)
	}
	if opts.maxEntries < 0 || opts.maxEntryMB < 0 || opts.maxTotalMB < 0 {
		return errors.New("合并规模限制不能为负数")
	}
	if strings.TrimSpace(opts.pages) != "" {
		if _, err := merge.ParsePageSelection(opts.pages); err != nil {
			return err
		}
		signatures := creator.SignatureMode(strings.ToLower(strings.TrimSpace(opts.signatures)))
		if signatures != "" && signatures != creator.SignaturePreserve {
			return errors.New("--pages 使用模型级合并，不支持 --signatures")
		}
		orphans := merge.OrphanMode(strings.ToLower(strings.TrimSpace(opts.orphans)))
		if orphans != "" && orphans != merge.OrphanError {
			return errors.New("--pages 使用模型级合并，不支持 --orphans")
		}
		if opts.maxEntries != 0 || opts.maxEntryMB != 0 || opts.maxTotalMB != 0 {
			return errors.New("--pages 使用模型级合并，不支持 --max-entries/--max-entry-mb/--max-total-mb")
		}
		if opts.workers < 1 {
			return errors.New("--workers 必须大于 0")
		}
	} else if opts.documentID != "" || opts.title != "" || opts.author != "" || opts.workers != defaultMergeWorkers {
		return errors.New("--document-id/--title/--author/--workers 仅在设置 --pages 时可用")
	}
	return nil
}
