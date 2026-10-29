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
	"github.com/zc310/ofd/internal/fileutil"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/split"
	"github.com/zc310/ofd/pkg/validator"
)

type splitOptions struct {
	input, outputDir, prefix, ranges, compression, signatures                 string
	every, document, compressionLevel, maxParts, maxPages, maxEntries         int
	maxInputMB, maxEntryMB, maxTotalMB, maxOutputMB                           int64
	byDocument, keepAttachments, dropIntegrity, deterministic, validate, help bool
	selection                                                                 split.Selection
	library                                                                   split.Options
}

var errSplitUsage = errors.New("拆分参数无效")

func splitUsage(message string) error { return fmt.Errorf("%w: %s", errSplitUsage, message) }

func parseSplitArgs(args []string, stdout io.Writer) (*splitOptions, error) {
	opts := &splitOptions{}
	command := &cobra.Command{Use: "ofd-creator split input.ofd --every 10 --output-dir parts", Short: "按页范围、每 N 页或完整文档体拆分 OFD", SilenceErrors: true, SilenceUsage: true,
		Args: cobra.MaximumNArgs(1), RunE: func(_ *cobra.Command, positional []string) error {
			if len(positional) == 1 {
				if opts.input != "" {
					return splitUsage("输入不能同时使用 -i 和位置参数")
				}
				opts.input = positional[0]
			}
			return nil
		}}
	command.SetArgs(args)
	command.SetOut(stdout)
	command.SetErr(stdout)
	command.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		opts.help = true
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), cmd.Use)
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "未指定分组方式时每页一个文件；输出文件已存在时拒绝覆盖。")
		cmd.Flags().SetOutput(cmd.OutOrStdout())
		cmd.Flags().PrintDefaults()
	})
	flags := command.Flags()
	flags.StringVarP(&opts.input, "input", "i", "", "OFD 输入路径")
	flags.StringVar(&opts.outputDir, "output-dir", "", "分卷输出目录（必填）")
	flags.StringVar(&opts.prefix, "prefix", "", "文件名前缀，默认使用输入文件名；输出为 prefix-001.ofd")
	flags.IntVar(&opts.every, "every", 0, "每 N 页一份，与 --ranges/--by-document 互斥")
	flags.StringVar(&opts.ranges, "ranges", "", "分组页码，例如 1-3;4-8;10，页码从 1 开始")
	flags.BoolVar(&opts.byDocument, "by-document", false, "每个 DocBody 一份，保留原文档和包内路径")
	flags.IntVar(&opts.document, "document", 0, "页面拆分使用的文档体，从 1 开始；多文档输入必填")
	flags.BoolVar(&opts.keepAttachments, "keep-attachments", false, "页面拆分时保留整份文档附件，默认移除")
	flags.StringVar(&opts.signatures, "signatures", "", "preserve 或 drop；默认文档体拆分 preserve，页面拆分 drop")
	flags.BoolVar(&opts.dropIntegrity, "drop-integrity", false, "允许移除原完整性清单，输出需要另行保护")
	flags.StringVar(&opts.compression, "compression", "auto", "ZIP 压缩策略：auto、store、deflate")
	flags.IntVar(&opts.compressionLevel, "compression-level", 0, "DEFLATE 压缩级别：0 使用默认值，或 1-9")
	flags.BoolVar(&opts.deterministic, "deterministic", false, "固定 ZIP 时间并按来源与页序生成确定性 DocID")
	flags.BoolVar(&opts.validate, "validate", false, "每份结果通过严格校验后再发布文件")
	flags.IntVar(&opts.maxParts, "max-parts", 10000, "最大分卷数")
	flags.IntVar(&opts.maxPages, "max-pages", 100000, "每个输入文档体和输出分卷的最大页数")
	flags.IntVar(&opts.maxEntries, "max-entries", 10000, "输入和每份输出的最大 ZIP 条目数")
	flags.Int64Var(&opts.maxInputMB, "max-input-mb", 512, "压缩输入上限，MiB")
	flags.Int64Var(&opts.maxEntryMB, "max-entry-mb", 64, "单个条目解压大小上限，MiB")
	flags.Int64Var(&opts.maxTotalMB, "max-total-mb", 512, "输入及每份输出的解压总大小上限，MiB")
	flags.Int64Var(&opts.maxOutputMB, "max-output-mb", 2048, "累计输出压缩字节上限，MiB")
	if err := command.Execute(); err != nil {
		return nil, err
	}
	if opts.help {
		return opts, nil
	}
	if opts.input == "" || opts.input == "-" || opts.outputDir == "" || opts.outputDir == "-" {
		return nil, splitUsage("需要输入文件和 --output-dir")
	}
	if opts.every < 0 || opts.document < 0 || opts.maxParts < 1 || opts.maxPages < 1 || opts.maxEntries < 1 {
		return nil, splitUsage("页数、文档体和分卷上限无效")
	}
	for _, value := range []int64{opts.maxInputMB, opts.maxEntryMB, opts.maxTotalMB, opts.maxOutputMB} {
		if value < 1 || value > (1<<63-1)>>20 {
			return nil, splitUsage("MiB 大小限制必须为正数且不能溢出")
		}
	}
	modeCount := 0
	if flags.Changed("every") {
		if opts.every < 1 {
			return nil, splitUsage("--every 必须为正数")
		}
		modeCount++
	}
	if flags.Changed("ranges") {
		if strings.TrimSpace(opts.ranges) == "" {
			return nil, splitUsage("--ranges 不能为空")
		}
		modeCount++
	}
	if opts.byDocument {
		modeCount++
	}
	if modeCount > 1 {
		return nil, splitUsage("--every、--ranges、--by-document 互斥")
	}
	if modeCount == 0 {
		opts.every = 1
	}
	if opts.byDocument && opts.document != 0 {
		return nil, splitUsage("--by-document 不能同时指定 --document")
	}
	if opts.prefix == "" {
		opts.prefix = strings.TrimSuffix(filepath.Base(opts.input), filepath.Ext(opts.input))
	}
	if opts.prefix == "" || opts.prefix == "." || opts.prefix == ".." || strings.ContainsAny(opts.prefix, "/\\\x00") {
		return nil, splitUsage("文件名前缀无效")
	}
	compression, err := creator.NormalizeCompression(creator.CompressionMode(strings.ToLower(strings.TrimSpace(opts.compression))))
	if err != nil {
		return nil, err
	}
	if _, err := creator.NormalizeCompressionLevel(opts.compressionLevel); err != nil {
		return nil, err
	}
	mode := creator.SignatureMode(strings.ToLower(strings.TrimSpace(opts.signatures)))
	if mode != "" && mode != creator.SignaturePreserve && mode != creator.SignatureDrop {
		return nil, splitUsage("--signatures 仅支持 preserve 或 drop")
	}
	opts.selection = split.Selection{Document: opts.document, Every: opts.every, ByDocument: opts.byDocument}
	if opts.ranges != "" {
		groups, err := split.ParseGroups(opts.ranges, opts.maxPages, opts.maxParts)
		if err != nil {
			return nil, err
		}
		opts.selection.Groups = groups
	}
	opts.library = split.Options{Compression: compression, CompressionLevel: opts.compressionLevel, Deterministic: opts.deterministic, KeepAttachments: opts.keepAttachments, Signatures: mode, DropIntegrity: opts.dropIntegrity,
		Limits: split.Limits{MaxInputBytes: opts.maxInputMB << 20, MaxEntryBytes: opts.maxEntryMB << 20, MaxTotalBytes: opts.maxTotalMB << 20, MaxOutputBytes: opts.maxOutputMB << 20, MaxParts: opts.maxParts, MaxPages: opts.maxPages, MaxEntries: opts.maxEntries}}
	return opts, nil
}

