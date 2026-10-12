package pdf2ofd

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/creator"
)

const transparencyDPMM = 300.0 / 25.4

// 页面、嵌套组与软掩码共用同一像素网格。组的初始背景必须与组自身的
// 不透明度分开保存，避免结束组时把背景再次叠加。
type pdfTransparencyGroup struct {
	rect            image.Rectangle
	initial, result *image.NRGBA
	alpha, shape    []float32
	knockout        bool
	texts           []creator.Text
}

func (p *pdfInterpreter) transparencyRect(bounds [4]float64) (image.Rectangle, error) {
	if p.page == nil || p.page.Area == nil || p.page.Area.PhysicalBox == nil {
		return image.Rectangle{}, fmt.Errorf("透明合成缺少页面尺寸")
	}
	box := p.page.Area.PhysicalBox
	for _, v := range bounds {
		if !calculatorFinite(v) || math.Abs(v*transparencyDPMM) >= float64(int(^uint(0)>>1))/2 {
			return image.Rectangle{}, fmt.Errorf("透明合成边界无效")
		}
	}
	bounds[0], bounds[1] = math.Max(0, bounds[0]), math.Max(0, bounds[1])
	bounds[2], bounds[3] = math.Min(box.Width, bounds[2]), math.Min(box.Height, bounds[3])
	if bounds[0] >= bounds[2] || bounds[1] >= bounds[3] {
		return image.Rectangle{}, nil
	}
	r := image.Rect(int(math.Floor(bounds[0]*transparencyDPMM)), int(math.Floor(bounds[1]*transparencyDPMM)), int(math.Ceil(bounds[2]*transparencyDPMM)), int(math.Ceil(bounds[3]*transparencyDPMM)))
	if p.transparency != nil {
		r = r.Intersect(p.transparency.rect)
	}
	// 图元的 Boundary 可能覆盖整页，而实际只在小裁剪区内绘制。先按所有
	// 裁剪区的保守包围盒缩小栅格区域，精确轮廓仍由渲染器求交。
	for _, clip := range p.state.clips {
		if clip.empty {
			return image.Rectangle{}, nil
		}
		bounds := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		add := func(x, y float64) {
			bounds[0] = math.Min(bounds[0], x)
			bounds[1] = math.Min(bounds[1], y)
			bounds[2] = math.Max(bounds[2], x)
			bounds[3] = math.Max(bounds[3], y)
		}
		for _, cmd := range clip.commands {
			for i := 0; i+1 < len(cmd.values); i += 2 {
				x, y := p.pagePoint(cmd.values[i], cmd.values[i+1])
				add(x, y)
			}
		}
		for _, text := range clip.texts {
			add(text.X, text.Y)
			add(text.X+text.Width, text.Y+text.Height)
		}
		if calculatorFinite(bounds[0]) && calculatorFinite(bounds[1]) && calculatorFinite(bounds[2]) && calculatorFinite(bounds[3]) {
			bounds[0], bounds[1] = math.Max(0, bounds[0]), math.Max(0, bounds[1])
			bounds[2], bounds[3] = math.Min(box.Width, bounds[2]), math.Min(box.Height, bounds[3])
			if bounds[0] >= bounds[2] || bounds[1] >= bounds[3] {
				return image.Rectangle{}, nil
			}
			cr := image.Rect(int(math.Floor(math.Max(0, bounds[0])*transparencyDPMM)), int(math.Floor(math.Max(0, bounds[1])*transparencyDPMM)), int(math.Ceil(math.Min(box.Width, bounds[2])*transparencyDPMM)), int(math.Ceil(math.Min(box.Height, bounds[3])*transparencyDPMM)))
			r = r.Intersect(cr)
		}
	}
	return r, nil
}

func (p *pdfInterpreter) reserveTransparency(rect image.Rectangle, buffers int) error {
	if rect.Empty() {
		return nil
	}
	if p.control == nil {
		return nil
	}
	pixels, err := p.control.imagePixels(int64(rect.Dx()), int64(rect.Dy()))
	if err != nil {
		return err
	}
	for i := 0; i < buffers; i++ {
		if err := p.control.addImagePixels(pixels); err != nil {
			return err
		}
	}
	return nil
}

