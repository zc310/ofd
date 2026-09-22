// Package sign 通过外部命令为 OFD 文档追加签名。
//
// 签名在包内容最终确定后追加：本包为每个文档体计算被引用文件的摘要并生成
// Signature.xml（SignedInfo），把该文件字节通过标准输入交给外部命令，命令在
// 标准输出返回 SignedValue.dat 字节，本包再写回 Signatures.xml 与 OFD.xml。
// 签名算法与私钥由外部命令负责，本包不接触密钥。
package sign

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/beevik/etree"
	"github.com/emmansun/gmsm/sm3"
	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/internal/spec"
)

const (
	// DefaultSignatureMethod 是 SM2 + SM3 的签名算法 OID。
	DefaultSignatureMethod = "1.2.156.10197.1.501"
	// DefaultCheckMethod 是默认摘要算法。
	DefaultCheckMethod = "SM3"
)

// Options 控制外部命令签名。
type Options struct {
	// Command 是外部签名命令，按空白拆分参数，不支持引号或 shell 语法。
	// 命令从标准输入读取 Signature.xml 字节，向标准输出写 SignedValue.dat 字节。
	Command string
	// ID 是签名标识，空值时使用 "1"。
	ID string
	// ProviderName、ProviderVersion、Company 写入 SignedInfo 的 Provider。
	ProviderName    string
	ProviderVersion string
	Company         string
	// SignatureMethod 是签名算法 OID，空值时使用 DefaultSignatureMethod。
	SignatureMethod string
	// CheckMethod 是摘要算法，空值时使用 DefaultCheckMethod。
	CheckMethod string
	// Date 是签名时间，零值时使用当前时间。
	Date time.Time
	// Environment 是传递给外部命令的额外环境变量，格式为 KEY=VALUE。
	Environment []string
}

type entry struct {
	name   string
	data   []byte
	method uint16
}

// Sign 读取 input（文件路径、字节数据或 io.Reader），为每个文档体追加签名后写入 w。
func Sign(input any, w io.Writer, options Options) error {
	if strings.TrimSpace(options.Command) == "" {
		return errors.New("外部签名命令为空")
	}
	if w == nil {
		return errors.New("签名输出写入器为空")
	}
	pkg, err := openPackage(input)
	if err != nil {
		return err
	}
	defer func() { _ = pkg.Close() }()

	entries, err := readEntries(pkg)
	if err != nil {
		return err
	}
	index := make(map[string][]byte, len(entries))
	for _, item := range entries {
		index[item.name] = item.data
	}
	rootData, ok := index[spec.RootDocument]
	if !ok {
		return fmt.Errorf("缺少 %s", spec.RootDocument)
	}
	document := etree.NewDocument()
	if err := document.ReadFromBytes(rootData); err != nil {
		return fmt.Errorf("解析 %s 失败: %w", spec.RootDocument, err)
	}
	root := document.Root()
	if root == nil || localName(root) != "OFD" {
		return fmt.Errorf("%s 根元素无效", spec.RootDocument)
	}
	bodies := directChildren(root, "DocBody")
	if len(bodies) == 0 {
		return errors.New("OFD.xml 中没有 DocBody")
	}

	id := strings.TrimSpace(options.ID)
	if id == "" {
		id = "sign-1"
	}
	signatureMethod := strings.TrimSpace(options.SignatureMethod)
	if signatureMethod == "" {
		signatureMethod = DefaultSignatureMethod
	}
	checkMethod := strings.TrimSpace(options.CheckMethod)
	if checkMethod == "" {
		checkMethod = DefaultCheckMethod
	}
	date := options.Date
	if date.IsZero() {
		date = time.Now()
	}

	var added []entry
	for _, body := range bodies {
		docDir, err := docDirOf(body)
		if err != nil {
			return err
		}
		signatureDir := path.Join(docDir, "Signatures")
		references := make([]string, 0)
		for _, item := range entries {
			if item.name == path.Join(docDir, "Signatures.xml") {
				continue
			}
			if item.name == signatureDir || strings.HasPrefix(item.name, signatureDir+"/") {
				continue
			}
			if !strings.HasPrefix(item.name, docDir+"/") {
				continue
			}
			references = append(references, path.Join("..", strings.TrimPrefix(item.name, docDir+"/")))
		}
		sort.Strings(references)
		if len(references) == 0 {
			return fmt.Errorf("%s 没有可签名的文件", docDir)
		}

		baseName := "Signature_" + id + ".xml"
		signatureXML, err := buildSignatureXML(signatureXMLInput{
			id:              id,
			providerName:    options.ProviderName,
			providerVersion: options.ProviderVersion,
			company:         options.Company,
			signatureMethod: signatureMethod,
			checkMethod:     checkMethod,
			date:            date,
			signedValue:     "Data/" + id + ".dat",
			references:      references,
			files:           index,
			signatureBase:   signatureDir,
		})
		if err != nil {
			return err
		}

		signedValue, err := runCommand(options.Command, signatureXML, map[string]string{
			"OFD_SIGN_DOCUMENT":         docDir,
			"OFD_SIGN_ID":               id,
			"OFD_SIGN_PROVIDER":         options.ProviderName,
			"OFD_SIGN_PROVIDER_VERSION": options.ProviderVersion,
			"OFD_SIGN_COMPANY":          options.Company,
			"OFD_SIGN_SIGNATURE_METHOD": signatureMethod,
			"OFD_SIGN_CHECK_METHOD":     checkMethod,
			"OFD_SIGN_TIME":             date.Format(time.RFC3339),
		}, options.Environment)
		if err != nil {
			return err
		}
		added = append(added,
			entry{name: path.Join(signatureDir, baseName), data: signatureXML, method: zip.Deflate},
			entry{name: path.Join(docDir, "Signatures.xml"), data: buildSignaturesXML(id, baseName), method: zip.Deflate},
			entry{name: path.Join(signatureDir, "Data", id+".dat"), data: signedValue, method: zip.Store},
		)
		setBodySignatures(body, docDir+"/Signatures.xml")
	}

	updatedRoot, err := document.WriteToBytes()
	if err != nil {
		return fmt.Errorf("生成 %s 失败: %w", spec.RootDocument, err)
	}
	return writePackage(w, entries, added, updatedRoot)
}

