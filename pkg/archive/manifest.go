package archive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BuildManifest 创建技术和档案清单，且不会修改输入文件。
func BuildManifest(ctx context.Context, input string, metadata Metadata, profile string, options Options) (Manifest, Report, error) {
	report, err := Check(ctx, input, options)
	if err != nil {
		return Manifest{}, report, err
	}
	manifest := Manifest{
		SchemaVersion: SchemaVersion,
		Tool:          report.Tool,
		CreatedAt:     time.Now(),
		Source:        report.Input,
		Metadata:      metadata,
		Profile:       profile,
		Report:        "reports/check.json",
		Analysis:      "reports/analysis.json",
		Fixity:        "fixity/manifest.sha256",
		Attachments:   report.Attachments,
		Files:         []FileInfo{report.Input},
	}
	if err := validateMetadata(metadata, profile); err != nil {
		return manifest, report, err
	}
	if _, err := os.Stat(input); err != nil {
		return manifest, report, fmt.Errorf("输入文件：%w", err)
	}
	return manifest, report, nil
}

func validateMetadata(metadata Metadata, profile string) error {
	if profile == "" {
		return nil
	}
	ext := strings.ToLower(filepath.Ext(profile))
	if !isSupportedProfileExtension(ext) {
		return fmt.Errorf("profile 文件必须使用 .json、.yaml 或 .yml 扩展名")
	}
	data, err := os.ReadFile(profile)
	if err != nil {
		return fmt.Errorf("读取档案 profile 失败：%w", err)
	}
	var value Profile
	if ext == ".yaml" || ext == ".yml" {
		if err := yamlUnmarshal(data, &value); err != nil {
			return fmt.Errorf("解析档案 profile 失败：%w", err)
		}
	} else {
		if err := jsonUnmarshalStrict(data, &value); err != nil {
			return fmt.Errorf("解析档案 profile 失败：%w", err)
		}
	}
	for _, field := range value.RequiredFields {
		var current string
		switch field {
		case "archive_code":
			current = metadata.ArchiveCode
		case "fonds_code":
			current = metadata.FondsCode
		case "retention_period":
			current = metadata.RetentionPeriod
		case "security_classification":
			current = metadata.SecurityClassification
		case "open_status":
			current = metadata.OpenStatus
		default:
			return fmt.Errorf("profile 包含不支持的必填字段 %q", field)
		}
		if current == "" {
			return fmt.Errorf("缺少 profile 要求的档案字段 %q", field)
		}
	}
	return nil
}

func isSupportedProfileExtension(ext string) bool {
	return ext == ".json" || ext == ".yaml" || ext == ".yml"
}
