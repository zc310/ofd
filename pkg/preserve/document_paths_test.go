package preserve

import (
	"bytes"
	"strings"
	"testing"
)

func TestPreserveRejectsMultipleDocumentBodiesBeforeWriting(t *testing.T) {
	files := pageAreaFiles("")
	files["OFD.xml"] = strings.Replace(files["OFD.xml"], "</OFD>", `<DocBody><DocRoot>Doc_1/Document.xml</DocRoot></DocBody></OFD>`, 1)
	files["Doc_1/Document.xml"] = files["Doc_0/Document.xml"]
	files["Doc_1/Pages/Page_0/Content.xml"] = files["Doc_0/Pages/Page_0/Content.xml"]
	source := buildOFD(t, files)
	if _, err := Plan(source, Options{DocType: "OFD-A"}); err == nil || !strings.Contains(err.Error(), "6.2.1 c)") {
		t.Fatalf("Plan 应明确拒绝多文档转换: %v", err)
	}
	var out bytes.Buffer
	if _, err := Apply(source, &out, Options{DocType: "OFD-A"}); err == nil || !strings.Contains(err.Error(), "多文档") {
		t.Fatalf("Apply 应明确拒绝多文档转换: %v", err)
	}
	if out.Len() != 0 {
		t.Fatal("拒绝多文档后不应写出部分转换的文件")
	}
}

func TestPreserveNormalizesAbsoluteAndRelativePagePaths(t *testing.T) {
	for _, location := range []string{"/Doc_0/Pages/Page_0/Content.xml", "./Pages/Page_0/Content.xml", "Pages/../Pages/Page_0/Content.xml"} {
		t.Run(location, func(t *testing.T) {
			files := pageAreaFiles(areaA4)
			files["Doc_0/Document.xml"] = strings.ReplaceAll(files["Doc_0/Document.xml"], "Pages/Page_0/Content.xml", location)
			result, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A"})
			if countOFDElements(t, after["Doc_0/Pages/Page_0/Content.xml"], "Area") != 0 {
				t.Fatal("目标页面没有转换")
			}
			for _, name := range result.Entries {
				if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
					t.Fatalf("路径清单不是规范 ZIP 条目名: %v", result.Entries)
				}
			}
		})
	}
}

func TestPreserveRejectsPagePathEscapingPackage(t *testing.T) {
	files := pageAreaFiles(areaA4)
	files["Doc_0/Document.xml"] = strings.ReplaceAll(files["Doc_0/Document.xml"], "Pages/Page_0/Content.xml", "../../outside.xml")
	if _, err := Plan(buildOFD(t, files), Options{DocType: "OFD-A"}); err == nil {
		t.Fatal("越过包根目录的页面引用应报错")
	}
}
