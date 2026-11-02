package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPDFLossyImagesFlagDefaultsAndLegacySyntax(t *testing.T) {
	defaults, err := parseArgs([]string{"input.ofd", "output.pdf"})
	if err != nil || defaults.pdfLossyImages {
		t.Fatalf("默认不应新增有损压缩：%+v，%v", defaults, err)
	}
	for _, flag := range []string{"--pdf-lossy-images", "-pdf-lossy-images"} {
		opts, err := parseArgs([]string{flag, "input.ofd", "output.pdf"})
		if err != nil || !opts.pdfLossyImages {
			t.Fatalf("图片有损参数解析失败：%+v，%v", opts, err)
		}
	}
}

func TestPDFLossyImagesFlagReachesSingleAndBatchOutput(t *testing.T) {
	const fixture = "../../testdata/ofd-creator/image-effects.ofd"
	input, err := os.ReadFile(fixture)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skipf("缺少样例 %s", fixture)
		}
		t.Fatal(err)
	}
	for _, batch := range []bool{false, true} {
		for _, lossy := range []bool{false, true} {
			t.Run(fmt.Sprintf("batch=%t/lossy=%t", batch, lossy), func(t *testing.T) {
				root := t.TempDir()
				inputPath := filepath.Join(root, "input.ofd")
				if err := os.WriteFile(inputPath, input, 0o600); err != nil {
					t.Fatal(err)
				}
				output := filepath.Join(root, "output.pdf")
				args := []string{"--format=pdf", "--page=1", fmt.Sprintf("--pdf-lossy-images=%t", lossy)}
				if batch {
					dir := filepath.Join(root, "out")
					args = append(args, "--input-dir", root, "--output-dir", dir)
					output = filepath.Join(dir, "input.pdf")
				} else {
					args = append(args, inputPath, output)
				}
				opts, err := parseArgs(args)
				if err != nil {
					t.Fatal(err)
				}
				if err := run(opts); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(output)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(data, []byte("/DCTDecode")) != lossy {
					t.Fatal("图片有损开关未传递到PDF编码器")
				}
			})
		}
	}
}
