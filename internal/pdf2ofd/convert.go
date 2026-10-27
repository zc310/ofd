// Package pdf2ofd 提供纯 Go 的 PDF 到 OFD 转换实现：解析 PDF 页面几何、文字、
// 路径和图像 XObject，并使用 pkg/creator 输出 OFD 包，不依赖原生 PDF 渲染器。
package pdf2ofd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"github.com/zc310/ofd/pkg/creator"
	"github.com/zc310/ofd/pkg/spec"
)

// Convert 把 PDF 输入（文件路径、[]byte、io.Reader 或 io.ReadSeeker）转换为
// OFD 包。转换保留基本页面几何、文字、路径和 JPEG/PNG 图像 XObject，
// 不使用原生 PDF 渲染器。
func Convert(gctx context.Context, input any, output io.Writer, password string) error {
	return ConvertWithOptions(gctx, input, output, password, Options{})
}

// ConvertWithOptions 在资源预算内转换 PDF，并按选项报告无法完整保留的内容。
// []byte 输入在调用期间只读借用，调用方应保持其内容不变，直到转换返回。
func ConvertWithOptions(gctx context.Context, input any, output io.Writer, password string, options Options) (err error) {
	return ConvertWithAttachments(gctx, input, output, password, options, nil)
}

// ConvertWithAttachments 将上游格式的原始附件与 PDF 页面一同写出为 OFD。
// 附件按 GB/T 33190—2016 §20.1/20.2 交给 creator 生成清单和 FileLoc，
// 不经过渲染或重新编码；调用方负责限制上游附件的数量和总字节数。
func ConvertWithAttachments(gctx context.Context, input any, output io.Writer, password string, options Options, attachments []creator.Attachment) (err error) {
	if gctx == nil {
		gctx = context.Background()
	}
	defer func() {
		if gctx.Err() != nil {
			err = gctx.Err()
		}
	}()
	if err := gctx.Err(); err != nil {
		return err
	}
	if output == nil {
		return errors.New("OFD 输出写入器为空")
	}
	options, err = options.Normalize()
	if err != nil {
		return err
	}
	control := &conversionControl{ctx: gctx, options: options, attachments: attachments}
	data, err := readPDFInputLimited(gctx, input, options.Limits.MaxInputBytes)
	if err != nil {
		return err
	}
	err = pdfToOFDBytes(gctx, data, output, password, control)
	if err != nil && strings.Contains(err.Error(), "PDF 解析器异常") {
		if repaired, repairErr := repairPDFXRefWithLimits(data, options.Limits, control); repairErr == nil {
			return pdfToOFDBytes(gctx, repaired, output, password, control)
		} else if errors.Is(repairErr, ErrLimitExceeded) {
			return repairErr
		}
	}
	return err
}

