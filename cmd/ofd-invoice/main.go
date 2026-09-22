// 命令 ofd-invoice 从 OFD 电子发票中抽取结构化发票信息并输出 JSON。
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/zc310/ofd/internal/utils"
	"github.com/zc310/ofd/pkg/invoice"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

type options struct {
	input  string
	output string
	pretty bool
	help   bool
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
	result, err := invoice.Extract(opts.input)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-invoice:", err)
		return exitError
	}
	if err := writeJSON(opts, result, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-invoice:", err)
		return exitError
	}
	return exitOK
}

func parseArgs(args []string, stdout, stderr io.Writer) (*options, error) {
	opts := &options{}
	root := &cobra.Command{
		Use:           "ofd-invoice [flags] input.ofd",
		Short:         "OFD 电子发票信息抽取工具",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, positional []string) error {
			if opts.help {
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
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		opts.help = true
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "ofd-invoice - OFD 电子发票信息抽取工具")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "用法：ofd-invoice [选项] input.ofd")
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
		flags := cmd.Flags()
		flags.SetOutput(cmd.OutOrStdout())
		flags.PrintDefaults()
	})
	flags := root.Flags()
	flags.StringVarP(&opts.output, "output", "o", "", "JSON 输出路径；- 或省略时输出到标准输出")
	flags.BoolVar(&opts.pretty, "pretty", false, "缩进 JSON 输出")
	if err := root.Execute(); err != nil {
		return nil, err
	}
	if opts.help || opts.input == "" {
		return opts, nil
	}
	if _, err := os.Stat(opts.input); err != nil {
		return nil, fmt.Errorf("输入文件：%w", err)
	}
	if opts.output != "" && opts.output != "-" && utils.SamePath(opts.input, opts.output) {
		return nil, errors.New("输出不能覆盖输入 OFD 文件")
	}
	return opts, nil
}

func writeJSON(opts *options, result *invoice.Invoice, stdout io.Writer) error {
	var buffer bytes.Buffer
	var data []byte
	var err error
	if opts.pretty {
		data, err = json.MarshalIndent(result, "", "  ")
	} else {
		data, err = json.Marshal(result)
	}
	if err != nil {
		return fmt.Errorf("序列化抽取结果失败：%w", err)
	}
	buffer.Write(data)
	buffer.WriteByte('\n')
	if opts.output == "" || opts.output == "-" {
		_, err = stdout.Write(buffer.Bytes())
		return err
	}
	file, err := os.Create(opts.output)
	if err != nil {
		return fmt.Errorf("创建输出文件失败：%w", err)
	}
	if _, err = file.Write(buffer.Bytes()); err != nil {
		_ = file.Close()
		return fmt.Errorf("写入输出文件失败：%w", err)
	}
	if err = file.Close(); err != nil {
		return fmt.Errorf("关闭输出文件失败：%w", err)
	}
	return nil
}
