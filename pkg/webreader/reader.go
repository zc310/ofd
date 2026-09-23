// Package webreader 包提供适合浏览器调用的 OFD 文档访问接口。
package webreader

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zc310/fontfix"
	"github.com/zc310/ofd/internal/models"
	"github.com/zc310/ofd/internal/parser"
	"github.com/zc310/ofd/internal/render"
	_ "github.com/zc310/ofd/internal/render/backends/canvas"
	"github.com/zc310/ofd/internal/render/geom"
	"github.com/zc310/ofd/internal/utils"
)

const (
	defaultDPI        = 96
	defaultPageWidth  = 210
	defaultPageHeight = 297
	maxDPI            = 600
	maxInputBytes     = 256 << 20
	maxRenderPixels   = 50_000_000
	maxFontBytes      = 32 << 20
	maxRenderPages    = 64
	maxRenderDocs     = 4
	// maxFontUsageScan 和 maxFontUsagePages 是按字体统计使用页面时的默认上限，
	// 避免十万级页面文档在一次统计请求中解析全部页面布局。
	maxFontUsageScan  = 10_000
	maxFontUsagePages = 500
	// maxFontUsageScanHard 和 maxFontUsagePagesHard 是调用方可以请求的绝对上限，
	// 防止传入过大的参数导致一次统计请求解析全部页面。
	maxFontUsageScanHard  = 100_000
	maxFontUsagePagesHard = 5_000
	// maxAttachmentBytes 是单个附件默认允许读取的最大字节数，硬上限为 maxAttachmentBytesHard。
	maxAttachmentBytes     = 32 << 20
	maxAttachmentBytesHard = 128 << 20
	// textCacheCapacity 和 searchCacheCapacity 限制按页缓存的文字与搜索索引，
	// 避免浏览/搜索大文档时把所有页面的布局快照都留在内存中。
	textCacheCapacity   = 64
	searchCacheCapacity = 64
)

// PageInfo 描述一个可渲染页面。尺寸单位为毫米。
type PageInfo struct {
	Index  int
	Width  float64
	Height float64
}

// TextRun 描述页面中的一个文字对象。坐标和尺寸单位为毫米。
// X/Y 是网页覆盖层使用的左上角坐标，不是 TextCode 的基线坐标。
type TextRun struct {
	Text   string
	X      float64
	Y      float64
	Width  float64
	Height float64
	// Scope 是文字所属文档体的索引，与 Font 一起唯一标识使用的字体。
	Scope         int
	Font          uint64
	Size          float64
	Weight        int
	ReadDirection int
	CharDirection int
	FontFamily    string
	Bold          bool
	Italic        bool
	Glyphs        []Glyph
}

// Glyph 描述一个字符的页面区域，坐标单位为毫米。
type Glyph struct {
	Text   string
	X      float64
	Y      float64
	Width  float64
	Height float64
	Angle  float64
}

// SearchResult 描述一个页面文字命中。
type SearchResult struct {
	Page  int
	Run   int
	Text  string
	Start int
	End   int
	Rects []Rect
}

// Rect 描述一个搜索命中的字符区域，坐标单位为毫米。
type Rect struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
	Angle  float64
}

// RenderFormat 是页面输出格式。
type RenderFormat string

const (
	// RenderPNG 输出 PNG 位图。为空时也使用该格式。
	RenderPNG RenderFormat = "png"
	// RenderSVG 输出 SVG 矢量文档。
	RenderSVG RenderFormat = "svg"
	// RenderJPG 输出 JPG 位图。JPG 不支持透明度，透明区域使用白色填充。
	RenderJPG RenderFormat = "jpg"
)

// RenderOptions 控制页面输出。DPI 控制 PNG、JPG 以及 PDF 中复杂效果的内部栅格化分辨率；
// PDF 页面主体保留文字和矢量内容，SVG 复杂渐变等仍可能包含栅格回退。
type RenderOptions struct {
	DPI        float64
	Background color.Color
	Format     RenderFormat
}

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

