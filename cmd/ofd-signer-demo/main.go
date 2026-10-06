// Command ofd-signer-demo 是 github.com/zc310/ofd 提供的演示签名器，
// 用于演示 ofd-creator merge --sign-cmd 协议与 SES 结构。
//
// 它从标准输入读取 ofd-creator 生成的 Signature.xml，用内存中的 SM2 自签名
// 证书分别签署 SES 印章信息和外层 TBS_Sign，向标准输出写出 SignedValue.dat。
// 证书、私钥和印章图片都在进程内生成，仅用于演示与测试，不能用于生产环境，
// 也不会建立可验证的信任链。
//
// 实际的 SES 数据结构与组装逻辑在 internal/ses 包中；本程序只提供参数化的
// 演示封装，并复用内嵌的占位印章图片。
package main

import (
	"crypto/ecdsa"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/emmansun/gmsm/sm2"
	"github.com/zc310/ofd/internal/ses"
)

// 以下标识写入 SES_Seal_Info 和外部命令环境变量，用于说明签名由本演示程序制作。
const (
	// producer 是签名生产者的包路径。
	producer = "github.com/zc310/ofd"
	// demoProvider 是默认的印章与证书主体名称。
	demoProvider = "ofd-signer-demo"
	// demoVersion 是演示签名器版本。
	demoVersion = "0.1.0"
)

const (
	// pictureType 是 SES_ESPictrueInfo.Type，演示印章使用 PNG。
	pictureType = "png"

	// headerID 是 SES_Header.ID 的固定值，GM/T 0031 规定为 "ES"。
	headerID = ses.HeaderID
	// sealVersion 是 SES 结构版本号；演示器使用与 OFD 规范配套的 V4。
	sealVersion = ses.SealVersion
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v", "--version":
			_, _ = fmt.Fprintf(os.Stdout, "%s %s (%s)\n", demoProvider, demoVersion, producer)
			return nil
		case "-h", "--help":
			printUsage()
			return nil
		default:
			return fmt.Errorf("未知参数 %q；本命令从标准输入读取 Signature.xml", os.Args[1])
		}
	}
	return writeSignedValue(os.Stdout, os.Stdin)
}

func printUsage() {
	_, _ = fmt.Fprintf(os.Stdout, `%s %s - %s 演示签名器

用法：ofd-signer-demo < Signature.xml > SignedValue.dat

从标准输入读取 ofd-creator 生成的 Signature.xml，向标准输出写出 SignedValue.dat。
本程序仅用于演示与测试，运行时现场生成 SM2 自签名证书，不建立可验证的信任链。

选项：
  -v, --version  输出版本与项目来源
  -h, --help     输出本帮助

环境变量：OFD_SIGN_ID、OFD_SIGN_TIME 可覆盖签名标识与签名时间。
`, demoProvider, demoVersion, producer)
}

// writeSignedValue 从 reader 读取 Signature.xml，向 writer 写出 SignedValue.dat。
func writeSignedValue(writer io.Writer, reader io.Reader) error {
	signatureXML, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("读取 Signature.xml 失败: %w", err)
	}
	signedValue, err := sign(signatureXML)
	if err != nil {
		return err
	}
	if _, err := writer.Write(signedValue); err != nil {
		return fmt.Errorf("写出 SignedValue.dat 失败: %w", err)
	}
	return nil
}

func sign(signatureXML []byte) ([]byte, error) {
	now := time.Now()
	if value := os.Getenv("OFD_SIGN_TIME"); value != "" {
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			now = parsed
		}
	}
	id := os.Getenv("OFD_SIGN_ID")
	if id == "" {
		id = "sign-1"
	}

	publicKey, privateKey, certificateDER, err := newCertificate(now)
	if err != nil {
		return nil, err
	}
	_ = publicKey

	picture, err := placeholderSeal()
	if err != nil {
		return nil, err
	}
	sealWidth, sealHeight, err := pictureSizeOf(picture)
	if err != nil {
		return nil, err
	}
	seal, err := ses.BuildSeal(ses.SealParams{
		Provider:    demoProvider + "/" + demoVersion,
		ESID:        demoProvider + "-" + id + "@" + producer,
		Name:        demoProvider + " " + demoVersion + " (演示印章, " + producer + ")",
		PictureType: pictureType,
		PictureData: picture,
		Width:       sealWidth,
		Height:      sealHeight,
	}, certificateDER, privateKey, now)
	if err != nil {
		return nil, err
	}
	return ses.BuildSignedValue(signatureXML, seal, certificateDER, privateKey, signaturePath(id), now)
}

// signaturePath 返回 TBS_Sign.PropertyInfo 使用的签名 XML 包内路径。
// 真实印章这里是签名 XML 相对文档根的可解析路径；演示器沿用同一语义。
func signaturePath(id string) string {
	document := os.Getenv("OFD_SIGN_DOCUMENT")
	if document == "" {
		document = "Doc_0"
	}
	return "/" + document + "/Signatures/Signature_" + id + ".xml"
}

func newCertificate(now time.Time) (*ecdsa.PublicKey, *sm2.PrivateKey, []byte, error) {
	key, publicKey, certificateDER, err := ses.NewSelfSignedCertificate(demoProvider+" ("+producer+")", "OFD Signer Demo", now)
	if err != nil {
		return nil, nil, nil, err
	}
	return publicKey, key, certificateDER, nil
}
