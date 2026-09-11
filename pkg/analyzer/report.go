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
	// IncludeTree 是否输出 ZIP 包目录树。默认关闭。
	IncludeTree bool
	// SignatureUID 是可选的 SM2 签名用户标识。
	SignatureUID []byte
	// SignatureFormat 是 SM2 签名值格式，支持 auto、der 和 raw。
	SignatureFormat string
	// SignatureTrustRootsPEM 是可选的 PEM 信任根证书集合。
	SignatureTrustRootsPEM []byte
	// SignatureCRLsPEM 是可选的 PEM/DER CRL 集合。
	SignatureCRLsPEM []byte
	// SignatureRevocationIssuersPEM 是用于验证 CRL 签名的 PEM 证书集合。
	SignatureRevocationIssuersPEM []byte
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

// WithTree 设置是否输出 ZIP 包目录树。
func WithTree(enabled bool) Option {
	return func(options *Options) { options.IncludeTree = enabled }
}

// WithSignatureUID 设置 SM2 签名用户标识。
func WithSignatureUID(uid string) Option {
	return func(options *Options) { options.SignatureUID = []byte(uid) }
}

// WithSignatureFormat 设置 SM2 签名值格式。
func WithSignatureFormat(format string) Option {
	return func(options *Options) { options.SignatureFormat = format }
}

// WithSignatureTrustRootsPEM 设置证书链校验使用的 PEM 信任根证书。
func WithSignatureTrustRootsPEM(certificates []byte) Option {
	return func(options *Options) { options.SignatureTrustRootsPEM = append([]byte(nil), certificates...) }
}

// WithSignatureCRLsPEM 设置离线吊销校验使用的 CRL 文件内容。
func WithSignatureCRLsPEM(crls []byte) Option {
	return func(options *Options) { options.SignatureCRLsPEM = append([]byte(nil), crls...) }
}

// WithSignatureRevocationIssuersPEM 设置用于验证 CRL 签名的证书文件内容。
func WithSignatureRevocationIssuersPEM(certificates []byte) Option {
	return func(options *Options) {
		options.SignatureRevocationIssuersPEM = append([]byte(nil), certificates...)
	}
}

// Report 是分析工具的稳定 JSON 报告模型。
type Report struct {
	// SchemaVersion 是报告 JSON 模式的版本。
	SchemaVersion string `json:"schema_version"`
	// Tool 是生成报告的工具信息。
	Tool ToolInfo `json:"tool"`
	// Input 是被分析输入的信息。
	Input InputInfo `json:"input"`
	// Status 是分析结果的完整性状态。
	Status Status `json:"status"`
	// Warnings 是分析过程中产生的警告信息。
	Warnings []string `json:"warnings,omitempty"`
	// Errors 是分析过程中产生的错误信息。
	Errors []string `json:"errors,omitempty"`
	// OFD 是 OFD 根元素的信息。
	OFD OFDInfo `json:"ofd"`
	// Package 是 ZIP 包的统计信息。
	Package PackageSummary `json:"package"`
	// Summary 是文档级汇总统计。
	Summary Summary `json:"summary"`
	// Documents 是文档体信息列表。
	Documents []DocumentInfo `json:"documents"`
	// Pages 是页面信息列表。
	Pages []PageInfo `json:"pages"`
	// Objects 是对象汇总统计。
	Objects ObjectSummary `json:"objects"`
	// Text 是页面正文文字统计。
	Text TextSummary `json:"text"`
	// TemplateText 是模板文字统计。
	TemplateText TextSummary `json:"template_text"`
	// AnnotationText 是注解外观文字统计。
	AnnotationText TextSummary `json:"annotation_text"`
	// Images 是图片资源统计。
	Images ResourceSummary `json:"images"`
	// Fonts 是字体资源统计。
	Fonts FontResourceSummary `json:"fonts"`
	// DrawParams 是绘制参数资源统计。
	DrawParams DrawParamResourceSummary `json:"draw_params"`
	// ColorSpaces 是颜色空间资源统计。
	ColorSpaces ResourceSummary `json:"color_spaces"`
	// Templates 是模板资源统计。
	Templates ResourceSummary `json:"templates"`
	// Composites 是复合对象资源统计。
	Composites ResourceSummary `json:"composites"`
	// Patterns 是图案资源统计。
	Patterns ResourceSummary `json:"patterns"`
	// Resources 是所有资源的汇总统计。
	Resources ResourceSummary `json:"resources"`
	// ResourceDetails 是资源定义及使用情况列表。
	ResourceDetails []ResourceInfo `json:"resource_details"`
	// Attachments 是附件清单。
	Attachments []AttachmentInfo `json:"attachments"`
	// Annotations 是注解清单。
	Annotations []AnnotationInfo `json:"annotations"`
	// Signatures 是签名清单。
	Signatures []SignatureInfo `json:"signatures"`
	// FileReferences 是文件引用关系列表。
	FileReferences []ReferenceEdge `json:"file_references"`
	// IDReferences 是逻辑 ID 引用关系列表。
	IDReferences []ReferenceEdge `json:"id_references"`
}

