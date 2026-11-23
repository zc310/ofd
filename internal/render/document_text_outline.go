package render

import (
	"github.com/zc310/ofd/internal/render/geom"
	textlayout "github.com/zc310/ofd/internal/render/layout/text"
	"github.com/zc310/ofd/pkg/spec"
)

// textOutline 在字体锁内构造可见轮廓，定位与正文共用 layout/text 的分段规则。
// 裁剪和底纹只需要几何，Fill、Stroke、Alpha 不影响轮廓。
func (p *Document) textOutline(object spec.TextObject, transform func(x, y float64) geom.Matrix) *geom.Path {
	var path *geom.Path
	p.withTextOutlineFace(object, func(face FontFace) {
		path = textlayout.Outline(face, object, transform)
	})
	return path
}

// withTextOutlineFace 只在轮廓访问期间持有字体锁；底纹内容必须在回调返回后绘制。
func (p *Document) withTextOutlineFace(object spec.TextObject, visit func(FontFace)) {
	if p.fonts == nil || !object.Boundary.IsFinite() || !object.CTM.IsFinite() ||
		!finiteFloat(object.Size) || object.Size <= 0 || !finiteFloat(textlayout.HScale(object)) {
		return
	}
	family, err := p.fonts.LoadFont(object.Font)
	if err != nil || family == nil {
		return
	}
	object.Stroke, object.Alpha = false, nil
	object.Fill.Set(true)
	face, family := p.fonts.FaceObject(family, object, &CTColor{Value: geom.Black, HasValue: true})
	if face == nil {
		return
	}
	lock := p.fonts.RenderLock(family)
	lock.Lock()
	defer lock.Unlock()
	visit(face)
}

// outlineRunMatrix 把 Y 向上的字形映射到画布，保持文字 CTM、字符方向与 HScale 的顺序。
func outlineRunMatrix(object spec.TextObject, x, y float64, ctm *spec.CTM) geom.Matrix {
	// 字体轮廓仍为 Y 向上；仅在字体到 OFD 页面空间的边界翻转。
	matrix := geom.Identity.Translate(x, y).Scale(1, -1)
	if linear := textlayout.CTMLinearMatrix(ctm); linear != nil {
		matrix = matrix.Mul(*linear)
	}
	return matrix.Rotate(-textlayout.CharDirection(object)).Scale(textlayout.HScale(object), 1)
}

// buildTextClipPath 遵循 GB/T 33190—2016 §8.4 表33、§11.3 表46/47、§11.4：
// 先应用文字 CTM，再加文字 Boundary，最后应用 Area CTM 与外层 Boundary。
// 字距、字形映射和字符方向与正文共用；字体轮廓在此映射到 Y 向下的页面空间。
func (p *Document) buildTextClipPath(text *spec.CtText, areaCTM spec.CTM, box spec.StBox) *geom.Path {
	if text == nil || !areaCTM.IsFinite() || !text.CTM.IsFinite() || !box.IsFinite() {
		return nil
	}
	combined := areaCTM
	if text.CTM != nil {
		combined = *areaCTM.Multiply(text.CTM)
		if !combined.IsFinite() {
			return nil
		}
	}
	object := spec.TextObject{CtText: *text}
	return p.textOutline(object, func(x, y float64) geom.Matrix {
		if text.CTM != nil {
			x, y = text.CTM.Transform(x, y)
		}
		x, y = areaCTM.Transform(x+text.Boundary.X, y+text.Boundary.Y)
		return outlineRunMatrix(object, x+box.X, y+box.Y, &combined)
	})
}

func (p *Document) patternTextOutline(object spec.TextObject, parent *spec.CTM) *geom.Path {
	transform := textPageTransform(object, parent)
	if transform == nil {
		return nil
	}
	return p.textOutline(object, transform)
}

// textPageTransform 按 GB/T 33190—2016 §8.1.4/5、§8.3.3 将父级与文字 CTM
// 同时作用于 run 原点和整个字形；再处理字符方向与 HScale。不含设备上下文变换。
func textPageTransform(object spec.TextObject, parent *spec.CTM) func(x, y float64) geom.Matrix {
	if !parent.IsFinite() || !object.CTM.IsFinite() || !object.Boundary.IsFinite() {
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
		return outlineRunMatrix(object, x+object.Boundary.X, y+object.Boundary.Y, ctm)
	}
}
