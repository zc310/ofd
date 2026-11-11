package spec_test

import (
	"encoding/json"
	"encoding/xml"
	"testing"

	"github.com/zc310/ofd/pkg/spec"
)

func TestIntegrityManifestDecodePreservesRootSpellingAndRawFileList(t *testing.T) {
	for _, name := range []string{"OFDEntries", "DocEntries"} {
		t.Run(name, func(t *testing.T) {
			data := `<` + name + ` xmlns="http://www.ofdspec.org/2016" ID="m1" CreatorName="测试" Version="原文" CreationDate="原始日期"><FileList><File ID="f1" FileLoc="/Doc_0/../Doc_0/Document.xml"/><File ID="f2" FileLoc="/OFD.xml"/></FileList><SignedValueLoc>/Signs/value.dat</SignedValueLoc></` + name + `>`
			var manifest spec.IntegrityManifest
			if err := xml.Unmarshal([]byte(data), &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest.XMLName.Local != name || manifest.XMLName.Space != spec.Namespace || manifest.ID != "m1" || manifest.CreationDate != "原始日期" || manifest.Version != "原文" {
				t.Fatalf("完整性清单的原始声明改变: %+v", manifest)
			}
			if len(manifest.Files) != 2 || manifest.Files[0].ID != "f1" || manifest.Files[0].Location != "/Doc_0/../Doc_0/Document.xml" || manifest.Files[1].Location != "/OFD.xml" || manifest.SignedValueLoc != "/Signs/value.dat" {
				t.Fatal("文件顺序或原始路径不应由声明解码提前修改")
			}
		})
	}
}

func TestIntegrityManifestJSONPreservesPublishedReportFields(t *testing.T) {
	value := spec.IntegrityManifest{
		XMLName: xml.Name{Space: spec.Namespace, Local: "OFDEntries"},
		ID:      "m1", CreatorName: "测试", Version: "1", CreationDate: "2026-01-02",
		Files: []spec.IntegrityFile{{ID: "f1", Location: "/OFD.xml"}}, SignedValueLoc: "/Signs/value.dat",
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"id":"m1","creator_name":"测试","version":"1","creation_date":"2026-01-02","files":[{"id":"f1","location":"/OFD.xml"}],"signed_value_location":"/Signs/value.dat"}`
	if string(data) != want {
		t.Fatalf("已发布完整性报告字段改变: %s", data)
	}
}
