// Command ofd-invoice 从 OFD 电子发票中抽取结构化发票信息，汇总多张发票的金额，
// 并支持把多张发票合并为一个便于打印的 PDF。
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-json"
	"github.com/spf13/cobra"
	"github.com/zc310/ofd/internal/utils"
	"github.com/zc310/ofd/internal/version"
	"github.com/zc310/ofd/pkg/invoice"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// 输出格式。
const (
	formatJSON = "json"
	formatCSV  = "csv"
	formatXLSX = "xlsx"
)

// invoiceExtension 是批处理时在目录里识别的发票文件扩展名。
var invoiceExtensions = map[string]bool{".ofd": true, ".zip": true}

type options struct {
	inputs      []string
	inputDir    string
	output      string
	format      string
	pretty      bool
	skipBroken  bool
	dedup       bool
	sortKey     string
	mergePDF    string
	cover       bool
	coverTitle  string
	pages       string
	paper       string
	orientation string
	nup         int
	nupGap      float64
	cellBorder  bool
	fit         string
	help        bool
	version     bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseArgs(args, stdout, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-invoice:", err)
		return exitUsage
	}
	if opts.help {
		return exitOK
	}
	if opts.version {
		_, _ = fmt.Fprintln(stdout, version.Version)
		return exitOK
	}
	sources, err := collectSources(opts.inputs)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-invoice:", err)
		return exitUsage
	}
	batchOptions := invoice.BatchOptions{SkipBroken: opts.skipBroken, Dedup: opts.dedup}
	var payload any
	var batch *invoice.Batch
	switch {
	case opts.mergePDF != "":
		batch, err = mergePDF(opts, sources, batchOptions)
		payload = batch
	case singleJSON(opts, sources):
		// 单张发票保持历史的单张 JSON 形态。
		payload, err = invoice.Extract(sources[0].Input)
	default:
		batch, err = extractBatch(opts, sources, batchOptions)
		payload = batch
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-invoice:", err)
		return exitError
	}
	if err := writeResult(opts, payload, batch, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-invoice:", err)
		return exitError
	}
	return exitOK
}

// singleJSON 判断是否按历史形态输出单张发票 JSON。
//
// 单张、只要 JSON、且没有改变处理口径（跳过失败或去重）时才走这条路径：已有脚本
// 按单个 Invoice 对象取值，多一张票或改了口径就应该拿到含 items 与 summary 的
// 批量形态。
func singleJSON(opts *options, sources []invoice.Source) bool {
	return len(sources) == 1 && opts.format == formatJSON && !opts.skipBroken && !opts.dedup
}

// extractBatch 批量抽取并按指定顺序重排结果。
func extractBatch(opts *options, sources []invoice.Source, batchOptions invoice.BatchOptions) (*invoice.Batch, error) {
	batch, err := invoice.ExtractBatch(sources, batchOptions)
	if err != nil {
		return nil, err
	}
	if err := invoice.SortItems(batch.Items, opts.sortKey); err != nil {
		return nil, err
	}
	return batch, nil
}

// mergePDF 合并输出 PDF，同时保留批量结果供报表输出。
func mergePDF(opts *options, sources []invoice.Source, batchOptions invoice.BatchOptions) (*invoice.Batch, error) {
	file, err := createOutput(opts.mergePDF)
	if err != nil {
		return nil, err
	}
	batch, err := invoice.MergePDF(context.Background(), sources, file, invoice.MergeOptions{
		BatchOptions: batchOptions,
		Sort:         opts.sortKey,
		Cover:        opts.cover,
		CoverTitle:   opts.coverTitle,
		Pages:        opts.pages,
		Paper:        opts.paper,
		Orientation:  opts.orientation,
		NUp:          opts.nup,
		NUpGap:       opts.nupGap,
		CellBorder:   opts.cellBorder,
		Fit:          opts.fit,
	})
	if err != nil {
		_ = file.Close()
		removeOutput(opts.mergePDF)
		return nil, err
	}
	if err := file.Close(); err != nil {
		removeOutput(opts.mergePDF)
		return nil, fmt.Errorf("关闭合并 PDF 失败：%w", err)
	}
	// 汇总页是按同一批次排序后生成的，报表也要用同一个次序。
	if err := invoice.SortItems(batch.Items, opts.sortKey); err != nil {
		return nil, err
	}
	return batch, nil
}

