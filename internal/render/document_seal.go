package render

import (
	"bytes"
	"crypto/sha256"
	"image"
	"image/color"
	"image/draw"
	"math"
	"sync"

	"github.com/h2non/filetype"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
)

func (p *Document) Seal(ctx DrawContext, info *parser.SealInfo, pb models.StBox) error {
	return p.seal(ctx, info, pb)
}

func (p *Document) seal(ctx DrawContext, info *parser.SealInfo, pb models.StBox) error {
	if info == nil || info.SealData == nil || info.StampAnnot == nil {
		return nil
	}
	if isSVGFormat(info.SealData.FileType, "") {
		return p.drawSVGSeal(ctx, info, pb)
	}
	if filetype.IsImage(info.SealData.Data) {
		return p.drawImageSeal(ctx, info, pb)
	}
	if info.SealData.FileType == "ofd" {
		return p.drawOFDSeal(ctx, info, pb)
	}
	return nil
}

// drawImageSeal 解码并绘制图片格式的印章。
func (p *Document) drawImageSeal(ctx DrawContext, info *parser.SealInfo, pb models.StBox) error {
	img, _, err := image.Decode(bytes.NewReader(info.SealData.Data))
	if err != nil {
		return err
	}
	return p.drawRasterSeal(ctx, info, pb, img)
}

// sealBackgroundCutoff 是判定印章纸张背景的通道阈值：R/G/B 均不低于该值
// 视为白色背景。
const sealBackgroundCutoff = 250

// sealTransparentBackground 把不带透明通道的印章位图中的白色背景置为透明。
// 电子印章是“墨水印记”，部分制章工具导出的图片没有透明通道、背景被压成不
// 透明白色，直接叠加会在页面内容上留下白块（真实印章应透出底下的内容）。
// 带透明通道的印章（绝大多数）保持原样，避免误删印章自身的不透明白色元素。
func sealTransparentBackground(img image.Image) image.Image {
	opaque, ok := img.(interface{ Opaque() bool })
	if !ok || !opaque.Opaque() {
		return img
	}
	b := img.Bounds()
	dst := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if r>>8 >= sealBackgroundCutoff && g>>8 >= sealBackgroundCutoff && bl>>8 >= sealBackgroundCutoff {
				continue // 透明像素为零值，无需写入
			}
			dst.SetNRGBA(x, y, color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(bl >> 8), A: uint8(a >> 8)})
		}
	}
	return dst
}

// drawRasterSeal 按 StampAnnot.Boundary 把印章位图绘制到页面上；存在
// StampAnnot.Clip 时只绘制印章落在裁剪窗口内的部分。
func (p *Document) drawRasterSeal(ctx DrawContext, info *parser.SealInfo, pb models.StBox, img image.Image) error {
	img = sealTransparentBackground(img)

	box := info.StampAnnot.Boundary
	if dst, ok := sealClipBox(box, info.StampAnnot.Clip); ok {
		img = cropSealImage(img, box, dst)
		box = dst
	}
	imgBounds := img.Bounds()
	if imgBounds.Empty() {
		return nil
	}
	ctx.Push()
	defer ctx.Pop()
	ctx.Translate(box.X, pb.Height-(box.Y+box.Height))
	ctx.Scale(box.Width/float64(imgBounds.Dx()), box.Height/float64(imgBounds.Dy()))
	ctx.DrawImage(img, 0, 0, 1)
	return nil
}

// sealClipBox 把 StampAnnot.Clip 归一化为印章 Boundary 坐标系内的目标框。
// Clip 是印章图上的裁剪窗口：骑缝章把同一枚印章按页切成若干条，每页用不同的
// Clip 只显示其中一片（例如 h.ofd 的 5 页各取 8mm，统一贴在页面右缘拼回完整
// 印章）。Clip 缺省（全零）或无效时返回 ok=false，按整枚印章绘制。
func sealClipBox(boundary, clip models.StBox) (models.StBox, bool) {
	if boundary.Width <= 0 || boundary.Height <= 0 {
		return models.StBox{}, false
	}
	if clip.Width <= 0 || clip.Height <= 0 {
		return models.StBox{}, false
	}
	// 裁剪窗口越出 Boundary 时先夹回 Boundary，避免盖住印章盒以外的页面内容。
	x0 := math.Max(clip.X, 0)
	y0 := math.Max(clip.Y, 0)
	x1 := math.Min(clip.X+clip.Width, boundary.Width)
	y1 := math.Min(clip.Y+clip.Height, boundary.Height)
	if x1 <= x0 || y1 <= y0 {
		return models.StBox{}, false
	}
	return models.StBox{
		X:      boundary.X + x0,
		Y:      boundary.Y + y0,
		Width:  x1 - x0,
		Height: y1 - y0,
	}, true
}