type signatureXMLInput struct {
	id              string
	providerName    string
	providerVersion string
	company         string
	signatureMethod string
	checkMethod     string
	date            time.Time
	signedValue     string
	references      []string
	files           map[string][]byte
	signatureBase   string
}

func buildSignatureXML(input signatureXMLInput) ([]byte, error) {
	doc := etree.NewDocument()
	doc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)
	root := doc.CreateElement("Signature")
	root.CreateAttr("xmlns", spec.Namespace)
	info := root.CreateElement("SignedInfo")
	provider := info.CreateElement("Provider")
	provider.CreateAttr("ProviderName", input.providerName)
	if input.providerVersion != "" {
		provider.CreateAttr("Version", input.providerVersion)
	}
	if input.company != "" {
		provider.CreateAttr("Company", input.company)
	}
	info.CreateElement("SignatureMethod").SetText(input.signatureMethod)
	info.CreateElement("SignatureDateTime").SetText(input.date.Format(time.RFC3339))
	references := info.CreateElement("References")
	references.CreateAttr("CheckMethod", input.checkMethod)
	for _, reference := range input.references {
		target := path.Clean(path.Join(input.signatureBase, reference))
		data, ok := input.files[target]
		if !ok {
			return nil, fmt.Errorf("签名引用目标不存在: %s", target)
		}
		digest := sm3.Sum(data)
		element := references.CreateElement("Reference")
		element.CreateAttr("FileRef", reference)
		element.CreateElement("CheckValue").SetText(base64.StdEncoding.EncodeToString(digest[:]))
	}
	root.CreateElement("SignedValue").SetText(input.signedValue)
	out, err := doc.WriteToBytes()
	if err != nil {
		return nil, fmt.Errorf("生成 Signature.xml 失败: %w", err)
	}
	return out, nil
}

func buildSignaturesXML(id, baseName string) []byte {
	doc := etree.NewDocument()
	doc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)
	root := doc.CreateElement("Signatures")
	root.CreateAttr("xmlns", spec.Namespace)
	root.CreateElement("MaxSignId").SetText("MaxSignId-1")
	element := root.CreateElement("Signature")
	element.CreateAttr("ID", id)
	element.CreateAttr("Type", "Seal")
	element.CreateAttr("BaseLoc", "Signatures/"+baseName)
	out, _ := doc.WriteToBytes()
	return out
}

