package creator

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zip"
	"github.com/knroy/go-xml/v2/xdm"
	"github.com/zc310/ofd/pkg/spec"
)

func TestCreateGeneratedXMLUsesOFDPrefix(t *testing.T) {
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	path := Path{Width: 10, Height: 10, Data: "M 0 0 L 10 10 C"}
	document := Document{
		ID: "namespace-test", Title: "命名空间测试",
		CreationDate: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		Keywords:     []string{"OFD"}, CustomDatas: []CustomData{{Name: "key", Value: "value"}},
		Templates: []TemplatePage{{ID: 10, Items: []Item{path}}},
		Pages: []Page{{
			Templates: []TemplateRef{{ID: 10}},
			Resources: []PageResource{{Images: []PageImage{{ID: 70, Data: picture.Bytes()}}}},
			Items: []Item{
				Text{Width: 20, Height: 10, Value: "正文"},
				Image{Width: 10, Height: 10, ResourceID: 70},
			},
		}},
		Annotations: []AnnotationPage{{Items: []Annotation{{
			ID: 40, Type: "Link", Creator: "creator-test", Remark: "注解",
			Parameters: []AnnotationParameter{{Name: "key", Value: "value"}},
			Boundary:   &Box{Width: 10, Height: 10}, Items: []Item{path},
		}}}},
		Attachments: []Attachment{{ID: "att-1", Name: "readme", Format: "txt", Data: []byte("readme")}},
		CustomTags:  []CustomTag{{NameSpace: "urn:example:tags", DataName: "tags.xml", Data: []byte(`<Tags xmlns="urn:example:tags"/>`)}},
		Extensions:  []Extension{{AppName: "creator-test", RefID: 70, Data: "text", Properties: []ExtensionProperty{{Name: "key", Value: "value"}}}},
		Signatures: []Signature{{
			ID: "sig-1", Type: spec.SigTypeSign, ProviderName: "creator-test",
			References:  []SignatureReference{{FileRef: "../Document.xml"}, {FileRef: "../Pages/Page_0/Content.xml"}},
			SignedValue: []byte("value"),
		}},
		Versions: []DocumentVersion{{ID: "version-1", Index: 1, Files: []VersionFile{{ID: "file-1", Path: "Document.xml"}}}},
	}
	data, err := MarshalWithOptions(document, CreateOptions{Compression: CompressionAuto, Deterministic: true})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	roots := make(map[string]int)
	for _, file := range archive.File {
		if !strings.HasSuffix(file.Name, ".xml") || file.Name == "Doc_0/CustomTags/Data/tags.xml" {
			continue
		}
		t.Run(file.Name, func(t *testing.T) {
			xmlData := readArchiveEntry(t, data, file.Name)
			root, err := parseXMLRoot(xmlData)
			if err != nil {
				t.Fatal(err)
			}
			if root == nil {
				t.Fatal("XML has no root element")
			}
			roots[root.Name().Local]++
			assertOFDPrefix(t, root)
		})
	}
	for root, count := range map[string]int{
		"OFD": 1, "Document": 1, "Res": 2, "Page": 2,
		"Annotations": 1, "PageAnnot": 1, "Attachments": 1, "CustomTags": 1,
		"Extensions": 1, "Signatures": 1, "Signature": 1, "DocVersion": 1,
	} {
		if roots[root] != count {
			t.Errorf("generated %s roots = %d, want %d", root, roots[root], count)
		}
	}
	checkGeneratedPackage(t, data)
	second, err := MarshalWithOptions(document, CreateOptions{Compression: CompressionAuto, Deterministic: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, second) {
		t.Fatal("namespace prefixes made deterministic output unstable")
	}
	t.Run("collected entries survive writer reuse", func(t *testing.T) {
		collected, err := build(document)
		if err != nil {
			t.Fatal(err)
		}
		if len(collected.entries) != len(archive.File) {
			t.Fatalf("collected %d entries, want %d", len(collected.entries), len(archive.File))
		}
		for index, entry := range collected.entries {
			if entry.name != archive.File[index].Name {
				t.Errorf("entry %d = %q, want %q", index, entry.name, archive.File[index].Name)
			}
			if want := readArchiveEntry(t, data, entry.name); !bytes.Equal(entry.data, want) {
				t.Errorf("collected entry %s changed after writer reuse", entry.name)
			}
		}
	})
}

func assertOFDPrefix(t *testing.T, element *xdm.Node) {
	t.Helper()
	name := element.Name()
	if name.Prefix != "ofd" || name.URI != ofNamespace {
		t.Errorf("element %s has namespace %q, want ofd prefix and %q", name.Lexical(), name.URI, ofNamespace)
	}
	for attr := range element.Attrs() {
		if attr.Name().Prefix != "" {
			t.Errorf("attribute %s unexpectedly has a prefix", attr.Name().Lexical())
		}
	}
	for _, child := range element.ChildElements() {
		assertOFDPrefix(t, child)
	}
}

func TestCreateRawExtensionXMLPreservesNamespaces(t *testing.T) {
	for _, test := range []struct {
		name, fragment, prefix, namespace string
	}{
		{"inherited default", `before<Config><Enabled>true</Enabled></Config>after`, "", ofNamespace},
		{"inherited ofd prefix", `<ofd:Config><ofd:Enabled>true</ofd:Enabled></ofd:Config>`, "ofd", ofNamespace},
		{"custom default", `<Config xmlns="urn:example:config"><Enabled>true</Enabled></Config>`, "", "urn:example:config"},
		{"empty default", `<Config xmlns=""><Enabled>true</Enabled></Config>`, "", ""},
		{"custom prefix", `<cfg:Config xmlns:cfg="urn:example:config"><cfg:Enabled>true</cfg:Enabled></cfg:Config>`, "cfg", "urn:example:config"},
		{"shadowed ofd prefix", `<ofd:Config xmlns:ofd="urn:example:config"><ofd:Enabled>true</ofd:Enabled></ofd:Config>`, "ofd", "urn:example:config"},
		{"mixed tokens", `before<Config>&lt;raw&gt;<!--note--><?app value?><Enabled>true</Enabled></Config>after`, "", ofNamespace},
		{"xml stylesheet instruction", `<?xml-stylesheet type="text/xsl" href="config.xsl"?><Config><Enabled>true</Enabled></Config>`, "", ofNamespace},
		{"cdata and entity spelling", `before<Config><![CDATA[甲<&乙]]>&#x41;<Enabled>true</Enabled></Config>after`, "", ofNamespace},
		{"fragment siblings", `<Config><Enabled>true</Enabled></Config>between<Other/>`, "", ofNamespace},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := Marshal(Document{
				ID: "raw-namespace-test", Pages: []Page{{}},
				Extensions: []Extension{{AppName: "creator-test", RefID: 1, DataXML: []byte(test.fragment)}},
			})
			if err != nil {
				t.Fatal(err)
			}
			xmlData := readArchiveEntry(t, data, "Doc_0/Extensions/Extensions.xml")
			if !bytes.Contains(xmlData, []byte(test.fragment)) {
				t.Fatalf("raw XML fragment changed: %s", xmlData)
			}
			root, err := parseXMLRoot(xmlData)
			if err != nil {
				t.Fatal(err)
			}
			extension := root.ChildElements()[0]
			container := extension.ChildElements()[0]
			for _, element := range []*xdm.Node{root, extension, container} {
				name := element.Name()
				if name.Prefix != "ofd" || name.URI != ofNamespace {
					t.Fatalf("generated %s has incorrect namespace", name.Local)
				}
			}
			config := container.ChildElements()[0]
			for _, element := range []*xdm.Node{config, config.ChildElements()[0]} {
				name := element.Name()
				if name.Prefix != test.prefix || name.URI != test.namespace {
					t.Errorf("raw %s has prefix %q and namespace %q, want %q and %q", name.Local, name.Prefix, name.URI, test.prefix, test.namespace)
				}
			}
			checkGeneratedPackage(t, data)
		})
	}
}

