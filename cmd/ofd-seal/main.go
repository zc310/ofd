// Command ofd-seal 生成符合 GB/T 38540-2020 的 SES 电子印章文件（Seal.esl）。
//
// 只负责封装格式：接收印章图片和元数据，生成一份 DER 编码的 SES_Seal 文件。
// 生成的文件由签名管线（--sign-seal）引用，写入 Signature.xml 的 Seal 元素。
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emmansun/gmsm/sm2"
	"github.com/spf13/cobra"
	"github.com/zc310/ofd/internal/sealimg"
	"github.com/zc310/ofd/internal/ses"
	"github.com/zc310/ofd/internal/version"
)

const (
	exitOK     = 0
	exitFailed = 1
	exitUsage  = 2
)

type options struct {
	image    string
	name     string
	esid     string
	vid      string
	out      string
	key      string
	cert     string
	keyOut   string
	validity int
	// generate 只产出印章图片，不封装成 Seal.esl。
	generate bool
	size     int
	shape    string
	fontName string
	version  bool
	help     bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseArgs(args, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-seal:", err)
		return exitUsage
	}
	if opts.help {
		return exitOK
	}
	if opts.version {
		_, _ = fmt.Fprintln(stdout, version.Version)
		return exitOK
	}
	if err := execute(opts, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "ofd-seal:", err)
		return exitFailed
	}
	return exitOK
}

