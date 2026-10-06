package render

import (
	"encoding/xml"
	"image/color"
	"testing"

	"github.com/zc310/ofd/internal/models"
)

// colorValue 构造一个已按 ST_Array 原文解析出通道取值的颜色。
func colorValue(t *testing.T, value string) *models.Color {
	t.Helper()
	parsed := new(models.Color)
	if err := parsed.UnmarshalXMLAttr(xml.Attr{Value: value}); err != nil {
		t.Fatalf("解析颜色值 %q 失败: %v", value, err)
	}
	return parsed
}

func graySpace(bits int) *models.ColorSpace {
	return &models.ColorSpace{Type: "GRAY", BitsPerComponent: bits}
}

func rgbSpace(bits int) *models.ColorSpace {
	return &models.ColorSpace{Type: "RGB", BitsPerComponent: bits}
}

func cmykSpace(bits int) *models.ColorSpace {
	return &models.ColorSpace{Type: "CMYK", BitsPerComponent: bits}
}

// TestScaleComponentNormalizesByBitsPerComponent 守住 GB/T 33190 表 27 的取值
// 区间：BitsPerComponent 有效时通道取值范围是 [0, 2^BPC-1]，必须归一化到 0-255，
// 越界按默认颜色（全 0）处理而不是截断到上限。
func TestScaleComponentNormalizesByBitsPerComponent(t *testing.T) {
	cases := []struct {
		bits  int
		value int
		want  uint8
	}{
		{1, 0, 0}, {1, 1, 255},
		{2, 0, 0}, {2, 1, 85}, {2, 2, 170}, {2, 3, 255},
		{4, 0, 0}, {4, 8, 136}, {4, 15, 255},
		{8, 0, 0}, {8, 128, 128}, {8, 255, 255},
		{16, 0, 0}, {16, 32768, 128}, {16, 65535, 255},
		// 越界按默认颜色处理：截断到上限会把本该作废的颜色画成满墨。
		{1, 2, 0}, {4, 16, 0}, {8, 256, 0}, {16, 65536, 0},
		// 非法位数按缺省 8 处理，不能因 1<<bits 负移位而 panic。
		{0, 255, 255}, {-1, 255, 255}, {7, 255, 255},
	}
	for _, tc := range cases {
		if got := scaleComponent(tc.value, tc.bits); got != tc.want {
			t.Errorf("scaleComponent(%d, BPC=%d) = %d，期望 %d", tc.value, tc.bits, got, tc.want)
		}
	}
}

// TestSpaceBitsPerComponentDefaultsToEight 未声明或声明非法 BitsPerComponent 时按
// 缺省值 8 处理（GB/T 33190 表 25：有效取值为 1、2、4、8、16，缺省值为 8）。
func TestSpaceBitsPerComponentDefaultsToEight(t *testing.T) {
	for _, bits := range []int{0, 3, 5, 7, 12, 32, -1} {
		if got := spaceBitsPerComponent(&models.ColorSpace{BitsPerComponent: bits}); got != 8 {
			t.Errorf("BitsPerComponent=%d 时取 %d，期望缺省 8", bits, got)
		}
	}
	for _, bits := range []int{1, 2, 4, 8, 16} {
		if got := spaceBitsPerComponent(&models.ColorSpace{BitsPerComponent: bits}); got != bits {
			t.Errorf("BitsPerComponent=%d 时取 %d，期望原值", bits, got)
		}
	}
}

// TestColorSpaceComponentsNormalizesGrayAndRGB 同一个「满值」在不同
// BitsPerComponent 下必须归一化到同一个 0-255 分量，否则同一颜色在不同 BPC 的
// 颜色空间下深浅不一。
func TestColorSpaceComponentsNormalizesGrayAndRGB(t *testing.T) {
	full := map[int]string{1: "1", 2: "3", 4: "15", 8: "255", 16: "65535"}
	for bits, value := range full {
		space := rgbSpace(bits)
		components, ok := colorSpaceComponents8(models.CTColor{Value: colorValue(t, value+" 0 0")}, space)
		if !ok {
			t.Fatalf("BPC=%d 取分量失败", bits)
		}
		if components[0] != 255 {
			t.Errorf("BPC=%d 满值红分量 = %d，期望 255", bits, components[0])
		}
	}
	// GRAY 单通道同理。
	for bits, value := range full {
		components, ok := colorSpaceComponents8(models.CTColor{Value: colorValue(t, value)}, graySpace(bits))
		if !ok {
			t.Fatalf("GRAY BPC=%d 取分量失败", bits)
		}
		if components[0] != 255 {
			t.Errorf("GRAY BPC=%d 满值分量 = %d，期望 255", bits, components[0])
		}
	}
}

