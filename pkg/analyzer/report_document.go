package analyzer

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	reporting "github.com/zc310/ofd/internal/report"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/creator/flow"
)

// DocumentOptions 配置 OFD/PDF 分析报告使用的字体。
type DocumentOptions struct {
	// Font 是中文 TTF、OTF 或 TTC 文件；为空时自动查找中文系统字体。
	Font string
}

// PDFOptions 是 DocumentOptions 的别名，原有 PDF 调用方式保持有效。
type PDFOptions = DocumentOptions

// RenderOFD 将分析报告排版为带子集字体、页码和章节大纲的标准 OFD。
func RenderOFD(writer io.Writer, report Report, options DocumentOptions) error {
	if writer == nil {
		return fmt.Errorf("OFD 输出写入器为空")
	}
	ctx := context.Background()
	doc, err := BuildReport(ctx, report, options)
	if err != nil {
		return err
	}
	return reporting.WriteOFD(ctx, writer, doc)
}

// RenderPDF 由同一 flow 报告生成 OFD，再在内存中转换为可搜索的矢量 PDF。
func RenderPDF(writer io.Writer, report Report, options PDFOptions) error {
	if writer == nil {
		return fmt.Errorf("PDF 输出写入器为空")
	}
	ctx := context.Background()
	doc, err := BuildReport(ctx, report, options)
	if err != nil {
		return err
	}
	return reporting.WritePDF(ctx, writer, doc)
}

// BuildReport 返回已分页的报告模型，供标准文档输出或预览复用。
func BuildReport(ctx context.Context, report Report, options DocumentOptions) (*creator.Document, error) {
	b, err := reporting.New(ctx, creator.Document{ID: "analysis-report", Title: "OFD 分析报告", Subject: "OFD 文档结构分析报告",
		Creator: "ofd-analyzer", CreatorVersion: ToolVersion}, options.Font)
	if err != nil {
		return nil, err
	}
	b.Heading(1, "OFD 分析报告", nil)
	b.Paragraph("输入文件："+report.Input.Path+"\n生成时间："+time.Now().Format("2006-01-02 15:04:05")+
		"\nOFD 版本："+nonEmptyLabel(report.OFD.Version)+"    文档类型："+nonEmptyLabel(report.OFD.DocType), nil)
	s := b.Document.Options.Theme.Headings[1]
	s.Color = analysisStatusColor(report.Status)
	b.Block("paragraph", 0, "分析状态："+reportStatusLabel(report.Status), s)
	b.Table([]string{"文档体", "页面", "对象", "文字字符", "字体"}, [][]flow.TableCell{{
		{Text: fmt.Sprint(report.Summary.DocumentBodies)}, {Text: fmt.Sprint(report.Summary.Pages)}, {Text: fmt.Sprint(report.Summary.Objects)},
		{Text: fmt.Sprint(report.Summary.TextCharacters)}, {Text: fmt.Sprint(report.Summary.Fonts)},
	}}, []float64{34.8, 34.8, 34.8, 34.8, 34.8})
	if report.Encryption != nil && report.Encryption.State != "unlocked" {
		b.Paragraph("文档已加密且未解锁，页面和资源等未执行阶段的统计零值不代表原文没有内容。", &reporting.Orange)
	}
	analysisSummary(b, report)
	analysisDocuments(b, report.Documents)
	analysisPages(b, report.Pages)
	analysisFonts(b, report.ResourceDetails)
	analysisAttachments(b, report.Attachments)
	analysisAnnotations(b, report.Annotations)
	analysisSignatures(b, report.Signatures)
	if report.Package.Tree != nil {
		b.Heading(2, "OFD 包目录结构", nil)
		b.Paragraph(strings.Join(packageTreeLines(*report.Package.Tree), "\n"), nil)
	}
	for _, section := range []struct {
		title string
		lines []string
		color *creator.Color
	}{{"警告", report.Warnings, &reporting.Orange}, {"错误", report.Errors, &reporting.Red}} {
		if len(section.lines) == 0 {
			continue
		}
		b.Heading(2, fmt.Sprintf("%s（%d 条）", section.title, len(section.lines)), section.color)
		for i, text := range section.lines {
			b.Paragraph(fmt.Sprintf("%d. %s", i+1, text), section.color)
		}
	}
	return b.Build()
}

