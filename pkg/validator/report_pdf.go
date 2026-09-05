package validator

import (
	"fmt"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/tdewolff/canvas"
	pdfrenderer "github.com/tdewolff/canvas/renderers/pdf"
	fontpkg "github.com/tdewolff/font"
)

// PDFOptions 配置 PDF 报告的字体。
type PDFOptions struct {
	// Font 指定报告使用的字体文件；未指定时自动查找支持中文的系统字体。
	Font string
}

type pdfBlock struct {
	height       float64
	keepWithNext bool
	render       func(*canvas.Context, float64, float64, float64)
}

const pdfTextYOffset = 3.5

func drawPDFText(ctx *canvas.Context, x, y float64, face *canvas.FontFace, text string, align canvas.TextAlign) {
	ctx.DrawText(x, y+pdfTextYOffset, canvas.NewTextLine(face, text, align))
}

func drawPDFRect(ctx *canvas.Context, x, y, width, height float64, fill, stroke color.Color, strokeWidth float64) {
	// CartesianIV uses a top-down user coordinate system, so positive height
	// extends the rectangle toward the bottom of the page.
	ctx.SetFillColor(fill)
	ctx.SetStrokeColor(stroke)
	ctx.SetStrokeWidth(strokeWidth)
	ctx.DrawPath(x, y, canvas.Rectangle(width, height))
}

func drawPDFRule(ctx *canvas.Context, x, y, width float64, stroke color.Color, strokeWidth float64) {
	ctx.SetFillColor(canvas.Transparent)
	ctx.SetStrokeColor(stroke)
	ctx.SetStrokeWidth(strokeWidth)
	ctx.DrawPath(x, y, canvas.Line(width, 0))
}

func pdfStatusColor(status Status) color.Color {
	switch status {
	case StatusValid:
		return color.RGBA{R: 38, G: 125, B: 85, A: 255}
	case StatusPartial:
		return color.RGBA{R: 186, G: 113, B: 25, A: 255}
	case StatusInvalid:
		return color.RGBA{R: 184, G: 59, B: 58, A: 255}
	default:
		return color.RGBA{R: 127, G: 49, B: 72, A: 255}
	}
}

func pdfCheckColor(status string) color.Color {
	switch status {
	case "passed":
		return color.RGBA{R: 38, G: 125, B: 85, A: 255}
	case "warning":
		return color.RGBA{R: 186, G: 113, B: 25, A: 255}
	case "failed":
		return color.RGBA{R: 184, G: 59, B: 58, A: 255}
	default:
		return color.RGBA{R: 112, G: 125, B: 139, A: 255}
	}
}

func pdfSeverityColor(severity Severity) color.Color {
	switch severity {
	case SeverityError:
		return color.RGBA{R: 184, G: 59, B: 58, A: 255}
	case SeverityWarning:
		return color.RGBA{R: 186, G: 113, B: 25, A: 255}
	default:
		return color.RGBA{R: 47, G: 104, B: 157, A: 255}
	}
}

func pdfSeverityFill(severity Severity) color.Color {
	switch severity {
	case SeverityError:
		return color.RGBA{R: 253, G: 243, B: 243, A: 255}
	case SeverityWarning:
		return color.RGBA{R: 255, G: 248, B: 237, A: 255}
	default:
		return color.RGBA{R: 243, G: 248, B: 253, A: 255}
	}
}

