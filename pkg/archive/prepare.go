package archive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zc310/ofd/internal/utils"
	"github.com/zc310/ofd/pkg/analyzer"
)

// Prepare 复制源 OFD，并写入自包含的归档目录。
func Prepare(ctx context.Context, input, output string, metadata Metadata, profile string, options Options) (Report, error) {
	options = withDefaults(options)
	manifest, report, err := BuildManifest(ctx, input, metadata, profile, options)
	if err != nil {
		return report, err
	}
	if utils.SamePath(input, output) {
		return report, fmt.Errorf("归档输出目录不能是输入文件")
	}
	if info, statErr := os.Stat(output); statErr == nil && !info.IsDir() {
		return report, fmt.Errorf("归档输出路径不是目录：%s", output)
	}
	if info, statErr := os.Lstat(output); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return report, fmt.Errorf("归档输出路径不能是符号链接：%s", output)
	}
	if err := ensureEmptyDirectory(output); err != nil {
		return report, err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return report, fmt.Errorf("解析归档输出路径失败：%w", err)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return report, fmt.Errorf("创建归档输出父目录失败：%w", err)
	}
	staging, err := os.MkdirTemp(filepath.Dir(output), ".ofd-archive-*")
	if err != nil {
		return report, fmt.Errorf("创建归档临时目录失败：%w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := os.MkdirAll(filepath.Join(staging, "original"), 0o700); err != nil {
		return report, err
	}
	if err := os.MkdirAll(filepath.Join(staging, "metadata"), 0o700); err != nil {
		return report, err
	}
	if err := os.MkdirAll(filepath.Join(staging, "reports"), 0o700); err != nil {
		return report, err
	}
	if err := os.MkdirAll(filepath.Join(staging, "fixity"), 0o700); err != nil {
		return report, err
	}
	if err := os.MkdirAll(filepath.Join(staging, "attachments"), 0o700); err != nil {
		return report, err
	}

	original := filepath.Join(staging, "original", "document.ofd")
	if err := copyFile(input, original); err != nil {
		return report, fmt.Errorf("保存原始 OFD 失败：%w", err)
	}
	if err := writeJSON(filepath.Join(staging, "metadata", "archive.json"), metadata); err != nil {
		return report, fmt.Errorf("写入档案元数据失败：%w", err)
	}
	profilePath := ""
	if profile != "" {
		ext := strings.ToLower(filepath.Ext(profile))
		if !isSupportedProfileExtension(ext) {
			return report, fmt.Errorf("profile 文件必须使用 .json、.yaml 或 .yml 扩展名")
		}
		profilePath = filepath.ToSlash(filepath.Join("metadata", "profile"+ext))
		if err := copyFile(profile, filepath.Join(staging, filepath.FromSlash(profilePath))); err != nil {
			return report, fmt.Errorf("保存档案 profile 失败：%w", err)
		}
	}
	if err := writeJSON(filepath.Join(staging, "reports", "analysis.json"), report.AnalysisReport); err != nil {
		return report, fmt.Errorf("写入分析报告失败：%w", err)
	}
	var attachmentIssues []Issue
	report.Attachments, attachmentIssues, err = extractAttachments(input, filepath.Join(staging, "attachments"), report.Attachments, options.MaxAttachmentSize)
	if err != nil {
		return report, fmt.Errorf("提取附件失败：%w", err)
	}
	report.Issues = append(report.Issues, attachmentIssues...)
	report.Status = statusFor(report.Issues, options.FailOnWarning)
	if err := writeJSON(filepath.Join(staging, "reports", "check.json"), report); err != nil {
		return report, fmt.Errorf("写入校验报告失败：%w", err)
	}
	manifest.Attachments = report.Attachments
	manifest.Profile = profilePath
	manifest.Source.Path = "original/document.ofd"
	manifest.Files = []FileInfo{
		{Path: "original/document.ofd", Size: report.Input.Size, SHA256: report.Input.SHA256, Algorithm: "sha256"},
		{Path: "metadata/archive.json", Algorithm: "sha256"},
		{Path: "reports/check.json", Algorithm: "sha256"},
		{Path: "reports/analysis.json", Algorithm: "sha256"},
	}
	if profilePath != "" {
		manifest.Files = append(manifest.Files, FileInfo{Path: profilePath, Algorithm: "sha256"})
	}
	for _, item := range report.Attachments {
		if item.Copied {
			manifest.Files = append(manifest.Files, FileInfo{Path: item.ArchivePath, Size: int64(item.Size), SHA256: item.SHA256, Algorithm: "sha256"})
		}
	}
	metadataFilesToHash := []string{"metadata/archive.json", "reports/check.json", "reports/analysis.json"}
	if profilePath != "" {
		metadataFilesToHash = append(metadataFilesToHash, profilePath)
	}
	metadataFiles, err := buildFixity(staging, metadataFilesToHash)
	if err != nil {
		return report, err
	}
	metadataByPath := make(map[string]FileInfo, len(metadataFiles))
	for _, item := range metadataFiles {
		metadataByPath[item.Path] = item
	}
	for index := range manifest.Files {
		if item, ok := metadataByPath[manifest.Files[index].Path]; ok {
			manifest.Files[index] = item
		}
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return report, err
	}
	if err := os.WriteFile(filepath.Join(staging, "metadata", "manifest.json"), append(manifestData, '\n'), 0o600); err != nil {
		return report, fmt.Errorf("写入归档清单失败：%w", err)
	}
	files := []string{"original/document.ofd", "metadata/archive.json", "metadata/manifest.json", "reports/check.json", "reports/analysis.json"}
	if profilePath != "" {
		files = append(files, profilePath)
	}
	for _, item := range report.Attachments {
		if item.Copied {
			files = append(files, item.ArchivePath)
		}
	}
	fixity, err := buildFixity(staging, files)
	if err != nil {
		return report, err
	}
	if err := writeFixity(filepath.Join(staging, "fixity", "manifest.sha256"), fixity); err != nil {
		return report, fmt.Errorf("写入固定性清单失败：%w", err)
	}
	if err := commitPreparedDirectory(staging, output); err != nil {
		return report, err
	}
	committed = true
	return report, nil
}

