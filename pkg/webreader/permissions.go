package webreader

import (
	"errors"
	"fmt"
	"time"

	"github.com/zc310/ofd/internal/models"
)

// ErrPermissionDenied 表示文档声明禁止本次操作。
var ErrPermissionDenied = errors.New("文档权限不允许此操作")

// PermissionAction 是阅读器可检查的操作。复制按 Export 权限执行，不增加非标准 Copy 字段。
type PermissionAction string

const (
	PermissionCopy   PermissionAction = "copy"
	PermissionExport PermissionAction = "export"
	PermissionPrint  PermissionAction = "print"
)

// DocumentPermissions 是当前版本的文档体权限快照。布尔字段已应用表 8 的默认 true；
// PrintCopies 小于零表示不限份数，零表示禁止打印。有效期为原声明，调用方可用于访问策略。
type DocumentPermissions struct {
	Scope       int
	Pages       []int
	Declared    bool
	Edit        bool
	Annot       bool
	Export      bool
	Signature   bool
	Watermark   bool
	PrintScreen bool
	Printable   bool
	PrintCopies int
	StartDate   string
	EndDate     string
}

func documentPermissions(scope int, value *models.CT_Permission) DocumentPermissions {
	result := DocumentPermissions{Scope: scope, Edit: true, Annot: true, Export: true,
		Signature: true, Watermark: true, PrintScreen: true, Printable: true, PrintCopies: -1}
	if value == nil {
		return result
	}
	result.Declared = true
	allowed := func(value *bool) bool { return value == nil || *value }
	result.Edit, result.Annot, result.Export = allowed(value.Edit), allowed(value.Annot), allowed(value.Export)
	result.Signature, result.Watermark, result.PrintScreen = allowed(value.Signature), allowed(value.Watermark), allowed(value.PrintScreen)
	if value.Print != nil {
		result.PrintCopies = value.Print.Copies
		result.Printable = value.Print.Printable && value.Print.Copies != 0
	}
	if value.ValidPeriod != nil {
		if !value.ValidPeriod.StartDate.IsZero() {
			result.StartDate = value.ValidPeriod.StartDate.Format(time.RFC3339)
		}
		if !value.ValidPeriod.EndDate.IsZero() {
			result.EndDate = value.ValidPeriod.EndDate.Format(time.RFC3339)
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
	if action != PermissionCopy && action != PermissionExport && action != PermissionPrint {
		return fmt.Errorf("未知权限操作: %q", action)
	}
	if scope < 0 || scope >= len(r.ofd.Documents) {
		return fmt.Errorf("文档体索引超出范围: %d", scope)
	}
	permission := documentPermissions(scope, r.ofd.Documents[scope].Permissions)
	if action == PermissionPrint && !permission.Printable {
		return fmt.Errorf("%w：文档体 %d 禁止打印", ErrPermissionDenied, scope)
	}
	if (action == PermissionCopy || action == PermissionExport) && !permission.Export {
		return fmt.Errorf("%w：文档体 %d 禁止复制或导出内容", ErrPermissionDenied, scope)
	}
	return nil
}

func (r *Reader) checkPermissionLocked(action PermissionAction, indices []int) error {
	if action != PermissionCopy && action != PermissionExport && action != PermissionPrint {
		return fmt.Errorf("未知权限操作: %q", action)
	}
	if len(indices) == 0 {
		return errors.New("权限检查页面列表为空")
	}
	seen := make(map[int]bool)
	for _, index := range indices {
		if index < 0 || index >= len(r.pages) {
			return fmt.Errorf("页面索引超出范围: %d", index)
		}
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
		return nil
	}
	if purpose != PermissionExport && purpose != PermissionPrint {
		return fmt.Errorf("无效的渲染用途: %q", purpose)
	}
	return r.checkPermissionLocked(purpose, indices)
}
