package models

import (
	"encoding/json"
	"encoding/xml"
	"testing"
)

func TestOptionalBool_DefaultTrue(t *testing.T) {
	var ob OptionalBool
	if ob.Value(true) != true {
		t.Fatal("expected default true when not set")
	}
	if ob.Value(false) != false {
		t.Fatal("expected default false when not set")
	}
	if ob.IsSet() {
		t.Fatal("expected IsSet to be false when not set")
	}
}

func TestOptionalBool_SetTrue(t *testing.T) {
	ob := NewOptionalBool(true)
	if !ob.Value(false) {
		t.Fatal("expected true when set to true")
	}
	if !ob.IsSet() {
		t.Fatal("expected IsSet to be true")
	}
}

func TestOptionalBool_SetFalse(t *testing.T) {
	ob := NewOptionalBool(false)
	if ob.Value(true) {
		t.Fatal("expected false when set to false")
	}
	if !ob.IsSet() {
		t.Fatal("expected IsSet to be true")
	}
}

func TestOptionalBool_Reset(t *testing.T) {
	ob := NewOptionalBool(false)
	ob.Reset()
	if ob.IsSet() {
		t.Fatal("expected IsSet to be false after reset")
	}
	if ob.Value(true) != true {
		t.Fatal("expected default true after reset")
	}
}

func TestOptionalBool_XMLAttr(t *testing.T) {
	type TestStruct struct {
		Visible OptionalBool `xml:"Visible,attr,omitempty"`
	}

	// 测试解析 true
	xmlTrue := `<Test Visible="true"/>`
	var s1 TestStruct
	if err := xml.Unmarshal([]byte(xmlTrue), &s1); err != nil {
		t.Fatal(err)
	}
	if !s1.Visible.Value(false) {
		t.Fatal("expected Visible=true")
	}

	// 测试解析 false
	xmlFalse := `<Test Visible="false"/>`
	var s2 TestStruct
	if err := xml.Unmarshal([]byte(xmlFalse), &s2); err != nil {
		t.Fatal(err)
	}
	if s2.Visible.Value(true) {
		t.Fatal("expected Visible=false")
	}

	// 测试未设置
	xmlNone := `<Test/>`
	var s3 TestStruct
	if err := xml.Unmarshal([]byte(xmlNone), &s3); err != nil {
		t.Fatal(err)
	}
	if !s3.Visible.Value(true) {
		t.Fatal("expected default true when not set")
	}
}

func TestOptionalBoolJSONPreservesUnsetTrueAndFalse(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value OptionalBool
		want  string
	}{
		{name: "未设置", want: "null"},
		{name: "显式true", value: NewOptionalBool(true), want: "true"},
		{name: "显式false", value: NewOptionalBool(false), want: "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.value)
			if err != nil || string(data) != tc.want {
				t.Fatalf("编码 = %s, %v，期望 %s", data, err, tc.want)
			}
			got := NewOptionalBool(true)
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if got.IsSet() != tc.value.IsSet() || got.Value(false) != tc.value.Value(false) {
				t.Fatalf("JSON 往返未保留可选布尔值: %+v", got)
			}
		})
	}
}

func TestOptionalBoolJSONRejectsInvalidTypesWithoutChangingValue(t *testing.T) {
	for _, data := range []string{`"false"`, `0`, `{}`, `[]`} {
		value := NewOptionalBool(true)
		if err := json.Unmarshal([]byte(data), &value); err == nil {
			t.Fatalf("未拒绝非法布尔值 %s", data)
		}
		if !value.IsSet() || !value.Value(false) {
			t.Fatalf("解析 %s 失败后原值被修改", data)
		}
	}
}
