package creator

import (
	"bytes"
	"encoding/xml"
	"math"
	"math/rand"
	"strconv"
	"testing"
)

// TestStreamXMLWriterReplacesInvalidXMLChars 验证文本和属性中的 XML 非法字符
// （如 PDF 文本里的控制符）会被替换为 U+FFFD，否则生成的 OFD 无法解析
// （PCDATA invalid Char value）。
func TestStreamXMLWriterReplacesInvalidXMLChars(t *testing.T) {
	w := newStreamXMLWriter()
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

	attr := newStreamXMLWriter()
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

// referenceAppendFloat 是 appendFloat 的参考实现：strconv 'f'4 + 尾零裁剪，
// 作为差分测试的基准。
func referenceAppendFloat(dst []byte, value float64) []byte {
	if !finite(value) {
		value = 0
	}
	start := len(dst)
	dst = strconv.AppendFloat(dst, value, 'f', numberPrecision, 64)
	end := len(dst)
	for end > start+1 && dst[end-1] == '0' {
		end--
	}
	if end > start+1 && dst[end-1] == '.' {
		end--
	}
	dst = dst[:end]
	if end == start+2 && dst[start] == '-' && dst[start+1] == '0' {
		dst[start] = '0'
		dst = dst[:start+1]
	}
	return dst
}

// TestAppendFloatMatchesReference 用随机与边界值差分验证 appendFloat 与
// strconv 'f'4 裁剪语义逐字节一致，防止整数快路径与兜底路径产生分叉。
func TestAppendFloatMatchesReference(t *testing.T) {
	checks := []float64{
		0, -0.0, 1, -1, 42, -42, 1000000, -1000000,
		0.5, -0.5, 0.25, -0.25, 96.35, -96.35, 4.2333333333,
		0.00004, -0.00004, 0.00005, -0.00005, 0.9999, -0.9999,
		595.2745, 595.2746, 123.4567, -0.4, 1e15, -1e15,
		9.223372036854775e18, -9.223372036854775e18,
		math.MaxFloat64, math.SmallestNonzeroFloat64,
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
	want := string(referenceAppendFloat(nil, value))
	got := string(appendFloat(nil, value))
	if want != got {
		t.Fatalf("value=%v: got %q, want %q", value, got, want)
	}
}
