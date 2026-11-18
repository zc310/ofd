// Package workflow 编排电子证照原件锁定与加注件制作。
// 本包复用 sign、validator 和条目编辑能力；不内置私钥、信任根或机构授权规则。
package workflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	protocol "github.com/zc310/ofd/internal/crypt"
	"github.com/zc310/ofd/pkg/certificate"
	"github.com/zc310/ofd/pkg/sign"
	"github.com/zc310/ofd/pkg/spec"
)

// Role 区分本次验收或签署的用途，由调用方明确指定，不根据签名者名称猜测。
type Role string

const (
	// Original 表示由颁发机构或其授权机构锁定的原件。
	Original Role = "original"
	// Copy 表示由加注件制作方重新锁定的加注件。
	Copy Role = "copy"
)

var (
	// ErrInvalid 表示本地结构、元数据、覆盖范围或签名已确认无效。
	ErrInvalid = errors.New("电子证照工作流检查失败")
	// ErrEvidenceRequired 表示缺少完整验签或业务授权所需证据。
	ErrEvidenceRequired = errors.New("电子证照工作流缺少验证证据")
	// ErrAuthorization 表示业务授权回调拒绝本次机构角色。
	ErrAuthorization = errors.New("电子证照签署角色未获授权")
	// ErrOptions 表示工作流参数错误。
	ErrOptions = errors.New("电子证照工作流参数无效")
)

// Authorization 是交给业务授权策略的快照。证书须与实际锁定签名匹配，
// 不应仅比较未验证的证书 Subject 文本与元数据机构名称。
type Authorization struct {
	Role        Role
	SignatureID string
	Metadata    certificate.Metadata
	// SignerCertificate 为实际验签使用的签名者证书 DER 副本。
	SignerCertificate []byte
}

// Authorize 验证实际签名者是否获准担任指定角色；nil 不代表默认授权。
// Original 应核对颁发机构或授权关系，Copy 应核对加注件制作方。
type Authorize func(context.Context, Authorization) error

// VerifyOptions 要求明确选择锁定签名 ID 及用途，避免误把业务签名当作锁定签名。
type VerifyOptions struct {
	LockID       string
	Role         Role
	Verification sign.VerificationOptions
	Authorize    Authorize
}

// LockOptions 配置锁定签署。Sign 必须指定 ID 和签名器，其他强制范围字段由工作流控制。
// 验收要求完整信任/吊销证据及业务授权，缺失时不写入输出。
type LockOptions struct {
	Sign         sign.Options
	Verification sign.VerificationOptions
	Authorize    Authorize
}

// CopyMetadata 是 GB/T 36903—2018 §5.4 的加注件信息，时间必须由调用方明确提供。
type CopyMetadata struct {
	CreationTime time.Time
	Creator      string
	Cause        string
	ExpiringDate string
}

// CopyOptions 控制派生过程。Edit 必须完成实际加注，并保留原有业务签名，
// 例如调用 watermark.Add 时显式选择 creator.SignaturePreserve。
type CopyOptions struct {
	Source   VerifyOptions
	Lock     LockOptions
	Metadata CopyMetadata
	// Edit 接收移除所选锁定签名并补入加注件元数据后的独立字节副本；
	// 返回新的 OFD，不得覆盖源文件。取消须返回 ctx.Err()。
	Edit func(context.Context, []byte) ([]byte, error)
}

// VerifyLock 检查明确选定的锁定签名、整包覆盖、全部签名的完整结论及机构角色授权。
// 目前仅支持未加密的单文档体、普通 Sign 锁定，不推断行业数据或 GM 码合规。
func VerifyLock(ctx context.Context, input []byte, options VerifyOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateVerifyOptions(options); err != nil {
		return err
	}
	input, err := freeze(input)
	if err != nil {
		return err
	}
	s, err := inspect(ctx, input)
	if err != nil {
		return err
	}
	defer s.close()
	return verifyLock(ctx, input, s, options)
}

// SignOriginal 为通过基础检查的原件追加锁定签名并在完整验收后一次写出。
// 不会移除已有业务签名；产物保护全部现存文件（本次签名 XML 与签名值除外）。
func SignOriginal(ctx context.Context, input []byte, w io.Writer, options LockOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateLockOptions(options, w); err != nil {
		return err
	}
	input, err := freeze(input)
	if err != nil {
		return err
	}
	s, err := inspect(ctx, input)
	if err != nil {
		return err
	}
	defer s.close()
	if err := s.checkRole(Original); err != nil {
		return err
	}
	output, err := lock(ctx, input, Original, options)
	if err != nil {
		return err
	}
	return publish(ctx, w, output)
}

func validateVerifyOptions(options VerifyOptions) error {
	if !validID(options.LockID) || (options.Role != Original && options.Role != Copy) {
		return fmt.Errorf("%w: 需提供合法 LockID 及 original/copy 用途", ErrOptions)
	}
	if options.Authorize == nil {
		return fmt.Errorf("%w: 必须提供机构角色授权策略", ErrEvidenceRequired)
	}
	return nil
}

func validateLockOptions(options LockOptions, w io.Writer) error {
	if w == nil {
		return fmt.Errorf("%w: 输出写入器为空", ErrOptions)
	}
	if err := validateVerifyOptions(VerifyOptions{LockID: options.Sign.ID, Role: Original, Authorize: options.Authorize}); err != nil {
		return err
	}
	o := options.Sign
	if err := o.PermissionPolicy.Validate(); err != nil {
		return err
	}
	if (o.Type != "" && o.Type != spec.SigTypeSign) || o.Physical || o.References != nil || o.Relative != "" || o.Append || o.LockSignatures {
		return fmt.Errorf("%w: 工作流固定 Type=Sign、Append 和 LockSignatures，不接受物理签署、Relative 或引用筛选", ErrOptions)
	}
	if (o.Sign == nil) == (strings.TrimSpace(o.Command) == "") {
		return fmt.Errorf("%w: 需且只能提供一个签名器", ErrOptions)
	}
	if options.Verification.Trust == nil || options.Verification.Trust.Roots == nil || options.Verification.Revocation == nil {
		return fmt.Errorf("%w: 锁定签署前须准备信任根和离线吊销证据", ErrEvidenceRequired)
	}
	return nil
}

// freeze 在调用业务授权或编辑回调之前固定源快照，回调不能通过调用方的旧切片偷换输入。
func freeze(input []byte) ([]byte, error) {
	if len(input) > 512<<20 {
		return nil, protocol.ErrLimitExceeded
	}
	return bytes.Clone(input), nil
}

func lock(ctx context.Context, input []byte, role Role, options LockOptions) ([]byte, error) {
	s, err := inspect(ctx, input)
	if err != nil {
		return nil, err
	}
	defer s.close()
	if err := s.checkSignablePaths(); err != nil {
		return nil, err
	}
	if _, err := verifyAll(ctx, input, s, options.Verification); err != nil {
		return nil, err
	}
	o := options.Sign
	o.Type, o.Append, o.LockSignatures = spec.SigTypeSign, true, true
	var output bytes.Buffer
	if err := sign.Sign(ctx, input, &output, o); err != nil {
		return nil, err
	}
	if err := VerifyLock(ctx, output.Bytes(), VerifyOptions{
		LockID: o.ID, Role: role, Verification: options.Verification, Authorize: options.Authorize,
	}); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func publish(ctx context.Context, w io.Writer, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n, err := w.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}
