package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zc310/ofd/internal/fileutil"
	"github.com/zc310/ofd/internal/securexml"
	"github.com/zc310/ofd/pkg/sign"
	"github.com/zc310/ofd/pkg/spec"
	"github.com/zc310/ofd/pkg/validator"
)

type signOptions struct {
	signFlags
	input, output, relative string
	append, lock, validate  bool
	physical                bool
	deterministic, help     bool
}

func parseSignArgs(args []string, output io.Writer) (*signOptions, error) {
	opts := &signOptions{}
	command := &cobra.Command{
		Use: "ofd-creator sign [选项] input.ofd", Short: "签署 OFD 或追加普通数字签名",
		SilenceErrors: true, SilenceUsage: true, Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, positional []string) error {
			if len(positional) == 1 {
				if opts.input != "" {
					return fmt.Errorf("输入文件不能同时使用位置参数和 --input")
				}
				opts.input = positional[0]
			}
			return nil
		},
	}
	command.SetArgs(args)
	command.SetOut(output)
	command.SetErr(output)
	command.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		opts.help = true
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "ofd-creator sign - 签署 OFD 或追加普通数字签名")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "用法：ofd-creator sign --append --sign-cmd ./signer -o signed.ofd input.ofd")
		cmd.Flags().SetOutput(cmd.OutOrStdout())
		cmd.Flags().PrintDefaults()
	})
	flags := command.Flags()
	flags.StringVarP(&opts.input, "input", "i", "", "输入 OFD 文件")
	flags.StringVarP(&opts.output, "output", "o", "", "输出 OFD 文件；- 写标准输出")
	flags.BoolVar(&opts.append, "append", false, "保留已有普通签名并追加；首次签署也可设置")
	flags.BoolVar(&opts.physical, "physical", false, "签署加密后的物理包；要求明文 OFD.xml 且没有已有签名入口")
	flags.StringVar(&opts.relative, "relative", "", "本次签名依赖的已有签名 ID，需要 --append")
	flags.BoolVar(&opts.lock, "lock-signatures", false, "摘要最终签名列表，结束追加流程；需要 --append")
	flags.BoolVar(&opts.validate, "validate", false, "输出前执行启用 GM/T 0099 扩展的严格结构和摘要校验")
	flags.BoolVar(&opts.deterministic, "deterministic", false, "使用固定 ZIP 时间")
	registerSignFlags(flags, &opts.signFlags)
	// 独立签署入口以普通签名为默认类型；空 ID 交给 API 自动分配。
	opts.signType, opts.signID = spec.SigTypeSign, ""
	flags.Lookup("sign-type").DefValue = spec.SigTypeSign
	flags.Lookup("sign-id").DefValue = ""
	if err := command.Execute(); err != nil {
		return nil, err
	}
	return opts, nil
}

func validateSignOptions(opts *signOptions) error {
	if err := validateSignFlags(&opts.signFlags); err != nil {
		return err
	}
	if strings.TrimSpace(opts.input) == "" || strings.TrimSpace(opts.output) == "" || strings.TrimSpace(opts.signCmd) == "" {
		return fmt.Errorf("需要输入文件、--output 和 --sign-cmd")
	}
	if opts.input == "-" {
		return fmt.Errorf("sign 需要输入 OFD 文件路径")
	}
	if opts.output != "-" && fileutil.SamePath(opts.input, opts.output) {
		return fmt.Errorf("输出文件不能覆盖输入 OFD 文件")
	}
	if !opts.append && (opts.relative != "" || opts.lock) {
		return fmt.Errorf("--relative 与 --lock-signatures 需要 --append")
	}
	if opts.append && opts.signType != spec.SigTypeSign {
		return fmt.Errorf("--append 仅支持 --sign-type Sign")
	}
	if opts.physical && (opts.signType != spec.SigTypeSign || opts.append || opts.relative != "" || opts.lock || signReferences(&opts.signFlags) != nil || opts.signStamp || opts.signStampSeams || opts.signSeal != "" || opts.signImage != "") {
		return fmt.Errorf("--physical 仅支持普通签名，不能组合追加、引用筛选或印模选项")
	}
	if id := strings.TrimSpace(opts.signID); id != "" && !securexml.ValidID(id) {
		return fmt.Errorf("--sign-id 必须为合法 xs:ID")
	}
	if opts.relative != "" && !securexml.ValidID(opts.relative) {
		return fmt.Errorf("--relative 必须为合法 xs:IDREF")
	}
	if opts.signType == spec.SigTypeSign {
		switch strings.TrimSpace(opts.signMethod) {
		case "", sign.SM2WithSM3, sign.SHA256WithRSA, sign.SHA384WithRSA, sign.SHA512WithRSA,
			sign.ECDSAWithSHA256, sign.ECDSAWithSHA384, sign.ECDSAWithSHA512:
		default:
			return fmt.Errorf("%w: 普通数字签名算法 %s", errors.ErrUnsupported, opts.signMethod)
		}
		switch strings.ToUpper(strings.TrimSpace(opts.signCheckMethod)) {
		case "", "SM3", "1.2.156.10197.1.401", "SHA256", "SHA384", "SHA512":
		default:
			return fmt.Errorf("%w: 普通数字签名仅支持 SM3 或 SHA256/SHA384/SHA512 引用摘要", errors.ErrUnsupported)
		}
	}
	return nil
}

func runSign(args []string, stdout, stderr io.Writer) int {
	opts, err := parseSignArgs(args, stdout)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator sign:", err)
		return exitUsage
	}
	if opts.help {
		return exitOK
	}
	if err := validateSignOptions(opts); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator sign:", err)
		return exitUsage
	}
	options := buildSignOptions(&opts.signFlags, opts.deterministic)
	options.Append, options.Relative, options.LockSignatures = opts.append, opts.relative, opts.lock
	options.Physical = opts.physical
	var data bytes.Buffer
	if err := sign.Sign(context.Background(), opts.input, &data, options); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator sign:", err)
		return exitResource
	}
	if opts.validate && opts.physical {
		results, err := sign.VerifyPhysical(context.Background(), data.Bytes(), sign.VerificationOptions{})
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator sign:", err)
			return exitValidate
		}
		for _, result := range results {
			if !result.DigestValid || result.Result == nil || !result.Result.Valid {
				_, _ = fmt.Fprintln(stderr, "ofd-creator sign: 物理包签名或摘要验证失败")
				return exitValidate
			}
		}
	} else if opts.validate {
		instance, err := validator.New(validator.WithGMT0099(true))
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "ofd-creator sign:", err)
			return exitValidate
		}
		report := instance.ValidateReader(context.Background(), bytes.NewReader(data.Bytes()), "signed.ofd")
		if report.HasErrors() {
			_ = validator.RenderText(stderr, report)
			return exitValidate
		}
	}
	if err := writeOutput(opts.output, data.Bytes(), stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-creator sign:", err)
		return exitOutput
	}
	return exitOK
}