func (options FontUsageOptions) normalized() FontUsageOptions {
	if options.MaxScan <= 0 {
		options.MaxScan = maxFontUsageScan
	} else if options.MaxScan > maxFontUsageScanHard {
		options.MaxScan = maxFontUsageScanHard
	}
	if options.MaxPages <= 0 {
		options.MaxPages = maxFontUsagePages
	} else if options.MaxPages > maxFontUsagePagesHard {
		options.MaxPages = maxFontUsagePagesHard
	}
	return options
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

// AttachmentInfo 描述文档中的一个附件，只包含清单元数据。
type AttachmentInfo struct {
	// Scope 是附件所属文档体的索引。
	Scope int
	// ID 是附件 ID。
	ID string
	// Name 是附件名称。
	Name string
	// Format 是附件格式，未声明时为空。
	Format string
	// Size 是附件声明的字节数，HasSize 为 false 时表示未声明。
	Size    int64
	HasSize bool
	// ActualSize 是附件在包中的实际字节数，0 表示未知或文件不存在。
	ActualSize int64
	// Usage 是附件用途，未声明时为空。
	Usage string
	// Visible 表示附件是否可见，未声明时为 true。
	Visible bool
	// Exists 表示附件文件是否存在于包中。
	Exists bool
}

// MediaInfo 描述文档中的一个多媒体资源（图片/音频/视频），只包含清单元数据。
type MediaInfo struct {
	// Scope 是资源所属文档体的索引。
	Scope int
	// ID 是多媒体资源标识。
	ID uint64
	// Name 是资源文件名，未解析时为空。
	Name string
	// Type 是多媒体类型，通常为 Image、Audio 或 Video。
	Type string
	// Format 是多媒体格式，未声明时为空。
	Format string
	// Size 是资源在包中的实际字节数，0 表示未知或文件不存在。
	Size int64
	// Exists 表示资源文件是否存在于包中。
	Exists bool
}

// AnnotationBoundary 是注解外观的边界框，单位为毫米。
type AnnotationBoundary struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

// AnnotationInfo 描述文档中的一个注解。
type AnnotationInfo struct {
	// Scope 是注解所属文档体的索引。
	Scope int
	// Page 是注解所在页面的全局索引。
	Page int
	// ID 是注解标识。
	ID string
	// Type 是注解类型，如 Link、Highlight、Stamp。
	Type string
	// Subtype 是注解子类型，未声明时为空。
	Subtype string
	// Creator 是创建注解的软件或用户，未声明时为空。
	Creator string
	// LastModDate 是最后修改日期（YYYY-MM-DD），未声明时为空。
	LastModDate string
	// Visible 表示注解是否可见，未声明时为 true。
	Visible bool
	// Remark 是注解备注，未声明时为空。
	Remark string
	// Boundary 是注解外观边界，未声明时为 nil。
	Boundary *AnnotationBoundary
}

// SignatureStamp 描述签名关联的一个签章位置。
type SignatureStamp struct {
	// Page 是签章所在页面的全局索引，-1 表示无法解析。
	Page int
	// ID 是签章标识。
	ID string
	// Boundary 是签章边界（毫米），未声明时为 nil。
	Boundary *AnnotationBoundary
	// HasSeal 表示是否提取到印章数据。
	HasSeal bool
	// SealType 是印章文件类型（如 png、jpg、ofd），无印章时为空。
	SealType string
}

// CertificateDetail 描述签名某一层（印章/外层）的证书与验证明细。
type CertificateDetail struct {
	// Slot 是所属层级：印章或外层。
	Slot string
	// SlotKey 是层级的机器可读标识：seal 或 outer，用于导出证书。
	SlotKey string
	// Subject、Issuer 是证书主体与签发者。
	Subject string
	Issuer  string
	// CommonName、Organization、OrganizationalUnit、Country、Locality、Province 是证书主体字段。
	CommonName         string
	Organization       string
	OrganizationalUnit string
	Country            string
	Locality           string
	Province           string
	// SerialNumber 是证书序列号。
	SerialNumber string
	// NotBefore、NotAfter 是证书有效期（RFC3339）。
	NotBefore string
	NotAfter  string
	// PublicKey 是公钥算法名称。
	PublicKey string
	// Algorithm、SignatureFormat 是签名算法与编码格式。
	Algorithm       string
	SignatureFormat string
	// SignatureValid 表示该层签名通过公钥验证。
	SignatureValid bool
	// CertificateValid 表示证书在签名时间点有效。
	CertificateValid bool
	// TrustChecked、Trusted、TrustError 是证书链校验结果。
	TrustChecked bool
	Trusted      bool
	TrustError   string
	// RevocationChecked、RevocationStatus、RevocationError 是吊销校验结果。
	RevocationChecked bool
	RevocationStatus  string
	RevocationError   string
	// Error 是验证失败原因。
	Error string
}

// SignatureReference 描述签名覆盖的一个文件引用及其摘要校验结果。
type SignatureReference struct {
	// FileRef 是签名中声明的文件引用路径。
	FileRef string
	// Exists 表示引用的文件是否存在于包中。
	Exists bool
	// Match 表示摘要是否一致。
	Match bool
	// Error 是校验失败原因。
	Error string
}

// SignatureInfo 描述文档中的一个签名及其校验结果。
type SignatureInfo struct {
	// Scope 是签名所属文档体的索引。
	Scope int
	// ID 是签名标识。
	ID string
	// Provider、Company、Version 是签名提供者信息。
	Provider string
	Company  string
	Version  string
	// Method 是签名算法标识（通常是 OID），未声明时为空。
	Method string
	// Date 是签名时间，保留原始文本。
	Date string
	// HasDigest、DigestValid 和 DigestMethod 是摘要校验结果。
	HasDigest    bool
	DigestValid  bool
	DigestMethod string
	// HasVerification、Verified、Trusted、TrustChecked 是验签结果。
	HasVerification bool
	Verified        bool
	Trusted         bool
	TrustChecked    bool
	// VerificationError 是验签错误信息，无错误时为空。
	VerificationError string
	// Stamps 是签名关联的签章位置。
	Stamps []SignatureStamp
	// Certificates 是印章与外层两层的证书与验证明细。
	Certificates []CertificateDetail
	// References 是签名覆盖的文件引用及逐项摘要校验结果。
	References []SignatureReference
	// HasDataHash、DataHashMatch 是签名数据摘要（Signature.xml）校验结果。
	HasDataHash   bool
	DataHashMatch bool
}

// DocumentStats 汇总文档声明的资源数量（不读取资源内容）。
type DocumentStats struct {
	// Fonts 是声明字体的数量。
	Fonts int
	// Attachments 是附件数量。
	Attachments int
	// Media 是多媒资资源数量。
	Media int
	// AnnotationPages 是声明了注解的页面数量。
	AnnotationPages int
	// Signatures 是签名数量。
	Signatures int
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

// DocumentInfo 描述 OFD 文档的元数据信息。
type DocumentInfo struct {
	DocID        string
	Title        string
	Author       string
	Subject      string
	Abstract     string
	CreationDate string
	ModDate      string
	Creator      string
	Version      string
}

// OutlineDest 描述跳转目标的位置与缩放，单位与 OFD 页面坐标一致（毫米）。
// 字段为 nil 表示文档未指定该值。
type OutlineDest struct {
	// Type 是目标类型，如 XYZ、Fit、FitH、FitV、FitR。
	Type string
	// Left、Top、Right、Bottom 是目标视图的边界。
	Left   *float64
	Top    *float64
	Right  *float64
	Bottom *float64
	// Zoom 是目标缩放比例。
	Zoom *float64
}

// OutlineNode 描述文档大纲中的一个节点。
type OutlineNode struct {
	// Title 是大纲项标题。
	Title string
	// Page 是从 0 开始的目标页索引，-1 表示没有可跳转目标。
	Page int
	// URI 是外部链接目标，非空时点击打开链接。
	URI string
	// Dest 是页面跳转目标的位置与缩放，nil 表示没有位置信息。
	Dest *OutlineDest
	// Expanded 是文档声明的是否默认展开子项，nil 表示未声明。
	Expanded *bool
	// Children 是子大纲项。
	Children []OutlineNode
}

// Bookmark 描述文档书签（命名目标）。
type Bookmark struct {
	// Name 是书签名称。
	Name string
	// Page 是从 0 开始的目标页索引，-1 表示无法解析。
	Page int
	// Dest 是书签目标的位置与缩放。
	Dest *OutlineDest
}

// OutlineTree 描述文档大纲、书签以及文档声明的打开显示模式。
type OutlineTree struct {
	// PageMode 是文档声明的页面显示模式，例如 UseOutlines；为空表示未声明。
	PageMode string
	// Nodes 是顶层大纲项。
	Nodes []OutlineNode
	// Bookmarks 是文档书签列表。
	Bookmarks []Bookmark
}

// ViewPreferences 描述文档声明的阅读器显示偏好。
type ViewPreferences struct {
	// PageLayout 是文档声明的页面布局方式，如 OneColumn、TwoPageL、TwoPageR。
	PageLayout string
	// ZoomMode 是文档声明的缩放模式，如 FitWidth、FitHeight、FitRect。
	ZoomMode string
	// Zoom 是文档声明的自定义缩放比例。
	Zoom *float64
}

// OpenOptions 配置 OFD 打开行为。
type OpenOptions struct {
	// PageCacheCapacity 是页面缓存最多保留的页面数量，0 表示使用默认值。
	PageCacheCapacity int
	// PageCacheBytes 是页面缓存允许使用的估算最大字节数，0 表示使用默认值。
	PageCacheBytes int64
}

// Reader 是一个已打开的 OFD 文档。
// Reader 负责持有文档资源，使用完毕后必须调用 Close。
type Reader struct {
	mu               sync.RWMutex
	cacheMu          sync.Mutex
	renderDocsMu     sync.Mutex
	closed           bool
	ofd              *parser.OFD
	pages            []pageRef
	renderDocs       *utils.LRU[renderDocumentKey, *render.Document]
	fallbackFamily   string
	fallbackFamilies []string
	text             *utils.LRU[int, []TextRun]
	search           *utils.LRU[int, searchPage]
	options          RenderOptions
}

type searchPage struct {
	runs   [][]rune
	byRune map[rune][]int
}

type pageRef struct {
	document  *render.Document
	page      *parser.Page
	fontScope int
}

type renderDocumentKey struct {
	base  *render.Document
	dpi   float64
	red   uint32
	green uint32
	blue  uint32
	alpha uint32
}

// Open 从内存中的 OFD 数据创建浏览器文档引擎。
func Open(data []byte) (*Reader, error) {
	return OpenWithOptions(data, OpenOptions{})
}

// OpenWithOptions 从内存中的 OFD 数据创建浏览器文档引擎。
func OpenWithOptions(data []byte, options OpenOptions) (*Reader, error) {
	if len(data) == 0 {
		return nil, errors.New("OFD 数据为空")
	}
	if len(data) > maxInputBytes {
		return nil, fmt.Errorf("OFD 数据超过大小限制 %d MB", maxInputBytes>>20)
	}
	ofd, err := parser.NewOFDWithOptions(data, parser.Options{
		PageCacheCapacity: options.PageCacheCapacity,
		PageCacheBytes:    options.PageCacheBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("解析 OFD 失败: %w", err)
	}

	r := &Reader{
		ofd:     ofd,
		options: RenderOptions{DPI: defaultDPI, Background: color.Transparent},
	}
	for documentIndex, document := range ofd.Documents {
		renderDocument := render.NewDocument(r.options.Background, document)
		for _, page := range renderDocument.Pages {
			r.pages = append(r.pages, pageRef{document: renderDocument, page: page, fontScope: documentIndex})
		}
	}
	if len(r.pages) == 0 {
		_ = ofd.Close()
		return nil, errors.New("OFD 文档没有页面")
	}
	r.text = utils.NewLRU[int, []TextRun](textCacheCapacity, nil)
	r.search = utils.NewLRU[int, searchPage](searchCacheCapacity, nil)
	return r, nil
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
	r.text = utils.NewLRU[int, []TextRun](textCacheCapacity, nil)
	r.search = utils.NewLRU[int, searchPage](searchCacheCapacity, nil)
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

// PageCount 返回文档的总页数，页码从 0 开始。
func (r *Reader) PageCount() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return 0
	}
	return len(r.pages)
}

// Info 返回 OFD 文档的元数据信息。
func (r *Reader) Info() (DocumentInfo, error) {
	if r == nil {
		return DocumentInfo{}, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return DocumentInfo{}, errors.New("文档引擎已经关闭")
	}
	if r.ofd == nil || len(r.ofd.DocBodies) == 0 {
		return DocumentInfo{}, nil
	}
	body := r.ofd.DocBodies[0]
	info := body.DocInfo
	result := DocumentInfo{
		DocID:   info.DocID,
		Version: r.ofd.Version,
	}
	if info.Title != nil {
		result.Title = *info.Title
	}
	if info.Author != nil {
		result.Author = *info.Author
	}
	if info.Subject != nil {
		result.Subject = *info.Subject
	}
	if info.Abstract != nil {
		result.Abstract = *info.Abstract
	}
	if info.Creator != nil {
		result.Creator = *info.Creator
	}
	if info.CreationDate != nil {
		result.CreationDate = info.CreationDate.String()
	}
	if info.ModDate != nil {
		result.ModDate = info.ModDate.String()
	}
	return result, nil
}

// Outline 返回文档大纲树、书签列表以及文档声明的显示模式。
// 跳转目标按全局页索引解析（跨文档体累加），无法解析的目标为 -1。
// 没有大纲或书签时返回空切片。
func (r *Reader) Outline() (OutlineTree, error) {
	if r == nil {
		return OutlineTree{}, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return OutlineTree{}, errors.New("文档引擎已经关闭")
	}
	if r.ofd == nil {
		return OutlineTree{}, nil
	}
	var result OutlineTree
	base := 0
	for _, document := range r.ofd.Documents {
		if document == nil {
			continue
		}
		pageIndexes := make(map[models.StID]int, len(document.Pages))
		for index, page := range document.Pages {
			if page != nil {
				pageIndexes[page.ID] = base + index
			}
		}
		resolvePage := func(dest models.CtDest) int {
			if page, ok := pageIndexes[models.StID(dest.PageID)]; ok {
				return page
			}
			return -1
		}
		bookmarkDests := make(map[string]models.CtDest)
		if document.Bookmarks != nil {
			for _, bookmark := range document.Bookmarks.Bookmarks {
				bookmarkDests[bookmark.Name] = bookmark.Dest
				result.Bookmarks = append(result.Bookmarks, Bookmark{
					Name: bookmark.Name,
					Page: resolvePage(bookmark.Dest),
					Dest: convertOutlineDest(bookmark.Dest),
				})
			}
		}
		if document.Outlines != nil {
			result.Nodes = append(result.Nodes, outlineNodes(document.Outlines.OutlineElems, resolvePage, bookmarkDests)...)
		}
		if result.PageMode == "" && document.VPreferences != nil && document.VPreferences.PageMode != nil {
			result.PageMode = string(*document.VPreferences.PageMode)
		}
		base += len(document.Pages)
	}
	return result, nil
}

// Preferences 返回文档声明的阅读器显示偏好（取第一个声明了偏好的文档体）。
// 没有声明时返回零值。
func (r *Reader) Preferences() (ViewPreferences, error) {
	if r == nil {
		return ViewPreferences{}, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return ViewPreferences{}, errors.New("文档引擎已经关闭")
	}
	if r.ofd == nil {
		return ViewPreferences{}, nil
	}
	for _, document := range r.ofd.Documents {
		if document == nil || document.VPreferences == nil {
			continue
		}
		preferences := document.VPreferences
		var result ViewPreferences
		if preferences.PageLayout != nil {
			result.PageLayout = string(*preferences.PageLayout)
		}
		if preferences.Zoom != nil {
			if preferences.Zoom.Mode != nil {
				result.ZoomMode = *preferences.Zoom.Mode
			}
			result.Zoom = preferences.Zoom.Value
		}
		return result, nil
	}
	return ViewPreferences{}, nil
}

// convertOutlineDest 把 OFD 目标转换为对调用方友好的位置与缩放描述。
func convertOutlineDest(dest models.CtDest) *OutlineDest {
	return &OutlineDest{
		Type:   string(dest.Type),
		Left:   dest.Left,
		Top:    dest.Top,
		Right:  dest.Right,
		Bottom: dest.Bottom,
		Zoom:   dest.Zoom,
	}
}

// outlineNodes 递归转换大纲项；目标优先取 Goto.Dest，其次按书签名称解析，
// 没有页面目标时回退到 URI 链接。
func outlineNodes(elems []models.CTOutlineElem, resolvePage func(models.CtDest) int, bookmarks map[string]models.CtDest) []OutlineNode {
	if len(elems) == 0 {
		return nil
	}
	nodes := make([]OutlineNode, 0, len(elems))
	for _, elem := range elems {
		node := OutlineNode{Title: elem.Title, Page: -1}
		if elem.Actions != nil {
			for _, action := range elem.Actions.Actions {
				if action.URI != nil && action.URI.URI != "" && node.URI == "" {
					node.URI = action.URI.URI
				}
				if action.Goto == nil {
					continue
				}
				if action.Goto.Dest != nil {
					node.Page = resolvePage(*action.Goto.Dest)
					node.Dest = convertOutlineDest(*action.Goto.Dest)
					node.URI = ""
					break
				}
				if action.Goto.Bookmark != nil {
					if dest, ok := bookmarks[action.Goto.Bookmark.Name]; ok {
						node.Page = resolvePage(dest)
						node.Dest = convertOutlineDest(dest)
						node.URI = ""
						break
					}
				}
			}
		}
		node.Expanded = elem.Expanded
		node.Children = outlineNodes(elem.OutlineElem, resolvePage, bookmarks)
		nodes = append(nodes, node)
	}
	return nodes
}

// Pages 返回所有页面的尺寸快照，尺寸单位为毫米。
// 优先读取页面 XML 中的 Area/PhysicalBox，不加载页面内容或资源。
func (r *Reader) Pages() ([]PageInfo, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}

	pages := make([]PageInfo, len(r.pages))
	for index, ref := range r.pages {
		if ref.document == nil || ref.document.Document == nil || ref.page == nil {
			return nil, fmt.Errorf("第 %d 页所属文档为空", index)
		}
		box := ref.document.CommonData.PageArea.PhysicalBox
		pageBox, err := ref.page.PhysicalBoxMetadata()
		if err == nil && finitePositive(pageBox.Width) && finitePositive(pageBox.Height) {
			box = pageBox
		}
		if !finitePositive(box.Width) || !finitePositive(box.Height) {
			box.Width = defaultPageWidth
			box.Height = defaultPageHeight
		}
		pages[index] = PageInfo{Index: index, Width: box.Width, Height: box.Height}
	}
	return pages, nil
}

// Page 返回指定页面的真实尺寸信息。
// 与 Pages 不同，查询单页信息会按需加载该页完整内容。
func (r *Reader) Page(index int) (PageInfo, error) {
	if r == nil {
		return PageInfo{}, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return PageInfo{}, errors.New("文档引擎已经关闭")
	}
	if index < 0 || index >= len(r.pages) {
		return PageInfo{}, fmt.Errorf("页面索引超出范围: %d", index)
	}
	return r.pageInfoLocked(index)
}

func (r *Reader) pageInfoLocked(index int) (PageInfo, error) {
	ref := r.pages[index]
	if ref.page == nil {
		return PageInfo{}, fmt.Errorf("第 %d 页为空", index)
	}
	box, err := ref.page.PhysicalBox()
	if err != nil {
		return PageInfo{}, fmt.Errorf("读取第 %d 页失败: %w", index, err)
	}
	if !finitePositive(box.Width) || !finitePositive(box.Height) {
		return PageInfo{}, fmt.Errorf("第 %d 页尺寸无效", index)
	}
	return PageInfo{Index: index, Width: box.Width, Height: box.Height}, nil
}

// Text 返回指定页面的文字对象快照。文字顺序与 OFD 页面绘制顺序一致。
func (r *Reader) Text(index int) ([]TextRun, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if index < 0 || index >= len(r.pages) {
		return nil, fmt.Errorf("页面索引超出范围: %d", index)
	}
	return cloneTextRuns(r.textAt(index)), nil
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

// Attachments 返回所有文档体的附件清单（只读元数据，不读取附件内容）。
func (r *Reader) Attachments() ([]AttachmentInfo, error) {
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
	infos := make([]AttachmentInfo, 0)
	for scope, document := range r.ofd.Documents {
		if document == nil || document.Document.Attachments == nil {
			continue
		}
		list := document.GetAttachments()
		if list == nil {
			continue
		}
		attachmentsPath := document.Document.Attachments.Resolve(document.BaseLoc).String()
		for _, attachment := range list.Attachments {
			info := AttachmentInfo{
				Scope:   scope,
				ID:      attachment.ID,
				Name:    attachment.Name,
				Usage:   attachment.Usage,
				Visible: attachment.Visible.Value(true),
			}
			if attachment.Format != nil {
				info.Format = *attachment.Format
			}
			if attachment.Size != nil {
				info.Size = int64(*attachment.Size)
				info.HasSize = true
			}
			path := resolveAttachmentAsset(document, attachmentsPath, attachment.FileLoc)
			if path != "" && document.FileCache != nil {
				if entry, ok := document.FileCache.Lookup(path); ok && !entry.IsDir {
					info.Exists = true
					info.ActualSize = int64(entry.UncompressedSize)
				}
			}
			infos = append(infos, info)
		}
	}
	return infos, nil
}

// AttachmentData 读取指定附件的二进制内容。maxBytes 为 0 时使用默认上限，
// 超过 maxAttachmentBytesHard 时按硬上限处理。
func (r *Reader) AttachmentData(scope int, id string, maxBytes int64) ([]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	if maxBytes <= 0 {
		maxBytes = maxAttachmentBytes
	} else if maxBytes > maxAttachmentBytesHard {
		maxBytes = maxAttachmentBytesHard
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	document, path, ok := r.attachmentPath(scope, id)
	if !ok || path == "" {
		return nil, fmt.Errorf("附件不存在: %d/%s", scope, id)
	}
	entry, exists := document.FileCache.Lookup(path)
	if !exists || entry.IsDir {
		return nil, fmt.Errorf("附件文件不存在: %s", path)
	}
	if entry.UncompressedSize > uint64(maxBytes) {
		return nil, fmt.Errorf("附件超过大小限制 %d MB", maxBytes>>20)
	}
	data, err := document.FileCache.ReadLimit(path, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("读取附件失败: %w", err)
	}
	return data, nil
}

func (r *Reader) attachmentPath(scope int, id string) (*parser.Document, string, bool) {
	if scope < 0 || scope >= len(r.ofd.Documents) {
		return nil, "", false
	}
	document := r.ofd.Documents[scope]
	if document == nil || document.Document.Attachments == nil {
		return nil, "", false
	}
	list := document.GetAttachments()
	if list == nil {
		return nil, "", false
	}
	attachmentsPath := document.Document.Attachments.Resolve(document.BaseLoc).String()
	for _, attachment := range list.Attachments {
		if attachment.ID != id {
			continue
		}
		return document, resolveAttachmentAsset(document, attachmentsPath, attachment.FileLoc), true
	}
	return nil, "", false
}

// resolveAttachmentAsset 解析附件文件在包内的路径：优先相对附件清单所在目录，
// 其次相对文档 BaseLoc，与 analyzer 的解析保持一致。
func resolveAttachmentAsset(document *parser.Document, attachmentsPath string, value models.StLoc) string {
	if value.IsEmpty() {
		return ""
	}
	if value.IsAbsolute() {
		return models.StLoc(value.String()).Clean().String()
	}
	candidates := []models.StLoc{
		models.StLoc(attachmentsPath).Dir().Join(value.String()).Clean(),
	}
	if document != nil {
		candidates = append(candidates, document.BaseLoc.Join(value.String()).Clean())
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if document != nil && document.FileCache != nil && document.FileCache.Has(candidate.String()) {
			return candidate.String()
		}
	}
	if candidates[0] == "" {
		return ""
	}
	return candidates[0].String()
}

// Media 返回所有文档体登记的多媒体资源（只读元数据，不读取资源内容）。
func (r *Reader) Media() ([]MediaInfo, error) {
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
	infos := make([]MediaInfo, 0)
	for scope, document := range r.ofd.Documents {
		if document == nil {
			continue
		}
		document.ForEachMedia(func(id models.StID, media *models.MultiMedia) bool {
			if media == nil {
				return true
			}
			info := MediaInfo{Scope: scope, ID: uint64(id), Type: media.Type, Format: media.Format}
			path := media.MediaFile.String()
			if path != "" {
				info.Name = models.StLoc(path).Base()
			}
			if path != "" && document.FileCache != nil {
				if entry, ok := document.FileCache.Lookup(path); ok && !entry.IsDir {
					info.Exists = true
					info.Size = int64(entry.UncompressedSize)
				}
			}
			infos = append(infos, info)
			return true
		})
	}
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].Scope != infos[j].Scope {
			return infos[i].Scope < infos[j].Scope
		}
		if infos[i].Type != infos[j].Type {
			return infos[i].Type < infos[j].Type
		}
		return infos[i].ID < infos[j].ID
	})
	return infos, nil
}

