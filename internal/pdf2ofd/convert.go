// Package pdf2ofd 提供纯 Go 的 PDF 到 OFD 转换实现：解析 PDF 页面几何、文字、
// 路径和图像 XObject，并使用 pkg/creator 输出 OFD 包，不依赖原生 PDF 渲染器。
package pdf2ofd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/pkg/creator"
)

// Convert 把 PDF 输入（文件路径、[]byte、io.Reader 或 io.ReadSeeker）转换为
// OFD 包。转换保留基本页面几何、文字、路径和 JPEG/PNG 图像 XObject，
// 不使用原生 PDF 渲染器。
func Convert(input any, output io.Writer) error {
	data, err := readPDFInput(input)
	if err != nil {
		return err
	}
	if output == nil {
		return errors.New("OFD 输出写入器为空")
	}
	err = pdfToOFDBytes(data, output)
	if err != nil && strings.Contains(err.Error(), "PDF 解析器异常") {
		if repaired, repairErr := repairPDFXRef(data); repairErr == nil {
			return pdfToOFDBytes(repaired, output)
		}
	}
	return err
}

func pdfToOFDBytes(data []byte, output io.Writer) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("解析 PDF 失败: %v", recovered)
		}
	}()
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	ctx, err, panicked := readPDFContext(data, conf)
	if panicked {
		if repaired, repairErr := repairPDFXRef(data); repairErr == nil {
			ctx, err, _ = readPDFContext(repaired, conf)
		}
	}
	if err != nil {
		return fmt.Errorf("读取 PDF 失败: %w", err)
	}
	if err := api.ValidateContext(ctx); err != nil {
		// 部分可正常读取的 PDF 带有损坏的历史 Info 日期。保留已提取的
		// 元数据，去掉不可用的 Info 引用后重新校验页面和内容对象。
		ctx.Info = nil
		if retryErr := api.ValidateContext(ctx); retryErr != nil {
			return fmt.Errorf("验证 PDF 失败: %w", err)
		}
	}
	if ctx.PageCount == 0 {
		return errors.New("PDF 没有页面")
	}

	creatorName, creatorVersion, customDatas := pdfDocumentMetadata(ctx.Creator)
	document := creator.Document{
		ID:             "pdf-converted",
		Title:          ctx.Title,
		Author:         ctx.Author,
		Subject:        ctx.Subject,
		Creator:        creatorName,
		CreatorVersion: creatorVersion,
		CustomDatas:    customDatas,
		PageSize:       creator.A4,
		Pages:          make([]creator.Page, 0, ctx.PageCount),
	}
	for pageNumber := 1; pageNumber <= ctx.PageCount; pageNumber++ {
		page, err := convertPDFPage(ctx, pageNumber, &document)
		if err != nil {
			return fmt.Errorf("转换 PDF 第 %d 页失败: %w", pageNumber, err)
		}
		document.Pages = append(document.Pages, page)
	}
	if len(document.Pages) > 0 {
		document.PageSize = creator.PageSize{Width: document.Pages[0].Area.PhysicalBox.Width, Height: document.Pages[0].Area.PhysicalBox.Height}
	}
	// PDF 目录大纲转换为 OFD 大纲；原文档要求显示大纲面板时同步设置显示偏好。
	document.Outlines = convertOutlines(ctx)
	if len(document.Outlines) > 0 && pdfPageModeUseOutlines(ctx) {
		document.Preferences = &creator.ViewPreferences{PageMode: creator.PageModeUseOutlines}
	}
	return creator.CreateWithOptions(document, output, creator.CreateOptions{PreserveEmbeddedFonts: true})
}

func readPDFContext(data []byte, conf *model.Configuration) (ctx *model.Context, err error, panicked bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("PDF 解析器异常: %v", recovered)
			panicked = true
		}
	}()
	ctx, err = api.ReadContext(bytes.NewReader(data), conf)
	return ctx, err, false
}