func pdfToOFDBytes(gctx context.Context, data []byte, output io.Writer, password string, control *conversionControl) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("解析 PDF 失败: %v", recovered)
		}
	}()
	// 无状态配置：只用 pdfcpu 内置的默认配置，不碰磁盘上的配置目录。
	//
	// 这里不能用 model.NewDefaultConfiguration()：它会读全局 model.ConfigPath，
	// 非 "disable" 时调用 os.UserConfigDir() 并创建配置目录。js/wasm 下没有
	// 文件系统，那一步会报 "config problem: mkdir /tmp: not implemented on js"，
	// PDF 导入直接失败。而这份配置是在本包内部创建的，调用方（cmd/ofd-wasm 等）
	// 没法从外面注入，只能在这里自己拿无状态的那份。
	//
	// 用户字体因此不可用，但 PDF 导入本来就不依赖它：字形来自 PDF 自带的
	// 字体程序，输出 OFD 的字体由 pkg/creator 按 OFD 规范内嵌。
	conf, err := api.LoadConfiguration(api.ConfigurationOptions{
		Mode: api.ConfigurationModeStateless,
	})
	if err != nil {
		return fmt.Errorf("加载 pdfcpu 配置失败: %w", err)
	}
	conf.ValidationMode = model.ValidationRelaxed
	conf.UserPW = password
	conf.OwnerPW = password
	l := control.options.Limits
	conf.Limits.MaxInputBytes = l.MaxInputBytes
	conf.Limits.MaxStreamBytes = l.MaxStreamBytes
	conf.Limits.MaxDecodeBytes = l.MaxDecodedStreamBytes
	conf.Limits.MaxImagePixels = l.MaxImagePixels
	conf.Limits.MaxObjectCount = int(l.MaxObjects)
	conf.Limits.MaxXRefEntries = int(l.MaxObjects)
	conf.Limits.MaxObjectStreamCount = int(l.MaxObjects)
	conf.Limits.MaxRecursionDepth = int(l.MaxDepth)
	ctx, err, panicked := readPDFContext(gctx, data, conf)
	if panicked {
		if repaired, repairErr := repairPDFXRefWithLimits(data, l, control); repairErr == nil {
			ctx, err, _ = readPDFContext(gctx, repaired, conf)
		} else if errors.Is(repairErr, ErrLimitExceeded) {
			return repairErr
		}
	}
	if err != nil {
		err = control.parserError(err)
		if errors.Is(err, ErrLimitExceeded) {
			return err
		}
		// 口令不对时 pdfcpu 的消息是英文且不点明成因，转换器不该把
		// "please provide the correct password" 直接甩给用户。
		if isPasswordError(err) {
			if password == "" {
				return fmt.Errorf("%w: PDF 已加密，请用 --password 提供打开口令", ErrEncrypted)
			}
			return fmt.Errorf("%w: PDF 口令不正确", ErrWrongPassword)
		}
		return fmt.Errorf("读取 PDF 失败: %w", err)
	}
	if err := control.accountParsedStreams(ctx); err != nil {
		return err
	}
	if err := api.ValidateContext(gctx, ctx); err != nil {
		err = control.parserError(err)
		if errors.Is(err, ErrLimitExceeded) {
			return err
		}
		// 部分可正常读取的 PDF 带有损坏的历史 Info 日期。保留已提取的
		// 元数据，去掉不可用的 Info 引用后重新校验页面和内容对象。
		ctx.Info = nil
		if retryErr := api.ValidateContext(gctx, ctx); retryErr != nil {
			return fmt.Errorf("验证 PDF 失败: %w", control.parserError(retryErr))
		}
	}
	if err := control.accountParsedStreams(ctx); err != nil {
		return err
	}
	if ctx.PageCount == 0 {
		return errors.New("PDF 没有页面")
	}
	if int64(ctx.PageCount) > l.MaxPages {
		return control.limit("pages", int64(ctx.PageCount), l.MaxPages)
	}
	if err := control.check(); err != nil {
		return err
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
		Attachments:    append([]creator.Attachment(nil), control.attachments...),
	}
	fontCache := make(map[types.Object]pdfFontInfo, 8)
	imageCache := make(map[types.IndirectRef]pdfImageDataCache, 8)
	// 内部跳转要按页号定位目标页，页面几何必须先全部拿到。
	geometries := pdfPageGeometries(gctx, ctx)
	linkID := linkAnnotationIDBase
	for pageNumber := 1; pageNumber <= ctx.PageCount; pageNumber++ {
		if err := control.check(); err != nil {
			return err
		}
		page, links, err := convertPDFPage(gctx, ctx, pageNumber, &document, fontCache, imageCache, geometries, &linkID, control)
		if err != nil {
			return fmt.Errorf("转换 PDF 第 %d 页失败: %w", pageNumber, err)
		}
		document.Pages = append(document.Pages, page)
		if len(links) > 0 {
			document.Annotations = append(document.Annotations, creator.AnnotationPage{
				Page: pageNumber - 1, Items: links,
			})
		}
	}
	if len(document.Pages) > 0 {
		document.PageSize = creator.PageSize{Width: document.Pages[0].Area.PhysicalBox.Width, Height: document.Pages[0].Area.PhysicalBox.Height}
	}
	// PDF 目录大纲转换为 OFD 大纲；原文档要求显示大纲面板时同步设置显示偏好。
	document.CreationDate, document.ModDate = pdfDocumentDates(ctx)
	document.Outlines = convertOutlines(gctx, ctx)
	if len(document.Outlines) > 0 && pdfPageModeUseOutlines(gctx, ctx) {
		document.Preferences = &creator.ViewPreferences{PageMode: spec.PageModeUseOutlines}
	}
	if err := control.check(); err != nil {
		return err
	}
	return creator.CreateWithOptions(document, output, creator.CreateOptions{PreserveEmbeddedFonts: true})
}

func readPDFContext(gctx context.Context, data []byte, conf *model.Configuration) (ctx *model.Context, err error, panicked bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("PDF 解析器异常: %v", recovered)
			panicked = true
		}
	}()
	ctx, err = api.ReadContext(gctx, bytes.NewReader(data), conf)
	return ctx, err, false
}

