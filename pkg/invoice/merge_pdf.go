package invoice

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"sync"

	"image/color"

	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
)

// maxMergeWorkers 是渲染输出的并发上限。
//
// 页面渲染是 CPU 密集的，PDF 写出必须串行（同一份交叉引用表），因此并发只用在
// 「先并行渲染各张纸的矢量表面，再按序写出」这一段。上限与转换器的 PDF 并行
// 渲染保持一致。
const maxMergeWorkers = 4

// cellBorderColor 是拼版格线颜色（浅灰），只在 --cell-border 打开时画；
// cellBorderWidth 是线宽（毫米），按打印可见又不至于吃掉内容取 0.2mm。
var (
	cellBorderColor = color.RGBA{R: 0xBF, G: 0xBF, B: 0xBF, A: 0xFF}
	cellBorderWidth = 0.2
)

// 裁切线用虚线：实线打印后容易被当成票面的一部分，虚线一眼能认出是后加的辅助线。
const cellBorderDashOffset = 0.0

// cellBorderDashes 是虚线的实线段与空白段长度（毫米）。空白取得比实线段还长：
// 票据打印分辨率下 2mm 的短线容易糊成灰实线，3mm 实 4mm 空在 300dpi 上仍是
// 清晰的断续感，一眼能数出这是裁切辅助线而不是票面内容。
var cellBorderDashes = []float64{3, 4}

// writeMergePDF 按版面把 entries 写成多页 PDF。
//
// 每张纸先并行渲染成矢量表面，再按页序串行写出：PDF 的页面对象、交叉引用表都要
// 顺序追加，并发写会得到损坏的文件；而渲染本身是纯计算，可以并发。
func writeMergePDF(ctx context.Context, w io.Writer, entries []sheetEntry, layout sheetLayout) (err error) {
	sheets := groupSheets(entries, layout)
	if len(sheets) == 0 {
		return fmt.Errorf("没有可输出的页面")
	}
	pdfDoc, err := render.NewPDFDocument(w, render.PDFOptions{
		Compress: true, SubsetFonts: true, LossyImages: false,
	})
	if err != nil {
		return fmt.Errorf("创建 PDF 文档失败: %w", err)
	}
	defer func() {
		// PDF 只有写出交叉引用表与 trailer 之后才可打开，因此失败路径也要关闭；
		// 关闭错误只在没有其它错误时上报，避免盖掉真正的失败原因。
		if closeErr := pdfDoc.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("关闭 PDF 文档失败: %w", closeErr)
		}
	}()

	surfaces, err := renderSheets(ctx, sheets, layout)
	if err != nil {
		return err
	}
	for index, surface := range surfaces {
		if err := ctx.Err(); err != nil {
			return err
		}
		// 发票页面之间没有需要保留的跳转关系，链接热区不传递：源 OFD 的链接目标
		// 页号属于各自的文档体，跨文档拼版后已经失效。
		if err := pdfDoc.AddPage(surface, nil); err != nil {
			return fmt.Errorf("输出第 %d 页失败: %w", index+1, err)
		}
	}
	return writeSheetOutlines(pdfDoc, sheets, layout)
}

// mergeSheet 是一张输出纸上的若干源页面。
type mergeSheet struct {
	width, height float64
	entries       []sheetEntry
}

// groupSheets 按页序把源页面分组到各张纸上，并给出每张纸的尺寸。
//
// 三种页面各走各的路：汇总页（fullSheet）独占一张纸；fitSource 模式下一页一纸、
// 尺寸就是源页尺寸；其余按网格填格，格号在这里就地分配。
func groupSheets(entries []sheetEntry, layout sheetLayout) []mergeSheet {
	if len(entries) == 0 {
		return nil
	}
	var sheets []mergeSheet
	var current []sheetEntry
	flush := func() {
		if len(current) == 0 {
			return
		}
		sheets = append(sheets, mergeSheet{
			width:   layout.width,
			height:  layout.height,
			entries: current,
		})
		current = nil
	}
	for _, entry := range entries {
		if entry.fullSheet || layout.fitSource {
			flush()
			width, height := entry.width, entry.height
			if !layout.fitSource {
				// 指定了纸张就按纸张出：汇总页用同一张纸的纵向，不是自己那个尺寸。
				// auto 可能把输出纸选成横向（--nup 4 时 210×140 的票在 2×2 横向更
				// 省），跟着横向的话 210×297 的汇总页会被缩到 0.7 倍塞进左半张。
				width, height = coverSheetSize(layout).width, coverSheetSize(layout).height
			}
			sheets = append(sheets, mergeSheet{
				width:   width,
				height:  height,
				entries: []sheetEntry{entry},
			})
			continue
		}
		if len(current) == layout.capacity() {
			flush()
		}
		entry.index = len(current)
		current = append(current, entry)
	}
	flush()
	return sheets
}

// renderSheets 并行渲染各张纸的矢量表面，结果按页序返回。
func renderSheets(ctx context.Context, sheets []mergeSheet, layout sheetLayout) ([]render.VectorSurface, error) {
	surfaces := make([]render.VectorSurface, len(sheets))
	errs := make([]error, len(sheets))
	workers := min(runtime.GOMAXPROCS(0), maxMergeWorkers)
	if workers > len(sheets) {
		workers = len(sheets)
	}
	if workers < 2 {
		for index, sheet := range sheets {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			surfaces[index], errs[index] = renderSheet(sheet, layout)
		}
		return surfaces, firstError(errs)
	}
	var next int64 = -1
	var mu sync.Mutex
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			for {
				mu.Lock()
				next++
				index := int(next)
				mu.Unlock()
				if index >= len(sheets) {
					return
				}
				if ctx.Err() != nil {
					errs[index] = ctx.Err()
					continue
				}
				surfaces[index], errs[index] = renderSheet(sheets[index], layout)
			}
		}()
	}
	group.Wait()
	return surfaces, firstError(errs)
}

