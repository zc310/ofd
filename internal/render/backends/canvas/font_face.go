package canvas

import (
	"log/slog"

	"github.com/zc310/canvas"
	"github.com/zc310/font"

	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render/backends/canvas/canvasconv"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/render/geom"
)

// 本文件实现 canvas 字体面适配 canvasFontFace（ToPath/DirectPath/TextWidth/
// GlyphIndex/Fill/ShapedRun 等），以及 Fonts.FaceObject、buildTextFace、
// textFontStyle、directTextPath 与中性/ canvas 字体样式转换。

// shapedTextPath 缓存按字体、字号、样式与文本计算的轮廓；返回路径只读，
// 跨对象复用时不能原地变换。缓存有界，关闭后不再保存计算结果。
func (p *Fonts) shapedTextPath(face *canvas.FontFace, value string) *canvas.Path {
	if face == nil || face.Font == nil || value == "" || p.isClosed() {
		return nil
	}
	key := fontPathKey{font: face.Font, size: face.Size, style: drawing.FontStyle(face.Style), variant: face.Variant, value: value}
	p.pathCacheMu.Lock()
	if p.pathCache != nil {
		if path := p.pathCache[key]; path != nil {
			p.pathCacheMu.Unlock()
			return path
		}
	}
	p.pathCacheMu.Unlock()
	path, _ := face.ToPath(value)
	if path == nil || path.Empty() {
		return nil
	}
	p.pathCacheMu.Lock()
	if p.isClosed() {
		p.pathCacheMu.Unlock()
		return path
	}
	if existing := p.pathCache[key]; existing != nil {
		p.pathCacheMu.Unlock()
		return existing
	}
	if len(p.pathCache) >= 2048 {
		p.pathCache = make(map[fontPathKey]*canvas.Path)
	} else if p.pathCache == nil {
		p.pathCache = make(map[fontPathKey]*canvas.Path)
	}
	p.pathCache[key] = path
	p.pathCacheMu.Unlock()
	return path
}

// shapedTextLine 返回 (face, value) 经 canvas 原生整形的文字行，带缓存。
// canvas 后端的 DrawText 每次 NewTextLine 都会重新整形/分项，是明显热点；
// 相同字体数据 + 字号 + 样式 + 纯色画笔 + 文本的整形成果完全一致，直接复用。
// 渐变/图案画笔的文字不缓存，避免不同文本对象互相污染。返回的 canvas.Text
// 只被读取渲染，跨对象复用安全；缓存有界，超出后整体清空。
func (p *Fonts) shapedTextLine(face *canvas.FontFace, value string) *canvas.Text {
	if face == nil || face.Font == nil || value == "" {
		return nil
	}
	if !face.Fill.IsColor() {
		return canvas.NewTextLine(face, value, canvas.Left)
	}
	key := textLineKey{
		font:    face.Font,
		size:    face.Size,
		style:   drawing.FontStyle(face.Style),
		variant: face.Variant,
		fill:    face.Fill.Color,
		hinting: face.Hinting,
		value:   value,
	}
	p.textLineMu.Lock()
	if p.textLineCache != nil {
		if line := p.textLineCache[key]; line != nil {
			p.textLineMu.Unlock()
			return line
		}
	}
	p.textLineMu.Unlock()
	line := canvas.NewTextLine(face, value, canvas.Left)
	p.textLineMu.Lock()
	if p.isClosed() {
		p.textLineMu.Unlock()
		return line
	}
	if existing := p.textLineCache[key]; existing != nil {
		p.textLineMu.Unlock()
		return existing
	}
	if len(p.textLineCache) >= 2048 {
		p.textLineCache = make(map[textLineKey]*canvas.Text)
	} else if p.textLineCache == nil {
		p.textLineCache = make(map[textLineKey]*canvas.Text)
	}
	p.textLineCache[key] = line
	p.textLineMu.Unlock()
	return line
}

type canvasFontFace struct {
	fonts *Fonts
	face  *canvas.FontFace
}