// MediaData 读取指定多媒体资源的二进制内容。maxBytes 为 0 时使用默认上限，
// 超过 maxAttachmentBytesHard 时按硬上限处理。
func (r *Reader) MediaData(scope int, mediaID uint64, maxBytes int64) ([]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	if maxBytes <= 0 {
		maxBytes = maxAttachmentBytes
	} else if maxBytes > maxAttachmentBytesHard {
		maxBytes = maxAttachmentBytesHard
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if scope < 0 || scope >= len(r.ofd.Documents) {
		return nil, fmt.Errorf("多媒体作用域超出范围: %d", scope)
	}
	document := r.ofd.Documents[scope]
	if document == nil {
		return nil, errors.New("多媒体所属文档为空")
	}
	media := document.GetMedia(models.StID(mediaID))
	if media == nil {
		return nil, fmt.Errorf("多媒体资源不存在: %d/%d", scope, mediaID)
	}
	path := media.MediaFile.String()
	if path == "" {
		return nil, fmt.Errorf("多媒体资源缺少文件路径: %d/%d", scope, mediaID)
	}
	entry, ok := document.FileCache.Lookup(path)
	if !ok || entry.IsDir {
		return nil, fmt.Errorf("多媒体文件不存在: %s", path)
	}
	if entry.UncompressedSize > uint64(maxBytes) {
		return nil, fmt.Errorf("多媒体文件超过大小限制 %d MB", maxBytes>>20)
	}
	data, err := document.FileCache.ReadLimit(path, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("读取多媒体失败: %w", err)
	}
	return data, nil
}

// Annotations 返回所有页面中的注解，按页面顺序排列。
func (r *Reader) Annotations() ([]AnnotationInfo, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	infos := make([]AnnotationInfo, 0)
	for index, ref := range r.pages {
		if ref.page == nil || ref.document == nil {
			continue
		}
		annot := ref.document.GetAnnotation(ref.page.ID)
		if annot == nil {
			continue
		}
		for _, item := range annot.Annots {
			if item == nil {
				continue
			}
			info := AnnotationInfo{
				Scope:   ref.fontScope,
				Page:    index,
				ID:      item.ID,
				Type:    string(item.Type),
				Subtype: item.Subtype,
				Creator: item.Creator,
				Visible: item.Visible.Value(true),
			}
			if !item.LastModDate.IsZero() {
				info.LastModDate = item.LastModDate.Time.Format("2006-01-02")
			}
			if item.Remark != nil {
				info.Remark = *item.Remark
			}
			if item.Appearance != nil && item.Appearance.Boundary != nil {
				boundary := item.Appearance.Boundary
				info.Boundary = &AnnotationBoundary{
					X:      boundary.X,
					Y:      boundary.Y,
					Width:  boundary.Width,
					Height: boundary.Height,
				}
			}
			infos = append(infos, info)
		}
	}
	return infos, nil
}

// Signatures 返回所有文档体的签名及其摘要/验签结果。
func (r *Reader) Signatures() ([]SignatureInfo, error) {
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
	pageIndex := make(map[models.StID]int, len(r.pages))
	for index, ref := range r.pages {
		if ref.page != nil {
			pageIndex[ref.page.ID] = index
		}
	}
	infos := make([]SignatureInfo, 0)
	for scope, document := range r.ofd.Documents {
		if document == nil {
			continue
		}
		document.ForEachSignature(func(id string, signature *models.Signature) bool {
			if signature == nil {
				return true
			}
			info := SignatureInfo{
				Scope:    scope,
				ID:       id,
				Provider: signature.SignedInfo.Provider.ProviderName,
				Company:  signature.SignedInfo.Provider.Company,
				Version:  signature.SignedInfo.Provider.Version,
				Method:   signature.SignedInfo.SignatureMethod,
				Date:     signature.SignedInfo.SignatureDateTime,
			}
			if digest := document.GetDigestResult(id); digest != nil {
				info.HasDigest = true
				info.DigestValid = digest.Valid
				info.DigestMethod = digest.Method
				for _, reference := range digest.References {
					info.References = append(info.References, SignatureReference{
						FileRef: reference.FileRef,
						Exists:  reference.Exists,
						Match:   reference.Match,
						Error:   reference.Error,
					})
				}
				if digest.DataHash != nil {
					info.HasDataHash = true
					info.DataHashMatch = digest.DataHash.Match
				}
			}
			if verification := document.GetVerificationResult(id); verification != nil {
				info.HasVerification = true
				info.Verified = verification.Valid
				info.Trusted = verification.Trusted
				info.TrustChecked = verification.TrustChecked
				info.Certificates = signatureCertificates(verification)
			}
			if verificationErr := document.GetVerificationError(id); verificationErr != nil {
				info.VerificationError = verificationErr.Error()
			}
			for _, annot := range signature.SignedInfo.StampAnnot {
				if annot == nil {
					continue
				}
				stamp := SignatureStamp{ID: annot.ID, Page: -1, Boundary: boxValue(annot.Boundary)}
				pageID := models.StID(annot.PageRef)
				if index, ok := pageIndex[pageID]; ok {
					stamp.Page = index
				}
				for _, seal := range document.GetSeals(pageID) {
					if seal == nil || seal.StampAnnot == nil || seal.SealData == nil {
						continue
					}
					if seal.StampAnnot.ID == annot.ID {
						stamp.HasSeal = true
						stamp.SealType = seal.SealData.FileType
						break
					}
				}
				info.Stamps = append(info.Stamps, stamp)
			}
			infos = append(infos, info)
			return true
		})
	}
	return infos, nil
}

// SignatureSeal 返回指定签名第 stampIndex 个签章的印章文件内容与类型。
func (r *Reader) SignatureSeal(scope int, signatureID string, stampIndex int) ([]byte, string, error) {
	if r == nil {
		return nil, "", errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, "", errors.New("文档引擎已经关闭")
	}
	if scope < 0 || scope >= len(r.ofd.Documents) {
		return nil, "", fmt.Errorf("签名作用域超出范围: %d", scope)
	}
	document := r.ofd.Documents[scope]
	if document == nil {
		return nil, "", errors.New("签名所属文档为空")
	}
	signature := document.GetSignature(signatureID)
	if signature == nil {
		return nil, "", fmt.Errorf("签名不存在: %s", signatureID)
	}
	if stampIndex < 0 || stampIndex >= len(signature.SignedInfo.StampAnnot) {
		return nil, "", fmt.Errorf("签章索引超出范围: %d", stampIndex)
	}
	annot := signature.SignedInfo.StampAnnot[stampIndex]
	if annot == nil {
		return nil, "", errors.New("签章为空")
	}
	for _, seal := range document.GetSeals(models.StID(annot.PageRef)) {
		if seal == nil || seal.StampAnnot == nil || seal.SealData == nil {
			continue
		}
		if seal.StampAnnot.ID == annot.ID {
			if int64(len(seal.SealData.Data)) > maxAttachmentBytesHard {
				return nil, "", fmt.Errorf("印章数据超过大小限制 %d MB", maxAttachmentBytesHard>>20)
			}
			return append([]byte(nil), seal.SealData.Data...), seal.SealData.FileType, nil
		}
	}
	return nil, "", errors.New("签章数据不存在")
}

// Stats 汇总文档声明的资源数量，只读取声明，不加载资源内容。
func (r *Reader) Stats() (DocumentStats, error) {
	if r == nil {
		return DocumentStats{}, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return DocumentStats{}, errors.New("文档引擎已经关闭")
	}
	stats := DocumentStats{}
	if r.ofd == nil {
		return stats, nil
	}
	fonts := make(map[string]struct{})
	for documentIndex, document := range r.ofd.Documents {
		if document == nil {
			continue
		}
		document.ForEachFont(func(id models.StID, font *models.Font) bool {
			fonts[fmt.Sprintf("%d:%d", documentIndex, id)] = struct{}{}
			return true
		})
		document.ForEachMedia(func(id models.StID, media *models.MultiMedia) bool {
			stats.Media++
			return true
		})
		document.ForEachSignature(func(id string, signature *models.Signature) bool {
			stats.Signatures++
			return true
		})
		if list := document.GetAttachments(); list != nil {
			stats.Attachments += len(list.Attachments)
		}
		stats.AnnotationPages += document.AnnotationPageCount()
	}
	stats.Fonts = len(fonts)
	return stats, nil
}

// SignatureCertificate 返回指定签名某一层（seal/outer）证书的 DER 内容。
func (r *Reader) SignatureCertificate(scope int, signatureID string, slot string) ([]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if scope < 0 || scope >= len(r.ofd.Documents) {
		return nil, fmt.Errorf("签名作用域超出范围: %d", scope)
	}
	document := r.ofd.Documents[scope]
	if document == nil {
		return nil, errors.New("签名所属文档为空")
	}
	verification := document.GetVerificationResult(signatureID)
	if verification == nil {
		return nil, fmt.Errorf("签名没有验证结果: %s", signatureID)
	}
	var component parser.SignatureComponentResult
	switch strings.ToLower(strings.TrimSpace(slot)) {
	case "seal", "印章":
		component = verification.Seal
	case "outer", "外层":
		component = verification.Outer
	default:
		return nil, fmt.Errorf("未知证书层级: %s", slot)
	}
	if component.Certificate == nil || len(component.Certificate.RawDER) == 0 {
		return nil, errors.New("证书数据不存在")
	}
	return append([]byte(nil), component.Certificate.RawDER...), nil
}

// SignatureValue 返回指定签名的签名值（SignedValue.dat）内容。
func (r *Reader) SignatureValue(scope int, signatureID string) ([]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if scope < 0 || scope >= len(r.ofd.Documents) {
		return nil, fmt.Errorf("签名作用域超出范围: %d", scope)
	}
	document := r.ofd.Documents[scope]
	if document == nil {
		return nil, errors.New("签名所属文档为空")
	}
	if document.GetSignature(signatureID) == nil {
		return nil, fmt.Errorf("签名不存在: %s", signatureID)
	}
	value := document.GetSignedValue(signatureID)
	if value == nil || len(value.Raw) == 0 {
		if err := document.GetSignedValueError(signatureID); err != nil {
			return nil, fmt.Errorf("读取签名值失败: %w", err)
		}
		return nil, errors.New("签名值不存在")
	}
	if int64(len(value.Raw)) > maxAttachmentBytesHard {
		return nil, fmt.Errorf("签名值超过大小限制 %d MB", maxAttachmentBytesHard>>20)
	}
	return append([]byte(nil), value.Raw...), nil
}

func signatureCertificates(verification *parser.SignatureVerificationResult) []CertificateDetail {
	if verification == nil {
		return nil
	}
	details := make([]CertificateDetail, 0, 2)
	appendComponent := func(slotKey, slot string, component parser.SignatureComponentResult) {
		if component.Certificate == nil && component.Error == "" && component.TrustError == "" &&
			component.RevocationError == "" && !component.TrustChecked && !component.RevocationChecked {
			return
		}
		detail := CertificateDetail{
			Slot:              slot,
			SlotKey:           slotKey,
			Algorithm:         component.Algorithm,
			SignatureFormat:   component.SignatureFormat,
			SignatureValid:    component.Valid,
			CertificateValid:  component.CertificateValid,
			TrustChecked:      component.TrustChecked,
			Trusted:           component.Trusted,
			TrustError:        component.TrustError,
			RevocationChecked: component.RevocationChecked,
			RevocationStatus:  component.RevocationStatus,
			RevocationError:   component.RevocationError,
			Error:             component.Error,
		}
		if certificate := component.Certificate; certificate != nil {
			detail.SerialNumber = certificate.SerialNumber
			detail.Subject = certificate.Subject.String()
			detail.Issuer = certificate.Issuer.String()
			detail.CommonName = certificate.Subject.CommonName
			detail.Organization = joinName(certificate.Subject.Organization)
			detail.OrganizationalUnit = joinName(certificate.Subject.OrganizationalUnit)
			detail.Country = joinName(certificate.Subject.Country)
			detail.Locality = joinName(certificate.Subject.Locality)
			detail.Province = joinName(certificate.Subject.Province)
			detail.PublicKey = certificate.PublicKey
			if !certificate.NotBefore.IsZero() {
				detail.NotBefore = certificate.NotBefore.Format(time.RFC3339)
			}
			if !certificate.NotAfter.IsZero() {
				detail.NotAfter = certificate.NotAfter.Format(time.RFC3339)
			}
		}
		details = append(details, detail)
	}
	appendComponent("seal", "印章", verification.Seal)
	appendComponent("outer", "外层", verification.Outer)
	return details
}

func joinName(values []string) string {
	kept := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			kept = append(kept, value)
		}
	}
	return strings.Join(kept, ", ")
}

