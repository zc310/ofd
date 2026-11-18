package creator

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestCreateCustomTagsPreservesIndependentTypeIDAndNamespace(t *testing.T) {
	tags := []CustomTag{
		{TypeID: "cert", DataName: "first.xml", Data: []byte("<Certificate/>")},
		{TypeID: "cert", DataName: "second.xml", Data: []byte("<Certificate/>")},
		{NameSpace: "urn:certificate", TypeID: "cert", DataName: "both.xml", Data: []byte("<Certificate/>")},
		{NameSpace: "urn:other", DataName: "other.xml", Data: []byte("<Other/>")},
	}
	document := Document{ID: "type-id-test", CustomTags: tags, Pages: []Page{{}}}
	first, err := MarshalWithOptions(document, CreateOptions{Deterministic: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := MarshalWithOptions(document, CreateOptions{Deterministic: true})
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("标引输出不确定: %v", err)
	}
	raw := readArchiveEntry(t, first, "Doc_0/CustomTags/CustomTags.xml")
	if bytes.Contains(raw, []byte(`NameSpace=""`)) {
		t.Fatalf("不应补写空命名空间: %s", raw)
	}
	var decoded spec.CustomTags
	if err := xml.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.CustomTags) != len(tags) {
		t.Fatalf("标引数量错误: %+v", decoded)
	}
	for index, want := range tags {
		got := decoded.CustomTags[index]
		if got.TypeID != want.TypeID || got.NameSpace != want.NameSpace || got.FileLoc.String() != "Data/"+want.DataName {
			t.Fatalf("第 %d 项标引属性被替换或丢失: %+v", index, got)
		}
	}
}

func TestCreateCustomTagsRejectsMissingOrBlankIdentifiers(t *testing.T) {
	for _, tag := range []CustomTag{
		{},
		{NameSpace: " ", TypeID: "\t"},
		{NameSpace: "urn:valid", TypeID: " \t"},
	} {
		tag.Data = []byte("<Data/>")
		_, err := Marshal(Document{ID: "invalid-tag", CustomTags: []CustomTag{tag}, Pages: []Page{{}}})
		if err == nil || !strings.Contains(err.Error(), "TypeID") {
			t.Fatalf("无效标引应被拒绝: %+v, %v", tag, err)
		}
	}
}
