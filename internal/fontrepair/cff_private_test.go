package fontrepair

import (
	"archive/zip"
	"bytes"
	"io"
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

func TestGB38540EmbeddedFontsRepairKeepsMetricsAndGlyphPrograms(t *testing.T) {
	z, err := zip.OpenReader("../../testdata/other/GBT_38540-2020.ofd")
	if err != nil {
		t.Skipf("可选真实样例不可用：%v", err)
	}
	defer z.Close()
	count := 0
	for _, file := range z.File {
		if !bytes.HasSuffix([]byte(file.Name), []byte(".otf")) {
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
		fixed, err := Repair(input)
		if err != nil {
			t.Fatal(err)
		}
		before, _ := readDirectory(input)
		after, _ := readDirectory(fixed)
		for _, tag := range []string{"hmtx", "hhea", "maxp"} {
			if !bytes.Equal(before.get(tag), after.get(tag)) {
				t.Fatalf("字体%s的%s度量改变", file.Name, tag)
			}
		}
		oldCFF, newCFF := before.get("CFF "), after.get("CFF ")
		_, pos, _ := cffIndex(oldCFF, int(oldCFF[2]))
		tops, _, _ := cffIndex(oldCFF, pos)
		top, _ := parseDict(tops[0])
		oldGlyphs, _, _ := cffIndex(oldCFF, top.integer(17))
		newGlyphs, _, _ := cffIndex(newCFF, top.integer(17))
		if !slices.EqualFunc(oldGlyphs, newGlyphs, bytes.Equal) {
			t.Fatal("修复改变了字形程序")
		}
		for _, dict := range cffTestDictionaries(t, newCFF) {
			fields, _ := parseDict(dict)
			ref := fields[18]
			private, _ := parseDict(newCFF[int(ref[1]):int(ref[1]+ref[0])])
			if private[0xc0f] != nil || private[0xc10] != nil {
				t.Fatalf("%s仍含浏览器拒绝的私有项", file.Name)
			}
		}
		count++
	}
	if count == 0 {
		t.Fatal("样例没有内嵌OTF字体")
	}
	t.Logf("核对了%d份内嵌字体", count)
}
