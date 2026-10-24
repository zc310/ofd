package crypt

import (
	"bytes"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func pageScopeStampFiles() map[string][]byte {
	files := pageScopeFiles()
	files["OFD.xml"] = bytes.ReplaceAll(files["OFD.xml"], []byte(`</DocBody>`), []byte(`<Signatures>Signatures.xml</Signatures></DocBody>`))
	files["Signatures.xml"] = []byte(`<Signatures xmlns="` + Namespace + `"><Signature ID="sig1" BaseLoc="Signature.xml"/></Signatures>`)
	files["Signature.xml"] = []byte(`<Signature xmlns="` + Namespace + `"><SignedInfo><StampAnnot ID="20" PageRef="1" Boundary="0 0 10 10"/><Seal><BaseLoc>Seal.esl</BaseLoc><ImageLoc>Seal.png</ImageLoc></Seal></SignedInfo><SignedValue>SignedValue.dat</SignedValue></Signature>`)
	files["SignedValue.dat"] = []byte("seal appearance bytes")
	files["Seal.esl"] = []byte("external seal bytes")
	files["Seal.png"] = []byte("external seal image bytes")
	return files
}

func TestPageSelectionMatchesNumericIDsAndPreservesOriginalXML(t *testing.T) {
	// GB/T 33190 §7.3 表 2 和附录 A：ST_ID/ST_RefID 比较无符号整数值。
	for _, value := range []string{"01", "+1", " 0001 "} {
		t.Run(value, func(t *testing.T) {
			files := pageScopeStampFiles()
			files["Doc_0/Document.xml"] = bytes.ReplaceAll(files["Doc_0/Document.xml"], []byte(`Page ID="1"`), []byte(`Page ID="`+value+`"`))
			files["Doc_0/Document.xml"] = bytes.ReplaceAll(files["Doc_0/Document.xml"], []byte(`TemplatePage ID="10"`), []byte(`TemplatePage ID="+0010"`))
			files["Doc_0/Page1.xml"] = bytes.ReplaceAll(files["Doc_0/Page1.xml"], []byte(`TemplateID="10"`), []byte(`TemplateID="010"`))
			files["Doc_0/Annotations.xml"] = bytes.ReplaceAll(files["Doc_0/Annotations.xml"], []byte(`PageID="1"`), []byte(`PageID="`+value+`"`))
			files["Signature.xml"] = bytes.ReplaceAll(files["Signature.xml"], []byte(`PageRef="1"`), []byte(`PageRef="`+value+`"`))
			options := testPasswordOptions()
			options.Pages = &PageSelection{Document: "Doc_0/Document.xml", IDs: []string{"1"}}
			encrypted, report := encryptFiles(t, files, options)
			for _, name := range []string{"Doc_0/Page1.xml", "Doc_0/Template.xml", "Doc_0/Annot1.xml", "Signature.xml", "SignedValue.dat", "Seal.esl", "Seal.png"} {
				if !slices.Contains(report.SelectedEntries, name) {
					t.Errorf("数值相等的 ID 未选中关联文件: %s", name)
				}
				if _, exists := encrypted[name]; exists {
					t.Errorf("目标页内容或外观仍以原路径保留明文: %s", name)
				}
			}
			if !bytes.Equal(encrypted["Doc_0/Annot2.xml"], files["Doc_0/Annot2.xml"]) {
				t.Fatal("未选中页面的注释应保留原始字节")
			}
			output, _ := decryptedFiles(t, testPackage(t, encrypted), DecryptOptions{Password: []byte("secret")})
			if !reflect.DeepEqual(output, files) {
				t.Fatal("ID 归一化只能用于匹配，不应改写恢复后的 XML 或其他原始字节")
			}
		})
	}
}

func TestPageSelectionRejectsNumericDuplicateAndInvalidIDsBeforeOutput(t *testing.T) {
	for _, test := range []struct{ name, path, old, replacement string }{
		{"duplicate page", "Doc_0/Document.xml", `Page ID="2"`, `Page ID="01"`},
		{"duplicate template", "Doc_0/Document.xml", `</CommonData>`, `<TemplatePage ID="010" BaseLoc="Template.xml"/></CommonData>`},
		{"invalid page", "Doc_0/Document.xml", `Page ID="1"`, `Page ID="not-an-ID"`},
		{"zero template", "Doc_0/Document.xml", `TemplatePage ID="10"`, `TemplatePage ID="0"`},
		{"invalid template reference", "Doc_0/Page1.xml", `TemplateID="10"`, `TemplateID="-10"`},
		{"overflow annotation reference", "Doc_0/Annotations.xml", `PageID="1"`, `PageID="4294967296"`},
		{"invalid annotation reference", "Doc_0/Annotations.xml", `PageID="1"`, `PageID="1e0"`},
		{"missing annotation reference", "Doc_0/Annotations.xml", `PageID="1"`, ``},
		{"invalid stamp reference", "Signature.xml", `PageRef="1"`, `PageRef="1.0"`},
		{"missing stamp reference", "Signature.xml", `PageRef="1"`, ``},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := pageScopeStampFiles()
			if !strings.Contains(string(files[test.path]), test.old) {
				t.Fatal("测试未命中待替换内容")
			}
			files[test.path] = bytes.ReplaceAll(files[test.path], []byte(test.old), []byte(test.replacement))
			options := testPasswordOptions()
			options.Pages = &PageSelection{Document: "Doc_0/Document.xml", IDs: []string{"1"}}
			called := false
			report, err := EncryptPackage(t.Context(), testPackage(t, files), options, func(string, []byte) error { called = true; return nil })
			if !errors.Is(err, ErrInvalidMetadata) || report != nil || called {
				t.Fatalf("无法确定保护范围时应在输出前拒绝: report=%+v err=%v emitted=%t", report, err, called)
			}
		})
	}
}
