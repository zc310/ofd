package validator

import (
	"bytes"
	"strings"
	"testing"
)

// 元数据直接写成 XML，避免只测试创建器与读取器共享的错误假设。
const certificateMetadataXML = `<CustomData Name="ZZMC">测试证照</CustomData>
<CustomData Name="ZZLXDM">11100000000013338W002</CustomData>
<CustomData Name="ZZBH">测试-001</CustomData>
<CustomData Name="certificateIdentifier">1.2.156.3005.2.11100000000013338W002.11320482014138860P.201807250201.001.M</CustomData>
<CustomData Name="ZZBFJG">测试机构</CustomData>
<CustomData Name="ZZBFRQ">2018年7月25日</CustomData>
<CustomData Name="CZZT">测试主体</CustomData>
<CustomData Name="certificateHolderCategory">法人和其他组织</CustomData>
<CustomData Name="ZZYXQJZRQ">长期</CustomData>`

func certificateArchive(t *testing.T, metadata string, extraBody bool) []byte {
	t.Helper()
	body := `<DocBody><DocInfo><DocID>cert-test</DocID><CustomDatas>` + metadata + `</CustomDatas></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody>`
	if extraBody {
		body += `<DocBody><DocInfo><DocID>second</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody>`
	}
	return makeArchive(t, map[string]string{
		"OFD.xml":            `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD">` + body + `</OFD>`,
		"Doc_0/Document.xml": `<Document xmlns="http://www.ofdspec.org/2016"><CommonData><MaxUnitID>2</MaxUnitID><PageArea><PhysicalBox>0 0 210 297</PhysicalBox></PageArea></CommonData><Pages><Page ID="1" BaseLoc="Page.xml"/><Page ID="2" BaseLoc="Page2.xml"/></Pages><CustomTags>Tags.xml</CustomTags></Document>`,
		"Doc_0/Page.xml":     `<Page xmlns="http://www.ofdspec.org/2016"/>`,
		"Doc_0/Page2.xml":    `<Page xmlns="http://www.ofdspec.org/2016"/>`,
		"Doc_0/Tags.xml":     `<CustomTags xmlns="http://www.ofdspec.org/2016"><CustomTag TypeID="cert"><FileLoc>data.xml</FileLoc></CustomTag></CustomTags>`,
		"Doc_0/data.xml":     `<Business xmlns="urn:industry"><certificateName>这不是元数据</certificateName></Business>`,
		"orphan.xml":         `<OFD xmlns="http://www.ofdspec.org/2016" Version="1.0" DocType="OFD"><DocBody><DocInfo><DocID>orphan</DocID></DocInfo><DocRoot>Doc_0/Document.xml</DocRoot></DocBody></OFD>`,
	})
}

func TestCertificateMetadataIsExplicitAndAcceptsMultiplePages(t *testing.T) {
	data := certificateArchive(t, certificateMetadataXML, false)
	for _, mode := range []Mode{ModeStrict, ModeStructural} {
		v, err := New(WithCertificateMetadata(true), WithMode(mode))
		if err != nil {
			t.Fatal(err)
		}
		report := v.ValidateReader(t.Context(), bytes.NewReader(data), "certificate.ofd")
		if report.HasErrors() || report.Summary.Warnings != 0 {
			t.Fatalf("合法基础元数据或多页证照被拒绝: %+v", report.Issues)
		}
		if !hasCertificateCheck(report, "passed") || !hasCertificateIssue(report, "certificate.scope") {
			t.Fatalf("报告未说明本地检查范围: %+v", report)
		}
	}
	data = certificateArchive(t, "", false)
	v, err := New(WithCustomTagTypeID(true))
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(t.Context(), bytes.NewReader(data), "tag-only.ofd")
	for _, issue := range report.Issues {
		if strings.HasPrefix(issue.Code, "certificate.") {
			t.Fatalf("TypeID 开关不应启用元数据检查: %+v", issue)
		}
	}
	for _, check := range report.Checks {
		if check.Name == "certificate_metadata" {
			t.Fatal("未启用时不应增加证照元数据检查项")
		}
	}
}

func TestCertificateMetadataUnknownDateIsWarningNotFalsePass(t *testing.T) {
	metadata := strings.Replace(certificateMetadataXML, "2018年7月25日", "2018-W30-3", 1)
	v, err := New(WithCertificateMetadata(true), WithMode(ModeStructural))
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(t.Context(), bytes.NewReader(certificateArchive(t, metadata, false)), "date.ofd")
	if report.HasErrors() || report.Summary.Warnings != 1 || !hasCertificateCheck(report, "warning") {
		t.Fatalf("未解释的GB/T 7408表达应提示复核，不能伪称通过或无效: %+v", report)
	}
}

func TestCertificateMetadataStructuralChecksLocateBadChecksumAndAliases(t *testing.T) {
	metadata := strings.Replace(certificateMetadataXML, ".001.M", ".001.0", 1) + `<CustomData Name="certificateName">重复名称</CustomData>`
	v, err := New(WithCertificateMetadata(true), WithMode(ModeStructural), WithScanXML(false))
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(t.Context(), bytes.NewReader(certificateArchive(t, metadata, false)), "invalid.ofd")
	for _, code := range []string{"certificate.metadata.identifier", "certificate.metadata.duplicate"} {
		found := false
		for _, issue := range report.Issues {
			if issue.Code == code {
				found = true
				if issue.File != "OFD.xml" || !strings.Contains(issue.Path, "CustomData") || len(issue.Clause) == 0 {
					t.Fatalf("问题缺少定位或条款: %+v", issue)
				}
			}
		}
		if !found {
			t.Fatalf("未发现 %s: %+v", code, report.Issues)
		}
	}
	if !hasCertificateCheck(report, "failed") {
		t.Fatal("元数据检查未报告失败")
	}
}

func TestCertificateMetadataDoesNotBorrowFieldsFromOtherDocumentBodies(t *testing.T) {
	v, err := New(WithCertificateMetadata(true), WithMode(ModeStructural))
	if err != nil {
		t.Fatal(err)
	}
	report := v.ValidateReader(t.Context(), bytes.NewReader(certificateArchive(t, certificateMetadataXML, true)), "multiple.ofd")
	count := 0
	for _, issue := range report.Issues {
		if issue.Code == "certificate.metadata.missing" {
			count++
			if !strings.Contains(issue.Path, "DocBody[2]") {
				t.Fatalf("缺失项应定位第二文档体: %+v", issue)
			}
		}
	}
	if count != 7 {
		t.Fatalf("第二文档体缺失项被其他文档或业务XML补齐: %+v", report.Issues)
	}
}

func hasCertificateCheck(report Report, status string) bool {
	for _, check := range report.Checks {
		if check.Name == "certificate_metadata" && check.Status == status && check.NameZh != "" {
			return true
		}
	}
	return false
}

func hasCertificateIssue(report Report, code string) bool {
	for _, issue := range report.Issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
