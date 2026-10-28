package canvasfont

import (
	"strings"

	"github.com/zc310/canvas"
	"github.com/zc310/font"
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
)

// nativeGlyphMappings 兼容未内嵌原字体的字形变换（如 z.ofd 的 Tahoma/Latha）。
// GB/T 33190—2016 11.4.1 指出 GID 依赖具体字体。只有实际 SFNT 名称匹配，且
// 文档至少给出一条可验证的 Unicode→GID 对应、全部对应均吻合时，才允许按原生
// GID 绘制。普通替代字体和其他子集仍按 Unicode 回退，不能套用原字体的编号。
func (p *Fonts) nativeGlyphMappings(face *canvas.FontFace, object models.TextObject) map[rune]uint16 {
	if p == nil || p.Document == nil || len(object.CGTransform) == 0 || face == nil || face.Font == nil || face.Font.SFNT == nil {
		return nil
	}
	resource := p.Document.GetFont(models.StID(object.Font))
	if resource == nil || resource.FontFile != "" || !matchesDeclaredFont(face, resource) {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	anchors := 0
	for r, gid := range p.glyphMappings[object.Font] {
		if fontrepair.ContainsGlyphRune(string(r)) {
			continue
		}
		if gid == 0 || face.Font.GlyphIndex(r) != gid {
			return nil
		}
		anchors++
	}
	if anchors == 0 {
		return nil
	}
	mapping := make(map[rune]uint16)
	for _, transform := range object.CGTransform {
		ids := transform.Glyphs
		if transform.GlyphCount > 0 && transform.GlyphCount < len(ids) {
			ids = ids[:transform.GlyphCount]
		}
		for _, id := range ids {
			if id > 0 && id < int(face.Font.NumGlyphs()) {
				mapping[fontrepair.GlyphRune(uint16(id))] = uint16(id)
			}
		}
	}
	return mapping
}

func matchesDeclaredFont(face *canvas.FontFace, resource *models.Font) bool {
	if face.Font.SFNT.Name == nil {
		return false
	}
	for _, id := range []font.NameID{font.NameFontFamily, font.NamePreferredFamily, font.NameFull, font.NamePostScript} {
		for _, record := range face.Font.SFNT.Name.Get(id) {
			name := strings.TrimSpace(record.String())
			if name != "" && (strings.EqualFold(name, strings.TrimSpace(resource.FontName)) || strings.EqualFold(name, strings.TrimSpace(resource.FamilyName))) {
				return true
			}
		}
	}
	return false
}

func (f canvasFontFace) nativeGlyphPath(value string) *canvas.Path {
	path := &canvas.Path{}
	advance := 0.0
	for _, r := range value {
		gid := f.GlyphIndex(r)
		if gid == 0 {
			return nil
		}
		if err := f.face.Font.GlyphPath(path, gid, f.face.PPEM(canvas.DefaultResolution), f.face.MmPerEm*advance, 0, f.face.MmPerEm, font.NoHinting); err != nil {
			return nil
		}
		advance += float64(f.face.Font.GlyphAdvance(gid))
	}
	return path
}
