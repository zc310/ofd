package content

import (
	"math"

	"github.com/zc310/ofd/internal/render/geom"
	textlayout "github.com/zc310/ofd/internal/render/layout/text"
	"github.com/zc310/ofd/pkg/spec"
)

// MaxDepth 限制块与复合图元的递归嵌套深度，防止循环引用无限展开。
const MaxDepth = 32

// Source 是文字来源，与文字布局元数据共用同一类型。
type Source = textlayout.Source

const (
	SourcePage       Source = "page"
	SourceTemplate   Source = "template"
	SourceAnnotation Source = "annotation"
	SourceWatermark  Source = "watermark"
	SourceSeal       Source = "seal"
)

// Visibility 是本次输出的正文层及模板引用过滤，不修改原模型。
type Visibility struct {
	HiddenLayers    map[spec.StID]bool
	HiddenTemplates map[spec.StID]bool
}

// Options 控制附加文字来源，正文、模板及其复合对象始终收集。
type Options struct {
	IncludeAnnotations bool
	IncludeWatermarks  bool
	IncludeSeals       bool
}

// Composite 描述待展开的单元。Box 是单元的逻辑画布，Source 是其中实际映射到
// CompositeObject.Boundary 的区域；测量裁边与预算扣除由资源提供方负责。
type Composite struct {
	Items     []spec.PageItem
	Box       spec.StBox
	Source    spec.StBox
	DrawParam *spec.DrawParam
}

// Resources 提供图层参数、复合单元范围及已经构建的裁剪路径。
// 裁剪路径采用当前画布左上原点坐标，由 Frame.OFDClip 映射到目标页坐标。
type Resources interface {
	GetDrawParam(id spec.StID) *spec.DrawParam
	ResolveComposite(object spec.CompositeObject, inherited *spec.DrawParam, depth int) (Composite, bool)
	TextClip(unit spec.CTGraphicUnit) *geom.Path
	CompositeClip(object spec.CompositeObject) *geom.Path
}

// Frame 记录当前资源作用域到目标页的映射。Scope 可携带调用方的文档与预算，
// 由泛型保留具体类型，遍历层不需要访问其中的绘制或字体状态。
type Frame[S Resources] struct {
	Scope       S
	Box         spec.StBox
	Matrix      geom.Matrix
	Clip        *geom.Path
	Source      Source
	TemplateID  uint64
	DrawParam   *spec.DrawParam
	Depth       int
	InComposite bool
	// TemplateScope 在模板遍历期间绑定资源作用域；返回的释放函数必须同步调用。
	TemplateScope func(spec.StID) (S, func(), error)
}

// Visitor 同步访问一个图元及其资源作用域、坐标和裁剪。
type Visitor[S Resources] func(Frame[S], spec.PageItem)

// OFDClip 将当前画布坐标的路径映射到目标页的 OFD 坐标，不修改原路径。
func (f Frame[S]) OFDClip(path *geom.Path) *geom.Path {
	if path == nil {
		return nil
	}
	return path.Copy().Transform(f.Matrix)
}

// MapBox 构造将 from 矩形映射到 to 矩形的矩阵。两个矩形使用相同的轴方向；
// 退化的源区域或溢出坐标无法生成有限矩阵时返回 false。
func MapBox(from, to spec.StBox) (geom.Matrix, bool) {
	sx, sy := to.Width/from.Width, to.Height/from.Height
	matrix := geom.Matrix{{sx, 0, to.X - from.X*sx}, {0, sy, to.Y - from.Y*sy}}
	return matrix, finiteMatrix(matrix)
}

// Place 将子文档帧放置到目标页，并合并目标页裁剪；资源作用域仍属于子文档。
func (f Frame[S]) Place(mapping geom.Matrix, source Source, clip *geom.Path) Frame[S] {
	f.Matrix = mapping.Mul(f.Matrix)
	f.Source = source
	if f.Clip != nil {
		f.Clip = f.Clip.Copy().Transform(mapping)
	}
	f.Clip = geom.IntersectClips(f.Clip, clip)
	return f
}

// MapLayout 将独占的布局结果映射到目标页；字符切片原位更新，不修改文档模型。
func (f Frame[S]) MapLayout(layout textlayout.Layout) textlayout.Layout {
	mapBox := func(x, y, w, h float64) geom.Rect {
		return geom.RectFromSize(x, y, w, h).Transform(f.Matrix)
	}
	layout.Source = f.Source
	layout.TemplateID = f.TemplateID
	bounds := mapBox(layout.X, layout.Y, layout.Width, layout.Height)
	layout.X, layout.Y, layout.Width, layout.Height = bounds.X0, bounds.Y0, bounds.W(), bounds.H()
	layout.Size *= math.Hypot(f.Matrix[0][1], f.Matrix[1][1])
	rotation := math.Atan2(f.Matrix[1][0], f.Matrix[0][0]) * 180 / math.Pi
	for i := range layout.Glyphs {
		g := &layout.Glyphs[i]
		r := mapBox(g.X, g.Y, g.Width, g.Height)
		if f.Clip != nil && !geom.ClipContainsRect(f.Clip, r) {
			intersection := geom.Rectangle(r.W(), r.H()).Translate(r.X0, r.Y0).And(f.Clip)
			if intersection.Empty() {
				g.Width, g.Height = 0, 0
				continue
			}
			r = intersection.Bounds()
		}
		g.X, g.Y, g.Width, g.Height = r.X0, r.Y0, r.W(), r.H()
		g.Angle += rotation
	}
	if f.Source == SourceSeal {
		layout.Font = 0
	}
	return layout
}

func finiteMatrix(matrix geom.Matrix) bool {
	for _, row := range matrix {
		for _, value := range row {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return false
			}
		}
	}
	return true
}