// ErrEncrypted 表示输入 PDF 已加密但没有可用口令。
var ErrEncrypted = errors.New("文档已加密")

// ErrWrongPassword 表示提供的口令无法打开 PDF。
//
// 与 ErrEncrypted 分开是因为处置不同：前者调用方补个口令就能继续，后者
// 是口令给错了，重试同一份不会变。
var ErrWrongPassword = errors.New("口令不正确")

// isPasswordError 判断错误是否源于加密口令。
//
// 用 errors.Is 匹配 pdfcpu 的哨兵而不是比对消息文本——库改一次措辞
// 这层就失效，而失效方式是"加密文档不再被识别成加密文档"，正是最难查的那种。
func isPasswordError(err error) bool {
	return errors.Is(err, pdfcpu.ErrWrongPassword) || errors.Is(err, pdfcpu.ErrOwnerPasswordRequired)
}

// ConvertFile 是 Convert 的按路径便捷形式。
func ConvertFile(gctx context.Context, pdfPath, ofdPath string, password string) error {
	return ConvertFileWithOptions(gctx, pdfPath, ofdPath, password, Options{})
}

// ConvertFileWithOptions 在临时文件中完成转换，成功后替换目标文件。
func ConvertFileWithOptions(gctx context.Context, pdfPath, ofdPath string, password string, options Options) error {
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
	if err := ConvertWithOptions(gctx, pdfPath, file, password, options); err != nil {
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
	options, _ := (Options{}).Normalize()
	return readPDFInputLimited(context.Background(), input, options.Limits.MaxInputBytes)
}

type pdfInputReader struct {
	ctx   context.Context
	input io.Reader
}

func (r pdfInputReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.input.Read(data)
}

func readPDFInputLimited(ctx context.Context, input any, limit int64) ([]byte, error) {
	read := func(r io.Reader) ([]byte, error) {
		data, err := io.ReadAll(io.LimitReader(pdfInputReader{ctx, r}, limit+1))
		if err != nil {
			return nil, fmt.Errorf("读取 PDF 输入失败: %w", err)
		}
		if int64(len(data)) > limit {
			return nil, &LimitError{Resource: "input_bytes", Limit: limit, Actual: int64(len(data))}
		}
		return data, nil
	}
	switch value := input.(type) {
	case string:
		file, err := os.Open(value)
		if err != nil {
			return nil, fmt.Errorf("读取 PDF 文件失败: %w", err)
		}
		defer func() { _ = file.Close() }()
		if stat, err := file.Stat(); err == nil && stat.Size() > limit {
			return nil, &LimitError{Resource: "input_bytes", Limit: limit, Actual: stat.Size()}
		}
		return read(file)
	case []byte:
		if int64(len(value)) > limit {
			return nil, &LimitError{Resource: "input_bytes", Limit: limit, Actual: int64(len(value))}
		}
		return value, nil
	case io.Reader:
		return read(value)
	default:
		return nil, fmt.Errorf("不支持的 PDF 输入类型 %T", input)
	}
}

func convertPDFPage(gctx context.Context, ctx *model.Context, pageNumber int, document *creator.Document, fontCache map[types.Object]pdfFontInfo, imageCache map[types.IndirectRef]pdfImageDataCache, geometries []pdfPageInfo, linkID *uint64, controls ...*conversionControl) (creator.Page, []creator.Annotation, error) {
	var control *conversionControl
	if len(controls) > 0 {
		control = controls[0]
	}
	if control == nil {
		options, _ := (Options{}).Normalize()
		control = &conversionControl{ctx: gctx, options: options}
	}
	pageDict, _, inherited, err := ctx.PageDict(gctx, pageNumber, false)
	if err != nil {
		return creator.Page{}, nil, err
	}
	info, err := pdfPageInfoFor(ctx, pageDict, inherited)
	if err != nil {
		return creator.Page{}, nil, err
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
			return creator.Page{}, nil, err
		}
	}
	content, err := controlledPageContent(ctx, pageDict, control)
	if err != nil {
		return creator.Page{}, nil, fmt.Errorf("读取页面内容失败: %w", err)
	}
	interpreter := newPDFInterpreter(ctx, &page, document, info, fontCache, imageCache)
	interpreter.control = control
	interpreter.source.Page = pageNumber
	if err := interpreter.parse(content, resources, nil, 0); err != nil {
		return creator.Page{}, nil, err
	}
	// 注解（外观流）绘制在页面内容之上。
	interpreter.renderAnnotations(pageDict)
	if err := control.check(); err != nil {
		return creator.Page{}, nil, err
	}
	// 合并相邻单字文字对象，减少 OFD 文字对象数量，逐字定位保持不变。
	page.Items = mergeAdjacentTextItems(page.Items)
	if err := positionPDFSemanticTexts(page.Items, document.Fonts, control); err != nil {
		return creator.Page{}, nil, err
	}
	return page, convertPageLinks(gctx, ctx, pageDict, info, geometries, linkID), nil
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

func newPDFInterpreter(ctx *model.Context, page *creator.Page, document *creator.Document, info pdfPageInfo, fontCache map[types.Object]pdfFontInfo, imageCache map[types.IndirectRef]pdfImageDataCache) *pdfInterpreter {
	if fontCache == nil {
		fontCache = map[types.Object]pdfFontInfo{}
	}
	if imageCache == nil {
		imageCache = map[types.IndirectRef]pdfImageDataCache{}
	}
	return &pdfInterpreter{ctx: ctx, page: page, document: document, info: info, fontCache: fontCache, imageCache: imageCache, fonts: map[string]pdfFontInfo{}, fontAliases: map[string]string{}, state: pdfGraphicsState{
		ctm: identityPDFMatrix(), textMatrix: identityPDFMatrix(), lineMatrix: identityPDFMatrix(), fontSize: 12,
		fill: pdfColor{r: 0, g: 0, b: 0}, stroke: pdfColor{r: 0, g: 0, b: 0}, lineWidth: 1, hScale: 100,
		miterLimit: 10,
		fillAlpha:  1, strokeAlpha: 1, groupAlpha: 1,
	}}
}

// applyExtGState 读取 ExtGState 的不透明度 ca/CA、混合模式 BM 与软掩码 SMask。
// 缺失的键保留当前取值；SMask 显式为 /None 时清除软掩码。OFD 无法直接表达
// 的混合与透明组在局部栅格上合成，近似范围通过诊断报告。
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
	previousSource := p.setSource(raw, name)
	defer func() { p.source = previousSource }()
	dict, err := p.ctx.XRefTable.DereferenceDict(raw)
	if err != nil || dict == nil {
		return
	}
	// ExtGState 与独立绘图操作符使用相同的线宽、线帽、连接和斜接参数语义。
	if value, found := dict.Find("AIS"); found {
		if resolved, err := dereferencePDFObject(p.ctx, value); err == nil {
			if flag, ok := resolved.(types.Boolean); ok {
				p.state.alphaIsShape = bool(flag)
				if flag {
					p.report("graphics.alpha_shape", "AlphaIsShape 按栅格形状覆盖率近似，源阅读器对该标志的支持不同")
				}
			}
		}
	}
	for _, setting := range [...]struct{ key, op string }{{"LW", "w"}, {"LC", "J"}, {"LJ", "j"}, {"ML", "M"}} {
		if value, ok := dereferencedPDFNumber(p.ctx, dict[setting.key]); ok {
			_ = p.operator(setting.op, []any{value}, nil, 0)
		}
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
			if array, ok := number.(types.Array); ok {
				number = types.Name("Normal")
				for _, candidate := range array {
					if name, ok := dereferenceName(p.ctx, candidate); ok && supportedPDFBlend(name) {
						number = types.Name(name)
						break
					}
				}
			}
			if name, ok := number.(types.Name); ok {
				p.state.blendMode = name.Value()
				if !isNormalBlendMode(p.state.blendMode) {
					p.report("graphics.blend", "混合模式 "+p.state.blendMode+" 以 300dpi 在 RGB 空间局部合成")
					if !supportedPDFBlend(p.state.blendMode) {
						p.report("graphics.blend_unsupported", "未知混合模式按 Normal 绘制")
						p.state.blendMode = "Normal"
					}
				}
			}
		}
	}
	if _, found := dict.Find("SMask"); found {
		p.state.softMask = nil
		p.state.constantMaskBlack = false
		if p.control != nil {
			if err := p.preflightGraphicsMask(dict["SMask"]); err != nil {
				p.report("image.mask_graphics", err.Error())
				return
			}
		}
		p.state.softMask = p.parseTransparencyMask(dict["SMask"], resources)
		if p.state.softMask != nil {
			p.state.lumMask = nil
			return
		}
		p.state.lumMask = pdfExtGStateSoftMask(p.ctx, dict, p.control)
		if p.state.lumMask == nil {
			mask, _ := dereferencePDFObject(p.ctx, dict["SMask"])
			if flag, ok := mask.(types.Name); !ok || flag.Value() != "None" {
				p.report("image.mask_graphics", "图形状态软掩码无法还原为单图像，保留未遮罩内容")
			}
		}
	}
}

