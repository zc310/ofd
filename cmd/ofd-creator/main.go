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
	"sync"

	"github.com/spf13/cobra"
	ofdexport "github.com/zc310/ofd/internal/export"
	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/merge"
	"github.com/zc310/ofd/pkg/sign"
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
	createOptions := creator.CreateOptions{Compression: compression, Deterministic: opts.deterministic, CompleteTextCodeDeltas: opts.completeTextCodeDeltas}
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
	if opts.output != "-" && opts.input != "-" && samePath(opts.input, opts.output) {
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
		Compression:   creator.CompressionMode(strings.ToLower(strings.TrimSpace(opts.compression))),
		Deterministic: opts.deterministic,
		Signatures:    merge.SignatureMode(strings.ToLower(strings.TrimSpace(opts.signatures))),
		Orphans:       merge.OrphanMode(strings.ToLower(strings.TrimSpace(opts.orphans))),
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
			Compression:   mergeOptions.Compression,
			Deterministic: mergeOptions.Deterministic,
			Selectors:     pages,
			ID:            opts.documentID,
			Title:         opts.title,
			Author:        opts.author,
			Concurrency:   opts.workers,
			OnSignature:   collector.add,
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
		if err := sign.Sign(raw.Bytes(), &signed, sign.Options{Command: opts.signCmd, ID: opts.signID}); err != nil {
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
			reportSignatureVerification(stderr, statuses)
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
			reportSignatureVerification(stderr, statuses)
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
		reportSignatureVerification(stderr, statuses)
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

type signatureCollector struct {
	mu     sync.Mutex
	events []merge.SignatureEvent
}

func (c *signatureCollector) add(event merge.SignatureEvent) {
	c.mu.Lock()
	c.events = append(c.events, event)
	c.mu.Unlock()
}

func (c *signatureCollector) snapshot() []merge.SignatureEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]merge.SignatureEvent(nil), c.events...)
}

func reportSignatureEvents(w io.Writer, events []merge.SignatureEvent) {
	if len(events) == 0 {
		return
	}
	var preserved, rewritten, dropped int
	for _, event := range events {
		switch event.Action {
		case merge.SignaturePreserved:
			preserved++
		case merge.SignatureRewritten:
			rewritten++
		case merge.SignatureDropped:
			dropped++
		}
		_, _ = fmt.Fprintf(w, "ofd-creator merge: 签名 %s#%d %s -> %s\n", event.Input, event.DocumentIndex, event.ID, event.Action)
	}
	_, _ = fmt.Fprintf(w, "ofd-creator merge: 签名汇总：保留 %d，重写 %d，丢弃 %d\n", preserved, rewritten, dropped)
}

func reportSignatureVerification(w io.Writer, statuses []merge.SignatureStatus) {
	if len(statuses) == 0 {
		_, _ = fmt.Fprintln(w, "ofd-creator merge: 输出文档没有签名")
		return
	}
	for _, status := range statuses {
		digest := "摘要有效"
		if !status.DigestValid {
			digest = "摘要无效"
		}
		signature := "密码学签名未验证"
		switch {
		case status.Verified:
			signature = "密码学签名有效"
		case status.VerificationError != "":
			signature = "密码学签名校验失败"
		}
		_, _ = fmt.Fprintf(w, "ofd-creator merge: 签名 %s：%s，%s\n", status.ID, digest, signature)
	}
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
	signatures       string
	orphans          string
	pages            string
	documentID       string
	title            string
	author           string
	signCmd          string
	signID           string
	workers          int
	maxEntries       int
	maxEntryMB       int
	maxTotalMB       int
	deterministic    bool
	verifySignatures bool
	validate         bool
	help             bool
}

func parseMergeArgs(args []string, output io.Writer) (*mergeOptions, error) {
	opts := &mergeOptions{compression: string(creator.CompressionAuto), signatures: string(merge.SignaturePreserve), orphans: string(merge.OrphanError)}
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
	flags.StringVar(&opts.signCmd, "sign-cmd", "", "合并后调用外部命令为输出追加签名；命令从 stdin 读 Signature.xml，向 stdout 写 SignedValue.dat")
	flags.StringVar(&opts.signID, "sign-id", "sign-1", "外部签名的签名标识，需为合法 xs:ID")
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
			if samePath(input, opts.output) {
				return errors.New("输出文件不能覆盖输入 OFD 文件")
			}
		}
	}
	switch creator.CompressionMode(strings.ToLower(strings.TrimSpace(opts.compression))) {
	case creator.CompressionAuto, creator.CompressionDeflate, creator.CompressionStore:
	default:
		return fmt.Errorf("不支持的 ZIP 压缩策略 %q", opts.compression)
	}
	switch merge.SignatureMode(strings.ToLower(strings.TrimSpace(opts.signatures))) {
	case "", merge.SignaturePreserve, merge.SignatureRewrite, merge.SignatureDrop:
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
		signatures := merge.SignatureMode(strings.ToLower(strings.TrimSpace(opts.signatures)))
		if signatures != "" && signatures != merge.SignaturePreserve {
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
	if opts.output != "-" && opts.input != "-" && samePath(opts.input, opts.output) {
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

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}
