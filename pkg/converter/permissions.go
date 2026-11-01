package converter

import (
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/pkg/permission"
	"github.com/zc310/ofd/pkg/spec"
)

// WithPermissionPolicy 设置OFD导出的权限策略，零值默认执行Export及有效期。
// 预览/打印应使用reader的用途接口，不能通过改变转换用途绕过Export声明。
func WithPermissionPolicy(policy permission.Policy) Option {
	return func(c *Converter) { c.permissionPolicy = policy }
}

type permissionScope struct {
	scope       int
	declaration *spec.Permissions
}

// preparePermissions 只授权实际选中的文档体，在任何输出或回调前完成全部预检。
func (c *Converter) preparePermissions(documents []*render.Document) error {
	if err := c.permissionPolicy.Validate(); err != nil {
		return err
	}
	start, end, err := pageRange(countDocumentPages(documents), c.page)
	if err != nil {
		return err
	}
	c.permissionScopes = nil
	position := 0
	for scope, document := range documents {
		if document == nil || document.Document == nil {
			continue
		}
		selected := false
		for _, page := range document.Pages {
			if page == nil {
				continue
			}
			if position >= start && position < end {
				selected = true
			}
			position++
		}
		if selected {
			c.permissionScopes = append(c.permissionScopes, permissionScope{scope, document.Permissions.Clone()})
		}
	}
	return c.checkCancelled()
}

func (c *Converter) checkPermissions() error {
	if err := c.permissionPolicy.Validate(); err != nil {
		return err
	}
	for _, scope := range c.permissionScopes {
		if err := c.permissionPolicy.Check(scope.declaration, permission.Export, scope.scope); err != nil {
			return err
		}
	}
	return nil
}