// isNormalBlendMode 判断 BM 是否可直接使用 OFD 的普通透明叠加。
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
	previousDepth := p.parseDepth
	p.parseDepth = depth
	defer func() { p.parseDepth = previousDepth }()
	maximum := int64(16)
	if p.control != nil {
		maximum = p.control.options.Limits.MaxDepth
	}
	if int64(depth) > maximum && p.control != nil {
		return p.control.limit("form_depth", int64(depth), maximum)
	}
	if int64(depth) > maximum {
		return errors.New("PDF Form XObject 嵌套过深")
	}
	if inherited != nil {
		p.state = *inherited
	}
	if p.state.ctm == [6]float64{} {
		p.state.ctm = identityPDFMatrix()
	}
	tokens := newPDFContentTokenizer(content)
	tokens.control = p.control
	operands := []any{}
	for {
		if p.control != nil {
			if err := p.control.check(); err != nil {
				return err
			}
		}
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
				if err := p.inlineImage(tokens, resources); err != nil {
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
	return p.control.check()
}

func (p *pdfInterpreter) operator(op string, args []any, resources types.Dict, depth int) error {
	if p.type3Uncolored {
		switch op {
		case "g", "G", "rg", "RG", "k", "K", "cs", "CS", "sc", "SC", "scn", "SCN":
			p.report("font.type3_color", "d1 未着色字形不能设置颜色，保留调用方颜色")
			return p.control.check()
		}
	}
	floatArg := func(index int) float64 {
		if index >= len(args) {
			return 0
		}
		return anyFloat(args[index])
	}
	switch op {
	case "q":
		if p.control != nil && int64(len(p.stack)) >= p.control.options.Limits.MaxDepth {
			return p.control.limit("graphics_state_depth", int64(len(p.stack))+1, p.control.options.Limits.MaxDepth)
		}
		p.stack = append(p.stack, p.state)
	case "Q":
		if len(p.stack) > 0 {
			// 文本矩阵属于文字对象的定位状态，不随 q/Q 保存和恢复；文字参数
			// （字号、间距、升降等）仍随图形状态恢复。
			textMatrix, lineMatrix := p.state.textMatrix, p.state.lineMatrix
			p.state = p.stack[len(p.stack)-1]
			p.state.textMatrix, p.state.lineMatrix = textMatrix, lineMatrix
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
	case "J":
		if value := int(floatArg(0)); value >= 0 && value <= 2 {
			p.state.lineCap = value
		}
	case "j":
		if value := int(floatArg(0)); value >= 0 && value <= 2 {
			p.state.lineJoin = value
		}
	case "M":
		if value := floatArg(0); value >= 1 {
			p.state.miterLimit = value
		}
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
		if len(args) > 0 {
			p.state.fillSpace = p.resolveColorSpace(resources, anyName(args[0]))
			p.state.fill, p.state.fillPaint = p.state.fillSpace.initialColor(), nil
		}
	case "CS":
		if len(args) > 0 {
			p.state.strokeSpace = p.resolveColorSpace(resources, anyName(args[0]))
			p.state.stroke, p.state.strokePaint = p.state.strokeSpace.initialColor(), nil
		}
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
		p.textClip, p.textClipActive = pdfClipRegion{}, false
	case "ET":
		p.commitTextClip()
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
	case "Ts":
		p.state.textRise = floatArg(0)
	case "Tr":
		p.state.renderMode = int(floatArg(0))
	case "Tj":
		if len(args) > 0 {
			p.showText(anyBytes(args[0]), resources, depth)
		}
	case "TJ":
		if len(args) > 0 {
			if values, ok := args[0].([]any); ok {
				for _, value := range values {
					if data, ok := value.(pdfString); ok {
						p.showText([]byte(data), resources, depth)
					} else {
						advance := -anyFloat(value) / 1000 * p.state.fontSize
						if !p.fonts[p.state.fontName].vertical {
							advance *= p.state.hScale / 100
						}
						p.adjustText(advance)
					}
				}
			}
		}
	case "'":
		p.moveText(0, -p.state.textLeading)
		if len(args) > 0 {
			p.showText(anyBytes(args[0]), resources, depth)
		}
	case "\"":
		if len(args) >= 3 {
			p.state.wordSpacing = floatArg(0)
			p.state.charSpacing = floatArg(1)
			p.moveText(0, -p.state.textLeading)
			p.showText(anyBytes(args[2]), resources, depth)
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
			p.pendingClipEvenOdd = op == "W*"
		}
	case "S":
		p.paintPath(true, false, spec.FillRuleNonZero)
		p.commitPendingClip()
	case "s":
		p.path = append(p.path, pdfPathCommand{op: "Z"})
		p.paintPath(true, false, spec.FillRuleNonZero)
		p.commitPendingClip()
	case "f", "F":
		p.paintPath(false, true, spec.FillRuleNonZero)
		p.commitPendingClip()
	case "f*":
		p.paintPath(false, true, spec.FillRuleEvenOdd)
		p.commitPendingClip()
	case "B":
		p.paintPath(true, true, spec.FillRuleNonZero)
		p.commitPendingClip()
	case "B*":
		p.paintPath(true, true, spec.FillRuleEvenOdd)
		p.commitPendingClip()
	case "b":
		p.path = append(p.path, pdfPathCommand{op: "Z"})
		p.paintPath(true, true, spec.FillRuleNonZero)
		p.commitPendingClip()
	case "b*":
		p.path = append(p.path, pdfPathCommand{op: "Z"})
		p.paintPath(true, true, spec.FillRuleEvenOdd)
		p.commitPendingClip()
	case "n":
		// n 结束路径；若前面有 W/W*，裁剪区在这里生效且不绘制。
		p.commitPendingClip()
		p.path = nil
	case "Do":
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
// ISO 32000-1 8.5.4：先用旧裁剪绘制当前路径，再更新裁剪；OFD 的 Clips
// 修饰单个图元（GB/T 33190—2016 8.4、表 34），因此只附加到后续图元。
// 切片追加时复制底层数组，避免 q/Q 恢复状态后被后续裁剪污染。
func (p *pdfInterpreter) commitPendingClip() {
	if !p.hasPendingClip {
		return
	}
	p.hasPendingClip = false
	region := p.pendingClip
	evenOdd := p.pendingClipEvenOdd
	p.pendingClip = nil
	p.pendingClipEvenOdd = false
	if len(region) == 0 {
		return
	}
	clips := make([]pdfClipRegion, 0, len(p.state.clips)+1)
	clips = append(clips, p.state.clips...)
	clips = append(clips, pdfClipRegion{commands: region, evenOdd: evenOdd})
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
				if cached, hit := p.fontCache[object]; hit {
					font = cached
				} else {
					previousSource := p.setSource(object, name)
					p.loadFont(&font, object)
					p.source = previousSource
					p.fontCache[object] = font
				}
			}
		} else {
			p.report("font.resource", "找不到字体资源字典")
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
		if value.Format == font.format && bytes.Equal(value.Data, font.data) &&
			(len(font.data) > 0 || (value.FamilyName == family && value.Bold == font.bold && value.Italic == font.italic)) {
			return
		}
		// 资源名只在 PDF 页面内局部有效：同名资源在另一页可能指向不同的
		// 内嵌字体，因此不能按名称合并字体。
		identity := font.format + ":"
		if len(font.data) == 0 {
			identity = fmt.Sprintf("%s:%s:%t:%t", font.format, family, font.bold, font.italic)
		}
		digest := sha256.Sum256(append([]byte(identity), font.data...))
		documentName = name + "-" + hex.EncodeToString(digest[:])[:12]
		break
	}
	_, _, serif, fixedWidth := pdfFontStyleFlags(family)
	current := creator.Font{Name: documentName, FamilyName: family, Charset: "unicode", Format: font.format, Data: font.data, Bold: font.bold, Italic: font.italic, Serif: serif, FixedWidth: fixedWidth}
	// 复用已有等价字体：嵌入字体按数据去重；非嵌入字体 Data 为空，摘要固定，
	// 必须按族名与样式去重，否则跨页（或注解外观）的同族逻辑字体会重复注册
	// 同名资源 "name-<摘要>" 而报错。
	for _, value := range p.document.Fonts {
		if current.Format == value.Format && len(current.Data) > 0 && bytes.Equal(current.Data, value.Data) {
			p.fontAliases[name] = value.Name
			return
		}
		if len(current.Data) == 0 && len(value.Data) == 0 && current.FamilyName == value.FamilyName &&
			current.Bold == value.Bold && current.Italic == value.Italic {
			p.fontAliases[name] = value.Name
			return
		}
	}
	p.document.Fonts = append(p.document.Fonts, current)
	if documentName != name {
		p.fontAliases[name] = documentName
	}
}
