package models

import (
	"encoding/xml"
	"image/color"
	"math"
	"testing"
)

func TestStIDUnmarshalXMLInvalidValueUsesZero(t *testing.T) {
	var id StID = 42
	if err := xml.Unmarshal([]byte("<ID>invalid</ID>"), &id); err != nil {
		t.Fatal(err)
	}
	if id != 0 {
		t.Fatalf("StID = %d, want 0", id)
	}
}

func TestStIDUnmarshalXMLAttrInvalidValueUsesZero(t *testing.T) {
	var document struct {
		ID StID `xml:"ID,attr"`
	}
	if err := xml.Unmarshal([]byte(`<Document ID="invalid"/>`), &document); err != nil {
		t.Fatal(err)
	}
	if document.ID != 0 {
		t.Fatalf("StID = %d, want 0", document.ID)
	}
}

func TestStRefIDUnmarshalXMLAttrInvalidValueUsesZero(t *testing.T) {
	var document struct {
		RefID StRefID `xml:"RefID,attr"`
	}
	if err := xml.Unmarshal([]byte(`<Document RefID="invalid"/>`), &document); err != nil {
		t.Fatal(err)
	}
	if document.RefID != 0 {
		t.Fatalf("StRefID = %d, want 0", document.RefID)
	}
}

func TestCTMUnmarshalXMLAttrRejectsNonFiniteValues(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-Inf"} {
		t.Run(value, func(t *testing.T) {
			var document struct {
				CTM CTM `xml:"CTM,attr"`
			}
			if err := xml.Unmarshal([]byte(`<Document CTM="1 0 0 1 `+value+` 0"/>`), &document); err == nil {
				t.Fatal("expected non-finite CTM value to be rejected")
			}
		})
	}
}

func TestCTMIsFinite(t *testing.T) {
	finite := CTM{1, 0, 0, 1, 10, 20}
	if !finite.IsFinite() {
		t.Fatal("finite CTM reported as invalid")
	}
	nonFinite := CTM{1, 0, 0, 1, math.NaN(), 0}
	if nonFinite.IsFinite() {
		t.Fatal("non-finite CTM reported as valid")
	}
}

func TestStBoxRejectsNonFiniteValues(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-Inf"} {
		t.Run(value, func(t *testing.T) {
			var box StBox
			if err := box.parseFromString("0 0 " + value + " 10"); err == nil {
				t.Fatal("expected non-finite StBox value to be rejected")
			}
		})
	}
}

func TestStBoxIsFinite(t *testing.T) {
	if !(StBox{X: 1, Y: 2, Width: 3, Height: 4}).IsFinite() {
		t.Fatal("finite StBox reported as invalid")
	}
	if (StBox{X: 1, Y: 2, Width: 3, Height: math.Inf(1)}).IsFinite() {
		t.Fatal("non-finite StBox reported as valid")
	}
}

func TestStPosRejectsNonFiniteValues(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-Inf"} {
		t.Run(value, func(t *testing.T) {
			var position StPos
			if err := position.parseFromString(value + " 10"); err == nil {
				t.Fatal("expected non-finite position to be rejected")
			}
		})
	}
}

func TestCtPageAreaEnsurePhysicalBoxUsesA4(t *testing.T) {
	tests := []StBox{
		{},
		{Width: 210},
		{Height: 297},
		{Width: -1, Height: 297},
		{Width: 210, Height: -1},
	}
	for _, box := range tests {
		area := CtPageArea{PhysicalBox: box}
		area.EnsurePhysicalBox()
		if area.PhysicalBox != (StBox{Width: 210, Height: 297}) {
			t.Fatalf("PhysicalBox = %+v, want A4", area.PhysicalBox)
		}
	}
}

func TestPageContentEnsurePhysicalBoxUsesA4(t *testing.T) {
	page := PageContent{}
	page.EnsurePhysicalBox()

	if page.Area == nil {
		t.Fatal("EnsurePhysicalBox() did not initialize Area")
	}
	if page.Area.PhysicalBox != (StBox{Width: 210, Height: 297}) {
		t.Fatalf("PhysicalBox = %+v, want A4", page.Area.PhysicalBox)
	}
}

func TestPageContentEnsurePhysicalBoxPreservesValidBox(t *testing.T) {
	want := StBox{X: 10, Y: 20, Width: 210, Height: 297}
	page := PageContent{Area: &CtPageArea{PhysicalBox: want}}

	page.EnsurePhysicalBox()
	if page.Area.PhysicalBox != want {
		t.Fatalf("PhysicalBox = %+v, want %+v", page.Area.PhysicalBox, want)
	}
}

