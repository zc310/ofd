// Package permission 读取原始包的标准权限声明，并按文档体范围执行预检。
package permission

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/pkg/permission"
	"github.com/zc310/ofd/pkg/spec"
)

var errPermissionDocument = errors.New("文档体权限入口无效")

// CheckPackage 按表8检查原始包的目标文档体，不解析页面或资源。
// scopes为nil时检查全部；任意条目编辑无法证明资源归属时应使用全部范围。
// 显式忽略声明允许诊断程序处理权限入口本身损坏或加密的物理包。
func CheckPackage(pkg *container.Package, policy permission.Policy, action permission.Action, scopes []int) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if !permission.ValidAction(action) {
		return fmt.Errorf("%w: %q", permission.ErrAction, action)
	}
	if policy.Mode == permission.IgnoreDeclarations {
		return nil
	}
	declarations, err := ReadPackage(pkg, scopes, 0)
	if err != nil {
		return err
	}
	for _, declaration := range declarations {
		if err := policy.Check(declaration.Value, action, declaration.Scope); err != nil {
			return err
		}
	}
	return nil
}

// ScopedDeclaration 保留声明的原始文档体索引，不能将选中范围重新编号。
type ScopedDeclaration struct {
	Scope int
	Value *spec.Permissions
}

// ReadPackage 在预算内读取原始声明，供长任务保留声明并重新评估时钟。
// limit为零沿用容器默认XML预算；不缓存授权结果。
func ReadPackage(pkg *container.Package, scopes []int, limit int64) ([]ScopedDeclaration, error) {
	read := func(name string, target any) error {
		if limit > 0 {
			return pkg.ReadXMLLimit(name, target, limit)
		}
		return pkg.ReadXML(name, target)
	}
	var root spec.OFD
	if err := read(spec.RootDocument, &root); err != nil {
		return nil, err
	}
	if len(root.DocBodies) == 0 {
		return nil, fmt.Errorf("%w: OFD.xml 中没有 DocBody", errPermissionDocument)
	}
	if scopes == nil {
		scopes = make([]int, len(root.DocBodies))
		for scope := range scopes {
			scopes[scope] = scope
		}
	}
	result := make([]ScopedDeclaration, 0, len(scopes))
	for _, scope := range scopes {
		if scope < 0 || scope >= len(root.DocBodies) {
			return nil, fmt.Errorf("%w: 文档体索引超出范围 %d", errPermissionDocument, scope)
		}
		name := strings.TrimLeft(root.DocBodies[scope].DocRoot.Resolve("/").String(), "/")
		if name == "" {
			return nil, fmt.Errorf("%w: 文档体 %d DocRoot为空", errPermissionDocument, scope)
		}
		var document spec.Document
		if err := read(name, &document); err != nil {
			return nil, fmt.Errorf("读取文档体 %d 权限失败: %w", scope, err)
		}
		result = append(result, ScopedDeclaration{Scope: scope, Value: document.Permissions})
	}
	return result, nil
}
