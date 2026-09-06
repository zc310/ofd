// Package analyzer 包提供 OFD 文档结构、对象、资源和引用关系分析能力。
package analyzer

import (
	"encoding/json"
	"errors"
	"io"
	"time"
)

const (
	// SchemaVersion 是分析工具 JSON 报告的模式版本。
	SchemaVersion = "1"
	// ToolVersion 是 analyzer 的工具版本。
	ToolVersion = "0.0.1"
)

// Status 表示分析结果是否完整。
type Status string

const (
	StatusComplete Status = "complete"
	StatusPartial  Status = "partial"
	StatusFailed   Status = "failed"
)

// Options 控制分析工具的统计范围。
type Options struct {
	// IncludeTemplates 是否分析模板定义、引用和模板 PageRes 资源。默认开启。
	IncludeTemplates bool
	// IncludeAnnotations 是否分析注解及其外观。默认开启。
	IncludeAnnotations bool
	// IncludeSignatures 是否分析签名清单。默认开启。
	IncludeSignatures bool
	// IncludePackage 是否输出 ZIP 条目统计。默认开启。
	IncludePackage bool
}

// Option 配置分析工具。
type Option func(*Options)

// WithTemplates 设置是否分析模板定义、引用和模板 PageRes 资源。
// 关闭后仍保留文档元数据中的模板数量和文档到模板文件的结构引用。
func WithTemplates(enabled bool) Option {
	return func(options *Options) { options.IncludeTemplates = enabled }
}

// WithAnnotations 设置是否分析注解。
func WithAnnotations(enabled bool) Option {
	return func(options *Options) { options.IncludeAnnotations = enabled }
}

// WithSignatures 设置是否分析签名。
func WithSignatures(enabled bool) Option {
	return func(options *Options) { options.IncludeSignatures = enabled }
}

// WithPackage 设置是否输出 ZIP 条目统计。
func WithPackage(enabled bool) Option {
	return func(options *Options) { options.IncludePackage = enabled }
}

// Report 是分析工具的稳定 JSON 报告模型。
type Report struct {
	SchemaVersion   string                   `json:"schema_version"`
	Tool            ToolInfo                 `json:"tool"`
	Input           InputInfo                `json:"input"`
	Status          Status                   `json:"status"`
	Warnings        []string                 `json:"warnings,omitempty"`
	Errors          []string                 `json:"errors,omitempty"`
	OFD             OFDInfo                  `json:"ofd"`
	Package         PackageSummary           `json:"package"`
	Summary         Summary                  `json:"summary"`
	Documents       []DocumentInfo           `json:"documents"`
	Pages           []PageInfo               `json:"pages"`
	Objects         ObjectSummary            `json:"objects"`
	Text            TextSummary              `json:"text"`
	TemplateText    TextSummary              `json:"template_text"`
	AnnotationText  TextSummary              `json:"annotation_text"`
	Images          ResourceSummary          `json:"images"`
	Fonts           FontResourceSummary      `json:"fonts"`
	DrawParams      DrawParamResourceSummary `json:"draw_params"`
	ColorSpaces     ResourceSummary          `json:"color_spaces"`
	Templates       ResourceSummary          `json:"templates"`
	Composites      ResourceSummary          `json:"composites"`
	Patterns        ResourceSummary          `json:"patterns"`
	Resources       ResourceSummary          `json:"resources"`
	ResourceDetails []ResourceInfo           `json:"resource_details"`
	Attachments     []AttachmentInfo         `json:"attachments"`
	Annotations     []AnnotationInfo         `json:"annotations"`
	Signatures      []SignatureInfo          `json:"signatures"`
	FileReferences  []ReferenceEdge          `json:"file_references"`
	IDReferences    []ReferenceEdge          `json:"id_references"`
}

// ToolInfo 描述报告生成工具。
type ToolInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InputInfo 描述被分析的输入。
type InputInfo struct {
	Path string `json:"path"`
	Size int64  `json:"size,omitempty"`
}

// OFDInfo 描述 OFD 根元素的属性。
type OFDInfo struct {
	Version string `json:"version"`
	DocType string `json:"doc_type"`
}

// PackageSummary 描述 ZIP 包的基本统计。
type PackageSummary struct {
	Entries           int    `json:"entries"`
	Files             int    `json:"files"`
	Directories       int    `json:"directories"`
	XMLFiles          int    `json:"xml_files"`
	CompressedBytes   uint64 `json:"compressed_bytes"`
	UncompressedBytes uint64 `json:"uncompressed_bytes"`
}

