package analyzer

import (
	"fmt"
	"image/color"
	"io"
	"strings"
	"time"

	"github.com/tdewolff/canvas"
	pdfrenderer "github.com/tdewolff/canvas/renderers/pdf"
)

// PDFOptions 配置 PDF 报告生成选项。
type PDFOptions struct {
	// Font 指定报告使用的字体文件；未指定时自动查找中文字体。
	Font string
}

// RenderPDF 将分析报告渲染为可搜索文本的 PDF 文档。
func RenderPDF(writer io.Writer, report Report, options PDFOptions) error {
	if writer == nil {
		return fmt.Errorf("PDF 输出写入器为空")
	}
	family, err := loadReportFont(options.Font)
	if err != nil {
		return err
	}

	pageWidth, pageHeight := canvas.A4.W, canvas.A4.H
	const margin = 28.0
	const lineHeight = 14.0
	const titleHeight = 30.0
	contentWidth := pageWidth - 2*margin
	face := family.Face(9, color.RGBA{R: 35, G: 48, B: 62, A: 255})
	titleFace := family.Face(18, color.RGBA{R: 31, G: 78, B: 121, A: 255}, canvas.FontBold)
	sectionFace := family.Face(12, color.RGBA{R: 31, G: 78, B: 121, A: 255}, canvas.FontBold)
	smallFace := family.Face(8, color.RGBA{R: 91, G: 104, B: 117, A: 255})

	lines := pdfReportLines(report)
	pdf := pdfrenderer.New(writer, pageWidth, pageHeight, nil)
	pdf.SetInfo("OFD 分析报告", "OFD 文档结构分析报告", "ofd-analyzer", "ofd-analyzer", "ofd-analyzer")
	pdf.SetLang("zh-CN")
	ctx := canvas.NewContext(pdf)
	page := 1
	y := margin
	newPage := func() {
		pdf.NewPage(pageWidth, pageHeight)
		page++
		y = margin
	}
	drawText := func(currentFace *canvas.FontFace, text string) {
		for _, line := range wrapPDFText(currentFace, text, contentWidth) {
			if y+lineHeight > pageHeight-margin {
				newPage()
			}
			ctx.DrawText(margin, y+3.5, canvas.NewTextLine(currentFace, line, canvas.Left))
			y += lineHeight
		}
	}

	ctx.SetCoordSystem(canvas.CartesianIV)
	ctx.SetFillColor(color.RGBA{R: 246, G: 249, B: 252, A: 255})
	ctx.DrawPath(margin-8, margin-10, canvas.Rectangle(contentWidth+16, titleHeight))
	ctx.DrawText(margin, margin+6, canvas.NewTextLine(titleFace, "OFD 分析报告", canvas.Left))
	y += titleHeight
	drawText(smallFace, "输入文件："+report.Input.Path)
	drawText(smallFace, "状态："+reportStatusLabel(report.Status)+"    生成时间："+formatReportTime(time.Now()))
	y += 8

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			y += 5
			drawText(sectionFace, strings.TrimPrefix(line, "## "))
		} else {
			drawText(face, line)
		}
	}

	return pdf.Close()
}