// ToolInfo 描述报告生成工具。
type ToolInfo struct {
	// Name 是工具名称。
	Name string `json:"name"`
	// Version 是工具版本。
	Version string `json:"version"`
}

// InputInfo 描述被分析的输入。
type InputInfo struct {
	// Path 是输入文件路径。
	Path string `json:"path"`
	// Size 是输入内容大小，单位为字节。
	Size int64 `json:"size,omitempty"`
}

// OFDInfo 描述 OFD 根元素的属性。
type OFDInfo struct {
	// Version 是 OFD 根元素声明的版本。
	Version string `json:"version"`
	// DocType 是 OFD 根元素声明的文档类型。
	DocType string `json:"doc_type"`
}

// PackageSummary 描述 ZIP 包的基本统计。
type PackageSummary struct {
	// Entries 是 ZIP 包条目总数。
	Entries int `json:"entries"`
	// Files 是 ZIP 包文件条目数。
	Files int `json:"files"`
	// Directories 是 ZIP 包目录条目数。
	Directories int `json:"directories"`
	// XMLFiles 是 ZIP 包 XML 文件数。
	XMLFiles int `json:"xml_files"`
	// CompressedBytes 是 ZIP 包条目的压缩后总字节数。
	CompressedBytes uint64 `json:"compressed_bytes"`
	// UncompressedBytes 是 ZIP 包条目的未压缩总字节数。
	UncompressedBytes uint64 `json:"uncompressed_bytes"`
	// Tree 是 ZIP 包目录树根节点。
	Tree *PackageTree `json:"tree,omitempty"`
}

// PackageTree 描述 ZIP 包目录树中的一个节点。
type PackageTree struct {
	// Name 是当前节点的名称。
	Name string `json:"name"`
	// Path 是当前节点在 ZIP 包中的路径。
	Path string `json:"path,omitempty"`
	// Kind 是当前节点的类型。
	Kind string `json:"kind"`
	// MediaType 是当前节点的媒体类型。
	MediaType string `json:"media_type,omitempty"`
	// Size 是当前节点的未压缩大小，单位为字节。
	Size uint64 `json:"size"`
	// CompressedSize 是当前节点的压缩后大小，单位为字节。
	CompressedSize uint64 `json:"compressed_size"`
	// Duplicate 表示当前节点是否为重复条目。
	Duplicate bool `json:"duplicate,omitempty"`
	// Children 是当前目录节点的子节点列表。
	Children []PackageTree `json:"children,omitempty"`
}

// Summary 是文档级汇总统计。
type Summary struct {
	// DocumentBodies 是文档体数量。
	DocumentBodies int `json:"document_bodies"`
	// Pages 是页面总数。
	Pages int `json:"pages"`
	// ParsedPages 是成功解析的页面数。
	ParsedPages int `json:"parsed_pages"`
	// Objects 是对象总数。
	Objects int `json:"objects"`
	// PageObjects 是页面对象总数。
	PageObjects int `json:"page_objects"`
	// AnnotationObjects 是注解外观对象总数。
	AnnotationObjects int `json:"annotation_objects"`
	// TextCharacters 是文字字符总数。
	TextCharacters int `json:"text_characters"`
	// PageTextCharacters 是页面文字字符总数。
	PageTextCharacters int `json:"page_text_characters"`
	// AnnotationTextChars 是注解外观文字字符总数。
	AnnotationTextChars int `json:"annotation_text_characters"`
	// Images 是图片资源数量。
	Images int `json:"images"`
	// Fonts 是字体资源数量。
	Fonts int `json:"fonts"`
	// DrawParams 是绘制参数资源数量。
	DrawParams int `json:"draw_params"`
	// ColorSpaces 是颜色空间资源数量。
	ColorSpaces int `json:"color_spaces"`
	// Templates 是模板资源数量。
	Templates int `json:"templates"`
	// Composites 是复合对象资源数量。
	Composites int `json:"composites"`
	// Patterns 是图案资源数量。
	Patterns int `json:"patterns"`
	// Attachments 是附件数量。
	Attachments int `json:"attachments"`
	// Annotations 是注解数量。
	Annotations int `json:"annotations"`
	// Signatures 是签名数量。
	Signatures int `json:"signatures"`
}

