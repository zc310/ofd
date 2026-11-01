package export

import (
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/pkg/permission"
)

// permissionCheck 只保存原始声明，每个检查点重新读取时钟，不能缓存授权结果。
func permissionCheck(ofd *parser.OFD, index int, policy permission.Policy) func() error {
	declaration := ofd.Documents[index].Permissions.Clone()
	return func() error { return policy.Check(declaration, permission.Export, index) }
}

func checkAllPermissions(ofd *parser.OFD, policy permission.Policy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	for index, document := range ofd.Documents {
		if document == nil {
			continue
		}
		if err := permissionCheck(ofd, index, policy)(); err != nil {
			return err
		}
	}
	return nil
}
