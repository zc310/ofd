package merge

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beevik/etree"
	"github.com/klauspost/compress/zip"
	"github.com/zc310/ofd/internal/container"
	"github.com/zc310/ofd/pkg/creator"
)

// 使用与 pkg/sign 相同的布局：签名清单与 Document.xml 共用目录。
func writeSignatureLayout(t *testing.T, list, signature, reference string) string {
	t.Helper()
	input := filepath.Join(t.TempDir(), "signed.ofd")
	root := strings.Replace(testOFDXML("Doc_0/Document.xml"), "</DocBody>", "<Signatures>"+list+"</Signatures></DocBody>", 1)
	writeTestOFD(t, input, []ofdEntry{
		{"OFD.xml", root},
		{"Doc_0/Document.xml", testDocumentXML},
		{"Doc_0/Pages/content.xml", "page bytes"},
		{list, `<Signatures><Signature ID="s1" BaseLoc="` + signature + `"/></Signatures>`},
		{"Doc_0/Signatures/custom.xml", `<Signature><SignedInfo><References><Reference FileRef="` + reference + `"/></References><Seal><BaseLoc>seal.esl</BaseLoc></Seal></SignedInfo><SignedValue>value.dat</SignedValue></Signature>`},
		{"Doc_0/Signatures/value.dat", "signed value"},
		{"Doc_0/Signatures/seal.esl", "seal"},
		{"Doc_0/Signatures/ordinary.txt", "ordinary file sharing the directory"},
	})
	return input
}

func TestSignatureDropKeepsFilesSharingSignatureDirectories(t *testing.T) {
	input := writeSignatureLayout(t, "Doc_0/Signatures.xml", "Signatures/custom.xml", "/Doc_0/Document.xml")
	var out bytes.Buffer
	if err := Files([]string{input}, &out, Options{Signatures: creator.SignatureDrop}); err != nil {
		t.Fatal(err)
	}
	pkg := openMerged(t, out.Bytes())
	for _, name := range []string{"Doc_0/Document.xml", "Doc_0/Pages/content.xml", "Doc_0/Signatures/ordinary.txt"} {
		if !pkg.Has(name) {
			t.Errorf("drop 删除了非签名文件 %s", name)
		}
	}
	for _, name := range []string{"Doc_0/Signatures.xml", "Doc_0/Signatures/custom.xml", "Doc_0/Signatures/value.dat", "Doc_0/Signatures/seal.esl"} {
		if pkg.Has(name) {
			t.Errorf("drop 保留了签名文件 %s", name)
		}
	}
}

func TestSignaturePreserveRejectsProtectedRootChanges(t *testing.T) {
	for _, ref := range []string{"/OFD.xml", "../../OFD.xml"} {
		t.Run(ref, func(t *testing.T) {
			input := writeSignatureLayout(t, "Doc_0/Signatures.xml", "Signatures/custom.xml", ref)
			var out bytes.Buffer
			err := Files([]string{input, input}, &out, Options{})
			if err == nil || !strings.Contains(err.Error(), "保护了 OFD.xml") {
				t.Fatalf("preserve 应拒绝修改受保护的主入口，实际 %v", err)
			}
		})
	}
}

func TestSignaturePreserveAllowsUnchangedProtectedRoot(t *testing.T) {
	input := writeSignatureLayout(t, "Doc_0/Signatures.xml", "Signatures/custom.xml", "/OFD.xml")
	pkg, err := container.OpenFile(input)
	if err != nil {
		t.Fatal(err)
	}
	defer pkg.Close()
	root := etree.NewDocument()
	if err := root.ReadFromString(readEntry(t, pkg, "OFD.xml")); err != nil {
		t.Fatal(err)
	}
	canonical, err := buildRootXML([]*etree.Element{cloneElement(directChild(root.Root(), "DocBody"))}, readRootAttrs(root.Root()))
	if err != nil {
		t.Fatal(err)
	}
	var source bytes.Buffer
	zw := zip.NewWriter(&source)
	for _, entry := range pkg.Entries() {
		data, err := pkg.Read(entry.Path)
		if err != nil {
			t.Fatal(err)
		}
		if entry.Path == "OFD.xml" {
			data = canonical
		}
		w, err := zw.Create(entry.Path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Bytes([][]byte{source.Bytes()}, &out, Options{}); err != nil {
		t.Fatalf("主入口字节不变时应允许 preserve: %v", err)
	}
}

func TestSignaturePreserveAllowsUnchangedRelativeReferences(t *testing.T) {
	input := writeSignatureLayout(t, "Doc_0/Signatures.xml", "Signatures/custom.xml", "../Document.xml")
	var out bytes.Buffer
	if err := Files([]string{input, input}, &out, Options{}); err != nil {
		t.Fatal(err)
	}
	pkg := openMerged(t, out.Bytes())
	if got := readEntry(t, pkg, "Doc_1/Document.xml"); got != testDocumentXML {
		t.Fatalf("受保护的正文发生变化: %s", got)
	}
}

func TestSignatureCustomNamesFollowManifest(t *testing.T) {
	for _, mode := range []creator.SignatureMode{creator.SignaturePreserve, creator.SignatureRewrite, creator.SignatureDrop} {
		t.Run(string(mode), func(t *testing.T) {
			ref := "../Document.xml"
			if mode == creator.SignatureRewrite {
				ref = "/Doc_0/Document.xml"
			}
			input := writeSignatureLayout(t, "Doc_0/custom-list.xml", "Signatures/custom.xml", ref)
			var out bytes.Buffer
			var events []SignatureEvent
			if err := Files([]string{input, input}, &out, Options{Signatures: mode, OnSignature: func(e SignatureEvent) { events = append(events, e) }}); err != nil {
				t.Fatal(err)
			}
			if len(events) != 2 || events[0].ID != "Doc_0/Signatures/custom.xml" || events[1].ID != events[0].ID {
				t.Fatalf("每个来源的自定义签名均应上报一次: %+v", events)
			}
			pkg := openMerged(t, out.Bytes())
			switch mode {
			case creator.SignaturePreserve:
				if events[1].Action != SignaturePreserved {
					t.Fatalf("preserve 事件错误: %+v", events)
				}
			case creator.SignatureRewrite:
				xml := readEntry(t, pkg, "Doc_1/Signatures/custom.xml")
				if !strings.Contains(xml, `FileRef="/Doc_1/Document.xml"`) || events[1].Action != SignatureRewritten {
					t.Fatalf("自定义签名没有重写: %s, %+v", xml, events)
				}
			case creator.SignatureDrop:
				if pkg.Has("Doc_1/custom-list.xml") || pkg.Has("Doc_1/Signatures/custom.xml") || events[1].Action != SignatureDropped {
					t.Fatalf("自定义签名没有丢弃: %+v", events)
				}
			}
		})
	}
}
