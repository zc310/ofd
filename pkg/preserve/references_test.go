package preserve

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/pkg/validator"
)

func extensionFiles() map[string]string {
	files := pageAreaFiles("")
	files["Doc_0/Document.xml"] = strings.Replace(files["Doc_0/Document.xml"], "</Document>", "<Extensions>Extensions.xml</Extensions></Document>", 1)
	files["Doc_0/Extensions.xml"] = `<Extensions` + ns + `><Extension AppName="test" RefId="1"><ExtendData>extension.bin</ExtendData></Extension></Extensions>`
	files["Doc_0/extension.bin"] = "payload"
	return files
}

func TestUnreferencedUsesConvertedDocumentReferences(t *testing.T) {
	source := buildOFD(t, extensionFiles())
	options := Options{DocType: "OFD-A", DropUnreferenced: true}
	plan, err := Plan(source, options)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Doc_0/Extensions.xml", "Doc_0/extension.bin"}
	if !reflect.DeepEqual(plan.Unreferenced, want) || plan.ClosureIncomplete || plan.UnreferencedDropped != 0 {
		t.Fatalf("计划未反映删除 Extensions 引用后的闭包: %+v", plan)
	}
	var out bytes.Buffer
	result, err := Apply(source, &out, options)
	if err != nil {
		t.Fatal(err)
	}
	if result.UnreferencedDropped != 2 || !reflect.DeepEqual(result.Unreferenced, want) {
		t.Fatalf("实际删除与计划不符: %+v", result)
	}
	pkg, err := core.OpenBytes(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer pkg.Close()
	for _, name := range want {
		if pkg.Has(name) {
			t.Errorf("输出仍保留转换后无人引用的 %s", name)
		}
	}
}

func TestConvertedReferencesDoNotDeleteStillReferencedPayload(t *testing.T) {
	files := extensionFiles()
	files["OFD.xml"] = strings.Replace(files["OFD.xml"], "<DocInfo><DocID>preserve-tests</DocID></DocInfo>", "<DocInfo><DocID>shared</DocID><Cover>Doc_0/extension.bin</Cover></DocInfo>", 1)
	result, _ := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A", DropUnreferenced: true})
	if !reflect.DeepEqual(result.Unreferenced, []string{"Doc_0/Extensions.xml"}) {
		t.Fatalf("仍被主入口引用的文件不能删除: %+v", result)
	}
}

func TestConvertedReferenceClosureStillGuardsMissingReferences(t *testing.T) {
	files := extensionFiles()
	files["Doc_0/Document.xml"] = strings.Replace(files["Doc_0/Document.xml"], "</Document>", "<Attachments>missing.xml</Attachments></Document>", 1)
	result, _ := appliedChangesBytes(t, buildOFD(t, files), Options{DocType: "OFD-A", DropUnreferenced: true})
	if !result.ClosureIncomplete || result.ClosureReason == "" || result.UnreferencedDropped != 0 {
		t.Fatalf("转换后闭包不完整时应保留全部候选: %+v", result)
	}
}

func TestReferenceOverridesDoNotModifyOriginalInput(t *testing.T) {
	files := extensionFiles()
	source := buildOFD(t, files)
	overrides := map[string][]byte{"Doc_0/Document.xml": []byte(strings.Replace(files["Doc_0/Document.xml"], "<Extensions>Extensions.xml</Extensions>", "", 1))}
	index, err := validator.PackageReferencesWithOverrides(context.Background(), source, overrides)
	if err != nil {
		t.Fatal(err)
	}
	if !index.Complete() || index.Reachable["Doc_0/Extensions.xml"] {
		t.Fatalf("替换未用于引用遍历: %+v", index)
	}
	original, err := validator.PackageReferences(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if !original.Reachable["Doc_0/Extensions.xml"] || !original.Reachable["Doc_0/extension.bin"] {
		t.Fatal("引用替换不应改变原包")
	}
}
