package invoice

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"strings"

	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/creator"
)

// MergeLimits 限制合并的规模。发票文件通常只有几十 KB，默认值主要用来挡住
// 误传进来的超大文件，而不是日常会碰到的限制。
type MergeLimits struct {
	// MaxInputBytes 是单个输入的最大字节数，0 表示默认 64MB。
	MaxInputBytes int64
	// MaxTotalBytes 是所有输入的总字节上限，0 表示默认 512MB。
	MaxTotalBytes int64
	// MaxInvoices 是最多合并的发票张数，0 表示默认 1000。
	MaxInvoices int
}

const (
	defaultMergeInputBytes   = int64(64 << 20)
	defaultMergeTotalBytes   = int64(512 << 20)
	defaultMergeInvoiceCount = 1000
)

// MergeOptions 控制多张发票合并为单个 PDF 的方式。
type MergeOptions struct {
	// BatchOptions 决定抽取失败与重复发票的处理方式，与汇总共用同一套口径：
	// 合并 PDF 只会包含 ActiveItems 返回的那些票。
	BatchOptions
	// Sort 是页序排序方式，取 SortItems 的同名常量，默认按输入顺序。
	Sort string
	// Cover 为 true 时在首页插入一张汇总对账页。
	Cover bool
	// CoverTitle 是汇总页标题，为空时使用默认标题。
	CoverTitle string
	// Pages 决定每张票输出哪些页面：all（默认，全部页）、first（只取第 1 页）、
	// face（只取票面页）或页码表达式（如 "1,3-5"，按票内页码）。
	Pages string
	// Paper 是输出纸张名称（A4/A3/A5/Letter/Legal/B5/16开）。为空且 NUp 为 1 时
	// 按每页自身尺寸出纸；拼版时默认 A4。
	Paper string
	// Orientation 是纸张方向：portrait（默认，纵向）与 landscape。缺省纵向是因为
	// 发票多为宽而矮的版式，纵向拼接能保持原始尺寸、空白最少。
	Orientation string
	// NUp 是一张纸上放几页源页面，可取 1、2、3、4、6、8、9，0 与 1 等价。
	NUp int
	// NUpGap 是拼版格子之间的间距（毫米），默认 0。
	NUpGap float64
	// CellBorder 为真时给每个格子描一圈细线，方便打印后裁切。
	CellBorder bool
	// Fit 是源页放入格子的适配方式，目前只有 FitContain（等比缩放居中补白）。
	Fit string
	// Limits 限制输入规模，零值使用默认限制。
	Limits MergeLimits
}

