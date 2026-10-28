package render

import (
	"fmt"
	"strings"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/content"
	"github.com/zc310/ofd/internal/render/geom"
	seallayout "github.com/zc310/ofd/internal/render/layout/seal"
)

// TextSource 标记文字来源，不把底纹中的装饰文字作为正文收集。
type TextSource = content.Source

const (
	TextSourcePage       = content.SourcePage
	TextSourceTemplate   = content.SourceTemplate
	TextSourceAnnotation = content.SourceAnnotation
	TextSourceWatermark  = content.SourceWatermark
	TextSourceSeal       = content.SourceSeal
)

// TextOptions 控制附加文字来源。正文、模板及其复合对象始终收集。
// 印章只收集 OFD 子文档里的真实文字，不对图片或轮廓执行 OCR。
type TextOptions = content.Options

// contentSource 将文档资源和本次预算交给遍历层，字体与绘制状态仍由文档持有。
type contentSource struct {
	*Document

	budget *renderBudget
}

type contentFrame = content.Frame[*contentSource]
type contentVisitor = content.Visitor[*contentSource]

func (s *contentSource) ResolveComposite(object models.CompositeObject, inherited *models.DrawParam, depth int) (content.Composite, bool) {
	unit := s.GetCompositeUnit(models.StID(object.ResourceID))
	if unit == nil || !s.budget.AllowComposite(models.StID(object.ResourceID)) {
		return content.Composite{}, false
	}
	dp := s.objectDrawParam(object.DrawParam, inherited)
	source, w, h, ok := s.compositeContentSource(object, unit, dp, depth, s.budget)
	return content.Composite{Items: unit.Content.Items, Source: source, Box: models.StBox{Width: w, Height: h}, DrawParam: dp}, ok
}

func (s *contentSource) TextClip(unit models.CTGraphicUnit, pageHeight float64) *geom.Path {
	return s.graphicClip(unit, pageHeight, nil, nil)
}

func (s *contentSource) CompositeClip(object models.CompositeObject, pageHeight float64) *geom.Path {
	ctm := models.IdentityMatrix
	if object.CTM != nil {
		ctm = *object.CTM
	}
	return s.buildImageClip(object.Clips, pageHeight, object.Boundary.X, object.Boundary.Y, ctm)
}

func (p *Document) walkPageText(page *parser.Page, visibility PageVisibility, options TextOptions, visit contentVisitor) error {
	if p == nil || p.Document == nil || page == nil {
		return fmt.Errorf("文字文档或页面为空")
	}
	lease, err := page.AcquireLease()
	if err != nil {
		return err
	}
	defer lease.Release()
	pageContent := lease.Content()
	if pageContent == nil {
		return fmt.Errorf("页面内容为空")
	}
	if p.fonts != nil {
		p.fonts.RegisterPageGlyphs(p.Document, page, pageContent)
	}
	var budget renderBudget
	budget.reset()
	frame := contentFrame{
		Scope: &contentSource{Document: p, budget: &budget}, Box: pageContent.Area.PhysicalBox,
		Matrix: geom.Identity, Source: TextSourcePage,
	}
	var annotations *models.PageAnnot
	if options.IncludeAnnotations || options.IncludeWatermarks {
		annotations, err = p.LoadAnnotation(page.ID)
		if err != nil {
			return fmt.Errorf("加载页面 %d 注解失败: %w", page.ID, err)
		}
	}
	return content.WalkPage(frame, pageContent, annotations, visibility, options, func(_ contentFrame, visit contentVisitor) error {
		for _, seal := range p.Document.GetSeals(page.ID) {
			if seal == nil || seal.SealData == nil || seal.StampAnnot == nil || !strings.EqualFold(seal.SealData.FileType, "ofd") {
				continue
			}
			if err := p.walkSealText(seal, visit); err != nil {
				return err
			}
		}
		return nil
	}, visit)
}

func (p *Document) walkSealText(info *parser.SealInfo, visit contentVisitor) error {
	entry, err := p.loadSealDocument(info.SealData.Data, true)
	if err != nil || entry == nil {
		return err
	}
	defer entry.mu.RUnlock()
	page, err := entry.ofd.Documents[0].GetPage(0)
	if err != nil {
		return err
	}
	box, err := page.PhysicalBox()
	if err != nil {
		return err
	}
	stamp := info.StampAnnot.Boundary
	if !box.IsFinite() || box.Width <= 0 || box.Height <= 0 || !stamp.IsFinite() || stamp.Width <= 0 || stamp.Height <= 0 {
		return nil
	}
	mapping := geom.Identity.Translate(stamp.X, stamp.Y).Scale(stamp.Width/box.Width, stamp.Height/box.Height)
	var clip *geom.Path
	if crop, ok := seallayout.ClipBox(stamp, info.StampAnnot.Clip); ok {
		clip = geom.Rectangle(crop.Width, crop.Height).Translate(crop.X, crop.Y)
	}
	return entry.doc.walkPageText(page, PageVisibility{}, TextOptions{}, func(child contentFrame, item models.PageItem) {
		visit(child.Place(mapping, TextSourceSeal, clip), item)
	})
}
