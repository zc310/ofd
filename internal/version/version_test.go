package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVersionMatchesMakefile 守住版本号的单一来源约定。
//
// Makefile 的 VERSION 与这里的 Version 是同一个版本号的两份文本：构建期注入前者，
// go run / go build 未经 Makefile 时用后者。两者曾漂移到相差 9 个版本（Makefile
// 0.0.5 而 git tag 已是 v0.1.2），而当时只有一行注释在声明"保持一致"——注释不拦人，
// 测试才拦得住。
func TestVersionMatchesMakefile(t *testing.T) {
	makefile, err := findMakefile()
	if err != nil {
		t.Fatalf("定位 Makefile 失败: %v", err)
	}
	raw, err := os.ReadFile(makefile)
	if err != nil {
		t.Fatalf("读取 Makefile 失败: %v", err)
	}
	const prefix = "VERSION ?= "
	var declared string
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) {
			declared = strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
			break
		}
	}
	if declared == "" {
		t.Fatal("Makefile 里找不到 VERSION ?= 赋值")
	}
	if declared != Version {
		t.Errorf("版本号不一致：Makefile VERSION=%s，internal/version.Version=%s；改动时两处都要更新",
			declared, Version)
	}
}

// TestVersionHasNoVPrefix 裸版本号不带 v 前缀。
//
// 报告和 OFD 元数据里的版本字段直接用 Version，写成 "v0.1.2" 会让按版本排序或
// 精确匹配的脚本多一次处理；v 前缀只属于展示层。
func TestVersionHasNoVPrefix(t *testing.T) {
	if strings.HasPrefix(Version, "v") {
		t.Errorf("Version = %q，不应带 v 前缀", Version)
	}
	if strings.TrimSpace(Version) != Version || Version == "" {
		t.Errorf("Version = %q，应为不含空白的非空版本号", Version)
	}
}

// findMakefile 向上查找 Makefile，不写死相对层数：internal/version 一旦搬家，
// 写死 "../../.." 的测试会以"文件不存在"失败，而那与版本号无关。
func findMakefile() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, "Makefile")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