func runCommand(command string, stdin []byte, environment map[string]string, extra []string) ([]byte, error) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil, errors.New("外部签名命令为空")
	}
	cmd := exec.Command(fields[0], fields[1:]...)
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Env = os.Environ()
	for key, value := range environment {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Env = append(cmd.Env, extra...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message != "" {
			return nil, fmt.Errorf("外部签名命令失败: %w: %s", err, message)
		}
		return nil, fmt.Errorf("外部签名命令失败: %w", err)
	}
	if stdout.Len() == 0 {
		return nil, errors.New("外部签名命令没有输出签名值")
	}
	return stdout.Bytes(), nil
}

func writePackage(w io.Writer, entries, added []entry, rootData []byte) error {
	archive := zip.NewWriter(w)
	seen := make(map[string]bool)
	write := func(name string, data []byte, method uint16) error {
		if err := core.ValidateEntryName(name); err != nil {
			return err
		}
		if seen[name] {
			return fmt.Errorf("OFD 包条目路径重复: %s", name)
		}
		seen[name] = true
		header := &zip.FileHeader{Name: name, Method: method}
		file, err := archive.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("创建 ZIP 条目 %q 失败: %w", name, err)
		}
		if _, err := file.Write(data); err != nil {
			return fmt.Errorf("写入 ZIP 条目 %q 失败: %w", name, err)
		}
		return nil
	}
	for _, item := range entries {
		if item.name == spec.RootDocument {
			continue
		}
		if strings.HasSuffix(item.name, "/Signatures.xml") || strings.Contains(item.name, "/Signatures/") {
			continue
		}
		if err := write(item.name, item.data, item.method); err != nil {
			return err
		}
	}
	for _, item := range added {
		if err := write(item.name, item.data, item.method); err != nil {
			return err
		}
	}
	if err := write(spec.RootDocument, rootData, zip.Deflate); err != nil {
		return err
	}
	if err := archive.Close(); err != nil {
		return fmt.Errorf("关闭 OFD ZIP 包失败: %w", err)
	}
	return nil
}

func openPackage(input any) (*core.Package, error) {
	switch value := input.(type) {
	case string:
		return core.OpenFile(value)
	case []byte:
		return core.OpenBytes(value)
	case io.Reader:
		return core.OpenReader(value)
	default:
		return nil, fmt.Errorf("不支持的输入类型: %T", input)
	}
}

func readEntries(pkg *core.Package) ([]entry, error) {
	result := make([]entry, 0)
	for _, info := range pkg.Entries() {
		if info.IsDir {
			continue
		}
		data, err := pkg.Read(info.Path)
		if err != nil {
			return nil, fmt.Errorf("读取 %s 失败: %w", info.Path, err)
		}
		result = append(result, entry{name: info.Path, data: data, method: info.Method})
	}
	return result, nil
}

func docDirOf(body *etree.Element) (string, error) {
	child := directChild(body, "DocRoot")
	if child == nil {
		return "", errors.New("DocBody 缺少 DocRoot")
	}
	value := strings.TrimSpace(child.Text())
	if value == "" {
		return "", errors.New("DocRoot 为空")
	}
	dir := path.Dir(path.Clean(strings.TrimPrefix(value, "/")))
	if dir == "." || dir == "" || strings.HasPrefix(dir, "../") {
		return "", fmt.Errorf("DocRoot 目录无效: %q", value)
	}
	return dir, nil
}

func setBodySignatures(body *etree.Element, value string) {
	prefix := ""
	if body.Space != "" {
		prefix = body.Space + ":"
	}
	if child := directChild(body, "Signatures"); child != nil {
		child.SetText(value)
		return
	}
	body.CreateElement(prefix + "Signatures").SetText(value)
}

func localName(element *etree.Element) string {
	if element == nil {
		return ""
	}
	if index := strings.Index(element.Tag, ":"); index >= 0 {
		return element.Tag[index+1:]
	}
	return element.Tag
}

func directChildren(element *etree.Element, local string) []*etree.Element {
	var result []*etree.Element
	for _, child := range element.ChildElements() {
		if localName(child) == local {
			result = append(result, child)
		}
	}
	return result
}

func directChild(element *etree.Element, local string) *etree.Element {
	children := directChildren(element, local)
	if len(children) == 0 {
		return nil
	}
	return children[0]
}