func parseArgs(args []string, output io.Writer) (*options, error) {
	opts := &options{}
	root := &cobra.Command{
		Use:           "ofd-seal [flags]",
		Short:         "生成 GB/T 38540 SES 电子印章文件",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetOut(output)
	root.SetErr(output)
	root.Flags().StringVarP(&opts.image, "image", "i", "", "印章图片路径（png/jpeg/gif/bmp），必填")
	root.Flags().StringVar(&opts.name, "name", "", "印章名称；默认使用图片文件名")
	root.Flags().StringVar(&opts.esid, "esid", "", "印章唯一编号（ia5 字符串）；默认 name@vid")
	root.Flags().StringVar(&opts.vid, "vid", "ofd-seal", "印章商标识（VID）")
	root.Flags().StringVarP(&opts.out, "out", "o", "seal.esl", "输出文件路径")
	root.Flags().StringVar(&opts.key, "key", "", "复用已有 SM2 私钥（PKCS#8 PEM 或 DER）；需同时提供 --cert")
	root.Flags().StringVar(&opts.cert, "cert", "", "复用证书（PEM 或 DER）；默认自签生成")
	root.Flags().StringVar(&opts.keyOut, "key-out", "", "新生成的私钥以 PKCS#8 PEM 写到该路径")
	root.Flags().IntVar(&opts.validity, "validity-days", 365*10, "证书与印章的有效天数")
	root.Flags().BoolVar(&opts.generate, "generate", false, "只生成测试用印章图片，不封装成 Seal.esl")
	root.Flags().IntVar(&opts.size, "size", 512, "--generate 的输出像素（长边）")
	root.Flags().StringVar(&opts.shape, "shape", "circle", "--generate 的外框形状：circle 或 ellipse")
	root.Flags().StringVar(&opts.fontName, "font", "", "--generate 的字体家族名，默认按候选顺序找中文字体")
	root.Flags().BoolVar(&opts.version, "version", false, "输出工具版本")
	root.Flags().BoolVarP(&opts.help, "help", "h", false, "显示帮助")
	if err := root.ParseFlags(args); err != nil {
		return nil, err
	}
	if opts.help {
		_, _ = fmt.Fprintln(output, root.UsageString())
		return opts, nil
	}
	if opts.version {
		return opts, nil
	}
	if opts.generate {
		if err := validateGenerateOptions(opts); err != nil {
			return nil, err
		}
		return opts, nil
	}
	if opts.image == "" {
		return nil, errors.New("必须通过 --image 指定印章图片；或用 --generate 直接生成测试印章图片")
	}
	if (opts.key == "") != (opts.cert == "") {
		return nil, errors.New("--key 与 --cert 必须同时提供，或都不提供")
	}
	if opts.validity <= 0 {
		return nil, fmt.Errorf("--validity-days 必须为正数，实际 %d", opts.validity)
	}
	if opts.esid != "" && !ses.IsIA5(opts.esid) {
		return nil, fmt.Errorf("--esid 必须是 IA5 字符串，实际 %q", opts.esid)
	}
	if opts.keyOut != "" && opts.key != "" {
		return nil, errors.New("--key-out 只对新生成的私钥有效；复用 --key 时不需要")
	}
	return opts, nil
}

// validateGenerateOptions 校验 --generate 的参数组合。
func validateGenerateOptions(opts *options) error {
	switch strings.ToLower(strings.TrimSpace(opts.shape)) {
	case "", "circle", "圆形":
	case "ellipse", "椭圆":
	default:
		return fmt.Errorf("--shape 只支持 circle 或 ellipse，实际 %q", opts.shape)
	}
	if opts.size < 32 || opts.size > 4096 {
		return fmt.Errorf("--size 应在 32-4096 之间，实际 %d", opts.size)
	}
	if opts.image != "" {
		return errors.New("--generate 与 --image 互斥：--generate 自己产图")
	}
	return nil
}

// generatePicture 渲染测试用印章图片。
func generatePicture(opts *options, stdout io.Writer) error {
	shape := sealimg.ShapeCircle
	if strings.EqualFold(strings.TrimSpace(opts.shape), "ellipse") {
		shape = sealimg.ShapeEllipse
	}
	picture := sealimg.Options{FontName: opts.fontName, Shape: shape}
	if shape == sealimg.ShapeEllipse {
		picture.Width, picture.Height = opts.size, opts.size*2/3
	} else {
		picture.Width, picture.Height = opts.size, opts.size
	}
	data, err := sealimg.RenderPNG(picture)
	if err != nil {
		return err
	}
	if err := os.WriteFile(opts.out, data, 0o644); err != nil {
		return fmt.Errorf("写入印章图片失败: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "已写出测试印章图片 %s（%dx%d，%s）\n", opts.out,
		picture.Width, picture.Height, shapeName(shape))
	_, _ = fmt.Fprintln(stdout, "注意：该图片仅供演示与测试，底部印有「非正式印章」字样。")
	return nil
}

func shapeName(shape sealimg.Shape) string {
	if shape == sealimg.ShapeEllipse {
		return "椭圆"
	}
	return "圆形"
}

func execute(opts *options, stdout io.Writer) error {
	if opts.generate {
		return generatePicture(opts, stdout)
	}
	data, err := os.ReadFile(opts.image)
	if err != nil {
		return fmt.Errorf("读取印章图片失败: %w", err)
	}
	typeName, width, height, err := ses.PictureTypeAndSize(data)
	if err != nil {
		return err
	}
	now := time.Now().UTC()

	name := strings.TrimSpace(opts.name)
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(opts.image), filepath.Ext(opts.image))
	}
	vid := strings.TrimSpace(opts.vid)
	if vid == "" {
		vid = "ofd-seal"
	}
	esid := strings.TrimSpace(opts.esid)
	if esid == "" {
		esid = name + "@" + vid
	}
	if !ses.IsIA5(esid) {
		return fmt.Errorf("印章编号必须是 IA5 字符串，实际 %q", esid)
	}

	var certDER []byte
	var key *sm2.PrivateKey
	if opts.key != "" {
		keyData, err := os.ReadFile(opts.key)
		if err != nil {
			return fmt.Errorf("读取私钥失败: %w", err)
		}
		keyValue, err := ses.ParsePrivateKey(keyData)
		if err != nil {
			return err
		}
		key = keyValue
		certData, err := os.ReadFile(opts.cert)
		if err != nil {
			return fmt.Errorf("读取证书失败: %w", err)
		}
		certDER, err = ses.ParseCertificateDER(certData)
		if err != nil {
			return fmt.Errorf("解析证书失败: %w", err)
		}
	} else {
		generatedKey, _, generatedCert, err := ses.NewSelfSignedCertificate(name, "ofd-seal", now)
		if err != nil {
			return err
		}
		key = generatedKey
		certDER = generatedCert
		if opts.keyOut != "" {
			pemData, err := ses.MarshalPrivateKeyPEM(key)
			if err != nil {
				return err
			}
			if err := os.WriteFile(opts.keyOut, pemData, 0o600); err != nil {
				return fmt.Errorf("写入私钥失败: %w", err)
			}
		}
	}

	params := ses.SealParams{
		Provider:    vid,
		ESID:        esid,
		Name:        name,
		PictureType: typeName,
		PictureData: data,
		Width:       width,
		Height:      height,
		ValidFrom:   now,
		ValidTo:     now.Add(time.Duration(opts.validity) * 24 * time.Hour),
	}
	seal, err := ses.BuildSeal(params, certDER, key, now)
	if err != nil {
		return err
	}
	der, err := seal.MarshalDER()
	if err != nil {
		return err
	}
	if err := os.WriteFile(opts.out, der, 0o644); err != nil {
		return fmt.Errorf("写入印章文件失败: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "印章: %s (%s)\n", name, esid)
	_, _ = fmt.Fprintf(stdout, "图片: %s %dx%d\n", typeName, width, height)
	_, _ = fmt.Fprintf(stdout, "已写出 %s\n", opts.out)
	return nil
}