// MergePDF 把多张发票合并为一个 PDF 写入 w，并返回合并前的抽取与汇总结果。
//
// 合并走的是「逐张解析成渲染文档，自己按版面输出 PDF」，而不是先合并 OFD 再转换：
// 这样保留了每张票的矢量文字与 PDF 大纲（打印时可在侧栏直接跳票），也不生成
// 中间文件，还能按票裁页与拼版（见 Pages、Paper、NUp）。被去重跳过和抽取失败的
// 输入不会出现在 PDF 里，具体清单见返回的 Batch.Skipped 与 Batch.Items。
//
// 默认行为与旧版一致：不给 Paper/NUp 时每页按自身尺寸 1:1 输出，不缩放不补白。
//
// 汇总页由 pkg/creator 现生成一张 OFD 再并入同一份 PDF，因此需要系统里能解析到
// 中文字体；解析不到时汇总页文字可能缺失，此时发票页面本身不受影响。
func MergePDF(ctx context.Context, sources []Source, w io.Writer, options MergeOptions) (*Batch, error) {
	if w == nil {
		return nil, fmt.Errorf("PDF 输出写入器为空")
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("至少需要一个发票输入")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	limits, err := resolveMergeLimits(options.Limits, len(sources))
	if err != nil {
		return nil, err
	}
	payloads, err := loadSources(sources, limits)
	if err != nil {
		return nil, err
	}
	batch, err := ExtractBatch(payloads, options.BatchOptions)
	if err != nil {
		return nil, err
	}
	items := batch.ActiveItems()
	if err := SortItems(items, options.Sort); err != nil {
		return nil, err
	}
	if err := SortItems(batch.Items, options.Sort); err != nil {
		return nil, err
	}
	pageSpec, err := normalizePageSpec(options.Pages)
	if err != nil {
		return nil, err
	}
	sources2, closers, err := parseDocuments(ctx, items, payloads, pageSpec)
	if err != nil {
		closeAll(closers)
		return nil, err
	}
	defer closeAll(closers)
	entries, err := collectSheetEntries(sources2, items)
	if err != nil {
		return nil, err
	}
	// 版面要等页面尺寸齐了才能定：--orientation auto 靠实际票源长宽比挑方向。
	layout, err := resolveSheetLayout(options, entryPageSizes(entries))
	if err != nil {
		return nil, err
	}
	if options.Cover {
		cover, closer, err := coverDocument(batch.Summary, options.CoverTitle)
		if err != nil {
			return nil, err
		}
		// 汇总页的 OFD 句柄同样要活到 PDF 写完，和发票文档一样不能提前关闭。
		closers = append(closers, closer)
		entries = append([]sheetEntry{{
			document: cover,
			page:     cover.Document.Pages[0],
			width:    coverPageWidth,
			height:   coverPageHeight,
			title:    coverTitleLabel(options.CoverTitle),
		}}, entries...)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("没有可输出的页面")
	}
	placeEntries(entries, layout)
	if err := writeMergePDF(ctx, w, entries, layout); err != nil {
		return nil, err
	}
	return batch, nil
}

// coverTitleLabel 返回汇总页的大纲标题。
func coverTitleLabel(title string) string {
	if strings.TrimSpace(title) == "" {
		return defaultCoverTitle
	}
	return strings.TrimSpace(title)
}

// resolveMergeLimits 校验并补齐合并规模限制。
func resolveMergeLimits(limits MergeLimits, count int) (MergeLimits, error) {
	if limits.MaxInputBytes < 0 {
		return limits, fmt.Errorf("单输入字节上限不能为负数")
	}
	if limits.MaxTotalBytes < 0 {
		return limits, fmt.Errorf("总字节上限不能为负数")
	}
	if limits.MaxInvoices < 0 {
		return limits, fmt.Errorf("发票张数上限不能为负数")
	}
	if limits.MaxInputBytes == 0 {
		limits.MaxInputBytes = defaultMergeInputBytes
	}
	if limits.MaxTotalBytes == 0 {
		limits.MaxTotalBytes = defaultMergeTotalBytes
	}
	if limits.MaxInvoices == 0 {
		limits.MaxInvoices = defaultMergeInvoiceCount
	}
	if limits.MaxTotalBytes < limits.MaxInputBytes {
		return limits, fmt.Errorf("总字节上限 %d 不能小于单输入上限 %d",
			limits.MaxTotalBytes, limits.MaxInputBytes)
	}
	if count > limits.MaxInvoices {
		return limits, fmt.Errorf("发票张数 %d 超过上限 %d", count, limits.MaxInvoices)
	}
	return limits, nil
}

// loadSources 把输入读成字节切片，并按字节上限拦截。
//
// 合并需要把同一个 OFD 读两遍（一次抽取发票信息，一次解析渲染），所以必须先把
// io.Reader 物化成字节；顺带在这里限制规模，避免把误传的巨型文件整个读进内存。
func loadSources(sources []Source, limits MergeLimits) ([]Source, error) {
	payloads := make([]Source, 0, len(sources))
	var total int64
	for index, source := range sources {
		data, err := readInput(source.Input)
		if err != nil {
			return nil, fmt.Errorf("读取 %s 失败: %w", sourceName(source, index), err)
		}
		if int64(len(data)) > limits.MaxInputBytes {
			return nil, fmt.Errorf("%s 大小 %d 超过单输入上限 %d",
				sourceName(source, index), len(data), limits.MaxInputBytes)
		}
		total += int64(len(data))
		if total > limits.MaxTotalBytes {
			return nil, fmt.Errorf("输入总大小 %d 超过上限 %d", total, limits.MaxTotalBytes)
		}
		name := source.Name
		if strings.TrimSpace(name) == "" {
			name = sourceName(source, index)
		}
		payloads = append(payloads, Source{Name: name, Input: data})
	}
	return payloads, nil
}

// mergeSource 是一张票的解析结果：渲染文档、它所属的文档体与选中的页面。
type mergeSource struct {
	rendered *render.Document
	document *parser.Document
	pages    []*parser.Page
	item     Item
}

// parseDocuments 按 items 的顺序把各票解析为渲染文档，并按 pageSpec 选出要输出的页。
//
// 逐项按 item.source 取输入字节，而不是按下标对齐 items 与 payloads：去重会
// 去掉一部分票，排序会打乱顺序，两者的长度与顺序都不再和输入一一对应。
//
// 返回的 closers 必须在 PDF 写完之后才关闭：渲染文档持有解析器的页面租约，
// 提前关闭会在渲染期间被清空。解析按给定顺序串行进行。
func parseDocuments(ctx context.Context, items []Item, payloads []Source,
	pageSpec string) ([]mergeSource, []io.Closer, error) {
	sources := make([]mergeSource, 0, len(items))
	closers := make([]io.Closer, 0, len(items))
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			// 取消错误原样返回，调用方靠 errors.Is 区分取消与超时。
			return nil, closers, err
		}
		if item.source < 0 || item.source >= len(payloads) {
			return nil, closers, fmt.Errorf("内部错误：%s 的输入下标 %d 越界", item.Name, item.source)
		}
		data, ok := payloads[item.source].Input.([]byte)
		if !ok {
			return nil, closers, fmt.Errorf("内部错误：%s 未物化为字节", item.Name)
		}
		ofd, err := parser.NewOFD(data)
		if err != nil {
			return nil, closers, fmt.Errorf("解析 %s 失败: %w", item.Name, err)
		}
		if len(ofd.Documents) == 0 {
			_ = ofd.Close()
			return nil, closers, fmt.Errorf("解析 %s 失败: 没有文档", item.Name)
		}
		closers = append(closers, ofd)
		document := ofd.Documents[0]
		pages, err := selectPages(document, pageSpec)
		if err != nil {
			return nil, closers, fmt.Errorf("选取 %s 的页面失败: %w", item.Name, err)
		}
		rendered := render.NewDocumentWithDPI(color.Transparent, document, geom.DPI(96))
		// 编码前把全局注册的回退字体挂到文档上。
		//
		// 这一步不能省：字体注册进引擎只是让引擎"有"这份字形，而渲染时是按
		// 文档自己的字体清单找字体的，不登记就等于没有——页面看着正常，实际上
		// 一个字都没画出来。走 Reader 的转换路径由 Reader 负责登记，这里直接
		// 拿 *render.Document 输出，得自己补上。
		for _, family := range render.RegisteredFallbackFamilies() {
			if err := rendered.UseFallbackFont(family); err != nil {
				return nil, closers, fmt.Errorf("应用回退字体 %q 失败: %w", family, err)
			}
		}
		sources = append(sources, mergeSource{
			rendered: rendered,
			document: document,
			pages:    pages,
			item:     item,
		})
	}
	return sources, closers, nil
}