func transparencyItemBounds(item creator.Item) [4]float64 {
	var x, y, w, h, margin float64
	switch v := item.(type) {
	case creator.Path:
		x, y, w, h = v.X, v.Y, v.Width, v.Height
		if v.Stroke {
			margin = math.Max(v.LineWidth, v.MiterLimit)
		}
	case creator.Text:
		x, y, w, h = v.X, v.Y, v.Width, v.Height
		// Boundary 已是变换后的包围盒；额外留出字形外伸的空间。
		margin = v.Size
	case creator.Image:
		x, y, w, h = v.X, v.Y, v.Width, v.Height
	}
	margin += 1 / transparencyDPMM
	return [4]float64{x - margin, y - margin, x + w + margin, y + h + margin}
}

// 只栅格化受影响的矩形；字体、裁剪、渐变和图片变换复用 OFD 输出的渲染路径。
func (p *pdfInterpreter) rasterTransparency(items []creator.Item, rect image.Rectangle) (*image.NRGBA, error) {
	if err := p.reserveTransparency(rect, 2); err != nil {
		return nil, err
	}
	if rect.Empty() {
		return image.NewNRGBA(rect), nil
	}
	if err := p.control.consumeOperations(int64(rect.Dx()) * int64(rect.Dy())); err != nil {
		return nil, err
	}
	for _, item := range items {
		work := int64(1)
		switch v := item.(type) {
		case creator.Path:
			work += int64(strings.Count(v.Data, " "))
		case creator.Image:
			if err := p.reserveImageDecode(v.Data); err != nil {
				return nil, err
			}
		case creator.Text:
			work += int64(len(v.Value))
		}
		if err := p.control.consumeOperations(work); err != nil {
			return nil, err
		}
	}
	box := p.page.Area.PhysicalBox
	snapshot := creator.Document{ID: "pdf-transparency", PageSize: creator.PageSize{Width: box.Width, Height: box.Height}, Pages: []creator.Page{{Area: p.page.Area, Items: items}}}
	if p.document != nil {
		snapshot.Fonts, snapshot.DrawParams = p.document.Fonts, p.document.DrawParams
	}
	data, err := creator.MarshalWithOptions(snapshot, creator.CreateOptions{PreserveEmbeddedFonts: true})
	if err != nil {
		return nil, err
	}
	if err := p.control.check(); err != nil {
		return nil, err
	}
	ofd, err := parser.NewOFD(data)
	if err != nil {
		return nil, err
	}
	defer func() { _ = ofd.Close() }()
	doc := render.NewDocumentWithDPI(color.Transparent, ofd.Documents[0], geom.DPI(300))
	defer doc.Close()
	w, h := float64(rect.Dx())/transparencyDPMM, float64(rect.Dy())/transparencyDPMM
	var drawErr error
	surface, err := render.NewVectorSurface(w, h, func(ctx render.DrawContext) {
		ctx.Translate(-float64(rect.Min.X)/transparencyDPMM, h+float64(rect.Min.Y)/transparencyDPMM-box.Height)
		drawErr = doc.Draw(ctx, ofd.Documents[0].Pages[0])
	})
	if err != nil {
		return nil, err
	}
	if drawErr != nil {
		return nil, drawErr
	}
	if err := p.control.check(); err != nil {
		return nil, err
	}
	raster := surface.Rasterize(geom.DPI(300))
	output := image.NewNRGBA(rect)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		if err := p.control.check(); err != nil {
			return nil, err
		}
		for x := rect.Min.X; x < rect.Max.X; x++ {
			output.Set(x, y, raster.At(x-rect.Min.X, y-rect.Min.Y))
		}
	}
	return output, nil
}

func (p *pdfInterpreter) transparencyBackdrop(rect image.Rectangle) (*image.NRGBA, error) {
	if p.transparency == nil {
		return p.rasterTransparency(p.page.Items, rect)
	}
	if err := p.reserveTransparency(rect, 1); err != nil {
		return nil, err
	}
	output := image.NewNRGBA(rect)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		if err := p.control.check(); err != nil {
			return nil, err
		}
		for x := rect.Min.X; x < rect.Max.X; x++ {
			output.SetNRGBA(x, y, p.transparency.result.NRGBAAt(x, y))
		}
	}
	return output, nil
}

func transparencyRGB(c color.NRGBA) [3]float64 {
	return [3]float64{float64(c.R) / 255, float64(c.G) / 255, float64(c.B) / 255}
}
func transparencyByte(v float64) byte { return byte(math.Round(clampOpacity(v) * 255)) }