// Summary 是文档级汇总统计。
type Summary struct {
	DocumentBodies      int `json:"document_bodies"`
	Pages               int `json:"pages"`
	ParsedPages         int `json:"parsed_pages"`
	Objects             int `json:"objects"`
	PageObjects         int `json:"page_objects"`
	AnnotationObjects   int `json:"annotation_objects"`
	TextCharacters      int `json:"text_characters"`
	PageTextCharacters  int `json:"page_text_characters"`
	AnnotationTextChars int `json:"annotation_text_characters"`
	Images              int `json:"images"`
	Fonts               int `json:"fonts"`
	DrawParams          int `json:"draw_params"`
	ColorSpaces         int `json:"color_spaces"`
	Templates           int `json:"templates"`
	Composites          int `json:"composites"`
	Patterns            int `json:"patterns"`
	Attachments         int `json:"attachments"`
	Annotations         int `json:"annotations"`
	Signatures          int `json:"signatures"`
}

// DocumentInfo 描述一个文档体及其元数据。
type DocumentInfo struct {
	Index          int        `json:"index"`
	DocID          string     `json:"doc_id"`
	Title          *string    `json:"title,omitempty"`
	Author         *string    `json:"author,omitempty"`
	Subject        *string    `json:"subject,omitempty"`
	Abstract       *string    `json:"abstract,omitempty"`
	CreationDate   *time.Time `json:"creation_date,omitempty"`
	ModDate        *time.Time `json:"mod_date,omitempty"`
	DocUsage       *string    `json:"doc_usage,omitempty"`
	Creator        *string    `json:"creator,omitempty"`
	CreatorVersion *string    `json:"creator_version,omitempty"`
	Keywords       []string   `json:"keywords,omitempty"`
	DocRoot        string     `json:"doc_root"`
	DeclaredPages  int        `json:"declared_pages"`
	ParsedPages    int        `json:"parsed_pages"`
	TemplateCount  int        `json:"template_count"`
	ResourceFiles  int        `json:"resource_files"`
	HasCover       bool       `json:"has_cover"`
	HasAttachments bool       `json:"has_attachments"`
	HasAnnotations bool       `json:"has_annotations"`
	HasSignatures  bool       `json:"has_signatures"`
}

// PageInfo 描述一个页面及其对象、文本和直接资源引用。
type PageInfo struct {
	DocumentIndex int           `json:"document_index"`
	DocumentPage  int           `json:"document_page"`
	PageNumber    int           `json:"page_number"`
	ID            uint64        `json:"id"`
	BaseLoc       string        `json:"base_loc,omitempty"`
	Layers        int           `json:"layers"`
	Size          PageSize      `json:"size"`
	Objects       ObjectCounts  `json:"objects"`
	Text          TextSummary   `json:"text"`
	Resources     PageResources `json:"resources"`
}

// PageSize 描述页面物理尺寸和方向。
type PageSize struct {
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	Width       float64 `json:"width"`
	Height      float64 `json:"height"`
	Unit        string  `json:"unit"`
	Orientation string  `json:"orientation"`
	Source      string  `json:"source"`
}

// ObjectSummary 是实际展开分析的页面对象和注解外观对象的汇总。
type ObjectSummary struct {
	Total                  int          `json:"total"`
	Pages                  int          `json:"pages"`
	AnnotationAppearances  int          `json:"annotation_appearances"`
	AnnotationObjectCounts ObjectCounts `json:"annotation_object_counts"`
	Text                   int          `json:"text"`
	Path                   int          `json:"path"`
	Image                  int          `json:"image"`
	Composite              int          `json:"composite"`
	PageBlock              int          `json:"page_block"`
	PathCommands           int          `json:"path_commands"`
	MaxPageBlockDepth      int          `json:"max_page_block_depth"`
}

// ObjectCounts 是单页面或单个对象容器的对象计数。
type ObjectCounts struct {
	Total        int `json:"total"`
	Text         int `json:"text"`
	Path         int `json:"path"`
	Image        int `json:"image"`
	Composite    int `json:"composite"`
	PageBlock    int `json:"page_block"`
	PathCommands int `json:"path_commands"`
}

// TextSummary 是文字统计。字符数按 Unicode 码点统计。
type TextSummary struct {
	Objects                 int `json:"objects"`
	TextCodes               int `json:"text_codes"`
	UTF8Bytes               int `json:"utf8_bytes"`
	UnicodeCodePoints       int `json:"unicode_code_points"`
	WhitespaceCodePoints    int `json:"whitespace_code_points"`
	NonWhitespaceCodePoints int `json:"non_whitespace_code_points"`
	Glyphs                  int `json:"glyphs"`
}

