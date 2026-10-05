package fielddocs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSource 写出一段临时 manifest 源码供 Extract 解析。
func writeSource(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.go")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写入临时源码失败: %v", err)
	}
	return path
}

const nestedSource = `package manifest

// Manifest 是入口。
type Manifest struct {
	// Version 指定版本。
	Version int ` + "`json:\"version,omitempty\"`" + `
	// Document 定义文档。
	Document Document ` + "`json:\"document\"`" + `
	// Pages 定义页面。
	Pages []Page ` + "`json:\"pages,omitempty\"`" + `
}

// Document 描述文档。
type Document struct {
	// Area 页面区域。
	Area *PageSize ` + "`json:\"area,omitempty\"`" + `
	// Ignored 没有 json tag，不属于 schema。
	Ignored string
	// Skipped 使用 json:"-" 排除。
	Skipped string ` + "`json:\"-\"`" + `
}

// PageSize 描述尺寸。
type PageSize struct {
	// Name 尺寸名称。
	Name string ` + "`json:\"name,omitempty\"`" + `
}

// Page 描述页面。
type Page struct {
	// Items 页面图元。
	Items []Item ` + "`json:\"items,omitempty\"`" + `
}

// Item 描述图元。
type Item struct {
	// Type 图元类型。
	Type string ` + "`json:\"type,omitempty\"`" + `
	// Items 子图元，可嵌套。
	Items []Item ` + "`json:\"items,omitempty\"`" + `
	// Fill 是否填充。
	Fill *bool ` + "`json:\"fill,omitempty\"`" + `
}

// Unreachable 不从 Manifest 可达，不应出现在结果里。
type Unreachable struct {
	// Name 名称。
	Name string ` + "`json:\"name,omitempty\"`" + `
}
`

func TestExtractWalksReachableTypes(t *testing.T) {
	schema, err := Extract(writeSource(t, nestedSource))
	if err != nil {
		t.Fatalf("提取失败: %v", err)
	}
	names := make([]string, 0, len(schema.Types))
	for _, typed := range schema.Types {
		names = append(names, typed.Name)
	}
	// Unreachable 不可达，BuildOptions 这类辅助类型同理。
	if strings.Contains(strings.Join(names, ","), "Unreachable") {
		t.Fatalf("不可达类型不应出现: %v", names)
	}
	lookup := func(name string) TypeDoc {
		t.Helper()
		typed, ok := schema.Lookup(name)
		if !ok {
			t.Fatalf("缺少结构体 %s，现有 %v", name, names)
		}
		return typed
	}
	if root := schema.Types[0]; root.Name != "Manifest" || root.Path != "" {
		t.Fatalf("根结构体 = {Name:%q Path:%q}", root.Name, root.Path)
	}
	// 广度优先：Document 在深度 1，Page 也在深度 1，Item 深度 2。
	if got := lookup("Document").Path; got != "document" {
		t.Fatalf("Document 路径 = %q", got)
	}
	if got := lookup("Page").Path; got != "pages[]" {
		t.Fatalf("Page 路径 = %q", got)
	}
	if got := lookup("Item").Path; got != "pages[].items[]" {
		t.Fatalf("Item 路径 = %q", got)
	}
	if got := lookup("PageSize").Path; got != "document.area" {
		t.Fatalf("PageSize 路径 = %q，指针字段不应改变路径", got)
	}
	if len(schema.Recursive) == 0 {
		t.Fatal("Item 的自引用应记入 Recursive")
	}
}

func TestExtractFieldMetadata(t *testing.T) {
	schema, err := Extract(writeSource(t, nestedSource))
	if err != nil {
		t.Fatalf("提取失败: %v", err)
	}
	document, _ := schema.Lookup("Document")
	// 没有 json tag 和 json:"-" 的字段都不属于 schema。
	if len(document.Fields) != 1 {
		names := make([]string, 0, len(document.Fields))
		for _, field := range document.Fields {
			names = append(names, field.Name)
		}
		t.Fatalf("Document 字段 = %v，应只保留 area", names)
	}
	root := schema.Types[0]
	byName := map[string]Field{}
	for _, field := range root.Fields {
		byName[field.Name] = field
	}
	if !byName["version"].Optional {
		t.Fatal("version 带 omitempty，Optional 应为 true")
	}
	if byName["document"].Optional {
		t.Fatal("document 没有 omitempty，Optional 应为 false")
	}
	if byName["pages"].Type != "[]Page" {
		t.Fatalf("pages 类型 = %q", byName["pages"].Type)
	}
	item, _ := schema.Lookup("Item")
	if itemType, ok := itemField(t, item, "fill"); !ok || itemType != "*bool" {
		t.Fatalf("Item.fill 类型 = %q，应为 *bool", itemType)
	}
	if _, ok := itemField(t, item, "items"); !ok {
		t.Fatal("Item.items 缺失")
	}
}

func itemField(t *testing.T, typed TypeDoc, name string) (string, bool) {
	t.Helper()
	for _, field := range typed.Fields {
		if field.Name == name {
			return field.Type, true
		}
	}
	return "", false
}

func TestExtractStripsIdentifierPrefix(t *testing.T) {
	schema, err := Extract(writeSource(t, nestedSource))
	if err != nil {
		t.Fatalf("提取失败: %v", err)
	}
	if got := schema.Types[0].Doc; got != "是入口。" {
		t.Fatalf("结构体说明 = %q，应剥离类型名", got)
	}
	if got, _ := itemField2(t, schema, "Manifest", "version"); got != "指定版本。" {
		t.Fatalf("字段说明 = %q，应剥离字段名", got)
	}
}

func itemField2(t *testing.T, schema Schema, typeName, fieldName string) (string, bool) {
	t.Helper()
	typed, ok := schema.Lookup(typeName)
	if !ok {
		t.Fatalf("缺少结构体 %s", typeName)
	}
	for _, field := range typed.Fields {
		if field.Name == fieldName {
			return field.Doc, true
		}
	}
	return "", false
}

func TestExtractRejectsMissingRoot(t *testing.T) {
	path := writeSource(t, "package manifest\n\n// Other 其他。\ntype Other struct{}\n")
	if _, err := Extract(path); err == nil || !strings.Contains(err.Error(), RootStruct) {
		t.Fatalf("错误 = %v，应指出缺少 %s", err, RootStruct)
	}
}

func TestExtractRejectsUnparsableSource(t *testing.T) {
	if _, err := Extract(writeSource(t, "package manifest\n\nfunc (")); err == nil {
		t.Fatal("非法源码应返回错误")
	}
}