func TestColorParse(t *testing.T) {
	tests := []struct {
		value    string
		wantRGBA color.RGBA
		wantErr  bool
	}{
		{value: "0 0 0 65535", wantRGBA: color.RGBA{A: 255}},
		{value: "65535 0 0 65535", wantRGBA: color.RGBA{R: 255, A: 255}},
		{value: "257 0 0 65535", wantRGBA: color.RGBA{R: 1, A: 255}},
		{value: "0 0 0 32768", wantRGBA: color.RGBA{A: 128}},
		{value: "156 82 35", wantRGBA: color.RGBA{R: 156, G: 82, B: 35, A: 255}},
		{value: "0 0 0 255", wantRGBA: color.RGBA{A: 255}},
		// 超出 16 位表示范围的值在任何合法 BPC 下都越界（表 25 的 BPC 上限是
		// 16），按表 27「取值超出了相应的区间，则按照默认颜色来处理」取 0，
		// 不再报错——报错会让异常冒泡到整页解析失败。
		{value: "70000 0 0 255", wantRGBA: color.RGBA{A: 255}},
		{value: "0 0 0", wantRGBA: color.RGBA{A: 255}},
	}
	for _, tc := range tests {
		var c Color
		err := c.parse(tc.value)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("parse(%q) 应报错", tc.value)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parse(%q) 失败: %v", tc.value, err)
		}
		if c.RGBA != tc.wantRGBA {
			t.Fatalf("parse(%q) = %+v, want %+v", tc.value, c.RGBA, tc.wantRGBA)
		}
	}
}

// TestColorParseToleratesChannelCountMismatch 分量个数由颜色空间决定，而 Color
// 解析属性时拿不到颜色空间。个数不匹配属未定义行为，实测有文档写出 4 分量 RGB、
// 2 分量 GRAY、5 分量 CMYK；此处若报错，异常会冒泡到整页解析失败，一个坏颜色就
// 能让整份文档无法转换，因此必须照原样保留。
func TestColorParseToleratesChannelCountMismatch(t *testing.T) {
	cases := []struct {
		value   string
		wantN   int
		wantRGB color.RGBA
	}{
		// RGB 空间给了 4 个分量（P1-16）
		{"255 0 0 128", 4, color.RGBA{R: 255, A: 128}},
		// GRAY 空间给了 2 个分量（P2-18）
		{"128 128", 2, color.RGBA{R: 128, G: 128, A: 255}},
		// RGB 缺分量（P8-07）
		{"255 0", 2, color.RGBA{R: 255, A: 255}},
		// CMYK 缺分量（P8-08）
		{"255 0 0", 3, color.RGBA{R: 255, A: 255}},
		// CMYK 多出第 5 个分量（P8-09）：必须忽略多余分量，且不能越界 panic
		{"255 0 0 0 0", 5, color.RGBA{R: 255, G: 0, B: 0, A: 0}},
		// 分量数远超颜色空间需求
		{"1 2 3 4 5 6 7 8 9 10", 10, color.RGBA{R: 1, G: 2, B: 3, A: 4}},
	}
	for _, tc := range cases {
		var c Color
		if err := c.parse(tc.value); err != nil {
			t.Fatalf("parse(%q) 不应报错: %v", tc.value, err)
		}
		if _, count, ok := c.Components(); !ok || count != tc.wantN {
			t.Errorf("parse(%q) 分量数 = %d（ok=%v），期望 %d", tc.value, count, ok, tc.wantN)
		}
		if c.RGBA != tc.wantRGB {
			t.Errorf("parse(%q) = %+v，期望 %+v", tc.value, c.RGBA, tc.wantRGB)
		}
	}
}

// TestColorParseToleratesOutOfRangeAndNegative 表 27 规定「当颜色通道的取值超出了
// 相应的区间，则按照默认颜色来处理」。越界与负值都不是解析错误：报错会让整页解析
// 失败，一个坏颜色就能让整份文档无法转换。
func TestColorParseToleratesOutOfRangeAndNegative(t *testing.T) {
	cases := []struct {
		value string
		want  color.RGBA
	}{
		// BPC=16 上限是 65535，65536 越界（P8-05）
		{"65536 0 0 65535", color.RGBA{A: 255}},
		// 任何合法 BPC 都装不下，按默认颜色（P8-06 附近的极端值）
		{"70000 0 0 255", color.RGBA{A: 255}},
		// 负值分量（P8-10）
		{"-10 0 0 255", color.RGBA{A: 255}},
	}
	for _, tc := range cases {
		var c Color
		if err := c.parse(tc.value); err != nil {
			t.Fatalf("parse(%q) 不应报错: %v", tc.value, err)
		}
		if c.RGBA != tc.want {
			t.Errorf("parse(%q) = %+v，期望 %+v", tc.value, c.RGBA, tc.want)
		}
	}
}

// TestColorParseStillRejectsNonNumeric 无法解释成整数才算解析失败。
func TestColorParseStillRejectsNonNumeric(t *testing.T) {
	for _, value := range []string{"abc 0 0 0", "#GG 0 0 0", "1.5 0 0 0", "99999999999999999999 0 0 0"} {
		var c Color
		if err := c.parse(value); err == nil {
			t.Errorf("parse(%q) 应报错", value)
		}
	}
}
