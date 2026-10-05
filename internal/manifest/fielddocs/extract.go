// Package fielddocs 从 manifest 的 schema 源码中提取字段说明，供
// ofd-creator schema 生成字段参考文档，也供生成器产出 fielddocs.go。
//
// 说明文本只存在于 Go 源码的文档注释里，构建产物读不到源码，因此由
// gen-fielddocs 在构建期提取成代码。提取以 Manifest 为根递归，只覆盖
// manifest 的输入结构，BuildOptions 这类辅助类型不会混入。
//
// manifest 的字段路径构成一棵树，而结构体构成一张 DAG：复合对象可以嵌套复合
// 对象，同一个 Item 会在几十条路径下出现。按路径逐个展开会让字段数组合爆炸，
// 因此这里按结构体组织，每个结构体只在首次到达处展开一次，并在 TypeDoc.Path
// 记录到达它的示例路径。
package fielddocs

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// RootStruct 是 manifest schema 的根结构体名。
const RootStruct = "Manifest"

// Field 是一条 manifest 字段的参考信息。
type Field struct {
	// Name 是相对所属结构体的键名，也是 manifest 里的字段名。
	Name string
	// Type 是该字段的 Go 类型，数组和指针保留书写形式。
	Type string
	// Doc 是去掉字段名前缀后的说明文本。
	Doc string
	// Optional 表示该字段的 json tag 带 omitempty，未设置时为零值即不输出。
	Optional bool
}

// TypeDoc 是 manifest 里一个结构体的字段说明。
type TypeDoc struct {
	// Name 是 Go 结构体名，例如 Item。
	Name string
	// Doc 是结构体自身的说明。
	Doc string
	// Path 是从 manifest 根首次到达该结构体的路径，根结构体为空串，例如
	// pages[].items[].fill_color.radial。字段的完整路径是在本路径后追加字段名，
	// 结构体字段还要追加 "[]"（如 pages[].items[]）。
	Path string
	// Fields 是该结构体的直接字段。
	Fields []Field
}

// Schema 是提取出的 manifest 结构体集合，按首次到达顺序排列。
type Schema struct {
	// Root 是根结构体名。
	Root string
	// Types 是全部结构体说明。
	Types []TypeDoc
	// Recursive 列出被多处引用的结构体名，它们只展开一次。
	Recursive []string
}

// Lookup 返回指定结构体的说明。
func (s Schema) Lookup(name string) (TypeDoc, bool) {
	for _, typed := range s.Types {
		if typed.Name == name {
			return typed, true
		}
	}
	return TypeDoc{}, false
}

// FieldCount 返回全部字段总数。
func (s Schema) FieldCount() int {
	total := 0
	for _, typed := range s.Types {
		total += len(typed.Fields)
	}
	return total
}

// Extract 解析 manifest 源码并提取字段说明。filename 只用于错误信息。
func Extract(filename string) (Schema, error) {
	fileset := token.NewFileSet()
	file, err := parser.ParseFile(fileset, filename, nil, parser.ParseComments)
	if err != nil {
		return Schema{}, fmt.Errorf("解析 manifest 源码失败: %w", err)
	}
	declarations := map[string]*ast.StructType{}
	docs := map[string]string{}
	// type X struct{} 上方的注释挂在 GenDecl.Doc 上，只有分组声明
	// （type ( A; B )）里的注释才在 TypeSpec.Doc 上，两处都要看。
	ast.Inspect(file, func(node ast.Node) bool {
		decl, ok := node.(*ast.GenDecl)
		if !ok || decl.Tok != token.TYPE {
			return true
		}
		for _, item := range decl.Specs {
			spec, ok := item.(*ast.TypeSpec)
			if !ok {
				continue
			}
			declared, ok := spec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			declarations[spec.Name.Name] = declared
			if comment := typeSpecDoc(spec, decl.Doc); comment != nil {
				docs[spec.Name.Name] = structDoc(comment, spec.Name.Name)
			}
		}
		return true
	})
	if _, ok := declarations[RootStruct]; !ok {
		return Schema{}, fmt.Errorf("manifest 源码中没有 %s 结构体", RootStruct)
	}
	schema := Schema{Root: RootStruct}
	expand(&schema, declarations, docs)
	return schema, nil
}

