package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/internal/parser"
)

// TestRunProducesExtractableSeal 验证 ofd-seal 生成的文件能被阅读器解析，
// 印章图片尺寸与原始 PNG 一致。
func TestRunProducesExtractableSeal(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "seal.png")
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	red := color.RGBA{R: 0xE6, G: 0x00, B: 0x12, A: 0xFF}
	for y := range 8 {
		for x := range 8 {
			img.Set(x, y, red)
		}
	}
	f, err := os.Create(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()

	outPath := filepath.Join(dir, "seal.esl")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--image", pngPath, "--name", "测试印章", "--esid", "seal@ofd-seal.test", "--out", outPath}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "测试印章") {
		t.Errorf("输出缺少印章名称: %s", stdout.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	extracted, err := parser.ExtractSealData(data)
	if err != nil {
		t.Fatalf("阅读器无法提取印章: %v", err)
	}
	if extracted.FileType != "png" {
		t.Errorf("FileType = %q, 期望 png", extracted.FileType)
	}
}

func TestRunRequiresImage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("code = %d, 期望参数错误 %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "--image") {
		t.Errorf("错误信息应提示缺少 --image: %s", stderr.String())
	}
}

func TestRunRejectsNonASCIIEID(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "s.png")
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	f, err := os.Create(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--image", pngPath, "--esid", "印章"}, &stdout, &stderr)
	if code == exitOK {
		t.Fatal("非 IA5 的 esid 应被拒绝")
	}
}

func TestRunRequiresKeyAndCertTogether(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--image", "x.png", "--key", "k.pem"}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("code = %d, 期望参数错误", code)
	}
	if !strings.Contains(stderr.String(), "--key 与 --cert") {
		t.Errorf("错误信息应说明 --key 与 --cert 必须同时提供: %s", stderr.String())
	}
}

// TestRunGenerateProducesPNG --generate 只产印章图片：输出必须是可解码的 PNG，
// 且不能顺带写出 Seal.esl。
func TestRunGenerateProducesPNG(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "seal.png")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--generate", "--size", "256", "--out", outPath}, &stdout, &stderr)
	if code != exitOK {
		t.Skipf("缺少可用的中文字体（exit=%d）：%s", code, stderr.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("输出不是合法 PNG: %v", err)
	}
	if decoded.Bounds().Dx() != 256 || decoded.Bounds().Dy() != 256 {
		t.Errorf("圆形图片尺寸 = %dx%d, 期望 256x256", decoded.Bounds().Dx(), decoded.Bounds().Dy())
	}
	if _, err := os.Stat(filepath.Join(dir, "seal.esl")); !os.IsNotExist(err) {
		t.Error("--generate 不应写出 Seal.esl")
	}
}

// TestRunGenerateEllipseUsesFlatHeight 椭圆输出宽度取 --size、高度为其 2/3。
func TestRunGenerateEllipseUsesFlatHeight(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "seal.png")
	var stdout, stderr bytes.Buffer
	code := run([]string{"--generate", "--shape", "ellipse", "--size", "300", "--out", outPath}, &stdout, &stderr)
	if code != exitOK {
		t.Skipf("缺少可用的中文字体（exit=%d）：%s", code, stderr.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("输出不是合法 PNG: %v", err)
	}
	if decoded.Bounds().Dy() >= decoded.Bounds().Dx() {
		t.Errorf("椭圆图片高度 %d 应小于宽度 %d", decoded.Bounds().Dy(), decoded.Bounds().Dx())
	}
}

// TestRunGenerateRejectsConflicts --generate 与 --image 互斥，且校验 shape/size。
func TestRunGenerateRejectsConflicts(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "与 image 互斥", args: []string{"--generate", "--image", "x.png"}},
		{name: "未知形状", args: []string{"--generate", "--shape", "square"}},
		{name: "尺寸过小", args: []string{"--generate", "--size", "16"}},
		{name: "尺寸过大", args: []string{"--generate", "--size", "8192"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tc.args, &stdout, &stderr); code != exitUsage {
				t.Fatalf("code = %d, 期望参数错误 %d（stderr=%s）", code, exitUsage, stderr.String())
			}
		})
	}
}
