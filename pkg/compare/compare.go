// Package compare 提供 OFD 包内文件、XML 结构、页面文字和渲染画面的对比能力。
package compare

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/zc310/ofd/internal/localization"
	"github.com/zc310/ofd/internal/version"
)

// SchemaVersion 标识 JSON 报告格式，与工具发行版本相互独立。
const SchemaVersion = "1"

// Options 控制对比范围。零值启用文件、结构和文字对比。
type Options struct {
	// Language selects report language (zh-CN or en); unsupported tags fall back to zh-CN.
	Language string `json:"language,omitempty"`
	// Baseline 是从 0 开始的输入索引，其他文件分别与该基准比较。
	Baseline int `json:"baseline_index"`
	// PageMatch 为 auto（默认）或 number。自动匹配优先关联相同页面内容或
	// 唯一的相同非空文字，再结合定位点和文字相似度配对修改页。
	PageMatch string `json:"page_match"`
	// PageMap 手工指定各目标相对基准的原始页码，例如 "2:3,4:5"。
	// 两侧页码不能重复，必须在所选范围内；仅适用于 auto 匹配。
	PageMap string `json:"page_map,omitempty"`
	// Pages 指定各输入中从 1 开始的全局页码，例如 "1-3,5"。
	// 空值选择全部页面；包内文件对比始终覆盖所有条目。
	Pages string `json:"pages,omitempty"`
	// Visual 启用像素对比及页面预览，默认内嵌图片。
	Visual bool `json:"visual"`
	// DPI 默认为 72；PixelTolerance 是每个颜色通道允许的差值，范围为 0–255。
	DPI            float64 `json:"dpi"`
	PixelTolerance int     `json:"pixel_tolerance"`
	// MinRegionPixels 过滤较小的视觉区域，不改变原始像素统计和相同判定。
	// 零值按 1 处理，即保留所有变化区域。
	MinRegionPixels int `json:"min_region_pixels"`
	// MaxXMLBytes 限制每个 XML 的结构分析大小，默认为 8 MiB。超过上限时
	// 仍计算文件摘要，并明确记录未进行结构比较。
	MaxXMLBytes int64 `json:"max_xml_bytes"`
	// MaxXMLStructureBytes 限制单个 XML 字段表的估算大小，默认 32 MiB。
	// 每次只保留当前文件两侧的字段表，比较后释放。
	MaxXMLStructureBytes int64 `json:"max_xml_structure_bytes"`
	// MaxChanges 限制每个 XML 的差异明细条数，默认为 200。
	MaxChanges int `json:"max_changes"`
	// MaxPagePixels 和 MaxTotalPixels 限制单页与累计视觉工作量，默认分别为 1000 万和 10 亿。
	MaxPagePixels  int64 `json:"max_page_pixels"`
	MaxTotalPixels int64 `json:"max_total_pixels"`
	// MaxPreviewBytes 限制所有预览图累计编码后的 PNG 字节数，默认 64 MiB。
	// MaxReportBytes 限制保留的报告数据和最终输出字节数，默认 256 MiB。
	MaxPreviewBytes int64 `json:"max_preview_bytes"`
	MaxReportBytes  int64 `json:"max_report_bytes"`
	// BaselineCacheBytes 限制基准页 RGBA 缓存，默认 64 MiB；-1 禁用。
	// 缓存命中不重复扣渲染像素预算，超过容量的单页不会缓存。
	BaselineCacheBytes int64 `json:"baseline_cache_bytes"`
	// ImageWriter 可将每张 PNG 写入外部存储并返回相对图片 URL。
	// 返回地址必须为安全的相对路径；零值将图片以 base64 内嵌。
	// 外部图片的创建和失败清理由调用方负责，写入错误会终止比较。
	ImageWriter func([]byte) (string, error) `json:"-"`
	images      *imageStore
	dataBudget  *byteBudget
}

