package fontrepair

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"slices"
	"testing"
)

func cffTestDictionaries(t *testing.T, b []byte) [][]byte {
	t.Helper()
	_, pos, err := cffIndex(b, int(b[2]))
	if err != nil {
		t.Fatal(err)
	}
	tops, _, err := cffIndex(b, pos)
	if err != nil {
		t.Fatal(err)
	}
	top, err := parseDict(tops[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, cid := top[0xc1e]; cid {
		fds, _, err := cffIndex(b, top.integer(0xc24))
		if err != nil {
			t.Fatal(err)
		}
		return fds
	}
	return tops
}

func TestCFFPrivateRepairRemovesLegacyFieldsAndKeepsSubroutines(t *testing.T) {
	for _, cid := range []bool{false, true} {
		for _, local := range []bool{false, true} {
			input := syntheticCFF(cid)
			input = slices.Grow(input, 3*len(input))
			// 构造旧生成器的私有字典，并保留各FD原有字宽。
			dicts := cffTestDictionaries(t, input)
			for _, dict := range dicts {
				fields, _ := parseDict(dict)
				ref := fields[18]
				private := slices.Clone(input[int(ref[1]):int(ref[1]+ref[0])])
				private = append(private, cffTestDict(0xc0f, 0)...)
				private = append(private, cffTestDict(0xc10, -1)...)
				if local {
					private = append(private, cffTestDict(19, len(private)+6)...)
				}
				entries, _ := dictEntries(dict)
				for _, entry := range entries {
					if entry.op == 18 {
						copy(dict[entry.start:entry.end], cffTestDict(18, len(private), len(input)))
					}
				}
				input = append(input, private...)
				if local {
					input = append(input, cffTestIndex([]byte{139, 11})...)
				}
			}
			saved := slices.Clone(input)
			fixed := repairCFFPrivate(input)
			if bytes.Equal(fixed, input) || !bytes.Equal(input, saved) {
				t.Fatalf("CID=%v LocalSubrs=%v：未修复或修改了原字体", cid, local)
			}
			for i, dict := range cffTestDictionaries(t, fixed) {
				fields, _ := parseDict(dict)
				ref := fields[18]
				private, err := parseDict(fixed[int(ref[1]):int(ref[1]+ref[0])])
				if err != nil || private[0xc0f] != nil || private[0xc10] != nil {
					t.Fatalf("私有字典仍有旧字段：%v", err)
				}
				old, _ := parseDict(dicts[i])
				want, err := privateData(input, old)
				if err != nil {
					t.Fatal(err)
				}
				got, err := privateData(fixed, fields)
				if err != nil || got.defaultWidth != want.defaultWidth || got.nominalWidth != want.nominalWidth ||
					!slices.EqualFunc(got.subrs, want.subrs, bytes.Equal) {
					t.Fatalf("私有字宽或局部子程序改变：%v", err)
				}
			}
			if !bytes.Equal(fixed, repairCFFPrivate(fixed)) {
				t.Fatal("重复修复改变了结果")
			}
			otf, err := Repair(input)
			if err != nil || checksum(otf) != 0xb1b0afba {
				t.Fatalf("修复后OTF封装失败：%v", err)
			}
			if again, err := Repair(otf); err != nil || !bytes.Equal(otf, again) {
				t.Fatalf("OTF重复修复不稳定：%v", err)
			}
		}
	}
}

func TestCFFPrivateRepairLeavesEncryptedType1Declaration(t *testing.T) {
	input := syntheticCFF(false)
	input = slices.Grow(input, 64)
	dict := cffTestDictionaries(t, input)[0]
	private := append(cffTestDict(20, 900), cffTestDict(0xc10, 4)...)
	entries, _ := dictEntries(dict)
	for _, entry := range entries {
		if entry.op == 18 {
			copy(dict[entry.start:entry.end], cffTestDict(18, len(private), len(input)))
		}
	}
	input = append(input, private...)
	if !bytes.Equal(input, repairCFFPrivate(input)) {
		t.Fatal("带加密声明的字形程序被当成未加密程序修复")
	}
}

func TestFixedDictIntegerDoesNotOverflowOriginalOperand(t *testing.T) {
	for _, tc := range []struct{ size, value int }{{1, 107}, {1, -107}, {2, 108}, {2, 1131}, {3, 32767}, {3, -32768}, {5, 1 << 24}} {
		b := make([]byte, tc.size)
		if !fixedDictInteger(b, tc.value) {
			t.Fatalf("合法整数无法编码：%v", tc)
		}
		value, n, err := dictNumber(b)
		if err != nil || value != float64(tc.value) || n != tc.size {
			t.Fatalf("定长编码不一致：%v，得到%g/%d/%v", tc, value, n, err)
		}
	}
	for _, tc := range []struct{ size, value int }{{1, 108}, {2, 107}, {2, 1132}, {3, 32768}, {4, 10}} {
		if fixedDictInteger(make([]byte, tc.size), tc.value) {
			t.Fatalf("编码接受了超出原长度的整数：%v", tc)
		}
	}
}

func TestIntroEmbeddedFontsRepairKeepsMetricsAndGlyphPrograms(t *testing.T) {
	z, err := zip.OpenReader("../../testdata/ofdrw/intro.ofd")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skip("缺少真实字体样例: ../../testdata/ofdrw/intro.ofd")
		}
		t.Fatal(err)
	}
	defer z.Close()
	count, cffCount := 0, 0
	for _, file := range z.File {
		if !bytes.HasPrefix([]byte(file.Name), []byte("Doc_0/Res/font_")) {
			continue
		}
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		input, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		// 官方阅读器产物中的扩展名不总与字体格式对应，按数据识别。
		original, source := input, input
		var oldCFF, newCFF []byte
		if isCFF(input) {
			// 只清理 Private DICT 时，字形程序必须原样保留；完整裸 CFF 修复
			// 另有 cntrmask 兼容改写，不能用它断言原始 CharStrings 字节不变。
			oldCFF, newCFF = input, repairCFFPrivate(input)
			original, err = Repair(input)
			if err != nil {
				t.Fatalf("字体 %s 包装失败: %v", file.Name, err)
			}
			source = newCFF
		}
		fixed, err := Repair(source)
		if err != nil {
			t.Fatalf("字体 %s 修复失败: %v", file.Name, err)
		}
		before, err := readDirectory(original)
		if err != nil {
			t.Fatalf("字体 %s 原始 SFNT 无效: %v", file.Name, err)
		}
		after, err := readDirectory(fixed)
		if err != nil {
			t.Fatalf("字体 %s 修复后 SFNT 无效: %v", file.Name, err)
		}
		for _, tag := range []string{"hmtx", "hhea", "maxp", "glyf", "loca"} {
			if !bytes.Equal(before.get(tag), after.get(tag)) {
				t.Fatalf("字体%s的%s度量改变", file.Name, tag)
			}
		}
		count++
		if oldCFF == nil {
			oldCFF, newCFF = before.get("CFF "), after.get("CFF ")
		}
		if len(oldCFF) == 0 {
			continue
		}
		glyphs := func(data []byte) [][]byte {
			t.Helper()
			_, pos, err := cffIndex(data, int(data[2]))
			if err != nil {
				t.Fatal(err)
			}
			tops, _, err := cffIndex(data, pos)
			if err != nil || len(tops) != 1 {
				t.Fatalf("字体 %s 的 Top DICT 无效: %v", file.Name, err)
			}
			top, err := parseDict(tops[0])
			if err != nil {
				t.Fatal(err)
			}
			values, _, err := cffIndex(data, top.integer(17))
			if err != nil {
				t.Fatal(err)
			}
			return values
		}
		oldGlyphs, newGlyphs := glyphs(oldCFF), glyphs(newCFF)
		if !slices.EqualFunc(oldGlyphs, newGlyphs, bytes.Equal) {
			t.Fatal("修复改变了字形程序")
		}
		for _, dict := range cffTestDictionaries(t, newCFF) {
			fields, _ := parseDict(dict)
			ref := fields[18]
			if len(ref) != 2 {
				continue
			}
			private, _ := parseDict(newCFF[int(ref[1]):int(ref[1]+ref[0])])
			if private[0xc0f] != nil || private[0xc10] != nil {
				t.Fatalf("%s仍含浏览器拒绝的私有项", file.Name)
			}
		}
		cffCount++
	}
	if cffCount == 0 || count == cffCount {
		t.Fatalf("intro.ofd 未覆盖 TrueType 和 CFF 字体: 总计 %d，CFF %d", count, cffCount)
	}
	t.Logf("核对了 %d 份内嵌字体，其中 CFF %d 份", count, cffCount)
}
