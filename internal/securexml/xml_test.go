package securexml

import (
	"context"
	"encoding/xml"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDecodeAcceptsNamespacePrefixesBOMAndRepeatedReferences(t *testing.T) {
	for _, source := range []string{
		`<Root xmlns="` + Namespace + `" ID="doc-1"><Value>hello</Value><Reference>A</Reference><Reference>B</Reference></Root>`,
		"\xef\xbb\xbf" + `<?xml version="1.0"?><o:Root xmlns:o="` + Namespace + `" ID="doc-1"><o:Value>hello</o:Value><o:Reference>A</o:Reference><o:Reference>B</o:Reference></o:Root>`,
	} {
		var target struct {
			ID         string   `xml:"ID,attr"`
			Value      string   `xml:"Value"`
			References []string `xml:"Reference"`
		}
		if err := Decode(context.Background(), []byte(source), "Root", &target); err != nil {
			t.Fatal(err)
		}
		if target.ID != "doc-1" || target.Value != "hello" || strings.Join(target.References, ",") != "A,B" {
			t.Fatalf("解码结果不完整: %+v", target)
		}
	}
}

func TestDecodeRejectsAmbiguousOrMalformedXMLBeforeUpdatingTarget(t *testing.T) {
	for _, test := range []struct{ name, source string }{
		{"empty", ""},
		{"comments only", "<!-- no root -->"},
		{"wrong root", `<Other xmlns="` + Namespace + `"/>`},
		{"multiple roots", `<Root xmlns="` + Namespace + `"/><Root xmlns="` + Namespace + `"/>`},
		{"missing namespace", `<Root/>`},
		{"foreign child namespace", `<Root xmlns="` + Namespace + `"><Value xmlns="urn:foreign"/></Root>`},
		{"duplicate value", `<Root xmlns="` + Namespace + `"><Value>A</Value><Value>B</Value></Root>`},
		{"duplicate attribute", `<Root xmlns="` + Namespace + `" ID="a" ID="b"/>`},
		{"namespaced attribute", `<Root xmlns="` + Namespace + `" xmlns:x="urn:foreign" x:ID="a"/>`},
		{"DTD", `<!DOCTYPE Root [<!ENTITY x "value">]><Root xmlns="` + Namespace + `"/>`},
		{"text before root", `outside<Root xmlns="` + Namespace + `"/>`},
		{"text after root", `<Root xmlns="` + Namespace + `"/>outside`},
		{"mismatched close", `<Root xmlns="` + Namespace + `"><Value></Root>`},
		{"truncated", `<Root xmlns="` + Namespace + `">`},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := struct{ Value string }{Value: "unchanged"}
			if err := Decode(context.Background(), []byte(test.source), "Root", &target); err == nil {
				t.Fatal("不合法或存在歧义的 XML 应被拒绝")
			}
			if target.Value != "unchanged" {
				t.Fatalf("结构校验失败不应修改目标，实际 %+v", target)
			}
		})
	}
}

func TestDecodeAllowsRepeatedProtocolCollectionsAndIndependentParents(t *testing.T) {
	for _, name := range []string{"DocBody", "Signature", "Reference", "StampAnnot", "Parameter", "File"} {
		t.Run(name, func(t *testing.T) {
			source := `<Root xmlns="` + Namespace + `"><` + name + `/><` + name + `/></Root>`
			var target any
			if err := Decode(context.Background(), []byte(source), "Root", &target); err != nil {
				t.Fatalf("协议集合允许重复元素: %v", err)
			}
		})
	}
	var target any
	source := `<Root xmlns="` + Namespace + `"><Reference><Value>A</Value></Reference><Reference><Value>B</Value></Reference></Root>`
	if err := Decode(context.Background(), []byte(source), "Root", &target); err != nil {
		t.Fatalf("不同父元素下的单值字段不应被误判为重复: %v", err)
	}
}

