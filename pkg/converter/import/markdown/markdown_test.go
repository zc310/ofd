package markdown_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zip"

	"github.com/zc310/ofd/pkg/converter"
	_ "github.com/zc310/ofd/pkg/converter/import/markdown"
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
	if err := converter.Convert(ctxTODO, "md", "ofd", []byte(sampleMarkdown), &output); err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	assertValidOFD(t, output.Bytes())
}

func TestConvertMarkdownSkipsRemoteImages(t *testing.T) {
	source := "# 远程图片\n\n![remote](https://example.com/a.png)\n\n结束。\n"
	var output bytes.Buffer
	if err := converter.Convert(ctxTODO, "markdown", "ofd", []byte(source), &output); err != nil {
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
	if err := converter.Convert(ctxTODO, "md", "ofd", mdPath, &output); err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte("PNG")) {
		t.Fatalf("输出未包含图片资源")
	}
	assertValidOFD(t, output.Bytes())
}

func TestConvertMarkdownFromReader(t *testing.T) {
	var output bytes.Buffer
	if err := converter.Convert(ctxTODO, "markdown", "ofd", strings.NewReader("# reader\n\n正文\n"), &output); err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	assertValidOFD(t, output.Bytes())
}

// 围栏代码块的语言标记要落到 OFD 文字层；缩进代码块没有语言，不应凭空造一个。
func TestConvertMarkdownFencedCodeLanguageLabel(t *testing.T) {
	values := ofdTextValues(t, sampleMarkdown)
	found := false
	for _, value := range values {
		if value == "go" {
			found = true
		}
	}
	if !found {
		t.Fatal("围栏代码块的语言标记 go 未写入 OFD")
	}

	indented := ofdTextValues(t, "段落。\n\n    indented code\n    second line\n")
	want := map[string]bool{"段落。": true, "indented code": true, "second line": true}
	for _, value := range indented {
		if value == "" {
			continue
		}
		if !want[value] {
			t.Fatalf("缩进代码块出现了预期外的文字 %q", value)
		}
		delete(want, value)
	}
	if len(want) > 0 {
		t.Fatalf("缺少文字 %v", want)
	}
}

// ofdTextValues 转换 Markdown 并返回 OFD 页面文字层的全部文字值。
func ofdTextValues(t *testing.T, source string) []string {
	t.Helper()
	var output bytes.Buffer
	if err := converter.Convert(ctxTODO, "md", "ofd", []byte(source), &output); err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	archive, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatalf("读取 OFD 失败: %v", err)
	}
	var values []string
	for _, file := range archive.File {
		if !strings.Contains(file.Name, "/Pages/") {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatalf("打开 %s 失败: %v", file.Name, err)
		}
		content, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", file.Name, err)
		}
		for _, part := range strings.Split(string(content), "<TextCode") {
			if !strings.Contains(part, ">") {
				continue
			}
			if index := strings.Index(part, ">"); index >= 0 {
				rest := part[index+1:]
				if end := strings.Index(rest, "<"); end >= 0 {
					values = append(values, rest[:end])
				}
			}
		}
	}
	return values
}

func TestConvertMarkdownLetterheadFrontMatter(t *testing.T) {
	source := "---\nletterhead:\n  org: \"××省档案局文件\"\n  doc_no: \"×档发〔2026〕1号\"\n  signatory: \"张三\"\n  serial_no: \"000018\"\n  security: \"内部\"\n  urgency: \"特急\"\nfooter:\n  page_number: true\nsign:\n  org: \"××省档案局\"\n  date: \"2026年9月19日\"\ncolophon:\n  cc: \"省委办公厅，省政府办公厅。\"\n  issued_by: \"××省档案局办公室\"\n  issued_date: \"2026年9月19日\"\n---\n\n# 公文标题\n\n正文内容。\n"
	var output bytes.Buffer
	if err := converter.Convert(ctxTODO, "md", "ofd", []byte(source), &output); err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	assertValidOFD(t, output.Bytes())
	headFromOFD(t, output.Bytes(), "省档案局文件")
	headFromOFD(t, output.Bytes(), "张三")
	headFromOFD(t, output.Bytes(), "抄送")
	headFromOFD(t, output.Bytes(), "印发")
	// 落款署名与成文日期渲染在版记之前。
	headFromOFD(t, output.Bytes(), "2026年9月19日印发")
}

func headFromOFD(t *testing.T, data []byte, want string) {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("打开 OFD 失败: %v", err)
	}
	for _, file := range reader.File {
		if !strings.HasSuffix(file.Name, "Content.xml") {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("读取页面内容失败: %v", err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("读取页面内容失败: %v", err)
		}
		if !bytes.Contains(content, []byte(want)) {
			t.Fatalf("页面内容未包含 %q", want)
		}
		return
	}
	t.Fatalf("未找到页面内容 XML")
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
