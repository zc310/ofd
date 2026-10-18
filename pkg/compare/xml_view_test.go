package compare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func xmlViewForTest(t *testing.T, a, b string) *XMLView {
	t.Helper()
	change := FileChange{Status: "modified"}
	var af, bf map[string]string
	for i, data := range []string{a, b} {
		if data == "" {
			continue
		}
		fields, err := xmlFields(t.Context(), []byte(data))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		file := &File{Path: "unused.xml", Kind: "xml", SHA256: digest([]byte(data)), XMLSHA256: digest(encoded)}
		if i == 0 {
			change.Before = file
			af = fields
		} else {
			change.After = file
			bf = fields
		}
	}
	view, err := buildXMLView(t.Context(), change, af, bf, []byte(a), []byte(b))
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func reconstructXMLView(view *XMLView) (string, string) {
	var a, b []string
	for _, block := range view.Blocks {
		for _, line := range block.Lines {
			if line.BeforeNumber > 0 {
				a = append(a, line.Before)
			}
			if line.AfterNumber > 0 {
				b = append(b, line.After)
			}
		}
	}
	return strings.Join(a, "\n"), strings.Join(b, "\n")
}

func TestXMLViewNormalizesPrefixesAttributesCommentsAndIndentation(t *testing.T) {
	a := `<a:Root xmlns:a="urn:test" z="2" x="1"><a:Child>value</a:Child><Empty/></a:Root>`
	b := "<b:Root x=\"1\" z=\"2\" xmlns:b=\"urn:test\">\n<!-- ignored --> <b:Child>value</b:Child> <Empty>  </Empty>\n</b:Root>"
	v := xmlViewForTest(t, a, b)
	if v.Kind != "format" || v.Note != "" || v.Coarse {
		t.Fatalf("%+v", v)
	}
	x, y := reconstructXMLView(v)
	if x != y || !strings.Contains(x, `xmlns:n1="urn:test"`) {
		t.Fatalf("规范化未消除格式变化：\n%s\n%s", x, y)
	}
	for _, block := range v.Blocks {
		if !block.Equal {
			t.Fatal("仅格式变化产生了结构变化行")
		}
	}
}

func TestXMLViewPreservesMixedContentWhitespaceAndNamespaceMeaning(t *testing.T) {
	for _, source := range []string{
		`<Root xmlns="urn:test"><TextCode> a&#xA;&#x9;&lt;b&gt; </TextCode><Mixed> A <B/> C </Mixed></Root>`,
		`<Root xml:space="preserve"> <A>  </A><B xml:space="default"> <C/> </B> </Root>`,
		`<Root xmlns="urn:root" xmlns:x="urn:attr" x:value="&quot;&amp;&#x9;"><Child xmlns=""> </Child></Root>`,
		`<?keep one?><Root><A> </A><?inside two?><B>value</B></Root><?keep three?>`,
		"<Root><?inside first\nsecond?><B>value</B></Root>",
		`<Root><Mixed>ab<!-- ignored -->cd<![CDATA[<e>]]><B/>f</Mixed></Root>`,
	} {
		v := xmlViewForTest(t, source, source)
		x, _ := reconstructXMLView(v)
		before, err := xmlFields(t.Context(), []byte(source))
		if err != nil {
			t.Fatal(err)
		}
		after, err := xmlFields(t.Context(), []byte(x))
		if err != nil {
			t.Fatal(err, x)
		}
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("规范化改变了 XML 语义：\n%s\n%s\n%v\n%v", source, x, before, after)
		}
	}
	v := xmlViewForTest(t, `<Root><TextCode> a </TextCode></Root>`, `<Root><TextCode>a</TextCode></Root>`)
	if v.Kind != "structure" {
		t.Fatal("TextCode 空白变化被忽略")
	}
	v = xmlViewForTest(t, `<Root xmlns="urn:a"/>`, `<Root xmlns="urn:b"/>`)
	if v.Kind != "structure" {
		t.Fatal("命名空间变化被忽略")
	}
}

