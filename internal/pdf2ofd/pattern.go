package pdf2ofd

import (
	"bytes"
	"image"
	"image/png"
	"math"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/pkg/creator"
)

// maxTilingPatternTiles 限制一次图案填充展开的图块数量，避免异常步长导致
// 数量爆炸。超过该数量时改为把图案合成为单张图片。
const maxTilingPatternTiles = 4096

// maxTilingComposedPixels 限制合成平铺图案图片的最大像素数，避免高密度图案
// 生成超大位图。
const maxTilingComposedPixels = 16 << 20

// pdfPatternPaint 是 scn/SCN 在 Pattern 颜色空间下选中的图案填充：渐变着色
// （PatternType 2）或平铺图案（PatternType 1）。
type pdfPatternPaint struct {
	shading *pdfShading
	tiling  *pdfTilingPattern
}

// pdfTilingPattern 保存 PatternType 1（平铺图案）用于填充的原始信息。
type pdfTilingPattern struct {
	content   []byte
	resources types.Dict
	// matrix 把图案空间映射到页面默认坐标空间。
	matrix       [6]float64
	xstep, ystep float64
	bbox         [4]float64
}

// resolvePattern 解析 Pattern 颜色空间中的图案。PatternType 2 返回渐变着色，
// PatternType 1 返回平铺图案；其他类型返回 nil 以回退到纯色填充。
func (p *pdfInterpreter) resolvePattern(resources types.Dict, name string) *pdfPatternPaint {
	if resources == nil || name == "" {
		return nil
	}
	patterns, ok := dereferencedSubDict(p.ctx, resources, "Pattern")
	if !ok {
		return nil
	}
	raw, found := patterns.Find(name)
	if !found {
		return nil
	}
	resolved, err := dereferencePDFObject(p.ctx, raw)
	if err != nil {
		return nil
	}
	var dict types.Dict
	var stream *types.StreamDict
	switch value := resolved.(type) {
	case types.Dict:
		dict = value
	case types.StreamDict:
		copy := value
		dict, stream = value.Dict, &copy
	case *types.StreamDict:
		if value == nil {
			return nil
		}
		dict, stream = value.Dict, value
	default:
		return nil
	}
	patternType, _ := dereferencedPDFNumber(p.ctx, dict["PatternType"])
	switch patternType {
	case 2:
		shadingObject, found := dict.Find("Shading")
		if !found {
			return nil
		}
		shading := p.parseShading(shadingObject, 0)
		if shading == nil {
			return nil
		}
		shading.pattern = true
		if matrix := pdfNumberArray(p.ctx, dict["Matrix"]); len(matrix) == 6 {
			shading.matrix = [6]float64{matrix[0], matrix[1], matrix[2], matrix[3], matrix[4], matrix[5]}
		}
		return &pdfPatternPaint{shading: shading}
	case 1:
		if stream == nil || stream.Decode() != nil || len(stream.Content) == 0 {
			return nil
		}
		tiling := &pdfTilingPattern{
			content: append([]byte(nil), stream.Content...),
			matrix:  identityPDFMatrix(),
		}
		if matrix := pdfNumberArray(p.ctx, dict["Matrix"]); len(matrix) == 6 {
			tiling.matrix = [6]float64{matrix[0], matrix[1], matrix[2], matrix[3], matrix[4], matrix[5]}
		}
		if bbox := pdfNumberArray(p.ctx, dict["BBox"]); len(bbox) == 4 {
			tiling.bbox = [4]float64{bbox[0], bbox[1], bbox[2], bbox[3]}
		}
		tiling.xstep, _ = dereferencedPDFNumber(p.ctx, dict["XStep"])
		tiling.ystep, _ = dereferencedPDFNumber(p.ctx, dict["YStep"])
		if object, found := stream.Find("Resources"); found {
			if value, err := p.ctx.XRefTable.DereferenceDict(object); err == nil {
				tiling.resources = value
			}
		}
		return &pdfPatternPaint{tiling: tiling}
	default:
		return nil
	}
}

// imagePlacement 是平铺图案单元中单个图像 XObject 的放置。
type imagePlacement struct {
	name string
	ctm  [6]float64
}