// DocumentInfo 描述一个文档体及其元数据。
type DocumentInfo struct {
	// Index 是文档体在报告中的索引。
	Index int `json:"index"`
	// DocID 是文档体 ID。
	DocID string `json:"doc_id"`
	// Title 是文档标题。
	Title *string `json:"title,omitempty"`
	// Author 是文档作者。
	Author *string `json:"author,omitempty"`
	// Subject 是文档主题。
	Subject *string `json:"subject,omitempty"`
	// Abstract 是文档摘要。
	Abstract *string `json:"abstract,omitempty"`
	// CreationDate 是文档创建时间。
	CreationDate *time.Time `json:"creation_date,omitempty"`
	// ModDate 是文档修改时间。
	ModDate *time.Time `json:"mod_date,omitempty"`
	// DocUsage 是文档用途。
	DocUsage *string `json:"doc_usage,omitempty"`
	// Creator 是文档创建程序。
	Creator *string `json:"creator,omitempty"`
	// CreatorVersion 是文档创建程序版本。
	CreatorVersion *string `json:"creator_version,omitempty"`
	// Keywords 是文档关键词列表。
	Keywords []string `json:"keywords,omitempty"`
	// DocRoot 是文档根文件路径。
	DocRoot string `json:"doc_root"`
	// DeclaredPages 是文档声明的页面数。
	DeclaredPages int `json:"declared_pages"`
	// ParsedPages 是成功解析的页面数。
	ParsedPages int `json:"parsed_pages"`
	// TemplateCount 是文档模板数量。
	TemplateCount int `json:"template_count"`
	// ResourceFiles 是文档资源文件数。
	ResourceFiles int `json:"resource_files"`
	// HasCover 表示文档是否包含封面。
	HasCover bool `json:"has_cover"`
	// HasAttachments 表示文档是否包含附件。
	HasAttachments bool `json:"has_attachments"`
	// HasAnnotations 表示文档是否包含注解。
	HasAnnotations bool `json:"has_annotations"`
	// HasSignatures 表示文档是否包含签名。
	HasSignatures bool `json:"has_signatures"`
}

// PageInfo 描述一个页面及其对象、文本和直接资源引用。
type PageInfo struct {
	// DocumentIndex 是所属文档体在报告中的索引。
	DocumentIndex int `json:"document_index"`
	// DocumentPage 是页面在所属文档体中的序号。
	DocumentPage int `json:"document_page"`
	// PageNumber 是页面在全局分析结果中的页码。
	PageNumber int `json:"page_number"`
	// ID 是页面 ID。
	ID uint64 `json:"id"`
	// BaseLoc 是页面资源引用的基准位置。
	BaseLoc string `json:"base_loc,omitempty"`
	// Layers 是页面图层数。
	Layers int `json:"layers"`
	// Size 是页面物理尺寸信息。
	Size PageSize `json:"size"`
	// Objects 是页面对象计数。
	Objects ObjectCounts `json:"objects"`
	// Text 是页面文字统计。
	Text TextSummary `json:"text"`
	// Resources 是页面直接引用的资源 ID。
	Resources PageResources `json:"resources"`
}

// PageSize 描述页面物理尺寸和方向。
type PageSize struct {
	// X 是页面物理框的 X 坐标。
	X float64 `json:"x"`
	// Y 是页面物理框的 Y 坐标。
	Y float64 `json:"y"`
	// Width 是页面物理宽度。
	Width float64 `json:"width"`
	// Height 是页面物理高度。
	Height float64 `json:"height"`
	// Unit 是尺寸单位。
	Unit string `json:"unit"`
	// Orientation 是页面方向。
	Orientation string `json:"orientation"`
	// Source 是页面尺寸的来源。
	Source string `json:"source"`
}

