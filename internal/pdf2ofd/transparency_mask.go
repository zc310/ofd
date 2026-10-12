package pdf2ofd

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/pkg/creator"
)

type pdfTransparencyMask struct {
	group      types.Object
	resources  types.Dict
	state      pdfGraphicsState
	depth      int
	luminosity bool
	background pdfColor
	transfer   [256]byte
	items      []creator.Item
	loaded     bool
	boundary   [4][2]float64
	reported   bool
}

func (p *pdfInterpreter) parseTransparencyMask(object types.Object, resources types.Dict) *pdfTransparencyMask {
	dict, err := p.ctx.XRefTable.DereferenceDict(object)
	if err != nil || dict == nil {
		return nil
	}
	stream, _, err := p.ctx.XRefTable.DereferenceStreamDict(dict["G"])
	if err != nil || stream == nil || stream.NameEntry("Subtype") == nil || *stream.NameEntry("Subtype") != "Form" {
		return nil
	}
	group, _ := p.ctx.XRefTable.DereferenceDict(stream.Dict["Group"])
	// 未声明透明组的旧式单图片包装继续使用图片掩码路径；正式透明组则保留
	// Form 的完整几何和资源状态。
	if group == nil || group.NameEntry("S") == nil || *group.NameEntry("S") != "Transparency" {
		return nil
	}
	if err := p.decodeStream(stream, 0); err != nil {
		p.report("graphics.mask", err.Error())
		return nil
	}
	if dict["BC"] == nil && dict["TR"] == nil && p.singleImageMaskForm(stream.Content) {
		if name, ok := dereferenceName(p.ctx, dict["S"]); ok && name == "Luminosity" {
			if candidate := pdfExtGStateSoftMask(p.ctx, types.Dict{"SMask": object}, p.control); candidate != nil && candidate.IntEntry("Width") != nil && *candidate.IntEntry("Width") == 1 && candidate.IntEntry("Height") != nil && *candidate.IntEntry("Height") == 1 && candidate.NameEntry("ColorSpace") != nil && *candidate.NameEntry("ColorSpace") == "DeviceGray" {
				if err := p.decodeStream(candidate, 0); err == nil && len(candidate.Content) == 1 && candidate.Content[0] == 0 && candidate.Dict["Decode"] == nil {
					p.state.constantMaskBlack = true
					return nil
				}
			}
		}
	}
	kind, ok := dereferenceName(p.ctx, dict["S"])
	if !ok || (kind != "Alpha" && kind != "Luminosity") {
		p.report("graphics.mask", "软掩码 S 无效")
		return nil
	}
	mask := &pdfTransparencyMask{group: dict["G"], resources: resources, state: p.state, depth: p.parseDepth + 1, luminosity: kind == "Luminosity"}
	for i := range mask.transfer {
		mask.transfer[i] = byte(i)
	}
	matrix := identityPDFMatrix()
	if values := pdfNumberArray(p.ctx, stream.Dict["Matrix"]); len(values) == 6 {
		copy(matrix[:], values)
	}
	ctm := multiplyPDFMatrix(p.state.ctm, matrix)
	if bbox := pdfNumberArray(p.ctx, stream.Dict["BBox"]); len(bbox) == 4 {
		for i, pt := range [4][2]float64{{bbox[0], bbox[1]}, {bbox[2], bbox[1]}, {bbox[2], bbox[3]}, {bbox[0], bbox[3]}} {
			x, y := transformPDFPoint(pt[0], pt[1], ctm)
			mask.boundary[i][0], mask.boundary[i][1] = p.pagePoint(x, y)
		}
	}
	mask.state.softMask, mask.state.lumMask = nil, nil
	mask.state.fillAlpha, mask.state.strokeAlpha, mask.state.groupAlpha = 1, 1, 1
	mask.state.blendMode, mask.state.clips = "Normal", nil
	space := p.parseColorSpace(group["CS"], 0)
	bc := pdfNumberArray(p.ctx, dict["BC"])
	if len(bc) == 0 && space != nil {
		bc = make([]float64, space.components)
	}
	if len(bc) > 0 {
		if space == nil || len(bc) != space.components {
			p.report("graphics.mask", "软掩码 BC 与颜色空间不匹配")
		} else {
			var ok bool
			mask.background, ok = space.colorFromComponents(bc)
			if !ok {
				p.report("graphics.mask", "软掩码 BC 无法转换")
			}
		}
	}
	var function *pdfFunction
	if tr := dict["TR"]; tr != nil {
		if name, ok := dereferenceName(p.ctx, tr); !ok || name != "Identity" {
			function = p.parseFunction(tr, 0)
			if function == nil {
				p.report("graphics.mask", "软掩码 TR 无法解析")
			}
		}
	}
	if err := p.control.consumeOperations(256); err != nil {
		return mask
	}
	for i := range mask.transfer {
		v := float64(i) / 255
		if function != nil {
			values, ok := function.eval([]float64{v})
			if !ok || len(values) != 1 {
				p.report("graphics.mask", "软掩码 TR 必须是单输入单输出函数")
				return mask
			}
			v = values[0]
			if len(function.rng) == 2 {
				v = math.Max(function.rng[0], math.Min(function.rng[1], v))
			}
		}
		mask.transfer[i] = transparencyByte(v)
	}
	return mask
}

