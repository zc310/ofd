package xmlcodec

import (
	"bytes"
	"encoding/xml"
	"math"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// referenceAppendEscaped 是 appendEscaped 的参考实现：逐 rune 解码并按
// XML 1.0 字符表转义/替换，作为差分测试基准。
func referenceAppendEscaped(dst []byte, value string, textMode bool) []byte {
	for index := 0; index < len(value); {
		r, size := utf8.DecodeRuneInString(value[index:])
		if r == utf8.RuneError && size == 1 {
			dst = append(dst, replacement...)
			index++
			continue
		}
		switch r {
		case '&':
			dst = append(dst, "&amp;"...)
		case '<':
			dst = append(dst, "&lt;"...)
		case '>':
			if textMode {
				dst = append(dst, "&gt;"...)
			} else {
				dst = append(dst, '>')
			}
		case '"':
			if textMode {
				dst = append(dst, '"')
			} else {
				dst = append(dst, "&quot;"...)
			}
		case '\t':
			if textMode {
				dst = append(dst, '\t')
			} else {
				dst = append(dst, "&#x9;"...)
			}
		case '\n':
			if textMode {
				dst = append(dst, '\n')
			} else {
				dst = append(dst, "&#xA;"...)
			}
		case '\r':
			dst = append(dst, "&#xD;"...)
		default:
			if isXMLChar(r) {
				dst = append(dst, value[index:index+size]...)
			} else {
				dst = append(dst, replacement...)
			}
		}
		index += size
	}
	return dst
}

// TestAppendEscapedMatchesReference 差分验证文本与属性转义快路径逐字节
// 等价于参考实现，覆盖特殊字节、控制符、DEL 与多字节 UTF-8。
func TestAppendEscapedMatchesReference(t *testing.T) {
	inputs := []string{
		"", "plain ascii", `A&B<C>"D'`,
		"line\nbreak\ttab\rreturn",
		"闭合 C 与坐标 0 0 210 297", "中文文本：甲&乙<丙>丁",
		"\x00\x01\x1f\x0b\x0c\x1d 控制符",
		"DEL:\x7f 合法?",
		"混合<b>标签</b>与&amp;实体",
		"多字节：\u65e5\u672c\u8a9e\x1e尾",
		"\xf0\x9f\x98\x80 emoji 结尾", // 四字节字符。
		"invalid utf8: \xe4\xb8\xad\xff\xfe 中文",
		"开头特殊&，中间<，结尾>",
		"非字符 \uFFFE\uFFFF 混在中文里",
		"全角，\uFFFD 与 \uFFFF 相邻",
		"长中文段落" + strings.Repeat("甲乙丙丁戊己庚辛壬癸", 30),
		strings.Repeat("平铺的文本", 20),
	}
	for _, value := range inputs {
		for _, textMode := range []bool{true, false} {
			want := referenceAppendEscaped(nil, value, textMode)
			w := &Writer{}
			w.appendEscaped(value, textMode)
			got := w.Bytes()
			if !bytes.Equal(got, want) {
				t.Fatalf("textMode=%v value=%q: got %q, want %q", textMode, value, got, want)
			}
		}
	}
}

// TestStreamXMLWriterReplacesInvalidXMLChars 验证文本和属性中的 XML 非法字符
// （如 PDF 文本里的控制符）会被替换为 U+FFFD，否则生成的 OFD 无法解析
// （PCDATA invalid Char value）。
func TestStreamXMLWriterReplacesInvalidXMLChars(t *testing.T) {
	w := New()
	w.Start("TextCode")
	w.Text("A\x1DB\x00C\x0bD\x0cE")
	w.End()
	out := w.Bytes()
	for _, b := range out {
		if b < 0x20 && b != 0x09 && b != 0x0A && b != 0x0D {
			t.Fatalf("输出含非法 XML 字节 %#x:\n%s", b, out)
		}
	}
	if !bytes.Contains(out, []byte("\uFFFD")) {
		t.Fatalf("非法字符未被替换:\n%s", out)
	}
	var parsed struct{}
	if err := xml.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("生成的 XML 无法解析: %v\n%s", err, out)
	}

	attr := New()
	attr.Start("TextObject")
	attr.Attr("Value", "x\x1Dy")
	attr.End()
	for _, b := range attr.Bytes() {
		if b < 0x20 && b != 0x09 && b != 0x0A && b != 0x0D {
			t.Fatalf("属性含非法 XML 字节 %#x:\n%s", b, attr.Bytes())
		}
	}
	if err := xml.Unmarshal(attr.Bytes(), &parsed); err != nil {
		t.Fatalf("属性 XML 无法解析: %v\n%s", err, attr.Bytes())
	}
}

// referenceFloat 使用标准库格式化与字符串裁剪作为独立基准，
// 不复用生产代码的整数快路径和字节切片裁剪逻辑。
func referenceFloat(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "0"
	}
	formatted := strconv.FormatFloat(value, 'f', 4, 64)
	formatted = strings.TrimSuffix(strings.TrimRight(formatted, "0"), ".")
	if formatted == "-0" {
		return "0"
	}
	return formatted
}

// TestAppendFloatMatchesReference 用随机与边界值差分验证 AppendFloat 与
// strconv 'f'4 裁剪语义逐字节一致，防止整数快路径与兜底路径产生分叉。
func TestAppendFloatMatchesReference(t *testing.T) {
	checks := []float64{
		0, math.Copysign(0, -1), 1, -1, 42, -42, 1000000, -1000000,
		0.5, -0.5, 0.25, -0.25, 96.35, -96.35, 4.2333333333,
		0.00004, -0.00004, 0.00005, -0.00005, 0.9999, -0.9999,
		595.2745, 595.2746, 123.4567, -0.4, 1e15, -1e15,
		9.223372036854775e18, -9.223372036854775e18,
		math.MaxFloat64, math.SmallestNonzeroFloat64,
		0x1p63, -0x1p63, math.Nextafter(0x1p63, 0),
		math.NaN(), math.Inf(1), math.Inf(-1),
	}
	for _, value := range checks {
		compareFloat(t, value)
	}

	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 200000; i++ {
		compareFloat(t, math.Round((rng.Float64()-0.5)*2e7*10000)/10000)
		compareFloat(t, (rng.Float64()-0.5)*1e9)
		compareFloat(t, float64(rng.Int63n(2e6)-1e6))
		compareFloat(t, (rng.Float64()-0.5)*1e18)
	}
}

func compareFloat(t *testing.T, value float64) {
	t.Helper()
	want := referenceFloat(value)
	got := string(AppendFloat(nil, value))
	if want != got {
		t.Fatalf("value=%v: got %q, want %q", value, got, want)
	}
}
