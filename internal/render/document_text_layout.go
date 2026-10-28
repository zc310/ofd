package render

import (
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	textlayout "github.com/zc310/ofd/internal/render/layout/text"
)

// GlyphLayout 描述一个文字字符在页面上的近似区域。坐标单位为毫米，
// 使用页面左上角作为原点。
type GlyphLayout = textlayout.Character

// TextLayout 描述一个 TextCode 及其字符级几何信息。
type TextLayout = textlayout.Layout

// TextLayouts 返回页面文字的字符级布局。字符步进使用渲染器的字体度量、
// DeltaX/DeltaY、ReadDirection 和 CTM 基础计算，避免网页字体度量造成高亮偏移。
// 按背景模板、正文、前景模板的绘制顺序返回，与 TextValues 的 run 索引一致。
func (p *Document) TextLayouts(page *parser.Page) []TextLayout {
	return p.TextLayoutsWithVisibility(page, PageVisibility{})
}

// TextLayoutsWithVisibility 返回过滤正文图层和模板后的布局。
func (p *Document) TextLayoutsWithVisibility(page *parser.Page, visibility PageVisibility) []TextLayout {
	layouts, _ := p.TextLayoutsWithVisibilityE(page, visibility)
	return layouts
}

// TextLayoutsWithVisibilityE 返回过滤后的文字布局，并保留页面或模板加载错误。
func (p *Document) TextLayoutsWithVisibilityE(page *parser.Page, visibility PageVisibility) ([]TextLayout, error) {
	return p.textLayouts(page, nil, visibility)
}

// TextLayoutsWithOptionsE 按同一来源策略返回文字布局，保留资源加载错误。
func (p *Document) TextLayoutsWithOptionsE(page *parser.Page, visibility PageVisibility, options TextOptions) ([]TextLayout, error) {
	return p.textLayoutsOptions(page, nil, visibility, options)
}

// TextLayoutsForRuns 返回指定 run 的字符级布局，结果按页面绘制顺序排列。
// run 索引与 TextValues 返回的顺序一致。
func (p *Document) TextLayoutsForRuns(page *parser.Page, runIndices []int) []TextLayout {
	return p.TextLayoutsForRunsWithVisibility(page, runIndices, PageVisibility{})
}

// TextLayoutsForRunsWithVisibility 的 run 索引与过滤后的 TextValues 保持一致。
func (p *Document) TextLayoutsForRunsWithVisibility(page *parser.Page, runIndices []int, visibility PageVisibility) []TextLayout {
	layouts, _ := p.TextLayoutsForRunsWithVisibilityE(page, runIndices, visibility)
	return layouts
}

// TextLayoutsForRunsWithVisibilityE 返回指定 run 的过滤后布局，并保留加载错误。
func (p *Document) TextLayoutsForRunsWithVisibilityE(page *parser.Page, runIndices []int, visibility PageVisibility) ([]TextLayout, error) {
	return p.TextLayoutsForRunsWithOptionsE(page, runIndices, visibility, TextOptions{})
}

// TextLayoutsForRunsWithOptionsE 的 run 索引与相同选项的 TextValuesWithOptionsE 一致。
func (p *Document) TextLayoutsForRunsWithOptionsE(page *parser.Page, runIndices []int, visibility PageVisibility, options TextOptions) ([]TextLayout, error) {
	if len(runIndices) == 0 {
		return nil, nil
	}
	selected := make(map[int]struct{}, len(runIndices))
	for _, index := range runIndices {
		if index >= 0 {
			selected[index] = struct{}{}
		}
	}
	if len(selected) == 0 {
		return nil, nil
	}
	return p.textLayoutsOptions(page, selected, visibility, options)
}

func (p *Document) textLayouts(page *parser.Page, selected map[int]struct{}, visibility PageVisibility) ([]TextLayout, error) {
	return p.textLayoutsOptions(page, selected, visibility, TextOptions{})
}

func (p *Document) textLayoutsOptions(page *parser.Page, selected map[int]struct{}, visibility PageVisibility, options TextOptions) ([]TextLayout, error) {
	layouts := make([]TextLayout, 0)
	runIndex := 0
	err := p.walkPageText(page, visibility, options, func(frame contentFrame, item models.PageItem) {
		if item.Kind != models.PageItemText || !item.Text.VisibleValue() {
			return
		}
		var local []TextLayout
		collectObjectTextLayouts(*item.Text, &local, frame.Scope.Document, selected, &runIndex)
		for _, layout := range local {
			layouts = append(layouts, frame.MapLayout(layout))
		}
	})
	if err != nil {
		return nil, err
	}
	return layouts, nil
}