func analysisSummary(b *reporting.Builder, report Report) {
	b.Heading(2, "统计汇总", nil)
	var counts [][]flow.TableCell
	for _, row := range []struct {
		name  string
		value string
	}{
		{"页面解析", fmt.Sprintf("%d / %d", report.Summary.ParsedPages, report.Summary.Pages)},
		{"图片 / 绘制参数 / 颜色空间", fmt.Sprintf("%d / %d / %d", report.Summary.Images, report.Summary.DrawParams, report.Summary.ColorSpaces)},
		{"模板 / 复合图元 / 图案", fmt.Sprintf("%d / %d / %d", report.Summary.Templates, report.Summary.Composites, report.Summary.Patterns)},
		{"附件 / 注解 / 签名", fmt.Sprintf("%d / %d / %d", report.Summary.Attachments, report.Summary.Annotations, report.Summary.Signatures)},
		{"注解外观对象", fmt.Sprint(report.Summary.AnnotationObjects)},
		{"字体声明 / 被引用 / 嵌入", fmt.Sprintf("%d / %d / %d", report.Fonts.Declared, report.Fonts.UniqueUsed, report.Fonts.Embedded)},
		{"绘制参数继承循环", fmt.Sprint(report.DrawParams.InheritanceCycles)},
		{"资源文件 / 缺失文件 / 未解析引用", fmt.Sprintf("%d / %d / %d", report.Resources.Files, report.Resources.MissingFiles, report.Resources.Unresolved)},
		{"ZIP 条目 / 目录 / 文件 / XML", fmt.Sprintf("%d / %d / %d / %d", report.Package.Entries, report.Package.Directories, report.Package.Files, report.Package.XMLFiles)},
		{"压缩后 / 解压后", formatPackageSize(report.Package.CompressedBytes) + " / " + formatPackageSize(report.Package.UncompressedBytes)},
	} {
		counts = append(counts, []flow.TableCell{{Text: row.name}, {Text: row.value}})
	}
	b.Table([]string{"统计项", "数值"}, counts, []float64{90, 84})
	b.Heading(2, "资源与引用", nil)
	problemCount := func(count int) flow.TableCell {
		color := &reporting.Ink
		if count > 0 {
			color = &reporting.Red
		}
		return flow.TableCell{Text: fmt.Sprint(count), Style: &flow.TextStyle{Color: color}}
	}
	var resources [][]flow.TableCell
	for _, r := range []struct {
		name    string
		summary ResourceSummary
	}{
		{"全部资源", report.Resources}, {"图片", report.Images}, {"字体", report.Fonts.ResourceSummary},
		{"绘制参数", report.DrawParams.ResourceSummary}, {"颜色空间", report.ColorSpaces},
		{"模板", report.Templates}, {"复合图元", report.Composites}, {"图案", report.Patterns},
	} {
		resources = append(resources, []flow.TableCell{{Text: r.name}, {Text: fmt.Sprint(r.summary.Declared)},
			{Text: fmt.Sprint(r.summary.References)}, problemCount(r.summary.Unresolved), problemCount(r.summary.MissingFiles)})
	}
	b.Table([]string{"资源类别", "定义 / 声明", "引用次数", "未解析", "缺失文件"}, resources, []float64{42, 33, 33, 33, 33})
}

func analysisDocuments(b *reporting.Builder, documents []DocumentInfo) {
	if len(documents) == 0 {
		return
	}
	b.Heading(2, "文档体", nil)
	for _, doc := range documents {
		b.Heading(3, fmt.Sprintf("[%d] %s", doc.Index, firstNonEmpty(doc.Title, doc.DocID)), nil)
		b.Paragraph(fmt.Sprintf("文档 ID：%s\n根文件：%s\n页面：已解析 %d / 声明 %d    资源文件：%d",
			doc.DocID, doc.DocRoot, doc.ParsedPages, doc.DeclaredPages, doc.ResourceFiles), nil)
		for _, field := range []struct {
			label string
			value *string
		}{{"作者", doc.Author}, {"主题", doc.Subject}, {"摘要", doc.Abstract}} {
			if field.value != nil && *field.value != "" {
				b.Paragraph(field.label+"："+*field.value, nil)
			}
		}
	}
}

func analysisPages(b *reporting.Builder, pages []PageInfo) {
	if len(pages) == 0 {
		return
	}
	b.Heading(2, "页面", nil)
	var rows [][]flow.TableCell
	for _, page := range pages {
		rows = append(rows, []flow.TableCell{{Text: fmt.Sprint(page.PageNumber)},
			{Text: fmt.Sprintf("%d / %d", page.DocumentIndex, page.DocumentPage)}, {Text: fmt.Sprint(page.ID)},
			{Text: fmt.Sprintf("%.2f × %.2f\n%s %s", page.Size.Width, page.Size.Height, page.Size.Unit, orientationLabel(page.Size.Orientation))},
			{Text: fmt.Sprint(page.Objects.Total)}, {Text: fmt.Sprint(page.Text.UnicodeCodePoints)}})
	}
	b.Table([]string{"页码", "文档 / 页", "ID", "物理尺寸", "对象", "字符"}, rows, []float64{15, 25, 24, 62, 24, 24})
}

