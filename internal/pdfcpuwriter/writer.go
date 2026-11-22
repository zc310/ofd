package pdfcpuwriter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"maps"
	"math"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

// Document 是基于 pdfcpu 的实验性 PDF 写入器，同时实现 canvas.Renderer。
// 不支持并发调用；可通过 pdfcpu 构建标签启用实验后端；坐标单位为毫米，y 轴向上。
// Content operators follow ISO 32000-1 sections 8.4/8.5/8.9 and 9.4/9.7/9.10.
type Document struct {
	ctx            context.Context //nolint:containedctx // Renderer callbacks have no context parameter; this prototype is request-scoped.
	pdf            *model.Context
	width          float64
	height         float64
	content        bytes.Buffer
	resources      types.Dict
	resourceRef    types.IndirectRef
	fontResources  types.Dict
	imageResources types.Dict
	states         types.Dict
	patterns       types.Dict
	patternCache   map[string]string
	functionCache  map[string]types.IndirectRef
	fonts          map[*canvas.Font]*prototypeFont
	fontOrder      []*prototypeFont
	pages          []types.IndirectRef
	imageCache     map[[32]byte]prototypeImage
	options        Options
	pageLinks      [][]drawing.PageLink
	pageHeights    []float64
	links          []drawing.PageLink
	outlines       []drawing.PageOutline
	fontPrograms   map[prototypeFontProgramKey]types.IndirectRef
	err            error
	written        bool
}

type prototypeImage struct {
	name          string
	width, height int
}

type prototypeFontProgramKey struct {
	digest   [32]byte
	subtype  string
	trueType bool
}

const prototypePointsPerMM = 72.0 / 25.4

// ErrUnsupported 表示尚未支持的绘制能力，可通过 errors.Is 识别。
var ErrUnsupported = errors.New("pdfcpu实验写入器尚不支持此功能")

// ErrClosed 表示已经尝试写出，不能继续绘制、换页或再次写出。
var ErrClosed = errors.New("pdfcpu实验写入器已结束")

var errInvalidPageSize = errors.New("页面尺寸必须为有限正数")

// New 创建含第一页的实验写入器，资源由本次文档独占。
func New(ctx context.Context, width, height float64) (*Document, error) {
	return NewWithOptions(ctx, width, height, Options{})
}

// Options 控制实验写入器的元数据和图片编码；默认保留无损编码及原始 JPEG。
type Options struct {
	LossyImages bool   // 对非 JPEG 颜色数据使用 JPEG，透明蒙版保持无损。
	Producer    string // PDF 元数据中的 Producer；为空时使用 pdfcpu 默认值。
}

// NewWithOptions 创建含第一页的写入器，并指定图片编码选项。
func NewWithOptions(ctx context.Context, width, height float64, options Options) (*Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validPageSize(width, height) {
		return nil, errInvalidPageSize
	}
	conf := model.NewStatelessConfiguration()
	conf.ValidationMode = model.ValidationStrict
	pdf, err := pdfcpu.CreateContextWithXRefTable(conf, &types.Dim{
		Width: width * prototypePointsPerMM, Height: height * prototypePointsPerMM,
	})
	if err != nil {
		return nil, err
	}
	if options.Producer != "" {
		now := types.DateString(time.Now())
		info, err := pdf.IndRefForNewObject(types.Dict{
			"Producer":     types.StringLiteral(options.Producer),
			"CreationDate": types.StringLiteral(now),
			"ModDate":      types.StringLiteral(now),
		})
		if err != nil {
			return nil, err
		}
		pdf.Info = info
		pdf.PreserveInfoDict = true
	}
	r := &Document{
		ctx: ctx, pdf: pdf, width: width, height: height, options: options,
		fontResources: types.Dict{}, imageResources: types.Dict{}, states: types.Dict{},
		fonts: map[*canvas.Font]*prototypeFont{}, patterns: types.Dict{},
		patternCache: map[string]string{}, functionCache: map[string]types.IndirectRef{},
		imageCache:   map[[32]byte]prototypeImage{},
		fontPrograms: map[prototypeFontProgramKey]types.IndirectRef{},
	}
	r.resources = types.Dict{"Font": r.fontResources, "XObject": r.imageResources, "ExtGState": r.states, "Pattern": r.patterns}
	r.resourceRef = r.object(r.resources)
	fmt.Fprintf(&r.content, "%.12g 0 0 %.12g 0 0 cm\n", prototypePointsPerMM, prototypePointsPerMM)
	return r, nil
}