// ObjectSummary 是实际展开分析的页面对象和注解外观对象的汇总。
type ObjectSummary struct {
	// Total 是对象总数。
	Total int `json:"total"`
	// Pages 是包含对象统计的页面数。
	Pages int `json:"pages"`
	// AnnotationAppearances 是已展开分析的注解外观数。
	AnnotationAppearances int `json:"annotation_appearances"`
	// AnnotationObjectCounts 是注解外观对象计数。
	AnnotationObjectCounts ObjectCounts `json:"annotation_object_counts"`
	// Text 是文字对象数。
	Text int `json:"text"`
	// Path 是路径对象数。
	Path int `json:"path"`
	// Image 是图片对象数。
	Image int `json:"image"`
	// Composite 是复合对象数。
	Composite int `json:"composite"`
	// PageBlock 是页面块对象数。
	PageBlock int `json:"page_block"`
	// PathCommands 是路径命令总数。
	PathCommands int `json:"path_commands"`
	// MaxPageBlockDepth 是页面块的最大嵌套深度。
	MaxPageBlockDepth int `json:"max_page_block_depth"`
}

// ObjectCounts 是单页面或单个对象容器的对象计数。
type ObjectCounts struct {
	// Total 是对象总数。
	Total int `json:"total"`
	// Text 是文字对象数。
	Text int `json:"text"`
	// Path 是路径对象数。
	Path int `json:"path"`
	// Image 是图片对象数。
	Image int `json:"image"`
	// Composite 是复合对象数。
	Composite int `json:"composite"`
	// PageBlock 是页面块对象数。
	PageBlock int `json:"page_block"`
	// PathCommands 是路径命令总数。
	PathCommands int `json:"path_commands"`
}

// TextSummary 是文字统计。字符数按 Unicode 码点统计。
type TextSummary struct {
	// Objects 是文字对象数。
	Objects int `json:"objects"`
	// TextCodes 是文字编码数量。
	TextCodes int `json:"text_codes"`
	// UTF8Bytes 是文字 UTF-8 编码的字节数。
	UTF8Bytes int `json:"utf8_bytes"`
	// UnicodeCodePoints 是 Unicode 码点数。
	UnicodeCodePoints int `json:"unicode_code_points"`
	// WhitespaceCodePoints 是空白 Unicode 码点数。
	WhitespaceCodePoints int `json:"whitespace_code_points"`
	// NonWhitespaceCodePoints 是非空白 Unicode 码点数。
	NonWhitespaceCodePoints int `json:"non_whitespace_code_points"`
	// Glyphs 是字形数量。
	Glyphs int `json:"glyphs"`
}

// ResourceSummary 是通用资源定义和引用统计。
type ResourceSummary struct {
	// Files 是资源文件数。
	Files int `json:"files"`
	// Declared 是声明的资源数。
	Declared int `json:"declared"`
	// Used 是被使用的资源数。
	Used int `json:"used"`
	// References 是资源引用次数。
	References int `json:"references"`
	// UniqueUsed 是被使用的不同资源数。
	UniqueUsed int `json:"unique_used"`
	// Unresolved 是无法解析的资源引用数。
	Unresolved int `json:"unresolved"`
	// MissingFiles 是缺失资源文件数。
	MissingFiles int `json:"missing_files"`
	// Unused 是未被使用的资源数。
	Unused int `json:"unused"`
	// ByType 是按资源类型统计的数量。
	ByType map[string]int `json:"by_type,omitempty"`
}

// FontResourceSummary 是字体资源统计。
type FontResourceSummary struct {
	// ResourceSummary 是通用资源统计字段。
	ResourceSummary
	// Embedded 是嵌入字体数。
	Embedded int `json:"embedded"`
}

// DrawParamResourceSummary 是绘制参数资源统计。
type DrawParamResourceSummary struct {
	// ResourceSummary 是通用资源统计字段。
	ResourceSummary
	// InheritanceCycles 是绘制参数继承环数量。
	InheritanceCycles int `json:"inheritance_cycles"`
}

