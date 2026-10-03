package schema

import (
	"io"
	"io/fs"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/knroy/go-xml/xsd"

	"github.com/zc310/ofd/internal/spec"
)

func TestDefaultCompilesAllRootSchemas(t *testing.T) {
	set, err := Default()
	if err != nil {
		t.Fatalf("compile embedded schemas: %v", err)
	}
	for root := range roots {
		if schema, ok := set.Schema(root); !ok || schema == nil {
			t.Errorf("missing compiled schema for %s", root)
		}
	}
}

func TestCatalogResolvesIncludedSchemas(t *testing.T) {
	resolver := xsd.NewCatalogResolver()
	for _, filename := range []string{"Definitions.xsd", "Page.xsd"} {
		source, err := Files.ReadFile(path.Join("xsd", filename))
		if err != nil {
			t.Fatal(err)
		}
		resolver.Add(namespace, source, filename, "ofd://schema/"+filename)
	}
	reader, resolved, err := resolver.Resolve("", "Definitions.xsd", "ofd://schema/Page.xsd")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "ofd://schema/Definitions.xsd" {
		t.Fatalf("resolved name = %q", resolved)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("resolved included schema is empty")
	}
}

func TestPageSchemaCompilesWithCatalog(t *testing.T) {
	resolver := xsd.NewCatalogResolver()
	for _, filename := range []string{"Definitions.xsd", "Page.xsd"} {
		source, err := Files.ReadFile(path.Join("xsd", filename))
		if err != nil {
			t.Fatal(err)
		}
		resolver.Add(namespace, source, filename, "ofd://schema/"+filename)
	}
	if _, err := xsd.LoadFile("Page.xsd", xsd.Options{Resolver: resolver}); err != nil {
		t.Fatalf("compile Page.xsd: %v", err)
	}
}

func TestExtensionsSchemaCompilesWithAllCatalogEntries(t *testing.T) {
	resolver := xsd.NewCatalogResolver()
	definitions, err := Files.ReadFile(path.Join("xsd", "Definitions.xsd"))
	if err != nil {
		t.Fatal(err)
	}
	resolver.Add(namespace, definitions, "Definitions.xsd", "ofd://schema/Definitions.xsd")
	for _, filename := range roots {
		source, err := Files.ReadFile(path.Join("xsd", filename))
		if err != nil {
			t.Fatal(err)
		}
		resolver.Add(namespace, source, filename, "ofd://schema/"+filename)
	}
	reader, resolved, err := resolver.Resolve("", "Definitions.xsd", "ofd://schema/Extensions.xsd")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != "ofd://schema/Definitions.xsd" || len(data) == 0 {
		t.Fatalf("Definitions.xsd resolved as %q with %d bytes", resolved, len(data))
	}
	if _, err := xsd.LoadFile("Extensions.xsd", xsd.Options{Resolver: resolver}); err != nil {
		t.Fatalf("compile Extensions.xsd: %v", err)
	}
}

// docTypeAttributePattern 匹配 DocType 属性声明的整个块（含匿名 simpleType）。
var docTypeAttributePattern = regexp.MustCompile(`(?s)<xs:attribute name="DocType".*?</xs:attribute>`)

// xsdBody 去掉 XML 声明、注释与首尾空白，只留模式内容。
func xsdBody(t *testing.T, name string) string {
	t.Helper()
	source, err := fs.ReadFile(Files, path.Join("xsd", name))
	if err != nil {
		t.Fatal(err)
	}
	text := commentPattern.ReplaceAllString(string(source), "")
	if idx := strings.Index(text, "<xs:schema"); idx >= 0 {
		text = text[idx:]
	}
	// 官方标准文件多为 CRLF，overlay 可能相反；行尾不属于模式语义，先归一化。
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.TrimSpace(text)
}

