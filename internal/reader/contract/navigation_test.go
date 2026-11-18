package contract

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/reader"
)

func TestCustomTagContractPreservesOptionalTypeID(t *testing.T) {
	tags := CustomTags([]reader.CustomTagTree{{TypeID: "cert"}, {Namespace: "urn:other"}})
	if tags[0]["type_id"] != "cert" || tags[0]["namespace"] != "" {
		t.Fatalf("业务类型与命名空间混淆: %+v", tags[0])
	}
	if _, exists := tags[1]["type_id"]; exists {
		t.Fatalf("无 TypeID 的旧标引不应增加空字段: %+v", tags[1])
	}
}

func TestNavigationContractPreservesEmptyListsTargetsAndAttributeNames(t *testing.T) {
	for _, value := range []any{Versions(nil), CustomTags(nil)} {
		data, err := json.Marshal(value)
		if err != nil || string(data) != "[]" {
			t.Fatalf("空导航清单 = %s, %v", data, err)
		}
	}
	root := &reader.CustomTagNode{Name: "标签", Text: "<unsafe>", Attributes: map[string]string{"__proto__": "text"}, Target: &reader.CustomTagTarget{Page: 1, ObjectID: 2, ObjectText: "正文"}}
	tags := CustomTags([]reader.CustomTagTree{{Root: root}})
	data, err := json.Marshal(tags)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"object_id":2`, `"object_text":"正文"`, `"children":[]`, `"__proto__":"text"`} {
		if !strings.Contains(string(data), key) {
			t.Fatalf("标签字段缺少 %s: %s", key, data)
		}
	}
	root.Attributes["__proto__"] = "changed"
	again, err := json.Marshal(tags)
	if err != nil || string(again) != string(data) {
		t.Fatalf("契约不应引用可变属性: %v", err)
	}
	data, err = json.Marshal(Versions([]reader.VersionInfo{{ID: "v1"}}))
	if err != nil || !strings.Contains(string(data), `"pages":[]`) || !strings.Contains(string(data), `"doc_root":""`) {
		t.Fatalf("版本字段 = %s, %v", data, err)
	}
}
