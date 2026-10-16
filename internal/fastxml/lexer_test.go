package fastxml

import (
	"strings"
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

var unescapeTests = []struct {
	name string
	in   string
	want string
}{
	{"empty", "", ""},
	{"plain", "plain", "plain"},
	{"long plain", strings.Repeat("甲a", 256), strings.Repeat("甲a", 256)},
	{"predefined entities", `&amp;&lt;&gt;&quot;&apos;`, `&<>"'`},
	{"decimal", "&#65;", "A"},
	{"hex", "&#x41;", "A"},
	{"four byte rune", "a&#x1F600;b", "a😀b"},
	{"highest rune", "&#x10FFFF;", "\U0010FFFF"},
	{"out of range rune", "&#x110000;", "\uFFFD"},
	{"unknown", "&unknown;", "&unknown;"},
	{"incomplete", "&amp", "&amp"},
	{"ampersand", "&", "&"},
	{"empty entity", "&;", "&;"},
	{"semicolons", ";;;", ";;;"},
	{"invalid number", "&#xZZ;", "&#xZZ;"},
	{"nested ampersand", "&&amp;", "&&amp;"},
	{"entity at length limit", "&#00000000000065;", "A"},
	{"entity above length limit", "&#000000000000065;", "&#000000000000065;"},
	{"long unknown then valid", "&abcdefghijklmnop;&amp;", "&abcdefghijklmnop;&"},
	{"unterminated run", strings.Repeat("&", 4096), strings.Repeat("&", 4096)},
	{"mixed text", "尾&amp;实体&#x1F600;结束", "尾&实体😀结束"},
	{"raw bytes", "\xff&amp;\xfe", "\xff&\xfe"},
}

func TestUnescape(t *testing.T) {
	for _, tc := range unescapeTests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Unescape([]byte(tc.in)); got != tc.want {
				t.Errorf("Unescape(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAppendUnescape(t *testing.T) {
	for _, tc := range unescapeTests {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte(tc.in)
			if got := string(AppendUnescape(nil, input)); got != tc.want {
				t.Errorf("AppendUnescape(nil, %q) = %q, want %q", tc.in, got, tc.want)
			}
			const prefix = "pre:"
			for _, capacity := range []int{len(prefix), len(prefix) + len(input)} {
				dst := make([]byte, len(prefix), capacity)
				copy(dst, prefix)
				if got := string(AppendUnescape(dst, input)); got != prefix+tc.want {
					t.Errorf("AppendUnescape(prefix, %q), cap=%d: got %q, want %q", tc.in, capacity, got, prefix+tc.want)
				}
			}
			if string(input) != tc.in {
				t.Fatal("AppendUnescape 修改了输入")
			}
		})
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
	void, err := l.ReadAttrs(func(name, value []byte) error {
		attrs[string(name)] = string(value)
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
