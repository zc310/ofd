package converter_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/zc310/ofd/pkg/converter"
)

// docxParts 读取产物里的全部部件，key 是条目名。
func docxParts(t *testing.T, data []byte) map[string]string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("产物不是合法 ZIP: %v", err)
	}
	parts := make(map[string]string, len(reader.File))
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("打开 %s 失败: %v", file.Name, err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatalf("读取 %s 失败: %v", file.Name, err)
		}
		rc.Close()
		parts[file.Name] = buf.String()
	}
	return parts
}

// docxParagraphTexts 解析 document.xml 里每个段落的纯文本，表格内的段落也计入。
func docxParagraphTexts(t *testing.T, document string) []string {
	t.Helper()
	type textBody struct {
		Paragraphs []struct {
			// w:t 嵌在 w:r 里，必须写成 r>t；只写 t 匹配不到，
			// 会把所有段落都解析成空字符串。
			Texts []struct {
				Value string `xml:",chardata"`
			} `xml:"r>t"`
		} `xml:"p"`
	}
	var parsed struct {
		Body textBody `xml:"body"`
	}
	if err := xml.Unmarshal([]byte(document), &parsed); err != nil {
		t.Fatalf("word/document.xml 解析失败: %v", err)
	}
	out := make([]string, 0, len(parsed.Body.Paragraphs))
	for _, paragraph := range parsed.Body.Paragraphs {
		var sb strings.Builder
		for _, run := range paragraph.Texts {
			sb.WriteString(run.Value)
		}
		out = append(out, sb.String())
	}
	return out
}

func TestDOCXProducesValidPackage(t *testing.T) {
	var output bytes.Buffer
	if err := converter.DOCX(context.Background(), "../../test/testdata/helloworld.ofd", &output); err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	parts := docxParts(t, output.Bytes())
	for _, name := range []string{
		"[Content_Types].xml",
		"_rels/.rels",
		"word/document.xml",
		"word/_rels/document.xml.rels",
		"word/styles.xml",
	} {
		if _, ok := parts[name]; !ok {
			t.Errorf("缺少部件 %s，实际有 %v", name, keysOf(parts))
		}
	}
	document := parts["word/document.xml"]
	if !strings.Contains(document, "你好呀") {
		t.Errorf("word/document.xml 应包含原文内容：\n%s", document)
	}
	if !strings.Contains(document, "<w:sectPr>") {
		t.Error("缺少 sectPr，Word 无法确定纸张大小")
	}
}

func TestDOCXPreservesFontNameAndSize(t *testing.T) {
	var output bytes.Buffer
	if err := converter.DOCX(context.Background(), "../../test/testdata/helloworld.ofd", &output); err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	document := docxParts(t, output.Bytes())["word/document.xml"]
	// 字体名必须写进 w:rFonts——本项目按「只写字体名、不嵌入字体」的策略输出，
	// 少了它打开方就只能回落到默认字体。
	if !strings.Contains(document, "<w:rFonts") {
		t.Errorf("未输出 w:rFonts：\n%s", document)
	}
	// w:sz 以半磅为单位，非零且不超过 200（约 100pt）才算合理字号。
	if !strings.Contains(document, "<w:sz w:val=") {
		t.Errorf("未输出字号 w:sz：\n%s", document)
	}
}

func TestDOCXTablesToggle(t *testing.T) {
	// intro.ofd 全篇能识别出 20 余个表格，适合做表格开关的回归样例。
	const source = "../../test/testdata/intro.ofd"
	var withTables, withoutTables bytes.Buffer
	if err := converter.DOCX(context.Background(), source, &withTables, converter.WithDOCXTables(true)); err != nil {
		t.Fatalf("开启表格转换失败: %v", err)
	}
	if err := converter.DOCX(context.Background(), source, &withoutTables, converter.WithDOCXTables(false)); err != nil {
		t.Fatalf("关闭表格转换失败: %v", err)
	}
	withPart := docxParts(t, withTables.Bytes())["word/document.xml"]
	withoutPart := docxParts(t, withoutTables.Bytes())["word/document.xml"]

	if !strings.Contains(withPart, "<w:tbl>") {
		t.Fatalf("开启表格识别后应输出 w:tbl")
	}
	if strings.Contains(withoutPart, "<w:tbl>") {
		t.Error("关闭表格识别后不应再输出 w:tbl")
	}
	if count := strings.Count(withPart, "<w:tblGrid>"); count != strings.Count(withPart, "<w:tbl>") {
		t.Errorf("每个表格都必须有 w:tblGrid：tbl=%d grid=%d",
			strings.Count(withPart, "<w:tbl>"), count)
	}
	// 两种模式下正文文字都不应丢失：表格行被拆散成普通段落后文字仍在。
	// 没有 w:t 的段落只有两类，都合法：分页符段落与内嵌图片段落。
	for _, part := range []string{withPart, withoutPart} {
		texts := docxParagraphTexts(t, part)
		if len(texts) == 0 {
			t.Fatal("不应丢掉全部正文")
		}
		empty := 0
		for _, text := range texts {
			if strings.TrimSpace(text) == "" {
				empty++
			}
		}
		allowed := strings.Count(part, `w:type="page"`) + strings.Count(part, "<w:drawing>")
		if empty != allowed {
			t.Errorf("无文字段落 %d 个，分页符与图片合计 %d 个，只有这两类允许没有文字",
				empty, allowed)
		}
	}
}

