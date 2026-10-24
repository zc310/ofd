package main

import (
	"bytes"
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/emmansun/gmsm/smx509"
	"github.com/spf13/cobra"
	"github.com/zc310/ofd/internal/securexml"
	"github.com/zc310/ofd/internal/version"
	"github.com/zc310/ofd/pkg/integrity"
	"github.com/zc310/ofd/pkg/sign"
	"github.com/zc310/ofd/pkg/spec"
)

func integrityCommand(ctx context.Context, stdin io.Reader, stdout io.Writer, showVersion *bool, selectAction func(string)) *cobra.Command {
	root := &cobra.Command{Use: "integrity", Short: "GM/T 0099 完整性清单、时间戳与防夹带", Args: cobra.NoArgs}
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		if *showVersion {
			_, err := fmt.Fprintln(stdout, version.Version)
			return err
		}
		return cmd.Help()
	}
	for _, action := range []string{"inspect", "protect", "verify"} {
		o := &options{}
		var command, id string
		var roots, tsaRoots, crls, issuers []string
		cmd := &cobra.Command{Use: action + " [选项] input.ofd", Short: map[string]string{"inspect": "检查清单及物理文件集合", "protect": "增加外部签名器提供的带时间戳清单", "verify": "验证清单、时间戳、引用闭包与内容覆盖"}[action]}
		cmd.Args = func(_ *cobra.Command, args []string) error {
			if *showVersion {
				return nil
			}
			if len(args) != 1 {
				return usage("必须且只能指定一个输入 OFD")
			}
			return nil
		}
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			if *showVersion {
				_, err := fmt.Fprintln(stdout, version.Version)
				return err
			}
			selectAction("integrity-" + action)
			o.action, o.input = "inspect", args[0]
			if action == "protect" {
				o.action = "protect"
			}
			o.userSet = cmd.Flags().Changed("user")
			o.protectedPaths = append(append(append(append([]string{}, roots...), tsaRoots...), crls...), issuers...)
			if action == "protect" && strings.TrimSpace(command) == "" {
				return usage("protect 需要 --sign-cmd 提供带时间戳的清单签名")
			}
			if action == "protect" && !securexml.ValidID(id) {
				return usage("--id 必须为合法 xs:ID")
			}
			if err := validate(o); err != nil {
				return err
			}
			verification, err := loadIntegrityTrust(ctx, roots, tsaRoots, crls, issuers)
			if err != nil {
				return err
			}
			opt := integrity.Options{Limits: o.limits, Verification: verification}
			var credentials *credentials
			if hasCredentials(o) {
				credentials, err = loadCredentials(ctx, o, stdin)
				if err != nil {
					return err
				}
				defer credentials.clear()
				decrypt := decryptOptions(o, credentials)
				opt.Decrypt = &decrypt
			}
			var report *integrity.Report
			switch action {
			case "inspect":
				report, err = integrity.Inspect(ctx, o.input, opt)
			case "verify":
				report, err = integrity.Verify(ctx, o.input, opt)
			case "protect":
				err = atomicOutput(ctx, o, func(w io.Writer) error {
					report, err = integrity.Protect(ctx, o.input, w, integrity.ProtectOptions{Options: opt, ID: id, CreatorName: "ofd-crypt", Sign: func(ctx context.Context, data []byte) ([]byte, error) { return runIntegritySigner(ctx, command, data) }})
					return err
				})
			}
			if err != nil && (report == nil || action == "protect") {
				return err
			}
			var reportErr error
			if action == "protect" || o.output == "" || o.output == "-" {
				reportErr = renderIntegrity(stdout, report, o.format)
			} else {
				reportErr = atomicOutput(ctx, o, func(w io.Writer) error { return renderIntegrity(w, report, o.format) })
			}
			if err != nil || reportErr != nil {
				return errors.Join(err, reportErr)
			}
			if action == "verify" && report.Status != "valid" {
				return fmt.Errorf("完整性验证结果: %s", report.Status)
			}
			return nil
		}
		flags := cmd.Flags()
		flags.StringVarP(&o.output, "output", "o", "", "protect 的 OFD 输出文件；其他命令为报告输出，缺省 stdout")
		flags.StringVar(&o.format, "format", "text", "报告格式：text、markdown 或 json")
		flags.BoolVar(&o.force, "force", false, "替换已有输出文件")
		flags.StringVar(&o.mode, "mode", "strict", "加密元数据读取模式：strict 或 compat")
		registerLimitFlags(flags, &o.limits)
		flags.StringArrayVar(&roots, "trust-root", nil, "签名信任根 PEM 文件，可重复")
		flags.StringArrayVar(&tsaRoots, "tsa-root", nil, "TSA 信任根 PEM 文件；缺省复用 trust-root")
		flags.StringArrayVar(&crls, "crl", nil, "离线 CRL PEM/DER 文件，可重复")
		flags.StringArrayVar(&issuers, "issuer", nil, "CRL 签发者证书 PEM/DER 文件，可重复")
		if action == "protect" {
			flags.StringVar(&command, "sign-cmd", "", "外部命令 stdin 读取 OFDEntries.xml，stdout 返回带时间戳的 GB/T 35275 签名")
			flags.StringVar(&id, "id", "integrity1", "完整性清单 xs:ID")
		}
		if action == "verify" {
			flags.StringVar(&o.passwordFile, "password-file", "", "解锁文档的 UTF-8 口令文件；- 表示 stdin")
			flags.StringVar(&o.key, "key", "", "SM2 私钥 PEM/DER 文件")
			flags.StringVar(&o.user, "user", "", "可选接收者名称")
			flags.StringVar(&o.missingScheme, "missing-scheme", "", "compat 模式缺省方案：password 或 certificate")
			flags.StringArrayVar(&o.passwordFor, "password-for", nil, "旧操作凭据 ID=FILE")
			flags.StringArrayVar(&o.keyFor, "key-for", nil, "旧操作私钥 ID=FILE")
			flags.StringArrayVar(&o.passwordForSeed, "password-for-seed", nil, "seed 凭据 PATH=FILE")
			flags.StringArrayVar(&o.keyForSeed, "key-for-seed", nil, "seed 私钥 PATH=FILE")
		}
		root.AddCommand(cmd)
	}
	return root
}