// 反推可用 Normal 叠加到现有背景上的源颜色：(1-ab)*Cs + ab*B(Cb,Cs)。
// 保留源图元的不透明度，避免把背景不透明度烘入覆盖图后在边缘重复绘制。
func transparencySource(b, s color.NRGBA, mode string) color.NRGBA {
	bv, sv := transparencyRGB(b), transparencyRGB(s)
	mixed, ab := pdfBlendColor(mode, bv, sv), float64(b.A)/255
	return color.NRGBA{R: transparencyByte((1-ab)*sv[0] + ab*mixed[0]), G: transparencyByte((1-ab)*sv[1] + ab*mixed[1]), B: transparencyByte((1-ab)*sv[2] + ab*mixed[2]), A: s.A}
}

func transparencyOver(b, s color.NRGBA) color.NRGBA {
	ab, as := float64(b.A)/255, float64(s.A)/255
	a := as + ab*(1-as)
	if a == 0 {
		return color.NRGBA{}
	}
	channel := func(b, s byte) byte { return transparencyByte((float64(s)/255*as + float64(b)/255*ab*(1-as)) / a) }
	return color.NRGBA{R: channel(b.R, s.R), G: channel(b.G, s.G), B: channel(b.B, s.B), A: transparencyByte(a)}
}

func (p *pdfInterpreter) transparencyImage(img *image.NRGBA) (creator.Image, error) {
	data, _, err := encodePNGImage(img)
	if err != nil {
		return creator.Image{}, err
	}
	r := img.Rect
	x, y, w, h := float64(r.Min.X)/transparencyDPMM, float64(r.Min.Y)/transparencyDPMM, float64(r.Dx())/transparencyDPMM, float64(r.Dy())/transparencyDPMM
	return creator.Image{X: x, Y: y, Width: w, Height: h, CTM: &creator.CTM{w, 0, 0, h, 0, 0}, Data: data, Format: "PNG"}, p.control.check()
}

func (p *pdfInterpreter) invisibleTransparencyText(text creator.Text) creator.Text {
	// 复用当前源字体的 Unicode 覆盖，避免中文组里的每个文字图元生成不同的
	// 空轮廓字体；源文本仍会补入映射，保证未声明 ToUnicode 的文字可提取。
	text, _ = p.semanticText(text, p.fonts[p.state.fontName])
	text.Stroke = false
	return text
}

// 去掉图元的常量不透明度，保留图片和渐变自身的覆盖率；这样透明图元
// 仍能在挖空组中移除此前的组内容。
func transparencyShapeItem(item creator.Item) creator.Item {
	colorShape := func(c *creator.Color) *creator.Color {
		if c == nil {
			return nil
		}
		copy := *c
		copy.Alpha = nil
		return &copy
	}
	switch v := item.(type) {
	case creator.Path:
		v.Alpha = nil
		v.FillColor = colorShape(v.FillColor)
		v.StrokeColor = colorShape(v.StrokeColor)
		return v
	case creator.Image:
		v.Alpha = nil
		return v
	case creator.Text:
		v.FillColor = colorShape(v.FillColor)
		v.StrokeColor = colorShape(v.StrokeColor)
		return v
	}
	return item
}

// 单次填充或描边的常量不透明度可从源栅格反算形状，避免重新加载字体、
// 图片或渐变。零不透明度仍需另画形状，以保留挖空语义。
func transparencyConstantOpacity(item creator.Item) (float64, bool) {
	alpha := func(a *uint8) float64 {
		if a == nil {
			return 1
		}
		return float64(*a) / 255
	}
	colorAlpha := func(c *creator.Color) float64 {
		if c == nil {
			return 1
		}
		return alpha(c.Alpha)
	}
	switch v := item.(type) {
	case creator.Image:
		return alpha(v.Alpha), true
	case creator.Path:
		if v.Fill && !v.Stroke {
			return alpha(v.Alpha) * colorAlpha(v.FillColor), true
		}
		if v.Stroke && !v.Fill {
			return alpha(v.Alpha) * colorAlpha(v.StrokeColor), true
		}
	case creator.Text:
		fill := v.Fill == nil || *v.Fill
		if fill && !v.Stroke {
			return colorAlpha(v.FillColor), true
		}
		if v.Stroke && !fill {
			return colorAlpha(v.StrokeColor), true
		}
	}
	return 0, false
}