// RenderPDF 将校验报告渲染为可搜索文本的 PDF 文档。
func RenderPDF(w io.Writer, report Report, options PDFOptions) error {
	if w == nil {
		return fmt.Errorf("PDF 输出写入器为空")
	}
	family, err := loadReportFont(options.Font)
	if err != nil {
		return err
	}
	ink := color.RGBA{R: 35, G: 48, B: 62, A: 255}
	navy := color.RGBA{R: 31, G: 78, B: 121, A: 255}
	muted := color.RGBA{R: 91, G: 104, B: 117, A: 255}
	border := color.RGBA{R: 213, G: 222, B: 231, A: 255}
	surface := color.RGBA{R: 246, G: 249, B: 252, A: 255}
	surfaceAlt := color.RGBA{R: 251, G: 253, B: 255, A: 255}
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	transparent := color.RGBA{R: 0, G: 0, B: 0, A: 0}
	face := family.Face(9, ink)
	bodyBoldFace := family.Face(9, ink, canvas.FontBold)
	smallFace := family.Face(8, muted)
	smallBoldFace := family.Face(8, muted, canvas.FontBold)
	titleFace := family.Face(20, navy, canvas.FontBold)
	sectionFace := family.Face(12, navy, canvas.FontBold)
	tableHeaderFace := family.Face(8.5, white, canvas.FontBold)
	metricLabelFace := family.Face(7.5, muted)
	metricValueFace := family.Face(12, ink, canvas.FontBold)
	footerFace := family.Face(8, muted)
	pageWidth, pageHeight := canvas.A4.W, canvas.A4.H

	const margin = 18.0
	contentWidth := pageWidth - 2*margin
	regularLineHeight := face.Metrics().LineHeight * 1.2
	smallLineHeight := smallFace.Metrics().LineHeight * 1.2
	titleLineHeight := titleFace.Metrics().LineHeight * 1.18
	sectionLineHeight := sectionFace.Metrics().LineHeight * 1.2
	footerLineHeight := footerFace.Metrics().LineHeight * 1.18
	const footerGap = 5.0
	contentBottom := pageHeight - margin - footerLineHeight - footerGap
	contentHeight := contentBottom - margin

	blocks := make([]pdfBlock, 0, len(report.Issues)+8)
	addSpacer := func(height float64) {
		blocks = append(blocks, pdfBlock{height: height, render: func(*canvas.Context, float64, float64, float64) {}})
	}

	headerLines := append(
		wrapPDFText(smallFace, "输入文件："+report.Input.Path, contentWidth-12),
		wrapPDFText(smallFace, "检测时间："+formatPDFDetectionTime(report.StartedAt), contentWidth-12)...,
	)
	headerHeight := 5.0 + titleLineHeight + 2.0 + float64(len(headerLines))*smallLineHeight + 5.0
	blocks = append(blocks, pdfBlock{
		height: headerHeight,
		render: func(ctx *canvas.Context, x, y, width float64) {
			drawPDFRect(ctx, x, y, width, headerHeight, surface, border, 0.45)
			drawPDFRect(ctx, x, y, 4, headerHeight, navy, transparent, 0)
			drawPDFText(ctx, x+11, y+5, titleFace, "OFD 校验报告", canvas.Left)
			pathY := y + 5 + titleLineHeight + 2
			for _, line := range headerLines {
				drawPDFText(ctx, x+11, pathY, smallFace, line, canvas.Left)
				pathY += smallLineHeight
			}
		},
	})
	addSpacer(7)

	statusColor := pdfStatusColor(report.Status)
	summaryHeight := 31.0
	metrics := []struct {
		label string
		value string
	}{
		{label: "错误", value: fmt.Sprintf("%d", report.Summary.Errors)},
		{label: "警告", value: fmt.Sprintf("%d", report.Summary.Warnings)},
		{label: "文件", value: fmt.Sprintf("%d", report.Summary.Files)},
		{label: "耗时", value: fmt.Sprintf("%d ms", report.DurationMS)},
	}
	statusFace := family.Face(12, statusColor, canvas.FontBold)
	blocks = append(blocks, pdfBlock{
		height: summaryHeight,
		render: func(ctx *canvas.Context, x, y, width float64) {
			drawPDFRect(ctx, x, y, width, summaryHeight, surface, border, 0.45)
			drawPDFRect(ctx, x, y, 4, summaryHeight, statusColor, transparent, 0)
			drawPDFText(ctx, x+11, y+6, statusFace, "状态："+statusLabel(report.Status), canvas.Left)
			metricsX := x + 68
			metricWidth := (width - 68) / float64(len(metrics))
			for index, metric := range metrics {
				center := metricsX + metricWidth*(float64(index)+0.5)
				drawPDFText(ctx, center, y+5, metricLabelFace, metric.label, canvas.Center)
				valueFace := metricValueFace
				if metric.label == "错误" && report.Summary.Errors > 0 {
					valueFace = family.Face(12, pdfSeverityColor(SeverityError), canvas.FontBold)
				} else if metric.label == "警告" && report.Summary.Warnings > 0 {
					valueFace = family.Face(12, pdfSeverityColor(SeverityWarning), canvas.FontBold)
				}
				drawPDFText(ctx, center, y+14, valueFace, metric.value, canvas.Center)
			}
		},
	})
	addSpacer(9)

	drawSectionHeading := func(ctx *canvas.Context, x, y, width float64, title, suffix string) {
		headingHeight := sectionLineHeight + 3
		drawPDFRect(ctx, x, y+1, 3, headingHeight-2, navy, transparent, 0)
		drawPDFText(ctx, x+8, y+2.5, sectionFace, title, canvas.Left)
		if suffix != "" {
			drawPDFText(ctx, x+width, y+2, smallBoldFace, suffix, canvas.Right)
		}
	}

	checkHeadingHeight := sectionLineHeight + 3
	tableHeaderHeight := 10.0
	checkRowHeight := 9.5
	checkTableHeight := tableHeaderHeight + checkRowHeight*float64(len(report.Checks))
	checkSectionHeight := checkHeadingHeight + 4 + checkTableHeight
	checkColumnWidth := contentWidth * 0.62
	blocks = append(blocks, pdfBlock{
		height: checkSectionHeight,
		render: func(ctx *canvas.Context, x, y, width float64) {
			drawSectionHeading(ctx, x, y, width, "检查结果", "")
			tableY := y + checkHeadingHeight + 4
			drawPDFRect(ctx, x, tableY, width, tableHeaderHeight, navy, navy, 0.4)
			drawPDFText(ctx, x+8, tableY+2.5, tableHeaderFace, "检查项", canvas.Left)
			drawPDFText(ctx, x+checkColumnWidth+8, tableY+2.5, tableHeaderFace, "状态", canvas.Left)
			for index, check := range report.Checks {
				rowY := tableY + tableHeaderHeight + checkRowHeight*float64(index)
				fill := surfaceAlt
				if index%2 == 1 {
					fill = surface
				}
				drawPDFRect(ctx, x, rowY, width, checkRowHeight, fill, transparent, 0)
				drawPDFRule(ctx, x, rowY+checkRowHeight, width, border, 0.3)
				drawPDFText(ctx, x+8, rowY+2.6, face, checkLabel(check.Name), canvas.Left)
				statusText := checkStatusLabel(check.Status)
				statusFace := family.Face(8, white, canvas.FontBold)
				pillWidth := statusFace.TextWidth(statusText) + 8
				pillX := x + width - pillWidth - 8
				drawPDFRect(ctx, pillX, rowY+2, pillWidth, checkRowHeight-4, pdfCheckColor(check.Status), transparent, 0)
				drawPDFText(ctx, pillX+pillWidth/2, rowY+2.4, statusFace, statusText, canvas.Center)
			}
			drawPDFRect(ctx, x, tableY, width, checkTableHeight, transparent, border, 0.5)
		},
	})
	addSpacer(9)

	issueHeadingHeight := sectionLineHeight + 3
	blocks = append(blocks, pdfBlock{
		height:       issueHeadingHeight,
		keepWithNext: true,
		render: func(ctx *canvas.Context, x, y, width float64) {
			drawSectionHeading(ctx, x, y, width, "问题", fmt.Sprintf("共 %d 条", len(report.Issues)))
		},
	})

	type issueLine struct {
		text       string
		face       *canvas.FontFace
		lineHeight float64
	}
	appendIssueLines := func(lines *[]issueLine, currentFace *canvas.FontFace, text string, lineHeight, width float64) {
		for _, line := range wrapPDFText(currentFace, text, width) {
			*lines = append(*lines, issueLine{text: line, face: currentFace, lineHeight: lineHeight})
		}
	}
	addIssueBlocks := func(index int, issue Issue) {
		severityColor := pdfSeverityColor(issue.Severity)
		issueLines := []issueLine{{
			text:       fmt.Sprintf("%d. %s", index+1, severityLabel(issue.Severity)),
			face:       family.Face(9, severityColor, canvas.FontBold),
			lineHeight: regularLineHeight,
		}}
		location := issue.File
		if issue.Line > 0 {
			location = fmt.Sprintf("%s:%d:%d", location, issue.Line, issue.Column)
		}
		if location != "" {
			appendIssueLines(&issueLines, smallFace, "文件："+location, smallLineHeight, contentWidth-16)
		}
		if issue.Path != "" {
			appendIssueLines(&issueLines, smallFace, "XML 路径："+issue.Path, smallLineHeight, contentWidth-16)
		}
		if issue.Message != "" {
			appendIssueLines(&issueLines, face, "信息："+issue.Message, regularLineHeight, contentWidth-16)
		}
		codeStage := ""
		if issue.Code != "" {
			codeStage = "代码=" + issue.Code
		}
		if issue.Stage != "" {
			if codeStage != "" {
				codeStage += "  "
			}
			codeStage += "阶段=" + stageLabel(issue.Stage)
		}
		if issue.EngineCode != "" {
			if codeStage != "" {
				codeStage += "  "
			}
			codeStage += "引擎代码=" + issue.EngineCode
		}
		if codeStage != "" {
			appendIssueLines(&issueLines, smallFace, codeStage, smallLineHeight, contentWidth-16)
		}
		if issue.Hint != "" {
			appendIssueLines(&issueLines, smallFace, "提示："+issue.Hint, smallLineHeight, contentWidth-16)
		}

		const cardTopPadding = 5.0
		const cardBottomPadding = 5.0
		const cardGap = 4.0
		maxCardHeight := contentHeight - cardGap
		chunks := make([][]issueLine, 0, 1)
		current := make([]issueLine, 0, len(issueLines))
		currentHeight := cardTopPadding + cardBottomPadding
		for _, line := range issueLines {
			if len(current) > 0 && currentHeight+line.lineHeight > maxCardHeight {
				chunks = append(chunks, current)
				current = nil
				currentHeight = cardTopPadding + cardBottomPadding
			}
			current = append(current, line)
			currentHeight += line.lineHeight
		}
		if len(current) > 0 {
			chunks = append(chunks, current)
		}
		for _, chunk := range chunks {
			cardHeight := cardTopPadding + cardBottomPadding
			for _, line := range chunk {
				cardHeight += line.lineHeight
			}
			blocks = append(blocks, pdfBlock{
				height: cardHeight + cardGap,
				render: func(lines []issueLine, fill, accent color.Color, height float64) func(*canvas.Context, float64, float64, float64) {
					return func(ctx *canvas.Context, x, y, width float64) {
						drawPDFRect(ctx, x, y, width, height, fill, border, 0.4)
						drawPDFRect(ctx, x, y, 3, height, accent, transparent, 0)
						lineY := y + cardTopPadding
						for _, line := range lines {
							drawPDFText(ctx, x+10, lineY, line.face, line.text, canvas.Left)
							lineY += line.lineHeight
						}
					}
				}(chunk, pdfSeverityFill(issue.Severity), severityColor, cardHeight),
			})
		}
	}
	if len(report.Issues) == 0 {
		const emptyHeight = 25.0
		blocks = append(blocks, pdfBlock{
			height: emptyHeight,
			render: func(ctx *canvas.Context, x, y, width float64) {
				drawPDFRect(ctx, x, y, width, emptyHeight, color.RGBA{R: 241, G: 250, B: 245, A: 255}, border, 0.4)
				drawPDFRect(ctx, x, y, 3, emptyHeight, pdfStatusColor(StatusValid), transparent, 0)
				drawPDFText(ctx, x+10, y+8, bodyBoldFace, "未发现问题。", canvas.Left)
			},
		})
	} else {
		for index, issue := range report.Issues {
			addIssueBlocks(index, issue)
		}
	}

	pages := make([][]pdfBlock, 1)
	usedHeight := 0.0
	for index, block := range blocks {
		requiredHeight := block.height
		if block.keepWithNext && index+1 < len(blocks) && block.height+blocks[index+1].height <= contentHeight {
			requiredHeight += blocks[index+1].height
		}
		lastPage := len(pages) - 1
		if len(pages[lastPage]) > 0 && usedHeight+requiredHeight > contentHeight {
			pages = append(pages, nil)
			lastPage++
			usedHeight = 0
		}
		pages[lastPage] = append(pages[lastPage], block)
		usedHeight += block.height
	}

	pdf := pdfrenderer.New(w, pageWidth, pageHeight, nil)
	pdf.SetInfo("OFD 校验报告", "OFD 文件包校验", "ofd-validator", "ofd-validator", "ofd-validator")
	pdf.SetLang("zh-CN")
	ctx := canvas.NewContext(pdf)
	for pageIndex, pageBlocks := range pages {
		if pageIndex > 0 {
			pdf.NewPage(pageWidth, pageHeight)
		}
		ctx.SetCoordSystem(canvas.CartesianIV)
		ctx.SetFillColor(ink)
		y := margin
		for _, block := range pageBlocks {
			block.render(ctx, margin, y, contentWidth)
			y += block.height
		}
		footerY := pageHeight - margin - footerLineHeight
		drawPDFRule(ctx, margin, footerY-footerGap/2, contentWidth, border, 0.35)
		footer := fmt.Sprintf("第 %d 页 / 共 %d 页", pageIndex+1, len(pages))
		drawPDFText(ctx, pageWidth/2, footerY, footerFace, footer, canvas.Center)
	}
	return pdf.Close()
}

