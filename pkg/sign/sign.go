// Package sign 提供 OFD 外部命令签署、SM2/SM3 SES 生成和完整签章验证。
//
// 签名在包内容最终确定后追加：本包为每个文档体计算被引用文件的摘要并生成
// Signature.xml（SignedInfo），把该文件字节通过标准输入交给外部命令，命令在
// 标准输出返回 SignedValue.dat 字节，本包再写回 Signatures.xml 与 OFD.xml。
// 签名算法与私钥由外部命令负责，本包不接触密钥。
package sign

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"hash"
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
	protocol "github.com/zc310/ofd/internal/crypt"
	"github.com/zc310/ofd/internal/physicalsig"
	"github.com/zc310/ofd/internal/securexml"
	"github.com/zc310/ofd/internal/signeddata"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/spec"
)

const (
	// DefaultSignatureMethod 是 SM2 + SM3 的签名算法 OID。
	DefaultSignatureMethod = "1.2.156.10197.1.501"
	// DefaultCheckMethod 是默认摘要算法。
	DefaultCheckMethod = "SM3"
)

// Options 控制外部命令签名。
type Options struct {
	// Physical 对加密后的物理条目签署，要求明文 OFD.xml 且没有已有签名入口。
	// 仅支持普通签名，覆盖全部输入文件及最终入口/列表，不使用解密视图。
	Physical bool
	// Append 保留已有普通签名及原文，只更新签名列表并增加新签名。
	// 首次签署也可设置；追加模式缺省不摘要仍需更新的签名列表。
	Append bool
	// Relative 指定本次签名依赖的已有签名 ID，仅用于 Append。
	Relative string
	// LockSignatures 在追加模式下摘要最终签名列表，结束该列表的追加流程。
	LockSignatures bool
	// Sign 与 Command 二选一，适配进程或硬件服务；参数为完整 Signature.xml 字节。
	Sign func(context.Context, []byte) ([]byte, error)
	// Type 为 Seal（缺省，SES 电子签章）或 Sign（GB/T 35275 普通数字签名）。
	Type string
	// UID 用于普通数字签名返回值的 SM2 原文校验，空值使用标准缺省标识。
	UID             []byte
	ProtocolVersion string
	ExtendData      []byte
	Parameters      []Parameter
	// Command 是外部签名命令，按空白拆分参数，不支持引号或 shell 语法。
	// 命令从标准输入读取 Signature.xml 字节，向标准输出写 SignedValue.dat 字节。
	Command string
	// ID 是签名标识，空值时使用 "sign-1"；追加时自动分配未使用的 sign-N。
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
	// Deterministic 使用固定 ZIP 时间，生成可复现的签名结果。
	// 需要 Date 固定时配合设置，否则 Signature.xml 中的签名时间仍会变化。
	Deterministic bool
	// Stamp 控制是否在 Signature.xml 写入 StampAnnot，让阅读器把印章图片绘制到页面上。
	// 为 nil 时不写入 StampAnnot（只验签，不显示印章）。
	Stamp *StampOptions
	// StampSeam 在同一文档体的每个页面右边缘写入一条同一印章的裁片。
	// 所有标注引用同一份 Seal，按页面顺序拼回完整骑缝章。
	StampSeam *StampSeamOptions
	// Seal 指定独立的电子印章文件（DER 编码的 SES_Seal，如 .esl）。提供时
	// Signature.xml 会写入 <Seal BaseLoc="Seal.esl"/> 并把该文件一起写入包，
	// 阅读器优先从独立印章文件提取印章图。SignedValue.dat 仍内嵌印章，
	// 作为阅读器未识别 Seal 元素时的回退路径。
	Seal string
	// SealImage 写入附录 B 的 Seal/ImageLoc，并将图片原字节纳入引用摘要。
	SealImage string
	// References 控制文档文件范围；nil 时包含文档目录，普通签名还包含 OFD.xml。
	// 普通签名的替换模式或 LockSignatures 还保护最终列表；追加时始终保护旧签名文件和签名值。
	References *ReferenceOptions
	// Environment 是传递给外部命令的额外环境变量，格式为 KEY=VALUE。
	Environment []string
}

