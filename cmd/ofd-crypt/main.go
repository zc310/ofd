// Command ofd-crypt inspects, encrypts, and decrypts GM/T 0099-2020 OFD packages.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/version"
	"github.com/zc310/ofd/pkg/crypt"
	"github.com/zc310/ofd/pkg/spec"
)

var errUsage = errors.New("command usage error")

type options struct {
	action, input, output, format, mode, user, role, passwordFile, key, cert, missingScheme string
	passwordRecipients, recipients, entries                                                 []string
	passwordFor, keyFor, passwordForSeed, keyForSeed                                        []string
	inputPasswordFile, inputKey, inputUser, pageDocument                                    string
	pageIDs                                                                                 []string
	appendLayer, independentKeys                                                            bool
	force, version, keepRoot, plaintextMap, userSet                                         bool
	limits                                                                                  crypt.Limits
	protectedPaths                                                                          []string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func usage(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errUsage, fmt.Sprintf(format, args...))
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	o := &options{}
	root := &cobra.Command{Use: "ofd-crypt", Short: "OFD 加解密与完整性保护", SilenceUsage: true, SilenceErrors: true}
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.PersistentFlags().BoolVarP(&o.version, "version", "v", false, "输出工具版本")
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		if o.version {
			_, err := fmt.Fprintln(stdout, version.Version)
			return err
		}
		return usage("需要指定 inspect、encrypt、decrypt 或 integrity 子命令")
	}
	for _, action := range []string{"inspect", "encrypt", "decrypt"} {
		cmd := &cobra.Command{Use: action + " [选项] input.ofd", Short: map[string]string{"inspect": "检查可见加密信息", "encrypt": "加密为 OFD", "decrypt": "恢复明文 OFD"}[action]}
		cmd.Args = func(_ *cobra.Command, values []string) error {
			if o.version {
				return nil
			}
			if len(values) != 1 {
				return usage("必须且只能指定一个输入 OFD 文件")
			}
			return nil
		}
		cmd.RunE = func(cmd *cobra.Command, values []string) error {
			if o.version {
				_, err := fmt.Fprintln(stdout, version.Version)
				return err
			}
			o.action, o.input = action, values[0]
			o.userSet = cmd.Flags().Changed("user")
			if err := validate(o); err != nil {
				return err
			}
			return execute(ctx, o, stdin, stdout)
		}
		flags := cmd.Flags()
		flags.StringVarP(&o.output, "output", "o", "", "输出路径；inspect 默认标准输出，加解密必须指定文件")
		flags.StringVar(&o.format, "format", "text", "报告格式：text、markdown 或 json")
		flags.BoolVar(&o.force, "force", false, "替换已存在的输出文件")
		flags.Int64Var(&o.limits.MaxInputBytes, "max-input-bytes", 0, "输入 ZIP 大小上限；0 使用库默认值")
		flags.Int64Var(&o.limits.MaxEntryBytes, "max-entry-bytes", 0, "单条解压/输出大小上限；0 使用默认值")
		flags.Int64Var(&o.limits.MaxTotalBytes, "max-total-bytes", 0, "输入解压及输出总大小分别限制；0 使用默认值")
		flags.IntVar(&o.limits.MaxEntries, "max-entries", 0, "ZIP 条目数量上限；0 使用默认值")
		flags.Int64Var(&o.limits.MaxMetadataBytes, "max-metadata-bytes", 0, "单份加密元数据大小上限；0 使用默认值")
		flags.IntVar(&o.limits.MaxCredentialAttempts, "max-credential-attempts", 0, "所有 seed 累计凭据尝试上限；0 使用默认 32")
		flags.IntVar(&o.limits.MaxRecipients, "max-recipients", 0, "所有 seed 累计接收者上限；0 使用默认 1024")
		flags.Int64Var(&o.limits.MaxProcessedBytes, "max-processed-bytes", 0, "视图累计处理字节上限；0 使用默认值")
		flags.IntVar(&o.limits.MaxLayers, "max-layers", 0, "加密依赖深度上限；0 使用默认值")
		flags.StringVar(&o.mode, "mode", "strict", "读取模式（加密时用于旧层预检）：strict 或 compat")
		flags.StringVar(&o.passwordFile, "password-file", "", "原样读取 UTF-8 口令文件；- 表示标准输入")
		flags.StringVar(&o.user, "user", "User", "单接收者名称；解密时省略则自动匹配")
		if action != "encrypt" {
			flags.StringVar(&o.key, "key", "", "SM2 PKCS#8 私钥文件（未加密 PEM 或 DER）")
		}
		flags.StringVar(&o.missingScheme, "missing-scheme", "", "compat 模式下缺失的方案：password 或 certificate")
		flags.StringArrayVar(&o.passwordFor, "password-for", nil, "按已有操作选择口令：ID=口令文件，可重复")
		flags.StringArrayVar(&o.keyFor, "key-for", nil, "按已有操作选择私钥：ID=私钥文件，可重复")
		flags.StringArrayVar(&o.passwordForSeed, "password-for-seed", nil, "按规范 seed 路径选择口令：PATH=口令文件，可重复")
		flags.StringArrayVar(&o.keyForSeed, "key-for-seed", nil, "按规范 seed 路径选择私钥：PATH=私钥文件，可重复")
		if action == "encrypt" {
			flags.StringVar(&o.cert, "cert", "", "单个接收者 SM2 证书（PEM 或 DER）")
			flags.StringArrayVar(&o.recipients, "recipient", nil, "证书接收者 NAME=证书路径，可重复")
			flags.StringArrayVar(&o.passwordRecipients, "password-recipient", nil, "口令接收者 NAME=口令文件路径，可重复")
			flags.StringVar(&o.role, "role", "User", "接收者角色：User 或 Owner")
			flags.StringArrayVar(&o.entries, "entry", nil, "仅加密指定 ZIP 相对路径，可重复；省略则全部")
			flags.BoolVar(&o.keepRoot, "keep-root", false, "保留 OFD.xml 明文")
			flags.BoolVar(&o.plaintextMap, "plaintext-map", false, "输出明文映射表")
			flags.BoolVar(&o.independentKeys, "independent-keys", false, "每个条目使用独立随机 FEK/IV 和 seed")
			flags.BoolVar(&o.appendLayer, "append", false, "用旧凭据预检后追加一层加密")
			flags.StringVar(&o.inputPasswordFile, "input-password-file", "", "追加前预检的缺省旧口令文件")
			flags.StringVar(&o.inputKey, "input-key", "", "追加前预检的缺省旧 SM2 私钥")
			flags.StringVar(&o.inputUser, "input-user", "", "追加前预检的旧接收者名称；省略则自动匹配")
			flags.StringVar(&o.pageDocument, "page-document", "", "页面范围所属 Document.xml 的规范 ZIP 路径")
			flags.StringArrayVar(&o.pageIDs, "page-id", nil, "选择页面 ID 并扩展模板、注释及资源范围，可重复")
		}
		root.AddCommand(cmd)
	}
	root.AddCommand(integrityCommand(ctx, stdin, stdout, &o.version, func(action string) { o.action = action }))
	if err := root.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-crypt:", err)
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return 130
		}
		if errors.Is(err, errUsage) {
			return 2
		}
		// Cobra parsing/unknown-command errors occur before action is selected.
		if o.action == "" {
			return 2
		}
		return 1
	}
	return 0
}