func (p *pdfInterpreter) compositeTransparencyItem(item creator.Item) error {
	if mask := p.state.softMask; mask != nil && !mask.reported {
		p.report("graphics.mask", "矢量软掩码以 300dpi 局部合成，保留 Alpha/Luminosity、BC 和 TR")
		mask.reported = true
	}
	if err := p.control.check(); err != nil {
		return err
	}
	if p.transparency != nil && p.control != nil {
		if err := p.control.check(); err != nil {
			return err
		}
		if p.control.output >= p.control.options.Limits.MaxOutputObjects {
			return p.control.limit("output_objects", p.control.output+1, p.control.options.Limits.MaxOutputObjects)
		}
		p.control.output++
	}
	r, err := p.transparencyRect(transparencyItemBounds(item))
	if err != nil {
		return err
	}
	if r.Empty() {
		if text, ok := item.(creator.Text); ok {
			if p.transparency != nil {
				p.transparency.texts = append(p.transparency.texts, p.invisibleTransparencyText(text))
			} else {
				p.appendRawItem(p.invisibleTransparencyText(text))
			}
		}
		return p.control.check()
	}
	source := p.transparencyRaster
	if source != nil {
		// 结束组得到的栅格已经位于相同像素网格，直接参与父组或背景混合，
		// 避免先编码 PNG、再解码与重采样而引入额外开销和边缘误差。
		r = r.Intersect(source.Rect)
	} else {
		source, err = p.rasterTransparency([]creator.Item{item}, r)
		if err != nil {
			return err
		}
	}
	shape := p.transparencyShape
	if p.transparency != nil && shape == nil && !p.state.alphaIsShape {
		needsShape := p.state.softMask != nil
		alphaColor := func(c *creator.Color) bool { return c != nil && c.Alpha != nil && *c.Alpha < 255 }
		switch v := item.(type) {
		case creator.Path:
			needsShape = needsShape || (v.Alpha != nil && *v.Alpha < 255) || alphaColor(v.FillColor) || alphaColor(v.StrokeColor)
		case creator.Image:
			needsShape = needsShape || (v.Alpha != nil && *v.Alpha < 255)
		case creator.Text:
			needsShape = needsShape || alphaColor(v.FillColor) || alphaColor(v.StrokeColor)
		}
		shape = source
		if needsShape {
			if opacity, ok := transparencyConstantOpacity(item); ok && opacity > 0 {
				if err := p.reserveTransparency(r, 1); err != nil {
					return err
				}
				shape = image.NewNRGBA(r)
				for y := r.Min.Y; y < r.Max.Y; y++ {
					if err := p.control.check(); err != nil {
						return err
					}
					for x := r.Min.X; x < r.Max.X; x++ {
						shape.SetNRGBA(x, y, color.NRGBA{A: transparencyByte(float64(source.NRGBAAt(x, y).A) / 255 / opacity)})
					}
				}
			} else {
				shape, err = p.rasterTransparency([]creator.Item{transparencyShapeItem(item)}, r)
				if err != nil {
					return err
				}
			}
		}
	}
	if p.state.softMask != nil {
		if err := p.applyTransparencyMask(source, p.state.softMask); err != nil {
			return err
		}
	}
	if p.state.alphaIsShape {
		shape = source
	}
	if p.transparency != nil {
		f := p.transparency
		if _, text := item.(creator.Text); text && f.knockout {
			p.report("graphics.knockout_text", "挖空组中的文字按输出文字图元合成，尚未完整保留 TK 的文字对象整体形状")
		}
		for y := r.Min.Y; y < r.Max.Y; y++ {
			if err := p.control.check(); err != nil {
				return err
			}
			for x := r.Min.X; x < r.Max.X; x++ {
				s := source.NRGBAAt(x, y)
				i := (y-f.rect.Min.Y)*f.rect.Dx() + x - f.rect.Min.X
				a := float32(s.A) / 255
				coverage := a
				b := f.result.NRGBAAt(x, y)
				if shape != nil {
					coverage = float32(shape.NRGBAAt(x, y).A) / 255
				}
				if f.knockout {
					// 挖空组按形状而非源 alpha 移除旧内容；新颜色与初始背景混合，
					// 再加入未被新形状覆盖的旧内容，不能把旧内容衰减两次。
					initial := f.initial.NRGBAAt(x, y)
					af, ai := float64(b.A)/255, float64(initial.A)/255
					t := float64(coverage)
					as := float64(a)
					mixed := transparencySource(initial, s, p.state.blendMode)
					outAlpha := (1-t)*af + (t-as)*ai + as
					channel := func(c, ic, sc byte) byte {
						if outAlpha == 0 {
							return 0
						}
						return transparencyByte(((1-t)*af*float64(c)/255 + (t-as)*ai*float64(ic)/255 + as*float64(sc)/255) / outAlpha)
					}
					f.result.SetNRGBA(x, y, color.NRGBA{R: channel(b.R, initial.R, mixed.R), G: channel(b.G, initial.G, mixed.G), B: channel(b.B, initial.B, mixed.B), A: transparencyByte(outAlpha)})
					f.alpha[i] = a + f.alpha[i]*(1-coverage)
				} else {
					f.result.SetNRGBA(x, y, transparencyOver(b, transparencySource(b, s, p.state.blendMode)))
					f.alpha[i] = a + f.alpha[i]*(1-a)
				}
				f.shape[i] = coverage + f.shape[i]*(1-coverage)
			}
		}
		if text, ok := item.(creator.Text); ok {
			f.texts = append(f.texts, p.invisibleTransparencyText(text))
		}
		return nil
	}
	if !isNormalBlendMode(p.state.blendMode) {
		background, err := p.transparencyBackdrop(r)
		if err != nil {
			return err
		}
		for y := r.Min.Y; y < r.Max.Y; y++ {
			if err := p.control.check(); err != nil {
				return err
			}
			for x := r.Min.X; x < r.Max.X; x++ {
				source.SetNRGBA(x, y, transparencySource(background.NRGBAAt(x, y), source.NRGBAAt(x, y), p.state.blendMode))
			}
		}
	}
	output, err := p.transparencyImage(source)
	if err != nil {
		return err
	}
	if !p.appendRawItem(output) {
		return p.control.check()
	}
	if text, ok := item.(creator.Text); ok {
		p.appendRawItem(p.invisibleTransparencyText(text))
	}
	return p.control.check()
}