func boxValue(box models.StBox) *AnnotationBoundary {
	if !box.IsFinite() {
		return nil
	}
	return &AnnotationBoundary{X: box.X, Y: box.Y, Width: box.Width, Height: box.Height}
}

// Search 在所有页面的文字对象中查找 query，匹配不区分大小写。
func (r *Reader) Search(query string) ([]SearchResult, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	query = strings.TrimSpace(query)
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	if query == "" {
		return []SearchResult{}, nil
	}
	needle := strings.ToLower(query)
	needleRunes := []rune(needle)
	results := make([]SearchResult, 0)
	for pageIndex := range r.pages {
		runs := r.textAt(pageIndex)
		indexed := r.searchAt(pageIndex, runs)
		for _, runIndex := range indexed.byRune[needleRunes[0]] {
			text := indexed.runs[runIndex]
			run := runs[runIndex]
			start := 0
			for {
				found := indexRunes(text[start:], needleRunes)
				if found < 0 {
					break
				}
				found += start
				end := found + len(needleRunes)
				result := SearchResult{Page: pageIndex, Run: runIndex, Text: run.Text, Start: found, End: end}
				glyphStart := minInt(found, len(run.Glyphs))
				glyphEnd := minInt(end, len(run.Glyphs))
				for _, glyph := range run.Glyphs[glyphStart:glyphEnd] {
					result.Rects = append(result.Rects, Rect{X: glyph.X, Y: glyph.Y, Width: glyph.Width, Height: glyph.Height, Angle: glyph.Angle})
				}
				results = append(results, result)
				start = end
				if start >= len(text) {
					break
				}
			}
		}
	}
	return results, nil
}