// singlePatternImage 提取平铺图案内容中唯一的图像 XObject 及其放置矩阵。
// 内容里出现其他绘制操作时返回 false，调用方回退到纯色填充。
func singlePatternImage(content []byte) (imagePlacement, bool) {
	tokens := newPDFContentTokenizer(content)
	var placement imagePlacement
	found := false
	ctm := identityPDFMatrix()
	var stack [][6]float64
	operands := []any{}
	for {
		value, ok, err := tokens.next()
		if err != nil || !ok {
			break
		}
		operator, isOperator := value.(string)
		if !isOperator || strings.HasPrefix(operator, "/") {
			operands = append(operands, value)
			continue
		}
		switch operator {
		case "q":
			stack = append(stack, ctm)
		case "Q":
			if len(stack) > 0 {
				ctm = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
		case "cm":
			if len(operands) >= 6 {
				matrix := [6]float64{anyFloat(operands[0]), anyFloat(operands[1]), anyFloat(operands[2]), anyFloat(operands[3]), anyFloat(operands[4]), anyFloat(operands[5])}
				ctm = multiplyPDFMatrix(ctm, matrix)
			}
		case "Do":
			if found {
				return imagePlacement{}, false
			}
			name := ""
			if len(operands) > 0 {
				name = anyName(operands[len(operands)-1])
			}
			placement = imagePlacement{name: name, ctm: ctm}
			found = true
		case "BT", "ET", "m", "l", "c", "v", "y", "re", "h", "S", "s", "f", "F", "f*", "B", "B*", "b", "b*", "sh", "BI":
			// 图案单元包含图像以外的绘制内容，暂不支持。
			return imagePlacement{}, false
		}
		operands = operands[:0]
	}
	if !found || placement.name == "" {
		return imagePlacement{}, false
	}
	return placement, true
}

// emitTilingFill 把平铺图案填充展开为若干图片对象，实现 PDF PatternType 1
// 背景图的转换。仅支持单元内容为单个图像且页面未旋转的情形。
func (p *pdfInterpreter) emitTilingFill(pattern *pdfTilingPattern, fillCommands []pdfPathCommand, fillBounds [4]float64) bool {
	if pattern == nil || p.page == nil || p.info.rotate != 0 || p.info.userUnit != 1 {
		return false
	}
	placement, ok := singlePatternImage(pattern.content)
	if !ok {
		return false
	}
	xobjects, ok := dereferencedSubDict(p.ctx, pattern.resources, "XObject")
	if !ok {
		return false
	}
	object, found := xobjects.Find(placement.name)
	if !found {
		return false
	}
	stream, _, err := p.ctx.XRefTable.DereferenceStreamDict(object)
	if err != nil || stream == nil || stream.NameEntry("Subtype") == nil || *stream.NameEntry("Subtype") != "Image" {
		return false
	}
	data, format, err := pdfImageData(p.ctx, stream, pdfColor{})
	if err != nil {
		return false
	}

	// 图案空间 -> 页面毫米坐标（仅处理未旋转页面、userUnit=1）。
	k := pdfPointToMillimeter
	m := pattern.matrix
	toPage := [6]float64{
		k * m[0], -k * m[1],
		k * m[2], -k * m[3],
		k * (m[4] - p.info.minX), k * (p.info.maxY - m[5]),
	}
	inverse, ok := invertAffineMatrix(toPage)
	if !ok {
		return false
	}

	// 图片单元在图案空间中的范围。
	imgMinX, imgMinY, imgMaxX, imgMaxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, corner := range [][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		x, y := transformPDFPoint(corner[0], corner[1], placement.ctm)
		imgMinX, imgMinY = math.Min(imgMinX, x), math.Min(imgMinY, y)
		imgMaxX, imgMaxY = math.Max(imgMaxX, x), math.Max(imgMaxY, y)
	}
	xstep, ystep := pattern.xstep, pattern.ystep
	if xstep <= 0 {
		xstep = pattern.bbox[2] - pattern.bbox[0]
	}
	if ystep <= 0 {
		ystep = pattern.bbox[3] - pattern.bbox[1]
	}
	if xstep <= 0 || ystep <= 0 {
		return false
	}

	// 填充范围在图案空间中的包围盒。
	pMinX, pMinY, pMaxX, pMaxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, corner := range [][2]float64{
		{fillBounds[0], fillBounds[1]},
		{fillBounds[2], fillBounds[1]},
		{fillBounds[0], fillBounds[3]},
		{fillBounds[2], fillBounds[3]},
	} {
		x, y := transformPDFPoint(corner[0], corner[1], inverse)
		pMinX, pMinY = math.Min(pMinX, x), math.Min(pMinY, y)
		pMaxX, pMaxY = math.Max(pMaxX, x), math.Max(pMaxY, y)
	}
	// 只保留与填充范围有正面积重叠的图块：ix*xstep+imgMin < pMax 且
	// ix*xstep+imgMax > pMin。
	startX := int(math.Floor((pMinX-imgMaxX)/xstep)) + 1
	endX := int(math.Ceil((pMaxX-imgMinX)/xstep)) - 1
	startY := int(math.Floor((pMinY-imgMaxY)/ystep)) + 1
	endY := int(math.Ceil((pMaxY-imgMinY)/ystep)) - 1
	countX, countY := endX-startX+1, endY-startY+1
	if countX <= 0 || countY <= 0 {
		return false
	}
	// 图块数量过多（例如整页 5x5pt 纹理背景）时不逐个输出图片对象，
	// 而是把图案合成为一张覆盖填充范围的位图，避免生成上万个小对象。
	if countX*countY > maxTilingPatternTiles {
		return p.emitComposedTilingImage(pattern, placement, data, toPage, inverse, fillCommands, fillBounds)
	}

	for ix := startX; ix <= endX; ix++ {
		for iy := startY; iy <= endY; iy++ {
			offsetX := float64(ix)*xstep + imgMinX
			offsetY := float64(iy)*ystep + imgMinY
			minXmm, minYmm, maxXmm, maxYmm := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
			for _, corner := range [][2]float64{
				{offsetX, offsetY},
				{offsetX + (imgMaxX - imgMinX), offsetY},
				{offsetX, offsetY + (imgMaxY - imgMinY)},
				{offsetX + (imgMaxX - imgMinX), offsetY + (imgMaxY - imgMinY)},
			} {
				x, y := transformPDFPoint(corner[0], corner[1], toPage)
				minXmm, minYmm = math.Min(minXmm, x), math.Min(minYmm, y)
				maxXmm, maxYmm = math.Max(maxXmm, x), math.Max(maxYmm, y)
			}
			width, height := maxXmm-minXmm, maxYmm-minYmm
			if width <= 0 || height <= 0 {
				continue
			}
			image := creator.Image{X: minXmm, Y: minYmm, Width: width, Height: height, Data: data, Format: format}
			if clips := p.buildPathClip(fillCommands, minXmm, minYmm, true, width, height); clips != nil {
				image.Clips = clips
			}
			p.page.Items = append(p.page.Items, image)
		}
	}
	return true
}

// emitComposedTilingImage 把平铺图案合成为一张覆盖填充范围的位图并输出为单个
// 图片对象，用于图块数量超过 maxTilingPatternTiles 的高密度图案（例如整页的
// 小尺寸纹理背景）。仅处理轴对齐（无旋转/错切）的图案与图像放置。
func (p *pdfInterpreter) emitComposedTilingImage(pattern *pdfTilingPattern, placement imagePlacement, data []byte, toPage, inverse [6]float64, fillCommands []pdfPathCommand, fillBounds [4]float64) bool {
	if placement.ctm[1] != 0 || placement.ctm[2] != 0 || toPage[1] != 0 || toPage[2] != 0 {
		return false
	}
	a, d := placement.ctm[0], placement.ctm[3]
	if a == 0 || d == 0 {
		return false
	}
	xstep, ystep := pattern.xstep, pattern.ystep
	if xstep <= 0 {
		xstep = pattern.bbox[2] - pattern.bbox[0]
	}
	if ystep <= 0 {
		ystep = pattern.bbox[3] - pattern.bbox[1]
	}
	if xstep <= 0 || ystep <= 0 {
		return false
	}
	tile, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return false
	}
	tileWidth, tileHeight := tile.Bounds().Dx(), tile.Bounds().Dy()
	if tileWidth <= 0 || tileHeight <= 0 {
		return false
	}
	e, f := placement.ctm[4], placement.ctm[5]
	tileOriginX := math.Min(e, e+a)
	tileOriginY := math.Min(f, f+d)

	// 填充范围在图案空间中的包围盒。
	pMinX, pMinY, pMaxX, pMaxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, corner := range [][2]float64{
		{fillBounds[0], fillBounds[1]},
		{fillBounds[2], fillBounds[1]},
		{fillBounds[0], fillBounds[3]},
		{fillBounds[2], fillBounds[3]},
	} {
		x, y := transformPDFPoint(corner[0], corner[1], inverse)
		pMinX, pMinY = math.Min(pMinX, x), math.Min(pMinY, y)
		pMaxX, pMaxY = math.Max(pMaxX, x), math.Max(pMaxY, y)
	}
	if !(pMaxX > pMinX) || !(pMaxY > pMinY) {
		return false
	}

	// 按图案单元的原生像素密度生成覆盖填充范围的位图。
	outWidth := int(math.Ceil((pMaxX - pMinX) * float64(tileWidth) / math.Abs(a)))
	outHeight := int(math.Ceil((pMaxY - pMinY) * float64(tileHeight) / math.Abs(d)))
	if outWidth <= 0 || outHeight <= 0 || int64(outWidth)*int64(outHeight) > maxTilingComposedPixels {
		return false
	}

	widthMM := fillBounds[2] - fillBounds[0]
	heightMM := fillBounds[3] - fillBounds[1]
	if widthMM <= 0 || heightMM <= 0 {
		return false
	}
	stepX := widthMM / float64(outWidth)
	stepY := heightMM / float64(outHeight)
	bounds := tile.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, outWidth, outHeight))
	for oy := 0; oy < outHeight; oy++ {
		mmY := fillBounds[1] + (float64(oy)+0.5)*stepY
		for ox := 0; ox < outWidth; ox++ {
			mmX := fillBounds[0] + (float64(ox)+0.5)*stepX
			px, py := transformPDFPoint(mmX, mmY, inverse)
			// 图案单元内的局部坐标；超出单元图像范围（存在间隙）时保持透明。
			lx := math.Mod(px-tileOriginX, xstep)
			if lx < 0 {
				lx += xstep
			}
			ly := math.Mod(py-tileOriginY, ystep)
			if ly < 0 {
				ly += ystep
			}
			unitX := (tileOriginX + lx - e) / a
			unitY := (tileOriginY + ly - f) / d
			if unitX < 0 || unitX >= 1 || unitY < 0 || unitY >= 1 {
				continue
			}
			col := clampInt(int(unitX*float64(tileWidth)), 0, tileWidth-1)
			row := clampInt(int((1-unitY)*float64(tileHeight)), 0, tileHeight-1)
			r, g, b, alpha := tile.At(bounds.Min.X+col, bounds.Min.Y+row).RGBA()
			target := rgba.PixOffset(ox, oy)
			rgba.Pix[target], rgba.Pix[target+1], rgba.Pix[target+2], rgba.Pix[target+3] = uint8(r>>8), uint8(g>>8), uint8(b>>8), uint8(alpha>>8)
		}
	}

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, rgba); err != nil {
		return false
	}
	img := creator.Image{X: fillBounds[0], Y: fillBounds[1], Width: widthMM, Height: heightMM, Data: encoded.Bytes(), Format: "PNG"}
	if clips := p.buildPathClip(fillCommands, fillBounds[0], fillBounds[1], true, widthMM, heightMM); clips != nil {
		img.Clips = clips
	}
	p.page.Items = append(p.page.Items, img)
	return true
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