func (f canvasFontFace) ToPath(value string) *geom.Path {
	if f.fonts != nil {
		if path := f.fonts.shapedTextPath(f.face, value); path != nil {
			return canvasconv.FromCanvasPath(path)
		}
		return nil
	}
	path, _ := f.face.ToPath(value)
	if path == nil || path.Empty() {
		return nil
	}
	return canvasconv.FromCanvasPath(path)
}

func (f canvasFontFace) DirectPath(value string) *geom.Path {
	path := f.fonts.directTextPath(f.face, value)
	if path == nil {
		return nil
	}
	return canvasconv.FromCanvasPath(path)
}

// maxTextWidthCacheEntries 限制文字宽度缓存条目数，超出后整体清空。
const maxTextWidthCacheEntries = 4096

// cachedTextWidth 返回文本的排版宽度，带缓存。
//
// canvas 的 TextWidth 每次调用都会重新做一次 harfbuzz 整形，而 OFD 排版需要
// 逐字测量步进宽度：y.ofd 的 588 个文字图元每次排版都要为每个字形整形一次，
// 重复整形一度占渲染 CPU 的四分之一。相同字体数据 + 字号 + 样式 + 书写方向
// + 文本的宽度完全一致，直接复用。缓存有界，超出后整体清空。
func (p *Fonts) cachedTextWidth(face *canvas.FontFace, value string) float64 {
	if face == nil {
		return 0
	}
	if face.Font == nil || value == "" {
		return face.TextWidth(value)
	}
	key := textWidthKey{
		font:      face.Font,
		size:      face.Size,
		style:     drawing.FontStyle(face.Style),
		variant:   face.Variant,
		direction: face.Direction,
		script:    face.Script,
		language:  face.Language,
		value:     value,
	}
	p.textWidthMu.Lock()
	if width, ok := p.textWidthCache[key]; ok {
		p.textWidthMu.Unlock()
		return width
	}
	p.textWidthMu.Unlock()
	width := face.TextWidth(value)
	p.textWidthMu.Lock()
	if p.isClosed() {
		p.textWidthMu.Unlock()
		return width
	}
	if existing, ok := p.textWidthCache[key]; ok {
		p.textWidthMu.Unlock()
		return existing
	}
	if p.textWidthCache == nil || len(p.textWidthCache) >= maxTextWidthCacheEntries {
		p.textWidthCache = make(map[textWidthKey]float64)
	}
	p.textWidthCache[key] = width
	p.textWidthMu.Unlock()
	return width
}

func (f canvasFontFace) TextWidth(value string) float64 {
	if f.fonts != nil {
		return f.fonts.cachedTextWidth(f.face, value)
	}
	return f.face.TextWidth(value)
}

func (f canvasFontFace) GlyphIndex(r rune) uint16 {
	if f.face == nil || f.face.Font == nil {
		return 0
	}
	return f.face.Font.GlyphIndex(r)
}

func (f canvasFontFace) IsCFF() bool {
	return f.face != nil && f.face.Font != nil && f.face.Font.SFNT != nil && f.face.Font.SFNT.IsCFF
}

func (f canvasFontFace) Fill() geom.Paint { return canvasconv.FromCanvasPaint(f.face.Fill) }

func (f canvasFontFace) Size() float64 { return f.face.Size }

// VerticalMetrics 返回字体面的上伸与下延（毫米），用于文字层与搜索高亮的
// 统一排版行框；这两个字体级度量不等于单个字形的实际轮廓边界。
func (f canvasFontFace) VerticalMetrics() (float64, float64) {
	if f.face == nil {
		return 0, 0
	}
	metrics := f.face.Metrics()
	return metrics.Ascent, metrics.Descent
}

func (f canvasFontFace) ShapedRun(value string) drawing.TextRun {
	if f.fonts == nil {
		return nil
	}
	if line := f.fonts.shapedTextLine(f.face, value); line != nil {
		return line
	}
	return nil
}

