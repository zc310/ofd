// Package permission 统一评估OFD文档权限声明。它不提供加密或操作系统级防复制能力。
package permission

import (
	"errors"
	"fmt"
	"time"

	"github.com/zc310/ofd/pkg/spec"
)

// Action 是待执行操作；Copy按Export声明检查，不引入非标准Copy字段。
type Action string

const (
	Access      Action = "access"
	Copy        Action = "copy"
	Export      Action = "export"
	Print       Action = "print"
	Edit        Action = "edit"
	Annot       Action = "annot"
	Signature   Action = "signature"
	Watermark   Action = "watermark"
	PrintScreen Action = "print_screen"
)

var (
	// ErrDenied 表示声明拒绝操作或文档不在有效期内。
	ErrDenied = errors.New("文档权限不允许此操作")
	// ErrAction 表示未知操作；即使显式忽略声明也不接受未知操作。
	ErrAction = errors.New("未知权限操作")
	// ErrMode 表示无效执行策略。
	ErrMode = errors.New("无效权限执行策略")
)

// Mode 指定是否执行声明。忽略声明仅供明确授权的诊断/修复，不应从不可信输入自动启用。
type Mode uint8

const (
	Enforce Mode = iota
	IgnoreDeclarations
)

// Policy 是可复用的执行策略，零值默认执行声明并使用time.Now。
// Now每次检查时调用，必须支持并发且不得重入持有此策略的组件。
type Policy struct {
	Mode Mode
	Now  func() time.Time
}

// Reason 是拒绝的稳定机器可读原因。
type Reason string

const (
	Forbidden     Reason = "forbidden"
	NotYetValid   Reason = "not_yet_valid"
	Expired       Reason = "expired"
	InvalidPeriod Reason = "invalid_period"
)

// DeniedError 保留操作、文档体索引及拒绝原因，可用errors.As读取。
type DeniedError struct {
	Action Action
	Scope  int
	Reason Reason
}

func (e *DeniedError) Error() string {
	return fmt.Sprintf("%s：文档体 %d 操作 %s (%s)", ErrDenied, e.Scope, e.Action, e.Reason)
}

func (e *DeniedError) Unwrap() error { return ErrDenied }

// Validate 检查策略配置，不调用时钟。
func (p Policy) Validate() error {
	if p.Mode != Enforce && p.Mode != IgnoreDeclarations {
		return fmt.Errorf("%w: %d", ErrMode, p.Mode)
	}
	return nil
}

// ValidAction 报告是否为支持的操作。
func ValidAction(action Action) bool {
	switch action {
	case Access, Copy, Export, Print, Edit, Annot, Signature, Watermark, PrintScreen:
		return true
	default:
		return false
	}
}

// Check 按§7.5表8检查有效期与操作声明。起止时刻均包含端点；空/倒置有效期拒绝。
// 此方法不缓存判断；有效期约束所有操作，普通Access不受Export/Print限制。
// nil声明及省略的布尔字段默认允许；检查不修改原始声明，也不缓存授权结果。
// 调用期间不得并发修改d；长任务需要固定原始声明时可先使用d.Clone()。
func (p Policy) Check(d *spec.Permissions, action Action, scope int) error {
	if !ValidAction(action) {
		return fmt.Errorf("%w: %q", ErrAction, action)
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if p.Mode == IgnoreDeclarations {
		return nil
	}
	if d == nil {
		return nil
	}
	deny := func(reason Reason) error { return &DeniedError{Action: action, Scope: scope, Reason: reason} }
	if d.ValidPeriod != nil {
		start, end := d.ValidPeriod.StartDate.Time, d.ValidPeriod.EndDate.Time
		if (start.IsZero() && end.IsZero()) || (!start.IsZero() && !end.IsZero() && start.After(end)) {
			return deny(InvalidPeriod)
		}
		now := time.Now()
		if p.Now != nil {
			now = p.Now()
		}
		if !start.IsZero() && now.Before(start) {
			return deny(NotYetValid)
		}
		if !end.IsZero() && now.After(end) {
			return deny(Expired)
		}
	}
	allowed := true
	declared := func(value *bool) bool { return value == nil || *value }
	switch action {
	case Copy, Export:
		allowed = declared(d.Export)
	case Print:
		allowed = d.Print == nil || (d.Print.Printable && d.Print.Copies != 0)
	case Edit:
		allowed = declared(d.Edit)
	case Annot:
		allowed = declared(d.Annot)
	case Signature:
		allowed = declared(d.Signature)
	case Watermark:
		allowed = declared(d.Watermark)
	case PrintScreen:
		allowed = declared(d.PrintScreen)
	case Access:
	}
	if !allowed {
		return deny(Forbidden)
	}
	return nil
}
