package webreader

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/render"
)

// FontResource 描述文档中可注入浏览器的嵌入字体。
// Data 为空表示该字体只声明了名称，没有嵌入字体文件。
type FontResource struct {
	ID     uint64
	Family string
	Name   string
	Bold   bool
	Italic bool
	Format string
	Data   []byte
}

// FontInfo 描述文档声明的一个字体，不包含嵌入字体数据。
type FontInfo struct {
	// ID 是字体在所属文档内的标识。
	ID uint64
	// Scope 是字体所属文档体的索引，与 ID 一起唯一标识一个字体。
	Scope int
	// Name 是 OFD 声明的字体名称（FontName）。
	Name string
	// Family 是字体族名称（FamilyName），可能为空。
	Family string
	// Bold、Italic 表示字体声明为粗体或斜体。
	Bold   bool
	Italic bool
	// Serif 表示衬线字体，FixedWidth 表示等宽字体。
	Serif      bool
	FixedWidth bool
	// Format 是嵌入字体文件的格式（如 ttf、otf），无嵌入时为空。
	Format string
	// Embedded 表示文档是否内嵌了字体文件。
	Embedded bool
}

// FontRef 唯一标识一个文档作用域内的字体。
type FontRef struct {
	// Scope 是字体所属文档体的索引。
	Scope int
	// ID 是字体在所属文档内的标识。
	ID uint64
}

// FontUsageOptions 控制按字体统计使用页面时的扫描上限，零值使用默认值。
type FontUsageOptions struct {
	// MaxScan 是最多扫描的页数，0 使用默认值 maxFontUsageScan，上限为 maxFontUsageScanHard。
	MaxScan int
	// MaxPages 是每个字体最多返回的页面数，0 使用默认值 maxFontUsagePages，上限为 maxFontUsagePagesHard。
	MaxPages int
}

// FontUsage 描述某个字体在文档文字中的使用情况。
// 为避免超大文档长时间扫描，统计页数和结果数量由 FontUsageOptions 限制。
type FontUsage struct {
	// Pages 是使用该字体的页面索引，升序排列。
	Pages []int
	// Scanned 是实际扫描的页数。
	Scanned int
	// Truncated 表示因达到扫描页数或结果数量上限而提前结束，结果可能不完整。
	Truncated bool
}

// FontUsageSummary 描述一个字体在文档中的使用页面。
type FontUsageSummary struct {
	// Scope 是字体所属文档体的索引。
	Scope int
	// ID 是字体在所属文档内的标识。
	ID uint64
	// Pages 是使用该字体的页面索引，升序排列，最多 maxFontUsagePages 个。
	Pages []int
}

// FontUsageReport 是一次批量字体使用统计的结果。
type FontUsageReport struct {
	// Fonts 按字体 ID 升序排列。
	Fonts []FontUsageSummary
	// Scanned 是实际扫描的页数。
	Scanned int
	// Truncated 表示因扫描页数或单个字体结果数量上限而可能不完整。
	Truncated bool
}

// FontSource 是由调用方提供给 WASM 渲染器的字体文件。
// 浏览器无法读取本机系统字体文件，因此无内嵌字体时应传入可访问的
// TTF/OTF Web Font 数据，例如 Google Fonts 的 Noto Sans SC。
type FontSource struct {
	Family string
	Name   string
	Weight int
	Italic bool
	Data   []byte
}

// RegisterFallbackFont 在进程内全局注册回退字体，与具体 Reader 无关。
// 同一字体族只注册一次（幂等）且不复制字体数据；之后可通过
// Reader.UseFallbackFont 应用到某个文档。
func RegisterFallbackFont(source FontSource) error {
	if len(source.Data) == 0 || source.Family == "" {
		return errors.New("回退字体数据或字体族名为空")
	}
	if len(source.Data) > maxFontBytes {
		return fmt.Errorf("回退字体超过大小限制 %d MB", maxFontBytes>>20)
	}
	slog.Info("register fallback font", "name", source.Name, "family", source.Family, "weight", source.Weight, "italic", source.Italic, "bytes", len(source.Data))
	return render.RegisterFallbackFont(source.Data, source.Family, fallbackFontStyle(source))
}