// Report 记录各目标相对 Inputs[Baseline-1] 的差异。报告中的输入和页面序号
// 从 1 开始；页面或文件在一侧不存在时，省略对应字段。
type Report struct {
	SchemaVersion string       `json:"schema_version"`
	ToolVersion   string       `json:"tool_version"`
	Baseline      int          `json:"baseline"`
	Options       Options      `json:"options"`
	Inputs        []Input      `json:"inputs"`
	Comparisons   []Comparison `json:"comparisons"`
}

type Input struct {
	Name        string         `json:"name"`
	Size        int64          `json:"size"`
	Documents   int            `json:"documents"`
	Files       []File         `json:"files"`
	Directories []string       `json:"directories"`
	Pages       []Page         `json:"pages"`
	Metadata    []DocumentInfo `json:"metadata"`
}

type DocumentInfo struct {
	Number  int    `json:"number"`
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	Author  string `json:"author,omitempty"`
	Subject string `json:"subject,omitempty"`
	Creator string `json:"creator,omitempty"`
}

type File struct {
	Path      string `json:"path"`
	Size      uint64 `json:"size"`
	SHA256    string `json:"sha256"`
	Kind      string `json:"kind"`
	XMLSHA256 string `json:"xml_sha256,omitempty"`
	XMLNote   string `json:"xml_note,omitempty"`
}

type Page struct {
	Number         int     `json:"number"`
	Document       int     `json:"document"`
	PageInDocument int     `json:"page_in_document"`
	Width          float64 `json:"width_mm"`
	Height         float64 `json:"height_mm"`
	Text           string  `json:"text"`
	ContentSHA256  string  `json:"content_sha256"`
	// Selected 标记是否属于 Options.Pages。未选中的页面只保留序号，
	// 不提取文字、尺寸或内容摘要，也不参与配对和渲染。
	Selected bool `json:"selected"`
}

type Comparison struct {
	Target             int          `json:"target"`
	Equal              bool         `json:"equal"`
	PagesEqual         bool         `json:"pages_equal"`
	Summary            Summary      `json:"summary"`
	DirectoriesAdded   []string     `json:"directories_added"`
	DirectoriesRemoved []string     `json:"directories_removed"`
	Files              []FileChange `json:"files"`
	Pages              []PageChange `json:"pages"`
}

type Summary struct {
	FilesAdded         int `json:"files_added"`
	FilesRemoved       int `json:"files_removed"`
	FilesModified      int `json:"files_modified"`
	FilesMoved         int `json:"files_moved"`
	PagesAdded         int `json:"pages_added"`
	PagesRemoved       int `json:"pages_removed"`
	PagesModified      int `json:"pages_modified"`
	PagesMoved         int `json:"pages_moved"`
	VisualPagesChanged int `json:"visual_pages_changed"`
}

type FileChange struct {
	Status       string        `json:"status"`
	Before       *File         `json:"before,omitempty"`
	After        *File         `json:"after,omitempty"`
	XMLChanges   []ValueChange `json:"xml_changes,omitempty"`
	XMLTruncated bool          `json:"xml_truncated,omitempty"`
	// XMLView 保存有大小限制的规范化 XML 行对照；字段差异仍由 XMLChanges 提供。
	XMLView       *XMLView `json:"xml_view,omitempty"`
	BeforePreview string   `json:"before_preview,omitempty"`
	AfterPreview  string   `json:"after_preview,omitempty"`
}

// XMLView 区分 format（仅格式变化）、structure、added、removed、unavailable。
// 规范化显示统一前缀、属性顺序和缩进，忽略注释；行号属于显示内容。
type XMLView struct {
	Kind   string         `json:"kind"`
	Blocks []XMLViewBlock `json:"blocks,omitempty"`
	Coarse bool           `json:"coarse,omitempty"`
	Note   string         `json:"note,omitempty"`
}

// XMLViewBlock 将相邻的相同行或变化行聚合；Folded 表示默认折叠相同部分。
type XMLViewBlock struct {
	Equal  bool          `json:"equal"`
	Folded bool          `json:"folded"`
	Lines  []XMLViewLine `json:"lines"`
}

// XMLViewLine 按行对齐两侧内容，行号为 0 表示该侧没有对应行。
type XMLViewLine struct {
	BeforeNumber int    `json:"before_number"`
	AfterNumber  int    `json:"after_number"`
	Before       string `json:"before,omitempty"`
	After        string `json:"after,omitempty"`
}