func runSplit(args []string, stdout, stderr io.Writer) int {
	opts, err := parseSplitArgs(args, stdout)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator split:", err)
		return exitUsage
	}
	if opts.help {
		return exitOK
	}
	ctx := context.Background()
	session, err := split.Open(ctx, opts.input, opts.library)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator split:", err)
		return exitResource
	}
	defer func() { _ = session.Close() }()
	parts, err := session.Plan(opts.selection)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator split:", err)
		return exitUsage
	}
	paths := make([]string, len(parts))
	for index := range parts {
		name := filepath.Join(opts.outputDir, fmt.Sprintf("%s-%03d.ofd", opts.prefix, index+1))
		if fileutil.SamePath(opts.input, name) {
			_, _ = fmt.Fprintln(stderr, "ofd-creator split: 输出不能覆盖输入")
			return exitUsage
		}
		if _, err := os.Lstat(name); err == nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator split: 输出已存在:", name)
			return exitOutput
		} else if !errors.Is(err, os.ErrNotExist) {
			_, _ = fmt.Fprintln(stderr, "ofd-creator split:", err)
			return exitOutput
		}
		paths[index] = name
	}
	if err := os.MkdirAll(opts.outputDir, 0o755); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator split:", err)
		return exitOutput
	}
	for index, part := range parts {
		report, code, err := writeSplitPart(ctx, session, part, paths[index], opts.validate, opts.library.Limits, stderr)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "ofd-creator split: 已完成 %d/%d 份；第 %d 份失败: %v\n", index, len(parts), index+1, err)
			return code
		}
		for _, dropped := range report.Dropped {
			_, _ = fmt.Fprintf(stderr, "ofd-creator split: %s 已移除 %s\n", filepath.Base(paths[index]), dropped)
		}
		for _, signature := range report.DroppedSignatures {
			_, _ = fmt.Fprintf(stderr, "ofd-creator split: %s 已移除签名 %s\n", filepath.Base(paths[index]), signature)
		}
		if _, err := fmt.Fprintln(stdout, paths[index]); err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator split: 写入结果列表失败:", err)
			return exitOutput
		}
	}
	return exitOK
}