func TestXMLViewAlignsInsertionsDeletionsAndFoldsOnlyEqualLines(t *testing.T) {
	var a, b strings.Builder
	a.WriteString("<Root>")
	b.WriteString("<Root>")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&a, "<Item n=\"%d\">same</Item>", i)
		if i == 12 {
			b.WriteString(`<Added>new</Added>`)
		}
		value := "same"
		if i == 20 {
			value = "changed"
		}
		fmt.Fprintf(&b, "<Item n=\"%d\">%s</Item>", i, value)
	}
	a.WriteString("</Root>")
	b.WriteString("</Root>")
	v := xmlViewForTest(t, a.String(), b.String())
	if v.Kind != "structure" || v.Coarse {
		t.Fatal(v)
	}
	folds, changes := 0, 0
	left, right := 0, 0
	for _, block := range v.Blocks {
		if block.Folded {
			folds++
			if !block.Equal {
				t.Fatal("变化行被折叠")
			}
		}
		if !block.Equal {
			changes++
		}
		for _, line := range block.Lines {
			if line.BeforeNumber > 0 {
				left++
				if line.BeforeNumber != left {
					t.Fatal("基准行号不连续")
				}
			}
			if line.AfterNumber > 0 {
				right++
				if line.AfterNumber != right {
					t.Fatal("目标行号不连续")
				}
			}
		}
	}
	if folds == 0 || changes != 2 || left != 32 || right != 33 {
		t.Fatalf("folds=%d changes=%d lines=%d/%d", folds, changes, left, right)
	}
	x, y := reconstructXMLView(v)
	for i, pair := range [][2]string{{a.String(), x}, {b.String(), y}} {
		af, _ := xmlFields(t.Context(), []byte(pair[0]))
		bf, err := xmlFields(t.Context(), []byte(pair[1]))
		if err != nil || !reflect.DeepEqual(af, bf) {
			t.Fatalf("重构 %d 失败：%v", i, err)
		}
	}
	for _, pair := range [][2]string{{"", b.String()}, {a.String(), ""}} {
		view := xmlViewForTest(t, pair[0], pair[1])
		if view.Kind != "added" && view.Kind != "removed" {
			t.Fatal(view.Kind)
		}
		for _, block := range view.Blocks {
			if block.Equal || block.Folded {
				t.Fatal("新增或删除 XML 存在相同行")
			}
		}
	}
}

