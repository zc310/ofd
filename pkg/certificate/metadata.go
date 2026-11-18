package certificate

import (
	"fmt"
	"slices"
	"strings"

	"github.com/zc310/ofd/pkg/spec"
)

// Metadata 是有序的原始自定义元数据快照，不改写值或丢弃未知项及重复项。
type Metadata struct {
	entries []spec.CustomData
}

// Issue 是本地格式检查结果。Index 对应输入条目，缺少条目时为 -1。
// Warning 为 true 表示当前检查范围无法作出确定判定，不是已确认的格式错误。
type Issue struct {
	// Code 为稳定的问题类别，不含校验器使用的前缀。
	Code string `json:"code"`
	// Field 为标准英文字段名，未知字段保留原名。
	Field string `json:"field"`
	// Index 为原始条目下标，缺失项为 -1。
	Index int `json:"index"`
	// Message 为不含原始主体代码的问题说明。
	Message string `json:"message"`
	// Clause 为本项检查依据的标准条款。
	Clause string `json:"clause"`
	// Warning 标记未能确定判定的检查，而非已确认错误。
	Warning bool `json:"warning,omitempty"`
}

type field struct {
	name     string
	short    string
	required bool
	clause   string
	multiple bool
}

// 必选性来自 GB/T 36903 §5.2、GB/T 36902 §5.2.2 和 GB/T 36905 §4 c)；
// 不把目录信息中的其他必选项强加给每份证照。
func metadataFields() []field {
	return []field{
		{name: "certificateName", short: "ZZMC", required: true, clause: "GB/T 36903—2018 §5.2.1"},
		{name: "certificateTypeCode", short: "ZZLXDM", required: true, clause: "GB/T 36902—2018 §5.2.2、§6.3"},
		{name: "certificateNumber", short: "ZZBH", required: true, clause: "GB/T 36903—2018 §5.2.3"},
		{name: "certificateIdentifier", required: true, clause: "GB/T 36905—2018 §4 c)、GB/T 36904—2018 §5"},
		{name: "certificateIssuingAuthorityName", short: "ZZBFJG", required: true, multiple: true, clause: "GB/T 36903—2018 §5.2.5"},
		{name: "certificateIssuingAuthorityCode", short: "ZZBFJGDM", multiple: true, clause: "GB/T 36903—2018 §5.2.6"},
		{name: "certificateIssuedDate", short: "ZZBFRQ", required: true, clause: "GB/T 36903—2018 §5.2.7"},
		{name: "certificateHolderName", short: "CZZT", required: true, multiple: true, clause: "GB/T 36903—2018 §5.2.8"},
		{name: "certificateHolderCode", short: "CZZTDM", multiple: true, clause: "GB/T 36903—2018 §5.2.9"},
		{name: "certificateHolderTypeName", short: "CZZTDMLX", multiple: true, clause: "GB/T 36903—2018 §5.2.10"},
		{name: "certificateHolderTypeCode", short: "CZZTDMLXDM", multiple: true, clause: "GB/T 36903—2018 §5.2.11"},
		{name: "certificateEffectiveDate", short: "ZZYXQQSRQ", clause: "GB/T 36903—2018 §5.2.12"},
		{name: "certificateExpiringDate", short: "ZZYXQJZRQ", clause: "GB/T 36903—2018 §5.2.13"},
	}
}

// ParseMetadata 复制已解码的 OFD DocInfo/CustomDatas；解析与校验分开，便于查看无效原文。
func ParseMetadata(entries []spec.CustomData) Metadata {
	return Metadata{entries: slices.Clone(entries)}
}

// Entries 返回原始条目的副本。
func (m Metadata) Entries() []spec.CustomData { return slices.Clone(m.entries) }

// Values 按顺序返回同一字段的全部声明值，标准英文名与短名视为同一字段。
// 不拆分 ^ 多值，不把 certificateHolderCategory 当作 certificateHolderTypeName。
func (m Metadata) Values(name string) []string {
	var result []string
	for _, entry := range m.entries {
		if canonical(entry.Name) == canonical(name) {
			result = append(result, entry.Value)
		}
	}
	return result
}

func canonical(name string) string {
	for _, f := range metadataFields() {
		if name == f.name || (f.short != "" && name == f.short) {
			return f.name
		}
	}
	return name
}

type metadataCheck struct {
	values  map[string]string
	indices map[string]int
	issues  []Issue
}

func (c *metadataCheck) add(name, code, clause, message string) {
	index, ok := c.indices[name]
	if !ok {
		index = -1
	}
	c.issues = append(c.issues, Issue{Field: name, Code: code, Clause: clause, Message: message, Index: index})
}