func indexRunes(text, needle []rune) int {
	if len(needle) == 0 {
		return 0
	}
	for index := 0; index+len(needle) <= len(text); index++ {
		matched := true
		for offset := range needle {
			if text[index+offset] != needle[offset] {
				matched = false
				break
			}
		}
		if matched {
			return index
		}
	}
	return -1
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

// RenderPage 将指定页面渲染为 PNG、JPG 或 SVG 数据。
func (r *Reader) RenderPage(index int, options RenderOptions) ([]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	return r.renderPage(index, options)
}

// RenderPages 将多个页面按传入顺序渲染为 PNG、JPG 或 SVG 数据。
// 所有页面共享一次 Reader 锁和同一份文档状态；渲染本身仍按顺序执行。
func (r *Reader) RenderPages(indices []int, options RenderOptions) ([][]byte, error) {
	if r == nil {
		return nil, errors.New("文档引擎为空")
	}
	if len(indices) > maxRenderPages {
		return nil, fmt.Errorf("批量渲染页面数量超过限制 %d", maxRenderPages)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return nil, errors.New("文档引擎已经关闭")
	}
	results := make([][]byte, 0, len(indices))
	for _, index := range indices {
		data, err := r.renderPage(index, options)
		if err != nil {
			return nil, err
		}
		results = append(results, data)
	}
	return results, nil
}

// RenderPDF 将多个页面按传入顺序写入一个保留文字和矢量内容的 PDF 文档。
// 该接口会把完整 PDF 保存在内存中，需要导出大量页面时应使用 RenderPDFTo。
func (r *Reader) RenderPDF(indices []int, options RenderOptions) (outputBytes []byte, err error) {
	var output bytes.Buffer
	err = r.RenderPDFTo(&output, indices, options)
	if err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// RenderPDFTo 将多个页面按传入顺序写入 output，保留文字和矢量内容。
// output 会在 PDF 生成过程中接收数据，适合流式保存大 PDF，因此不限制页数。
func (r *Reader) RenderPDFTo(output io.Writer, indices []int, options RenderOptions) (err error) {
	if r == nil {
		return errors.New("文档引擎为空")
	}
	if output == nil {
		return errors.New("PDF 输出为空")
	}
	if len(indices) == 0 {
		return errors.New("PDF 页面列表为空")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return errors.New("文档引擎已经关闭")
	}
	background := options.Background
	if background == nil {
		background = color.Transparent
	}
	dpi := options.DPI
	if dpi == 0 {
		dpi = defaultDPI
	}
	if dpi < 1 || dpi > maxDPI || math.IsNaN(dpi) || math.IsInf(dpi, 0) {
		return fmt.Errorf("DPI 必须在 1 到 %d 之间", maxDPI)
	}
	// TrueType 字体应进行子集化，避免将完整中文字体写入每个 PDF。
	// CFF/TTC 回退字体暂时不能交给 canvas 的 CFF 子集器；对这类字体
	// 关闭子集化仍会保留 ToUnicode 和原生文字对象，避免 Close 时 panic。
	pdfDoc, err := render.NewPDFDocument(output, render.PDFOptions{Compress: true, SubsetFonts: !r.hasUnsafeFallbackFontSubset()})
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := pdfDoc.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("关闭 PDF 文档失败: %w", closeErr)
		}
	}()
	for position, index := range indices {
		page, pageErr := r.pdfPage(index, background, geom.DPI(dpi))
		if pageErr != nil {
			return fmt.Errorf("处理 PDF 第 %d 页失败: %w", position+1, pageErr)
		}
		resolution := geom.DPI(dpi)
		width := page.Width() * resolution.DPMM()
		height := page.Height() * resolution.DPMM()
		if math.IsNaN(width) || math.IsInf(width, 0) || math.IsNaN(height) || math.IsInf(height, 0) || width*height > maxRenderPixels {
			return fmt.Errorf("第 %d 页 PDF 渲染尺寸过大", position+1)
		}
		if addErr := pdfDoc.AddPage(page); addErr != nil {
			return fmt.Errorf("处理 PDF 第 %d 页失败: %w", position+1, addErr)
		}
	}
	return nil
}

