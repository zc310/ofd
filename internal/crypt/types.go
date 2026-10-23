// Package crypt 实现 GM/T 0099-2020 规定的容器协议。
package crypt

import (
	"context"
	"crypto"
	"errors"

	"github.com/zc310/ofd/pkg/spec"
)

const (
	EntryPoint        = spec.EncryptionEntryPoint
	Namespace         = spec.Namespace
	PasswordScheme    = "1.1.1"
	CertificateScheme = "1.1.2"
)

var (
	ErrLocked              = errors.New("crypt: encrypted OFD is locked")
	ErrInvalidMetadata     = errors.New("crypt: invalid encryption metadata")
	ErrLimitExceeded       = errors.New("crypt: resource limit exceeded")
	ErrNotEncrypted        = errors.New("crypt: no encryption entry point")
	ErrAlreadyEncrypted    = errors.New("crypt: input is already encrypted")
	ErrUnsupported         = errors.New("crypt: unsupported encryption feature")
	ErrCredentialsRequired = errors.New("crypt: credentials required")
	ErrDecryptFailed       = errors.New("crypt: credentials do not match or ciphertext is damaged")
	ErrProtectedContent    = errors.New("crypt: transformation would change protected content")
)

type Mode string

const (
	ModeStrict Mode = "strict"
	ModeCompat Mode = "compat"
)

// Limits 限制检查和解密的资源用量，包括 .dat 条目中的 XML。
// 字段为零时使用有限的默认值。
type Limits struct {
	MaxInputBytes         int64
	MaxEntries            int
	MaxMetadataBytes      int64
	MaxTotalMetadataBytes int64
	MaxXMLNodes           int
	MaxXMLDepth           int
	MaxOperations         int
	MaxRecipients         int
	MaxMappings           int
	MaxEntryBytes         int64
	MaxTotalBytes         int64
	MaxCredentialAttempts int
	MaxLayers             int
	// MaxProcessedBytes 限制构建和读取视图时累计处理的字节数，
	// 包括中间层和重复读取；为零时默认使用 2 GiB。
	MaxProcessedBytes int64
}

type InspectOptions struct {
	Mode   Mode
	Limits Limits
}

// EncryptRecipient 描述同一加密方案中的一个接收者。
// 必须提供非 nil 的 UTF-8 口令或 DER 格式的 SM2 证书，二者互斥。
type EncryptRecipient struct {
	UserName    string
	UserType    string
	Password    []byte
	Certificate []byte
}

type EncryptOptions struct {
	Limits     Limits
	Recipients []EncryptRecipient
	// Entries 为 nil 时选择全部文件，可通过 KeepRoot 排除 OFD.xml。
	// 非 nil 切片按规范 ZIP 相对路径精确选择，空列表会返回错误。
	Entries  []string
	KeepRoot bool
	// PlaintextMap 为 true 时输出明文 XML 映射表，默认加密映射表以隐藏原始路径。
	PlaintextMap bool
	// IndependentKeys 为每个选中条目生成独立 FEK/IV 和 seed，通用密钥仅用于映射表。
	IndependentKeys bool
	// Append 非 nil 时追加到已有线性加密链；其中的凭据用于保护范围预检。
	// 新层选择物理 ZIP 条目，不隐式解开、改写旧层。
	Append *DecryptOptions
	// Pages 计算指定文档页面的模板、注释及资源范围，与 Entries、Append 互斥。
	Pages *PageSelection
}

// PageSelection 按 Document.xml 的规范包内路径和页面 ID 选择范围。
// 共用资源文件整体纳入范围，可能影响使用这些资源的其他页面。
type PageSelection struct {
	Document string
	IDs      []string
}

type EncryptReport struct {
	Relative         string      `json:"relative,omitempty"`
	IndependentKeys  bool        `json:"independent_keys,omitempty"`
	SelectedEntries  []string    `json:"selected_entries,omitempty"`
	SharedResources  bool        `json:"shared_resources,omitempty"`
	State            string      `json:"state"`
	OperationID      string      `json:"operation_id"`
	Scheme           string      `json:"scheme"`
	Recipients       []Recipient `json:"recipients"`
	EncryptedEntries int         `json:"encrypted_entries"`
	OutputEntries    int         `json:"output_entries"`
	OutputBytes      int64       `json:"output_bytes"`
}

type DecryptOptions struct {
	Mode   Mode
	Limits Limits
	// Password 为 nil 表示未提供口令，非 nil 的空切片表示空口令。
	// 字节必须为 UTF-8，由调用方持有，调用期间不得修改。
	// 库不会对口令进行归一化或清零。
	Password []byte
	// PrivateKey 接受支持 SM2 的 crypto.Decrypter，接收 ASN.1 SM2Cipher 和
	// sm2.ASN1DecrypterOpts，随机源参数为 crypto/rand.Reader；硬件适配器须遵守此约定。
	// 仅检查接收者证书与公钥匹配，不验证证书信任链、有效期或吊销状态；
	// 应用的证书使用策略须单独执行，解锁成功不表示接收者身份受信任。
	PrivateKey crypto.Decrypter
	UserName   string
	// MissingScheme 在 compat 模式下为缺失的 EncryptCaseId 显式选择 1.1.1 或 1.1.2。
	// 它不会覆盖源文件中已声明的方案。
	MissingScheme string
	Resolve       CredentialResolver
}