// Size 返回当前页面尺寸，单位为毫米。
func (r *Document) Size() (float64, float64) { return r.width, r.height }

// Err 返回绘制中遇到的首个错误或原始 ctx.Err()。
// canvas 绘制回调没有错误返回值，调用方可在每页绘制后主动检查。
func (r *Document) Err() error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	return r.err
}

func (r *Document) ready() bool {
	if r.written {
		if r.err == nil {
			r.err = ErrClosed
		}
		return false
	}
	if r.err == nil {
		r.err = r.ctx.Err()
	}
	return r.err == nil
}

func validPageSize(width, height float64) bool {
	return width > 0 && height > 0 && !math.IsInf(width*prototypePointsPerMM, 0) && !math.IsInf(height*prototypePointsPerMM, 0)
}

// Stats 是用于实验测量的只读资源计数，不暴露可变 PDF 对象。
type Stats struct {
	Fonts, Glyphs, Images, Patterns, Functions, Pages, ContentBytes int
}

// Stats 返回当前资源计数；Pages 仅包括已封存页面，ContentBytes 为当前页未压缩内容大小。
func (r *Document) Stats() Stats {
	s := Stats{Fonts: len(r.fontOrder), Images: len(r.imageResources), Patterns: len(r.patterns), Functions: len(r.functionCache), Pages: len(r.pages), ContentBytes: r.content.Len()}
	for _, font := range r.fontOrder {
		s.Glyphs += len(font.glyphs)
	}
	return s
}

func (r *Document) object(obj types.Object) types.IndirectRef {
	if r.err != nil {
		return types.IndirectRef{}
	}
	ref, err := r.pdf.IndRefForNewObject(obj)
	if err != nil {
		r.err = err
		return types.IndirectRef{}
	}
	return *ref
}

func (r *Document) stream(data []byte, dict types.Dict) types.IndirectRef {
	if r.err != nil {
		return types.IndirectRef{}
	}
	sd, err := r.pdf.NewStreamDictForBuf(data)
	if err == nil {
		maps.Copy(sd.Dict, dict)
		err = sd.Encode()
	}
	if err != nil {
		r.err = err
		return types.IndirectRef{}
	}
	return r.object(*sd)
}

// Font dictionaries keep their own encodings and Unicode aliases; only byte-
// identical programs with the same stream semantics share an indirect object.
func (r *Document) fontProgram(data []byte, subtype string, trueType bool) types.IndirectRef {
	key := prototypeFontProgramKey{digest: sha256.Sum256(data), subtype: subtype, trueType: trueType}
	if ref, ok := r.fontPrograms[key]; ok {
		return ref
	}
	dict := types.Dict{}
	if trueType {
		dict["Length1"] = types.Integer(len(data))
	} else {
		dict["Subtype"] = types.Name(subtype)
	}
	ref := r.stream(data, dict)
	r.fontPrograms[key] = ref
	return ref
}

func (r *Document) matrix(matrix canvas.Matrix) {
	fmt.Fprintf(&r.content, "%.12g %.12g %.12g %.12g %.12g %.12g cm\n",
		matrix[0][0], matrix[1][0], matrix[0][1], matrix[1][1], matrix[0][2], matrix[1][2])
}