func (r *Reader) hasUnsafeFallbackFontSubset() bool {
	for _, family := range r.fallbackFamilies {
		data, ok := render.FallbackFontData(family)
		if !ok || len(data) < 4 {
			continue
		}
		signature := string(data[:4])
		if signature == "OTTO" || signature == "ttcf" {
			return true
		}
	}
	return false
}

// pdfPage 获取 PDF 渲染所需的页面画布；调用方必须持有 Reader 读锁。
func (r *Reader) pdfPage(index int, background color.Color, dpi geom.Resolution) (render.VectorSurface, error) {
	if index < 0 || index >= len(r.pages) {
		return nil, fmt.Errorf("页面索引超出范围: %d", index)
	}
	ref := r.pages[index]
	lease, err := ref.page.AcquireLease()
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	content := lease.Content()
	if content == nil {
		return nil, errors.New("页面内容为空")
	}
	document, err := r.pageDocument(ref, background, dpi)
	if err != nil {
		return nil, err
	}
	return document.Page(ref.page)
}

// renderPage 将页面渲染为 PNG、JPG 或 SVG；调用方必须持有 Reader 读锁。
func (r *Reader) renderPage(index int, options RenderOptions) ([]byte, error) {
	if index < 0 || index >= len(r.pages) {
		return nil, fmt.Errorf("页面索引超出范围: %d", index)
	}
	if options.DPI == 0 {
		options.DPI = defaultDPI
	}
	if options.DPI < 1 || options.DPI > maxDPI || math.IsNaN(options.DPI) || math.IsInf(options.DPI, 0) {
		return nil, fmt.Errorf("DPI 必须在 1 到 %d 之间", maxDPI)
	}
	format := RenderFormat(strings.ToLower(strings.TrimSpace(string(options.Format))))
	if format == "" {
		format = RenderPNG
	}
	if format != RenderPNG && format != RenderSVG && format != RenderJPG {
		return nil, fmt.Errorf("不支持的页面输出格式: %q", format)
	}

	ref := r.pages[index]
	lease, err := ref.page.AcquireLease()
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	content := lease.Content()
	if content == nil {
		return nil, fmt.Errorf("页面内容为空")
	}
	box := content.Area.PhysicalBox
	if !finitePositive(box.Width) || !finitePositive(box.Height) {
		return nil, fmt.Errorf("第 %d 页尺寸无效", index)
	}
	pixels := box.Width * options.DPI / 25.4 * box.Height * options.DPI / 25.4
	if math.IsNaN(pixels) || math.IsInf(pixels, 0) || pixels > maxRenderPixels {
		return nil, fmt.Errorf("第 %d 页渲染尺寸过大", index)
	}

	background := options.Background
	if background == nil {
		background = color.Transparent
	}
	// NewDocument 保证页面背景和渲染内容使用同一个文档级渲染上下文。
	document, err := r.pageDocument(ref, background, geom.DPI(options.DPI))
	if err != nil {
		return nil, err
	}
	page, err := document.Page(ref.page)
	if err != nil {
		return nil, fmt.Errorf("渲染第 %d 页失败: %w", index, err)
	}

	var output bytes.Buffer
	if format == RenderSVG {
		if err := page.Write(&output, "svg"); err != nil {
			return nil, fmt.Errorf("编码第 %d 页 SVG 失败: %w", index, err)
		}
		return output.Bytes(), nil
	}
	var rendered image.Image = page.Rasterize(geom.DPI(options.DPI))
	if format == RenderJPG {
		rendered = opaqueImage(rendered, color.White)
		if err := jpeg.Encode(&output, rendered, &jpeg.Options{Quality: 90}); err != nil {
			return nil, fmt.Errorf("编码第 %d 页 JPG 失败: %w", index, err)
		}
		return output.Bytes(), nil
	}
	if err := png.Encode(&output, rendered); err != nil {
		return nil, fmt.Errorf("编码第 %d 页 PNG 失败: %w", index, err)
	}
	return output.Bytes(), nil
}