// ResourceInfo 描述一个资源定义及其使用情况。
type ResourceInfo struct {
	// DocumentIndex 是资源所属文档体在报告中的索引。
	DocumentIndex int `json:"document_index"`
	// ID 是资源 ID。
	ID uint64 `json:"id"`
	// Kind 是资源类型。
	Kind string `json:"kind"`
	// Scope 是资源定义的作用域。
	Scope string `json:"scope,omitempty"`
	// SourceFile 是资源定义所在的文件路径。
	SourceFile string `json:"source_file"`
	// Path 是资源关联的文件路径。
	Path string `json:"path,omitempty"`
	// Exists 表示关联文件是否存在。
	Exists bool `json:"exists"`
	// Used 是资源被使用的次数。
	Used int `json:"used"`
	// Embedded 表示资源是否嵌入文档。
	Embedded bool `json:"embedded"`
	// FontName 是字体名称。
	FontName string `json:"font_name,omitempty"`
	// FamilyName 是字体族名称。
	FamilyName string `json:"family_name,omitempty"`
	// Charset 是字体字符集。
	Charset string `json:"charset,omitempty"`
	// Italic 表示字体是否为斜体。
	Italic bool `json:"italic,omitempty"`
	// Bold 表示字体是否为粗体。
	Bold bool `json:"bold,omitempty"`
	// Serif 表示字体是否为衬线字体。
	Serif bool `json:"serif,omitempty"`
	// FixedWidth 表示字体是否为等宽字体。
	FixedWidth bool `json:"fixed_width,omitempty"`
}

// PageResources 描述页面内容直接引用的资源 ID。
type PageResources struct {
	// Images 是页面直接引用的图片资源 ID。
	Images []uint64 `json:"images,omitempty"`
	// Fonts 是页面直接引用的字体资源 ID。
	Fonts []uint64 `json:"fonts,omitempty"`
	// Templates 是页面直接引用的模板资源 ID。
	Templates []uint64 `json:"templates,omitempty"`
	// DrawParams 是页面直接引用的绘制参数资源 ID。
	DrawParams []uint64 `json:"draw_params,omitempty"`
	// Composites 是页面直接引用的复合对象资源 ID。
	Composites []uint64 `json:"composites,omitempty"`
	// ColorSpaces 是页面直接引用的颜色空间资源 ID。
	ColorSpaces []uint64 `json:"color_spaces,omitempty"`
}

// AttachmentInfo 描述附件清单项。
type AttachmentInfo struct {
	// DocumentIndex 是附件所属文档体在报告中的索引。
	DocumentIndex int `json:"document_index"`
	// ID 是附件 ID。
	ID string `json:"id"`
	// Name 是附件名称。
	Name string `json:"name"`
	// Format 是附件格式。
	Format *string `json:"format,omitempty"`
	// Path 是附件文件路径。
	Path string `json:"path"`
	// DeclaredSize 是附件声明的大小，单位为字节。
	DeclaredSize *float64 `json:"declared_size,omitempty"`
	// ActualSize 是附件实际大小，单位为字节。
	ActualSize uint64 `json:"actual_size,omitempty"`
	// Exists 表示附件文件是否存在。
	Exists bool `json:"exists"`
	// Visible 表示附件是否可见。
	Visible bool `json:"visible"`
	// Usage 是附件用途。
	Usage string `json:"usage,omitempty"`
}

// AnnotationInfo 描述注解清单项。
type AnnotationInfo struct {
	// DocumentIndex 是注解所属文档体在报告中的索引。
	DocumentIndex int `json:"document_index"`
	// PageID 是注解所属页面的 ID。
	PageID uint64 `json:"page_id"`
	// ID 是注解 ID。
	ID string `json:"id"`
	// Type 是注解类型。
	Type string `json:"type"`
	// Subtype 是注解子类型。
	Subtype string `json:"subtype,omitempty"`
	// Creator 是注解创建者。
	Creator string `json:"creator,omitempty"`
	// Visible 表示注解是否可见。
	Visible bool `json:"visible"`
	// Print 表示注解是否打印。
	Print bool `json:"print"`
	// HasAppearance 表示注解是否包含外观。
	HasAppearance bool `json:"has_appearance"`
	// Objects 是注解外观对象计数。
	Objects ObjectCounts `json:"objects"`
}

