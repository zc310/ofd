package canvas

import (
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/zc310/ofd/internal/cache"
	"github.com/zc310/ofd/internal/fontrepair"
	"github.com/zc310/ofd/internal/models"
)

const (
	fontSourceCapacity    = 64
	fontSourceBytes       = 32 << 20
	glyphSnapshotCapacity = 64
	glyphSnapshotBytes    = 8 << 20
)

// embeddedFontSource 属于当前文档中的只读 ZIP 资源。保留有限数量的原始数据
// 与摘要，逐页补充 cmap 时不必再次解压、分配整份字体并计算 SHA-256。
type embeddedFontSource struct {
	data   []byte
	digest [sha256.Size]byte
}

func (p *Fonts) readEmbeddedFont(path string) (*embeddedFontSource, error) {
	p.embeddedMu.Lock()
	if p.isClosed() {
		p.embeddedMu.Unlock()
		return nil, fmt.Errorf("字体上下文已关闭")
	}
	if p.embeddedSources != nil {
		if source, ok := p.embeddedSources.Get(path); ok {
			p.embeddedMu.Unlock()
			return source, nil
		}
	}
	p.embeddedMu.Unlock()
	data, err := p.FileCache.Read(path)
	if err != nil {
		return nil, err
	}
	source := &embeddedFontSource{data: data, digest: sha256.Sum256(data)}
	p.embeddedMu.Lock()
	defer p.embeddedMu.Unlock()
	if p.isClosed() {
		return nil, fmt.Errorf("字体上下文已关闭")
	}
	if p.embeddedSources == nil {
		p.embeddedSources = cache.NewWeightedLRU[string, *embeddedFontSource](fontSourceCapacity, fontSourceBytes, nil)
	}
	if existing, ok := p.embeddedSources.Get(path); ok {
		return existing, nil
	}
	p.embeddedSources.AddWeighted(path, source, int64(len(data)))
	return source, nil
}

// fontGlyphSnapshot 与某个字形映射版本绑定。映射切片发布后只读，摘要按需
// 计算一次；回退字体变化或字体加载重试不会反复复制、排序全部映射。
type fontGlyphSnapshot struct {
	version  uint64
	mappings []fontrepair.GlyphMapping
	once     sync.Once
	digest   [sha256.Size]byte
}

func (s *fontGlyphSnapshot) hash() [sha256.Size]byte {
	if s == nil {
		return [sha256.Size]byte{}
	}
	s.once.Do(func() { s.digest = hashGlyphMappings(s.mappings) })
	return s.digest
}

// glyphSnapshot 由 LoadFont 在 mu 下调用；排序与摘要计算延迟到锁外。
func (p *Fonts) glyphSnapshot(id models.StRefID) *fontGlyphSnapshot {
	if len(p.glyphMappings[id]) == 0 {
		return nil
	}
	if p.glyphSnapshots == nil {
		p.glyphSnapshots = cache.NewWeightedLRU[models.StRefID, *fontGlyphSnapshot](glyphSnapshotCapacity, glyphSnapshotBytes, nil)
	}
	version := p.glyphVersions[id]
	if snapshot, ok := p.glyphSnapshots.Get(id); ok && snapshot.version == version {
		return snapshot
	}
	snapshot := &fontGlyphSnapshot{version: version, mappings: p.glyphMappingList(id)}
	p.glyphSnapshots.AddWeighted(id, snapshot, int64(len(snapshot.mappings))*16)
	return snapshot
}