func opaqueImage(source image.Image, background color.Color) image.Image {
	bounds := source.Bounds()
	result := image.NewRGBA(bounds)
	draw.Draw(result, bounds, &image.Uniform{C: background}, image.Point{}, draw.Src)
	draw.Draw(result, bounds, source, bounds.Min, draw.Over)
	return result
}

// pageDocument 获取页面对应的渲染文档；调用方必须持有 Reader 读锁。
func (r *Reader) pageDocument(ref pageRef, background color.Color, dpi geom.Resolution) (*render.Document, error) {
	document := ref.document
	if document == nil || document.Document == nil {
		return nil, errors.New("页面渲染上下文为空")
	}
	red, green, blue, alpha := background.RGBA()
	key := renderDocumentKey{base: document, dpi: dpi.DPI(), red: red, green: green, blue: blue, alpha: alpha}
	r.renderDocsMu.Lock()
	defer r.renderDocsMu.Unlock()
	if r.renderDocs != nil {
		if cached, ok := r.renderDocs.Get(key); ok && cached != nil {
			return cached, nil
		}
	}
	document = render.NewDocumentWithDPI(background, ref.document.Document, dpi)
	for _, family := range r.fallbackFamilies {
		// 字体已在打开/注册时全局登记，这里只把已锁定字体族应用到该文档。
		_ = document.UseFallbackFont(family)
	}
	if r.renderDocs == nil {
		r.renderDocs = utils.NewLRU[renderDocumentKey, *render.Document](maxRenderDocs, nil)
	}
	r.renderDocs.Add(key, document)
	return document, nil
}

