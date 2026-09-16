// Package archive 提供 OFD 文档的档案预检、清单和归档准备辅助功能。
// 它不会修改源 OFD，也不宣称文档符合法律或合规要求。
package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zc310/ofd/internal/core"
	"github.com/zc310/ofd/pkg/analyzer"
	"github.com/zc310/ofd/pkg/validator"
)

const (
	SchemaVersion = "1"
	ToolVersion   = "0.0.1"

	StatusPassed      = "passed"
	StatusWarning     = "warning"
	StatusFailed      = "failed"
	StatusNotAssessed = "not_assessed"
	StatusUnsupported = "unsupported"
)

type Options struct {
	ValidatorOptions  []validator.Option
	AnalyzerOptions   []analyzer.Option
	FailOnWarning     bool
	HashAlgorithm     string
	MaxAttachmentSize int64
	MaxXMLBytes       int64
}

type Issue struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
	Path     string `json:"path,omitempty"`
}

type FileInfo struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
	Algorithm string `json:"algorithm"`
}

type FeatureSummary struct {
	Actions    int `json:"actions,omitempty"`
	Audio      int `json:"audio,omitempty"`
	Video      int `json:"video,omitempty"`
	Media      int `json:"media,omitempty"`
	Encryption int `json:"encryption,omitempty"`
	Extensions int `json:"extensions,omitempty"`
}

