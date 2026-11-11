package spec

import (
	"encoding/xml"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func FuzzNumericFieldsMatchStringsFields(f *testing.F) {
	for _, input := range []string{
		"", "1 2 3 4", "1\t2\n3\r4\v5\f6", "1\u00a02\u00853\u30004",
		" 1 2 3 4 5 6 7 8 9 ", "\x00 \xff 1", strings.Repeat("0 ", 10000),
	} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		want := strings.Fields(input)
		if count := countFields(input); count != len(want) {
			t.Fatalf("countFields(%q) = %d, want %d", input, count, len(want))
		}
		var fields [6]string
		if count := scanFields(input, fields[:]); count != len(want) {
			t.Fatalf("scanFields(%q) = %d, want %d", input, count, len(want))
		}
		for i := 0; i < min(len(fields), len(want)); i++ {
			if fields[i] != want[i] {
				t.Fatalf("字段 %d = %q, want %q", i, fields[i], want[i])
			}
		}
	})
}

// referenceStArray 保留优化前的 Fields 与逐次 append 路径，验证分词和展开行为。
func referenceStArray(input string) (StArray[float64], error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return StArray[float64]{}, nil
	}
	parts := strings.Fields(input)
	if !strings.Contains(input, "g") {
		if len(parts) > stArrayMaxElements {
			return nil, errStArrayMaxElements
		}
		result := make(StArray[float64], len(parts))
		for i, part := range parts {
			value, err := parseFiniteFloat(part, "数组元素")
			if err != nil {
				return nil, fmt.Errorf("无效的浮点数 %q: %w", part, err)
			}
			result[i] = value
		}
		return result, nil
	}
	var result StArray[float64]
	needCount, hasCount, count := false, false, 0
	for _, part := range parts {
		if part == "g" {
			if needCount {
				return nil, errors.New("重复展开标记 g 缺少重复次数")
			}
			if hasCount {
				return nil, errors.New("展开次数后缺少重复值")
			}
			needCount = true
			continue
		}
		if needCount {
			var err error
			count, err = strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("无效的展开次数 %q: %w", part, err)
			}
			if count < 0 || count > stArrayMaxRepeat {
				return nil, errStArrayMaxRepeat
			}
			needCount, hasCount = false, true
			continue
		}
		value, err := parseFiniteFloat(part, "数组元素")
		if err != nil {
			return nil, fmt.Errorf("无效的浮点数 %q: %w", part, err)
		}
		n := 1
		if hasCount {
			n = count
		}
		if len(result)+n > stArrayMaxElements {
			return nil, errStArrayMaxElements
		}
		for range n {
			result = append(result, value)
		}
		hasCount = false
	}
	if needCount {
		return nil, errors.New("展开标记 g 后缺少重复次数")
	}
	return result, nil
}

func FuzzNumericArraysMatchFieldsParser(f *testing.F) {
	for _, input := range []string{
		"", "1.5 0 -0 1e2", "1\u00a02\u30003", "1 g 3 2 3", "g 0 2",
		"g 2", "g", "g g 2", "g 2 g 3", "g -1 0", "g 100001 0", //nolint:dupword // 故意构造重复的 g 指令。
		"NaN", "Inf", "1 nope", "1 2 3x", "\xff 2", "+1 0x10 2",
	} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		want, wantErr := referenceStArray(input)
		got := StArray[float64]{99}
		gotErr := got.parseString(input)
		if (gotErr == nil) != (wantErr == nil) || gotErr != nil && gotErr.Error() != wantErr.Error() {
			t.Fatalf("浮点数组 %q: got %v, want %v", input, gotErr, wantErr)
		}
		if gotErr == nil {
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("浮点数组 %q: got %v, want %v", input, got, want)
			}
		} else if !reflect.DeepEqual(got, StArray[float64]{99}) {
			t.Fatal("失败的解析修改了原数组")
		}
		wantInts := StArray[int]{}
		for _, part := range strings.Fields(input) { //nolint:modernize // 独立参照实现保留 Fields 分词路径。
			if value, err := strconv.Atoi(part); err == nil {
				wantInts = append(wantInts, value)
			}
		}
		var gotInts StArray[int]
		if err := gotInts.parseString(input); err != nil || !reflect.DeepEqual(gotInts, wantInts) {
			t.Fatalf("整数数组 %q: got %v (%v), want %v", input, gotInts, err, wantInts)
		}
	})
}

func TestNumericAttributesPreserveWhitespaceAndFailureState(t *testing.T) {
	var box StBox
	if err := box.parseFromString("1\u00852\u00a0-3\u30004"); err != nil || box != (StBox{1, 2, -3, 4}) {
		t.Fatalf("Unicode Boundary: %+v, %v", box, err)
	}
	var matrix CTM
	if err := matrix.UnmarshalXMLAttr(xml.Attr{Value: "1\u00850\u00a00\u30001\t2\n3"}); err != nil || matrix != (CTM{1, 0, 0, 1, 2, 3}) {
		t.Fatalf("Unicode CTM: %v, %v", matrix, err)
	}
	box = StBox{9, 9, 9, 9}
	if err := box.parseFromString("1 2 bad 4"); err == nil || box != (StBox{1, 2, 9, 9}) {
		t.Fatalf("Boundary 错误时的部分写入行为改变: %+v, %v", box, err)
	}
	matrix = CTM{9, 9, 9, 9, 9, 9}
	if err := matrix.UnmarshalXMLAttr(xml.Attr{Value: "1 2 bad 4 5 6"}); err == nil || matrix != (CTM{1, 2, 9, 9, 9, 9}) {
		t.Fatalf("CTM 错误时的部分写入行为改变: %v, %v", matrix, err)
	}
	if err := box.parseFromString("bad 2 3 4 5 6 7"); err == nil || err.Error() != "格式错误: 期望4个数值(X Y Width Height)，实际得到7个" {
		t.Fatalf("Boundary 字段数错误改变: %v", err)
	}
	if err := matrix.UnmarshalXMLAttr(xml.Attr{Value: "bad 2 3 4 5 6 7 8"}); err == nil || err.Error() != "OFD CTM需要6个值，但得到 8 个" {
		t.Fatalf("CTM 字段数错误改变: %v", err)
	}
}
