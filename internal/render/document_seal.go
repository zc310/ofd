package render

import (
	"bytes"
	"crypto/sha256"
	"image"
	"image/color"
	"sync"

	"github.com/h2non/filetype"
	"github.com/tdewolff/canvas"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
)

func (p *Document) Seal(ctx *canvas.Context, info *parser.SealInfo, pb models.StBox) error {
	return p.seal(NewCanvasBackend(ctx), info, pb)
}

func (p *Document) seal(ctx DrawContext, info *parser.SealInfo, pb models.StBox) error {
	if info == nil || info.SealData == nil || info.StampAnnot == nil {
		return nil
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
	imgBounds := img.Bounds()
	if imgBounds.Empty() {
		return nil
	}

	box := info.StampAnnot.Boundary
	ctx.Push()
	defer ctx.Pop()
	ctx.Translate(box.X, pb.Height-(box.Y+box.Height))
	ctx.Scale(box.Width/float64(imgBounds.Dx()), box.Height/float64(imgBounds.Dy()))
	ctx.DrawImage(img, 0, 0, 1)
	return nil
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
	ctx.Push()
	defer ctx.Pop()
	ctx.Translate(box.X, pb.Height-(box.Y+box.Height))
	ctx.Scale(box.Width/sealBox.Width, box.Height/sealBox.Height)
	var budget renderBudget
	budget.reset()
	entry.doc.pageContent(ctx, page, false, &budget)
	return nil
}