func TestXMLViewLimitsAndCoarseFallbackAreExplicit(t *testing.T) {
	a := strings.Repeat("a", maxXMLViewSourceBytes)
	if _, err := parseXMLView(t.Context(), []byte("<Root>"+a+"</Root>"), map[string]string{}); err == nil {
		t.Fatal("未执行源大小上限")
	}
	view, err := buildXMLView(t.Context(), FileChange{Before: &File{Kind: "xml", XMLSHA256: "a"}, After: &File{Kind: "xml", XMLSHA256: "b"}}, map[string]string{"a": "a"}, map[string]string{"b": "b"}, nil, []byte("<Root/>"))
	if err != nil || view.Kind != "structure" || !strings.Contains(view.Note, "256 KiB") {
		t.Fatalf("%+v %v", view, err)
	}
	many := "<Root>" + strings.Repeat("<A/>", maxXMLViewLines) + "</Root>"
	if _, err := parseXMLView(t.Context(), []byte(many), map[string]string{}); err == nil {
		t.Fatal("未执行节点上限")
	}
	depth := strings.Repeat("<A>", 120) + strings.Repeat(`<B value="`+strings.Repeat("x", 55)+`"/>`, 3600) + strings.Repeat("</A>", 120)
	doc, err := parseXMLView(t.Context(), []byte(depth), map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := formatXMLView(t.Context(), doc, map[string]string{}); err == nil || !strings.Contains(err.Error(), "字节预算") {
		t.Fatal("缩进增长未受预算约束", err)
	}
	lines := "<Root>" + strings.Repeat("<A/>", 2100) + "</Root>"
	doc, err = parseXMLView(t.Context(), []byte(lines), map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := formatXMLView(t.Context(), doc, map[string]string{}); err != nil {
		t.Fatal("空元素应只占一行", err)
	}
	lines = "<Root>" + strings.Repeat("<A><B/></A>", 1400) + "</Root>"
	doc, err = parseXMLView(t.Context(), []byte(lines), map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := formatXMLView(t.Context(), doc, map[string]string{}); err == nil || !strings.Contains(err.Error(), "4000 行") {
		t.Fatal("未执行格式化行数上限", err)
	}
	left, right := make([]string, 600), make([]string, 600)
	for i := range left {
		left[i] = fmt.Sprintf("old %d", i)
		right[i] = fmt.Sprintf("new %d", i)
	}
	blocks, coarse, err := alignXMLLines(t.Context(), left, right)
	if err != nil || !coarse || len(blocks) != 1 || len(blocks[0].Lines) != 600 {
		t.Fatalf("粗粒度回退失败：%v %v %d", coarse, err, len(blocks))
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := alignXMLLines(ctx, left, right); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestXMLViewIntegrationRetainsFieldDiffAndReportsUnavailableXML(t *testing.T) {
	base := testOFD(t, "same")
	withXML := func(source string) string {
		return rewrite(t, base, func(n string, data []byte) (string, []byte) {
			if strings.HasSuffix(n, ".ttf") {
				return "unused.xml", []byte(source)
			}
			return n, data
		})
	}
	a := withXML(`<Root><Value>100</Value></Root>`)
	b := withXML(`<Root><Value>120</Value></Root>`)
	r, err := CompareFiles(t.Context(), []string{a, b}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	f := r.Comparisons[0].Files[0]
	if f.XMLView == nil || f.XMLView.Kind != "structure" || len(f.XMLView.Blocks) == 0 || len(f.XMLChanges) == 0 {
		t.Fatalf("XML 对照或原字段明细丢失：%+v", f)
	}
	for _, format := range []string{"html", "json", "text", "markdown"} {
		var out bytes.Buffer
		if err := WriteReport(&out, r, format); err != nil {
			t.Fatal(err)
		}
		if format == "json" {
			var decoded Report
			if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || !reflect.DeepEqual(decoded.Comparisons[0].Files[0].XMLView, f.XMLView) {
				t.Fatalf("XML 对照 JSON 往返失败：%v", err)
			}
		} else if !strings.Contains(out.String(), "XML 结构或内容变化") {
			t.Fatalf("%s 缺少 XML 变化类型", format)
		}
	}
	formatted := withXML("<Root>\n<Value>100</Value>\n</Root>")
	r, err = CompareFiles(t.Context(), []string{a, formatted}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if f := r.Comparisons[0].Files[0]; f.XMLView.Kind != "format" || len(f.XMLChanges) != 0 {
		t.Fatal("仅格式变化分类错误")
	}
	large := withXML("<Root><Value>" + strings.Repeat("a", maxXMLViewSourceBytes) + "</Value></Root>")
	r, err = CompareFiles(t.Context(), []string{a, large}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if f := r.Comparisons[0].Files[0]; f.XMLView.Kind != "structure" || len(f.XMLView.Blocks) != 0 || !strings.Contains(f.XMLView.Note, "256 KiB") || len(f.XMLChanges) == 0 {
		t.Fatal("大 XML 没有保留字段差异和展示限制提示")
	}
	c := withXML("<broken>")
	r, err = CompareFiles(t.Context(), []string{a, c}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Comparisons[0].Files[0].XMLView.Kind != "unavailable" {
		t.Fatal("畸形 XML 未说明不可用")
	}
}
