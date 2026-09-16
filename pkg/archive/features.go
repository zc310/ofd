package archive

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zc310/ofd/internal/core"
)

func scanFeatures(filename string, maxXMLBytes int64) (FeatureSummary, error) {
	packageReader, err := openPackage(filename)
	if err != nil {
		return FeatureSummary{}, err
	}
	defer packageReader.Close()
	var summary FeatureSummary
	var scanErr error
	err = packageReader.WalkEntries(func(entry core.Entry) bool {
		if entry.IsDir || filepath.Ext(entry.Path) != ".xml" {
			return true
		}
		reader, openErr := packageReader.OpenEntry(entry)
		if openErr != nil {
			scanErr = fmt.Errorf("打开 XML 条目 %s 失败：%w", entry.Path, openErr)
			return false
		}
		scanErr = scanXML(reader, &summary, maxXMLBytes)
		closeErr := reader.Close()
		if scanErr == nil && closeErr != nil {
			scanErr = fmt.Errorf("关闭 XML 条目 %s 失败：%w", entry.Path, closeErr)
		}
		if scanErr != nil {
			return false
		}
		return true
	})
	if err != nil {
		return summary, err
	}
	if scanErr != nil {
		return summary, scanErr
	}
	return summary, nil
}

func scanXML(reader io.Reader, summary *FeatureSummary, maxXMLBytes int64) error {
	var limited *io.LimitedReader
	if maxXMLBytes > 0 && maxXMLBytes < int64(^uint64(0)>>1) {
		limited = &io.LimitedReader{R: reader, N: maxXMLBytes + 1}
		reader = limited
	}
	decoder := xml.NewDecoder(reader)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if limited != nil && limited.N == 0 {
				return fmt.Errorf("XML 内容超过大小限制：%d", maxXMLBytes)
			}
			return nil
		}
		if err != nil {
			return err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		local := strings.ToLower(start.Name.Local)
		switch local {
		case "action":
			summary.Actions++
		case "audio":
			summary.Audio++
		case "video":
			summary.Video++
		case "media":
			summary.Media++
		case "encrypt", "encryption", "encrypteddoc":
			summary.Encryption++
		case "extension", "extensions":
			summary.Extensions++
		}
	}
}

func extractAttachments(input, output string, attachments []Attachment, maxSize int64) ([]Attachment, []Issue, error) {
	packageReader, err := openPackage(input)
	if err != nil {
		return attachments, nil, err
	}
	defer packageReader.Close()
	if err := os.MkdirAll(output, 0o700); err != nil {
		return attachments, nil, err
	}
	used := map[string]bool{}
	var issues []Issue
	for index := range attachments {
		item := &attachments[index]
		if !item.Exists || item.SourcePath == "" {
			issues = append(issues, Issue{Severity: "warning", Code: "attachment.missing", Message: fmt.Sprintf("附件不存在：%s", item.SourcePath)})
			continue
		}
		entry, ok := packageReader.Lookup(item.SourcePath)
		if !ok || entry.IsDir {
			issues = append(issues, Issue{Severity: "warning", Code: "attachment.missing", Message: fmt.Sprintf("附件不存在：%s", item.SourcePath)})
			continue
		}
		if maxSize > 0 && entry.UncompressedSize > uint64(maxSize) {
			issues = append(issues, Issue{Severity: "error", Code: "attachment.too_large", Message: fmt.Sprintf("附件超过大小限制：%s", item.SourcePath)})
			continue
		}
		reader, openErr := packageReader.OpenEntry(entry)
		if openErr != nil {
			issues = append(issues, Issue{Severity: "error", Code: "attachment.open", Message: openErr.Error()})
			continue
		}
		name := uniquePath(item.Name, used)
		path := filepath.Join(output, name)
		file, createErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if createErr != nil {
			_ = reader.Close()
			return attachments, issues, createErr
		}
		_, copyErr := io.Copy(file, reader)
		closeErr := file.Close()
		readerCloseErr := reader.Close()
		if copyErr != nil || closeErr != nil || readerCloseErr != nil {
			_ = os.Remove(path)
			issues = append(issues, Issue{Severity: "error", Code: "attachment.copy", Message: fmt.Sprintf("复制附件失败：%s", item.SourcePath)})
			continue
		}
		item.ArchivePath = filepath.ToSlash(filepath.Join("attachments", name))
		item.SHA256, err = hashFile(path)
		if err != nil {
			_ = os.Remove(path)
			issues = append(issues, Issue{Severity: "error", Code: "attachment.hash", Message: fmt.Sprintf("计算附件固定性失败：%s", item.SourcePath)})
			continue
		}
		item.Copied = true
	}
	return attachments, issues, nil
}
