package docx

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestWriteProducesMinimalDocument(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, Options{Title: "增值税电子普通发票", Author: "转换器"}, func(w *Writer) error {
		return w.AddParagraph(&Paragraph{
			Properties: &ParagraphProperties{Justification: StringVal("center")},
			Runs: []Run{{
				Properties: &RunProperties{
					Fonts: &RunFonts{ASCII: "Calibri", EastAsia: "宋体"},
					Bold:  BoolVal(true),
					Size:  IntVal(32),
				},
				Text: NewText("增值税电子普通发票"),
			}},
		})
	})
	if err != nil {
		t.Fatalf("写出失败: %v", err)
	}

	parts := partNames(t, buf.Bytes())
	// [Content_Types].xml 必须是第一个条目，部分阅读器依赖 OPC 惯例。
	want := []string{partContentTypes, partRootRels, partDocument, partDocRels, partStyles, partNumbering, partCoreProps}
	if len(parts) != len(want) {
		t.Fatalf("部件数量 = %d（%v），期望 %d", len(parts), parts, len(want))
	}
	for i := range want {
		if parts[i] != want[i] {
			t.Errorf("第 %d 个部件 = %s，期望 %s", i, parts[i], want[i])
		}
	}

	document := partText(t, buf.Bytes(), partDocument)
	for _, snippet := range []string{
		`xmlns:w="` + namespaceWordprocessing + `"`,
		"<w:body>",
		`<w:jc w:val="center">`,
		`w:eastAsia="宋体"`,
		`<w:sz w:val="32">`,
		`xml:space="preserve"`,
		"<w:sectPr>",
		`<w:pgSz w:w="11906" w:h="16838">`,
		`<w:pgMar w:top="1440"`,
	} {
		if !strings.Contains(document, snippet) {
			t.Errorf("word/document.xml 缺少 %s\n%s", snippet, document)
		}
	}
	if got := strings.Count(document, "<w:sectPr>"); got != 1 {
		t.Errorf("sectPr 出现 %d 次，期望 1 次", got)
	}
}

// TestFontSlotIsNotPrefixed 记录一个真实踩过的坑：w:rFonts 的 ascii/eastAsia
// 是「属性名无前缀、属性值是字体名」，不能写成 w:ascii=，否则 Word 会拒绝。
func TestFontSlotIsNotPrefixed(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, Options{}, func(w *Writer) error {
		return w.AddParagraph(&Paragraph{Runs: []Run{{
			Properties: &RunProperties{Fonts: &RunFonts{ASCII: "Times New Roman", EastAsia: "SimSun"}},
			Text:       NewText("字体"),
		}}})
	})
	if err != nil {
		t.Fatalf("写出失败: %v", err)
	}
	document := partText(t, buf.Bytes(), partDocument)
	for _, want := range []string{`w:ascii="Times New Roman"`, `w:eastAsia="SimSun"`} {
		if !strings.Contains(document, want) {
			t.Errorf("缺少 %s\n%s", want, document)
		}
	}
	if strings.Contains(document, `w:w:eastAsia`) {
		t.Error("eastAsia 被多加了 w: 前缀")
	}
}

// TestAddTableRejectsColumnMismatch 校验表格的列宽与单元格数必须一致，
// 否则 Word 会把网格和单元格错位渲染。
func TestAddTableRejectsColumnMismatch(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, Options{}, func(w *Writer) error {
		return w.AddTable(&Table{
			Grid: NewTableGrid([]int{2000, 2000}),
			Rows: []TableRow{{Cells: []TableCell{
				{Paragraphs: []Paragraph{{Runs: []Run{{Text: NewText("只有一列")}}}}},
			}}},
		})
	})
	if err == nil {
		t.Fatal("列宽与单元格数不一致时应报错")
	}
	if !strings.Contains(err.Error(), "列宽") {
		t.Errorf("错误信息应说明列宽问题，实际为 %v", err)
	}
}