// cropSealImage 按目标框在 Boundary 坐标系中的位置裁剪印章位图。dst 必须是
// boundary 的子区域；印章位图顶部对应 Boundary 顶部，故源图按同一比例取子
// 矩形，裁剪后保持原分辨率。
func cropSealImage(img image.Image, boundary, dst models.StBox) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || boundary.Width <= 0 || boundary.Height <= 0 {
		return img
	}
	sx := func(v float64) int { return clampInt(int(math.Round(float64(b.Min.X)+v)), b.Min.X, b.Max.X) }
	sy := func(v float64) int { return clampInt(int(math.Round(float64(b.Min.Y)+v)), b.Min.Y, b.Max.Y) }
	x0 := sx((dst.X - boundary.X) / boundary.Width * float64(w))
	y0 := sy((dst.Y - boundary.Y) / boundary.Height * float64(h))
	x1 := sx((dst.X + dst.Width - boundary.X) / boundary.Width * float64(w))
	y1 := sy((dst.Y + dst.Height - boundary.Y) / boundary.Height * float64(h))
	if x0 >= x1 || y0 >= y1 {
		return img
	}
	if x0 == b.Min.X && y0 == b.Min.Y && x1 == b.Max.X && y1 == b.Max.Y {
		return img // 整枚印章，无需复制
	}
	region := image.Rect(x0, y0, x1, y1)
	cropped := image.NewNRGBA(image.Rect(0, 0, region.Dx(), region.Dy()))
	draw.Draw(cropped, cropped.Bounds(), img, region.Min, draw.Src)
	return cropped
}

// clampInt 把 v 夹到 [lo, hi]。
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// sealDocEntry 是一个已解析并加载好字体的 OFD 印章文档。印章在同一文档的
// 多页/多次渲染中通常保持不变，缓存它可避免每次渲染都重新解包印章并重新
// 解析其内嵌字体（实测占印章页渲染耗时的近一半）。mu 串行化对共享文档的
// 绘制，保证平行渲染安全。
type sealDocEntry struct {
	mu  sync.Mutex
	ofd *parser.OFD
	doc *Document
}

// sealDocument 返回印章对应的缓存文档，不存在时解析并缓存。key 由印章
// 数据与父文档回退字体列表共同决定，确保回退字体变化时不会复用旧文档。
func (p *Document) sealDocument(data []byte) (*sealDocEntry, error) {
	key := sealCacheKey(data, p.fallbackFontFamilies())

	p.sealMu.Lock()
	entry := p.sealDocs[key]
	p.sealMu.Unlock()
	if entry != nil {
		return entry, nil
	}

	var ofd parser.OFD
	if err := ofd.Open(data); err != nil {
		return nil, err
	}
	if len(ofd.Documents) == 0 || ofd.Documents[0].PageCount() == 0 {
		_ = ofd.Close()
		return nil, nil
	}
	doc := NewDocument(color.Transparent, ofd.Documents[0])
	for _, family := range p.fallbackFontFamilies() {
		if err := doc.UseFallbackFont(family); err != nil {
			_ = ofd.Close()
			return nil, err
		}
	}

	entry = &sealDocEntry{ofd: &ofd, doc: doc}
	p.sealMu.Lock()
	if existing := p.sealDocs[key]; existing != nil {
		p.sealMu.Unlock()
		_ = ofd.Close()
		return existing, nil
	}
	if p.sealDocs == nil {
		p.sealDocs = make(map[[32]byte]*sealDocEntry)
	}
	p.sealDocs[key] = entry
	p.sealMu.Unlock()
	return entry, nil
}