// UseFallbackFont 使当前文档缺失字体时使用已全局注册的回退字体族。
// 该字体族必须先通过 RegisterFallbackFont 注册。文字和搜索快照会失效，
// 因为它们的字形度量可能使用了不同的回退字体。
func (r *Reader) UseFallbackFont(family string) error {
	if r == nil {
		return errors.New("文档引擎为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("文档引擎已经关闭")
	}
	if family == "" {
		return errors.New("回退字体族名为空")
	}
	if _, ok := render.FallbackFontData(family); !ok {
		return fmt.Errorf("回退字体族 %q 未注册", family)
	}
	seenDocuments := make(map[*render.Document]struct{})
	for _, ref := range r.pages {
		if _, seen := seenDocuments[ref.document]; seen {
			continue
		}
		seenDocuments[ref.document] = struct{}{}
		if err := ref.document.UseFallbackFont(family); err != nil {
			return err
		}
	}
	r.fallbackFamily = family
	r.fallbackFamilies = append(r.fallbackFamilies, family)
	r.renderDocsMu.Lock()
	r.renderDocs = nil
	r.renderDocsMu.Unlock()
	r.text = newTextCache()
	r.search = newSearchCache(len(r.pages))
	return nil
}

// RemoveFallbackFont 取消当前文档先前通过 UseFallbackFont 登记的回退字体族，
// 使缺失字体恢复为内嵌或默认字体。全局字体注册表不回滚，但本 Reader 已解析的
// 文档与文字/搜索缓存会失效，以便下次渲染使用默认字体。
func (r *Reader) RemoveFallbackFont(family string) error {
	if r == nil {
		return errors.New("文档引擎为空")
	}
	if family == "" {
		return errors.New("回退字体族名为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("文档引擎已经关闭")
	}
	seenDocuments := make(map[*render.Document]struct{})
	for _, ref := range r.pages {
		if _, seen := seenDocuments[ref.document]; seen {
			continue
		}
		seenDocuments[ref.document] = struct{}{}
		ref.document.RemoveFallbackFont(family)
	}
	if len(r.fallbackFamilies) > 0 {
		kept := r.fallbackFamilies[:0]
		for _, value := range r.fallbackFamilies {
			if value != family {
				kept = append(kept, value)
			}
		}
		r.fallbackFamilies = kept
	}
	if r.fallbackFamily == family {
		r.fallbackFamily = ""
		if len(r.fallbackFamilies) > 0 {
			r.fallbackFamily = r.fallbackFamilies[len(r.fallbackFamilies)-1]
		}
	}
	r.renderDocsMu.Lock()
	r.renderDocs = nil
	r.renderDocsMu.Unlock()
	r.text = newTextCache()
	r.search = newSearchCache(len(r.pages))
	return nil
}

func fallbackFontStyle(source FontSource) render.FontStyle {
	style := render.FontRegular
	if source.Weight >= 650 {
		style = render.FontBold
	}
	if source.Italic {
		style |= render.FontItalic
	}
	return style
}

// Fonts 返回文档中声明的字体资源。只有包含 FontFile 的字体会带有 Data，
// 调用方可以将这些数据交给浏览器 FontFace 构造器进行注入。
func (r *Reader) Fonts() ([]FontResource, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	r.metadataMu.Lock()
	defer r.metadataMu.Unlock()
	r.metadata.fontsOnce.Do(func() {
		r.metadata.fontsVal, r.metadata.fontsErr = r.computeFonts()
	})
	return r.metadata.fontsVal, r.metadata.fontsErr
}

func (r *Reader) computeFonts() ([]FontResource, error) {
	resources := make([]FontResource, 0)
	seen := make(map[string]struct{})
	for documentIndex, document := range r.ofd.Documents {
		document.ForEachFont(func(id models.StID, font *models.Font) bool {
			if font == nil || font.FontFile == "" {
				return true
			}
			key := fmt.Sprintf("%d:%d:%s", documentIndex, id, font.FontFile)
			if _, ok := seen[key]; ok {
				return true
			}
			seen[key] = struct{}{}
			data, err := document.FileCache.ReadLimit(string(font.FontFile), maxFontBytes)
			if err != nil {
				return true
			}
			if fixed, fixErr := fontfix.Repair(data); fixErr == nil {
				data = fixed
			}
			resources = append(resources, FontResource{
				ID: uint64(id), Family: browserFontFamily(documentIndex, uint64(id)), Name: font.FontName,
				Bold: font.Bold, Italic: font.Italic, Format: fontFormat(font.FontFile), Data: append([]byte(nil), data...),
			})
			return true
		})
	}
	return resources, nil
}

// FontList 返回文档声明的全部字体（含没有嵌入文件的逻辑字体），不读取字体数据。
func (r *Reader) FontList() ([]FontInfo, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if r.ofd == nil {
		return nil, nil
	}
	r.metadataMu.Lock()
	defer r.metadataMu.Unlock()
	r.metadata.fontListOnce.Do(func() {
		r.metadata.fontListVal, r.metadata.fontListErr = r.computeFontList()
	})
	return r.metadata.fontListVal, r.metadata.fontListErr
}

func (r *Reader) computeFontList() ([]FontInfo, error) {
	fonts := make([]FontInfo, 0)
	seen := make(map[string]struct{})
	for documentIndex, document := range r.ofd.Documents {
		document.ForEachFont(func(id models.StID, font *models.Font) bool {
			if font == nil {
				return true
			}
			key := fmt.Sprintf("%d:%d", documentIndex, id)
			if _, ok := seen[key]; ok {
				return true
			}
			seen[key] = struct{}{}
			embedded := font.FontFile != ""
			format := ""
			if embedded {
				format = fontFormat(font.FontFile)
			}
			fonts = append(fonts, FontInfo{
				ID:         uint64(id),
				Scope:      documentIndex,
				Name:       font.FontName,
				Family:     font.FamilyName,
				Bold:       font.Bold,
				Italic:     font.Italic,
				Serif:      font.Serif,
				FixedWidth: font.FixedWidth,
				Format:     format,
				Embedded:   embedded,
			})
			return true
		})
	}
	return fonts, nil
}

// FontUsage 返回使用指定字体的页面。为避免超大文档长时间扫描，扫描页数和
// 结果数量由 options 限制；Truncated 表示结果可能不完整。
func (r *Reader) FontUsage(ref FontRef, options FontUsageOptions) (FontUsage, error) {
	if r == nil {
		return FontUsage{}, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if r.closed {
		return FontUsage{}, errors.New("文档引擎已经关闭")
	}
	options = options.normalized()
	usage := FontUsage{}
	limit := len(r.pages)
	if limit > options.MaxScan {
		limit = options.MaxScan
	}
	for index := 0; index < limit; index++ {
		usage.Scanned = index + 1
		for _, run := range r.textAt(index) {
			if run.Scope != ref.Scope || run.Font != ref.ID {
				continue
			}
			usage.Pages = append(usage.Pages, index)
			break
		}
		if len(usage.Pages) >= options.MaxPages {
			usage.Truncated = true
			break
		}
	}
	if !usage.Truncated && limit < len(r.pages) {
		usage.Truncated = true
	}
	return usage, nil
}

// FontUsageAll 一次扫描统计所有字体的使用页面，供字体列表批量展示。
// 扫描页数和单个字体的结果数量由 options 限制。
func (r *Reader) FontUsageAll(options FontUsageOptions) (FontUsageReport, error) {
	if r == nil {
		return FontUsageReport{}, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if r.closed {
		return FontUsageReport{}, errors.New("文档引擎已经关闭")
	}
	options = options.normalized()
	report := FontUsageReport{}
	byFont := make(map[FontRef][]int)
	limit := len(r.pages)
	if limit > options.MaxScan {
		limit = options.MaxScan
	}
	for index := 0; index < limit; index++ {
		report.Scanned = index + 1
		for _, run := range r.textAt(index) {
			ref := FontRef{Scope: run.Scope, ID: run.Font}
			pages := byFont[ref]
			if len(pages) >= options.MaxPages {
				continue
			}
			if len(pages) > 0 && pages[len(pages)-1] == index {
				continue
			}
			byFont[ref] = append(pages, index)
		}
	}
	if limit < len(r.pages) {
		report.Truncated = true
	}
	refs := make([]FontRef, 0, len(byFont))
	for ref := range byFont {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Scope != refs[j].Scope {
			return refs[i].Scope < refs[j].Scope
		}
		return refs[i].ID < refs[j].ID
	})
	for _, ref := range refs {
		pages := byFont[ref]
		if len(pages) >= options.MaxPages {
			report.Truncated = true
		}
		report.Fonts = append(report.Fonts, FontUsageSummary{Scope: ref.Scope, ID: ref.ID, Pages: pages})
	}
	return report, nil
}