// TestNumberingPartIsAlwaysWritten 校验 numbering.xml 与它的关系、内容类型声明
// 恒定存在。[Content_Types].xml 必须在 build 之前写出，无法按需增删 part，
// 所以编号部件无条件输出；这里锁住这个取舍，避免以后有人改成按需生成却漏了
// 内容类型声明（那正是本次开发中被这个顺序约束坑到的地方）。
func TestNumberingPartIsAlwaysWritten(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Options{}, func(w *Writer) error {
		return w.AddParagraph(&Paragraph{Runs: []Run{{Text: NewText("无编号")}}})
	}); err != nil {
		t.Fatalf("写出失败: %v", err)
	}
	found := false
	for _, name := range partNames(t, buf.Bytes()) {
		if name == partNumbering {
			found = true
		}
	}
	if !found {
		t.Fatalf("应恒定写出 %s，实际部件：%v", partNumbering, partNames(t, buf.Bytes()))
	}
	for _, part := range []string{partContentTypes, partDocRels} {
		if !strings.Contains(partText(t, buf.Bytes(), part), "numbering") {
			t.Errorf("%s 应声明 numbering 部件", part)
		}
	}
	// 用到 w:numPr 时应指向恒定输出的那个定义。
	var numbered bytes.Buffer
	if err := Write(&numbered, Options{}, func(w *Writer) error {
		return w.AddParagraph(&Paragraph{
			Properties: &ParagraphProperties{Numbering: &NumberingProps{Level: IntVal(0), NumID: IntVal(1)}},
			Runs:       []Run{{Text: NewText("条目")}},
		})
	}); err != nil {
		t.Fatalf("写出失败: %v", err)
	}
	if got := partText(t, numbered.Bytes(), partDocument); !strings.Contains(got, `<w:numId w:val="1">`) {
		t.Errorf("段落未引用编号定义\n%s", got)
	}
}

// TestOutputIsReproducible 校验同样的输入产出字节相同的 ZIP：
// ZIP 时间戳与样式遍历顺序都可能让输出漂移。
func TestOutputIsReproducible(t *testing.T) {
	build := func() []byte {
		var buf bytes.Buffer
		err := Write(&buf, Options{Title: "标题"}, func(w *Writer) error {
			paragraphs := []Paragraph{
				{Runs: []Run{{Text: NewText("第一段")}}},
				{Runs: []Run{{Text: NewText("第二段")}, {Text: NewText("第三段")}}},
			}
			for i := range paragraphs {
				if err := w.AddParagraph(&paragraphs[i]); err != nil {
					return err
				}
			}
			return w.AddPageBreak()
		})
		if err != nil {
			t.Fatalf("写出失败: %v", err)
		}
		return buf.Bytes()
	}
	first, second := build(), build()
	if !bytes.Equal(first, second) {
		t.Error("同样输入产出了不同的 ZIP 字节，输出不可复现")
	}
}

// TestLandscapeSwapsPageSize 校验横向模式下页宽页高被交换且写出 orient。
func TestLandscapeSwapsPageSize(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Options{Landscape: true}, func(w *Writer) error { return nil }); err != nil {
		t.Fatalf("写出失败: %v", err)
	}
	document := partText(t, buf.Bytes(), partDocument)
	if !strings.Contains(document, `w:orient="landscape"`) {
		t.Errorf("横向模式缺少 w:orient\n%s", document)
	}
	if !strings.Contains(document, `w:w="16838"`) || !strings.Contains(document, `w:h="11906"`) {
		t.Errorf("横向模式未交换页宽页高\n%s", document)
	}
}

func TestWriteRejectsNilOutput(t *testing.T) {
	if err := Write(nil, Options{}, func(*Writer) error { return nil }); err == nil {
		t.Fatal("输出为 nil 时应报错")
	}
}

func partNames(t *testing.T, data []byte) []string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("产物不是合法 ZIP: %v", err)
	}
	names := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	return names
}

func partText(t *testing.T, data []byte, name string) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("产物不是合法 ZIP: %v", err)
	}
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("打开 %s 失败: %v", name, err)
		}
		defer rc.Close()
		content, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", name, err)
		}
		return string(content)
	}
	t.Fatalf("找不到部件 %s", name)
	return ""
}
