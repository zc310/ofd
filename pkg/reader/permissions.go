package reader

import (
	"errors"
	"fmt"
	"time"

	"github.com/zc310/ofd/pkg/permission"
	"github.com/zc310/ofd/pkg/spec"
)

// ErrPermissionDenied 表示文档声明禁止本次操作。
var ErrPermissionDenied = permission.ErrDenied

// PermissionAction 是阅读器可检查的操作。复制按 Export 权限执行，不增加非标准 Copy 字段。
type PermissionAction = permission.Action

const (
	PermissionAccess      = permission.Access
	PermissionCopy        = permission.Copy
	PermissionExport      = permission.Export
	PermissionPrint       = permission.Print
	PermissionEdit        = permission.Edit
	PermissionAnnot       = permission.Annot
	PermissionSignature   = permission.Signature
	PermissionWatermark   = permission.Watermark
	PermissionPrintScreen = permission.PrintScreen
)

// DocumentPermissions 是当前版本的文档体权限快照。布尔字段已应用表 8 的默认 true；
// PrintCopies 小于零表示不限份数，零表示禁止打印。时间字段保留声明，访问时重新评估。
type DocumentPermissions struct {
	// Accessible/AccessReason是本次快照时的访问判断，不应缓存为永久授权。
	Accessible   bool              `json:"accessible"`
	AccessReason permission.Reason `json:"access_reason"`
	Scope        int               `json:"scope"`
	Pages        []int             `json:"pages"`
	Declared     bool              `json:"declared"`
	Edit         bool              `json:"edit"`
	Annot        bool              `json:"annot"`
	Export       bool              `json:"export"`
	Signature    bool              `json:"signature"`
	Watermark    bool              `json:"watermark"`
	PrintScreen  bool              `json:"print_screen"`
	Printable    bool              `json:"printable"`
	PrintCopies  int               `json:"print_copies"`
	StartDate    string            `json:"start_date"`
	EndDate      string            `json:"end_date"`
}

func documentPermissions(scope int, value *spec.Permissions) DocumentPermissions {
	result := DocumentPermissions{Scope: scope, Declared: value != nil, Edit: true, Annot: true, Export: true,
		Signature: true, Watermark: true, PrintScreen: true, Printable: true, PrintCopies: -1}
	if value == nil {
		return result
	}
	allowed := func(value *bool) bool { return value == nil || *value }
	result.Edit, result.Annot, result.Export = allowed(value.Edit), allowed(value.Annot), allowed(value.Export)
	result.Signature, result.Watermark, result.PrintScreen = allowed(value.Signature), allowed(value.Watermark), allowed(value.PrintScreen)
	if value.Print != nil {
		result.Printable = value.Print.Printable && value.Print.Copies != 0
		result.PrintCopies = value.Print.Copies
	}
	if value.ValidPeriod != nil {
		if !value.ValidPeriod.StartDate.IsZero() {
			result.StartDate = value.ValidPeriod.StartDate.Format(time.RFC3339Nano)
		}
		if !value.ValidPeriod.EndDate.IsZero() {
			result.EndDate = value.ValidPeriod.EndDate.Format(time.RFC3339Nano)
		}
	}
	return result
}

// Permissions 返回所有文档体当前版本的独立权限快照，Pages 为从零开始的全局页码。
func (r *Reader) Permissions() ([]DocumentPermissions, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	result := make([]DocumentPermissions, len(r.ofd.Documents))
	for scope, document := range r.ofd.Documents {
		result[scope] = documentPermissions(scope, document.Permissions)
		accessErr := r.checkDocumentPermissionLocked(PermissionAccess, scope)
		result[scope].Accessible = accessErr == nil
		if denied, ok := errors.AsType[*permission.DeniedError](accessErr); ok {
			result[scope].AccessReason = denied.Reason
		}
	}
	for index, ref := range r.pages {
		result[ref.fontScope].Pages = append(result[ref.fontScope].Pages, index)
	}
	return result, nil
}

// CheckPermission 检查所选页面所属文档体的权限；空列表和非法页码返回错误。
// 打印的正份数限制供调用方展示，浏览器无法可靠控制系统打印对话框的份数或完成情况。
func (r *Reader) CheckPermission(action PermissionAction, indices []int) error {
	if r == nil {
		return errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return errors.New("文档引擎已经关闭")
	}
	return r.checkPermissionLocked(action, indices)
}

// CheckDocumentPermission 检查指定文档体当前版本的操作权限，供附件、媒体等文档资源操作使用。
func (r *Reader) CheckDocumentPermission(action PermissionAction, scope int) error {
	if r == nil {
		return errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return errors.New("文档引擎已经关闭")
	}
	return r.checkDocumentPermissionLocked(action, scope)
}

func (r *Reader) checkDocumentPermissionLocked(action PermissionAction, scope int) error {
	if r.ofd == nil {
		return errEmptyReader
	}
	if scope < 0 || scope >= len(r.ofd.Documents) {
		return fmt.Errorf("文档体索引超出范围: %d", scope)
	}
	return r.permissionPolicy.Check(r.ofd.Documents[scope].Permissions, action, scope)
}

func (r *Reader) checkPermissionLocked(action PermissionAction, indices []int) error {
	if !permission.ValidAction(action) {
		return fmt.Errorf("%w: %q", permission.ErrAction, action)
	}
	if len(indices) == 0 {
		return errors.New("权限检查页面列表为空")
	}
	for _, index := range indices {
		if index < 0 || index >= len(r.pages) {
			return fmt.Errorf("%w: %d", errRenderPageIndex, index)
		}
	}
	seen := make(map[int]bool)
	for _, index := range indices {
		ref := r.pages[index]
		if seen[ref.fontScope] {
			continue
		}
		seen[ref.fontScope] = true
		if err := r.checkDocumentPermissionLocked(action, ref.fontScope); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reader) checkRenderPermissionLocked(indices []int, purpose PermissionAction) error {
	if purpose == "" {
		// 普通页面预览不受导出、打印权限影响。
		if len(indices) == 0 {
			return nil
		}
		return r.checkPermissionLocked(PermissionAccess, indices)
	}
	if purpose != PermissionExport && purpose != PermissionPrint {
		return fmt.Errorf("无效的渲染用途: %q", purpose)
	}
	return r.checkPermissionLocked(purpose, indices)
}

// checkAccessLocked 在缓存读取前检查全部文档体；逐页接口改用对应页面作用域检查。
// Permissions、PageCount及Close不经过此门控，便于诊断与资源释放。
func (r *Reader) checkAccessLocked() error {
	if r.ofd == nil {
		return nil
	}
	for scope := range r.ofd.Documents {
		if err := r.checkDocumentPermissionLocked(PermissionAccess, scope); err != nil {
			return err
		}
	}
	return nil
}
