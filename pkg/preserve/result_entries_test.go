package preserve

import (
	"bytes"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestResultEntriesExactlyMatchChangedFiles(t *testing.T) {
	files := pageAreaFiles(areaA4, areaA5)
	files["Doc_0/Pages/Page_0/Content.xml"] = strings.Replace(files["Doc_0/Pages/Page_0/Content.xml"], "</Page>", "<Actions>"+actionXML(false)+"</Actions></Page>", 1)
	source := buildOFD(t, files)
	result, after := appliedChangesBytes(t, source, Options{DocType: "OFD-A"})
	var changed []string
	for name, before := range files {
		if before != after[name] {
			changed = append(changed, name)
		}
	}
	slices.Sort(changed)
	if !reflect.DeepEqual(result.Entries, changed) {
		t.Fatalf("路径清单 %v，实际改动 %v", result.Entries, changed)
	}
	plan, err := Plan(source, Options{DocType: "OFD-A"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Entries, result.Entries) || plan.Entry != result.Entry {
		t.Fatalf("Plan 与 Apply 路径清单不同: %+v, %+v", plan, result)
	}
}

func TestMatchingDefaultDoesNotRewriteDocumentOrReportChange(t *testing.T) {
	files := pageAreaFiles(`<Area><PhysicalBox>0.0 0 100.0 100.00</PhysicalBox></Area>`)
	files["Doc_0/Document.xml"] = strings.ReplaceAll(files["Doc_0/Document.xml"], "<Permissions/>", "")
	result, after := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A"})
	if after["Doc_0/Document.xml"] != files["Doc_0/Document.xml"] {
		t.Fatal("相同默认设置不应重新写入 Document.xml")
	}
	if !reflect.DeepEqual(result.Entries, []string{"Doc_0/Pages/Page_0/Content.xml"}) || result.Entry != result.Entries[0] {
		t.Fatalf("只改页面时路径清单错误: %+v", result)
	}
	for _, change := range result.Changes {
		if change.Clause == "GB/T 42133 6.2.3 a)" {
			t.Fatalf("未修改默认设置却报告改动: %+v", change)
		}
	}
}

func TestPageAreaNormalizationIsIdempotent(t *testing.T) {
	files := pageAreaFiles("", areaA4, areaA4)
	var out bytes.Buffer
	if _, err := Apply(buildOFD(t, files), &out, Options{DocType: "OFD-A"}); err != nil {
		t.Fatal(err)
	}
	result, err := Plan(out.Bytes(), Options{DocType: "OFD-A"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 0 || len(result.Entries) != 0 || result.Entry != "" {
		t.Fatalf("重复转换又改变默认值或虚报路径: %+v", result)
	}
}

func TestResultEntriesIncludesDeletedFiles(t *testing.T) {
	result, _ := appliedChangesBytes(t, buildWithOrphan(t), Options{DocType: "OFD-A", DropUnreferenced: true})
	if !slices.Contains(result.Entries, "Doc_0/Res/orphan.bin") {
		t.Fatalf("删除的条目未计入路径清单: %v", result.Entries)
	}
}