func validate(o *options) error {
	if o.key == "-" || o.cert == "-" || o.inputKey == "-" {
		return usage("证书和私钥必须使用文件路径")
	}
	if o.input == "-" {
		return usage("输入 OFD 必须为文件路径；标准输入仅用于口令")
	}
	if o.format != "text" && o.format != "markdown" && o.format != "json" {
		return usage("报告格式必须为 text、markdown 或 json")
	}
	if o.mode != "" && o.mode != "strict" && o.mode != "compat" {
		return usage("读取模式必须为 strict 或 compat")
	}
	if o.limits.MaxInputBytes < 0 || o.limits.MaxEntryBytes < 0 || o.limits.MaxTotalBytes < 0 || o.limits.MaxEntries < 0 || o.limits.MaxMetadataBytes < 0 || o.limits.MaxProcessedBytes < 0 || o.limits.MaxLayers < 0 || o.limits.MaxCredentialAttempts < 0 || o.limits.MaxRecipients < 0 {
		return usage("预算参数不能为负数")
	}
	if o.action != "inspect" && (o.output == "" || o.output == "-") {
		return usage("加解密必须用 --output 指定文件路径")
	}
	if o.action != "encrypt" || o.appendLayer {
		password, key := o.passwordFile, o.key
		if o.action == "encrypt" {
			password, key = o.inputPasswordFile, o.inputKey
			if !hasInputCredentials(o) {
				return usage("追加层需要旧口令、旧私钥或按操作/seed 指定的旧凭据")
			}
		}
		if password != "" && key != "" {
			return usage("缺省口令和私钥必须二选一")
		}
		if o.action == "decrypt" && !hasCredentials(o) {
			return usage("解密需要口令、私钥或按操作/seed 指定的凭据")
		}
		if o.missingScheme != "" && (o.mode != "compat" || (o.missingScheme != "password" && o.missingScheme != "certificate")) {
			return usage("--missing-scheme 仅可在 compat 模式指定 password 或 certificate")
		}
		seen := map[string]bool{}
		stdinCount := 0
		if password == "-" {
			stdinCount++
		}
		for _, spec := range credentialSelections(o) {
			for _, value := range spec.values {
				selector, file, err := namedFile(value)
				if err != nil {
					return err
				}
				id := spec.kind + selector
				if seen[id] {
					return usage("同一操作或 seed 不能重复指定凭据")
				}
				seen[id] = true
				if spec.kind == "seed:" && (core.ValidateEntryName(selector) != nil || strings.Contains(selector, ":")) {
					return usage("seed 选择必须使用规范 ZIP 相对路径")
				}
				if file == "-" {
					if !spec.password {
						return usage("私钥必须使用文件路径")
					}
					stdinCount++
				}
			}
		}
		if stdinCount > 1 {
			return usage("标准输入只能供一个凭据读取")
		}
	}
	if o.action == "encrypt" {
		if !o.appendLayer && (hasInputCredentials(o) || o.inputUser != "" || o.mode != "strict" || o.missingScheme != "") {
			return usage("旧凭据和兼容读取选项仅用于 --append")
		}
		if (o.pageDocument == "") != (len(o.pageIDs) == 0) || len(o.pageIDs) > 0 && (len(o.entries) > 0 || o.appendLayer) {
			return usage("页面范围需要 --page-document 和 --page-id，且不能与 --entry、--append 同用")
		}
		if len(o.pageIDs) > 0 {
			if core.ValidateEntryName(o.pageDocument) != nil || strings.Contains(o.pageDocument, ":") {
				return usage("--page-document 需要规范 ZIP 相对路径")
			}
			seen := map[string]bool{}
			for _, id := range o.pageIDs {
				n, err := strconv.ParseUint(id, 10, 32)
				if err != nil || n == 0 || strconv.FormatUint(n, 10) != id || seen[id] {
					return usage("--page-id 必须为唯一的规范正整数")
				}
				seen[id] = true
			}
		}
		families := 0
		for _, present := range []bool{o.passwordFile != "", o.cert != "", len(o.recipients) > 0, len(o.passwordRecipients) > 0} {
			if present {
				families++
			}
		}
		if families != 1 {
			return usage("加密必须选择一种接收者输入：--password-file、--cert、--recipient 或 --password-recipient")
		}
		if o.role != "User" && o.role != "Owner" {
			return usage("--role 必须为 User 或 Owner")
		}
		if o.userSet && (len(o.recipients) > 0 || len(o.passwordRecipients) > 0) {
			return usage("多接收者名称通过 NAME=文件指定，不能再指定 --user")
		}
		for _, value := range append(append([]string{}, o.recipients...), o.passwordRecipients...) {
			if _, _, err := namedFile(value); err != nil {
				return err
			}
		}
		stdinCount := 0
		if o.passwordFile == "-" {
			stdinCount++
		}
		if o.inputPasswordFile == "-" {
			stdinCount++
		}
		for _, value := range append(append([]string{}, o.passwordFor...), o.passwordForSeed...) {
			_, file, _ := namedFile(value)
			if file == "-" {
				stdinCount++
			}
		}
		for _, value := range o.passwordRecipients {
			_, file, _ := namedFile(value)
			if file == "-" {
				stdinCount++
			}
		}
		if stdinCount > 1 {
			return usage("标准输入只能供一个接收者读取口令")
		}
		for _, value := range o.recipients {
			_, file, _ := namedFile(value)
			if file == "-" {
				return usage("证书接收者必须使用文件路径")
			}
		}
		seen := map[string]bool{}
		for _, name := range o.entries {
			if o.appendLayer && name == spec.EncryptionEntryPoint {
				return usage("追加层必须保留明文 Encryptions.xml")
			}
			if core.ValidateEntryName(name) != nil || strings.Contains(name, ":") || seen[name] {
				return usage("--entry 必须为唯一的规范 ZIP 相对路径")
			}
			seen[name] = true
		}
		if o.keepRoot {
			for _, name := range o.entries {
				if name == spec.RootDocument {
					return usage("--keep-root 与 --entry OFD.xml 互斥")
				}
			}
		}
	}
	return validatePaths(o, credentialPaths(o))
}

