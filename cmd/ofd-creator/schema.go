package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/goccy/go-json"
	"github.com/spf13/cobra"
	"github.com/zc310/ofd/internal/manifest"
)

// schemaOptions 是 schema 子命令的选项。
type schemaOptions struct {
	format string
	output string
	help   bool
}

func runSchema(args []string, stdout, stderr io.Writer) int {
	opts, err := parseSchemaArgs(args, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator schema:", err)
		return exitUsage
	}
	if opts.help {
		return exitOK
	}
	format := strings.ToLower(strings.TrimSpace(opts.format))
	if format == "" {
		format = schemaFormatMarkdown
	}
	if format != schemaFormatMarkdown && format != schemaFormatJSON {
		_, _ = fmt.Fprintln(stderr, "ofd-creator schema:", fmt.Errorf("不支持的参考格式 %q，可用取值: markdown、json", opts.format))
		return exitUsage
	}
	var data []byte
	if format == schemaFormatJSON {
		data, err = schemaJSON(manifest.SchemaDocs)
	} else {
		data, err = schemaMarkdown(manifest.SchemaDocs)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator schema:", err)
		return exitOutput
	}
	// 未指定输出与 "-" 都表示标准输出，其余由 writeOutput 原子落盘。
	target := opts.output
	if strings.TrimSpace(target) == "" {
		target = "-"
	}
	if err := writeOutput(target, data, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator schema:", err)
		return exitOutput
	}
	return exitOK
}

// schema 支持的参考格式。
const (
	schemaFormatMarkdown = "markdown"
	schemaFormatJSON     = "json"
)

func parseSchemaArgs(args []string, output io.Writer) (*schemaOptions, error) {
	opts := &schemaOptions{format: schemaFormatMarkdown}
	root := &cobra.Command{
		Use:           "ofd-creator schema [选项]",
		Short:         "输出 manifest 字段参考文档",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		RunE:          func(_ *cobra.Command, _ []string) error { return nil },
	}
	root.SetArgs(args)
	root.SetOut(output)
	root.SetErr(output)
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		opts.help = true
		out := cmd.OutOrStdout()
		_, _ = fmt.Fprintln(out, "ofd-creator schema - 输出 manifest 字段参考文档")
		_, _ = fmt.Fprintln(out, "用法：ofd-creator schema [选项]")
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "字段说明取自 manifest schema 的文档注释，随代码一起生成，不会与实现脱节。")
		_, _ = fmt.Fprintln(out, "结构体按首次到达的路径分组；复合对象可以嵌套复合对象，同一个结构体只列一次。")
		_, _ = fmt.Fprintln(out)
		flags := cmd.Flags()
		flags.SetOutput(out)
		flags.PrintDefaults()
	})
	flags := root.Flags()
	flags.StringVar(&opts.format, "format", opts.format, "参考格式：markdown 或 json")
	flags.StringVarP(&opts.output, "output", "o", "", "输出路径；省略或使用 - 写入标准输出")
	if err := root.Execute(); err != nil {
		return nil, err
	}
	if opts.help {
		return opts, nil
	}
	return opts, validateSchemaOptions(opts)
}

func validateSchemaOptions(opts *schemaOptions) error {
	// 输出路径留空或写 - 都表示标准输出，其余交给 writeOutput 原子落盘。
	if strings.TrimSpace(opts.format) == "" {
		return errors.New("--format 不能是空值")
	}
	return nil
}