// TextValues 返回页面中可见的非空 TextCode 文本，顺序与 TextLayouts 一致。
// 搜索等只需读取文字内容的调用方可避免构建字符级几何信息。
func (p *Document) TextValues(page *parser.Page) []string {
	return p.TextValuesWithVisibility(page, PageVisibility{})
}

// TextValuesWithVisibility 返回过滤正文图层和模板后的文字，供搜索索引使用。
func (p *Document) TextValuesWithVisibility(page *parser.Page, visibility PageVisibility) []string {
	values, _ := p.TextValuesWithVisibilityE(page, visibility)
	return values
}

// TextValuesWithVisibilityE 返回过滤后的文字值，并保留页面或模板加载错误。
func (p *Document) TextValuesWithVisibilityE(page *parser.Page, visibility PageVisibility) ([]string, error) {
	return p.TextValuesWithOptionsE(page, visibility, TextOptions{})
}

// TextValuesWithOptionsE 与布局共用来源、遍历顺序和复合对象展开规则。
func (p *Document) TextValuesWithOptionsE(page *parser.Page, visibility PageVisibility, options TextOptions) ([]string, error) {
	values := make([]string, 0)
	err := p.walkPageText(page, visibility, options, func(_ contentFrame, item models.PageItem) {
		if item.Kind != models.PageItemText || !item.Text.VisibleValue() {
			return
		}
		for _, code := range item.Text.TextCode {
			if code.Value != "" {
				values = append(values, code.Value)
			}
		}
	})
	if err != nil {
		return nil, err
	}
	return values, nil
}

func collectTextValuesBlock(block *models.CTPageBlock, values *[]string) {
	if block == nil {
		return
	}
	for _, item := range block.Items {
		if !pageItemPresent(item) {
			continue
		}
		switch item.Kind {
		case models.PageItemText:
			if !item.Text.VisibleValue() {
				continue
			}
			for _, code := range item.Text.TextCode {
				if code.Value != "" {
					*values = append(*values, code.Value)
				}
			}
		case models.PageItemBlock:
			collectTextValuesBlock(&item.Block.CTPageBlock, values)
		}
	}
}

func collectTextLayoutBlock(block *models.CTPageBlock, layouts *[]TextLayout, document *Document, selected map[int]struct{}, runIndex *int) {
	if block == nil {
		return
	}
	for _, item := range block.Items {
		if !pageItemPresent(item) {
			continue
		}
		switch item.Kind {
		case models.PageItemText:
			if !item.Text.VisibleValue() {
				continue
			}
			collectObjectTextLayouts(*item.Text, layouts, document, selected, runIndex)
		case models.PageItemBlock:
			collectTextLayoutBlock(&item.Block.CTPageBlock, layouts, document, selected, runIndex)
		}
	}
}

// 同一文字对象的全部 TextCode 使用相同字体选择，只在确实请求布局时创建一次
// 字体面并持锁度量；不跨对象复用，以保留按实际文字覆盖选择回退字体的语义。
func collectObjectTextLayouts(object models.TextObject, layouts *[]TextLayout, document *Document, selected map[int]struct{}, runIndex *int) {
	var face FontFace
	var release func()
	defer func() {
		if release != nil {
			release()
		}
	}()
	position := 0
	for _, code := range object.TextCode {
		if code.Value != "" {
			_, wanted := selected[*runIndex]
			if selected == nil || wanted {
				if release == nil {
					face, release = loadTextLayoutFace(document, object)
				}
				*layouts = append(*layouts, textlayout.Build(face, object, code, position))
			}
			*runIndex = *runIndex + 1
		}
		position += len([]rune(code.Value))
	}
}

func loadTextLayoutFace(document *Document, object models.TextObject) (FontFace, func()) {
	if document == nil || document.fonts == nil {
		return nil, func() {}
	}
	family, err := document.fonts.LoadFont(object.Font)
	if err != nil || family == nil {
		return nil, func() {}
	}
	lock := document.fonts.RenderLock(family)
	lock.Lock()
	if object.CTM != nil {
		if scale := object.CTM.YScale(); scale > 0 {
			object.Size *= scale
		}
	}
	return document.fonts.FaceObject(family, object, nil), lock.Unlock
}

func buildTextLayout(document *Document, object models.TextObject, code models.TextCode, codePosition int) TextLayout {
	face, release := loadTextLayoutFace(document, object)
	defer release()
	return textlayout.Build(face, object, code, codePosition)
}