// ResourceSummary 是通用资源定义和引用统计。
type ResourceSummary struct {
	Files        int            `json:"files"`
	Declared     int            `json:"declared"`
	Used         int            `json:"used"`
	References   int            `json:"references"`
	UniqueUsed   int            `json:"unique_used"`
	Unresolved   int            `json:"unresolved"`
	MissingFiles int            `json:"missing_files"`
	Unused       int            `json:"unused"`
	ByType       map[string]int `json:"by_type,omitempty"`
}

// FontResourceSummary 是字体资源统计。
type FontResourceSummary struct {
	ResourceSummary
	Embedded int `json:"embedded"`
}

// DrawParamResourceSummary 是绘制参数资源统计。
type DrawParamResourceSummary struct {
	ResourceSummary
	InheritanceCycles int `json:"inheritance_cycles"`
}

// ResourceInfo 描述一个资源定义及其使用情况。
type ResourceInfo struct {
	DocumentIndex int    `json:"document_index"`
	ID            uint64 `json:"id"`
	Kind          string `json:"kind"`
	Scope         string `json:"scope,omitempty"`
	SourceFile    string `json:"source_file"`
	Path          string `json:"path,omitempty"`
	Exists        bool   `json:"exists"`
	Used          int    `json:"used"`
	Embedded      bool   `json:"embedded"`
	FontName      string `json:"font_name,omitempty"`
	FamilyName    string `json:"family_name,omitempty"`
	Charset       string `json:"charset,omitempty"`
	Italic        bool   `json:"italic,omitempty"`
	Bold          bool   `json:"bold,omitempty"`
	Serif         bool   `json:"serif,omitempty"`
	FixedWidth    bool   `json:"fixed_width,omitempty"`
}

// PageResources 描述页面内容直接引用的资源 ID。
type PageResources struct {
	Images      []uint64 `json:"images,omitempty"`
	Fonts       []uint64 `json:"fonts,omitempty"`
	Templates   []uint64 `json:"templates,omitempty"`
	DrawParams  []uint64 `json:"draw_params,omitempty"`
	Composites  []uint64 `json:"composites,omitempty"`
	ColorSpaces []uint64 `json:"color_spaces,omitempty"`
}

// AttachmentInfo 描述附件清单项。
type AttachmentInfo struct {
	DocumentIndex int      `json:"document_index"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Format        *string  `json:"format,omitempty"`
	Path          string   `json:"path"`
	DeclaredSize  *float64 `json:"declared_size,omitempty"`
	ActualSize    uint64   `json:"actual_size,omitempty"`
	Exists        bool     `json:"exists"`
	Visible       bool     `json:"visible"`
	Usage         string   `json:"usage,omitempty"`
}

// AnnotationInfo 描述注解清单项。
type AnnotationInfo struct {
	DocumentIndex int          `json:"document_index"`
	PageID        uint64       `json:"page_id"`
	ID            string       `json:"id"`
	Type          string       `json:"type"`
	Subtype       string       `json:"subtype,omitempty"`
	Creator       string       `json:"creator,omitempty"`
	Visible       bool         `json:"visible"`
	Print         bool         `json:"print"`
	HasAppearance bool         `json:"has_appearance"`
	Objects       ObjectCounts `json:"objects"`
}

// SignatureInfo 描述签名清单项。
type SignatureInfo struct {
	DocumentIndex     int      `json:"document_index"`
	ID                string   `json:"id"`
	Type              string   `json:"type,omitempty"`
	Path              string   `json:"path,omitempty"`
	Provider          string   `json:"provider,omitempty"`
	Company           string   `json:"company,omitempty"`
	Version           string   `json:"version,omitempty"`
	Method            string   `json:"method,omitempty"`
	Date              string   `json:"date,omitempty"`
	CheckMethod       string   `json:"check_method,omitempty"`
	ReferenceCount    int      `json:"reference_count"`
	StampCount        int      `json:"stamp_count"`
	Pages             []uint64 `json:"pages,omitempty"`
	SignedValue       string   `json:"signed_value,omitempty"`
	SignedValueExists bool     `json:"signed_value_exists"`
	Seal              string   `json:"seal,omitempty"`
	SealExists        bool     `json:"seal_exists"`
}

// ReferenceEdge 描述一条文件或逻辑 ID 引用关系。
type ReferenceEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Type   string `json:"type"`
	Source string `json:"source,omitempty"`
	Exists bool   `json:"exists"`
	Count  int    `json:"count"`
}

// RenderJSON 将分析报告写为 JSON。
func RenderJSON(writer io.Writer, report Report, pretty bool) error {
	if writer == nil {
		return errors.New("分析报告输出为空")
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(report)
}
