package validator

import (
	"github.com/knroy/go-xml/v2/xdm"
	"github.com/zc310/ofd/pkg/certificate"
	"github.com/zc310/ofd/pkg/spec"
)

// certificateMetadataChecks 只沿实际 OFD.xml/DocBody/DocInfo 读取元数据，
// 不将未引用的 OFD 根或业务标引 XML 中的同名元素当作文档元数据。
func (v *Validator) certificateMetadataChecks(documents map[string]*xmlDocument, report *Report) {
	doc := documents[rootDocument]
	if doc == nil || !doc.ofd || doc.rootName != "OFD" {
		return
	}
	report.addIssue(issueAt(doc.root, SeverityInfo, StageSemantic, "certificate.scope",
		"仅检查证照基础元数据的本地格式；未验证标识唯一性、登记、机构授权、GB 32100 校验位、行业扩展、加注件条件、锁定签名或 GM 码", rootDocument), v.opts.MaxErrors)
	status := "passed"
	bodies := childElements(doc.root, "DocBody")
	if len(bodies) == 0 {
		report.setCheck("certificate_metadata", "skipped")
		return
	}
	for _, body := range bodies {
		var entries []spec.CustomData
		var nodes []*xdm.Node
		// 不依赖 XSD 模式，结构模式也覆盖缺失元数据和重复容器中的条目。
		for _, info := range childElements(body, "DocInfo") {
			for _, custom := range childElements(info, "CustomDatas") {
				for _, node := range childElements(custom, "CustomData") {
					entries = append(entries, spec.CustomData{Name: node.AttrValue("Name"), Value: node.StringValue()})
					nodes = append(nodes, node)
				}
			}
		}
		for _, problem := range certificate.ParseMetadata(entries).Validate() {
			node := body
			if problem.Index >= 0 {
				node = nodes[problem.Index]
			}
			severity := SeverityError
			if problem.Warning {
				severity = SeverityWarning
				if status != "failed" {
					status = "warning"
				}
			} else {
				status = "failed"
			}
			issue := issueAt(node, severity, StageSemantic, "certificate.metadata."+problem.Code, problem.Field+": "+problem.Message, rootDocument)
			issue.Clause = []string{problem.Clause}
			report.addIssue(issue, v.opts.MaxErrors)
		}
	}
	report.setCheck("certificate_metadata", status)
}