func writeSplitPart(ctx context.Context, session *split.Session, part split.Part, target string, validate bool, limits split.Limits, stderr io.Writer) (split.Report, int, error) {
	temporary, err := os.CreateTemp(filepath.Dir(target), ".ofd-creator-split-*")
	if err != nil {
		return split.Report{}, exitOutput, err
	}
	defer func() { _ = temporary.Close(); _ = os.Remove(temporary.Name()) }()
	tracked := &splitWriteTracker{writer: temporary}
	report, err := session.Write(ctx, part, tracked)
	if err != nil {
		if tracked.err != nil {
			return split.Report{}, exitOutput, err
		}
		return split.Report{}, exitResource, err
	}
	if err := temporary.Close(); err != nil {
		return split.Report{}, exitOutput, err
	}
	if validate {
		instance, err := validator.New(validator.WithGMT0099(true),
			validator.WithMaxEntries(limits.MaxEntries), validator.WithMaxFileSize(limits.MaxEntryBytes),
			validator.WithMaxTotalSize(limits.MaxTotalBytes), validator.WithMaxInputSize(limits.MaxOutputBytes))
		if err != nil {
			return split.Report{}, exitValidate, err
		}
		result := instance.ValidatePath(ctx, temporary.Name())
		if result.HasErrors() {
			_ = validator.RenderText(stderr, result)
			return split.Report{}, exitValidate, errOFDInvalid
		}
	}
	// 同目录硬链接以不可覆盖的方式发布完整文件，避免检查后出现同名文件时被覆盖。
	if err := os.Link(temporary.Name(), target); err != nil {
		return split.Report{}, exitOutput, err
	}
	return report, exitOK, nil
}

type splitWriteTracker struct {
	writer io.Writer
	err    error
}

func (w *splitWriteTracker) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = err
	}
	return n, err
}