// SignatureInfo 描述签名清单项。
type SignatureInfo struct {
	// DocumentIndex 是签名所属文档体在报告中的索引。
	DocumentIndex int `json:"document_index"`
	// ID 是签名 ID。
	ID string `json:"id"`
	// Type 是签名类型。
	Type string `json:"type,omitempty"`
	// Path 是签名文件路径。
	Path string `json:"path,omitempty"`
	// Provider 是签名提供方名称。
	Provider string `json:"provider,omitempty"`
	// Company 是签名提供方公司名称。
	Company string `json:"company,omitempty"`
	// Version 是签名版本。
	Version string `json:"version,omitempty"`
	// Method 是签名方法。
	Method string `json:"method,omitempty"`
	// Date 是签名时间。
	Date string `json:"date,omitempty"`
	// CheckMethod 是签名引用校验方法。
	CheckMethod string `json:"check_method,omitempty"`
	// ReferenceCount 是签名引用数量。
	ReferenceCount int `json:"reference_count"`
	// StampCount 是签名印章注解数量。
	StampCount int `json:"stamp_count"`
	// Pages 是签名覆盖的页面 ID 列表。
	Pages []uint64 `json:"pages,omitempty"`
	// SignedValue 是签名值文件路径。
	SignedValue string `json:"signed_value,omitempty"`
	// SignedValueExists 表示签名值文件是否存在。
	SignedValueExists bool `json:"signed_value_exists"`
	// SignedValueFormat 是签名值识别出的格式，例如 SES 或 ASN.1。
	SignedValueFormat string `json:"signed_value_format,omitempty"`
	// SignedValueParsed 表示签名值是否成功解析。
	SignedValueParsed bool `json:"signed_value_parsed"`
	// SignedValueParseError 是签名值解析失败原因。
	SignedValueParseError string `json:"signed_value_parse_error,omitempty"`
	// Seal 是印章文件路径。
	Seal string `json:"seal,omitempty"`
	// SealExists 表示印章文件是否存在。
	SealExists bool `json:"seal_exists"`
	// SealInfo 是 SignedValue 中解析出的电子印章主体信息。
	SealInfo *SignatureSealInfo `json:"seal_info,omitempty"`
	// SealSignatureAlgorithm 是印章内部签名算法 OID。
	SealSignatureAlgorithm string `json:"seal_signature_algorithm,omitempty"`
	// OuterSignatureAlgorithm 是 SignedValue 外层签名算法 OID。
	OuterSignatureAlgorithm string `json:"outer_signature_algorithm,omitempty"`
	// DigestChecked 表示是否执行了签名引用摘要校验。
	DigestChecked bool `json:"digest_checked"`
	// DigestValid 表示签名引用摘要及 DataHash 校验是否全部通过。
	DigestValid bool `json:"digest_valid"`
	// DigestMethod 是签名引用声明的摘要算法。
	DigestMethod string `json:"digest_method,omitempty"`
	// DigestReferences 是签名引用的逐项摘要结果。
	DigestReferences []SignatureDigestInfo `json:"digest_references,omitempty"`
	// DataHash 是 SES 签名中 TBS_Sign.DataHash 的校验结果。
	DataHash *SignatureDataHashInfo `json:"data_hash,omitempty"`
	// VerificationChecked 表示是否执行了密码学签名验证。
	VerificationChecked bool `json:"verification_checked"`
	// VerificationValid 表示 SES 内外两层签名是否均通过。
	VerificationValid bool `json:"verification_valid"`
	// VerificationError 是密码学验证无法执行时的错误。
	VerificationError string `json:"verification_error,omitempty"`
	// TrustChecked 表示是否执行了证书链校验。
	TrustChecked bool `json:"trust_checked"`
	// Trusted 表示两层签名证书是否均被显式信任根信任。
	Trusted bool `json:"trusted"`
	// RevocationChecked 表示是否执行了证书吊销校验。
	RevocationChecked bool `json:"revocation_checked"`
	// RevocationValid 表示两层证书均未被 CRL 吊销。
	RevocationValid bool `json:"revocation_valid"`
	// VerificationTime 是验证证书有效期使用的签名时间。
	VerificationTime string `json:"verification_time,omitempty"`
	// SealVerification 是印章内部签名验证结果。
	SealVerification *SignatureComponentInfo `json:"seal_verification,omitempty"`
	// OuterVerification 是 SignedValue 外层签名验证结果。
	OuterVerification *SignatureComponentInfo `json:"outer_verification,omitempty"`
}