func (r *Document) paint(paint canvas.Paint, stroke bool, matrix canvas.Matrix) {
	if r.err != nil {
		return
	}
	if paint.IsGradient() {
		r.gradientPaint(paint, stroke, matrix)
		return
	}
	if !paint.IsColor() {
		r.err = fmt.Errorf("%w：图案画笔", ErrUnsupported)
		return
	}
	c, _ := color.NRGBAModel.Convert(paint.Color).(color.NRGBA)
	op := "rg"
	if stroke {
		op = "RG"
	}
	fmt.Fprintf(&r.content, "%.12g %.12g %.12g %s\n", float64(c.R)/255, float64(c.G)/255, float64(c.B)/255, op)
	r.opacity(c.A)
}

func (r *Document) opacity(value uint8) {
	alpha := types.Float(float64(value) / 255)
	name := fmt.Sprintf("GS%d", value)
	if _, ok := r.states[name]; !ok {
		r.states[name] = r.object(types.Dict{"Type": types.Name("ExtGState"), "ca": alpha, "CA": alpha})
	}
	fmt.Fprintf(&r.content, "/%s gs\n", name)
}

// RenderPath 绘制路径，失败通过 Err 返回。
func (r *Document) RenderPath(path *canvas.Path, style canvas.Style, matrix canvas.Matrix) {
	if !r.ready() || path == nil || path.Empty() {
		return
	}
	if err := r.ctx.Err(); err != nil {
		r.err = err
		return
	}
	if style.HasFill() {
		r.content.WriteString("q\n")
		r.matrix(matrix)
		r.paint(style.Fill, false, matrix)
		r.content.WriteString(path.ToPDF())
		if style.FillRule == canvas.EvenOdd {
			r.content.WriteString(" f*\nQ\n")
		} else {
			r.content.WriteString(" f\nQ\n")
		}
	}
	if style.HasStroke() {
		r.stroke(path, style, matrix)
	}
}

func (r *Document) stroke(path *canvas.Path, style canvas.Style, matrix canvas.Matrix) {
	if r.err != nil {
		return
	}
	capCode := 0
	switch style.StrokeCapper.(type) {
	case canvas.ButtCapper:
	case canvas.RoundCapper:
		capCode = 1
	case canvas.SquareCapper:
		capCode = 2
	default:
		r.err = fmt.Errorf("%w：自定义端点", ErrUnsupported)
		return
	}
	joinCode, miter := 0, 10.0
	switch join := style.StrokeJoiner.(type) {
	case canvas.BevelJoiner:
		joinCode = 2
	case canvas.RoundJoiner:
		joinCode = 1
	case canvas.MiterJoiner:
		if _, ok := join.GapJoiner.(canvas.BevelJoiner); !ok || join.Limit <= 0 {
			r.err = fmt.Errorf("%w：自定义斜接", ErrUnsupported)
			return
		}
		miter = 2 * join.Limit / style.StrokeWidth
	default:
		r.err = fmt.Errorf("%w：自定义连接点", ErrUnsupported)
		return
	}
	r.content.WriteString("q\n")
	r.matrix(matrix)
	r.paint(style.Stroke, true, matrix)
	fmt.Fprintf(&r.content, "%.12g w %d J %d j %.12g M\n[", style.StrokeWidth, capCode, joinCode, miter)
	offset, dashes := canvas.ScaleDash(style.StrokeWidth, style.DashOffset, style.Dashes)
	for _, dash := range dashes {
		fmt.Fprintf(&r.content, "%.12g ", dash)
	}
	fmt.Fprintf(&r.content, "] %.12g d\n%s S\nQ\n", offset, path.ToPDF())
}

func (r *Document) clip(matrix canvas.Matrix, clip *canvas.Path, draw func()) {
	if !r.ready() || clip == nil || clip.Empty() {
		return
	}
	fmt.Fprintf(&r.content, "q\n%s W n\n", clip.Copy().Transform(matrix).ToPDF())
	draw()
	r.content.WriteString("Q\n")
}

// RenderClippedPath 在局部裁剪路径内绘制，clip 使用图元局部坐标。
func (r *Document) RenderClippedPath(path *canvas.Path, style canvas.Style, matrix canvas.Matrix, clip *canvas.Path) {
	r.clip(matrix, clip, func() { r.RenderPath(path, style, matrix) })
}