func pdfReportLines(report Report) []string {
	lines := []string{
		"## 汇总",
		fmt.Sprintf("文档体：%d    页面：%d（已解析 %d）    对象：%d", report.Summary.DocumentBodies, report.Summary.Pages, report.Summary.ParsedPages, report.Summary.Objects),
		fmt.Sprintf("文字字符：%d    图片：%d    字体：%d    绘制参数：%d    颜色空间：%d", report.Summary.TextCharacters, report.Summary.Images, report.Summary.Fonts, report.Summary.DrawParams, report.Summary.ColorSpaces),
		fmt.Sprintf("模板：%d    复合图元：%d    图案：%d", report.Summary.Templates, report.Summary.Composites, report.Summary.Patterns),
		fmt.Sprintf("模板：定义 %d    引用 %d    无法解析 %d", report.Templates.Declared, report.Templates.References, report.Templates.Unresolved),
		fmt.Sprintf("复合图元：定义 %d    引用 %d    无法解析 %d", report.Composites.Declared, report.Composites.References, report.Composites.Unresolved),
		fmt.Sprintf("图案：定义 %d    引用 %d    无法解析 %d", report.Patterns.Declared, report.Patterns.References, report.Patterns.Unresolved),
		fmt.Sprintf("附件：%d    注解：%d    签名：%d", report.Summary.Attachments, report.Summary.Annotations, report.Summary.Signatures),
		fmt.Sprintf("资源文件：%d    缺失资源文件：%d    未解析引用：%d", report.Resources.Files, report.Resources.MissingFiles, report.Resources.Unresolved),
		fmt.Sprintf("绘制参数：定义数 %d    引用次数 %d    无法解析引用 %d    继承循环 %d", report.DrawParams.Declared, report.DrawParams.References, report.DrawParams.Unresolved, report.DrawParams.InheritanceCycles),
		fmt.Sprintf("字体：声明 %d    被引用 %d    嵌入 %d", report.Fonts.Declared, report.Fonts.UniqueUsed, report.Fonts.Embedded),
		fmt.Sprintf("OFD 包：条目 %d    目录 %d    文件 %d    XML 文件 %d    压缩后 %s    解压后 %s", report.Package.Entries, report.Package.Directories, report.Package.Files, report.Package.XMLFiles, formatPackageSize(report.Package.CompressedBytes), formatPackageSize(report.Package.UncompressedBytes)),
	}
	if report.Package.Tree != nil {
		lines = append(lines,
			"## OFD 包目录结构",
		)
		lines = append(lines, packageTreeLines(*report.Package.Tree)...)
	}
	lines = append(lines, "## 文档体")
	for _, document := range report.Documents {
		lines = append(lines, fmt.Sprintf("[%d] %s，页面 %d/%d，资源文件 %d，根文件 %s", document.Index, firstNonEmpty(document.Title, document.DocID), document.ParsedPages, document.DeclaredPages, document.ResourceFiles, document.DocRoot))
	}
	lines = append(lines, "## 字体明细")
	for _, resource := range report.ResourceDetails {
		if resource.Kind == "font" {
			lines = append(lines, fmt.Sprintf("文档体 %d，ID %d，名称 %s，族名 %s，字符集 %s，文件 %s，嵌入 %s，样式 %s，使用 %d", resource.DocumentIndex, resource.ID, nonEmptyLabel(resource.FontName), nonEmptyLabel(resource.FamilyName), nonEmptyLabel(resource.Charset), nonEmptyLabel(resource.Path), yesNoLabel(resource.Embedded), fontStyleLabel(resource), resource.Used))
		}
	}
	lines = append(lines, "## 页面")
	for _, page := range report.Pages {
		lines = append(lines, fmt.Sprintf("[%d] 文档体 %d 第 %d 页，ID %d，尺寸 %.2fx%.2f %s，对象 %d，文字 %d", page.PageNumber, page.DocumentIndex, page.DocumentPage, page.ID, page.Size.Width, page.Size.Height, orientationLabel(page.Size.Orientation), page.Objects.Total, page.Text.UnicodeCodePoints))
	}
	if len(report.Attachments) > 0 {
		lines = append(lines, "## 附件")
		for _, attachment := range report.Attachments {
			lines = append(lines, fmt.Sprintf("文档体 %d：%s (%s)，路径 %s，存在 %s，大小 %d", attachment.DocumentIndex, attachment.Name, attachment.ID, attachment.Path, yesNoLabel(attachment.Exists), attachment.ActualSize))
		}
	}
	if len(report.Annotations) > 0 {
		lines = append(lines, "## 注解", fmt.Sprintf("注解数量：%d，注解外观对象：%d", len(report.Annotations), report.Summary.AnnotationObjects))
	}
	if len(report.Signatures) > 0 {
		lines = append(lines, "## 签名")
		for _, signature := range report.Signatures {
			lines = append(lines, fmt.Sprintf("文档体 %d：%s，路径 %s，引用 %d，盖章 %d", signature.DocumentIndex, signature.ID, signature.Path, signature.ReferenceCount, signature.StampCount))
		}
	}
	if len(report.Warnings) > 0 {
		lines = append(lines, "## 警告")
		for _, warning := range report.Warnings {
			lines = append(lines, "- "+warning)
		}
	}
	if len(report.Errors) > 0 {
		lines = append(lines, "## 错误")
		for _, reportError := range report.Errors {
			lines = append(lines, "- "+reportError)
		}
	}
	return lines
}

func formatReportTime(value time.Time) string {
	if value.IsZero() {
		return "未知"
	}
	return value.Format("2006-01-02 15:04:05")
}

func wrapPDFText(face *canvas.FontFace, text string, width float64) []string {
	if text == "" {
		return []string{""}
	}
	var lines []string
	for paragraph := range strings.SplitSeq(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		runes := []rune(paragraph)
		if len(runes) == 0 {
			lines = append(lines, "")
			continue
		}
		for len(runes) > 0 {
			fit := len(runes)
			for index := 1; index <= len(runes); index++ {
				if face.TextWidth(string(runes[:index])) > width {
					fit = index - 1
					break
				}
			}
			if fit <= 0 {
				fit = 1
			}
			lines = append(lines, string(runes[:fit]))
			runes = runes[fit:]
		}
	}
	return lines
}

func loadReportFont(explicit string) (*canvas.FontFamily, error) {
	if explicit != "" {
		family := canvas.NewFontFamily("ofd-analyzer-report")
		if err := family.LoadFontFile(explicit, canvas.FontRegular); err != nil {
			return nil, fmt.Errorf("加载 PDF 字体 %q 失败：%w", explicit, err)
		}
		return family, nil
	}
	for _, name := range []string{"仿宋", "楷体", "黑体", "SimSun", "宋体", "等线", "Microsoft YaHei", "微软雅黑", "Noto Sans CJK SC", "Source Han Sans SC", "WenQuanYi Micro Hei"} {
		family := canvas.NewFontFamily("ofd-analyzer-report")
		if err := family.LoadSystemFont(name, canvas.FontRegular); err == nil {
			return family, nil
		}
	}
	return nil, fmt.Errorf("未找到可用于 PDF 输出的中文字体，请通过 --font 指定字体文件")
}