func TestCreateRejectsInvalidRawExtensionXML(t *testing.T) {
	for _, fragment := range []string{
		`<Config>`,
		`<Config></Other>`,
		`<?xml version="1.0"?><Config/>`,
		`<Config/><?xml version="1.0"?>`,
		`<!DOCTYPE Config><Config/>`,
		`<Config>&unknown;</Config>`,
		`<cfg:Config/>`,
		`<Config a="1" a="2"/>`,
		`</RawXML><Config/><RawXML>`,
		"<Config>\x00</Config>",
	} {
		t.Run(fragment, func(t *testing.T) {
			_, err := Marshal(Document{
				ID: "invalid-fragment", Pages: []Page{{}},
				Extensions: []Extension{{AppName: "creator-test", RefID: 1, DataXML: []byte(fragment)}},
			})
			if err == nil {
				t.Fatalf("无效 XML 片段被接受: %q", fragment)
			}
		})
	}
}

func TestCreateRawResourceXMLPreservesPrefixes(t *testing.T) {
	publicXML := []byte(`<Res xmlns="http://www.ofdspec.org/2016" BaseLoc="."/>`)
	pageXML := []byte(`<r:Res xmlns:r="http://www.ofdspec.org/2016" BaseLoc="."/>`)
	data, err := Marshal(Document{
		ID:        "raw-resource-namespace-test",
		PublicRes: []PublicResource{{Name: "PublicRes.xml", Data: publicXML}},
		Pages:     []Page{{Resources: []PageResource{{Data: pageXML}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]byte{
		"Doc_0/PublicRes.xml":                publicXML,
		"Doc_0/Pages/Page_0/PageRes_0_0.xml": pageXML,
	} {
		if got := readArchiveEntry(t, data, name); !bytes.Equal(got, want) {
			t.Errorf("raw resource %s changed: %s", name, got)
		}
	}
	checkGeneratedPackage(t, data)
}
