package canvasfont

import (
	"github.com/zc310/ofd/internal/cache"
	"github.com/zc310/ofd/internal/render/drawing"
	"github.com/zc310/ofd/internal/thirdparty/canvas"
	"github.com/zc310/ofd/internal/thirdparty/font"
	"github.com/zc310/ofd/pkg/spec"
)

type systemStyleKey struct {
	current *canvas.FontFamily
	id      spec.StRefID
	style   drawing.FontStyle
}

// 逻辑字体的文字样式优先匹配系统中的原生字体面，再退回轮廓合成。
// 不替换内嵌字体或用户注册字体，也不改变带实际 GID 的资源。
func (p *Fonts) systemObjectFamily(current *canvas.FontFamily, object spec.TextObject, style drawing.FontStyle) *canvas.FontFamily {
	if p == nil || p.Document == nil || !systemFontLookupUsable() ||
		(object.Weight <= 0 || object.Weight == 400) && !object.Italic {
		return current
	}
	p.mu.Lock()
	resource := p.GetFont(spec.StID(object.Font))
	if p.isClosed() || resource == nil || resource.FontFile != "" ||
		p.Fonts[object.Font] == nil || p.fallbackByFont[object.Font] != "" ||
		len(p.glyphMappings[object.Font]) > 0 {
		p.mu.Unlock()
		return current
	}
	key := systemStyleKey{current: current, id: object.Font, style: style}
	var names []string
	if p.systemStyleCandidates != nil {
		names, _ = p.systemStyleCandidates.Get(key)
	}
	generation := p.generation
	p.mu.Unlock()
	if names == nil {
		// FontName 可能精确指向 Regular 面；对象请求 Bold 时，应先在该面
		// 所属的真实字体族中找原生样式，不能一直停在完整名对应的常规面。
		if face := probeFamilyFace(current, cjkProbeSize, style); face != nil && face.Font.SFNT != nil && face.Font.SFNT.Name != nil {
			for _, id := range []font.NameID{font.NamePreferredFamily, font.NameFontFamily} {
				for _, record := range face.Font.SFNT.Name.Get(id) {
					names = append(names, record.String())
				}
			}
		}
		names = append(names, systemFontCandidates(resource)...)
		p.mu.Lock()
		if !p.isClosed() && generation == p.generation {
			if p.systemStyleCandidates == nil {
				p.systemStyleCandidates = cache.NewLRU[systemStyleKey, []string](128, nil)
			}
			p.systemStyleCandidates.Add(key, names)
		}
		p.mu.Unlock()
	}
	// 只缓存候选名称，不缓存依赖文字内容的最终选择；每个对象独立检查覆盖。
	// 磁盘字体加载仍复用系统缓存，避免为每段文字重新解析字体。
	for _, name := range names {
		if family, ok := loadCachedSystemFont(p, name, style); ok && p.familyCoversText(family, object, style) {
			return family
		}
	}
	return current
}
