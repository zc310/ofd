package validator

import (
	"bytes"
	"testing"
)

// 直接采用独立 XML 测试表63及 GB/T 36905 第6章形式，不只依赖创建器自编码。
func TestCustomTagTypeIDValidationIsExplicitAndKeepsOtherConstraints(t *testing.T) {
	for _, test := range []struct {
		name       string
		entry      string
		enabled    bool
		wantErrors bool
	}{
		{name: "基础模式保留附录A约束", entry: `<CustomTag TypeID="cert"><FileLoc>data.xml</FileLoc></CustomTag>`, wantErrors: true},
		{name: "启用类型标引", entry: `<CustomTag TypeID="cert"><FileLoc>data.xml</FileLoc></CustomTag>`, enabled: true},
		{name: "兼容命名空间标引", entry: `<CustomTag NameSpace="urn:test"><FileLoc>data.xml</FileLoc></CustomTag>`, enabled: true},
		{name: "两个属性独立", entry: `<CustomTag NameSpace="urn:test" TypeID="cert"><FileLoc>data.xml</FileLoc></CustomTag>`, enabled: true},
		{name: "支持包内绝对路径", entry: `<CustomTag TypeID="cert"><FileLoc>/Doc_0/Tags/data.xml</FileLoc></CustomTag>`, enabled: true},
		{name: "类型标识不限于证照", entry: `<CustomTag TypeID="invoice"><FileLoc>data.xml</FileLoc></CustomTag>`, enabled: true},
		{name: "允许同业务类别多个标引", entry: `<CustomTag TypeID="cert"><FileLoc>data.xml</FileLoc></CustomTag><CustomTag TypeID="cert"><FileLoc>data.xml</FileLoc></CustomTag>`, enabled: true},
		{name: "不能缺少标识", entry: `<CustomTag><FileLoc>data.xml</FileLoc></CustomTag>`, enabled: true, wantErrors: true},
		{name: "不能使用纯空白标识", entry: `<CustomTag NameSpace=" " TypeID=" "><FileLoc>data.xml</FileLoc></CustomTag>`, enabled: true, wantErrors: true},
		{name: "显式空TypeID仍无效", entry: `<CustomTag NameSpace="urn:test" TypeID=""><FileLoc>data.xml</FileLoc></CustomTag>`, enabled: true, wantErrors: true},
		{name: "不能缺少文件位置", entry: `<CustomTag TypeID="cert"/>`, enabled: true, wantErrors: true},
		{name: "不能使用未知属性", entry: `<CustomTag TypeID="cert" Unknown="x"><FileLoc>data.xml</FileLoc></CustomTag>`, enabled: true, wantErrors: true},
		{name: "仍需检查目标存在", entry: `<CustomTag TypeID="cert"><FileLoc>missing.xml</FileLoc></CustomTag>`, enabled: true, wantErrors: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := customTagTypeIDPackage(t, test.entry)
			instance, err := New(WithCustomTagTypeID(test.enabled))
			if err != nil {
				t.Fatal(err)
			}
			report := instance.ValidateReader(t.Context(), bytes.NewReader(data), "custom-tags.ofd")
			if report.HasErrors() != test.wantErrors || (!test.wantErrors && report.Summary.Warnings != 0) {
				t.Fatalf("校验结论错误: %+v", report.Issues)
			}
		})
	}
}

func TestCustomTagTypeIDStructuralValidationStillRequiresIdentifier(t *testing.T) {
	data := customTagTypeIDPackage(t, `<CustomTag><FileLoc>data.xml</FileLoc></CustomTag>`)
	instance, err := New(WithCustomTagTypeID(true), WithMode(ModeStructural))
	if err != nil {
		t.Fatal(err)
	}
	report := instance.ValidateReader(t.Context(), bytes.NewReader(data), "missing-identifier.ofd")
	for _, issue := range report.Issues {
		if issue.Code == "semantic.custom_tag_identifier_missing" {
			if issue.Stage != StageSemantic || issue.File != "Doc_0/Tags/CustomTags.xml" || len(issue.Clause) == 0 {
				t.Fatalf("标引问题缺少定位或依据: %+v", issue)
			}
			return
		}
	}
	t.Fatalf("跳过 XSD 不应放过缺少标识的标引: %+v", report.Issues)
}

func customTagTypeIDPackage(t *testing.T, entry string) []byte {
	t.Helper()
	return makeArchive(t, map[string]string{
		"OFD.xml":                   `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>cert-test</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`,
		"Doc_0/Document.xml":        `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>1</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/></Pages><CustomTags>Tags/CustomTags.xml</CustomTags></Document>`,
		"Doc_0/Page.xml":            `<Page xmlns="http://www.ofdspec.org/2016"/>`,
		"Doc_0/Tags/CustomTags.xml": `<CustomTags xmlns="http://www.ofdspec.org/2016">` + entry + `</CustomTags>`,
		"Doc_0/Tags/data.xml":       `<Certificate xmlns="urn:test:certificate"/>`,
	})
}