// Close 释放文档资源。Close 可以安全地重复调用。
func (r *Reader) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	r.pages = nil
	r.text = nil
	r.search = nil
	r.fallbackFamily = ""
	r.fallbackFamilies = nil
	r.renderDocsMu.Lock()
	r.renderDocs = nil
	r.renderDocsMu.Unlock()
	ofd := r.ofd
	r.ofd = nil
	if ofd == nil {
		return nil
	}
	return ofd.Close()
}

func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func browserFontFamily(scope int, id uint64) string {
	return fmt.Sprintf("OFDFont-%d-%d", scope, id)
}

func fontFormat(file models.StLoc) string {
	name := strings.ToLower(string(file))
	if index := strings.LastIndexByte(name, '.'); index >= 0 {
		return name[index+1:]
	}
	return ""
}

func textRunsWithFallback(document *render.Document, page *parser.Page, fontScope int, fallbackFamily string) []TextRun {
	layouts := document.TextLayouts(page)
	runs := make([]TextRun, 0, len(layouts))
	for _, layout := range layouts {
		run := TextRun{
			Text: layout.Text, X: layout.X, Y: layout.Y, Width: layout.Width, Height: layout.Height,
			Scope: fontScope, Font: layout.Font, Size: layout.Size, ReadDirection: layout.ReadDirection,
			Weight:        layout.Weight,
			CharDirection: layout.CharDirection, FontFamily: textFontFamily(document, fontScope, layout.Font, fallbackFamily),
			Bold: layout.Bold, Italic: layout.Italic,
		}
		for _, glyph := range layout.Glyphs {
			run.Glyphs = append(run.Glyphs, Glyph{Text: glyph.Text, X: glyph.X, Y: glyph.Y, Width: glyph.Width, Height: glyph.Height, Angle: glyph.Angle})
		}
		runs = append(runs, run)
	}
	return runs
}

// textAt 在持有 Reader 锁时缓存不可变的文档布局。
// 向外部返回结果的调用方必须先复制结果。
func (r *Reader) textAt(index int) []TextRun {
	if runs, ok := r.text.Get(index); ok {
		return runs
	}
	runs := textRunsWithFallback(r.pages[index].document, r.pages[index].page, r.pages[index].fontScope, r.fallbackFamily)
	r.text.Add(index, runs)
	return runs
}

func textFontFamily(document *render.Document, scope int, id uint64, fallback string) string {
	if document != nil {
		if family := document.FallbackFontFamily(models.StRefID(id)); family != "" {
			return family
		}
		if document.HasLoadedEmbeddedFont(models.StRefID(id)) {
			return browserFontFamily(scope, id)
		}
	}
	return fallback
}

func (r *Reader) searchAt(index int, runs []TextRun) searchPage {
	if indexed, ok := r.search.Get(index); ok {
		return indexed
	}
	indexed := searchPage{
		runs:   make([][]rune, len(runs)),
		byRune: make(map[rune][]int),
	}
	for runIndex, run := range runs {
		indexed.runs[runIndex] = []rune(strings.ToLower(run.Text))
		seen := make(map[rune]struct{})
		for _, value := range indexed.runs[runIndex] {
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			indexed.byRune[value] = append(indexed.byRune[value], runIndex)
		}
	}
	r.search.Add(index, indexed)
	return indexed
}

func cloneTextRuns(source []TextRun) []TextRun {
	cloned := make([]TextRun, len(source))
	for index, run := range source {
		cloned[index] = run
		cloned[index].Glyphs = append([]Glyph(nil), run.Glyphs...)
	}
	return cloned
}

func sameColor(left, right color.Color) bool {
	if left == nil || right == nil {
		return left == right
	}
	lr, lg, lb, la := left.RGBA()
	rr, rg, rb, ra := right.RGBA()
	return lr == rr && lg == rg && lb == rb && la == ra
}