func (p *pdfInterpreter) applyTransparencyMask(img *image.NRGBA, mask *pdfTransparencyMask) error {
	if !mask.loaded {
		page := creator.Page{Area: p.page.Area}
		child := newPDFInterpreter(p.ctx, &page, p.document, p.info, p.fontCache, p.imageCache)
		child.control, child.source, child.state = p.control, p.source, mask.state
		resources := types.Dict{"XObject": types.Dict{"Mask": mask.group}}
		// 掩码使用自己的资源字典，缺省时沿用 gs 时的资源；CTM 在 gs 时捕获，
		// 不随后续图元的变换重新定位。
		for key, value := range mask.resources {
			if key != "XObject" {
				resources[key] = value
			}
		}
		if original, ok := dereferencedSubDict(p.ctx, mask.resources, "XObject"); ok {
			objects := types.Dict{}
			for key, value := range original {
				objects[key] = value
			}
			objects["Mask"] = mask.group
			resources["XObject"] = objects
		}
		if err := child.xobject("Mask", resources, mask.depth); err != nil {
			return err
		}
		mask.items, mask.loaded = page.Items, true
	}
	raster, err := p.rasterTransparency(mask.items, img.Rect)
	if err != nil {
		return fmt.Errorf("渲染软掩码: %w", err)
	}
	bc := color.NRGBA{R: mask.background.r, G: mask.background.g, B: mask.background.b, A: 255}
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		if err := p.control.check(); err != nil {
			return err
		}
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			m := raster.NRGBAAt(x, y)
			value := m.A
			if mask.luminosity {
				c := transparencyOver(bc, m)
				value = transparencyByte(blendLum(transparencyRGB(c)))
			}
			s := img.NRGBAAt(x, y)
			alpha := mask.transfer[value]
			if !mask.contains(float64(x)+.5, float64(y)+.5) {
				// 组边界外使用初始背景的常量掩码值，TR 只作用于组内。
				alpha = 0
				if mask.luminosity {
					alpha = transparencyByte(blendLum(transparencyRGB(bc)))
				}
			}
			s.A = byte((uint32(s.A)*uint32(alpha) + 127) / 255)
			img.SetNRGBA(x, y, s)
		}
	}
	return p.control.check()
}

func (m *pdfTransparencyMask) contains(x, y float64) bool {
	x, y = x/transparencyDPMM, y/transparencyDPMM
	positive, negative := false, false
	for i, a := range m.boundary {
		b := m.boundary[(i+1)%4]
		cross := (b[0]-a[0])*(y-a[1]) - (b[1]-a[1])*(x-a[0])
		positive = positive || cross > 0
		negative = negative || cross < 0
	}
	return positive != negative
}

// 只有一次 Do、没有其它绘制或 gs 的包装才能使用恒黑图片掩码的快速路径。
func (p *pdfInterpreter) singleImageMaskForm(content []byte) bool {
	tokens := newPDFContentTokenizer(content)
	tokens.control = p.control
	draws := 0
	for {
		value, ok, err := tokens.next()
		if err != nil {
			return false
		}
		if !ok {
			return draws == 1
		}
		if op, ok := value.(string); ok {
			switch op {
			case "Do":
				draws++
				if draws > 1 {
					return false
				}
			case "q", "Q", "cm":
			default:
				return false
			}
		}
	}
}
