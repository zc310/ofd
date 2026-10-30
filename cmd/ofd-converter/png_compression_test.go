package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/zc310/ofd/pkg/converter"
)

func TestPNGCompressionFlagDefaultsAndLegacySyntax(t *testing.T) {
	defaults, err := parseArgs([]string{"input.ofd"})
	if err != nil || defaults.pngCompressionLevel != converter.DefaultPNGCompressionLevel {
		t.Fatalf("默认 PNG 压缩级别错误：options=%+v err=%v", defaults, err)
	}
	for _, flag := range []string{"--png-compression-level=1", "-png-compression-level=1"} {
		opts, err := parseArgs([]string{flag, "input.ofd"})
		if err != nil || opts.pngCompressionLevel != 1 {
			t.Fatalf("PNG 压缩参数解析失败：options=%+v err=%v", opts, err)
		}
	}
}

func TestPNGCompressionFlagReachesSingleDirectoryZipAndBatchOutput(t *testing.T) {
	input, err := os.ReadFile("../../testdata/ofd-creator/helloworld.ofd")
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("缺少 ../../testdata/ofd-creator/helloworld.ofd")
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []int{1, 9} {
		var expected bytes.Buffer
		if err := converter.Convert(context.Background(), "ofd", "png", input, &expected,
			converter.Page(1), converter.DPI(36), converter.BgColor(color.White), converter.PNGCompressionLevel(level)); err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"single", "directory", "zip", "batch"} {
			t.Run(fmt.Sprintf("%s/%d", mode, level), func(t *testing.T) {
				root := t.TempDir()
				inputPath := filepath.Join(root, "input.ofd")
				if err := os.WriteFile(inputPath, input, 0600); err != nil {
					t.Fatal(err)
				}
				output := filepath.Join(root, "output")
				args := []string{"--format=png", "--dpi=36", fmt.Sprintf("--png-compression-level=%d", level)}
				pngPath := filepath.Join(output, "page-0001.png")
				switch mode {
				case "single":
					output += ".png"
					pngPath = output
					args = append(args, "--page=1", inputPath, output)
				case "directory":
					args = append(args, "--dir", inputPath, output)
				case "zip":
					output += ".zip"
					args = append(args, inputPath, output)
				case "batch":
					args = append(args, "--input-dir", root, "--output-dir", output)
					pngPath = filepath.Join(output, "input", "page-0001.png")
				}
				opts, err := parseArgs(args)
				if err != nil {
					t.Fatal(err)
				}
				if err := run(opts); err != nil {
					t.Fatal(err)
				}
				var got []byte
				if mode == "zip" {
					archive, err := zip.OpenReader(output)
					if err != nil {
						t.Fatal(err)
					}
					defer archive.Close()
					file, err := archive.Open("page-0001.png")
					if err != nil {
						t.Fatal(err)
					}
					defer file.Close()
					got, err = io.ReadAll(file)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					got, err = os.ReadFile(pngPath)
				}
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got, expected.Bytes()) {
					t.Fatal("命令行 PNG 压缩级别未传递到页面编码器")
				}
			})
		}
	}
}

func TestInvalidPNGCompressionKeepsOutputAndReturnsUsageError(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, level := range []int{-1, 10} {
			t.Run(fmt.Sprintf("batch=%t/level=%d", batch, level), func(t *testing.T) {
				root := t.TempDir()
				output := filepath.Join(root, "existing.png")
				original := []byte("保留原输出")
				if err := os.WriteFile(output, original, 0600); err != nil {
					t.Fatal(err)
				}
				opts := &options{input: "missing.ofd", output: output, format: "png", pngCompressionLevel: level}
				if batch {
					opts.inputDir, opts.outputDir = root, filepath.Join(root, "out")
				}
				if err := run(opts); !errors.Is(err, ErrUsage) {
					t.Fatalf("无效压缩级别应返回用法错误：%v", err)
				}
				got, err := os.ReadFile(output)
				if err != nil || !bytes.Equal(got, original) {
					t.Fatalf("参数错误改写了输出：%q %v", got, err)
				}
			})
		}
	}
}