func formatPDFDetectionTime(value time.Time) string {
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
	for _, paragraph := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if paragraph == "" {
			lines = append(lines, "")
			continue
		}
		runes := []rune(paragraph)
		for len(runes) > 0 {
			fit := len(runes)
			for index := 1; index <= len(runes); index++ {
				if face.TextWidth(string(runes[:index])) > width {
					fit = index - 1
					break
				}
			}
			if fit == len(runes) {
				lines = append(lines, string(runes))
				break
			}
			if fit == 0 {
				fit = 1
			}
			breakAt := fit
			for index := fit - 1; index >= 0; index-- {
				if isPDFBreakRune(runes[index]) {
					breakAt = index + 1
					break
				}
			}
			line := strings.TrimRightFunc(string(runes[:breakAt]), unicode.IsSpace)
			if line == "" {
				breakAt = fit
				line = string(runes[:breakAt])
			}
			lines = append(lines, line)
			runes = runes[breakAt:]
			for len(runes) > 0 && unicode.IsSpace(runes[0]) {
				runes = runes[1:]
			}
		}
	}
	return lines
}

func isPDFBreakRune(r rune) bool {
	switch r {
	case '/', '\\', '.', ':', '-', '_', '?', '&', '=', '#', ' ', '\t':
		return true
	default:
		return false
	}
}