// ValueChange 使用命名空间展开名标识 XML 元素或属性。
// nil 表示该值不存在，非 nil 的空字符串表示值存在但为空。
type ValueChange struct {
	Path   string  `json:"path"`
	Before *string `json:"before,omitempty"`
	After  *string `json:"after,omitempty"`
}

type PageChange struct {
	Status string `json:"status"`
	Before int    `json:"before,omitempty"`
	After  int    `json:"after,omitempty"`
	Moved  bool   `json:"moved"`
	// Match 记录实际采用的配对依据，独立于页面是否发生变化。
	Match          PageMatchInfo `json:"match"`
	TextChanged    bool          `json:"text_changed"`
	SizeChanged    bool          `json:"size_changed"`
	TextDiff       []TextSpan    `json:"text_diff,omitempty"`
	TextDiffCoarse bool          `json:"text_diff_coarse,omitempty"`
	Visual         *VisualChange `json:"visual,omitempty"`
}

// PageMatchInfo 描述配对依据：manual、content、text、text_similarity、
// sequence（自动匹配的顺序兜底）、number（显式顺序匹配）、unmatched。
type PageMatchInfo struct {
	Method string `json:"method"`
	// TextSimilarity 仅用于 text_similarity，范围为 0–1，是文字特征相似度，
	// 不代表页面相同的概率或视觉相似度。
	TextSimilarity float64 `json:"text_similarity,omitempty"`
	// SearchLimited 表示相似度搜索达到工作量上限；只标记随后顺序配对或未匹配的页面。
	SearchLimited bool `json:"search_limited,omitempty"`
}

// TextSpan 描述按 Unicode 字符计算的编辑操作：equal 为共同文字，delete
// 只属于基准，insert 只属于目标。拼接 equal+delete 可还原基准原文，
// 拼接 equal+insert 可还原目标原文，空白字符完整保留。
type TextSpan struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

type VisualChange struct {
	ChangedPixels int64   `json:"changed_pixels"`
	TotalPixels   int64   `json:"total_pixels"`
	Ratio         float64 `json:"ratio"`
	// Bounds 是变化像素的整体矩形，采用半开区间 [x0,y0,x1,y1)。
	Bounds              [4]int         `json:"bounds"`
	BeforePNG           string         `json:"before_png,omitempty"`
	AfterPNG            string         `json:"after_png,omitempty"`
	DiffPNG             string         `json:"diff_png,omitempty"`
	Width               int            `json:"width"`
	Height              int            `json:"height"`
	Regions             []VisualRegion `json:"regions,omitempty"`
	RegionCount         int            `json:"region_count"`
	RegionsTruncated    bool           `json:"regions_truncated,omitempty"`
	IgnoredRegionPixels int64          `json:"ignored_region_pixels"`
}

// VisualRegion 聚合相邻的变化网格。Bounds 以页面像素坐标覆盖这些网格，
// ChangedPixels 保存实际变化像素数量。
type VisualRegion struct {
	Bounds        [4]int `json:"bounds"`
	ChangedPixels int64  `json:"changed_pixels"`
}