// ConvertFile 是 Convert 的按路径便捷形式。
func ConvertFile(pdfPath, ofdPath string) error {
	if strings.TrimSpace(ofdPath) == "" {
		return errors.New("OFD 输出文件名为空")
	}
	inputInfo, inputErr := os.Stat(pdfPath)
	if inputErr != nil {
		return fmt.Errorf("读取 PDF 文件失败: %w", inputErr)
	}
	if outputInfo, outputErr := os.Stat(ofdPath); outputErr == nil && os.SameFile(inputInfo, outputInfo) {
		return errors.New("PDF 输入文件和 OFD 输出文件不能相同")
	}
	outputDir := filepath.Dir(ofdPath)
	file, err := os.CreateTemp(outputDir, "."+filepath.Base(ofdPath)+".tmp-")
	if err != nil {
		return fmt.Errorf("创建临时 OFD 文件失败: %w", err)
	}
	tempPath := file.Name()
	removeTemp := true
	defer func() {
		_ = file.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if err := Convert(pdfPath, file); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("关闭临时 OFD 文件失败: %w", err)
	}
	if err := os.Rename(tempPath, ofdPath); err != nil {
		return fmt.Errorf("替换 OFD 文件失败: %w", err)
	}
	removeTemp = false
	return nil
}

func readPDFInput(input any) ([]byte, error) {
	switch value := input.(type) {
	case string:
		data, err := os.ReadFile(value)
		if err != nil {
			return nil, fmt.Errorf("读取 PDF 文件失败: %w", err)
		}
		return data, nil
	case []byte:
		return append([]byte(nil), value...), nil
	case io.Reader:
		data, err := io.ReadAll(value)
		if err != nil {
			return nil, fmt.Errorf("读取 PDF 输入失败: %w", err)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("不支持的 PDF 输入类型 %T", input)
	}
}

func convertPDFPage(ctx *model.Context, pageNumber int, document *creator.Document) (creator.Page, error) {
	pageDict, _, inherited, err := ctx.PageDict(pageNumber, false)
	if err != nil {
		return creator.Page{}, err
	}
	info, err := pdfPageInfoFor(ctx, pageDict, inherited)
	if err != nil {
		return creator.Page{}, err
	}
	width := info.maxX - info.minX
	height := info.maxY - info.minY
	if info.rotate == 90 || info.rotate == 270 {
		width, height = height, width
	}
	pageWidth := width * info.userUnit * pdfPointToMillimeter
	pageHeight := height * info.userUnit * pdfPointToMillimeter
	area := &creator.PageArea{PhysicalBox: &creator.Box{Width: pageWidth, Height: pageHeight}}
	page := creator.Page{Area: area}
	var resources types.Dict
	if inherited != nil {
		resources = inherited.Resources
	}
	if resources == nil {
		resourceObject, found := pageDict.Find("Resources")
		resources, err = dereferenceDict(ctx, resourceObject, found)
		if err != nil {
			return creator.Page{}, err
		}
	}
	content, err := ctx.XRefTable.PageContent(pageDict, pageNumber)
	if err != nil {
		return creator.Page{}, fmt.Errorf("读取页面内容失败: %w", err)
	}
	interpreter := newPDFInterpreter(ctx, &page, document, info)
	if err := interpreter.parse(content, resources, nil, 0); err != nil {
		return creator.Page{}, err
	}
	// 注解（外观流）绘制在页面内容之上。
	interpreter.renderAnnotations(pageDict)
	// 合并相邻单字文字对象，减少 OFD 文字对象数量，逐字定位保持不变。
	page.Items = mergeAdjacentTextItems(page.Items)
	return page, nil
}

func pdfPageInfoFor(ctx *model.Context, page types.Dict, inherited *model.InheritedPageAttrs) (pdfPageInfo, error) {
	var rect *types.Rectangle
	if box := page.ArrayEntry("CropBox"); box != nil {
		rect = types.RectForArray(box)
	}
	if rect == nil && inherited != nil {
		rect = inherited.CropBox
	}
	if rect == nil && inherited != nil {
		rect = inherited.MediaBox
	}
	if rect == nil {
		if box := page.ArrayEntry("MediaBox"); box != nil {
			rect = types.RectForArray(box)
		}
	}
	if rect == nil {
		return pdfPageInfo{}, errors.New("PDF 页面缺少 MediaBox")
	}
	minX, maxX := rect.LL.X, rect.UR.X
	minY, maxY := rect.LL.Y, rect.UR.Y
	if minX > maxX {
		minX, maxX = maxX, minX
	}
	if minY > maxY {
		minY, maxY = maxY, minY
	}
	info := pdfPageInfo{minX: minX, minY: minY, maxX: maxX, maxY: maxY, userUnit: 1}
	userUnitObject, found := page.Find("UserUnit")
	if value, ok := numberValue(userUnitObject); found && ok && value > 0 {
		info.userUnit = value
	}
	if inherited != nil {
		info.rotate = inherited.Rotate
	}
	rotateObject, found := page.Find("Rotate")
	if value, ok := integerValue(rotateObject); found && ok {
		info.rotate = value
	}
	info.rotate = ((info.rotate % 360) + 360) % 360
	return info, nil
}

func dereferenceDict(ctx *model.Context, object types.Object, found bool) (types.Dict, error) {
	if !found || object == nil {
		return nil, nil
	}
	return ctx.XRefTable.DereferenceDict(object)
}

func newPDFInterpreter(ctx *model.Context, page *creator.Page, document *creator.Document, info pdfPageInfo) *pdfInterpreter {
	return &pdfInterpreter{ctx: ctx, page: page, document: document, info: info, fonts: map[string]pdfFontInfo{}, fontAliases: map[string]string{}, state: pdfGraphicsState{
		ctm: identityPDFMatrix(), textMatrix: identityPDFMatrix(), lineMatrix: identityPDFMatrix(), fontSize: 12,
		fill: pdfColor{r: 0, g: 0, b: 0}, stroke: pdfColor{r: 0, g: 0, b: 0}, lineWidth: 1, hScale: 100,
		fillAlpha: 1, strokeAlpha: 1, groupAlpha: 1,
	}}
}

// applyExtGState 读取 ExtGState 的不透明度 ca/CA。混合模式与软掩码暂不处理，
// 缺失的键保留当前取值。
func (p *pdfInterpreter) applyExtGState(resources types.Dict, name string) {
	if resources == nil || name == "" {
		return
	}
	states, ok := dereferencedSubDict(p.ctx, resources, "ExtGState")
	if !ok {
		return
	}
	raw, found := states.Find(name)
	if !found {
		return
	}
	dict, err := p.ctx.XRefTable.DereferenceDict(raw)
	if err != nil || dict == nil {
		return
	}
	if value, found := dict.Find("ca"); found {
		if number, err := p.ctx.XRefTable.Dereference(value); err == nil {
			if opacity, ok := numberValue(number); ok {
				p.state.fillAlpha = clampOpacity(opacity)
			}
		}
	}
	if value, found := dict.Find("CA"); found {
		if number, err := p.ctx.XRefTable.Dereference(value); err == nil {
			if opacity, ok := numberValue(number); ok {
				p.state.strokeAlpha = clampOpacity(opacity)
			}
		}
	}
	if value, found := dict.Find("BM"); found {
		if number, err := p.ctx.XRefTable.Dereference(value); err == nil {
			if name, ok := number.(types.Name); ok {
				p.state.blendMode = name.Value()
			}
		}
	}
}

// isNormalBlendMode 判断 BM 是否等价于不混合。OFD 没有混合模式，只有 Normal
// 才可以用 ca/CA 的纯透明度近似。
func isNormalBlendMode(mode string) bool {
	switch mode {
	case "", "Normal", "Compatible":
		return true
	default:
		return false
	}
}

func clampOpacity(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func (p *pdfInterpreter) parse(content []byte, resources types.Dict, inherited *pdfGraphicsState, depth int) error {
	if depth > 16 {
		return errors.New("PDF Form XObject 嵌套过深")
	}
	if inherited != nil {
		p.state = *inherited
	}
	if p.state.ctm == [6]float64{} {
		p.state.ctm = identityPDFMatrix()
	}
	tokens := newPDFContentTokenizer(content)
	operands := []any{}
	for {
		value, ok, err := tokens.next()
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		if operator, isOperator := value.(string); isOperator && !strings.HasPrefix(operator, "/") {
			if operator == "BI" {
				operands = operands[:0]
				if err := p.inlineImage(tokens); err != nil {
					return err
				}
				continue
			}
			if err := p.operator(operator, operands, resources, depth); err != nil {
				return err
			}
			operands = operands[:0]
			continue
		}
		operands = append(operands, value)
	}
	return nil
}

func (p *pdfInterpreter) operator(op string, args []any, resources types.Dict, depth int) error {
	floatArg := func(index int) float64 {
		if index >= len(args) {
			return 0
		}
		return anyFloat(args[index])
	}
	switch op {
	case "q":
		p.stack = append(p.stack, p.state)
	case "Q":
		if len(p.stack) > 0 {
			p.state = p.stack[len(p.stack)-1]
			p.stack = p.stack[:len(p.stack)-1]
		}
	case "cm":
		if len(args) >= 6 {
			// PDF 把新矩阵右乘到当前 CTM 上。颠倒这个顺序会让嵌套
			// Form/Image XObject 中的平移被缩放，把对象移到页面外数千毫米。
			matrix := [6]float64{floatArg(0), floatArg(1), floatArg(2), floatArg(3), floatArg(4), floatArg(5)}
			p.state.ctm = multiplyPDFMatrix(p.state.ctm, matrix)
		}
	case "gs":
		if len(args) > 0 {
			p.applyExtGState(resources, anyName(args[0]))
		}
	case "w":
		p.state.lineWidth = floatArg(0)
	case "d":
		// [array] phase d：设置虚线数组与起始偏移；空数组恢复实线。
		p.state.dashPattern = nil
		p.state.dashOffset = 0
		if len(args) > 0 {
			if values, ok := args[0].([]any); ok {
				pattern := make([]float64, 0, len(values))
				for _, value := range values {
					pattern = append(pattern, anyFloat(value))
				}
				// PDF 规定元素个数为奇数时重复一次补成偶数。
				if len(pattern)%2 == 1 {
					pattern = append(pattern, pattern...)
				}
				if len(pattern) > 0 {
					p.state.dashPattern = pattern
				}
			}
		}
		if len(args) > 1 {
			p.state.dashOffset = anyFloat(args[1])
		}
	case "rg":
		if len(args) >= 3 {
			p.state.fillSpace = deviceRGBSpace
			p.state.fill, p.state.fillPaint = rgbColor(floatArg(0), floatArg(1), floatArg(2)), nil
		}
	case "RG":

		if len(args) >= 3 {
			p.state.strokeSpace = deviceRGBSpace
			p.state.stroke, p.state.strokePaint = rgbColor(floatArg(0), floatArg(1), floatArg(2)), nil
		}
	case "g":
		if len(args) >= 1 {
			p.state.fillSpace = deviceGraySpace
			p.state.fill, p.state.fillPaint = rgbColor(floatArg(0), floatArg(0), floatArg(0)), nil
		}
	case "G":
		if len(args) >= 1 {
			p.state.strokeSpace = deviceGraySpace
			p.state.stroke, p.state.strokePaint = rgbColor(floatArg(0), floatArg(0), floatArg(0)), nil
		}
	case "k":
		if len(args) >= 4 {
			p.state.fillSpace = deviceCMYKSpace
			p.state.fill, p.state.fillPaint = cmykColor(floatArg(0), floatArg(1), floatArg(2), floatArg(3)), nil
		}
	case "K":

		if len(args) >= 4 {
			p.state.strokeSpace = deviceCMYKSpace
			p.state.stroke, p.state.strokePaint = cmykColor(floatArg(0), floatArg(1), floatArg(2), floatArg(3)), nil
		}
	case "cs":
		// 选择颜色空间本身不改变当前颜色，实际颜色由随后的 scn/SCN 提供。
		p.state.fillSpace = p.resolveColorSpace(resources, anyName(args[0]))
	case "CS":
		p.state.strokeSpace = p.resolveColorSpace(resources, anyName(args[0]))
	case "sc", "scn":
		if p.state.fillSpace == patternSpace {
			p.state.fillPaint = p.resolvePattern(resources, anyName(args[0]))
			break
		}
		if value, ok := p.colorFromOperands(p.state.fillSpace, args); ok {
			p.state.fill, p.state.fillPaint = value, nil
		}
	case "SC", "SCN":

		if p.state.strokeSpace == patternSpace {
			p.state.strokePaint = p.resolvePattern(resources, anyName(args[0]))
			break
		}
		if value, ok := p.colorFromOperands(p.state.strokeSpace, args); ok {
			p.state.stroke, p.state.strokePaint = value, nil
		}
	case "BT":
		p.state.textMatrix, p.state.lineMatrix = identityPDFMatrix(), identityPDFMatrix()
	case "Tf":
		if len(args) >= 2 {
			p.state.fontName = anyName(args[0])
			p.state.fontSize = floatArg(1)
			p.ensureFont(resources, p.state.fontName)
		}
	case "Tm":
		if len(args) >= 6 {
			p.state.textMatrix = [6]float64{floatArg(0), floatArg(1), floatArg(2), floatArg(3), floatArg(4), floatArg(5)}
			p.state.lineMatrix = p.state.textMatrix
		}
	case "Td", "TD":
		if len(args) >= 2 {
			p.moveText(floatArg(0), floatArg(1))
			if op == "TD" {
				p.state.textLeading = -floatArg(1)
			}
		}
	case "TL":
		p.state.textLeading = floatArg(0)
	case "T*":
		p.moveText(0, -p.state.textLeading)
	case "Tc":
		p.state.charSpacing = floatArg(0)
	case "Tw":
		p.state.wordSpacing = floatArg(0)
	case "Tz":
		p.state.hScale = floatArg(0)
	case "Tr":
		p.state.renderMode = int(floatArg(0))
	case "Tj":
		if len(args) > 0 {
			p.showText(anyBytes(args[0]), nil)
		}
	case "TJ":
		if len(args) > 0 {
			if values, ok := args[0].([]any); ok {
				for _, value := range values {
					if data, ok := value.(pdfString); ok {
						p.showText([]byte(data), nil)
					} else {
						p.adjustText(-anyFloat(value) / 1000 * p.state.fontSize)
					}
				}
			}
		}
	case "'":
		p.moveText(0, -p.state.textLeading)
		if len(args) > 0 {
			p.showText(anyBytes(args[0]), nil)
		}
	case "\"":
		if len(args) >= 3 {
			p.state.wordSpacing = floatArg(0)
			p.state.charSpacing = floatArg(1)
			p.moveText(0, -p.state.textLeading)
			p.showText(anyBytes(args[2]), nil)
		}
	case "m":
		if len(args) >= 2 {
			p.moveTo(floatArg(0), floatArg(1))
		}
	case "l":
		if len(args) >= 2 {
			p.lineTo(floatArg(0), floatArg(1))
		}
	case "c":
		if len(args) >= 6 {
			p.curveTo(args)
		}
	case "v":
		if len(args) >= 4 {
			x2, y2 := transformPDFPoint(anyFloat(args[0]), anyFloat(args[1]), p.state.ctm)
			x3, y3 := transformPDFPoint(anyFloat(args[2]), anyFloat(args[3]), p.state.ctm)
			p.appendCurve(p.pointX, p.pointY, x2, y2, x3, y3)
		}
	case "y":
		if len(args) >= 4 {
			x1, y1 := transformPDFPoint(anyFloat(args[0]), anyFloat(args[1]), p.state.ctm)
			x3, y3 := transformPDFPoint(anyFloat(args[2]), anyFloat(args[3]), p.state.ctm)
			p.appendCurve(x1, y1, x3, y3, x3, y3)
		}
	case "h":
		p.path = append(p.path, pdfPathCommand{op: "Z"})
		p.pointX, p.pointY = p.startX, p.startY
	case "re":
		if len(args) >= 4 {
			x, y, w, h := floatArg(0), floatArg(1), floatArg(2), floatArg(3)
			p.moveTo(x, y)
			p.lineTo(x+w, y)
			p.lineTo(x+w, y+h)
			p.lineTo(x, y+h)
			p.path = append(p.path, pdfPathCommand{op: "Z"})
			p.pointX, p.pointY = transformPDFPoint(x, y, p.state.ctm)
		}
	case "W", "W*":
		// W/W* 把当前路径设为裁剪区，在随后的路径绘制操作（n/S/f 等）时生效。
		if len(p.path) > 0 {
			p.pendingClip = append([]pdfPathCommand(nil), p.path...)
			p.hasPendingClip = true
		}
	case "S":
		p.commitPendingClip()
		p.paintPath(true, false, "NonZero")
	case "s":
		p.path = append(p.path, pdfPathCommand{op: "Z"})
		p.commitPendingClip()
		p.paintPath(true, false, "NonZero")
	case "f", "F":
		p.commitPendingClip()
		p.paintPath(false, true, "NonZero")
	case "f*":
		p.commitPendingClip()
		p.paintPath(false, true, "Even-Odd")
	case "B":
		p.commitPendingClip()
		p.paintPath(true, true, "NonZero")
	case "B*":
		p.commitPendingClip()
		p.paintPath(true, true, "Even-Odd")
	case "b":
		p.path = append(p.path, pdfPathCommand{op: "Z"})
		p.commitPendingClip()
		p.paintPath(true, true, "NonZero")
	case "b*":
		p.path = append(p.path, pdfPathCommand{op: "Z"})
		p.commitPendingClip()
		p.paintPath(true, true, "Even-Odd")
	case "n":
		// n 结束路径；若前面有 W/W*，裁剪区在这里生效且不绘制。
		p.commitPendingClip()
		p.path = nil
	case "Do":
		p.commitPendingClip()
		if len(args) > 0 {
			return p.xobject(anyName(args[0]), resources, depth)
		}
	case "sh":
		// sh 用着色填充当前裁剪区，不改变路径与裁剪状态。
		if len(args) > 0 {
			p.paintShading(anyName(args[0]), resources)
		}
	}
	return nil
}

// commitPendingClip 把 W/W* 暂存的裁剪路径并入当前图形状态。
// 切片追加时复制底层数组，避免 q/Q 恢复状态后被后续裁剪污染。
func (p *pdfInterpreter) commitPendingClip() {
	if !p.hasPendingClip {
		return
	}
	p.hasPendingClip = false
	region := p.pendingClip
	p.pendingClip = nil
	if len(region) == 0 {
		return
	}
	clips := make([]pdfClipRegion, 0, len(p.state.clips)+1)
	clips = append(clips, p.state.clips...)
	clips = append(clips, pdfClipRegion{commands: region})
	p.state.clips = clips
}

func (p *pdfInterpreter) ensureFont(resources types.Dict, name string) {
	if name == "" {
		return
	}
	if _, ok := p.fonts[name]; ok {
		return
	}
	font := pdfFontInfo{widths: map[int]float64{}, defaultW: 500, codeBytes: 1}
	if resources != nil {
		if fonts, ok := dereferencedSubDict(p.ctx, resources, "Font"); ok {
			if object, found := fonts.Find(name); found {
				p.loadFont(&font, object)
			}
		}
	}
	family := strings.TrimPrefix(name, "/")
	if font.familyName != "" {
		family = font.familyName
	}
	if strings.Contains(family, "+") {
		family = family[strings.Index(family, "+")+1:]
	}
	if family == "" {
		family = "Helvetica"
	}
	font.bold, font.italic, _, _ = pdfFontStyleFlags(family)
	// PDF 未嵌入字体时保持逻辑字体，由阅读器按字体族名回退到本机字体。
	p.fonts[name] = font
	documentName := name
	for _, value := range p.document.Fonts {
		if value.Name != name {
			continue
		}
		if value.Format == font.format && bytes.Equal(value.Data, font.data) {
			return
		}
		// 资源名只在 PDF 页面内局部有效：同名资源在另一页可能指向不同的
		// 内嵌字体，因此不能按名称合并字体。
		digest := sha256.Sum256(append([]byte(font.format+":"), font.data...))
		documentName = name + "-" + hex.EncodeToString(digest[:])[:12]
		break
	}
	_, _, serif, fixedWidth := pdfFontStyleFlags(family)
	current := creator.Font{Name: documentName, FamilyName: family, Charset: "unicode", Format: font.format, Data: font.data, Bold: font.bold, Italic: font.italic, Serif: serif, FixedWidth: fixedWidth}
	p.document.Fonts = append(p.document.Fonts, current)
	for _, value := range p.document.Fonts[:len(p.document.Fonts)-1] {
		if current.Format == value.Format && len(current.Data) > 0 && bytes.Equal(current.Data, value.Data) {
			p.document.Fonts = p.document.Fonts[:len(p.document.Fonts)-1]
			p.fontAliases[name] = value.Name
			return
		}
	}
	if documentName != name {
		p.fontAliases[name] = documentName
	}
}