func loadReportFont(explicit string) (*canvas.FontFamily, error) {
	if explicit != "" {
		family, err := loadCJKFontFile(explicit)
		if err != nil {
			return nil, fmt.Errorf("加载 PDF 字体 %q 失败：%w", explicit, err)
		}
		return family, nil
	}

	for _, name := range []string{
		"SimSun", "宋体", "NSimSun", "Microsoft YaHei", "微软雅黑",
		"Noto Sans CJK SC", "Source Han Sans SC", "WenQuanYi Micro Hei",
	} {
		family := canvas.NewFontFamily("ofd-report")
		if err := family.LoadSystemFont(name, canvas.FontRegular); err == nil && familySupportsCJK(family) {
			return family, nil
		}
	}

	for _, candidate := range preferredSystemFontFiles() {
		if family, err := loadCJKFontFile(candidate); err == nil {
			return family, nil
		}
	}
	return nil, fmt.Errorf("未找到可用于 PDF 输出的中文字体")
}

func preferredSystemFontFiles() []string {
	needles := []string{
		"simsun", "song", "yahei", "msyh", "notoSansCJKsc", "notosanscjk", "sourcehansanssc",
		"sourcehansans", "wenquanyi", "wqy", "cjk", "hanserif", "hansans",
	}
	var paths []string
	seen := make(map[string]struct{})
	fontDirs := fontpkg.DefaultFontDirs()
	addFontFiles := func(onlyPreferred bool) {
		for _, dir := range fontDirs {
			_ = filepath.WalkDir(dir, func(filename string, entry os.DirEntry, err error) error {
				if err != nil || entry.IsDir() {
					return nil
				}
				extension := strings.ToLower(filepath.Ext(filename))
				if extension != ".ttf" && extension != ".ttc" && extension != ".otf" {
					return nil
				}
				if onlyPreferred {
					base := strings.ToLower(filepath.Base(filename))
					preferred := false
					for _, needle := range needles {
						if strings.Contains(base, strings.ToLower(needle)) {
							preferred = true
							break
						}
					}
					if !preferred {
						return nil
					}
				}
				if _, ok := seen[filename]; !ok {
					seen[filename] = struct{}{}
					paths = append(paths, filename)
				}
				return nil
			})
		}
	}
	// 先尝试常见中文字体，再扫描全部字体，兼容自定义名称的字体文件。
	addFontFiles(true)
	addFontFiles(false)
	return paths
}