func loadIntegrityTrust(ctx context.Context, roots, tsaRoots, crls, issuers []string) (sign.VerificationOptions, error) {
	var opt sign.VerificationOptions
	pool := func(paths []string) (*sign.CertificateTrustOptions, error) {
		if len(paths) == 0 {
			return nil, nil
		}
		p := smx509.NewCertPool()
		for _, file := range paths {
			if file == "-" {
				return nil, usage("信任根必须使用文件路径")
			}
			data, err := readCredential(ctx, file, nil, 4<<20)
			if err != nil {
				return nil, err
			}
			if !p.AppendCertsFromPEM(data) {
				return nil, fmt.Errorf("信任根文件没有可解析证书: %s", file)
			}
		}
		return &sign.CertificateTrustOptions{Roots: p}, nil
	}
	var err error
	opt.Trust, err = pool(roots)
	if err != nil {
		return opt, err
	}
	opt.TimestampTrust, err = pool(tsaRoots)
	if err != nil {
		return opt, err
	}
	if len(crls)+len(issuers) > 0 {
		opt.Revocation = &sign.CertificateRevocationOptions{}
		for _, group := range []struct {
			paths []string
			kind  string
			out   *[][]byte
		}{{crls, "X509 CRL", &opt.Revocation.CRLs}, {issuers, "CERTIFICATE", &opt.Revocation.Issuers}} {
			for _, file := range group.paths {
				if file == "-" {
					return opt, usage("证据必须使用文件路径")
				}
				data, err := readCredential(ctx, file, nil, 4<<20)
				if err != nil {
					return opt, err
				}
				if bytes.HasPrefix(bytes.TrimSpace(data), []byte("-----BEGIN")) {
					for len(bytes.TrimSpace(data)) != 0 {
						block, rest := pem.Decode(data)
						if block == nil || block.Type != group.kind {
							return opt, fmt.Errorf("证据 PEM 类型无效")
						}
						*group.out = append(*group.out, block.Bytes)
						data = rest
					}
				} else {
					*group.out = append(*group.out, data)
				}
			}
		}
	}
	return opt, nil
}

type limitedBuffer struct {
	buffer bytes.Buffer
	max    int
}

func (b *limitedBuffer) Len() int       { return b.buffer.Len() }
func (b *limitedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *limitedBuffer) String() string { return b.buffer.String() }

func (b *limitedBuffer) Write(data []byte) (int, error) {
	if len(data) > b.max-b.Len() {
		return 0, fmt.Errorf("外部签名器输出超过上限")
	}
	return b.buffer.Write(data)
}

func runIntegritySigner(ctx context.Context, command string, data []byte) ([]byte, error) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil, usage("外部签名命令为空")
	}
	cmd := exec.CommandContext(ctx, fields[0], fields[1:]...)
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdin = bytes.NewReader(data)
	cmd.Env = append(os.Environ(), "OFD_SIGN_TYPE="+spec.SigTypeSign)
	stdout, stderr := &limitedBuffer{max: 4 << 20}, &limitedBuffer{max: 64 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("清单签名命令失败: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func renderIntegrity(w io.Writer, report *integrity.Report, format string) error {
	if format == "json" {
		return render(w, report, format)
	}
	if format == "markdown" {
		if _, err := fmt.Fprint(w, "# OFD 完整性保护报告\n\n"); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "状态：%s\n清单存在：%t\n", report.Status, report.Present); err != nil {
		return err
	}
	for _, check := range report.Checks {
		if _, err := fmt.Fprintf(w, "检查：%s  %s  %s\n", check.Code, check.Status, check.Message); err != nil {
			return err
		}
	}
	if report.Signature != nil && report.Signature.Validation != nil {
		for _, check := range report.Signature.Validation.Checks {
			if _, err := fmt.Fprintf(w, "签名检查：%s  %s  %s\n", check.Code, check.Status, check.Message); err != nil {
				return err
			}
		}
	}
	for _, signature := range report.PhysicalSignatures {
		if signature.Result == nil || signature.Result.Validation == nil {
			continue
		}
		for _, check := range signature.Result.Validation.Checks {
			if _, err := fmt.Fprintf(w, "物理签名检查：%s  %s  %s  %s\n", signature.ID, check.Code, check.Status, check.Message); err != nil {
				return err
			}
		}
	}
	for _, list := range []struct {
		label string
		names []string
	}{{"未列入清单", report.Unlisted}, {"清单目标缺失", report.Missing}, {"未被引用", report.Unreferenced}, {"内容保护未确认", report.Unprotected}} {
		for _, name := range list.names {
			if _, err := fmt.Fprintf(w, "%s：%s\n", list.label, name); err != nil {
				return err
			}
		}
	}
	return nil
}