// Parameter 是 GM/T 0099 表 B.2 的扩展键值，不由库执行其中的内容。
type Parameter struct {
	Name, Type, Value string
}

// ReferenceOptions 选择 SignedInfo/References 覆盖的文件。
// Include/Exclude 是不区分大小写的 glob（相对文档体目录，如 "Pages/**"、"*.xml"），
// 为空表示包含全部文件。RootDocument 为真时把 OFD.xml 纳入引用。
type ReferenceOptions struct {
	Include      []string
	Exclude      []string
	RootDocument bool
}

// StampOptions 描述签章在页面上的位置。
type StampOptions struct {
	// PageRef 是 StampAnnot@PageRef 引用的页面 ID；为空时使用首个文档体的首页。
	PageRef string
	// Boundary 是 StampAnnot@Boundary，格式为 "x y width height"；为空时在首页右下角放置默认大小印章。
	Boundary string
}

// StampSeamOptions 描述跨整个文档体页面序列的骑缝章。
// 每页按自身 Area/PhysicalBox 定位，缺失时回退文档公共 PageArea。
type StampSeamOptions struct {
	// Edge 是骑缝章贴靠的页面边缘，支持 "left"、"right"、"top" 与 "bottom"；
	// 空值默认为 "right"。
	Edge string
	// Pages 选择参与分片的页码，支持 "all"、"odd"、"even"；空值默认为 "all"。
	Pages string
	// Pieces 是印章拆分份数；零值按参与分片的页数计算，非零时必须与参与页数相等。
	Pieces int
	// GroupPages 是每组骑缝章覆盖的页数；零值表示所有参与页面共用一枚章。
	GroupPages int
	// Size 是印章边长，单位为毫米；零值使用默认值。
	Size float64
	// MinStrip 是每页裁片的最小宽度，单位为毫米；零值使用 2mm。
	MinStrip float64
	// X 是上下边缘时印章左边坐标，单位为毫米；负值按页面水平居中。
	X float64
	// Y 是印章底边坐标，单位为毫米；负值按页面垂直居中，零值可贴页面底部。
	Y float64
}

// defaultStampSize 是默认印章边长（毫米）。
const defaultStampSize = 40.0

const defaultMinSeamStrip = 2.0

type entry struct {
	name   string
	data   []byte
	method uint16
}