type Credentials struct {
	Password      []byte
	PrivateKey    crypto.Decrypter
	UserName      string
	MissingScheme string
}

type CredentialRequest struct {
	OperationID    string
	DecryptSeedLoc string
	Scheme         string
	Recipients     []Recipient
}

type CredentialResolver func(context.Context, CredentialRequest) (Credentials, error)

type OpenOptions struct {
	DecryptOptions
	// AllowPartial 仅允许因缺少凭据而保留部分解锁的视图。
	// 元数据无效、凭据错误或密文损坏仍会使打开失败。
	AllowPartial bool
}

type OperationResult struct {
	Keys             []SeedResult `json:"keys,omitempty"`
	ID               string       `json:"id"`
	State            string       `json:"state"`
	Scheme           string       `json:"scheme,omitempty"`
	Recipient        Recipient    `json:"recipient"`
	DecryptedEntries int          `json:"decrypted_entries"`
	LockedEntries    int          `json:"locked_entries"`
}

type SeedResult struct {
	DecryptSeedLoc string    `json:"decrypt_seed_loc"`
	Scheme         string    `json:"scheme"`
	Recipient      Recipient `json:"recipient"`
}

// DecryptReport 仅在解密成功时返回。解锁不代表 CBC 明文完整性已验证，
// 也不代表证书受信任或签名有效。
type DecryptReport struct {
	Operations       []OperationResult `json:"operations,omitempty"`
	State            string            `json:"state"`
	OperationID      string            `json:"operation_id"`
	Scheme           string            `json:"scheme"`
	Recipient        Recipient         `json:"recipient"`
	DecryptedEntries int               `json:"decrypted_entries"`
	OutputEntries    int               `json:"output_entries"`
	OutputBytes      int64             `json:"output_bytes"`
	Diagnostics      []Diagnostic      `json:"diagnostics,omitempty"`
}

// Info 仅描述可见元数据，不表示解密成功、内容完整、证书受信任或文档签名有效。
type Info struct {
	// Opaque 表示不可读的映射表可能遮蔽尚未知晓的逻辑路径。
	Opaque      bool         `json:"opaque,omitempty"`
	Encrypted   bool         `json:"encrypted"`
	State       string       `json:"state"`
	EntryPoint  string       `json:"entry_point,omitempty"`
	Operations  []Operation  `json:"operations,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

type Operation struct {
	State          string   `json:"state,omitempty"`
	ID             string   `json:"id"`
	Relative       string   `json:"relative,omitempty"`
	Provider       Provider `json:"provider"`
	Scope          string   `json:"scope"`
	EncryptDate    string   `json:"encrypt_date,omitempty"`
	DecryptSeedLoc string   `json:"decrypt_seed_loc"`
	EntriesMapLoc  string   `json:"entries_map_loc"`
	Seed           SeedInfo `json:"seed"`
	// Deferred 表示较早加密层的元数据需要在后续解密步骤中读取。
	Deferred bool `json:"deferred,omitempty"`
	// MapState 为 plaintext、encrypted 或 unknown（检查失败时）。
	MapState string `json:"map_state"`
	// Entries 在缺少凭据而无法读取映射表时省略。
	Entries []Mapping `json:"entries,omitempty"`
}

type Provider struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Company string `json:"company,omitempty"`
}

type SeedInfo struct {
	ID         string      `json:"id"`
	Scheme     string      `json:"scheme,omitempty"`
	Recipients []Recipient `json:"recipients"`
}

// Recipient 不包含包装后的密钥和原始证书数据。
type Recipient struct {
	UserName          string `json:"user_name"`
	UserType          string `json:"user_type"`
	CertificateSHA256 string `json:"certificate_sha256,omitempty"`
	IVDefaulted       bool   `json:"iv_defaulted"`
}

// Mapping 中的路径为规范 ZIP 相对路径；XML 中使用包内绝对路径。
type Mapping struct {
	Path           string    `json:"path"`
	EPath          string    `json:"encrypted_path,omitempty"`
	DecryptSeedLoc string    `json:"decrypt_seed_loc,omitempty"`
	Seed           *SeedInfo `json:"seed,omitempty"`
}

type Diagnostic struct {
	Code    string `json:"code"`
	File    string `json:"file"`
	Message string `json:"message"`
}

// LockedError 在使用方需要明文时保留加密元数据的检查详情。
// Cause 非 nil 表示加密元数据检查失败。
type LockedError struct {
	Info  *Info
	Cause error
}

func (e *LockedError) Error() string {
	if e.Cause != nil {
		return "OFD 文档已加密且未解锁；检查加密信息失败: " + e.Cause.Error()
	}
	return "OFD 文档已加密且未解锁（GM/T 0099-2020）"
}

func (e *LockedError) Is(target error) bool { return target == ErrLocked }
func (e *LockedError) Unwrap() error        { return e.Cause }
