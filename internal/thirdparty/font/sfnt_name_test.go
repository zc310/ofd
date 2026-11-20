package font

import "testing"

func TestNameRecordStringPreservesUnsupportedEncodingBytes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		platform PlatformID
		encoding EncodingID
	}{
		{"Android 字体中的 Macintosh 简体中文名称", PlatformMacintosh, EncodingMacintoshChineseSimplified},
		{"Macintosh 日文名称", PlatformMacintosh, EncodingMacintoshJapanese},
		{"自定义平台名称", PlatformCustom, 0},
		{"未知平台名称", PlatformID(0xffff), EncodingID(0xffff)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// 未实现的名称编码不影响字形使用，也不能把空解码器传入 transform.String。
			value := []byte("Android Sans \x80\xff")
			record := nameRecord{Platform: tc.platform, Encoding: tc.encoding, Value: value}
			if got := record.String(); got != string(value) {
				t.Fatalf("不支持的编码应保留原始字节: got=%q want=%q", got, value)
			}
		})
	}
}

func TestNameRecordStringStillDecodesSupportedEncodings(t *testing.T) {
	for _, tc := range []struct {
		name   string
		record nameRecord
		want   string
	}{
		{"Unicode", nameRecord{Platform: PlatformUnicode, Encoding: EncodingUnicode2BMP, Value: []byte{0, 'G', 0x4e, 0x2d}}, "G中"},
		{"Windows", nameRecord{Platform: PlatformWindows, Encoding: EncodingWindowsUnicodeBMP, Value: []byte{0, 'G', 0x4e, 0x2d}}, "G中"},
		{"Macintosh Roman", nameRecord{Platform: PlatformMacintosh, Encoding: EncodingMacintoshRoman, Value: []byte{'C', 'a', 'f', 0x8e}}, "Café"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.record.String(); got != tc.want {
				t.Fatalf("名称解码错误: got=%q want=%q", got, tc.want)
			}
		})
	}
}