func namedFile(value string) (string, string, error) {
	name, file, ok := strings.Cut(value, "=")
	if !ok || name == "" || file == "" {
		return "", "", usage("接收者格式必须为 NAME=文件路径")
	}
	return name, file, nil
}

func execute(ctx context.Context, o *options, stdin io.Reader, stdout io.Writer) error {
	if o.action == "inspect" && !hasCredentials(o) {
		info, err := crypt.Inspect(ctx, o.input, crypt.InspectOptions{Mode: crypt.Mode(o.mode), Limits: o.limits})
		if err != nil {
			return err
		}
		if o.output == "" || o.output == "-" {
			return render(stdout, info, o.format)
		}
		return atomicOutput(ctx, o, func(w io.Writer) error { return render(w, info, o.format) })
	}
	credentials, err := loadCredentials(ctx, o, stdin)
	if err != nil {
		return err
	}
	defer credentials.clear()
	options := decryptOptions(o, credentials)
	if o.action == "inspect" {
		view, err := crypt.Open(ctx, o.input, crypt.OpenOptions{DecryptOptions: options, AllowPartial: true})
		if err != nil {
			return err
		}
		defer view.Close()
		info := view.EncryptionInfo()
		if o.output == "" || o.output == "-" {
			return render(stdout, info, o.format)
		}
		return atomicOutput(ctx, o, func(w io.Writer) error { return render(w, info, o.format) })
	}
	var result any
	err = atomicOutput(ctx, o, func(w io.Writer) error {
		if o.action == "encrypt" {
			opt := crypt.EncryptOptions{Limits: o.limits, Recipients: credentials.recipients, Entries: o.entries, KeepRoot: o.keepRoot, PlaintextMap: o.plaintextMap, IndependentKeys: o.independentKeys}
			if o.appendLayer {
				opt.Append = &options
			}
			if len(o.pageIDs) > 0 {
				opt.Pages = &crypt.PageSelection{Document: o.pageDocument, IDs: o.pageIDs}
			}
			result, err = crypt.Encrypt(ctx, o.input, w, opt)
		} else {
			result, err = crypt.Decrypt(ctx, o.input, w, options)
		}
		return err
	})
	if err != nil {
		return err
	}
	return render(stdout, result, o.format)
}