// TestDOCXEmbedsImages 校验栅格图片被内嵌成 word/media 部件并与关系表对上，
// 以及 WithDOCXImages(false) 能整体关掉内嵌。
func TestDOCXEmbedsImages(t *testing.T) {
	// image-effects.ofd 第 1 页有 3 张 PNG 图片。
	const source = "../../test/testdata/image-effects.ofd"
	var output bytes.Buffer
	if err := converter.DOCX(context.Background(), source, &output); err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	parts := docxParts(t, output.Bytes())
	document := parts["word/document.xml"]

	drawings := strings.Count(document, "<w:drawing>")
	if drawings == 0 {
		t.Fatal("未内嵌任何图片")
	}
	media := 0
	for name, content := range parts {
		if strings.HasPrefix(name, "word/media/") {
			media++
			if len(content) == 0 {
				t.Errorf("%s 是空部件", name)
			}
		}
	}
	if media != drawings {
		t.Errorf("w:drawing %d 个，媒体部件 %d 个，两者应一一对应", drawings, media)
	}
	// 根元素必须声明 DrawingML 的三个前缀，否则 XML 不是命名空间良构的，
	// 阅读器会直接拒绝加载整个文档。
	start := strings.Index(document, "<w:document")
	root := document[start : start+strings.Index(document[start:], ">")+1]
	for _, prefix := range []string{"xmlns:wp=", "xmlns:a=", "xmlns:pic="} {
		if !strings.Contains(root, prefix) {
			t.Errorf("根元素缺少 %s：%s", prefix, root)
		}
	}
	// 每张图的 r:embed 都要在关系表里能找到对应条目。
	rels := parts["word/_rels/document.xml.rels"]
	for _, embed := range docxEmbedIDs(document) {
		if !strings.Contains(rels, `Id="`+embed+`"`) {
			t.Errorf("关系表缺少 %s 对应的条目：\n%s", embed, rels)
		}
	}

	// 关掉内嵌后不应再有图片部件。
	var plain bytes.Buffer
	if err := converter.DOCX(context.Background(), source, &plain, converter.WithDOCXImages(false)); err != nil {
		t.Fatalf("关闭图片内嵌转换失败: %v", err)
	}
	for name := range docxParts(t, plain.Bytes()) {
		if strings.HasPrefix(name, "word/media/") {
			t.Errorf("关闭图片内嵌后不应出现 %s", name)
		}
	}
	if strings.Contains(docxParts(t, plain.Bytes())["word/document.xml"], "<w:drawing>") {
		t.Error("关闭图片内嵌后不应出现 w:drawing")
	}
}

// docxEmbedIDs 提取 document.xml 里全部 r:embed 的关系 ID。
func docxEmbedIDs(document string) []string {
	var out []string
	for rest := document; ; {
		index := strings.Index(rest, `r:embed="`)
		if index < 0 {
			return out
		}
		rest = rest[index+len(`r:embed="`):]
		end := strings.Index(rest, `"`)
		if end < 0 {
			return out
		}
		out = append(out, rest[:end])
		rest = rest[end+1:]
	}
}

// TestDOCXDropsAnnotationWatermarkByDefault 校验批注层的水印文字默认被剔除，
// 且能用 WithDOCXAnnotations 保留。ano.ofd 首页的「保密资料」水印由 81 个
// 批注文字对象组成，同页真实正文只有 5 个对象——过滤前后段落数差异巨大。
func TestDOCXDropsAnnotationWatermarkByDefault(t *testing.T) {
	const source = "../../test/testdata/ano.ofd"
	var filtered, kept bytes.Buffer
	if err := converter.DOCX(context.Background(), source, &filtered, converter.Page(1)); err != nil {
		t.Fatalf("默认转换失败: %v", err)
	}
	if err := converter.DOCX(context.Background(), source, &kept, converter.Page(1),
		converter.WithDOCXAnnotations(true)); err != nil {
		t.Fatalf("保留批注转换失败: %v", err)
	}
	clean := docxParts(t, filtered.Bytes())["word/document.xml"]
	raw := docxParts(t, kept.Bytes())["word/document.xml"]

	if strings.Contains(clean, "保密资料") {
		t.Error("默认输出不应包含批注层的水印文字")
	}
	if !strings.Contains(raw, "保密资料") {
		t.Error("WithDOCXAnnotations(true) 应保留批注层文字")
	}
	// 页面图层的真实正文两种模式下都必须保留。
	for _, want := range []string{"可信安全浏览器", "应用开发指南"} {
		if !strings.Contains(clean, want) {
			t.Errorf("过滤水印后丢失了正文 %q", want)
		}
	}
	// 不能用 run 数多少来断言：去掉水印条目后 textdoc.Rows 的行聚类会改变，
	// 表格识别结果随之改变，段落数可能反而变多。水印是否残留只按文本判定。
}

func TestDOCXPageSelection(t *testing.T) {
	var output bytes.Buffer
	if err := converter.DOCX(context.Background(), "../../test/testdata/helloworld.ofd", &output, converter.Page(1)); err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	// 只选一页时不应输出分页符，否则末尾会多出一个空段落。
	document := docxParts(t, output.Bytes())["word/document.xml"]
	if strings.Contains(document, `w:type="page"`) {
		t.Errorf("单页输出不应含分页符：\n%s", document)
	}
}

func TestDOCXRejectsMissingDocument(t *testing.T) {
	if err := converter.DOCX(context.Background(), []byte("not an ofd"), &bytes.Buffer{}); err == nil {
		t.Fatal("非 OFD 输入应报错")
	}
}

func keysOf(parts map[string]string) []string {
	out := make([]string, 0, len(parts))
	for name := range parts {
		out = append(out, name)
	}
	return out
}