func loadCJKFontFile(filename string) (*canvas.FontFamily, error) {
	if info, err := os.Stat(filename); err != nil || !info.Mode().IsRegular() {
		if err == nil {
			err = fmt.Errorf("不是普通文件")
		}
		return nil, err
	}
	if family := tryFontFile(filename, false, 0); family != nil {
		return family, nil
	}
	for index := 0; index < 16; index++ {
		if family := tryFontFile(filename, true, index); family != nil {
			return family, nil
		}
	}
	return nil, fmt.Errorf("字体不包含可用的中文字符")
}

func tryFontFile(filename string, collection bool, index int) (family *canvas.FontFamily) {
	defer func() {
		if recover() != nil {
			family = nil
		}
	}()
	family = canvas.NewFontFamily("ofd-report")
	var err error
	if collection {
		err = family.LoadFontCollection(filename, index, canvas.FontRegular)
	} else {
		err = family.LoadFontFile(filename, canvas.FontRegular)
	}
	if err != nil || !familySupportsCJK(family) {
		return nil
	}
	return family
}

func familySupportsCJK(family *canvas.FontFamily) (supported bool) {
	defer func() {
		if recover() != nil {
			supported = false
		}
	}()
	if family == nil {
		return false
	}
	face := family.Face(1, canvas.Black)
	return face != nil && face.Font != nil && face.Font.GlyphIndex('中') != 0
}