// collectSheetEntries 把各票选中的页面摊平成输出页序，并记下每页的源尺寸。
func collectSheetEntries(sources []mergeSource, items []Item) ([]sheetEntry, error) {
	entries := make([]sheetEntry, 0, len(items))
	for _, source := range sources {
		title := source.item.Name
		for index, page := range source.pages {
			box, err := page.PhysicalBoxMetadata()
			if err != nil {
				return nil, fmt.Errorf("读取 %s 第 %d 页尺寸失败: %w", source.item.Name, index+1, err)
			}
			entries = append(entries, sheetEntry{
				document: source.rendered,
				page:     page,
				width:    box.Width,
				height:   box.Height,
				title:    title,
			})
		}
	}
	return entries, nil
}

func closeAll(closers []io.Closer) {
	for _, closer := range closers {
		_ = closer.Close()
	}
}

// coverDocument 生成汇总对账页，返回渲染文档与必须保持打开的 OFD 句柄。
func coverDocument(summary Summary, title string) (*render.Document, io.Closer, error) {
	if strings.TrimSpace(title) == "" {
		title = defaultCoverTitle
	}
	document := creator.Document{
		ID:      "invoice-summary",
		Title:   title,
		Creator: creatorName,
		PageSize: creator.PageSize{
			Width:  coverPageWidth,
			Height: coverPageHeight,
		},
	}
	layout := coverPages(summary, title)
	pages := make([]creator.Page, 0, len(layout))
	for _, data := range layout {
		pages = append(pages, coverPage(data))
	}
	encoded, err := creator.MarshalWithPages(document, coverPageProvider{pages: pages},
		creator.CreateOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("生成汇总页失败: %w", err)
	}
	ofd, err := parser.NewOFD(encoded)
	if err != nil {
		return nil, nil, fmt.Errorf("解析汇总页失败: %w", err)
	}
	if len(ofd.Documents) == 0 {
		_ = ofd.Close()
		return nil, nil, fmt.Errorf("解析汇总页失败: 没有文档")
	}
	return render.NewDocumentWithDPI(color.Transparent, ofd.Documents[0], geom.DPI(96)), ofd, nil
}