// TestResolveColorSpaceColorCMYKChannelOrder 守住 GB/T 33190 表 27 规定的 OFD
// CMYK 通道顺序：依次是青、黄、品红、黑。顺序错成青、品红、黄、黑（PDF 的
// DeviceCMYK 顺序）时黄会画成品红、品红会画成黄。
func TestResolveColorSpaceColorCMYKChannelOrder(t *testing.T) {
	space := cmykSpace(8)
	cases := []struct {
		value string
		want  color.RGBA
		note  string
	}{
		{"255 0 0 0", color.RGBA{0, 173, 239, 255}, "通道 1 满值应为青"},
		{"0 255 0 0", color.RGBA{255, 242, 0, 255}, "通道 2 满值应为黄"},
		{"0 0 255 0", color.RGBA{236, 0, 140, 255}, "通道 3 满值应为品红"},
		{"0 0 0 255", color.RGBA{35, 31, 32, 255}, "通道 4 满值应为黑"},
		{"255 255 0 0", color.RGBA{0, 166, 80, 255}, "青+黄 叠印为绿"},
		{"255 0 255 0", color.RGBA{46, 49, 146, 255}, "青+品红 叠印为蓝"},
		{"0 255 255 0", color.RGBA{237, 28, 36, 255}, "黄+品红 叠印为红"},
		{"0 0 0 0", color.RGBA{255, 255, 255, 255}, "全 0 无油墨为纯白"},
	}
	for _, tc := range cases {
		got := resolveColorSpaceColor(space, models.CTColor{Value: colorValue(t, tc.value)})
		if got != tc.want {
			t.Errorf("CMYK %q → %v，期望 %v（%s）", tc.value, got, tc.want, tc.note)
		}
	}
}

// TestResolveColorSpaceColorCMYKAcrossBitsPerComponent 同一个满值青用
// BPC=1/2/4/8/16 表达时必须渲染成同一个青。
func TestResolveColorSpaceColorCMYKAcrossBitsPerComponent(t *testing.T) {
	full := map[int]string{1: "1", 2: "3", 4: "15", 8: "255", 16: "65535"}
	var reference color.RGBA
	for _, bits := range []int{1, 2, 4, 8, 16} {
		got := resolveColorSpaceColor(cmykSpace(bits), models.CTColor{Value: colorValue(t, full[bits]+" 0 0 0")})
		if bits == 1 {
			reference = got
			continue
		}
		if got != reference {
			t.Errorf("BPC=%d 满值青 = %v，与 BPC=1 的 %v 不一致", bits, got, reference)
		}
	}
	// 越界通道按默认颜色（全 0）处理，不截断到上限。
	outOfRange := resolveColorSpaceColor(cmykSpace(1), models.CTColor{Value: colorValue(t, "2 0 0 0")})
	zero := resolveColorSpaceColor(cmykSpace(1), models.CTColor{Value: colorValue(t, "0 0 0 0")})
	if outOfRange != zero {
		t.Errorf("BPC=1 越界值 2 → %v，期望按默认颜色处理得到 %v", outOfRange, zero)
	}
}