// CompareFiles 对比两份或多份本地 OFD 文件，源文件只读。
// 输入、解析、渲染或取消失败均返回错误；选中页面不会因失败而被静默跳过。
func CompareFiles(ctx context.Context, paths []string, options Options) (*Report, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(paths) < 2 {
		return nil, errors.New("至少需要两份 OFD 文件")
	}
	options.Language = localization.CanonicalLanguage(options.Language)
	if options.Baseline < 0 || options.Baseline >= len(paths) {
		return nil, errors.New("基准文件序号超出范围")
	}
	if options.PageMatch == "" {
		options.PageMatch = "auto"
	}
	if options.PageMatch != "auto" && options.PageMatch != "number" {
		return nil, errors.New("页面匹配方式必须为 auto 或 number")
	}
	if _, err := ParsePageMap(options.PageMap); err != nil {
		return nil, err
	}
	if options.PageMap != "" && options.PageMatch != "auto" {
		return nil, errors.New("手工页映射仅适用于 auto 匹配")
	}
	if options.DPI == 0 {
		options.DPI = 72
	}
	if math.IsNaN(options.DPI) || math.IsInf(options.DPI, 0) || options.DPI < 10 || options.DPI > 300 {
		return nil, errors.New("DPI 必须在 10–300 之间")
	}
	if options.PixelTolerance < 0 || options.PixelTolerance > 255 {
		return nil, errors.New("像素容差必须在 0–255 之间")
	}
	if options.MinRegionPixels == 0 {
		options.MinRegionPixels = 1
	}
	if options.MinRegionPixels < 0 {
		return nil, errors.New("区域最小像素数必须为正数")
	}
	if _, err := ParsePageRanges(options.Pages); err != nil {
		return nil, err
	}
	if options.MaxXMLBytes == 0 {
		options.MaxXMLBytes = 8 << 20
	}
	if options.MaxXMLStructureBytes == 0 {
		options.MaxXMLStructureBytes = 32 << 20
	}
	if options.MaxPreviewBytes == 0 {
		options.MaxPreviewBytes = 64 << 20
	}
	if options.MaxReportBytes == 0 {
		options.MaxReportBytes = 256 << 20
	}
	if options.BaselineCacheBytes == 0 {
		options.BaselineCacheBytes = 64 << 20
	}
	if options.BaselineCacheBytes < -1 {
		return nil, errors.New("基准页缓存大小必须为正数或 -1（禁用）")
	}
	if options.MaxChanges == 0 {
		options.MaxChanges = 200
	}
	if options.MaxPagePixels == 0 {
		options.MaxPagePixels = 10_000_000
	}
	if options.MaxTotalPixels == 0 {
		options.MaxTotalPixels = 1_000_000_000
	}
	if options.MaxXMLBytes < 0 || options.MaxXMLBytes >= math.MaxInt64 || options.MaxChanges < 0 || options.MaxPagePixels < 0 || options.MaxTotalPixels < 0 || options.MaxXMLStructureBytes < 0 || options.MaxPreviewBytes < 0 || options.MaxReportBytes < 0 {
		return nil, errors.New("对比资源限制必须为正数")
	}
	options.images = &imageStore{remaining: options.MaxPreviewBytes, writer: options.ImageWriter}
	options.dataBudget = &byteBudget{remaining: options.MaxReportBytes, label: "报告数据"}
	base, err := openSnapshot(ctx, paths[options.Baseline], options)
	if err != nil {
		return nil, fmt.Errorf("基准文件: %w", err)
	}
	defer base.close()
	if options.Visual && len(paths) > 2 && options.BaselineCacheBytes > 0 {
		base.cache = newPageCache(options.BaselineCacheBytes)
	}
	reportOptions := options
	reportOptions.images, reportOptions.dataBudget, reportOptions.ImageWriter = nil, nil, nil
	report := &Report{SchemaVersion: SchemaVersion, ToolVersion: version.Version, Baseline: options.Baseline + 1, Options: reportOptions, Inputs: make([]Input, len(paths)), Comparisons: make([]Comparison, 0, len(paths)-1)}
	report.Inputs[options.Baseline] = base.input
	remainingPixels := options.MaxTotalPixels
	for i, path := range paths {
		if i == options.Baseline {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c, input, err := compareTarget(ctx, base, path, options, &remainingPixels)
		if err != nil {
			return nil, fmt.Errorf("对比 %q: %w", path, err)
		}
		c.Target = i + 1
		report.Inputs[i] = input
		report.Comparisons = append(report.Comparisons, c)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return report, nil
}

func compareTarget(ctx context.Context, base *snapshot, path string, options Options, remaining *int64) (Comparison, Input, error) {
	target, err := openSnapshot(ctx, path, options)
	if err != nil {
		return Comparison{}, Input{}, err
	}
	defer target.close()
	c, err := compareSnapshots(ctx, base, target, options, remaining)
	return c, target.input, err
}
