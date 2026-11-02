package font

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// readFontFixture 保留上游样本的精确断言，不把其他字体冒充该样本。
// 外部样本根目录需包含原有的 resources 和 testdata 子目录。
func readFontFixture(tb testing.TB, name string) ([]byte, error) {
	tb.Helper()
	if root := os.Getenv("OFD_FONT_TEST_ROOT"); root != "" {
		name = filepath.Join(root, name)
	}
	data, err := os.ReadFile(name)
	if errors.Is(err, os.ErrNotExist) {
		tb.Skipf("缺少上游字体测试资源 %s: %v", name, err)
	}
	if err != nil {
		tb.Fatalf("读取上游字体测试资源 %s: %v", name, err)
	}
	return data, nil
}
