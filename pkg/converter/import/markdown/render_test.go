package markdown_test

import (
	"bytes"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/converter"
	markdown "github.com/zc310/ofd/pkg/converter/import/markdown"
	"github.com/zc310/ofd/pkg/creator/flow"
)

// 仓库 README 包含超长 shell 命令和围栏代码，应可使用默认选项直接转换。
func TestRepositoryReadmeConvertsWithWrappedCode(t *testing.T) {
	output := filepath.Join(t.TempDir(), "readme.ofd")
	if err := markdown.ConvertFile(t.Context(), "../../../../README.md", output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	assertValidOFD(t, data)
}

// 使用实际内置字体走完整导入、光栅、PDF 和文字提取链，不依赖系统字体。
func TestMarkdownFlowOutputSupportsRasterPDFAndText(t *testing.T) {
	source := "# Report\n\nText with `a.zip`.\n\n> Quote\n\n> [!NOTE]\n> Alert body.\n\n```go\n    x := 1\n    print(x)\n```\n\n---\n"
	var ofd bytes.Buffer
	if err := markdown.Convert(t.Context(), []byte(source), &ofd, flow.DefaultOptions()); err != nil {
		t.Fatal(err)
	}
	assertValidOFD(t, ofd.Bytes())
	var text bytes.Buffer
	if err := converter.Text(t.Context(), ofd.Bytes(), &text); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Report", "a.zip", "Quote", "Note", "Alert body.", "x := 1", "print(x)", "go"} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("文字提取缺少 %q：%s", want, text.String())
		}
	}
	if strings.Contains(text.String(), "[!NOTE]") {
		t.Fatal("提示标记不应落入 OFD 文字层")
	}
	var pdf bytes.Buffer
	if err := converter.PDF(t.Context(), ofd.Bytes(), &pdf); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf.Bytes(), []byte("%PDF-")) {
		t.Fatal("PDF 输出无效")
	}
	pages := 0
	err := converter.Image(t.Context(), ofd.Bytes(), converter.PNG(), converter.DPI(72), converter.ImageWriter(func(_ int, img image.Image) error {
		if img.Bounds().Empty() {
			t.Fatal("光栅输出为空")
		}
		ink := false
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y && !ink; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				r, g, b, a := img.At(x, y).RGBA()
				if a > 0 && r+g+b < 3*0xf000 {
					ink = true
					break
				}
			}
		}
		if !ink {
			t.Fatal("光栅页没有可见内容")
		}
		pages++
		return nil
	}))
	if err != nil || pages != 1 {
		t.Fatalf("光栅输出失败: pages=%d err=%v", pages, err)
	}
}
