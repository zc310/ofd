package fastxml

import (
	"bytes"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// referenceXMLEntity 保留优化前的 strconv 实现，独立验证数字字符引用的兼容语义。
func referenceXMLEntity(ent []byte) (rune, bool) {
	switch string(ent) {
	case "amp":
		return '&', true
	case "lt":
		return '<', true
	case "gt":
		return '>', true
	case "quot":
		return '"', true
	case "apos":
		return '\'', true
	}
	if len(ent) > 1 && ent[0] == '#' {
		s := ent[1:]
		base := 10
		if len(s) > 0 && (s[0] == 'x' || s[0] == 'X') {
			s = s[1:]
			base = 16
		}
		if v, err := strconv.ParseUint(string(s), base, 32); err == nil {
			return rune(v), true
		}
	}
	return 0, false
}

func TestNumericEntitiesMatchParseUint(t *testing.T) {
	inputs := []string{
		"", "#", "#x", "#X", "#0", "#00000", "#65", "#x41", "#XAf",
		"#4294967295", "#4294967296", "#4294967290", "#4294967299",
		"#xFFFFFFFF", "#x100000000", "#xFFFFFFF0", "#xFFFFFFFG",
		"#55296", "#xDFFF", "#x10FFFF", "#x110000", "#2147483648",
		"#+65", "#-65", "#x+41", "# 65", "#65 ", "#6_5", "#x0x41",
		"#xg", "#x/", "#x:", "#x`", "#x\xff", "#\x00", "amp", "unknown",
		"#" + strings.Repeat("0", 100) + "65",
	}
	for value := range 256 {
		inputs = append(inputs, "#"+string([]byte{byte(value)}), "#x"+string([]byte{byte(value)}))
	}
	rng := rand.New(rand.NewPCG(1, 2))
	for range 10000 {
		value := rng.Uint64N(1 << 33)
		inputs = append(inputs, "#"+strconv.FormatUint(value, 10), "#x"+strconv.FormatUint(value, 16))
	}
	for _, input := range inputs {
		want, wantOK := referenceXMLEntity([]byte(input))
		got, gotOK := xmlEntity([]byte(input))
		if got != want || gotOK != wantOK {
			t.Fatalf("实体 %q: got (%U, %v), want (%U, %v)", input, got, gotOK, want, wantOK)
		}
	}
}

func referenceAppendUnescape(dst, b []byte) []byte {
	for index := bytes.IndexByte(b, '&'); index >= 0; index = bytes.IndexByte(b, '&') {
		dst = append(dst, b[:index]...)
		b = b[index:]
		end := bytes.IndexByte(b[:min(len(b), 17)], ';')
		if end < 0 {
			dst = append(dst, '&')
			b = b[1:]
		} else {
			if r, ok := referenceXMLEntity(b[1:end]); ok {
				dst = utf8.AppendRune(dst, r)
			} else {
				dst = append(dst, b[:end+1]...)
			}
			b = b[end+1:]
		}
	}
	return append(dst, b...)
}

func FuzzUnescapeMatchesReference(f *testing.F) {
	for _, test := range unescapeTests {
		f.Add([]byte(test.in))
	}
	for _, input := range []string{
		"&#xD800;&#4294967295;&#4294967296;",
		strings.Repeat("&", 4096) + ";&amp;",
		"&" + strings.Repeat("a", 10000) + ";&amp;",
		strings.Repeat("甲&amp;", 100),
	} {
		f.Add([]byte(input))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		original := bytes.Clone(input)
		want := referenceAppendUnescape(nil, input)
		if got := Unescape(input); got != string(want) {
			t.Fatalf("Unescape(%q): got %q, want %q", input, got, want)
		}
		if got := AppendUnescape([]byte("prefix:"), input); !bytes.Equal(got, append([]byte("prefix:"), want...)) {
			t.Fatalf("AppendUnescape(%q): got %q, want prefix:%q", input, got, want)
		}
		if !bytes.Equal(input, original) {
			t.Fatal("实体解码修改了输入")
		}
	})
}

func TestUnescapeReturnsIndependentString(t *testing.T) {
	for _, input := range []string{"plain", "甲&amp;乙", strings.Repeat("甲a", 256) + "&amp;"} {
		data := []byte(input)
		want := string(referenceAppendUnescape(nil, data))
		got := Unescape(data)
		clear(data)
		if got != want {
			t.Fatalf("输入修改影响了解码结果: got %q, want %q", got, want)
		}
	}
}