// Sign 读取文件路径、[]byte 或 io.Reader，为每个文档体签署后写入 w。
// Append 保留已有普通签名，否则替换已有签名；取消传递给外部签名命令。
// 错误时应丢弃 Writer 的部分输出；旧签名保护了需修改的入口时返回 crypt.ErrProtectedContent。
func Sign(ctx context.Context, input any, w io.Writer, options Options) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if options.Type == "" {
		options.Type = spec.SigTypeSeal
	}
	if options.Type != spec.SigTypeSeal && options.Type != spec.SigTypeSign {
		return fmt.Errorf("签名类型必须为 Seal 或 Sign")
	}
	if err := validateAppendOptions(options); err != nil {
		return err
	}
	seenParameters := map[string]bool{}
	for _, parameter := range options.Parameters {
		if parameter.Name == "" || seenParameters[parameter.Name] {
			return fmt.Errorf("扩展参数名称为空或重复")
		}
		seenParameters[parameter.Name] = true
	}
	if (strings.TrimSpace(options.Command) == "") == (options.Sign == nil) {
		return errors.New("需要且只能指定外部签名命令或 Sign 回调")
	}
	if w == nil {
		return errors.New("签名输出写入器为空")
	}
	open := openPackage
	if options.Append || options.Physical {
		open = openAppendPackage
	}
	pkg, err := open(input)
	if err != nil {
		return err
	}
	defer func() { _ = pkg.Close() }()
	if protocol.Present(pkg) && !options.Physical {
		info, err := protocol.InspectPackage(ctx, pkg, protocol.InspectOptions{Mode: protocol.ModeCompat})
		return &protocol.LockedError{Info: info, Cause: err}
	}
	if options.Physical {
		if err := physicalsig.CheckContainer(ctx, pkg, protocol.Limits{}); err != nil {
			return err
		}
		info, err := protocol.InspectPackage(ctx, pkg, protocol.InspectOptions{})
		if err != nil {
			return err
		}
		if !info.Encrypted {
			return protocol.ErrNotEncrypted
		}
		for _, operation := range info.Operations {
			for _, mapping := range operation.Entries {
				if mapping.Path == spec.RootDocument {
					return fmt.Errorf("%w: 物理签署要求主入口不被加密映射覆盖", protocol.ErrProtectedContent)
				}
			}
		}
	}
	if pkg.Has(spec.IntegrityEntryPoint) {
		return fmt.Errorf("%w: 重签前需单独处理完整性清单", protocol.ErrProtectedContent)
	}

	var entries []entry
	if options.Append || options.Physical {
		entries, err = readAppendEntries(ctx, pkg)
	} else {
		entries, err = readEntries(pkg)
	}
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
	if (options.Append || options.Physical) && len(rootData) > 4<<20 {
		return fmt.Errorf("签名入口 XML 超过 4 MiB")
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
	var appendState *appendPlan
	if options.Append {
		appendState, err = prepareAppend(ctx, pkg, entries, index, document, options)
		if err != nil {
			return err
		}
		options.ID = appendState.id
	}
	if options.Physical {
		appendState, err = preparePhysical(ctx, pkg, entries, index, document, options)
		if err != nil {
			return err
		}
		options.ID = appendState.id
	}

	id := strings.TrimSpace(options.ID)
	if id == "" {
		id = "sign-1"
	}
	if !securexml.ValidID(id) {
		return fmt.Errorf("签名 ID 必须为合法 xs:ID")
	}
	stampID := normalizeStampID(id)
	signatureMethod := strings.TrimSpace(options.SignatureMethod)
	if signatureMethod == "" {
		signatureMethod = DefaultSignatureMethod
	}
	checkMethod := strings.TrimSpace(options.CheckMethod)
	if checkMethod == "" {
		checkMethod = DefaultCheckMethod
	}
	if options.Type == spec.SigTypeSign && (signatureMethod != DefaultSignatureMethod || (checkMethod != "SM3" && checkMethod != "1.2.156.10197.1.401")) {
		return fmt.Errorf("普通数字签名仅支持 SM2-with-SM3 和 SM3 引用摘要")
	}
	if options.Type == spec.SigTypeSign && options.References == nil {
		options.References = &ReferenceOptions{RootDocument: true}
	}
	date := options.Date
	if date.IsZero() {
		date = time.Now()
	}

	// 先确定最终入口，RootDocument 引用必须摘要更新后的真实字节。
	if !options.Append && !options.Physical {
		for _, body := range bodies {
			docDir, err := docDirOf(body)
			if err != nil {
				return err
			}
			setBodySignatures(body, docDir+"/Signatures.xml")
		}
	}
	updatedRoot, err := document.WriteToBytes()
	if err != nil {
		return err
	}
	if options.Append || options.Physical {
		updatedRoot = appendState.root
	}
	index[spec.RootDocument] = updatedRoot
	var added []entry
	for bodyIndex, body := range bodies {
		addedStart := len(added)
		docDir, err := docDirOf(body)
		if err != nil {
			return err
		}
		signatureDir := path.Join(docDir, "Signatures")
		listName := path.Join(docDir, "Signatures.xml")
		listData := buildSignaturesXML(id, "Signature_"+id+".xml", options.Type)
		references := collectReferences(docDir, entries, options.References)
		if options.Append || options.Physical {
			plan := appendState.bodies[bodyIndex]
			signatureDir, listName, listData = plan.signatureDir, plan.listName, plan.listData
			references = appendReferences(docDir, entries, options.References, appendState.priorFiles, appendState.catalogs)
		}
		if options.Physical {
			references = physicalReferences(index)
		}
		index[listName] = listData
		if options.Type == spec.SigTypeSign && !options.Physical && (!options.Append || options.LockSignatures) {
			// 列表不包含摘要，不会形成自引用循环；签署它以固定 Type/Relative/路径。
			references = append(references, "/"+listName)
		}
		if len(references) == 0 {
			return fmt.Errorf("%s 没有可签名的文件", docDir)
		}

		baseName := "Signature_" + id + ".xml"
		var stamps []*stampAnnot
		if options.Stamp != nil && options.StampSeam != nil {
			return errors.New("Stamp 与 StampSeam 不能同时使用")
		}
		if options.Stamp != nil {
			stamp, stampErr := resolveStamp(*options.Stamp, docDir, entries)
			if stampErr != nil {
				return stampErr
			}
			stamps = append(stamps, stamp)
		}
		if options.StampSeam != nil {
			stamps, err = resolveStampSeams(*options.StampSeam, docDir, entries)
			if err != nil {
				return err
			}
		}
		var sealBaseLoc, sealImageLoc string
		if options.Seal != "" {
			read := os.ReadFile
			if options.Append {
				read = func(name string) ([]byte, error) { return readAppendAsset(name, 64<<20) }
			}
			sealData, err := read(options.Seal)
			if err != nil {
				return fmt.Errorf("读取电子印章文件失败: %w", err)
			}
			sealBaseLoc = "Seal.esl"
			references = append(references, sealBaseLoc)
			sort.Strings(references)
			sealEntryName := path.Join(signatureDir, sealBaseLoc)
			added = append(added, entry{
				name:   sealEntryName,
				data:   sealData,
				method: zip.Deflate,
			})
			// index 的数据在 added 之前构建，新打包进来的 Seal.esl 必须手动登记，
			// 否则 SignedInfo/References 算摘要时在 files 里找不到它。
			index[sealEntryName] = sealData
		}
		if options.SealImage != "" {
			sealImageLoc = "SealImage" + strings.ToLower(path.Ext(options.SealImage))
			imageData, err := readAppendAsset(options.SealImage, 4<<20)
			if err != nil {
				return err
			}
			name := path.Join(signatureDir, sealImageLoc)
			index[name] = imageData
			added = append(added, entry{name: name, data: imageData, method: zip.Store})
			references = append(references, sealImageLoc)
			sort.Strings(references)
		}
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
			stamps:          stamps,
			stampID:         stampID,
			sealBaseLoc:     sealBaseLoc,
			sealImageLoc:    sealImageLoc,
			protocolVersion: options.ProtocolVersion,
			extendData:      options.ExtendData,
			parameters:      options.Parameters,
		})
		if err != nil {
			return err
		}
		if len(signatureXML) > 4<<20 {
			return fmt.Errorf("签名 XML 超过 4 MiB")
		}
		if options.Append || options.Physical {
			var checked struct{}
			if err := physicalsig.ValidateXML(ctx, signatureXML, "Signature", "", &checked); err != nil {
				return err
			}
		}

		var signedValue []byte
		if options.Sign != nil {
			signedValue, err = options.Sign(ctx, bytes.Clone(signatureXML))
		} else {
			signedValue, err = runCommand(ctx, options.Command, signatureXML, map[string]string{
				"OFD_SIGN_TYPE":             options.Type,
				"OFD_SIGN_SCOPE":            signatureScope(options.Physical),
				"OFD_SIGN_DOCUMENT":         docDir,
				"OFD_SIGN_ID":               id,
				"OFD_SIGN_PROVIDER":         options.ProviderName,
				"OFD_SIGN_PROVIDER_VERSION": options.ProviderVersion,
				"OFD_SIGN_COMPANY":          options.Company,
				"OFD_SIGN_SIGNATURE_METHOD": signatureMethod,
				"OFD_SIGN_CHECK_METHOD":     checkMethod,
				"OFD_SIGN_TIME":             date.Format(time.RFC3339),
			}, options.Environment)
		}
		if err != nil {
			return err
		}
		if len(signedValue) == 0 || len(signedValue) > 4<<20 {
			return fmt.Errorf("签名值为空或超过 4 MiB")
		}
		if options.Type == spec.SigTypeSign {
			proof, err := signeddata.Verify(signedValue, signatureXML, options.UID)
			if err != nil {
				return fmt.Errorf("外部数字签名未绑定当前 Signature.xml: %w", err)
			}
			if proof.TimestampError != nil {
				return proof.TimestampError
			}
			if proof.Timestamp != nil && proof.Timestamp.Time.Before(proof.SigningTime) {
				return fmt.Errorf("时间戳早于签名时间")
			}
		}
		added = append(added,
			entry{name: path.Join(signatureDir, baseName), data: signatureXML, method: zip.Deflate},
			entry{name: listName, data: listData, method: zip.Deflate},
			entry{name: path.Join(signatureDir, "Data", id+".dat"), data: signedValue, method: zip.Store},
		)
		if options.Append || options.Physical {
			if err := appendState.consume(added[addedStart:]); err != nil {
				return err
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	return writePackage(w, entries, added, updatedRoot, options.Deterministic, options.Append || options.Physical)
}

type signatureXMLInput struct {
	protocolVersion string
	extendData      []byte
	parameters      []Parameter
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
	stamps          []*stampAnnot
	stampID         string
	sealBaseLoc     string
	sealImageLoc    string
}

// stampAnnot 是写入 Signature.xml 的 StampAnnot 属性值。
type stampAnnot struct {
	pageRef  string
	boundary string
	clip     string
}

func buildSignatureXML(input signatureXMLInput) ([]byte, error) {
	doc := etree.NewDocument()
	doc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)
	root := doc.CreateElement("Signature")
	root.CreateAttr("xmlns", spec.Namespace)
	root.CreateAttr("ID", input.id)
	info := root.CreateElement("SignedInfo")
	provider := info.CreateElement("Provider")
	provider.CreateAttr("ProviderName", input.providerName)
	if input.providerVersion != "" {
		provider.CreateAttr("Version", input.providerVersion)
	}
	if input.company != "" {
		provider.CreateAttr("Company", input.company)
	}
	if input.protocolVersion != "" {
		provider.CreateAttr("ProtocolVer", input.protocolVersion)
	}
	if input.extendData != nil {
		provider.CreateElement("ExtendData").SetText(base64.StdEncoding.EncodeToString(input.extendData))
	}
	info.CreateElement("SignatureMethod").SetText(input.signatureMethod)
	info.CreateElement("SignatureDateTime").SetText(input.date.Format(time.RFC3339))
	if len(input.parameters) > 0 {
		parameters := info.CreateElement("Parameters")
		for _, value := range input.parameters {
			node := parameters.CreateElement("Parameter")
			node.CreateAttr("Name", value.Name)
			if value.Type != "" {
				node.CreateAttr("Type", value.Type)
			}
			node.SetText(value.Value)
		}
	}
	references := info.CreateElement("References")
	references.CreateAttr("CheckMethod", input.checkMethod)
	for _, reference := range input.references {
		target, err := securexml.Resolve(path.Join(input.signatureBase, "Signature.xml"), reference)
		if err != nil {
			return nil, err
		}
		data, ok := input.files[target]
		if !ok {
			return nil, fmt.Errorf("签名引用目标不存在: %s", target)
		}
		digest, err := signatureDigest(input.checkMethod, data)
		if err != nil {
			return nil, err
		}
		element := references.CreateElement("Reference")
		element.CreateAttr("FileRef", reference)
		element.CreateElement("CheckValue").SetText(base64.StdEncoding.EncodeToString(digest))
	}
	for index, item := range input.stamps {
		stamp := info.CreateElement("StampAnnot")
		stampID := input.stampID
		if len(input.stamps) > 1 {
			stampID = fmt.Sprintf("%s-stamp-%d", input.stampID, index+1)
		}
		stamp.CreateAttr("ID", stampID)
		stamp.CreateAttr("PageRef", item.pageRef)
		stamp.CreateAttr("Boundary", item.boundary)
		if item.clip != "" {
			stamp.CreateAttr("Clip", item.clip)
		}
	}
	if input.sealBaseLoc != "" || input.sealImageLoc != "" {
		// Seal 的 BaseLoc 是子元素而不是属性：真实产物（如 ofdrw 的样例）写的是
		// <Seal><BaseLoc>/Doc_0/Signs/Sign_0/Seal.esl</BaseLoc></Seal>。
		// 写成属性时 models.Seal.BaseLoc 为空，parser 会把空路径解析成签名目录
		// 本身并报"打开文件失败: Doc_0/Signatures"。
		seal := info.CreateElement("Seal")
		if input.sealBaseLoc != "" {
			seal.CreateElement("BaseLoc").SetText(input.sealBaseLoc)
		}
		if input.sealImageLoc != "" {
			seal.CreateElement("ImageLoc").SetText(input.sealImageLoc)
		}
	}
	root.CreateElement("SignedValue").SetText(input.signedValue)
	out, err := doc.WriteToBytes()
	if err != nil {
		return nil, fmt.Errorf("生成 Signature.xml 失败: %w", err)
	}
	return out, nil
}

