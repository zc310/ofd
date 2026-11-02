package render

import (
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
)

// drawPatternText 按 §8.3 表28把底纹裁到字形轮廓，再对整组应用颜色与图元透明度。
// 先完成轮廓收集并释放字体锁，底纹内引用同一字体时也能正常绘制。
func (p *Document) drawPatternText(ctx DrawContext, source *models.CTColor, object models.TextObject, pb models.StBox, parentCTM *models.CTM, clip *geom.Path, budget *renderBudget) bool {
	path := p.patternTextOutline(object, pb.Height, parentCTM)
	if path != nil && clip != nil {
		path = intersectClips(path, clip)
	}
	return p.drawPatternTextPath(ctx, source, object, path, pb, parentCTM, budget, true)
}

// drawPatternTextPath 将底纹裁到已准备的填充或描边区域，在字体锁外平铺并合成透明度。
func (p *Document) drawPatternTextPath(ctx DrawContext, source *models.CTColor, object models.TextObject, path *geom.Path, pb models.StBox, parentCTM *models.CTM, budget *renderBudget, semantic bool) bool {
	pathObject := models.PathObject{CtPath: models.CtPath{CTGraphicUnit: object.CTGraphicUnit, Fill: true}}
	if !p.drawPatternPaint(ctx, path, source, pathObject, pb, parentCTM, budget) {
		return false
	}
	// 位图保存底纹的可见效果，原排版另输出不可见文字以供 PDF 复制与搜索。
	if semantic {
		object.FillColor = &models.CTColor{Value: &models.Color{RGBA: geom.Black}}
		object.Stroke, object.Alpha = false, nil
		object.Fill.Set(true)
		p.textWithBudget(semanticOnlyContext{ctx}, object, nil, pb, parentCTM, path, budget)
	}
	return true
}