// sealCacheKey 计算印章缓存键：印章数据 + 父文档回退字体列表。
func sealCacheKey(data []byte, fallbacks []string) [32]byte {
	h := sha256.New()
	_, _ = h.Write(data)
	for _, family := range fallbacks {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(family))
	}
	var key [32]byte
	copy(key[:], h.Sum(nil))
	return key
}

// drawOFDSeal 解码并绘制 OFD 格式的印章。
func (p *Document) drawOFDSeal(ctx DrawContext, info *parser.SealInfo, pb models.StBox) error {
	entry, err := p.sealDocument(info.SealData.Data)
	if err != nil || entry == nil {
		return err
	}
	// 缓存文档可能被并行渲染复用，串行化本印章的绘制。
	entry.mu.Lock()
	defer entry.mu.Unlock()

	page, err := entry.ofd.Documents[0].GetPage(0)
	if err != nil {
		return err
	}
	lease, err := page.AcquireLease()
	if err != nil {
		return err
	}
	defer lease.Release()
	content := lease.Content()
	if content == nil || content.Area == nil {
		return nil
	}
	sealBox := content.Area.PhysicalBox
	if sealBox.Width <= 0 || sealBox.Height <= 0 {
		return nil
	}
	box := info.StampAnnot.Boundary
	if _, ok := sealClipBox(box, info.StampAnnot.Clip); ok {
		// 矢量印章页没有可用的裁剪接口，带 Clip 时先把印章页栅格化再交给位图
		// 路径，与 SVG 印章共用同一套 Clip 坐标映射。
		img := rasterSealPage(entry.doc, page, sealBox, box)
		if img == nil {
			return nil
		}
		return p.drawRasterSeal(ctx, info, pb, img)
	}
	ctx.Push()
	defer ctx.Pop()
	ctx.Translate(box.X, pb.Height-(box.Y+box.Height))
	ctx.Scale(box.Width/sealBox.Width, box.Height/sealBox.Height)
	var budget renderBudget
	budget.reset()
	entry.doc.pageContent(ctx, page, false, &budget)
	return nil
}

// rasterSealPage 按印章页自身尺寸把矢量印章栅格化为透明位图，分辨率由
// sealRasterResolution 选取。离屏表面未注册时返回 nil，调用方跳过绘制。
func rasterSealPage(doc *Document, page *parser.Page, sealBox, box models.StBox) image.Image {
	surface := newOffscreenSurface(sealBox.Width, sealBox.Height, sealRasterResolution(box, sealBox.Width, sealBox.Height))
	if surface == nil {
		return nil
	}
	var budget renderBudget
	budget.reset()
	doc.pageContent(surface, page, false, &budget)
	return surface.Raster()
}

// drawSVGSeal 解析并绘制 SVG 格式的印章。
// 先栅格化为位图再复用位图印章的坐标映射，保证矢量方向与 PNG/JPG 印章一致；
// 栅格化分辨率按印章盒尺寸取 96dpi 以上，保持边缘清晰。
func (p *Document) drawSVGSeal(ctx DrawContext, info *parser.SealInfo, pb models.StBox) error {
	scene, err := parseSVGScene(info.SealData.Data)
	if err != nil {
		return err
	}
	if scene == nil || scene.Width() <= 0 || scene.Height() <= 0 {
		return nil
	}
	img := scene.Rasterize(sealRasterResolution(info.StampAnnot.Boundary, scene.Width(), scene.Height()))
	if img == nil || img.Bounds().Empty() {
		return nil
	}
	return p.drawRasterSeal(ctx, info, pb, img)
}

// sealRasterResolution 选择栅格化分辨率：按印章盒的毫米尺寸换算为像素，
// 使 SVG 的短边至少达到 canvasDPI 像素，避免放大后模糊。
func sealRasterResolution(box models.StBox, svgW, svgH float64) geom.Resolution {
	const canvasDPI = 192.0
	shortSide := math.Min(box.Width, box.Height)
	reference := math.Min(svgW, svgH)
	if shortSide <= 0 || reference <= 0 {
		return geom.DPI(canvasDPI)
	}
	// 1 画布单位对应 shortSide/reference 毫米，使短边渲染出 canvasDPI 像素。
	perMM := canvasDPI / shortSide
	return geom.Resolution(perMM)
}
