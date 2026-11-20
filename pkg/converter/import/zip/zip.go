// Package zip 注册 ZIP 图片包导入器，按完整相对路径自然排序并展开为 OFD 页面。
package zip

import (
	"github.com/zc310/ofd/pkg/converter"
	imageimport "github.com/zc310/ofd/pkg/converter/import/image"
)

func init() { //nolint:gochecknoinits // 导入器按项目约定通过 init 注册。
	converter.RegisterImporter(&imageimport.Importer{
		NameValue: "zip", ExtensionsValue: []string{".zip"},
	})
}
