package fastxml

import (
	"testing"

	txml "github.com/tdewolff/parse/v2/xml"
)

func TestStripBOM(t *testing.T) {
	for _, tc := range []struct {
		name  string
		in    []byte
		want  []byte
		utf16 bool
	}{
		{"无 BOM", []byte("<a/>"), []byte("<a/>"), false},
		{"UTF-8 BOM", append([]byte{0xEF, 0xBB, 0xBF}, "<a/>"...), []byte("<a/>"), false},
		{"UTF-16 LE", append([]byte{0xFF, 0xFE}, 'a', 0), []byte{0xFF, 0xFE, 'a', 0}, true},
		{"UTF-16 BE", append([]byte{0xFE, 0xFF}, 0, 'a'), []byte{0xFE, 0xFF, 0, 'a'}, true},
	} {
		got, utf16 := StripBOM(tc.in)
		if string(got) != string(tc.want) || utf16 != tc.utf16 {
			t.Errorf("%s: StripBOM = (%q, %v), want (%q, %v)", tc.name, got, utf16, tc.want, tc.utf16)
		}
	}
}

func TestNameHelpers(t *testing.T) {
	for _, tc := range []struct {
		fn   func([]byte) []byte
		in   string
		want string
	}{
		{LocalName, "ofd:Document", "Document"},
		{LocalName, "Document", "Document"},
		{EndTagName, "</Page>", "Page"},
		{EndTagName, "</ofd:Page >", "Page"},
		{TargetName, "<Page ID=\"1\">", "Page"},
		{TargetName, "<ofd:Document xmlns:ofd=\"x\">", "Document"},
		{TargetName, "<Page/>", "Page"},
		{Unquote, `"abc"`, "abc"},
		{Unquote, `'abc'`, "abc"},
		{Unquote, `abc`, "abc"},
	} {
		if got := string(tc.fn([]byte(tc.in))); got != tc.want {
			t.Errorf("%q → %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestUnescape(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"plain", "plain"},
		{"a&amp;b", "a&b"},
		{"&lt;x&gt;", "<x>"},
		{"&#65;", "A"},
		{"&#x41;", "A"},
		{"&unknown;", "&unknown;"},
	} {
		if got := Unescape([]byte(tc.in)); got != tc.want {
			t.Errorf("Unescape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLexerAttrsAndSpan(t *testing.T) {
	data := []byte(`<Root><Page ID="7" BaseLoc="Pages/Page_0/Content.xml"/><Nested><Leaf>x</Leaf></Nested></Root>`)
	l := NewLexer(data)

	tt, buf, start := l.Next()
	if tt != txml.StartTagToken || string(TargetName(buf)) != "Root" {
		t.Fatalf("第一个 token = %v %q", tt, buf)
	}
	if void, err := l.SkipAttrs(); err != nil || void {
		t.Fatalf("SkipAttrs = %v, %v", void, err)
	}

	// <Page ID BaseLoc/> 自闭合，属性必须能取到。
	tt, buf, start = l.Next()
	if tt != txml.StartTagToken || string(TargetName(buf)) != "Page" {
		t.Fatalf("第二个 token = %v %q", tt, buf)
	}
	attrs := map[string]string{}
	void, err := l.ReadAttrs(func(name string, value []byte) error {
		attrs[name] = string(value)
		return nil
	})
	if err != nil || !void {
		t.Fatalf("ReadAttrs = %v, %v", void, err)
	}
	if attrs["ID"] != "7" || attrs["BaseLoc"] != "Pages/Page_0/Content.xml" {
		t.Fatalf("attrs = %v", attrs)
	}

	// <Nested>...</Nested> 的原始区间应完整覆盖子元素。
	tt, buf, start = l.Next()
	if tt != txml.StartTagToken || string(TargetName(buf)) != "Nested" {
		t.Fatalf("第三个 token = %v %q", tt, buf)
	}
	span, err := l.SpanElement(start)
	if err != nil {
		t.Fatal(err)
	}
	if want := `<Nested><Leaf>x</Leaf></Nested>`; string(span) != want {
		t.Fatalf("SpanElement = %q, want %q", span, want)
	}
}
