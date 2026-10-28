package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"math"
	"sync"

	"github.com/h2non/filetype"
	"github.com/zc310/ofd/internal/cache"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/render/imageasset"
	"github.com/zc310/ofd/internal/render/imageops"
	seallayout "github.com/zc310/ofd/internal/render/layout/seal"
)

func (p *Document) Seal(ctx DrawContext, info *parser.SealInfo, pb models.StBox) error {
	return p.seal(ctx, info, pb)
}

func (p *Document) seal(ctx DrawContext, info *parser.SealInfo, pb models.StBox) error {
	return p.sealWithBudget(ctx, info, pb, nil)
}

func (p *Document) sealWithBudget(ctx DrawContext, info *parser.SealInfo, pb models.StBox, parent *renderBudget) error {
	if info == nil || info.SealData == nil || info.StampAnnot == nil {
		return nil
	}
	if imageasset.IsSVG(info.SealData.FileType, "") {
		return p.drawSVGSeal(ctx, info, pb)
	}
	if filetype.IsImage(info.SealData.Data) {
		return p.drawImageSeal(ctx, info, pb)
	}
	if info.SealData.FileType == "ofd" {
		return p.drawOFDSealBudget(ctx, info, pb, parent)
	}
	return nil
}

// drawImageSeal 解码并绘制图片格式的印章。
func (p *Document) drawImageSeal(ctx DrawContext, info *parser.SealInfo, pb models.StBox) error {
	img, err := p.decodeSealImage(info.SealData.Data)
	if err != nil {
		return err
	}
	return p.drawPreparedRasterSeal(ctx, info, pb, img)
}

// decodeSealImage 按内容缓存完整印章，解码与白底透明化只在缓存未命中时执行。
// NUL 前缀将内存印章键与文档资源路径隔离；不同页面的 Clip 不参与缓存键。
func (p *Document) decodeSealImage(data []byte) (image.Image, error) {
	digest := sha256.Sum256(data)
	key := "\x00seal-image:" + string(digest[:])
	return p.cachedImage(key, func() (image.Image, error) {
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return imageops.SealTransparentBackground(img), nil
	})
}

// drawRasterSeal 按 StampAnnot.Boundary 把印章位图绘制到页面上；存在
// StampAnnot.Clip 时只绘制印章落在裁剪窗口内的部分。
func (p *Document) drawRasterSeal(ctx DrawContext, info *parser.SealInfo, pb models.StBox, img image.Image) error {
	return p.drawPreparedRasterSeal(ctx, info, pb, imageops.SealTransparentBackground(img))
}