// schemaMarkdown 把字段说明渲染成 Markdown 文档。
func schemaMarkdown(docs []manifest.TypeDoc) ([]byte, error) {
	fields := 0
	for _, typed := range docs {
		fields += len(typed.Fields)
	}
	var out bytes.Buffer
	_, _ = fmt.Fprintln(&out, "# ofd-creator manifest 字段参考")
	_, _ = fmt.Fprintln(&out)
	_, _ = fmt.Fprintf(&out, "共 %d 个结构体、%d 个字段。字段说明取自 `internal/manifest/manifest.go` 的文档注释，", len(docs), fields)
	_, _ = fmt.Fprintln(&out, "由 `go generate ./internal/manifest` 生成，不会与实现脱节。")
	_, _ = fmt.Fprintln(&out)
	_, _ = fmt.Fprintln(&out, "结构体按首次到达的路径分组。manifest 的字段路径构成一棵树，而结构体构成一张图：")
	_, _ = fmt.Fprintln(&out, "复合对象可以嵌套复合对象，同一个结构体（例如 `Item`）会在几十条路径下出现，")
	_, _ = fmt.Fprintln(&out, "因此每个结构体只列出一次，表中的路径是首次到达它的示例路径，字段的完整路径是在该路径后追加字段名，")
	_, _ = fmt.Fprintln(&out, "结构体字段还要追加 `[]`，例如 `pages[].items[].fill_color.axial.segments[]`。")
	_, _ = fmt.Fprintln(&out)
	_, _ = fmt.Fprintln(&out, "表头中的“可省略”列表示该字段的 manifest tag 是否带 `omitempty`，它只描述序列化行为，")
	_, _ = fmt.Fprintln(&out, "不等于“必填”：`version` 带 `omitempty`，但缺省或不是 1 仍会被拒绝。")
	_, _ = fmt.Fprintln(&out)
	for _, typed := range docs {
		_, _ = fmt.Fprintf(&out, "## %s\n\n", typed.Name)
		if typed.Doc != "" {
			_, _ = fmt.Fprintf(&out, "%s\n\n", typed.Doc)
		}
		if typed.Path == "" {
			_, _ = fmt.Fprintln(&out, "示例路径：文档根。")
			_, _ = fmt.Fprintln(&out)
		} else {
			_, _ = fmt.Fprintf(&out, "示例路径：`%s`。\n\n", typed.Path)
		}
		_, _ = fmt.Fprintln(&out, "| 字段 | 类型 | 可省略 | 说明 |")
		_, _ = fmt.Fprintln(&out, "|------|------|:------:|------|")
		for _, field := range typed.Fields {
			_, _ = fmt.Fprintf(&out, "| `%s` | `%s` | %s | %s |\n", field.Name, field.Type, requiredMark(field.Optional), cellText(field.Doc))
		}
		_, _ = fmt.Fprintln(&out)
	}
	return out.Bytes(), nil
}

// requiredMark 把 omitempty 标记翻译成“可省略”列的取值。
func requiredMark(optional bool) string {
	if optional {
		return "是"
	}
	return "否"
}

// cellText 转义表格单元格里的竖线并把换行压平。
func cellText(text string) string {
	text = strings.ReplaceAll(text, "|", "\\|")
	text = strings.ReplaceAll(text, "\r\n", " ")
	return strings.ReplaceAll(text, "\n", " ")
}

// schemaJSON 把字段说明编码成 JSON，便于其它工具消费。
func schemaJSON(docs []manifest.TypeDoc) ([]byte, error) {
	type field struct {
		Name string `json:"name"`
		Type string `json:"type"`
		// OmitEmpty 表示 manifest tag 带 omitempty，即零值时可以不写。
		// 它只描述序列化行为，不代表字段必填。
		OmitEmpty bool   `json:"omitempty"`
		Doc       string `json:"doc,omitempty"`
	}
	type structDoc struct {
		Name  string  `json:"name"`
		Path  string  `json:"path"`
		Doc   string  `json:"doc,omitempty"`
		Count int     `json:"field_count"`
		List  []field `json:"fields"`
	}
	list := make([]structDoc, 0, len(docs))
	for _, typed := range docs {
		entry := structDoc{Name: typed.Name, Path: typed.Path, Doc: typed.Doc, Count: len(typed.Fields), List: []field{}}
		for _, item := range typed.Fields {
			entry.List = append(entry.List, field{Name: item.Name, Type: item.Type, OmitEmpty: item.Optional, Doc: item.Doc})
		}
		list = append(list, entry)
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("编码 JSON 参考失败: %w", err)
	}
	return append(data, '\n'), nil
}