func render(w io.Writer, value any, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	}
	line := func(pattern string, values ...any) error { _, err := fmt.Fprintf(w, pattern, values...); return err }
	if format == "markdown" {
		if err := line("# OFD 密码处理报告\n\n"); err != nil {
			return err
		}
	}
	switch report := value.(type) {
	case *crypt.Info:
		state := "未发现根目录加密入口"
		if report.Encrypted {
			state = "已加密且未解锁"
			if report.State == "partially_unlocked" {
				state = "部分解锁"
			}
			if report.State == "unlocked" {
				state = "已解锁"
			}
		}
		if err := line("状态：%s\n", state); err != nil {
			return err
		}
		for _, op := range report.Operations {
			if err := line("操作：%s  方案：%s  映射表：%s\n", op.ID, op.Seed.Scheme, op.MapState); err != nil {
				return err
			}
			for _, user := range op.Seed.Recipients {
				if err := line("接收者：%s（%s）\n", user.UserName, user.UserType); err != nil {
					return err
				}
			}
		}
		for _, d := range report.Diagnostics {
			if err := line("诊断：%s：%s\n", d.Code, d.Message); err != nil {
				return err
			}
		}
	case *crypt.EncryptReport:
		if err := line("状态：已加密\n操作：%s  方案：%s  加密条目：%d  输出条目：%d  输出字节：%d\n", report.OperationID, report.Scheme, report.EncryptedEntries, report.OutputEntries, report.OutputBytes); err != nil {
			return err
		}
		if report.Relative != "" {
			if err := line("前次操作：%s\n", report.Relative); err != nil {
				return err
			}
		}
		if report.IndependentKeys {
			if err := line("条目密钥：独立随机 FEK/IV\n"); err != nil {
				return err
			}
		}
		if report.SharedResources {
			if err := line("页面范围包含资源文件整体，共享资源可能同时锁定其他页面\n"); err != nil {
				return err
			}
		}
		for _, name := range report.SelectedEntries {
			if err := line("选中条目：%s\n", name); err != nil {
				return err
			}
		}
	case *crypt.DecryptReport:
		if err := line("状态：已解锁\n操作：%s  方案：%s  恢复条目：%d  输出条目：%d  输出字节：%d\n", report.OperationID, report.Scheme, report.DecryptedEntries, report.OutputEntries, report.OutputBytes); err != nil {
			return err
		}
		if len(report.Operations) > 1 {
			for _, op := range report.Operations {
				if err := line("操作：%s  状态：%s  恢复条目：%d\n", op.ID, op.State, op.DecryptedEntries); err != nil {
					return err
				}
			}
		}
		for _, d := range report.Diagnostics {
			if err := line("诊断：%s：%s\n", d.Code, d.Message); err != nil {
				return err
			}
		}
	}
	return nil
}