func commitPreparedDirectory(staging, output string) error {
	backup := ""
	if _, err := os.Stat(output); err == nil {
		backup = output + ".old"
		if _, backupErr := os.Lstat(backup); backupErr == nil {
			return fmt.Errorf("归档输出临时备份路径已存在：%s", backup)
		} else if !os.IsNotExist(backupErr) {
			return fmt.Errorf("检查归档输出临时备份路径失败：%w", backupErr)
		}
		if err := os.Rename(output, backup); err != nil {
			return fmt.Errorf("暂存原归档输出目录失败：%w", err)
		}
	}
	if err := os.Rename(staging, output); err != nil {
		if backup != "" {
			_ = os.Rename(backup, output)
		}
		return fmt.Errorf("提交归档输出目录失败：%w", err)
	}
	if backup != "" {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("清理归档输出临时备份失败：%w", err)
		}
	}
	return nil
}

func ensureEmptyDirectory(path string) error {
	entries, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("检查归档输出目录失败：%w", err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("归档输出目录必须为空：%s", path)
	}
	return nil
}

func Verify(root string) (Report, error) {
	start := now()
	report := Report{SchemaVersion: SchemaVersion, Tool: ToolInfo{Name: "ofd-archive", Version: ToolVersion}, Status: StatusNotAssessed, StartedAt: start, Issues: []Issue{}}
	report.Input.Path = root
	rootInfo, rootErr := os.Lstat(root)
	if rootErr != nil {
		return report, fmt.Errorf("读取归档目录失败：%w", rootErr)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return report, fmt.Errorf("归档路径不是普通目录：%s", root)
	}
	fixityFiles, issues, err := readFixity(root)
	if err != nil {
		return report, err
	}
	report.Issues = append(report.Issues, issues...)
	manifestPath := filepath.Join(root, "metadata", "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return report, fmt.Errorf("读取归档清单失败：%w", err)
	}
	var manifest Manifest
	if err := jsonUnmarshalStrict(data, &manifest); err != nil {
		return report, fmt.Errorf("解析归档清单失败：%w", err)
	}
	metadataData, metadataErr := os.ReadFile(filepath.Join(root, "metadata", "archive.json"))
	if metadataErr != nil {
		return report, fmt.Errorf("读取档案元数据失败：%w", metadataErr)
	}
	var metadata Metadata
	if err := jsonUnmarshalStrict(metadataData, &metadata); err != nil {
		return report, fmt.Errorf("解析档案元数据失败：%w", err)
	}
	if metadata != manifest.Metadata {
		report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.metadata_mismatch", Message: "manifest 中的档案元数据与 metadata/archive.json 不一致"})
	}
	if manifest.SchemaVersion != SchemaVersion {
		report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.version", Message: fmt.Sprintf("不支持的归档清单版本：%s", manifest.SchemaVersion)})
	}
	if manifest.Report != "reports/check.json" || manifest.Analysis != "reports/analysis.json" || manifest.Fixity != "fixity/manifest.sha256" {
		report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.reference_mismatch", Message: "归档清单中的 report、analysis 或 fixity 路径不符合准备目录约定"})
	}
	checkReport, analysisReport, reportErr := readStoredReports(root)
	if reportErr != nil {
		return report, reportErr
	}
	report.Issues = append(report.Issues, validateStoredReports(manifest, checkReport, analysisReport)...)
	requiredPaths := []string{"original/document.ofd", "metadata/archive.json", "reports/check.json", "reports/analysis.json"}
	registered := make(map[string]FileInfo, len(manifest.Files))
	for _, item := range manifest.Files {
		clean, pathErr := cleanRelative(item.Path)
		if pathErr != nil {
			report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.invalid_path", Message: pathErr.Error()})
			continue
		}
		if _, exists := registered[clean]; exists {
			report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.duplicate_path", Message: "归档清单存在重复路径：" + clean})
			continue
		}
		item.Path = clean
		if item.Algorithm != "sha256" || item.SHA256 == "" {
			report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.invalid_digest", Message: "归档清单必须使用非空 SHA-256 摘要：" + clean})
		}
		registered[clean] = item
	}
	for _, required := range requiredPaths {
		if _, ok := registered[required]; !ok {
			report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.required_file", Message: "归档清单缺少必需文件：" + required})
		}
	}
	if manifest.Profile != "" {
		profilePath, pathErr := cleanRelative(manifest.Profile)
		if pathErr != nil {
			report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.profile_path", Message: pathErr.Error()})
		} else if _, ok := registered[profilePath]; !ok {
			report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.profile_unregistered", Message: "归档清单 profile 未登记为归档文件：" + profilePath})
		}
	}
	for _, attachment := range manifest.Attachments {
		if !attachment.Copied && attachment.ArchivePath != "" {
			report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.attachment_state", Message: "未复制的附件不能包含归档路径：" + attachment.ArchivePath})
		}
		if !attachment.Copied || attachment.ArchivePath == "" {
			continue
		}
		attachmentPath, pathErr := cleanRelative(attachment.ArchivePath)
		if pathErr != nil {
			report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.attachment_path", Message: pathErr.Error()})
		} else if file, ok := registered[attachmentPath]; !ok {
			report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.attachment_unregistered", Message: "附件未登记为归档文件：" + attachmentPath})
		} else {
			if attachment.Size != uint64(file.Size) {
				report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.attachment_size_mismatch", Message: "附件登记大小不匹配：" + attachmentPath})
			}
			if attachment.SHA256 == "" || !strings.EqualFold(attachment.SHA256, file.SHA256) {
				report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.attachment_digest_mismatch", Message: "附件登记摘要不匹配：" + attachmentPath})
			}
		}
	}
	fixitySet := make(map[string]bool, len(fixityFiles))
	for _, item := range fixityFiles {
		fixitySet[item.Path] = true
		registeredItem, ok := registered[item.Path]
		if !ok {
			if item.Path == "metadata/manifest.json" {
				continue
			}
			report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.unregistered_file", Message: "固定性文件未登记：" + item.Path})
			continue
		}
		if registeredItem.SHA256 != "" && !strings.EqualFold(registeredItem.SHA256, item.SHA256) {
			report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.digest_mismatch", Message: "归档清单摘要不匹配：" + item.Path})
		}
	}
	for _, required := range append(requiredPaths, "metadata/manifest.json") {
		if !fixitySet[required] {
			report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.unfixed_file", Message: "必需文件未出现在固定性清单：" + required})
		}
	}
	for _, item := range manifest.Files {
		clean, pathErr := cleanRelative(item.Path)
		if pathErr == nil && !fixitySet[clean] {
			report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.unfixed_file", Message: "归档清单文件未出现在固定性清单：" + clean})
		}
		if pathErr == nil {
			if info, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(clean))); statErr != nil {
				report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.missing_file", Message: "归档清单文件不存在：" + clean})
			} else if item.Size != info.Size() {
				report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.size_mismatch", Message: "归档清单文件大小不匹配：" + clean})
			}
		}
	}
	if manifest.Source.Path != "" {
		sourcePath, pathErr := cleanRelative(manifest.Source.Path)
		if pathErr != nil {
			report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.source_path", Message: pathErr.Error()})
		} else if source, ok := registered[sourcePath]; !ok {
			report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.source_unregistered", Message: "归档清单 source 未登记：" + sourcePath})
		} else {
			if source.SHA256 != "" && manifest.Source.SHA256 != "" && !strings.EqualFold(source.SHA256, manifest.Source.SHA256) {
				report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.source_digest_mismatch", Message: "归档清单 source 摘要不匹配"})
			}
			if source.Size != manifest.Source.Size {
				report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.source_size_mismatch", Message: "归档清单 source 大小不匹配"})
			}
		}
	}
	if manifest.Source.Path != "original/document.ofd" {
		report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.source_invalid", Message: "归档清单 source 必须指向 original/document.ofd"})
	}
	if _, err := os.Stat(filepath.Join(root, "original", "document.ofd")); err != nil {
		report.Issues = append(report.Issues, Issue{Severity: "error", Code: "original.missing", Message: err.Error()})
	}
	registeredFixity := make(map[string]bool, len(fixityFiles))
	for _, item := range fixityFiles {
		registeredFixity[item.Path] = true
	}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.symlink", Message: "准备目录不能包含符号链接：" + path})
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		clean, cleanErr := cleanRelative(relative)
		if cleanErr != nil {
			return cleanErr
		}
		if clean == "fixity/manifest.sha256" || clean == "metadata/manifest.json" {
			return nil
		}
		if !registeredFixity[clean] {
			report.Issues = append(report.Issues, Issue{Severity: "error", Code: "manifest.unregistered_file", Message: "准备目录存在未登记文件：" + clean})
		}
		return nil
	}); err != nil {
		return report, fmt.Errorf("遍历归档目录失败：%w", err)
	}
	report.Status = statusFor(report.Issues, false)
	report.DurationMS = now().Sub(start).Milliseconds()
	return report, nil
}