// drawPreparedRasterSeal 绘制已完成透明背景处理的印章，只为当前页裁剪和放置。
func (p *Document) drawPreparedRasterSeal(ctx DrawContext, info *parser.SealInfo, pb models.StBox, img image.Image) error {
	box := info.StampAnnot.Boundary
	if dst, ok := seallayout.ClipBox(box, info.StampAnnot.Clip); ok {
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

// cropSealImage 按目标框在 Boundary 坐标系中的位置裁剪印章位图。dst 必须是
// boundary 的子区域；印章位图顶部对应 Boundary 顶部，故源图按同一比例取子
// 矩形，裁剪后保持原分辨率。
func cropSealImage(img image.Image, boundary, dst models.StBox) image.Image {
	region, ok := seallayout.CropRect(img.Bounds(), boundary, dst)
	if !ok {
		return img
	}
	return imageops.CropNRGBA(img, region)
}

// sealDocEntry 是一个已解析并加载好字体的 OFD 印章文档。印章在同一文档的
// 多页/多次渲染中通常保持不变，缓存它可避免每次渲染都重新解包印章并重新
// 解析其内嵌字体（实测占印章页渲染耗时的近一半）。mu 的读锁保护在途绘制的
// 生命周期，各页可并行读取；关闭通过写锁等待所有绘制结束。
type sealDocEntry struct {
	mu     sync.RWMutex
	ofd    *parser.OFD
	doc    *Document
	closed bool
}

func (entry *sealDocEntry) close() {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.closed {
		return
	}
	entry.closed = true
	entry.doc.Close()
	_ = entry.ofd.Close()
}

// sealDocument 返回印章对应的缓存文档，不存在时解析并缓存。key 由印章
// 数据与父文档回退字体列表共同决定，确保回退字体变化时不会复用旧文档。
func (p *Document) sealDocument(data []byte) (*sealDocEntry, error) {
	return p.loadSealDocument(data, false)
}

const sealDocCacheCapacity = 16

// pin 为真时在缓存锁内取得条目的读锁，调用方绘制后必须 RUnlock。
// 这保证条目从查找到开始绘制之间不会被 LRU 淘汰并提前关闭。
func (p *Document) loadSealDocument(data []byte, pin bool) (*sealDocEntry, error) {
	fallbacks := p.fallbackFontFamilies()
	key := sealCacheKey(data, fallbacks)

	p.sealMu.Lock()
	if p.sealClosed {
		p.sealMu.Unlock()
		return nil, errors.New("渲染文档已关闭")
	}
	if p.sealDocs != nil {
		if entry, ok := p.sealDocs.Get(key); ok {
			if pin {
				entry.mu.RLock()
			}
			p.sealMu.Unlock()
			return entry, nil
		}
	}
	p.sealMu.Unlock()

	var ofd parser.OFD
	if err := ofd.Open(data); err != nil {
		return nil, err
	}
	if len(ofd.Documents) == 0 || ofd.Documents[0].PageCount() == 0 {
		_ = ofd.Close()
		return nil, nil
	}
	doc := NewDocument(color.Transparent, ofd.Documents[0])
	for _, family := range fallbacks {
		if err := doc.UseFallbackFont(family); err != nil {
			doc.Close()
			_ = ofd.Close()
			return nil, err
		}
	}

	entry := &sealDocEntry{ofd: &ofd, doc: doc}
	p.sealMu.Lock()
	if p.sealClosed {
		p.sealMu.Unlock()
		entry.close()
		return nil, errors.New("渲染文档已关闭")
	}
	if p.sealDocs == nil {
		p.sealDocs = cache.NewLRU[[32]byte, *sealDocEntry](sealDocCacheCapacity, func(_ [32]byte, entry *sealDocEntry) {
			// close 等待正在绘制该印章的读锁，字体和解析资源一并释放。
			entry.close()
		})
	}
	if existing, ok := p.sealDocs.Get(key); ok {
		if pin {
			existing.mu.RLock()
		}
		p.sealMu.Unlock()
		entry.close()
		return existing, nil
	}
	p.sealDocs.Add(key, entry)
	if pin {
		entry.mu.RLock()
	}
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
	h.Sum(key[:0])
	return key
}

// drawOFDSeal 解码并绘制 OFD 格式的印章。
func (p *Document) drawOFDSeal(ctx DrawContext, info *parser.SealInfo, pb models.StBox) error {
	return p.drawOFDSealBudget(ctx, info, pb, nil)
}

func (p *Document) drawOFDSealBudget(ctx DrawContext, info *parser.SealInfo, pb models.StBox, parent *renderBudget) error {
	entry, err := p.loadSealDocument(info.SealData.Data, true)
	if err != nil || entry == nil {
		return err
	}
	// 共享读锁只阻止提前关闭，不串行化同一印章的各页绘制。
	defer entry.mu.RUnlock()
	if entry.closed {
		return errors.New("印章文档已关闭")
	}

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
	if _, ok := seallayout.ClipBox(box, info.StampAnnot.Clip); ok {
		// 矢量印章页没有可用的裁剪接口，带 Clip 时先把印章页栅格化再交给位图
		// 路径，与 SVG 印章共用同一套 Clip 坐标映射。
		img := rasterSealPageBudget(entry.doc, page, sealBox, box, parent)
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
	if parent != nil {
		budget.Report = parent.Report
	}
	entry.doc.pageContent(ctx, page, false, &budget)
	return nil
}

// rasterSealPage 按印章页自身尺寸把矢量印章栅格化为透明位图，分辨率由
// sealRasterResolution 选取。离屏表面未注册时返回 nil，调用方跳过绘制。
func rasterSealPage(doc *Document, page *parser.Page, sealBox, box models.StBox) image.Image {
	return rasterSealPageBudget(doc, page, sealBox, box, nil)
}

func rasterSealPageBudget(doc *Document, page *parser.Page, sealBox, box models.StBox, parent *renderBudget) image.Image {
	surface := newOffscreenSurface(sealBox.Width, sealBox.Height, sealRasterResolution(box, sealBox.Width, sealBox.Height))
	if surface == nil {
		return nil
	}
	var budget renderBudget
	budget.reset()
	if parent != nil {
		budget.Report = parent.Report
	}
	doc.pageContent(surface, page, false, &budget)
	return surface.Raster()
}

// drawSVGSeal 解析并绘制 SVG 格式的印章。
// 先栅格化为位图再复用位图印章的坐标映射，保证矢量方向与 PNG/JPG 印章一致；
// 栅格化分辨率按印章盒尺寸取 96dpi 以上，保持边缘清晰。
func (p *Document) drawSVGSeal(ctx DrawContext, info *parser.SealInfo, pb models.StBox) error {
	img, err := p.decodeSVGSealImage(info.SealData.Data, info.StampAnnot.Boundary)
	if err != nil {
		return err
	}
	if img == nil || img.Bounds().Empty() {
		return nil
	}
	return p.drawPreparedRasterSeal(ctx, info, pb, img)
}

// decodeSVGSealImage 复用完整印章位图，Clip 和页面位置只在绘制时施加。
// 实际分辨率参与图片键，场景解析结果另受 SVG 缓存预算限制。
func (p *Document) decodeSVGSealImage(data []byte, box models.StBox) (image.Image, error) {
	digest := sha256.Sum256(data)
	sceneKey := "\x00seal-svg:" + string(digest[:])
	scene, err := p.cachedSVGScene(sceneKey, func() ([]byte, error) { return data, nil })
	if err != nil {
		return nil, err
	}
	if scene == nil || scene.Width() <= 0 || scene.Height() <= 0 {
		return nil, nil
	}
	resolution := sealRasterResolution(box, scene.Width(), scene.Height())
	var bits [8]byte
	binary.LittleEndian.PutUint64(bits[:], math.Float64bits(float64(resolution)))
	key := "\x00seal-svg-raster:" + string(digest[:]) + string(bits[:])
	return p.cachedImage(key, func() (image.Image, error) {
		img := scene.Rasterize(resolution)
		if img == nil || img.Bounds().Empty() {
			return img, nil
		}
		return imageops.SealTransparentBackground(img), nil
	})
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