// TestProfileSchemaDiffersOnlyInDocType 保护 profile overlay 与官方模式的差异
// 仅限 DocType 属性声明。overlay 若与 OFD.xsd 出现其它差异，说明上游模式更新
// 后两者漂移，overlay 会静默放过本该由官方模式拦下的问题。
func TestProfileSchemaDiffersOnlyInDocType(t *testing.T) {
	officialBody := xsdBody(t, ofdRoot)
	overlayBody := xsdBody(t, ofdProfileRoot)

	// 剥掉 DocType 属性块后，两者必须逐字节相同。
	strippedOfficial := strings.TrimSpace(docTypeAttributePattern.ReplaceAllString(officialBody, ""))
	strippedOverlay := strings.TrimSpace(docTypeAttributePattern.ReplaceAllString(overlayBody, ""))
	if strippedOfficial != strippedOverlay {
		t.Fatalf("overlay 与官方模式在 DocType 之外存在差异：\n--- 官方 ---\n%s\n--- overlay ---\n%s",
			strippedOfficial, strippedOverlay)
	}

	officialAttr := docTypeAttributePattern.FindString(officialBody)
	overlayAttr := docTypeAttributePattern.FindString(overlayBody)
	if officialAttr == "" || overlayAttr == "" {
		t.Fatal("未能定位 DocType 属性声明")
	}
	if !strings.Contains(officialAttr, `fixed="OFD"`) {
		t.Errorf("官方模式的 DocType 应保留 fixed=\"OFD\" 约束：%s", officialAttr)
	}
	if strings.Contains(overlayAttr, "fixed=") {
		t.Errorf("overlay 未移除 DocType 的 fixed 约束：%s", overlayAttr)
	}
	for _, value := range spec.DocTypes {
		want := `<xs:enumeration value="` + value + `" />`
		if !strings.Contains(overlayAttr, want) {
			t.Errorf("overlay 未接受 DocType 取值 %q：%s", value, overlayAttr)
		}
	}
	if got := strings.Count(overlayAttr, "<xs:enumeration"); got != len(spec.DocTypes) {
		t.Errorf("overlay 的 DocType 枚举行数 = %d, want %d", got, len(spec.DocTypes))
	}
}

var commentPattern = regexp.MustCompile(`(?s)<!--.*?-->`)

// TestSchemaForSelectsOverlayByDocType 保护按 DocType 选择模式：基础与未知取值
// 走官方模式，已知 profile 走 overlay，其余根元素两者共用。
func TestSchemaForSelectsOverlayByDocType(t *testing.T) {
	set, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	base, ok := set.SchemaFor("OFD", spec.DocTypeOFD)
	if !ok {
		t.Fatal("官方 OFD 模式缺失")
	}
	empty, _ := set.SchemaFor("OFD", "")
	if empty != base {
		t.Error("空 DocType 应与基础 profile 共用官方模式")
	}
	for _, unknown := range []string{"ofd-a", "OFD_A", "OFD-X"} {
		got, ok := set.SchemaFor("OFD", unknown)
		if !ok || got != base {
			t.Errorf("未知 DocType %q 应回落到官方模式", unknown)
		}
	}
	for _, profile := range []string{spec.DocTypeOFDA, spec.DocTypeOFDH} {
		overlay, ok := set.SchemaFor("OFD", profile)
		if !ok {
			t.Fatalf("profile %q 没有 overlay", profile)
		}
		if overlay == base {
			t.Errorf("profile %q 仍返回官方模式", profile)
		}
	}
	// overlay 只在 OFD 根元素上生效，其他根元素必须仍是官方模式。
	for root := range roots {
		if root == "OFD" {
			continue
		}
		official, _ := set.SchemaFor(root, spec.DocTypeOFD)
		for _, profile := range []string{spec.DocTypeOFDA, spec.DocTypeOFDH} {
			got, _ := set.SchemaFor(root, profile)
			if got != official {
				t.Errorf("根元素 %s 在 profile %q 下不应更换模式", root, profile)
			}
		}
	}
}
