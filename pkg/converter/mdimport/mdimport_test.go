package mdimport_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/converter"
	_ "github.com/zc310/ofd/pkg/converter/mdimport"
	"github.com/zc310/ofd/pkg/validator"
)

const sampleMarkdown = `# 标题一

这是一个包含 **粗体**、*斜体* 和 ` + "`行内代码`" + ` 的段落，
还有中文换行与 [链接](https://example.com)。

## 列表

- 第一项
- 第二项
  - 嵌套项

1. 有序一
2. 有序二

> 引用文本

` + "```go" + `
fmt.Println("hello")
` + "```" + `

| 名称 | 数量 |
| --- | ---: |
| 苹果 | 3 |
| 香蕉 | 12 |

---

结束段落。
`

func TestMarkdownImporterRegistered(t *testing.T) {
	for _, name := range []string{"md", "markdown", ".md"} {
		if _, ok := converter.ImporterByName(name); !ok {
			t.Fatalf("未注册 Markdown 导入器: %s", name)
		}
	}
	if imp, ok := converter.ImporterByExtension(".md"); !ok || imp.Name() != "markdown" {
		t.Fatalf("按扩展名查找 Markdown 导入器失败: %v %v", imp, ok)
	}
}

func TestConvertMarkdownToOFD(t *testing.T) {
	var output bytes.Buffer
	if err := converter.Convert("md", "ofd", []byte(sampleMarkdown), &output); err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	assertValidOFD(t, output.Bytes())
}

func TestConvertMarkdownSkipsRemoteImages(t *testing.T) {
	source := "# 远程图片\n\n![remote](https://example.com/a.png)\n\n结束。\n"
	var output bytes.Buffer
	if err := converter.Convert("markdown", "ofd", []byte(source), &output); err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	assertValidOFD(t, output.Bytes())
}

func TestConvertMarkdownLocalImage(t *testing.T) {
	dir := t.TempDir()
	writeTestPNG(t, filepath.Join(dir, "img.png"))
	mdPath := filepath.Join(dir, "doc.md")
	source := "# 本地图片\n\n![图片](img.png)\n"
	if err := os.WriteFile(mdPath, []byte(source), 0o600); err != nil {
		t.Fatalf("写入 Markdown 失败: %v", err)
	}
	var output bytes.Buffer
	if err := converter.Convert("md", "ofd", mdPath, &output); err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte("PNG")) {
		t.Fatalf("输出未包含图片资源")
	}
	assertValidOFD(t, output.Bytes())
}

func TestConvertMarkdownFromReader(t *testing.T) {
	var output bytes.Buffer
	if err := converter.Convert("markdown", "ofd", strings.NewReader("# reader\n\n正文\n"), &output); err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	assertValidOFD(t, output.Bytes())
}

func assertValidOFD(t *testing.T, data []byte) {
	t.Helper()
	if !bytes.HasPrefix(data, []byte("PK")) {
		t.Fatalf("输出不是 ZIP/OFD 数据")
	}
	if !bytes.Contains(data, []byte("OFD.xml")) {
		t.Fatalf("输出缺少 OFD.xml")
	}
	validatorInstance, err := validator.New()
	if err != nil {
		t.Fatalf("创建校验器失败: %v", err)
	}
	report := validatorInstance.ValidateReader(context.Background(), bytes.NewReader(data), "generated.ofd")
	if report.Summary.Errors != 0 {
		t.Fatalf("OFD 校验存在错误: %d", report.Summary.Errors)
	}
}

func writeTestPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 0x33, G: 0x66, B: 0x99, A: 0xff})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建图片失败: %v", err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("编码 PNG 失败: %v", err)
	}
}