// Validate 检查原件基础元数据的必选项、重复别名、标识、日期及多值对应关系。
// 不要求可选项全部出现，不验证行业扩展的定义或加注件业务条件。
func (m Metadata) Validate() []Issue {
	c := &metadataCheck{values: make(map[string]string), indices: make(map[string]int)}
	for index, entry := range m.entries {
		name := canonical(entry.Name)
		if _, found := c.indices[name]; found {
			c.issues = append(c.issues, Issue{Code: "duplicate", Field: name, Index: index,
				Message: "同一元数据字段存在重复声明或英文名与短名同时声明", Clause: "GB/T 36905—2018 §5、GB/T 36903—2018 §6.2 a)"})
			continue
		}
		c.indices[name] = index
		c.values[name] = entry.Value
		if entry.Name == "KZ_" {
			c.add(name, "extension_name", "GB/T 36903—2018 §6.1 a)", "扩展名称不能仅包含 KZ_ 前缀")
		}
	}
	for _, f := range metadataFields() {
		value, present := c.values[f.name]
		if (f.required || present) && strings.TrimSpace(value) == "" {
			c.add(f.name, "missing", f.clause, "元数据字段缺失或取值为空")
		} else if present && f.multiple {
			for part := range strings.SplitSeq(value, "^") {
				if strings.TrimSpace(part) == "" {
					c.add(f.name, "empty_component", f.clause, "以 ^ 分隔的多值字段含空项")
					break
				}
			}
		}
	}
	c.checkCodes()
	c.checkMultiple()
	c.checkDates()
	return c.issues
}

func (c *metadataCheck) checkCodes() {
	if value := c.values["certificateTypeCode"]; value != "" && !validTypeCode(value) {
		c.add("certificateTypeCode", "type_code", "GB/T 36902—2018 §5.2.2、§6.3", "类型代码须为18位定义机构代码和3位大写字母数字分类码，分类码不含 I、O")
	}
	if value := c.values["certificateIssuingAuthorityCode"]; value != "" {
		for code := range strings.SplitSeq(value, "^") {
			if !validCreditCode(code) {
				c.add("certificateIssuingAuthorityCode", "authority_code", "GB/T 36903—2018 §5.2.6", "颁发机构代码须为18位统一社会信用代码字符")
				break
			}
		}
	}
	value := c.values["certificateIdentifier"]
	if value == "" {
		return
	}
	id, err := ParseIdentifier(value)
	if err != nil {
		c.add("certificateIdentifier", "identifier", "GB/T 36904—2018 §5", err.Error())
		return
	}
	if code := c.values["certificateTypeCode"]; code != "" && code != id.TypeCode {
		c.add("certificateTypeCode", "identifier_type_mismatch", "GB/T 36904—2018 §5.3", "元数据类型代码与标识中的类型代码不一致")
	}
	if codes := c.values["certificateIssuingAuthorityCode"]; codes != "" && !slices.Contains(strings.Split(codes, "^"), id.IssuingAuthorityCode) {
		c.add("certificateIssuingAuthorityCode", "identifier_authority_mismatch", "GB/T 36904—2018 §5.4", "标识中的颁发机构代码不在元数据颁发机构代码列表中")
	}
}

func (c *metadataCheck) checkMultiple() {
	for _, pair := range [][2]string{
		{"certificateIssuingAuthorityName", "certificateIssuingAuthorityCode"},
		{"certificateHolderName", "certificateHolderCode"},
		{"certificateHolderName", "certificateHolderTypeName"},
		{"certificateHolderName", "certificateHolderTypeCode"},
	} {
		base, value := c.values[pair[0]], c.values[pair[1]]
		if base != "" && value != "" && len(strings.Split(base, "^")) != len(strings.Split(value, "^")) {
			c.add(pair[1], "multiple_count", "GB/T 36903—2018 §5.2.5—§5.2.11", fmt.Sprintf("多值项数须与 %s 一致，顺序也应对应", pair[0]))
		}
	}
	types := strings.Split(c.values["certificateHolderTypeCode"], "^")
	typeNames := strings.Split(c.values["certificateHolderTypeName"], "^")
	codes := strings.Split(c.values["certificateHolderCode"], "^")
	for index, codeType := range types {
		if (codeType == "001" || codeType == "111") && (index >= len(codes) || strings.TrimSpace(codes[index]) == "") {
			c.add("certificateHolderCode", "holder_code_required", "GB/T 36903—2018 §5.2.9", "使用统一社会信用代码或公民身份号码时，持证主体代码不能为空")
			return
		}
	}
	for index, name := range typeNames {
		if (name == "统一社会信用代码" || name == "公民身份号码") && (index >= len(codes) || strings.TrimSpace(codes[index]) == "") {
			c.add("certificateHolderCode", "holder_code_required", "GB/T 36903—2018 §5.2.9", "使用统一社会信用代码或公民身份号码时，持证主体代码不能为空")
			break
		}
	}
}