func (f canvasFontFace) InvisibleShapedRun(value string) drawing.TextRun {
	if f.face == nil || value == "" {
		return nil
	}
	face := *f.face
	face.Fill = canvas.Paint{}
	return canvas.NewTextLine(&face, value, canvas.Left)
}

// FaceObject 为字体族与文字对象创建一个中性字体面。fill 为文字填充色/渐变。
//
// 选中的字体族画不出对象里的某些字符时（例如 sans-serif 被 fontconfig 解析成
// Latin-only 的 DejaVu Sans，而正文是中文），换一个真能画出来的系统字体。
// FontFamily 没有逐字形回退，同一 style 再载入字体只会覆盖前一个，因此只能
// 整族替换。渲染与文字层度量都经由本函数，替换决策一致，不会出现「按 A 字体
// 度量、按 B 字体绘制」导致的文字层错位。
func (p *Fonts) FaceObject(family drawing.FontFamily, object models.TextObject, fill *drawing.CTColor) drawing.FontFace {
	cf, ok := family.(*canvas.FontFamily)
	if !ok || cf == nil {
		return nil
	}
	if fallback := p.cjkFallbackFamily(cf, object); fallback != nil {
		cf = fallback
	}
	face := buildTextFace(cf, object, fill)
	if face == nil {
		return nil
	}
	return canvasFontFace{fonts: p, face: face}
}

// cjkFallbackFamily 在当前字体族画不出对象文字时，另找一个能画出来的系统字体；
// 原字体族够用时返回 nil，让调用方沿用它。等宽对象先试等宽的 CJK 字体——
// 代码块里的中文注释必须保持等宽度量，否则缩进会全部错位。
//
// 找不到替代时返回 nil 而不是硬换：把拉丁文画成另一种拉丁字体，好过整段画不出来。
func (p *Fonts) cjkFallbackFamily(family *canvas.FontFamily, object models.TextObject) *canvas.FontFamily {
	style := textFontStyle(object.Weight, object.Italic)
	if familyCoversText(family, object, style) {
		return nil
	}
	if !systemFontLookupUsable() {
		// wasm 没有可枚举的系统字体，改查已注册的回退字体：浏览器里用户注册了
		// 什么就用什么兜底。此前这里直接返回 nil，于是注册的字画不出的中文
		// 只能变成一片空白——而空白在产物上看不出原因，也没有报错。
		return p.registeredFallbackCovering(family, object, style)
	}
	// 族名就是当初选中的候选名：系统字体取候选族名，等宽回退统一叫
	// "fixed-width"，内嵌字体取 OFD 里的字体名。因此按族名判断等宽够用，
	// 也避免在 OFD 没有等宽标志的情况下靠 HScale 之类的间接特征去猜。
	order := cjkFallbackFamilies
	if isFixedWidthName(family.Name()) {
		order = cjkFixedWidthFamilies
	}
	for _, name := range order {
		candidate, ok := loadCachedSystemFont(p, name, style)
		if !ok || candidate == family {
			continue
		}
		if familyCoversText(candidate, object, style) {
			slog.Debug("fallback to CJK-capable system font", "from", family.Name(), "to", name)
			return candidate
		}
	}
	return nil
}

// registeredFallbackCovering 在已注册的回退字体里找一个能画出对象文字的族。
//
// 这是 wasm 下的缺字兜底：那里没有可枚举的系统字体，可用的只有用户注册
// 进来的那几份。先挑样式对得上的，再退到任意样式——粗体用常规体画好过
// 整段画不出来。找不到就返回 nil，让调用方沿用原字体（画成豆腐块，而不是
// 整段空白）。
//
// 换字体会改变度量，所以只在原字体确实画不出时才换：文字层与绘制都经由
// FaceObject，二者看到的是同一份决策，不会出现"按 A 度量、按 B 绘制"。
func (p *Fonts) registeredFallbackCovering(
	current *canvas.FontFamily, object models.TextObject, style drawing.FontStyle,
) *canvas.FontFamily {
	p.mu.Lock()
	faces := append([]fallbackFace(nil), p.fallbackFaces...)
	p.mu.Unlock()
	// 两轮：先只要样式匹配的，再放宽到任意样式。
	for _, sameStyleOnly := range []bool{true, false} {
		for _, face := range faces {
			if face.family == nil || face.family == current {
				continue
			}
			if sameStyleOnly && face.style.CSS() != style.CSS() {
				continue
			}
			if familyCoversText(face.family, object, style) {
				slog.Debug("fallback to registered font",
					"from", current.Name(), "to", face.family.Name())
				return face.family
			}
		}
	}
	return nil
}

