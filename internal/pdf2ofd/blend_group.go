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
	_ "github.com/zc310/ofd/internal/render/backends/canvas"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/pkg/creator"
)

func (p *pdfInterpreter) isMultiplyShadingForm(stream *types.StreamDict, resources types.Dict) bool {
	if !strings.EqualFold(p.state.blendMode, "Multiply") {
		return false
	}
	group, err := p.ctx.XRefTable.DereferenceDict(stream.Dict["Group"])
	if err != nil || group == nil || group.NameEntry("S") == nil || *group.NameEntry("S") != "Transparency" {
		return false
	}
	name, ok := singleFormShading(stream.Content, p.control)
	if !ok {
		return false
	}
	shadings, ok := dereferencedSubDict(p.ctx, resources, "Shading")
	if !ok {
		return false
	}
	shading, err := p.ctx.XRefTable.DereferenceDict(shadings[name])
	if err != nil || shading == nil {
		return false
	}
	kind, _ := dereferencedPDFNumber(p.ctx, shading["ShadingType"])
	return kind == 2 || kind == 3
}

// singleFormShading 只识别含一次 sh、没有其他绘制操作的 Form。背景依赖的
// 混合不能靠改色标表达；这里只识别单次着色，其他透明组仍按现有近似处理。
func singleFormShading(content []byte, control *conversionControl) (string, bool) {
	tokens := newPDFContentTokenizer(content)
	tokens.control = control
	shadings := 0
	name := ""
	var previous any
	for {
		value, ok, err := tokens.next()
		if err != nil {
			return "", false
		}
		if !ok {
			return name, shadings == 1
		}
		if op, ok := value.(string); ok {
			switch op {
			case "sh":
				shadings++
				resource, ok := previous.(pdfName)
				if !ok {
					return "", false
				}
				name = string(resource)
			case "q", "Q", "gs", "cm", "g", "G", "rg", "RG", "k", "K", "cs", "CS", "sc", "SC", "scn", "SCN", "m", "l", "c", "v", "y", "h", "re", "W", "W*", "n", "BX", "EX":
			default:
				return "", false
			}
		}
		previous = value
	}
}

// multiplyShadingImage 以 300dpi 在当前已转换的背景上合成一个 Multiply 着色组。
// 只栅格化着色边界内的区域；输出 PNG 的 alpha 保留形状，区域外仍由原矢量图元绘制。
// 颜色使用当前转换链的 sRGB，因此仍报告混合近似诊断，Strict 保持原有失败语义。
func (p *pdfInterpreter) multiplyShadingImage(path creator.Path, backdrop []creator.Item, opacity float64) (*creator.Image, error) {
	if p.page.Area == nil || p.page.Area.PhysicalBox == nil {
		return nil, fmt.Errorf("着色组缺少页面尺寸")
	}
	pageBox := p.page.Area.PhysicalBox
	left, top := math.Max(0, path.X), math.Max(0, path.Y)
	right, bottom := math.Min(pageBox.Width, path.X+path.Width), math.Min(pageBox.Height, path.Y+path.Height)
	if left >= right || top >= bottom {
		return nil, nil
	}
	resolution := geom.DPI(300)
	width, height := math.Ceil((right-left)*resolution.DPMM()), math.Ceil((bottom-top)*resolution.DPMM())
	if !calculatorFinite(width) || !calculatorFinite(height) || width >= 0x1p63 || height >= 0x1p63 {
		return nil, fmt.Errorf("着色组栅格尺寸无效")
	}
	if p.control != nil {
		pixels, err := p.control.imagePixels(int64(width), int64(height))
		if err != nil {
			return nil, err
		}
		// 背景、着色及合成输出三张缓冲；在任何栅格分配之前检查。
		for i := 0; i < 3; i++ {
			if err := p.control.addImagePixels(pixels); err != nil {
				return nil, err
			}
		}
		if err := p.control.consumeOperations(pixels); err != nil {
			return nil, err
		}
	}
	// 像素密度取整后调整表面尺寸，确保每个栅格像素与输出毫米坐标一一对应。
	w, h := width/resolution.DPMM(), height/resolution.DPMM()
	backPage := creator.Page{Area: p.page.Area, Items: backdrop}
	groupPage := creator.Page{Area: p.page.Area, Items: []creator.Item{path}}
	snapshot := creator.Document{ID: "pdf-blend-snapshot", PageSize: creator.PageSize{Width: pageBox.Width, Height: pageBox.Height}, Pages: []creator.Page{backPage, groupPage}}
	if p.document != nil {
		snapshot.Fonts = p.document.Fonts
		snapshot.DrawParams = p.document.DrawParams
	}
	if p.control != nil {
		for _, item := range backdrop {
			work := int64(1)
			switch value := item.(type) {
			case creator.Path:
				work += int64(strings.Count(value.Data, " "))
			case creator.Image:
				// 快照重新解码背景图片，不能复用导入阶段的像素计数。
				if err := p.reserveImageDecode(value.Data); err != nil {
					return nil, err
				}
			}
			if err := p.control.consumeOperations(work); err != nil {
				return nil, err
			}
		}
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
	document := render.NewDocumentWithDPI(color.Transparent, ofd.Documents[0], resolution)
	defer document.Close()
	raster := func(index int) (*image.RGBA, error) {
		var drawErr error
		surface, err := render.NewVectorSurface(w, h, func(ctx render.DrawContext) {
			ctx.Translate(-left, h+top-pageBox.Height)
			drawErr = document.Draw(ctx, ofd.Documents[0].Pages[index])
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
		result := surface.Rasterize(resolution)
		return result, p.control.check()
	}
	background, err := raster(0)
	if err != nil {
		return nil, err
	}
	foreground, err := raster(1)
	if err != nil {
		return nil, err
	}
	output := image.NewNRGBA(foreground.Bounds())
	for y := output.Rect.Min.Y; y < output.Rect.Max.Y; y++ {
		if err := p.control.check(); err != nil {
			return nil, err
		}
		for x := output.Rect.Min.X; x < output.Rect.Max.X; x++ {
			source := color.NRGBAModel.Convert(foreground.RGBAAt(x, y)).(color.NRGBA)
			base := color.NRGBAModel.Convert(background.RGBAAt(x, y)).(color.NRGBA)
			// 背景未覆盖的区域以透明参与混合，不能把其零 RGB 当作黑色。
			channel := func(s, b byte) byte {
				return byte((uint32(s)*(uint32(b)*uint32(base.A)+255*uint32(255-base.A)) + 32512) / 65025)
			}
			output.SetNRGBA(x, y, color.NRGBA{R: channel(source.R, base.R), G: channel(source.G, base.G), B: channel(source.B, base.B), A: byte(math.Round(float64(source.A) * clampOpacity(opacity)))})
		}
	}
	encoded, _, err := encodePNGImage(output)
	if err != nil {
		return nil, err
	}
	return &creator.Image{X: left, Y: top, Width: w, Height: h, CTM: &creator.CTM{w, 0, 0, h, 0, 0}, Data: encoded, Format: "PNG"}, p.control.check()
}