func signatureDigest(method string, data []byte) ([]byte, error) {
	var newHash func() hash.Hash
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "MD5":
		newHash = md5.New
	case "SHA1":
		newHash = sha1.New
	case "SM3", "1.2.156.10197.1.401":
		newHash = sm3.New
	default:
		return nil, fmt.Errorf("签名摘要算法无效: %q", method)
	}
	digest := newHash()
	_, _ = digest.Write(data)
	return digest.Sum(nil), nil
}

func buildSignaturesXML(id, baseName, kind string) []byte {
	doc := etree.NewDocument()
	doc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)
	root := doc.CreateElement("Signatures")
	root.CreateAttr("xmlns", spec.Namespace)
	root.CreateElement("MaxSignId").SetText("MaxSignId-1")
	element := root.CreateElement("Signature")
	element.CreateAttr("ID", id)
	element.CreateAttr("Type", kind)
	element.CreateAttr("BaseLoc", "Signatures/"+baseName)
	out, _ := doc.WriteToBytes()
	return out
}

func runCommand(ctx context.Context, command string, stdin []byte, environment map[string]string, extra []string) ([]byte, error) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil, errors.New("外部签名命令为空")
	}
	cmd := exec.CommandContext(ctx, fields[0], fields[1:]...)
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Env = os.Environ()
	for key, value := range environment {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	cmd.Env = append(cmd.Env, extra...)
	stdout, stderr := &boundedOutput{limit: 4 << 20}, &boundedOutput{limit: 64 << 10}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
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

type boundedOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedOutput) Write(data []byte) (int, error) {
	if len(data) > b.limit-b.buffer.Len() {
		return 0, fmt.Errorf("外部签名输出超过上限")
	}
	return b.buffer.Write(data)
}
func (b *boundedOutput) Len() int       { return b.buffer.Len() }
func (b *boundedOutput) Bytes() []byte  { return b.buffer.Bytes() }
func (b *boundedOutput) String() string { return b.buffer.String() }

// isSignatureEntry 判断条目是否属于文档体的签名文件。
// 除当前 layout 的 Signatures 目录外，同时识别历史生产者使用的 Signs 目录，
// 避免重签后旧签名残留。
func isSignatureEntry(name string) bool {
	return creator.IsSignatureEntry(name)
}

func writePackage(w io.Writer, entries, added []entry, rootData []byte, deterministic, preserveSignatures bool) error {
	archive := zip.NewWriter(w)
	seen := make(map[string]bool)
	replaced := make(map[string]bool, len(added))
	for _, item := range added {
		replaced[item.name] = true
	}
	write := func(name string, data []byte, method uint16) error {
		if err := core.ValidateEntryName(name); err != nil {
			return err
		}
		if seen[name] {
			return fmt.Errorf("OFD 包条目路径重复: %s", name)
		}
		seen[name] = true
		header := &zip.FileHeader{Name: name, Method: method}
		if deterministic {
			header.Modified = time.Unix(0, 0).UTC()
		}
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
		if preserveSignatures && replaced[item.name] || !preserveSignatures && isSignatureEntry(item.name) {
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
