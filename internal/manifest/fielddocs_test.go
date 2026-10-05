package manifest_test

import (
	"testing"

	"github.com/zc310/ofd/internal/manifest"
	"github.com/zc310/ofd/internal/manifest/fielddocs"
)

// schemaSource 是被提取的 manifest schema 源码，相对本包目录。
const schemaSource = "manifest.go"

// TestSchemaDocsMatchesSource 是同步守卫：SchemaDocs 由 go generate 从
// manifest.go 的文档注释提取，一旦改了字段却忘了重新生成，这里就会失败。
func TestSchemaDocsMatchesSource(t *testing.T) {
	source, err := fielddocs.Extract(schemaSource)
	if err != nil {
		t.Fatalf("提取 manifest 字段说明失败: %v", err)
	}
	if len(source.Types) != len(manifest.SchemaDocs) {
		t.Fatalf("结构体数量 = %d，生成文件里是 %d；改动 manifest 字段后请执行 go generate ./internal/manifest",
			len(source.Types), len(manifest.SchemaDocs))
	}
	for index, want := range source.Types {
		got := manifest.SchemaDocs[index]
		if got.Name != want.Name || got.Path != want.Path || got.Doc != want.Doc {
			t.Fatalf("结构体 %d 不一致:\n提取 = {Name:%q Path:%q Doc:%q}\n生成 = {Name:%q Path:%q Doc:%q}",
				index, want.Name, want.Path, want.Doc, got.Name, got.Path, got.Doc)
		}
		if len(want.Fields) != len(got.Fields) {
			t.Fatalf("%s 字段数量 = %d，生成文件里是 %d", want.Name, len(want.Fields), len(got.Fields))
		}
		for position, field := range want.Fields {
			actual := got.Fields[position]
			if actual.Name != field.Name || actual.Type != field.Type ||
				actual.Doc != field.Doc || actual.Optional != field.Optional {
				t.Fatalf("%s 第 %d 个字段不一致:\n提取 = %+v\n生成 = %+v", want.Name, position, field, actual)
			}
		}
	}
}

// TestSchemaDocsCoversEveryField 确认每个字段都带说明，避免参考文档出现空行。
func TestSchemaDocsCoversEveryField(t *testing.T) {
	for _, typed := range manifest.SchemaDocs {
		if typed.Name == "" {
			t.Fatal("存在没有名字的结构体")
		}
		if len(typed.Fields) == 0 {
			t.Fatalf("%s 没有任何字段", typed.Name)
		}
		for _, field := range typed.Fields {
			if field.Doc == "" {
				t.Fatalf("%s.%s 缺少字段说明", typed.Name, field.Name)
			}
			if field.Type == "" || field.Type == "?" {
				t.Fatalf("%s.%s 的类型无法识别: %q", typed.Name, field.Name, field.Type)
			}
		}
	}
}

// TestSchemaDocsRootIsManifest 确认根结构体的路径为空串，示例路径不带前导点。
func TestSchemaDocsRootIsManifest(t *testing.T) {
	root := manifest.SchemaDocs[0]
	if root.Name != "Manifest" || root.Path != "" {
		t.Fatalf("首个结构体 = {Name:%q Path:%q}，期望 Manifest 且路径为空", root.Name, root.Path)
	}
	for _, typed := range manifest.SchemaDocs[1:] {
		if typed.Path == "" {
			t.Fatalf("%s 的示例路径为空", typed.Name)
		}
		if typed.Path[0] == '.' {
			t.Fatalf("%s 的示例路径 %q 不应带前导点", typed.Name, typed.Path)
		}
	}
}

// TestSchemaDocsTracksRecursiveType 确认递归结构体只展开一次且记录了自引用字段。
func TestSchemaDocsTracksRecursiveType(t *testing.T) {
	var item manifest.TypeDoc
	found := false
	for _, typed := range manifest.SchemaDocs {
		if typed.Name == "Item" {
			item, found = typed, true
			break
		}
	}
	if !found {
		t.Fatal("SchemaDocs 里没有 Item 结构体")
	}
	for _, field := range item.Fields {
		if field.Name == "items" {
			if field.Type != "[]Item" {
				t.Fatalf("Item.items 类型 = %q，期望 []Item", field.Type)
			}
			return
		}
	}
	t.Fatal("Item 结构体里没有 items 自引用字段")
}
