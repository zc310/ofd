package csv

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// RFC 4180 引号、空字段、前导零及显式换行必须在进入排版前完整保留。
func TestReadRecordsPreservesCSVAndTSVFields(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		comma       rune
		want        [][]string
	}{
		{"csv", "ID,Note,Empty\r\n0007,\"a,b \"\"quoted\"\"\r\nnext\",\r\n", ',', [][]string{{"ID", "Note", "Empty"}, {"0007", "a,b \"quoted\"\nnext", ""}}},
		{"tsv", "ID\tNote\tEmpty\n0007\t\"a\tb\"\t\n", '\t', [][]string{{"ID", "Note", "Empty"}, {"0007", "a\tb", ""}}},
		{"spaces", " a ,b\n 001 , x \n", ',', [][]string{{" a ", "b"}, {" 001 ", " x "}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readRecords(t.Context(), []byte(tc.input), tc.comma)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("records=%q error=%v，期望 %q", got, err, tc.want)
			}
		})
	}
}

func TestReadRecordsRejectsMalformedOrOversizedTables(t *testing.T) {
	for _, input := range []string{
		"", "\n\r\n", "a,b\n1\n", "a,b\n1,2,3\n", "a\n\"unterminated\n",
		strings.Repeat("x,", maxColumns) + "x\n",
		strings.Repeat("x\n", maxRecords+1),
	} {
		if _, err := readRecords(t.Context(), []byte(input), ','); err == nil {
			t.Fatalf("无效表格未返回错误（输入 %d 字节）", len(input))
		}
	}
}

func TestDecodeTextHandlesBOMAndExplicitChineseEncodings(t *testing.T) {
	text := []byte("名称,编号\n测试,0001\n")
	gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes(text)
	if err != nil {
		t.Fatal(err)
	}
	gb18030, err := simplifiedchinese.GB18030.NewEncoder().Bytes(text)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		encoding string
		data     []byte
	}{
		{"utf-8", append([]byte{0xEF, 0xBB, 0xBF}, text...)}, {"gbk", gbk}, {"gb18030", gb18030},
	} {
		got, err := decodeText(tc.data, tc.encoding)
		if err != nil || !bytes.Equal(got, text) {
			t.Fatalf("%s 解码结果=%q error=%v", tc.encoding, got, err)
		}
	}
	if _, err := decodeText(gbk, "utf-8"); err == nil {
		t.Fatal("未指定 GBK 编码时没有报错")
	}
}

func TestReadLimitedStopsAtLimitAndPreservesCancellation(t *testing.T) {
	if _, err := readLimited(t.Context(), strings.NewReader("12345"), 4); err == nil {
		t.Fatal("没有执行输入大小限制")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readLimited(ctx, strings.NewReader("123"), 4); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误丢失: %v", err)
	}
}