func TestDecodeWithLimitsEnforcesBudgets(t *testing.T) {
	source := []byte(`<Root xmlns="` + Namespace + `"><Value>hello</Value></Root>`)
	for _, test := range []struct {
		name         string
		nodes, depth int
		wantLimit    bool
	}{
		{"exact limits", 2, 2, false},
		{"node budget", 1, 2, true},
		{"depth budget", 2, 1, true},
		{"default limits", 0, 0, false},
		{"oversized limits use defaults", 100001, 65, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var target any
			err := DecodeWithLimits(context.Background(), source, "Root", &target, test.nodes, test.depth)
			if test.wantLimit {
				if !errors.Is(err, ErrLimitExceeded) {
					t.Fatalf("应保留预算错误，实际 %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, test := range []struct{ name, source string }{
		{"byte budget", strings.Repeat(" ", (4<<20)+1)},
		{"hard depth cap", `<Root xmlns="` + Namespace + `">` + strings.Repeat("<Value>", 64) + strings.Repeat("</Value>", 64) + `</Root>`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var target any
			if err := DecodeWithLimits(context.Background(), []byte(test.source), "Root", &target, 100001, 1000); !errors.Is(err, ErrLimitExceeded) {
				t.Fatalf("调用方不能扩大硬上限，实际 %v", err)
			}
		})
	}
}

func TestDecodePreservesCancellationAndDestinationErrors(t *testing.T) {
	source := []byte(`<Root xmlns="` + Namespace + `"><Value>not-an-integer</Value></Root>`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deadline, cancelDeadline := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancelDeadline()
	for _, test := range []struct {
		ctx  context.Context
		want error
	}{{ctx, context.Canceled}, {deadline, context.DeadlineExceeded}} {
		var target any
		if err := Decode(test.ctx, source, "Root", &target); !errors.Is(err, test.want) {
			t.Fatalf("取消原因应可识别，实际 %v，期望 %v", err, test.want)
		}
	}
	var target struct{ Value int }
	if err := Decode(context.Background(), source, "Root", &target); err == nil {
		t.Fatal("应传播目标字段的类型转换错误")
	}
	if err := Decode(context.Background(), source, "Root", target); err == nil {
		t.Fatal("应拒绝非指针解码目标")
	}
	var syntaxError *xml.SyntaxError
	if err := Decode(context.Background(), []byte("<"), "Root", &target); !errors.As(err, &syntaxError) {
		t.Fatalf("应保留 XML 语法错误类型，实际 %v", err)
	}
}

func TestResolvePackagePaths(t *testing.T) {
	for _, test := range []struct{ from, value, want string }{
		{"Doc_0/Signs/Signature.xml", "../Res/Seal.esl", "Doc_0/Res/Seal.esl"},
		{"Doc_0/Signs/Signature.xml", "./SignedValue.dat", "Doc_0/Signs/SignedValue.dat"},
		{"Doc_0/Signs/Signature.xml", "/Doc_1/Signs/Value.dat", "Doc_1/Signs/Value.dat"},
		{"OFD.xml", "Doc_0//./Document.xml", "Doc_0/Document.xml"},
		{"Doc_0/Document.xml", "../OFD.xml", "OFD.xml"},
		{"OFD.xml", "资源/印章.esl", "资源/印章.esl"},
	} {
		t.Run(test.value, func(t *testing.T) {
			got, err := Resolve(test.from, test.value)
			if err != nil || got != test.want {
				t.Fatalf("Resolve(%q, %q) = %q, %v，期望 %q", test.from, test.value, got, err, test.want)
			}
		})
	}
}

func TestResolveRejectsEscapesAndExternalPaths(t *testing.T) {
	for _, value := range []string{"", " x", "x ", "//server/file", `C:\file`, "https://example.com/a", "a\x00b", "../../outside", "/../outside", "/", ".", "a/.."} {
		t.Run(value, func(t *testing.T) {
			if got, err := Resolve("OFD.xml", value); err == nil || got != "" {
				t.Fatalf("非法包内路径应失败并返回空串，实际 %q, %v", got, err)
			}
		})
	}
}

func TestValidID(t *testing.T) {
	for _, value := range []string{"sign-1", "_id", "Signature.2", "签名一", "éclair", "a١"} {
		if !ValidID(value) {
			t.Errorf("应接受合法标识 %q", value)
		}
	}
	for _, value := range []string{"", "1sign", "-sign", ".sign", "a:b", "a/b", "a b", "a\x00b", "a\xff"} {
		if ValidID(value) {
			t.Errorf("应拒绝非法标识 %q", value)
		}
	}
}