func parseArgs(args []string, stdout, stderr io.Writer) (*options, error) {
	opts := &options{}
	root := &cobra.Command{
		Use:           "ofd-invoice [选项] input.ofd [input2.ofd ...]",
		Short:         "OFD 电子发票信息抽取与汇总工具",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, positional []string) error {
			if opts.help || opts.version {
				return nil
			}
			opts.inputs = positional
			if len(positional) == 0 && opts.inputDir == "" {
				return errors.New("缺少输入 OFD 文件")
			}
			return nil
		},
	}
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		opts.help = true
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "ofd-invoice - OFD 电子发票信息抽取与汇总工具")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "用法：ofd-invoice [选项] input.ofd [input2.ofd ...]")
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		flags := cmd.Flags()
		flags.SetOutput(cmd.OutOrStdout())
		flags.PrintDefaults()
	})
	flags := root.Flags()
	flags.StringVar(&opts.inputDir, "input-dir", "", "批量处理目录下全部 OFD 文件")
	flags.StringVarP(&opts.output, "output", "o", "", "输出路径；- 或省略时输出到标准输出")
	flags.StringVar(&opts.format, "format", "", "输出格式 json、csv 或 xlsx；省略时按输出扩展名推断")
	flags.BoolVar(&opts.pretty, "pretty", false, "缩进 JSON 输出")
	flags.BoolVar(&opts.skipBroken, "skip-broken", false, "跳过抽取失败的输入继续处理（默认整批报错）")
	flags.BoolVar(&opts.dedup, "dedup", false, "按发票代码与号码去重，重复票不计入合计")
	flags.StringVar(&opts.sortKey, "sort", invoice.SortInput, "页序与报表排序 input、date、code 或 name")
	flags.StringVar(&opts.mergePDF, "merge-pdf", "", "把全部发票合并为一个 PDF 并写入该路径")
	flags.BoolVar(&opts.cover, "cover", false, "合并 PDF 时在首页插入汇总对账页（需 --merge-pdf）")
	flags.StringVar(&opts.coverTitle, "cover-title", "", "汇总对账页标题")
	flags.StringVar(&opts.pages, "pages", "", "每张票输出的页面：all（默认）、first（只取首页）、face（只取票面页）或页码表达式")
	flags.StringVar(&opts.paper, "paper", "", "输出纸张 A4/A3/A5/Letter/Legal/B5/16开；省略时按页面原尺寸")
	flags.StringVar(&opts.orientation, "orientation", "", "纸张方向 auto（默认）、portrait 或 landscape")
	flags.IntVar(&opts.nup, "nup", 1, "一张纸放几页发票：1、2、3、4、6、8、9")
	flags.Float64Var(&opts.nupGap, "nup-gap", 0, "拼版格子之间的间距（毫米）")
	flags.BoolVar(&opts.cellBorder, "cell-border", false, "给每个拼版格子描一圈裁切线")
	flags.StringVar(&opts.fit, "fit", "", "页面适配方式 contain（默认，等比缩放居中补白）")
	flags.BoolVar(&opts.version, "version", false, "输出工具版本")
	if err := root.Execute(); err != nil {
		return nil, err
	}
	if opts.help || opts.version {
		return opts, nil
	}
	if err := validate(opts); err != nil {
		return nil, err
	}
	inputs, err := expandInputs(opts)
	if err != nil {
		return nil, err
	}
	opts.inputs = inputs
	return opts, nil
}

// validate 校验选项组合，不合规的组合按参数错误处理。
func validate(opts *options) error {
	if opts.format != "" {
		switch opts.format {
		case formatJSON, formatCSV, formatXLSX:
		default:
			return fmt.Errorf("不支持的输出格式 %q，可用 json、csv 或 xlsx", opts.format)
		}
	}
	if opts.cover && opts.mergePDF == "" {
		return errors.New("--cover 需要与 --merge-pdf 一起使用")
	}
	for _, flag := range []struct {
		name  string
		value string
	}{
		{"pages", opts.pages},
		{"paper", opts.paper},
		{"orientation", opts.orientation},
		{"fit", opts.fit},
		{"nup-gap", formatFloat(opts.nupGap)},
	} {
		if opts.mergePDF == "" && flag.value != "" && flag.value != "0" {
			return fmt.Errorf("--%s 需要与 --merge-pdf 一起使用", flag.name)
		}
	}
	if opts.mergePDF == "" && (opts.nup != 1 || opts.cellBorder) {
		return errors.New("--nup 与 --cell-border 需要与 --merge-pdf 一起使用")
	}
	if opts.pretty {
		switch opts.format {
		case formatCSV, formatXLSX:
			return errors.New("--pretty 只适用于 JSON 输出")
		}
	}
	switch opts.sortKey {
	case invoice.SortInput, invoice.SortDate, invoice.SortCode, invoice.SortName:
	default:
		return fmt.Errorf("不支持的排序方式 %q，可用 input、date、code 或 name", opts.sortKey)
	}
	if opts.inputDir != "" {
		info, err := os.Stat(opts.inputDir)
		if err != nil {
			return fmt.Errorf("输入目录：%w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("输入目录不是目录：%s", opts.inputDir)
		}
	}
	for _, path := range opts.inputs {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("输入文件：%w", err)
		}
	}
	format, err := resolveFormat(opts)
	if err != nil {
		return err
	}
	opts.format = format
	// 输出不能覆盖任何一张输入发票，也不允许覆盖合并 PDF 自身。
	if opts.output != "" && opts.output != "-" {
		if samePathAny(opts.output, opts.inputs) {
			return errors.New("输出不能覆盖输入 OFD 文件")
		}
		if opts.mergePDF != "" && utils.SamePath(opts.output, opts.mergePDF) {
			return errors.New("输出不能覆盖合并 PDF 文件")
		}
		if opts.mergePDF != "" && samePathAny(opts.mergePDF, opts.inputs) {
			return errors.New("合并 PDF 不能覆盖输入 OFD 文件")
		}
	}
	return nil
}