// TestTransformerOrderComponentsSwapsYellowAndMagenta coloricc 与 ICC 转换器都按
// PDF 的青、品红、黄、黑解释四分量，两条渲染路径都必须换序，否则同一份 OFD 在
// 有无 CMYK ICC 配置的机器上画出不同颜色。
func TestTransformerOrderComponentsSwapsYellowAndMagenta(t *testing.T) {
	// OFD 顺序的青、黄、品红、黑。
	ofdOrder := []uint8{10, 20, 30, 40}
	got := transformerOrderComponents(cmykSpace(8), ofdOrder)
	want := []uint8{10, 30, 20, 40} // PDF 顺序：青、品红、黄、黑
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("换序结果 = %v，期望 %v", got, want)
		}
	}
	// GRAY 与 RGB 不换序。
	rgb := []uint8{1, 2, 3}
	if out := transformerOrderComponents(rgbSpace(8), rgb); out[0] != 1 || out[1] != 2 || out[2] != 3 {
		t.Errorf("RGB 分量不应换序，得到 %v", out)
	}
	gray := []uint8{9}
	if out := transformerOrderComponents(graySpace(8), gray); len(out) != 1 || out[0] != 9 {
		t.Errorf("GRAY 分量不应换序，得到 %v", out)
	}
}

// TestParseChannelValueAcceptsHexNotation GB/T 33190 表 27 允许用 "#" 前缀的
// 十六进制表示通道取值，例如 "#11 #22 #33 #44"。
func TestParseChannelValueAcceptsHexNotation(t *testing.T) {
	cases := []struct {
		text string
		want int
		ok   bool
	}{
		{"255", 255, true},
		{"0", 0, true},
		{" 128 ", 128, true},
		{"#FF", 255, true},
		{"#ff", 255, true},
		{"#00", 0, true},
		{"#8000", 32768, true},
		{"#11", 17, true},
		{"#FFFF", 65535, true},
		{"#GG", 0, false},
		{"", 0, false},
		{"abc", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseChannelValue(tc.text)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("parseChannelValue(%q) = (%d, %v)，期望 (%d, %v)", tc.text, got, ok, tc.want, tc.ok)
		}
	}
}

// TestColorSpaceComponents8ReadsPaletteWithHex 调色板 CV 与 Value 用同一套通道
// 记法（表 27），并且同样要按 BitsPerComponent 归一化。
func TestColorSpaceComponents8ReadsPaletteWithHex(t *testing.T) {
	space := &models.ColorSpace{
		Type:             "RGB",
		BitsPerComponent: 4,
		Palette:          &models.Palette{CV: []models.StArray{{"#F", "#0", "#0"}, {"0", "#F", "#0"}}},
	}
	red, ok := colorSpaceComponents8(models.CTColor{Index: 0}, space)
	if !ok {
		t.Fatal("按 Index 取调色板分量失败")
	}
	if red[0] != 255 || red[1] != 0 || red[2] != 0 {
		t.Errorf("调色板 0 = %v，期望满值红", red)
	}
	green, ok := colorSpaceComponents8(models.CTColor{Index: 1}, space)
	if !ok {
		t.Fatal("按 Index 取调色板分量失败")
	}
	if green[0] != 0 || green[1] != 255 || green[2] != 0 {
		t.Errorf("调色板 1 = %v，期望满值绿", green)
	}
	// Index 越界时按默认颜色处理：取不到分量，交由调用方回退。
	if _, ok := colorSpaceComponents8(models.CTColor{Index: 99}, space); ok {
		t.Error("越界 Index 不应取到分量")
	}
}

// TestColorComponentsPreservesRawChannels 守住原文留存：Color 必须保留按
// ST_Array 解析出的通道取值，否则渲染端拿到 BitsPerComponent 也无法把 BPC=4 的
// 15 与 BPC=8 的 15 区分开。
func TestColorComponentsPreservesRawChannels(t *testing.T) {
	values, count, ok := colorValue(t, "15 0 0 0").Components()
	if !ok {
		t.Fatal("应保留原文通道")
	}
	if count != 4 || values[0] != 15 {
		t.Errorf("原文通道 = %v（%d 个），期望 [15 0 0 0]（4 个）", values, count)
	}
	// 没有原文（代码直接构造）时 ok 为 false，调用方回退到 RGBA。
	direct := models.Color{RGBA: color.RGBA{R: 1, G: 2, B: 3, A: 4}}
	if _, _, ok := direct.Components(); ok {
		t.Error("直接构造的 Color 不应报告原文通道")
	}
}