// expand 从根结构体出发广度优先地展开全部可达结构体。
//
// 用广度优先而不是深度优先：manifest 里 pages[].items[] 与
// document.annotations[].items[].items[] 都能到达 Item，深度优先会让 Item 从
// 更绕的 annotations 路径展开，参考文档里的示例路径就不如 pages[].items[] 直观。
// 每个结构体只展开一次，重复引用记入 Recursive。
func expand(schema *Schema, declarations map[string]*ast.StructType, docs map[string]string) {
	type item struct {
		name string
		path string
	}
	queue := []item{{name: RootStruct, path: ""}}
	seen := map[string]bool{RootStruct: true}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		typed := TypeDoc{Name: current.name, Doc: docs[current.name], Path: current.path}
		for _, member := range declarations[current.name].Fields.List {
			if len(member.Names) == 0 {
				// 匿名嵌入字段不是 manifest 的输入字段，跳过。
				continue
			}
			key, ok := jsonName(member)
			if !ok {
				// 没有 json tag 的字段不参与序列化，不属于 schema。
				continue
			}
			for _, ident := range member.Names {
				field := Field{
					Name:     key,
					Type:     typeString(member.Type),
					Doc:      docText(member, ident.Name),
					Optional: hasOmitEmpty(member),
				}
				typed.Fields = append(typed.Fields, field)
				nested, suffix, ok := nestedStruct(member.Type, declarations)
				if !ok {
					continue
				}
				if seen[nested] {
					schema.Recursive = append(schema.Recursive, nested)
					continue
				}
				seen[nested] = true
				queue = append(queue, item{name: nested, path: joinPath(current.path, field.Name+suffix)})
			}
		}
		schema.Types = append(schema.Types, typed)
	}
}

// nestedStruct 返回字段类型中需要继续展开的结构体名，以及追加到路径的后缀。
// 数组的后缀是 "[]"，标量和外部类型返回 ok=false。
func nestedStruct(expr ast.Expr, declarations map[string]*ast.StructType) (string, string, bool) {
	switch typed := expr.(type) {
	case *ast.ArrayType:
		// 只支持 []T 这种无长度数组；[N]T 定长数组不是 manifest 的形态。
		if typed.Len != nil {
			return "", "", false
		}
		if name, ok := structName(typed.Elt, declarations); ok {
			return name, "[]", true
		}
	case *ast.StarExpr:
		// *Preferences 这类指针同样要展开；路径不额外标记。
		if name, ok := structName(typed.X, declarations); ok {
			return name, "", true
		}
	case *ast.Ident:
		if _, ok := declarations[typed.Name]; ok {
			return typed.Name, "", true
		}
	case *ast.SelectorExpr:
		// creator.Document 等外部类型不在 manifest schema 内，不再展开。
	}
	return "", "", false
}

// structName 返回类型表达式指向的本包结构体名。
func structName(expr ast.Expr, declarations map[string]*ast.StructType) (string, bool) {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return "", false
	}
	if _, ok := declarations[ident.Name]; !ok {
		return "", false
	}
	return ident.Name, true
}

// jsonName 读取字段的 json tag 名；没有可用名字时返回 false。
func jsonName(member *ast.Field) (string, bool) {
	raw, ok := tagValue(member)
	if !ok {
		return "", false
	}
	name := strings.Split(reflect.StructTag(raw).Get("json"), ",")[0]
	if name == "" || name == "-" {
		return "", false
	}
	return name, true
}

// hasOmitEmpty 判断字段的 json tag 是否带 omitempty。
func hasOmitEmpty(member *ast.Field) bool {
	raw, ok := tagValue(member)
	if !ok {
		return false
	}
	parts := strings.Split(reflect.StructTag(raw).Get("json"), ",")
	return slices.Contains(parts[1:], "omitempty")
}

// tagValue 取出字段的结构体 tag 原文。
func tagValue(member *ast.Field) (string, bool) {
	if member.Tag == nil {
		return "", false
	}
	raw, err := strconv.Unquote(member.Tag.Value)
	if err != nil {
		return "", false
	}
	return raw, true
}

// docText 取字段的文档注释并去掉开头的字段名，得到纯说明文本。
func docText(member *ast.Field, name string) string {
	if member.Doc == nil {
		return ""
	}
	return trimDoc(member.Doc.Text(), name)
}

// structDoc 取结构体的文档注释并去掉开头的类型名。
func structDoc(doc *ast.CommentGroup, name string) string {
	return trimDoc(doc.Text(), name)
}

// trimDoc 去掉注释开头的标识符，避免与字段名列重复。
func trimDoc(text, name string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, name))
}

// typeString 把字段类型表达式还原成可读的 Go 类型写法。
func typeString(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		return "*" + typeString(typed.X)
	case *ast.ArrayType:
		if typed.Len != nil {
			return "[" + typeString(typed.Len) + "]" + typeString(typed.Elt)
		}
		return "[]" + typeString(typed.Elt)
	case *ast.SelectorExpr:
		return typeString(typed.X) + "." + typed.Sel.Name
	case *ast.MapType:
		return "map[" + typeString(typed.Key) + "]" + typeString(typed.Value)
	case *ast.InterfaceType:
		return "any"
	}
	return "?"
}

// joinPath 拼接 manifest 字段路径，根路径为空串时不产生前导点。
func joinPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// typeSpecDoc 返回结构体声明的文档注释。type X struct{} 上方的注释挂在
// GenDecl.Doc 上，只有分组声明里的注释才在 TypeSpec.Doc 上，两处都要看。
func typeSpecDoc(spec *ast.TypeSpec, decl *ast.CommentGroup) *ast.CommentGroup {
	if spec.Doc != nil {
		return spec.Doc
	}
	return decl
}
