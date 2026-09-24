package pdf2ofd

import (
	"bytes"
	"fmt"

	"github.com/zc310/ofd/internal/models"
)

// layerTexts 返回图层文档序列表中的全部文字对象。
func layerTexts(layer *models.Layer) []models.TextObject {
	var out []models.TextObject
	for i := range layer.Items {
		if layer.Items[i].Kind == models.PageItemText {
			out = append(out, *layer.Items[i].Text)
		}
	}
	return out
}

// layerPaths 返回图层文档序列表中的全部路径对象。
func layerPaths(layer *models.Layer) []models.PathObject {
	var out []models.PathObject
	for i := range layer.Items {
		if layer.Items[i].Kind == models.PageItemPath {
			out = append(out, *layer.Items[i].Path)
		}
	}
	return out
}

// layerImages 返回图层文档序列表中的全部图像对象。
func layerImages(layer *models.Layer) []models.ImageObject {
	var out []models.ImageObject
	for i := range layer.Items {
		if layer.Items[i].Kind == models.PageItemImage {
			out = append(out, *layer.Items[i].Image)
		}
	}
	return out
}

// cjkFontPDF 返回一个带 Type0 CJK 字体（UniGB-UCS2-H、/W 按 CID 给出字宽、未嵌入）
// 的单页 PDF。页面尺寸与电子发票一致（595.2756×396.8504pt＝210×140mm），内容流
// 首行用 cm 把用户单位放大为 1mm。
func cjkFontPDF(content []byte) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595.2756 396.8504] " +
			"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /KaiTi /Encoding /UniGB-UCS2-H " +
			"/DescendantFonts [6 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /KaiTi /DW 500 /W [97 4665 1000] " +
			"/CIDToGIDMap /Identity /FontDescriptor 7 0 R >>",
		"<< /Type /FontDescriptor /FontName /KaiTi /Flags 4 >>",
	}
	var output bytes.Buffer
	output.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")
	offsets := make([]int, len(objects)+1)
	for index, object := range objects {
		offsets[index+1] = output.Len()
		fmt.Fprintf(&output, "%d 0 obj\n%s\nendobj\n", index+1, object)
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&output, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return output.Bytes()
}
