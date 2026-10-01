package coloricc

import (
	"math/rand"
	"os"
	"testing"
)

func TestNewRejectsInvalidProfile(t *testing.T) {
	if _, err := New(nil, 4); err == nil {
		t.Fatal("expected error for empty profile")
	}
	if _, err := New([]byte("not an icc profile"), 4); err == nil {
		t.Fatal("expected error for invalid profile")
	}
	if _, err := New([]byte{0, 1, 2, 3}, 2); err == nil {
		t.Fatal("expected error for unsupported channel count")
	}
}

func TestDefaultCMYKRequiresExplicitProfile(t *testing.T) {
	// 未设置 OFD_CMYK_ICC 时不启用默认 ICC，调用方应回退近似模型。
	t.Setenv("OFD_CMYK_ICC", "")
	transformer, err := DefaultCMYK()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transformer != nil {
		t.Fatal("expected no default CMYK transformer without OFD_CMYK_ICC")
	}
}

func TestDeviceCMYKToRGBMatchesPrintModel(t *testing.T) {
	// 纯色油墨的结果应与标准印刷色一致，混合色遵循 Adobe/poppler 矩阵模型，
	// 而不是逐油墨独立吸收的近似模型（青 + 黄在高覆盖率下会被压成青柠色）。
	cases := []struct {
		c, m, y, k          float64
		wantR, wantG, wantB uint8
	}{
		{0, 0, 0, 0, 255, 255, 255},
		{1, 0, 0, 0, 0, 173, 239},
		{0, 1, 0, 0, 236, 0, 140},
		{0, 0, 1, 0, 255, 242, 0},
		{0, 0, 0, 1, 35, 31, 32},
		// 青 + 黄的高覆盖率混合色：矩阵模型给出偏橄榄的绿，而旧的独立吸收模型
		// 会得到青柠色 (123,167,8)。poppler 的 8 位结果与此相差不超过 2。
		{113.0 / 255, 21.0 / 255, 243.0 / 255, 33.0 / 255, 127, 172, 40},
	}
	for _, tc := range cases {
		r, g, b := DeviceCMYKToRGB(tc.c, tc.m, tc.y, tc.k)
		got := [3]uint8{uint8(r*255 + 0.5), uint8(g*255 + 0.5), uint8(b*255 + 0.5)}
		want := [3]uint8{tc.wantR, tc.wantG, tc.wantB}
		if got != want {
			t.Fatalf("DeviceCMYKToRGB(%v,%v,%v,%v) = %v, want %v", tc.c, tc.m, tc.y, tc.k, got, want)
		}
	}
}

func TestDeviceCMYKToRGBKeepsRoundedResults(t *testing.T) {
	random := rand.New(rand.NewSource(1))
	for i := 0; i < 10000; i++ {
		components := [4]float64{
			float64(random.Intn(256)) / 255,
			float64(random.Intn(256)) / 255,
			float64(random.Intn(256)) / 255,
			float64(random.Intn(256)) / 255,
		}
		red, green, blue := DeviceCMYKToRGB(components[0], components[1], components[2], components[3])
		got := [3]float64{red, green, blue}
		want := deviceCMYKReference(components)
		for channel := range got {
			if uint8(got[channel]*255+0.5) != uint8(want[channel]*255+0.5) {
				t.Fatalf("分量 %v 的通道 %d 取整结果改变: 得到 %g，原结果 %g", components, channel, got[channel], want[channel])
			}
		}
	}
}

func TestDeviceCMYK8ToRGBMatchesFloatConversion(t *testing.T) {
	random := rand.New(rand.NewSource(2))
	for i := 0; i < 10000; i++ {
		components := [4]uint8{uint8(random.Intn(256)), uint8(random.Intn(256)), uint8(random.Intn(256)), uint8(random.Intn(256))}
		got := [3]uint8{}
		got[0], got[1], got[2] = DeviceCMYK8ToRGB(components[0], components[1], components[2], components[3])
		red, green, blue := DeviceCMYKToRGB(float64(components[0])/255, float64(components[1])/255, float64(components[2])/255, float64(components[3])/255)
		want := [3]uint8{uint8(red*255 + 0.5), uint8(green*255 + 0.5), uint8(blue*255 + 0.5)}
		if got != want {
			t.Fatalf("分量 %v 的转换结果为 %v，原浮点路径为 %v", components, got, want)
		}
	}
}

// deviceCMYKReference 保留原始计算过程，用于确认优化不改变取整后的颜色。
func deviceCMYKReference(components [4]float64) [3]float64 {
	var result [3]float64
	for mask := 0; mask < 16; mask++ {
		weight := 1.0
		for ink := range components {
			if mask&(1<<ink) != 0 {
				weight *= components[ink]
			} else {
				weight *= 1 - components[ink]
			}
		}
		for channel := range result {
			result[channel] += deviceCMYKMatrix[mask][channel] * weight
		}
	}
	return result
}

func TestCMYKTransformationWithProfile(t *testing.T) {
	path := os.Getenv("OFD_CMYK_ICC")
	if path == "" {
		t.Skip("OFD_CMYK_ICC 未设置，跳过 ICC 转换测试")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("无法读取 %s: %v", path, err)
	}
	transformer, err := New(data, 4)
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b := transformer.ToRGB([]uint8{0, 0, 0, 0}); r < 240 || g < 240 || b < 240 {
		t.Fatalf("white = (%d,%d,%d), want near white", r, g, b)
	}
	// Perceptual 渲染意图会压缩黑场，纯黑不一定是 (0,0,0)，只要求偏暗。
	if r, g, b := transformer.ToRGB([]uint8{0, 0, 0, 255}); r > 90 || g > 90 || b > 90 {
		t.Fatalf("black = (%d,%d,%d), want dark", r, g, b)
	}
}
