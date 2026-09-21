package coloricc

import (
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