type Attachment struct {
	DocumentIndex int    `json:"document_index"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Format        string `json:"format,omitempty"`
	SourcePath    string `json:"source_path"`
	ArchivePath   string `json:"archive_path,omitempty"`
	Size          uint64 `json:"size,omitempty"`
	SHA256        string `json:"sha256,omitempty"`
	Exists        bool   `json:"exists"`
	Copied        bool   `json:"copied"`
	Conversion    string `json:"conversion,omitempty"`
}

type Report struct {
	SchemaVersion   string           `json:"schema_version"`
	Tool            ToolInfo         `json:"tool"`
	Input           FileInfo         `json:"input"`
	Status          string           `json:"status"`
	StartedAt       time.Time        `json:"started_at"`
	DurationMS      int64            `json:"duration_ms"`
	Issues          []Issue          `json:"issues"`
	Features        FeatureSummary   `json:"features"`
	Attachments     []Attachment     `json:"attachments"`
	ValidatorReport validator.Report `json:"validator_report"`
	AnalysisReport  analyzer.Report  `json:"analysis_report"`
}

type ToolInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Metadata struct {
	ArchiveCode            string `json:"archive_code" yaml:"archive_code"`
	FondsCode              string `json:"fonds_code" yaml:"fonds_code"`
	RetentionPeriod        string `json:"retention_period" yaml:"retention_period"`
	SecurityClassification string `json:"security_classification" yaml:"security_classification"`
	OpenStatus             string `json:"open_status" yaml:"open_status"`
}

type Profile struct {
	Name           string   `json:"name" yaml:"name"`
	RequiredFields []string `json:"required_fields" yaml:"required_fields"`
}

type Manifest struct {
	SchemaVersion string       `json:"schema_version"`
	Tool          ToolInfo     `json:"tool"`
	CreatedAt     time.Time    `json:"created_at"`
	Source        FileInfo     `json:"source"`
	Metadata      Metadata     `json:"metadata"`
	Profile       string       `json:"profile,omitempty"`
	Report        string       `json:"report"`
	Analysis      string       `json:"analysis"`
	Fixity        string       `json:"fixity"`
	Attachments   []Attachment `json:"attachments"`
	Files         []FileInfo   `json:"files"`
}

func DefaultOptions() Options {
	return Options{
		ValidatorOptions:  []validator.Option{validator.WithFailOnWarning(false)},
		AnalyzerOptions:   []analyzer.Option{analyzer.WithTree(false)},
		HashAlgorithm:     "sha256",
		MaxAttachmentSize: 64 << 20,
		MaxXMLBytes:       64 << 20,
	}
}

func Check(ctx context.Context, input string, options Options) (Report, error) {
	start := time.Now()
	report := Report{
		SchemaVersion: SchemaVersion,
		Tool:          ToolInfo{Name: "ofd-archive", Version: ToolVersion},
		Status:        StatusNotAssessed,
		StartedAt:     start,
		Issues:        []Issue{},
		Attachments:   []Attachment{},
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.ToLower(options.HashAlgorithm) != "sha256" && options.HashAlgorithm != "" {
		return report, fmt.Errorf("不支持的固定性算法 %q", options.HashAlgorithm)
	}
	if options.MaxAttachmentSize < 0 {
		return report, errors.New("附件大小限制不能为负数")
	}
	if options.MaxXMLBytes < 0 {
		return report, errors.New("XML 大小限制不能为负数")
	}
	options = withDefaults(options)
	if info, err := os.Stat(input); err == nil {
		report.Input.Path = input
		report.Input.Size = info.Size()
		report.Input.SHA256, err = hashFile(input)
		if err != nil {
			return report, fmt.Errorf("计算输入固定性失败: %w", err)
		}
		report.Input.Algorithm = "sha256"
	} else {
		report.Issues = append(report.Issues, Issue{Severity: "error", Code: "input.open", Message: fmt.Sprintf("无法打开输入文件：%v", err)})
		report.Status = StatusFailed
		report.DurationMS = time.Since(start).Milliseconds()
		return report, nil
	}

	instance, err := validator.New(options.ValidatorOptions...)
	if err != nil {
		return report, fmt.Errorf("创建校验器失败: %w", err)
	}
	report.ValidatorReport = instance.ValidatePath(ctx, input)
	analysis, analysisErr := analyzer.Analyze(input, options.AnalyzerOptions...)
	report.AnalysisReport = analysis
	if analysisErr != nil {
		report.Issues = append(report.Issues, Issue{Severity: "error", Code: "analysis.failed", Message: analysisErr.Error()})
	}
	for _, issue := range report.ValidatorReport.Issues {
		severity := string(issue.Severity)
		report.Issues = append(report.Issues, Issue{Severity: severity, Code: "validator." + issue.Code, Message: issue.Message, Path: issue.File})
	}
	for _, warning := range report.AnalysisReport.Warnings {
		report.Issues = append(report.Issues, Issue{Severity: "warning", Code: "analyzer.warning", Message: warning})
	}
	report.Features, err = scanFeatures(input, options.MaxXMLBytes)
	if err != nil {
		report.Issues = append(report.Issues, Issue{Severity: "warning", Code: "features.unavailable", Message: err.Error()})
	}
	for _, feature := range []struct {
		count      int
		code, name string
	}{
		{report.Features.Actions, "feature.action", "动作"},
		{report.Features.Audio, "feature.audio", "音频"},
		{report.Features.Video, "feature.video", "视频"},
		{report.Features.Media, "feature.media", "媒体"},
		{report.Features.Encryption, "feature.encryption", "加密"},
	} {
		if feature.count > 0 {
			report.Issues = append(report.Issues, Issue{Severity: "warning", Code: feature.code, Message: fmt.Sprintf("文档包含 %d 个%s元素；未执行自动删除或转换，请人工确认", feature.count, feature.name)})
		}
	}
	report.Attachments = attachmentsFromAnalysis(report.AnalysisReport)
	report.Status = statusFor(report.Issues, options.FailOnWarning)
	report.DurationMS = time.Since(start).Milliseconds()
	return report, nil
}

func withDefaults(options Options) Options {
	defaults := DefaultOptions()
	if options.HashAlgorithm == "" {
		options.HashAlgorithm = defaults.HashAlgorithm
	}
	if options.MaxAttachmentSize == 0 {
		options.MaxAttachmentSize = defaults.MaxAttachmentSize
	}
	if options.MaxXMLBytes == 0 {
		options.MaxXMLBytes = defaults.MaxXMLBytes
	}
	if options.ValidatorOptions == nil {
		options.ValidatorOptions = defaults.ValidatorOptions
	}
	if options.AnalyzerOptions == nil {
		options.AnalyzerOptions = defaults.AnalyzerOptions
	}
	return options
}

func statusFor(issues []Issue, failOnWarning bool) string {
	for _, issue := range issues {
		if issue.Severity == "error" {
			return StatusFailed
		}
	}
	if failOnWarning {
		for _, issue := range issues {
			if issue.Severity == "warning" {
				return StatusFailed
			}
		}
	}
	for _, issue := range issues {
		if issue.Severity == "warning" {
			return StatusWarning
		}
	}
	return StatusPassed
}

func attachmentsFromAnalysis(report analyzer.Report) []Attachment {
	items := make([]Attachment, 0, len(report.Attachments))
	for _, item := range report.Attachments {
		format := ""
		if item.Format != nil {
			format = *item.Format
		}
		items = append(items, Attachment{DocumentIndex: item.DocumentIndex, ID: item.ID, Name: item.Name, Format: format, SourcePath: item.Path, Size: item.ActualSize, Exists: item.Exists})
	}
	return items
}

func hashFile(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeJSON(filename string, value any) error {
	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func cleanRelative(value string) (string, error) {
	value = filepath.ToSlash(value)
	if value == "" || filepath.IsAbs(value) || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\\x00") {
		return "", errors.New("路径必须为非空相对路径")
	}
	clean := filepath.ToSlash(filepath.Clean(value))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("路径越界：%s", value)
	}
	return clean, nil
}

func uniquePath(name string, used map[string]bool) string {
	base := filepath.Base(filepath.ToSlash(name))
	if base == "." || base == "/" || base == "" {
		base = "attachment"
	}
	base = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`/\\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, base)
	candidate := base
	for index := 2; used[candidate]; index++ {
		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)
		candidate = fmt.Sprintf("%s-%d%s", stem, index, ext)
	}
	used[candidate] = true
	return candidate
}

func openPackage(filename string) (*core.Package, error) { return core.OpenFile(filename) }

func sortFiles(files []FileInfo) {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
}
