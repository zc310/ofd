package docx

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// 一张 1×1 的合法 PNG，用作内嵌测试的最小图片。
var testPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

func TestAddImageWiresMediaRelsAndContentTypes(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, Options{}, func(w *Writer) error {
		for i := 0; i < 2; i++ {
			if err := w.AddImage(testPNG, "png", 20, 10); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("写出失败: %v", err)
	}

	parts := map[string][]byte{}
	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("产物不是合法 ZIP: %v", err)
	}
	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("打开 %s 失败: %v", file.Name, err)
		}
		var content bytes.Buffer
		content.ReadFrom(rc)
		rc.Close()
		parts[file.Name] = content.Bytes()
	}

	for _, name := range []string{"word/media/image1.png", "word/media/image2.png"} {
		if _, ok := parts[name]; !ok {
			t.Fatalf("缺少图片部件 %s", name)
		}
		if !bytes.Equal(parts[name], testPNG) {
			t.Errorf("%s 应原样保留 PNG 字节，不该重新编码", name)
		}
	}

	document := string(parts[partDocument])
	if got := strings.Count(document, "<w:drawing>"); got != 2 {
		t.Errorf("document.xml 里 w:drawing 出现 %d 次，期望 2 次", got)
	}
	// 关系 ID 不能和 styles(rId1)/numbering(rId2) 冲突。
	rels := string(parts[partDocRels])
	for _, want := range []string{`Id="rId3"`, `Id="rId4"`} {
		if !strings.Contains(rels, want) {
			t.Errorf("关系表缺少 %s：\n%s", want, rels)
		}
	}
	// 关系目标相对 word/document.xml，不能带 word/ 前缀。
	if strings.Contains(rels, `Target="word/media`) {
		t.Errorf("关系目标应去掉 word/ 前缀：\n%s", rels)
	}
	if !strings.Contains(rels, `Target="media/image1.png"`) {
		t.Errorf("关系目标应为 media/image1.png：\n%s", rels)
	}
	// r:embed 必须与关系表里的 ID 对上。
	if !strings.Contains(document, `r:embed="rId3"`) || !strings.Contains(document, `r:embed="rId4"`) {
		t.Errorf("w:drawing 的 r:embed 与关系 ID 不匹配：\n%s", document)
	}
	// [Content_Types].xml 预声明图片扩展名，因此不受图片数量影响。
	if !strings.Contains(string(parts[partContentTypes]), `Extension="png"`) {
		t.Error("[Content_Types].xml 应预声明 png")
	}
}

func TestInlineImageDimensionsUseEMU(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Options{}, func(w *Writer) error {
		return w.AddImage(testPNG, "png", 25.4, 12.7)
	}); err != nil {
		t.Fatalf("写出失败: %v", err)
	}
	document := partText(t, buf.Bytes(), partDocument)
	// 25.4mm = 1 英寸 = 914400 EMU，12.7mm = 457200 EMU。
	for _, want := range []string{
		`cx="914400"`, `cy="457200"`,
		`<wp:extent cx="914400" cy="457200">`,
		`<a:ext cx="914400" cy="457200">`,
	} {
		if !strings.Contains(document, want) {
			t.Errorf("缺少 %s\n%s", want, document)
		}
	}
	// wp:docPr/@id 全文唯一，两张图不能都是 1。
	if strings.Count(document, `<wp:docPr id="1"`) > 1 {
		t.Error("wp:docPr/@id 重复")
	}
}

// TestDocumentDeclaresAllPrefixes 校验根元素声明了正文里用到的全部前缀。
// 少声明任何一个，XML 就不是命名空间良构的，阅读器会直接拒绝加载整个文档。
func TestDocumentDeclaresAllPrefixes(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, Options{}, func(w *Writer) error {
		if err := w.AddParagraph(&Paragraph{Runs: []Run{{Text: NewText("文字")}}}); err != nil {
			return err
		}
		return w.AddImage(testPNG, "png", 10, 10)
	})
	if err != nil {
		t.Fatalf("写出失败: %v", err)
	}
	document := partText(t, buf.Bytes(), partDocument)
	// 根元素是 <w:document ...>，取第一个 w:document 之后到它自己闭合尖括号
	// 之间的那段属性。注意不能直接找第一个 ">"，那会命中 XML 声明的 "?>"。
	start := strings.Index(document, "<w:document")
	if start < 0 {
		t.Fatalf("没有 w:document 根元素：\n%s", document)
	}
	end := strings.Index(document[start:], ">")
	root := document[start : start+end+1]
	for _, prefix := range []string{"w", "r", "wp", "a", "pic"} {
		if !strings.Contains(root, `xmlns:`+prefix+`=`) {
			t.Errorf("根元素缺少 xmlns:%s 声明：%s", prefix, root)
		}
	}
}

func TestAddImageRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name  string
		build func(*Writer) error
	}{
		{"空内容", func(w *Writer) error { return w.AddImage(nil, "png", 10, 10) }},
		{"未知格式", func(w *Writer) error { return w.AddImage(testPNG, "tif2", 10, 10) }},
		{"宽为零", func(w *Writer) error { return w.AddImage(testPNG, "png", 0, 10) }},
		{"高为负", func(w *Writer) error { return w.AddImage(testPNG, "png", 10, -1) }},
		{"尺寸为 NaN", func(w *Writer) error {
			return w.AddImage(testPNG, "png", mathNaN(), 10)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Write(&buf, Options{}, tc.build); err == nil {
				t.Error("应拒绝无效输入")
			}
		})
	}
}

func mathNaN() float64 {
	var zero float64
	return zero / zero
}
