// Command ofd-signer-demo 是 github.com/zc310/ofd 提供的演示签名器，
// 用于演示 ofd-creator merge --sign-cmd 协议与 SES 结构。
//
// 它从标准输入读取 ofd-creator 生成的 Signature.xml，用内存中的 SM2 自签名
// 证书分别签署 SES 印章信息和外层 TBS_Sign，向标准输出写出 SignedValue.dat。
// 证书、私钥和印章图片都在进程内生成，仅用于演示与测试，不能用于生产环境，
// 也不会建立可验证的信任链。
package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/asn1"
	"fmt"
	"io"
	"math/big"
	"os"
	"time"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/sm3"
	gmx509 "github.com/emmansun/gmsm/smx509"
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
	sm2WithSM3OID = "1.2.156.10197.1.501"

	// pictureType 是 SES_ESPictrueInfo.Type，演示印章使用 SVG。
	pictureType   = "svg"
	pictureWidth  = 40
	pictureHeight = 40

	// headerID 是 SES_Header.ID 的固定值，GM/T 0031 规定为 "ES"。
	headerID = "ES"
	// sealVersion 是 SES 结构版本号；演示器使用与 OFD 规范配套的 V4。
	sealVersion = 4
)

var sealPropertyType = asn1.ObjectIdentifier{1, 2, 156, 10197, 1, 1024}

type sesSignature struct {
	TBS                tbsSign
	Certificate        []byte
	SignatureAlgorithm asn1.ObjectIdentifier
	Signature          asn1.BitString
}

type tbsSign struct {
	Version      int
	Seal         sesSeal
	SignTime     time.Time `asn1:"generalized"`
	DataHash     asn1.BitString
	PropertyInfo string `asn1:"ia5"`
}

type sesSeal struct {
	SealInfo           sesSealInfo
	Certificate        []byte
	SignatureAlgorithm asn1.ObjectIdentifier
	Signature          asn1.BitString
}

type sesSealInfo struct {
	Header   sesHeader
	ESID     string `asn1:"ia5"`
	Property sesProperty
	Picture  sesPicture
}

type sesHeader struct {
	ID      string `asn1:"ia5"`
	Version int
	VID     string `asn1:"ia5"`
}

type sesProperty struct {
	Type            int
	Name            string `asn1:"utf8"`
	CertificateType int
	CertList        [][]byte
	CreateTime      time.Time `asn1:"generalized"`
	ValidFrom       time.Time `asn1:"generalized"`
	ValidTo         time.Time `asn1:"generalized"`
}

type sesPicture struct {
	Type   string `asn1:"ia5"`
	Data   []byte
	Width  int
	Height int
}

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

	sealInfo := sesSealInfo{
		Header: sesHeader{ID: headerID, Version: sealVersion, VID: demoProvider + "/" + demoVersion},
		ESID:   demoProvider + "-" + id + "@" + producer,
		Property: sesProperty{
			Type:            1,
			Name:            demoProvider + " " + demoVersion + " (演示印章, " + producer + ")",
			CertificateType: 1,
			CertList:        [][]byte{certificateDER},
			CreateTime:      now,
			ValidFrom:       now,
			ValidTo:         now.AddDate(10, 0, 0),
		},
		Picture: sesPicture{Type: pictureType, Data: placeholderSeal(), Width: pictureWidth, Height: pictureHeight},
	}
	sealInfoDER, err := asn1.Marshal(sealInfo)
	if err != nil {
		return nil, fmt.Errorf("编码 SES_Seal_Info 失败: %w", err)
	}
	sealSignature, err := signSM2(publicKey, privateKey, sealInfoDER)
	if err != nil {
		return nil, fmt.Errorf("签署印章信息失败: %w", err)
	}
	seal := sesSeal{
		SealInfo:           sealInfo,
		Certificate:        certificateDER,
		SignatureAlgorithm: sm2WithSM3OIDValue(),
		Signature:          sealSignature,
	}

	dataHash := sm3.Sum(signatureXML)
	tbs := tbsSign{
		Version:      sealVersion,
		Seal:         seal,
		SignTime:     now,
		DataHash:     asn1.BitString{Bytes: dataHash[:], BitLength: len(dataHash) * 8},
		PropertyInfo: signaturePath(id),
	}
	tbsDER, err := asn1.Marshal(tbs)
	if err != nil {
		return nil, fmt.Errorf("编码 TBS_Sign 失败: %w", err)
	}
	outerSignature, err := signSM2(publicKey, privateKey, tbsDER)
	if err != nil {
		return nil, fmt.Errorf("签署 TBS_Sign 失败: %w", err)
	}

	signedValue, err := asn1.Marshal(sesSignature{
		TBS:                tbs,
		Certificate:        certificateDER,
		SignatureAlgorithm: sm2WithSM3OIDValue(),
		Signature:          outerSignature,
	})
	if err != nil {
		return nil, fmt.Errorf("编码 SignedValue.dat 失败: %w", err)
	}
	return signedValue, nil
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

func signSM2(publicKey *ecdsa.PublicKey, privateKey *sm2.PrivateKey, data []byte) (asn1.BitString, error) {
	digest, err := sm2.CalculateSM2Hash(publicKey, data, nil)
	if err != nil {
		return asn1.BitString{}, err
	}
	r, s, err := sm2.Sign(rand.Reader, &privateKey.PrivateKey, digest)
	if err != nil {
		return asn1.BitString{}, err
	}
	signature, err := asn1.Marshal(struct {
		R *big.Int
		S *big.Int
	}{R: r, S: s})
	if err != nil {
		return asn1.BitString{}, err
	}
	return asn1.BitString{Bytes: signature, BitLength: len(signature) * 8}, nil
}

func newCertificate(now time.Time) (*ecdsa.PublicKey, *sm2.PrivateKey, []byte, error) {
	privateKey, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("生成 SM2 私钥失败: %w", err)
	}
	template := &gmx509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkixName(demoProvider + " (" + producer + ")"),
		Issuer:                pkixName(demoProvider + " (" + producer + ")"),
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage:              gmx509.KeyUsageDigitalSignature | gmx509.KeyUsageCertSign,
		SignatureAlgorithm:    gmx509.SM2WithSM3,
	}
	certificateDER, err := gmx509.CreateCertificate(rand.Reader, template, template, publicKeyOf(privateKey), privateKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("签发演示证书失败: %w", err)
	}
	return publicKeyOf(privateKey), privateKey, certificateDER, nil
}