// buildPathClip 把一条设备坐标路径转换为单个 OFD 裁剪区域。
func (p *pdfInterpreter) buildPathClip(commands []pdfPathCommand, objX, objY float64, isImage bool, width, height float64) *creator.Clips {
	if len(commands) == 0 {
		return nil
	}
	clipPath, ok := p.clipPathFor(pdfClipRegion{commands: commands}, objX, objY)
	if !ok {
		return nil
	}
	area := creator.ClipArea{Path: clipPath}
	if isImage && width > 0 && height > 0 {
		area.CTM = &creator.CTM{1 / width, 0, 0, 1 / height, 0, 0}
	}
	return &creator.Clips{Items: []creator.Clip{{Areas: []creator.ClipArea{area}}}}
}

// invertAffineMatrix 求 2x3 仿射矩阵的逆矩阵。
func invertAffineMatrix(matrix [6]float64) ([6]float64, bool) {
	determinant := matrix[0]*matrix[3] - matrix[1]*matrix[2]
	if determinant == 0 || math.IsNaN(determinant) || math.IsInf(determinant, 0) {
		return [6]float64{}, false
	}
	return [6]float64{
		matrix[3] / determinant,
		-matrix[1] / determinant,
		-matrix[2] / determinant,
		matrix[0] / determinant,
		(matrix[2]*matrix[5] - matrix[3]*matrix[4]) / determinant,
		(matrix[1]*matrix[4] - matrix[0]*matrix[5]) / determinant,
	}, true
}