func analysisFonts(b *reporting.Builder, resources []ResourceInfo) {
	started := false
	for _, r := range resources {
		if r.Kind != "font" {
			continue
		}
		if !started {
			b.Heading(2, "字体明细", nil)
			started = true
		}
		b.Heading(3, fmt.Sprintf("文档体 %d · 字体 ID %d", r.DocumentIndex, r.ID), nil)
		b.Paragraph("名称："+nonEmptyLabel(r.FontName)+"\n族名："+nonEmptyLabel(r.FamilyName)+"\n文件："+nonEmptyLabel(r.Path), nil)
		b.Paragraph(fmt.Sprintf("嵌入：%s    字符集：%s    样式：%s    使用：%d", yesNoLabel(r.Embedded), nonEmptyLabel(r.Charset), fontStyleLabel(r), r.Used), nil)
	}
}

func analysisAttachments(b *reporting.Builder, attachments []AttachmentInfo) {
	if len(attachments) == 0 {
		return
	}
	b.Heading(2, "附件", nil)
	for _, a := range attachments {
		b.Heading(3, a.Name, nil)
		b.Paragraph(fmt.Sprintf("文档体：%d    ID：%s\n路径：%s", a.DocumentIndex, a.ID, a.Path), nil)
		c := &reporting.Green
		if !a.Exists {
			c = &reporting.Red
		}
		b.Paragraph(fmt.Sprintf("文件存在：%s    大小：%d 字节", yesNoLabel(a.Exists), a.ActualSize), c)
	}
}

func analysisAnnotations(b *reporting.Builder, annotations []AnnotationInfo) {
	if len(annotations) == 0 {
		return
	}
	b.Heading(2, "注解", nil)
	for _, a := range annotations {
		b.Paragraph(fmt.Sprintf("文档体 %d，页面 ID %d，注解 ID %s\n类型：%s    外观：%s    对象：%d", a.DocumentIndex, a.PageID, a.ID, a.Type, yesNoLabel(a.HasAppearance), a.Objects.Total), nil)
	}
}

func analysisSignatures(b *reporting.Builder, signatures []SignatureInfo) {
	if len(signatures) == 0 {
		return
	}
	b.Heading(2, "签名", nil)
	for _, sig := range signatures {
		b.Heading(3, fmt.Sprintf("文档体 %d · 签名 %s", sig.DocumentIndex, sig.ID), nil)
		b.Paragraph(fmt.Sprintf("路径：%s\n引用：%d    盖章：%d", sig.Path, sig.ReferenceCount, sig.StampCount), nil)
		color := &reporting.Muted
		if sig.Validation != nil {
			color = signatureResultColor(sig.Validation.Status)
		}
		b.Paragraph("完整验证："+signatureValidationLabel(sig), color)
		b.Paragraph(fmt.Sprintf("摘要：%s    算法：%s    DataHash：%s\n密码学验证：%s    证书信任：%s    吊销校验：%s",
			digestCheckedLabel(sig), nonEmptyLabel(sig.DigestMethod), dataHashMatchLabel(sig.DataHash), verificationCheckedLabel(sig), trustVerificationLabel(sig), revocationVerificationLabel(sig)), nil)
		if sig.SealInfo != nil {
			b.Paragraph(fmt.Sprintf("印章：%s（%s）\n有效期：%s 至 %s\n内部签名：%s    外层签名：%s", sig.SealInfo.Name, sig.SealInfo.ID, sig.SealInfo.ValidFrom, sig.SealInfo.ValidTo, componentVerificationLabel(sig.SealVerification), componentVerificationLabel(sig.OuterVerification)), nil)
		}
		if sig.Validation != nil {
			var checks [][]flow.TableCell
			for _, check := range sig.Validation.Checks {
				checks = append(checks, []flow.TableCell{{Text: check.Code}, {Text: signatureResultLabel(check.Status), Style: &flow.TextStyle{Color: signatureResultColor(check.Status)}}})
			}
			b.Table([]string{"验证项", "状态"}, checks, []float64{120, 54})
			for _, check := range sig.Validation.Checks {
				if check.Message != "" && (check.Status == "invalid" || check.Status == "incomplete") {
					b.Paragraph(check.Code+"："+check.Message, signatureResultColor(check.Status))
				}
			}
		}
		if sig.VerificationError != "" {
			b.Paragraph("验证错误："+sig.VerificationError, &reporting.Red)
		}
	}
}

func analysisStatusColor(status Status) *creator.Color {
	switch status {
	case StatusComplete:
		return &reporting.Green
	case StatusPartial:
		return &reporting.Orange
	default:
		return &reporting.Red
	}
}

func signatureResultColor(status string) *creator.Color {
	switch status {
	case "valid":
		return &reporting.Green
	case "invalid":
		return &reporting.Red
	case "incomplete":
		return &reporting.Orange
	default:
		return &reporting.Muted
	}
}

func signatureResultLabel(status string) string {
	switch status {
	case "valid":
		return "已通过"
	case "invalid":
		return "未通过"
	case "incomplete":
		return "未完成"
	case "skipped":
		return "已跳过"
	case "not_applicable":
		return "不适用"
	default:
		return status
	}
}