// 单次 Normal 路径或着色绘制没有组内重叠，可以把外层透明度保留在该图元上。
// 只接受明确的操作符与无软掩码的 Normal gs，避免把嵌套内容误判为单次绘制。
func (p *pdfInterpreter) singleNormalFormPaint(content []byte, resources types.Dict) bool {
	tokens := newPDFContentTokenizer(content)
	tokens.control = p.control
	paints := 0
	var previous any
	for {
		value, ok, err := tokens.next()
		if err != nil {
			return false
		}
		if !ok {
			return paints == 1
		}
		if op, ok := value.(string); ok {
			switch op {
			case "f", "f*", "S", "s", "sh":
				paints++
				if paints > 1 {
					return false
				}
			case "gs":
				name := anyName(previous)
				states, ok := dereferencedSubDict(p.ctx, resources, "ExtGState")
				if !ok {
					return false
				}
				dict, err := p.ctx.XRefTable.DereferenceDict(states[name])
				if err != nil || dict == nil {
					return false
				}
				if bm := dict["BM"]; bm != nil {
					mode, ok := dereferenceName(p.ctx, bm)
					if !ok || !isNormalBlendMode(mode) {
						return false
					}
				}
				if mask := dict["SMask"]; mask != nil {
					name, ok := dereferenceName(p.ctx, mask)
					if !ok || name != "None" {
						return false
					}
				}
			case "q", "Q", "cm", "g", "G", "rg", "RG", "k", "K", "cs", "CS", "sc", "SC", "m", "l", "c", "v", "y", "h", "re", "W", "W*", "n", "w", "J", "j", "M", "d", "BX", "EX":
			default:
				return false
			}
		}
		previous = value
	}
}

