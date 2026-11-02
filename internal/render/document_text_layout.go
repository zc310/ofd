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

// TextQueryOptions 统一文字来源、可见性及 run 选择。RunIndices 为 nil 时选择全部，
// 非 nil 的空切片选择零个 run；索引始终基于相同可见性和来源策略的完整文字流。
type TextQueryOptions struct {
	Visibility PageVisibility
	Sources    TextOptions
	RunIndices []int
}

func selectedTextRuns(indices []int) map[int]struct{} {
	if indices == nil {
		return nil
	}
	selected := make(map[int]struct{}, len(indices))
	for _, index := range indices {
		if index >= 0 {
			selected[index] = struct{}{}
		}
	}
	return selected
}

// QueryTextLayouts 返回字符布局，并显式报告页面或模板加载错误。
func (p *Document) QueryTextLayouts(page *parser.Page, options TextQueryOptions) ([]TextLayout, error) {
	selected := selectedTextRuns(options.RunIndices)
	layouts := make([]TextLayout, 0)
	runIndex := 0
	err := p.walkPageText(page, options.Visibility, options.Sources, func(frame contentFrame, item models.PageItem) {
		if item.Kind != models.PageItemText || !item.Text.VisibleValue() {
			return
		}
		var local []TextLayout
		collectObjectTextLayouts(*item.Text, &local, frame.Scope.Document, selected, &runIndex)
		for _, layout := range local {
			if frame.TemplateID != 0 && frame.Scope.fonts != nil {
				layout.EmbeddedFontLoaded = frame.Scope.fonts.HasLoadedEmbeddedFont(models.StRefID(layout.Font))
				layout.FallbackFontFamily = frame.Scope.fonts.FallbackFontFamily(models.StRefID(layout.Font))
			}
			layouts = append(layouts, frame.MapLayout(layout))
		}
	})
	if err != nil {
		return nil, err
	}
	return layouts, nil
}

// QueryTextValues 返回文字值；与 QueryTextLayouts 使用相同来源、选择和遍历顺序。
func (p *Document) QueryTextValues(page *parser.Page, options TextQueryOptions) ([]string, error) {
	selected := selectedTextRuns(options.RunIndices)
	runIndex := 0
	values := make([]string, 0)
	err := p.walkPageText(page, options.Visibility, options.Sources, func(_ contentFrame, item models.PageItem) {
		if item.Kind != models.PageItemText || !item.Text.VisibleValue() {
			return
		}
		for _, code := range item.Text.TextCode {
			if code.Value != "" {
				if _, wanted := selected[runIndex]; selected == nil || wanted {
					values = append(values, code.Value)
				}
				runIndex++
			}
		}
	})
	if err != nil {
		return nil, err
	}
	return values, nil
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
	face, family := document.fonts.FaceObject(family, object, nil)
	if face == nil {
		return nil, func() {}
	}
	lock := document.fonts.RenderLock(family)
	lock.Lock()
	return face, lock.Unlock
}

func buildTextLayout(document *Document, object models.TextObject, code models.TextCode, codePosition int) TextLayout {
	face, release := loadTextLayoutFace(document, object)
	defer release()
	return textlayout.Build(face, object, code, codePosition)
}