// SignatureSealInfo 描述 SES SignedValue 中的印章主体信息。
type SignatureSealInfo struct {
	// ID 是电子印章标识。
	ID string `json:"id,omitempty"`
	// Name 是电子印章名称。
	Name string `json:"name,omitempty"`
	// CreateTime 是印章创建时间。
	CreateTime string `json:"create_time,omitempty"`
	// ValidFrom 是印章有效期起点。
	ValidFrom string `json:"valid_from,omitempty"`
	// ValidTo 是印章有效期终点。
	ValidTo string `json:"valid_to,omitempty"`
	// PictureType 是印章图片类型。
	PictureType string `json:"picture_type,omitempty"`
	// PictureWidth 是印章图片宽度。
	PictureWidth int64 `json:"picture_width,omitempty"`
	// PictureHeight 是印章图片高度。
	PictureHeight int64 `json:"picture_height,omitempty"`
}

// SignatureComponentInfo 描述一层签名的证书和验证结果。
type SignatureComponentInfo struct {
	// Valid 表示数学签名验证通过。
	Valid bool `json:"valid"`
	// Algorithm 是签名算法 OID。
	Algorithm string `json:"algorithm,omitempty"`
	// SignatureFormat 是签名值实际使用的编码格式。
	SignatureFormat string `json:"signature_format,omitempty"`
	// CertificateValid 表示证书在签名时间点有效。
	CertificateValid bool `json:"certificate_valid"`
	// TrustChecked 表示是否执行了证书链校验。
	TrustChecked bool `json:"trust_checked"`
	// Trusted 表示证书链校验通过。
	Trusted bool `json:"trusted"`
	// TrustError 是证书链校验失败原因。
	TrustError string `json:"trust_error,omitempty"`
	// RevocationChecked 表示是否执行了证书吊销校验。
	RevocationChecked bool `json:"revocation_checked"`
	// RevocationStatus 是吊销状态：good、revoked、unknown 或 error。
	RevocationStatus string `json:"revocation_status,omitempty"`
	// RevocationError 是吊销校验失败原因。
	RevocationError string `json:"revocation_error,omitempty"`
	// SerialNumber 是证书序列号。
	SerialNumber string `json:"serial_number,omitempty"`
	// Subject 是证书主体。
	Subject string `json:"subject,omitempty"`
	// Issuer 是证书颁发者。
	Issuer string `json:"issuer,omitempty"`
	// NotBefore 是证书有效期起点。
	NotBefore string `json:"not_before,omitempty"`
	// NotAfter 是证书有效期终点。
	NotAfter string `json:"not_after,omitempty"`
	// PublicKey 是证书公钥类型。
	PublicKey string `json:"public_key,omitempty"`
	// Error 是验证失败原因。
	Error string `json:"error,omitempty"`
}

// SignatureDigestInfo 描述一个签名引用的摘要校验结果。
type SignatureDigestInfo struct {
	// FileRef 是 Signature.xml 中的原始引用路径。
	FileRef string `json:"file_ref"`
	// ResolvedPath 是解析后的包内路径。
	ResolvedPath string `json:"resolved_path,omitempty"`
	// Exists 表示被引用文件是否存在。
	Exists bool `json:"exists"`
	// Match 表示实际摘要是否与 CheckValue 一致。
	Match bool `json:"match"`
	// Expected 是 Signature.xml 中的 Base64 摘要值。
	Expected string `json:"expected,omitempty"`
	// Actual 是实际计算得到的 Base64 摘要值。
	Actual string `json:"actual,omitempty"`
	// Error 是当前引用的校验错误。
	Error string `json:"error,omitempty"`
}

// SignatureDataHashInfo 描述 SES DataHash 的校验结果。
type SignatureDataHashInfo struct {
	// Match 表示 DataHash 是否与 Signature.xml 摘要一致。
	Match bool `json:"match"`
	// Expected 是 SignedValue.dat 中的 Base64 摘要值。
	Expected string `json:"expected,omitempty"`
	// Actual 是实际计算得到的 Base64 摘要值。
	Actual string `json:"actual,omitempty"`
	// Error 是 DataHash 的校验错误。
	Error string `json:"error,omitempty"`
}

// ReferenceEdge 描述一条文件或逻辑 ID 引用关系。
type ReferenceEdge struct {
	// From 是引用关系的起点。
	From string `json:"from"`
	// To 是引用关系的目标。
	To string `json:"to"`
	// Type 是引用关系的类型。
	Type string `json:"type"`
	// Source 是引用关系的来源位置。
	Source string `json:"source,omitempty"`
	// Exists 表示引用目标是否存在。
	Exists bool `json:"exists"`
	// Count 是相同引用关系出现的次数。
	Count int `json:"count"`
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
