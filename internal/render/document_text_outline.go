package render

import (
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/geom"
	textlayout "github.com/zc310/ofd/internal/render/layout/text"
)

// textOutline 在字体锁内构造可见轮廓，定位与正文共用 layout/text 的分段规则。
// 裁剪和底纹只需要几何，Fill、Stroke、Alpha 不影响轮廓。
func (p *Document) textOutline(object models.TextObject, transform func(x, y float64) geom.Matrix) *geom.Path {
	if p.fonts == nil || !object.Boundary.IsFinite() || !object.CTM.IsFinite() ||
		!finiteFloat(object.Size) || object.Size <= 0 || !finiteFloat(textlayout.HScale(object)) {
		return nil
	}
	family, err := p.fonts.LoadFont(object.Font)
	if err != nil || family == nil {
		return nil
	}
	lock := p.fonts.RenderLock(family)
	lock.Lock()
	defer lock.Unlock()
	object.Stroke, object.Alpha = false, nil
	object.Fill.Set(true)
	face := p.fonts.FaceObject(family, object, &CTColor{Value: geom.Black, HasValue: true})
	if face == nil {
		return nil
	}
	return textlayout.Outline(face, object, transform)
}

// outlineRunMatrix 把 Y 向上的字形映射到画布，保持文字 CTM、字符方向与 HScale 的顺序。
func outlineRunMatrix(object models.TextObject, x, y float64, ctm *models.CTM) geom.Matrix {
	matrix := geom.Identity.Translate(x, y)
	if linear := textlayout.CTMLinearMatrix(ctm); linear != nil {
		matrix = matrix.Mul(*linear)
	}
	return matrix.Rotate(-textlayout.CharDirection(object)).Scale(textlayout.HScale(object), 1)
}

// buildTextClipPath 遵循 GB/T 33190—2016 §8.4 表33、§11.3 表46/47、§11.4：
// 先应用文字 CTM，再加文字 Boundary，最后应用 Area CTM 与外层 Boundary。
// 字距、字形映射和字符方向与正文共用；轮廓始终以基线为原点、Y 向上。
func (p *Document) buildTextClipPath(text *models.CtText, areaCTM models.CTM, box models.StBox, pageHeight float64) *geom.Path {
	if text == nil || !areaCTM.IsFinite() || !text.CTM.IsFinite() || !box.IsFinite() || !finiteFloat(pageHeight) {
		return nil
	}
	combined := areaCTM
	if text.CTM != nil {
		combined = *areaCTM.Multiply(text.CTM)
		if !combined.IsFinite() {
			return nil
		}
	}
	object := models.TextObject{CtText: *text}
	return p.textOutline(object, func(x, y float64) geom.Matrix {
		if text.CTM != nil {
			x, y = text.CTM.Transform(x, y)
		}
		x, y = areaCTM.Transform(x+text.Boundary.X, y+text.Boundary.Y)
		return outlineRunMatrix(object, x+box.X, pageHeight-(y+box.Y), &combined)
	})
}

func (p *Document) patternTextOutline(object models.TextObject, pageHeight float64, parent *models.CTM) *geom.Path {
	transform := textPageTransform(object, pageHeight, parent)
	if transform == nil {
		return nil
	}
	return p.textOutline(object, transform)
}

// textPageTransform 与正文保持一致：父级 CTM 参与 run 原点定位，文字自身的
// CTM 线性部分、字符方向和 HScale 作用于字形。返回值不含设备上下文变换。
func textPageTransform(object models.TextObject, pageHeight float64, parent *models.CTM) func(x, y float64) geom.Matrix {
	if !parent.IsFinite() || !object.CTM.IsFinite() || !object.Boundary.IsFinite() || !finiteFloat(pageHeight) {
		return nil
	}
	ctm := object.CTM
	if parent != nil {
		ctm = parent
		if object.CTM != nil {
			ctm = parent.Multiply(object.CTM)
		}
	}
	if !ctm.IsFinite() {
		return nil
	}
	return func(x, y float64) geom.Matrix {
		if ctm != nil {
			x, y = ctm.Transform(x, y)
		}
		return outlineRunMatrix(object, x+object.Boundary.X, pageHeight-(y+object.Boundary.Y), object.CTM)
	}
}