// buildTextFace 根据文字对象样式创建 canvas 字体面。
func buildTextFace(family *canvas.FontFamily, object models.TextObject, fill *drawing.CTColor) *canvas.FontFace {
	args := make([]any, 0, 3)
	switch {
	case fill != nil && fill.Gradient != nil:
		args = append(args, canvasconv.ToCanvasGradient(fill.Gradient))
	case fill != nil && fill.HasValue:
		args = append(args, fill.Value)
	default:
		// OFD 未指定 FillColor 时，文字填充默认为黑色。
		// 不能使用透明色，否则 PDF 渲染器会输出非法的 NaN 颜色值破坏内容流。
		args = append(args, canvas.Black)
	}
	style := textFontStyle(object.Weight, object.Italic)
	args = append(args, canvasStyle(style), canvas.FontNormal)
	if object.CTM != nil {
		// 模板常用极小字号配合CTM放大，并叠加独立字形轮廓描边。
		// 文字单独吸附像素会偏离精确定位的轮廓，低DPI下形成两层错位重影。
		args = append(args, font.NoHinting)
	}
	return family.Face(object.Size*2.83465, args...)
}

// textFontStyle 将 OFD 字重映射为 canvas 支持的标准字重。
func textFontStyle(weight int, italic bool) drawing.FontStyle {
	if weight <= 0 {
		weight = 400
	}

	style := drawing.FontRegular
	switch {
	case weight < 150:
		style = drawing.FontThin
	case weight < 250:
		style = drawing.FontExtraLight
	case weight < 350:
		style = drawing.FontLight
	case weight < 450:
		style = drawing.FontRegular
	case weight < 550:
		style = drawing.FontMedium
	case weight < 650:
		style = drawing.FontSemiBold
	case weight < 750:
		style = drawing.FontBold
	case weight < 850:
		style = drawing.FontExtraBold
	default:
		style = drawing.FontBlack
	}
	if italic {
		style |= drawing.FontItalic
	}
	return style
}

// directTextPath 处理 cmap 可通过 GlyphIndex 使用、但无法被文字整形器使用的
// 子集字体。普通文字仍会在后续使用 Canvas 的文字整形功能；这里对整形成功的
// 情形返回 nil（示意“无需按字形映射构建”），并利用缓存后的轮廓判断，避免
// 与后续 drawTextInline 重复计算同一字形轮廓。
func (p *Fonts) directTextPath(face *canvas.FontFace, value string) *canvas.Path {
	var path *canvas.Path
	if !containsPrivateGlyphRune(value) {
		path := p.shapedTextPath(face, value)
		if path != nil {
			return nil
		}
	}
	if face.Font == nil || face.Font.SFNT == nil {
		return nil
	}
	path = &canvas.Path{}
	advance := 0.0
	for _, r := range []rune(value) {
		glyphID := face.Font.GlyphIndex(r)
		if glyphID == 0 {
			return nil
		}
		if err := face.Font.GlyphPath(path, glyphID, face.PPEM(canvas.DefaultResolution), face.MmPerEm*advance, 0, face.MmPerEm, font.NoHinting); err != nil {
			return nil
		}
		advance += float64(face.Font.SFNT.GlyphAdvance(glyphID))
	}
	if path.Empty() {
		return nil
	}
	return path
}

// canvasStyle 把中性字体样式转换为 canvas 字体样式（两者取值一致）。
func canvasStyle(style drawing.FontStyle) canvas.FontStyle { return canvas.FontStyle(style) }
