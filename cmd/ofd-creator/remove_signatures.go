package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zc310/ofd/internal/fileutil"
	"github.com/zc310/ofd/internal/securexml"
	"github.com/zc310/ofd/pkg/sign"
	"github.com/zc310/ofd/pkg/validator"
)

func runRemoveSignatures(args []string, stdout, stderr io.Writer) int {
	var input, output string
	var options sign.RemoveOptions
	var validate, help bool
	command := &cobra.Command{
		Use: "ofd-creator remove-signatures [选项] input.ofd", Short: "移除全部或指定签名，清理历史签名残留",
		SilenceErrors: true, SilenceUsage: true, Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, positional []string) error {
			if len(positional) == 1 {
				if input != "" {
					return fmt.Errorf("输入文件不能同时使用位置参数和 --input")
				}
				input = positional[0]
			}
			return nil
		},
	}
	command.SetArgs(args)
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		help = true
		_, _ = fmt.Fprintln(stdout, "ofd-creator remove-signatures - 移除已有签名，无需签名器")
		_, _ = fmt.Fprintln(stdout, "用法：ofd-creator remove-signatures -o unsigned.ofd input.ofd")
		cmd.Flags().SetOutput(stdout)
		cmd.Flags().PrintDefaults()
	})
	flags := command.Flags()
	flags.StringVarP(&input, "input", "i", "", "输入 OFD 文件")
	flags.StringVarP(&output, "output", "o", "", "输出 OFD 文件；- 写标准输出")
	flags.StringArrayVar(&options.IDs, "id", nil, "仅移除指定 ID，可重复；默认移除全部签名")
	flags.BoolVar(&options.Cleanup, "cleanup", false, "全部移除时清理 Signatures/、Signs/ 等惯用路径残留，容许签名元数据损坏")
	flags.BoolVar(&options.Deterministic, "deterministic", false, "使用固定 ZIP 时间")
	flags.BoolVar(&validate, "validate", false, "输出前执行启用 GM/T 0099 扩展的严格校验")
	usageError := func(err error) int {
		_, _ = fmt.Fprintln(stderr, "ofd-creator remove-signatures:", err)
		return exitUsage
	}
	if err := command.Execute(); err != nil {
		return usageError(err)
	}
	if help {
		return exitOK
	}
	if strings.TrimSpace(input) == "" || strings.TrimSpace(output) == "" || input == "-" {
		return usageError(fmt.Errorf("需要输入 OFD 文件路径和 --output"))
	}
	if output != "-" && fileutil.SamePath(input, output) {
		return usageError(fmt.Errorf("输出文件不能覆盖输入 OFD 文件"))
	}
	if options.Cleanup && len(options.IDs) > 0 {
		return usageError(fmt.Errorf("--cleanup 不能与 --id 同时使用"))
	}
	for _, id := range options.IDs {
		if !securexml.ValidID(id) {
			return usageError(fmt.Errorf("签名 ID 无效: %q", id))
		}
	}
	var data bytes.Buffer
	if err := sign.Remove(context.Background(), input, &data, options); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator remove-signatures:", err)
		return exitResource
	}
	if validate {
		instance, err := validator.New(validator.WithGMT0099(true))
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator remove-signatures:", err)
			return exitValidate
		}
		report := instance.ValidateReader(context.Background(), bytes.NewReader(data.Bytes()), "unsigned.ofd")
		if report.HasErrors() {
			_ = validator.RenderText(stderr, report)
			return exitValidate
		}
	}
	if err := writeOutput(output, data.Bytes(), stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator remove-signatures:", err)
		return exitOutput
	}
	return exitOK
}