func (p *pdfInterpreter) beginTransparencyGroup(stream *types.StreamDict, resources types.Dict, state pdfGraphicsState) (*pdfTransparencyGroup, error) {
	group, err := p.ctx.XRefTable.DereferenceDict(stream.Dict["Group"])
	if err != nil || group == nil || group.NameEntry("S") == nil || *group.NameEntry("S") != "Transparency" {
		return nil, nil
	}
	isolation, _ := dereferencePDFObject(p.ctx, group["I"])
	knockout, _ := dereferencePDFObject(p.ctx, group["K"])
	isolated, _ := isolation.(types.Boolean)
	knockoutFlag, _ := knockout.(types.Boolean)
	// 非隔离、非挖空的 Normal 不透明组可以直接在原背景上展开；只有需要
	// 整组合成或与背景隔离的情况才分配栅格缓冲。
	if !bool(isolated) && !bool(knockoutFlag) && isNormalBlendMode(p.state.blendMode) && p.fillOpacity() >= 1 && p.state.softMask == nil && p.state.lumMask == nil && (p.transparency == nil || !p.transparency.knockout) {
		return nil, nil
	}
	if isNormalBlendMode(p.state.blendMode) && p.state.softMask == nil && p.state.lumMask == nil && p.singleNormalFormPaint(stream.Content, resources) {
		return nil, nil
	}
	p.report("graphics.group", "透明组以 300dpi 在 RGB 空间局部合成")
	if err := p.control.check(); err != nil {
		return nil, err
	}
	bbox := pdfNumberArray(p.ctx, stream.Dict["BBox"])
	if len(bbox) != 4 {
		return nil, fmt.Errorf("透明组 BBox 无效")
	}
	bounds := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, pt := range [][2]float64{{bbox[0], bbox[1]}, {bbox[0], bbox[3]}, {bbox[2], bbox[1]}, {bbox[2], bbox[3]}} {
		x, y := transformPDFPoint(pt[0], pt[1], state.ctm)
		x, y = p.pagePoint(x, y)
		bounds[0], bounds[1] = math.Min(bounds[0], x), math.Min(bounds[1], y)
		bounds[2], bounds[3] = math.Max(bounds[2], x), math.Max(bounds[3], y)
	}
	r, err := p.transparencyRect(bounds)
	if err != nil {
		return nil, err
	}
	if err := p.reserveTransparency(r, 4); err != nil {
		return nil, err
	}
	f := &pdfTransparencyGroup{rect: r, initial: image.NewNRGBA(r), result: image.NewNRGBA(r), alpha: make([]float32, r.Dx()*r.Dy()), shape: make([]float32, r.Dx()*r.Dy())}
	f.knockout = bool(knockoutFlag)
	if !bool(isolated) && !r.Empty() {
		f.initial, err = p.transparencyBackdrop(r)
		if err != nil {
			return nil, err
		}
		copy(f.result.Pix, f.initial.Pix)
	}
	return f, nil
}

func (p *pdfInterpreter) finishTransparencyGroup(f *pdfTransparencyGroup) error {
	if f.rect.Empty() {
		return nil
	}
	if err := p.reserveTransparency(f.rect, 2); err != nil {
		return err
	}
	output := image.NewNRGBA(f.rect)
	shape := image.NewNRGBA(f.rect)
	for y := f.rect.Min.Y; y < f.rect.Max.Y; y++ {
		if err := p.control.check(); err != nil {
			return err
		}
		for x := f.rect.Min.X; x < f.rect.Max.X; x++ {
			i := (y-f.rect.Min.Y)*f.rect.Dx() + x - f.rect.Min.X
			shape.SetNRGBA(x, y, color.NRGBA{A: transparencyByte(float64(f.shape[i]))})
			a := float64(f.alpha[i])
			if a == 0 {
				continue
			}
			b, c := f.initial.NRGBAAt(x, y), f.result.NRGBAAt(x, y)
			ab, ac := float64(b.A)/255, float64(c.A)/255
			channel := func(b, c byte) byte { return transparencyByte((float64(c)/255*ac - float64(b)/255*ab*(1-a)) / a) }
			output.SetNRGBA(x, y, color.NRGBA{R: channel(b.R, c.R), G: channel(b.G, c.G), B: channel(b.B, c.B), A: transparencyByte(a)})
			if p.state.constantMaskBlack {
				output.SetNRGBA(x, y, color.NRGBA{})
			}
		}
	}
	item, err := p.transparencyImage(output)
	if err != nil {
		return err
	}
	item.Alpha = ofdAlpha(p.fillOpacity())
	if item.Alpha != nil {
		for y := output.Rect.Min.Y; y < output.Rect.Max.Y; y++ {
			if err := p.control.check(); err != nil {
				return err
			}
			for x := output.Rect.Min.X; x < output.Rect.Max.X; x++ {
				c := output.NRGBAAt(x, y)
				c.A = byte((uint32(c.A)*uint32(*item.Alpha) + 127) / 255)
				output.SetNRGBA(x, y, c)
			}
		}
	}
	previousShape := p.transparencyShape
	previousRaster := p.transparencyRaster
	p.transparencyShape = shape
	p.transparencyRaster = output
	p.appendItem(item)
	p.transparencyShape = previousShape
	p.transparencyRaster = previousRaster
	for _, text := range f.texts {
		if p.transparency != nil {
			p.transparency.texts = append(p.transparency.texts, text)
		} else {
			p.appendRawItem(text)
		}
	}
	return p.control.check()
}
