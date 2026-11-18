package certificate

import (
	"slices"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func validEntries() []spec.CustomData {
	return []spec.CustomData{
		{Name: "certificateName", Value: "结构测试证照"},
		{Name: "certificateTypeCode", Value: "11100000000013338W002"},
		{Name: "certificateNumber", Value: "测试-001"},
		{Name: "certificateIdentifier", Value: appendixIdentifier},
		{Name: "certificateIssuingAuthorityName", Value: "示例机构"},
		{Name: "certificateIssuedDate", Value: "2018年7月25日"},
		{Name: "certificateHolderName", Value: "示例主体"},
	}
}

func TestMetadataMinimumRequiredDoesNotRequireDirectoryOrOptionalFields(t *testing.T) {
	if issues := ParseMetadata(validEntries()).Validate(); len(issues) != 0 {
		t.Fatalf("未提供目录字段及可选字段不应失败: %+v", issues)
	}
	for _, f := range metadataFields() {
		if !f.required {
			continue
		}
		entries := slices.DeleteFunc(validEntries(), func(entry spec.CustomData) bool { return entry.Name == f.name })
		issues := ParseMetadata(entries).Validate()
		if len(issues) != 1 || issues[0].Code != "missing" || issues[0].Field != f.name || issues[0].Index != -1 {
			t.Fatalf("必选项缺失未准确定位: %s, %+v", f.name, issues)
		}
	}
}

func TestMetadataShortNamesAndRawSnapshotPreserveAllDeclarations(t *testing.T) {
	entries := validEntries()
	for index := range entries {
		for _, f := range metadataFields() {
			if f.name == entries[index].Name && f.short != "" {
				entries[index].Name = f.short
			}
		}
	}
	entries = append(entries, spec.CustomData{Name: "certificateHolderCategory", Value: "法人和其他组织"},
		spec.CustomData{Name: "KZ_TEST", Value: "扩展"},
		spec.CustomData{Name: "industryDefined", Value: "行业字段"})
	m := ParseMetadata(entries)
	entries[0].Value = "被修改"
	snapshot := m.Entries()
	snapshot[0].Value = "再次修改"
	if got := m.Values("certificateName"); len(got) != 1 || got[0] != "结构测试证照" {
		t.Fatalf("快照被调用方修改: %v", got)
	}
	if len(m.Values("certificateHolderTypeName")) != 0 || m.Values("certificateHolderCategory")[0] != "法人和其他组织" {
		t.Fatal("目录持证主体类别不能映射成持证主体代码类型")
	}
	if issues := m.Validate(); len(issues) != 0 {
		t.Fatalf("标准短名及未知行业字段应保留: %+v", issues)
	}
}

func TestMetadataRejectsDuplicateAliasRatherThanSilentlyPickingOne(t *testing.T) {
	entries := append(validEntries(), spec.CustomData{Name: "ZZMC", Value: "另一个名称"})
	m := ParseMetadata(entries)
	issues := m.Validate()
	if len(m.Values("ZZMC")) != 2 || len(issues) != 1 || issues[0].Code != "duplicate" || issues[0].Index != len(entries)-1 {
		t.Fatalf("重复别名被吞掉: %+v", issues)
	}
}

func TestMetadataFormatsAndMultiValueConditions(t *testing.T) {
	for _, test := range []struct {
		name   string
		values map[string]string
		code   string
	}{
		{name: "纯空白必选项", values: map[string]string{"certificateName": " "}, code: "missing"},
		{name: "显式空可选项", values: map[string]string{"certificateIssuingAuthorityCode": ""}, code: "missing"},
		{name: "错误类型长度", values: map[string]string{"certificateTypeCode": "002"}, code: "type_code"},
		{name: "类型与标识不符", values: map[string]string{"certificateTypeCode": "11100000000013338W003"}, code: "identifier_type_mismatch"},
		{name: "机构与标识不符", values: map[string]string{"certificateIssuingAuthorityCode": "11100000000013338W"}, code: "identifier_authority_mismatch"},
		{name: "错误机构字符", values: map[string]string{"certificateIssuingAuthorityCode": "11320482014138860I"}, code: "authority_code"},
		{name: "空多值项", values: map[string]string{"certificateHolderName": "甲^^乙"}, code: "empty_component"},
		{name: "主体代码项数不符", values: map[string]string{"certificateHolderName": "甲^乙", "certificateHolderCode": "1"}, code: "multiple_count"},
		{name: "类型代码项数不符", values: map[string]string{"certificateHolderName": "甲^乙", "certificateHolderTypeCode": "999"}, code: "multiple_count"},
		{name: "条件必填主体代码", values: map[string]string{"certificateHolderTypeCode": "111"}, code: "holder_code_required"},
		{name: "条件必填信用代码", values: map[string]string{"certificateHolderTypeCode": "001"}, code: "holder_code_required"},
		{name: "中文代码类型条件必填", values: map[string]string{"certificateHolderTypeName": "公民身份号码"}, code: "holder_code_required"},
		{name: "无效日期", values: map[string]string{"certificateIssuedDate": "2023-02-29"}, code: "date_invalid"},
		{name: "有效期倒置", values: map[string]string{"certificateEffectiveDate": "2024-03-01", "certificateExpiringDate": "2024-02-29"}, code: "date_order"},
		{name: "未解释日期不当作格式错误", values: map[string]string{"certificateIssuedDate": "2024-W01-1"}, code: "date_unchecked"},
		{name: "空扩展名", values: map[string]string{"KZ_": "x"}, code: "extension_name"},
	} {
		t.Run(test.name, func(t *testing.T) {
			entries := validEntries()
			// 按固定顺序追加，避免测试依赖 map 迭代顺序。
			for _, name := range sortedKeys(test.values) {
				value := test.values[name]
				found := false
				for index := range entries {
					if entries[index].Name == name {
						entries[index].Value = value
						found = true
					}
				}
				if !found {
					entries = append(entries, spec.CustomData{Name: name, Value: value})
				}
			}
			issues := ParseMetadata(entries).Validate()
			for _, issue := range issues {
				if issue.Code == test.code {
					if issue.Clause == "" || (issue.Index < 0 && issue.Code != "holder_code_required") || (issue.Code == "date_unchecked" && !issue.Warning) {
						t.Fatalf("问题缺少依据、位置或警告标记: %+v", issue)
					}
					return
				}
			}
			t.Fatalf("未报告 %s: %+v", test.code, issues)
		})
	}
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for name := range values {
		keys = append(keys, name)
	}
	slices.Sort(keys)
	return keys
}

func TestMetadataAllowsLongTermAndAlignedMultipleValues(t *testing.T) {
	entries := validEntries()
	entries[4].Value = "机构甲^机构乙"
	entries[6].Value = "主体甲^主体乙"
	entries = append(entries,
		spec.CustomData{Name: "certificateIssuingAuthorityCode", Value: "11100000000013338W^11320482014138860P"},
		spec.CustomData{Name: "certificateHolderCode", Value: "其他代码1^其他代码2"},
		spec.CustomData{Name: "certificateHolderTypeCode", Value: "999^999"},
		spec.CustomData{Name: "certificateExpiringDate", Value: "长期"},
		spec.CustomData{Name: "certificateEffectiveDate", Value: "20240229"})
	if issues := ParseMetadata(entries).Validate(); len(issues) != 0 {
		t.Fatalf("合法多值、长期和闰日不应失败: %+v", issues)
	}
}
