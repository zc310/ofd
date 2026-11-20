package canvasfont

import (
	"strings"
	"unsafe"
	"weak"

	"github.com/zc310/ofd/internal/thirdparty/canvas"
)

const (
	textCoverageCacheEntries  = 4096
	textCoverageCacheBytes    = 1 << 20
	textCoverageMaxValueBytes = 4096
)

// textCoverageKey 按实际选中的字体与单个 TextCode 的文字缓存覆盖结果。字号、填充色、
// 合成粗斜体不改变 cmap；不同样式选中不同字体、字形修复或回退字体替换都会产生新键。
// 弱键不延长字体生命周期，文字单独复制以免小片段钉住整页内容。
type textCoverageKey struct {
	font  weak.Pointer[canvas.Font]
	value string
}

// fontCoversValue 只复用确定的覆盖结果，不缓存回退选择；每个文字对象仍按自己的
// 内容、等宽声明和当前回退注册表决策，避免一次命中让整族永久跳过后续缺字检查。
func (p *Fonts) fontCoversValue(loadedFont *canvas.Font, value string) bool {
	if value == "" {
		return true
	}
	if p == nil || p.isClosed() || len(value) > textCoverageMaxValueBytes {
		return fontCoversValue(loadedFont, value)
	}
	key := textCoverageKey{font: weak.Make(loadedFont), value: value}
	p.coverageMu.Lock()
	covered, ok := p.coverageCache.Get(key)
	p.coverageMu.Unlock()
	if ok {
		return covered
	}
	covered = fontCoversValue(loadedFont, value)
	p.coverageMu.Lock()
	defer p.coverageMu.Unlock()
	if !p.isClosed() && !p.fontRetired(loadedFont) {
		// 并发计算同一段文字时沿用已发布条目，避免重复复制文本。
		if previous, exists := p.coverageCache.Get(key); exists {
			return previous
		}
		key.value = strings.Clone(value)
		p.coverageCache.AddWeighted(key, covered, textCoverageCacheEntries, textCoverageCacheBytes,
			int64(len(value))+int64(unsafe.Sizeof(key))+64)
	}
	return covered
}

// fontCoversValue 用有界集合对当前文字去重。集合满后仍检查全部后续字符，
// 只是不再记录新字符；超长文字可以不缓存，但不能截断检查范围。
func fontCoversValue(loadedFont *canvas.Font, value string) bool {
	seen := make(map[rune]bool, 32)
	for _, r := range value {
		if seen[r] {
			continue
		}
		if loadedFont.GlyphIndex(r) == 0 {
			return false
		}
		if len(seen) < textCoverageDedupLimit {
			seen[r] = true
		}
	}
	return true
}