// firstError 返回页序最靠前的错误，保证同样的输入每次报同一个原因。
func firstError(errs []error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// renderSheet 渲染一张纸：把各源页面按格子等比缩放居中画进去。
//
// 矢量表面在创建时就完成绘制（工厂回调立即执行），所以这里把 Draw 的错误收集
// 到闭包里带出去，而不是从回调里返回。
func renderSheet(sheet mergeSheet, layout sheetLayout) (render.VectorSurface, error) {
	var drawErr error
	surface, err := render.NewVectorSurface(sheet.width, sheet.height, func(ctx render.DrawContext) {
		// fitSource（一页一票、纸张就是页面本身）没有可裁的东西，不画框。
		if layout.border && !layout.fitSource {
			drawCellBorders(ctx, layout, sheet)
		}
		for _, entry := range sheet.entries {
			if drawErr != nil {
				return
			}
			x, y, width, height := sheetCell(layout, sheet, entry)
			scale, dx, dy := fitTransform(entry.width, entry.height, x, y, width, height)
			ctx.Push()
			// dy 是自上而下的落位，绘制上下文自下而上，换算后票才不会落到下半个格子。
			ctx.Translate(dx, canvasOrigin(sheet.height, dy, entry.height*scale))
			ctx.Scale(scale, scale)
			drawErr = entry.document.Draw(ctx, entry.page)
			ctx.Pop()
		}
	})
	if err != nil {
		return nil, fmt.Errorf("创建 PDF 页面失败: %w", err)
	}
	if drawErr != nil {
		return nil, fmt.Errorf("渲染页面失败: %w", drawErr)
	}
	return surface, nil
}

// sheetCell 返回源页面在纸上的目标区域。
//
// 独占整张纸的页面（汇总页、fitSource 模式）铺满全纸，其余按格号取格子。
func sheetCell(layout sheetLayout, sheet mergeSheet, entry sheetEntry) (x, y, width, height float64) {
	if entry.fullSheet || layout.fitSource {
		return 0, 0, sheet.width, sheet.height
	}
	return layout.cell(entry.index)
}

// drawCellBorders 给每个格子描一圈虚线，方便打印后裁切。
//
// 两种必要处理都在这里：
//   - 必须先 ClearFill：DrawPath 同时描边和填充，画布的默认填充色是黑色，不清掉
//     会把整格涂成黑块；
//   - 坐标要按 canvasOrigin 翻转：绘制上下文的 y 自纸张下边缘起算，直接用自上而下的
//     格子坐标会画到相反的半张纸上。
//
// 格子按票的实际落位取，与票的绘制走同一个 sheetCell：fitSource 下格子就是页面
// 本身，layout 的宽高是 0，用 layout.cell 会得到空矩形。只画放了票的格子——
// 最后一张纸的空格子没有要裁的东西，画出来像是出错。
func drawCellBorders(ctx render.DrawContext, layout sheetLayout, sheet mergeSheet) {
	ctx.Push()
	defer ctx.Pop()
	ctx.ClearFill()
	ctx.SetStrokeColor(cellBorderColor)
	ctx.SetStrokeWidth(cellBorderWidth)
	ctx.SetDashes(cellBorderDashOffset, cellBorderDashes...)
	for _, entry := range sheet.entries {
		if entry.fullSheet {
			// 独占整张纸的汇总页没有可裁的东西，不画格线。
			continue
		}
		x, y, width, height := sheetCell(layout, sheet, entry)
		bottom := canvasOrigin(sheet.height, y, height)
		// 每个格子使用独立路径，提交给后端后视为只读。
		rectangle := &geom.Path{}
		rectangle.MoveTo(x, bottom)
		rectangle.LineTo(x+width, bottom)
		rectangle.LineTo(x+width, bottom+height)
		rectangle.LineTo(x, bottom+height)
		rectangle.Close()
		ctx.DrawPath(0, 0, rectangle)
	}
}

// writeSheetOutlines 按票写 PDF 大纲：一票一条，指向它第一次出现的那张纸。
//
// 拼版时一页有多张票，条目指向该票所在格子；不写大纲的话，几百页的合并 PDF
// 在阅读器侧栏里只能一路翻。
func writeSheetOutlines(pdfDoc render.PDFDocument, sheets []mergeSheet, layout sheetLayout) error {
	outlines := make([]render.PageOutline, 0, len(sheets))
	seen := make(map[string]bool, len(sheets))
	for index, sheet := range sheets {
		for _, entry := range sheet.entries {
			if entry.title == "" || seen[entry.title] {
				continue
			}
			seen[entry.title] = true
			x, y, _, _ := sheetCell(layout, sheet, entry)
			outlines = append(outlines, render.PageOutline{
				Title: entry.title,
				Level: 0,
				Target: &render.LinkTarget{
					Page: index, Type: render.DestXYZ,
					Left: x, Top: y, PageHeight: sheet.height,
				},
			})
		}
	}
	if len(outlines) == 0 {
		return nil
	}
	return pdfDoc.AddOutlines(outlines)
}