// RenderImage 绘制图片，失败通过 Err 返回。
func (r *Document) RenderImage(img image.Image, matrix canvas.Matrix) {
	if !r.ready() {
		return
	}
	resource, err := r.imageResource(img)
	if err != nil {
		r.err = err
		return
	}
	r.content.WriteString("q\n")
	// Match canvas/pdf's explicit image footprint clipping. Image interpolation
	// alone does not antialias the outer boundary in the same way as a clip.
	width, height := float64(resource.width), float64(resource.height)
	outer := (canvas.Rect{X1: width, Y1: height}).Transform(matrix)
	fmt.Fprintf(&r.content, "%.12g %.12g %.12g %.12g re W n\n", outer.X0, outer.Y0, outer.W(), outer.H())
	footprint := canvas.Rectangle(width, height).Transform(matrix)
	fmt.Fprintf(&r.content, "%s W n\n", footprint.ToPDF())
	r.matrix(matrix.Scale(float64(resource.width), float64(resource.height)))
	fmt.Fprintf(&r.content, "/%s Do\nQ\n", resource.name)
}

// RenderClippedImage 在图像局部坐标定义的裁剪路径内绘制图片。
func (r *Document) RenderClippedImage(img image.Image, matrix canvas.Matrix, clip *canvas.Path) {
	r.clip(matrix, clip, func() { r.RenderImage(img, matrix) })
}

// NextPage 封存当前页并开始下一页，保留文档级资源。
// 各页显式指定 MediaBox，遵循 ISO 32000-1 §7.7.3。
func (r *Document) NextPage(width, height float64) error {
	if r.written {
		return ErrClosed
	}
	if err := r.Err(); err != nil {
		return err
	}
	if !validPageSize(width, height) {
		return errInvalidPageSize
	}
	if err := r.finishPage(); err != nil {
		return err
	}
	r.width, r.height = width, height
	r.content.Reset()
	fmt.Fprintf(&r.content, "%.12g 0 0 %.12g 0 0 cm\n", prototypePointsPerMM, prototypePointsPerMM)
	return nil
}

func (r *Document) finishPage() error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	if r.err != nil {
		return r.err
	}
	content := r.stream(r.content.Bytes(), nil)
	root, err := r.pdf.Pages()
	if err != nil {
		return err
	}
	page := r.object(types.Dict{
		"Type": types.Name("Page"), "Parent": *root, "Contents": content, "Resources": r.resourceRef,
		"MediaBox": types.Array{types.Integer(0), types.Integer(0), types.Float(r.width * prototypePointsPerMM), types.Float(r.height * prototypePointsPerMM)},
	})
	r.pages = append(r.pages, page)
	r.pageLinks = append(r.pageLinks, r.links)
	r.pageHeights = append(r.pageHeights, r.height)
	r.links = nil
	return r.err
}

// Write 完成字体嵌入及页面树并写出 PDF，只能调用一次。
// 绘制失败在写出前返回；底层写入错误可能留下部分输出，调用方必须丢弃。
func (r *Document) Write(w io.Writer) error {
	if r.written {
		return ErrClosed
	}
	r.written = true
	if err := r.finishPage(); err != nil {
		return err
	}
	for _, font := range r.fontOrder {
		font.embed(r)
	}
	root, err := r.pdf.Pages()
	if err != nil {
		return err
	}
	pages, err := r.pdf.DereferenceDict(*root)
	if err != nil {
		return err
	}
	kids := make(types.Array, len(r.pages))
	for i, page := range r.pages {
		kids[i] = page
	}
	pages["Kids"], pages["Count"] = kids, types.Integer(len(kids))
	r.pdf.PageCount = len(kids)
	if err := r.writeNavigation(); err != nil {
		return err
	}
	if r.err != nil {
		return r.err
	}
	return api.WriteContext(r.ctx, r.pdf, w)
}