func readStoredReports(root string) (Report, analyzer.Report, error) {
	var checkReport Report
	checkData, err := os.ReadFile(filepath.Join(root, "reports", "check.json"))
	if err != nil {
		return checkReport, analyzer.Report{}, fmt.Errorf("读取归档校验报告失败：%w", err)
	}
	if err := jsonUnmarshalStrict(checkData, &checkReport); err != nil {
		return checkReport, analyzer.Report{}, fmt.Errorf("解析归档校验报告失败：%w", err)
	}

	var analysisReport analyzer.Report
	analysisData, err := os.ReadFile(filepath.Join(root, "reports", "analysis.json"))
	if err != nil {
		return checkReport, analysisReport, fmt.Errorf("读取归档分析报告失败：%w", err)
	}
	if err := jsonUnmarshalStrict(analysisData, &analysisReport); err != nil {
		return checkReport, analysisReport, fmt.Errorf("解析归档分析报告失败：%w", err)
	}
	return checkReport, analysisReport, nil
}

func validateStoredReports(manifest Manifest, checkReport Report, analysisReport analyzer.Report) []Issue {
	var issues []Issue
	if checkReport.SchemaVersion != SchemaVersion {
		issues = append(issues, Issue{Severity: "error", Code: "report.check_version", Message: fmt.Sprintf("归档校验报告版本不受支持：%s", checkReport.SchemaVersion)})
	}
	if checkReport.Tool.Name != "ofd-archive" || checkReport.Tool.Version == "" {
		issues = append(issues, Issue{Severity: "error", Code: "report.check_tool", Message: "归档校验报告的工具信息无效"})
	}
	if checkReport.Input.Size != manifest.Source.Size || !strings.EqualFold(checkReport.Input.SHA256, manifest.Source.SHA256) {
		issues = append(issues, Issue{Severity: "error", Code: "report.check_source", Message: "归档校验报告的输入固定性信息与 manifest 不一致"})
	}
	if analysisReport.SchemaVersion != analyzer.SchemaVersion {
		issues = append(issues, Issue{Severity: "error", Code: "report.analysis_version", Message: fmt.Sprintf("归档分析报告版本不受支持：%s", analysisReport.SchemaVersion)})
	}
	if analysisReport.Tool.Name != "ofd-analyzer" || analysisReport.Tool.Version == "" {
		issues = append(issues, Issue{Severity: "error", Code: "report.analysis_tool", Message: "归档分析报告的工具信息无效"})
	}
	if analysisReport.Input.Size != manifest.Source.Size {
		issues = append(issues, Issue{Severity: "error", Code: "report.analysis_source", Message: "归档分析报告的输入大小与 manifest 不一致"})
	}
	return issues
}

var now = func() time.Time { return time.Now() }

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
