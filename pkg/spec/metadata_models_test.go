package spec_test

import (
	"encoding/xml"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestAttachmentDecodePreservesOptionalAttributes(t *testing.T) {
	for _, attrs := range []string{"", ` Visible="true"`, ` Visible="false"`, ` CreationDate="2020-01-02T03:04:05Z" ModDate="2020-01-03T03:04:05Z" Size="0" Visible="false"`} {
		var decoded spec.Attachment
		if err := xml.Unmarshal([]byte(`<Attachment ID="attach_1" Name="附件"`+attrs+`><FileLoc>../Files/a.xml</FileLoc></Attachment>`), &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.ID != "attach_1" || decoded.Name != "附件" || decoded.FileLoc != "../Files/a.xml" {
			t.Fatalf("附件标识、名称或词法路径改变: %+v", decoded)
		}
		if decoded.CreationDate.IsZero() != !strings.Contains(attrs, `CreationDate="`) || decoded.ModDate.IsZero() != !strings.Contains(attrs, `ModDate="`) || (decoded.Size != nil) != strings.Contains(attrs, `Size="`) || decoded.Visible.IsSet() != strings.Contains(attrs, `Visible="`) {
			t.Fatalf("附件可选属性的省略状态改变: %+v", decoded)
		}
		if decoded.Visible.Value(true) != !strings.Contains(attrs, `Visible="false"`) || (decoded.Size != nil && *decoded.Size != 0) {
			t.Fatal("附件显式 false 或零值丢失")
		}
		if !decoded.CreationDate.IsZero() && (decoded.CreationDate.Format("2006-01-02T15:04:05Z07:00") != "2020-01-02T03:04:05Z" || decoded.ModDate.Format("2006-01-02T15:04:05Z07:00") != "2020-01-03T03:04:05Z") {
			t.Fatal("附件日期内容改变")
		}
	}
}

func TestCustomTagsDecodePreservesOrderAndLexicalPaths(t *testing.T) {
	var decoded spec.CustomTags
	input := `<CustomTags><CustomTag NameSpace="urn:first"><SchemaLoc>../Schema/a.xsd</SchemaLoc><FileLoc>Data/../a.xml</FileLoc></CustomTag><CustomTag NameSpace="urn:second" TypeID="legacy"><FileLoc>./b.xml</FileLoc></CustomTag></CustomTags>`
	if err := xml.Unmarshal([]byte(input), &decoded); err != nil {
		t.Fatal(err)
	}
	want := []spec.CustomTag{
		{NameSpace: "urn:first", SchemaLoc: new(spec.StLoc("../Schema/a.xsd")), FileLoc: "Data/../a.xml"},
		{NameSpace: "urn:second", TypeID: "legacy", FileLoc: "./b.xml"},
	}
	if !reflect.DeepEqual(decoded.CustomTags, want) {
		t.Fatalf("自定义标引解码改变了路径、顺序或省略状态: %+v", decoded)
	}
}

func TestAnnotationsDecodePreservesPageOrderAndReferences(t *testing.T) {
	input := `<Annotations><Page PageID="2"><FileLoc>../Annots/2.xml</FileLoc></Page><Page PageID="1"><FileLoc>./1.xml</FileLoc></Page></Annotations>`
	var decoded spec.Annotations
	if err := xml.Unmarshal([]byte(input), &decoded); err != nil {
		t.Fatal(err)
	}
	want := []spec.AnnotPage{{PageID: 2, FileLoc: "../Annots/2.xml"}, {PageID: 1, FileLoc: "./1.xml"}}
	if !reflect.DeepEqual(decoded.Pages, want) {
		t.Fatalf("注释索引解码改变了顺序或引用: %+v", decoded)
	}
}

func TestMetadataRootsDecodeDefaultAndPrefixedNamespace(t *testing.T) {
	for _, tc := range []struct {
		name     string
		newValue func() any
	}{
		{name: "Attachments", newValue: func() any { return &spec.Attachments{} }},
		{name: "Annotations", newValue: func() any { return &spec.Annotations{} }},
		{name: "CustomTags", newValue: func() any { return &spec.CustomTags{} }},
	} {
		for _, input := range []string{
			`<` + tc.name + ` xmlns="` + spec.Namespace + `"></` + tc.name + `>`,
			`<ofd:` + tc.name + ` xmlns:ofd="` + spec.Namespace + `"></ofd:` + tc.name + `>`,
		} {
			value := tc.newValue()
			if err := xml.Unmarshal([]byte(input), value); err != nil {
				t.Fatal(err)
			}
			name, ok := reflect.TypeAssert[xml.Name](reflect.ValueOf(value).Elem().FieldByName("XMLName"))
			if !ok {
				t.Fatalf("%T 的根名称字段不是 xml.Name", value)
			}
			if name.Local != tc.name || name.Space != spec.Namespace {
				t.Fatalf("%s 根节点的命名空间解码丢失: %+v", tc.name, name)
			}
		}
	}
}

func TestDeclarationModelsDoNotImplementXMLMarshaler(t *testing.T) {
	for _, value := range []any{
		spec.Document{}, spec.VPreferences{}, spec.Attachments{}, spec.Attachment{},
		spec.Annotations{}, spec.CustomTags{}, spec.Palette{},
	} {
		for _, model := range []any{value, reflect.New(reflect.TypeOf(value)).Interface()} {
			if _, ok := model.(xml.Marshaler); ok {
				t.Errorf("%T 不应自行实现整结构 XML 编码，创建产物使用 pkg/creator", model)
			}
		}
	}
}