// resolveFormat 确定输出格式：显式 --format 优先，否则按输出扩展名推断，
// 都没有时用 JSON。
func resolveFormat(opts *options) (string, error) {
	if opts.format != "" {
		return opts.format, nil
	}
	if opts.output == "" || opts.output == "-" {
		return formatJSON, nil
	}
	switch strings.ToLower(filepath.Ext(opts.output)) {
	case ".csv":
		return formatCSV, nil
	case ".xlsx":
		return formatXLSX, nil
	case ".json", "":
		return formatJSON, nil
	default:
		return "", fmt.Errorf("无法从输出扩展名 %q 推断格式，请用 --format 指定",
			filepath.Ext(opts.output))
	}
}

// expandInputs 汇总位置参数与 --input-dir 的文件，目录内文件按名称排序后追加
// 在位置参数之后，保证同一目录重复运行得到同一页序。
func expandInputs(opts *options) ([]string, error) {
	inputs := append([]string{}, opts.inputs...)
	if opts.inputDir == "" {
		return inputs, nil
	}
	entries, err := os.ReadDir(opts.inputDir)
	if err != nil {
		return nil, fmt.Errorf("读取输入目录失败：%w", err)
	}
	found := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !invoiceExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
			continue
		}
		found = append(found, filepath.Join(opts.inputDir, entry.Name()))
	}
	sort.Strings(found)
	if len(inputs) == 0 && len(found) == 0 {
		return nil, fmt.Errorf("目录 %s 里没有 OFD 文件", opts.inputDir)
	}
	return append(inputs, found...), nil
}

// formatFloat 规整浮点选项的字符串形式，用于判断用户是否显式给了值。
func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// collectSources 把输入路径转成批量抽取的来源列表。
func collectSources(inputs []string) ([]invoice.Source, error) {
	if len(inputs) == 0 {
		return nil, errors.New("缺少输入 OFD 文件")
	}
	sources := make([]invoice.Source, 0, len(inputs))
	for _, path := range inputs {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("输入文件：%w", err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("输入是目录：%s（批量处理请用 --input-dir）", path)
		}
		sources = append(sources, invoice.Source{Name: path, Input: path})
	}
	return sources, nil
}

func samePathAny(target string, inputs []string) bool {
	for _, path := range inputs {
		if utils.SamePath(target, path) {
			return true
		}
	}
	return false
}

// writeResult 按格式写出抽取或汇总结果。payload 是实际写出的 JSON 内容，
// batch 只在 CSV/XLSX 报表下使用。
func writeResult(opts *options, payload any, batch *invoice.Batch, stdout io.Writer) error {
	var (
		data []byte
		err  error
	)
	switch opts.format {
	case formatCSV:
		if batch == nil {
			return errors.New("CSV 报表需要批量抽取结果")
		}
		var buffer bytes.Buffer
		if err = invoice.WriteCSV(&buffer, batch); err == nil {
			data = buffer.Bytes()
		}
	case formatXLSX:
		if batch == nil {
			return errors.New("XLSX 报表需要批量抽取结果")
		}
		var buffer bytes.Buffer
		if err = invoice.WriteXLSX(&buffer, batch); err == nil {
			data = buffer.Bytes()
		}
	default:
		if opts.pretty {
			data, err = json.MarshalIndent(payload, "", "  ")
		} else {
			data, err = json.Marshal(payload)
		}
	}
	if err != nil {
		return fmt.Errorf("序列化抽取结果失败：%w", err)
	}
	data = append(data, '\n')
	if opts.output == "" || opts.output == "-" {
		_, err = stdout.Write(data)
		return err
	}
	if err := os.WriteFile(opts.output, data, 0o600); err != nil {
		return fmt.Errorf("写入输出文件失败：%w", err)
	}
	return nil
}

// createOutput 打开合并 PDF 的输出文件，标准输出用 - 表示。
func createOutput(path string) (*os.File, error) {
	if path == "-" {
		return os.Stdout, nil
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("创建合并 PDF 失败：%w", err)
	}
	return file, nil
}

// removeOutput 删除半成品输出文件，只在写到真实文件时执行。
func removeOutput(path string) {
	if path == "" || path == "-" {
		return
	}
	_ = os.Remove(path)
}
